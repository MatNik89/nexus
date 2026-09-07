//go:build linux

// Package repl is the 14.1-min terminal client: line input, streamed
// rendering, and a prompt that ALWAYS shows the session mode (HARDQ F2).
package repl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
)

// Prompt renders the input prompt; yolo mode is always visible.
func Prompt(yolo bool) string {
	if yolo {
		return "nexus[YOLO]> "
	}
	return "nexus> "
}

type frame struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Yolo bool   `json:"yolo,omitempty"`
	// Code/Tool: typed error metadata crossing the UDS structurally
	// (tgout plan — the edge maps codes without parsing text).
	Code string `json:"code,omitempty"`
	Tool string `json:"tool,omitempty"`
}

// Run drives the REPL until input ends: prompt → line → chat → render
// (deltas as they arrive; a final replaces nothing — deltas ARE the
// stream, the final line closes the reply; errors render as errors).
func Run(in io.Reader, out io.Writer, sock string, yolo bool) error {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return fmt.Errorf("repl: daemon not reachable at %s — is `nexus daemon` running? (%w)", sock, err)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	if err := enc.Encode(frame{Type: "hello", Yolo: yolo}); err != nil {
		return fmt.Errorf("repl: %w", err)
	}
	r := bufio.NewReader(conn)
	sc := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, Prompt(yolo))
		if !sc.Scan() {
			fmt.Fprintln(out)
			return sc.Err()
		}
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if text == "/quit" || text == "/exit" {
			return nil
		}
		if err := enc.Encode(frame{Type: "chat", Text: text}); err != nil {
			return fmt.Errorf("repl: send: %w", err)
		}
		streamed := false
	reply:
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return fmt.Errorf("repl: daemon connection broke: %w", err)
			}
			var f frame
			if err := json.Unmarshal([]byte(line), &f); err != nil {
				return fmt.Errorf("repl: malformed frame: %w", err)
			}
			switch f.Type {
			case "delta":
				streamed = true
				fmt.Fprint(out, f.Text)
			case "final":
				if streamed {
					// Deltas already printed the content; close the line.
					fmt.Fprintln(out)
				} else {
					fmt.Fprintln(out, f.Text)
				}
				break reply
			case "error":
				if f.Code == "TOOL_SCHEMA_DRIFT" {
					// Structural edge mapping (tgout impl codex #1):
					// same Croatian message as the Telegram edge.
					fmt.Fprintf(out, "Nisam uspio ispravno pozvati alat (%s). Preformuliraj zahtjev.\n", f.Tool)
				} else {
					fmt.Fprintf(out, "error: %s\n", f.Text)
				}
				break reply
			}
		}
	}
}

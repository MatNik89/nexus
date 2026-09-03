//go:build linux

// T17 REPL half: prompt surfaces the yolo mode (HARDQ F2 — the user
// ALWAYS sees the mode), frames render in order (delta streaming print +
// final), error frames render as errors.
package repl

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptSurfacesMode(t *testing.T) {
	if Prompt(false) != "nexus> " {
		t.Fatalf("default prompt: %q", Prompt(false))
	}
	p := Prompt(true)
	if !strings.Contains(p, "YOLO") {
		t.Fatalf("yolo mode not surfaced in the prompt: %q", p)
	}
}

// fake daemon: verifies hello, then answers one chat with deltas+final and
// the next with an error frame.
func fakeServer(t *testing.T, sock string) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		line, _ := r.ReadString('\n')
		var hello map[string]any
		json.Unmarshal([]byte(line), &hello)
		if hello["type"] != "hello" || hello["yolo"] != true {
			t.Error("client did not send its session mode in hello")
			return
		}
		r.ReadString('\n') // chat 1
		conn.Write([]byte(`{"type":"delta","text":"partial "}` + "\n"))
		conn.Write([]byte(`{"type":"delta","text":"answer"}` + "\n"))
		conn.Write([]byte(`{"type":"final","text":"partial answer"}` + "\n"))
		r.ReadString('\n') // chat 2
		conn.Write([]byte(`{"type":"error","text":"NEEDS_APPROVAL: tool x"}` + "\n"))
	}()
}

func TestRenderStreamingAndErrors(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	fakeServer(t, sock)
	in := strings.NewReader("hi\nsecond\n")
	var out strings.Builder
	if err := Run(in, &out, sock, true); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	// Deltas print BEFORE the final line lands (streaming print), the
	// final is rendered once, the error frame is rendered as an error.
	if !strings.Contains(got, "partial answer") {
		t.Fatalf("streamed reply not rendered: %q", got)
	}
	if strings.Count(got, "partial answer") != 1 {
		t.Fatalf("final duplicated over deltas: %q", got)
	}
	if !strings.Contains(got, "NEEDS_APPROVAL") {
		t.Fatalf("error frame not rendered: %q", got)
	}
	if !strings.Contains(got, "nexus[YOLO]> ") {
		t.Fatalf("prompt with mode not rendered: %q", got)
	}
}

//go:build linux

// Composition-root test (Phase-2-r2 codex #13): the PRODUCTION wiring —
// buildDaemon exactly as `nexus daemon` runs it (journal + known-ref
// redactor, S7 authority, governed provider, streaming planner factory,
// fail-closed journal audit) — serves a real REPL conversation over a UDS
// against a deterministic local transport.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/app/repl"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/pathx"
)

func TestCompositionRootServesConversation(t *testing.T) {
	// Serves BOTH wire modes: the tool-enabled planner uses buffered
	// Chat; a stream:true request gets SSE.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream bool `json:"stream"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"root \"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"reply\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"root reply"}}]}`)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("NEXUS_ROOT_KEY", "sk-root")
	base := filepath.Join(t.TempDir(), "nexus")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgJSON := fmt.Sprintf(`{"provider_base_url":%q,"provider_key_env":"NEXUS_ROOT_KEY",
		"provider_model":"root-model","egress_allow":[%q],"default_profile":"private"}`, srv.URL, host)
	if err := os.WriteFile(filepath.Join(base, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := pathx.Layout{Base: base}
	resolved, err := config.Resolve(filepath.Join(base, "config.json"), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, j, err := buildDaemon(layout, resolved)
	if err != nil {
		t.Fatalf("production composition root failed: %v", err)
	}
	t.Cleanup(func() { j.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sock := socketPath(layout)
	go d.Serve(ctx, sock)
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var out strings.Builder
	if err := repl.Run(strings.NewReader("hello\n"), &out, sock, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "root reply") {
		t.Fatalf("composition-root conversation broken: %q", out.String())
	}
}

// The Phase-3 e2e memory literal (codex #2): REPL → provider TOOL CALL →
// PEP (yolo session: ALLOWED_BY_YOLO journaled) → memory projection in
// the ONE profile journal → daemon RESTART → recall through the same
// production spine. The fake provider is deterministic and scripted.
func TestMemoryToolSpineSurvivesRestart(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		reply := ""
		switch step {
		case 1: // remember request → tool call
			reply = `{"action":"tool","tool_id":"memory_remember","arguments":{"content":"the SPINEFACT is alive"}}`
		case 2: // tool observation → final
			reply = "saved it"
		case 3: // recall request → tool call
			reply = `{"action":"tool","tool_id":"memory_recall","arguments":{"query":"SPINEFACT"}}`
		default: // recall observation → final (echo the observation back)
			var req struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			reply = "recalled: " + req.Messages[len(req.Messages)-1].Content
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": reply}}}})
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("NEXUS_SPINE2_KEY", "sk-spine2")
	base := filepath.Join(t.TempDir(), "nexus")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgJSON := fmt.Sprintf(`{"provider_base_url":%q,"provider_key_env":"NEXUS_SPINE2_KEY",
		"provider_model":"m","egress_allow":[%q],"default_profile":"private"}`, srv.URL, host)
	if err := os.WriteFile(filepath.Join(base, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := pathx.Layout{Base: base}
	resolved, err := config.Resolve(filepath.Join(base, "config.json"), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	runSession := func(input string) string {
		d, j, err := buildDaemon(layout, resolved)
		if err != nil {
			t.Fatalf("composition root: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		sock := socketPath(layout)
		serveDone := make(chan error, 1)
		go func() { serveDone <- d.Serve(ctx, sock) }()
		for i := 0; i < 100; i++ {
			if c, err := net.Dial("unix", sock); err == nil {
				c.Close()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		var out strings.Builder
		// yolo session: the ASK on memory_remember is journaled
		// ALLOWED_BY_YOLO instead of blocking on the T24 HITL owner.
		rerr := repl.Run(strings.NewReader(input), &out, sock, true)
		// Orderly incarnation teardown BEFORE the next one reuses the
		// socket (Phase-3-r2 codex #16: the flaky gate connected to a
		// dying daemon).
		cancel()
		<-serveDone
		j.Close()
		if rerr != nil {
			t.Fatal(rerr)
		}
		return out.String()
	}
	// Session 1: remember through the tool spine.
	out1 := runSession("remember the SPINEFACT\n")
	if !strings.Contains(out1, "saved it") {
		t.Fatalf("remember turn broken: %q", out1)
	}
	// DAEMON RESTART (fresh buildDaemon over the same layout): the fact
	// must come back through the journal-projected store.
	out2 := runSession("what is the SPINEFACT?\n")
	if !strings.Contains(out2, "the SPINEFACT is alive") {
		t.Fatalf("fact did not survive the restart through the production spine: %q", out2)
	}
}

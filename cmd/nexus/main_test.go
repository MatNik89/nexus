//go:build linux

// Composition-root test (Phase-2-r2 codex #13): the PRODUCTION wiring —
// buildDaemon exactly as `nexus daemon` runs it (journal + known-ref
// redactor, S7 authority, governed provider, streaming planner factory,
// fail-closed journal audit) — serves a real REPL conversation over a UDS
// against a deterministic local transport.
package main

import (
	"context"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"root \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"reply\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
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

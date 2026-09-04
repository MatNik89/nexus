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
	"github.com/MatNik89/nexus/internal/obligation"
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
	b, err := buildDaemon(layout, resolved)
	if err != nil {
		t.Fatalf("production composition root failed: %v", err)
	}
	t.Cleanup(func() { b.j.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sock := socketPath(layout)
	go b.d.Serve(ctx, sock)
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
		b, err := buildDaemon(layout, resolved)
		if err != nil {
			t.Fatalf("composition root: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		sock := socketPath(layout)
		serveDone := make(chan error, 1)
		go func() { serveDone <- b.d.Serve(ctx, sock) }()
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
		b.j.Close()
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

// The Phase-4 production-spine literal: reminder_set through the REAL
// spine (REPL → planner tool call → PEP → obligation+schedule batch) →
// DAEMON RESTART → the due reminder fires with its obligation moved to
// DELIVERY_PENDING in the SAME batch — then delivered + acked, with the
// ack correlated to the exact occurrence.
func TestReminderSpineAcrossRestart(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		reply := ""
		switch step {
		case 1: // set a reminder DUE IN THE PAST (fires on the next sweep)
			reply = `{"action":"tool","tool_id":"reminder_set","arguments":{"id":"rem-spine","body":"spine reminder","year":2026,"month":1,"day":2,"hour":9,"minute":0,"tz":"Europe/Zagreb"}}`
		case 2:
			reply = "reminder placed"
		case 3: // after restart: acknowledge the delivered occurrence
			reply = `{"action":"tool","tool_id":"reminder_ack","arguments":{"occurrence_id":"occ-rem-spine#1"}}`
		default:
			reply = "acknowledged"
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": reply}}}})
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("NEXUS_SPINE3_KEY", "sk-spine3")
	base := filepath.Join(t.TempDir(), "nexus")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgJSON := fmt.Sprintf(`{"provider_base_url":%q,"provider_key_env":"NEXUS_SPINE3_KEY",
		"provider_model":"m","egress_allow":[%q],"default_profile":"private"}`, srv.URL, host)
	if err := os.WriteFile(filepath.Join(base, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := pathx.Layout{Base: base}
	resolved, err := config.Resolve(filepath.Join(base, "config.json"), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := func(input string, sweep bool) (string, *daemonBundle) {
		b, err := buildDaemon(layout, resolved)
		if err != nil {
			t.Fatalf("composition root: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		sock := socketPath(layout)
		serveDone := make(chan error, 1)
		go func() { serveDone <- b.d.Serve(ctx, sock) }()
		for i := 0; i < 100; i++ {
			if c, err := net.Dial("unix", sock); err == nil {
				c.Close()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if sweep {
			// The PRODUCTION scheduler goroutine (startup sweep + tick),
			// exactly as runDaemon runs it — the decorated batch fires
			// the overdue occurrence.
			sctx, scancel := context.WithCancel(context.Background())
			go b.sched.Run(sctx, 50*time.Millisecond, nil)
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if st, err := b.obl.Status(context.Background(), "rem-spine"); err == nil && st == obligation.StateDeliveryPending {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			scancel()
			if herr := b.sched.Health(); herr != nil {
				t.Fatalf("scheduler health after production run: %v", herr)
			}
		}
		var out strings.Builder
		if input != "" {
			if err := repl.Run(strings.NewReader(input), &out, sock, true); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		<-serveDone
		return out.String(), b
	}
	// Session 1: place the reminder through the spine, then shut down.
	out1, b1 := session("remind me\n", false)
	if !strings.Contains(out1, "reminder placed") {
		t.Fatalf("reminder_set turn broken: %q", out1)
	}
	if st, err := b1.obl.Status(context.Background(), "rem-spine"); err != nil || st != obligation.StateScheduled {
		t.Fatalf("obligation not SCHEDULED after set: %v %v", st, err)
	}
	b1.j.Close()
	// Session 2 (RESTART): the sweep fires the missed occurrence and the
	// obligation lands DELIVERY_PENDING in the SAME batch; deliver + ack
	// through the spine.
	_, b2 := session("", true)
	st, err := b2.obl.Status(context.Background(), "rem-spine")
	if err != nil || st != obligation.StateDeliveryPending {
		b2.j.Close()
		t.Fatalf("fired obligation not DELIVERY_PENDING after restart: %v %v", st, err)
	}
	if err := b2.obl.MarkDelivered(context.Background(), "occ-rem-spine#1", obligation.DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-spine"}); err != nil {
		b2.j.Close()
		t.Fatal(err)
	}
	b2.j.Close()
	out3, b3 := session("ack it\n", false)
	defer b3.j.Close()
	if !strings.Contains(out3, "acknowledged") {
		t.Fatalf("ack turn broken: %q", out3)
	}
	if st, err := b3.obl.Status(context.Background(), "rem-spine"); err != nil || st != obligation.StateAcked {
		t.Fatalf("final state %v (%v), want ACKED", st, err)
	}
}

//go:build linux

// S6.1 per-argument PEP gating: composition-root proof that
// mergedArgGates' output is ACTUALLY wired into the live PEP the
// channel path serves through, not just correct in isolation (codex
// round-1 code-review: a disposable ablation that removed ArgGates
// wiring from all four production injection points — mergedArgGates
// itself plus the system, interactive-session, and channel PEP
// constructors — left every existing test green; nothing proved the
// wiring was actually connected). TestExecArgGateDeniesNonAllowlisted
// ThroughRealCompositionRoot is the missing real-boundary detector:
// it drives a model-requested, NON-allowlisted exec command through
// buildDaemon's REAL telegram handler and asserts POLICY_DENIED
// surfaces with NO approval ceremony — round-trip through the exact
// code path a live deployment uses, not a hand-built test PEP.
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
	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/exectool"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/sandbox"
)

func TestMergedArgGatesAlwaysRegistersExecEvenWithoutSandbox(t *testing.T) {
	gates := mergedArgGates(nil)
	gate, ok := gates[exectool.ToolID]
	if !ok {
		t.Fatal("exec's gate is missing entirely when the sandbox is unavailable")
	}
	if err := gate(json.RawMessage(`{"command":"/bin/ls"}`)); err == nil {
		t.Fatal("nil-adapter gate must deny everything, not pass through")
	}
}

// The real-boundary proof: a non-allowlisted command, requested through
// the ACTUAL production composition (buildDaemon + the channel handler,
// exactly as a live Telegram message would arrive), is denied with NO
// approval ceremony — proving mergedArgGates' output genuinely reaches
// the channel PEP's Decide, not just a hand-constructed test PEP.
func TestExecArgGateDeniesNonAllowlistedThroughRealCompositionRoot(t *testing.T) {
	if _, err := sandbox.NewBwrap().Probe(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		// /bin/whoami is deliberately NOT in exec_allow below.
		reply := `{"action":"tool","tool_id":"exec","arguments":{"command":"/bin/whoami","args":[]}}`
		if step > 1 {
			var req struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			last := req.Messages[len(req.Messages)-1].Content
			reply = "observed: " + last[:min(200, len(last))]
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": reply}}}})
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("NEXUS_TG_ARGGATE_KEY", "sk-x")
	base := filepath.Join(t.TempDir(), "nexus")
	os.MkdirAll(base, 0o700)
	// exec_allow lists ONLY /bin/ls — /bin/whoami above is a foreign,
	// non-allowlisted command the ArgGate must deny before any approval
	// prompt is ever generated.
	cfgJSON := fmt.Sprintf(`{"provider_base_url":%q,"provider_key_env":"NEXUS_TG_ARGGATE_KEY",
		"provider_model":"m","egress_allow":[%q],"default_profile":"private","exec_allow":["/bin/ls"]}`, srv.URL, host)
	os.WriteFile(filepath.Join(base, "config.json"), []byte(cfgJSON), 0o600)
	resolved, err := config.Resolve(filepath.Join(base, "config.json"), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildDaemon(pathx.Layout{Base: base}, resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer b.j.Close()
	h := telegramHandler(b)
	reply, err := h(context.Background(), channel.Inbound{
		AdapterID: "telegram", ChannelIdentity: "chat-argagte", UpdateID: 1,
		Text: "who am I on the host", Profile: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply, "APPROVAL NEEDED") {
		t.Fatalf("non-allowlisted command reached the ASK gate — the ArgGate never fired through the real composition root: %q", reply)
	}
	if !strings.Contains(reply, "POLICY_DENIED") {
		t.Fatalf("non-allowlisted command was not denied by policy: %q", reply)
	}
}

// The composition-root test above proves the CHANNEL PEP (daemon.go,
// consuming Deps.ArgGates). The SYSTEM PEP (main.go:733) is a SEPARATE
// production injection point — its own independent `mergedArgGates
// (execAdapter)` call, not Deps.ArgGates — and it is the ONE that
// resumeApprovedPending's startup-replay path actually runs through
// (round-3 code review's original motivating scenario: "durable
// approved calls are replayed through the system PEP at startup
// recovery"). Proving the channel path alone leaves this real,
// distinct injection point unverified. This test closes that gap
// directly: a non-allowlisted exec call is durably approved (bypassing
// the ASK ceremony, simulating a stale approval or a config change
// after approval), then resumed at startup through the REAL sysPath —
// and must be denied, not executed.
func TestExecArgGateDeniesNonAllowlistedThroughSystemPEPStartupReplay(t *testing.T) {
	if _, err := sandbox.NewBwrap().Probe(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	base := filepath.Join(t.TempDir(), "nexus")
	os.MkdirAll(base, 0o700)
	// exec_allow lists ONLY /bin/ls — the durably-approved call below
	// requests a DIFFERENT, non-allowlisted command.
	cfgJSON := `{"provider_base_url":"http://127.0.0.1:1","provider_key_env":"NEXUS_TG_SYSPEP_KEY",
		"provider_model":"m","egress_allow":["127.0.0.1:1"],"default_profile":"private","exec_allow":["/bin/ls"]}`
	os.Setenv("NEXUS_TG_SYSPEP_KEY", "sk-x")
	t.Cleanup(func() { os.Unsetenv("NEXUS_TG_SYSPEP_KEY") })
	os.WriteFile(filepath.Join(base, "config.json"), []byte(cfgJSON), 0o600)
	resolved, err := config.Resolve(filepath.Join(base, "config.json"), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildDaemon(pathx.Layout{Base: base}, resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer b.j.Close()

	args, _ := json.Marshal(map[string]any{"command": "/bin/whoami", "args": []string{}})
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-syspep", ToolID: exectool.ToolID, Arguments: args,
		ArgsSchemaHash: "exec.v1", Effect: contracts.EffectIrreversible,
		ExecutionKind: contracts.ExecProcess, Deadline: time.Now().Add(time.Hour),
		AttemptNo: 1, IdempotencyKey: strPtr("ik-syspep"), ProfileID: "private",
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := b.approvals.Suspend(context.Background(), "turn-syspep", "run-syspep", call, "tg:chat-syspep", testBlocks(t, "who am I on the host"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.approvals.Approve(context.Background(), ch.ChallengeID, "tg:chat-syspep"); err != nil {
		t.Fatal(err)
	}
	err = b.resumeApprovedPending(context.Background())
	if err == nil {
		t.Fatal("expected resumeApprovedPending to report the system-PEP denial")
	}
	if !strings.Contains(err.Error(), "POLICY_DENIED") {
		t.Fatalf("system PEP did not deny the non-allowlisted resumed command: %v", err)
	}
}

func strPtr(s string) *string { return &s }

// The third and last production PEP construction site: the interactive
// local-CLI UDS session (daemon.go's handle(), NOT the channel or
// system/startup path). Codex's round-2 disposable ablation (replacing
// ONLY this site's d.deps.ArgGates with nil) left the entire suite
// green, proving this path had zero real-boundary coverage even after
// the channel and system paths were both proven. This test closes it:
// a real REPL session over the REAL UDS socket, driven through
// buildDaemon exactly as `nexus repl` connects, requesting a non-
// allowlisted exec command — must see POLICY_DENIED, never
// NEEDS_APPROVAL.
func TestExecArgGateDeniesNonAllowlistedThroughInteractiveSessionPEP(t *testing.T) {
	if _, err := sandbox.NewBwrap().Probe(context.Background()); err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		reply := `{"action":"tool","tool_id":"exec","arguments":{"command":"/bin/whoami","args":[]}}`
		if step > 1 {
			var req struct {
				Messages []struct{ Role, Content string } `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			last := req.Messages[len(req.Messages)-1].Content
			reply = "observed: " + last[:min(200, len(last))]
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"message": map[string]string{"role": "assistant", "content": reply}}}})
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("NEXUS_INTERACTIVE_ARGGATE_KEY", "sk-x")
	base := filepath.Join(t.TempDir(), "nexus")
	os.MkdirAll(base, 0o700)
	cfgJSON := fmt.Sprintf(`{"provider_base_url":%q,"provider_key_env":"NEXUS_INTERACTIVE_ARGGATE_KEY",
		"provider_model":"m","egress_allow":[%q],"default_profile":"private","exec_allow":["/bin/ls"]}`, srv.URL, host)
	os.WriteFile(filepath.Join(base, "config.json"), []byte(cfgJSON), 0o600)
	resolved, err := config.Resolve(filepath.Join(base, "config.json"), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	layout := pathx.Layout{Base: base}
	b, err := buildDaemon(layout, resolved)
	if err != nil {
		t.Fatal(err)
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
	if err := repl.Run(strings.NewReader("who am I on the host\n"), &out, sock, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "NEEDS_APPROVAL") {
		t.Fatalf("non-allowlisted command reached the ASK gate through the interactive session — the ArgGate never fired: %q", out.String())
	}
	if !strings.Contains(out.String(), "POLICY_DENIED") {
		t.Fatalf("non-allowlisted command was not denied by policy through the interactive session: %q", out.String())
	}
}

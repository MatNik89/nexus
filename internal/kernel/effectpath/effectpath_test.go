//go:build linux

// T14 RED table (tasks-P0): PEP default-deny total switch (ASK != ALLOW),
// S6.9 order-only middleware, EffectPath concrete-executor branching,
// grant-before-execute, yolo policy mode (HARDQ F2). Anchored to
// DESIGN-FIXES-r2 K1/K2 + SPEC P0.2 + ledger RED names.
package effectpath

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
)

// --- contract fakes ---

type fakeSandbox struct{ launches int }

func (f *fakeSandbox) Launch(ctx context.Context, call contracts.ToolCall) (contracts.ToolResult, error) {
	f.launches++
	return okResult(call), nil
}

type fakeAudit struct{ events []string }

func (f *fakeAudit) Record(event string, call contracts.ToolCall) {
	f.events = append(f.events, event+":"+string(call.ToolID))
}

type recordingMW struct {
	before, after, onerr int
	vetoAfter            error
	beforeErr            error
}

func (m *recordingMW) BeforeTool(ctx context.Context, c contracts.ToolCall) error {
	m.before++
	return m.beforeErr
}
func (m *recordingMW) AfterTool(ctx context.Context, c contracts.ToolCall, r contracts.ToolResult) error {
	m.after++
	return m.vetoAfter
}
func (m *recordingMW) OnError(ctx context.Context, e error) error {
	m.onerr++
	return e
}

func idem(s string) *string { return &s }

func call(t *testing.T, tool string, effect contracts.EffectClass, kind contracts.ExecutionKind) contracts.ToolCall {
	t.Helper()
	p := contracts.ToolCallParams{
		ToolCallID: contracts.ToolCallID("tc-" + tool), ToolID: contracts.ToolID(tool),
		Arguments: json.RawMessage(`{"path":"/tmp/x"}`), ArgsSchemaHash: "h1",
		Effect: effect, ExecutionKind: kind,
		Deadline: time.Unix(2000, 0), AttemptNo: 1, ProfileID: "work",
	}
	if effect != contracts.EffectReadOnly {
		p.IdempotencyKey = idem("ik-" + tool)
	}
	c, err := contracts.NewToolCall(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func okResult(c contracts.ToolCall) contracts.ToolResult {
	return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
		Status: contracts.ResultSucceeded, StartedAt: time.Unix(1, 0), FinishedAt: time.Unix(2, 0)}
}

type harness struct {
	pep     *PEP
	path    *EffectPath
	sandbox *fakeSandbox
	audit   *fakeAudit
	mw      *recordingMW
	grants  *s7min.Authority
	inprocN *int
}

func build(t *testing.T, mode PolicyMode, rules map[contracts.ToolID]Decision) *harness {
	t.Helper()
	audit := &fakeAudit{}
	approvals := NewApprovals(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	pep, err := NewPEP(rules, approvals, audit, mode)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	inproc := NewInProcessExecutor(map[contracts.ToolID]InProcFunc{
		"read": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			n++
			return okResult(c), nil
		},
		"boom": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			return contracts.ToolResult{}, fmt.Errorf("tool exploded")
		},
	})
	sb := &fakeSandbox{}
	mw := &recordingMW{}
	grants := s7min.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	path, err := NewEffectPath(pep, mw, inproc, NewSandboxedProcessExecutor(sb), grants)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{pep: pep, path: path, sandbox: sb, audit: audit, mw: mw, grants: grants, inprocN: &n}
}

func grantFor(t *testing.T, h *harness, op string) s7min.Grant {
	t.Helper()
	g, err := h.grants.Issue(contracts.OperationID(op), "local")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestUnknownDecisionDenies: an unknown tool and a rule carrying the zero
// Decision BOTH deny — total switch, default-deny (S6.0).
func TestUnknownDecisionDenies(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionInvalid})
	for _, tool := range []string{"read", "never-registered"} {
		c := call(t, tool, contracts.EffectReadOnly, contracts.ExecInProcess)
		_, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-"+tool))
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("%s: want ErrDenied, got %v", tool, err)
		}
	}
	if *h.inprocN != 0 || h.sandbox.launches != 0 {
		t.Fatal("denied call reached an executor")
	}
}

// TestUnknownExecKindRejectsNotInproc: zero/unknown ExecutionKind is
// rejected — NEVER falls through to in-process.
func TestUnknownExecKindRejectsNotInproc(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	c.ExecutionKind = contracts.ExecutionKind(0) // sealed field tampered after construction
	_, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1"))
	if !errors.Is(err, ErrUnknownExecKind) {
		t.Fatalf("want ErrUnknownExecKind, got %v", err)
	}
	c.ExecutionKind = contracts.ExecutionKind(99)
	_, err = h.path.RunTool(context.Background(), c, grantFor(t, h, "op-2"))
	if !errors.Is(err, ErrUnknownExecKind) {
		t.Fatalf("want ErrUnknownExecKind, got %v", err)
	}
	if *h.inprocN != 0 || h.sandbox.launches != 0 {
		t.Fatal("unknown-kind call reached an executor")
	}
	if h.mw.onerr == 0 {
		t.Fatal("rejection bypassed OnError (audit/lifecycle branch)")
	}
}

// TestAskRequiresExactApproval: ASK without approval stops; an EXACT
// approval admits exactly once; a changed payload invalidates it (C4).
func TestAskRequiresExactApproval(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"write": DecisionAsk})
	c := call(t, "write", contracts.EffectReversible, contracts.ExecInProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1")); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("unapproved ASK must stop: %v", err)
	}
	// Approve the EXACT call, then tamper the payload: approval is dead.
	h.pep.Approvals().Approve(c)
	tampered := c
	tampered.Arguments = json.RawMessage(`{"path":"/etc/shadow"}`)
	if _, err := h.path.RunTool(context.Background(), tampered, grantFor(t, h, "op-2")); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("changed payload must invalidate the approval: %v", err)
	}
	// The untampered call passes ONCE...
	h2 := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAsk})
	rc := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	h2.pep.Approvals().Approve(rc)
	if _, err := h2.path.RunTool(context.Background(), rc, grantFor(t, h2, "op-3")); err != nil {
		t.Fatalf("exactly-approved call refused: %v", err)
	}
	// ...and the approval is SINGLE-USE.
	if _, err := h2.path.RunTool(context.Background(), rc, grantFor(t, h2, "op-4")); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("approval reused: %v", err)
	}
}

// TestInProcessToolNeverSpawns + TestReadOnlyProcessStillSandboxed: the
// branch is by SEALED ExecutionKind — in-process never touches the sandbox
// executor, and a READ-ONLY process tool still goes through it.
func TestExecutorBranchBySealedKind(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow, "shell-cat": DecisionAllow})
	if _, err := h.path.RunTool(context.Background(),
		call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess), grantFor(t, h, "op-1")); err != nil {
		t.Fatal(err)
	}
	if *h.inprocN != 1 || h.sandbox.launches != 0 {
		t.Fatalf("in-process tool spawned: inproc=%d launches=%d", *h.inprocN, h.sandbox.launches)
	}
	if _, err := h.path.RunTool(context.Background(),
		call(t, "shell-cat", contracts.EffectReadOnly, contracts.ExecProcess), grantFor(t, h, "op-2")); err != nil {
		t.Fatal(err)
	}
	if h.sandbox.launches != 1 {
		t.Fatal("read-only PROCESS tool bypassed the sandbox executor")
	}
}

// TestNoAttemptWithoutGrant: no valid s7 grant, no execution — forged and
// reused grants alike (SPEC P0.2 grant-before-execute).
func TestNoAttemptWithoutGrant(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	forged := s7min.Grant{OperationID: "op-x", AttemptNo: 1, TargetID: "local", Nonce: "deadbeef"}
	if _, err := h.path.RunTool(context.Background(), c, forged); !errors.Is(err, s7min.ErrAttemptNotAuthorized) {
		t.Fatalf("forged grant executed: %v", err)
	}
	g := grantFor(t, h, "op-1")
	if _, err := h.path.RunTool(context.Background(), c, g); err != nil {
		t.Fatal(err)
	}
	if _, err := h.path.RunTool(context.Background(), c, g); !errors.Is(err, s7min.ErrAttemptNotAuthorized) {
		t.Fatalf("reused grant executed: %v", err)
	}
	if *h.inprocN != 1 {
		t.Fatalf("executor invocations %d, want exactly 1", *h.inprocN)
	}
}

// TestVetoThroughOnErrorReconciles: an AfterTool veto goes through OnError
// and an effectful result WITHOUT a commit receipt lands the attempt in
// UNKNOWN (reconcile, never blind retry); a read-only veto is terminal.
func TestVetoThroughOnErrorReconciles(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	h.mw.vetoAfter = fmt.Errorf("post-exec veto")
	// EFFECTFUL call routed through the inproc fake (registered as "read").
	c2 := call(t, "read", contracts.EffectReversible, contracts.ExecInProcess)
	g := grantFor(t, h, "op-1")
	_, err := h.path.RunTool(context.Background(), c2, g)
	if err == nil {
		t.Fatal("vetoed result returned without error")
	}
	if h.mw.onerr == 0 {
		t.Fatal("veto bypassed OnError")
	}
	if st, _ := h.grants.State("op-1"); st != contracts.AttemptUnknown {
		t.Fatalf("effectful veto without receipt must reconcile (UNKNOWN), got %v", st)
	}
	// Read-only veto: nothing durable could exist → FAILED_TERMINAL.
	h2 := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	h2.mw.vetoAfter = fmt.Errorf("veto")
	rc := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	g2 := grantFor(t, h2, "op-2")
	if _, err := h2.path.RunTool(context.Background(), rc, g2); err == nil {
		t.Fatal("vetoed result returned without error")
	}
	if st, _ := h2.grants.State("op-2"); st != contracts.AttemptFailed {
		t.Fatalf("read-only veto must be terminal, got %v", st)
	}
}

// TestExecErrorSkipsAfterToolAndOutput: an executor error never reaches
// AfterTool, returns an explicitly EMPTY result, and goes through OnError.
func TestExecErrorSkipsAfterToolAndOutput(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"boom": DecisionAllow})
	c := call(t, "boom", contracts.EffectReadOnly, contracts.ExecInProcess)
	out, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1"))
	if err == nil {
		t.Fatal("executor error swallowed")
	}
	if h.mw.after != 0 {
		t.Fatal("AfterTool ran on an executor error")
	}
	if h.mw.onerr == 0 {
		t.Fatal("executor error bypassed OnError")
	}
	if out.Status != contracts.ResultInvalid || len(out.Output) != 0 {
		t.Fatalf("executor error leaked output: %+v", out)
	}
	if st, _ := h.grants.State("op-1"); st != contracts.AttemptFailed {
		t.Fatalf("failed attempt state %v, want FAILED (terminal, no retry)", st)
	}
}

// TestYoloAllowsAskButNeverDeny (HARDQ F2): yolo maps ASK → ALLOW with an
// ALLOWED_BY_YOLO audit record; DENY is untouched by mode.
func TestYoloAllowsAskButNeverDeny(t *testing.T) {
	h := build(t, ModeYolo, map[contracts.ToolID]Decision{"read": DecisionAsk, "rmrf": DecisionDeny})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1")); err != nil {
		t.Fatalf("yolo ASK must execute without approval: %v", err)
	}
	if len(h.audit.events) != 1 || h.audit.events[0] != "ALLOWED_BY_YOLO:read" {
		t.Fatalf("yolo override not journaled: %v", h.audit.events)
	}
	d := call(t, "rmrf", contracts.EffectIrreversible, contracts.ExecProcess)
	if _, err := h.path.RunTool(context.Background(), d, grantFor(t, h, "op-2")); !errors.Is(err, ErrDenied) {
		t.Fatalf("yolo weakened DENY: %v", err)
	}
	if h.sandbox.launches != 0 {
		t.Fatal("denied call executed under yolo")
	}
}

// TestYoloCannotBeSetByChannelInput (HARDQ F2): PolicyMode is a session
// construct fixed at construction — no message/call payload reaches it.
func TestYoloCannotBeSetByChannelInput(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAsk})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	// Adversarial payload asking for yolo: just bytes in Arguments.
	c.Arguments = json.RawMessage(`{"policy_mode":"yolo","yolo":true}`)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1")); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("payload flipped the policy mode: %v", err)
	}
	// The wire ToolCall carries no mode field at all (structural half).
	b, _ := json.Marshal(c)
	var m map[string]any
	json.Unmarshal(b, &m)
	for k := range m {
		if k == "policy_mode" || k == "mode" || k == "yolo" {
			t.Fatalf("ToolCall wire carries a policy-mode field %q", k)
		}
	}
	// An invalid mode at construction is refused (fail closed).
	if _, err := NewPEP(nil, NewApprovals(nil, time.Minute), &fakeAudit{}, PolicyMode(0)); err == nil {
		t.Fatal("zero PolicyMode accepted")
	}
}

// classifyEffectPhase (N4-C03): valid receipt → its phase; invalid/foreign
// receipt → UNKNOWN; no receipt: read-only → BEFORE_COMMIT, effectful → UNKNOWN.
func TestClassifyEffectPhaseFailClosed(t *testing.T) {
	ro := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	eff := call(t, "write", contracts.EffectReversible, contracts.ExecInProcess)
	if got := ClassifyEffectPhase(ro, okResult(ro)); got != contracts.PhaseBeforeCommit {
		t.Fatalf("read-only no-receipt: %v", got)
	}
	if got := ClassifyEffectPhase(eff, okResult(eff)); got != contracts.PhaseUnknown {
		t.Fatalf("effectful no-receipt must be UNKNOWN: %v", got)
	}
	good := okResult(eff)
	good.Commit = &contracts.CommitReceipt{Phase: contracts.PhaseAfterCommit,
		ToolCallID: eff.ToolCallID, AttemptNo: eff.AttemptNo, ContentHash: "abc"}
	if got := ClassifyEffectPhase(eff, good); got != contracts.PhaseAfterCommit {
		t.Fatalf("valid receipt phase lost: %v", got)
	}
	foreign := okResult(eff)
	foreign.Commit = &contracts.CommitReceipt{Phase: contracts.PhaseAfterCommit,
		ToolCallID: "tc-other", AttemptNo: eff.AttemptNo, ContentHash: "abc"}
	if got := ClassifyEffectPhase(eff, foreign); got != contracts.PhaseUnknown {
		t.Fatalf("foreign receipt must be UNKNOWN: %v", got)
	}
}

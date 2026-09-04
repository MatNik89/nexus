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

type fakeAudit struct {
	events []string
	fail   bool
}

func (f *fakeAudit) Record(event string, call contracts.ToolCall) error {
	if f.fail {
		return fmt.Errorf("journal down")
	}
	f.events = append(f.events, event+":"+string(call.ToolID))
	return nil
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
		Deadline: time.Now().Add(time.Minute), AttemptNo: 1, ProfileID: "work",
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
	// Real clock: grant expiry participates in the S7 attempt context.
	grants := s7min.NewAuthority(nil, time.Minute)
	path, err := NewEffectPath(pep, mw, inproc, NewSandboxedProcessExecutor(sb), grants)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{pep: pep, path: path, sandbox: sb, audit: audit, mw: mw, grants: grants, inprocN: &n}
}

func grantFor(t *testing.T, h *harness, op string, c contracts.ToolCall) s7min.Grant {
	t.Helper()
	g, err := h.grants.Issue(contracts.OperationID(op), ToolTarget(c))
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
		_, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-"+tool, c))
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("%s: want ErrDenied, got %v", tool, err)
		}
	}
	if *h.inprocN != 0 || h.sandbox.launches != 0 {
		t.Fatal("denied call reached an executor")
	}
}

// TestUnknownExecKindRejectsNotInproc: zero/unknown ExecutionKind is
// rejected — NEVER falls through to in-process. Contract validation
// catches it first (fail closed); the executor switch default is
// defense-in-depth behind it.
func TestUnknownExecKindRejectsNotInproc(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	for _, kind := range []contracts.ExecutionKind{0, 99} {
		c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
		c.ExecutionKind = kind // sealed field tampered after construction
		if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, fmt.Sprintf("op-%d", kind), c)); err == nil {
			t.Fatalf("kind %d accepted", kind)
		}
	}
	if *h.inprocN != 0 || h.sandbox.launches != 0 {
		t.Fatal("unknown-kind call reached an executor")
	}
}

// TestAskRequiresExactApproval: ASK without approval stops; an EXACT
// approval admits exactly once; a changed payload invalidates it (C4).
func TestAskRequiresExactApproval(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"write": DecisionAsk})
	c := call(t, "write", contracts.EffectReversible, contracts.ExecInProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("unapproved ASK must stop: %v", err)
	}
	// Approve the EXACT call, then tamper the payload: approval is dead.
	h.pep.Approvals().Approve(c)
	tampered := c
	tampered.Arguments = json.RawMessage(`{"path":"/etc/shadow"}`)
	if _, err := h.path.RunTool(context.Background(), tampered, grantFor(t, h, "op-2", tampered)); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("changed payload must invalidate the approval: %v", err)
	}
	// The untampered call passes ONCE...
	h2 := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAsk})
	rc := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	h2.pep.Approvals().Approve(rc)
	if _, err := h2.path.RunTool(context.Background(), rc, grantFor(t, h2, "op-3", rc)); err != nil {
		t.Fatalf("exactly-approved call refused: %v", err)
	}
	// ...and the approval is SINGLE-USE.
	if _, err := h2.path.RunTool(context.Background(), rc, grantFor(t, h2, "op-4", rc)); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("approval reused: %v", err)
	}
}

// The branch is by SEALED ExecutionKind: in-process never touches the
// sandbox executor (ledger literal name).
func TestInProcessToolNeverSpawns(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err != nil {
		t.Fatal(err)
	}
	if *h.inprocN != 1 || h.sandbox.launches != 0 {
		t.Fatalf("in-process tool spawned: inproc=%d launches=%d", *h.inprocN, h.sandbox.launches)
	}
}

// A READ-ONLY process tool STILL goes through the sandbox executor
// (ledger literal name).
func TestReadOnlyProcessStillSandboxed(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"shell-cat": DecisionAllow})
	c := call(t, "shell-cat", contracts.EffectReadOnly, contracts.ExecProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-2", c)); err != nil {
		t.Fatal(err)
	}
	if h.sandbox.launches != 1 || *h.inprocN != 0 {
		t.Fatal("read-only PROCESS tool bypassed the sandbox executor")
	}
}

// TestNoAttemptWithoutGrant: no valid s7 grant, no execution — forged and
// reused grants alike (SPEC P0.2 grant-before-execute).
func TestNoAttemptWithoutGrant(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	forged := s7min.Grant{OperationID: "op-x", AttemptNo: 1, TargetID: ToolTarget(c), Nonce: "deadbeef"}
	if _, err := h.path.RunTool(context.Background(), c, forged); !errors.Is(err, s7min.ErrAttemptNotAuthorized) {
		t.Fatalf("forged grant executed: %v", err)
	}
	g := grantFor(t, h, "op-1", c)
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
	g := grantFor(t, h, "op-1", c2)
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
	g2 := grantFor(t, h2, "op-2", rc)
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
	out, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c))
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
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err != nil {
		t.Fatalf("yolo ASK must execute without approval: %v", err)
	}
	if len(h.audit.events) != 1 || h.audit.events[0] != "ALLOWED_BY_YOLO:read" {
		t.Fatalf("yolo override not journaled: %v", h.audit.events)
	}
	d := call(t, "rmrf", contracts.EffectIrreversible, contracts.ExecProcess)
	if _, err := h.path.RunTool(context.Background(), d, grantFor(t, h, "op-2", d)); !errors.Is(err, ErrDenied) {
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
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); !errors.Is(err, ErrNeedsApproval) {
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

// An EXPIRED call deadline never reaches policy or an executor.
func TestExpiredCallRefused(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	c.Deadline = time.Now().Add(-time.Second)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); !errors.Is(err, ErrCallExpired) {
		t.Fatalf("expired call executed: %v", err)
	}
	if *h.inprocN != 0 {
		t.Fatal("expired call reached an executor")
	}
}

// A grant minted for ANOTHER call never authorizes this one (target
// binding — Phase-2 codex #3 replay literal).
func TestForeignGrantCannotAuthorizeThisCall(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	a := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	b := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	b.ToolCallID = "tc-other"
	gForB := grantFor(t, h, "op-b", b)
	if _, err := h.path.RunTool(context.Background(), a, gForB); !errors.Is(err, s7min.ErrAttemptNotAuthorized) {
		t.Fatalf("grant for another call authorized this one: %v", err)
	}
	if *h.inprocN != 0 {
		t.Fatal("replayed grant reached an executor")
	}
}

// Order-only middleware can NEVER erase a refusal: an OnError that
// returns nil still leaves the causal error standing (Phase-2 codex #11).
type nilSwallowMW struct{ recordingMW }

func (m *nilSwallowMW) OnError(ctx context.Context, e error) error {
	m.onerr++
	return nil // hostile order hook trying to decide outcome
}

func TestOnErrorCannotEraseRefusal(t *testing.T) {
	audit := &fakeAudit{}
	approvals := NewApprovals(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	pep, err := NewPEP(map[contracts.ToolID]Decision{"boom": DecisionAllow}, approvals, audit, ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	inproc := NewInProcessExecutor(map[contracts.ToolID]InProcFunc{
		"boom": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			return contracts.ToolResult{}, fmt.Errorf("tool exploded")
		},
	})
	grants := s7min.NewAuthority(nil, time.Minute)
	mw := &nilSwallowMW{}
	path, err := NewEffectPath(pep, mw, inproc, NewSandboxedProcessExecutor(&fakeSandbox{}), grants)
	if err != nil {
		t.Fatal(err)
	}
	c := call(t, "boom", contracts.EffectReadOnly, contracts.ExecInProcess)
	g, _ := grants.Issue("op-1", ToolTarget(c))
	if _, err := path.RunTool(context.Background(), c, g); err == nil {
		t.Fatal("a nil-returning OnError erased an executor failure into success")
	}
}

// YOLO exists ONLY with its durable record: an audit append failure
// refuses the confirmation bypass (Phase-2 codex #9 / kilo #1).
func TestYoloRefusedWhenAuditNotDurable(t *testing.T) {
	h := build(t, ModeYolo, map[contracts.ToolID]Decision{"read": DecisionAsk})
	h.audit.fail = true
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err == nil {
		t.Fatal("yolo executed without a durable ALLOWED_BY_YOLO record")
	}
	if *h.inprocN != 0 {
		t.Fatal("unaudited yolo bypass reached an executor")
	}
}

// Result correlation is enforced: a result for another call/attempt or an
// invalid status is an executor defect, classified conservatively — an
// EFFECTFUL call parks UNKNOWN (Phase-2 codex #5), and an effectful
// "success" WITHOUT a receipt is a claim, not proof.
func TestResultValidationAndEffectfulReceiptRule(t *testing.T) {
	mk := func(fn InProcFunc) (*EffectPath, *s7min.Authority) {
		audit := &fakeAudit{}
		pep, _ := NewPEP(map[contracts.ToolID]Decision{"w": DecisionAllow},
			NewApprovals(func() time.Time { return time.Unix(1000, 0) }, time.Minute), audit, ModeDefault)
		grants := s7min.NewAuthority(nil, time.Minute)
		path, _ := NewEffectPath(pep, &recordingMW{}, NewInProcessExecutor(map[contracts.ToolID]InProcFunc{"w": fn}),
			NewSandboxedProcessExecutor(&fakeSandbox{}), grants)
		return path, grants
	}
	// (a) mis-correlated result, effectful call → UNKNOWN.
	path, grants := mk(func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
		r := okResult(c)
		r.ToolCallID = "tc-other"
		return r, nil
	})
	c := call(t, "w", contracts.EffectReversible, contracts.ExecInProcess)
	g, _ := grants.Issue("op-a", ToolTarget(c))
	if _, err := path.RunTool(context.Background(), c, g); !errors.Is(err, ErrEffectUnknown) {
		t.Fatalf("mis-correlated effectful result not parked UNKNOWN: %v", err)
	}
	if st, _ := grants.State("op-a"); st != contracts.AttemptUnknown {
		t.Fatalf("state %v, want UNKNOWN", st)
	}
	// (b) effectful SUCCESS without a receipt → UNKNOWN (claim, not proof).
	path, grants = mk(func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
		return okResult(c), nil // no commit receipt
	})
	c2 := call(t, "w", contracts.EffectReversible, contracts.ExecInProcess)
	g2, _ := grants.Issue("op-b", ToolTarget(c2))
	if _, err := path.RunTool(context.Background(), c2, g2); !errors.Is(err, ErrEffectUnknown) {
		t.Fatalf("receipt-less effectful success accepted: %v", err)
	}
	// (c) with a valid receipt the same call SUCCEEDS.
	path, grants = mk(func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
		r := okResult(c)
		r.Commit = &contracts.CommitReceipt{Phase: contracts.PhaseAfterCommit,
			ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo, ContentHash: "abc"}
		return r, nil
	})
	c3 := call(t, "w", contracts.EffectReversible, contracts.ExecInProcess)
	g3, _ := grants.Issue("op-c", ToolTarget(c3))
	if _, err := path.RunTool(context.Background(), c3, g3); err != nil {
		t.Fatalf("receipted effectful success refused: %v", err)
	}
	if st, _ := grants.State("op-c"); st != contracts.AttemptSucceeded {
		t.Fatalf("state %v, want SUCCEEDED", st)
	}
	// (d) an EFFECTFUL executor ERROR parks UNKNOWN, never terminal
	// (codex #4: an irreversible tool may have committed before dying).
	path, grants = mk(func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
		return contracts.ToolResult{}, fmt.Errorf("connection lost mid-commit")
	})
	c4 := call(t, "w", contracts.EffectIrreversible, contracts.ExecInProcess)
	g4, _ := grants.Issue("op-d", ToolTarget(c4))
	if _, err := path.RunTool(context.Background(), c4, g4); !errors.Is(err, ErrEffectUnknown) {
		t.Fatalf("effectful executor error not parked UNKNOWN: %v", err)
	}
	// (e) a FAILED result status is never a nil-error success.
	path, grants = mk(func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
		r := okResult(c)
		r.Status = contracts.ResultFailed
		return r, nil
	})
	c5 := call(t, "w", contracts.EffectReadOnly, contracts.ExecInProcess)
	g5, _ := grants.Issue("op-e", ToolTarget(c5))
	if _, err := path.RunTool(context.Background(), c5, g5); err == nil {
		t.Fatal("ResultFailed returned as success")
	}
	if st, _ := grants.State("op-e"); st != contracts.AttemptFailed {
		t.Fatalf("state %v, want FAILED", st)
	}
}

// The grant binds to the EXACT call digest: same ids with modified
// arguments (or any other field) never authorize (Phase-2-r2 codex #4).
func TestModifiedCallCannotRideOldGrant(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	g := grantFor(t, h, "op-1", c)
	mutated := c
	mutated.Arguments = json.RawMessage(`{"path":"/etc/shadow"}`) // same ids, new payload
	if _, err := h.path.RunTool(context.Background(), mutated, g); !errors.Is(err, s7min.ErrAttemptNotAuthorized) {
		t.Fatalf("modified call rode the old grant: %v", err)
	}
	if *h.inprocN != 0 {
		t.Fatal("modified call reached an executor")
	}
	// The unmodified call still authorizes.
	if _, err := h.path.RunTool(context.Background(), c, g); err != nil {
		t.Fatalf("exact call refused its own grant: %v", err)
	}
}

// The call's deadline is PROPAGATED into execution: the executor context
// expires at call.Deadline, and a read-only call killed by it lands
// CANCELLED (nothing durable can exist).
func TestCallDeadlinePropagatedIntoExecution(t *testing.T) {
	audit := &fakeAudit{}
	pep, _ := NewPEP(map[contracts.ToolID]Decision{"slow": DecisionAllow},
		NewApprovals(func() time.Time { return time.Unix(1000, 0) }, time.Minute), audit, ModeDefault)
	grants := s7min.NewAuthority(nil, time.Minute)
	var sawDeadline time.Time
	inproc := NewInProcessExecutor(map[contracts.ToolID]InProcFunc{
		"slow": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			dl, ok := ctx.Deadline()
			if !ok {
				return contracts.ToolResult{}, fmt.Errorf("no deadline propagated")
			}
			sawDeadline = dl
			<-ctx.Done() // wait for the deadline to kill us
			return contracts.ToolResult{}, ctx.Err()
		},
	})
	path, _ := NewEffectPath(pep, &recordingMW{}, inproc, NewSandboxedProcessExecutor(&fakeSandbox{}), grants)
	c := call(t, "slow", contracts.EffectReadOnly, contracts.ExecInProcess)
	c.Deadline = time.Now().Add(50 * time.Millisecond)
	g, _ := grants.Issue("op-1", ToolTarget(c))
	if _, err := path.RunTool(context.Background(), c, g); err == nil {
		t.Fatal("deadline-killed call returned success")
	}
	if !sawDeadline.Equal(c.Deadline) {
		t.Fatalf("executor deadline %v != call deadline %v", sawDeadline, c.Deadline)
	}
	if st, _ := grants.State("op-1"); st != contracts.AttemptCancelled {
		t.Fatalf("deadline-killed read-only attempt in state %v (want CANCELLED)", st)
	}
}

// A failed pre-dispatch STARTED append CANCELS the attempt (legal from
// AUTHORIZED in the durable stream) — never LOST/UNKNOWN, which would
// replay illegally (Phase-2-r3 codex #7 literal).
func TestStartObserverFailureCancelsNeverLoses(t *testing.T) {
	h := build(t, ModeDefault, map[contracts.ToolID]Decision{"read": DecisionAllow})
	h.path.SetStartObserver(func(ctx context.Context, op contracts.OperationID) error {
		return fmt.Errorf("journal write failed")
	})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err == nil {
		t.Fatal("undurable start dispatched anyway")
	}
	if *h.inprocN != 0 {
		t.Fatal("executor ran without a durable STARTED record")
	}
	if st, _ := h.grants.State("op-1"); st != contracts.AttemptCancelled {
		t.Fatalf("state %v — must be CANCELLED (LOST would replay illegally from AUTHORIZED)", st)
	}
}

// The execution context is S7-OWNED: the authority supplies the earlier
// of call deadline and grant expiry, and refuses a non-RUNNING operation.
func TestAttemptContextIsS7Owned(t *testing.T) {
	auth := s7min.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	if _, _, err := auth.AttemptContext(context.Background(), "op-never", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("attempt context granted for an unknown operation")
	}
	g, _ := auth.Issue("op-1", "t")
	if _, _, err := auth.AttemptContext(context.Background(), "op-1", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("attempt context granted before consume (not RUNNING)")
	}
	if err := auth.Consume(g); err != nil {
		t.Fatal(err)
	}
	// Grant expiry (issue+1m from the pinned clock) caps a longer call
	// deadline.
	dctx, cancel, err := auth.AttemptContext(context.Background(), "op-1", time.Unix(999999, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	dl, ok := dctx.Deadline()
	if !ok || !dl.Equal(g.ExpiresAt) {
		t.Fatalf("S7 did not cap the deadline at grant expiry: %v (want %v)", dl, g.ExpiresAt)
	}
}

// DUPLICATE-KEY injectivity (Phase-6 kilo #1): a duplicate-key document
// must NEVER hash equal to its last-wins collapse — the approver-visible
// summary and the executed value could otherwise diverge.
func TestEffectHashDuplicateKeysDoNotCollapse(t *testing.T) {
	mk := func(args string) contracts.ToolCall {
		idem := "ik-dup"
		c, err := contracts.NewToolCall(contracts.ToolCallParams{
			ToolCallID: "tc-dup", ToolID: "exec", Arguments: json.RawMessage(args),
			ArgsSchemaHash: "exec.v1", Effect: contracts.EffectIrreversible,
			ExecutionKind: contracts.ExecProcess, Deadline: time.Now().Add(time.Hour),
			AttemptNo: 1, IdempotencyKey: &idem, ProfileID: "work",
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	// PRIMARY guard (Phase-6 codex #6): construction REJECTS ambiguous
	// duplicate member names outright.
	idemD := "ik-dup"
	if _, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-dup", ToolID: "exec",
		Arguments:      json.RawMessage(`{"command":"/bin/echo","command":"/bin/rm","args":["/"]}`),
		ArgsSchemaHash: "exec.v1", Effect: contracts.EffectIrreversible,
		ExecutionKind: contracts.ExecProcess, Deadline: time.Now().Add(time.Hour),
		AttemptNo: 1, IdempotencyKey: &idemD, ProfileID: "work",
	}); err == nil {
		t.Fatal("duplicate-key arguments constructed a valid ToolCall")
	}
	// DEFENSE IN DEPTH: even a call that bypassed construction (struct
	// literal) must not hash equal to its last-wins collapse.
	collapsed := mk(`{"command":"/bin/rm","args":["/"]}`)
	dup := collapsed
	dup.Arguments = json.RawMessage(`{"command":"/bin/echo","command":"/bin/rm","args":["/"]}`)
	if EffectHash(dup) == EffectHash(collapsed) {
		t.Fatal("duplicate-key document collapsed to its last-wins form (C4 weakening)")
	}
	// Plain reorder of a CLEAN document still unifies (the reorder fix).
	reordered := mk(`{"args":["/"],"command":"/bin/rm"}`)
	if EffectHash(reordered) != EffectHash(collapsed) {
		t.Fatal("clean key reorder no longer unifies")
	}
	if !HasDuplicateJSONKeys([]byte(`{"a":1,"b":{"x":1,"x":2}}`)) {
		t.Fatal("nested duplicate missed")
	}
	if HasDuplicateJSONKeys([]byte(`{"a":1,"b":{"x":1},"c":[{"x":1},{"x":2}]}`)) {
		t.Fatal("false positive on sibling objects")
	}
}

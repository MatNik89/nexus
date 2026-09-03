//go:build linux

// Package effectpath owns the S6.0 PEP (default-deny total switch,
// ASK != ALLOW), the S6.9 order-only middleware seam, and the EffectPath
// (DESIGN-FIXES-r2 K1/K2): concrete executor types branched by the SEALED
// ExecutionKind, s7 grant consumed BEFORE any execution, veto and error
// paths through OnError, effectful-without-receipt → UNKNOWN (reconcile,
// never blind retry). PolicyMode yolo (HARDQ F2) maps ASK → ALLOW with an
// ALLOWED_BY_YOLO audit record; DENY is untouched; the mode is a session
// construct fixed at construction — no channel payload can reach it.
package effectpath

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
)

// Decision is the closed S6.0 policy outcome; zero = INVALID (fail closed).
type Decision uint8

const (
	DecisionInvalid Decision = iota
	DecisionAllow
	DecisionAsk
	DecisionDeny
)

// PolicyMode is the SESSION approval mode (HARDQ F2). It exists only at
// PEP construction (local CLI wires it in T17); nothing on the wire or in
// a tool payload can set it.
type PolicyMode uint8

const (
	ModeInvalid PolicyMode = iota
	ModeDefault
	ModeYolo
)

var (
	ErrDenied          = errors.New("POLICY_DENIED")
	ErrNeedsApproval   = errors.New("NEEDS_APPROVAL")
	ErrUnknownExecKind = errors.New("UNKNOWN_EXECUTION_KIND")
	ErrCallExpired     = errors.New("CALL_DEADLINE_EXCEEDED")
	// ErrEffectUnknown: an EFFECTFUL call finished without a valid commit
	// receipt — the attempt is parked UNKNOWN and ONLY reconciliation may
	// resolve it (E9). Callers must stop, never blind-retry or continue.
	ErrEffectUnknown = errors.New("EFFECT_UNKNOWN_RECONCILE")
)

// ToolTarget derives the S7 target a grant must be minted FOR to
// authorize this exact call — a grant minted for another call can never
// be replayed here (Phase-2 codex #3).
func ToolTarget(c contracts.ToolCall) contracts.TargetID {
	return contracts.TargetID("tool:" + string(c.ToolID) + "/" + string(c.ToolCallID))
}

// AuditSink receives policy-relevant records (the journal owner wires the
// real sink; tests pass capturing fakes). Record returning an error means
// the record is NOT durable — a yolo confirmation bypass without its
// ALLOWED_BY_YOLO record is refused (Phase-2 codex #9 / kilo #1).
type AuditSink interface {
	Record(event string, call contracts.ToolCall) error
}

// Middleware is the S6.9 seam — ORDER only, never policy (S6.0 owns
// ALLOW/ASK/DENY). One signature per hook (DESIGN-FIXES-r2 R5-C01).
type Middleware interface {
	BeforeTool(ctx context.Context, c contracts.ToolCall) error
	AfterTool(ctx context.Context, c contracts.ToolCall, r contracts.ToolResult) error // veto = non-nil
	OnError(ctx context.Context, e error) error
}

// Approvals is the exact-intent approval store (HARDQ C4 / P1.4): a grant
// hashes the EXACT effect (tool id + canonical args + schema hash + effect
// + kind + profile) and is expiring and SINGLE-USE. Any payload change
// invalidates it. topknot ceiling: canonical args = raw argument bytes
// (schema-hash pinned); JSON canonicalization lands with the T15 schema
// owner if key-reordering ever produces a false mismatch (safe direction:
// mismatch = re-ask, never silent allow).
type Approvals struct {
	mu     sync.Mutex
	now    func() time.Time
	ttl    time.Duration
	grants map[string]time.Time // effect hash → expiry
}

func NewApprovals(now func() time.Time, ttl time.Duration) *Approvals {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Approvals{now: now, ttl: ttl, grants: map[string]time.Time{}}
}

func effectHash(c contracts.ToolCall) string {
	h := sha256.New()
	for _, part := range [][]byte{
		[]byte(c.ToolID), c.Arguments, []byte(c.ArgsSchemaHash),
		{byte(c.Effect)}, {byte(c.ExecutionKind)}, []byte(c.ProfileID),
	} {
		// Length-prefix every part: no concatenation ambiguity.
		fmt.Fprintf(h, "%d:", len(part))
		h.Write(part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Approve registers the user's approval of THIS exact call.
func (a *Approvals) Approve(c contracts.ToolCall) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grants[effectHash(c)] = a.now().Add(a.ttl)
}

// consumeExact burns the approval for this exact call, if one is live.
func (a *Approvals) consumeExact(c contracts.ToolCall) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := effectHash(c)
	exp, ok := a.grants[key]
	if !ok {
		return false
	}
	delete(a.grants, key) // single-use, even when expired
	return a.now().Before(exp)
}

// PEP is the S6.0 policy enforcement point: a closed per-tool rule table,
// default-DENY for everything it does not know.
type PEP struct {
	rules     map[contracts.ToolID]Decision
	approvals *Approvals
	audit     AuditSink
	mode      PolicyMode
}

func NewPEP(rules map[contracts.ToolID]Decision, approvals *Approvals, audit AuditSink, mode PolicyMode) (*PEP, error) {
	if approvals == nil || audit == nil {
		return nil, fmt.Errorf("effectpath: approvals and audit sink are required (fail closed)")
	}
	if mode != ModeDefault && mode != ModeYolo {
		return nil, fmt.Errorf("effectpath: unknown policy mode %d (fail closed)", mode)
	}
	cp := make(map[contracts.ToolID]Decision, len(rules))
	for k, v := range rules {
		cp[k] = v
	}
	return &PEP{rules: cp, approvals: approvals, audit: audit, mode: mode}, nil
}

// Approvals exposes the approval store (the HITL owner feeds it in T16).
func (p *PEP) Approvals() *Approvals { return p.approvals }

// Decide is the total policy switch: unknown tool or an invalid stored
// rule → DENY (S6.0 default-deny; N4-C01).
func (p *PEP) Decide(c contracts.ToolCall) Decision {
	switch p.rules[c.ToolID] {
	case DecisionAllow:
		return DecisionAllow
	case DecisionAsk:
		return DecisionAsk
	case DecisionDeny:
		return DecisionDeny
	default:
		return DecisionDeny
	}
}

// InProcFunc is a pure in-process tool: plain Go, no subprocess access —
// the type owns no spawner, structurally (V-K1).
type InProcFunc func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error)

// InProcessExecutor runs registered pure-Go tools. Unknown tool → error.
type InProcessExecutor struct {
	fns map[contracts.ToolID]InProcFunc
}

func NewInProcessExecutor(fns map[contracts.ToolID]InProcFunc) *InProcessExecutor {
	cp := make(map[contracts.ToolID]InProcFunc, len(fns))
	for k, v := range fns {
		cp[k] = v
	}
	return &InProcessExecutor{fns: cp}
}

func (e *InProcessExecutor) Execute(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
	fn, ok := e.fns[c.ToolID]
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("effectpath: no in-process tool %q (fail closed)", c.ToolID)
	}
	return fn(ctx, c)
}

// SandboxBackend is the S6.2 membrane contract. The REAL bwrap backend
// binds in T26 (its integration RED lives there); everything ExecProcess
// goes through Launch — there is no other spawn path in this package.
type SandboxBackend interface {
	Launch(ctx context.Context, call contracts.ToolCall) (contracts.ToolResult, error)
}

// SandboxedProcessExecutor is the ONLY process-executing type: the sandbox
// membrane is in the type itself, not optional (V-K1).
type SandboxedProcessExecutor struct {
	sandbox SandboxBackend
}

func NewSandboxedProcessExecutor(sb SandboxBackend) *SandboxedProcessExecutor {
	return &SandboxedProcessExecutor{sandbox: sb}
}

func (e *SandboxedProcessExecutor) Execute(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
	if e.sandbox == nil {
		return contracts.ToolResult{}, fmt.Errorf("effectpath: no sandbox backend bound (fail closed)")
	}
	return e.sandbox.Launch(ctx, c)
}

// ClassifyEffectPhase is deterministic from executor attestation
// (N4-C03): a valid, THIS-call-bound receipt carries its phase; a present
// but invalid/foreign receipt is UNKNOWN; with no receipt only READ_ONLY
// can claim BEFORE_COMMIT — every effectful call without a receipt is
// UNKNOWN → reconciliation.
func ClassifyEffectPhase(c contracts.ToolCall, r contracts.ToolResult) contracts.EffectPhase {
	if r.Commit != nil {
		if r.Commit.ValidFor(c) {
			return r.Commit.Phase
		}
		return contracts.PhaseUnknown
	}
	if c.Effect == contracts.EffectReadOnly {
		return contracts.PhaseBeforeCommit
	}
	return contracts.PhaseUnknown
}

// EffectPath is the one tool-execution path (K1/K2): concrete executor
// fields — a non-sandboxed process executor cannot be injected.
type EffectPath struct {
	pep    *PEP
	mw     Middleware
	inproc *InProcessExecutor
	sbproc *SandboxedProcessExecutor
	grants *s7min.Authority
}

func NewEffectPath(pep *PEP, mw Middleware, inproc *InProcessExecutor, sbproc *SandboxedProcessExecutor, grants *s7min.Authority) (*EffectPath, error) {
	if pep == nil || mw == nil || inproc == nil || sbproc == nil || grants == nil {
		return nil, fmt.Errorf("effectpath: all collaborators are required (fail closed)")
	}
	return &EffectPath{pep: pep, mw: mw, inproc: inproc, sbproc: sbproc, grants: grants}, nil
}

// onErr routes err through the S6.9 OnError hook while GUARANTEEING the
// causal error survives — middleware is order-only and can never erase a
// refusal into success (Phase-2 codex #11).
func (p *EffectPath) onErr(ctx context.Context, err error) error {
	extra := p.mw.OnError(ctx, err)
	if extra == nil || errors.Is(extra, err) || extra.Error() == err.Error() {
		return err
	}
	return errors.Join(err, extra)
}

// failureOutcome classifies a post-dispatch failure (E9 / K2): an
// EFFECTFUL call whose receipt does not prove BEFORE_COMMIT parks
// UNKNOWN — something durable may exist; only READ_ONLY (or a receipt
// proving nothing committed) is safely terminal (Phase-2 codex #4).
func failureOutcome(call contracts.ToolCall, out contracts.ToolResult) s7min.Outcome {
	if ClassifyEffectPhase(call, out) == contracts.PhaseBeforeCommit {
		return s7min.OutcomeFailedTerminal
	}
	return s7min.OutcomeUnknown
}

// report lands the outcome and, for UNKNOWN, tags the returned error with
// ErrEffectUnknown so callers must reconcile, never continue blind.
func (p *EffectPath) report(ctx context.Context, op contracts.OperationID, outcome s7min.Outcome, cause error) error {
	if rerr := p.grants.Report(op, outcome); rerr != nil {
		cause = errors.Join(cause, rerr)
	}
	if outcome == s7min.OutcomeUnknown {
		cause = errors.Join(cause, ErrEffectUnknown)
	}
	return cause
}

// RunTool executes ONE tool call: validate → S6.0 Decide → S6.9 Before →
// grant bound+consumed (s7, BEFORE execute) → sealed-kind executor →
// result validation → S6.9 After/OnError → s7 outcome. Every refusal
// returns an explicitly empty ToolResult.
func (p *EffectPath) RunTool(ctx context.Context, call contracts.ToolCall, grant s7min.Grant) (contracts.ToolResult, error) {
	// 0) The call itself must be CONTRACT-VALID and alive (Phase-2 codex
	// #3): an expired deadline, broken correlation, or missing idempotency
	// key never reaches policy, let alone an executor.
	if err := call.Validate(); err != nil {
		return contracts.ToolResult{}, fmt.Errorf("effectpath: invalid tool call (fail closed): %w", err)
	}
	if !time.Now().Before(call.Deadline) {
		return contracts.ToolResult{}, fmt.Errorf("effectpath: tool %q: %w", call.ToolID, ErrCallExpired)
	}
	// The grant must have been minted FOR this call (target binding) and
	// this attempt — a grant for another call is a replay.
	if grant.TargetID != ToolTarget(call) || grant.AttemptNo != call.AttemptNo {
		return contracts.ToolResult{}, fmt.Errorf("effectpath: grant is not bound to this call: %w", s7min.ErrAttemptNotAuthorized)
	}
	// 1) S6.0 total switch, default-deny; ASK != ALLOW.
	switch p.pep.Decide(call) {
	case DecisionAllow:
		// continue
	case DecisionAsk:
		if p.pep.mode == ModeYolo {
			// HARDQ F2: confirmations only — and the bypass exists ONLY
			// with its durable record (fail closed on audit failure).
			if aerr := p.pep.audit.Record("ALLOWED_BY_YOLO", call); aerr != nil {
				return contracts.ToolResult{}, p.onErr(ctx,
					fmt.Errorf("effectpath: yolo audit not durable — confirmation bypass refused (fail closed): %w", aerr))
			}
		} else if !p.pep.approvals.consumeExact(call) {
			return contracts.ToolResult{}, fmt.Errorf("effectpath: tool %q: %w", call.ToolID, ErrNeedsApproval)
		}
	case DecisionDeny:
		return contracts.ToolResult{}, fmt.Errorf("effectpath: tool %q: %w", call.ToolID, ErrDenied)
	default:
		return contracts.ToolResult{}, fmt.Errorf("effectpath: tool %q: invalid decision: %w", call.ToolID, ErrDenied)
	}
	// 2) S6.9 order: Before.
	if err := p.mw.BeforeTool(ctx, call); err != nil {
		return contracts.ToolResult{}, p.onErr(ctx, err)
	}
	// 3) Total switch on the SEALED kind (N4-C02): unknown → reject, NEVER
	// in-process.
	var exec interface {
		Execute(context.Context, contracts.ToolCall) (contracts.ToolResult, error)
	}
	switch call.ExecutionKind {
	case contracts.ExecInProcess:
		exec = p.inproc
	case contracts.ExecProcess:
		exec = p.sbproc
	default:
		return contracts.ToolResult{}, p.onErr(ctx,
			fmt.Errorf("effectpath: tool %q kind %d: %w", call.ToolID, call.ExecutionKind, ErrUnknownExecKind))
	}
	// 4) s7 grant BEFORE any execution (SPEC P0.2): no grant, no attempt.
	if err := p.grants.Consume(grant); err != nil {
		return contracts.ToolResult{}, p.onErr(ctx, err)
	}
	op := grant.OperationID
	// 5) Execute.
	out, err := exec.Execute(ctx, call)
	if err != nil {
		// Executor error: AfterTool and output are SKIPPED; the result is
		// explicitly empty; an effectful call parks UNKNOWN (codex #4 —
		// an irreversible tool may have committed before losing its reply).
		return contracts.ToolResult{}, p.report(ctx, op, failureOutcome(call, contracts.ToolResult{}), p.onErr(ctx, err))
	}
	// 5b) The RESULT must correlate to this exact call (Phase-2 codex #5:
	// a mis-correlated or invalid-status result is an executor defect,
	// classified conservatively — never fed onward).
	if out.ToolCallID != call.ToolCallID || out.AttemptNo != call.AttemptNo || !out.Status.Valid() {
		cause := fmt.Errorf("effectpath: executor returned a result that does not correlate to the call (fail closed)")
		return contracts.ToolResult{}, p.report(ctx, op, failureOutcome(call, contracts.ToolResult{}), p.onErr(ctx, cause))
	}
	// 6) S6.9 order: After — a veto goes through OnError; an effectful
	// result without a valid receipt parks UNKNOWN (N3-C02/N4-C03).
	if verr := p.mw.AfterTool(ctx, call, out); verr != nil {
		return contracts.ToolResult{}, p.report(ctx, op, failureOutcome(call, out), p.onErr(ctx, verr))
	}
	// 7) s7 records the outcome by the CLOSED result status + effect
	// phase, never err == nil (codex #5).
	switch out.Status {
	case contracts.ResultSucceeded:
		if call.Effect != contracts.EffectReadOnly {
			phase := ClassifyEffectPhase(call, out)
			if phase != contracts.PhaseBeforeCommit && phase != contracts.PhaseAfterCommit {
				// Effectful "success" without a valid receipt is a CLAIM.
				cause := fmt.Errorf("effectpath: effectful result carries no valid commit receipt")
				return contracts.ToolResult{}, p.report(ctx, op, s7min.OutcomeUnknown, p.onErr(ctx, cause))
			}
		}
		if rerr := p.grants.Report(op, s7min.OutcomeSucceeded); rerr != nil {
			return contracts.ToolResult{}, p.onErr(ctx, rerr)
		}
		return out, nil
	case contracts.ResultFailed, contracts.ResultCancelled:
		cause := fmt.Errorf("effectpath: tool %q reported %s", call.ToolID, out.Status)
		return contracts.ToolResult{}, p.report(ctx, op, failureOutcome(call, out), p.onErr(ctx, cause))
	default: // ResultUnknown
		cause := fmt.Errorf("effectpath: tool %q reported UNKNOWN", call.ToolID)
		return contracts.ToolResult{}, p.report(ctx, op, s7min.OutcomeUnknown, p.onErr(ctx, cause))
	}
}

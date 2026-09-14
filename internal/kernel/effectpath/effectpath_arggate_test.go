//go:build linux

// S6.1 "permission gating ask/allow/deny po alatu i argumentu": ArgGate
// is a per-tool, argument-aware restriction consulted by PEP.Decide
// BEFORE the static per-ToolID table, but it can only NARROW an existing
// Allow/Ask down to Deny — never grant anything a static DENY or unknown
// tool already refuses. Anchored to the 4-round plan review that
// converged on this exact shape (S6.1 PEP ArgGate, 2026-09-14): round 1's
// naive "Decision-returning hook" let an ArgGate escalate a DENY/unknown
// tool into Allow/Ask; this deny-only shape makes that structurally
// impossible.
package effectpath

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

// buildWithGates mirrors build() but also wires argGates — kept separate
// from the shared build() helper so this file's additions don't force a
// signature change (and a mechanical update of every existing build()
// call site) onto the rest of the suite.
func buildWithGates(t *testing.T, mode PolicyMode, rules map[contracts.ToolID]Decision, gates map[contracts.ToolID]ArgGate) *harness {
	t.Helper()
	audit := &fakeAudit{}
	approvals := NewApprovals(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	pep, err := NewPEP(rules, gates, approvals, audit, mode)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	inproc := NewInProcessExecutor(map[contracts.ToolID]InProcFunc{
		"read": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			n++
			return okResult(c), nil
		},
	})
	sb := &fakeSandbox{}
	mw := &recordingMW{}
	grants := s7.NewAuthority(nil, time.Minute)
	path, err := NewEffectPath(pep, mw, inproc, NewSandboxedProcessExecutor(sb), grants)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{pep: pep, path: path, sandbox: sb, audit: audit, mw: mw, grants: grants, inprocN: &n}
}

func denyIfContains(needle string) ArgGate {
	return func(raw json.RawMessage) error {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		if s, _ := m["path"].(string); s == needle {
			return fmt.Errorf("denied: %q", needle)
		}
		return nil
	}
}

// An ArgGate narrows Allow to Deny for arguments it rejects, and lets
// non-matching arguments through to the static decision unchanged.
func TestArgGateNarrowsAllowToDeny(t *testing.T) {
	h := buildWithGates(t, ModeDefault,
		map[contracts.ToolID]Decision{"read": DecisionAllow},
		map[contracts.ToolID]ArgGate{"read": denyIfContains("/tmp/x")})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess) // Arguments: {"path":"/tmp/x"}
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err == nil {
		t.Fatal("expected the gate to deny this argument")
	}
	if *h.inprocN != 0 {
		t.Fatal("denied call must never reach the in-process tool")
	}
}

// The SAME gate lets a DIFFERENT argument value through to the static
// Allow — proving the gate narrows selectively, not unconditionally.
func TestArgGateAllowsNonMatchingArguments(t *testing.T) {
	h := buildWithGates(t, ModeDefault,
		map[contracts.ToolID]Decision{"read": DecisionAllow},
		map[contracts.ToolID]ArgGate{"read": denyIfContains("/nope")})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess) // {"path":"/tmp/x"}
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err != nil {
		t.Fatalf("non-matching argument must reach the static Allow: %v", err)
	}
	if *h.inprocN != 1 {
		t.Fatal("allowed call never reached the in-process tool")
	}
}

// A gate can narrow Ask to Deny too — Decide must never reach the
// approval machinery at all when the gate itself refuses.
func TestArgGateNarrowsAskToDeny(t *testing.T) {
	h := buildWithGates(t, ModeDefault,
		map[contracts.ToolID]Decision{"read": DecisionAsk},
		map[contracts.ToolID]ArgGate{"read": denyIfContains("/tmp/x")})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if d := h.pep.Decide(c); d != DecisionDeny {
		t.Fatalf("Decide = %v, want Deny", d)
	}
}

// The structural invariant round 2 added after round 1's escalation
// finding: a gate registered against a STATIC DENY tool is inert — it
// is never even consulted, so it cannot upgrade the refusal.
func TestArgGateCannotEscalateStaticDeny(t *testing.T) {
	alwaysAllow := func(json.RawMessage) error { return nil }
	pep, err := NewPEP(
		map[contracts.ToolID]Decision{"rmrf": DecisionDeny},
		map[contracts.ToolID]ArgGate{"rmrf": alwaysAllow},
		NewApprovals(nil, time.Minute), &fakeAudit{}, ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	c := call(t, "rmrf", contracts.EffectIrreversible, contracts.ExecProcess)
	if d := pep.Decide(c); d != DecisionDeny {
		t.Fatalf("a gate escalated a static DENY: got %v", d)
	}
}

// Same invariant for the OTHER terminal case: an UNKNOWN tool (no static
// rule at all — today's default-deny) cannot be granted access merely by
// registering a gate for its ToolID.
func TestArgGateCannotGrantUnknownTool(t *testing.T) {
	alwaysAllow := func(json.RawMessage) error { return nil }
	pep, err := NewPEP(nil,
		map[contracts.ToolID]ArgGate{"never-registered": alwaysAllow},
		NewApprovals(nil, time.Minute), &fakeAudit{}, ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	c := call(t, "never-registered", contracts.EffectReadOnly, contracts.ExecInProcess)
	if d := pep.Decide(c); d != DecisionDeny {
		t.Fatalf("a gate granted access to an unrecognized tool: got %v", d)
	}
}

// A gate that itself returns an invalid non-nil "denial" is still just a
// denial (error is a boolean signal here, not a Decision) — nothing to
// misinterpret, but confirms the plumbing treats ANY non-nil error as
// Deny regardless of its content.
func TestArgGateAnyNonNilErrorDenies(t *testing.T) {
	pep, err := NewPEP(
		map[contracts.ToolID]Decision{"read": DecisionAllow},
		map[contracts.ToolID]ArgGate{"read": func(json.RawMessage) error { return fmt.Errorf("") }},
		NewApprovals(nil, time.Minute), &fakeAudit{}, ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if d := pep.Decide(c); d != DecisionDeny {
		t.Fatalf("Decide = %v, want Deny", d)
	}
}

// An explicitly present nil ArgGate entry is a caller defect, not a
// legitimate "no restriction" request — optionality means the KEY is
// absent. NewPEP must refuse construction rather than silently drop it
// and fail open (codex round-1 code-review: the original nil-filtering
// masked a misconfiguration as an inert no-op instead of surfacing it;
// this replaces the round-2-era "drop silently, don't panic" test, which
// is now the wrong behavior to assert).
func TestArgGateNilEntryRefusedAtConstruction(t *testing.T) {
	_, err := NewPEP(
		map[contracts.ToolID]Decision{"read": DecisionAllow},
		map[contracts.ToolID]ArgGate{"read": nil},
		NewApprovals(nil, time.Minute), &fakeAudit{}, ModeDefault)
	if err == nil {
		t.Fatal("an explicitly nil ArgGate entry was silently accepted")
	}
}

// Omitting the key entirely (the actual "no restriction" spelling)
// still works exactly as before — nil gate ENTRIES are refused, but a
// nil/empty argGates MAP is the normal, common case (every existing
// test call site that doesn't care about gates passes nil for the
// whole map).
func TestArgGateOmittedKeyStillWorks(t *testing.T) {
	pep, err := NewPEP(
		map[contracts.ToolID]Decision{"read": DecisionAllow}, nil,
		NewApprovals(nil, time.Minute), &fakeAudit{}, ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess)
	if d := pep.Decide(c); d != DecisionAllow {
		t.Fatalf("Decide = %v, want Allow", d)
	}
}

// yolo (HARDQ F2) still only ever touches ASK, never DENY: a gate-denied
// call must be refused even under yolo, and a gate-passed Ask call still
// gets the ALLOWED_BY_YOLO audit treatment exactly as before this slice.
func TestArgGateInteractsCorrectlyWithYolo(t *testing.T) {
	h := buildWithGates(t, ModeYolo,
		map[contracts.ToolID]Decision{"read": DecisionAsk},
		map[contracts.ToolID]ArgGate{"read": denyIfContains("/tmp/x")})
	c := call(t, "read", contracts.EffectReadOnly, contracts.ExecInProcess) // {"path":"/tmp/x"}
	if _, err := h.path.RunTool(context.Background(), c, grantFor(t, h, "op-1", c)); err == nil {
		t.Fatal("yolo must not bypass a gate-produced DENY")
	}
	if len(h.audit.events) != 0 {
		t.Fatalf("a denied call must never be journaled as ALLOWED_BY_YOLO: %v", h.audit.events)
	}
}

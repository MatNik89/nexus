//go:build linux

package s7

import (
	"encoding/json"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Detector (code-review r3 codex #4): the durable narrative is validated
// against the closed (outcome, landing, code, next_at) table and reconcile
// exits; every incompatible pair is refused BEFORE append.
func TestEventValidatorsRefuseIncompatibleNarratives(t *testing.T) {
	ev := Events()
	reported := func(o Outcome, l LandingKind, code string, next int64) error {
		raw, _ := json.Marshal(reportedPayload{Op: "x", AttemptNo: 1, Outcome: int(o), Code: code, Landing: int(l), NextAt: next})
		return ev[EvAttemptReported](raw)
	}
	bad := []struct {
		o    Outcome
		l    LandingKind
		code string
		next int64
	}{
		{OutcomeSucceeded, LandingUnknown, "", 0},
		{OutcomeSucceeded, LandingSucceeded, CodeHTTP429, 0},
		{OutcomeUnknown, LandingSucceeded, "", 0},
		{OutcomeUnknown, LandingUnknown, "", 5},
		{OutcomeFailedTerminal, LandingRetry, CodeHTTP429, 5},
		{OutcomeFailedRetryable, LandingRetry, CodeHTTP429, 0},
		{OutcomeFailedRetryable, LandingTerminal, CodeHTTP429, 5},
		{OutcomeFailedRetryable, LandingSucceeded, "", 0},
		{Outcome(99), LandingTerminal, "", 0},
		{OutcomeFailedTerminal, LandingTerminal, "not_a_code", 0},
	}
	for i, b := range bad {
		if err := reported(b.o, b.l, b.code, b.next); err == nil {
			t.Fatalf("incompatible narrative %d accepted: %+v", i, b)
		}
	}
	good := []struct {
		o    Outcome
		l    LandingKind
		code string
		next int64
	}{
		{OutcomeSucceeded, LandingSucceeded, "", 0},
		{OutcomeUnknown, LandingUnknown, CodeHTTP5xx, 0},
		{OutcomeFailedRetryable, LandingRetry, CodeHTTP429, 5},
		{OutcomeFailedRetryable, LandingTerminal, CodeHTTP429, 0},
		{OutcomeFailedTerminal, LandingTerminal, CodeHTTP4xx, 0},
	}
	for i, g := range good {
		if err := reported(g.o, g.l, g.code, g.next); err != nil {
			t.Fatalf("compatible narrative %d refused: %v", i, err)
		}
	}
	reconcile := func(state string, next int64) error {
		raw, _ := json.Marshal(reconcilePayload{Op: "x", State: state, NextAt: next})
		return ev[EvOperationReconcile](raw)
	}
	for _, st := range []string{contracts.AttemptUnknown.String(), contracts.AttemptCancelled.String(), "BOGUS", contracts.AttemptRunning.String()} {
		if reconcile(st, 0) == nil {
			t.Fatalf("reconcile to %s accepted (not an UNKNOWN exit)", st)
		}
	}
	if reconcile(contracts.AttemptFailedRetryable.String(), 0) == nil || reconcile(contracts.AttemptSucceeded.String(), 7) == nil {
		t.Fatal("reconcile due-time relation not enforced")
	}
	if reconcile(contracts.AttemptSucceeded.String(), 0) != nil || reconcile(contracts.AttemptFailedRetryable.String(), 9) != nil || reconcile(contracts.AttemptFailed.String(), 0) != nil {
		t.Fatal("valid reconcile exits refused")
	}
	// Unknown fields / trailing data are refused too.
	if ev[EvAttemptStarted](json.RawMessage(`{"op":"x","attempt_no":1,"extra":true}`)) == nil {
		t.Fatal("unknown field accepted")
	}
	if ev[EvAttemptStarted](json.RawMessage(`{"op":"x","attempt_no":1} {}`)) == nil {
		t.Fatal("trailing data accepted")
	}
}

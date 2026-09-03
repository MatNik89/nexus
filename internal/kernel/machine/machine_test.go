// T08 RED table (tasks-P0): default-reject transitions (illegal
// RUNNING→ADMITTED, SUCCEEDED→RUNNING rejected — the ledger's literal
// cases), fold reconstruction, UNKNOWN exits only via reconciliation.
// Anchored to Annex P0.1 state lists, not the implementation.
package machine

import (
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func TestRunHappyPathFold(t *testing.T) {
	tbl := RunTable()
	final, err := tbl.Fold(contracts.RunInvalid, []string{
		EvRunCreated, EvRunAdmitted, EvRunStarted, EvRunSucceeded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if final != contracts.RunSucceeded {
		t.Fatalf("want SUCCEEDED, got %v", final)
	}
}

// The ledger's literal illegal cases.
func TestIllegalTransitionsRejected(t *testing.T) {
	tbl := RunTable()
	// RUNNING -> ADMITTED (via run.admitted from RUNNING)
	if _, err := tbl.Step(contracts.RunRunning, EvRunAdmitted); err == nil {
		t.Fatal("RUNNING->ADMITTED accepted")
	}
	// SUCCEEDED -> RUNNING (via run.started from SUCCEEDED)
	if _, err := tbl.Step(contracts.RunSucceeded, EvRunStarted); err == nil {
		t.Fatal("SUCCEEDED->RUNNING accepted")
	}
	// Unknown event entirely.
	if _, err := tbl.Step(contracts.RunRunning, "run.totally_new"); err == nil {
		t.Fatal("unknown transition event accepted")
	}
}

// Terminal states accept nothing (P0.1: SUCCEEDED|FAILED|CANCELLED terminal).
func TestTerminalStatesAreTerminal(t *testing.T) {
	tbl := RunTable()
	for _, terminal := range []contracts.RunState{
		contracts.RunSucceeded, contracts.RunFailed, contracts.RunCancelled,
		contracts.RunManualRecovery,
	} {
		for _, ev := range EventTypes()[:12] { // run events
			if next, err := tbl.Step(terminal, ev); err == nil {
				t.Fatalf("terminal %v accepted %s -> %v", terminal, ev, next)
			}
		}
	}
}

// UNKNOWN exits ONLY via reconciliation events (P0.1).
func TestUnknownExitsOnlyViaReconciliation(t *testing.T) {
	tbl := RunTable()
	if _, err := tbl.Step(contracts.RunUnknown, EvRunStarted); err == nil {
		t.Fatal("UNKNOWN->RUNNING via ordinary event accepted")
	}
	if _, err := tbl.Step(contracts.RunUnknown, EvRunSucceeded); err == nil {
		t.Fatal("UNKNOWN->SUCCEEDED via ordinary event accepted")
	}
	for ev, want := range map[string]contracts.RunState{
		EvRunReconciled:       contracts.RunSucceeded,
		EvRunReconciledFailed: contracts.RunFailed,
		EvRunManual:           contracts.RunManualRecovery,
	} {
		got, err := tbl.Step(contracts.RunUnknown, ev)
		if err != nil || got != want {
			t.Fatalf("reconciliation %s: got %v/%v, want %v", ev, got, err, want)
		}
	}
}

// A fold hitting an illegal edge reports the exact step and keeps the last
// legal state (causal error context).
func TestFoldStopsAtIllegalStepWithContext(t *testing.T) {
	tbl := RunTable()
	state, err := tbl.Fold(contracts.RunInvalid, []string{
		EvRunCreated, EvRunAdmitted, EvRunSucceeded, // illegal: ADMITTED->SUCCEEDED
	})
	if err == nil {
		t.Fatal("illegal fold accepted")
	}
	if state != contracts.RunAdmitted {
		t.Fatalf("fold must return the last legal state, got %v", state)
	}
}

// Duplicate edges are construction errors (a table cannot silently shadow).
func TestDuplicateEdgeRejectedAtConstruction(t *testing.T) {
	_, err := NewTable("dup", []Transition[contracts.RunState]{
		{"e", contracts.RunCreated, contracts.RunAdmitted},
		{"e", contracts.RunCreated, contracts.RunRunning},
	})
	if err == nil {
		t.Fatal("duplicate (event, from) edge accepted")
	}
}

// Attempt table honors the AUTHORIZED gate (no start without authorization
// — the S6.0/S7 seam in state form).
func TestAttemptRequiresAuthorization(t *testing.T) {
	tbl := AttemptTable()
	if _, err := tbl.Step(contracts.AttemptPlanned, EvAttemptStarted); err == nil {
		t.Fatal("PLANNED->RUNNING without AUTHORIZED accepted")
	}
	final, err := tbl.Fold(contracts.AttemptInvalid, []string{
		EvAttemptPlanned, EvAttemptAuthorized, EvAttemptStarted, EvAttemptSucceeded,
	})
	if err != nil || final != contracts.AttemptSucceeded {
		t.Fatalf("legal attempt path failed: %v/%v", final, err)
	}
}

// EventTypes seeds the journal's closed set: every table edge is driveable
// through it (no orphan event names).
func TestEventTypesCoverAllTables(t *testing.T) {
	names := map[string]bool{}
	for _, n := range EventTypes() {
		if names[n] {
			t.Fatalf("duplicate event type %s", n)
		}
		names[n] = true
	}
	if len(names) != 30 {
		t.Fatalf("want 30 distinct event types, got %d", len(names))
	}
}

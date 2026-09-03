// T08 RED table (tasks-P0): default-reject transitions (illegal
// RUNNING→ADMITTED, SUCCEEDED→RUNNING rejected — the ledger's literal
// cases), fold reconstruction, UNKNOWN exits only via reconciliation.
// Anchored to Annex P0.1 state lists, not the implementation.
package machine

import (
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func evs(offsetStart uint64, types ...string) []FoldEvent {
	out := make([]FoldEvent, len(types))
	for i, ty := range types {
		out[i] = FoldEvent{Type: ty, Offset: offsetStart + uint64(i)}
	}
	return out
}

func TestRunHappyPathFoldWithIntermediatesAndCheckpoint(t *testing.T) {
	tbl := RunTable()
	var seen []contracts.RunState
	final, checkpoint, err := tbl.Fold(contracts.RunInvalid,
		evs(10, EvRunCreated, EvRunAdmitted, EvRunStarted, EvRunSucceeded),
		func(s contracts.RunState, _ uint64) { seen = append(seen, s) })
	if err != nil {
		t.Fatal(err)
	}
	if final != contracts.RunSucceeded {
		t.Fatalf("want SUCCEEDED, got %v", final)
	}
	if checkpoint != 13 {
		t.Fatalf("checkpoint must be the last applied offset: %d", checkpoint)
	}
	// EVERY intermediate state reproduced (ledger acceptance literal).
	want := []contracts.RunState{contracts.RunCreated, contracts.RunAdmitted, contracts.RunRunning, contracts.RunSucceeded}
	if len(seen) != len(want) {
		t.Fatalf("intermediates: %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("intermediate %d: got %v want %v", i, seen[i], want[i])
		}
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
		for _, ev := range EventTypes()[:10] { // run events
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
func TestFoldStopsAtIllegalStepWithOffsetContext(t *testing.T) {
	tbl := RunTable()
	state, checkpoint, err := tbl.Fold(contracts.RunInvalid,
		evs(5, EvRunCreated, EvRunAdmitted, EvRunSucceeded), nil) // illegal 3rd
	if err == nil {
		t.Fatal("illegal fold accepted")
	}
	if state != contracts.RunAdmitted {
		t.Fatalf("fold must return the last legal state, got %v", state)
	}
	if checkpoint != 6 {
		t.Fatalf("checkpoint must stop BEFORE the bad event: %d", checkpoint)
	}
	if !strings.Contains(err.Error(), "offset 7") {
		t.Fatalf("error must carry the failing offset: %v", err)
	}
}

// Step returns the CURRENT state on reject (codex #10 literal).
func TestStepReturnsCurrentOnReject(t *testing.T) {
	tbl := RunTable()
	got, err := tbl.Step(contracts.RunRunning, "run.nonsense")
	if err == nil || got != contracts.RunRunning {
		t.Fatalf("rejected step must return current state: got %v err %v", got, err)
	}
}

// Canonical cancel event works from every non-terminal state (kilo #1
// literal: the REAL run.cancelled must not fail closed at emit time).
func TestCanonicalCancelFromEveryNonTerminalState(t *testing.T) {
	tbl := RunTable()
	for _, from := range []contracts.RunState{contracts.RunCreated, contracts.RunAdmitted, contracts.RunRunning} {
		got, err := tbl.Step(from, EvRunCancelled)
		if err != nil || got != contracts.RunCancelled {
			t.Fatalf("run.cancelled from %v: got %v err %v", from, got, err)
		}
	}
	if _, err := tbl.Step(contracts.RunUnknown, EvRunCancelled); err == nil {
		t.Fatal("cancel from UNKNOWN accepted (must reconcile instead)")
	}
}

// Exhaustive edge matrix: EVERY (state, event) pair is either in the
// declared legal set or rejected — no undeclared edges (codex #8/#20).
func TestExhaustiveRunEdgeMatrix(t *testing.T) {
	tbl := RunTable()
	legal := map[string]map[contracts.RunState]contracts.RunState{
		EvRunCreated:          {contracts.RunInvalid: contracts.RunCreated},
		EvRunAdmitted:         {contracts.RunCreated: contracts.RunAdmitted},
		EvRunStarted:          {contracts.RunAdmitted: contracts.RunRunning},
		EvRunSucceeded:        {contracts.RunRunning: contracts.RunSucceeded},
		EvRunFailed:           {contracts.RunRunning: contracts.RunFailed},
		EvRunCancelled:        {contracts.RunCreated: contracts.RunCancelled, contracts.RunAdmitted: contracts.RunCancelled, contracts.RunRunning: contracts.RunCancelled},
		EvRunLost:             {contracts.RunRunning: contracts.RunUnknown},
		EvRunReconciled:       {contracts.RunUnknown: contracts.RunSucceeded},
		EvRunReconciledFailed: {contracts.RunUnknown: contracts.RunFailed},
		EvRunManual:           {contracts.RunUnknown: contracts.RunManualRecovery},
	}
	states := []contracts.RunState{
		contracts.RunInvalid, contracts.RunCreated, contracts.RunAdmitted,
		contracts.RunRunning, contracts.RunSucceeded, contracts.RunFailed,
		contracts.RunCancelled, contracts.RunUnknown, contracts.RunManualRecovery,
	}
	for ev, froms := range legal {
		for _, st := range states {
			want, isLegal := froms[st]
			got, err := tbl.Step(st, ev)
			if isLegal {
				if err != nil || got != want {
					t.Fatalf("legal edge (%v,%s) failed: %v %v", st, ev, got, err)
				}
			} else if err == nil {
				t.Fatalf("undeclared edge (%v,%s) accepted -> %v", st, ev, got)
			}
		}
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
	final, _, err := tbl.Fold(contracts.AttemptInvalid,
		evs(1, EvAttemptPlanned, EvAttemptAuthorized, EvAttemptStarted, EvAttemptSucceeded), nil)
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
	if len(names) != 25 {
		t.Fatalf("want 25 distinct event types, got %d", len(names))
	}
}

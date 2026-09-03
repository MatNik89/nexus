// Package machine holds the explicit state-transition tables (S0.2, Annex
// P0.1 state lists): default-reject transition maps, state RECONSTRUCTED by
// folding journal events (never a mutable field), checkpoint = journal
// offset. UNKNOWN exits ONLY via an explicit reconciliation event to
// SUCCEEDED | FAILED | MANUAL_RECOVERY.
package machine

import (
	"fmt"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Transition names one legal edge, keyed by the event type that drives it.
type Transition[S comparable] struct {
	Event string
	From  S
	To    S
}

// Table is an explicit default-reject transition table.
type Table[S comparable] struct {
	name  string
	edges map[string]map[S]S // event -> from -> to
}

// NewTable builds a table; duplicate (event, from) edges are a programming
// error and rejected at construction.
func NewTable[S comparable](name string, transitions []Transition[S]) (*Table[S], error) {
	t := &Table[S]{name: name, edges: map[string]map[S]S{}}
	for _, tr := range transitions {
		if tr.Event == "" {
			return nil, fmt.Errorf("table %s: transition with empty event", name)
		}
		byFrom, ok := t.edges[tr.Event]
		if !ok {
			byFrom = map[S]S{}
			t.edges[tr.Event] = byFrom
		}
		if _, dup := byFrom[tr.From]; dup {
			return nil, fmt.Errorf("table %s: duplicate edge for event %s", name, tr.Event)
		}
		byFrom[tr.From] = tr.To
	}
	return t, nil
}

// Step applies one event to the current state. Unknown event or illegal
// (event, from) pair → error, state unchanged (default-reject).
// (Phase-1B codex #10: on reject the CURRENT state is returned unchanged —
// the zero value let a careless caller overwrite valid state.)
func (t *Table[S]) Step(current S, event string) (S, error) {
	byFrom, ok := t.edges[event]
	if !ok {
		return current, fmt.Errorf("table %s: unknown transition event %q (fail closed)", t.name, event)
	}
	next, ok := byFrom[current]
	if !ok {
		return current, fmt.Errorf("table %s: event %q is illegal from state %v (fail closed)", t.name, event, current)
	}
	return next, nil
}

// FoldEvent is one journaled event as the fold consumes it: type plus its
// JOURNAL OFFSET (checkpoint currency — Phase-1B codex #9).
type FoldEvent struct {
	Type   string
	Offset uint64
}

// Fold replays journal events in order, returning the final state and the
// CHECKPOINT (offset of the last applied event). observe receives every
// intermediate state (nil to skip). An illegal step reports the exact
// offset and returns the last legal state with the checkpoint BEFORE it.
// Offsets are JOURNAL offsets and must be STRICTLY INCREASING (gaps are
// legal after entity filtering; zero, duplicate, or decreasing offsets are
// rejected — Phase-1B-r2 codex #6: a regressing checkpoint would make
// resume replay already-applied events). A RESUMED fold passes the state
// folded so far as initial and only events PAST its prior checkpoint.
func (t *Table[S]) Fold(initial S, events []FoldEvent, observe func(state S, offset uint64)) (S, uint64, error) {
	state := initial
	var checkpoint uint64
	for _, ev := range events {
		if ev.Offset <= checkpoint {
			return state, checkpoint, fmt.Errorf(
				"fold: offset %d is not strictly increasing after checkpoint %d (fail closed)", ev.Offset, checkpoint)
		}
		next, err := t.Step(state, ev.Type)
		if err != nil {
			return state, checkpoint, fmt.Errorf("fold at offset %d: %w", ev.Offset, err)
		}
		state = next
		checkpoint = ev.Offset
		if observe != nil {
			observe(state, ev.Offset)
		}
	}
	return state, checkpoint, nil
}

// --- Run lifecycle (P0.1): CREATED→ADMITTED→RUNNING→{SUCCEEDED|FAILED|
// CANCELLED|UNKNOWN}; UNKNOWN → reconciliation only. ---

// Run event types (closed; these are journal event_type values).
const (
	EvRunCreated          = "run.created"
	EvRunAdmitted         = "run.admitted"
	EvRunStarted          = "run.started"
	EvRunSucceeded        = "run.succeeded"
	EvRunFailed           = "run.failed"
	EvRunCancelled        = "run.cancelled"
	EvRunLost             = "run.lost" // → UNKNOWN
	EvRunReconciled       = "run.reconciled_ok"
	EvRunReconciledFailed = "run.reconciled_failed"
	EvRunManual           = "run.manual_recovery"
)

// RunTable returns the canonical run-state table.
func RunTable() *Table[contracts.RunState] {
	t, err := NewTable("run", []Transition[contracts.RunState]{
		{EvRunCreated, contracts.RunInvalid, contracts.RunCreated},
		{EvRunAdmitted, contracts.RunCreated, contracts.RunAdmitted},
		{EvRunStarted, contracts.RunAdmitted, contracts.RunRunning},
		{EvRunSucceeded, contracts.RunRunning, contracts.RunSucceeded},
		{EvRunFailed, contracts.RunRunning, contracts.RunFailed},
		// ONE canonical cancel event, legal from every non-terminal state
		// (per-event multi-from — Phase-1B kilo #1: alias names would make
		// the real run.cancelled fail closed at emit time). Cancel-from-
		// CREATED/ADMITTED = explicit owner amendment (SPEC P0.1 record).
		{EvRunCancelled, contracts.RunCreated, contracts.RunCancelled},
		{EvRunCancelled, contracts.RunAdmitted, contracts.RunCancelled},
		{EvRunCancelled, contracts.RunRunning, contracts.RunCancelled},
		{EvRunLost, contracts.RunRunning, contracts.RunUnknown},
		// UNKNOWN exits ONLY via explicit reconciliation events (P0.1).
		{EvRunReconciled, contracts.RunUnknown, contracts.RunSucceeded},
		{EvRunReconciledFailed, contracts.RunUnknown, contracts.RunFailed},
		{EvRunManual, contracts.RunUnknown, contracts.RunManualRecovery},
	})
	if err != nil {
		panic(err) // static table; a duplicate is a compile-time-class bug
	}
	return t
}

// Turn event types.
const (
	EvTurnCreated   = "turn.created"
	EvTurnStarted   = "turn.started"
	EvTurnSucceeded = "turn.succeeded"
	EvTurnFailed    = "turn.failed"
	EvTurnCancelled = "turn.cancelled"
)

// TurnTable returns the canonical turn-state table.
func TurnTable() *Table[contracts.TurnState] {
	t, err := NewTable("turn", []Transition[contracts.TurnState]{
		{EvTurnCreated, contracts.TurnInvalid, contracts.TurnCreated},
		{EvTurnStarted, contracts.TurnCreated, contracts.TurnRunning},
		{EvTurnSucceeded, contracts.TurnRunning, contracts.TurnSucceeded},
		{EvTurnFailed, contracts.TurnRunning, contracts.TurnFailed},
		{EvTurnCancelled, contracts.TurnCreated, contracts.TurnCancelled},
		{EvTurnCancelled, contracts.TurnRunning, contracts.TurnCancelled},
	})
	if err != nil {
		panic(err)
	}
	return t
}

// Attempt event types.
const (
	EvAttemptPlanned          = "attempt.planned"
	EvAttemptAuthorized       = "attempt.authorized"
	EvAttemptStarted          = "attempt.started"
	EvAttemptSucceeded        = "attempt.succeeded"
	EvAttemptFailed           = "attempt.failed"
	EvAttemptCancelled        = "attempt.cancelled"
	EvAttemptLost             = "attempt.lost"
	EvAttemptReconciledOK     = "attempt.reconciled_ok"
	EvAttemptReconciledFailed = "attempt.reconciled_failed"
	EvAttemptManual           = "attempt.manual_recovery"
)

// AttemptTable returns the canonical tool-attempt table (P0.1:
// PLANNED→AUTHORIZED→RUNNING→terminal; UNKNOWN reconciles only).
func AttemptTable() *Table[contracts.AttemptState] {
	t, err := NewTable("attempt", []Transition[contracts.AttemptState]{
		{EvAttemptPlanned, contracts.AttemptInvalid, contracts.AttemptPlanned},
		{EvAttemptAuthorized, contracts.AttemptPlanned, contracts.AttemptAuthorized},
		{EvAttemptStarted, contracts.AttemptAuthorized, contracts.AttemptRunning},
		{EvAttemptSucceeded, contracts.AttemptRunning, contracts.AttemptSucceeded},
		{EvAttemptFailed, contracts.AttemptRunning, contracts.AttemptFailed},
		{EvAttemptCancelled, contracts.AttemptPlanned, contracts.AttemptCancelled},
		{EvAttemptCancelled, contracts.AttemptAuthorized, contracts.AttemptCancelled},
		{EvAttemptCancelled, contracts.AttemptRunning, contracts.AttemptCancelled},
		{EvAttemptLost, contracts.AttemptRunning, contracts.AttemptUnknown},
		{EvAttemptReconciledOK, contracts.AttemptUnknown, contracts.AttemptSucceeded},
		{EvAttemptReconciledFailed, contracts.AttemptUnknown, contracts.AttemptFailed},
		{EvAttemptManual, contracts.AttemptUnknown, contracts.AttemptManualRecovery},
	})
	if err != nil {
		panic(err)
	}
	return t
}

// EventTypes returns every event type the three canonical tables drive —
// the seed for the journal's closed event-type set.
func EventTypes() []string {
	return []string{
		EvRunCreated, EvRunAdmitted, EvRunStarted, EvRunSucceeded, EvRunFailed,
		EvRunCancelled, EvRunLost, EvRunReconciled, EvRunReconciledFailed, EvRunManual,
		EvTurnCreated, EvTurnStarted, EvTurnSucceeded, EvTurnFailed, EvTurnCancelled,
		EvAttemptPlanned, EvAttemptAuthorized, EvAttemptStarted, EvAttemptSucceeded,
		EvAttemptFailed, EvAttemptCancelled, EvAttemptLost, EvAttemptReconciledOK,
		EvAttemptReconciledFailed, EvAttemptManual,
	}
}

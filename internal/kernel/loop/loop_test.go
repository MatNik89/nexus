//go:build linux

// T16 RED table (tasks-P0): one-turn plan→act→observe loop — tool errors
// packed into observation, provenance laundering rejected (Annex P0.1),
// identical-call stuck-breaker within one INTERACTIVE turn with the
// continuous-loop exemption (HARDQ C3), e2e turn through the journal with
// replay reproduction. Anchored to E5 + P0.1 + C3.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/security/redact"
)

type scriptedPlanner struct {
	actions []Action
	i       int
	seen    [][]contracts.ContextBlock
}

func (p *scriptedPlanner) Plan(ctx context.Context, blocks []contracts.ContextBlock) (Action, error) {
	p.seen = append(p.seen, blocks)
	if p.i >= len(p.actions) {
		return Action{}, fmt.Errorf("planner script exhausted")
	}
	a := p.actions[p.i]
	p.i++
	return a, nil
}

type nopMW struct{}

func (nopMW) BeforeTool(context.Context, contracts.ToolCall) error                      { return nil }
func (nopMW) AfterTool(context.Context, contracts.ToolCall, contracts.ToolResult) error { return nil }
func (nopMW) OnError(ctx context.Context, e error) error                                { return e }

type nopAudit struct{}

func (nopAudit) Record(string, contracts.ToolCall) {}

type nopSandbox struct{}

func (nopSandbox) Launch(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
	return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
		Status: contracts.ResultSucceeded, StartedAt: time.Unix(1, 0), FinishedAt: time.Unix(2, 0)}, nil
}

func strp(s string) *string { return &s }

func block(id string, trust contracts.TrustClass, lineage []string) contracts.ContextBlock {
	if lineage == nil {
		lineage = []string{}
	}
	b, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID(id), Kind: "text", Content: strp("content of " + id),
		ContentHash: "h", SourceURI: "test://" + id, Producer: "test",
		Trust: trust, Sensitivity: contracts.Sensitivity(1), Lineage: lineage,
		ObservedAt: time.Unix(500, 0),
	})
	if err != nil {
		panic(err)
	}
	return b
}

func toolCall(tool, id string) contracts.ToolCall {
	c, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: contracts.ToolCallID(id), ToolID: contracts.ToolID(tool),
		Arguments: json.RawMessage(`{"q":1}`), ArgsSchemaHash: "h1",
		Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
		Deadline: time.Unix(9000, 0), AttemptNo: 1, ProfileID: "work",
	})
	if err != nil {
		panic(err)
	}
	return c
}

type harness struct {
	loop    *Loop
	planner *scriptedPlanner
	journal *journal.Journal
	grants  *s7min.Authority
	toolOut *[]contracts.ContextBlock // next inproc tool output blocks
	toolErr *error
}

func build(t *testing.T, policy Policy, actions []Action) *harness {
	t.Helper()
	ev := map[string]journal.PayloadValidator{}
	for _, n := range machine.EventTypes() {
		ev[n] = nil
	}
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.db"), "work", redact.None{}, ev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	var outBlocks []contracts.ContextBlock
	var outErr error
	inproc := effectpath.NewInProcessExecutor(map[contracts.ToolID]effectpath.InProcFunc{
		"read": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if outErr != nil {
				return contracts.ToolResult{}, outErr
			}
			return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
				Status: contracts.ResultSucceeded, Output: outBlocks,
				StartedAt: time.Unix(1, 0), FinishedAt: time.Unix(2, 0)}, nil
		},
	})
	pep, err := effectpath.NewPEP(map[contracts.ToolID]effectpath.Decision{"read": effectpath.DecisionAllow},
		effectpath.NewApprovals(nil, time.Minute), nopAudit{}, effectpath.ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	grants := s7min.NewAuthority(nil, time.Minute)
	path, err := effectpath.NewEffectPath(pep, nopMW{}, inproc,
		effectpath.NewSandboxedProcessExecutor(nopSandbox{}), grants)
	if err != nil {
		t.Fatal(err)
	}
	planner := &scriptedPlanner{actions: actions}
	l, err := New(planner, path, grants, j, policy, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{loop: l, planner: planner, journal: j, grants: grants, toolOut: &outBlocks, toolErr: &outErr}
}

var initialBlocks = []contracts.ContextBlock{block("sys-1", contracts.TrustSystem, nil)}

// E2E: plan → one tool call → observe → final; the turn and its attempt
// live in the journal, and REPLAY through the canonical tables reproduces
// the terminal states (ledger acceptance literal).
func TestEndToEndTurnThroughJournalAndReplay(t *testing.T) {
	call := toolCall("read", "tc-1")
	h := build(t, PolicyInteractive, []Action{{Call: &call}, {Final: strp("done answer")}})
	*h.toolOut = []contracts.ContextBlock{block("obs-1", contracts.TrustToolTrusted, []string{"tc-1"})}
	final, err := h.loop.RunTurn(context.Background(), "turn-1", "run-1", "work", initialBlocks)
	if err != nil {
		t.Fatal(err)
	}
	if final != "done answer" {
		t.Fatalf("final %q", final)
	}
	// The observation reached the second plan.
	last := h.planner.seen[len(h.planner.seen)-1]
	found := false
	for _, b := range last {
		if b.BlockID == "obs-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("tool observation never reached the planner")
	}
	// Replay: fold the turn's journal events → SUCCEEDED reproduced.
	var turnEvents, attemptEvents []machine.FoldEvent
	if err := h.journal.Replay(0, func(e journal.Event) error {
		if strings.HasPrefix(e.Envelope.EventType, "turn.") {
			turnEvents = append(turnEvents, machine.FoldEvent{Type: e.Envelope.EventType, Offset: e.JournalOffset})
		}
		if strings.HasPrefix(e.Envelope.EventType, "attempt.") {
			attemptEvents = append(attemptEvents, machine.FoldEvent{Type: e.Envelope.EventType, Offset: e.JournalOffset})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, _, err := machine.TurnTable().Fold(contracts.TurnInvalid, turnEvents, nil)
	if err != nil || st != contracts.TurnSucceeded {
		t.Fatalf("replayed turn folds to %v (%v)", st, err)
	}
	ast, _, err := machine.AttemptTable().Fold(contracts.AttemptInvalid, attemptEvents, nil)
	if err != nil || ast != contracts.AttemptSucceeded {
		t.Fatalf("replayed attempt folds to %v (%v)", ast, err)
	}
}

// Tool errors are PACKED INTO OBSERVATION — the turn continues and the
// planner sees the failure as data.
func TestToolErrorPackedIntoObservation(t *testing.T) {
	call := toolCall("read", "tc-1")
	h := build(t, PolicyInteractive, []Action{{Call: &call}, {Final: strp("recovered")}})
	*h.toolErr = fmt.Errorf("disk on fire")
	final, err := h.loop.RunTurn(context.Background(), "turn-1", "run-1", "work", initialBlocks)
	if err != nil || final != "recovered" {
		t.Fatalf("turn must survive a tool error: %q %v", final, err)
	}
	last := h.planner.seen[len(h.planner.seen)-1]
	sawError := false
	for _, b := range last {
		if b.Content != nil && strings.Contains(*b.Content, "disk on fire") && b.Trust == contracts.TrustToolTrusted {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("tool error not packed into an observation block")
	}
}

// test_s0_rejects_provenance_laundering (P0.1 literal): a tool output
// claiming SYSTEM/USER trust — or missing its tool-call lineage — never
// enters the context; the turn fails.
func TestS0RejectsProvenanceLaundering(t *testing.T) {
	for name, out := range map[string]contracts.ContextBlock{
		"launder-to-system": block("evil-1", contracts.TrustSystem, []string{"tc-1"}),
		"launder-to-user":   block("evil-2", contracts.TrustUser, []string{"tc-1"}),
		"missing-lineage":   block("evil-3", contracts.TrustToolTrusted, nil),
	} {
		t.Run(name, func(t *testing.T) {
			call := toolCall("read", "tc-1")
			h := build(t, PolicyInteractive, []Action{{Call: &call}, {Final: strp("never")}})
			*h.toolOut = []contracts.ContextBlock{out}
			_, err := h.loop.RunTurn(context.Background(), "turn-1", "run-1", "work", initialBlocks)
			if err == nil {
				t.Fatal("laundered observation accepted")
			}
			for _, seen := range h.planner.seen {
				for _, b := range seen {
					if strings.HasPrefix(string(b.BlockID), "evil") {
						t.Fatal("laundered block reached the planner")
					}
				}
			}
		})
	}
}

// POSITIVE breaker literal: the same tool+args repeated N times within one
// INTERACTIVE turn trips the breaker.
func TestIdenticalCallBreakerTrips(t *testing.T) {
	c1, c2, c3 := toolCall("read", "tc-1"), toolCall("read", "tc-2"), toolCall("read", "tc-3")
	h := build(t, PolicyInteractive, []Action{{Call: &c1}, {Call: &c2}, {Call: &c3}, {Final: strp("never")}})
	*h.toolOut = []contracts.ContextBlock{}
	_, err := h.loop.RunTurn(context.Background(), "turn-1", "run-1", "work", initialBlocks)
	if err == nil || !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("identical-call loop did not trip the breaker: %v", err)
	}
}

// The C3 exemption: a CONTINUOUS-policy turn (scheduled/polling) repeats
// the identical call N times without tripping.
func TestContinuousPolicyExemptFromBreaker(t *testing.T) {
	c1, c2, c3 := toolCall("read", "tc-1"), toolCall("read", "tc-2"), toolCall("read", "tc-3")
	h := build(t, PolicyContinuous, []Action{{Call: &c1}, {Call: &c2}, {Call: &c3}, {Final: strp("polled")}})
	*h.toolOut = []contracts.ContextBlock{}
	final, err := h.loop.RunTurn(context.Background(), "turn-1", "run-1", "work", initialBlocks)
	if err != nil || final != "polled" {
		t.Fatalf("continuous policy tripped the breaker: %q %v", final, err)
	}
}

// A planner action with BOTH or NEITHER of final/call is rejected (closed
// XOR — fail closed), and the turn lands FAILED in the journal.
func TestMalformedActionFailsTurn(t *testing.T) {
	h := build(t, PolicyInteractive, []Action{{}})
	_, err := h.loop.RunTurn(context.Background(), "turn-1", "run-1", "work", initialBlocks)
	if err == nil {
		t.Fatal("empty action accepted")
	}
	var turnEvents []machine.FoldEvent
	h.journal.Replay(0, func(e journal.Event) error {
		if strings.HasPrefix(e.Envelope.EventType, "turn.") {
			turnEvents = append(turnEvents, machine.FoldEvent{Type: e.Envelope.EventType, Offset: e.JournalOffset})
		}
		return nil
	})
	st, _, ferr := machine.TurnTable().Fold(contracts.TurnInvalid, turnEvents, nil)
	if ferr != nil || st != contracts.TurnFailed {
		t.Fatalf("malformed action must land TURN FAILED in the journal: %v (%v)", st, ferr)
	}
}

//go:build linux

// Package loop is the S3.1-min one-turn agent loop: plan → act → observe,
// journaled end to end (turn + attempt events fold through the canonical
// tables on replay). Tool errors become OBSERVATIONS, not turn failures;
// provenance laundering is rejected at the observe seam (Annex P0.1: a
// tool can never mint SYSTEM/USER trust, and every observation carries its
// producing tool call in lineage); the identical-call stuck-breaker fires
// WITHIN one interactive turn only — continuous (scheduled/polling) turns
// are exempt by explicit policy (HARDQ C3).
package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/assembler"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
)

// Action is the planner's closed XOR outcome: exactly one of a final
// answer or a tool call.
type Action struct {
	Final *string
	Call  *contracts.ToolCall
}

// Planner produces the next action from the assembled context. The real
// provider-backed planner adapts in T17; contract fakes drive T16 REDs.
type Planner interface {
	Plan(ctx context.Context, blocks []contracts.ContextBlock) (Action, error)
}

// Policy scopes the stuck-breaker (HARDQ C3): INTERACTIVE turns break on
// identical repetition; CONTINUOUS (scheduled/polling) turns are exempt.
type Policy uint8

const (
	PolicyInvalid Policy = iota
	PolicyInteractive
	PolicyContinuous
)

// Loop drives one turn at a time. All collaborators are required.
type Loop struct {
	planner  Planner
	path     *effectpath.EffectPath
	grants   *s7min.Authority
	journal  *journal.Journal
	policy   Policy
	maxIters int
	breakerN int
}

func New(p Planner, path *effectpath.EffectPath, grants *s7min.Authority, j *journal.Journal,
	policy Policy, maxIters, breakerN int) (*Loop, error) {
	if p == nil || path == nil || grants == nil || j == nil {
		return nil, fmt.Errorf("loop: all collaborators are required (fail closed)")
	}
	if policy != PolicyInteractive && policy != PolicyContinuous {
		return nil, fmt.Errorf("loop: unknown policy %d (fail closed)", policy)
	}
	if maxIters <= 0 || breakerN <= 1 {
		return nil, fmt.Errorf("loop: iteration and breaker bounds must be positive (fail closed)")
	}
	return &Loop{planner: p, path: path, grants: grants, journal: j,
		policy: policy, maxIters: maxIters, breakerN: breakerN}, nil
}

// append journals one lifecycle event for this turn's run.
func (l *Loop) append(ctx context.Context, run contracts.RunID, profile contracts.ProfileID,
	turn contracts.TurnID, eventType string, seq int) error {
	_, err := l.journal.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-%s-%s-%d", turn, eventType, seq)),
		EventType: eventType, RunID: run, EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "loop", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: profile, AttemptNo: 1,
		Payload: json.RawMessage(fmt.Sprintf(`{"turn_id":%q}`, turn)), PayloadHash: "recomputed",
	})
	if err != nil {
		return fmt.Errorf("loop: journal %s: %w", eventType, err)
	}
	return nil
}

// observeGuard enforces P0.1 provenance at the observe seam: a tool
// output block may carry only TOOL_TRUSTED or UNTRUSTED_EXTERNAL trust
// (laundering to SYSTEM/USER is rejected), and its lineage MUST name the
// producing tool call.
func observeGuard(call contracts.ToolCall, blocks []contracts.ContextBlock) error {
	for _, b := range blocks {
		if b.Trust != contracts.TrustToolTrusted && b.Trust != contracts.TrustUntrustedExternal {
			return fmt.Errorf("loop: observation %s launders trust to %v (rejected — P0.1)", b.BlockID, b.Trust)
		}
		found := false
		for _, l := range b.Lineage {
			if l == string(call.ToolCallID) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("loop: observation %s carries no lineage to its tool call (rejected — P0.1)", b.BlockID)
		}
	}
	return nil
}

// errorObservation packs a tool failure into a TOOL_TRUSTED observation —
// failure is data the planner must see, never a hidden crash.
func errorObservation(call contracts.ToolCall, seq int, toolErr error) (contracts.ContextBlock, error) {
	content := fmt.Sprintf("tool %s failed: %v", call.ToolID, toolErr)
	sum := sha256.Sum256([]byte(content))
	return contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID(fmt.Sprintf("obs-err-%s-%d", call.ToolCallID, seq)),
		Kind:    "tool_error", Content: &content, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://loop/observation", Producer: "loop",
		Trust: contracts.TrustToolTrusted, Sensitivity: contracts.Sensitivity(1),
		Lineage: []string{string(call.ToolCallID)}, ObservedAt: time.Now().UTC(),
	})
}

// RunTurn executes ONE turn to a final answer or a failure. Every path
// lands a terminal turn event in the journal.
func (l *Loop) RunTurn(ctx context.Context, turn contracts.TurnID, run contracts.RunID,
	profile contracts.ProfileID, initial []contracts.ContextBlock) (string, error) {
	if !turn.Valid() || !run.Valid() || !profile.Valid() {
		return "", fmt.Errorf("loop: turn, run and profile ids are required (fail closed)")
	}
	seq := 0
	next := func() int { seq++; return seq }
	if err := l.append(ctx, run, profile, turn, machine.EvTurnCreated, next()); err != nil {
		return "", err
	}
	if err := l.append(ctx, run, profile, turn, machine.EvTurnStarted, next()); err != nil {
		return "", err
	}
	failTurn := func(cause error) (string, error) {
		if jerr := l.append(ctx, run, profile, turn, machine.EvTurnFailed, next()); jerr != nil {
			return "", fmt.Errorf("%w (and journal: %v)", cause, jerr)
		}
		return "", cause
	}
	blocks := append([]contracts.ContextBlock{}, initial...)
	identical := map[string]int{}
	for iter := 0; iter < l.maxIters; iter++ {
		// Trust fencing is proven on every iteration: the context must
		// assemble (untrusted content stays fenced — assembler owner).
		if _, err := assembler.Base(blocks); err != nil {
			return failTurn(fmt.Errorf("loop: context does not assemble: %w", err))
		}
		action, err := l.planner.Plan(ctx, blocks)
		if err != nil {
			return failTurn(fmt.Errorf("loop: planner: %w", err))
		}
		switch {
		case action.Final != nil && action.Call == nil:
			if err := l.append(ctx, run, profile, turn, machine.EvTurnSucceeded, next()); err != nil {
				return "", err
			}
			return *action.Final, nil
		case action.Call != nil && action.Final == nil:
			// continue below
		default:
			return failTurn(fmt.Errorf("loop: planner action must be exactly one of final/call (fail closed)"))
		}
		call := *action.Call
		// Identical-call stuck-breaker (interactive turns only — C3).
		if l.policy == PolicyInteractive {
			sum := sha256.Sum256(append([]byte(call.ToolID+"\x00"), call.Arguments...))
			key := hex.EncodeToString(sum[:])
			identical[key]++
			if identical[key] >= l.breakerN {
				return failTurn(fmt.Errorf("loop: stuck — identical call %s repeated %d times in one interactive turn", call.ToolID, identical[key]))
			}
		}
		// One attempt per call, granted by S7 and journaled.
		op := contracts.OperationID(fmt.Sprintf("%s/%s", turn, call.ToolCallID))
		if err := l.append(ctx, run, profile, turn, machine.EvAttemptPlanned, next()); err != nil {
			return "", err
		}
		grant, err := l.grants.Issue(op, "local")
		if err != nil {
			return failTurn(fmt.Errorf("loop: grant: %w", err))
		}
		if err := l.append(ctx, run, profile, turn, machine.EvAttemptAuthorized, next()); err != nil {
			return "", err
		}
		if err := l.append(ctx, run, profile, turn, machine.EvAttemptStarted, next()); err != nil {
			return "", err
		}
		out, toolErr := l.path.RunTool(ctx, call, grant)
		if toolErr != nil {
			// Failure is an OBSERVATION; the turn continues.
			if err := l.append(ctx, run, profile, turn, machine.EvAttemptFailed, next()); err != nil {
				return "", err
			}
			obs, oerr := errorObservation(call, seq, toolErr)
			if oerr != nil {
				return failTurn(fmt.Errorf("loop: observation: %w", oerr))
			}
			blocks = append(blocks, obs)
			continue
		}
		if err := observeGuard(call, out.Output); err != nil {
			// Laundered provenance NEVER enters the context.
			if jerr := l.append(ctx, run, profile, turn, machine.EvAttemptFailed, next()); jerr != nil {
				return "", jerr
			}
			return failTurn(err)
		}
		if err := l.append(ctx, run, profile, turn, machine.EvAttemptSucceeded, next()); err != nil {
			return "", err
		}
		blocks = append(blocks, out.Output...)
	}
	return failTurn(fmt.Errorf("loop: %d iterations without a final answer (fail closed)", l.maxIters))
}

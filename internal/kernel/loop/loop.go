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
	"errors"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/assembler"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/security/redact"
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
// Suspender commits the durable HITL suspension (T24, B6) and returns
// the challenge summary the user sees.
type Suspender func(ctx context.Context, turn contracts.TurnID, run contracts.RunID, call contracts.ToolCall, blocks []contracts.ContextBlock) (string, error)

type Loop struct {
	planner   Planner
	path      *effectpath.EffectPath
	grants    *s7min.Authority
	journal   *journal.Journal
	redactor  redact.Redactor
	policy    Policy
	maxIters  int
	breakerN  int
	suspender Suspender
	// resumeTag namespaces event ids per resume cycle (Phase-5-r3 codex
	// #2): empty for the first incarnation.
	resumeTag string
}

// SetSuspender wires the durable HITL owner: with it, an unapproved ASK
// SUSPENDS the turn (TurnSuspended committed by the owner, loop exits,
// the user gets the challenge) instead of failing it.
func (l *Loop) SetSuspender(s Suspender) { l.suspender = s }

func New(p Planner, path *effectpath.EffectPath, grants *s7min.Authority, j *journal.Journal,
	r redact.Redactor, policy Policy, maxIters, breakerN int) (*Loop, error) {
	if p == nil || path == nil || grants == nil || j == nil || r == nil {
		return nil, fmt.Errorf("loop: all collaborators are required (fail closed)")
	}
	if policy != PolicyInteractive && policy != PolicyContinuous {
		return nil, fmt.Errorf("loop: unknown policy %d (fail closed)", policy)
	}
	if maxIters <= 0 || breakerN <= 1 {
		return nil, fmt.Errorf("loop: iteration and breaker bounds must be positive (fail closed)")
	}
	return &Loop{planner: p, path: path, grants: grants, journal: j, redactor: r,
		policy: policy, maxIters: maxIters, breakerN: breakerN}, nil
}

// append journals one lifecycle event. Attempt events carry their
// ToolCallID so a multi-call turn REPLAYS per attempt (Phase-2 codex #7:
// an undifferentiated attempt stream folds illegally past the first call).
func (l *Loop) append(ctx context.Context, run contracts.RunID, profile contracts.ProfileID,
	turn contracts.TurnID, eventType string, seq int, toolCall *contracts.ToolCallID) error {
	return l.appendPayload(ctx, run, profile, turn, eventType, seq, toolCall,
		json.RawMessage(fmt.Sprintf(`{"turn_id":%q}`, turn)))
}

// appendPayload journals one lifecycle event with an explicit payload.
// Resume incarnations carry l.resumeTag in the event id so a turn that
// suspends and resumes MORE than once can never collide with an earlier
// cycle's ids (Phase-5-r3 codex #2).
func (l *Loop) appendPayload(ctx context.Context, run contracts.RunID, profile contracts.ProfileID,
	turn contracts.TurnID, eventType string, seq int, toolCall *contracts.ToolCallID, payload json.RawMessage) error {
	id := fmt.Sprintf("ev-%s-%s-%d", turn, eventType, seq)
	if l.resumeTag != "" {
		id = fmt.Sprintf("ev-%s-r%s-%s-%d", turn, l.resumeTag, eventType, seq)
	}
	_, err := l.journal.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(id),
		EventType: eventType, RunID: run, TurnID: &turn, ToolCallID: toolCall,
		EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "loop", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: profile, AttemptNo: 1,
		Payload: payload, PayloadHash: "recomputed",
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
		// Lying-metadata channel: a tool output claiming a reserved
		// producer identity is rejected (Phase-2 kilo #5).
		switch b.Producer {
		case "user", "system", "repl", "loop":
			return fmt.Errorf("loop: observation %s claims reserved producer %q (rejected)", b.BlockID, b.Producer)
		}
	}
	return nil
}

// errorObservation packs a tool failure into an UNTRUSTED observation —
// failure is data the planner must see, but error text is externally
// influenced (subprocess stderr, upstream bodies) and goes to the model
// ONLY inside the untrusted fence (Phase-2 codex #8: TOOL_TRUSTED error
// text was an injection lane). Content is bounded.
// Error text passes the KNOWN-REF redactor before any model/UI boundary
// (Phase-2-r2 codex #9: a credential inside subprocess stderr would
// otherwise reach the external provider verbatim).
func errorObservation(r redact.Redactor, call contracts.ToolCall, seq int, toolErr error) (contracts.ContextBlock, error) {
	msg := RedactText(r, toolErr.Error())
	if len(msg) > 1024 {
		msg = msg[:1024] + "…(truncated)"
	}
	content := fmt.Sprintf("tool %s failed: %s", call.ToolID, msg)
	sum := sha256.Sum256([]byte(content))
	return contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID(fmt.Sprintf("obs-err-%s-%d", call.ToolCallID, seq)),
		Kind:    "tool_error", Content: &content, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://loop/observation", Producer: "loop",
		Trust: contracts.TrustUntrustedExternal, Sensitivity: contracts.SensitivityInternal,
		Lineage: []string{string(call.ToolCallID)}, ObservedAt: time.Now().UTC(),
	})
}

// RedactText applies the known-ref redactor to one plain string (the
// redactor's native unit is a JSON document).
func RedactText(r redact.Redactor, s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "(unrenderable)"
	}
	red, err := r.Redact(b)
	if err != nil {
		return "(redaction failed — content withheld)"
	}
	var out string
	if json.Unmarshal(red, &out) != nil {
		return "(unrenderable)"
	}
	return out
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
	if err := l.append(ctx, run, profile, turn, machine.EvTurnCreated, next(), nil); err != nil {
		return "", err
	}
	if err := l.append(ctx, run, profile, turn, machine.EvTurnStarted, next(), nil); err != nil {
		return "", err
	}
	return l.iterate(ctx, turn, run, profile, initial, next)
}

// ResumeTurn re-enters a SUSPENDED turn after its approval decision (B6
// rehydration, Phase-5-r2 codex #2): turn.resumed is journaled, then the
// planner continues from the supplied blocks (the approved tool's
// observation) to a real final. Event seq starts high so resume event ids
// can never collide with the original incarnation's.
func (l *Loop) ResumeTurn(ctx context.Context, turn contracts.TurnID, run contracts.RunID,
	profile contracts.ProfileID, tag string, blocks []contracts.ContextBlock) (string, error) {
	if !turn.Valid() || !run.Valid() || !profile.Valid() || tag == "" {
		return "", fmt.Errorf("loop: turn, run, profile and a unique resume tag are required (fail closed)")
	}
	l.resumeTag = tag
	seq := 0
	next := func() int { seq++; return seq }
	if err := l.append(ctx, run, profile, turn, machine.EvTurnResumed, next(), nil); err != nil {
		return "", err
	}
	return l.iterate(ctx, turn, run, profile, blocks, next)
}

// iterate is the shared plan→act→observe core (turn already RUNNING).
func (l *Loop) iterate(ctx context.Context, turn contracts.TurnID, run contracts.RunID,
	profile contracts.ProfileID, initial []contracts.ContextBlock, next func() int) (string, error) {
	failTurn := func(cause error) (string, error) {
		if jerr := l.append(ctx, run, profile, turn, machine.EvTurnFailed, next(), nil); jerr != nil {
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
			// The final rides the succeeded event (REDACTED) so a crash
			// between turn completion and channel delivery can recover the
			// real outcome on replay (Phase-5-r3 codex #3).
			finalPayload, perr := json.Marshal(map[string]string{
				"turn_id": string(turn), "final": RedactText(l.redactor, *action.Final)})
			if perr != nil {
				return failTurn(fmt.Errorf("loop: final payload: %w", perr))
			}
			if err := l.appendPayload(ctx, run, profile, turn, machine.EvTurnSucceeded, next(), nil, finalPayload); err != nil {
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
		// One attempt per call, granted by S7 (target-BOUND to this exact
		// call) and journaled FROM THE AUTHORITY'S ACTUAL STATE — the fold
		// never records a physical attempt that never ran (Phase-2 codex
		// #6 / kilo #2).
		tcID := call.ToolCallID
		op := contracts.OperationID(fmt.Sprintf("%s/%s", turn, tcID))
		if err := l.append(ctx, run, profile, turn, machine.EvAttemptPlanned, next(), &tcID); err != nil {
			return "", err
		}
		grant, err := l.grants.Issue(op, effectpath.ToolTarget(call))
		if err != nil {
			return failTurn(fmt.Errorf("loop: grant: %w", err))
		}
		if err := l.append(ctx, run, profile, turn, machine.EvAttemptAuthorized, next(), &tcID); err != nil {
			return "", err
		}
		// STARTED is appended by the effect path AFTER consume and BEFORE
		// dispatch (Phase-2-r2 codex #7): the durable record proves
		// consumption preceded any effect — a crash mid-dispatch replays
		// as RUNNING and goes to reconciliation, never silently AUTHORIZED.
		l.path.SetStartObserver(func(obsCtx context.Context, obsOp contracts.OperationID) error {
			return l.append(obsCtx, run, profile, turn, machine.EvAttemptStarted, next(), &tcID)
		})
		out, toolErr := l.path.RunTool(ctx, call, grant)
		l.path.SetStartObserver(nil)
		// Journal the attempt terminal from the AUTHORITY state.
		st, _ := l.grants.State(op)
		switch st {
		case contracts.AttemptAuthorized:
			// Pre-execution refusal: the grant was never consumed —
			// nothing physically ran. Cancel the operation and record the
			// honest terminal.
			l.grants.Cancel(op)
			if jerr := l.append(ctx, run, profile, turn, machine.EvAttemptCancelled, next(), &tcID); jerr != nil {
				return "", jerr
			}
		case contracts.AttemptSucceeded, contracts.AttemptFailed, contracts.AttemptUnknown:
			terminal := map[contracts.AttemptState]string{
				contracts.AttemptSucceeded: machine.EvAttemptSucceeded,
				contracts.AttemptFailed:    machine.EvAttemptFailed,
				contracts.AttemptUnknown:   machine.EvAttemptLost,
			}[st]
			// topknot ceiling: if THIS append fails, the durable stream
			// already holds STARTED — replay folds RUNNING and the crash-
			// recovery owner reconciles it; the turn still fails loudly.
			if jerr := l.append(ctx, run, profile, turn, terminal, next(), &tcID); jerr != nil {
				return failTurn(fmt.Errorf("loop: terminal append failed — attempt %s stays RUNNING in the journal for reconciliation: %w", op, jerr))
			}
		case contracts.AttemptCancelled:
			if jerr := l.append(ctx, run, profile, turn, machine.EvAttemptCancelled, next(), &tcID); jerr != nil {
				return "", jerr
			}
		}
		if errors.Is(toolErr, effectpath.ErrNeedsApproval) {
			// HITL gate: the USER must act — never packed as an
			// observation the model could talk itself past. With the T24
			// suspender wired, the owner commits TurnSuspended durably
			// (B6), the loop EXITS, and the turn's outcome IS the
			// challenge shown to the user; without it (interactive
			// sessions) the gate surfaces as a failure.
			if l.suspender != nil {
				summary, serr := l.suspender(ctx, turn, run, call, blocks)
				if serr != nil {
					return failTurn(fmt.Errorf("loop: suspension failed: %w", errors.Join(toolErr, serr)))
				}
				// The turn is SUSPENDED — recording it SUCCEEDED would
				// lie to recovery while the effect still awaits approval
				// (Phase-5-r2 codex #4). ResumeTurn re-enters it.
				if err := l.append(ctx, run, profile, turn, machine.EvTurnSuspended, next(), nil); err != nil {
					return "", err
				}
				return summary, nil
			}
			return failTurn(fmt.Errorf("loop: tool %s: %w", call.ToolID, toolErr))
		}
		if errors.Is(toolErr, effectpath.ErrEffectUnknown) {
			// E9: UNKNOWN reconciles — the turn STOPS; no blind
			// continuation past a possibly-committed effect.
			return failTurn(fmt.Errorf("loop: tool %s: %w", call.ToolID, toolErr))
		}
		if toolErr != nil {
			// Failure is an OBSERVATION; the turn continues.
			obs, oerr := errorObservation(l.redactor, call, next(), toolErr)
			if oerr != nil {
				return failTurn(fmt.Errorf("loop: observation: %w", oerr))
			}
			blocks = append(blocks, obs)
			continue
		}
		if err := observeGuard(call, out.Output); err != nil {
			// Laundered provenance NEVER enters the context.
			return failTurn(err)
		}
		blocks = append(blocks, out.Output...)
	}
	return failTurn(fmt.Errorf("loop: %d iterations without a final answer (fail closed)", l.maxIters))
}

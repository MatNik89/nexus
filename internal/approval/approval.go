//go:build linux

// Package approval is the durable remote-HITL owner (T24, HARDQ B6/C4/B3;
// SPEC P1.4/P1.6-HITL): DecisionAsk commits TurnSuspended + an
// ApprovalChallenge to the journal and the loop EXITS — never an
// in-memory wait. The challenge is exact-intent (a hash over canonical
// tool + args + schema + effect + kind + target + ProfileID), expiring
// and single-use; ApprovalReceived/ApprovalDenied append durably and the
// resumed turn rehydrates from the journal. Cross-profile invisibility is
// PHYSICAL (each store rides its own profile journal, B3) and enforced
// again at the API (an approval addressed to a foreign challenge id
// simply does not exist here).
package approval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
)

// DefaultChallengeTTL bounds how long an approval can stay answerable.
const DefaultChallengeTTL = 15 * time.Minute

// ErrApprovalReplay is the typed replayed-approval refusal.
var ErrApprovalReplay = errors.New("APPROVAL_REPLAY")

// Event types (closed).
const (
	EvTurnSuspended    = "approval.turn_suspended"
	EvApprovalReceived = "approval.received"
	EvApprovalDenied   = "approval.denied"
	EvApprovalConsumed = "approval.consumed"
	// EvResumeCompleted closes the resume: without it a CONSUMED
	// challenge is an E9 UNKNOWN (the effect may or may not have run) and
	// the startup scan surfaces it for USER reconciliation — never a
	// blind retry (Phase-5-r3 kilo #1).
	EvResumeCompleted = "approval.resume_completed"
)

// Challenge is what the approver sees.
type Challenge struct {
	ChallengeID string
	Summary     string
}

// effectHash delegates to THE canonical exact-intent digest (Phase-5
// kilo #4: two divergent hashes would silently self-invalidate approvals
// once the durable store feeds the effect path). topknot ceiling (C4):
// (device,inode) binding for destructive FS ops lands with the first
// direct-FS destructive tool — P0's only exec surface is the sandboxed
// T26 path; trigger: that tool's owner.
func effectHash(c contracts.ToolCall) string { return effectpath.EffectHash(c) }

// --- payloads (closed) ---

type suspendedPayload struct {
	ChallengeID string `json:"challenge_id"`
	TurnID      string `json:"turn_id"`
	RunID       string `json:"run_id"`
	EffectHash  string `json:"effect_hash"`
	Summary     string `json:"summary"`
	ExpiresUnix int64  `json:"expires_unix"`
	// ExpectedSource binds WHO may decide: the originating channel
	// identity (Phase-5 codex #5 — a foreign chat can never approve).
	ExpectedSource string `json:"expected_source"`
	// Call is the canonical ToolCall JSON — resume re-executes EXACTLY it.
	Call json.RawMessage `json:"call"`
	// Context is the suspended turn's live context-block slice — resume
	// REHYDRATES the original conversation, not a synthetic stub
	// (Phase-5-r3 codex #1).
	Context json.RawMessage `json:"context"`
}

type decisionPayload struct {
	ChallengeID string `json:"challenge_id"`
	Source      string `json:"source"`
}

type consumedPayload struct {
	ChallengeID string `json:"challenge_id"`
	EffectHash  string `json:"effect_hash"`
}

// Events returns the closed payload validators.
func Events() map[string]journal.PayloadValidator {
	decV := func(raw json.RawMessage) error {
		var p decisionPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.ChallengeID == "" || p.Source == "" {
			return fmt.Errorf("approval: a decision requires the challenge id and its source")
		}
		return nil
	}
	return map[string]journal.PayloadValidator{
		EvTurnSuspended: func(raw json.RawMessage) error {
			var p suspendedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ChallengeID == "" || p.TurnID == "" || p.RunID == "" || p.EffectHash == "" ||
				p.ExpiresUnix == 0 || p.ExpectedSource == "" || len(p.Call) == 0 || len(p.Context) == 0 {
				return fmt.Errorf("approval: suspension requires challenge, turn, run, effect hash, expiry, expected source, the call and its context")
			}
			return nil
		},
		EvApprovalReceived: decV, EvApprovalDenied: decV,
		EvApprovalConsumed: func(raw json.RawMessage) error {
			var p consumedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ChallengeID == "" || p.EffectHash == "" {
				return fmt.Errorf("approval: consumption requires the challenge id and effect hash")
			}
			return nil
		},
		EvResumeCompleted: func(raw json.RawMessage) error {
			var p decisionPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ChallengeID == "" {
				return fmt.Errorf("approval: resume completion requires the challenge id")
			}
			return nil
		},
	}
}

// Projection folds the challenge lifecycle in the append transaction
// (default-reject: replays and illegal jumps abort their own event).
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "approval" }
func (Projection) Version() int { return 2 }

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS appr_challenges (
			challenge_id TEXT PRIMARY KEY,
			turn_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			effect_hash TEXT NOT NULL,
			summary TEXT NOT NULL,
			expires_unix INTEGER NOT NULL,
			expected_source TEXT NOT NULL,
			call_json TEXT NOT NULL,
			context_json TEXT NOT NULL,
			status TEXT NOT NULL, -- PENDING | APPROVED | DENIED | CONSUMED
			decided_by TEXT NOT NULL DEFAULT '',
			resume_done INTEGER NOT NULL DEFAULT 0,
			created INTEGER NOT NULL
		);
	`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS appr_challenges`)
	return err
}

func (Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	raw := ev.Envelope.Payload
	switch ev.Envelope.EventType {
	case EvTurnSuspended:
		var p suspendedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO appr_challenges
			(challenge_id, turn_id, run_id, effect_hash, summary, expires_unix, expected_source, call_json, context_json, status, created)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			p.ChallengeID, p.TurnID, p.RunID, p.EffectHash, p.Summary, p.ExpiresUnix,
			p.ExpectedSource, string(p.Call), string(p.Context), "PENDING", int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("approval: challenge insert: %w", err)
		}
	case EvApprovalReceived:
		var p decisionPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// Expiry is enforced HERE, in the same transaction as the append,
		// against the EVENT-OWNED decision time — a pre-check race can no
		// longer commit a decision past expiry (Phase-5-r2 codex #5).
		res, err := tx.Exec(`UPDATE appr_challenges SET status='APPROVED', decided_by=?
			WHERE challenge_id=? AND status='PENDING' AND expected_source=? AND expires_unix > ?`,
			p.Source, p.ChallengeID, p.Source, ev.Envelope.EmittedAt.Unix())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("approval: challenge is not PENDING (unknown, decided, expired, or replayed): %w", ErrApprovalReplay)
		}
	case EvApprovalDenied:
		var p decisionPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE appr_challenges SET status='DENIED', decided_by=?
			WHERE challenge_id=? AND status='PENDING' AND expected_source=? AND expires_unix > ?`,
			p.Source, p.ChallengeID, p.Source, ev.Envelope.EmittedAt.Unix())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("approval: challenge is not PENDING (unknown or decided): %w", ErrApprovalReplay)
		}
	case EvApprovalConsumed:
		var p consumedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// Single-use, exact-intent: consumption requires APPROVED and the
		// MATCHING effect hash — a tampered call aborts here.
		res, err := tx.Exec(`UPDATE appr_challenges SET status='CONSUMED'
			WHERE challenge_id=? AND status='APPROVED' AND effect_hash=? AND expires_unix > ?`,
			p.ChallengeID, p.EffectHash, ev.Envelope.EmittedAt.Unix())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("approval: consumption does not match an approved exact effect (fail closed)")
		}
	case EvResumeCompleted:
		var p decisionPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE appr_challenges SET resume_done=1
			WHERE challenge_id=? AND status='CONSUMED' AND resume_done=0`, p.ChallengeID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("approval: resume completion for a challenge that is not CONSUMED-and-open (fail closed)")
		}
	}
	return nil
}

// Store is the HITL facade over the profile's ONE journal.
type Store struct {
	j     *journal.Journal
	clock clockid.Clock
	seq   atomic.Uint64
}

func NewStore(j *journal.Journal, c clockid.Clock) (*Store, error) {
	if j == nil || c == nil {
		return nil, fmt.Errorf("approval: a journal and a clock are required (fail closed)")
	}
	return &Store{j: j, clock: c}, nil
}

func (s *Store) params(eventType string, payload any) (contracts.EnvelopeParams, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contracts.EnvelopeParams{}, err
	}
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-appr-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), s.seq.Add(1))),
		EventType: eventType, RunID: "run-approval", EmittedAt: s.clock.Now(),
		ActorType: contracts.ActorSystem, ActorID: "approval", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: s.j.Profile(), AttemptNo: 1,
		Payload: raw, PayloadHash: "recomputed",
	}, nil
}

// Suspend commits TurnSuspended + the exact-intent challenge in ONE
// durable event; the caller (loop) EXITS afterwards (B6 — no in-memory
// wait). expectedSource binds WHO may decide (the originating channel
// identity — Phase-5 codex #5). The challenge id is RANDOM (codex #12:
// a hash-prefix id coupled identity to intent and invited collision
// DoS); consumption matches by the full effect hash. A payload the
// journal redactor would rewrite is REFUSED — a secret in tool args
// must never reach an approver's phone (codex #8).
func (s *Store) Suspend(ctx context.Context, turn contracts.TurnID, run contracts.RunID,
	c contracts.ToolCall, expectedSource string, blocks []contracts.ContextBlock) (Challenge, error) {
	if !turn.Valid() || !run.Valid() || expectedSource == "" {
		return Challenge{}, fmt.Errorf("approval: turn, run and the expected decision source are required (fail closed)")
	}
	if err := c.Validate(); err != nil {
		return Challenge{}, fmt.Errorf("approval: invalid call (fail closed): %w", err)
	}
	if c.ProfileID != s.j.Profile() {
		return Challenge{}, fmt.Errorf("approval: call profile does not match this journal's binding (fail closed, B3)")
	}
	hash := effectHash(c)
	rb := make([]byte, 12)
	if _, err := rand.Read(rb); err != nil {
		return Challenge{}, err
	}
	id := "ch-" + hex.EncodeToString(rb)
	callJSON, err := json.Marshal(c)
	if err != nil {
		return Challenge{}, err
	}
	if len(blocks) == 0 {
		return Challenge{}, fmt.Errorf("approval: suspension requires the turn context (fail closed — resume must rehydrate it)")
	}
	contextJSON, err := json.Marshal(blocks)
	if err != nil {
		return Challenge{}, err
	}
	summary := fmt.Sprintf("APPROVAL NEEDED [%s]: tool %s with args %s (profile %s). Reply approve %s or deny %s.",
		id, c.ToolID, string(c.Arguments), c.ProfileID, id, id)
	payload := suspendedPayload{
		ChallengeID: id, TurnID: string(turn), RunID: string(run),
		EffectHash: hash, Summary: summary,
		ExpiresUnix:    s.clock.Now().Add(DefaultChallengeTTL).Unix(),
		ExpectedSource: expectedSource, Call: callJSON, Context: contextJSON}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return Challenge{}, err
	}
	rewrites, err := s.j.RedactorRewrites(rawPayload)
	if err != nil {
		return Challenge{}, err
	}
	if rewrites {
		return Challenge{}, fmt.Errorf("approval: the challenge would expose a known secret to the approver — refused (fail closed)")
	}
	p, err := s.params(EvTurnSuspended, payload)
	if err != nil {
		return Challenge{}, err
	}
	if _, err := s.j.Append(ctx, p); err != nil {
		return Challenge{}, err
	}
	return Challenge{ChallengeID: id, Summary: summary}, nil
}

// RetryChallenge is the E9 reconcile recipe (Phase-5-r4 codex #1): it
// closes the CONSUMED-and-open challenge AND issues the fresh replacement
// in ONE AppendBatch — a crash can never leave both the old reconcile
// item open and a new challenge live, so the effect can never accumulate
// two consumable authorizations.
func (s *Store) RetryChallenge(ctx context.Context, oldID, source string) (Challenge, error) {
	turn, run, c, blocks, err := s.ConsumedCall(ctx, oldID, source)
	if err != nil {
		return Challenge{}, err
	}
	hash := effectHash(c)
	rb := make([]byte, 12)
	if _, err := rand.Read(rb); err != nil {
		return Challenge{}, err
	}
	id := "ch-" + hex.EncodeToString(rb)
	callJSON, err := json.Marshal(c)
	if err != nil {
		return Challenge{}, err
	}
	contextJSON := []byte("")
	if blocks != nil {
		if contextJSON, err = json.Marshal(blocks); err != nil {
			return Challenge{}, err
		}
	} else {
		// Legacy v1 suspension without context: carry an empty slice so
		// the v2 payload validator holds.
		contextJSON = []byte("[]")
	}
	summary := fmt.Sprintf("APPROVAL NEEDED [%s]: tool %s with args %s (profile %s). Reply approve %s or deny %s.",
		id, c.ToolID, string(c.Arguments), c.ProfileID, id, id)
	payload := suspendedPayload{
		ChallengeID: id, TurnID: string(turn), RunID: string(run),
		EffectHash: hash, Summary: summary,
		ExpiresUnix:    s.clock.Now().Add(DefaultChallengeTTL).Unix(),
		ExpectedSource: source, Call: callJSON, Context: contextJSON}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return Challenge{}, err
	}
	rewrites, err := s.j.RedactorRewrites(rawPayload)
	if err != nil {
		return Challenge{}, err
	}
	if rewrites {
		return Challenge{}, fmt.Errorf("approval: the challenge would expose a known secret to the approver — refused (fail closed)")
	}
	closeP, err := s.params(EvResumeCompleted, decisionPayload{ChallengeID: oldID, Source: "retry"})
	if err != nil {
		return Challenge{}, err
	}
	suspendP, err := s.params(EvTurnSuspended, payload)
	if err != nil {
		return Challenge{}, err
	}
	if _, err := s.j.AppendBatch(ctx, []contracts.EnvelopeParams{closeP, suspendP}); err != nil {
		return Challenge{}, err
	}
	return Challenge{ChallengeID: id, Summary: summary}, nil
}

// Pending lists live (unexpired, undecided) challenges.
func (s *Store) Pending(ctx context.Context) ([]Challenge, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT challenge_id, summary FROM appr_challenges WHERE status='PENDING' AND expires_unix > ? ORDER BY created`,
		s.clock.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Challenge
	for rows.Next() {
		var c Challenge
		if err := rows.Scan(&c.ChallengeID, &c.Summary); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) challengeRow(ctx context.Context, id string) (status, effect string, expires int64, err error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT status, effect_hash, expires_unix FROM appr_challenges WHERE challenge_id=?`, id)
	if err != nil {
		return "", "", 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", 0, fmt.Errorf("approval: unknown challenge (fail closed)")
	}
	if err := rows.Scan(&status, &effect, &expires); err != nil {
		return "", "", 0, err
	}
	return status, effect, expires, rows.Err()
}

func (s *Store) decide(ctx context.Context, eventType, id, source string) error {
	if source == "" {
		return fmt.Errorf("approval: a decision source is required (fail closed)")
	}
	status, _, expires, err := s.challengeRow(ctx, id)
	if err != nil {
		return err
	}
	if status != "PENDING" {
		return fmt.Errorf("approval: challenge already %s: %w", status, ErrApprovalReplay)
	}
	if s.clock.Now().Unix() >= expires {
		return fmt.Errorf("approval: challenge expired (fail closed)")
	}
	p, err := s.params(eventType, decisionPayload{ChallengeID: id, Source: source})
	if err != nil {
		return err
	}
	_, err = s.j.Append(ctx, p)
	return err
}

// Approve appends the durable ApprovalReceived (source = the gesture's
// channel identity).
func (s *Store) Approve(ctx context.Context, id, source string) error {
	return s.decide(ctx, EvApprovalReceived, id, source)
}

// Deny appends the durable denial; the challenge is dead.
func (s *Store) Deny(ctx context.Context, id, source string) error {
	return s.decide(ctx, EvApprovalDenied, id, source)
}

// ConsumeApproval burns the approval for THIS exact call (single-use,
// exact-intent, UNEXPIRED — codex #7: an approval must not stay
// consumable forever). Lookup is by the FULL effect hash.
func (s *Store) ConsumeApproval(ctx context.Context, c contracts.ToolCall) error {
	hash := effectHash(c)
	rows, err := s.j.QueryProjection(ctx,
		`SELECT challenge_id, expires_unix FROM appr_challenges WHERE effect_hash=? AND status='APPROVED'`, hash)
	if err != nil {
		return err
	}
	var id string
	var expires int64
	found := false
	if rows.Next() {
		found = true
		if err := rows.Scan(&id, &expires); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("approval: no approved challenge matches this exact effect (fail closed)")
	}
	if s.clock.Now().Unix() >= expires {
		return fmt.Errorf("approval: the approval expired before consumption (fail closed)")
	}
	p, err := s.params(EvApprovalConsumed, consumedPayload{ChallengeID: id, EffectHash: hash})
	if err != nil {
		return err
	}
	_, err = s.j.Append(ctx, p)
	return err
}

// ApprovedCall returns the canonical ToolCall of an APPROVED challenge —
// resume re-executes EXACTLY it.
func (s *Store) ApprovedCall(ctx context.Context, id string) (contracts.ToolCall, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT call_json FROM appr_challenges WHERE challenge_id=? AND status='APPROVED'`, id)
	if err != nil {
		return contracts.ToolCall{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return contracts.ToolCall{}, fmt.Errorf("approval: no approved challenge %q (fail closed)", id)
	}
	var raw string
	if err := rows.Scan(&raw); err != nil {
		return contracts.ToolCall{}, err
	}
	var c contracts.ToolCall
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return contracts.ToolCall{}, err
	}
	return c, nil
}

// SuspendedTurn rehydrates the suspended turn context for a challenge.
func (s *Store) SuspendedTurn(ctx context.Context, id string) (contracts.TurnID, contracts.RunID, bool, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT turn_id, run_id FROM appr_challenges WHERE challenge_id=?`, id)
	if err != nil {
		return "", "", false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", false, nil
	}
	var turn, run string
	if err := rows.Scan(&turn, &run); err != nil {
		return "", "", false, err
	}
	return contracts.TurnID(turn), contracts.RunID(run), true, rows.Err()
}

// ApprovedChallenge is one APPROVED-but-unconsumed challenge (startup
// resume scan, Phase-5-r2 codex #2: a crash between the approval receipt
// and the resume must not strand the action).
type ApprovedChallenge struct {
	ChallengeID    string
	ExpectedSource string
}

// Approved lists APPROVED, unconsumed, unexpired challenges.
func (s *Store) Approved(ctx context.Context) ([]ApprovedChallenge, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT challenge_id, expected_source FROM appr_challenges WHERE status='APPROVED' AND expires_unix > ? ORDER BY created`,
		s.clock.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovedChallenge
	for rows.Next() {
		var c ApprovedChallenge
		if err := rows.Scan(&c.ChallengeID, &c.ExpectedSource); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SuspendedCall returns the suspended turn/run ids AND canonical call of
// an APPROVED challenge — resume rehydrates the ORIGINAL turn (B6).
func (s *Store) SuspendedCall(ctx context.Context, id, source string) (contracts.TurnID, contracts.RunID, contracts.ToolCall, []contracts.ContextBlock, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT turn_id, run_id, call_json, context_json FROM appr_challenges WHERE challenge_id=? AND status='APPROVED' AND expected_source=?`, id, source)
	if err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", contracts.ToolCall{}, nil, fmt.Errorf("approval: no approved challenge %q (fail closed)", id)
	}
	var turn, run, raw, rawCtx string
	if err := rows.Scan(&turn, &run, &raw, &rawCtx); err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	var c contracts.ToolCall
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	blocks, err := parseContextJSON(rawCtx)
	if err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	return contracts.TurnID(turn), contracts.RunID(run), c, blocks, nil
}

// parseContextJSON restores a suspension's context blocks. A row folded
// from a PRE-v2 suspension event has no context (Phase-5-r4 codex #2):
// it degrades to an observation-only resume (nil blocks) instead of
// stranding an approvable challenge behind a JSON error.
func parseContextJSON(raw string) ([]contracts.ContextBlock, error) {
	if raw == "" {
		return nil, nil // legacy v1 suspension: no persisted context
	}
	var blocks []contracts.ContextBlock
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		return nil, fmt.Errorf("approval: suspension context corrupt (fail closed): %w", err)
	}
	return blocks, nil
}

// MarkResumeCompleted closes a CONSUMED challenge's resume — the E9
// reconcile scan stops surfacing it (Phase-5-r3 kilo #1).
func (s *Store) MarkResumeCompleted(ctx context.Context, id string) error {
	p, err := s.params(EvResumeCompleted, decisionPayload{ChallengeID: id, Source: "resume"})
	if err != nil {
		return err
	}
	_, err = s.j.Append(ctx, p)
	return err
}

// ConsumedUnfinished lists CONSUMED challenges whose resume never
// completed: the effect may or may not have run (E9 UNKNOWN) — the owner
// reconciles with the USER; never a blind retry.
func (s *Store) ConsumedUnfinished(ctx context.Context) ([]ApprovedChallenge, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT challenge_id, expected_source FROM appr_challenges WHERE status='CONSUMED' AND resume_done=0 ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovedChallenge
	for rows.Next() {
		var c ApprovedChallenge
		if err := rows.Scan(&c.ChallengeID, &c.ExpectedSource); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConsumedCall returns the canonical call+context of a CONSUMED-but-open
// challenge, source-bound — the retry command re-issues a FRESH challenge
// over the same exact intent after the user confirms (E9 reconcile).
func (s *Store) ConsumedCall(ctx context.Context, id, source string) (contracts.TurnID, contracts.RunID, contracts.ToolCall, []contracts.ContextBlock, error) {
	rows, err := s.j.QueryProjection(ctx,
		`SELECT turn_id, run_id, call_json, context_json FROM appr_challenges WHERE challenge_id=? AND status='CONSUMED' AND resume_done=0 AND expected_source=?`, id, source)
	if err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", contracts.ToolCall{}, nil, fmt.Errorf("approval: no open consumed challenge %q (fail closed)", id)
	}
	var turn, run, raw, rawCtx string
	if err := rows.Scan(&turn, &run, &raw, &rawCtx); err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	var c contracts.ToolCall
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	blocks, err := parseContextJSON(rawCtx)
	if err != nil {
		return "", "", contracts.ToolCall{}, nil, err
	}
	return contracts.TurnID(turn), contracts.RunID(run), c, blocks, nil
}

//go:build linux

// Package s7 is the FULL S7 engine (HARNESS-SPEC P0.2; PLAN-AUDIT-FIXES
// Slice B1, owner decision 2026-09-08): the SOLE owner of retry, backoff,
// deadline and cancel. Every physical provider/tool/delivery/poll attempt
// presents a unique, single-use, expiring AttemptGrant issued here; only
// this package may ever issue a second grant for an operation (from
// FAILED_RETRYABLE), and it does so only inside the operation's Policy
// (attempt cap, deadline, backoff, retryable codes). Adapters, loops and
// planners hold NO retry loop: they ask Next or submit one Execute.
//
// State names map to SPEC P0.2 one-to-one: PLANNED=PENDING,
// AUTHORIZED=GRANTED, RUNNING, FAILED_RETRYABLE, FAILED=FAILED_TERMINAL,
// CANCELLED, UNKNOWN. Policy + the operation key together ARE the SPEC
// ExecutionPolicy (operation_id = the key; cancel_token_id = the key —
// Cancel(op) is the token).
//
// Two narratives, ONE story: the in-memory record (driven through the
// canonical machine.AttemptTable, default-reject) is the runtime authority;
// for Durable policies every transition is ALSO journaled as an s7.* event
// and folded into the s7_operations projection, which is the durable
// source — New rehydrates the in-memory record from it after a restart.
// Paired transitions (S7 + the owning component's companion event) commit
// in ONE journal.AppendBatch: S7 is the atomic committer, the journal's
// single actor stays the only sequencer.
package s7

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"reflect"
	"sync"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
)

// Grant carries the SPEC P0.2 MUST-fields. It is a bearer CAPABILITY for
// exactly one physical attempt; the authority's own records stay
// authoritative — a forged or tampered struct never authorizes.
type Grant struct {
	OperationID contracts.OperationID
	AttemptNo   int
	TargetID    contracts.TargetID
	IssuedAt    time.Time
	ExpiresAt   time.Time
	Nonce       string
}

// Outcome is the closed physical-attempt result set. The adapter's outcome
// is a PROPOSAL: S7 decides retryability from the operation's Policy.
type Outcome int

const (
	OutcomeSucceeded Outcome = iota + 1
	// OutcomeFailedRetryable proposes a retry; S7 lands FAILED_RETRYABLE only
	// when the code is in Policy.RetryableCodes and cap/deadline allow.
	OutcomeFailedRetryable
	OutcomeFailedTerminal
	// OutcomeUnknown: an effectful attempt without a valid commit receipt —
	// the attempt parks in UNKNOWN and is NEVER re-granted (E9); only
	// reconciliation may exit it.
	OutcomeUnknown
)

// ErrAttemptNotAuthorized is the SPEC P0.2 literal refusal.
var ErrAttemptNotAuthorized = errors.New("ATTEMPT_NOT_AUTHORIZED")

// ErrNotDue: the operation is retryable but its backoff has not elapsed
// (nothing committed). ErrExhausted: the attempt cap or deadline is spent
// and the operation has been durably landed FAILED (together with the
// owner's companion). Both wrap ErrAttemptNotAuthorized: no grant exists.
var (
	ErrNotDue    = fmt.Errorf("s7: next attempt not due yet: %w", ErrAttemptNotAuthorized)
	ErrExhausted = fmt.Errorf("s7: attempts exhausted or deadline passed (operation FAILED): %w", ErrAttemptNotAuthorized)
	// ErrNoBuilder: a Durable operation was driven without the mandatory
	// companion builder (fail closed — the paired commit would be broken).
	ErrNoBuilder = errors.New("s7: a durable operation requires a companion builder (fail closed)")
	// ErrCompanion: the companion is missing, foreign (Key != op) or supplied
	// to a non-durable operation.
	ErrCompanion = fmt.Errorf("s7: companion missing, foreign or not allowed: %w", ErrAttemptNotAuthorized)
	// ErrNotDurable wraps every failed durable append: the journal (substrate)
	// refused the paired batch — nothing transitioned, no grant exists.
	ErrNotDurable = errors.New("s7: durable append failed (substrate)")
)

// BackoffPolicy is exponential with optional full jitter: attempt n (1-based
// count of consumed attempts) waits min(Base*2^(n-1), Max) [* U(0,1)].
type BackoffPolicy struct {
	Base   time.Duration
	Max    time.Duration
	Jitter bool
}

// Policy is the SPEC P0.2 ExecutionPolicy minus the operation key (the
// key is supplied to Begin; cancel_token_id == the key).
type Policy struct {
	EffectClass     contracts.EffectClass
	MaxAttempts     int
	AttemptTimeout  time.Duration
	Deadline        time.Duration
	Backoff         BackoffPolicy
	RetryableCodes  []string
	FallbackTargets []contracts.TargetID
	IdempotencyKey  string
	// Durable: the operation outlives the process (delivery). Every
	// transition is journaled and paired transitions require a Companion.
	// Non-durable operations live one interactive turn / poll cycle and can
	// never be asked for a grant after a restart (honest in-memory).
	Durable bool
}

// knownCodes is the closed failure-code vocabulary; a policy naming an
// unknown code is a typo, not a policy (E11: unknown typed input is refused).
var knownCodes = map[string]bool{
	CodeTransportPreWire: true, CodeHTTP429: true, CodeHTTP4xx: true, CodeHTTP5xx: true,
	CodeTransportPostWrite: true, CodeMalformedReply: true, CodeLocalRefused: true,
	CodeInvalidGeneral: true, CodeCrashRecovered: true, CodeHTTP400Format: true,
	CodeReceiptNotDurable: true, CodeMutationRolledBack: true, CodeRollbackIncomplete: true,
}

// Validate rejects a policy that cannot be honoured or serialised.
func (p Policy) Validate() error {
	if p.MaxAttempts < 1 {
		return fmt.Errorf("s7 policy: MaxAttempts must be >= 1")
	}
	if !p.EffectClass.Valid() {
		return fmt.Errorf("s7 policy: an effect class is required")
	}
	if p.AttemptTimeout < 0 || p.Deadline < 0 || p.Backoff.Base < 0 || p.Backoff.Max < 0 {
		return fmt.Errorf("s7 policy: negative timing")
	}
	if p.Backoff.Max > 0 && p.Backoff.Base > p.Backoff.Max {
		return fmt.Errorf("s7 policy: backoff base exceeds max")
	}
	for _, c := range p.RetryableCodes {
		if !knownCodes[c] {
			return fmt.Errorf("s7 policy: unknown retryable code %q", c)
		}
	}
	for _, t := range p.FallbackTargets {
		if !t.Valid() {
			return fmt.Errorf("s7 policy: invalid fallback target")
		}
	}
	return nil
}

// canonical is the comparison/serialisation form (same identity ⇔ same bytes).
func (p Policy) canonical() ([]byte, error) { return marshalPolicy(p) }

func (p Policy) retryable(code string) bool {
	for _, c := range p.RetryableCodes {
		if c == code {
			return true
		}
	}
	return false
}

// The closed failure-code vocabulary shared by every adapter (Slice B2).
const (
	CodeTransportPreWire = "transport_prewire"
	CodeHTTP429          = "http_429"
	CodeHTTP4xx          = "http_4xx"
	// CodeHTTP400Format: a formatted first send rejected by the remote
	// parser — definite and FIXABLE (the next attempt carries plain text).
	CodeHTTP400Format      = "http_400_format"
	CodeHTTP5xx            = "http_5xx"
	CodeTransportPostWrite = "transport_postwrite"
	CodeMalformedReply     = "malformed_reply"
	CodeLocalRefused       = "local_refused"
	CodeInvalidGeneral     = "invalid_general"
	CodeCrashRecovered     = "crash_recovered"
	// CodeReceiptNotDurable: the shared E11 egress owner could not durably
	// record a dial receipt — substrate, terminal, never retried blind
	// (code-review CODE5 codex #1: this must be a canonical vocabulary
	// member so Report's closed-code check recognizes it directly, not
	// only via a caller-side Failure fallback).
	CodeReceiptNotDurable = "receipt_not_durable"
	// CodeMutationRolledBack: an irreversible multi-file mutation attempt
	// failed AFTER a real physical write began, but every already-written
	// file was verified restored to its captured before-state — the
	// workspace is definitely clean, so the SAME operation may safely
	// retry within its budget (PLAN-CODING-TRIO.md invariant 2's
	// state-transition binding: "a FULLY VERIFIED rollback to all-BEFORE
	// may report FailedRetryable"). Never used for an UNVERIFIED or
	// partial rollback — that lands UNKNOWN instead, never retried blind.
	CodeMutationRolledBack = "mutation_rolled_back"
	// CodeRollbackIncomplete: a governed restart-time rollback attempt
	// restored SOME but not ALL of a sealed bundle's write set back to
	// its captured before-images this pass (a mixed BEFORE/AFTER state
	// remains) — the underlying primitive (workspace.rollbackBundle) is
	// idempotent and re-verifies fresh identity on every call, so the
	// SAME rollback operation may safely retry within its budget
	// (PLAN-CODING-TRIO.md invariant 4 §2: "a mixed BEFORE/AFTER state
	// remaining -> rollback Reconcile(false) then Next on that SAME
	// rollback operation"). Never used for a FOREIGN or otherwise
	// unverifiable result — that lands UNKNOWN instead, never retried
	// blind, the same discipline CodeMutationRolledBack's own doc
	// comment already establishes for the forward-direction analogue.
	CodeRollbackIncomplete = "rollback_incomplete"
)

// Policy constants — the ONE place retry policy lives (no config knob;
// upgrade trigger: a second provider/target configured).
var (
	// PolicyTool: effectful tools never retry (unchanged P0 behaviour).
	PolicyTool = Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 1}
	// PolicyProvider: a chat completion is read-only (cost aside).
	PolicyProvider = Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 3, AttemptTimeout: 120 * time.Second,
		Deadline: 5 * time.Minute, Backoff: BackoffPolicy{Base: time.Second, Max: 4 * time.Second, Jitter: true},
		RetryableCodes: []string{CodeHTTP429, CodeHTTP5xx, CodeTransportPreWire}}
	// PolicyStructured: initial generation + EXACTLY one re-ask.
	PolicyStructured = Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 2, AttemptTimeout: 120 * time.Second,
		Deadline: 5 * time.Minute, RetryableCodes: []string{CodeInvalidGeneral}}
	// PolicyDelivery: durable at-least-once delivery (HARDQ B2).
	PolicyDelivery = Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 8, Deadline: 24 * time.Hour,
		Backoff:        BackoffPolicy{Base: 5 * time.Second, Max: 30 * time.Minute, Jitter: true},
		RetryableCodes: []string{CodeTransportPreWire, CodeHTTP429, CodeHTTP400Format}, Durable: true}
	// PolicyPoll: read-only polling iteration (getUpdates re-reads the same
	// durable offset; retrying advances no admission).
	PolicyPoll = Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 6,
		Backoff:        BackoffPolicy{Base: 2 * time.Second, Max: 5 * time.Minute, Jitter: true},
		RetryableCodes: []string{CodeTransportPreWire, CodeHTTP429, CodeHTTP5xx, CodeTransportPostWrite, CodeMalformedReply}}
	// PolicyControlRead: getMe.
	PolicyControlRead = Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 3,
		Backoff:        BackoffPolicy{Base: 2 * time.Second, Max: 30 * time.Second, Jitter: true},
		RetryableCodes: []string{CodeTransportPreWire, CodeHTTP429, CodeHTTP5xx, CodeTransportPostWrite, CodeMalformedReply}}
	// PolicyControlEffect: setMyCommands mutates the remote menu — an
	// ambiguous outcome is UNKNOWN (E9), never blindly retried.
	PolicyControlEffect = Policy{EffectClass: contracts.EffectReversible, MaxAttempts: 3,
		Backoff:        BackoffPolicy{Base: 2 * time.Second, Max: 30 * time.Second, Jitter: true},
		RetryableCodes: []string{CodeTransportPreWire, CodeHTTP429}}
	// PolicyUI: ephemeral chrome (typing indicator, picker edits): one shot.
	PolicyUI = Policy{EffectClass: contracts.EffectReversible, MaxAttempts: 1}
)

// LandingKind is S7's typed decision for a reported outcome.
type LandingKind int

const (
	LandingSucceeded LandingKind = iota + 1
	LandingRetry
	LandingTerminal
	LandingUnknown
	LandingCancelled
)

// Landing is what S7 decided; the owner builds ONE companion from it.
type Landing struct {
	Kind   LandingKind
	NextAt time.Time // LandingRetry only
	Code   string
}

// Companion is the owning component's event that must commit ATOMICALLY
// with the S7 transition. Key must equal the operation id (S7 validates key
// + cardinality; it never interprets the payload).
type Companion struct {
	Key    contracts.OperationID
	Params contracts.EnvelopeParams
}

// Builder returns the owner's companion for S7's landing. For a durable
// operation it is mandatory; it may return the zero Companion ONLY for
// LandingUnknown (the row is already durably parked from Consume).
type Builder func(Landing) Companion

type operation struct {
	state    contracts.AttemptState
	target   contracts.TargetID
	policy   Policy
	begun    time.Time
	deadline time.Time
	attempts int // CONSUMED attempts
	nextAt   time.Time
	// current lease (AUTHORIZED)
	nonce      string
	nonceHash  string
	expires    time.Time
	consumed   bool
	rehydrated bool // lease rehydrated from the projection: never trusted, revoked on Next
	lastCode   string
	execCancel context.CancelFunc
}

// Authority is the sole grant issuer. Safe for concurrent use.
type Authority struct {
	mu   sync.Mutex
	now  func() time.Time
	ttl  time.Duration
	tbl  *machine.Table[contracts.AttemptState]
	ops  map[contracts.OperationID]*operation
	j    *journal.Journal // nil: in-memory only (no Durable operations)
	rand func() float64
	// appendFault is a FAULT SEAM for detectors (inert unless set): it sees
	// the event types of a batch about to be appended and may refuse it.
	appendFault func(types []string) error
}

// SetAppendFault installs the append fault seam (tests). nil clears it.
func (a *Authority) SetAppendFault(f func(types []string) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.appendFault = f
}

// NewAuthority builds an IN-MEMORY authority (turn-scoped operations only;
// a Durable policy is refused). now is an injectable clock; grantTTL is the
// authorization lease (expires_at = issued_at + ttl).
func NewAuthority(now func() time.Time, grantTTL time.Duration) *Authority {
	if now == nil {
		now = time.Now
	}
	if grantTTL <= 0 {
		grantTTL = time.Minute
	}
	return &Authority{now: now, ttl: grantTTL, tbl: machine.AttemptTable(),
		ops: map[contracts.OperationID]*operation{}, rand: mrand.Float64}
}

// New builds the durable-capable authority bound to the profile journal and
// REHYDRATES every non-terminal durable operation from the s7_operations
// projection: a record with a durable STARTED and no report (crash
// mid-attempt) is landed UNKNOWN (never re-granted, E9); an AUTHORIZED
// lease with no STARTED is marked for revocation on its next Next.
func New(j *journal.Journal, now func() time.Time, grantTTL time.Duration) (*Authority, error) {
	if j == nil {
		return nil, fmt.Errorf("s7: a journal is required for the durable authority (fail closed)")
	}
	a := NewAuthority(now, grantTTL)
	a.j = j
	if err := a.rehydrate(context.Background()); err != nil {
		return nil, err
	}
	return a, nil
}

// SetJitterSource injects the backoff jitter source (tests pin it).
func (a *Authority) SetJitterSource(f func() float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rand = f
}

// Begin registers an operation under a policy. It is IDEMPOTENT: an
// existing record (including one rehydrated from the projection) is a
// strict no-op; a different target or policy for the same key is refused.
func (a *Authority) Begin(op contracts.OperationID, target contracts.TargetID, policy Policy) error {
	if !op.Valid() || !target.Valid() {
		return fmt.Errorf("s7 begin: operation and target ids are required (fail closed)")
	}
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("s7 begin: %w (fail closed)", err)
	}
	want, err := policy.canonical()
	if err != nil {
		return fmt.Errorf("s7 begin: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if rec, ok := a.ops[op]; ok {
		if rec.state == contracts.AttemptCancelled && rec.target == "" {
			return fmt.Errorf("s7 begin: operation %q was cancelled before it began: %w", op, ErrAttemptNotAuthorized)
		}
		// Re-begin is a strict no-op ONLY for the identical canonical policy
		// and target (code-review r2 codex #6: partial comparison let the
		// contract drift silently).
		have, _ := rec.policy.canonical()
		if rec.target != target || string(have) != string(want) {
			return fmt.Errorf("s7 begin: operation %q already begun with a different target/policy (fail closed)", op)
		}
		return nil
	}
	if policy.Durable && a.j == nil {
		return fmt.Errorf("s7 begin: durable policy needs the journal-bound authority (fail closed)")
	}
	now := a.now()
	rec := &operation{target: target, policy: policy, begun: now}
	if policy.Deadline > 0 {
		rec.deadline = now.Add(policy.Deadline)
	}
	state, err := a.tbl.Step(contracts.AttemptInvalid, machine.EvAttemptPlanned)
	if err != nil {
		return fmt.Errorf("s7 begin: %w", err)
	}
	rec.state = state
	if policy.Durable {
		ev, err := a.evBegun(op, rec)
		if err != nil {
			return fmt.Errorf("s7 begin: %w", err)
		}
		if err := a.appendLocked(context.Background(), ev); err != nil {
			return fmt.Errorf("s7 begin: durable record failed (fail closed): %w", err)
		}
	}
	a.ops[op] = rec
	return nil
}

// Issue is the one-shot convenience for PolicyTool operations (the loop's
// tool attempts): Begin(PolicyTool) + Next. A second Issue for the same
// operation is refused (outstanding lease or exhausted single attempt).
func (a *Authority) Issue(op contracts.OperationID, target contracts.TargetID) (Grant, error) {
	if err := a.Begin(op, target, PolicyTool); err != nil {
		return Grant{}, fmt.Errorf("s7 issue: %w", err)
	}
	g, err := a.Next(op, nil)
	if err != nil {
		return Grant{}, fmt.Errorf("s7 issue: %w", err)
	}
	return g, nil
}

// Next issues the next grant for an operation ONLY from PLANNED (first
// attempt) or FAILED_RETRYABLE, only when the backoff is due, the attempt
// cap is not spent and the deadline has not passed. Exhaustion/deadline is
// a durable transition to FAILED committed in ONE batch with the owner's
// companion (build); a durable operation refuses a nil build. An expired or
// rehydrated (unconsumed) lease is durably revoked and a fresh grant issued
// — the old nonce is dead, the attempt number is unchanged (nothing
// physical was consumed).
func (a *Authority) Next(op contracts.OperationID, build Builder) (Grant, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		return Grant{}, fmt.Errorf("s7 next: operation %q not begun: %w", op, ErrAttemptNotAuthorized)
	}
	if rec.policy.Durable && build == nil {
		return Grant{}, ErrNoBuilder
	}
	now := a.now()
	switch rec.state {
	case contracts.AttemptAuthorized:
		if !rec.rehydrated && now.Before(rec.expires) {
			return Grant{}, fmt.Errorf("s7 next: operation %q already holds an outstanding grant: %w", op, ErrAttemptNotAuthorized)
		}
		// Lease recovery: revoke the dead lease durably, back to PLANNED.
		state, err := a.tbl.Step(rec.state, machine.EvAttemptLeaseRevoked)
		if err != nil {
			return Grant{}, fmt.Errorf("s7 next: %w", err)
		}
		if rec.policy.Durable {
			if err := a.appendLocked(context.Background(), a.evLeaseRevoked(op, rec)); err != nil {
				return Grant{}, fmt.Errorf("s7 next: lease revocation not durable (fail closed): %w", err)
			}
		}
		rec.state, rec.nonce, rec.nonceHash, rec.expires, rec.rehydrated, rec.consumed = state, "", "", time.Time{}, false, false
	case contracts.AttemptPlanned, contracts.AttemptFailedRetryable:
	default:
		return Grant{}, fmt.Errorf("s7 next: operation %q is %s: %w", op, rec.state, ErrAttemptNotAuthorized)
	}
	// Cap / deadline: terminal landing in ONE batch with the companion.
	if rec.attempts >= rec.policy.MaxAttempts || (!rec.deadline.IsZero() && !now.Before(rec.deadline)) {
		if err := a.terminalizeLocked(op, rec, machine.EvAttemptExhausted, build, Landing{Kind: LandingTerminal, Code: rec.lastCode}); err != nil {
			return Grant{}, err
		}
		return Grant{}, ErrExhausted
	}
	if now.Before(rec.nextAt) {
		return Grant{}, ErrNotDue
	}
	ev := machine.EvAttemptAuthorized
	if rec.state == contracts.AttemptFailedRetryable {
		ev = machine.EvAttemptRetryAuthorized
	}
	state, err := a.tbl.Step(rec.state, ev)
	if err != nil {
		return Grant{}, fmt.Errorf("s7 next: %w", err)
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return Grant{}, fmt.Errorf("s7 next: nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	sum := sha256.Sum256([]byte(nonce))
	g := Grant{OperationID: op, AttemptNo: rec.attempts + 1, TargetID: rec.target,
		IssuedAt: now, ExpiresAt: now.Add(a.ttl), Nonce: nonce}
	if rec.policy.Durable {
		if err := a.appendLocked(context.Background(), a.evAuthorized(op, g, hex.EncodeToString(sum[:]))); err != nil {
			return Grant{}, fmt.Errorf("s7 next: authorization not durable — no grant (fail closed): %w", err)
		}
	}
	rec.state, rec.nonce, rec.nonceHash, rec.expires, rec.consumed = state, nonce, hex.EncodeToString(sum[:]), g.ExpiresAt, false
	return g, nil
}

// Consume authorizes ONE physical attempt: every field must match the
// authority's record, the grant must be unconsumed and unexpired, and the
// operation must be AUTHORIZED. For a Durable operation exactly ONE
// companion with Key == op is required and the durable STARTED event
// commits with it in ONE batch BEFORE the wire (no wire without a durable
// STARTED). Any mismatch is the same opaque ATTEMPT_NOT_AUTHORIZED.
func (a *Authority) Consume(g Grant, companions ...Companion) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[g.OperationID]
	if !ok {
		return fmt.Errorf("s7 consume: %w", ErrAttemptNotAuthorized)
	}
	nonceOK := subtle.ConstantTimeCompare([]byte(rec.nonce), []byte(g.Nonce)) == 1
	if rec.consumed || rec.rehydrated || !nonceOK || g.AttemptNo != rec.attempts+1 || g.TargetID != rec.target ||
		rec.state != contracts.AttemptAuthorized || !a.now().Before(rec.expires) {
		return fmt.Errorf("s7 consume: %w", ErrAttemptNotAuthorized)
	}
	if rec.policy.Durable {
		if len(companions) != 1 || companions[0].Key != g.OperationID {
			return fmt.Errorf("s7 consume: %w", ErrCompanion)
		}
	} else if len(companions) != 0 {
		return fmt.Errorf("s7 consume: %w", ErrCompanion)
	}
	state, err := a.tbl.Step(rec.state, machine.EvAttemptStarted)
	if err != nil {
		return fmt.Errorf("s7 consume: %w", err)
	}
	if rec.policy.Durable {
		if err := a.appendLocked(context.Background(), a.evStarted(g.OperationID, g.AttemptNo), companions[0].Params); err != nil {
			return fmt.Errorf("s7 consume: STARTED not durable — no wire (fail closed): %w", err)
		}
	}
	rec.state = state
	rec.consumed = true
	rec.attempts++
	return nil
}

// Report lands the physical outcome. Legal only from RUNNING. The outcome
// is the adapter's proposal; S7 decides the Landing from the Policy:
// Succeeded -> SUCCEEDED; Unknown -> UNKNOWN (never re-granted);
// FailedTerminal -> FAILED; FailedRetryable -> FAILED_RETRYABLE with
// next_attempt_at when the code is retryable and cap/deadline allow, else
// FAILED. The owner's companion (build) commits in the same batch.
func (a *Authority) Report(op contracts.OperationID, outcome Outcome, code string, build Builder) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		return fmt.Errorf("s7 report: unknown operation (fail closed)")
	}
	if rec.policy.Durable && build == nil {
		return ErrNoBuilder
	}
	if rec.state != contracts.AttemptRunning {
		return fmt.Errorf("s7 report: operation %q is %s, not RUNNING (fail closed)", op, rec.state)
	}
	now := a.now()
	var ev string
	landing := Landing{Code: code}
	switch outcome {
	case OutcomeSucceeded:
		ev, landing.Kind = machine.EvAttemptSucceeded, LandingSucceeded
	case OutcomeUnknown:
		ev, landing.Kind = machine.EvAttemptLost, LandingUnknown
	case OutcomeFailedTerminal:
		ev, landing.Kind = machine.EvAttemptFailed, LandingTerminal
	case OutcomeFailedRetryable:
		wait := a.backoffLocked(rec)
		if rec.policy.retryable(code) && rec.attempts < rec.policy.MaxAttempts &&
			(rec.deadline.IsZero() || now.Add(wait).Before(rec.deadline)) {
			ev, landing.Kind, landing.NextAt = machine.EvAttemptFailedRetryable, LandingRetry, now.Add(wait)
		} else {
			ev, landing.Kind = machine.EvAttemptFailed, LandingTerminal
		}
	default:
		return fmt.Errorf("s7 report: unknown outcome %d (fail closed)", outcome)
	}
	// The closed (outcome, landing, code, next_at) compatibility check is
	// the S7 API boundary's own invariant, not just the durable journal
	// event validator's — a non-durable operation must refuse the same
	// impossible narratives a durable one would (code-review CODE4 codex #1).
	if code != "" && !knownCodes[code] {
		return fmt.Errorf("s7 report: code %q outside the closed vocabulary (fail closed)", code)
	}
	nextAtUnix := int64(0)
	if !landing.NextAt.IsZero() {
		nextAtUnix = landing.NextAt.Unix()
	}
	if err := validReport(outcome, landing.Kind, landing.Code, nextAtUnix); err != nil {
		return fmt.Errorf("s7 report: %w", err)
	}
	state, err := a.tbl.Step(rec.state, ev)
	if err != nil {
		return fmt.Errorf("s7 report: %w", err)
	}
	if rec.policy.Durable {
		params := []contracts.EnvelopeParams{a.evReported(op, rec.attempts, outcome, code, landing)}
		if terminal(state) {
			params = append(params, a.evTerminal(op, state))
		}
		c := build(landing)
		if c.Key == "" && c.Params.EventType == "" {
			if landing.Kind != LandingUnknown {
				return fmt.Errorf("s7 report: %w", ErrCompanion)
			}
		} else {
			if c.Key != op {
				return fmt.Errorf("s7 report: %w", ErrCompanion)
			}
			params = append(params, c.Params)
		}
		if err := a.appendLocked(context.Background(), params...); err != nil {
			return fmt.Errorf("s7 report: landing not durable (fail closed): %w", err)
		}
	}
	rec.state = state
	rec.lastCode = code
	rec.nonce, rec.nonceHash, rec.expires = "", "", time.Time{}
	if landing.Kind == LandingRetry {
		rec.nextAt = landing.NextAt
	}
	if rec.execCancel != nil {
		rec.execCancel = nil
	}
	return nil
}

// Cancel is the terminal cancel token. Cancelling an operation that has no
// record yet REGISTERS the token, so a later Begin/Issue is refused.
// Cancelling a terminal operation is refused by the canonical table. For a
// durable operation the owner's companion commits in the same batch.
func (a *Authority) Cancel(op contracts.OperationID, build Builder) error {
	if !op.Valid() {
		return fmt.Errorf("s7 cancel: operation id is required (fail closed)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		a.ops[op] = &operation{state: contracts.AttemptCancelled}
		return nil
	}
	if rec.policy.Durable && build == nil {
		return ErrNoBuilder
	}
	if err := a.terminalizeLocked(op, rec, machine.EvAttemptCancelled, build, Landing{Kind: LandingCancelled, Code: rec.lastCode}); err != nil {
		return err
	}
	if rec.execCancel != nil {
		rec.execCancel() // the running execution context dies WITH the attempt
		rec.execCancel = nil
	}
	return nil
}

// terminalizeLocked applies a terminal event (exhausted/cancelled) and, for a
// durable operation, commits it with the owner's companion in ONE batch.
func (a *Authority) terminalizeLocked(op contracts.OperationID, rec *operation, ev string, build Builder, landing Landing) error {
	state, err := a.tbl.Step(rec.state, ev)
	if err != nil {
		return fmt.Errorf("s7 %s: %w", ev, err)
	}
	if rec.policy.Durable {
		c := build(landing)
		if c.Key != op || c.Params.EventType == "" {
			return fmt.Errorf("s7 %s: %w", ev, ErrCompanion)
		}
		if err := a.appendLocked(context.Background(), a.evTerminal(op, state), c.Params); err != nil {
			return fmt.Errorf("s7 %s: terminal landing not durable (fail closed): %w", ev, err)
		}
	}
	rec.state = state
	rec.nonce, rec.nonceHash, rec.expires = "", "", time.Time{}
	return nil
}

func terminal(s contracts.AttemptState) bool {
	switch s {
	case contracts.AttemptSucceeded, contracts.AttemptFailed, contracts.AttemptCancelled, contracts.AttemptUnknown, contracts.AttemptManualRecovery:
		return true
	}
	return false
}

// backoffLocked computes the wait BEFORE attempt attempts+1 (attempts is the
// number already consumed, >= 1 here).
func (a *Authority) backoffLocked(rec *operation) time.Duration {
	b := rec.policy.Backoff
	if b.Base <= 0 {
		return 0
	}
	n := rec.attempts
	if n < 1 {
		n = 1
	}
	wait := time.Duration(float64(b.Base) * math.Pow(2, float64(n-1)))
	if b.Max > 0 && wait > b.Max {
		wait = b.Max
	}
	if b.Jitter && a.rand != nil {
		wait = time.Duration(float64(wait) * a.rand())
	}
	return wait
}

// AttemptContext derives the execution context for a CONSUMED attempt —
// S7 owns deadline and cancel: the attempt runs under the EARLIEST of the
// caller-supplied call deadline, the grant expiry, the policy
// AttemptTimeout and the operation deadline. Refused for operations that
// are not RUNNING (nothing may execute outside a consumed grant).
func (a *Authority) AttemptContext(ctx context.Context, op contracts.OperationID, callDeadline time.Time) (context.Context, context.CancelFunc, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok || rec.state != contracts.AttemptRunning {
		return nil, nil, fmt.Errorf("s7 attempt-context: operation is not RUNNING: %w", ErrAttemptNotAuthorized)
	}
	// Authority-clock bounds (grant expiry, operation deadline, attempt
	// timeout) are converted to the WALL clock through their REMAINING
	// duration, so an injected clock and the wall clock agree; the caller's
	// call deadline is already wall time and wins when it is earlier (so an
	// executor sees EXACTLY the call deadline it was given).
	now := a.now()
	var authBound time.Time
	earliest := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if authBound.IsZero() || t.Before(authBound) {
			authBound = t
		}
	}
	earliest(rec.expires)
	earliest(rec.deadline)
	if rec.policy.AttemptTimeout > 0 {
		earliest(now.Add(rec.policy.AttemptTimeout))
	}
	deadline := callDeadline
	if !authBound.IsZero() {
		remaining := authBound.Sub(now)
		if remaining <= 0 {
			return nil, nil, fmt.Errorf("s7 attempt-context: attempt deadline already passed: %w", ErrAttemptNotAuthorized)
		}
		if wallBound := time.Now().Add(remaining); deadline.IsZero() || wallBound.Before(deadline) {
			deadline = wallBound
		}
	}
	var dctx context.Context
	var cancel context.CancelFunc
	if deadline.IsZero() {
		dctx, cancel = context.WithCancel(ctx)
	} else {
		dctx, cancel = context.WithDeadline(ctx, deadline)
	}
	// Registered under the SAME lock as the state check: a Cancel racing
	// this call either sees non-RUNNING (context refused) or finds the
	// stored cancel and kills the live context.
	rec.execCancel = cancel
	return dctx, cancel, nil
}

// Attempts reports how many PHYSICAL attempts the operation consumed.
func (a *Authority) Attempts(op contracts.OperationID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if rec, ok := a.ops[op]; ok {
		return rec.attempts
	}
	return 0
}

// Target exposes the resource identity a currently-known operation's grant
// is bound to — false only for an OperationID this Authority has never
// Begin'd in this process, or one whose TERMINAL record was not carried
// forward by rehydrate() across a restart (rehydrate() only reloads
// Planned/FailedRetryable/Unknown/Authorized/Running into a.ops — a
// same-process terminal operation, by contrast, stays queryable here
// until the process itself restarts; Target and State share this exact
// "not in a.ops" contract). Lets a caller that discovers a candidate
// OperationID from some OTHER source (e.g. an owner's own journal
// payload, which S7 never verifies matches the operation a companion
// was actually Consumed against — Consume only enforces Companion.Key, a
// structural field, not the payload's own JSON content) cross-check it
// against S7's OWN authoritative binding before trusting it belongs to a
// specific resource (code-review finding, workspace/scan.go's restart
// scan).
func (a *Authority) Target(op contracts.OperationID) (contracts.TargetID, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		return "", false
	}
	return rec.target, true
}

// MatchesPolicy reports whether a currently-known operation's bound
// policy is deeply equal to want (false, ok=false for unknown/terminal-
// and-not-rehydrated — the same contract Target/State share). Compares
// by VALUE, never hands the internal Policy back to the caller: Policy
// carries slice fields (RetryableCodes, FallbackTargets) that would
// alias this Authority's own stored record if returned directly, letting
// a caller mutate durable internal state through what looks like a
// read-only accessor (code-review finding: a caller discovering a
// candidate operation from an external source — see Target's own doc
// comment — must be able to verify it was actually Begin'd under the
// EXACT expected policy, e.g. the correct EffectClass/Durable shape, not
// just the right target — an operation sharing a prefix AND a target but
// begun under a foreign policy is an impossible governance narrative for
// that owner, not a legitimate one it happens to also match).
func (a *Authority) MatchesPolicy(op contracts.OperationID, want Policy) (matches bool, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, exists := a.ops[op]
	if !exists {
		return false, false
	}
	return reflect.DeepEqual(rec.policy, want), true
}

// BoundJournal exposes the *journal.Journal this Authority was
// constructed from (New's own j argument — nil only for an in-memory-only
// Authority built with no Durable-operation support at all). Lets a
// caller holding a (*journal.Journal, *Authority) pair from two
// DIFFERENT, independently-obtained sources prove they are actually
// bound together (pointer identity — the SAME open journal, not merely
// two journals sharing a profile) before trusting the Authority's
// authoritative state to describe events replayed from that journal
// (code-review finding: workspace/scan.go's restart scan independently
// accepted a journal to replay and an authority to cross-check against,
// with nothing proving they were the SAME journal — a caller passing a
// mismatched pair could get a closed transaction from one journal
// reported as MID_CRASH using a same-operation-ID UNKNOWN record that
// actually belongs to a completely different journal).
func (a *Authority) BoundJournal() *journal.Journal {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.j
}

// State exposes the authoritative attempt state (false for unknown).
func (a *Authority) State(op contracts.OperationID) (contracts.AttemptState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		return contracts.AttemptInvalid, false
	}
	return rec.state, true
}

// NextAt reports when a FAILED_RETRYABLE operation becomes due (zero when
// not retry-waiting).
func (a *Authority) NextAt(op contracts.OperationID) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	if rec, ok := a.ops[op]; ok && rec.state == contracts.AttemptFailedRetryable {
		return rec.nextAt
	}
	return time.Time{}
}

// policyJSON is the durable, strictly-decoded form of a Policy.
type policyJSON struct {
	EffectClass     contracts.EffectClass `json:"effect_class"`
	MaxAttempts     int                   `json:"max_attempts"`
	AttemptTimeout  time.Duration         `json:"attempt_timeout"`
	Deadline        time.Duration         `json:"deadline"`
	Backoff         BackoffPolicy         `json:"backoff"`
	RetryableCodes  []string              `json:"retryable_codes"`
	FallbackTargets []contracts.TargetID  `json:"fallback_targets"`
	IdempotencyKey  string                `json:"idempotency_key"`
	Durable         bool                  `json:"durable"`
}

func marshalPolicy(p Policy) ([]byte, error) {
	b, err := json.Marshal(policyJSON(p))
	if err != nil {
		return nil, fmt.Errorf("s7: policy not serializable (fail closed): %w", err)
	}
	return b, nil
}

// unmarshalPolicy fails closed on corrupt, unknown, trailing or semantically
// invalid policy data (code-review r2 codex #6).
func unmarshalPolicy(b []byte) (Policy, error) {
	var pj policyJSON
	if err := strictDecode(b, &pj); err != nil {
		return Policy{}, fmt.Errorf("s7: corrupt policy_json (fail closed): %w", err)
	}
	p := Policy(pj)
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("s7: invalid policy_json (fail closed): %w", err)
	}
	return p, nil
}

// strictDecode decodes exactly ONE JSON value with no unknown fields and no
// trailing data.
func strictDecode(b []byte, out any) error {
	dec := json.NewDecoder(bytesReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

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
	"math"
	mrand "math/rand/v2"
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
	CodeTransportPreWire   = "transport_prewire"
	CodeHTTP429            = "http_429"
	CodeHTTP4xx            = "http_4xx"
	CodeHTTP5xx            = "http_5xx"
	CodeTransportPostWrite = "transport_postwrite"
	CodeMalformedReply     = "malformed_reply"
	CodeLocalRefused       = "local_refused"
	CodeInvalidGeneral     = "invalid_general"
	CodeCrashRecovered     = "crash_recovered"
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
		RetryableCodes: []string{CodeTransportPreWire, CodeHTTP429}, Durable: true}
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
	if policy.MaxAttempts < 1 {
		return fmt.Errorf("s7 begin: policy needs MaxAttempts >= 1 (fail closed)")
	}
	if !policy.EffectClass.Valid() {
		return fmt.Errorf("s7 begin: policy needs an effect class (fail closed)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if rec, ok := a.ops[op]; ok {
		if rec.state == contracts.AttemptCancelled && rec.target == "" {
			return fmt.Errorf("s7 begin: operation %q was cancelled before it began: %w", op, ErrAttemptNotAuthorized)
		}
		if rec.target != target || rec.policy.Durable != policy.Durable || rec.policy.MaxAttempts != policy.MaxAttempts {
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
	deadline := callDeadline
	earliest := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if deadline.IsZero() || t.Before(deadline) {
			deadline = t
		}
	}
	earliest(rec.expires)
	earliest(rec.deadline)
	if rec.policy.AttemptTimeout > 0 {
		earliest(a.now().Add(rec.policy.AttemptTimeout))
	}
	dctx, cancel := context.WithDeadline(ctx, deadline)
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

// unmarshalPolicy fails closed on corrupt or unknown fields.
func unmarshalPolicy(b []byte) (Policy, error) {
	dec := json.NewDecoder(bytesReader(b))
	dec.DisallowUnknownFields()
	var pj policyJSON
	if err := dec.Decode(&pj); err != nil {
		return Policy{}, fmt.Errorf("s7: corrupt policy_json (fail closed): %w", err)
	}
	if pj.MaxAttempts < 1 {
		return Policy{}, fmt.Errorf("s7: corrupt policy_json: max_attempts %d in %.200s (fail closed)", pj.MaxAttempts, string(b))
	}
	return Policy(pj), nil
}

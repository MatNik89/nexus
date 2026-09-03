//go:build linux

// Package s7min is the P0 -min slice of S7 — the SOLE owner of retry,
// deadline and cancel (HARNESS-SPEC P0.2; HARDQ A2): every physical
// provider/tool attempt presents a unique, single-use, expiring
// AttemptGrant issued here; a cancel token is terminal; the no-retry
// policy collapses FAILED_RETRYABLE to FAILED_TERMINAL (one grant per
// operation — the full retry/backoff/fallback engine is P2/P3, and ONLY
// it may ever issue a second grant). Adapters and loops never retry.
package s7min

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
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

// Outcome is the closed physical-attempt result set.
type Outcome int

const (
	OutcomeSucceeded Outcome = iota + 1
	// OutcomeFailedRetryable exists so ADAPTERS can report honestly; in P0
	// it still lands as FAILED_TERMINAL (no-retry policy, HARDQ A2).
	OutcomeFailedRetryable
	OutcomeFailedTerminal
)

// ErrAttemptNotAuthorized is the SPEC P0.2 literal refusal.
var ErrAttemptNotAuthorized = errors.New("ATTEMPT_NOT_AUTHORIZED")

// operation is the authority's authoritative per-operation record. State
// legality is driven through the CANONICAL attempt table (machine) — one
// source of transition truth, default-reject.
type operation struct {
	state    contracts.AttemptState
	attempts int
	target   contracts.TargetID
	nonce    string
	expires  time.Time
	consumed bool
}

// Authority is the sole grant issuer. Safe for concurrent use.
type Authority struct {
	mu  sync.Mutex
	now func() time.Time
	ttl time.Duration
	tbl *machine.Table[contracts.AttemptState]
	ops map[contracts.OperationID]*operation
}

// NewAuthority builds an authority with an injectable clock (tests pin
// time) and a grant TTL (expires_at = issued_at + ttl).
func NewAuthority(now func() time.Time, grantTTL time.Duration) *Authority {
	if now == nil {
		now = time.Now
	}
	if grantTTL <= 0 {
		grantTTL = time.Minute
	}
	return &Authority{now: now, ttl: grantTTL, tbl: machine.AttemptTable(), ops: map[contracts.OperationID]*operation{}}
}

// Issue mints the ONE grant an operation gets in P0. A second issue for
// the same operation — outstanding, finished, or cancelled — is refused:
// only the full S7 engine (P2/P3) may ever authorize a retry.
func (a *Authority) Issue(op contracts.OperationID, target contracts.TargetID) (Grant, error) {
	if !op.Valid() || !target.Valid() {
		return Grant{}, fmt.Errorf("s7min issue: operation and target ids are required (fail closed)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.ops[op]; exists {
		return Grant{}, fmt.Errorf("s7min issue: operation %q already holds its one P0 grant (no-retry): %w", op, ErrAttemptNotAuthorized)
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return Grant{}, fmt.Errorf("s7min issue: nonce: %w", err)
	}
	// PLANNED → AUTHORIZED through the canonical table (default-reject).
	state, err := a.tbl.Step(contracts.AttemptInvalid, machine.EvAttemptPlanned)
	if err == nil {
		state, err = a.tbl.Step(state, machine.EvAttemptAuthorized)
	}
	if err != nil {
		return Grant{}, fmt.Errorf("s7min issue: %w", err)
	}
	issued := a.now()
	rec := &operation{
		state:   state,
		target:  target,
		nonce:   hex.EncodeToString(nonceBytes),
		expires: issued.Add(a.ttl),
	}
	a.ops[op] = rec
	return Grant{
		OperationID: op, AttemptNo: 1, TargetID: target,
		IssuedAt: issued, ExpiresAt: rec.expires, Nonce: rec.nonce,
	}, nil
}

// Consume authorizes ONE physical attempt: every field must match the
// authority's record, the grant must be unconsumed and unexpired, and the
// operation must still be in AUTHORIZED. Any mismatch is the same opaque
// ATTEMPT_NOT_AUTHORIZED (no oracle for probing which check failed).
func (a *Authority) Consume(g Grant) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[g.OperationID]
	if !ok {
		return fmt.Errorf("s7min consume: %w", ErrAttemptNotAuthorized)
	}
	nonceOK := subtle.ConstantTimeCompare([]byte(rec.nonce), []byte(g.Nonce)) == 1
	if rec.consumed || !nonceOK || g.AttemptNo != 1 || g.TargetID != rec.target ||
		rec.state != contracts.AttemptAuthorized || !a.now().Before(rec.expires) {
		return fmt.Errorf("s7min consume: %w", ErrAttemptNotAuthorized)
	}
	state, err := a.tbl.Step(rec.state, machine.EvAttemptStarted)
	if err != nil {
		return fmt.Errorf("s7min consume: %w", err)
	}
	rec.state = state
	rec.consumed = true
	rec.attempts++
	return nil
}

// Report lands the physical outcome. Legal only from RUNNING (nothing can
// "succeed" without a consumed grant); OutcomeFailedRetryable collapses to
// FAILED_TERMINAL — P0 retries nothing.
func (a *Authority) Report(op contracts.OperationID, outcome Outcome) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		return fmt.Errorf("s7min report: unknown operation (fail closed)")
	}
	var ev string
	switch outcome {
	case OutcomeSucceeded:
		ev = machine.EvAttemptSucceeded
	case OutcomeFailedRetryable, OutcomeFailedTerminal:
		ev = machine.EvAttemptFailed
	default:
		return fmt.Errorf("s7min report: unknown outcome %d (fail closed)", outcome)
	}
	state, err := a.tbl.Step(rec.state, ev)
	if err != nil {
		return fmt.Errorf("s7min report: %w", err)
	}
	rec.state = state
	return nil
}

// Cancel is the terminal cancel token. Cancelling an operation that has no
// record yet REGISTERS the token, so a later Issue is refused (the cancel
// arrived first — HARDQ A2 cancel half). Cancelling a terminal operation
// is refused by the canonical table.
func (a *Authority) Cancel(op contracts.OperationID) error {
	if !op.Valid() {
		return fmt.Errorf("s7min cancel: operation id is required (fail closed)")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, ok := a.ops[op]
	if !ok {
		// Pre-issue token: recorded terminal, blocks any future grant.
		a.ops[op] = &operation{state: contracts.AttemptCancelled}
		return nil
	}
	state, err := a.tbl.Step(rec.state, machine.EvAttemptCancelled)
	if err != nil {
		return fmt.Errorf("s7min cancel: %w", err)
	}
	rec.state = state
	return nil
}

// Attempts reports how many PHYSICAL attempts the operation spent.
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

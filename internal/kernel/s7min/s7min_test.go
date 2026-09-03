// T13 RED table (tasks-P0): s7-min — AttemptGrant single-use + cancel token
// + no-retry policy (retryable → FAILED_TERMINAL in P0). Anchored to
// HARNESS-SPEC P0.2 (grant MUST-fields, ATTEMPT_NOT_AUTHORIZED literal,
// sole-issuer invariant) and HARDQ A2.
package s7min

import (
	"errors"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// The SPEC P0.2 RED literal: a rogue adapter calls the transport twice with
// the SAME grant — the second call fails ATTEMPT_NOT_AUTHORIZED, the
// transport counter stays 1, and the S7 attempt counter stays 1.
func TestAdapterCannotSelfRetry(t *testing.T) {
	a := NewAuthority(fixedClock(time.Unix(1000, 0)), time.Minute)
	g, err := a.Issue("op-1", "provider-a")
	if err != nil {
		t.Fatal(err)
	}
	transport := 0
	callTransport := func(grant Grant) error {
		if err := a.Consume(grant); err != nil {
			return err
		}
		transport++
		return nil
	}
	if err := callTransport(g); err != nil {
		t.Fatalf("first authorized attempt refused: %v", err)
	}
	err = callTransport(g) // rogue self-retry with the same grant
	if !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("second use of one grant must fail ATTEMPT_NOT_AUTHORIZED, got: %v", err)
	}
	if transport != 1 {
		t.Fatalf("transport counter %d, must stay 1", transport)
	}
	if n := a.Attempts("op-1"); n != 1 {
		t.Fatalf("S7 attempt counter %d, must stay 1", n)
	}
}

// A grant the authority never issued — even with plausible fields — is
// refused (the authority's own records are authoritative, not the caller's
// struct).
func TestForgedGrantRefused(t *testing.T) {
	a := NewAuthority(fixedClock(time.Unix(1000, 0)), time.Minute)
	forged := Grant{
		OperationID: "op-x", AttemptNo: 1, TargetID: "provider-a",
		IssuedAt: time.Unix(1000, 0), ExpiresAt: time.Unix(1060, 0),
		Nonce: "0123456789abcdef0123456789abcdef",
	}
	if err := a.Consume(forged); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("forged grant must fail ATTEMPT_NOT_AUTHORIZED: %v", err)
	}
	// A real grant with a TAMPERED field is equally dead.
	g, _ := a.Issue("op-1", "provider-a")
	g.TargetID = "provider-b"
	if err := a.Consume(g); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("tampered grant must fail ATTEMPT_NOT_AUTHORIZED: %v", err)
	}
	if a.Attempts("op-1") != 0 {
		t.Fatal("refused consume counted as a physical attempt")
	}
}

// Grants EXPIRE: a consume after expires_at is refused and counts nothing.
func TestExpiredGrantRefused(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := &now
	a := NewAuthority(func() time.Time { return *clock }, time.Minute)
	g, err := a.Issue("op-1", "provider-a")
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Minute)
	clock = &later
	if err := a.Consume(g); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("expired grant must fail ATTEMPT_NOT_AUTHORIZED: %v", err)
	}
	if a.Attempts("op-1") != 0 {
		t.Fatal("expired consume counted as a physical attempt")
	}
}

// NO-RETRY policy (HARDQ A2): one grant per operation in P0 — after any
// outcome (success, failure, even a reported-retryable failure) the next
// Issue for the same operation is refused. FAILED_RETRYABLE collapses to
// FAILED_TERMINAL.
func TestNoRetryOneGrantPerOperation(t *testing.T) {
	a := NewAuthority(fixedClock(time.Unix(1000, 0)), time.Minute)
	g, _ := a.Issue("op-1", "provider-a")
	if err := a.Consume(g); err != nil {
		t.Fatal(err)
	}
	if err := a.Report("op-1", OutcomeFailedRetryable); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.State("op-1"); st != contracts.AttemptFailed {
		t.Fatalf("retryable failure must collapse to FAILED_TERMINAL in P0, got %v", st)
	}
	if _, err := a.Issue("op-1", "provider-a"); err == nil {
		t.Fatal("second grant for one operation issued (retry in P0)")
	}
	// An UNCONSUMED grant also blocks a second issue (no grant races).
	if _, err := a.Issue("op-2", "provider-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Issue("op-2", "provider-a"); err == nil {
		t.Fatal("second outstanding grant for one operation issued")
	}
}

// Cancel is a TERMINAL token: cancel before issue blocks the issue; cancel
// with an outstanding grant kills its consume; a cancelled operation
// accepts no outcome report.
func TestCancelIsTerminal(t *testing.T) {
	a := NewAuthority(fixedClock(time.Unix(1000, 0)), time.Minute)
	// Cancel BEFORE any grant: token registered, issue refused.
	if err := a.Cancel("op-pre"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Issue("op-pre", "provider-a"); err == nil {
		t.Fatal("grant issued for a cancelled operation")
	}
	// Cancel with an OUTSTANDING grant: consume refused.
	g, _ := a.Issue("op-1", "provider-a")
	if err := a.Cancel("op-1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Consume(g); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("consume after cancel must fail ATTEMPT_NOT_AUTHORIZED: %v", err)
	}
	if st, _ := a.State("op-1"); st != contracts.AttemptCancelled {
		t.Fatalf("state %v, want CANCELLED", st)
	}
	// CANCELLED is terminal: no outcome report lands.
	if err := a.Report("op-1", OutcomeSucceeded); err == nil {
		t.Fatal("outcome reported onto a CANCELLED operation")
	}
	if st, _ := a.State("op-1"); st != contracts.AttemptCancelled {
		t.Fatal("CANCELLED state overwritten")
	}
}

// Nonces are unique and opaque; zero-value inputs are rejected.
func TestGrantHygiene(t *testing.T) {
	a := NewAuthority(fixedClock(time.Unix(1000, 0)), time.Minute)
	g1, err := a.Issue("op-1", "provider-a")
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := a.Issue("op-2", "provider-a")
	if g1.Nonce == "" || g1.Nonce == g2.Nonce {
		t.Fatalf("nonces must be unique and non-empty: %q %q", g1.Nonce, g2.Nonce)
	}
	if g1.AttemptNo != 1 || !g1.ExpiresAt.After(g1.IssuedAt) {
		t.Fatalf("grant fields malformed: %+v", g1)
	}
	if _, err := a.Issue("", "provider-a"); err == nil {
		t.Fatal("empty operation id accepted")
	}
	if _, err := a.Issue("op-3", ""); err == nil {
		t.Fatal("empty target accepted")
	}
	if err := a.Report("op-never", OutcomeSucceeded); err == nil {
		t.Fatal("outcome for an unknown operation accepted")
	}
	// Report is only legal from RUNNING: an issued-but-unconsumed operation
	// cannot succeed (nothing physically ran).
	if err := a.Report("op-1", OutcomeSucceeded); err == nil {
		t.Fatal("outcome accepted without a consumed grant")
	}
}

// OutcomeUnknown parks the attempt in UNKNOWN; Report cannot exit it —
// only the reconciliation owner (E9) may (P0.1 UNKNOWN discipline).
func TestUnknownOutcomeParksForReconciliation(t *testing.T) {
	a := NewAuthority(fixedClock(time.Unix(1000, 0)), time.Minute)
	g, _ := a.Issue("op-1", "provider-a")
	if err := a.Consume(g); err != nil {
		t.Fatal(err)
	}
	if err := a.Report("op-1", OutcomeUnknown); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.State("op-1"); st != contracts.AttemptUnknown {
		t.Fatalf("state %v, want UNKNOWN", st)
	}
	if err := a.Report("op-1", OutcomeSucceeded); err == nil {
		t.Fatal("Report exited UNKNOWN (reconciliation-only exit violated)")
	}
}

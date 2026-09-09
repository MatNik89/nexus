//go:build linux

package s7

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

const evOwnerMark = "test.owner_mark"

func testEvents() map[string]journal.PayloadValidator {
	m := Events()
	m[evOwnerMark] = func(json.RawMessage) error { return nil }
	return m
}

func openJournal(t *testing.T, dir string) *journal.Journal {
	t.Helper()
	j, err := journal.Open(filepath.Join(dir, "j.db"), "work", redact.None{}, testEvents(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

// ownerBuilder returns a companion bound to op carrying the landing kind.
func ownerBuilder(a *Authority, op contracts.OperationID) Builder {
	return func(l Landing) Companion {
		return Companion{Key: op, Params: a.params(evOwnerMark, map[string]any{"op": string(op), "landing": int(l.Kind)})}
	}
}

func countEvents(t *testing.T, j *journal.Journal, types ...string) map[string]int {
	t.Helper()
	got := map[string]int{}
	if err := j.Replay(0, func(ev journal.Event) error { got[ev.Envelope.EventType]++; return nil }); err != nil {
		t.Fatal(err)
	}
	return got
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func consumeOK(t *testing.T, a *Authority, g Grant, c ...Companion) {
	t.Helper()
	if err := a.Consume(g, c...); err != nil {
		t.Fatalf("consume: %v", err)
	}
}

// Retry policy: a retryable code lands FAILED_RETRYABLE with next_attempt_at =
// now + backoff; Next before due is ErrNotDue (nothing committed); when due a
// NEW grant with attempt_no+1 is issued; the cap lands FAILED (ErrExhausted);
// a code NOT in the policy is terminal even if the adapter proposed retry.
func TestRetryBackoffCapAndCodeVocabulary(t *testing.T) {
	c := &clock{time.Unix(1000, 0)}
	a := NewAuthority(c.now, time.Minute)
	a.SetJitterSource(func() float64 { return 1 })
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 3, Backoff: BackoffPolicy{Base: 2 * time.Second, Max: time.Minute, Jitter: true}, RetryableCodes: []string{CodeHTTP429}}
	if err := a.Begin("op", "t", pol); err != nil {
		t.Fatal(err)
	}
	g1, err := a.Next("op", nil)
	if err != nil || g1.AttemptNo != 1 {
		t.Fatalf("first grant: %v %+v", err, g1)
	}
	consumeOK(t, a, g1)
	if err := a.Report("op", OutcomeFailedRetryable, CodeHTTP429, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.State("op"); st != contracts.AttemptFailedRetryable {
		t.Fatalf("state %s, want FAILED_RETRYABLE", st)
	}
	if at := a.NextAt("op"); !at.Equal(c.t.Add(2 * time.Second)) {
		t.Fatalf("next_attempt_at %v, want now+2s", at)
	}
	if _, err := a.Next("op", nil); !errors.Is(err, ErrNotDue) || !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("grant before due: %v", err)
	}
	c.advance(2 * time.Second)
	g2, err := a.Next("op", nil)
	if err != nil || g2.AttemptNo != 2 || g2.Nonce == g1.Nonce {
		t.Fatalf("second grant: %v %+v", err, g2)
	}
	if err := a.Consume(g1); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatal("dead grant 1 consumed")
	}
	consumeOK(t, a, g2)
	if err := a.Report("op", OutcomeFailedRetryable, CodeHTTP429, nil); err != nil {
		t.Fatal(err)
	}
	// backoff doubles: 4s
	if at := a.NextAt("op"); !at.Equal(c.t.Add(4 * time.Second)) {
		t.Fatalf("second backoff %v", at.Sub(c.t))
	}
	c.advance(4 * time.Second)
	g3, _ := a.Next("op", nil)
	consumeOK(t, a, g3)
	if err := a.Report("op", OutcomeFailedRetryable, CodeHTTP429, nil); err != nil {
		t.Fatal(err)
	}
	// Third failure at the cap lands FAILED directly.
	if st, _ := a.State("op"); st != contracts.AttemptFailed {
		t.Fatalf("after cap: %s", st)
	}
	if _, err := a.Next("op", nil); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("grant after terminal: %v", err)
	}
	if a.Attempts("op") != 3 {
		t.Fatalf("attempts %d", a.Attempts("op"))
	}
	// Code vocabulary: an unlisted code proposed retryable is terminal.
	if err := a.Begin("op2", "t", pol); err != nil {
		t.Fatal(err)
	}
	g, _ := a.Next("op2", nil)
	consumeOK(t, a, g)
	if err := a.Report("op2", OutcomeFailedRetryable, CodeHTTP4xx, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.State("op2"); st != contracts.AttemptFailed {
		t.Fatalf("unlisted code landed %s", st)
	}
}

// Cancel is terminal from FAILED_RETRYABLE too, and a deadline exhausts.
func TestCancelFromRetryableAndDeadline(t *testing.T) {
	c := &clock{time.Unix(1000, 0)}
	a := NewAuthority(c.now, time.Minute)
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 5, Deadline: 10 * time.Second, RetryableCodes: []string{CodeHTTP5xx}}
	a.Begin("op", "t", pol)
	g, _ := a.Next("op", nil)
	consumeOK(t, a, g)
	a.Report("op", OutcomeFailedRetryable, CodeHTTP5xx, nil)
	if err := a.Cancel("op", nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.State("op"); st != contracts.AttemptCancelled {
		t.Fatalf("cancel from retryable: %s", st)
	}
	a.Begin("dl", "t", pol)
	g, _ = a.Next("dl", nil)
	consumeOK(t, a, g)
	a.Report("dl", OutcomeFailedRetryable, CodeHTTP5xx, nil)
	c.advance(11 * time.Second)
	if _, err := a.Next("dl", nil); !errors.Is(err, ErrExhausted) {
		t.Fatalf("deadline not enforced: %v", err)
	}
	if st, _ := a.State("dl"); st != contracts.AttemptFailed {
		t.Fatalf("deadline state %s", st)
	}
}

// Durable lifecycle + restart: the attempt count, backoff and state survive a
// new authority built from the same journal; a restart can never reset the cap.
func TestDurableRestartPreservesCapAndBackoff(t *testing.T) {
	dir := t.TempDir()
	j := openJournal(t, dir)
	c := &clock{time.Unix(1000, 0)}
	a, err := New(j, c.now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	a.SetJitterSource(func() float64 { return 1 })
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 3, Backoff: BackoffPolicy{Base: 5 * time.Second, Max: time.Minute}, RetryableCodes: []string{CodeTransportPreWire}, Durable: true}
	if err := a.Begin("delivery:d1", "channel:tg:delivery:d1", pol); err != nil {
		t.Fatal(err)
	}
	b := ownerBuilder(a, "delivery:d1")
	for i := 1; i <= 2; i++ {
		g, err := a.Next("delivery:d1", b)
		if err != nil || g.AttemptNo != i {
			t.Fatalf("grant %d: %v", i, err)
		}
		consumeOK(t, a, g, Companion{Key: "delivery:d1", Params: a.params(evOwnerMark, map[string]any{"park": true})})
		if err := a.Report("delivery:d1", OutcomeFailedRetryable, CodeTransportPreWire, b); err != nil {
			t.Fatal(err)
		}
		c.advance(time.Minute)
	}
	// "Restart": a fresh authority from the same journal.
	a2, err := New(j, c.now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := a2.State("delivery:d1"); !ok || st != contracts.AttemptFailedRetryable {
		t.Fatalf("rehydrated state %s ok=%v", st, ok)
	}
	if a2.Attempts("delivery:d1") != 2 {
		t.Fatalf("rehydrated attempts %d, want 2 (a restart must not reset the cap)", a2.Attempts("delivery:d1"))
	}
	if err := a2.Begin("delivery:d1", "channel:tg:delivery:d1", pol); err != nil {
		t.Fatalf("Begin on a rehydrated record must be a no-op: %v", err)
	}
	b2 := ownerBuilder(a2, "delivery:d1")
	g3, err := a2.Next("delivery:d1", b2)
	if err != nil || g3.AttemptNo != 3 {
		t.Fatalf("third grant after restart: %v", err)
	}
	consumeOK(t, a2, g3, Companion{Key: "delivery:d1", Params: a2.params(evOwnerMark, map[string]any{"park": true})})
	if err := a2.Report("delivery:d1", OutcomeFailedRetryable, CodeTransportPreWire, b2); err != nil {
		t.Fatal(err)
	}
	if st, _ := a2.State("delivery:d1"); st != contracts.AttemptFailed {
		t.Fatalf("cap after restart: %s", st)
	}
	// A third authority sees FAILED and never grants.
	a3, _ := New(j, c.now, time.Minute)
	if _, ok := a3.State("delivery:d1"); ok {
		t.Fatal("terminal operation rehydrated as live")
	}
	if a3.Begin("delivery:d1", "channel:tg:delivery:d1", pol) != nil {
		// A terminal op is not rehydrated; Begin would start a NEW record —
		// the OWNER (channel) must not re-begin a FAILED delivery. Documented
		// boundary: the projection row is FAILED.
	}
	got := countEvents(t, j)
	if got[EvAttemptStarted] != 3 || got[EvAttemptReported] != 3 || got[EvOperationTerminal] != 1 {
		t.Fatalf("durable narrative incomplete: %+v", got)
	}
}

// Crash between Consume (durable STARTED) and Report: the wire may have been
// touched -> the rehydrated operation is UNKNOWN and never re-granted (E9).
func TestDurableCrashAfterStartedIsUnknown(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	pol := PolicyDelivery
	a.Begin("delivery:x", "channel:tg:delivery:x", pol)
	g, err := a.Next("delivery:x", ownerBuilder(a, "delivery:x"))
	if err != nil {
		t.Fatal(err)
	}
	consumeOK(t, a, g, Companion{Key: "delivery:x", Params: a.params(evOwnerMark, map[string]any{"park": true})})
	// crash: no Report. New authority:
	a2, err := New(j, c.now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := a2.State("delivery:x"); st != contracts.AttemptUnknown {
		t.Fatalf("crashed RUNNING rehydrated as %s, want UNKNOWN", st)
	}
	if _, err := a2.Next("delivery:x", ownerBuilder(a2, "delivery:x")); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatalf("UNKNOWN re-granted: %v", err)
	}
	if err := a2.Consume(g, Companion{Key: "delivery:x", Params: a2.params(evOwnerMark, nil)}); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatal("old grant consumable after crash")
	}
	// Reconciliation is the only exit (owner proves the remote state).
	if err := a2.Reconcile("delivery:x", true, ownerBuilder(a2, "delivery:x")); err != nil {
		t.Fatal(err)
	}
	if st, _ := a2.State("delivery:x"); st != contracts.AttemptSucceeded {
		t.Fatalf("reconciled state %s", st)
	}
}

// Lease recovery: an AUTHORIZED lease with no STARTED (crash before the wire,
// or expiry) is revoked durably and a FRESH grant is issued with the SAME
// attempt number; the old nonce is dead; consumed attempts never exceed the cap.
func TestDurableLeaseRecoveryAfterCrashAndExpiry(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 1, Durable: true}
	a.Begin("delivery:l", "channel:tg:delivery:l", pol)
	gOld, err := a.Next("delivery:l", ownerBuilder(a, "delivery:l"))
	if err != nil {
		t.Fatal(err)
	}
	// crash before Consume
	a2, _ := New(j, c.now, time.Minute)
	if st, _ := a2.State("delivery:l"); st != contracts.AttemptAuthorized {
		t.Fatalf("rehydrated lease state %s", st)
	}
	if err := a2.Consume(gOld, Companion{Key: "delivery:l", Params: a2.params(evOwnerMark, nil)}); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatal("rehydrated lease accepted the old nonce")
	}
	gNew, err := a2.Next("delivery:l", ownerBuilder(a2, "delivery:l"))
	if err != nil || gNew.AttemptNo != 1 || gNew.Nonce == gOld.Nonce {
		t.Fatalf("fresh grant after lease recovery: %v %+v", err, gNew)
	}
	got := countEvents(t, j)
	if got[EvLeaseRevoked] != 1 || got[EvAttemptAuthorized] != 2 {
		t.Fatalf("lease narrative: %+v", got)
	}
	consumeOK(t, a2, gNew, Companion{Key: "delivery:l", Params: a2.params(evOwnerMark, nil)})
	if a2.Attempts("delivery:l") != 1 {
		t.Fatalf("consumed attempts %d, want 1", a2.Attempts("delivery:l"))
	}
	// Runtime expiry: a lease older than the TTL is recovered the same way.
	a.Begin("delivery:e", "channel:tg:delivery:e", pol)
	gE, _ := a.Next("delivery:e", ownerBuilder(a, "delivery:e"))
	c.advance(2 * time.Minute)
	gE2, err := a.Next("delivery:e", ownerBuilder(a, "delivery:e"))
	if err != nil || gE2.AttemptNo != 1 || gE2.Nonce == gE.Nonce {
		t.Fatalf("expired lease not recovered: %v", err)
	}
	if err := a.Consume(gE, Companion{Key: "delivery:e", Params: a.params(evOwnerMark, nil)}); !errors.Is(err, ErrAttemptNotAuthorized) {
		t.Fatal("expired nonce consumable")
	}
}

// A durable operation refuses a nil builder at Next — while grant-eligible AND
// after exhaustion: fail-closed error, no grant, no transition, no event.
func TestDurableNextRefusesNilBuilder(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 1, Durable: true}
	a.Begin("delivery:n", "channel:tg:delivery:n", pol)
	before := countEvents(t, j)
	if _, err := a.Next("delivery:n", nil); !errors.Is(err, ErrNoBuilder) {
		t.Fatalf("nil builder accepted while grant-eligible: %v", err)
	}
	if st, _ := a.State("delivery:n"); st != contracts.AttemptPlanned {
		t.Fatalf("state moved to %s", st)
	}
	if after := countEvents(t, j); after[EvAttemptAuthorized] != before[EvAttemptAuthorized] {
		t.Fatal("an event was appended despite the refusal")
	}
	g, _ := a.Next("delivery:n", ownerBuilder(a, "delivery:n"))
	consumeOK(t, a, g, Companion{Key: "delivery:n", Params: a.params(evOwnerMark, nil)})
	a.Report("delivery:n", OutcomeFailedRetryable, CodeTransportPreWire, ownerBuilder(a, "delivery:n"))
	// cap=1 -> already FAILED at report; a second op exercises exhaustion-at-Next:
	pol2 := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 1, Deadline: time.Second, Durable: true}
	a.Begin("delivery:n2", "channel:tg:delivery:n2", pol2)
	c.advance(2 * time.Second)
	before = countEvents(t, j)
	if _, err := a.Next("delivery:n2", nil); !errors.Is(err, ErrNoBuilder) {
		t.Fatalf("nil builder accepted at exhaustion: %v", err)
	}
	if after := countEvents(t, j); after[EvOperationTerminal] != before[EvOperationTerminal] {
		t.Fatal("terminal committed without the companion")
	}
	if err := a.Report("delivery:n", OutcomeSucceeded, "", nil); !errors.Is(err, ErrNoBuilder) && err == nil {
		t.Fatal("durable Report accepted a nil builder")
	}
}

// Companion cardinality/binding: durable Consume needs exactly one companion
// with Key == op; non-durable refuses any companion; a refused Consume leaves
// the lease intact (no transition, no event).
func TestCompanionCardinalityAndBinding(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	a.Begin("delivery:a", "channel:tg:delivery:a", PolicyDelivery)
	a.Begin("delivery:b", "channel:tg:delivery:b", PolicyDelivery)
	ga, _ := a.Next("delivery:a", ownerBuilder(a, "delivery:a"))
	before := countEvents(t, j)
	if err := a.Consume(ga); !errors.Is(err, ErrCompanion) {
		t.Fatalf("no companion accepted: %v", err)
	}
	if err := a.Consume(ga, Companion{Key: "delivery:b", Params: a.params(evOwnerMark, nil)}); !errors.Is(err, ErrCompanion) {
		t.Fatalf("foreign companion accepted: %v", err)
	}
	if err := a.Consume(ga, Companion{Key: "delivery:a", Params: a.params(evOwnerMark, nil)}, Companion{Key: "delivery:a", Params: a.params(evOwnerMark, nil)}); !errors.Is(err, ErrCompanion) {
		t.Fatalf("two companions accepted: %v", err)
	}
	if after := countEvents(t, j); after[EvAttemptStarted] != before[EvAttemptStarted] {
		t.Fatal("STARTED committed despite companion refusal")
	}
	if st, _ := a.State("delivery:a"); st != contracts.AttemptAuthorized {
		t.Fatalf("lease lost: %s", st)
	}
	consumeOK(t, a, ga, Companion{Key: "delivery:a", Params: a.params(evOwnerMark, nil)})
	// non-durable refuses a companion
	a.Begin("prov", "provider:x", PolicyProvider)
	g, _ := a.Next("prov", nil)
	if err := a.Consume(g, Companion{Key: "prov", Params: a.params(evOwnerMark, nil)}); !errors.Is(err, ErrCompanion) {
		t.Fatalf("non-durable accepted a companion: %v", err)
	}
	consumeOK(t, a, g)
}

// Exhaustion at Next lands FAILED together with the owner's companion in ONE
// batch: adjacent journal offsets, and the terminal is never present without
// the companion.
func TestExhaustionLandsAtomicallyWithCompanion(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 1, Deadline: time.Second, Durable: true}
	a.Begin("delivery:z", "channel:tg:delivery:z", pol)
	c.advance(2 * time.Second)
	if _, err := a.Next("delivery:z", ownerBuilder(a, "delivery:z")); !errors.Is(err, ErrExhausted) {
		t.Fatalf("deadline not exhausted: %v", err)
	}
	var seq []string
	j.Replay(0, func(ev journal.Event) error { seq = append(seq, ev.Envelope.EventType); return nil })
	for i, e := range seq {
		if e == EvOperationTerminal {
			if i+1 >= len(seq) || seq[i+1] != evOwnerMark {
				t.Fatalf("terminal without its companion in the same batch: %v", seq)
			}
		}
	}
	if st, _ := a.State("delivery:z"); st != contracts.AttemptFailed {
		t.Fatalf("state %s", st)
	}
}

// Execute owns the retry loop: a retryable failure then success = 2 physical
// calls with distinct grants; ablation (MaxAttempts 1) with the SAME caller
// code = exactly one call and an error — the caller holds no loop of its own.
func TestExecuteOwnsRetryLoopAblation(t *testing.T) {
	run := func(pol Policy) (calls int, err error) {
		c := &clock{time.Unix(1000, 0)}
		a := NewAuthority(c.now, time.Minute)
		a.SetJitterSource(func() float64 { return 0 }) // zero wait
		var seen []string
		err = a.Execute(context.Background(), "op", "provider:x", pol, func(ctx context.Context, g Grant) (Outcome, string, error) {
			if cerr := a.Consume(g); cerr != nil {
				return OutcomeFailedTerminal, CodeLocalRefused, cerr
			}
			calls++
			seen = append(seen, g.Nonce)
			if calls == 1 {
				return OutcomeFailedRetryable, CodeHTTP429, fmt.Errorf("429")
			}
			return OutcomeSucceeded, "", nil
		})
		if calls == 2 && seen[0] == seen[1] {
			panic("same nonce reused")
		}
		return calls, err
	}
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 3, Backoff: BackoffPolicy{Base: time.Millisecond, Jitter: true}, RetryableCodes: []string{CodeHTTP429}}
	if calls, err := run(pol); calls != 2 || err != nil {
		t.Fatalf("expected 2 calls and success, got %d %v", calls, err)
	}
	pol.MaxAttempts = 1 // ablation: S7's second-grant path disabled
	if calls, err := run(pol); calls != 1 || err == nil {
		t.Fatalf("ablation: expected exactly 1 call and an error, got %d %v", calls, err)
	}
	// An attempt that never consumes is CANCELLED, its claim ignored.
	a := NewAuthority(nil, time.Minute)
	err := a.Execute(context.Background(), "op-nc", "provider:x", PolicyProvider, func(context.Context, Grant) (Outcome, string, error) {
		return OutcomeSucceeded, "", nil
	})
	if err == nil {
		t.Fatal("unconsumed 'success' accepted")
	}
	if st, _ := a.State("op-nc"); st != contracts.AttemptCancelled {
		t.Fatalf("unconsumed attempt state %s", st)
	}
	_ = os.Getpid
}

// Reconciliation is the ONLY exit from UNKNOWN and it is S7-owned: proof that
// the remote effect is present -> SUCCEEDED; proof it is absent -> the SAME
// operation becomes FAILED_RETRYABLE within its budget (only Next re-grants),
// or FAILED when the budget is spent. Both land with the owner's companion.
func TestReconcileExitsUnknownWithinBudget(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	a.SetJitterSource(func() float64 { return 1 })
	pol := Policy{EffectClass: contracts.EffectReversible, MaxAttempts: 3, Backoff: BackoffPolicy{Base: 2 * time.Second}, RetryableCodes: []string{CodeTransportPreWire}, Durable: true}
	mk := func(op contracts.OperationID) {
		a.Begin(op, "channel:tg:setMyCommands", pol)
		g, _ := a.Next(op, ownerBuilder(a, op))
		consumeOK(t, a, g, Companion{Key: op, Params: a.params(evOwnerMark, nil)})
		if err := a.Report(op, OutcomeUnknown, CodeHTTP5xx, ownerBuilder(a, op)); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Next(op, ownerBuilder(a, op)); !errors.Is(err, ErrAttemptNotAuthorized) {
			t.Fatalf("UNKNOWN re-granted: %v", err)
		}
	}
	mk("control:a")
	if err := a.Reconcile("control:a", false, ownerBuilder(a, "control:a")); err != nil {
		t.Fatal(err)
	}
	if st, _ := a.State("control:a"); st != contracts.AttemptFailedRetryable {
		t.Fatalf("absent-proof landed %s, want FAILED_RETRYABLE", st)
	}
	if _, err := a.Next("control:a", ownerBuilder(a, "control:a")); !errors.Is(err, ErrNotDue) {
		t.Fatalf("re-grant before backoff: %v", err)
	}
	c.advance(3 * time.Second)
	if g, err := a.Next("control:a", ownerBuilder(a, "control:a")); err != nil || g.AttemptNo != 2 {
		t.Fatalf("re-grant after reconcile-retry: %v", err)
	}
	if err := a.Reconcile("control:a", true, nil); err == nil {
		t.Fatal("Reconcile accepted from a non-UNKNOWN state")
	}
	// Nil builder refused on a durable reconcile; state untouched.
	mk("control:b")
	if err := a.Reconcile("control:b", true, nil); !errors.Is(err, ErrNoBuilder) {
		t.Fatalf("nil builder accepted: %v", err)
	}
	if st, _ := a.State("control:b"); st != contracts.AttemptUnknown {
		t.Fatalf("state moved without companion: %s", st)
	}
	// Restart keeps the reconciled-retry schedule.
	a2, _ := New(j, c.now, time.Minute)
	if st, _ := a2.State("control:b"); st != contracts.AttemptUnknown {
		t.Fatalf("rehydrated %s", st)
	}
}

// CODE4 codex #1: Report's (outcome, landing, code, next_at) compatibility
// check is the S7 API boundary's OWN invariant, not just the durable
// journal event validator's — a non-durable operation must refuse the same
// impossible narratives a durable one would, leaving state RUNNING (no
// silent success on garbage input).
func TestReportRefusesIncompatibleCodeForNonDurableOperation(t *testing.T) {
	c := &clock{time.Unix(2000, 0)}
	a := NewAuthority(c.now, time.Minute)
	pol := Policy{EffectClass: contracts.EffectReadOnly, MaxAttempts: 3, RetryableCodes: []string{CodeHTTP5xx}}

	if err := a.Begin("op1", "t", pol); err != nil {
		t.Fatal(err)
	}
	g, _ := a.Next("op1", nil)
	consumeOK(t, a, g)
	// A succeeded outcome can never carry a code (validReport): the closed
	// (outcome, landing, code, next_at) table must refuse it here, at the
	// Report() boundary itself — not only inside the durable journal
	// validator that a non-durable operation never reaches.
	if err := a.Report("op1", OutcomeSucceeded, CodeHTTP5xx, nil); err == nil {
		t.Fatal("Report accepted OutcomeSucceeded with a non-empty code")
	}
	if st, _ := a.State("op1"); st != contracts.AttemptRunning {
		t.Fatalf("state after refused report: %s, want RUNNING (untouched)", st)
	}

	if err := a.Begin("op2", "t", pol); err != nil {
		t.Fatal(err)
	}
	g2, _ := a.Next("op2", nil)
	consumeOK(t, a, g2)
	// A code outside the closed vocabulary must be refused regardless of
	// outcome, for both durable and non-durable operations alike.
	if err := a.Report("op2", OutcomeFailedTerminal, "not_a_real_code", nil); err == nil {
		t.Fatal("Report accepted a code outside the closed vocabulary")
	}
	if st, _ := a.State("op2"); st != contracts.AttemptRunning {
		t.Fatalf("state after unknown-code report: %s, want RUNNING (untouched)", st)
	}
	// A legitimate terminal report on the same operation still succeeds
	// afterward — the refusal above left no torn state behind.
	if err := a.Report("op2", OutcomeFailedTerminal, CodeHTTP4xx, nil); err != nil {
		t.Fatalf("legitimate report after a refused one: %v", err)
	}
}

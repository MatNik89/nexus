//go:build linux

// T22 RED table (tasks-P0): transport-neutral durable channel core
// (HARDQ B2/B7, E15) — durable inbox keyed (adapter_id, channel_identity,
// update_id) RECEIVED→ADMITTED→TERMINAL with exactly-once ADMISSION
// (replay returns the existing outcome); transactional outbox with a
// stable delivery id and at-least-once remote delivery;
// sent-but-unrecorded → UNKNOWN→RECONCILING, never blind retry. The two
// concrete B7 recipes: inbox-admission+journal and terminal-result+outbox
// — each ONE transaction, both halves durable or neither.
package channel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/channel/health"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

// testEvents merges the channel and S7 event sets (Slice B2: the delivery
// lifecycle journals both owners in one batch).
func testEvents() map[string]journal.PayloadValidator {
	m := Events()
	for k, v := range s7.Events() {
		m[k] = v
	}
	return m
}

// fakeClock drives S7 backoff deterministically.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// authFor builds the journal-bound S7 authority for a core (jitter pinned to
// 1 so backoff = Base * 2^(n-1)).
func authFor(t *testing.T, c *Core, clock *fakeClock) *s7.Authority {
	t.Helper()
	a, err := s7.New(c.j, clock.now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	a.SetJitterSource(func() float64 { return 1 })
	return a
}

// sendVia is the test adapter: it CONSUMES the grant with the park
// companion (as the real adapter does immediately before the wire) and then
// returns the scripted transport result. A scripted result may be nil, a
// typed *Failure, or an ErrAmbiguousSend-wrapped error.
func sendVia(auth *s7.Authority, f func(Outbound) error) Send {
	return func(o Outbound, g s7.Grant, park s7.Companion) error {
		if err := auth.Consume(g, park); err != nil {
			return &Failure{Code: s7.CodeLocalRefused, Cause: err}
		}
		return f(o)
	}
}

func preWire() error {
	return &Failure{Code: s7.CodeTransportPreWire, Retryable: true, Cause: fmt.Errorf("network down")}
}

func open(t *testing.T, dir string) (*Core, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, testEvents(), NewProjection(), s7.NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	c, err := New(j)
	if err != nil {
		t.Fatal(err)
	}
	return c, j
}

func inbound(update int64, text string) Inbound {
	return Inbound{
		AdapterID:       "telegram",
		ChannelIdentity: "chat-42",
		UpdateID:        update,
		Text:            text,
		Profile:         "work",
	}
}

// Exactly-once ADMISSION: the same (adapter, identity, update_id) admitted
// twice returns the EXISTING outcome — no second admission, no second
// journal event; a different update admits independently.
func TestExactlyOnceAdmission(t *testing.T) {
	c, j := open(t, t.TempDir())
	out1, err := c.Admit(ctxT(), inbound(1, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if out1.Replayed {
		t.Fatal("first admission marked as replay")
	}
	out2, err := c.Admit(ctxT(), inbound(1, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !out2.Replayed || out2.MessageID != out1.MessageID {
		t.Fatalf("replayed update did not return the existing outcome: %+v vs %+v", out2, out1)
	}
	count := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvInboundAdmitted {
			count++
		}
		return nil
	})
	if count != 1 {
		t.Fatalf("replay produced %d admission events (want 1)", count)
	}
	if out3, _ := c.Admit(ctxT(), inbound(2, "second")); out3.Replayed {
		t.Fatal("a NEW update was treated as replay")
	}
}

// The B7 recipe inbox-admission+journal: SIGKILL inside the recipe →
// on reopen NO inbound row without its journal event and vice versa.
func TestInboxRecipeAtomicUnderSigkill(t *testing.T) {
	if os.Getenv("CHAN_CRASH_CHILD") == "1" {
		inboxCrashChild()
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "TestInboxRecipeAtomicUnderSigkill")
	cmd.Env = append(os.Environ(), "CHAN_CRASH_CHILD=1", "CHAN_CRASH_DIR="+dir,
		"NEXUS_TEST_KILL_MID_BATCH=single")
	if err := cmd.Run(); err == nil {
		t.Fatal("child survived the mid-batch kill seam")
	}
	c, j := open(t, dir)
	rows := c.testInboxRows(t)
	events := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvInboundAdmitted {
			events++
		}
		return nil
	})
	if rows != events {
		t.Fatalf("recipe torn: %d inbox rows vs %d admission events", rows, events)
	}
	// Recovery: the same update admits cleanly now.
	if _, err := c.Admit(ctxT(), inbound(1, "crashy")); err != nil {
		t.Fatalf("post-crash admission: %v", err)
	}
}

func inboxCrashChild() {
	dir := os.Getenv("CHAN_CRASH_DIR")
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, testEvents(), NewProjection(), s7.NewProjection())
	if err != nil {
		os.Exit(1)
	}
	c, err := New(j)
	if err != nil {
		os.Exit(1)
	}
	c.Admit(context.Background(), inbound(1, "crashy")) // dies mid-batch via the seam
	os.Exit(0)
}

// The B7 recipe terminal-result+outbox: enqueueing a reply commits the
// terminal result AND the outbox row together — SIGKILL inside leaves
// both or neither.
func TestOutboxRecipeAtomicUnderSigkill(t *testing.T) {
	if os.Getenv("CHAN_OUTCRASH_CHILD") == "1" {
		outboxCrashChild()
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run", "TestOutboxRecipeAtomicUnderSigkill")
	cmd.Env = append(os.Environ(), "CHAN_OUTCRASH_CHILD=1", "CHAN_CRASH_DIR="+dir,
		"NEXUS_TEST_KILL_MID_BATCH=single")
	if err := cmd.Run(); err == nil {
		t.Fatal("child survived the mid-batch kill seam")
	}
	c, j := open(t, dir)
	pending := len(mustPending(t, c))
	terminals := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvOutboundEnqueued {
			terminals++
		}
		return nil
	})
	if pending != terminals {
		t.Fatalf("recipe torn: %d outbox rows vs %d enqueue events", pending, terminals)
	}
}

func outboxCrashChild() {
	dir := os.Getenv("CHAN_CRASH_DIR")
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, testEvents(), NewProjection(), s7.NewProjection())
	if err != nil {
		os.Exit(1)
	}
	c, err := New(j)
	if err != nil {
		os.Exit(1)
	}
	c.EnqueueReply(context.Background(), "telegram", "chat-42", "work", "the reply") // dies mid-batch
	os.Exit(0)
}

func mustPending(t *testing.T, c *Core) []Outbound {
	t.Helper()
	p, err := c.Pending(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// At-least-once delivery honesty: a transport ERROR keeps the row pending
// for retry; transport ACCEPT marks it sent exactly once; a
// sent-but-unrecorded crash (accept returned, daemon died before the
// journal write) parks the row UNKNOWN → reconciliation, never blind
// retry.
func TestDeliveryHonesty(t *testing.T) {
	c, _ := open(t, t.TempDir())
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "hi"); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{time.Unix(1000, 0)}
	auth := authFor(t, c, clock)
	// DEFINITE pre-wire failure → S7 lands FAILED_RETRYABLE → re-pended
	// for the S7-scheduled retry (nothing resends before it is due).
	fails := 0
	err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error { fails++; return preWire() }))
	if err == nil {
		t.Fatal("transport failure swallowed")
	}
	if len(mustPending(t, c)) != 1 {
		t.Fatal("failed delivery lost the pending row")
	}
	sent := 0
	// Not due yet: NO physical attempt, and the cycle says so (ErrNothingDue —
	// health must not read a no-op as recovery).
	if err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error { sent++; return nil })); !errors.Is(err, ErrNothingDue) || sent != 0 {
		t.Fatalf("resent before S7 said due: sent=%d err=%v", sent, err)
	}
	clock.advance(time.Minute) // past the 5s backoff
	// Accept → sent exactly once; second flush sends nothing.
	if err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error { sent++; return nil })); err != nil && !errors.Is(err, ErrNothingDue) {
		t.Fatal(err)
	}
	if sent != 1 || len(mustPending(t, c)) != 0 {
		t.Fatalf("delivery not exactly-once-marked: sent=%d pending=%d", sent, len(mustPending(t, c)))
	}
	if err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error { sent++; return nil })); err != nil && !errors.Is(err, ErrNothingDue) {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("already-sent row re-delivered: %d", sent)
	}
}

// sent-but-unrecorded: the transport ACCEPTED but the sent-mark append
// failed → the row parks UNKNOWN (never silently retried, never lost).
func TestSentButUnrecordedParksUnknown(t *testing.T) {
	c, _ := open(t, t.TempDir())
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "hi"); err != nil {
		t.Fatal(err)
	}
	auth := authFor(t, c, &fakeClock{time.Unix(1000, 0)})
	c.testFailSentMark = true
	if err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error { return nil })); err == nil {
		t.Fatal("sent-mark failure swallowed")
	}
	c.testFailSentMark = false
	// The row is UNKNOWN: not pending (no blind retry)...
	if len(mustPending(t, c)) != 0 {
		t.Fatal("UNKNOWN row still pending (blind retry possible)")
	}
	// ...and visible for reconciliation.
	unknown, err := c.Unreconciled(ctxT())
	if err != nil || len(unknown) != 1 {
		t.Fatalf("UNKNOWN row not visible for reconciliation: %v %v", unknown, err)
	}
	// Reconciliation with proof-of-send closes it; proof-of-loss re-pends.
	if err := c.ReconcileFor(ctxT(), unknown[0].DeliveryID, true, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if u, _ := c.Unreconciled(ctxT()); len(u) != 0 {
		t.Fatalf("reconciled row still unknown: %v", u)
	}
}

// Profile binding at admission (B3): the inbound row carries its bound
// profile; an admission for an unbound identity is refused fail-closed
// upstream (T23 adapter RED) — here the CORE refuses an empty profile.
func TestAdmissionRequiresProfile(t *testing.T) {
	c, _ := open(t, t.TempDir())
	in := inbound(1, "x")
	in.Profile = ""
	if _, err := c.Admit(ctxT(), in); err == nil {
		t.Fatal("profile-less admission accepted")
	}
	in.Profile = "private" // journal is bound to work
	if _, err := c.Admit(ctxT(), in); err == nil {
		t.Fatal("cross-profile admission accepted")
	}
}

// Visibility: committed recipe rows are readable in the next same-process
// read (B7 read-your-own-writes).
func TestReadYourOwnWrites(t *testing.T) {
	c, _ := open(t, t.TempDir())
	out, err := c.Admit(ctxT(), inbound(7, "visible"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.InboundStatus(ctxT(), out.MessageID)
	if err != nil || st != StateAdmitted {
		t.Fatalf("admitted row not immediately visible: %v %v", st, err)
	}
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "r"); err != nil {
		t.Fatal(err)
	}
	if len(mustPending(t, c)) != 1 {
		t.Fatal("enqueued outbox row not immediately visible")
	}
}

// Restart durability: pending outbox rows and admission dedup survive a
// reopen (the whole point of DURABLE channel state).
func TestChannelStateSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c, j := open(t, dir)
	if _, err := c.Admit(ctxT(), inbound(1, "before")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "pending reply"); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j2, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, testEvents(), NewProjection(), s7.NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	c2, err := New(j2)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := c2.Admit(ctxT(), inbound(1, "before")); err != nil || !out.Replayed {
		t.Fatalf("dedup lost across restart: %+v %v", out, err)
	}
	if len(mustPending(t, c2)) != 1 {
		t.Fatal("pending outbox row lost across restart")
	}
	_ = contracts.ProfileID("work")
	_ = time.Now
}

// UNKNOWN-FIRST honesty (Phase-5 codex #3/#4): an AMBIGUOUS transport
// result (wire touched, outcome unknown) parks the row UNKNOWN — never a
// blind retry; and because the row is parked BEFORE the wire, even a
// dual-mark failure or restart cannot resend it.
func TestAmbiguousSendParksUnknownNeverResends(t *testing.T) {
	dir := t.TempDir()
	c, j := open(t, dir)
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "hi"); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{time.Unix(1000, 0)}
	auth := authFor(t, c, clock)
	accepts := 0
	err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error {
		accepts++ // the wire WAS touched
		return fmt.Errorf("connection reset: %w", ErrAmbiguousSend)
	}))
	if err == nil {
		t.Fatal("ambiguous send swallowed")
	}
	// UNKNOWN, not pending: a second flush sends NOTHING (even long after).
	clock.advance(time.Hour)
	if err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error { accepts++; return nil })); err != nil && !errors.Is(err, ErrNothingDue) {
		t.Fatal(err)
	}
	if accepts != 1 {
		t.Fatalf("ambiguous delivery retried blindly: accepts=%d", accepts)
	}
	// RESTART: still not resent (the parking is durable).
	j.Close()
	c2, _ := open(t, dir)
	clock.advance(time.Hour)
	auth2 := authFor(t, c2, clock)
	if err := c2.Flush(ctxT(), auth2, sendVia(auth2, func(o Outbound) error { accepts++; return nil })); err != nil && !errors.Is(err, ErrNothingDue) {
		t.Fatal(err)
	}
	if accepts != 1 {
		t.Fatalf("ambiguous delivery resent after restart: accepts=%d", accepts)
	}
	if u, _ := c2.Unreconciled(ctxT()); len(u) != 1 {
		t.Fatalf("ambiguous row not awaiting reconciliation: %v", u)
	}
	// A DEFINITE pre-wire failure re-pends for a safe retry.
	if _, err := c2.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "second"); err != nil {
		t.Fatal(err)
	}
	if err := c2.Flush(ctxT(), auth2, sendVia(auth2, func(o Outbound) error { return preWire() })); err == nil {
		t.Fatal("definite failure swallowed")
	}
	p, _ := c2.Pending(ctxT())
	if len(p) != 1 {
		t.Fatalf("definite failure did not re-pend: %v", p)
	}
}

// Inbound TERMINAL lifecycle (Phase-5 codex #2): CompleteInbound is ONE
// batch (terminal + reply); a message admitted but NOT terminal is
// re-runnable; a terminal one is done.
func TestInboundTerminalRecipe(t *testing.T) {
	c, j := open(t, t.TempDir())
	out, err := c.Admit(ctxT(), inbound(1, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := c.InboundStatus(ctxT(), out.MessageID); st != StateAdmitted {
		t.Fatalf("state %v", st)
	}
	if _, err := c.CompleteInbound(ctxT(), out.MessageID, "telegram", "chat-42", "work", "the reply"); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.InboundStatus(ctxT(), out.MessageID); st != StateTerminal {
		t.Fatalf("state %v, want TERMINAL", st)
	}
	if len(mustPending(t, c)) != 1 {
		t.Fatal("reply not enqueued with the terminal")
	}
	// Double-complete aborts (the recipe is once-only).
	if _, err := c.CompleteInbound(ctxT(), out.MessageID, "telegram", "chat-42", "work", "again"); err == nil {
		t.Fatal("double terminal accepted")
	}
	// Replay events → terminal and reply BOTH exist (one batch).
	terminals, enqueues := 0, 0
	j.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case EvInboundTerminal:
			terminals++
		case EvOutboundEnqueued:
			enqueues++
		}
		return nil
	})
	if terminals != 1 || enqueues != 1 {
		t.Fatalf("recipe events: terminals=%d enqueues=%d", terminals, enqueues)
	}
}

// CONCURRENT admissions of one update (Phase-5 codex #11): exactly one
// admission event; both callers converge on the same outcome.
func TestConcurrentAdmissionRace(t *testing.T) {
	c, j := open(t, t.TempDir())
	// Deterministic losing interleaving: BOTH racers pass the dedup check
	// before EITHER appends — only the journal-side UNIQUE backstop can
	// keep admission exactly-once now (removing it turns this RED).
	var arrived sync.WaitGroup
	arrived.Add(2)
	release := make(chan struct{})
	c.testPostCheckHook = func() {
		arrived.Done()
		<-release
	}
	go func() { arrived.Wait(); close(release) }()
	type res struct {
		out AdmitOutcome
		err error
	}
	results := make(chan res, 2)
	for i := 0; i < 2; i++ {
		go func() {
			o, err := c.Admit(ctxT(), inbound(7, "race"))
			results <- res{o, err}
		}()
	}
	ids := map[string]bool{}
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("concurrent admission errored: %v", r.err)
		}
		ids[r.out.MessageID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent admissions diverged: %v", ids)
	}
	count := 0
	j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvInboundAdmitted {
			count++
		}
		return nil
	})
	if count != 1 {
		t.Fatalf("%d admission events (want 1)", count)
	}
}

// POISON HEAD (Phase-5-r2 codex #6): one permanently failing delivery
// must not starve every later row — the flush continues past a definite
// failure and still reports it.
func TestPoisonHeadDoesNotStarve(t *testing.T) {
	c, _ := open(t, t.TempDir())
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-poison", "work", "always fails"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "must deliver"); err != nil {
		t.Fatal(err)
	}
	auth := authFor(t, c, &fakeClock{time.Unix(1000, 0)})
	good := 0
	err := c.Flush(ctxT(), auth, sendVia(auth, func(o Outbound) error {
		if o.ChannelIdentity == "chat-poison" {
			return preWire()
		}
		good++
		return nil
	}))
	if err == nil {
		t.Fatal("poison failure swallowed")
	}
	if good != 1 {
		t.Fatalf("poison head starved the later delivery: good=%d", good)
	}
	// The poison row is re-pended (definite failure), not lost.
	if p, _ := c.Pending(ctxT()); len(p) != 1 || p[0].ChannelIdentity != "chat-poison" {
		t.Fatalf("poison row not re-pended: %v", p)
	}
}

// Detector 2 (plan B): two PENDING rows in one flush receive two DISTINCT
// grants; a reused grant is refused before the wire (the first use consumed
// it); a flush with nothing to attempt is ErrNothingDue, not a success.
func TestTwoRowsDistinctGrantsAndReuseRefused(t *testing.T) {
	c, _ := open(t, t.TempDir())
	clock := &fakeClock{time.Unix(1000, 0)}
	auth := authFor(t, c, clock)
	c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "a")
	c.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "b")
	var grants []s7.Grant
	reuseRefused := 0
	err := c.Flush(ctxT(), auth, func(o Outbound, g s7.Grant, park s7.Companion) error {
		grants = append(grants, g)
		if err := auth.Consume(g, park); err != nil {
			return &Failure{Code: s7.CodeLocalRefused, Cause: err}
		}
		if err := auth.Consume(g, park); errors.Is(err, s7.ErrAttemptNotAuthorized) {
			reuseRefused++ // a second physical use of the same grant is refused
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 2 || grants[0].OperationID == grants[1].OperationID || grants[0].Nonce == grants[1].Nonce {
		t.Fatalf("grants not distinct per row: %+v", grants)
	}
	if reuseRefused != 2 {
		t.Fatalf("grant reuse refused %d/2 times", reuseRefused)
	}
	if err := c.Flush(ctxT(), auth, sendVia(auth, func(Outbound) error { t.Fatal("resend"); return nil })); !errors.Is(err, ErrNothingDue) {
		t.Fatalf("idle flush must be ErrNothingDue, got %v", err)
	}
}

// CODE4 codex #2: ClassOf must find substrate dominance even when the join
// is nested inside a %w wrap (not just a direct top-level join), and must
// normalize an invalid/unrecognized Class to substrate — both BEFORE the
// caller computes fatal, so an unknown class can never fail open.
func TestClassOfNestedJoinAndUnknownClassNormalizeToSubstrate(t *testing.T) {
	transportErr := &ClassifiedError{Class: health.ClassTransport, Code: "t1"}
	substrateErr := &ClassifiedError{Class: health.ClassSubstrate, Code: "s1"}
	nested := fmt.Errorf("outer: %w", errors.Join(transportErr, substrateErr))
	if cls, code := ClassOf(nested); cls != health.ClassSubstrate || code != "s1" {
		t.Fatalf("nested-join substrate dominance: got %s/%s, want substrate/s1", cls, code)
	}

	bogus := &ClassifiedError{Class: health.Class("bogus"), Code: "b1"}
	cls, _ := ClassOf(bogus)
	if cls != health.ClassSubstrate {
		t.Fatalf("unknown class normalized to %s, want substrate (fail closed)", cls)
	}
	if cls.Fatal() != true {
		t.Fatal("substrate-normalized unknown class must be Fatal()")
	}

	// Precedence when substrate is absent: remote_rejected beats transport.
	rejErr := &ClassifiedError{Class: health.ClassRemoteRejected, Code: "r1"}
	mixed := errors.Join(transportErr, rejErr)
	if cls, code := ClassOf(mixed); cls != health.ClassRemoteRejected || code != "r1" {
		t.Fatalf("remote_rejected precedence: got %s/%s, want remote_rejected/r1", cls, code)
	}
}

package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/channel/health"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

// runUntilStop drives Run with a fast ticker until it returns or the
// deadline passes.
func runUntilStop(t *testing.T, h *harness, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.a.Run(ctx, 5*time.Millisecond) }()
	select {
	case err := <-done:
		return err
	case <-time.After(d + time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func entry(t *testing.T, h *harness, component string) health.Entry {
	t.Helper()
	entries, err := health.Read(h.hpath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Component == component {
			return e
		}
	}
	t.Fatalf("no health entry for %s in %+v", component, entries)
	return health.Entry{}
}

// D1: a permanent getUpdates 401 -> health remote_rejected (projection file
// written, stopped), Run RETURNED with the typed class, and across several
// ticker intervals EXACTLY ONE failed poll happened (RED today: a poll every
// tick, error discarded).
func TestPoll401RecordsRemoteRejectedAndStops(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.mu.Lock()
	h.bot.pollStatus, h.bot.pollStatusLeft = 401, 1000
	h.bot.mu.Unlock()
	err := runUntilStop(t, h, 300*time.Millisecond)
	var ce *channel.ClassifiedError
	if !errors.As(err, &ce) || ce.Class != health.ClassRemoteRejected || !errors.Is(err, ErrPollTerminal) {
		t.Fatalf("Run returned %v, want remote_rejected ErrPollTerminal", err)
	}
	e := entry(t, h, "telegram.poll")
	if e.Healthy || e.Class != health.ClassRemoteRejected || !e.Stopped {
		t.Fatalf("health entry %+v", e)
	}
	if h.bot.polls() != 1 {
		t.Fatalf("expected exactly one failed poll, got %d", h.bot.polls())
	}
}

// CODE4 codex #3: a persistently-retryable poll failure (HTTP 500, never
// 401/403) is ClassTransport — cls.Fatal() alone is FALSE for that class —
// yet S7 exhausting the operation's MaxAttempts still STOPS the adapter,
// because record()'s fatal decision also carries an explicit
// errors.Is(err, ErrPollTerminal) arm. Ablating that OR arm must leave this
// test RED (verified by hand: commenting it out fails the Stopped assertion
// below, since ClassTransport.Fatal() returns false).
func TestPollRetryableExhaustionStopsViaErrPollTerminal(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.mu.Lock()
	h.bot.pollStatus, h.bot.pollStatusLeft = 500, 1000
	h.bot.mu.Unlock()
	var last error
	for i := 0; i < s7.PolicyPoll.MaxAttempts*2; i++ {
		last = h.a.PollOnce(ctxT())
		tgClock.advance(time.Hour)
		if errors.Is(last, ErrPollTerminal) {
			break
		}
	}
	if !errors.Is(last, ErrPollTerminal) {
		t.Fatalf("poll never reached terminal exhaustion: %v", last)
	}
	var ce *channel.ClassifiedError
	if !errors.As(last, &ce) || ce.Class != health.ClassTransport {
		t.Fatalf("exhausted poll classified %v, want ClassTransport (not remote_rejected)", last)
	}
	if ce.Class.Fatal() {
		t.Fatal("precondition broken: ClassTransport must NOT be Fatal() by class alone, or this detector proves nothing")
	}
	rerr := h.a.record(ctxT(), "telegram.poll", last)
	if rerr == nil {
		t.Fatal("record() did not stop the adapter on a terminal (exhausted, non-remote-rejected) poll")
	}
	e := entry(t, h, "telegram.poll")
	if !e.Stopped || e.Class != health.ClassTransport {
		t.Fatalf("health entry %+v, want stopped transport", e)
	}
}

// D3: an outbox transport failure degrades health (transport, not stopped)
// and the adapter keeps running; a later success RECOVERS the component.
func TestOutboxTransportFailureDegradesThenRecovers(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	enqueue(t, h, "plain")
	h.bot.mu.Lock()
	h.bot.sendStatus, h.bot.sendStatusLeft = 429, 1
	h.bot.mu.Unlock()
	// One cycle: the 429 is recorded as a NON-fatal transport degradation
	// (health = the last cycle's outcome; the pending row stays visible in
	// the outbox), and the adapter keeps running.
	if err := h.a.record(ctxT(), "telegram.outbox", h.a.FlushOutbox(ctxT())); err != nil {
		t.Fatalf("transport failure classified fatal: %v", err)
	}
	e := entry(t, h, "telegram.outbox")
	if e.Healthy || e.Class != health.ClassTransport || e.Code != s7.CodeHTTP429 || e.Stopped {
		t.Fatalf("degraded entry %+v", e)
	}
	if err := runUntilStop(t, h, 60*time.Millisecond); err != nil {
		t.Fatalf("transport failure stopped the adapter: %v", err)
	}
	tgClock.advance(time.Minute)
	if err := runUntilStop(t, h, 60*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if e := entry(t, h, "telegram.outbox"); !e.Healthy {
		t.Fatalf("outbox did not recover after the S7-scheduled resend: %+v", e)
	}
	if sends(h) != 1 {
		t.Fatalf("resend count %d", sends(h))
	}
}

// D4: a broken durable substrate (journal closed under the adapter) ->
// health substrate, Run RETURNS, and no further poll/flush wire calls.
func TestSubstrateFailureStopsAdapter(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	enqueue(t, h, "plain")
	h.j.Close()
	before := h.bot.polls()
	err := runUntilStop(t, h, 300*time.Millisecond)
	var ce *channel.ClassifiedError
	if !errors.As(err, &ce) || ce.Class != health.ClassSubstrate {
		t.Fatalf("Run returned %v, want substrate", err)
	}
	entries, rerr := health.Read(h.hpath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	stopped := false
	for _, e := range entries {
		if !e.Healthy && e.Class == health.ClassSubstrate && e.Stopped {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("no substrate stop recorded: %+v", entries)
	}
	if h.bot.polls()-before > 1 || sends(h) != 0 {
		t.Fatalf("channel work continued on a broken substrate: polls=%d sends=%d", h.bot.polls()-before, sends(h))
	}
}

// D5: classification is TYPED and survives wrapping; an error with no class
// is substrate (fatal, fail closed); a transport class stays non-fatal; a
// joined tree with any substrate member is substrate.
func TestClassOfTable(t *testing.T) {
	cases := []struct {
		err   error
		class health.Class
		fatal bool
	}{
		{nil, "", false},
		{fmt.Errorf("plain journal failure"), health.ClassSubstrate, true},
		{fmt.Errorf("wrapped: %w", &channel.ClassifiedError{Class: health.ClassTransport, Code: "http_429"}), health.ClassTransport, false},
		{fmt.Errorf("deep: %w", fmt.Errorf("er: %w", &channel.ClassifiedError{Class: health.ClassRemoteRejected, Code: "http_4xx"})), health.ClassRemoteRejected, true},
		{errors.Join(&channel.ClassifiedError{Class: health.ClassTransport}, &channel.ClassifiedError{Class: health.ClassSubstrate, Code: "landing"}), health.ClassSubstrate, true},
		// A bare typed Failure classifies by its own fields (a 404 on
		// setMyCommands degrades, it never stops the adapter).
		{&channel.Failure{Code: "http_4xx", Status: 404}, health.ClassTransport, false},
		{&channel.Failure{Code: "http_4xx", Status: 401}, health.ClassRemoteRejected, true},
		{&channel.Failure{Code: "receipt_not_durable"}, health.ClassSubstrate, true},
	}
	for i, tc := range cases {
		cls, _ := channel.ClassOf(tc.err)
		if cls != tc.class || (cls != "" && cls.Fatal() != tc.fatal) {
			t.Fatalf("case %d: class %q fatal=%v, want %q/%v", i, cls, cls.Fatal(), tc.class, tc.fatal)
		}
	}
}

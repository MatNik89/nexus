package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

// Slice B2 detectors: every Telegram Bot API call is S7-governed. The
// fakeBot scripts HTTP statuses; tgClock drives S7 backoff.

func enqueue(t *testing.T, h *harness, text string) string {
	t.Helper()
	id, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", text)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sends(h *harness) int {
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	return len(h.bot.sent)
}

// Detector 3: a permanent HTTP 400 on a PLAIN send is a terminal code — the
// row parks FAILED after ONE attempt and N later ticks produce ZERO wire
// calls (RED against the old re-pend-every-tick pump).
func TestPermanent400ParksFailedNoResend(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	id := enqueue(t, h, "plain text")
	h.bot.mu.Lock()
	h.bot.sendStatus, h.bot.sendStatusLeft = 400, 100
	h.bot.mu.Unlock()
	_ = h.a.FlushOutbox(ctxT())
	for i := 0; i < 5; i++ {
		tgClock.advance(time.Hour)
		_ = h.a.FlushOutbox(ctxT())
	}
	h.bot.mu.Lock()
	left := h.bot.sendStatusLeft
	h.bot.mu.Unlock()
	if 100-left != 1 {
		t.Fatalf("expected exactly 1 wire call for a terminal 400, got %d", 100-left)
	}
	failed, _ := h.core.FailedFor(ctxT(), "telegram", "chat-42")
	if len(failed) != 1 || failed[0].DeliveryID != id || failed[0].LastCode != s7.CodeHTTP4xx {
		t.Fatalf("row not parked FAILED with http_4xx: %+v", failed)
	}
	if st, _ := h.auth.State(channel.OperationFor(failed[0])); st != contracts.AttemptFailed {
		t.Fatalf("S7 state %s, want FAILED", st)
	}
	if p, _ := h.core.Pending(ctxT()); len(p) != 0 {
		t.Fatalf("FAILED row still pending: %v", p)
	}
}

// Detector 4: a transient 429 -> FAILED_RETRYABLE, row PENDING, ZERO wire
// calls before next_attempt_at, exactly one resend when due -> SENT.
func TestTransient429RetriedWhenDueWithNewGrant(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	enqueue(t, h, "plain text")
	h.bot.mu.Lock()
	h.bot.sendStatus, h.bot.sendStatusLeft = 429, 1
	h.bot.mu.Unlock()
	_ = h.a.FlushOutbox(ctxT())
	p, _ := h.core.Pending(ctxT())
	if len(p) != 1 || p[0].LastCode != s7.CodeHTTP429 {
		t.Fatalf("429 did not re-pend with its code: %+v", p)
	}
	op := channel.OperationFor(p[0])
	if st, _ := h.auth.State(op); st != contracts.AttemptFailedRetryable {
		t.Fatalf("S7 state %s, want FAILED_RETRYABLE", st)
	}
	// Not due yet: zero wire calls.
	_ = h.a.FlushOutbox(ctxT())
	if sends(h) != 0 {
		t.Fatal("resent before S7 said due")
	}
	tgClock.advance(time.Minute)
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if sends(h) != 1 {
		t.Fatalf("expected exactly one resend when due, got %d", sends(h))
	}
	if st, _ := h.auth.State(op); st != contracts.AttemptSucceeded || h.auth.Attempts(op) != 2 {
		t.Fatalf("S7 %s attempts=%d, want SUCCEEDED/2", st, h.auth.Attempts(op))
	}
	if p, _ := h.core.Pending(ctxT()); len(p) != 0 {
		t.Fatal("row still pending after accept")
	}
}

// Detector 0c: HTTP 5xx on an effectful send -> UNKNOWN (E9), ZERO later wire
// calls however long we wait; S7 UNKNOWN; visible for reconciliation.
func TestSend5xxParksUnknownNeverResends(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	enqueue(t, h, "plain text")
	h.bot.mu.Lock()
	h.bot.sendStatus, h.bot.sendStatusLeft = 502, 100
	h.bot.mu.Unlock()
	_ = h.a.FlushOutbox(ctxT())
	for i := 0; i < 4; i++ {
		tgClock.advance(24 * time.Hour)
		_ = h.a.FlushOutbox(ctxT())
	}
	h.bot.mu.Lock()
	calls := 100 - h.bot.sendStatusLeft
	h.bot.mu.Unlock()
	if calls != 1 {
		t.Fatalf("ambiguous 5xx was retried: %d wire calls", calls)
	}
	u, _ := h.core.Unreconciled(ctxT())
	if len(u) != 1 {
		t.Fatalf("row not UNKNOWN: %v", u)
	}
	if st, _ := h.auth.State(channel.OperationFor(u[0])); st != contracts.AttemptUnknown {
		t.Fatalf("S7 state %s, want UNKNOWN", st)
	}
}

// Detector 5 + 6: MaxAttempts retryable failures exhaust the budget -> FAILED,
// zero further wire calls; the count survives a restart (rehydrated S7).
func TestExhaustionAcrossRestartParksFailed(t *testing.T) {
	dir := t.TempDir()
	h, _ := buildAt(t, dir, map[int64]string{42: "work"})
	enqueue(t, h, "plain text")
	h.bot.mu.Lock()
	h.bot.sendStatus, h.bot.sendStatusLeft = 429, 1000
	h.bot.mu.Unlock()
	max := s7.PolicyDelivery.MaxAttempts
	// Half the attempts before the "restart".
	for i := 0; i < max/2; i++ {
		_ = h.a.FlushOutbox(ctxT())
		tgClock.advance(time.Hour)
	}
	h.j.Close()
	h2, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h2.bot.mu.Lock()
	h2.bot.sendStatus, h2.bot.sendStatusLeft = 429, 1000
	h2.bot.mu.Unlock()
	for i := 0; i < max*2; i++ {
		_ = h2.a.FlushOutbox(ctxT())
		tgClock.advance(time.Hour)
	}
	h2.bot.mu.Lock()
	calls := 1000 - h2.bot.sendStatusLeft
	h2.bot.mu.Unlock()
	if calls != max-max/2 {
		t.Fatalf("after restart expected %d remaining attempts, got %d (cap reset?)", max-max/2, calls)
	}
	failed, _ := h2.core.FailedFor(ctxT(), "telegram", "chat-42")
	if len(failed) != 1 {
		t.Fatalf("exhausted row not FAILED: %v", failed)
	}
	if p, _ := h2.core.Pending(ctxT()); len(p) != 0 {
		t.Fatal("exhausted row still pending")
	}
}

// Detector 9d/0b: a grant bound to another delivery (or none) never reaches
// the wire — the identity check precedes Consume; the row lands FAILED as a
// local refusal, and the other delivery's grant stays usable.
func TestForeignGrantRefusedBeforeWire(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	idA := enqueue(t, h, "a")
	rows, _ := h.core.Pending(ctxT())
	oA := rows[0]
	opA, tA := channel.OperationFor(oA), channel.TargetFor(oA)
	if err := h.auth.Begin(opA, tA, s7.PolicyDelivery); err != nil {
		t.Fatal(err)
	}
	gA, err := h.auth.Next(opA, func(s7.Landing) s7.Companion { return s7.Companion{} })
	if err != nil {
		t.Fatal(err)
	}
	// Present A's grant for a DIFFERENT operation/target: refused pre-consume.
	oB := oA
	oB.DeliveryID = "dlv-other"
	err = h.a.call(ctxT(), "sendMessage", map[string]any{"chat_id": 42, "text": "x"}, nil, gA, kindDelivery,
		channel.OperationFor(oB), channel.TargetFor(oB))
	var f *channel.Failure
	if !errors.As(err, &f) || f.Code != s7.CodeLocalRefused || !errors.Is(err, s7.ErrAttemptNotAuthorized) {
		t.Fatalf("foreign grant not refused as a local refusal: %v", err)
	}
	if sends(h) != 0 {
		t.Fatal("wire touched with a foreign grant")
	}
	if st, _ := h.auth.State(opA); st != contracts.AttemptAuthorized {
		t.Fatalf("A's grant was consumed by the refusal: %s", st)
	}
	_ = idA
}

// Detector D6/D7-ish (poll): a 5xx on getUpdates is a RETRYABLE read — zero
// polls before next_attempt_at, exactly one when due; a 401 is terminal:
// ErrPollTerminal and no further polls.
func TestPollGovernedRetryAndTerminal(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.mu.Lock()
	h.bot.pollStatus, h.bot.pollStatusLeft = 503, 1
	h.bot.mu.Unlock()
	if err := h.a.PollOnce(ctxT()); err == nil {
		t.Fatal("503 poll reported success")
	}
	before := h.bot.polls()
	_ = h.a.PollOnce(ctxT()) // not due
	if h.bot.polls() != before {
		t.Fatal("poll retried before S7 said due")
	}
	tgClock.advance(time.Minute)
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatalf("due poll failed: %v", err)
	}
	if h.bot.polls() != before+1 {
		t.Fatalf("expected exactly one poll when due, got %d", h.bot.polls()-before)
	}
	// Terminal 401: stops.
	h.bot.mu.Lock()
	h.bot.pollStatus, h.bot.pollStatusLeft = 401, 100
	h.bot.mu.Unlock()
	err := h.a.PollOnce(ctxT())
	if !errors.Is(err, ErrPollTerminal) {
		t.Fatalf("401 not terminal: %v", err)
	}
	before = h.bot.polls()
	for i := 0; i < 3; i++ {
		tgClock.advance(time.Hour)
		if err := h.a.PollOnce(ctxT()); !errors.Is(err, ErrPollTerminal) {
			t.Fatalf("poll resumed after terminal: %v", err)
		}
	}
	if h.bot.polls() != before {
		t.Fatal("polled again after a terminal 401")
	}
}

// Detector D2/D2b/D2c/D2d: command registration is a durable bot-bound S7
// operation. Same-bot restart: zero setMyCommands. Ambiguous 5xx -> UNKNOWN,
// zero retries; restart -> reconciliation through getMyCommands (equal ->
// SUCCEEDED, zero set calls). Bot rotation: bot B registers once; bot A's
// UNKNOWN is untouched. A foreign proof is refused.
func TestRegistrationDurableBotBoundReconciled(t *testing.T) {
	dir := t.TempDir()
	h, _ := buildAt(t, dir, map[int64]string{42: "work"})
	if err := h.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if h.bot.setCalls() != 1 {
		t.Fatalf("first registration: %d calls", h.bot.setCalls())
	}
	// Same bot, same menu, repeated ticks + restart: zero further calls.
	for i := 0; i < 3; i++ {
		if err := h.a.registerCommands(ctxT()); err != nil {
			t.Fatal(err)
		}
	}
	h.j.Close()
	h2, _ := buildAt(t, dir, map[int64]string{42: "work"})
	if err := h2.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if h2.bot.setCalls() != 0 {
		t.Fatalf("same-bot restart re-registered: %d calls", h2.bot.setCalls())
	}
	// Bot B on the SAME journal: a different durable operation -> exactly one
	// governed call; bot A's record untouched.
	h2.j.Close()
	h3, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h3.bot.mu.Lock()
	h3.bot.botID = 2
	h3.bot.mu.Unlock()
	if err := h3.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if h3.bot.setCalls() != 1 {
		t.Fatalf("bot B did not register exactly once: %d", h3.bot.setCalls())
	}
	// Ambiguous 5xx (bot 3): UNKNOWN; in-process the ONLY exit is
	// reconciliation through getMyCommands (remote equal -> SUCCEEDED with
	// ZERO further setMyCommands); a restart sees SUCCEEDED and makes no call.
	h3.j.Close()
	h4, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h4.bot.mu.Lock()
	h4.bot.botID = 3
	h4.bot.setStatus, h4.bot.setStatusLeft = 502, 1
	h4.bot.remoteCommands = mustJSON(commandMenu())
	h4.bot.mu.Unlock()
	if err := h4.a.registerCommands(ctxT()); err == nil {
		t.Fatal("502 registration reported success")
	}
	if st, _ := h4.auth.State(channel.ControlOperation("tg", 3, "setMyCommands", menuHash())); st != contracts.AttemptUnknown {
		t.Fatalf("ambiguous registration landed %s, want UNKNOWN", st)
	}
	tgClock.advance(time.Hour)
	if err := h4.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if h4.bot.setCalls() != 1 || h4.bot.getMyCommandsCalls != 1 {
		t.Fatalf("reconciliation path: set=%d get=%d (want 1/1)", h4.bot.setCalls(), h4.bot.getMyCommandsCalls)
	}
	h4.j.Close()
	h5, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h5.bot.mu.Lock()
	h5.bot.botID = 3
	h5.bot.mu.Unlock()
	if err := h5.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if h5.bot.setCalls() != 0 || h5.bot.getMyCommandsCalls != 0 {
		t.Fatalf("reconciled registration re-touched after restart: set=%d get=%d", h5.bot.setCalls(), h5.bot.getMyCommandsCalls)
	}
	// Bot 4: 5xx -> UNKNOWN; restart with a DIFFERENT remote menu -> the same
	// operation becomes retryable and exactly ONE new governed setMyCommands
	// happens when due.
	h5.j.Close()
	h6, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h6.bot.mu.Lock()
	h6.bot.botID = 4
	h6.bot.setStatus, h6.bot.setStatusLeft = 502, 1
	h6.bot.mu.Unlock()
	_ = h6.a.registerCommands(ctxT())
	h6.j.Close()
	h7, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h7.bot.mu.Lock()
	h7.bot.botID = 4
	h7.bot.remoteCommands = `[{"command":"stale","description":"old"}]`
	h7.bot.mu.Unlock()
	_ = h7.a.registerCommands(ctxT()) // reconcile -> FAILED_RETRYABLE (not due yet)
	if h7.bot.setCalls() != 0 {
		t.Fatal("re-registered before S7 said due")
	}
	tgClock.advance(time.Hour)
	if err := h7.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if h7.bot.setCalls() != 1 {
		t.Fatalf("differing remote menu: want exactly one setMyCommands, got %d", h7.bot.setCalls())
	}
	// A proof from another bot is refused before reconciliation.
	op := channel.ControlOperation("tg", 3, "setMyCommands", "deadbeef")
	if _, err := channel.VerifyControlProof(op, "tg", "setMyCommands", channel.ControlProof{BotID: 2, PayloadHash: "deadbeef", RemoteEqual: true}); err == nil {
		t.Fatal("foreign-bot proof accepted")
	}
	if _, err := channel.VerifyControlProof(op, "tg", "setMyCommands", channel.ControlProof{BotID: 3, PayloadHash: "other", RemoteEqual: true}); err == nil {
		t.Fatal("foreign-hash proof accepted")
	}
	_ = context.Background
}

func menuHash() string {
	raw, _ := json.Marshal(map[string]any{"commands": commandMenu()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

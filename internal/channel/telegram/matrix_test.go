package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/channel/health"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

func faultOn(types ...string) func([]string) error {
	return func(batch []string) error {
		for _, b := range batch {
			for _, t := range types {
				if b == t {
					return errors.New("injected append fault")
				}
			}
		}
		return nil
	}
}

// Detector 9d (full table): a valid grant minted for one kind presented under
// another kind/method — or a method outside its kind — never reaches the
// wire (refused BEFORE Consume; the grant stays usable for its real kind).
func TestGrantKindMethodSubstitutionTable(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	// A real UI grant and a real delivery grant.
	uiOp, uiTarget := contracts.OperationID("ui:tg:sendChatAction:1"), contracts.TargetID("channel:tg:chat:42")
	if err := h.auth.Begin(uiOp, uiTarget, s7.PolicyUI); err != nil {
		t.Fatal(err)
	}
	uiGrant, _ := h.auth.Next(uiOp, nil)
	pollOp := contracts.OperationID("poll:tg:1")
	h.auth.Begin(pollOp, pollTarget, s7.PolicyPoll)
	pollGrant, _ := h.auth.Next(pollOp, nil)
	id := enqueue(t, h, "x")
	rows, _ := h.core.Pending(ctxT())
	dOp, dTarget := channel.OperationFor(rows[0]), channel.TargetFor(rows[0])
	h.auth.Begin(dOp, dTarget, s7.PolicyDelivery)
	dGrant, _ := h.auth.Next(dOp, func(s7.Landing) s7.Companion { return s7.Companion{} })
	_ = id
	cases := []struct {
		name   string
		g      s7.Grant
		method string
		kind   callKind
		op     contracts.OperationID
		target contracts.TargetID
	}{
		{"ui-grant-as-delivery-sendMessage", uiGrant, "sendMessage", kindDelivery, uiOp, uiTarget},
		{"ui-grant-as-control-effect", uiGrant, "setMyCommands", kindControlEffect, uiOp, uiTarget},
		{"ui-grant-wrong-method", uiGrant, "getUpdates", kindUI, uiOp, uiTarget},
		{"poll-grant-as-delivery", pollGrant, "sendMessage", kindDelivery, pollOp, pollTarget},
		{"poll-grant-as-control-read", pollGrant, "getMe", kindControlRead, pollOp, pollTarget},
		{"delivery-grant-as-poll", dGrant, "getUpdates", kindPoll, dOp, dTarget},
		{"delivery-grant-as-ui", dGrant, "sendMessage", kindUI, dOp, dTarget},
		{"delivery-grant-wrong-method", dGrant, "getMe", kindDelivery, dOp, dTarget},
		{"delivery-without-companion", dGrant, "sendMessage", kindDelivery, dOp, dTarget},
	}
	for _, tc := range cases {
		before := h.bot.polls() + sends(h) + h.bot.getMes() + h.bot.setCalls()
		err := h.a.call(ctxT(), tc.method, map[string]any{"chat_id": 42, "text": "x", "offset": 0}, nil, tc.g, tc.kind, tc.op, tc.target)
		if !errors.Is(err, s7.ErrAttemptNotAuthorized) {
			t.Fatalf("%s: not refused: %v", tc.name, err)
		}
		if after := h.bot.polls() + sends(h) + h.bot.getMes() + h.bot.setCalls(); after != before {
			t.Fatalf("%s: wire touched", tc.name)
		}
	}
	// The grants were never consumed by the refusals.
	for _, op := range []contracts.OperationID{uiOp, pollOp, dOp} {
		if st, _ := h.auth.State(op); st != contracts.AttemptAuthorized {
			t.Fatalf("%s consumed by a refused substitution: %s", op, st)
		}
	}
}

// Detector D6 table: every transient poll failure class — pre-wire (dead
// endpoint), 429, 5xx, post-write reset — lands FAILED_RETRYABLE, produces
// ZERO polls before next_attempt_at, then exactly one fresh-grant poll when
// due which succeeds.
func TestPollTransientTable(t *testing.T) {
	cases := []struct {
		name string
		arm  func(h *harness)
		code string
	}{
		{"pre-wire", func(h *harness) { h.a.base = "http://127.0.0.1:1" }, s7.CodeTransportPreWire},
		{"http-429", func(h *harness) { h.bot.mu.Lock(); h.bot.pollStatus, h.bot.pollStatusLeft = 429, 1; h.bot.mu.Unlock() }, s7.CodeHTTP429},
		{"http-5xx", func(h *harness) { h.bot.mu.Lock(); h.bot.pollStatus, h.bot.pollStatusLeft = 503, 1; h.bot.mu.Unlock() }, s7.CodeHTTP5xx},
		{"post-write-reset", func(h *harness) { h.bot.mu.Lock(); h.bot.pollCloseLeft = 1; h.bot.mu.Unlock() }, s7.CodeTransportPostWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := build(t, map[int64]string{42: "work"})
			live := h.a.base
			tc.arm(h)
			err := h.a.PollOnce(ctxT())
			var ce *channel.ClassifiedError
			if !errors.As(err, &ce) || ce.Class != health.ClassTransport || ce.Code != tc.code {
				t.Fatalf("classification: %v", err)
			}
			if st, _ := h.auth.State(h.a.pollOp); st != contracts.AttemptFailedRetryable {
				t.Fatalf("S7 state %s, want FAILED_RETRYABLE", st)
			}
			h.a.base = live
			before := h.bot.polls()
			if err := h.a.PollOnce(ctxT()); !errors.Is(err, channel.ErrNothingDue) {
				t.Fatalf("poll before due: %v", err)
			}
			if h.bot.polls() != before {
				t.Fatal("polled before S7 said due")
			}
			tgClock.advance(time.Hour)
			if err := h.a.PollOnce(ctxT()); err != nil {
				t.Fatalf("due poll: %v", err)
			}
			if h.bot.polls() != before+1 {
				t.Fatalf("expected exactly one poll when due, got %d", h.bot.polls()-before)
			}
		})
	}
}

// Detector D7 table: getMe (control-read) 5xx and post-write reset are
// retried by S7's Execute exactly once due (the fake clock is advanced during
// the S7-owned wait), total two calls; the same failures on setMyCommands
// land UNKNOWN with zero retries (D2, effectful).
func TestControlReadRetryTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(h *harness)
	}{
		{"getMe-5xx", func(h *harness) {
			h.bot.mu.Lock()
			h.bot.getMeStatus, h.bot.getMeStatusLeft = 503, 1
			h.bot.mu.Unlock()
		}},
		{"getMe-post-write", func(h *harness) { h.bot.mu.Lock(); h.bot.getMeCloseLeft = 1; h.bot.mu.Unlock() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := build(t, map[int64]string{42: "work"})
			tc.arm(h)
			done := make(chan error, 1)
			var me struct {
				ID int64 `json:"id"`
			}
			go func() { done <- h.a.controlRead(ctxT(), "getMe", map[string]any{}, &me) }()
			time.Sleep(400 * time.Millisecond)
			if h.bot.getMes() != 1 {
				t.Fatalf("retried before S7 backoff elapsed: %d calls", h.bot.getMes())
			}
			tgClock.advance(time.Hour) // the S7-owned wait observes the due time
			select {
			case err := <-done:
				if err != nil || me.ID != 1 || h.bot.getMes() != 2 {
					t.Fatalf("retry when due: err=%v id=%d calls=%d", err, me.ID, h.bot.getMes())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("controlRead did not complete")
			}
		})
	}
	// Effectful control: 5xx on setMyCommands -> UNKNOWN, zero retries across
	// any number of due ticks (the only exit is reconciliation).
	h := build(t, map[int64]string{42: "work"})
	h.bot.mu.Lock()
	h.bot.setStatus, h.bot.setStatusLeft = 503, 1
	h.bot.mu.Unlock()
	_ = h.a.registerCommands(ctxT())
	op := channel.ControlOperation("tg", 1, "setMyCommands", menuHash())
	if st, _ := h.auth.State(op); st != contracts.AttemptUnknown {
		t.Fatalf("effectful 5xx landed %s, want UNKNOWN", st)
	}
	h.bot.mu.Lock()
	h.bot.remoteCommands = `[{"command":"stale","description":"x"}]`
	h.bot.mu.Unlock()
	// Reconciliation (remote differs) is the honest path: it does NOT blindly
	// re-send; it lands the same operation retryable and S7 schedules it.
	_ = h.a.registerCommands(ctxT())
	if h.bot.setCalls() != 1 {
		t.Fatalf("blind re-send after UNKNOWN: %d", h.bot.setCalls())
	}
}

// Detector D2 (Run level): a setMyCommands 5xx degrades telegram.register
// (transport, UNKNOWN in S7) while polling and delivery CONTINUE; with the
// remote menu equal to the desired set, reconciliation succeeds read-only.
func TestRegistration5xxDegradesButAdapterContinues(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.mu.Lock()
	h.bot.setStatus, h.bot.setStatusLeft = 502, 1
	h.bot.remoteCommands = mustJSON(commandMenu())
	h.bot.batches = [][]map[string]any{{textUpdate(1, 42, "hi")}}
	h.bot.mu.Unlock()
	if err := runUntilStop(t, h, 250*time.Millisecond); err != nil {
		t.Fatalf("transport degradation stopped the adapter: %v", err)
	}
	if h.bot.polls() == 0 || len(h.got) != 1 {
		t.Fatalf("polling/handling did not continue: polls=%d got=%d", h.bot.polls(), len(h.got))
	}
	if h.bot.setCalls() != 1 {
		t.Fatalf("setMyCommands retried blindly: %d", h.bot.setCalls())
	}
	op := channel.ControlOperation("tg", 1, "setMyCommands", menuHash())
	if st, _ := h.auth.State(op); st != contracts.AttemptSucceeded {
		t.Fatalf("reconciliation did not land SUCCEEDED: %s", st)
	}
	entries, _ := health.Read(h.hpath)
	seen := false
	for _, e := range entries {
		if e.Component == "telegram.register" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("register component never recorded: %+v", entries)
	}
}

// Detector D2b (torn batch) + D2d (provenance): a faulted reconcile batch
// leaves S7 UNKNOWN and the control_effect row UNKNOWN (nothing torn); after
// the journal recovers reconciliation lands. Bot A's UNKNOWN registration is
// never resolved by bot B: B registers exactly once under its own operation.
func TestReconcileTornBatchAndProvenance(t *testing.T) {
	dir := t.TempDir()
	h, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h.bot.mu.Lock()
	h.bot.botID = 6
	h.bot.setStatus, h.bot.setStatusLeft = 502, 1
	h.bot.remoteCommands = mustJSON(commandMenu())
	h.bot.mu.Unlock()
	_ = h.a.registerCommands(ctxT())
	opA := channel.ControlOperation("tg", 6, "setMyCommands", menuHash())
	if st, _ := h.auth.State(opA); st != contracts.AttemptUnknown {
		t.Fatalf("A not UNKNOWN: %s", st)
	}
	h.auth.SetAppendFault(faultOn(s7.EvOperationReconcile))
	if err := h.a.registerCommands(ctxT()); !errors.Is(err, s7.ErrNotDurable) {
		t.Fatalf("torn reconcile not surfaced: %v", err)
	}
	if st, _ := h.auth.State(opA); st != contracts.AttemptUnknown {
		t.Fatalf("S7 moved without the companion: %s", st)
	}
	if st, _ := h.core.ControlEffectState(ctxT(), opA); st != "UNKNOWN" {
		t.Fatalf("control_effect row moved without S7: %s", st)
	}
	h.auth.SetAppendFault(nil)
	if err := h.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.auth.State(opA); st != contracts.AttemptSucceeded || h.bot.setCalls() != 1 {
		t.Fatalf("recovered reconcile: %s set=%d", st, h.bot.setCalls())
	}
	// Provenance: bot 8 lands UNKNOWN; restart as bot 9 with the remote menu
	// equal to the desired set -> 8 stays UNKNOWN (no Reconcile), 9 makes ONE call.
	h.j.Close()
	h2, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h2.bot.mu.Lock()
	h2.bot.botID = 8
	h2.bot.setStatus, h2.bot.setStatusLeft = 502, 1
	h2.bot.mu.Unlock()
	_ = h2.a.registerCommands(ctxT())
	op8 := channel.ControlOperation("tg", 8, "setMyCommands", menuHash())
	h2.j.Close()
	h3, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h3.bot.mu.Lock()
	h3.bot.botID = 9
	h3.bot.remoteCommands = mustJSON(commandMenu())
	h3.bot.mu.Unlock()
	if err := h3.a.registerCommands(ctxT()); err != nil {
		t.Fatal(err)
	}
	if st, _ := h3.auth.State(op8); st != contracts.AttemptUnknown {
		t.Fatalf("bot 8's UNKNOWN was touched by bot 9: %s", st)
	}
	if h3.bot.setCalls() != 1 || h3.bot.getMyCommandsCalls != 0 {
		t.Fatalf("bot 9: set=%d getMyCommands=%d (want 1/0)", h3.bot.setCalls(), h3.bot.getMyCommandsCalls)
	}
}

// Detector D4 (per mark): a fault on EACH outbox mark batch — the STARTED+park
// (outbound_unknown), the SENT landing, the re-pend and the FAILED landing —
// is a SUBSTRATE failure: Flush returns it typed, the row is never half-landed,
// and Run stops.
func TestOutboxMarkFaultsAreSubstrate(t *testing.T) {
	cases := []struct {
		name  string
		event string
		arm   func(h *harness)
		row   string // expected outbox status after the faulted cycle
	}{
		{"park", channel.EvOutboundUnknown, func(*harness) {}, "PENDING"},
		{"sent", channel.EvOutboundSent, func(*harness) {}, "UNKNOWN"},
		{"repend", channel.EvOutboundRepend, func(h *harness) { h.bot.mu.Lock(); h.bot.sendStatus, h.bot.sendStatusLeft = 429, 1; h.bot.mu.Unlock() }, "UNKNOWN"},
		{"failed", channel.EvOutboundFailed, func(h *harness) { h.bot.mu.Lock(); h.bot.sendStatus, h.bot.sendStatusLeft = 400, 1; h.bot.mu.Unlock() }, "UNKNOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := build(t, map[int64]string{42: "work"})
			enqueue(t, h, "plain")
			tc.arm(h)
			h.auth.SetAppendFault(faultOn(tc.event))
			// Driven THROUGH Run: the faulted outbox cycle is substrate and
			// STOPS the adapter (typed return), the row is never half-landed.
			err := runUntilStop(t, h, 2*time.Second)
			cls, _ := channel.ClassOf(err)
			if err == nil || cls != health.ClassSubstrate {
				t.Fatalf("mark fault did not stop the adapter as substrate: %v", err)
			}
			e := entry(t, h, "telegram.outbox")
			if e.Healthy || e.Class != health.ClassSubstrate || !e.Stopped {
				t.Fatalf("health entry %+v", e)
			}
			status := "SENT"
			if p, _ := h.core.Pending(ctxT()); len(p) == 1 {
				status = "PENDING"
			} else if u, _ := h.core.Unreconciled(ctxT()); len(u) == 1 {
				status = "UNKNOWN"
			} else if f, _ := h.core.FailedFor(ctxT(), "telegram", "chat-42"); len(f) == 1 {
				status = "FAILED"
			}
			if status != tc.row {
				t.Fatalf("row status %s after faulted %s, want %s", status, tc.name, tc.row)
			}
		})
	}
}

func init() {
	_ = fmt.Sprint
	_ = context.Background
}

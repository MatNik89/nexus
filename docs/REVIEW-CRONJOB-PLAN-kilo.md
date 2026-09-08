# Review of `/cronjob` inline-calendar picker plan (`docs/PLAN-CRONJOB.md`)

**Scope**: `docs/PLAN-CRONJOB.md` at `13308eb` on `main`, with
`docs/CRONJOB-RESEARCH.md`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. Every cited claim verifies against the actual code, and the design is a
sound clone of the existing HITL + schedule machinery. No substantive
correctness / security / behavioral-equivalence / unbuildable defect found.

## Verification (as asked)

1. **callback_query correctness + deny-default.** `processUpdate` does ignore
   non-message updates (`if u.Message == nil { return nil }`, `telegram.go:255-256`),
   and `tgUpdate` has no `CallbackQuery` field (`telegram.go:125-139`) — the plan's
   "extend + branch" is accurate. The private-chat + deny-default binding gate
   (`telegram.go:260-276`) is the correct gate to mirror for taps.
2. **callback_data as untrusted input.** Strict-parse / owner-binding
   (`ExpectedSource`), single-use terminal commit, TTL, and replay-idempotency are
   all specified (`PLAN-CRONJOB.md:66-79`) and mirror `approval`'s `suspendedPayload`
   (`approval.go:67-84`), `decide()` replay reject, and `DefaultChallengeTTL`.
   The research doc correctly cites the "bad client can send arbitrary data" fact
   and the 64-byte cap with a 26-byte budget proof.
3. **Durability.** The session is a journaled state machine + `picker_sessions`
   projection, surviving restart — HARDQ B6 satisfied (research §5.2 explicitly
   rejects hermes' in-memory `_approval_state` for this reason).
4. **Description-as-next-message.** Correct and idempotent: single-threaded poll
   loop removes the race; wrong-source is denied (capture only from
   `ExpectedSource`); a redelivered commit is a no-op because the session commit is
   single-use. No B2 violation — the description is a picker control, not an
   admission, and its durable record is the `schedule.created` body.
5. **WallTime + one-shot.** `WallTime{Year,Month,Day,Hour,Minute,TZ}`
   (`schedule.go:51-58`) validates the civil date roundtrip (`:60-77`), so a
   tapped grid date can never produce an impossible instant.
   `Scheduler.CreatedParams(id, body, w)` (`schedule.go:319`) is the correct
   no-append composition seam, and `Journal.AppendBatch` (`journal.go:601-603`)
   exists for the atomic `[schedule.created, picker.committed]` recipe.
6. **Buildability.** `a.call(ctx, method, …)` (`telegram.go:171`) is a generic
   POST, so `editMessageReplyMarkup` / `editMessageText` / `answerCallbackQuery`
   need no new wire machinery; the sender's `From`/`Message.Chat` give the
   owner-binding fields.

## Notes (not FAIL reasons)

1. **`CallbackQuery.Message` nil guard.** A callback carrying no `message`
   (inline-mode/`inline_message_id`) would nil-deref `Message.Chat.ID`. Unreachable
   for this bot (it never renders inline-mode keyboards), but "bad client arbitrary
   data" argues for a fail-closed nil check.
2. **Branch order.** The `CallbackQuery` branch must precede the existing
   `u.Message == nil` early-return, or taps are silently ignored.
3. **Calendar/confirmation send path is under-specified.** Sending via `a.call`
   (direct) is transient — a failed calendar send loses the UI (the session is
   still durable, so a re-tap reopens). Sending via the outbox needs a
   `reply_markup` extension to `FlushOutbox`. Either is fine; the plan should
   pick one. The REMINDER itself is delivered by the existing durable
   schedule→delivery path, so no delivery-contract risk.
4. **`schedule.created` id must be deterministic** (e.g. derived from `sid`) so a
   redelivered commit is rejected as a duplicate (`schedule.go:334`) — the
   single-use `picker.committed` plus a deterministic schedule id is what makes
   the commit idempotent; the plan should state the id derivation.
5. **Intercept point.** The description intercept must happen before the
   command router (`telegramHandler`, `main.go:1256+`), else a command typed
   during `await_description` is routed as a command, not captured. The plan's
   intent is clear but the ordering is unstated.
6. **Expiry mechanism** ("sweep or lazy check") is left open; the approval lazy
   expiry-in-projection is the natural clone.
7. **Tap redelivery.** A crash between a tap's state append and the offset
   advance redelivers the same `callback_query.id`; the state transition is
   idempotent at the value level (same date/time), but the plan does not say
   whether tap events are keyed by query id — worth one line.

VERDICT: PASS

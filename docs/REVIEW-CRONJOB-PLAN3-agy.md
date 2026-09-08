# Design Review 3: `docs/PLAN-CRONJOB.md` (Slice `p1-cronjob` v3 Pragmatic)

**Reviewer**: `agy`  
**Date**: 2026-09-08  
**Repository**: `/home/matej/HARNESS/nexus`  
**Branch**: `main` (commit `8216aae5fb8a880c5a292966a1fc86cfdeb0e285`)  
**Target Document**: `docs/PLAN-CRONJOB.md` (v3)  
**References**: `internal/obligation/obligation.go`, `internal/schedule/schedule.go`, `internal/channel/telegram/telegram.go`, `internal/channel/channel.go`, `cmd/nexus/main.go`, `docs/CRONJOB-RESEARCH.md`, `docs/ARCHITECTURE-ESSENTIALS.md`

---

## 1. Executive Summary

`docs/PLAN-CRONJOB.md` (v3) defines a pragmatic, minimal design for the `/cronjob` inline calendar reminder slice. Under the owner's explicit durability scope decision:
- **Durable Path**: The final reminder creation (`schedule.created` + `obligation.created` via `obligation.Manager.CreateReminder`) and the user confirmation delivery (`channel.inbound_terminal` + `channel.outbound_enqueued` via `channel.Core.CompleteInbound`) are fully durable, crash-safe, and idempotent under replayed inbound turns.
- **Ephemeral Path**: The in-progress calendar widget interaction (button taps, month navigation, date/hour/minute selection) is explicitly ephemeral and in-memory with a TTL. A daemon crash during picking simply resets the session, requiring the user to re-invoke `/cronjob`. This avoids heavy journaled HITL turn-suspension state machines for non-blocking single-owner UI chrome.

The design is sound, safe against untrusted input, honest in its delivery guarantees, and completely buildable against the named Go interfaces.

---

## 2. Evaluation Against Review Criteria

### 2.1 Reminder Durability, Correctness, and Idempotency
- **Code Anchors**: `internal/obligation/obligation.go:702-712`, `internal/schedule/schedule.go:49-59, 319-333`, `internal/channel/telegram/telegram.go:284-321`.
- **Obligation & Schedule Pair**: `obligation.Manager.CreateReminder` appends both `schedule.CreatedParams(id, body, w)` and `obligation.created` in a single `AppendBatch`. This guarantees that the reminder is registered with the scheduler and tracked by the obligation manager without orphaned half-states.
- **WallTime Construction**: `schedule.WallTime{Year, Month, Day, Hour, Minute, TZ}` is built directly from the picked date/time and `resolved.Config.Timezone`, properly validated before append.
- **Deterministic ID & Idempotency**:
  - The reminder ID is derived deterministically from the admitted inbound description update (`rem-` + hash(`adapterID | channelIdentity | updateID`)).
  - `CreateReminder` checks whether the reminder ID already exists in the projection (`sched_schedules` / `obl_obligations`). If present, it skips the batch append and succeeds cleanly.
  - In the event of a daemon crash between `CreateReminder` and `CompleteInbound`, `processUpdate` re-runs the description turn on restart (`outcome.Replayed = true`, `st = ADMITTED`). The deterministic ID check makes the re-run a no-op create that returns the identical confirmation string.
- **Verdict**: Fully correct, durable, and replay-safe.

### 2.2 Description vs. Normal Turn Routing
- **Code Anchors**: `cmd/nexus/main.go:1257-1285`, `internal/channel/channel.go:406-444`.
- **Routing Rules**:
  - The in-memory session map is keyed by `source` (e.g. `"tg:chat-<chatID>"`).
  - `/cronjob` replaces any existing session for the same source (enforcing at most one live picker per chat).
  - When a text message arrives, `telegramHandler` checks if `source` has an active `await_description` session.
  - If yes, the message is intercepted as the description body, the reminder is created, the confirmation is returned, and the session is cleared.
  - If no active session exists (or a different chat messages the bot), the message falls through to the standard conversational turn (`b.d.RunChannelTurn`).
- **Verdict**: Clean, unambiguous routing with no cross-source leakage or wrong-source capture.

### 2.3 `callback_query` Gate, Grammar & Security
- **Code Anchors**: `internal/channel/telegram/telegram.go:125-141, 242-285`, `docs/CRONJOB-RESEARCH.md:385-422`.
- **Structural Validation**:
  - `tgUpdate` is extended with `CallbackQuery`, requiring all nested fields (`From.ID`, `Message.Chat.ID`, `Message.Chat.Type`, `Message.MessageID`) to be present.
  - The private-chat gate (`Chat.Type == "private"`) and profile-binding gate (`a.bindings[chat] == a.profile`) execute before any callback handling.
- **Untrusted Grammar**:
  - The 5-token grammar `pk:v1:<sid>:<act>:<arg>` ($\le 26$ bytes $\ll 64$ bytes) enforces closed action verbs (`d`, `h`, `m`, `nav`, `x`) and strict per-action argument validation.
  - State-machine preconditions are checked (e.g. `h` is rejected if date is not set; `nav` is rejected outside month view).
  - Owner binding ensures `from.id` and `chat` match the session's `ExpectedSource`.
  - Malformed or out-of-sequence callbacks receive an immediate benign answer ("Isteklo ili nevažeće") with no state change.
  - `answerCallbackQuery` is guaranteed on all branches to clear client loading spinners.
- **Verdict**: Completely secure against malicious or forged `callback_data`.

### 2.4 Ephemeral vs. Durable Delivery Split & Honesty
- **Code Anchors**: `internal/channel/telegram/telegram.go:300-335`, `internal/channel/channel.go:599-637`.
- **Honesty Invariants**:
  - Interactive UI operations (`sendMessage` for opening, `editMessageReplyMarkup`/`editMessageText` for redraws, and `answerCallbackQuery`) are explicitly unjournaled, best-effort ephemeral chrome.
  - Only the user-visible confirmation message is enqueued into the durable T22 outbox (`chan_outbox`) via `CompleteInbound`.
  - The bot never makes an unjournaled durable promise: a reminder is only confirmed once `CreateReminder` succeeds and the turn completes.
- **Verdict**: Delivery honesty is preserved throughout.

---

## 3. Top-3 Implementation Notes & Weakest Points

1. **Replay Handler Lookup Order (`cmd/nexus/main.go:1257-1285`, `PLAN-CRONJOB.md:70-74`)**:
   - *Detail*: In `telegramHandler`, check if a reminder with the deterministic ID (`rem-<hash(in)>`) already exists in the projection *before* checking the in-memory session map.
   - *Rationale*: If a crash occurs between `CreateReminder` and `CompleteInbound`, the in-memory session will be gone upon restart. Checking the projection first ensures the replayed update is recognized as an already-committed reminder turn and returns the confirmation string immediately, preventing an accidental fall-through to a normal LLM turn.

2. **Slash-Command Escape During `await_description` (`PLAN-CRONJOB.md:67-78`)**:
   - *Detail*: When receiving text in `await_description`, if the text starts with a slash (e.g. `/cancel`, `/help`, `/new`, `/cronjob`), cancel or abort the active picker session rather than scheduling a reminder whose body text is literally `"/cancel"`.
   - *Rationale*: Protects against user change-of-mind or unexpected command routing.

3. **Telegram "message is not modified" HTTP 400 Handling (`docs/CRONJOB-RESEARCH.md:100-107`, `internal/channel/telegram/telegram.go:315-330`)**:
   - *Detail*: In `editMessageReplyMarkup` and `editMessageText`, treat Telegram Bot API error `Bad Request: message is not modified` as an idempotent success.
   - *Rationale*: Ensures `answerCallbackQuery` clears client spinners without error logs when a user double-taps the same view or month nav button.

---

## 4. Verdict

`docs/PLAN-CRONJOB.md` (v3) satisfies all repository invariants, security requirements, and durability guarantees within the owner-approved pragmatic scope.

VERDICT: PASS

# Design Review 4: `docs/PLAN-CRONJOB.md` (Slice `p1-cronjob` v3 Pragmatic, Round 4)

**Reviewer**: `agy`  
**Date**: 2026-09-08  
**Repository**: `/home/matej/HARNESS/nexus`  
**Branch**: `main` (commit `acbad82d20065c33a2a6510b26cdc54cfab334b9`)  
**Target Document**: `docs/PLAN-CRONJOB.md` (v3)  
**References**: `internal/obligation/obligation.go`, `internal/schedule/schedule.go`, `internal/channel/telegram/telegram.go`, `internal/channel/channel.go`, `cmd/nexus/main.go`, `docs/CRONJOB-RESEARCH.md`, `docs/ARCHITECTURE-ESSENTIALS.md`

---

## 1. Executive Summary

`docs/PLAN-CRONJOB.md` (v3, updated for Round 4) incorporates the shared Fold F1: **front durable reminder lookup via `obligation.Manager.ReminderIntent`**.

Under the owner-approved pragmatic scope:
- The reminder creation (`schedule.created` + `obligation.created` via `CreateReminder`) and confirmation delivery (`CompleteInbound` -> `chan_outbox`) are fully durable.
- The in-progress calendar picking is in-memory and ephemeral with a TTL.
- The handler's FIRST step for every admitted text update is a durable check against `Manager.ReminderIntent(deterministicID)`. This decouples replay safety from the ephemeral session state, ensuring that a crash between `CreateReminder` and `CompleteInbound` cleanly reconfirms on restart without misrouting to a normal LLM turn.

The design contains no substantive defects and is ready for implementation.

---

## 2. Verification of Round-3 Fold (F1) & Core Criteria

### 2.1 Fold F1: Front Durable Reminder Lookup & Fresh-Process Replay
- **Code Anchors**: `internal/obligation/obligation.go:702-712`, `internal/channel/telegram/telegram.go:284-321`, `PLAN-CRONJOB.md:66-82`.
- **Mechanism & Proof**:
  - `telegramHandler` derives the deterministic reminder ID `rem-<hash(adapter|identity|update_id)>` for EVERY admitted text update before any session or command check.
  - Calls `Manager.ReminderIntent(id)` returning `{NotFound | Found | StorageError}`.
  - **`Found` Case**: Reconstructs the confirmation string directly from the persisted reminder body and wall time, returning it as the turn reply. This ensures that even if the daemon crashed after `CreateReminder` and lost all in-memory session state, the replayed update is recognized and reconfirmed identically without running an LLM turn.
  - **`StorageError` Case**: Fails closed immediately (returns a typed error; does NOT fall through to normal conversation).
  - **`NotFound` Case**: Only then proceeds to check the in-memory `await_description` session or standard command/conversation routing.
- **Verdict**: Fold F1 is completely closed and correct.

### 2.2 Reminder Correctness, Durability & Idempotency
- **Code Anchors**: `internal/obligation/obligation.go:702-712`, `internal/schedule/schedule.go:49-59, 319-333`.
- `obligation.Manager.CreateReminder` appends `schedule.CreatedParams` and `obligation.created` in one `AppendBatch`, ensuring both scheduler firing and obligation tracking are atomically durable.
- `schedule.WallTime` is constructed using the picked date/time and the configured timezone (`resolved.Config.Timezone`), with fail-closed civil date validation.

### 2.3 Description vs. Normal Turn Routing
- **Code Anchors**: `cmd/nexus/main.go:1257-1285`, `internal/channel/channel.go:406-444`.
- Sessions are strictly bound to `source` (`"tg:chat-<chatID>"`).
- Opening a new `/cronjob` replaces any pending session for that source (enforcing at most one live picker per chat).
- If `ReminderIntent` is `NotFound`, text from a chat with an active `await_description` session creates the reminder; otherwise it routes to normal commands and conversational turns.

### 2.4 `callback_query` Security & Delivery Honesty
- **Code Anchors**: `internal/channel/telegram/telegram.go:125-141, 242-285, 300-335`.
- Structural validation on `tgUpdate.CallbackQuery` validates `From.ID`, `Message.Chat.ID`, `Message.Chat.Type`, and `Message.MessageID`.
- Private-chat (`Chat.Type == "private"`) and profile-binding gates apply before any callback handling.
- 5-part grammar (`pk:v1:<sid>:<act>:<arg>`) with strict per-action and legal-in-state checks fails closed benignly on any deviation, always calling `answerCallbackQuery`.
- Ephemeral UI ops (`sendMessage` for calendar, `editMessageReplyMarkup`, `answerCallbackQuery`) are unjournaled chrome. Only the user confirmation is enqueued into the durable T22 outbox (`chan_outbox`). No unjournaled durable promises exist.

---

## 3. Top-3 Implementation Notes & Weakest Points

1. **Slash-Command Escape During `await_description` (`PLAN-CRONJOB.md:83-95`)**:
   - *Detail*: When receiving text in `await_description`, if the text starts with a slash (e.g. `/cancel`, `/help`, `/new`), cancel or abort the active picker session rather than scheduling a reminder with literal body text `"/cancel"`.
   - *Rationale*: Prevents user mistakes when cancelling an in-progress flow.

2. **Telegram Bot API "message is not modified" HTTP 400 Handling (`docs/CRONJOB-RESEARCH.md:100-107`, `internal/channel/telegram/telegram.go:315-330`)**:
   - *Detail*: In `editMessageReplyMarkup` and `editMessageText`, treat Telegram Bot API error `Bad Request: message is not modified` as an idempotent success.
   - *Rationale*: Ensures `answerCallbackQuery` clears client spinners without logging errors when a user taps an already-active view.

3. **Session Intent Mismatch Guard in Replay Path (`PLAN-CRONJOB.md:77-79`)**:
   - *Detail*: When `ReminderIntent` returns `Found`, if an in-memory session still exists for the chat, verify that its chosen date/time matches the persisted reminder's wall time before reconfirming; if there is a mismatch, fail closed.
   - *Rationale*: Protects against state corruption in edge cases where a session outlives a crash or replay.

---

## 4. Verdict

`docs/PLAN-CRONJOB.md` (v3, Round 4) satisfies all repository contracts, durability invariants, and security requirements.

VERDICT: PASS

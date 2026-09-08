# Design Review: `docs/PLAN-CRONJOB.md` (Slice `p1-cronjob`)

**Reviewer**: `agy`  
**Date**: 2026-09-08  
**Repository**: `/home/matej/HARNESS/nexus`  
**Branch**: `main` (commit `13308eba344bcf563ff4b4bfce7e70eff7949c46`)  
**Target Document**: `docs/PLAN-CRONJOB.md` (v1)  
**References**: `docs/CRONJOB-RESEARCH.md`, `internal/approval/approval.go`, `internal/schedule/schedule.go`, `internal/channel/telegram/telegram.go`, `internal/channel/channel.go`, `cmd/nexus/main.go`, `docs/ARCHITECTURE-ESSENTIALS.md`

---

## 1. Executive Summary

`docs/PLAN-CRONJOB.md` defines a minimal, secure, durable inline-calendar picker for scheduling one-shot reminders via Telegram (`/cronjob`). The design adheres to the locked repository invariants and borrows proven patterns already implemented in the codebase:
- **HITL durable state machine**: Clones the projection and journal event lifecycle from `internal/approval/approval.go` (`EvTurnSuspended`, `EvTurnReceived`, `EvTurnConsumed`, `ExpiresUnix`, `ExpectedSource`, SQLite projection table `picker_sessions`), upholding HARDQ B6 (no in-memory blocking waits; surviving daemon restarts).
- **Scheduler integration**: Constructs `schedule.WallTime` from local date/time + configured timezone (`resolved.Config.Timezone`), invoking `Scheduler.CreatedParams` / `CreateReminder` (`internal/schedule/schedule.go:49-59, 319-333`).
- **Callback query & untrusted input handling**: Extends `internal/channel/telegram/telegram.go` `tgUpdate` with `CallbackQuery`, applies the strict single-user private-chat and profile binding gate before processing, bounds `callback_data` grammar to $< 27$ bytes (`pk:v1:<sid>:<act>:<arg>`), enforces single-use terminal commits, and always calls `answerCallbackQuery` to clear client spinners.
- **Turn admission discipline**: Standard `a.core.Admit(ctx, in)` and `CompleteInbound` lifecycle is preserved when capturing the reminder description text.

The design contains no substantive security, correctness, or behavioral-equivalence defects.

---

## 2. Axis-by-Axis Verification Against Actual Code & Contracts

### 2.1 Callback Query Handling & Security Gates
- **Code Anchor**: `internal/channel/telegram/telegram.go:242-285` (`processUpdate`), `telegram.go:300-335` (`call` wire helper).
- **Verification**:
  - `tgUpdate` struct in `telegram.go` is extended with `CallbackQuery *tgCallbackQuery`.
  - The security gate in `processUpdate` enforces `u.CallbackQuery.Message.Chat.Type == "private"` and `a.bindings[u.CallbackQuery.Message.Chat.ID] == a.profile` prior to routing to the picker handler.
  - `answerCallbackQuery` is invoked on all code paths (success, validation failure, expired session, unauthorized tap) to ensure the Telegram client loading spinner is cleanly dismissed without hanging.

### 2.2 Untrusted `callback_data` Input Discipline
- **Contract Anchor**: `docs/CRONJOB-RESEARCH.md:35-70`, `docs/ARCHITECTURE-ESSENTIALS.md:146-158`.
- **Verification**:
  - The grammar `pk:v1:<sid>:<act>:<arg>` uses a closed action set `{d, h, m, nav, x}` and compact numeric arguments, consuming $\le 27$ bytes, well within the Telegram 64-byte payload limit.
  - Strict colon splitting with fail-closed rejection: any malformed data, unexpected field count, or unknown action verb produces a benign response ("Isteklo ili nevažeće") with no state modification.
  - `ExpectedSource` binding (e.g. `"tg:chat-<chatID>"`) prevents cross-user or cross-chat hijacking of active pickers.
  - Single-use terminal commit prevents replay and double-scheduling.

### 2.3 Durability & Restart Survivability (HARDQ B6)
- **Code Anchor**: `internal/approval/approval.go:40-48, 67-84, 145-173`.
- **Verification**:
  - State transitions (`opened -> date_set -> time_set -> await_description -> committed/cancelled`) are journaled as discrete events and projected into SQLite table `picker_sessions`.
  - Mid-interaction daemon restarts reload active sessions without loss of selected date/time or expiration state, satisfying HARDQ B6.

### 2.4 Description Capture & Turn Lifecycle
- **Code Anchor**: `cmd/nexus/main.go:1250-1320` (`telegramHandler`).
- **Verification**:
  - When the session reaches `await_description`, the next text message from the session's `ExpectedSource` is admitted via `a.core.Admit(ctx, in)`.
  - `telegramHandler` intercepts the message, builds the recipe committing the reminder (`Scheduler.CreatedParams`), transitions the session to `committed`, and completes the inbound turn.
  - Plain messages from chats without an active `await_description` session proceed through normal agent conversation routing unaffected.

### 2.5 Timezone & `schedule.WallTime` Construction
- **Code Anchor**: `internal/schedule/schedule.go:49-59`, `internal/foundation/config/config.go:48-55, 309`.
- **Verification**:
  - The picked date and time fields populate `schedule.WallTime{Year, Month, Day, Hour, Minute, TZ: cfg.Timezone}`.
  - `schedule.WallTime.validate()` correctly rejects impossible calendar combinations (e.g. Feb 30) before journal append.

---

## 3. Top-3 Implementation Notes & Weakest Points

While the design is architecturally sound and ready for implementation, the following edge cases should be explicitly accounted for during code construction:

1. **Session Supersession / Single Active Picker Per Chat (`cmd/nexus/main.go:1250-1320`, `PLAN-CRONJOB.md:37-41`)**:
   - *Risk*: A user may issue a second `/cronjob` command while an existing picker session in the same chat is pending (e.g. in `await_description` or `date_set`).
   - *Mitigation*: When opening a new picker session, `telegramHandler` or `picker` engine should cancel or supersede any existing active session for that `ExpectedSource` to prevent multiple pending sessions or ambiguity in description capture.

2. **Command Escape During `await_description` (`PLAN-CRONJOB.md:52-58`, `cmd/nexus/main.go:1260-1280`)**:
   - *Risk*: A user in `await_description` state who changes their mind might send `/cancel` or `/help`. If treated naively as raw text, a reminder with body `"/cancel"` would be scheduled.
   - *Mitigation*: The text interceptor in `telegramHandler` should check for slash commands: if `/cancel` is received, cancel the picker session and confirm; if another command is received, abort the picker session and route the command normally.

3. **Telegram Bot API "Message is not modified" Idempotent Error Handling (`docs/CRONJOB-RESEARCH.md:35-45`, `internal/channel/telegram/telegram.go:315-330`)**:
   - *Risk*: When a user taps a navigation button that results in identical markup (e.g. double-tapping month nav or redundant view switch), Telegram Bot API returns HTTP 400 with `description: "Bad Request: message is not modified..."`.
   - *Mitigation*: The `editMessageReplyMarkup` and `editMessageText` wire calls in `telegram.Adapter` must treat `"message is not modified"` errors as benign successes so that `answerCallbackQuery` still completes normally and no spurious error is logged.

---

## 4. Acceptance Criteria & Test Plan Verification

The test plan outlined in `PLAN-CRONJOB.md:81-94` covers all essential behaviors and failure modes:
- [x] Strict grammar parsing (malformed/unknown action verbs fail closed).
- [x] Owner binding enforcement (unauthorized callback source refused).
- [x] Single-use terminal commit (second tap/commit is a no-op).
- [x] End-to-end picker flow (`/cronjob` -> date -> hour -> minute -> text -> `schedule.created` event -> confirmation message).
- [x] Durability across restarts (rehydration from SQLite `picker_sessions` table).
- [x] Spinner clearance (`answerCallbackQuery` guaranteed across all paths).

---

## 5. Conclusion

`docs/PLAN-CRONJOB.md` is complete, minimal, and fully aligned with the NEXUS architecture contracts and security rules.

# Code Review 2: `/cronjob` Slice (`p1-cronjob`)

**Reviewer**: `agy`  
**Date**: 2026-09-08  
**Repository**: `/home/matej/HARNESS/nexus`  
**Branch**: `slice/p1-cronjob` (commit `211e9ddc8c96f8dcb78617cdba21a93b3ae02616`)  
**Design Reference**: `docs/PLAN-CRONJOB.md` (v4)  
**Focus Files**:
- `internal/channel/telegram/picker.go`
- `internal/channel/telegram/telegram_picker.go`
- `internal/channel/telegram/telegram.go`
- `internal/obligation/obligation.go`
- `cmd/nexus/main.go`
- `internal/channel/telegram/picker_test.go`
- `internal/channel/telegram/picker_callback_test.go`
- `internal/obligation/reminderintent_test.go`
- `cmd/nexus/main_test.go` (`TestCronjobReconfirmBeforeGate`)

---

## 1. Executive Summary

Slice `p1-cronjob` implements the inline-calendar scheduled reminder according to the approved pragmatic architecture in `docs/PLAN-CRONJOB.md` (v4).

Round-1 fold F1 (missing detector coverage) is **completely closed**:
- `internal/channel/telegram/picker_callback_test.go` verifies:
  1. Foreign-owner refusal (`cq.From.ID != chat`) proves no admission into the journal and guarantees `answerCallbackQuery` is called to clear the client spinner (`TestCallbackForeignOwnerRefused`).
  2. Illegal-in-state callbacks (e.g. tapping an hour before picking a date) are refused without state mutation (`TestCallbackIllegalInStateRefused`).
  3. Malformed and expired/unknown SIDs are refused benignly with spinner clearance (`TestCallbackExpiredOrMalformedRefused`).
  4. End-to-end multi-step tap drilldown (day -> hour -> minute) transitions to `stageAwaitText` and is queryable via `AwaitingPick` (`TestCallbackFullFlowReachesAwait`).
- `cmd/nexus/main_test.go:540-588` (`TestCronjobReconfirmBeforeGate`) verifies:
  1. A replayed description update for an existing reminder with NO in-memory session reconfirms from persisted `ReminderIntent` before the ephemeral session gate is checked.
  2. Replay is strictly idempotent (produces the same confirmation string without double-creating).
  3. Unrelated conversational turns with no active session or reminder are not captured.
  4. Ablation proof: moving or removing the front `ReminderIntent` check causes test failure.

All code paths are buildable, all test suites pass, and no substantive defects remain.

---

## 2. Axis-by-Axis Verification Against Code Sources

### 2.1 Callback Trust & Grammar Validation
- **Code Anchors**: `internal/channel/telegram/picker.go:149-179`, `internal/channel/telegram/telegram_picker.go:46-76`.
- **Grammar & Input Bounds**:
  - `parseCallback` strictly validates $1 \le \text{len}(data) \le 64$, splitting into exactly 5 colon-delimited tokens `pk:v1:<sid>:<act>:<arg>`.
  - `validSID` restricts `sid` to exactly 22 URL-safe base64 characters (`[A-Za-z0-9_-]`).
  - `act` is restricted to the closed set `{nav, d, h, m, x}`.
- **Owner-Binding & Preconditions**:
  - `handlePickerCallback` checks `cq.Message.Chat.Type == "private"`, `a.bindings[chat] == a.profile`, and `cq.From.ID == chat` before reading session state.
  - Foreign tappers receive `answer("Nije tvoj izbornik.")` without touching session state or admitting messages.
  - Stage checks enforce step order (`stageMonth` for navigation and day selection; `stageHour` for hour selection; `stageMinute` for minute selection). Out-of-order or invalid values return benign responses.
  - `answerCallbackQuery` is guaranteed on every return path.

### 2.2 Durable Path & Replay Safety (Front `ReminderIntent` Guard)
- **Code Anchors**: `internal/obligation/obligation.go:715-752`, `cmd/nexus/main.go:1270-1335`.
- **Deterministic ID**: `reminderIDFor(in)` derives `rem-` + hex(`sha256(adapter | identity | update_id)[:8]`).
- **Front Lookup**:
  - `cronjobDescription` runs `b.obl.ReminderIntent(ctx, remID)` before checking `AwaitingPick`.
  - `ReminderFound`: Reconstructs the confirmation string `cronjobConfirm(wall)` directly from persisted state. This guarantees replay safety even if the daemon crashed and lost the in-memory picker session.
  - `ReminderStorageError`: Fails closed (`handled = true`, returns error), preventing fall-through to `RunChannelTurn`.
  - `ReminderNotFound`: Only then queries `AwaitingPick(chatID)`. If active, `CreateReminder` creates the reminder and `DropSource(chatID)` drops the ephemeral session *only after* `CreateReminder` succeeds.

### 2.3 Reminder Correctness & Invariants
- **Code Anchors**: `internal/obligation/obligation.go:702-712`, `cmd/nexus/main.go:1315-1320`.
- `obligation.Manager.CreateReminder` batches `schedP` (`schedule.CreatedParams`) and `oblP` (`obligation.created`) into one atomic `m.j.AppendBatch`.
- `schedule.WallTime` is constructed using `b.cfg.Timezone` with full civil date validation in `schedule.go`.

### 2.4 Ephemeral / Durable Delivery Honesty
- **Code Anchors**: `internal/channel/telegram/telegram_picker.go:23-41, 157-173`, `internal/channel/telegram/telegram.go:318-361`.
- `openPicker`, `handlePickerCallback`, `editCalendar`, and `editText` execute direct wire calls via `a.call` without outbox tracking.
- The description turn outcome is committed via `CompleteInbound`, which atomically persists `channel.inbound_terminal` and `channel.outbound_enqueued`.
- No unjournaled durable promises exist.

### 2.5 Concurrency & Single Poller Execution
- **Code Anchors**: `internal/channel/telegram/picker.go:48-120`, `internal/channel/telegram/telegram.go:247-264, 348`.
- `PickerStore` locks its map operations with `s.mu`.
- In `internal/channel/telegram/telegram.go`, all update dispatches (callbacks, command opens, description turns) run synchronously on the single `PollOnce` polling loop, ensuring thread-safe execution.

---

## 3. Top-3 Implementation Notes & Weakest Points

1. **Unsynchronized `pickerSession` Field Mutation Outside `PickerStore.mu` (`internal/channel/telegram/picker.go:76-89`, `internal/channel/telegram/telegram_picker.go:98-137`)**:
   - *Detail*: `PickerStore.get()` releases `s.mu` before returning `*pickerSession`. Stage and value updates in `handlePickerCallback` mutate fields on the pointer directly without holding `s.mu`.
   - *Impact*: Safe in the current single-threaded `PollOnce` architecture. If polling is ever parallelized across goroutines, this would become a data race with `AwaitingPick`.
   - *Status*: NOTE (ephemeral UI ceiling).

2. **Month Header Text Button Encodes `actCancel` (`internal/channel/telegram/picker.go:204`)**:
   - *Detail*: `btn(monthNames[m]+" "+strconv.Itoa(y), encodeCallback(sess.sid, actCancel, ""))` cancels the session if a user taps the month title header.
   - *Impact*: Tapping the header title cancels the session rather than acting as an inert label button.
   - *Status*: NOTE (minor UX quirk).

3. **`cronjobConfirm` Language Localization Scope (`cmd/nexus/main.go:1284-1287`)**:
   - *Detail*: `cronjobConfirm` uses hardcoded Croatian format `"Podsjetnik postavljen za %04d-%02d-%02d %02d:%02d."`.
   - *Impact*: Matches the repo's single-user Croatian edge persona (aligned with `/new` and error drift mappings), but serves as a design boundary if multi-language support is ever scoped.
   - *Status*: NOTE.

---

## 4. Verdict

The code in `slice/p1-cronjob` satisfies all specification contracts, security requirements, and durability invariants. Fold F1 is completely closed with full detector test coverage.

VERDICT: PASS

# Code Review: `/cronjob` Slice (`p1-cronjob`)

**Reviewer**: `agy`  
**Date**: 2026-09-08  
**Repository**: `/home/matej/HARNESS/nexus`  
**Branch**: `slice/p1-cronjob` (commit `fee4022713d13af3bb68d3ad51b9458d80a19677`)  
**Design Reference**: `docs/PLAN-CRONJOB.md` (v4)  
**Focus Files**:
- `internal/channel/telegram/picker.go`
- `internal/channel/telegram/telegram_picker.go`
- `internal/channel/telegram/telegram.go`
- `internal/obligation/obligation.go`
- `cmd/nexus/main.go`
- `internal/channel/telegram/picker_test.go`
- `internal/obligation/reminderintent_test.go`

---

## 1. Executive Summary

The code implementation for slice `p1-cronjob` adheres strictly to the approved design in `docs/PLAN-CRONJOB.md` (v4) and repo architecture invariants.

Key verified properties:
1. **Strict Callback Security**: `parseCallback` enforces an exact 5-part grammar ($\le 64$ bytes, 22-char base64url SID, closed action set). The single-user private chat gate and profile binding are checked before any interaction. Owner-binding (`cq.From.ID == chat`) and legal-in-state checks prevent unauthorized or out-of-order mutations. All invalid/unauthorized callback paths invoke `answerCallbackQuery` and make zero durable writes.
2. **Durable Path & Replay Safety (F1)**: In `cmd/nexus/main.go`, `cronjobDescription` runs the front durable lookup `obligation.Manager.ReminderIntent(remID)` before evaluating ephemeral session state. When `ReminderFound`, it reproduces the confirmation directly from persisted `WallTime` and body, closing the crash-between-`CreateReminder`-and-`CompleteInbound` window. `ReminderStorageError` fails closed.
3. **Atomic Reminder Pair**: `obligation.Manager.CreateReminder` appends both `schedule.CreatedParams` and `obligation.created` in one atomic `AppendBatch`.
4. **Delivery Honesty**: Ephemeral UI chrome (`sendMessage`, `editMessageReplyMarkup`, `editMessageText`, `answerCallbackQuery`) calls the wire directly without outbox tracking. Only the final user confirmation is enqueued into the durable T22 outbox (`chan_outbox`) via `CompleteInbound`.
5. **Clean Concurrency**: Session mutations occur on the single `getUpdates` polling goroutine.

The code is buildable, all unit tests pass, and no substantive security, correctness, or durability defects exist.

---

## 2. Detailed Verification Against Code Sources

### 2.1 Callback Trust & Grammar Validation
- **Code Anchors**: `internal/channel/telegram/picker.go:149-179`, `internal/channel/telegram/telegram_picker.go:46-76`.
- **Grammar Checks**:
  - `parseCallback` checks `len(data) <= 64`, verifies 5 colon-delimited tokens `pk:v1:<sid>:<act>:<arg>`, validates `sid` against `validSID` (22 URL-safe characters), and restricts `act` to `{nav, d, h, m, x}`.
- **Security Gates**:
  - `handlePickerCallback` validates `cq.Message.Chat.Type == "private"`, `a.bindings[chat] == a.profile`, and `cq.From.ID == chat`.
  - `sess := a.picker.get(cd.sid)` verifies session existence and `sess.chatID == chat`.
- **Legal-in-State Machine**:
  - `actNav` and `actDay` require `sess.stage == stageMonth`.
  - `actDay` validates `parseISODayInMonth(cd.arg, sess.viewYear, sess.viewMonth)`.
  - `actHour` requires `sess.stage == stageHour` and bounds $h \in [0, 23]$.
  - `actMinute` requires `sess.stage == stageMinute` and validates $mi \in \{0, 15, 30, 45\}$.
  - Any mismatch returns `answer("")` with no state modification.
- **Delivery**: `answerCallbackQuery` is invoked across all valid, invalid, and unauthorized paths to prevent persistent client spinners.

### 2.2 Durable Path & Replay Safety (F1 Guard)
- **Code Anchors**: `internal/obligation/obligation.go:715-752`, `cmd/nexus/main.go:1270-1335`.
- **Deterministic ID**: `reminderIDFor(in)` computes `rem-` + hex(`sha256(in.AdapterID | in.ChannelIdentity | in.UpdateID)[:8]`), guaranteeing an identical ID on replay.
- **Front Lookup (`ReminderIntent`)**:
  - `ReminderIntent` executes `SELECT o.body, s.wall FROM obl_obligations o JOIN sched_schedules s ON s.id = o.id WHERE o.id = ? AND o.kind = 'reminder'`.
  - Returns `ReminderFound`, `ReminderNotFound`, or `ReminderStorageError`.
  - `cronjobDescription` runs this lookup *before* checking `AwaitingPick`.
  - If `ReminderFound`, `cronjobConfirm(wall)` is returned immediately. If the daemon crashed after `CreateReminder` and lost the ephemeral session, replaying the update reconfirms without mis-routing to a conversation turn.
  - If `ReminderStorageError`, it fails closed (`handled = true`, returns error), preventing fall-through to `RunChannelTurn`.
  - If `ReminderNotFound`, it checks `AwaitingPick(chatID)`: if present, creates the reminder and calls `DropSource(chatID)` only upon success.

### 2.3 Reminder Correctness & Timezone Handling
- **Code Anchors**: `internal/obligation/obligation.go:702-712`, `cmd/nexus/main.go:1315-1320`.
- `CreateReminder` batches `schedP` and `oblP` into `m.j.AppendBatch(ctx, []contracts.EnvelopeParams{schedP, oblP})`.
- `schedule.WallTime` is constructed using `b.cfg.Timezone` with full civil date validation in `schedule.go`.

### 2.4 Ephemeral / Durable Delivery Honesty
- **Code Anchors**: `internal/channel/telegram/telegram_picker.go:23-41, 157-173`, `internal/channel/telegram/telegram.go:318-361`.
- `openPicker`, `handlePickerCallback`, `editCalendar`, and `editText` execute best-effort wire calls via `a.call`.
- Only the description message outcome is committed via `CompleteInbound(outcome.MessageID, adapterID, identity, a.profile, reply)`, which durably persists `channel.inbound_terminal` and `channel.outbound_enqueued`.

### 2.5 Concurrency & Race Discipline
- **Code Anchors**: `internal/channel/telegram/picker.go:48-120`, `internal/channel/telegram/telegram.go:247-264, 348`.
- `PickerStore` synchronizes map operations (`open`, `get`, `AwaitingPick`, `DropSource`) with `s.mu`.
- All `processUpdate` dispatches (callback queries, text `/cronjob`, and text description turns) run synchronously on the single `PollOnce` polling loop, ensuring thread-safe interaction.

---

## 3. Candidate Defects & Weakest Points

1. **Unsynchronized `pickerSession` Field Mutation Outside `PickerStore.mu` (`internal/channel/telegram/picker.go:76-89`, `internal/channel/telegram/telegram_picker.go:98-137`)**:
   - *Evidence*: `PickerStore.get()` releases `s.mu` before returning `*pickerSession`. Mutations (`sess.day`, `sess.hour`, `sess.minute`, `sess.stage`) in `handlePickerCallback` occur directly on the struct pointer.
   - *Assessment*: Safe under the current architecture because `Adapter.PollOnce` executes strictly sequentially on a single goroutine. However, if the adapter polling is ever parallelized, reading `sess.stage` in `AwaitingPick` under `s.mu` while `handlePickerCallback` writes without `s.mu` would constitute a data race.
   - *Classification*: NOTE (architectural hygiene recommendation to mutate stage transitions via mutex-guarded methods).

2. **Month Header Text Button Uses `actCancel` (`internal/channel/telegram/picker.go:204`)**:
   - *Evidence*: `btn(monthNames[m]+" "+strconv.Itoa(y), encodeCallback(sess.sid, actCancel, ""))` binds tapping the month/year label to `actCancel`.
   - *Assessment*: If a user taps the month title expecting a no-op or year view, the entire session is cancelled rather than ignored.
   - *Classification*: NOTE (minor UX quirk; does not compromise security or correctness).

3. **End-to-End Test in `cmd/nexus/main_test.go` (`internal/channel/telegram/picker_test.go:8-102`, `internal/obligation/reminderintent_test.go:8-25`)**:
   - *Evidence*: `picker_test.go` thoroughly covers grammar, rendering, and store logic, while `reminderintent_test.go` verifies the SQL projection lookup. A unified end-to-end test in `main_test.go` exercising the full `/cronjob` flow through the fake Bot API server would reinforce integration coverage.
   - *Classification*: NOTE (test suite enhancement).

---

## 4. Verdict

The implementation in `slice/p1-cronjob` is sound, secure, durable, and completely adheres to all specification and architecture contracts.

VERDICT: PASS

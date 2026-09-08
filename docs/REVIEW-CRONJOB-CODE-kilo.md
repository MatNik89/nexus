# Review of `/cronjob` code slice (`fee4022`)

**Scope**: commit `fee4022` on `slice/p1-cronjob` — `picker.go`,
`telegram_picker.go`, `telegram.go`, `obligation.go`, `main.go`, tests.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The durable path is correct and idempotent, callback trust is sound, and
the ephemeral/durable delivery split is honest. Build clean; targeted suites
pass. No substantive security / correctness / replay / unbuildable defect.

## Verification (as asked)

### 1. Callback trust — sound

- `parseCallback` (`picker.go:149-167`) is strict: exact prefix `pk:v1`, 5 fixed
  fields, `validSID` (22 url-safe chars), closed action set, ≤64 bytes.
  `TestParseCallbackStrict` exercises 7 malformed shapes.
- Owner binding: `cq.From.ID != chat` refused (`telegram_picker.go:65`), plus
  `sess.chatID != chat` refused (`:73`). For a private chat `chat.id == the
  owner's user id`, so `from.id == chat` is the correct binding; a forwarded
  message into another private chat fails the `sess.chatID != chat` check.
- Legal-in-state: each action gates on `sess.stage` (`actNav`/`actDay` on
  `stageMonth`, `actHour` on `stageHour`, `actMinute` on `stageMinute`); a
  crafted out-of-order `callback_data` answers benignly with no mutation.
- Always `answerCallbackQuery`; no durable write anywhere in the callback path.
  No crafted data can drive a foreign/expired session or mutate durable state.

### 2. Durable path — correct and idempotent

- `cronjobDescription` (`main.go`) runs `ReminderIntent` BEFORE the session gate,
  and the `telegramHandler` invokes it before command parsing. A replay after a
  lost session hits `ReminderFound` and reconfirms from the persisted
  `body`+`wall`; `ReminderStorageError` fails closed (handled=true, error
  propagates, offset does not advance).
- `reminderIDFor` is deterministic over `(AdapterID, ChannelIdentity,
  UpdateID)` — stable across redelivery.
- Double-create impossible: the front `ReminderFound` path never calls
  `CreateReminder`; and even if a create races, `CreateReminder` batches
  `[schedule.created, obligation.created]` atomically (`obligation.go:702-712`)
  with the schedule projection aborting a duplicate id (`schedule.go:334`).
- `DropSource` is called only on cancel or after a successful
  `CreateReminder` — NOT on error — so a failed commit leaves the session to
  retry, and the front lookup resolves the create-succeeded-but-response-lost
  ambiguity (E9) on replay.

### 3. Reminder correctness

- `CreateReminder` produces the schedule+obligation pair; `WallTime` is built
  from the picked components + `b.cfg.Timezone`, and `schedule.WallTime.validate`
  (`schedule.go:60-77`) rejects an impossible civil date (past-day buttons are
  still tappable but validated at commit).
- Confirmation `cronjobConfirm(wall)` is a pure function of the persisted
  `WallTime`, so the front-lookup reconfirmation is byte-identical to the
  original. `ReminderIntent` JOINs `obl_obligations(kind='reminder')` ×
  `sched_schedules` on `id` and returns `(state, body, wall)` — verified columns
  `obl_obligations.id/kind/body` (`obligation.go:304-307`) and
  `sched_schedules.id/wall` (`schedule.go:179-183`).

### 4. Delivery honesty

- `/cronjob` open (`processUpdate` `isCronjobCmd` branch, `telegram.go:315-320`)
  and every callback are NOT admitted and use best-effort `a.call`
  (`sendMessage`/`editMessageReplyMarkup`/`editMessageText`/
  `answerCallbackQuery`). Only the description is an ordinary admitted turn, and
  its confirmation rides the durable `CompleteInbound` recipe. No unjournaled
  durable message.

### 5. Concurrency

- All session mutation (`handlePickerCallback`) and reads (`AwaitingPick`) run
  on the single getUpdates goroutine; the description handler is invoked
  synchronously from `processUpdate`, so there is no cross-goroutine access.
  The `PickerStore` mutex guards the map; the session-pointer fields are mutated
  outside it, which is correct ONLY under that single-goroutine invariant.

### 6. Tests

`picker_test.go` and `reminderintent_test.go` are non-vacuous and contract-
anchored (strict parse, one-live-per-chat replace, TTL expiry, AwaitingPick
stage gating, month-grid round-trip). See notes for coverage gaps.

## Notes (not FAIL reasons)

1. **Session-pointer mutation outside the mutex.** `handlePickerCallback` writes
   `sess.stage/day/hour/...` after `get()` releases the lock, while
   `AwaitingPick` reads them under the lock. Correct only because everything is
   on one goroutine (documented at `telegram_picker.go:6-7`); a future async
   handler would race. Fragile invariant, not a current defect.
2. **Description-vs-command interaction.** While a pick awaits its description,
   ANY text (including a real command like `approve <id>`) becomes the
   description, because the front guard runs before command parsing. User-owned
   text, so not a security issue, but a UX sharp edge.
3. **`cancel`/`odustani` are reserved** — a reminder whose body is literally
   "cancel" cannot be created.
4. **Stale UI after a failed edit.** The session advances before the best-effort
   `editMessageText`; on a 400 the state/UI diverge and the next tap is
   answered benignly without re-render. Ephemeral (TTL), owner ceiling.
5. **Coverage gaps:** no dedicated test for the owner-binding refusal, the
   illegal-in-state tap, or the durable-path replay guard (the latter lives in
   `main.go` and is only exercised end-to-end, if at all).

VERDICT: PASS

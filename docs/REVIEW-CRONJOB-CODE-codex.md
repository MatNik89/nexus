# Adversarial code review: `/cronjob`

Reviewed branch `slice/p1-cronjob` at commit
`fee4022713d13af3bb68d3ad51b9458d80a19677` against
`docs/PLAN-CRONJOB.md` v4 and the cited production owners. The review used a
clean export of that commit. The owner-decided ephemeral picker durability
ceiling was treated as non-blocking.

## Substantive finding

### F1 — Required: the committed tests do not exercise the durable or callback trust contracts

The approved plan makes the end-to-end detectors part of the delivery contract:
the reminder pair and outbox confirmation, duplicate-description idempotency,
fresh-process replay without picker state, storage-error fail-closed behavior,
normal-turn preservation, callback owner/state/refusal behavior, and unconditional
`answerCallbackQuery` are all named at `docs/PLAN-CRONJOB.md:111-133`.

The slice does not contain those detectors. `picker_test.go:8-102` tests the
envelope parser, direct store operations, and generated day-grid parse-back, but
never calls `handlePickerCallback` or `processUpdate`. It therefore cannot detect
removal of the private-chat/profile/from-owner gates, illegal-in-state callback
transitions, a callback that writes durable state, or a missing callback answer.
`reminderintent_test.go:8-26` proves only a direct NotFound/Found lookup. No test
executes `cronjobDescription`, `telegramHandler`, `CreateReminder` through the
description path, or `CompleteInbound`; consequently the critical crash window
at `cmd/nexus/main.go:1291-1320` and the terminal-plus-outbox boundary at
`internal/channel/telegram/telegram.go:328-360` are untested.

This is not an editorial coverage preference. The repository requires a
red-capable detector for behavioral changes, and the approved design explicitly
names these detectors. The existing generic Telegram replay tests predate this
slice and do not observe reminder creation, session loss, persisted-intent
reconfirmation, or the confirmation outbox row. A regression that moves the
`ReminderIntent` lookup behind `AwaitingPick`, drops one half of the reminder
pair, or removes callback owner binding can therefore remain green.

Required fix: add boundary-level tests for the named plan detectors, using fresh
journals and the real handler/adapter seams. At minimum, causally prove RED for
(a) moving the durable lookup behind the session gate, (b) attempting a duplicate
create on replay, and (c) removing callback owner/state validation or callback
answering.

## Production-code trace

- **Callback ownership:** the callback branch is kept outside admission and
  checks private chat, profile binding, `From.ID == Chat.ID`, live SID, and
  `sess.chatID == chat` before transitions (`telegram.go:284-287`,
  `telegram_picker.go:46-75`). Unknown and expired sessions cannot reach the
  durable reminder path. No callback handler writes the journal.
- **Durable replay:** `processUpdate` admits before invoking the handler and
  reruns only non-terminal replays (`telegram.go:324-360`). The handler invokes
  `ReminderIntent` before `AwaitingPick`; Found reconfirms from persisted wall
  time, StorageError returns an error, and only NotFound consults the ephemeral
  session (`cmd/nexus/main.go:1291-1307`). `CreateReminder` atomically appends
  `schedule.created` plus `obligation.created` (`obligation.go:700-712`). The
  deterministic ID is derived from adapter, identity, and update ID
  (`cmd/nexus/main.go:1270-1273`). This static trace supports the intended
  idempotency, but the absent detectors leave it unproved as a delivered slice.
- **Wall time and confirmation:** the picked components and configured timezone
  construct `schedule.WallTime` (`cmd/nexus/main.go:1315-1320`); schedule
  validation and admission-time UTC resolution occur in
  `schedule.go:319-330`. Reconfirmation uses the persisted wall value returned by
  the schedule/obligation join (`obligation.go:729-751`).
- **Delivery honesty:** calendar sends/edits and callback answers use direct
  best-effort Bot API calls (`telegram_picker.go:23-40,46-51,156-172`). Only the
  description reply reaches `CompleteInbound`, which batches terminal state and
  outbox enqueue (`telegram.go:348-360`, `channel.go:599-626`). No durable
  reminder claim is made by the ephemeral UI.
- **Concurrency:** production starts one `Adapter.Run` loop, whose `PollOnce`
  processes updates synchronously (`telegram.go:247-263,428-443`). The handler is
  called synchronously from that same path, while the store also locks map
  access. There is no second production writer to a session pointer in this
  slice.

## Notes (non-blocking)

1. `parseCallback` recognizes the action but does not enforce canonical
   per-action syntax itself. The handler accepts, for example, `h:+1` and an
   `x` action with a non-empty argument (`picker.go:149-166`,
   `telegram_picker.go:78-139`). The accepted values remain range-bounded and
   source-bound, so this does not create a foreign-session or privilege path;
   it is a strict-grammar cleanup, not the FAIL reason.
2. `handlePickerCallback` does not validate a positive callback message ID or
   compare it with `sess.messageID`. Telegram-authenticated callback provenance,
   the unguessable SID, and chat/from/session binding prevent a demonstrated
   cross-source effect. Treat this as defense-in-depth unless a reachable
   alternate callback producer is added.
3. The calendar permits past instants, which the one-shot scheduler will catch
   up immediately as overdue. The approved design did not lock a reject-past
   product policy, so this remains a product-choice ceiling rather than a defect.
4. The hour/minute views omit the plan's Back navigation. This affects only the
   explicitly best-effort picker and is covered by the owner-decided ephemeral
   ceiling.

## Verification and proof ceiling

- `go test ./internal/channel/telegram ./internal/obligation ./internal/schedule ./internal/channel ./cmd/nexus` — PASS.
- `go vet ./internal/channel/telegram ./internal/obligation ./internal/schedule ./internal/channel ./cmd/nexus` — PASS.
- `go test -list ...` confirmed that the slice contributes only the five picker
  unit tests and `TestReminderIntentLookup`; no cronjob durable/replay/callback
  boundary detector is present.
- `go test -race` could not run on this host because ThreadSanitizer rejected the
  process VMA layout (`Found 47 - Supported 48`). This is a host proof ceiling,
  not evidence of a race.
- A full `go test ./...` was stopped after it produced no further output for
  roughly one minute; focused scoped packages were green. No live Telegram call
  was made.

The weakest production-code point is the uncodified assumption that only the
single `Adapter.Run` goroutine invokes picker callbacks; the mutex protects the
map but returned session pointers are mutated after unlocking. That assumption
holds in the reviewed composition, but a future second caller would require
store-owned mutations.

VERDICT: FAIL

# Adversarial code re-review: `/cronjob`

Reviewed branch `slice/p1-cronjob` at commit
`211e9ddc8c96f8dcb78617cdba21a93b3ae02616` against
`docs/PLAN-CRONJOB.md` v4 and the named production owners. Review evidence came
from a clean on-disk export of that commit. The owner-approved ephemeral picker
durability ceiling was treated as non-blocking.

## Verdict

Round-1 F1 is **CLOSED**. The added detectors exercise the two risk-owning
seams rather than merely parsing fixtures:

- `picker_callback_test.go:51-122` invokes the real callback handler against a
  fake Bot API. A foreign sender cannot advance the live session, an hour action
  cannot skip the day state, unknown/malformed SIDs are refused, the valid
  day-hour-minute sequence reaches `AwaitingPick`, and each exercised result
  calls `answerCallbackQuery`. Removing the owner check, state check, or answer
  call makes an asserted observable change.
- `main_test.go:535-587` builds the real daemon owners over a fresh journal,
  persists the schedule/obligation pair, deliberately supplies an empty picker
  store, and invokes `cronjobDescription` twice for the same canonical inbound.
  It therefore detects moving the Found return behind `AwaitingPick`: the
  response would become `handled=false`. The second invocation confirms that
  replay returns the same persisted-wall confirmation without attempting a
  duplicate create; a different update remains a normal turn.

No new substantive security, correctness, replay, durability, concurrency, or
buildability defect was found in the reviewed slice.

## Contract trace

### 1. Callback trust

`processUpdate` routes callbacks directly to the picker handler and does not
admit them (`telegram.go:284-290`). Before any state transition, the handler
requires a private chat bound to this adapter's profile, checks
`From.ID == Chat.ID`, strictly parses the callback envelope, resolves a live SID,
and binds that session back to the same chat (`telegram_picker.go:46-75`). Each
action has a closed switch and a stage/range check before mutation
(`telegram_picker.go:78-140`). Unknown, expired, foreign, malformed, and
illegal-in-state callbacks cannot reach reminder creation; all valid callback
IDs are answered through the same local `answer` closure.

### 2. Durable creation and replay

The description is admitted before the handler runs, and only a non-terminal
replay reruns the handler (`telegram.go:324-348`). `cronjobDescription` derives
the reminder ID from adapter, channel identity, and update ID, then calls
`ReminderIntent` before `AwaitingPick` (`main.go:1270-1273,1291-1307`). Found
reconstructs the confirmation from persisted wall time; StorageError returns an
error; only NotFound may consult the ephemeral session. The session is dropped
only after `CreateReminder` returns success (`main.go:1315-1320`).

`CreateReminder` appends `schedule.created` and `obligation.created` in one
journal batch (`obligation.go:700-712`). The lookup joins those two synchronous
projections and returns the persisted body and wall time
(`obligation.go:724-751`). A crash after that batch but before terminalization
leaves the inbound non-terminal and the Telegram offset unadvanced; replay finds
the pair and reconfirms instead of creating again.

### 3. Reminder and delivery correctness

The selected civil components plus configured timezone form the exact
`schedule.WallTime` passed to the obligation owner (`main.go:1315-1320`). The
scheduler validates that wall time and persists its resolved due instant in
`CreatedParams` (`schedule.go:319-330`). Confirmation uses the persisted wall
returned by `ReminderIntent`, so it does not depend on surviving picker state.

Calendar sends, edits, and callback answers use direct best-effort Bot API calls
(`telegram_picker.go:23-40,46-51,156-172`). The durable confirmation alone
returns through the admitted handler and reaches `CompleteInbound`, which
atomically appends inbound terminal state and the outbox row
(`telegram.go:348-360`, `channel.go:599-626`). The ephemeral UI does not claim
that a reminder exists.

### 4. Concurrency

Production starts one `Adapter.Run`; its loop invokes `PollOnce`, which processes
each update and the synchronous handler serially (`telegram.go:247-263,428-443`).
`SetPicker` is called during startup before that loop. `AwaitingPick` runs inside
the same synchronous handler path, and the store additionally locks map access
(`picker.go:47-119`). No second production writer to a session pointer was found.

## Non-blocking notes / weakest points

1. The replay detector directly exercises `cronjobDescription`, not the outer
   `processUpdate` plus `CompleteInbound` composition. Existing channel tests
   already own admission replay and terminal-plus-outbox atomicity, so duplicating
   those mechanics here is not required for PASS. A single cronjob integration
   test would raise the proof ceiling if this composition later changes.
2. There is no dedicated injected-storage-error cronjob test. The current code
   path is direct and fail-closed (`ReminderIntent` returns
   `ReminderStorageError`; `cronjobDescription` returns an error), but an explicit
   closed-journal/failing-query detector would better pin that branch.
3. The callback tests call `handlePickerCallback` directly, so removal of the
   `tgUpdate.CallbackQuery` dispatch branch would not fail these new tests. The
   branch is present and buildable by inspection; add a `PollOnce` callback case
   if callback dispatch becomes a recurring regression class.
4. The previously noted non-canonical per-action spellings (`h:+1`, or `x` with
   a non-empty argument), missing Back buttons, and past-instant product policy
   affect only the source-bound ephemeral interaction or an unlocked product
   choice. They do not provide foreign-session control or weaken reminder
   durability, so they remain NOTES under the owner's verdict policy.

## Verification

- `go test -count=1 -run 'TestCallback(ForeignOwnerRefused|IllegalInStateRefused|ExpiredOrMalformedRefused|FullFlowReachesAwait)$' ./internal/channel/telegram` — PASS.
- `go test -count=1 -run '^TestCronjobReconfirmBeforeGate$' ./cmd/nexus` — PASS.
- `go test -count=1 ./internal/channel/telegram ./internal/obligation ./internal/schedule ./internal/channel ./cmd/nexus` — PASS.
- `go vet ./internal/channel/telegram ./internal/obligation ./internal/schedule ./internal/channel ./cmd/nexus` — PASS.
- The claimed RED sensitivity was checked statically against the assertions and
  control flow; this review did not mutate the read-only candidate to reproduce
  the author's ablation run.

## Topknot assessment

The test-only fold adds no dependency or production abstraction and reuses the
existing fake Bot API and daemon builders. Lean already.

Skill update: none. The prior review identified a commit-specific absence of
detectors; this fold does not expose a new reusable audit-process miss.

VERDICT: PASS

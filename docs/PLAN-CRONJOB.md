# PLAN: /cronjob — inline-calendar scheduled reminder (slice p1-cronjob)

Owner ask: typing `/cronjob` opens a picker in the chat — tap a DATE, tap a
TIME, then type a DESCRIPTION — and NEXUS schedules a reminder that fires at
that instant and delivers to Telegram.

Scope: a ONE-SHOT scheduled reminder (the `schedule`/`obligation` backend is
one-shot in P0). Recurring cron, timezone picker, and NLP date entry are later
slices. Uses the configured timezone.

## Durability scope (owner decision 2026-09-08: PRAGMATIC)
- DURABLE (survives any restart): the created reminder + its confirmation.
- EPHEMERAL (best-effort, re-runnable): the calendar picking interaction. The
  in-progress picker session is in-memory with a TTL; if the daemon restarts
  mid-pick the session is gone and the owner re-types `/cronjob`. The picker
  blocks NO turn (each tap is its own immediate callback), so it is NOT a
  journaled HITL wait (HARDQ B6 governs blocking turn-suspends, which this is
  not). Only the final reminder is durable — deliberately, to avoid crash-safe
  machinery for a transient single-owner UI.

## Steal basis (verified in the actual code, file:line)
- Reminder create is the OBLIGATION PAIR (r1 codex F1, verified
  `obligation.go:702-712`): `Manager.CreateReminder` appends BOTH
  `schedule.CreatedParams(id,body,w)` AND `obligation.created` in one
  `AppendBatch`; the scheduler firing decorator expects the obligation half
  (`main.go:541-549`). The picker reuses `CreateReminder` directly (no new
  seam, no duplicated payload) — see idempotency below.
- `WallTime{Year,Month,Day,Hour,Minute,TZ}` = schedule.go:49-59.
- Update path: `internal/channel/telegram` — `PollOnce` loops updates,
  `processUpdate` handles only `u.Message` today (`if u.Message == nil { return
  nil }`); it gains a `u.CallbackQuery` branch.
- The existing turn recipe already gives durable reply delivery: `Admit`
  (exactly-once admission) -> `handle` -> `CompleteInbound` terminal + outbox
  (telegram.go processUpdate). The description turn reuses this verbatim.

## Mechanism
### 1. Button taps (callback_query) — adapter-owned, typed
Extend `tgUpdate` with `CallbackQuery *{ ID string; From *{ID int64}; Message *{
MessageID int64; Chat{ID int64; Type string} }; Data string }`; require all of
`From`, `Message`, `Chat.ID`, `Chat.Type`, `MessageID` — an unsupported form is
answered benignly (no state change). `processUpdate` routes
`u.CallbackQuery != nil` to the adapter's picker handler; the SAME single-user +
deny-default gate as messages applies first (`Chat.Type=="private"`, bound
profile, `From!=nil`, telegram.go:258-276). `answerCallbackQuery` is ALWAYS
called (clears the spinner). The calendar send/`editMessageReplyMarkup` and
`answerCallbackQuery` are EPHEMERAL best-effort ops (like the existing
`sendChatAction`), NOT outbox-tracked — justified because they are transient UI,
not durable messages; the only durable user-visible message is the confirmation.

### 2. `/cronjob` (text command) -> open a picker
telegramHandler on `cronjob` returns nothing durable; instead the adapter
creates an in-memory session for the source (chat+user), REPLACING any existing
live session for that same source (one live picker per source), sets a TTL, and
best-effort `sendMessage` the current-month inline calendar. A restart loses it
(re-run). No journal write for opening.

### 3. Inline calendar (buttons)
Month view: 7-wide day grid + `< month >` nav + Cancel. Tap day -> edit the
message in place to the hour view. Hour view: 0-23 buttons + Back -> minute
view. Minute view: {00,15,30,45} + Back. Tap minute -> session records the full
local datetime, edits the message to "Send the reminder text as a message."
Every redraw edits the one message; on any edit failure the session still holds
state and the next tap re-renders — or the TTL expires and the owner re-runs.

### 4. Description -> reminder via the EXISTING turn recipe (durable, idempotent)
The description arrives as an ordinary text update. `processUpdate` runs today's
path: `Admit` -> `handle`. The handler checks: does this source own an
`await_description` session? If yes, it is the description:
- Build `WallTime` from the picked date/time + configured TZ.
- Reminder id is DETERMINISTIC from the admitted update identity
  (`rem-<hash(adapter|identity|update_id)>`), so a re-run creates the SAME id.
- `CreateReminder(id, body, w)` is made IDEMPOTENT: if a reminder with that id
  already exists (projection lookup), skip the create and just re-confirm — so a
  crash between create and inbound-terminal cannot double-create on replay.
- Return the confirmation string ("Podsjetnik postavljen za <local time>.") as
  the turn reply; the existing recipe appends the inbound TERMINAL + outbox
  confirmation. Clear the in-memory session.
If the source has NO active session, the text is a normal turn (today's path).
A TERMINAL replay of the description update skips the handler (today's
processUpdate rule); a non-terminal replay re-runs it, and the deterministic-id
idempotency makes that a no-op create + same confirmation.

### 5. callback_data grammar (<= 64 bytes, strict per-action + legal-in-state)
`pk:v1:<sid>:<act>:<arg>`, fixed 5 fields:
- `sid`: exact generated alphabet/length (22 url-safe base64 chars).
- `nav`: `arg` in {`-1`,`+1`}, one-step; month view only.
- `d`: `arg` `YYYY-MM-DD` in the shown month; month view only.
- `h`: `arg` `0..23`; only after a day is set.
- `m`: `arg` in {`0`,`15`,`30`,`45`}; only after an hour.
- `x`: empty `arg`; any non-terminal state.
Any deviation (unknown act, wrong field count, bad sid, out-of-range, or an
action illegal in the session's current state) OR an unknown/expired `sid` is
answered benignly ("isteklo") with NO reminder and NO durable write. Owner
binding: a callback whose `from.id`+chat != the session's source is refused; no
cross-source control.

## Detectors (RED before, GREEN after; anchored to the contract)
- reminder PAIR: finishing the flow produces exactly one `schedule.created` AND
  `obligation.created` with the same id, WallTime matching the taps, body = the
  description; confirmation delivered via the outbox (RED against a schedule-only
  create).
- idempotent create: running the description handler twice for the same update
  (replay) yields ONE reminder, same confirmation (RED against double-create).
- normal turn unaffected: a text message with no active session is handled as a
  normal turn (no reminder, no session).
- callback validation: malformed / unknown-act / bad-sid / out-of-range /
  illegal-in-state / foreign-source callbacks make no durable write and no
  reminder, and are always answered.
- one live session per source: a second `/cronjob` replaces the first (the old
  sid no longer commits).
- answerCallbackQuery always called, even on refusal.

## Non-goals (later slices)
- Recurring schedules, timezone UI, NLP date entry, listing/editing existing
  reminders, crash-safe RESUME of an in-progress pick (ephemeral by decision).

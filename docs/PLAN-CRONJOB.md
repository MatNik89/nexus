# PLAN: /cronjob — inline-calendar scheduled reminder (slice p1-cronjob)

Owner ask: typing `/cronjob` opens a picker in the chat — tap a DATE, tap a
TIME, then type a DESCRIPTION — and NEXUS schedules a reminder that fires at
that instant and delivers to Telegram.

Scope note: this is a ONE-SHOT scheduled reminder (the `schedule`/`obligation`
backend is one-shot in P0). Recurring cron is a later slice; the command is
still named `/cronjob` per the owner. No timezone picker (uses the configured
timezone); no natural-language date parsing.

## Steal basis (verified in the actual code, file:line)
- HITL durable state machine: clone `internal/approval/approval.go` — journaled
  Events (`EvTurnSuspended/Received/Consumed`, approval.go:40-48), a payload
  carrying `ExpiresUnix` + `ExpectedSource` (approval.go:67-84), a Projection
  table (approval.go:145-173), single-use consume. The picker session is the
  same shape.
- Reminder create: `internal/schedule` — `WallTime{Year,Month,Day,Hour,Minute,
  TZ}` (schedule.go:49-59), `Scheduler.CreatedParams(id, body, w)`
  (schedule.go:319) / `CreateReminder` (schedule.go:333); the Scheduler already
  fires occurrences and delivers.
- Update path: `internal/channel/telegram` — `PollOnce` loops updates,
  `processUpdate` currently handles only `u.Message` (`if u.Message == nil {
  return nil }`); it must also branch on a new `u.CallbackQuery`.
- Button/callback discipline (research, docs/CRONJOB-RESEARCH.md): ECC
  `adapter.py` shape — prefix dispatch, fail-closed owner check, single-use
  claim, resolve-before-render, 64-byte callback_data limit.

## Mechanism
### 1. Receive button taps (callback_query)
Extend `tgUpdate` with `CallbackQuery *{ ID string; From *{ID}; Message *{
Chat{ID} }; Data string }`. `processUpdate`: if `u.CallbackQuery != nil`, route
to the picker handler and ALWAYS call `answerCallbackQuery` (clears the client
spinner) — best-effort, never blocks. The same single-user + deny-default
profile gate as messages (private chat, bound profile) applies BEFORE acting.

### 2. `/cronjob` command (text) -> open a picker
`cmd/nexus/main.go` telegramHandler: on `cronjob`, create a durable picker
session and send the current-month inline-keyboard calendar. The session id is
a random opaque `sid`; the session records `ExpectedSource` (the chat+user that
opened it) and `expires_unix` (TTL, e.g. 15 min like approval).

### 3. Inline calendar (buttons, not a native picker)
- Month view: a 7-wide grid of day buttons + a header row (`< month >`
  prev/next nav) + a Cancel button. Tap a day -> edit the SAME message
  (`editMessageReplyMarkup`/`editMessageText`) to the hour grid.
- Hour view: 24 hour buttons (0-23 in rows) + Back. Tap -> minute view.
- Minute view: coarse (00/15/30/45) + Back. Tap -> the session records the full
  local datetime and edits the message to "Send the reminder text as a message."
- Every redraw edits the one message in place; no new messages per tap.

### 4. Description -> schedule the reminder
While a session is in `await_description`, the NEXT text message from the SAME
`ExpectedSource` becomes the reminder body (mirrors hermes' clarify "type the
answer"). Build `WallTime` from the picked date/time + configured TZ, call
`Scheduler.CreatedParams(id, body, w)` in ONE journal recipe, mark the session
committed (single-use), and confirm ("Podsjetnik postavljen za <local time>.").
A plain text message with NO active session is handled as today (normal turn).

### 5. callback_data grammar (<= 64 bytes, strict)
`pk:v1:<sid>:<act>:<arg>` where `act` in the CLOSED set {`d`(day),`h`(hour),
`m`(minute),`nav`(month +/-),`x`(cancel)}; `arg` numeric/short. Parse with a
fixed-field split and REJECT any deviation (unknown act, wrong field count, bad
sid) as a benign "expired" answer — never act on malformed input.

## Security (callback_data is UNTRUSTED input)
- Strict parse; unknown/extra fields -> reject.
- OWNER BINDING: a callback whose `from.id`+chat != the session's
  `ExpectedSource` is refused (answerCallbackQuery "not your picker"); no
  cross-chat/user control of a session.
- SINGLE-USE terminal commit: a committed/cancelled/expired/unknown `sid` ->
  answerCallbackQuery "isteklo/gotovo", no state change (anti double-fire /
  replay).
- TTL: expired sessions are inert; a sweep or lazy check treats them as
  expired.
- Durable: the session state machine (opened -> date_set -> time_set ->
  await_description -> committed/cancelled) is JOURNALED (new events +
  projection table `picker_sessions`), so a daemon restart mid-pick resumes or
  safely expires — never an in-memory-only wait (mirrors approval / HARDQ B6).

## Detectors (RED before, GREEN after; anchored to the contract)
- callback parse: malformed/unknown-act/short `sid` -> benign expired answer,
  no state change, no reminder.
- owner binding: a callback from a different from.id/chat than ExpectedSource
  is refused; the session is untouched.
- single-use: a second commit callback on a committed session creates NO second
  reminder.
- full flow: open -> pick day -> pick hour -> pick minute -> send description
  produces exactly one `schedule.created` with the WallTime matching the taps
  and the body = the description; confirmation delivered.
- durability: a session persisted then reloaded (fresh Scheduler/journal)
  still commits the correct reminder; replay is idempotent.
- answerCallbackQuery is always called (spinner cleared) even on refusal.

## Non-goals (later slices)
- Recurring schedules (real cron), timezone selection UI, NLP date entry,
  editing/listing existing reminders from the calendar.

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
- Reminder create is the OBLIGATION PAIR, not a bare schedule (r1 codex F1,
  verified): `obligation.Manager.CreateReminder` (obligation.go:702-712)
  appends BOTH `schedule.CreatedParams(id,body,w)` AND an `obligation.created`
  envelope in ONE `AppendBatch` — a schedule alone would never be tracked or
  delivered as an obligation. So the picker MUST emit the pair, and it appends
  internally today, so this slice adds a NON-appending seam
  `Manager.ReminderParams(id, body, w) ([]EnvelopeParams, error)` returning
  `[schedP, oblP]` (refactor `CreateReminder` to call it), so the picker commit
  can batch the pair together with its own commit event. No obligation payload
  is duplicated in the picker.
  `WallTime{Year,Month,Day,Hour,Minute,TZ}` = schedule.go:49-59.
- Update path: `internal/channel/telegram` — `PollOnce` loops updates,
  `processUpdate` currently handles only `u.Message` (`if u.Message == nil {
  return nil }`); it must also branch on a new `u.CallbackQuery`.
- Button/callback discipline (research, docs/CRONJOB-RESEARCH.md): ECC
  `adapter.py` shape — prefix dispatch, fail-closed owner check, single-use
  claim, resolve-before-render, 64-byte callback_data limit.

## Mechanism
### 1. Receive button taps (callback_query) — one owner, typed seams (r1 codex F3)
Extend `tgUpdate` with `CallbackQuery *{ ID string; From *{ID int64}; Message *{
MessageID int64; Chat{ID int64; Type string} }; Data string }`, and VALIDATE all
of `From`, `Message`, `Message.Chat.ID`, `Message.Chat.Type`, `Message.MessageID`
are present — an inaccessible/unsupported callback form is answered benignly
with NO state change. `processUpdate` routes `u.CallbackQuery != nil` to the
picker interaction owner (the adapter, holding the picker store + typed
Telegram ops). The SAME single-user + deny-default gate as messages
(`Chat.Type=="private"`, bound profile, `From!=nil`) applies BEFORE acting.
Delivery-honesty split (explicit): `answerCallbackQuery` and the calendar
render/`editMessageReplyMarkup` are EPHEMERAL interaction chrome (best-effort,
like the existing `sendChatAction`), NOT outbox-tracked. Only the durable
user-visible CONFIRMATION goes through the T22 outbox (below). No unjournaled
durable message is sent.

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

### 4. Description -> schedule the reminder (one durable batch; replay-safe, r1 codex F2)
The description arrives as an ordinary admitted Telegram update. Routing rule:
if `Admit` succeeds for that update AND its `ExpectedSource` owns an
`await_description` picker session, it is the description; otherwise it is a
normal turn (today's path). Ownership is unambiguous: the session is keyed to
`ExpectedSource` and is single-use, so at most one open session per source.
COMMIT is ONE `AppendBatch` sharing a single journal recipe:
`[picker.committed(sid), schedP, oblP, channel inbound-terminal(for THIS
update), outbox confirmation]` — the reminder pair from `Manager.ReminderParams`
(F1) plus the picker commit plus the message's terminal plus the confirmation,
all durable together or not at all.
Replay-safety: the batch is keyed to the admitted update id; a NON-terminal
replay of that update returns the SAME confirmation and does NOT re-interpret
the text or create a second reminder (the session is already committed ->
single-use short-circuit). Simultaneous descriptions cannot both win: the first
commit marks the session committed; a second observes committed and answers
"already set". Confirm: "Podsjetnik postavljen za <local time>." A crash AFTER
the batch commits is a no-op on replay (terminal); a crash BEFORE leaves the
session `await_description` and the update non-terminal -> re-processed once.

### 5. callback_data grammar (<= 64 bytes, strict per-action, r1 codex F4)
`pk:v1:<sid>:<act>:<arg>`, fixed 5 fields. Closed per-action validation, each
also checked LEGAL for the session's current state:
- `sid`: exactly the generated alphabet/length (e.g. 22 url-safe base64 chars).
- `nav`: `arg` in {`-1`,`+1`}, a bounded one-step month change; legal only in
  the month view.
- `d`(day): `arg` = `YYYY-MM-DD` within the shown month's valid range; legal in
  month view.
- `h`(hour): `arg` integer `0..23`; legal only after a day is set.
- `m`(minute): `arg` in exactly {`0`,`15`,`30`,`45`}; legal only after an hour.
- `x`(cancel): empty `arg`; legal in any non-terminal state.
Any deviation (unknown act, wrong field count, bad sid, out-of-range arg, or an
action illegal in the current state) is answered benignly ("isteklo") with NO
journal mutation and NO reminder.

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
  produces exactly one reminder PAIR (`schedule.created` AND `obligation.created`
  with matching id) whose WallTime matches the taps and body = the description;
  confirmation delivered. RED against emitting the schedule half only (F1).
- one batch / replay: the commit is a single AppendBatch; a non-terminal replay
  of the description update returns the same confirmation and creates NO second
  reminder (F2).
- simultaneous description: two description messages for one session -> exactly
  one reminder; the second is answered "already set".
- crash windows: crash after the batch = no-op replay (terminal); crash before =
  session stays await_description and the update is re-processed once.
- durability: a session persisted then reloaded (fresh Scheduler/journal)
  still commits the correct reminder pair.
- illegal-in-state callback (e.g. `h` before a day is set) is answered benignly
  with no state change.
- answerCallbackQuery is always called (spinner cleared) even on refusal.

## Non-goals (later slices)
- Recurring schedules (real cron), timezone selection UI, NLP date entry,
  editing/listing existing reminders from the calendar.

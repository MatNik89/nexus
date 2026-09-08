# CRONJOB-RESEARCH — Telegram inline calendar date/time picker

Research for a durable, single-owner Go personal-assistant bot. Evidence-first;
every claim carries a cited source (official docs, real implementations, or a
local file path). No invented APIs. English / Latin only.

Scope: build an inline-keyboard date+time picker (month grid -> hour grid ->
minute grid -> free-text description) for creating reminders/tasks, obeying the
NEXUS hard rule that HITL/multi-step waits are **durable (journaled), never an
in-memory blocking wait, and survive a daemon restart** (repo CLAUDE.md; HARDQ
B6).

---

## 0. TL;DR ranked recommendation

- **(a) Encode calendar state:** HYBRID. Put only *navigation cursor + a short
  opaque session id* in `callback_data`; keep the accumulating selection
  (chosen date, chosen hour, description) in a durable picker session. Do NOT
  try to pack the whole in-progress datetime+description into 64 bytes.
  Rationale: §1.4 (hard 64-byte limit) + §3.
- **(b) Store the in-progress picker durably:** journal it, exactly like the
  existing `internal/approval` challenge. A picker session is a small
  append-only state machine (`picker.opened` -> `picker.date_set` ->
  `picker.time_set` -> `picker.committed`/`picker.cancelled`) on the profile's
  single journal, keyed by a random `session_id`, carrying `ExpectedSource`
  (owning chat+user) and an `expires_unix`. This reuses the proven pattern in
  `internal/approval/approval.go` and satisfies HARDQ B6 verbatim. §3, §5.
- **(c) callback_data grammar:** `pk:<v>:<sid>:<act>:<arg>` — see §6 for the
  exact grammar, field widths, and the 64-byte budget proof.
- **(d) Security checks:** strict parse (reject malformed / reject unknown
  `act`), bind every tap to the session's `ExpectedSource` (deny cross-chat /
  cross-user), single-use claim on the terminal action (anti double-commit),
  and treat a tap on an expired/consumed/unknown session as a benign
  "expired" answer — never an error and never an action. §4.

The single most valuable finding: **the NEXUS repo already contains the exact
durable, exact-intent, single-use, source-bound challenge machine this picker
should be modeled on** — `internal/approval/approval.go`. Steal its shape; do
not reinvent it. §5.

---

## 1. Telegram Bot API callback_query mechanics

Source: official Bot API reference, https://core.telegram.org/bots/api
(fetched 2026-09-08). Field wording below is quoted from that page.

### 1.1 Update.callback_query
- `Update.callback_query` — type `CallbackQuery`, "*Optional*. New incoming
  callback query". A button tap arrives as an ordinary `getUpdates` /
  webhook `Update` whose `callback_query` field is populated.

### 1.2 The CallbackQuery object (quoted fields)
| Field | Type | Quoted description |
|---|---|---|
| `id` | String | "Unique identifier for this query" |
| `from` | User | "Sender of the query" |
| `message` | MaybeInaccessibleMessage | "*Optional*. Message with the callback button that originated the query" |
| `inline_message_id` | String | "*Optional*. Identifier of the message sent via the bot in inline mode, that originated the query" |
| `chat_instance` | String | "Global identifier, uniquely corresponding to the chat to which the message with the callback button was sent" |
| `data` | String | "*Optional*. Data associated with the callback button. Be aware that a bad client can send arbitrary data in this field (1-64 bytes)" |
| `game_short_name` | String | "*Optional*. Short name of a Game..." |

Two decisive facts here: `data` is **1-64 bytes** and a **"bad client can send
arbitrary data"** — i.e. `callback_data` is UNTRUSTED input (drives §4), and
`from` (the tapping user) is authoritative for authorization, not the message
author.

### 1.3 answerCallbackQuery (stop the spinner)
Parameters (official): `callback_query_id` (required), `text` (optional, 1-200
chars), `show_alert` (optional bool), `url` (optional), `cache_time` (optional
int).

Documented behavior (this is the reason the call is mandatory): after a user
presses a callback button, Telegram clients display a progress bar / loading
spinner until `answerCallbackQuery` is called, so the bot must call it even
when it shows no notification (call with no optional params to just clear the
spinner). Source: https://core.telegram.org/bots/api#answercallbackquery and
the CallbackQuery note on the same page.

Practical rule for the picker: **every** branch of the callback handler
(navigation, selection, denied, expired, malformed-but-answerable) ends by
answering the query. Only a hard-drop of an unparseable/foreign tap skips it.

### 1.4 Editing the keyboard in place (redraw without new messages)
- `editMessageReplyMarkup` — parameters `chat_id` (Integer|String, optional),
  `message_id` (Integer, optional), `inline_message_id` (String, optional),
  `reply_markup` (InlineKeyboardMarkup, optional). Use this to swap the whole
  keyboard (month -> next month, month -> hour grid) on the *same* message.
- `editMessageText` — replaces text + optional `reply_markup` on the same
  message; use when the prompt line changes too ("Pick a day" -> "Pick the
  hour"). (Param table on the page was not cleanly captured in the fetch; the
  shape mirrors editMessageReplyMarkup plus `text`/`parse_mode`. Verify the
  exact list against the live page before coding — flagged, not assumed.)
  Source: https://core.telegram.org/bots/api#editmessagereplymarkup ,
  https://core.telegram.org/bots/api#editmessagetext

Editing in place is what makes a calendar feel like a widget: one message,
redrawn on each tap, instead of a flood of new messages.

Gotcha (from real calendar code, §2): editing to an *identical* markup raises
Telegram "Bad Request: message is not modified". Libraries dodge this by
appending a random salt to `callback_data` so the markup always differs
(artembakhanov `is_random`, §2.2). For an in-place picker prefer to only edit
when the view actually changes, and answer the query otherwise.

### 1.5 InlineKeyboardMarkup / InlineKeyboardButton
- `InlineKeyboardMarkup.inline_keyboard` — "Array of button rows, each
  represented by an Array of `InlineKeyboardButton` objects".
- `InlineKeyboardButton.callback_data` — "Data to be sent back to the bot when
  the button is pressed, **1-64 bytes**".
  Source: https://core.telegram.org/bots/api#inlinekeyboardmarkup ,
  https://core.telegram.org/bots/api#inlinekeyboardbutton

**The 64-byte ceiling is the single hardest design constraint** and is why the
recommendation (§0, §3, §6) keeps bulk state server-side.

---

## 2. Inline calendar UX patterns — 4 real implementations

### 2.1 unmonoqueteclea/calendar-telegram (pyTelegramBotAPI)
Source: https://github.com/unmonoqueteclea/calendar-telegram/blob/master/telegramcalendar.py

- Layout: header row (month + year, ignore buttons), weekday header row, up to
  6 rows of day buttons, then a nav row `< >`.
- Encoding: `;`-separated `"<CALENDAR_CALLBACK>;<action>;<year>;<month>;<day>"`.
  - blank/ignore + weekday headers: `...;IGNORE;year;month;0`
  - selectable day: `...;DAY;year;month;day`
  - prev month: `...;PREV-MONTH;year;month;0`; next month: `...;NEXT-MONTH;...`
- Handler splits on `;` via `separate_callback_data` -> `(prefix, action,
  year, month, day)`; `IGNORE` does nothing (just answers), `PREV/NEXT-MONTH`
  re-renders, `DAY` is the terminal selection.
- Takeaway: fully **stateless** — the entire cursor (year+month) lives in each
  button's `callback_data`. Works because it only ever selects a *date* (no
  time, no description), so it never overflows 64 bytes.

### 2.2 artembakhanov/python-telegram-bot-calendar (DetailedTelegramCalendar)
Source: https://github.com/artembakhanov/python-telegram-bot-calendar ,
base.py `_build_callback`.

- Steps: `LSTEP` = YEAR -> MONTH -> DAY. `process(callback_data)` returns
  `(result, keyboard, step)`: `result` is a `date` when finished else `None`;
  `keyboard` is the next markup; `step` is the current LSTEP.
- Encoding: prefix `cbcal` (const `CB_CALENDAR`), `_`-separated:
  `"cbcal_<calendar_id>_<action>_<step>_<year>_<month>_<day>"`; with
  `is_random=True` a salt `"_" + randint(1,1e18)` is appended to defeat
  "message is not modified".
- `calendar_id` (small int/str) namespaces multiple concurrent calendars in one
  chat; `min_date`/`max_date` prune out-of-range buttons.
- Takeaway: still stateless, but the README warns the salt (up to ~19 digits)
  plus fields pushes against the 64-byte limit — concrete evidence that a
  stateless encoding does not scale once you add fields.

### 2.3 noXplode/aiogram_calendar (aiogram 3, typed callback factory)
Source: https://github.com/noXplode/aiogram_calendar (and aiogram3 fork
https://github.com/o-murphy/aiogram3_calendar)

- Uses aiogram's `CallbackData` factory: a typed `SimpleCalendarCallback` with
  named fields (`act`, `year`, `month`, `day`), serialized to a `prefix:...`
  string and parsed back into a typed object — the framework enforces the
  shape on parse (malformed -> no match). Two modes: navigation calendar
  (month/year arrows) and dialog calendar (year -> month -> day).
- `SimpleCalendar().start_calendar()` builds the markup;
  `process_selection(callback_query, callback_data)` handles a tap.
- Takeaway: the **typed-callback** discipline (a fixed grammar validated on
  parse) is the right model for §4's "reject malformed" — mirror it in Go with
  a strict `parseCallback` that returns an error on any deviation.

### 2.4 NEXUS-local hermes adapter (production, Python, the closest prior art)
Source: `/home/matej/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py`

Not a calendar, but the same button-driven, stateful, multi-choice flow
(exec-approval, slash-confirm, clarify, model picker, paged pickers) — see §5
for the security and state details. Its callback_data grammar is the compact
`"<prefix>:<verb>:<id>"` family (`ea:once:<id>`, `sc:cancel:<id>`,
`cl:<id>:<idx>`, `mpv:<page>`) with short prefixes precisely because of the
64-byte cap (in-code comment line 3847: "Telegram caps callback_data at 64
bytes; keep 'cl:<id>:<idx>' short."). This is the grammar §6 adopts.

### Common design distilled
1. A grid message edited **in place** (§1.4) as the user drills down.
2. Distinct button classes: **navigation** (prev/next, drill-down) vs
   **selection** (terminal for that step) vs **ignore** (headers/padding).
3. State is carried either entirely in `callback_data` (stateless) or split
   between a cursor in `callback_data` and a server session.
4. Final datetime is assembled step-by-step; the **free-text description** is a
   separate capture step (a follow-up text message intercepted for the session)
   — exactly how the hermes `clarify` "✏️ Other (type answer)" branch flips to
   text-capture (adapter.py line 3849).

For this picker the flow is: **month grid (days) -> hour grid (00-23) ->
minute grid (e.g. 00/05/.../55) -> prompt for a free-text description as the
next message -> commit reminder**.

---

## 3. Stateful multi-step flow, done DURABLY — two approaches

### (a) All state in callback_data (stateless server)
- How: pack cursor + partial selection into each button's `callback_data`
  (§2.1, §2.2). No server memory; a daemon restart loses nothing because the
  next tap re-supplies the state.
- Pros: trivially crash-safe; no session store; no expiry bookkeeping.
- Cons — **fatal here**:
  - 64-byte hard cap (§1.4). A date+hour+minute+calendar_id+action already
    crowds the budget; a free-text **description cannot** live in
    `callback_data` at all. artembakhanov's own salt-vs-64-byte warning (§2.2)
    is direct evidence this does not scale past a bare date.
  - `callback_data` is untrusted (§1.2) — a stateless design must re-validate
    the *entire* selection on every tap and has nowhere to anchor
    "who owns this picker" except re-deriving it, which it cannot bind to a
    server-side expiry/consumption record.
  - No natural place to journal the in-progress reminder, which the repo rule
    (HARDQ B6) requires.

### (b) Persist an in-progress picker session (recommended)
- How: on open, create a durable session keyed by a random `session_id`;
  `callback_data` carries only `session_id` + a compact cursor/action. Each tap
  advances a small journaled state machine.
- Pros: no byte pressure (description and full datetime live server-side);
  survives restart by definition (journaled); gives a single anchor for
  auth-binding, expiry, and single-use (§4); matches the existing NEXUS
  approval/schedule machinery (§5) so it is *reuse*, not new machinery.
- Cons: needs an expiry sweep and a stale-tap answer path (both already exist
  in `internal/approval`).

### Recommendation for a single-owner Go bot with a SQLite journal
**Approach (b), implemented as a journaled state machine on the profile's one
journal**, keyed by `session_id`, carrying `ExpectedSource` + `expires_unix`,
terminal step single-use — i.e. a near-clone of `internal/approval`'s
`Suspend`/`decide`/`ConsumeApproval` lifecycle (§5). Put a **compact cursor**
in `callback_data` too (year-month for the day grid, chosen day for the hour
grid) so a redraw needs no read for pure navigation, but treat the journaled
session as the source of truth for anything that commits.

This is the only approach that simultaneously (i) respects 64 bytes, (ii)
holds a free-text description, (iii) is crash-safe by construction, and (iv)
reuses proven code instead of adding a parallel state store.

---

## 4. SECURITY — callback_data is untrusted input

Official basis: `CallbackQuery.data` is 1-64 bytes and "**a bad client can send
arbitrary data**" (§1.2, https://core.telegram.org/bots/api). Therefore every
tap is attacker-controlled bytes; the *button you rendered* is only a
suggestion.

Standard hardening (each mapped to real code):

1. **Strict parse, reject malformed.** Split on the known separator, require
   the exact field count, and reject any unknown `act`/verb. Real precedent:
   hermes returns silently on a wrong split length and answers "Invalid
   approval data." on a non-int id
   (`adapter.py` `_handle_exec_approval_callback`, lines 4275-4283:
   `parts = data.split(":", 2); if len(parts) != 3: return`, then
   `int(parts[2])` guarded). aiogram enforces this structurally via the typed
   `CallbackData` factory (§2.3). Go: a `parseCallback` returning `(cb, error)`;
   any error -> answer "expired/invalid", take no action.

2. **Bind the tap to the session owner (deny cross-chat / cross-user).** The
   authorization identity is `callback_query.from` (the tapper), and the
   session must record who may act on it. Real precedent: hermes builds a
   callback context from the query and gates it before doing anything —
   `_callback_ctx` (adapter.py 4222) + `_callback_authorized` (4230) call
   `_is_callback_user_authorized(user_id, chat_id, chat_type, thread_id)`
   (746), which is **fail-closed**: "no allowlist means deny unless
   GATEWAY_ALLOW_ALL_USERS is set" (line 777-780). In NEXUS the durable analog
   is `approval.suspendedPayload.ExpectedSource` — "binds WHO may decide: the
   originating channel identity ... a foreign chat can never approve"
   (`internal/approval/approval.go` lines 74-76). Store the owning
   chat_id+user_id on the picker session and compare on every tap.

3. **Single-use terminal action (anti replay / double-commit).** The step that
   creates the reminder must be claimed exactly once. Real precedent: hermes
   `_claim_callback_state` **pops** the pending entry so a second tap finds
   nothing and is answered "already resolved" (adapter.py 4264-4271); and it
   resolves BEFORE rendering so a late tap after timeout (count==0) does not
   falsely claim success (comment 4290-4295). NEXUS durable analog:
   `decide()` rejects any challenge "not PENDING (unknown, decided, expired, or
   replayed)" with typed `ErrApprovalReplay` (approval.go 36, 208-222), and
   `ConsumeApproval` "burns the approval for THIS exact call (single-use)"
   (approval.go 481-484). Model the picker's `picker.committed` event the same:
   a CAS/append that fails if the session is not still open.

4. **Stale / expired sessions are benign, not errors.** A tap landing on an
   expired or already-consumed session must answer a friendly "This picker
   expired" and do nothing — never throw, never act. Precedent: hermes renders
   "⌛ Approval expired — no command was waiting" on count==0 (adapter.py
   4309-4311); NEXUS bounds answerability with `DefaultChallengeTTL = 15m`
   (approval.go 33) and an `expires_unix` check. Add a TTL to the picker
   session (a picker is a short interaction; 15 min is reasonable) and a sweep.

5. **Where real bots get this wrong (cite):**
   - **Trusting the button, not the tapper:** acting on `callback_data` without
     checking `from`/owner lets any group member resolve another user's prompt.
     hermes fixed exactly this class with the fail-closed gate (#24457 referenced
     in-code at adapter.py 779) and with `_source_from_message_for_auth`
     falling back to `sender_chat` "so an unauthorized channel can't inject"
     (adapter.py 785).
   - **No single-use -> double action / stale success:** resolving after a
     wait timed out and still reporting "Approved" — the regression hermes
     guards against by resolving-first and treating count==0 as expired
     (adapter.py 4290-4295, referencing regression #63501).
   - **Stateless designs re-trusting packed state:** a stateless calendar that
     reads year/month/day straight out of `callback_data` and writes a record
     without re-validating bounds trusts attacker bytes; artembakhanov mitigates
     only *rendering* via `min_date`/`max_date` pruning (§2.2), which does not
     stop a hand-crafted out-of-range `callback_data` — the server must
     re-validate. (General class; official basis §1.2.)
   - **Rate/DoS:** taps are cheap to spam; a per-owner picker session with TTL +
     single-use naturally caps damage, and `answerCallbackQuery`'s `cache_time`
     (§1.3) lets the client suppress rapid re-taps. NEXUS journal is a single
     serialized append actor (CLAUDE.md P0.3 / HARDQ B7), which serializes
     concurrent taps deterministically.

---

## 5. Local source check (honest report)

Command basis: `rg -i 'callback_query|InlineKeyboard|answerCallbackQuery|
calendar|reminder|schedule' ~/.hermes /home/matej/HARNESS --type py --type go`.

### 5.1 NEXUS repo (Go) — the primary steal target
- `/home/matej/HARNESS/nexus/internal/approval/approval.go` (687 lines).
  A **durable, exact-intent, single-use, source-bound, expiring** HITL
  challenge machine on the profile journal. This is a direct template for the
  durable picker session. Key stealable pieces:
  - Closed event set + `PayloadValidator`s (`Events()`, lines 96-145) — the
    "fail-closed on unknown typed inputs" pattern for picker events.
  - `Suspend()` (294) commits state in ONE journal transaction and the loop
    EXITS — the canonical "durable, never in-memory wait".
  - `ExpectedSource` binding (74-76) — copy verbatim as the picker's owner bind.
  - `decide()` (448-470) + `ErrApprovalReplay` (36) — single-use / replay reject.
  - `DefaultChallengeTTL` (33) + expiry status in `Apply` (178-263) — the TTL
    and stale-tap handling.
  - Projection into a SQLite table (`Init`/`Apply`, 152-263) — how the durable
    state is queried for a "still PENDING?" check on each tap.
- `/home/matej/HARNESS/nexus/internal/schedule/schedule.go` (498 lines).
  The reminder side the picker feeds: `WallTime` (validate/`dueUTC`/`sameWall`,
  51-110), closed events `schedule.occurrence_fired` etc., `Scheduler` sweep
  over the one journal (`New`, 270; `SetFireDecorator`), `CreatedParams`
  (319) to append a schedule. The picker's commit step should produce a
  `schedule` created-event via this API rather than writing SQLite directly.
- `/home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go` (408 lines)
  + `render.go` — the existing Telegram builtin channel adapter; the callback
  handler + keyboard rendering for the picker belong here. (No inline-keyboard
  callback handling present yet — this is greenfield to add.)

### 5.2 hermes-agent (Python) — the closest working button-flow prior art
- `/home/matej/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py`
  (6549 lines). Production callback flow. Concrete, reusable patterns:
  - Dispatch by `callback_data` prefix: `_handle_callback_query` (4240-4262)
    matches short prefixes and routes to per-feature handlers.
  - Auth gate: `_callback_ctx` (4222), `_callback_authorized` (4230),
    `_is_callback_user_authorized` (746, fail-closed 777-780).
  - Single-use claim: `_claim_callback_state` (4264-4271, pops entry).
  - Prompt builders that map a short `callback_data` id -> a server-side
    session key: `send_exec_approval` (3795, `ea:<choice>:<id>`),
    `send_slash_confirm` (3820, `sc:<choice>:<id>`), `send_clarify` (3836,
    `cl:<id>:<idx>` + "✏️ Other (type answer)" text-capture branch 3849).
  - The 64-byte discipline is explicit in-code (comment line 3847).
  - NOTE: hermes keeps the id->session map **in memory** (`_approval_state`,
    `itertools.count`), so it is NOT restart-durable. That is acceptable for
    hermes' short approval waits but is exactly what the NEXUS rule forbids —
    so steal the *shape* (prefix dispatch, auth gate, single-use claim,
    resolve-before-render) but back it with the journaled session from §5.1,
    not an in-memory dict.
- Tests worth reading as executable specs of the flow:
  `/home/matej/.hermes/hermes-agent/tests/gateway/test_telegram_approval_buttons.py`,
  `.../test_telegram_clarify_buttons.py`.

### 5.3 Honest gaps
- No existing **calendar** widget anywhere local (Python or Go) — the grid
  rendering is genuinely new code.
- The Bot API `editMessageText` full param list was not cleanly captured by the
  doc fetch (§1.4); confirm against the live page before implementing.
- The nexus Telegram adapter has **no** inline-keyboard/callback handling today;
  §5.1/§5.2 give the pattern but the wiring is to be written.

---

## 6. Exact callback_data grammar to use

Design goals: strict, self-describing, comfortably under 64 bytes (§1.4),
prefix-dispatchable (§5.2), and carrying only a cursor + opaque session id
(§3b). Reject anything not matching this grammar (§4.1).

```
callback_data := "pk" SEP ver SEP sid SEP act SEP arg
SEP := ":"                         ; single-byte separator (matches hermes)
ver := 1*1 DIGIT                   ; grammar version, currently "1"
sid := 8*8 (base32 lower / hex)    ; opaque picker session id (random)
act := "d" / "h" / "m" / "n" / "p" / "x" / "i"
        ; d=pick day, h=pick hour, m=pick minute,
        ; n=next-month, p=prev-month, x=cancel, i=ignore (headers/padding)
arg := 0*6 (DIGIT / "-")           ; act-specific payload, compact
```

Per-action `arg`:
- `pk:1:<sid>:d:<YYYY-MM-DD>`  day selected (arg = ISO day, 10 bytes)
- `pk:1:<sid>:n:<YYYY-MM>`     jump to next month (arg = 7 bytes)
- `pk:1:<sid>:p:<YYYY-MM>`     jump to prev month
- `pk:1:<sid>:h:<HH>`          hour selected (arg 2 bytes)
- `pk:1:<sid>:m:<MM>`          minute selected
- `pk:1:<sid>:x:`             cancel (arg empty)
- `pk:1:<sid>:i:`             ignore (weekday headers, blank padding)

Byte-budget proof (worst case, the day button):
`"pk" + ":" + "1" + ":" + 8 + ":" + "d" + ":" + "YYYY-MM-DD"`
= 2+1+1+1+8+1+1+1+10 = **26 bytes** << 64. Ample headroom; no salting needed
because we only edit the message when the view actually changes (§1.4 gotcha).

Parse rule (Go): `strings.SplitN(data, ":", 5)`; require exactly 5 parts;
`parts[0]=="pk"`, `parts[1]=="1"`, `parts[2]` matches the sid charset/length,
`parts[3]` in the closed `act` set, and `arg` validated per-act (e.g. day
re-parsed with `time.Parse("2006-01-02", ...)` and range-checked against the
session). Any deviation -> `answerCallbackQuery("This picker expired")` + no
action (§4.1, §4.4). The full date/time being assembled and the free-text
description live in the journaled session, never in `callback_data` (§3b).

Text-description step: after minute selection, edit the message to "Send me a
short description", set the journaled session to `awaiting_description`, and let
the channel's next text message from the **same ExpectedSource** be intercepted
and stored, then commit the reminder via `schedule.CreatedParams` (§5.1). This
mirrors the hermes clarify "type answer" branch (adapter.py 3849).

---

## 7. Sources
- Telegram Bot API (official): https://core.telegram.org/bots/api
  (CallbackQuery, answerCallbackQuery, editMessageReplyMarkup, editMessageText,
  InlineKeyboardMarkup, InlineKeyboardButton).
- unmonoqueteclea/calendar-telegram:
  https://github.com/unmonoqueteclea/calendar-telegram/blob/master/telegramcalendar.py
- artembakhanov/python-telegram-bot-calendar:
  https://github.com/artembakhanov/python-telegram-bot-calendar
- noXplode/aiogram_calendar: https://github.com/noXplode/aiogram_calendar ;
  aiogram3 fork: https://github.com/o-murphy/aiogram3_calendar
- Local: `/home/matej/HARNESS/nexus/internal/approval/approval.go`
- Local: `/home/matej/HARNESS/nexus/internal/schedule/schedule.go`
- Local: `/home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go`
- Local: `/home/matej/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py`

Confidence: HIGH on the local NEXUS/hermes patterns (read directly) and on the
64-byte / untrusted-data / answerCallbackQuery facts (official page, quoted).
MEDIUM on the exact `editMessageText` parameter list (fetch was partial — verify
live) and on aiogram's precise callback field names (from README/search, not a
line-level read of its source). No fabricated APIs.

# Review of `/cronjob` picker plan r2 (`docs/PLAN-CRONJOB.md`)

**Scope**: `docs/PLAN-CRONJOB.md` at `116695d` on `main`, with
`docs/CRONJOB-RESEARCH.md`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. All four round-1 folds are closed and verified against the actual code.
No substantive correctness / security / behavioral-equivalence / unbuildable
defect found.

## Round-1 folds — closure status

### F1 — reminder is the OBLIGATION PAIR via a non-appending seam — closed

Verified: `obligation.Manager.CreateReminder` (`obligation.go:700-713`) already
builds `schedP = m.sched.CreatedParams(id, body, w)` (`:703`) AND
`oblP = m.params(EvCreated, createdPayload{ID, Kind:"reminder", Body})` (`:707`)
and appends them in ONE `AppendBatch` (`:711`). The proposed
`Manager.ReminderParams(id, body, w) ([]EnvelopeParams, error)` is a mechanical
extract-and-return refactor of exactly that pair — buildable, and the plan no
longer emits a bare `schedule.created`.

### F2 — description commit is ONE AppendBatch, replay-safe — closed

The batch `[picker.committed(sid), schedP, oblP, channel inbound-terminal,
outbox confirmation]` (`PLAN-CRONJOB.md:73-77`) is buildable:
`Journal.AppendBatch` exists (`journal.go:601-603`); `EvInboundTerminal` +
`EvOutboundEnqueued` are the real channel events (`channel.go:39-40`), and the
channel's own `CompleteInbound` already does exactly `[termP, outP]` in one
batch (`channel.go:605-623`). Replay-safety, single-use short-circuit,
simultaneous-description ("already set"), and both crash windows are specified
(`:78-85`).

### F3 — callback_query full validation + ephemeral/outbox split — closed

`CallbackQuery` now validates `From`, `Message`, `Message.Chat.ID`,
`Message.Chat.Type`, `Message.MessageID` present, and an unsupported form is
answered benignly with no state change (`:37-42`) — closing the round-1 nil-
deref note. The delivery-honesty split (`:46-50`) correctly classifies
`answerCallbackQuery` + calendar `editMessageReplyMarkup` as ephemeral chrome
(like `sendChatAction`) and routes only the durable confirmation through the
T22 outbox — no unjournaled durable send.

### F4 — strict per-action grammar + legal-in-state — closed

`pk:v1:<sid>:<act>:<arg>` with per-action arg validation (nav ±1, day
YYYY-MM-DD in shown month, hour 0-23, minute {0,15,30,45}, cancel empty) AND a
legal-in-current-state check (`:87-100`) — a hand-crafted out-of-order tap
(e.g. `m` before a day) is answered benignly with no mutation. Closes the
round-1 "illegal-in-state" note.

## Notes (not FAIL reasons)

1. **"At most one open session per source" is asserted, not enforced.** The
   reasoning "single-use ⇒ at most one open" (`:71-72`) is fallacious — single-
   use constrains the terminal commit, not the count of concurrent open
   sessions. The session is keyed by a random `sid`, not by `ExpectedSource`, so
   a re-tap of `/cronjob` while a picker is open creates a SECOND session and
   makes the description routing ("an await_description session for this
   source") ambiguous — the user's "start over" intent would commit the
   description to the stale session's date/time. Fix is one mechanism: on open,
   supersede (append `picker.cancelled` for any existing open session of that
   source) or refuse ("a picker is already open"). This is the single remaining
   correctness gap; everything else is closed.
2. **The channel's non-appending seam is assumed, not named.** The commit batch
   needs `[inbound-terminal, outbox]` params from the channel core, whose
   `params` is private and whose `CompleteInbound` appends internally
   (`channel.go:385,605-623`). The plan names the obligation `ReminderParams`
   seam (F1) but not the analogous channel seam — same mechanical extract-
   and-return refactor, worth one line.

VERDICT: PASS

# Review of `/cronjob` picker plan v3 (PRAGMATIC) (`docs/PLAN-CRONJOB.md`)

**Scope**: `docs/PLAN-CRONJOB.md` at `8216aae` on `main`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

FAIL. The ephemeral-picker ceiling is accepted as owner-chosen and is NOT the
reason. The defect is in the DURABLE path the plan itself claims is solid: the
confirmation is not actually durable, because the description handler clears
the in-memory session BEFORE the confirmation is journaled, so a crash in that
window double-processes the description as a normal turn and never delivers the
"Podsjetnik postavljen" confirmation.

## Substantive finding

### F1 — the confirmation's durability is broken; the description can be double-processed

The plan's durability scope claims "DURABLE (survives any restart): the created
reminder + its confirmation" (`:12`). The mechanism does not deliver that.

The description branch (`:65-81`) is:
1. `processUpdate` runs `Admit` → `handle` (today's recipe).
2. The handler gates on the in-memory session ("does this source own an
   `await_description` session?").
3. `CreateReminder(id, body, w)` — durable (obligation pair,
   `obligation.go:702-712`).
4. **"Clear the in-memory session" (`:77`)** — inside the handler, before it
   returns.
5. The handler returns the confirmation string; `processUpdate` then runs
   `CompleteInbound` (terminal + outbox confirmation).

The session is cleared at step 4, BEFORE the confirmation is durably enqueued at
step 5. A crash between steps 4 and 5 leaves: the reminder durable, the inbound
ADMITTED and NON-terminal, and the session GONE. On replay — **with or without a
restart** — the handler's session gate finds no session, so the same admitted
description text re-runs as a NORMAL turn: the model answers "call the doctor"
as chat, and the "Podsjetnik postavljen" confirmation is never enqueued.

The deterministic-id idempotency (`:70-74`) does NOT close this, because it is
gated behind the session check — once the session is cleared, the idempotency
lookup is unreachable and the text is mis-routed to the normal-turn path. So the
"idempotent create" prevents a double **reminder** but not the double
**processing** of the description, and not the lost confirmation.

This is an admission/outbox-honesty defect in the durable path, not an
ephemeral-picker weakness: the reminder half is durable, the confirmation half
is session-dependent and therefore not "survives any restart" as claimed.

### Fix (either, both mechanical)

- Clear the session only AFTER `CompleteInbound` succeeds (so a non-terminal
  replay still finds the session and re-confirms); or
- Make the description detection session-independent: on a non-terminal replay,
  look up `rem-<hash(adapter|identity|update_id)>` in the schedule projection
  (the id is derivable from the update identity alone, no WallTime needed) and
  re-confirm when it exists — evaluating that lookup BEFORE the session gate.

## Verified correct (not the FAIL reason)

- Reminder idempotency against **double-create**: `CreateReminder` batches
  `[schedule.created, obligation.created]` atomically (`obligation.go:702-712`),
  and the schedule projection already aborts a duplicate id (`schedule.go:334`),
  so a re-run of the SAME id cannot create a second reminder. The projection
  lookup (`sched_schedules WHERE id=?`) is buildable (`schedule.go:348`).
- Deterministic id from `(adapter|identity|update_id)` is stable across
  redelivery (Telegram update ids are stable).
- Description-vs-normal-turn routing for the NON-crash case is sound: one
  live session per source (REPLACE on open, `:52-53`), no wrong-source capture,
  single-threaded poll loop removes the race.
- callback_query gate + strict grammar + legal-in-state + owner binding + benign
  rejection are correct and buildable against `tgUpdate`/`processUpdate`
  (`telegram.go:125-139,254-276`).
- The ephemeral/durable split is otherwise honest: calendar sends/edits and
  `answerCallbackQuery` are best-effort `a.call` chrome; the reminder is
  journaled. The one dishonesty is the confirmation (above).

## Notes (not FAIL reasons)

- The ephemeral picker itself (in-memory session, TTL, re-run on crash) is the
  accepted owner ceiling and is not a defect; a `tap -> immediate callback` is
  not a blocking HITL wait, so HARDQ B6 does not govern it.

VERDICT: FAIL

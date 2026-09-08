# Review of `/cronjob` picker plan v3 (PRAGMATIC) r2 (`docs/PLAN-CRONJOB.md`)

**Scope**: `docs/PLAN-CRONJOB.md` at `acbad82` on `main`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The round-3 F1 (confirmation not durable across the crash window) is
closed and buildable. No new substantive defect in the durable path.

## Round-3 F1 — closure status

### Confirmation durability via a session-independent front lookup — closed

- The handler's FIRST step for every admitted text derives
  `rem-<hash(adapter|identity|update_id)>` and runs a typed
  `Manager.ReminderIntent(id) -> {NotFound|Found|StorageError}` BEFORE any
  session gate (`:69-81`). On a crash-after-create-before-terminal, the replay
  hits `Found` and reconstructs "Podsjetnik postavljen za <wall>" from the
  persisted `body`+`wall` — independent of the (lost) in-memory session. This
  is exactly the round-3 fix, and it no longer depends on the best-effort
  session clear (`:91-92` is now explicitly non-authoritative).
- **Buildability of the lookup**: `sched_schedules` persists `id`, `body`,
  `wall` (`schedule.go:179-183`; `createdPayload{ID,Body,Wall,DueUTC}` at
  `:122-128`, `wall` JSON-marshaled at `:208-219`). The obligation `Manager`
  holds `m.sched` and `m.j` (`obligation.go:702-711`), so a
  `QueryProjection("SELECT body, wall FROM sched_schedules WHERE id=?")`
  returning `(Found, body, wall)` vs `NotFound` (no row) vs `StorageError`
  (non-`ErrNoRows`) is a mechanical typed method. The confirmation is a pure
  function of the persisted `WallTime`, so re-confirmation is deterministic.
- **Double-create still impossible**: the deterministic id is derived from the
  update identity alone (stable across Telegram redelivery), and
  `CreateReminder` batches `[schedule.created, obligation.created]` atomically
  (`obligation.go:702-712`); the schedule projection aborts a duplicate id
  (`schedule.go:334`). The `Found` path never calls `CreateReminder`, so replay
  cannot double-create.

## Notes (not FAIL reasons)

1. **The MISMATCH guard can over-fail-closed.** `:78-79` fails closed when a
   live session's intent differs from the persisted reminder. In the narrow
   window (a description committed but crashed pre-terminal, then the owner
   opened a NEW picker before the old update's replay), the front lookup
   correctly `Found`s the old reminder, but the unrelated new session's intent
   mismatches and suppresses the re-confirmation. Safe (no double reminder, no
   wrong commit — R1 is still durable), only its confirmation is dropped in
   that anomaly. Defense-in-depth behaving conservatively.
2. **StorageError blocks ALL messages, not just descriptions.** Because the
   lookup runs before routing, a transient projection-read failure fails the
   handler closed for a plain normal turn too (no fall-through). Correct — the
   alternative mis-routes a description — but it means a projection read
   failure takes the whole channel offline rather than just the picker. A
   catastrophic-state tradeoff, acceptable for single-owner.
3. **One extra O(1) indexed read per admitted message** (the front lookup).
   Negligible at this scale; worth a one-line comment that it is id-keyed and
   indexed.

## Verified correct (unchanged, re-confirmed)

- Reminder pair atomicity + duplicate-id abort (`obligation.go:702-712`,
  `schedule.go:334`).
- `WallTime` civil-date validation (`schedule.go:60-77`).
- `processUpdate` non-terminal replay semantics (`telegram.go:254-299`).
- callback_query gate + strict per-action grammar + legal-in-state + owner
  binding + benign rejection (`:37-48,97-109`).
- Ephemeral/durable delivery split: calendar sends/edits and
  `answerCallbackQuery` are best-effort `a.call` chrome; the reminder and its
  confirmation are journaled; no unjournaled durable claim.

VERDICT: PASS

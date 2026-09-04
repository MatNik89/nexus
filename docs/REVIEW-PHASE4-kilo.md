# REVIEW-PHASE4 — T20 scheduler + T21 obligations (mandatory phase gate)

Scope: internal/schedule, internal/obligation, the journal `AppendBatch` refactor, cmd/nexus wiring.
`CGO_ENABLED=0 go vet ./...` clean; `CGO_ENABLED=0 go test -count=1 ./...` GREEN.

---

## HIGH — none

## MED

**1. [MED] `dueUTC`'s ONCE_FIRST is a hard-coded one-hour fold heuristic — non-one-hour DST zones
and historic offset shifts fire at the wrong instant.** `dueUTC` (schedule.go:69-82) derives the due
instant with `time.Date`, then probes `t.Add(-time.Hour)` and, if that still names the same wall
time, takes the earlier instant. Go's `time.Date` does not guarantee which side of a fold it returns,
and the correction is a fixed one hour. Concrete failure: a reminder in a 30-minute-DST zone (Lord
Howe) set for a wall time that occurs twice is fired at whichever side `time.Date` chose — the
30-minute-earlier occurrence is never considered because `t-1h` lands in a different wall minute, so
"ONCE_FIRST" silently degrades to "whichever". Historic non-DST offset shifts (present inside the
allowed 2000–2100 window) are the same class. The spring-gap forward-normalization and the
`time.Date` fold behavior are correct for the common one-hour case, but the invariant is
zone-agnostic in the contract and the implementation is not. No RED covers a 30-minute fold.

**2. [MED] `MarkAcked`'s checker grading is ceremonial — the evidence is synthesized and always
passes, while the real gate is the projection state.** `MarkAcked` (obligation.go:467-490) first
reads `st == StateDelivered`, then feeds the T12 checker a `DeliveredAndAcked` contract with evidence
built from `now.Add(-1)` / `now` (lines 477-481). That evidence is constructed by the same code that
grades it, so the verdict can only be `Pass` when `st == DELIVERED`; the checker is not grading the
durable `obligation.delivered`/`obligation.acked` events, it is re-stating the projection read. The
actual B5 gate — delivery receipt + ack for the SAME occurrence, legal transition — is correctly
enforced by the `reminderTable` default-reject fold and the unique occurrence index, so correctness
holds, but the E13 "checker grades real artifacts, never prose" posture is being dressed up with
synthetic timestamps. Fix: either drop the decorative Grade and rely on the projection state machine,
or feed the checker the REAL event timestamps (the `delivered` event's `EmittedAt`, the current ack
time).

## LOW

**3. [LOW] `FileNoteHandler` is a lock-free read-modify-write.** It reads `notes.txt`, appends, and
`atomicwrite.Write`s the whole file (obligation.go:336-343). Two concurrent `RunTask` calls for two
tasks both read the same base and the later write silently drops the earlier note. P0 runs tasks
sequentially, so this is latent, but the handler offers no mutual exclusion.

**4. [LOW] The C8 counter-file mirror can lag a crash.** `Sweep` writes the mirror only AFTER the
journal batch commits (schedule.go:347-351); a crash in between leaves the file stale until the next
fire. The DB counter is authoritative (`LastOccurrenceFired` reads `sched_meta`), so this is
observability drift, not a safety property.

**5. [LOW] `MarkTaskDone` verifies the file hash, then journals DONE with no re-check** —
a modification between the read and the append is not re-verified (obligation.go:533-554). The
postcondition held at grade time; a later edit is the user's own, so this is benign TOCTOU.

**6. [LOW] `reminderTable()` is rebuilt on every `Apply` step** (obligation.go:63-76, 213) — a static
table constructed per event. Correct (validated by construction), just wasteful.

## Verified sound

- **AppendBatch** (journal.go:516-546) prepares each member pre-tx, then `insertInTx` sequences AND
  chains the integrity hash across members (`prevHash` threaded, offset increments) inside ONE actor
  transaction; single `Append` delegates to a one-element batch. The schedule's B7 recipe
  `[occurrence_fired, run.created, run.admitted]` is therefore atomic and replay-consistent. ✓
- **COALESCE double-fire** is structurally impossible: the projection's `UPDATE … WHERE fired=0` +
  `RowsAffected != 1` abort the append on a replay/race (schedule.go:193-203); the single `Run`
  goroutine sweeps startup-then-tick sequentially. ✓
- **Ack-for-N correlation**: `step` looks up the obligation BY occurrence id and the default-reject
  table plus the unique `(occurrence_id)` index make an ack for N+1 (or an unknown occurrence) abort
  the append (obligation.go:194-220, 180-181). ✓
- **file_note checker != worker**: the handler only ATTESTs (path+hash); `MarkTaskDone` re-reads the
  real file and grades the hash independently (obligation.go:524-556) — this path is genuine, unlike
  MarkAcked. ✓
- **EXPIRED** is legal from DELIVERY_PENDING and DELIVERED only (table), and MarkExpired is
  occurrence-bound. ✓

---

## Verdict

The phase core is solid: AppendBatch is atomic and chain-correct, COALESCE cannot double-fire, ack
correlation and the file_note checker are real, and replay legality shares one default-reject table
with the live path. Two MED defects remain on locked semantics: the DST ONCE_FIRST is a one-hour-only
heuristic (wrong for 30-minute/other zones), and `MarkAcked`'s checker grading is synthetic rather
than evidence-based.

VERDICT: FAIL

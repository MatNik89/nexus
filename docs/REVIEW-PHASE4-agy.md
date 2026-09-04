# REVIEW-PHASE4 — Phase 4 Deep Review (T20 Scheduler + T21 Obligations)

**Target Ref:** `slice/p0-phase4` @ HEAD (`b298083` over `5d54cf3`)  
**Commits:** `33c1f67` (T20: durable one-shot scheduler + journal AppendBatch) + `b298083` (T21: ObligationStore 9.6-min + verified file_note task)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `internal/schedule/`, `internal/obligation/`, `internal/kernel/journal/` (`AppendBatch` refactor), `cmd/nexus/main.go`, `internal/preflight/doctor/`

---

## 1. Findings

### 1. [MED] Hardcoded 60-minute fold subtraction in `WallTime.dueUTC()` fails to select the first occurrence in fractional-hour DST timezones (`dst=ONCE_FIRST`).
- **File:Line:** [`internal/schedule/schedule.go:69-82`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L69-L82)
- **Concrete Failure Scenario:** `dueUTC()` resolves ambiguous autumn fold wall times by evaluating `earlier := t.Add(-time.Hour)` and calling `sameWall(earlier, w, loc)`. While standard European and North American zones shift by exactly 60 minutes, timezones such as `Australia/LHI` (Lord Howe Island) use a 30-minute daylight saving offset (UTC+10:30 to UTC+11:00). During an autumn transition in Lord Howe Island (e.g. 02:00 LHDT -> 01:30 LHST), an ambiguous reminder set for 01:45 occurs at 14:45 UTC (LHDT) and 15:15 UTC (LHST). Go's `time.Date` returns standard time (15:15 UTC). `t.Add(-time.Hour)` evaluates to 14:15 UTC (which is 01:15 LHDT, not 01:45). Consequently, `sameWall` returns `false`, and `dueUTC()` fails to select the earlier instant, violating `dst=ONCE_FIRST` for 30-minute DST zones.
- **Fix:** Either check fractional offsets dynamically (e.g. testing the zone's actual transition delta or testing 30m / 60m offsets), or document that P0 `dst=ONCE_FIRST` assumes 1-hour DST transitions with fractional DST support explicitly deferred to the full P2.6 scheduler engine.

---

### 2. [MED] `MarkAcked` synthesizes delivery evidence timestamps on the fly rather than validating persisted delivery receipts.
- **File:Line:** [`internal/obligation/obligation.go:474-489`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L474-L489)
- **Concrete Failure Scenario:** In `MarkAcked`, `checker.Grade` is called with synthesized in-memory `DeliveryEvidence{OccurrenceID: occ, DeliveredAt: now.Add(-1)}` and `AckEvidence{OccurrenceID: occ, AckAt: now}`. Although `st == StateDelivered` correctly verifies that the obligation previously transitioned to `DELIVERED` via the journal state machine, the actual delivery timestamp from the durable `EvDelivered` event is not retrieved or evaluated. The temporal invariant (`AckAt >= DeliveredAt`) is checked against an artificial `now.Add(-1)` offset rather than the real historical delivery time.
- **Fix:** Persist the delivery timestamp on the `obl_obligations` row (e.g. `delivered_at INTEGER`) during `EvDelivered`, and feed that actual persisted timestamp into `DeliveryEvidence.DeliveredAt`.

---

### 3. [LOW] Reminder tool lifecycle (`reminder_set`, `reminder_ack`) is not exercised in the daemon e2e composition test.
- **File:Line:** [`cmd/nexus/main_test.go:87-174`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L87-L174)
- **Concrete Failure Scenario:** `buildDaemon` wires `obligation.Specs()`, `obligation.Rules()`, and `obligation.Tools()` into the daemon and planner dependencies. However, `cmd/nexus/main_test.go` only exercises conversation and memory tools (`memory_remember` / `memory_recall`). While unit tests in `internal/obligation/obligation_test.go` exercise the manager and tools directly, there is no end-to-end spine test verifying `REPL → UDS → planner → PEP → reminder_set → schedule/obligation projection`.
- **Fix:** Add an e2e subtest in `main_test.go` verifying `reminder_set` through the production daemon spine.

---

### 4. [LOW] `Scheduler.Sweep` fires candidates sequentially across separate transactions, potentially deferring subsequent fires if an intermediate batch fails.
- **File:Line:** [`internal/schedule/schedule.go:310-346`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L310-L346)
- **Concrete Failure Scenario:** In `Scheduler.Sweep`, each due candidate fires via its own `j.AppendBatch(ctx, [occP, createdP, admittedP])`. If multiple reminders are overdue (e.g. on waking from system suspend) and an error occurs appending candidate $k$, `Sweep` returns immediately with an error. Candidates $k+1 \dots N$ remain unfired until the next scheduled sweep interval. While state remains crash-consistent and COALESCE guarantees they will eventually fire on a subsequent sweep, a single failing reminder can temporarily delay subsequent independent overdue reminders.
- **Fix:** Either continue the loop on candidate error while accumulating errors, or document sequential per-candidate batch firing as a design choice for incremental progress.

---

## 2. Standards & Spec Verification

### (a) AppendBatch Atomicity & Chain Integrity
- `appendBatch` prepares all envelopes before opening `tx *sql.Tx`.
- Sequences, envelopes, and hashes are computed sequentially in the transaction (`offset, hash = ev.JournalOffset+1, ev.IntegrityHash`), correctly linking `IntegrityPrevHash` across all batch members.
- `applySyncProjections` runs for each event within the same transaction.
- If any event or commit fails, `tx.Rollback()` rolls back the entire batch and all projection updates. Single `Append` delegates directly to `AppendBatch(ctx, []EnvelopeParams{p})`.
- Verified by [`internal/kernel/journal/journal_test.go:656-707`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal_test.go#L656-L707) (`TestAppendBatchAllOrNothing`).

### (b) DST Correctness & Overdue Grace
- Autumn fold in standard 1-hour zones fires at the first occurrence via `sameWall(earlier, w, loc)` (`TestZagrebAutumnFoldFiresOnce`).
- Spring gap normalizes forward to the next legal wall instant (`TestZagrebSpringGapPolicyApplied`).
- `overdueGrace` (60s) properly distinguishes on-time fires from overdue catch-ups (`TestRestartBeforeDueFiresOnce` vs `TestRestartAfterDueFiresOnceOverdue`).

### (c) COALESCE & Counter File
- Single-fire atomicity: `UPDATE sched_schedules SET fired=1 WHERE id=? AND fired=0` inside the journal append transaction prevents any double-fire across concurrent sweeps.
- `sched_meta.last_occurrence_fired` is updated synchronously inside the append transaction and mirrored via `atomicwrite` to `system/last_occurrence_fired` for doctor observability (`doctor.go:121-128`).
- Replay/rebuild deterministically reconstructs `sched_schedules` and `sched_meta` from the canonical event stream.

### (d) Obligation Lifecycle & B5 Invariants
- Reminder lifecycle strictly follows `SCHEDULED → DUE → DELIVERY_PENDING → DELIVERED → ACKED | EXPIRED` backed by `reminderTable()` default-reject machine (`TestIllegalTransitionsAbort`).
- Occurrence binding is unique per obligation (`ux_obl_occurrence` index).
- An ACK for occurrence $N$ can never close $N+1$ (`TestAckForWrongOccurrenceNeverCloses`).

### (e) Typed Task (`file_note`) & Postcondition Verification
- `file_note` handler appends note lines to `notes.txt` in `profileDir` via `atomicwrite.Write` and returns the expected SHA256 (`obligation.go:327-347`).
- Worker (`file_note-handler`) and Checker (`postcondition-verifier`) are cleanly separated.
- `MarkTaskDone` independently reads `notes.txt` from disk and verifies the SHA256 through `checker.Grade`; an unexecuted or tampered task claim is rejected with `Grade=FAIL` (`TestFileNoteTaskVerifiedPostcondition`).

### (f) RED Names Verification
- All T20 and T21 ledger RED names exist and are causal:
  - `restart-before-due → once`: `TestRestartBeforeDueFiresOnce` (`schedule_test.go:70`)
  - `restart-after-due → once with OVERDUE`: `TestRestartAfterDueFiresOnceOverdue` (`schedule_test.go:110`)
  - `Zagreb autumn fold → once`: `TestZagrebAutumnFoldFiresOnce` (`schedule_test.go:135`)
  - `spring gap → policy applied`: `TestZagrebSpringGapPolicyApplied` (`schedule_test.go:164`)
  - `suspend-over-due → catch-up on wake`: `TestSuspendOverDueCatchUpOnWake` (`schedule_test.go:186`)
  - `recipe atomicity`: `TestOccurrenceAndRunAdmissionAtomic` (`schedule_test.go:204`) & `TestAppendBatchAllOrNothing` (`journal_test.go:656`)
  - `ACK for occurrence N does not close N+1`: `TestAckForWrongOccurrenceNeverCloses` (`obligation_test.go:112`)
  - `MarkDone without evidence → rejected`: `TestFileNoteTaskVerifiedPostcondition` (`obligation_test.go:163`) & `TestIllegalTransitionsAbort` (`obligation_test.go:134`)
  - `file_note "done" claim with unchanged file → Grade=FAIL`: `TestFileNoteTaskVerifiedPostcondition` (`obligation_test.go:163`)

---

## 3. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages passed uncached (exit code 0).

---

VERDICT: FAIL

# REVIEW-PHASE4-R2 — Phase 4 Verification Round 2 (T20 Scheduler + T21 Obligations)

**Target Ref:** `slice/p0-phase4` @ HEAD (`f1d3889` over `b298083`)  
**Commit:** `f1d3889` (`fix(phase4-review): fold deep review r1`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** The fold diff `b298083..f1d3889` across `internal/schedule/`, `internal/obligation/`, `internal/kernel/checker/`, `internal/kernel/journal/`, and `cmd/nexus/`.

---

## 1. Verification of Round 1 Findings

| Finding ID | Origin | Severity | Description | Status | Evidence |
|---|---|---|---|---|---|
| **Agy #1** | agy | MED | Hardcoded 60m fold in `dueUTC()` fails for non-1h DST zones (Lord Howe). | **[OK]** | Replaced with backward scan up to 3h in 1m steps in [`internal/schedule/schedule.go:90-100`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L90-L100). Tested in `TestLordHoweHalfHourFoldFiresFirst` ([`schedule_test.go:297-326`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L297-L326)). |
| **Agy #2** | agy | MED | `MarkAcked` synthesizes delivery evidence timestamps on the fly. | **[OK]** | `delivered_at` persisted in `EvDelivered` from `EmittedAt` ([`obligation.go:301`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L301)), read via `occurrenceState` and fed to `checker.Grade` ([`obligation.go:578-595`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L578-L595)). Tested in `TestAckGradesPersistedDeliveryReceipt` ([`obligation_test.go:297-315`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L297-L315)). |
| **Agy #3** | agy | LOW | Reminder tool lifecycle not exercised in e2e daemon composition test. | **[OK]** | Added `TestReminderSpineAcrossRestart` in [`cmd/nexus/main_test.go:181-281`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L181-L281), testing full REPL → provider tool call → PEP → batch append → restart → decorated fire → deliver → ack. |
| **Agy #4** | agy | LOW | `Scheduler.Sweep` sequential candidate batches and counter lag. | **[OK]** | Handled benign concurrent fire with `ALREADY_FIRED` skip ([`schedule.go:400-402`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L400-L402)); counter reconciled from authoritative projection on every sweep ([`schedule.go:411-421`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L411-L421)). |
| **Codex #1** | codex | HIGH | Crash window between scheduler fire and obligation admission. | **[OK]** | `FireDecorator` allows obligation manager to join the same atomic transaction batch in `Scheduler.Sweep` ([`schedule.go:391-397`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L391-L397), [`obligation.go:504-514`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L504-L514), [`cmd/nexus/main.go:219`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L219)). |
| **Codex #2** | codex | HIGH | B5 grading ceremonial with manufactured timestamps. | **[OK]** | Folded via persisted `delivered_at` (see Agy #2). |
| **Codex #3** | codex | HIGH | `RunTask` bypasses `EffectPath`, PEP, and S7 grant governance. | **[OK]** | `RunTask` routes execution through `EffectPath` using registered in-process tool `task_file_note` with S7 authority grant ([`obligation.go:665-690`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L665-L690)). |
| **Codex #4** | codex | HIGH | `file_note` task tools unreachable from assistant. | **[OK]** | Added `task_create`, `task_file_note`, `task_done` to `obligation.Specs()`, `Rules()`, and `Tools()` ([`internal/obligation/tools.go:27-36,45-48,101-154`](file:///home/matej/HARNESS/nexus/internal/obligation/tools.go#L27-L36)). |
| **Codex #5** | codex | HIGH | Unreconciled task execution / duplicate note append on crash. | **[OK]** | Intent journaled via `EvTaskIntent` before effect; crashed intent reconciles against `notes.txt` before retry and attests `Reconciled: true` without duplicating lines ([`obligation.go:631-643`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L631-L643)). Tested in `TestTaskIntentAndReconcileNeverDuplicates` ([`obligation_test.go:321-380`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L321-L380)). |
| **Codex #6** | codex | HIGH | Whole-file hash TOCTOU and concurrent append loss. | **[OK]** | Converted postcondition to immutable marker line verified by `checker.FileContainsLine` ([`checker.go:25-28,273-288`](file:///home/matej/HARNESS/nexus/internal/kernel/checker/checker.go#L25-L28)), and serialized appends via `fileNoteMu` ([`obligation.go:382-409`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L382-L409)). |
| **Codex #7** | codex | HIGH | Persisted UTC due instant not stored (tzdata drift / non-deterministic replay). | **[OK]** | `DueUTC` resolved once at admission and persisted in `createdPayload` and `sched_schedules.due_utc` ([`schedule.go:126,181,210,316`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L126)). Replay and evaluation read stored instant. |
| **Codex #8** | codex | MED | `ONCE_FIRST` 1h heuristic fails on non-1h folds. | **[OK]** | Folded via scan in `dueUTC()` (see Agy #1). |
| **Codex #9** | codex | MED | Invalid civil dates (e.g. 2026-02-30) silently normalize. | **[OK]** | Added civil roundtrip check in `WallTime.validate()` ([`schedule.go:70-73`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L70-L73)). Tested in `TestImpossibleCivilDateRefused` ([`schedule_test.go:328-343`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L328-L343)). |
| **Codex #10** | codex | MED | Arbitrary 60s `overdueGrace` tolerance without policy foundation. | **[OK]** | Replaced with `DefaultSweepInterval` (the mechanism's own tick) ([`schedule.go:45,370`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L45)). Tested with exact boundary tests in `TestOverdueBoundaryIsSweepInterval` ([`schedule_test.go:273-295`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L273-L295)). |
| **Codex #11** | codex | MED | C8 counter file mirror can become permanently stale. | **[OK]** | `mirrorCounter` called on every sweep, reconciling from `sched_meta` ([`schedule.go:411-421`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L411-L421)). |
| **Codex #12** | codex | MED | Journal DSN lacks immediate transaction locking. | **[OK]** | Added `_txlock=immediate` to SQLite DSN in [`internal/kernel/journal/journal.go:158`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L158). |
| **Codex #13** | codex | MED | Journal validator accepts arbitrary obligation kinds. | **[OK]** | Sealed kind whitelist in `obligation.Events(taskKinds...)` rejecting unknown kinds at journal append ([`obligation.go:131-158`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L131-L158)). Tested in `TestUnknownKindEventRejectedAtAppend` ([`obligation_test.go:280-292`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L280-L292)). |
| **Codex #14** | codex | MED | Non-causal boundary REDs in scheduler tests. | **[OK]** | Added `TestPersistedDueSurvivesRestartBetweenFoldOccurrences` ([`schedule_test.go:345-371`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L345-L371)) and `TestConcurrentSweepsFireOnce` ([`schedule_test.go:373-421`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L373-L421)). |
| **Codex #15** | codex | MED | Missing e2e production composition test and negative controls. | **[OK]** | Added `TestReminderSpineAcrossRestart` (see Agy #3) and comprehensive tool governance unit tests in `obligation_test.go`. |
| **Kilo #1** | kilo | MED | `dueUTC` hardcoded 1h fold heuristic. | **[OK]** | Same as Agy #1 / Codex #8. |
| **Kilo #2** | kilo | MED | Ceremonial `MarkAcked` grading. | **[OK]** | Same as Agy #2 / Codex #2. |
| **Kilo #3** | kilo | LOW | `FileNoteHandler` lock-free read-modify-write. | **[OK]** | Serialized via `fileNoteMu` ([`obligation.go:382,399`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L382)). |
| **Kilo #4** | kilo | LOW | C8 counter mirror lag. | **[OK]** | Reconciled every sweep (see Codex #11). |
| **Kilo #5** | kilo | LOW | `MarkTaskDone` whole-file hash TOCTOU. | **[OK]** | Converted to marker-line postcondition (see Codex #6). |
| **Kilo #6** | kilo | LOW | Rebuilding `reminderTable` on every event. | **[OK]** | Static table instantiation in `obligation.go:73-86`. |

---

## 2. Key Judgments & Structural Verification

### (a) FireDecorator & Atomic Fire↔Admission Batch
`schedule.FireDecorator` ([`schedule.go:243,391-397`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L243)) allows external owners (the obligation manager) to append envelopes to the fire batch. In `buildDaemon`, `sched.SetFireDecorator(oblManager.FireParams)` wires the obligation transitions (`EvDue` and `EvDeliveryPending`).
The entire batch (`[EvOccurrenceFired, EvRunCreated, EvRunAdmitted, EvDue, EvDeliveryPending]`) is committed in a single atomic transaction via `j.AppendBatch`.
If any event validation, projection transition, or DB write fails, the entire transaction rolls back cleanly; `fired=0` remains in `sched_schedules`, ensuring zero crash window between fire and admission.

### (b) Evidence-Based `delivered_at` Grading
The delivery timestamp is durably recorded from `ev.Envelope.EmittedAt` during `EvDelivered` projection apply ([`obligation.go:301`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L301)). `MarkAcked` retrieves the persisted timestamp via `occurrenceState` and passes it to `checker.Grade`. An un-delivered occurrence has a zero delivery timestamp and fails closed. The checker validates separation (`Producer != Worker`), contract binding, and temporal causality (`AckAt >= DeliveredAt`).

### (c) Governed Task Execution (`task_file_note`)
Both interactive model tool calls and programmatic `RunTask` dispatches route through the sealed `EffectPath` ([`obligation.go:665-690`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L665-L690)), ensuring:
1. S6.0 PEP evaluation (`DecisionAllow` for execution).
2. S7 grant issuance and execution attempt tracking (`AttemptGrant`).
3. Pre-execution intent persistence (`EvTaskIntent`).
4. Reconcile on crash: if an intent exists but attestation was lost, `executeGoverned` checks for the existence of the deterministic marker line `[<id>] <note>` and attests `Reconciled: true` without duplicating the file append ([`obligation.go:631-643`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L631-L643)).

### (d) Immutable Marker-Line Postcondition
Replacing whole-file hashing with `FileContainsLine` ensures that subsequent legitimate appends to `notes.txt` do not invalidate or prevent the completion of previously executed tasks. The checker independently reads `notes.txt` to verify line presence, maintaining strict `Checker != Worker` separation.

### (e) DST Fold Scanning & Civil Date Roundtrip
- `dueUTC()` scans 3 hours backwards in 1-minute steps to find the earliest matching UTC instant (`ONCE_FIRST`), successfully handling non-1h transitions such as Lord Howe Island's 30-minute DST fold.
- `WallTime.validate()` roundtrips local date construction to reject non-existent civil dates like `2026-02-30` while permitting clock shifts on DST gap boundaries.
- Overdue policy aligns exactly with `DefaultSweepInterval` (30s), verified by boundary tests.

### (f) Concurrency & Immediate Locking
The SQLite journal DSN includes `_txlock=immediate`, ensuring transactions acquire write locks immediately upon `BeginTx`, preventing deadlocks and race conditions across concurrent sweeps and projection queries.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **`notesDir` package-level atomic storage in `obligation.go`:**
   - **File:Line:** [`internal/obligation/obligation.go:694-704`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L694-L704)
   - **Evidence:** `var notesDir atomic.Value` is a global package variable set by `SetNotesDir(dir)`. While safe for single-profile P0 operation, if multiple `Manager` instances with distinct profiles were run concurrently in the same process, they would overwrite each other's notes directory during crash reconciliation.

2. **Verifier read $\to$ `EvTaskDone` append window:**
   - **File:Line:** [`internal/obligation/obligation.go:744-765`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L744-L765)
   - **Evidence:** In `MarkTaskDone`, `fileContainsLine` re-reads the filesystem, passes evidence to `checker.Grade`, and subsequently appends `EvTaskDone`. An external deletion or truncation of `notes.txt` occurring precisely between the filesystem read and the journal append would result in `EvTaskDone` committing for content removed just prior to commit. (This is a declared topknot ceiling: replay determinism forbids I/O within SQLite projection transactions).

3. **Deferred D-Bus sleep/wake signal hook:**
   - **File:Line:** [`internal/schedule/schedule.go:13-16`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L13-L16)
   - **Evidence:** Periodic sweeping relies on `time.NewTicker(30s)` without listening to OS sleep/resume signals (e.g., `org.freedesktop.login1.Manager.PrepareForSleep`). Upon system resume, overdue reminders will wait up to 30 seconds for the next ticker sweep rather than firing instantly upon wake. (Declared topknot ceiling in T20).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages with tests passed uncached (exit code 0).
- `git ls-files cmd/nexus/main.go`: Verified tracked and present.
- Clean composition root test `TestReminderSpineAcrossRestart` verifies the full production lifecycle end-to-end.

---

VERDICT: PASS

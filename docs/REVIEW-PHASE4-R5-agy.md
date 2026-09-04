# REVIEW-PHASE4-R5 — Phase 4 Verification Round 5 (FINAL NARROW)

**Target Ref:** `slice/p0-phase4` @ HEAD (`f09fe31` over `ee93c46`)  
**Commit:** `f09fe31` (`fix(phase4-r4): fold verification round 4`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Scoped fold diff `ee93c46..f09fe31` across `internal/obligation/`, `internal/schedule/`, `internal/preflight/doctor/`, `internal/kernel/journal/`, and `cmd/nexus/`.

---

## 1. Verification of Round 4 Codex Findings

| Finding ID | Severity | Description | Status | Evidence |
|---|---|---|---|---|
| **Codex #3 / R4** | HIGH | Parallel execution RED not causal (clock timestamp collision masked in-flight lease). | **[OK]** | `TestParallelExecutionSingleEffect` ([`obligation_test.go:480-530`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L480-L530)) uses a blocking handler holding executor A inside the claim→effect window while B enters with a distinct sequence-incremented operation ID; B is refused by the in-flight lease (`"in progress"`). |
| **Codex #5 / R4** | HIGH | Forged attestation & DONE chain could bypass verification using `/tmp/notes.txt` and constant verifier string. | **[OK]** | `task_done` requires manager's process-local `DoneCapability` at the journal boundary ([`obligation.go:145-164,242-251`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L145-L164)); attacker-chosen path removed from `executedPayload` (derived from immutable `notesDir`). Tested in `TestForgedDoneChainDiesAtCapability` ([`obligation_test.go:560-588`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L560-L588)). |
| **Codex #6 / R4** | HIGH | Migration errors untyped (`strings.Contains`); legacy v1 delivered/acked events got generic errors. | **[OK]** | Sentinel `ErrMigrationRequired` defined in `schedule` and `obligation` packages ([`schedule.go:251`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L251), [`obligation.go:232`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L232)); all legacy shapes (unresolved due instant, legacy delivery, legacy ack, legacy attestation, legacy done) wrap `ErrMigrationRequired`. Tested with `errors.Is` in `TestV1ScheduleEventRequiresMigration` ([`schedule_test.go:453-457`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L453-L457)). |
| **Codex #7 / R4** | MED | Scheduler health failed open on `EACCES`/unreadable state; missing doctor REDs. | **[OK]** | `doctor.go` maps unreadable health state (`EACCES`/`EISDIR`) and malformed counters to `StatusOff` ([`doctor.go:132-154`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor.go#L132-L154)); `runDaemon` mirrors health immediately and on ticks ([`cmd/nexus/main.go:124-150`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L124-L150)). Tested in `TestSchedulerObservabilityFailClosed` ([`doctor_test.go:130-168`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor_test.go#L130-L168)). |
| **Codex #8 / R4** | HIGH | SIGKILL test non-causal at recipe boundary (barrier written after commit). | **[OK]** | Added test-gated seam in `journal.appendBatch` (`testing.Testing() && NEXUS_TEST_KILL_MID_BATCH=1`) delivering `SIGKILL` after envelope 1 insert before commit ([`journal.go:540-545`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L540-L545)); `TestSweepKilledMidBatchLeavesNothing` ([`schedule_test.go:590-645`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L590-L645)) proves zero events committed and clean recovery. |

---

## 2. Key Seam & Architectural Judgments

### (a) DONE Admission Capability & Anti-Forgery Boundary
In P0, cryptographic asymmetric signatures over artifacts are deferred to full multi-agent/remote architectures. Within single-binary, single-user P0:
- `NewDoneCapability()` generates a process-local 128-bit cryptographic random token at daemon startup held exclusively by the `Manager` that runs the postcondition verifier.
- `Events(reg, cap)` binds this capability to the `EvTaskDone` validator. Any other in-process actor attempting to submit a forged `EvTaskDone` is rejected at the journal admission boundary.
- `executedPayload` no longer accepts an external `ExpectedPath` field; the canonical path is derived internally from the profile-bound `notesDir`.
- During restart/replay, `Events(reg, "")` permits existing committed events while live admission requires the capability.

### (b) Parallel Execution Lease & RED Causality
`Manager.RunTask` generates sequence-numbered unique operation IDs (`fmt.Sprintf("task-%s-%d-%d", id, now, seq.Add(1))`), eliminating S7 authority ID collisions. In `TestParallelExecutionSingleEffect`, executor A enters a blocking handler while holding the `inflight` task lease; executor B attempts execution and is immediately rejected with `"obligation: task task-par is already in progress in this process"`. Releasing A completes the execution with exactly one physical append. Deleting the in-flight lease immediately breaks this test.

### (c) Typed `ErrMigrationRequired` Error Contract
Both `schedule.ErrMigrationRequired` and `obligation.ErrMigrationRequired` are exported sentinel errors. Projection `Apply` wraps these sentinels across:
1. `schedule.EvScheduleCreated` with `due_utc == 0`.
2. `obligation.EvDelivered` lacking `Producer` or `ReceiptID`.
3. `obligation.EvAcked` lacking `Source`.
4. `obligation.EvTaskExecuted` lacking `OperationID` or `MarkerLine`.
5. `obligation.EvTaskDone` lacking `MarkerLine` or `Verifier`.
Replay failures on legacy shapes are test-verified via `errors.Is(err, ErrMigrationRequired)`.

### (d) Fail-Closed Observability & Doctor Integrity
`doctor.go` treats missing files (`os.IsNotExist`) as clean pre-fire state, but strictly treats any unreadable state (`EACCES`, directory in place of file, I/O errors) or non-empty health errors as `StatusOff`. `runDaemon` mirrors `Scheduler.Health()` immediately on startup and across sweeps, ensuring mirror failures are logged to stderr and surfaced through `system/scheduler_health`.

### (e) Mid-Batch SIGKILL Seam Safety & Causality
The mid-batch kill hook in `journal.appendBatch` is dual-gated:
1. `testing.Testing()`: compiled-in Go runtime check that evaluates to `false` in production binaries built via `go build cmd/nexus`.
2. `os.Getenv("NEXUS_TEST_KILL_MID_BATCH") == "1"`: only set in `TestSweepKilledMidBatchLeavesNothing`.
In production, the seam is completely inert. In test execution, it delivers `SIGKILL` directly between the first envelope insert and transaction commit, proving that partial recipe batches are rolled back by SQLite WAL recovery without leaving orphaned events.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **Process-local capability scope vs out-of-process isolation:**
   - **File:Line:** [`internal/obligation/obligation.go:145-156`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L145-L156)
   - **Evidence:** `DoneCapability` is a process-local random token gating live admission in the single daemon process. If multiple independent processes opened the SQLite file simultaneously without daemon mediation, they would not share the capability token. (Mitigated by SQLite `_txlock=immediate` / single-daemon ownership).

2. **Single-file append contention on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:497-507`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L497-L507)
   - **Evidence:** `FileNoteHandler` synchronizes in-process appends via `fileNoteMu sync.Mutex` and writes via `atomicwrite.Write`. All tasks within a profile share `notes.txt`. While marker lines are immutable and postcondition checks use line presence, heavy concurrent task throughput relies on serialized atomic file replacements.

3. **Verifier-read $\to$ `EvTaskDone` append window:**
   - **File:Line:** [`internal/obligation/obligation.go:965-985`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L965-L985)
   - **Evidence:** In `MarkTaskDone`, `fileContainsLine` checks the filesystem, grades it with `checker.Grade`, and appends `EvTaskDone`. External file truncation between the read and the append is tolerated as a declared topknot ceiling (replay determinism prohibits filesystem I/O inside SQLite projection transactions).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages with tests passed uncached (exit code 0).
- `TestSweepKilledMidBatchLeavesNothing`: Verified passing (mid-batch SIGKILL leaves zero partial events).
- `TestParallelExecutionSingleEffect`: Verified passing (causal barrier proof of in-flight task lease).
- `TestForgedDoneChainDiesAtCapability`: Verified passing (forged DONE rejected at capability gate).
- `TestSchedulerObservabilityFailClosed`: Verified passing (doctor fails closed on unreadable/corrupt mirrors).
- `git ls-files cmd/nexus/main.go`: Verified tracked and present.

---

VERDICT: PASS

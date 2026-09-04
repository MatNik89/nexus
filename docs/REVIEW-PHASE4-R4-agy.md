# REVIEW-PHASE4-R4 — Phase 4 Verification Round 4 (T20 Scheduler + T21 Obligations)

**Target Ref:** `slice/p0-phase4` @ HEAD (`ee93c46` over `14f0b65`)  
**Commit:** `ee93c46` (`fix(phase4-r3): fold verification round 3`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** The fold diff since the r2-fold commit (`14f0b65..ee93c46`) across `internal/schedule/`, `internal/obligation/`, `internal/preflight/doctor/`, and `cmd/nexus/`.

---

## 1. Verification of Round 3 Codex & Kilo Findings

| Finding ID | Severity | Description | Status | Evidence |
|---|---|---|---|---|
| **Codex #1 / R3** | HIGH | `AckGesture` not durable in `EvAcked` canonical payload; projection admitted gesture-less acks. | **[OK]** | `ackedPayload` carries `Source`; `Events(reg)` validator and `Projection.Apply` enforce non-empty gesture source; `acked_by` persisted in `obl_obligations` ([`obligation.go:108-111,177-185,344-352`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L108-L111)). Tested in `TestDeliveryReceiptAndGestureRequired` ([`obligation_test.go:497-512`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L497-L512)). |
| **Codex #2 / R3** | HIGH | Intent CAS did not prevent concurrent in-process executions during claim→effect window. | **[OK]** | Live executor lease via in-process `taskLock(id)` per-task mutex ([`obligation.go:768-787`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L768-L787)) synchronizes parallel executions within the single-writer daemon process. Subsequent callers observe the committed attestation and exit cleanly. Tested in `TestParallelExecutionsSerializeAndRunOnce` ([`obligation_test.go:446-474`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L446-L474)). |
| **Codex #4 / R3** | MED | Observability: `Health()` was unconsumed in daemon; doctor missed write-failure scenario; missing REDs. | **[OK]** | `mirrorCounter` errors surface from `Sweep` while keeping fires durable ([`schedule.go:420-435`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L420-L435)); `runDaemon` mirrors `Scheduler.Health()` to `system/scheduler_health` and stderr ([`cmd/nexus/main.go:124-137`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L124-L137)); doctor flags non-empty health file as `StatusOff` ([`doctor.go:145-154`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor.go#L145-L154)). Tested in `TestMirrorFailureSurfaces` ([`schedule_test.go:564-585`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L564-L585)). |
| **Codex #5 / R3** | MED | Per-kind schema validator not enforced at canonical journal admission boundary. | **[OK]** | `Events(reg *Registry)` receives registry and validates `k.ValidateParams(p.TaskParams)` directly inside `EvCreated` validator ([`obligation.go:139-174`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L139-L174)). Tested in `TestEventsSealsJournalBoundary` ([`obligation_test.go:476-495`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L476-L495)). |
| **Codex #6 / R3** | MED | SIGKILL test non-causal (arbitrary sleep, unverified kill, vacuous oracle). | **[OK]** | Parent waits on child first-fire `ready` barrier file before sending `SIGKILL`, asserts non-zero wait exit error, and verifies non-empty pre/post workload with `sawWork` assertion across 6 iterations ([`schedule_test.go:465-530`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L465-L530)). |
| **Codex #9 / R3** | MED | Canonical admission accepted unvalidated task IDs and multi-line notes. | **[OK]** | `EvCreated` validator verifies `taskIDOK(p.ID)` (`^[A-Za-z0-9_-]{1,64}$`) and runs `ValidateFileNoteParams` prohibiting newlines ([`obligation.go:162,496-507`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L162)). |
| **Codex #10 / R3** | HIGH | Projection accepted forged attestation markers and unattested DONE transitions. | **[OK]** | Projection `Apply` on `EvTaskExecuted` recomputes expected marker from stored task params without filesystem I/O and rejects deviations ([`obligation.go:386-406`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L386-L406)); `EvTaskDone` carries `VerifiedMarker` and `Verifier` identity and requires exact match against stored marker ([`obligation.go:420-460`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L420-L460)). Tested in `TestProjectionEnforcesExecutionProtocol` ([`obligation_test.go:420-431`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L420-L431)). |
| **Codex #12 / Kilo** | HIGH | v1 schedule replay derived tzdata dynamically; obligation projection lacked version bump and migration check. | **[OK]** | `schedule.Projection.Apply` returns typed `MIGRATION_REQUIRED` on `p.DueUTC == 0` ([`schedule.go:211-216`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L211-L216)); legacy obligation events fail with `MIGRATION_REQUIRED`; `obligation.Projection.Version()` bumped to `3` ([`obligation.go:237`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L237)). Tested in `TestV1ScheduleEventRequiresMigration` ([`schedule_test.go:425-458`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule_test.go#L425-L458)). |

---

## 2. Key Seam & Architectural Judgments

### (a) Durable Ack Gesture at Canonical Admission
`EvAcked` now contains `{occurrence_id, source}`. Both the journal validator and the projection `Apply` require `p.Source != ""` and record it in `obl_obligations.acked_by`. Replay preserves the exact actor that acknowledged the occurrence, and forged acks without gesture source are rejected at the journal admission boundary.

### (b) Live-Executor Lease & Single-Writer Premise
The single-writer premise holds: the daemon exclusively owns the profile's journal database. Within the daemon process, `taskLock(id)` provides an in-flight per-task mutex across `executeGoverned`. Parallel execution attempts for the same task serialize cleanly: the first acquires the lock, claims the intent, performs the effect, and records the attestation; subsequent callers acquire the lock, observe the attested execution, and return without duplicating file modifications.

### (c) Journal-Boundary Sealing via `Events(reg)`
`Events(reg *Registry)` seals payload validation directly against the kind registry. `EvCreated` enforces slug format `[A-Za-z0-9_-]{1,64}` for IDs and runs kind-specific parameter validation (e.g., `ValidateFileNoteParams` checking JSON validity and single-line constraints). Forged or buggy in-process producers cannot admit malformed task params into the canonical stream.

### (d) Replay Purity & Projection Marker Recomputation
On `EvTaskExecuted`, `Projection.Apply` queries the stored `task_params` and pure-recomputes `expectedMarker(id, params)`. If the event's `MarkerLine` deviates from the canonical derivation, the append is aborted as a forged attestation. Because marker derivation is pure string formatting (`"[" + id + "] " + note`), no filesystem reads are performed during projection fold, preserving 100% replay purity and determinism.

### (e) Typed `MIGRATION_REQUIRED` and Obligation Version 3
Replay no longer evaluates dynamic timezone rules for legacy v1 events. A legacy `schedule.created` without `due_utc` fails `journal.Open` with `fmt.Errorf("schedule: MIGRATION_REQUIRED — ...")`. Legacy obligation events missing operation IDs or receipt identities fail similarly. The obligation projection version is bumped to `3`, ensuring clean DB reset and refold.

### (f) Observability & Health Transparency
`Scheduler.Sweep` surfaces counter mirror failures without abandoning durable occurrence fires. Errors are stored in `Scheduler.Health()`, monitored by `runDaemon`, mirrored to `system/scheduler_health`, and logged to `stderr`. `doctor.go` reports non-empty health files and malformed counters as `StatusOff`.

### (g) Causal SIGKILL Barrier Fuzzing
`TestSweepCrashConsistencyUnderSigkill` uses a filesystem barrier (`ready`) written by the child process after its first fire. The parent process waits for the barrier before delivering `SIGKILL`, verifies the non-zero process exit error, replays the survivor database, and asserts that every fired occurrence possesses its corresponding admitted run.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **Declared T22 transport authentication ceiling:**
   - **File:Line:** [`internal/obligation/obligation.go:678-680`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L678-L680)
   - **Evidence:** `DeliveryReceipt` and `AckGesture` accept declared string identifiers (`Producer`, `Source`) passed across Go API boundaries. While persisted and checked against `Worker` identities in `checker.Grade`, cryptographic transport signature verification is deferred to the T22 channel slice.

2. **In-process task lock scope:**
   - **File:Line:** [`internal/obligation/obligation.go:768-787`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L768-L787)
   - **Evidence:** `taskLock` synchronizes in-process executions of the same task within the single-writer daemon process. If multiple uncoordinated processes were granted concurrent write access to the same profile directory without daemon mediation, they would rely solely on the database transaction CAS without in-memory lock coordination.

3. **Verifier-read $\to$ `EvTaskDone` append window:**
   - **File:Line:** [`internal/obligation/obligation.go:947-968`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L947-L968)
   - **Evidence:** In `MarkTaskDone`, `fileContainsLine` checks the filesystem, grades it with `checker.Grade`, and appends `EvTaskDone`. External file truncation between the read and the append is tolerated as a declared ceiling (replay determinism prohibits filesystem I/O inside SQLite projection transactions).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages with tests passed uncached (exit code 0).
- `TestSweepCrashConsistencyUnderSigkill`: Verified passing (6 iterations, barrier-synchronized SIGKILL).
- `TestParallelExecutionsSerializeAndRunOnce`: Verified passing (4 parallel execution callers run exactly once).
- `TestV1ScheduleEventRequiresMigration`: Verified passing (typed `MIGRATION_REQUIRED`).
- `git ls-files cmd/nexus/main.go`: Verified tracked and present.

---

VERDICT: PASS

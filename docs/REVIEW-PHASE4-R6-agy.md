# REVIEW-PHASE4-R6 — Phase 4 Verification Round 6 (FINAL NARROW)

**Target Ref:** `slice/p0-phase4` @ HEAD (`1e4dc38` over `f09fe31`)  
**Commit:** `1e4dc38` (`fix(phase4-r5): fold verification round 5 (codex 2 unfolded)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Scoped fold diff `f09fe31..1e4dc38` across `internal/obligation/`, `internal/preflight/doctor/`, and `cmd/nexus/`.

---

## 1. Verification of Round 5 Codex Findings

| Finding ID | Severity | Description | Status | Evidence |
|---|---|---|---|---|
| **Codex #1 / R5** | HIGH | Bearer capability was serialized into canonical JSON (`donePayload.Cap`), enabling replay disclosure and forgery of unverified tasks. | **[OK]** | `Cap` field removed from `donePayload` ([`obligation.go:127-135`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L127-L135)); introduced out-of-band `DoneGate` ([`obligation.go:145-163`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L145-L163)) with unexported single-use `arm`/`consume`. `Events(reg, gate)` enforces `gate.consume` at journal admission. Tested via `TestReplayedDoneDisclosesNoCapability` ([`obligation_test.go:599-641`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L599-L641)). |
| **Codex #4 / R5** | MED | Health handling not fail-closed (heartbeat live + missing mirror reported OK; async startup race). | **[OK]** | `runDaemon` runs startup `Sweep` synchronously and mirrors result to disk prior to serving ([`cmd/nexus/main.go:127-141`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L127-L141)); `doctor.go` checks daemon heartbeat liveness and reports missing health mirror as `StatusOff` ([`doctor.go:157-172`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor.go#L157-L172)). Tested in `TestLiveDaemonMissingHealthMirrorIsOff` ([`doctor_test.go:173-188`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor_test.go#L173-L188)). |

---

## 2. Key Seam & Architectural Judgments

### (a) `DoneGate` Out-of-Band Admission & Anti-Forgery Boundary
- `DoneGate` manages process-local, single-use `(taskID, markerLine)` tickets guarded by `sync.Mutex` ([`obligation.go:145-163`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L145-L163)).
- The admission token is never serialized into JSON or persisted to the journal (`donePayload` only contains `ID`, `MarkerLine`, and `Verifier`). Replaying canonical bytes yields zero capability fields.
- `gate.arm` and `gate.consume` are unexported methods on `DoneGate`. The only path that arms a ticket is `Manager.MarkTaskDone` after `checker.Grade(checker.FileContainsLine)` verifies physical presence of the marker line in `notes.txt`.
- `gate.consume` deletes the ticket immediately upon consumption, preventing token reuse.
- Replay and journal catch-up do not require gate arming (`Apply` independently validates state transition and marker recomputation without reading the filesystem).

### (b) Synchronous Startup Health Mirror & Heartbeat Correlation
- In `cmd/nexus/main.go`, `b.sched.Sweep(ctx)` executes synchronously on the startup thread. The outcome is immediately written to `system/scheduler_health` via `atomicwrite.Write` before background loops and socket serving begin. This eliminates the asynchronous race window where startup sweep failures could be lost before the 5-second periodic ticker runs.
- `doctor.Run` correlates `system/scheduler_health` with `system/heartbeat`. If `scheduler_health` is missing (`ENOENT`) but `system/heartbeat` was updated within the last 60 seconds (daemon is live), `doctor` flags `StatusOff` ("daemon is live but its health mirror is missing"), ensuring fail-closed diagnostics.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **In-memory ticket lifetime on crash:**
   - **File:Line:** [`internal/obligation/obligation.go:145-163`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L145-L163)
   - **Evidence:** `DoneGate` tickets reside exclusively in an in-memory map. If the daemon process crashes between `m.gate.arm(id, markerLine)` and `m.j.Append(ctx, p)`, the ticket is lost. This behavior is fail-closed (the task remains in `StateRunning`/`Executed` state until `MarkTaskDone` is invoked again).

2. **Single-file append contention on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:497-507`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L497-L507)
   - **Evidence:** `FileNoteHandler` synchronizes all task file appends within a profile via `fileNoteMu sync.Mutex` and `atomicwrite.Write`. All tasks share `notes.txt`, serializing concurrent writes through atomic file replacement.

3. **Verifier-read to `EvTaskDone` append concurrency window:**
   - **File:Line:** [`internal/obligation/obligation.go:1000-1025`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1000-L1025)
   - **Evidence:** In `MarkTaskDone`, `checker.Grade(checker.FileContainsLine)` reads `notes.txt` prior to appending `EvTaskDone`. An external truncation of `notes.txt` occurring between the check and the append is tolerated under the declared topknot ceiling (replay determinism forbids non-deterministic filesystem I/O inside SQLite projection transactions).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 test packages passed uncached (exit code 0).
- `TestReplayedDoneDisclosesNoCapability`: Verified passing (no capability field in canonical replay; unarmed forged DONE rejected at admission).
- `TestLiveDaemonMissingHealthMirrorIsOff`: Verified passing (live heartbeat with missing health mirror reports `StatusOff`).
- Scoped diff verification: No new errors or regressions found in `f09fe31..1e4dc38`.

---

VERDICT: PASS

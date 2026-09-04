# REVIEW-PHASE4-R7 — Phase 4 Verification Round 7 (FINAL NARROW)

**Target Ref:** `slice/p0-phase4` @ HEAD (`d26ffa4` over `1e4dc38`)  
**Commit:** `d26ffa4` (`fix(phase4-r6): fold verification round 6 (codex 1 unfolded)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Scoped fold diff `1e4dc38..d26ffa4` across `internal/obligation/`.

---

## 1. Verification of Round 6 Codex Findings

| Finding ID | Severity | Description | Status | Evidence |
|---|---|---|---|---|
| **Codex #1 / R6** | HIGH | `DoneGate` ticket keyed only by `id\|marker` allowed in-process race during armed window and stranded authority on cancelled/failed appends. | **[OK]** | `DoneGate` tickets are now keyed by a 128-bit per-request unguessable nonce (`donePayload.Nonce`) with explicit `arm`, `disarm`, and `consume` methods ([`obligation.go:130-189,1030-1048`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L130-L189)). Tested in `TestArmedWindowRaceAndDisarm` ([`obligation_test.go:676-722`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L676-L722)). |

---

## 2. Key Seam & Architectural Judgments

### (a) Per-Request Nonce Binding & Armed-Window Race Prevention
- In `internal/obligation/obligation.go`, `DoneGate.arm(id, marker)` generates a cryptographically random 128-bit nonce via `crypto/rand.Read` and stores a single-use authorization ticket `tickets[nonce] = ticket{id: id, marker: marker}`.
- The nonce is transmitted inside `donePayload.Nonce`. The journal-boundary validator in `Events(reg, gate)` calls `gate.consume(p.Nonce, p.ID, p.MarkerLine)`, requiring the exact nonce to match the authorized `(id, marker)` pair.
- An in-process adversary racing during the armed window cannot forge admission because knowing the durable task ID and marker line provides zero knowledge of the 128-bit random nonce. Guessed or empty nonces fail closed.

### (b) Ticket Disarming & No Stranded Authority
- In `Manager.MarkTaskDone`, if `m.params` fails or `m.j.Append(ctx, p)` fails/cancels (such as when the caller's context is cancelled before or during the journal admission select), `m.gate.disarm(nonce)` is immediately called in cleanup paths to delete the ticket from `g.tickets`.
- This ensures authority is never left stranded in memory for subsequent opportunistic consumption.

### (c) Replayed Nonce Inertness
- When an `EvTaskDone` event is admitted, `gate.consume` immediately deletes the nonce from `g.tickets` (single-use).
- On daemon restart or replay, `NewDoneGate()` initializes a fresh, empty tickets map. Historical nonces stored in canonical journal records cannot be reused to authorize new events.
- Journal replay and catch-up do not evaluate live gate tickets (`Apply` verifies marker equality and task state transitions deterministically without filesystem reads or gate state).

### (d) RED Causality in `TestArmedWindowRaceAndDisarm`
- `TestArmedWindowRaceAndDisarm` ([`obligation_test.go:676-722`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L676-L722)) rigorously validates:
  1. Armed window defense: An attacker attempting an append with the correct `(id, marker)` but empty or guessed nonce is refused admission while a legitimate ticket is live.
  2. Single-use consumption: The legitimate append with the matching nonce succeeds exactly once.
  3. Disarm on failure: A cancelled context during `MarkTaskDone` disarms the ticket, preventing any subsequent consumption.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **Doctor 60s freshness window heuristic vs daemon 5s heartbeat interval:**
   - **File:Line:** [`internal/preflight/doctor/doctor.go:161-172`](file:///home/matej/HARNESS/nexus/internal/preflight/doctor/doctor.go#L161-L172)
   - **Evidence:** `doctor.Run` uses `time.Since(info.ModTime()) < time.Minute` to classify the heartbeat as live. This heuristic errs toward `StatusOff` for up to one minute after an ungraceful daemon termination if `scheduler_health` is missing, but does not create false-healthy states.

2. **Single-file append contention on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:497-507`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L497-L507)
   - **Evidence:** `FileNoteHandler` synchronizes all task file appends within a profile via `fileNoteMu sync.Mutex` and `atomicwrite.Write`. Concurrent task completions serialize disk I/O on the single `notes.txt` file.

3. **Verifier-read to `EvTaskDone` append concurrency window:**
   - **File:Line:** [`internal/obligation/obligation.go:1020-1045`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1020-L1045)
   - **Evidence:** In `MarkTaskDone`, `checker.Grade(checker.FileContainsLine)` reads `notes.txt` prior to arming and appending `EvTaskDone`. External file truncation between read and append is tolerated under the declared topknot ceiling (replay determinism forbids non-deterministic filesystem I/O inside SQLite projection transactions).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 test packages passed uncached (exit code 0).
- `TestArmedWindowRaceAndDisarm`: Verified passing (armed window race fails closed; cancelled request disarms ticket).
- `TestReplayedDoneDisclosesNoCapability`: Verified passing (canonical stream discloses no usable capability; post-replay forge fails).
- `TestLiveDaemonMissingHealthMirrorIsOff`: Verified passing (live heartbeat with missing health mirror reports `StatusOff`).
- Scoped diff verification: No new errors or regressions found in `1e4dc38..d26ffa4`.

---

VERDICT: PASS

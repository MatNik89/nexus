# REVIEW-PHASE4-R8 — Phase 4 Verification Round 8 (FINAL NARROW)

**Target Ref:** `slice/p0-phase4` @ HEAD (`c6dd2a6` over `d26ffa4`)  
**Commit:** `c6dd2a6` (`fix(phase4-r7): fold verification round 7 (codex 2 unfolded — proof quality)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Scoped fold diff `d26ffa4..c6dd2a6` across `internal/obligation/`.

---

## 1. Verification of Round 7 Codex Findings & Proof Quality Claims

| Finding / Claim | Severity | Description | Status | Evidence |
|---|---|---|---|---|
| **Disarm Causal RED** (Codex #3 / R7) | HIGH | Claimed test proving `disarm` revokes stranded authority on append failure. | **[UNFOLDED]** | `TestArmedWindowRaceAndDisarm` ([`obligation_test.go:718-730`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L718-L730)) calls `MarkTaskDone(cancelled, "task-x")` on an un-executed task; the call fails at line 996 before `gate.arm` is ever reached, and asserts an arbitrary guessed nonce (`deadbeef...`). `testPostArmFail` was added to `Manager` ([`obligation.go:655,1043-1048`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655)) but is never set in `obligation_test.go`. Removing all `gate.disarm` calls leaves the test suite green. |
| **Exact-Tuple Live Nonce RED** (Codex #4 / R7) | HIGH | Claimed test proving live nonce fails with wrong task ID or wrong marker line. | **[UNFOLDED]** | `TestArmedWindowRaceAndDisarm` ([`obligation_test.go:704-716`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L704-L716)) only tests empty/guessed nonces against the matching tuple, then tests the live nonce on the matching tuple. Deleting `tk.id != id \|\| tk.marker != marker` from `DoneGate.consume` ([`obligation.go:183`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L183)) leaves the entire test suite green. |
| **Harvested Nonce Replay Oracle** (Codex #4 / R7) | HIGH | Nonce harvested from canonical replay of task A is non-vacuous and rejected when attempted for task B. | **[OK]** | `TestReplayedDoneDisclosesNoCapability` ([`obligation_test.go:611-650`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L611-L650)) parses `harvested` nonce from canonical `EvTaskDone` replay, explicitly asserts `harvested != ""` (preventing vacuous oracle), and proves attempting task B with `Nonce: harvested` fails at admission. |
| **Test-Only Hook Safety** | LOW | Injection hook `testPostArmFail` does not leak into production paths. | **[OK]** | `testPostArmFail` is an unexported field on `Manager` ([`obligation.go:655`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655)), uninitialized in `NewManager`, and nil-checked before invocation. Production builds are unaffected. |

---

## 2. Key Seam & Architectural Judgments

### (a) Disarm Path Causality & Vacuous Test Analysis
- In `internal/obligation/obligation.go:655,1043-1048`, `testPostArmFail func() error` was introduced on `Manager` to allow injecting append failures after `gate.arm` mints a ticket.
- However, in `internal/obligation/obligation_test.go`, the test for disarming on cancellation was not updated:
  ```go
  if err := h.m.CreateTask(ctxT(), "task-x", "file_note", `{"note":"strand"}`); err != nil {
      t.Fatal(err)
  }
  cancelled, cancel := context.WithCancel(context.Background())
  cancel()
  _ = h.m.MarkTaskDone(cancelled, "task-x")
  ```
  Because `RunTask` was never called for `task-x`, `MarkTaskDone` exits at the pre-flight check (`row.State != StateRunning`) before reaching `m.gate.arm`. No ticket is ever created for `task-x`.
- Furthermore, the assertion attempts a raw append using a hardcoded dummy nonce `Nonce: "deadbeefdeadbeefdeadbeefdeadbeef"`, which would fail regardless of whether `disarm` ran.
- Removing all `m.gate.disarm(nonce)` calls from `obligation.go` does not fail `TestArmedWindowRaceAndDisarm`.

### (b) Exact Tuple-Binding Oracle Analysis
- In `internal/obligation/obligation.go:183`, `DoneGate.consume(nonce, id, marker)` enforces `tk.id == id && tk.marker == marker`.
- In `internal/obligation/obligation_test.go:704-716`, `TestArmedWindowRaceAndDisarm` tests:
  1. `guess: ""` on `("task-w", "[task-w] window")` (rejected).
  2. `guess: "00112233445566778899aabbccddeeff"` on `("task-w", "[task-w] window")` (rejected).
  3. `nonce` on `("task-w", "[task-w] window")` (accepted).
- The test never presents the live `nonce` with a wrong task ID (`"task-other"`) or a wrong marker line (`"[task-w] wrong"`). If the tuple checks `tk.id != id || tk.marker != marker` are deleted from `DoneGate.consume`, no test in the repository fails.

### (c) Harvested-Nonce Replay Proof Quality
- In `internal/obligation/obligation_test.go:611-650`, `TestReplayedDoneDisclosesNoCapability` actively parses the serialized `Nonce` from canonical `EvTaskDone` replay into `harvested`.
- The test explicitly verifies `harvested != ""` to ensure the oracle is non-vacuous, then proves that an adversary presenting `harvested` in a forged `EvTaskDone` for `task-b` is rejected at admission because the gate was consumed and not armed for `task-b`.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the folded codebase are:

1. **Unexercised `testPostArmFail` hook and vacuous disarm test:**
   - **File:Line:** [`internal/obligation/obligation.go:655`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655), [`internal/obligation/obligation_test.go:718-730`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L718-L730)
   - **Evidence:** `testPostArmFail` is declared on `Manager` but never configured in `obligation_test.go`. The test fails to exercise post-arm cancellation or verify ticket removal on append failure.

2. **Missing negative tuple checks in live-window test:**
   - **File:Line:** [`internal/obligation/obligation_test.go:704-716`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L704-L716)
   - **Evidence:** `TestArmedWindowRaceAndDisarm` does not test the live nonce against mismatched ID or marker, allowing regressions in `DoneGate.consume` tuple validation to go undetected.

3. **Single-file append contention on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:497-507`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L497-L507)
   - **Evidence:** `FileNoteHandler` synchronizes all task file appends within a profile via `fileNoteMu sync.Mutex` and `atomicwrite.Write`.

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 test packages passed uncached (exit code 0).
- Proof verification:
  - Harvested nonce replay oracle is verified causal and non-vacuous.
  - Disarm causal test is missing (unfolded).
  - Exact-tuple live nonce validation test is missing (unfolded).

---

VERDICT: FAIL

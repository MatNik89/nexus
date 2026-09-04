# REVIEW-PHASE4-R9 — Phase 4 Verification Round 9 (FINAL NARROW)

**Target Ref:** `slice/p0-phase4` @ HEAD  
**Reviewer:** `agy` (Antigravity)  
**Scope:** The fold diff in `internal/obligation/obligation_test.go` landing causal REDs for disarm on failure and exact tuple binding.

---

## 1. Verification of Proof-Quality RED Upgrades

| Test / Property | Severity | Description | Status | Evidence & Ablation Proof |
|---|---|---|---|---|
| **Causal Disarm RED** | HIGH | `TestArmedWindowRaceAndDisarm` proves `gate.disarm` leaves zero stranded tickets on post-arm failure. | **[OK]** | [`obligation_test.go:718-744`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L718-L744) sets `testPostArmFail` on an attested task, verifies `MarkTaskDone` fails, asserts `len(tickets) == 0`, and proves retry succeeds. **Ablation proof:** commenting out `m.gate.disarm(nonce)` in `MarkTaskDone` fails the test with `obligation_test.go:737: 1 ticket(s) stranded after a failed request (disarm missing)`. |
| **Causal Tuple-Binding RED** | HIGH | `TestLiveNonceTupleBinding` proves a live nonce is strictly bound to its `(id, marker)` pair. | **[OK]** | [`obligation_test.go:746-782`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L746-L782) submits the live nonce with wrong task ID (`task-t2`) and wrong marker (`[task-t1] forged-marker`), verifying both are refused without burning the ticket, and proves the exact tuple succeeds. **Ablation proof:** removing `tk.id != id \|\| tk.marker != marker` from `DoneGate.consume` fails the test with `obligation_test.go:767: live nonce authorized a DIFFERENT task`. |
| **Harvested Nonce Replay Oracle** | HIGH | Replayed nonce is non-vacuous and inert against subsequent tasks. | **[OK]** | [`obligation_test.go:611-650`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L611-L650) parses `harvested` nonce from canonical `EvTaskDone` replay, asserts non-empty, and proves attempting task B with `harvested` fails at journal admission. |
| **Production Safety & Hook Encapsulation** | LOW | `testPostArmFail` test hook does not affect production code paths. | **[OK]** | Unexported struct field on `Manager` ([`obligation.go:655`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655)), nil by default in `NewManager`, and nil-checked before execution. |

---

## 2. Key Seam & Architectural Judgments

### (a) Disarm Verification & Clean Revocation
- In `TestArmedWindowRaceAndDisarm`, task `task-x` is fully created and executed (`h.m.RunTask(ctxT(), "task-x")`).
- `h.m.testPostArmFail` injects a deterministic failure immediately after `gate.arm(id, markerLine)` mints a 128-bit nonce.
- `MarkTaskDone` catches the error, calls `m.gate.disarm(nonce)`, and returns the error.
- The test asserts `len(h.m.gate.tickets) == 0`, confirming that no residual ticket remains in the gate map.
- Clearing the hook and re-invoking `MarkTaskDone` mints a fresh nonce and successfully closes the task to `StateDone`.
- Controlled ablation confirmed that omitting `disarm` causes an immediate, causal failure.

### (b) Live Nonce Tuple-Binding Verification
- `TestLiveNonceTupleBinding` creates two attested tasks (`task-t1` and `task-t2`).
- A ticket is armed for `("task-t1", "[task-t1] tuple")`, returning a live `nonce`.
- The test attempts:
  1. Append `EvTaskDone` for `task-t2` with the live `nonce` $\to$ rejected by `gate.consume`.
  2. Append `EvTaskDone` for `task-t1` with a forged marker line $\to$ rejected by `gate.consume`.
  3. Verifies that non-matching attempts did not consume the ticket.
  4. Append `EvTaskDone` for `task-t1` with the matching marker line $\to$ accepted and committed.
- Controlled ablation confirmed that removing tuple validation from `consume` causes an immediate, causal failure.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline, the top 3 weakest points in the verified codebase are:

1. **Test hook coupling in production struct:**
   - **File:Line:** [`internal/obligation/obligation.go:655`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655)
   - **Evidence:** `Manager` contains `testPostArmFail func() error` specifically to test the disarm branch. While unexported and safe (nil in production), it is a white-box testing hook embedded in the core manager struct.

2. **Single-file append contention on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:497-507`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L497-L507)
   - **Evidence:** `FileNoteHandler` synchronizes all task file appends within a profile via `fileNoteMu sync.Mutex` and `atomicwrite.Write`. All tasks within a profile serialize disk writes on `notes.txt`.

3. **Verifier-read to `EvTaskDone` append concurrency window:**
   - **File:Line:** [`internal/obligation/obligation.go:1020-1045`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1020-L1045)
   - **Evidence:** In `MarkTaskDone`, `checker.Grade(checker.FileContainsLine)` reads `notes.txt` prior to arming and appending `EvTaskDone`. External file truncation between read and append is tolerated under the declared topknot ceiling (replay determinism forbids non-deterministic filesystem I/O inside SQLite projection transactions).

---

## 4. Test & Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 25 test packages passed uncached (exit code 0).
- Controlled ablations:
  - Disarm omission in `MarkTaskDone` $\to$ RED on `TestArmedWindowRaceAndDisarm` (`obligation_test.go:737`).
  - Tuple-check omission in `DoneGate.consume` $\to$ RED on `TestLiveNonceTupleBinding` (`obligation_test.go:767`).
  - Restored production code $\to$ GREEN on all tests.
- Scoped diff verification: No new errors or regressions found.

---

VERDICT: PASS

# REVIEW-PHASE4-R10 — Phase 4 Verification Round 10 (FINAL)

**Target Ref:** `slice/p0-phase4` @ HEAD (`6930ee1` + `8924383`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Commits `6930ee1` (landing causal REDs for disarm on failure and exact tuple binding) and `8924383` (wall-clock tool-call deadline fix avoiding frozen-clock landmine).

---

## 1. Verification of Target REDs AT HEAD & Causal Ablation

| Test / Property | Severity | Description | Status | Evidence & Ablation Proof |
|---|---|---|---|---|
| **Causal Disarm RED at HEAD** | HIGH | `TestArmedWindowRaceAndDisarm` is committed at HEAD and proves `gate.disarm` leaves zero stranded tickets on post-arm failure. | **[OK]** | [`obligation_test.go:718-743`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L718-L743) creates and attests `task-x`, assigns `h.m.testPostArmFail`, calls `MarkTaskDone`, asserts `stranded == 0`, and proves retry self-heals. **Ablation proof:** commenting out `m.gate.disarm(nonce)` calls in `MarkTaskDone` ([`obligation.go:1048,1054,1058`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1048-L1058)) causes `TestArmedWindowRaceAndDisarm` to fail RED with `obligation_test.go:737: 1 ticket(s) stranded after a failed request (disarm missing)`. |
| **Causal Tuple-Binding RED at HEAD** | HIGH | `TestLiveNonceTupleBinding` is committed at HEAD and proves a live nonce is strictly bound to its `(id, marker)` pair. | **[OK]** | [`obligation_test.go:749-782`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L749-L782) arms a ticket for `("task-t1", "[task-t1] tuple")`, tests wrong ID (`task-t2`) and wrong marker (`[task-t1] forged-marker`), verifies they are refused without burning the ticket, and proves the exact tuple succeeds. **Ablation proof:** deleting the tuple comparison `tk.id != id \|\| tk.marker != marker` from `DoneGate.consume` ([`obligation.go:186`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L186)) causes `TestLiveNonceTupleBinding` to fail RED with `obligation_test.go:767: live nonce authorized a DIFFERENT task`. |
| **Wall-Clock Deadline Fix** | HIGH | Tool-call deadline in `RunTask` uses real wall-clock time (`time.Now().Add(2 * time.Minute)`), preventing frozen-clock expiry in `effectpath` while event timestamps preserve the injected clock. | **[OK]** | [`obligation.go:963`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L963) mints `Deadline: time.Now().Add(2 * time.Minute)`. `effectpath.PEP.checkDeadline` ([`effectpath.go:335`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L335)) evaluates `!time.Now().Before(call.Deadline)` and sets Go runtime context timeouts against wall-clock time. Logical event timestamps remain strictly bound to `m.clock.Now()` ([`obligation.go:678,851`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L678)). No gaps introduced. |
| **Production Safety of Test Hook** | LOW | `testPostArmFail` is unexported, nil by default in `NewManager`, and never invoked in production. | **[OK]** | Field on unexported struct `Manager` ([`obligation.go:655`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655)), nil-checked before invocation ([`obligation.go:1046`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1046)). |

---

## 2. Standards and Spec Analysis for Commits `6930ee1` + `8924383`

### Standards Axis
- **smell baseline:** No code smells detected. The test helper hook is narrowly scoped to test the disarm branch. Variable and function names (`TestLiveNonceTupleBinding`, `testPostArmFail`, `DoneGate.consume`) clearly express intent.
- **Fail-closed discipline:** Disarm on any failure/cancellation before journal append ensures zero leaked credentials.
- **Single responsibility:** `DoneGate` handles in-memory ticket lifecycle (`arm`, `disarm`, `consume`), `Manager` handles workflow coordination.

### Spec Axis
- **Annex A / HARDQ B5 Invariants:** Task completion requires attestation via the S6.0/S7 governed `EffectPath`, independent checker grading against real state (`notes.txt`), and single-use `DoneGate` admission token.
- **Replay Determinism:** Event payloads (`EvTaskDone`) retain the consumed nonce for auditability; replay projections fold events without requiring gate re-validation.
- **Time Semantics:** Event `EmittedAt` and `AckAt` use the logical/injected `clockid.Clock` for deterministic simulation, whereas transport/execution deadlines evaluated by OS/runtime primitives (`time.Now()` / `context.WithDeadline`) use wall-clock time.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline:

1. **Dual clock domain split in `RunTask`:**
   - **File:Line:** [`internal/obligation/obligation.go:950,963`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L950-L963)
   - **Evidence:** `RunTask` derives `op` from `m.clock.Now().UnixNano()` (injected clock) while `call.Deadline` uses `time.Now().Add(...)` (wall-clock). While necessary because `effectpath` and `context.WithDeadline` check real time, mixed time sources in a single constructor requires careful awareness from future maintainers.

2. **Test hook on production `Manager` struct:**
   - **File:Line:** [`internal/obligation/obligation.go:655,1046-1051`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655)
   - **Evidence:** `testPostArmFail func() error` resides directly on `Manager` to simulate append failures between `gate.arm` and `m.j.Append`. While unexported and safe, it represents white-box testing instrumentation embedded in the production type.

3. **Mutex contention on `notes.txt`:**
   - **File:Line:** [`internal/obligation/obligation.go:537,554-562`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L537)
   - **Evidence:** `FileNoteHandler` synchronizes all file note appends using package-global `fileNoteMu sync.Mutex` and `atomicwrite.Write`. All concurrent task completions write through this shared file lock.

---

## 4. Test & Verification Evidence

- `git grep TestLiveNonceTupleBinding`: Confirmed present at HEAD in [`obligation_test.go:749`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L749).
- `git grep testPostArmFail`: Confirmed present at HEAD in [`obligation.go:655,1046`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L655) and assigned in [`obligation_test.go:728`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L728).
- **Controlled Ablation 1 (Disarm RED):** Commenting out `m.gate.disarm(nonce)` failed with `obligation_test.go:737: 1 ticket(s) stranded after a failed request (disarm missing)`.
- **Controlled Ablation 2 (Tuple Binding RED):** Deleting `tk.id != id || tk.marker != marker` from `consume` failed with `obligation_test.go:767: live nonce authorized a DIFFERENT task`.
- **Full Test Suite Gate:**
  - `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
  - `CGO_ENABLED=0 go test -count=1 ./...`: All 25 packages passed uncached (exit code 0).

---

VERDICT: PASS

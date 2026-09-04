# REVIEW-PHASE4-R11 — Phase 4 Verification Round 11 (FINAL NARROW)

**Target Ref:** `slice/p0-phase4` @ HEAD (`196c4ee`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Commit `196c4ee` diff only (`internal/obligation/obligation.go` and `internal/obligation/obligation_test.go`).

---

## 1. Verification of Scope Changes & Causal Ablations

| Target Property | Severity | Description | Status | Evidence & Ablation Proof |
|---|---|---|---|---|
| **Single Deferred Disarm Owner** | HIGH | `MarkTaskDone` routes ticket cleanup into a single `defer` after `gate.arm`, preventing stranded authority on any failure path. | **[OK]** | [`obligation.go:1049-1054`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1049-L1054) registers `defer func() { if !committed { m.gate.disarm(nonce) } }()`. **Ablation proof:** commenting out `m.gate.disarm(nonce)` inside the defer causes `TestArmedWindowRaceAndDisarm` to fail RED with `obligation_test.go:737: 1 ticket(s) stranded after a failed request (disarm missing)`. |
| **ID-Half Isolation Probe** | HIGH | `TestLiveNonceTupleBinding` adds `dWrongIDOnly` probe (`task-t2` with original `[task-t1] tuple` marker) to prove the ID check is independently load-bearing. | **[OK]** | [`obligation_test.go:775-779`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L775-L779) tests wrong ID with original marker. **Ablation proof:** deleting only `tk.id != id` from `DoneGate.consume` ([`obligation.go:186`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L186)) causes the probe to burn the ticket, making the subsequent legitimate `dOK` append fail RED with `obligation_test.go:791: exact tuple refused: journal append: payload invalid for event type: obligation: done without the verifying manager's armed admission ticket (fail closed)`. |
| **Branch Safety & Leak Resistance** | LOW | All post-arm return paths (`testPostArmFail`, `m.params`, `m.j.Append`, and potential panics) unwind through the deferred disarm. | **[OK]** | `arm` inserts into `g.tickets` only on success ([`obligation.go:168`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L168)). On success, `committed = true` is set, and `gate.consume` has already deleted the ticket upon journal admission ([`obligation.go:189,1067`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L189)). |

---

## 2. Standards and Spec Analysis for Commit `196c4ee`

### Standards Axis
- **smell baseline:** Clean Go idiom using standard `defer` pattern for resource lifecycle management. No code smells found.
- **Fail-closed discipline:** Disarm-by-default on any non-committed termination guarantees zero stranded authority across all error and panic paths.
- **Minimal machinery:** Eliminates triplicate `m.gate.disarm(nonce)` calls across multiple branches in favor of a single lexically scoped owner.

### Spec Axis
- **Annex A / HARDQ B5 Invariants:** Preserves the exact `(id, marker)` tuple binding for single-use `DoneGate` tickets.
- **Detector Quality:** The `dWrongIDOnly` probe eliminates the masking effect where the marker check alone could satisfy rejection, ensuring full independent coverage of the ID constraint.

---

## 3. Weakest Points Analysis

In accordance with mandatory review discipline:

1. **Indirect causal observable in `dWrongIDOnly`:**
   - **File:Line:** [`internal/obligation/obligation_test.go:775-791`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L775-L791)
   - **Evidence:** When `tk.id != id` is ablated, `dWrongIDOnly` fails projection (since `task-t2` does not match the marker line in the projection database), but consumes the in-memory ticket. The causal failure manifests downstream at `dOK` ([`obligation_test.go:791`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation_test.go#L791)). While completely deterministic and documented, the failure signal is indirect via ticket exhaustion.

2. **Single-owner construction coverage vs explicit append failure injection:**
   - **File:Line:** [`internal/obligation/obligation.go:1049-1067`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1049-L1067)
   - **Evidence:** The zero-ticket assertion is driven by the pre-append `testPostArmFail` hook. Coverage of the `m.j.Append` error branch is guaranteed structurally by Go's `defer` semantics rather than a distinct test hook injecting a failure into `Journal.Append`.

3. **Harmless redundancy in `!committed` guard:**
   - **File:Line:** [`internal/obligation/obligation.go:1049-1053`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1049-L1053)
   - **Evidence:** Because `DoneGate.consume` deletes the ticket upon successful journal validation, an unconditional `defer m.gate.disarm(nonce)` would execute a no-op map deletion. The `!committed` guard is explicit and clear, though technically redundant with `consume`'s single-use deletion.

---

## 4. Test & Verification Evidence

- **Controlled Ablation 1 (Deferred Disarm):** Neutralizing `m.gate.disarm(nonce)` inside `defer` $\to$ RED on `TestArmedWindowRaceAndDisarm` (`obligation_test.go:737`).
- **Controlled Ablation 2 (ID Check Isolation):** Deleting `tk.id != id` from `DoneGate.consume` $\to$ RED on `TestLiveNonceTupleBinding` (`obligation_test.go:791`).
- **Full Gate Verification:**
  - `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
  - `CGO_ENABLED=0 go test -count=1 ./...`: All 27 packages passed uncached (exit code 0).
- Diff analysis: No `[UNFOLDED]` gaps, no `[NEW-ERROR]` defects.

---

VERDICT: PASS

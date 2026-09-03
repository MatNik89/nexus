# REVIEW-PHASE1A-R5-agy.md — Phase 1A Verification Round 5 (Final Convergence)

**Target Ref:** `slice/p0-phase1` @ HEAD (`53bac1f`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Verification of the 5 Round-4 folds (`REVIEW-PHASE1A-R4-codex.md`) and final convergence audit on foundations (`journal.go`, `contracts.go`, `redact.go`, and test suites).

---

## 1. Executive Summary

Commit `53bac1f` achieves complete adversarial convergence on Phase 1A foundations (T04–T06). All 5 items from Round 4 are verified in code: (1) `Redactor.Redact` returns typed errors on over-budget JSON inputs, rejecting them at admission rather than silently failing open; (2) the writer lease is acquired before chain verification/recovery with exact-token rollback on error, and `Close()` surfaces lease release failures via `errors.Join`; (3) `Envelope.MarshalJSON` deletes known keys case-insensitively, preventing casing aliases (`TURN_ID`) from resurrecting cleared optionals; (4) the structural-secret `event_type` subtest registers the candidate event type, making rejection causal to `Touches`; (5) the admission barrier test asserts that `Append` cannot return while the actor is blocked post-cancel, proving true admission-definitive semantics.

Verification is clean across all 10 packages (62 passing tests).

---

## 2. Verification of the 5 Round-4 Folds

| Fold ID | Target Area | Description | Implementation in `53bac1f` | Status |
|---|---|---|---|---|
| **Fold 1** | Typed Redaction Error & Admission Rejection | `Redact` returns error; over-budget valid JSON rejected at admission (REDs). | `redact.go:25, 146-176` updates `Redactor` interface to return `([]byte, error)`. Over-budget (>1 MiB) or over-depth (>64) JSON returns typed error; `journal.go:356-359` halts admission and returns error. Tested in `redact_test.go:49-65` and `journal_test.go:610-627`. | `[OK]` |
| **Fold 2** | Lease-Before-Verify & Failure Cleanup | Lease acquired before recovery; token released on verify failure; `Close` error joined; injected delete RED. | `journal.go:220-267` acquires lease before `verifyChainFull()`; `journal.go:276-280` releases token if verify fails; `journal.go:534-549` uses `errors.Join(leaseErr, db.Close())`. Tested in `journal_test.go:593-608`. | `[OK]` |
| **Fold 3** | Case-Insensitive Key Deletion | Case-insensitive known-key deletion in `MarshalJSON`; case-alias RED. | `contracts.go:96-103` iterates preserved `orig` keys and deletes matches using `strings.EqualFold(k, known)`. Tested in `contracts_test.go:388-410` covering `TURN_ID`, `TOOL_CALL_ID`, `PARENT_EVENT_ID`, `TENANT_ID`, `IDEMPOTENCY_KEY`. | `[OK]` |
| **Fold 4** | Causal Structural Event-Type Preflight | Structural `event_type` secret subtest must causally exercise `Touches`. | `journal_test.go:193` registers `ev["x-"+secret] = nil` in the closed event set so admission reaches and tests `Touches` directly. | `[OK]` |
| **Fold 5** | Revert-Proof Admission Barrier | Assert no return while actor is blocked post-cancel; require committed result. | `journal_test.go:356-385` pauses actor after commit, cancels `ctx`, asserts via 300ms `select` that `Append` does not return early, releases pause, and requires committed result with `err == nil`. | `[OK]` |

---

## 3. Adversarial Analysis & Weakest Points Analysis

Under mandatory review discipline, the changed lines were inspected for candidate defects:

1. **`internal/security/redact/redact.go:151-153` (1 MiB JSON Payload Admission Boundary)**:
   JSON payloads exceeding 1 MiB are now rejected fail-closed at admission. For large text/code artifacts, callers must reference large objects via `ContentRef` in `ContextBlock` rather than passing multi-megabyte inline JSON payloads.
2. **`internal/kernel/contracts/contracts.go:96-103` (`strings.EqualFold` Case Folding)**:
   Case folding matches standard ASCII/Unicode case mappings (`TURN_ID` -> `turn_id`). Ensures that no casing variant of an omitempty field can evade key deletion.
3. **`internal/kernel/journal/journal.go:269-271` (`releaseLease` Seam on Verify Failure)**:
   `releaseLease` deletes the exact `pid:start:nonce` token if `verifyChainFull` fails during `Open`, leaving no stale locks on broken databases.

---

## 4. Verification Evidence

- `go vet ./internal/...`: Clean (exit code 0).
- `go test -v -count=1 ./internal/...`: All 62 tests passed across 10 packages:
  - `internal/buildcheck`: PASS (3 tests)
  - `internal/foundation/atomicwrite`: PASS (2 tests)
  - `internal/foundation/clockid`: PASS (3 tests)
  - `internal/kernel/assembler`: PASS (3 tests)
  - `internal/kernel/budget`: PASS (2 tests)
  - `internal/kernel/contracts`: PASS (19 tests incl. `TestEnvelopeMarshalJSONClearsCaseAliasedOptionals`)
  - `internal/kernel/journal`: PASS (19 tests incl. `TestCancelBetweenCommitAndReplyStillDefinitive`, `TestLeaseDeleteFailureSurfacesOnClose`, `TestOverBudgetPayloadRejected`)
  - `internal/preflight/doctor`: PASS (3 tests)
  - `internal/preflight/probe`: PASS (23 tests)
  - `internal/security/redact`: PASS (7 tests incl. `TestOverBudgetValidJSONRejected`, `TestOverDepthValidJSONRejected`)

---

VERDICT: PASS

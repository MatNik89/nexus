# Implementation Review (Round 5): Conversation Read-Model Projection (`convproj`)

**Scope**: Commit `f4889a8` on branch `slice/p0-convproj`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `d64140e`  

---

## Executive Summary

Commit `f4889a8` commits the missing `var testRecoveryVerifyHook func()` declaration and invocation in `internal/app/daemon/daemon.go`, resolving the Round 4 compilation failure.

A fresh clean-export build and full-suite test run succeed across all 39 packages with **0 failures**. All three detector-adequacy fixes are verified clean and RED-capable:
1. **`TestDuplicateEventSignalIsEventIDOnly` (RED-Capable)**: Forces a real SQLite `journal_offset` PRIMARY KEY conflict via a raw shadow row at offset 2, confirming that `eventIDExists(tx, id)` isolates `event_id` uniqueness from other SQLite constraint failures. Ablating `eventIDExists` to broad string matching fails RED.
2. **`malformed-succeed` Oracle Kind (RED-Capable)**: Included in the differential oracle's `kinds` pool (`{"final": 123}`). Both the projection and reference replay safely drop malformed JSON payloads across all 200 prefix sequences. Ablating the `json.Unmarshal` guard fails RED.
3. **`TestOrdinaryFailureSkipsRecovery` (RED-Capable)**: Verifies via `testRecoveryVerifyHook` that an ordinary first-run failure triggers **0** `VerifyChain` full-journal scans, while a redelivery collision executes exactly **1** scan. Ablating the `ErrDuplicateEvent` gate fails RED.
4. **Non-Vacuous Rebuild Oracle**: Asserts non-empty history (`len(before) > 0`) and validates parity against `referenceConversationHistory` across journal version resets.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Read-Model & Projection Architecture (`ARCHITECTURE-ESSENTIALS.md` E4, `internal/kernel/journal/projection.go:89-103`)**:
  - Synchronous projection lifecycle maintained inside `Journal.Append` transactions.
- **[COMPLIANT] Single Ownership & Event Sourcing (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Event journal remains sole source of truth; projections are derived and fully rebuildable.
- **[COMPLIANT] Cryptographic Integrity & Fail-Closed Recovery (`HARNESS-SPEC.md`, Annex A P0.3)**:
  - `VerifyChain()` integrity verification is preserved on redelivered collisions without imposing $O(\text{events})$ overhead on first-run turns.
- **[COMPLIANT] Lexical Guard Isolation (`internal/kernel/journal/projection.go:26-33, 196-211`)**:
  - `conv_turns` table name avoids collision with guarded canonical table keywords.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - All code, identifiers, comments, error strings, and documentation are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: Test hook is scoped to testing verification and does not introduce runtime overhead in production.

---

## Spec & Implementation Verification

### 1. Verification of Key Verification Areas & RED-Capability

| Area | Implementation & RED-Check | Evaluation |
| :--- | :--- | :--- |
| **`testRecoveryVerifyHook` Integration** | `daemon.go:72, 489-491`, `convproj_diff_test.go:258-279` | **Verified**: Declared and called in `recoveredTurnOutcome`. Compilation error resolved. `TestOrdinaryFailureSkipsRecovery` asserts 0 scans on ordinary failure and 1 scan on collision. |
| **Exact Event ID Sentinel** | `journal.go:511-518, 647-653`, `journal_test.go:747-779` | **Verified**: `TestDuplicateEventSignalIsEventIDOnly` forces `journal_offset` conflict and confirms `!errors.Is(serr, ErrDuplicateEvent)`. |
| **Malformed Payload Replay Equivalence** | `conv.go:107-109`, `convproj_diff_test.go:88, 111-114` | **Verified**: Differential oracle actively includes `"malformed-succeed"` and confirms 100% prefix equality against reference replay. |
| **Non-Vacuous Rebuild Oracle** | `convproj_diff_test.go:210-252` | **Verified**: Corrected turn IDs yield 5 completed turns; asserts non-empty history and pre/post equality against reference replay. |
| **Performance Gate** | `convproj_diff_test.go:151-193` | **Verified**: 1,118x speedup on 3,000 turns with a 1,500-turn incomplete tail. |

### 2. Empirical Verification

- **Full Workspace Test Suite**: `go test ./...` passed across all 39 packages (0 failures).
- **Differential Oracle**: `TestConvProjectionMatchesReferenceOnEveryPrefix` passed (200 randomized lifecycle sequences, 51.07s).
- **Recovery Isolation Test**: `TestOrdinaryFailureSkipsRecovery` passed (0.03s).
- **Collision Sentinel Test**: `TestDuplicateEventSignalIsEventIDOnly` passed (0.02s).
- **Rebuild Detector**: `TestConvProjectionRebuild` passed (0.06s).
- **Relative Latency**: `TestConvProjectionFasterThanReference` passed (11.35ms projection vs. 12.70s reference replay = **1,118x speedup**).

---

## Top 3 Weakest Points & Non-Blocking Notes

1. **Test Hook Global Variable Scope (`daemon.go:72`)**:
   - *Note*: `testRecoveryVerifyHook` is a package-level function pointer used strictly in tests with `t.Cleanup(func() { testRecoveryVerifyHook = nil })`.
2. **Hardcoded Projection Version in Upsert (`conv.go:69`)**:
   - *Note*: `upsert` writes literal `1` for `projection_version`. Future version bumps should update this constant alongside `Projection.Version()`.
3. **Latency Relative Constant (`convproj_diff_test.go:189`)**:
   - *Note*: Test asserts `projDur*10 > refDur`, while empirical measurement shows >1000x speedup.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All findings from previous rounds are completely resolved. All detectors are RED-capable, build is clean, and test suite passes 100%. | `internal/app/daemon/daemon.go:67-73, 485-492` |

---

VERDICT: PASS

# Implementation Review (Round 3): Conversation Read-Model Projection (`convproj`)

**Scope**: Commit `62f28d3` on branch `slice/p0-convproj`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `9430b37`  

---

## Executive Summary

Commit `62f28d3` resolves all Round 2 implementation findings, cementing exact error classification, collision isolation, and detector rigor:
1. **Exact `event_id` Duplicate Sentinel (`eventIDExists`)**: Replaced fragile driver string matching with an exact SQL query (`SELECT 1 FROM events WHERE event_id=? LIMIT 1`) executed inside the same transaction handle `tx`. Other constraint failures (e.g. `journal_offset` primary key collisions or sequence violations) are never misclassified as redeliveries.
2. **Dedicated Sentinel Regression Test (`TestDuplicateEventSignalIsEventIDOnly`)**: `internal/kernel/journal/journal_test.go:735-756` asserts that identical event IDs produce `journal.ErrDuplicateEvent` while fresh event IDs succeed and never surface the sentinel.
3. **Collision-Only Recovery Gate Test (`TestOrdinaryFailureSkipsRecovery`)**: `internal/app/daemon/convproj_diff_test.go:255-274` proves that an ordinary first-run failure returns immediately without entering recovery or running full-journal `VerifyChain()` scans, while subsequent redeliveries of the same turn ID enter recovery correctly.
4. **Non-Vacuous Rebuild Oracle**: Pre- and post-rebuild states are verified against `referenceConversationHistory` with a non-empty history assertion (`len(before) > 0`).

All 39 packages across the repository build and pass `go test ./...` with zero failures.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Read-Model & Projection Architecture (`ARCHITECTURE-ESSENTIALS.md` E4, `internal/kernel/journal/projection.go:89-103`)**:
  - `conv.Projection` maintains the synchronous projection lifecycle inside `Journal.Append` transactions.
- **[COMPLIANT] Single Ownership & Event Sourcing (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Canonical event journal remains the sole source of truth; projections are derived and rebuilt via `Projection.Reset()`.
- **[COMPLIANT] Cryptographic Integrity & Fail-Closed Recovery (`HARNESS-SPEC.md`, Annex A P0.3)**:
  - Recovery continues to execute `VerifyChain()` exclusively upon confirmed duplicate event collisions.
- **[COMPLIANT] Lexical Guard Isolation (`internal/kernel/journal/projection.go:26-33, 196-211`)**:
  - `conv_turns` table name avoids collision with guarded canonical table keywords.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - All code, identifiers, comments, error messages, and documentation are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The pre-check `eventIDExists` is executed within the active transaction `tx`, avoiding driver-dependent string inspection or out-of-band queries.

---

## Spec & Implementation Verification

### 1. Verification of Transactional Safety & Error Classification

| Analysis Area | Verification | Evaluation |
| :--- | :--- | :--- |
| **Transaction Visibility & TOCTOU** | `journal.go:511-518, 647-653` | **Verified**: `eventIDExists` runs `tx.QueryRow` on the active transaction `tx` that experienced the `INSERT INTO events` failure. Because journal writes are single-writer serialized by the append actor, there is zero TOCTOU risk or cross-transaction race condition. |
| **Exact Uniqueness Classification** | `journal.go:514` | **Verified**: Only a collision on `event_id` causes `eventIDExists` to return `true`. Collisions on `journal_offset` or other constraints return `false`, preserving the underlying error without wrapping `ErrDuplicateEvent`. |
| **Recovery Gating** | `daemon.go:285-287`, `convproj_diff_test.go:255-274` | **Verified**: Ordinary first-run errors return directly (`!errors.Is(err, journal.ErrDuplicateEvent)`), preventing $O(\text{events})$ chain verification during provider outages. |
| **Rebuild Equivalence** | `convproj_diff_test.go:210-250` | **Verified**: Non-vacuous test ensures `len(before) != 0` and proves that projection matches reference replay both before and after database refold. |

### 2. Empirical Verification

- **Full Workspace Test Suite**: `go test ./...` passed across all 39 packages (0 failures).
- **Collision Sentinel Test**: `TestDuplicateEventSignalIsEventIDOnly` passed.
- **Recovery Isolation Test**: `TestOrdinaryFailureSkipsRecovery` passed.
- **Rebuild Detector**: `TestConvProjectionRebuild` passed (0.07s).
- **Latency Benchmark**: `TestConvProjectionFasterThanReference` passed (2298x speedup on 3,000 turns).
- **Differential Oracle**: `TestConvProjectionMatchesReferenceOnEveryPrefix` passed (200 randomized sequences).

---

## Top 3 Weakest Points & Non-Blocking Notes

1. **Differential Oracle Kinds List (`convproj_diff_test.go:89`)**:
   - *Note*: While `conv.go:107-109` enforces `json.Unmarshal(...) != nil` parity, `kinds` in `TestConvProjectionMatchesReferenceOnEveryPrefix` does not explicitly generate malformed JSON payloads. (Non-blocking test coverage note).
2. **Hardcoded Projection Version in Upsert (`conv.go:69`)**:
   - *Note*: `upsert` writes literal `1` for `projection_version`. Future version bumps should update this query in tandem with `Projection.Version()`.
3. **Relative Latency Constant in Test (`convproj_diff_test.go:185`)**:
   - *Note*: Test asserts `projDur*10 > refDur`, while empirical measurement demonstrates a 2,298x speedup.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All round-2 findings fully resolved. Exact event_id sentinel pre-check, recovery gating, and rebuild verification are sound and complete. | `internal/kernel/journal/journal.go:511-518, 647-653` |

---

VERDICT: PASS

# Implementation Review (Round 2): Conversation Read-Model Projection (`convproj`)

**Scope**: Commit `9430b37` on branch `slice/p0-convproj`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `5da84aa`  

---

## Executive Summary

Commit `9430b37` successfully addresses and closes all substantive findings raised in Round 1 of implementation review:
1. **JSON Decode Error Handling in `turn.succeeded`**: `internal/conv/conv.go:107-109` now explicitly checks `json.Unmarshal(ev.Envelope.Payload, &pl) != nil` and aborts the fold without marking completion or recovery state. This ensures 100% observational equivalence with `referenceConversationHistory` and `referenceRecoveredTurnOutcome` for any malformed payload accepted by the journal.
2. **Collision-Gated Recovery & Integrity Verification**: `internal/kernel/journal/journal.go:514-516` classifies unique-constraint violations on `events.event_id` into the typed error `journal.ErrDuplicateEvent`. In `internal/app/daemon/daemon.go:285-287`, `RunChannelTurn` gates `recoveredTurnOutcome` and its underlying `VerifyChain()` full scan strictly behind `errors.Is(err, journal.ErrDuplicateEvent)`. Ordinary first-run failures (e.g. planner/provider errors) fail fast without triggering $O(\text{events})$ chain verification, structurally eliminating backlog explosion during upstream outages.
3. **Non-Vacuous Rebuild Oracle**: `internal/app/daemon/convproj_diff_test.go:210-247` corrects the turn ID mapping (`turn-chan-chat-1-%d` with matching update ID `i`), asserts non-empty pre-rebuild history (`len(before) > 0`), and validates pre- and post-rebuild projection states directly against `referenceConversationHistory`.

All tests across all 39 packages pass cleanly.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Read-Model & Projection Architecture (`ARCHITECTURE-ESSENTIALS.md` E4, `internal/kernel/journal/projection.go:89-103`)**:
  - `conv.Projection` adheres to the `journal.SyncProjection` contract, folding synchronously in the journal append transaction.
- **[COMPLIANT] Single Ownership & Event Sourcing (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Canonical event journal remains the sole source of truth. Read-model tables are derived and rebuildable via `Projection.Reset()`.
- **[COMPLIANT] Cryptographic Integrity & Fail-Closed Recovery (`HARNESS-SPEC.md`, Annex A P0.3)**:
  - Redelivery outcome recovery retains full-chain integrity validation via `VerifyChain()`, while isolating this verification exclusively to duplicate event collisions.
- **[COMPLIANT] Lexical Guard Isolation (`internal/kernel/journal/projection.go:26-33, 196-211`)**:
  - `conv_turns` table avoids reserved identifiers (`events`, `journal_meta`, `projection_offsets`, `proj_sync_offsets`).
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - All code, comments, error strings, and documentation are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The implementation cleanly isolates collision detection through Go's standard typed error wrapping (`%w` with `ErrDuplicateEvent`) without string parsing or foreign dependencies.

---

## Spec & Implementation Verification

### 1. Verification of Round 1 Fixes

| Item | Implementation Check | Evaluation |
| :--- | :--- | :--- |
| **Malformed `turn.succeeded` Payload** | `conv.go:107-109` | **Verified**: `if json.Unmarshal(...) != nil { return nil }` prevents invalid JSON from setting `hist_done=1` or `rec_state='SUCCEEDED'`, exactly matching `daemon.go:379-385` and `daemon.go:509-511`. |
| **`ErrDuplicateEvent` Classification** | `journal.go:514-516, 642-644`, `daemon.go:285-287` | **Verified**: `insertInTx` wraps SQLite UNIQUE constraint failures as `ErrDuplicateEvent`. `RunChannelTurn` checks `!errors.Is(err, journal.ErrDuplicateEvent)` and returns immediately on normal errors. |
| **Non-Vacuous Rebuild Detector** | `convproj_diff_test.go:210-247` | **Verified**: Corrected turn ID formatting produces 5 completed turns. Asserts `len(before) != 0`, `blocksEqual(before, ref0)`, `blocksEqual(before, after)`, and `blocksEqual(after, refAfter)`. |

### 2. Empirical Verification

- **Full Workspace Test Suite**: `go test ./...` passed across all 39 packages.
- **Differential Oracle**: `TestConvProjectionMatchesReferenceOnEveryPrefix` passed (200 randomized lifecycle sequences, 61.91s).
- **Relative Latency**: `TestConvProjectionFasterThanReference` passed (4.37ms projection vs. 10.04s reference replay = **2298x speedup** on 3,000 turns with 1,500 uncompleted tail).
- **Rebuild Detector**: `TestConvProjectionRebuild` passed (0.07s).

---

## Top 3 Weakest Points & Non-Blocking Notes

1. **Differential Oracle Generator Kinds Pool (`convproj_diff_test.go:88`)**:
   - *Note*: While `conv.go:107-109` correctly handles decode errors, `kinds` in `TestConvProjectionMatchesReferenceOnEveryPrefix` can optionally include `"malformed-succeed"` to explicitly exercise corrupted payloads during prefix fuzzing.
2. **Projection Version Literal in Upsert (`conv.go:69`)**:
   - *Note*: `upsert` writes `projection_version = 1`. In the event of future schema migrations, this constant should be updated in tandem with `Projection.Version()`.
3. **Relative Latency Assertion Constant (`convproj_diff_test.go:185`)**:
   - *Note*: Test asserts `projDur*10 > refDur` (10x minimum), whereas the measured ratio was 2298x.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All round-1 findings are completely and correctly resolved. Observational equivalence, collision-gated recovery, and non-vacuous rebuild testing are fully verified. | `internal/conv/conv.go:103-124`, `internal/kernel/journal/journal.go:510-520` |

---

VERDICT: PASS

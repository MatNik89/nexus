# Review of Design Plan: Conversation Read-Model Projection v2 (`docs/PLAN-CONVPROJ.md`)

**Scope**: Review of `docs/PLAN-CONVPROJ.md` (commit `d29b7bd` on branch `slice/p0-convproj`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `a51aa65699ba9946a05d0644dbc3c1c1a82507de` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-CONVPROJ.md` (v2) fully folds all round-1 findings from both reviewers (Codex and Kilo) into a robust, complete, and mathematically grounded design for the `conv` read-model projection.

The plan replaces per-turn $O(N)$ full-journal scans in `conversationHistory` and `recoveredTurnOutcome` with a synchronous SQLite projection (`conv_turns`) while guaranteeing exact observational equivalence against the existing replay implementation:
1. **Universal Lifecycle UPSERT**: Rows are created/updated on any lifecycle event (`channel.inbound_admitted`, `turn.succeeded`, `approval.turn_suspended`, `turn.resumed`, `turn.failed`), supporting sparse and out-of-order event sequences without requiring a prior admission record.
2. **Immutable Admission Order (`seq`)**: `seq` is assigned the journal offset of the first-observed event for the turn and is never modified by subsequent lifecycle updates, ensuring that completion order never corrupts admission history order.
3. **True $O(12)$ Completed History via Partial Index**: Partial index `ON conv_turns(identity, seq) WHERE hist_final IS NOT NULL` together with `AND turn_id != ?` guarantees direct index range scans for the FIFO-12 completed window even in the presence of an arbitrarily large incomplete tail.
4. **Distinct Recovery vs. History State**: Separate `hist_final` (history completion) and `rec_state` + `susp_summary` (recovery state machine) preserve the subtle differences between history and recovery semantics (e.g., empty-final success and challenge summary preservation).
5. **Differential Reference Oracle**: The legacy replay code is preserved in test code as an independent ground-truth oracle against which randomized, seeded lifecycle sequences are tested on every prefix.
6. **Relative Latency Gate**: Replaces brittle wall-clock assertions with a relative performance detector requiring the projection to answer $\ge 20\times$ faster than reference replay on the identical corpus.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Read-Model & Projection Architecture (`ARCHITECTURE-ESSENTIALS.md` E4, `internal/kernel/journal/projection.go:89-103`)**:
  - `conv_turns` is a derived read model updated synchronously inside `Journal.Append`'s immediate transaction.
- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Canonical event journal remains the sole source of truth; read-model rebuilds on version bump via `SyncProjection.Reset` and replay.
- **[COMPLIANT] Lexical Guard Isolation (`internal/kernel/journal/projection.go:26-33, 196-211`)**:
  - `conv_turns` strictly avoids reserved table identifiers (`events`, `journal_meta`, `projection_offsets`, `proj_sync_offsets`) and avoids keyword collisions in SQL comments.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - All documentation, schema, and planned identifiers are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The design eliminates $O(N)$ hot-path full scans through standard SQLite indexing and projection reuse without introducing background threads, memory caches, or third-party dependencies.

---

## Spec & Design Review

### 1. Observational Equivalence & Incremental Precedence

| Event | `conv_turns` Field Updates | History Observation | Recovery Observation |
| :--- | :--- | :--- | :--- |
| `channel.inbound_admitted` | Sets `identity, update_id, user_text, seq = offset` (if new). | Ignored until completed. | Ignored (`rec_state = 'NONE'`). |
| `turn.succeeded` | Sets `hist_final = final` (non-NULL). If `final != ""`, sets `rec_state = 'SUCCEEDED'`. | Becomes visible in history (`hist_final IS NOT NULL`). | Yields `RecoveredOutcome{Kind: "SUCCEEDED", Final: hist_final}` if `final != ""`. |
| `approval.turn_suspended` | Sets `susp_summary = summary, rec_state = 'SUSPENDED'`. | Ignored (`hist_final` remains NULL). | Yields `RecoveredOutcome{Kind: "SUSPENDED", Final: susp_summary}`. |
| `turn.resumed` | If `rec_state == 'SUSPENDED'`, clears `susp_summary = NULL, rec_state = 'NONE'`. | Unchanged. | Stale suspension suppressed. |
| `turn.failed` | Sets `rec_state = 'FAILED', code = err_code, tool = tool_id`. | Ignored (`hist_final` remains NULL). | Yields `RecoveredOutcome{Kind: "FAILED", Code: code, Tool: tool}`. |

- **Admission Exclusion**: History query includes `WHERE identity = ? AND turn_id != ? AND hist_final IS NOT NULL ORDER BY seq DESC LIMIT 12`, ensuring current in-flight turn is never self-included on redelivery.
- **Sparse Rows**: Unadmitted suspended or failed turns create rows with `identity = NULL, user_text = NULL`, satisfying recovery lookup by `turn_id` without polluting history.

### 2. Proof & Verification Architecture

- **Differential Equivalence Oracle**:
  - Independent reference implementation (`conversationHistoryReference`, `recoveredTurnOutcomeReference`) exercised against randomized, seeded lifecycle sequences.
  - Verifies exact match across all prefix states (empty-final, suspended-first, resumed-terminal, interleaved identities).
- **Relative Latency Detector**:
  - Evaluated on $N=3000$ completed + $N=3000$ incomplete tail turns.
  - Asserts $\text{Latency}(\text{Projection}) \le \frac{1}{20} \times \text{Latency}(\text{Reference Replay})$.
  - Machine-independent and resistant to CI hardware variance.

---

## Top 3 Weakest Points & Implementation Notes

1. **SQL NULL vs. Empty String Distinction (`PLAN-CONVPROJ.md:29-34`)**:
   - *Note*: Ensure Go DB driver writes non-NULL empty string `""` when `turn.succeeded` has `final = ""` so that `WHERE hist_final IS NOT NULL` matches empty-final completed turns while uncompleted turns remain `NULL`.
2. **`ON CONFLICT(turn_id) DO UPDATE` Column Mask (`PLAN-CONVPROJ.md:26-39`)**:
   - *Note*: The SQL UPSERT statement must explicitly omit `seq` from the `UPDATE SET` clause (e.g. `user_text = COALESCE(excluded.user_text, conv_turns.user_text)`) so that `seq` remains immutable at the first-observed event offset.
3. **`recoveredTurnOutcome` Final Mapping (`PLAN-CONVPROJ.md:29-34`)**:
   - *Note*: For `rec_state = 'SUCCEEDED'`, `Final` is mapped from `hist_final`; for `rec_state = 'SUSPENDED'`, `Final` is mapped from `susp_summary`.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All round-1 findings fully folded. Observational equivalence, indexing, lifecycle UPSERTs, and differential oracle are complete and sound. | `PLAN-CONVPROJ.md:23-83` |

---

VERDICT: PASS

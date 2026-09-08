# Review of Design Plan: Conversation Read-Model Projection v3 (`docs/PLAN-CONVPROJ.md`)

**Scope**: Review of `docs/PLAN-CONVPROJ.md` (commit `323f4e4` on branch `slice/p0-convproj`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `a51aa65699ba9946a05d0644dbc3c1c1a82507de` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-CONVPROJ.md` (v3) rigorously addresses and resolves all round-2 findings from peer review (Codex and Kilo), solidifying the mathematical and behavioral equivalence between the projection read model and the legacy replay engine.

The v3 design establishes three crucial architectural refinements:
1. **Dedicated History Completion Marker (`hist_done`)**: Decouples the completion predicate from row content. `hist_done` is a boolean set by every valid `turn.succeeded` event (including empty-final completions), while `hist_final` stores the lossless final payload text.
2. **Decoupled Sequence Offsets (`created_seq` vs. `hist_seq`)**: `created_seq` records the immutable first-observed journal offset of the row, while `hist_seq` records the journal offset set strictly by `channel.inbound_admitted`. This preserves exact admission-order history even under out-of-order or sparse lifecycle sequences (e.g., failure or suspension observed before admission).
3. **Annex A P0.3 Provenance Columns**: Adds `source_event_id`, `source_offset`, and `projection_version` to `conv_turns`, updated on every fold touch to track provenance for auditing and versioned rebuilds.
4. **Unconditional $O(12)$ Completed History via Partial Index**: The partial index `ON conv_turns(identity, hist_seq) WHERE hist_done AND hist_seq IS NOT NULL` paired with `AND turn_id != ?` guarantees direct index range scans for the FIFO-12 completed window regardless of an unbounded incomplete or failed tail.
5. **Differential Reference Oracle**: The legacy replay code is retained in test harnesses as an independent oracle, asserting prefix-by-prefix equivalence over randomized, seeded lifecycle sequences.
6. **Relative Latency Gate**: Replaces machine-dependent wall-clock cutoffs with a relative benchmark asserting $\ge 20\times$ speedup over the reference replay on a corpus with 3,000 completed and 3,000 incomplete tail turns.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Read-Model & Projection Architecture (`ARCHITECTURE-ESSENTIALS.md` E4, `internal/kernel/journal/projection.go:89-103`)**:
  - `conv_turns` is maintained synchronously inside `Journal.Append` (`BEGIN IMMEDIATE` transaction).
- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Canonical event journal remains the sole source of truth; read-model rebuilds on version bump via `SyncProjection.Reset` and replay.
- **[COMPLIANT] Projection Provenance Metadata (`HARNESS-SPEC.md:1395-1403`, Annex A P0.3)**:
  - `source_event_id`, `source_offset`, and `projection_version` are explicitly tracked per row.
- **[COMPLIANT] Lexical Guard Isolation (`internal/kernel/journal/projection.go:26-33, 196-211`)**:
  - `conv_turns` avoids reserved table names (`events`, `journal_meta`, `projection_offsets`, `proj_sync_offsets`) and avoids keyword collisions in SQL comments.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - All documentation, schema, and code symbols are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: Eliminates $O(N)$ full-scan complexity on every turn by reusing existing `SyncProjection` infrastructure without introducing background polling, in-memory caches, or foreign dependencies.

---

## Spec & Design Review

### 1. Observational Equivalence & Incremental Precedence

| Event | `conv_turns` Field Updates | History Observation | Recovery Observation |
| :--- | :--- | :--- | :--- |
| `channel.inbound_admitted` | Sets `identity, update_id, user_text, hist_seq = offset`. If new row, sets `created_seq = offset`. Updates P0.3 provenance. | Inactive until `hist_done = true`. | Ignored (`rec_state = 'NONE'`). |
| `turn.succeeded` | Sets `hist_done = true, hist_final = final`. If `final != ""`, sets `rec_state = 'SUCCEEDED'`. If new row, sets `created_seq = offset`. Updates P0.3 provenance. | Enters history if `hist_seq IS NOT NULL` (even if `hist_final == ""`). | Yields `RecoveredOutcome{Kind: "SUCCEEDED", Final: hist_final}` if `final != ""`. |
| `approval.turn_suspended` | Sets `susp_summary = summary, rec_state = 'SUSPENDED'`. If new row, sets `created_seq = offset`. Updates P0.3 provenance. | Ignored (`hist_done` remains false). | Yields `RecoveredOutcome{Kind: "SUSPENDED", Final: susp_summary}`. |
| `turn.resumed` | If `rec_state == 'SUSPENDED'`, clears `susp_summary = NULL, rec_state = 'NONE'`. Updates P0.3 provenance. | Unchanged. | Stale suspension suppressed. |
| `turn.failed` | Sets `rec_state = 'FAILED', code = err_code, tool = tool_id`. If new row, sets `created_seq = offset`. Updates P0.3 provenance. | Ignored (`hist_done` remains false). | Yields `RecoveredOutcome{Kind: "FAILED", Code: code, Tool: tool}`. |

- **Admission Order Invariant**: History is ordered strictly by `hist_seq` (the admission offset), not `created_seq`. Even if a turn is first observed via a lifecycle event, its history position is determined solely by the arrival offset of its admission.
- **Empty-Final History Completeness**: Succeeded turns with `final = ""` have `hist_done = true` and enter history correctly, preserving the user block and FIFO-12 window without rendering a non-existent assistant block.
- **Current Turn Exclusion**: History query includes `turn_id != ?`, preventing redelivered turns from seeing themselves.

### 2. Proof & Verification Architecture

- **Seeded Differential Reference Oracle**:
  - Independent reference functions (`conversationHistoryReference`, `recoveredTurnOutcomeReference`) run against randomized lifecycle event streams covering all state transitions, sparse arrivals, and interleaved identities.
  - Asserts identical history blocks and recovery outcomes on every sequence prefix.
- **Host-Independent Relative Latency Gate**:
  - Evaluated on $N=3000$ completed + $N=3000$ incomplete tail turns.
  - Requires $\text{Latency}(\text{Projection}) \le \frac{1}{20} \times \text{Latency}(\text{Reference Replay})$.

---

## Top 3 Weakest Points & Editorial Notes

1. **Section Header Version Tag (`PLAN-CONVPROJ.md:23`)**:
   - *Editorial Note*: The section header reads `## Change (v2 — round-1 findings folded)` while incorporating v3 round-2 findings. (Non-blocking editorial note per verdict policy).
2. **Boolean Mapping in SQLite (`PLAN-CONVPROJ.md:29-33`)**:
   - *Implementation Note*: In SQLite DDL, `hist_done` is `INTEGER NOT NULL DEFAULT 0`. The partial index `WHERE hist_done AND hist_seq IS NOT NULL` evaluates `hist_done` as truthy (`hist_done != 0`).
3. **`ON CONFLICT(turn_id) DO UPDATE` Immutable Fields (`PLAN-CONVPROJ.md:26-39`)**:
   - *Implementation Note*: `created_seq` must never be updated in `DO UPDATE SET`, and `hist_seq` must only be updated when `excluded.hist_seq IS NOT NULL` and `conv_turns.hist_seq IS NULL`.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All round-2 findings folded cleanly. Separation of `hist_done`, `created_seq`, `hist_seq`, P0.3 provenance, and differential oracle is fully sound and complete. | `PLAN-CONVPROJ.md:23-83` |

---

VERDICT: PASS

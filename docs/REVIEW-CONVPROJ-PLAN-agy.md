# Review of Design Plan: Conversation Read-Model Projection (`docs/PLAN-CONVPROJ.md`)

**Scope**: Review of `docs/PLAN-CONVPROJ.md` (commit `a542126` on branch `slice/p0-convproj`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `a51aa65699ba9946a05d0644dbc3c1c1a82507de` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-CONVPROJ.md` addresses the root cause of the 24-hour soak failure (`soak-msg-001237 unanswered for >60s (backlog explosion)` at 42m), where `conversationHistory` and `recoveredTurnOutcome` executed full `Journal.Replay(0, ...)` scans on every single channel turn.

The proposed design replaces both full-journal scans with a dedicated `conv` projection implemented via the established in-repo `SyncProjection` framework (`internal/kernel/journal/projection.go:89-103`):
1. **Incremental Event Folding**: Synchronously folds `channel.inbound_admitted`, `turn.succeeded`, `turn.failed`, `approval.turn_suspended`, and `turn.resumed` in the append transaction.
2. **O(12) History & O(1) Recovery**: Replaces linear $O(\text{events})$ replay scans with indexed `conv_turns` queries (`SELECT ... ORDER BY seq DESC LIMIT 12` and primary key lookup by `turn_id`).
3. **Observational Equivalence**: Fully preserves existing behavioral semantics (completion bit owned by `turn.succeeded`, rune-safe 1500 clipping via `historyBlocks`, resumed challenge suppression, and typed drift reconstruction).
4. **Seamless Rebuild**: Leverages the existing `SyncProjection.Reset` and `catchupProjections` mechanics on version bump to rebuild historical state from canonical events automatically on `journal.Open`.
5. **Decisive Latency Detector**: Adds a red-capable benchmark test with $N=3000$ seeded turns asserting `RunChannelTurn` latency $< 250\text{ms}$ (dropping from seconds under replay to $< 1\text{ms}$ under indexed query).

The design is sound, minimally scoped, fail-closed, and architecturally verified.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Read-Model & Projection Architecture (`ARCHITECTURE-ESSENTIALS.md` E4, `journal/projection.go:89-103`)**:
  - `conv_turns` is a derived projection folded synchronously inside `Journal.Append` (`BEGIN IMMEDIATE` transaction). State reconstruction and catchup are owned by the journal engine.
- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Canonical events remain the sole source of truth; the projection is purely a read model for channel turn execution.
- **[COMPLIANT] Lexical Guard Isolation (`journal/projection.go:26-33, 196-211`)**:
  - `conv_turns` avoids canonical table names (`events`, `journal_meta`, `projection_offsets`, `proj_sync_offsets`) and queries via `j.QueryProjection`.
- **[COMPLIANT] Repository Language Invariant (`AGENTS.md`)**:
  - English throughout documentation, schema definitions, and comments.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The plan eliminates the $O(N)$ full-scan smell by reusing the existing `SyncProjection` infrastructure without adding external dependencies or background daemon threads.

---

## Spec & Design Review

### 1. Observational Equivalence & Incremental Precedence

- **History Selection**:
  - `conversationHistory` queries `conv_turns` for `identity = ? AND completed = 1 ORDER BY seq DESC LIMIT 12`.
  - Reversing the returned slice yields the exact chronological order matching the existing `order` slice in `daemon.go:346-384`.
  - Passing entries into `historyBlocks` preserves identical zero-padded block IDs (`hist-<scope>-000000-a-user`) and rune-safe 1500 clipping.
- **Turn Recovery Lifecycle**:
  - `approval.turn_suspended` sets `state = 'SUSPENDED'` with summary final.
  - `turn.resumed` clears `state = 'SUSPENDED'` back to `''` / `'RUNNING'`, materializing suppression incrementally.
  - `turn.succeeded` or `turn.failed` sets terminal state (`'SUCCEEDED'` or `'FAILED'` with `code` and `tool`).
  - Lookup by `turn_id` immediately yields the authoritative terminal or suspended outcome, matching `recoveredTurnOutcome` (`daemon.go:455-509`).

### 2. Failure Policy & Guard Verification

- **Crash & Replay Recovery**:
  - If a crash occurs mid-turn, `conv_turns` reflects the exact state at the last committed journal offset.
  - On restart or version bump, `journal.Open` rebuilds `conv_turns` via canonical replay, producing identical state.
- **Lexical Guard Safety**:
  - DDL and SQL queries in `conv` projection do not reference canonical table keywords in identifiers or comments.

---

## Top 3 Weakest Points (Mandatory Review Discipline)

1. **Schema Column List Clarification (`PLAN-CONVPROJ.md:32-33`)**:
   - *Analysis*: Line 32 lists columns `(identity, update_id, turn_id PRIMARY KEY, user_text, final, code, tool, state, seq)` while lines 27 and 35 refer to `completed`. The implementation should explicitly include `completed INTEGER NOT NULL DEFAULT 0` (or filter by `state = 'SUCCEEDED'`).
2. **Current Turn Defensive Filter (`PLAN-CONVPROJ.md:34-35`)**:
   - *Analysis*: While `completed = 1` naturally filters out the current in-flight turn (which has `completed = 0`), adding `AND turn_id != ?` (passing the current turn ID) ensures defensive exclusion on re-entrant calls.
3. **Compound Index Optimization (`PLAN-CONVPROJ.md:33`)**:
   - *Analysis*: Line 33 specifies an index on `(identity, seq)`. Creating `(identity, completed, seq)` (or `(identity, state, seq)`) allows SQLite to satisfy `WHERE identity = ? AND completed = 1 ORDER BY seq DESC` via direct index range scan without filtering discarded uncompleted rows.

None of these weak points represent design flaws; all are straightforward implementation details.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | Design is sound, minimal, and fully compatible with all existing invariants. | `PLAN-CONVPROJ.md:23-59` |

---

VERDICT: PASS

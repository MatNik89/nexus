# Review of Slice: Native Telegram Tables via `sendRichMessage` (`richtable`)

**Scope**: Commit `5871a71` on branch `slice/p0-richtable`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `f4889a8` (`origin/main`)  

---

## Executive Summary

Commit `5871a71` implements native Telegram table rendering via `sendRichMessage` (Bot API 10.1) and updates the planner system prompt to direct the model to emit GitHub-style pipe tables.

The implementation is minimal, correct, and strictly preserves all delivery honesty and leasing contracts:
1. **Delivery Contract Preservation (B2 / E15 / HARDQ B2)**:
   - Maintains strict **ONE wire send per flush tick** discipline.
   - On the first attempt (`o.Attempts == 0`), if `hasPipeTable(o.Text)` is true, the adapter calls `sendRichMessage` carrying `rich_message: {"markdown": clip32k(o.Text)}`.
   - If the remote call fails (e.g. 400 Bad Request on legacy servers or network timeout), the row is re-pended with `Attempts > 0`. Subsequent flush ticks fall through to plain `sendMessage` without formatting, preventing infinite retry loops or duplicate sends.
2. **Table Detection & Content Bounding**:
   - `hasPipeTable` reuses the existing table-divider parser from `render.go` (`isTableDivider`).
   - `clip32k` enforces a 32,768-rune safe boundary preventing UTF-8 split errors.
3. **Planner System Prompt Alignment**:
   - `internal/llm/planner/planner.go:79-85` instructs the model to format tabular data as GFM pipe tables rather than bullet lists.
4. **Empirical Test Verification**:
   - `TestPipeTableUsesRichMessage` asserts that pipe tables are dispatched via `sendRichMessage` carrying the raw Markdown content.
   - `TestNonTableSkipsRichMessage` verifies that non-tabular text bypasses `sendRichMessage` and uses the plain/HTML path.
   - Full workspace test suite (`go test ./...`) passes with 0 failures across all 39 packages.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Delivery Honesty & Outbox Leasing (`ARCHITECTURE-ESSENTIALS.md` E15, `HARDQ B2`)**:
  - `FlushOutbox` executes exactly one remote HTTP call per row attempt. No in-tick double sends.
- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5)**:
  - Outbox manager remains the sole owner of delivery status and attempt tracking.
- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - Code, documentation, comments, and prompts are in English (with Croatian instruction preserved for user replies).

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The implementation stops at Rung 3 (minimal local code, reusing `isTableDivider` and existing `Flush` loop) without introducing new dependencies or background daemons.

---

## Spec & Implementation Review

### 1. Verification of Delivery & Formatting Semantics

| Path | Condition | Method Called | Payload Structure |
| :--- | :--- | :--- | :--- |
| **First Attempt (`Attempts == 0`) with Table** | `hasPipeTable(o.Text) == true` | `sendRichMessage` | `{"chat_id": chat, "rich_message": {"markdown": clip32k(o.Text)}}` |
| **First Attempt (`Attempts == 0`) without Table** | `renderHTML(o.Text)` ok | `sendMessage` | `{"chat_id": chat, "text": rendered, "parse_mode": "HTML"}` |
| **First Attempt (`Attempts == 0`) Plain Text** | `renderHTML(o.Text)` not ok | `sendMessage` | `{"chat_id": chat, "text": o.Text}` |
| **Retry (`Attempts > 0`)** | Any | `sendMessage` | `{"chat_id": chat, "text": o.Text}` |

- **No Double Send**: Each outbox entry makes at most one network call per invocation of `FlushOutbox`.
- **Graceful Fallback**: If a Telegram client/server rejects `sendRichMessage` (e.g. 400), the attempt counter increments and the message automatically degrades to plain text on the next tick.

### 2. Empirical Verification

- **Workspace Test Suite**: `go test ./...` passed across all packages.
- **Table Delivery Test**: `TestPipeTableUsesRichMessage` passed.
- **Non-Table Fallback Test**: `TestNonTableSkipsRichMessage` passed.

---

## Non-Blocking Notes & Observations

1. **`clip32k` Limit Constant (`telegram.go:336-343`)**:
   - *Note*: The 32,768 rune cap comfortably accommodates large markdown tables within Bot API payload ceilings.
2. **Table Detection Ordering (`telegram.go:309-316`)**:
   - *Note*: Evaluating `hasPipeTable` before `renderHTML` ensures that messages containing both fences/formatting and tables prioritize native rich table rendering.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | Implementation correctly delivers GFM pipe tables natively via `sendRichMessage`, preserves single-send delivery semantics, and passes all tests. | `internal/channel/telegram/telegram.go:300-344` |

---

VERDICT: PASS

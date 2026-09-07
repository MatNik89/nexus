# Review of Design Plan v4: Telegram Output Quality (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `81acb9f` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` v4 makes several clean design simplifications:
- **Scope Cut**: Multipart/chunking is cut from this slice entirely, removing the multi-chunk transaction and tag-splitting complexity.
- **No Fenced Execution**: Fenced tool calls are not executed; they are classified as drift and safely handled via a typed final.
- **Declared Collision Limitation**: The rare collision where a user explicitly asks for JSON containing a top-level `action` matching a known tool ID is explicitly declared as an intentional fail-closed restriction and locked by a committed test.
- **Constructive HTML Renderer**: Send-boundary rendering with strict precedence (`fence > code > bold`), non-overlapping spans, and a 50-tag entity budget ensuring valid, non-nested Telegram HTML without wire retries.

However, detailed review along the Standards and Spec axes identifies **critical design contradictions and defects** that must be resolved:

1. **Document Contradiction on Fallback Send (Hard Invariant Violation)**: Lines 42 and 122–123 state that an entity-parse fallback re-sends original plain text, directly contradicting Section B (line 102: *"NO fallback path exists; any Telegram 400 keeps today's semantics"*).
2. **Repository Constitution Language Violation (`AGENTS.md`)**: Lines 66–67 hardcode Croatian text (`"Nisam uspio ispravno pozvati alat (<tool>)..."`) inside the kernel planner. Kernel constants and typed fallbacks must remain in English.
3. **Empty Tag Rejection in Telegram Bot API**: The tokenizer and table renderer do not guard against generating empty tags (e.g. `<b></b>`, `<code></code>`, or `****`). Telegram Bot API strictly rejects empty tags with HTTP 400, permanently breaking delivery under no-fallback semantics.
4. **Drift Bypass on Mixed Prose + Fenced JSON**: The classifier only strips fences if the *entire* message is a single fence. If a model outputs leading conversational text (e.g. `"Here is your tool call:\n```json\n{"action":"memory_recall"}\n```"`), the drift check is bypassed and raw tool JSON leaks to chat.

Because unresolved design flaws and contradictions remain, the plan is **rejected**.

---

## Standards Review

### Documented Standards Compliance

- **[HARD VIOLATION] Contradictory Retry / Delivery Contract (`ARCHITECTURE-ESSENTIALS.md` E15, `AGENTS.md` S7, `PLAN-TGOUT.md:42, 102, 122-123`)**:
  - `PLAN-TGOUT.md:42`: *"We keep hermes' SEMANTICS (tables->bullets, plain fallback)"*
  - `PLAN-TGOUT.md:122-123`: *"HTML escape bugs could eat formatting, never content: the narrow entity-parse fallback re-sends the original plain."*
  - `PLAN-TGOUT.md:102`: *"parse_mode=HTML on every send; NO fallback path exists; any Telegram 400 keeps today's semantics byte-for-byte."*
  - Lines 42 and 122–123 are stale remnants from v2/v3 that directly contradict the core v4 architectural decision to remove all adapter-level retries. They must be deleted.

- **[HARD VIOLATION] Repository Language Rule (`AGENTS.md: Language`, `RULE[user_global]`)**:
  - `PLAN-TGOUT.md:66-67` specifies hardcoding a Croatian string: `"Nisam uspio ispravno pozvati alat (<tool>). Pokušaj ponovno ili preformuliraj."` in `ChatPlanner`.
  - `AGENTS.md` establishes: *"Everything you produce here is English — code, comments, docs, review files, commits"*.
  - All existing kernel refusals in `internal/channel/telegram/telegram.go:230, 239, 246, 275` are written in English. Kernel code and constants must remain in English; runtime conversational translation is governed by system prompt directives.

### Baseline Code Smells (Judgement Calls)

- **Primitive Obsession** (`PLAN-TGOUT.md:65-66`): Checking raw string keys on untyped JSON rather than unmarshaling into a dedicated Go struct (`struct { Action string `json:"action"`; ToolID string `json:"tool_id"` }`).
- **Speculative Generality** (`PLAN-TGOUT.md:93-97`): The 50-tag entity budget is an arbitrary heuristic; while safe, Telegram Bot API supports 100+ entities.

---

## Spec & Design Review

### 1. Telegram Bot API Rejection on Empty Tags (High Severity)

- **Plan Claim**: `PLAN-TGOUT.md:86-92` — *"tokenizer precedence: fenced block > inline code > bold... inside <pre>/<code> content is escaped only... table cell content is escaped, cell headings bolded whole"*.
- **Flaw**:
  - If markdown input contains empty formatting markers (e.g. `****` empty bold, ` `` ` empty inline code) or an empty table header (`| | Name |`), naive wrapping emits `<b></b>` or `<code></code>`.
  - Telegram Bot API rejects empty formatting tags with HTTP 400 (`Bad Request: can't parse entities: Tag must not be empty`).
  - Because v4 has no fallback send, any message with an empty formatting tag will fail delivery permanently.
- **Required Fix**:
  - The renderer must explicitly prune empty tags: if the token's escaped content is empty, emit the empty string without wrapping HTML tags.

### 2. Schema Drift Bypass on Mixed Conversational Text (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:62-64` — *"if the ENTIRE reply is one fenced block, strip exactly one fence FOR CLASSIFICATION ONLY. If the result still starts with a fence, the reply is prose (delivered)."*
- **Flaw**:
  - LLMs frequently prepend or append conversational prose to drifting tool calls:
    ```markdown
    I will look that up in memory for you:
    ```json
    {"action":"memory_recall","query":"prva poruka danas"}
    ```
    ```
  - Because the response is not *entirely* a single fenced block, the fence is not stripped, JSON decoding of the entire response fails, and the raw tool call leaks directly into the chat.
- **Required Fix**:
  - In addition to checking the whole response, the drift classifier should scan for single fenced JSON blocks within the reply if tools are active.

### 3. Table Bullet Expansion vs. Omitted Chunking (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:48-56, 89-92` — Chunking is deferred, but all markdown tables are expanded into bold-heading + bullet groups.
- **Flaw**:
  - Converting a multi-column table (e.g. 5 columns $\times$ 10 rows) into bullet groups expands character volume significantly due to repeated bullet labels (`• Header: value\n`).
  - A table that fit comfortably in 1,200 characters of compact markdown can easily balloon past 4,096 characters, failing Telegram's message limit.
- **Required Fix**:
  - Note this tradeoff explicitly in the plan and ensure table formatting avoids redundant whitespace or omits empty bullet lines.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | Docs | **HIGH** | Stale references in L42 and L122–123 contradict Section B's no-fallback contract. | `PLAN-TGOUT.md:42, 102, 122-123` |
| **F2** | Kernel | **HIGH** | Hardcoded Croatian fallback string in planner violates English-only repository standard. | `PLAN-TGOUT.md:66-67`, `AGENTS.md` |
| **F3** | Renderer | **HIGH** | Renderer does not suppress empty tags (`<b></b>`), triggering fatal Telegram 400 parse errors. | `PLAN-TGOUT.md:86-92` |
| **F4** | Planner | **MEDIUM** | Drift classifier fails on mixed prose + fenced tool calls, leaking raw JSON when models add commentary. | `PLAN-TGOUT.md:62-64` |
| **F5** | Renderer | **LOW** | Table bullet expansion significantly multiplies message length, increasing risk of 4096-limit rejections. | `PLAN-TGOUT.md:48-56, 89-92` |

---

## Required Plan Revisions

Before implementation begins, `docs/PLAN-TGOUT.md` must be revised to:
1. Delete lines 42 and 122–123 (removing all stale references to plain fallback resends).
2. Change the typed fallback final in `ChatPlanner` to English (`"Failed to execute tool (<tool>). Please try again or rephrase."`).
3. Explicitly require the renderer to suppress empty HTML tags (`<b></b>`, `<code></code>`, `<pre></pre>`).
4. Extend drift detection to inspect embedded code fences containing tool drift JSON.

---

VERDICT: FAIL

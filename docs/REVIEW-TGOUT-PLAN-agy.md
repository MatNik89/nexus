# Review of Design Plan: Telegram Output Quality (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `00dba3e` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` addresses two critical dogfood findings: raw tool-JSON leaking into chat upon model schema drift, and unformatted Telegram output. While the plan correctly identifies the user pain points and references local `hermes-agent` patterns, the proposed design contains **material architectural and behavioral flaws** across both functional areas:

1. **S7 Governance Violation**: The planner corrective retry issues a second physical provider call without specifying the allocation, issuance, consumption, and terminal reporting of an `AttemptGrant` (violating `AGENTS.md` and `ARCHITECTURE-ESSENTIALS.md` E5).
2. **Severe False-Positive JSON Trap**: The heuristic for "tool-shaped JSON" (`single JSON object whose top level has an action field`) traps legitimate user requests for JSON (e.g., GitHub webhook payloads, Redux actions, AWS IAM policies), actively suppressing the user's requested output.
3. **Infinite Re-pend Loop on Blanket 400 Fallback**: The Telegram adapter falls back to plain-text resend on *any* HTTP 400 error. For non-parse 400s (e.g., message > 4096 characters, blocked bot, chat not found), the plain resend fails identically, causing `channel.Core.Flush` to re-pend the delivery indefinitely and flood Telegram on every flush tick.
4. **Table Heuristic Data Loss**: Blindly copying hermes' `_render_table_block` algorithm imports a bug where data cells matching the heading text are silently dropped, and non-table pipe runs (ASCII diagrams, shell pipelines) are mangled.
5. **HTML Parser Edge Cases**: Unclosed markdown tags and language specifiers in code fences (`<pre>`) are unhandled, guaranteeing Telegram entity parse rejections.

Because unresolved flaws exist across high, medium, and low severity tiers, the plan is **rejected**.

---

## Standards Review

### Documented Standards Compliance

- **[HARD VIOLATION] S7 AttemptGrant Governance on Corrective Retry (`AGENTS.md`, `ARCHITECTURE-ESSENTIALS.md` E5, `s7min.go:94-105`, `planner.go:3-9, 255-273`)**:
  - `AGENTS.md` and E5 establish: *"S7 = only retry/cancel owner (`AttemptGrant` on every attempt)"*.
  - `internal/llm/planner/planner.go` strictly mandates: *"EVERY provider call is a PHYSICAL attempt: the planner issues a fresh S7 AttemptGrant per call and the provider transport consumes it"*.
  - The plan proposes: *"do ONE corrective retry: re-issue the chat with an appended system-style correction"* (`PLAN-TGOUT.md:51-52`).
  - In `s7min`, `Authority.Issue` grants exactly ONE attempt per `OperationID`. If `ChatPlanner.Plan` calls `c.chat.Chat` a second time without generating a fresh `opID` and calling `c.auth.Issue(op2, c.target)`, the transport will reject execution (`ATTEMPT_NOT_AUTHORIZED`), or the initial grant `op` will remain unfinalized / double-consumed. The plan must formally specify the multi-grant lifecycle in `Plan`.

- **[HARD VIOLATION] Delivery Honesty and Indiscriminate 400 Re-pend (`AGENTS.md` B2, `ARCHITECTURE-ESSENTIALS.md` E11/E15, `channel.go:500-538`)**:
  - `channel.Core.Flush` treats non-ambiguous errors as definite pre-wire failures and executes `Reconcile(ctx, o.DeliveryID, false)`, re-pending the row for future delivery.
  - Blindly falling back on *any* 400 (`PLAN-TGOUT.md:72-74`) converts permanent client errors (such as messages exceeding Telegram's 4096-character limit) into permanent failing retries every 2 seconds.

- **[HARD VIOLATION] Repository Language Invariant (`AGENTS.md`)**:
  - Hardcoding Croatian UI fallback strings directly in kernel planning logic (`PLAN-TGOUT.md:55-56`: `"Nisam uspio ispravno pozvati alat — pokušaj ponovno."`) violates the repo rule: *"Everything you produce here is English"*. Kernel-level typed errors and default messages must be in English; localized conversational behavior belongs to prompt instructions.

### Baseline Code Smells (Judgement Calls)

- **Primitive Obsession** (`PLAN-TGOUT.md:50-51`): Detecting tool calls via raw substring and unvalidated top-level JSON maps (`action` key existence) instead of typed schema discriminators.
- **Duplicated Code & Divergent Change** (`PLAN-TGOUT.md:72-76`): Mixing HTML entity conversion, error status parsing, and secondary transport dispatch into the `telegram.FlushOutbox` closure.
- **Untyped Wire Payloads** (`ARCHITECTURE-ESSENTIALS.md` E3): `telegram.go` passes untyped `map[string]any` to `a.call("sendMessage", ...)`. Adding `parse_mode` should introduce a concrete `sendMessageReq` struct.

---

## Spec & Design Review

### 1. Tool-Shaped JSON Heuristic False Positives (High Severity)

- **Plan Claim**: `PLAN-TGOUT.md:50-51` — *"if the reply still LOOKS tool-shaped (a single JSON object whose top level has an `action` field), do ONE corrective retry... If the second reply is still tool-shaped JSON, return a typed final... never the raw JSON."*
- **Flaw**:
  - This heuristic causes severe false positives on legitimate user queries that ask for JSON containing an `action` key.
  - **Concrete Failing Scenarios**:
    1. User: *"Give me an example GitHub webhook payload for an issue event."* $\rightarrow$ Model outputs `{"action":"opened","issue":{...}}`.
    2. User: *"Write a Redux action object to increment a counter."* $\rightarrow$ Model outputs `{"action":"INCREMENT","payload":1}`.
    3. User: *"Draft an AWS IAM statement for S3 read access."* $\rightarrow$ Model outputs `{"action":"s3:GetObject","effect":"Allow"}`.
  - Under `PLAN-TGOUT.md`, all such valid prose responses will be intercepted, re-prompted with an error message, and if repeated, replaced with a generic tool failure error.
- **Required Fix**:
  - The heuristic must only trigger when `len(c.specs) > 0` (tools path enabled).
  - The heuristic must verify whether the `action` field matches a registered tool name in `c.specs` (`_, ok := c.specs[contracts.ToolID(action)]`), or if `req.Action == "tool"` with malformed parameters. Arbitrary JSON objects must not be intercepted.

### 2. Blanket 400 Fallback & Delivery Poisoning (High Severity)

- **Plan Claim**: `PLAN-TGOUT.md:72-74` — *"on ANY Telegram 400 it RESENDS the ORIGINAL text plain (hermes' strip fallback, simpler: we still have the original — delivery honesty unchanged, same delivery id row, one SENT)."*
- **Flaw**:
  - Telegram returns HTTP 400 for a variety of reasons having nothing to do with entity parsing:
    - Message exceeds 4096 characters (`Bad Request: message is too long`).
    - Recipient blocked the bot (`Bad Request: bot was blocked by the user`).
    - Chat does not exist (`Bad Request: chat not found`).
  - If a message exceeds 4096 characters:
    1. HTML send fails with 400 (`message is too long`).
    2. Plain-text fallback fails identically with 400 (`message is too long`).
    3. `Flush` receives a non-ambiguous error and resets the outbox row to `PENDING`.
    4. On the next tick (2 seconds later), `Adapter.Run` retries the same message, triggering an infinite loop of duplicate 400 requests.
- **Required Fix**:
  - Inspect Telegram's JSON error description (`can't parse entities`). Only entity parsing errors may trigger the plain fallback.
  - Non-formatting 400 errors must be handled as terminal delivery failures or logged without triggering an immediate redundant send.
  - Long messages (> 4096 runes) must be chunked or capped prior to transport.

### 3. HTML Mode Strictness vs MarkdownV2 (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:40-44` — *"hermes uses MarkdownV2... We keep hermes' SEMANTICS... but use parse_mode=HTML — only 3 escapes (& < >), no placeholder engine, identical rendered result."*
- **Flaw**:
  - While HTML requires fewer escape characters than MarkdownV2, Telegram's HTML parser is exceptionally strict:
    1. **Language Tags in Code Blocks**: Markdown code fences often specify language (```` ```go ````). In Telegram HTML, bare `<pre>` does not parse language tags; it must be `<pre><code class="language-go">...</code></pre>`. A naive `<pre>` replacement leaves the language name leaked on the first line of code text.
    2. **Escaping Inside Code Spans**: Telegram HTML requires `&`, `<`, and `>` to be escaped *even inside* `<pre>` and `<code>`.
    3. **Unclosed Tags on Truncated Responses**: If an LLM response cuts off mid-turn with unclosed `**bold` or ```` ``` ````, naive substitution generates unclosed `<b>` or `<pre>`, causing immediate Telegram 400 parse failures.
    4. **Missing Formatting Support**: The plan completely drops markdown links (`[title](url)` $\rightarrow$ `<a href="url">title</a>`), italics (`*text*` / `_text_`), and headers (`# Title`), rendering them as ugly raw markdown syntax.

### 4. Table-to-Bullets Heuristic Data Loss (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:26-31, 64-68` — *"ALL markdown pipe-tables... -> bold-heading + bullet row groups, exactly hermes' convert_table_to_bullets semantics"*.
- **Flaw**:
  - In `hermes-agent` (`gateway/platforms/helpers.py:369-370`), the alignment loop contains:
    ```python
    if not has_row_label_col and value == heading:
        continue
    ```
  - For standard tables without a row-label column, `heading` is set to the first non-empty cell. Any subsequent cell in that row whose string value equals `heading` (e.g. status tables where multiple columns contain `"OK"`, `"Active"`, or `"None"`) is **silently deleted from the output**.
  - Non-table text using pipe characters (e.g. ASCII directory trees `|-- dir`, shell pipelines `cat a | grep b`, regex patterns `a|b`) can match `TABLE_SEPARATOR_RE` and be corrupted into mangled bullet groups.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | Planner | **HIGH** | Corrective retry performs ungoverned physical provider attempt without S7 `AttemptGrant` issuance and lifecycle management. | `PLAN-TGOUT.md:51-52`, `s7min.go:94-105`, `planner.go:255-273` |
| **F2** | Planner | **HIGH** | Tool-JSON detection heuristic traps and suppresses legitimate user requests for JSON containing top-level `action` keys. | `PLAN-TGOUT.md:50-57` |
| **F3** | Telegram | **HIGH** | Indiscriminate 400 fallback triggers infinite re-pend loops on non-parse errors (e.g., message length > 4096). | `PLAN-TGOUT.md:72-74`, `channel.go:530-538` |
| **F4** | Telegram | **MEDIUM** | HTML rendering ignores code block language specifiers, drops links/italics, and fails on unclosed syntax. | `PLAN-TGOUT.md:40-44, 69-71` |
| **F5** | Telegram | **MEDIUM** | Hermes table-to-bullets algorithm silently drops data cells matching row headings and misidentifies pipe runs. | `PLAN-TGOUT.md:26-31, 64-68`, `helpers.py:369` |
| **F6** | Kernel | **LOW** | Hardcoded Croatian UI text in planner violates English-only repository standard; untyped wire payloads in `telegram.go`. | `PLAN-TGOUT.md:55-56`, `AGENTS.md` |

---

## Required Plan Revisions

Before implementation begins, `docs/PLAN-TGOUT.md` must be revised to:
1. Specify exact S7 `AttemptGrant` issuance (`c.auth.Issue`), reporting, and error landing (`landFailure`) for the second physical provider call.
2. Constrain tool-JSON detection to `len(c.specs) > 0` and match specifically against known tools in `c.specs` or malformed `action:"tool"` payloads.
3. Restrict Telegram 400 fallback strictly to entity parse errors (`can't parse entities`), and add message chunking/length bounds for messages exceeding 4096 runes.
4. Correct the table-to-bullets algorithm to avoid dropping matching cell values, and properly escape language-tagged `<pre><code class="language-...">` code blocks.
5. Move all user-facing strings to English in kernel types and keep untrusted payload structures strictly typed.

---

VERDICT: FAIL

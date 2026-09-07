# QA Verification Report: Telegram Output Quality Plan v5 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `d140212` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` v5 completely resolves all findings from rounds 1–4 across both the Standards and Spec review axes:

1. **Ordered, Mutually Exclusive Classifier**: Strict `toolCallFromReply` executes raw valid protocol calls first (positive control preserved); only subsequent non-tool replies undergo single-fence stripping and schema drift inspection across `action`, `tool_id`, and `name` keys against registered tools.
2. **S7 & Architecture Compliance**: The planner executes exactly one physical provider call, consumes one `AttemptGrant`, and performs no in-band retries. Drift outcome is returned as the typed English sentinel `TOOL_SCHEMA_DRIFT(<tool>)` through the S2.3 strict-refusal path, with edge localization cleanly mapped at the Telegram/REPL presentation boundary.
3. **Pre-Wire Render Boundary Decision**: `renderHTML(original)` returns `(string, bool)`. The boolean decision is made entirely before wire dispatch: if rendered text exceeds 4096 UTF-16 code units, exceeds 90 entity tags, or contains nothing to format, `bool=false` instructs the adapter to transmit the original text without `parse_mode`. Exactly one message is transmitted; zero adapter-level fallback retries exist.
4. **Constructive HTML Validity**: Tokenizer precedence (`fence > inline code > bold`), first-match non-overlapping spans, suppression of empty formatting tags (`****`, ` `` `), and bullet-group table conversions guarantee valid Telegram HTML, verified by a property-style validator test.
5. **Cleaned Text**: All stale references to plain-text fallback resends in previous drafts have been removed.

The design is sound, safe, and ready for implementation.

---

## Standards Review

### Documented Standards Compliance

- **S7 Owner Invariant & AttemptGrant Governance (`AGENTS.md`, `ARCHITECTURE-ESSENTIALS.md` E5)**: **PASS**. The planner issues and consumes exactly one `AttemptGrant` per turn. No in-band retries, re-asks, or transport loops exist.
- **Delivery Honesty & Single-Wire Effect (`AGENTS.md` B2, `ARCHITECTURE-ESSENTIALS.md` E15)**: **PASS**. The `(string, bool)` render-boundary decision is evaluated locally prior to network transport. One wire message is dispatched per outbox row; wire-level 400 errors retain standard journal reconciliation semantics without unauthorized plain-text resends.
- **Repository Language Rule (`AGENTS.md: Language`, `RULE[user_global]`)**: **PASS**. The kernel planner returns the typed English sentinel `TOOL_SCHEMA_DRIFT(<tool>)`. Localization is strictly isolated to presentation edge handlers.
- **Journal Truth & Persistence (`ARCHITECTURE-ESSENTIALS.md` E4)**: **PASS**. The durable outbox row and journal record the original message text; HTML rendering is ephemeral and computed at the send boundary.

### Baseline Code Smells (Judgement Calls)

- **Primitive Obsession** (`PLAN-TGOUT.md:82`): Returning `(string, bool)` from `renderHTML` relies on an out-of-band convention that `false` implies sending `original`. A typed result struct (e.g. `type RenderResult struct { Text string; ParseMode string }`) would provide clearer type semantics.
- **Duplicated Presentation Logic** (`PLAN-TGOUT.md:67-68`): Mapping the `TOOL_SCHEMA_DRIFT` sentinel independently across multiple edge adapters (Telegram and REPL) could cause presentation drift; extracting a shared edge error translator is recommended during implementation.

---

## Spec Review

### Requirements Completeness & Edge Cases

- **Tool-JSON Leak Prevention**: **PASS**.
  - Valid protocol tool calls (`{"action":"tool","tool_id":...}`) execute immediately without degradation.
  - Known drift dialects (`{"action":"memory_recall"}`, `{"tool_id":"memory_recall"}`, `{"name":"memory_recall"}`) in bare or single-fenced formats trigger the typed `TOOL_SCHEMA_DRIFT` sentinel.
  - Declared limits (known-tool collisions, double-fenced JSON, mixed prose+JSON) are documented and covered by committed tests.
- **Telegram Formatting & HTML Construction**: **PASS**.
  - Markdown pipe tables convert to bullet groups before span tokenization.
  - Code fences map to `<pre>`, inline backticks to `<code>`, and bold spans to `<b>`.
  - Content within all tokens is strictly escaped (`&`, `<`, `>`).
  - Empty markers (`****`, ` `` `) emit no HTML tags, preventing Telegram 400 empty tag rejections.
  - Messages exceeding 4096 UTF-16 code units or 90 tags safely downgrade to unformatted original text before touching the wire.

---

## Top-3 Weakest Points

1. **[internal/channel/telegram/telegram.go:270-277](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L270-L277)**: The existing `telegram.go` error boundary treats any non-nil error from `a.handle` by writing the generic English fallback (`"I could not process that message..."`) to `CompleteInbound`. The edge handler wiring must catch `TOOL_SCHEMA_DRIFT` and map it to `(croatianText, nil)` before returning to `telegram.go`, ensuring the localized text is recorded in the journal outbox.
2. **[docs/PLAN-TGOUT.md:58-61](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L58-L61)**: Drift classification inspects `action`, `tool_id`, and `name` keys on an unmarshaled map. Implementation should unmarshal into a typed Go candidate struct (`struct { Action string `json:"action"`; ToolID string `json:"tool_id"`; Name string `json:"name"` }`) to satisfy repository typing guidelines (`E3`).
3. **[docs/PLAN-TGOUT.md:82-87](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L82-L87)**: The `(string, bool)` return signature requires the caller to know that `false` discards `string` in favor of `original`. While functionally correct, returning a typed struct with explicit `ParseMode` would prevent accidental misuse.

---

VERDICT: PASS

# QA Verification Report: Telegram Output Quality Plan v7 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `389d361` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` v7 completely resolves all findings from Round 6 (Codex F1–F3, Kilo F1) across both the **Standards** and **Spec** review axes:

1. **Top-Level Duplicate-Key Gate (Codex F1 / HIGH)**: Strict `toolCallFromReply` adds a top-level duplicate-member check using `contracts.HasDuplicateJSONKeys` before unmarshaling into Go structs. Any duplicate top-level key invalidates the strict tool call (falling back to schema drift classification) and prevents Go's default last-member-wins unmarshaling from inadvertently executing an ambiguous or spoofed tool call.
2. **Pipes Inside Table Cells (Codex F2 / MED)**: Any candidate GFM table block containing an escaped pipe (`\|`) or an inline backtick code span with a pipe (`` `|` ``) bypasses naive splitting and passes through escaped and verbatim. This preserves raw code snippets and regular expressions without loss or distortion ("lossless beats pretty").
3. **Budget Counting Unit Clarity (Codex F3 / LOW)**: The renderer budget counting unit is explicitly defined as opening tags (formatted spans, $\le 90$), aligning 1:1 with Telegram `MessageEntity` spans and disambiguating tag counts for the 90/91 boundary test fixtures.
4. **Rephrase-Only Edge Message (Kilo F1 / LOW)**: The localized Croatian message is pruned to `"Preformuliraj zahtjev."` (removing `"pokušaj ponovno"`), recognizing that failed turns are excluded from conversation history folding and thus have no prior context to "try again" against.

---

## Standards Review

### 1. Compliance by Section

#### Section A — Planner: Known Drift Dialects Never Ship Raw
- **Hard Violations**: 0
- **S7 Invariants & Error Classification**: **PASS**. Single provider attempt and single `AttemptGrant` consumed per planning step with no in-band retries. Drift outcome returns `contracts.TypedError` (`Code="TOOL_SCHEMA_DRIFT"`, `Category=validation`, `Retryability=NEVER`, `Origin=planner`, English `SafeMessage`). Structured `error_code` is journaled in `turn.failed` and forwarded as `code` across the UDS frame ([`contracts.go`](file:///home/matej/HARNESS/nexus/internal/kernel/contracts/contracts.go#L523-L563), `E3`).
- **Duplicate JSON Key Invariants (`HARDQ C4`, `E11`)**: **PASS**. `HasDuplicateJSONKeys` gates raw tool requests before decoding, ensuring unambiguous discriminator selection.
- **Language Invariant (AGENTS.md)**: **PASS**. Kernel and UDS error frames remain strictly in neutral English; Croatian mapping is isolated to presentation edges.
- **Conversation History Invariant (E14)**: **PASS**. History folds only `turn.succeeded` events; drifted `turn.failed` turns never enter assistant context.

#### Section B — Telegram Adapter: Outbound Rendering
- **Hard Violations**: 0
- **Delivery Honesty (B2, E15)**: **PASS**. `renderHTML(original) (string, bool)` decides `parse_mode=HTML` before wire dispatch. Original content is preserved unchanged in the journal and transactional outbox. Zero multi-attempt adapter fallbacks exist on the wire.
- **Fail-Closed Parser & Complexity Budget**: **PASS**. Precedence (`fence > inline code > bold`), tag suppression on empty spans (`****`), post-parse UTF-16 bounds ($\le 4096$), and opening tag budget ($\le 90$ spans) prevent malformed or over-budget messages from reaching Telegram.

### 2. Baseline Smells & Judgement Calls
- **Primitive Obsession (Judgement Call — Clean / Idiomatic)**: Returning `(string, bool)` at the send boundary is standard Go comma-ok idiom for choosing between formatted and unformatted wire transmission.
- **Speculative Generality (Clean)**: Reuses existing `HasDuplicateJSONKeys` owner; chunking/multipart is explicitly deferred to non-goals.

---

## Spec Review

### 1. Requirements & Problem Statement Coverage
- **Raw Tool JSON Leak**: **PASS**. Positive control preserves bare protocol calls; duplicate keys and single-fence stripped drifts are caught by the classifier; undeclared dialects and mixed prose/JSON are bounded by declared limits with committed tests.
- **Telegram Formatting & Table Transformation**: **PASS**. GFM tables convert to bullet groups before span tokenization. Balanced `<b>`, `<code>`, `<pre>` tags surround fully escaped text (`&`, `<`, `>`). Escaped pipes and backtick spans with pipes pass through verbatim.
- **Round-6 Findings Closure**:
  - Codex F1 (Top-level duplicate keys): Closed ([`PLAN-TGOUT.md:59-67`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L59-L67)).
  - Codex F2 (Pipes in cell content): Closed ([`PLAN-TGOUT.md:128-132`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L128-L132)).
  - Codex F3 (Span counting unit): Closed ([`PLAN-TGOUT.md:133-134`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L133-L134)).
  - Kilo F1 (Rephrase-only edge prompt): Closed ([`PLAN-TGOUT.md:85-88`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L85-L88)).

### 2. Scope Creep
- **None**. Multipart/chunking, Bot API 10.1 `sendRichMessage`, full Markdown AST parsing, and provider-native function calling remain deferred under Non-goals ([`PLAN-TGOUT.md:150-157`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L150-L157)).

---

## Top-3 Weakest Points

1. **Telegram Inbound Handler Error Mapping ([`docs/PLAN-TGOUT.md:78-79`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L78-L79), [`telegram.go:270-277`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L270-L277))**:
   - *Evidence*: `telegram.go`'s inbound loop catches `if herr != nil` from `a.handle` and writes the generic English string `"I could not process that message. Try again, or check the daemon log."`.
   - *Implementation Note*: During implementation of Section A, the daemon channel turn handler must map `TOOL_SCHEMA_DRIFT` to `("Preformuliraj zahtjev.", nil)` before returning to `telegram.go`, ensuring `CompleteInbound` enqueues the Croatian reply into the outbox rather than tripping `telegram.go`'s error fallback.

2. **Empty Header Bullets in Table Transformation ([`docs/PLAN-TGOUT.md:28-30`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L30))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Implementation Note*: If a table column header is empty (e.g. `| | Col B |`), the bullet formatting should use `'—'` or column index fallback (e.g. `• —: value`) to avoid emitting malformed bullet text like `• : value`.

3. **Short-Circuit on Plain Prose with HTML Special Characters ([`docs/PLAN-TGOUT.md:120`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L120))**:
   - *Evidence*: `rendering found nothing to format (plain prose short-circuits)`.
   - *Implementation Note*: Plain prose without markdown styling that contains `&`, `<`, or `>` must short-circuit to `(original, false)` rather than escaping characters and sending with `parse_mode=HTML`, keeping wire output byte-identical to existing behavior.

---

VERDICT: PASS

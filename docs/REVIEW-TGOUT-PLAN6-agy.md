# QA Verification Report: Telegram Output Quality Plan v6 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `f7dad76` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` v6 successfully resolves all outstanding findings from Round 5 (Codex F1–F5, Kilo F1–F2) across both the **Standards** and **Spec** review axes:

1. **Local Complexity Budget Honesty**: The $\le 90$ tag threshold is explicitly scoped as a local renderer-complexity budget rather than an undocumented Telegram API entity limit. The design openly acknowledges that auto-detected entities (such as URLs) are not counted.
2. **Canonical TypedError Lifecycle**: `TOOL_SCHEMA_DRIFT` is fully specified as `contracts.TypedError` (`Code="TOOL_SCHEMA_DRIFT"`, `Category=validation`, `Retryability=NEVER`, `Origin=planner`, `SafeMessage` in English naming the tool). The turn cleanly lands `TERMINAL-FAILED`: `turn.failed` payload gains an `error_code` field (structured journal logging), and the UDS error frame gains a `code` field so REPL/Telegram presentation layers map without fragile string scraping. Failed turns are explicitly excluded from conversation history folding.
3. **Deterministic Field Precedence**: Conflicting tool candidate keys are resolved deterministically via `action > tool_id > name`, backed by committed detector tests.
4. **Deliberate Deviations Clarified**: The distinction between Hermes' lossy table rules (padding/truncation) and NEXUS's lossless rules (`'—'` for empty cells, `'(extra)'` for surplus cells) is firmly established. The reference section clarifies that Hermes' plain resend describes Hermes only, whereas NEXUS uses a single-attempt pre-wire `(string, bool)` decision.

---

## Standards Review

### 1. Compliance by Section

#### Section A — Planner: Known Drift Dialects Never Ship Raw
- **Hard Violations**: 0
- **S7 Invariants & Error Classification**: **PASS**. The planner issues exactly one `AttemptGrant` per provider call with zero in-band retry loops. Drift returns `contracts.TypedError` with `Retryability: NEVER`, terminating the turn cleanly via `turn.failed`. Structured `error_code` in journal payload and `code` in UDS frame uphold kernel typing rules ([`contracts.go`](file:///home/matej/HARNESS/nexus/internal/kernel/contracts/contracts.go#L523-L563), `E3`).
- **Language Invariant (AGENTS.md)**: **PASS**. Kernel protocol and error messages are neutral English. Croatian mapping is strictly isolated to the presentation edges (Telegram adapter and REPL).
- **Conversation History Invariant (E14)**: **PASS**. History folds only `turn.succeeded` events; drifted `turn.failed` turns never pollute context memory.

#### Section B — Telegram Adapter: Outbound Rendering
- **Hard Violations**: 0
- **Delivery Honesty (B2, E15)**: **PASS**. Outbound rendering evaluates `renderHTML(original) (string, bool)` before wire dispatch. Journal and outbox store the unmutated original text. Exactly one message is transmitted; zero adapter-level resend loops exist.
- **Fail-Closed Parser**: **PASS**. Strict precedence (`fence > inline code > bold`), tag suppression on empty spans (`****`), and post-parse UTF-16 bounds ($\le 4096$) prevent malformed HTML from reaching the wire.

### 2. Baseline Smells & Judgement Calls
- **Primitive Obsession (Judgement Call)**: The return signature `renderHTML(original) (string, bool)` uses a bare boolean to select between `parse_mode=HTML` and unformatted text. An explicit struct or enum (e.g. `RenderResult{Text string, Mode ParseMode}`) would be slightly more descriptive, though `(string, bool)` is standard Go comma-ok idiom.
- **Shotgun Surgery (Judgement Call — Justified)**: Adding structured `error_code` touches `loop.go`, `daemon.go` UDS frames, and presentation handlers. This cross-boundary addition is necessary to eliminate string pattern matching across layers.

---

## Spec Review

### 1. Requirements & Problem Statement Coverage
- **Raw Tool JSON Leak**: **PASS**. Strict protocol parser executes first (positive control); single-fence stripped drift detector intercepts `action`/`tool_id`/`name` matching registered tools; unknown dialects and mixed text are declared limits with committed tests.
- **Telegram Table & Text Formatting**: **PASS**. GFM tables convert to bullet groups before span tokenization. Balanced `<b>`, `<code>`, `<pre>` tags surround fully escaped text (`&`, `<`, `>`). Empty markers emit no tags, avoiding Telegram 400 empty-tag rejections.
- **Round-5 Findings Closure**:
  - Codex F1 / Kilo F1 (90-tag limit honesty): Closed ([`PLAN-TGOUT.md:105-110`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L105-L110)).
  - Codex F2 / Kilo F2 (`TOOL_SCHEMA_DRIFT` lifecycle): Closed ([`PLAN-TGOUT.md:66-76`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L66-L76)).
  - Codex F3 / Kilo F1 (Hermes fallback contradiction): Closed ([`PLAN-TGOUT.md:41-45, 123-126`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L41-L45)).
  - Codex F4 (Lossless table rules): Closed ([`PLAN-TGOUT.md:30-32, 45-48`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L30-L32)).
  - Codex F5 (Field precedence): Closed ([`PLAN-TGOUT.md:77-79`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L77-L79)).

### 2. Scope Creep
- **None**. Chunking/multipart, function calling, and full Markdown AST parsing remain explicitly out of scope under Non-goals ([`PLAN-TGOUT.md:134-141`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L134-L141)).

---

## Top-3 Weakest Points

1. **Telegram Outbox Error Seam ([`docs/PLAN-TGOUT.md:72-74`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L72-L74), [`telegram.go:270-277`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L270-L277))**:
   - *Evidence*: `telegram.go`'s poll loop checks `if err != nil` from `a.handle` and overwrites `reply` with the English string `"I could not process that message. Please try again."`.
   - *Risk*: When implementing Section A, the daemon channel turn handler must map `TOOL_SCHEMA_DRIFT` to `(croatianText, nil)` so `CompleteInbound` records the localized Croatian text in the outbox rather than tripping `telegram.go`'s fallback.

2. **Empty Column Header Formatting ([`docs/PLAN-TGOUT.md:28-32`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Risk*: If a table has an empty column header cell, bullet formatting could produce `• : value` or `• —: value`. The table converter should use `'—'` or column index fallback to preserve lossless formatting.

3. **Short-Circuit on Raw HTML Characters ([`docs/PLAN-TGOUT.md:111`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L111))**:
   - *Evidence*: `rendering found nothing to format (plain prose short-circuits)`.
   - *Risk*: Plain prose without markdown styling that contains `&`, `<`, or `>` must short-circuit to `(original, false)` rather than escaping characters and sending with `parse_mode=HTML`, keeping wire output byte-identical to current behavior.

---

VERDICT: PASS

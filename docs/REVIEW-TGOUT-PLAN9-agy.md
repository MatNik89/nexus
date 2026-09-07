# QA Verification Report: Telegram Output Quality Plan v9 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `b2a2359` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `b2a2359` (v9) completely resolves all findings from Round 8 across both the **Standards** and **Spec** review axes:

1. **Narrowed Duplicate/Alias Drift Token-Walk (Kilo F1 Closure)**: The drift token scan is narrowed to collect only the values of keys `action`, `tool_id`, and `name` (evaluated case-insensitively across all member positions, including duplicates). Member values under unrelated keys are ignored: legitimate answers mentioning tool names in other keys (e.g., `{"description":"memory_recall"}`) stay prose (committed test), while adverse duplicates targeting tool-invoking keys (e.g., `{"action":"memory_recall","action":"tool"}`) route deterministically to `TOOL_SCHEMA_DRIFT` (committed test).
2. **Harmonized Wire Contract & Absolute Wording Removal (Codex LOW / Kilo F2 Closure)**: The contradictory "no second attempt ever exists" phrasing is replaced with precise, single-send tick semantics: "ONE message either way per flush tick, decided BEFORE the wire... recovery across TICKS is the attempts rule".
3. **Journal-Derived Outbox Attempts & Deterministic Rebuilds (Codex MED Closure)**: Outbox attempt tracking is strictly derived from the canonical journal by having `channel.Projection.Apply` fold the existing `channel.outbound_unknown` event into an `attempts` counter in `chan_outbox`. This preserves the single-write-owner invariant (no out-of-band SQL mutations), bumps `Projection.Version()` from `1` to `2` for clean schema migration and deterministic replay, and includes a full two-flush fake-bot 400 detector with a red-capable ablation.

---

## Standards Compliance

### 1. Delivery Honesty & Single-Write-Owner Commit Boundaries (HARDQ B2/B7, E4, E15)
- **Status**: **PASS**
- **Analysis**: The `attempts` count is managed entirely within the journal projection fold (`EvOutboundEnqueued` initializes `attempts = 0`, `EvOutboundUnknown` increments `attempts = attempts + 1`).
- **Wire Invariants**: Exactly one wire transmission occurs per flush tick per outbox row. When Telegram returns an entity-parse 400 rejection (definite failure), `Core.Flush` reconciles the delivery row back to `PENDING`. On the subsequent periodic flush tick (2 seconds later), `attempts > 0` causes `telegram.FlushOutbox` to transmit the unformatted original plain text without `parse_mode`. Ambiguous errors (`5xx`, network drops) remain safely parked in `UNKNOWN` for reconciliation.

### 2. S7 Governance & Error Classification (S7.1, Annex P0.1, E11)
- **Status**: **PASS**
- **Analysis**: Schema drift consumes exactly one `AttemptGrant` and emits canonical `contracts.TypedError`:
  - `Code`: `"TOOL_SCHEMA_DRIFT"`
  - `Category`: `ErrCatValidation`
  - `Retryability`: `RetryNever`
  - `Origin`: `"planner"`
  - `SafeMessage`: Neutral English string naming the target tool
- **Terminal Outcome**: Drift turns land `TERMINAL-FAILED` (`turn.failed` journal payload with structured `error_code`, surfaced on UDS via `code` field).

### 3. Closed Key Contract & Typing Rules (E3, E11)
- **Status**: **PASS**
- **Analysis**: Tool calls require exactly the lowercase keys `{"action", "tool_id", "arguments"}` (arguments optional). Case-insensitive duplicate detection and non-lowercase/unknown key rejection on raw tokens eliminate Go `encoding/json` struct unmarshaling last-member-wins vulnerabilities.

### 4. Language Constitution (AGENTS.md)
- **Status**: **PASS**
- **Analysis**: Kernel error codes, payloads, safe messages, and journal records are strictly in English. Croatian user copy (`"Preformuliraj zahtjev."`) is confined solely to edge presentation adapters (Telegram and REPL).

### 5. Conversation History Invariants
- **Status**: **PASS**
- **Analysis**: Drifted turns record `turn.failed` and never emit `turn.succeeded`. `conversationHistory` filters strictly on completed turns (`pairs[id].completed`), ensuring malformed tool outputs do not pollute conversation history.

---

## Spec Alignment & Attack Analysis

### 1. Verification of Round-8 Finding Closures
- **Narrowed Drift Walk**: Closed ([`docs/PLAN-TGOUT.md:71-80`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L71-L80)). Limits drift classification to `action`/`tool_id`/`name` values, eliminating false positives on legitimate prose payloads like `{"description":"memory_recall"}`.
- **Wire Contract Consistency**: Closed ([`docs/PLAN-TGOUT.md:113-120`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L113-L120)). Formulates single-send per tick with cross-tick recovery.
- **Journal-Derived Attempt Tracking**: Closed ([`docs/PLAN-TGOUT.md:121-136`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L121-L136)). Folds `EvOutboundUnknown` into `attempts`, bumps `Projection.Version()` to `2`, and locks behavior with a two-flush detector and ablation.

### 2. Preserved Invariants
- **Local Tag Budget**: Opening tag / span count bounded at $\le 90$ with boundary tests at 90/91 ([`docs/PLAN-TGOUT.md:144-149,163-164`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L144-L149)).
- **Table Formatting**: GFM pipe tables convert to bullet groups with lossless `'—'` (empty) and `'(extra)'` (surplus) rules. Pipe-in-cell blocks pass through escaped and verbatim ([`docs/PLAN-TGOUT.md:26-32,158-162`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L26-L32)).
- **Edge Copy**: Croatian rephrase message is strictly `"Preformuliraj zahtjev."` without ungrounded retry directives ([`docs/PLAN-TGOUT.md:96-101`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L96-L101)).

---

## Top-3 Weakest Points

1. **Outbound Struct Projection Exposure & Snapshot Timing ([`docs/PLAN-TGOUT.md:121-128`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L121-L128), [`internal/channel/channel.go:77-83,509-514`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L77-L83))**:
   - *Evidence*: `channel.Outbound` struct in `channel.go:77-83` must expose `Attempts int` populated from `chan_outbox.attempts` by `Core.Pending()` (`attempts == 0` on initial load).
   - *Implementation Note*: In `Core.Flush` (`channel.go:509-514`), `EvOutboundUnknown` marks the row in-flight and increments `attempts` before `send(o)` is invoked. The snapshot `o` passed into `send(o)` should reflect the state read at `Pending()` (`o.Attempts == 0`), ensuring `telegram.FlushOutbox` transmits formatted HTML on initial attempt and suppresses formatting only on subsequent tick re-flushes (`Attempts >= 1`).

2. **Telegram Inbound Handler Error Mapping ([`docs/PLAN-TGOUT.md:88-92`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L88-L92), [`internal/channel/telegram/telegram.go:270-277`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L270-L277))**:
   - *Evidence*: `telegram.go:270-277` catches `herr != nil` from `a.handle(ctx, in)` and falls back to `"I could not process that message. Try again, or check the daemon log."`.
   - *Implementation Note*: The channel turn handler in the daemon must map `contracts.TypedError` with `Code == "TOOL_SCHEMA_DRIFT"` to `("Preformuliraj zahtjev.", nil)` before returning to `telegram.go`, ensuring `CompleteInbound` enqueues the localized Croatian text into the outbox.

3. **Empty Table Column Header Bullets ([`docs/PLAN-TGOUT.md:28-32`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Implementation Note*: If a table column header is empty (e.g., `| | Col B |`), the bullet formatter should emit `• —: value` instead of `• : value` to prevent empty header label formatting anomalies.

---

VERDICT: PASS

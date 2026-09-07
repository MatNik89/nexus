# QA Verification Report: Telegram Output Quality Plan v8 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `8f8bc4c` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `8f8bc4c` fully resolves all findings from Round 7 across both the **Standards** and **Spec** review axes:

1. **Closed Key Contract for Tool Calls (Codex HIGH Closure)**: Strict `toolCallFromReply` enforces a closed contract requiring exactly the lowercase top-level keys `{"action", "tool_id", "arguments"}` (with `arguments` optional). Verification is performed via a token-level scan with case-insensitive duplicate checking. Any unknown key, uppercase/mixed-case key (e.g., `"Action"`), or casing alias (e.g., `{"action":"x","Action":"tool"}`) is rejected fail-closed before Go struct unmarshaling, eliminating Go `encoding/json` last-member-wins decoder ambiguities.
2. **Adverse Duplicate Routing to Drift (Kilo F1 Closure)**: When the closed-contract check rejects, the drift classifier avoids struct re-decoding (which would hide earlier values under last-member-wins). Instead, the same token-level stream walk inspects all top-level string values. If any top-level key/value identifies a registered tool (e.g., `{"action":"memory_recall","action":"tool"}`), the reply routes directly to `TOOL_SCHEMA_DRIFT` rather than leaking as prose.
3. **First-Attempt-Only Formatting & Self-Healing Flushes (Codex MED Closure)**: The `chan_outbox` projection gains an additive `attempts` counter. Initial flush attempts (`attempts == 0`) send formatted HTML with `parse_mode=HTML`. If Telegram returns a 400 parse rejection, the channel core re-pends the delivery row as a definite pre-wire failure. On subsequent periodic flush ticks (2 seconds later), `attempts > 0` causes `FlushOutbox` to transmit the unformatted original plain text without `parse_mode`. This self-heals remote parser rejections without intra-tick wire retries, multi-send fallback paths, or message loss, preserving single-send wire invariants and at-least-once delivery honesty (B2).

---

## Standards Compliance

### 1. Delivery Honesty & Commit Boundaries (HARDQ B2/B7, E4, E15)
- **Status**: **PASS**
- **Analysis**: The `attempts` column in `chan_outbox` is purely additive and updated strictly within event projection application (`EvOutboundEnqueued`, `EvOutboundUnknown`, `EvOutboundResolved`).
- **Wire Invariants**: Exactly one wire transmission occurs per flush tick per outbox row. A 400 error marks the in-flight delivery as a definite failure and returns it to `PENDING`. On subsequent flush ticks, `attempts > 0` sends plain text. No second send or retry loop is executed within a single flush tick. Ambiguous network failures (`5xx`, timeouts) remain durably parked in `UNKNOWN` for reconciliation.

### 2. S7 Governance & Error Classification (S7.1, Annex P0.1, E11)
- **Status**: **PASS**
- **Analysis**: Schema drift continues to consume exactly one `AttemptGrant` and returns a canonical `contracts.TypedError`:
  - `Code`: `"TOOL_SCHEMA_DRIFT"`
  - `Category`: `ErrCatValidation`
  - `Retryability`: `RetryNever`
  - `Origin`: `"planner"`
  - `SafeMessage`: Neutral English string naming the target tool
- **Terminal Outcome**: Drift yields a terminal turn failure recorded as `turn.failed` in the journal with structured `error_code` and surfaced across UDS via the `code` field.

### 3. Closed Key Contract & Typing Rules (E3, E11)
- **Status**: **PASS**
- **Analysis**: Tool call validation enforces exact lowercase member names and case-insensitive duplicate rejection at the token level, eliminating Go struct unmarshaling last-member-wins vulnerabilities.

### 4. Language Constitution (AGENTS.md)
- **Status**: **PASS**
- **Analysis**: Kernel error codes, payloads, safe messages, and journal records are strictly in English. Croatian user copy (`"Preformuliraj zahtjev."`) is confined solely to edge presentation adapters (Telegram and REPL).

### 5. Conversation History Invariants
- **Status**: **PASS**
- **Analysis**: Drifted turns record `turn.failed` and never emit `turn.succeeded`. `conversationHistory` filters strictly on completed turns (`pairs[id].completed`), ensuring malformed tool outputs do not enter conversation history.

---

## Spec Alignment & Attack Analysis

### 1. Verification of Round-7 Finding Closures
- **Codex HIGH (Case-Insensitive Duplicate Aliases)**: Closed ([`docs/PLAN-TGOUT.md:59-70`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L59-L70)). Token-level walk validates top-level keys case-insensitively before Go `json.Unmarshal`.
- **Kilo F1 (Adverse Duplicate Drift Routing)**: Closed ([`docs/PLAN-TGOUT.md:71-77`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L71-L77)). Token walk scans all top-level string values to route adverse duplicates to `TOOL_SCHEMA_DRIFT`.
- **Codex MED (400 Re-flush Loop)**: Closed ([`docs/PLAN-TGOUT.md:117-127`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L117-L127)). Outbox `attempts` counter differentiates initial formatted send (`attempts == 0`) from subsequent plain re-flushes (`attempts > 0`), ensuring convergence.

### 2. Preserved Invariants
- **Local Tag Budget**: Opening tag / span count bounded at $\le 90$ with boundary tests at 90/91 ([`docs/PLAN-TGOUT.md:135-140,154-155`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L135-L140)).
- **Table Formatting**: GFM pipe tables convert to bullet groups with lossless `'—'` (empty) and `'(extra)'` (surplus) rules. Pipe-in-cell blocks pass through escaped and verbatim ([`docs/PLAN-TGOUT.md:26-32,149-153`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L26-L32)).
- **Edge Copy**: Croatian rephrase message is strictly `"Preformuliraj zahtjev."` without ungrounded retry directives ([`docs/PLAN-TGOUT.md:88-98`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L88-L98)).

---

## Top-3 Weakest Points

1. **Outbox Projection Versioning & Replay Logic ([`docs/PLAN-TGOUT.md:120-123`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L120-L123), [`internal/channel/channel.go:181-204`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L181-L204), [`internal/channel/channel.go:287-290`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L287-L290))**:
   - *Evidence*: `chan_outbox` schema in `channel.go:195-202` gains `attempts INTEGER NOT NULL DEFAULT 0`.
   - *Implementation Note*: Adding `attempts` requires bumping `Projection.Version()` from `1` to `2` so SQLite tables reset cleanly on migration. Additionally, in `channel.go:287-290`, `EvOutboundResolved` (with `Proved=false` indicating re-pending after definite failure) must increment `attempts = attempts + 1` to ensure replay determinism.

2. **Telegram Inbound Handler Error Mapping ([`docs/PLAN-TGOUT.md:88-89`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L88-L89), [`internal/channel/telegram/telegram.go:270-277`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L270-L277))**:
   - *Evidence*: `telegram.go:270-277` catches `herr != nil` from `a.handle(ctx, in)` and falls back to `"I could not process that message. Try again, or check the daemon log."`.
   - *Implementation Note*: The channel turn handler in the daemon must map `contracts.TypedError` with `Code == "TOOL_SCHEMA_DRIFT"` to `("Preformuliraj zahtjev.", nil)` before returning to `telegram.go`, ensuring `CompleteInbound` enqueues the localized Croatian text into the outbox.

3. **Empty Table Column Header Bullets ([`docs/PLAN-TGOUT.md:28-32`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Implementation Note*: If a table column header is empty (e.g., `| | Col B |`), the bullet formatter should emit `• —: value` instead of `• : value` to prevent empty header label formatting anomalies.

---

VERDICT: PASS

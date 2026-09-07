# QA Verification Report: Telegram Output Quality Plan v12 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `aafd411` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Commit**: `aafd411f5bb106c5476a6d63bb1296fdfd5b5120` (`docs(tgout): plan v12 — narrowed recovery contract (code+tool), explicit recovery helper mechanism`)  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `aafd411` (v12) completely and cleanly resolves both findings from Round 11 across the **Standards** and **Spec** review axes:

1. **Narrowed Recovery Contract (Round-11 LOW #1 Closure)**:
   - The restart recovery contract is honestly and precisely narrowed: `turn.failed` persists `error_code` AND `tool` (the precedence winner) (`docs/PLAN-TGOUT.md:98-104`).
   - The contract makes the exact claim: *same validated error code + tool $\rightarrow$ same edge response after restart*. It explicitly documents that `Category`, `Origin`, and `Retryability` are reconstructed constants for this code, avoiding any false claims of field-for-field `TypedError` struct equality.
   - Dual red-capable ablations are specified: dropping failed-turn recovery entirely $\rightarrow$ RED; dropping the persisted `tool` field $\rightarrow$ the tool name vanishes from the edge message $\rightarrow$ RED (`docs/PLAN-TGOUT.md:110-112`).

2. **Explicit Recovery Helper Mechanism (Round-11 LOW #2 Closure)**:
   - Rather than overloading the string-typed `completedTurnFinal` helper, the plan specifies a dedicated recovery helper `failedTurnOutcome(turn) (code, tool, ok)` (`docs/PLAN-TGOUT.md:104-108`).
   - On turn collision in `RunChannelTurn`, `failedTurnOutcome` is consulted after checking succeeded/suspended states. `RunChannelTurn` reconstructs the typed error and returns it to the edge handler (`telegram.go`), where it is mapped to the Croatian reply (`"Preformuliraj zahtjev."`) identically to the live path without re-invoking the planner.

3. **Preserved Invariants & Soundness Controls**:
   - **Single-Fence Duplicate/Alias Routing**: Closed key contract `{action, tool_id, arguments}` with case-insensitive token-walk duplicate detection runs across both raw replies and normalized classification views (`fenced duplicate -> drift`; `docs/PLAN-TGOUT.md:80-82, 118-122`).
   - **Bytes-Only Wire Contract**: Adds no attempt and changes no scheduling; ungranted tick re-flush pump is isolated to `docs/tasks-P0.md:464-472` (`docs/PLAN-TGOUT.md:138-145`).
   - **First-Lease-Only Formatting**: Formatting is applied solely on the first in-flight lease (`outbound_unknown` count == 0); pre-wire failures safely degrade subsequent tick re-flushes to plain text (`docs/PLAN-TGOUT.md:146-158`).
   - **Durability & Rebuild Detectors**: Full detector coverage for restart persistence (close/reopen), version-bump projection rebuild (`Projection.Version() = 2`), dial failure, and dual red-capable ablations (`ignore-the-count` RED, `drop-the-event-fold` RED; `docs/PLAN-TGOUT.md:159-168`).
   - **Constructive HTML Generation**: Strict tag budget ($\le 90$ opening spans), 4096 UTF-16 unit limit, verbatim pass-through for escaped pipes / backticks, lossless table bullet conversion (`docs/PLAN-TGOUT.md:28-32, 169-196`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **Replay Honesty & Idempotency ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The separation between `completedTurnFinal` (for string-typed finals from `turn.succeeded` / `approval.turn_suspended`) and `failedTurnOutcome` (for structured failure outcomes from `turn.failed`) maintains clear typing boundaries.
  - Replaying an admitted update after a crash between `turn.failed` and `CompleteInbound` cleanly reconstructs the typed error, enqueueing the identical Croatian message without corrupting turn history.
- **S7 AttemptGrant Invariants & Outbox Pump Scope ([`AGENTS.md` S7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:68-75`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The slice adds zero attempts and preserves existing delivery mechanics byte-for-byte. The pre-existing ungranted outbox tick pump is formally decoupled into `docs/tasks-P0.md:464-472`.
- **Fail-Closed Typing & Language Boundaries ([`AGENTS.md` Language & HARDQ C7](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Kernel errors remain typed English sentinels (`TOOL_SCHEMA_DRIFT`); user-facing Croatian localization (`"Preformuliraj zahtjev."`) is strictly isolated to the adapter edge.

### 2. Spec Axis (Requirements & Edge Cases)

- **Dogfood Findings Resolution**:
  - **Tool-JSON Leak Guard**: Strict closed contract + token-walk drift classifier eliminates model schema drift leaks to the user.
  - **Telegram Formatting**: Send-boundary HTML rendering converts GFM tables to bullet groups and escapes markup safely with local tag and size budgets.
- **Detector Completeness**:
  - The detector suite covers all positive controls, drift dialects (bare and single-fenced with duplicates), declared limits, crash seams, restart persistence, version-bump rebuilds, and required causal ablations.

---

## Top-3 Weakest Points & Implementation Guardrails

In accordance with the mandatory review discipline, the top-3 weakest points of the design and their implementation guardrails are identified below:

1. **`failedTurnOutcome` Journal Replay Loop Scope ([`docs/PLAN-TGOUT.md:104-108`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L104-L108), [`internal/app/daemon/daemon.go:263-281,411-446`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L263-L281))**:
   - *Evidence*: `failedTurnOutcome` and `completedTurnFinal` both scan the journal via `Replay(0, ...)`.
   - *Guardrail*: `failedTurnOutcome` must follow the same fail-closed integrity rules as `completedTurnFinal` (`daemon.go:439-445`): an unmarshal error or journal corruption error during replay must fail closed and propagate the integrity error rather than returning `ok = false`.

2. **Snapshot Timing for `Outbound.Attempts` in Outbox Flush ([`docs/PLAN-TGOUT.md:146-149`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L146-L149), [`internal/channel/channel.go:77-83,509-514`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L77-L83))**:
   - *Evidence*: `Core.Flush` iterates over `pending` rows and invokes `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` *before* calling `send(o)`.
   - *Guardrail*: `channel.Outbound` must expose `Attempts int` populated during `c.Pending(ctx)` snapshot from `chan_outbox.attempts`. `send(o)` must inspect `o.Attempts == 0` (the pre-parking count) so the initial send carries formatted HTML, rather than querying the database post-park where `attempts == 1`.

3. **Empty Table Header Bullet Formatting ([`docs/PLAN-TGOUT.md:28-32, 173-175`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Guardrail*: If a table header cell is empty (e.g. `| | Metric |`), the bullet renderer must fall back to placeholder `'—'` (`• —: value`) rather than emitting malformed `• : value` bullet strings.

---

VERDICT: PASS

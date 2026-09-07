# QA Verification Report: Telegram Output Quality Plan v16 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `df06ed5` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Commit**: `df06ed5ef3772ee785718df2e20b33230489cf8b` (`docs(tgout): plan v16 — honest render-risk statement (deferral, not formatting-only)`)  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `df06ed5` (v16) completely and cleanly resolves all architectural and specification findings across both the **Standards** and **Spec** review axes:

1. **Honest Render-Risk & Eventual Delivery Latency (Codex r15 LOW Closure)**:
   - The render-risk assessment is precisely stated: a Telegram 400 parse rejection costs *formatting and delivery latency* (`docs/PLAN-TGOUT.md:248-253`).
   - The rejected formatted send is re-pended by existing machinery and delivered plain on the next regular flush tick; the committed parse-400 detector provides the formal test bound proving eventual plain delivery.
   - The durable payload bytes (original text) remain strictly independent of the renderer, preserving B2 at-least-once delivery honesty.

2. **Preserved Invariants & Soundness Controls**:
   - **Carrier in Loop Package**: `loop.DriftError{Typed contracts.TypedError; Tool contracts.ToolID}` preserves an acyclic dependency graph without string parsing (`docs/PLAN-TGOUT.md:104-113`).
   - **Unified Recovery Helper**: `recoveredTurnOutcome(turn) (RecoveredOutcome, bool, error)` with closed sum `Kind {SUCCEEDED|SUSPENDED|FAILED}` performs a single state-aware replay pass and propagates errors fail-closed (`docs/PLAN-TGOUT.md:114-125`).
   - **Non-Drift `turn.failed` Handling**: Ordinary failures return `Kind=FAILED` with empty `Code`, suppressing stale suspensions while preserving today's collision-error path without inventing synthetic responses (`docs/PLAN-TGOUT.md:126-136`).
   - **Within-Tier Source-Token Precedence**: Duplicate same-field members naming different registered tools resolve deterministically by first occurrence in source token order (`docs/PLAN-TGOUT.md:137-143`).
   - **Single-Fence Duplicate/Alias Routing**: Closed key contract `{action, tool_id, arguments}` with case-insensitive token-walk duplicate detection runs across both raw replies and normalized classification views (`docs/PLAN-TGOUT.md:80-82, 162-166`).
   - **Bytes-Only Wire Contract**: Adds zero attempts and preserves existing delivery mechanics; ungranted tick re-flush pump is isolated to `docs/tasks-P0.md:464-472` (`docs/PLAN-TGOUT.md:176-183`).
   - **First-Lease-Only Formatting**: Formatting is applied solely on the first in-flight lease (`outbound_unknown` count == 0); pre-wire failures safely degrade subsequent tick re-flushes to plain text (`docs/PLAN-TGOUT.md:184-196`).
   - **Durability & Rebuild Detectors**: Full detector coverage for restart persistence (close/reopen), version-bump projection rebuild (`Projection.Version() = 2`), dial failure, mixed lifecycles, and all required causal ablations (`docs/PLAN-TGOUT.md:144-150, 197-206`).
   - **Constructive HTML Generation**: Strict tag budget ($\le 90$ opening spans), 4096 UTF-16 unit limit, verbatim pass-through for escaped pipes / backticks, lossless table bullet conversion (`docs/PLAN-TGOUT.md:28-32, 207-234`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **Delivery Honesty & Risk Precision ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The updated risk statement accurately describes the cross-tick recovery mechanism without overclaiming "formatting-only" costs, upholding delivery honesty.
- **Replay Honesty & Idempotency ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - `recoveredTurnOutcome` unifies recovery into a single $O(N)$ journal scan pass with explicit closed-sum outcomes, guaranteeing that replayed updates never re-invoke the planner or yield stale suspension challenges after resume.
- **Package Architecture & Clean Dependency DAG ([`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - `loop.DriftError` defined in `internal/kernel/loop` ensures strict unidirectional dependency (`planner -> loop -> contracts`).
- **S7 AttemptGrant Invariants & Outbox Pump Scope ([`AGENTS.md` S7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:68-75`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The slice adds zero attempts and preserves existing delivery mechanics byte-for-byte. The pre-existing ungranted outbox tick pump is formally decoupled into `docs/tasks-P0.md:464-472`.
- **Fail-Closed Typing & Language Boundaries ([`AGENTS.md` Language & HARDQ C7](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Kernel errors remain typed English sentinels (`TOOL_SCHEMA_DRIFT`); user-facing Croatian localization (`"Preformuliraj zahtjev."`) is strictly isolated to the adapter edge.

### 2. Spec Axis (Requirements & Edge Cases)

- **Dogfood Findings Resolution**:
  - **Tool-JSON Leak Guard**: Strict closed contract + token-walk drift classifier eliminates model schema drift leaks to the user.
  - **Telegram Formatting**: Send-boundary HTML rendering converts GFM tables to bullet groups and escapes markup safely with local tag and size budgets.
- **Detector Completeness**:
  - The detector suite covers all positive controls, drift dialects (bare and single-fenced with duplicates), declared limits, mixed lifecycles (`suspended -> resumed -> failed`), ordinary failures, crash seams, restart persistence, version-bump rebuilds, and required causal ablations.

---

## Top-3 Weakest Points & Implementation Guardrails

In accordance with the mandatory review discipline, the top-3 weakest points of the design and their implementation guardrails are identified below:

1. **Snapshot Timing for `Outbound.Attempts` in Outbox Flush ([`docs/PLAN-TGOUT.md:184-187`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L184-L187), [`internal/channel/channel.go:77-83,509-514`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L77-L83))**:
   - *Evidence*: `Core.Flush` iterates over `pending` rows and invokes `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` *before* calling `send(o)`.
   - *Guardrail*: `channel.Outbound` must expose `Attempts int` populated during `c.Pending(ctx)` snapshot from `chan_outbox.attempts`. `send(o)` must inspect `o.Attempts == 0` (the pre-parking count) so the initial send carries formatted HTML, rather than querying the database post-park where `attempts == 1`.

2. **`RecoveredOutcome` Zero-Value Discipline ([`docs/PLAN-TGOUT.md:116-118, 126-133`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L116-L118), [`internal/app/daemon/daemon.go:411-446`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L411-L446))**:
   - *Evidence*: `RecoveredOutcome` uses a tagged struct containing `Kind`, `Final`, `Code`, and `Tool`.
   - *Guardrail*: In `recoveredTurnOutcome`, fields not applicable to the resolved `Kind` (e.g., `Final` on `Kind == FAILED`, or `Code`/`Tool` on `Kind == SUCCEEDED` or non-drift `Kind == FAILED`) must remain explicit zero values to prevent state leakage across outcome branches.

3. **Empty Table Header Bullet Formatting ([`docs/PLAN-TGOUT.md:28-32, 211-213`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Guardrail*: If a table header cell is empty (e.g. `| | Metric |`), the bullet renderer must fall back to placeholder `'—'` (`• —: value`) rather than emitting malformed `• : value` bullet strings.

---

VERDICT: PASS

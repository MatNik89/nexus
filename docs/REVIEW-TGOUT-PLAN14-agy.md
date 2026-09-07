# QA Verification Report: Telegram Output Quality Plan v14 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `83f16ca` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Commit**: `83f16ca6541f5e8f19dafe19a2862e3d74bc05ec` (`docs(tgout): plan v14 — carrier in the loop package (no import cycle), one recovery sum-type API`)  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `83f16ca` (v14) completely and cleanly resolves all architectural and specification findings from Round 13 across both the **Standards** and **Spec** review axes:

1. **Carrier in Loop Package & Clean DAG (Codex r13 MED Closure)**:
   - The typed error carrier is placed directly in the `loop` package: `loop.DriftError{Typed contracts.TypedError; Tool contracts.ToolID}` (`docs/PLAN-TGOUT.md:104-113`).
   - Because `planner` already imports `loop` (to return `loop.Action`), while `loop` never imports `planner`, `loop.failTurn` extracts its own package's type via `errors.As` without creating cyclic imports or introducing new package boundaries.
   - Both live and recovered edge handlers extract `tool` structurally via `errors.As` without parsing `SafeMessage` or error message strings.

2. **Unified State-Aware Recovery API & Closed Sum-Type (Codex r13 LOW + Kilo r13 F1 Closure)**:
   - `completedTurnFinal` is refactored into a single, unified helper `recoveredTurnOutcome(turn) (RecoveredOutcome, bool, error)` with closed sum-type `RecoveredOutcome{Kind: SUCCEEDED|SUSPENDED|FAILED; Final string; Code string; Tool contracts.ToolID}` (`docs/PLAN-TGOUT.md:114-125`).
   - A single journal replay pass enforces strict chronological precedence: later success wins, an earlier suspension is suppressed if followed by `turn.resumed` for the same turn, and terminal failure supersedes.
   - Existing succeeded and suspended recovery tests pass unchanged through `Kind`, while replay and projection errors propagate fail-closed via the `error` return.

3. **Preserved Invariants & Soundness Controls**:
   - **Single-Fence Duplicate/Alias Routing**: Closed key contract `{action, tool_id, arguments}` with case-insensitive token-walk duplicate detection runs across both raw replies and normalized classification views (`docs/PLAN-TGOUT.md:80-82, 144-148`).
   - **Bytes-Only Wire Contract**: Adds zero attempts and preserves existing delivery mechanics; ungranted tick re-flush pump is isolated to `docs/tasks-P0.md:464-472` (`docs/PLAN-TGOUT.md:158-165`).
   - **First-Lease-Only Formatting**: Formatting is applied solely on the first in-flight lease (`outbound_unknown` count == 0); pre-wire failures safely degrade subsequent tick re-flushes to plain text (`docs/PLAN-TGOUT.md:166-178`).
   - **Durability & Rebuild Detectors**: Full detector coverage for restart persistence (close/reopen), version-bump projection rebuild (`Projection.Version() = 2`), dial failure, mixed lifecycles (`suspended -> resumed -> failed`), and all required causal ablations (`docs/PLAN-TGOUT.md:126-132, 179-188`).
   - **Constructive HTML Generation**: Strict tag budget ($\le 90$ opening spans), 4096 UTF-16 unit limit, verbatim pass-through for escaped pipes / backticks, lossless table bullet conversion (`docs/PLAN-TGOUT.md:28-32, 189-216`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **Package Architecture & Clean Dependency DAG ([`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Defining `loop.DriftError` in the `loop` package strictly adheres to Go package hierarchy and DAG rules. `planner` depends on `loop`, and `loop` has no dependency on `planner`.
- **Replay Honesty & Idempotency ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - `recoveredTurnOutcome` unifies recovery into a single $O(N)$ journal scan pass with explicit closed-sum outcomes, guaranteeing that replayed updates never re-invoke the planner or yield stale suspension challenges after resume.
  - Fail-closed error propagation preserves journal integrity guarantees.
- **S7 AttemptGrant Invariants & Outbox Pump Scope ([`AGENTS.md` S7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:68-75`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The slice adds zero attempts and preserves existing delivery mechanics byte-for-byte. The pre-existing ungranted outbox tick pump is formally decoupled into `docs/tasks-P0.md:464-472`.
- **Fail-Closed Typing & Language Boundaries ([`AGENTS.md` Language & HARDQ C7](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Closed key contract `{action, tool_id, arguments}` with case-insensitive duplicate and unknown key rejection ensures deterministic fail-closed behavior across both raw and fenced representations.
  - Kernel errors remain typed English sentinels (`TOOL_SCHEMA_DRIFT`); user-facing Croatian localization (`"Preformuliraj zahtjev."`) is strictly isolated to the adapter edge.

### 2. Spec Axis (Requirements & Edge Cases)

- **Dogfood Findings Resolution**:
  - **Tool-JSON Leak Guard**: Strict closed contract + token-walk drift classifier eliminates model schema drift leaks to the user.
  - **Telegram Formatting**: Send-boundary HTML rendering converts GFM tables to bullet groups and escapes markup safely with local tag and size budgets.
- **Detector Completeness**:
  - The detector suite covers all positive controls, drift dialects (bare and single-fenced with duplicates), declared limits, mixed lifecycles (`suspended -> resumed -> failed`), crash seams, restart persistence, version-bump rebuilds, and required causal ablations.

---

## Top-3 Weakest Points & Implementation Guardrails

In accordance with the mandatory review discipline, the top-3 weakest points of the design and their implementation guardrails are identified below:

1. **`RecoveredOutcome` Zero-Value Discipline ([`docs/PLAN-TGOUT.md:116-118`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L116-L118), [`internal/app/daemon/daemon.go:411-446`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L411-L446))**:
   - *Evidence*: `RecoveredOutcome` uses a tagged struct containing `Kind`, `Final`, `Code`, and `Tool`.
   - *Guardrail*: When populating `RecoveredOutcome` in `recoveredTurnOutcome`, fields not applicable to the resolved `Kind` (e.g., `Final` on `Kind == FAILED`, or `Code`/`Tool` on `Kind == SUCCEEDED`) must remain explicit zero values to prevent state leakage across outcome branches.

2. **Snapshot Timing for `Outbound.Attempts` in Outbox Flush ([`docs/PLAN-TGOUT.md:166-169`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L166-L169), [`internal/channel/channel.go:77-83,509-514`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L77-L83))**:
   - *Evidence*: `Core.Flush` iterates over `pending` rows and invokes `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` *before* calling `send(o)`.
   - *Guardrail*: `channel.Outbound` must expose `Attempts int` populated during `c.Pending(ctx)` snapshot from `chan_outbox.attempts`. `send(o)` must inspect `o.Attempts == 0` (the pre-parking count) so the initial send carries formatted HTML, rather than querying the database post-park where `attempts == 1`.

3. **Empty Table Header Bullet Formatting ([`docs/PLAN-TGOUT.md:28-32, 193-195`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Guardrail*: If a table header cell is empty (e.g. `| | Metric |`), the bullet renderer must fall back to placeholder `'—'` (`• —: value`) rather than emitting malformed `• : value` bullet strings.

---

VERDICT: PASS

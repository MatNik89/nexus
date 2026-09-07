# QA Verification Report: Telegram Output Quality Plan v13 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `b2b9624` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Commit**: `b2b9624cb1e22709214736f1b34383aa632c0288` (`docs(tgout): plan v13 — DriftError typed carrier, state-aware ordered recovery fold, error-returning helper`)  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `b2b9624` (v13) completely and cleanly resolves all findings from Round 12 across both the **Standards** and **Spec** review axes:

1. **Typed Carrier `DriftError` & Structural Propagation (Codex r12 MED#1 Closure)**:
   - The planner returns a dedicated typed error `DriftError{Typed contracts.TypedError; Tool contracts.ToolID}` (`docs/PLAN-TGOUT.md:104-110`).
   - `loop.failTurn` extracts both `error_code` and `tool` structurally via `errors.As` and appends them to `turn.failed` in the journal. Both live and recovered edges extract the tool identity structurally without parsing `SafeMessage` or error message strings.

2. **State-Aware Ordered Recovery Fold & Resumed Suppression (Codex r12 MED#2 Closure)**:
   - Recovery is specified as one state-aware ordered fold (`docs/PLAN-TGOUT.md:111-116`): an earlier `approval.turn_suspended` candidate is suppressed if followed by a later `turn.resumed` for the same turn (mirroring the existing later-success-wins rule).
   - In a legal `suspended -> resumed -> failed` lifecycle, redelivery deterministically recovers the drift failure outcome rather than replaying the stale suspension.
   - The detector suite adds a dedicated mixed-lifecycle test (`suspended -> resumed -> failed -> crash -> collision`) asserting the Croatian drift response and zero planner calls, with a red-capable ablation removing resumed suppression (`docs/PLAN-TGOUT.md:121-125`).

3. **Replay Error Propagation in `failedTurnOutcome` (Codex r12 LOW Closure)**:
   - The helper signature is updated to `failedTurnOutcome(turn) (code, tool, ok, err)` (`docs/PLAN-TGOUT.md:116-118`), propagating journal replay and projection failures fail-closed.

4. **Preserved Invariants & Soundness Controls**:
   - **Single-Fence Duplicate/Alias Routing**: Closed key contract `{action, tool_id, arguments}` with case-insensitive token-walk duplicate detection runs across both raw replies and normalized classification views (`docs/PLAN-TGOUT.md:80-82, 137-141`).
   - **Bytes-Only Wire Contract**: Adds zero attempts and preserves existing delivery mechanics; ungranted tick re-flush pump is isolated to `docs/tasks-P0.md:464-472` (`docs/PLAN-TGOUT.md:151-158`).
   - **First-Lease-Only Formatting**: Formatting is applied solely on the first in-flight lease (`outbound_unknown` count == 0); pre-wire failures safely degrade subsequent tick re-flushes to plain text (`docs/PLAN-TGOUT.md:159-171`).
   - **Durability & Rebuild Detectors**: Full detector coverage for restart persistence (close/reopen), version-bump projection rebuild (`Projection.Version() = 2`), dial failure, and dual red-capable ablations (`ignore-the-count` RED, `drop-the-event-fold` RED; `docs/PLAN-TGOUT.md:172-181`).
   - **Constructive HTML Generation**: Strict tag budget ($\le 90$ opening spans), 4096 UTF-16 unit limit, verbatim pass-through for escaped pipes / backticks, lossless table bullet conversion (`docs/PLAN-TGOUT.md:28-32, 182-209`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **Typed Contracts & Anti-String-Parsing ([`docs/ARCHITECTURE-ESSENTIALS.md:E3`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75), [`AGENTS.md:Hard rules`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - `DriftError` provides a strongly-typed bridge between `Planner.Plan` and `Loop.failTurn`, ensuring that tool identifiers are appended to `turn.failed` and processed by edge adapters purely via `errors.As` structural unpacking.
- **Replay Honency & State Machine Invariants ([`AGENTS.md:B2/B7`](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E5, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - Resumed-suppression ordering prevents stale approval suspensions from shadowing terminal drift failures on replay.
  - Replay errors returned by `failedTurnOutcome` maintain fail-closed guarantees across journal corruption.
- **S7 AttemptGrant Invariants & Outbox Pump Scope ([`AGENTS.md:S7`](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:68-75`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The slice adds zero attempts and alters no scheduling. The pre-existing ungranted outbox tick pump is formally decoupled into `docs/tasks-P0.md:464-472`.
- **Fail-Closed Typing & Language Boundaries ([`AGENTS.md:Language & HARDQ C7`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Kernel schemas, error constants (`TOOL_SCHEMA_DRIFT`), and journal payloads remain in English; Croatian localization (`"Preformuliraj zahtjev."`) is strictly isolated to the adapter edge.

### 2. Spec Axis (Requirements & Edge Cases)

- **Dogfood Findings Resolution**:
  - **Tool-JSON Leak Guard**: Strict closed contract + token-walk drift classifier eliminates model schema drift leaks to the user.
  - **Telegram Formatting**: Send-boundary HTML rendering converts GFM tables to bullet groups and escapes markup safely with local tag and size budgets.
- **Detector Completeness**:
  - The detector suite covers all positive controls, drift dialects (bare and single-fenced with duplicates), declared limits, mixed lifecycles (`suspended -> resumed -> failed`), crash seams, restart persistence, version-bump rebuilds, and required causal ablations.

---

## Top-3 Weakest Points & Implementation Guardrails

In accordance with the mandatory review discipline, the top-3 weakest points of the design and their implementation guardrails are identified below:

1. **Unification of Turn Outcome Replay Scans in Daemon ([`docs/PLAN-TGOUT.md:111-118`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L111-L118), [`internal/app/daemon/daemon.go:263-281,411-446`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L263-L281))**:
   - *Evidence*: `completedTurnFinal` and `failedTurnOutcome` both scan the journal via `Replay(0, ...)`.
   - *Guardrail*: During implementation, unifying turn-terminal recovery into a single state-aware replay pass in `daemon.go` avoids dual linear scans on turn collision while enforcing the identical resumed-suppression ordering.

2. **Snapshot Timing for `Outbound.Attempts` in Outbox Flush ([`docs/PLAN-TGOUT.md:159-162`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L159-L162), [`internal/channel/channel.go:77-83,509-514`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L77-L83))**:
   - *Evidence*: `Core.Flush` iterates over `pending` rows and invokes `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` *before* calling `send(o)`.
   - *Guardrail*: `channel.Outbound` must expose `Attempts int` populated during `c.Pending(ctx)` snapshot from `chan_outbox.attempts`. `send(o)` must inspect `o.Attempts == 0` (the pre-parking count) so the initial send carries formatted HTML, rather than querying the database post-park where `attempts == 1`.

3. **Empty Table Header Bullet Formatting ([`docs/PLAN-TGOUT.md:28-32, 186-188`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Guardrail*: If a table header cell is empty (e.g. `| | Metric |`), the bullet renderer must fall back to placeholder `'—'` (`• —: value`) rather than emitting malformed `• : value` bullet strings.

---

VERDICT: PASS

# QA Verification Report: Telegram Output Quality Plan v11 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `3b43fcb` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Commit**: `3b43fcb4a8eef5862e3d3ddb23bbffb6ae1a7b8e` (`docs(tgout): plan v11 — restart-stable drift recovery + fenced-duplicate cases`)  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `3b43fcb` (v11) completely and cleanly resolves all findings from Round 10 across both the **Standards** and **Spec** review axes:

1. **Restart-Stable Drift Recovery (Codex r10 MED / Kilo r10 F1 Closure)**:
   - The plan explicitly extends `completedTurnFinal` to recover a durable `turn.failed` payload containing `error_code` (`docs/PLAN-TGOUT.md:98-106`).
   - A redelivered update whose turn already failed with `TOOL_SCHEMA_DRIFT` deterministically returns the typed outcome, ensuring the Telegram edge maps the exact Croatian message (`"Preformuliraj zahtjev."`) without re-invoking the planner.
   - The detector suite adds a dedicated close/reopen crash-seam test between `turn.failed` and `CompleteInbound`, asserting identical Croatian output and zero planner calls upon redelivery, accompanied by a red-capable ablation removing failed-turn recovery (`docs/PLAN-TGOUT.md:102-106`).

2. **Single-Fence Duplicate/Alias Routing & Detectors (Codex r10 LOW / Kilo r10 F2 Closure)**:
   - The plan commits duplicate/alias cases across both raw replies and the single-fence normalization path (`fenced duplicate -> drift`; `docs/PLAN-TGOUT.md:80-82`).
   - The detector matrix asserts that token walking is performed on the normalized classification view, preventing last-member-wins standard unmarshaling from bypassing the drift classifier on fenced payloads (`docs/PLAN-TGOUT.md:118-122`).

3. **Preserved Invariants & Soundness Controls**:
   - **Bytes-Only Wire Contract**: Slice adds no attempt and changes no scheduling; outbox re-flush ungranted tick pump is isolated to `docs/tasks-P0.md:464-472` (`docs/PLAN-TGOUT.md:132-139`).
   - **First-Lease-Only Formatting**: Formatting is applied solely on the first in-flight lease (`outbound_unknown` count == 0); pre-wire failures safely degrade subsequent tick re-flushes to plain text (`docs/PLAN-TGOUT.md:140-152`).
   - **Durability & Rebuild Detectors**: Full detector coverage for restart persistence (close/reopen), version-bump projection rebuild (`Projection.Version() = 2`), dial failure, and dual red-capable ablations (`ignore-the-count` RED, `drop-the-event-fold` RED; `docs/PLAN-TGOUT.md:153-162`).
   - **Constructive HTML Generation**: Strict tag budget ($\le 90$ opening spans), 4096 UTF-16 limit, verbatim pass-through for escaped pipes / backticks, lossless table bullet conversion (`docs/PLAN-TGOUT.md:28-32, 163-190`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **At-Least-Once Delivery & Replay Honesty ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - `completedTurnFinal` extension guarantees that the replay oracle recognizes terminal failures (`turn.failed{error_code}`), maintaining identical user-visible output and zero planner re-invocation across daemon restarts.
  - The outbox attempts counter is strictly journal-derived by folding canonical `channel.outbound_unknown` events into the `chan_outbox` projection without ad-hoc mutable SQLite state.
- **S7 AttemptGrant Invariants & Outbox Pump Scope ([`AGENTS.md` S7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:68-75`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The slice adds zero attempts and preserves existing delivery mechanics byte-for-byte. The pre-existing ungranted outbox tick pump is formally decoupled into `docs/tasks-P0.md:464-472`.
- **Fail-Closed Typing & Language Isolation ([`AGENTS.md` Language & HARDQ C7](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Closed key contract `{action, tool_id, arguments}` with case-insensitive duplicate and unknown key rejection ensures deterministic fail-closed behavior across both raw and fenced representations.
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

1. **`completedTurnFinal` Error Synthesis Seam ([`docs/PLAN-TGOUT.md:98-106`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L98-L106), [`internal/app/daemon/daemon.go:263-281,411-446`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L263-L281), [`internal/channel/telegram/telegram.go:270-277`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L270-L277))**:
   - *Evidence*: `completedTurnFinal` currently returns `(string, bool, error)` for succeeded/suspended turns.
   - *Guardrail*: When recovering `turn.failed{error_code == "TOOL_SCHEMA_DRIFT"}`, `RunChannelTurn` must propagate a structured `contracts.TypedError` so the daemon/telegram turn handler maps it to `("Preformuliraj zahtjev.", nil)` before returning to `telegram.go`, ensuring `CompleteInbound` enqueues the Croatian reply rather than tripping the generic English error fallback.

2. **Snapshot Timing for `Outbound.Attempts` in Outbox Flush ([`docs/PLAN-TGOUT.md:140-143`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L140-L143), [`internal/channel/channel.go:77-83,509-514`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L77-L83))**:
   - *Evidence*: `Core.Flush` iterates over `pending` rows and invokes `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` *before* calling `send(o)`.
   - *Guardrail*: `channel.Outbound` must expose `Attempts int` populated during `c.Pending(ctx)` snapshot from `chan_outbox.attempts`. `send(o)` must inspect `o.Attempts == 0` (the pre-parking count) so the initial send carries formatted HTML, rather than querying the database post-park where `attempts == 1`.

3. **Empty Table Header Bullet Formatting ([`docs/PLAN-TGOUT.md:28-32, 167-169`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md#L28-L32))**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Guardrail*: If a table header cell is empty (e.g. `| | Metric |`), the bullet renderer must fall back to placeholder `'—'` (`• —: value`) rather than emitting malformed `• : value` bullet strings.

---

VERDICT: PASS

# QA Verification Report: Telegram Output Quality Plan v10 (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `7cda3b5` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` at commit `7cda3b5` (v10) completely and cleanly resolves all outstanding findings from Round 9 across both the **Standards** and **Spec** review axes:

1. **Bytes-Only Contract & S7 Architectural Isolation (Codex r9 HIGH Closure)**:
   - The plan explicitly specifies that the tgout slice **adds no attempt and changes no scheduling**; it modifies solely *what bytes* an already-scheduled attempt carries (`docs/PLAN-TGOUT.md:122-128`).
   - The pre-existing architectural seam where the outbox tick re-flush pump executes physical attempts without an S7 `AttemptGrant` is formally decoupled and filed in `docs/tasks-P0.md:464-472` for dedicated backlog resolution.
   - The detector asserts the carried bytes on the row the existing machinery re-pends, without altering wire retry policies.

2. **First-Lease-Only Formatting & Honest Degradation (Codex r9 MED#2 Closure)**:
   - The delivery contract is precisely specified as *first in-flight lease only* (`outbound_unknown` count == 0 at flush time; `docs/PLAN-TGOUT.md:121, 129-138`).
   - Pre-wire failures (e.g. dial/network failures or crashes during `c.mark(EvOutboundUnknown)`) consume lease 0 and safely degrade subsequent tick re-flushes to plain text; this is explicitly declared in `Risks / tradeoffs` and asserted by a dedicated dial-failure detector case (`docs/PLAN-TGOUT.md:137, 149`).

3. **Durability, Migration Rebuild & Ablation Detectors (Codex r9 MED#3 & Kilo r9 F1 Closure)**:
   - The detector suite incorporates restart persistence (journal close/reopen asserting count preservation; `docs/PLAN-TGOUT.md:145-147`), version-bump projection rebuild from a v1 database (`docs/PLAN-TGOUT.md:147-149`), pre-wire dial failure (`docs/PLAN-TGOUT.md:149`), and dual red-capable ablations (`ignore-the-count` RED, `drop-the-event-fold` RED; `docs/PLAN-TGOUT.md:149-151`).

4. **Preserved Invariants & Soundness Controls**:
   - **Closed Key Contract & Strict Gating**: Strict lowercase-only key set `{action, tool_id, arguments}` with case-insensitive token-walk duplicate detection (`docs/PLAN-TGOUT.md:71-76`).
   - **Narrowed Drift Classification**: Token walk inspects only `action`, `tool_id`, and `name` keys across all member positions; non-tool keys mentioning tool names remain prose (`docs/PLAN-TGOUT.md:77-84`).
   - **Typed Error Lifecycle**: `contracts.TypedError` with code `TOOL_SCHEMA_DRIFT` lands `TERMINAL-FAILED` in `turn.failed`; failed turns are excluded from conversation history; localized Croatian text (`"Preformuliraj zahtjev."`) is mapped at the edge (`docs/PLAN-TGOUT.md:88-100`).
   - **Constructively Valid HTML & Budget**: Local span budget ($\le 90$ opening tags), 4096 UTF-16 unit limit, verbatim pass-through for escaped pipes / backticks, lossless table bullet conversion (`docs/PLAN-TGOUT.md:28-32, 160-179`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **S7 AttemptGrant Invariants & Outbox Pump Scope (`AGENTS.md`, `docs/ARCHITECTURE-ESSENTIALS.md:68-75`)**:
  - v10 adheres strictly to the single-attempt model for the slice. The plan adds no retry logic, no loop timers, and no scheduling mutations. The pre-existing outbox tick re-flush pump is tracked as an architectural item in `docs/tasks-P0.md:464-472`.
- **Delivery Honesty & Single-Write-Owner (`AGENTS.md` B2/B7)**:
  - The attempts count is strictly derived via journal replay by folding canonical `channel.outbound_unknown` events into the `chan_outbox` projection. No ad-hoc SQLite state mutation is introduced.
  - Bumping `Projection.Version()` to 2 guarantees that existing databases rebuild their projection tables cleanly upon startup.
- **Fail-Closed Typing & Language Isolation (`AGENTS.md` Language & HARDQ C7)**:
  - Kernel errors are strictly typed English sentinels (`TOOL_SCHEMA_DRIFT`). User-facing localization (`"Preformuliraj zahtjev."`) is confined to the Telegram adapter edge.
  - History replay excludes failed turns, ensuring that schema drift errors never pollute the LLM conversation context.

### 2. Spec Axis (Requirements & Edge Cases)

- **First-Lease Degradation Contract**:
  - Telegram Bot API 400 parsing errors mark the delivery in-flight, incrementing the journal-derived `attempts` counter. On the subsequent tick flush, `attempts >= 1` causes the adapter to transmit the original raw text without `parse_mode`, preventing delivery stall loops.
- **Markdown / HTML Rendering Soundness**:
  - Precedence (`fence > code > bold`), span counting ($\le 90$), length checks ($\le 4096$ UTF-16 units), and verbatim handling for escaped pipes prevent malformed HTML generation by construction.

---

## Top-3 Weakest Points & Implementation Guardrails

In accordance with the mandatory review discipline, the top-3 weakest points of the design and their implementation guardrails are identified below:

1. **Snapshot Timing for `Outbound.Attempts` in Outbox Flush (`docs/PLAN-TGOUT.md:129-132`, `internal/channel/channel.go:77-83,509-514`)**:
   - *Evidence*: `Core.Flush` iterates over `pending` rows and invokes `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` *before* calling `send(o)`.
   - *Guardrail*: `channel.Outbound` must expose `Attempts int` populated during `c.Pending(ctx)` snapshot from `chan_outbox.attempts`. `send(o)` must inspect `o.Attempts == 0` (the pre-parking count) so the initial send carries formatted HTML, rather than querying the database post-park where `attempts == 1`.

2. **Daemon Crash Recovery for `turn.failed` Error Codes (`docs/PLAN-TGOUT.md:88-92`, `internal/app/daemon/daemon.go:411-446`, `internal/channel/telegram/telegram.go:270-277`)**:
   - *Evidence*: `daemon.completedTurnFinal` only inspects `turn.succeeded` and `approval.turn_suspended`.
   - *Guardrail*: If the daemon crashes between appending `turn.failed` and completing inbound outbox reply creation (`CompleteInbound`), the replayed message re-executes. The turn handler must ensure clean re-execution or fallback to generic English failure without state corruption.

3. **Empty Table Header Bullet Formatting (`docs/PLAN-TGOUT.md:28-32`)**:
   - *Evidence*: `heading = the row-label cell (or first non-empty cell), remaining cells as "• Header: value" bullets`.
   - *Guardrail*: If a table header cell is empty (e.g. `| | Metric |`), the bullet renderer must fall back to placeholder `'—'` (`• —: value`) rather than emitting malformed `• : value` bullet strings.

---

VERDICT: PASS

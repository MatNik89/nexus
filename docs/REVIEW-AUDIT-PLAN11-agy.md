# Design Review Round 11: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v11)

**Reviewed Revision**: `cf3790d02cc8d1a09e8293ee866b34e9b4f1f641` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v11, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-10 Findings & Notes Disposition

All substantive findings and notes from Round 10 ([`docs/REVIEW-AUDIT-PLAN10-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN10-codex.md) and [`docs/REVIEW-AUDIT-PLAN10-agy.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN10-agy.md)) are fully resolved and **CLOSED** in Plan v11:

1. **Codex F1 (HIGH) — Command-Registration Identity & Evidence Not Bound to Remote Bot**: **CLOSED**
   - *Plan v11 Reference*: [`docs/PLAN-AUDIT-FIXES.md:315-339, 572-576`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L315-L339)
   - *Resolution*:
     - **Bot-Bound Operation & Target Identity**: Command registration binds both the remote bot ID (obtained from the governed startup `getMe` probe) and the exact wire payload hash:
       - Operation ID: `control:tg:<bot-id>:setMyCommands:<sha256 of canonical wire payload>`
       - Target ID: `channel:tg:bot:<bot-id>:setMyCommands`
     - **Cross-Bot Isolation**: Replacing a bot token with bot B creates a distinct durable operation even for an identical command list, preventing false-positive registration skips on bot rotation across restarts.
     - **Companion Schema & Validation**: `channel.control_effect` carries `{operation_id, adapter, bot_id, method, payload_hash, state}`. The channel `PayloadValidator` and projection fold enforce `operation_id == "control:" + adapter + ":" + bot_id + ":" + method + ":" + payload_hash`, rejecting foreign hashes or foreign bot IDs at the journal boundary before wire dispatch.
     - **Detector D2c (Bot Rotation)**: Added dedicated detector verifying that a same-bot restart performs zero `setMyCommands` calls, while a restart on the same profile journal with bot B performs exactly one governed call. Ablating bot binding turns RED, and presenting a bot-A companion for bot B is rejected before the wire.

2. **Codex Note 1 — Wire Payload Hashing (Ordered Array vs Sorted List)**: **CLOSED**
   - *Plan v11 Reference*: [`docs/PLAN-AUDIT-FIXES.md:316-318`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L316-L318)
   - *Resolution*: Updated to specify hashing the exact canonical wire payload as an ordered array without sorting away semantically significant menu order.

3. **Round-10 Agy Implementation Notes**: **CLOSED**
   - *Transition Table Registration*: `machine.AttemptTable` registers `Transition{EvAttemptReconciledRetry, contracts.AttemptUnknown, contracts.AttemptFailedRetryable}` as part of Slice B1 machine definitions ([`docs/PLAN-AUDIT-FIXES.md:213-215, 225-230`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L213-L215)).
   - *Non-Durable Rejection*: S7 `Reconcile` fails closed on non-durable operations ([`docs/PLAN-AUDIT-FIXES.md:210-219, 231-233`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L210-L219)).
   - *Attempt Count Invariance*: `Reconcile` transitions preserve the consumed `attempts` count without spurious increments ([`docs/PLAN-AUDIT-FIXES.md:213-215`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L213-L215)).

---

## 2. Technical Evaluation of Major Plan Components

### A. Shared Egress & Bounded Input Streams (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP transport for `provider` and `telegram`. Guarantees mandatory `ReceiptSink`, exports `ErrPreWire`, and enforces canonical `Endpoint{Host, Port}` normalization.
- **Bounded Buffers**: Protects against unbounded allocations via `io.LimitReader(body, max+1)` and `maxStreamBytes > maxStreamTotal`.

### B. Complete S7 Retry & Reconciliation Engine (Slices B1, B2, B3)
- **Single Retry Authority**: Full S7 engine owns attempt limits, backoff, deadlines, and cancellations. Loop and adapters maintain zero retry logic.
- **Atomic Paired Commits**: `Consume(g, c)`, Landing-driven `Report`/`Cancel`, `Next(op, build)`, and `Reconcile(op, ok, build)` commit S7 state and owner companions atomically in a single `journal.AppendBatch` transaction.
- **E9 Reconciliation**: `s7.Reconcile` provides the single, verifiable exit from `UNKNOWN` for durable effectful operations.
- **Bot-Bound Durable Control Operations**: Command registration is bound to the immutable remote bot identity and canonical wire payload hash.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Verifies `ContextHardLimitTokens` against serialized wire payloads before provider execution.
- **Framing Desynchronization Defense**: Refuses oversized hello/chat frames (`maxFrameBytes`), returning an error frame and terminating the connection.

### D. Supervised Polling & Channel Diagnostics (Slice D)
- **Failure-Independent Health Projection**: Writes snapshots directly to `channel_health.json` independent of SQLite/journal state.
- **Supervised Bot API Lifecycle**: All Telegram endpoints operate under closed policies (`PolicyDelivery`, `PolicyPoll`, `PolicyControlRead`, `PolicyControlEffect`, `PolicyUI`).

### E. Doctor Resolution & Release Gates (Slices E & F)
- **Typed Config Resolution**: Unifies diagnostic resolution in `nexus doctor` with daemon runtime rules.
- **Release Gating**: Enforces Go toolchain pinning (`toolchain go1.26.6`), binary version verification, and clean `govulncheck -mode=binary`.

---

## 3. Numbered New Findings

**Zero substantive design flaws found.**

---

## 4. Notes & Implementation Considerations

1. **Editorial — Summary Call-Site Table Shorthand ([`docs/PLAN-AUDIT-FIXES.md:301`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L301))**:
   - *Observation*: The call-site summary table row on line 301 retains the abbreviated shorthand `control:tg:setMyCommands:<sha256(desired set)> / channel:tg:setMyCommands`, whereas the normative specification in lines 317–319 and lines 335–336 defines the full bot-bound schema `control:tg:<bot-id>:setMyCommands:<sha256>` and `channel:tg:bot:<bot-id>:setMyCommands`. This is an editorial note.
2. **Implementation Consideration — `getMe` Initialization Ordering ([`docs/PLAN-AUDIT-FIXES.md:320-322`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L320-L322))**:
   - *Observation*: During adapter startup, the read-only `getMe` probe (`control:tg:getMe:<n>` under `PolicyControlRead`) must successfully complete and yield `bot_id` before constructing or evaluating the durable registration operation identity.
3. **Implementation Consideration — Canonical JSON Wire Serialization ([`docs/PLAN-AUDIT-FIXES.md:317-318`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L317-L318))**:
   - *Observation*: The canonical wire payload hash must be computed over deterministic JSON serialization (stable key ordering and formatting) to ensure hash identity stability across daemon restarts.

---

VERDICT: PASS

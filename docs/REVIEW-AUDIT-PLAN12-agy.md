# Design Review Round 12: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v12)

**Reviewed Revision**: `5358518796ca4f720b7f01b2bd91831aa10fdeb2` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v12, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-11 Findings & Notes Disposition

All substantive findings and notes from Round 11 ([`docs/REVIEW-AUDIT-PLAN11-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN11-codex.md) and [`docs/REVIEW-AUDIT-PLAN11-agy.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN11-agy.md)) are fully resolved and **CLOSED** in Plan v12:

1. **Codex F1 (HIGH) — Reconciliation Evidence Not Bound to Remote Bot**: **CLOSED**
   - *Plan v12 Reference*: [`docs/PLAN-AUDIT-FIXES.md:302-303, 328-339, 586-591`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L302-L303)
   - *Resolution*:
     - **Bot-Bound Reconciliation Scope**: Startup reconciliation acts *only* on an `UNKNOWN` registration whose embedded `bot_id` strictly matches the current `bot_id` returned by the startup `getMe` probe. An `UNKNOWN` operation belonging to a previous bot token remains `UNKNOWN` indefinitely (bot B never supplies proof for bot A).
     - **Bot-Bound Read Operation & Target**: The reconciliation read operation and target explicitly bind the bot identity: `control:tg:<bot-id>:getMyCommands:<n>` / `channel:tg:bot:<bot-id>:getMyCommands` under `PolicyControlRead`.
     - **Typed Proof Verification**: The channel owner constructs and verifies a typed proof `channel.ControlProof{BotID, PayloadHash, RemoteEqual}` against the operation's embedded `bot_id` and `payload_hash` before reducing it to the boolean handed to `s7.Reconcile(op, equal, build)`.
     - **Detector D2d (Proof Provenance)**: Added dedicated detector verifying that when bot A lands `UNKNOWN` and the daemon restarts with bot B (whose remote menu equals the desired set), bot A remains `UNKNOWN` with no `Reconcile` calls, while bot B executes its own governed `setMyCommands`. Presenting a `ControlProof` for bot A against bot B's operation is refused before reconciliation, and ablating the proof-to-bot check turns RED.

2. **Codex Round-11 Notes (Stale Call-Site & Slice D Rows, D2b Wording)**: **CLOSED**
   - *Plan v12 Reference*: [`docs/PLAN-AUDIT-FIXES.md:302-303, 540, 573`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L302-L303)
   - *Resolution*: Call-site table lines 302–303 and Slice D line 540 updated to the explicit bot-bound identities (`control:tg:<bot-id>:setMyCommands:<sha256(canonical wire payload)>` and `control:tg:<bot-id>:getMyCommands:<n>`). D2b wording aligned to specify matching bot ID and payload hash.

3. **Round-11 Agy Notes (`getMe` Ordering, Canonical Serialization)**: **CLOSED**
   - *Plan v12 Reference*: [`docs/PLAN-AUDIT-FIXES.md:317-322, 328-334`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L317-L322)
   - *Resolution*: Explicitly documented startup ordering from governed `getMe` to bot-id extraction and deterministic wire array hashing.

---

## 2. Technical Evaluation of Major Plan Components

### A. Shared Egress & Bounded Input Streams (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP transport for `provider` and `telegram`. Guarantees mandatory `ReceiptSink`, exports `ErrPreWire`, and enforces canonical `Endpoint{Host, Port}` normalization.
- **Bounded Buffers**: Protects against unbounded allocations via `io.LimitReader(body, max+1)` and `maxStreamBytes > maxStreamTotal`.

### B. Complete S7 Retry & Reconciliation Engine (Slices B1, B2, B3)
- **Single Retry Authority**: Full S7 engine owns attempt limits, backoff, deadlines, and cancellations. Loop and adapters maintain zero retry logic.
- **Atomic Paired Commits**: `Consume(g, c)`, Landing-driven `Report`/`Cancel`, `Next(op, build)`, and `Reconcile(op, ok, build)` commit S7 state and owner companions atomically in a single `journal.AppendBatch` transaction.
- **E9 Reconciliation**: `s7.Reconcile` provides the single, verifiable exit from `UNKNOWN` for durable effectful operations.
- **Bot-Bound Durable Control Operations**: Command registration and reconciliation are strictly bound to the immutable remote bot identity and canonical wire payload hash.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Verifies `ContextHardLimitTokens` against serialized wire payloads before provider execution.
- **Framing Desynchronization Defense**: Refuses oversized hello/chat frames (`maxFrameBytes`), returning an error frame and terminating the connection.

### D. Supervised Polling & Channel Diagnostics (Slice D)
- **Failure-Independent Health Projection**: Writes snapshots directly to `channel_health.json` independent of SQLite/journal state.
- **Supervised Bot API Lifecycle**: All Telegram endpoints operate under closed policies (`PolicyDelivery`, `PolicyPoll`, `PolicyControlRead`, `PolicyControlEffect`, `PolicyUI`).
- **Comprehensive Detectors (D1, D2, D2b, D2c, D2d, D3–D7)**: Fully verify failure handling, journal substrate faults, restart reconciliation, bot rotation, and proof provenance.

### E. Doctor Resolution & Release Gates (Slices E & F)
- **Typed Config Resolution**: Unifies diagnostic resolution in `nexus doctor` with daemon runtime rules.
- **Release Gating**: Enforces Go toolchain pinning (`toolchain go1.26.6`), binary version verification, and clean `govulncheck -mode=binary`.

---

## 3. Numbered New Findings

**Zero substantive design flaws found.**

---

## 4. Notes & Implementation Considerations

1. **Implementation Consideration — `ControlProof` Evaluation on Default / Empty Menus ([`docs/PLAN-AUDIT-FIXES.md:328-337`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L328-L337))**:
   - *Observation*: When `getMyCommands` returns an empty array `[]` (the Telegram default if commands were never registered), the comparison logic must treat this as non-equal against any non-empty desired payload, setting `RemoteEqual = false` so `Reconcile` transitions to `FAILED_RETRYABLE` and allows `Next` to issue the registration grant.
2. **Implementation Consideration — `bot_id` Decimal Formatting & Delimiter Hygiene ([`docs/PLAN-AUDIT-FIXES.md:317-320, 343-346`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L317-L320))**:
   - *Observation*: In the Telegram Bot API, `bot_id` is an integer (e.g. `123456789`). The adapter must serialize `bot_id` as standard base-10 ASCII digits, ensuring unambiguous parsing and preventing any collision with `:` delimiters in operation or target strings.
3. **Implementation Consideration — Startup Diagnostic Progression ([`docs/PLAN-AUDIT-FIXES.md:326-339, 539-543`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L326-L339))**:
   - *Observation*: During adapter startup, the sequential chain (`getMe` -> reconciliation / registration) should resolve (or mark health degraded) before the long-polling `getUpdates` loop enters steady state, keeping startup diagnostics and log sequencing cleanly separated.

---

VERDICT: PASS

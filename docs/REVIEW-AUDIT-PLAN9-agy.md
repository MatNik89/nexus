# Design Review Round 9: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v9)

**Reviewed Revision**: `b8e1ab78f89e5dc2aa4223df1f19e4871b695128` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v9, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-8 Findings & Notes Disposition

All substantive findings and notes from Round 8 ([`docs/REVIEW-AUDIT-PLAN8-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN8-codex.md)) and previous reviewer notes are **CLOSED** in Plan v9:

1. **Codex F1 (HIGH) — `setMyCommands` Ambiguous Effect Blind Re-Registration Across Daemon Restart**: **CLOSED**
   - *Plan v9 Reference*: [`docs/PLAN-AUDIT-FIXES.md:287, 301-314, 535-540`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L287)
   - *Resolution*: Command registration (`setMyCommands`) is made a durable S7 operation with a deterministic, stable identity: `control:tg:setMyCommands:<sha256(sorted desired commands)>` under `PolicyControlEffect{Durable: true}` and owner companion `channel.control_effect{operation_id, method, payload_hash, state}`. On daemon startup:
     - A rehydrated `SUCCEEDED` registration for the same payload hash performs zero wire calls.
     - A rehydrated `UNKNOWN` registration performs zero `setMyCommands` calls; instead, it executes an S7-governed read-only reconciliation via `getMyCommands` (`control:tg:getMyCommands:<n>` under `PolicyControlRead`, added to the call-site table).
     - If the remote command menu matches the desired payload, the UNKNOWN operation transitions to `SUCCEEDED` (`attempt.reconciled_ok`); if different, a new effect operation is authorized.
     - Added Detector D2b asserting zero `setMyCommands` calls on startup before reconciliation, verified against an ablation that blinds re-registration.

2. **Round-8 Notes Disposition**: **CLOSED**
   - *`PolicyUI` E9 Classification*: Clarified at [`docs/PLAN-AUDIT-FIXES.md:288-289`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L288-L289) that while definite failures (e.g. 400 Bad Request) are terminal, 5xx / post-write / malformed replies strictly land in `UNKNOWN` per E9.
   - *Target Separation*: Distinct targets (`channel:tg:getMe`, `channel:tg:getMyCommands`, `channel:tg:setMyCommands`) prevent cross-method grant substitution ([`docs/PLAN-AUDIT-FIXES.md:285-287`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L285-L287)).

---

## 2. Technical Evaluation of Major Plan Components

### A. Egress Owner & Bounded Buffers (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP configuration across `provider` and `telegram`. Eliminates ambient proxy / default transport leaks, enforces mandatory `ReceiptSink`, exports `ErrPreWire`, and applies canonical `Endpoint{Host, Port}` normalization.
- **Body & Stream Bounds**: Implements bounded readers on JSON payloads (`io.LimitReader(body, max+1)`) and streams (`maxStreamBytes > maxStreamTotal`), securing against memory exhaustion while preserving detector observability.

### B. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Single Retry Owner**: Retains absolute S7 ownership over attempt counts, deadlines, backoff, and cancellations across tool execution, outbox delivery, provider completions, structured re-asks, polling, and control calls.
- **End-to-End Paired Atomic Commits**: `Consume(g, c)`, Landing-driven `Report`/`Cancel`, and `Next(op, build)` coordinate with `journal.AppendBatch` to commit every paired S7 + outbox/control transition in a single serialized SQLite transaction.
- **Lease Recovery & E9 Compliance**: Unconsumed `AUTHORIZED` leases expired or rehydrated without `attempt_started` transition via `s7.lease_revoked` (`AUTHORIZED -> PLANNED`) to issue fresh grants with new nonces without advancing consumed attempt counts. Records with `attempt_started` rehydrate strictly to `UNKNOWN` without re-granting.
- **Synchronous Driver (`s7.Execute`)**: Encapsulates retry loops and backoff sleeps behind S7 for both chat completions and structured output generation.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Enforces `ContextHardLimitTokens` directly on assembled wire payloads before provider dispatch, failing closed on overflow.
- **Framing Desynchronization Defense**: Exceeding `maxFrameBytes` on hello or chat frames returns an error frame and closes the UDS connection, preventing frame desynchronization.

### D. Channel Health & Supervised Polling (Slice D)
- **Failure-Independent Health Projection**: Writes atomic snapshot to `channel_health.json` independent of SQLite/journal availability.
- **S7-Governed Polling & Control**: Polling and command registration are tracked under `PolicyPoll`, `PolicyControlRead`, and `PolicyControlEffect`.
- **Durable Control Reconciliation**: Guarantees zero unverified duplicate mutations across process restarts for effectful Bot API operations.

### E. Config Resolution & Gated Toolchain Deployment (Slices E & F)
- **Dynamic Doctor Resolution**: Shares typed config resolution with the runtime daemon.
- **Hardened Release & Deploy Gates**: `go.mod` toolchain directive (`toolchain go1.26.6`) together with binary version checking and `govulncheck -mode=binary` in `scripts/lib/release-gate.sh` ensures that vulnerable binaries cannot be signed, published, or deployed.

---

## 3. Top-3 Implementation Considerations & Weakest Points

No substantive design flaws remain. The following items represent implementation considerations to observe during coding:

1. **`channel.control_effect` Payload Validation & Projection ([`docs/PLAN-AUDIT-FIXES.md:301-305`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L301-L305))**:
   - *Observation*: The `channel.control_effect` payload validator must verify that `operation_id == "control:tg:setMyCommands:" + payload_hash`, enforcing companion binding integrity consistent with `channel.outbound_*` marks.
2. **`getMyCommands` Response Normalization for Reconciliation ([`docs/PLAN-AUDIT-FIXES.md:307-310`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L307-L310))**:
   - *Observation*: During startup reconciliation, the commands array returned by `getMyCommands` must be sorted and normalized by command name before comparison with the desired list to prevent spurious re-registration caused solely by server-side array ordering.
3. **`PolicyUI` In-Memory Non-Retryable Error Diagnostics ([`docs/PLAN-AUDIT-FIXES.md:288-289`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L288-L289))**:
   - *Observation*: Ambiguous failures (5xx / post-write) on ephemeral UI operations land in `UNKNOWN` in memory; ensure these surface as degraded diagnostics without impeding ongoing turn execution.

---

VERDICT: PASS

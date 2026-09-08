# Design Review Round 8: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v8)

**Reviewed Revision**: `794197931c89fbf02a59e9ba7d100cecf0e7845f` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v8, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-7 Findings & Notes Disposition

All substantive findings and editorial notes from Round 7 ([`docs/REVIEW-AUDIT-PLAN7-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN7-codex.md)) and previous reviewer notes are **CLOSED** in Plan v8:

1. **Codex F1 (HIGH) — Incompatible Control Contracts & Policy Separation**: **CLOSED**
   - *Plan v8 Reference*: [`docs/PLAN-AUDIT-FIXES.md:281-284, 298-305, 531-534`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L281-L284)
   - *Resolution*: Eliminated the singular `PolicyControl` and established two closed, immutable policy constants: `PolicyControlRead` (for `getMe`: MaxAttempts 3, backoff 2s..30s, RetryableCodes `{transport_prewire, http_429, http_5xx, transport_postwrite, malformed_reply}`) and `PolicyControlEffect` (for `setMyCommands`: MaxAttempts 3, backoff 2s..30s, RetryableCodes `{transport_prewire, http_429}` only). Ambient 5xx / post-write / malformed replies on `setMyCommands` land in `OutcomeUnknown` per E9. Added Detector D7 asserting `getMe` retry behavior against `setMyCommands` UNKNOWN / zero-retry behavior.

2. **Codex F2 (HIGH) — Durable `Next` Nil-Builder Fail-Closed Detector**: **CLOSED**
   - *Plan v8 Reference*: [`docs/PLAN-AUDIT-FIXES.md:206, 397-400`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L206)
   - *Resolution*: Formally specified that `Next(op, build)` on a durable operation strictly refuses a nil builder (`build == nil`). Added Detector 9g asserting that calling `Next(op, nil)` on a durable operation fails closed (no grant, no S7 transition, no journal event) both while grant-eligible and after exhaustion, turning RED upon ablation.

3. **Round-7 Editorial Cleanups**: **CLOSED**
   - *Lease Recovery API Reference*: [`docs/PLAN-AUDIT-FIXES.md:189`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L189) updated to consistently use `Next(op, build)`.

---

## 2. Technical Evaluation of Major Plan Components

### A. Egress Owner & Bounded Buffers (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP configuration across `provider` and `telegram`. Eliminates ambient proxy / default transport leaks, enforces mandatory `ReceiptSink`, exports `ErrPreWire`, and applies canonical `Endpoint{Host, Port}` normalization.
- **Body & Stream Bounds**: Implements bounded readers on JSON payloads (`io.LimitReader(body, max+1)`) and streams (`maxStreamBytes > maxStreamTotal`), securing against memory exhaustion while preserving detector observability.

### B. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Single Retry Owner**: Retains absolute S7 ownership over attempt counts, deadlines, backoff, and cancellations across tool execution, outbox delivery, provider completions, structured re-asks, polling, and control calls.
- **End-to-End Paired Atomic Commits**: `Consume(g, c)`, Landing-driven `Report`/`Cancel`, and `Next(op, build)` coordinate with `journal.AppendBatch` to commit every paired S7 + outbox transition (start, success, retry, cancel, exhaustion, deadline) in a single serialized SQLite transaction.
- **Lease Recovery & E9 Compliance**: Unconsumed `AUTHORIZED` leases expired or rehydrated without `attempt_started` transition via `s7.lease_revoked` (`AUTHORIZED -> PLANNED`) to issue fresh grants with new nonces without advancing consumed attempt counts. Records with `attempt_started` rehydrate strictly to `UNKNOWN` without re-granting.
- **Synchronous Driver (`s7.Execute`)**: Encapsulates retry loops and backoff sleeps behind S7 for both chat completions and structured output generation.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Enforces `ContextHardLimitTokens` directly on assembled wire payloads before provider dispatch, failing closed on overflow.
- **Framing Desynchronization Defense**: Exceeding `maxFrameBytes` on hello or chat frames returns an error frame and closes the UDS connection, preventing frame desynchronization.

### D. Channel Health & Supervised Polling (Slice D)
- **Failure-Independent Health Projection**: Writes atomic snapshot to `channel_health.json` independent of SQLite/journal availability.
- **S7-Governed Polling & Control**: Polling and command registration are tracked under `PolicyPoll`, `PolicyControlRead`, and `PolicyControlEffect`.
- **Supervisor Transparency**: Preserves originating classification without masking terminal remote rejections under generic substrate labels.

### E. Config Resolution & Gated Toolchain Deployment (Slices E & F)
- **Dynamic Doctor Resolution**: Shares typed config resolution with the runtime daemon.
- **Hardened Release & Deploy Gates**: `go.mod` toolchain directive (`toolchain go1.26.6`) together with binary version checking and `govulncheck -mode=binary` in `scripts/lib/release-gate.sh` ensures that vulnerable binaries cannot be signed, published, or deployed.

---

## 3. Top-3 Implementation Considerations & Weakest Points

No substantive design flaws remain. The following items represent implementation considerations to observe during coding:

1. **`PolicyControlRead` vs `PolicyControlEffect` Target Name Separation ([`docs/PLAN-AUDIT-FIXES.md:283-284`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L283-L284))**:
   - *Observation*: Ensure `getMe` binds to `TargetID("channel:tg:getMe")` and `setMyCommands` binds to `TargetID("channel:tg:setMyCommands")` so that authority target validation prevents cross-method grant substitution.
2. **`ExtractVia` Generator Grant Parameter Discipline ([`docs/PLAN-AUDIT-FIXES.md:338-341`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L338-L341))**:
   - *Observation*: The `generate func(ctx context.Context, g s7.Grant, reask bool)` callback must ensure that the inner transport invocation strictly passes the presented grant `g` to `Chat`/`client.Do`, ensuring attempt 1 consumes attempt 1's grant and attempt 2 consumes attempt 2's grant.
3. **`s7.Next(op, build)` Durable Builder Execution on Early Return ([`docs/PLAN-AUDIT-FIXES.md:151-158, 206`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L151-L158))**:
   - *Observation*: When `Next` is called on a durable operation, `build` must be validated against `nil` immediately upon function entry before performing any deadline or attempt evaluations, guaranteeing consistent fail-closed behavior across all execution branches.

---

VERDICT: PASS

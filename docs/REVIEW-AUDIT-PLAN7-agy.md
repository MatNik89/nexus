# Design Review Round 7: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v7)

**Reviewed Revision**: `82de4b97ce1c793ff826b52a5c4d7ecf8b5a0e37` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v7, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-6 Findings & Notes Disposition

All substantive findings and editorial notes from Round 6 ([`docs/REVIEW-AUDIT-PLAN6-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN6-codex.md)) and previous reviewer notes are **CLOSED** in Plan v7:

1. **Codex F1 (HIGH) — Structured Output Salvage Execution vs S7 Terminal State**: **CLOSED**
   - *Plan v7 Reference*: [`docs/PLAN-AUDIT-FIXES.md:342-349, 401-409`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L342-L349)
   - *Resolution*: Salvage is placed strictly INSIDE the final attempt's outcome decision. In `ExtractVia`, when attempt 2 cannot strict-validate (or fails in transport), the callback executes the ordered salvage ladder (re-ask bytes, then original bytes) BEFORE returning its S7 outcome. An accepted salvage returns `OutcomeSucceeded` alongside the validated value; failure of salvage returns `OutcomeFailedTerminal` and no value. Returned value and S7 `SUCCEEDED` state are observationally equivalent (`returned value <=> SUCCEEDED`). Detector 9f (a–d) asserts result and S7 state together across all branches.

2. **Codex F2 (HIGH) — `setMyCommands` Effectful Mutation vs E9 Ambiguity Handling**: **CLOSED**
   - *Plan v7 Reference*: [`docs/PLAN-AUDIT-FIXES.md:281-285, 292-301, 507-510`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L281-L285)
   - *Resolution*: Split control classification by effect: `control-read` (`getMe`) and `control-effect` (`setMyCommands`). `getMe` allows 5xx/post-write retry under `PolicyControl`. `setMyCommands` mutates remote bot state and carries no commit receipt on 5xx/post-write/malformed replies; such failures strictly land in `OutcomeUnknown` (E9 compliance, no blind resend). Retry is permitted solely for definite pre-wire refusals and 429 rate limits (`PolicyControl` `RetryableCodes {transport_prewire, http_429}`). Detector D2 verifies that a 5xx on `setMyCommands` lands in S7 `UNKNOWN` with zero subsequent calls across all backoff intervals.

3. **Round-6 Notes & Stale Wording Cleanup**: **CLOSED**
   - *B2 Paired `Next` Invocation*: [`docs/PLAN-AUDIT-FIXES.md:250-253`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L250-L253) consistently uses `s7.Next(op, build)` where `ErrExhausted` indicates that S7 `FAILED` and the `outbound_failed` companion were already atomically committed in one batch.
   - *Unified `PolicyPoll` Vocabulary*: Removed stale code lists; [`docs/PLAN-AUDIT-FIXES.md:280, 477-478`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L280) share the exact closed vocabulary `{transport_prewire, http_429, http_5xx, transport_postwrite, malformed_reply}`.
   - *Delivery-Kind Classification*: [`docs/PLAN-AUDIT-FIXES.md:308-311`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L308-L311) cleanly isolates delivery classification (`5xx / post-write / malformed -> ErrAmbiguousSend (UNKNOWN, E9)`).
   - *Nil Builder Guard on Durable Ops*: Explicitly added at [`docs/PLAN-AUDIT-FIXES.md:204`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L204).
   - *Dual-Narrative Doc Comment*: Documented at [`docs/PLAN-AUDIT-FIXES.md:204-207`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L204-L207) linking `machine.AttemptTable` and `s7_operations` projection.

---

## 2. Technical Evaluation of Major Plan Components

### A. Egress Owner & Bounded Buffers (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP configuration across `provider` and `telegram`. Eliminates ambient proxy / default transport leaks, enforces mandatory `ReceiptSink`, exports `ErrPreWire`, and applies canonical `Endpoint{Host, Port}` normalization.
- **Body & Stream Bounds**: Implements bounded readers on JSON payloads (`io.LimitReader(body, max+1)`) and streams (`maxStreamBytes > maxStreamTotal`), securing against memory exhaustion while preserving detector observability.

### B. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Single Retry Owner**: Retains absolute S7 ownership over attempt counts, deadlines, backoff, and cancellations across tool execution, outbox delivery, provider completions, and structured re-asks.
- **End-to-End Paired Atomic Commits**: `Consume(g, c)`, Landing-driven `Report`/`Cancel`, and `Next(op, build)` coordinate with `journal.AppendBatch` to commit every paired S7 + outbox transition (start, success, retry, cancel, exhaustion, deadline) in a single serialized SQLite transaction.
- **Lease Recovery & E9 Compliance**: Unconsumed `AUTHORIZED` leases expired or rehydrated without `attempt_started` transition via `s7.lease_revoked` (`AUTHORIZED -> PLANNED`) to issue fresh grants with new nonces without advancing consumed attempt counts. Records with `attempt_started` rehydrate strictly to `UNKNOWN` without re-granting.
- **Synchronous Driver (`s7.Execute`)**: Encapsulates retry loops and backoff sleeps behind S7 for both chat completions and structured output generation.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Enforces `ContextHardLimitTokens` directly on assembled wire payloads before provider dispatch, failing closed on overflow.
- **Framing Desynchronization Defense**: Exceeding `maxFrameBytes` on hello or chat frames returns an error frame and closes the UDS connection, preventing frame desynchronization.

### D. Channel Health & Supervised Polling (Slice D)
- **Failure-Independent Health Projection**: Writes atomic snapshot to `channel_health.json` independent of SQLite/journal availability.
- **S7-Governed Polling & Control**: Polling and command registration are tracked under `PolicyPoll` and `PolicyControl`.
- **Supervisor Transparency**: Preserves originating classification without masking terminal remote rejections under generic substrate labels.

### E. Config Resolution & Gated Toolchain Deployment (Slices E & F)
- **Dynamic Doctor Resolution**: Shares typed config resolution with the runtime daemon.
- **Hardened Release & Deploy Gates**: `go.mod` toolchain directive (`toolchain go1.26.6`) together with binary version checking and `govulncheck -mode=binary` in `scripts/lib/release-gate.sh` ensures that vulnerable binaries cannot be signed, published, or deployed.

---

## 3. Top-3 Implementation Considerations & Weakest Points

No substantive design flaws remain. The following items represent implementation considerations to observe during coding:

1. **`ExtractVia` Salvage Sequence in Attempt 2 Callback ([`docs/PLAN-AUDIT-FIXES.md:342-349`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L342-L349))**:
   - *Observation*: Implementation must ensure attempt 2's callback executes strict validation on the re-ask bytes first, and only falls back to the salvage ladder (re-ask bytes, then original attempt 1 bytes) if strict decode fails, before reporting `OutcomeSucceeded` or `OutcomeFailedTerminal`.
2. **`control-read` vs `control-effect` Kind Discrimination in `call` ([`docs/PLAN-AUDIT-FIXES.md:281-285, 292-298`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L281-L285))**:
   - *Observation*: The `kind` parameter passed to `a.call` must distinguish `KindControlRead` (`getMe`) from `KindControlEffect` (`setMyCommands`) so that transport 5xx / post-write errors are mapped to `DefiniteFailure{http_5xx, Retryable}` for `getMe` and `ErrAmbiguousSend` (`UNKNOWN`) for `setMyCommands`.
3. **`s7.Next` Durable Operation Builder Non-Nil Assertion ([`docs/PLAN-AUDIT-FIXES.md:204`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L204))**:
   - *Observation*: In `s7.Next(op, build)`, if `Policy.Durable == true` and `build == nil`, `Next` must return an error before checking attempts or due timestamps, guarding against programmatic omissions in durable callers.

---

VERDICT: PASS

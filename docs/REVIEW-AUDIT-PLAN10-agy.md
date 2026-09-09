# Design Review Round 10: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v10)

**Reviewed Revision**: `8c3120b0802c63ef6ec3bfb9b0f7690626b9195d` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v10, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-9 Findings & Notes Disposition

All findings and notes from Round 9 ([`docs/REVIEW-AUDIT-PLAN9-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN9-codex.md) and [`docs/REVIEW-AUDIT-PLAN9-agy.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN9-agy.md)) are fully resolved and **CLOSED** in Plan v10:

1. **Codex F1 (HIGH) — Reconciliation S7 API, Atomic Batch Recipe, and Companion Validation**: **CLOSED**
   - *Plan v10 Reference*: [`docs/PLAN-AUDIT-FIXES.md:210-219, 234-244, 318-332, 559-564`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L210-L219)
   - *Resolution*:
     - **S7 Owner API**: Plan v10 introduces `s7.Reconcile(op, ok bool, build func(Landing) Companion)` as the single authority and only exit from `UNKNOWN`.
     - **Canonical Lifecycle & Retry Transition**: `Reconcile` verifies `op` is in state `UNKNOWN`. If `ok == true` (remote effect present), it transitions to `SUCCEEDED` via `attempt.reconciled_ok`. If `ok == false` (remote effect absent) and the operation retains remaining attempt/deadline budget, it transitions to `FAILED_RETRYABLE` via the new canonical edge `attempt.reconciled_retry` (`contracts.AttemptUnknown -> contracts.AttemptFailedRetryable`), allowing `Next(op, build)` to issue the subsequent grant under the same existing operation identity. If budget is exhausted, it transitions to `FAILED` via `attempt.reconciled_failed`.
     - **Atomic Batch Commit**: Durable reconciliation commits `s7.operation_reconciled{op, state, next_attempt_unix}` and exactly ONE owner companion (`c.Key == op`) via `journal.AppendBatch`. A nil builder is refused before transition.
     - **Companion Integrity & Payload Validation**: `channel.control_effect{operation_id, method, payload_hash, state}` is validated by the channel `PayloadValidator` and projection fold, requiring `operation_id == "control:tg:setMyCommands:" + payload_hash`, `method` in the closed method set, `state` in the closed landing set, and the journal's `ProfileID`. Foreign hashes and mismatches are rejected at the journal boundary.
     - **Detector D2b Extensions**: Detector D2b is extended to assert S7 states (`UNKNOWN`, `SUCCEEDED`, `FAILED_RETRYABLE`, `FAILED`) alongside wire counts, verify refusal of mismatched companion hashes (zero wire), test torn-batch injection (leaving S7 `UNKNOWN` with no `control_effect` row), and include red-capable ablations of both the reconciliation transition and atomic batching.

2. **Stale Policy Enumeration Wording Note**: **CLOSED**
   - *Plan v10 Reference*: [`docs/PLAN-AUDIT-FIXES.md:140-141`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L140-L141)
   - *Resolution*: Updated to explicitly list all eight closed policy constants (`PolicyTool`, `PolicyProvider`, `PolicyStructured`, `PolicyDelivery`, `PolicyPoll`, `PolicyControlRead`, `PolicyControlEffect`, `PolicyUI`).

3. **Round-9 Agy Notes (Companion Validation, `getMyCommands` Normalization, `PolicyUI` Diagnostics)**: **CLOSED**
   - Explicitly integrated into the companion validation specification ([`docs/PLAN-AUDIT-FIXES.md:328-332`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L328-L332)), registration reconciliation comparison ([`docs/PLAN-AUDIT-FIXES.md:320-323`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L320-L323)), and UI error handling ([`docs/PLAN-AUDIT-FIXES.md:301-302`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L301-L302)).

---

## 2. Technical Evaluation of Major Plan Components

### A. Egress Owner & Bounded Buffers (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP configuration across `provider` and `telegram`. Eliminates ambient proxy and default transport leaks, enforces mandatory `ReceiptSink`, exports `ErrPreWire`, and applies canonical `Endpoint{Host, Port}` normalization.
- **Body & Stream Bounds**: Implements bounded readers on JSON payloads (`io.LimitReader(body, max+1)`) and streams (`maxStreamBytes > maxStreamTotal`), securing against memory exhaustion while preserving detector observability.

### B. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Single Retry Authority**: Retains absolute S7 ownership over attempt counts, deadlines, backoff, and cancellations across tool execution, outbox delivery, provider completions, structured re-asks, polling, and control calls.
- **End-to-End Paired Atomic Commits**: `Consume(g, c)`, Landing-driven `Report`/`Cancel`, `Next(op, build)`, and `Reconcile(op, ok, build)` coordinate with `journal.AppendBatch` to commit every paired S7 + outbox/control transition in a single serialized SQLite transaction.
- **Lease Recovery & E9 Compliance**: Unconsumed `AUTHORIZED` leases expired or rehydrated without `attempt_started` transition via `s7.lease_revoked` (`AUTHORIZED -> PLANNED`) to issue fresh grants with new nonces without advancing consumed attempt counts. Records with `attempt_started` rehydrate strictly to `UNKNOWN` without re-granting until reconciliation.
- **Synchronous Driver (`s7.Execute`)**: Encapsulates retry loops and backoff sleeps behind S7 for both chat completions and structured output generation.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Enforces `ContextHardLimitTokens` directly on assembled wire payloads before provider dispatch, failing closed on overflow.
- **Framing Desynchronization Defense**: Exceeding `maxFrameBytes` on hello or chat frames returns an error frame and closes the UDS connection, preventing frame desynchronization.

### D. Channel Health & Supervised Polling (Slice D)
- **Failure-Independent Health Projection**: Writes atomic snapshot to `channel_health.json` independent of SQLite/journal availability.
- **S7-Governed Polling & Control**: Polling and command registration are tracked under `PolicyPoll`, `PolicyControlRead`, and `PolicyControlEffect`.
- **Durable Control Reconciliation**: Guarantees zero unverified duplicate mutations across process restarts for effectful Bot API operations via `s7.Reconcile`.

### E. Config Resolution & Gated Toolchain Deployment (Slices E & F)
- **Dynamic Doctor Resolution**: Shares typed config resolution with the runtime daemon.
- **Hardened Release & Deploy Gates**: `go.mod` toolchain directive (`toolchain go1.26.6`) together with binary version checking and `govulncheck -mode=binary` in `scripts/lib/release-gate.sh` ensures that vulnerable binaries cannot be signed, published, or deployed.

---

## 3. Top-3 Implementation Considerations & Weakest Points

In accordance with mandatory review discipline, hunting for potential failure modes during slice execution reveals the top-3 implementation considerations:

1. **`machine.AttemptTable` Transition Table Registration for `attempt.reconciled_retry` ([`docs/PLAN-AUDIT-FIXES.md:213-215, 225-230`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L213-L215))**:
   - *Observation*: The canonical state machine in `internal/kernel/machine/machine.go` must register `Transition{EvAttemptReconciledRetry, contracts.AttemptUnknown, contracts.AttemptFailedRetryable}` in `AttemptTable`. Implementing `s7.Reconcile` without updating the underlying transition table would cause the default-deny table check in `machine.Step` to fail closed with `ErrInvalidTransition`.
2. **`Reconcile` Rejection on Non-Durable Operations ([`docs/PLAN-AUDIT-FIXES.md:210-219, 231-233`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L210-L219))**:
   - *Observation*: Non-durable operations (such as turn-scoped tool calls or provider completions) do not survive restart and have no asynchronous reconciliation path. `s7.Reconcile` should immediately return an error if called on an operation with `Policy.Durable == false` or if a non-durable operation attempts to supply a companion.
3. **Reconciliation Attempt Budget Count Invariance ([`docs/PLAN-AUDIT-FIXES.md:213-215`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L213-L215))**:
   - *Observation*: When `Reconcile(op, false, build)` transitions an operation from `UNKNOWN` to `FAILED_RETRYABLE`, the `attempts` count on the record must remain unchanged. The reconciliation query (`getMyCommands`) is an observational read, not a consumption of the effectful operation's attempt budget; the budget must be consumed only when `Consume` is called on the subsequent `setMyCommands` attempt grant.

---

VERDICT: PASS

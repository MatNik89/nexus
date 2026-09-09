# Design Review Round 3: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v3)

**Reviewed Revision**: `a5088e4b15aca1a70b6ea6b0904f4afecf7c2f54` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v3, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2)

---

## 1. Round-2 Notes Disposition

All 3 implementation considerations from Round 2 ([`docs/REVIEW-AUDIT-PLAN2-agy.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN2-agy.md)) are **CLOSED** in Plan v3:

1. **UDS Framing Connection Close on Over-Long Frames**: **CLOSED**
   - *Plan v3 Reference*: [`docs/PLAN-AUDIT-FIXES.md:271-274, 285-288`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L271-L274)
   - *Resolution*: Exceeding `maxFrameBytes` on either hello or chat frames returns an `error` frame and immediately terminates the UDS connection, preventing frame desynchronization from residual unread bytes. Detector #3 verifies that an aligned valid suffix in an oversized frame is never parsed as a subsequent frame.
2. **Substrate Failure Clean Teardown**: **CLOSED**
   - *Plan v3 Reference*: [`docs/PLAN-AUDIT-FIXES.md:323-327`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L323-L327)
   - *Resolution*: Upon `Adapter.Run` error return (substrate failure), the supervising composition root records `substrate` health and immediately cancels the adapter context, guaranteeing prompt termination of background poll/flush goroutines.
3. **Transport vs Accumulator Streaming Ceiling Hierarchy**: **CLOSED**
   - *Plan v3 Reference*: [`docs/PLAN-AUDIT-FIXES.md:74-77, 275-277`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L74-L77)
   - *Resolution*: Transport `maxStreamBytes` is explicitly set above the planner's accumulator limit (`maxStreamTotal`), ensuring that Slice C detector #4 cleanly observes the planner accumulation ceiling without being masked by transport socket truncation.

---

## 2. Technical Evaluation of Major Plan Components

### A. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Engine Promotion (`internal/kernel/s7`)**: S7 is promoted to a full `ExecutionPolicy` engine conforming to `SPEC P0.2:1386-1393`. The state table is extended with `AttemptFailedRetryable` (`FAILED_RETRYABLE`), preserving default-reject state legality in `machine.AttemptTable`. Adapters and loops hold zero retry loops, only requesting `Next(op)` grants.
- **Delivery Honesty (HARDQ B2) & Causal Coupling**: In `Core.Flush`, each pending row mints a grant for `OperationID("delivery:" + o.DeliveryID)` bound to `TargetID("channel:<adapter_id>:delivery:<delivery_id>")`. Definite transient failures (429, pre-wire) transition via `FAILED_RETRYABLE` back to outbox `PENDING` with exponential backoff; permanent errors (4xx, pre-consume local refusal, max attempts) commit to `FAILED`. Ambiguous sends (5xx post-write) remain `UNKNOWN`. S7 state and outbox state commit as a unified causal result.
- **Durability & Crash Recovery**: Durable operations (`Durable: true`) journal through `s7.operation_*` events and project to `s7_operations`. Startup rehydration restores non-terminal attempts without counter resets, and rehydrates mid-flight `RUNNING` records fail-closed to `UNKNOWN`.

### B. Channel Health & Polling Governance (Slice D)
- **S7-Governed Polling**: Polling operates under `PolicyPoll` (`Durable: false`, `MaxAttempts: 6`, backoff). Transient polling failures back off via S7 rather than tight-looping. Fatal remote rejections (401/403) and substrate failures immediately halt `Adapter.Run`.
- **Failure-Independent Health Projection**: The health owner writes to `<profile system dir>/channel_health.json` via `atomicwrite.Write`, ensuring diagnostic observability even when the database/journal substrate is completely broken.

### C. Toolchain Directives, Vulnerability Gating, and Execution Order (Slice F)
- **Toolchain Directive**: Adding `toolchain go1.26.6` to `go.mod` with ambient `GOTOOLCHAIN=auto` automatically downloads and builds with Go 1.26.6, eliminating the host system toolchain upgrade as a blocking prerequisite.
- **Binary Release & Deployment Gates**: `p0-accept.sh` and `scripts/deploy.sh` inspect the binary version (`go version "$BIN"`) against floor `1.26.6` and execute `govulncheck -mode=binary "$BIN"`, halting release signing and automated deployment upon any finding.
- **Topological Slice Ordering**: Slices are ordered `F -> A -> B1 -> B2 -> B3 -> C -> D -> E`. Establishing Slice F first secures the deployment pipeline before any subsequent feature slice is merged and deployed.

---

## 3. Top-3 Implementation Considerations & Weakest Points

No substantive design flaws remain. The following items represent implementation considerations to observe during coding:

1. **`s7_operations` JSON Policy Rehydration Validation ([`internal/kernel/s7/s7.go`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L156-L161))**:
   - *Note*: Rehydration of `policy_json` from the database projection must fail closed if the JSON payload is corrupt or fails schema validation, rather than silently defaulting struct fields.
2. **`s7.Begin` Idempotency Guard in `Core.Flush` ([`internal/channel/channel.go`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L134,L179-L181))**:
   - *Note*: When `Core.Flush` processes a retried row on a subsequent tick, calling `s7.Begin` must be an absolute no-op if the operation already exists, preserving the active attempt count and `next_attempt_at` timestamp.
3. **Shared Toolchain & Vulnerability Verification Helper ([`scripts/p0-accept.sh`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L369-L379), [`scripts/deploy.sh`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L376-L379))**:
   - *Note*: To prevent verification drift between the acceptance signing harness and the deployment script, the version check and `govulncheck` invocation should be factored into a shared shell helper.

---

VERDICT: PASS

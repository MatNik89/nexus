# Design Review Round 5: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v5)

**Reviewed Revision**: `3bbdf0d5ae25c345a9ba6f199b5a034ae11f6293` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v5, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-4 Findings & Notes Disposition

All 3 substantive findings from Codex Round 4 ([`docs/REVIEW-AUDIT-PLAN4-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN4-codex.md)) and all previous reviewer considerations are **CLOSED** in Plan v5:

1. **Codex F1 (HIGH) — Sibling Batch API Companion Binding & Cardinality**: **CLOSED**
   - *Plan v5 Reference*: [`docs/PLAN-AUDIT-FIXES.md:149-167, 221-251, 342-345`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L149-L167)
   - *Resolution*: Replaced unconstrained variadic siblings with a typed `Companion{Key: OperationID, Params: EnvelopeParams}`. `s7.Consume` strictly requires exactly one companion for durable operations and asserts `c.Key == g.OperationID` (mismatch/missing is `ErrAttemptNotAuthorized`). `Report` and `Cancel` take a Landing-driven callback `build func(Landing) Companion` where S7 first computes the typed `Landing` (`Retry{NextAt}` | `Terminal` | `Unknown` | `Succeeded` | `Cancelled`) and the owner constructs the single companion bound to that landing. Channel marks carry `operation_id` verified against `delivery:<id>` in both PayloadValidator and projection. Detector 9e explicitly tests companion omission and cross-delivery substitution.

2. **Codex F2 (HIGH) — Ungoverned Bot API Call Sites**: **CLOSED**
   - *Plan v5 Reference*: [`docs/PLAN-AUDIT-FIXES.md:252-269, 338-341`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L252-L269)
   - *Resolution*: Enumerated the complete call-site table across `telegram.go` and `telegram_picker.go` (`sendMessage`, `sendRichMessage`, `getUpdates`, `setMyCommands`, `getMe`, `sendChatAction`, and picker `sendMessage`/`answerCallbackQuery`/`editMessageReplyMarkup`/`editMessageText`). Assigned explicit governed kinds (`delivery`, `poll`, `control`, `ui`) and policies (`PolicyDelivery`, `PolicyPoll`, `PolicyControl`, `PolicyUI`). Ephemeral UI actions are governed under `PolicyUI` (`MaxAttempts: 1`, `EffectReversible`, terminal on failure, in-memory). Detector 9d verifies that every method in the call-site table enforces grant authorization.

3. **Codex F3 (HIGH) — Structured Output Re-Ask S7 Ownership Migration**: **CLOSED**
   - *Plan v5 Reference*: [`docs/PLAN-AUDIT-FIXES.md:292-300, 346-348`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L292-L300)
   - *Resolution*: Migrated `provider.Extract` to `s7.Execute(ctx, op, target, PolicyStructured, attempt)` with `PolicyStructured{MaxAttempts: 2, RetryableCodes: {"invalid_general"}}`. Extractor only validates and proposes `OutcomeFailedRetryable, "invalid_general"` for GENERAL class, while SECURITY/EFFECT classes return `OutcomeFailedTerminal` (fail closed, no repair). Extractor retains its salvage ladder without calling `Issue`/`Next`. Detector 9f verifies structured re-ask ownership ablation.

4. **Round-4 Implementation Notes Disposition**: **CLOSED**
   - *Streaming Terminal Failure*: Explicitly specified at [`docs/PLAN-AUDIT-FIXES.md:300-301`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L300-L301) that stream failures after the first delivered byte are strictly non-retryable terminal.
   - *Non-Durable Sibling Rejection*: Documented at [`docs/PLAN-AUDIT-FIXES.md:158-159`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L158-L159) that non-durable operations reject companions.
   - *Injected Clock Consistency*: Documented at [`docs/PLAN-AUDIT-FIXES.md:205-208`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L205-L208) that authority operations use the injected clock across lease recovery and due-date evaluations.

---

## 2. Technical Evaluation of Major Plan Components

### A. Egress Owner & Bounded Buffers (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Centralizes outbound HTTP configuration across `provider` and `telegram`. Eliminates ambient proxy / default transport leaks, enforces mandatory `ReceiptSink`, exports `ErrPreWire`, and applies canonical `Endpoint{Host, Port}` normalization.
- **Body & Stream Bounds**: Implements bounded readers on JSON payloads (`io.LimitReader(body, max+1)`) and streams (`maxStreamBytes > maxStreamTotal`), securing against memory exhaustion while preserving detector observability.

### B. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Single Retry Owner**: Retains absolute S7 ownership over attempt counts, deadlines, backoff, and cancellations across tool execution, outbox delivery, provider completions, and structured re-asks.
- **Typed Mandatory Companion & Atomic Multi-Event Commits**: `Consume(g, Companion)` and Landing-driven `Report`/`Cancel` coordinate with `journal.AppendBatch` to commit paired S7 + outbox transitions in a single serialized SQLite transaction, eliminating split-brain desynchronization.
- **Lease Recovery & E9 Compliance**: Unconsumed `AUTHORIZED` leases expired or rehydrated without `attempt_started` transition via `s7.lease_revoked` (`AUTHORIZED -> PLANNED`) to issue fresh grants with new nonces without advancing consumed attempt counts. Records with `attempt_started` rehydrate strictly to `UNKNOWN` without re-granting, enforcing E9.
- **Synchronous Driver (`s7.Execute`)**: Encapsulates retry loops and backoff sleeps behind S7 for both chat completions and structured output re-asks.

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

1. **`PolicyUI` Non-Durable In-Memory Operation Lifecycle ([`docs/PLAN-AUDIT-FIXES.md:262-266`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L262-L266))**:
   - *Observation*: For `PolicyUI` operations (`sendChatAction`, picker buttons), `Durable == false`. S7 must verify that non-durable operations do not write to the `s7_operations` SQLite projection, do not require companion envelope parameters, and cleanly record one-shot attempt lifecycle in memory.
2. **`PolicyStructured` Closure Re-Ask Output Capture ([`docs/PLAN-AUDIT-FIXES.md:292-300`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L292-L300))**:
   - *Observation*: In `provider.Extract`, the attempt closure passed to `s7.Execute` captures intermediate decode and salvage artifacts across retry attempts. Implementation must ensure that context cancellation cleanly terminates the attempt loop without leaking unvalidated partial structs.
3. **Landing Builder Nil-Handling for Non-Durable / Ambiguous Operations ([`docs/PLAN-AUDIT-FIXES.md:161-167`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L161-L167))**:
   - *Observation*: For non-durable operations or outcomes where no companion event is emitted (e.g. `LandingUnknown`), `Report`/`Cancel` should accept a nil builder or ignore the companion, ensuring no empty or spurious envelopes are passed to `journal.AppendBatch`.

---

VERDICT: PASS

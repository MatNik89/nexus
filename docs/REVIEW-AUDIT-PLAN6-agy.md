# Design Review Round 6: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v6)

**Reviewed Revision**: `64084cd6b054238ae2b591ae9f6a6ae5fb2cb5c6` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v6, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-5 Findings & Notes Disposition

All 3 substantive findings from Codex Round 5 ([`docs/REVIEW-AUDIT-PLAN5-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN5-codex.md)) and all previous reviewer considerations are **CLOSED** in Plan v6:

1. **Codex F1 (HIGH) — Unpaired `Next` Exhaustion & Deadline Terminalization**: **CLOSED**
   - *Plan v6 Reference*: [`docs/PLAN-AUDIT-FIXES.md:147-154, 243-245, 347-350`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L147-L154)
   - *Resolution*: Updated `Next(op, build func(Landing) Companion)` to accept the owner's terminal landing builder. Upon attempt exhaustion (`attempts >= MaxAttempts`) or deadline expiry (`now >= deadline`), S7 computes `Landing{Terminal}`, invokes `build(LandingTerminal)` to produce the `FAILED` companion (`channel.outbound_failed`), and commits BOTH S7 state and the outbox transition in ONE `journal.AppendBatch` transaction. Eliminates any observable `S7=FAILED / outbox=PENDING` split-brain state. Detector 5 verifies atomic landing under seam ablation.

2. **Codex F2 (HIGH) — Structured Output Attempt Accounting & Ownership**: **CLOSED**
   - *Plan v6 Reference*: [`docs/PLAN-AUDIT-FIXES.md:314-329, 378-382`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L314-L329)
   - *Resolution*: Unified initial generation and re-ask into a single S7 execution: `ExtractVia(ctx, e, class, validate, op, target, generate func(ctx, g s7.Grant, reask bool) ([]byte, error))` driving `s7.Execute(ctx, op, target, PolicyStructured, attempt)`. Attempt 1 runs `generate(g, false)` + strict validation; GENERAL class validation failure returns `OutcomeFailedRetryable, "invalid_general"`. Attempt 2 runs `generate(g, true)` (re-ask) + validation. `PolicyStructured{MaxAttempts: 2}` accurately counts initial request + exactly ONE re-ask (never a 3rd attempt). SECURITY/EFFECT validation failures return `OutcomeFailedTerminal` (no repair). Salvage runs post-terminalization outside the transport count. Detector 9f (a–d) asserts attempt limits and ownership ablation.

3. **Codex F3 (HIGH) — Telegram Classification & Poll Retry Code Vocabulary Alignment**: **CLOSED**
   - *Plan v6 Reference*: [`docs/PLAN-AUDIT-FIXES.md:270-285, 486-490`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L270-L285)
   - *Resolution*: Established kind/effect-aware Telegram classification with a closed code vocabulary (`transport_prewire`, `http_429`, `http_4xx`, `http_5xx`, `transport_postwrite`, `malformed_reply`, `local_refused`). Effectful delivery treats 5xx/post-write as `ErrAmbiguousSend -> UNKNOWN` (E9 compliance). Read-only/idempotent operations (`getUpdates`, `getMe`, `setMyCommands`) propose `DefiniteFailure{code, Retryable}` to S7. `PolicyPoll` explicitly includes `{transport_prewire, http_429, http_5xx, transport_postwrite, malformed_reply}`. Detector 6 tests transient poll retry across all failure codes in a table.

4. **Round-5 Implementation Notes Disposition**: **CLOSED**
   - *Non-Durable & In-Memory Operations*: Explicitly specified that non-durable operations (`PolicyUI`, `PolicyPoll`, `PolicyControl`, `PolicyStructured`, `PolicyProvider`) pass a nil builder and do not emit companions ([`docs/PLAN-AUDIT-FIXES.md:153-154, 158-159, 173-176`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L153-L154)).
   - *Nil / Zero Builder Rules*: Formally defined that for durable operations the builder returns a zero `Companion{}` strictly for `Landing{Unknown}` (where outbox is already parked `UNKNOWN` by `Consume`), while all other durable landings mandate a companion with `Key == op` ([`docs/PLAN-AUDIT-FIXES.md:173-176`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L173-L176)).

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

1. **`ExtractVia` Legacy Callers Compatibility ([`docs/PLAN-AUDIT-FIXES.md:314-329`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L314-L329))**:
   - *Observation*: Existing callers of `provider.Extract` should be completely refactored to `ExtractVia` to ensure no lingering code paths perform un-governed extraction or maintain an independent re-ask invocation loop.
2. **`Next(op, build)` Nil Builder Enforcement on Durable Operations ([`docs/PLAN-AUDIT-FIXES.md:147-154`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L147-L154))**:
   - *Observation*: For `Durable: true` operations, `s7.Next` must enforce that `build != nil`, immediately returning an error if an internal caller invokes `Next` on a durable operation without providing the mandatory terminalization companion builder.
3. **Pre-Wire vs Post-Write Error Classification in Egress Transport ([`docs/PLAN-AUDIT-FIXES.md:276-285`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L276-L285))**:
   - *Observation*: The egress transport layer must strictly distinguish pre-wire failures (DNS, connection refusal, TLS handshake failure before request transmission) from post-write errors (reset, read timeout after request headers/body sent), ensuring `transport_prewire` vs `transport_postwrite` codes are accurately propagated to S7.

---

VERDICT: PASS

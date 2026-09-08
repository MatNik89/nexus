# Design Review Round 4: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v4)

**Reviewed Revision**: `fdb39dc6616274d0b27a857d3dea5dcc691447b4` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v4, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), [`docs/HARNESS-SPEC.md:1386-1393`](file:///home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md#L1386-L1393) (SPEC P0.2), [`docs/HARDQ-CONSOLIDATED.md`](file:///home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md) (HARDQ A2, B2, B7, C3, E9)

---

## 1. Round-3 Findings & Notes Disposition

All 5 substantive findings from Round 3 ([`docs/REVIEW-AUDIT-PLAN3-codex.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN3-codex.md)) and all previous reviewer notes are **CLOSED** in Plan v4:

1. **Codex F1 (HIGH) — Durable `AUTHORIZED` Grant Recovery After Crash/Expiry**: **CLOSED**
   - *Plan v4 Reference*: [`docs/PLAN-AUDIT-FIXES.md:163-174, 191-201, 302-304`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L163-L174)
   - *Resolution*: Unconsumed `AUTHORIZED` leases expired or rehydrated without an `attempt_started` event are recovered via `s7.lease_revoked{op, attempt_no, nonce_hash}` (`AUTHORIZED -> PLANNED`), issuing a fresh grant with a new nonce without advancing `attempts` (which counts only consumed attempts). Old nonces are dead and projection marks them revoked. Records with `attempt_started` rehydrate strictly to `UNKNOWN` without re-granting, enforcing E9. Detector 9b directly asserts lease recovery across crash and expiry boundaries.

2. **Codex F2 (HIGH) — Multi-Step Append Stranding Definitely-Unsent Delivery**: **CLOSED**
   - *Plan v4 Reference*: [`docs/PLAN-AUDIT-FIXES.md:147-158, 222-240, 305-308`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L147-L158)
   - *Resolution*: S7 acts as the atomic committer for paired S7 and outbox state transitions. `s7.Consume`, `s7.Report`, and `s7.Cancel` accept sibling envelope parameters (`...contracts.EnvelopeParams`) and commit both the S7 event and the channel outbox event (`outbound_unknown`, `outbound_sent`, `outbound_failed`, or `outbound_pending`) in ONE `journal.AppendBatch` transaction. Detector 9c asserts pair atomicity and proves that no definitely-unsent delivery can be stranded `UNKNOWN`.

3. **Codex F3 (HIGH) — Ungoverned Telegram Bot API Calls (`getUpdates`, `setMyCommands`)**: **CLOSED**
   - *Plan v4 Reference*: [`docs/PLAN-AUDIT-FIXES.md:241-255, 381-386, 309-311`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L241-L255)
   - *Resolution*: Every physical Bot API invocation passes through `call(ctx, method, req, out, g s7.Grant, kind)` which recomputes the expected operation and target identity (`delivery:<id>`, `poll:tg:<n>`, `control:tg:setMyCommands`) and calls `s7.Consume` immediately before `client.Do`. Governed under `PolicyPoll` and `PolicyControl`. Detector 9d verifies that reuse, missing, or swapped grants fail `ATTEMPT_NOT_AUTHORIZED` with zero socket calls.

4. **Codex F4 (HIGH) — Provider Retry Orchestration in Planner (Second Retry Owner)**: **CLOSED**
   - *Plan v4 Reference*: [`docs/PLAN-AUDIT-FIXES.md:175-180, 263-272, 297-299`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L175-L180)
   - *Resolution*: S7 owns the synchronous retry execution loop via `s7.Execute(ctx, op, target, PolicyProvider, attemptFunc)`. The planner submits a single operation callback and contains zero sleep, loop, or `Next` calls. Ownership ablation detector 8b verifies that forcing S7 `MaxAttempts=1` halts second transport attempts without planner changes.

5. **Codex F5 (MED) — Supervisor Overwriting Originating Terminal Health Class**: **CLOSED**
   - *Plan v4 Reference*: [`docs/PLAN-AUDIT-FIXES.md:392-399, 405-407`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L392-L399)
   - *Resolution*: `Adapter.Run` returns a typed `ClassifiedError` and the supervising composition root persists that exact class unchanged (`remote_rejected` persists as `remote_rejected`), defaulting to `substrate` only for unclassified errors, recovered panics, or health substrate failures. Detector D1 asserts that `remote_rejected` is preserved in the final health projection.

6. **Round-3 Notes & Vocabulary Alignment**: **CLOSED**
   - *State-name mapping*: Explicitly documented at [`docs/PLAN-AUDIT-FIXES.md:135-139`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L135-L139) mapping code constants (`PLANNED`, `AUTHORIZED`, `RUNNING`, `FAILED_RETRYABLE`, `FAILED`, `CANCELLED`, `UNKNOWN`) to SPEC P0.2 terminology.
   - *Shared Release Gate*: Factored into `scripts/lib/release-gate.sh` ([`docs/PLAN-AUDIT-FIXES.md:448-453`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L448-L453)) shared by both `p0-accept.sh` and `scripts/deploy.sh`.
   - *Fail-Closed Policy Rehydration & Idempotent Begin*: Documented at [`docs/PLAN-AUDIT-FIXES.md:140-142, 181`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L140-L142).

---

## 2. Technical Evaluation of Major Plan Components

### A. Egress Owner & Bounded Buffers (Slice A)
- **Shared Egress Owner (`internal/foundation/egress`)**: Fully centralizes outbound HTTP configuration across `provider` and `telegram`. Eliminates `http.DefaultTransport` leakage, enforces mandatory `ReceiptSink` fail-closed at construction, and exports `ErrPreWire` for definite pre-wire failure classification.
- **Canonical Endpoint Unit**: Normalized `Endpoint{Host, Port}` ensures consistent allowlisting, receipt recording, and S7 target specification across non-default and default scheme ports.
- **Body & Stream Bounds**: Enforces `io.LimitReader` bounds on both full JSON payloads and streaming responses, preventing memory exhaustion attacks while preserving detector observability (`maxStreamBytes > maxStreamTotal`).

### B. Full S7 Retry Engine Architecture (Slices B1, B2, B3)
- **Sole Retry Owner**: Retains absolute S7 ownership over attempts, deadlines, backoff, and cancellations across tools, delivery, and provider operations.
- **Atomic Batch Commit**: Unifies S7 state transitions and channel outbox transitions through single `journal.AppendBatch` transactions in `s7.Consume`, `s7.Report`, and `s7.Cancel`, eliminating split-brain desynchronization.
- **Lease Recovery & E9 Compliance**: Unconsumed grants transition via `lease_revoked` to `PLANNED` for fresh authorization without physical attempt count increments, while attempted records (`attempt_started` logged) strictly transition to `UNKNOWN` and are never automatically retried.
- **Synchronous Execution API**: `s7.Execute` encapsulates retry loops and backoff sleeps behind S7, keeping planner and adapter callers purely reactive.

### C. Context Budgeting & Framing Integrity (Slice C)
- **Wire Message Measurement**: Enforces `ContextHardLimitTokens` directly on the final assembled wire payload (including system prompt, tool definitions, and timestamps) immediately before dispatch, failing closed on overflow.
- **Framing Desynchronization Defense**: Exceeding `maxFrameBytes` on hello or chat frames returns an error frame and closes the UDS connection, preventing malicious or corrupted trailing frame bytes from desynchronizing the stream.

### D. Channel Health & Supervised Polling (Slice D)
- **Failure-Independent Health Projection**: Writes atomic snapshot to `channel_health.json` independent of SQLite/journal availability.
- **S7-Governed Polling & Control**: Polling and command registration are tracked under `PolicyPoll` and `PolicyControl`, preventing tight error polling loops and governing all wire requests.
- **Supervisor Transparency**: Preserves originating classification without masking terminal remote rejections under generic substrate labels.

### E. Config Resolution & Gated Toolchain Deployment (Slices E & F)
- **Dynamic Doctor Resolution**: `nexus doctor` shares typed config resolution with the production engine, eliminating false positives/negatives on custom environment variable names.
- **Hardened Release & Deploy Gates**: `go.mod` toolchain directive (`toolchain go1.26.6`) together with binary version checking and `govulncheck -mode=binary` in `scripts/lib/release-gate.sh` guarantees that neither manual releases nor automated deployments can install vulnerable binaries.

---

## 3. Top-3 Implementation Considerations & Weakest Points

No substantive design flaws remain. The following points represent specific implementation nuances to verify during coding:

1. **Streaming Partial Failure vs `s7.Execute` Abort Semantics ([`docs/PLAN-AUDIT-FIXES.md:271-272`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L271-L272))**:
   - *Observation*: In `s7.Execute`, if a streaming callback encounters a network or parsing failure after yielding initial tokens to the user surface, the callback must return a non-retryable terminal outcome (e.g. `OutcomeFailedTerminal`) so S7 does not re-issue a second attempt that would emit duplicate or garbled text.
2. **Sibling Parameter Validation on S7 Calls ([`docs/PLAN-AUDIT-FIXES.md:147-154`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L147-L154))**:
   - *Observation*: `s7.Consume`, `s7.Report`, and `s7.Cancel` must explicitly validate that `siblings` slice is empty when `Policy.Durable == false`, returning an error if an in-memory caller mistakenly supplies durable sibling parameters.
3. **Monotonic / Injected Clock Consistency ([`docs/PLAN-AUDIT-FIXES.md:163-170`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L163-L170))**:
   - *Observation*: Lease expiration evaluation (`now >= lease_expires_at`) in `Next()` must strictly consult the authority's injected clock (`clock()`) to preserve deterministic timing simulation during chaos and restart unit tests.

---

VERDICT: PASS

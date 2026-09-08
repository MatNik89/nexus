# Design Review Round 2: Full-Audit Remediation Plan (`PLAN-AUDIT-FIXES.md` v2)

**Reviewed Revision**: `c89897ba10f02963b14e1d7925578f1909104e34` (branch `slice/p1-audit`)  
**Verdict**: `PASS`  
**Source Audit**: [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md) (Findings F1–F9)  
**Plan Under Review**: [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v2, Slices A–F)  
**Binding Constitution**: `AGENTS.md`, [`docs/ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md) (E11, S7 sole retry owner, S8.1 ContextBudget, fail-closed)

---

## 1. Round-1 Findings Disposition

All 3 substantive findings from the Round 1 review ([`docs/REVIEW-AUDIT-PLAN-agy.md`](file:///home/matej/HARNESS/nexus/docs/REVIEW-AUDIT-PLAN-agy.md)) are **CLOSED** in Plan v2:

1. **Finding 1 (HIGH — S7 Scope & Grant Granularity in Slice B)**: **CLOSED**
   - *Plan v2 Reference*: [`docs/PLAN-AUDIT-FIXES.md:84-115`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L84-L115)
   - *Evidence & Resolution*: `internal/kernel/s7min/s7min.go` remains completely unchanged (P0 no-retry, strictly one grant per operation). S7 grants are minted per-row inside `channel.Core.Flush` with identity `delivery:<delivery_id>` and target `channel:<adapter_id>`, and consumed prior to `client.Do`. The self-repending `Reconcile(id, false) -> PENDING` loop is deleted from `Flush` (`internal/channel/channel.go:583-589`); definite transport/pre-wire errors park into a new durable `FAILED` outbox status (`UNKNOWN` is preserved strictly for ambiguous remote effects). The tradeoff is explicitly documented: transient delivery failures park terminally in P0 without auto-resend.
2. **Finding 2 (MEDIUM — ContextBudget Trimming vs Refusal & Missing Schema in Slice C)**: **CLOSED**
   - *Plan v2 Reference*: [`docs/PLAN-AUDIT-FIXES.md:126-153`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L126-L153)
   - *Evidence & Resolution*: `ContextHardLimitTokens int` (`json:"context_hard_limit_tokens"`) is added to `config.Config` (default `64000`, validated bounds `0 < limit <= 2_000_000` on `Resolve`). Enforcement happens at the wire message boundary in the planner (`MeasureMessages`/`EnforceMessages`) immediately before transport dispatch. All references to context trimming/truncation have been removed; over-budget requests strictly refuse fail-closed with `ErrOverBudget`.
3. **Finding 3 (MEDIUM — Unbounded Stream Body Reader in Slice A/C)**: **CLOSED**
   - *Plan v2 Reference*: [`docs/PLAN-AUDIT-FIXES.md:58-64, 150-153`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L58-L64)
   - *Evidence & Resolution*: `provider.Stream` wraps `resp.Body` in `io.LimitReader(resp.Body, maxStreamBytes)` before passing it to `bufio.NewScanner` (`internal/llm/provider/provider.go:272-302`), providing transport-layer byte bounding in tandem with the planner's accumulation ceiling (`maxStreamTotal`).

---

## 2. Slice B Assessment & Trade-Off Evaluation

Slice B ([`docs/PLAN-AUDIT-FIXES.md:84-125`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L84-L125)) correctly models the P0 delivery honesty contract without violating S7 invariants:
- **Exact Capability Binding**: `Core.Flush` issues exactly one grant per pending row (`OperationID("delivery:" + o.DeliveryID)` bound to `TargetID("channel:" + a.id)`), which the adapter consumes immediately before executing the HTTP request. Reusing or omitting a grant fails closed as `ErrAttemptNotAuthorized`.
- **Honest Terminal State Separation**: Definite pre-wire refusals, DNS/dial failures, and HTTP 4xx errors commit the row to `FAILED`. `UNKNOWN` remains strictly isolated to ambiguous outcomes where remote receipt cannot be disproven (`ErrAmbiguousSend`).
- **Elimination of Uncontrolled Retry**: Deleting `c.Reconcile(ctx, o.DeliveryID, false)` from `channel.go:585` guarantees that the 2-second polling loop in `telegram.go:433-442` cannot repeatedly re-send failed deliveries outside of S7.
- **Trade-Off Honesty**: The plan honestly states that in P0, any definite failure terminally parks the delivery until human/operator reconciliation, deferring multi-attempt retry scheduling and backoff policies to P2.

---

## 3. Top-3 Weakest Points & Implementation Notes

No new substantive design flaws remain. The following items represent implementation considerations:

1. **UDS Framing Desynchronization on Frame-Limit Breach ([`internal/app/daemon/daemon.go:201-210`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L201-L210), [`docs/PLAN-AUDIT-FIXES.md:147-149`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L147-L149))**:
   - *Note*: If a client sends a chat frame exceeding `maxFrameBytes`, emitting an `error` frame while keeping the connection open could leave residual bytes from the oversized line in the buffer, corrupting subsequent `ReadString('\n')` parsing.
   - *Recommendation*: Close the connection on any frame length violation (same as hello frame failure), or explicitly drain up to the next newline.
2. **Substrate Failure Clean Teardown ([`cmd/nexus/main.go:289-296`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L289-L296), [`internal/channel/telegram/telegram.go:428-445`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L428-L445), [`docs/PLAN-AUDIT-FIXES.md:179-184`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L179-L184))**:
   - *Note*: When `Adapter.Run` encounters a durable substrate error and exits, the supervising daemon loop should cancel the adapter context to stop background goroutines and close open connections promptly.
3. **Transport vs Accumulator Streaming Ceiling Hierarchy ([`internal/llm/provider/provider.go:272-302`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider.go#L272-L302), [`internal/llm/planner/planner.go:330-345`](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L330-L345), [`docs/PLAN-AUDIT-FIXES.md:61, 150`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L61))**:
   - *Note*: To ensure the planner accumulator ceiling detector (Slice C #4) is observed independently of the transport reader cap, `maxStreamBytes` (provider HTTP transport limit) should be set comfortably above `maxStreamTotal` (planner text accumulation limit).

---

VERDICT: PASS

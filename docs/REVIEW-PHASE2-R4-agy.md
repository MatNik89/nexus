# REVIEW-PHASE2-R4-agy.md — Phase-2 Verification Round 4 (Narrow)

**Target Ref:** `slice/p0-phase2` @ commit `f1b9d3e`  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Narrow verification of diff `fbd8dc4..f1b9d3e` folding the 3 residual round-3 findings (`codex #2`, `#4`, `#7`).

---

## 1. Verification of Round 3 Folds

| # | Finding & Source | Resolution & Code Location | Status |
| :- | :--- | :--- | :--- |
| 1 | **Unconditional Redirect Refusal**<br>`codex #2a` (R3) | `provider.CheckRedirect` unconditionally returns `fmt.Errorf("provider: redirects refused — one grant, one physical request (fail closed)")` (`provider.go:135-139`). An allowlisted bouncer is refused without hitting the redirect target. Verified in [`provider_test.go:540-568`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L540-L568) (redirect target receives 0 requests). | `[OK]` |
| 2 | **Planner S7 Failure Landing**<br>`codex #2b` (R3) | `planner.landFailure` inspects `c.auth.State(op)`: unconsumed local refusals cancel the operation (`AttemptCancelled`), while consumed transport errors report `OutcomeFailedTerminal` (`planner.go:79-85`). Landing errors are joined and propagated via `errorsJoin` (`planner.go:87-92, 125, 138`). Verified in [`planner_test.go:153-186`](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner_test.go#L153-L186). | `[OK]` |
| 3 | **S7-Owned Attempt Deadline Context**<br>`codex #4` (R3) | `s7min.Authority.AttemptContext(ctx, op, callDeadline)` derives the execution context under `min(callDeadline, expires)` and enforces `AttemptRunning` state (`s7min.go:211-230`). `EffectPath.RunTool` consumes this S7-owned context (`effectpath.go:402-406`). Verified in [`effectpath_test.go:597-623`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L597-L623). | `[OK]` |
| 4 | **STARTED Append Failure Cancels Attempt**<br>`codex #7` (R3) | In `EffectPath.RunTool`, a failed `onStarted` hook cancels the in-memory attempt (`p.grants.Cancel(op)`) and returns the error without executing or reporting `UNKNOWN`/`LOST` (`effectpath.go:389-397`). The durable journal remains at `AUTHORIZED`, replaying legally to `CANCELLED`. Verified in [`effectpath_test.go:578-595`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L578-L595). | `[OK]` |

---

## 2. Adjudication of S7 Deadline/Cancel Ownership

`s7min.Authority.AttemptContext` establishes S7 as the sole authority for attempt execution context derivation. By requiring the operation to be in `AttemptRunning` (grant already consumed) and calculating the deadline as `min(callDeadline, expires)`, S7 guarantees that:
- Non-running/unauthorized operations cannot receive execution contexts.
- Execution cannot outlive the grant's cryptographic validity or the call's deadline.
- Tool executors operate strictly within S7-governed boundaries.

This fully satisfies the S7-owns-deadline/cancel invariant for P0.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Host-String Egress Allowlist vs Resolved-IP Pinning ([`internal/llm/provider/provider.go:61-66`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider.go#L61-L66))**:
   `provider.APIKey` validates host strings against `cfg.EgressAllow` and enforces TLS (except for local loopback parsed via `net.ParseIP().IsLoopback()`) on construction and redirects (`CheckRedirect`). Resolved-IP pinning and DNS rebinding protections (E11) remain explicitly declared as owned by S6.3 when the full outbound dialer lands.
2. **`EnvelopeParams.PayloadHash` Recomputation Seam ([`cmd/nexus/main.go:98`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L98), [`internal/kernel/loop/loop.go:90`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop.go#L90))**:
   Callers pass `"recomputed"` placeholder because `journal.Append` calculates the SHA-256 hash over the sanitized payload after redaction.
3. **P0 In-Process Sandbox Refusal Floor ([`internal/app/daemon/daemon.go:214-220`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L214-L220))**:
   `noSandbox` in the daemon fails closed on any `ExecProcess` tool call until T26 binds the `bwrap` executor.

---

## 4. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 24 packages passed uncached (exit code 0).

---

VERDICT: PASS

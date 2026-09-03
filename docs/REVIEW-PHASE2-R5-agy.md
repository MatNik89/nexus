# REVIEW-PHASE2-R5-agy.md — Phase-2 Verification Round 5 (Final Narrow)

**Target Ref:** `slice/p0-phase2` @ commit `84606a7`  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Narrow verification of diff `f1b9d3e..HEAD` folding the 2 residual round-4 findings (`codex #3`, `#4`).

---

## 1. Verification of Round 4 Folds

| # | Finding & Source | Resolution & Code Location | Status |
| :- | :--- | :--- | :--- |
| 1 | **S7 Race-Free Cancel Propagation**<br>`codex #4` (R4) | `operation.execCancel` is stored under `Authority.mu` within `AttemptContext` (`s7min.go:68, 220-237`). When `Authority.Cancel(op)` is called, it transitions state and immediately invokes `rec.execCancel()` under the lock (`s7min.go:208-211`), propagating cancellation to running executors race-free. Verified in [`s7min_test.go:201-230`](file:///home/matej/HARNESS/nexus/internal/kernel/s7min/s7min_test.go#L201-L230). | `[OK]` |
| 2 | **Causal Planner S7 Landing RED**<br>`codex #3` (R4) | `TestFailedPlanLandsHonestS7State` captures `g.OperationID` via `failingChat` and directly asserts authority states: (a) unconsumed local refusal → `AttemptCancelled`, (b) consumed transport error → `AttemptFailed`, (c) adversarial self-reporting adapter → illegal transition caught and surfaces `"S7 landing"` error (`planner_test.go:153-221`). Reverting `landFailure` fails the test. | `[OK]` |

---

## 2. Adjudication of Cancel-Propagation & Test Causality

1. **S7 Cancel-Propagation (`s7min.go`)**:
   - `AttemptContext` holds `a.mu` while checking that `state == AttemptRunning`, deriving the deadline, and registering `rec.execCancel = cancel`.
   - `Cancel` holds `a.mu` while transitioning the attempt state to `AttemptCancelled` and invoking `rec.execCancel()`.
   - This eliminates any state-check/use race between context registration and attempt cancellation, fully satisfying the SPEC P0.2 requirement that attempt cancellation propagates to running executions.
2. **Planner Landing Detector (`planner_test.go`)**:
   - By capturing `g.OperationID` across all 3 legs, the detector directly observes the authoritative S7 state. If `landFailure` were removed or failed to cancel/report, the test fails deterministically on the unconsumed or surfaced-error legs.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Host-String Egress Allowlist vs Resolved-IP Pinning ([`internal/llm/provider/provider.go:61-67`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider.go#L61-L67))**:
   `provider.APIKey` validates host strings against `cfg.EgressAllow` and enforces TLS (except for local loopback parsed via `net.ParseIP().IsLoopback()`) on construction, while categorically rejecting all redirects. Resolved-IP pinning and DNS rebinding protections (E11) remain explicitly declared as owned by S6.3 when the full outbound dialer lands.
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

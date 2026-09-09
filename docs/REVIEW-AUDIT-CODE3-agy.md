# Adversarial Code Review Round 3: Audit Remediation Stack (Slices F, A, C, E, B1, B2, B3, D)

- **Reviewer:** agy
- **Revision Under Review:** `457c56d86c2d19f16904ba8ff44f412b7b39b53d` (worktree `/home/matej/HARNESS/nexus-b`, branch `slice/audit-b`)
- **Diff Range:** `fdb39dc..457c56d`
- **Plan Reference:** [`docs/PLAN-AUDIT-FIXES.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md) (v12, converged)
- **Source Audit:** [`docs/AUDIT-FULL-codex-2026-09-08.md`](file:///home/matej/HARNESS/nexus/docs/AUDIT-FULL-codex-2026-09-08.md)

---

## 1. Executive Summary

The audit remediation stack in `slice/audit-b` at revision `457c56d` implements the architecture specified in Plan v12 and closes findings F1–F9 at their owner boundaries.

- **Round-2 Finding Resolution:** The round-2 acceptance test suite failure (`acceptance_linux_test.go:220` missing bot ID) was **CLOSED** in commit `63a02bc`, and `internal/acceptance` now passes cleanly.
- **Detector Matrix & Fault Seams:** Commit `457c56d` comprehensively adds the full B/D detector matrix and journal fault seams (`SetAppendFault`), verifying atomic landing pairs, kind/method substitution tables, poll transient tables, and provenance isolation.
- **Remaining Defect:** A regression introduced in commit `9ce4dd0` causes `s7.Authority.AttemptContext` to derive context deadlines using `context.WithTimeout(ctx, remaining)` instead of preserving the exact deadline timestamp via `context.WithDeadline(ctx, deadline)`. This causes nanosecond time-drift against real clocks and year-drift against injected epoch test clocks, failing two tests in `internal/kernel/effectpath` (`TestCallDeadlinePropagatedIntoExecution` and `TestAttemptContextIsS7Owned`).

---

## 2. Round-2 Finding Disposition

| Round-2 Finding | Commit & Resolution | Status |
| :--- | :--- | :--- |
| **Acceptance fakeBot missing bot ID in `/getMe` response:** `acceptance_linux_test.go:220` omitted `"id"`, causing `telegram.go:742` to fail startup with substrate fatal error and stopping 6 acceptance tests. | [`63a02bc0e0a150983f28f816b816fa02fcf28117`](file:///home/matej/HARNESS/nexus-b/internal/acceptance/acceptance_linux_test.go#L225) updated `/getMe` mock response to `{"ok":true,"result":{"id":1,"is_bot":true,"username":"acceptance_bot"}}`. Furthermore, [`internal/channel/telegram/telegram.go:740`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram.go#L740) now classifies missing bot ID as a degraded transport error rather than fatal substrate stoppage. | **CLOSED** |

---

## 3. Per-Slice Code Verification

### Slice F: Toolchain Floor & Vulnerability Gating (F3)
- [`scripts/p0-accept.sh:22-26`](file:///home/matej/HARNESS/nexus-b/scripts/p0-accept.sh#L22-L26) and [`scripts/lib/release-gate.sh:11`](file:///home/matej/HARNESS/nexus-b/scripts/lib/release-gate.sh#L11) enforce Go >= 1.26.6 (`readonly RELEASE_GO_FLOOR=1.26.6`) and run `govulncheck -mode=binary` on the release artifact prior to signature and deployment.

### Slice A: Shared Egress Owner & Provider Transport Caps (F1, F8)
- [`internal/foundation/egress/egress.go:215-285`](file:///home/matej/HARNESS/nexus-b/internal/foundation/egress/egress.go#L215-L285) acts as the single egress owner, emitting canonical lowercase `Host:Port` receipts without default transport fallbacks.
- [`internal/llm/provider/provider.go:189-220`](file:///home/matej/HARNESS/nexus-b/internal/llm/provider/provider.go#L189-L220) enforces bounded response bodies and streams via `io.LimitReader`.

### Slice C: Context Budget Wire Enforcement & Frame Limits (F5, F8)
- [`internal/kernel/budget/budget.go:61-98`](file:///home/matej/HARNESS/nexus-b/internal/kernel/budget/budget.go#L61-L98) and [`internal/llm/planner/planner.go:210-245`](file:///home/matej/HARNESS/nexus-b/internal/llm/planner/planner.go#L210-L245) enforce `ContextHardLimitTokens` at the wire boundary immediately prior to provider dispatch, refusing over-budget requests without silent truncation.
- [`internal/app/daemon/daemon.go:154-245`](file:///home/matej/HARNESS/nexus-b/internal/app/daemon/daemon.go#L154-L245) bounds UDS frames to `maxFrameBytes` (1 MiB), closing connections on oversize frames.

### Slice E: Config-Aware Doctor Preflight (F7)
- [`internal/preflight/doctor/doctor.go:75-140`](file:///home/matej/HARNESS/nexus-b/internal/preflight/doctor/doctor.go#L75-L140) resolves runtime configuration prior to executing diagnostics.

### Slice B1: S7 Engine (F2)
- [`internal/kernel/s7/s7.go:1-789`](file:///home/matej/HARNESS/nexus-b/internal/kernel/s7/s7.go#L1-L789) implements the full S7 retry engine with durable journal events (`s7.operation_begun`, `s7.attempt_granted`, `s7.operation_landed`, `s7.operation_reconciled`), in-memory projection rehydration, mandatory companion validation, lease recovery, and `SetAppendFault` seam for test verification.
- [`internal/kernel/s7/execute.go:1-110`](file:///home/matej/HARNESS/nexus-b/internal/kernel/s7/execute.go#L1-L110) implements synchronous retry driving under `Execute`.

### Slice B2: Governed Channel Outbox & Telegram Operations (F2)
- [`internal/channel/channel.go:210-380`](file:///home/matej/HARNESS/nexus-b/internal/channel/channel.go#L210-L380) implements outbox state machine (`PENDING`, `UNKNOWN`, `SENT`, `FAILED`) with paired atomic commits in `Core.Flush`.
- [`internal/channel/telegram/telegram.go:300-345,783-840`](file:///home/matej/HARNESS/nexus-b/internal/channel/telegram/telegram.go#L300-L345) governs all Bot API calls, binds kind/method to grant operations, and manages durable bot-bound registration (`control:tg:<bot-id>:setMyCommands:<hash>`) and reconciliation.

### Slice B3: Provider & Structured Output via S7 (F2)
- [`internal/llm/provider/structured.go:85-180`](file:///home/matej/HARNESS/nexus-b/internal/llm/provider/structured.go#L85-L180) runs `ExtractVia` through `s7.Execute` (`PolicyStructured`, MaxAttempts 2). Salvage ladder evaluates inside attempt 2's outcome decision. Returned value <=> S7 `SUCCEEDED`.

### Slice D: Channel Health Owner (F6)
- [`internal/channel/health/health.go:45-160`](file:///home/matej/HARNESS/nexus-b/internal/channel/health/health.go#L45-L160) provides the dedicated health owner writing `channel_health.json` via atomic file projection.

---

## 4. Substantive Findings

### Finding 1: [HIGH] `s7.Authority.AttemptContext` uses `context.WithTimeout` causing timestamp drift and test suite failures

- **Source Location:** [`internal/kernel/s7/s7.go:696-704`](file:///home/matej/HARNESS/nexus-b/internal/kernel/s7/s7.go#L696-L704)
- **Breaks:** `internal/kernel/effectpath` test suite (`CGO_ENABLED=0 go test ./internal/kernel/effectpath`)
- **Failed Tests:**
  1. `TestCallDeadlinePropagatedIntoExecution` ([`internal/kernel/effectpath/effectpath_test.go:571-573`](file:///home/matej/HARNESS/nexus-b/internal/kernel/effectpath/effectpath_test.go#L571-L573)): fails because `context.WithTimeout(ctx, remaining)` re-evaluates `time.Now()`, drifting from `c.Deadline` by elapsed nanoseconds.
  2. `TestAttemptContextIsS7Owned` ([`internal/kernel/effectpath/effectpath_test.go:621-623`](file:///home/matej/HARNESS/nexus-b/internal/kernel/effectpath/effectpath_test.go#L621-L623)): fails because with an injected epoch clock (`time.Unix(1000, 0)`), `g.ExpiresAt` is in 1970 (`1970-01-01 01:17:40`), but `context.WithTimeout(ctx, 60*time.Second)` evaluates against the host wall clock, assigning a 2026 deadline.

#### Mechanism:
In commit `9ce4dd0`, `AttemptContext` was modified to compute `remaining := deadline.Sub(now)` and call `context.WithTimeout(ctx, remaining)`:
```go
// internal/kernel/s7/s7.go:696-704
dctx, cancel := ctx, context.CancelFunc(func() {})
if !deadline.IsZero() {
    remaining := deadline.Sub(now)
    if remaining <= 0 {
        return nil, nil, fmt.Errorf("s7 attempt-context: attempt deadline already passed: %w", ErrAttemptNotAuthorized)
    }
    dctx, cancel = context.WithTimeout(ctx, remaining)
} else {
    dctx, cancel = context.WithCancel(ctx)
}
```
`context.WithTimeout(ctx, remaining)` always sets the context deadline to `time.Now().Add(remaining)` using Go's internal wall/monotonic clock. This causes two distinct failure modes:
1. **Real-time drift:** When `deadline` was already an absolute timestamp (`call.Deadline`), calculating `remaining` at `t1` and then passing it to `WithTimeout` at `t2` computes `t2 + (deadline - t1) = deadline + (t2 - t1)`. The resulting context deadline does not equal the intended call deadline.
2. **Injected clock desynchronization:** When the authority uses an injected clock (e.g. `time.Unix(1000, 0)`), `g.ExpiresAt` is `1060`. `remaining` is `60s`. `WithTimeout` sets the deadline to `time.Now() + 60s` in 2026, completely ignoring the grant's actual expiration timestamp.

Per Plan v12 ([`docs/PLAN-AUDIT-FIXES.md:162`](file:///home/matej/HARNESS/nexus/docs/PLAN-AUDIT-FIXES.md#L162)), `AttemptContext` should maintain exact deadline semantics: `deadline = min(call, grant expiry, operation deadline)`.

#### Concrete Fix:
In [`internal/kernel/s7/s7.go:696-704`](file:///home/matej/HARNESS/nexus-b/internal/kernel/s7/s7.go#L696-L704), check if `deadline` has expired using `a.now()`, and construct the context directly with `context.WithDeadline(ctx, deadline)`:

```diff
--- a/internal/kernel/s7/s7.go
+++ b/internal/kernel/s7/s7.go
@@ -692,13 +692,10 @@ func (a *Authority) AttemptContext(ctx context.Context, op contracts.OperationI
 	// The deadline is expressed in the AUTHORITY's clock; the context gets
-	// the REMAINING duration so an injected clock and the wall clock agree
-	// (an already-expired attempt is refused, fail closed).
+	// the exact deadline timestamp (an already-expired attempt is refused, fail closed).
 	dctx, cancel := ctx, context.CancelFunc(func() {})
 	if !deadline.IsZero() {
-		remaining := deadline.Sub(now)
-		if remaining <= 0 {
+		if !deadline.After(now) {
 			return nil, nil, fmt.Errorf("s7 attempt-context: attempt deadline already passed: %w", ErrAttemptNotAuthorized)
 		}
-		dctx, cancel = context.WithTimeout(ctx, remaining)
+		dctx, cancel = context.WithDeadline(ctx, deadline)
 	} else {
 		dctx, cancel = context.WithCancel(ctx)
 	}
```

---

## 5. Notes and Observations

1. **Extraction Scope Note:**
   `ExtractVia` is fully implemented and tested with the attempt 2 salvage ladder under `s7.Execute`. As noted in the updated plan, `ExtractVia` has no direct P1 production caller by design (tool-call extraction remains drift-typed via planner/loop).
2. **Health Supervisor Log Fallback:**
   The supervisor now logs `channel health update failed (substrate fatal)` when the health projection cannot be written, matching the specified fail-closed behavior.

---

VERDICT: FAIL

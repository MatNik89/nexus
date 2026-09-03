# REVIEW-PHASE2-R3-agy.md — Phase-2 Verification Round 3 (Narrow)

**Target Ref:** `slice/p0-phase2` @ commit `fbd8dc4`  
**Reviewer:** `agy` (Antigravity)  
**Input Ledger:** Fold verification of Round 2 findings from `docs/REVIEW-PHASE2-R2-codex.md` (2 NEW-ERROR + 8 UNFOLDED).

---

## 1. Verification of Round 2 Folds

| # | Finding & Source | Resolution & Code Location | Status |
| :- | :--- | :--- | :--- |
| 1 | **Artifact Identity: Tracked `cmd/nexus` Entry Point**<br>`codex #1, #10` | `.gitignore` pattern anchored to `/nexus` (`.gitignore:5`), un-ignoring `cmd/nexus/`. `git ls-files cmd/nexus/main.go` is non-empty. Build from a clean `git archive` succeeds (`CGO_ENABLED=0 go build -buildvcs=false ./cmd/nexus`). | `[OK]` |
| 2 | **Provider Target Binding & S7 Outcome Reporting**<br>`codex #2` | `provider.transport` verifies `g.TargetID == p.Target()` (`provider.go:167-170`), rejecting foreign-target grants before the wire. `planner.Plan` enforces fatal error handling if `c.auth.Report` fails (`planner.go:107-117`). Verified in [`provider_test.go:480-502`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L480-L502). | `[OK]` |
| 3 | **Re-Ask State Machine Leak Resolution**<br>`codex #3` | `Extract` inspects `e.auth.State(op)` on callback exit (`structured.go:136-144`): consumed-then-error attempts land `FAILED` via `land(OutcomeFailedTerminal)` (no permanent RUNNING leak), unconsumed callbacks have their operation cancelled and bytes ignored, and failed reports fail-closed. Verified in [`provider_test.go:505-538`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L505-L538). | `[OK]` |
| 4 | **Exact-Call Target Binding & Propagated Deadline**<br>`codex #4` | `ToolTarget` computes a length-prefixed SHA-256 digest across the entire `ToolCall` (IDs, arguments, schema hash, effect, kind, deadline, attempt, idempotency key, profile) (`effectpath.go:58-75`). `RunTool` propagates `call.Deadline` into `context.WithDeadline(ctx, call.Deadline)` (`effectpath.go:392-398`). Read-only deadline timeouts land `CANCELLED`. Verified in [`effectpath_test.go:522-575`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L522-L575). | `[OK]` |
| 5 | **Full Canonical Result Validation**<br>`codex #6` | `RunTool` passes results through the canonical `contracts.NewToolResult(call, ...)` constructor (`effectpath.go:417-425`), validating correlation, closed status, ordered timestamps, normalized output blocks, typed-error structure, and commit receipts without field-picking. | `[OK]` |
| 6 | **Durable STARTED Event Before Dispatch**<br>`codex #7` | `EffectPath` adds `SetStartObserver` (`effectpath.go:273-281, 386-391`), invoked by `loop.RunTurn` (`loop.go:246-248`) after grant consumption and immediately before executor dispatch. A mid-dispatch crash replays as `RUNNING` for reconciliation. If start append fails, dispatch is refused and parks `UNKNOWN`. | `[OK]` |
| 7 | **Secret Redaction on Tool Errors & UDS Frames**<br>`codex #9` | `errorObservation` applies `RedactText(l.redactor, toolErr.Error())` (`loop.go:138-170`), scrubbing known secret tokens from tool errors before they enter the model context, and tags sensitivity as `SensitivityInternal`. UDS error frames pass through `RedactText` (`daemon.go:193`). Verified in [`loop_test.go:381-400`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop_test.go#L381-L400). | `[OK]` |
| 8 | **IP Loopback Check via `net.ParseIP`**<br>`codex #11` | `loopbackHost` replaces string-prefix matching with `ip := net.ParseIP(h); ip != nil && ip.IsLoopback()` (`provider.go:78-87`), rejecting hostnames like `127.attacker.example`. Verified in [`provider_test.go:497-502`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L497-L502). | `[OK]` |
| 9 | **Tested Production Composition Root (`buildDaemon`)**<br>`codex #13` | Extracted `buildDaemon` (`cmd/nexus/main.go:132-180`) and added [`cmd/nexus/main_test.go`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go). This integration test caught and fixed a real production target mismatch (`prov.Target()` vs base URL). Full REPL → UDS → daemon conversation verified through production wiring (`main_test.go:27-74`). | `[OK]` |

---

## 2. Adjudication of Declared Ceilings

1. **Terminal-Append Failure Leaves Durable `RUNNING`**:
   `loop.go:268-274`: Because `attempt.started` is durably appended before dispatch, any failure during terminal-event append leaves `RUNNING` in the journal, enabling the crash-recovery / reconciliation owner to resolve the outcome. Declared ceiling is sound.
2. **Attempt Timeout Vector = P2/P3 S7 Engine**:
   `effectpath.go:392-398`: P0 enforces the call deadline at the effect boundary via context propagation. Full retry, backoff, and attempt-timeout taxonomy is appropriately deferred to P2/P3.
3. **Egress Dialer IP-Pinning = S6.3**:
   `provider.go:61-66`: Allowlist validates host strings and loopback IP classes; dialer IP-pinning (E11) is owned by S6.3. Declared ceiling is sound.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Host-String Egress Allowlist vs Resolved-IP Pinning ([`internal/llm/provider/provider.go:61-66, 137-146`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider.go#L61-L66))**:
   `provider.APIKey` validates host strings against `cfg.EgressAllow` and enforces TLS (except for local loopback parsed via `net.ParseIP().IsLoopback()`) on construction and redirects (`CheckRedirect`). Resolved-IP pinning and DNS rebinding protections (E11) remain explicitly declared as owned by S6.3 when the full outbound dialer lands.
2. **`EnvelopeParams.PayloadHash` Recomputation Seam ([`cmd/nexus/main.go:98`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L98), [`internal/kernel/loop/loop.go:90`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop.go#L90))**:
   Callers pass `"recomputed"` placeholder because `journal.Append` calculates the SHA-256 hash over the sanitized payload after redaction.
3. **P0 In-Process Sandbox Refusal Floor ([`internal/app/daemon/daemon.go:214-220`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L214-L220))**:
   `noSandbox` in the daemon fails closed on any `ExecProcess` tool call until T26 binds the `bwrap` executor.

---

## 4. Verification Evidence

- `git ls-files cmd/nexus/main.go`: `cmd/nexus/main.go` (tracked).
- `git archive HEAD | tar -x && CGO_ENABLED=0 go build -buildvcs=false ./cmd/nexus`: Built successfully (exit code 0).
- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 24 packages passed uncached (exit code 0).

---

VERDICT: PASS

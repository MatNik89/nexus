# REVIEW-PHASE2-R2-agy.md — Phase-2 Verification Round 2

**Target Ref:** `slice/p0-phase2` @ commit `a9f8874`  
**Reviewer:** `agy` (Antigravity)  
**Input Ledger:** Fold verification of Round 1 findings (`docs/REVIEW-PHASE2-codex.md` [11 HIGH, 4 MED] & `docs/REVIEW-PHASE2-kilo.md` [3 MED, 4 LOW]).

---

## 1. Verification of Round 1 Folds

| # | Finding & Source | Resolution & Code Location | Status |
| :- | :--- | :--- | :--- |
| 1 | **S7 Transport-Consumed Grants**<br>`codex #1, #2` | `provider.NewAPIKey` mandates `auth *s7min.Authority` (`provider.go:92-95`). `Chat`, `Stream`, and `Probe` consume the grant directly inside `transport` immediately before HTTP dispatch (`provider.go:156-159`). `ReAsk` callbacks cannot self-retry because the second transport attempt fails `ATTEMPT_NOT_AUTHORIZED` before leaving the process. Verified in [`provider_test.go:405-433`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L405-L433) (transport=1, attempts=1). | `[OK]` |
| 2 | **Effect Boundary & Call Validation**<br>`codex #3` | `RunTool` validates `call.Validate()` and ensures `time.Now().Before(call.Deadline)` before evaluating policy (`effectpath.go:305-310`). S7 grants are target-bound to `ToolTarget(call)` and reject cross-call reuse (`effectpath.go:313-315`). Verified in [`effectpath_test.go:368-393`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L368-L393). | `[OK]` |
| 3 | **Effectful Outcome & UNKNOWN Rule**<br>`codex #4, #5` | `failureOutcome` classifies post-dispatch failures: effectful calls without a receipt proving `BeforeCommit` park `UNKNOWN` (`effectpath.go:278-283`). Correlation and status validation reject malformed results fail-closed (`effectpath.go:370-373`). Receiptless effectful success returns `ErrEffectUnknown` (`effectpath.go:384-390`). The loop immediately stops on `ErrEffectUnknown` (`loop.go:255-259`). Verified in [`effectpath_test.go:440-519`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L440-L519). | `[OK]` |
| 4 | **Honest Attempt Lifecycle in Journal**<br>`codex #6, kilo #2` | The loop derives journal transitions from `grants.State(op)` (`loop.go:221-247`). Pre-execution refusals (DENY, unapproved ASK, invalid call) never emit `attempt.started`; they cancel the authority record and journal `machine.EvAttemptCancelled`. Post-attempt append failure treats the attempt as UNKNOWN pending reconciliation. Verified in [`loop_test.go:362-378`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop_test.go#L362-L378). | `[OK]` |
| 5 | **Per-Tool-Call Attempt Replay**<br>`codex #7` | Attempt events include `ToolCallID` (`loop.go:207-246`). Replay splits events by `ToolCallID`, enabling independent folding of multiple tool attempts per turn. Verified in [`loop_test.go:201-245`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop_test.go#L201-L245). | `[OK]` |
| 6 | **Fenced Untrusted Error Observations**<br>`codex #8, kilo #5` | Tool errors are mapped to `contracts.TrustUntrustedExternal` (fenced by assembler), truncated to 1KB, and given `obs-err-*` identifiers (`loop.go:119-131`). Verified in [`loop_test.go:257-269`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop_test.go#L257-L269). | `[OK]` |
| 7 | **Fail-Closed YOLO Audit**<br>`codex #9, kilo #1` | `AuditSink.Record` returns an error (`effectpath.go:57`). `RunTool` refuses the YOLO confirmation bypass if `Record` fails (`effectpath.go:324-327`). `journalAudit` uses an atomic counter and a 10s context timeout (`main.go:76-101`). Verified in [`effectpath_test.go:429-438`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L429-L438). | `[OK]` |
| 8 | **Egress Allowlist & Redirect Containment**<br>`codex #10, kilo #3` | `NewAPIKey` enforces HTTPS (loopback only for HTTP) (`provider.go:100-102`). `CheckRedirect` re-verifies destination against `cfg.EgressAllow` and blocks cleartext redirects (`provider.go:129-137`). Verified in [`provider_test.go:343-370`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L343-L370). | `[OK]` |
| 9 | **S6.9 Middleware Cannot Erase Errors**<br>`codex #11` | `EffectPath.onErr` uses `errors.Join(err, extra)`, guaranteeing that an `OnError` hook returning `nil` can never erase a refusal (`effectpath.go:266-272`). Verified in [`effectpath_test.go:399-427`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath_test.go#L399-L427). | `[OK]` |
| 10 | **Full-Spine Deterministic E2E Test**<br>`codex #12` | Added [`TestFullSpineDeterministicTransport`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L295) exercising `repl.Run` → `UDS` → `daemon` → `loop` → `planner` (streaming) → `provider.transport` → local SSE fake → terminal rendering → journal replay fold (`daemon_test.go:295-383`). | `[OK]` |
| 11 | **Streaming Print Wired End-to-End**<br>`codex #13` | `Daemon` accepts `PlannerFactory` (`daemon.go:36`), binding provider SSE deltas directly to `writeFrame("delta")` on the UDS connection (`daemon.go:154-156`, `planner.go:61-70`). | `[OK]` |
| 12 | **Literal Closed-Discriminator & RED Names**<br>`codex #14` | Added `TestInProcessToolNeverSpawns` / `TestReadOnlyProcessStillSandboxed` (`effectpath_test.go:197-220`) and [`TestUnknownEffectDiscriminatorReachesNoSink`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L438) verifying zero sink invocations for unknown effect kinds. | `[OK]` |
| 13 | **`procx` Asynchronous SIGKILL Polling**<br>`codex #15` | `Terminate` straggler reaping uses a 2s deadline polling loop with `time.Sleep(20ms)` to handle asynchronous process-tree teardown (`procx_linux.go:142-159`). | `[OK]` |
| 14 | **Structured Output Salvage & Strict Token Check**<br>`kilo #6, #7` | `Extract` reports S7 outcome *after* salvage (`structured.go:145-156`). `decodeStrict` uses `dec.Token()` checking for `io.EOF` to catch trailing brackets (`structured.go:56-58`). | `[OK]` |

---

## 2. Adjudication of Key Seam Judgments

1. **Transport-Consumed Grants (`provider.transport`)**:
   - `provider.APIKey` enforces that every `Chat`, `Stream`, and `Probe` attempt consumes its `s7min.Grant` inside `transport` immediately before `http.Client.Do`. Re-ask callbacks cannot self-retry. Oracle is airtight.
2. **Attempt Lifecycle from Authority State**:
   - Pre-execution refusals (e.g. unapproved ASK, policy DENY, expired deadline) fold to `contracts.AttemptCancelled` without emitting `attempt.started`. This matches physical reality and conforms to SPEC P0.1.
3. **Effectful-without-receipt → UNKNOWN + Loop Stop**:
   - `failureOutcome` classifies effectful calls without commit receipts as `OutcomeUnknown`, wrapping the error with `ErrEffectUnknown`. `loop.RunTurn` halts immediately on `ErrEffectUnknown` (conforming to E9).
4. **Full-Spine Deterministic Integration**:
   - `TestFullSpineDeterministicTransport` covers the complete production pipeline from REPL to UDS, streaming deltas, provider transport, and journal verification.
5. **Declared Ceiling Skips**:
   - `kilo LOW#4` (`PayloadHash: "recomputed"` placeholder): The journal recomputes the hash over the redacted payload. Cleanup safely deferred to future API refactoring.

---

## 3. Weakest Points Analysis (Epistemic Honesty)

1. **Host-String Egress Allowlist vs Resolved-IP Pinning ([`internal/llm/provider/provider.go:61-66, 128-137`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider.go#L61-L66))**:
   `provider.APIKey` validates host strings against `cfg.EgressAllow` and enforces TLS (except for local loopback) on construction and redirects (`CheckRedirect`). Resolved-IP pinning and DNS rebinding protections (E11) remain explicitly declared as owned by S6.3 when the full outbound dialer lands.
2. **`EnvelopeParams.PayloadHash` Recomputation Seam ([`cmd/nexus/main.go:98`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L98), [`internal/kernel/loop/loop.go:88`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop.go#L88))**:
   Callers pass `"recomputed"` placeholder because `journal.Append` calculates the SHA-256 hash over the sanitized payload after redaction.
3. **P0 In-Process Sandbox Refusal Floor ([`internal/app/daemon/daemon.go:214-220`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L214-L220))**:
   `noSandbox` in the daemon fails closed on any `ExecProcess` tool call until T26 binds the `bwrap` executor.

---

## 4. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 23 packages passed uncached (exit code 0).

---

VERDICT: PASS

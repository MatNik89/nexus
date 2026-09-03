# REVIEW-PHASE2-agy.md — Phase-2 Deep Review & E2E Integration Review

**Target Ref:** `slice/p0-phase2` @ commit `17f5603` (commits `a2f4a33..17f5603`, Tasks T13–T17)  
**Reviewer:** `agy` (Antigravity)  
**Scope:**
- (A) Per-task adversarial code review of `internal/kernel/{s7min,effectpath,loop}`, `internal/llm/{provider,planner}`, `internal/app/{daemon,repl}`, `cmd/nexus/main.go`.
- (B) E2E Integration Review across the full pipeline (`REPL` → `UDS` → `daemon` → `loop` → `PEP` → `s7` → `provider` → `journal`).

---

## 1. Per-Task Adversarial Review & RED Verification

### T13: `s7-min` (`internal/kernel/s7min/`)
- **Owner Contract:** HARNESS-SPEC P0.2, HARDQ A2 (sole owner of retry, deadline, and cancel; no-retry policy in P0).
- **Invariants Verified:**
  - `Issue` enforces exactly one grant per operation ID (`s7min.go:97-99`); a second issue for the same operation is rejected with `ATTEMPT_NOT_AUTHORIZED`.
  - `Consume` validates exact target ID, attempt number 1, constant-time nonce equality (`s7min.go:137`), unconsumed state, unexpired TTL, and canonical state `AttemptAuthorized` (`s7min.go:138-141`).
  - `OutcomeFailedRetryable` collapses to `FAILED_TERMINAL` in P0 (`s7min.go:167`).
  - `OutcomeUnknown` transitions to `AttemptUnknown` via canonical machine step; `Report` refuses further transitions out of `UNKNOWN` (reconciliation-only exit, E9) (`s7min.go:169-176`).
  - `Cancel` pre-issue registers a terminal cancelled state; cancel on outstanding grant invalidates consumption (`s7min.go:185-202`).
- **RED Verification:**
  - `test_adapter_cannot_self_retry` → [`TestAdapterCannotSelfRetry`](file:///home/matej/HARNESS/nexus/internal/kernel/s7min/s7min_test.go#L20) (`s7min_test.go:20-47`) is causal and green.

### T14: PEP + Lifecycle Chain + EffectPath (`internal/kernel/effectpath/`)
- **Owner Contract:** S6.0 PEP default-deny, S6.9 order-only middleware, DESIGN-FIXES-r2 K1/K2 EffectPath, HARDQ F2 YOLO mode.
- **Invariants Verified:**
  - `Decide` total switch: unknown tools or `DecisionInvalid` default-deny (`effectpath.go:153-163`).
  - `RunTool` step ordering (`effectpath.go:252-328`):
    1. S6.0 Policy `Decide` (ASK ≠ ALLOW; YOLO maps ASK → ALLOW with `ALLOWED_BY_YOLO` audit record; DENY is untouched).
    2. S6.9 `BeforeTool` hook (veto → `OnError`).
    3. Total switch on sealed `ExecutionKind`: unknown kinds reject fail-closed without in-process fallback (`effectpath.go:278-286`).
    4. S7 grant `Consume` before execution (`effectpath.go:288-290`).
    5. Tool execution via concrete executor (`inproc` or `sbproc`).
    6. S6.9 `AfterTool` hook (veto → `OnError` + `OutcomeUnknown` if effectful).
    7. Outcome reporting to S7 authority.
  - `Approvals`: Single-use, profile-bound, expiring SHA-256 hash over length-prefixed `ToolID`, `Arguments`, `ArgsSchemaHash`, `Effect`, `ExecutionKind`, and `ProfileID` (`effectpath.go:91-123`).
  - `ClassifyEffectPhase`: Deterministic from commit receipt; unreceipted effectful calls classify as `PhaseUnknown` (`effectpath.go:219-230`).
- **RED Verification:**
  - `TestUnknownDecisionDenies` (`effectpath_test.go:129-141`)
  - `TestUnknownExecKindRejectsNotInproc` (`effectpath_test.go:145-164`)
  - `TestAskRequiresExactApproval` (`effectpath_test.go:168-192`)
  - `TestExecutorBranchBySealedKind` (`effectpath_test.go:197-213`)
  - `TestNoAttemptWithoutGrant` (`effectpath_test.go:217-234`)
  - `TestVetoThroughOnErrorReconciles` (`effectpath_test.go:239-266`)
  - `TestExecErrorSkipsAfterToolAndOutput` (`effectpath_test.go:270-289`)
  - `TestYoloAllowsAskButNeverDeny` (`effectpath_test.go:293-309`)
  - `TestYoloCannotBeSetByChannelInput` (`effectpath_test.go:313-334`)

### T15: Provider + Structured Output (`internal/llm/provider/`, `internal/llm/planner/`)
- **Owner Contract:** S2.1 APIKey provider, S2.3-min structured output, SPEC P0.7 tolerance ladder.
- **Invariants Verified:**
  - `NewAPIKey` validates `http(s)` scheme, enforces `cfg.EgressAllow` allowlist against `u.Host` (kernel floor), and validates presence of API key env var and model (`provider.go:65-94`).
  - `Chat` returns generic status errors on non-2xx codes without echoing upstream response bodies (`provider.go:128-131`).
  - `Stream` enforces sequential SSE deltas and errors if connection terminates without `[DONE]` (`provider.go:170-236`).
  - `Extract[T]`: Disallows unknown JSON fields. Re-ask requires fresh `s7min.Grant`. `ClassSecurity` and `ClassEffect` are strict (no re-ask, no salvage). Nil validator refused (`structured.go:41-144`).
  - `Probe`: Registers live provider health check bound to `config.Resolved.ConfigHash()` (`provider.go:154-164`).
- **RED Verification:**
  - `test_structured_output_never_silent_accept` → [`TestStructuredOutputNeverSilentAccept`](file:///home/matej/HARNESS/nexus/internal/llm/provider/provider_test.go#L149) (`provider_test.go:149-171`)
  - `TestSecurityEffectClassStrictNoRepair` (`provider_test.go:224-242`)
  - `TestProbeBindsResolvedConfig` (`provider_test.go:258-276`)

### T16: One-Turn Loop (`internal/kernel/loop/`)
- **Owner Contract:** S3.1-min loop, Annex P0.1 provenance, HARDQ C3 stuck-breaker.
- **Invariants Verified:**
  - Context blocks must pass `assembler.Base` trust fencing before each planning iteration (`loop.go:157-161`).
  - `observeGuard` rejects tool outputs laundering trust to `SYSTEM`/`USER` or lacking tool-call lineage (`loop.go:100-116`).
  - Tool execution errors are packed into `TrustToolTrusted` observation blocks so planner observes failure data rather than crashing (`loop.go:119-131, 214-224`).
  - `PolicyInteractive` trips identical tool+args breaker at threshold `breakerN`; `PolicyContinuous` is exempt (`loop.go:179-186`).
  - HITL gate: `ErrNeedsApproval` fails turn immediately with `NEEDS_APPROVAL` sentinel and does not allow model to observe/bypass (`loop.go:203-212`).
  - Full journal lifecycle: `EvTurnCreated` → `EvTurnStarted` → `EvAttemptPlanned` → `EvAttemptAuthorized` → `EvAttemptStarted` → `EvAttemptSucceeded`/`EvAttemptFailed` → `EvTurnSucceeded`/`EvTurnFailed` (`loop.go:80-94, 141-235`).
- **RED Verification:**
  - `test_s0_rejects_provenance_laundering` → [`TestS0RejectsProvenanceLaundering`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop_test.go#L236) (`loop_test.go:236-259`)
  - `TestIdenticalCallBreakerTrips` (`loop_test.go:263-271`)
  - `TestContinuousPolicyExemptFromBreaker` (`loop_test.go:275-283`)
  - `TestNeedsApprovalSurfacesNotObserved` (`loop_test.go:310-324`)

### T17: REPL + Daemon/UDS Split + Liveness (`internal/app/daemon/`, `internal/app/repl/`, `cmd/nexus/`)
- **Owner Contract:** 14.1-min terminal REPL, HARDQ B7 single-writer UDS, HARDQ C8 liveness heartbeat, HARDQ F2 session YOLO flag.
- **Invariants Verified:**
  - `daemon.Serve` creates socket `0600` and validates peer UID matches daemon EUID via `SO_PEERCRED` (`daemon.go:73, 89-105`).
  - `handle` reads `hello` frame to set session `PolicyMode`; chat payloads cannot modify session policy (`daemon.go:123-134`).
  - Single DB writer: concurrent UDS sessions serialize journal appends via the journal append actor without surfacing `SQLITE_BUSY` (`daemon_test.go:188-216`).
  - Heartbeat: touches file periodically; `Stale` detects missing or stopped heartbeat within 2 intervals (`heartbeat.go:20-59`).
  - Prompt clearly displays `nexus[YOLO]> ` when YOLO mode is active (`repl.go:17-22`).
- **RED Verification:**
  - `TestScriptedConversationThroughJournal` (`daemon_test.go:164-184`)
  - `TestTwoConcurrentClientsNoSqliteBusy` (`daemon_test.go:188-216`)
  - `TestYoloSessionModeIsPerSessionOnly` (`daemon_test.go:221-243`)
  - `TestHeartbeatStopDetectable` (`daemon_test.go:247-267`)

---

## 2. E2E Integration & Seam Verification

1. **UDS Peer Authentication & Isolation (`sameUID`)**:
   - `daemon.sameUID` inspects socket options via `syscall.SO_PEERCRED` ensuring only the owner process UID can communicate over the UDS.
2. **YOLO Policy Containment**:
   - Session policy mode is fixed at connection handshake (`hello` frame) and cannot be modified by prompt injection or tool payload fields.
   - YOLO mode converts `DecisionAsk` to `DecisionAllow` with mandatory `ALLOWED_BY_YOLO` audit journal event (`effectpath.go:258-260`, `main.go:78-89`). `DecisionDeny` is never bypassed by YOLO.
3. **Journal Single-Writer Serialization**:
   - Multiple daemon client sessions write through a shared `journal.Journal` handle, which executes all appends sequentially over an internal actor channel. Zero `SQLITE_BUSY` contention.
4. **Secret Redaction at Ingestion & Ingress**:
   - `cmd/nexus/main.go` creates a `redact.NewKnownRefs` redactor populated with secrets from `ProviderKeyEnv` and `TelegramTokenEnv`, sanitizing payloads before journal insertion.
   - Provider HTTP errors suppress response body content, preventing secret echo from upstream error responses.

---

## 3. Assessment of Declared Topknot Ceilings

1. **`Approvals` Exact Byte Argument Matching ([`internal/kernel/effectpath/effectpath.go:70-73`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L70-L73))**:
   - *Ceiling:* Hashes exact `c.Arguments` bytes without JSON AST canonicalization.
   - *Adjudication:* Acceptable. Key reordering in JSON produces a mismatch leading to a safe re-ask (`ErrNeedsApproval`), never a false-allow.
2. **Order-Only Middleware Seam ([`internal/app/daemon/daemon.go:195-203`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L195-L203))**:
   - *Ceiling:* `orderOnlyMW` provides no-op hooks for `BeforeTool`/`AfterTool`.
   - *Adjudication:* Acceptable. S6.9 lifecycle ordering is established; concrete budget/redaction middleware plugs in during later task slices.
3. **Sandboxed Process Refusal Floor ([`internal/app/daemon/daemon.go:205-211`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L205-L211))**:
   - *Ceiling:* `noSandbox` fails closed on any `ExecProcess` tool call until T26 binds the `bwrap` executor.
   - *Adjudication:* Acceptable. Fails closed, preventing uncontained subprocess execution.

---

## 4. Weakest Points Analysis (Epistemic Honesty)

1. **P0 Conversation Context Window Scoping ([`internal/app/daemon/daemon.go:173`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L173))**:
   `daemon.handle` passes each turn the single immediate `userBlock` (`[]contracts.ContextBlock{block}`) rather than replaying prior turns from the journal. Multi-turn context rehydration and memory fold arrive in Phase 3 with the dedicated conversation state owner.
2. **Exact-Byte Argument Hashing in `Approvals` ([`internal/kernel/effectpath/effectpath.go:70-73, 91-102`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L70-L73))**:
   `effectHash` hashes the raw `c.Arguments` byte slice rather than a canonicalized JSON object tree. If a client or planner serializes identical JSON object fields in a different order between approval and execution, `consumeExact` will reject with `ErrNeedsApproval` (fail-closed / safe false-positive).
3. **P0 In-Process Process Refusal Floor ([`internal/app/daemon/daemon.go:205-211`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L205-L211))**:
   `noSandbox` unconditionally refuses `ExecProcess` tool calls in `daemon` until the real `bwrap` executor membrane binds in T26. Pure Go `ExecInProcess` tools run, while any attempted subprocess execution fails closed.

---

## 5. Verification Evidence

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: 20 packages passed uncached (exit code 0).

---

VERDICT: PASS

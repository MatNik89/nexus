# REVIEW-PHASE5-R2 — Phase 5 Verification Review (Round 2)

**Target Ref:** `slice/p0-phase5` @ HEAD (`91ed2c701e53fd92642d10e790a845da0a15bacc`)  
**Commit Covered:** `91ed2c7` (`fix(phase5-r1): fold deep+security review round 1 (codex #1-14, kilo #1-5, agy #1-4)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `internal/channel/`, `internal/channel/telegram/`, `internal/approval/`, `cmd/nexus/main.go`, `cmd/nexus/main_test.go`, `internal/app/daemon/daemon.go`, `internal/kernel/effectpath/effectpath.go`, `internal/kernel/loop/loop.go`.

---

## 1. Executive Summary & Round-1 Disposition Matrix

Round 1 identified 4 primary findings from `agy` (alongside 14 findings from `codex` and 5 from `kilo`) spanning missing loop↔approval suspension/resumption wiring, unwritten inbound terminal state leading to dropped messages on crash, blind outbox retries on ambiguous transport / dual-mark failure, bot token disclosure in HTTP error strings, unconstrained approval decision sources, expiry bypass at consumption, and binding parser ambiguities.

Fold commit `91ed2c7` addresses each finding directly. Every fold defense was evaluated adversarially in an isolated temporary export (`git archive HEAD | tar -x -C /tmp/nexus-r2-probe`), leaving the repository working tree completely clean. All 7 negative ablation probes failed as expected when the corresponding defense was removed, and passed uncached when restored.

| Round-1 Finding | Category | Originating Ref | Fold Disposition | Verification Status |
|---|---|---|---|---|
| **HITL Loop Suspension & Resumption** | Integration / T24 | agy #4, codex #1, kilo #1 | `loop.Suspender` + PEP `DurableApprovals` + daemon `SuspenderFor` + `telegramHandler` `approve` $\to$ `b.resumeApproved` | **[RESOLVED & CAUSAL]** (Probe 1: ModeYolo ablation fails `TestChannelAskSuspendsThenApproveResumes`) |
| **Inbound Terminal Lifecycle** | Delivery / T22 | agy #3, codex #2, kilo #3 | `EvInboundTerminal` + `CompleteInbound` one-batch recipe; non-terminal replay re-runs handler | **[RESOLVED & CAUSAL]** (Probe 2: skipping handler on replay fails `TestNonTerminalReplayRerunsHandler`) |
| **Delivery Honesty & Ambiguous Parking** | Outbox / T22 | agy #2, codex #3, codex #4 | UNKNOWN-first parking before network call; `ErrAmbiguousSend` keeps row `UNKNOWN`; definite pre-wire errors re-pend | **[RESOLVED & CAUSAL]** (Probe 3: removing pre-wire parking fails `TestAmbiguousSendParksUnknownNeverResends`) |
| **Bot Token Leak in Transport Errors** | Security / T23 | agy #1, codex #9, kilo #2 | `Adapter.sanitize` redacts `/bot<token>/` to `[REDACTED-TOKEN]` in all transport errors and probe output | **[RESOLVED & CAUSAL]** (Probe 4: disabling `sanitize` leaks token and fails `TestTokenNeverInErrors`) |
| **Originating Source Authorization** | Authorization / T24 | codex #5, codex #6 | `expected_source` stored in challenge and required in `Approve`/`Deny`; private-chat only | **[RESOLVED & CAUSAL]** (Probe 5: foreign chat decision fails `TestForeignSourceCannotDecide`) |
| **Approval Expiry at Consumption** | Integrity / T24 | codex #7, kilo #5 | `ConsumeApproval` queries `expires_unix` and checks `now >= expires` fail-closed | **[RESOLVED & CAUSAL]** (Probe 6: boundary change fails `TestApprovalExpiresAtConsume`) |
| **Secret Redaction in Summaries** | Security / T24 | codex #8, kilo #3 | `Suspend` checks `j.RedactorRewrites(rawPayload)` and rejects challenge creation fail-closed | **[RESOLVED & CAUSAL]** (Probe 7: disabling check fails `TestChallengeRefusesSecretExposure`) |
| **Unified Canonical EffectHash** | Contract / C4 | kilo #4, codex #8 | Single canonical `effectpath.EffectHash` exported and used by PEP and `approval.Store` | **[RESOLVED]** (Verified identical length-prefixed digest computation across both paths) |
| **Strict Telegram Bindings & Probe Gate** | Preflight / T23 | codex #10, codex #13, agy #6 | `telegramBindings` strictly parses key-value pairs (rejects duplicate chats/empty profiles); `telegramProbeGate` runs `Probe` at startup | **[RESOLVED & CAUSAL]** (Probe 8: duplicate bindings fail `TestTelegramBindingsStrict`) |
| **Channel Input Provenance** | Attribution / Lineage | agy #5, codex #14 | `sourcedBlock` tags channel inputs with `Producer: "telegram"` and `SourceURI: "nexus://telegram/<identity>"` | **[RESOLVED]** (Verified in `daemon.go:237-240`) |

---

## 2. Adversarial Verification & Negative Ablation Evidence

All negative probes were executed in a clean temporary checkout (`/tmp/nexus-r2-probe`) without modifying `/home/matej/HARNESS/nexus`.

### Probe 1: HITL ModeDefault vs ModeYolo Causality (Integration / F2)
- **Target:** [`internal/app/daemon/daemon.go:216`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L216)
- **Ablation:** Mutated `effectpath.ModeDefault` to `effectpath.ModeYolo` in `RunChannelTurn`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestChannelAskSuspendsThenApproveResumes
      main_test.go:488: channel ASK did not suspend (yolo leak? F2): "saved it"
  --- FAIL: TestChannelAskSuspendsThenApproveResumes (0.08s)
  ```
- **Post-Restoration:** `PASS` (0.09s). Proves that channel turns cannot bypass confirmations via YOLO and that `TestChannelAskSuspendsThenApproveResumes` is strictly causal.

### Probe 2: Inbound Non-Terminal Crash Replay (Channel Lifecycle / B2)
- **Target:** [`internal/channel/telegram/telegram.go:225-231`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L225-L231)
- **Ablation:** Reverted `processUpdate` to treat all replayed update IDs as no-ops (returning `nil` unconditionally on `outcome.Replayed`).
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestNonTerminalReplayRerunsHandler
      telegram_test.go:277: non-terminal replay did not re-run the handler: 0 runs
  --- FAIL: TestNonTerminalReplayRerunsHandler (0.04s)
  ```
- **Post-Restoration:** `PASS` (0.04s). Proves that non-terminal inbound messages re-execute following crash/redelivery.

### Probe 3: UNKNOWN-First Outbox Parking (Delivery Honesty / B2)
- **Target:** [`internal/channel/channel.go:445-447`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L445-L447)
- **Ablation:** Disabled the pre-wire `c.mark(ctx, EvOutboundUnknown, o.DeliveryID)` parking step before calling `send(o)`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestAmbiguousSendParksUnknownNeverResends
      channel_test.go:340: ambiguous delivery retried blindly: accepts=2
  --- FAIL: TestAmbiguousSendParksUnknownNeverResends (0.05s)
  ```
- **Post-Restoration:** `PASS` (0.02s). Proves that ambiguous transport outcomes and post-accept crashes cannot cause blind duplicate sends.

### Probe 4: Bot Token Sanitization in Error Strings (Credential Secrecy / C1)
- **Target:** [`internal/channel/telegram/telegram.go:109-114`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L109-L114)
- **Ablation:** Disabled token redactor `sanitize` in `Adapter.call` to return the underlying Go `*url.Error` raw.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestTokenNeverInErrors
      telegram_test.go:326: token leaked into the error: telegram: transport: AMBIGUOUS_SEND: Post "http://127.0.0.1:1/bot123:token/getUpdates": dial tcp 127.0.0.1:1: connect: connection refused
  --- FAIL: TestTokenNeverInErrors (0.01s)
  ```
- **Post-Restoration:** `PASS` (0.03s). Proves that `a.sanitize` prevents bot credentials from leaking into doctor probe detail, logs, or error frames.

### Probe 5: Expected Source Authorization (Authorization Isolation / B3)
- **Target:** [`internal/approval/approval.go:178, 191`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L178)
- **Ablation:** Removed `AND expected_source=?` from the SQL update in `EvApprovalReceived` / `EvApprovalDenied`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestForeignSourceCannotDecide
      approval_test.go:238: foreign chat approved the challenge
  --- FAIL: TestForeignSourceCannotDecide (0.01s)
  ```
- **Post-Restoration:** `PASS` (0.04s). Proves that only the chat channel identity where the challenge originated can approve or deny it.

### Probe 6: Consume Expiry Boundary (`now >= expires`)
- **Target:** [`internal/approval/approval.go:398`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L398)
- **Ablation:** Changed `s.clock.Now().Unix() >= expires` to `>` in `ConsumeApproval`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestApprovalExpiresAtConsume
      approval_test.go:272: approval consumed at the expiry boundary
  --- FAIL: TestApprovalExpiresAtConsume (0.01s)
  ```
- **Post-Restoration:** `PASS` (0.01s). Proves exact boundary fail-closed behavior at the consumption instant.

### Probe 7: Secret Exposure Refusal in Challenge Creation
- **Target:** [`internal/approval/approval.go:286-292`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L286-L292)
- **Ablation:** Removed `if rewrites { return Challenge{}, ... }` check in `Store.Suspend`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestChallengeRefusesSecretExposure
      approval_test.go:294: challenge exposing a known secret was created
  --- FAIL: TestChallengeRefusesSecretExposure (0.03s)
  ```
- **Post-Restoration:** `PASS` (0.03s). Proves that tool calls containing secrets that would be rewritten by the journal redactor are rejected before emitting a challenge summary to Telegram.

---

## 3. Analysis of Top Weakest Points (Mandatory Review Discipline)

In accordance with mandatory review discipline, the following weakest architectural points and topknot ceilings were identified and audited:

### Weakest Point 1: C4 Topknot Ceiling — `(device, inode)` target resource binding deferred
- **Location:** [`internal/kernel/effectpath/effectpath.go:124-139`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L124-L139) and [`internal/approval/approval.go:52-58`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L52-L58)
- **Details:** `EffectHash` digests `ToolID`, `Arguments`, `ArgsSchemaHash`, `Effect`, `ExecutionKind`, `IdempotencyKey`, and `ProfileID`. It does not resolve symlinks or bind the underlying OS `(device, inode)` pair of filesystem targets. As documented in `approval.go:54-57`, this topknot ceiling is deferred to the first direct destructive filesystem tool (P0 execution goes strictly through in-process or sandboxed executors).
- **Risk Assessment:** Acceptable for P0 scope; no path exists for an unprivileged local attacker to swap paths between admission and single-use execution under the existing sandboxed model.

### Weakest Point 2: Single-Action Tool Resume vs Multi-Turn Planner Resumption
- **Location:** [`cmd/nexus/main.go:219-237`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L219-L237) and [`cmd/nexus/main.go:534-538`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L534-L538)
- **Details:** When an ASK tool suspends, `loop.RunTurn` completes with `EvTurnSucceeded` and the challenge summary is delivered to the user ([`loop.go:300-305`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop.go#L300-L305)). When the user replies `approve <id>`, `resumeApproved` executes the single approved `ToolCall` directly through `b.sysPath.RunTool` and returns its direct output string (`"Approved <id>. Done: <result>"`). If a complex multi-step plan requires feeding the tool output back into another LLM iteration, P0 does not re-enter the model loop; it delivers the tool result directly to the channel.
- **Risk Assessment:** Fully satisfies P0 T24 requirement ("Acceptance: irreversible action from Telegram requires approval before execution; approve from Telegram resumes approved action"); multi-turn conversation loop resumption is a P1 concern.

### Weakest Point 3: Unbound / Non-Text Redelivery Duplication Window on SIGKILL
- **Location:** [`internal/channel/telegram/telegram.go:188-211`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L188-L211)
- **Details:** Messages from unbound chats, group chats, or non-text messages enqueue a typed refusal reply via `a.core.EnqueueReply` before `Admit` is called (enforcing B3 profile isolation so unauthenticated chats never create inbox records in the profile database). Telegram's in-memory polling offset `a.offset` advances only after `processUpdate` returns. If a process crash occurs immediately after `EnqueueReply` before the next poll updates the offset, Telegram will redeliver the unbound/non-text update on restart, enqueueing a duplicate typed refusal reply.
- **Risk Assessment:** Acknowledged intentional trade-off. Prevents unauthorized inbox state in profile DBs while maintaining fail-closed refusals.

---

## 4. Test Suite Execution & Gate Verification

- **Lint & Vet Check:**
  `CGO_ENABLED=0 go vet ./...` completed cleanly with zero warnings or errors (exit code 0).
- **Test Suite Execution:**
  `CGO_ENABLED=0 go test -count=1 ./...` ran across all 32 packages in the repository and completed with all tests passing (exit code 0).
- **Conformance & Ledger Integrity:**
  All ledger RED tests across T22 (channel core), T23 (telegram adapter), and T24 (durable remote HITL) are present, active, and causally protective.

---

VERDICT: PASS

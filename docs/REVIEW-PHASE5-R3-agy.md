# REVIEW-PHASE5-R3 — Phase 5 Verification Review (Round 3)

**Target Ref:** `slice/p0-phase5` @ HEAD (`594c4573a6551a7979a7e56f71c08a44c497ec7d`)  
**Commit Covered:** `594c457` (`fix(phase5-r2): fold verification round 2 (codex 9 findings, kilo 2+3 low)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `cmd/nexus/main.go`, `cmd/nexus/main_test.go`, `internal/app/daemon/daemon.go`, `internal/app/daemon/daemon_test.go`, `internal/approval/approval.go`, `internal/approval/approval_test.go`, `internal/channel/channel.go`, `internal/channel/channel_test.go`, `internal/channel/telegram/telegram.go`, `internal/channel/telegram/telegram_test.go`, `internal/kernel/contracts/enums.go`, `internal/kernel/loop/loop.go`, `internal/kernel/machine/machine.go`.

---

## 1. Executive Summary & Verification Matrix

In verification round 2, the reviewers identified several critical gaps in the first fold batch:
1. HITL resume executed isolated tool calls without rehydrating the original conversation turn or updating canonical machine state (falsely recording `turn.succeeded` upon suspension).
2. The frozen 2-minute planner call deadline self-invalidated delayed approvals arriving within the 15-minute challenge TTL.
3. A crash between approval receipt and resume stranded the approved action on replay.
4. Non-terminal inbound replay minted non-deterministic turn IDs, allowing duplicate tool effects across crashes.
5. Approval expiry was checked before append rather than inside the projection transaction against event-owned time.
6. A single permanently failing outbox delivery starved all later outbox rows.
7. The live Telegram probe was not sealed into the T11 `closure.Seal` capability snapshot.
8. Connection-refused transport errors were over-broadly classified as ambiguous, permanently parking rows `UNKNOWN`.

Fold commit `594c457` resolves all 9 areas with rigorous, causal implementations. All claims were verified via code inspection, full test suite execution, and independent negative ablation probes executed in an isolated clean temporary export (`/tmp/nexus-r3-probe`), leaving the repository working tree untouched.

| # | Fold Claim | Primary Mechanisms | Negative Probe / Evidence | Status |
|---|---|---|---|---|
| 1 | **Turn Rehydration & Honest Machine State** | `TurnSuspendedState`, `turn.suspended`/`turn.resumed`, `loop.ResumeTurn`, `iterate` core | Mutating `EvTurnSuspended` $\to$ `EvTurnSucceeded` fails `TestSuspendedTurnHonestState` (`suspended=0 succeeded=1`) | **[VERIFIED & CAUSAL]** |
| 2 | **Transport Deadline Refresh at Resume** | `call.Deadline = time.Now().Add(2*time.Minute)` in `resumeApproved` | Removing refresh fails `TestDelayedApprovalResumes` (`CALL_DEADLINE_EXCEEDED`) | **[VERIFIED & CAUSAL]** |
| 3 | **Crash-Window Recovery & Startup Scan** | Source-bound `SuspendedCall` fallback on replay; `resumeApprovedPending` startup scan | Removing replay fallback fails `TestApproveReplayAfterCrashResumes` (`APPROVAL_REPLAY`) | **[VERIFIED & CAUSAL]** |
| 4 | **Deterministic Channel Turn IDs** | `turn-chan-<identity>-<updateID>` prevents duplicate effects | Dynamic turn ID ablation fails `TestRedeliveredUpdateCannotRerunCompletedTurn` | **[VERIFIED & CAUSAL]** |
| 5 | **Projection-Enforced Approval Expiry** | `expires_unix > ev.Envelope.EmittedAt.Unix()` in SQL CAS for `APPROVED`/`DENIED`/`CONSUMED` | Removing projection expiry check fails `TestProjectionRefusesExpiredDecision` | **[VERIFIED & CAUSAL]** |
| 6 | **Outbox Non-Starvation (Poison Head)** | `Flush` collects failures into `failures []error` and continues loop | Reverting to immediate abort on error fails `TestPoisonHeadDoesNotStarve` (`good=0`) | **[VERIFIED & CAUSAL]** |
| 7 | **Sealed Snapshot Gating (`closure.Seal`)** | `telegramProbeGate` builds sealed snapshot via `sealStartupSnapshot` gating adapter | Disabling snapshot status check fails `TestTelegramProbeGate` | **[VERIFIED & CAUSAL]** |
| 8 | **Pre-Wire Classification & Sanitization** | `isPreWire(err)` classifies dial/DNS errors as definite pre-wire; `NewRequestWithContext` sanitized | Disabling `isPreWire` fails `TestPreWireFailureRepends`; unsanitized URL fails `TestRequestConstructionSanitized` | **[VERIFIED & CAUSAL]** |
| 9 | **Deterministic Regression Detectors** | Deterministic race barrier in `TestConcurrentAdmissionRace`; `TestChallengeIDsUnpredictable`; `TestChannelTurnCarriesChannelProvenance` | Ablations of UNIQUE backstop, random challenge IDs, and channel provenance turn all 3 respective tests `RED` | **[VERIFIED & CAUSAL]** |

---

## 2. Detailed Adversarial Verification & Negative Ablation Results

All probes were executed against a fresh, clean `git archive HEAD` export.

### 1. Honest Machine State & Turn Continuation (`loop.go`, `machine.go`, `daemon.go`)
- **Code Audit:** [`internal/kernel/machine/machine.go:157-175`](file:///home/matej/HARNESS/nexus/internal/kernel/machine/machine.go#L157-L175) defines `EvTurnSuspended` (`TurnRunning` $\to$ `TurnSuspendedState`) and `EvTurnResumed` (`TurnSuspendedState` $\to$ `TurnRunning`). When an unapproved ASK occurs, [`internal/kernel/loop/loop.go:328`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop.go#L328) journals `EvTurnSuspended` and exits without executing the tool. Upon approval, [`cmd/nexus/main.go:250-256`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L250-L256) executes the tool via `sysPath.RunTool`, wraps the result into a `tool_result` context block via `resumeObservation`, and invokes `b.d.ResumeChannelTurn` $\to$ `loop.ResumeTurn`.
- **Ablation Probe:** In `loop.go:328`, reverted `EvTurnSuspended` to `EvTurnSucceeded`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestSuspendedTurnHonestState
      main_test.go:793: suspended turn state dishonest: suspended=0 succeeded=1
  --- FAIL: TestSuspendedTurnHonestState (0.07s)
  ```
- **Restoration:** `PASS` (0.08s).

### 2. Transport Deadline Refresh at Resume (`main.go`)
- **Code Audit:** In [`cmd/nexus/main.go:225`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L225), `call.Deadline` is refreshed to `time.Now().Add(2 * time.Minute)` prior to `b.authority.Issue` and `b.sysPath.RunTool`. The transport deadline is deliberately excluded from `effectpath.EffectHash` ([`effectpath.go:124-139`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L124-L139)), ensuring exact-intent digest integrity is preserved while allowing delayed remote approvals within the 15-minute challenge TTL.
- **Ablation Probe:** In `main.go:225`, removed `call.Deadline = time.Now().Add(2 * time.Minute)`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestDelayedApprovalResumes
      main_test.go:677: delayed approval self-invalidated: "Approved ch-... but the resume failed: effectpath: tool \"memory_remember\": CALL_DEADLINE_EXCEEDED"
  --- FAIL: TestDelayedApprovalResumes (0.16s)
  ```
- **Restoration:** `PASS` (0.19s).

### 3. Crash-Window Recovery & Startup Scan (`main.go`, `approval.go`)
- **Code Audit:** In [`cmd/nexus/main.go:623-628`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L623-L628), when `b.approvals.Approve` returns `APPROVAL_REPLAY`, `telegramHandler` checks `b.approvals.SuspendedCall(ctx, id, source)`. If the challenge was already committed as `APPROVED` for this exact source, it proceeds directly to `b.resumeApproved`. Furthermore, [`main.go:271-291`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L271-L291) (`resumeApprovedPending`) scans for any unconsumed approved challenges on daemon startup, resuming them and routing the outcome through the outbox.
- **Ablation Probe:** In `main.go:623-628`, removed the `SuspendedCall` fallback on `Approve` error.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestApproveReplayAfterCrashResumes
      main_test.go:707: replayed approve stranded the action: "Approval failed: approval: challenge already APPROVED: APPROVAL_REPLAY"
  --- FAIL: TestApproveReplayAfterCrashResumes (0.03s)
  ```
- **Restoration:** `PASS` (0.06s).

### 4. Deterministic Per-Message Turn IDs (`daemon.go`)
- **Code Audit:** In [`internal/app/daemon/daemon.go:224-227`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L224-L227), `TurnID` and `RunID` are minted deterministically from the update identity: `turn-chan-%s-%d` and `run-chan-%s-%d`. On update redelivery, the journal's unique `(run_id, sequence)` and event ID constraints prevent re-executing a completed turn.
- **Ablation Probe:** Replaced deterministic turn IDs with dynamic nonces (`time.Now().UnixNano()`).
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestRedeliveredUpdateCannotRerunCompletedTurn
      daemon_test.go:433: redelivered update re-ran a completed turn
  --- FAIL: TestRedeliveredUpdateCannotRerunCompletedTurn (0.03s)
  ```
- **Restoration:** `PASS` (0.04s).

### 5. Projection-Enforced Expiry (`approval.go`)
- **Code Audit:** In [`internal/approval/approval.go:178, 192, 207`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L178), all state transitions (`APPROVED`, `DENIED`, `CONSUMED`) execute an atomic SQL update requiring `expires_unix > ?` bound to `ev.Envelope.EmittedAt.Unix()`. If the event was emitted at or after expiration, the projection update affects 0 rows and aborts the append.
- **Ablation Probe:** In `approval.go:178`, removed `AND expires_unix > ?` from the `APPROVED` projection transition.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestProjectionRefusesExpiredDecision
      approval_test.go:328: projection committed a decision emitted past expiry
  --- FAIL: TestProjectionRefusesExpiredDecision (0.01s)
  ```
- **Restoration:** `PASS` (0.01s).

### 6. Outbox Flush Poison-Head Non-Starvation (`channel.go`)
- **Code Audit:** In [`internal/channel/channel.go:452-484`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L452-L484), `Flush` iterates through all pending rows, re-pending definite transport failures, and collecting errors into `failures []error`. The loop continues to subsequent deliveries rather than returning immediately on the first failure.
- **Ablation Probe:** Reverted `Flush` to return immediately upon encountering the first definite failure.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestPoisonHeadDoesNotStarve
      channel_test.go:481: poison head starved the later delivery: good=0
  --- FAIL: TestPoisonHeadDoesNotStarve (0.04s)
  ```
- **Restoration:** `PASS` (0.03s).

### 7. Sealed Snapshot Gating (`main.go`)
- **Code Audit:** In [`cmd/nexus/main.go:580-607`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L580-L607), `telegramProbeGate` calls `adapter.Probe`, builds the capability snapshot via `sealStartupSnapshot` which invokes `closure.Seal`, and verifies `snap.Status("telegram").On` before permitting the adapter to run.
- **Ablation Probe:** Bypassed the snapshot status check in `telegramProbeGate`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestTelegramProbeGate
      main_test.go:599: dead channel passed the probe gate
  --- FAIL: TestTelegramProbeGate (0.01s)
  ```
- **Restoration:** `PASS` (0.02s).

### 8. Pre-Wire Transport Classification & Request Construction Sanitization (`telegram.go`)
- **Code Audit:** In [`internal/channel/telegram/telegram.go:108-117, 148-151`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L108-L117), `isPreWire(err)` detects `*net.DNSError` and `*net.OpError` (where `Op == "dial"`), returning plain errors that cause `Core.Flush` to re-pend rows for safe retry. Ambiguous post-wire errors return `channel.ErrAmbiguousSend` to park rows `UNKNOWN`. In [`telegram.go:137-140`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L137-L140), errors from `http.NewRequestWithContext` pass through `a.sanitize(err)`.
- **Ablation Probes:**
  - Disabling `isPreWire`: `TestPreWireFailureRepends` failed `RED` (`connection-refused misclassified: pending=0 unknown=1`).
  - Disabling request construction sanitization: `TestRequestConstructionSanitized` failed `RED` (`token leaked from request construction`).
- **Restorations:** Both tests passed cleanly.

### 9. Detector Causality Audits
- **Admission Race:** In [`internal/channel/channel_test.go:410-456`](file:///home/matej/HARNESS/nexus/internal/channel/channel_test.go#L410-L456), `TestConcurrentAdmissionRace` uses `testPostCheckHook` to park both goroutines after their dedup checks. Ablating both `message_id PRIMARY KEY` and `UNIQUE(adapter_id, channel_identity, update_id)` caused the test to fail `RED` (`2 admission events (want 1)`).
- **Challenge ID Unpredictability:** In [`internal/approval/approval_test.go:336-353`](file:///home/matej/HARNESS/nexus/internal/approval/approval_test.go#L336-L353), `TestChallengeIDsUnpredictable` verifies random nonces. Reverting to deterministic hash prefixes caused the test to fail `RED` (`UNIQUE constraint failed: appr_challenges.challenge_id`).
- **Channel Provenance:** In [`internal/app/daemon/daemon_test.go:403-422`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L403-L422), `TestChannelTurnCarriesChannelProvenance` captures the initial context block. Mutating the URI/producer to REPL caused the test to fail `RED` (`channel input masquerades as "nexus://repl"/"repl"`).

---

## 3. Analysis of Top Weakest Points & Acknowledged Ceilings

In accordance with mandatory review discipline:

### Weakest Point 1: Static Attestations for Provider/Store in Sealed Snapshot (Topknot Ceiling)
- **Location:** [`cmd/nexus/main.go:593-605`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L593-L605) (`sealStartupSnapshot`)
- **Details:** The provider and store probes passed to `closure.Seal` at daemon startup are static attestations (`"static: config resolved, key env bound"` and `"journal open"`). While the live Telegram channel probe is dynamically executed, live provider network round trips are deferred to P1 when a standardized lightweight provider health endpoint is defined.
- **Risk Assessment:** Acknowledged topknot ceiling documented inline. The fail-closed startup validation ensures keys and layouts are bound before sealing.

### Weakest Point 2: At-Least-Once Tool Semantics for Crash between Tool Commit and `turn.succeeded`
- **Location:** [`internal/channel/telegram/telegram.go:220-245`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L220-L245) and [`internal/app/daemon/daemon.go:224-230`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L224-L230)
- **Details:** With deterministic turn IDs (`turn-chan-<identity>-<updateID>`), a redelivered update re-enters the same turn ID. If a crash occurs strictly after a tool commits an irreversible external effect but before `machine.EvTurnSucceeded` / `CompleteInbound`, redelivery re-enters the turn and will fail at the journal level if the same event ID is replayed, or may execute an idempotent tool step.
- **Risk Assessment:** In P0, tool handlers are id-fail-closed (`task_create`/`reminder_set` reject duplicate IDs; `memory_remember` supersedes facts).

### Weakest Point 3: Approve / Deny Prefix Interception in Natural Language
- **Location:** [`cmd/nexus/main.go:618-644`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L618-L644)
- **Details:** Any user message beginning with `"approve "` or `"deny "` is routed to the HITL store. If the challenge ID is invalid or unknown, it replies with an approval error instead of routing the text to the LLM.
- **Risk Assessment:** Acknowledged UX behavior. Security integrity is completely preserved because decisions are strictly source-bound to the originating `expected_source`.

---

## 4. Test Suite Execution & Gate Verification

- **Lint & Vet Check:**
  `CGO_ENABLED=0 go vet ./...` completed with exit code 0 (zero errors/warnings).
- **Test Suite Execution:**
  `CGO_ENABLED=0 go test -count=1 ./...` ran across all 32 packages in the repository and passed completely (exit code 0).
- **Ledger Conformance:**
  All P0 acceptance criteria and ledger RED requirements across T22, T23, and T24 are fully satisfied.

---

VERDICT: PASS

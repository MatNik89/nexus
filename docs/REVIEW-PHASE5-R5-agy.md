# REVIEW-PHASE5-R5 — Phase 5 Verification Review (Round 5)

**Target Ref:** `slice/p0-phase5` @ HEAD (`bded8a2d823c03c0582b4fb167542bcec5b632ce`)  
**Commit Covered:** `bded8a2` (`fix(phase5-r4): fold verification round 4 (codex #1-3)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `cmd/nexus/main.go`, `internal/app/daemon/daemon.go`, `internal/app/daemon/daemon_test.go`, `internal/approval/approval.go`, `internal/approval/approval_test.go`.

---

## 1. Executive Summary & Verification Matrix

In verification round 4, reviewer `codex` reported three critical defects in the previous fold batch:
1. **[codex #1] Non-Atomic Retry Recipe Minting Double Authorizations:** `telegramHandler` executed `Suspend` followed by a separate `MarkResumeCompleted`. A process crash between the two appends left the old challenge open while creating a new one, permitting two independently consumable approvals for one UNKNOWN effect.
2. **[codex #2] Migration Stranding of Legacy v1 Suspensions:** Projection v2 added mandatory `context_json`. Pre-v2 suspensions rebuilt with empty `context_json`, causing `SuspendedCall` to fail unmarshalling with `unexpected end of JSON input` and stranding live pending approvals across projection rebuilds.
3. **[codex #3] Completed-Turn Recovery Ignoring Corrupt Replay:** `completedTurnFinal` captured matching `turn.succeeded` events during replay but discarded `Journal.Replay` errors, serving cached final outcomes even when subsequent canonical journal events failed integrity hash checks.

Fold commit `bded8a2` resolves all three findings with robust, minimal-machinery implementations conforming to Annex A contracts and PRD invariants. All claims were verified via code audits, negative ablation probes in clean temporary exports (`git archive HEAD`), and a complete green run of the repository test suite. No new defects were introduced into any previously verified Phase 5 dimensions.

| # | Round 4 Finding / Claim | Primary Mechanisms | Negative Probe / Evidence | Status |
|---|---|---|---|---|
| 1 | **Atomic Retry Recipe (`codex #1`)** | `approval.Store.RetryChallenge` issues `EvResumeCompleted` (closing old) and `EvTurnSuspended` (minting new) in ONE `AppendBatch` | Splitting `RetryChallenge` into two separate `Append` calls turns `TestRetryRecipeAtomicUnderSigkill` **RED** (`retry recipe torn: old reconcile item open AND a new challenge live`) | **[OK] VERIFIED & CAUSAL** |
| 2 | **Graceful Legacy Context Degradation (`codex #2`)** | `parseContextJSON` degrades empty `context_json` (legacy v1 rows) to `nil` blocks (observation-only resume) while failing closed on corrupt JSON | Ablating `parseContextJSON("")` to error turns `TestLegacyContextDegradesNotStrands` **RED** (`legacy empty context must degrade to nil blocks`) | **[OK] VERIFIED & CAUSAL** |
| 3 | **Fail-Closed Integrity in Completed Recovery (`codex #3`)** | `completedTurnFinal` propagates `Journal.Replay` error, returning `("", false)` if journal integrity verification fails | Ignoring `Replay` error in `completedTurnFinal` turns `TestRecoveryFailsClosedOnCorruptJournal` **RED** (`recovered a final from a journal that fails integrity verification`) | **[OK] VERIFIED & CAUSAL** |

---

## 2. Detailed Adversarial Verification & Negative Ablation Results

All negative ablation probes were executed against a clean `git archive HEAD` export in an isolated temporary directory (`/tmp/nexus-r5-probe`), leaving the repository working tree clean.

### 1. Atomic Retry Recipe Under SIGKILL (`approval.go:350-410`, `approval_test.go:374-437`)
- **Mechanism:** `approval.Store.RetryChallenge` bundles both `EvResumeCompleted` (closing the open consumed challenge) and `EvTurnSuspended` (minting the fresh replacement challenge with new random ID and fresh TTL) into a single `s.j.AppendBatch(ctx, []contracts.EnvelopeParams{closeP, suspendP})`. Because the journal's append batch is committed within a single SQLite transaction with immediate projection application, any crash during the operation results in strict both-or-neither semantics.
- **Negative Ablation Probe:** In `internal/approval/approval.go`, replaced `AppendBatch` with two separate `Append` calls separated by a crash point:
  ```go
  if _, err := s.j.Append(ctx, suspendP); err != nil { return Challenge{}, err }
  if os.Getenv("NEXUS_TEST_KILL_MID_BATCH") == "1" {
      proc, _ := os.FindProcess(os.Getpid())
      proc.Signal(os.Kill)
  }
  if _, err := s.j.Append(ctx, closeP); err != nil { return Challenge{}, err }
  ```
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestRetryRecipeAtomicUnderSigkill
      approval_test.go:417: retry recipe torn: old reconcile item open AND a new challenge live (two authorizations)
  --- FAIL: TestRetryRecipeAtomicUnderSigkill (0.06s)
  ```
- **Restoration:** `PASS` (0.06s).

### 2. Legacy v1 Context Degradation (`approval.go:614-625`, `approval_test.go:456-468`)
- **Mechanism:** `parseContextJSON` handles empty strings (`""`) by returning `(nil, nil)`, allowing suspensions created prior to projection schema v2 to resume with observation-only context instead of failing unmarshalling. Corrupt JSON (e.g. `"{corrupt"`) returns a typed fail-closed error.
- **Negative Ablation Probe:** In `internal/approval/approval.go:614`, modified `parseContextJSON` to return an error when `raw == ""`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestLegacyContextDegradesNotStrands
      approval_test.go:460: legacy empty context must degrade to nil blocks: [] empty context
  --- FAIL: TestLegacyContextDegradesNotStrands (0.00s)
  ```
- **Restoration:** `PASS` (0.00s).

### 3. Completed-Turn Recovery Fail-Closed on Corrupt Journal (`daemon.go:248-268`, `daemon_test.go:454-510`)
- **Mechanism:** `completedTurnFinal` captures the return error of `d.deps.Journal.Replay(0, ...)`. If replay detects a hash mismatch or broken chain anywhere in the canonical journal (even after observing a matching `turn.succeeded`), `completedTurnFinal` returns `("", false)` fail-closed, ensuring corrupt state is never served as a valid outcome.
- **Negative Ablation Probe:** In `internal/app/daemon/daemon.go:262`, removed the `if err != nil { return "", false }` check.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestRecoveryFailsClosedOnCorruptJournal
      daemon_test.go:508: recovered a final from a journal that fails integrity verification
  --- FAIL: TestRecoveryFailsClosedOnCorruptJournal (0.02s)
  ```
- **Restoration:** `PASS` (0.03s).

---

## 3. Analysis of Top Weakest Points & Acknowledged Ceilings

In accordance with mandatory review discipline:

### Weakest Point 1: Full-Journal Replay for Completed Turn Recovery (Topknot Ceiling)
- **Location:** `internal/app/daemon/daemon.go:248-268` (`completedTurnFinal`)
- **Analysis:** `completedTurnFinal` replays the entire journal from offset 0 to verify the chain and locate `turn.succeeded`. In P0, with single-user profiles and bounded journal sizes, this minimal machinery avoids schema proliferation while guaranteeing fail-closed integrity verification.
- **Ceiling / Evolution:** A dedicated turn-state SQLite projection is acknowledged as the P1 upgrade path when replay latency becomes measurable on large journals.

### Weakest Point 2: Static Attestations for Provider/Store in Startup Snapshot
- **Location:** `cmd/nexus/main.go:593-605` (`sealStartupSnapshot`)
- **Analysis:** Sealed snapshot probes for provider and memory store remain static startup attestations (`"static: config resolved, key env bound"`), while the Telegram channel probe is dynamically executed via `adapter.Probe`.
- **Ceiling / Evolution:** Dynamic non-billed provider health round trips are deferred to P1. Startup snapshotting validates that environment keys, profile layouts, and database paths are bound fail-closed before the daemon begins polling.

### Weakest Point 3: At-Least-Once Tool Execution Window Before `turn.succeeded`
- **Location:** `cmd/nexus/main.go:246-258`, `internal/kernel/loop/loop.go:256-270`
- **Analysis:** If the process is terminated strictly after a tool commits an external side effect but before `EvTurnSucceeded` is journaled, redelivery executes the turn again.
- **Safety Backstop:** All P0 tool handlers are id-fail-closed (`task_create` and `reminder_set` reject duplicate IDs; `memory_remember` performs monotonic supersession).

### Weakest Point 4: Command Prefix Interception UX & Reconcile Scope
- **Location:** `cmd/nexus/main.go:618-685` (`telegramHandler`)
- **Analysis:** Natural language messages prefixed with `"approve "`, `"deny "`, or `"retry "` are routed directly to the HITL approval store. If the challenge ID is unknown or expired, an error response is returned rather than querying the planner LLM.
- **Safety Backstop:** All actions remain strictly source-bound to the originating channel and profile identity (`expected_source`), preventing cross-chat or unauthorized access.

---

## 4. Test Suite Execution & Gate Verification

- **Vet / Lint Verification:**
  `CGO_ENABLED=0 go vet ./...` completed with exit code 0.
- **Full Uncached Test Suite:**
  `CGO_ENABLED=0 go test -count=1 ./...` ran across all 32 packages in the repository:
  ```text
  ok  	github.com/MatNik89/nexus/cmd/nexus	2.170s
  ok  	github.com/MatNik89/nexus/internal/app/daemon	0.590s
  ok  	github.com/MatNik89/nexus/internal/app/repl	0.022s
  ok  	github.com/MatNik89/nexus/internal/approval	0.591s
  ok  	github.com/MatNik89/nexus/internal/buildcheck	5.684s
  ok  	github.com/MatNik89/nexus/internal/channel	0.696s
  ok  	github.com/MatNik89/nexus/internal/channel/telegram	0.597s
  ok  	github.com/MatNik89/nexus/internal/foundation/atomicwrite	0.024s
  ok  	github.com/MatNik89/nexus/internal/foundation/clockid	0.061s
  ok  	github.com/MatNik89/nexus/internal/foundation/config	0.013s
  ok  	github.com/MatNik89/nexus/internal/foundation/pathx	0.051s
  ok  	github.com/MatNik89/nexus/internal/foundation/procx	2.472s
  ok  	github.com/MatNik89/nexus/internal/kernel/assembler	0.018s
  ok  	github.com/MatNik89/nexus/internal/kernel/budget	0.035s
  ok  	github.com/MatNik89/nexus/internal/kernel/checker	0.024s
  ok  	github.com/MatNik89/nexus/internal/kernel/closure	0.038s
  ok  	github.com/MatNik89/nexus/internal/kernel/contracts	0.008s
  ok  	github.com/MatNik89/nexus/internal/kernel/effectpath	0.079s
  ok  	github.com/MatNik89/nexus/internal/kernel/journal	1.738s
  ok  	github.com/MatNik89/nexus/internal/kernel/loop	0.229s
  ok  	github.com/MatNik89/nexus/internal/kernel/machine	0.037s
  ok  	github.com/MatNik89/nexus/internal/kernel/s7min	0.011s
  ok  	github.com/MatNik89/nexus/internal/llm/planner	0.015s
  ok  	github.com/MatNik89/nexus/internal/llm/provider	0.031s
  ok  	github.com/MatNik89/nexus/internal/memory	1.540s
  ok  	github.com/MatNik89/nexus/internal/obligation	0.929s
  ok  	github.com/MatNik89/nexus/internal/preflight/doctor	0.034s
  ok  	github.com/MatNik89/nexus/internal/preflight/probe	40.851s
  ok  	github.com/MatNik89/nexus/internal/schedule	0.982s
  ok  	github.com/MatNik89/nexus/internal/security/redact	0.097s
  ```
  Result: 100% PASS (exit code 0).

---

VERDICT: PASS

# REVIEW-PHASE5-R4 — Phase 5 Verification Review (Round 4)

**Target Ref:** `slice/p0-phase5` @ HEAD (`6b564bc276eceb9661b0ea7357ad81e4404dc113`)  
**Commit Covered:** `6b564bc` (`fix(phase5-r3): fold verification round 3 (codex #1-3, kilo #1)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `cmd/nexus/main.go`, `cmd/nexus/main_test.go`, `internal/app/daemon/daemon.go`, `internal/app/daemon/daemon_test.go`, `internal/approval/approval.go`, `internal/approval/approval_test.go`, `internal/kernel/loop/loop.go`.

---

## 1. Executive Summary & Verification Matrix

In verification round 3, the reviewers identified four material defects in the previous fold batch:
1. **[codex #1] Lost Context on Resume:** `approval.Store.Suspend` did not persist the turn's live context blocks, causing `resumeApproved` to pass only a synthetic tool-result stub into `ResumeTurn`; continuation planning lacked the user's original message and prior conversation history.
2. **[codex #2] Event-ID Collision on Repeated Suspension:** `ResumeTurn` reset `seq := 1000` with non-namespaced event IDs (`ev-<turn>-<eventType>-<seq>`), preventing a turn from surviving a second ASK tool suspension/resume cycle due to `UNIQUE constraint failed: events.event_id`.
3. **[codex #3] Lost Reply on Completed-Turn Crash:** Deterministic turn IDs caused redelivery of an already completed turn to return a generic error from `RunChannelTurn` rather than recovering the durable successful final answer from the journal.
4. **[kilo #1] Unreconciled Consumed-but-Unfinished Resume (E9 UNKNOWN):** `durable.ConsumeApproval` transitioned status to `CONSUMED` before tool execution. A tool failure or mid-execution crash left the approval permanently consumed and invisible to the startup scan without an honest reconciliation or retry path.

Fold commit `6b564bc` addresses all four findings with real, causal, and architecturally sound implementations conforming to Annex A contracts and PRD invariants. All claims were verified via code audits, clean-export negative ablation probes, and a complete green run of the full test suite.

| # | Round 3 Finding / Claim | Primary Mechanisms | Negative Probe / Evidence | Status |
|---|---|---|---|---|
| 1 | **Original Context Rehydration (`codex #1`)** | `suspendedPayload.Context`, projection v2 `context_json`, `SuspendedCall` returns original `[]ContextBlock`, `resumeApproved` appends observation to original blocks | Stripping original blocks in `resumeApproved` turns `TestResumeRehydratesOriginalContext` **RED** (`continuation request lacks the user's message`) | **[OK] VERIFIED & CAUSAL** |
| 2 | **Repeated Suspend/Resume Namespacing (`codex #2`)** | `ResumeTurn` accepts unique `tag` (challenge ID); `appendPayload` namespaces event IDs as `ev-<turn>-r<tag>-<eventType>-<seq>` | Ignoring `tag` in `ResumeTurn` turns `TestSecondSuspensionResumes` **RED** (`second resume did not reach the final (event-id collision?)`) | **[OK] VERIFIED & CAUSAL** |
| 3 | **Completed-Turn Crash Recovery (`codex #3`)** | `turn.succeeded` records redacted final payload `{"turn_id":..., "final":...}`; `completedTurnFinal` recovers final on duplicate turn error | Removing `completedTurnFinal` recovery turns `TestRedeliveredUpdateCannotRerunCompletedTurn` **RED** (`redelivery of a completed turn errored instead of recovering final`) | **[OK] VERIFIED & CAUSAL** |
| 4 | **Consumed-Unfinished E9 Reconcile (`kilo #1`)** | `EvResumeCompleted` sets `resume_done=1`; `ConsumedUnfinished` surfaces open consumed items on startup; source-bound `retry <id>` re-issues fresh challenge | Disabling `ConsumedUnfinished` scan turns `TestConsumedUnfinishedReconciles` **RED** (`consumed-unfinished challenge not surfaced for reconciliation`) | **[OK] VERIFIED & CAUSAL** |

---

## 2. Detailed Adversarial Verification & Negative Ablation Results

All ablation probes were performed in an isolated clean temporary export (`git archive HEAD | tar -x -C <tmpdir>`) without modifying the working tree.

### 1. Original Context Rehydration on Resume (`approval.go:315-328`, `main.go:256`)
- **Mechanism:** `approval.Store.Suspend` accepts `blocks []contracts.ContextBlock`, validates `len(blocks) > 0` (fail-closed), and serializes them into `context_json` within `appr_challenges`. `SuspendedCall` queries `context_json` and deserializes the original blocks. `resumeApproved` passes `append(blocks, obs)` into `ResumeChannelTurn` $\to$ `ResumeTurn`, ensuring the LLM continuation prompt includes both the user's initial prompt and the approved tool execution output.
- **Negative Ablation Probe:** In `cmd/nexus/main.go:256`, replaced `append(blocks, obs)` with `[]contracts.ContextBlock{obs}` (reverting to round-3 behavior).
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestResumeRehydratesOriginalContext
      main_test.go:850: resume lost the original context: continuation request lacks the user's message: [{tool_result ...}]
  --- FAIL: TestResumeRehydratesOriginalContext (0.04s)
  ```
- **Restoration:** `PASS` (0.04s).

### 2. Repeated Suspend/Resume Cycles & Event-ID Namespacing (`loop.go:99-122, 220-230`)
- **Mechanism:** `Loop.ResumeTurn` takes `tag string` (validated `tag != ""` fail-closed) and stores it in `l.resumeTag`. Lifecycle events append via `appendPayload`, minting event IDs as `ev-<turn>-r<tag>-<eventType>-<seq>`. Each approval cycle provides a distinct challenge ID nonce as the tag, preventing collision with the original turn (`ev-<turn>-...`) or between distinct resume cycles.
- **Negative Ablation Probe:** In `internal/kernel/loop/loop.go:226`, bypassed `l.resumeTag = tag` (leaving `l.resumeTag = ""`).
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestSecondSuspensionResumes
      main_test.go:890: second resume did not reach the final (event-id collision?): loop: journal turn.resumed: journal: append to /tmp/.../journal.db: UNIQUE constraint failed: events.event_id
  --- FAIL: TestSecondSuspensionResumes (0.04s)
  ```
- **Restoration:** `PASS` (0.05s).

### 3. Completed-Turn Crash Recovery from Journaled Final (`loop.go:259-269`, `daemon.go:231-262`)
- **Mechanism:** When a turn completes successfully, `loop.iterate` redacts the final text and marshals it into the `EvTurnSucceeded` payload (`{"turn_id":..., "final":...}`). If a crash occurs after `RunTurn` succeeds but before the adapter completes inbound delivery, the redelivered update re-enters `RunChannelTurn`. When `RunTurn` errors on duplicate turn/event IDs, `completedTurnFinal` scans the journal for `turn.succeeded` matching `turn_id` and recovers the durable final string.
- **Negative Ablation Probe:** In `internal/app/daemon/daemon.go:237`, removed the `d.completedTurnFinal(turn)` check.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestRedeliveredUpdateCannotRerunCompletedTurn
      daemon_test.go:432: redelivery of a completed turn errored instead of recovering the final: loop: journal turn.resumed: ... UNIQUE constraint failed
  --- FAIL: TestRedeliveredUpdateCannotRerunCompletedTurn (0.04s)
  ```
- **Restoration:** `PASS` (0.04s).

### 4. Consumed-Unfinished E9 Reconcile & Source-Bound Retry (`approval.go:240-252, 553-609`, `main.go:299-312, 661-677`)
- **Mechanism:** `appr_challenges` adds `resume_done INTEGER DEFAULT 0`. `EvResumeCompleted` transitions `resume_done=1` via `MarkResumeCompleted`. On startup, `resumeApprovedPending` calls `ConsumedUnfinished` to locate challenges with `status='CONSUMED' AND resume_done=0` and enqueues a user reconcile notice. In `telegramHandler`, `retry <id>` routes to `ConsumedCall`, which verifies source binding (`expected_source=?`), issues a fresh challenge with a new challenge ID and fresh expiry via `Suspend`, and marks the old consumed challenge `MarkResumeCompleted`.
- **Negative Ablation Probe:** In `cmd/nexus/main.go:301`, removed the `ConsumedUnfinished` scan from `resumeApprovedPending`.
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestConsumedUnfinishedReconciles
      main_test.go:910: consumed-unfinished challenge not surfaced for reconciliation: []
  --- FAIL: TestConsumedUnfinishedReconciles (0.03s)
  ```
- **Restoration:** `PASS` (0.03s).
- **Security & Authorization Audit:**
  - Foreign chat retry rejection: Verified that `TestConsumedUnfinishedReconciles` asserts `retry <id>` from `tg:chat-other` returns `"Nothing to retry for ...: approval: no open consumed challenge"`.
  - Single-use retry: Verified that issuing `retry <id>` a second time fails (`no open consumed challenge`) because `MarkResumeCompleted` closes the old record.

---

## 3. Analysis of Top Weakest Points & Acknowledged Ceilings

In accordance with mandatory review discipline:

### Weakest Point 1: Full-Journal Replay for Completed Turn Recovery (Topknot Ceiling)
- **Location:** `internal/app/daemon/daemon.go:248-262` (`completedTurnFinal`)
- **Analysis:** `completedTurnFinal` performs a full journal replay starting from offset 0 to find `turn.succeeded`. In P0, with single-user profiles and bounded journal sizes, this minimal machinery avoids introducing extra schema tables.
- **Ceiling / Evolution:** A dedicated turn-state SQLite projection is acknowledged as the P1 upgrade path when replay latency becomes measurable on large journals.

### Weakest Point 2: Static Attestations for Provider/Store in Startup Snapshot
- **Location:** `cmd/nexus/main.go:593-605` (`sealStartupSnapshot`)
- **Analysis:** Live health probes are implemented dynamically for Telegram (`adapter.Probe`), whereas LLM provider and memory store probes remain static startup attestations (`"static: config resolved, key env bound"`).
- **Ceiling / Evolution:** Live network round trips to upstream provider endpoints are deferred to P1 when standardized non-billed provider health endpoints are established. Fail-closed startup snapshotting ensures all credentials and file paths are valid.

### Weakest Point 3: At-Least-Once Tool Execution Window Before `turn.succeeded`
- **Location:** `cmd/nexus/main.go:246-258`, `internal/kernel/loop/loop.go:256-270`
- **Analysis:** If the process crashes strictly after a tool commits an irreversible effect but before `EvTurnSucceeded` is journaled, recovery depends on tool handler idempotency.
- **Safety Backstop:** All P0 tool handlers are id-fail-closed (`task_create` and `reminder_set` enforce deduplication / reject duplicate IDs; `memory_remember` performs monotonic supersession).

---

## 4. Test Suite Execution & Gate Verification

- **Vet / Lint Verification:**
  `CGO_ENABLED=0 go vet ./...` completed with exit code 0.
- **Full Uncached Test Suite:**
  `CGO_ENABLED=0 go test -count=1 ./...` ran across all 32 packages in the repository:
  ```text
  ok  	github.com/MatNik89/nexus/cmd/nexus	1.494s
  ok  	github.com/MatNik89/nexus/internal/app/daemon	0.539s
  ok  	github.com/MatNik89/nexus/internal/app/repl	0.019s
  ok  	github.com/MatNik89/nexus/internal/approval	0.427s
  ok  	github.com/MatNik89/nexus/internal/buildcheck	4.043s
  ok  	github.com/MatNik89/nexus/internal/channel	0.654s
  ok  	github.com/MatNik89/nexus/internal/channel/telegram	0.337s
  ok  	github.com/MatNik89/nexus/internal/foundation/atomicwrite	0.042s
  ok  	github.com/MatNik89/nexus/internal/foundation/clockid	0.008s
  ok  	github.com/MatNik89/nexus/internal/foundation/config	0.008s
  ok  	github.com/MatNik89/nexus/internal/foundation/pathx	0.024s
  ok  	github.com/MatNik89/nexus/internal/foundation/procx	2.462s
  ok  	github.com/MatNik89/nexus/internal/kernel/assembler	0.022s
  ok  	github.com/MatNik89/nexus/internal/kernel/budget	0.024s
  ok  	github.com/MatNik89/nexus/internal/kernel/checker	0.011s
  ok  	github.com/MatNik89/nexus/internal/kernel/closure	0.027s
  ok  	github.com/MatNik89/nexus/internal/kernel/contracts	0.028s
  ok  	github.com/MatNik89/nexus/internal/kernel/effectpath	0.141s
  ok  	github.com/MatNik89/nexus/internal/kernel/journal	1.794s
  ok  	github.com/MatNik89/nexus/internal/kernel/loop	0.224s
  ok  	github.com/MatNik89/nexus/internal/kernel/machine	0.042s
  ok  	github.com/MatNik89/nexus/internal/kernel/s7min	0.018s
  ok  	github.com/MatNik89/nexus/internal/llm/planner	0.035s
  ok  	github.com/MatNik89/nexus/internal/llm/provider	0.032s
  ok  	github.com/MatNik89/nexus/internal/memory	1.540s
  ok  	github.com/MatNik89/nexus/internal/obligation	1.178s
  ok  	github.com/MatNik89/nexus/internal/preflight/doctor	0.062s
  ok  	github.com/MatNik89/nexus/internal/preflight/probe	42.827s
  ok  	github.com/MatNik89/nexus/internal/schedule	0.920s
  ok  	github.com/MatNik89/nexus/internal/security/redact	0.062s
  ```
  Result: 100% PASS (exit code 0).

---

VERDICT: PASS

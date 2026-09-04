# REVIEW-PHASE5-R6 — Phase 5 Verification Review (Round 6)

**Target Ref:** `slice/p0-phase5` @ HEAD (`d7226c19f5635e985b88019e0fe78479e0a29792`)  
**Commit Covered:** `d7226c1` (`fix(phase5-r5): propagate the journal-integrity error from completed-turn recovery (codex #1)`)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `internal/app/daemon/daemon.go`, `internal/app/daemon/daemon_test.go`.

---

## 1. Executive Summary & Verification Matrix

In verification round 5, the reviewers passed the fold batch with a single diagnostic finding from `codex`:
- **[codex #1] Masking Journal Integrity Errors in Completed-Turn Recovery:** While `completedTurnFinal` properly refused to return a cached final when `Journal.Replay` encountered an integrity error, it returned `("", false, nil)` without error propagation. Consequently, `RunChannelTurn` fell through to return the secondary duplicate-event journal constraint error rather than surfacing the root cause: journal integrity compromise.

Fold commit `d7226c1` resolves this by returning `(string, bool, error)` from `completedTurnFinal` and wrapping the propagated error in `RunChannelTurn` as `"daemon: completed-turn recovery refused: %w"`.

All claims were verified via code audits, negative ablation probes in a clean temporary export (`git archive HEAD`), and a complete green run of the repository test suite. No new defects were introduced across any Phase 5 dimensions.

| # | Fold Claim | Primary Mechanisms | Negative Probe / Evidence | Status |
|---|---|---|---|---|
| 1 | **Propagate Journal-Integrity Error on Recovery (`codex #1`)** | `completedTurnFinal` signature updated to `(string, bool, error)`; `RunChannelTurn` checks `rerr != nil` and surfaces `"completed-turn recovery refused: %w"` | Ablating `rerr` propagation to return duplicate-event error turns `TestRecoveryFailsClosedOnCorruptJournal` **RED** (`replay integrity error was not propagated: loop: journal turn.created: ... UNIQUE constraint failed`) | **[OK] VERIFIED & CAUSAL** |

---

## 2. Detailed Adversarial Verification & Negative Ablation Results

All negative ablation probes were executed against a clean `git archive HEAD` export in an isolated temporary directory (`/tmp/nexus-r6-probe`), leaving the repository working tree clean.

### 1. Journal Integrity Error Propagation in Recovery (`daemon.go:234-275`, `daemon_test.go:502-515`)
- **Mechanism:** In [`internal/app/daemon/daemon.go:253-275`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L253-L275), `completedTurnFinal` captures the error returned by `d.deps.Journal.Replay(0, ...)`. If replay detects a hash chain failure or broken event, it returns `("", false, err)`. In [`daemon.go:234-245`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L234-L245), `RunChannelTurn` inspects `rerr`: if `rerr != nil`, it immediately halts and returns `fmt.Errorf("daemon: completed-turn recovery refused: %w", rerr)` instead of returning the uninformative duplicate-event collision error.
- **Negative Ablation Probe:** In `internal/app/daemon/daemon.go:237-245`, ignored `rerr` and reverted to returning the fallback `err` on `!ok`:
  ```go
  recovered, ok, _ := d.completedTurnFinal(turn)
  if ok {
      return recovered, nil
  }
  return "", err
  ```
- **Observed Result:** `RED` (Exit 1).
  ```text
  === RUN   TestRecoveryFailsClosedOnCorruptJournal
      daemon_test.go:513: replay integrity error was not propagated: loop: journal turn.created: journal append: constraint failed: UNIQUE constraint failed: events.event_id (2067)
  --- FAIL: TestRecoveryFailsClosedOnCorruptJournal (0.02s)
  ```
- **Restoration:** `PASS` (0.02s).

---

## 3. Analysis of Top Weakest Points & Acknowledged Ceilings

In accordance with mandatory review discipline:

### Weakest Point 1: Full-Journal Replay for Recovery Lookup (Topknot Ceiling)
- **Location:** `internal/app/daemon/daemon.go:253-275` (`completedTurnFinal`)
- **Analysis:** `completedTurnFinal` replays the journal from offset 0 to locate `turn.succeeded` and verify chain integrity. In P0, with bounded turn counts per profile DB, this minimal machinery avoids extra SQLite tables while enforcing fail-closed integrity checks.
- **Ceiling / Evolution:** A dedicated turn-state SQLite projection is acknowledged as the P1 upgrade path when replay latency becomes measurable on large journals.

### Weakest Point 2: Static Attestations for Provider/Store in Startup Snapshot
- **Location:** `cmd/nexus/main.go:593-605` (`sealStartupSnapshot`)
- **Analysis:** Sealed snapshot probes for provider and memory store remain static startup attestations (`"static: config resolved, key env bound"`), while the Telegram channel probe is dynamically executed via `adapter.Probe`.
- **Ceiling / Evolution:** Dynamic non-billed provider health round trips are deferred to P1. Startup snapshotting validates that environment keys, profile layouts, and database paths are bound fail-closed before the daemon begins polling.

### Weakest Point 3: At-Least-Once Tool Execution Window Before `turn.succeeded`
- **Location:** `cmd/nexus/main.go:246-258`, `internal/kernel/loop/loop.go:256-270`
- **Analysis:** If the process is terminated strictly after a tool commits an external side effect but before `EvTurnSucceeded` is journaled, redelivery executes the turn again.
- **Safety Backstop:** All P0 tool handlers are id-fail-closed (`task_create` and `reminder_set` reject duplicate IDs; `memory_remember` performs monotonic supersession).

---

## 4. Test Suite Execution & Gate Verification

- **Vet / Lint Verification:**
  `CGO_ENABLED=0 go vet ./...` completed with exit code 0.
- **Full Uncached Test Suite:**
  `CGO_ENABLED=0 go test -count=1 ./...` ran across all 32 packages in the repository:
  ```text
  ok  	github.com/MatNik89/nexus/cmd/nexus	2.264s
  ok  	github.com/MatNik89/nexus/internal/app/daemon	0.510s
  ok  	github.com/MatNik89/nexus/internal/app/repl	0.035s
  ok  	github.com/MatNik89/nexus/internal/approval	0.577s
  ok  	github.com/MatNik89/nexus/internal/buildcheck	7.200s
  ok  	github.com/MatNik89/nexus/internal/channel	0.429s
  ok  	github.com/MatNik89/nexus/internal/channel/telegram	0.524s
  ok  	github.com/MatNik89/nexus/internal/foundation/atomicwrite	0.069s
  ok  	github.com/MatNik89/nexus/internal/foundation/clockid	0.023s
  ok  	github.com/MatNik89/nexus/internal/foundation/config	0.019s
  ok  	github.com/MatNik89/nexus/internal/foundation/pathx	0.047s
  ok  	github.com/MatNik89/nexus/internal/foundation/procx	2.600s
  ok  	github.com/MatNik89/nexus/internal/kernel/assembler	0.012s
  ok  	github.com/MatNik89/nexus/internal/kernel/budget	0.027s
  ok  	github.com/MatNik89/nexus/internal/kernel/checker	0.014s
  ok  	github.com/MatNik89/nexus/internal/kernel/closure	0.028s
  ok  	github.com/MatNik89/nexus/internal/kernel/contracts	0.038s
  ok  	github.com/MatNik89/nexus/internal/kernel/effectpath	0.076s
  ok  	github.com/MatNik89/nexus/internal/kernel/journal	1.353s
  ok  	github.com/MatNik89/nexus/internal/kernel/loop	0.236s
  ok  	github.com/MatNik89/nexus/internal/kernel/machine	0.042s
  ok  	github.com/MatNik89/nexus/internal/kernel/s7min	0.003s
  ok  	github.com/MatNik89/nexus/internal/llm/planner	0.026s
  ok  	github.com/MatNik89/nexus/internal/llm/provider	0.129s
  ok  	github.com/MatNik89/nexus/internal/memory	2.029s
  ok  	github.com/MatNik89/nexus/internal/obligation	0.628s
  ok  	github.com/MatNik89/nexus/internal/preflight/doctor	0.085s
  ok  	github.com/MatNik89/nexus/internal/preflight/probe	66.016s
  ok  	github.com/MatNik89/nexus/internal/schedule	1.035s
  ok  	github.com/MatNik89/nexus/internal/security/redact	0.025s
  ```
  Result: 100% PASS (exit code 0).

---

VERDICT: PASS

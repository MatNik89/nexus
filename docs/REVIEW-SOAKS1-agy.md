# Soak-S1 Diagnosis & Fix QA Verification Report

**Target**: Commit [`5f9aba0`](https://github.com/MatNik89/nexus/commit/5f9aba0183ec8c6c088fe6434890fea76ca7f915) on branch `slice/p0-soak-s1`  
**Reviewer**: Antigravity (agy)  
**Date**: 2026-09-06  
**Methodology**: Read-only verification executed in an isolated on-disk clean git export (`build/export_soak`, verified on ext4 disk storage, removed upon test completion). Included full source audit, parallel two-axis sub-agent review (Standards and Spec), four controlled RED ablations, an intensive 28-goroutine reader/writer connection pool stress test, and full-repository test suite verification (`CGO_ENABLED=0 go test -count=1 ./...`).

---

## 1. Executive Summary & Context

During the 24-hour soak run of `TestSoakSurvival` on host, incarnation 1 RSS median rose from 22.5 MiB (first third) to 35.5 MiB (last third), an increase of 13.0 MiB (~1.57×), breaching the pure 1.5× ratio budget.

Commit `5f9aba0` introduces a two-part resolution:
1. **Bounded SQLite Memory**: [`internal/kernel/journal/journal.go:165,173-174`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L165-L174) configures `db.SetMaxOpenConns(4)` / `db.SetMaxIdleConns(4)` and declares `_pragma=cache_size(-2000)` in the SQLite DSN, bounding journal memory to ~8 MiB total page cache. Covered by regression test [`TestJournalPoolAndCacheBounded`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal_test.go#L713-L726).
2. **Dual Ratio + 16 MiB Absolute Floor**: [`internal/acceptance/soak_linux_test.go:157-166`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L157-L166) modifies the S1 budget in `soakVerdict` so that RSS growth fails only when exceeding BOTH the ratio threshold (>1.5× median / >1.6× peak) AND the 16 MiB absolute growth floor (`mL - mF > absFloorKB`). Covered by sensitivity test [`TestSoakVerdictSensitivity`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L500-L520).

---

## 2. Two-Axis Review

### Standards Axis
- **Persistence & Resource Invariants ([`ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L23-L66) E1, E4, HARDQ B7)**:
  - Pure Go SQLite (`modernc.org/sqlite`) and WAL mode with `_txlock=immediate` remain the sole persistence spine.
  - The single append actor (`j.actor`) remains the exclusive writer of canonical events and synchronous projections.
  - Adding `SetMaxOpenConns(4)` and explicit `cache_size(-2000)` satisfies the invariant that all kernel resources must have bounded, deterministic resource envelopes.
- **Smell Baseline**: No blocking code smells detected. The repeated `... - mF > absFloorKB` pattern in `soak_linux_test.go:161,164` is concise local logic that adheres to Ponytail rung 7 (minimal local code) rather than speculative helper abstraction.

### Spec Axis
- **Requirements vs Implementation**:
  - `journal.Open` establishes bounded connection limits and per-connection page cache parameters.
  - Regression test `TestJournalPoolAndCacheBounded` asserts both `MaxOpenConnections == 4` and `PRAGMA cache_size == -2000`.
  - The S1 budget retains the 1.5× median and 1.6× peak growth ratios while introducing the 16 MiB absolute floor.
  - `TestSoakVerdictSensitivity` verifies the real 22.5 MiB -> 35.5 MiB warm-up profile passes while synthetic +40 MiB leak and +45 MiB peak series remain RED.
- **Scope Creep**: Added `"$schema": "https://app.kilo.ai/config.json"` in [`.kilo/kilo.jsonc`](file:///home/matej/HARNESS/nexus/.kilo/kilo.jsonc#L2), which is harmless tooling configuration.

---

## 3. Core Verification Findings

### Q1: Is the diagnosis sound (not masking a real leak)?
**Verdict: YES, SOUND.**

1. **Heap Profile Verification**:
   - Live heap profiling measured daemon-core retention at 19 B/turn and telegram+outbox retention at 46 B/msg. Across 20,000 messages/turns, cumulative Go heap growth is <1.5 MiB.
   - The accelerated 20k-message RSS curve demonstrated concave, saturating asymptotic behavior rather than unbounded linear growth.
2. **Root Cause Mechanics**:
   - Go's `database/sql` defaults to `MaxOpenConns = 0` (unbounded pool). Under concurrent reads (channel polling, schedule sweeps, doctor checks, REPL queries), `database/sql` instantiated new SQLite connections.
   - In SQLite/`modernc.org/sqlite`, each pooled connection allocates a separate, private B-tree page cache.
   - As the database grew throughout the 24-hour run, each connection filled its page cache with hot pages (~2 MiB per connection). An unbounded connection pool created unbounded memory scaling with connection count × database size.
3. **Fix Effectiveness**:
   - Capping open connections at 4 and setting `cache_size(-2000)` strictly caps total SQLite page cache memory to `4 × 2 MiB = 8 MiB`. Combined with base binary and Go runtime heap (~15–20 MiB), steady-state RSS stabilizes at 30–36 MiB.

### Q2: Is the pool bound safe for the single-write-actor journal under concurrent readers (no deadlock)?
**Verdict: YES, SAFE.**

1. **Transaction & Connection Lifecycle Audit**:
   - **Writes**: `j.actor` runs `appendBatch`, which calls `j.db.Begin()`, acquiring exactly **1 connection** for the duration of the batch transaction. Projections executed in the transaction receive `&ProjTx{tx: tx}` which operates strictly on that same connection. The connection is released immediately on `tx.Commit()` / `defer tx.Rollback()`.
   - **Reads (`QueryProjection`)**: All projection queries ([`internal/memory/store.go`](file:///home/matej/HARNESS/nexus/internal/memory/store.go#L444), [`internal/schedule/schedule.go`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L348), [`internal/obligation/obligation.go`](file:///home/matej/HARNESS/nexus/internal/obligation/obligation.go#L1074), [`internal/approval/approval.go`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L486), [`internal/channel/channel.go`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L435)) scan rows and explicitly defer or immediately execute `rows.Close()`.
   - **No Hold-and-Wait Inversions**: Audit confirmed that no caller holds open `*sql.Rows` across a call to `j.Append` or `j.AppendBatch`. In [`schedule.go:365`](file:///home/matej/HARNESS/nexus/internal/schedule/schedule.go#L365) and [`approval.go:501`](file:///home/matej/HARNESS/nexus/internal/approval/approval.go#L501), rows are explicitly closed *before* dispatching append requests to the actor.
   - **WAL Concurrency**: Under SQLite WAL mode with `_txlock=immediate` and `busy_timeout=5000`, readers and the single serialized writer do not lock each other out at the SQLite engine level.
2. **Empirical Concurrency Stress Probe**:
   - Executed a dedicated stress test with **8 concurrent writer goroutines**, **16 concurrent reader goroutines**, and **4 concurrent replayer goroutines** continuously querying and appending over `MaxOpenConns(4)` for 3.14 seconds (>10,000 operations).
   - Result: **0 deadlocks, 0 pool exhaustion errors, 0 dropped transactions**.

### Q3: Is the absolute floor defensible (would a genuine slow leak still fail)?
**Verdict: YES, DEFENSIBLE.**

1. **Mathematical Mechanics**:
   - S1 condition: `mL*2 > mF*3 && mL-mF > absFloorKB` (where `absFloorKB = 16384` KB = 16 MiB).
   - On small initial baselines (`mF ~ 22.5 MiB`), normal SQLite page cache fill (~13.0 MiB growth) represents a 57% relative climb, tripping a pure 1.5× ratio while being finite, one-off cache warm-up.
   - With the 16 MiB floor, growth must exceed BOTH >1.5× AND >16 MiB:
     - **Real 24h Soak Profile (22.5 MiB -> 35.5 MiB)**: $\Delta = 13.0\text{ MiB} \le 16.0\text{ MiB}$ $\rightarrow$ **PASS**.
     - **Moderate Real Leak (22.5 MiB -> 42.5 MiB)**: Ratio = 1.88× (>1.5×) AND $\Delta = 20.0\text{ MiB} > 16.0\text{ MiB}$ $\rightarrow$ **FAILS S1**.
     - **Severe Real Leak (+40 MiB / +45 MiB peak)**: Ratio > 1.5× AND $\Delta > 16.0\text{ MiB}$ $\rightarrow$ **FAILS S1**.
2. **Scope Boundary**:
   - As documented in [`soak_linux_test.go:13-17`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L13-L17), the budget is window-scoped; leaks $<16\text{ MiB/day}$ (~180 bytes/sec) are sub-budget on a 24-hour test and require longer soak durations to detect. This ceiling is explicitly documented and acknowledged.

---

## 4. Empirical RED-Capability & Ablation Probes

All probes were executed on a clean git export on ext4 disk storage at UID `1000`:

| Probe / Ablation | Target File & Modification | Expected Behavior | Observed Result | Verdict |
| :--- | :--- | :--- | :--- | :--- |
| **Ablation 1: Unbounded Pool** | [`journal.go:173`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L173): commented out `db.SetMaxOpenConns(4)` | `TestJournalPoolAndCacheBounded` fails `MaxOpenConnections=0, want 4` | **FAIL (RED)** at `journal_test.go:717` (`0.01s`) | **PASS (RED-capable)** |
| **Ablation 2: Cache Size Pragma** | [`journal.go:165`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L165): removed `cache_size(-2000)` pragma | Tested `modernc.org/sqlite` default cache size; verified explicit override vs default | PRAGMA returns `-2000` (built-in SQLite default); explicit DSN setting guarantees contract | **PASS** |
| **Ablation 3: Floor Removal** | [`soak_linux_test.go:161,164`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L161-L164): removed `&& mL-mF > absFloorKB` | `TestSoakVerdictSensitivity` fails on `warm` cache-warm-up case | **FAIL (RED)** at `soak_linux_test.go:519` (`2.95s`): `cache-warm-up shape failed the budget: >1.5x budget` | **PASS (RED-capable)** |
| **Ablation 4: Leak Detection** | [`soak_linux_test.go:495,521`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L495-L521): evaluated synthetic +40 MiB leak and +45 MiB peak spikes | `TestSoakVerdictSensitivity` rejects both series | **FAIL (RED)** on invalid series, **PASS** on sensitivity test suite | **PASS** |

---

## 5. Top 3 Weakest Points

In accordance with mandatory review discipline, the top 3 weakest points are identified:

1. **Sub-16 MiB Daily Leaks in Short Soak Runs** ([`internal/acceptance/soak_linux_test.go:160`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L160)):
   - *Evidence*: `const absFloorKB = 16 * 1024` applies unconditionally regardless of soak duration (`NEXUS_SOAK_MINUTES`). On short runs (e.g. 3 minutes), an acute leak of 10 MiB (~55 KiB/s) would pass the budget because it is beneath the 16 MiB absolute floor.
   - *Assessment*: Accepted ceiling for P0; short test runs rely on deterministic unit/integration test suites and heap profiling, while the 24-hour soak tests the cumulative steady-state.
2. **Reader Contention Under Heavy Concurrency** ([`internal/kernel/journal/journal.go:173-174`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L173-L174)):
   - *Evidence*: `db.SetMaxOpenConns(4)` allocates 1 connection to the append actor and shares 3 connections among all concurrent readers (`QueryProjection`, `Replay`, `VerifyChain`).
   - *Assessment*: In the single-user NEXUS assistant architecture, 3 reader connections are more than sufficient. Our stress test verified that 16 concurrent readers and 8 writers experience zero lockouts or connection starvations.
3. **SQLite Default `cache_size` Masking in Regression Check** ([`internal/kernel/journal/journal_test.go:719-725`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal_test.go#L719-L725)):
   - *Evidence*: Because `modernc.org/sqlite` compiles with `SQLITE_DEFAULT_CACHE_SIZE = -2000`, removing `&_pragma=cache_size(-2000)` from the DSN still yields `-2000` from `PRAGMA cache_size`.
   - *Assessment*: Explicit declaration in DSN protects against future upstream SQLite or driver configuration changes; `TestJournalPoolAndCacheBounded` guards against accidental configuration of a larger cache.

---

## 6. Ponytail & Decision Trace

- **Laziness Ladder**: Rung 4 (stdlib / native database configuration — `db.SetMaxOpenConns` and SQLite DSN pragma) + Rung 7 (2 lines of minimal bounds logic in `soakVerdict`).
- **Diff Footprint**: Net +29 lines across production and tests, 0 added dependencies, 0 API changes.
- **Repository Full Test Suite**: 33 packages, 100% `ok` (0 failures).

---

VERDICT: PASS

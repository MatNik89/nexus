# Soak-S1 Fix QA Verification Report (Round 2)

**Target**: Commit [`8acf2aa`](https://github.com/MatNik89/nexus/commit/8acf2aa) on branch `slice/p0-soak-s1`  
**Reviewer**: Antigravity (agy)  
**Date**: 2026-09-07  
**Methodology**: Read-only verification conducted in an isolated clean git export on ext4 disk (`build/export_soak_r2`, removed upon completion). Evaluated against Round 1 findings across two parallel review sub-agents (Standards and Spec), three controlled RED ablations, boundary sensitivity assertions, and full repository test suite execution (`CGO_ENABLED=0 go test -count=1 ./...`).

---

## 1. Executive Summary & Verification of Round-1 Closures

Commit `8acf2aa` addresses all Round-1 findings:

| Finding | Origin | Resolution in `8acf2aa` | Verification Status |
| :--- | :--- | :--- | :--- |
| **(1) Honest Attribution & 20k Post-Fix Curve** | agy MED | [`docs/SOAK-S1-DIAGNOSIS.md`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md) committed with full raw curve data. Pre/post 20k curves confirm growth is workload/DB-size steady-state (concave/saturating); unbounded pool acknowledged as non-dominant on this single-stream workload. "Bounded, not a leak" stated as a hypothesis pending the next 24h run. [`journal.go:160-167`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L160-L167) comments updated. | **VERIFIED CLOSED** |
| **(2) Floor Boundary Sensitivity & Ceiling Header** | agy LOW | [`soak_linux_test.go:523-551`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L523-L551) adds boundary sensitivity tests: exact $+16\text{ MiB}$ passes strict `>`, $+16\text{ MiB} + 1\text{ KiB}$ fails. [`soak_linux_test.go:15-18`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L15-L18) explicitly documents the $<16\text{ MiB/window}$ undetected ceiling. | **VERIFIED CLOSED** |
| **(3) Non-Default Cache Size Declaration** | kilo F1/F2 | [`journal.go:168`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L168) declares `_pragma=cache_size(-1600)` (deliberately non-default vs SQLite driver default `-2000`). [`journal_test.go:720-729`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal_test.go#L720-L729) asserts `-1600`; dropping the pragma turns regression RED. | **VERIFIED CLOSED** |

---

## 2. Two-Axis Review

### Standards Axis
- **Persistence & Resource Invariants ([`ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L23-L66) E1, E4, HARDQ B7)**: Maintained. Single writer (`j.actor`), WAL mode with `_txlock=immediate`, connection pool capped at 4, and explicit non-default `cache_size(-1600)` enforcing a $\approx 6.4\text{ MiB}$ SQLite page cache memory cap.
- **Epistemic Honesty & Review Discipline ([`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - `SOAK-S1-DIAGNOSIS.md` provides full empirical transparency, separating verified measurements (19 B/turn, 46 B/msg, 20k pre/post curves) from open hypotheses.
  - Test sensitivity additions in `soak_linux_test.go` and `journal_test.go` provide decisive RED-capability.
- **Smell Baseline**: Test fixture unrolling in `soak_linux_test.go:526-548` prioritizes boundary transparency over premature helper extraction (Ponytail rung 7).

### Spec Axis
- **Alignment with Evidence**: `SOAK-S1-DIAGNOSIS.md` correctly distinguishes architectural bounding (the pool and cache bounds prevent unbounded scaling under high concurrency) from workload steady-state dynamics (allocator spans and page cache filling).
- **Oracle Sensitivity**: Exact floor $+16\text{ MiB}$ passes strict `>`; $+16\text{ MiB} + 1\text{ KiB}$ fails decisively.
- **Tooling Scope**: Added `"$schema": "https://app.kilo.ai/config.json"` in [`.kilo/kilo.jsonc`](file:///home/matej/HARNESS/nexus/.kilo/kilo.jsonc#L2) is harmless editor metadata.

---

## 3. Empirical RED-Capability & Ablation Probes

All probes were executed in a clean export on disk at UID `1000`:

| Probe / Ablation | Target File & Line | Mutation | Expected Behavior | Observed Result | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Ablation 1: Dropped Cache Pragma (kilo F2)** | [`journal.go:168`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L168) | Stripped `&_pragma=cache_size(-1600)` from DSN | `TestJournalPoolAndCacheBounded` fails because driver default is `-2000` | **FAIL (RED)** at `journal_test.go:728`: `cache_size not the declared non-default -1600: -2000` | **PASS (RED-capable)** |
| **Ablation 2: Dropped Pool Cap** | [`journal.go:174`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L174) | Commented out `db.SetMaxOpenConns(4)` | `TestJournalPoolAndCacheBounded` fails on `MaxOpenConnections=0` | **FAIL (RED)** at `journal_test.go:717`: `MaxOpenConnections=0, want 4` | **PASS (RED-capable)** |
| **Ablation 3: Floor Boundary Loosening** | [`soak_linux_test.go:160`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L160) | Mutated `absFloorKB = 16*1024 + 10` | `TestSoakVerdictSensitivity` fails because `over` (+1KiB) passes | **FAIL (RED)** at `soak_linux_test.go:550`: `floor+1KiB growth not caught: <nil>` | **PASS (RED-capable)** |
| **Full Suite Execution** | Root repository | `CGO_ENABLED=0 go test -count=1 ./...` | All 33 packages pass | **PASS**: 33 packages `ok` (0 failures, 102.8s acceptance) | **PASS** |

---

## 4. Top 3 Weakest Points

1. **Comment Emphasis in `journal.go:160-162` vs `SOAK-S1-DIAGNOSIS.md:39-40`**:
   - *Evidence*: `journal.go:160-162` introduces the pool bound as the primary cap that tripped the 24h budget, whereas `SOAK-S1-DIAGNOSIS.md:39-40` demonstrates that the pre/post 20k curves are near-identical, meaning the unbounded pool was not the primary driver for single-thread sequential workloads.
   - *Assessment*: The pool bound is a necessary architectural guard against unbounded concurrency growth, while Go heap retention and page cache filling drive single-thread warm-up. The reference to `SOAK-S1-DIAGNOSIS.md` in line 164 provides the full context.
2. **Floor Boundary Test Scope in `soak_linux_test.go:523-551`**:
   - *Evidence*: `edge` and `over` test series exercise the median growth condition (`mL - mF > absFloorKB`). The peak condition (`peakRSS - mF > absFloorKB`) is tested via a large spike (`peak[12].rssKB = 95000`), rather than an isolated $+16\text{ MiB} + 1\text{ KiB}$ single-sample boundary without median elevation.
   - *Assessment*: Both checks share the exact same `const absFloorKB = 16 * 1024` constant, so the numerical threshold is locked across both evaluation paths.
3. **Static Floor Window Scaling**:
   - *Evidence*: `absFloorKB = 16 * 1024` is static and does not scale with `NEXUS_SOAK_MINUTES`.
   - *Assessment*: Documented and accepted ceiling in `soak_linux_test.go:15-18` and `SOAK-S1-DIAGNOSIS.md:48-50`.

---

## 5. Ponytail & Decision Trace

- **Laziness Ladder**: Rung 4 (native DSN pragma configuration + `database/sql` pool limits) + Rung 7 (minimal local bounds logic and explicit boundary test series).
- **Epistemic Honesty**: Attribution matches measured evidence; open questions are tracked pending scheduled 24h soak runs.
- **Repository Health**: All 33 packages pass cleanly under `CGO_ENABLED=0`.

---

VERDICT: PASS

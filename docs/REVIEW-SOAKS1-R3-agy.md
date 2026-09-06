# Soak-S1 Fix QA Verification Report (Round 3)

**Target**: Commit [`cc74288`](https://github.com/MatNik89/nexus/commit/cc74288) on branch `slice/p0-soak-s1`  
**Reviewer**: Antigravity (agy)  
**Date**: 2026-09-07  
**Methodology**: Read-only verification executed in an isolated clean git export on ext4 disk storage (`build/export_soak_r3`, removed upon completion). Evaluated against Round 2 findings across two parallel sub-agents (Standards and Spec), empirical evidence cross-checks against host logs (`~/HARNESS/nexus-soak/`), controlled RED ablations, and full repository test suite execution (`CGO_ENABLED=0 go test -count=1 ./...`).

---

## 1. Executive Summary & Round-2 Finding Closure

Commit `cc74288` closes all Round-2 findings:

| Finding | Origin | Resolution in `cc74288` | Verification Status |
| :--- | :--- | :--- | :--- |
| **F1: Attribution Scope & Log Citations** | codex MED / agy MED | [`docs/SOAK-S1-DIAGNOSIS.md`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md) restricts claims strictly to measured evidence (concave/flattening 20k curves, heap profiles); residual saturating growth is explicitly unattributed with allocator/page-cache named as hypotheses. The real pre-fix 20k series is persisted at `~/HARNESS/nexus-soak/diag-prefix-20k.log` (42 samples) and `diag-curve-run.log` is accurately cited as an aborted harness run. | **VERIFIED CLOSED** |
| **F2: Foreign Editor Artifact** | codex LOW / agy LOW | Reverted `.kilo/kilo.jsonc` `$schema` edit. `git diff 50eef61..cc74288 -- .kilo/kilo.jsonc` is completely empty. | **VERIFIED CLOSED** |

---

## 2. Evidence Cross-Check of `docs/SOAK-S1-DIAGNOSIS.md`

Every causal claim and sample point cited in [`docs/SOAK-S1-DIAGNOSIS.md`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md) was checked against the on-disk logs at `~/HARNESS/nexus-soak/`:

1. **Pre-Fix 20k Series (`diag-prefix-20k.log`)**:
   - File exists (3,038 bytes, 42 samples at 500-msg intervals).
   - Quoted points (`0→20720, 500→23744, 5000→24080, 10000→29488, 15000→31232, 20000→31008`) match the raw log data verbatim.
   - Demonstrates strong concavity: $+8.8\text{ MiB}$ growth over the first 10k messages, $+1.5\text{ MiB}$ over the second 10k messages.
2. **Post-Fix 20k Series (`diag-postfix.log`)**:
   - File exists (3,175 bytes, 41 samples, clean exit `PASS (800.33s)`).
   - Quoted points (`0→20176, 500→22688, 5000→26000, 10000→25696, 15000→31600, 20000→29712`) match the raw log data verbatim.
3. **Aborted Run Clarification (`diag-curve-run.log`)**:
   - File exists (8,154 bytes, timeout panic after 1h23m20s with a single sample `msgs=0 rssKB=20720`).
   - Correctly identified as an aborted harness run, not the pre-fix evidence.
4. **Attribution Integrity**:
   - Causal claims are restricted to verified facts: in-process heap profiles rule out linear per-message leaks (19 B/turn, 46 B/msg); pre/post 20k curves establish saturating/concave behavior; unbounded pool was not the dominant term for single-worker sequential workloads.
   - Go allocator span retention and SQLite page-cache warm-up are explicitly labeled as hypotheses, and the 24h post-fix soak is identified as the decision boundary.

---

## 3. Two-Axis Review

### Standards Axis
- **Persistence & Resource Invariants ([`ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L23-L66) E1, E4, HARDQ B7)**: Maintained. Single writer (`j.actor`), WAL mode with `_txlock=immediate`, connection pool capped at 4 (`SetMaxOpenConns(4)` / `SetMaxIdleConns(4)`), and explicit non-default `cache_size(-1600)` capping SQLite page-cache memory to $\approx 6.4\text{ MiB}$.
- **Evidence-Graded Completion & Epistemic Honesty ([`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Full traceability from diagnosis documentation to committed raw logs.
  - Minimal causal diff restored by reverting `.kilo/kilo.jsonc`.
- **Smell Baseline**: Explicit test unrolling in `soak_linux_test.go:526-548` keeps boundary arithmetic directly inspectable without speculative helper indirection (Ponytail rung 7).

### Spec Axis
- **Oracle Sensitivity**: Exact $+16\text{ MiB}$ growth passes strict `>`, while $+16\text{ MiB} + 1\text{ KiB}$ fails `soakVerdict`.
- **Enforceable Declaration**: Non-default `cache_size(-1600)` ensures `TestJournalPoolAndCacheBounded` fails RED if the DSN pragma is dropped.
- **Scope**: Clean diff containing only the memory bounds, soak oracle calibration, diagnosis documentation, and regression/sensitivity tests.

---

## 4. Empirical RED-Capability & Ablation Probes

All probes were executed in a clean export on ext4 disk storage at UID `1000`:

| Probe / Ablation | Target File & Line | Mutation | Expected Behavior | Observed Result | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Ablation 1: Dropped Cache Pragma** | [`journal.go:168`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L168) | Stripped `&_pragma=cache_size(-1600)` | `TestJournalPoolAndCacheBounded` fails (driver default `-2000`) | **FAIL (RED)** at `journal_test.go:728`: `cache_size not the declared non-default -1600: -2000` | **PASS (RED-capable)** |
| **Ablation 2: Dropped Pool Cap** | [`journal.go:177`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L177) | Commented out `db.SetMaxOpenConns(4)` | `TestJournalPoolAndCacheBounded` fails on `MaxOpenConnections=0` | **FAIL (RED)** at `journal_test.go:717`: `MaxOpenConnections=0, want 4` | **PASS (RED-capable)** |
| **Ablation 3: Floor Boundary Loosening** | [`soak_linux_test.go:160`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L160) | Mutated `absFloorKB = 16*1024 + 10` | `TestSoakVerdictSensitivity` fails because `over` (+1KiB) passes | **FAIL (RED)** at `soak_linux_test.go:550`: `floor+1KiB growth not caught: <nil>` | **PASS (RED-capable)** |
| **Full Suite Execution** | Root repository | `CGO_ENABLED=0 go test -count=1 ./...` | All 33 packages pass | **PASS**: 33 packages `ok` (0 failures, 102.4s acceptance) | **PASS** |

---

## 5. Top 3 Weakest Points

1. **Source Comment vs Diagnosis Doc Nuance** ([`internal/kernel/journal/journal.go:160-164`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L160-L164)):
   - *Evidence*: `journal.go:160-164` states that the pool bound is the primary cap, whereas `SOAK-S1-DIAGNOSIS.md:41-47` notes that for single-stream workloads the unbounded pool was not the dominant term.
   - *Assessment*: Bounding the pool is a necessary architectural ceiling preventing unbounded concurrency scaling; line 164 correctly references `docs/SOAK-S1-DIAGNOSIS.md` for full context.
2. **Floor Boundary Test Scope in `soak_linux_test.go:523-551`**:
   - *Evidence*: `edge` and `over` test series exercise the median growth condition (`mL - mF > absFloorKB`). Peak RSS boundary thresholding is tested via large spike rather than an isolated $+16\text{ MiB} + 1\text{ KiB}$ single-sample boundary without median elevation.
   - *Assessment*: Both checks share the exact same `const absFloorKB = 16 * 1024` constant, locking the numerical threshold across both evaluation paths.
3. **Static Floor Window Scaling** ([`internal/acceptance/soak_linux_test.go:15-18`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L15-L18)):
   - *Evidence*: `absFloorKB = 16 * 1024` is static and does not scale with `NEXUS_SOAK_MINUTES`.
   - *Assessment*: Documented and accepted ceiling in `soak_linux_test.go:15-18` and `SOAK-S1-DIAGNOSIS.md:53-55`: slow leaks $<16\text{ MiB}$ per run window are undetected by construction, and the ratchet is a longer run.

---

## 6. Ponytail & Decision Trace

- **Laziness Ladder**: Rung 4 (native DSN pragma configuration + `database/sql` pool limits) + Rung 7 (minimal local bounds logic and explicit boundary test series).
- **Diff Cleanliness**: Foreign editor artifact reverted; diff strictly contains causal code, tests, and documentation.
- **Verification Proof**: All causal claims in `SOAK-S1-DIAGNOSIS.md` match host log artifacts; all ablations turn RED for the expected reasons; all 33 packages pass cleanly.

---

VERDICT: PASS

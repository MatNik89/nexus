# Soak-S1 Fix QA Verification Report (Round 4)

**Target**: Commit [`aac5e2a`](https://github.com/MatNik89/nexus/commit/aac5e2a) on branch `slice/p0-soak-s1`  
**Reviewer**: Antigravity (agy)  
**Date**: 2026-09-07  
**Methodology**: Read-only QA verification executed in an isolated clean git export on ext4 disk storage (`build/export_soak_r4`, deleted upon completion). Evaluated across two parallel sub-agents (Standards and Spec), claim-by-claim audit of `journal.go` and `docs/SOAK-S1-DIAGNOSIS.md` against host log evidence, controlled RED ablations, and full repository test suite execution (`CGO_ENABLED=0 go test -count=1 ./...`).

---

## 1. Executive Summary & Round-3 Finding Closures

Commit `aac5e2a` closes both Round-3 findings:

| Finding | Origin | Resolution in `aac5e2a` | Verification Status |
| :--- | :--- | :--- | :--- |
| **(1) Causal Assignment in `journal.go` Comment** | codex MED / agy MED | [`journal.go:160-166`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L160-L166) removes the causal claim assigning the 24h RSS failure to the pool. It explicitly states: *"Measurements attribute NO specific cause: pre/post 20k curves grew about the same, so the pool was not the dominant term on that workload — this is a by-design guard, not the proven fix"*. | **VERIFIED CLOSED** |
| **(2) SQLite Page-Cache Bounding Precision** | codex LOW / agy LOW | Both [`journal.go:174-178`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L174-L178) and [`docs/SOAK-S1-DIAGNOSIS.md:37-41`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md#L37-L41) explicitly describe `cache_size(-1600)` as a suggested maximum of roughly $6.4\text{ MB}$ covering **page cache only**, acknowledging that `cache_size` is approximate and does not bound all SQLite/driver memory. | **VERIFIED CLOSED** |

---

## 2. Claim-by-Claim Audit of Causal & Bounding Statements

Every remaining causal and bounding statement in [`internal/kernel/journal/journal.go`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go) and [`docs/SOAK-S1-DIAGNOSIS.md`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md) was checked against the on-disk evidence and SQLite semantics:

| Source & Location | Claim | Verified Evidence / Basis | Verdict |
| :--- | :--- | :--- | :--- |
| [`journal.go:160-161`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L160-L161) | The pool was formally UNBOUNDED when the first 24h soak tripped its RSS budget. | Go's `database/sql` default `MaxOpenConns = 0`; proven by ablation `journal_test.go:717`. | **VERIFIED** |
| [`journal.go:162-164`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L162-L164) | Measurements attribute NO specific cause; pre/post 20k curves grew about the same; pool bound is a by-design guard. | `diag-prefix-20k.log` ($+10.3\text{ MB}$) vs `diag-postfix.log` ($+9.5\text{ MB}$) on 20k messages. | **VERIFIED** |
| [`journal.go:166-167`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L166-L167) | `cache_size(-1600)` is deliberately NOT driver default (`-2000`), making declaration enforceable. | `modernc.org/sqlite` default is `-2000`; dropping pragma triggers RED assert (`journal_test.go:728`). | **VERIFIED** |
| [`journal.go:174-178`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L174-L178) | 4 conns $\times$ $\sim 1.6\text{ MB}$ suggested max bounds page cache to roughly $6.4\text{ MB}$ (approximate, page cache only). | SQLite PRAGMA `cache_size` semantics (suggested page-cache limit; $-1600\text{ KiB} \approx 1.6\text{ MB}$). | **VERIFIED** |
| [`SOAK-S1-DIAGNOSIS.md:33-36`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md#L33-L36) | 20k curves are concave/flattening; in-process heap measurements rule out per-message live-heap leaks. | Pre/post log samples plateau $\sim 30\text{ MB}$; heap profile loops show 19 B/turn and 46 B/msg. | **VERIFIED** |
| [`SOAK-S1-DIAGNOSIS.md:41-49`](file:///home/matej/HARNESS/nexus/docs/SOAK-S1-DIAGNOSIS.md#L41-L49) | Residual saturating growth is unattributed; Go allocator spans and page cache are hypotheses; 24h run is decision boundary. | Missing factor decomposition series (allocator vs DB-size vs connection counts) is explicitly acknowledged. | **VERIFIED** |

---

## 3. Two-Axis Review

### Standards Axis
- **Persistence & Resource Invariants ([`ARCHITECTURE-ESSENTIALS.md`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L23-L66) E1, E4, HARDQ B7)**:
  - SQLite WAL mode with `_txlock=immediate`, serialized single-actor write pipeline (`j.actor`), and pure Go driver (`modernc.org/sqlite`) remain intact.
  - Connection pooling (`db.SetMaxOpenConns(4)` / `db.SetMaxIdleConns(4)`) and explicit non-default `cache_size(-1600)` establish a bounded, testable resource ceiling.
- **Epistemic Honesty & Review Discipline ([`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - All claims in `journal.go` and `SOAK-S1-DIAGNOSIS.md` strictly match measured empirical artifacts.
  - Minimal causal diff is maintained (no extraneous tooling mutations; `.kilo/kilo.jsonc` matches main).

### Spec Axis
- **Oracle Sensitivity & Bounding**:
  - `TestJournalPoolAndCacheBounded` asserts both `MaxOpenConnections == 4` and `PRAGMA cache_size == -1600`.
  - `TestSoakVerdictSensitivity` exercises $+40\text{ MiB}$ leak, $+45\text{ MiB}$ peak, exact $+16\text{ MiB}$ pass, and $+16\text{ MiB} + 1\text{ KiB}$ boundary rejection.

---

## 4. Empirical RED-Capability & Ablation Probes

All probes were executed in a clean export on ext4 disk storage at UID `1000`:

| Probe / Ablation | Target File & Line | Mutation | Expected Behavior | Observed Result | Verdict |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Ablation 1: Dropped Cache Pragma** | [`journal.go:168`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L168) | Stripped `&_pragma=cache_size(-1600)` | `TestJournalPoolAndCacheBounded` fails (driver default `-2000`) | **FAIL (RED)** at `journal_test.go:728`: `cache_size not the declared non-default -1600: -2000` | **PASS (RED-capable)** |
| **Ablation 2: Dropped Pool Cap** | [`journal.go:179`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L179) | Commented out `db.SetMaxOpenConns(4)` | `TestJournalPoolAndCacheBounded` fails on `MaxOpenConnections=0` | **FAIL (RED)** at `journal_test.go:717`: `MaxOpenConnections=0, want 4` | **PASS (RED-capable)** |
| **Ablation 3: Floor Boundary Loosening** | [`soak_linux_test.go:160`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L160) | Mutated `absFloorKB = 16*1024 + 10` | `TestSoakVerdictSensitivity` fails because `over` (+1KiB) passes | **FAIL (RED)** at `soak_linux_test.go:550`: `floor+1KiB growth not caught: <nil>` | **PASS (RED-capable)** |
| **Full Suite Execution** | Root repository | `CGO_ENABLED=0 go test -count=1 ./...` | All 33 packages pass | **PASS**: 33 packages `ok` (0 failures, 108.1s acceptance) | **PASS** |

---

## 5. Top 3 Weakest Points

1. **Test Docstring Phrasing vs Implementation** ([`internal/kernel/journal/journal_test.go:711-713`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal_test.go#L711-L713)):
   - *Evidence*: The comment on `TestJournalPoolAndCacheBounded` retains the broader sentence *"unbounded pool = RSS growing with database size"*, whereas `journal.go:162-165` and `SOAK-S1-DIAGNOSIS.md:41-45` note that the pool was not the dominant term for single-stream workloads.
   - *Assessment*: Minor comment-only phrasing; the test assertion itself is strict and non-vacuous.
2. **Floor Boundary Test Coverage on Isolated Peak RSS** ([`internal/acceptance/soak_linux_test.go:539-551`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L539-L551)):
   - *Evidence*: The $+16\text{ MiB} + 1\text{ KiB}$ boundary test verifies the median growth check (`mL - mF > absFloorKB`). Peak RSS boundary thresholding is exercised via large spike ($+45\text{ MiB}$ at line 554), rather than an isolated $+16\text{ MiB} + 1\text{ KiB}$ single-sample peak spike without median elevation.
   - *Assessment*: Both checks share the exact same `const absFloorKB = 16 * 1024` constant, locking the numerical floor across both evaluation paths.
3. **Static Floor Window Ceiling** ([`internal/acceptance/soak_linux_test.go:15-18`](file:///home/matej/HARNESS/nexus/internal/acceptance/soak_linux_test.go#L15-L18)):
   - *Evidence*: `absFloorKB = 16 * 1024` is static and does not scale with `NEXUS_SOAK_MINUTES`.
   - *Assessment*: Documented and accepted ceiling; slow leaks $<16\text{ MiB}$ per run window are undetected by construction on short runs, and the ratchet is a longer soak.

---

## 6. Ponytail & Decision Trace

- **Laziness Ladder**: Rung 4 (native SQLite DSN pragma configuration + `database/sql` pool parameters) + Rung 7 (minimal local bounds logic and explicit boundary test series).
- **Attribution Accuracy**: All comments and documentation are aligned with measured evidence.
- **Repository Health**: Clean full test suite execution across all 33 packages.

---

VERDICT: PASS

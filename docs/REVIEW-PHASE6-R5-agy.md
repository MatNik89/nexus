# REVIEW-PHASE6-R5 — Phase 6 Verification Round 5 (Narrow Single-Diff Review)

**Target Ref:** `slice/p0-phase6` @ HEAD (`e78771f2510b64d4b1a459b9ca7513ffbf91cfce`)  
**Diff Under Review:** `git show e78771f` (test(phase6-r4): shebang detector requires COMPILE-time refusal) on top of `79ba58c`.  
**Reviewer:** `agy` (Antigravity)  
**Methodology:** Full code-read of commit `e78771f`; test suite execution (`CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...`); ablation re-verification in clean `git archive HEAD` export (`/tmp/nexus-p6-r5-probe`). Zero repository working tree modifications during test probes.

---

## 1. Executive Summary & Verification Matrix

Commit `e78771f` updates [`internal/sandbox/sandbox_linux_test.go:162-176`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox_linux_test.go#L162-L176) (`TestBackendShebangRejected`) to strictly require that a shebang script is rejected at `Compile` time, eliminating the previous launch-fallback acceptance path.

The detector is verified strictly causal: tolerating closure resolution failures in `Compile` now causes `TestBackendShebangRejected` to fail **RED**. The full suite passes cleanly with zero warnings or regressions.

| # | Verified Item | Dimension & Mechanism | Source Location | Negative Probe Evidence | Status |
|---|---|---|---|---|---|
| 1 | **Shebang Rejection at Compile Time** | **Strict Compile-Time Gate:** `TestBackendShebangRejected` asserts `b.Compile(...)` returns an error (`err != nil`) matching `"not a native ELF"` / `"closure"` and fails immediately if a policy is returned. | [`internal/sandbox/sandbox_linux_test.go:162-176`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox_linux_test.go#L162-L176) | Ablating `Compile` error check for `probe.ResolveClosureHashes` turns `TestBackendShebangRejected` **RED**: `shebang script compiled into a policy (must be rejected at Compile)`. | **[OK] VERIFIED & CAUSAL** |
| 2 | **No Regressions in Prior Phase 6 Invariants** | **End-to-End Security Chain:** Re-verified trust root verification, runtime closure pinning, duplicate key rejection, secret redaction, S7 cancellation propagation, and `exec_allow` deny-default list. | All 34 packages | All packages pass uncached `go test -count=1 ./...`. Hostile conformance suite 13/13 PASS. | **[OK] VERIFIED & CAUSAL** |

---

## 2. Adversarial Verification Details & Negative Probe

In an isolated clean `git archive HEAD` export (`/tmp/nexus-p6-r5-probe`):

1. **Baseline Positive Control:**
   ```bash
   CGO_ENABLED=0 go test -run TestBackendShebangRejected ./internal/sandbox
   ```
   **Result:** `PASS` (`ok github.com/MatNik89/nexus/internal/sandbox 0.143s`).

2. **Causal Ablation Probe (Compile Tolerating Closure Failure):**
   In [`internal/sandbox/sandbox.go:223-226`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L223-L226), modified `Compile` to ignore errors from `probe.ResolveClosureHashes(spec.Target)` (falling back to a target-only pin without failing closed).
   ```bash
   CGO_ENABLED=0 go test -run TestBackendShebangRejected ./internal/sandbox
   ```
   **Result:** `RED` (Exit 1).
   ```text
   --- FAIL: TestBackendShebangRejected (0.12s)
       sandbox_linux_test.go:171: shebang script compiled into a policy (must be rejected at Compile)
   FAIL
   FAIL	github.com/MatNik89/nexus/internal/sandbox	0.130s
   ```

---

## 3. Top Weakest Points Audit (Mandatory Review Discipline)

1. **Weakest Point 1: Static Binary ELF Toolchain Assumptions**
   - **Location:** [`internal/preflight/probe/probe.go:704-716`](file:///home/matej/HARNESS/nexus/internal/preflight/probe/probe.go#L704-L716)
   - **Analysis:** `ResolveClosureHashes` assumes dynamic libraries are declared in static ELF headers (`DT_NEEDED`, `DT_RPATH`, `DT_RUNPATH`). Non-ELF scripts (shebang) fail closed at `Compile` as expected, while static binaries pin only `/nexus-target`.
   - **Ceiling / Evolution:** Supported P0 tools are standard native ELF binaries.

2. **Weakest Point 2: Canonical Path Traversal Boundary**
   - **Location:** [`internal/sandbox/sandbox.go:143-156`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L143-L156)
   - **Analysis:** `Bwrap.Probe` checks ownership and permissions of the resolved leaf executable (`sys.Uid == 0 && st.Mode().Perm()&0o022 == 0`). It assumes standard system root directories (`/usr/bin`, `/bin`) are root-owned and non-writable by unprivileged users.
   - **Mitigation:** Live content digest verification in `Launch` (`liveHash == b.bwrapHash`) prevents TOCTOU replacement.

3. **Weakest Point 3: Reason Substring Assertion Coupling**
   - **Location:** [`internal/sandbox/sandbox_linux_test.go:173-175`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox_linux_test.go#L173-L175)
   - **Analysis:** `TestBackendShebangRejected` checks `strings.Contains(err.Error(), "not a native ELF") || strings.Contains(err.Error(), "closure")`. If lower-level error phrasing changes, test assertions must remain in sync.
   - **Mitigation:** The error is typed and surfaced through `probe.ResolveClosureHashes`.

---

## 4. Test Suite Execution & Conformance Verification

1. **Lint & Vet Check:**
   ```bash
   CGO_ENABLED=0 go vet ./...
   ```
   **Result:** Exit 0 (zero errors/warnings).

2. **Full Uncached Repository Test Suite:**
   ```bash
   CGO_ENABLED=0 go test -count=1 ./...
   ```
   **Result:** All 34 packages `ok` (zero failures, zero skips).
   ```text
   ok  	github.com/MatNik89/nexus/cmd/nexus	6.454s
   ok  	github.com/MatNik89/nexus/internal/app/daemon	0.500s
   ok  	github.com/MatNik89/nexus/internal/app/repl	0.020s
   ok  	github.com/MatNik89/nexus/internal/approval	0.537s
   ok  	github.com/MatNik89/nexus/internal/buildcheck	4.628s
   ok  	github.com/MatNik89/nexus/internal/channel	0.423s
   ok  	github.com/MatNik89/nexus/internal/channel/telegram	0.467s
   ok  	github.com/MatNik89/nexus/internal/exectool	2.954s
   ok  	github.com/MatNik89/nexus/internal/foundation/atomicwrite	0.046s
   ok  	github.com/MatNik89/nexus/internal/foundation/clockid	0.007s
   ok  	github.com/MatNik89/nexus/internal/foundation/config	0.029s
   ok  	github.com/MatNik89/nexus/internal/foundation/pathx	0.061s
   ok  	github.com/MatNik89/nexus/internal/foundation/procx	2.461s
   ok  	github.com/MatNik89/nexus/internal/kernel/assembler	0.026s
   ok  	github.com/MatNik89/nexus/internal/kernel/budget	0.017s
   ok  	github.com/MatNik89/nexus/internal/kernel/checker	0.023s
   ok  	github.com/MatNik89/nexus/internal/kernel/closure	0.030s
   ok  	github.com/MatNik89/nexus/internal/kernel/contracts	0.034s
   ok  	github.com/MatNik89/nexus/internal/kernel/effectpath	0.066s
   ok  	github.com/MatNik89/nexus/internal/kernel/journal	1.544s
   ok  	github.com/MatNik89/nexus/internal/kernel/loop	0.144s
   ok  	github.com/MatNik89/nexus/internal/kernel/machine	0.008s
   ok  	github.com/MatNik89/nexus/internal/kernel/s7min	0.026s
   ok  	github.com/MatNik89/nexus/internal/llm/planner	0.041s
   ok  	github.com/MatNik89/nexus/internal/llm/provider	0.133s
   ok  	github.com/MatNik89/nexus/internal/memory	1.306s
   ok  	github.com/MatNik89/nexus/internal/obligation	0.688s
   ok  	github.com/MatNik89/nexus/internal/preflight/doctor	0.014s
   ok  	github.com/MatNik89/nexus/internal/preflight/probe	44.418s
   ok  	github.com/MatNik89/nexus/internal/sandbox	17.276s
   ok  	github.com/MatNik89/nexus/internal/schedule	0.894s
   ok  	github.com/MatNik89/nexus/internal/security/redact	0.037s
   ```

3. **T25 Hostile Conformance Suite on Host (bwrap 0.11.2):**
   13/13 real backend containment tests passed (`TestCompileRequiresLiveProbe`, `TestLaunchRequiresCompiledPolicy`, `TestCompileRejectsRelativeTarget`, `TestStaleProbePolicyRefused`, `TestBackendShadowClassDenied`, `TestBackendEgressDenied`, `TestBackendDynamicELFRuns`, `TestBackendShebangRejected`, `TestBackendWorkdirWritable`, `TestBackendTimeoutKillsTree`, `TestAttestationMismatchUntrusted`, `TestOutputBounded`, `TestLaunchRefusesSwappedClosureMember`).

---

VERDICT: PASS

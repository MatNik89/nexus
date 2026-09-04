# REVIEW-PHASE6-R3 — Phase 6 Verification Round 3 (Fold Verification)

**Target Ref:** `slice/p0-phase6` @ HEAD (`23db432513fc0a4382031a30bfc8a8af1c80c463`)  
**Diff Under Review:** `d222082..23db432` (fold of Codex Round 2 findings #1-3)  
**Reviewer:** `agy` (Antigravity)  
**Methodology:** Full code-read of diff `d222082..23db432`; test suite execution (`CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./...`); clean `git archive HEAD` export probes in `/tmp/nexus-p6-r3-probe`. Zero repository working tree modifications during test probes.

---

## 1. Executive Summary & Verification Matrix

The fold in commit `23db432` attempted to address the three findings from Codex Round 2 (`codex #1` backend trust root, `codex #2` dynamic closure pinning, and `codex #3` duplicate arg key detector causality). 

However, **the fold commit is incomplete and stranded:** while test assertions and the closure helper (`internal/sandbox/sandbox_linux_test.go` and `internal/preflight/probe/probe.go`) were committed, the core implementation changes to `internal/sandbox/sandbox.go` were **omitted from the commit**. 

Consequently, `TestFakeBwrapRejected` in `internal/sandbox` fails at HEAD, full closure pinning is uncalled, and `CGO_ENABLED=0 go test ./...` exits with code 1.

| # | Item | Status | Source Location | Evidence |
|---|---|---|---|---|
| 1 | **codex #1 (Backend Trust Root)** | **[UNFOLDED] STRANDED** | [`internal/sandbox/sandbox.go:130-166`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L130-L166), [`internal/sandbox/sandbox_linux_test.go:265-282`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox_linux_test.go#L265-L282) | `sandbox.go` was not updated to enforce root-owned canonical path checks. `TestFakeBwrapRejected` fails at HEAD: `fake rejected for the wrong reason (trust root not causal): SANDBOX_CAPABILITY_UNAVAILABLE: the backend did NOT confine`. |
| 2 | **codex #2 (Dynamic Closure Pinning)** | **[UNFOLDED] STRANDED** | [`internal/preflight/probe/probe.go:704-716`](file:///home/matej/HARNESS/nexus/internal/preflight/probe/probe.go#L704-L716), [`internal/sandbox/sandbox.go:189-204`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L189-L204), [`262-267`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L262-L267) | `probe.ResolveClosureHashes` was added in `probe.go` but is never called. `Compile` still hashes only `spec.Target` and `Launch` verifies only `/nexus-target`. |
| 3 | **codex #3 (Detector Causality for Duplicate Keys)** | **[OK] FIXED & CAUSAL** | [`internal/exectool/exectool_linux_test.go:241-255`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool_linux_test.go#L241-L255) | `TestDuplicateArgKeysRejected` now uses allowlisted `/bin/ls` as the last-wins value and asserts the `"duplicate"` error message. Passes and is causal. |

---

## 2. Detailed Findings & Root Cause Analysis

### 1. [UNFOLDED] Backend Trust Root Check Missing from `internal/sandbox/sandbox.go`
- **Claim:** Commit `23db432` states that `Probe` now requires the backend binary to reside on a root-owned, non-user-writable canonical path (`EvalSymlinks` + `uid == 0` + `perm & 0o022 == 0`) before executing the behavioral canary.
- **Reality:** `internal/sandbox/sandbox.go:130-166` was not modified in commit `23db432`. `Bwrap.Probe` still directly invokes `probe.Detect()`, hashes `av.BwrapPath`, and runs the canary without any canonical path or ownership/permission verification.
- **Failure at HEAD:** Running `CGO_ENABLED=0 go test -run TestFakeBwrapRejected ./internal/sandbox` fails immediately:
  ```text
  --- FAIL: TestFakeBwrapRejected (0.07s)
      sandbox_linux_test.go:280: fake rejected for the wrong reason (trust root not causal): SANDBOX_CAPABILITY_UNAVAILABLE: the backend did NOT confine (closure-external directory visible) — refusing to treat it as a sandbox
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/sandbox	0.072s
  ```

### 2. [UNFOLDED] Full Runtime Closure Pinning Stranded in `probe.go`
- **Claim:** Commit `23db432` states that `Compile` now resolves and pins the full runtime closure (`probe.ResolveClosureHashes` — target, loader, every library; `closureDigest` in `policyHash`) and `Launch` verifies every memfd-pinned member.
- **Reality:** `probe.ResolveClosureHashes` was added at [`internal/preflight/probe/probe.go:704-716`](file:///home/matej/HARNESS/nexus/internal/preflight/probe/probe.go#L704-L716), but `internal/sandbox/sandbox.go` was not modified:
  - In [`internal/sandbox/sandbox.go:193-203`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L193-L203), `Compile` still only calls `hashFile(spec.Target)`.
  - `CompiledPolicy` does not contain `closureDigest` or closure maps.
  - In [`internal/sandbox/sandbox.go:264-267`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L264-L267), `Launch` still only checks `h.ClosureHashes()["/nexus-target"] == policy.targetHash`.
  - Dynamic library substitution between `Compile` and `Launch` remains unverified by the backend.

### 3. [OK] Codex #3: Duplicate Arg Key Test Causality
- **Implementation:** [`internal/exectool/exectool_linux_test.go:241-255`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool_linux_test.go#L241-L255) was updated so that the last-wins value in the test payload is `/bin/ls` (`{"command":"/bin/echo","command":"/bin/ls","args":["/"]}`). Because `/bin/ls` is allowlisted, removing only the duplicate key check causes execution, making the test strictly causal for the adapter guard.
- **Evidence:** `CGO_ENABLED=0 go test ./internal/exectool` passes.

---

## 3. Top Weakest Points Audit (Mandatory Review Discipline)

1. **Weakest Point 1: Stranded Implementation File (`internal/sandbox/sandbox.go`)**
   - **Location:** [`internal/sandbox/sandbox.go:130-204`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L130-L204)
   - **Analysis:** The commit modified tests in `sandbox_linux_test.go` and added helper functions in `probe.go`, but omitted the actual `sandbox.go` changes. This breaks CI across the package.
   - **Remediation:** Wire root ownership validation into `Bwrap.Probe` and full closure resolution into `Bwrap.Compile`/`Launch`.

2. **Weakest Point 2: Unused Helper Function in Public Seam**
   - **Location:** [`internal/preflight/probe/probe.go:704-716`](file:///home/matej/HARNESS/nexus/internal/preflight/probe/probe.go#L704-L716)
   - **Analysis:** `ResolveClosureHashes` is declared and exported, but has zero call sites in the repository.
   - **Remediation:** Call `probe.ResolveClosureHashes` in `sandbox.Compile` to derive `closureDigest` and pin all dependencies into `CompiledPolicy`.

3. **Weakest Point 3: Shebang Targets Accepted at `Compile` Time**
   - **Location:** [`internal/sandbox/sandbox.go:173-203`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L173-L203)
   - **Analysis:** Because `Compile` only computes `hashFile(spec.Target)` (which reads any regular file), a script with a `#!/bin/sh` shebang succeeds at `Compile` and is only rejected later at `Launch` when `probe.Prepare` parses ELF headers.
   - **Remediation:** Resolving the full ELF closure during `Compile` rejects non-ELF/shebang targets at `Compile` time.

---

## 4. Test Suite Execution & Gate Verification

1. **Lint & Vet Check:**
   ```bash
   CGO_ENABLED=0 go vet ./...
   ```
   **Result:** Exit 0 (zero errors/warnings).

2. **Full Repository Test Suite:**
   ```bash
   CGO_ENABLED=0 go test ./...
   ```
   **Result:** Exit 1 (FAIL).
   ```text
   ok  	github.com/MatNik89/nexus/cmd/nexus	6.953s
   ok  	github.com/MatNik89/nexus/internal/buildcheck	6.605s
   ok  	github.com/MatNik89/nexus/internal/exectool	1.622s
   ok  	github.com/MatNik89/nexus/internal/preflight/probe	37.694s
   --- FAIL: TestFakeBwrapRejected (0.04s)
       sandbox_linux_test.go:280: fake rejected for the wrong reason (trust root not causal): SANDBOX_CAPABILITY_UNAVAILABLE: the backend did NOT confine (closure-external directory visible) — refusing to treat it as a sandbox
   FAIL
   FAIL	github.com/MatNik89/nexus/internal/sandbox	15.172s
   FAIL
   ```

---

VERDICT: FAIL

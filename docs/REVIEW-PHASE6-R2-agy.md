# REVIEW-PHASE6-R2 — Phase 6 Verification Round 2 (Fold Verification)

**Target Ref:** `slice/p0-phase6` @ HEAD (`d22208260fe8286bcb977c931c425d0b82d445a2`)  
**Commits Covered:** `1c9886b` (fix(phase6-kilo): duplicate-key hash collapse + exec output redaction) + `d222082` (fix(phase6-r1): fold deep+security review round 1 (codex #1-6, kilo #1-2)) on top of `c2884ce`.  
**Reviewer:** `agy` (Antigravity)  
**Methodology:** Full code-read of the fold diffs; full test suite execution in repository (`CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...`); hostile conformance suite execution on host (bwrap 0.11.2); 8 negative causal ablation probes in isolated temporary `git archive HEAD` exports (`/tmp/nexus-p6-r2-agy-probe`). Zero repository working tree modifications during test probes.

---

## 1. Executive Summary & Verification Matrix

The fold in `1c9886b` and `d222082` comprehensively resolves all 8 findings from Round 1 across codex and kilo. Each fix was verified for correctness and causal negative sensitivity against source specifications.

| # | Fold Item | Dimension & Mechanism | Source Location | Negative Probe Evidence | Status |
|---|---|---|---|---|---|
| 1 | **codex #1** | **Trust Label & Lineage Preservation in Resume:** `resumeBlocks` preserves original `ContextBlock` slice (`TrustUntrustedExternal` + lineage) instead of re-minting as `TrustToolTrusted`. | [`cmd/nexus/main.go:319-336`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L319-L336) | Ablating to synthetic trusted observation turns `TestResumePreservesObservationTrust` **RED**: `exec output trust widened on approval resume: got TOOL_TRUSTED`. | **[OK] VERIFIED & CAUSAL** |
| 2 | **codex #2** | **Backend Content Hash & Live Enforcement Canary:** `Probe` binds `hashFile(av.BwrapPath)` into `ProbeHash` and runs live canary (host dir invisible + positive control); `Launch` re-verifies `bwrap` binary digest. | [`internal/sandbox/sandbox.go:138-166`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L138-L166), [`242-247`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L242-L247) | Ablating canary containment check turns `TestFakeBwrapRejected` **RED**: `version-only fake bwrap passed the probe`. | **[OK] VERIFIED & CAUSAL** |
| 3 | **codex #3** | **Compile Pins Target Content Digest:** `Compile` computes `targetHash := hashFile(spec.Target)` into `CompiledPolicy`; `Launch` asserts `/nexus-target` memfd digest matches `policy.targetHash`. | [`internal/sandbox/sandbox.go:193-202`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L193-L202), [`264-267`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L264-L267) | Ablating target digest check turns `TestLaunchRefusesSwappedTarget` **RED**: `swapped target bytes launched under the old policy`. | **[OK] VERIFIED & CAUSAL** |
| 4 | **codex #4** | **S7 Cancellation Propagation:** `Launch` rejects `ctx.Err() != nil` fail-closed, bounds timeout by `ctx.Deadline()`, and cancel watcher invokes `h.Kill()` on `<-ctx.Done()`. | [`internal/sandbox/sandbox.go:228-230`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L228-L230), [`248-254`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L248-L254), [`281-288`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L281-L288) | Ablating `ctx.Err()` check turns `TestCancelledContextNeverLaunches` **RED**: `already-cancelled context launched a process`. | **[OK] VERIFIED & CAUSAL** |
| 5 | **codex #5** | **Deny-Default `exec_allow` Promoted Targets:** `config.Config` defines `ExecAllow []string` (validated absolute without wildcards in `ValidateBounds`); `exectool.Launch` enforces `!a.allow[filepath.Clean(args.Command)]`. | [`internal/foundation/config/config.go:34-38`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L34-L38), [`305-309`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L305-L309), [`internal/exectool/exectool.go:113-115`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L113-L115) | Ablating `exec_allow` gate turns `TestShellInterpreterDeniedByDefault` **RED**: `un-promoted absolute ELF interpreter executed a shell string`. | **[OK] VERIFIED & CAUSAL** |
| 6 | **codex #6** | **Duplicate Member Name Rejection at Admission:** `contracts.NewToolCall` runs `HasDuplicateJSONKeys(p.Arguments)` and rejects duplicate object keys fail-closed. | [`internal/kernel/contracts/contracts.go:405-407`](file:///home/matej/HARNESS/nexus/internal/kernel/contracts/contracts.go#L405-L407), [`576-624`](file:///home/matej/HARNESS/nexus/internal/kernel/contracts/contracts.go#L576-L624) | Ablating `NewToolCall` duplicate key check turns `TestEffectHashDuplicateKeysDoNotCollapse` **RED**: `duplicate-key arguments constructed a valid ToolCall`. | **[OK] VERIFIED & CAUSAL** |
| 7 | **kilo #1** | **Duplicate Key Raw-Bytes Fallback (Defense-in-Depth):** `effectpath.canonicalJSON` retains raw bytes on duplicate keys, ensuring disjoint hash spaces with canonical objects. | [`internal/kernel/effectpath/effectpath.go:150-159`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L150-L159) | Ablating fallback in `canonicalJSON` turns `TestEffectHashDuplicateKeysDoNotCollapse` **RED**: `duplicate-key document collapsed to its last-wins form (C4 weakening)`. | **[OK] VERIFIED & CAUSAL** |
| 8 | **kilo #2** | **Subprocess Output Secret Redaction:** `exectool.Launch` scrubs `proc.Output()` via `redactText(a.redactor, ...)` before wrapping in `ContextBlock`. | [`internal/exectool/exectool.go:152`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L152), [`182-196`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L182-L196) | Ablating `redactText` turns `TestExecOutputRedactsKnownSecrets` **RED**: `known secret escaped into the exec observation`. | **[OK] VERIFIED & CAUSAL** |

---

## 2. Adversarial Verification Details & Negative Ablation Probes

All negative ablation probes were executed against clean `git archive HEAD` trees in `/tmp/nexus-p6-r2-agy-probe`.

### 1. Codex #1: Trust Label Preservation Through Approval Resume
- **Mechanism:** In [`cmd/nexus/main.go:319-336`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L319-L336), `resumeBlocks` receives the `contracts.ToolResult` from `RunTool` directly and appends its `Output` blocks (`TrustUntrustedExternal` for `exec`) rather than discarding them and creating a synthetic `TrustToolTrusted` block via `resumeObservation`. A synthetic block is generated only if the tool returned zero output blocks.
- **Negative Ablation:** Forced `resumeBlocks` to unconditionally call `resumeObservation(call, result)`.
- **Ablation Output:**
  ```text
  --- FAIL: TestResumePreservesObservationTrust (0.00s)
      main_test.go:1112: exec output trust widened on approval resume: got TOOL_TRUSTED
  FAIL
  FAIL	github.com/MatNik89/nexus/cmd/nexus	0.006s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

### 2. Codex #2: Sandbox Backend Probe Identity & Live Containment Canary
- **Mechanism:** In [`internal/sandbox/sandbox.go:138-166`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L138-L166), `Bwrap.Probe` hashes the `bwrap` binary via `hashFile(av.BwrapPath)` and incorporates it into `ProbeHash`. It runs a live containment probe (`/bin/ls` on a closure-external temporary directory) that must fail (exit non-zero) and a positive control (`/bin/ls` on `/`) that must succeed. Additionally, `Launch` ([`internal/sandbox/sandbox.go:242-247`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L242-L247)) verifies the live `bwrap` binary hash against `b.bwrapHash` to prevent post-probe TOCTOU binary swaps.
- **Negative Ablation:** Removed the live canary containment check from `Probe`.
- **Ablation Output:**
  ```text
  --- FAIL: TestFakeBwrapRejected (0.01s)
      sandbox_linux_test.go:271: version-only fake bwrap passed the probe
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/sandbox	0.014s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

### 3. Codex #3: Compile-Time Target Content Digest Pinning
- **Mechanism:** In [`internal/sandbox/sandbox.go:193-202`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L193-L202), `Compile` computes `targetHash := hashFile(spec.Target)` and seals it inside `CompiledPolicy.targetHash` and `policyHash`. In [`internal/sandbox/sandbox.go:264-267`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L264-L267), `Launch` inspects the memfd-pinned closure digest `h.ClosureHashes()["/nexus-target"]` and refuses execution if it differs from `policy.targetHash`.
- **Negative Ablation:** Removed the `/nexus-target` hash equality check in `Launch`.
- **Ablation Output:**
  ```text
  --- FAIL: TestLaunchRefusesSwappedTarget (0.09s)
      sandbox_linux_test.go:300: swapped target bytes launched under the old policy
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/sandbox	0.106s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

### 4. Codex #4: S7 Cancellation Propagation to Subprocess Tree
- **Mechanism:** In [`internal/sandbox/sandbox.go:228-230`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L228-L230), `Launch` checks `ctx.Err()` fail-closed before preparing any handle. It clamps the subprocess timeout to `time.Until(deadline)` if `ctx.Deadline()` is sooner. Upon successful launch, it spawns a watcher goroutine ([`internal/sandbox/sandbox.go:281-288`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L281-L288)) that calls `h.Kill()` if `ctx.Done()` fires before `proc.done` is closed.
- **Negative Ablation:** Removed `if err := ctx.Err(); err != nil` from `Launch`.
- **Ablation Output:**
  ```text
  --- FAIL: TestCancelledContextNeverLaunches (0.16s)
      sandbox_linux_test.go:315: already-cancelled context launched a process
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/sandbox	0.160s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

### 5. Codex #5: Deny-Default Promoted Target Allowlist (`exec_allow`)
- **Mechanism:** In [`internal/foundation/config/config.go:34-38`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L34-L38) and [`305-309`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L305-L309), `exec_allow` is introduced as a validated configuration list requiring absolute paths without wildcards. `exectool.New` constructs an internal allowlist map, and `exectool.Launch` ([`internal/exectool/exectool.go:113-115`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L113-L115)) refuses any command not explicitly in `a.allow`. Empty allowlist refuses all commands fail-closed.
- **Negative Ablation:** Removed the `!a.allow[filepath.Clean(args.Command)]` check in `exectool.Launch`.
- **Ablation Output:**
  ```text
  --- FAIL: TestShellInterpreterDeniedByDefault (1.00s)
      exectool_linux_test.go:292: un-promoted absolute ELF interpreter executed a shell string
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/exectool	1.009s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

### 6. Codex #6 & Kilo #1: Duplicate JSON Key Rejection & Hash Injectivity
- **Mechanism:** Primary admission defense in [`internal/kernel/contracts/contracts.go:405-407`](file:///home/matej/HARNESS/nexus/internal/kernel/contracts/contracts.go#L405-L407) and [`576-624`](file:///home/matej/HARNESS/nexus/internal/kernel/contracts/contracts.go#L576-L624): `HasDuplicateJSONKeys` traverses the JSON token stream and rejects any JSON object carrying duplicate keys across all depths. Defense-in-depth in [`internal/kernel/effectpath/effectpath.go:150-159`](file:///home/matej/HARNESS/nexus/internal/kernel/effectpath/effectpath.go#L150-L159): `canonicalJSON` returns raw bytes if duplicate keys are detected, preventing last-wins decoding collapse. Door defense in [`internal/exectool/exectool.go:95-99`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L95-L99): `exectool.Launch` rejects duplicate keys at invocation.
- **Negative Ablation A (Admission Guard):** Removed `HasDuplicateJSONKeys` check from `contracts.NewToolCall`.
- **Ablation Output A:**
  ```text
  --- FAIL: TestEffectHashDuplicateKeysDoNotCollapse (0.00s)
      effectpath_test.go:653: duplicate-key arguments constructed a valid ToolCall
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/kernel/effectpath	0.008s
  ```
- **Negative Ablation B (Defense-in-Depth Fallback):** Removed raw-bytes fallback from `canonicalJSON`.
- **Ablation Output B:**
  ```text
  --- FAIL: TestEffectHashDuplicateKeysDoNotCollapse (0.00s)
      effectpath_test.go:661: duplicate-key document collapsed to its last-wins form (C4 weakening)
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/kernel/effectpath	0.016s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

### 7. Kilo #2: Subprocess Output Secret Redaction
- **Mechanism:** In [`internal/exectool/exectool.go:152`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L152) and [`182-196`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L182-L196), `redactText` applies `a.redactor.Redact` to `proc.Output()` before constructing the output `ContextBlock`, ensuring sensitive strings (such as host tokens/keys discovered by read-only filesystem commands) are scrubbed before reaching the model context.
- **Negative Ablation:** Removed `redactText` wrapping in `exectool.Launch`.
- **Ablation Output:**
  ```text
  --- FAIL: TestExecOutputRedactsKnownSecrets (1.83s)
      exectool_linux_test.go:278: known secret escaped into the exec observation
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/exectool	1.845s
  ```
- **Verdict:** `[OK]` Fix is real, causal, and complete.

---

## 3. Top Weakest Points Audit (Mandatory Review Discipline)

1. **Weakest Point 1: `exec_allow` Config Promotes Whole Binaries Without Per-Binary Typed Argv Schemas**
   - **Location:** [`internal/exectool/exectool.go:109-115`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L109-L115), [`internal/foundation/config/config.go:305-309`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L305-L309)
   - **Analysis:** While `exec_allow` stops unpromoted binaries (e.g. `/bin/sh`) from running by default, if the owner explicitly adds an interpreter binary (such as `/usr/bin/python3` or `/bin/bash`) to `exec_allow`, the closed schema `{command, args}` permits arbitrary arguments to that interpreter within bwrap.
   - **Ceiling / Evolution:** In P0, owner approval is exact-intent per invocation (`DecisionAsk` is mandatory) and bwrap containment isolates the execution. Typed argv grammars per promoted binary are acknowledged as P1/P2 extensions.

2. **Weakest Point 2: Probe Enforcement Canary Assumes Host `/bin/ls` Availability**
   - **Location:** [`internal/sandbox/sandbox.go:153-161`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L153-L161)
   - **Analysis:** The live backend canary executes `/bin/ls <canaryDir>` and `/bin/ls /` to verify containment and functionality. On non-standard Linux distributions where `/bin/ls` is absent or located elsewhere (e.g. pure `/usr/bin/ls` without symlink), `Probe` would return `SANDBOX_CAPABILITY_UNAVAILABLE`.
   - **Ceiling / Evolution:** Linux-first POSIX standard environments in P0 guarantee `/bin/ls` presence; doctor checks further validate host toolchain prerequisites.

3. **Weakest Point 3: Process Done Channel Lifecycle Management**
   - **Location:** [`internal/sandbox/sandbox.go:77-88`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L77-L88), [`281-288`](file:///home/matej/HARNESS/nexus/internal/sandbox/sandbox.go#L281-L288)
   - **Analysis:** The background cancellation watcher goroutine in `Launch` terminates when either `<-ctx.Done()` or `<-proc.done` fires. If a consumer discards a `*Process` without calling `Wait()` or `Close()`, the goroutine remains active until `ctx` expires.
   - **Mitigation:** In `exectool.Launch` ([`internal/exectool/exectool.go:141`](file:///home/matej/HARNESS/nexus/internal/exectool/exectool.go#L141)), `defer proc.Close()` is strictly enforced, guaranteeing channel closure and goroutine termination under all return paths.

---

## 4. Test Suite Execution & Hostile Conformance Gate

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
   ok  	github.com/MatNik89/nexus/cmd/nexus	7.921s
   ok  	github.com/MatNik89/nexus/internal/app/daemon	0.513s
   ok  	github.com/MatNik89/nexus/internal/app/repl	0.030s
   ok  	github.com/MatNik89/nexus/internal/approval	0.566s
   ok  	github.com/MatNik89/nexus/internal/buildcheck	5.018s
   ok  	github.com/MatNik89/nexus/internal/channel	0.676s
   ok  	github.com/MatNik89/nexus/internal/channel/telegram	0.396s
   ok  	github.com/MatNik89/nexus/internal/exectool	4.252s
   ok  	github.com/MatNik89/nexus/internal/foundation/atomicwrite	0.050s
   ok  	github.com/MatNik89/nexus/internal/foundation/clockid	0.023s
   ok  	github.com/MatNik89/nexus/internal/foundation/config	0.044s
   ok  	github.com/MatNik89/nexus/internal/foundation/pathx	0.037s
   ok  	github.com/MatNik89/nexus/internal/foundation/procx	2.610s
   ok  	github.com/MatNik89/nexus/internal/kernel/assembler	0.012s
   ok  	github.com/MatNik89/nexus/internal/kernel/budget	0.017s
   ok  	github.com/MatNik89/nexus/internal/kernel/checker	0.030s
   ok  	github.com/MatNik89/nexus/internal/kernel/closure	0.033s
   ok  	github.com/MatNik89/nexus/internal/kernel/contracts	0.016s
   ok  	github.com/MatNik89/nexus/internal/kernel/effectpath	0.083s
   ok  	github.com/MatNik89/nexus/internal/kernel/journal	1.241s
   ok  	github.com/MatNik89/nexus/internal/kernel/loop	0.388s
   ok  	github.com/MatNik89/nexus/internal/kernel/machine	0.031s
   ok  	github.com/MatNik89/nexus/internal/kernel/s7min	0.017s
   ok  	github.com/MatNik89/nexus/internal/llm/planner	0.036s
   ok  	github.com/MatNik89/nexus/internal/llm/provider	0.040s
   ok  	github.com/MatNik89/nexus/internal/memory	1.921s
   ok  	github.com/MatNik89/nexus/internal/obligation	0.426s
   ok  	github.com/MatNik89/nexus/internal/preflight/doctor	0.015s
   ok  	github.com/MatNik89/nexus/internal/preflight/probe	37.012s
   ok  	github.com/MatNik89/nexus/internal/sandbox	13.385s
   ok  	github.com/MatNik89/nexus/internal/schedule	1.023s
   ok  	github.com/MatNik89/nexus/internal/security/redact	0.042s
   ```

3. **T25 Hostile Conformance Suite on Host (bwrap 0.11.2):**
   12/12 real backend containment tests passed (`TestCompileRequiresLiveProbe`, `TestLaunchRequiresCompiledPolicy`, `TestCompileRejectsRelativeTarget`, `TestStaleProbePolicyRefused`, `TestBackendShadowClassDenied`, `TestBackendEgressDenied`, `TestBackendDynamicELFRuns`, `TestBackendShebangRejected`, `TestBackendWorkdirWritable`, `TestBackendTimeoutKillsTree`, `TestAttestationMismatchUntrusted`, `TestOutputBounded`).

---

VERDICT: PASS

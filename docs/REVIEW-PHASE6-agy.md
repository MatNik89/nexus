# REVIEW-PHASE6 — Phase 6 Deep Review & Security Integration Review

**Target Ref:** `slice/p0-phase6` @ HEAD (`c2884ce1186d32664ecd8c254f957560a6b76ff7`)  
**Commits Covered:** `446a12b` (T25: S6.2-P0 SandboxBackend) + `c2884ce` (T26: exec tool on the effect-path)  
**Reviewer:** `agy` (Antigravity)  
**Scope A (Phase 6 Core):** `internal/sandbox/sandbox.go`, `internal/sandbox/sandbox_linux_test.go`, `internal/exectool/exectool.go`, `internal/exectool/exectool_linux_test.go`, `internal/kernel/effectpath/effectpath.go`, `cmd/nexus/main.go`, `cmd/nexus/main_test.go`.  
**Scope B (Security Integration):** Full end-to-end security chain across `sandbox`, `exectool`, `daemon`, `approval`, `telegram`, `profile`, and `yolo`.

---

## 1. Executive Summary & Verification Matrix

Phase 6 implements the process-execution membrane for NEXUS P0:
1. **T25 `internal/sandbox`:** The S6.2-P0 `Backend` interface (`Probe`, `Compile`, `Launch`, `Attest`) wrapping bubblewrap (`bwrap`) with content-pinned closures (B4), disposable `/work` directory, unshared network, seccomp floor, sealed unforgeable `CompiledPolicy`, stale-probe refusal, and process-to-policy attestation binding.
2. **T26 `internal/exectool`:** The sole process execution door on `effectpath.SandboxBackend`, enforcing a closed args schema (`command`, `args`), absolute ELF target validation, `ExecProcess` kind enforcement, attestation-backed E9 commit receipts, `TrustUntrustedExternal` context fencing, output pruning preserving exit status and error tail (E5/E8), and fail-closed capability gating in the composition root.
3. **C4 Canonical Exact-Intent Digesting:** `effectpath.EffectHash` canonicalizes argument JSON via `canonicalJSON` (deterministic key ordering with `json.Number` format preservation), preventing key-reordering self-invalidation across journal redactor round trips while preserving exact-intent tamper detection.

All 12 hostile conformance suite tests passed through the real backend on the host. End-to-end integration was verified from Telegram channel inbound turns through ASK suspension, human approval, and sandboxed resume execution (`TestExecSpineEndToEndSandboxed`).

| # | Target Dimension | Primary Mechanisms | Negative Probe / Evidence | Status |
|---|---|---|---|---|
| 1 | **T25 Sandbox Backend (`internal/sandbox`)** | `Backend{Probe,Compile,Launch,Attest}`, sealed `CompiledPolicy`, stale probe guard, attestation digest comparison | Mutating policyHash in `Attest` turns `TestAttestationMismatchUntrusted` **RED**; forging `probeHash` turns `TestStaleProbePolicyRefused` **RED** | **[OK] VERIFIED & CAUSAL** |
| 2 | **T25 Hostile Conformance Suite (12/12)** | Net unshare, `/` remount read-only, `/work` RW 0700, dynamic ELF execution, shebang pre-rejection, timeout tree kill | All 12 hostile tests pass on host; canary read fails (`TestBackendShadowClassDenied`); egress fails (`TestBackendEgressDenied`) | **[OK] VERIFIED & CAUSAL** |
| 3 | **T26 Exec Tool Membrane (`internal/exectool`)** | `ExecProcess` kind enforcement, `DisallowUnknownFields`, absolute ELF check, `TrustUntrustedExternal` fence, E9 commit receipt | Removing `DisallowUnknownFields` turns `TestShellStringAndOpenSchemaRejected` **RED**; changing trust fence turns `TestExecRunsSandboxedE2EWithAsk` **RED** | **[OK] VERIFIED & CAUSAL** |
| 4 | **C4 Canonical Argument Hashing (`effectpath`)** | `canonicalJSON` with `json.Decoder.UseNumber()` and sorted key marshal; `EffectHash` length-prefixed | Reordered keys match in `TestApprovalSurvivesKeyReorder`; value, key, type, and array-order tampering produce distinct hashes | **[OK] VERIFIED & CAUSAL** |
| 5 | **Composition Wiring & Spine E2E (`cmd/nexus`)** | Probe gates `execDoor` and planner specs; exec is `DecisionAsk`; daemon passes `execDoor` into `sysPath` and `channelLoop` | `TestExecSpineEndToEndSandboxed` verifies Telegram ASK suspend $\to$ approve $\to$ sandboxed resume execution with exit status | **[OK] VERIFIED & CAUSAL** |

---

## 2. Scope A: Detailed Adversarial Verification & Negative Probes

All negative ablation probes were executed against an isolated `git archive HEAD` export in `/tmp/nexus-p6-probe`, leaving the repository working tree clean.

### 1. T25 Sandbox Protocol & Attestation Mismatch (`internal/sandbox/sandbox.go`)
- **Audit:** `CompiledPolicy` fields are unexported, preventing forged policies. `Launch` enforces `policy.probeHash == digest("probe", b.av.BwrapPath, b.av.BwrapVersion)`. `Attest` computes a SHA-256 digest over the sorted content hashes of all closure files and asserts `p.policyHash == policy.policyHash`.
- **Ablation 1 (Attestation Mismatch):** In `internal/sandbox/sandbox.go:191`, removed `if p.policyHash != policy.policyHash`.
- **Result:** `RED` (Exit 1).
  ```text
  === RUN   TestAttestationMismatchUntrusted
      sandbox_linux_test.go:228: process launched under policy A attested against policy B
  --- FAIL: TestAttestationMismatchUntrusted (0.12s)
  ```
- **Ablation 2 (Stale Probe Refusal):** In `internal/sandbox/sandbox.go:161`, bypassed the probe hash check in `Launch`.
- **Result:** `RED` (Exit 1).
  ```text
  === RUN   TestStaleProbePolicyRefused
      sandbox_linux_test.go:115: policy compiled under a different probe launched
  --- FAIL: TestStaleProbePolicyRefused (0.13s)
  ```

### 2. T26 Exec Tool Fencing & Closed Schema (`internal/exectool/exectool.go`)
- **Audit:** `execArgs` enforces a closed schema via `dec.DisallowUnknownFields()`. Relative binary targets (e.g. `"ls"`) and shell strings (e.g. `"ls | sh"`) are rejected before compilation. Output context blocks are fenced with `Trust: contracts.TrustUntrustedExternal`.
- **Ablation 3 (Untrusted Output Fencing):** In `internal/exectool/exectool.go:138`, changed `TrustUntrustedExternal` to `TrustUser`.
- **Result:** `RED` (Exit 1).
  ```text
  === RUN   TestExecRunsSandboxedE2EWithAsk
      exectool_linux_test.go:150: subprocess output not fenced UNTRUSTED
  --- FAIL: TestExecRunsSandboxedE2EWithAsk (0.08s)
  ```
- **Ablation 4 (Closed Schema Violation):** In `internal/exectool/exectool.go:83`, removed `dec.DisallowUnknownFields()`.
- **Result:** `RED` (Exit 1).
  ```text
  === RUN   TestShellStringAndOpenSchemaRejected
      exectool_linux_test.go:201: unknown args field accepted (closed schema violated)
  --- FAIL: TestShellStringAndOpenSchemaRejected (0.10s)
  ```

### 3. C4 Exact-Intent Argument Canonicalization (`internal/kernel/effectpath/effectpath.go`)
- **Audit:** `canonicalJSON` decodes JSON using `dec.UseNumber()` to prevent float truncation or large integer corruption, marshals into sorted Go maps, and returns normalized byte representations.
- **Adversarial Verification:** Tested 9 boundary conditions in an independent test suite:
  1. *Key Reordering:* `{"command":"/bin/ls","args":["/"]}` and `{"args":["/"],"command":"/bin/ls"}` produce identical canonical bytes.
  2. *Whitespace Variation:* Newlines and indentation normalize to compact JSON.
  3. *Unicode Escapes:* `\u002fbin\u002fls` normalizes to `/bin/ls`.
  4. *Numeric Preservation:* Large 64-bit integers (`9223372036854775807`) and floating point literals (`3.141592653589793`) retain exact string representations without scientific notation or rounding mutations.
  5. *Value Tampering:* Changing `"/bin/ls"` to `"/bin/rm"` produces distinct `EffectHash` digests.
  6. *Key Tampering:* Changing `"command"` to `"cmd"` produces distinct `EffectHash` digests.
  7. *Array Ordering:* `["a","b"]` vs `["b","a"]` produces distinct `EffectHash` digests (arrays preserve strict positional ordering).
  8. *Type Tampering:* `{"v":1}`, `{"v":"1"}`, and `{"v":true}` produce distinct `EffectHash` digests.
  9. *Non-JSON Fallback:* Malformed or non-JSON payloads fall back safely to raw bytes without panic.

---

## 3. Scope B: Full Security Surface Integration Review

We evaluated the complete security chain across all subsystem boundaries:

### 1. Inbound Channel to Exec Execution Path
- **Analysis:** Remote Telegram messages enter via `telegramHandler` $\to$ `RunChannelTurn` $\to$ `channelLoop`. `channelLoop` is hardcoded to `effectpath.ModeDefault` (F2). `exectool.Rules()` mandates `exec: DecisionAsk`.
- **Verification:** An unapproved model tool call for `exec` returns `ErrNeedsApproval`, which suspends the turn (`loop.Suspender` $\to$ `approvals.Suspend`), journals `EvTurnSuspended`, and sends an `APPROVAL NEEDED` challenge to Telegram. No execution occurs without explicit user approval.

### 2. YOLO Mode Boundary Containment
- **Analysis:** YOLO mode (`ModeYolo`) is restricted strictly to local interactive UDS sessions verified via `sameUID(conn)` (SO_PEERCRED). Under `ModeYolo`, `PEP.Evaluate` translates `DecisionAsk` $\to$ `DecisionAllow` (journaling `DecisionAllowedByYolo`).
- **Sandbox Invariant:** Bypassing manual approval confirmation under YOLO mode **does not** bypass the sandbox. The execution path (`RunTool`) routes directly to `path.execProcess.Launch` $\to$ `exectool.Adapter.Launch` $\to$ `sandbox.Bwrap.Launch`. All bwrap isolation constraints (read-only root, net unshare, disposable workdir, seccomp floor, attestation verification) remain 100% active and enforced.

### 3. Approval Resume Sandbox Binding
- **Analysis:** When a user replies `approve <id>`, `resumeApproved` executes the tool via `b.sysPath.RunTool(ctx, call, grant)`.
- **Verification:** `sysPath` is constructed in `buildDaemon` with `effectpath.NewSandboxedProcessExecutor(execDoor)`. `execDoor` is the verified `exectool.Adapter`. If sandbox capability is unavailable at startup, `execDoor` is `nil` and `noSandbox{}` rejects `ExecProcess` calls fail-closed. Approval resume cannot execute an ELF binary outside of bwrap.

### 4. Profile Isolation & Workdir Disposal
- **Analysis:** Every execution invocation allocates a fresh disposable temporary directory: `work, err := os.MkdirTemp("", "nexus-exec-")` with `0700` permissions. Bubblewrap mounts this directory as `/work`.
- **Verification:** The host root `/` is mounted read-only (`--ro-bind / /`), and `/work` is purged via `defer os.RemoveAll(work)` upon tool completion. Processes cannot write to profile databases, journals, or cross-session files.

### 5. Untrusted Output Lineage & Fencing
- **Analysis:** Process standard output and standard error can contain adversarial prompt injection payloads from external sources.
- **Verification:** `exectool.Launch` tags all output context blocks with `Trust: contracts.TrustUntrustedExternal`. Downstream subsystems (e.g. `memory_remember`, `approvals`, `obligation`) enforce trust monotonicity and refuse untrusted blocks as authoritative instructions.

---

## 4. Analysis of Top Weakest Points & Acknowledged Ceilings

In accordance with mandatory review discipline:

### Weakest Point 1: Transient `/work` Directory Lifecycle (Topknot Ceiling)
- **Location:** `internal/exectool/exectool.go:92-96`
- **Analysis:** Each execution runs in a fresh, isolated `os.MkdirTemp` directory that is removed upon tool completion. Inter-command filesystem state persistence (e.g., building code in one step and running tests in a subsequent step within the same workspace) is not supported in P0.
- **Ceiling / Evolution:** Preserved worktrees and shadow-git workspaces are acknowledged as P3 scope (HARNESS-SPEC Annex A). Transient isolation satisfies all P0 requirements.

### Weakest Point 2: Output Pruning Window (4 KiB Context Bound)
- **Location:** `internal/exectool/exectool.go:164-175` (`pruneOutput`)
- **Analysis:** Output exceeding 4096 bytes retains the first 2048 bytes and the last 2048 bytes. For commands emitting voluminous diagnostics, middle chunks are elided.
- **Mitigation:** The exit status and error tail (last lines) are guaranteed to be preserved for model diagnosis (E5/E8), and raw buffer capture is bounded at 1 MiB in `sandbox.go:171`.

### Weakest Point 3: Static Provider/Store Attestations in Startup Snapshot
- **Location:** `cmd/nexus/main.go:593-605` (`sealStartupSnapshot`)
- **Analysis:** While Telegram channel and bwrap sandbox probes are dynamically executed at startup, LLM provider and memory store probes remain static startup attestations.
- **Ceiling / Evolution:** Live upstream health check round trips are deferred to P1.

---

## 5. Test Suite Execution & Gate Verification

- **Lint & Vet Check:**
  `CGO_ENABLED=0 go vet ./...` completed with exit code 0 (zero errors/warnings).
- **Full Uncached Test Suite:**
  `CGO_ENABLED=0 go test -count=1 ./...` ran across all 34 packages in the repository:
  ```text
  ok  	github.com/MatNik89/nexus/cmd/nexus	1.451s
  ok  	github.com/MatNik89/nexus/internal/app/daemon	0.455s
  ok  	github.com/MatNik89/nexus/internal/app/repl	0.004s
  ok  	github.com/MatNik89/nexus/internal/approval	0.364s
  ok  	github.com/MatNik89/nexus/internal/buildcheck	5.209s
  ok  	github.com/MatNik89/nexus/internal/channel	0.524s
  ok  	github.com/MatNik89/nexus/internal/channel/telegram	0.290s
  ok  	github.com/MatNik89/nexus/internal/exectool	2.357s
  ok  	github.com/MatNik89/nexus/internal/foundation/atomicwrite	0.033s
  ok  	github.com/MatNik89/nexus/internal/foundation/clockid	0.017s
  ok  	github.com/MatNik89/nexus/internal/foundation/config	0.047s
  ok  	github.com/MatNik89/nexus/internal/foundation/pathx	0.008s
  ok  	github.com/MatNik89/nexus/internal/foundation/procx	2.511s
  ok  	github.com/MatNik89/nexus/internal/kernel/assembler	0.043s
  ok  	github.com/MatNik89/nexus/internal/kernel/budget	0.011s
  ok  	github.com/MatNik89/nexus/internal/kernel/checker	0.041s
  ok  	github.com/MatNik89/nexus/internal/kernel/closure	0.018s
  ok  	github.com/MatNik89/nexus/internal/kernel/contracts	0.013s
  ok  	github.com/MatNik89/nexus/internal/kernel/effectpath	0.080s
  ok  	github.com/MatNik89/nexus/internal/kernel/journal	1.416s
  ok  	github.com/MatNik89/nexus/internal/kernel/loop	0.143s
  ok  	github.com/MatNik89/nexus/internal/kernel/machine	0.024s
  ok  	github.com/MatNik89/nexus/internal/kernel/s7min	0.007s
  ok  	github.com/MatNik89/nexus/internal/llm/planner	0.010s
  ok  	github.com/MatNik89/nexus/internal/llm/provider	0.110s
  ok  	github.com/MatNik89/nexus/internal/memory	1.406s
  ok  	github.com/MatNik89/nexus/internal/obligation	0.827s
  ok  	github.com/MatNik89/nexus/internal/preflight/doctor	0.055s
  ok  	github.com/MatNik89/nexus/internal/preflight/probe	54.473s
  ok  	github.com/MatNik89/nexus/internal/sandbox	9.772s
  ok  	github.com/MatNik89/nexus/internal/schedule	1.049s
  ok  	github.com/MatNik89/nexus/internal/security/redact	0.055s
  ```
  Result: 100% PASS (exit code 0).

---

VERDICT: PASS

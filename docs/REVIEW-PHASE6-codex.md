# Phase 6 deep review and full security-surface integration review — Codex

Reviewed immutable target: `c2884ce1186d32664ecd8c254f957560a6b76ff7` on `slice/p0-phase6`, covering `446a12b` (T25) and `c2884ce` (T26).

Result: **FAIL**. The committed suite and the requested real-bwrap conformance run are green, but six independent clean-export probes expose reachable gaps. The dominant integration failure is trust laundering: exec output is correctly created as `UNTRUSTED_EXTERNAL`, then approval resume discards that provenance and recreates it as `TOOL_TRUSTED` before the planner sees it.

## Findings

1. **[HIGH] Approval resume launders attacker-controlled exec output into trusted planner context.** `internal/exectool/exectool.go:133-140` correctly emits subprocess output with `TrustUntrustedExternal`. `cmd/nexus/main.go:256-268` reduces that block to a string, and `cmd/nexus/main.go:318-329` recreates the string as `TrustToolTrusted`. The assembler applies its structural injection fence only to `TrustUntrustedExternal` (`internal/kernel/assembler/assembler.go:45-56`), so text produced by the executable crosses the approval boundary as trusted instructions. Concrete scenario: an approved executable prints `IGNORE ALL PRIOR INSTRUCTIONS` and requests another tool or memory mutation; the resumed planner receives it without the untrusted-data fence. Clean-export probe `TestReviewExecOutputTrustDoesNotLaunderOnResume` failed with `exec output trust widened on approval resume: got TOOL_TRUSTED`. Preserve the returned `ContextBlock` and its lineage/trust through `ResumeChannelTurn`; do not flatten and reconstruct it.

2. **[HIGH] Startup accepts and later attests a version-only fake sandbox backend.** `internal/sandbox/sandbox.go:110-119` calls only `probe.Detect`, whose evidence is `LookPath` plus `bwrap --version` (`internal/preflight/probe/probe.go:47-61`). It does not run the already-existing enforcement probe used by doctor (`internal/preflight/doctor/doctor.go:84-102`). Composition enables exec from this report (`cmd/nexus/main.go:422-435`), while `Attest` verifies only parent-maintained hashes (`internal/sandbox/sandbox.go:184-208`), not that confinement occurred. A harmless fake `bwrap` that printed version `0.8.0`, then ran unconfined, was accepted and attested: clean-export `TestReviewRejectsVersionOnlyFakeBwrap` failed with `version-only fake bwrap was accepted and attested as a confined run`. A replaced/malicious PATH backend can therefore inherit daemon secrets and execute outside every claimed sandbox restriction. Probe the real floor through the exact backend identity used for launch, bind that executable identity/content into the report and policy, and revalidate it at launch.

3. **[HIGH] C4 approval and `Compile` bind an executable pathname, not the executable that runs.** `CompiledPolicy` hashes the target path and arguments but not target identity/content (`internal/sandbox/sandbox.go:126-147`); target bytes are first opened and pinned only during `Launch` (`internal/sandbox/sandbox.go:154-181`, `internal/preflight/probe/probe.go:587-668`). Thus a user-approved mutable pathname can be replaced after approval or between `Compile` and `Launch`; the replacement is the content that gets pinned and attested. Clean-export `TestReviewCompilePinsTargetBytes` compiled a copied `/bin/true`, overwrote the same path with `/bin/echo`, and observed the replacement execute and attest: `target bytes changed after Compile but the replacement executed and attested`. This defeats exact-intent even though post-`Prepare` swaps are inert. Resolve and pin the promoted target at compile/approval time, include its stable identity/content digest in the policy and C4 intent, and launch only that sealed object.

4. **[HIGH] S7 cancellation is severed before the process is started.** EffectPath derives and passes the S7-owned attempt context (`internal/kernel/effectpath/effectpath.go:450-459`), and S7 cancellation calls its registered cancel function (`internal/kernel/s7min/s7min.go:205-237`). However, `exectool.Adapter.Launch` never checks the context (`internal/exectool/exectool.go:75-115`), all sandbox backend context parameters are unused, and `probe.Prepare` derives its process timeout from `context.Background()` (`internal/preflight/probe/probe.go:637-668`). Clean-export `TestReviewCancelledContextCannotExecute` canceled the context before dispatch and still observed `CANCELLED_CALL_EXECUTED`; it failed with `already-cancelled execution context still launched the process`. A cancel/deadline race can therefore execute an irreversible command for up to two minutes after S7 says the attempt is canceled. Derive the command context from the supplied attempt context plus the local timeout, reject an already-canceled context, and kill/reap the process tree when that context is canceled.

5. **[MED] The advertised closed exec grammar still accepts shell-string interpreters and any absolute ELF.** The planner specification promises `absolute ELF path, no shell strings` (`internal/exectool/exectool.go:62-69`), and the owner requires promoted ELF targets while rejecting shell strings (`docs/tasks-P0.md:322-330`, `docs/HARDQ-CONSOLIDATED.md:83-88`). Runtime validation checks only that `command` is absolute (`internal/exectool/exectool.go:75-90`); the backend's ELF check consequently accepts `/bin/sh -c ...`. Clean-export `TestReviewShellStringRejected` ran `/bin/sh -c 'echo SHELL_STRING_RAN'` successfully and failed with `absolute ELF interpreter accepted a shell-string program`. The committed `TestShellStringAndOpenSchemaRejected` covers a relative string in `command` and an unknown JSON field, not an absolute ELF interpreter. Enforce a promoted-target registry with typed argv contracts (including interpreter modes), rather than treating every absolute ELF as promoted.

6. **[LOW] Canonical hashing erases duplicate JSON member names instead of rejecting ambiguous input.** `ToolCall` validation uses `json.Valid`, which accepts duplicate object names (`internal/kernel/contracts/contracts.go:405-406`), and `canonicalJSON` decodes into a Go map before hashing (`internal/kernel/effectpath/effectpath.go:145-162`). Consequently `{"a":0,"a":1}` has the same effect hash as `{"a":1}`. Current Go consumers are last-key-wins, so I did not find a present first-key/last-key parser exploit, but this is an unnecessary parser-differential hazard at the exact-intent boundary and the canonicalization commit makes the ambiguity persistent across approval. Clean-export `TestReviewDuplicateArgumentNamesRejected` failed with `ambiguous duplicate JSON member names were accepted`. Reject duplicate member names during ToolCall construction/validation before schema validation and hashing.

## Verified behavior and integration assessment

- The exact 12 T25 backend tests exist and passed through real bwrap on this host: `TestCompileRequiresLiveProbe`, `TestLaunchRequiresCompiledPolicy`, `TestCompileRejectsRelativeTarget`, `TestStaleProbePolicyRefused`, `TestBackendShadowClassDenied`, `TestBackendEgressDenied`, `TestBackendDynamicELFRuns`, `TestBackendShebangRejected`, `TestBackendWorkdirWritable`, `TestBackendTimeoutKillsTree`, `TestAttestationMismatchUntrusted`, and `TestOutputBounded`. Result: 12 PASS, zero skips, 10.471 seconds.
- `TestExecSpineEndToEndSandboxed` passed independently. The normal Telegram path uses `ModeDefault`; `exec` is `DecisionAsk` (`internal/exectool/exectool.go:56-60`); approval resume returns through `sysPath`; and every `ExecProcess` dispatch selects `sbproc` (`internal/kernel/effectpath/effectpath.go:386-426`). I found no separate direct channel-to-exec bypass.
- Yolo remains a PEP-only ASK bypass: `DecisionDeny` is unchanged and the executor switch still selects the sandbox process door (`internal/kernel/effectpath/effectpath.go:386-426`). `TestYoloAllowsAskButNeverDeny`, `TestYoloCannotBeSetByChannelInput`, and `TestYoloRefusedWhenAuditNotDurable` passed. This containment is conditional on a genuine backend; finding 2 breaks that prerequisite globally.
- Each exec call receives a fresh mode-0700 temporary workdir and no profile directory is mounted (`internal/exectool/exectool.go:91-106`, `internal/preflight/probe/probe.go:605-665`). I found no direct cross-profile filesystem path through the real bwrap configuration. Mutable absolute executable paths remain the cross-boundary weakness described in finding 3.
- `TestApprovalSurvivesKeyReorder` passed. The independent canonical edge matrix also passed: key reorder and equivalent Unicode escapes hash identically; changed values hash differently; `1` and `1.0` remain distinct (safe re-approval direction); and excessive nesting is rejected by ToolCall validation. I found no number-rounding, Unicode, or depth-based C4 widening beyond duplicate member names.
- The backend output buffer is bounded and exectool pruning retains status plus tail. Those controls do not repair the trust-label loss during resume.

## Test evidence

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed

12 named T25 real-backend tests
exit 0 — 12 PASS, 0 SKIP

clean git-archive export: canonical edge matrix and TestApprovalSurvivesKeyReorder
exit 0 — PASS

clean git-archive export: TestExecSpineEndToEndSandboxed and focused yolo tests
exit 0 — PASS

clean git-archive export negative probes
TestReviewExecOutputTrustDoesNotLaunderOnResume — RED
TestReviewRejectsVersionOnlyFakeBwrap — RED
TestReviewCompilePinsTargetBytes — RED
TestReviewCancelledContextCannotExecute — RED
TestReviewShellStringRejected — RED
TestReviewDuplicateArgumentNamesRejected — RED
```

All negative probes were added and run only in `/tmp/nexus-phase6-codex.h43V5A`, created from `git archive HEAD`; the working tree was not modified by probes. The only repository write from this review is this requested report. An unrelated untracked `docs/REVIEW-PHASE6-agy.md` was present and was left untouched.

Weakest link: the system treats parent-generated policy/attestation hashes as proof that the selected backend enforced confinement. Until the live enforcement probe and backend identity are part of the same launch trust chain, all downstream sandbox assurances have a single replaceable prerequisite.

VERDICT: FAIL

# Phase 6 verification round 2 — Codex

Reviewed immutable target: `d22208260fe8286bcb977c931c425d0b82d445a2` on `slice/p0-phase6` (`1c9886b` + `d222082`).

Result: **FAIL**. Five of the six Codex fixes are complete and independently causal. The target-byte portion of Codex #3 is causal, but the same exact-intent class still permits a dynamic-library substitution. Codex #2 rejects the committed version-only fake, but its two predictable canary outcomes can be spoofed by an adaptive fake backend that is then accepted and attested. One Kilo detector is also false-green under its claimed single-guard ablation.

## Findings

1. **[HIGH] The live canary does not authenticate the sandbox backend and an adaptive fake still becomes the attested trust anchor.** `Probe` hashes whichever executable `LookPath` selected, then asks that same executable to produce one expected failure and one expected success (`internal/sandbox/sandbox.go:130-166`). There is no independent expected digest, ownership/mode trust check, or pinned executable handle. `Launch` only re-hashes the same pathname (`internal/sandbox/sandbox.go:237-244`), and the final attestation is still derived from parent-maintained policy/closure metadata. A fake script can inspect the visible `nexus-probe-canary-*` argument, exit nonzero for that invocation, exit zero for the positive control and later launches, and print an unconfined marker. Clean-export probe `TestReviewAdaptiveFakeBwrapRejected` failed with `adaptive fake backend spoofed the canary and was attested`. Counterevidence checked: `TestFakeBwrapRejected` is causal when the negative canary is ablated, but its fake always exits zero and therefore proves only rejection of a non-adaptive version stub. This leaves Codex #2 incomplete. Establish a trust root for the selected backend (for example, a fixed root-owned executable with ownership/mode validation and a pinned descriptor/identity) and launch that verified object; a finite, self-reported black-box canary cannot authenticate a malicious implementation.

2. **[HIGH] Compile pins only the main ELF; the dynamic closure can change after approval or Compile and the replacement still executes and attests.** `Compile` hashes only `spec.Target` and places only `targetHash` in `policyHash` (`internal/sandbox/sandbox.go:173-203`). The loader and shared libraries are resolved later by `probe.Prepare`, and `Launch` compares only `ClosureHashes()["/nexus-target"]` with the compile-time hash (`internal/sandbox/sandbox.go:255-267`). Clean-export probe `TestReviewCompilePinsDynamicClosure` built a target with an `$ORIGIN` shared library, compiled the policy, replaced only the library, and observed `SWAPPED_CLOSURE` execute with a successful attestation: `dependency changed after Compile but replacement closure executed and attested`. This can also occur during the much longer suspend-to-approval window because no dependency identity participates in the approved call. Counterevidence checked: `TestLaunchRefusesSwappedTarget` is causal when only the target comparison is ablated and correctly closes direct target replacement. The accepted `exec_allow` pathname ceiling is not being re-flagged; this finding concerns the unbound bytes that form the executable's resolved runtime closure. Resolve and pin the entire loader/library closure at Compile, bind its digest into the policy/exact intent, and launch those same sealed objects.

3. **[LOW] `TestDuplicateArgKeysRejected` is not causal for exectool's duplicate-key guard.** The adapter guard is at `internal/exectool/exectool.go:95-99`, but the test uses duplicate values `/bin/echo` then `/bin/rm` (`internal/exectool/exectool_linux_test.go:241-247`). When I removed only that guard in a clean export, the test still passed because last-wins decoding selected `/bin/rm`, which the independent deny-default allowlist rejected at `internal/exectool/exectool.go:109-115`. This is not a production bypass: `NewToolCall`/`Validate` rejects duplicates first, and the raw-hash branch remains causal. It does invalidate the claim that every added guard was ablation-verified RED. Make both duplicate values an allowed harmless target, or construct an adapter whose allowlist contains the last value, so removing only the duplicate check reaches execution and turns the test RED.

## Per-finding verification

1. **[OK] Codex #1 — trust and lineage preservation is fixed and causal.** `resumeApproved` passes the tool's own output blocks through `resumeBlocks` (`cmd/nexus/main.go:237-269`); `resumeBlocks` copies the original context and appends those blocks unchanged (`cmd/nexus/main.go:319-335`). `TestResumePreservesObservationTrust` passed. Replacing `obsBlocks := out.Output` with an empty slice made it RED with `exec output trust widened on approval resume: got TOOL_TRUSTED`.

2. **[NEW-ERROR] Codex #2 — version-only rejection is causal but the reported class is not completely closed.** Hashing and the negative/positive controls are present at `internal/sandbox/sandbox.go:130-166`, and launch-time re-hashing is present at `internal/sandbox/sandbox.go:237-244`. `TestFakeBwrapRejected` passed; disabling only the negative canary made it RED with `version-only fake bwrap passed the probe`. Finding 1 demonstrates the surviving adaptive fake.

3. **[NEW-ERROR] Codex #3 — direct target swapping is fixed and causal, but closure swapping remains.** Target hashing and launch comparison are present at `internal/sandbox/sandbox.go:189-203` and `internal/sandbox/sandbox.go:262-267`. `TestLaunchRefusesSwappedTarget` passed; disabling only the comparison made it RED with `swapped target bytes launched under the old policy`. Finding 2 demonstrates the unresolved sibling bytes in the same runtime closure.

4. **[OK] Codex #4 — cancellation reaches the sandbox and is causal.** Launch refuses an already-canceled context, caps its timeout by the context deadline, and installs a live cancel watcher that kills the handle (`internal/sandbox/sandbox.go:224-288`). `TestCancelledContextNeverLaunches` passed, including the live-tree kill. Removing only the initial canceled-context refusal made it RED with `already-cancelled context launched a process`.

5. **[OK] Codex #5 — exec is deny-default behind `exec_allow` and the fix is causal.** Configuration declares and validates an absolute, wildcard-free list; adapter construction normalizes it; launch rejects every non-member (`internal/foundation/config/config.go:31-39`, `internal/foundation/config/config.go:303-310`, `internal/exectool/exectool.go:55-66`, `internal/exectool/exectool.go:109-115`). `TestShellInterpreterDeniedByDefault` passed. Disabling only the membership check made it RED with `un-promoted absolute ELF interpreter executed a shell string`. Empty allow still refuses all.

6. **[OK] Codex #6 — duplicate JSON names are rejected at ToolCall construction and the fix is causal.** `NewToolCall` invokes the contracts-owned recursive token walker (`internal/kernel/contracts/contracts.go:394-409`, `internal/kernel/contracts/contracts.go:575-622`). `TestEffectHashDuplicateKeysDoNotCollapse` passed. Disabling only the constructor check made it RED with `duplicate-key arguments constructed a valid ToolCall`.

7. **[OK] Kilo #1 hash injectivity and key-reorder compatibility are causal; the adapter detector has finding 3.** The canonicalizer returns raw bytes for duplicates before map decoding (`internal/kernel/effectpath/effectpath.go:150-170`). Removing only that branch made `TestEffectHashDuplicateKeysDoNotCollapse` RED with `duplicate-key document collapsed to its last-wins form`. `TestApprovalSurvivesKeyReorder` remained GREEN at HEAD. `TestDuplicateArgKeysRejected` is not causal for its own adapter guard, as independently demonstrated in finding 3.

8. **[OK] Kilo #2 — known-ref redaction of exec observations is fixed and causal.** Exectool redacts the captured output before pruning and ContextBlock construction (`internal/exectool/exectool.go:148-165`, `internal/exectool/exectool.go:180-194`). `TestExecOutputRedactsKnownSecrets` passed. Bypassing only `redactText` made it RED with `known secret escaped into the exec observation`.

## Verification evidence

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed

12 named T25 hostile tests through real bwrap
exit 0 — 12 PASS, 0 SKIP, 5.286s

clean git-archive export, all named fold tests
exit 0 — all PASS

clean git-archive one-guard ablations
resume trust, version-only canary, target comparison, canceled-context refusal,
exec_allow membership, ToolCall duplicate rejection, raw duplicate hash branch,
and exec-output redaction — all expected RED

clean git-archive adapter duplicate-key ablation
exit 0 — false-green due to independent exec_allow rejection

clean git-archive fight-the-fix probes
TestReviewAdaptiveFakeBwrapRejected — RED
TestReviewCompilePinsDynamicClosure — RED
```

All probe and ablation edits were confined to fresh `git archive HEAD` exports under `/tmp`. This review modified only the requested Codex report; concurrently appearing untracked Agy/Kilo reports were left untouched.

Topknot assessment: the fixes reuse existing contracts and standard library primitives without new dependencies. The extra `effectpath.HasDuplicateJSONKeys` forwarding seam exists only to expose the contracts helper to exectool and is avoidable, but deleting that one-line alias is not material beside the confirmed security gaps. Weakest link: the sandbox still asks an untrusted candidate backend to self-certify enforcement, then treats a hash of that candidate as authenticity.

Skill update: none — the existing audit rules already required adaptive counterexamples, full exact-intent closure comparison, and single-guard ablation; applying them exposed both incomplete fixes and the false-green detector.

VERDICT: FAIL

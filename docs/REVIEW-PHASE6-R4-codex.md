# Phase 6 Verification Round 4 — Codex

Target: `slice/p0-phase6` at `79ba58c87b7029af2ccf9668ea3b90344860d5f5`.

Scope discipline: production and ablation probes ran in clean `git archive HEAD` exports. The working tree was not modified except for this requested report. Concurrent untracked Kilo/Agy review files were left untouched.

## Finding

1. [LOW] `TestBackendShebangRejected` does not prove rejection occurs at `Compile`.

   Evidence: `internal/sandbox/sandbox_linux_test.go:159` calls `Compile`, but lines 169-173 accept either a compile error or a later launch error. This contradicts the test comment and the fold claim that shebang targets are rejected before a policy exists.

   Failing probe: in a clean export I changed only the `ResolveClosureHashes` error branch in `Compile` to continue with a target-only pin, leaving `Launch` rejection intact. Then:

   ```text
   CGO_ENABLED=0 go test -count=1 ./internal/sandbox -run '^TestBackendShebangRejected$' -v
   --- PASS: TestBackendShebangRejected (0.09s)
   PASS
   ```

   Therefore the committed detector stays GREEN when its claimed compile-time guard is ablated. The production guard itself is currently correct: an independent compile-only clean-export probe (`TestReviewShebangRejectedAtCompile`) passed at HEAD.

   Concrete repair: require `Compile` to return an error and assert the non-ELF refusal cause; do not fall back to testing `Launch` in this detector.

## Fold verification

### 1. Trust root — verified causal

- `internal/sandbox/sandbox.go:133-155` resolves symlinks, stats the canonical backend, requires UID 0 and rejects group/other-writable mode bits before any canary runs.
- `internal/sandbox/sandbox.go:163-184` uses a random unmarked temporary path for the negative control and retains a positive control.
- `internal/sandbox/sandbox_linux_test.go:263-281` requires the trust-root refusal reason.
- Clean-export ablation of only the trust-root condition made `TestFakeBwrapRejected` RED with `fake rejected for the wrong reason (trust root not causal)`.
- On this host `/usr/bin/bwrap` and every component of its canonical path are root-owned and non-user-writable. I found no concrete counterexample in which an unprivileged user can place or replace a root-owned `0755` backend on this host. The adaptive-fake rationale therefore holds for the measured deployment.

### 2. Full runtime-closure pin — verified causal

- `internal/sandbox/sandbox.go:216-235` hashes the target, calls `probe.ResolveClosureHashes`, stores `closurePins`, and incorporates the deterministic closure digest into `policyHash`.
- `internal/sandbox/sandbox.go:301-323` obtains the memfd-prepared closure, verifies the target, exact member count, and every destination hash before launch.
- Clean-export ablation of only the per-member comparison made `TestLaunchRefusesSwappedClosureMember` RED with `closure member with non-compile-time bytes launched`.
- The clean-export black-box `$ORIGIN` probe compiled an ELF against a local shared library, replaced that library after `Compile`, and observed `Launch` refusal: PASS.
- A separate compile-only shebang probe confirmed that current production code rejects a script during `Compile`: PASS. The committed detector gap is finding 1.

### 3. Duplicate argument keys — verified causal

- `internal/exectool/exectool_linux_test.go:244-256` uses an allowlisted last-wins value and requires the duplicate-key refusal reason.
- Clean-export ablation of only the duplicate-key rejection made `TestDuplicateArgKeysRejected` RED with `duplicate-key args executed (last-wins divergence): SUCCEEDED`.

### 4. Prior Phase 6 guards — reconfirmed

The following focused tests all passed in the clean export:

```text
TestResumePreservesObservationTrust
TestCancelledContextNeverLaunches
TestShellInterpreterDeniedByDefault
TestEffectHashDuplicateKeysDoNotCollapse
TestDuplicateArgKeysRejected
TestExecOutputRedactsKnownSecrets
```

This reconfirms observation trust/lineage on resume, cancellation reaching the sandbox, deny-default `exec_allow`, duplicate JSON rejection at both tool-call and exec boundaries, and known-secret redaction.

## Commands and results

```text
git branch --show-current
slice/p0-phase6

git rev-parse HEAD
79ba58c87b7029af2ccf9668ea3b90344860d5f5

CGO_ENABLED=0 go test ./...
PASS (all packages; internal/sandbox 17.252s)

CGO_ENABLED=0 go test -count=1 ./internal/sandbox -run 'TestFakeBwrapRejected|TestLaunchRefusesSwappedClosureMember|TestReviewOriginLibrarySwapRefused|TestBackendShebangRejected|TestCancelledContextNeverLaunches' -v
PASS
```

The three requested fold guards are implemented and causal. No production security regression was reproduced. The verdict is nevertheless FAIL because the newly shipped shebang acceptance detector is demonstrably false-green, which violates the repository's mandatory red-capable-detector rule.

Weakest link: the trust-root check authenticates the final canonical inode, not each path ancestor. The actual `/usr/bin/bwrap` ancestry was checked and is safe; no rootless exploit was established. Re-evaluate if the backend is ever installed below a user-writable directory.

Proof ceiling: this review proves behavior on the current Linux host and clean HEAD exports; it does not establish properties for an unmeasured future installation path or a hostile root.

Skill update: none.

VERDICT: FAIL

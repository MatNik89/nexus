# Phase 6 verification round 4 — Kilo (verify the re-landed codex-r2 fixes)

Target: `slice/p0-phase6` at HEAD `79ba58c` (`b575e3a` lands the real code, `79ba58c` commits the closure-member RED). Round 3 caught that `23db432` shipped only a dead helper and a RED test; this round verifies the code actually landed. Method: code-read, run the suite in the repo, two ablation probes in a clean `git archive HEAD` export (`/tmp/kilo/p6r4-export`). Working tree never modified. bwrap 0.11.2 present.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (round-3 RED is gone).

## Finding 1 verification — trust root (codex #1)

`Bwrap.Probe` now enforces the trust root BEFORE the behavioral canary (`sandbox.go:138-155`): `filepath.EvalSymlinks` → `os.Stat` → `sys.Uid != 0 || st.Mode().Perm()&0o022 != 0` → refuse with `"backend %s is not a root-owned, non-writable executable"`; the canonical path then replaces `av.BwrapPath`. The canary now uses a random unmarked prefix `os.MkdirTemp("", randomHex(8))` (`sandbox.go:167-169`).

`TestFakeBwrapRejected` passes and asserts the trust-root reason. **Ablation** (removed the `sys.Uid/perm` check in the export): the fake is then refused by the canary instead, and the test goes RED with `fake rejected for the wrong reason (trust root not causal): … did NOT confine …` — so the trust root is causal, not cosmetic.

Adaptive-fake rationale — I attacked "a root-owned 0755 executable already requires root" and found no non-root counterexample: a non-root user cannot write into a root-owned, non-user-writable directory, cannot place or replace a file there, and cannot create a symlink there (`EvalSymlinks` resolves and re-stats the target). A hard link to the genuine root-owned bwrap in a user-writable dir is the SAME inode/bytes (an alias, not a fake). The reasoning holds; the trust chain correctly terminates at root ownership.

## Finding 2 verification — full-closure pin (codex #2)

`Compile` now resolves and pins the entire runtime closure (`sandbox.go:220-226`): `probe.ResolveClosureHashes(spec.Target)` populates `CompiledPolicy.closurePins` and `closureDigest(pins)` is folded into `policyHash`. `Launch` verifies the target (`/nexus-target`), the member count, and **every** member (`sandbox.go:310-324`).

`TestLaunchRefusesSwappedClosureMember` (white-box: corrupts a non-target pin, `sandbox_linux_test.go:354-372`) passes, and it first asserts `len(pol.closurePins) >= 2` (a dynamic `/bin/ls` pins target+loader+libs), so the pin is not vacuous. **Ablation** (removed the `for dest, want := range policy.closurePins` loop): the test goes RED with `closure member with non-compile-time bytes launched` — per-member verification is causal. The `$ORIGIN` swap is closed by construction: `resolveClosure` (shared by `ResolveClosureHashes` at Compile and `Prepare` at Launch) reads each library from disk and hashes it; a swap between the two reads yields a different dest-hash and Launch refuses.

## codex #3 and prior kilo items — still hold

`TestDuplicateArgKeysRejected` (allowlisted last-wins `/bin/ls` + `"duplicate"` reason) passes. Re-confirmed at this HEAD: `TestEffectHashDuplicateKeysDoNotCollapse` + `TestApprovalSurvivesKeyReorder` (dup-key injectivity), `TestExecOutputRedactsKnownSecrets` (redaction), `TestShellInterpreterDeniedByDefault` (exec_allow deny-default), `TestResumePreservesObservationTrust` (resume trust), `TestCancelledContextNeverLaunches` (S7 cancel), `TestBackendShebangRejected` (shebang refused at Compile). All green.

## NEW-defect hunt

None. The trust root checks root-ownership + non-group/world-writability (the "executable" wording in the message is descriptive; executability is enforced by the canary). The double read of the target within `Compile` (`hashFile` + `resolveClosure`) is self-correcting — a change between the two reads makes `targetHash` and `closurePins["/nexus-target"]` disagree, and Launch's dual check refuses. `ResolveClosureHashes` closes the memfds it opens, so Compile leaks nothing. `randomHex`'s `"nexus-canary"` fallback (only on `crypto/rand` failure) still gets a random `MkdirTemp` suffix.

## Verdict

Both round-3 findings are genuinely fixed and causal (ablation RED on each guard), the closure pin is non-vacuous and closes the `$ORIGIN` swap, and no new defect was introduced.

VERDICT: PASS

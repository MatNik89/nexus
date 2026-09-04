# Phase 6 verification round 3 — Kilo (narrow: codex round-2 fold)

Target: `slice/p0-phase6` at HEAD `23db432` (fold of codex round 2, range `d222082..HEAD`). Kilo/agy passed round 2; this review confirms the round-2 fold in the kilo dimensions — and finds the fold does not implement two of its three claimed fixes, with the suite RED.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0. **`CGO_ENABLED=0 go test -count=1 ./...` FAILS** — exactly one failure:

```
--- FAIL: TestFakeBwrapRejected (0.02s)
    sandbox_linux_test.go:280: fake rejected for the wrong reason (trust root not causal):
    SANDBOX_CAPABILITY_UNAVAILABLE: the backend did NOT confine (closure-external directory visible) …
FAIL  github.com/MatNik89/nexus/internal/sandbox
```

## Finding 1 — [HIGH] codex #1 (trust root) is NOT implemented; the fold ships a RED test

The commit claims "Probe now requires the backend binary to live on a root-owned, non-user-writable canonical path … BEFORE any behavioral check." No such code exists. `probe.Detect()` (`internal/preflight/probe/probe.go:47-62`) does only `exec.LookPath("bwrap")` + `bwrap --version` + the floor-version comparison — no `EvalSymlinks`, no `stat` uid-0, no `perm & 0o022` check. The string `"root-owned"` appears **only** in the test assertion (`sandbox_linux_test.go:279`) and one comment (`probe.go:456`); no source produces it. The behavioral canary still refuses the fake, but that is precisely the pre-fix behavior the round-1 finding flagged as spoofable by an adaptive fake. The failing test is correct evidence that the trust-root guard was never wired in.

## Finding 2 — [HIGH] codex #2 (full-closure pinning) is NOT wired; the helper is dead code

The commit claims "Compile now resolves and pins the ENTIRE runtime closure … closureDigest inside policyHash … Launch verifies every memfd-pinned member (count + per-dest hash)." The only source change in the fold was adding `ResolveClosureHashes` (`probe.go:699-713`), which has **zero callers**. The actual path is unchanged:

- `Compile` pins only the target ELF: `targetHash, err := hashFile(spec.Target)` (`internal/sandbox/sandbox.go:193`); the policy hash includes `targetHash` only (`:201-202`).
- `CompiledPolicy` has no closure field: `{spec, probeHash, targetHash, policyHash}` (`sandbox.go:52-57`).
- `Launch` verifies only the target: `h.ClosureHashes()["/nexus-target"] != policy.targetHash` (`sandbox.go:264`); every other memfd-pinned member (loader + libraries) is re-resolved at `probe.Prepare`→`resolveClosure` (`probe.go:594`) and never compared against a compile-time pin.

Consequence: the `$ORIGIN` library-swap TOCTOU (the round-1 [HIGH] "dynamic closure unpinned") remains open — a dependency swapped between `Compile` and `Launch` is read fresh at launch, memfd-pinned with its new bytes, and runs, because no compile-time pin exists to compare against. The claimed fix is a no-op (a dead helper).

## Finding 3 — codex #3 (causal dup-key detector) is real

`TestDuplicateArgKeysRejected` (`internal/exectool/exectool_linux_test.go:241-253`) now uses an allowlisted last-wins value (`/bin/ls`) and asserts the `"duplicate"` refusal reason; removing only the duplicate guard would execute the call. This one is genuinely causal and passes.

## Kilo dimensions

No regression in the kilo dimensions themselves (dup-key injectivity, exec-output redaction, resume trust preservation all still pass). The defect is entirely within the codex round-2 scope: two [HIGH]-severity fixes claimed in the commit message are absent from the source, and the fold ships with a failing test.

## Verdict

The round-2 fold does not deliver two of its three claimed fixes (trust-root and full-closure pinning) — the trust-root guard is unimplemented (the suite is RED on `TestFakeBwrapRejected`) and the closure pinning is dead code. This is a confirmed broken state, not taste.

VERDICT: FAIL

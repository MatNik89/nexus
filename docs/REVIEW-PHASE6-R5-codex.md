# Phase 6 Verification Round 5 — Codex

Target: `slice/p0-phase6` at `e78771f5b589cd13224545b621df7101b24e9a1f`.

Scope: the single commit `e78771f` only. Baseline and ablation probes ran in separate clean `git archive HEAD` exports. The repository working tree was not modified except for this requested report.

## Result

[OK] The round-4 detector defect is fixed, causal, and complete for its claimed boundary.

- `internal/sandbox/sandbox_linux_test.go:168-171` now requires `Bwrap.Compile` itself to return an error. There is no `Launch` fallback.
- `internal/sandbox/sandbox_linux_test.go:172-174` rejects an unrelated refusal reason and accepts the production non-ELF/closure error chain.
- Production behavior remains owned by `internal/sandbox/sandbox.go:223-225`, where `probe.ResolveClosureHashes` failure prevents policy construction.

Clean-export baseline:

```text
CGO_ENABLED=0 go test -count=1 ./internal/sandbox -run '^TestBackendShebangRejected$' -v
--- PASS: TestBackendShebangRejected (0.15s)
PASS
```

Clean-export causal ablation: I changed only the `ResolveClosureHashes` error branch in `Compile` to tolerate the failure and construct a target-only pin, matching the round-4 recipe. The detector turned RED at the new compile-boundary assertion:

```text
CGO_ENABLED=0 go test -count=1 ./internal/sandbox -run '^TestBackendShebangRejected$' -v
=== RUN   TestBackendShebangRejected
    sandbox_linux_test.go:171: shebang script compiled into a policy (must be rejected at Compile)
--- FAIL: TestBackendShebangRejected (0.20s)
FAIL
```

## Diff review

No new functional defect was found in `git show e78771f`. The only executable change is the narrow test correction; the remaining additions are the three committed round-4 review reports. The change adds no production path, dependency, state, or abstraction. Lean already.

## Full suite

```text
CGO_ENABLED=0 go test -count=1 ./...
PASS — all packages; internal/preflight/probe 28.761s, internal/sandbox 10.382s
```

## Weakest points / proof ceilings

1. The reason assertion uses string matching and permits the generic wrapper word `closure`; `errors.Is(err, probe.ErrNotELF)` would be more exact, but this does not defeat the compile-boundary detector or the specified ablation.
2. This regression fixture covers a shebang script, not every possible foreign executable format; that is the stated round-4 defect class.
3. The runtime proof is Linux-host-specific and depends on the real bwrap-backed fixture. This host executed it successfully; no cross-platform claim is made.

Skill update: none. The prior review already identified the reusable detector-validity failure, and the existing NEXUS audit procedure explicitly requires same-boundary causal ablation.

VERDICT: PASS

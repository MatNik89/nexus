# Phase 6 verification round 5 — Kilo (narrow single-diff check)

Target: `slice/p0-phase6` at HEAD `e78771f` (single test-only diff fixing the codex round-4 LOW). Kilo/agy passed round 4; this confirms `git show e78771f` introduces no NEW defect and that the shebang detector is now causal.

## Diff scope

The commit changes only `internal/sandbox/sandbox_linux_test.go` (13 lines) plus review docs — no production code. `TestBackendShebangRejected` now **requires** a Compile-time refusal: the launch-time fallback (`if err == nil { Launch … }`) is removed, replaced by `_, err := b.Compile(...); if err == nil { t.Fatal("shebang script compiled into a policy (must be rejected at Compile)") }`, plus a reason assert (`"not a native ELF"` or `"closure"`).

## Verification

- **Code-read**: `Compile` refuses the shebang at closure resolution — `probe.ResolveClosureHashes` → `resolveClosure` → `parseELFMeta` returns `ErrNotELF` (`"launch target is not a native ELF executable …"`, `probe.go:116`), wrapped by `Compile` as `"cannot pin the runtime closure (fail closed): …"` (`sandbox.go:223-225`). The error contains both asserted substrings.
- **Suite (in repo)**: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`. `TestBackendShebangRejected` passes at HEAD.
- **Ablation (clean export `/tmp/kilo/p6r5-export`)**: changed only `Compile`'s closure-resolve error branch to tolerate failure with a target-only pin (`pins = map[string]string{"/nexus-target": targetHash}`). Result:

  ```
  --- FAIL: TestBackendShebangRejected (0.18s)
      sandbox_linux_test.go:171: shebang script compiled into a policy (must be rejected at Compile)
  ```

  The detector turns RED under the exact codex-r4 ablation recipe (Compile tolerating a closure failure), proving the compile-time refusal — not the launch fallback — is what the test now pins. The previous false-green is closed.

## New-defect hunt

None. No production code changed; the reason assert (`"not a native ELF" || "closure"`) matches the actual wrapped `ErrNotELF` and cannot misfire on a shape-check refusal (a shebang reaches the closure pin with a valid absolute path and sane timeout).

## Verdict

The single-diff is correct and causal: the shebang detector now requires the Compile-time closure refusal and goes RED when that guard is ablated. No NEW defect introduced.

VERDICT: PASS

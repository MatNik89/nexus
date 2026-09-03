# PHASE-1B verification round 4 — Codex

Scope: `slice/p0-phase1` at `e746fd8b968c7ab2e68fb3f5a85d97ba0dee5125`; reviewed only `cf09f36..e746fd8` against findings 3 and 10 in `docs/REVIEW-PHASE1B-R3-codex.md`, plus regressions introduced by those changed lines.

1. [OK] **Round-3 finding 3 is fully folded.** `Fold` is now the genesis wrapper over `FoldFrom(initial, 0, ...)`, while `FoldFrom` initializes its comparison and return checkpoint from the explicit `priorCheckpoint` (`internal/kernel/machine/machine.go:71-106`). Therefore an event at or behind the durable checkpoint rejects before state transition, an empty resumed batch returns the unchanged state and prior checkpoint, and a legal later offset advances both. The three cases are independently asserted in `TestFoldFromEnforcesPriorCheckpoint` (`internal/kernel/machine/machine_test.go:317-344`). The RED is causal: ignoring or zeroing `priorCheckpoint` makes both the stale-event and empty-resume assertions fail.

2. [OK] **Round-3 finding 10 is fully folded.** The configuration owner now computes `sha256(json.Marshal(r.Config))` from the effective typed `config.Resolved` values (`internal/foundation/config/config.go:50-67`). `closure.Seal` accepts a `ConfigBinding`, rejects nil or malformed output, extracts the owner-produced hash once, retains it in the snapshot, and compares every probe against it (`internal/kernel/closure/closure.go:137-159,186-220`). Closure tests now obtain the positive binding through the real `config.Resolve` path; a differently resolved model and the former arbitrary 64-hex token both leave all dependent capabilities OFF (`internal/kernel/closure/closure_test.go:14-38,214-245`). This closes the former free-floating-string path.

3. [OK] **The declared local-interface-forgery ceiling is acceptable for P0.** A Go interface cannot authenticate which in-binary type implemented it, but arbitrary local code injection is not a P0 runtime threat boundary: P0 has no dynamic capability activation/plugin path, the intended composing root supplies the real `config.Resolved`, and `T27` owns the final all-live snapshot verification (`internal/kernel/closure/closure.go:137-146`; `docs/tasks-P0.md:323-334`). A fake binding remains useful only as an in-package negative fixture. This ceiling would become unacceptable if runtime-loaded implementations could call `Seal`, or if T27 failed to verify the production composition path.

4. [OK] **No material new defect was found in `cf09f36..e746fd8`.** Canonical hashing covers the whole current `Config` struct without maps or unstable ordering; provenance (`Origins`) is correctly excluded because it does not change effective configuration. `nil` versus empty slices may conservatively produce different hashes, which causes extra invalidation rather than stale authorization. `FoldFrom` preserves the last legal checkpoint on both offset and transition errors. No dependency or unrelated implementation machinery was added.

## Verification

- `go test -count=1 ./internal/kernel/machine ./internal/foundation/config ./internal/kernel/closure` — PASS.
- `go vet ./internal/...` — PASS (exit 0).
- `go test -count=1 ./internal/...` — PASS (all discovered internal tests; `telemetry` and `negotiation` report no test files).
- `git diff --check cf09f36..e746fd8` — implementation diff clean; only two pre-existing trailing-space lines in the committed review artifact `docs/REVIEW-PHASE1B-R3-agy.md` were reported, immaterial to these folds.

Top three weakest points, none blocking this narrow fold: (1) the real composing root does not exist yet and is therefore proven only at T27; (2) `ConfigHash` relies on the current all-marshalable `Config` field set and intentionally ignores the impossible present-day marshal error; (3) `ConfigBinding` establishes structural ownership, not provenance against malicious code already linked into the binary.

VERDICT: PASS

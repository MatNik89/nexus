# P0-prep verification round 6 — Kilo (tiny: NUL-byte guard)

Target: `slice/p0-prep1` at HEAD `6c142b9` (single codex round-5 finding). Kilo/agy passed all prior rounds. Method: code-read, run the suite in the repo, manual NUL-spliced fixture + ablation in a clean `git archive HEAD` export (`/tmp/kilo/prep1r6`). Working tree never modified.

## Fix analysis

`scripts/machine-id-check.sh` now refuses NUL bytes BEFORE the content reaches a shell variable:

```sh
RAW_LEN="$(wc -c < "$F")"
NONUL_LEN="$(tr -d '\000' < "$F" | wc -c)"
[ "$RAW_LEN" -eq "$NONUL_LEN" ] || { echo "$F content malformed …" >&2; exit 2; }
```

This is correct: `wc -c` counts raw bytes, `tr -d '\000' | wc -c` counts non-NUL bytes, and any difference means a NUL was present — refused as "malformed" before `CONTENT="$(cat …)"` (whose command substitution would silently drop the NUL and normalize a spliced id to valid 32-hex). The Go verifier already refuses NUL via `validateMachineID` (`c` not in `[0-9a-f]`), so the two now agree.

## Verification

- `TestMachineIDCheckScriptMirrorsDoctor` passes (nulsplice fixture asserts the "malformed" reason).
- Manual (correct 16+NUL+16 fixture): with the guard present → `content malformed`; with the guard ablated → `not root-owned` (the spliced id normalized to 32-hex and fell through to the ownership check) — i.e. the detector turns RED on the wrong reason, proving the guard is causal.
- `CGO_ENABLED=0 go vet ./...` exit 0.

## Suite note (not a code defect)

The first `go test -count=1 ./...` run showed a transient `FAIL internal/acceptance` (665s) alongside `internal/probe` 526s and `internal/buildcheck` 219s — severe parallel resource contention on the cold cache. `go test -count=1 ./internal/acceptance/` re-runs green (74.7s). This is the same pre-existing parallel-build flake observed in earlier rounds, not introduced by this diff (which touches only the shell checker and its test).

## NEW-defect hunt

None. The NUL check is a pure read-only byte-count comparison placed before the first variable load; it cannot misclassify a legitimate (no-NUL) file (`RAW_LEN == NONUL_LEN`), and it fails closed on any NUL at any position.

## Verdict

The NUL-byte guard is correct and causal, and `6c142b9` introduces no NEW defect.

VERDICT: PASS

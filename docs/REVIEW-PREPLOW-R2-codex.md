# PREP-LOW round-2 verification — Codex

Reviewed `7477bc2` and `6e375f5` on `slice/p0-prep-low`, bound to immutable HEAD `6e375f52d65a22c91a1b10e32766f5e193ee6ac3` (tree `460ee120921eb7014ce38da700e8fce6a3f0dc7d`). All behavioral runs and controlled mutations used fresh `git archive 6e375f5` exports; the repository was not mutated during probing.

## Finding

### [CONCRETE][SEV: LOW] The claimed shared ASCII trim contract still diverges on leading LF

`cmd/nexus/main.go:1023-1024` uses `strings.Trim(raw, " \t\r\n")`, which removes a leading LF. `scripts/machine-id-check.sh:28-30` applies `sed` per line; an empty leading line remains as a record separator, so the shell value retains that LF and fails its length guard. Thus the implementations do not yet mirror the declared `[ \t\r\n]` outer-trim contract byte for byte. The older comment at `scripts/machine-id-check.sh:20` also still incorrectly says the script mirrors Go `TrimSpace`.

PROBE: in a clean export, a temporary strengthened fixture first asserted that the real production helper accepts `LF + 32 lowercase hex + LF`, then passed the same bytes to the real shell checker. `TestMachineIDCheckScriptMirrorsDoctor` turned RED with `leading-newline content malformed (fail closed)`. This is a reproduced product/parity defect and a missing committed regression case, not a hypothetical ceiling.

FIX: make the shell path trim the same ASCII set over the whole byte sequence (or redefine and implement one narrower contract on both sides), add leading-LF and mixed outer-whitespace fixtures, and correct the stale `TrimSpace` comment.

## Round-1 finding verification

1. **NBSP-specific parity and detector binding — PASS, but the broader parity finding remains open.** Normal clean-export tests passed. Replacing only `trimMachineID` with `strings.TrimSpace` made `TestMachineIDValidation` RED with `NBSP-padded machine id survived the ASCII trim contract`. Separately mutating only the shell trim to remove NBSP made `TestMachineIDCheckScriptMirrorsDoctor` RED because `nbsppad` reached ownership rather than the required malformed-content refusal. The committed NBSP checks are therefore causal on both sides. The leading-LF finding above prevents class closure.
2. **Suspension fixture and later-success ordering — PASS.** The test now creates suspensions through `approval.Store.Suspend`; its `ToolCall` and `ContextBlock` pass the production constructors and the store's validation. It redelivers turn 7 and requires exact recovery of the later success, while turn 8 independently requires the real challenge summary. Mutating only `completedTurnFinal` so `turn.succeeded` updates state only when no earlier suspension was found made `TestRedeliveredSuspendedTurnRecoversChallenge` RED: it recovered `APPROVAL NEEDED ...` instead of `ok`. The unused map is gone. Same-class search found one recovery implementation and one caller, so no sibling ordering path was left unchecked.

## Commands and results

- `git rev-parse HEAD` — `6e375f52d65a22c91a1b10e32766f5e193ee6ac3`; branch `slice/p0-prep-low`; pre-report worktree clean.
- `git ls-tree -r --name-only 6e375f5` plus `git check-ignore -v` / `git status --ignored` — every scoped source and test is tracked; only pre-existing build outputs were ignored.
- `git archive 6e375f5 | sha256sum` — archive stream SHA-256 `7233dfe62a716b220854be63e1b135e5ab821eb55f629a6a757ec671a1ce2098`.
- Clean export: `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^(TestMachineIDValidation|TestMachineIDCheckScriptMirrorsDoctor)$' -v ./cmd/nexus` — PASS, 2 tests.
- Clean export: `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestRedeliveredSuspendedTurnRecoversChallenge$' -v ./internal/app/daemon` — PASS, 1 test.
- Clean export: `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./cmd/nexus ./internal/app/daemon` — PASS for both affected packages.
- Go `TrimSpace` mutation — expected RED, exit 1 at the NBSP assertion.
- Replay `!found` mutation — expected RED, exit 1; earlier challenge displaced required later success `ok`.
- Shell NBSP-acceptance mutation — expected RED, exit 1; `nbsppad` failed the asserted malformed-content reason.
- Strengthened leading-LF parity fixture against unmodified production — RED, exit 1; Go accepted the fixture and the shell rejected it as malformed.
- Clean export: `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -timeout 900s ./...` — exit 1 only in the three `internal/buildcheck` tests because `git archive` contains no `.git` metadata (`repo root: exit status 128`); all other packages passed. The focused and affected-package gates above provide the revision-bound behavioral evidence.

## Simplification and proof ceiling

The reviewed implementation changes add no dependency or speculative abstraction; the helper extraction is the minimum production seam needed to bind the Go detector. The replay test is larger because a production-valid suspension requires a typed call and context, but it reuses the real store and directly covers both terminal orderings.

Weakest link: the NBSP example is fixed and causally tested, but the declared four-byte outer-trim class was not swept across record-boundary placement; leading LF exposes the remaining divergence.

Proof ceiling: this review establishes behavior on Linux/arm64 with Go 1.26.4 and the installed `/bin/sh`, `sed`, and locales. The clean archive cannot satisfy tests that discover the repository root through `.git`; no installed/release artifact was in this two-commit scope.

VERDICT: FAIL

# PREP-LOW round-3 verification — Codex

Reviewed commit `2a49441e12c9e7d873552dc1da0622d80b0d453e` on `slice/p0-prep-low`, bound to tree `a8de129d02ec3dda1f3f8a66997929e73a39e42e`. All behavioral runs and controlled mutations used fresh `git archive 2a49441` exports under `/tmp`; the repository was not mutated by any probe. This report is the only file written by this review.

## Outcome

The production behavior from the round-2 leading-LF finding is fixed: the shell checker now trims the whole outer byte run of exactly `[ \t\r\n]`, agrees with `trimMachineID`, preserves interior whitespace for rejection, and remains fail-closed for NUL and U+00A0. The new `leadlf` fixture is RED-capable on this non-root host. The round nevertheless fails because the committed detector silently omits the new regression cases under UID 0, and the claimed stale-comment correction is incomplete.

## Findings

### [CONCRETE][SEV: LOW] `cmd/nexus/main_test.go:1505` — UID 0 skips both new regression fixtures

`leadlf` and `outmix` are inside `if os.Geteuid() != 0`. Under UID 0 the block executes zero cases, so the committed test does not test the round-3 trim fix at all. This is avoidable: a valid trimmed fixture may prove content acceptance either by succeeding under root or by reaching the expected `not root-owned` guard under a non-root account.

PROBE: in a clean export I restored only the parent commit's per-line `sed` trim while keeping the new test. At UID 1000, `TestMachineIDCheckScriptMirrorsDoctor` exited 1 on `leadlf` with `script diverges from Go`, proving the fixture is sensitive when executed. I then selected the test's UID-0 branch by making only this guard false; the same defective production trim exited 0 and the test passed. Thus the committed detector has a demonstrated environment-dependent false-green path.

FIX: execute the padded/`leadlf`/`outmix` table unconditionally; treat either successful checker output (root-owned fixture) or the exact `not root-owned` refusal (non-root fixture) as evidence that content validation accepted the trimmed ID, while still rejecting any `malformed` result.

### [CONCRETE][SEV: LOW] `scripts/machine-id-check.sh:20`, `cmd/nexus/main_test.go:1502` — the round-2 stale `TrimSpace` claim remains

Both comments still say the shell path “mirrors Go TrimSpace”. The canonical Go owner is deliberately `strings.Trim(raw, " \t\r\n")`, and nearby comments correctly state that Unicode `TrimSpace` would diverge on U+00A0. The old wording therefore contradicts both the implementation and the explicit four-byte contract; the round-2 requested comment correction was not completed.

PROBE: immutable-tree search with `git grep -n -E 'machine-id-check|trimMachineID|TrimSpace' 2a49441 -- scripts cmd/nexus` found the stale claims at exactly those two locations, while `cmd/nexus/main.go:1019-1024` names and implements the ASCII-only owner.

FIX: replace both stale references with `trimMachineID` or “the shared ASCII `[ \t\r\n]` contract”.

## Behavioral closure evidence

- Source trace: `cmd/nexus/main.go:1011-1024` trims with `strings.Trim(raw, " \t\r\n")`; `scripts/machine-id-check.sh:22-35` preserves the whole byte sequence, removes only its maximal outer run of the same four bytes with POSIX parameter expansion, then applies the 32-byte/hex guards. `scripts/p0-accept.sh:30` has no sibling validator and consumes this checker.
- Clean export, `sh -n scripts/machine-id-check.sh` — exit 0.
- Clean export, `LC_ALL=C /home/matej/.local/go/bin/go test ./cmd/nexus -run 'TestMachineID(Validation|CheckScriptMirrorsDoctor)$' -count=1 -v` — exit 0; both committed tests passed.
- The same committed tests under `LC_ALL=C.utf8` and `LC_ALL=en_US.utf8` — exit 0 in both runs.
- Independent clean-export parity test at the real Go helper and real shell-checker boundary — exit 0 for 1,605 comparisons: 535 byte fixtures under each of `C`, `C.utf8`, and `en_US.utf8`. The matrix covered every possible single leading and trailing byte, all four accepted outer bytes individually and mixed/repeated, interior space/tab/CR/LF, two IDs separated by LF, NUL, U+00A0, vertical tab, form feed, empty/trim-only content, and the normal machine-ID shape.
- Parent-trim ablation with the new committed test retained — expected RED, exit 1 at `main_test.go:1519`: `leadlf id refused as content (script diverges from Go)`.
- The independent matrix under that same ablation — expected RED, exit 1 across all three locales for leading-LF/mixed record-boundary cases.
- Pristine second export: the extracted script blob `8a2a7510c7e6f52b07768965ca1b33f655fa2c05` and test blob `1bd5af8c8942991d36d0bb8295b1602d4af55da3` matched the target commit; `/home/matej/.local/go/bin/go test ./cmd/nexus -count=1` exited 0.

## Simplification, weakest link, and proof ceiling

The production change is lean: it removes the `sed` process from this trim, adds no dependency or abstraction, and changes the existing owner only. No smaller unchanged implementation preserves leading-LF parity, because the parent implementation was directly ablated and failed.

Weakest link: the root-only false-green was proved by the exact committed guard plus a controlled branch negative control, not by running the suite as UID 0. Upgrade that proof by making the table UID-independent and showing the parent-trim ablation RED in both root and non-root CI containers.

Proof ceiling: this review establishes source and runtime behavior for commit `2a49441` on Linux/arm64, Go 1.26.4, `/bin/sh` = dash, and the three installed locales. It does not establish behavior on another POSIX shell/platform or release readiness. The full repository gate was not run because this review is narrowly scoped to the machine-ID parity fix; the complete affected Go package passed from a pristine export.

VERDICT: FAIL

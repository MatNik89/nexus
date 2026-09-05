# P0-PREP verification round 4 — Codex

Reviewed branch `slice/p0-prep1` at immutable HEAD `7c883be25d15d438c8932328fafda0e12404eb35`, limited to the Codex round-3 findings and defects introduced by `7b7e5f4..7c883be`. All mutations and adversarial fixtures ran in fresh `git archive HEAD` exports. The repository was not modified except for this requested report; other reviewers' untracked report files appeared during the run and were neither read nor used.

## Findings

[CONCRETE][SEV: MED] `scripts/machine-id-check.sh:14-18` — the extracted producer checker still does not mirror the Go verifier: it validates only the first line and ignores every later byte. The Go path reads the complete file, trims its outer whitespace, and validates the complete remaining value (`cmd/nexus/main.go:1007-1015`). In a clean export, a controlled `stat` shim supplied the already-reviewed `uid=0, mode=644` trust prerequisites; a file containing a valid 32-hex first line followed by `garbage` exited 0 and printed the first line. Doctor would reject the same content. Thus `p0-accept.sh` can again announce acceptance PASS and sign an attestation that doctor refuses. This is a workflow false-PASS, not a grant bypass. Require EOF after the permitted machine-ID line (or otherwise validate the complete file using semantics shared with Go), and add a valid-first-line-plus-garbage fixture.

[CONCRETE][SEV: LOW] `cmd/nexus/main_test.go:1474-1486` — the shell mirror detector is not revert-proof for its content and symlink guards. The spaced fixture is user-owned and therefore stops at the ownership check before reaching content validation; the symlink target is also user-owned, and the test asserts only nonzero exit rather than the symlink refusal reason. In separate clean exports, deleting the content-regex line left `TestMachineIDCheckScriptMirrorsDoctor` PASS, and deleting the symlink guard also left it PASS. Assert the expected refusal reasons and arrange each fixture so all earlier predicates pass, as the permission cases already do.

## Verified fold behavior

- Clean-export direct mode-`602` probe — refused with `is group/world-writable (602, fail closed)`.
- Clean-export direct embedded-space probe — refused; after isolating trust prerequisites it reached `content malformed`.
- Clean-export direct symlink probe — refused with `is a symlink (fail closed)`.
- The numeric permission arithmetic rejects the previously missed `602`, `642`, `646`, and `622` shapes. Replacing it with the old broken glob made `TestMachineIDCheckScriptMirrorsDoctor` fail because mode `602` was refused for the wrong ownership reason — expected RED.
- `p0-accept.sh` obtains `MID` only through `scripts/machine-id-check.sh`; no stale inline validator remains.
- Both Go and shell user-owned-file assertions now skip when `os.Geteuid() == 0`, removing the root-run false failure.
- Clean-export committed test: `CGO_ENABLED=0 go test -count=1 -run TestMachineIDCheckScriptMirrorsDoctor ./cmd/nexus` — PASS.

## Suite and checks

- Repository: `CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0; complete combined-log `grep -n 'FAIL'` — no matches.
- `sh -n scripts/machine-id-check.sh` and `sh -n scripts/p0-accept.sh` — PASS.
- Content-guard deletion ablation: `TestMachineIDCheckScriptMirrorsDoctor` — unexpected PASS, proving the detector gap.
- Symlink-guard deletion ablation: `TestMachineIDCheckScriptMirrorsDoctor` — unexpected PASS, proving the detector gap.
- The bound revision remained `7c883be25d15d438c8932328fafda0e12404eb35`, with no tracked working-tree modification before this report was created.

Complexity review: extracting the shell checker is a small, appropriate reuse step and adds no dependency. The remaining defect is incomplete whole-file validation plus fixtures that do not isolate their claimed guards, not excess machinery.

Weakest link: the multiline parser defect was exercised with a controlled `stat` shim rather than a real root-owned malformed `/etc/machine-id`, because the review was read-only; the source-level divergence and exit-0 behavior are otherwise direct.

VERDICT: FAIL

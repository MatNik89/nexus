# P0-PREP verification round 5 — Codex

Reviewed branch `slice/p0-prep1` at immutable HEAD `e2f4442868c48cbaa6409b05435647a7163ecf6e`, limited to the Codex round-4 findings and defects introduced by `7c883be..e2f4442`. All mutations and adversarial fixtures ran in fresh `git archive HEAD` exports. Codex modified no repository file except this requested report. Other reviewers' report files were neither read nor used; concurrent tracked edits appeared only after this report was first written and were not used as evidence.

## Finding

[CONCRETE][SEV: MED] `scripts/machine-id-check.sh:15-22` — the shell “whole file” path cannot preserve NUL bytes. `/bin/sh` command substitution silently removes embedded NULs before the length and charset guards inspect `MID`. In a clean export, a binary fixture containing `0123456789abcdef`, one NUL byte, `0123456789abcdef`, and a newline was run with the same controlled `stat` shim used to isolate already-reviewed trust prerequisites; the checker exited 0 and printed the normalized 32-hex ID. Go's `strings.TrimSpace` does not remove NUL, so `machineIDSHAAt` rejects that same 33-byte value. This restores the producer/verifier false-PASS class: `p0-accept.sh` can sign an attestation that doctor refuses. The committed test has no binary-content case. Reject NUL bytes before loading file data into shell variables and add a NUL fixture that asserts the `malformed` reason.

## Verified fold behavior

- The prior stat-shim valid-first-line-plus-`garbage` probe now refuses with `content malformed`.
- Clean-export mode-`602`, embedded-space, and symlink probes refuse for their exact `writable`, `malformed`, and `symlink` reasons respectively.
- A trusted fixture with surrounding blank lines/spaces is normalized to the same 32-hex value, matching the intended outer-whitespace behavior for the exercised ASCII inputs.
- The committed `garbage`, `twoline`, `spaced`, and `shorthex` fixtures all reach the content guards before ownership and assert `malformed`.
- Length-guard deletion made `TestMachineIDCheckScriptMirrorsDoctor` fail because `shorthex` reached the wrong ownership reason — expected RED.
- Charset-guard deletion made the test fail because `spaced` reached the wrong ownership reason — expected RED.
- Symlink-guard deletion made the test fail because the link reached the wrong permission reason — expected RED.
- User-owned refusal assertions remain skipped under effective UID 0.

## Suite and checks

- Repository: `CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0; complete combined-log `grep -n 'FAIL'` — no matches.
- Clean export: `CGO_ENABLED=0 go test -count=1 -run TestMachineIDCheckScriptMirrorsDoctor ./cmd/nexus` — PASS.
- `sh -n scripts/machine-id-check.sh` and `sh -n scripts/p0-accept.sh` — PASS.
- The bound revision remained `e2f4442868c48cbaa6409b05435647a7163ecf6e`, with no tracked working-tree modification before this report was created.

Complexity review: the whole-file sentinel and two small guards are otherwise lean and dependency-neutral. The remaining issue is a shell data-model boundary, not excess abstraction.

Weakest link: the NUL probe used a controlled `stat` shim rather than replacing the real root-owned `/etc/machine-id`, as required by the read-only constraint; the exact checker still executed against the binary fixture and returned exit 0.

VERDICT: FAIL

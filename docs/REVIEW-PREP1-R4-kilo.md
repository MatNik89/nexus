# P0-prep verification round 4 — Kilo (narrow: confirm no NEW defect in 7b7e5f4..HEAD)

Target: `slice/p0-prep1` at HEAD `7c883be` (`63a61a5` script/verifier mirror + `7c883be` causal perm test). Kilo/agy passed all prior rounds. Method: code-read, run the suite in the repo, manual fixtures against the shared checker. Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (no FAIL anywhere).

## codex #1 — producer/verifier mirror (verified)

`scripts/machine-id-check.sh` now mirrors `machineIDSHAAt`/`validateMachineID` exactly: symlink refusal (`[ -L ]`), regular-file (`[ -f ]`), numeric perm arithmetic (`[ $(( 0$PERMS & 022 )) -eq 0 ]`), root-ownership (`stat -c '%u'` = 0), and first-line-verbatim content (`IFS= read -r MID`, then `^[0-9a-f]{32}$` + non-zero). `p0-accept.sh` delegates to the shared checker.

I ran the fixtures directly: 602 and 622 are refused with `group/world-writable (…, fail closed)`, a symlink is refused (`is a symlink`), embedded-whitespace is refused. The numeric `& 022` replaces the broken `*[2367]?|?[2367]` glob (which missed 602/642/646/622), and the perm check runs BEFORE ownership, so `TestMachineIDCheckScriptMirrorsDoctor` asserts the "writable" reason causally without root.

## codex #2 — euid-0 skip (verified)

`TestMachineIDValidation` skips the user-owned-refusal assertions under `os.Geteuid() == 0`, where the fixture is legitimately root-owned. Correct.

## My round-3 finding — fixed

The symlink divergence I flagged (script `-f` followed symlinks, doctor `Lstat` refused) is closed: the script now refuses symlinks (`[ -L ]`) exactly as the Go `Lstat`+`IsRegular` does.

## Remaining observations (LOW, not defects)

The "mirror exactly" goal still has two theoretical content edge-cases:

1. **Leading whitespace**: `IFS= read -r` preserves leading whitespace (script refuses), while `machineIDSHAAt` uses `strings.TrimSpace` (doctor accepts and hashes the trimmed id).
2. **Multi-line file**: `read -r MID` takes the first line (script accepts and hashes the first line), while `TrimSpace` keeps the interior newline and `validateMachineID` refuses.

Both require a malformed `/etc/machine-id` that never occurs in practice (systemd writes `32-hex\n`), and both are fail-closed in the grant-transferring direction (the doctor never accepts a malformed id in a way that weakens machine binding). Not security findings.

## Verdict

Both codex fixes are correct and causal (numeric perm check, symlink refusal, euid-0 skip), my round-3 symlink finding is closed, and no NEW defect was introduced in `7b7e5f4..HEAD`.

VERDICT: PASS

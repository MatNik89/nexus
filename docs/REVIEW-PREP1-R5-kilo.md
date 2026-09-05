# P0-prep verification round 5 — Kilo (narrow: confirm no NEW defect in 7c883be..HEAD)

Target: `slice/p0-prep1` at HEAD `e2f4442` (fold of codex round 4: whole-file mirror + revert-proof fixtures). Kilo/agy passed all prior rounds. Method: code-read, run the suite in the repo, manual fixtures against the shared checker. Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (no FAIL anywhere).

## codex #1 — whole-file validation (verified for the lenient direction)

`scripts/machine-id-check.sh` now validates the WHOLE file: `CONTENT="$(cat "$F"; printf x)"; CONTENT="${CONTENT%x}"` preserves the exact bytes, then a `sed` outer-trim, then a 32-length guard + POSIX `case *[!0-9a-f]*` charset guard (no per-line regex). I ran the fixtures: `valid+garbage` → "malformed", two-line → "malformed", shorthex (16 hex) → "malformed". The prior LENIENT bug (valid-first-line-plus-garbage accepted) is genuinely closed.

## codex #2 — content-before-ownership + per-fixture reason (verified)

The content guards now run BEFORE ownership, and every fixture asserts its own refusal reason (`malformed`/`symlink`/`writable`/`not root-owned`). `TestMachineIDCheckScriptMirrorsDoctor` is causal on a non-root run (the `shorthex` fixture pins the length guard; the symlink fixture pins the symlink guard).

## NEW finding

### 1. [LOW] The `sed` outer-whitespace trim is a no-op for single-line files, so the script still diverges from the Go verifier on whitespace-padded ids

The script's trim is `sed -e ':a' -e 'N;$!ba' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'`. The `N` command on a **single-line** file (the normal `/etc/machine-id` shape) reads past EOF and terminates sed **before** the two `s` commands run, so neither the leading nor trailing whitespace is actually trimmed.

Probe (manual, clean export): `printf ' 0123456789abcdef0123456789abcdef\n' | sed -e ':a' -e 'N;$!ba' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'` outputs 34 bytes (leading space preserved), so `machine-id-check.sh` refuses a leading-space id as "malformed" (length 33). The Go verifier, by contrast, does `strings.TrimSpace(raw)` and then `validateMachineID`, which **accepts** the same padded id (trimmed to 32 hex, hashed to the same value).

Consequence: the fold's stated goal — "outer whitespace trimmed exactly like Go TrimSpace" — is not realized for the single-line case. The divergence is strictly fail-closed and theoretical: the script is stricter than the verifier (a false-negative in `p0-accept.sh`, never a false-accept), and a machine-generated `/etc/machine-id` never carries leading/trailing whitespace. Fix: replace the slurp idiom with `$!N` (or drop the slurp and use the plain `sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'`, which trims per line and still refuses multi-line content via the length guard).

## Minor observation (not a defect)

The script's check order is `symlink → regular → perm → content → ownership`, while `machineIDSHAAt` is `symlink/regular → ownership+perm → content`; for a user-owned malformed file the two refuse with different reasons ("malformed" vs "not root-owned") but both refuse. Diagnostic-only divergence.

## Verdict

The whole-file lenient bug is fixed and the fixtures are causal, but the new `sed` trim is itself a no-op for single-line files — a LOW, fail-closed, theoretical divergence from the Go verifier (not a confirmed security/correctness break).

VERDICT: PASS

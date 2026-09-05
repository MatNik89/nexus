# P0-prep verification round 3 — Kilo (narrow: confirm no NEW defect in d780b51..HEAD)

Target: `slice/p0-prep1` at HEAD `7b7e5f4` (fold of codex round 2: #1 machine-id trust+validity, #2 full-destination guard). Kilo/agy passed both prior rounds. Method: code-read, run the suite in the repo, two probes in a clean `git archive HEAD` export (`/tmp/kilo/prep1r3`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 84s, no FAIL anywhere).

## codex #1 — machine-id trust + validity (verified)

`machineIDSHAAt` (`main.go:989-1022`) now requires a trustworthy identity file: `os.Lstat` + `IsRegular()`, `sys.Uid == 0`, `perm & 0o022 == 0`, and `validateMachineID` (exactly 32 lowercase hex, non-zero). `p0-accept.sh` mirrors the same checks (root-owned, non-writable, `^[0-9a-f]{32}$`, not all-zeros). `TestMachineIDValidation` covers six invalid shapes + user-owned-file refusal + the real `/etc/machine-id` pass; I confirmed the script and `sha256(TrimSpace(read))` agree byte-for-byte on this host.

## codex #2 — full-destination guard (verified, causal)

The projection's `EvOutboundResolved` predicate now binds BOTH `adapter_id` and `channel_identity` (`channel.go:292-300`), and refuses a half-guard (only one of adapter/identity set). `ReconcileFor` carries the adapter; the telegram handler passes `"telegram"`.

**Ablation** (dropped the `AND adapter_id=?` term in the export): `TestOutboxIsDestinationBound` goes RED — `telegram command re-pended a foreign-adapter delivery: "Re-queued dlv-…"`. The adapter half of the guard is causal.

## NEW finding

### 1. [LOW] Symlink handling diverges between the script and the doctor (fail-closed false-negative)

`machineIDSHAAt` uses `os.Lstat`, which does NOT follow symlinks, so a symlinked `/etc/machine-id` is refused as `"not a regular file"`; `p0-accept.sh`'s `[ -f … ]` and `stat -c '%u'` DO follow symlinks, so it accepts the same path and writes the attestation. Probe (clean export): `machineIDSHAAt(link)` → error, while `os.Stat(link).Mode().IsRegular() == true`.

Consequence: on a host where `/etc/machine-id` is a symlink (some immutable/stateless distros, or a bind-mounted container id), `p0-accept.sh` succeeds and writes the attestation, but `doctor --p0` then refuses with a confusing mismatch. This is strictly fail-closed (no false grant — a symlink cannot transfer a grant to another machine, and the doctor refuses rather than trusting it), so it is a portability false-negative, not a security hole. The fix is cosmetic consistency: have the script and the doctor use the same symlink policy (both `Lstat`+`IsRegular`, or both resolve via `EvalSymlinks`+`Stat`).

## NEW-defect hunt (elsewhere)

None. The `validateMachineID` and writability checks agree with the script's `case … in *[2367]?|?[2367]` octal logic; the half-guard refusal is fail-closed; the internal unbound `Reconcile` (Flush path) is correctly unaffected by the destination guard.

## Verdict

Both codex fixes are correct and causal (machine-id validation mirrors the script; adapter-guard ablation RED), and the only new observation is a LOW fail-closed symlink-policy divergence — not a confirmed broken/security finding.

VERDICT: PASS

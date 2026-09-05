# P0-prep verification round 2 — Kilo (narrow: confirm no NEW defect in eb0d6f8..HEAD)

Target: `slice/p0-prep1` at HEAD `d780b51` (fold of codex round 1: #1 machine binding, #2 destination binding). Kilo/agy passed round 1; this confirms the fold introduces no NEW defect. Method: code-read, run the suite in the repo, one ablation probe in a clean `git archive HEAD` export (`/tmp/kilo/prep1r2`). Working tree never modified.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 90s, no FAIL anywhere).

## codex #1 — machine binding (verified)

`verifyAcceptanceAttestation` now also verifies `att.MachineID` against `machineIDSHA()` (sha256 of `/etc/machine-id`, `main.go:983-992`), after `runtimeHostID()` returns `(string, error)` (no `"unknown"` sentinel — a `syscall.Uname` failure fails closed). `p0-accept.sh` writes `machine_id_sha256` from `tr -d '\n' < /etc/machine-id | sha256sum`.

Format consistency confirmed on this host: the script and `sha256(TrimSpace(read))` both yield the same digest for a standard `32-hex + \n` machine-id (I compared them directly). Identity-read failures fail closed; an empty/missing machine-id refuses. Spoofing requires either writing `/etc/machine-id` (root) or re-signing the attestation (owner key) — no key-less path. `TestDoctorP0GrantLive`'s same-kernel-other-machine branch is genuine.

## codex #2 — destination-bound outbox (verified, causal)

`UnreconciledFor(adapter, identity)` filters by `channel_identity=?` (`channel.go:601-619`), and `ReconcileFor` carries the confirming `Identity` into the projection transition as an atomic SQL predicate (`channel.go:289-295`: `UPDATE … WHERE delivery_id=? AND status='UNKNOWN' AND channel_identity=?`, guarded by `RowsAffected()==1`). A same-profile sibling chat sees nothing and cannot re-pend.

**Ablation** (removed the `AND channel_identity=?` append in the export): `TestOutboxIsDestinationBound` goes RED — `sibling chat re-pended another chat's delivery: "Re-queued dlv-…"`. The destination guard is causal, not just the listing filter.

## NEW-defect hunt

None. Two LOW observations (not defects):

- `machineIDSHA()` does not stat-check `/etc/machine-id` as root-owned/non-writable (unlike the bwrap/ssh-keygen trust roots) — but writing it already requires root, so it is within the accepted ceiling, not a key-less vector.
- The confirming `Source` is persisted in the `channel.outbound_resolved` journal event (durable, auditable via replay) but not exposed as a projection column; the "persisted for audit" claim holds via the event, not via a projection query.

## Verdict

Both codex fixes are correct and causal (machine-id format matches the script; destination guard ablation RED), and no NEW defect was introduced in `eb0d6f8..HEAD`.

VERDICT: PASS

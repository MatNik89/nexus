# P0-PREP verification round 3 — Codex

Reviewed branch `slice/p0-prep1` at immutable HEAD `7b7e5f433e39cd484916e38344cce5922d191f11`, limited to the Codex round-2 findings and defects introduced by `d780b51..7b7e5f4`. All mutations and adversarial tests ran in fresh `git archive HEAD` exports. The repository was not modified except for this requested report; other reviewers' untracked report files appeared during the run and were neither read nor used.

## Findings

[CONCRETE][SEV: MED] `scripts/p0-accept.sh:28-39` — the shell producer does not mirror the doctor's trust and content checks. Its permission glob `*[2367]?|?[2367]` checks the group digit of normal three-digit modes but misses the world digit: an exact `/bin/sh` probe accepted world-writable modes `602`, `642`, and `646`. It also removes all whitespace before validation, so an embedded-space value such as `0123456789abcdef 0123456789abcdef` becomes valid and is signed, while `machineIDSHAAt` retains the embedded space and rejects the same file. `-f` and `stat` also follow a symlink that the doctor's `Lstat` rejects. Consequently the acceptance script can finish with `acceptance PASS`, write a signed attestation, and optionally install the binary on a host where doctor necessarily refuses the grant. This is a false-PASS acceptance workflow, not a P0-grant bypass, because the Go verifier still fails closed. Fix the world-write glob, reject symlinks, and validate the original file as exactly one 32-lowercase-hex nonzero line without deleting interior whitespace; add shell-level fixtures for mode `0646`, a symlink, and embedded whitespace.

[CONCRETE][SEV: LOW] `cmd/nexus/main_test.go:1412-1418` — the “user-owned file refusal” detector assumes the test runner is non-root. `t.TempDir` and `os.WriteFile` create a file owned by the effective UID; under UID 0 it is root-owned, regular, mode 0644, and valid, so `machineIDSHAAt` correctly accepts it and the test fails. The current suite ran as UID 1000 and therefore cannot establish root-run portability. Skip this assertion under UID 0 or create a genuinely non-root-owned fixture where the platform and test privileges permit it.

## Verified fold behavior

- Go-side identity validation is fail-closed for non-regular files, non-root ownership, group/world write bits, empty/template/all-zero/malformed content, and read/stat errors (`cmd/nexus/main.go:988-1037`). The real host file passed as a regular root-owned mode-0644, single 32-lowercase-hex value.
- Clean-export committed tests passed: `CGO_ENABLED=0 go test -count=1 -run 'TestMachineIDValidation|TestOutboxIsDestinationBound' ./cmd/nexus` — PASS.
- All-zero comparison ablation (`false && !nonzero`) made `TestMachineIDValidation` fail at `invalid machine id ... accepted` — expected RED.
- The unchanged round-2 foreign-adapter exploit expectation turned RED: Telegram received `Redeliver failed`, and the journal projection rejected the cross-adapter row atomically.
- Adapter-predicate ablation (dropping `adapter_id=?` while retaining the identity predicate) made `TestOutboxIsDestinationBound` fail because Telegram re-pended the foreign-adapter delivery — expected RED.
- A clean probe supplied each half guard independently to `ReconcileFor`; both calls failed and the UNKNOWN row remained untouched. The handler passes the Telegram adapter together with the admitted channel identity.
- The reconciliation transition still requires `status='UNKNOWN'`, and the legacy unbound `Reconcile` remains used only for the core's definite pre-wire failure recovery and its existing core test.

## Suite and checks

- Repository: `CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0; complete combined-log `grep -n 'FAIL'` — no matches.
- `sh -n scripts/p0-accept.sh` — PASS.
- Exact `/bin/sh` mirror probe — modes `602`, `642`, and `646` incorrectly classified ACCEPT; embedded-whitespace machine ID normalized to a regex-valid 32-hex value.
- The bound revision remained `7b7e5f433e39cd484916e38344cce5922d191f11`, with no tracked working-tree modification before this report was created.

Complexity review: the Go fold is lean and uses existing standard-library/file metadata primitives; no new dependency or speculative abstraction was introduced. The shell duplicates the Go validation semantics, and that duplication is the source of the confirmed divergence.

Weakest link: the root-run test failure is source-proven but was not executed under UID 0; the shell false-PASS was directly reproduced with the exact `/bin/sh` patterns, while a full acceptance run against a deliberately unsafe real `/etc/machine-id` was excluded by the read-only constraint.

VERDICT: FAIL

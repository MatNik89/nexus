# P0-PREP verification round 2 — Codex

Reviewed branch `slice/p0-prep1` at immutable HEAD `d780b5166282133746b83c263b2adf1fa14457aa`, limited to the Codex round-1 findings and regressions introduced by `eb0d6f8..d780b51`. Production and test probes used clean `git archive HEAD` exports. The working tree was not modified except for this requested report; other reviewers' untracked report files appeared during the run and were neither read nor used.

## Findings

[CONCRETE][SEV: HIGH] `cmd/nexus/main.go:988-997`, `scripts/p0-accept.sh:30-31` — the new machine binding does not establish the claimed root-owned, valid machine identity. Both producer and verifier accept and hash arbitrary file contents after whitespace handling; neither rejects an empty, `uninitialized`, malformed, all-zero, non-regular, non-root-owned, or writable `/etc/machine-id`. Therefore two same-kernel hosts with the same invalid/template value receive the same binding, and a user-writable identity file lets the local user select a copied attestation's machine digest. The new test supplies a different 64-hex digest (`internal/acceptance/acceptance_linux_test.go:980-993`), so it proves comparison sensitivity but not identity-file authenticity or validity. This host happened to have a regular root-owned mode-0644, 32-lowercase-hex file; the code does not enforce that deployment precondition. Fix both the acceptance script and doctor to fail closed unless the file is a root-owned, non-group/world-writable regular file containing exactly one nonzero 32-lowercase-hex machine ID (and test invalid/unsafe file states through an injectable read seam).

[CONCRETE][SEV: MED] `internal/channel/channel.go:289-296,587-597`, `cmd/nexus/main.go:1222-1231` — reconciliation is identity-bound, not destination-bound. Listing correctly filters by `(adapter_id, channel_identity)`, but `ReconcileFor` carries no adapter and its atomic SQL predicate checks only `channel_identity`. A clean probe created an UNKNOWN row for adapter `other-adapter`, identity `chat-42`, then submitted its known delivery ID through Telegram identity `chat-42`; the handler returned `Re-queued` and changed the foreign-adapter row. Delivery-ID secrecy is not authorization. P0 currently wires only Telegram, which limits present production reachability, but the generic channel owner already models adapter as part of destination identity and the prior finding explicitly required the same `(adapter_id, channel_identity)` at transition time. Carry adapter into `ReconcileFor` and require both columns in the atomic predicate; add a cross-adapter collision case to `TestOutboxIsDestinationBound`.

## Verified fold behavior

- Clean-export focused tests passed: `CGO_ENABLED=0 go test -count=1 -run 'TestDoctorP0GrantLive|TestOutboxIsDestinationBound' ./cmd/nexus ./internal/acceptance` — PASS.
- The unchanged round-1 foreign-same-profile exploit probe turned RED at `foreign same-profile chat could not see target row: "Outbox clean..."`; `chat-666` can no longer list or obtain `chat-42`'s row through the command path.
- Listing-filter ablation (`UnreconciledFor` back to `Unreconciled`) made `TestOutboxIsDestinationBound` fail because the sibling saw the delivery — expected RED.
- Projection-guard ablation (`false && p.Identity != ""`) made `TestOutboxIsDestinationBound` fail because the sibling re-pended the delivery — expected RED.
- Machine-comparison ablation (`false && att.MachineID != mid`) made `TestDoctorP0GrantLive` fail at `same-kernel other-machine attestation granted` — expected RED.
- `runtimeHostID` now returns the `syscall.Uname` error and `verifyAcceptanceAttestation` rejects it; `/etc/machine-id` read errors are also rejected. The accepted-content and file-trust checks described above remain missing.
- Reconciliation still requires status `UNKNOWN`, so the new predicate cannot resurrect a `SENT` row. The confirming source is serialized in the journal event payload, and the sibling-chat test confirms a rejected identity leaves the row UNKNOWN before the owner succeeds.

## Suite and checks

- Repository: `CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0; complete combined-log `grep -n 'FAIL'` — no matches.
- `git diff --check eb0d6f8..HEAD` reported only pre-existing trailing whitespace in the committed round-1 Agy report, outside this narrow fold; `sh -n scripts/p0-accept.sh` passed.
- The bound revision remained `d780b5166282133746b83c263b2adf1fa14457aa`, with no tracked working-tree modifications before this report was created.

Weakest link: the cross-adapter defect requires knowledge of the 96-bit delivery ID and a second adapter, which P0 does not currently wire; it is nevertheless a demonstrated authorization failure in the destination-aware channel API. The machine-ID defect is source-proven but was not exercised by replacing the host's real `/etc/machine-id`, because the review was expressly read-only.

VERDICT: FAIL

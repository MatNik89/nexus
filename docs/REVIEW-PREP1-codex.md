# P0-PREP review round 1 — Codex

Artifact reviewed: branch `slice/p0-prep1`, commit `eb0d6f847025ef8a148a49f157d36b84762f6f1f` (`eb0d6f8^..eb0d6f8`). The commit changes only `cmd/nexus/main.go`, `cmd/nexus/main_test.go`, and `internal/acceptance/acceptance_linux_test.go`; all three are tracked in the reviewed tree. The worktree was clean at review start. Other reviewers' untracked artifacts appeared later and were neither read nor used.

## Findings

[CONCRETE][SEV: HIGH] `cmd/nexus/main.go:953` — the new value is kernel-platform binding, not host binding. `runtimeHostID` returns only `Sysname + Release + Machine` (`:955-970`), exactly the non-unique tuple emitted by `uname -srm` into the signed file (`scripts/p0-accept.sh:30-34`). Two machines running the same kernel release on the same architecture are indistinguishable, so copying the exact signed binary/attestation/anchor set to such a machine still transfers the `P0-capable` grant despite the stated invariant at `main.go:884-886`. The new detector uses a deliberately different kernel tuple (`internal/acceptance/acceptance_linux_test.go:958-964`), so it proves that the comparison is causal but cannot detect same-tuple cross-host transfer.
PROBE: clean-export `TestDoctorP0GrantLive` passed; ablating only `att.Host != runtimeHostID()` made it fail at `foreign-host attestation granted`. A clean probe also proved Go formatting equals `uname -srm` on this host and that an empty host is rejected. The remaining defect follows directly from the compared field set; no second physical same-kernel host was available, so that transfer was not executed live.
FIX: bind the attestation to a stable machine-specific identifier or host public-key digest in addition to the kernel tuple, and make identity-read failure return an error rather than the sentinel `"unknown"`.

[CONCRETE][SEV: MED] `cmd/nexus/main.go:1174` — outbox listing and redelivery are profile-wide rather than destination-chat-bound. `Unreconciled` queries only by status (`internal/channel/channel.go:433-458`), while `Reconcile` accepts only a delivery ID (`:567-576`); the handler never compares the row's `AdapterID` or `ChannelIdentity` with the admitted inbound source (`cmd/nexus/main.go:1174-1202`). The Telegram boundary rejects unbound/cross-profile chats, but it admits every chat bound to the active profile (`internal/channel/telegram/telegram.go:220-235`). Consequently, a second chat bound to the same profile can list text addressed to another chat and re-pend that chat's UNKNOWN delivery. This is weaker than the existing exact-source authorization used for approvals and does not establish that the delivery's owner accepted the duplicate risk.
PROBE: clean-export `TestProbeForeignSameProfileChatCanSeeAndRedeliver` created an UNKNOWN row for `chat-42`; an admitted `chat-666` input listed its ID and text, issued `redeliver`, and changed it to `PENDING`. The committed `TestOutboxRedeliverCommand` uses `chat-42` for both operations (`cmd/nexus/main_test.go:1281-1294`) and therefore misses this boundary.
FIX: list UNKNOWN rows by `(adapter_id, channel_identity)` and reconcile with an atomic predicate over delivery ID plus that same admitted destination/source; persist the confirming source in the reconciliation event.

## Attacked checks that held

- A `SENT` delivery cannot be resurrected: the projection accepts reconciliation only from `UNKNOWN` (`internal/channel/channel.go:274-288`); the clean probe left a targeted SENT row unchanged and returned `Redeliver failed`.
- Cross-profile rows remain physically separated by the profile journal, and an unbound or differently bound Telegram chat is rejected before admission. Stored outbox text crosses the journal redactor before projection; a clean probe containing the configured provider secret listed only its redaction marker.
- Production delivery IDs are lowercase 24-hex hashes, challenge IDs are lowercase 24-hex random values, and reminder creation validates the source ID with `[A-Za-z0-9_-]` before deriving `occ-<id>#1` (`internal/obligation/obligation.go:217-227,365-380`). The clean probe rejected `meds/pm`, accepted `Meds_pm`, and successfully routed `ack occ-Meds_pm#1`.
- The anchored Go regular expressions are RE2 expressions with no catastrophic-backtracking path. Case-insensitive command words still work with the lowercase generated challenge/delivery IDs; malformed IDs fall through to `RunChannelTurn` (`cmd/nexus/main.go:1217-1225`), not to a shell or direct effect sink, so no command-injection bypass was found.
- On this Linux/aarch64 host, `runtimeHostID()` exactly matched `uname -srm`; an empty attestation host did not match.

## Evidence

- Repository suite, quiet rerun: `CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0; combined-log `grep -n FAIL` — CLEAN. The first identical run overlapped other full-suite processes and timed out while nested `go build` children waited; the quiet rerun removed that environmental variable and completed normally.
- Clean HEAD export: `CGO_ENABLED=0 go test -count=1 -run '^(TestCommandWordsNeedExactIDs|TestOutboxRedeliverCommand)$' -v ./cmd/nexus` — PASS.
- Clean HEAD export: `CGO_ENABLED=0 go test -count=1 -run '^TestDoctorP0GrantLive$' -v ./internal/acceptance` — PASS.
- Clean export, redeliver-route ablation only: `TestOutboxRedeliverCommand` — FAIL at `redeliver refused`; detector is causal.
- Clean export, host-comparison ablation only: `TestDoctorP0GrantLive` — FAIL at `foreign-host attestation granted`; detector is causal for comparison removal.
- Clean export, approve-regex ablation only: `TestCommandWordsNeedExactIDs` — FAIL because `approve my vacation plan` was swallowed as a command; detector is causal.
- Clean adversarial probes: foreign same-profile redelivery, SENT terminality, journal redaction, production occurrence-ID compatibility, and current-host formatting/empty-host rejection — 5/5 PASS. The foreign-chat probe's PASS means the vulnerability was reproduced.

Complexity review: lean already. The commit uses the standard-library regexp and syscall facilities, adds no dependency, and introduces no speculative abstraction; the defects are missing identity strength and missing source binding, not excess machinery.

Weakest link: same-kernel cross-host transfer is proven from the compared fields but was not exercised on a second physical host. Upgrade that proof with two machines sharing `uname -srm` and different machine identities.

VERDICT: FAIL

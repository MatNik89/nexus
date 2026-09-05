# T27 verification round 3 — Kilo (narrow: confirm no NEW defect in d154153..HEAD)

Target: `slice/p0-phase7` at HEAD `6eab172` (folds `f5577ad` codex #1-3 + `82feed8`/`6eab172` causal REDs, on top of `d154153` agy #1-3). Kilo passed R2; this confirms the round-3 fold introduces no NEW defect in the kilo dimensions.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (acceptance 64s, no transient failure this run).

## codex #1 — seal-first ordering (verified)

`runDaemon` now performs adapter construction + live probes + `sealStartupSnapshot` + the `conversation`-OFF `return 1` **before** every effectful consumer (`main.go:132-188`); the heartbeat, startup sweep, scheduler, `resumeApprovedPending`, delivery loop and telegram `Run` all execute strictly after (`main.go:190+`, marked "EVERYTHING effectful below this line"). The provider probe is bounded (`provider.Probe` wraps 30s `context.WithTimeout`, `provider.go:231`), so a hung provider cannot hang startup. `TestSealedOffStartupRunsNoConsumers` (`acceptance_linux_test.go:902-952`) is causal black-box: a failing-provider incarnation with an overdue reminder and a wired channel delivers nothing, and a later healthy incarnation delivers it — proving no consumer advanced `DELIVERY_PENDING` under a sealed-OFF startup.

## codex #2 — signed attestation (verified, key-less forgery defended)

`verifyAcceptanceAttestation` (`main.go:835-899`) now requires `acceptance.json.sig` and verifies it cryptographically via `ssh-keygen -Y verify -f <config>/nexus/allowed_signers -I owner -n nexus-acceptance`, feeding the attestation JSON on stdin; `scripts/p0-accept.sh` signs with `NEXUS_RELEASE_KEY`. The chain is signature → signed JSON → `binary_sha256` → `os.Executable()` hash, so the grant binds to the running binary and cannot be forged without the owner key. The JSON is read twice (parse, then stdin to `ssh-keygen`); replacing it between the two reads makes the signature fail, so the TOCTOU is self-defeating. No command injection (args are a slice, no shell). The accepted ceiling (config-dir/owner-key write = owner) holds; no key-less path found. `TestDoctorP0GrantLive` carries no-attestation / unsigned / rogue-key / digest-mismatch branches.

## codex #3 — group routing (verified)

`telegramBindings` now refuses `chat <= 0` (`main.go:976-981`), rejecting Telegram groups/supergroups/channels (negative ids) so a reminder can never route to a group via the lowest-id rule. `TestTelegramBindingsStrict` covers negative/zero.

## agy folds (verified)

`ack` settles the SENT-proven receipt inline before `MarkAcked` (`TestAckSettlesSentReceiptInline`, both directions); `mustProbeCore` returns a cleanup closure that removes the doctor's temp dir; the dead `telegramProbeGate` is removed and its test exercises `sealStartupSnapshot` directly.

## NEW-defect hunt

None. The reorder is a strict superset of the prior behavior (the seal already exited 1 on `conversation`-OFF in R2; only the position moved, and no effectful step was moved above the seal). A group-chat binding now fails both the adapter construction (`berr` branch, adapter OFF) and the `ownerChat` resolution (`""`, no delivery loop), consistently fail-closed. No change touches the kilo-owned delivery honesty (SENT-gated receipt, occurrence-stable id) or exact-intent surfaces.

## Verdict

The three codex fixes are real and causal (each backed by a RED-capable test), the agy folds are correct, and no NEW defect was introduced in `d154153..HEAD`.

VERDICT: PASS

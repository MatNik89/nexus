# T27 Verification Round 3 — Codex

Target: `slice/p0-phase7` at `6eab172307c04a0a1155ae571533cc3026cddf42` (`f5577ad`, `82feed8`, and `6eab172` on `d154153`). The repository was not modified during verification except for this requested report. All probe and ablation changes were made in clean `git archive HEAD` exports.

## Findings

1. [HIGH] The signed acceptance artifact remains forgeable without the owner's private key because neither trust input is rooted.

   `verifyAcceptanceAttestation` loads the trusted public keys from the same user-writable configuration tree as the artifact (`cmd/nexus/main.go:878-885`) and invokes `ssh-keygen` by an unqualified PATH lookup (`cmd/nexus/main.go:892-896`). The committed rogue-key test signs with an untrusted key while leaving `allowed_signers` unchanged (`internal/acceptance/acceptance_linux_test.go:821-835`); it does not attack replacement of the trust anchor itself.

   Two independent clean-export negative probes reached `P0-capable` without the owner key:

   - `TestR3ProbeRogueKeyCannotReplaceTrustAnchor` generated a new key, replaced `<config>/nexus/allowed_signers` with that public key, signed the correct-digest JSON with the new key, and ran the real binary. Doctor exited 0 and printed `LIVE acceptance` and `P0-capable`.
   - `TestR3ProbePATHCannotReplaceSignatureVerifier` kept the legitimate `allowed_signers`, replaced the signature with invalid bytes, and invoked doctor with a PATH whose first `ssh-keygen` was a zero-exit shim. Doctor again exited 0 and granted `P0-capable`.

   Both are key-less forgeries and are outside the accepted “attacker holds the owner private key” ceiling. The acceptance public-key fingerprint must be rooted outside mutable acceptance state (for example, compiled/pinned or otherwise independently authenticated), and runtime verification must not trust an arbitrary PATH executable. The regression must replace the trust anchor and spoof the verifier, not merely present a wrong key to an intact anchor.

2. [MED] `TestSealedOffStartupRunsNoConsumers` is false-green for the pre-seal state-advance defect it claims to detect.

   Production ordering is fixed: the adapter probe, live probes, seal, and conversation-OFF return occupy `cmd/nexus/main.go:135-186`; heartbeat, startup sweep, scheduler, resume scan, reminder loop, Telegram run, and serving all occur afterward at `cmd/nexus/main.go:191-280`. My source-order probe passed, and my previous DELIVERY_PENDING probe now passed because an overdue reminder remained `SCHEDULED` after an all-failing provider caused exit 1.

   The committed detector at `internal/acceptance/acceptance_linux_test.go:902-951`, however, observes only whether Telegram delivered during the refused incarnation and whether a later healthy incarnation delivered. In a clean export I inserted only `b.sched.Sweep(ctx)` immediately before the seal, recreating the original forbidden state advance while leaving Telegram startup after the seal. `TestSealedOffStartupRunsNoConsumers` still passed (`exit 0`, 13.68s): the pre-seal sweep changed the reminder to `DELIVERY_PENDING`, no adapter ran during the refused incarnation, and the later healthy incarnation delivered it exactly as the test expected.

   The implementation fix is real, but its named detector is not causal for “no consumers.” It must assert the refused incarnation leaves the durable occurrence state unchanged, or add a pending approved-resume side-effect observable.

## Fold verification

1. Pre-seal consumers: **implementation fixed; named detector incomplete**. `TestSealedOffStartupRunsNoConsumers` passed at HEAD. Independent source-order and durable-state probes both passed. The controlled pre-seal-sweep ablation above remained green, producing Finding 2.
2. Signed attestation: **not complete**. No-attestation, unsigned, intact-anchor rogue-key, and digest-mismatch branches all pass at HEAD. Returning success immediately after the digest check made `TestDoctorP0GrantLive` fail at the unsigned branch, so the signature gate is causal for those tested cases. Finding 1 demonstrates two untested key-less bypasses.
3. Group routing: **fixed and causal**. `telegramBindings` rejects `chat <= 0` at `cmd/nexus/main.go:972-981`; `TestTelegramBindingsStrict` passes at HEAD. Removing only this guard made it fail on `-100123=private`.
4. Ack/receipt race: **fixed and causal**. The ack path settles only a durable `SENT` delivery before `MarkAcked` (`cmd/nexus/main.go:1076-1091`). `TestAckSettlesSentReceiptInline` passed in both directions; removing only the inline settlement made the SENT-proven case fail while the no-send refusal remained.
5. Doctor probe cleanup: **fixed**. `mustProbeCore` closes the journal and removes its private temporary directory on every constructed path (`cmd/nexus/main.go:901-921`). Running `TestDoctorP0GrantLive` under a private `TMPDIR` left that directory empty.
6. Dead `telegramProbeGate`: **removed**. No production definition remains; the current test directly exercises `sealStartupSnapshot` for healthy/dead channel states and token-safe reasons.

## Verification evidence

- `CGO_ENABLED=0 go test -count=1 ./...` in the repository: PASS for every package; `internal/acceptance` passed in 64.332s, `internal/preflight/probe` in 52.457s, and `internal/sandbox` in 18.668s.
- `CGO_ENABLED=0 go vet ./...`: PASS.
- Clean-export focused tests: `TestTelegramBindingsStrict`, `TestAckSettlesSentReceiptInline`, `TestDoctorP0GrantLive`, and `TestSealedOffStartupRunsNoConsumers`: PASS.
- Clean-export seal source-order probe: PASS.
- Clean-export sealed-OFF DELIVERY_PENDING probe: PASS; reminder remained `SCHEDULED` after exit 1.
- Pre-seal startup-sweep ablation: committed `TestSealedOffStartupRunsNoConsumers` incorrectly remained GREEN.
- Signature-gate ablation: `TestDoctorP0GrantLive` RED at the unsigned branch.
- Binding guard ablation: `TestTelegramBindingsStrict` RED on the negative group ID.
- Ack inline-settlement ablation: `TestAckSettlesSentReceiptInline` RED with `no durable delivery receipt`.
- `scripts/p0-accept.sh` in a clean export, with a real temporary Ed25519 owner key and destructive cleanup shimmed: PASS; acceptance package passed in 41.036s, installed binary SHA-256 matched the JSON, and `ssh-keygen -Y verify -n nexus-acceptance` accepted the emitted signature.
- One discarded script-probe setup used an excessively long custom `TMPDIR`, exceeding Unix-socket path limits and causing expected harness failures; it was rerun with normal `/tmp` and is not used as product evidence.

Topknot: the fold is otherwise small and reuses the existing OpenSSH signature mechanism; no diff-focused bloat finding.

Weakest remaining proof outside the blockers: the acceptance signature verifies binary identity but still does not bind or validate the recorded host field; this review did not promote that separate replay concern because the two direct key-less bypasses already invalidate the same provenance boundary.

Skill update: none.

VERDICT: FAIL

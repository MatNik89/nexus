# T27 Verification Round 2 — Codex

Target: `slice/p0-phase7` at `5fedd4e884c71bcc84371167c492edccc68f2fa1` (`7c68cda` + `5fedd4e`). The repository was not modified during review except for this report. All probe and ablation changes were made in clean `git archive HEAD` exports under `/tmp`.

## Findings

1. [HIGH] The capability seal still occurs after effectful consumers have started.

   `runDaemon` starts the heartbeat and scheduler, performs the startup sweep, starts the periodic scheduler, and calls `resumeApprovedPending` at `cmd/nexus/main.go:134-175`; the final snapshot is not built until `cmd/nexus/main.go:242-247`. Therefore a startup whose final snapshot has `obligations=OFF` can still advance obligations, and a pending approved resume can execute through the system effect path before any sealed capability decision exists. The later conversation refusal at `cmd/nexus/main.go:259-262` cannot undo those effects.

   Concrete clean-export probe: `TestR2ProbeSealedOffStartupCannotAdvanceReminder` seeded an overdue `SCHEDULED` reminder, made the provider startup probe return HTTP 503, and called the production `runDaemon`. The daemon printed `obligations OFF` and returned 1, but the reopened journal showed `DELIVERY_PENDING`; the probe failed with `sealed-OFF startup advanced reminder before seal: state=DELIVERY_PENDING`. A separate source-order probe failed with `consumer "go b.sched.Run" starts before capability seal (consumer=1294 seal=4671)`.

   The narrower dispatch guards are real: the recovering-provider test passes at HEAD and fails when the conversation refusal is removed; an independent recovering-Telegram probe passes at HEAD and fails when `snap.On("telegram")` is ignored. The unresolved defect is the pre-seal startup work, so fold claim #1 is incomplete.

2. [HIGH] `doctor --p0` cannot authenticate that the acceptance suite produced `acceptance.json`, so a consumer-disabled binary still self-grants.

   `scripts/p0-accept.sh:13-20` writes a plain JSON object containing a binary digest. `verifyAcceptanceAttestation` at `cmd/nexus/main.go:837-867` validates only attacker/user-writable fields and the current binary digest; it does not verify a signature, trusted producer, host binding, suite digest, or other provenance. The acceptance test itself demonstrates the bypass path by directly writing this JSON at `internal/acceptance/acceptance_linux_test.go:678-703` before expecting a grant at `internal/acceptance/acceptance_linux_test.go:781-784`.

   Two clean-export probes confirmed impact:

   - With the production reminder-consumer call ablated, `TestCriterion3ReminderDeliversAfterRestart` failed (`no outbound message containing "water the plants"`), while `TestDoctorP0GrantLive` still passed and granted the same binary.
   - `TestR2ProbeForgedAcceptanceJSONCannotGrant` wrote the correct digest JSON directly without running the suite. `doctor --p0` exited 0 and printed `LIVE acceptance` plus `P0-capable`, so the negative probe failed.

   The added absence/digest checks are causal: removing their result from the grant makes `TestDoctorP0GrantLive` fail at its no-attestation assertion. The supplied script also works when invoked correctly: in a clean export it ran the acceptance package successfully, installed the pinned binary, and the installed SHA-256 exactly matched the emitted attestation. Those facts do not supply provenance at doctor time. Fold claim #3 remains incomplete.

3. [MED] Deterministic `ownerChat` selection can route private reminder text to a Telegram group.

   `telegramBindings` accepts every signed `int64`, including negative group/supergroup-style IDs, at `cmd/nexus/main.go:926-950`. The new selection chooses the numerically lowest matching binding at `cmd/nexus/main.go:180-196`, so a configuration containing both `-100123=private` and `42=private` deterministically selects `chat--100123`. Outbound delivery parses and sends that identity without an outbound private-chat check (`internal/channel/telegram/telegram.go:270-279`); the inbound `chat.type == "private"` guard cannot protect reminders.

   Clean-export black-box probe `TestR2ProbeReminderNeverTargetsGroupBinding` configured those two bindings, created a reminder through the real binary, restarted the daemon, and captured the Bot API request. It failed with `private reminder routed to group-like chat id -100123`. A smaller parser probe also failed because `telegramBindings("-100123=private")` was accepted. This is a concrete confidentiality failure caused by the new lowest-ID rule unless bindings are restricted to private-chat IDs or outbound ownership is independently proven.

## Round-1 finding verification

1. Seal enforcement: **partially fixed, still failing**. Conversation OFF now exits 1, and Telegram `Run` is gated by the sealed snapshot; both guards are causal. Finding 1 shows that the seal is still not before every consumer.
2. Reminder receipt honesty: **fixed**. `deliveryIDFor` is occurrence-stable (`cmd/nexus/main.go:916-920`), `EnqueueReplyID` is PK-idempotent (`internal/channel/channel.go:383-410`), and `MarkDelivered` requires durable `SENT` (`cmd/nexus/main.go:899-910`). Neutralizing only the `st != "SENT"` predicate made `TestReminderReceiptOnlyFromSent` fail with `receipt minted without a SENT transition: DELIVERED`. An independent HTTP-500 probe remained `DELIVERY_PENDING` with exactly one `UNKNOWN` row and no retryable row.
3. Doctor grant honesty: **partially fixed, still failing**. Missing and mismatched digests withdraw the grant, and the guard is causal, but Finding 2 shows the attestation is forgeable and a disabled consumer can still be granted.
4. `telegram_api_base`: **fixed** for the claimed production-or-loopback policy. The production URL and literal loopback endpoints pass; `http://evil.example.com`, private-network addresses, invalid schemes, and malformed URLs fail in `ValidateBounds` (`internal/foundation/config/config.go:314-325`). Removing this block made `TestTelegramAPIBaseLoopbackOnly` fail on `http://evil.example.com`.
5. Acceptance artifact isolation: **fixed**. `TestMain` owns an `os.MkdirTemp` package-lifetime directory and honors `NEXUS_ACCEPT_BIN` (`internal/acceptance/acceptance_linux_test.go:38-62`). Two differently versioned clean exports were run concurrently under one shared `TMPDIR`; each executed its own expected binary and both passed, so cross-export replacement was not reproduced.
6. No-channel sensitivity: **fixed and causal**. The clean HEAD test passed after a channel-less incarnation and late wiring. Ablating the production reminder consumer made `TestCriterion3SensitivityNoChannel` fail at the late-delivery assertion (`sent=[]`).
7. Lowest owner chat: **deterministic but unsafe**. Map-order nondeterminism is removed; Finding 3 is a newly confirmed confidentiality gap in the selection domain.

## Verification evidence

- `CGO_ENABLED=0 go test -count=1 ./...` in the repository: PASS for all packages, including `internal/acceptance` (37.760s), `internal/preflight/probe` (38.489s), and `internal/sandbox` (13.093s).
- `CGO_ENABLED=0 go vet ./...`: PASS.
- Clean-export focused HEAD tests: `TestReminderReceiptOnlyFromSent`, `TestTelegramAPIBaseLoopbackOnly`, `TestCriterion3SensitivityNoChannel`, `TestDoctorP0GrantLive`, and `TestSealedOffCapabilityNeverServes`: PASS.
- Conversation-seal ablation: named acceptance test RED with a recovered provider reply.
- Telegram-seal ablation: independent probe RED with a recovered Telegram reply.
- SENT-gate ablation: named detector RED with a fabricated `DELIVERED` state.
- API-base validation ablation: named detector RED on `http://evil.example.com`.
- Acceptance-gate ablation: named doctor detector RED because no-attestation still granted.
- Disabled reminder consumer: criterion 3 RED, no-channel sensitivity RED, but doctor test GREEN (false grant).
- Independent HTTP-500 reminder probe: PASS; occurrence remained pending with one stable `UNKNOWN` outbox row.
- Shared-`TMPDIR`, two-revision concurrent probe: PASS for both distinct binary identities.
- Clean `scripts/p0-accept.sh` execution with temporary XDG/install paths: PASS; installed binary digest matched `acceptance.json`.

Weakest verified point outside the findings: the loopback exception trusts the configured local Bot API endpoint; this review proved host validation and rejection causality, not the behavior of an independently compromised loopback service.

Skill update: none.

VERDICT: FAIL

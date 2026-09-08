# Adversarial design review round 11 — audit-remediation plan v11
Reviewed revision: `cf3790d02cc8d1a09e8293ee866b34e9b4f1f641`
Verdict: FAIL

The committed header is `PLAN v11` (`docs/PLAN-AUDIT-FIXES.md:1`). This review is
bound to the revision above and is static design evidence only. The separate B1
implementation and in-progress B2 work are not acceptance evidence for this plan.

## Round-10 disposition

1. **CLOSED — former HIGH finding: durable command-registration identity did not bind
   the remote bot.** V11 defines the effect operation as
   `control:tg:<bot-id>:setMyCommands:<payload-hash>` and its target as
   `channel:tg:bot:<bot-id>:setMyCommands`; the hash covers the exact canonical wire
   payload and preserves command-array order (`docs/PLAN-AUDIT-FIXES.md:315-322`). The
   companion carries `adapter`, `bot_id`, `method`, and `payload_hash`, and the channel
   validator reconstructs the operation identity and rejects a foreign bot or hash
   (`docs/PLAN-AUDIT-FIXES.md:323-339`). D2c supplies the requested causal detector:
   same-bot restart makes zero effect calls, bot B on the same journal makes exactly one,
   bot-binding ablation turns RED, and a bot-A companion is refused for bot B
   (`docs/PLAN-AUDIT-FIXES.md:572-576`). This closes the exact round-10 counterexample.

   The current code confirms why this binding is necessary and realizable: the token is
   sealed into each adapter at construction (`internal/channel/telegram/telegram.go:75-106`),
   while the current startup probe only checks `is_bot` and retains no remote bot ID
   (`internal/channel/telegram/telegram.go:466-484`). V11 explicitly makes the governed
   `getMe` result supply that ID (`docs/PLAN-AUDIT-FIXES.md:320-321`).

2. **CLOSED — round-10 ordered-payload note.** V11 hashes the exact canonical wire
   payload and says the ordered command array must never be sorted away
   (`docs/PLAN-AUDIT-FIXES.md:315-319`). That matches the current wire input, whose
   command slice has deliberate source order (`internal/channel/telegram/telegram.go:450-463`).

3. **CLOSED AS TO THE FORMER WEAKEST LINK — effect identity omitted the remote
   resource.** Bot ID is now present in the operation, target, companion, validator, and
   restart detector (`docs/PLAN-AUDIT-FIXES.md:315-339,572-576`).

4. **STILL OPEN AS A PROOF CEILING — implementation/runtime evidence.** The audit
   findings remain unchecked in the reviewed plan (`docs/PLAN-AUDIT-FIXES.md:23-29`).
   Revision `cf3790d` therefore supplies no integrated B1/B2/D implementation,
   RED-to-GREEN ablation result, durable restart result, live Telegram result, release
   publication, or deployment evidence. This is not independently a verdict reason.

## Numbered new findings

1. **HIGH — reconciliation evidence is not bound to the bot whose UNKNOWN effect it
   resolves, so bot B can supply false proof for bot A.**

   Evidence: the registration effect identity is now correctly bot-specific
   (`docs/PLAN-AUDIT-FIXES.md:315-324`), but the reconciliation read remains the generic
   operation/target `control:tg:getMyCommands:<n>` / `channel:tg:getMyCommands`
   (`docs/PLAN-AUDIT-FIXES.md:300`). Recovery then reduces that read to an unbound boolean
   and calls `s7.Reconcile(op, equal, build)` (`docs/PLAN-AUDIT-FIXES.md:325-331`);
   `Reconcile` itself accepts only `op`, `ok`, and a companion builder, with no proof
   identity (`docs/PLAN-AUDIT-FIXES.md:211-220`). Nothing in the plan requires the bot ID
   embedded in the UNKNOWN `op` to equal the bot ID established by the current governed
   `getMe` before that boolean may resolve it.

   This matters on the uncovered rotation path: bot A's `setMyCommands` lands UNKNOWN;
   the daemon restarts with bot B's token; B happens to expose the same menu. A recovery
   implementation may use B's `getMyCommands` equality to mark A's operation SUCCEEDED,
   corrupting the durable effect record and violating E9's requirement that UNKNOWN exit
   only through reconciliation of that effect (`AGENTS.md:26`;
   `docs/ARCHITECTURE-ESSENTIALS.md:114-120`). D2c starts from bot A **SUCCEEDED**, not
   bot A UNKNOWN, so it cannot detect this proof substitution
   (`docs/PLAN-AUDIT-FIXES.md:572-576`). The current adapter's token is process-local and
   replaceable only by reconstruction (`internal/channel/telegram/telegram.go:82-106`),
   making this restart boundary reachable.

   **Concrete fix:** after the governed `getMe`, reconcile only an UNKNOWN registration
   whose embedded bot ID equals the current bot ID. Bind the reconciliation read itself
   to that resource, for example
   `control:tg:<bot-id>:getMyCommands:<n>` / `channel:tg:bot:<bot-id>:getMyCommands`, and
   have the channel owner verify a typed proof containing `bot_id` and the expected
   payload hash before reducing it to `ok` for S7. An UNKNOWN operation for an old bot
   remains UNKNOWN; the current bot begins its distinct effect operation. Add a detector:
   bot A lands UNKNOWN, restart the same journal with bot B and the same remote menu,
   assert A remains UNKNOWN and receives no `Reconcile`, while B receives exactly one
   governed `setMyCommands`; ablating only the proof-to-bot check must turn RED. A proof
   for bot A presented to bot B (and vice versa) must be refused before reconciliation.

## Notes (not verdict reasons)

- The supposedly complete call-site table still gives `setMyCommands` the pre-v11 generic
  operation and target (`docs/PLAN-AUDIT-FIXES.md:301`), and Slice D repeats the generic
  operation name (`docs/PLAN-AUDIT-FIXES.md:531-533`). The detailed owner rule and D2c are
  unambiguously bot-bound, so these are stale editorial rows under the repository's verdict
  rule (`AGENTS.md:81-87`); update them to prevent implementation drift.
- D2b still says that a rehydrated SUCCEEDED registration for the same “hash” makes zero
  calls (`docs/PLAN-AUDIT-FIXES.md:560-565`). It should say the same bot ID **and** payload
  hash. D2c supplies the missing behavioral distinction, so this is wording, not a second
  substantive finding.
- Topknot simplification pass: the v11 identity change itself is lean; no additional
  abstraction is needed beyond binding the existing reconciliation proof to the existing
  bot-specific identity.

## Weakest link and proof ceiling

The weakest link is the provenance of the boolean handed to `S7.Reconcile`: the effect key
names a bot, but the read grant and proof do not. This review does not establish buildability,
runtime state transitions, detector causality, remote API behavior, release-gate execution,
or deployment safety.

Skill update: none

VERDICT: FAIL

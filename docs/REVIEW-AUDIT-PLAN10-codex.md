# Adversarial design review round 10 — audit-remediation plan v10
Reviewed revision: `8c3120b1fa89ce074442d51964b66de060766874`
Verdict: FAIL

The committed plan header is `PLAN v10` (`docs/PLAN-AUDIT-FIXES.md:1`). This review is
bound to the revision above and is static design evidence only. The optional B1 worktree was
consulted only as a realizability cross-check; it is not the reviewed artifact and supplies no
implementation or acceptance evidence for this plan revision.

## Round-9 disposition

1. **CLOSED — former finding 1 (reconciliation had no realizable S7 lifecycle or atomic
   owner boundary).** V10 adds `S7.Reconcile`, legal only from UNKNOWN, with explicit
   present -> SUCCEEDED and proven-absent -> FAILED_RETRYABLE/FAILED outcomes. Only `Next`
   may issue the replacement grant, so the contradictory terminalize-and-re-Begin path is
   gone (`docs/PLAN-AUDIT-FIXES.md:210-219,319-326`). The new canonical
   `attempt.reconciled_retry` edge preserves the same operation's attempt/deadline budget,
   while `s7.operation_reconciled` and exactly one owner companion commit in one batch
   (`docs/PLAN-AUDIT-FIXES.md:210-218`). This is compatible with the current machine's
   reconciliation-only UNKNOWN exits (`internal/kernel/machine/machine.go:184-213`) and the
   current authority's refusal to let ordinary `Report` exit UNKNOWN
   (`internal/kernel/s7min/s7min.go:158-184`).

   V10 also assigns the channel owner its missing semantic checks: the
   `channel.control_effect` validator binds `operation_id` to `payload_hash`, rejects unknown
   method/state values, and relies on the journal's profile binding
   (`docs/PLAN-AUDIT-FIXES.md:327-332`). Extended detector D2b asserts S7 state beside wire
   counts, rejects a foreign hash, injects a torn-batch failure, and requires isolated
   reconciliation-transition and atomic-batch ablations to turn RED
   (`docs/PLAN-AUDIT-FIXES.md:553-564`). This closes the complete round-9 finding at the S7,
   journal, and sibling-owner boundaries.

2. **CLOSED — stale policy-constant enumeration note.** B1 now names the full closed set of
   Tool, Provider, Structured, Delivery, Poll, ControlRead, ControlEffect, and UI policies
   (`docs/PLAN-AUDIT-FIXES.md:139-147`).

3. **CLOSED AS TO THE FORMER WEAKEST LINK — reconciliation ownership and atomicity.** The
   S7 API, canonical edge, durable event, paired companion, owner validator, and causal
   ablations are now specified together (`docs/PLAN-AUDIT-FIXES.md:210-219,327-332,559-564`).

4. **STILL OPEN AS A PROOF CEILING — implementation/runtime evidence in the reviewed
   revision.** F1, F2, F3, F5, F6, F7, and F8 remain unchecked
   (`docs/PLAN-AUDIT-FIXES.md:28`). The separate `slice/audit-b` implementation is not part
   of revision `8c3120b`; no integrated implementation, cross-slice causal ablation, live
   Telegram run, release publication, or deployment evidence is established here. This is
   not independently a verdict reason.

## Numbered new findings

1. **HIGH — the durable command-registration identity binds the desired payload but not the
   remote bot, so success for one bot suppresses registration for a replacement bot.**

   Evidence: V10 defines the durable operation as
   `control:tg:setMyCommands:<sha256(desired set)>`, targets only
   `channel:tg:setMyCommands`, and records a companion containing the method and payload hash
   but no bot identity (`docs/PLAN-AUDIT-FIXES.md:295-300,314-318`). It then requires a
   rehydrated SUCCEEDED registration for the same payload hash to perform no call
   (`docs/PLAN-AUDIT-FIXES.md:323-327`). The actual adapter obtains its bot token afresh from
   the configured environment at construction (`internal/channel/telegram/telegram.go:75-106`),
   and the plan explicitly supports token repair followed by daemon restart
   (`docs/PLAN-AUDIT-FIXES.md:527-531`). Therefore the same profile journal can contain a
   SUCCEEDED registration for bot A, restart with a token for bot B, find the same command
   payload hash, and incorrectly skip `setMyCommands` for bot B. The current `getMe` probe
   verifies only that the token identifies some bot and does not retain that bot's ID as the
   durable target (`internal/channel/telegram/telegram.go:466-483`). This reuses proof across
   different remote resources and violates the SPEC requirement that each policy/grant bind
   a `target_id` (`docs/HARNESS-SPEC.md:1389-1390`). D2b tests restart with the same journal
   and payload but never rotates the bot identity, so all its assertions can pass with this
   cross-target false success (`docs/PLAN-AUDIT-FIXES.md:553-564`).

   Concrete fix: obtain the immutable Telegram bot ID from the governed `getMe` startup
   probe and include it in both the operation and target, for example
   `control:tg:<bot-id>:setMyCommands:<payload-hash>` and
   `channel:tg:bot:<bot-id>:setMyCommands`. Add `bot_id` to
   `channel.control_effect` and make the validator bind operation ID, bot ID, method, and the
   hash of the exact canonical wire payload. A different bot must create a different durable
   operation even when the desired commands are identical. Extend D2b with one combined
   detector: bot A succeeds; same-bot restart makes zero calls; restart from the same profile
   journal with bot B and the same command set makes exactly one governed `setMyCommands`
   call for B. Ablating only bot binding must turn it RED, and a bot-A companion presented
   for bot B must be refused before the wire.

## Notes (not verdict reasons)

- The plan hashes a “sorted command list” (`docs/PLAN-AUDIT-FIXES.md:314-316`). Because a
  Bot API command list is carried as an ordered array in current code
  (`internal/channel/telegram/telegram.go:450-463`), implementation should hash the exact
  canonical wire payload without sorting away any semantically visible list order. This is
  a clarification to the concrete fix above, not a second verdict reason.
- The weakest link is durable effect identity: v10 now preserves state and retry ownership
  correctly, but its evidence key still omits the remote resource to which that evidence
  applies.

## Proof ceiling

This review establishes only plan consistency against the committed owner documents and
current call graph. It does not establish integrated buildability, causal RED-to-GREEN
behavior, remote API semantics, durable restart behavior, release-gate execution, or
deployment safety.

Skill update: none

VERDICT: FAIL

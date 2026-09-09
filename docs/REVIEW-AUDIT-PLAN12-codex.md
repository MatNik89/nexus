# Adversarial design review round 12 — audit-remediation plan v12
Reviewed revision: `5358518796ca4f720b7f01b2bd91831aa10fdeb2`
Verdict: PASS

The committed header is `PLAN v12` (`docs/PLAN-AUDIT-FIXES.md:1`). This review is
bound to the revision above and is static design evidence only. The separate B1/B2
implementation worktree is not part of the reviewed artifact and supplies no acceptance
evidence here.

## Round-11 disposition

1. **CLOSED — former HIGH finding: reconciliation proof was not bound to the bot whose
   UNKNOWN effect it resolved.** The reconciliation read now carries the same bot ID in
   both its operation and target (`docs/PLAN-AUDIT-FIXES.md:302,327-330`). Recovery is
   explicitly limited to an UNKNOWN registration whose embedded bot ID equals the current
   bot ID returned by governed `getMe`; an old bot's UNKNOWN remains UNKNOWN while the
   current bot starts its own effect operation (`docs/PLAN-AUDIT-FIXES.md:330-334`). The
   channel owner verifies typed `ControlProof{BotID, PayloadHash, RemoteEqual}` against the
   effect operation's bot ID and payload hash before reducing it to the boolean accepted by
   S7 (`docs/PLAN-AUDIT-FIXES.md:334-339`). This keeps semantic proof validation in the
   channel owner while S7 remains the sole lifecycle/retry owner, consistent with S7's
   owner boundary (`AGENTS.md:19-22`; `docs/HARNESS-SPEC.md:1386-1393`).

   D2d exercises the previously uncovered path: bot A UNKNOWN, restart as bot B with an
   equal menu, A remains UNKNOWN, B performs exactly one governed effect, cross-bot proofs
   are refused, and ablating only proof-to-bot binding turns RED
   (`docs/PLAN-AUDIT-FIXES.md:586-591`). The current adapter seals its token at construction,
   so restart is the real bot-rotation boundary (`internal/channel/telegram/telegram.go:75-106`).

2. **CLOSED — stale call-site-table and Slice-D identity note.** The complete table now
   gives both `getMyCommands` and `setMyCommands` bot-bound operations and targets
   (`docs/PLAN-AUDIT-FIXES.md:298-305`), and Slice D uses the bot-bound durable
   `setMyCommands` identity (`docs/PLAN-AUDIT-FIXES.md:539-542`).

3. **STILL OPEN AS AN EDITORIAL NOTE — D2b's same-hash wording.** D2b still says that a
   rehydrated SUCCEEDED registration for the same “hash” makes zero calls
   (`docs/PLAN-AUDIT-FIXES.md:569-574`), and the owner paragraph has the same shorthand
   (`docs/PLAN-AUDIT-FIXES.md:339-341`). It should say the same bot ID **and** payload
   hash. The bot-bound operation, target, validator, D2c, and D2d already make the behavior
   unambiguous (`docs/PLAN-AUDIT-FIXES.md:317-347,581-591`), so under the repository's
   verdict rule this stale wording is not a substantive defect (`AGENTS.md:81-87`).

4. **CLOSED / NO ACTION — Topknot simplification note.** V12 adds one narrow typed proof
   at the existing channel/S7 seam and does not create a second retry owner or a general
   proof subsystem (`docs/PLAN-AUDIT-FIXES.md:327-339`). Lean already.

5. **STILL OPEN AS A PROOF CEILING — implementation/runtime evidence.** F1, F2, F3,
   F5, F6, F7, and F8 remain unchecked in this revision
   (`docs/PLAN-AUDIT-FIXES.md:25-31`). No integrated B1/B2/D build, causal ablation result,
   durable restart run, live Telegram result, release publication, or deployment is proven
   by this plan review. This is not independently a verdict reason.

## Numbered new findings

None. The v12 design closes the round-11 proof-substitution path at the channel owner,
preserves S7 as the only retry/reconciliation state owner, leaves an old bot's unresolved
effect honestly UNKNOWN, and adds a detector that can go RED under the isolated binding
ablation. I found no new substantive correctness, security, constitution, realizability,
owner-placement, detector, or cross-slice flaw.

## Notes and three weakest points (not verdict reasons)

1. The D2b “same hash” shorthand remains stale as described above. This is the only
   unclosed round-11 editorial item.
2. D2d should make the externally visible health expectation explicit: after bot B's own
   registration succeeds, current-bot health should recover even though bot A's historical
   operation remains UNKNOWN. The detector-section preamble says every detector checks
   external health (`docs/PLAN-AUDIT-FIXES.md:560-561`), but D2d does not spell out that
   particular expected value (`docs/PLAN-AUDIT-FIXES.md:586-591`). The owner design is not
   contradictory; this is a useful assertion-strengthening note for the B2/D integration.
3. The largest remaining proof gap is integration: current production still has one
   ungoverned `call` reaching `client.Do`, best-effort registration, and a `getMe` response
   that retains only `is_bot` (`internal/channel/telegram/telegram.go:193-206,428-484`).
   Those lines demonstrate that the planned detector can go RED, not that the in-progress
   implementation is GREEN.

## Proof ceiling

This review establishes plan consistency against the committed owner documents and current
call graph only. It does not establish buildability, runtime state transitions, detector
causality, exact wire hashing, remote Telegram semantics, release-gate execution, or
deployment safety.

Skill update: none

VERDICT: PASS

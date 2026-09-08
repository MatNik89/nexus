# Adversarial design review round 9 — audit-remediation plan v9
Reviewed revision: `b8e1ab72e23591d5d58ed494d69e57caf30698a5`
Verdict: FAIL

The committed plan header is `PLAN v9` (`docs/PLAN-AUDIT-FIXES.md:1`). This review is
bound to the revision above and is static design evidence only; it does not claim that the
unimplemented slices or their RED-to-GREEN detectors have run.

## Round-8 disposition

1. **CLOSED AS TO BLIND RESTART RESEND — former finding 1.** V9 makes command
   registration durable and keys it by the SHA-256 of the canonical desired command set. A
   rehydrated UNKNOWN registration first performs an S7-governed read-only
   `getMyCommands` comparison, while a rehydrated SUCCEEDED record for the same hash makes
   no remote call (`docs/PLAN-AUDIT-FIXES.md:301-314`). `getMyCommands` is included in the
   complete governed method table (`docs/PLAN-AUDIT-FIXES.md:283-289`). Detector D2b
   reconstructs the daemon and S7 from the same journal, requires zero pre-reconciliation
   `setMyCommands` calls, checks both equal and different remote menus, and includes a blind-
   restart ablation (`docs/PLAN-AUDIT-FIXES.md:532-538`). This closes the specific current
   restart path in which every new `Adapter.Run` unconditionally calls registration
   (`internal/channel/telegram/telegram.go:428-463`; `cmd/nexus/main.go:289-292`). The
   reconciliation's S7 lifecycle remains incomplete; see new finding 1.

2. **CLOSED — `PolicyUI` E9 wording note.** V9 now states that only definite UI failures
   are terminal and that 5xx, post-write, and malformed outcomes become UNKNOWN
   (`docs/PLAN-AUDIT-FIXES.md:288-289`). This aligns the shared UI policy used by
   `sendChatAction` and the picker mutation calls with E9
   (`internal/channel/telegram/telegram.go:345-347`;
   `internal/channel/telegram/telegram_picker.go:33-49,156-171`;
   `docs/ARCHITECTURE-ESSENTIALS.md:114-120`).

3. **CLOSED AS TO THE FORMER WEAKEST LINK — restart coverage.** D2b now crosses a real
   daemon/S7 reconstruction boundary and causally ablates the durable recovery guard
   (`docs/PLAN-AUDIT-FIXES.md:532-538`).

4. **STILL OPEN AS A PROOF CEILING — implementation/runtime evidence.** F1, F2, F3, F5,
   F6, F7, and F8 remain unchecked (`docs/PLAN-AUDIT-FIXES.md:26`). No implementation,
   causal ablation, remote Telegram run, restart proof, release publication, or deployment
   evidence exists at this design-review stage. This is not independently a verdict reason.

## Numbered new findings

1. **HIGH — the reconciliation is specified only in adapter prose and cannot legally issue
   the promised replacement effect through the stated S7 owner API.**

   Evidence: V9 says an equal remote menu reconciles the UNKNOWN operation to SUCCEEDED,
   while a different menu reconciles the same operation to FAILED and then **re-begins that
   same identity** when the desired hash is unchanged (`docs/PLAN-AUDIT-FIXES.md:301-313`).
   B1 simultaneously specifies that `Begin` is a strict no-op for every existing record,
   including rehydrated and terminal records, and that `Next` issues grants only from PENDING
   or FAILED_RETRYABLE (`docs/PLAN-AUDIT-FIXES.md:150-156`). UNKNOWN is parked and ordinary
   `Report` never retries it (`docs/PLAN-AUDIT-FIXES.md:173-187`). Therefore an UNKNOWN ->
   FAILED transition followed by `Begin(sameOp, ...)` cannot reach a grant: the old record
   still exists, `Begin` does nothing, and `Next` refuses FAILED. The current authority
   enforces the same separation—`Report` cannot exit UNKNOWN—and its regression requires a
   reconciliation owner (`internal/kernel/s7min/s7min.go:158-184`;
   `internal/kernel/s7min/s7min_test.go:183-199`).

   The canonical machine already supplies explicit UNKNOWN -> SUCCEEDED and UNKNOWN ->
   FAILED reconciliation events (`internal/kernel/machine/machine.go:184-213`), but V9 adds
   no `Reconcile` method to the S7 API, no durable S7 reconciliation event/projection rule,
   and no paired atomic recipe for that transition. B1's public APIs remain only
   Begin/Next/Consume/Report/Cancel/Execute, and its durable event list has no reconciliation
   event (`docs/PLAN-AUDIT-FIXES.md:150-187,201-230`). The newly introduced
   `channel.control_effect{operation_id, method, payload_hash, state}` companion is mentioned
   once, but unlike delivery companions it has no owner validator requiring the operation
   suffix to equal `payload_hash`, nor a detector for a foreign hash or a torn S7/channel
   reconciliation batch (`docs/PLAN-AUDIT-FIXES.md:241-248,301-310,408-423`). Since S7
   deliberately validates only companion key/cardinality and not channel semantics
   (`docs/PLAN-AUDIT-FIXES.md:161-172`), adapter code could append an unpaired or mismatched
   control record while D2b's wire-count assertions remain green.

   Concrete fix: add one explicit S7-owner reconciliation API, for example
   `Reconcile(op, proof, build)`, legal only from UNKNOWN. It must validate the typed proof,
   compute either SUCCEEDED or FAILED_RETRYABLE/FAILED, and commit the S7 event plus exactly
   one `channel.control_effect` companion in one `AppendBatch`. When `getMyCommands` proves
   the desired menu is absent, transition the **same operation** to FAILED_RETRYABLE (subject
   to its existing attempt/deadline budget) and let only `Next` issue the next grant; do not
   terminalize and call no-op `Begin`. Register the durable reconciliation event in the S7
   projection/rehydration contract. Make the channel validator enforce
   `operation_id == "control:tg:setMyCommands:" + payload_hash` and a closed method/state
   set. Extend D2b with S7-state assertions, a mismatched-hash refusal,
   an injected paired-batch failure, and ablations of both the reconciliation transition and
   its atomic batch.

## Notes (not verdict reasons)

- B1 still calls its list “Three policy CONSTANTS” although later owner text defines
  `PolicyPoll`, `PolicyControlRead`, `PolicyControlEffect`, `PolicyUI`, and
  `PolicyStructured` (`docs/PLAN-AUDIT-FIXES.md:137-144,283-320,343-345`). The later
  definitions are unambiguous; this is stale enumeration wording only.
- The weakest link is the new reconciliation state transition, not the remote comparison:
  the plan now proves what the adapter should observe but does not yet give the sole retry
  owner a realizable, atomic way to act on that proof.

## Proof ceiling

This review establishes only plan consistency against the committed owner documents and
current call graph. It does not establish buildability, causal RED-to-GREEN behavior, remote
API semantics, durable restart behavior, release-gate execution, or deployment safety.

Skill update: none

VERDICT: FAIL

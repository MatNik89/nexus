# Constitution review — `CLAUDE.md` and `AGENTS.md`

Scope: fidelity to the four named locked authorities, missing safeguards that can change implementation behavior, cross-file contradictions, and unsourced process constraints. Reviewed at `7ef9245ee9bcf5e6ad811a5b95e35ea72eebf3b0`.

1. **[CONTRADICTION] Both constitutions make a nonexistent file the mandatory and exclusive P0 task queue.**

   `CLAUDE.md:12-15,43-46` requires reading `docs/tasks-P0.md` and building one task from it; `AGENTS.md:41-43` calls it “the only task queue for P0.” At this HEAD, `docs/tasks-P0.md` does not exist. The authorities describe it only as future work: `HARDQ-CONSOLIDATED.md:31-38,181-184` says the order and RED list will be materialized there, and `ARCHITECTURE-ESSENTIALS.md:82-90` points forward to it. A compliant agent therefore cannot begin any P0 task, while an agent that invents a task violates the same rules.

   **Fix:** either add the ledger before making these constitutions binding, or state “once created; until then no P0 implementation is authorized” and keep architecture/doc work governed by an explicit user task.

2. **[FIDELITY] `GAPFIX-*` is incorrectly promoted to an owner-document class.**

   Both read orders say the slice owner may be `GAPFIX-*` (`CLAUDE.md:12-15`; `AGENTS.md:9-14`), but neither precedence list assigns GAPFIX a rank. The locked precedence in `ARCHITECTURE-ESSENTIALS.md:8-17` likewise does not make raw GAPFIX files authoritative. This is dangerous, not theoretical: the locked design already supersedes GAPFIX-kilo's direct SQLite `ObligationStore` write with EventJournal ownership (summarized at `ARCHITECTURE-ESSENTIALS.md:47-59`). A worker following a GAPFIX as “owner” can revive a rejected design.

   **Fix:** remove `GAPFIX-*` from the generic owner list. Permit a GAPFIX clause only when a higher-ranked current owner explicitly incorporates it, and say the higher-ranked incorporation—not the raw GAPFIX—is binding.

3. **[FIDELITY] The unknown-schema rule is globalized beyond its locked P0 scope.**

   `CLAUDE.md:27-28` says every unknown schema is rejected; `AGENTS.md:21` says fail closed on “anything unknown.” The locked rule is phase-specific: P0 rejects an unknown schema ID/version unprocessed, while the v2 migration layer preserves and quarantines it (`ARCHITECTURE-ESSENTIALS.md:140-143`; `HARNESS-SPEC.md:1381`). The constitutions erase that later behavior. AGENTS' “anything unknown” is broader still and can be misread to reject ordinary uncertainty rather than only closed protocol/policy discriminators.

   **Fix:** use the exact split: unknown enum/kind/capability is rejected; unknown schema is rejected unprocessed in P0 and quarantined by the v2 migration owner when that layer activates. Scope “unknown” to typed protocol, policy, and capability inputs.

4. **[OVERREACH] The repository-wide English-only policy is invented and broader than the locked architecture.**

   `CLAUDE.md:8-10` governs code, comments, docs, commits and every agent prompt; `AGENTS.md:6-8` governs everything an agent produces. None of PRD, HARDQ-CONSOLIDATED, Annex A, or Architecture Essentials locks a language policy—the named authorities themselves predominantly use Croatian. This rule can reject an otherwise correct user-requested Croatian document or review without any product or safety reason.

   **Fix:** either record English as a separate explicit user-approved repository convention, outside the architecture-fidelity claim, or narrow it to code/public identifiers while always allowing the user's requested output language.

5. **[OVERREACH] Mandatory three-agent Herdr review for every non-trivial deliverable has no source and is recursively non-terminating.**

   `CLAUDE.md:49-53` requires codex+kilo+agy review, moderator fold, and targeted re-verification before *every* non-trivial design, document, or code slice is done. The authorities record three-agent reviews as provenance (`ARCHITECTURE-ESSENTIALS.md:3-6`) and use such a process for the hard-questions merge (`HARDQ-CONSOLIDATED.md:1-6`); they never make it a universal gate. Under the literal rule, each non-trivial review file and fold is itself a non-trivial deliverable requiring another three-agent cycle, so “done” has no finite base case. It also imposes three external review passes on a one-person personal project without a risk trigger.

   **Fix:** make independent review risk-based and explicitly non-recursive: required once for locked-contract changes, security boundaries, migrations, irreversible effects, and release gates; optional for ordinary docs/local slices. Review artifacts produced by that gate are outputs of the same gate, not fresh gated deliverables.

6. **[OVERREACH] The constitutions invent a bundle of workflow rules not present in the locked sources.**

   Unsupported additions include observed RED-before-change for every non-trivial task and mandatory fresh temp fixtures (`CLAUDE.md:43-47`; `AGENTS.md:29-32`), absolute paths in every cross-agent instruction (`CLAUDE.md:53`; `AGENTS.md:33`), fixed review tags/last-line syntax plus suspicion of every zero-finding review (`AGENTS.md:34-39`), and docs-on-main/code-branch-per-slice workflow (`CLAUDE.md:60-62`). Annex A requires its RED tests to be red-capable and spec-anchored (`HARNESS-SPEC.md:1362-1367`), but it does not lock this universal development ceremony or Git topology. The fixed tag list already omits valid task-specific tags such as `[CONTRADICTION]` and `[OVERREACH]` requested for this review.

   **Fix:** retain source-backed requirements—red-capable spec-anchored detectors and no criterion weakening. Move Git, fixture, path, review-format, and test-first preferences to a separately approved workflow guide, with explicit task instructions taking precedence.

7. **[OVERREACH] Explicit user sign-off is required for changes the sources do not reserve to the user.**

   `CLAUDE.md:23` says any PR touching any listed hard rule needs explicit user sign-off. The locked precedence reserves product identity/scope decisions to the user (`ARCHITECTURE-ESSENTIALS.md:8-12`) and records specific user decisions such as bwrap; it does not require personal approval for every implementation or correctness fix involving retry, typing, journal serialization, or tests. This turns normal owner-preserving maintenance into an unnecessary authorization bottleneck.

   **Fix:** require explicit user approval only to change PRD product decisions, an expressly user-decided HARDQ resolution, or an accepted security/proof criterion. Implementation that preserves the invariant should use the normal task/review gate.

8. **[MISSING] Neither constitution locks exact-intent approval, although it is required by a P0 done criterion.**

   Both files say ASK is not ALLOW, but neither says approval must be exact-intent, expiring, single-use, and bound to the profile/target. The locked effect rule requires an approval hash over the exact effect (`ARCHITECTURE-ESSENTIALS.md:107-120`), and HARDQ C4 fixes the canonical fields to tool name, canonical args, target resource, `ProfileID`, and `(device,inode)` for destructive filesystem operations (`HARDQ-CONSOLIDATED.md:140-143`). PRD requires Telegram approval before irreversible action (`PRD.md:28,50`). A well-meaning worker can otherwise implement a reusable “approve this tool” boolean.

   **Fix:** add one shared hard rule: an ASK grant is exact-intent, expiring, single-use, profile-bound, and cannot widen kernel policy; any payload/target change invalidates it.

9. **[MISSING] Neither constitution states the UNKNOWN-effect recovery boundary or prohibition on blind retry.**

   “S7 is the only retry owner” (`CLAUDE.md:24-26`; `AGENTS.md:18-20`) does not say what S7 is allowed to retry. The locked rule is that an effectful call without a valid commit receipt becomes `UNKNOWN → RECONCILING`, never blind retry or direct READY (`ARCHITECTURE-ESSENTIALS.md:107-120`; `HARNESS-SPEC.md:1442-1449`). Omitting it allows the sole retry owner to duplicate an irreversible effect while still obeying both constitutions.

   **Fix:** add `IRREVERSIBLE/side-effect + UNKNOWN => RECONCILING; no automatic retry until reconciliation proves the prior outcome` to both hard-rule sections.

10. **[MISSING] The honest Telegram delivery boundary is absent from both files.**

    Nothing in either constitution prevents a future channel worker from claiming exactly-once remote delivery. The locked resolution says exactly-once local admission but at-least-once remote delivery; sent-but-unrecorded becomes UNKNOWN/RECONCILING (`ARCHITECTURE-ESSENTIALS.md:187-205`; `HARDQ-CONSOLIDATED.md:53-61`). This was a P0 blocker found specifically because local outbox idempotency cannot make Telegram transactional.

    **Fix:** add the exact boundary to the shared hard rules, together with persist-before-offset durable inbox and “never blind retry sent-but-unrecorded.”

11. **[MISSING] AGENTS omits channel-to-profile binding before admission, creating an unintended Claude/worker divergence.**

    CLAUDE correctly requires channel identity to bind before admission (`CLAUDE.md:34-35`); AGENTS requires non-null ProfileID from admission onward and one DB per profile but omits how a remote channel obtains that ProfileID (`AGENTS.md:24-25`). HARDQ B3 requires per-chat deny-default binding before admission and forbids a mutable current-profile lookup afterward (`HARDQ-CONSOLIDATED.md:63-73`; also `ARCHITECTURE-ESSENTIALS.md:180-184`). A worker implementing Telegram can therefore derive the profile after admission or from mutable process state while believing it complied.

    **Fix:** copy the pre-admission, deny-default channel binding and no-post-admission-global-lookup rule into AGENTS.

12. **[MISSING] The P0 static-capability boundary is not protected against well-meaning framework work.**

    Neither file says P0 has no runtime activation and must not implement `ActivationPlan`/`RollbackVault`/`ReconcilePrepare`. The locked resolution deliberately defers that machinery to the first dynamic consumer and allows only fail-closed Resolve plus a sealed startup snapshot (`ARCHITECTURE-ESSENTIALS.md:93-104`; `HARDQ-CONSOLIDATED.md:121-127`). Because both constitutions direct agents to DESIGN-* and call the big plan a reference, an agent opening DESIGN-S0 can reasonably implement the detailed Activator unless this scope lock is surfaced.

    **Fix:** add a P0 scope guard: static compiled capabilities, fail-closed Resolve, sealed startup snapshot, restart on config change; transactional Activator forbidden until an explicitly selected dynamic consumer.

VERDICT: FAIL

# FOLD VERIFICATION — commit `18aabde`

Scope was limited to the ADOPTED resolutions in `HARDQ-CONSOLIDATED.md` and the three designated fold targets. `tasks-P0.md` does not yet exist; only the detailed seven-step build order is therefore accepted as an explicit future defer.

1. **[NEW-ERROR] The new authority was not inserted into source precedence, so several folds lose to the stale documents they are meant to amend.**

   `ARCHITECTURE-ESSENTIALS.md:3-14` says “the full source always wins” and ranks Annex A and `DESIGN-*` above the plan, but does not place `HARDQ-CONSOLIDATED` anywhere. This is now operationally contradictory: E10 declares bwrap P0 at `ARCHITECTURE-ESSENTIALS.md:115-130`, while higher-ranked `DESIGN-S0-sandbox-codex.md:981,1236-1237` still defines native Landlock/seccomp as the Linux candidate and explicitly says there is no bwrap fallback; E14 removes Decay from P0 at `ARCHITECTURE-ESSENTIALS.md:161-176`, while higher-ranked Annex P0.13 still mandates Decay at `HARNESS-SPEC.md:1570-1573`. An implementer following the cheat-sheet's own precedence can legally undo the user-approved fold.

   **Required correction:** put user-approved `HARDQ-CONSOLIDATED` resolutions immediately after PRD in precedence and explicitly mark the exact superseded clauses in DESIGN-S0, DESIGN-STATUS, HARNESS-PLAN and Annex A as stale, or amend those owners directly. A cross-reference in a lower-ranked summary is not a binding override.

2. **[NEW-ERROR] P1.6 is an appended exception beside still-unqualified contradictory MUST clauses.**

   The old owner still says `channels requires extensions+7.3+approval-core` (`HARNESS-SPEC.md:1461`), the manifest still requires S11.5/S6.8 fields for every `ChannelAdapterManifest` (`:1462`), and the invariant still says no “channel adapter” registers without active extensions (`:1465`). The amendment at `:1467` says built-ins require only delivery, approval and identity and that only `channel:plugin` requires extensions. “AMENDMENT” signals intent, but the normative contract now contains both commands. The RED prose at `:1466` also still activates generic `channels`; only the appended sentence reinterprets it as the plugin path.

   **Required correction:** rewrite, rather than append to, the owner/manifest/invariant/RED clauses: define a common adapter contract, a built-in descriptor without extension-only fields, a plugin manifest with S6.8/S11.5 fields, and make RED case A literally activate `channel:plugin`. Keep replay case B common to both paths. This preserves the plugin-path RED instead of merely annotating it.

3. **[UNFOLDED] B8 is contradicted by both Annex A and SECTION-MAP.**

   The adopted resolution moves Decay+Audn out of P0 and exempts explicit facts (`HARDQ-CONSOLIDATED.md:112-119`), and E14 reflects that correctly (`ARCHITECTURE-ESSENTIALS.md:161-176`). But Annex still labels P0.13 “Decay” and mandates hot/warm/cold behavior (`HARNESS-SPEC.md:1570-1573`), while SECTION-MAP still says `Decay+Audn(P0)` twice (`SECTION-MAP.md:39,110`). Because Annex outranks E14 under the current header, this is material, not cosmetic.

   **Required correction:** relabel P0.13 as a conditional `memory-decay`/P1 contract (preserving its no-byte-loss invariant when activated), and change both SECTION-MAP occurrences to explicit-facts P0 / Decay+Audn P1.

4. **[UNFOLDED] C6 did not reach the Annex contracts whose activation triggers it was meant to change.**

   C6 says P0.8 gates `local-inference`, while P0.9/P0.11 gate `coding` or user-workspace mutation (`HARDQ-CONSOLIDATED.md:147-149`). Commit `18aabde` changes only the P1.6 line in HARNESS-SPEC; P0.8, P0.9 and P0.11 remain unconditional “P0” contracts at `HARNESS-SPEC.md:1555-1568`. E4 also still calls workspace mutation P0.11 at `ARCHITECTURE-ESSENTIALS.md:53-59` without its conditional trigger.

   **Required correction:** add an explicit activation/scope line to each of P0.8/P0.9/P0.11 and to the Annex index, while retaining the contract IDs. P0.11 may gate only `AtomicWriter` for a P0 file mutation; shadow checkpoint/rollback gates coding or user-workspace mutation.

5. **[UNFOLDED] C7's P0 reject-unknown rule conflicts with E11's unchanged quarantine rule.**

   C7 adopts `schema_version + reject-unknown` for P0 and defers the upcast chain plus quarantine sink (`HARDQ-CONSOLIDATED.md:150-151`). The additions list says the upcast chain is deferred (`ARCHITECTURE-ESSENTIALS.md:224-225`), but E11 still states “unknown schema version → QUARANTINE” (`ARCHITECTURE-ESSENTIALS.md:132-134`). Annex P0.1 still mandates monotone upcasting and quarantine (`HARNESS-SPEC.md:1381-1383`).

   **Required correction:** split the contract explicitly: P0 rejects an unknown ID/version without processing it; v2 migration support preserves raw input and quarantines it for later upcast. Remove quarantine/upcast from unconditional P0 language.

6. **[UNFOLDED] C3 is absent from every fold target.**

   C3 requires identical-argument stuck detection only within one interactive turn and exempts scheduled/polling iterations through an explicit continuous-loop policy (`HARDQ-CONSOLIDATED.md:138-139`). None of `ARCHITECTURE-ESSENTIALS.md`, the `HARNESS-SPEC.md` amendment, or the SECTION-MAP -min note records this. The untouched plan continues to describe generic same-tool/argument stagnation detection at `HARNESS-PLAN.md:254-259`, so a reminder poll can still trip the interactive loop breaker.

   **Required correction:** add the scope invariant to the P0 additions/effect-loop decision and make it a task-level RED: repeated polling occurrences do not increment an interactive-turn stagnation counter.

7. **[UNFOLDED] C8 records what is deferred but omits the positive P0 health contract.**

   The adopted decision is two-sided: P0 gets a liveness heartbeat plus `last-occurrence-fired` counter, while SemanticHealth is deferred (`HARDQ-CONSOLIDATED.md:152-153`). `ARCHITECTURE-ESSENTIALS.md:224-225` records only the deferral. No changed target records the two minimal signals.

   **Required correction:** add the two P0 health signals to the P0 additions and later materialize their checks in `tasks-P0.md`; do not pull the SemanticHealth evaluator or metric backend forward.

8. **[UNFOLDED] F1 was narrowed to the sandbox prerequisite and lost three first-start gates.**

   F1 requires `nexus doctor` to check OS/kernel floor, bwrap, data-directory permissions, provider key and Telegram token, with consented install or exact instructions (`HARDQ-CONSOLIDATED.md:165-172`). E10 records bwrap/kernel and consented installation (`ARCHITECTURE-ESSENTIALS.md:125-130`), while the P0 additions only name “doctor preflight” (`:218-225`). Searches of all three fold targets find no data-dir, provider-key or Telegram-token doctor contract. A doctor can therefore report green while conversation or Telegram is unusable.

   **Required correction:** list all five checks and define capability-scoped results: missing provider key disables conversation; missing Telegram token disables Telegram; unsafe/unwritable data directory blocks stateful startup; missing bwrap disables exec. Overall `P0-capable` requires all six PRD done criteria, not merely sandbox readiness.

9. **[UNFOLDED] B7's owner summary omits the transaction boundaries that make the resolution effective.**

   `ARCHITECTURE-ESSENTIALS.md:218-225` correctly names a single append actor, synchronous core projections and daemon-owned DB/UDS, but drops B7's WAL + bounded `busy_timeout` and its three required `BEGIN IMMEDIATE` recipes: inbox-admission+journal, occurrence+run-admission, terminal-result+outbox (`HARDQ-CONSOLIDATED.md:99-110`). These are design boundaries, not merely build-order detail; without them the named actor does not prove crash atomicity across the P0 ingress/scheduler/delivery seams.

   **Required correction:** add the physical transaction rule to E4 or the P0 additions. `tasks-P0.md` may hold the implementation sequence and RED cases, but not invent the atomic ownership contract later.

10. **[OK] D1 preserves the two sandbox invariants explicitly called out by this verification.**

    E10 says bwrap is the P0 ENFORCED backend behind `Probe/Compile/Launch/Attest`, remains fail-closed, and is gated by the SAME hostile suite (`ARCHITECTURE-ESSENTIALS.md:115-129`). Absence yields exec OFF/conversation-only, not a weaker enabled backend (`:127-129`). The runtime-closure restrictions from B4 and host/kernel preflight from C5 are also present (`:121-130`). This fold does not weaken the hostile suite or fail-closed behavior. The precedence defect in finding 1 still prevents this otherwise-correct wording from being reliably binding.

11. **[OK] B2 does not regress from at-least-once remote delivery to an exactly-once claim.**

    E15 precisely limits exactly-once to local admission and states at-least-once remote delivery; `sent-but-unrecorded` goes to `UNKNOWN → RECONCILING`, never blind retry (`ARCHITECTURE-ESSENTIALS.md:178-187`). The durable inbox state and persist-before-offset rule are also retained (`:180-183`).

12. **[OK] A2 and the remaining principal P0 folds are represented without a detected distortion.**

    A2's named `s7-min`, `s5-min`, `3.6-min`, and `7.3-min` cuts appear in both E6 (`ARCHITECTURE-ESSENTIALS.md:70-84`) and SECTION-MAP (`SECTION-MAP.md:62-68`); deferring the seven-step task sequence to the future `tasks-P0.md` is appropriate. B1/B3/B4/B5/B6/B9 and C1/C2/C4/C5 are materially present across E7, E10, E14, E15 and the P0 additions (`ARCHITECTURE-ESSENTIALS.md:86-130,161-195,218-230`). No additional distortion was found in those folds beyond the precedence and completeness findings above.

VERDICT: FAIL

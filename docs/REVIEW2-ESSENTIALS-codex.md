# ARCHITECTURE-ESSENTIALS round-2 re-review (Codex)

Scope: verify every material round-1 finding from the Codex, Kilo, and Agy reviews against rewrite `70dd66e`, then sweep the English rewrite for new mistranslations, distortions, and bad references. `HARNESS-SPEC.md` is treated as normative as instructed by the round-2 brief. `[OK]` means correctly folded or correctly rejected with source-backed reasoning; it does not mean the whole document passes.

1. **[OK] The false “locked / 0 blockers” readiness claim was removed.** Essentials:3-6 now says “current design,” lists open preconditions, and explicitly denies that go/no-go was granted. This correctly folds Codex #1 and Kilo #1/#7. `HARNESS-SPEC.md:3-6` still says `U PREGOVORU` and that the verification matrix is next, so the weaker wording is faithful.

2. **[OK] The stale Annex-A blocker was removed and the stale-source conflict was explained.** Essentials:8-11 and :180 explicitly say P0.5-P0.13 now exist and that PLAN-HOLES C3 predates them. This correctly folds Codex #14 and Agy C1. `HARNESS-SPEC.md:1534-1572` contains the seven contracts; `SECTION-MAP.md:93` and `DESIGN-STATUS.md:9` mark the gap resolved.

3. **[UNFOLDED] The open PRD/identity decision was not resolved; it was relabelled “approved” without evidence.** Essentials:24-31 says “assistant FIRST (decided)” and “Decided in PRD (approved),” but `PRD.md:64-66` still says “Ovaj PRD čeka tvoju potvrdu/korekciju,” while `DESIGN-STATUS.md:13,37-41` still marks #6 `OPEN — TI VODIŠ` and one of two pre-code items. No rejection reason or approval record is present in the target, sources, or rewrite commit. This fails Codex #2 and also makes Essentials:175-180 omit a still-open source precondition.

4. **[OK] EventJournal ownership is now correctly scoped.** Essentials:40-46 limits the sole-writer claim to canonical domain events and derived state, while naming workspace/memory/backup writes as separate side-effects. This correctly folds Codex #3 and matches `HARNESS-SPEC.md:1394-1402` plus `DESIGN-FIXES-r2.md:92-93`.

5. **[NEW-ERROR] The atomic-write fold overgeneralizes S5/P0.11 to every workspace, memory-spine, and backup write.** Essentials:45-48 says each such write uses `AtomicWriter(tmp → fsync → rename)` and takes a snapshot before every FS effect. Normative `HARNESS-SPEC.md:1564-1567` assigns P0.11 only to S5.1/S5.3 and promises old-or-new/rollback semantics for the supported checkpoint scope; `HARNESS-PLAN.md:340-346` defines `AtomicWriter` for file mutation. Neither source requires replacing each SQLite-WAL transaction or backup write with rename, and doing so is not the SQLite spine protocol. Agy M2 was therefore folded too broadly. Limit this rule to supported file/workspace mutations; persistence engines must provide their own durable atomicity contract.

6. **[UNFOLDED] The missing build-order decision was added, but the chosen order contradicts the declared normative source.** Essentials:61-67 calls SECTION-MAP's `K0 → K1 → L → M-P` order normative. Yet `HARNESS-SPEC.md:1020-1056` normatively specifies a different vertical `P0 → P6` sequence: the minimal P1 slice includes provider/core-loop/tool/sandbox before P2 S7 and P3 S5, whereas SECTION-MAP puts S7 and S5 in K1 before provider/core-loop/tools. The target's own precedence at :8-11 puts HARNESS-SPEC above SECTION-MAP. This is not a renaming; the dependency edges differ. Codex #4 is not correctly folded until one DAG is made normative or the two are reconciled explicitly.

7. **[OK] The missing capability activation lifecycle is materially present.** Essentials:69-75 includes immutable `ActivationPlan`/PlanHash, Prepare/Commit/Activate, `RollbackToken`, explicit failure states, same-config attestations, capability-floor invalidation, and the ablation RED. This folds Codex #5, Kilo #8, and Agy M1 and is supported by `DESIGN-STATUS.md:9`, `HARNESS-SPEC.md:1404-1413,1539-1542`, and `HARNESS-PLAN.md:119-125`.

8. **[NEW-ERROR] E7 calls a three-step lifecycle “Two-phase” without defining two phases.** Essentials:69-72 names `Prepare → Commit → Activate`; `DESIGN-STATUS.md:9` uses the same three operations and does not call them two-phase. “Two-phase” was reviewer shorthand, not a source-backed contract. Rename the heading to “transactional capability activation” or explicitly define which operations form each phase; otherwise a K0 implementer receives two incompatible state-machine shapes.

9. **[OK] The egress boundary is no longer reduced to string filtering.** Essentials:103-109 now includes resolver+dialer IP pinning, DNS-rebinding/multi-A handling, redirect re-check, proxy-env sanitization, child-socket containment, and an egress receipt. This correctly folds Codex #6 and matches `PLAN-HOLES-CONSOLIDATED.md:90-93`.

10. **[OK] Delivery retry ownership is repaired.** Essentials:141-147 assigns idempotency keys/receipts to the gateway and authorization/scheduling of every delivery retry to S7. This correctly folds Codex #7 and preserves `HARNESS-SPEC.md:1385-1392` and `HARNESS-PLAN.md:433-437`.

11. **[OK] HMAC, Ed25519, and pinned hashes are now correctly separated.** Essentials:110-112 identifies HMAC as a keyed MAC, Ed25519 as a digital signature, and the skill lock as a plain pinned hash. This correctly folds Codex #8 and matches `DESIGN-STATUS.md:31-35` plus `HARNESS-PLAN.md:561-565`.

12. **[OK] “Not in P0” is now separated from “out of v1.”** Essentials:164-173 uses the PRD §5 list for out-of-v1 items and a distinct P1/P2/decision-pending list for P0 cuts. This folds Codex #9 and Kilo's scope criticism and matches `PRD.md:32-44` and `PLAN-HOLES-CONSOLIDATED.md:64-69`.

13. **[OK] The minimal checker is separated from the P1 coding trio.** Essentials:122-130 makes S16.6-det a K0/P0 dependency for obligations and leaves full TIA/symedit/deep coding evidence for P1. This correctly folds Codex #10 and Kilo #9. Sources: `SECTION-MAP.md:23-25`, `HARNESS-PLAN.md:502-504,728-734`.

14. **[OK] The P0 done-contract is now explicit.** Essentials:164-169 binds P0 to PRD §6 and states the restart, durable reminder, approval, profile non-leakage, and hostile sandbox boundaries; conversation is already named in the six-capability list. This folds Codex #11 and matches `PRD.md:46-53`.

15. **[OK] The provider material was compressed without deleting its P0-critical boundary.** Essentials:156-162 removes the four-auth-mode taxonomy from the 15 slots, preserves the provider interface/no-self-retry structured-output path, restores `DataDescriptor`, and labels SubscriptionOAuth cut from P0. This folds Codex #12 and Kilo #3/#5. `HARNESS-PLAN.md:186-209` supports the interface and flow; `PLAN-HOLES-CONSOLIDATED.md:64-69` supports the P0 cut.

16. **[OK] The static-linking overclaim was corrected.** Essentials:17-21 says static linking is a goal verified by CI, not an inference from `CGO_ENABLED=0`. This correctly folds Codex #15; `HARNESS-PLAN.md:13-16,750-751` only promises the pure-Go/single-binary floor.

17. **[OK] The no-autonomous-self-modification invariant was added.** Essentials:24-31 gives the opt-in RepairBundle → human/Claude → git upgrade path. This folds Kilo #10 and matches `PRD.md:39-44` and `HARNESS-PLAN.md:892-909`.

18. **[OK] Provenance laundering is now attributed to P0.1, not P0.3.** Essentials:115-120 names P0.1 and the correct test. This folds Agy F1 and matches the normative wiring and RED at `HARNESS-SPEC.md:1335-1338,1371-1383`.

19. **[OK] The bad PRD sandbox citation was fixed.** Essentials:101 cites “PRD §6 item 6, §7,” exactly matching `PRD.md:46-57`. This folds Kilo #2.

20. **[OK] The CodingProfile source mismatch and USP-selection asymmetry were repaired.** Essentials:24-31 explicitly says C7 is corrected by A8 and lists TIA/evidence-gate/AST-symedit; Essentials:122-130 exposes all three and their P1 status. This folds Kilo #4/#9 and correctly explains why shadow-git is not in the USP trio (`HARNESS-PLAN.md:1061-1072`).

21. **[OK] The P0 Telegram versus P1.6 extensions conflict is not silently papered over.** Essentials:148-152 and :175-180 preserve it as an explicit hard-questions blocker rather than claiming a false resolution. That is a valid fold of Agy C2: normative `HARNESS-SPEC.md:1459-1466` makes every channel adapter require extensions, while `PRD.md:24-30` requires Telegram in P0 and `SECTION-MAP.md:47-53` distinguishes a Phase-O stdio path from Phase-P plugin channels. The target correctly says this needs a design decision.

22. **[OK] The lower-priority E9/E15 selection concern was handled by consolidation.** The typed-contract rule remains E3 because it is a kernel API invariant; the crypto footgun was compressed into the broader fail-closed E11, freeing slots for build order and activation. This is a reasoned fold of Agy N1 and Kilo N1 rather than a blind deletion.

23. **[OK] The A9 citation is now specific enough to reject the low-severity “wrong addendum” objection.** Essentials:22 cites “A9 component strategy.” `HARNESS-PLAN.md:1114-1124` actually defines the component/adapter strategy, including pure-Go SQLite and adapter-time component choices, while PLAN-HOLES C4 supplies the non-Go sidecar/pure-Go/descope trilemma. This is source-backed; Agy F2's claim that A9 only concerns orchestration relies on SECTION-MAP's abbreviated A9 row, not the full A9 text.

24. **[NEW-ERROR] The new source-precedence rule is incomplete and ambiguous, so it cannot resolve the conflicts the rewrite relies on.** Essentials:8-11 omits both PRD and DESIGN-STATUS, even though E2 and the open-preconditions section depend on them. It also says “HARNESS-SPEC.md (normative Annex A),” leaving unclear whether only Annex A or the entire file wins. The round-2 brief says HARNESS-SPEC is normative; its `HARNESS-SPEC.md:1020-1056` build order then defeats E6, while its stale `HARNESS-SPEC.md:350-353,755-757,778` salvage references conflict with the greenfield `HARNESS-PLAN.md:3-5`. A usable precedence rule must name PRD and DESIGN-STATUS, scope HARNESS-SPEC normativity precisely, and state how later superseding design/status records beat stale body text.

25. **[NEW-ERROR] The sandbox rewrite loses the source's exact `execveat` boundary and overstates the probe's stop scope.** Essentials:93-101 says “irreversible execve” and blocks “production exec/scaffold code.” `DESIGN-STATUS.md:10` specifies `execveat` (material to the no-pre-exec/TOCTOU design), while `PLAN-HOLES-CONSOLIDATED.md:104-113` blocks S6.2 and arbitrary-exec code before the feasibility probe, not unrelated P0 scaffold work. Use `execveat` and say “before production S6.2/arbitrary-exec implementation.” The Win/mac deferral also cannot be called settled while the PRD approval remains open (`DESIGN-STATUS.md:37-39`).

## Verdict rationale

Most round-1 mechanism-level findings were folded correctly. The rewrite still fails because one central round-1 status finding is unsupported rather than resolved (PRD approval), the added build-order decision contradicts the declared normative source, and the rewrite introduces overbroad persistence and ambiguous activation/precedence rules. Those are implementation-directing errors, not editorial nits.

VERDICT: FAIL

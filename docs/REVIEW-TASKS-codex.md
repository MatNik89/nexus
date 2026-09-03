# P0 task-ledger review

Target: `docs/tasks-P0.md` at repository HEAD `c23bfe714b50a7b404e8b6c3e1d30878c7655875`. Review axes: source trace, dependency order, RED quality, and reviewable slice size.

1. **[TRACE] The binding `s5-min = AtomicWriter` cut has no implementation task.**

   `docs/tasks-P0.md:121-126` materializes `s7-min`, but no task owns `AtomicWriter`; the only S5 mention is the full-engine deferral at `docs/tasks-P0.md:259-260`. HARDQ A2 explicitly pulls `s5-min = AtomicWriter only` into P0 (`docs/HARDQ-CONSOLIDATED.md:22-30`), and Essentials repeats it as a P0 dependency and defines tmp → fsync → rename semantics (`docs/ARCHITECTURE-ESSENTIALS.md:57-60,86-90`). This lets the ledger reach P0 DONE without the adopted P0.11 atomic-file contract. Add a K0 `s5-min` task with `test_atomic_write_no_partial`; do not pull in rollback, shadow-git, or worktrees.

2. **[TRACE] HARDQ C8's two P0 health signals are lost.**

   The ledger defers SemanticHealth at `docs/tasks-P0.md:259-261`, which is correct, but no task implements the separately adopted P0 liveness heartbeat and last-occurrence-fired counter. HARDQ makes that split explicit (`docs/HARDQ-CONSOLIDATED.md:150-153`), as does Essentials (`docs/ARCHITECTURE-ESSENTIALS.md:227-242`). Add only those two positive signals to the daemon/scheduler closure; keep the evaluator and circuit-breaker machinery deferred.

3. **[TRACE] T20 folds only half of HARDQ A1: built-in registration is present, release-signature attestation is not.**

   T20 says `channel:builtin` and correctly bypasses the extensions closure (`docs/tasks-P0.md:201-208`), but no task compiles/attests the adapter as part of a signed release artifact. A1 requires the P0 adapter to be compiled into the signed binary and its integrity attested by the release artifact signature (`docs/HARDQ-CONSOLIDATED.md:12-20`). Add the minimum signing/verification acceptance to T24 or an explicit packaging task; do not import the plugin-path S6.8/S11.5 closure.

4. **[TRACE] T17 smuggles the full `DATA_PURGE` contract from P2 into P0.**

   T17 includes irreversible purge, tombstone deletion, VACUUM handling, and a purge RED while citing Annex P2.1 (`docs/tasks-P0.md:164-174`). The normative contract is explicitly P2 and requires per-store status, backup disposition, post-delete probes, and partial/legal-hold states (`docs/HARNESS-SPEC.md:1471-1478`); PRD's six P0 criteria do not require purge (`docs/PRD.md:46-53`). The task therefore both expands P0 and implements an unsafe subset of the cited contract. Remove `DATA_PURGE` from P0 and defer it explicitly; retain `MEMORY_FORGET` only if its P0 authority is made explicit.

5. **[ORDER] T03 can falsely publish `P0-capable` before any P0 capability exists.**

   T03 equates five green prerequisite checks with `P0-capable` (`docs/tasks-P0.md:37-45`), although conversation, memory, reminders, Telegram, profiles, and sandbox execution are built in T13-T23. Essentials defines `P0-capable` as all six PRD §6 criteria being reachable, not merely dependencies being installed (`docs/ARCHITECTURE-ESSENTIALS.md:235-240`). At T03 report `prerequisites-ready` plus capability-specific OFF reasons; reserve `P0-capable` for T24 after the six live checks pass.

6. **[ORDER] Required K0 seams are absent or land inside their consumer.**

   SECTION-MAP places `S8.1-min` (`ContextBudget.Measure+HardLimit`) and `S11.1-min` (`Assembler.Base`) in K0 and makes S3 depend on them (`docs/SECTION-MAP.md:20-24,32-35`); HARDQ A2 also requires deterministic clock/ID seams in K0 (`docs/HARDQ-CONSOLIDATED.md:31-34`). The ledger has no context-budget task, introduces prompt assembly only inside T14, and introduces injected clocks only in T18 (`docs/tasks-P0.md:136-143,178-187`). Add explicit K0 minimum contracts before T11/T14; otherwise early replay, deadlines, prompt bounds, and loop tests will invent incompatible local seams.

7. **[ORDER] T09's acceptance depends on provider and channel probe implementations built later.**

   T09 requires a sealed snapshot from live sandbox/provider/channel probes and “exactly the compiled P0 capabilities” (`docs/tasks-P0.md:91-98`), but provider and Telegram arrive in T13 and T20. No later task explicitly registers those probes and re-runs snapshot validation. Narrow T09 to resolver/snapshot behavior using contract-valid fakes, then make T13/T20 register their live probes and make T24 verify the final sealed snapshot.

8. **[ORDER] T11 is impossible to complete under its own acceptance rule.**

   T11 says all eight DESIGN-FIXES REDs must be GREEN (`docs/tasks-P0.md:110-119`) while declaring `TestReadOnlyProcessStillSandboxed` stub-skipped until T23. The ledger's global DONE rule requires the task's RED list GREEN (`docs/tasks-P0.md:3-8`); skipped is not GREEN. Move that RED and its acceptance ownership to T23, or split T11 into a contract-only test with a fake sandbox and retain one real-backend integration RED at T23.

9. **[TRACE] HARDQ B3's cross-profile causal-chain proof is truncated to memory and replay.**

   B3 requires immutable `ProfileID` through obligation, occurrence, inbox/outbox, approval, delivery, and restart/replay, with none crossing profiles (`docs/HARDQ-CONSOLIDATED.md:63-73`). T16 tests memory/FTS and replay only (`docs/tasks-P0.md:154-162`); T19-T21 add the later consumers but no cross-profile delivery or approval test (`docs/tasks-P0.md:189-223`). A wrong-chat approval or outbox row routed under the other profile can therefore pass every listed RED. Add integration REDs after T21 for cross-profile occurrence, delivery, and approval denial, including same principal/channel binding.

10. **[RED] The Phase-0 gate detectors do not prove all behavior they accept.**

    T02's only declared RED removes bwrap, so it proves availability fail-closed but not that the hostile assertions detect a loosened filesystem, network, child-exec, or process-tree profile (`docs/tasks-P0.md:27-35`). T03 accepts correct behavior for all five prerequisite failures but its RED mutates only PATH/bwrap (`docs/tasks-P0.md:37-45`). Annex requires each RED to use a controlled invariant-breaking mutation and observe durable state, process, or sink—not just a returned error (`docs/HARNESS-SPEC.md:1362-1367`). Add safe negative controls per hostile boundary and a table covering all five doctor prerequisites and their capability-scoped outcomes.

11. **[RED] T13's “tolerant parser” can violate the cited P0.7 security/effect boundary.**

    T13 permits a tolerant parser and tests only generic silent acceptance (`docs/tasks-P0.md:128-134`). Annex P0.7 explicitly forbids tolerant acceptance for security/effect payloads (`docs/HARNESS-SPEC.md:1550-1553`). A parser that repairs an unknown approval decision or effect field could still satisfy the current RED. State that tolerance is limited to non-security/non-effect payloads and add a RED proving malformed/unknown effect or policy discriminators reach no sink.

12. **[RED] T15's detector cannot prove its PRD conversation acceptance.**

    Acceptance is a “useful conversation” with the daemon, but the only RED checks concurrent clients for surfaced `SQLITE_BUSY` (`docs/tasks-P0.md:145-150`). That proves concurrency plumbing, not terminal input → provider → model response → journal → rendered output, the behavior required by PRD §6.1 (`docs/PRD.md:46-48`). Add a deterministic end-to-end scripted conversation oracle plus a separately labeled live-provider smoke; keep the concurrency RED as an additional reliability test.

13. **[RED] T19 can pass with neither a usable typed Task nor an interactive stuck detector.**

    T19 defines a generic typed-handler shape but chooses no concrete P0 handler, and its acceptance only rejects evidence-free completion (`docs/tasks-P0.md:189-197`). An empty handler registry passes. Its C3 test is also negative-only: ten polling repeats pass automatically if no stuck breaker exists. HARDQ requires typed Task handlers with handler-specific postconditions and scopes an existing identical-argument breaker to interactive turns (`docs/HARDQ-CONSOLIDATED.md:84-90,138-139`). Select at least one concrete P0 Task handler with an end-to-end verifier, and pair the polling exemption test with a positive same-turn repetition test that must trip the breaker.

14. **[RED] T24's ablation rule is internally weaker than its claim and can remove the detector with the feature.**

    T24 says any one of six criteria can be made RED by reverting its feature commit, then requires spot checks for only two (`docs/tasks-P0.md:248-255`). If feature and check share the reverted commit, both disappear and nothing proves RED; checking two also cannot establish sensitivity for all six. This violates the ledger's own red-capable rule (`docs/tasks-P0.md:3-6`) and the Annex detector oracle rule (`docs/HARNESS-SPEC.md:1364-1367`). Keep the acceptance harness outside the ablated implementation and run a controlled feature-off or targeted mutation for every one of the six criteria.

15. **[SIZE] T05 and especially T20 are not single reviewable slices.**

    T05 combines the SQLite engine, serialized actor, sequence allocation, synchronous projections, pre-journal redaction, three transaction recipes, daemon ownership, concurrency, and crash durability (`docs/tasks-P0.md:58-67`). T20 combines gateway core, Telegram transport, inbox state machine, outbox/reconciliation, offset protocol, profile binding, non-text handling, capability registration, and a six-point SIGKILL matrix (`docs/tasks-P0.md:201-212`). Each spans multiple owners and independent failure modes. Split T05 into journal durability/serialization and projection+recipe integration; split T20 into transport-neutral inbox/outbox durability and the Telegram built-in adapter. Preserve the same phase order and end-to-end crash tests after integration.

16. **[OK] The macro walking-skeleton order and the principal scope exclusions are otherwise preserved.**

    The ledger follows HARDQ A2's conversation → memory/profile → scheduler/Telegram → sandboxed-exec vertical order, gates only arbitrary execution T22-T23 on T02 (`docs/tasks-P0.md:11-12,225-244`), and does not pull in full S7, full S5, Activator, decay, entropy detection, SemanticHealth, native sandbox, plugin channels, or the coding trio (`docs/tasks-P0.md:257-262`). This does not cure the missing minimum contracts or the P2 purge creep above.

VERDICT: FAIL

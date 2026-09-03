# P0 task-ledger verification round 3

Target: `docs/tasks-P0.md` and the matching E14 annotation at repository HEAD `ace4bb765353425272e770e69ed0bb8f4aa0e205`. Scope is limited to the six round-2 residuals named in the brief and new defects introduced by those folds.

1. **[OK] MEMORY_FORGET and DATA_PURGE are now consistently deferred to their normative P2.1 owner.**

   T19 now uses append-only supersession for P0 correction and explicitly defers both operations (`docs/tasks-P0.md:197-207`); the deferred ledger agrees (`:306-312`). E14 now says the same (`docs/ARCHITECTURE-ESSENTIALS.md:170-187`), matching Annex P2.1 (`docs/HARNESS-SPEC.md:1469-1478`). No P0 forget/purge implementation or RED remains.

2. **[OK] T02 negative controls are hazard-safe without weakening the hostile-suite invariant.**

   The real-host positive suite still proves `/etc/shadow`, egress, and process-tree denial, while detector-sensitivity mutations now use a non-secret canary in a test-owned directory, a test-owned local network sink, and a test-owned process tree (`docs/tasks-P0.md:26-39`). The text explicitly forbids recursive real `/etc` exposure and uncontrolled egress; bwrap absence still fails closed. This closes the round-2 hazard finding.

3. **[OK] T05 is now task-local and has a conformance RED for each of its four seams.**

   Acceptance no longer depends on future consumers (`docs/tasks-P0.md:64-71`). AtomicWriter has the interrupted-write oracle, ContextBudget has hard-limit refusal, clock/ID injection has deterministic repeated timelines, and Assembler.Base has deterministic golden output (`:72-75`). All four detectors can run within T05 using the already-built T04 contracts.

4. **[UNFOLDED] The concrete B7 recipes were promised to T20/T22 but were not actually added there with their REDs.**

   T07 is correctly reduced to a generic fake-row projection harness and says the three concrete recipes and their crash/visibility REDs land with their consumers (`docs/tasks-P0.md:85-95`). But T20 never names or tests `occurrence+run-admission` in one `BEGIN IMMEDIATE` transaction (`:211-219`). T22 merely says “Uses T07 recipes”; it does not locally name `inbox-admission+journal` or `terminal-result+outbox`, nor give each recipe an atomic visibility oracle (`:236-245`). HARDQ B7 requires all three named transactions (`docs/HARDQ-CONSOLIDATED.md:99-110`). The consumer-before-producer defect is gone, but the requested ownership transfer is incomplete. Add the occurrence recipe and RED to T20, and the two channel recipes plus their atomicity/visibility REDs to T22.

5. **[OK] T27 now verifies the final live capability snapshot and both missing/stale probe failure paths.**

   T27 requires live sandbox, provider, and channel attestations with all fakes removed (`docs/tasks-P0.md:290-298`). Its RED removes or stale-hashes each live probe and requires the affected capability OFF with zero dispatch (`:299-302`). This now fulfills T11's deferred integration promise and HARDQ B9's fail-closed sealed-snapshot contract (`docs/HARDQ-CONSOLIDATED.md:121-127`).

6. **[OK] No new defect was introduced by the five effective folds.**

   Task numbering remains T01-T27 in order; T02 still gates only T25-T26; T05 and T07 are independently satisfiable; T19 no longer contains P2 behavior; T27 does not introduce runtime activation; and the deferred ledger remains aligned with the task bodies. The only failure is the incomplete B7 recipe relocation in finding 4, not a new contradiction.

VERDICT: FAIL

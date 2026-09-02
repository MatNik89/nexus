# ARCHITECTURE-ESSENTIALS round-3 targeted verification (Codex)

Scope: commit `d419885721e629df057c8bb13cae3495c61b4db8`; only the six requested round-2 folds and regressions introduced by those edits. This is not a full re-review.

1. **[UNFOLDED] E2 still contains a false decision signal in its heading.** Essentials:27 says `assistant FIRST (decided)`, while Essentials:30-31 correctly says formal approval is `PENDING` and Essentials:187-188 says “E2 is a proposal, not a decision.” The sources remain explicit: `PRD.md:64-66` awaits user confirmation and `DESIGN-STATUS.md:13,37-41` keeps #6 OPEN. The body and Open item 1 correctly fold the fix, but the cheat-sheet heading contradicts them and is the fastest text a developer will scan. Replace `(decided)` with `(PENDING product approval)` (or remove the qualifier). This is the residual of Codex r2 #3 / Kilo r2 #1-2, not a new regression.

2. **[OK] E4 now scopes `AtomicWriter` to S5.1/S5.3 file/workspace mutation and explicitly exempts per-record SQLite-WAL writes.** Essentials:50-55 separates durable domains, limits `tmp → fsync → rename` plus pre-FS snapshot to the P0.11 file/workspace scope, and assigns persistence-engine atomicity to SQLite-WAL. This matches `HARNESS-SPEC.md:1564-1567` and `HARNESS-PLAN.md:340-346`; Codex r2 #5 / Kilo r2 #3 is folded without the former overreach.

3. **[OK] E6 now distinguishes the two build-order documents by role and preserves the unresolved conflict.** Essentials:68-77 calls SECTION-MAP §1 the dependency DAG, HARNESS-SPEC:1020-1056 the vertical delivery-slice plan, gives the concrete S7/S5 ordering mismatch, and sends residual conflicts to hard-questions; Open item 4 repeats the stop condition at :192-193. This is an honest reconciliation of Codex r2 #6: it does not falsely claim that the two orderings are equivalent or already adjudicated.

4. **[OK] E7 is correctly renamed “Transactional capability activation.”** Essentials:79-85 no longer calls the three-operation `Prepare → Commit → Activate` lifecycle “two-phase.” The heading now matches `DESIGN-STATUS.md:9` and retains the required rollback/failure-state contract. Codex r2 #8 is fully folded.

5. **[OK] Header precedence is now scoped and includes the missing owners.** Essentials:8-14 gives PRD product authority, limits HARNESS-SPEC priority to Annex A contracts only, includes DESIGN-STATUS as the dated status ledger, and explicitly warns that stale salvage in the SPEC body must not override the greenfield decision. This directly closes Codex r2 #24 / Kilo r2 #4 and is consistent with `HARNESS-PLAN.md:3-5` versus the stale salvage references in `HARNESS-SPEC.md:350-353`.

6. **[OK] E10 restores the exact `execveat` boundary and narrows the probe stop condition correctly.** Essentials:103-112 now says `execveat` and blocks only production S6.2/arbitrary-exec implementation or reliance on the sandbox boundary; it explicitly allows other P0 work to continue. Open item 2 at :189-190 repeats the same scope. This matches `DESIGN-STATUS.md:10` and `PLAN-HOLES-CONSOLIDATED.md:104-113`, closing Codex r2 #25.

7. **[OK] Targeted regression sweep: the five corrected edit areas introduce no additional material error.** The new `tasks-P0.md` mention at Essentials:193 is a future handoff artifact already specified by `HANDOFF.md:30`, not a broken reference. E6 does not silently choose a winner, E4 does not impose rename on SQLite, precedence does not revive stale salvage, and E10 does not block unrelated scaffold work. The only remaining defect is the pre-existing E2 heading captured in finding 1.

VERDICT: FAIL

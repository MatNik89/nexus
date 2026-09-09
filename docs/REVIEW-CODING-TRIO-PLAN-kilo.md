# Review of PLAN-CODING-TRIO.md (round 6, plan-only)

- **Reviewed**: `docs/PLAN-CODING-TRIO.md` v6 at `adc10b0` on `main`
- **Verdict**: PASS

The plan is complete and buildable. The round-5 finding (restart-time rollback
needs its own grant) is folded correctly, and — the specific thing round 6 asked
me to hunt for — `PolicyWorkspaceRollback` does NOT reintroduce the problem it
was built to fix: its own crash is a fixed-point, recovered by the same durable
primitive, not an unbounded rollback-of-the-rollback.

## Verification (the four questions)

### 1. PolicyWorkspaceRollback is durable — it does NOT fall back to the non-durable PolicyTool pipeline

The rollback is specified to "run through the same `RunDurableTool`/S6/S7 path as
`PolicyWorkspaceApply` itself" (`:204-205`), consumes "its OWN grant before performing any
write" (`:210`), and has "its own reconciliation predicate" (`:211`). `RunDurableTool` is
the durable lifecycle method invariant 2 names at `:147-153` (companion builder passed
directly, never the `PolicyTool`/nil-builder path of `EffectPath.RunTool` at
`effectpath.go:445,369,516`). So the rollback is durable and governed, not the non-durable
pipeline the whole plan exists to avoid. The rollback's specific companion event *names*
are not spelled out (implied by "same path") — Note 1, not a defect.

### 2. A crash during the rollback itself is a fixed-point, not an unrecoverable state

Walk the rollback-crash case: the rollback operation restores files, crashes partway,
leaves a mixed BEFORE/AFTER state, and its durable STARTED is rehydrated to UNKNOWN on
restart. Invariant 2's rollback reconciliation already covers this exactly: "a mixed
BEFORE/AFTER state remaining → rollback `Reconcile(false)` then `Next` on that SAME
rollback operation" (`:213-214`). Because the rollback's identity is deterministic —
derived from the ORIGINAL operation's identity + profile + workspace + sealed-bundle
digest + the all-BEFORE target (`:206-207`) — "continue the existing rollback" and "start a
fresh one" are the SAME operation (`Begin` is idempotent, `Reconcile(false)` is legal from
the rehydrated UNKNOWN). So a crash inside the rollback is recovered by re-issuing a grant
to the same operation, which then continues restoring the remaining files — no third
operation, no unbounded regress. It is also genuinely *simpler* than the original Apply's
recovery, because the target state is fixed (all-BEFORE from the already-sealed bundle) and
restoring an already-BEFORE file is a no-op. The plan states the mechanism (`:213-214`) but
does not frame it explicitly as "the rollback's own crash" nor attach a crash-injection
detector for it — Note 2, not a defect.

### 3. End to end internally consistent

Every restart scenario now closes:
- committed → CLOSED, no S7 action (`:181-183`, `:332-336`);
- no-commit all-BEFORE → `Reconcile(false)`+`Next` original Apply (`:337-338`);
- no-commit all-AFTER / mixed → governed rollback, then original Apply
  `Reconcile(false)`+`Next` (`:339-350`, `:208-217`);
- FOREIGN → both operations stay UNKNOWN/manual (`:215`, `:351-352`);
- entry missing / bundle missing / corrupt entry → SAFE / FOREIGN / journal fails closed
  (`:353-363`).

The ordering "ONLY after the rollback succeeds may the original Apply `Reconcile(false)`"
(`:216-217`) makes the two operations' states unambiguous, and the two new detectors —
ablate the rollback's grant → zero writes (`:218-220`, `:368-369`), and the closed
transaction never rescanned (`:224-226`) — are RED-capable. No disagreement between
invariant 2's contract, invariant 4's table, and the rollback section.

### 4. Genuinely complete — this is convergence, and PASS is the honest verdict

The rollback does not reintroduce its own problem: its crash is a fixed-point via
`Reconcile(false)`+`Next` on a deterministic identity, not a new operation needing its own
rollback. The remaining items are slice-level tuning, all flagged as such: the rollback
companion event names, the `RunDurableTool` S6.0/S6.9 re-enforcement mechanism, the
`GOTOOLDIR` child-closure discovery, the retention threshold, and the policy constants.
Finding count 5→5→3→2→1 across five rounds, each fix independently re-verified (including
two factual disagreements resolved by reading the code/plan text, not by averaging), is the
signature of a plan that has converged. Continuing to demand more rounds would be the
pathological non-convergent loop the project's own rule rejects.

## Notes (not FAIL reasons)

1. **Name the rollback's companion events.** `workspace.rollback_started` /
   `workspace.rollback_committed` (mirroring `apply_started` / `mutation_committed`) are
   implied by "same path" but not spelled out; name them in Slice 3's slice plan.
2. **Frame and test the rollback's own crash explicitly.** The fixed-point mechanism is
   stated (`:213-214`) but not labeled "rollback's own crash"; add one crash-injection
   detector — crash the rollback mid-restore, restart, prove `Reconcile(false)`+`Next` on
   the same identity continues (not a third operation) and reaches all-BEFORE.
3. **`RunDurableTool`'s S6.0/S6.9 re-enforcement** remains a named requirement without a
   mechanism (`:147-156`), correctly deferred to Slice 3's own review.

VERDICT: PASS

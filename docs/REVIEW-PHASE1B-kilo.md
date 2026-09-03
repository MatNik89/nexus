# REVIEW-PHASE1B — T07–T12 (adversarial)

Scope: projection harness, state tables, config resolver, paths/procx, closure snapshot, checker.
`go vet ./internal/...` clean; `go test -count=1 ./internal/...` GREEN. T04–T06 were NOT reopened (no
new task broke them).

---

## T07 — projection

**1. [OK] Sync-projection tx semantics are sound.** `appendOne` does INSERT event →
`applySyncProjections(tx, pendingEv)` → `tx.Commit` (journal.go:413-437): a projection observes the
fully-built `Event` inside the same transaction, and state+event commit atomically. The async
`Projector` advances its offset via a `WHERE current_offset = <prev>` CAS and fails on
`RowsAffected != 1` (fencing, projection.go:94-101), so two concurrent `Run`s cannot both advance.

**2. [QUALITY] `Projector.Run` re-scans and re-verifies the WHOLE journal on every call.**
`Run` drives `Replay(from, …)`, whose query has no `WHERE journal_offset > from` — it reads and
verifies every row from 0, then skips `<= from` (journal.go:435-472). Fine for P0 volumes, but O(n)
per run → O(n²) cumulative for a lagging observability projector. A `WHERE journal_offset > from`
path (sacrificing the per-run chain re-verify, which Open already did) is the eventual ceiling.

## T08 — state tables

**3. [OK] All three tables match Annex P0.1 edge-for-edge.** Run: CREATED→ADMITTED→RUNNING→
{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}, UNKNOWN→{SUCCEEDED|FAILED|MANUAL_RECOVERY} only, cancel from
CREATED/ADMITTED/RUNNING. Turn: CREATED→RUNNING→{SUCCEEDED|FAILED|CANCELLED}. Attempt:
PLANNED→AUTHORIZED→RUNNING→terminal + the same UNKNOWN reconciliation set. No missing legal edge, no
invented edge, and cancel-from-UNKNOWN is correctly rejected (default-reject). `NewTable` rejects
duplicate (event,from) and `Step`/`Fold` fail closed on unknown/illegal.

**4. [QUALITY] `Fold`'s comment claims "checkpoint = journal offset" but `Fold` neither tracks nor
returns offsets.** It takes `[]string` and returns only the final state (machine.go:67-76); the
offset is the caller's (Replay's) responsibility. Doc drift, not a defect.

## T09 — config

**5. [OK] Bounds are enforced; no layer skips `knownKeys`.** File (`parseFileLayer`), env
(`envLayer` iterates the closed set), and CLI (`cliLayer`) all reject unknown keys; `ValidateBounds`
rejects egress wildcards (any `*`) and `sandbox_disabled:true`; origin tracking records the
last-setting layer in precedence order.

**6. [QUALITY] `egress_allow` comma parsing does not trim or reject empty entries.** `"a,,b"` →
`["a","","b"]`, `"a, b"` → `["a"," b"]` (config.go:161); ValidateBounds only flags `*`. The result is
a silently non-matching allowlist (fail-closed, but a broken config that the user won't notice).
**7. [QUALITY] `sandbox_disabled` is case-sensitive at the env layer.** `val == "true"` treats
`NEXUS_CFG_SANDBOX_DISABLED=TRUE` as false and silently accepts — fail-closed (sandbox stays on) but
silent. Minor.

## T10 — procx

**8. [QUALITY] `Terminate`'s post-`Wait` "belt-and-braces" `Kill(-pgid, SIGKILL)` is a PID-reuse
hazard.** After `<-done` reaps the leader, `-pgid` can be recycled before the final group kill
(procx_linux.go:89). Narrow window, but the `Token.Start` is not consulted to guard it. Low.
**9. [OK] setpgid (not setsid) is honest, and the limitation is inherent.** A `setsid`-escaping
grandchild is not reaped — but the comment claims only process GROUPS, the sandbox isolates via its
own pid namespace (bwrap), and the provider subprocess is trusted. `Start` handles the instant-exit
path (reap + error). Token is pid+starttime (PID reuse cannot forge identity).

## T11 — closure

**10. [OK] Seal is deterministic and requested-subset correct.** `Resolve` sorts traversal + request
for a deterministic topological order; unknown names, duplicates, and cycles are rejected; conflicts
are checked over the resolved closure. `Seal` turns a capability ON only when all its probes pass and
all `Requires` are ON (deps decided first), and `On()` for an unrequested/unknown name returns false.
**11. [QUALITY] `Resolve` does not validate that `Conflicts` names are KNOWN.** A typo'd conflict
(e.g. `"telegarm"`) is silently ignored (closure.go:98-102) rather than fail-closed, so an intended
mutual-exclusion can silently drop. Dormant in P0 (the P0 manifest declares no conflicts), but a
validator gap. **12. [QUALITY] duplicate probe names silently last-wins** in `Seal`'s `probeOK` map;
no rejection (closure.go:142-145).

## T12 — checker

**13. [OK] Grading is deterministic, artifact-only, and XOR-validated.** `Grade` rejects empty
contracts, validates each criterion/evidence (exactly one kind), and has NO free-text kind
(structural anti-sycophancy, E13). `DeliveredAndAcked` correlates receipt+ack to the SAME occurrence
id (B5).

**14. [QUALITY] `gradeOne` uses first-match for single-value kinds — ambiguous bundles are resolved
arbitrarily, not rejected.** For `ExitCodeIs` and `FileHashIs`, the FIRST matching-kind evidence wins
and later same-kind evidence is silently ignored (checker.go:151-169). Inert for P0 (the harness, not
the worker, assembles the bundle — checker ≠ worker), but a stricter checker would reject a bundle
with conflicting same-kind evidence rather than pick one. **15. [QUALITY] `DeliveredAndAcked` does
not check temporal order** (`AckAt` after `DeliveredAt`); an ack-before-delivery bundle passes. Low
(the harness stamps timestamps correctly). **16. [QUALITY] `AcceptanceContract.ID` is not validated**
(no `requireID`), so a control-char ID is admitted. Low.

## RED quality

Spec-anchored and non-vacuous where read: the transition tables are exercised against the Annex
lists; `Resolve`/`Seal` cover cycle/unknown/conflict/missing-probe; `Grade` covers empty/ambiguous
contracts and each kind. Missing (ablation-worthy) REDs, all low: ambiguous same-kind bundle (finding
14), unknown `Conflicts` name (11), and `egress_allow` empty/space entries (6) are untested.

---

## Verdict

The batch is faithful to the sources and correctly implements the hard invariants: sync projections
commit atomically with the event, the three state tables match Annex P0.1 edge-for-edge, config only
narrows the floor, identity is pid+starttime, closure/Seal are deterministic and fail-closed, and the
checker is deterministic and prose-free. All findings are low-severity QUALITY notes (doc drift,
edge-case parsing, narrow PID-reuse windows, first-match semantics, missing low-value REDs) — no
BREAK, SEC, or material RED vacuity.

VERDICT: PASS

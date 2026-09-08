# PLAN-CONVPROJ round-4 design review (Codex)

## Scope and immutable binding

- Review type: design-only and read-only except for this requested report; no implementation or production tests were run.
- Target: branch `slice/p0-convproj` at commit `2e56e3523d7d0dde1837a236fc43403a3e1cf812`, tree `ada8db5883a35edeb969b0f9e4eae2c37d53c6f1`.
- Evidence source: temporary clean on-disk export `/home/matej/HARNESS/nexus-plan4-codex.ASYj6N`, removed after verification; `docs/PLAN-CONVPROJ.md` SHA-256 `0a91c6d284f88cdbd9bca5cf37e537ce0b70daa557f89f2e5c5a4846e9b55d95`.
- Required marker: `does NOT resurrect` is present at `docs/PLAN-CONVPROJ.md:33`.
- Verdict policy: FAIL only for substantive correctness, security, behavioral-equivalence, or buildability flaws; editorial issues remain notes.

## Round-3 finding verification

The single round-3 behavioral-equivalence defect is resolved.

The current replay creates a history pair only when admission is observed (`internal/app/daemon/daemon.go:329-350`). A preceding `turn.succeeded` cannot update a nonexistent pair and is therefore discarded for history (`internal/app/daemon/daemon.go:351-367`); a later admission creates only the admitted half and does not replay the earlier success.

The v4 rule now matches that behavior exactly: `hist_done` is set by a valid `turn.succeeded`, including an empty final, only if `hist_seq` proves admission was already observed; a later admission explicitly does not resurrect a discarded pre-admission success (`docs/PLAN-CONVPROJ.md:29-35`). Consequently:

- `admission -> success` sets `hist_seq`, then `hist_done`, so the row enters history.
- `success -> admission` leaves `hist_done=false`; admission supplies ordering and content but not completion, so the row remains outside history.
- `success -> admission -> success` enters history only on the second success, matching the retained prefix fold.
- Recovery remains independent: `rec_state=SUCCEEDED` still requires a non-empty success final, exactly as the current `recoveredTurnOutcome` does (`docs/PLAN-CONVPROJ.md:36-39`; `internal/app/daemon/daemon.go:455-466`).

The completed-history index still predicates on the dedicated marker plus admission order (`docs/PLAN-CONVPROJ.md:52-56`), so empty-final completions remain distinct from pre-admission discarded successes. The `created_seq`/`hist_seq` split and per-row P0.3 provenance remain intact (`docs/PLAN-CONVPROJ.md:39-51`) and comply with Annex P0.3 (`docs/HARNESS-SPEC.md:1395-1402`).

## Five-question re-review

1. **Solves the reported class — YES.** The exact `turn.succeeded -> channel.inbound_admitted` counterexample no longer becomes completed history.
2. **Regression — NO observed regression.** Admission-first success, empty-final completion, lifecycle-first recovery, admission ordering, and current-turn exclusion retain their prior semantics.
3. **Detector validity — YES at design level.** The oracle remains the retained old replay rather than a second implementation of the new fold and compares both query surfaces on every generated prefix (`docs/PLAN-CONVPROJ.md:65-75`).
4. **Revert-proof — YES at design level.** Reverting only the new `hist_seq` precondition/no-resurrection rule makes the round-3 success-before-admission prefix diverge again while the retained oracle remains unchanged.
5. **Class sweep — YES.** The corrected rule composes with admission-first, pre-admission, repeated-success, empty-final, failed-first, suspended-first, resume, and interleaved-identity sequences without requiring another state owner.

## Non-blocking notes and weakest points

1. The detector list says “randomized lifecycle sequences” but does not name `turn.succeeded -> channel.inbound_admitted` explicitly (`docs/PLAN-CONVPROJ.md:68-73`). The amended rule makes the required behavior unambiguous, but implementation review should require that exact deterministic prefix rather than assume the fixed seed reaches it.
2. The precedence sentence at `docs/PLAN-CONVPROJ.md:57-61` says succeeded overwrites what “later events” set; the current replay is chronological and the differential oracle is declared the lock. This appears to mean earlier events and is editorial, not a conflicting owner because the oracle/equivalence clauses control behavior.
3. “A relative assert cannot flake” at `docs/PLAN-CONVPROJ.md:86-90` remains too absolute without specified warm-up, repetitions, and statistic. This is a detector-quality note, not a substantive design defect.

Topknot simplification review: lean already. The fix adds only the missing history-membership precondition and no new table, dependency, API, or state owner.

## Strongest counterargument and proof ceiling

The strongest counterargument is that the exact ordering regression detector is implied rather than explicitly enumerated. That is insufficient to prove a future implementation, but it does not make the design incorrect or unbuildable: the transition rule and retained-reference outcome are now explicit and mutually consistent. It remains an implementation-review requirement and proof ceiling, not a FAIL reason under the owner-directed verdict policy.

Proof ceiling: static review of the committed design, governing owner contracts, current retained replay, and immutable clean-export identity only. No implementation exists at this revision, so runtime behavior, migration/rebuild execution, query plans, latency, and detector RED capability are not verified.

VERDICT: PASS

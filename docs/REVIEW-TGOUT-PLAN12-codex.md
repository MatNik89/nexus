# PLAN-TGOUT v12 review — Codex

## Artifact binding

- Reviewed commit: `aafd411dbbf352a9e6c53724b023d3fad0a70cdf` only.
- Clean on-disk export: `/home/matej/HARNESS/nexus-review-aafd411-codex`.
- Exported `docs/PLAN-TGOUT.md` SHA-256: `a8f8df3c9710a8b291ec540ee20ebfe9d19b44f76f5e3261a9870501f939613d`, identical to `git show aafd411:docs/PLAN-TGOUT.md`.
- Required marker verified: committed `docs/PLAN-TGOUT.md:105` contains `failedTurnOutcome`.
- The live worktree's untracked review files were excluded from evidence.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:87-105` — The plan has no structured producer-to-journal path for the precedence-winning `tool`. It says the planner returns a `contracts.TypedError`, but that contract has only code/category/retryability/safe-message/origin/retry metadata (`internal/kernel/contracts/contracts.go:523-530`), while the planner boundary returns only `(Action, error)` (`internal/kernel/loop/loop.go:38-42`) and `failTurn` receives only the wrapped `error` (`internal/kernel/loop/loop.go:239-255`). Consequently the loop cannot populate `turn.failed.tool`, and the live Telegram edge cannot obtain the tool for the named-tool Croatian response, without parsing `SafeMessage` or another error string—the exact string classification the plan rejects at `docs/PLAN-TGOUT.md:91-94`. The new recovery helper only consumes persisted data; it does not solve how that data reaches the event.

PROBE: Static end-to-end data-flow trace from drift classification through `Planner.Plan`, `Loop.failTurn`, `turn.failed`, recovery, and edge mapping at the immutable export.

FIX: Specify one typed carrier for the live failure, e.g. a planner-specific error/outcome containing both `contracts.TypedError` and `contracts.ToolID`; require the loop to validate/extract it structurally and append both fields, and require live and recovered edge tests to obtain the tool without parsing `SafeMessage` or `error.Error()`.

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:104-110` — Consulting failed recovery *after succeeded/suspended* gives a stale suspension priority over a later terminal failure. The existing `completedTurnFinal` treats any `approval.turn_suspended` summary as a successful recovery candidate (`internal/app/daemon/daemon.go:424-435`), while the state machine permits the valid sequence `turn.suspended -> turn.resumed -> turn.failed` (`internal/kernel/machine/machine.go:171-176`). On a collision after that sequence, `completedTurnFinal` returns the old approval summary, so `failedTurnOutcome` is never consulted and the edge does not return the reconstructed drift response. The proposed detector covers a failed-only turn, not this valid mixed lifecycle.

PROBE: Static state-machine counterexample: durable suspension summary, resume, `TOOL_SCHEMA_DRIFT`, durable `turn.failed`, crash before edge completion, then collision/redelivery. The specified lookup order selects the earlier suspension.

FIX: Make recovery state-aware in one ordered fold, or make `completedTurnFinal` suppress a suspension superseded by a later `turn.resumed` plus terminal failure; add a committed `suspended -> resumed -> failed -> collision` detector asserting the Croatian drift response and zero planner calls.

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:104-108` — `failedTurnOutcome(turn) (code, tool, ok)` has no error result even though durable recovery requires journal/projection I/O. The current recovery helper deliberately returns replay errors so a broken canonical stream is not masked by the duplicate-event symptom (`internal/app/daemon/daemon.go:411-445`). A second recovery lookup can fail after the succeeded/suspended lookup succeeds; the specified signature can only collapse that failure into `ok=false`, panic, or hide it behind the original collision error.

PROBE: Partial-failure analysis of the two sequential recovery reads required by the specified order; the second read's error has no representable return path.

FIX: Use `failedTurnOutcome(turn) (code, tool string, ok bool, err error)`, or perform one replay that returns a typed outcome union plus `error`; add a corrupt/read-failure detector proving the canonical-stream error remains decisive.

## Five-question check

1. Solves reported class? **NO.** Persisting `tool` is stated, but its structured live data path is absent, and one valid lifecycle returns the wrong recovered outcome.
2. Regression? **NO proof.** The stale-suspension precedence would regress terminal-outcome honesty after resume.
3. Detector validity? **NO.** The recovery detector omits the valid suspended/resumed/failed sequence and the recovery-read failure boundary.
4. Revert-proof? **PARTIAL.** The two stated ablations are causal for failed-only recovery, but neither detects the missing live carrier or stale-suspension precedence.
5. Class sweep? **NO.** Recovery was not swept across all legal predecessor states of `turn.failed` or across the journal-read error path.

## Topknot lens

The new helper is not yet the smallest complete mechanism: two independently replaying, priority-ordered helpers create both an error-propagation gap and cross-state precedence ambiguity. One state-aware recovery fold returning a typed outcome union and an error has fewer reads and one owner for precedence.

Proof ceiling: This is a design-only static review. No implementation or runtime detector exists at `aafd411`; therefore no RED/GREEN execution claim is made.

VERDICT: FAIL

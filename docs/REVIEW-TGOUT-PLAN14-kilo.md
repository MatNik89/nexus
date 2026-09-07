# REVIEW-TGOUT-PLAN14-kilo — design review of PLAN-TGOUT.md v14 (commit 83f16ca)

**Scope:** `docs/PLAN-TGOUT.md` at `83f16ca` on `slice/p0-tgout` (HEAD verified; file
contains `loop.DriftError` and `recoveredTurnOutcome`). Read-only DESIGN review — no code.
Cross-checked against the round-13 findings (`REVIEW-TGOUT-PLAN13-{codex,agy,kilo}.md`)
and the current `planner.go`/`loop.go`/`daemon.go`.

## What v14 correctly resolves

- **codex MED (import cycle) — closed.** The carrier is `loop.DriftError{Typed
  contracts.TypedError; Tool contracts.ToolID}` in the LOOP package: `planner` already
  imports `loop` (to return `loop.Action`), `loop` never imports `planner`, and
  `failTurn` `errors.As`-es its own package's type — no import edge is added, and both
  fields are exposed structurally (`:104-113`). This matches the existing edge
  (`planner -> loop` via `planner.go:226`) with no `loop -> planner` reference.
- **kilo F1 (one-fold inconsistency) — closed.** `completedTurnFinal` is refactored into a
  single `recoveredTurnOutcome(turn) (RecoveredOutcome, bool, error)` returning a closed
  sum `{Kind: SUCCEEDED|SUSPENDED|FAILED; Final; Code; Tool}` — one replay, one ordered
  precedence (later success wins; suspension suppressed by a later `turn.resumed`; a
  terminal failed supersedes), with existing succeeded/suspended behavior preserved
  through `Kind` and the error return propagating replay/projection failures (`:114-125`).

The precedence is a clean state machine over the legal sequences
(`suspended → resumed → succeeded|failed`), and the closed sum correctly separates
`Final` (reply text for SUCCEEDED/SUSPENDED) from `Code`+`Tool` (FAILED).

## Notes (not findings)

- `loop.DriftError` must implement `error` (`Error() string` delegating to
  `Typed.Error()`) for it to be returned as the planner error and recovered via
  `errors.As`; the plan implies this but does not state the method. This is the same
  implied contract the round-13 codex FIX relied on, not a new gap.
- "later success wins" is technically redundant with "a later `turn.resumed` suppresses the
  suspension" (a resumed event always precedes a terminal succeeded/failed in the legal
  state machine), but it is harmless and preserves the existing behavior verbatim.

## Confidence

Grounded in the round-13 docs and the current source. Both round-13 findings are
functionally closed, the mechanism is now a single dependency-correct carrier and a single
union-returning fold, and the two notes are non-material.

VERDICT: PASS

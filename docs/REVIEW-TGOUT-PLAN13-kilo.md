# REVIEW-TGOUT-PLAN13-kilo — design review of PLAN-TGOUT.md v13 (commit b2b9624)

**Scope:** `docs/PLAN-TGOUT.md` at `b2b9624` on `slice/p0-tgout` (HEAD verified; file
contains `DriftError`). Read-only DESIGN review — no code. Cross-checked against the
round-12 findings (`REVIEW-TGOUT-PLAN12-{codex,agy,kilo}.md`) and the current
`daemon.go`/`loop.go` recovery path.

## What v13 correctly resolves

- **codex MED#1 (no structured producer→journal tool path) — closed.** The planner
  returns `DriftError{Typed contracts.TypedError; Tool contracts.ToolID}`; `failTurn`
  extracts it via `errors.As` and appends both `error_code` and `tool`; live and recovered
  edges obtain the tool structurally — no `SafeMessage`/`error.Error()` string parsing
  (`:104-110`).
- **codex MED#2 (stale suspension priority) — closed in intent.** Recovery is a
  state-aware ordered fold where a suspension candidate is suppressed by a later
  `turn.resumed`, so `suspended → resumed → failed` recovers the drift outcome; the
  mixed-lifecycle detector and resumed-suppression ablation are committed (`:111-125`).
- **codex LOW#3 (no error result) — closed.** `failedTurnOutcome(turn) (code, tool, ok,
  err)` propagates replay/projection failures (`:116-118`).

## Finding

**F1 [LOW] — "ONE state-aware ordered fold" is inconsistent with the named
`failedTurnOutcome(turn) (code, tool, ok, err)` signature, leaving the recovery structure
and the resumed-suppression location unpinned.** The plan states the recovery is ONE
fold, but then names only a failed-case helper whose return is `(code, tool, ok, err)`. A
unified fold must also carry the succeeded final and the suspended summary (what
`completedTurnFinal` returns today, `daemon.go:411-447`), which this signature cannot
represent. So it is ambiguous whether `completedTurnFinal` is replaced by a single union-
returning fold, or kept as a resumed-suppression-aware helper consulted before
`failedTurnOutcome` — and that is exactly the decision codex MED#2 required to resolve
(whether the suspension suppression lives in the succeeded/suspended lookup or in the
failed lookup). The mixed-lifecycle detector would catch a wrong implementation, but the
mechanism is not pinned: the plan should state either "one fold returning a terminal-
outcome union (final | summary | failed code+tool) plus error", or "`completedTurnFinal`
suppresses a suspension on `turn.resumed`, then `failedTurnOutcome` is consulted".

## Note (not a finding)

`DriftError` carries the tool both as `Tool contracts.ToolID` and inside
`Typed.SafeMessage` ("naming the tool"); the redundancy is intentional (structured access
without string parsing), but the planner must derive the `SafeMessage` from `Tool` to keep
them consistent — an implementation invariant the plan leaves implicit.

## Confidence

Grounded in the round-12 docs and the current source. All three round-12 findings are
functionally closed; F1 is a mechanism-pinning gap, LOW but unresolved under the
all-severity rule (mitigated by the mixed-lifecycle detector, but the plan does not state
the fold structure).

VERDICT: FAIL

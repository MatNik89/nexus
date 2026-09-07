# REVIEW-TGOUT-PLAN11-kilo — design review of PLAN-TGOUT.md v11 (commit 3b43fcb)

**Scope:** `docs/PLAN-TGOUT.md` at `3b43fcb` on `slice/p0-tgout` (HEAD verified).
Read-only DESIGN review — no code. Cross-checked against the round-10 findings
(`REVIEW-TGOUT-PLAN10-{codex,agy,kilo}.md`) and the current `daemon.go` recovery path.

## What v11 correctly resolves

- **r10 kilo F1 / codex MED#4 (crash erases typed drift) — closed.** `completedTurnFinal`
  is extended to recover a durable `turn.failed{error_code}`; a redelivered update whose
  turn already failed with `TOOL_SCHEMA_DRIFT` returns the same typed outcome with the
  planner not re-invoked, and a crash-seam detector (close/reopen after `turn.failed`
  before `CompleteInbound`, redeliver, assert Croatian + zero planner calls) plus a
  failed-recovery-only ablation are specified (`:98-106`). The "zero planner calls"
  property holds structurally: on redelivery the deterministic turn id collides on the
  already-appended `turn.created`/`turn.started` events *before* `iterate` reaches
  `planner.Plan` (`daemon.go:263-280`, `loop.go`).
- **r10 kilo F2 / codex LOW#5 (duplicate-key fence crossing) — closed.** The
  duplicate/alias cases are committed again through the single-fence normalization path
  (`:80-82`).

## Finding

**F1 [LOW] — the recovery specification conflates a final-string recovery with a
typed-error recovery, and "the same typed outcome" is imprecise.** Two sub-points, both
mitigated but not pinned:

(a) `completedTurnFinal` today returns `(string, bool, error)` — the recovered *reply
text* for `turn.succeeded` / `approval.turn_suspended` (`daemon.go:411-447`). A failed
turn's outcome is a *typed error*, not a string, so "extend `completedTurnFinal` to also
recover `turn.failed`" (`:98-102`) requires a return-contract change (surface the
reconstructed `TypedError` as an error so `RunChannelTurn` returns it for the handler to
map) that the plan does not state. The detector's "assert the same Croatian response"
pins the correct behavior, but the mechanism is underspecified.

(b) `turn.failed`'s payload stores only the `error_code` (`:91`), while the `TypedError`
carries a `SafeMessage` "naming the tool" (`:88-90`) — the tool name is not in the code,
so the recovered `TypedError` has a *generic* `SafeMessage`, and "returns the same typed
outcome" is literally false for that field. The edges map the Code (not the SafeMessage),
so the user-visible Croatian message is identical, but the claim should be narrowed to
"same code" or the full typed error (or the tool name) stored.

## Confidence

Both round-10 findings are functionally closed. F1 is a wording/specification residual on
the recovery contract, LOW but unresolved under the all-severity rule; it is mitigated
(the detector pins the Croatian response and the parenthetical clarifies intent), but the
plan does not state the return-contract change or the SafeMessage loss.

VERDICT: FAIL

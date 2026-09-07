# REVIEW-TGOUT-PLAN9-kilo — design review of PLAN-TGOUT.md v9 (commit b2a2359)

**Scope:** `docs/PLAN-TGOUT.md` at `b2a2359` on `slice/p0-tgout` (HEAD verified). Read-only
DESIGN review — no code. Cross-checked against the round-8 findings
(`REVIEW-TGOUT-PLAN8-{codex,agy,kilo}.md`) and the current `channel.go` delivery machinery.

## What v9 correctly resolves

- **kilo F1 (walk widening) — closed.** The drift walk is narrowed to the values of
  `action`/`tool_id`/`name` only (case-insensitive key match, all positions incl.
  duplicates); `{"description":"memory_recall"}` stays prose, `{"action":"memory_recall",
  "action":"tool"}` is drift (`:71-80`).
- **kilo F2 (absolute wording) — closed.** "no second attempt ever exists" is gone; the
  contract is "one message per flush tick" with cross-tick recovery via the attempts rule,
  and the plan states it makes no absolute claim (`:117-120`).
- **codex MED (attempts ownership) — closed.** The attempted-before signal is
  journal-derived: the projection folds the existing pre-wire `channel.outbound_unknown`
  event into an `attempts` count, single-write-owner untouched, no SQL mutation, and
  `Projection.Version()` is bumped so old databases rebuild (`:121-130`). The ordering is
  correct against the current `Flush` (`channel.go:500-538`): `Pending` snapshots the row
  *before* `mark(EvOutboundUnknown)`, so the `attempts` value the send sees is the pre-park
  count — first flush renders, the park then advances the count, and the re-pended row
  re-flushes plain.

## Finding

**F1 [LOW] — the codex MED detector set is only partially folded.** The plan lists the
parse-400 → next-flush-plain → SENT end-to-end detector and the ignore-attempts ablation
(`:132-136`), but omits the two proof obligations the round-8 codex FIX explicitly named
for the journal-derived counter: (a) a journal **close/reopen** (restart) observation that
the count survives and the second flush still sends plain; and (b) an **upgrade/rebuild
test from a version-1 database** exercising the `Projection.Version()` bump. The plan
states the version-bump mechanism but does not require the migration/restart tests, so a
conforming implementation could pass the listed end-to-end test while shipping a broken
`Projection.Version()` bump (the exact failure codex MED cited: `CREATE TABLE IF NOT
EXISTS` does not alter an existing v1 database).

## Note (not a finding)

Because the increment happens on the pre-wire park (the codex-chosen fold point), a crash
between `mark(EvOutboundUnknown)` and `send(o)` advances the count without a wire attempt,
so the recovered re-flush sends plain and forfeits formatting on that rare crash window.
This is the deliberate tradeoff of the pre-wire increment (a post-send increment would
lose the decision across failure) — content and delivery are unaffected.

## Confidence

Grounded in the round-8 docs and the current `channel.go`. The three functional folds are
correct; F1 is a missing-detector gap, LOW but unresolved under the all-severity rule.

VERDICT: FAIL

# Review of Design Plan: conversation projection (`docs/PLAN-CONVPROJ.md`)

**Scope**: `docs/PLAN-CONVPROJ.md` at commit `a542126` on branch `slice/p0-convproj`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

The diagnosis is right (per-turn `Journal.Replay(0)` is the backlog cause) and
the fix direction (fold into the existing `SyncProjection` framework, delete
the replays) is correct. But the plan under-specifies the one thing the whole
slice depends on — the fold must be *observationally identical* to the replay
— in three concrete places where the two consumers (`conversationHistory`,
`recoveredTurnOutcome`) have subtly different semantics for the same events.
Two of those divergences are not covered by the unchanged conformance suite,
so they would ship silently.

## Findings

### F1 (MED): schema under-specifies `completed` and conflates `final` vs recovery-final

The table is `conv_turns(identity, update_id, turn_id, user_text, final, code,
tool, state, seq)` (`PLAN-CONVPROJ.md:32-33`), but the fold needs two things
this schema cannot hold:

- **`completed` ≠ `state`.** History's "completed" is owned by `turn.succeeded`
  *even when the final is empty* (`daemon.go:363-365`), while recovery's
  `SUCCEEDED` is set by `turn.succeeded` *only when `final != ""`*
  (`daemon.go:464`). An empty-final succeeded turn is completed for history but
  not SUCCEEDED for recovery. The schema has `state` (recovery sum) but no
  `completed` column, and the fold text says "set final + completed"
  (`PLAN-CONVPROJ.md:27`) — a column the schema omits.
- **`final` serves two masters.** Recovery `Final` is "succeeded final **or**
  suspension summary" (`daemon.go:450,468-478`): an `approval.turn_suspended`
  *overwrites* a prior succeeded final. History's `final` is the succeeded
  final only. One `final` column cannot hold both; a suspension-after-success
  would poison history with a challenge summary.
- **`seq` semantics unspecified.** History order is ADMISSION order
  (`daemon.go:326` builds `order` in replay order), not completion order; `seq`
  must be the admission's offset. If `seq` were assigned at completion, two
  turns admitted A→B but completed B→A would reorder. Unstated.

### F2 (MED): history query omits the current-turn exclusion

The replay excludes the current turn twice: its admission is skipped
(`daemon.go:341-343`) and its `turn.succeeded` is skipped (`daemon.go:356`).
The projection query is specified as only "SELECT completed pairs for identity
ORDER BY seq DESC LIMIT 12" (`PLAN-CONVPROJ.md:34-35`) — no current-turn
predicate. On a redelivered turn the current turn is already completed, so the
projection would include the turn in its own history, diverging from the
replay. `RunChannelTurn` calls `conversationHistory` before collision
detection (`daemon.go:272` then `277-283`), so the polluted history is
actually assembled (the planner never sees it because `RunTurn` collides, but
the function's observable output differs, and any future path that reads it
breaks).

### F3 (MED): incremental precedence details under-specified; golden detector cannot catch them

"Same precedence rules as today's fold, now applied incrementally"
(`PLAN-CONVPROJ.md:30-31`) hides three non-obvious rules: the **conditional**
resumed-clear (only when the current recovery state is `SUSPENDED`,
`daemon.go:483-485`), the **empty-final succeeded no-op** on recovery state
(`daemon.go:464`), and the **suspension-summary overwrite** of a prior outcome
(`daemon.go:468-478`). The golden rebuild detector compares rebuild-fold vs
fresh-fold (`PLAN-CONVPROJ.md:55-56`) — fold-vs-fold, not fold-vs-replay — so
it is blind to any divergence where both folds agree but differ from the
replay. Observational equivalence therefore rests entirely on the unchanged
conformance suite, which does not cover current-turn exclusion on redelivery
or suspension-after-success (the existing tests cover empty-final history —
`daemon_test.go:736,770-772` — and mixed-lifecycle recovery, but not these two).

### F4 (LOW): latency detector is host-anchored and scope-ambiguous

"one RunChannelTurn … completes in < 250ms on the dev host"
(`PLAN-CONVPROJ.md:52-53`) is a wall-clock benchmark. It does not state
whether the provider is stubbed: if `RunChannelTurn` includes the provider
round-trip the 250ms is dominated by provider latency and flakes; if stubbed,
the threshold is trivially generous. The "dev host" anchor also bakes a
specific machine's speed into the gate. RED-capable-by-construction is sound
(the old replay is seconds), but the GREEN gate is under-defined.

## Verified clean

- Diagnosis and topknot trigger are accurate; `SyncProjection` is the right,
  in-repo mechanism (synchronous fold, versioned reset/rebuild,
  `journal/projection.go:89-120`).
- Table name `conv_turns` does not collide with the lexical guard set
  (`events|journal_meta|projection_offsets|proj_sync_offsets`,
  `projection.go:26`); the "avoid guarded keywords in comments" note is real.
- O(1) recovery by `turn_id` PRIMARY KEY and the LIMIT-12 history shape are
  correct in structure; `recoveredTurnOutcome` is identity-agnostic (turn_id
  embeds identity), so a PK lookup is sound.
- Versioned rebuild via the existing framework is correctly reused.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| F1 | MED | Schema omits `completed` and conflates history-`final` with recovery-`Final` (suspension summary); `seq` assignment unspecified. | `PLAN-CONVPROJ.md:26-33`, `daemon.go:363-365,450,464,468-478` |
| F2 | MED | History query lacks the current-turn exclusion the replay enforces. | `PLAN-CONVPROJ.md:34-35`, `daemon.go:341-343,356` |
| F3 | MED | Conditional resumed-clear / empty-final no-op / suspension-overwrite precedence unstated; golden detector is fold-vs-fold, blind to replay divergence. | `PLAN-CONVPROJ.md:30-31,55-56`, `daemon.go:464,468-485` |
| F4 | LOW | Latency gate host-anchored; provider stub/real scope unstated. | `PLAN-CONVPROJ.md:52-53` |

## Required revisions before implementation

1. Add a `completed` column (set by any `turn.succeeded`) distinct from `state`;
   store the recovery outcome (`Final` = final-or-summary) separately from the
   history `final`; define `seq` as the admission-order offset.
2. Enumerate the exact per-event fold transitions (empty-final succeeded sets
   `completed` only, conditional resumed-clear, suspension overwrites
   recovery-final).
3. Specify the current-turn exclusion in the history query.
4. Define the latency detector's provider stubbing and a threshold that is not
   a single host's wall-clock number (or mark it `-short`-excluded).

VERDICT: FAIL

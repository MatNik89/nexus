# Review of Design Plan: conversation projection v2 (`docs/PLAN-CONVPROJ.md`)

**Scope**: `docs/PLAN-CONVPROJ.md` at commit `d29b7bd` on branch `slice/p0-convproj`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v2 is substantially stronger than v1: the differential replay-vs-fold
oracle (not fold-vs-fold golden), the relative latency assert, the `seq`
first-observed owner, and the partial index all close the round-1 gaps
correctly. One substantive correctness flaw remains: the plan conflates
"completed" (a `turn.succeeded` was seen, even with an empty final) with
"`hist_final IS NOT NULL`" (a non-empty final), which would drop the
empty-final completed turn — a committed invariant.

## Finding

### F1 (substantive): empty-final completion is conflated with a non-empty `hist_final`

- The plan defines `hist_final` as "non-empty final of turn.succeeded"
  (`PLAN-CONVPROJ.md:30`) and uses the partial index `WHERE hist_final IS
  NOT NULL` as the "completed" predicate — "the 12-pair query scans only
  completed rows" (`PLAN-CONVPROJ.md:40-42`).
- But "completed" is owned by the `turn.succeeded` EVENT, **even when the
  final is empty**: `daemon.go:363-365` sets `completed = true // even when
  the final is empty`. The committed detector `TestConversationHistoryRules`
  sends an empty-final turn and asserts its user text must appear in
  history (`daemon_test.go:736,770-772`), and the plan itself lists
  "empty-final" as an unchanged invariant (`PLAN-CONVPROJ.md:67`).
- Under the plan's own definition, an empty-final succeeded turn has
  `hist_final` NULL, so the partial index excludes it and it vanishes from
  history — a behavioral-equivalence break against the conformance suite.

The "completed" signal must be distinct from the non-empty final: either a
dedicated `completed`/`succeeded_seq` column (indexed `WHERE completed`), or
`hist_final` must store the final verbatim **including** `""` so that NULL
means "not completed" and `""` means "completed with empty final". The plan
specifies neither; as written the schema cannot implement the invariant the
plan claims to preserve. The differential oracle would catch the resulting
mismatch (`PLAN-CONVPROJ.md:58` covers "empty-final succeeded"), but the
oracle is a safety net, not a substitute for a buildable schema.

## Notes (editorial, not FAIL reasons)

- "succeeded overwrites everything later events would have set"
  (`PLAN-CONVPROJ.md:47-48`) is backwards prose — the fold is online, so a
  `turn.succeeded` overwrites what EARLIER events set; and the empty-final
  no-op on `rec_state` is left to the "SUCCEEDED additionally requires the
  non-empty final" clause. The plan correctly defers to the oracle as "the
  lock, not prose" (`:49`), so this is wording only.
- `identity`/`update_id` derivation for non-admission events (terminal
  events carry `TurnID`/payload `turn_id`, not `channel_identity`) is
  unstated; the envelope-vs-payload turn_id source differs across event
  types (`daemon.go:459,473-476,483,487`). Harmless in practice (history
  only reads admitted/completed rows; recovery keys on `turn_id`), but the
  fold must parse `turn-chan-{identity}-{update_id}` on the last `-`.
- The NULL-vs-empty-string distinction for `hist_final` is never spelled
  out — the ambiguity is precisely what F1 turns into a correctness issue.

## Verified clean

- Differential oracle design is sound: independent retained replay as the
  reference, every-prefix identity check, and sequence coverage (empty-final,
  suspend/resume cycles, resume-after-terminal, interleaved identities) that
  structurally excludes a fold bug mirrored in the reference
  (`PLAN-CONVPROJ.md:53-63`).
- `seq` = first-observed journal offset reproduces admission order and is
  immune to out-of-order completion (`PLAN-CONVPROJ.md:35-39`).
- Relative (≥20x) latency assert on the same corpus is CI-robust and
  includes the incomplete-tail case (`PLAN-CONVPROJ.md:73-78`).
- Rebuild oracle-vs-reference check is correct; versioned rebuild reuses the
  existing framework; table name avoids the lexical guard set.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| F1 | substantive | "completed" (turn.succeeded, empty final allowed) conflated with `hist_final IS NOT NULL` (non-empty final); empty-final turn dropped. | `PLAN-CONVPROJ.md:30,40-42,67`, `daemon.go:363-365`, `daemon_test.go:736,770-772` |

## Required revision before implementation

1. Make "completed" a first-class column (or store `hist_final` verbatim,
   empty included, with NULL = incomplete) and index on that, so empty-final
   completed turns survive the history filter.

VERDICT: FAIL

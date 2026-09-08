# Review of Design Plan: conversation projection v3 (`docs/PLAN-CONVPROJ.md`)

**Scope**: `docs/PLAN-CONVPROJ.md` at commit `323f4e4` on branch `slice/p0-convproj`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v3 resolves the round-2 empty-final defect correctly: `hist_done` is a
dedicated completion marker (set by `turn.succeeded`, empty final included),
`hist_final` stays lossless content, the `created_seq`/`hist_seq` split
reproduces admission-order history, and the partial index predicates on the
marker rather than on content. No substantive (correctness / behavioral-
equivalence / unbuildable) flaw remains. The differential replay-vs-fold
oracle is the authoritative lock and is correctly constructed.

## Notes (editorial, not FAIL reasons)

1. **`hist_done` validity guard is under-specified.** The replay marks a
   turn completed only if its `turn.succeeded` is seen *after* its admission
   (`daemon.go:363` — the `pairs[turnID]` exists guard). The fold's
   `hist_done` "set by EVERY valid turn.succeeded" (`:30-31`) leaves "valid"
   undefined. In a causal journal admission always precedes `succeeded`, so
   this never diverges in production, and the query's `hist_seq IS NOT NULL`
   predicate already drops any unadmitted row — but for literal
   replay-equivalence the fold should set `hist_done` only when
   `hist_seq IS NOT NULL` (admission seen first), or the oracle should add an
   out-of-order prefix case. One clarifying sentence.

2. **Recovery O(1) mapping is implied, not stated.** `RecoveredOutcome.Final`
   must come from `hist_final` when `rec_state = SUCCEEDED` and from
   `susp_summary` when `SUSPENDED` (the replay's `Final` is "succeeded final
   or suspension summary", `daemon.go:450`). The columns make this obvious but
   the plan never names the mapping; the oracle locks it regardless.

3. **Stale precedence prose.** "succeeded overwrites everything later events
   would have set" (`:56-57`) is still backwards (the fold is online; a
   `succeeded` overwrites what *earlier* events set, and its empty-final case
   is a no-op on `rec_state`). The plan correctly defers to the oracle as
   "the lock, not prose" (`:58`), so this is wording only. The `approval.
   turn_suspended → SUSPENDED (summary != "")` overwrite rule is likewise
   omitted from the per-event list but covered by the oracle.

## Verified clean

- **Empty-final completion**: `hist_done` set by any `turn.succeeded`,
  `hist_final = ""` preserved, `rec_state` left unchanged for empty final —
  exactly matches `daemon.go:363-365` and `:464`, satisfying
  `daemon_test.go:736,770-772`.
- **Completion vs content separation**: partial index
  `(identity, hist_seq) WHERE hist_done AND hist_seq IS NOT NULL` keys
  membership on the marker, not on a non-empty `hist_final`.
- **Ordering**: `hist_seq` = admission offset reproduces admission order,
  including the failed-before-admitted counterexample; `created_seq` is
  immutable row identity.
- **Current-turn exclusion** at query time (`:52-53`) is equivalent to the
  replay's fold-time exclusion (`daemon.go:341-343,356`).
- **Cap-after-filter**: `ORDER BY hist_seq DESC LIMIT 12` over `hist_done`
  rows matches the replay's completed-then-last-12 semantics.
- **Precedence fold**: resumed `SUSPENDED→NONE` + summary clear, failed
  unconditional overwrite, succeeded non-empty-only — consistent with
  `daemon.go:455-509`.
- **Oracle**: independent retained replay, every-prefix identity, sequence
  coverage, structurally excludes a mirrored fold bug.
- **Latency**: relative ≥20x on the same corpus, incomplete tail included.
- **Provenance**: per-row `source_event_id`/`source_offset`/`projection_version`
  folded per the cited Annex P0.3.
- **Guard**: `conv_turns` avoids the lexical canonical-table set
  (`projection.go:26`).

VERDICT: PASS

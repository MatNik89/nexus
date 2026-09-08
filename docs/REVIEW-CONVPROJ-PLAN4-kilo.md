# Review of Design Plan: conversation projection v4 (`docs/PLAN-CONVPROJ.md`)

**Scope**: `docs/PLAN-CONVPROJ.md` at commit `2e56e35` on branch `slice/p0-convproj`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v4 correctly resolves the round-3 MED: `hist_done` is now set by a
`turn.succeeded` only when the admission was already observed (`hist_seq`
set), so a success-before-admission is discarded and a later admission does
not resurrect it — mirroring the reference guard at `daemon.go:363`
(`pairs[turnID]` must exist). No substantive (correctness / behavioral-
equivalence / unbuildable) flaw remains.

## Notes (editorial, not FAIL reasons)

1. **The oracle's coverage list does not name the ordering v4 fixes.** The
   `hist_done` admission-guard is the whole point of v4, but the differential
   oracle's sequence list (`:68-71`) is unchanged from v3 — all causal
   (admitted-first) lifecycles, no "success-before-admission / out-of-order
   prefix" case. If the generator never emits such an ordering, the "false-
   green is structurally excluded" claim (`:74-75`) is not literally true for
   this guard. The user's intent is that the oracle covers the ordering; the
   plan text should say so explicitly (add "success-before-admission" to the
   `covering:` list).

2. **The turn.succeeded fold has three independent sub-effects, only one of
   which is admission-guarded.** The reference treats the two consumers
   differently on a success-before-admission: history discards it (guard at
   `daemon.go:363`), but recovery still recovers it (`daemon.go:459-467` has
   no admission guard). So the fold must set `hist_final` unconditionally,
   `hist_done` only when `hist_seq IS NOT NULL`, and `rec_state = SUCCEEDED`
   only when `final != ""` — the plan states the `hist_done` guard (`:30-34`)
   and the `rec_state` non-empty rule (`:37-39`) but never spells out that
   `hist_final`/`rec_state` are *not* admission-guarded. One explicit line
   would prevent an implementer from over-applying the guard to recovery.

3. **Stale precedence prose.** "succeeded overwrites everything later events
   would have set" (`:59-60`) remains backwards wording, deferred to the
   oracle ("the lock, not prose", `:61`). Carried from v3.

## Verified clean

- **Guard mirror**: success-before-admission leaves `hist_done` unset, later
  admission sets only `hist_seq`/`user_text` and does not resurrect — exactly
  `daemon.go:363` plus the admission handler's non-completion.
- **Empty-final completion**: `hist_done` set by any valid `turn.succeeded`,
  `hist_final = ""` preserved, `rec_state` unchanged — matches
  `daemon.go:363-365,464` and `daemon_test.go:736,770-772`.
- **Completion vs content**: partial index
  `(identity, hist_seq) WHERE hist_done AND hist_seq IS NOT NULL` keys
  membership on the marker, not on a non-empty `hist_final`.
- **Ordering**: `hist_seq` = admission offset (history order, including the
  failed-before-admitted counterexample); `created_seq` immutable identity.
- **Current-turn exclusion** at query time, cap-after-filter
  (`ORDER BY hist_seq DESC LIMIT 12`) — equivalent to the replay.
- **Precedence fold**: resumed `SUSPENDED→NONE` + summary clear, failed
  unconditional, succeeded non-empty-only — consistent with
  `daemon.go:455-509`.
- **Oracle/rebuild/latency/provenance/guard** all remain sound as reviewed in
  v3.

VERDICT: PASS

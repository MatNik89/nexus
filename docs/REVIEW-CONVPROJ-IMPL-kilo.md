# Review of convproj implementation (`84e6da3` + `5da84aa`)

**Scope**: commits `84e6da3` (feat) + `5da84aa` (detectors) on `slice/p0-convproj`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

The implementation is substantively correct. The fold mirrors the retained
reference for every production-reachable lifecycle shape, the partial index
delivers the O(12) hot path, `VerifyChain` is confined to the cold
redelivery path, the projection guard is not tripped, and the rebuild path is
framework-driven. No substantive (correctness / behavioral-equivalence /
unbuildable) flaw found. Verified by inspection against `conv.go`,
`daemon.go`, `journal.go`, and the three new detectors.

## Verification (as asked)

### 1. Fold-vs-reference equivalence completeness

The differential oracle (`convproj_diff_test.go:86-146`) drives random
(ident, update_id, kind) steps, so **success-before-admission IS generated**
by random ordering — the v4 admission-guard is actually exercised, not just
specified. Kinds cover admit / succeed / empty-succeed / suspend / resume /
fail / ordinary-fail, and the identity pool includes the `chat-1`/`chat-11`
prefix trap. Two degenerate shapes are NOT generated (notes, not flaws):
- **empty-text admission** — `admit` always emits non-empty `"u-"+id`, so the
  "first non-empty admission wins" rule (`COALESCE(NULLIF(user_text,''), …)`,
  `conv.go:94`) is not exercised for an empty first admission. The fold logic
  is correct by inspection; the case is untested.
- **malformed payloads** — see note N1.

### 2. Partial-index O(12) under a pathological tail

`conv_hist` is `ON conv_turns(identity, hist_seq) WHERE hist_done=1 AND
hist_seq IS NOT NULL` (`conv.go:53-54`). Incomplete-tail rows have
`hist_done=0` and are absent from the index, so the `ORDER BY hist_seq DESC
LIMIT n` scan touches only completed rows regardless of a 3000-row incomplete
tail; `turn_id<>?` is a residual predicate (at most one extra row scanned).
Empirically confirmed: `TestConvProjectionFasterThanReference` builds 1500
completed + 1500 incomplete turns and the commit reports 649x.

### 3. VerifyChain placement (hot path)

`conversationHistory` (the every-turn hot path) is the O(12) query only — no
`VerifyChain`. `recoveredTurnOutcome` calls `VerifyChain()` then the O(1)
lookup, and is invoked **only** from `RunChannelTurn`'s error branch
(`daemon.go:284`), i.e. the cold redelivery/collision path. The reference did
`Replay(0)` (which also verifies integrity, `journal.go:656-662`) on the same
path, so cost parity holds and integrity fail-closed is preserved. No hot
path is O(events).

### 4. Projection guard / provenance

Guard: every `conv_turns` statement avoids the canonical set
`\b(events|journal_meta|projection_offsets|proj_sync_offsets)\b`
(`projection.go:26`); `source_event_id`/`source_offset` are singular and do
not match. Provenance: `source_event_id`/`source_offset` refresh on every
`upsert` touch; `created_seq` is INSERT-only (immutable). One gap — note N2.

### 5. Version-bump rebuild

`TestConvProjectionRebuild` regresses `proj_sync_offsets.version=0` for
`conv` and reopens; the framework detects the mismatch → `Reset` + full
refold (`journal.go:318-368`), and `before == after` asserts fold
determinism. Correct. One scope note — N5.

## Notes (not FAIL reasons)

- **N1 — `turn.succeeded` drops the `json.Unmarshal` guard.** The reference
  only acts on `turn.succeeded` when `json.Unmarshal(...) == nil`
  (`daemon.go:362`); the fold ignores the error (`conv.go:106`) and treats a
  malformed payload as an empty final, marking `hist_done=1`. Machine events
  (`turn.succeeded/failed/resumed`) have `nil` payload validators
  (`main.go:474-476`), so the divergence is *technically* reachable, but the
  daemon is the sole producer and always emits valid JSON — unreachable in
  practice. One-line parity fix: skip on `err != nil`.
- **N2 — `projection_version` hardcoded to `1`.** `upsert` writes the literal
  `1` (`conv.go:69`) instead of `p.Version()`, and it is not refreshed on
  conflict. A future schema bump would leave stale per-row provenance unless
  the literal is updated by hand. `source_event_id`/`source_offset` are
  correct; only the version column is stale-prone.
- **N3 — chat-session turns fold junk rows.** The fold keys on any
  `TurnID`, so REPL session turns (`turn-{nonce}-{session}-{msgN}`, no
  admission) produce rows with `identity=NULL, hist_seq=NULL`. They are never
  read (history filters by identity; recovery is channel-only), so it is
  storage-only, but the projection is meant to be channel-scoped.
- **N4 — reference retained in `daemon.go`, not a `_test.go`.** The plan said
  "test-only reference"; the two reference methods are production-compiled
  dead code (called only from the oracle). Harmless, but they ship in the
  binary and could be accidentally reused.
- **N5 — rebuild test compares projection-vs-projection.** It asserts
  `before == after` (both projection), not projection-vs-reference on the
  rebuilt state as the plan worded it. Transitive coverage holds (oracle
  proves projection==reference; rebuild proves rebuilt==fresh), so the gap is
  only in the literal test wording.
- **N6 — latency threshold is 10x, not the plan's 20x.** `projDur*10 > refDur`
  fails (`convproj_diff_test.go:184`); actual margin is 649x, so immaterial.

## Verified clean

- `hist_done` admission-guard + no-resurrection; empty-final completion;
  `hist_final` (history) vs `rec_final` (recovery) separation; `hist_seq`
  admission-order vs `created_seq` identity.
- Recovery precedence (succeeded non-empty-only, suspended unconditional,
  resumed `SUSPENDED→NONE` conditional, failed unconditional) matches
  `daemon.go:455-509`.
- `Final` mapping (`rec_final` for SUCCEEDED, `susp_summary` for SUSPENDED)
  and `Kind`/`Code`/`Tool` reconstruction are exact.
- Current-turn exclusion at query time; cap-after-filter; oldest-first
  reverse; `QueryProjection` SELECT-only single-statement reads.
- `conv` registered at the single journal open (`main.go:499`) and in the
  test journal helpers.

VERDICT: PASS

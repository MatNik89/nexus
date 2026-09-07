# REVIEW-TGOUT-PLAN10-kilo — design review of PLAN-TGOUT.md v10 (commit 7cda3b5)

**Scope:** `docs/PLAN-TGOUT.md` at `7cda3b5` on `slice/p0-tgout` (HEAD verified).
Read-only DESIGN review — no code. Cross-checked against the round-9 findings
(`REVIEW-TGOUT-PLAN9-{codex,agy,kilo}.md`), `docs/tasks-P0.md`, and the current source.

## What v10 correctly resolves

- **codex HIGH (S7 retry owner) — closed by scope cut.** The slice is now "ADD NO
  ATTEMPT, CHANGE NO SCHEDULING" — it changes only *what bytes* an already-scheduled
  attempt carries; the pre-existing grant-less tick re-flush seam is filed as its own
  backlog item in `docs/tasks-P0.md` (same commit) and stays byte-for-byte out of scope
  (`:121-128`, `tasks-P0.md:464-471`).
- **codex MED#2 (wire-attempted semantics) — closed.** The honest contract is "HTML only
  on the FIRST IN-FLIGHT LEASE" (`outbound_unknown` count==0); the pre-wire-failure
  degradation is declared and committed as a dial-failure case (`:129-138`).
- **codex MED#3 + kilo F1 (durability detectors) — closed.** Detectors now cover restart
  persistence (close/reopen), version-bump rebuild, and a drop-the-event-fold ablation
  alongside ignore-the-count (`:142-151`).

## Findings

**F1 [MED] — r9 codex MED#4 is NOT folded: failed-turn replay is still unspecified.**
The DRIFT OUTCOME section (`:85-101`) is unchanged from v9 — it journals `error_code` in
`turn.failed` and maps `TOOL_SCHEMA_DRIFT` to the Croatian rephrase at the edge, but never
specifies recovery of that failed terminal outcome. In the existing crash window after
`turn.failed` is durable and before the terminal+outbox recipe commits, the update is
redelivered (at-least-once admission) and `RunChannelTurn` recovers only a
succeeded/suspended final (`daemon.go:263-279,411-446`); a durable `turn.failed` code is
not recovered, so the handler falls through to the generic English failure reply
(`telegram.go:257-276`). The promised structured edge mapping is therefore not stable
across restart/replay, and no crash/reopen detector between `turn.failed` and
`CompleteInbound` is specified.

**F2 [LOW] — r9 codex LOW#5 is NOT folded: the duplicate/alias case is not crossed with
the fence-normalization path.** The detector list (`:107-111`) still has bare and
single-fenced drift dialects as one dimension and the duplicate case as a separate bare
example, but does not commit the single-fenced duplicate/alias case
(``` ```json {"action":"memory_recall","action":"tool"} ``` ```), so a faulty implementation
could token-walk duplicates on the bare object and last-member-wins decode after fence
stripping while still passing the listed cases.

## Confidence

Grounded in the round-9 review docs, `tasks-P0.md`, and the current source. The three
folds the task named are correct; but "ALL round-9 findings" is not met — the codex MED#4
and LOW#5 findings remain open (the task's three-item list omitted them). F1 is MEDIUM, F2
is LOW; under the all-severity rule both are unresolved.

VERDICT: FAIL

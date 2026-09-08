# Review of Design Plan: Current Time in Model Context — v3 (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `e72b389` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v3 is substantively sound: the location is now its own injected seam
(`clockid.Clock` + `*time.Location`, rendering `clock.Now().In(loc)`), which
resolves the round-2 UTC/local contradiction; the stale duplicate Detectors
block is gone; the zone-fallback branch is declared and not faked; the
character-class invariant is honestly dropped. Two small items remain
unresolved, one of which is a round-2 finding v3 claims to fold but did not.

## Findings

### K1 (LOW, unresolved from round 2): `clockid.Real` does not exist

`PLAN-TIME.md:24` still says "Production wires clockid.Real + time.Local".
There is no `clockid.Real` type. The production clock is `clockid.System`
(`clockid.go:27-30`), wired as `clockid.System{}` (`cmd/nexus/main.go:513,
524,530`). This was round-2 K3 ("Correct `clockid.Real` →
`clockid.System`") and is not among the four findings v3 folded; the text is
unchanged. An implementer following the plan literally writes
`clockid.Real{}`, which does not compile.

### K2 (LOW): nil `*time.Location` handling unspecified

`PLAN-TIME.md:23` adds `*time.Location` as a constructor-injected dependency
but does not state its nil/required contract. `time.Time.In(nil)` panics
("missing Location"), so a nil location crashes `Plan` rather than failing
cleanly. The repo's own convention is explicit fail-closed validation of
required constructor deps (`New`: `p == nil || auth == nil`, `planner.go:
128-133`; `NewStreaming`: `sp == nil || deliver == nil`, `planner.go:142-
147`). The plan should state the location is required and fail-closed on
nil (or default to `time.UTC`), matching the file's idiom.

### Trivial (not a finding): stale section header

`PLAN-TIME.md:13` still reads "Change (v2 — all round-1 findings folded)"
while the body is v3. Cosmetic only.

## Verified clean

- **Clock/location seam**: `clockid.Fake.Now()` returns UTC; `.In(loc)` with
  a test `time.FixedZone` yields a deterministic, CI-independent expected
  line. Round-2 K1 is genuinely resolved.
- **Placement/cache**: anchor at the tail of the current user message; the
  system + history prefix stays byte-stable; the fence always closes
  (`assembler.go:53`), so the line lands outside any untrusted fence.
- **Layout**: `"2006-01-02 15:04 Monday"` valid; minute resolution holds;
  `t.Zone()` + `t.Format("-07:00")` produce the abbreviation + numeric
  offset; no user/model bytes on the path.
- **Declared UTC limit**: accurate — a missing host zone DB makes
  `time.Local` UTC, so the line truthfully shows `UTC (UTC+00:00)`; no
  degraded-state detection attempted, scoped to the owner's box. Sound.
- **No fallback branch**: correct, `Zone()`/`Local` cannot fail.
- **Determinism**: `TestToolPromptDeterministic` captures only the `system`
  role, so it is genuinely unaffected.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| K1 | LOW | `clockid.Real` does not exist; production type is `clockid.System`. Round-2 finding not folded. | `PLAN-TIME.md:24`, `clockid.go:27-30`, `cmd/nexus/main.go:524` |
| K2 | LOW | `*time.Location` nil contract unspecified; `Time.In(nil)` panics instead of failing closed. | `PLAN-TIME.md:23`, `planner.go:128-133,142-147` |

## Required revisions before implementation

1. Replace `clockid.Real` with `clockid.System` (`PLAN-TIME.md:24`).
2. State the location's nil contract (required, fail-closed, or default to
   `time.UTC`), matching the file's constructor idiom.

VERDICT: FAIL

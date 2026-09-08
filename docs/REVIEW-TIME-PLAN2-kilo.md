# Review of Design Plan: Current Time in Model Context — v2 (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `07551ab` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v2 correctly fixes the round-1 cache-inversion (anchor moved to the tail of
the current user message, leaving the `system + history` prefix byte-stable)
and the package-global seam. But the new clock-owner decision is internally
inconsistent: it injects `clockid.Clock`, whose canonical implementations
return **UTC by construction**, while the plan still demands the host-local
zone and an example `CEST (UTC+02:00)` — and the detector's "injected fixed
`*time.Location`" has no seam to travel through, because `clockid.Clock`
exposes only `Now`/`Since`. The plan also retains a stale duplicate
"Detectors" section that contradicts the v2 placement.

## Findings

### K1 (HIGH, blocking): UTC clock vs host-local-zone requirement; location seam undefined

- `clockid.Clock` (the injected seam, `PLAN-TIME.md:19-22`) exposes only
  `Now() time.Time` and `Since()` (`clockid.go:16-19`). It has **no
  Location method**.
- Both production and test implementations return UTC by construction:
  `System.Now()` → `time.Now().UTC()` (`clockid.go:29`, comment "Annex:
  every timestamp UTC") and `Fake.Now()` → `.UTC()` (`clockid.go:57`).
- The plan still requires the host-local zone — "the host zone IS the
  owner's zone" (`PLAN-TIME.md:54-55`) — and its example renders
  `CEST (UTC+02:00)` (`PLAN-TIME.md:28-29`) from `t.Zone()` +
  `t.Format("-07:00")` (`PLAN-TIME.md:26-27`).

These are mutually inconsistent. If the planner formats `clock.Now()`
directly, `t.Zone()` is `"UTC"` and the offset `"+00:00"`, so the line is
always `UTC (UTC+00:00)` — the owner in Europe/Zagreb asking "koliko je
sati" is told UTC, failing the dogfood. To produce host-local time the
planner must apply a location (`t.In(loc)`), but the plan defines no seam
for that location:

- `CLOCK OWNER` (`PLAN-TIME.md:19-22`) injects only `clockid.Clock`.
- The detector (`PLAN-TIME.md:40-45`) says it injects "an injected fixed
  `*time.Location` (test uses time.FixedZone)" — but nothing in the design
  threads a `*time.Location` into `ChatPlanner`. `time.FixedZone` does not
  change `time.Local`, and `clockid.Fake` returns UTC regardless of any
  FixedZone the test constructs.

Consequence: the "CI-independent" detector cannot be implemented as
described — there is no seam carrying the location into `Plan`, and the
production clock cannot yield `CEST`. The plan must either (a) add a
second injected `*time.Location` (or a `Zone` method on the clock) and
state that `Plan` renders `clock.Now().In(loc)`, or (b) accept UTC-only
output and drop the host-local-zone non-goal and the `CEST` example. Until
one is chosen, the design is unresolved.

### K2 (MEDIUM): stale duplicate "Detectors" section contradicts v2

Lines 57-63 are the unedited round-1 detector block. It still says "the
system message contains the exact formatted line" (`PLAN-TIME.md:58-59`)
and "nowFn pinned" (`PLAN-TIME.md:58`), which directly contradicts the v2
placement lock — "assert the SYSTEM message does NOT contain the line"
(`PLAN-TIME.md:43-44`) — and the now-removed `nowFn` seam. A single
document now instructs both "system contains the line" and "system must not
contain the line". The stale section must be deleted; as it stands an
implementer cannot tell which directive is authoritative.

### K3 (LOW): `clockid.Real` does not exist

`PLAN-TIME.md:22` says "production wires clockid.Real"; the production type
is `clockid.System` (`clockid.go:27-30`, wired as `clockid.System{}` at
`cmd/nexus/main.go:513,524,530`). Minor, but evidence the clock package was
not re-read — consistent with the K1 UTC/local confusion.

## Non-findings (verified clean)

- **Placement / cache**: anchor at the tail of the current user message;
  `assembler.Base` always terminates untrusted fences with a closing tag
  (`assembler.go:53`), so appending after `assembled` lands the line outside
  any fence — no injection, and the cacheable `system + history` prefix is
  byte-stable. Correct.
- **Layout**: `"2006-01-02 15:04 Monday"` is valid Go; minute resolution
  holds. The zone string is closed-grammar (`Zone()`/`-07:00` output), no
  user/model bytes. Sound.
- **No fallback branch**: correct — `Zone()`/`Local` cannot fail.
- **Determinism**: `TestToolPromptDeterministic` captures only the `system`
  role, so it is genuinely unaffected; the new unit detector pins a fake
  clock, no minute-boundary flake.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| K1 | HIGH | Injected `clockid.Clock` is UTC-by-construction; host-local-zone requirement and `CEST (UTC+02:00)` example are unproducible; the detector's `*time.Location` injection has no seam in `clockid.Clock`. | `PLAN-TIME.md:19-32,40-45,54-55`, `clockid.go:16-19,29,57` |
| K2 | MEDIUM | Stale duplicate "Detectors" section (lines 57-63) contradicts the v2 placement lock. | `PLAN-TIME.md:43-44,57-63` |
| K3 | LOW | "clockid.Real" does not exist; production type is `clockid.System`. | `PLAN-TIME.md:22`, `clockid.go:27-30` |

## Required revisions before implementation

1. Resolve the clock/location inconsistency: inject the location (or add a
   zone-bearing clock method) and state that `Plan` renders
   `clock.Now().In(loc)`; or drop the host-local-zone non-goal and the
   `CEST` example and commit to UTC output. The detector's FixedZone must
   have a defined seam into `Plan`.
2. Delete the stale "Detectors" block at `PLAN-TIME.md:57-63`.
3. Correct `clockid.Real` → `clockid.System`.

VERDICT: FAIL

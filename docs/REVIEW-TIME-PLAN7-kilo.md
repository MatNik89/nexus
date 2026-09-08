# Review of Design Plan: Current Time in Model Context — v7 (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `00fd1b3` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v7 closes the round-6 constructor/"Local" gap at the detector level and
the config level, removes the "host zone is the owner's zone" dual-ownership
wording, and adds a sound default lock. One inconsistency remains: the
canonical constructor description (the "definitive" SEAM bullet) was not
updated to include the "Local" rejection, so it still disagrees with the
CONFIG bullet and the detectors.

## Finding

### F1 (LOW): canonical constructor description still omits "Local"

The `CLOCK + TIMEZONE SEAM (v5, definitive)` bullet states the constructor
rejects "on an empty name or a LoadLocation error" (`PLAN-TIME.md:22-24`).
It does not list `"Local"`. But `time.LoadLocation("Local")` returns
`time.Local` **without error**, so a constructor implementing exactly that
contract accepts `"Local"` and silently reintroduces host-dependent time —
the precise round-6 hole. The rejection *is* specified elsewhere: the
CONFIG bullet adds `"Local"` "AT BOTH LAYERS — the config validation and
the planner constructor itself" (`PLAN-TIME.md:32-35`), and the detectors
assert `"Local"` rejection "at BOTH the config layer and the planner
constructor" (`PLAN-TIME.md:69-71`). So the canonical constructor contract
(line 22-24) and the rest of the plan disagree: an implementer following
the definitive seam bullet alone would ship the F1 bug. The SEAM bullet's
rejection set should read: empty name, `"Local"`, and LoadLocation error.

### F2 (trivial): stale header

`PLAN-TIME.md:13` still reads "Change (v6 — current; rounds 1-5 folded)"
while the commit is v7 (rounds 1-6). Cosmetic.

## Verified clean

- Seam/render/config: name + loaded location stored; no `time.Local`; no
  silent-UTC; `"Local"`/`""`/unloadable rejected, `"UTC"` allowed.
- Config contract matches the resolver (`config.go:88-98,113-120,276-341`).
- Default lock: the "no timezone key → exactly Europe/Zagreb" case is
  implementable against the existing default-origin seam
  (`config_test.go:47-51,95-100` asserts `Origins[...] == OriginDefault`
  and "defaults not applied").
- Unit DST detectors (Jan CET / Jul CEST via the real name seam), nil-clock
  rejection, composition-root `America/New_York` wiring detector
  (`main_test.go:316,320` captures `Messages`), ablations, and
  `TestToolPromptDeterministic` all remain sound.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| F1 | LOW | Canonical SEAM bullet (line 22-24) lists only "empty name or LoadLocation error" as constructor rejections, omitting `"Local"`; contradicts the CONFIG bullet and detectors. | `PLAN-TIME.md:22-24,32-35,69-71` |
| F2 | trivial | Stale "v6" header. | `PLAN-TIME.md:13` |

## Required revision before implementation

1. Update the SEAM bullet's constructor contract to reject `"Local"`
   explicitly (empty name, `"Local"`, and LoadLocation error).

VERDICT: FAIL

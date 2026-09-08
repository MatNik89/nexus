# Review of Design Plan: Current Time in Model Context — v4 (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `ae2d236` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v4's intent is right — make the timezone a first-class IANA-name seam with
fail-closed `time.LoadLocation`, and carry the IANA identifier so the model
can fill `reminder_set.tz` DST-correctly. But the edit was applied to only
part of the document: the FORMAT section and example still render only
abbrev + offset (no IANA identifier), and three spots still describe the
old `*time.Location` seam that v4 replaced. The plan text therefore
contradicts its own v4 rationale and the downstream tool contract.

## Findings

### F1 (HIGH): rendered line does not carry the IANA identifier the v4 rationale requires

The stated reason for carrying the IANA identifier is that `reminder_set`
needs a loadable zone name — `schedule.WallTime` calls
`time.LoadLocation(w.TZ)` (`internal/schedule/schedule.go:65,87`) and its
SEALED description advertises `"tz":"Europe/Zagreb"`
(`internal/obligation/tools.go:23,73`). An abbreviation like `CEST` will not
load. Yet the plan's FORMAT spec still says the zone is rendered as
`<IANA-or-abbrev> (UTC+02:00)` (`PLAN-TIME.md:32`, "either/or"), the example
shows only the abbreviation — `"... 04:55 Monday, CEST (UTC+02:00)"`
(`PLAN-TIME.md:35-36`) — and the stated mechanism is `t.Zone()` +
`t.Format("-07:00")` (`PLAN-TIME.md:33`). `t.Zone()` returns the
**abbreviation**, not the IANA name; producing `Europe/Zagreb` requires
`loc.String()` (or retaining the constructor's zone name), which the plan
never specifies. As written, the model still cannot populate `tz`
correctly, so the v4 rationale ("DST-correct future reminders") is unmet by
the plan's own format spec and example.

### F2 (MED): the seam type is described two incompatible ways

- `PLAN-TIME.md:23` still says the constructor takes `*time.Location`
  (the v3 seam).
- `PLAN-TIME.md:24` and the detector at `52-53` require an IANA zone
  **name** whose `time.LoadLocation` is fail-closed ("empty zone name and
  an unloadable zone name both REJECT construction") — only meaningful for
  a name-based seam.
- The detector body still says the test injects "a fixed `*time.Location`
  (test uses time.FixedZone)" (`PLAN-TIME.md:46-48`), which a name-based
  seam cannot consume.

One constructor cannot simultaneously take a pre-loaded `*time.Location`
and reject an "unloadable zone name". Line 23 and lines 46-48 are stale
from v3 and contradict the v4 name seam.

### F3 (MED): DECLARED LIMIT contradicts the fail-closed design

`PLAN-TIME.md:26-29` still declares that Go "silently falls back to UTC
when the host zone database is missing". That was the v3 `time.Local`
behavior. v4 loads the zone via `time.LoadLocation` in the constructor and
fails closed on error — a missing zone database now rejects construction;
it does not silently produce `UTC (UTC+00:00)`. The declared limit
describes a code path that no longer exists and must be rewritten or
removed.

### F4 (LOW): stale header and missing default

- `PLAN-TIME.md:13` still reads "Change (v2 — all round-1 findings
  folded)" while the body is v4.
- The config field and its default (`timezone` → `Europe/Zagreb`) appear in
  the change log but not in the plan text, which says only "the configured
  IANA zone" (`PLAN-TIME.md:24`). Minor, but the non-goal "the host zone IS
  the owner's zone" (`PLAN-TIME.md:62-63`) no longer holds literally once
  the zone is configured rather than read from `time.Local`.

## Verified clean

- Clock seam: `clockid.System` (correct type), location resolved
  fail-closed in the constructor; nil/empty/unloadable cases are committed
  detector cases.
- Placement/cache: anchor at the tail of the current user message; the
  `system + history` prefix stays byte-stable; the fence always closes
  (`assembler.go:53`), so the line lands outside any untrusted fence.
- No fallback branch: correct (`Zone()`/`Local` cannot fail).
- Determinism: `TestToolPromptDeterministic` captures only the `system`
  role and is unaffected.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| F1 | HIGH | Rendered line carries only abbrev + offset, no IANA identifier; `t.Zone()` cannot yield it, so `reminder_set.tz` cannot be filled. | `PLAN-TIME.md:32-36`, `schedule.go:65,87`, `tools.go:23,73` |
| F2 | MED | Seam described as both `*time.Location` (line 23, 46-48) and IANA zone name + fail-closed LoadLocation (line 24, 52-53). | `PLAN-TIME.md:23-24,46-53` |
| F3 | MED | DECLARED LIMIT still describes silent UTC fallback, contradicted by fail-closed LoadLocation. | `PLAN-TIME.md:26-29` |
| F4 | LOW | Stale "v2" header; config default omitted from text. | `PLAN-TIME.md:13,24` |

## Required revisions before implementation

1. Update the FORMAT spec and example to carry the IANA identifier
   alongside abbrev + offset (e.g. `Europe/Zagreb CEST (UTC+02:00)`), and
   name the mechanism (`loc.String()` or the retained constructor name).
2. Reconcile the seam description: it is an IANA zone name, fail-closed in
   the constructor; remove the stale `*time.Location` text at line 23 and
   the FixedZone wording at lines 46-48.
3. Rewrite or remove the DECLARED LIMIT block to match fail-closed
   LoadLocation semantics.
4. Fix the header and state the `timezone` config field + `Europe/Zagreb`
   default explicitly.

VERDICT: FAIL

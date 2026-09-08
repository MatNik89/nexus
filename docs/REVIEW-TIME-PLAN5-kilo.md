# Review of Design Plan: Current Time in Model Context — v5 (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `36e323b` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v5 resolves the round-4 F1/F3 cleanly: the seam is now an IANA zone name
stored alongside its loaded `*time.Location`, the line prints the stored
name verbatim plus `MST` + `-07:00`, there is no `time.Local` / silent-UTC
path, and the config contract is concrete and fail-closed. Two defects
remain: the detector still prescribes a `time.FixedZone` injection that is
impossible through a name-based seam, and the config-wiring bullet names
`runChat` as a wiring point that does not exist.

## Findings

### F1 (MED): detector still injects `time.FixedZone`, impossible through the name seam

The seam is "constructor-injected `clockid.Clock` AND the IANA zone NAME as
a string … calls `time.LoadLocation(name)`" (`PLAN-TIME.md:19-24`). But the
detector still reads "injected fixed `*time.Location` (test uses
time.FixedZone …)" (`PLAN-TIME.md:45-47`). A `time.FixedZone` is an
in-memory `*time.Location`; `time.LoadLocation` resolves only real tzdata
names (or the special `UTC`/`Local`), so a FixedZone name never loads and
cannot be injected through this seam. This is the round-4 F2 detector half,
still unfixed.

Worse, FixedZone is fundamentally incompatible with the render contract:
the line prints "the STORED IANA name verbatim" (`PLAN-TIME.md:34-35`), and
a FixedZone has no IANA name — so even conceptually the test cannot produce
the spec'd line that way. The detector must instead inject a real IANA
name (e.g. `Europe/Zagreb`, or `UTC` for guaranteed load) and assert the
exact line (name + abbrev + offset) computed from that name and the pinned
instant. The "CI-independent" claim needs a stated mechanism: for
`Europe/Zagreb`, the test binary must embed tzdata (`import _ "time/tzdata"`)
or the plan must accept system-tzdata dependence.

### F2 (LOW): `runChat` does not pass anything into a planner factory

"runDaemon and runChat pass resolved.Config.Timezone into the planner
factory" (`PLAN-TIME.md:32-33`) is wrong for chat mode. `runChat`
(`cmd/nexus/main.go:673-687`) discards the resolved config
(`layout, _, err := resolveEnv()`) and delegates to `repl.Run`, which
dialogs a UDS socket (`internal/app/repl/repl.go:38`) and constructs no
planner. The planner is built only in `buildDaemon`'s `PlannerFactory`
(`cmd/nexus/main.go:574-596`), reached through `runDaemon`; chat reuses that
daemon over the socket. The bullet should name `buildDaemon`'s
`PlannerFactory` as the sole wiring point.

### F3 (LOW): stale header

`PLAN-TIME.md:13` still reads "Change (v2 — all round-1 findings folded)"
while the body is v5. Carried over since round 3; cosmetic but should be
fixed once.

## Trivial (not findings)

- The example (`PLAN-TIME.md:37-38`) renders "UTC+02:00" but the stated
  mechanism is `t.Format("-07:00")` (which yields `+02:00`); the literal
  `UTC` prefix is implied by the example but never stated.
- "2026-09-08 05:10 Monday" pairs a Tuesday date with "Monday"
  (2026-09-07 is Monday). Illustrative only.

## Verified clean

- Seam: `clockid.System` (correct type); constructor stores name + loaded
  location; empty/unloadable name rejects construction.
- No `time.Local`, no silent-UTC fallback; missing tzdata rejects daemon
  startup (config `ValidateBounds` + constructor both fail-closed).
- Config contract: typed `timezone` field, keySchema, default
  `Europe/Zagreb`, `applyValue`, `ValidateBounds` LoadLocation — consistent
  with the existing resolver (`config.go:88-98,113-120,276-304,306-341`).
- Render: IANA name verbatim + `MST` abbrev + offset; no user/model bytes.
- Placement/cache, no-fallback, determinism guard all remain sound.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| F1 | MED | Detector injects `time.FixedZone` through a name seam — impossible; CI-independence mechanism unstated. | `PLAN-TIME.md:19-24,45-47` |
| F2 | LOW | `runChat` has no planner factory and discards resolved config; sole wiring point is `buildDaemon`. | `PLAN-TIME.md:32-33`, `main.go:574-596,673-687`, `repl.go:38` |
| F3 | LOW | Stale "v2" section header. | `PLAN-TIME.md:13` |

## Required revisions before implementation

1. Rewrite the detector to inject a real IANA zone name and assert the
   exact line from it; state the CI-independence mechanism (`import _
   "time/tzdata"` or `UTC`).
2. Correct the config-wiring bullet to name `buildDaemon`'s
   `PlannerFactory` (not `runChat`) as the wiring point.
3. Fix the stale header.

VERDICT: FAIL

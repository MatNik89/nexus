# Review of Design Plan: Current Time in Model Context — v6 (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `8093d29` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07

## Summary

v6 is now a coherent, nearly-complete design: the header, seam, render,
config contract, and detectors are consistent, the FixedZone/CI portability
gap is closed, the composition-root wiring detector is sound, and the
`runChat` wiring error is fixed. One genuine gap remains: the "Local"
special value is rejected at the **config** layer but not at the
**constructor** seam, which is the only other `time.LoadLocation` call site
and the actual render boundary.

## Finding

### F1 (LOW): constructor accepts "Local" — the `time.Local` hole is closed at config, not at the seam

`time.LoadLocation` resolves the special name `"Local"` to `time.Local`
**without error** (only `""`/`"UTC"`/`"Local"` are special-cased). The
constructor contract is "rejecting construction … on an empty name or a
LoadLocation error" (`PLAN-TIME.md:22-24`) — neither condition fires for
`"Local"`. So a directly-constructed planner with `name = "Local"` stores
`time.Local`, renders `clock.Now().In(time.Local)` (host-dependent), and
prints `Local` as the "IANA name" (which `reminder_set.tz` cannot load).

This contradicts the plan's own slice-wide invariant "There is NO
time.Local use … anywhere in this slice" (`PLAN-TIME.md:24`), and the plan
already demonstrates awareness of the exact vector: "LoadLocation("Local")
succeeds and would smuggle host-dependent time back in" (`PLAN-TIME.md:
32-34`) — yet that rejection is applied only to the config validator, not
to the constructor. The constructor detectors confirm the gap: they cover
"empty zone name and an unloadable zone name" only (`PLAN-TIME.md:68-69`);
no constructor case rejects `"Local"`.

The production path is safe (config scrubs `"Local"` before `buildDaemon`
forwards `resolved.Config.Timezone`), but the seam itself is not
fail-closed against the one name that silently reintroduces `time.Local`.
Fix: the constructor must reject `"Local"` explicitly alongside `""` and
`LoadLocation` errors, with a matching constructor detector case.

(Note: `LoadLocation("")` returns `UTC` without error too, so the plan's
explicit "empty name" check is the correct handling — that part is fine;
the gap is specifically the omitted `"Local"`.)

## Trivial (not a finding)

- Example `"2026-09-08 05:10 Monday"` pairs a Tuesday date with "Monday"
  (2026-09-07 is Monday). Illustrative only.

## Verified clean

- Seam: `clockid.System`, name + loaded location stored, no `time.Local`,
  no silent-UTC fallback.
- Config contract: typed `timezone`, keySchema, default `Europe/Zagreb`,
  `applyValue`, `ValidateBounds` `LoadLocation` + explicit `"Local"`/`""`
  rejection + `"UTC"` allowance — consistent with the resolver
  (`config.go:88-98,113-120,276-341`).
- Render: stored name verbatim + `MST` + `-07:00`; no user/model bytes.
- Unit detector: real `"Europe/Zagreb"` name seam at January (CET +01) and
  July (CEST +02) instants; `time/tzdata` portability note stated.
- Composition-root detector: `buildDaemon` sole root; `America/New_York`
  case against the captured last user message correctly turns a hard-coded
  default RED (existing spine tests capture `Messages`
  `main_test.go:316,320`).
- Nil-clock rejection added; placement/cache; no-fallback; determinism
  guard.

## Findings table

| ID | Severity | Finding | Source |
| :-- | :-- | :-- | :-- |
| F1 | LOW | Constructor accepts `"Local"` (LoadLocation special success), reintroducing `time.Local`; rejected only at config, not at the seam; no constructor detector. | `PLAN-TIME.md:22-24,32-34,68-69` |

## Required revision before implementation

1. Add `"Local"` to the constructor's fail-closed set (reject `""`,
   `"Local"`, and LoadLocation errors), and add a `"Local"` case to the
   constructor fail-closed detectors.

VERDICT: FAIL

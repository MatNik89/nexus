# PLAN-TIME v6 design review (Codex, round 6)

## Artifact binding

- Branch: `slice/p0-time`
- Commit: `8093d292ac6bcfdf6183031120af7c8e93c8fd61`
- Tree: `e17cca2bfd4d362a8780bc65bbf7d9fdd3be22c0`
- `docs/PLAN-TIME.md` blob: `940e9feddebb4bff19fde95e0cd2c0e3c03e01ef`
- Required marker: `REJECTS the special value "Local"` present at `docs/PLAN-TIME.md:32`
- Clean export: `/home/matej/HARNESS/nexus-review-time-plan6-8093d29-codex` (no `.git`; removed after review)
- Review mode: static design only. No implementation or production tests were run.

## Round-5 closure check

The v6 text closes the substantive round-5 defects: `Local` is explicitly rejected while `UTC` is accepted; the planner detectors use the real name seam and real Zagreb winter/summer rules; nil clock and config failure cases are named; a non-default New York case crosses the production composition root; `buildDaemon` is correctly identified as the only planner owner; and the stale section header is corrected. The two issues below remain.

## Findings

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:28-37,74-79` - Timezone ownership is still contradictory. The executable contract makes `resolved.Config.Timezone` authoritative and expressly prohibits `time.Local`, but the non-goal still says "the host zone IS the owner's zone." These diverge whenever the host zone differs from the configured value and leave future maintainers with two claimed owners for the same civil-time meaning.

PROBE: Direct static contradiction between the config/`buildDaemon` owner at `docs/PLAN-TIME.md:28-37` and the host-zone owner at `docs/PLAN-TIME.md:78-79`. A host configured as UTC with `timezone: "Europe/Zagreb"` gives opposite answers under the two clauses.

FIX: Replace the stale sentence with: `No per-user timezone config; the single configured IANA timezone is authoritative for the owner.`

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:28-31,48-72` - The required default `Europe/Zagreb` has no red-capable detector. Every listed positive path supplies an explicit zone (`Europe/Zagreb` directly to the planner, `America/New_York` through config, and `UTC` in validation). Therefore changing `defaults()` to `UTC` or an empty value can leave the planner, DST, non-default composition-root, and validation cases green; an empty wrong default may fail only incidentally at startup rather than identify the default-contract regression. The current config suite already has a default-origin seam (`internal/foundation/config/config_test.go:24-53,93-102`) that can lock this cheaply.

PROBE: Controlled static ablation analysis: replace only the proposed `defaults()` timezone value while retaining all detectors enumerated at `docs/PLAN-TIME.md:48-72`; none asserts the resolved no-override value is `Europe/Zagreb` with `OriginDefault`.

FIX: Extend the config precedence/default detector to resolve with no timezone override and assert both `Config.Timezone == "Europe/Zagreb"` and `Origins["timezone"] == OriginDefault`; mutate only the default to `UTC` and require RED.

## Simplification and proof ceiling

The design otherwise stops at the correct minimal rung: one typed string, one loaded location, the existing config resolver, and the existing composition-root test. Both fixes are documentation/test assertions; neither requires another abstraction, dependency, or runtime path.

Proof ceiling: this review establishes static design consistency against commit `8093d29`; it does not establish runtime behavior because implementation and execution were explicitly out of scope.

VERDICT: FAIL

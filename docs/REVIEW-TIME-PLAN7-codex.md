# PLAN-TIME v7 design review (Codex, round 7)

## Artifact binding

- Requested branch resolved to commit `00fd1b3f2777272ad00efc428da4069392ae761d` during preflight.
- Tree: `ad86ade440231b6df70f535f4a2389fdf7dd0563`.
- `docs/PLAN-TIME.md` blob: `0f030c9d71c708ff60ff6f6b2a0de2ba7c333060`.
- Exported file SHA-256: `64e708a9c849f4077c73a1d39313b6159450a75a27b3d079083dc84363bdef8a`.
- Required marker `AT BOTH LAYERS` is present at `docs/PLAN-TIME.md:33`.
- Clean export: `/home/matej/HARNESS/.codex-review-time-plan7-00fd1b3` (outside `/tmp`; removed after review).
- Review mode: static design only. No implementation or production tests were run.

## Round-6 closure check

The v7 delta closes both round-6 findings: the stale host-zone ownership sentence is gone, and a no-timezone-key config case now locks the resolved default to exactly `Europe/Zagreb` through the existing default-origin seam. The config and detector sections also say that `Local`, empty, and unloadable names are rejected at both the config and planner-constructor layers. Those closures do not remove the two design inconsistencies below.

## Findings

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:19-27,28-38,69-77` - The plan's definitive constructor contract and its dedicated constructor-failure list still omit the required `Local` rejection. They say construction rejects an empty name or a `LoadLocation` error, and later repeat only empty and unloadable constructor cases. `time.LoadLocation("Local")` succeeds, so implementing either canonical-looking list literally admits `time.Local`, contradicting the separate statement that the planner constructor rejects `Local` and that there is no `time.Local` use. The phrase `AT BOTH LAYERS` therefore exists, but the constructor owner remains internally inconsistent.

PROBE: Static contract comparison. Go's `time.LoadLocation` documentation states that `"Local"` returns `time.Local`; it is not a load error. The constructor bullets at lines 19-27 and 76-77 therefore do not imply the explicit rejection required at lines 32-38 and 69-71.

FIX: Add `name == "Local"` to the definitive constructor rejection sentence and include `Local` in the dedicated constructor fail-closed list, leaving the existing both-layer detector requirement intact.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:86-88` - "The CONFIG field is the ONE owner of civil time" is unqualified and conflicts with the already-owned reminder contract. The shipped scheduler says a reminder's persisted `WallTime.TZ` is the source of truth (`internal/schedule/schedule.go:49-58`), while HARDQ B1 requires each persisted occurrence to retain its IANA zone (`docs/HARDQ-CONSOLIDATED.md:42-48`) and Annex P2.6 assigns schedule semantics to S3.6 (`docs/HARNESS-SPEC.md:1516-1523`). Taken literally, the new sentence either creates two owners or directs a future implementation to replace explicit reminder-zone intent with the conversational default.

PROBE: Static owner sweep from the plan's unqualified civil-time claim to the existing scheduler contract and higher-ranked schedule owners. `WallTime.TZ` accepts zones other than `Europe/Zagreb` (for example `Australia/Lord_Howe` in `internal/schedule/schedule_test.go:304-324`), so this is not merely duplicate storage of the config value.

FIX: Scope the claim to this slice: `Config.Timezone is the one owner of the conversational current-time anchor/default civil zone; explicit schedule manifests continue to own their requested IANA zone.`

## Simplification and proof ceiling

The implementation shape remains minimal: reuse the existing clock seam and config resolver, load one validated location in the planner, and append one derived line. Both findings are owner-text corrections; neither needs a new abstraction, dependency, runtime state, or second timezone fallback.

Weakest link: this is a static plan review; it establishes design consistency only, not that the future implementation or detectors behave as specified.

Proof ceiling: no runtime behavior, cache behavior, tzdata availability, or RED-to-GREEN detector capability was executed because the requested scope was design only.

VERDICT: FAIL

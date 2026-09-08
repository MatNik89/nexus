# PLAN-TIME v4 design review (Codex, round 4)

## Artifact binding

- Branch: `slice/p0-time`
- Commit: `ae2d2364ed225930d621f945b7a643eb3895d044`
- Tree: `115e604fd6c2dc9f15c0836e0ee307ca1e442bed`
- `docs/PLAN-TIME.md` blob: `4bbb2b268e589a046628f53bf36b9693da38825f`
- Clean export: `/home/matej/HARNESS/nexus-review-time-plan4-ae2d236-codex` (no `.git`; removed after review)
- Review mode: static design only. No implementation or production tests were run.

## Claimed-fold check

The immutable v4 document does not contain all folds described in the review request. The `clockid.Real` name is corrected to `clockid.System` (`docs/PLAN-TIME.md:24`), and two string-zone rejection cases are added (`docs/PLAN-TIME.md:52-53`). The central named-IANA-zone design, rendered IANA identifier, typed config field/default, fail-closed load contract, nil case, and production-wiring detector are not specified by the committed text.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:19-25,30-38` - The round-3 IANA-identity flaw remains. The seam is still specified as `clockid.Clock AND *time.Location`, and the render is still built from `t.Zone()`. Go's `Time.Zone` returns only the active abbreviation and offset, so the documented example still emits `CEST (UTC+02:00)`, not `Europe/Zagreb`. The current reminder contract requires an IANA string (`internal/obligation/tools.go:21-24`) and rejects an unloadable zone (`internal/schedule/schedule.go:60-68`). Saying that production uses "the configured IANA zone" does not preserve or render that identifier once the constructor receives only `*time.Location`.

PROBE: Static type/data-flow trace plus `go doc time.Time.Zone`, which states that `Zone()` returns an abbreviated name such as `CET`.

FIX: Define one small named-zone value (`Name string`, `Location *time.Location`) constructed from a validated IANA name, inject that value, and render `Name` plus `Zone()` abbreviation and numeric offset.

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:24-29,39-41` - The failure policy is contradictory. A configured non-special IANA name requires `time.LoadLocation`; if its database cannot be found, `LoadLocation` returns an error and the claimed fail-closed constructor must reject startup. The plan instead retains the old `time.Local` behavior and declares silent UTC fallback with no degraded-state detection. These are mutually exclusive production outcomes, so an implementer cannot determine whether missing tzdata must reject or silently change the owner's civil-time semantics.

PROBE: `go doc time.LoadLocation` shows the searched IANA database sources and its error result; the target text simultaneously specifies configured-IANA wiring and silent UTC fallback.

FIX: Delete the stale UTC-fallback clause and state that a non-empty configured IANA name must load successfully or planner/daemon construction fails before any provider call.

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:24-25,58-63` - The purported timezone configuration has no executable design contract. The plan names neither a field/key nor its `Europe/Zagreb` default, precedence, validation owner, or production call-site propagation, and it still says the host zone is the owner's zone. The current config owner is closed and typed: a field must be added to `Config`, `keySchema`, `defaults`, `applyValue`, and bounds validation (`internal/foundation/config/config.go:28-47,79-98,113-120,276-340`). Without those obligations, the claimed default/config fold can be omitted while the plan's listed detectors remain green.

PROBE: Static sweep of the complete config resolution path and the production planner factory at `cmd/nexus/main.go:452-580`; no v4 clause or detector binds a timezone key from resolution to planner construction.

FIX: Specify `timezone` as a non-empty typed string with default `Europe/Zagreb`, normal layer precedence and origin/hash participation, fail-closed `LoadLocation` validation, and explicit propagation through `buildDaemon` into both planner constructors; replace the stale host-zone non-goal wording.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:45-56` - The detector contract cannot prove the claimed seam or failure boundary. It still says the unit injects `*time.Location` created by `time.FixedZone`, which has no IANA database behavior and no separate stable IANA identifier, yet its constructor cases pass empty/unloadable zone names, which are strings rather than `*time.Location` values. It also omits the requested nil dependency case and a production composition-root check. Therefore an abbreviation-only render, a UTC production wiring, or a nil-location panic can survive the stated proof surface.

PROBE: Contract/type comparison; `go doc time.FixedZone` confirms it constructs a fixed-offset location, and `go doc time.Time.In` confirms a nil location panics. The current production factory is `cmd/nexus/main.go:572-580`, outside the named unit detector.

FIX: Rewrite the detector contract around the named-zone constructor: reject empty/unloadable names and nil clock, assert the exact IANA+abbreviation+offset line with a real loaded DST zone, and add a controlled non-UTC composition-root capture whose timezone-to-UTC ablation turns RED before the provider call.

## Simplification and proof ceiling

The smallest coherent design is still one clock plus one named-zone value. No time tool, fallback state machine, new package, or additional runtime branch is needed. The current text is incomplete rather than under-abstracted.

Proof ceiling: this review establishes static design consistency against commit `ae2d236`; it does not establish runtime behavior because implementation and execution were explicitly out of scope.

VERDICT: FAIL

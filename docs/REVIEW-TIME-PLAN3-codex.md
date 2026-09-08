# PLAN-TIME v3 design review (Codex, round 3)

## Artifact binding

- Branch: `slice/p0-time`
- Commit: `e72b38924486a3c26b9cc708978a4e7747d46c8e`
- Tree: `1e4bc1127d847e6f306e2664d09beab4011b7140`
- `docs/PLAN-TIME.md` blob: `15afdeaf4f42e645bb41404d65589f7b02fe5e1d`
- Clean export: `/home/matej/HARNESS/nexus-review-time-plan3-e72b389-codex` (no `.git`; removed after review)
- Review mode: static design only. No implementation or production tests were run.

## Round-2 closure check

The four stated round-2 folds are present: the location is a separate injected dependency and conversion uses `clock.Now().In(loc)` (`docs/PLAN-TIME.md:19-25`); missing host zone data is explicitly accepted as a UTC fallback (`docs/PLAN-TIME.md:26-29`); the stale `nowFn` detector block is absent and only one detector section remains (`docs/PLAN-TIME.md:45-54`); and no character-class invariant is claimed for runtime zone bytes (`docs/PLAN-TIME.md:35-38`). These closures do not resolve the findings below.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:30-34` - The proposed value cannot provide the IANA zone required by the reminder boundary. The plan describes `<IANA-or-abbrev>` but explicitly constructs it with `t.Zone()`, whose contract returns an abbreviation such as `CET`, not an IANA identifier. The current reminder tool asks the model for `"tz":"Europe/Zagreb"` (`internal/obligation/tools.go:21-24`), and the scheduler rejects anything that `time.LoadLocation` cannot resolve as an IANA zone (`internal/schedule/schedule.go:60-68`). Consequently, a host-local result such as `CEST (UTC+02:00)` is enough to describe the current instant but cannot identify the owner's civil-time rules for a future reminder across a DST transition; the plan's stated reminder rationale is not satisfied.

PROBE: Static end-to-end trace from planned render to the current reminder schema and scheduler validation; `go doc time.Time.Zone` confirmed that `Zone()` returns the abbreviated name and numeric offset.

FIX: Inject one validated named-zone descriptor containing both the IANA ID and `*time.Location`, render the IANA ID plus current offset, and add a DST-boundary detector proving a relative reminder uses that same IANA ID.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:22-25` - The production wiring names `clockid.Real`, but the target tree has no such type; the production implementation is `clockid.System` (`internal/foundation/clockid/clockid.go:26-30`). An implementation that follows the plan literally does not compile.

PROBE: Repository-wide symbol search at the bound tree found `clockid.System` and no declaration of `clockid.Real`.

FIX: Specify `clockid.System{}` as the production clock.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:22-25` - The new pointer dependency has no fail-closed constructor invariant. `clock.Now().In(loc)` panics when `loc == nil`, while the current planner constructors explicitly reject missing dependencies (`internal/llm/planner/planner.go:128-147`). The detector contract (`docs/PLAN-TIME.md:45-54`) has no nil-location case, so this failure boundary is neither designed nor locked.

PROBE: `go doc time.Time.In` confirmed that `Time.In` panics for a nil location; static comparison against the existing constructor validation established the missing contract.

FIX: Require non-nil clock and location in both constructor paths, return a construction error, and add a detector that passes a nil location and observes rejection before any provider call.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:45-54` - The sole detector contract exercises only fake-clock/fixed-zone planner units. It does not exercise the production composition root, so wiring `time.UTC` instead of `time.Local` would compile and all specified detectors could remain green even though the owner sees UTC on a non-UTC host. This leaves the production-wiring claim at `docs/PLAN-TIME.md:24-25` self-unverified.

PROBE: Constructor/call-site sweep found the production planner factory is owned by `cmd/nexus` through `daemon.Deps.PlannerFactory` (`internal/app/daemon/daemon.go:32-40`), while the plan names no composition-root detector for that path.

FIX: Add one subprocess-safe composition-root detector with a controlled non-UTC `TZ` that captures the provider request and proves production wiring emits the expected local line; ablate the production location argument to UTC and require RED.

## Simplification and proof ceiling

The implementation shape itself is lean: no time tool, package global, fallback branch, new package, or dependency is justified. The necessary repair is to make the existing injected location dependency carry the stable zone identity and to close constructor/wiring proof, not to add a time subsystem.

Proof ceiling: this review establishes static coherence of the plan against commit `e72b389`; it does not establish runtime behavior because implementation and execution were explicitly out of scope.

VERDICT: FAIL

# PLAN-TIME v5 design review (Codex, round 5)

## Artifact binding

- Branch: `slice/p0-time`
- Commit: `36e323b48f9733040db4ed94b7fb0e5efa6429d6`
- Tree: `a39f00e7256a7a024842c9eb5c9d3ee31f96de18`
- `docs/PLAN-TIME.md` blob: `30da2cf0b594f9a85be16e1bdc1675ee7d173041`
- Required marker: `CLOCK + TIMEZONE SEAM (v5` present at `docs/PLAN-TIME.md:19`
- Clean export: `/home/matej/HARNESS/nexus-review-time-plan5-36e323b-codex` (no `.git`; removed after review)
- Review mode: static design only. No implementation or production tests were run.

## Round-4 closure check

The committed v5 text now defines a string IANA-name seam that stores both the name and loaded location, emits the stored name plus abbreviation and offset, removes silent UTC fallback, uses `clockid.System`, and enumerates the typed config-owner changes. Those corrections close the main round-4 representation and failure-policy findings, subject to the remaining flaws below.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:19-27,28-32` - `time.LoadLocation` success is not sufficient to prove that the configured value is an IANA database name or that `time.Local` is absent. Go defines the special input `"Local"` to return `time.Local` successfully. Therefore `timezone: "Local"` passes both proposed validation calls, directly violates the plan's "NO time.Local use" invariant, renders `Local` rather than a stable IANA identifier, and lets the same persisted reminder intent resolve differently when the host zone changes.

PROBE: `go doc time.LoadLocation` states: `If the name is "Local", LoadLocation returns Local.` Static tracing shows the proposed validator has no rejection rule beyond empty name and `LoadLocation` error.

FIX: Reject `Local` explicitly (and document whether the special `UTC` spelling is accepted); require the configured name to be a stable IANA identifier before calling `LoadLocation`, with a detector proving `Local` cannot construct config or planner state.

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:44-55` - The sole positive detector still targets the deleted API: it injects `*time.Location` made by `time.FixedZone`, while the definitive v5 constructor accepts an IANA name string and internally calls `LoadLocation`. `FixedZone` neither exercises IANA loading nor carries DST rules, so this detector cannot prove the stored-name contract, the real `Europe/Zagreb` transition behavior, or even compile against the specified constructor without adding an undocumented alternate seam. The same detector block also omits nil-clock rejection even though the new clock dependency is dereferenced on every plan.

PROBE: Direct contract comparison between `docs/PLAN-TIME.md:19-27` and `docs/PLAN-TIME.md:44-52`; `go doc time.FixedZone` confirms it creates a fixed-offset location rather than loading IANA rules.

FIX: Replace the stale `FixedZone` detector with `New(..., fakeClock, "Europe/Zagreb")`, pin instants on both sides of a real DST transition, assert the exact stored-name/abbreviation/offset output, and add nil-clock plus `Local` constructor rejection cases.

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:28-33,44-55` - No detector binds the new config field through the production composition root. Config-unit and planner-unit checks can both pass while `buildDaemon` ignores `resolved.Config.Timezone`, hard-codes the default, or passes the wrong field. The current production owner is the `buildDaemon` planner factory (`cmd/nexus/main.go:452-580`), and the existing production-spine test captures provider requests (`cmd/nexus/main_test.go:42-99`), but the plan does not require extending it. Thus the claimed configured-zone wiring has a false-green path.

PROBE: Static producer-to-consumer sweep from `config.Resolve` through `buildDaemon` to `daemon.Deps.PlannerFactory`; none of the listed v5 detectors observes that chain. Controlled ablation candidate: replace `resolved.Config.Timezone` with `"Europe/Zagreb"`; all specified unit detectors remain green for a non-default configured zone.

FIX: Add one composition-root detector that resolves a non-default real IANA zone, captures the provider's last user message through `buildDaemon`, and turns RED when the production argument is replaced by the default or UTC.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:32-33` - The wiring instruction assigns planner construction to `runChat`, but `runChat` is only a UDS client (`cmd/nexus/main.go:673-686`); the daemon process exclusively owns `buildDaemon` and `PlannerFactory` (`cmd/nexus/main.go:131-142,452-580`). Making `runChat` pass a timezone into a planner would either be impossible or introduce a second composition path contrary to the daemon-owned architecture.

PROBE: Static call graph: `runDaemon -> buildDaemon -> daemon.Deps.PlannerFactory -> planner.NewStreaming`; `runChat -> repl.Run` and never constructs a planner.

FIX: State that `buildDaemon` passes `resolved.Config.Timezone` into the planner factory; `runDaemon` reaches it through `buildDaemon`, while `runChat` remains unchanged.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:57-62` - The non-goal still says the host zone is the owner's zone, contradicting the definitive rule that `time.Local` is never used and the configured `timezone` (default `Europe/Zagreb`) is authoritative. This leaves ownership ambiguous when host and configured zones differ.

PROBE: Direct contradiction between `docs/PLAN-TIME.md:24-32` and `docs/PLAN-TIME.md:61-62`.

FIX: Replace the stale sentence with: `No per-user timezone; the single configured IANA timezone is authoritative for the owner.`

## Simplification and proof ceiling

The minimum coherent implementation remains one clock, one validated zone name, and one loaded location stored by the planner. Rejecting the `Local` sentinel and repairing the existing detector/wiring text require no new package, alternate constructor, or fallback state.

Proof ceiling: this review establishes static design consistency against commit `36e323b`; it does not establish runtime behavior because implementation and execution were explicitly out of scope.

VERDICT: FAIL

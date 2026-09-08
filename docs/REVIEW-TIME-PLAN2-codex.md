# PLAN-TIME v2 design review — Codex, round 2

## Scope and artifact binding

- Review target: branch `slice/p0-time`, commit `07551ab3eff2f177353148cca67b28eb8a886a14`.
- The branch ref resolved to that exact commit. The reviewed `docs/PLAN-TIME.md` blob is `742c9b505eaa6c840744d990b44dc93db6dee247` (SHA-256 `8b05c8f5e8fb98e286f5481cd50ba96f0d93ac61b68cf9020c3d9439965d08ee`).
- Evidence came from the clean, gitless on-disk export `/home/matej/HARNESS/nexus-review-07551ab-codex.gFxZkh`, not from the dirty working tree. Existing working-tree changes and other review files were excluded from substantive evidence.
- Design-only review: no implementation changes and no production tests.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:19-30,40-44` — The named clock seam cannot produce or test the promised local-zone line. The plan says `ChatPlanner` receives only `clockid.Clock`, production uses that clock, and `clockid.NewFake` is initialized with a `time.FixedZone` instant. At the reviewed revision, however, `internal/foundation/clockid/clockid.go:26-30` makes the production clock return `time.Now().UTC()`, while `internal/foundation/clockid/clockid.go:50-58` stores only Unix nanoseconds and reconstructs every fake time with `.UTC()`. The input location is therefore discarded, `t.Zone()` is `UTC`, and the example `CEST (UTC+02:00)` is neither produced in production nor reachable through the specified fake. The phrase “an injected fixed `*time.Location`” has no defined owner or constructor seam.

PROBE: Static call-path trace from `ChatPlanner.Plan` (`internal/llm/planner/planner.go:235-263`) through the proposed constructor dependency to both concrete clock implementations.

FIX: Inject the display `*time.Location` explicitly beside `clockid.Clock`, use `clock.Now().In(location)`, wire `time.Local` in production and `time.FixedZone` in tests; alternatively define one display-time source that preserves location and name that exact interface and wiring.

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:33-35,54-55` — “`Local` cannot fail” confuses an errorless API with guaranteed local-zone resolution. Go's Unix implementation attempts `$TZ` or `/etc/localtime` and silently falls back to UTC when loading fails (`$GOROOT/src/time/zoneinfo_unix.go:28-68`). Thus removing an impossible error-return branch is correct, but the plan still cannot guarantee its stated invariant that the host zone is the owner's zone. On a missing or invalid host zone database it will confidently show UTC, not detect the degraded state.

PROBE: Static inspection of the installed Go standard library's `time.initLocal` implementation, including its unconditional UTC fallback.

FIX: Either validate/load the intended location at startup through an error-returning owner and fail or report degradation explicitly, or amend the requirement and detector to accept documented UTC fallback. Do not claim that errorless access proves successful host-zone resolution.

[CONCRETE][SEV: MED] `docs/PLAN-TIME.md:57-63` — A stale second `## Detectors` section directly contradicts v2. It still mandates a package-level `nowFn`, requires the time line in the system message, and describes system-prompt determinism using that obsolete seam. These instructions conflict with the constructor clock and last-user-message requirements at lines 19-48, so the plan does not have one executable detector contract and still authorizes both round-1 regressions.

PROBE: Internal contradiction analysis within the immutable `PLAN-TIME.md` blob.

FIX: Delete lines 57-63; the v2 detector section at lines 39-48 is the sole owner.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:23-32` — The asserted closed zone-name grammar is not enforced by construction. `t.Zone()` returns the location's zone name unchanged, and `time.FixedZone(name, offset)` accepts an arbitrary name; the Go implementation stores `name` without validating it (`$GOROOT/src/time/zoneinfo.go:112-139`). No validation or escaping step in the plan establishes `[A-Za-z0-9+/_-]+`. This is not user/model input in the proposed production wiring, so the immediate security impact is low, but the stated invariant and its detector are false.

PROBE: Static inspection of Go's `time.FixedZone` and `fixedZone` implementation.

FIX: Validate the abbreviation against the declared grammar and choose a deterministic safe substitute on mismatch, or omit the abbreviation and retain the numeric offset as the sole closed field.

[CONCRETE][SEV: LOW] `docs/PLAN-TIME.md:19-22` — The production wiring names `clockid.Real`, but no such type exists at this revision; the production implementation is `clockid.System` (`internal/foundation/clockid/clockid.go:26-30`). The plan is not mechanically executable as written.

PROBE: Full-tree symbol search in the clean export plus inspection of the canonical clock package.

FIX: Replace `clockid.Real` with `clockid.System{}` (or deliberately introduce and justify a rename, which would be a larger unrelated change).

## Counterargument and decision

The strongest counterargument is that UTC plus a numeric offset is still an unambiguous instant and the target is a single controlled Linux host. That does not resolve the feature contract: the plan explicitly promises the owner's host-local wall time, its example and detector require CEST, and its chosen production and fake clocks erase that zone. The contradictory detector section independently prevents PASS.

## Topknot and proof ceiling

The no-tool, one-line prompt approach is the smallest plausible mechanism. The avoidable machinery is the stale duplicate detector contract: delete seven lines; otherwise no new abstraction is justified beyond an explicit location seam needed to satisfy the stated behavior. `net: -7 lines possible.`

This static review establishes design contradictions at the bound revision. It does not establish implementation behavior, compilation, detector RED capability, model answer quality, or provider cache behavior; those require the later RED-to-GREEN implementation phase.

VERDICT: FAIL

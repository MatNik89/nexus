# Review of Design Plan: Current Time in Model Context (`docs/PLAN-TIME.md`)

**Scope**: `docs/PLAN-TIME.md` at commit `9677f16` on branch `slice/p0-time`.
**Reviewer**: kilo
**Date**: 2026-09-07
**Base**: `9425baa` (`origin/main`)

## Summary

The plan proposes appending a formatted date/time line to the system
prompt in `ChatPlanner.Plan` (stateless, no tool), with a `nowFn` seam,
minute resolution, host timezone and UTC fallback. The no-tool non-goal is
sound; the cache reasoning is essentially correct. The plan still carries
several unresolved design flaws — the timezone representation is
underspecified (and the literal layout verb is invalid), the determinism
guard and the unit detector are not host/clock-deterministic as written,
and the clock seam deviates from the repo's established pattern. Any one
of these blocks implementation.

## Attack surface assessment

### 1. Injection surface of the formatted time string — sound (non-blocker)

The time line carries **zero untrusted bytes**: the date, time and weekday
are numeric/derived, and the zone name comes from host tzdata (`CET` /
`CEST` for `Europe/Zagreb`). Appending it **unfenced** to the system
message is correct — it is daemon-generated trusted context, exactly like
`systemPrompt` (`planner.go:78-82`) and the SEALED tool descriptions, and
must not be fenced or the model would discount it as DATA.

One residual: if the zone were ever rendered from a POSIX `TZ` env string
(e.g. `TZ=ABC-3` → abbreviation `ABC`), an operator-controlled env value
could inject a short arbitrary token. `TZ` is set at daemon launch by the
owner (single-owner threat model), so this is out of scope, but a one-line
guarantee ("zone name is host-tzdata-derived, never from a user-provided
`TZ`") would close it. The plan does not state this. Minor, non-blocking.

### 2. Cache / determinism claims — one real flaw

The cache premise (`PLAN-TIME.md:38-40`) is **correct**: the time line is
appended at the *tail* of `system` (`planner.go:246-263`), and DeepSeek
prefix caching tolerates a changing tail — the stable prefix
(`systemPrompt` + `toolProtocol` + tool list) remains cacheable, and minute
resolution makes the whole system message reusable within a minute. The
history/user messages change every turn regardless, so they were never
cacheable across turns. This is not the "inverted premise" a prior review
claimed.

The genuine flaw is the **determinism guard**: the plan states
"TestToolPromptDeterministic keeps passing … the time line is identical
within one pinned nowFn" (`PLAN-TIME.md:33-35`). But
`TestToolPromptDeterministic` (`planner_test.go:300-332`) does **not** pin
`nowFn`; it runs 8 iterations against the real clock and asserts the system
prompt is **byte-identical** (`len(prompts) != 1`). Under minute
resolution, if the loop straddles a minute boundary the minute digit
changes and the test flakes RED. The parenthetical already names the
condition ("within one pinned nowFn") yet the plan never specifies pinning
`nowFn` in that existing test. The claim "keeps passing" is therefore false
as written. **Blocking.**

### 3. The no-tool non-goal — sound

A prompt line answers "koliko je sati" with no effect path, no S7 grant,
no tool round-trip; the dogfood only asks for current-time awareness, which
this satisfies at minute resolution. Per-user timezone and sub-minute
precision are correctly deferred. Sound.

### 4. Detector adequacy — two blocking gaps

- **Underspecified timezone representation.** The literal
  `"\nCurrent date and time: <2006-01-02 15:04 (Monday), TZ>"`
  (`PLAN-TIME.md:16`) uses `TZ`, which is **not** a Go layout verb — it
  would print the literal characters `"TZ"` (Go reference layout is
  `Mon Jan 2 15:04:05 MST 2006`; the verbs are `MST`, `-07:00`/`Z07:00`).
  Read charitably as a placeholder, the plan still does not say whether the
  rendered zone is the abbreviation (`CEST`), the IANA name
  (`Europe/Zagreb`), or a numeric offset. The unit detector asserts "the
  exact formatted line" (`PLAN-TIME.md:30-32`) against this unspecified
  value, so the detector cannot be written portably or verified against a
  concrete expected string. **Blocking.**
- **Host-timezone non-determinism.** The detector pins `nowFn` but the
  output is formatted in `time.Local`, so the expected string depends on
  the runner host (`CEST` in Zagreb vs `UTC` in CI vs `EDT` elsewhere). The
  plan does not state that the expected value must be *derived* from
  `nowFn().In(loc)` (rather than hardcoded) nor that the location must be
  injectable. Without that, "exact formatted line" is environment-dependent.
  **Blocking.**

## Standards

- **Clock seam deviation (LOW/MEDIUM).** The repo's documented rule is
  "no component invents its own clock" (`clockid.go:1-5`) and the
  established seam is constructor-injected `now func() time.Time`
  defaulting to `time.Now` when nil (`s7min.go:76-91`,
  `effectpath.go:106-121`). The plan's package-level `var nowFn = time.Now`
  (`PLAN-TIME.md:18-19`) is mutable global state (test pollution / races
  under `t.Parallel`) and deviates from that idiom. It also leaves a second,
  unrelated time source in the same function (`planner.go:222`,
  `Deadline: time.Now().Add(...)`), fragmenting clocks. Inject `now` as a
  `ChatPlanner` field.

## Secondary findings

- **Fallback branch ill-defined and untested (LOW).** "If the zone lookup
  fails, fall back to UTC" (`PLAN-TIME.md:18-20`) has no Go analogue:
  `time.Local` never fails (it is UTC when the OS lacks tzdata). The plan
  neither names the failing lookup nor adds a detector for the fallback.
- **Placement vs tool grammar (LOW).** "Append to the system content"
  (`PLAN-TIME.md:14-16`) lands the line *after* `toolProtocol` + tool list
  (`planner.go:246-259`), directly under `Available tools:`. Specify that
  the anchor is appended to `systemPrompt` **before** `toolProtocol`.

## Findings table

| ID | Area | Severity | Finding | Source |
| :-- | :-- | :-- | :-- | :-- |
| K1 | Spec/Go | HIGH | `TZ` is not a valid Go layout verb; zone representation (abbrev vs IANA vs offset) unspecified, so "exact formatted line" is undefinable. | `PLAN-TIME.md:16,30-32` |
| K2 | Testing | HIGH | Determinism guard claims `TestToolPromptDeterministic` "keeps passing" but that test pins no clock; byte-identity flaky at minute boundaries. | `PLAN-TIME.md:33-35`, `planner_test.go:300-332` |
| K3 | Testing | MEDIUM | Unit detector formats in `time.Local`; expected string host-dependent unless derived from `nowFn().In(loc)` and location injectable — unspecified. | `PLAN-TIME.md:30-32` |
| K4 | Standards | MEDIUM | Package-level `nowFn` violates constructor-injected clock seam; fragments `planner.go:222`. | `PLAN-TIME.md:18-19`, `clockid.go:1-5`, `s7min.go:84-91` |
| K5 | Spec | LOW | "Zone lookup fails → UTC" has no Go analogue and no detector. | `PLAN-TIME.md:18-20` |
| K6 | Prompt | LOW | Anchor placement vs `Available tools:` grammar unspecified. | `PLAN-TIME.md:14-16`, `planner.go:246-259` |

## Disagreements with prior review (agy)

- The inverted-cache "HIGH" finding is wrong: the anchor is at the tail of
  `messages[0]`, and the changing-history argument is irrelevant since
  history/user messages were never cross-turn cacheable. The plan's cache
  premise stands.
- "Numeric offset for RFC 3339 tool args" is out of scope: scheduling is an
  explicit later slice; no P0 tool asks the model to mint timestamps.

## Required revisions before implementation

1. Specify a valid Go layout (e.g. `2006-01-02 15:04 (Monday) MST` or with
   `-07:00`) and state exactly what the zone component renders.
2. Inject the clock as a `ChatPlanner` field (`now func() time.Time`,
   `nil` → `time.Now`), replacing the package-level var; use it for both the
   anchor and `planner.go:222`.
3. Make the unit detector host-independent: derive the expected string from
   the pinned clock and an injectable location; cover both plain and
   WithTools planners and the UTC fallback.
4. Pin the clock in `TestToolPromptDeterministic` (or assert only the
   tool-ordering suffix, not full byte-identity) so the determinism guard
   cannot flake at a minute boundary.
5. State the anchor's placement relative to `toolProtocol` (before it).
6. Define the fallback condition in Go terms and add a detector for it.

VERDICT: FAIL

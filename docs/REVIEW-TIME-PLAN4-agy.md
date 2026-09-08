# Review of Design Plan: Current Time in Model Context — Round 4 (`docs/PLAN-TIME.md`)

**Scope**: Review of `docs/PLAN-TIME.md` (commit `ae2d236` on branch `slice/p0-time`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `9425baaf20b5f13bd3ef92616aa22fb92073f790` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TIME.md` at commit `ae2d236` (v4) attempts to address the round-3 findings by correcting `clockid.Real` to `clockid.System` and adding constructor fail-closed test cases. However, a static audit of commit `ae2d236` reveals **critical unresolved design gaps and internal contradictions**:

1. **Missing IANA Identifier in Rendered Line & Scheduler Breakdown (HIGH)**:
   The rendered line specified in `PLAN-TIME.md:35-36` is still `"\nCurrent date and time: 2026-09-08 04:55 Monday, CEST (UTC+02:00)"`. It completely omits the IANA identifier (e.g., `Europe/Zagreb`). Because `reminder_set` (`internal/obligation/tools.go:21-24`) requires an IANA zone name (`"tz":"Europe/Zagreb"`) and `schedule.WallTime.validate()` (`internal/schedule/schedule.go:65-68`) rejects abbreviations like `"CEST"` with `schedule: unknown IANA zone (fail closed)`, any reminder created by the model from the prompted string will fail validation.
2. **Direct Constructor Seam Contradiction (MEDIUM)**:
   The document is internally split on what the constructor actually accepts:
   - Line 22-23 states: `ChatPlanner gains constructor-injected clockid.Clock AND *time.Location;` (takes `*time.Location`).
   - Line 24-25 states: `Production wires clockid.System + the configured IANA zone; tests wire clockid.NewFake + a fixed IANA name.` (takes a `string`).
   - Line 46-47 states: `injected fixed *time.Location (test uses time.FixedZone...` (takes `*time.Location`).
   - Line 52-53 states: `Constructor fail-closed cases: empty zone name and an unloadable zone name both REJECT construction` (takes a `string` and calls `time.LoadLocation`).
   If the parameter is `*time.Location`, passing `time.FixedZone` cannot test empty/unloadable strings inside `New`. If the parameter is `string`, line 22-23 and line 47 are invalid.
3. **Unspecified Configuration Field (LOW)**:
   Line 24 refers to "the configured IANA zone" without specifying the field name in `config.Config`, its default value (`"Europe/Zagreb"`), startup bounds validation in `Config.Validate()`, or call-site wiring in `cmd/nexus/main.go:580`.

Because unresolved flaws exist across high, medium, and low severity tiers, the plan is **rejected**.

---

## Standards Review

### Documented Standards Compliance

- **[VIOLATION] Internal Coherence & Interface Design (`PLAN-TIME.md:22-25, 46-48, 52-53`)**:
  - The plan simultaneously specifies `ChatPlanner` receiving `*time.Location` (lines 22-23, 46-47) AND receiving an IANA zone name `string` that is resolved via `time.LoadLocation` inside the constructor (lines 24-25, 52-53).
  - The interface signature and dependency types must be unified and unambiguous.

- **[VIOLATION] Integration with Obligation/Scheduler Contracts (`internal/obligation/tools.go:21-24`, `internal/schedule/schedule.go:60-68`)**:
  - `reminder_set` expects a valid IANA zone string in `"tz"`. Providing only `CEST (UTC+02:00)` forces the model to emit `"tz":"CEST"`, which `schedule.WallTime.validate()` unconditionally rejects.

- **[COMPLIANT] Repository Language Invariant (`AGENTS.md`)**:
  - All prompt additions and documentation are in English.

---

## Spec & Design Review

### 1. Missing IANA Identifier in Rendered Line (High Severity)

- **Plan Claim**: `PLAN-TIME.md:35-36` — `The full line: "\nCurrent date and time: 2026-09-08 04:55 Monday, CEST (UTC+02:00)"`
- **Flaw**:
  - `t.Zone()` returns the abbreviation (`CEST` / `CET`), NOT the IANA location identifier (`Europe/Zagreb`).
  - `internal/obligation/tools.go:21-24` defines `reminder_set` schema as `{"id":"...","body":"...","year":2026,"month":9,"day":5,"hour":12,"minute":30,"tz":"Europe/Zagreb"}`.
  - `internal/schedule/schedule.go:65-68` validates `w.TZ` using `time.LoadLocation(w.TZ)`.
  - In Go, `time.LoadLocation("CEST")` returns `unknown time zone CEST`, causing the scheduler to reject the reminder.
  - Furthermore, future reminders across DST boundaries require the IANA zone rules, not an instantaneous abbreviation.
- **Required Fix**:
  - The rendered line must include the IANA identifier alongside the current abbreviation and offset, e.g.:
    `"\nCurrent date and time: 2026-09-08 04:55 Monday, Europe/Zagreb (CEST, UTC+02:00)"`.

### 2. Constructor Parameter Signature Contradiction (Medium Severity)

- **Plan Statements**:
  - Line 22-23: *"ChatPlanner gains constructor-injected `clockid.Clock` AND `*time.Location`;"*
  - Line 24-25: *"Production wires clockid.System + the configured IANA zone; tests wire clockid.NewFake + a fixed IANA name."*
  - Line 46-47: *"injected fixed `*time.Location` (test uses time.FixedZone...)"*
  - Line 52-53: *"Constructor fail-closed cases: empty zone name and an unloadable zone name both REJECT construction"*
- **Flaw**:
  - Passing `*time.Location` means resolution happens outside `New`; constructor cannot reject "empty zone name" or "unloadable zone name".
  - Passing `ianaZone string` means `New` calls `time.LoadLocation(ianaZone)` and fails closed on invalid/empty strings, but `time.FixedZone` cannot be passed directly into `New`.
- **Required Fix**:
  - Explicitly define the constructor signature, e.g.:
    `func New(p ChatProvider, auth *s7min.Authority, target contracts.TargetID, clock clockid.Clock, ianaZone string) (*ChatPlanner, error)`
    where `New` validates `clock == nil || ianaZone == ""`, calls `loc, err := time.LoadLocation(ianaZone)`, and rejects on error. Tests pass `"Europe/Zagreb"`, `"UTC"`, or other valid IANA zone strings.
  - Remove all conflicting references to injecting bare `*time.Location` or `time.FixedZone`.

### 3. Missing Config Definition for Timezone (Low Severity)

- **Plan Statement**: `PLAN-TIME.md:24` — *"Production wires clockid.System + the configured IANA zone;"*
- **Flaw**:
  - `internal/foundation/config/config.go` currently lacks a `Timezone` field.
  - The plan does not specify:
    1. Adding `Timezone string `json:"timezone"`` to `config.Config`.
    2. Setting default value `Timezone: "Europe/Zagreb"`.
    3. Validating `Timezone` in `Config.Validate()` via `time.LoadLocation`.
    4. Passing `cfg.Timezone` at the `cmd/nexus/main.go:580` composition root.
- **Required Fix**:
  - Add explicit bullet in the plan specifying the `config.Config` field, default value, validation, and wiring.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | LLM / Tools | **HIGH** | Rendered line lacks IANA zone identifier; model cannot pass valid `tz` to `reminder_set`, failing `schedule.WallTime.validate()`. | `PLAN-TIME.md:35-36`, `tools.go:21-24`, `schedule.go:65-68` |
| **F2** | Architecture | **MEDIUM** | Constructor parameter type is contradictory across lines 22-23 (`*time.Location`), 24-25 (`string`), 46-47 (`FixedZone`), and 52-53 (`LoadLocation`). | `PLAN-TIME.md:22-25, 46-47, 52-53` |
| **F3** | Config | **LOW** | "Configured IANA zone" references non-existent config field without schema, default, or validation specification. | `PLAN-TIME.md:24`, `config.go:28-47` |

---

## Top 3 Weakest Points

1. **Omission of IANA zone in output line (`PLAN-TIME.md:35-36`)**: Directly breaks integration with `reminder_set` and `schedule.go`.
2. **`*time.Location` vs `ianaZone string` seam conflict (`PLAN-TIME.md:22-25, 52-53`)**: Two incompatible constructor designs mixed in the same document.
3. **Unspecified `config.Config.Timezone` wiring (`PLAN-TIME.md:24`)**: Missing config schema changes.

---

## Required Plan Revisions

1. Update the rendered line format in `PLAN-TIME.md:35-36` to include the IANA zone identifier:
   `"\nCurrent date and time: 2026-09-08 04:55 Monday, Europe/Zagreb (CEST, UTC+02:00)"`.
2. Unify the constructor seam: specify that `ChatPlanner` receives `(clock clockid.Clock, ianaZone string)`, `New`/`NewStreaming` execute `time.LoadLocation(ianaZone)` fail-closed, and tests pass standard IANA names (e.g. `"Europe/Zagreb"`, `"UTC"`). Remove conflicting `*time.Location` / `time.FixedZone` wording.
3. Specify the new `Timezone string` field in `config.Config` with default `"Europe/Zagreb"`, validated in `Config.Validate()`, and wired in `cmd/nexus/main.go`.

---

VERDICT: FAIL

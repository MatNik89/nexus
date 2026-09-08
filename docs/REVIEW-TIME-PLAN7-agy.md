# Review of Design Plan: Current Time in Model Context — Round 7 (`docs/PLAN-TIME.md`)

**Scope**: Review of `docs/PLAN-TIME.md` (commit `00fd1b3` on branch `slice/p0-time`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `9425baaf20b5f13bd3ef92616aa22fb92073f790` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TIME.md` at commit `00fd1b3` (v7) achieves complete, rigorous closure across all architectural, operational, and testability dimensions:

1. **Single Authoritative Ownership of Civil Time**: The non-goal has been updated to establish `resolved.Config.Timezone` as the sole owner of civil time in NEXUS, eliminating the dual-ownership ambiguity.
2. **Defensive "Local" Rejection AT BOTH LAYERS**: The special value `"Local"` (which `time.LoadLocation` resolves to `time.Local` without error) is explicitly rejected at both the configuration boundary (`ValidateBounds`) and the planner constructor boundary (`New` / `NewStreaming`). `""` is similarly rejected, while `"UTC"` and valid IANA names like `"Europe/Zagreb"` are accepted.
3. **Default Lock Detector**: A dedicated config-suite detector proves that omitting the `timezone` key resolves to `"Europe/Zagreb"` with origin `OriginDefault`, turning RED if `defaults()` is altered.
4. **DST & Portability Verification**: Real January (CET, `+01:00`) and July (CEST, `+02:00`) instants are pinned against `"Europe/Zagreb"`, locking stored IANA name verbatim output and real Go tzdata transition rules.
5. **End-to-End Composition Root Verification**: The `cmd/nexus` production-spine integration test verifies that `buildDaemon` forwards a non-default timezone (`"America/New_York"`) to the provider request, making a hardcoded default fail RED.
6. **Prompt Cache Prefix Stability & Safe Placement**: Placement at the tail of the assembled user message ensures the `system + history` KV cache prefix remains 100% byte-stable across minutes and turns, situated outside all untrusted fences and tool grammar.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Single Ownership (`ARCHITECTURE-ESSENTIALS.md` E5, `AGENTS.md`)**:
  - `Config.Timezone` is the sole owner of civil-time timezone configuration; `time.Local` is prohibited slice-wide.
- **[COMPLIANT] Fail-Closed Governance (`ARCHITECTURE-ESSENTIALS.md` E11)**:
  - Both config validation and planner constructors fail closed on `""`, `"Local"`, and unloadable zone strings before any provider or daemon dispatch.
- **[COMPLIANT] Clock Seam Architecture (`internal/foundation/clockid/clockid.go:1-5`)**:
  - Leverages `clockid.Clock` interface with `clockid.System{}` in production and `clockid.NewFake` in tests.
- **[COMPLIANT] Trust Fencing & Monotonicity (`AGENTS.md` E12, `assembler.go:51-54`)**:
  - Appended strictly after `assembler.Base(rest)` outside untrusted fences.
- **[COMPLIANT] Repository Language Invariant (`AGENTS.md`)**:
  - English throughout code, comments, config keys, and documentation.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The implementation adheres strictly to the Ponytail minimal-machinery contract without introducing superfluous packages or state machines.

---

## Spec & Design Review

### Verification of Closed Round-6 Findings

- **Single Civil Time Owner (`PLAN-TIME.md:86-88`)**:
  - Confirmed: The phrase "the host zone IS the owner's zone" is removed. `Config.Timezone` is the sole authority.
- **"Local" Rejection at Both Layers (`PLAN-TIME.md:32-35, 69-71`)**:
  - Confirmed: `Config.ValidateBounds` and `ChatPlanner` constructor both explicitly reject `"Local"` and `""`, eliminating the silent `time.Local` bypass.
- **Default Lock Detector (`PLAN-TIME.md:72-75`)**:
  - Confirmed: Config suite verifies resolution with no key yields `"Europe/Zagreb"` with `OriginDefault`.

---

## Top 3 Weakest Points (Mandatory Review Discipline)

1. **Test Environment tzdata Availability (`PLAN-TIME.md:59-61`)**:
   - *Analysis*: In minimal container environments without `/usr/share/zoneinfo`, loading `"Europe/Zagreb"` requires tzdata. The plan explicitly provides for `import _ "time/tzdata"` in test binaries if the runner lacks system tzdata.
2. **Illustrative String Literal (`PLAN-TIME.md:44-45`)**:
   - *Analysis*: The illustrative string in the plan text (`"2026-09-08 05:10 Monday"`) combines a Tuesday calendar date with "Monday". Detectors use programmatic `time.Time.Format` where weekday calculation is strictly derived from the pinned instant.
3. **Dual Constructor Parity (`PLAN-TIME.md:19-24`)**:
   - *Analysis*: Both `New` and `NewStreaming` in `planner.go` must receive `(clock clockid.Clock, ianaZone string)` to ensure streaming and buffered planners maintain identical temporal configuration.

None of these points constitute design defects or blockers.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All findings across rounds 1–6 are fully resolved. | `PLAN-TIME.md:13-94` |

---

VERDICT: PASS

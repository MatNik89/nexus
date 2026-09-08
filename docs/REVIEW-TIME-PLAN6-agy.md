# Review of Design Plan: Current Time in Model Context — Round 6 (`docs/PLAN-TIME.md`)

**Scope**: Review of `docs/PLAN-TIME.md` (commit `8093d29` on branch `slice/p0-time`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `9425baaf20b5f13bd3ef92616aa22fb92073f790` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TIME.md` at commit `8093d29` (v6) represents a fully resolved, watertight, and production-ready design for temporal anchoring in NEXUS (slice `p0-time`).

All feedback across rounds 1 through 5 has been systematically incorporated and verified:
1. **Explicit "Local" Rejection**: `Config.ValidateBounds` explicitly rejects `"Local"` (closing the `time.LoadLocation("Local")` host-leak bypass) and `""`, while accepting valid IANA identifiers like `"Europe/Zagreb"` and `"UTC"`.
2. **Seam & Real DST Rules**: `ChatPlanner` receives `(clock clockid.Clock, ianaZone string)` and loads the location fail-closed; the unit detector tests real `"Europe/Zagreb"` January (CET, `+01:00`) and July (CEST, `+02:00`) instants, proving stored IANA name rendering and real DST evaluation across seasonal transitions.
3. **Composition-Root Wiring Detector**: The production-spine integration test gains an `"America/New_York"` case to verify that `buildDaemon` forwards the resolved config timezone into `PlannerFactory` (preventing false-green hardcoded defaults).
4. **Accurate Composition Root Wiring**: `buildDaemon` is specified as the sole composition root, reflecting the daemon-owned architecture where the CLI chat client connects over UDS.
5. **Prompt Cache Prefix Stability & Tool Safety**: The anchor is appended to the tail of the assembled user message, preserving 100% of the `system + history` KV cache prefix and keeping the line outside all untrusted data fences and tool protocol grammar.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Clock Seam Architecture (`internal/foundation/clockid/clockid.go:1-5`)**:
  - Leverages `clockid.Clock` interface with `clockid.System{}` in production and `clockid.NewFake` in tests.
- **[COMPLIANT] Configuration Governance (`internal/foundation/config/config.go:1-12`, `E11`)**:
  - `timezone` field is typed and bound via `keySchema`, `defaults()`, `applyValue()`, and `ValidateBounds()`.
- **[COMPLIANT] Fail-Closed Security & Trust Fencing (`AGENTS.md` E11/E12, `assembler.go:51-54`)**:
  - The temporal string is appended after `assembler.Base(rest)`, guaranteeing placement outside untrusted fences.
- **[COMPLIANT] Repository Language Invariant (`AGENTS.md`)**:
  - Documentation, configuration keys, prompt additions, and tests are strictly in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Smells**: The design is lean, direct, and adheres to the Ponytail minimal-machinery contract.

---

## Spec & Design Review

### Verification of Closed Round-5 Findings

- **Rejection of Special "Local" Value (`PLAN-TIME.md:31-35, 66-67`)**:
  - `time.LoadLocation("Local")` returns `time.Local, nil` in Go stdlib. Explicitly rejecting `"Local"` in `ValidateBounds` prevents silent host-timezone leakage into persisted intents.
- **Real IANA Name & DST Detector (`PLAN-TIME.md:49-59`)**:
  - Replacing the stale FixedZone phrasing with real `"Europe/Zagreb"` tests across winter (January) and summer (July) verifies both stored name retention and DST offset shifts (`CET +01:00` vs `CEST +02:00`).
- **Composition Root Detector (`PLAN-TIME.md:60-65`)**:
  - Binding the `"America/New_York"` configuration test through the `cmd/nexus` production spine ensures the wiring from `config.Resolve` $\rightarrow$ `buildDaemon` $\rightarrow$ `daemon.Deps.PlannerFactory` $\rightarrow$ `planner.NewStreaming` is strictly tested against regression.
- **Nil-Clock and Fail-Closed Validation (`PLAN-TIME.md:21-24, 59, 68-69`)**:
  - Nil clock, empty zone, and unloadable zone names are all verified to reject construction before any provider call.

---

## Top 3 Weakest Points (Mandatory Review Discipline)

1. **Test Environment tzdata Availability (`PLAN-TIME.md:56-58`)**:
   - *Analysis*: In ultra-minimal container runners without system `/usr/share/zoneinfo`, `time.LoadLocation("Europe/Zagreb")` requires tzdata. The plan explicitly anticipates this by noting `import _ "time/tzdata"` in test binaries if needed.
2. **Non-Goal Phrasing Context (`PLAN-TIME.md:78-79`)**:
   - *Analysis*: `PLAN-TIME.md:78-79` mentions "the host zone IS the owner's zone" in the context of single-owner P0 scope (no per-profile dynamic timezone overrides), while the system timezone is explicitly configured in `config.Config` with default `"Europe/Zagreb"`.
3. **Example Timestamp Literal (`PLAN-TIME.md:41`)**:
   - *Analysis*: The illustrative string `"2026-09-08 05:10 Monday"` in the plan text pairs a Tuesday date with "Monday". The detector contract uses real Go `time.Time` formatting where weekday calculation is strictly programmatic and exact.

None of these points constitute design defects or implementation blockers.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All findings across rounds 1–5 are completely resolved. | `PLAN-TIME.md:13-73` |

---

VERDICT: PASS

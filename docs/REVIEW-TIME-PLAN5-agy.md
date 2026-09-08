# Review of Design Plan: Current Time in Model Context — Round 5 (`docs/PLAN-TIME.md`)

**Scope**: Review of `docs/PLAN-TIME.md` (commit `36e323b` on branch `slice/p0-time`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `9425baaf20b5f13bd3ef92616aa22fb92073f790` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TIME.md` at commit `36e323b` (v5) presents a complete, coherent, and rigorously fail-closed design that successfully resolves all findings from rounds 1–4:

1. **Definitive Seam & Stored IANA Identifier**: `ChatPlanner` receives `(clock clockid.Clock, ianaZone string)`. The constructor calls `time.LoadLocation(ianaZone)` fail-closed, storing both the resolved `*time.Location` and the verbatim IANA zone name.
2. **Unambiguous Rendering with Full Tool Compatibility**: The anchored line renders:
   `"\nCurrent date and time: 2026-09-08 05:10 Monday, Europe/Zagreb (CEST, UTC+02:00)"`
   using layout `"2006-01-02 15:04 Monday"`, `t.Format("MST")`, and `t.Format("-07:00")`. By printing the canonical IANA zone name (`Europe/Zagreb`), the LLM can populate the required `"tz"` field in `reminder_set` (`internal/obligation/tools.go:21-24`), satisfying the `schedule.WallTime.validate()` requirement (`internal/schedule/schedule.go:65-68`) and preserving DST transition semantics for future reminders.
3. **Executable Config Contract**: A typed `timezone` field is added to `config.Config`, registered in `keySchema`, defaulted to `"Europe/Zagreb"` in `defaults()`, handled in `applyValue`, validated via `time.LoadLocation` in `ValidateBounds`, and wired into the daemon's planner factory in `cmd/nexus/main.go`.
4. **Single Fail-Closed Production Failure Policy**: Silent UTC fallback is eliminated; unloadable zone databases or empty names cause immediate startup refusal.
5. **Prompt Cache Prefix Stability**: Placement at the tail of the assembled user message leaves the `system + history` KV cache prefix 100% byte-stable.

All candidate defects have been investigated and verified; no blocking design flaws remain.

---

## Standards Review

### Documented Standards Compliance

- **[COMPLIANT] Clock Seam Architecture (`internal/foundation/clockid/clockid.go:1-5`)**:
  - Uses `clockid.Clock` interface with `clockid.System{}` in production and `clockid.NewFake` in tests.
- **[COMPLIANT] Configuration Governance (`internal/foundation/config/config.go:1-12`, `E11`)**:
  - New `timezone` field adheres to typed schema rules (`keySchema`, `defaults`, `applyValue`, `ValidateBounds`) and rejects invalid IANA strings before runtime startup.
- **[COMPLIANT] Fail-Closed Security & Trust Fencing (`AGENTS.md` E11/E12, `assembler.go:51-54`)**:
  - Temporal line is appended after `assembler.Base(rest)`, ensuring it lands strictly outside untrusted data fences.
- **[COMPLIANT] Repository Language Invariant (`AGENTS.md`)**:
  - All prompt additions, config fields, and documentation are in English.

### Baseline Code Smells (Judgement Calls)

- **Zero Hard Smells**: The design introduces no unnecessary abstractions, packages, or background machinery (Ponytail minimal-machinery contract satisfied).

---

## Spec & Design Review

### 1. Verification of Closed Findings

- **Prompt Cache Prefix Integrity (`PLAN-TIME.md:14-18`)**:
  - Verified: Moving the temporal anchor from `system` to the tail of the current user message prevents cache invalidation of the system prompt and conversation history across turns.
- **IANA Identity for Obligation Scheduler (`PLAN-TIME.md:34-40`, `tools.go:21-24`, `schedule.go:60-68`)**:
  - Verified: Storing and printing the verbatim IANA name (`Europe/Zagreb`) gives the model the exact timezone identifier needed to pass `schedule.WallTime.validate()`.
- **Fail-Closed Constructor & Startup Bounds (`PLAN-TIME.md:19-33`, `config.go:306-340`)**:
  - Verified: Empty or unloadable zone names reject construction and configuration loading cleanly without unhandled panics or silent degradation.

---

## Top 3 Weakest Points (Mandatory Review Discipline)

1. **Residual FixedZone Mention in Detector Description (`PLAN-TIME.md:46-47`)**:
   - `PLAN-TIME.md:46-47` mentions `(test uses time.FixedZone — zone-independent of the host/CI, agy #4)`.
   - *Analysis*: In v5, `ChatPlanner` receives an IANA string (e.g. `"Europe/Zagreb"` or `"UTC"`), and tests pass standard IANA strings into `New`. `time.FixedZone` is not passed to `New`. This is a harmless textual remnant from v3, as standard IANA zones like `"Europe/Zagreb"` or `"UTC"` resolve deterministically across all environments with `clockid.NewFake`.
2. **Mention of `runChat` in Planner Factory Wiring (`PLAN-TIME.md:32-33`)**:
   - `PLAN-TIME.md:32-33` mentions `runDaemon and runChat pass resolved.Config.Timezone into the planner factory`.
   - *Analysis*: In `cmd/nexus/main.go:673-688`, `runChat` is the terminal REPL client connecting over UDS (`repl.Run`), whereas `runDaemon` (`cmd/nexus/main.go:572-596`) instantiates `daemon.New` and `planner.NewStreaming`. The planner factory is hosted exclusively in `runDaemon`.
3. **Stale Section Heading Version Tag (`PLAN-TIME.md:13`)**:
   - The section header at line 13 reads `## Change (v2 — all round-1 findings folded)`.
   - *Analysis*: Minor cosmetic artifact; the body text at line 19 correctly declares `v5, definitive`.

None of these weak points impact runtime correctness, interface integrity, testability, or security.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **N/A** | — | **INFO** | All previously identified findings across rounds 1–4 are fully resolved. | `PLAN-TIME.md:14-43` |

---

VERDICT: PASS

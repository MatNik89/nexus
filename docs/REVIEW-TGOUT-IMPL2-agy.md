# Implementation Review Round 2: Telegram Output Quality Slice (`tgout`)

**Scope**: Implementation review of commits `be7a1fa` and `2447127` (`HEAD`) on branch `slice/p0-tgout` in `/home/matej/HARNESS/nexus`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Plan**: [`docs/PLAN-TGOUT.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md) (v17, commit `2447127`)  
**Base Commit**: `dcb6f01`  
**Target Commit Range**: `be7a1fa..2447127`  

---

## Executive Summary

Implementation round 2 (commits `be7a1fa` and `2447127`) completely and faithfully resolves all findings from round 1 (Codex 4 MED + 3 LOW, Kilo 3 LOW) across both the **Standards** and **Spec** review axes:

1. **REPL Edge Structural Error Mapping (Finding 1 / Codex MED #1)**:
   - `internal/app/repl/repl.go:28-32` extends the UDS `frame` struct to unmarshal `Code` and `Tool` fields.
   - `internal/app/repl/repl.go:88-95` structurally maps `TOOL_SCHEMA_DRIFT` to `"Nisam uspio ispravno pozvati alat (" + f.Tool + "). Preformuliraj zahtjev."` without error-string parsing.
   - Verified by dedicated unit test [`internal/app/repl/repl_test.go:86-116`](file:///home/matej/HARNESS/nexus/internal/app/repl/repl_test.go#L86-L116) (`TestDriftFrameMapsStructurally`).

2. **Table Heading Index-Only Skip (Finding 2 / Codex MED #2)**:
   - `internal/channel/telegram/render.go:178-208` tracks `headingIdx` (index of the cell promoted to heading) and skips only `ci == headingIdx`.
   - Table cells in data positions matching the heading's literal string value (e.g. `| ana | ana |`) are preserved as real data.
   - Verified by adversarial corpus case in [`internal/channel/telegram/render_test.go:85`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render_test.go#L85).

3. **`topLevelWalk` Strict `io.EOF` Validation (Finding 3 / Codex MED #3)**:
   - `internal/llm/planner/drift.go:63-65` checks `dec.Token()` returns `io.EOF` after consuming one whole object, treating trailing tokens or garbage as non-whole objects (prose).

4. **`classifyView` Strict Wrapping Fence (Finding 4 / Codex MED #4)**:
   - `internal/llm/planner/drift.go:138-143` enforces that the last non-empty line must strictly be ```` ``` ````, preventing trailing un-fenced prose from being trimmed into a synthetic drift classification view.

5. **`TOOL_SCHEMA_DRIFT` Exact Replay Reconstruction (Finding 5 / Codex LOW #1)**:
   - `internal/app/daemon/daemon.go:291-308` reconstructs `loop.DriftError` *only* when `rec.Kind == "FAILED" && rec.Code == "TOOL_SCHEMA_DRIFT"`.
   - Ordinary or unrecognized failure codes (e.g. `OTHER_FAILURE`) land in today's generic collision error path without fabricating a drift error.
   - Verified by [`internal/app/daemon/daemon_test.go:1182-1201`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L1182-L1201) (`TestOtherFailureCodeStaysGeneric`).

6. **Property Validator De-entitization & Raw Ampersand Detection (Finding 6 / Codex LOW #2)**:
   - `internal/channel/telegram/render_test.go:52-55` unescapes valid HTML entities before testing for raw `&` characters, preventing false-positive test failures on valid escaped input (`A&B` -> `A&amp;B`).

7. **Committed Edge & History Detectors (Finding 7 / Codex LOW #3)**:
   - Live Telegram Croatian drift message mapping: [`cmd/nexus/main_test.go:1808-1845`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1808-L1845) (`TestTelegramLiveDriftMessage`).
   - Drift excluded from conversation history: [`internal/app/daemon/daemon_test.go:1145-1180`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L1145-L1180) (`TestDriftTurnExcludedFromHistory`).

8. **Post-Parse UTF-16 Measurement (Finding 8 / Kilo F3)**:
   - `internal/channel/telegram/render.go:52-57` (`postParseUTF16Len`) strips HTML tags and unescapes entities prior to measuring the 4096-unit bound, accurately reflecting Telegram Bot API's post-entity-parsing text measurement.
   - Boundary test updated and verified in [`internal/channel/telegram/render_test.go:175-182`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render_test.go#L175-L182).

9. **Plan Alignment (Finding 9 / Kilo F2)**:
   - `docs/PLAN-TGOUT.md:154` updated to reflect the shipped tool-naming Croatian error string (`"Nisam uspio ispravno pozvati alat (<tool>). Preformuliraj zahtjev."`).

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **Delivery Honesty & Crash Recovery ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - Exact error code gating in `recoveredTurnOutcome` ensures only verified drift errors reconstruct typed outcomes on redelivery collision.
- **Fail-Closed & Input Boundaries ([`AGENTS.md` HARDQ C7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E11`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - `topLevelWalk` and `classifyView` fail closed against trailing tokens or trailing unclosed prose blocks.
- **Language Boundaries ([`AGENTS.md` Language](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Kernel and daemon layers remain strictly English; Croatian localization is isolated exclusively to REPL and Telegram presentation edges.
- **Package Hierarchy & Invariants**:
  - No package cycles introduced; UDS communication remains strictly typed.

### 2. Spec Axis (Requirements & Edge Cases)

- **Conformance**:
  - All 9 round-1 findings are completely addressed with matching committed detector tests.
- **Suite Verification & RED-Capability**:
  - Full test suite passes cleanly across all packages (0 FAIL).
  - Round-2 additions were verified RED via targeted ablation probes:
    - REPL drift mapping ablation: `TestDriftFrameMapsStructurally` fails **RED**.
    - Table heading index ablation: `TestRenderHTMLPropertyValidator` on `ana|ana` fails **RED**.
    - Other-failure code ablation: `TestOtherFailureCodeStaysGeneric` fails **RED**.
    - Post-parse UTF-16 ablation: `TestRenderBudgetBoundaries` fails **RED**.

---

## Top-3 Weakest Points & Code Smells

In accordance with mandatory review discipline, the top-3 weakest points / judgement-call code smells in the implementation are identified below:

1. **Duplicated Edge Presentation Template ([`internal/app/repl/repl.go:92`](file:///home/matej/HARNESS/nexus/internal/app/repl/repl.go#L92), [`cmd/nexus/main.go:1360`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1360))**:
   - *Observation*: The Croatian error string format `"Nisam uspio ispravno pozvati alat (" + tool + "). Preformuliraj zahtjev."` is duplicated in both the REPL presentation handler and the Telegram daemon handler.
   - *Impact*: Acceptable for package decoupling across distinct client/daemon boundaries, but creates minor maintenance divergence risk if localized phrasing is updated.

2. **Regex-Based Tag Stripper in Length Check ([`internal/channel/telegram/render.go:52-57`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render.go#L52-L57))**:
   - *Observation*: `postParseUTF16Len` strips tags via `var tagRe = regexp.MustCompile("</?(b|code|pre)>")` rather than deriving the post-parse length during block tokenization.
   - *Impact*: Negligible runtime cost for message sizes <= 4096 characters, but creates an implicit tag-name coupling with `renderSpans` and `renderBlocks`.

3. **Full-String Line Splitting in `classifyView` ([`internal/llm/planner/drift.go:138-142`](file:///home/matej/HARNESS/nexus/internal/llm/planner/drift.go#L138-L142))**:
   - *Observation*: `classifyView` splits the entire reply string by newline (`strings.Split(t, "\n")`) to inspect the last line for ```` ``` ````.
   - *Impact*: Minor heap allocation overhead on non-fenced prose messages; safe and correct for P0 message volumes.

---

VERDICT: PASS

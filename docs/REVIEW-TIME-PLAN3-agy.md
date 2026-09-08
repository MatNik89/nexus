# Review of Design Plan: Current Time in Model Context — Round 3 (`docs/PLAN-TIME.md`)

**Scope**: Review of `docs/PLAN-TIME.md` (commit `e72b389` on branch `slice/p0-time`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `9425baaf20b5f13bd3ef92616aa22fb92073f790` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TIME.md` at commit `e72b389` (v3) successfully resolves the major architectural issues from rounds 1 and 2:
- **Cache-stable placement**: Temporal anchor is appended to the tail of the current assembled user message, preserving 100% of the `system + history` KV cache prefix.
- **Clock and Location seam**: `clockid.Clock` and `*time.Location` are constructor-injected, allowing deterministic testing via `clockid.NewFake` and `time.FixedZone`.
- **Exact Go Layout**: The format layout `"2006-01-02 15:04 Monday"` with explicit `Zone() + Format("-07:00")` produces clean, unambiguous strings like `2026-09-08 04:55 Monday, CEST (UTC+02:00)`.
- **Cleanup of contradictions**: The stale duplicate Detectors block from v1 has been removed, leaving a single, coherent detector contract.

However, one **unresolved design flaw** remains from round 2 (previously cited in `REVIEW-TIME-PLAN2-codex.md:36-40` and `REVIEW-TIME-PLAN2-kilo.md:67-73`):
- **Non-Existent Identifier `clockid.Real`**: Line 24 prescribes `Production wires clockid.Real + time.Local;`. In `internal/foundation/clockid/clockid.go:26-31`, the canonical production type is `clockid.System`, not `clockid.Real`. `clockid.Real` does not exist in the codebase and cannot compile.

Per the review protocol (*"FAIL for ANY unresolved design flaw of ANY severity"*), this round is **rejected** until the identifier is corrected.

---

## Standards Review

### Documented Standards Compliance

- **[VIOLATION] Type Name Accuracy & Mechanical Executability (`clockid.go:26-31`, `PLAN-TIME.md:24`)**:
  - `internal/foundation/clockid/clockid.go:26-30` defines:
    ```go
    // System is the production clock.
    type System struct{}
    
    func (System) Now() time.Time                  { return time.Now().UTC() }
    func (System) Since(t time.Time) time.Duration { return time.Since(t) }
    ```
  - `PLAN-TIME.md:24` specifies: `Production wires clockid.Real + time.Local;`.
  - There is no `clockid.Real` type in `internal/foundation/clockid/`. The production clock is `clockid.System{}`. Specifying a non-existent symbol violates mechanical executability.

- **[COMPLIANT] Language Invariant (`AGENTS.md`)**:
  - The plan and all specified prompt strings are in English.

### Baseline Code Smells (Judgement Calls)

- **Mysterious Name / Stale Type Ref (`PLAN-TIME.md:24`)**: Using `clockid.Real` instead of the canonical `clockid.System`.
- **Stale Section Header (`PLAN-TIME.md:13`)**: Heading still reads `## Change (v2 — all round-1 findings folded)` despite incorporating round-2 folds.

---

## Spec & Design Review

### 1. Non-Existent Production Clock Identifier (Low Severity — Blocking)

- **Plan Statement**: `PLAN-TIME.md:24` — *"Production wires clockid.Real + time.Local;"*
- **Source Verification**:
  - Grepping `internal/foundation/clockid/` confirms `System` is the sole production implementation of `Clock`.
  - `cmd/nexus/main.go:513, 524, 530` wires `clockid.System{}`.
- **Impact**: Code written to the plan's literal instruction will fail compilation with `undefined: clockid.Real`.
- **Required Fix**: Change `clockid.Real` to `clockid.System{}` in `PLAN-TIME.md:24`.

### 2. Constructor Nil-Safety / Fail-Closed Specification (Weakness / Recommendation)

- **Plan Statement**: `PLAN-TIME.md:22-25` — *"ChatPlanner gains constructor-injected `clockid.Clock` AND `*time.Location`; the line renders `clock.Now().In(loc)`."*
- **Analysis**:
  - In Go, `time.Time.In(nil)` panics immediately (`panic: nil Location in Time.In`).
  - Nexus constructors are strictly fail-closed (`planner.go:129`, `NewAPIKey`, etc.).
- **Recommendation**:
  - Specify that `New` and `NewStreaming` validate `clock == nil || loc == nil` and return an error (or safely default `clock = clockid.System{}` and `loc = time.Local`).

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | Spec / Types | **LOW** | `clockid.Real` does not exist; the canonical production type is `clockid.System{}`. | `PLAN-TIME.md:24`, `clockid.go:26-30` |

---

## Top 3 Weakest Points

1. **`clockid.Real` naming error (`PLAN-TIME.md:24`)**: Prescribes a non-existent struct name instead of `clockid.System{}`.
2. **Implicit nil-handling on `*time.Location` (`PLAN-TIME.md:23-24`)**: Does not explicitly mandate fail-closed constructor checks for `nil` location/clock.
3. **Stale header label (`PLAN-TIME.md:13`)**: Header reads `v2` on a v3 document.

---

## Required Plan Revision

1. In `docs/PLAN-TIME.md:24`, replace `clockid.Real` with `clockid.System{}` (or `clockid.System`).

---

VERDICT: FAIL

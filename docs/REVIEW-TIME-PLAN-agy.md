# Review of Design Plan: Current Time in Model Context (`docs/PLAN-TIME.md`)

**Scope**: Review of `docs/PLAN-TIME.md` (commit `9677f16` on branch `slice/p0-time`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `9425baaf20b5f13bd3ef92616aa22fb92073f790` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TIME.md` proposes a minimal, stateless pattern to solve dogfood finding #5 (NEXUS unable to answer what time it is) by appending a formatted date/time line to the system prompt in `ChatPlanner.Plan`, referencing the local `hermes-agent` context compressor pattern (`agent/context_compressor.py:4519`).

While the "no-tool" decision is sound (avoiding unnecessary round-trips, tool execution overhead, and S7 grants for read-only time awareness), the proposed design contains **material technical, architectural, and testability flaws**:

1. **Inverted Prompt Cache Reasoning**: The plan claims minute-resolution formatting keeps the prefix stable because *"DeepSeek prompt caching tolerates a changing tail"*. However, `messages[0]` (the `system` prompt) is the *prefix* (head) of the entire token sequence. Mutating the system prompt every minute invalidates the KV cache prefix for all subsequent tokens—including all multi-turn conversation history—for any turn occurring $\ge 1$ minute after the prior turn.
2. **Invalid Go Time Layout Verb**: The plan specifies `"\nCurrent date and time: <2006-01-02 15:04 (Monday), TZ>"`. In Go's `time.Format` reference layout, `TZ` is not a valid format verb and will literally print the characters `"TZ"`. Go uses `MST` for time zone abbreviations and `-07:00` for offsets.
3. **Package-Level Global `nowFn` Seam**: Introducing a package-level mutable `var nowFn = time.Now` in `internal/llm/planner` introduces mutable package-level global state, causes test races under `t.Parallel()`, and violates the repository's established constructor-injected seam pattern (`s7min.NewAuthority`, `effectpath.NewApprovals`, `clockid.Clock`).
4. **Timezone Environment Non-Determinism in Detectors**: Asserting an "exact formatted line" against host `time.Local` without an injectable `*time.Location` or pinned timezone will cause detectors to pass on developer machines in Zagreb (`CEST`) and fail in CI environments running under `UTC`.
5. **Ambiguous Timezone Abbreviation vs Numeric Offset**: Emitting only an abbreviation (`CEST`) rather than a numeric UTC offset (`+02:00`) impairs the model's ability to compute RFC 3339 timestamps for downstream obligation tools (`Reminder` / `Task`).
6. **Prompt Grammar Corruption**: Appending to the system prompt after `toolProtocol` places the timestamp inside/after the `Available tools:` list, corrupting the tool list grammar.
7. **Underspecified and Untested Fallback**: The "zone lookup fails" fallback is undefined in Go's `time` model and has zero test detectors.

Because unresolved design flaws exist across high, medium, and low severity tiers, the plan is **rejected**.

---

## Standards Review

### Documented Standards Compliance

- **[VIOLATION] Clock Seam Architecture & Seam Injection Pattern (`clockid.go:1-5`, `s7min.go:76-88`, `effectpath.go:108-115`, `tasks-P0.md:78-83`)**:
  - `internal/foundation/clockid/clockid.go:1-5` establishes: *"Every downstream component takes these interfaces — no component invents its own clock or ID source, so any scenario can be replayed deterministically in tests."*
  - All existing packages in `nexus` inject time sources via constructor arguments or struct fields (e.g., `s7min.NewAuthority(now func() time.Time, ...)`, `effectpath.NewApprovals(now func() time.Time, ...)`), defaulting to `time.Now` if nil.
  - `PLAN-TIME.md:18-19` specifies: *"A package-level `nowFn = time.Now` seam makes it testable"*.
  - Package-level mutable variables create global state, violate encapsulation, and cause data races if tests run in parallel. The time source must be an instance field on `ChatPlanner` (e.g. `c.now func() time.Time`).

- **[VIOLATION] Repository Language Invariant (`AGENTS.md`)**:
  - The plan specifies English system prompt additions (`"\nCurrent date and time: ..."`), which complies with `AGENTS.md` and aligns with `systemPrompt` (`planner.go:78-83`).

### Baseline Code Smells (Judgement Calls)

- **Primitive Obsession & Layout Specifier Error (`PLAN-TIME.md:16`)**: Passing raw pseudocode `TZ` to Go's `time.Format` demonstrates improper reliance on unverified format strings.
- **Speculative Generality (`PLAN-TIME.md:18-20`)**: Claiming *"if the zone lookup fails, fall back to UTC with the zone name printed"* when `time.Now()` in Go returns a `time.Time` struct that never fails or returns an error.

---

## Spec & Design Review

### 1. Inverted Prompt Cache Architecture (High Severity)

- **Plan Claim**: `PLAN-TIME.md:38-40` — *"Prompt cache friction upstream (per-minute prompt changes) — DeepSeek prompt caching tolerates a changing tail; format to MINUTE resolution to keep the prefix stable within a minute."*
- **Flaw**:
  - In OpenAI/DeepSeek-compatible chat APIs, requests are structured as:
    `[ {"role": "system", "content": ...}, {"role": "user", "content": ...}, ... ]`.
  - KV prompt caching works strictly via **common prefix matching from token 0**.
  - `messages[0]` (`system`) is the **HEAD/PREFIX** of the entire token sequence, not the "tail".
  - If the timestamp inside `messages[0]` changes every minute, then for any user interaction that occurs $> 1$ minute after the previous turn, the token prefix diverges at token ~50.
  - Consequently, **all subsequent cached tokens**—including the static tool definitions (`toolProtocol` + `c.specs`) and all accumulated conversation history (`splitHistory`)—are **cache misses** across minute boundaries!
- **Required Fix**:
  - The plan must correctly describe the prompt cache mechanics:
    - If temporal anchoring is kept in `system`, acknowledge the architectural tradeoff that multi-turn history cache is invalidated whenever turn gap $\ge 1$ minute.
    - Alternatively, if prefix cache stability across long multi-turn conversations is required, evaluate placing the temporal anchor in the current turn's `user` message/context block (the true tail), which preserves 100% of the system prompt and conversation history in the KV cache across turns.

### 2. Invalid Go Time Layout Specifier (High Severity)

- **Plan Claim**: `PLAN-TIME.md:16` — `"\nCurrent date and time: <2006-01-02 15:04 (Monday), TZ>"`
- **Flaw**:
  - Go's `time.Format` uses a fixed reference time (`Mon Jan 2 15:04:05 MST 2006`).
  - `TZ` is **not** a valid layout specifier in Go. Calling `t.Format("2006-01-02 15:04 (Monday), TZ")` outputs the literal characters `"TZ"`, e.g. `2026-09-07 19:15 (Monday), TZ`.
  - The correct Go layout specifiers are:
    - `MST` for time zone abbreviation (e.g. `CEST`, `UTC`).
    - `-07:00` or `-0700` for numeric offset (e.g. `+02:00`).
- **Required Fix**:
  - Update the format layout to a valid Go layout string, e.g. `2006-01-02 15:04 (Monday), MST` or `2006-01-02 15:04 (Monday) -07:00 MST`.

### 3. Package-Level Mutable `nowFn` vs Codebase Seam Standards (Medium Severity)

- **Plan Claim**: `PLAN-TIME.md:18-19` — *"A package-level `nowFn = time.Now` seam makes it testable"*
- **Flaw**:
  - Package-level variables create global mutable state across all tests in `internal/llm/planner`.
  - In `planner.go:222`, `time.Now()` is already used to set `Deadline: time.Now().Add(2 * time.Minute)`. A package variable creates fragmented time sources.
  - Nexus standard pattern: pass `now func() time.Time` to constructors or store `now` as a private field on `ChatPlanner`, initialized in `New` / `NewStreaming` (`if now == nil { now = time.Now }`).
- **Required Fix**:
  - Add `now func() time.Time` to `ChatPlanner` (or constructor / test helper), keeping all state instance-bound.

### 4. Timezone Non-Determinism in Detectors & CI (Medium Severity)

- **Plan Claim**: `PLAN-TIME.md:17-20, 30-32` — *"using the HOST's local time zone (the Pi runs Europe/Zagreb)... Unit: with nowFn pinned, the system message contains the exact formatted line... ablation removes the injection -> RED."*
- **Flaw**:
  - If a test pins `nowFn` to return `time.Unix(1725732938, 0)` or `time.Date(2026, 9, 8, 14, 30, 0, 0, time.UTC)`, but `Plan()` formats using `now.Local()`, the rendered output depends on the test runner host:
    - Local host in Zagreb: `2026-09-08 16:30 (Tuesday), CEST`
    - CI runner in UTC: `2026-09-08 14:30 (Tuesday), UTC`
    - Host in US Eastern: `2026-09-08 10:30 (Tuesday), EDT`
  - A detector asserting the "exact formatted line" will fail in CI or across environments with different local timezones.
- **Required Fix**:
  - The time formatting helper must either accept an explicit `*time.Location` or the test must set/inject the timezone (e.g. via `time.Local` scoping or location parameter in the seam) so tests are deterministic everywhere.

### 5. Ambiguous Timezone Abbreviation vs Numeric Offset (Medium Severity)

- **Plan Claim**: `PLAN-TIME.md:16-17` — `"\nCurrent date and time: <2006-01-02 15:04 (Monday), TZ>"`
- **Flaw**:
  - 3-letter abbreviations like `CEST` or `CST` are ambiguous and lack numeric offset information.
  - When the model plans downstream tool calls (such as scheduling reminders in `internal/kernel/contracts` which require RFC 3339 timestamps `2006-01-02T15:04:05+02:00`), the model is forced to guess whether `CEST` is `+01:00` or `+02:00`.
  - Providing the ISO offset in the prompt eliminates timezone offset arithmetic errors by the LLM.
- **Required Fix**:
  - Include the numeric offset in the anchored line, e.g.:
    `"\nCurrent date and time: 2006-01-02 15:04 (Monday) -07:00 MST"` $\rightarrow$ `"Current date and time: 2026-09-07 19:15 (Monday) +02:00 CEST"`.

### 6. Ambiguous Placement vs `toolProtocol` Grammar (Low Severity)

- **Plan Claim**: `PLAN-TIME.md:14-16` — *"In `ChatPlanner.Plan` (internal/llm/planner), append to the system content, every call: \nCurrent date and time: ..."*
- **Flaw**:
  - In `planner.go:246-259`, when `len(c.specs) > 0`, `system` is constructed by appending `toolProtocol` which ends with `"Available tools:\n"` followed by the tool list.
  - Appending the time line at the very end of `system` places it directly under `Available tools:`, corrupting the tool list grammar.
- **Required Fix**:
  - Specify that the time line is appended to `systemPrompt` *before* `toolProtocol` and tool specs are attached.

### 7. Underspecified and Untested Fallback Path (Low Severity)

- **Plan Claim**: `PLAN-TIME.md:18-20, 30-35` — *"if the zone lookup fails, fall back to UTC with the zone name printed (never omit the line — unlike hermes we always have a clock)."*
- **Flaw**:
  - In Go, `time.Time` values always carry a location. Calling `now := nowFn()` does not fail.
  - The plan does not define what "zone lookup fails" means (e.g. `time.LoadLocation` error, or `name == "" || name == "Local"`).
  - The detector list in `PLAN-TIME.md:29-35` contains zero test cases covering this fallback branch.
- **Required Fix**:
  - Clarify the exact fallback condition (e.g. if `time.Local` is unresolvable or `now.Location() == nil`, format in `time.UTC`), and add an explicit unit test verifying the fallback output.

### 8. Unsanitized Timezone Ingestion (Low Severity)

- **Plan Claim**: `PLAN-TIME.md:16-20`
- **Flaw**:
  - If a system has a custom `TZ` environment variable containing newlines or control characters, injecting it directly into the system prompt could alter prompt structure.
- **Required Fix**:
  - Sanitize the timezone string to ensure it contains only standard alphanumeric and safe punctuation characters (`[A-Za-z0-9_/+ -]`).

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | LLM / Cache | **HIGH** | Inverted prompt cache premise: mutating `system` (`messages[0]`) busts KV cache prefix for all history and tools on every turn gap $\ge 1$ min. | `PLAN-TIME.md:38-40`, `planner.go:260-263` |
| **F2** | Spec / Go | **HIGH** | Invalid Go layout verb `TZ` in format string literal prints `"TZ"` instead of timezone. | `PLAN-TIME.md:16`, Go `time.Format` |
| **F3** | Standards | **MEDIUM** | Package-level mutable `nowFn` variable introduces global state and violates codebase constructor seam pattern. | `PLAN-TIME.md:18-19`, `clockid.go:1-5`, `s7min.go:76-88` |
| **F4** | Testing | **MEDIUM** | Timezone non-determinism across environments causes "exact formatted line" unit detector to fail in CI. | `PLAN-TIME.md:17-20, 30-32` |
| **F5** | LLM / Tooling| **MEDIUM** | Omitting numeric UTC offset in timezone format risks model calculation errors on RFC 3339 tool parameters. | `PLAN-TIME.md:16-17`, `contracts.go` |
| **F6** | Prompt Grammar | **LOW** | Appending after `toolProtocol` corrupts the `Available tools:` list structure. | `PLAN-TIME.md:14-16`, `planner.go:247-259` |
| **F7** | Testing / Spec | **LOW** | UTC fallback path is underspecified in Go's type model and has zero test detectors. | `PLAN-TIME.md:18-20, 29-35` |
| **F8** | Security | **LOW** | Timezone string concatenated into system prompt without character validation. | `PLAN-TIME.md:16-20`, `AGENTS.md` E12 |

---

## Required Plan Revisions

Before implementation begins, `docs/PLAN-TIME.md` must be revised to:

1. Correct the prompt cache explanation to accurately reflect prefix caching mechanics and trade-offs.
2. Replace invalid `TZ` layout specifier with valid Go time layout verbs (e.g. `2006-01-02 15:04 (Monday) -07:00 MST`).
3. Replace the package-level `nowFn` variable with an instance-bound clock/time seam on `ChatPlanner`.
4. Define deterministic timezone handling in test fixtures so detectors pass reliably in CI and non-Zagreb environments.
5. Include the numeric UTC offset in the formatted string to assist LLM timestamp generation.
6. Specify exact placement of the temporal anchor before `toolProtocol`.
7. Define concrete fallback semantics for unresolvable timezones and add a corresponding detector.

---

VERDICT: FAIL

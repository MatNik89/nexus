# Implementation Review: Telegram Output Quality Slice (`tgout`)

**Scope**: Implementation review of commits `554824a..dcb6f01` on branch `slice/p0-tgout` in `/home/matej/HARNESS/nexus`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Binding Plan**: [`docs/PLAN-TGOUT.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md) (v17, commit `6e27dfb`)  
**Target Commit Range**: `554824a` to `dcb6f01` (`HEAD`)  

---

## Executive Summary

The implementation of the `tgout` slice (commits `554824a..dcb6f01`) strictly conforms to [`docs/PLAN-TGOUT.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md) (v17) across all four functional components:

1. **Component A (Planner — `drift.go`, `planner.go`)**:
   - Implements the strict closed key contract `{action, tool_id, arguments}` via a case-insensitive token-level walk on raw replies before struct decoding, preventing case-alias and duplicate-key execution exploits.
   - Classification view cleanly strips one wrapping code fence, routing dialect drift (`action`/`tool_id`/`name`) across all positions with tier precedence (`action > tool_id > name`) and within-tier first-occurrence token ordering.
   - Preserves declared limits (unrelated actions, other-key mentions, double-fenced JSON, and mixed prose+JSON remain prose).
   - Returns typed `loop.DriftError{Typed: contracts.TypedError, Tool: contracts.ToolID}` with `TOOL_SCHEMA_DRIFT` and `RetryNever`.

2. **Component B (Kernel Loop — `loop.go`)**:
   - `loop.DriftError` carrier is hosted directly in package `loop`, maintaining an acyclic package hierarchy (`planner -> loop -> contracts`).
   - `failTurn` extracts `loop.DriftError` structurally via `errors.As` and durably journals both `error_code` and `tool` in `turn.failed` payloads.

3. **Component C (Daemon — `daemon.go`, `cmd/nexus/main.go`)**:
   - `recoveredTurnOutcome` unifies recovery into a single-pass ordered fold: later successes win, suspension candidates are suppressed by subsequent `turn.resumed` events, and terminal `turn.failed` events supersede stale challenges.
   - Non-drift `turn.failed` events return `Kind=FAILED` with empty `Code`, preserving generic collision behavior without synthesizing artificial user responses.
   - Edge mapping in `cmd/nexus/main.go:1358-1361` structurally inspects `de.Tool` and maps `TOOL_SCHEMA_DRIFT` to `"Nisam uspio ispravno pozvati alat (" + string(de.Tool) + "). Preformuliraj zahtjev."` without error string parsing.

4. **Component D (Channel & Outbox — `channel.go`, `telegram/render.go`, `telegram/telegram.go`)**:
   - Outbound `Attempts` is folded from canonical `channel.outbound_unknown` events, with `Projection.Version() = 2` rebuilding state on replay.
   - First-lease formatting (`o.Attempts == 0`) ensures rendered HTML is attempted once on the initial in-flight lease; subsequent re-flushes degrade to plain original bytes without modifying retry scheduling or granting unbudgeted attempts.
   - `renderHTML` construct complies with all formatting rules: GFM tables convert to bold headers and bullet groups with lossless `'—'` (empty) and `'(extra)'` (surplus) markers, pipe-in-cell blocks pass through verbatim, empty tags are never emitted, and strict budgets ($\le 90$ opening spans, $\le 4096$ UTF-16 units) are enforced.

---

## Detailed Evaluation by Axis

### 1. Standards Axis (Contracts & Architecture)

- **Delivery Honesty & Commit Boundaries ([`AGENTS.md` B2/B7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E4, E15`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - `chan_outbox.attempts` is folded deterministically from canonical `EvOutboundUnknown` events in `Projection.Apply` (`internal/channel/channel.go:214-220`).
  - First-lease HTML formatting triggers only when `attempts == 0` (`internal/channel/telegram/telegram.go:303-308`), cleanly degrading to verbatim plain text on retry.
- **Package Hierarchy & Dependency DAG ([`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - `loop.DriftError` lives in `internal/kernel/loop/loop.go:242-248`, ensuring strict unidirectional dependencies (`planner` imports `loop`, `loop` does not import `planner`).
- **S7 Attempt Isolation ([`AGENTS.md` S7](file:///home/matej/HARNESS/nexus/AGENTS.md), [`docs/ARCHITECTURE-ESSENTIALS.md:E5`](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md#L68-L75))**:
  - The slice adds zero attempts and changes no scheduling; pre-existing outbox tick re-flush grant debt remains quarantined in `docs/tasks-P0.md:464-472`.
- **Typing & Fail-Closed Validation ([`AGENTS.md` Language & HARDQ C7](file:///home/matej/HARNESS/nexus/AGENTS.md))**:
  - Closed key contract (`internal/llm/planner/drift.go:72-125`) rejects unvalidated fields, case aliases, and duplicate keys.
  - Kernel errors and safe messages are in English; Croatian localization is isolated to the edge (`cmd/nexus/main.go:1358-1361`).

### 2. Spec Axis (Plan Conformance & Ablation Probes)

- **Conformance to PLAN-TGOUT.md v17**:
  - All requirements from Sections A, B, and Non-goals in [`docs/PLAN-TGOUT.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md) are faithfully implemented.
- **Suite Status & Ablation Verification**:
  - Full test suite across all touched packages passes cleanly (0 FAIL).
  - All 9 required causal ablations were verified RED in isolated disk testing:
    1. **Contract gate ablation**: Bypassing `validClosedContract` in `toolCallFromReply` causes case-alias/duplicate calls to execute -> `TestDriftDialectsBecomeTypedError` fails **RED**.
    2. **Known-tool check ablation**: Removing `specs[id]` validation in `driftTool` classifies general JSON as drift -> `TestDeclaredProseLimits` fails **RED**.
    3. **Ordering ablation**: Swapping drift classification before `toolCallFromReply` misclassifies valid tool calls as drift -> `TestValidBareCallStillExecutes` fails **RED**.
    4. **Resumed suppression ablation**: Dropping `turn.resumed` suppression in `recoveredTurnOutcome` replays stale challenges -> `TestResumedWithoutTerminalSuppressesChallenge` fails **RED**.
    5. **Attempts-ignore ablation**: Forcing formatted HTML on re-attempts carries rendered bytes instead of plain -> `TestFirstLeaseFormattedThenPlainAfterParse400` fails **RED**.
    6. **Event-fold drop ablation**: Dropping `EvOutboundUnknown` handling in projection loses attempts count -> `TestOldVersionDatabaseRebuildsAttempts` fails **RED**.
    7. **Nesting ablation**: Allowing nested span tags in code blocks violates property validation -> `TestRenderHTMLPropertyValidator` fails **RED**.
    8. **Tool-persist ablation**: Omitting `tool` in `turn.failed` payload causes tool name loss on redelivery -> `TestDriftOutcomeRecoveredOnRedelivery` fails **RED**.
    9. **Fence-strip ablation**: Removing single-fence normalization fails fenced drift detection -> `TestDriftDialectsBecomeTypedError` fails **RED**.

---

## Top-3 Weakest Points & Code Smells

In accordance with mandatory review discipline, the top-3 weakest points / judgement-call code smells in the implementation are identified below:

1. **Duplicate Tokenizer State Machines ([`internal/llm/planner/drift.go:17-66, 72-125`](file:///home/matej/HARNESS/nexus/internal/llm/planner/drift.go#L17-L66))**:
   - *Observation*: `topLevelWalk` and `validClosedContract` both implement separate `json.Decoder` token-loop walkers with identical delimiter depth tracking (`depth`), `json.Delim` switching, and `expectKey` toggles.
   - *Impact*: Low maintenance risk; both paths are verified by property and dialect test cases, but could be unified in a future refactor.

2. **Repetitive `DriftError` Instantiation ([`internal/llm/planner/drift.go:174-184`](file:///home/matej/HARNESS/nexus/internal/llm/planner/drift.go#L174-L184), [`internal/app/daemon/daemon.go:296-301`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L296-L301))**:
   - *Observation*: Construction of `loop.DriftError` with identical `Category: contracts.ErrCatValidation`, `Retryability: contracts.RetryNever`, `Origin: "planner"`, and template `SafeMessage` is written manually in both `planner` and `daemon`.
   - *Impact*: Minor boilerplate duplication; a constructor helper like `loop.NewDriftError(tool)` in `internal/kernel/loop` would ensure single-source definition.

3. **Bespoke Integer Formatter in Renderer ([`internal/channel/telegram/render.go:220-232`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render.go#L220-L232))**:
   - *Observation*: `itoa(n int)` is a custom base-10 digit-slicing function implemented inside `render.go` rather than using `strconv.Itoa`.
   - *Impact*: Minor code bloat; `strconv` is already imported and used elsewhere in the package.

---

VERDICT: PASS

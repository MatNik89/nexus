# Implementation Review Round 3: `tgout` Slice (Commit `4c6f02e`)

**Review Target**: Commit `4c6f02e` on branch `slice/p0-tgout` in `/home/matej/HARNESS/nexus`  
**Reference Plan**: [`docs/PLAN-TGOUT.md`](file:///home/matej/HARNESS/nexus/docs/PLAN-TGOUT.md) (v17)  
**Review Method**: Two-axis (Standards + Spec) per `~/.gemini/skills/code-review.md` and repository constitution (`AGENTS.md`).

---

## Executive Summary

Commit `4c6f02e` successfully resolves all round-2 findings across the `tgout` slice:
1. **One Consistent Lossless Table Rule** ([`render.go:174-214`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render.go#L174-L214)): The previous Hermes-shape branch (`len(cells) == len(headers)+1`) is removed. Table rows are rendered via a single deterministic rule: the first non-empty cell is skipped by index (`headingIdx`) and promoted to bold title `<b>...</b>`, subsequent data cells are labeled by header position `headers[ci]`, and surplus columns receive `• (extra): ...`. Label alignment on one-cell surplus is explicitly verified by [`TestRenderSurplusRowLabelsAligned`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render_test.go#L219-L232).
2. **Duplicate-Value Cell Survival** ([`render_test.go:207-215`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render_test.go#L207-L215)): `TestRenderDuplicateValueCellSurvives` content-asserts `strings.Contains(out, "• Role: ana")` on row `| ana | ana |`, locking that index-based heading skipping never drops equal-value data cells.
3. **Oracle Validator** ([`render_test.go:27-77, 236-249`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render_test.go#L27-L77)): Property validator logic is factored into `renderedViolation(out string) error` and directly verified via `TestRenderedViolationOracle` across raw ampersands, empty tags, and nested formatting tags.
4. **Live Edge Drift Test** ([`main_test.go:1846-1889`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1846-L1889)): `TestTelegramEdgeMapsLiveDrift` drives the complete live pipeline using `driftBundle` with mock HTTP provider returning `{"action":"memory_remember","content":"test note"}` into `telegramHandler`, confirming exact Croatian edge rendering (`"Nisam uspio ispravno pozvati alat (memory_remember). Preformuliraj zahtjev."`).

All 7 touched packages pass test suites without regression (0 failures, clean build).

---

## Axis 1: Standards Compliance

1. **Delivery Honesty & S7 Boundaries (`AGENTS.md` B2, S7)**:
   - Table rendering and Telegram HTML formatting remain strictly on the edge rendering path.
   - Outbox storage and journal payloads preserve raw text; HTML transformation occurs strictly on first lease (`outbound_unknown == 0`) with parse-400 fallback cleanly falling back to raw plain text.
2. **Crash Recovery & Invariant Replay (`AGENTS.md` B7, HARDQ B7)**:
   - Recovered turn fold in `recoveredTurnOutcome` maintains state-aware precedence: later successes win, intermediate suspensions are suppressed by later `turn.resumed`, and terminal `turn.failed` reconstructs `loop.DriftError` only when `error_code == "TOOL_SCHEMA_DRIFT"`. Non-drift failures retain empty code.
3. **Package DAG & Type Cleanliness**:
   - `loop.DriftError` remains rooted in `internal/kernel/loop`, imported downward by `internal/llm/planner` with zero cyclic imports.
   - Structural error inspection uses `errors.As` without error-string or `SafeMessage` heuristics.
4. **Repository Language Integrity**:
   - Kernel, engine, interfaces, and test fixtures remain English-only.
   - Croatian translation is strictly isolated to user-facing edge handlers (`cmd/nexus/main.go`, `internal/app/repl/repl.go`).

---

## Axis 2: Spec Alignment & Requirement Traceability

| Requirement / Finding | Plan / Review Anchor | Implementation & Detector Proof | Status |
|---|---|---|---|
| **One Lossless Table Rule** | Round-2 MED; Plan §6 | `render.go:174-214`, `TestRenderSurplusRowLabelsAligned` (`render_test.go:219-232`) | **CLOSED** |
| **Duplicate Value Preservation** | Round-2 LOW; Plan §6 | `render.go:177-203`, `TestRenderDuplicateValueCellSurvives` (`render_test.go:207-215`) | **CLOSED** |
| **Oracle Validator Property Test** | Round-2 LOW; Plan §6 | `renderedViolation` (`render_test.go:27-77`), `TestRenderedViolationOracle` (`render_test.go:236-249`) | **CLOSED** |
| **Live Telegram Drift Edge** | Round-2 LOW; Plan §1 | `TestTelegramEdgeMapsLiveDrift` (`main_test.go:1846-1889`) | **CLOSED** |
| **Span Budget & Nesting Limits** | Plan §6 (<=90 spans, <=4096 UTF-16) | `render.go:119-158`, `TestRenderBudgetBoundaries` (`render_test.go:187-195`) | **CLOSED** |

---

## Candidate Defects & Top-3 Weakest Points

1. **Weak Oracle Violation Attribution ([`internal/channel/telegram/render_test.go:236-249`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render_test.go#L236-L249))**:
   - `TestRenderedViolationOracle` asserts `err != nil` for violating strings (e.g. `"A&B"`, `"<b>x</b><b></b>"`, `"<b>a<code>b</code></b>"`) and `err == nil` for valid strings, but does not assert specific error message contents (e.g., verifying that `"A&B"` fails specifically with `"raw ampersand outside entities"` rather than an arbitrary parse error).
2. **Custom Integer Formatter ([`internal/channel/telegram/render.go:237-248`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render.go#L237-L248))**:
   - `itoa(n int)` manually formats surplus column indices into a stack buffer `[20]byte`. While allocation-free, standard library `strconv.Itoa(n)` is standard Go idiom and negligible in table rendering overhead.
3. **Redundant Slice Variable Alias ([`internal/channel/telegram/render.go:190`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/render.go#L190))**:
   - The alias `data := cells` is retained after removing the hermes row-label branch. Direct iteration/indexing over `cells` would be cleaner and avoid unnecessary variable binding.

---

VERDICT: PASS

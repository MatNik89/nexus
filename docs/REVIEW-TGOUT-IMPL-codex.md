# TGOUT Implementation Review — Codex

## Scope and binding

- Review type: read-only implementation review against `docs/PLAN-TGOUT.md` v17.
- Branch/revision: `slice/p0-tgout` at `dcb6f018fb590e02b3734cb68faedff959959f70`.
- Reviewed range: `554824a^..dcb6f01` (inclusive of `554824a`). The prompt's detailed component A scope requires including `554824a`; literal Git two-dot syntax would incorrectly exclude it.
- Target tree: `92813ae865f17781d24fb955575398893178e3e5`.
- Clean export: `/home/matej/HARNESS/nexus-review-tgout-codex-dcb6f01` on disk under `/home/matej/HARNESS`, never `/tmp`.
- Owners read before review: `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/PLAN-TGOUT.md`, PRD, Annex A portions of `docs/HARNESS-SPEC.md`, `docs/DESIGN-FIXES-r2.md`, and the relevant `docs/tasks-P0.md` channel/backlog clauses.
- Cleanup: VERIFIED — the disposable export was deleted after review and the path was confirmed absent.

## Findings

### 1. [CONCRETE][SEV: MED] The UDS carries drift metadata, but the REPL discards it and never performs the required structural edge mapping

Sources: `docs/PLAN-TGOUT.md:87-97`, `internal/app/daemon/daemon.go:79-89`, `internal/app/repl/repl.go:24-28`, `internal/app/repl/repl.go:68-86`.

- Input: an error frame `{"type":"error","text":"opaque-safe-message","code":"TOOL_SCHEMA_DRIFT","tool":"memory_recall"}`.
- Expected: the REPL consumes `code` and `tool` and maps the typed drift at the edge without parsing `text`.
- Actual: the REPL frame has only `type`, `text`, and `yolo`; it prints `error: opaque-safe-message` and silently ignores both structural fields.
- Root cause: only the daemon-side frame was extended. The UDS consumer and its test were not changed.
- Probe: an external boundary test using the real `repl.Run` returned `"nexus> error: opaque-safe-message\nnexus> \n"` and failed because neither `memory_recall` nor `Preformuliraj zahtjev.` appeared.
- Fix: add `Code` and `Tool` to the REPL frame, switch on the exact code, render the intended localized message, and add a daemon-to-REPL structural frame test.

### 2. [CONCRETE][SEV: MED] The table renderer can drop a real cell and mislabel a single surplus cell

Sources: `docs/PLAN-TGOUT.md:41-48`, `internal/channel/telegram/render.go:164-199`, `internal/channel/telegram/render_test.go:112-125`.

- Input A: `| Name | Role |\n|---|---|\n| ana | ana |`.
- Expected A: heading `ana` plus `• Role: ana`; equal text in two columns does not make the second cell redundant.
- Actual A: output is only `<b>ana</b>`; line 194 skips every value equal to the heading rather than only the selected heading cell.
- Input B: `| Name | Role |\n|---|---|\n| ana | admin | extra |`.
- Expected B: heading `ana`, `• Role: admin`, and `• (extra): extra`.
- Actual B: `<b>ana</b>\n• Name: admin\n• Role: extra`; the `len(headers)+1` branch shifts all labels and consumes the surplus as a normal column.
- Root cause: the renderer tracks the heading by value rather than source index and special-cases exactly one surplus cell as though it were an extra row-label column.
- Probe: both independent corpus cases failed through the real `renderHTML` boundary.
- Fix: retain the selected heading index; skip exactly that index, map remaining in-range cells to their original headers, and label every index beyond the header width `(extra)`.

### 3. [CONCRETE][SEV: MED] `topLevelWalk` treats trailing JSON syntax errors as EOF, so a mixed reply is misclassified as a single object

Sources: `docs/PLAN-TGOUT.md:83-86`, `docs/PLAN-TGOUT.md:157-161`, `internal/llm/planner/drift.go:12-16`, `internal/llm/planner/drift.go:62-65`.

- Input: `{"action":"memory_recall"} trailing prose`.
- Expected: prose delivery because the classification view is not exactly one JSON object.
- Actual: `TOOL_SCHEMA_DRIFT`; the user-visible reply is suppressed.
- Root cause: after the closing object, `topLevelWalk` returns false only when the next `Token` succeeds. It treats both `io.EOF` and a non-EOF syntax error as successful whole-object termination.
- Probe: the real planner returned the typed drift error rather than `Action.Final`.
- Fix: set `whole=true` only when the post-object token returns exactly `io.EOF`; every other result is not one whole object.

### 4. [CONCRETE][SEV: MED] The classification view strips fences that are not wrapping fences

Sources: `docs/PLAN-TGOUT.md:81-86`, `internal/llm/planner/drift.go:127-145`.

- Input A: an unclosed fence: `````json\n{"action":"memory_recall"}``.
- Input B: a closed fence followed by prose: `````json\n{"action":"memory_recall"}\n```\nthis is prose``.
- Expected: both remain prose; only one complete wrapping fence may be stripped.
- Actual: both become `TOOL_SCHEMA_DRIFT`.
- Root cause: `classifyView` accepts a missing close and uses `LastIndex("```")` without requiring the close to be the final fence line, discarding any trailing text.
- Probe: both subcases failed through the real planner.
- Fix: recognize an opening fence line only when a matching closing fence line is the final non-whitespace content; otherwise return the original view as still fenced/prose.

### 5. [CONCRETE][SEV: LOW] Recovery reconstructs every nonempty failure code as `DriftError`, contrary to the exact-code rule

Sources: `docs/PLAN-TGOUT.md:126-136`, `internal/app/daemon/daemon.go:290-305`.

- Input: a durable `turn.failed` with `error_code="OTHER_FAILURE"` and a colliding redelivery.
- Expected: the existing generic failed-collision path; only `TOOL_SCHEMA_DRIFT` may reconstruct `loop.DriftError`.
- Actual: `RunChannelTurn` returns `loop.DriftError{Code:"OTHER_FAILURE"}`.
- Root cause: the recovery switch tests `rec.Code != ""` instead of exact equality with `TOOL_SCHEMA_DRIFT`.
- Probe: a journal-backed collision test observed `errors.As(err, *loop.DriftError)==true` for `OTHER_FAILURE`.
- Fix: reconstruct drift only for the exact supported code; treat other nonempty codes according to their own typed owner or as the generic failure path.

### 6. [CONCRETE][SEV: LOW] The renderer property validator can accept a raw ampersand

Sources: `docs/PLAN-TGOUT.md:221-224`, `internal/channel/telegram/render_test.go:14-16`, `internal/channel/telegram/render_test.go:48-55`.

- Input: validator outside-text `A&B`.
- Expected: rejection because every ampersand outside renderer tags must be escaped.
- Actual: the regex `&[^#a-zA-Z]` finds nothing when `&` is followed by a letter, so the claimed property passes.
- Root cause: the oracle checks only the next character and mistakes arbitrary entity-looking prefixes for escaped entities.
- Probe: an independent oracle test failed with `property validator accepts a raw ampersand when followed by a letter`.
- Fix: tokenize only the exact entity forms the renderer can emit (or decode/re-encode canonically) and reject every other `&`.

### 7. [CONCRETE][SEV: LOW] Several plan-mandated committed edge detectors are absent

Sources: `docs/PLAN-TGOUT.md:95-97`, `docs/PLAN-TGOUT.md:110-113`, `docs/PLAN-TGOUT.md:137-150`, `docs/PLAN-TGOUT.md:162-166`, `internal/app/daemon/daemon_test.go:903-1112`, `internal/llm/planner/drift_test.go:80-158`.

- Expected: committed tests assert the drift is excluded from conversation history; live and recovered edge responses use structural code/tool and the identical Croatian string; within-tier precedence reaches the journal, live edge, and recovered edge.
- Actual: repository-wide search of `*_test.go` finds `TOOL_SCHEMA_DRIFT`/`DriftError` only in planner-level classification and daemon-level recovered-error tests. There is no REPL mapping test, no `cmd/nexus` Telegram Croatian mapping test, no drift-specific history test, and no precedence propagation test beyond the planner return value.
- Root cause: tests stop before the edge/history consumers despite the plan explicitly requiring those seams.
- Consequence: finding 1 remained green, and the Telegram edge mapping and full precedence propagation are not revert-proof.
- Fix: add the literal plan cases at the actual UDS, Telegram handler, history fold, journal payload, and recovered-edge boundaries.

## Plan conformance matrix

| Plan point | Result | Evidence |
|---|---|---|
| Raw strict parse before classification | PASS | `planner.go:282-298`; ordering ablation made `TestValidBareCallStillExecutes` RED. |
| Closed lowercase key set, case-insensitive duplicate rejection | PASS | `drift.go:68-125`; gate ablation made the dialect detector RED. |
| Drift values from action/tool_id/name, all duplicate positions, known tools only, tier and first-occurrence precedence | PASS with finding 3 | `drift.go:147-170`; known-tool ablation RED; precedence cases pass. The whole-object terminator is incorrect. |
| Exactly one wrapping fence for classification only | FAIL | Finding 4. The committed fence-strip ablation is RED but does not cover malformed wrappers. |
| Fully specified typed drift carrier and prompt negative example | PASS | `drift.go:173-184`, `loop.go:236-247`, `planner.go:84-91`. |
| `failTurn` structurally journals code+tool | PASS | `loop.go:252-265`; persisted-tool ablation RED. |
| One ordered recovery fold; resume suppresses stale challenge; non-drift empty-code behavior | PASS with finding 5 | No-terminal resumed-suppression ablation RED; ordinary empty-code test passes. Exact-code reconstruction is wrong. |
| Structural Telegram and UDS edge mapping | FAIL | Telegram production branch exists at `cmd/nexus/main.go:1353-1361`; UDS consumer is missing (finding 1), and required edge tests are absent (finding 7). |
| Drift omitted from succeeded-only history | Behavior supported; detector FAIL | `conversationHistory` folds only `turn.succeeded`, but the required drift-specific committed assertion is absent (finding 7). |
| Attempts folded from canonical `outbound_unknown`; projection v2 rebuild | PASS | `channel.go:178-318`; event-fold ablation RED; restart and old-version rebuild tests pass. |
| First-lease-only formatted bytes; later leases original; no scheduling change | PASS | `telegram.go:286-309`; attempts-ignore ablation RED; parse-400, restart, rebuild, and pre-wire-degradation tests pass. The pre-existing no-S7-grant tick is correctly left as the filed backlog item. |
| Hermes-shape lossless tables | FAIL | Finding 2. |
| Fence/code/bold no-overlap and escaped code content | PASS | bold-inside-code ablation made the property test RED. |
| Empty spans, escaped-pipe/backtick-pipe verbatim handling | PASS | direct implementation and committed corpus cases. |
| 90 opening-span and 4096 UTF-16 budgets, bool decision | PASS | exact 90/91 and 4096/4097 tests pass. |
| Constructive property validator | FAIL | Finding 6. |
| Non-goals: no multipart, no Bot API 10.1 surface, no full Markdown engine, no native function calling | PASS | no out-of-scope machinery added. |

## Independent RED-capability results

Each mutation changed only the named protection in the clean export, retained the committed test, observed the expected RED, and was reversed. Restored production files were then verified by Git blob hash against `dcb6f01`.

| Ablation | Retained detector and observed RED |
|---|---|
| Bypass closed-key contract gate | `TestDriftDialectsBecomeTypedError`: duplicate case ceased producing `DriftError`. |
| Ignore known-tool membership | `TestDeclaredProseLimits`: unrelated action `deploy` became drift. |
| Classify before strict parse | `TestValidBareCallStillExecutes`: valid call became drift. |
| Disable one-fence view | `TestDriftDialectsBecomeTypedError`: fenced dialect became a delivered final. |
| Omit persisted tool | `TestDriftOutcomeRecoveredOnRedelivery`: recovered/live returned an empty tool. |
| Drop `turn.resumed` suppression | `TestResumedWithoutTerminalSuppressesChallenge`: stale approval challenge replayed. |
| Ignore `Outbound.Attempts` | `TestFirstLeaseFormattedThenPlainAfterParse400`: re-attempt carried HTML. |
| Drop `outbound_unknown` attempts increment | restart and old-version rebuild tests both re-carried HTML. |
| Parse bold inside inline code | `TestRenderHTMLPropertyValidator`: detected nested `<b>` inside `<code>`. |

Result: all 9/9 requested causal mutations produced RED for the intended reason.

## Baseline verification

- `CGO_ENABLED=0 go test -count=1 ./...` — PASS, all discovered packages, 0 FAIL. This is the architecture-bound result; the host default was CGO-enabled, so CGO was explicitly disabled.
- Focused package run over planner, loop, daemon, channel, Telegram, REPL, and `cmd/nexus` — PASS.
- `go vet ./...` — PASS, exit 0.
- `git diff --check 554824a^..dcb6f01 -- '*.go'` — PASS, exit 0.
- Full-range `git diff --check` reports trailing spaces only in added Markdown review metadata where two spaces are Markdown hard-break syntax; not classified as an implementation defect.
- Post-mutation blob check — MATCH for every mutated production file; all temporary review-probe files removed.

## Simplification review

- `[BLOAT] internal/llm/planner/drift_test.go:7,160` — delete the unused `strings` import and sentinel `var _ = strings.TrimSpace` (net -2 lines).
- `[BLOAT] internal/channel/telegram/render.go:180,220-232` — use standard-library `strconv.Itoa` and delete the private integer formatter (approximately net -11 lines).
- Net: approximately -13 lines possible. These do not replace the correctness fixes above.

## Counterargument, weakest link, and proof ceiling

Strongest counterargument: the complete CGO-disabled suite is green and every one of the nine claimed ablations is genuinely red-capable. That is meaningful evidence for the implemented happy paths. It is not sufficient for PASS because independent boundary cases reproduce six behavioral/oracle defects, and the plan's required edge tests are absent.

Weakest link: no live Telegram Bot API or real provider was exercised; adapter evidence uses the repository fake server. This does not affect the reproduced pure renderer/classifier/REPL/recovery defects.

Proof ceiling: this review establishes behavior of commit `dcb6f01` on this Linux/arm64 host and the tested clean archive. It does not establish live-provider dialect frequency, live Telegram HTML acceptance, other platforms, race-detector cleanliness, or performance at inputs beyond the bounded test corpus.

Topknot projection: smallest repair surface is the existing owners only (`drift.go`, `daemon.go`, `repl.go`, `render.go`, and their tests); no new dependency or abstraction is justified.

VERDICT: FAIL

# TGOUT Implementation Review Round 3 — Codex

## Scope and artifact binding

- Review type: read-only implementation re-review of commit `4c6f02e` and its round-2 closure diff.
- Branch/revision: `slice/p0-tgout` at `4c6f02e7e016657a690d5afffac61e0c6f532bfd`.
- Parent: `2447127b4adafb6052e7bf8c6705b8b4507a027c`; target tree: `ab0a1b185ce3c751b27bd529424226734a7d90d3`.
- Reviewer UID: `1000`.
- Clean export: created from `git archive` under `/home/matej/HARNESS`, never under `/tmp`; `git get-tar-commit-id` returned the full target commit.
- The original worktree was not used for runtime proof. Only this report was added there.

## Findings

No unresolved HIGH, MED, or LOW finding was found in the scoped fixes or their same-class regression surface.

## Round-2 closure and RED-capability

| Round-2 item | Closure at `4c6f02e` | Retained detector under isolated mutation |
|---|---|---|
| Surplus-row label shift | CLOSED. `renderTableBlock` now always selects the first non-empty cell as the heading, skips only its source index, labels remaining in-range cells by the same index, and labels all surplus positions `(extra)` (`internal/channel/telegram/render.go:175-212`). | Reintroducing only the removed `len(cells) == len(headers)+1` row-label branch made `TestRenderSurplusRowLabelsAligned` RED: `Role: admin` disappeared and `Name: admin` / `Role: extra` reappeared. |
| Equal-value cell false green | CLOSED. `TestRenderDuplicateValueCellSurvives` content-asserts `• Role: ana` (`internal/channel/telegram/render_test.go:205-215`). | Restoring value-based skipping while keeping the test made it RED with output containing only `<b>ana</b>`. |
| Validator false green | CLOSED. `validateRendered` delegates to the directly callable `renderedViolation` oracle; `TestRenderedViolationOracle` directly checks raw ampersand, valid entity, empty tag, and nesting behavior (`internal/channel/telegram/render_test.go:14-75,234-249`). | Three isolated negative mutations each made the test RED for the intended reason: accepted raw ampersand, accepted empty tag, and accepted nesting. |
| Missing live Telegram drift path | CLOSED. `TestTelegramEdgeMapsLiveDrift` constructs a fresh real daemon bundle, serves the observed drift dialect from the provider boundary, calls `telegramHandler`, and asserts the exact Croatian response naming `memory_remember` (`cmd/nexus/main_test.go:1843-1889`). | Disabling the planner's registered-tool drift match made only the live test RED with the raw JSON reply, while the planted recovery-edge test remained GREEN. This distinguishes live loop coverage from recovery-fixture coverage. |

## Regression sweep

- The focused renderer tests and both Telegram drift-edge tests passed from the clean export.
- An independent temporary detector checked leading-empty rows, all-empty rows, and multiple surplus cells against the uniform source-coordinate rule; all passed, and every output also passed `renderedViolation`.
- The production change is confined to the canonical table renderer. The other changes are tests/helpers; no dependency, public API, persistent state, retry owner, journal owner, or transport behavior was added.
- `gofmt` reported zero diff lines for all three changed Go files. `git diff --check 4c6f02e^ 4c6f02e -- '*.go'` passed.
- After every mutation and the exploratory detector, all tracked export blobs matched the target tree and no extra files remained.

## Verification

- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -v ./internal/channel/telegram -run 'Test(RenderDuplicateValueCellSurvives|RenderSurplusRowLabelsAligned|RenderedViolationOracle|RenderTableToBullets|RenderTableLossless|RenderHTMLPropertyValidator)$'` — PASS, 6/6 selected tests.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -v ./cmd/nexus -run 'TestTelegramEdgeMaps(LiveDrift|DriftCroatian)$'` — PASS, 2/2 selected tests.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...` — PASS, all discovered packages, zero failures.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go vet ./...` — PASS, exit 0.
- Clean-export integrity — PASS: 0 missing tracked blobs, 0 changed tracked blobs, 0 extra files.

## Simplification review

- `[BLOAT] internal/channel/telegram/render.go:190,197`: the local `data := cells` alias can be deleted and the loop can range over `cells` directly; net `-1` line. This is not a correctness finding.
- The pre-existing private `itoa` helper and the pre-existing sentinel use of `strings.TrimSpace` remain removable as identified in round 2, but they are outside the causal round-3 diff and do not weaken any behavior or detector.

## Weakest points and proof ceiling

1. The live-drift test exercises the real local provider adapter, planner, loop, journal, daemon, and Telegram handler, but its provider is an `httptest` server and it does not call the external Telegram Bot API.
2. `renderedViolation` is a test oracle, not a production runtime validator. Its controlled negative mutations establish detector sensitivity; production safety still relies on constructive rendering plus regression coverage.
3. The table sweep covers the reported defects and additional coordinate shapes, but it is not an exhaustive proof over all strings. Escaped-pipe and inline-code-pipe inputs remain the plan's intentional verbatim fallback.

Strongest counterargument: a green suite and targeted mutations cannot prove real Telegram parser acceptance or all possible table inputs. That limitation does not conceal a reproduced defect in this round: the reported failure classes turn RED under their causal reversions, the real local live path is distinguished from recovery, and the same-class adversarial cases stayed GREEN.

Proof ceiling: this review establishes the committed behavior on this Linux/arm64 host at UID 1000 using the exact clean archive, local HTTP boundaries, deterministic mutations, and the repository suite. It does not establish live Telegram/provider behavior, other operating systems, race-detector cleanliness, or exhaustive renderer correctness.

VERDICT: PASS

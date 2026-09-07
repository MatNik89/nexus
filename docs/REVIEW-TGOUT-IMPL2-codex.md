# TGOUT Implementation Review Round 2 — Codex

## Scope and artifact binding

- Review type: read-only implementation review of commits `be7a1fa` and `2447127` against `docs/PLAN-TGOUT.md`.
- Branch/revision: `slice/p0-tgout` at `2447127b4adafb6052e7bf8c6705b8b4507a027c`.
- Reviewed range: `be7a1fa^..2447127`; target tree: `ead9c82665f01582c555ed3853275b4afb10fca7`.
- Reviewer UID: `1000`.
- Clean export: `/home/matej/HARNESS/nexus-review-tgout-r2-codex-2447127`, on disk under `/home/matej/HARNESS`, never under `/tmp`.
- Export integrity: every tracked file hash matched the target tree after all controlled mutations; no missing, changed, or extra files remained.
- Cleanup: VERIFIED. The export and the review-created auxiliary tree listing were deleted, and both paths were confirmed absent.

## Findings

### 1. [CONCRETE][SEV: MED] The prior surplus-cell table defect remains

Sources: `internal/channel/telegram/render.go:175-210`, `internal/channel/telegram/render_test.go:114-127`, `docs/PLAN-TGOUT.md:41-48`.

- Input: `| Name | Role |\n|---|---|\n| ana | admin | extra |`.
- Expected: heading `ana`, `• Role: admin`, and `• (extra): extra`; the plan promises lossless surplus handling.
- Actual: `<b>ana</b>\n• Name: admin\n• Role: extra`.
- Root cause: the `len(cells) == len(headers)+1` branch unconditionally consumes the first cell as an out-of-band row label and shifts every remaining value one header left. A one-cell surplus in an ordinary GFM row is therefore mislabelled instead of receiving `(extra)`.
- Probe: an independent test through the real `renderHTML` boundary passed the `ana|ana` equal-value subcase but failed the single-surplus subcase at `2447127`.
- Fix: select the promoted heading by source index without changing the data/header coordinate system; skip only that index, preserve every other in-range label, and label every index beyond the header width `(extra)`.

### 2. [CONCRETE][SEV: LOW] The committed `ana|ana` case is false-green against the index fix

Sources: `internal/channel/telegram/render_test.go:67-95`, `internal/channel/telegram/render.go:177-210`.

- Input: restore the old value-based skip, `val == heading && ci < len(headers)`, while retaining all committed tests.
- Expected: the committed `ana|ana` detector turns RED because `Role: ana` is dropped.
- Actual: `CGO_ENABLED=0 go test -count=1 ./internal/channel/telegram` remained GREEN.
- Root cause: `ana|ana` appears only in the property corpus. The property loop calls `validateRendered` for syntactic validity but never asserts that input table cells survive with the correct labels. Data loss still produces balanced, escaped HTML.
- Fix: add a semantic assertion that the exact output contains `• Role: ana`; retain a separate single-surplus assertion for finding 1.

### 3. [CONCRETE][SEV: LOW] The committed `A&B` case does not exercise the repaired ampersand oracle

Sources: `internal/channel/telegram/render_test.go:13-65`, `internal/channel/telegram/render_test.go:67-95`, `internal/channel/telegram/render.go:32-39`.

- Input: restore the old `&[^#a-zA-Z]` validator check while retaining the committed `A&B raw ampersand` corpus entry.
- Expected: the committed detector turns RED because the old oracle accepts `A&B`.
- Actual: `CGO_ENABLED=0 go test -count=1 ./internal/channel/telegram` remained GREEN.
- Root cause: `renderHTML("A&B raw ampersand")` takes the unformatted path (`ok=false`), and the property loop invokes `validateRendered` only when `ok=true`. The new corpus entry never reaches the validator whose behavior it claims to lock.
- Fix: add a direct validator regression seam for raw `A&B`, or extract the outside-text/entity predicate into a small boolean helper tested directly.

### 4. [CONCRETE][SEV: LOW] The claimed live Telegram drift detector exercises only collision recovery

Sources: `cmd/nexus/main_test.go:1806-1841`, `cmd/nexus/main.go:1349-1364`, `docs/PLAN-TGOUT.md:140-167`.

- Input: a fresh Telegram turn whose planner returns a drift dialect and whose `turn.failed` is produced by the live loop.
- Expected: a committed detector drives that fresh path through `telegramHandler` and asserts the exact Croatian response and selected tool.
- Actual: `TestTelegramEdgeMapsDriftCroatian` preplants `turn.failed` plus the colliding `turn.created` event, then invokes the handler. The planner/live failure path is never run; the test covers recovered mapping only despite its `live path` label.
- Root cause: the fixture substitutes a durable recovered outcome for a live drifting planner.
- Fix: wire a drifting planner into the bundle and send a fresh update through `telegramHandler`; keep the existing collision case as the distinct recovered-edge detector.

## Round-1 closure and RED-capability

| Fold | Production closure | Retained detector under isolated ablation |
|---|---|---|
| REPL consumes structural code/tool and maps Croatian text | PASS | RED after disabling the exact code branch in `repl.Run` |
| Table heading skipped only by index; equal values are data | Behavior PASS for `ana|ana` | GREEN after restoring the old value comparison — finding 2 |
| `topLevelWalk` requires exact `io.EOF` | PASS | RED on trailing prose after restoring the old post-object check |
| `classifyView` strips only a strict wrapping fence | PASS | RED on the unclosed-fence case after restoring the old loose strip |
| Only `TOOL_SCHEMA_DRIFT` reconstructs `DriftError` | PASS | RED in `TestOtherFailureCodeStaysGeneric` after restoring `Code != ""` |
| Validator de-entities and rejects raw ampersands | Implementation present | GREEN after restoring the old oracle — finding 3 |
| Drift excluded from conversation history | PASS | RED in `TestDriftTurnExcludedFromHistory` after admitting incomplete pairs |
| Telegram Croatian edge mapping | Mapping PASS; live detector incomplete | Mapping test RED after disabling the edge branch, but its input is recovered — finding 4 |
| 4096 decision uses post-parse UTF-16 length | PASS | RED at the exact boundary after restoring raw-rendered length |
| Plan wording matches shipped tool-naming message | PASS | Static equality at `docs/PLAN-TGOUT.md:153-155` and `cmd/nexus/main.go:1360` |

The independent table probe also confirms that the index change preserves `• Role: ana`; it does not close the still-reproduced single-surplus defect in finding 1.

## Baseline verification

- `TMPDIR=/run/user/1000 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...` — PASS, all discovered packages, zero failures.
- `TMPDIR=/run/user/1000 CGO_ENABLED=0 /home/matej/.local/go/bin/go vet ./...` — PASS.
- `git diff --check be7a1fa^ 2447127 -- '*.go'` — PASS.
- Post-mutation immutable-tree hash comparison — PASS for every tracked file; target tree restored exactly before cleanup.

## Simplification review

- `internal/llm/planner/drift_test.go:7,163`: `delete` the unused `strings` import and sentinel `var _ = strings.TrimSpace`; net `-2` lines.
- `internal/channel/telegram/render.go:192,233-245`: `stdlib` — use `strconv.Itoa` and delete the private integer formatter; approximately net `-11` lines.
- Net: approximately `-13` lines possible. These are non-blocking simplifications and do not repair the findings above.

## Counterargument, weakest link, and proof ceiling

Strongest counterargument: the complete clean-export suite and vet are green, seven behavioral protections are causally RED-capable, and the requested production fixes other than the surplus-row subcase behave correctly. That evidence is meaningful but cannot satisfy the explicit zero-severity rule: one prior data-loss subcase remains reproducible, and two claimed committed renderer detectors remain GREEN when their protections are removed.

Weakest link: no live Telegram Bot API or live provider was invoked. The pure renderer defect and detector false-greens do not depend on either external service.

Proof ceiling: this review establishes commit `2447127` behavior on this Linux/arm64 host at UID 1000, in the verified clean archive and tested local fakes. It does not establish real Telegram parser acceptance, live-provider dialect frequency, other operating systems, race-detector cleanliness, or performance beyond the bounded corpus.

VERDICT: FAIL

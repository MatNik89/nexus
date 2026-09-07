# REVIEW-TGOUT-IMPL2-kilo — implementation review round 2 of the tgout slice (be7a1fa..2447127)

**Scope:** commits `be7a1fa`+`2447127` on `slice/p0-tgout`, HEAD `2447127`. Read-only
review in a clean detached worktree (`/home/matej/qa-tgout2`). Held to the round-1 findings
(`REVIEW-TGOUT-IMPL-{codex,agy,kilo}.md`) and `docs/PLAN-TGOUT.md` v17.

## Closure verification (round-1 findings)

- **codex #1 (REPL discards code/tool) — CLOSED.** `repl.go` frame gains `Code`/`Tool`;
  the `error` branch maps `TOOL_SCHEMA_DRIFT` to the Croatian message and never prints the
  raw text (`repl.go:25-30,89-95`); `TestDriftFrameMapsStructurally` asserts both.
- **codex #3 (topLevelWalk trailing-EOF) — CLOSED.** `whole=true` only when the post-object
  token is exactly `io.EOF` (`drift.go:62-64`); "trailing-prose" is a committed prose case.
  RED-probed: reverting to `err == nil` makes `TestDeclaredProseLimits` fail.
- **codex #4 (non-wrapping fence) — CLOSED.** `classifyView` strips only when the final
  non-empty line is exactly ` ``` ` (`drift.go:135-143`); "unclosed-fence" and
  "fence-then-prose" are committed cases.
- **codex #5 (non-drift code reconstructed) — CLOSED.** `rec.Code == "TOOL_SCHEMA_DRIFT"`
  exact equality (`daemon.go:292`); `TestOtherFailureCodeStaysGeneric` asserts `OTHER_FAILURE`
  stays generic. RED-probed: reverting to `!= ""` makes that test fail.
- **codex #6 (validator raw ampersand) — CLOSED.** The validator de-entities the exact forms
  then rejects any remaining `&` (`render_test.go:52-55`); "A&B" corpus case added.
- **codex #7 (missing edge/history detectors) — CLOSED.** `TestTelegramEdgeMapsDriftCroatian`
  (`cmd/nexus/main_test.go`), `TestDriftTurnExcludedFromHistory` + `TestOtherFailureCodeStaysGeneric`
  (`daemon_test.go`), `TestDriftFrameMapsStructurally` (`repl_test.go`).
- **kilo F3 (UTF-16 post-parse) — CLOSED.** `postParseUTF16Len` strips renderer tags and
  decodes entities before counting (`render.go:49-55`); boundary test recalculated.
- **kilo F2 (plan wording) — CLOSED.** `PLAN-TGOUT.md:153-155` now names the shipped
  tool-naming message.

Full `go test ./...` = 0 FAIL (exit 0); the changed packages pass.

## Finding

**F1 [LOW] — codex #2 is only half-closed: the single-surplus-cell mislabel persists.**
The round-2 fix added `headingIdx` and skips by index, which repairs the *value-equal* drop
(Input A: `| ana | ana |` now yields `<b>ana</b>\n• Role: ana`). But the `len(headers)+1`
row-label branch is untouched and still maps `data[i] = cells[i+1] → headers[i]` — an
off-by-one. Probed at `2447127`:

`| Name | Role |\n|---|---|\n| ana | admin | extra |` → `<b>ana</b>\n• Name: admin\n• Role: extra`

whereas the codex fix and the plan's "surplus → `(extra)`" deviation require
`<b>ana</b>\n• Role: admin\n• (extra): extra`. The surplus cell is relabelled as a normal
"Role" column and the real "Role" cell is relabelled "Name". No committed test exercises
the `len(headers)+1` (exactly-one-surplus) shape: `TestRenderTableLossless` uses a 4-cell
row (which takes the else branch), and the property corpus never asserts column labels.

## Confidence

Provable from code + execution: seven of the eight named round-1 fixes are closed and
RED-capable (two spot-checked by ablation), the suite is green, and the plan wording/UTF-16
fixes are in place. F1 is a reproduced, still-open sub-case of codex #2 (mislabel, not a
data drop), LOW but unresolved under the all-severity rule.

VERDICT: FAIL

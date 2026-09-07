# REVIEW-TGOUT-IMPL3-kilo — implementation review round 3 of the tgout slice (4c6f02e)

**Scope:** commit `4c6f02e` on `slice/p0-tgout` (HEAD verified). Read-only review in a
clean detached worktree (`/home/matej/qa-tgout3`). Held to the round-2 findings
(`REVIEW-TGOUT-IMPL2-{codex,agy,kilo}.md`).

## Closure verification (round-2 findings)

- **MED (hermes row-label off-by-one) — CLOSED.** The `len(headers)+1` branch is removed;
  one consistent lossless rule: heading = first non-empty cell skipped by `headingIdx`,
  every other cell labelled by its own position, surplus beyond the headers → `(extra)`
  (`render.go:174-205`). `TestRenderSurplusRowLabelsAligned` asserts `<b>ana</b>`,
  `• Role: admin`, `• (extra): extra`, and that `• Name: admin` never appears.
  RED-probed: re-adding the row-label branch makes that test fail with "labels shifted".
- **TestRenderDuplicateValueCellSurvives — present.** Content-asserts `• Role: ana` for
  `| ana | ana |`. RED-probed: switching the skip to value equality leaves `headingIdx`
  unused (compile error) and would drop the duplicate-value cell — the content assertion
  is the lock.
- **renderedViolation oracle — present.** The validator is extracted as
  `renderedViolation(out) error`; `TestRenderedViolationOracle` locks raw-ampersand,
  empty-tag, and nesting rejection directly (and accepts `A&amp;B and <b>x</b>`).
- **TestTelegramEdgeMapsLiveDrift — present.** `driftBundle` drives the REAL loop (the
  provider returns `{"action":"memory_remember","content":"x"}`) through `telegramHandler`,
  asserting `Nisam uspio ispravno pozvati alat (memory_remember). Preformuliraj zahtjev.` —
  PASS.

Full `go test ./...` = 0 FAIL (exit 0).

## Note (not a finding)

`renderedViolation` re-declares a local `tagRe` identical to the package-level `tagRe` in
`render.go`; `postParseUTF16Len` uses the package one and the outside-text check uses the
local one. They are byte-identical, so this is pure duplication/shadowing in the test file,
not a defect.

## Confidence

All four round-2 findings are closed; two ablations (row-label re-add, value-skip revert)
turned their detectors RED, the live-drift edge test passes against the real loop, and the
full suite is green with no regression.

VERDICT: PASS

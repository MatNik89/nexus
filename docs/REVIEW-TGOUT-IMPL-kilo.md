# REVIEW-TGOUT-IMPL-kilo — implementation review of the tgout slice (commits 554824a..dcb6f01)

**Scope:** implementation of `docs/PLAN-TGOUT.md` (v17) on `slice/p0-tgout`, HEAD
`dcb6f01`. Read-only review in a clean detached worktree (`/home/matej/qa-tgout`). Held to
the plan point by point; regressions hunted; RED-capability probed.

## Plan conformance — verified point by point

- **A. Planner (drift.go / planner.go):** closed key contract via `validClosedContract`
  (token walk, case-INSENSITIVE duplicate detection, lowercase-only + unknown-key reject,
  action+tool_id required) gate *before* the struct decode (`planner.go:195`); fence-strip
  classification view (`classifyView`, one fence, double-fenced=prose); drift classifier
  `driftTool` (action/tool_id/name values, ALL positions, tier `action>tool_id>name` outer
  loop + first-source-occurrence inner loop, `planner.go:153-171`); `loop.DriftError`
  carrier built in `driftError`; prompt negative example in `toolProtocol`
  (`planner.go:89-90`). Ordered mutually-exclusive flow in `Plan` (`planner.go:282-298`).
- **B. Loop:** `DriftError` type + `Error()` (`loop.go:242-247`); `failTurn` extracts via
  `errors.As` and journals `{turn_id,error_code,tool}` vs the ordinary `{turn_id}`
  (`loop.go:252-266`).
- **C. Daemon:** `recoveredTurnOutcome` closed sum (SUCCEEDED/SUSPENDED/FAILED) with the
  resumed-suppression and terminal-failed-supersedes precedence (`daemon.go:455-509`);
  `RunChannelTurn` reconstructs the typed error only for `FAILED`+non-empty Code, falls
  through to today's collision path for ordinary failures (`daemon.go:290-308`); UDS frame
  carries `code`/`tool` (`daemon.go:84-88,225-227`); Croatian localized at the telegram
  edge via `errors.As` (`main.go:1358-1360`).
- **D. Channel:** `Projection.Version()==2`, `chan_outbox.attempts` column, and
  `EvOutboundUnknown` folds `attempts=attempts+1` (`channel.go:185,210,283`); renderer
  (`render.go`) implements tokenize-escape-wrap (fence>code>bold first-match-wins, empty
  spans literal, tables→bullets before spans, pipe-in-cell verbatim, 90-span / 4096-UTF-16
  budgets, `(string,bool)`); `FlushOutbox` first-lease-only (`o.Attempts==0` → rendered,
  else original plain) (`telegram.go:302-308`).

## Regressions and RED-capability

- Full `go test ./...` = **0 FAIL** across all packages.
- RED-probed at reviewer uid (revert → RED → restore):
  - drop the closed-key gate → `case-alias` executes instead of `DriftError` (RED);
  - `attempts=attempts+0` → re-attempt re-sends HTML (RED);
  - rename `case "turn.resumed"` → `TestResumedWithoutTerminalSuppressesChallenge` replays
    the stale challenge (RED).
- Committed detector suites cover the remaining ablations (tool-persist, fence-strip,
  known-tool check, ordering, nesting corpus, restart/rebuild).

## Findings

**F1 [LOW] — table rendering drops a data cell whose value coincides with the heading,
violating the plan's "lossless" claim.** `renderTableBlock` skips `if val == heading && ci
< len(headers)` (`render.go:194-196`) — by VALUE, not by the heading's POSITION. A data
cell that happens to equal the heading (or the first-non-empty cell) is dropped. Probed:
`| Name | Manager |\n|---|---|\n| alice | alice |` renders `<b>alice</b>` (the
"Manager: alice" data is lost); `| Name | Role |\n|---|---|\n| x | admin |\n| | boss |`
drops "Role: boss". The plan declares "ours is lossless"; no test covers this. The fix is
positional skip (the row-label/first-non-empty cell's own index), not value equality.

**F2 [LOW] — the Croatian edge message deviates from the plan's wording.** The plan states
"The Croatian edge message says 'Preformuliraj zahtjev.' only" (`PLAN-TGOUT.md:153-154`),
but the handler emits `"Nisam uspio ispravno pozvati alat (<tool>). Preformuliraj
zahtjev."` (`main.go:1360`). The "no 'pokušaj ponovno'" intent is satisfied and the extra
prefix is informative (not harmful), but it is not the "only" message the plan specifies.

**F3 [LOW] — the 4096 bound is measured on the raw rendered HTML, not post-parse.**
`render.go:42` checks `len(utf16.Encode([]rune(out)))` on the rendered string *with* tags
and `&amp;`/`&lt;` entities, whereas the plan's bound is "applied post-parse"
(`PLAN-TGOUT.md:108-109`). This is conservative (never exceeds the post-parse limit), but
over-degrades entity/tag-heavy replies near the boundary — a conformance deviation from the
stated post-parse measurement.

## Confidence

Provable from code + execution: the classifier, carrier, recovery fold, projection, and
first-lease path match the plan; the suite is green; and three of the nine named ablations
turn RED on revert. F1 is a reproduced data loss; F2/F3 are conformance deviations. All
three are LOW but unresolved under the all-severity rule.

VERDICT: FAIL

# PREP4 verification round 2 — Kilo (soak harness re-verification)

Target: `slice/p0-prep4` at HEAD `10994f2` (fold of my round-1 findings). Method: code-read, run `TestSoakVerdictSensitivity` + a `NEXUS_SOAK_MINUTES=3` soak + the suite in the repo. Working tree never modified.

## Runs

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok`. `TestSoakVerdictSensitivity` passes (now covers pre-restart RSS peak, pre-restart FD spike, mid-run WAL spike, all-invalid WAL, sparse latencies — each RED). A real 3-min soak passes: 90 msgs, 9 reminders, cold 216ms/156ms, RSS 19MB, FDs 14, WAL 4.1MB, maxBacklog=2.

## r1 findings — addressed

| r1 finding | Status |
|---|---|
| [LOW] S1 1.5× median weak for short runs | **Addressed** — S1/S2 are now judged per-incarnation with a peak check (`peakRSS*5 > mF*8`, i.e. >1.6× baseline, `soak_linux_test.go:154`) that catches a pre-restart spike; and the header now states the window-scoped budget explicitly ("not a universal leak proof … a sub-budget slow leak needs a longer run", `:11-13`). The slow-linear-leak limit is acknowledged, not hidden. |
| [LOW] tool-less traffic not stated | **FIXED** — the header states the scope verbatim (`:27-31`): conversation replies + reminder creation; memory write/recall and durable ASK/HITL are excluded and delegated to the T27 acceptance + chaos harness. |
| [LOW] p95 max-dominated | **Addressed** — sparse latencies now FAIL (`:182-184`), and the small-N max degeneracy is acknowledged in the header (`:19-20`). |
| [LOW/obs] S6 weaker than chaos I4 | **Hardened** — S6 now fails on drain-timeout (`:335-337`) and asserts zero non-SENT independently after the drain (`:338-341`); the reminder DELIVERED state remains chaos I4's job, which is the right division. |

## codex fold (verified)

- **#1 S6 hard**: drain `drained=false` on deadline → fail; separate `notSent != 0` assertion after the drain.
- **#2/#3 per-incarnation + WAL max/invalid**: `byInc` grouping with `len(ss) < 6` skip (defensive); WAL bound = MAX valid over the run with `invalid*10 > len(work)` fail (never a healthy zero).
- **#4 open-loop producer**: fixed 1-msg/2s rate, async reply correlation, 60s-unanswered hard-fail (`:262-264`), backlog sampled and reported (`maxBacklog`). This removes the per-tick 30s wait that previously coupled load to latency.

## NEW-defect hunt

None material. Two notes (not defects): the backlog is *reported* but not itself a threshold — the 60s-per-message hard-fail caps it, so a sub-60s throughput degradation inside a short window is only visible in the report (consistent with the stated window-scoped honesty); and `fileSizeChecked` marks the WAL invalid on any `os.Stat` failure, which is correct (the `-wal` file is persistent in WAL mode while the daemon runs, so invalid samples should be rare, and the >10% guard is defensive).

## Verdict

All four round-1 findings are genuinely addressed (per-incarnation + peak budgets, explicit scope, sparse-latency failure, hard S6), the open-loop producer removes the latency-masking flaw, and no NEW defect was introduced.

VERDICT: PASS

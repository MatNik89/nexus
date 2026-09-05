# PREP4 review round 1 — Kilo (soak harness)

Target: `slice/p0-prep4` at HEAD `3f98301` (new `internal/acceptance/soak_linux_test.go`). Method: code-read, run `TestSoakVerdictSensitivity` + a `NEXUS_SOAK_MINUTES=3` soak + the suite in the repo. Working tree never modified.

## Runs

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 33 packages `ok` (the soak is env-gated and skipped by default). `TestSoakVerdictSensitivity` passes (healthy accepted; leak/FD/WAL/latency/vacuous each refused). A real `NEXUS_SOAK_MINUTES=3` run passes: 45 msgs, 4 reminders, cold starts 180ms/270ms, RSS 19MB, FDs 14, WAL 4.1MB, journal 0.5MB.

## What holds

- Sampling is correct: RSS/FD read from `/proc/<pid>/…` with the pid re-bound after the mid-soak restart (`soak_linux_test.go:243`); a transient `-1` read is median-smoothed and a dead process is caught (`mF <= 0 || mL <= 0` → "broken", `:123-125`). WAL path is the default `journal.db-wal` (`:180`).
- S5 cold start is dial-verified readiness on the grown journal, bounded at 60s (`:230-248`).
- S6 end-state reuses the chaos checkers (production replay + exact-set TERMINAL + all SENT).
- The verdict is RED-capable (`TestSoakVerdictSensitivity`) and refuses a short/vacuous series (`len(samples) < warmup+9`, `:102-104`).

## Findings (all LOW — threshold/scoping observations, not defects)

1. **[LOW] S1 (RSS 1.5× median) is weak for short runs.** The median-of-thirds is a rate-of-change check; a slow linear leak (say 10%/hour) stays well under 1.5× across a 2–5 minute smoke (≈1.003×) and only trips over the 8-hour thirds of a real 24–48h run. The commit's own 2-min evidence ("RSS 23MB" flat) cannot distinguish "no leak" from "slow leak". This is a known smoke-vs-soak limitation, but the 1.5× threshold is only meaningful at the long-run scale it is designed for.

2. **[LOW] The traffic is tool-less and the scoping is not stated.** Traffic is `soak-msg-N` → `"reply-for …"` (channel admission + reply only) plus a `reminder_set` via yolo chat every 10th tick. It never exercises `memory_remember`/recall, the HITL/ASK flow, or the sandbox/exec path — so a leak or latency regression specific to those stores/paths is invisible to the soak. The header describes the traffic but does not say "no memory/recall, no HITL, no exec". Reasonable for a leak detector (the signals are path-agnostic), but the omission should be stated, not implied.

3. **[LOW] S4's p95 with few samples is max-dominated.** `p95Int64` indexes `s[(len*95)/100]`; for the small thirds of a short run this is the single worst sample, so one GC pause or one slow provider response in the last third can spike it. The `>= 3`-samples guard and the 3× threshold blunt this, and the test is env-gated, so it is acceptable noise rather than a flake.

4. **[LOW/obs] S6 is less strict than the chaos I4.** The drain waits for `status NOT IN ('SENT') == 0` + exact-set TERMINAL, but does not assert the reminders converge to DELIVERED (the chaos harness does). A reminder stranded DELIVERY_PENDING would leave a non-SENT row (caught by `notSent == 0`), but a reminder whose obligation state diverges from its outbox row without leaving a non-SENT row would not be — acceptable for a soak, but the end-state is strictly weaker than the chaos checker it reuses.

## Verdict

The soak harness is correct and RED-capable, with sensible sampling, cold-start and end-state checks; the findings are threshold/scoping limits inherent to a short-run smoke, not broken behavior.

VERDICT: PASS

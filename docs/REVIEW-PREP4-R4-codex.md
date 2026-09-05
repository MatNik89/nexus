# PREP4 verification round 4 — Codex

Reviewed branch `slice/p0-prep4` at immutable HEAD `c118de14a48bd4e8ff9ad296bbfed43e9f0ffac6`. The real working tree was read-only during review and test execution. All behavioral probes ran in fresh `git archive` exports; the exported and HEAD copies of `internal/acceptance/soak_linux_test.go` initially shared SHA-256 `3d2ca5c540bc1abd65b9f1975596b8ac159f979734967b294dc6879942052c14`.

## Finding

### [CONCRETE][SEV: MED] The offered-rate oracle checks total volume only, so it accepts both a long producer stall followed by a burst and an over-fast producer

The implementation does move fake-Bot reply scans outside the producer mutex: `collect` snapshots `sentAt` while locked, unlocks, and then calls `bot.countSent` (`internal/acceptance/soak_linux_test.go:298-316`). The producer is deadline-based and immediately catches up every elapsed deadline (`internal/acceptance/soak_linux_test.go:269-297`). However, its oracle does not prove the declared fixed two-second cadence. It checks only `offered < expect-1` (`internal/acceptance/soak_linux_test.go:366-376`), with no upper bound and no bound on inter-arrival spacing.

Two clean-export probes establish false-green behavior:

1. I replayed the exact round-3 probe: the first `collect` call slept for 12 seconds after acquiring `mu`. The test unexpectedly passed with 30 messages in one minute. The producer caught up by bursting the missed arrivals; `maxBacklog=6` exposed the bunching, but the oracle accepted it. This directly contradicts the fold claim that the same injection turns the test RED.
2. I changed only the producer deadline from two seconds to one second while leaving the expected-rate calculation at two seconds. The test unexpectedly passed with 60 messages in one minute, although “within one tick” permits approximately 30, not twice that count.

This matters because traffic spacing determines the load and latency regime. A total-count check after burst recovery does not prove a fixed-rate open-loop workload, and an unbounded upper side permits an accidental load change without a targeted diagnostic.

FIX: record each scheduled and actual arrival time and fail when any arrival deviates beyond the declared one-tick tolerance; also enforce `abs(offered-expect) <= 1`. A controlled 12-second producer stall and both one-second/three-second cadence mutations should turn the detector RED for the fixed-rate reason.

## Fold-claim verification

- **Claim #1, snapshot outside lock — PASS.** Bot scans occur after `mu.Unlock` (`internal/acceptance/soak_linux_test.go:298-316`) and therefore no longer serialize normal arrivals.
- **Claim #1, deadline catch-up — PASS as implemented.** Missed deadlines are emitted immediately (`internal/acceptance/soak_linux_test.go:272-296`). This preserves total arrivals but does not preserve fixed-rate spacing.
- **Claim #1, asserted within one tick / 12-second probe RED — FAIL.** The assertion is lower-bound-only, and both the exact stall replay and the over-fast mutation passed.
- **Claim #2 — PASS.** `stopProducer` uses `sync.Once`, always waits for `prodDone`, is registered with `t.Cleanup` immediately, and is reused on the normal path (`internal/acceptance/soak_linux_test.go:261-270,366`). Repeated calls safely receive from the closed completion channel.

## Commands and results

- `git show HEAD` — PASS; reviewed fold commit `c118de1`.
- Clean export: `git archive c118de14a48bd4e8ff9ad296bbfed43e9f0ffac6 | tar -x -C <new-temp-dir>` — PASS; source hash matched HEAD.
- Exact 12-second-stall export: `NEXUS_SOAK_MINUTES=1 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance` — **unexpected PASS** in 60.31s; 30 messages, `maxBacklog=6`; machine scan found no `FAIL` token.
- Unmodified clean export: `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance` — PASS in 180.57s; 90 messages, 9 reminders, `maxBacklog=2`; machine scan found no `FAIL` token.
- One-second producer / unchanged two-second oracle export: `NEXUS_SOAK_MINUTES=1 CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance` — **unexpected PASS** in 60.41s; 60 messages; machine scan found no `FAIL` token.

Weakest link: the test proves cumulative offered volume, not the fixed-rate temporal workload whose resource and latency signals it interprets.

VERDICT: FAIL

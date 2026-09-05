# PREP4 verification round 3 — Codex

Reviewed branch `slice/p0-prep4` at immutable HEAD `56467ac6b5595cd10724c4744ffd2d1fc10acc1e` (`fix(prep4-r2): fold codex round 2 (incarnation guard + producer purity)`). The committed working tree was not modified while inspecting or running tests. Probes and the controlled ablation ran only in a fresh `git archive` export whose initial `internal/acceptance/soak_linux_test.go` SHA-256 matched HEAD (`0f72da5b78f6a657307ac01aa872edb73a5388cfcfde7fb72d6b5352e754a6ee`).

## Findings

### [CONCRETE][SEV: MED] The claimed fixed-rate producer can still be stalled by reply correlation, and the harness does not detect lost arrival ticks

The producer goroutine is dedicated, but it is not independent of correlation. It must acquire `mu` before every arrival (`internal/acceptance/soak_linux_test.go:259-276`), while `collect` holds that same mutex across every reply lookup (`internal/acceptance/soak_linux_test.go:280-294`). Each lookup calls `fakeBot.countSent`, which locks the fake API and linearly scans all recorded sends (`internal/acceptance/acceptance_linux_test.go:1183-1193`). As outstanding messages and recorded sends grow, correlation work directly delays the producer. `time.Ticker` does not queue every missed deadline, so this converts intended offered load into silently dropped arrivals.

Clean-export probe: I inserted one 12-second delay after `collect` acquired `mu`, modeling a slow correlation pass without altering the verdict. A one-minute soak should offer approximately 30 arrivals at the declared two-second rate, but it reported only 26 messages and still passed:

`soak: 1m0s, 26 msgs, 2 reminders, ... maxBacklog=1`

This is a false-green at the precise overload boundary the open-loop change is meant to protect. The test has no assertion tying accepted arrivals to elapsed fixed-rate deadlines.

Smallest repair: never hold the producer lock during reply polling. Snapshot correlation state under the lock and release it before calling `countSent`, or give the producer exclusive ownership through channels. Record scheduled arrival deadlines (or explicitly catch up missed ticks), and assert that the offered count is within one boundary tick of the count implied by elapsed time.

### [CONCRETE][SEV: LOW] Producer shutdown is only on the successful path

The goroutine starts at `internal/acceptance/soak_linux_test.go:259-279`, but `prodStop` is closed only at `internal/acceptance/soak_linux_test.go:336-337`. Failures before that point—including the unanswered-message failure at line 290, reminder failure at lines 305-306, and cold-start failure at lines 330-331—call `t.Fatalf` and leave the ticker goroutine running. It can continue mutating the fake API and shared state during test cleanup.

Register an idempotent stop-and-drain function with `t.Cleanup` immediately after starting the goroutine, and reuse it on the normal path.

## Fold-claim verification

- **Claim #1 — PASS.** `soakVerdict` now takes `expectedIncarnations` and requires at least six post-warmup samples for every planned incarnation (`internal/acceptance/soak_linux_test.go:114-131`). The committed sensitivity test includes the three-insane-sample second-incarnation branch. In the clean export, changing the guard from `< 6` to `< 0` made `TestSoakVerdictSensitivity` fail with `short insane incarnation not caught: <nil>`. The committed guard is therefore causal and RED-capable.
- **Claim #2, chronology — PASS.** Latency records carry the message sequence, and the final latency series is sorted by sequence before the first/last thirds are selected (`internal/acceptance/soak_linux_test.go:246-253,388-400`). Map iteration order no longer defines S4 chronology.
- **Claim #2, fixed-rate independence — FAIL.** The clean-export contention probe above proves that correlation can still suppress scheduled arrivals without making the test fail.

## Executed evidence

- `CGO_ENABLED=0 go test -count=1 -run '^TestSoakVerdictSensitivity$' -v ./internal/acceptance` — PASS; machine scan found no `FAIL` token.
- `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance` — PASS in 180.58s; 90 messages, 9 reminders, maximum backlog 2; machine scan found no `FAIL` token.
- Fresh archive: `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 -run '^TestSoakVerdictSensitivity$' -v ./internal/acceptance` — PASS; machine scan found no `FAIL` token.
- Fresh-archive incarnation-guard ablation (`< 6` to `< 0`) followed by the sensitivity command — expected FAIL with `short insane incarnation not caught: <nil>`.
- Fresh-archive 12-second correlation-lock probe with `NEXUS_SOAK_MINUTES=1` — unexpected PASS with only 26 messages, demonstrating the false-green.

Weakest link: the harness labels the source open-loop, but a shared correlation mutex still controls whether fixed-rate ticks become arrivals, and no offered-load oracle notices the loss.

VERDICT: FAIL

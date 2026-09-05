# PREP4 Review Round 1 — Codex

Scope: `slice/p0-prep4` at `3f98301b16e940fdd9f52bc0ff4a9ec26b5bb4b8`. I reviewed the single HEAD commit, ran the committed tests in the untouched real repository, and ran adversarial mutations only in clean `git archive` exports. I did not inspect other reviewers' reports.

## Findings

### [CONCRETE][SEV: HIGH] S6 does not enforce the advertised all-SENT end state

The drain loop waits for zero non-SENT rows, but reaching its 60-second deadline merely falls through (`internal/acceptance/soak_linux_test.go:261-270`). After stopping the drain daemon, the final S6 checks verify production journal replay and exact-set terminal inbox only; they never query outbox state again (`internal/acceptance/soak_linux_test.go:271-280`). Therefore `PENDING`, `UNKNOWN`, or other non-SENT rows can survive and the test still pass, contradicting the header's “every reply row SENT” signal (`internal/acceptance/soak_linux_test.go:21-22`).

PROBE: In a clean HEAD export, after a successful drain I changed one real durable outbox row to `PENDING`, independently confirmed exactly one pending row, and continued through the committed final checker. `NEXUS_SOAK_MINUTES=1 TestSoakSurvival` passed. Production replay does not bind this projection invariant.

FIX: After the drain, make timeout an explicit failure and independently assert `COUNT(*) WHERE status <> 'SENT'` is zero before granting S6.

### [CONCRETE][SEV: MED] S1/S2 ignore the resource peak around the planned restart

The test samples the old PID until the half-time restart and correctly switches `pid` afterward (`internal/acceptance/soak_linux_test.go:224-248`), but the verdict has no incarnation identity. It discards the entire middle third, compares only first-third versus last-third RSS medians, and compares only one early FD value with the final post-restart value (`internal/acceptance/soak_linux_test.go:105-135`). A leak or FD accumulation that becomes severe late in the first incarnation and is reset by the planned restart is invisible. Even without restart masking, the claim “a steady leak fails” is too strong: monotonic linear growth below the 1.5x endpoint ratio passes.

PROBE: A clean-export synthetic series with a large FD spike in the ignored pre-restart region and a reset final count passed. A second series whose RSS increased monotonically by 100 KB on every sample also passed.

FIX: Record process incarnation with each sample and evaluate peak/delta or a time-normalized slope within each incarnation; publish an explicit acceptable growth-rate/absolute-growth budget instead of describing the 1.5x ratio as detection of every steady leak.

### [CONCRETE][SEV: MED] S3 treats a missing WAL measurement as a healthy zero and checks only the last sample

`fileSize` maps every `os.Stat` error to zero (`internal/acceptance/soak_linux_test.go:73-79`), while S3 accepts zero and inspects only the final sample (`internal/acceptance/soak_linux_test.go:136-139`). A wrong path, permission failure, or transient observation error therefore earns a green WAL signal. Earlier oversized samples are also ignored, so “WAL bound” means only “the final successful-looking number is at most 8 MiB.” The 8 MiB numerical ceiling is plausible as a smoke threshold—the real run ended near 4.1 MiB—but the measurement validity and maximum-over-run are not bound.

PROBE: A clean-export synthetic series populated every WAL sample from a definitely nonexistent path; `fileSize` returned zero throughout and `soakVerdict` passed.

FIX: Return `(size, error)`, reject unexpected stat failures while explicitly distinguishing a legitimately absent WAL, and evaluate the maximum valid WAL size observed across the measurement window.

### [CONCRETE][SEV: MED] The “steady traffic” generator is closed-loop and masks overload degradation

Each tick pushes one update, waits synchronously up to 30 seconds for that exact reply, optionally performs a reminder command, samples, sleeps two seconds, and only then pushes the next update (`internal/acceptance/soak_linux_test.go:199-250`). Thus offered load falls automatically when replies slow: the harness cannot build a queue or observe latency/throughput degradation caused by sustained arrival rate. The 30-second path does not cap a recorded latency—missing the deadline fails at lines 216-218—but the serial wait creates coordinated omission before S4 computes p95.

The workload is also narrower than an overall P0 soak: it covers Telegram conversation replies and occasional yolo reminder creation, but no memory write/recall and no durable ASK/HITL suspension-resume. That division can be valid, but the header should state it rather than implying broad real-daemon survival coverage.

FIX: Drive arrivals from an independent fixed-rate producer, correlate reply timestamps asynchronously, report throughput/backlog as well as latency, and explicitly list the excluded P0 flows (or add a small representative mix).

## Checks that held

- The live sampler updates `/proc/<pid>` targets after restart; no stale-PID bug was found.
- `soakVerdict` rejects fewer than `warmup+9` samples, which leaves at least three samples in each compared third. In the live path every recorded latency is positive or the test fails, so the S4 comparison is not vacuous from empty latency sets.
- S5 performs one half-time restart, measures dial-verified readiness on the grown journal, and fails if the second start exceeds its bound. The shared production replay and exact-set inbox checks are substantive; only the missing outbox assertion breaks S6.
- `TestSoakVerdictSensitivity` is red-capable for its synthetic S1-S4 step mutations and short-series case. It does not prove every advertised signal: S5/S6 are outside `soakVerdict`, and the S6 clean-export mutation above remains green.

## Commands and results

- `git branch --show-current; git rev-parse HEAD; git status --short --branch; git show HEAD` — PASS; bound to `slice/p0-prep4` / `3f98301b16e940fdd9f52bc0ff4a9ec26b5bb4b8`.
- `CGO_ENABLED=0 go test -count=1 -run '^TestSoakVerdictSensitivity$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep4-codex-sensitivity.log` plus literal `FAIL` scan — PASS; the test executed and passed, exit 0, no `FAIL` token.
- `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep4-codex-soak3.log` plus literal `FAIL` scan — PASS; 45 messages, 4 reminders, cold starts 251/309 ms, RSS 18,496 KB, 14 FDs, WAL 4,120,032 B, journal 503,808 B, exit 0, no `FAIL` token.
- Clean-export S6 mutation with one confirmed durable `PENDING` row after drain; `NEXUS_SOAK_MINUTES=1 ... TestSoakSurvival` — UNEXPECTED PASS, proving the all-SENT false green.
- Clean-export `TestReviewSoakVerdictFalseGreens` covering monotonic RSS growth, a pre-restart FD spike, and a missing WAL path — UNEXPECTED PASS for all three subtests.

## Topknot and proof ceiling

The implementation reuses the PREP3 world, fake Bot API, replay, and inbox checker and adds no dependency; that reuse is appropriately lean. The weakest link is not code volume but the gap between the six named signals and the states actually bound by the final verdict. A three-minute green smoke confirms this host and workload sample only; it cannot validate 24–48-hour growth thresholds, open-loop capacity, memory/HITL paths, or the currently unchecked all-SENT state.

VERDICT: FAIL

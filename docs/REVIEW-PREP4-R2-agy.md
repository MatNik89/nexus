# PREP4 Verification Round 2 — agy

Target: branch `slice/p0-prep4` at HEAD `10994f2` (`fix(prep4-r1): fold soak review round 1 (codex 1H+3M, agy 2H+1M, kilo)`).
Method: Adversarial verification of all fold claims against sources and contracts, executed in clean `git archive HEAD` isolated export directories (`/tmp/nexus-prep4-r2-probe`) with synthetic series mutation probes, a 3-minute open-loop soak test (`NEXUS_SOAK_MINUTES=3`), and full repository test suite machine verification (`CGO_ENABLED=0 go test -p 1 -count=1 ./...`). The repository working tree was not modified during testing.

---

## 1. Verified Fold Claims

| Fold Claim | Implementation Details | Verification & Proof |
|---|---|---|
| **(codex#1) S6 Hard Assertions & Independent Non-SENT Check** | `internal/acceptance/soak_linux_test.go:323-347`: The calm drain loop explicitly sets `drained = true` and fails if the 90s deadline expires (`if !drained { t.Fatal(...) }`). An independent `pollQuery` for non-`SENT` outbox rows runs post-drain and fails if any row is not `SENT`. | **PASS (RED-Capable)**: In an isolated clean-export probe mutating an outbox row to `PENDING` post-drain, `TestSoakSurvival` failed immediately at `:346` with `S6 1 non-SENT outbox rows after the drain (err=<nil>)`. |
| **(codex#2 / agy#1) Per-Incarnation S1 (RSS) & S2 (FDs) Evaluation** | `internal/acceptance/soak_linux_test.go:117-163`: Samples are partitioned by `incarnation` (tracked in `soakSample`). For each incarnation, S1 asserts both last-third median $\le 1.5\times$ first-third median AND peak $\le 1.6\times$ baseline median. S2 asserts peak $\le$ baseline + 10. | **PASS (RED-Capable)**: In `TestSoakVerdictSensitivity`, synthetic series modeling in-incarnation growth, pre-restart RSS peak, and pre-restart FD spike all fail decisively. The header explicitly documents this as a window-scoped growth budget. |
| **(codex#3 / agy#2) S3 WAL Max Bound & Invalid Sample Detection** | `internal/acceptance/soak_linux_test.go:85-91, 164-180`: `fileSizeChecked` records `walValid = false` on stat failure. S3 computes `maxWal` across all valid samples in `work` (asserting $\le 8\text{ MiB}$), and fails if $>10\%$ samples are invalid (`"measurement broken"`). | **PASS (RED-Capable)**: `TestSoakVerdictSensitivity` verifies that a mid-run 20MiB WAL spike fails, and an all-invalid WAL series fails. |
| **(agy#3) S4 Sparse Latency Rejection** | `internal/acceptance/soak_linux_test.go:181-192`: Latencies are passed explicitly as `latFirst, latLast []int64`. If `len(latFirst) < 3 || len(latLast) < 3`, `soakVerdict` fails immediately with `"S4 too few latency samples (%d/%d) — collection broken"`. | **PASS (RED-Capable)**: `TestSoakVerdictSensitivity` verifies that sparse latency sets ($N < 3$) fail rather than silently passing. |
| **(codex#4) Open-Loop Producer & Explicit Workload Scoping** | `internal/acceptance/soak_linux_test.go:236-265, 298-311`: Messages are pushed on a fixed 2s cadence without blocking on replies. An asynchronous pass collects answers in order into `latencies`; any message unanswered for $>60\text{s}$ fails immediately for backlog explosion. Max backlog is sampled and logged. Header explicitly states exclusion of memory recall/write and HITL approvals. | **PASS**: 3-minute soak run processed 90 messages at steady 2s arrival rate with `maxBacklog=2` and cold starts $286\text{ms}$ / $185\text{ms}$. |

---

## 2. Top 3 Weakest Points

1. **Quantization Noise in Latency Measurement (`soak_linux_test.go:256-261`)**:
   Because Telegram adapter poll/flush runs on a 2-second ticker, end-to-end `replyMS` includes 0-2000ms ticker alignment jitter, masking sub-second internal engine latency variations.
2. **Small-N Degeneracy of `p95Int64` (`soak_linux_test.go:102-109`)**:
   For sample sizes $N < 20$, `(N * 95) / 100 == N - 1`, meaning p95 is effectively the maximum sample in the slice.
3. **Fixed Window-Scoped Leak Budget Ceiling (`soak_linux_test.go:9-13`)**:
   A very slow memory leak accumulating at $<50\%$ over the test duration requires multi-hour/multi-day runs to trip the 1.5x/1.6x thresholds.

---

## 3. Test Suite Execution

- `CGO_ENABLED=0 go test -v -count=1 -run TestSoakVerdictSensitivity ./internal/acceptance`: **PASS** (0.00s).
- `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 go test -v -count=1 -run TestSoakSurvival ./internal/acceptance`: **PASS** (181.80s; 90 msgs, 9 reminders, cold1=286ms, cold2=185ms, RSS 18512KB, FDs 19, WAL 4128272B, journal 876544B, maxBacklog=2).
- `CGO_ENABLED=0 go test -p 1 -count=1 ./...`: **PASS** (all packages green, 0 failures, 0 panics).

---

VERDICT: PASS

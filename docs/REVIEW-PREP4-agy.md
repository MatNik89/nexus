# PREP4 Review Round 1 — agy

Target: branch `slice/p0-prep4`, commit `3f98301` (`feat(prep4): soak harness over the real binary`).
Method: Adversarial review of the new soak harness (`internal/acceptance/soak_linux_test.go`), verified against execution traces, clean `git archive HEAD` isolated export directories (`/tmp/nexus-prep4-probe`) with synthetic series mutation probes, and full repository test execution including a 3-minute soak run (`NEXUS_SOAK_MINUTES=3`). The repository working tree was not modified during testing.

---

## 1. Findings

### [CONCRETE][SEV: HIGH] Cross-Process Restart Conflation in S1 (RSS) and S2 (FDs) Masks In-Incarnation Leaks

- **Location**: `internal/acceptance/soak_linux_test.go:101-135, 187-248`
- **Mechanism**:
  `TestSoakSurvival` starts Incarnation 1 at $t=0$, terminates it at half-time ($t=T/2$), and spawns Incarnation 2 with a new PID for the second half.
  `soakVerdict` partitions the whole sample sequence into thirds: `first := work[:third]` (which samples Incarnation 1) and `last := work[len(work)-third:]` (which samples Incarnation 2). It evaluates:
  ```go
  if mL*2 > mF*3 { return fmt.Errorf("S1 RSS grew %dKB -> %dKB (leak signal, >1.5x)", mF, mL) }
  if fdN > fd0+10 { return fmt.Errorf("S2 FDs grew %d -> %d (leak)", fd0, fdN) }
  ```
  Because the mid-soak restart terminates the first process, the Linux kernel reclaims all allocated heap memory and closes all open file descriptors. Incarnation 2 starts with fresh heap and baseline FDs (~18-20).
  If Incarnation 1 suffers a massive linear memory leak (e.g. 50MB -> 120MB, 2.4x) or leaks 50+ file descriptors (e.g. 20 -> 70 FDs), the process termination wipes the slate clean. Incarnation 2 starts back at 50MB and 20 FDs. Even if Incarnation 2 continues leaking at the same rate up to 78MB and 34 FDs:
  - `median(rssF) = 60MB`, `median(rssL) = 80MB` $\implies 80 \le 1.5 \times 60 = 90$ (S1 **PASSES**).
  - `fd0 = 20`, `fdN = 34` $\implies 34 \le 20 + 10$ is evaluated against `fdN \le fd0+10` (if `fdN \le 30`, it passes; any leak in Incarnation 1 is completely uncounted).
- **Evidence**:
  In an isolated clean-export probe (`/tmp/nexus-prep4-probe`), feeding a synthetic series modeling a 2.4x RSS leak and 3x FD leak in Incarnation 1 reset by restart produced a FALSE GREEN:
  ```
  soakVerdict FALSE GREEN: accepted 2.4x RSS leak and 3x FD leak masked by restart
  ```
- **Remediation**:
  Evaluate S1 and S2 per uninterrupted incarnation (checking slope/growth within Incarnation 1 and within Incarnation 2 independently, or checking pre-restart vs post-warmup baselines), rather than comparing across the process termination boundary.

---

### [CONCRETE][SEV: HIGH] S3 WAL Bound Checks Only the Single Final Sample, Missing Mid-Run Unbounded Spikes

- **Location**: `internal/acceptance/soak_linux_test.go:136-139`
- **Mechanism**:
  `soakVerdict` evaluates S3 as:
  ```go
  walN := last[len(last)-1].walB
  if walN > 8<<20 {
      return fmt.Errorf("S3 WAL unbounded: %d bytes at the end (checkpoint broken)", walN)
  }
  ```
  This only inspects the single final sample `samples[N-1]`.
  If SQLite automatic checkpointing is broken or stalled under steady load, causing the `-wal` file to grow to 50MB, 100MB, or more mid-run, but a checkpoint or truncation happens at the end (or on restart / calm drain), `walN` will be small and S3 reports a clean pass.
- **Evidence**:
  Clean-export probe in `/tmp/nexus-prep4-probe`: a synthetic series with a 50MB WAL spike across samples 5-28 and 1MB at sample 29 produced a FALSE GREEN:
  ```
  soakVerdict FALSE GREEN: accepted 50MB mid-run WAL spike because sample 29 was small
  ```
- **Remediation**:
  Assert `max(walB) <= 8<<20` across all steady-state samples (or across `work`), rather than checking only the last sample.

---

### [CONCRETE][SEV: MED] S4 Latency Degradation Check Silently Passes on Sparse/Missing Samples

- **Location**: `internal/acceptance/soak_linux_test.go:140-146`
- **Mechanism**:
  S4 is guarded by `if len(latF) >= 3 && len(latL) >= 3`.
  If fewer than 3 valid positive latency samples exist in either bucket (e.g. if samples had negative/unmeasured latencies), the check is bypassed and `soakVerdict` returns `nil` with no error or warning.
  Furthermore, for small sample sizes ($N < 20$), `p95Int64` calculates `(N * 95) / 100 = N - 1`, which is literally the single maximum sample ($\max$) rather than a statistical percentile.
- **Evidence**:
  Clean-export probe in `/tmp/nexus-prep4-probe`: a synthetic series with 1000x latency degradation but only 2 valid latency samples in `latF`/`latL` produced a FALSE GREEN:
  ```
  soakVerdict FALSE GREEN: silently skipped S4 and passed 1000x latency degradation because len(lat) < 3
  ```
- **Remediation**:
  Require a minimum valid latency sample count (failing if latency data is missing), and clarify that for small sample sizes p95 degenerates to the maximum sample.

---

### [CONCRETE][SEV: MED] Latency Sampling is Quantized by the 2s Adapter Ticker

- **Location**: `internal/acceptance/soak_linux_test.go:207-218`
- **Mechanism**:
  Telegram outbox polling and delivery in `tgAdapter.Run` operates on a 2-second ticker (`interval = 2 * time.Second`). The measured latency `lat = time.Since(sentAt)` is dominated by when `sentAt` occurred relative to the next 2-second ticker boundary (ranging from ~50ms to ~2050ms).
  Subtle microsecond/millisecond engine latency degradations (e.g. database query slowdowns from 1ms to 200ms) are drowned out by 2000ms ticker quantization noise.
- **Evidence**:
  During the 3-minute soak run, `replyMS` values fluctuate between ~150ms and ~2100ms depending purely on ticker alignment.
- **Remediation**:
  Document that end-to-end bot latency measures ticker delivery quantization; consider adding internal engine execution time sampling if sub-second planner/journal degradation is to be detected.

---

### [CONCRETE][SEV: LOW] Traffic Scoping Unstated in Documentation

- **Location**: `internal/acceptance/soak_linux_test.go:3-23, 162-178`
- **Mechanism**:
  The soak harness traffic consists exclusively of Telegram conversational echos (`reply-for soak-msg-NNN`) and periodic reminder tool calls (`reminder_set`). It does not exercise memory facts (`mem_facts`), context assembly, sandboxed exec tools, multi-profile routing, or HITL approval suspensions.
- **Remediation**:
  State the exact workload boundaries in the test header (aligning with the honest scoping in the chaos harness).

---

### [CONCRETE][SEV: LOW] `soakVerdict` Negative Warmup Slice Bounds

- **Location**: `internal/acceptance/soak_linux_test.go:101-106`
- **Mechanism**:
  If `warmup < 0`, `samples[warmup:]` panics at runtime due to negative slice indexing.
- **Remediation**:
  Add an explicit guard `if warmup < 0 { return fmt.Errorf("invalid negative warmup: %d", warmup) }`.

---

## 2. Top 3 Weakest Points

1. **Restart Reset Masking Leaks (S1/S2)**: Comparing first-third metrics of PID 1 against last-third metrics of PID 2 allows large per-request leaks in either incarnation to go undetected.
2. **S3 Final-Sample Only**: Ignoring WAL sizes throughout the run allows transient but dangerous WAL growth spikes to pass if a checkpoint happens near the end.
3. **S4 Silent Skip on Missing Samples**: Incomplete latency sample sets bypass the degradation check entirely without failing the test.

---

## 3. Test Suite Execution

- `CGO_ENABLED=0 go test -v -count=1 -run TestSoakVerdictSensitivity ./internal/acceptance`: **PASS** (0.00s).
- `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 go test -v -count=1 -run TestSoakSurvival ./internal/acceptance`: **PASS** (181.82s; 45 msgs, 4 reminders, cold1=525ms, cold2=763ms, RSS 18528KB, FDs 19, WAL 4120032B, journal 503808B).
- `CGO_ENABLED=0 go test -p 1 -count=1 ./...`: **PASS** (all packages green, 0 failures, 0 panics).

---

VERDICT: FAIL

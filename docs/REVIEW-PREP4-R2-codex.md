# PREP4 Verification Round 2 — Codex

Scope: `slice/p0-prep4` at `10994f2dd564bb0d29a44388ee3b87d809b1c22a`. I reviewed the committed round-1 fold, ran the requested tests in the untouched real repository, and ran mutations/probes only in a byte-matched `git archive` export. I did not inspect other reviewers' reports.

## Findings

### [CONCRETE][SEV: MED] “Per incarnation” S1/S2 silently exempts any incarnation with fewer than six samples

Samples are grouped by incarnation, but `soakVerdict` simply continues when an incarnation has fewer than six post-warmup samples (`internal/acceptance/soak_linux_test.go:117-127`). No later check requires both planned incarnations to have been judged. This contradicts the per-incarnation contract: a slow-but-accepted restart late in a short soak can leave fewer than six samples for incarnation 2, whose RSS and FD values then have no effect at all.

PROBE: In a clean HEAD export, I supplied 24 healthy incarnation-1 samples followed by three incarnation-2 samples with RSS 200,000 KB and 100 FDs. `soakVerdict` returned success. The fixture satisfied the global sample minimum and valid WAL/latency requirements; only the short-incarnation bypass made it green.

FIX: Require every observed/planned incarnation to meet an explicit minimum sample count, failing the verdict when it cannot be judged. Apply warmup per incarnation or state precisely which startup samples are excluded.

### [CONCRETE][SEV: MED] The open-loop/S4 path still loses fixed-rate and chronological guarantees under backlog

The Telegram reply wait is gone, but the producer is not independently scheduled: each next arrival occurs only after scanning the entire outstanding map, possibly running synchronous reminder chat, sampling resources, and then sleeping two seconds (`internal/acceptance/soak_linux_test.go:248-296`). Correlation cost grows with backlog, and every tenth synchronous command can block the same loop. In the overload condition this signal is meant to measure, offered rate can therefore fall instead of remaining fixed.

Latency samples are appended while ranging over the `sentAt` map (`internal/acceptance/soak_linux_test.go:255-265,299-307`). Go map iteration is unordered, yet the final S4 split treats append order as temporal “first” and “last” thirds (`internal/acceptance/soak_linux_test.go:348-353`). A batch of accumulated replies can be permuted across that boundary and dilute or move the slow samples.

PROBE: A clean-export probe repeated the collector's map-range append pattern over 20 sequence-ordered messages and observed nonchronological output. The committed three-minute run had maximum backlog 2, so it did not exercise the larger batch that exposes the defect.

FIX: Drive arrivals from a deadline-based ticker/goroutine independent of correlation and reminder work; store latency records with message sequence or send timestamp, then sort by that key before selecting temporal thirds.

## Fold verification

- **S6 hard end state: PASS.** Drain timeout now fails, and zero non-SENT rows is queried independently after the daemon stops (`internal/acceptance/soak_linux_test.go:321-346`). In a clean export, changing one real durable outbox row to `PENDING` after a successful drain made the test fail with `S6 1 non-SENT outbox rows after the drain`.
- **S1/S2 peak and restart laundering: PARTIAL.** Incarnation IDs, within-incarnation median growth, RSS peak, and FD peak checks are present (`internal/acceptance/soak_linux_test.go:118-163`). The committed sensitivity suite rejects the prior pre-restart RSS/FD spike cases. The short-incarnation exemption above remains new.
- **S3 WAL validity and maximum: PASS.** `fileSizeChecked` distinguishes stat failure, invalid samples above 10% fail, and the maximum valid WAL value is bounded (`internal/acceptance/soak_linux_test.go:85-91,164-180`). The clean-export sensitivity suite rejected mid-run spike and all-invalid series; a nonexistent path returned `walValid=false`.
- **S4 sparse data: PASS.** Fewer than three values in either comparison set fails, zero first p95 fails, and degradation remains bounded (`internal/acceptance/soak_linux_test.go:181-191`). Temporal ordering remains defective as described above.
- **Scope statement: PASS.** The header explicitly excludes memory write/recall and durable ASK/HITL and describes the actual Telegram/reminder workload (`internal/acceptance/soak_linux_test.go:27-31`).

## Commands and results

- `git branch --show-current; git rev-parse HEAD; git status --short --branch; git show HEAD` — PASS; bound to `slice/p0-prep4` / `10994f2dd564bb0d29a44388ee3b87d809b1c22a`.
- `CGO_ENABLED=0 go test -count=1 -run '^TestSoakVerdictSensitivity$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep4-r2-codex-sensitivity.log` plus literal `FAIL` scan — PASS; exit 0 and no `FAIL` token.
- `NEXUS_SOAK_MINUTES=3 CGO_ENABLED=0 go test -count=1 -run '^TestSoakSurvival$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep4-r2-codex-soak3.log` plus literal `FAIL` scan — PASS; 90 messages, 9 reminders, cold starts 288/320 ms, RSS 19,072 KB, 14 FDs, WAL 4,136,512 B, journal 876,544 B, maximum backlog 2, exit 0, no `FAIL` token.
- `git archive 10994f2dd564bb0d29a44388ee3b87d809b1c22a | tar -x -C <fresh-temp-dir>` plus source SHA-256 comparison — PASS; the exported soak source matched HEAD byte-for-byte before probes.
- Clean-export `TestSoakVerdictSensitivity` — PASS; the folded RSS/FD/WAL/sparse-latency counterexamples were rejected as intended.
- Clean-export real-PENDING mutation followed by `NEXUS_SOAK_MINUTES=1 ... TestSoakSurvival` — EXPECTED FAIL; the independent S6 assertion reported exactly one non-SENT row.
- Clean-export edge probe with a three-sample catastrophic second incarnation and the collector's unordered map append pattern — UNEXPECTED PASS for the verdict and observed nonchronological iteration, respectively.

## Topknot and proof ceiling

The fold remains confined to the soak test and reuses existing process/store checkers; no dependency or production abstraction was added. One unused local (`rssAll`) is appended but never consumed at `internal/acceptance/soak_linux_test.go:129,139`; delete it (net -2 lines).

The weakest link is that the harness calls traffic fixed-rate and resource checks per-incarnation without structurally enforcing either property in the edge conditions. The green three-minute run proves the low-backlog path on this host, not temporal S4 validity under accumulated backlog or S1/S2 coverage after a slow late restart.

VERDICT: FAIL

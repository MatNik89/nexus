# REVIEW-SOAKS1-R3-kilo — soak-S1 round-3 closure verification (commit cc74288)

**Scope:** commit `cc74288` on `slice/p0-soak-s1` (HEAD), which claims to close the two
round-2 findings (MED + LOW, raised by both reviewers). Read-only QA verification:
(a) is each round-2 finding actually closed, (b) does the diagnosis document's every
causal claim now match its cited evidence.

**Method:** reviewed the round-3 diff; cross-checked `docs/SOAK-S1-DIAGNOSIS.md` claim
by claim against the on-disk logs in `~/HARNESS/nexus-soak/`; confirmed the code delta
from round 2 is empty; ran the two regression tests in a clean detached worktree at
`cc74288` (`/home/matej/qa-soak-s1-r3`), at the reviewer uid.

## Round-2 finding closure

**F1 [MED] — diagnosis doc overclaimed / pre-fix curve uncorroborated. — CLOSED.**

- The pre-fix 20k series is now persisted at `~/HARNESS/nexus-soak/diag-prefix-20k.log`
  (42 lines: start + 40 samples at 500-msg steps + end). All six raw-curve data points
  quoted in the doc match verbatim: `0→20720, 500→23744, 5000→24080, 10000→29488,
  15000→31232, 20000→31008`. The post-fix points match `diag-postfix.log` (unchanged).
- `diag-curve-run.log` is now correctly identified in the doc as "an aborted earlier
  harness run — a timeout panic with one sample — NOT the pre-fix evidence."
- The "Honest attribution" section is rewritten to claim only what the measurements
  establish: "Both 20k curves are concave and flatten in their final thirds" plus
  "no per-message live-heap leak" is stated as "the extent of what the collected
  evidence establishes." The residual saturating growth is explicitly **UNATTRIBUTED**;
  allocator retained spans and page-cache warm-up are named as "hypotheses, not
  findings," with the specific missing controls enumerated (no allocator/RSS
  decomposition, DB-size series, connection-count series, idle/time control). The 24 h
  post-fix run is the stated decision boundary.

**F2 [LOW] — unrelated `.kilo/kilo.jsonc` `$schema` editor artifact. — CLOSED.**

- `git diff 50eef61..HEAD -- .kilo/kilo.jsonc` is empty; the file is back to
  `{ "snapshot": false }`, byte-identical to its pre-slice state.

## Claim-by-claim evidence check

| Doc claim | Evidence | Verdict |
|---|---|---|
| Pre-fix 20k `20.7 → 31.0 MB`, first 10k `+8.8 MB`, second 10k `+1.5 MB` | `diag-prefix-20k.log`: 20720→31008; 20720→29488=+8768 KB; 29488→31008=+1520 KB | matches |
| Post-fix 20k `20.2 → 29.7 MB`, "materially the same shape" | `diag-postfix.log`: 20176→29712; both plateau ~30 MB, totals +10.0 vs +9.3 MB | matches |
| "pool was not the dominant term" | post-fix curve still grows ~9.3 MB with the pool bounded (4 conns), so the dominant growth term persists regardless of the pool | supported |
| pool bound makes SQLite "bounded by design (~6.4 MB)" | `journal.go:168,177-178` `cache_size(-1600)` + `SetMaxOpenConns(4)`; assert RED-capable (round 2) | verified |
| "no per-message live-heap leak" (19 B/turn, 46 B/msg) | in-process heap loops; pprof artifacts still not committed | narrow, hedged (see note) |

## Code delta

`git diff 8acf2aa..cc74288` touches only `.kilo/kilo.jsonc` and docs — the three code
files (`journal.go`, `journal_test.go`, `soak_linux_test.go`) are byte-identical to
round 2, whose RED-capability (non-default `-1600` assert, pool-bound assert, 16 MiB
floor boundary) was already verified. Both focused tests pass at `cc74288`:

- `TestJournalPoolAndCacheBounded` — GREEN
- `TestSoakVerdictSensitivity` — GREEN

## Note (not a finding)

The in-process heap numbers (19 B/turn, 46 B/msg) remain asserted without committed
pprof artifacts, as they have been since round 1; the doc now uses them only for the
narrow claim "no per-message live-heap leak" and explicitly bounds its own reach
("that is the extent of what the collected evidence establishes"). This is an
observation, not a finding, consistent with prior rounds. (Minor: the doc says
"42 samples" for `diag-prefix-20k.log`; the file has 40 sample lines plus a start and
end line.)

## Confidence

The two round-2 findings are closed, verified by direct inspection of the reverted file
and the persisted pre/post logs, and by execution of the regression tests. The doc's
causal claims now match their cited evidence; the residual growth is honestly
unattributed and deferred to the 24 h post-fix run.

VERDICT: PASS

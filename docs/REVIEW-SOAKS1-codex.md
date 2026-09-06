# QA Review: soak-S1 diagnosis and fix

Target: branch `slice/p0-soak-s1`, commit `5f9aba0183ec8c6c088fe6434890fea76ca7f915`.

Method: read-only review of the committed diff, with execution and controlled
mutations in a clean `git archive` export at
`/home/matej/HARNESS/nexus-soaks1-qa.Dbk3QoP1` on the ext4-backed root
filesystem. Probes ran as `uid=1000(matej)`. The source worktree was not used
for execution or mutation.

## Findings

### [MEDIUM] The root-cause mechanism is plausible and bounded by the fix, but the immutable evidence does not prove the “not a real leak” conclusion

`internal/kernel/journal/journal.go:160-174` correctly applies a per-connection
`cache_size=-2000` pragma and caps the pool at four connections. The selected
modernc SQLite driver applies DSN `_pragma` values when each connection opens;
an independent probe reserved four distinct physical connections and observed
`-2000` on all four. This makes the proposed off-Go-heap SQLite page-cache
mechanism technically sound and bounds that mechanism to approximately 7.8
MiB per journal, plus SQLite/driver overhead.

However, commit `5f9aba0` contains no raw 24-hour sample series, heap profiles,
20,000-message curve, connection-count trace, or post-fix same-workload RSS
measurement. The only committed representation of 22,544 -> 35,536 KiB is a
synthetic oracle input at `internal/acceptance/soak_linux_test.go:503-520`.
That proves how the revised oracle classifies those numbers; it cannot prove
why the real process produced them or exclude a concurrent slow leak.

PROBE: tree search found no committed soak/profile artifact beyond the harness;
the dependency and four-connection checks confirmed the proposed mechanism,
but no current-revision before/after workload evidence exists to attribute the
observed RSS delta causally.

FIX: retain the pool/cache bound, but do not close S1 as “not a leak” until a
same-host, same-workload post-fix long run records RSS, database size,
`sql.DBStats` connection counts, and preferably SQLite cache usage. The
decisive result is bounded/saturating RSS under the fixed build while traffic
and database growth continue.

### [LOW] The S1 sensitivity suite does not test its stated slow-leak ceiling

The production oracle at `internal/acceptance/soak_linux_test.go:160-165`
requires both the ratio and an absolute increase greater than 16 MiB. The
committed RED cases at `internal/acceptance/soak_linux_test.go:495-525` are a
+40 MiB endpoint jump and a +45 MiB peak. An independent monotone +15 MiB
growth series over one incarnation passed. Therefore a genuine sub-floor slow
leak does not fail this window, exactly as the file header admits; a longer run
is required. The 16 MiB floor is defensible as a small-baseline cache-warm-up
allowance, but only with this explicit proof ceiling. A boundary test should
lock the intended behavior immediately below and above 16 MiB so later edits
cannot silently widen it.

PROBE: reviewer-only `TestQASoakS1AbsoluteFloorCeiling` passed for monotone
22,544 KiB -> 37,904 KiB (+15 MiB). The committed +40 MiB and +45 MiB cases
remained RED-capable through `TestSoakVerdictSensitivity`.

FIX: add deterministic monotone cases at 16 MiB (accepted by the current `>`
boundary) and 16 MiB + 1 KiB with ratios chosen to exercise the absolute
boundary, and state the maximum undetected rate for the 24-hour window.

### [LOW] The commit contains an unrelated Kilo editor configuration change

`.kilo/kilo.jsonc:2` adds a remote schema URL while the commit otherwise owns
the journal memory bound and soak oracle. It is not causal to soak-S1 and
violates the repository's smallest-causal-diff rule.

PROBE: `git diff 5f9aba0^ 5f9aba0 -- .kilo/kilo.jsonc` shows the single
unrelated `$schema` addition.

FIX: move the Kilo metadata change to a separate commit or drop it from this
slice.

## Pool safety and liveness

No pool-induced deadlock was reproduced at the production read seam. Three
simultaneously open result sets held three distinct connections while the
single append actor committed through the fourth. Production
`QueryProjection` callers close rows, and the writer owns one transaction at a
time. Additional readers queue behind the four-connection cap; they do not
hold journal mutexes required by the writer.

An explicit `BeginTx` “reader” probe was rejected as non-causal: the existing
DSN uses `_txlock=immediate`, so such transactions contend for SQLite's write
lock before the new pool limit matters. The actual `QueryProjection`/open-rows
probe passed.

## RED capability and verification

- Baseline focused tests: `CGO_ENABLED=0 go test -count=1 -run
  'TestJournalPoolAndCacheBounded|TestConcurrentAppendsContiguousOffsets|TestProjectorsIndependent'
  ./internal/kernel/journal` — exit 0.
- Baseline oracle test: `CGO_ENABLED=0 go test -count=1 -run
  TestSoakVerdictSensitivity ./internal/acceptance` — exit 0.
- Pool/cache ablation: removed only `cache_size(-2000)` and both pool setters;
  `TestJournalPoolAndCacheBounded` — exit 1 with
  `MaxOpenConnections=0, want 4`.
- Floor ablation: removed only the absolute-floor predicates;
  `TestSoakVerdictSensitivity` — exit 1 because the committed 22,544 ->
  35,536 KiB case was rejected by the retained ratio.
- Independent connection/liveness probes: all four connections reported
  `cache_size=-2000`; writer progress with three held readers — exit 0.
- `CGO_ENABLED=0 go vet ./...` — exit 0.
- `CGO_ENABLED=0 go test -count=1 ./...` — exit 0; all packages passed, with
  the environment-gated wall-clock soak skipped by design.

## Conclusion

The implementation is a small, technically appropriate bound and the focused
detectors are genuinely RED-capable. The four-connection topology is safe for
the observed single-writer/short-reader production design. The absolute floor
is a reasonable operational budget if its admitted sub-16-MiB/window leak
ceiling is accepted.

The requested stronger conclusion—this was definitely cache warm-up rather
than a real leak—remains unverified at commit `5f9aba0`, and two LOW findings
also remain unresolved. Under the requested all-severity rule, the review
cannot pass.

VERDICT: FAIL

# QA Verification Round 3: soak-S1

Target: `slice/p0-soak-s1` at commit
`cc742888e12929ce7504ddf0b5cc83138380f1e1`.

Method: read-only review of the immutable target tree in clean `git archive`
export `/home/matej/HARNESS/nexus-soaks1-r3-codex.WIhAiX`, stored on ext4
(`/dev/mmcblk0p2`), plus read-only inspection of the cited host logs under
`/home/matej/HARNESS/nexus-soak`. The source worktree was used only for this
requested report.

## Result

Round 3 closes the missing pre-fix-series citation and the unrelated `.kilo`
artifact. It also corrects the diagnosis document's allocator/page-cache
attribution. It does not close every round-2 finding: the same round-2 Codex
finding also required the unsupported production comment to be softened, and
a separate round-2 LOW finding rejected the claimed 6.4 MB SQLite hard cap.
Both claims remain unchanged. Under the requested any-severity failure rule,
the verdict is FAIL.

## Findings

### [CONCRETE][SEV: MED] The production comment still assigns the 24h RSS failure to the pool without causal evidence

`internal/kernel/journal/journal.go:160-164` still says an unbounded pool
"grew daemon RSS with database size until the 24h budget tripped." The cited
measurements have no database-size series, connection-count series, or
allocator/RSS decomposition. The pre/post 20k curves instead have similar
total growth (10,288 KiB pre-fix and 9,536 KiB post-fix), which supports only
the narrower conclusion that the pool/cache change was not the dominant term
for this sequential workload. This source comment was expressly included in
the round-2 MED finding and commit `cc74288` does not change it.

PROBE: `git diff 8acf2aa cc74288 -- internal/kernel/journal/journal.go` is empty;
the two 20k host series were parsed independently, and neither series contains
connection-count or database-size measurements.

FIX: state only that the pool was formally unbounded and is now capped; do not
claim it caused the observed 24h RSS failure or growth with database size.

### [CONCRETE][SEV: LOW] The diagnosis still overstates an approximate page-cache setting as a 6.4 MB SQLite hard cap

`docs/SOAK-S1-DIAGNOSIS.md:37-40` still claims the "SQLite side" has a
"~6.4 MB/journal hard cap regardless of database size or reader concurrency."
The matching overclaim remains at `internal/kernel/journal/journal.go:173-176`.
The implementation proves only four maximum open connections and an effective
`PRAGMA cache_size=-1600`. SQLite documents negative `cache_size` as an
approximately sized, suggested maximum for database page-cache pages, not a
hard bound on all SQLite/driver memory. The round-2 Codex report identified
this as a separate LOW finding; round 3 leaves both statements unchanged.

PROBE: `internal/kernel/journal/journal_test.go:714-729` asserts only
`MaxOpenConnections == 4` and `PRAGMA cache_size == -1600`. SQLite's primary
documentation calls `cache_size` a "suggested maximum" and explains that a
negative value is converted to a page count using approximately `abs(N*1024)`
bytes: <https://www.sqlite.org/pragma.html#pragma_cache_size>.

FIX: say "approximately 6.4 MiB aggregate configured main-database page-cache
envelope across at most four connections, plus SQLite/driver overhead."

## Round-2 closure matrix

| Round-2 requirement | Evidence at `cc74288` | Closure |
|---|---|---|
| Diagnosis limits allocator/page-cache explanations to hypotheses and retains the 24h boundary | `SOAK-S1-DIAGNOSIS.md:31-49,51-59` now separates measured curve shape from hypotheses and says the post-fix 24h run is pending | PASS for the document; FAIL for the unchanged causal source comment above |
| Persist and correctly cite the pre-fix 20k series | `diag-prefix-20k.log` exists, has SHA-256 `0ac812763b034fe56a412089f19f8db3cef7e5828718cd4387e71107431cc670`, and contains 42 `RSSCURVE` lines (41 unique message counts plus the duplicate end marker); all six cited points match | PASS |
| Identify `diag-curve-run.log` as aborted | It contains one `RSSCURVE` sample followed by `panic: test timed out after 1h23m20s` | PASS |
| Remove unrelated `.kilo/kilo.jsonc` schema artifact | `git diff --exit-code 50eef61 cc74288 -- .kilo/kilo.jsonc` exits 0 | PASS |
| Replace the claimed 6.4 MB SQLite hard cap with the actual configured page-cache envelope | Wording is unchanged in the diagnosis and source | FAIL |

## Diagnosis claim-to-evidence audit

- `SOAK-S1-DIAGNOSIS.md:5-13`: the cited 24h log exactly contains the reported
  S1 median failure, `22544KB -> 35536KB`. Because `soakVerdict` returns on S1
  before later budget checks, this short failure log does not independently
  establish the additional sentence that FD, WAL, latency, and cadence all
  passed; that sentence is outside the causal closure claimed by round 3.
- Lines 17-23: the 19 B/turn and 46 B/message heap results support the scoped
  statement that no material linear live-heap retention was found in those two
  measured paths. Their raw profiles are not among the cited host artifacts,
  so this round did not independently reproduce those measurements.
- Lines 24-29 and 63-70: every displayed pre/post point matches the cited host
  series. Both runs have 41 unique samples from 0 through 20,000 messages. The
  second-half endpoint growth is smaller than the first-half growth in each
  run (pre: 8,768 then 1,520 KiB; post: 5,520 then 4,016 KiB), supporting the
  document's limited concave/flattening description despite noisy samples.
- Lines 33-36: the conclusion is correctly scoped to live heap in the turn and
  adapter/outbox paths; it does not claim to decompose process RSS.
- Lines 37-40: FAIL as written; the configured page-cache envelope is not a
  hard bound on the whole SQLite side.
- Lines 41-49: the similar total pre/post growth supports "not the dominant
  term" for this workload. Allocator spans and page-cache warm-up are now
  expressly hypotheses, the missing controls are listed, and the 24h post-fix
  run is correctly retained as the decision boundary.
- Lines 53-55: the stated oracle ceiling matches the strict `> 16 MiB` checks
  at `internal/acceptance/soak_linux_test.go:162-168`; exactly 16 MiB passes and
  16 MiB + 1 KiB fails in `TestSoakVerdictSensitivity`.
- Lines 56-59: no post-fix 24h result is present, so "pending" and
  "best-supported hypothesis, not a closed verdict" are accurate.

## Verification and limits

- `CGO_ENABLED=0 /home/matej/.local/go/bin/go vet ./...`: exit 0.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...`: exit 0;
  all packages passed in the clean export, including `internal/acceptance` in
  110.616 seconds. The environment-gated 24h soak was not run.
- The host logs are mutable external artifacts and do not bind their producing
  revision or command. Their contents establish the cited samples, not
  provenance equivalent to a revision-bound benchmark bundle.
- Weakest link: pre-fix provenance is limited to the newly persisted sample
  series; it is not a complete run transcript and has no embedded commit hash.

Topknot: the round-3 doc edit and `.kilo` deletion are small and dependency-free;
no additional simplification finding was found. Skill update: none; existing
revision-binding and prior-finding closure rules already expose both misses.

VERDICT: FAIL

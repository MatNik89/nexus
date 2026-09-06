# QA Verification Round 2: soak-S1

Target: `slice/p0-soak-s1` at commit
`8acf2aacdd36d8c9fcdbc0fc5d6fea4003ad6f13`.

Method: read-only review of the target revision. Execution and controlled
production-code mutations were confined to the clean `git archive` export
`/home/matej/HARNESS/nexus-soaks1-r2.Z4LOSsmr`, on `/dev/mmcblk0p2` (`ext4`),
as `uid=1000(matej)`. The export was byte-restored after each mutation and
deleted after verification. The source worktree was used only for this
requested report.

## Result

The new detectors are genuinely RED-capable and the repository-wide gates are
GREEN. However, two round-1 closure requirements remain unresolved. Under the
requested rule that any unresolved severity fails the review, the overall
verdict is FAIL.

## Findings

### [CONCRETE][SEV: MED] The diagnosis still states causes that its measurements do not establish

`docs/SOAK-S1-DIAGNOSIS.md:24-29,58-61` records materially similar pre/post
20k RSS curves. That supports the narrow conclusion at lines 39-40: bounding
the pool and changing the cache declaration did not materially change the
observed curve, so the formerly unbounded pool was not the dominant observed
growth term in that workload.

It does not establish the stronger statements at lines 33-44 that the
remainder is specifically Go allocator retained spans plus page-cache warming,
that it is tied to database size, or that it is not tied to time. The document
presents no DB-size series, allocator/RSS decomposition, connection-count
series, or idle/time control. The cited pre-fix “full log” at lines 62-63 also
does not preserve the stated curve on this host: it contains only the initial
`20720KB` sample and then `panic: test timed out`; the cited post-fix log does
contain the complete 20k series and PASS.

The proof ceiling at lines 51-54 correctly makes the final “bounded, not a
leak” conclusion a hypothesis pending the owner-scheduled 24h post-fix soak,
but it does not turn the preceding causal assertions into evidence. The source
comment at `internal/kernel/journal/journal.go:160-164` likewise still says the
unbounded pool grew daemon RSS until the 24h budget tripped, which is stronger
than the observed pre/post comparison supports.

PROBE: compared the committed document with both cited host logs and the
available measurements. The post-fix log has samples 0 through 20,000 and
passes; the pre-fix log has no samples after zero and exits on timeout.

FIX: retain the observed curve and pool-not-dominant conclusion, but label the
remainder's allocator/cache/DB-size/time explanation as a hypothesis. Soften
the journal comment to say the pool was formally unbounded and is now bounded,
without claiming it caused the 24h trip. Preserve the next same-workload 24h
run as the decision boundary.

### [CONCRETE][SEV: LOW] Codex round-1's unrelated-change finding was not closed

`docs/REVIEW-SOAKS1-codex.md:62-72` required the unrelated Kilo editor schema
addition to be removed or separated. At target revision 8acf2aa,
`.kilo/kilo.jsonc:2` still adds
`"$schema": "https://app.kilo.ai/config.json"` relative to the pre-slice base
`50eef61`. Commit 8acf2aa does not touch that file.

PROBE: `git diff 50eef61 8acf2aa -- .kilo/kilo.jsonc` reproduces the one-line
addition; `git diff --name-status 5f9aba0 8acf2aa` confirms no closure change to
`.kilo/kilo.jsonc` in round 2.

FIX: revert the schema line from this slice or move it to a separate commit.

### [CONCRETE][SEV: LOW] The stated 6.4 MB hard cap is broader than the implemented guard

`docs/SOAK-S1-DIAGNOSIS.md:35-38` calls approximately 6.4 MB a hard cap on the
“SQLite side,” and `internal/kernel/journal/journal.go:173-176` similarly says
the SQLite memory is bounded to that amount. The implementation caps four
open connections and requests approximately 1.6 MiB of main-database page
cache per connection. `PRAGMA cache_size` is a suggested page-cache maximum,
and its negative value is converted to an approximate page count; it is not a
hard bound on all SQLite/driver memory or overhead.

PROBE: the production test proves `MaxOpenConnections == 4` and the effective
`PRAGMA cache_size == -1600`. It does not measure or cap total SQLite bytes.
[SQLite's `PRAGMA cache_size` contract](https://www.sqlite.org/pragma.html#pragma_cache_size)
describes a suggested page-cache limit, not a total-memory hard limit.

FIX: say “approximately 6.4 MiB aggregate configured page-cache envelope for
the four open connections, plus SQLite/driver overhead,” not “SQLite hard
cap.”

## Closure and RED-capability

| Round-1 item | Target evidence | Closure |
|---|---|---|
| Codex MED: post-fix measurement and honest attribution | Post-fix 20k series is committed and externally present; pre/post similarity supports “pool not dominant”; causal remainder is still overstated | FAIL |
| Codex LOW: exact 16 MiB boundary and stated blind spot | `soak_linux_test.go:9-19,523-550`; exact +16 MiB passes and +16 MiB+1 KiB fails | PASS |
| Codex LOW: unrelated `.kilo` change | Still present at `.kilo/kilo.jsonc:2` | FAIL |
| Kilo F1/F2: non-default cache declaration and sensitive assertion | `journal.go:165-168`; `journal_test.go:723-729`; dropping only the pragma yields `-2000` and RED | PASS |

Executed evidence at reviewer UID:

- Baseline journal guard and neighboring tests: PASS.
- Baseline `TestSoakVerdictSensitivity`: PASS.
- Remove only `cache_size(-1600)`: RED, observed `cache_size ... -2000`.
- Remove both pool setters: RED, observed `MaxOpenConnections=0, want 4`.
- Remove the absolute-floor predicates: RED on the 22,544 to 35,536 KiB
  warm-up case.
- Change strict `>` to `>=`: RED because exactly +16 MiB was rejected.
- Widen the floor by 1 KiB: RED because +16 MiB+1 KiB was not caught.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go vet ./...`: exit 0.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...`: exit 0;
  all packages passed. The 24h soak remains environment-gated and was not run.

## Review limits

The clean-export suite proves the committed detectors and ordinary repository
tests at this UID. It does not prove 24h post-fix saturation, total SQLite
memory consumption, or the unsupported pre-fix raw series. The strongest
counterargument is that the document explicitly labels its final “not a leak”
conclusion a hypothesis; that caveat is valid, but it does not cure the
definitive causal statements earlier in the same document.

Topknot: the behavioral patch remains small and dependency-free. The unrelated
editor configuration is the only avoidable non-causal slice change; otherwise
the round-2 detector changes are lean.

Skill update: none; the existing revision-binding and prior-finding closure
rules exposed both unresolved items.

VERDICT: FAIL

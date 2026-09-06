# QA Verification Round 4: soak-S1

Target: `slice/p0-soak-s1` at commit
`aac5e2ac542d43726dbe967b737c2032d3e62701`.

Method: solo read-only review of the immutable target tree in clean `git
archive` export `/home/matej/HARNESS/nexus-soak-r4.dKzPau` on ext4
(`/dev/mmcblk0p2`), plus read-only inspection of the cited host logs under
`/home/matej/HARNESS/nexus-soak`. The source worktree was used only for this
requested report.

## Result

Commit `aac5e2a` closes both round-3 Codex findings. The production comment no
longer attributes the 24-hour RSS failure to the connection pool, and neither
the comment nor the diagnosis describes `cache_size` as a hard cap on total
SQLite/driver memory. No unresolved finding of any severity remains in the
requested claim scope.

## Round-3 closure

| Round-3 finding | Evidence at `aac5e2a` | Closure |
|---|---|---|
| MED: `journal.go` assigned the 24-hour RSS failure to the pool without causal evidence | `internal/kernel/journal/journal.go:160-165` now says the pool was formally unbounded, the measurements attribute no specific cause, the pre/post curves make the pool non-dominant on that workload, and the bound is a by-design guard rather than a proven fix | PASS |
| LOW: source and diagnosis called about 6.4 MB a hard cap on the whole SQLite side | `journal.go:174-178` and `docs/SOAK-S1-DIAGNOSIS.md:37-41` now limit the statement to approximate, suggested page-cache maxima and expressly exclude all other SQLite/driver memory | PASS |

The removed formulations (`primary memory cap`, `unbounded pool grew daemon
RSS`, and `hard cap regardless of database size or reader concurrency`) are
absent from both scoped files.

## Claim-to-evidence audit

- **24-hour observation:** `soak-24h-20260905-2114.log` contains the exact S1
  result `22544KB -> 35536KB`; that is a 12,992 KiB increase. The log does not
  identify a cause.
- **Pool state:** before commit `5f9aba0`, `journal.Open` did not call
  `SetMaxOpenConns`; Go's `database/sql` default is zero, meaning unlimited.
  At `aac5e2a`, `SetMaxOpenConns(4)` caps simultaneous open connections and
  `SetMaxIdleConns(4)` caps retained idle connections. This proves the narrow
  "formally unbounded, now bounded" statement.
- **Cache declaration:** the production DSN declares
  `_pragma=cache_size(-1600)`. The pinned modernc driver applies every `_pragma`
  value when a connection opens, and the focused test observes effective
  `PRAGMA cache_size == -1600`. SQLite documents a negative value as an
  approximate byte-based conversion to a suggested page-count maximum per
  open database file; its default page cache treats that as a suggested upper
  bound rather than total SQLite memory. Four connection-local maxima of about
  1,600 KiB give an aggregate configured page-cache envelope of about 6,400
  KiB (6.4 MB, approximately 6.25 MiB). The new wording accurately limits both
  object and precision. Primary reference:
  <https://www.sqlite.org/pragma.html#pragma_cache_size>.
- **Pre/post comparison:** the persisted series each contain 41 unique points
  from zero through 20,000 messages. Pre-fix RSS grows 10,288 KiB
  (`20720 -> 31008`); post-fix RSS grows 9,536 KiB (`20176 -> 29712`). The
  similar totals and continued post-fix growth support only the stated
  workload-local inference that the pool/cache change was not the dominant
  observed term. They do not establish the remaining cause, and both files
  now say so.
- **Curve shape:** the quoted points match the host logs exactly. Pre-fix
  endpoint growth is 8,768 KiB in the first 10,000 messages and 1,520 KiB in
  the second; the post-fix final-third least-squares slope is approximately
  `-0.0064 KiB/message`. This supports the limited concave/flattening
  description. The diagnosis correctly leaves 24-hour post-fix saturation
  pending rather than promoting the accelerated curves to a final result.
- **Heap-path exclusion:** the diagnosis restricts the 19 B/turn and 46 B/msg
  observations to live heap in the measured turn and adapter/outbox paths; it
  does not use them to decompose RSS or name the residual cause. The raw heap
  profiles and temporary measurement harness are not retained, so the exact
  values were not independently reproducible in this round. This is an
  evidence-provenance ceiling, not a contradiction in the revised causal or
  bounding claims.
- **S1 detector ceiling:** `soakVerdict` uses strict conjunctions: ratio growth
  must exceed 1.5x (median) or 1.6x (peak), and absolute growth must exceed
  16 MiB. `TestSoakVerdictSensitivity` proves exactly 16 MiB passes and 16 MiB
  plus 1 KiB fails for the ratio-satisfying fixture. The diagnosis's one-way
  statement that sub-16-MiB growth is undetected is correct; it does not claim
  that every larger leak is detected independent of the ratio gates.

## Verification

- Export identity: SHA-256 of exported `journal.go` and the diagnosis exactly
  matched `git show aac5e2a:<path>`; both paths are tracked in the target tree
  and neither is ignored or supplied by an untracked overlay.
- `CGO_ENABLED=0 go test -count=1 ./internal/kernel/journal -run
  '^TestJournalPoolAndCacheBounded$'`: PASS (`0.420s`).
- `CGO_ENABLED=0 go test -count=1 ./internal/acceptance -run
  '^TestSoakVerdictSensitivity$'`: PASS (`6.359s`).
- `CGO_ENABLED=0 go vet ./...`: PASS.
- `CGO_ENABLED=0 go test -count=1 ./...`: PASS from the clean export; all
  packages passed, including `internal/acceptance` (`85.624s`).
- An earlier full-suite attempt forced `TMPDIR` under the long export path and
  failed because Unix socket paths became invalid and sandbox fixtures were
  outside their required `/run/user/1000` root. Re-running without that
  noncanonical override passed; those environment-induced failures are not
  candidate evidence.

## Limits and challenge pass

The 24-hour post-fix soak was not run. The mutable host logs establish their
contents but do not embed a producing commit or complete command, and the heap
profiles are absent. Therefore this review proves the honesty and technical
accuracy of the narrowed claims, the configured guards, and the committed
detectors; it does not prove that fixed-build RSS saturates over 24 hours or
identify the residual RSS contributor.

Strongest counterargument: "bounds the page caches" could still be read as a
hard byte ceiling in isolation. In context it is immediately qualified as a
"SUGGESTED" maximum, "approximate", and page-cache-only, which matches SQLite's
contract and removes the round-3 overclaim.

Weakest link: the pre/post diagnostic logs are not revision-bound benchmark
bundles. Upgrade this proof when the pending 24-hour fixed-build run records
its commit, command, RSS series, database size, and connection counts.

Topknot review: lean already; the round-4 change is comment/document wording
only, introduces no dependency or abstraction, and makes the smallest causal
correction. Skill update: none; the existing revision-binding and causal-claim
checks were sufficient for this re-review.

VERDICT: PASS

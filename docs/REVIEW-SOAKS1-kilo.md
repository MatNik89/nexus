# REVIEW-SOAKS1-kilo — soak-S1 diagnosis & fix (commit 5f9aba0)

**Scope:** commit `5f9aba0` on `slice/p0-soak-s1` (HEAD). Read-only QA verification of
the S1 RSS diagnosis (unbounded `database/sql` pool × per-connection SQLite page cache)
and its fix (bounded pool + `cache_size(-2000)` + 16 MiB absolute floor).

**Method:** reviewed the full diff and surrounding owners (`journal.go`,
`projection.go`, `soak_linux_test.go`, `daemon.go`); inspected the modernc.org/sqlite
v1.58.0 driver source for `_pragma`/`cache_size` semantics; ran the regression tests and
performed ablations in a clean detached worktree at `5f9aba0`
(`/home/matej/qa-soak-s1`), at the reviewer uid. All probes executed, none inferred.

## Q1 — Is the diagnosis sound (not masking a real leak)?

**Mechanism is real.** `database/sql` defaults `MaxOpenConns=0` (unlimited);
`SetMaxOpenConns`/`SetMaxIdleConns` are the only bounds. SQLite's page cache is
per-connection (each pooled connection owns one). The daemon shares ONE pool across the
append actor and every read path — `channel`, `memory`, `schedule`, `obligation`,
`approval` all reach it via `QueryProjection` on the same `j.db`
(`journal.go:210`, `channel.go:435..644`, `schedule.go:348,445`, `memory/store.go:444`),
and `main.go:202/221/281` run the heartbeat, scheduler and telegram adapter as
concurrent goroutines. So the pool was genuinely subject to concurrent open under
real soak traffic, and "unbounded pool × per-connection cache, filling as the DB grows
then saturating" is a coherent mechanism for a concave, bounded RSS curve — distinct
from a linear leak.

**Not clearly masking a leak, but the evidence is out-of-repo.** The "not a leak"
conclusion rests on (a) daemon-core heap 19 B/turn live, (b) telegram+outbox 46 B/msg
live, (c) an accelerated 20k-message concave/saturating RSS curve. None of these
artifacts are committed; I could not inspect or reproduce them. The mechanism is
plausible and the fix is self-checking (if the growth were a real >16 MiB/day leak, the
next 24 h soak still fails the budget), but the specific "not a leak" attribution is
asserted, not independently provable from the repo.

## Q2 — Is the pool bound safe (no deadlock)?

**Safe.** The single append actor holds exactly one connection per `appendBatch`
(`journal.go:536` `j.db.Begin()` … `tx.Commit()`) and never acquires a second while
holding one; sync projections fold into that same transaction (`projection.go:106`),
not a new connection. Readers are short `QueryContext` calls (ctx-bounded) or
startup/recovery-only full scans (`Replay`, `verifyChainFull`, `catchupProjections`)
that run before the actor starts. WAL mode means readers do not block the writer and
the writer does not block readers (`busy_timeout(5000)` covers the rare write-write
contention). There is no circular wait: the writer never needs a 2nd connection, and no
read transaction blocks on the writer. Worst case is transient head-of-line latency if
more than 3 readers hold connections simultaneously, bounded by short query durations.
The one two-connection pattern (`Projector.Run`: `Replay` cursor + per-event `Begin`,
`projection.go:168-169`) is not wired into production (`NewProjector` has no non-test
caller).

## Q3 — Is the 16 MiB absolute floor defensible?

**Defensible, with a real sensitivity reduction.** The arithmetic is correct:
`mL*2 > mF*3` ⇔ median > 1.5×, `peakRSS*5 > mF*8` ⇔ peak > 1.6×, and both now require
`delta > 16384 KB` (`soak_linux_test.go:160-166`). A genuine slow leak must therefore
exceed **both** 1.5× **and** 16 MiB absolute to fail; a sub-16 MiB/day leak is now
invisible to S1. That is a genuine loosening, but it is explicitly documented in the
same file header ("a sub-budget slow leak needs a longer run — stated, not hidden"),
and 16 MiB/day is a defensible absolute threshold for a 24 h window-scoped budget. The
real 24 h shape (22.5→35.5 MB, +13 MB) is only ~3 MiB under the floor, so the floor is
calibrated to the pre-fix warm-up; the post-fix warm-up should be far smaller (bounded
pool), leaving comfortable headroom. A >16 MiB/day leak still fails (proven below).

## Q4 — RED-capability probes (executed at reviewer uid)

| Probe | Result |
|---|---|
| `TestJournalPoolAndCacheBounded` (committed) | GREEN |
| `TestSoakVerdictSensitivity` (committed) | GREEN (warm-up passes; +40 MB leak RED; +45 MB peak RED; pre-restart peak RED; FD/WAL/latency/invalid-sample/short-incarnation/vacuous probes all RED) |
| Ablation 1a — remove `SetMaxOpenConns(4)`/`SetMaxIdleConns(4)` **and** `cache_size(-2000)` | RED: `MaxOpenConnections=0, want 4` |
| Ablation 1b — remove **only** `_pragma=cache_size(-2000)`, keep pool bound | GREEN |
| Ablation 2 — remove the 16 MiB floor conditions | RED: warm-up case `22544KB -> 35536KB` fails |
| Full `internal/kernel/journal` + `internal/acceptance` suites | GREEN |
| `go vet` (changed packages) / `go build -buildvcs=false ./...` | clean / exit 0 |

The pool-bound assertion is RED-capable (Ablation 1a). The floor assertion is
RED-capable (Ablation 2). The cache_size assertion is **not** (Ablation 1b).

## Findings

**F1 [LOW] — `_pragma=cache_size(-2000)` is a no-op; the commit overstates its role.**
modernc.org/sqlite v1.58.0 compiles `SQLITE_DEFAULT_CACHE_SIZE = -2000`
(`modernc.org/sqlite@v1.58.0/lib/sqlite.go:3366`) and initializes each new connection's
Btree with `-2000` (`lib/sqlite_*_amd64.go:10276`). Declaring `cache_size(-2000)`
(`journal.go:165`) sets the value to the driver default, so it contributes nothing to
the memory cap. The entire cap comes from `SetMaxOpenConns(4)` (`journal.go:173-174`).
The comment (`journal.go:160-164,170-172`) and the commit message frame `cache_size` as
a co-equal bounding measure ("cache_size is DECLARED … never inherited … memory cap is
maxOpenConns x cache_size"), which is misleading — the ~8 MB figure holds because the
default cache is 2 MB/connection, not because of the declaration.

**F2 [LOW] — the cache_size regression assertion is vacuous.**
`TestJournalPoolAndCacheBounded` asserts `PRAGMA cache_size == -2000`
(`journal_test.go:720-724`). Because `-2000` is the default, this check passes with the
`_pragma` removed (Ablation 1b), so it cannot distinguish "declared" from "default" and
does not prove the cache_size half of the fix has any effect. Only the
`MaxOpenConnections=4` assertion (`journal_test.go:716-717`) is RED-capable.

**Observation (not a finding):** the diagnostic evidence (heap profiles, 20k curve) is
not in-repo, so "not a leak" is asserted rather than reproducible; the fix is
directionally correct and self-checking via the next 24 h soak regardless.

## Confidence

Provable from committed code + executions: the pool was unbounded (Ablation 1a); the
pool bound and the floor are RED-capable; the fix compiles and the suites pass. Not
independently verified: the heap-profile numbers and the saturating-curve shape that
underpin the "not a leak" claim (out-of-repo). The two findings are objective and
reproduced.

VERDICT: FAIL

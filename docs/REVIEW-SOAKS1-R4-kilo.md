# REVIEW-SOAKS1-R4-kilo — soak-S1 round-4 closure verification (commit aac5e2a)

**Scope:** commit `aac5e2a` on `slice/p0-soak-s1` (HEAD), which closes the two round-3
findings (codex MED + LOW): the `journal.go` comment's causal attribution to the pool,
and the "~6.4 MB hard cap" overclaim. Read-only QA verification of those two closures
plus every remaining causal/bounding claim in `journal.go` and
`docs/SOAK-S1-DIAGNOSIS.md` against evidence.

**Method:** reviewed the round-4 diff; read the current `journal.go` Open comment block
and the full diagnosis doc; confirmed the code delta from round 3 is comments-only;
ran the two regression tests, `go vet`, and `go build` in a clean detached worktree at
`aac5e2a` (`/home/matej/qa-soak-s1-r4`), at the reviewer uid.

## Round-3 finding closure

**F1 [MED] — journal.go comment assigned the 24h RSS failure to the pool. — CLOSED.**

The comment (`journal.go:160-166`) now states only what is established: "the pool was
formally UNBOUNDED when the first 24h soak tripped its RSS budget (S1, 2026-09-06)",
"Measurements attribute NO specific cause", "pre/post 20k curves grew about the same,
so the pool was not the dominant term on that workload — this is a by-design guard, not
the proven fix." No causal assignment of the observed failure to the pool remains.

**F2 [LOW] — cache_size described as a ~6.4 MB hard cap on all SQLite memory. — CLOSED.**

Both sites now qualify it correctly:
- `journal.go:172-176`: "4 connections x the ~1.6MB SUGGESTED page-cache maximum bound
  the page caches to roughly 6.4MB per journal (cache_size is approximate and covers
  page cache only, not all SQLite/driver memory)."
- `SOAK-S1-DIAGNOSIS.md:37-41`: "bounds the SQLite **page caches** to a suggested
  maximum of roughly 6.4 MB/journal (`cache_size` is approximate and does not bound all
  SQLite/driver memory)."

This matches SQLite semantics (`cache_size` is a suggested maximum for the page cache;
a negative value is approximately `abs(N*1024)` bytes) and the arithmetic (`-1600` KiB
≈ 1.6 MB × 4 connections ≈ 6.4 MB).

## Remaining causal/bounding claims — all match evidence

| Claim | Evidence | Verdict |
|---|---|---|
| pool was "formally UNBOUNDED" before the fix | `database/sql` default `MaxOpenConns=0`; Ablation (round 2) `MaxOpenConnections=0` | true |
| "pre/post 20k curves grew about the same → pool not the dominant term" | `diag-prefix-20k.log` +10288 KB; `diag-postfix.log` +9536 KB | supported |
| "by-design guard, not the proven fix" | no causal evidence collected (per doc) | honest |
| `cache_size(-1600)` "deliberately NOT the driver default (-2000)" | modernc `SQLITE_DEFAULT_CACHE_SIZE = -2000`; assert RED when pragma dropped | true |
| "~1.6 MB suggested page-cache maximum, ~6.4 MB aggregate, page cache only" | SQLite `cache_size` semantics; `-1600` KiB ≈ 1.6 MB | accurate |

## Code delta

`git diff cc74288..aac5e2a` on `journal.go`, `journal_test.go`, `soak_linux_test.go`
contains **no non-comment change** (verified: filtering out comment lines yields empty).
The functional fix (DSN, `SetMaxOpenConns(4)`/`SetMaxIdleConns(4)`, 16 MiB floor,
boundary tests) is byte-identical to round 3, whose RED-capability was already proven.
At `aac5e2a`:

- `TestJournalPoolAndCacheBounded` — GREEN
- `TestSoakVerdictSensitivity` — GREEN
- `go vet` / `go build -buildvcs=false ./...` — clean / exit 0

## Observation (not a causal/bounding finding)

`SOAK-S1-DIAGNOSIS.md:12` — "FD budget, WAL budget, latency and cadence all passed" —
is imprecise but outside the round-4 scope. `soakVerdict` checks S1 median first and
returns on it, so in the 24h run S2 (FD), S3 (WAL) and S4 (latency) were never
evaluated; only cadence (checked before the verdict) demonstrably passed. This is a
factual context sentence, not a causal or bounding claim, and codex round 3 recorded it
as an audit note rather than a formal finding. Flagging it so the owner can tighten the
wording if desired; it does not affect the RSS diagnosis substance.

## Confidence

Both round-3 findings are closed, verified by reading the current comments and doc
against the persisted logs and SQLite semantics. Every remaining causal/bounding claim
matches its evidence. The code is unchanged from the RED-proven round 3.

VERDICT: PASS

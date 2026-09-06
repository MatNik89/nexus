# REVIEW-SOAKS1-R2-kilo — soak-S1 round-2 closure verification (commit 8acf2aa)

**Scope:** commit `8acf2aa` on `slice/p0-soak-s1` (HEAD), which claims to close all
round-1 findings. Read-only QA verification: (a) is each round-1 finding actually
closed, (b) are the new detectors RED-capable, (c) do the diagnosis document's claims
match its evidence.

**Method:** reviewed the round-2 diff and the three round-1 review docs
(`REVIEW-SOAKS1-{codex,agy,kilo}.md`); ran tests and ablations in a clean detached
worktree at `8acf2aa` (`/home/matej/qa-soak-s1-r2`); cross-checked every data point in
`docs/SOAK-S1-DIAGNOSIS.md` against the on-disk soak logs in
`~/HARNESS/nexus-soak/`. All probes executed at the reviewer uid.

## Round-1 finding inventory and closure status

Round-1 produced **five** findings: codex MED (attribution), codex LOW (floor
boundary), codex LOW (unrelated `.kilo/kilo.jsonc` change), kilo F1 (no-op cache_size),
kilo F2 (vacuous assert). The commit message enumerates three closures and omits the
fourth.

| # | Round-1 finding | Round-2 status |
|---|---|---|
| 1 | codex MED — attribution stronger than evidence | Partially closed (see F1) |
| 2 | codex LOW — floor boundary not locked | **Closed** |
| 3 | codex LOW — unrelated `.kilo/kilo.jsonc` `$schema` | **NOT closed** |
| 4 | kilo F1 — `cache_size(-2000)` no-op | **Closed** |
| 5 | kilo F2 — cache_size assert vacuous | **Closed** |

## RED-capability probes (executed at reviewer uid)

| Probe | Result |
|---|---|
| `TestJournalPoolAndCacheBounded` (committed) | GREEN — asserts `MaxOpenConnections=4` and `PRAGMA cache_size = -1600` |
| `TestSoakVerdictSensitivity` (committed) | GREEN — incl. new boundary cases |
| Ablation: drop `cache_size(-1600)` pragma only | **RED**: `cache_size not the declared non-default -1600: -2000` |
| Ablation: drop `SetMaxOpenConns(4)`/`SetMaxIdleConns(4)` | **RED**: `MaxOpenConnections=0, want 4` |
| Ablation: drop the 16 MiB floor predicates | **RED**: warm-up case `22544KB -> 35536KB` fails |
| Full `internal/kernel/journal` + `internal/acceptance` suites | GREEN |
| `go vet` / `go build -buildvcs=false ./...` | clean / exit 0 |

The `-1600` change is verified non-default and enforceable: dropping the pragma now
turns the assert RED (kilo F2 closed). The pool bound and the floor are RED-capable.
The floor boundary cases are arithmetically correct: `edge` (+16384 KB exactly) passes
the strict `>`, `over` (+16385 KB) fails — traced through `soakVerdict` with
`warmup=3, expectedIncarnations=1`, `mF=22544`, `mL=38928/38929`.

## Evidence cross-check of `docs/SOAK-S1-DIAGNOSIS.md`

The doc's raw-curve section cites two logs: `diag-curve-run.log` (pre) and
`diag-postfix.log` (post). Both exist on the dev host. Cross-checking them:

- **Post-fix (`diag-postfix.log`): matches exactly.** 41 samples at 500-msg intervals;
  `0→20176, 500→22688, 5000→26000, 10000→25696, 15000→31600, 20000→29712` all present
  verbatim. The post-fix half of the evidence is traceable.
- **Pre-fix (`diag-curve-run.log`): does NOT contain the cited data.** The file is a
  `panic: test timed out after 1h23m20s` from `TestSoakDiagRSSCurve` containing exactly
  **one** sample line — `RSSCURVE start msgs=0 rssKB=20720` — then the timeout panic.
  The five other pre-fix points quoted in the doc (`500→23744, 5000→24080,
  10000→29488, 15000→31232, 20000→31008`) appear in **no log on the host** (grep across
  `~/HARNESS` finds them only in the doc and its copies). `diag-rss-curve.log` is a
  single `timestamp value` pair, not a curve; `soak-24h-20260905-2114.log` is the 24 h
  verdict only.

Consequence: the doc's headline comparison — "the pre/post curves are materially the
same" and therefore "the unbounded pool was **not the dominant term**" — rests on a
pre-fix curve that is not traceable to the cited (timeout) log. The post-fix curve
alone shows the pool bound did not *eliminate* ~9.3 MB of growth, but it cannot by
itself support "materially the same" or "pool not dominant"; those require the missing
pre-fix series. The doc does hedge the ultimate conclusion correctly ("bounded, not a
leak" is "the best-supported hypothesis, not a closed verdict"), which satisfies the
honesty half of the codex MED finding, but the new factual evidence it introduces is
only half-verifiable.

## Findings

**F1 [MEDIUM] — `SOAK-S1-DIAGNOSIS.md` cites a pre-fix curve that does not exist in
the referenced log.** `diag-curve-run.log` (cited as "pre") is a timeout panic with one
sample; the quoted pre-fix samples are untraceable on the host. "Pre/post materially
the same" and "unbounded pool was not the dominant term" are therefore unsupported by
the cited evidence. This is the same evidence-vs-attribution class as the codex MED
finding round 2 was meant to close.

**F2 [LOW, unresolved from round 1] — the unrelated `.kilo/kilo.jsonc` `$schema`
change remains.** Introduced in `5f9aba0` (`.kilo/kilo.jsonc:2`), flagged LOW by codex
("not causal to soak-S1, violates smallest-causal-diff; move or drop"), and neither
moved nor dropped in `8acf2aa`; the round-2 commit message does not mention it.

## Confidence

Verified by execution: `-1600` is non-default and its assert is RED-capable; the pool
bound and floor are RED-capable; the boundary cases lock the floor; the two changed
packages build and pass. Verified by log inspection: the post-fix curve matches its
log; the pre-fix curve does not. The two findings are concrete and reproducible, not
inferred.

VERDICT: FAIL

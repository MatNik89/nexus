# Soak S1 diagnosis — daemon RSS growth (2026-09-06/07)

## The failure

The first real 24h soak (`NEXUS_SOAK_MINUTES=1440`, log
`soak-24h-20260905-2114`) failed exactly one oracle:

```
S1 inc1 RSS median grew 22544KB -> 35536KB (>1.5x budget)
```

FD budget, WAL budget, latency and cadence all passed. Isolated,
monotonic-looking RSS growth of ~13 MiB over 24h (~43k messages).

## Evidence collected (all on the same host, Pi 4-core/8GB)

1. **Daemon-core heap** (in-process `RunChannelTurn` loop, 4000 turns,
   double-GC before/after): **19 B/turn live** — no per-turn leak in the
   turn pipeline.
2. **Telegram+outbox path heap** (in-process adapter+channel-core loop,
   8000 messages, interleaved single-batch fixture): **46 B/msg live**,
   pprof shows no NEXUS frame above noise — no per-message leak in the
   adapter/outbox path.
3. **Accelerated black-box curve, PRE-fix** (real daemon binary, 20k
   messages): 20.7 → 31.0 MB, strongly **concave/saturating** (first
   10k: +8.8 MB; second 10k: +1.5 MB).
4. **Accelerated black-box curve, POST-fix** (same workload, pool
   bounded + cache declared): 20.2 → 29.7 MB — **materially the same
   shape**.

## Honest attribution

- Both 20k curves are **concave and flatten in their final thirds**;
  the in-process heap measurements (points 1-2) rule out a per-message
  live-heap leak in the turn and adapter/outbox paths. That is the
  extent of what the collected evidence establishes.
- The committed fix (pool bound 4 + non-default `cache_size(-1600)`)
  bounds the SQLite **page caches** to a suggested maximum of roughly
  6.4 MB/journal (`cache_size` is approximate and does not bound all
  SQLite/driver memory). It is a real by-design guard: before it, the
  pool was formally unbounded.
- The near-identical pre/post curves show the unbounded pool was **not
  the dominant term** of the observed growth on this workload. The
  remainder is **unattributed** saturating growth: plausible
  contributors are Go allocator retained spans and page-cache warm-up,
  but no allocator/RSS decomposition, DB-size series, connection-count
  series, or idle/time control was collected, so those names are
  hypotheses, not findings. "Saturating rather than time-linear" rests
  on the two 20k curves' concavity alone; the 24h post-fix run is the
  decision boundary.

## Proof ceiling (stated, not hidden)

- A leak slower than **16 MiB per run window** is undetected by S1 by
  construction (the absolute floor added for small-baseline warm-up);
  the ratchet against it is a longer run.
- Final confirmation that the 24h shape also saturates under the fixed
  build is **pending the next 24h post-fix soak** (owner-scheduled — it
  monopolizes the dev host for a day). Until that run, "bounded, not a
  leak" is the best-supported hypothesis, not a closed verdict.

## Raw curve data

Pre-fix 20k (msgs → rssKB): 0→20720, 500→23744, 5000→24080,
10000→29488, 15000→31232, 20000→31008.
Post-fix 20k: 0→20176, 500→22688, 5000→26000, 10000→25696,
15000→31600, 20000→29712.
Full sample series on the dev host:
`~/HARNESS/nexus-soak/diag-prefix-20k.log` (pre-fix, 42 samples) and
`~/HARNESS/nexus-soak/diag-postfix.log` (post-fix). (`diag-curve-run.log`
is an aborted earlier harness run — a timeout panic with one sample —
and is NOT the pre-fix evidence.)

# REVIEW-PHASE0-R6 — round-6 convergence check (17888b3, very narrow)

Scope: verify the 3 round-5 folds + NEW defects only in lines changed by 17888b3.
`go test -count=1 ./internal/preflight/probe/` — GREEN (33 tests, 20.8s).

---

**1. [OK] hostile-XDG fixture now MkdirTemp-unique.**
`TestHostileXDGRuntimeDirCannotWidenRoots` now creates its probe dir with
`os.MkdirTemp(home, ".nexus-probe-red-*")` + `os.Chmod(sub, 0o700)` instead of
`os.MkdirAll(filepath.Join(home, ".nexus-probe-red"), 0o700)`. The old fixed path could pre-exist
and be recursively deleted by the deferred `os.RemoveAll`; the unique temp name removes that hazard
and the defer now targets only the fixture the test created. ✓

**2. [OK] lib-shadowing RED is causal (dynamic writer + EROFS + remount-loosened control).**
`buildDynamicWriter` compiles a DYNAMIC C fixture (so its closure materializes `/nexus-libs` and
`/lib` inside — a static helper hit ENOENT and proved nothing). `TestRootfsReadOnlyPreventsLibShadowing`
writes to `/nexus-libs/libc.so.6` and `/lib/evil` and asserts BOTH `err != nil` and
`strings.Contains(out, "Read-only file system")` (EROFS, not merely a failed write). The new
`loosen.remountRW` switch (probe.go:107) skips `--remount-ro /`, and
`TestNegativeControlLibWriteSucceedsWithoutRemountRO` asserts the same write SUCCEEDS when loosened —
proving the EROFS is caused by the remount, not vacuous. ✓

**3. [OK] budgets enforced pre-allocation with unit REDs.**
`loadBounded(path, remaining)` (probe.go:156-184) now caps at `min(maxClosureOneFile, remaining)`
and rejects `cap <= 0`, `st.Size() > cap`, and a `LimitReader(cap+1)` over-read — the aggregate
allowance is enforced BEFORE the read/allocation. `checkQueueBudget(cur, add)` (probe.go:115-122)
rejects before any node slice is materialized, called both at the root and before each child
expansion. Unit REDs `TestLoadBoundedHonorsAggregateAllowance` (over/at/exhausted allowance) and
`TestQueueBudgetRejectsBeforeAllocation` (oversized / overflow / exact-capacity). ✓

---

## NEW defects in 17888b3 lines — none material

- The double budget check (pre-construction `checkQueueBudget` + the `push`-time check) is redundant
  but identical and consistent, not contradictory.
- `loadBounded`'s `cap` arithmetic is int64-safe (`remaining ≥ 0` because `addBuf` rejects any
  `total > maxClosureBytes` before `remaining()` can be re-evaluated); `cap+1` cannot overflow.
- The dynamic-writer fixture depends on a glibc-resolvable `libc` (the same assumption the rest of
  the suite already makes via `TestDynamicELFRuns`/`$ORIGIN` fixtures), so it is not a new gap.

No defect introduced by the changed lines; the three folds are correctly implemented and the REDs
are red-capable and causal.

---

## Verdict

All 3 round-5 folds are correctly implemented, test-verified, and introduce no new defect.

VERDICT: PASS

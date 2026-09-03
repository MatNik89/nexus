# PHASE-1B verification round 3 — Codex

Scope: `slice/p0-phase1` at `cf09f36a964db778262160a58db3be51760f5ee9`; fold-only review of `2579b77..cf09f36`, using `docs/REVIEW-PHASE1B-R2-codex.md` as the finding ledger. Kilo/Agy round-2 PASSes were spot context, not substitutes for code inspection.

1. [OK] **R2 #2 is folded to the stated T18 ceiling.** `SyncProjection.Init` now receives `*ProjDB`, whose exported surface is only guarded `Exec`/`Query`; the underlying `*sql.DB` remains private (`internal/kernel/journal/projection.go:52-74,86-94`). `Open` supplies that restricted handle only after lease acquisition and chain verification (`internal/kernel/journal/journal.go:289-305`). The RED exercises both a direct canonical insert and a trigger whose SQL mentions `events` (`internal/kernel/journal/projection_test.go:268-306`). Lexical matching is not a complete SQL capability sandbox, but that limitation is explicitly scoped to T18 storage ownership and is the deliberate ceiling in this review.

2. [OK] **R2 #4 is folded.** `ProjTx.QueryRow` is gone, so there is no API that can turn `NON_CANONICAL_WRITE` into `sql.ErrNoRows`; guarded reads use the error-returning `Query` method (`internal/kernel/journal/projection.go:40-50`).

3. [UNFOLDED] **R2 #6 is fixed only within one input slice; resumed folds can still regress the durable checkpoint.** `Fold` initializes its comparison checkpoint to zero and accepts no prior checkpoint (`internal/kernel/machine/machine.go:80-98`). Concrete failure: state already folded through offset 10 is passed as `initial`, but an accidentally replayed legal next-state event at offset 9 is accepted and returned with checkpoint 9. An empty resumed batch similarly returns checkpoint 0. The comment shifts the invariant to the caller (“only events PAST its prior checkpoint,” lines 78-79), but the R2 finding explicitly required an explicit prior checkpoint and there is no rejection/defer record. Fix: accept `priorCheckpoint` (or expose `FoldFrom`) and compare the first event against it; return it for an empty batch. Add both stale-first-event and empty-resume REDs.

4. [OK] **R2 #7 is folded.** Independent exhaustive matrices now cover every declared event against every state for Turn and Attempt, including the amended multi-from cancellation edges and rejection from terminal/UNKNOWN states (`internal/kernel/machine/machine_test.go:251-315`).

5. [OK] **R2 #9 is folded.** `NEXUS_CFG_*` suffixes must be canonical uppercase and duplicate normalized keys reject before assignment (`internal/foundation/config/config.go:172-205`); both duplicate orders and the lowercase alias are tested (`internal/foundation/config/config_test.go:189-205`).

6. [OK] **R2 #10 is folded for the named env/CLI rejection paths.** Boolean errors omit the raw value and list errors identify only the entry position (`internal/foundation/config/config.go:94-123`). The canary RED covers invalid env bool, CLI bool, and padded env-list input (`internal/foundation/config/config_test.go:207-240`).

7. [OK] **R2 #11 is folded under the deliberately narrowed trust root.** `EnsureDir(trustedRoot, path)` proves lexical containment, then `Lstat`s every component below the root and rejects symlinks, non-directories, foreign ownership, and group/world permissions (`internal/foundation/pathx/pathx.go:77-120`). The parent-symlink and outside-root REDs are causal (`internal/foundation/pathx/pathx_test.go:69-90`). Symlinks at or above `trustedRoot` remain a caller trust decision as stated; this pass does not expand that boundary.

8. [OK] **R2 #12 is folded, and the PGID-reuse premise is sound on Linux.** Group signalling is now suppressed only after the leader PID has been reallocated with a different start time (`internal/foundation/procx/procx_linux.go:105-150`). A process-group reference keeps that numeric PID allocated while any group member exists, so observing reuse proves the old group is gone; a dead/unreaped leader no longer prevents signalling surviving members. The exact prompt-exit/TERM-resistant-child fixture is non-vacuous and passes (`internal/foundation/procx/procx_linux_test.go:145-169`).

9. [OK] **The P0.5 amendment legitimately narrows the P0 attestation fields.** The owner now explicitly limits T11/P0 to `{probe name, passed, detail, config-hash}` under HARDQ B9's startup-only snapshot and defers probe identity/revision, expiry, and measured-vector to the P1 S0.3 negotiation owner (`docs/HARNESS-SPEC.md:1541-1553`). This is a scoped owner amendment, not an undocumented weakening of the original P0.5 MUST list.

10. [UNFOLDED] **R2 #13's remaining P0 requirement—binding to the actual resolved configuration—is still caller assertion, not proof.** The amendment says the value MUST be the SHA-256 digest of RESOLVED configuration and “not an arbitrary string” (`docs/HARNESS-SPEC.md:1546-1550`), but `Seal` receives only a string and checks lowercase-hex shape/equality (`internal/kernel/closure/closure.go:150-177,187-207`). It has neither `config.Resolved` nor canonical resolved-config bytes from which to compute or verify the digest. The shipped positive fixture is literally 64 repeated `1` characters and passes (`internal/kernel/closure/closure_test.go:12-20`), demonstrating that an arbitrary shape-valid token is accepted. Snapshot retention and mismatch-to-OFF are correct but cannot create the missing binding. Fix: make the configuration owner compute a canonical digest through a typed API consumed by `Seal`, or pass canonical resolved bytes/config into `Seal` and compute there; add a RED where a valid-looking invented digest cannot attest a different resolved config.

11. [OK] **R2 #14 is folded.** `Resolve` validates every declared conflict target against the manifest set before closure traversal (`internal/kernel/closure/closure.go:49-59`), and the RED covers the dangling conflict both requested and unrequested (`internal/kernel/closure/closure_test.go:179-190`).

12. [OK] **R2 #15 is folded within the declared producer-authentication ceiling.** `Grade` requires `AcceptanceContract.Worker`; every evidence record requires and must match `Evidence.Contract`; worker-produced evidence rejects unconditionally (`internal/kernel/checker/checker.go:68-95,116-148,167-193`). Missing-worker, cross-contract replay, and unbound-evidence REDs are present (`internal/kernel/checker/checker_test.go:180-195`). Authentication of producer strings remains explicitly deferred to the T19/T21 evidence owners.

13. [OK] **R2 #17 is folded.** Grading first derives the earliest delivery for the same occurrence ID, then rejects any matching acknowledgment earlier than it; a later redelivery does not invalidate an already valid delivery→ack pair (`internal/kernel/checker/checker.go:243-275`; `internal/kernel/checker/checker_test.go:197-222`).

14. [OK] **No additional material defect was found in lines introduced by `cf09f36`.** The changed APIs compile across all current callers, and the restricted-scope regression pass was green. This does not override findings 3 and 10, which are deterministic false-green gaps not exercised by the suite.

## Verification

- `go vet ./internal/...` — PASS (exit 0).
- `go test -count=1 ./internal/...` — PASS (15 tested packages; 2 packages correctly reported no test files).
- `git diff --check 2579b77..cf09f36` — code clean; it reports only two trailing-space lines in the committed prior-review artifact `docs/REVIEW-PHASE1B-R2-agy.md`, outside the implementation folds and not material to this verdict.

Weakest link: no runtime caller yet composes T11's snapshot from a real `config.Resolved`; consequently the configuration-binding claim currently has no end-to-end proof surface.

VERDICT: FAIL

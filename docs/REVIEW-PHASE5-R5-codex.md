# Phase 5 verification round 5 — Codex

Reviewed immutable target: `bded8a2d823c03c0582b4fb167542bcec5b632ce` on `slice/p0-phase5`.

Result: **FAIL**. The retry transaction and legacy-context compatibility fixes are real and causal. Completed-turn recovery now fails closed, but it still discards the journal replay integrity error instead of propagating it, so round-4 finding #3 is only partially fixed.

## Finding

1. **[MED] Completed-turn recovery hides the canonical journal-integrity failure.** `completedTurnFinal` receives the error from `Journal.Replay`, but its `(string, bool)` API converts every replay failure to `("", false)` (`internal/app/daemon/daemon.go:249-268`). `RunChannelTurn` then returns the earlier duplicate-event error from the attempted rerun (`internal/app/daemon/daemon.go:231-240`), not the decisive hash-chain failure. **Concrete scenario:** after a completed channel turn, a tail event is corrupted and Telegram redelivers the completed update. Recovery correctly refuses the cached final, but the caller sees `UNIQUE constraint failed: events.event_id`; the integrity breach is hidden as an ordinary duplicate-turn failure and can be retried or diagnosed incorrectly. **Clean-export probe:** I strengthened `TestRecoveryFailsClosedOnCorruptJournal` to require `journal replay` plus `hash mismatch`. It failed with `replay integrity error was not propagated: loop: journal turn.created: journal append: constraint failed: UNIQUE constraint failed: events.event_id (2067)`. The recovery helper needs an error result, and `RunChannelTurn` must return/wrap the replay error when recovery cannot establish an intact final.

## Round-4 finding verification

1. **Retry recipe atomicity — PASS.** `RetryChallenge` reads the source-bound CONSUMED-and-open intent, creates the replacement, and sends `EvResumeCompleted` plus `EvTurnSuspended` through one `AppendBatch` (`internal/approval/approval.go:350-409`). `AppendBatch` applies both projections inside one SQLite transaction and commits only after both inserts (`internal/kernel/journal/journal.go:509-560`). The Telegram handler now calls this owner directly (`cmd/nexus/main.go:661-673`). In a clean export, `TestRetryRecipeAtomicUnderSigkill` passed. My controlled ablation replaced the batch with `Append(suspend)`, a SIGKILL, then `Append(close)`; the detector turned RED with `retry recipe torn: old reconcile item open AND a new challenge live (two authorizations)`. This establishes the claimed crash boundary and recovery behavior for the committed path.

2. **Legacy context degradation — PASS within the accepted parser seam.** `parseContextJSON` returns nil blocks without error only for the legacy empty string, while malformed JSON returns a fail-closed error; both `SuspendedCall` and `ConsumedCall` use it (`internal/approval/approval.go:587-627`, `internal/approval/approval.go:661-686`). `RetryChallenge` converts legacy nil blocks to `[]` for the v2 payload validator (`internal/approval/approval.go:370-379`). In a clean export, `TestLegacyContextDegradesNotStrands` passed. Making empty context return an error turned it RED at `legacy empty context must degrade`; accepting malformed JSON turned it RED at `corrupt context accepted`. No full two-binary harness was required under the stated ceiling.

3. **Corrupt-journal recovery — PARTIAL / FAIL.** The committed detector passes, and disabling the `err != nil` check turns it RED with `recovered a final from a journal that fails integrity verification`. That causally proves the false-success path is closed (`internal/app/daemon/daemon_test.go:455-510`). It does not prove the fold's separate claim that the integrity error is propagated; finding 1 demonstrates that it is not.

## New-defect hunt

No additional confirmed defect was found in `6b564bc..bded8a2`. I checked retry concurrency/partial-failure ordering, random-ID collision rollback, source/profile binding, legacy retry payload validation, redactor refusal before mutation, and the complete replay walk. The duplicated challenge construction in `RetryChallenge` is a maintenance risk, but I found no behavioral failure from it and do not count taste as a finding.

## Verification evidence

Repository command:

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed
```

Clean `git archive HEAD` exports:

```text
TestRetryRecipeAtomicUnderSigkill: PASS
TestLegacyContextDegradesNotStrands: PASS
TestRecoveryFailsClosedOnCorruptJournal: PASS

split retry into two appends with SIGKILL between them: RED — old open + new live
reject legacy empty context: RED — legacy request stranded
accept malformed context: RED — corrupt context accepted
ignore Replay error: RED — corrupt cached final recovered
require Replay integrity error propagation: RED — duplicate event-id error returned instead
```

The required suite ran in the repository because `internal/buildcheck` requires `.git`; all probes and mutations ran only in clean archive exports under `/tmp`. The repository worktree was not modified except for this requested report; concurrent untracked Kilo/Agy round-5 reports were preserved.

Weakest link: the SIGKILL detector establishes transactional rollback at its explicit pre-commit seam; it does not exhaust every storage/filesystem failure mode. That ceiling does not affect the confirmed error-propagation failure.

Skill update: none — the existing review workflow already requires causal error preservation and a negative probe that distinguishes fail-closed behavior from correct error propagation.

VERDICT: FAIL

# Phase 5 verification round 4 — Codex

Reviewed immutable target: `6b564bc276eceb9661b0ea7357ad81e4404dc113` on `slice/p0-phase5`.

Result: **FAIL**. All three round-3 fixes work for fresh, intact state and their committed detectors are causal. The new retry reconciliation path is not crash-atomic and can mint two independently consumable approvals for one UNKNOWN effect; projection v2 strands pre-fold suspensions; and completed-turn recovery accepts a partial replay after journal-integrity failure.

## Findings

1. **[HIGH] A crash during `retry` can mint two live, consumable approvals for the same UNKNOWN effect.** The handler first commits the fresh challenge through `Suspend`, then separately closes the old consumed challenge through `MarkResumeCompleted` (`cmd/nexus/main.go:661-677`; the two append owners are `internal/approval/approval.go:294-347` and `internal/approval/approval.go:552-560`). A crash after the first append leaves the old challenge open, so redelivery issues another fresh challenge. Both can be approved, and `ConsumeApproval` selects and burns approvals only by effect hash (`internal/approval/approval.go:419-453`), allowing both authorizations to be consumed. **Concrete failure:** the original UNKNOWN effect may already have committed; the crash-window creates two more approval paths, permitting up to two additional executions. **Clean-export probe:** `TestR4RetryCrashCannotMintTwoLiveChallenges` failed with `two fresh approvals for one UNKNOWN effect were both consumable`. This disproves the claimed durable once-only retry. The owner needs one atomic append recipe that creates the replacement challenge and closes the old reconcile item, plus a detector that crashes between the two state changes.

2. **[MED] Projection v2 cannot rehydrate suspensions created by the immediately preceding schema.** The projection version changes to 2 and adds mandatory `context_json` (`internal/approval/approval.go:149-168`). A version mismatch drops and rebuilds the projection from all canonical events (`internal/kernel/journal/journal.go:339-373`), but v1 `approval.turn_suspended` events have no `context` field. They rebuild with an empty context (`internal/approval/approval.go:181-191`), then `SuspendedCall` fails unmarshalling it (`internal/approval/approval.go:525-549`). **Concrete failure:** upgrade a journal containing a live v1 pending approval, approve it after restart, and resume fails with `unexpected end of JSON input`; the durable HITL request is stranded. **Clean cross-version probe:** `594c457` created the suspended approval; `6b564bc` reopened the same SQLite journal and `TestR4UpgradeKeepsV1SuspendedApprovalUsable` failed exactly that way. A compatible rebuild must explicitly translate legacy events or surface them as typed unrecoverable reconciliation items rather than leaving apparently approvable rows that cannot resume.

3. **[MED] Completed-turn recovery ignores journal replay failure and can return a final from a corrupt canonical stream.** `completedTurnFinal` records a matching final during replay but discards `Journal.Replay`'s error and returns the partial result (`internal/app/daemon/daemon.go:245-262`). Replay can detect a later hash/chain failure after the callback has already observed that final (`internal/kernel/journal/journal.go:613-660`). **Concrete failure:** a completed turn is followed by another event whose integrity hash is corrupt; redelivery of the earlier update returns the cached final as if recovery succeeded despite the canonical journal failing integrity verification. **Clean-export probe:** `TestR4CompletedFinalRecoveryFailsClosedOnBrokenJournal` corrupted only the last temporary event and failed with `corrupt replay was accepted as a recovered final: "ok"`. Recovery must propagate replay errors and fail closed.

## Round-3 finding verification

1. **Original-context rehydration — PASS for v2-created suspensions.** `suspendedPayload.Context` is persisted and projected (`internal/approval/approval.go:67-83`, `186-190`), `SuspendedCall` restores it, and `resumeApproved` appends the tool observation before resuming (`cmd/nexus/main.go:235-266`). `TestResumeRehydratesOriginalContext` passed in a clean export; replacing the original blocks with only the observation made it RED because the provider request lacked `REHYDRATE-MARKER`. Finding 2 covers upgrade compatibility, not the fresh-row path.
2. **Repeated suspend/resume — PASS.** `ResumeTurn` requires the challenge id as a cycle tag and namespaces every event id with it (`internal/kernel/loop/loop.go:103-125`, `217-234`). `TestSecondSuspensionResumes` passed through two ASK cycles to `both done`; replacing the tag with one fixed value made it RED on the second cycle.
3. **Completed-turn crash recovery — PASS on an intact journal.** `turn.succeeded` now carries the redacted final (`internal/kernel/loop/loop.go:257-270`), and `RunChannelTurn` recovers it when deterministic event ids reject re-execution (`internal/app/daemon/daemon.go:231-242`). The upgraded test passed and removing the fallback made it RED with the duplicate event-id error. Finding 3 is the distinct corrupt-replay branch.
4. **Consumed-but-unfinished reconciliation — PARTIAL/FAIL.** Startup scanning, foreign-source refusal, fresh random challenge issuance, and sequential once-only retry all pass. Removing the consumed-open startup scan made `TestConsumedUnfinishedReconciles` RED. Finding 1 proves the claimed once-only property does not survive the crash boundary between replacement issuance and closure.

## Verification evidence

Repository commands at the reviewed HEAD:

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed

CGO_ENABLED=0 go vet ./...
exit 0
```

Clean `git archive` evidence:

```text
TestResumeRehydratesOriginalContext: PASS
TestSecondSuspensionResumes: PASS
TestRedeliveredUpdateCannotRerunCompletedTurn: PASS
TestConsumedUnfinishedReconciles: PASS

ablate original-context append: TestResumeRehydratesOriginalContext RED
ignore unique resume tag: TestSecondSuspensionResumes RED
remove completed-final fallback: TestRedeliveredUpdateCannotRerunCompletedTurn RED
skip consumed-open scan: TestConsumedUnfinishedReconciles RED

TestR4RetryCrashCannotMintTwoLiveChallenges: FAIL — both replacements consumable
TestR4UpgradeKeepsV1SuspendedApprovalUsable: FAIL — unexpected end of JSON input
TestR4CompletedFinalRecoveryFailsClosedOnBrokenJournal: FAIL — corrupt partial replay accepted
```

The required full suite ran in the repository because `internal/buildcheck` requires `.git`; all custom probes and mutations ran only in clean archive exports. No live Telegram bot or external canary was used. Unrelated Agy/Kilo round-4 review files were left untouched.

Skill update: none — the existing audit rules already require crash-boundary, migration, causal-ablation, and fail-closed integrity checks; these are product defects rather than a repeatable methodology miss.

VERDICT: FAIL

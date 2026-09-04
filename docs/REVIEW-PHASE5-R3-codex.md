# Phase 5 verification round 3 — Codex

Reviewed immutable target: `594c4573a6551a7979a7e56f71c08a44c497ec7d` on `slice/p0-phase5`.

Result: **FAIL**. The fold makes the advertised single-approval paths and all repaired round-2 detectors real and causal. It does not actually rehydrate the original turn context, a turn cannot survive a second suspend/resume cycle, and the completed-turn crash seam loses the successful reply.

## Findings

1. **[HIGH] Resume does not rehydrate the original turn context.** The durable suspension stores the turn/run ids and call, but no original context (`internal/approval/approval.go:62-74`), and `SuspendedCall` returns only those ids plus the call (`internal/approval/approval.go:484-504`). After execution, `resumeApproved` creates one synthetic tool-result block and passes only that block to `ResumeChannelTurn` (`cmd/nexus/main.go:254-260`); `Loop.ResumeTurn` begins planning from exactly the supplied blocks (`internal/kernel/loop/loop.go:205-215`). **Concrete failure:** a request whose final answer depends on the original user text resumes with only “tool executed with result”; the planner cannot recover the request or earlier context. **Clean-export probe:** `TestR3ResumeRestoresOriginalContext` failed with `planner saw only [{approved-result ...}]`. This disproves fold claim 1 even though the original turn id is reused.

2. **[HIGH] A second approval in the same turn executes its effect but cannot resume the turn.** Every `ResumeTurn` resets `seq := 1000` (`internal/kernel/loop/loop.go:205-212`), while the event id is derived from `(turn,eventType,seq)` (`internal/kernel/loop/loop.go:94-103`). Thus every resume of one turn mints the same `turn.resumed` event id. In the production path the approved effect runs first (`cmd/nexus/main.go:240-249`), then the duplicate resume append fails; that continuation error is swallowed and the tool result is returned as success (`cmd/nexus/main.go:260-265`). **Concrete failure:** planner asks for action A, resumes, then asks separately for action B; approving B consumes its approval and executes B, but the turn remains `SUSPENDED` and never produces its final. **Clean-export probe:** `TestR3SecondSuspensionCanResume` failed with `UNIQUE constraint failed: events.event_id` on the second `turn.resumed`.

3. **[MED] Crash after a completed turn but before inbox completion loses the successful outcome.** Deterministic channel turn ids correctly make a completed turn reject re-execution (`internal/app/daemon/daemon.go:220-231`), but the reply is not committed until the adapter later calls `CompleteInbound` (`internal/channel/telegram/telegram.go:254-267`). On redelivery, the duplicate-turn error is treated as an ordinary handler failure and the inbox is closed with a generic failure reply (`internal/channel/telegram/telegram.go:254-260`). **Concrete failure:** `RunChannelTurn` returns the real final, the daemon dies before `CompleteInbound`, and replay tells the user “I could not process that message” instead of returning the existing successful outcome. **Clean-export probe:** `TestR3CompletedTurnReplayReturnsOriginalOutcome` failed: sent generic failure, wanted `original successful answer`. This seam is after `turn.succeeded`, so it is distinct from the accepted ceiling for a crash strictly between tool commit and `turn.succeeded`.

## Round-2 finding verification

1. **Inbox replay — PARTIAL/FAIL.** Deterministic turn/run ids prevent a completed turn from executing twice, and replacing them with fresh ids made `TestRedeliveredUpdateCannotRerunCompletedTurn` RED. Finding 3 proves the durable existing outcome is still not recoverable.
2. **Durable HITL resume — PARTIAL/FAIL.** Same-source replay after `ApprovalReceived` and startup scanning both complete one approved call. Removing the `ResumeChannelTurn` call made `TestSuspendedTurnHonestState` RED. Findings 1 and 2 prove that context rehydration and repeated suspension are incomplete.
3. **Approval versus transport deadline — PASS.** The call deadline is refreshed before issuing the resume grant (`cmd/nexus/main.go:240-246`). Removing that refresh made `TestDelayedApprovalResumes` fail with `CALL_DEADLINE_EXCEEDED`.
4. **Suspended turn honesty — PASS for one suspension.** The machine has `SUSPENDED -> RUNNING` (`internal/kernel/machine/machine.go:153-177`), and the loop records `turn.suspended` rather than success. Restoring the old success append made `TestSuspendedTurnHonestState` RED. Finding 2 covers the distinct repeated-resume defect.
5. **Expiry race — PASS.** Both decision and consumption projection updates require `expires_unix > event.EmittedAt` in the append transaction (`internal/approval/approval.go:172-218`). Removing the decision predicate made `TestProjectionRefusesExpiredDecision` RED; independent `TestR3ProjectionRefusesExpiredConsumption` passed at the exact expiry boundary.
6. **Poison-head starvation — PASS.** `Flush` accumulates definite failures and continues (`internal/channel/channel.go:447-484`). Restoring the early return made `TestPoisonHeadDoesNotStarve` RED with `good=0`.
7. **Sealed channel probe — PASS under the accepted provider/store ceiling.** `telegramProbeGate` seals and checks the channel result (`cmd/nexus/main.go:574-607`). Omitting the probe from `Seal` made the healthy `TestTelegramProbeGate` RED because the channel probe was missing.
8. **Telegram error classification/sanitization — PASS.** Dial/DNS failures are definite pre-wire failures and request-construction errors are sanitized (`internal/channel/telegram/telegram.go:108-152`). Neutralizing both guards made `TestPreWireFailureRepends` and `TestRequestConstructionSanitized` RED, respectively.
9. **Detector causality — PASS.** Removing both independent admission backstops made `TestConcurrentAdmissionRace` RED with divergent message ids. Restoring deterministic effect-derived challenge ids made `TestChallengeIDsUnpredictable` RED on the challenge primary key. Restoring REPL provenance made `TestChannelTurnCarriesChannelProvenance` RED. Each mutation ran in its own clean export.

## Verification evidence

Repository commands at the reviewed HEAD:

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed

CGO_ENABLED=0 go vet ./...
exit 0
```

Clean `git archive HEAD` evidence:

```text
all 13 named fold tests: PASS
TestR3ProjectionRefusesExpiredConsumption: PASS
TestR3ResumeRestoresOriginalContext: FAIL (original context absent)
TestR3SecondSuspensionCanResume: FAIL (duplicate turn.resumed event id)
TestR3CompletedTurnReplayReturnsOriginalOutcome: FAIL (generic failure replaced final)

causal ablations: deterministic ids, continuation call, deadline refresh,
turn.suspended, projection expiry, outbox continue, sealed channel probe,
pre-wire classification, request sanitization, both admission backstops,
random challenge id, and channel provenance each turned its named detector RED
```

The full suite cannot be executed literally from a bare `git archive` because `internal/buildcheck` deliberately invokes Git to locate the repository root; its three tests fail with Git exit 128 when `.git` is absent. The required full suite was therefore run at the immutable repository HEAD, while every probe and mutation ran in clean archive exports. No live Telegram bot or paid/external canary was used.

Skill update: none — the existing nexus-audit rules already required clean immutable probes, causal ablation, partial-failure analysis, and ordered-queue poison-head coverage; this review exposed product-state defects rather than a repeatable methodology miss.

VERDICT: FAIL

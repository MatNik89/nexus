# Phase 5 verification round 3 — Kilo (defensive fold review)

Target: `slice/p0-phase5` at HEAD `594c457` (fold of round 2). Method: code-read each fold point, run the suite in the working tree, re-run my round-2 negative probes against a clean `git archive HEAD` export (`/tmp/kilo/r3-export`). Working tree never modified. Suite: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 32 packages `ok`.

## Round-2 finding status (verified)

| # | Round-2 finding | Status at HEAD | Evidence |
|---|---|---|---|
| 1 | [HIGH] Resume re-executes frozen 2-min deadline | **FIXED** | `call.Deadline = time.Now().Add(2*time.Minute)` before execution (`main.go:240`); deadline is not in `EffectHash` so the refresh cannot alter intent. Re-ran my probe (`zz_probe_resume_test.go`): suspend(1s deadline) → sleep 1.5s → approve → `resumeApproved` → `RESUME OK: "saved it"` (round-2 observed `CALL_DEADLINE_EXCEEDED`). |
| 2 | [MED] Connection-refused parked UNKNOWN | **FIXED** | `isPreWire` (`telegram.go:108-122`) classifies dial-phase `*net.OpError`/`*net.DNSError` as definite pre-wire; `call` re-pends them (`telegram.go:143-149`). Re-ran my probe: connection-refused → `pending=1 unknown=0` (round-2 observed `unknown=1`). |
| 3 | [MED] Redelivery re-runs non-idempotent turn | **FIXED** | Deterministic per-message turn ids `turn-chan-<identity>-<updateID>` (`daemon.go:220-224`); a redelivered update re-enters the same turn and the machine TurnTable/event-id uniqueness refuse a second run. `TestRedeliveredUpdateCannotRerunCompletedTurn` is causal (redelivery of update 7 → error, `runs==1`). |
| 4 | [LOW] Duplicate `SetDurableApprovals` | **FIXED** | Removed (`main.go:401` single call). |
| 5 | [LOW] Projection approve/deny no expiry | **FIXED** | `EvApprovalReceived`/`EvApprovalDenied`/`EvApprovalConsumed` now carry `AND expires_unix > ?` against `ev.Envelope.EmittedAt.Unix()` (event-owned time) inside the append transaction (`approval.go:177-213`); `TestProjectionRefusesExpiredDecision` covers it. |
| 6 | [LOW] `approve `/`deny ` prefix routing UX | Accepted ceiling (no security impact; `expected_source` already blocks foreign approval). |

All six round-2 findings are genuinely fixed. The headline resume is now a real turn rehydration: `turn.suspended`/`turn.resumed` state transitions (`machine.go:157-176`), `loop.ResumeTurn` re-enters the ORIGINAL turn with the tool observation (`loop.go:205-216`), and `TestSuspendedTurnHonestState` proves a suspended turn is never falsely `turn.succeeded`.

## NEW defect introduced by the fold

### 1. [MED] The resume path burns the single-use approval BEFORE the tool's outcome is known, with no S7 attempt record and no retry path

`resumeApproved` (`main.go:235-267`) executes the approved tool via the **system** effect path:

```go
out, err := b.sysPath.RunTool(ctx, call, grant)   // main.go:246
```

Inside `RunTool`, the `DecisionAsk` branch calls `durable.ConsumeApproval(call)` (`effectpath.go:375-383`), which commits `EvApprovalConsumed` (status APPROVED→CONSUMED) **before** `BeforeTool`, `grants.Consume`, the (absent) `onStarted` record, and the actual executor dispatch (`effectpath.go:390-410`). `sysPath` was built with no `StartObserver`, so unlike the normal loop path there is no durable `EvAttemptStarted` between consumption and execution.

Consequence — two concrete failure modes, neither recoverable:

- **Tool fails after consumption**: if the executor returns an error (e.g. the memory store's journal append fails), `resumeApproved` returns the error at `main.go:247-249`, but the approval is already CONSUMED. The turn stays `SUSPENDED`, and:
  - `resumeApprovedPending` scans only `status='APPROVED'` (`approval.go:466-468`), so a CONSUMED challenge is invisible to the startup scan;
  - a re-`approve` fails: `SuspendedCall` requires `status='APPROVED'` (`approval.go:488-489`), so the crash-replay path in `telegramHandler` (`main.go:624-632`) also cannot resume it.
  → the approved action is permanently lost (0 executions) with no way to retry.

- **Crash between `EvApprovalConsumed` and the executor dispatch** (same window, process death instead of error) → identical outcome: approval consumed, effect never ran, turn stranded SUSPENDED, no recovery.

This is *at-most-once* semantics for the effect with a silently burned approval — the opposite of the accepted "at-least-once tool semantics" ceiling (which only covers a crash between a tool commit and `turn.succeeded`, `main.go:262-264`). The root cause is that consumption precedes execution with no durable attempt/STARTED state to make the window retryable (the normal loop path has this via `SetStartObserver`/`EvAttemptStarted`, `loop.go:280-282`). Suggested fix: defer `EvApprovalConsumed` until after the executor returns success (or add a durable `resume.started`/attempt record the startup scan can reconcile).

Evidence is code-level: no failure-injection seam exists in `resumeApproved`, so this is not probe-backed (unlike round-2's two findings). The non-atomic ordering (`main.go:246` consume-before-execute, then `main.go:260` turn-resume) and the `status='APPROVED'`-only recovery predicates (`approval.go:466-468`, `:488-489`) are the proof.

## What holds

The `isPreWire` classification is correct (dial/DNS = nothing left the process; post-send reset/EOF/timeout stays ambiguous). Projection expiry now uses event-owned `EmittedAt` (no TOCTOU). The startup scan (`resumeApprovedPending`, `main.go:272`) correctly resumes APPROVED-unconsumed challenges and enqueues the reply. `TestConcurrentAdmissionRace` deterministically parks both racers via `testPostCheckHook` and the message-PK + update-UNIQUE backstops are each sufficient (defense in depth, correctly not a causality failure). Challenge ids are random 96-bit (`approval.go:266-270`) and `TestChallengeIDsUnpredictable` covers it; channel provenance is proven at the daemon level (`TestChannelTurnCarriesChannelProvenance`).

## Verdict

All six round-2 findings are genuinely fixed (two re-verified by my own probes, the rest by code + causal REDs). But the fold introduced one confirmed durability defect: the resume consumes the single-use approval before the tool's success is known, with no attempt record and no retry path, so a tool failure or a crash in that window permanently loses the approved action. That is a broken behavior, not taste.

VERDICT: FAIL

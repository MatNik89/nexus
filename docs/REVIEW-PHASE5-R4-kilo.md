# Phase 5 verification round 4 — Kilo (defensive fold review)

Target: `slice/p0-phase5` at HEAD `6b564bc` (fold of round 3). Method: code-read each fold point, run the suite in the repo (`internal/buildcheck` needs `.git`), verify the causal REDs, re-run probes in a clean `git archive HEAD` export (`/tmp/kilo/r4-export`). Working tree never modified. Suite: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 32 packages `ok`.

## Round-3 finding status

| # | Round-3 finding | Status at HEAD |
|---|---|---|
| 1 | [MED] Resume burns single-use approval before the tool's outcome is known → lost action, no retry | **FIXED** — consumed-but-unfinished is now E9 UNKNOWN, surfaced and reconcilable, never a blind retry: `EvResumeCompleted`/`resume_done` (`approval.go:44-49,237-250`), `ConsumedUnfinished` (`:558-577`), `ConsumedCall` (`:581-610`), the `retry <id>` command (`main.go:661-676`) issues a FRESH source-bound challenge, and `resumeApproved` returns an explicit "did not complete … Reply retry" error instead of a silent drop (`main.go:246-253`). |

Verified by `TestConsumedUnfinishedReconciles` (`main_test.go:955-1015`): the startup scan surfaces the CONSUMED-and-open challenge as a `retry` notice without auto-executing anything; a foreign chat's `retry` is refused; the owner's `retry` issues a fresh challenge (new id) and the old one is closed (once-only). This is exactly the reconciliation my finding demanded.

## Fold claims (codex #1-#3) verified

- **codex #1 — original-context rehydration**: `suspendedPayload.Context` (`approval.go:79-81`, projection v2 `context_json`), `Suspend` requires and persists the live block slice (`approval.go:313-323`), `SuspendedCall` returns it (`:527-550`), `resumeApproved` re-enters the original turn with `append(blocks, obs)` (`main.go:263`). `TestResumeRehydratesOriginalContext` asserts the continuation provider request contains the original user text (`REHYDRATE-MARKER`) — causal (the loop `Suspender` now receives `blocks` and the loop passes the live `blocks` slice, `loop.go:347`).
- **codex #2 — repeated suspend/resume**: `ResumeTurn` takes a per-cycle `tag` (the challenge id) namespacing event ids via `l.resumeTag` (`loop.go:220-237`), so a turn suspending twice uses two distinct challenge ids and never collides. `TestSecondSuspensionResumes` (two sequential ASK tools, both approve+resume to `"both done"`) passes.
- **codex #3 — completed-turn crash recovery**: `turn.succeeded` now carries the REDACTED final (`loop.go:256-263`), and `RunChannelTurn` recovers it via `completedTurnFinal` on redelivery collision (`daemon.go:231-243`). `TestRedeliveredUpdateCannotRerunCompletedTurn` was upgraded to recovery semantics and passes (redelivered update returns the durable final, not a false failure).

## NEW finding (LOW, non-blocking)

### 1. [LOW] Suspended-turn redelivery is not recovered — only `turn.succeeded` is

`completedTurnFinal` (`daemon.go:249-259`) matches **only** `turn.succeeded`. The crash window *between a turn suspending and the channel terminal committing* is the SUSPENDED analog of codex #3, and it is not handled: `RunChannelTurn` on redelivery of a suspended turn collides on `EvTurnStarted`, `completedTurnFinal` finds no `turn.succeeded`, and the error propagates to `processUpdate`, which sends a generic "could not process" reply.

Concrete scenario (crash between `l.suspender` returning the summary and `CompleteInbound` in `telegram.go`): the challenge is durably PENDING and the turn is durably SUSPENDED, but the message is non-terminal; on redelivery the owner gets "could not process" instead of the challenge summary. It is fail-closed (the action stays correctly blocked), fully recoverable via the `pending` command (which lists the PENDING challenge), and confined to a narrow crash window — hence LOW, not a blocker. Evidence is code-level (`daemon.go:252` filters `"turn.succeeded"` only; no `turn.suspended` branch exists).

## What holds

The E9 UNKNOWN reconcile is genuinely causal and once-only: `ConsumeApproval` leaves `resume_done=0`, `MarkResumeCompleted` sets it (`approval.go:243-250`), the scan and `ConsumedCall` both filter `resume_done=0`, and a second `retry` finds nothing. The `retry` re-issue reuses the same `turn`/`run` and the original context, and the fresh challenge id (random 96-bit) namespaces the new resume cycle. No blind auto-retry anywhere: `resumeApprovedPending` only *notifies* the user for CONSUMED-and-open challenges and only *resumes* APPROVED-unconsumed ones (`main.go:272-317`). Deadline refresh (`main.go:240`) and the non-idempotent-but-bounded at-least-once ceiling remain correct.

## Verdict

My round-3 finding is genuinely fixed (E9 UNKNOWN reconciliation with a source-bound, once-only retry path), and the three codex fold claims are each backed by causal REDs. The one new observation is a LOW, fail-closed, recoverable recovery gap for the suspended-turn redelivery window — not a confirmed broken finding.

VERDICT: PASS

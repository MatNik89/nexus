# Phase 5 verification round 5 — Kilo (defensive fold review)

Target: `slice/p0-phase5` at HEAD `bded8a2` (fold of round 4, codex #1-#3). Kilo/agy PASSED round 4, so this review verifies the round-4 fold introduced no NEW defect in the kilo dimensions (HITL resume durability, delivery honesty, approval exact-intent, B3 profile isolation, F2 yolo) and confirms the three codex fold claims. Method: code-read, run the suite in the repo (`internal/buildcheck` needs `.git`), read the causal REDs, work in a clean `git archive HEAD` export (`/tmp/kilo/r5-export`). Working tree never modified. Suite: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 32 packages `ok`.

## Fold claims (codex #1-#3) verified

1. **codex #1 — `RetryChallenge` is one atomic batch.** `Store.RetryChallenge` (`approval.go:350-409`) reads the CONSUMED-and-open item via `ConsumedCall`, mints a fresh random challenge, and commits `EvResumeCompleted`(old) + `EvTurnSuspended`(new) in a single `AppendBatch` (`:405-407`). The handler uses it (`main.go:663-672`). Causal: `TestRetryRecipeAtomicUnderSigkill` kills a child inside the batch (via `NEXUS_TEST_KILL_MID_BATCH`) and asserts the reopen state is never "old open AND new live" (two authorizations) nor "both lost"; a clean recovery yields exactly one live challenge and the closed item is unretryable. This closes the round-3 non-atomic window I could reason about (a crash between `Suspend` and `MarkResumeCompleted` previously left both a retryable old item and a pending new one).

2. **codex #2 — `parseContextJSON` legacy degradation.** `parseContextJSON` (`approval.go:612-625`) returns nil blocks for an empty `context_json` (pre-v2 rows), errors on corrupt context, and is used by both `SuspendedCall` and `ConsumedCall`. `TestLegacyContextDegradesNotStrands` asserts empty→nil, `"{corrupt"`→error, `"[]"`→empty slice. No strand: a legacy suspension resumes observation-only (`append(nil, obs) == [obs]`).

3. **codex #3 — `completedTurnFinal` propagates replay integrity errors.** `Replay` verifies the hash chain (`journal.go:634-640`) and returns on break; `completedTurnFinal` now checks that error and returns `"", false` (`daemon.go:252-263`). `TestRecoveryFailsClosedOnCorruptJournal` completes a turn, appends a tail event, corrupts its `integrity_hash` in SQLite, and asserts redelivery refuses rather than serving the cached final. Verified causal: with the check removed the cached final would have been served.

## NEW-defect hunt (kilo dimensions)

No new defect found. Specifically:

- **Exact-intent / approval:** `RetryChallenge` reuses the stored canonical call (validated at the original `Suspend`), re-hashes it with the unified `effectHash`, source-binds via `ConsumedCall`'s `expected_source=?`, and runs the same `RedactorRewrites` fail-closed secret check as `Suspend` (`approval.go:391-397`). The fresh challenge is a new random 96-bit id (`:365-368`), so `retry` cannot reuse a consumed authorization. No cross-profile path: `ConsumedCall` reads from the same profile journal, and the stored call's `ProfileID` was bound to that journal at suspension.
- **Durability:** the atomic `AppendBatch([close, suspend])` removes the two-authorization crash window; once-only is enforced by `EvResumeCompleted`'s `WHERE status='CONSUMED' AND resume_done=0` (`approval.go:243-250`), so a second `retry` of the same id fails closed (`TestRetryRecipeAtomicUnderSigkill:435-437`).
- **F2/B3/classification:** unchanged by this fold (no `mode`, binding, or transport-classification edits).

## Honest observations (not defects)

- `RetryChallenge` duplicates `Suspend`'s payload-construction block (id/hash/callJSON/contextJSON/summary/redactor) instead of factoring it out — a maintenance divergence risk (a future change to `Suspend`'s summary or field set could silently diverge), but functionally equivalent today.
- The E9 `retry` path still re-executes a tool that may already have run (at-least-once, user-confirmed) — the accepted ceiling, unchanged.

## Verdict

The three codex fold claims are real and causal (each backed by a RED-capable test that fails on ablation of the relevant guard), and the round-4 fold introduces no NEW defect in the kilo dimensions.

VERDICT: PASS

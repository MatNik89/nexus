# Adversarial design review round 6 — audit-remediation plan v6
Reviewed revision: `64084cde865d5400de554f3cc246f613cd02806c`
Verdict: FAIL

The review is bound to the committed tree above. `HEAD` matched the requested revision,
the branch was `slice/p1-audit`, the worktree was clean at binding, and the committed plan
header was `PLAN v6`. Slices A-F remain designs; this is static owner-boundary analysis,
not implementation or RED-to-GREEN proof.

## Round-5 disposition

1. **CLOSED — former finding 1 (exhaustion/deadline terminalization was unpaired).** V6
   changes the owner API to `Next(op, build)` and requires a durable cap/deadline exhaustion
   to commit S7 `FAILED` and the owner's `FAILED` companion in one batch
   (`docs/PLAN-AUDIT-FIXES.md:147-154`). Detector 5 now covers cap and deadline exhaustion,
   injects the former inter-append failure, and requires the split-append ablation to expose
   `S7=FAILED/outbox=PENDING` (`docs/PLAN-AUDIT-FIXES.md:347-350`). This uses the existing
   all-or-none serialized journal boundary (`internal/kernel/journal/journal.go:601-619`) and
   closes the current separate-mark seam at `internal/channel/channel.go:533-540`.

2. **CLOSED AS TO ATTEMPT ACCOUNTING — former finding 2 (`MaxAttempts: 2` counted two
   re-asks).** V6 replaces the already-produced-raw design with `ExtractVia`: attempt 1 is
   initial generation and validation, attempt 2 is the one re-ask, and both are one S7
   operation (`docs/PLAN-AUDIT-FIXES.md:314-329`). Detector 9f now asserts two calls for
   invalid-then-valid and invalid-then-invalid, one call under the second-grant ablation, and
   one call for strict classes (`docs/PLAN-AUDIT-FIXES.md:378-382`). That matches the current
   exactly-one-re-ask contract test (`internal/llm/provider/provider_test.go:232-249`). The
   location of salvage relative to terminal reporting creates a different lifecycle defect;
   see new finding 1.

3. **CLOSED AS TO POLL CODE/OUTCOME REACHABILITY — former finding 3.** V6 defines one
   closed Telegram failure vocabulary, gives `PolicyPoll` the exact pre-wire, 429, 5xx,
   post-write, and malformed-reply codes, and makes classification kind/effect-aware so
   read-only polling proposes retryable outcomes while effectful delivery remains UNKNOWN
   (`docs/PLAN-AUDIT-FIXES.md:264-285`). Detector D6 is now a table over pre-wire, 429, 5xx,
   and post-write reset and observes due-time wire calls (`docs/PLAN-AUDIT-FIXES.md:486-490`).
   This closes the mismatch against the current generic Telegram boundary, which classifies
   all post-write/5xx failures as ambiguous (`internal/channel/telegram/telegram.go:193-232`).
   The same new paragraph incorrectly extends the read-only exception to an effectful
   control call; see new finding 2.

4. **STILL OPEN AS A PROOF CEILING — no implementation/runtime evidence.** The plan still
   marks F1/F2/F3/F5/F6/F7/F8 incomplete (`docs/PLAN-AUDIT-FIXES.md:14-20`). No production
   implementation, live provider/Telegram call, signing, publication, install, restart, or
   paid canary was performed. This is not independently a verdict reason.

## Numbered new findings

1. **HIGH — Salvage runs after S7 has terminalized the structured operation, so an accepted
   value can escape while its authoritative operation remains `FAILED`.**

   Evidence: V6 says each attempt performs strict validation; after the second invalid
   result, `Execute` has no further grant and terminalizes the operation. It then runs the
   salvage ladder only *after the operation is terminal*
   (`docs/PLAN-AUDIT-FIXES.md:314-327`). A prose-wrapped but valid object is intentionally
   recoverable by salvage: the current regression supplies invalid strict JSON containing a
   valid `{"steps":4}` object and expects that value to be returned
   (`internal/llm/provider/provider_test.go:288-300`). Under V6, two strict failures can
   therefore land S7 `FAILED`, after which salvage can validate and return a successful value
   to the planner. The state machine makes terminal outcomes terminal
   (`docs/HARNESS-SPEC.md:1391-1392`), so the accepted result cannot subsequently repair that
   operation to `SUCCEEDED`. This contradicts V6's one-operation design and the extractor's
   stated invariant that success is reported only for the value actually accepted
   (`internal/llm/provider/structured.go:121-130,153-171`). Detector 9f checks call counts and
   salvage inputs but never asserts S7 state beside the returned result
   (`docs/PLAN-AUDIT-FIXES.md:378-382`), so it can remain GREEN with this split-brain outcome.

   Concrete fix: keep salvage outside the *physical attempt count* but inside the final
   attempt's outcome decision. After attempt 2 cannot strict-validate (or its transport
   fails), run the ordered salvage ladder over re-ask then original bytes before returning
   the callback's final S7 outcome: accepted salvage -> `OutcomeSucceeded`; no accepted value
   -> `OutcomeFailedTerminal`. Do not return a value after `Execute` has committed a failed
   terminal state. Extend 9f with state/result pairs: every returned value requires S7
   `SUCCEEDED`, and every exhausted invalid case requires S7 `FAILED` and no value.

2. **HIGH — V6 blindly retries ambiguous `setMyCommands` effects despite E9 requiring
   effectful calls without a receipt to become UNKNOWN.**

   Evidence: V6 groups `setMyCommands` with read-only/idempotent kinds and converts its 5xx,
   post-write, and malformed-reply outcomes into retryable `DefiniteFailure` proposals
   (`docs/PLAN-AUDIT-FIXES.md:276-285`). The same plan explicitly calls command registration
   an “effectful POST” and gives it a three-attempt policy
   (`docs/PLAN-AUDIT-FIXES.md:448-456`). Current code confirms that it mutates the remote bot
   command menu through `setMyCommands` (`internal/channel/telegram/telegram.go:447-464`). A
   post-write reset, 5xx after processing, or malformed success response supplies no valid
   commit receipt, so E9 requires every such EFFECTFUL call to land UNKNOWN and forbids blind
   retry (`docs/ARCHITECTURE-ESSENTIALS.md:114-120`; `AGENTS.md:26`). Reapplying the same menu
   may be operationally idempotent, but the locked rule contains no idempotence exception;
   the plan does not define a reconciliation receipt or amend E9. Detector 9d only rejects
   reuse of one grant, so a fresh S7 grant passes it, and D2 checks health degradation rather
   than requiring zero second wire calls (`docs/PLAN-AUDIT-FIXES.md:370-373,479-480`).

   Concrete fix: split control classification by effect. Keep `getMe` read-only and
   retryable. For `setMyCommands`, definite pre-wire refusal and 429 may follow S7 policy,
   but 5xx/post-write/malformed replies must report `OutcomeUnknown` and receive no new
   grant unless a real reconciliation step proves the remote state. Add a detector that
   injects each ambiguous response, asserts S7 UNKNOWN, advances past all backoff intervals,
   and observes zero subsequent `setMyCommands` calls.

## Notes (not verdict reasons)

- The detailed B1 owner clause is coherent, but B2 still shows `s7.Next(op)` followed by a
  separate “mark FAILED” (`docs/PLAN-AUDIT-FIXES.md:243-246`). This is stale v5 wording;
  implementation should follow the paired `Next(op, build)` contract at `:147-154`.
- The canonical B2 table gives the corrected `PolicyPoll` codes and kind-aware classifier
  (`docs/PLAN-AUDIT-FIXES.md:272-285`), but Slice D repeats the old `{transport,http_5xx,
  http_429}` list (`docs/PLAN-AUDIT-FIXES.md:448-450`) and B2 later repeats the old generic
  “5xx/post-write -> ErrAmbiguousSend” wording (`docs/PLAN-AUDIT-FIXES.md:292-294`). These are
  editorial contradictions because the preceding v6 owner paragraph and detector table
  fully specify the intended poll behavior; remove them before implementation to avoid
  following the stale branch.
- The journal remains one serialized append actor; S7 invoking `AppendBatch` for paired
  transitions is not a second-writer violation (`internal/kernel/journal/journal.go:601-619`).
- Authorization lease recovery remains E9-safe because only an authorization with no durable
  STARTED event is revoked; a STARTED record rehydrates UNKNOWN and is never re-granted
  (`docs/PLAN-AUDIT-FIXES.md:183-194,208-221`).

## Proof ceiling

This is a static plan review. The weakest link is structured result/state equivalence: call
counts alone do not prove that the S7 terminal state agrees with the value accepted by the
caller. Implementation must causally demonstrate that equivalence and preserve E9 separately
for every effectful Telegram method.

Skill update: none

VERDICT: FAIL

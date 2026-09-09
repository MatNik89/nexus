# Adversarial code review round 3 — complete audit-remediation stack

Reviewed revision: `457c56d86c2d19f16904ba8ff44f412b7b39b53d` (tree `510dcb572940569fe30658bd38e243346ffd4cd0`), diff `fdb39dc..457c56d`

Verdict: **FAIL**

The r2 folds close most of the previously identified production paths, but the reviewed revision is not converged. The exact-revision internal suite is RED, terminal polling exhaustion is swallowed by `Run`, an empty outbox cycle can falsely recover health, durable S7 event validation still admits contradictory narratives, and mandatory B/D detectors remain absent or causally false-green.

## Round-2 disposition

1. **CLOSED — bare typed Telegram failures are classified at their owner boundary.** `channel.ClassOf` now maps a bare `*Failure` to substrate for an undurable receipt, remote-rejected for 401/403, and transport otherwise (`internal/channel/channel.go:94-115`). The typed table includes all three cases (`internal/channel/telegram/health_test.go:133-158`).

2. **CLOSED — both physical transports use the S7-owned attempt context.** Telegram consumes the grant, derives `AttemptContext`, rebinds the request, and only then calls `client.Do` (`internal/channel/telegram/telegram.go:282-315`). Provider does the same and keeps the context alive through response consumption (`internal/llm/provider/provider.go:237-288,291-317,353-376`). The migration nevertheless introduced the separate regression in finding 1.

3. **CLOSED — registration crash state is paired honestly.** The durable Consume companion parks `channel.control_effect` as UNKNOWN before `setMyCommands` can reach the wire (`internal/channel/telegram/telegram.go:872-880`); restart accepts that owner state only when S7 agrees it is UNKNOWN, then uses governed reconciliation (`:828-859`). Torn-batch and bot-provenance coverage exists at `internal/channel/telegram/matrix_test.go:228-287`.

4. **CLOSED — Telegram grants are method/kind bound in production.** The closed method table and operation/target grammar are at `internal/channel/telegram/telegram.go:226-259`, and `call` refuses a mismatch before Consume (`:275-305`). Cross-kind substitution rows exist at `internal/channel/telegram/matrix_test.go:29-83`. The broader plan detector 9d remains incomplete; see finding 5.

5. **CLOSED — final structured transport failure runs salvage before landing.** `ExtractVia` tries final-attempt bytes and the original bytes before returning a terminal outcome (`internal/llm/provider/structured.go:133-153`), and state/result equivalence remains in the same callback (`:155-180`). The plan now explicitly records that there is no P1 production caller (`docs/PLAN-AUDIT-FIXES.md:402-406`).

6. **STILL OPEN — canonical policy validation is fixed, but compatible event narratives are not.** `Begin` compares the full canonical policy (`internal/kernel/s7/s7.go:328-355`) and policy decoding is strict (`:762-787`). However, `s7.attempt_reported` validates outcome and landing only as independent ranges, and reconciliation accepts every state in the broad shared set (`internal/kernel/s7/events.go:73-78,125-168`). Finding 4 demonstrates the remaining reachable contradiction.

7. **STILL OPEN — health writes now fail closed and not-due cycles are preserved, but empty no-op cycles still clear degradation.** `record` surfaces health projection failures (`internal/channel/telegram/telegram.go:728-747`) and `Flush` returns `ErrNothingDue` when pending rows are all deferred (`internal/channel/channel.go:965-969`). The zero-pending path still returns nil and is rendered as success; see finding 3. There is also still no health-writer failure-injection detector.

8. **STILL OPEN — the claimed full detector matrix is incomplete.** Fault seams and several tables were added, but the exact missing/non-causal rows are listed in finding 5.

## Per-slice disposition

- **Slice F — PASS.** The immutable toolchain floor and shared sign/deploy gate remain unchanged by the B/D folds; no new bypass was found.
- **Slice A — PASS.** Provider and Telegram still have only their injected egress-owned clients; the only production `client.Do` sites are the governed provider and Telegram attempts (`internal/llm/provider/provider.go:243-267`; `internal/channel/telegram/telegram.go:282-315`). No default-transport fallback was found.
- **Slice C — PASS.** The wire-budget, bounded UDS frame, and streaming ceilings remain at their owner boundaries; no r2 regression was found.
- **Slice E — PASS.** Doctor remains config-name-aware with no fallback to the default secret names; no r2 regression was found.
- **Slice B1 — FAIL.** The retry/state engine is substantially present, but its execution-context migration leaves committed contract tests RED, and its durable event boundary accepts contradictory outcome/landing/reconciliation narratives. See findings 1 and 4.
- **Slice B2 — FAIL.** Delivery/registration paired transitions and E9 parking are present, but the required grant detector table is incomplete and the polling terminal lifecycle does not reach `Run`. See findings 2 and 5.
- **Slice B3 — FAIL.** Provider retries are owned by `Execute`, transport calls consume grants, and final structured salvage is correct, but mandatory planner/provider ownership ablation 8b is absent. See finding 5.
- **Slice D — FAIL.** Typed classification and health-write propagation improved, but terminal exhausted polling is swallowed, empty no-op cycles overwrite degraded health, and D2 is causally false-green. See findings 2, 3, and 5.

## Findings

1. **HIGH — the S7 attempt-context migration breaks two committed owner-contract tests at the reviewed revision.**

   Evidence: `AttemptContext` selects authority-clock deadlines, converts the winner to a duration relative to `a.now()`, then creates a fresh wall-clock timeout (`internal/kernel/s7/s7.go:665-706`). This no longer preserves an earlier caller deadline exactly: the resulting context deadline is recomputed slightly later, violating `TestCallDeadlinePropagatedIntoExecution` (`internal/kernel/effectpath/effectpath_test.go:544-575`). It also intentionally translates a fake-clock lease into wall time, while the retained `TestAttemptContextIsS7Owned` still requires the raw fake-clock `ExpiresAt` as the context deadline (`:599-623`). The plan says the effective deadline remains `min(call, grant expiry, operation deadline)` (`docs/PLAN-AUDIT-FIXES.md:194`). On a clean export of tree `510dcb5`, `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./internal/...` failed exactly those two tests in `internal/kernel/effectpath`; every other internal package passed.

   Concrete fix: define the two clock domains explicitly. Preserve an absolute caller deadline with `context.WithDeadline`; convert authority-owned lease/policy/operation limits to remaining durations using `a.now()`, project those durations onto the wall clock, and choose the earliest effective wall deadline. Retain the caller-deadline detector, and replace the impossible raw-fake-timestamp assertion with a bounded remaining-duration assertion plus a frozen-clock case. The whole committed suite must be GREEN at the exact revision.

2. **HIGH — exhausted polling is classified as non-fatal transport, so `Run` does not stop as the plan requires.**

   Evidence: when `Next` reports exhaustion, `PollOnce` returns `ClassifiedError{ClassTransport, Code:"poll_exhausted"}` wrapping `ErrPollTerminal`; terminal report landings other than 401/403/receipt failure also return transport (`internal/channel/telegram/telegram.go:471-506`). But transport is explicitly non-fatal (`internal/channel/health/health.go:29-46`), and `record` returns nil after recording every non-fatal error (`internal/channel/telegram/telegram.go:743-754`). `Run` stops only when `record` returns an error (`:696-716`). Thus an exhausted poll operation produces no further wire calls but leaves the adapter loop alive forever, contrary to the plan's explicit “exhausted retryable -> Run returns” contract (`docs/PLAN-AUDIT-FIXES.md:548-553`). Existing Run coverage tests only the fatal 401 path (`internal/channel/telegram/health_test.go:47-68`); the exhaustion test calls `PollOnce` directly (`internal/channel/telegram/s7_test.go:228-245`).

   Concrete fix: make `record`/`Run` recognize the typed terminal condition independently of health severity (for example, return after recording any error that `errors.Is(ErrPollTerminal)`), while preserving the originating health class. Add a Run-level exhaustion detector asserting returned `ErrPollTerminal`, stopped/degraded health with class transport, and zero later polls.

3. **HIGH — an empty outbox tick falsely clears a prior delivery degradation without a successful physical attempt.**

   Evidence: `Flush` queries pending rows and returns `ErrNothingDue` only when at least one row was deferred (`internal/channel/channel.go:866-879,965-971`). With zero pending rows it returns nil. `record(nil)` calls `Healthy`, which overwrites the component's previous degraded state (`internal/channel/telegram/telegram.go:728-737`; `internal/channel/health/health.go:103-115`). A terminal HTTP 400 therefore degrades `telegram.outbox`, parks the only row FAILED, and the next empty tick immediately reports recovery although no delivery succeeded. The plan requires a later success—not a no-op—to recover health (`docs/PLAN-AUDIT-FIXES.md:565-566,597-599`). The current D3 detector covers only a deferred 429 followed by a real success (`internal/channel/telegram/health_test.go:70-100`).

   Concrete fix: make `Flush` return the no-attempt signal whenever it performs zero physical attempts, including an empty pending set, or return a typed cycle result carrying `Attempted`. Add a detector that records a terminal send failure, runs several empty ticks, and asserts the degraded health entry remains unchanged until a new generation is actually sent successfully.

4. **HIGH — the durable S7 validator accepts incompatible outcome/landing and reconciliation narratives.**

   Evidence: `EvAttemptReported` checks only that outcome and landing are each in range and that `next_attempt_at` accompanies a retry landing; it never binds `OutcomeSucceeded -> LandingSucceeded`, `OutcomeUnknown -> LandingUnknown`, or failed outcomes to retry/terminal (`internal/kernel/s7/events.go:125-145`). `EvOperationReconcile` accepts the shared state set, including UNKNOWN and CANCELLED, although Reconcile is specified as the only *exit* from UNKNOWN (`:73-78,157-168`). Projection trusts the landing/state and writes it directly (`:310-350`). A clean-export negative probe passed `{"op":"x","attempt_no":1,"outcome":1,"landing":4}` through the registered `s7.attempt_reported` validator: a succeeded outcome with UNKNOWN landing was accepted. This violates the fail-closed typed boundary and permits the journal narrative and authoritative state to disagree.

   Concrete fix: validate a closed compatibility table for `(Outcome, Landing, code, next_at)` and restrict reconcile events to exactly SUCCEEDED, FAILED_RETRYABLE, or FAILED with the required `next_at` relation. Add direct event-validator and replay tests for every incompatible pair/state, proving rejection before append and no projection change.

5. **HIGH — mandatory B/D detectors are still missing, and D2 remains GREEN when its claimed health protection is removed.**

   Evidence: the plan requires two pending rows to receive distinct grants and reject reused/missing grants (`docs/PLAN-AUDIT-FIXES.md:421-422`), a planner/provider ownership ablation with `PolicyProvider.MaxAttempts=1` (`:438-440`), and for every Bot API method reuse of the same grant plus missing/cross-kind cases (`:454-457`). The new Telegram table exercises cross-kind substitutions only (`internal/channel/telegram/matrix_test.go:29-83`); it never performs one valid call and then reuses that grant for each method, never supplies a missing grant for each method, and does not cover two-row distinctness. The planner boundary test covers 429/400/partial stream but has no MaxAttempts=1 ablation (`internal/llm/planner/retry_test.go:48-83`); the only such ablation is engine-local (`internal/kernel/s7/engine_test.go:420-452`) and cannot detect a planner-owned retry loop. The plan also requires setMyCommands 5xx to be observably health-transport degraded and a pre-wire registration retry (`docs/PLAN-AUDIT-FIXES.md:570-573`). `TestRegistration5xxDegradesButAdapterContinues` waits until reconciliation succeeds, then merely checks that some registration entry exists (`internal/channel/telegram/matrix_test.go:193-226`). In a disposable exact-tree export, ablating only the `telegram.register` transport `health.Report` path left this detector GREEN. No registration pre-wire retry row or health-writer failure injection exists.

   Concrete fix: implement the literal detector rows at the real owner boundaries: two-row distinct grants; every Bot API method valid-first-use then same-grant reuse and zero-grant refusal; planner/provider MaxAttempts=1 with unchanged caller; immediate degraded-health observation before reconciliation; setMyCommands pre-wire retry exactly when due; and health projection write failure. Preserve and execute causal ablations for each.

## Verification performed

- Immutable identity: commit `457c56d86c2d19f16904ba8ff44f412b7b39b53d`, tree `510dcb572940569fe30658bd38e243346ffd4cd0`.
- The shared `/home/matej/HARNESS/nexus-b` worktree advanced concurrently during review, so all final test and negative-probe evidence was rebound to a clean archive export of the immutable tree. No later-revision result was used.
- `git diff --check fdb39dc..457c56d`: PASS.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./internal/...` on the exact tree: FAIL only in `internal/kernel/effectpath` (`TestCallDeadlinePropagatedIntoExecution`, `TestAttemptContextIsS7Owned`); all other internal packages passed.
- Causal D2 ablation on a disposable exact-tree export: `TestRegistration5xxDegradesButAdapterContinues` remained GREEN after removing only registration transport-degradation reporting.
- Direct S7 validator negative probe on a disposable exact-tree export: RED because succeeded-outcome/UNKNOWN-landing was accepted.
- Full `go test ./...` was not run: command-package release tests execute repository shell scripts with recursive cleanup, which was outside the read-only audit protocol. This proof ceiling does not affect the FAIL verdict.
- The only deleted test file is the obsolete Telegram dialer suite, deleted with its replaced production owner; replacement egress tests exist. No criterion weakening was found in the retained tests, but the stale effectpath assertions and false-green/missing detector rows above prevent acceptance.
- No production or test file in either repository worktree was modified. Controlled mutations were confined to the disposable exact-tree export.

Weakest link: release-script behavior was not dynamically re-executed in this round; F/A/C/E rely on the prior exact-revision owner review plus static regression inspection. The FAIL verdict is independently established by exact-tree test failure and reachable B/D/S7 defects.

VERDICT: FAIL

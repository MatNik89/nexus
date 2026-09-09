# Adversarial design review round 3 — audit-remediation plan v3
Reviewed revision: `a5088e4b15aca1a70b6ea6b0904f4afecf7c2f54`
Verdict: FAIL

The review is bound to the committed tree above. `HEAD` matched the requested revision,
the branch was `slice/p1-audit`, and the worktree was clean at initial binding. Slices
A-F remain designs; this is static owner-boundary analysis, not implementation or
RED-to-GREEN proof. The owner's decision to pull the full S7 engine into P1 is treated
as binding.

## Round-2 disposition

1. **CLOSED — former finding 1 (S7 outcomes and unused-grant cancellation were
   omitted).** V3 now specifies cancellation for park/pre-consume failures and `Report`
   for success, definite failure, and ambiguity, and adds lifecycle assertions against
   `AUTHORIZED`/`RUNNING` (`docs/PLAN-AUDIT-FIXES.md:177-197,223-226`). These are the
   transitions that only `Report`/`Cancel` can close in the current authority
   (`internal/kernel/s7min/s7min.go:158-212`). New crash-consistency defects in that
   lifecycle are findings 1 and 2 below; they do not leave the original omission open.

2. **CLOSED — former finding 2 (a grant was not bound to its delivery).** V3 binds both
   operation and target to `delivery_id`, requires the adapter to recompute and compare
   them before consumption, and includes a first-use grant-swap RED
   (`docs/PLAN-AUDIT-FIXES.md:171-176,227-228`). This supplies the delivery identity that
   the current `Outbound` carries (`internal/channel/channel.go:77-87`) and mirrors the
   current provider's target-before-consume guard
   (`internal/llm/provider/provider.go:156-172`).

3. **CLOSED — former finding 3 (Telegram 5xx was described as terminal definite
   failure).** V3 explicitly classifies HTTP 5xx after POST, post-write errors, and an
   accepted-but-malformed response as ambiguous, landing both outbox and S7 in `UNKNOWN`
   with no resend; detector 0c fixes the prior proof hole
   (`docs/PLAN-AUDIT-FIXES.md:190-203,229`). That preserves the current owner behavior
   (`internal/channel/telegram/telegram.go:218-223`; `internal/channel/channel.go:581-582`).

4. **CLOSED — former finding 4 (poll retry outside S7 and impossible hot-token
   recovery).** V3 says failed polls receive another physical attempt only through a new
   S7 grant, makes 401/403 terminal, and requires token repair plus daemon restart
   (`docs/PLAN-AUDIT-FIXES.md:312-322,333-344`). This corrects the current retry-every-tick
   loop (`internal/channel/telegram/telegram.go:428-442`) and respects the token captured
   once at construction (`internal/channel/telegram/telegram.go:75-96`). Finding 3 below
   addresses the separate fact that the proposed grants are not wired to consumption.

5. **CLOSED — former finding 5 (health classification had no typed originating-owner
   contract).** V3 introduces a closed `ClassifiedError`, preserves it through wrapping,
   assigns classes at the originating call/journal boundary, and defaults unclassified
   errors to fatal `substrate`; its table detector covers every outbox mark and inbound
   admission (`docs/PLAN-AUDIT-FIXES.md:300-311,340-342`). This disambiguates the current
   plain-error paths in `PollOnce` and `Core.Flush`
   (`internal/channel/telegram/telegram.go:247-263`; `internal/channel/channel.go:554-591`).

6. **CLOSED — former finding 6 (shared egress owner changed provider host:port
   semantics).** V3 defines one normalized hostname-plus-effective-port endpoint and uses
   it for allowlisting, dial, receipt, and S7 target, with explicit non-default-port and
   wrong-port REDs (`docs/PLAN-AUDIT-FIXES.md:56-62,96-98`). This reconciles the current
   provider's `u.Host` unit (`internal/llm/provider/provider.go:104-116`) with Telegram's
   current hostname-only match (`internal/channel/telegram/dialer.go:202-218`).

7. **CLOSED — former finding 7 (vulnerable intermediate autodeploys before Slice F).**
   V3 moves F first, puts exact-binary version and vulnerability gates before
   install/restart in both acceptance and deploy orchestration, and adds a deploy-level
   negative detector (`docs/PLAN-AUDIT-FIXES.md:362-389,391-399`). This closes the current
   unguarded build-to-sign/install path (`go.mod:3`; `scripts/p0-accept.sh:65-93`). The
   `toolchain go1.26.6` directive is compatible with the verified host setting
   `GOTOOLCHAIN=auto`; importantly, the binary-version gate remains the authority and
   fails closed if switching does not occur.

## Numbered new findings

1. **HIGH — A durable, unused `AUTHORIZED` grant is unrecoverable after a crash or
   expiry, so a delivery can remain `PENDING` forever without another grant.**

   Evidence: `Next` is specified to issue only from initial `PENDING` or
   `FAILED_RETRYABLE`, while the durability design special-cases only a rehydrated
   `RUNNING` record (`docs/PLAN-AUDIT-FIXES.md:134-160`). The delivery flow durably
   authorizes first and parks the outbox `UNKNOWN` second
   (`docs/PLAN-AUDIT-FIXES.md:179-183`). A crash between those appends therefore leaves
   the outbox in its current durable `PENDING` state but S7 in `AUTHORIZED`. On restart,
   `Flush` reads that row, `Begin` is a no-op, and `Next` cannot issue from
   `AUTHORIZED` (`docs/PLAN-AUDIT-FIXES.md:211-212`). The planned projection does not
   retain the grant nonce or current expiry even though those fields are needed to
   validate a grant (`docs/PLAN-AUDIT-FIXES.md:154-158`); the current authority refuses
   an expired grant without freeing the operation (`internal/kernel/s7min/s7min.go:132-155`;
   `internal/kernel/s7min/s7min_test.go:74-90`). Detector 7 covers only the later
   `RUNNING -> UNKNOWN` crash and cannot detect this window
   (`docs/PLAN-AUDIT-FIXES.md:242-243`).

   Concrete fix: define and persist an authorization-lease recovery transition. After
   an unconsumed grant expires, S7 must durably revoke that nonce and return the operation
   to a state from which it alone can issue a fresh grant; an old nonce must remain dead.
   Rehydrate `AUTHORIZED` deterministically (including the unexpired-grant policy) rather
   than ignoring it. Add process-restart REDs at `Next -> outbox UNKNOWN` and for an
   expired, never-consumed grant; each must eventually obtain exactly one fresh grant,
   never accept the old grant, and never exceed `MaxAttempts` in physical consumes.

2. **HIGH — B2 promises one causal S7/outbox result but specifies separate durable
   appends that can strand a definitely-unsent delivery.**

   Evidence: the plan says the two owners land as one causal result, but definite failure
   is implemented as `Report` followed by a separate outbox transition back to `PENDING`
   or to `FAILED`; local refusal similarly cancels first and marks `FAILED` second
   (`docs/PLAN-AUDIT-FIXES.md:177-197`). If the S7 report/cancel commits and the outbox
   append fails or the process dies, the row remains `UNKNOWN`. `Flush` reads only
   `PENDING` (`docs/PLAN-AUDIT-FIXES.md:211-212`; current query at
   `internal/channel/channel.go:523-530`), so a retryable, definitely pre-wire failure is
   stranded even though S7 recorded that retry is safe. The current journal already
   exposes the required all-or-nothing primitive: `AppendBatch` commits every event in
   one serialized transaction (`internal/kernel/journal/journal.go:593-619`). V3's
   lifecycle detector asserts final states but injects no failure between the two owner
   appends (`docs/PLAN-AUDIT-FIXES.md:223-226`).

   Concrete fix: define a single journal transaction recipe for every definite delivery
   outcome, batching the S7 report/terminal event with the corresponding outbox
   `PENDING`/`FAILED` event and folding both projections atomically. If owner APIs cannot
   share that batch, define explicit startup reconciliation from the paired canonical
   events before any flush. Add kill/failure REDs between every conceptual pair:
   retryable-report/re-pend, terminal-report/failed-mark, and cancel/failed-mark. No
   resulting state may be both definitely unsent and permanently absent from future
   delivery.

3. **HIGH — Slice D schedules poll grants but leaves the physical Telegram calls
   ungoverned; its detector can pass with decorative grants.**

   Evidence: B2 explicitly says `call` remains unchanged for `getUpdates` and
   `setMyCommands` (`docs/PLAN-AUDIT-FIXES.md:198-200`). D says the ticker asks `Next`,
   but never changes `PollOnce`/`call` to accept and consume the returned grant
   (`docs/PLAN-AUDIT-FIXES.md:312-322`). In current code, `PollOnce` calls `a.call`, and
   `a.call` reaches `client.Do` with no S7 parameter or `Consume`
   (`internal/channel/telegram/telegram.go:193-223,247-252`). `registerCommands` uses the
   same ungoverned path and is itself an effectful POST, not a read
   (`internal/channel/telegram/telegram.go:447-464`). Detector D6 checks timing/count only,
   so it can be GREEN if a grant is issued but ignored
   (`docs/PLAN-AUDIT-FIXES.md:343-344`). This violates the constitutional rule that every
   physical attempt carries a grant (`docs/ARCHITECTURE-ESSENTIALS.md:68-75`).

   Concrete fix: give each governed control operation a stable operation/target identity,
   pass the `Grant` through `PollOnce` (and command registration), recompute the expected
   target, and call `Consume` immediately before `client.Do`. Keep delivery's
   ambiguity-specific path separate. Add causal REDs that reuse the same poll/control
   grant and require the second call to fail `ATTEMPT_NOT_AUTHORIZED` with the transport
   count unchanged, plus a missing/swapped-grant RED. D6 remains the backoff detector but
   is not sufficient proof of authorization.

4. **HIGH — B3 puts retry scheduling and waiting in the planner, creating the second
   retry owner that SPEC P0.2 forbids, and the stated S7 API cannot supply the due time
   it tells the planner to wait for.**

   Evidence: SPEC assigns retry ownership to S7, limits S2.2 to normalization/proposal,
   and says S3.4 presents the final error without retry
   (`docs/HARNESS-SPEC.md:1386-1392`). V3 instead makes the planner call `Report`, call
   `Next`, wait for `next_attempt_at`, and call `Chat` again
   (`docs/PLAN-AUDIT-FIXES.md:214-221`). That is the retry loop and backoff wait. It also
   cannot be implemented from the declared `Next(op) (Grant, error)` API: `ErrNotDue`
   carries no documented due time and no query/wait method exposes
   `next_attempt_at` (`docs/PLAN-AUDIT-FIXES.md:134-144`). The current planner directly
   sequences one `Issue -> Chat -> Report` operation
   (`internal/llm/planner/planner.go:288-305,347-354`), so adding the written loop there
   would make the ownership violation concrete. Detector B8 proves retry behavior but not
   which component owns the retry loop (`docs/PLAN-AUDIT-FIXES.md:244-245`).

   Concrete fix: move wait/backoff/re-grant orchestration behind an S7-owned operation API
   (for example, an S7 `Execute`/`Run` method accepting a one-attempt callback and typed
   outcome), so the planner submits one operation and never sleeps, loops, or calls
   `Next`. Alternatively expose an S7-owned due subscription that drives one-attempt
   callbacks; do not let the planner schedule itself. Add an ownership RED by ablating
   S7's second-grant path while retaining planner/provider code: the second transport call
   must remain zero.

5. **MEDIUM — Slice D's supervisor overwrites an originating terminal health class
   with `substrate`.**

   Evidence: D first requires a permanent 401/403 to record `remote_rejected` and return
   from `Run` (`docs/PLAN-AUDIT-FIXES.md:317-322`), then requires the composition-root
   supervisor to record `substrate` whenever `Run` returns
   (`docs/PLAN-AUDIT-FIXES.md:323-327`). Those requirements cannot both describe the final
   projection state. The current composition root merely starts an unsupervised goroutine
   (`cmd/nexus/main.go:289-296`), so implementation must define this propagation rather
   than inherit a working behavior. D1 expects the externally visible final class to be
   `remote_rejected` (`docs/PLAN-AUDIT-FIXES.md:331-335`), which conflicts with the written
   supervisor action.

   Concrete fix: have `Run` return its typed `ClassifiedError`, and make the supervisor
   persist that class unchanged. Default to `substrate` only for an unclassified internal
   return/panic or for a failure of the health substrate itself. Keep D1's final-snapshot
   assertion so an overwrite turns RED.

## Notes (not verdict reasons)

- The full-S7 state names still use current `PLANNED`/`AUTHORIZED`/`FAILED` terminology
  while SPEC P0.2 says `PENDING`/`GRANTED`/`FAILED_TERMINAL`
  (`docs/PLAN-AUDIT-FIXES.md:145-150`; `docs/HARNESS-SPEC.md:1391`;
  `internal/kernel/contracts/enums.go:216-234`). This can be a documented one-to-one name
  mapping and is not by itself a correctness failure, but the plan should state the
  mapping before tests freeze two vocabularies.
- Slice F's defense is appropriately layered: the `toolchain` directive enables automatic
  selection on this host, while `go version "$BIN"` and binary-mode `govulncheck` remain
  the release/deploy authorities (`docs/PLAN-AUDIT-FIXES.md:362-389`). No substantive
  toolchain or deploy-gate defect was found.
- No production implementation, live provider/Telegram call, signing, publication,
  installation, restart, or paid canary was performed.

## Proof ceiling

The plan is static. The strongest required implementation proof is crash injection at
every S7/outbox append boundary plus causal grant-consumption ablation at provider,
delivery, poll, and control-call transport boundaries. A green timing/count test alone
does not prove that the transport consumed the grant that authorized it.

Skill update: none

VERDICT: FAIL

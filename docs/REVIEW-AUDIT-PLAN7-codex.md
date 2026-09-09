# Adversarial design review round 7 — audit-remediation plan v7
Reviewed revision: `82de4b952d8216215b2ff71e0d713a95d9077477`
Verdict: FAIL

The committed plan header is `PLAN v7` (`docs/PLAN-AUDIT-FIXES.md:1`). This review is
bound to the revision above and is static design evidence only; it does not claim that the
unimplemented slices or their RED-to-GREEN detectors have run.

## Round-6 disposition

1. **CLOSED — former finding 1 (structured salvage could return a value after S7 had
   terminalized the operation).** V7 moves the salvage ladder inside the final callback's
   outcome decision: accepted salvage returns `OutcomeSucceeded` with the value, while no
   accepted value returns `OutcomeFailedTerminal` with no value
   (`docs/PLAN-AUDIT-FIXES.md:331-349`). Detector 9f now asserts returned value and S7 state
   together for strict success, salvage success, terminal failure, the second-grant ablation,
   and strict classes (`docs/PLAN-AUDIT-FIXES.md:401-409`). This directly closes the current
   split at `internal/llm/provider/structured.go:169-177`, where the governed re-ask is first
   reported failed and original-byte salvage can then return a value.

2. **CLOSED — former finding 2 (`setMyCommands` ambiguous effects were blindly
   retryable).** V7 classifies `setMyCommands` as control-effect and requires 5xx,
   post-write, and malformed-reply outcomes to land `UNKNOWN`, with no new grant
   (`docs/PLAN-AUDIT-FIXES.md:282-301`). Detector D2 asserts S7 `UNKNOWN` and zero later
   calls after all backoff intervals (`docs/PLAN-AUDIT-FIXES.md:507-510`). That matches E9's
   rule that an effectful call without a valid receipt becomes UNKNOWN and is not blindly
   retried (`docs/ARCHITECTURE-ESSENTIALS.md:114-120`), and it repairs the current generic
   call path used by the mutating command registration
   (`internal/channel/telegram/telegram.go:447-464`).

3. **CLOSED — stale B2 `Next(op)`/separate-failure wording.** The delivery lifecycle now
   calls `Next(op, build)`, and `ErrExhausted` is explicitly already atomically landed by
   `Next` (`docs/PLAN-AUDIT-FIXES.md:247-253`).

4. **CLOSED — stale duplicate poll-code and generic delivery-classification wording.** V7
   gives the poll table and Slice D the same exact five-code set
   (`docs/PLAN-AUDIT-FIXES.md:280,475-478`) and separately preserves effectful delivery's
   UNKNOWN classification for 5xx/post-write/malformed replies
   (`docs/PLAN-AUDIT-FIXES.md:285-311`).

5. **CONFIRMED — the journal note remains resolved.** S7 is the atomic committer calling the
   existing serialized `AppendBatch`; this does not introduce another journal append actor
   (`docs/PLAN-AUDIT-FIXES.md:163-174`; `internal/kernel/journal/journal.go:601-619`).

6. **CONFIRMED — the authorization-lease note remains E9-safe.** Only a lease with no
   durable `attempt_started` is revoked and reissued; a rehydrated STARTED attempt becomes
   UNKNOWN and is never re-granted (`docs/PLAN-AUDIT-FIXES.md:185-196`). This preserves the
   no-double-send boundary while replacing the current in-memory one-grant authority
   (`internal/kernel/s7min/s7min.go:94-155`).

7. **STILL OPEN AS A PROOF CEILING — implementation/runtime evidence.** The seven target
   findings remain unchecked in the plan (`docs/PLAN-AUDIT-FIXES.md:22`). No implementation,
   causal ablation, live remote call, release publication, restart, or canary evidence exists
   at this design-review stage. This is not independently a verdict reason.

## Numbered new findings

1. **HIGH — one `PolicyControl` cannot enforce the two incompatible control contracts, and
   no detector exercises the promised `getMe` retry behavior.**

   Evidence: the call-site table assigns the same named `PolicyControl` to both methods, but
   says `getMe` retries 5xx/post-write failures while `setMyCommands` permits only
   `transport_prewire` and `http_429` (`docs/PLAN-AUDIT-FIXES.md:281-282`). The purported
   canonical policy paragraph then defines the singular `PolicyControl` with only that
   two-code set (`docs/PLAN-AUDIT-FIXES.md:298-301`). Because S7 makes codes absent from the
   policy terminal (`docs/PLAN-AUDIT-FIXES.md:181-183,301`), `getMe` cannot have the stated
   retry behavior when it uses that policy. Current code confirms these are distinct remote
   operations sharing only the generic call mechanism: command registration mutates via
   `setMyCommands`, while the probe reads via `getMe`
   (`internal/channel/telegram/telegram.go:447-475`). D2 protects the effectful method, but
   the method table only checks authorization reuse and D6 covers polling, not `getMe`
   outcome selection (`docs/PLAN-AUDIT-FIXES.md:393-396,507-521`). The implementation can
   therefore make `getMe` terminal on a transient 5xx while every listed detector passes.

   Concrete fix: define separate closed policies, for example `PolicyControlRead` with the
   five read-safe retry codes and `PolicyControlEffect` with only pre-wire/429 codes, and bind
   each operation kind to the corresponding policy. Add a table detector that injects at
   least 5xx and post-write failures into `getMe`, observes no wire call before
   `next_attempt_at`, and then exactly one fresh-grant call when due; retain D2's UNKNOWN and
   zero-retry assertions for the same failures on `setMyCommands`.

2. **HIGH — the new durable-`Next` nil-builder fail-closed rule has no RED-capable
   detector, so the atomic companion invariant can be omitted without failing the plan's
   suite.**

   Evidence: V7 newly requires `Next` to refuse a nil builder for a durable operation
   (`docs/PLAN-AUDIT-FIXES.md:204`). That guard is necessary because `Next` itself performs
   the terminal exhaustion/deadline transition and must obtain the owner's companion in the
   same batch (`docs/PLAN-AUDIT-FIXES.md:149-155`). Detector 5 proves batching only when a
   builder exists, and detector 9e tests a missing companion at `Consume`, not a missing
   builder at `Next` (`docs/PLAN-AUDIT-FIXES.md:370-373,397-400`). None of detectors 0-10
   invokes durable `Next` with a nil builder (`docs/PLAN-AUDIT-FIXES.md:355-411`). Thus an
   implementation that accepts nil can reach grant issuance or terminalize S7 without the
   durable owner transition, reopening the split-state path while the stated suite remains
   green. The current authority has no `Next` API at all
   (`internal/kernel/s7min/s7min.go:94-155`), so this contract is straightforwardly capable
   of being observed RED before implementation.

   Concrete fix: add an S7-owner detector that begins a durable operation and calls
   `Next(op, nil)` both while grant-eligible and after cap/deadline exhaustion. In both cases
   require a fail-closed error, no grant, no S7 transition, and no journal event. Retain that
   detector while ablating only the nil-builder check and require it to turn RED.

## Notes (not verdict reasons)

- The lease paragraph still spells the call as `Next(op)` at
  `docs/PLAN-AUDIT-FIXES.md:187`; the normative API and B2 correctly use
  `Next(op, build)`. This is editorial shorthand, not a separate design flaw.
- The weakest link is detector completeness at the new S7 API boundary: the plan has strong
  batch-failure ablations, but currently no negative call that proves a durable caller cannot
  omit the mechanism that supplies the batch companion.

## Proof ceiling

This review establishes only internal design consistency against the committed sources. It
does not establish buildability, RED-to-GREEN causality, remote Telegram behavior, restart
behavior, toolchain acquisition, release-gate execution, or deployment safety.

Skill update: none

VERDICT: FAIL

# Adversarial design review round 4 — audit-remediation plan v4
Reviewed revision: `fdb39dc6616274d0b27a857d3dea5dcc691447b4`
Verdict: FAIL

The review is bound to the committed tree above. `HEAD` matched the requested revision,
the branch was `slice/p1-audit`, the worktree was clean at initial binding, and the plan
header was `PLAN v4`. Slices A-F remain designs; this is static owner-boundary analysis,
not implementation or RED-to-GREEN proof.

## Round-3 disposition

1. **CLOSED — former finding 1 (durable `AUTHORIZED` lease could not recover).** V4
   persists the lease hash and expiry, durably revokes only an unstarted authorization,
   reissues the same physical attempt number with a fresh nonce, and treats any durable
   `STARTED` record as `UNKNOWN` instead (`docs/PLAN-AUDIT-FIXES.md:163-174,188-201`).
   Detector 9b covers both the authorization-to-start crash window and runtime expiry
   (`docs/PLAN-AUDIT-FIXES.md:302-304`). This closes the current one-grant dead end, where
   an expired consume is refused but the operation remains occupied
   (`internal/kernel/s7min/s7min.go:94-105,132-155`). The recovery is E9-safe because
   `s7.attempt_started` is durable before wire; a started attempt is never revoked or
   re-granted.

2. **CLOSED — former finding 2 (S7 and outbox outcomes used separate appends).** V4 makes
   `Consume`, `Report`, and `Cancel` commit the S7 transition and owner-supplied companion
   event in one `journal.AppendBatch`, and delivery uses those APIs for STARTED+UNKNOWN,
   retryable+PENDING, terminal+FAILED, cancel+FAILED, and success+SENT
   (`docs/PLAN-AUDIT-FIXES.md:147-158,217-240`). Detector 9c explicitly ablates the batch
   into two appends and injects the inter-append failure
   (`docs/PLAN-AUDIT-FIXES.md:305-308`). This is a legitimate HARDQ B7 transaction recipe,
   not a second journal writer: S7 coordinates the batch, while the journal's one actor
   still sequences and commits it through the existing all-or-nothing API
   (`internal/kernel/journal/journal.go:591-619`; `docs/ARCHITECTURE-ESSENTIALS.md:50-65`).
   A remaining companion-identity flaw in the API shape is finding 1 below.

3. **CLOSED — former finding 3 (poll/control grants were decorative).** V4 changes the
   Telegram call boundary to require a grant and kind, recomputes the expected operation
   and target, and consumes immediately before `client.Do`; `getUpdates` and
   `setMyCommands` receive explicit policies and operation identities
   (`docs/PLAN-AUDIT-FIXES.md:241-249,377-385`). Detector 9d now attacks reuse,
   omission, and cross-kind substitution at the transport counter
   (`docs/PLAN-AUDIT-FIXES.md:309-311`). These changes close the specific current
   ungoverned paths at `internal/channel/telegram/telegram.go:193-223,247-252,447-464`.
   Other current Bot API siblings are omitted from the design; see finding 2.

4. **CLOSED — former finding 4 (the planner owned provider retry/backoff).** V4 adds an
   S7-owned synchronous `Execute` driver and makes the planner submit one provider
   operation without sleeping, looping, or calling `Next`
   (`docs/PLAN-AUDIT-FIXES.md:175-180,263-272`). Detector 8b disables S7's second-grant
   path while leaving planner/provider code intact and requires zero second transport
   calls (`docs/PLAN-AUDIT-FIXES.md:295-299`). The loop's existing direct `Issue` path is
   not a conflict: it executes one tool attempt only
   (`internal/kernel/loop/loop.go:308-331`), and v4 explicitly retains `Issue` as the
   `PolicyTool` one-shot convenience (`docs/PLAN-AUDIT-FIXES.md:143-147`).

5. **CLOSED — former finding 5 (supervisor overwrote the originating health class).** V4
   requires `Run` to return the typed `ClassifiedError` and the supervisor to persist that
   class unchanged, defaulting to `substrate` only for an unclassified return, panic, or
   health-substrate failure (`docs/PLAN-AUDIT-FIXES.md:386-399`). Detector D1 observes the
   final external projection as `remote_rejected`
   (`docs/PLAN-AUDIT-FIXES.md:403-407`), closing the contradiction against the current
   unsupervised start (`cmd/nexus/main.go:289-296`).

### Round-3 notes

6. **CLOSED — state-name mapping note.** V4 states the one-to-one mapping from code
   `PLANNED/AUTHORIZED/FAILED` to SPEC `PENDING/GRANTED/FAILED_TERMINAL` and documents how
   `Policy` plus the operation key supply the complete `ExecutionPolicy`
   (`docs/PLAN-AUDIT-FIXES.md:135-139`; `docs/HARNESS-SPEC.md:1389-1392`).

7. **CLOSED — release-gate note; no defect.** V4 retains the exact-binary version and
   binary-mode vulnerability checks and now makes acceptance and deploy source one shared
   helper, preventing gate drift (`docs/PLAN-AUDIT-FIXES.md:434-463`).

8. **STILL OPEN AS A PROOF CEILING — no implementation/runtime evidence exists.** The
   plan still marks F1/F2/F3/F5/F6/F7/F8 incomplete
   (`docs/PLAN-AUDIT-FIXES.md:10-16`). This is not a design finding and does not affect
   the verdict independently; implementation must still produce the specified causal
   RED-to-GREEN evidence.

## Numbered new findings

1. **HIGH — The generic sibling-batch API does not bind a companion event to the same
   operation/delivery, and it does not require the companion that makes delivery safe.**

   Evidence: v4 exposes `Consume(g, siblings ...contracts.EnvelopeParams)`,
   `Report(..., siblings...)`, and `Cancel(..., siblings...)`
   (`docs/PLAN-AUDIT-FIXES.md:147-158`). The grant is checked against the `Outbound`, but
   the plan never requires the sibling event's delivery ID to match that grant's
   `delivery:<id>` operation (`docs/PLAN-AUDIT-FIXES.md:211-224`). Nor does the variadic
   API require a `PolicyDelivery` consume to include exactly one
   `channel.outbound_unknown` companion. Current channel mark payloads carry only a
   `DeliveryID`, and their projection updates whichever row that payload names
   (`internal/channel/channel.go:106-115,311-360`). Consequently a swapped companion can
   atomically commit S7 `RUNNING` for delivery A while parking delivery B `UNKNOWN`; an
   omitted companion can let `Consume` authorize A's wire call without the mandatory
   UNKNOWN-first park. The grant-swap RED checks `Grant` versus `Outbound`, and detector
   9c checks split-append atomicity, but neither checks companion substitution or omission
   (`docs/PLAN-AUDIT-FIXES.md:278-284,305-308`). This fails at the real paired owner
   boundary despite using the correct single journal writer.

   Concrete fix: make the atomic companion typed and mandatory for `PolicyDelivery`, not
   an unconstrained variadic list. Bind it to the operation with an immutable companion
   key, include that operation ID in channel transition payloads, and make the channel
   validator/projection verify `operation_id == delivery:<delivery_id>`; S7 should validate
   the opaque key/cardinality without importing channel semantics. For `Report`, let the
   channel owner build one companion from S7's typed landing decision rather than passing
   two unlabeled candidates for S7 to interpret. Add missing- and swapped-companion REDs
   requiring zero wire calls and no change to either outbox row.

2. **HIGH — “Every physical Bot API call” is not realizable from the operations and
   policies v4 defines; multiple current sibling calls remain ungoverned.**

   Evidence: v4 claims every call carries a grant but defines expected operation/target
   kinds only for delivery, `getUpdates`, and `setMyCommands`
   (`docs/PLAN-AUDIT-FIXES.md:241-249`). Current Telegram code also performs physical
   `sendChatAction` and `getMe` calls (`internal/channel/telegram/telegram.go:345-347,466-476`).
   The picker performs `sendMessage`, `answerCallbackQuery`, `editMessageReplyMarkup`, and
   `editMessageText` outside the durable outbox
   (`internal/channel/telegram/telegram_picker.go:22-40,43-50,156-171`). Those sites have
   no planned operation identity, target, effect classification, policy, or outcome
   landing. Changing `call` to require a grant will force compile edits, but an arbitrary
   grant added merely to compile would not establish the required owner contract.
   Detector 9d exercises only poll/control reuse and a delivery-for-poll swap
   (`docs/PLAN-AUDIT-FIXES.md:309-311`), so it can be GREEN while these siblings bypass S7.

   Concrete fix: enumerate every current `a.call` site and assign it to a governed path.
   Route user-visible durable sends through the delivery/outbox operation; give truly
   ephemeral idempotent UI calls and `getMe` explicit one-shot/control policies with
   stable resource-bound targets and terminal outcome landing. Add a table detector over
   every Bot API method that omits or reuses its grant and requires zero second wire calls.

3. **HIGH — The existing structured-output re-ask still owns retry policy outside S7,
   and v4 neither migrates it to `Execute` nor provides a detector for it.**

   Evidence: the constitutional owner rule says provider re-ask components may classify
   or propose but S7 alone owns retry (`docs/ARCHITECTURE-ESSENTIALS.md:68-75`; SPEC P0.2
   at `docs/HARNESS-SPEC.md:1386-1393`). Current `provider.Extract` decides that
   `ClassGeneral` receives a re-ask, issues the new operation itself, invokes the callback,
   and lands it (`internal/llm/provider/structured.go:102-151`). V4's `Execute` migration
   covers the planner's ordinary `Chat` operation only
   (`docs/PLAN-AUDIT-FIXES.md:263-272`); its ownership ablation likewise retains only
   planner/provider Chat code (`docs/PLAN-AUDIT-FIXES.md:295-299`). Renaming `s7min` forces
   `structured.go` to be edited, but the plan supplies no full-S7 policy or owner flow for
   this existing physical re-ask. Leaving its current `Issue` branch preserves a second
   retry-policy decision point.

   Concrete fix: represent invalid GENERAL structured output as a typed retry proposal to
   an S7-owned policy/driver (bounded to the contract's one re-ask); the extractor should
   validate and retain salvage candidates but must not decide or issue the repeat physical
   attempt. SECURITY/EFFECT classes remain terminal. Add an ownership ablation that makes
   S7 refuse the re-ask while retaining extractor/provider code and requires the re-ask
   transport count to remain zero.

## Notes (not verdict reasons)

- S7 accepting another owner's already-constructed event parameters and forwarding the
  combined batch to `Journal.AppendBatch` is not itself a journal single-writer violation.
  The substantive issue is companion binding and cardinality, not which service invokes
  the sole append actor.
- Authorization lease recovery does not create an E9 double-send path as designed:
  `STARTED` is the durable may-have-touched-wire boundary; only a lease with no STARTED
  event is revocable (`docs/PLAN-AUDIT-FIXES.md:163-174,191-201,217-225`).
- No production implementation, live provider/Telegram call, signing, publication,
  installation, restart, or paid canary was performed.

## Proof ceiling

The plan is static. The weakest remaining proof surface is the sibling class sweep: a
single guarded `call` implementation does not prove every producer acquired a correctly
bound grant, and a successful batch does not prove its companion belongs to the same
domain entity.

Skill update: none

VERDICT: FAIL

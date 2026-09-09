# Adversarial design review round 5 — audit-remediation plan v5
Reviewed revision: `3bbdf0d4f917dff3badce4941e8a5a4f44a47aac`
Verdict: FAIL

The review is bound to the committed tree above. `HEAD` matched the requested revision,
the branch was `slice/p1-audit`, and the committed plan header was `PLAN v5`. The only
pre-existing worktree entries were unrelated untracked round-5 reviews; they were not read
or modified. Slices A-F remain designs, so this is static owner-boundary analysis rather
than implementation or RED-to-GREEN proof.

## Round-4 disposition

1. **CLOSED — former finding 1 (unbound/optional atomic companion).** V5 replaces the
   variadic sibling list with `Companion{Key, Params}`, requires exactly one companion for a
   durable consume, rejects a missing or foreign key before wire, and makes `Report` and
   `Cancel` accept a landing-driven owner builder
   (`docs/PLAN-AUDIT-FIXES.md:149-169`). The channel payload validator and projection must
   independently enforce `operation_id == "delivery:"+delivery_id`
   (`docs/PLAN-AUDIT-FIXES.md:219-228`), closing the current condition in which a mark names
   only a delivery row (`internal/channel/channel.go:106-115,311-360`). Detector 9e now
   covers both omission and cross-row substitution at S7 and channel boundaries
   (`docs/PLAN-AUDIT-FIXES.md:342-345`). A distinct unpaired exhaustion path remains and is
   new finding 1 below.

2. **CLOSED — former finding 2 (Bot API sibling calls omitted from the design).** V5
   enumerates every current `call` site and assigns delivery, poll, control, or UI identity
   and policy (`docs/PLAN-AUDIT-FIXES.md:252-269`). That table covers the current physical
   calls in `internal/channel/telegram/telegram.go:249,347,392-400,463,475` and
   `internal/channel/telegram/telegram_picker.go:33,49,158,171`. Detector 9d is expanded over
   the method table and checks reuse, omission, and cross-kind substitution before the wire
   (`docs/PLAN-AUDIT-FIXES.md:338-341`). The table's poll outcome mapping is internally
   inconsistent; that is new finding 3, not the former sibling-omission defect.

3. **CLOSED AS TO OWNERSHIP — former finding 3 (the extractor directly issued its own
   re-ask).** V5 explicitly removes `Issue`/`Next` from the extractor and routes the operation
   through S7's `Execute`, leaving the extractor to validate and propose outcomes
   (`docs/PLAN-AUDIT-FIXES.md:292-301`). This removes the second grant/retry owner present in
   current code (`internal/llm/provider/structured.go:121-151`) and adds an S7 ownership
   ablation (`docs/PLAN-AUDIT-FIXES.md:346-348`). The proposed attempt count and that
   ablation are nevertheless incorrect; see new finding 2.

4. **CLOSED/CONFIRMED — round-4 journal-writer note.** S7 forwarding a typed paired batch
   remains a legitimate HARDQ B7 coordinator design: the existing journal actor is still the
   sole sequencer and transaction committer through `AppendBatch`
   (`internal/kernel/journal/journal.go:591-619`; `docs/ARCHITECTURE-ESSENTIALS.md:50-65`).
   The new defect is not who invokes that actor, but that `Next` terminalization does not use
   the paired API.

5. **CLOSED/CONFIRMED — round-4 lease/E9 note.** V5 still persists STARTED before wire and
   revokes only an authorization for which no STARTED event exists
   (`docs/PLAN-AUDIT-FIXES.md:171-182,196-209`). A rehydrated STARTED record becomes UNKNOWN
   and is never re-granted, matching E9 (`docs/ARCHITECTURE-ESSENTIALS.md:114-120`).

6. **STILL OPEN AS A PROOF CEILING — no implementation/runtime evidence.** The plan still
   marks F1/F2/F3/F5/F6/F7/F8 incomplete (`docs/PLAN-AUDIT-FIXES.md:12-18`). No production
   implementation, live provider/Telegram call, signing, publication, install, restart, or
   paid canary was performed. This is not independently a verdict reason.

## Numbered new findings

1. **HIGH — Exhaustion/deadline terminalization still commits S7 and the outbox in two
   operations, violating the plan's own paired owner boundary.**

   Evidence: `Next` is specified to durably transition the S7 operation to `FAILED` when the
   attempt cap or deadline is exhausted, then return `ErrExhausted`
   (`docs/PLAN-AUDIT-FIXES.md:142-148`). `Core.Flush` handles that return by marking the row
   `FAILED` afterward (`docs/PLAN-AUDIT-FIXES.md:229-234`). The only APIs that accept an
   atomic companion are `Consume`, `Report`, and `Cancel`; `Next` has no companion/builder
   (`docs/PLAN-AUDIT-FIXES.md:149-169`). Therefore a crash or channel-mark append failure
   after S7 commits exhaustion leaves S7 terminal `FAILED` while the durable row remains
   `PENDING`. On restart `Begin` is a no-op and no new grant is possible, so the row can be
   permanently presented as pending even though its retry owner is terminal. This is the
   same class of stranded cross-owner state the batch design is intended to eliminate. The
   current channel mark is indeed a separate one-event append
   (`internal/channel/channel.go:533-540`), while only `Journal.AppendBatch` supplies the
   all-or-none boundary (`internal/kernel/journal/journal.go:601-619`). Detector 9c covers
   Report/cancel/terminal-report pairs, not `Next` exhaustion, and detector 5 has no injected
   failure between the two commits (`docs/PLAN-AUDIT-FIXES.md:318-318,334-337`).

   Concrete fix: make durable exhaustion/deadline landing use the same mandatory companion
   transaction. Either let `Next` accept a terminal landing builder for durable operations,
   or have it return an uncommitted typed exhaustion decision that a single S7 terminalize
   API commits with the owner's `FAILED` companion. Add a cap- and deadline-exhaustion
   detector that injects failure at the former inter-append seam and proves there is no
   observable `S7=FAILED/outbox=PENDING` state.

2. **HIGH — `PolicyStructured{MaxAttempts: 2}` authorizes two additional re-asks, and
   detector 9f cannot produce its claimed RED/zero-call result.**

   Evidence: the original structured response is already supplied to `Extract` and validated
   before its re-ask callback runs (`internal/llm/provider/structured.go:102-121`). Current
   behavior deliberately permits exactly one additional callback, which is asserted by the
   existing regression (`internal/llm/provider/provider_test.go:232-249`). V5 defines
   `Execute` as `Begin -> Next -> callback -> Report -> repeat`
   (`docs/PLAN-AUDIT-FIXES.md:183-188`), but then starts that driver only for the re-ask with
   `MaxAttempts 2` (`docs/PLAN-AUDIT-FIXES.md:292-300`). If the first re-ask returns
   `invalid_general`, S7 therefore authorizes a second re-ask: the initial response is not
   one of those two attempts. Conversely, forcing this re-ask operation to `MaxAttempts 1`
   still authorizes its first callback; it cannot make the transport count zero as detector
   9f claims (`docs/PLAN-AUDIT-FIXES.md:346-348`). The detector is non-RED-capable for the
   proposed API semantics and can miss the extra physical call.

   Concrete fix: count the initial structured generation and its validation inside one S7
   `Execute` operation, so `MaxAttempts 2` means initial request plus exactly one re-ask and
   `invalid_general` is the typed proposal between them. Keep salvage outside the transport
   retry count. The ownership ablation must disable S7's second-grant path while retaining
   attempt 1, then assert exactly one total transport call; separately assert that two
   invalid responses never cause a third call. If the already-produced raw input API is
   retained instead, the re-ask operation must be one-shot (`MaxAttempts 1`) and its ablation
   must disable grant issuance rather than merely setting the same value.

3. **HIGH — The poll retry policy cannot retry two of the failures it names because its
   exact codes and outcomes disagree with the shared Telegram classifier.**

   Evidence: V5 says S7 retries only codes present in the policy and never retries
   `OutcomeUnknown` (`docs/PLAN-AUDIT-FIXES.md:161-169`). Telegram classifies a pre-wire
   failure as `transport_prewire`, while every 5xx/post-write failure becomes
   `ErrAmbiguousSend` (`docs/PLAN-AUDIT-FIXES.md:270-272`), matching the current generic
   `call` boundary (`internal/channel/telegram/telegram.go:193-232`). But `PolicyPoll` lists
   `transport`, not `transport_prewire`, and also lists `http_5xx`
   (`docs/PLAN-AUDIT-FIXES.md:414-420`). Thus a DNS/dial refusal is terminal by exact code
   mismatch, and a getUpdates 5xx lands UNKNOWN instead of proposing `http_5xx`; neither can
   reach the promised S7 retry path. This contradicts the stated transient-poll behavior,
   while detector D6 says only “transient poll failure” and can pass using 429 without
   exposing either broken branch (`docs/PLAN-AUDIT-FIXES.md:452-453`). `getUpdates` is the
   current read-only call at `internal/channel/telegram/telegram.go:243-252`, so treating its
   uncertain response as an irreversible remote effect is unnecessary; retrying from the
   same durable offset does not advance admission.

   Concrete fix: make Telegram outcome classification kind/effect-aware and align the closed
   code vocabulary exactly. `PolicyPoll` should include `transport_prewire`; read-only
   getUpdates 5xx/post-write and unreadable-response failures should propose the appropriate
   retryable poll code to S7, while effectful delivery keeps the E9 `UNKNOWN` classification.
   Expand D6 into a table over pre-wire, 429, and 5xx/read failures, asserting no call before
   `next_attempt_at` and exactly one new-grant call when due.

## Proof ceiling

This is a static plan review. The weakest link is the set of terminal paths outside
`Report`: the typed companion closes the main outcome path but does not by itself prove that
every S7 terminal transition shares the owner's durable commit. Implementation must also
demonstrate the structured-attempt accounting and kind-specific Telegram classifications
with causal ablations.

Skill update: none

VERDICT: FAIL

# Phase 5 deep review + security integration review — Kilo

Target: `slice/p0-phase5` at HEAD `f63143c` (T22 `9606515` + T23 `63f2314` + T24 `f63143c`). Both gates: deep review + security integration review. Scope: `internal/channel/`, `internal/channel/telegram/`, `internal/approval/`, `cmd/nexus/main.go` + `internal/app/daemon/daemon.go` wiring.

Evidence: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 32 packages `ok`. Ledger RED names all exist (`TestExactlyOnceAdmission`, `TestInboxRecipeAtomicUnderSigkill`, `TestOutboxRecipeAtomicUnderSigkill`, `TestDeliveryHonesty`, `TestSentButUnrecordedParksUnknown`, `TestAdmissionRequiresProfile`, `TestUnboundChatRefusedNothingAdmitted`, `TestNonTextTypedRefusalLoopAlive`, `TestReplayedUpdateNoDoubleEffect`, `TestApproveAfterRestartCompletesOnce`, `TestApprovalReplayRejected`, `TestApprovalCannotAuthorizeModifiedEffect`, `TestExpiredChallengeDead`, `TestCrossProfileChallengeInvisible`, `TestDenyClosesChallenge`).

---

## 1. [HIGH] T24 HITL suspend→approve→resume→execute is NOT wired into the production loop/effect path

`approval.Store.Suspend` (`internal/approval/approval.go:250`), `ConsumeApproval` (`:348`) and `SuspendedTurn` (`:360`) have **zero non-test callers**. The loop's `DecisionAsk`/`ErrNeedsApproval` handling does not commit `TurnSuspended`:

```go
// internal/kernel/loop/loop.go:279-284
if errors.Is(toolErr, effectpath.ErrNeedsApproval) {
    // comment claims "Durable TurnSuspended (HARDQ B6) lands with its owner task"
    return failTurn(fmt.Errorf("loop: tool %s: %w", call.ToolID, toolErr))
}
```

The claim in the comment and in the commit message ("DecisionAsk commits TurnSuspended + the loop EXITS") is not realized. Both the CLI path (`internal/app/daemon/daemon.go:147`) and the channel path (`daemon.go:208`) construct a **fresh, empty** `effectpath.NewApprovals(nil, 5*time.Minute)`, so `effectpath.go:355`'s `consumeExact` can never succeed. The durable `approval.Store` and the in-memory `effectpath.Approvals` are two unrelated mechanisms, and neither is connected to the other or to the loop.

Concrete failure: an irreversible `EffectIrreversible`/`DecisionAsk` tool from Telegram → `ErrNeedsApproval` → turn fails closed (action correctly blocked) → the owner replies `approve ch-…` → `b.approvals.Approve` → `approval: unknown challenge (fail closed)` because **nothing ever called `Suspend`**. There is no way, through the production composition root, for an approved action to actually execute. `ConsumeApproval` and `SuspendedTurn` (resume) are dead code in production.

The e2e spine RED (`cmd/nexus/main_test.go:398-411`) hides this by calling `b.approvals.Suspend(...)` and `b.approvals.ConsumeApproval(...)` **directly**, bypassing the loop and the effect path entirely. `TestApproveAfterRestartCompletesOnce` (`approval_test.go:65`) is package-local: it tests the store in isolation, not the integration the ledger's "Acceptance: irreversible action from Telegram requires approval before execution" describes.

**Judgment on the prompt's explicit question:** T24 acceptance is **not met**. The fail-closed half (an ASK tool never executes without approval) holds, but the positive half (approve → resume → execute, durable across restart) is absent from the production wiring. The loop↔approval RESUME integration is unwired; `SuspendedTurn` returns only `(turn, run, ok)` and cannot reconstruct the suspended `ToolCall` (args/idempotency key are not stored), so even a wired resume could not recompute `effectHash` for `ConsumeApproval`. This is a blocking gap in a task marked `[x]`.

## 2. [MED] Bot token leaks into error strings → capability snapshot/doctor output

Confirmed by experiment: Go's `http.Client.Do` wraps transport failures in `*url.Error` whose message embeds the full request URL, including the `bot<token>` path segment. `telegram.call` returns `fmt.Errorf("telegram: transport: %w", err)` (`telegram.go:115`), and `Adapter.Probe` stores it verbatim:

```go
// internal/channel/telegram/telegram.go:252-254
if err := a.call(ctx, "getMe", map[string]any{}, &me); err != nil {
    pr.Detail = err.Error()   // "Post \"https://api.telegram.org/bot<TOKEN>/getMe\": dial tcp ..."
    return pr
}
```

`closure.Seal` copies `pr.Detail` into `CapabilityStatus.Reason` (`internal/kernel/closure/closure.go:227`), which `runDoctor` prints (`cmd/nexus/main.go:417`) and which the T27 snapshot/attestation will surface. The known-ref redactor (C1) covers only journal bytes, never this live-probe detail. Latent today (the channel probe is not yet wired into `buildDaemon`), but the ledger (T23/T27) requires wiring it, so this is a structural leak, not a theoretical one. `PollOnce`/`FlushOutbox` errors carry the same token and are currently swallowed only by `Run`'s ignore (`telegram.go:237-238`). Fix: strip the URL from `call`'s error (redact `a.token` from any error string) before it propagates.

## 3. [MED] Crash between admission and handler permanently drops the message; RECEIVED→ADMITTED→TERMINAL is incomplete

`Admit` is the idempotency anchor; the handler (`a.handle` → `RunChannelTurn`, a full journaled turn with tool effects) runs **after** admission, not atomically with it (`telegram.go:182-207`). `StateTerminal` is declared (`channel.go:51`) but **never written** — no event transitions an inbox row to TERMINAL, and `EnqueueReply` takes no `message_id`, so the handler outcome is not durably linked to the admission. The "terminal-result+outbox" recipe (`channel.go:332`) only enqueues the outbox row.

Concrete failure: SIGKILL after `Admit` commits but before `handle` runs → on redelivery `Admit` returns `Replayed=true` → `processUpdate` returns nil (`telegram.go:189-191`) → offset advances past the update (`telegram.go:153-155`) → the admitted message is **never** handled and never answered, with no durable signal of the gap. The crash-matrix REDs assert recipe atomicity (rows==events) but not the admit→handle window, so this is unexercised. This is exactly the class of "admission ≠ effect" gap B2's honest-boundary wording was meant to close.

## 4. [MED] Two divergent `effectHash` definitions; C4 `(device,inode)` never included

`effectpath.effectHash` (`internal/kernel/effectpath/effectpath.go:121-132`) hashes `ToolID+Args+SchemaHash+Effect+Kind+ProfileID`. `approval.effectHash` (`internal/approval/approval.go:53-67`) additionally folds the **idempotency key** (`idem`). C4 (`docs/HARDQ-CONSOLIDATED.md:146`) prescribes one canonical exact-intent field set and requires the target `(device,inode)` for destructive FS ops; neither hash implements `(device,inode)`. Today the two hashes feed unrelated mechanisms so the divergence is latent, but the moment the durable store is wired into the effect path (finding #1), a call approved under one digest will not match the other. The `idem` inclusion also means a resumed turn that re-mints its idempotency key silently self-invalidates its own approval — compounding finding #1's resume gap.

## 5. [LOW] Challenge expiry is enforced only at the API layer, not the projection

`Store.decide` checks `s.clock.Now().Unix() > expires` (`approval.go:324`), but the `EvApprovalReceived`/`EvApprovalDenied` projection transitions (`approval.go:178-197`) only require `status='PENDING'` — no expiry predicate. A direct in-process append (not the channel, which is trusted) could approve an expired challenge. Defense-in-depth gap only; no remote path reaches the raw journal.

## 6. [LOW] Challenge id is a 64-bit truncation of the effect hash

`id := "ch-" + hash[:16]` (`approval.go:261`) exposes a 16-hex (64-bit) id to the human approver. Preimage/collision on the short id is not exploitable for authorization — `ConsumeApproval` requires the **full** 256-bit `effect_hash` match (`approval.go:206-212`) — but two distinct pending effects can share a short id (birthday ~2^32), making the "approve ch-…" command ambiguous at the human layer. Also note `challenge_id` is the table PK, so a genuine 64-bit collision would abort a second `Suspend` fail-closed rather than mis-authorize.

## 7. [LOW] Naive `approve `/`deny ` prefix routing; unbound-chat refusal semantics

`telegramHandler` (`cmd/nexus/main.go:462-475`) intercepts any message whose lowercase form starts with `"approve "`/`"deny "` as a HITL command, so a natural-language owner message beginning with those words can never be a conversation turn (it errors `unknown challenge`). Not a security hole (the chat is owner-bound), but a UX/correctness wrinkle. Separately, an unbound chat — or one bound to a *different* profile — still receives a `"not bound to a profile"` reply **from this adapter's profile** (`telegram.go:169-174`), which is a cross-profile send (generic content, no data leak) and misleading when the chat is in fact bound to another profile.

---

## What holds (verified, not rubber-stamped)

- **B3 physical isolation** is structurally sound: one DB per profile (`journal.Open` per profile), admission re-checks `in.Profile == c.j.Profile()` (`channel.go:307-309`), cross-profile challenge invisibility is proven both physically and at the API (`TestCrossProfileChallengeInvisible`), and the bindings map is validated + immutable after construction (`telegram.go:71-78`).
- **F2 yolo** is correctly handled: `RunChannelTurn` hardcodes `ModeDefault` (`daemon.go:208`); an adversarial `--yolo …` text is echoed as plain conversation (`main_test.go:415-429`); a DENY-classified effect stays denied regardless of channel input.
- **Exactly-once admission** is causal: `UNIQUE(adapter_id, channel_identity, update_id)` backstop (`channel.go:165`) plus fast-path and append-race paths both return the existing outcome (`channel.go:312-328`).
- **Delivery honesty** is correct: transport error keeps PENDING (retry-safe), ACCEPT+sent-mark = SENT, ACCEPT+sent-mark-failure parks UNKNOWN and `Flush` never re-sends it (`channel.go:399-426`); the residual at-least-once duplicate window (crash between remote-accept and local sent-mark) is an acknowledged honest boundary, not a defect.

Weakest overall link: the T24 HITL chain is proven only at the package and manually-wired e2e level, never through the production loop — the ledger's own "irreversible action from Telegram requires approval before execution" acceptance is not demonstrably met.

VERDICT: FAIL

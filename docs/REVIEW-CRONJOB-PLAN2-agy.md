# Design Review 2: `docs/PLAN-CRONJOB.md` (Slice `p1-cronjob`)

**Reviewer**: `agy`  
**Date**: 2026-09-08  
**Repository**: `/home/matej/HARNESS/nexus`  
**Branch**: `main` (commit `116695d4cbe6d91642ff0494d86a320480448f69`)  
**Target Document**: `docs/PLAN-CRONJOB.md` (v2)  
**References**: `docs/CRONJOB-RESEARCH.md`, `internal/obligation/obligation.go`, `internal/schedule/schedule.go`, `internal/approval/approval.go`, `internal/channel/telegram/telegram.go`, `internal/channel/channel.go`, `cmd/nexus/main.go`, `docs/ARCHITECTURE-ESSENTIALS.md`

---

## 1. Executive Summary

`docs/PLAN-CRONJOB.md` (v2) provides a complete, robust, and hardened design for the `/cronjob` inline-calendar reminder flow. Round-1 folds (F1–F4) have been incorporated and verified against the actual repository source code:
- **F1 (Obligation Pair Seam)**: The design correctly recognizes that reminders require both schedule and obligation events. It introduces a non-appending `obligation.Manager.ReminderParams(id, body, w) ([]contracts.EnvelopeParams, error)` returning `[schedP, oblP]`, verified against `internal/obligation/obligation.go:702-712`.
- **F2 (Atomic Description Commit Batch)**: Commits the reminder in a single `journal.AppendBatch` recipe `[picker.committed(sid), schedP, oblP, inbound_terminal, outbox_confirmation]`, providing atomic consistency, crash safety, single-use enforcement, and replay idempotency.
- **F3 (Strict Callback Query Validation & Ephemeral/Durable Split)**: `tgUpdate.CallbackQuery` validates `From`, `Message`, `Message.Chat.ID`, `Message.Chat.Type`, and `Message.MessageID` before acting. The private-chat and deny-default profile gates apply to taps. Best-effort ephemeral chrome (`answerCallbackQuery`, `editMessageReplyMarkup`) is cleanly separated from durable outbox confirmations.
- **F4 (Grammar & State-Machine Validation)**: Strict 5-part `pk:v1:<sid>:<act>:<arg>` grammar with per-action value and legal-in-state checks. Any deviation or illegal transition is answered benignly with no state mutation.

No substantive security, correctness, or behavioral-equivalence defects remain.

---

## 2. Verification of Round-1 Folds Against Code Sources

### 2.1 Fold F1: Reminder Pair & `Manager.ReminderParams` Seam
- **Code Anchor**: `internal/obligation/obligation.go:702-712`, `internal/schedule/schedule.go:319-333`.
- **Finding & Proof**:
  - In `obligation.go:702-712`, `CreateReminder` constructs `schedP` via `s.sched.CreatedParams(id, body, w)` and `oblP` via `m.params(EvCreated, createdPayload{ID: id, Kind: "reminder", Body: body})`, then calls `m.j.AppendBatch(ctx, []contracts.EnvelopeParams{schedP, oblP})`.
  - A bare `schedule.CreatedParams` without `obligation.created` would omit delivery tracking in the obligation manager.
  - Adding `Manager.ReminderParams` as a non-appending constructor returning `[]contracts.EnvelopeParams{schedP, oblP}` and refactoring `CreateReminder` to call it allows the picker commit to batch the reminder pair alongside its own state events without code duplication or partial commits.

### 2.2 Fold F2: Single `AppendBatch` Description Commit
- **Code Anchor**: `internal/channel/channel.go:406-444, 599-637`, `PLAN-CRONJOB.md:67-86`.
- **Finding & Proof**:
  - When the description text arrives, `channel.Inbound` is admitted via `a.core.Admit(ctx, in)`.
  - The commit transaction is executed as one `journal.AppendBatch` containing:
    1. `picker.committed(sid)`
    2. `schedP` (schedule event)
    3. `oblP` (obligation event)
    4. `channel.inbound_terminal` (marking the incoming description message TERMINAL)
    5. `channel.outbound_enqueued` (enqueuing the user-facing confirmation in `chan_outbox`)
  - **Crash / Replay Invariants**:
    - If a crash occurs before the batch commits, the message remains non-terminal and the session remains `await_description`; on restart, replay re-executes the commit cleanly.
    - If a crash occurs after the batch commits, the message is TERMINAL; on replay, the handler observes the session as already committed, short-circuiting to the existing confirmation without double-scheduling.
    - Simultaneous descriptions for the same session race on the projection CAS/unique constraint: the first wins, and the second is informed that the reminder was already set.

### 2.3 Fold F3: Callback Query Validation & Ephemeral/Durable Boundary
- **Code Anchor**: `internal/channel/telegram/telegram.go:242-285`, `docs/CRONJOB-RESEARCH.md:45-85`.
- **Finding & Proof**:
  - `tgUpdate` is extended with `CallbackQuery` containing typed sub-structs for `From`, `Message`, and `Data`.
  - Full structural validation verifies all required fields (`From`, `Message.Chat.ID`, `Message.Chat.Type`, `Message.MessageID`) exist.
  - Deny-default profile gate (`a.bindings[chat] == a.profile`) and single-user private chat gate (`Chat.Type == "private"`) execute before any picker action.
  - Ephemeral interaction chrome (`answerCallbackQuery`, `editMessageReplyMarkup`) is best-effort and unjournaled. Only user-visible confirmation messages ride the T22 outbox (`chan_outbox`), satisfying E15 and delivery honesty.

### 2.4 Fold F4: Strict Grammar & Legal-in-State Validation
- **Code Anchor**: `PLAN-CRONJOB.md:87-100`, `docs/CRONJOB-RESEARCH.md:385-422`.
- **Finding & Proof**:
  - Grammar `pk:v1:<sid>:<act>:<arg>` splits into exactly 5 colon-delimited tokens.
  - `sid` is validated against the base64/hex alphabet.
  - Each action (`d`, `h`, `m`, `nav`, `x`) is validated for syntactic bounds and validated against the session's current state (e.g. `h` is rejected if date is not yet set; `nav` is rejected outside month view).
  - Malformed or out-of-order requests receive an immediate benign answer ("Isteklo ili nevažeće") with zero journal mutations.

---

## 3. Top-3 Implementation Notes & Weakest Points

1. **Deterministic Delivery ID for Confirmation Outbox (`PLAN-CRONJOB.md:73-77`, `internal/channel/channel.go:450-483`)**:
   - *Detail*: When building the confirmation `EvOutboundEnqueued` envelope in the description commit batch, derive `DeliveryID` deterministically from the admitted message ID or session ID (e.g. `dlv-` + sha256(`"cronjob-confirm|" + sid`)).
   - *Rationale*: Guarantees that any replayed execution of the batch cannot create duplicate outbox entries.

2. **Command Interception in `await_description` (`PLAN-CRONJOB.md:67-72`, `cmd/nexus/main.go:1257-1285`)**:
   - *Detail*: In `await_description`, if the incoming message starts with `/` (e.g. `/cancel`, `/help`, `/new`), intercept it before committing the description. Specifically, `/cancel` should cancel the picker session, while other commands should abort the session and route to their normal handlers.
   - *Rationale*: Prevents accidental creation of reminders with text bodies like `"/cancel"`.

3. **Telegram "message is not modified" HTTP 400 Handling (`docs/CRONJOB-RESEARCH.md:100-107`, `internal/channel/telegram/telegram.go:315-330`)**:
   - *Detail*: In `editMessageReplyMarkup` and `editMessageText`, treat Telegram Bot API error `Bad Request: message is not modified` as a benign no-op success.
   - *Rationale*: Ensures `answerCallbackQuery` completes and clears client loading spinners even if a user repeatedly taps the same navigation button.

---

## 4. Acceptance Criteria & Detectors

The detector matrix in `PLAN-CRONJOB.md:117-140` provides comprehensive RED/GREEN verification:
- [x] Malformed / invalid action tokens rejected fail-closed.
- [x] Foreign / mismatched callback sender rejected fail-closed.
- [x] Single-use terminal commit prevents double-scheduling.
- [x] Full flow produces matching `schedule.created` and `obligation.created` pair.
- [x] Restart during interaction preserves session state in SQLite projection `picker_sessions`.
- [x] Replay of description message returns identical confirmation without duplicate side effects.
- [x] `answerCallbackQuery` guaranteed across all valid and invalid callback paths.

---

## 5. Verdict

The design in `docs/PLAN-CRONJOB.md` is complete, minimal, and fully conformant with all repository contracts and architecture essentials.

VERDICT: PASS

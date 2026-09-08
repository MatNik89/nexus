# Adversarial design review: `/cronjob` v2

Reviewed revision: `116695d4cbe6d91642ff0494d86a320480448f69` on `main`  
Tree: `d3df35563e9d610dcb76d68e1ceb9a3858f38053`

Scope: `docs/PLAN-CRONJOB.md`, checked against the cited approval, schedule, obligation, channel, Telegram, and daemon composition code, plus `docs/CRONJOB-RESEARCH.md`.

## Verdict

FAIL. Round-1 F1 and F4 are closed, and the callback field/gate portion of F3 is closed. The description and interaction ownership fixes remain unproducible against the named channel interfaces, and two restart/concurrency claims lack the mechanism needed to make them true.

## Substantive findings

### F1 — HIGH — The five-event description batch has no buildable channel-owned params seam or completion disposition

The plan requires one batch containing `picker.committed`, the reminder pair, `channel.inbound_terminal`, and the confirmation outbox event (`docs/PLAN-CRONJOB.md:67-85`). The reminder half is now buildable through the proposed obligation-owned `Manager.ReminderParams`, but the channel half is not:

- `terminalPayload`, `outboundPayload`, and `Core.params` are package-private (`internal/channel/channel.go:91-120,385-397`).
- The only public combined operation, `Core.CompleteInbound`, creates and immediately appends its own two-event batch (`internal/channel/channel.go:599-626`); it cannot contribute params to another package's five-event transaction.
- Telegram's handler returns only `(string, error)` (`internal/channel/telegram/telegram.go:51-53`). After every successful handler call, `processUpdate` unconditionally calls either `MarkInboundTerminal` or `CompleteInbound` (`internal/channel/telegram/telegram.go:308-321`). If the picker owner has already committed the promised five-event batch, either path attempts to terminalize the same inbox row again; the channel projection rejects that transition (`internal/channel/channel.go:287-299`), so `PollOnce` does not advance the Telegram offset.

This is not an implementation detail: the proposed transaction and current handler lifecycle cannot coexist.

Required correction: add a channel-owned non-appending completion seam that returns the terminal and outbox params (including a stable delivery ID), and a typed handler result/disposition that tells `processUpdate` the admitted update was already durably completed by a larger recipe. Name the one layer that assembles and appends the five-event batch. Add a detector proving that a successful description commit returns `nil` from `processUpdate`, advances on the next poll, and does not append a second terminal event.

### F2 — HIGH — “Single-use” does not enforce one open session per source, and picker opening is not replay-safe

The plan says a random opaque SID identifies each session (`docs/PLAN-CRONJOB.md:52-56`) and then concludes that source-keying plus single-use means at most one open session per source (`:68-72`). That implication is false. Single-use prevents a given SID from committing twice; it does not prevent the same `(profile, chat, user)` from opening two different random SIDs. Both can reach `await_description`, making the next message ambiguous.

The opening path has the same channel crash boundary the description path was changed to remove. `/cronjob` runs after `Admit` inside `telegramHandler`; session creation and the current channel terminal/outbox operation are not specified as one recipe. A crash after `picker.opened` but before inbound completion causes the admitted non-terminal update to rerun (`internal/channel/telegram/telegram.go:288-304`). Generating a fresh random SID then creates a second session; rejecting the second open can instead strand the already-created session without replaying its calendar.

Required correction: enforce one live session per exact source in the projection transaction (for example, one source-keyed active row or an equivalent partial uniqueness invariant), and make opening idempotent by the admitted message/update identity. Specify whether a new `/cronjob` resumes, replaces, or rejects an existing live session, and atomically tie that outcome to inbound completion and its initial-calendar delivery intent. Add detectors for two `/cronjob` updates from one source and crash-after-open-before-inbound-terminal.

### F3 — HIGH — A durable callback transition followed by a best-effort edit can permanently desynchronize state and UI

The plan makes every picker transition journaled (`docs/PLAN-CRONJOB.md:102-115`) but declares calendar edits best-effort and outside the outbox (`:46-50`). Consider a valid day tap:

1. `date_set` commits durably.
2. `editMessageReplyMarkup` fails, times out ambiguously, or the daemon crashes before it is called.
3. Telegram redelivers the same callback because `processUpdate` did not complete, or the user taps the still-visible day button again.
4. The design now classifies `d` as illegal outside the month view and returns benign “isteklo” with no state change (`:87-100`).

The durable session expects an hour tap, but the user still has the day keyboard. Neither callback replay nor daemon restart reconstructs the desired hour view. This contradicts the promised resumable interaction and can leave the only live source session unusable until expiry.

Required correction: persist a callback-update/render intent keyed by callback query/update identity and allow an idempotent replay to render the already-committed target state without reapplying the transition, or route the render through an honestly tracked delivery mechanism. A RED detector must crash after the state append but before the edit, replay the callback with a fresh store/adapter, and observe the correct next keyboard rather than “expired.”

### F4 — HIGH — The initial inline-calendar send is neither owned nor covered by the declared delivery split

The plan names the adapter as the single interaction owner holding the picker store and typed Telegram operations (`docs/PLAN-CRONJOB.md:37-45`), but assigns `/cronjob` session creation and calendar sending to `cmd/nexus/main.go`'s `telegramHandler` (`:52-56`). That handler receives `channel.Inbound`, which has no Telegram sender ID or message ID (`internal/channel/channel.go:61-68`), and returns only text. It therefore cannot record the promised chat+user `ExpectedSource` or request `sendMessage` with `reply_markup`. The adapter does have the original `From.ID` and generic Telegram call capability, but no plan-defined typed command/result seam connects it to the main handler.

The delivery classification is also internally contradictory. The first calendar must be created by `sendMessage` with inline markup and remains a Telegram chat message; it is not an edit or a transient typing indicator. Yet the plan says only the final confirmation uses the outbox and that no durable message is sent unjournaled (`docs/PLAN-CRONJOB.md:46-50`). Today's outbox carries only text (`internal/channel/channel.go:77-86`) and `FlushOutbox` has no `reply_markup` path (`internal/channel/telegram/telegram.go:324-361`).

Required correction: keep the adapter as the actual owner of command interception, source construction, and picker operations, or define a typed bidirectional handler contract carrying sender identity and a calendar response. Specify a durable/outbox-owned initial `sendMessage` representation with inline markup and stable identity, or explicitly narrow and justify the delivery invariant; direct best-effort `sendMessage` cannot satisfy the present “no unjournaled durable message” claim.

## Round-1 closure matrix

- **F1 reminder pair — CLOSED.** The code confirms that `obligation.Manager.CreateReminder` builds `schedP` plus `obligation.created` and appends both (`internal/obligation/obligation.go:700-712`). The proposed non-appending `Manager.ReminderParams` is the correct canonical reuse, and the scheduler's firing decorator expects the obligation half (`cmd/nexus/main.go:541-549`).
- **F2 description atomicity/replay — PARTIAL.** The intended five-event outcome is correct, but F1 above shows it is not expressible through the current channel API, and F2 shows opening/source arbitration is still unsafe.
- **F3 callback fields/gate — FIELD CONTRACT CLOSED; OWNERSHIP NOT CLOSED.** The proposed callback shape now contains `From`, `Message`, `Chat.ID`, `Chat.Type`, and `MessageID`; it rejects unsupported message forms and applies the private-chat/profile gate before session access (`docs/PLAN-CRONJOB.md:37-50`). The existing message gate being mirrored is exactly private-chat plus deny-default binding (`internal/channel/telegram/telegram.go:258-276`). F4 is the remaining ownership/delivery residual.
- **F4 strict callback grammar — CLOSED.** SID shape, action set, per-action ranges, current-state legality, and no-mutation rejection are now explicit (`docs/PLAN-CRONJOB.md:87-100`) and match the research's server-side validation requirement (`docs/CRONJOB-RESEARCH.md:416-422`).

## Other verified claims

- `tgUpdate` currently has only `Message`, and `processUpdate` ignores all non-message update classes (`internal/channel/telegram/telegram.go:125-141,254-257`). Adding a callback branch is structurally feasible.
- `PollOnce` advances the offset only after `processUpdate` returns successfully (`internal/channel/telegram/telegram.go:221-240`). State-append failures must therefore propagate; `answerCallbackQuery` may remain best-effort.
- Approval is a defensible state-machine template: expected source and expiry are persisted, while the projection transaction enforces pending state, exact source, and expiry (`internal/approval/approval.go:67-88,178-239`). The picker must preserve those transaction-time predicates rather than only performing pre-checks.
- `WallTime` contains the correct civil fields and verbatim IANA zone. Creation validates bounds/civil date, resolves DST once, and persists the derived instant (`internal/schedule/schedule.go:49-108,315-330`). `daemonBundle.cfg` retains the validated configured timezone (`cmd/nexus/main.go:304-316,454-461,633-636`).
- The journal really can atomically fold the proposed cross-package batch: every event and synchronous projection is applied in one SQLite transaction (`internal/kernel/journal/journal.go:500-588,601-619`). The blockers are missing params/ownership contracts, not the journal primitive.

## Notes (non-blocking)

- The mandatory callback-field list omits a non-empty `CallbackQuery.ID`, although `answerCallbackQuery` requires it. Telegram supplies the field, but validating it would make the boundary contract complete.
- The detector still says “second commit callback,” although the description message, not a callback, commits the reminder (`docs/PLAN-CRONJOB.md:67-85,122-123`).
- The plan uses `pk:v1`/`nav`, while the research example uses `pk:1` and `n`/`p`; the plan can intentionally supersede the research, but implementation and tests must choose one grammar.
- The calendar does not state whether past instants are selectable. The current scheduler accepts them and fires them as overdue on the next sweep (`internal/schedule/schedule.go:368-384`). This needs a product choice before implementation, but the owner request does not make either policy uniquely mandatory, so it is not a FAIL reason.

## Upfront guidance for Claude

Before the next revision, write three executable contract sketches before the prose: (1) the exact public params producers and typed handler disposition for each `AppendBatch`; (2) the projection constraint that proves every claimed uniqueness statement; and (3) the replay table for crashes immediately before and after every local append and Telegram call. Also classify the initial `sendMessage` separately from subsequent edits. Those checks would have caught every residual above before review.

## Verification and proof ceiling

- The requested commit, branch, tree, and scoped paths were verified; inspection and tests used a clean on-disk export.
- Focused baseline command: `/home/matej/.local/go/bin/go test ./internal/approval ./internal/schedule ./internal/channel ./internal/channel/telegram ./internal/obligation ./cmd/nexus`. All six packages passed. These are current-code baselines; no cronjob implementation exists, so they do not prove the future detectors.
- This is a static design review. It proves interface and failure-sequence contradictions at the immutable revision, not behavior of a future implementation.

VERDICT: FAIL

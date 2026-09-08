# Adversarial design review: `/cronjob`

Reviewed commit: `13308eba344bcf563ff4b4bfce7e70eff7949c46` (`main`)

Scope: `docs/PLAN-CRONJOB.md`, checked jointly against the cited Telegram, channel, approval, schedule, obligation, and daemon wiring code, plus `docs/CRONJOB-RESEARCH.md`.

## Verdict

FAIL. The durable picker concept and use of `schedule.WallTime` are sound, but the proposed composition does not produce a working reminder on the current backend, and the described Telegram/handler boundary cannot implement its own authorization, same-message edits, or crash-safe description handoff.

## Substantive findings

### F1 — HIGH — The plan creates only a schedule, not the obligation required for delivery

`PLAN-CRONJOB.md:18-21,52-57` says to call `Scheduler.CreatedParams` and claims that the scheduler already fires and delivers. `CreatedParams` deliberately creates only a `schedule.created` envelope; its comment says the intended composition is `[schedule.created, obligation.created]` (`internal/schedule/schedule.go:315-330`). The actual reminder owner is `obligation.Manager.CreateReminder`, which appends both events atomically (`internal/obligation/obligation.go:700-712`). The daemon also decorates every schedule firing through `oblManager.FireParams` (`cmd/nexus/main.go:541-549`), and the obligation projection fails closed when an occurrence has no corresponding obligation (`internal/obligation/obligation.go:330-350`).

Consequently, the proposed recipe can persist a schedule that cannot traverse the configured delivery path. At firing time the decorated batch has no obligation to advance and aborts instead of delivering the Telegram reminder.

Required design correction: compose the picker terminal event with the canonical reminder pair produced by the obligation owner. Because `Manager.CreateReminder` currently appends internally, define a non-appending obligation-owned params seam (or another single canonical recipe owner) that returns both schedule and obligation envelopes, then append those together with the picker commit. Do not duplicate obligation payload construction in the picker.

### F2 — HIGH — Description capture has neither unambiguous ownership nor a replay-safe channel outcome

The plan says that the next text from the source is captured, the picker and schedule are committed in one recipe, and otherwise the text follows the normal path (`PLAN-CRONJOB.md:52-58`). It does not define what happens when more than one session for the same `(profile, chat, user)` reaches `await_description`; one message then has multiple valid consumers. It must either enforce at most one such session atomically or correlate the description to a specific picker prompt/session (for example through a reply-to contract).

There is also a concrete crash window against today's channel flow. `processUpdate` first admits the text, invokes the handler, and only afterwards calls `CompleteInbound` (`internal/channel/telegram/telegram.go:284-320`). `CompleteInbound` is separately responsible for atomically recording the inbound terminal state and confirmation outbox row (`internal/channel/channel.go:599-626`). If the proposed picker/reminder commit succeeds inside `telegramHandler` but the process crashes before `CompleteInbound`, redelivery finds a non-terminal admitted inbox row and deliberately reruns the handler (`internal/channel/telegram/telegram.go:292-304`). The picker is now committed, so the plan's “no active session” rule routes the same description as an ordinary assistant turn. The confirmation can also be lost or replaced by that ordinary reply.

Required design correction: specify one durable routing/arbitration rule and replay outcome keyed to the admitted Telegram update. Either expose params that allow picker claim + canonical reminder pair + inbound terminal + confirmation outbox to share one journal batch, or durably record that this update committed this session and make every non-terminal replay return the same canonical confirmation without reinterpreting the text. Include simultaneous-description and crash-after-reminder-before-channel-terminal detectors.

### F3 — HIGH — The proposed callback wire and existing handler boundary cannot perform the promised gate or UI operations

The proposed `CallbackQuery.Message` contains only `Chat.ID` (`PLAN-CRONJOB.md:30-35`). It omits:

- `Message.MessageID`, although every redraw promises to edit the same message (`PLAN-CRONJOB.md:43-50`);
- `Message.Chat.Type`, although the design promises to apply the existing private-chat gate before acting;
- a defined fail-closed branch for an absent/inaccessible `message` or an inline callback, despite the research recording `message` as optional (`docs/CRONJOB-RESEARCH.md:54-62`).

The broader interface topology is also missing. The Telegram `Handler` accepts only `channel.Inbound` and returns only a string (`internal/channel/telegram/telegram.go:51-53`). `Inbound` carries neither sender ID nor message/callback ID (`internal/channel/channel.go:61-68`); `Outbound` carries only text (`internal/channel/channel.go:77-86`); and `FlushOutbox` emits text methods without `reply_markup` (`internal/channel/telegram/telegram.go:324-361`). Yet the plan assigns session creation and initial keyboard rendering to `cmd/nexus/main.go` while assigning callback routing to the adapter. The adapter has no picker/scheduler dependency (`internal/channel/telegram/telegram.go:55-69`), and the main handler has no typed way to request `sendMessage` with an inline keyboard or Telegram edit/answer operations.

The generic internal `Adapter.call` can technically invoke `answerCallbackQuery` and edit methods, and its existing HTTP client preserves the Telegram egress boundary; that makes the wire operations feasible in principle. It does not make the described ownership/interfaces buildable.

Required design correction: name one owner for the picker interaction and define the minimal typed seams. The callback shape must include and validate `From`, `Message`, `Message.Chat.ID`, `Message.Chat.Type`, and `Message.MessageID`, rejecting unsupported inline/inaccessible forms without state change. The same owner needs durable picker access and typed Telegram operations for initial `reply_markup`, edit, and answer. Preserve channel admission/outbox honesty rather than bypassing it with unjournaled user-visible sends.

### F4 — MEDIUM — “Numeric/short” is not a strict per-action callback grammar

The callback is attacker-controlled, but the plan validates `arg` only as “numeric/short” (`PLAN-CRONJOB.md:60-64`). That accepts values the UI never offers: for example minute `17` passes `WallTime` validation even though the promised grid is `{00,15,30,45}`. It also does not require a day/month argument to match the session's currently rendered view or define legal state transitions for each action. The research explicitly requires per-action parsing and session-range validation (`docs/CRONJOB-RESEARCH.md:416-422`).

Required design correction: specify closed validation per action and current state: exact SID alphabet/length; exact date/month syntax and allowed range/view; hour `0..23`; minute exactly `{0,15,30,45}`; navigation as a bounded one-step transition; cancel with an empty argument; and rejection of actions illegal in the current session state. Malformed callbacks must still be answered benignly with no journal mutation.

## Claims that do hold

- A journal projection modeled on approval is a defensible durability pattern. Approval persists expiry and expected source and rejects non-pending or expired decisions (`internal/approval/approval.go:67-88,143-190,448-467`). The picker still needs its own transition/CAS contract; merely citing approval does not provide it.
- `schedule.WallTime{Year, Month, Day, Hour, Minute, TZ}` is the correct type. The scheduler validates civil dates and IANA zones and resolves DST behavior at creation (`internal/schedule/schedule.go:49-108,319-330`). The configured timezone must be copied verbatim into `TZ`.
- Telegram currently ignores non-message updates exactly as claimed (`internal/channel/telegram/telegram.go:254-257`), and `PollOnce` advances its offset only after `processUpdate` succeeds (`internal/channel/telegram/telegram.go:221-240`). A future callback handler must return persistence failures so the offset does not advance; `answerCallbackQuery` alone may remain best-effort.
- Binding taps to both chat and `callback_query.from.id`, applying the private-chat/profile gate before lookup or mutation, using a TTL, and making terminal consumption single-use are appropriate controls. The present message gate is private-chat plus deny-default profile binding (`internal/channel/telegram/telegram.go:258-276`), but its required fields and source identity are not yet available on the proposed callback/handler path.

## Notes (non-blocking)

- The plan grammar uses `pk:v1` and action `nav`, whereas the research grammar uses `pk:1` and actions `n`/`p` (and includes `i`) (`PLAN-CRONJOB.md:60-64`; `CRONJOB-RESEARCH.md:391-409`). Choose one canonical grammar before writing detectors.
- “Second commit callback” is inaccurate because the minute callback only enters `await_description`; the description message performs the commit (`PLAN-CRONJOB.md:48-57,86-87`). Test duplicate terminal description gestures and callback replay separately.
- The current research grammar says `arg := 0*6` but its date/month examples require 10/7 bytes (`CRONJOB-RESEARCH.md:399-405`). This is editorial unless that ABNF is promoted as the implementation contract.

## Upfront guidance for Claude on the next design

Before sending the revision to reviewers, trace one complete event through its durable owners: Telegram update -> channel admission -> picker transition -> obligation+schedule creation -> channel terminal+outbox -> scheduler fire -> obligation delivery. For every claimed atomic step, list the exact existing `EnvelopeParams` producers and the single `AppendBatch` owner. For every Telegram method, list all required wire identifiers and which layer owns them. Finally, write the crash replay for each boundary and the arbitration rule for two active pickers before writing happy-path prose. This would have exposed F1-F3 at the start.

## Verification and proof ceiling

- Repository identity was verified at the requested commit and branch; review reads were performed from a clean on-disk export.
- No code, branch, or runtime state was changed. This report is the only created artifact.
- Focused Go tests could not run because this environment has no `go` executable (`zsh: command not found: go`). That is a tooling proof ceiling, not a verdict reason. The blockers above are static contradictions between the proposed design and committed interfaces.

VERDICT: FAIL

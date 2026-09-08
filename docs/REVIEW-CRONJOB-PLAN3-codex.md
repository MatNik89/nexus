# Adversarial design review: `/cronjob` v3

Reviewed revision: `8216aae5fb8a880c5a292966a1fc86cfdeb0e285` on `main`  
Tree: `f2cc70260b5c39e2422e996ccbbcecc998cd624e`

Scope: `docs/PLAN-CRONJOB.md`, checked against the named approval, schedule, obligation, channel, Telegram, and daemon code and `docs/CRONJOB-RESEARCH.md`. The owner-selected ephemeral picker boundary is accepted as a ceiling, not reviewed as a durability defect.

## Verdict

FAIL. There is one substantive residual in the durable path: a restart in the exact `CreateReminder`-to-`CompleteInbound` window loses the in-memory routing evidence before the proposed idempotency lookup can be reached, so the confirmation is not durable as claimed.

## Substantive finding

### F1 — HIGH — The deterministic reminder ID does not make the confirmation replay-safe because the lookup is behind the lost session gate

The plan's handler order is explicit: first ask whether the source owns an `await_description` in-memory session; only inside that branch derive the deterministic reminder ID, check the projection, create or skip, return the confirmation, and clear the session (`docs/PLAN-CRONJOB.md:65-78`). It then claims that a non-terminal replay repeats the idempotent lookup and returns the same confirmation (`:79-81`). That claim does not hold across the stated durability boundary.

Concrete reachable sequence:

1. A valid description update is admitted and the handler finds its live session.
2. `obligation.Manager.CreateReminder` commits both `schedule.created` and `obligation.created` atomically (`internal/obligation/obligation.go:700-712`).
3. The daemon dies before `processUpdate` calls `Core.CompleteInbound`, or `CompleteInbound` fails and the handler has already cleared the session.
4. The inbox row remains `ADMITTED`; on redelivery, `Admit` returns the existing non-terminal outcome and `processUpdate` reruns the handler (`internal/channel/channel.go:406-444`; `internal/channel/telegram/telegram.go:288-304`).
5. After restart the picker session is deliberately absent (`docs/PLAN-CRONJOB.md:11-19`). The handler therefore takes the plan's “NO active session -> normal turn” branch (`:67-68,78`) before deriving/checking the reminder ID.
6. The already-created reminder survives, but no terminal+outbox confirmation is produced for it; the description can instead be interpreted as a normal assistant turn. This violates the explicit durable-confirmation promise (`:12,32-34,75-81`).

The deterministic ID prevents a second reminder only when the existing-reminder lookup is actually reached. It cannot itself preserve the lost routing decision.

Required correction: at the start of handling every admitted Telegram text, derive the deterministic reminder ID from `(adapter, channel identity, update ID)` and perform the durable lookup **before** consulting ephemeral picker state. If the exact reminder exists, return a confirmation reconstructed from its persisted data; only a true not-found proceeds to session/normal-turn routing. The lookup should be an obligation-owned typed method that distinguishes not-found from storage failure and returns enough persisted intent (or a canonical persisted confirmation) to reproduce the reply. An existing ID with mismatched body/WallTime must fail closed rather than silently treating a different intent as a replay.

The planned idempotency detector is insufficiently fresh: “run the description handler twice” can retain process memory and miss the stated restart failure. The required detector must:

- admit a description and commit `CreateReminder`;
- stop before `CompleteInbound`;
- construct a fresh adapter/handler with an empty picker map against the same journal;
- replay the same Telegram update;
- observe exactly one reminder pair, the same durable confirmation outbox row, a terminal inbox row, and no normal conversation turn.

## Verified durable-path claims

- **Reminder pair — correct.** `Manager.CreateReminder` obtains the validated schedule params and obligation event and appends them in one batch (`internal/obligation/obligation.go:700-712`). The daemon's fire decorator expects that obligation half (`cmd/nexus/main.go:541-549`).
- **WallTime — correct.** The selected civil fields plus configured IANA timezone match `schedule.WallTime`; creation validates field bounds/civil dates and resolves the due instant once (`internal/schedule/schedule.go:49-108,315-330`). The validated configured timezone remains available through `daemonBundle.cfg` (`cmd/nexus/main.go:303-316,454-461,633-636`).
- **Confirmation recipe — correct once the handler returns it.** `CompleteInbound` commits inbound terminal state and the outbox row in one batch (`internal/channel/channel.go:599-626`). A terminal replay is skipped (`internal/channel/telegram/telegram.go:292-303`), leaving the durable outbox to finish delivery.
- **Projection lookup — feasible but needs a narrow typed seam.** `obligation.Manager.Status` already proves that obligation existence is queryable (`internal/obligation/obligation.go:1071-1086`), and the schedule projection stores body, serialized WallTime, and due instant (`internal/schedule/schedule.go:177-221`). There is no current public reminder getter returning those values, but adding one is a small buildable interface change.

## Verified routing and callback claims

- **One live picker per source — defensible within the selected scope.** An adapter-owned map keyed by exact `(chat ID, user ID)`, with replacement on `/cronjob` and TTL checks on access, removes the prior multi-session ambiguity. Since the production adapter is the long-poll owner, access can be serialized there; the map must still be mutex-protected if `processUpdate` remains callable concurrently in tests or future wiring.
- **Wrong-source capture — prevented by the stated owner key.** The adapter still has `Message.From.ID` and `Message.Chat.ID` before constructing `channel.Inbound` (`internal/channel/telegram/telegram.go:125-140,258-286`), so description interception must remain adapter-owned; moving the check into today's `telegramHandler` would lose the user ID because `channel.Inbound` does not carry it (`internal/channel/channel.go:61-68`).
- **Callback gate — correct and buildable.** The proposed wire shape includes the message and tapper fields needed for private-chat, bound-profile, and exact-source checks. Routing callbacks before today's `Message == nil` return is a localized adapter change (`internal/channel/telegram/telegram.go:125-141,254-276`). Unsupported/missing message forms are rejected before state access, consistent with the research's optional `CallbackQuery.message` and authoritative `from` (`docs/CRONJOB-RESEARCH.md:49-68`).
- **Callback grammar — correct.** Fixed field count, fixed SID shape, closed actions, per-action bounds, legal-state checks, TTL, and owner binding cover the research's untrusted-data requirements (`docs/PLAN-CRONJOB.md:83-95`; `docs/CRONJOB-RESEARCH.md:244-312,416-422`).

## Accepted ceilings and notes

- **[BY-DESIGN] Picker restart loss.** Losing an unfinished picker, losing an intermediate edit, or requiring `/cronjob` again after a crash is the explicit owner-selected ceiling (`docs/PLAN-CRONJOB.md:11-19,57-63,113-115`). None is a FAIL reason. The finding above begins only after the durable reminder pair has committed.
- **[BY-DESIGN] Calendar transport.** Initial calendar sends, edits, and callback acknowledgements are explicitly best-effort interaction chrome; only the final confirmation is a durable outbox delivery (`docs/PLAN-CRONJOB.md:37-55`). This is honest as long as none of those calls claims that the reminder was durably created. The confirmation must be emitted only after `CreateReminder` succeeds or the exact replay lookup succeeds.
- **[NOTE] Ownership wording.** The plan says `telegramHandler` handles `/cronjob` while also assigning the source-keyed session to the adapter. The buildable version is adapter interception around the admitted text path, because only the adapter retains `From.ID`; keep the normal handler solely as the fallback normal-turn handler.
- **[NOTE] ID encoding.** Pin the hash encoding/truncation. Obligation IDs are limited to 64 characters from `[A-Za-z0-9_-]` (`internal/obligation/obligation.go:715-729`); for example, `rem-` plus full hexadecimal SHA-256 would be 68 characters and be rejected.
- **[NOTE] Callback query ID.** Validate non-empty `CallbackQuery.ID` before attempting the mandatory `answerCallbackQuery`; the research identifies it as the required acknowledgement key (`docs/CRONJOB-RESEARCH.md:54-73`).
- **[NOTE] Past instants.** The plan does not choose whether past calendar values are selectable. The current scheduler accepts and fires them as overdue on its next sweep (`internal/schedule/schedule.go:368-384`). This is a product choice, not a defect under the stated request.

## Upfront guidance for Claude

For any retry/idempotency claim, write the fresh-process replay path before the happy path: list which routing facts survive, then place the durable lookup before every ephemeral gate. Also make the detector reconstruct every in-memory owner from empty state. That would have exposed this residual immediately; calling a handler twice in one process is not proof of restart idempotency.

## Verification and proof ceiling

- The requested commit, branch, tree, and scoped paths were verified; review and tests used a clean on-disk export.
- Focused baseline passed: `/home/matej/.local/go/bin/go test ./internal/approval ./internal/schedule ./internal/channel ./internal/channel/telegram ./internal/obligation ./cmd/nexus`.
- No cronjob implementation exists at this revision. The review proves a static replay contradiction in the design; it does not prove future implementation or RED-detector behavior.

VERDICT: FAIL

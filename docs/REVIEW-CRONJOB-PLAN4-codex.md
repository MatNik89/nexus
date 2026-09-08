# Adversarial design review: `/cronjob` v4

Reviewed revision: `acbad82d20065c33a2a6510b26cdc54cfab334b9` on `main`  
Tree: `d62202fb7464a0df5ab42b6b9e21ca350cf0e0aa`

Scope: `docs/PLAN-CRONJOB.md`, checked against the named approval, schedule, obligation, channel, Telegram, and daemon code and `docs/CRONJOB-RESEARCH.md`. The owner-selected ephemeral picker boundary is treated as an accepted ceiling.

## Verdict

PASS. The round-3 durable replay defect is closed. No new substantive defect was found in reminder correctness/idempotency, confirmation admission/outbox honesty, description ownership, or callback authorization/validation.

## Round-3 F1 — CLOSED

The revised order at `docs/PLAN-CRONJOB.md:65-95` repairs the exact crash sequence from round 3:

1. Every admitted text first derives a reminder ID from the stable inbound identity `(adapter, channel identity, update ID)` (`:69-71`). Those fields already exist in `channel.Inbound` (`internal/channel/channel.go:61-68`).
2. `Manager.ReminderIntent` is consulted before any ephemeral session gate and has closed `NotFound`, `Found`, and `StorageError` outcomes (`docs/PLAN-CRONJOB.md:72-81`). `Found` reconstructs the confirmation from persisted intent rather than process memory.
3. Only `NotFound` can enter description-versus-normal routing (`:83-95`). A live, exactly source-bound `await_description` session creates the reminder; no session means today's normal path.
4. `Manager.CreateReminder` atomically appends both `schedule.created` and `obligation.created` (`internal/obligation/obligation.go:700-712`). The schedule projection rejects duplicate IDs (`internal/schedule/schedule.go:201-221`), so the durable identity is unique.
5. If the daemon dies after reminder creation but before inbound completion, `Core.Admit` finds the existing non-terminal inbox row and `processUpdate` reruns the handler (`internal/channel/channel.go:406-444`; `internal/channel/telegram/telegram.go:288-304`). The first-step lookup now returns `Found` even though the in-memory picker is gone, so the handler returns the persisted confirmation instead of invoking the normal assistant path.
6. `Core.CompleteInbound` then commits the inbound terminal state and confirmation outbox row in one batch (`internal/channel/channel.go:599-626`). A later terminal replay is skipped (`internal/channel/telegram/telegram.go:292-303`), while the outbox remains durable for delivery.

The new fresh-process detector is correctly targeted: it explicitly drops the in-memory session before replay and asserts one reminder pair, a confirmation, and no normal turn (`docs/PLAN-CRONJOB.md:118-125`). It would fail if the durable lookup moved behind the session gate. The separate storage-error detector prevents a lookup failure from becoming an ordinary turn.

## Buildability of `ReminderIntent`

The proposed lookup is buildable without duplicating reminder ownership:

- `obligation.Manager` already owns the journal and exposes guarded projection reads; `Manager.Status` demonstrates the pattern (`internal/obligation/obligation.go:642-668,1071-1086`).
- `sched_schedules` persists the body, serialized `WallTime`, and resolved instant (`internal/schedule/schedule.go:177-221`).
- `obl_obligations` persists the matching reminder ID, kind, body, state, and occurrence identity (`internal/obligation/obligation.go:302-320,363-382`).
- Because `CreateReminder` creates the schedule and obligation in one `AppendBatch`, a successful canonical create cannot expose only half the pair (`internal/obligation/obligation.go:700-712`). `ReminderIntent` should nevertheless treat a missing/malformed half as `StorageError`, not `NotFound`.

The lookup result is typed and distinguishes absence from failure, satisfying the fail-closed boundary. Returning persisted body+wall is sufficient to reproduce the claimed confirmation without retaining picker state.

## Description routing

The routing is sound within the chosen scope:

- The adapter receives both `Message.Chat.ID` and `Message.From.ID` before normalizing an inbound message (`internal/channel/telegram/telegram.go:125-140,258-286`), so it can key the ephemeral session by the exact source.
- Replacing the existing entry on a second `/cronjob` enforces one live session per source and invalidates the old SID (`docs/PLAN-CRONJOB.md:50-55,131-132`).
- Callback and description matching use the same chat+user source, preventing a different chat or tapper from controlling/capturing another session.
- The durable-first lookup is keyed by the admitted update identity, not by the ephemeral SID. Therefore clearing, replacing, expiring, or losing a picker cannot cause an already-created reminder's description replay to enter the normal path.

## Callback trust boundary

The callback design remains defensible:

- The proposed wire shape includes the tapper, message, chat ID/type, message ID, data, and callback ID needed by the flow (`docs/PLAN-CRONJOB.md:37-44`). Unsupported/missing message forms are rejected without state change, matching the research's optional `CallbackQuery.message` rule (`docs/CRONJOB-RESEARCH.md:49-68`).
- The private-chat and deny-default profile gate runs before picker access, mirroring the existing message gate (`internal/channel/telegram/telegram.go:258-276`).
- `callback_data` is treated as hostile: exact five-field parse, fixed SID shape, closed action set, per-action ranges, current-state legality, TTL, and exact source binding (`docs/PLAN-CRONJOB.md:97-109`). This covers the research's arbitrary-data, strict-parse, owner-binding, and stale/replay patterns (`docs/CRONJOB-RESEARCH.md:244-312,416-422`).
- Adding a callback branch before today's `Message == nil` return is localized and buildable; the adapter's existing generic `call` method can invoke `answerCallbackQuery`, `sendMessage`, and edit methods through the already-governed Telegram client (`internal/channel/telegram/telegram.go:171-218,254-257`).

## Delivery-honesty boundary

The split is explicit and honest under the owner decision:

- Calendar sends, edits, and callback acknowledgements are labelled best-effort interaction chrome and never claim that a reminder was created (`docs/PLAN-CRONJOB.md:37-63`). Their loss requires re-running `/cronjob`, which is the accepted ceiling.
- The only success statement about durable reminder creation is returned after `CreateReminder` succeeds or `ReminderIntent` proves the exact reminder already exists (`:65-95`). That statement then goes through the existing terminal+outbox recipe.
- No calendar transport outcome is used as evidence of reminder creation. Conversely, no successful reminder confirmation bypasses the outbox.

## Notes and weakest points (non-blocking)

1. **ID encoding:** specify the hash encoding/truncation. Obligation IDs must be at most 64 characters and use only `[A-Za-z0-9_-]` (`internal/obligation/obligation.go:715-729`); `rem-` plus full hexadecimal SHA-256 would be 68 characters.
2. **Typed lookup invariant:** define `Found` as a matching schedule+reminder pair and return `StorageError` for a partial row, invalid serialized WallTime, unknown result enum, or query failure. If the same ID is presented with an available live session whose body/wall differs, the stated mismatch refusal must remain fail-closed.
3. **Owner wording:** `telegramHandler` cannot itself key by user because `channel.Inbound` omits `From.ID`. The buildable implementation is adapter interception around the admitted-text handler, with the ordinary handler retained as fallback; document the exact small seam when coding.
4. **Storage-error terminal behavior:** today's `processUpdate` converts a handler error into a durable generic failure reply and terminal inbox state (`internal/channel/telegram/telegram.go:308-320`). That is fail-closed and does not falsely claim reminder success, but it is not automatic retry. If retry-on-transient-storage-error is desired, it needs a typed handler disposition; it is not required by this plan.
5. **Ephemeral edit recovery:** the sentence “the next tap re-renders” is not guaranteed when a transition committed in memory but the corresponding edit failed; the still-visible old action may now be illegal in the new state. This affects only the explicitly ephemeral picker and is covered by re-running `/cronjob`, so it is a NOTE rather than a FAIL reason.
6. **Past instants:** the plan does not choose whether past values are selectable. The scheduler accepts and fires them as overdue on the next sweep (`internal/schedule/schedule.go:368-384`). This remains a product choice, not a correctness defect under the current ask.

## Upfront guidance for Claude

Keep the durable lookup before every ephemeral routing decision, and make the fresh-process replay detector rebuild the adapter with an empty session map. During implementation, pin the reminder-ID encoding and define `ReminderIntent` as a strict pair lookup with explicit not-found/error semantics. Those two details are the likeliest places for correct prose to become incorrect code.

## Verification and proof ceiling

- The requested commit, branch, tree, and scoped paths were verified; inspection and tests used a clean on-disk export.
- Focused baseline passed: `/home/matej/.local/go/bin/go test ./internal/approval ./internal/schedule ./internal/channel ./internal/channel/telegram ./internal/obligation ./cmd/nexus`.
- This is a static design review. It validates that the design is producible against current interfaces but cannot prove the future implementation or its RED-to-GREEN detectors.

VERDICT: PASS

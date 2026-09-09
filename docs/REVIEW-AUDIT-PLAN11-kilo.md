# Review of audit-remediation plan v11 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `cf3790d` on `slice/p1-audit`
- **Verdict**: PASS

## Round-10 disposition

- **Round-10 note (`Reconcile` `ok bool` proof is sufficient)** — still valid; v11 does
  not touch the reconciliation proof shape (the adapter remains the trusted owner of
  the `getMyCommands`-vs-desired comparison). Not a finding.

## New findings

None substantive. The codex round-10 finding and note are folded and verified:

- **Cross-bot false-success (codex r10 #1) — closed.** The durable registration
  identity now binds the REMOTE BOT: `control:tg:<bot-id>:setMyCommands:<sha256 of
  the exact canonical wire payload>` (ordered array, never sorted — `:317-318`),
  target `channel:tg:bot:<bot-id>:setMyCommands` (`:319`), bot id obtained from the
  governed startup `getMe` (`:320-321`). The companion gains `bot_id`
  (`channel.control_effect{operation_id, adapter, bot_id, method, payload_hash,
  state}`, `:323,334`), and the validator binds
  `operation_id == "control:" + adapter + ":" + bot_id + ":" + method + ":" +
  payload_hash` with closed method/state sets (`:335-338`), so a foreign hash OR a
  foreign bot is refused at the journal boundary before the wire. A replacement token
  (bot B) is a DIFFERENT durable operation even for identical commands.
- **Sorted-vs-ordered hash (codex r10 note) — closed.** `:317-318` explicitly hashes
  the ordered command array as sent, never sorted away.
- **Detector D2c (`:572-576`)** proves bot rotation: bot A SUCCEEDED → same-bot
  restart zero calls; restart with bot B and the SAME command set → exactly ONE
  governed `setMyCommands` for B; ablating only the bot binding turns it RED; a bot-A
  companion presented for bot B is refused before the wire.

## Note (not a FAIL reason)

- The `bot_id` is a Telegram-issued numeric string; the plan does not state a
  sanitization/format guard on it, but a malformed `getMe` reply is retryable under
  `PolicyControlRead` (a read never lands UNKNOWN), so a non-numeric bot id cannot be
  observed in practice. Worth a one-line "numeric" validation comment at the parse
  site, not a defect.

VERDICT: PASS

# Review of audit-remediation plan v12 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `5358518` on `slice/p1-audit`
- **Verdict**: PASS

## Round-11 disposition

- **Round-11 note (`bot_id` numeric validation)** — still open as a note; v12 does
  not add a numeric-format guard (a malformed `getMe` is retryable under
  `PolicyControlRead`, so a non-numeric id is not observable in practice). Not a
  finding.

## New findings

None substantive. The codex round-11 finding and the stale-row note are folded:

- **Reconciliation evidence not bot-bound (codex r11 #1) — closed.** The
  reconciliation read is now bot-bound (`control:tg:<bot-id>:getMyCommands:<n>` /
  `channel:tg:bot:<bot-id>:getMyCommands`, `:302,329`), runs ONLY for an UNKNOWN
  registration whose embedded bot id equals the current governed `getMe` bot id
  (`:328-334`), and the channel owner verifies the typed
  `channel.ControlProof{BotID, PayloadHash, RemoteEqual}` against the operation's
  bot id + hash BEFORE reducing it to the boolean for `s7.Reconcile` (`:334-337`).
  An old bot's UNKNOWN stays UNKNOWN. Detector D2d (`:586-591`) proves provenance:
  bot A UNKNOWN → bot B restart with an equal menu → A stays UNKNOWN with no
  Reconcile, B performs exactly one governed `setMyCommands`, a cross-bot
  ControlProof is refused, and ablating only the proof-to-bot check turns RED.
- **Stale call-site table rows (codex r11 note 1) — closed.** `getMyCommands` and
  `setMyCommands` both now carry the bot-bound operation and target (`:302-303`).

## Note (not a FAIL reason)

- The D2b wording at `:573` (and the B2 owner paragraph at `:340`) still says
  "rehydrated SUCCEEDED registration for the same **hash** → zero calls", rather
  than "same bot ID and payload hash". This is the codex r11 note #2 item, which
  that review itself classified as wording rather than a substantive finding; the
  behavior is correct because the identity is bot-bound and D2c/D2d supply the
  bot-distinction. A one-word touch-up remains.

VERDICT: PASS

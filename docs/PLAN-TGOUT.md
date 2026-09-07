# PLAN: Telegram output quality (slice tg-output)

Two dogfood findings from the owner's live chat (2026-09-07), verbatim:
1. "popravi ovo da lijepo vidim a ne da hijeroglife vidim na telegramu"
   — the bot delivered a RAW tool-call JSON as a chat message.
2. "hermes-agent ima skill ili neki dodatak, pogledaj i ugradi" — tables
   and formatting render as ugly flat text in Telegram.

## Diagnosed causes (verified read-only)

A. **Tool-JSON leak.** The journal holds a delivered final of exactly
   `{"action":"memory_recall","query":"prva poruka danas"}`. The model
   hallucinated a SIMPLER schema than ours (`action:"<tool name>"`
   instead of `action:"tool",tool_id:...`). `toolCallFromReply`
   correctly rejects it as not-a-tool-call — and then the raw JSON is
   delivered to the user as the final. Any model-side schema drift
   becomes user-visible garbage.

B. **No formatting at all.** `telegram.go` FlushOutbox sends
   `{chat_id, text}` with no `parse_mode`; Telegram renders plain text,
   markdown tables become left-aligned noise.

## Stolen patterns (references — READ FROM THE OWNER'S LOCAL
## hermes-agent INSTALL at ~/.hermes/hermes-agent, per owner order)

- Tables: `gateway/platforms/helpers.py:convert_table_to_bullets` (line
  379) + `_render_table_block` (line 330): EVERY GFM pipe table becomes
  bold-heading + bullet row groups — heading = the row-label cell (or
  first non-empty cell), remaining cells as "• Header: value" bullets;
  tables inside code fences untouched. No aligned-pre branch at all —
  bullets always. We adopt exactly this.
- Formatting: `plugins/platforms/telegram/adapter.py:format_message`
  (line 8302): markdown -> MarkdownV2 with code blocks stashed behind
  placeholders, everything else escaped.
- Fallback: on Telegram BadRequest the adapter re-sends PLAIN via
  `_strip_mdv2` (line 475) — formatting can fail, delivery cannot.
- karfly/chatgpt_telegram_bot: malformed model output is never shipped
  raw; retry-with-correction is standard.

DELIBERATE DEVIATION (topknot): hermes uses MarkdownV2, which needs
18-character escaping plus placeholder machinery. We keep hermes'
SEMANTICS (tables->bullets, plain fallback) but use parse_mode=HTML —
only 3 escapes (& < >), no placeholder engine, identical rendered
result. Reviewers: challenge this if HTML mode has a hole we missed.

## Proposed changes

### A. Planner: no tool-shaped JSON ever reaches the user (v3)
NO retry (S7 owns retries; unchanged from v2). Additions from round 2:
- FENCE-TOLERANT PARSE (agy #1): before `toolCallFromReply`, strip ONE
  wrapping markdown code fence (``` or ```json ... ```) if the entire
  reply is a single fenced block. A correctly-schemed tool call inside
  a fence then EXECUTES (same reply, tolerant parse — not a retry); a
  drifted schema inside a fence hits the same drift classifier.
- Drift classifier (unchanged v2 core): single JSON object AND
  (action == known tool id OR tool_id field == known tool id) ->
  typed final, exactly one transport call/grant.
Detectors: fenced valid tool call executes; fenced drifted JSON ->
typed final; legitimate JSON with unrelated action delivered verbatim;
one-transport-call + one-grant assertions; ablations for the fence
strip and the known-tool check.

### B. Telegram adapter: outbound rendering (v3, the hermes shape)
Round-2 F1/F2/F3 (all three reviewers) proved BOTH multi-send shapes —
chunk loop and format fallback inside one Flush callback — violate the
B2/E15 one-effect-per-attempt contract. v3 removes multi-send entirely:

- CHUNKING MOVES TO THE ENQUEUE BOUNDARY (codex F1/F2, agy F1): a long
  reply is split BEFORE the outbox — EnqueueReply splits the rendered
  text into N parts and enqueues N ROWS with STABLE derived ids
  (dlv-<base>-p<i>-of-<n>); each row is one message, one wire effect,
  one SENT — the existing Flush/UNKNOWN/RECONCILE semantics apply per
  row untouched. A crash between parts leaves later parts PENDING
  (normal at-least-once), an accepted part is never resent blind.
  Split on line boundaries at <=4000 chars; a single line longer than
  4000 hard-splits on a rune boundary (agy #3); a split inside an open
  <pre> closes it and reopens in the next part.
- NO FALLBACK SEND (codex F3, kilo F2/F3): the renderer emits
  CONSTRUCTIVELY VALID Telegram HTML — the ONLY tags are <b>, <code>,
  <pre> built by the renderer itself around fully-escaped content
  (& < > escaped everywhere, tags always balanced by construction), so
  the "can't parse entities" class is impossible by construction and no
  second send path exists. Any 400 keeps today's failure semantics
  byte-for-byte. PROOF obligation: a property-style test feeds
  adversarial inputs (unclosed markers, model-typed <pre>, entities,
  huge pipes) and machine-validates the output — balanced known tags
  only, no unescaped & < > outside renderer tags.
- Tables/fences/bold/code rules unchanged from v2 (hermes semantics,
  lossless bullets).
Detectors: renderer property test above; unit tests per rule; adapter
tests: parse_mode present on every send; long reply -> N rows with the
derived ids, each SENT independently, crash-sim between parts leaves
later parts PENDING and resends nothing accepted; ablations: chunker
split-at-enqueue (single-row revert) and the property validator.

## Non-goals (P0)
- Bot API 10.1 sendRichMessage (needs new API surface).
- Full markdown engine; only the listed constructs.
- Native provider function-calling (bigger provider-layer slice; noted
  as the durable fix for A, deferred with a trigger: second schema-drift
  class observed).

## Risks / tradeoffs
- No retry at all in P0: a drifting model yields a typed "try again"
  final — one bad UX beat, zero contract violations. Native function
  calling (provider layer) remains the durable fix, deferred.
- HTML escape bugs could eat formatting, never content: the narrow
  entity-parse fallback re-sends the original plain.
- Table bullets are lossless by construction (empty/extra-cell rules);
  unparseable blocks pass through escaped.

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

### SCOPE CUT (v4, moderator decision after round 3)
Multipart/chunking is REMOVED from this slice entirely. Every round-3
HIGH (part ordering, B7 multipart transaction, reconcile grammar,
tag-splitting, UTF-16 budget) belongs to that machinery. Messages whose
text exceeds Telegram's limit keep TODAY'S behavior byte-for-byte (the
send fails as it does now); a separate chunking slice with its own plan
owns that — trigger: the first real over-limit reply observed in
dogfood. The dogfood pain (tables, tool-JSON leak) involves short
messages only.

### A. Planner: no tool-shaped JSON ever reaches the user (v4)
NO retry, NO execution of fenced payloads (codex r3 F1: the structured
owner forbids tolerant-accept for effect payloads — a fenced valid call
is indistinguishable from a display example and must NOT execute).
- Classification input: the raw reply; if the ENTIRE reply is one
  fenced block, strip exactly one fence FOR CLASSIFICATION ONLY. If the
  result still starts with a fence, the reply is prose (delivered).
- DRIFT (single JSON object AND (action==known tool id OR tool_id==
  known tool id)) -> typed Croatian final: "Nisam uspio ispravno
  pozvati alat (<tool>). Pokušaj ponovno ili preformuliraj." Exactly
  one transport call, one grant. A FENCED valid-schema call is also
  classified DRIFT (never executed, never delivered raw).
- KNOWN LIMITATION, DECLARED (kilo r3 F2): a legitimate content answer
  whose top-level action/tool_id equals a registered tool id is
  suppressed into the typed final. This is an INTENTIONAL fail-closed
  restriction (no soundness claim); the exact collision is a COMMITTED
  test asserting the typed final, so the behavior is visible, not
  hidden. Durable fix remains native function calling (deferred).
Detectors: drift shapes (bare + single-fence + valid-schema-fenced) ->
typed final, one call/one grant; unrelated-action JSON delivered
verbatim; double-fenced JSON delivered as prose; collision case
asserts the declared typed final; ablations: known-tool check, fence
strip.

### B. Telegram adapter: outbound rendering (v4)
`renderHTML(original) string` at the SEND boundary only — the outbox
row and journal keep the ORIGINAL text (kilo r3 F4 resolved: journal
truth = original, rendering is ephemeral per attempt).
- TOKENIZER PRECEDENCE SPECIFIED (kilo r3 F1): fenced block > inline
  code > bold; spans never overlap (first match wins, later overlapping
  markers are literal text); inside <pre>/<code> content is escaped
  only — NO nested tags can be emitted, by construction. Tables ->
  hermes bullet groups (lossless rules from v2) BEFORE span parsing;
  table cell content is escaped, cell headings bolded whole (no span
  parsing inside cells).
- ENTITY BUDGET (codex r3 constructive-validity residue): if the
  rendered message would exceed 50 tags, render DEGRADES at the render
  boundary to fully-escaped plain text (no tags at all) — one message,
  one send, no second attempt; Telegram's documented entity limit is
  never approached.
- Property validator test: for adversarial inputs (overlapping
  markers, model-typed HTML, entities, pipes, huge inputs) the output
  machine-checks: only renderer tags, balanced, NEVER nested, all
  & < > outside tags escaped, tag count <= 50.
- parse_mode=HTML on every send; NO fallback path exists; any Telegram
  400 keeps today's semantics byte-for-byte.
Detectors: property validator; per-rule unit tests (incl. nested-marker
overlap -> non-nested output; `<b>use <code>ls</code></b>` shape never
emitted); adapter test asserting parse_mode and unchanged 400
semantics; ablations: precedence rule (allow overlap -> validator RED),
entity-budget degrade.

## Non-goals (P0)
- Multipart/chunking (own future slice: enqueue-boundary rows, B7
  batch, reconcile grammar, UTF-16 budget — trigger: first real
  over-limit reply in dogfood).
- Bot API 10.1 sendRichMessage (needs new API surface).
- Full markdown engine; only the listed constructs.
- Native provider function-calling (durable fix for A, deferred).

## Risks / tradeoffs
- No retry at all in P0: a drifting model yields a typed "try again"
  final — one bad UX beat, zero contract violations. Native function
  calling (provider layer) remains the durable fix, deferred.
- HTML escape bugs could eat formatting, never content: the narrow
  entity-parse fallback re-sends the original plain.
- Table bullets are lossless by construction (empty/extra-cell rules);
  unparseable blocks pass through escaped.

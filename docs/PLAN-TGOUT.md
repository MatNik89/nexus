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

### A. Planner: no tool-shaped JSON ever reaches the user (v2)
NO retry. P0.2/S7 owns every retry and s7-min grants ONE attempt per
operation (review round 1, codex BLOCKER) — the planner must not issue
a second physical provider call. Instead:
- After `toolCallFromReply` says not-a-tool, classify the reply as
  SCHEMA DRIFT only when it is a single JSON object AND (its top-level
  `action` value equals a KNOWN tool id, OR it has a top-level
  `tool_id` field whose value is a known tool id). This is the exact
  observed drift signature (`{"action":"memory_recall",...}`) and
  cannot fire on legitimate JSON answers the user asked for (kilo F2:
  an `action` key alone is common in ordinary JSON content).
- On drift: return the TYPED final "Nisam uspio ispravno pozvati alat
  (<tool>). Pokušaj ponovno ili preformuliraj." — never the raw JSON,
  never another provider call. The user retries by talking (HITL
  spirit), the system never retries blind.
- Prompt hardening in the same slice: toolProtocol gains an explicit
  negative example ("NEVER {"action":"<tool name>"} — action is
  always the literal "tool"").
Detectors: scripted provider returning the drift shape -> typed final,
EXACTLY ONE transport call and one grant consumed (transport-call
counter + grant assertions per codex); legitimate JSON answer with an
unrelated `action` value -> delivered verbatim (false-positive guard);
ablation: drop the known-tool check -> RED on the false-positive case.

### B. Telegram adapter: outbound rendering (v2, the hermes shape)
`renderHTML(text) string` pure function, rules as v1 (tables ->
bold-heading + bullet groups exactly like the owner's local
hermes-agent convert_table_to_bullets; fences -> <pre>; `code` ->
<code>; **bold** -> <b>; all else HTML-escaped) with these review
corrections:
- ESCAPE PIPELINE SPECIFIED (codex #4/kilo F1): tokenize first (fences,
  inline code, bold spans, tables), HTML-escape every token's CONTENT,
  then wrap in tags — tags are constructed only by the renderer, never
  present in escaped content. Tests include `<script>` inside bold,
  `<pre>` typed by the model, `&` in table cells, nested/unclosed
  markers (pass through escaped).
- LOSSLESS tables (codex #5/agy #4): empty cells render as "• Header:
  —"; surplus cells beyond the header count are appended as "• (extra):
  value"; any row that fails to parse keeps the whole block verbatim
  (escaped). Nothing is dropped.
- 4096 LIMIT owned (codex #6/agy #2): before send, split the RENDERED
  text on line boundaries into <=4000-char chunks (each chunk closes/
  reopens an open <pre>); every chunk sends under the SAME delivery id
  row and the row is SENT only after the LAST chunk is accepted
  (at-least-once honesty: a crash mid-chunks redelivers all chunks —
  duplicates allowed, loss not).
- NARROW fallback (codex #3/kilo F3/agy #2): plain resend ONLY when the
  Telegram error description contains "can't parse entities" (the
  entity-parse class). Every other 400 keeps the existing failure path
  (PENDING/UNKNOWN per current outbox semantics) — a blocked bot or
  dead chat is not a formatting problem. The fallback send reuses the
  same delivery id; the outbox row becomes SENT only on wire
  acceptance of whichever attempt succeeded; the attempt sequence is
  journal-visible (outbound_unknown/resolved unchanged).
Detectors: unit tests per rule incl. the lossless-table cases and the
escape pipeline cases; adapter tests with the fake Bot API: (a)
parse_mode present, (b) entity-parse 400 -> ONE plain resend, one SENT
row, (c) non-entity 400 (too long simulated) -> NO plain resend, row
not SENT, (d) >4096 rendered -> chunked sends, SENT only after last
chunk, chunk count asserted; ablations for the fallback-narrowing and
the chunker.

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

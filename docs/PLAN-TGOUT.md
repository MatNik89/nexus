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

### A. Planner: no tool-shaped JSON ever reaches the user
In `ChatPlanner.Plan` (and the tools path), after `toolCallFromReply`
returns not-a-tool: if the reply still LOOKS tool-shaped (a single JSON
object whose top level has an `action` field), do ONE corrective retry:
re-issue the chat with an appended system-style correction ("your last
reply was malformed tool JSON; either use the exact schema
{"action":"tool","tool_id":...,"arguments":{...}} or answer the user in
prose"). If the second reply is still tool-shaped JSON, return a typed
final ("Nisam uspio ispravno pozvati alat — pokušaj ponovno.") — never
the raw JSON. Detector: RED test with a scripted provider returning the
malformed shape twice; assert the user-visible final contains no `{`
JSON and the corrective retry happened exactly once.

### B. Telegram adapter: outbound rendering (the hermes shape)
New `render.go` in internal/channel/telegram, pure function
`renderHTML(text) (html string)`:
- ALL markdown pipe-tables (outside code fences) -> bold-heading +
  bullet row groups, exactly hermes' convert_table_to_bullets
  semantics: heading = row-label cell (or first non-empty cell),
  remaining cells "• Header: value"; malformed pipe runs pass through
  unchanged.
- Fenced code blocks -> `<pre>` (contents escaped, otherwise verbatim);
  inline backticks -> `<code>`; `**bold**` -> `<b>`. Everything else
  HTML-escaped (& < >). No full markdown engine — exactly these rules.
- FlushOutbox sends `parse_mode:"HTML"` with the rendered text; on ANY
  Telegram 400 it RESENDS the ORIGINAL text plain (hermes' strip
  fallback, simpler: we still have the original — delivery honesty
  unchanged, same delivery id row, one SENT).
- The outbox row keeps storing the ORIGINAL text (journal truth
  unchanged); rendering happens at the send boundary only.
Detectors: unit tests per rule (table -> bullets incl. row-label and
no-label shapes, fence protection, `<script>` escape, bold/code
mapping); adapter test with the fake Bot API asserting parse_mode
present and the 400-fallback plain resend (fake returns 400 once, one
SENT row).

## Non-goals (P0)
- Bot API 10.1 sendRichMessage (needs new API surface).
- Full markdown engine; only the listed constructs.
- Native provider function-calling (bigger provider-layer slice; noted
  as the durable fix for A, deferred with a trigger: second schema-drift
  class observed).

## Risks / tradeoffs
- Corrective retry adds one provider round trip in the malformed case
  only. Bounded to ONE retry (no loop risk).
- HTML escape bugs could eat user content: the fallback-to-plain resend
  bounds the damage to formatting, never delivery loss.
- Row-bullet table heuristics can misfire on pathological pipes; the
  `<pre>` fallback keeps content lossless.

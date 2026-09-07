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
  tables inside code fences untouched. We adopt this SHAPE with the
  lossless deviations stated below (empty -> '—', surplus -> '(extra)'
  bullets; hermes pads/truncates).
- Formatting: `plugins/platforms/telegram/adapter.py:format_message`
  (line 8302): markdown -> MarkdownV2 with code blocks stashed behind
  placeholders, everything else escaped.
- Fallback: on Telegram BadRequest the adapter re-sends PLAIN via
  `_strip_mdv2` (line 475) — formatting can fail, delivery cannot.
- karfly/chatgpt_telegram_bot: malformed model output is never shipped
  raw; retry-with-correction is standard.

DELIBERATE DEVIATIONS (topknot): hermes uses MarkdownV2 (18-char
escaping + placeholders) and, on a parse failure, issues a SECOND
plain send (adapter.py:5241-5268) — we use parse_mode=HTML (3 escapes)
and NO second send at all: the render-boundary bool decides BEFORE the
wire (see B). Hermes' table algorithm is the MODEL, not a byte
contract: we deviate with '—' for empty cells and '(extra)' bullets
for surplus cells (hermes emits empty strings and truncates surplus —
lossy; ours is lossless).

## Proposed changes

### SCOPE CUT (v4, unchanged in v5)
Multipart/chunking is a separate future slice. See Non-goals.

### A. Planner: known drift dialects never ship raw (v5)
(The v4 heading overclaimed — codex F2/kilo F4. No completeness claim:
UNKNOWN dialects can still reach the user; every observed dialect gets
a committed case and the classifier grows per dialect.)
- ORDERED, MUTUALLY EXCLUSIVE: (1) strict `toolCallFromReply` on the
  RAW reply first, hardened to a CLOSED KEY CONTRACT (r7 codex HIGH —
  Go struct decoding matches keys case-insensitively, so a
  case-sensitive duplicate gate alone lets {"action":"x","Action":
  "tool"} execute): a valid call's top level must contain EXACTLY the
  lowercase keys {action, tool_id, arguments} (arguments optional),
  each appearing ONCE — checked by a token-level walk over the raw
  object (case-INSENSITIVE duplicate detection; unknown or
  non-lowercase keys reject). Anything else is not a valid call. A
  valid closed-contract call EXECUTES exactly as today (committed
  positive control; committed case-alias and duplicate cases assert
  non-execution);
- DUPLICATE/ALIAS ROUTING (r7 kilo F1, narrowed r8 kilo): when the
  closed-contract check rejects, the DRIFT decision does NOT re-decode
  (last-member-wins would hide the earlier value): the same
  token-level walk collects the values of the keys action / tool_id /
  name ONLY (key match case-insensitive), in ALL positions including
  duplicates; if ANY such value equals a registered tool id -> DRIFT;
  else prose. Values under OTHER keys are ignored — a legitimate
  answer like {"description":"memory_recall"} stays prose (committed
  case), while {"action":"memory_recall","action":"tool"} is drift,
  never prose, never execution (committed case) — and the SAME
  duplicate/alias cases are committed AGAIN through the single-fence
  normalization path (fenced duplicate -> drift; r10 kilo F2);
  (2) only when it says not-a-tool, build the classification view (one
  wrapping fence stripped; still-fenced-after-one -> prose); (3) DRIFT
  when the view is a single JSON object AND any of action/tool_id/name
  equals a registered tool id; (4) everything else is prose, delivered.
- DRIFT OUTCOME, FULLY SPECIFIED (r5 codex F2 / kilo F2): the planner
  returns a `contracts.TypedError` — Code "TOOL_SCHEMA_DRIFT",
  Category validation, Retryability NEVER, Origin planner, SafeMessage
  in neutral English naming the tool. The turn lands TERMINAL-FAILED:
  `turn.failed`'s payload gains an `error_code` field (journaled,
  structured — additive payload change), and the UDS error frame gains
  a `code` field so the repl edge maps WITHOUT string classification;
  the telegram handler maps the code to the Croatian user message.
  HISTORY INTERACTION (kilo F2): conversation history folds ONLY
  turn.succeeded, so a drifted turn never enters history as an
  assistant reply — declared and asserted in a committed test.
  RESTART-STABLE RECOVERY (r9/r10 codex MED): completedTurnFinal is
  extended to also recover a durable `turn.failed` with an
  `error_code` — a redelivered update whose turn already FAILED with
  TOOL_SCHEMA_DRIFT returns the same typed outcome (edge maps the same
  Croatian message) and the planner is NOT re-invoked. Detector:
  close/reopen (crash seam) after turn.failed but before
  CompleteInbound, redeliver, assert the same Croatian response and
  zero planner calls; ablation removes only failed-turn recovery ->
  RED.
  FIELD PRECEDENCE (r5 codex F5): when several fields match known
  tools, action > tool_id > name decides the reported <tool>;
  the conflicting-fields case is a committed test. The Croatian edge
  message says "Preformuliraj zahtjev." only — no "pokušaj ponovno"
  (r6 kilo F1: a failed turn leaves no history pair, so "again" has no
  referent; rephrasing restates the question as the current message).
- DECLARED LIMITS (committed tests assert each): the known-tool content
  collision suppresses a legitimate answer (fail-closed, visible);
  prose+JSON mixed replies are delivered as prose (agy #2 — classifier
  sees only whole-object replies; trigger to extend: first observed
  mixed-drift in dogfood); double-fenced JSON is prose.
Detectors: valid bare call executes once (positive control); drift
dialects (action=/tool_id=/name=known, bare + single-fenced) -> typed
sentinel, one transport call/grant, edge renders Croatian; collision +
mixed + double-fence declared-limit cases; ablations: ordering (swap
steps 1/2 -> positive control RED), known-tool check, fence strip.

### B. Telegram adapter: outbound rendering (v5)
`renderHTML(original) (string, bool)` at the SEND boundary; journal/
outbox keep the ORIGINAL. The bool says "send rendered with
parse_mode=HTML"; false means send the ORIGINAL exactly as today (no
parse_mode) — ONE message either way per flush tick, decided BEFORE
the wire (this replaces both the fallback and the degrade of earlier
drafts; recovery across TICKS is the attempts rule below — the plan
makes no "never a second attempt" absolute).
FIRST-LEASE-ONLY FORMATTING (v10 — honest contract per r9 codex):
THIS SLICE ADDS NO ATTEMPT AND CHANGES NO SCHEDULING. When and whether
a send happens is exactly today's machinery; the slice changes ONLY
WHAT BYTES an already-scheduled attempt carries. (The pre-existing
tick re-flush path runs without an S7 grant — a real architectural
seam gap that r9 surfaced; it is FILED as its own backlog item in
docs/tasks-P0.md in this same commit and is out of scope here,
unchanged byte-for-byte.)
- Rule: the rendered text with parse_mode is carried ONLY on the FIRST
  IN-FLIGHT LEASE of a delivery (projected count of the existing
  `channel.outbound_unknown` events == 0 at flush time); any later
  lease carries the ORIGINAL with no parse_mode.
- DECLARED DEGRADATION (r9 codex #2): outbound_unknown is appended
  BEFORE the wire, so a pre-wire failure (crash after parking, dial
  failure, callback rejection) also consumes the first lease — the
  first ACTUAL wire send is then already plain. Formatting-only cost,
  stated in tradeoffs, committed as a test case (pre-wire dial-failure
  world: message arrives plain).
- Ownership: the count is FOLDED from the canonical event by the
  projection (single-write-owner untouched, no mutable column path);
  `Projection.Version()` is bumped so old databases rebuild by replay.
Detectors (r9 codex #3 + r8/9 kilo): (a) fake-bot parse-400 world —
the row the EXISTING machinery re-pends carries plain bytes on its
next flush and lands SENT (the detector asserts carried BYTES, it does
not require or authorize any additional attempt); (b) RESTART
persistence — journal close/reopen between flushes, count survives,
plain carried; (c) VERSION rebuild — a database folded at the old
projection version refolds on open and the count is reconstructed from
events; (d) pre-wire dial-failure degradation case; ablations:
ignore-the-count (HTML re-carried -> RED) and drop-the-event-fold
(count never rises -> RED).
Renderer construction rules (v4 base plus):
- tokenizer precedence fence > inline code > bold, first-match-wins,
  no overlap; inside <pre>/<code> escaped content only; tables ->
  hermes bullet groups before span parsing;
- EMPTY SPANS ARE NEVER WRAPPED (agy #1): `****`, empty inline code
  and empty table cells emit no tag (markers become literal escaped
  text; empty cells use the '—' placeholder from v2 rules);
- PIPES INSIDE CELL CONTENT (r6 codex MED): the cell splitter is the
  naive hermes split; any candidate table block containing an escaped
  pipe (`\|`) or a backtick span with a `|` is NOT split — the whole
  block passes through escaped, verbatim (lossless beats pretty).
  Committed cases for both shapes;
- COUNTING UNIT (r6 codex LOW): the budget counts OPENING tags (spans),
  "span count <= 90"; the 90/91 boundary tests count spans;
- property validator: only renderer tags, balanced, never nested,
  NO EMPTY TAG, all & < > outside tags escaped, RENDERER tag count
  <= 90 (local budget — the validator makes no total-entity claim),
  rendered length <= 4096 UTF-16 units whenever bool=true.
Contradiction cleanup (codex F5/kilo F1): the earlier reference-section
sentence about hermes' plain-resend fallback describes HERMES ONLY; our
design has no fallback send — the render-boundary bool is a decision,
not a resend. The Risks section is rewritten accordingly.
Detectors: property validator (adversarial corpus incl. empty
markers); per-rule unit tests; boundary tests at 4096/4097 UTF-16
units and 90/91 tags (bool flips); adapter tests: parse_mode present
iff bool, original path byte-identical to today when bool=false,
unchanged 400 semantics; ablations: empty-tag rule, ordering,
tag/length budget.

## Non-goals (P0)
- Multipart/chunking (own future slice: enqueue-boundary rows, B7
  batch, reconcile grammar, UTF-16 budget — trigger: first real
  over-limit reply in dogfood).
- Bot API 10.1 sendRichMessage (needs new API surface).
- Full markdown engine; only the listed constructs.
- Native provider function-calling (durable fix for A, deferred).

## Risks / tradeoffs
- No retry in P0: a drifting model yields the typed sentinel and a
  Croatian edge message — one bad UX beat, no contract violations.
  Native function calling remains the durable fix, deferred.
- A render bug can only cost FORMATTING: bool=false sends the original
  through today's path; content and delivery semantics never depend on
  the renderer.
- Declared classifier limits (collision, mixed prose+JSON, unknown
  dialects) are visible in committed tests, not hidden claims.

# REVIEW-TGOUT-PLAN-kilo — design review of PLAN-TGOUT.md (slice tg-output)

**Scope:** `docs/PLAN-TGOUT.md` on `slice/p0-tgout` (HEAD 00dba3e). Read-only DESIGN
review — no code exists yet. Attacked along the requested axes, grounded in the current
`planner.go`, `telegram.go`, and `channel.go` the plan will modify.

## What the plan gets right

- **Root cause is correct and verified.** `toolCallFromReply` (`planner.go:179-224`)
  rejects `{"action":"memory_recall",...}` as not-a-tool (action != "tool") and the raw
  JSON then becomes the final (`planner.go:285`); `FlushOutbox` sends `{chat_id,text}`
  with no `parse_mode` (`telegram.go:294`).
- **Retry is bounded to ONE** (`PLAN-TGOUT.md:93`) — structurally safe against an
  infinite loop, because the retry lives inside `Plan`, which returns a Final or Call;
  the loop's `maxIters`/stuck-breaker still apply. No loop risk.
- **Delivery honesty is fundamentally preserved by the fallback.** A Telegram 400 is a
  *definite rejection* (`telegram.go:162-166` treats <500 as non-ambiguous), so an
  HTML-send 400 means nothing was delivered and a plain resend cannot double-send; the
  outbox row keeps the ORIGINAL text (`PLAN-TGOUT.md:75-76`), so one delivery id = one
  SENT whose content matches the journal. The T22 `Flush` UNKNOWN→SENT machinery
  (`channel.go:500-538`) is not undermined by a same-id fallback.
- Rendering at the send boundary only (journal truth unchanged) is the right seam.

## Findings

**F1 [MED] — the HTML escape surface is under-specified; the bold/code construct
insides are the injection hole you asked me to find in the HTML deviation.**
`PLAN-TGOUT.md:69-71` says fenced blocks get `<pre>` "contents escaped", but for
`**bold**` → `<b>` and inline backticks → `<code>` it never says the *inside* is
escaped, nor the ordering (escape-then-markdown vs markdown-then-escape). Telegram
`parse_mode=HTML` interprets nested tags: if `**X**` maps to `<b>X</b>` with X raw, then
model output `**<a href="http://evil">click</a>**` (or `` `<tg-spoiler>x</tg-spoiler>` ``)
becomes a live, disguised clickable link — a phishing vector that plain-text mode and
the `& < >` fallthrough cannot produce. The plan must pin: escape **all** content (`& < >`)
first, then map *only* the delimiters to tags; and add a detector for the `<b>`-wrapped
`<a href>` case (the current "`<script>` escape" detector only covers the bare-fallthrough
path, not construct insides). This is the concrete answer to "challenge HTML vs
MarkdownV2": HTML is fine only if the escape is airtight on every construct.

**F2 [MED] — the tool-shaped-JSON heuristic has a false positive on legitimate answers.**
`PLAN-TGOUT.md:50-52` triggers on "a single JSON object whose top level has an `action`
field". A user who asks "show me the JSON your tools expect" (or any dev-tooling JSON
with an `action` key — a very common key name) gets a *correct* JSON answer that is then
misread as a malformed tool call, retried, and — if the model correctly keeps answering
with the requested JSON — replaced by the typed failure "Nisam uspio ispravno pozvati
alat". The actual drift signature is `action == <a known tool id>` (the dogfood
`"memory_recall"`), so the guard should key on `action` ∈ `c.specs` (the sealed
registry), not on any `action` field. No detector covers the false-positive case.

**F3 [LOW] — the 400 fallback is over-scoped and its trigger is not detectable.**
"on ANY Telegram 400" (`PLAN-TGOUT.md:72-74`) resends plain for 400s plain text cannot
fix — chat not found, bot blocked, message too long — producing a PENDING↔retry loop.
Worse, `call` discards the Telegram error body (`telegram.go:166` returns only the
status code), so the fallback cannot distinguish a parse/entity 400 ("can't parse
entities") from those others; the plan must surface the Bot API error description and
scope the fallback to parse/entity errors (and never to `ErrAmbiguousSend`, else the
fallback *would* double-send). The committed detector ("400 once → one SENT") covers the
happy path only, not the both-send-400 or ambiguous-trigger cases.

**F4 [LOW] — table heuristic failure modes are unaddressed.** The pipe-table detector's
predicate (how many `|`, consecutive lines, header separator?) is unspecified
(`PLAN-TGOUT.md:64-67`), so prose or single-line content containing `|`, or a code
snippet the user asked for that isn't fenced, can be mangled into bullet groups; the
"<pre> fallback keeps content lossless" claim only holds if fences are perfectly
tracked, but unterminated ``` fences are not addressed. No detector for prose-with-pipes.

**F5 [LOW] — omissions.** (a) The corrective retry is a second physical provider call,
but the plan doesn't specify a fresh S7 `AttemptGrant` (issue → consume → Report) for it;
the current buffered path issues exactly one grant per `Plan` (`planner.go:259-272`), so
a governed retry needs an explicit second op. (b) The correction's message placement is
ambiguous ("system-style" vs a second `user` role message) — and appending to the shared
static system prompt vs. a transient user message have different effects on the
role-message contract just locked in the conv-history slice. (c) Message-length growth:
rendering (bullets + tags) can push a reply past the 4096-char Bot API limit, yielding a
400 that plain fallback may not fix (or may also exceed). (d) The guard is drift-specific:
it catches only the `action`-field shape; a malformed tool JSON without `action`
(e.g. `{"tool_id":"memory_recall"}`) still leaks raw — the plan's own title "no
tool-shaped JSON ever reaches the user" overstates the heuristic's reach (the non-goal
trigger partly acknowledges this, but the detector doesn't lock the boundary).

## Confidence

Grounded in the current code (`planner.go:179-311`, `telegram.go:136-184,286-296`,
`channel.go:500-538`). F1/F2 are the material design gaps (injection + false-positive);
F3–F5 are scoping/omission gaps. All are design-level (no code to test), and none is
resolved by the plan as written.

VERDICT: FAIL

# REVIEW-TGOUT-PLAN2-kilo — design review of PLAN-TGOUT.md v2 (commit c1c827c)

**Scope:** `docs/PLAN-TGOUT.md` at `c1c827c` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-1 findings (`REVIEW-TGOUT-PLAN-{codex,agy}.md`),
the current `planner.go`/`telegram.go`/`channel.go`, and `ARCHITECTURE-ESSENTIALS.md` E15.

## What v2 correctly resolves

- **S7 retry BLOCKER (codex #1 / agy F1) — closed.** The planner issues NO second
  provider call: schema drift → typed final, exactly one transport call and one grant
  (`PLAN-TGOUT.md:48-62`). Prompt negative-example hardening is a prompt change, not a
  retry. The planner's own "every call = one AttemptGrant" contract is honored.
- **JSON false-positive (codex #2 / agy F2 / kilo F1) — materially closed.** The drift
  signature is narrowed to `action == known-tool-id` OR `tool_id == known-tool-id`
  (`PLAN-TGOUT.md:52-58`); a known tool id is an internal name, so a user's requested
  JSON (e.g. `{"action":"opened"}`) can no longer fire it. A false-positive guard test
  is specified (`:68-70`).
- **Escape pipeline (codex #4 / kilo F1) — specified.** "tokenize → HTML-escape every
  token's CONTENT → wrap in tags; tags only from the renderer" (`:78-83`) is the correct
  trust-preserving construction that structurally prevents model bytes from becoming
  tags.
- **Lossless tables (codex #5 / agy F5) — addressed.** Empty `—`, surplus `(extra)`,
  unparseable-block-verbatim (`:84-87`) close the silent-cell-drop loss.
- **4096 limit (codex #6 / agy F3) — acknowledged** via chunking (`:88-93`).

## Findings

**F1 [MED] — chunking lacks per-part identity / partial-acceptance state, and is not
composed with the fallback.** (a) All chunks share ONE delivery id and the row is SENT
only after the last chunk (`PLAN-TGOUT.md:90-92`); a crash or `ErrAmbiguousSend` on
chunk k leaves the row UNKNOWN with no record that chunks 1..k-1 were delivered, so
redelivery re-sends ALL chunks (duplicates) — codex #6 explicitly required "stable
per-part identity, partial-acceptance state", which v2 omits in favor of re-send-all.
(b) The boundary rule only closes/reopens `<pre>` (`:89`); a `<b>`/`<code>` span crossing
a chunk boundary yields an unclosed tag → "can't parse entities" 400. (c) The fallback
"re-sends the original plain" (`:121-123`) is defined for one message; on a chunked
message it is unspecified whether it re-sends that chunk plain, the whole original plain,
or re-chunks — and "whole original plain" is un-chunked and may itself exceed 4096.

**F2 [MED→LOW] — the fallback and chunking remain adapter-local multi-send operations
invisible to the durable core (codex #3 residual).** The core parks UNKNOWN and observes
one `send(o)` aggregate; the HTML-400→plain-success sequence and the N chunk sends are
not journaled per-attempt, and E15 ("S7 alone authorizes and schedules delivery
retries", `ARCHITECTURE-ESSENTIALS.md:205`) is not honored by an adapter-local resend.
Narrowed to a definite 400 it cannot double-send, but it still is not "one physical send
per durable/S7-authorized delivery attempt" that codex #3 required. (I rate this LOW
rather than HIGH because the entity-parse 400 is a definite rejection, so the resend is
safe; the residual is observability/ownership, not delivery correctness.)

**F3 [LOW] — the fallback classification cannot be implemented against the current
`call`.** `call` discards the Telegram error body and returns only `HTTP %d`
(`telegram.go:161-166`), so "the description contains 'can't parse entities'" has no
source. The plan must specify surfacing + token-sanitizing the description.

**F4 [LOW] — detector adequacy (codex #4 residual).** The escape tests still list
"`<script>` inside bold" (`:82`) — `<script>` is not a Telegram tag, so a bug merely
400s into the fallback — and the real link-spoofing surface
(`</b><a href="tg://user?id=123">spoof</a><b>`, `<a href>` in bold) is still untested.
The pipeline is now correctly specified, but its security detector does not exercise the
supported-tag close/reopen injection it guards against.

**F5 [LOW] — the typed final is hardcoded Croatian in the kernel.** "Nisam uspio
ispravno pozvati alat …" (`:59-62`) is produced by the planner directly (not the model),
contradicting the repo's English-in-code rule and the existing English kernel refusal
strings (`telegram.go:238,246`). agy F6 is unresolved; the plan must state the policy
(English kernel strings with Croatian left to the model prompt, or an explicit
justified exception).

**F6 [LOW] — the heading still overclaims.** "no tool-shaped JSON ever reaches the user"
(`:48`) is the claim codex #2 asked to narrow; the classifier catches only
action/tool_id == known-tool-id, so sibling drift shapes (`{"tool":"memory_recall"}`,
`{"name":"memory_recall"}`, an OpenAI `function` object, fenced JSON) still reach the
user as raw finals. The body and non-goal (`:113-115`) scope this honestly, but the
heading does not.

## Answers to the specific questions

- **Drift classifier sound?** Yes for false positives (known-tool-id match eliminates the
  user-JSON trap); not complete (sibling shapes leak — acknowledged). The title (F6) is
  the only overclaim.
- **Chunk-redelivery honest under crash?** Yes (at-least-once, duplicates allowed, no
  loss, B2-compliant), but coarse: no partial-acceptance means re-send-all (F1).
- **Fallback covers the real entity-error strings?** Yes — every Telegram entity error
  contains "can't parse entities"; but the description is currently discarded (F3).
- **Remaining S7/delivery violation?** Adapter-local multi-send (fallback + chunking) not
  promoted to durable/S7 state (F2).
- **Detector adequacy?** Missing link-spoofing and crash-seam/partial-acceptance detectors
  (F4, F1).

## Confidence

Grounded in the round-1 review docs and the current source (`planner.go:179-311`,
`telegram.go:136-183,286-296`, `channel.go:500-538`, `ARCHITECTURE-ESSENTIALS.md:196-205`).
Design-only, so no RED/GREEN execution is possible. The findings are specification gaps,
not code defects; none is resolved by the plan as written.

VERDICT: FAIL

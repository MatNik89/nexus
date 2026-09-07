# REVIEW-TGOUT-PLAN5-kilo — design review of PLAN-TGOUT.md v5 (commit d140212)

**Scope:** `docs/PLAN-TGOUT.md` at `d140212` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-4 findings
(`REVIEW-TGOUT-PLAN4-{codex,agy,kilo}.md`), the current source, and the live Telegram
Bot API docs (`sendMessage` + `Formatting options`, fetched to verify the length/entity
claims).

## What v5 correctly resolves

- **Heading overclaim (kilo F4) — closed.** "known drift dialects never ship raw" with
  an explicit no-completeness claim (`:51-54`).
- **Croatian in the kernel (kilo F2 / codex F8) — closed.** The planner returns the
  language-free typed sentinel `TOOL_SCHEMA_DRIFT`; localization moves to the
  telegram/repl edge (`:62-69`).
- **Stale fallback prose (kilo F1 / codex F9) — closed.** The Risks section is rewritten;
  the reference-section fallback is explicitly "hermes ONLY", and the render-boundary
  bool is a pre-wire decision, not a resend (`:109-112`).
- **Ordered classifier (codex F1) — closed.** Strict `toolCallFromReply` on the raw reply
  first (valid bare call executes, committed positive control), drift only after
  not-a-tool, signature widened to action/tool_id/name, drift lands via the S2.3
  strict-refusal sentinel with no salvage/retry (`:55-69`).
- **Render-boundary decision — sound.** `(string, bool)`; bool=false sends the ORIGINAL
  byte-for-byte as today, one message either way, no second attempt. The 4096 limit is
  correctly identified as "after entities parsing" (verified in the Bot API docs,
  `sendMessage` text field), and the nesting rule is correct: the docs state bold/italic/
  underline/strike/spoiler may nest but `pre`/`code` are mutually exclusive with the rest,
  so the non-overlapping bold/code/pre tokenizer emits only valid HTML.

## Findings

**F1 [LOW] — the "100 message entities" premise is unverified and likely wrong.** The plan
states "Bot API allows 100 message entities" (`:95-96`). I fetched and searched the live
Bot API `sendMessage` and `Formatting options` sections: they document the 4096-character
"after entities parsing" limit (confirmed) but contain **no** per-message entity count
limit. The 90-tag budget therefore degrades large tables — the primary dogfood target
("tables render as ugly flat text") — to unformatted plain on a limit that is not in the
cited source. Either cite the actual limit (and the section), or drop/raise the budget and
accept that large tables render as bullets.

**F2 [LOW] — the TOOL_SCHEMA_DRIFT turn outcome is under-specified and the analogy is
misleading.** "The turn lands through the existing typed-error path (like NEEDS_APPROVAL)"
(`:65-66`) — but `NEEDS_APPROVAL` SUSPENDS the turn durably (HITL), whereas a strict
refusal is terminal ("no salvage, no re-ask"). The plan never says whether the drifted
turn lands TERMINAL with the Croatian message as its final (so it enters conversation
history as the assistant reply), or as a distinct refusal/failed state. This is material
to the conversation-history contract just locked in the conv-history slice.

## Notes (not findings)

- **Edge localization is inconsistent with existing refusals.** The edge maps
  `TOOL_SCHEMA_DRIFT` to Croatian (`:67-69`) while the existing refusal strings are
  English (`telegram.go:238,246`); the localization policy for the edge is undeclared.
- **`tool` field dialect still uncaught.** The signature is action/tool_id/name (`:60-61`),
  so `{"tool":"memory_recall"}` — the natural `tool_id` sibling codex named in round 2 —
  is deferred as an "unknown dialect". Declared, so not a hidden claim, but it is the most
  likely next drift.
- **"characters" vs "UTF-16 code units".** The Bot API text field says "characters" (the
  entity offset/length fields are the UTF-16 ones); measuring the 4096 bound in UTF-16
  units is conservative/safe, just slightly stricter than the documented wording.

## Confidence

Grounded in the round-4 docs, the current source, and the fetched Bot API docs (4096
"after entities parsing" confirmed; no entity-count limit found). F1 is a citation error
with a real consequence (large-table degrade); F2 is a turn-state ambiguity. Both LOW, but
unresolved under the all-severity rule.

VERDICT: FAIL

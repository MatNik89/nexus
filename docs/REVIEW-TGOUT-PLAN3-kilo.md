# REVIEW-TGOUT-PLAN3-kilo — design review of PLAN-TGOUT.md v3 (commit 7a976cc)

**Scope:** `docs/PLAN-TGOUT.md` at `7a976cc` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-2 findings
(`REVIEW-TGOUT-PLAN2-{codex,agy,kilo}.md`) and the current
`planner.go`/`telegram.go`/`channel.go`/`obligation.go`.

## What v3 correctly resolves

- **Multi-send delivery violations (codex F1/F2/F3/F6, agy F1/F4, kilo F1/F2) — closed.**
  Chunking moves to the ENQUEUE boundary as N rows with derived ids, one wire effect per
  row, so the existing `Flush` UNKNOWN/PENDING/SENT semantics apply per-row untouched and
  an accepted part is never blind-resent (`PLAN-TGOUT.md:68-77`). The plain fallback is
  REMOVED entirely (`:78-87`), eliminating the adapter-owned second attempt.
- **Fenced-JSON bypass (agy F3) — addressed** by the fence-tolerant parse (`:50-54`).
- **Error-description reachability (codex F4, agy F2) — mooted** by removing the fallback
  (no string-matching on the discarded `call` body remains).
- **Single-line overflow (codex F6, agy F4) — addressed** by the rune-boundary hard split
  (`:75-76`).

## Findings

**F1 [MED] — the constructive-validity claim is unsound for nested tags, and the fallback
that would have masked it is gone.** "The ONLY tags are `<b>`,`<code>`,`<pre>` …
balanced by construction, so `can't parse entities` is impossible" (`:78-82`) proves only
**balanced**, not **non-nested**. Telegram HTML rejects nested entities (e.g.
`<b>use <code>ls</code> now</b>` → "can't parse entities"), and the tokenizer's precedence
is never specified, so a bold span overlapping an inline-code span can emit exactly that
shape. With the fallback removed, such a message fails with a 400 that today's semantics
re-pend forever (PENDING retry loop). The property test must assert *non-nesting* (and no
overlap), and the tokenizer must define precedence (e.g. code wins over bold, or
overlapping markers flatten to escaped text), not merely "balanced known tags."

**F2 [MED] — the drift-classifier known-tool collision is still unresolved (codex F5).**
"Drift classifier (unchanged v2 core)" (`:55-57`) still suppresses a legitimate requested
answer whose `action` or `tool_id` equals a registered tool id (e.g.
`{"action":"memory_recall","enabled":true}`). codex F5 required either documenting this
as an intentional fail-closed restriction and removing the soundness claim, or adding the
exact collision as a false-positive detector; v3 does neither, and the heading
"no tool-shaped JSON ever reaches the user" (`:48`) still overstates.

**F3 [LOW] — the Croatian typed final is still unresolved (codex F8 / agy F5).** The
"typed final" is unchanged from v2 (still the Croatian string), in the kernel, against the
repo's English-in-code rule and the existing English refusal strings (`telegram.go:238,246`).

**F4 [LOW] — chunk-at-enqueue of "the rendered text" contradicts v2's "journal truth =
original", and the chunk budget domain is unspecified.** v2 stored the ORIGINAL text with
rendering at the send boundary; v3 now "EnqueueReply splits the rendered text" (`:69-70`)
without reconciling whether the outbox stores original or rendered. If rendered-at-enqueue,
the journal truth becomes the HTML rendering (a rendering bug now persists into the durable
row); if original-at-enqueue with render-at-send, each chunk's HTML expansion can exceed
4096 after the ≤4000-char split. The plan must state what the outbox row stores and that
the split budget is measured on the *rendered* (post-entity) length.

**F5 [LOW] — part ordering and enqueue atomicity are unspecified.** The derived ids
`dlv-<base>-p<i>-of-<n>` sort correctly, but `Flush` reads `ORDER BY created`
(`channel.go:453`) and `created = journal offset` (`channel.go:254`), so order is preserved
only if the N parts are enqueued as N in-order events in a single `AppendBatch` — the plan
names "EnqueueReply" while the real atomic terminal+rows batch boundary is `CompleteInbound`
(`channel.go:551-573`), and it never states the in-order/monotonic guarantee.

**F6 [LOW] — fence-strip residual: double-fenced JSON leaks raw, and there is no
false-positive guard.** Stripping exactly ONE wrapping fence (`:50-52`) leaves a second
fence on ``` ``` ```json {...} ``` ``` ``` → `toolCallFromReply` sees a non-`{` prefix and the
drift classifier sees a non-single-object body, so the raw fenced JSON is delivered. The
fence-tolerant parse also now EXECUTES a correctly-schemed call the user asked to *see*
("show me the memory_recall JSON") with no negative detector; the listed detectors cover
only "fenced valid call executes" and "fenced drifted JSON → typed final".

**F7 [LOW] — derived-id scheme is compatible but unreconciled with the reminder path.**
`-p<i>-of-<n>` disambiguates cleanly from the 28-char `dlv-<hex>` refusal/reply ids
(`telegram.go:214-217`, `channel.go:396-398`) — no collision. But the plan does not state
that the obligation/reminder delivery (a SEPARATE `DELIVERY_PENDING→DELIVERED→ACKED` state
machine, `obligation.go:55-86`, not the channel outbox) also passes through the chunked
enqueue; a >4096-char reminder that bypasses `EnqueueReply` would still exceed the limit.

## Answers to the specific questions

- **Derived-id vs reminder/refusal conventions:** compatible (no collision), but the
  reminder path's chunking coverage is unstated (F7).
- **Part-ordering at the receiver:** not guaranteed by the plan — depends on
  `created`-monotonic enqueue it doesn't specify (F5).
- **Constructive validity:** false for nesting; balanced ≠ non-nested, and the fallback
  that would mask the 400 is removed (F1).
- **Fence-strip abuse:** double-fence leak + no false-positive guard (F6).
- **Detector adequacy:** the property test must assert non-nesting, and the known-tool
  collision + fenced-false-positive cases are untested (F1/F2/F6).

## Confidence

Grounded in the round-2 review docs and the current source (`planner.go:179-311`,
`telegram.go:136-183,214-217`, `channel.go:254,396-398,453,551-573`, `obligation.go:55-86`).
Design-only, so no RED/GREEN execution is possible. F1 and F2 are the material gaps; the
rest are specification omissions. None is resolved by the plan as written.

VERDICT: FAIL

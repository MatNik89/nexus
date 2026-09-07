# REVIEW-TGOUT-PLAN4-kilo — design review of PLAN-TGOUT.md v4 (commit 81acb9f)

**Scope:** `docs/PLAN-TGOUT.md` at `81acb9f` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-3 findings
(`REVIEW-TGOUT-PLAN3-{codex,agy,kilo}.md`) and the current source.

## What v4 correctly resolves

- **Every multipart/chunking finding (codex F2–F6, agy F1–F4, kilo F5/F7) — closed by
  scope cut.** Chunking is removed entirely; over-limit sends keep today's behavior,
  deferred to a dedicated slice with its own trigger (`PLAN-TGOUT.md:48-56`).
- **Fenced execution (codex F1, agy F5) — closed.** A fenced valid call classifies as
  DRIFT → typed final (never executed, never delivered raw); the strip is
  "FOR CLASSIFICATION ONLY", so single-fenced prose is delivered unstripped (`:58-69`).
- **Known-tool collision (codex F5 / kilo F2) — closed.** Declared an intentional
  fail-closed restriction with a committed collision test and no soundness claim
  (`:70-75`).
- **Nesting (codex F7 / kilo F1) — closed.** Tokenizer precedence fence>code>bold with
  first-match-wins non-overlap makes nested tags impossible by construction, and the
  property validator asserts non-nested + balanced + escaped + ≤50 tags (`:86-101`).
- **Journal-truth (kilo F4) — closed.** Outbox/journal keep the ORIGINAL; rendering is
  per-attempt at the send boundary (`:82-85`).
- **Property-test ablation (codex F8) — closed.** Ablations are production mutations
  (allow overlap → validator RED; drop the entity-budget degrade) (`:107-108`).

## Findings

**F1 [LOW] — stale fallback prose still contradicts the no-fallback decision (codex F9 /
agy F6, unresolved across two rounds).** Line 102 states "NO fallback path exists", but
line 42 still says "We keep hermes' SEMANTICS (tables->bullets, plain fallback)" and
line 122-123 still says "the narrow entity-parse fallback re-sends the original plain."
These are mutually exclusive wire behaviors; the stale sentences must be deleted.

**F2 [LOW] — the Croatian typed final is still hardcoded in the kernel (codex F8 /
agy F5, unresolved across three rounds).** Line 66-67 still emits
"Nisam uspio ispravno pozvati alat …" from the planner, against the repo's English-in-code
rule and the existing English kernel refusal strings (`telegram.go:238,246`). Unlike the
collision, this deviation is not declared as an intentional owner exception.

**F3 [LOW] — the 50-tag entity budget degrades the primary dogfood target to flat text
on an uncited limit.** The degrade at >50 tags (`:93-97`) means a table of ~10 rows × 5
columns (≈50 bold headings/cells under the bullet rendering) falls back to fully-escaped
plain — the exact "ugly flat text" the slice exists to fix. The plan asserts "Telegram's
documented entity limit" without citation, so a threshold that defeats the primary use
case is justified by an unverified premise. Either verify/cite the limit and raise the
threshold, or acknowledge that large tables are intentionally unformatted.

**F4 [LOW] — the heading still overclaims.** "no tool-shaped JSON ever reaches the user"
(`:58`) is false for the double-fenced shape, which is declared "prose (delivered)"
(`:63-64`) and therefore ships raw fenced JSON. The body is honest; the heading is a
holdover that should be narrowed to the actual guard (bare + single-fenced drift →
typed final).

## Contract-violation check (clean)

- S7/planner: one transport call, one grant; no retry; no fenced execution. The
  structured-output owner (no tolerant-accept of effect payloads) is honored by routing
  fenced valid calls to DRIFT.
- Delivery: one send per outbox row, no adapter-owned second attempt; the entity-budget
  degrade is a render-time decision, not a resend. No multipart, so no B7/reconcile/ID
  grammar exposure.
- Renderer: balanced + non-nested + escaped + ≤50 tags is a sound constructive-validity
  claim for the *parse* class; the 4096-length class is deliberately out of scope.

## Confidence

Grounded in the round-3 review docs and the current source. The four findings are
documentation/premise gaps, not new contract violations; all are LOW, but under the
all-severity rule they are unresolved.

VERDICT: FAIL

# REVIEW-TGOUT-PLAN8-kilo — design review of PLAN-TGOUT.md v8 (commit 8f8bc4c)

**Scope:** `docs/PLAN-TGOUT.md` at `8f8bc4c` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-7 findings
(`REVIEW-TGOUT-PLAN7-{codex,agy,kilo}.md`) and the current source.

## What v8 correctly resolves

- **codex HIGH (case-alias bypass) — closed.** The CLOSED KEY CONTRACT requires exactly the
  lowercase `{action, tool_id, arguments(optional)}` keys, each once, via a token-level
  walk with case-insensitive duplicate detection and non-lowercase/unknown-key rejection
  *before* struct decode, so `{"action":"x","Action":"tool",…}` can no longer execute
  (`:59-70`).
- **kilo F1 (duplicate-key raw leak) — closed.** The drift decision no longer re-decodes
  (last-member-wins); the same walk routes `{"action":"memory_recall","action":"tool"}` to
  DRIFT, never prose, never execution (`:71-77`).
- **codex MED (parse-400 retry loop) — functionally closed.** FIRST-ATTEMPT-ONLY
  formatting: `attempts==0` sends rendered+parse_mode, every re-flush sends the ORIGINAL
  plain, so a parse rejection self-heals next tick with one wire attempt per tick and the
  400 code untouched (`:117-127`).

## Findings

**F1 [LOW] — the "ALL top-level string values" walk widens the declared collision beyond
`action`/`tool_id`/`name`.** The reject-routing step collects *every* top-level string
value, not just the drift fields, so a legitimate JSON answer that merely mentions a tool
name in an unrelated key — e.g. `{"description":"memory_recall"}` or
`{"tools":["memory_recall"]}` — is rejected by the closed-key contract (unknown key) and
then classified DRIFT, suppressing a correct answer. The kilo-F1 case only requires
scanning the `action`/`tool_id`/`name` key values in *all positions* (to defeat
last-member-wins on a duplicate); scanning arbitrary keys over-classifies. Either narrow
the walk to those three keys or declare the wider collision explicitly.

**F2 [LOW] — "no second attempt ever exists" now contradicts the FIRST-ATTEMPT-ONLY
mechanism.** Line 115 still asserts "no second attempt ever exists", while `:122-123`
defines a re-flush ("every re-flush of a previously attempted row sends the ORIGINAL") —
which *is* a second wire attempt on a later tick. The round-7 codex MED fix explicitly
required rewriting this to "no fallback attempt within the same flush"; the functional
fix landed but the wording fix did not, leaving the plan self-contradictory on the exact
point the finding named.

## Notes (not findings)

- The `attempts` counter's increment timing (re-pend vs. UNKNOWN-park) is left to the
  projection; the required property — any re-flush sends plain and the counter is
  monotonic — is stated, and reconcile-proved-loss is the only resend path, so no
  double-send is introduced.

## Confidence

Grounded in the round-7 docs and the current source. The three functional fixes are
correct; the two findings are scope/wording residuals, both LOW but unresolved under the
all-severity rule.

VERDICT: FAIL

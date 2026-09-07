# REVIEW-TGOUT-PLAN7-kilo — design review of PLAN-TGOUT.md v7 (commit 389d361)

**Scope:** `docs/PLAN-TGOUT.md` at `389d361` on `slice/p0-tgout`. Read-only DESIGN
review — no code. Cross-checked against the round-6 findings
(`REVIEW-TGOUT-PLAN6-{codex,agy,kilo}.md`), the current source, and the referenced
contracts owner (`HasDuplicateJSONKeys`).

## What v7 correctly resolves

- **Top-level duplicate-member gate (codex HIGH) — execution risk closed.** Any
  duplicate top-level key makes the reply not-a-valid-call before struct decode, so
  `{"action":"memory_recall","action":"tool",…}` can no longer be decoded by Go's
  last-member-wins rule into an executed effect (`:59-67`).
- **Pipe-in-cell content loss (codex MED) — closed.** A candidate table block containing
  an escaped pipe or a backtick span with a `|` is passed through escaped-verbatim,
  lossless (`:128-132`).
- **Budget counting unit (codex LOW) — defined.** The unit is opening tags (spans),
  `span count <= 90`, boundary tests count spans (`:133-134`).
- **Rephrase-only message (kilo F1) — closed.** "Preformuliraj zahtjev." with no
  "pokušaj ponovno" (`:85-88`).

## Finding

**F1 [LOW] — the duplicate-key gate prevents execution but does not route the reply to
DRIFT, so an adverse duplicate leaks raw JSON as prose.** The plan states "any duplicate
top-level key means NOT a valid call (falls to the classifier)" and that the "committed
duplicate-key case asserts drift, not execution" (`:62-67`). But the drift classifier
(`:69-71`) re-decodes the view with the same last-member-wins semantics
(`toolCallFromReply`-style `json.Unmarshal`, per `planner.go:184-195`) and matches only
`action/tool_id/name == known-tool` on the *last* value. So
`{"action":"memory_recall","action":"tool"}` — a duplicate where the known tool id sits in
the non-last position and no `tool_id`/`name` field remains to catch it — is classified as
prose and shipped raw, contradicting the "asserts drift" claim. The codex fix said "route
duplicate action/tool_id/name objects that identify a registered tool to TOOL_SCHEMA_DRIFT";
a registered tool in the first of two `action` members still identifies the object, so the
gate must fail closed: any duplicate top-level key → DRIFT (typed final), independent of the
decoded last value, with the adverse-duplicate case committed.

## Notes (not findings)

- The budget unit is defined as "opening tags (spans)" (`:133-134`), but the
  render-false condition (`:114`) and the property validator (`:137`) still say "tag
  count". The definition resolves the ambiguity, but the wording should say "span" for
  consistency with the codex-LOW rename.
- `\|` pass-through shows the raw escape (backslash-pipe) rather than a literal pipe;
  this is lossless-but-not-pretty and is consistent with the declared "lossless beats
  pretty" tradeoff.

## Contract check (clean)

- No S7/delivery regression: one transport call/grant, `NEVER` retryability, additive
  `error_code`, pre-wire render decision, no fallback/multipart, non-nested renderer.

## Confidence

Grounded in the round-6 docs, the current source, and the `HasDuplicateJSONKeys` owner.
F1 is a genuine but narrow gap (a contrived duplicate-key payload leaks as prose instead
of drifting); the notes are trivial. Under the all-severity rule, F1 is unresolved.

VERDICT: FAIL

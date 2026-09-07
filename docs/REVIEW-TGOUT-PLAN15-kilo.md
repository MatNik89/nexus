# REVIEW-TGOUT-PLAN15-kilo — design review of PLAN-TGOUT.md v15 (commit 87c2643)

**Scope:** `docs/PLAN-TGOUT.md` at `87c2643` on `slice/p0-tgout` (HEAD verified; file
contains `NON-DRIFT FAILED CLASS`). Read-only DESIGN review — no code. Cross-checked
against the round-14 findings (`REVIEW-TGOUT-PLAN14-{codex,agy,kilo}.md`) and the current
`loop.go`/`daemon.go`.

## What v15 correctly resolves

- **codex MED (non-drift `FAILED` undefined) — closed.** Ordinary failures journal
  `turn.failed` with no code; the fold returns `Kind=FAILED` with EMPTY `Code` =
  "terminal observed, no renderable payload", `RunChannelTurn` then behaves exactly as
  today's collision-error path (no invented response), and a later terminal still
  suppresses the stale suspension regardless of payload; only `Code=="TOOL_SCHEMA_DRIFT"`
  reconstructs the typed outcome (`:126-136`). The ordinary-failed and
  `suspended → resumed → ordinary-failed → crash → collision` detectors are committed.
- **codex LOW (same-tier duplicate precedence) — closed.** First occurrence in source
  token order wins for duplicate same-field members naming different registered tools,
  with bare + single-fenced two-registered-tools cases asserting the same tool across the
  structural error, journal payload, live edge, and recovered edge (`:137-143`).

Both rules are deterministic and consistent with the existing field precedence
(`action > tool_id > name` picks the tier; first-source-occurrence picks within the tier).

## Note (not a finding)

The ablation set covers "drop the persisted tool field" but has no symmetric "drop the
persisted `error_code` field" ablation. A hypothetical bug that persists `tool` but omits
`error_code` would silently misclassify a drift turn as an ordinary failure (generic reply
instead of the Croatian rephrase). This is a minor detector asymmetry rather than a design
flaw: the code is the constant discriminator, the "only `Code==TOOL_SCHEMA_DRIFT`" rule
makes its criticality explicit, and the `drop tool` ablation already locks the analogous
variable field.

## Confidence

Grounded in the round-14 docs and the current source. Both round-14 findings are
functionally closed; the single note is a non-material detector asymmetry.

VERDICT: PASS

# REVIEW-TGOUT-PLAN17-kilo — design review of PLAN-TGOUT.md v17 (commit 6e27dfb)

**Scope:** `docs/PLAN-TGOUT.md` at `6e27dfb` on `slice/p0-tgout` (HEAD verified).
Read-only DESIGN review — no code. Cross-checked against the round-16 note
(`REVIEW-TGOUT-PLAN16-kilo.md`) and the current delivery machinery.

## What v17 correctly resolves

- **r16 kilo note ("next flush tick delivers PLAIN" overreach) — closed.** The risk
  statement now reads: a rejected rendered send re-pends the row; the next existing flush
  tick **CARRIES** the original plain bytes, and **SENT remains conditional on remote
  acceptance** exactly as the at-least-once boundary requires — "that plain attempt can
  itself fail or go ambiguous — no eventual-delivery guarantee is claimed"
  (`:248-253`). The detector is now correctly scoped as bounding **BYTE DEGRADATION**
  (plain carried on the re-attempt) plus the scripted accept-on-next-attempt case, rather
  than "proving eventual plain delivery" (`:254-257`).

This is an accurate statement of the real behavior: `Core.Flush` re-pends on a definite
400 (`channel.go:514-535`), the next tick re-attempts, and the SENT mark is only set on
wire acceptance — so "carries" (the bytes on the re-attempt) is honest where "delivers"
would overclaim, and the at-least-once boundary admits failure/ambiguity on the plain
attempt too.

## Notes (not findings)

- Line 191 ("Formatting-only cost") remains correct for the pre-wire-failure class, whose
  one-tick re-pend latency is pre-existing, so the slice's new cost there is genuinely
  formatting-only — a different case from the parse-400 class the tradeoffs describe.
- Line 193's "message arrives plain" is a test-case description (dial-failure world), not
  an eventual-delivery guarantee.

## Confidence

Grounded in the round-16 note and the current source. The single round-16 note is folded,
the statement is honest with no remaining delivery guarantee overreach, and the two notes
are non-material.

VERDICT: PASS

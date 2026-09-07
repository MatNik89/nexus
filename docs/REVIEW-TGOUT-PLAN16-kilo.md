# REVIEW-TGOUT-PLAN16-kilo — design review of PLAN-TGOUT.md v16 (commit df06ed5)

**Scope:** `docs/PLAN-TGOUT.md` at `df06ed5` on `slice/p0-tgout` (HEAD verified).
Read-only DESIGN review — no code. Cross-checked against the round-15 finding
(`REVIEW-TGOUT-PLAN15-codex.md`) and the current `telegram.go`/`channel.go`.

## What v16 correctly resolves

- **codex LOW (round 15 — "can only cost FORMATTING" false) — closed.** The risk
  statement now reads "A render bug costs FORMATTING and, in the parse-400 case, DELIVERY
  LATENCY: a rejected rendered send is re-pended and delivers PLAIN on the next existing
  flush tick (the committed parse-400 detector is the bound proving that eventual plain
  delivery). The durable bytes (original text) never depend on the renderer"
  (`:248-253`). This matches the real machinery: a non-2xx 400 returns an ordinary error
  (`telegram.go:160-167`), `Core.Flush` re-pends it (`channel.go:514-535`), and the next
  opportunity is a later flush tick (`telegram.go:301-315`). The "formatting-only"
  absolute is gone and the honest latency consequence is stated, with the parse-400
  detector named as the eventual-delivery bound and the renderer-independent durable
  bytes preserved.

## Notes (not findings)

- Line 191 ("Formatting-only cost" in the DECLARED DEGRADATION) refers to the **pre-wire
  failure** class (crash after parking / dial failure / callback rejection), whose one-tick
  re-pend latency is pre-existing (today's dial failure also re-pends to the next tick), so
  the slice's *new* cost there is genuinely formatting-only — it is a different case from
  the parse-400 class that v16's tradeoffs now describe. The wording is consistent with the
  corrected tradeoffs, not a stale absolute.
- Line 250 says "the next existing flush tick" while the codex FIX said "an existing later
  flush"; the immediately following "eventual plain delivery" bound (`:251`) already admits
  the multi-tick case (a plain send can itself fail and re-pend), so the honest bound is
  present.

## Confidence

Grounded in the round-15 finding and the current source. The single round-15 LOW is folded
and the two notes are non-material wording nuances, not false claims.

VERDICT: PASS

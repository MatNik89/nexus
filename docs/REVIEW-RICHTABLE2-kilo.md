# Re-review of richtable fix (`bab40f8`)

**Scope**: commit `bab40f8` on `slice/p0-richtable` — never clip into
`sendRichMessage`; over-limit tables fall through.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The round-1 silent-clip defect is eliminated: the rich path is taken
only when the whole reply fits, with no truncation, and the over-limit case
falls through to the existing HTML/plain path. Verified: `go build ./...`
clean; `TestPipeTableUsesRichMessage`, `TestOverLimitTableSkipsRich`,
`TestNonTableSkipsRichMessage` PASS; full `telegram` package PASS.

## Verification

- `fitsRich` (`telegram.go:339-343`) = `len([]rune(s)) <= 32768`, and the
  rich branch is now `hasPipeTable(o.Text) && fitsRich(o.Text)`
  (`telegram.go:309-312`) carrying `o.Text` verbatim — no `clip32k`. A body
  that does not fit is sent losslessly via the old path or not at all, never
  truncated-then-SENT.
- The over-limit case falls to `renderHTML` (bold+bullets if within the 4096
  UTF-16 budget) or plain `sendMessage` — the pre-existing path, unchanged.
- `TestOverLimitTableSkipsRich` is RED-capable: reverting to the clipped
  `clip32k(o.Text)` would set `lastMethod == "sendRichMessage"` for the
  >32768-rune fixture and fail the assertion.
- Delivery contract unchanged: one wire send per tick inside the
  `Attempts == 0` first-lease block; a pre-10.1 400 re-pends and the next
  tick carries plain; no double-send (B2/E15 preserved, as in round 1).

## Notes (not FAIL reasons)

1. `fitsRich` counts **runes**, not UTF-16 code units. An emoji-heavy table
   within 32768 runes but over 32768 UTF-16 units would 400 on
   `sendRichMessage` and self-heal to plain — no silent loss, only a lost
   rich render for that edge. Minor.
2. An over-limit table that also exceeds the 4096 HTML budget falls to plain
   `sendMessage`, which 400s and re-pends until the deferred **multipart**
   slice lands. This is the pre-existing behavior for *all* >4096 messages
   (explicitly deferred in the commit), not a regression from this fix —
   the fix's scope was "never silently drop the suffix", which it honors.

VERDICT: PASS

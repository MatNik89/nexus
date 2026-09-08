# Review of richtable slice (`5871a71`)

**Scope**: commit `5871a71` on `slice/p0-richtable` — native Telegram tables via
`sendRichMessage` + prompt fix.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The slice ships tables natively without a B2/E15 delivery-contract
violation and is not a correctness/security defect. Verified: `go build ./...`
clean, the two new detectors PASS, and the full `telegram` and `planner`
package suites PASS.

## Substantive checks (all clean)

### Delivery contract (B2/E15)

The `sendRichMessage` branch lives inside the existing `o.Attempts == 0`
first-lease block (`telegram.go:302-312`) and returns `a.call(...)`, so it
inherits the exact discipline of the HTML path:

- **ONE wire send per tick** — no second send; the branch is an exclusive
  alternative to `renderHTML`, not an additional send.
- **Re-pend on failure** — a pre-10.1 client 400 propagates out of `a.call`,
  the core `Flush` re-pends the row (attempts→1) without advancing the
  offset, and the next tick (`attempts>0`) falls through to plain
  `sendMessage`. No double-send: a 400 is a client rejection, nothing was
  delivered, and the re-pend carries plain.
- **Offset-honesty** — `sent-but-unrecorded → UNKNOWN → RECONCILING` is the
  pre-existing framework behavior, unchanged by this slice (it only changes
  the carried bytes, not the marks).

### Correctness

- `hasPipeTable` (`telegram.go:325-333`) uses the same `isTableDivider`
  (`render.go:127`) as the HTML renderer, so both paths agree on what a table
  is; no detection divergence.
- `clip32k` bounds the rich payload; on failure the row self-heals to plain,
  so a 400 never strands the message.

### Security

- The markdown path sends the redacted model final raw (no `html.EscapeString`),
  unlike the HTML path. This is the assistant's own output, not untrusted
  content, and Telegram renders markdown (it does not execute instructions or
  HTML), so no instruction-injection or HTML-injection boundary is crossed.
  The HTML path already interprets markdown entities via `renderSpans`, so no
  new interpretation surface is introduced either.

## Notes (not FAIL reasons)

1. `clip32k` truncates at 32768 **runes** with no ellipsis, so a >32k table
   silently drops its tail on a 10.1 client. Strict improvement over the old
   4096-UTF-16 HTML ceiling (which would fall back to a 4096-limited plain
   send), and a pre-10.1 client still receives the full text as plain — UX
   nit only.
2. `clip32k` counts runes, not UTF-16 code units; an emoji-heavy >32k table
   could 400 and self-heal to plain. Minor; not a delivery failure.
3. The claimed "pre-10.1 400 → re-pend → plain" path has no dedicated
   detector (the two detectors cover table→rich and non-table→plain only).
   The re-pend behavior is inherited from the HTML path and already covered
   by the existing flush tests — a missing nice-to-have, not a gap.
4. A message containing BOTH a table and HTML-only formatting now routes
   entirely through markdown (the table check short-circuits `renderHTML`).
   Bold/code render natively in markdown, so formatting survives; benign
   behavior shift, worth noting.
5. The prompt's "NEVER as a bullet list" may cause over-tabling of marginal
   tabular data — UX taste, not correctness/security.

VERDICT: PASS

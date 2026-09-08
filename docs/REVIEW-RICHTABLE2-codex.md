# Re-review: rich-table over-limit delivery fix

**Scope:** immutable commit `bab40f88e6dda54b5bda2a6b4db003e8d318f081` on `slice/p0-richtable`  
**Reviewer:** Codex  
**Date:** 2026-09-08  
**Verdict policy:** FAIL only for a substantive correctness, security, delivery-contract, or buildability defect.

## Outcome

No substantive findings.

The round-1 delivery-integrity defect is resolved. `FlushOutbox` now selects
`sendRichMessage` only when the complete original table satisfies the 32,768-character
rich-message limit. It sends `o.Text` unchanged; no clipping helper or other lossy transform
remains. An over-limit table follows the pre-existing `renderHTML`/plain `sendMessage` path,
so remote rejection remains governed by the existing outbox failure/reconciliation behavior
instead of a clipped payload earning `SENT`.

The boundary operator is correct: `fitsRich` accepts exactly 32,768 Unicode code points and
rejects 32,769. This matches Telegram's current documented limit of “up to 32768 UTF-8
characters” for rich-message text: <https://core.telegram.org/bots/api#rich-message-limits>.

## Immutable evidence

- Source checkout: `HEAD` and `slice/p0-richtable` both resolved to
  `bab40f88e6dda54b5bda2a6b4db003e8d318f081`.
- Clean export: `/home/matej/HARNESS/nexus-review-bab40f8-codex`, produced with
  `git archive --format=tar bab40f8 | tar -xf - -C ...`; the exported
  `internal/channel/telegram/telegram.go` SHA-256 matched `git show bab40f8:...`:
  `1e4274f6aacc24167926915fb1687eeb013de35e1fee2d1d8c6422494dff8877`.
- Focused package:
  `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./internal/channel/telegram`
  — exit 0.
- Full suite:
  `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...`
  — exit 0; all discovered packages passed.
- Vet: `/home/matej/.local/go/bin/go vet ./...` — exit 0.
- Same-class sweep: the committed tree has one `sendRichMessage` call site, guarded by
  `hasPipeTable(o.Text) && fitsRich(o.Text)`; `clip32k` has no remaining production or test
  reference.
- Causal detector ablation in the disposable export: removing only
  `&& fitsRich(o.Text)` while retaining the committed test made
  `TestOverLimitTableSkipsRich` fail with exit 1 at
  `telegram_test.go:628` (`over-limit table clipped into sendRichMessage`).
- Review-only boundary probe: exact 32,768/32,769 cases for ASCII and four-byte UTF-8 input,
  plus the three committed rich-routing tests, passed with exit 0.

## NEXUS audit questions

1. **Solves the reported class? YES.** The remote-success path can no longer acknowledge a
   clipped table because the rich request carries the complete original or is not selected.
2. **Regression? NO found.** In-limit tables keep the rich path; over-limit tables preserve
   the original bytes and enter the existing fallback path.
3. **Detector validity? YES for the claimed gate.** It drives the production adapter through
   the fake Bot API and observes the selected wire method.
4. **Revert-proof? YES.** Removing only the new size gate turns the committed detector RED.
5. **Class sweep? YES.** There is one rich-send call site and no remaining clipping path.

## Non-blocking notes and weakest points

- The committed over-limit detector asserts only that `sendRichMessage` was not selected and
  ignores the returned flush error. It would be stronger if it also asserted the exact
  fallback method/body. Static flow, the fake-wire record, and the causal ablation establish
  the current fix, so this is detector-strength debt, not a product defect.
- The detector's failure text says “clipped” even when the controlled regression merely
  routes the untrimmed over-limit body to `sendRichMessage`; this is editorial only.
- No live Telegram call was authorized. The exact request bytes and state-machine behavior
  are locally proven; Telegram acceptance/rendering is bounded by the official API contract,
  not a live observation.

The strongest counterargument is that `sendRichMessage` has limits beyond text length (for
example block and table-shape limits). Those can still make an in-limit rich attempt fail,
but they do not recreate silent successful truncation: the existing failure path re-pends the
original and a later lease carries it without the rich mode. Multipart remains correctly
outside this slice.

Lean already. The fix deletes the lossy helper, adds one predicate, introduces no dependency
or new delivery state, and reuses the existing fallback path.

Minimal Diff: review report only; candidate code was not modified.
-> proof: clean-export focused tests, full suite, and vet GREEN; gate-only ablation RED; exact boundary probe GREEN.
-> weakest link: no live Telegram request was authorized, so remote rendering was not observed.
-> skipped: live canary and multipart implementation; upgrade only with explicit live authorization or selection of the multipart slice.
Proof ceiling: local Linux/ARM64 evidence proves routing, complete carried bytes, detector sensitivity, and repository build/test health; it does not prove live Telegram availability or client rendering.
Status: PASS at `bab40f88e6dda54b5bda2a6b4db003e8d318f081`
topknot: ultra+preflight

VERDICT: PASS

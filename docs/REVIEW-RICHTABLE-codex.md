# Rich-table implementation review

## Result

**FAIL.** The normal-size feature is correctly wired: a detected GFM pipe table takes one
`sendRichMessage` call carrying `rich_message.markdown`; non-table replies remain on the
existing `sendMessage` path; and a definite rich-message 400 re-pends the row so the next
flush sends the original plain text once. The clean export builds and the full suite is
green.

One substantive correctness and B2/E15 defect remains. The transport silently truncates a
durable outbox payload before sending it, then records the original delivery as `SENT`.

## Artifact binding

- Target: `5871a716a2b861bfba83b0d16e15ba76920c7413` on
  `slice/p0-richtable`.
- Tree: `4fb2aac980505e180aa4e3d824572bbd24afa6ec`.
- Clean on-disk export:
  `/home/matej/HARNESS/nexus-richtable-review.PbAfya7X`.
- Archive SHA-256:
  `dbf82042357c0ddfb9407881aa721f461551627699b1c5d461ab2bd78509414e`.
- Scoped export blob hashes matched Git for `telegram.go`, `telegram_test.go`,
  `render.go`, `channel.go`, `planner.go`, `PLAN-TGOUT.md`,
  `ARCHITECTURE-ESSENTIALS.md`, and `HARDQ-CONSOLIDATED.md`.
- Tests ran as UID 1000 with Go 1.26.4 on Linux/ARM64.
- The disposable export was removed after all target blobs were restored and re-hashed;
  cleanup verification passed.
- Concurrent untracked `docs/REVIEW-RICHTABLE-agy.md` and
  `docs/REVIEW-RICHTABLE-kilo.md` appeared only after the evidence run. They were not read,
  modified, or used as evidence.

## Substantive finding

### 1. [CONCRETE][SEV: MED] A lossy transport-local clip still earns `SENT` for the full durable reply

`internal/channel/telegram/telegram.go:309-311,335-342` passes
`clip32k(o.Text)` to Telegram, while the durable outbox row and delivery identity continue
to represent the complete `o.Text`. On API success, the unchanged core marks that delivery
`SENT` at `internal/channel/channel.go:523-535`. Nothing records that the suffix was removed,
and no later delivery can recover it.

Input -> a durable 32,811-rune reply containing a valid pipe table and a unique tail
sentinel.

Expected -> the full durable payload is delivered before its row becomes `SENT`, or any
intentional shortening is made explicit and canonical before enqueue so the receipt binds
the bytes actually promised.

Actual -> the fake Bot API accepted a 32,768-rune `rich_message.markdown`; the tail sentinel
was absent, and `DeliveryStatus` returned `SENT` for the original 32,811-rune outbox row.

Root cause -> the size transform is inside the transport callback, after durable enqueue
and outside the delivery identity/receipt owner. This violates the B2/E15 honest-delivery
contract even though Telegram accepted the shortened request.

PROBE: review-only package-local test
`TestReviewRichDeliveryDoesNotAcknowledgeTruncatedOutboxText`; it failed with
`carried_runes=32768 original_runes=32811 tail_present=false` while status was `SENT`.

FIX: remove transport-local `clip32k`; enforce/store the exact bounded user-visible payload
before outbox enqueue (with an explicit truncation marker), or implement durable multipart
rows before claiming the complete reply delivered.

## Verified behavior

- Telegram's official Bot API documentation confirms that `sendRichMessage` accepts the
  required `rich_message: InputRichMessage`, that `InputRichMessage.markdown` is valid, that
  rich Markdown supports tables, and that rich text is limited to 32,768 UTF-8 characters:
  <https://core.telegram.org/bots/api#sendrichmessage>.
- `TestPipeTableUsesRichMessage` and `TestNonTableSkipsRichMessage` passed in the pristine
  export.
- Disabling only the table branch made `TestPipeTableUsesRichMessage` RED with
  `table not sent via sendRichMessage: "sendMessage"`.
- Forcing every first lease through the rich branch made `TestNonTableSkipsRichMessage` RED
  with `plain reply wrongly used sendRichMessage`.
- Review-only `TestReviewRich400RependsThenNextTickPlainOnce` passed: tick 1 made exactly
  one rich call and left one PENDING row with `Attempts == 1`; tick 2 made exactly one plain
  call and landed the row `SENT`.
- Static flow confirms UNKNOWN-first parking remains before the wire and no second send was
  added inside a flush callback.
- The system prompt explicitly requests GitHub-style pipe tables.

## Non-blocking notes

- `TestPipeTableUsesRichMessage` checks that the carried body contains the header rather
  than equals the complete source string. Production currently carries the complete string
  for in-limit inputs, but equality would make the stated raw-markdown detector stronger.
- `TestNonTableSkipsRichMessage` proves only “not rich”; it does not assert the exact
  `sendMessage` method/body. Existing adapter coverage and the current branch establish that
  behavior, so this is a test-strength note, not a product defect.
- The committed suite has no rich-specific 400 fallback case. The review-only probe proved
  the current behavior, so this is missing regression coverage, not a FAIL reason by itself.
- `FlushOutbox` still says it delivers through `sendMessage`, and `PLAN-TGOUT.md` still
  lists Bot API 10.1 rich messages as a non-goal. These are stale editorial artifacts.
- The new system-prompt instruction has no direct regression assertion. The literal is
  present and correct; this is non-blocking coverage debt under the requested verdict policy.

## NEXUS audit questions

1. **Solves the reported class? PARTIAL.** In-limit pipe tables use Telegram's native rich
   path, but over-limit table replies can be acknowledged after silent data loss.
2. **Regression? YES.** The new rich path can turn a formerly rejected over-limit payload
   into a successful but incomplete delivery.
3. **Detector validity? YES for routing, limited for byte identity.** Both routing tests are
   causally RED; the table test does not assert full-body equality.
4. **Revert-proof? YES for routing.** Removing the table branch or widening it to non-tables
   independently turns the corresponding committed test RED.
5. **Class sweep? YES.** Table detection, rich request construction, API error
   classification, UNKNOWN-first parking, re-pend, attempt folding, later plain delivery,
   sent marking, and the prompt producer were traced.

## Counterargument, Topknot pass, and proof ceiling

The strongest counterargument is that ordinary replies are far below 32,768 characters,
the normal-size table path is correct, and multipart is an explicit non-goal. That does not
resolve this finding: deferring multipart can justify a visible failure or an explicit
canonical truncation, but it cannot justify recording delivery of bytes that were silently
discarded after durable enqueue.

Apart from `clip32k`, the change is lean: it reuses the existing table detector and outbox
state machine, adds no dependency or new retry mechanism, and makes one wire call per flush.
The smallest safe simplification is to delete the transport-local lossy transform and place
any bounded representation at the canonical enqueue boundary.

Minimal Diff: review report only; candidate code was not modified.
-> proof: immutable full suite and vet GREEN; both routing ablations RED; rich-400 two-tick probe GREEN; lossy-SENT probe RED.
-> weakest link: no live Telegram call was authorized, so client rendering is supported by the official API contract and the fake-wire request, not a live screenshot.
-> skipped: live/paid canary and multipart design; upgrade live proof only with explicit authorization, and multipart when the owner selects that slice.
Proof ceiling: local Linux/ARM64 evidence proves request bytes and durable state transitions, not production Telegram rendering or availability.
Status: FAIL at `5871a716a2b861bfba83b0d16e15ba76920c7413`
topknot: ultra+preflight

VERDICT: FAIL

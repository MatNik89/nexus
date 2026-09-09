# Review of audit-remediation plan v5 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `3bbdf0d` on `slice/p1-audit`
- **Verdict**: PASS

## Round-4 disposition

- **Note 1 (picker chrome ungoverned / cross-slice coupling)** — CLOSED. The v5
  call-site table (`:256-268`) enumerates every current `a.call` site
  (`telegram.go:249,347,392,396,400,463,475`; `telegram_picker.go:33,49,158,171`)
  and assigns governed kinds: `delivery`, `poll`, `control` (`setMyCommands`,
  `getMe`), and `ui` (`sendChatAction` typing; picker `sendMessage`/
  `answerCallbackQuery`/`editMessageReplyMarkup`/`editMessageText`). The picker
  chrome is `PolicyUI` (MaxAttempts 1, terminal on failure, no retry, no outbox
  routing) — the owner-accepted ephemeral boundary.
- **Note 2 (S7 dual narrative)** — STILL OPEN as a note; a tying comment remains
  advisable.

## New findings

None substantive. All three codex round-4 findings are folded and verified; the
attack surfaces named in the dispatch hold.

### Companion binding — codex r4 #1 closed

`Companion{Key contracts.OperationID; Params contracts.EnvelopeParams}` is typed
and mandatory (`:149-160`): for a `PolicyDelivery` consume exactly one companion
is required, `c.Key == g.OperationID` is enforced (missing/foreign →
`ErrAttemptNotAuthorized`, zero wire), and `Consume(g, c)` / `Report(op, …,
build func(Landing) Companion)` / `Cancel(op, build)` commit the S7 transition
plus the ONE owner-built companion in a single `journal.AppendBatch`
(`:161-166`). The swapped-companion bug (companion for B under A's grant) is
caught at the S7 key boundary; the channel payloads carry `operation_id` and the
validator/projection reject `operation_id != "delivery:"+delivery_id`
(`:221-228`). This is exactly the codex r4 #1 prescribed fix. Detector 9e tests
no-companion, key-mismatch, and malformed-`operation_id` independently.

### Every-physical-call table — codex r4 #2 closed

The complete method table (`:258-263`) covers `sendMessage`/`sendRichMessage`,
`getUpdates`, `setMyCommands`/`getMe`, `sendChatAction`, and all picker chrome,
each with an operation/target identity and a policy; `call` refuses a kind whose
recomputed identity does not match the presented grant, so a compile-only grant
is impossible. Detector 9d is now table-driven over every method.

### Structured re-ask — codex r4 #3 closed

`provider.Extract`'s GENERAL re-ask migrates to `s7.Execute` under
`PolicyStructured{MaxAttempts 2, RetryableCodes {invalid_general}}` (`:292-300`);
the extractor validates and proposes (`OutcomeFailedRetryable`) without issuing
or looping, SECURITY/EFFECT stay terminal, and detector 9f ablates S7's second
grant while leaving extractor/provider code untouched.

## Notes (not FAIL reasons)

1. **The opaque-key binding has an inherent, accepted residual.** S7 checks
   `Key == op` and never interprets channel semantics (`:159-160`); the channel
   validator checks the payload's own `operation_id == delivery:<delivery_id>`.
   A deliberately-mismatched companion (`Key=delivery:A`, `Params=mark(B)` with
   internally-consistent `operation_id=delivery:B`) would pass both checks, since
   neither side cross-checks the other's field. This is the exact split the codex
   prescribed (opaque key + no cross-owner semantics), reachable only by a bug in
   the single `Flush` loop that builds Key and Params from the same `DeliveryID` —
   a correctness nit, not a security hole (A's wire stays authorized, B is parked
   UNKNOWN, never wrongly sent). A one-line note on the invariant
   (`Key == payload.operation_id == op` by construction) would make it explicit.
2. **S7 dual narrative** (in-memory `machine.AttemptTable` `attempt.*` + durable
   `s7.*` projection) remains; rehydration rebuilds the former from the latter. A
   tying comment would help.

VERDICT: PASS

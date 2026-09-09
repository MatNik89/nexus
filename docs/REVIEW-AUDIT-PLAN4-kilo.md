# Review of audit-remediation plan v4 (`docs/PLAN-AUDIT-FIXES.md`)

- **Reviewed revision**: `fdb39dc` on `slice/p1-audit`
- **Verdict**: PASS

## Round-3 disposition

- **Note 1 (`Policy` omits `operation_id`/`cancel_token_id`)** — CLOSED.
  `:135-139` now states `Policy` + the `op` key together ARE the SPEC
  `ExecutionPolicy`, with `operation_id`/`cancel_token_id` = the key and a doc
  comment on the field mapping.
- **Note 2 ("rehydrated RUNNING" wording)** — CLOSED. `:163-174,192-200` now
  makes the durable `s7.attempt_started` event the discriminator: rehydrated
  AUTHORIZED-with-no-STARTED → `lease_revoked` → fresh grant; rehydrated
  RUNNING (STARTED, no report) → UNKNOWN.
- **Note 3 (`FAILED` vs `FAILED_TERMINAL`)** — CLOSED. `:135-137` states the
  one-vocabulary mapping.
- **Note 4 (Slice A "S7 target" ambiguity)** — STILL OPEN as a note; B2 now uses
  explicit target strings (`channel:<adapter>:delivery:<id>`,
  `channel:tg:getUpdates`, `channel:tg:setMyCommands`), while Slice A's
  `<host>:<port>` remains the provider's target. Wording only.

## New findings

None substantive. All five codex round-3 findings are folded and verified
buildable against the named interfaces; the four attack surfaces the dispatch
named hold:

### Batch-commit API — legitimate HARDQ B7 recipe, not a single-writer violation

`Consume(g, siblings...)` / `Report(..., siblings...)` / `Cancel(op, siblings...)`
(`:147-161`) take `contracts.EnvelopeParams` built by the OWNER (the channel
builds `outbound_unknown`/`PENDING`/`FAILED` params; S7 builds its own
`attempt.*`/`s7.*` params) and commit them in ONE `journal.AppendBatch`
(`journal.go:601-603` — the single serialized append actor). Ownership stays
with the builder; S7 is only the committer through the single actor, and a
malformed sibling is still rejected by each event's `PayloadValidator` at the
journal boundary (fail closed). `Report` selects retryable-vs-terminal sibling by
its own retryability decision (`:235-239`) — correct, since retryability IS S7's
domain and the channel supplies both consequences.

### Lease recovery vs E9 — correct, no double-send

The durable `s7.attempt_started` (appended in the same batch as the outbox
UNKNOWN park, `:149-152,222-225`) makes the wire-touched distinction exact:
AUTHORIZED-with-no-STARTED = wire never touched → `lease_revoked` → fresh grant
(`:163-174`); STARTED-with-no-report = wire touched → UNKNOWN, never re-granted.
`attempts` counts CONSUMED attempts only, so an expired never-consumed lease does
not burn the cap. The old nonce is dead (SHA-256 recorded, `:192-197`), so a
stale grant is refused. No E9 double-send window.

### Execute vs the loop's Issue path — correct

`s7.Execute` (`:175-180`) is the S7-owned synchronous retry driver; the planner
submits ONE operation (`:264-267`); `Issue` remains `Begin(PolicyTool)+Next` for
single-attempt tools. Both entry styles are S7 code, and detector 8b abletes
S7's second-grant path to prove the planner holds no loop.

### Ungoverned physical calls — delivery/poll/control all governed; one NOTE remains

`call(ctx, method, req, out, g s7.Grant, kind)` consumes before `client.Do` for
`delivery`/`poll`/`control` (`:241-249`, `:377-385`). See Note 1 below for the
picker chrome.

## Notes (not FAIL reasons)

1. **The `/cronjob` picker's ephemeral chrome is an ungoverned physical call.**
   `answerCallbackQuery`/`editMessageReplyMarkup`/`editMessageText`/
   `sendForceReply`/calendar `sendMessage` reach `client.Do` via `a.call`, but B2
   enumerates only `delivery`/`poll`/`control` kinds. The plan's claim "EVERY
   physical Bot API call carries a grant" (`:241`) is therefore over-broad, and
   the `call` signature change breaks those already-merged call sites
   (`telegram_picker.go`) without the plan naming them. Not a retry-owner or
   security violation: the chrome is best-effort/no-retry and egress-governed via
   Slice A, and the SPEC's "every provider/tool attempt" does not cover channel
   UI. Fix: either enumerate a `control:tg:<chrome>` kind (MaxAttempts 1) or state
   explicitly that the ephemeral picker chrome is a no-retry best-effort path
   exempt from grant governance.
2. **S7 keeps a dual narrative** — the in-memory `machine.AttemptTable`
   (`attempt.*` transitions) plus the durable `s7.*` projection. Reasonable
   (in-memory fast path + durable crash-safety); rehydration rebuilds the former
   from the latter. A doc comment tying the two would help.

VERDICT: PASS

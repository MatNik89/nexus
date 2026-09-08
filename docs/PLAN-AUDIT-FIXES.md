# PLAN v6: full-audit remediation (codex AUDIT-FULL-2026-09-08) — zero-defect

Source: `docs/AUDIT-FULL-codex-2026-09-08.md` (9 findings; bound at f135eef,
reverified against current tree). v2 folded round 1 (`docs/REVIEW-AUDIT-PLAN-{codex,kilo,agy}.md`, 3x FAIL); v3 folds
round 2 (`docs/REVIEW-AUDIT-PLAN2-*.md`: codex FAIL 7, kilo FAIL 1, agy PASS) and the
OWNER decision (final): full S7 engine now. v4 folds round 3 (`REVIEW-AUDIT-PLAN3-*.md`:
codex FAIL 5 [B/D lifecycle], kilo PASS, agy PASS). v5 folds round 4
(`REVIEW-AUDIT-PLAN4-*.md`: codex FAIL 3 [companion binding, ungoverned Bot API
siblings, structured re-ask], kilo PASS, agy PASS). v6 folds round 5
(`REVIEW-AUDIT-PLAN5-*.md`: codex FAIL 3 [Next exhaustion unpaired, structured attempt
accounting, poll code vocabulary], kilo PASS, agy PASS). Every fix: RED-capable
detector at the real owner boundary, then 3-agent review to 3xPASS.

## Status
- [x] **F4 (HIGH)** acceptance subprocess-output race — DONE (96b0c49). Round-1
  verified closed (codex: removing the fix makes the package fail to compile with
  the landed callers = causal RED; ceiling: host race detector unavailable).
- [x] **F9 (LOW)** duplicate JSON config keys — DONE (96b0c49). Round-1 verified
  revert-proof (deleting the guard -> `TestDuplicateJSONKeysRejected` RED).
- [ ] F1 F2 F3 F5 F6 F7 F8 — slices A-F below.

## Cross-slice invariants (bind every slice)
- S7 is the ONLY retry/deadline/cancel owner (`AGENTS.md:19-22`; SPEC P0.2). Slice B
  pulls the full S7 engine forward (owner decision); adapters and loops still hold NO
  retry loop — they only ask S7 for the next grant. No default-transport fallback, no
  fail-open path, no silent truncation.
- Every detector names the RED it observes against the CURRENT code and is
  anchored to a literal from the audit or constitution.

## Slice A — F1 + F8(provider): one shared E11 egress owner
Root: `internal/llm/provider/provider.go:132-141` builds `http.Client` with no
`Transport` -> `http.DefaultTransport` (ambient proxy, unpinned DNS, no receipt)
while carrying the bearer token (E11 violation, `ARCHITECTURE-ESSENTIALS.md:146-158`).
Telegram already has the compliant dialer (`internal/channel/telegram/dialer.go`),
but its receipt sink is optional (`dialer.go:82-87`: nil receipt = success) and its
pre-wire marker is private (`errEgressPreWire`, `dialer.go:27`).

Fix — new package `internal/foundation/egress` (transport-neutral, imports only
stdlib + contracts):
- `type Decision struct{Component, Host string; Resolved []netip.Addr; Pinned netip.Addr; Allowed bool; Reason string}`
- `type ReceiptSink func(Decision) error` — MANDATORY: `NewPinnedClient` returns an
  error when the sink is nil (fail closed at construction; no silent no-receipt mode,
  not even loopback).
- `var ErrPreWire` exported typed sentinel: every refusal and every receipt failure
  wraps it (nothing left the process). Telegram's `isPreWire` maps
  `errors.Is(err, egress.ErrPreWire)` exactly as it maps the private marker today
  (`telegram.go:165-181,206-215`) — the definite-pre-wire classification is preserved.
- `NewPinnedClient(component, apiBase string, egressAllow []string, timeout time.Duration, opts Options, sink ReceiptSink) (*http.Client, error)`
  carrying the EXACT policy moved from telegram (Unmap, embedded-v4 forms, mode floor
  incl. RFC1918/ULA/link-local/CGNAT/doc/benchmark, whole-set refusal, Proxy:nil,
  reject-redirects, durable receipt BEFORE the permitted dial, receipt failure fails
  the dial closed). Options = resolve/dial injection for tests only.
- Receipt wiring: the composition root (`cmd/nexus/main.go`, right after
  `journal.Open` and BEFORE `provider.NewAPIKey` at `:522-527`) builds ONE
  journal-backed sink; the event kind stays `channel.egress_attempt` (existing
  registration/projection compat); its payload gains `component` — the
  `channel.egress_attempt` PayloadValidator and the projection fold are updated
  accordingly (kilo r2 note).
  `channel.Core.RecordEgress` is deleted (its body moves to the sink constructor);
  telegram receives the sink instead of building its own.
- Canonical endpoint unit (codex r2 #6): the shared owner defines ONE
  `Endpoint{Host string /*lowercased hostname*/; Port int /*effective: explicit or
  scheme default 443/80*/}` and uses it for allowlist matching, dial address, receipt
  `Host`, and the S7 target string (`<host>:<port>`). An `egress_allow` entry without
  a port means the scheme default port; with a port it means exactly that port. The
  provider's current `host:port` entries keep working (`provider.example:8443`),
  Telegram's `api.telegram.org` means `:443`. No wildcarding; case-normalized.
- Provider: `NewAPIKey(cfg, auth, sink)` builds its client ONLY through
  `egress.NewPinnedClient("provider", ...)`; production host must be in
  `egress_allow` (already required at `provider.go:112-115`). The topknot-ceiling
  comment at `provider.go:62-67` is removed (S6.3 has landed).
- Telegram `dialer.go` is deleted; `newPinnedClient` callers call
  `egress.NewPinnedClient("telegram", ...)` directly (no wrapper).
- F8-provider body bound (BOTH `Chat` and `Stream`): `Chat` reads at most
  `maxBodyBytes+1` bytes (`io.LimitReader(body, max+1)`), rejects when more than
  `max` were read, then strictly decodes the bounded bytes and requires EOF after the
  single JSON value. `Stream` wraps `resp.Body` in `io.LimitReader(..., maxStreamBytes)`
  before the scanner (transport-layer ceiling; the planner accumulator ceiling is
  Slice C). Both maxima are constants owned by the provider package (upgrade trigger:
  a real provider reply exceeding them); `maxStreamBytes` (transport) is set above
  the planner's `maxStreamTotal` so Slice C detector #4 observes the accumulator
  ceiling independently (agy r2 note).

Detectors (RED against current code):
1. Provider spy resolver answering a metadata/RFC1918 address: dial refused, ZERO
   socket dials, receipt journaled with `Allowed=false` — RED today (default
   transport connects).
2. Provider client `Transport.Proxy == nil` with `HTTPS_PROXY` set in env: the spy
   dial target is the pinned IP, not the proxy — RED today.
3. Nil sink -> `NewPinnedClient` returns error — RED against the moved code.
4. Receipt sink returns error on a permitted decision -> ZERO socket dials, error
   wraps `ErrPreWire` — for both components.
5. Telegram: a denied delivery ends in outbox state `FAILED` (Slice B) / today
   `PENDING`, never `UNKNOWN` — pre-wire classification preserved through the shared
   owner.
6. Body cap: a VALID chat JSON object completed below the cap plus trailing bytes
   that push the total above it is REJECTED (a truncated object alone is not a
   red-capable detector for this bypass); a stream exceeding `maxStreamBytes` is cut
   with an error.
7. Shared deny-floor table test asserting provider and telegram reject the same set.
8. Endpoint unit: explicit non-default provider port admitted only by a matching
   `host:port` entry; `https://h` and `https://h:443` are the same unit; `H.example`
   matches `h.example`; same host + wrong port is DENIED.
Phase note (codex r2): detector 5's expected outbox state is `PENDING` while Slice A
is reviewed alone (today's definite-failure re-pend) and `PENDING` (S7-scheduled) or
`FAILED` (exhausted/terminal) after Slice B; both expectations are stated in the test.

## Slice B — F2: FULL S7 retry engine (P2 pulled forward, owner decision) + delivery wiring
**OWNER DECISION (2026-09-08, final): build the full S7 engine NOW.** Round-2 kilo
proved option 1 (park every definite failure terminally) violates HARDQ B2
at-least-once delivery, which an owner cannot waive; the engine is therefore a HARD
dependency of closing F2, not a deferral.

Root: `channel.go:583-589` re-pends definite failures to `PENDING`; the Run loop
`FlushOutbox`es every tick (`telegram.go:433-442`); `Adapter.call` reaches
`client.Do` (`telegram.go:193-223`) with no `AttemptGrant`; `s7min` is one-grant
no-retry (`s7min.go:3-9,97-105`), so the constitutionally required owner of a
SECOND delivery attempt (`SPEC P0.2:1386-1393`; `ESSENTIALS E15:196-205`) does not
exist yet. `Core.Flush` sends one row per callback (`channel.go:554-568`).

### B1 — engine: `internal/kernel/s7` (s7min promoted; ONE authority, ONE package)
`s7min` is renamed/promoted to `s7` (callers `loop`, `planner`, `provider`, `daemon`
updated; `Grant`, `Outcome`, `ErrAttemptNotAuthorized` keep their shape so the
existing `test_adapter_cannot_self_retry` family stays valid).
- `type Policy struct{ EffectClass contracts.EffectClass; MaxAttempts int; AttemptTimeout,
  Deadline time.Duration; Backoff BackoffPolicy; RetryableCodes []string;
  FallbackTargets []contracts.TargetID; IdempotencyKey string; Durable bool }`
  (= SPEC P0.2 `ExecutionPolicy` MUST-fields; `cancel_token_id` = the OperationID,
  `Cancel(op)` is the token). `BackoffPolicy{Base, Max time.Duration; Jitter bool}`
  exponential with full jitter (jitter source injectable for tests).
- Three policy CONSTANTS owned by `s7` (no config knob; upgrade trigger: a second
  provider/target configured):
  - `PolicyTool`: MaxAttempts 1 (effectful; unchanged P0 behavior).
  - `PolicyProvider`: MaxAttempts 3, AttemptTimeout 120s, Deadline 5min, Backoff
    1s/2s/4s jitter, RetryableCodes {`http_429`, `http_5xx`, `transport_prewire`},
    Durable false (turn-scoped).
  - `PolicyDelivery`: MaxAttempts 8, Deadline 24h, Backoff 5s..30min jitter,
    RetryableCodes {`transport_prewire`, `http_429`}, Durable true.
- State-name mapping (one vocabulary, stated once — codex r3 note): code
  `PLANNED`=SPEC `PENDING`, `AUTHORIZED`=`GRANTED`, `RUNNING`, `FAILED_RETRYABLE`,
  `FAILED`=`FAILED_TERMINAL`, `CANCELLED`, `UNKNOWN`. `Policy` + the `op` key together
  ARE the SPEC `ExecutionPolicy` (`operation_id` = the key, `cancel_token_id` = the key;
  a doc comment on `Policy` states the field mapping — kilo r3 note 1).
- API: `Begin(op, target, Policy) error` (idempotent: an existing record — including
  one rehydrated from the projection with its attempts/next_attempt_at — is a strict
  no-op; agy r3 note 2);
  `Next(op, build func(Landing) Companion) (Grant, error)` issues attempt N+1 ONLY
  from PENDING (first) or FAILED_RETRYABLE, only when `now >= next_attempt_at`,
  `attempts < MaxAttempts`, `now < deadline`; otherwise `ErrNotDue` (nothing
  committed) or `ErrExhausted`. Exhaustion / deadline is a durable transition to
  FAILED that uses the SAME paired recipe (codex r5 #1): S7 computes
  `Landing{Terminal}`, the owner's builder returns the `FAILED` companion, and both
  commit in ONE batch — there is no observable `S7=FAILED / outbox=PENDING` state. For
  a non-durable operation `build` is nil. `Issue(op,target)` remains as `Begin(PolicyTool)+Next` for
  the loop. Paired transitions use a TYPED, MANDATORY companion (codex r4 #1 — not an
  unconstrained variadic list): `type Companion struct{ Key contracts.OperationID;
  Params contracts.EnvelopeParams }`. `Consume(g, c Companion)`: field checks as
  today plus `AttemptNo` must equal the record's current attempt; for a Durable
  operation exactly ONE companion is REQUIRED and `c.Key == g.OperationID` is
  verified (a missing or foreign companion is `ErrAttemptNotAuthorized`, zero wire);
  the `s7.attempt_started` event and the companion are appended in ONE
  `journal.AppendBatch` BEFORE Consume returns (no wire without a durable STARTED —
  mirrors the loop's STARTED-before-dispatch discipline; codex r3 #2: S7 is the
  atomic committer of every paired S7+owner transition). A non-durable operation
  refuses a companion (no half-durable pairs). S7 validates only the opaque key and
  cardinality; it never interprets channel semantics.
  `Report(op, Outcome, code string, build func(Landing) Companion)` and
  `Cancel(op, build func(Landing) Companion)`: S7 first computes the typed `Landing`
  (`Retry{NextAt}` | `Terminal` | `Unknown` | `Succeeded` | `Cancelled`), the OWNER's
  builder returns the ONE companion for that landing (bound by `Key == op`), and both
  commit in one batch or not at all — there is no window in which S7 says "retry is
  safe" while the row is stranded `UNKNOWN`, and S7 never chooses between unlabeled
  candidates. A non-durable operation passes a nil builder; for a durable operation
  the builder may return the zero `Companion` ONLY for `Landing{Unknown}` (the row is
  already durably `UNKNOWN` from the Consume park — agy r5 note 3); every other landing
  requires a companion with `Key == op`. By construction the channel builds `Key` and
  the payload's `operation_id` from the same `DeliveryID` in the one `Flush` loop, so
  `Key == payload.operation_id == op` (kilo r5 note 1). The adapter's outcome is a
  PROPOSAL; S7 decides retryability from `Policy.RetryableCodes` — a code not in the
  list is terminal even if the adapter said retryable; `OutcomeUnknown` parks UNKNOWN
  and is NEVER retried (`IRREVERSIBLE+UNKNOWN` needs reconciliation, SPEC P0.2).
  `AttemptContext` unchanged (deadline = min(call, grant expiry, operation deadline)).
- Authorization-lease recovery (codex r3 #1): an issued-but-unconsumed grant is a
  LEASE with `nonce` + `expires_at`, persisted in `s7.attempt_authorized` for durable
  operations. `Next(op)` on an `AUTHORIZED` record whose lease has expired (or that was
  rehydrated with no `attempt_started` for it) durably appends
  `s7.lease_revoked{op, attempt_no, nonce_hash}` (transition
  `attempt.lease_revoked`: AUTHORIZED -> PLANNED) and then issues a FRESH grant (new
  nonce; `attempt_no` unchanged because no physical attempt was consumed; `attempts`
  counts CONSUMED attempts only). The old nonce is dead: it is not in memory and the
  projection marks it revoked. A rehydrated record WITH `attempt_started` and no
  report is RUNNING -> UNKNOWN (the wire may have been touched; never re-granted — E9;
  kilo r3 note 2 wording fixed: the distinction is made by the durable STARTED event,
  not by guessing).
- `Execute(ctx, op, target, Policy, attempt func(ctx, Grant) (Outcome, string, error)) error`
  (codex r3 #4): the S7-OWNED synchronous driver — Begin, Next, callback, Report,
  backoff wait under ctx, repeat until terminal; the CALLER submits one operation and
  never sleeps, loops, or calls `Next`. Delivery keeps the tick-polled `Next` style
  (S7 still decides due-ness; `Flush` neither waits nor loops). Both entry styles are
  S7 code; adapters/loops/planner contain no retry logic.
- `policy_json` rehydration fails closed on corrupt/unknown fields (agy r3 note 1).
- State machine (`contracts.AttemptState` + `machine.AttemptTable`): new state
  `AttemptFailedRetryable` APPENDED to the enum (existing numeric values unchanged),
  name `FAILED_RETRYABLE`; new events `attempt.failed_retryable` (RUNNING ->
  FAILED_RETRYABLE), `attempt.retry_authorized` (FAILED_RETRYABLE -> AUTHORIZED),
  `attempt.exhausted` (FAILED_RETRYABLE -> FAILED), `attempt.cancelled`
  (FAILED_RETRYABLE -> CANCELLED). Default-reject table stays the single truth.
- Durability (`Durable: true` operations only — an operation whose lifetime is one
  interactive turn cannot be asked for a grant after restart, so it is honestly
  in-memory; the loop already journals its turn-narrative `attempt.*` events):
  journal events `s7.operation_begun{op,target,policy}`, `s7.attempt_authorized{op,
  attempt_no,nonce_hash,expires_at}`, `s7.attempt_started{op,attempt_no}`,
  `s7.lease_revoked{op,attempt_no,nonce_hash}`, `s7.attempt_reported{op,attempt_no,
  outcome,code,next_attempt_at}`, `s7.operation_terminal{op,state}`; projection
  `s7_operations` (`op PRIMARY KEY, target, state, attempts, max_attempts, deadline,
  next_attempt_at, lease_nonce_hash, lease_expires_at, policy_json`). The nonce itself
  is never journaled (bearer secret); a SHA-256 of it identifies the lease. `s7.New(j, clock)` rehydrates every non-terminal durable operation;
  a rehydrated RUNNING record (durable STARTED, no report = crash mid-attempt)
  transitions to UNKNOWN (never re-granted); a rehydrated AUTHORIZED record (lease, no
  STARTED) is recovered through `lease_revoked` -> fresh grant (see lease recovery). An append failure on `attempt_authorized` refuses the
  grant (fail closed: no wire without a durable authorization).
- Invariants (tests): attempts never exceed MaxAttempts across restart; no grant
  before `next_attempt_at`; no grant after deadline; CANCELLED terminal from every
  non-terminal state; second `Consume` of the same grant refused; adapter/loop hold
  NO retry loop (they only ask `Next`).

### B2 — delivery wiring (channel core + telegram)
- Outbox statuses: `PENDING | UNKNOWN | SENT | FAILED` (new `FAILED` via event
  `channel.outbound_failed{delivery_id, code, reason}`; projection v3 rebuilds by
  replay). `UNKNOWN` stays reserved for a possibly-committed remote effect.
- Identity binding (codex r2 #2): operation `delivery:<delivery_id>`, target
  `channel:<adapter_id>:delivery:<delivery_id>` — the grant is bound to the immutable
  delivery resource, not the adapter. Companion binding (codex r4 #1): every outbox
  transition payload (`deliveryMark`, `outbound_failed`) gains `operation_id`; the
  channel PayloadValidator and projection fold REJECT a mark whose
  `operation_id != "delivery:"+delivery_id`, so a companion for row B can never ride
  A's grant even inside a valid batch. The adapter RECOMPUTES the expected operation and
  target from the `Outbound` it is about to send and compares them with the presented
  grant BEFORE `Consume` (mirrors `provider.go:156-172`); a mismatch is
  `ErrAttemptNotAuthorized` with zero wire calls.
- One ordered lifecycle per row (codex r2 #1) — S7 and the outbox land as ONE causal
  result; neither owner may claim success while the other is non-terminal:
  `Core.Flush(ctx, send func(Outbound, s7.Grant) error)`: for each `PENDING` row:
  `s7.Begin(delivery:<id>, target, PolicyDelivery)`; `g, err := s7.Next(op)`;
  `ErrNotDue` -> skip this tick; `ErrExhausted`/terminal -> mark `FAILED`; grant ->
  `send(o, g)`, inside which the adapter recomputes identity and calls
  `s7.Consume(g, Companion{Key: op, Params: outboxUnknownParams(op)})` — the S7
  STARTED and the outbox UNKNOWN park commit in ONE batch immediately before `client.Do` (if that batch fails: no wire,
  lease stays and is recovered by `Next` on the next tick) -> outcomes:
  - any LOCAL pre-consume refusal (marshal, request build, identity mismatch) ->
    `s7.Cancel(op, build)` where the builder returns the outbox `FAILED`
    (`local_refused`) companion — ONE batch (operation CANCELLED, row `FAILED`);
  - nil -> `Report(Succeeded, "", build)` (builder: `SENT` companion) — one batch -> `SENT`; if that
    batch fails after a remotely accepted send the row STAYS `UNKNOWN` (never claims
    `SENT`), S7 stays RUNNING -> rehydrates UNKNOWN, and the error surfaces;
  - `ErrAmbiguousSend` (HTTP 5xx after POST, post-write timeout/reset, accepted-but-
    malformed reply) -> `Report(Unknown)` -> stays `UNKNOWN` (reconciliation, NO
    resend — E9); a 5xx is NEVER a definite failure (codex r2 #3);
  - `channel.DefiniteFailure{Code, Retryable}` (new typed error; nothing committed
    remotely) -> `Report(outcome, Code, build)`: S7 DECIDES the `Landing`
    (`Retry{NextAt}` -> the builder returns the outbox `PENDING` re-pend companion;
    `Terminal` -> the `FAILED` companion) and commits S7 + companion in one batch.
  The old unconditional `Reconcile(id,false)` self-repend is deleted.
- Telegram: EVERY physical Bot API call carries a grant (codex r3 #3, r4 #2 — no
  ungoverned `client.Do` remains). `call(ctx, method, req, out, g s7.Grant, kind)`
  recomputes the expected operation/target for its kind and calls `Consume`
  IMMEDIATELY before `client.Do` (mirrors `provider.go:163-180`). The COMPLETE current
  call-site table (`telegram.go:249,347,392,396,400,463,475`;
  `telegram_picker.go:33,49,158,171`) and its governed paths:
  | method(s) | kind | operation / target | policy |
  | sendMessage, sendRichMessage (outbox `Flush`) | delivery | `delivery:<id>` / `channel:tg:delivery:<id>` | `PolicyDelivery` (durable) |
  | getUpdates | poll | `poll:tg:<n>` / `channel:tg:getUpdates` | `PolicyPoll` (RetryableCodes {`transport_prewire`, `http_429`, `http_5xx`, `transport_postwrite`, `malformed_reply`}) |
  | setMyCommands, getMe | control | `control:tg:<method>:<n>` / `channel:tg:<method>` | `PolicyControl` (MaxAttempts 3, backoff 2s..30s) |
  | sendChatAction (typing) | ui | `ui:tg:<chat>:typing:<n>` / `channel:tg:chat:<chat>` | `PolicyUI` (MaxAttempts 1, EffectReversible, terminal on failure, outcome landed, in-memory) |
  | picker sendMessage, answerCallbackQuery, editMessageReplyMarkup, editMessageText | ui | `ui:tg:<callback_or_message_id>:<method>` / `channel:tg:chat:<chat>` | `PolicyUI` |
  Classification is KIND/EFFECT-aware with ONE closed code vocabulary (codex r5 #3):
  `transport_prewire` (egress refusal / DNS / dial), `http_429`, `http_4xx`,
  `http_5xx`, `transport_postwrite` (timeout/reset after the request may have been
  sent), `malformed_reply`, `local_refused`. For the EFFECTFUL delivery kind, 5xx /
  post-write / malformed stay `ErrAmbiguousSend` -> `UNKNOWN` (E9: the remote may have
  processed it). For READ-ONLY / idempotent kinds (getUpdates re-reads the same durable
  offset and advances no admission; getMe; setMyCommands sets the same menu) those
  same failures are `DefiniteFailure{code, Retryable}` proposals — S7's policy decides.
  `PolicyPoll` and `PolicyControl` list exactly the codes above; a code not in a
  policy is terminal.
  The picker chrome is the owner-accepted EPHEMERAL boundary (cronjob plan; not a
  durable delivery) — it is governed one-shot, never retried, never routed through the
  outbox. A grant added "merely to compile" is impossible: `call` refuses a kind whose
  recomputed identity does not match the presented grant. Polling and command
  registration are governed operations (Slice D); a poll iteration is a scheduled
  operation (HARDQ C3), a failed one is re-attempted only through S7.
  Classification: pre-wire (egress refusal / DNS / dial) -> `DefiniteFailure{
  transport_prewire, Retryable}`; HTTP 429 -> `{http_429, Retryable}`; other 4xx ->
  `{http_4xx, Terminal}`; 5xx / post-write errors -> `ErrAmbiguousSend` (unchanged).
  An EGRESS refusal is additionally surfaced as a distinct redacted health/log event
  (kilo r2 note: poisoned DNS is a security signal, not a routine 4xx) — the health
  owner is Slice D; until D lands, a stderr line.
- `/outbox` lists `UNKNOWN` (as today) AND `FAILED` rows with code; `redeliver <id>`
  on a `FAILED` row begins a NEW human-authorized operation
  `delivery:<id>:r<attempts>` (`PolicyDelivery`) and re-pends the row — human
  authority starts a new operation; it never re-grants an exhausted one.
- Restart: `Flush` reads `PENDING` only; the rehydrated S7 record enforces
  attempts/deadline/next_attempt_at, so a restart cannot reset the cap.

### B3 — provider retry through the same engine
Planner submits ONE operation: `s7.Execute(ctx, op, target, PolicyProvider,
func(ctx, g) { out, err := Chat(ctx, msgs, g); return classify(err) })` — S7 runs
Next/callback/Report/backoff (codex r3 #4: the planner never sleeps, loops, or calls
`Next`). The provider classifies: `http_429`, `http_5xx` with NO body consumed,
dial/DNS -> `transport_prewire` (retryable proposals; a chat completion is
`EffectReadOnly` — re-issuing it has no remote side effect beyond cost); anything
after the body started is terminal; `ErrExhausted` -> the turn fails with the causal
error.
Structured output (codex r4 #3, r5 #2): `provider.Extract` today receives an
ALREADY-produced raw reply and then decides AND issues its own re-ask
(`structured.go:102-151`). v6 makes the initial generation and the re-ask ONE S7
operation so the attempt count is honest: `ExtractVia(ctx, e, class, validate, op,
target, generate func(ctx, g s7.Grant, reask bool) ([]byte, error))` runs
`s7.Execute(ctx, op, target, PolicyStructured, attempt)` where attempt 1 =
`generate(g, false)` + strict validate, and — for GENERAL only — an invalid result is
the typed proposal `OutcomeFailedRetryable, "invalid_general"`; attempt 2 =
`generate(g, true)` (the re-ask prompt) + validate. `PolicyStructured{MaxAttempts 2,
RetryableCodes {invalid_general}}` therefore means initial request + EXACTLY one
re-ask; two invalid results never cause a third call. SECURITY/EFFECT classes propose
`OutcomeFailedTerminal` on the first invalid result (no repair, no re-ask). The
salvage ladder (re-ask text, then the original) runs AFTER the operation is terminal
and is outside the transport count. The extractor never calls `Issue`/`Next`. The
planner's structured path submits this one operation (its plain-chat path submits
its own `PolicyProvider` operation; the two are distinct operations, never nested). `Stream` failures after the first delivered byte are TERMINAL (partial output
already reached the user). Tool attempts keep `PolicyTool` (MaxAttempts 1).

Detectors (RED against current code):
0. Lifecycle: after success, definite failure, ambiguity, pre-consume refusal, and
   park failure NO delivery operation remains `AUTHORIZED` or `RUNNING` in S7
   (`State(op)` asserted alongside the outbox row in the same test).
0b. Grant swap: two unused grants minted for rows A and B, A's grant presented with
   B's `Outbound` -> refused, ZERO wire calls (first-use substitution, not reuse).
0c. HTTP 5xx on send -> row `UNKNOWN`, S7 `UNKNOWN`, zero later wire calls.
1. `test_adapter_cannot_self_retry` (SPEC P0.2 literal): same grant twice -> second
   refused `ATTEMPT_NOT_AUTHORIZED`, transport counter 1, S7 attempts 1.
2. Two `PENDING` rows in one flush -> two DISTINCT grants; a send with a reused or
   missing grant is refused before `client.Do`.
3. Permanent HTTP 400 -> row `FAILED` after ONE attempt; the next N ticks produce
   ZERO wire calls (RED today: re-pend -> resend every tick).
4. Transient failure (429, then dial error) -> `FAILED_RETRYABLE`, row `PENDING`,
   ZERO wire calls before `next_attempt_at`, resend exactly when due; success -> `SENT`.
5. `MaxAttempts` retryable failures -> row `FAILED`, zero further wire calls; the
   exhaustion (and, separately, the deadline) landing is ONE batch: with an injected
   failure at the former inter-append seam there is no observable
   `S7=FAILED / outbox=PENDING` state (ablation to two appends -> RED).
6. Durability: 3 failures -> daemon restart (fresh process, same journal) ->
   `s7_operations` rehydrated -> remaining attempts = MaxAttempts-3; after they fail
   -> `FAILED`; a restart never resets the count (RED against in-memory-only).
7. Crash between Consume and Report (RUNNING) -> restart -> operation UNKNOWN, row
   UNKNOWN, ZERO re-grants.
8. Provider 429 -> retried once after backoff with a NEW grant, succeeds; provider
   HTTP 400 -> no retry, turn fails; stream mid-way failure -> no retry.
8b. Ownership ablation: S7's second-grant path disabled (MaxAttempts forced to 1)
   while planner/provider code is untouched -> the second transport call is ZERO
   (proves the planner holds no retry loop of its own).
9. `attempt_authorized` append failure -> no grant, no wire; `attempt_started`+park
   batch failure -> no wire, lease recovered on the next tick.
9b. Lease recovery: crash after `attempt_authorized` and before the STARTED+park batch
   -> restart -> exactly ONE fresh grant, the old nonce refused, consumed attempts
   never exceed MaxAttempts; an expired never-consumed lease at runtime -> same.
9c. Pair atomicity ablation: split the Report+sibling batch into two appends with an
   injected failure between -> a definitely-unsent row stranded `UNKNOWN` is DETECTED
   (RED), the batched recipe leaves no such state (GREEN); same for cancel+FAILED and
   terminal-report+FAILED.
9d. Bot API method TABLE (every method in the call-site table): the same grant
   presented twice -> second `client.Do` never happens (`ATTEMPT_NOT_AUTHORIZED`,
   transport count unchanged); a missing grant or a cross-kind swap (delivery-for-poll,
   ui-for-control) -> zero wire calls.
9e. Companion binding: `Consume` with NO companion for a `PolicyDelivery` grant ->
   refused, zero wire, no outbox change; a companion built for row B presented with
   A's grant -> refused by S7 (`Key != op`) AND, if forged past S7, by the channel
   validator (`operation_id` mismatch) — zero wire, neither row changes.
9f. Structured accounting + ownership: (a) invalid then valid -> exactly TWO transport
   calls, value accepted from the re-ask; (b) invalid, invalid -> exactly TWO calls
   (never a third), salvage only over those two; (c) ablation: S7's second-grant path
   disabled (MaxAttempts forced to 1) while extractor/provider code is untouched ->
   exactly ONE total transport call; (d) SECURITY/EFFECT invalid -> ONE call, no re-ask.
10. Egress refusal on delivery -> `DefiniteFailure{transport_prewire}` -> S7 retry
    path (not `UNKNOWN`) + distinct redacted security line (ties to Slice A #5).

## Slice C — F5 + F8(reads): context budget at the wire + bounded trust reads
Root: `internal/kernel/budget` has ZERO production importers; `budget.go:22-34`
measures only `[]ContextBlock` and `HardLimit<=0` DISABLES enforcement
(`budget.go:50-56`). The planner adds system prompt, tool protocol and time text
AFTER the blocks (`planner.go:253-287`), so a block-level check can pass while the
wire request exceeds the limit. `config.Config` has no limit field
(`config.go:29-52`). UDS `ReadString('\n')` unbounded for hello AND chat frames
(`daemon.go:157,202`); streaming accumulates without a total cap (`planner.go:331`).

Fix:
- Config owner: `ContextHardLimitTokens int` (`json:"context_hard_limit_tokens"`) in
  `config.Config`; default `64000`; `ValidateBounds` rejects `<= 0` and `> 2_000_000`
  (a zero/negative limit must NOT mean "unlimited": fail closed at resolve, never at
  `Enforce`). Wired at composition into the planner.
- Enforcement point: the planner measures the FINAL wire messages (role+content of
  every `ChatMessage`, incl. system/tool/time text) immediately before EVERY
  `Issue`/`Chat`/`Stream`; `budget` gains `MeasureMessages` (same len/4 heuristic +
  per-message overhead floor) and `Budget.EnforceMessages`. Over budget -> REFUSE
  (`ErrOverBudget`), NEVER trim/truncate (`budget.go:1-6`; trimming is S8.2). The loop
  treats the refusal as a terminal turn failure with the causal error surfaced to the
  user surface; no state advances, no grant is issued/consumed.
- UDS: bounded frame reader (`maxFrameBytes`, constant, e.g. 1 MiB) for BOTH the hello
  frame and every chat frame; an over-long frame -> `error` frame + CLOSE the
  connection in BOTH cases (residual bytes of the oversized line would desynchronize
  the next `ReadString` — agy r2 note), never an unbounded allocation.
- Stream accumulation: the planner's builder refuses growth beyond a total ceiling
  (`maxStreamTotal`, constant) and cancels the stream with an error; the provider
  transport ceiling is Slice A.

Detectors (RED against current code):
1. Provider spy: input blocks fit the budget but system+tool material pushes the
   final request over -> ZERO grants consumed, ZERO provider calls, `ErrOverBudget`
   surfaced. (RED today: budget never consulted.)
2. Config `context_hard_limit_tokens: 0` -> `Resolve` fails; `-1` fails; absent ->
   default 64000 applied.
3. Hello frame > `maxFrameBytes` -> rejected + closed; chat frame > `maxFrameBytes`
   whose tail is an aligned valid-looking `{"type":"chat",...}\n` -> rejected + closed
   and the suffix is NEVER processed as a second frame (two independent REDs, each
   against the unbounded `ReadString`; codex r2 note).
4. Never-ending stream -> cut at `maxStreamTotal` with an error, builder never exceeds
   the ceiling.

## Slice D — F6: channel-health owner with a journal-independent projection
Root: `Adapter.Run` returns nothing and discards `registerCommands`, `PollOnce`,
`FlushOutbox` results (`telegram.go:428-464`); `main.go:289-296` launches it in an
unsupervised goroutine. Four silent-death paths: setMyCommands, getUpdates,
FlushOutbox transport, FlushOutbox journal-mark. A health record kept ONLY in the
journal cannot report the journal-failure case.

Fix:
- One health owner `internal/channel/health` (`Report(component, class, redactedErr)`,
  `Snapshot()`): in-memory state + a failure-independent durable projection written
  with `atomicwrite.Write` to `<profile system dir>/channel_health.json` (redacted;
  no token, no message text) + one redacted stderr log line per state transition.
  Classes (closed enum, typed): `transport`, `remote_rejected`, `substrate`.
- Typed classification contract (codex r2 #5): the ORIGINATING owner returns
  `channel.ClassifiedError{Class, Code, cause}` preserved through wrapping
  (`errors.As`); health only RENDERS the type — no string matching. An error that
  carries NO class defaults to `substrate` (fatal, fail closed). `PollOnce`
  distinguishes `a.call` transport errors from `processUpdate`/journal errors;
  `Core.Flush` tags journal-mark failures `substrate` and send failures by the
  adapter's class.
- Polling is S7-governed (codex r2 #4; full S7 from Slice B): operation
  `poll:<adapter>:<n>` with `PolicyPoll` (Durable false, MaxAttempts 6, Backoff
  2s..5min jitter, RetryableCodes {`transport`, `http_5xx`, `http_429`}); a SUCCESSFUL
  poll completes the operation and the next tick begins `poll:<adapter>:<n+1>` (a
  scheduled iteration, HARDQ C3); a FAILED poll is retried ONLY when S7 issues the
  next grant (backoff), the ticker merely asks `Next`; the grant is PASSED INTO `PollOnce` and consumed by
  `call` immediately before `client.Do` (codex r3 #3 — D6 is the backoff detector,
  B 9d the authorization detector). `registerCommands` (an effectful POST) runs under
  `control:tg:setMyCommands` with `PolicyControl` (MaxAttempts 3, backoff 2s..30s).
  Terminal codes (401/403 =
  `remote_rejected`, exhausted retryable) -> `Run` returns; health records the class;
  the capability stays OFF/degraded until config/token repair AND daemon restart (the
  token is sealed at construction, `telegram.go:75-96`; a running process cannot
  observe a shell change — no "keeps polling so a token fix recovers").
  `substrate` -> `Run` returns immediately (no retry of a broken journal).
- `Adapter.Run` returns the typed `ClassifiedError`; the composition root supervises:
  on return it persists THAT class unchanged (`remote_rejected` stays
  `remote_rejected` — codex r3 #5), defaulting to `substrate` ONLY for an unclassified
  return, a recovered panic, or a failure of the health substrate itself; logs
  redacted, cancels the adapter context (prompt
  teardown of poll/flush goroutines — agy r2 note), and marks the runtime capability
  state degraded/OFF in the health projection. Sealed-capability rule preserved: we
  REPORT runtime health, never activate a fallback (`AGENTS.md:53-57`).
- `nexus doctor` reads the health projection when present and reports the last
  recorded class/time (Slice E touches doctor too; ordered E after D).

Detectors (RED against current code — every one asserts the externally visible
health state, not an internal counter):
1. Scripted permanent getUpdates 401 -> health `remote_rejected`, projection file
   written, stderr line present, `Run` returned; advancing several ticker intervals
   observes EXACTLY ONE failed poll (RED today: a poll every tick).
2. setMyCommands 5xx -> health `transport` degraded (RED: today discarded).
3. FlushOutbox transport failure -> health `transport` degraded.
4. Injected journal-mark failure in FlushOutbox -> health `substrate`, `Run` returns,
   NO further poll/flush wire calls (further channel work stops).
5. Table: wrapped journal failures from inbound admission and from EVERY outbox mark
   (`unknown`, `sent`, `failed`) + an error with no class -> all `substrate`, adapter
   stops; a transport control stays non-substrate and non-fatal.
6. Transient poll failure TABLE — pre-wire (DNS/dial refusal), HTTP 429, HTTP 5xx,
   post-write reset: each -> zero polls before S7 `next_attempt_at`, then exactly one
   new-grant poll when due; a success starts a fresh poll operation on the normal
   interval. (A code missing from `PolicyPoll` would turn the pre-wire or 5xx row
   terminal — RED.)

## Slice E — F7: config-aware doctor (three-case table)
Root: doctor hardcodes `NEXUS_API_KEY`/`NEXUS_TELEGRAM_TOKEN`
(`doctor.go:109-118,178-187`); `runDoctor` (`main.go:713-740`) never resolves config,
while production resolves names from typed config (`config.go:29-39,120-127`;
`main.go:84-95`).
Fix: `runDoctor` resolves configuration first (same resolver as the daemon; a
resolve failure is itself a doctor finding, not a crash) and passes a
`doctor.Secrets{ProviderKeyEnv, TelegramTokenEnv}` struct into the checks; no
hardcoded names remain in doctor.
Detectors (through the REAL `runDoctor` config path, table-driven):
1. custom names configured + custom populated + defaults absent -> ON (false-negative
   RED today).
2. custom names configured + custom ABSENT + defaults POPULATED -> OFF (the causal
   false-positive RED: today reports ON by reading the default name).
3. default names configured + defaults populated -> ON (regression guard).

## Slice F — F3: release toolchain floor + govulncheck gate at the sign boundary
Root: `go.mod:3` is `go 1.26`; `scripts/p0-accept.sh` builds `$BIN` (`:65-67`),
grades (`:69-75`), signs (`:81-87`), publishes the trust pointer (`:23-46,87`) and may
install (`:92-93`) with no toolchain/vulnerability gate.
Fix (code): `go.mod` gains `toolchain go1.26.6` — with `GOTOOLCHAIN=auto` (the host
default, verified) Go 1.26.4 downloads and builds with 1.26.6 automatically, so NO host
upgrade is required for the floor (the owner may still upgrade the system Go; it is
no longer a blocker). In `p0-accept.sh`, immediately AFTER building `$BIN` and BEFORE
grading/signing: (a) the version recorded IN the binary (`go version "$BIN"`) parsed
and compared to floor `1.26.6` (fail closed on parse failure); (b)
`govulncheck -mode=binary "$BIN"` on that EXACT binary — missing checker, scanner
error, or ANY finding -> exit non-zero before sign/publish/install. The floor is one
variable at the top of the script. `govulncheck` is installed on the dev box via
`go install golang.org/x/vuln/cmd/govulncheck@latest` (dev tool, not a module dep).
- Deploy gating (codex r2 #7): the SAME gate runs inside the autodeploy step
  (`scripts/deploy.sh`, new: build -> version floor -> govulncheck -> install ->
  restart -> verify capability ON). Both scripts source ONE helper
  `scripts/lib/release-gate.sh` (`release_gate "$BIN"`) so the acceptance and deploy
  checks cannot drift (agy r3 note 3). No slice's binary is installed unless the gate is
  GREEN. Slice F is therefore built FIRST (see Order).
Detectors (whole-script shim tests with an ISOLATED release key + publication target,
`PATH`-shimmed `go`/`govulncheck`):
1. Old toolchain shim -> script exits non-zero; `ssh-keygen -Y sign`, trust-pointer
   switch and install are NEVER reached.
2. Acceptable toolchain + scanner shim reporting one finding -> same: never reached.
3. Acceptable toolchain + clean scan -> the script proceeds to the next gate (proves
   the detector is not "always fail").
4. Missing `govulncheck` -> fail closed.
5. `scripts/deploy.sh` shim test: old toolchain or a finding -> install/restart never
   reached (orchestration RED); clean -> reaches install.

## Order + review
F -> A -> B1 -> B2 -> B3 -> C -> D -> E. F FIRST: its toolchain floor + govulncheck
gate guards every later autodeploy (codex r2 #7) and the `toolchain` directive removes
the host-upgrade dependency. E after D (doctor reads the health projection). D after
B (polling uses the S7 engine). Constitution touch: `AGENTS.md:57-60` and
`ESSENTIALS E6` gain an inline change-record that full S7 landed in P1 by owner
decision (HARDQ A2 deferral lifted). Each slice: branch, RED-before/GREEN-after, 3-agent review to
3xPASS, merge to main, then AUTODEPLOY (build -> ~/bin/nexus -> restart -> verify
capability ON). F4/F9 already landed on `slice/p1-audit`.

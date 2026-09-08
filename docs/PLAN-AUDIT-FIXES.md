# PLAN v2: full-audit remediation (codex AUDIT-FULL-2026-09-08) — zero-defect

Source: `docs/AUDIT-FULL-codex-2026-09-08.md` (9 findings; bound at f135eef,
reverified against current tree). v2 folds plan-review round 1
(`docs/REVIEW-AUDIT-PLAN-{codex,kilo,agy}.md`, 3x FAIL). Every fix: RED-capable
detector at the real owner boundary, then 3-agent review to 3xPASS.

## Status
- [x] **F4 (HIGH)** acceptance subprocess-output race — DONE (96b0c49). Round-1
  verified closed (codex: removing the fix makes the package fail to compile with
  the landed callers = causal RED; ceiling: host race detector unavailable).
- [x] **F9 (LOW)** duplicate JSON config keys — DONE (96b0c49). Round-1 verified
  revert-proof (deleting the guard -> `TestDuplicateJSONKeysRejected` RED).
- [ ] F1 F2 F3 F5 F6 F7 F8 — slices A-F below.

## Cross-slice invariants (bind every slice)
- No new retry owner, no default-transport fallback, no fail-open path, no silent
  truncation. `s7min` is P0 no-retry (`AGENTS.md:57-60`, `s7min.go:3-9`): ONE grant
  per operation; a second `Issue` for the same operation is refused.
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
  registration/projection compat) and its payload gains `component`.
  `channel.Core.RecordEgress` is deleted (its body moves to the sink constructor);
  telegram receives the sink instead of building its own.
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
  a real provider reply exceeding them).

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

## Slice B — F2: grant-per-physical-send, definite failure parks FAILED (P0 no-retry)
**OWNER DECISION (2026-09-08, pending confirmation): option 1 below. Option 2 is
recorded so the tradeoff is explicit.**
Root: `channel.go:583-589` re-pends definite failures to `PENDING`; the Run loop
`FlushOutbox`es every tick (`telegram.go:433-442`); `Adapter.call` reaches
`client.Do` (`telegram.go:193-223`) with no `AttemptGrant`. Unbounded retry outside
S7 (`AGENTS.md:19-22`; `tasks-P0.md:464-471`). `Core.Flush` sends one row per
callback (`channel.go:554-568`), so a single outer grant would cover many sends.

Option 1 (CHOSEN — P0-conformant, minimal machinery): `s7min` UNCHANGED.
- Operation identity = `delivery:<delivery_id>` (stable, durable); target =
  `channel:<adapter_id>`. `Core.Flush` issues ONE grant per row and passes it to the
  send callback; the adapter CONSUMES it immediately before `client.Do` (mirrors
  `provider.go:163-180`). No grant, no wire.
- Outcomes: accept -> `SENT`; ambiguous (wire touched) -> `UNKNOWN` (unchanged);
  DEFINITE failure (pre-wire refusal, DNS/dial error, terminal HTTP 4xx, or any
  non-ambiguous transport error) -> new durable outbox status **`FAILED`**
  (`attempts` recorded, redacted reason recorded). `FAILED` is distinct from
  `UNKNOWN` (`UNKNOWN` stays reserved for possibly-committed remote effect). The
  `Reconcile(id,false)` -> `PENDING` self-repend path is REMOVED from `Flush`.
- Restart durability: `Flush` reads only `PENDING`; attempted rows are `SENT`,
  `UNKNOWN` or `FAILED` — an in-memory S7 table reset cannot resurrect an attempt.
- Visibility: `/outbox` (and `nexus outbox`) lists `FAILED` rows with reason;
  `TestDeliveryHonesty` is amended to assert the new terminal state.
- Tradeoff (explicit): a transient failure (one DNS blip, one 5xx) parks the message
  terminally; nothing auto-resends. Operator resurrection of a `FAILED` row is OUT
  of this slice (ceiling; upgrade trigger: a real parked row that the owner wants
  re-sent -> Option 2 slice).
Option 2 (NOT chosen now): the P2 full-S7 retry engine as its OWN authorized slice
(S7-owned durable policy: attempt count, deadline, `next_attempt_at` backoff, exhausted
state; only IT issues a second grant). Larger scope; deferred.

Detectors (RED against current code):
1. Two `PENDING` rows in one flush -> two DISTINCT grants consumed; a send with a
   reused grant is refused `ErrAttemptNotAuthorized` before `client.Do`.
2. Send callback invoked without a live grant -> refused, zero wire calls.
3. Permanent HTTP 400 -> row `FAILED`; the next N ticks produce ZERO wire calls
   (RED today: re-pend -> resend every tick).
4. Definite failure -> daemon restart (fresh `Authority`) -> ZERO wire calls for
   that row (durability across restart).
5. Pre-wire egress refusal -> `FAILED`, not `UNKNOWN` (ties to Slice A #5).

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
  frame and every chat frame; an over-long frame -> `error` frame + close (hello) or
  `error` frame (chat), never an unbounded allocation.
- Stream accumulation: the planner's builder refuses growth beyond a total ceiling
  (`maxStreamTotal`, constant) and cancels the stream with an error; the provider
  transport ceiling is Slice A.

Detectors (RED against current code):
1. Provider spy: input blocks fit the budget but system+tool material pushes the
   final request over -> ZERO grants consumed, ZERO provider calls, `ErrOverBudget`
   surfaced. (RED today: budget never consulted.)
2. Config `context_hard_limit_tokens: 0` -> `Resolve` fails; `-1` fails; absent ->
   default 64000 applied.
3. Hello frame > `maxFrameBytes` -> rejected; chat frame > `maxFrameBytes` -> rejected
   (two independent REDs, each against the unbounded `ReadString`).
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
  Classes: `transport` (degraded, keeps running), `remote_rejected` (e.g. 401
  revoked token: degraded, keeps polling so a token fix recovers), `substrate`
  (journal append/mark failure: FATAL — `Run` returns an error and STOPS all channel
  work).
- `Adapter.Run` returns `error`; the composition root supervises: on return it
  records `substrate` health, logs redacted, and marks the runtime capability
  state degraded/OFF in the health projection. Sealed-capability rule preserved: we
  REPORT runtime health, never activate a fallback (`AGENTS.md:53-57`).
- `nexus doctor` reads the health projection when present and reports the last
  recorded class/time (Slice E touches doctor too; ordered E after D).

Detectors (RED against current code — every one asserts the externally visible
health state, not an internal counter):
1. Scripted permanent getUpdates 401 -> health `remote_rejected`, projection file
   written, stderr line present.
2. setMyCommands 5xx -> health `transport` degraded (RED: today discarded).
3. FlushOutbox transport failure -> health `transport` degraded.
4. Injected journal-mark failure in FlushOutbox -> health `substrate`, `Run` returns,
   NO further poll/flush wire calls (further channel work stops).

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
Fix (code): in `p0-accept.sh`, immediately AFTER building `$BIN` and BEFORE grading/
signing: (a) `go version` parsed and compared to floor `1.26.6` (fail closed on parse
failure); (b) `govulncheck -mode=binary "$BIN"` on that EXACT binary — missing
checker, scanner error, or ANY finding -> exit non-zero before sign/publish/install.
`go.mod` gains `toolchain go1.26.6`. The floor is one variable at the top of the
script.
HOST DEPENDENCY (owner): the Pi's Go (1.26.4 today) must be upgraded to >= 1.26.6
before a clean sign. The gate is the code deliverable; the upgrade is an owner action.
Detectors (whole-script shim tests with an ISOLATED release key + publication target,
`PATH`-shimmed `go`/`govulncheck`):
1. Old toolchain shim -> script exits non-zero; `ssh-keygen -Y sign`, trust-pointer
   switch and install are NEVER reached.
2. Acceptable toolchain + scanner shim reporting one finding -> same: never reached.
3. Acceptable toolchain + clean scan -> the script proceeds to the next gate (proves
   the detector is not "always fail").
4. Missing `govulncheck` -> fail closed.

## Order + review
A -> B -> C -> D -> E -> F (E after D: doctor reads the health projection; F last:
owner Go upgrade). Each slice: branch, RED-before/GREEN-after, 3-agent review to
3xPASS, merge to main, then AUTODEPLOY (build -> ~/bin/nexus -> restart -> verify
capability ON). F4/F9 already landed on `slice/p1-audit`.

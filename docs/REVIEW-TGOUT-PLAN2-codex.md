# PLAN-TGOUT v2 Review — Codex

## Scope and artifact binding

- Target: `docs/PLAN-TGOUT.md` at immutable commit `c1c827cd122f322f3b909041b415c126d9e54062` on `slice/p0-tgout`.
- Mode: design-only, read-only review. The only repository write is this requested report.
- Governing owners checked: `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARDQ-CONSOLIDATED.md`, Annex A in `docs/HARNESS-SPEC.md`, and `docs/tasks-P0.md`, all read from the target commit.
- Implementation surfaces checked at the same commit: `internal/llm/planner/planner.go`, `internal/channel/channel.go`, and `internal/channel/telegram/telegram.go`.

## Result

FAIL. The planner retry violation from round 1 is removed, and the tokenize/escape/wrap and lossless-table rules are materially clearer. However, v2 still has two delivery-contract violations, an unsound drift claim, an unwired error classifier, and inadequate crash/boundary detectors.

## Findings

### F1 — Chunk restart behavior contradicts the locked UNKNOWN-first contract

[CONCRETE][SEV: HIGH] `docs/PLAN-TGOUT.md:88` — The plan says a crash between chunk sends “redelivers all chunks” under the same delivery ID. HARDQ B2 requires `sent-but-unrecorded → UNKNOWN → RECONCILING`, never blind retry (`docs/HARDQ-CONSOLIDATED.md:53-61`), and E15 repeats that rule (`docs/ARCHITECTURE-ESSENTIALS.md:196-205`). The current core deliberately parks the row UNKNOWN before touching the wire, so a crash during the send callback makes it ineligible for another flush (`internal/channel/channel.go:488-528`). A local delivery ID is not a Telegram idempotency key. If chunk 1 was accepted and the process dies before recording anything about it, automatically replaying every chunk is exactly the forbidden blind retry.

PROBE: Static state-machine trace from plan crash claim through the immutable B2 owner and `Core.Flush` UNKNOWN-first transition.

FIX: State that any crash after the first wire call leaves the aggregate delivery UNKNOWN and requires reconciliation; do not promise automatic all-chunk redelivery. If resumable chunk delivery is required, design durable per-chunk identity/progress and its reconciliation rules first, while retaining UNKNOWN for every sent-but-unrecorded chunk.

### F2 — A later chunk failure is misclassified after an earlier chunk was accepted

[CONCRETE][SEV: HIGH] `docs/PLAN-TGOUT.md:88` — One `Core.Flush` callback currently represents one remote effect. Chunking turns it into several effects. After chunk 1 succeeds, a definite 400 or pre-wire failure on chunk 2 does not mean “nothing left the process,” yet the existing non-ambiguous error branch re-pends the whole row on exactly that premise (`internal/channel/channel.go:529-534`). The plan's “PENDING/UNKNOWN per current outbox semantics” wording at lines 94-101 does not define the required partial-delivery state and can cause duplicated accepted prefixes on the next flush.

PROBE: Static counterexample: chunk 1 returns HTTP 200; chunk 2 returns a non-entity HTTP 400; the callback returns an ordinary error; current `Core.Flush` resolves the delivery back to PENDING.

FIX: Define aggregate classification explicitly: once any chunk may have been accepted, every later failure leaves the delivery UNKNOWN, never PENDING. Define what evidence can reconcile a partially accepted sequence and which message IDs/chunk indexes must be durable.

### F3 — Plain fallback is an adapter-owned second physical delivery attempt

[CONCRETE][SEV: HIGH] `docs/PLAN-TGOUT.md:94` — On an entity-parse 400, the adapter immediately performs one plain resend. That is a second physical `sendMessage` call initiated by the adapter. Reusing the delivery ID does not reuse or create S7 authority. Annex A P0.2 prohibits adapter retry loops and allows only S7 to issue the next attempt (`docs/HARNESS-SPEC.md:1386-1393`); E15 specifically says S7 alone authorizes and schedules delivery retries (`docs/ARCHITECTURE-ESSENTIALS.md:196-205`). The plan specifies journal visibility but no fresh `AttemptGrant`, S7 transition, or no-retry exception.

PROBE: Attempt count from the proposed detector itself: formatted call returns 400, then plain call executes, for two physical transport calls inside one adapter callback.

FIX: Remove the adapter-local resend, or promote the fallback to a typed retry proposal consumed by S7 and require a fresh grant for the plain attempt. The latter also needs an explicit amendment if P0 `s7-min` remains no-retry (`docs/tasks-P0.md:154-159`).

### F4 — The narrowed error predicate is not reachable through the current transport API

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:94` — Matching raw Telegram `description` text containing `can't parse entities` is a reasonable narrow signature for the observed HTML/Markdown parse failures, but the current `call` returns `telegram: <method> HTTP 400` without reading the response body (`internal/channel/telegram/telegram.go:160-166`). Telegram places the human-readable `description` in the JSON error response, not in the status line ([Bot API response contract](https://core.telegram.org/bots/api)). The plan never adds a typed error envelope or otherwise surfaces that field, so its predicate cannot fire against the current implementation. The exact phrase is also human-readable text rather than a documented stable error code; matching should at least be case-normalized and jointly gated by method/status.

PROBE: Static wire trace plus the official Bot API response schema. A representative real response is `Bad Request: can't parse entities: ...`, but current code discards it before classification ([tdlib/telegram-bot-api example](https://github.com/tdlib/telegram-bot-api/issues/265)).

FIX: Specify bounded decoding of non-2xx Bot API envelopes into a typed error carrying method, status/error_code, and sanitized description. Gate fallback on `sendMessage`, HTTP/error code 400, and a case-normalized entity-parse signature; define malformed/missing-description behavior as no fallback.

### F5 — The drift classifier still has a known-tool collision

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:52` — Narrowing from any `action` field to a known tool ID removes common false positives, but it does not justify the claim that the classifier “cannot fire on legitimate JSON answers.” A user can legitimately ask for an example of NEXUS tool metadata and receive `{"tool_id":"memory_recall","description":"..."}`, or ask for an action object and receive `{"action":"memory_recall","enabled":true}`. Both are valid requested JSON and both match v2. Because the same unlabelled model text is used for final answers and protocol messages (`internal/llm/planner/planner.go:175-195,263-285`), no content-only known-ID heuristic can make that distinction sound.

PROBE: Static collision against the exact predicate using a registered tool ID in an otherwise legitimate JSON final.

FIX: Either document this as an intentional fail-closed output restriction and remove the soundness claim, or introduce an unambiguous provider/tool response discriminator. At minimum, add the exact known-ID collision as the false-positive detector; the proposed “unrelated action value” case does not test the remaining ambiguity.

### F6 — The line-boundary chunking algorithm is incomplete

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:88` — “Split ... on line boundaries into <=4000-char chunks” has no result for one line longer than 4000 characters. A long URL, minified JSON, table cell, or code line is a direct counterexample. A forced split after HTML rendering can also cut an escape entity or renderer-owned tag; only `<pre>` close/reopen behavior is specified, not inline `<b>`/`<code>` tokens. Telegram limits `sendMessage.text` to 1-4096 characters after entity parsing ([official `sendMessage` contract](https://core.telegram.org/bots/api#sendmessage)), so the measurement domain and Unicode-safe split rule must be explicit.

PROBE: Static input `strings.Repeat("x", 5000)` with no newline; no legal line boundary produces the promised chunks.

FIX: Chunk renderer tokens before final wrapping; hard-split oversized text tokens on a defined Unicode boundary, never inside an entity/tag, then independently wrap balanced tags per chunk. Define the length measure against Telegram's post-entity text limit.

### F7 — The detector set cannot prove the new delivery semantics

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:102` — The proposed adapter tests cover successful chunking and final SENT timing but omit the decisive failure surfaces:

- crash/restart after each accepted chunk and before each durable mark;
- chunk 1 accepted followed by chunk N definite, ambiguous, or entity-parse failure;
- a single over-limit line, escaping expansion, Unicode, and a split adjacent to every renderer tag/entity;
- assertion that every physical fallback call has a distinct S7 grant and legal terminal transition;
- an exact Bot API 400 JSON envelope, plus malformed and missing descriptions;
- a legitimate JSON final whose `action` or `tool_id` equals a registered tool ID.

The listed chunker/fallback ablations do not substitute for the Annex A requirement that the oracle observe durable state and the external sink (`docs/HARNESS-SPEC.md:1364-1367`).

PROBE: Detector-to-invariant comparison against F1-F6 and the required B2 after-remote-accept crash seam (`docs/HARDQ-CONSOLIDATED.md:53-61`).

FIX: Add a fake transport with per-call accept/fail/crash seams and durable-state assertions across reopen; add the exact planner collision and transport-envelope cases above; include controlled ablations of UNKNOWN-first and grant enforcement.

### F8 — The proposed hard-coded final violates the repository language directive

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:59` — The proposed production string is Croatian. The repository instruction requires everything produced here, including code, to be English. This round-1 finding remains unresolved.

PROBE: Literal comparison of the proposed string with the repository's explicit English-only rule.

FIX: Specify the typed final in English; localization, if later desired, needs an owned localization boundary rather than a Croatian kernel literal.

## Requested-axis answers

- Drift classifier sound: **No.** It is narrower, but known-tool JSON remains indistinguishable from a legitimate final.
- Chunk redelivery honest under crash: **No.** Automatic replay contradicts UNKNOWN-first, and partial delivery is not modeled.
- Narrow fallback covers real entity strings: **Partially in intent, not operationally.** The canonical raw phrase is covered, but the current transport discards `description`, the string is not a stable typed code, and malformed/case variants are unspecified.
- Remaining S7/delivery violation: **Yes.** The plain resend is adapter-owned; multi-call chunk aggregation also breaks the current delivery error contract.
- Detector adequacy: **No.** The crash, partial-effect, grant, exact-response-envelope, long-line, and known-ID collision cases are missing.

## What v2 resolves

- Planner schema drift no longer causes a second provider call; the proposed one-call/one-grant assertions are appropriate for that path.
- Tokenize → escape token content → wrap renderer-owned tags is the correct HTML safety order at plan level.
- Empty, surplus, and unparseable table-row rules close the specific round-1 loss cases at plan level.
- Restricting fallback to entity parsing is directionally correct; F3-F4 concern ownership and reachable classification, not a request to broaden it back to all 400s.

## Proof ceiling

This is a static design review. No implementation for this slice exists at the target revision, so no RED/GREEN execution was possible and no live Telegram call was made. The strongest unresolved link is the absence of a durable, S7-owned state model for a delivery that now contains multiple remote calls.

VERDICT: FAIL

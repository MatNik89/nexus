# PLAN-TGOUT v9 review — Codex — round 9

## Review binding

- Reviewed commit: `b2a2359379a3cbf81b369ceae3db7361e8f3dd16` (`docs(tgout): plan v9 — narrowed drift walk, journal-derived attempts, absolute wording removed`).
- Reviewed tree: `9ef4bbb65d70df1fb75b6dd40e8c2df49c3d50f8`.
- The review used a clean on-disk `git archive` export under `/home/matej/HARNESS/`; `git get-tar-commit-id` returned the full commit above, and the exported `docs/PLAN-TGOUT.md` blob matched `b2a2359:docs/PLAN-TGOUT.md` as `9a1bae99de95cd75f3a3db7ec0d3621c226c03f1`.
- The source worktree's untracked prior review files were excluded from evidence.
- Scope: design only; no production code or tests were changed or executed.

## Findings

### 1. [HIGH][CONCRETE] The required cross-tick resend bypasses the sole retry owner

The plan requires a formatted send on one flush tick and, after a parse rejection, a plain send on the next tick (`docs/PLAN-TGOUT.md:113-136`). That is a second physical delivery attempt. The plan assigns its scheduling to the adapter's “next regular flush tick” and specifies no S7 authorization or fresh `AttemptGrant` for either transport call.

This conflicts with the locked owner rules: retry/deadline/cancel belongs only to S7 and every physical attempt carries a grant (`docs/ARCHITECTURE-ESSENTIALS.md:68-75`); the Telegram-specific contract says S7 alone authorizes and schedules delivery retries (`docs/ARCHITECTURE-ESSENTIALS.md:196-205`); HARDQ's binding P0 order explicitly requires Telegram inbox/outbox plus S7-owned retries (`docs/HARDQ-CONSOLIDATED.md:22-38`); and Annex P0.2 forbids an adapter-owned retry loop (`docs/HARNESS-SPEC.md:1386-1393`). The current path demonstrates the missing owner seam: `Adapter.Run` calls `FlushOutbox` directly each tick (`internal/channel/telegram/telegram.go:301-315`), while `Core.Flush` invokes the transport callback without any grant (`internal/channel/channel.go:488-537`).

The proposed parse-400 detector would normalize this violation: it requires two sends but asserts neither two unique S7 grants nor S7's decision to authorize the second attempt.

PROBE: static end-to-end trace from timer → `FlushOutbox` → `Core.Flush` → transport callback, compared with the cited locked owners.

FIX: Make S7 authorize and schedule every Telegram wire attempt; classify the definite parse rejection as the input to S7's delivery policy, require a fresh single-use grant for the plain retry, and make the detector assert one consumed grant per transport call plus zero adapter-initiated ungranted retries.

### 2. [MEDIUM][CONCRETE] `channel.outbound_unknown` cannot prove “wire-attempted”

The plan says formatting is allowed only when a row has never been wire-attempted, then derives that fact by counting `channel.outbound_unknown` (`docs/PLAN-TGOUT.md:121-130`). In the real state machine that event is durably appended *before* the send callback is invoked (`internal/channel/channel.go:488-514`). Therefore it proves only that a delivery was parked in-flight, not that the wire was attempted.

Concrete counterexamples are a daemon crash after the UNKNOWN append but before the callback, a malformed channel identity rejected inside the callback before `a.call`, and a DNS/dial failure classified pre-wire (`internal/channel/telegram/telegram.go:114-166,288-295`). After proof-of-loss/re-pending, the first actual wire send will be plain because the projected count is already nonzero. Delivery remains safe, but v9's stated first-wire-attempt formatting semantics are false and the degradation is absent from the declared tradeoffs and detectors.

PROBE: static event-order trace; no destructive or live Telegram probe was used.

FIX: Use the smaller honest contract “HTML only on the first in-flight lease” and commit pre-wire/crash degradation cases, or introduce a journaled signal that actually distinguishes dispatch from pre-wire failure. Do not call the pre-wire UNKNOWN event a wire-attempt fact.

### 3. [MEDIUM][CONCRETE] The detector does not prove journal derivation or versioned rebuild

V9 makes two new durability claims: attempts are derived from canonical events, and a `Projection.Version()` bump rebuilds old databases (`docs/PLAN-TGOUT.md:124-130`). Its detector performs two flushes and ablates only the adapter's use of the count (`docs/PLAN-TGOUT.md:132-136`). That detector can stay GREEN if the count is updated through a noncanonical mutable path or if fresh databases work while upgrade/refold loses the count. It therefore does not causally verify either new round-8 ownership claim.

The actual projection framework resets a version-mismatched projection and replays the journal (`internal/kernel/journal/journal.go:343-383`), so this is directly testable without a second production mechanism.

PROBE: detector-to-claim comparison plus projection rebuild trace.

FIX: Add a committed upgrade/refold detector that seeds the v1 event sequence, opens it with the bumped channel projection, and proves the reconstructed attempt state forces the next send plain. Ablating only the `EvOutboundUnknown` fold (and separately omitting the version bump against a v1 schema) must turn the detector RED.

### 4. [MEDIUM][CONCRETE] A crash can erase the typed drift outcome at the Telegram edge

The plan journals `error_code` in `turn.failed` and promises that the Telegram edge maps `TOOL_SCHEMA_DRIFT` to the Croatian rephrase message (`docs/PLAN-TGOUT.md:85-101`), but it does not specify replay of that failed terminal outcome. This matters in the existing crash window after `turn.failed` is durable and before the inbound-terminal + outbox recipe commits.

On redelivery, the same turn IDs are reused; `RunChannelTurn` handles the duplicate by recovering only a successful/suspended final (`internal/app/daemon/daemon.go:263-279,411-446`). A durable `turn.failed` code is not recovered. The handler then falls through to the generic Telegram failure reply (`internal/channel/telegram/telegram.go:257-276`), so the promised structured edge mapping is not stable across restart and replay.

PROBE: static crash-window trace through the existing deterministic turn ID and recovery branches.

FIX: Specify failed-turn recovery by `turn_id` and structured `error_code`, then add a crash/reopen detector between `turn.failed` and `CompleteInbound`; replay must enqueue the same Croatian drift response and must not re-run the model turn.

### 5. [LOW][CONCRETE] Duplicate-key coverage is not crossed with the fence-normalization path

The prose requires the token walk to inspect all occurrences of `action`/`tool_id`/`name`, but the ordered algorithm runs the strict check on the raw reply before constructing the one-fence-stripped classification view (`docs/PLAN-TGOUT.md:71-84`). The detector list covers generic drift dialects as bare and single-fenced, while the explicit duplicate case is only stated separately (`docs/PLAN-TGOUT.md:77-80,107-111`). A faulty implementation can token-walk duplicates in a bare object, then use last-member-wins decoding after fence stripping, and satisfy the listed cases unless the duplicate/fence dimensions are crossed.

PROBE: detector matrix review against the ordered branches.

FIX: Commit the single-fenced duplicate/alias case (including `{"action":"memory_recall","action":"tool"}`) and ablate token walking specifically on the normalized classification view.

## Round-8 disposition

- The drift scan is textually narrowed to the three named keys and includes the two requested committed examples.
- The absolute “no second attempt ever exists” wording is removed.
- The journal-derived attempt design is stated, but Findings 2 and 3 show that its source event and detector do not establish the claimed semantics.

## Weakest link / proof ceiling

The strongest unresolved defect is retry ownership: v9's recovery behavior requires and tests an adapter-tick-driven second physical send while the binding architecture requires S7 to authorize and schedule it. This was a design-only review; static analysis does not prove how a future implementation will behave, and no live Telegram or executable mutation probe was appropriate.

VERDICT: FAIL

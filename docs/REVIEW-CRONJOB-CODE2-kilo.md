# Review of `/cronjob` code slice r2 (`211e9dd`)

**Scope**: commit `211e9dd` on `slice/p1-cronjob` — the round-1 detector
additions only (`picker_callback_test.go`, `main_test.go`); production code is
unchanged from the previously-reviewed `fee4022`.
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The round-1 F1 (missing detectors) is closed with non-vacuous,
contract-anchored, RED-capable tests. Build clean; all new tests pass. No new
substantive residual — the production code is byte-identical to the prior
review.

## Round-1 F1 — closure status

### Missing detectors — closed

- **`picker_callback_test.go`** (new, 4 tests):
  - `TestCallbackForeignOwnerRefused` — a tapper `from.id != chat` is refused,
    the session stage is untouched, zero `channel.inbound_admitted` events, and
    `answerCallbackQuery` still fired (spinner cleared). Proves callback trust +
    ephemeral-no-admission together.
  - `TestCallbackIllegalInStateRefused` — an hour tap in the month stage is
    answered but does not advance the session.
  - `TestCallbackExpiredOrMalformedRefused` — unknown `sid` and garbage
    `callback_data` are both answered benignly.
  - `TestCallbackFullFlowReachesAwait` — day→hour→minute reaches `AwaitingPick`
    with the exact tapped `2026-09-14 09:30`, and exactly 3 answers were sent.
- **`cmd/nexus/main_test.go::TestCronjobReconfirmBeforeGate`** (new):
  seeds a durable reminder, drops all session state (empty store), then asserts
  `cronjobDescription` reconfirms from the persisted `body`+`wall` via the
  front `ReminderIntent` lookup (handled=true, correct text), that a replay is
  idempotent (same reply, no second create), and that a different update with
  no reminder/session is NOT captured (`handled=false`). The test comment names
  the exact ablation (move the lookup behind `AwaitingPick`) that turns it RED.

These cover the three round-1 gaps precisely: (a) lookup-behind-gate,
(b) duplicate-create-on-replay, (c) callback owner/state/answer.

## Re-confirmed (unchanged production, still valid)

The durable path, callback trust, reminder correctness, delivery honesty, and
single-goroutine concurrency analysis from the prior review (`fee4022`) all
still hold — no production lines changed. The four standing notes remain:
session-pointer mutation outside the mutex (correct only under the
single-getUpdates-goroutine invariant), the description-vs-command interaction,
the `cancel`/`odustani` reserved words, and the stale-UI-after-failed-edit
edge.

VERDICT: PASS

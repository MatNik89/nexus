# PREP3 Verification Round 4 — Codex

Scope: `slice/p0-prep3` at `45065cd46360d2c542bfd166d7dbc349d816ae56`, reviewing the combined fold in `b0457cf0c5809cd554819a2840b0ab757de63c19` and `45065cd46360d2c542bfd166d7dbc349d816ae56`. Tests ran against the untouched real repository; mutations ran only in clean `git archive` exports. I did not inspect another reviewer's report.

## Finding

### [CONCRETE][SEV: LOW] A transient busy result can still falsely certify a phase-B completed-turn kill

`pollQuery` represents SQLite `busy`/`locked` as `(0, nil)` so a polling predicate can retry (`internal/acceptance/chaos_linux_test.go:483-499`). The fold now uses that helper once to capture phase B's baseline and treats `(0, nil)` as a valid count (`internal/acceptance/chaos_linux_test.go:188-199`). After one earlier reply exists, a transient lock therefore changes the baseline to zero; the following predicate immediately succeeds on the earlier reply and can kill the new update before its own reply is enqueued. This defeats phase B's stated guarantee that each of its three kills lands after a completed turn.

PROBE: In a clean HEAD export, I simulated `pollQuery`'s documented transient result by setting the later phase-B baseline samples to zero. The probe logged `terminal-at-kill=0` for updates 5 and 6, while the entire `TestChaosKillSurvival` still passed. This is a demonstrated false completion, not only a possible flake.

FIX: Preserve the transient state separately from a real zero (for example, return a sentinel error that `waitStore` retries), and acquire the baseline through a bounded loop that accepts only a successful database sample.

## Fold verification

- **Phase-D no-auto-resend oracle: PASS.** After calm restart, the harness waits five seconds without an owner command, requires the exact acceptance count to remain unchanged, and requires the target row to remain `UNKNOWN` (`internal/acceptance/chaos_linux_test.go:264-278`). It then excludes that delivery ID from the generic reconciliation loop, submits one owner command, drains to `SENT`, and requires exactly one additional acceptance (`internal/acceptance/chaos_linux_test.go:279-337`).
- **Duplicate-send sensitivity: PASS.** In a clean HEAD export, I changed production `telegram.Adapter.FlushOutbox` to issue a second `sendMessage` only for deterministic phase-D text `reply-for chaos-msg-011`. The test failed for the intended reason: `phase D: EXACTLY one extra arrival required after ONE owner redeliver (1 -> 3)`.
- **Phase-D quiescence hardening: PASS.** The barrier is armed only after both pending outbox rows and admitted inbox rows reach zero, with a 40-second deadline; stray filtering permits ten attempts (`internal/acceptance/chaos_linux_test.go:220-256`). No new defect was found in this narrow change.
- **Phase-B busy tolerance: FAIL.** The direct fatal query was removed, but the helper's zero-on-transient encoding is unsafe for a baseline sample, as proven above.

## Commands and results

- `git branch --show-current; git rev-parse HEAD; git show b0457cf; git show 45065cd` — PASS; branch and exact revision matched, and both requested commits were inspected.
- `CGO_ENABLED=0 go test -count=3 -run '^TestChaosKillSurvival$' -v ./internal/acceptance 2>&1 | tee /tmp/nexus-prep3-r4-codex-chaos3.log` followed by a literal `FAIL` scan — PASS; three executions passed in 36.08 s, 32.12 s, and 36.22 s, with no `FAIL` token.
- Clean archive duplicate-send production mutation, then `CGO_ENABLED=0 go test -count=1 -run '^TestChaosKillSurvival$' -v ./internal/acceptance` — EXPECTED FAIL; exit 1 with the exact-count phase-D assertion (`1 -> 3`).
- Clean archive phase-B transient-zero negative control, then the same focused command — UNEXPECTED PASS; updates 5 and 6 were each confirmed non-terminal at their purported completed-turn kill boundary, yet the harness returned exit 0.

## Topknot and proof ceiling

The fold is otherwise small and reuses existing helpers; no new dependency or speculative abstraction was added. The weakest link is that one helper value currently conflates a valid count of zero with a retryable read failure. The three green untouched repetitions establish current-host stability for this sample, but cannot establish the phase-B completed-turn boundary when SQLite returns busy/locked.

VERDICT: FAIL

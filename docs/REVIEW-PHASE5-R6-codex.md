# Phase 5 verification round 6 — Codex

Reviewed immutable target: `d7226c1fd73ffcfe0dfc856bfa8845f6c96f68be` on `slice/p0-phase5`.

Result: **PASS**. The sole round-5 finding is fixed causally and completely in the requested narrow scope. No new defect was found in the production/test diff.

## Verification of the fold claim

1. **[OK] Replay integrity errors now reach the caller.** `completedTurnFinal` returns `(string, bool, error)` and returns the exact `Journal.Replay` error on any replay failure (`internal/app/daemon/daemon.go:256-276`). Its only caller checks that error before accepting a recovered final and wraps it as `daemon: completed-turn recovery refused: %w` (`internal/app/daemon/daemon.go:231-247`). A corrupt canonical stream therefore takes precedence over the incidental duplicate-event error from re-entering the completed turn.

2. **[OK] The committed detector checks the previously missing observable.** `TestRecoveryFailsClosedOnCorruptJournal` corrupts the integrity hash of an event after the completed turn, redelivers the same update, requires an error, and requires `chain broken` in the surfaced error (`internal/app/daemon/daemon_test.go:455-514`). The test reaches `RunChannelTurn`, not the helper in isolation.

3. **[OK] Independent strengthened clean-export probe passed.** I strengthened the test only in a clean `git archive HEAD` export to require all of `daemon: completed-turn recovery refused:`, `journal replay`, `chain broken`, and `hash mismatch`. The focused test passed, proving the public caller receives the causal replay diagnosis rather than merely any error.

4. **[OK] Propagation ablation is RED-capable.** In a separate clean export, I kept the test and changed only the `rerr != nil` branch to return the original `RunTurn` error. The detector failed with `replay integrity error was not propagated: ... UNIQUE constraint failed: events.event_id (2067)`. This is the exact old symptom and establishes causality.

## New-defect hunt

No new defect was confirmed in `d7226c1^..d7226c1`. The signature change has one caller and it is updated; intact completed-turn recovery is unchanged; no-error/no-final still returns the original execution error; and the replay error is wrapped with `%w`, preserving its error chain. The change adds no dependency, state, concurrency, authorization, or persistence transition. Lean already.

Top three weakest points, none blocking:

1. The committed assertion uses error text because replay integrity failures have no typed sentinel; wording changes could require test maintenance, but the clean probe confirms the current causal payload.
2. Recovery still replays the full journal, the explicitly accepted P1 performance ceiling.
3. The detector injects a tail hash mismatch rather than every possible `Replay` failure, but the implementation returns the single `Replay` error without class-specific branches, so this covers the changed propagation path.

## Verification evidence

```text
CGO_ENABLED=0 go test ./...
exit 0 — all packages passed

clean export, strengthened TestRecoveryFailsClosedOnCorruptJournal
exit 0 — PASS

clean export, propagation ablated while detector retained
exit 1 — RED with the original duplicate-event symptom
```

The full suite ran in the repository because `internal/buildcheck` requires `.git`. All probe modifications were confined to fresh directories under `/tmp`; the repository was not modified except for this requested report.

Skill update: none — round 5 already added the necessary distinction between fail-closed behavior and propagation of the causal error; the existing audit workflow caught and verified it.

VERDICT: PASS

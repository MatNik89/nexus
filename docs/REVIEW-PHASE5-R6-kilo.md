# Phase 5 verification round 6 — Kilo (narrow)

Target: `slice/p0-phase5` at HEAD `d7226c1` (fold of my single round-5 finding; kilo/agy PASSED round 5, so this is codex-only scope: confirm `d7226c1` introduces no NEW defect). Method: code-read `git show d7226c1`, run the suite in the repo, re-run the ablation probe in a clean `git archive HEAD` export (`/tmp/kilo/r6-export`). Working tree never modified.

## Fold claim verified

The diff changes only `internal/app/daemon/daemon.go` (plus the test):

- `completedTurnFinal` now returns `(string, bool, error)`, propagating `Journal.Replay`'s error (`daemon.go:256-280`); on a chain break it returns `("", false, err)` instead of `("", false)`.
- `RunChannelTurn` checks the error and surfaces `"daemon: completed-turn recovery refused: %w"` (`daemon.go:237-243`) — the decisive hash-chain failure, not the duplicate-event symptom.
- `TestRecoveryFailsClosedOnCorruptJournal` (`daemon_test.go:507-514`) now requires `strings.Contains(rerr.Error(), "chain broken")`.

Correctness: `Journal.Replay` verifies the chain (`journal.go:634-640`) and returns `"journal replay: chain broken at offset N (hash mismatch)"` for a corrupted `integrity_hash`; the propagation wraps it, so the user-visible error contains `chain broken`. `completedTurnFinal` has exactly one caller (`RunChannelTurn`), correctly updated.

## Ablation (causal proof)

In a clean export I replaced the propagation branch with `recovered, ok, _ := d.completedTurnFinal(turn); if ok { ... }` (discarding the error). Result:

```
--- FAIL: TestRecoveryFailsClosedOnCorruptJournal (0.01s)
    daemon_test.go:513: replay integrity error was not propagated: loop: journal turn.created:
    journal append: constraint failed: UNIQUE constraint failed: events.event_id (2067)
```

The detector turns RED with the duplicate-event symptom — exactly the codex round-5 finding — proving the propagation is causal, not cosmetic.

## Suite

`CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 32 packages `ok`. `TestRecoveryFailsClosedOnCorruptJournal` passes at HEAD.

## New-defect hunt

None. The change is a pure error-propagation upgrade: for an intact journal with no `turn.succeeded`, `completedTurnFinal` returns `("", false, nil)` and `RunChannelTurn` falls through to the original error (unchanged behavior); a non-nil `Replay` error only ever means the canonical stream is unreadable/corrupt, so the refusal cannot misfire. No secret leaks (the propagated text is a chain diagnostic), no signature-mismatch at any other call site.

## Verdict

The fold claim is real and causal (ablation RED, test green), and `d7226c1` introduces no NEW defect.

VERDICT: PASS

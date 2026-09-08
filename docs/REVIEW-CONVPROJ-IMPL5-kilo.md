# Review of convproj implementation round 5 (`f4889a8`)

**Scope**: commit `f4889a8` on `slice/p0-convproj` (re-commits the hook lost to a
checkout revert in round 4).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The hook is now declared and wired, the export builds clean, the full
suite is GREEN, and I verified each of the three round-3 detectors is
RED-capable by performing the ablation, observing RED, and restoring.

## Verification performed

### Build and GREEN

- `go build ./...` — clean; `go vet` on `daemon`/`journal`/`conv` — clean.
- GREEN (all PASS, `-count=1`):
  `TestConvProjectionMatchesReferenceOnEveryPrefix` (with the
  `malformed-succeed` kind), `TestConvProjectionRebuild`,
  `TestOrdinaryFailureSkipsRecovery`,
  `TestRedeliveredUpdateCannotRerunCompletedTurn`,
  `TestConversationHistoryRules`,
  `TestDuplicateEventSignalIsEventIDOnly`, `TestDuplicateEventIDRejected`.

### Hook wiring (round-4 fix) — correct

`var testRecoveryVerifyHook func()` is declared (`daemon.go:71`) and invoked
just before the recovery-path `VerifyChain`
(`daemon.go:489-491: if testRecoveryVerifyHook != nil { testRecoveryVerifyHook() }`).
It counts *entry into the recovery path*, which is the exact signal the test
needs. No race: set by the test before the synchronous `RunChannelTurn` and
reset in `t.Cleanup`; nil (no-op) in production.

### RED-capability, proven by ablation (then restored)

1. **Hook/gate detector** — ablated `if !errors.Is(err, journal.ErrDuplicateEvent)`
   → `if false`. RED: `ordinary first-run failure ran recovery/VerifyChain 1 times`.
   Confirms the gate is what the test locks.
2. **Collision-signal detector** — ablated the `eventIDExists` pre-check →
   `if true`. RED: `a non-event-id constraint failure was misclassified as a
   collision: … UNIQUE constraint failed: events.journal_offset`. Confirms the
   exact event-id pre-check (not the broad string match) is what the test locks.
3. **Malformed-succeed oracle kind** — ablated the `json.Unmarshal != nil`
   guard in `conv.go` → `if false`. RED: `recovery diverged … proj=(false)
   ref=SUCCEEDED`. Confirms the oracle catches a fold that stops discarding a
   malformed final.

After each ablation `git checkout` restored the file; final `git status` is
clean and a re-run of the two fast detectors plus `go build` is GREEN.

## Notes (not FAIL reasons)

- `testRecoveryVerifyHook` is a package-level mutable test hook living in the
  production file `daemon.go` (shipped nil-by-default). A common Go idiom, but
  a `_test.go`-scoped hook or an interface seam would keep test plumbing out of
  the production binary. No correctness impact.
- The hook fires *before* `VerifyChain`, so it counts "recovery entered"
  rather than "VerifyChain completed" — correct for the gate's purpose; if a
  future assertion needed "scan actually ran to completion" the hook would
  need to move after the `VerifyChain` call.

## Verified clean

- `eventIDExists` pre-check inside the same tx, no TOCTOU, exact event-id
  classification (unchanged from round 3).
- `malformed-succeed` (`{"final":123}`) present in the oracle `kinds`
  (`convproj_diff_test.go:88,111-113`).
- Gate keeps `VerifyChain` on the cold redelivery path; hot history stays
  O(12).
- Differential oracle, rebuild (non-vacuous, projection-vs-reference),
  redelivery conformance, and journal duplicate-signal detectors all green.

VERDICT: PASS

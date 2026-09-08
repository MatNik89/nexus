# Review of convproj implementation round 2 (`9430b37`)

**Scope**: commit `9430b37` on `slice/p0-convproj` (folds impl-r1 findings).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

The three fixes are correct where applied, and I verified closure by running
the tests (`go test`, read-only repo, on-disk export):
`TestRedeliveredUpdateCannotRerunCompletedTurn`,
`TestOrdinaryFailedCollisionStaysGeneric`,
`TestMixedLifecycleRecoversDriftNotChallenge`,
`TestResumedWithoutTerminalSuppressesChallenge`,
`TestConvProjectionMatchesReferenceOnEveryPrefix`,
`TestConvProjectionRebuild`, `TestConversationHistoryRules`,
`TestDriftTurnExcludedFromHistory` all PASS; `TestDuplicateEventIDRejected`
PASS. No substantive (correctness / equivalence / unbuildable) flaw found.
One finding is stated incorrectly in the dispatch and must be corrected.

## Verification (as asked)

### ErrDuplicateEvent classification — correct, verified end-to-end

- **Real redelivery surfaces it.** On a crash between `turn.succeeded` and the
  inbound-terminal mark, the adapter's `Admit` returns `Replayed=true` with
  non-terminal status, falls through to `handle` → `RunChannelTurn`
  (`telegram.go:253-270`), whose `RunTurn` re-appends the deterministic turn
  event ids → `insertInTx` hits `event_id UNIQUE` (`journal.go:514-516`) →
  `ErrDuplicateEvent` → gate passes → recovery. Verified: the redelivery
  conformance tests (which require the recovered final, not a false failure)
  PASS against the real `modernc.org/sqlite` driver, proving both the
  string-match fires and `errors.Is` survives the actor round-trip.
- **No non-collision path falsely matches.** The only reachable UNIQUE
  violation is `event_id` (redelivery). `UNIQUE(run_id, sequence)` cannot
  collide (serialized actor + `MAX(sequence)+1`, `journal.go:486-490`); NOT
  NULL is unreachable (all columns validated/set). Provider/planner errors
  never contain the matched substrings, so the round-2 goal — an ordinary
  first-run failure returns directly with no `VerifyChain` scan — holds.

### VerifyChain remains cold-path only

`recoveredTurnOutcome` (the only `VerifyChain` caller, `daemon.go:481`) is
reached solely through the `ErrDuplicateEvent` gate in `RunChannelTurn`'s
error branch (`daemon.go:284`). Hot `conversationHistory` stays the O(12)
query. Confirmed by inspection.

### Rebuild detector non-vacuous — verified

`TestConvProjectionRebuild` now asserts non-empty pre-rebuild history and
compares projection to `referenceConversationHistory` BOTH before and after
the refold (`convproj_diff_test.go:214-247`). It also fixes the fixture
turn-id mismatch (`i` not `i+1`) that had made the old test vacuous. PASS.

## Notes (not FAIL reasons)

1. **The dispatch claim "(1) … the differential oracle gains a
   malformed-succeed kind" is not true.** The oracle's `kinds` list is
   unchanged (`convproj_diff_test.go:88`) — no malformed-succeed case was
   added, and there is no dedicated malformed-final test anywhere. The
   `conv.go` fix itself IS applied and correct (`conv.go:106-108` mirrors
   `daemon.go:362`'s `json.Unmarshal == nil` guard), but it is **untested by
   the oracle**, so the malformed-parity is not locked by the differential
   suite. Unreachable in production (the daemon always emits valid JSON;
   machine events carry nil validators = trusted-by-construction), so it is a
   coverage gap, not a correctness flaw — but the finding's "catch" was not
   delivered.
2. **`ErrDuplicateEvent` uses string-matching on the driver error**
   (`strings.Contains(err.Error(), "UNIQUE constraint") || … "constraint
   failed")`). It works today (proven by the passing redelivery tests), but it
   is fragile across driver versions and the `|| "constraint failed"` branch
   would also match a NOT NULL or `(run_id, sequence)` violation. Those are
   unreachable in production, so no false positive today; a type-assertion on
   the driver's error code would be more robust.
3. **Misclassification cost on a hypothetical `(run_id, sequence)` bug**: it
   would be labeled a redelivery, run one extra `VerifyChain`, then return the
   original error (recovery finds `ok=false`); the underlying SQLite error is
   preserved via `%v`, so the diagnosis is not lost — minor.

## Verified clean

- `turn.succeeded` now returns on a JSON decode error — parity with the
  reference for malformed finals.
- `ErrDuplicateEvent` is defined as a sentinel (`journal.go:644`) and wrapped
  with `%w`, preserving `errors.Is` through `appendBatch`/actor/`Append`.
- Recovery mapping (`rec_final` for SUCCEEDED, `susp_summary` for SUSPENDED),
  `hist_done` admission-guard, `hist_seq`/`created_seq` split, partial index,
  current-turn exclusion, and the O(12) hot path all remain as reviewed in
  round 1.

VERDICT: PASS

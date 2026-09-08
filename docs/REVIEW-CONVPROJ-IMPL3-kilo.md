# Review of convproj implementation round 3 (`62f28d3`)

**Scope**: commit `62f28d3` on `slice/p0-convproj` (folds impl-r2 findings).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

The one substantive change — the `eventIDExists` collision pre-check — is
correct, and I verified closure by running the tests (read-only, on-disk):
`TestDuplicateEventSignalIsEventIDOnly`, `TestDuplicateEventIDRejected`,
`TestOrdinaryFailureSkipsRecovery`,
`TestRedeliveredUpdateCannotRerunCompletedTurn`,
`TestConvProjectionMatchesReferenceOnEveryPrefix`,
`TestConvProjectionRebuild` all PASS. No substantive (correctness /
equivalence / unbuildable) flaw found. Two of the four dispatch claims are
not actually delivered and are flagged below as notes.

## Verification (as asked)

### eventIDExists pre-check — correct

`insertInTx`, on any INSERT failure, now classifies by `eventIDExists(tx,
string(env.EventID))` (`journal.go:514-518`):

- **Same transaction**: `eventIDExists` runs `tx.QueryRow` on the same
  `*sql.Tx` that just failed the INSERT — the check is inside the append
  transaction, and a UNIQUE violation is a statement-level SQLite error that
  does not abort the transaction, so the SELECT works (proven by the passing
  `TestDuplicateEventSignalIsEventIDOnly`).
- **No TOCTOU**: the journal is a single-writer actor (serialized appends);
  no concurrent writer exists between the failed INSERT and the SELECT, and
  the SELECT sees committed state plus this tx's (empty, since the INSERT
  failed) uncommitted state.
- **Correctness**: event ids are globally unique, so `eventIDExists == true`
  iff this exact event was already appended — a redelivery — and the
  classification no longer depends on the driver's error string. A
  `UNIQUE(run_id, sequence)` or NOT NULL failure with a fresh event id returns
  `false` → `journal append: …`, never `ErrDuplicateEvent`. Exact and
  driver-string-independent.

## Notes (not FAIL reasons)

1. **"(2) malformed-succeed oracle kind" is again not delivered.** The
   oracle's `kinds` list is unchanged
   (`convproj_diff_test.go:89`: `admit/succeed/empty-succeed/suspend/resume/
   fail/ordinary-fail`) — no malformed-succeed case exists, and there is no
   other malformed-final test. The `conv.go` decode-error guard (correct, from
   round 2) remains **untested by the differential oracle**. This is the
   second consecutive dispatch that claims this kind exists when it does not;
   the finding was not actually folded.
2. **"(3) TestOrdinaryFailureSkipsRecovery does not lock the skip."** The
   test asserts `err != nil` and `!contains "recovery refused"`, but on a
   healthy journal the recovery path returns the *same* ordinary error
   (`case rec.Kind == "FAILED": return "", err`, `daemon.go:302-305`), and
   "recovery refused" only appears when `VerifyChain` fails on a corrupt
   journal. So the test cannot distinguish gate-present from gate-removed; it
   is a smoke test, not a RED-capable lock on "no recovery/VerifyChain". To
   lock it, run the ordinary-failure case against a corrupt journal (then a
   broken gate yields "recovery refused").
3. **`TestDuplicateEventSignalIsEventIDOnly` negative is a surrogate.** The
   comment admits it cannot force a real `UNIQUE(run_id, sequence)` clash, so
   it asserts the negative via a fresh append. Acceptable — that constraint is
   unreachable through the serialized `MAX(sequence)+1` path
   (`journal.go:486-490`) — but the negative is weaker than its name implies.

## Verified clean

- `eventIDExists` narrows the sentinel to `event_id` only; no string matching
  on the driver error remains.
- `ErrDuplicateEvent` sentinel wrapped with `%w`, surviving actor round-trip.
- Gate placement (`daemon.go:284`) keeps `VerifyChain` on the cold
  redelivery path only; hot `conversationHistory` stays the O(12) query.
- Rebuild detector (non-empty + pre/post vs reference) unchanged and passing.
- Oracle, redelivery recovery, history conformance all still green.

VERDICT: PASS

# Implementation Review Round 3: conversation projection

## Result

**FAIL.** The production `event_id` collision classification at commit
`62f28d316f42a9f8703c5d468c3a8f8044ad7b95` is correct at the tested SQLite
boundary: the existence query executes after the failed insert, through the same
`*sql.Tx`, under the journal's `_txlock=immediate` transaction and serialized append
actor. It sees an earlier uncommitted member of the same batch, distinguishes a
`journal_offset` constraint failure, and introduces no check/insert TOCTOU window.

The claimed review closure is nevertheless not present. Four round-2 detectors
remain GREEN when their claimed protection is removed, and the promised
`malformed-succeed` oracle kind is absent. The unchanged latency detector also
remains GREEN when the required history index is replaced with an irrelevant one.
These are substantive blockers under this repository's mandatory red-capable
detector rule, not editorial notes.

## Artifact binding

- Review target: `62f28d316f42a9f8703c5d468c3a8f8044ad7b95`
  (tree `d624de9f95f91491afdc7da5d8084e9138f49e59`).
- Target parent: `caa65182758b86d688c9d2335d4c5cb8d15954c0`.
- Evidence source: clean `git archive` export at
  `/home/matej/HARNESS/nexus-convproj-impl3.us9UqD`, on disk and outside
  `/tmp`; the export was deleted after verification.
- Archive SHA-256:
  `ad0a317a08f52573cb6697d89333ec3b850a976494b09738b4ed0cf4161791f0`.
- Exported scoped blobs for `convproj_diff_test.go`, `daemon.go`, `conv.go`,
  `journal.go`, `journal_test.go`, `projection.go`, and `PLAN-CONVPROJ.md`
  matched the target's Git blob IDs.
- Tests and probes ran as UID 1000 with Go 1.26.4 on Linux/ARM64.
- After the export was bound, concurrent uncommitted changes appeared in
  `convproj_diff_test.go`, `daemon.go`, and `journal_test.go`, together
  with an untracked `docs/REVIEW-CONVPROJ-IMPL3-agy.md`. They were not used
  as evidence or modified; this verdict applies only to the immutable target.

## Substantive findings

### 1. [CONCRETE][SEV: MED] The exact-collision regression does not exercise a non-event-ID constraint

`internal/kernel/journal/journal_test.go:733-755` says it proves that
`UNIQUE(run_id,sequence)` and other constraints are not redelivery collisions, but
its negative case is a fresh append that succeeds. No competing constraint fails.
Restoring the old broad `"UNIQUE constraint" || "constraint failed"` classifier in
production while retaining `TestDuplicateEventSignalIsEventIDOnly` left the test
GREEN:

```text
ok github.com/MatNik89/nexus/internal/kernel/journal
```

An external package-local probe inserted a distinct row at the actor's next
`journal_offset`; the production append failed that primary-key constraint and did
not return `ErrDuplicateEvent`. Thus production is correct, but the committed test
does not causally guard the round-2 defect.

PROBE: controlled old-classifier mutation with the committed test retained; separate real SQLite non-event constraint probe.

FIX: make the committed negative case induce a distinct `journal_offset` or other non-event-ID constraint failure and assert both a real failure and `!errors.Is(err, ErrDuplicateEvent)`.

### 2. [CONCRETE][SEV: MED] The malformed-success differential case is still absent

The oracle kind list at `internal/app/daemon/convproj_diff_test.go:87-120` contains
`admit`, `succeed`, `empty-succeed`, `suspend`, `resume`, `fail`, and
`ordinary-fail`; it has no `malformed-succeed` case. Replacing the production
decode guard at `internal/conv/conv.go:98-109` with ignored `json.Unmarshal` while
retaining the committed differential test left
`TestConvProjectionMatchesReferenceOnEveryPrefix` GREEN in 29.655s.

PROBE: controlled malformed-final guard ablation with the committed oracle retained.

FIX: add an explicit deterministic malformed-success prefix (or a guaranteed generated kind) and demonstrate divergence from the retained reference when only the production decode guard is removed.

### 3. [CONCRETE][SEV: MED] `TestOrdinaryFailureSkipsRecovery` does not observe recovery or `VerifyChain`

The production gate at `internal/app/daemon/daemon.go:278-288` is correct, but the
new test at `internal/app/daemon/convproj_diff_test.go:253-274` only checks returned
errors. On an ordinary failure, ungated recovery finds no recoverable outcome and
returns the same original error; on the repeated failed turn, both correct and
incorrect paths also return an error. Removing only the
`errors.Is(err, journal.ErrDuplicateEvent)` gate left
`TestOrdinaryFailureSkipsRecovery` GREEN:

```text
ok github.com/MatNik89/nexus/internal/app/daemon
```

PROBE: controlled collision-gate ablation with the new committed test retained.

FIX: use a `VerifyChain` counting/failing seam or a corrupt-chain fixture that makes any ordinary-path recovery call observably fail, while separately asserting a real collision invokes recovery.

### 4. [CONCRETE][SEV: MED] The rebuild detector still does not prove version-triggered rebuilding

`internal/app/daemon/convproj_diff_test.go:193-251` proves a non-empty answer and
pre/post equivalence, but the already-correct projection rows remain available if
the version mismatch is ignored. Changing
`internal/kernel/journal/journal.go:355` from
`missing || ver != sp.Version()` to `missing` left
`TestConvProjectionRebuild` GREEN.

PROBE: controlled version-mismatch bypass with the committed rebuild test retained.

FIX: poison or add projection-only state before reopen and assert `Reset` removes it; also assert the rebuilt checkpoint and rows carry the current projection version.

### 5. [CONCRETE][SEV: MED] The root-cause index still has no causal detector

The production query/index alignment at `internal/conv/conv.go:29-55,184-207` is
correct, but the round-2 latency finding is unchanged. Replacing
`conv_hist(identity,hist_seq) WHERE hist_done=1 AND hist_seq IS NOT NULL` with
`conv_hist(turn_id)` left `TestConvProjectionFasterThanReference` GREEN in 8.549s.
The test also still asserts 10x at
`internal/app/daemon/convproj_diff_test.go:149-189`, while the owner plan requires
20x at `docs/PLAN-CONVPROJ.md:80-84`.

This remains substantive because the missing bounded history lookup is the direct
cause class of the soak backlog failure.

PROBE: controlled index ablation with the committed latency detector retained.

FIX: add a causal `EXPLAIN QUERY PLAN`/statement-step or bounded-growth assertion for `conv_hist`; retain timing only as supporting evidence and align its threshold with the owner.

## Transaction and TOCTOU analysis

The implementation's comment calls `eventIDExists` a pre-check, but operationally it
is a post-failure classification query:

1. `insertInTx` attempts the `INSERT` at `journal.go:503-509`.
2. Only when that statement fails, `eventIDExists(tx, ...)` runs at
   `journal.go:519`.
3. The helper queries through the same transaction at `journal.go:647-652`.

This ordering is preferable to a pre-insert check because it adds no check/use gap.
`Open` configures `_txlock=immediate` at `journal.go:169`, and all normal appends are
serialized by the append actor. A direct `AppendBatch` probe with the same event ID
twice proved that the query sees the first member's uncommitted row; a separate
`journal_offset` collision proved that a different constraint is not classified as
`ErrDuplicateEvent`.

The remaining ceiling is external violation of the journal's sole-writer contract or
a database/transaction failure that also prevents the classification query. The
helper deliberately returns false on every query error, so such a case preserves the
original insert failure rather than falsely claiming redelivery. No practical
TOCTOU or same-transaction visibility defect was found.

## Verification

- `CGO_ENABLED=0 /home/matej/.local/go/bin/go test -count=1 ./...` passed all
  discovered packages from the immutable export. Notable durations: acceptance
  120.891s, daemon 52.677s, sandbox 37.039s, probe 55.724s.
- `CGO_ENABLED=0 /home/matej/.local/go/bin/go vet ./...` exited 0.
- Focused baseline tests for the exact-collision signal, prefix differential,
  rebuild, and ordinary-failure gate passed.
- External SQLite probe passed both same-batch uncommitted duplicate visibility and
  rejection of non-event constraint misclassification.
- Five controlled ablations remained GREEN as recorded above; therefore the suite
  is green but not causally closed for the reviewed findings.

## NEXUS audit questions

1. **Solves the reported class? PARTIAL.** Production collision classification and
   ordinary-failure gating are correct, but committed proof for four claimed closures
   and the index mechanism is absent.
2. **Regression? NO production regression found.** The same-transaction classifier
   behaved correctly under the tested visibility and competing-constraint cases.
3. **Detector validity? NO.** Four claimed detectors and the latency detector remain
   GREEN under direct protection ablations; malformed-success is not generated.
4. **Revert-proof? NO.** The malformed guard, collision gate, version rebuild,
   exact-classifier distinction, and index mechanism are not causally locked by the
   named tests.
5. **Class sweep? YES.** Insert constraints, actor/transaction ownership, batch
   visibility, daemon recovery call sites, reference/projection folds, rebuild owner,
   query/index alignment, and the round-2 reports were traced.

## Counterargument, Topknot pass, and proof ceiling

The strongest counterargument is that production behavior is correct in every direct
probe and the full suite is green. That is not enough for PASS: this repository makes
observed RED capability mandatory for behavioral changes, and each failed ablation
targets the precise protection claimed by this revision.

Lean already. The production classifier is a small owner-local change with no new
dependency or abstraction. The required work is stronger assertions/fixtures, not
more production machinery.

Minimal Diff: no production files changed; this report is the only intentional repository write.
-> proof: immutable clean-export full suite and vet GREEN at UID 1000; five protection ablations falsely GREEN; external transaction/classification probe GREEN.
-> weakest link: no concurrent hostile writer fault injection or long quiet soak was run.
-> skipped: live Telegram/provider canary and post-fix soak; upgrade when explicitly authorized or owner-scheduled.
Proof ceiling: local Linux/ARM64 immutable-tree evidence proves the tested SQLite transaction behavior, not live delivery or long-run backlog elimination.
Status: FAIL at `62f28d316f42a9f8703c5d468c3a8f8044ad7b95`
topknot: ultra+preflight

VERDICT: FAIL

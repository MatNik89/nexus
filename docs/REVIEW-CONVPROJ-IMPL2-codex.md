# Implementation Review Round 2: conversation projection

## Result

**FAIL.** The three production changes described in the dispatch are present at
`9430b378b1be3407a3e9c9c07e1c2de27704f5c9`, and direct probes confirm that
malformed `turn.succeeded` payloads no longer complete history and ordinary
first-run planner failures no longer enter full-chain recovery. Real completed-turn
redelivery also reaches the new duplicate-event signal and recovers correctly.

The revision is not ready under this repository's RED-capability rule. The committed
suite stays GREEN when the malformed-final fix or the collision-only gate is removed,
and the repaired rebuild test stays GREEN when version-mismatch rebuild is bypassed
entirely. The latency detector also remains GREEN after replacing the intended partial
history index with an irrelevant index. In addition, `ErrDuplicateEvent` is not
classified exactly: a collision on the distinct `journal_offset` primary key is
reported as a duplicate event ID and can falsely enter recovery.

## Artifact binding

- Review target: `9430b378b1be3407a3e9c9c07e1c2de27704f5c9`
  (tree `9dd2820cd4dd719354cf5c22f5d48b9f34c2bc59`).
- Evidence source: clean `git archive` export at
  `/home/matej/HARNESS/nexus-convproj-impl2.Z8g2xS`, on disk and outside
  `/tmp`.
- Archive SHA-256:
  `467bf099d3c819258a5ed262659fcea27740c001f334cd504bf586e3a40ab115`.
- The scoped exported blobs for `convproj_diff_test.go`, `daemon.go`,
  `conv.go`, `journal.go`, and `PLAN-CONVPROJ.md` matched the target's
  Git blob IDs.
- Tests and probes ran as UID 1000.
- The branch advanced concurrently to `caa65182758b86d688c9d2335d4c5cb8d15954c0`
  after the export was created. Nothing from that newer revision was used as
  evidence; this verdict applies only to `9430b37`.

## Substantive findings

### 1. [CONCRETE][SEV: MED] The typed collision signal falsely matches non-event-ID constraints

`internal/kernel/journal/journal.go:503-517` wraps an insert failure as
`ErrDuplicateEvent` whenever its rendered text contains either
`"UNIQUE constraint"` or the much broader `"constraint failed"`. The
`events` insert has three different uniqueness constraints:
`journal_offset` primary key, `event_id` unique, and
`(run_id, sequence)` unique (`internal/kernel/journal/journal.go:182-193`).
The classifier never establishes that the violated constraint is
`events.event_id`.

An external probe inserted a distinct row at the append actor's next offset, then
appended a fresh event ID. The resulting non-event-ID collision was misclassified:

```text
non-event-id uniqueness failure falsely classified as ErrDuplicateEvent:
journal: duplicate event id (redelivery collision):
constraint failed: UNIQUE constraint failed: events.journal_offset (1555)
```

The probe requires a broken single-writer boundary, so it is not an ordinary healthy
path. It still demonstrates that the public error classification is semantically
false precisely when storage ownership or integrity is already suspect; callers then
take the wrong recovery branch.

**Fix:** use the SQLite error type/code only as the first filter, then establish inside
the transaction that the attempted `event_id` already exists before wrapping
`ErrDuplicateEvent`. Preserve every other constraint/storage error unchanged.

### 2. [CONCRETE][SEV: MED] The malformed-success fix has no committed red-capable detector

The implementation correctly returns before upsert when the typed `final` decode
fails (`internal/conv/conv.go:98-114`). A direct accepted-payload probe using
`{"final":123}` passed: projection and retained replay both returned empty history.

However, the claimed `malformed-succeed` oracle kind is absent. The committed kind
list contains only `admit`, `succeed`, `empty-succeed`, `suspend`,
`resume`, `fail`, and `ordinary-fail`
(`internal/app/daemon/convproj_diff_test.go:86-120`), and no deterministic test
covers malformed success. Restoring only the old ignored-unmarshal behavior while
retaining the full committed differential test produced:

```text
ok github.com/MatNik89/nexus/internal/app/daemon 38.074s
```

Thus the implementation closure is real, but its promised detector and causal
regression lock are not.

**Fix:** add a deterministic admitted-turn case or an explicit randomized
`malformed-succeed` kind using a journal-accepted typed-decode failure, and show it
RED with only the unmarshal guard removed.

### 3. [CONCRETE][SEV: MED] Collision-only recovery is implemented but not regression-locked

`RunChannelTurn` now returns every non-`ErrDuplicateEvent` error directly before
calling `recoveredTurnOutcome` (`internal/app/daemon/daemon.go:278-288`), so
`VerifyChain` at `daemon.go:478-487` is gated to the collision branch. A direct
probe corrupted an earlier chain row and then caused an ordinary first-run planner
failure; the call returned `ordinary planner explosion`, not `chain broken`.

The real redelivery side is also valid: the committed
`TestRedeliveredUpdateCannotRerunCompletedTurn` passed, while removing only the
journal's `ErrDuplicateEvent` wrapper made it RED with the duplicate
`events.event_id` error.

But removing only the `errors.Is(err, journal.ErrDuplicateEvent)` gate and retaining
the committed short daemon suite still yielded:

```text
ok github.com/MatNik89/nexus/internal/app/daemon 39.355s
```

That leaves the exact outage/backlog regression which motivated the change
undetected.

**Fix:** commit the corrupt-chain plus ordinary-first-failure detector (or a
`VerifyChain` counting seam) and demonstrate RED when only the collision gate is
removed.

### 4. [CONCRETE][SEV: MED] The rebuild test still does not prove that rebuilding occurs

The specific round-1 vacuity is fixed: admission and success IDs now match, the
pre-rebuild history must be non-empty, and both pre- and post-rebuild answers are
compared to `referenceConversationHistory`
(`internal/app/daemon/convproj_diff_test.go:209-247`).

Those assertions prove stable non-empty answers, not the claimed version-triggered
refold. Changing the generic owner so `catchupProjections` ignores
`ver != sp.Version()` entirely left the committed rebuild test GREEN:

```text
ok github.com/MatNik89/nexus/internal/app/daemon 0.055s
```

Because the existing projection table already contains the correct rows and its
checkpoint is at head, all three answer comparisons succeed without any reset or
replay. The test also does not assert rebuilt row provenance, while
`conv.upsert` hard-codes `projection_version=1`
(`internal/conv/conv.go:64-72`) separately from `Projection.Version()`
(`internal/conv/conv.go:24-27`).

**Fix:** seed a stale/poison projection-only row or schema artifact that only
`Reset` removes, then assert it is gone after reopen; assert checkpoint and every
rebuilt row use `Projection.Version()`, and remove the duplicated version literal.

### 5. [CONCRETE][SEV: LOW] The latency detector remains insensitive to the O(12) index

The production query and index align correctly at this revision
(`internal/conv/conv.go:53-54,184-207`). But the detector threshold is 10x
(`internal/app/daemon/convproj_diff_test.go:148-187`), while the owner plan specifies
20x (`docs/PLAN-CONVPROJ.md:80-84`), and the replay reference is expensive enough
that a table scan still wins.

Replacing the intended partial `conv_hist(identity, hist_seq)` index with an
irrelevant `conv_hist(turn_id)` index retained the detector and passed at 78x:

```text
proj=72.910996ms ref=5.693372967s (78x)
--- PASS: TestConvProjectionFasterThanReference
```

This is the unresolved mechanism-level false green from round 1. It matters because
the incomplete-tail growth mode is the slice's root production failure.

**Fix:** assert `EXPLAIN QUERY PLAN` uses `conv_hist`, or use a statement-step
counter/bounded-growth detector as the causal oracle. Keep relative latency as
supporting evidence and restore the owner-specified threshold unless the plan is
explicitly amended.

## Verified closures and checks

- `CGO_ENABLED=0 go test -count=1 ./...` passed every discovered package in
  the immutable export. Notable durations: acceptance 100.532s, daemon 62.086s,
  probe 55.724s, sandbox 37.039s.
- `CGO_ENABLED=0 go vet ./...` exited 0.
- Focused `go test -count=1 ./internal/conv ./internal/kernel/journal
  ./internal/app/daemon` passed; `internal/conv` has no package-local tests.
- The external malformed-success parity and corrupt-chain ordinary-failure probes
  both passed on unmodified production code.
- Real redelivery reaches `ErrDuplicateEvent` through
  `loop.appendPayload -> Journal.Append`; `errors.Is` survives the loop wrapper,
  and the end-to-end test recovers the durable original final without replanning.
- No fold-vs-reference divergence was found for the committed seeded lifecycle
  shapes plus the explicit malformed-success probe. This is bounded evidence, not
  exhaustive equivalence.
- Production composition registers `conv.NewProjection()` in
  `cmd/nexus/main.go:499`. History and keyed recovery use guarded projection reads;
  full-chain verification remains only inside collision recovery.

## NEXUS audit questions

1. **Solves the reported class? PARTIAL.** The runtime hot path and the three stated
   behaviors are corrected, but the exact index mechanism is not causally guarded.
2. **Regression? YES.** The new typed error can label a non-event-ID storage
   constraint as a redelivery collision.
3. **Detector validity? NO.** Three claimed mechanisms remain GREEN under direct
   controlled ablation; the latency oracle also survives loss of its intended index.
4. **Revert-proof? PARTIAL.** Duplicate-event recovery is revert-proof, but the
   malformed decode guard, collision-only gate, rebuild trigger, and partial index
   are not.
5. **Class sweep? YES.** Journal insert constraints and wrappers, loop append
   propagation, all daemon recovery callers, projection/reference folds, rebuild
   ownership, production registration, query/index alignment, and relevant
   conformance tests were traced.

## Counterargument, weakest link, and proof ceiling

The strongest counterargument is that the current production code fixes both
observed behavioral regressions, and the false `ErrDuplicateEvent` case requires a
violation of the single-writer invariant. That does not justify PASS here: this
repository explicitly makes red-capable behavioral detectors review blockers, the
collision gate protects the same backlog class that triggered the slice, the rebuild
test does not observe rebuilding, and the typed classification is demonstrably
broader than its contract.

Minimal Diff: no production files changed; this report is the only repository write.
-> proof: full suite and vet GREEN at UID 1000; four controlled mechanism ablations
and two external closure probes recorded above.
-> weakest link: no long quiet soak or concurrent fault injection was run.
-> skipped: live Telegram/provider canary and post-fix soak; upgrade when the owner
authorizes live/paid execution or schedules the long soak.
Proof ceiling: local Linux/ARM64 clean-export evidence at immutable `9430b37`; it
does not establish live delivery, long-run backlog behavior, or correctness of the
newer concurrent branch tip.
Status: FAIL at `9430b378b1be3407a3e9c9c07e1c2de27704f5c9`
topknot: ultra+preflight

VERDICT: FAIL

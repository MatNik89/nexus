# Implementation Review: conversation projection

## Result

**FAIL.** Commit `5da84aad4240d11f22239bfaea89c95654199de9` builds and its committed suite is green, and the production history query does use the intended partial index. However, the projection is not observationally equivalent to the retained replay for all events accepted by the journal, and the claimed cold-only integrity scan is also reachable on every ordinary first-run channel failure. The rebuild and latency detectors additionally have material false-green gaps.

Reviewed commits:

- `84e6da3f83897abb87dce33c80cc98b3cef728a2`
- `5da84aad4240d11f22239bfaea89c95654199de9`

Immutable evidence source: clean `git archive` export of `5da84aa` at `/home/matej/HARNESS/nexus-review-convproj-5da84aa`, outside `/tmp`. Core exported blobs matched `git show` before probing. The source checkout was not used for build/test evidence. Unrelated untracked files, including review reports that appeared concurrently, were neither inspected nor touched.

After evidence capture, a concurrent uncommitted edit appeared in `internal/conv/conv.go` that addresses Finding 1. It is outside the requested immutable commits, was not changed or verified by this review, and does not alter the verdict for `5da84aa`.

## Substantive findings

### 1. [CONCRETE][SEV: MED] A journal-accepted succeeded event can make projection history diverge from replay

`internal/conv/conv.go:103-114` ignores the `json.Unmarshal` error for `turn.succeeded`, then marks an admitted row `hist_done=1` with the zero-value final. The retained reference updates completion only when that same decode succeeds (`internal/app/daemon/daemon.go:376-383`). The randomized oracle cannot expose this because its kind pool emits only a string final or an empty string final (`internal/app/daemon/convproj_diff_test.go:88,103-120`).

The journal currently accepts machine lifecycle events without a payload validator in the production composition root (`cmd/nexus/main.go:473-476`). A clean-export probe therefore successfully appended the valid JSON payload `{"final":123}` after an admission. The projection returned a `history_user` block, while the reference returned no history blocks:

```text
=== RUN   TestReviewMalformedSucceededPayloadMatchesReference
review_convproj_probe_test.go:35: projection/reference divergence for journal-accepted succeeded payload
--- FAIL
```

This is a real observational-equivalence defect, not merely missing coverage. It requires a malformed internal lifecycle producer rather than direct remote control, so MED is proportionate; the consequence is silently different conversation context after restart or continued operation.

**Fix:** handle the decode exactly like the reference before any upsert/completion update: if decoding the typed final fails, return without applying this event. Preferably also give machine lifecycle events canonical payload validators at journal admission, but that broader owner change is not required to close this slice's parity defect.

### 2. [CONCRETE][SEV: MED] `VerifyChain` is not confined to redelivery/collision

`RunChannelTurn` calls `recoveredTurnOutcome` after every `Loop.RunTurn` error (`internal/app/daemon/daemon.go:278-290`). `Loop.RunTurn` can fail on a first execution for ordinary planner, assembler, policy, journal, or iteration failures; for example, planner failure is converted into a durable `turn.failed` and returned (`internal/kernel/loop/loop.go:252-277`). `recoveredTurnOutcome` unconditionally runs the full-chain `VerifyChain` before its keyed projection lookup (`internal/app/daemon/daemon.go:472-484`), and `VerifyChain` walks the complete events table (`internal/kernel/journal/journal.go:685-729`).

Therefore the successful channel path is O(12), and collision recovery is correctly fail-closed, but every ordinary first-run channel failure is still O(events). During a provider or planner outage, each incoming message can pay the full scan and recreate a backlog-growth mode. The comment that recovery runs only on the cold redelivery/collision path (`internal/app/daemon/daemon.go:475-479`) is false for the actual caller.

**Fix:** classify the duplicate turn-event collision structurally and invoke recovery/`VerifyChain` only for that collision. A new turn that has already produced its own ordinary failure should return the original error without recovery scanning.

### 3. [CONCRETE][SEV: MED] The version-rebuild detector is vacuous and does not test its stated oracle

The fixture admits update `i` but succeeds turn `i+1` (`internal/app/daemon/convproj_diff_test.go:210-215`). Each success therefore precedes the matching next admission and is deliberately discarded by the plan's semantics; the fifth success has no admission at all. The resulting pre-rebuild history is empty. The test then compares only projection-before to projection-after (`internal/app/daemon/convproj_diff_test.go:217-236`), never either result to `referenceConversationHistory`, despite its comment claiming a differential oracle.

A clean-export non-vacuity probe repeated the committed construction and failed:

```text
=== RUN   TestReviewCommittedRebuildFixtureHasCompletedHistory
review_convproj_probe_test.go:47: committed rebuild fixture has zero history blocks; before==after is vacuous
--- FAIL
```

A second negative control changed only `Projection.Version()` from 1 to 2 while leaving the row provenance literal at 1 (`internal/conv/conv.go:27,68-70`); the committed `TestConvProjectionRebuild` remained GREEN. It therefore does not prove that a real version bump rebuilds non-empty behavior or maintains per-row `projection_version` provenance.

**Fix:** use matching admission/success turn IDs, assert non-empty expected history and recovery before rebuilding, compare both rebuilt answers to the retained references, and assert the checkpoint and every rebuilt row carry `Projection.Version()`.

### 4. [CONCRETE][SEV: LOW] The latency detector does not causally prove the partial-index/O(12) mechanism

The production mechanism is correct on this revision: `conv_hist` is partial on completed, admitted rows (`internal/conv/conv.go:53-54`), the query filters the same predicate and limits to 12 (`internal/conv/conv.go:177-203`), and SQLite reported:

```text
SEARCH conv_turns USING INDEX conv_hist (identity=? AND hist_seq>?)
```

On the restored committed code, the 1,500-completed plus 1,500-incomplete-tail detector measured 7.38 ms for 20 projection queries versus 5.46 s for replay, or 740x. This supports the current implementation under the tested corpus.

But deleting only `CREATE INDEX conv_hist` while retaining the detector still passed at 76x (96.23 ms versus 7.31 s). The 10x relative threshold compares against an extremely expensive full cryptographic replay, so a table scan can remain green. Thus the test establishes a large constant/work reduction, not the claimed O(12) access path or the causal role of the partial index.

**Fix:** add an `EXPLAIN QUERY PLAN` assertion for `conv_hist` or a statement-step/counting seam whose cost stays bounded as the incomplete tail grows; retain the latency measurement as supporting evidence rather than the asymptotic oracle.

## Verified behavior

- The clean export's committed focused projection suite passed. The randomized every-prefix oracle passed 200 seeded sequences; the restored latency detector passed at 740x; the committed rebuild test passed, subject to Finding 3.
- `TMPDIR=/run/user/1000 CGO_ENABLED=0 go test ./...` passed all discovered packages from the clean export, including `cmd/nexus`, acceptance, daemon, journal, channel, sandbox, and schedule.
- `CGO_ENABLED=0 go vet ./...` exited 0.
- `CGO_ENABLED=0 go build -trimpath -o review-nexus-bin ./cmd/nexus` produced a statically linked ARM64 ELF, SHA-256 `309fd69d55c7e7d20a4f448c4facc5efa4714412eb872fb0dde4aff6a59f9f34`.
- Production registration includes `conv.NewProjection()` in `buildDaemon` (`cmd/nexus/main.go:494-500`). No production `Replay(0)` remains in the normal history path. The only daemon replays are the explicitly retained reference implementations.
- Projection state is folded synchronously in the journal append transaction, with the checkpoint advanced in that transaction (`internal/kernel/journal/projection.go:89-119`). Version mismatch resets and refolds through the generic owner (`internal/kernel/journal/journal.go:331-389`).
- `conv` receives only restricted `ProjDB`/`ProjTx` handles; its statements do not name canonical tables. Reads go through the SELECT-only, single-statement, canonical-table-rejecting `QueryProjection` seam (`internal/kernel/journal/projection.go:192-210`). No guard bypass was found.
- Rows carry `source_event_id`, `source_offset`, and `projection_version`; relevant valid lifecycle folds refresh event/offset provenance through `upsert` (`internal/conv/conv.go:35-52,64-72`). At the current version, the stored version literal and `Version()` both equal 1. Finding 3 covers future-bump proof.
- History preserves exact identity filtering, admission order, completed-before-cap semantics, empty-final completion, current-turn exclusion, and chronological output for the generated valid lifecycle shapes. Recovery preserves success/suspend/resume/failure precedence for those same shapes.

## Detector and scope assessment

The lifecycle pool covers the named valid states and many pathological orders, but it is not complete over the journal's accepted payload domain. The decisive missing class is a valid JSON object whose typed field cannot decode, such as numeric `final`; Finding 1 proves the resulting divergence. It also ignores both recovery-call errors at `internal/app/daemon/convproj_diff_test.go:137-138`, which can mask a recovery-only query failure when both zero-value results happen to match. Those errors should be asserted before comparing outcomes.

The change reuses the existing `SyncProjection` framework, SQLite driver, journal transaction, and guarded query seam; no dependency or parallel persistence framework was added. The retained replay functions are test references but remain in the production `.go` file; moving them to `_test.go` would make that ownership literal and reduce maintained production surface. This is a simplification note, not a verdict reason.

Commit `84e6da3` also contains many pre-existing plan-review documents and `.kilo`/`AGENTS.md` edits unrelated to the conv projection. That weakens commit isolation but does not change this implementation verdict.

## NEXUS audit questions

1. **Solves the reported class? NO, not completely.** Successful turns avoid full replay and use the indexed projection, but ordinary first-run failures still invoke a full `VerifyChain`, and accepted malformed success payloads are not equivalent.
2. **Regression? YES.** The projection can admit a history pair the replay excludes for a typed-decode failure.
3. **Detector validity? NO.** The every-prefix generator misses that accepted payload class and discards recovery errors; the rebuild fixture is empty; the latency threshold remains green without the index.
4. **Revert-proof? PARTIAL.** The external malformed-payload probe is red-capable and the production index plan is directly observed, but the committed rebuild and latency detectors do not reject the decisive negative controls.
5. **Class sweep? YES.** Production and reference history/recovery callers, all daemon `Replay(0)`/`VerifyChain` sites, projection registration, guarded SQL handles, provenance updates, checkpoint/rebuild owner, lifecycle kinds, and prior conformance callers were traced.

## Strongest counterargument and proof ceiling

The strongest counterargument is that the built-in loop always marshals `final` as a string, so Finding 1 needs a buggy or future internal producer, while the actual successful-message performance path is correctly indexed and measured hundreds of times faster. That does not rescue PASS: the journal currently accepts the counterexample, the slice claims equivalence to the replay, and the clean probe demonstrates different user-visible context. Finding 2 separately contradicts the claimed cold-only placement under routine first-run failures.

Proof ceiling: this was local, clean-export verification on Linux/ARM64. It did not rerun the long quiet soak or a live Telegram/provider canary, did not establish behavior on another SQLite version or host, and cannot prove absence of concurrency defects. No live or paid action was started.

VERDICT: FAIL

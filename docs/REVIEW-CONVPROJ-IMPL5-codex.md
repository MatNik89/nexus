# Conversation Projection Implementation QA — Codex Round 5

## Scope and identity

- Review type: read-only substantive correctness, security, behavioral-equivalence, buildability, and detector-sensitivity verification.
- Target: `slice/p0-convproj` at commit `f4889a836f60f37e30d25ab4aae0a75e7b2fbfc3`.
- Target tree: `43818173e8e3e0c52e4ee1226beb18714b1aed10`.
- Clean export: created on disk at `/home/matej/HARNESS/nexus-convproj-codex.zxFiz2` with `git archive`; the archive SHA-256 was `06856b7233c7dacdbf7ff434e6c1331552f1c807d81f0d29b9c1a8de40ad45c7`.
- Cleanup: the clean export, mutation copy, and build-output directory were moved to the desktop trash after verification; all three original paths were confirmed absent.
- Reviewer identity: UID `1000` (`matej`), Linux ARM64, Go `1.26.4`.
- The live checkout changed branch and acquired unrelated modifications during this audit. No conclusion below relies on that checkout; all source inspection, builds, and tests used the immutable export.

## Result

No substantive correctness, security, behavioral-equivalence, or buildability flaw was found.

The production composition root registers the conversation projection (`cmd/nexus/main.go:499`). The projection schema owns the admission-ordered partial history index and Annex P0.3 provenance fields (`internal/conv/conv.go:29-55`), updates provenance on contributing events (`internal/conv/conv.go:64-73`), gates history completion on prior admission while retaining independent recovery semantics (`internal/conv/conv.go:75-173`), limits indexed history reads to the requested window (`internal/conv/conv.go:182-207`), and performs primary-key recovery lookup (`internal/conv/conv.go:219-238`).

The channel path returns ordinary failures directly and enters recovery only for `journal.ErrDuplicateEvent` (`internal/app/daemon/daemon.go:283-320`). Recovery verifies the canonical journal chain before trusting the projection (`internal/app/daemon/daemon.go:481-509`). The journal emits the sentinel only after an insertion failure and an exact `event_id` existence check (`internal/kernel/journal/journal.go:503-523`, `647-657`). Version mismatch handling resets and refolds the derived projection from the verified canonical stream (`internal/kernel/journal/journal.go:307-388`).

The retained reference implementations remain independent replay folds (`internal/app/daemon/daemon.go:346-414`, `512-565`). The normal binary did not retain their symbols under Go dead-code elimination. The small unexported `testRecoveryVerifyHook` symbol is present in the binary, but it is nil by default, only observed on the cold collision path, and does not create a reachable substantive defect.

## Clean-export proof

Baseline commands and observed results:

- `/home/matej/.local/go/bin/go test ./internal/app/daemon ./internal/conv ./internal/kernel/journal -count=1 -v` — PASS. The differential oracle, latency detector, rebuild detector, recovery gate, daemon neighbors, and journal sentinel tests all ran. The live-provider smoke was the only skip and is unrelated to this local slice.
- `TestConvProjectionMatchesReferenceOnEveryPrefix` — PASS over all 200 seeded randomized sequences and every generated prefix for both history and recovery.
- `TestConvProjectionFasterThanReference` — PASS: projected history took `3.900409ms` versus `5.631172246s` for retained replay over 20 reads, approximately `1444x` faster on this host and above the committed `10x` threshold.
- `env CGO_ENABLED=0 /home/matej/.local/go/bin/go test ./... -count=1` — PASS for every package in the clean export.
- `env CGO_ENABLED=0 /home/matej/.local/go/bin/go build -trimpath ... ./cmd/nexus`, `/home/matej/.local/go/bin/go vet ./...`, and `./scripts/static-check.sh <built-binary>` — PASS; static check reported the binary statically linked.

## RED-capability and negative controls

All mutations changed production code only while retaining the candidate tests:

1. Removed the `hist_seq IS NOT NULL` admission gate from `turn.succeeded` — the differential oracle turned RED at sequence 18, step 11 with projected history present and reference history empty. This detects the prior success-before-admission resurrection class.
2. Made empty-final success populate recovery — the oracle turned RED at sequence 0, step 3 with projected `SUCCEEDED` versus reference `SUSPENDED`. This independently exercises recovery equivalence.
3. Ignored the malformed-success decode error — the oracle turned RED at sequence 10, step 5 with projected history diverging from the reference. The malformed-success input is therefore reached by the fixed seed.
4. Disabled the ordinary-error early return before recovery — `TestOrdinaryFailureSkipsRecovery` turned RED because the first-run failure invoked `VerifyChain` once instead of zero times. The unchanged collision half observed exactly one invocation in the baseline.
5. Made the collision pre-check classify every insertion constraint failure as a duplicate event — `TestDuplicateEventSignalIsEventIDOnly` turned RED on its non-event-ID primary-key collision.
6. Replaced projected history with the retained replay implementation — the latency detector turned RED at approximately `1x` (`5.692s` versus `5.469s`).

## Non-blocking notes

[NOTE][DETECTOR] `TestConvProjectionRebuild` has non-empty before/after data, but disabling the production `ver != sp.Version()` branch leaves that committed test GREEN because the already-correct projection rows remain in place. A separate reviewer-side probe poisoned `conv_turns.hist_final`, set the durable projection version to zero, and reopened the journal: the target code repaired `POISON` back to the journal-derived `expected` value; disabling the version-mismatch branch made that probe RED with `Final:POISON`. Production rebuild behavior is therefore verified, but the committed rebuild test should poison or delete derived state before reopening if it is intended to prove the version branch causally.

[NOTE][LEDGER] At the target revision, `docs/tasks-P0.md` records the earlier conversation-history slice but does not yet record this projection slice. This is a project-ledger omission, not a defect in the reviewed runtime behavior under the owner's substantive-only verdict policy.

## Complexity and proof ceiling

Topknot result: Lean already. The change reuses the existing `SyncProjection` mechanism and SQLite indexes, adds no dependency or parallel persistence owner, and keeps retained replay solely as an independent oracle. No simpler implementation preserves the required bounded queries, rebuildability, and behavioral differential.

The strongest counterargument is the committed rebuild detector's false-green ablation. It does not block this verdict because the production version-reset/refold path was independently exercised with poisoned derived state and turned RED when disabled.

Proof ceiling: the race detector could not run on this ARM64 host because ThreadSanitizer aborted before tests with `unsupported VMA range` (`Found 47`, `Supported 48`). This review also does not claim a fresh long-duration soak or live-provider result; upgrade the evidence when a supported race environment or the next owner-authorized soak is available.

VERDICT: PASS

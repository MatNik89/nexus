# PLAN-CONVPROJ round-2 design review (Codex)

## Scope and binding

- Review type: design-only; no implementation or production tests were run.
- Target: `slice/p0-convproj` at `d29b7bd9c61b5c7f9182e35d0d7e99a07b8d3a0a` (tree `220787ba887375154f829379b2008790ea692034`).
- Clean source: on-disk archive `/home/matej/HARNESS/nexus-plan2-codex-export.Fbg5ul`; `docs/PLAN-CONVPROJ.md` SHA-256 `e552ba3a1d73d045e7ad25b24d42799ada28cbd7ad6d1c9398697ba3d8b53260`.
- Required marker: `EVERY event UPSERTS` is present at `docs/PLAN-CONVPROJ.md:26`.
- Verdict policy applied: only substantive correctness, security, behavioral-equivalence, or buildability defects fail this review. Editorial issues are notes.

## Substantive findings

### [HIGH] Empty-final completion semantics contradict the index and the differential oracle

`docs/PLAN-CONVPROJ.md:29-34` defines `hist_final` as the **non-empty** final of `turn.succeeded`, and `docs/PLAN-CONVPROJ.md:40-44` makes `hist_final IS NOT NULL` the completed-history membership predicate. The retained implementation has a separate completion bit: any successfully decoded `turn.succeeded` marks the pair complete even when `final == ""` (`internal/app/daemon/daemon.go:359-366`), and that completed pair remains in the ordered history set (`internal/app/daemon/daemon.go:374-383`). Only rendering omits the empty assistant block; the admitted user block remains (`internal/app/daemon/daemon.go:415-433`).

Therefore an admitted turn followed by `turn.succeeded{"final":""}` is included by the reference history but excluded by the proposed partial index. This directly conflicts with the promised empty-final coverage and identical-on-every-prefix oracle at `docs/PLAN-CONVPROJ.md:55-71`. No implementation can satisfy both stated contracts.

Required design fix: retain an independent history-completion marker (for example nullable `hist_completed_at`/boolean set by every valid `turn.succeeded`) and index that marker, while keeping `hist_final` as lossless content that may be the empty string. The partial index should predicate on completion, not content.

### [MEDIUM] First-observed `seq` cannot also be admission order for every admitted turn

The plan requires a row and immutable `seq` for whichever lifecycle event is observed first (`docs/PLAN-CONVPROJ.md:26-39`), while also claiming that an admitted turn's `seq` is its admission offset and therefore identical to today's order. Today's replay creates and orders a history pair only when admission is encountered (`internal/app/daemon/daemon.go:329-350`); lifecycle events observed before admission do not establish history order.

Counterexample allowed by the plan's unrestricted randomized lifecycle stream: turn A is first seen as failed, turn B is admitted and succeeds, then A is admitted and succeeds. The reference orders B then A by admission observation; the projection orders A then B by immutable first-observed `seq`. The plan explicitly includes failed-only prefixes, interleaved identities, noncanonical `resume-after-terminal`, and equality on every prefix (`docs/PLAN-CONVPROJ.md:55-61`), so it does not establish a legality restriction that removes this case.

Required design fix: separate immutable row-creation offset from nullable history-order/admission offset, and order the history index by the latter. If the intended oracle is restricted to a validated event language, define that language and remove impossible sequences such as `resume-after-terminal`; otherwise the proposed single `seq` cannot preserve the reference semantics.

### [MEDIUM] The proposed projection schema omits mandatory source identity metadata

The listed `conv_turns` columns at `docs/PLAN-CONVPROJ.md:29-34` omit `source_event_id`, `source_offset`, and `projection_version`. Annex A P0.3 requires those fields for projections and requires replay/audit correlation to the same event (`docs/HARNESS-SPEC.md:1395-1403`). The generic projection checkpoint records a projection-wide applied offset/version, but it does not supply per-row `source_event_id` provenance (`internal/kernel/journal/projection.go:105-119`).

Required design fix: specify how each aggregate row records the last applied source event identity/offset and projection version (or amend the higher-ranked owner explicitly if aggregate projections are intentionally exempt). As written, implementing the listed schema violates the governing contract.

## Round-1 fold assessment

| Required fold | Assessment |
|---|---|
| Every lifecycle event UPSERTS | Present, but conflicts with the single immutable ordering field for lifecycle-first/admission-later streams. |
| Separate `hist_final` and `rec_state` | Present, but `hist_final` incorrectly conflates non-empty content with history completion. |
| `seq` is first-observed and immutable | Present, but not behaviorally equivalent to admission order in the oracle's stated input space. |
| Partial completed-history index plus current-turn exclusion | Present; the index shape is bounded, but its predicate excludes empty-final completed turns. |
| Retained old replay as seeded prefix differential oracle | Present and appropriately stronger than fold-vs-fold, but it will expose the two contradictions above rather than resolve them. |
| Relative latency detector, at least 20x on the same corpus including incomplete tail | Present. |

## Non-blocking notes

- `docs/PLAN-CONVPROJ.md:74-78` overstates that a relative timing assertion “cannot flake.” Relative measurement controls machine speed, but the plan does not yet specify warm-up, repetitions, statistic, or interference handling. This is a detector-quality note, not by itself a design failure.
- `docs/PLAN-CONVPROJ.md:62-63` overstates that false-green is structurally excluded. Retaining the old replay makes precedence mirroring much less likely, but generator omissions and shared rendering helpers can still produce false confidence.

## Strongest counterargument and conclusion

The strongest argument for passing is that the seeded differential oracle is capable of forcing implementation details toward the old behavior, and the existing `SyncProjection` framework makes the storage mechanism buildable. That does not cure a contradictory written contract: the empty-final predicate cannot pass the unchanged reference, and the single immutable first-event offset cannot universally equal admission order over the oracle's stated lifecycle space. These are substantive behavioral-equivalence flaws, so the owner-directed policy requires FAIL.

Proof ceiling: static review of the committed design and its referenced current implementation only. No implementation exists at this revision, and design-only scope forbids claiming runtime, migration, query-plan, or latency proof.

VERDICT: FAIL

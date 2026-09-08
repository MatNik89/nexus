# PLAN-CONVPROJ design review (Codex)

## Artifact binding

- Reviewed commit: `a542126910a236df4db7c410fd38e6b4f74213ea`.
- Requested branch `slice/p0-convproj` contains that commit but had advanced to `52e05c5f2f1baff5f694a1eeedc9c6ce1cd9ef89`; the later tip was excluded.
- Tree: `2506ccdb337b6aeb8ff05696f689e96e077567d4`.
- `docs/PLAN-CONVPROJ.md` blob: `bf20805ca59cbf7f1ab8684c3de3e0291e268051`.
- Exported plan SHA-256: `204b784e483a4d4cbbc915b4da84f061105b0fa6ac665292d793bf476e06f25f`.
- Clean export: `/home/matej/HARNESS/.codex-review-convproj-plan-a542126` (outside `/tmp`; removed after review).
- Review mode: static design only. No implementation or production tests were run.

## Decision

The projection is the correct minimum mechanism now that the measured replay-latency trigger has fired: it reuses the journal's existing synchronous read-model owner and deletes two per-turn full replays. The proposed design is not implementation-ready because the row lifecycle, completed-row access path, equivalence oracle, and performance gate remain under-specified.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-CONVPROJ.md:24-38` - The design assumes an admission-created row exists before every suspension or terminal update, but current recovery semantics do not have that precondition. It says `turn.succeeded`, `turn.failed`, and `approval.turn_suspended` merely "set" a matching row. Existing recovery detectors deliberately create a suspended-only turn without `channel.inbound_admitted` (`internal/app/daemon/daemon_test.go:589-608`) and a failed-only turn before the collision (`internal/app/daemon/daemon_test.go:1167-1192`). A normal `UPDATE` affects zero rows, so the O(1) lookup loses outcomes that today's replay finds. This is especially material for the documented crash between suspension and channel terminal.

PROBE: Static event-flow comparison against `recoveredTurnOutcome`, which derives recovery directly from lifecycle events without consulting an admission set (`internal/app/daemon/daemon.go:455-508`). The proposed table has no stated sparse-row or later-admission merge rule.

FIX: Define lifecycle-event UPSERTs keyed by `turn_id`, permit a recovery-only row until admission arrives, and specify how a later admission fills identity/update/user text without overwriting recovery state or ordering. Require every update/insert to check its affected-row invariant explicitly.

[CONCRETE][SEV: MED] `docs/PLAN-CONVPROJ.md:26-35` - The `seq` column has no owner or update rule. Today's history order is the first matching `channel.inbound_admitted` journal order (`internal/app/daemon/daemon.go:325-350,380-388`), not completion, suspension, or last-update order. If an UPSERT stores the latest lifecycle event's offset in `seq`, out-of-order completion reorders the conversation and diverges from replay. The current sequential history tests do not distinguish admission order from completion order.

PROBE: Static fold trace. The replay appends to `order` only when it first sees the admission; later `turn.succeeded` changes fields but never order. The plan's single generic `seq` can represent either admission order or latest state, and those values differ under interleaving.

FIX: Rename and define it as immutable `admitted_offset`, set exactly once by `channel.inbound_admitted`; lifecycle events must never modify it. Add two admitted turns that complete in reverse order and assert history remains in admission order across live fold and rebuild.

[CONCRETE][SEV: MED] `docs/PLAN-CONVPROJ.md:32-35,50-54` - Index `(identity, seq)` does not establish the claimed O(12) completed-history query. A query filtered to completed rows can scan an unbounded tail of admitted, suspended, or failed rows before finding 12 completed pairs. The latency detector seeds 3,000 completed turns, which is the best case for this index and cannot expose that degradation; the same backlog class can return with a large incomplete tail.

PROBE: Static SQLite access-path analysis. The proposed index orders every state for an identity, while `LIMIT 12` applies only after the completed predicate. A corpus of 12 old completed rows followed by 3,000 incomplete rows requires scanning the tail even though the result contains only 12 rows.

FIX: Add a completed-selective access path, such as `(identity, state, admitted_offset DESC)` or an equivalent partial index, and seed the latency/scaling detector with a large newest-first non-completed tail. Lock the intended plan with `EXPLAIN QUERY PLAN` or a controlled index-removal mutation in addition to elapsed time.

[CONCRETE][SEV: MED] `docs/PLAN-CONVPROJ.md:42-56,64-65` - The proposed golden rebuild comparison is not an observational-equivalence oracle. Both a fresh live fold and a version-bump rebuild call the same `SyncProjection.Apply` implementation (`internal/kernel/journal/projection.go:105-119,355-387`), so the same precedence bug can make both outputs identical. The golden compares history only, not `RecoveredOutcome`; the retained examples do not exhaust prefix-sensitive states such as empty `turn.succeeded` (history-complete but currently not recoverable), sparse suspension/failure rows, repeated suspend/resume cycles, or success/failure followed by resume. Therefore the move from replay-time precedence to incremental mutation can be false-green.

PROBE: Controlled static self-oracle analysis: mutate one `Apply` precedence branch identically for live append and rebuild. The stated fresh-vs-rebuild comparison still agrees because both sides share the mutation. Current replay also treats empty success differently in its two consumers: history marks it complete (`internal/app/daemon/daemon.go:351-366`), while recovery requires a non-empty final (`internal/app/daemon/daemon.go:459-466`). A single unqualified `state=SUCCEEDED` query does not define which observation to preserve.

FIX: Retain the old replay folds as test-only independent oracles and compare both history and recovery projection queries after every prefix of a table of lifecycle sequences. Include empty success, absent/late admission, reverse completion order, multiple suspend/resume cycles, ordinary and typed failure, and cross-identity cases; then ablate each precedence transition and require RED.

[CONCRETE][SEV: LOW] `docs/PLAN-CONVPROJ.md:50-54` - One absolute `<250ms` observation on an unspecified "dev host" is neither a stable CI gate nor an asymptotic detector. It has no warm-up, repetition/percentile, pinned runner class, load allowance, or relative scaling assertion. A busy runner can fail a correct indexed query, while an optimized O(N) replay can pass at N=3,000 on faster hardware. Calling the threshold generous does not supply calibration.

PROBE: Static detector review against the claimed O(12)/O(1) behavior and the real fixed-cadence backlog failure. The plan specifies only one N and one elapsed-time cutoff, so it cannot distinguish complexity class from machine speed.

FIX: Separate correctness from performance: assert the indexed query plan and no hot-path `Replay(0)` structurally, then use warmed repeated measurements at two materially different corpus sizes with a bounded scaling ratio. Keep a wall-time budget only on a named/pinned soak host or derive it from the producer cadence with recorded baseline variance.

## Simplification and proof ceiling

The simplest complete design is still one synchronous projection and two indexed reads; no cache, background projector, dependency, or second persistence owner is justified. The missing work is contract precision and red-capable proof, not more machinery.

Weakest link: no implementation exists at the reviewed revision, so SQL planner behavior and the real post-change soak remain unmeasured.

Proof ceiling: this review establishes static design defects at `a542126`; it does not prove the reported 42-minute diagnosis, runtime latency, rebuild behavior, or RED-to-GREEN results.

VERDICT: FAIL

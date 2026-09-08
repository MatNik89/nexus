# PLAN-CONVPROJ round-3 design review (Codex)

## Scope and immutable binding

- Review type: design-only and read-only except for this requested report; no implementation or production tests were run.
- Target: branch `slice/p0-convproj` at commit `323f4e463e1a76b92951d2b8e70869250c81bca5`, tree `ceb9623f25c922c6a210954f932db65c303baec7`.
- Evidence source: temporary clean on-disk export `/home/matej/HARNESS/nexus-plan3-codex.WRrWRo`, removed after verification; `docs/PLAN-CONVPROJ.md` SHA-256 `d4528b7c79e08e392f90805f45e66181d4b11288a0129b9659cb3c9dda92d549`.
- Required marker: `hist_done` is present at `docs/PLAN-CONVPROJ.md:30` and is the partial-index predicate at line 50.
- Verdict policy: FAIL only for substantive correctness, security, behavioral-equivalence, or buildability flaws; editorial issues remain notes.

## Round-2 fold verification

All three stated round-2 findings are resolved in the committed text:

1. `hist_done` is independent of `hist_final`, is set by every valid `turn.succeeded`, includes empty finals, and owns completed-history membership (`docs/PLAN-CONVPROJ.md:29-35,49-53`). This matches the current reference fold, where successful JSON decoding marks completion independently of `Final != ""` (`internal/app/daemon/daemon.go:351-366`).
2. `created_seq` is immutable first-observed identity while nullable `hist_seq` is assigned only by admission, and the history index orders by `hist_seq` (`docs/PLAN-CONVPROJ.md:36-53`). This fixes the failed-before-admitted ordering counterexample from round 2.
3. Per-row `source_event_id`, `source_offset`, and `projection_version` are explicit and updated on every contributing fold (`docs/PLAN-CONVPROJ.md:40-43`), satisfying the Annex P0.3 field requirement (`docs/HARNESS-SPEC.md:1395-1402`).

## Substantive finding

### [MEDIUM] Lifecycle-first success becomes history after a later admission, unlike the retained reference

The plan applies its UPSERT rule when **whichever lifecycle event arrives first** (`docs/PLAN-CONVPROJ.md:26-28`), sets `hist_done` on every valid `turn.succeeded` (`docs/PLAN-CONVPROJ.md:29-33`), later assigns `hist_seq` when admission arrives (`docs/PLAN-CONVPROJ.md:36-40,44-48`), and selects every row having both fields via the stated partial index (`docs/PLAN-CONVPROJ.md:49-53`).

Counterexample:

1. Turn A emits a valid non-empty `turn.succeeded` before its `channel.inbound_admitted` event. The proposed row has `hist_done=true`, `hist_final="ok"`, and `hist_seq=NULL`.
2. A later admission for A fills `identity`, `user_text`, and `hist_seq`.
3. The proposed history query now includes A because both index predicates are true.

The retained replay does not. It creates history membership only while processing admission (`internal/app/daemon/daemon.go:329-350`); an earlier success has no existing pair and is discarded (`internal/app/daemon/daemon.go:351-367`). A later admission does not recover that discarded completion. Therefore the reference still excludes A after step 2.

This sequence is inside the plan's stated lifecycle-first design space: the text says whichever lifecycle event arrives first, expressly reasons about failed-before-admitted ordering, and promises equality with the retained replay on every prefix of randomized lifecycle sequences (`docs/PLAN-CONVPROJ.md:26-28,44-48,62-72`). The journal does not enforce channel admission as a prerequisite for accepting a turn event; it validates closed event types/payloads, while the current replay itself defines the observable fallback for such ordering. As written, no implementation can both use the specified `hist_done + hist_seq` membership rule and remain identical to the reference for this prefix.

Required design correction: either (a) explicitly constrain the differential input language to producible channel sequences and prove that `turn.succeeded` cannot precede admission, removing lifecycle-first success from the UPSERT/equivalence claim, or (b) preserve whether admission had already been observed when success arrived and require that fact for history membership. Add the exact `turn.succeeded -> channel.inbound_admitted` prefix to the differential detector. Recovery may still retain the success independently; this finding concerns conversation-history membership only.

## Non-blocking notes

- The heading at `docs/PLAN-CONVPROJ.md:23` still says “v2 — round-1 findings folded” although the document is v3. Editorial only.
- The latency assertion at `docs/PLAN-CONVPROJ.md:83-87` is relative, but “cannot flake” remains stronger than the design supports without warm-up, repetitions, and a statistic. Detector-quality note only.
- The claim at `docs/PLAN-CONVPROJ.md:71-72` that false-green is “structurally excluded” is too absolute: generator omissions remain possible, as the missing success-before-admission prefix demonstrates. Editorial/proof-ceiling note; the retained reference is still the correct oracle architecture.

## Strongest counterargument, weakest link, and proof ceiling

The strongest counterargument is that production `RunChannelTurn` normally follows durable channel admission, making success-before-admission unreachable through that entry point. The plan, however, does not state or prove that restriction and deliberately extends the projection/oracle contract to lifecycle-first rows and nonstandard randomized prefixes. The written equivalence contract therefore remains contradictory until it narrows the valid input language or adds the missing membership state.

Weakest link: this is a static counterexample against the stated event-language breadth; no implementation exists at this revision, so runtime reachability through every future append caller is not proven.

Proof ceiling: committed design, governing owner contracts, current reference implementation, and immutable clean-export identity only. No runtime, migration, query-plan, or latency claim was executed or verified.

VERDICT: FAIL

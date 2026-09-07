# PLAN-TGOUT v13 design review — Codex

## Artifact binding

- Reviewed commit: `b2b96248f94a49d3f447dfb7825c2d82538b7c54` only.
- Reviewed tree: `e12c88d5b813570b2b6a76ca9b609b97ec1ca8d0`.
- Clean on-disk export: `/home/matej/HARNESS/nexus-plan13-codex-export`.
- Exported `docs/PLAN-TGOUT.md` SHA-256: `36d9935ecf4be17eb376f43ea9b466d655506850cc71a914f2796200faee31f8`, identical to `git show b2b9624:docs/PLAN-TGOUT.md`.
- Required marker verified: exported `docs/PLAN-TGOUT.md:105` contains `DriftError`.
- Scope was design-only. No production code or tests were changed or executed; the only repository write is this requested report.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:104-110` — The new typed carrier is placed on the wrong side of the existing Go dependency boundary. The plan makes `DriftError` planner-specific and directs `loop.failTurn` to extract that concrete error with `errors.As`. However, `internal/llm/planner` already imports `internal/kernel/loop` to return `loop.Action` and implement `loop.Planner` (`internal/llm/planner/planner.go:23-27,226`), while `loop.failTurn` lives in `internal/kernel/loop` (`internal/kernel/loop/loop.go:236-255`). Importing the planner package there to name `planner.DriftError` creates a forbidden Go import cycle. `errors.As` against the embedded `contracts.TypedError` cannot recover `Tool`, and the proposed struct exposes only fields; the plan specifies no dependency-neutral accessor interface that an `errors.As` target could use. Thus the claimed structural producer-to-journal path remains unbuildable as written.

PROBE: Static package/data-flow trace: `planner -> loop` is an existing import edge; the proposed concrete `loop -> planner` type reference closes a cycle. `contracts.TypedError` at `internal/kernel/contracts/contracts.go:523-530` contains no tool field, so extracting only that embedded error loses the precedence-winning tool.

FIX: Put the carrier contract on the dependency-neutral side: define a minimal interface in `loop` or a carrier type in `contracts` that exposes both the validated `TypedError` and `ToolID`; have the planner-specific error implement it, and make `failTurn` use `errors.As` only against that lower-level contract. Add `go build ./...` plus a live structural-extraction detector that fails when either accessor is removed.

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:111-118` — The stated “ONE state-aware ordered fold” has no return shape capable of preserving all outcomes it is required to order. Existing collision recovery returns either a succeeded final or a suspension challenge via `completedTurnFinal(turn) (string, bool, error)` (`internal/app/daemon/daemon.go:407-446`). V13 instead names only `failedTurnOutcome(turn) (code, tool, ok, err)`, which cannot return either of those strings or identify their outcome kind. Retaining `completedTurnFinal` therefore requires a second replay and split precedence ownership; replacing it with the named helper loses success/suspension recovery. The resumed-suppression detector constrains one lifecycle but does not resolve this API contradiction.

PROBE: Return-shape analysis against the three durable candidates consumed by `RunChannelTurn`: `turn.succeeded` needs final text, `approval.turn_suspended` needs challenge text, and drift `turn.failed` needs code plus tool. No value of `(code, tool, ok, err)` can faithfully represent the first two while preserving the existing caller contract at `internal/app/daemon/daemon.go:263-280`.

FIX: Specify one recovery helper returning a closed outcome union such as `{kind, text, error_code, tool}` plus `error`; perform success, suspension, resume suppression, and failure precedence in its single replay, and make the mixed-lifecycle, succeeded, suspended, and corrupt-replay detectors all call that same public recovery path.

## Round-12 disposition

- The plan now names a typed carrier, but the concrete type is not reachable from `loop.failTurn` under the current acyclic package graph; finding 1 remains open at the ownership boundary.
- The mixed `suspended -> resumed -> failed` lifecycle and resumed-suppression ablation are now explicit, but the named helper cannot be the single owner for all recovery candidates; finding 2 remains open at the API boundary.
- The new `err` result correctly represents replay/projection failure for the failure lookup itself.

## Five-question check

1. Solves reported class? **NO.** The structural tool path cannot be implemented as specified without an import cycle or an unstated interface, and the single recovery fold has an insufficient result type.
2. Regression? **NO new runtime regression is proven at plan stage**, but an implementation following the named helper literally must either split recovery precedence again or drop an existing recovered outcome.
3. Detector validity? **PARTIAL.** The mixed-lifecycle ablation is red-capable for resumed suppression, but no detector owns the package-bound carrier contract or proves that one recovery call preserves all three candidate kinds.
4. Revert-proof? **PARTIAL.** Dropping persisted `tool` and resumed suppression is covered; removing the missing dependency-neutral carrier seam or splitting the recovery fold is not.
5. Class sweep? **NO.** The plan covers the failed and mixed lifecycle but does not close the existing succeeded/suspended sibling outcomes under the proposed helper signature.

## Topknot lens

The smallest coherent design is one lower-level carrier contract and one closed recovery-outcome union. That preserves the requested typed behavior with no package cycle, no string parsing, one journal replay, and one precedence owner. No dependency or parallel recovery helper is warranted.

Minimal Diff: report only; no production or test changes
-> proof: immutable commit/tree/blob binding plus package-graph, typed-carrier, lifecycle, and return-shape trace
-> weakest link: static design evidence cannot prove how an implementation would choose to repair the missing carrier boundary
-> skipped: implementation and runtime RED/GREEN execution by explicit design-only scope; upgrade when the plan names the acyclic carrier contract and complete recovery union
Proof ceiling: This review proves two internal design contradictions at `b2b9624`; it does not assess unimplemented runtime behavior or live Telegram rendering.
Status: FAIL at b2b96248f94a49d3f447dfb7825c2d82538b7c54
topknot: ultra+preflight
VERDICT: FAIL

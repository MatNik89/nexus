# PLAN-TGOUT v14 Design Review — Codex

## Review binding

- Reviewed exactly commit `83f16ca680e9920f5e5006957b5d0f25b3704cb9` from a clean on-disk export at `/home/matej/HARNESS/nexus-review-83f16ca-codex`.
- Exported `docs/PLAN-TGOUT.md` SHA-256: `3c6008c8c6f4aec4aba1681db42b1ceefe8d288a68b2301a085f6b1131abf10c`, identical to `git show 83f16ca:docs/PLAN-TGOUT.md`.
- Required identifiers are present: `loop.DriftError` at `docs/PLAN-TGOUT.md:106` and `recoveredTurnOutcome` at `docs/PLAN-TGOUT.md:116`.
- This was a design-only review. No implementation or test was run or changed.

## Findings

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:117` — `FAILED` recovery is undefined for the existing non-drift failure class. The plan says a terminal `turn.failed` supersedes prior candidates and yields `Kind=FAILED`, but the outcome contains only `Code` and `Tool`, and the only specified recovered edge mapping is `TOOL_SCHEMA_DRIFT`. Current `failTurn` writes `turn.failed` with only `turn_id` for every ordinary planner, assembler, grant, tool-unknown, and iteration failure (`internal/kernel/loop/loop.go:239-243`). Therefore a simple ordinary failed turn, or `suspended -> resumed -> ordinary failed`, reaches an underspecified state after restart: the fold must either return a `FAILED` outcome with empty fields, return no outcome after suppressing the stale suspension, or invent a user response. The mixed-lifecycle detector at `docs/PLAN-TGOUT.md:126-132` covers only drift and cannot distinguish these choices.

PROBE: Static lifecycle trace from the current `failTurn` payload through the proposed one-fold precedence and the Telegram edge contract.

FIX: Specify one fail-closed rule for non-drift `turn.failed` events (including whether `ok` means “terminal observed” or “renderable outcome”), define the edge result without string reconstruction, and add committed simple-failure plus `suspended -> resumed -> non-drift failed -> crash -> collision` detectors. Keep a later generic failure suppressing the stale challenge even if it has no recoverable user payload.

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:72` — tool precedence is incomplete for duplicate members at the same field tier. The token walk collects all case-insensitive `action`/`tool_id`/`name` occurrences, while `docs/PLAN-TGOUT.md:133-135` orders only different field names (`action > tool_id > name`). For `{"action":"memory_recall","Action":"other_registered_tool"}`, both values are registered and both occupy the highest tier, so the plan does not determine which `Tool` is placed in `loop.DriftError`, persisted in `turn.failed`, or named to the user. The listed duplicate detector uses one registered value plus `"tool"`, so it does not close this ambiguity.

PROBE: Adversarial token-walk case with two distinct registered tool IDs in duplicate case aliases of the same precedence field.

FIX: Define a deterministic within-tier rule (for example, first matching occurrence in source order) and commit bare plus single-fenced cases with two registered tools; assert the same selected tool in the structural error, journal payload, live edge, and recovered edge.

## Round-13 closure check

- The carrier move removes the proposed reverse import: planner already imports loop, and the pinned loop package does not import planner.
- The plan now names one recovery API and one replay, so the prior split-helper mechanism is removed.
- These two corrections do not resolve the findings above.

## Topknot assessment

The v14 delta is otherwise small and reuses the existing replay path. No additional abstraction is needed beyond fully specifying the two existing mechanisms.

## Proof ceiling

This review establishes design completeness only against the pinned sources. It does not establish implementation correctness, RED capability, or clean-checkout GREEN behavior.

VERDICT: FAIL

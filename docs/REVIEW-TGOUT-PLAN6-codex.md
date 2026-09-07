# PLAN-TGOUT v6 design review — Codex

Target: `f7dad76a7c4475067e153d81cfae49a8586042cc` on `slice/p0-tgout`.

Scope: design review only. The target plan was read from the immutable commit; no production code or tests were changed or executed. The four stated round-5 folds are present: the renderer budget is explicitly local, `TOOL_SCHEMA_DRIFT` has a complete `contracts.TypedError` and terminal/edge/history path, conflicting drift fields have `action > tool_id > name` precedence, and the Hermes resend/table deviations are explicit.

## Findings

[CONCRETE][SEV: HIGH] `docs/PLAN-TGOUT.md:59-65` — The raw “strict” tool-call path still executes before the drift classifier, but the plan does not require duplicate-free top-level JSON. At this revision, `toolCallFromReply` decodes directly into a struct and performs no duplicate-member check (`internal/llm/planner/planner.go:184-195`), while duplicate detection is applied only to `arguments` later (`internal/kernel/contracts/contracts.go:394-409`). Therefore `{"action":"memory_recall","action":"tool","tool_id":"memory_recall","arguments":{}}` is accepted under Go's last-member-wins decode and executes instead of reaching `TOOL_SCHEMA_DRIFT`; likewise duplicate `tool_id` members select one effect ambiguously. This defeats the plan's mutually exclusive classification and exact-schema premise at the execution boundary.

PROBE: Static end-to-end trace at `f7dad76`: plan step 1 precedes classification; the decoder has neither a top-level duplicate-key check nor a duplicate-aware decode; the only existing duplicate check receives `req.Arguments`, not the raw reply.

FIX: Define a valid bare protocol call as a duplicate-free top-level object before struct decoding (and state the allowed-member policy); route duplicate `action`/`tool_id`/`name` objects that identify a registered tool to `TOOL_SCHEMA_DRIFT`, with committed duplicate-key execution-sink-zero tests.

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:26-32,41-48,112-118` — “EVERY GFM pipe table” plus “ours is lossless” is still not a complete table contract. The only declared deviations from the Hermes model cover empty and surplus cells. The cited Hermes splitter is a literal `s.split("|")` (`/home/matej/.hermes/hermes-agent/agent/markdown_tables.py:65-73`), so valid cell content containing an escaped pipe, such as `a \| b`, or a pipe inside an inline-code span is split as a delimiter and the literal pipe is consumed/relabelled. The plan neither specifies a GFM-aware cell tokenizer nor says these inputs must make rendering return false/pass through unchanged, and its detector list has no such cases. Preserving surplus fragments does not preserve the original cell content or semantics.

PROBE: Static counterexample against the referenced algorithm and the proposed preprocessing order. The renderer transforms tables before span parsing (`PLAN-TGOUT.md:112-115`), so the later inline-code precedence cannot protect a pipe inside a table cell.

FIX: Either specify escape/code-span-aware table tokenization, or narrow the accepted table grammar and require `renderHTML` to return false for any row containing escaped or inline-code pipes; commit both counterexamples and a losslessness oracle.

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:105-110,119-129` — The local budget's counting unit is internally ambiguous. A 10-row × 5-column no-row-label table produces about 50 formatted bold spans under the stated shape, but each span serializes as two HTML tags (`<b>` and `</b>`), or about 100 lexical tags. Thus “tag count <= 90,” “10x5 table (~50 tags) renders,” and 90/91 boundary tests cannot all have one conventional meaning. Two conforming implementations can flip the bool differently for the promised example.

PROBE: Contract arithmetic from the plan's table shape: one heading plus four labelled bullets per row is five wrapped spans; 10 × 5 = 50 spans but 100 opening/closing tags.

FIX: Rename and define the budget unit explicitly, for example `renderer-owned formatted spans` where one balanced open/close pair counts as one, and make the validator and 90/91 fixtures count that exact unit.

## Topknot simplification pass

No new dependency or generalized Markdown engine is needed. The smallest complete design is the current narrow renderer plus explicit reject/pass-through grammar at ambiguous boundaries. The weakest link is the absence of executable examples for the two parser ambiguities above; this review proves specification contradictions and reachable paths, not future implementation behavior.

Proof ceiling: Static review of the immutable plan, governing contracts, current production call path, and the locally referenced Hermes implementation. No implementation exists for this slice, so runtime rendering, Telegram acceptance, and RED-to-GREEN detector capability are not established.

VERDICT: FAIL

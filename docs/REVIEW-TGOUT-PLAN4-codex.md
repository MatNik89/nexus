# PLAN-TGOUT v4 design review — Codex

## Scope and artifact binding

- Target: `docs/PLAN-TGOUT.md` at commit `81acb9fac586294b3e1012f63ded951e3abf3676` on `slice/p0-tgout`.
- Target tree: `427e5f428e4171a375a78c81b1358399db2d8811`.
- Reviewed from clean on-disk export `/home/matej/HARNESS/nexus-review-81acb9f-codex-r4.sVHYQk`; its plan SHA-256 matched `git show` at `07198905c8d5aed0a096e57a5bacc6ebad2821a42c0d9287ee4927d99ddf4e46`.
- Mode: design-only, source review and non-destructive static probes. No implementation, test execution, Telegram call, tool execution, checkout, or git-state mutation.
- Verdict rule: any unresolved design flaw of any severity is FAIL.

## Outcome

FAIL. The multipart scope cut removes the round-3 multipart defects, and rejecting fenced effect payloads is correct. The dominant remaining weakness is that v4 still does not define a disjoint planner classification: its literal DRIFT predicate includes a normal valid bare tool call, while its detector matrix omits the positive control that would expose that regression. The rendering section also cannot preserve the promised over-limit behavior, and stale fallback/table claims prescribe mutually exclusive implementations.

## Findings

### F1

[CONCRETE][SEV: HIGH] docs/PLAN-TGOUT.md:62 - The DRIFT grammar is not disjoint from the valid bare tool-call grammar. A normal protocol reply such as `{"action":"tool","tool_id":"memory_recall","arguments":{"query":"x"}}` is a single JSON object whose `tool_id` equals a known tool, so the predicate at lines 65-69 classifies it as DRIFT if implemented literally. The current strict owner recognizes exactly that bare shape as executable (`internal/llm/planner/planner.go:175-223`). v4 removed v2's explicit sequencing, “after `toolCallFromReply` says not-a-tool,” and supplies no unchanged-valid-bare-call detector.

PROBE: Static decision-table evaluation of the literal predicate against the protocol example at `internal/llm/planner/planner.go:84-89`; both VALID and DRIFT evaluate true.

FIX: Specify the ordered, mutually exclusive classifier: strict-parse the raw bare reply first and execute a valid known call; only after strict parse returns non-tool may a classification-only single-fence view enter DRIFT detection. Commit a positive-control test proving a valid bare known-tool call still executes exactly once.

### F2

[CONCRETE][SEV: MED] docs/PLAN-TGOUT.md:58 - “No tool-shaped JSON ever reaches the user” is still an unsound absolute claim. v4 explicitly delivers double-fenced JSON as prose at lines 76-80, and its closed two-field signature does not catch other recognizable drift dialects such as `{"action":"call","name":"memory_recall","arguments":{}}`. Declaring the known-tool content collision does not establish completeness of the drift detector.

PROBE: Static predicate evaluation: the counterexample is one JSON object, names a registered tool, is rejected by the strict protocol, and has neither `action == known tool id` nor `tool_id == known tool id`; it therefore reaches the final path. The specified double-fence test independently contradicts the heading's absolute wording.

FIX: Narrow the claim and acceptance criterion to the explicitly detected drift signatures, preserve the declared false-negative ceiling, and leave complete control/content separation to native function calling. Do not broaden the heuristic without new collision controls.

### F3

[CONCRETE][SEV: MED] docs/PLAN-TGOUT.md:58 - The proposed direct final is not reconciled with the locked structured-output lifecycle. Annex A requires invalid structured output to go through S2.3 salvage/re-ask and says security/effect payloads have no tolerant accept path (`docs/HARNESS-SPEC.md:1560-1563`); only S7 may authorize another attempt (`docs/HARNESS-SPEC.md:1386-1393`). v4 correctly forbids execution and an adapter-owned retry, but it silently substitutes a successful-looking ordinary final string without specifying the validation failure event/state or explaining how this satisfies the S2.3 owner. Therefore the “zero contract violations” claim at lines 118-121 is unproved.

PROBE: Owner-precedence trace from the proposed Planner outcome to Annex A P0.7 and P0.2. The plan specifies a localized string and provider-call count, but no `TypedError`, failed-terminal transition, S2.3 salvage result, or S7 decision.

FIX: Define this as an S2.3 validation failure with an explicit typed error and terminal state. If P0's no-retry policy lands it terminally, say so and prove that lifecycle; if a re-ask is retained, S7 must issue its fresh `AttemptGrant`.

### F4

[CONCRETE][SEV: HIGH] docs/PLAN-TGOUT.md:48 - Removing chunking does not preserve today's over-limit behavior byte-for-byte. v4 renders every send and adds `parse_mode=HTML` (`docs/PLAN-TGOUT.md:82-103`), while today's request sends the original `text` with no parse mode (`internal/channel/telegram/telegram.go:286-295`). Telegram applies the 1-4096 limit after entity parsing. A 4097-character original consisting of one 4093-character bold span plus its four markers becomes 4093 post-parse characters and can newly succeed; conversely, table-to-bullet expansion can push an original below 4096 above the limit and newly fail. The property oracle checks tags and escaping, not post-parse length.

PROBE: Boundary analysis against the official Bot API `sendMessage` contract (1-4096 characters after entity parsing): https://core.telegram.org/bots/api#sendmessage. The current and proposed wire objects also differ statically for every message.

FIX: Either remove the byte-for-byte/unchanged-limit claim and explicitly accept the changed boundary behavior, or define an old-path bypass for the exact excluded class and test both crossing directions at 4095/4096/4097 after parsing. The latter needs a precise character metric; it must not reintroduce multipart machinery.

### F5

[CONCRETE][SEV: LOW] docs/PLAN-TGOUT.md:35 - v4 still specifies both fallback and no fallback. The reference/adoption section says the retained semantics include plain fallback (`docs/PLAN-TGOUT.md:35-44`), and the risk section says an entity-parse failure resends the original (`docs/PLAN-TGOUT.md:122-123`), while the normative renderer section says one send and “NO fallback path” (`docs/PLAN-TGOUT.md:93-103`). These produce different wire-effect counts and different B2/S7 behavior.

PROBE: Static contradiction across the three cited clauses; no runtime probe is needed.

FIX: Delete the stale fallback-adoption and fallback-risk statements. State one rule only: one transport call per outbox-row attempt; a 400 follows the current definite-rejection transition with no adapter-owned resend.

### F6

[CONCRETE][SEV: LOW] docs/PLAN-TGOUT.md:26 - The table contract simultaneously says “adopt exactly” the local Hermes renderer and use lossless v2 rules. The referenced Hermes code truncates surplus cells to the header count (`/home/matej/.hermes/hermes-agent/gateway/platforms/helpers.py:362-365`) and renders missing values as empty strings (`helpers.py:367-374`); v2's lossless rule instead preserved surplus cells as `(extra)` and represented empty cells with an em dash. Thus “exact Hermes,” “lossless,” and the referenced algorithm cannot all be true, and v4 does not restate the chosen empty/extra-cell behavior.

PROBE: Static comparison of the target plan with the exact locally referenced owner implementation and the v2 rules incorporated by reference.

FIX: Make v4 self-contained: choose the lossless NEXUS behavior, list the empty/surplus/malformed-row rules explicitly, and label it a deliberate deviation from Hermes rather than an exact adoption.

## Positive controls and rejected hypotheses

- The known-tool content collision is now candidly declared and tested; it is an intentional fail-closed limitation, not a hidden soundness claim.
- Rejecting a single-fenced valid call rather than executing it resolves the round-3 effect/control ambiguity.
- Keeping original text in the journal/outbox and rendering deterministically at the adapter send boundary preserves canonical durable truth for messages that remain in this slice.
- Non-overlapping renderer tokens plus direct whole-heading tokens can construct only non-nested `<b>`, `<code>`, and `<pre>` entities. The remaining issue is the contradictory table/fallback contract and the unowned length boundary, not ordinary HTML tag support.
- A conservative tag budget is reasonable, but the official Bot API page documents the message-length and entity-nesting rules, not the plan's asserted “50-tag” limit. Treat 50 as a local defensive budget, not a verified Telegram limit.

## Minimum coherent v5

1. Define a mutually exclusive planner decision table and retain an executable-valid-bare-call positive control.
2. Narrow the no-leak claim to the signatures actually recognized; retain native function calling as the only durable completeness fix.
3. Bind detected invalid structured output to S2.3/S7's explicit failure lifecycle.
4. Either preserve the excluded over-limit wire path exactly or acknowledge and test renderer-induced crossings of the post-parse boundary.
5. Delete every fallback statement and fully restate one lossless table algorithm, explicitly deviating from Hermes where necessary.

## Proof ceiling

This is a design review of an immutable plan. Static analysis proves the internal predicate/contract contradictions and reachable boundary counterexamples; it does not establish a future implementation's behavior or live Telegram acceptance. No live Bot API probe was authorized or performed.

VERDICT: FAIL

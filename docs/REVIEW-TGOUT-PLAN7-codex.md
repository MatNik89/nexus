# PLAN-TGOUT v7 review — Codex

Revision reviewed: `389d36129e55742e7dab5446491f42e253696c21` on `slice/p0-tgout`.

Scope: design-only review of `docs/PLAN-TGOUT.md`, checked against the committed planner, contracts, loop, daemon, Telegram adapter, channel core, PRD, HARDQ resolutions, Annex A contracts, and `docs/tasks-P0.md`. No code or runtime state was changed.

## Findings

[CONCRETE][SEV: HIGH] `docs/PLAN-TGOUT.md:59` - The top-level duplicate-member gate does not close the decoder ambiguity it is meant to close. The required reused owner compares decoded key strings case-sensitively (`internal/kernel/contracts/contracts.go:591-601`), while the strict parser decodes into tagged struct fields (`internal/llm/planner/planner.go:184-190`) and Go matches those keys case-insensitively. Consequently, `{"action":"not-a-call","Action":"tool","tool_id":"memory_recall","arguments":{"query":"x"}}` contains no duplicate according to `HasDuplicateJSONKeys`, but the later case alias overwrites `Action` and can make the registered tool execute. This contradicts the stated reason for the gate: last-member-wins decoding must never select an effect.

PROBE: Static trace plus the locally installed `go doc encoding/json.Unmarshal`, which states that struct keys match ignoring case and later duplicate matches replace earlier values. The repository already acknowledges the same decoder behavior at `internal/kernel/contracts/contracts.go:91-99` and tests it at `internal/kernel/contracts/contracts_test.go:462-483`.

FIX: Make strict protocol admission require the exact canonical top-level key spellings and reject every unknown/case-aliased key before struct decoding; add both key orders for `action`/`Action` and `tool_id`/`Tool_ID` as committed no-execution RED cases.

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:101` - The one-attempt and formatting-only claims are incompatible with the retained delivery state machine. The plan says that the pre-wire render decision means “no second attempt ever exists” (`docs/PLAN-TGOUT.md:104-106`), retains “unchanged 400 semantics” (`docs/PLAN-TGOUT.md:143-148`), and claims a render bug can only cost formatting (`docs/PLAN-TGOUT.md:162-164`). In the committed boundary, an HTTP 400 returns an ordinary error after the request (`internal/channel/telegram/telegram.go:161-166`); the channel core classifies every ordinary send error as definite, returns the delivery to PENDING (`internal/channel/channel.go:527-534`), and the adapter flushes it again on later ticks (`internal/channel/telegram/telegram.go:301-315`). A Telegram parse rejection therefore causes repeated formatted wire attempts and no user delivery, not a formatting-only degradation.

PROBE: End-to-end static trace from `renderHTML`'s planned send boundary through the committed HTTP-status classifier, outbox transition, and periodic flush. No destructive or live Telegram probe was run.

FIX: Specify “no fallback attempt within the same flush” instead of “no second attempt ever,” and define a durable, testable outcome for a confirmed formatted-message parse rejection: either persist a plain-mode retry decision for that delivery or explicitly accept and test terminal/non-delivery. Add a detector that returns a parse-related 400, advances another tick, and asserts the chosen wire sequence and durable outbox state.

## Round-6 fold check

The escaped-pipe/backtick-pipe whole-block pass-through, opening-tag counting unit, 90/91 span boundary, and rephrase-only Croatian message are internally stated without the round-6 contradictions described in the prompt. They do not cure the findings above.

## Weakest link / proof ceiling

This is a design review, so it proves contradictions and uncovered counterexamples in the committed plan; it does not prove future implementation behavior. The strongest counterargument to finding 2 is that a constructively valid renderer should never elicit a parse-related 400, but the plan's own risk statement is expressly about renderer bugs and its listed property validator is not the remote parser.

VERDICT: FAIL

# PLAN-TGOUT v5 design review — Codex

## Scope and artifact binding

- Target: `docs/PLAN-TGOUT.md` at commit `d140212fc95184e45d0740cc15dc86330396c8bb` on `slice/p0-tgout`.
- Target blob: `c25ef7131236810958e8fcde44dbce0f29a6fda8`; SHA-256 of the reviewed bytes: `760ffa1e27f6317d4d59f3c0db8829f72522eea87bb42d3b92a8b6213af9dcc0`.
- Method: immutable `git show` source review against PRD, Architecture Essentials, Annex A P0.2/P0.7, the current production seams, the owner-local Hermes source, and the current Telegram Bot API contract.
- Constraints: design only; no implementation, test execution, live Telegram call, checkout, or git-state mutation. The only repository write is this requested report.
- Verdict rule: any unresolved design flaw of any severity is FAIL.

## Outcome

FAIL. v5 fixes the round-4 classifier overlap, narrows the leak claim, gives drift a terminal S2.3 outcome, and restores the original one-send path when rendering declines. It does not, however, make the proposed proof complete. The dominant remaining flaw is that the 90-tag guard counts only renderer-created tags, while Telegram can create additional entities from untagged message text. The plan can therefore pass its property validator and still exceed Telegram's operational 100-entity behavior. Two round-4 contradictions also remain in the committed text, and the typed-failure lifecycle is not specified down to the canonical error/event boundary.

## Findings

### F1

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:95` — The 90-tag budget does not establish the claimed headroom below 100 Telegram entities. Telegram recognizes entities that are not renderer tags, including URLs, mentions, hashtags, bot commands, email addresses, and phone numbers. A rendered message with 90 `<b>`/`<code>`/`<pre>` spans plus eleven plain URLs can satisfy every proposed property at lines 106-108 while producing more than 100 candidate entities. The Bot API documents automatic entity types and parse-mode processing but does not document the 100 ceiling; current observed/library behavior describes the ceiling as array-wide and silently ignores further entities. Counting only generated tags is therefore neither a complete oracle nor a guarantee that formatting is retained.

PROBE: Static counterexample against the proposed validator and Telegram's documented entity model: <https://core.telegram.org/bots/api#formatting-options> and <https://core.telegram.org/api/entities>. The undocumented 100 behavior is explicitly labeled as such by the library constant the plan effectively relies on: <https://docs.python-telegram-bot.org/en/v20.0a4/telegram.constants.html#telegram.constants.MessageLimit.MESSAGE_ENTITIES>.

FIX: Do not claim that a tag count proves a total-entity bound. Either treat 90 as a local renderer-complexity budget only and test accepted rendered semantics at the real Telegram boundary, or define an entity-generation path whose complete entity list is counted and whose returned/stored entities are checked. Add the mixed case `90 renderer spans + 11 auto-detected entities`; the property oracle must turn RED.

### F2

[CONCRETE][SEV: MED] `docs/PLAN-TGOUT.md:62` — “TYPED sentinel error” and “existing typed-error path (like NEEDS_APPROVAL)” do not define the Annex A error contract or the durable failure record. Annex A requires a rejection to carry a typed `error.code`, and `TypedError` requires category, retryability, safe message, and origin (`docs/HARNESS-SPEC.md:1364-1366,1378`). The current planner-error path merely wraps an `error`, appends `turn.failed` with only `turn_id` (`internal/kernel/loop/loop.go:239-255`), and serializes the REPL error as text (`internal/app/daemon/daemon.go:213-217`). Thus the plan does not say whether `TOOL_SCHEMA_DRIFT(<tool>)` is a `contracts.TypedError`, how `Retryability=NEVER` is preserved, where its code is journaled, or how the REPL maps it without string classification after the UDS boundary.

PROBE: End-to-end static trace from `ChatPlanner.Plan` error to `Loop.failTurn`, Telegram handler, daemon frame, and REPL. The named “existing” path has no structured error field on either the journal event or UDS frame.

FIX: Specify one canonical `contracts.TypedError` value (`Code=TOOL_SCHEMA_DRIFT`, `Category=VALIDATION`, `Retryability=NEVER`, stable safe message/origin), persist its code in the failed-turn event, and preserve or localize the code before any boundary that reduces it to text. Add a detector that reads the durable event and the UDS/Telegram edge output, not only the returned Go error.

### F3

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:40` — The fallback contradiction was not fully removed. The deliberate-deviation paragraph still says, “We keep hermes' SEMANTICS (tables->bullets, plain fallback),” while lines 81-87 and 109-112 require exactly one pre-decided send and no fallback resend. Hermes really does issue a second `send_message(..., parse_mode=None)` after a Markdown parse failure (`/home/matej/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py:5241-5268`), so “plain fallback” cannot describe the proposed bool-false pre-wire decision.

PROBE: Static contradiction within the reviewed plan, confirmed against the exact owner-local Hermes implementation.

FIX: Delete “plain fallback” from line 42; retain only the table-to-bullet semantics. Describe Hermes' resend solely as a rejected reference behavior.

### F4

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:26` — The table contract still says NEXUS adopts the Hermes algorithm “exactly,” but line 105 mandates an em dash for empty cells. Hermes pads missing cells with empty strings and emits them unchanged, and truncates surplus cells (`helpers.py:362-371`). The plan also does not state what NEXUS does with surplus cells. “Exactly Hermes,” the em-dash deviation, and an unspecified extra-cell policy do not define one implementable table oracle.

PROBE: Static comparison with `/home/matej/.hermes/hermes-agent/gateway/platforms/helpers.py:330-376`.

FIX: Make the plan self-contained: explicitly define missing, empty, and surplus-cell behavior and call every divergence from Hermes a deliberate NEXUS deviation. Commit literal cases for each branch.

### F5

[CONCRETE][SEV: LOW] `docs/PLAN-TGOUT.md:60` — The widened drift predicate can match several different known tools in one object, but `TOOL_SCHEMA_DRIFT(<tool>)` has a singular parameter and no field precedence. For example, `{"action":"memory_recall","tool_id":"reminder_set","name":"exec"}` is not a strict protocol call and matches three registered IDs. The listed dialect tests exercise fields individually, so they do not determine which stable typed value and localized message this input produces.

PROBE: Decision-table evaluation of the literal “any of” predicate with conflicting known-tool values.

FIX: Prefer the simpler unparameterized `TOOL_SCHEMA_DRIFT`, or define fixed field precedence and a conflicting-fields detector. The localized message need not identify a tool that the model named ambiguously.

## Positive controls and rejected hypotheses

- The raw strict parser now runs before the classification view, and the valid-bare-call positive control directly protects execution of the existing protocol.
- The heading and declared-limit language no longer claim complete drift detection. Collision, mixed prose/JSON, double-fence, and unknown dialects are candid limitations rather than hidden soundness claims.
- Returning bool=false before the wire is the right one-send shape for B2/S7; it avoids Hermes' duplicate-risk resend.
- Counting post-parse UTF-16 units is the correct message-length model. The remaining issue is proving the post-parse entity set, not the 4096 boundary itself.
- The proposed implementation surface is otherwise lean; no additional dependency or general Markdown engine is justified.

## Round-4 closure

- F1 classifier overlap: closed.
- F2 completeness overclaim: closed.
- F3 S2.3 ownership: directionally closed, but F2 above identifies the still-missing typed/durable contract.
- F4 render-boundary behavior: closed.
- F5 fallback contradiction: not closed; one adoption sentence remains (F3 above).
- F6 table contradiction: not closed; empty and surplus-cell semantics still disagree or remain absent (F4 above).

Skill update: none. The surviving round-4 findings were already detectable by the existing owner/contradiction checks; no reusable audit-method gap was demonstrated.

## Proof ceiling

This review proves document contradictions and static counterexamples at the committed design and current production seams. It does not establish a future implementation's behavior, Telegram server acceptance, or the undocumented 100-entity behavior on the owner's bot. No live or paid probe was authorized or performed.

VERDICT: FAIL

# PLAN-TGOUT design review — Codex

## Review binding

- Reviewed branch/revision: `slice/p0-tgout` at `00dba3e9b14f57985bcdeb03edd1522b600753bd`.
- Reviewed artifact: `docs/PLAN-TGOUT.md`, SHA-256 `01c598853bc824f64fa5543a52118867d8f552db144e8794bddd2c5312b48432`; it is tracked at that revision and has no worktree delta.
- Owners checked: `docs/ARCHITECTURE-ESSENTIALS.md`, Annex A in `docs/HARNESS-SPEC.md`, `docs/HARDQ-CONSOLIDATED.md`, `docs/tasks-P0.md`, and the current planner/channel seams.
- Local source checked: `/home/matej/.hermes/hermes-agent/gateway/platforms/helpers.py` and `/home/matej/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py` at the lines cited by the plan.

## Findings

### 1. [BLOCKER][OWNER] The corrective retry is owned by the wrong layer and is forbidden by the P0 retry contract

`docs/PLAN-TGOUT.md:48-59,91-93` puts a second physical provider call inside `ChatPlanner.Plan` and treats “one” as sufficient loop safety. It is not. Annex P0.2 says S7 is the only retry owner, every physical provider attempt needs a unique grant, and an adapter/loop may not own a retry loop (`docs/HARNESS-SPEC.md:1386-1393`). The active P0 owner is stricter: `s7-min` grants one attempt per operation and collapses retryable failure to terminal (`internal/kernel/s7min/s7min.go:3-9,94-105,158-184`; `docs/tasks-P0.md:154-159`). `ChatPlanner` itself documents that every call is a physical attempt and currently issues and lands exactly one grant (`internal/llm/planner/planner.go:3-8,255-285`).

There is already a re-ask owner in S2.3, but it permits repair only for GENERAL structured payloads; EFFECT payloads are strict and receive no repair (`internal/llm/provider/structured.go:16-34,102-179`; `docs/HARNESS-SPEC.md:1560-1563`). A tool-control envelope is an effect payload even when the selected tool later proves read-only. Moving this exact retry into S2.3 without changing that owner contract would still be invalid.

The proposed detector only counts calls. It would pass an implementation that recursively re-enters `Plan`, creates a fresh operation to disguise an unauthorized retry, leaks the first attempt in RUNNING, reuses a consumed grant, or delivers the malformed first reply before correction.

Required design correction: for P0, strictly reject the malformed effect envelope and return the typed safe final without another provider call. If corrective re-ask is a required product outcome, amend the S7/S2.3 owner contracts first and specify two distinct S7-authorized attempts, terminal landing on every exit, shared cancellation/deadline, and no recursive planner entry. The RED must assert transport calls, unique grant consumption, both terminal states, zero delivery of the first output, and a controlled no-guard mutation.

### 2. [HIGH][CLASS] The heuristic is simultaneously over-broad and incomplete

The proposed predicate is “a single JSON object whose top level has an `action` field” (`docs/PLAN-TGOUT.md:49-57`). It suppresses a legitimate answer when the user explicitly asks for, for example, `{"action":"deploy","reason":"demo"}`. Nothing in the plan defines whether exact JSON requested by the user wins over the reserved tool-envelope rule. Restricting the predicate to tool-enabled sessions reduces but does not remove that ambiguity.

At the same time, the predicate does not establish its headline promise that no tool-shaped JSON reaches the user. Common drift shapes with no top-level `action`, such as `{"tool":"memory_recall","arguments":{...}}`, `{"name":"memory_recall","arguments":{...}}`, an OpenAI-style `function` object, or fenced JSON still fall through as finals. The current parser only recognizes `action == "tool"` (`internal/llm/planner/planner.go:175-223`), so those are reachable siblings of the reported class.

The plan also calls the requested schema “exact,” but the current outer decoder does not reject unknown fields and treats missing `arguments` as `{}` (`internal/llm/planner/planner.go:184-202`). `NewToolCall` checks JSON validity and attaches the registry's schema hash; it does not validate arguments against that schema (`internal/kernel/contracts/contracts.go:394-437`). Several in-process handlers use permissive `json.Unmarshal` (`internal/memory/tools.go:63-69,99-104`; `internal/obligation/tools.go:65-76`). The plan therefore patches one observed alternate envelope, not schema drift as a class.

Required design correction: define one unambiguous response contract that represents both `final(content)` and `tool(id,args)` without inspecting arbitrary answer JSON. Native provider tool calling is the cleanest boundary. If it remains deferred, narrow the claim to the exact observed legacy shape, explicitly preserve user-requested JSON, and add a documented fail-closed disposition for every other malformed reserved envelope. Tests must include requested JSON with `action`, each sibling drift shape, unknown/missing/extra members, valid corrected tool, valid prose, and tool-disabled sessions.

### 3. [HIGH][DELIVERY] The 400 fallback hides a second wire attempt inside one outbox attempt and its test cannot prove delivery honesty

`docs/PLAN-TGOUT.md:72-82` proposes two `sendMessage` calls inside the callback that the durable core sees as one send. This crosses the locked boundary that S7 alone authorizes and schedules delivery retries (`docs/ARCHITECTURE-ESSENTIALS.md:196-205`). The core parks one row UNKNOWN before invoking the callback and can observe only the callback's final aggregate result (`internal/channel/channel.go:488-537`); it cannot authorize, count, or journal the formatted and plain wire attempts separately.

The plan also misstates the copied Hermes behavior. Its normal send path falls back only when the error text indicates a parse/Markdown failure, not on every BadRequest (`/home/matej/.hermes/hermes-agent/plugins/platforms/telegram/adapter.py:5241-5270`). “Any Telegram 400” also includes invalid chat, blocked bot, empty/oversized text, and other permanent request failures for which a plain resend is pointless. The current NEXUS `call` discards Telegram's error body and returns only an untyped status string (`internal/channel/telegram/telegram.go:136-183`), so the plan has not specified a sound way to identify a formatting rejection.

A genuine Bot API 400 means the formatted request was rejected, so formatted-400 followed by plain-success does not by itself double-send. The unresolved seams are elsewhere: a crash before the fallback leaves a conservative UNKNOWN although neither request delivered; a crash or sent-mark failure after fallback acceptance leaves UNKNOWN and later human redelivery may duplicate, as honest at-least-once semantics allow; an ambiguous fallback must remain UNKNOWN; and a non-format 400 must not trigger a hidden second request. “One SENT row” proves only that some callback path eventually returned acceptance, not that there was one wire attempt or one remote delivery.

Required design correction: keep one physical send per durable/S7-authorized delivery attempt. If plain fallback is retained, make render mode and each attempt explicit durable state owned by the delivery/S7 layer, with a typed sanitized Telegram rejection classification. Replace the happy-path-only fake with deterministic barriers for: formatted 400 before fallback; fallback acceptance before SENT mark; fallback ambiguous result; SENT-mark failure; process interruption at both seams; non-format 400; and 5xx/timeout (which must never cause an immediate plain resend). Ablating UNKNOWN-first must turn the test RED.

### 4. [MED][SEC] HTML is a reasonable simplification, but the escaping construction is underspecified and the proposed security test is weak

The deliberate HTML deviation is defensible. Telegram supports the required `<b>`, `<code>`, and `<pre>` tags, and its HTML rules require model-controlled `<`, `>`, and `&` to be escaped. That is materially simpler than MarkdownV2 escaping. The plan nevertheless lists transformations without defining the trust-preserving construction order (`docs/PLAN-TGOUT.md:61-71`). Regex replacement followed by global escaping escapes the renderer's own tags; global escaping followed by regex replacement can reinterpret model-controlled Markdown after boundaries have shifted; capture-and-substitute code can place unescaped model bytes inside trusted tags. Code/pre entities also have nesting restrictions.

`<script>` is the wrong principal injection detector (`docs/PLAN-TGOUT.md:78-80`): Telegram does not support that tag, so a bug may merely produce a 400 and exercise fallback. A supported-tag payload such as `</b><a href="tg://user?id=123">spoof</a><b>` tests the real entity-injection/link-spoofing surface.

Required design correction: specify a small scanner/tokenizer that first recognizes fences/inline-code/bold in source text, then HTML-escapes every model-derived segment exactly once, and emits only fixed trusted tags. No placeholder engine is required. Add supported-tag close/reopen attacks, pre/code breakout attempts, pre/code versus bold/table precedence, unmatched delimiters, nested delimiters, ampersand/entity text, and a structural assertion that every raw `<` in output belongs to the fixed renderer tag set.

### 5. [MED][DATA LOSS] “Exactly Hermes” table conversion is lossy on valid and near-valid inputs

The plan promises all GFM pipe tables, malformed runs unchanged, and losslessness via a `<pre>` fallback (`docs/PLAN-TGOUT.md:61-71,91-97`). The cited Hermes implementation splits cells with plain `strings.split("|")`, decides row-label shape from only the first data row, pads short rows, truncates long rows, and keeps consuming any following line containing a pipe (`/home/matej/.hermes/hermes-agent/agent/markdown_tables.py:65-73`; `/home/matej/.hermes/hermes-agent/gateway/platforms/helpers.py:330-376,379-422`). It is a useful narrow heuristic, not a GFM parser.

Read-only probes against that exact local function showed:

- `| job | a \\| b |` split the escaped-pipe cell into separate values instead of preserving `a | b`.
- An ordinary `Use A | B in prose` line immediately after a table was consumed and rendered as another table row.
- A later row `| task | x | y |` under two headers silently dropped `y`.

The proposed changes contain no `<pre>` fallback at all; malformed runs are only said to pass through escaped plain text. Tilde fences, longer backtick fences, inline-code pipes, escaped pipes, changing row widths, and a prose line adjacent to a table are absent from the detectors.

Required design correction: either narrow the contract to the exact conservative subset and pass the whole block through unchanged on any inconsistent row, or implement the minimum GFM-aware cell scanner needed to preserve escaped/code-span pipes. Assert a content-conservation property and keep the three counterexamples above as fixed regressions. Do not claim exact Hermes semantics and losslessness simultaneously unless the lossy owner behavior is deliberately repaired.

### 6. [MED][LIMIT] Telegram's message-size boundary is omitted, so “formatting can fail, delivery cannot” is false

Telegram `sendMessage` accepts 1–4096 characters after entity parsing. The adapter currently has no chunking/length guard (`internal/channel/telegram/telegram.go:286-295`). Table-to-bullet conversion can substantially expand visible text by repeating headers for every row, and the plain fallback still cannot deliver an original message already over the limit. A permanent 400 is currently classified as a definite failure, so the core re-pends it (`internal/channel/telegram/telegram.go:161-167`; `internal/channel/channel.go:529-535`) and the daemon will retry the same impossible message on later ticks (`internal/channel/telegram/telegram.go:301-315`).

Required design correction: add an explicit UTF-16/Telegram-length policy before sending. If one logical outbox item may become multiple Telegram messages, the plan must define stable per-part identity, partial-acceptance state, crash recovery, and SENT/receipt meaning; otherwise cap to one message and return an honest terminal/user-visible disposition. Add boundary cases at 4095/4096/4097 parsed characters and a table whose rendered form crosses the boundary. See the official Telegram Bot API `sendMessage` and HTML-formatting contracts: https://core.telegram.org/bots/api#sendmessage and https://core.telegram.org/bots/api#html-style.

## Detector verdict

The proposed tests are not red-capable for the governing risks. The planner test proves only one scripted example and a call count; the adapter test proves only one success sequence and one database terminal state. Neither detector exercises the owner contracts, negative controls, sibling shapes, crash seams, or a controlled ablation of the guard/UNKNOWN-first boundary. The table tests are examples derived from the intended implementation and omit demonstrated content-loss cases.

## Smallest acceptable redesign

1. P0 tool leak: strict fail-closed typed final, no corrective provider retry; narrow and explicitly name the detected legacy envelope until native tool calling or an unambiguous final/tool response envelope lands.
2. Renderer: keep HTML, but use a deterministic state scanner with escape-on-emission and a conservative table subset that rejects the whole candidate table unchanged on ambiguity.
3. Delivery: one wire call per authorized durable attempt; no adapter-local resend. If fallback is mandatory, promote render mode/attempts into the delivery owner and prove the full crash matrix.
4. Length: define the 4096-character behavior and durable semantics before claiming delivery preservation.

## Proof ceiling and weakest link

This is a design review; no implementation exists for the slice, so no RED/GREEN execution is possible. Static owner tracing proves the contract conflicts, and the local Hermes probe proves the table counterexamples. No live Telegram call was made. The weakest external assumption is that future Bot API behavior continues to follow its current documented 400 and formatting contracts; the design should remain conservative if that changes.

VERDICT: FAIL

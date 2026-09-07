# PLAN-TGOUT v3 design review — Codex

## Scope and artifact binding

- Target: `docs/PLAN-TGOUT.md` at commit `7a976cc84e8c1070457507e15ae8898e52bf64ee` on `slice/p0-tgout`.
- Target tree: `c6432df3cd735bc2bbd0fb18b0071b6409dfd8d7`.
- Reviewed from the clean on-disk export whose plan SHA-256 was `b2df0c1efb486ed30891743abe82cee0bd84dd9854843927b2dcdc727c60da0b`.
- Mode: design-only, source review and non-destructive static probes. No implementation, tests, Telegram calls, tool execution, or live effects.
- Verdict rule: any unresolved finding of any severity is FAIL.

## Outcome

FAIL. Moving chunking out of the send callback and deleting the fallback are directionally correct, but v3 is not implementable without violating locked owners or leaving reachable delivery failures. The dominant weakness is that the plan treats multipart delivery as a text-splitting detail; in this repository it changes the terminal/outbox transaction, stable-ID consumers, reconciliation grammar, delivery evidence, and receiver ordering.

## Findings

### F1 — HIGH — Fence-tolerant execution violates the locked structured-output owner

**Evidence.** The plan strips a wrapping Markdown fence and executes a correctly-schemed call (`docs/PLAN-TGOUT.md:48-54`). Annex A says invalid structured output must never be silently accepted and, specifically, that security/effect payloads have no tolerant-accept path (`docs/HARNESS-SPEC.md:1560-1563`). The current protocol also defines the entire reply as exactly one JSON object and anything else as a final answer (`internal/llm/planner/planner.go:84-89`). A fenced object is not that protocol object.

**Concrete counterexample.** The user asks, “Show the exact `memory_remember` call as a fenced JSON example.” If the model returns one single fenced, correctly-schemed object, v3 executes it. The same bytes are indistinguishable from the proposed “accidentally fenced call,” so tests cannot resolve the intent ambiguity.

**Required fix.** Do not execute fence-stripped effect payloads. Classify the fenced valid-call shape as an invalid structured response and land it through the owner-approved terminal/re-ask path; a re-ask, if enabled, requires a new S7 `AttemptGrant`. Native provider function calling is the future unambiguous control channel.

**Detector requirement.** Add the display-example negative control above and assert zero tool calls/effects. The proposed “fenced valid tool call executes” test encodes the owner violation rather than detecting it.

### F2 — HIGH — Hard-splitting rendered HTML can manufacture invalid per-message HTML

**Evidence.** v3 splits the rendered string at a rune boundary and repairs only an open `<pre>` (`docs/PLAN-TGOUT.md:68-77`). The renderer can also construct `<b>`, `<code>`, and escaped entities (`docs/PLAN-TGOUT.md:78-89`). A 4,000-rune cut can land inside `<b>`, `</code>`, or `&amp;`, or inside the content of a long bold/code span. Closing/reopening `<pre>` alone does not repair those cases. Telegram requires supported, properly nested entities and escaping, and limits `sendMessage.text` to 1–4096 characters after entity parsing ([Telegram Bot API, `sendMessage` and HTML style](https://core.telegram.org/bots/api#sendmessage)).

**Concrete counterexamples.** Choose prefix padding so boundary 4,000 lands after `&am` in `&amp;`, inside `</b>`, or inside a >4,000-rune `**bold span**`. At least one emitted row is no longer valid Telegram HTML even if the unsplit renderer output was valid.

**Required fix.** Chunk the renderer's semantic tokens/content before final HTML serialization, then serialize each part independently. Each part must be independently valid and within the post-parse Telegram limit; never cut serialized tag/entity syntax.

**Detector requirement.** Validate every final outbox part, not only the pre-chunk renderer output. Pin deterministic boundary cases that cut every renderer token/entity kind at positions 3,999/4,000/4,001.

### F3 — HIGH — Enqueue order does not guarantee receiver order after a failed or ambiguous earlier part

**Evidence.** The plan promises N ordered rows but only says each row retains independent existing Flush semantics (`docs/PLAN-TGOUT.md:68-74`). Rows are read by creation order (`internal/channel/channel.go:451-466`), but `Flush` intentionally continues after a definite or ambiguous delivery failure so one poison row cannot starve later independent deliveries (`internal/channel/channel.go:488-537`; detector at `internal/channel/channel_test.go:458-485`). Therefore part 1 can re-pend or park UNKNOWN while part 2 is accepted and displayed first.

**Required fix.** Define group-local predecessor gating: part `i+1` is not send-eligible until part `i` is SENT. An UNKNOWN predecessor blocks only its own later parts; unrelated delivery groups must still progress. Persist explicit group/base ID and part index rather than relying only on lexical ID parsing.

**Detector requirement.** Force part 1 into both definite pre-wire failure and UNKNOWN, assert zero sends for parts 2..N, and simultaneously assert that a later unrelated delivery still sends. Then reconcile part 1 and prove parts resume in order.

### F4 — HIGH — The plan does not preserve the terminal-result plus complete multipart outbox transaction

**Evidence.** v3 names `EnqueueReply` as the split boundary (`docs/PLAN-TGOUT.md:68-74`), but ordinary Telegram replies are committed through `CompleteInbound`, not `EnqueueReply` (`internal/channel/telegram/telegram.go:270-283`). `CompleteInbound` is the locked B7 recipe and currently atomically appends one terminal event plus one outbox row (`internal/channel/channel.go:545-572`; owner at `docs/HARDQ-CONSOLIDATED.md:105-116`). The plan neither names this path nor requires terminal plus all N parts to commit in one `AppendBatch`.

**Impact.** Implementing only the named method leaves the main dogfood path unchunked. Sequentially adding parts around the existing transaction permits terminal state with an incomplete reply, violating the named B7 recipe.

**Required fix.** Specify one transport-neutral atomic API that accepts an already-rendered non-empty part set and commits terminal plus every part enqueue in one journal batch. Keep Telegram rendering outside the transport-neutral core. Cover `CompleteInbound` explicitly.

**Detector requirement.** Crash-ablate every position in terminal plus N-enqueue construction and require either terminal+all N rows or neither; zero terminal+prefix states are legal.

### F5 — HIGH — Stable-ID producers have no coherent multipart/evidence contract

**Evidence.** Refusals and reminders bypass `EnqueueReply` through caller-stable `EnqueueReplyID` (`internal/channel/telegram/telegram.go:209-247`; `cmd/nexus/main.go:1126-1147`). Reminder delivery evidence later queries and records the exact unsuffixed occurrence-derived delivery ID (`cmd/nexus/main.go:1137-1156`, `cmd/nexus/main.go:1295-1303`). v3 proposes only part rows named `dlv-<base>-p<i>-of-<n>` (`docs/PLAN-TGOUT.md:68-74`).

**Impact.** If stable-ID messages are split, the base reminder row never becomes SENT, so delivery evidence and inline ACK settlement cannot succeed. If `EnqueueReplyID` is exempt, a long user-controlled reminder body can remain unescaped/overlong while the adapter sends every row with HTML mode, disproving the constructive-validity claim. Refusals are short today, but share the same public enqueue path and replay contract.

**Required fix.** Define multipart derivation for both generated and caller-stable bases, plus a durable logical-delivery completion rule. For reminders, either enforce and prove a single-part escaped/limited reminder contract or make obligation delivery evidence depend on all occurrence-derived parts being SENT while preserving the same occurrence correlation.

**Detector requirement.** Cover long reminders, user text containing `<`, `&`, and Markdown, restart/replay, refusal idempotency, and ACK both before and after all parts become SENT.

### F6 — MEDIUM — Multipart IDs are incompatible with the existing reconciliation command grammar

**Evidence.** The plan's IDs carry a part suffix (`docs/PLAN-TGOUT.md:68-74`), while the only accepted `redeliver` ID shape is exactly `^dlv-[0-9a-f]{24}$` (`cmd/nexus/main.go:1231-1236`, `cmd/nexus/main.go:1325-1334`). The outbox command displays the actual row delivery ID (`cmd/nexus/main.go:1304-1324`). Thus a multipart UNKNOWN row can be shown to the owner but cannot be selected for reconciliation.

**Required fix.** Define one canonical multipart ID grammar and update every parser, formatter, destination guard, and test that consumes delivery IDs. Remove the ambiguous `dlv-<base>` notation: current bases already include `dlv-`; specify whether the canonical form is `<base>-p<i>-of-<n>`.

**Detector requirement.** Feed an actual generated part ID through `outbox` then `redeliver`; assert the exact row transitions UNKNOWN→PENDING for the same adapter and chat, while malformed/cross-destination IDs remain rejected.

### F7 — MEDIUM — “Balanced known tags” is not a sufficient Telegram HTML oracle

**Evidence.** The property oracle checks only balanced known tags and unescaped metacharacters (`docs/PLAN-TGOUT.md:78-87`). Telegram additionally restricts entity nesting: code/pre entities cannot participate in ordinary nested formatting, with only the documented `<pre><code class="language-…">` special form ([Telegram Bot API formatting options](https://core.telegram.org/bots/api#formatting-options)). A validator limited to v3's stated properties accepts `<b><code>x</code></b>` even though balance and escaping are not enough to establish Bot API acceptance.

**Required fix.** Define the renderer grammar and an independent validator that checks allowed parent/child relationships, complete entities, non-empty/post-parse size constraints, and each serialized chunk. Prefer a small parser/state machine over regex-only balance checks.

**Detector requirement.** The oracle must reject balanced-but-forbidden nesting, truncated entities/tags, empty parts, and >4096 post-parse characters. It must accept the exact renderer grammar.

### F8 — LOW — The proposed property-test ablation is not a causal production mutation

**Evidence.** v3 lists an ablation of “the property validator” (`docs/PLAN-TGOUT.md:90-94`). Removing or disabling the test oracle does not show that the oracle detects a broken renderer; it only removes the detector. No named negative mutation proves sensitivity to forbidden nesting or chunk-induced corruption.

**Required fix.** Keep the validator installed and mutate only production output generation: emit an unescaped ampersand, an unknown tag, crossed/forbidden nesting, and a chunk boundary inside an entity/tag. Each mutation must turn the same test RED for the intended reason.

### F9 — LOW — v3 contradicts itself about whether fallback delivery exists

**Evidence.** The normative v3 section removes all fallback sends (`docs/PLAN-TGOUT.md:78-84`), but the reference/adoption section still says the plan keeps “plain fallback” (`docs/PLAN-TGOUT.md:35-44`), and the risk section still says an entity-parse failure resends the original plain text (`docs/PLAN-TGOUT.md:103-110`). These statements prescribe mutually exclusive wire behavior.

**Required fix.** Delete the stale fallback-adoption and fallback-risk claims. State one rule only: exactly one transport call per outbox row; any 400 follows the existing definite-failure path with no adapter-owned second send.

## Rejected hypotheses / positive controls

- The chosen tags `<b>`, `<code>`, and `<pre>` are supported by Telegram HTML mode. The problem is per-chunk serialization/nesting proof, not the basic tag set.
- A 4,000-character target is a reasonable safety margin below Telegram's documented 4,096 post-parse limit, provided the metric is explicitly post-parse characters and each final part is checked.
- Independent rows correctly preserve the current per-row UNKNOWN/SENT honesty. The unresolved issue is dependency ordering and logical-delivery evidence across those rows.
- Deleting the adapter fallback is the correct response to the one-effect-per-attempt constraint. The stale contrary prose must also be deleted.

## Minimum coherent v4

1. Reject fenced tool calls as invalid effect/control payloads; keep zero tolerant execution.
2. Render into semantic tokens, partition tokens/content, and independently serialize+validate every Telegram HTML part.
3. Add a transport-neutral atomic terminal-plus-parts enqueue primitive; route every Telegram producer through an adapter-owned renderer before that primitive.
4. Persist logical delivery group/base, part index/count, and group-local eligibility; preserve poison-head progress across unrelated groups.
5. Define generated and caller-stable multipart IDs, reconciliation grammar, and reminder evidence/ACK semantics.
6. Replace test-removal “ablations” with production negative mutations and add the exact adverse cases listed above.
7. Remove all fallback prose.

## Proof ceiling

This is a design review of an immutable plan. No implementation exists at the target commit, so the review establishes owner conflicts, reachable counterexamples, and missing detector contracts; it does not establish runtime behavior or Telegram acceptance. No live Bot API probe was authorized or performed.

VERDICT: FAIL

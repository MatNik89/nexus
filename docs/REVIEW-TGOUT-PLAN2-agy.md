# Review of Design Plan v2: Telegram Output Quality (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `c1c827c` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` v2 resolves several critical issues from round 1:
- The in-band planner retry has been **eliminated**, fully preserving S7 `AttemptGrant` single-attempt invariants (`s7-min`).
- The schema drift signature has been narrowed to match known tool IDs (`action == <known-tool-id>` or `tool_id == <known-tool-id>`), preventing false positives on ordinary user JSON requests.
- The escape pipeline (tokenize $\rightarrow$ escape $\rightarrow$ wrap) and lossless table rules are explicitly defined.

However, deep architectural review along the Standards and Spec axes reveals **unresolved design and delivery-honesty flaws** that block approval:

1. **Multi-Chunk Partial-Delivery Honesty Violation (`channel.go:530-538`, `AGENTS.md` B2)**: If chunk 1 is delivered to Telegram but chunk 2 fails with a transport error, the adapter returns a standard error. `channel.Core.Flush` treats any standard error as a "definite pre-wire failure (nothing left the process)" and resets the row to `PENDING`. This causes duplicate re-delivery of chunk 1 on every flush tick.
2. **Error Decoding Blind Spot in Telegram Adapter (`telegram.go:161-167`)**: `telegram.Adapter.call` currently discards the HTTP 400 response body and returns a generic `HTTP 400` error string. The narrowed entity fallback (`strings.Contains(err, "can't parse entities")`) will never trigger in production unless `call` is updated to parse Telegram's JSON error description. Furthermore, Telegram entity errors include other phrases (`"can't find end of ... entity"`, `"tag ... is not allowed"`).
3. **Fenced Code Blocks Bypass Schema Drift Classifier**: Drifting models frequently wrap malformed tool JSON inside markdown fences (```` ```json {"action":"memory_recall"} ``` ````). The classifier checks only bare JSON objects and fails to detect fenced JSON, allowing raw tool calls to leak to chat.
4. **Single-Line Overflow in Line Chunker**: The chunker splits strictly on line boundaries. A line exceeding 4000 characters without newlines (e.g. minified data, long URLs) will not be split, exceeding Telegram's 4096-character limit and permanently failing delivery.
5. **Kernel Language Invariant (`AGENTS.md`)**: Hardcoding Croatian fallback text in `planner.go` violates the repository rule that all code and messages produced in the kernel must be in English.

Because unresolved design flaws exist across high, medium, and low severity tiers, the plan is **rejected**.

---

## Standards Review

### Documented Standards Compliance

- **[HARD VIOLATION] Delivery Honesty on Multi-Chunk Partial Failures (`AGENTS.md` B2, `ARCHITECTURE-ESSENTIALS.md` E15, `channel.go:530-538`)**:
  - `channel.Core.Flush` operates on the strict binary invariant:
    - `send == nil` $\rightarrow$ `SENT`
    - `errors.Is(send, ErrAmbiguousSend)` $\rightarrow$ `UNKNOWN` (parked for reconciliation)
    - Any other error $\rightarrow$ **DEFINITE PRE-WIRE FAILURE: nothing left the process** $\rightarrow$ Reconciled back to `PENDING` for clean re-attempt.
  - In `PLAN-TGOUT.md:88-93`, a message is split into multiple chunks under one delivery ID. If chunk 1 is accepted by Telegram (and is now visible on the user's screen) but chunk 2 fails with a network or HTTP error, returning a standard error to `Flush` falsely asserts that zero wire side-effects occurred.
  - `Core.Flush` resets the row to `PENDING` and re-sends chunk 1 on the next flush tick (every 2 seconds), causing duplicate message spam on the user's client.
  - **Required Fix**: Once any chunk in a multi-chunk batch succeeds on the wire, any subsequent chunk failure MUST return `channel.ErrAmbiguousSend` to park the delivery in `UNKNOWN`, or the adapter must manage chunk-level retry locally before returning.

- **[HARD VIOLATION] Repository Language Invariant (`AGENTS.md`)**:
  - `PLAN-TGOUT.md:59-60` specifies hardcoded Croatian text: `"Nisam uspio ispravno pozvati alat (<tool>). Pokušaj ponovno ili preformuliraj."` in `ChatPlanner`.
  - All kernel fallback strings in `daemon.go` and `telegram.go` are strictly English (e.g. `daemon.go:275`: `"I could not process that message. Try again, or check the daemon log."`).
  - Kernel code must use English error and fallback messages (`"Failed to execute tool (<tool>). Please try again or rephrase."`).

### Baseline Code Smells (Judgement Calls)

- **Feature Envy** (`PLAN-TGOUT.md:89`): The transport chunker inspects and manipulates HTML tag state (`"each chunk closes/reopens an open <pre>"`), coupling transport wire chunking to HTML tokenizer internals.
- **Primitive Obsession & Error Disconnect** (`PLAN-TGOUT.md:94-98`): Matching Telegram error classes via string substring matching on error text rather than structured error response structures.

---

## Spec & Design Review

### 1. Fenced Code Blocks Bypass Schema Drift Classifier (High Severity)

- **Plan Claim**: `PLAN-TGOUT.md:52-58` — *"After `toolCallFromReply` says not-a-tool, classify the reply as SCHEMA DRIFT only when it is a single JSON object AND (its top-level `action` value equals a KNOWN tool id, OR it has a top-level `tool_id` field whose value is a known tool id)."*
- **Flaw**:
  - Many LLM providers format structured responses with markdown code fences:
    ````markdown
    ```json
    {
      "action": "memory_recall",
      "query": "prva poruka danas"
    }
    ```
    ````
  - `strings.TrimSpace(reply)` on this response begins with ```` ``` ````, not `{`.
  - The drift classifier fails to recognize it as a JSON object, treats it as legitimate prose, and delivers the raw fenced tool JSON straight to Telegram.
- **Required Fix**:
  - The schema drift classifier must strip outer markdown code fences (e.g. ```` ```json ... ``` ```` or ```` ``` ... ``` ````) before attempting JSON decoding.

### 2. Error Decoding Blind Spot in Telegram Adapter (High Severity)

- **Plan Claim**: `PLAN-TGOUT.md:94-98` — *"plain resend ONLY when the Telegram error description contains 'can't parse entities' (the entity-parse class). Every other 400 keeps the existing failure path..."*
- **Flaw**:
  - In `internal/channel/telegram/telegram.go:161-167`, `a.call` handles HTTP 400 by executing:
    ```go
    if resp.StatusCode < 200 || resp.StatusCode > 299 {
        return fmt.Errorf("telegram: %s HTTP %d", method, resp.StatusCode)
    }
    ```
  - It does NOT decode the HTTP response body on 4xx status codes.
  - The error returned by `a.call` is literally `"telegram: sendMessage HTTP 400"`.
  - The check `strings.Contains(err.Error(), "can't parse entities")` will ALWAYS evaluate to `false`, completely disabling the plain fallback in production.
  - Furthermore, Telegram Bot API entity parse errors include several distinct descriptions depending on syntax:
    - `"Bad Request: can't parse entities: ..."`
    - `"Bad Request: can't find end of ... entity"`
    - `"Bad Request: tag ... is not allowed"`
    - `"Bad Request: unmatched start tag"`
- **Required Fix**:
  - `telegram.go` must decode the Bot API error response body (`{"ok": false, "error_code": 400, "description": "..."}`).
  - The entity-error classifier must match the broad entity parse error class (e.g. checking for `"can't parse entities"`, `"entity"`, or `"tag"`).

### 3. Single-Line Overflow in Line Chunker (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:88-93` — *"before send, split the RENDERED text on line boundaries into <=4000-char chunks"*.
- **Flaw**:
  - If the model generates a line with no newlines exceeding 4000 characters (e.g. base64 data, minified JSON, long URLs, hex dumps), line-boundary splitting will fail to chunk the line.
  - The resulting chunk will exceed Telegram's 4096-character limit, resulting in `Bad Request: message is too long`.
  - Because this is not an entity parse error, plain fallback will not trigger, and the message will permanently fail delivery.
- **Required Fix**:
  - The chunker must split primarily on line boundaries, but include a hard rune-slice fallback at 4000 runes when an individual line exceeds the budget.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | Telegram | **HIGH** | Multi-chunk partial failure violates `channel.Core.Flush` pre-wire assumption, triggering duplicate re-sends. | `PLAN-TGOUT.md:88-93`, `channel.go:530-538` |
| **F2** | Telegram | **HIGH** | `telegram.go:call` discards HTTP 400 response body; `"can't parse entities"` fallback check will never match. | `PLAN-TGOUT.md:94-98`, `telegram.go:161-167` |
| **F3** | Planner | **MEDIUM** | Fenced JSON (` ```json {...} ``` `) bypasses bare-JSON schema drift classifier, leaking raw tool calls. | `PLAN-TGOUT.md:52-58` |
| **F4** | Telegram | **MEDIUM** | Line-boundary chunker fails on single lines > 4000 characters, exceeding Telegram's 4096-character limit. | `PLAN-TGOUT.md:88-93` |
| **F5** | Kernel | **LOW** | Hardcoded Croatian fallback string in planner violates English-only repository standard. | `PLAN-TGOUT.md:59-60`, `AGENTS.md` |

---

## Required Plan Revisions

Before implementation begins, `docs/PLAN-TGOUT.md` must be revised to:
1. Specify that multi-chunk sends treat post-chunk-1 failures as partial deliveries (`ErrAmbiguousSend`) or handle chunk retry locally.
2. Specify decoding the Telegram Bot API error description in `telegram.go` on 4xx status codes and matching the full family of entity parse errors.
3. Strip markdown code fences in the schema drift classifier before JSON parsing.
4. Add a hard 4000-rune split fallback for lines without newlines.
5. Translate kernel fallback strings to English.

---

VERDICT: FAIL

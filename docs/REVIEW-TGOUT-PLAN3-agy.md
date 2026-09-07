# Review of Design Plan v3: Telegram Output Quality (`docs/PLAN-TGOUT.md`)

**Scope**: Review of `docs/PLAN-TGOUT.md` (commit `7a976cc` on branch `slice/p0-tgout`)  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-07  
**Base Revision**: `37d9ac3` (`origin/main`)  

---

## Executive Summary

`docs/PLAN-TGOUT.md` v3 demonstrates substantial architectural progress:
- Multi-send loops inside the adapter's `Flush` callback have been **eliminated**.
- Chunking has been moved to the enqueue boundary ($N$ distinct outbox rows with independent wire effects), fully preserving T22/B2 delivery honesty.
- Fallback re-sends on 400 errors have been removed in favor of constructively valid Telegram HTML.
- Tool parsing is made fence-tolerant for drifting and valid calls alike.

However, deep evaluation of the updated design against repo contracts and Telegram Bot API mechanics uncovers **material design gaps** that require revision before implementation:

1. **`CompleteInbound` B7 Batch Recipe Omission**: Inbound chat turns commit outbox replies through `CompleteInbound` (`channel.go:551`, `telegram.go:282`), not `EnqueueReply`. If chunking is specified only on `EnqueueReply`, chat replies >4000 characters bypass chunking entirely. Furthermore, all $N$ chunk rows must commit in the same atomic `AppendBatch` as `EvInboundTerminal`.
2. **Reminder Delivery Receipts Broken by Part Suffixing**: The reminder loop (`main.go:1137-1144`) enqueues under caller-stable IDs (`dlv-remind-<occ_id>`) and polls `DeliveryStatus(ctx, id) == "SENT"`. If single-part or multi-part messages are unconditionally suffixed with `-p1-of-N`, `DeliveryStatus` on the base ID fails, stranding reminders in `DELIVERY_PENDING`. Single-part messages ($N=1$) must retain their exact base ID.
3. **Unclosed `<b>` and `<code>` Entity Splitting Across Chunks**: The plan specifies reopening `<pre>` across chunks, but omits inline `<b>` and `<code>` spans. Line-boundary splitting across multi-line bold spans emits unclosed `<b>` in part $i$ and orphan `</b>` in part $i+1$, triggering fatal Telegram 400 entity parse rejections.
4. **Rune Limit vs. Telegram UTF-16 Code Unit Limit**: Telegram's 4096-character limit is enforced in **UTF-16 code units**, not unicode runes. 4000 emoji runes equal 8000 UTF-16 code units and will fail wire delivery.
5. **Fence-Strip Mutation on Legitimate Prose Finals**: Stripping code fences in-place before tool checks mutates legitimate single-block prose responses (e.g. code snippets), delivering them with code fences stripped.
6. **Stale Tradeoff Section Reference**: Line 107–108 still references the removed plain fallback send.

Because unresolved flaws exist across high, medium, and low severity tiers, the plan is **rejected**.

---

## Standards Review

### Documented Standards Compliance

- **[HARD VIOLATION] B7 Commit Boundary & `CompleteInbound` Batch Atomicity (`ARCHITECTURE-ESSENTIALS.md` E4, HARDQ B7, `channel.go:551-573`)**:
  - `telegram.go:281-283` executes the canonical B7 recipe:
    ```go
    _, err = a.core.CompleteInbound(ctx, outcome.MessageID, adapterID, identity, a.profile, reply)
    ```
  - `CompleteInbound` commits `EvInboundTerminal` and `EvOutboundEnqueued` in ONE transaction via `c.j.AppendBatch`.
  - `PLAN-TGOUT.md:69-70` specifies chunking exclusively for `EnqueueReply`. Inbound chat turns do not call `EnqueueReply`.
  - **Required Invariant**: `CompleteInbound` (or the Telegram adapter before calling it) must split the reply into $N$ parts and commit `[EvInboundTerminal, EvOutboundEnqueued_1, ..., EvOutboundEnqueued_N]` in a single atomic `AppendBatch`.

- **[HARD VIOLATION] Reminder Evidence Contract & Caller-Stable Delivery IDs (`AGENTS.md` HARDQ B5, `channel.go:406-429`)**:
  - Reminders require durable delivery receipts correlated to the exact occurrence ID (`dlv-remind-<occurrence_id>`).
  - The reminder processor queries `c.DeliveryStatus(ctx, id)` using the exact caller-provided ID.
  - If a single-part notification is assigned `dlv-remind-...-p1-of-1`, `DeliveryStatus` on the base ID returns empty.
  - **Required Invariant**: When $N = 1$, the delivery ID must remain exactly `<base>`. Suffixing (`-p<i>-of-<n>`) must apply strictly when $N > 1$.

- **[HARD VIOLATION] Contradictory Delivery Contract in Text (`ARCHITECTURE-ESSENTIALS.md` E15, `PLAN-TGOUT.md:78, 107-108`)**:
  - `PLAN-TGOUT.md:78` states *"NO FALLBACK SEND... no second send path exists"*.
  - `PLAN-TGOUT.md:107-108` contradicts this: *"the narrow entity-parse fallback re-sends the original plain"*.
  - Line 108 is a stale v2 artifact and must be deleted.

### Baseline Code Smells (Judgement Calls)

- **Feature Envy / Leaky Abstraction** (`PLAN-TGOUT.md:69-77`): Assigning HTML-aware line splitting and tag balancing to transport-neutral `channel.Core`. HTML formatting and chunking belongs in `internal/channel/telegram` before calling `Core.CompleteInbound` or `Core.EnqueueReply`.
- **Destructive Mutation Smell** (`PLAN-TGOUT.md:50-53`): Modifying the planner's internal reply buffer during fence stripping rather than treating stripped text as a temporary probe.

---

## Spec & Design Review

### 1. Tag Splitting Across Chunk Boundaries (High Severity)

- **Plan Claim**: `PLAN-TGOUT.md:76-77` — *"a split inside an open `<pre>` closes it and reopens in the next part."*
- **Flaw**:
  - The plan only handles `<pre>`, ignoring `<b>` and `<code>`.
  - Markdown text frequently contains multi-line bold spans or inline code spanning linebreaks:
    ```markdown
    **Important Notice:
    Please review the following items carefully before proceeding.**
    ```
  - If a line split occurs between "Important Notice:" and "Please review...", Part 1 ends with an unclosed `<b>`, and Part 2 begins with an orphan `</b>`.
  - Telegram Bot API strictly rejects both with HTTP 400 (`Bad Request: can't parse entities: Unclosed start tag` / `Unexpected end tag`).
  - Because v3 eliminated the plain fallback, this is a **fatal terminal delivery failure**.
- **Required Fix**:
  - The chunker must track all active open tags (`<b>`, `<code>`, `<pre>`), close all open tags at the boundary of chunk $i$, and reopen them in identical order at the start of chunk $i+1$.

### 2. Character Limit Units: UTF-16 Code Units vs. Runes (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:75` — *"Split on line boundaries at <=4000 chars; a single line longer than 4000 hard-splits on a rune boundary"*.
- **Flaw**:
  - Telegram Bot API enforces its 4096-character limit in **UTF-16 code units** (the same unit used by `MessageEntity.offset`).
  - Characters outside the Basic Multilingual Plane (such as emojis, symbols, and mathematical alphanumeric characters) occupy 2 UTF-16 code units (a surrogate pair) despite being a single Go rune.
  - A message with 2,200 emoji runes is 2,200 runes long, but 4,400 UTF-16 code units, causing Telegram to reject it with `Bad Request: message is too long`.
- **Required Fix**:
  - Chunk size budget must be calculated using UTF-16 code units (`len(utf16.Encode([]rune(text)))`), or use a conservative rune budget (e.g. 3500 runes).

### 3. Prose Output Corruption by Fence Stripping (Medium Severity)

- **Plan Claim**: `PLAN-TGOUT.md:50-53` — *"before `toolCallFromReply`, strip ONE wrapping markdown code fence (``` or ```json ... ```) if the entire reply is a single fenced block. A correctly-schemed tool call inside a fence then EXECUTES... a drifted schema inside a fence hits the same drift classifier."*
- **Flaw**:
  - If the user asks for code (e.g. *"Show me a hello world script in Python"*), the model may output solely a fenced code block:
    ````markdown
    ```python
    print("Hello, world!")
    ```
    ````
  - If the planner strips the code fence in-place and subsequently determines it is neither a valid tool call nor a schema drift, returning the stripped text corrupts the user's requested markdown formatting.
- **Required Fix**:
  - The stripped string must be used as a probe for `toolCallFromReply` and schema drift detection only. If neither matches, the original unstripped `reply` must be returned as the `Final` action.

---

## Detailed Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | Telegram | **HIGH** | `CompleteInbound` omitted from chunking specification; B7 atomic batch commit undefined for chat replies. | `PLAN-TGOUT.md:69-70`, `channel.go:551-573`, `telegram.go:281-283` |
| **F2** | Channel | **HIGH** | Unconditional `-p<i>-of-<n>` suffixing breaks `DeliveryStatus` queries on caller-stable reminder IDs. | `PLAN-TGOUT.md:70-71`, `channel.go:406-435` |
| **F3** | Renderer | **HIGH** | Chunking ignores open `<b>` and `<code>` tags across line splits, causing fatal Telegram 400 unclosed tag rejections. | `PLAN-TGOUT.md:76-77` |
| **F4** | Renderer | **MEDIUM** | Chunk budget calculates unicode runes rather than Telegram's UTF-16 code units, risking `message is too long` on emoji-rich replies. | `PLAN-TGOUT.md:75-76` |
| **F5** | Planner | **MEDIUM** | In-place fence stripping mutates legitimate single-block fenced prose finals, stripping requested code fences. | `PLAN-TGOUT.md:50-53` |
| **F6** | Docs | **LOW** | Stale reference to removed plain fallback send in Risks/Tradeoffs section. | `PLAN-TGOUT.md:107-108` |

---

## Required Plan Revisions

Before implementation begins, `docs/PLAN-TGOUT.md` must be revised to:
1. Explicitly specify that `CompleteInbound` chunks replies and commits all $N$ outbox events in the same atomic `AppendBatch` as `EvInboundTerminal`.
2. Guarantee that single-part deliveries ($N=1$) preserve the exact caller-provided `DeliveryID` without suffixing.
3. Require the chunker to close and reopen all active HTML tags (`<b>`, `<code>`, `<pre>`) across chunk boundaries.
4. Calculate chunk length against a 4000 UTF-16 code unit ceiling (or a safe 3500 rune ceiling).
5. Preserve the original unstripped response for prose finals when fence stripping is used during tool probing.
6. Delete the stale fallback reference at line 108.

---

VERDICT: FAIL

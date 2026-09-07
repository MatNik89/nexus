# QA Verification Report: Conversation History Slice (slice/p0-conv-history)

**Scope**: Commit `3640982` + `06cc664` on branch `slice/p0-conv-history` (HEAD `06cc6643a98818f9bbfdf12f8a4c8fbaf28a5b93`).  
**Reviewer**: `agy` (Antigravity QA / Reviewer)  
**Date**: 2026-09-07  
**Repository**: `/home/matej/HARNESS/nexus`  
**Base**: `53353d1` (`origin/main`)  

---

## Executive Summary

The `slice/p0-conv-history` slice addresses the dogfood context hole where every incoming turn responded with "no prior context". The implementation adapts the industry-standard multi-turn conversational pattern (`karfly/chatgpt_telegram_bot` / OpenClaw / Hermes-Agent / AstrBot) by lifting conversation history out of a flattened single user prompt into structured, alternating `user` and `assistant` role messages (`provider.ChatMessage`).

All 4 key regression detectors were verified as strictly **RED-capable** under deliberate mutation and ablation. The full project test suite (`go test -count=1 ./...`) passes cleanly across all packages.

However, an adversarial audit revealed **4 material defects and code smells** (including a UTF-8 character splitting bug on 1500-byte boundaries and an unpaired-role asymmetry during uncompleted/suspended turns in channel history). Under the strict repo review rule (*"FAIL for ANY unresolved finding of ANY severity (high, medium, AND low)"*), this slice is scored **FAIL**.

---

## Two-Axis Review

### Standards

Evaluated against [AGENTS.md](file:///home/matej/HARNESS/nexus/AGENTS.md), [ARCHITECTURE-ESSENTIALS.md](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), and Fowler baseline smells.

1. **[LOW / Code Defect] UTF-8 Rune Splitting on Cap Boundary** ([internal/app/daemon/daemon.go:382-386](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L382-L386))
   - `clip` truncates strings via raw byte slicing:
     ```go
     const entryCap = 1500
     clip := func(s string) string {
         if len(s) > entryCap {
             return s[:entryCap] + "…"
         }
         return s
     }
     ```
   - In Go, `len(s)` returns byte count, and `s[:1500]` slices raw byte offsets. When text contains multi-byte UTF-8 runes (such as Croatian diacritics `č, ć, š, ž, đ` [2 bytes] or emoji [3–4 bytes]) crossing byte index 1500, slicing mid-character corrupts the rune into invalid UTF-8 bytes.
   - Downstream JSON serialization (`json.Marshal`) or LLM provider HTTP decoders will either insert unicode replacement characters (`\ufffd`) or reject the payload with unmarshal errors. In addition, appending `…` (3 bytes) produces a 1503-byte output, exceeding the cap.
   - **Fix**: Slices must truncate at valid UTF-8 rune boundaries (e.g. via `utf8.DecodeLastRuneInString` or converting to `[]rune`).

2. **[LOW / Code Smell] Stale Architectural Header Comment** ([internal/app/daemon/daemon.go:295-308](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L295-L308))
   - The docstring for `conversationHistory` states:
     > *"folds this identity's past channel turns ... into ONE TrustUser block — not per-message blocks — because the assembler orders by trust class then BlockID, which would split a multi-trust history out of chronology."*
   - This header describes the initial single-block approach from commit `3640982` that was superseded in commit `06cc664`. In `06cc664`, history is emitted as separate `history_user` and `history_assistant` blocks for `planner.splitHistory`. The comment directly contradicts the current implementation.

3. **[LOW / Code Smell] Dead Constant and Slice Index Parsing Indirection** ([internal/app/daemon/daemon.go:210-223, 313-314, 381](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L210-L223))
   - `const entryCap = 1500` is declared at line 314 inside `conversationHistory` where it is unused/dead, and re-declared at line 381 inside `historyBlocks`.
   - `const historyMaxPairs = 12` is declared at line 313, but the chat session loop in `handleChat` hardcodes the raw literal `12` at line 210 (`if len(hist) > 12 { start = len(hist) - 12 }`).
   - `handleChat` converts integer array indices to strings (`ids[i] = fmt.Sprintf("%d", i)`) only to parse them back via `fmt.Sscanf(id, "%d", &i)` inside the callback to satisfy `historyBlocks`'s string-keyed signature.

---

### Spec

Evaluated against PRD §4/§6, Annex A Contracts, and dogfood multi-turn history specifications.

1. **[LOW / Logic Asymmetry] Incomplete/Suspended Turns Injected as Unpaired User Messages** ([internal/app/daemon/daemon.go:320-358](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L320-L358))
   - In `conversationHistory`, every `channel.inbound_admitted` event records the turn into `order` and creates a `pair{user: p.Text}`.
   - If a turn failed, panicked, or suspended for HITL approval (`approval.turn_suspended`), no subsequent `turn.succeeded` event is ever recorded for that turn.
   - When generating context blocks, `historyBlocks` produces a `history_user` block for this turn, but `pairs[tid].final` is empty (`""`), so no `history_assistant` block is emitted.
   - Downstream in `planner.splitHistory`, this generates consecutive `user` role messages without an intervening `assistant` reply (`[system, user_hist_1, user_hist_2, user_current]`).
   - In contrast, the interactive chat session path (`handleChat`, line 236) updates `hist` *only* after `RunTurn` succeeds (`hist = append(hist, histPair{...})`), maintaining strict user/assistant alternation.
   - **Fix**: The channel replay should omit turns that lack a succeeded final, or collapse consecutive user messages.

2. **[LOW / Test Coverage] Missing FIFO Cap and Boundary Assertions** ([internal/app/daemon/daemon_test.go:608-686](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L608-L686))
   - Neither `TestChannelTurnCarriesConversationHistory` nor `TestChatSessionCarriesHistory` exercises the 12-pair FIFO sliding window cap or the 1500-character truncation logic. `TestChatSessionCarriesHistory` tests only a 2-turn single session without verifying cross-session isolation.

---

## Architectural & Security Verification

| Dimension | Assessment | Evidence |
| :--- | :--- | :--- |
| **Trust & Prompt Injection** | **PASS** | Past user messages remain at `TrustUser` with `Kind: "history_user"`. Assistant finals are emitted as `Kind: "history_assistant"` and mapped by `planner.splitHistory` directly to `provider.ChatMessage{Role: "assistant"}`. Model outputs are never re-minted as user-trust instruction text. Current user prompt is assembled separately via `assembler.Base` and placed strictly last in the message sequence. |
| **Cross-Identity Isolation** | **PASS** | `conversationHistory` filters channel admissions strictly by `identity` matching the turn ID scheme (`turn-chan-<identity>-<updateID>`). Verified by `TestChannelTurnCarriesConversationHistory` where `chat-99` cannot observe `chat-42`'s history. Interactive session history in `handleChat` is connection-scoped and in-memory. |
| **Journal Replay Cost** | **PASS** | Channel history performs a linear $O(\text{events})$ scan via `d.deps.Journal.Replay(0, ...)`. In P0, with bounded retention and single-user profile DBs, this replay takes $<2\text{ms}$. Future transcript projection trigger is acknowledged for P1+. |
| **HITL & Suspensions** | **PASS** | Active suspension challenges recover their summary correctly via `completedTurnFinal` / `approval.turn_suspended`. Rehydration resumes into the original turn without duplicating context. |
| **FIFO & Capping** | **PARTIAL** | Capped at 12 pairs / 1500 bytes per entry. However, clipping splits multi-byte UTF-8 runes (Finding #1). |
| **System Language Directive** | **PASS** | System prompt instructs replying in Croatian (`hrvatski`) across both channel and CLI sessions, verified in `TestHistoryBlocksBecomeRoleMessages`. |

---

## Empirical RED Ablation Probes

All 4 detectors were proven RED-capable through targeted mutations in an isolated on-disk workspace:

| Detector | Mutation / Ablation | Observed RED Failure |
| :--- | :--- | :--- |
| `TestHistoryBlocksBecomeRoleMessages` | Drop `splitHistory` (pass raw blocks to assembler) | `FAIL: role order [system user], want [system user assistant user]` |
| `TestHistoryBlocksBecomeRoleMessages` | Remove Croatian language directive from `systemPrompt` | `FAIL: system prompt lost the language directive: "You are NEXUS..."` |
| `TestChannelTurnCarriesConversationHistory` | Disable `conversationHistory` in `RunChannelTurn` | `FAIL: turn 2 does not see turn 1's user message` |
| `TestChatSessionCarriesHistory` | Disable `hist` accumulation in `handleChat` | `FAIL: second turn does not carry the session history` |

---

## Top-3 Weakest Points

1. **[internal/app/daemon/daemon.go:382-386](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L382-L386)**: `s[:1500]` byte slicing cuts multi-byte UTF-8 characters (Croatian diacritics / emoji) mid-character, generating malformed UTF-8 payloads sent to LLM providers.
2. **[internal/app/daemon/daemon.go:320-358](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L320-L358)**: Channel history replays admitted turns without verifying turn success, causing failed or suspended turns to emit unpaired user messages and break alternating role sequences.
3. **[internal/app/daemon/daemon.go:295-308](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L295-L308)**: Header docstring describes superseded single-block design, contradicting the current role-message implementation.

---

VERDICT: FAIL

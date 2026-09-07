# QA Verification Report: Conversation History Slice (Round 3)

**Scope**: Commit `017e051` on branch `slice/p0-conv-history` (HEAD `017e05191656dfe17944a18fd75b25b6bb45faf4`).  
**Reviewer**: `agy` (Antigravity QA / Reviewer)  
**Date**: 2026-09-07  
**Repository**: `/home/matej/HARNESS/nexus`  
**Base**: `53353d1` (`origin/main`)  

---

## Executive Summary

Commit `017e051` resolves all remaining round-2 findings across reviewers. The turn completion semantic is explicitly decoupled from payload string content via a dedicated `completed` bit recorded on the `turn.succeeded` journal event. Dedicated, committed regression suites (`TestConversationHistoryRules` and `TestSessionHistoryStoresRedactedFinal`) now rigidly guard every history aggregation rule: completed-only filtering, post-filter FIFO sliding window, exact B3 identity isolation under prefix-sharing identities, rune-safe UTF-8 clipping, empty final handling, and secret redaction propagation. Furthermore, `splitHistory`'s trust-checking leg is fortified and verified against untrusted-external blocks, the interactive session history is bounded in place to `historyMaxPairs = 12`, and the `fmt.Sprintf` / `fmt.Sscanf` index round-trip is eliminated with `[]histPair`.

All tests in the repository (`CGO_ENABLED=0 go test -count=1 ./...`) pass cleanly. All 6 detector and ablation probes were independently proven **RED-capable** in a clean on-disk export.

---

## Verification of Round-2 Finding Closures

1. **[codex #1] Turn Completion Owned by `turn.succeeded` Event ([internal/app/daemon/daemon.go:290-293, 331-336, 348-353](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L290-L293))**:
   - Completion status is tracked by an explicit boolean `pr.completed = true` set unconditionally upon receiving `turn.succeeded`, regardless of whether `p.Final` is non-empty. Turns with empty replies are recognized as completed and properly emitted into history without breaking role alternation. **CLOSED**.

2. **[codex #2 / kilo F2 / agy L5] Committed Daemon Regression Detectors ([internal/app/daemon/daemon_test.go:699-780, 786-821](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L699-L780))**:
   - `TestConversationHistoryRules` exercises and asserts all 5 boundary invariants in a single multi-turn scenario:
     - *Completed-only filter*: An admitted-but-never-run update (ID 100) is filtered out.
     - *Cap-AFTER-filter FIFO*: 14 completed turns retain strictly the newest 12 completed turns without uncompleted turns consuming window slots.
     - *Exact B3 membership*: Prefix identity `chat-42-x` does not leak into `chat-42`.
     - *Empty-final rule*: Empty final turns are retained and asserted.
     - *Rune-safe clip*: 1600 runes of `š` are clipped without splitting UTF-8 bytes.
   - `TestSessionHistoryStoresRedactedFinal` uses a marking redactor (`sk-VERYSECRET` $\rightarrow$ `[REDACTED]`) to verify that the session history feeds the redacted text back to the provider on subsequent turns. **CLOSED**.

3. **[kilo F1] Trust-Leg Spoof Negative Case ([internal/llm/planner/planner_test.go:390-396, 418-425](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner_test.go#L390-L396))**:
   - Added `zz-trust-spoof` (`Producer: "daemon"`, `SourceURI: "nexus://h"`, `Trust: TrustUntrustedExternal`) to `TestHistoryBlocksBecomeRoleMessages`. Asserts that despite matching daemon producer and nexus URI prefix, untrusted blocks are fenced through `assembler.Base` and excluded from provider role messages. **CLOSED**.

4. **[codex #4] In-Place Sliding Window for Interactive Sessions ([internal/app/daemon/daemon.go:225-229](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L225-L229))**:
   - Session history is truncated in place with `hist = append(hist[:0], hist[len(hist)-historyMaxPairs:]...)` on every turn, preventing unbounded memory growth in long-running REPL sessions. **CLOSED**.

5. **[kilo F3] `historyBlocks` Accepts `[]histPair` Directly ([internal/app/daemon/daemon.go:207, 357, 363, 375](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L207))**:
   - Replaced stringified integer slice indices (`fmt.Sprintf("%d", i)`) and `fmt.Sscanf` callback parsing with direct `[]histPair` passing. Shared `const historyMaxPairs = 12` is used across both channel and chat paths. **CLOSED**.

6. **[codex #3] Stale Documentation Cleanup ([internal/app/daemon/daemon.go:257, 407](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L257))**:
   - `RunChannelTurn` docstring updated to reference role blocks; stray `completedTurnFinal` comments moved to their proper function. **CLOSED**.

---

## Two-Axis Review

### Standards

Evaluated against [AGENTS.md](file:///home/matej/HARNESS/nexus/AGENTS.md), [ARCHITECTURE-ESSENTIALS.md](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), and Fowler baseline smells.

- **Documented Standards Compliance**: **PASS**. Trust lineage is strictly monotone; secret redaction is active across all persistence and history feedback paths; fail-closed behavior on unknown inputs and untrusted sources is maintained; single-binary and pure Go constraints are preserved.
- **Baseline Code Smells (Judgement Calls)**:
  1. *Data Clump / Struct Duplication* ([daemon.go:290-293, 363](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L290-L293)): Local `pair` struct (`user, final string; completed bool`) duplicates fields of package-level `histPair` (`user, final string`).
  2. *Primitive Obsession* ([planner.go:57](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L57)): URI scheme check uses `strings.HasPrefix(b.SourceURI, "nexus://")` instead of structured URI parsing.
  3. *Minor Windowing Divergence* ([daemon.go:225-228, 354-356](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L225-L228)): Chat session uses in-place `append(hist[:0], ...)` while channel history re-slices `entries = entries[...]`.

---

### Spec

Evaluated against PRD §4/§6, Annex A Contracts, and multi-turn history specifications.

- **Requirements Completeness**: **PASS**. All 7 spec requirements (explicit completion tracking, committed detectors, trust spoofing negative tests, in-place session bounds, clean `[]histPair` signature, comment accuracy, and Croatian language directives) are verified.
- **Scope Creep**: **None**.
- **Implementation Soundness**: **PASS**.

---

## Empirical RED Ablation Probes

All detectors were verified in a clean on-disk export:

| Detector | Mutation / Ablation | Observed RED Result |
| :--- | :--- | :--- |
| `TestConversationHistoryRules` | Make `completed = true` conditional on `p.Final != ""` | `FAIL: empty-final turn misclassified as incomplete` |
| `TestSessionHistoryStoresRedactedFinal` | Store unredacted `final` in session `hist` | `FAIL: raw secret re-fed to the provider via session history: "the token is sk-VERYSECRET"` |
| `TestHistoryBlocksBecomeRoleMessages` | Remove `b.Trust == TrustUser` check in `splitHistory` | `FAIL: role order [system user assistant assistant user], want [system user assistant user]` |
| `TestHistoryBlocksBecomeRoleMessages` | Remove `Producer`/`SourceURI` checks in `splitHistory` | `FAIL: role order [system user assistant assistant assistant user], want [system user assistant user]` |
| `TestHistoryBlocksBecomeRoleMessages` | Remove Croatian language directive from `systemPrompt` | `FAIL: system prompt lost the language directive` |
| `TestChannelTurnCarriesConversationHistory` | Disable `conversationHistory` in `RunChannelTurn` | `FAIL: turn 2 does not see turn 1's user message` |

---

## Top-3 Weakest Points

1. **[internal/app/daemon/daemon.go:395-408](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L395-L408)**: When a completed turn has an empty reply (`final == ""`), `historyBlocks` emits a `history_assistant` block with empty text, which yields an empty assistant role message in the provider payload.
2. **[internal/app/daemon/daemon.go:290-293, 363](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L290-L293)**: `type pair struct { user, final string; completed bool }` duplicates the `(user, final)` field structure of package-level `histPair` rather than embedding it.
3. **[internal/llm/planner/planner.go:57](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L57)**: `strings.HasPrefix(b.SourceURI, "nexus://")` performs substring matching rather than formal URI scheme parsing.

---

VERDICT: PASS

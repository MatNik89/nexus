# QA Verification Report: Conversation History Slice (Round 2)

**Scope**: Commit `22dc6c9` on branch `slice/p0-conv-history` (HEAD `22dc6c9d4ce99bda7d03ac2911ed0906f9576a8c`).  
**Reviewer**: `agy` (Antigravity QA / Reviewer)  
**Date**: 2026-09-07  
**Repository**: `/home/matej/HARNESS/nexus`  
**Base**: `53353d1` (`origin/main`)  

---

## Executive Summary

Commit `22dc6c9` comprehensively addresses all round-1 review findings across codex (1 HIGH, 1 MED, 2 LOW), kilo (3 LOW), and agy (1 MED, 2 LOW). The trust boundary on `splitHistory` is now strictly gated by producer, trust class, and URI origin with a committed negative-control regression; channel history enforces completion filtering before the 12-pair FIFO cap; identity matching is exact set membership; session history stores redacted finals; string truncation operates on Unicode runes; and stale documentation/dead constants are removed.

All tests across the entire repository (`CGO_ENABLED=0 go test -count=1 ./...`) pass cleanly. All 5 regression and ablation detectors were independently verified as **RED-capable** in an isolated on-disk workspace.

---

## Verification of Round-1 Finding Closures

1. **[codex HIGH] Trust Spoofing Gate in `splitHistory` ([internal/llm/planner/planner.go:49-62](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L49-L62))**:
   - `splitHistory` now enforces that `history_user` / `history_assistant` blocks are extracted into provider roles *only* if they satisfy `b.Producer == "daemon" && b.Trust == contracts.TrustUser && strings.HasPrefix(b.SourceURI, "nexus://")`.
   - Any external or spoofed blocks (e.g. emitted by tools or untrusted external sources) are retained in `rest` and flow into the prompt via `assembler.Base` under the untrusted data fence.
   - Verified via negative-control regression in `TestHistoryBlocksBecomeRoleMessages` ([planner_test.go:377-404](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner_test.go#L377-L404)). Ablating the gate turns the test **RED** (`role order [system user assistant assistant user], want [system user assistant user]`). **CLOSED**.

2. **[codex MED] Completed-Pairs Filter & Post-Filter FIFO Window ([internal/app/daemon/daemon.go:351-366](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L351-L366))**:
   - `conversationHistory` filters admitted turns for completed exchanges (`pairs[id].final != ""`) into a `completed` slice before applying the 12-pair sliding window cap. Suspended or failed turns do not emit unpaired user messages or consume the history window.
   - In `handleChat` ([daemon.go:236-239](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L236-L239)), interactive history appends *only* after `l.RunTurn` completes without error. **CLOSED**.

3. **[kilo F1] Exact Set Membership in `turn.succeeded` Filter ([internal/app/daemon/daemon.go:332-349](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L332-L349))**:
   - Prefix matching (`strings.HasPrefix`) was replaced with exact map membership `if _, ok := pairs[tid]; ok`, where `pairs` is populated strictly from admissions matching `p.ChannelIdentity == identity`. Identities sharing hyphen prefixes (e.g. `chat-4` vs `chat-4-x`) cannot collide or leak. **CLOSED**.

4. **[kilo F3] Redacted Finals in Session History ([internal/app/daemon/daemon.go:236-239](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L236-L239))**:
   - `handleChat` now calls `redacted := loop.RedactText(d.deps.Redactor, final)` and stores `redacted` in `hist`, matching the channel path's journaled redaction and preventing secret re-transmission on subsequent turns. **CLOSED**.

5. **[agy L1 / codex L3] Rune-Safe Truncation in `clip()` ([internal/app/daemon/daemon.go:382-390](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L382-L390))**:
   - `clip` converts strings to `[]rune` and slices at rune count `r[:entryCap-1] + "…"`, ensuring valid UTF-8 and capping entries to exactly 1500 Unicode runes maximum without cutting multi-byte sequences. **CLOSED**.

6. **[agy L3 / codex L4 / kilo F2] Stale Comments & Dead Constants ([internal/app/daemon/daemon.go:295-303](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L295-L303))**:
   - Docstring was rewritten to accurately describe the completed-pair role message architecture; dead `entryCap` declaration removed. **CLOSED**.

---

## Two-Axis Review

### Standards

Evaluated against [AGENTS.md](file:///home/matej/HARNESS/nexus/AGENTS.md), [ARCHITECTURE-ESSENTIALS.md](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), and Fowler baseline smells.

- **Documented Standards Compliance**: **PASS**. Trust boundaries, fail-closed guards, secret redaction, single binary constraints, and language directives conform to repository standards.
- **Baseline Code Smells (Judgement Calls)**:
  1. *Duplicated Code / Data Clumps* ([daemon.go:188, 305](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L188)): `histPair` and `pair` both declare `struct{ user, final string }` in `daemon.go`.
  2. *Magic Number* ([daemon.go:209-211, 304](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L209-L211)): Window size `12` is hardcoded in `handleChat` while declared as `const historyMaxPairs = 12` in `conversationHistory`.
  3. *Primitive Obsession / Parsing Indirection* ([daemon.go:213-222](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L213-L222)): `handleChat` formats integer slice indices as strings (`fmt.Sprintf("%d", i)`) to parse them back via `fmt.Sscanf` in the `historyBlocks` callback.

---

### Spec

Evaluated against PRD §4/§6, Annex A Contracts, and multi-turn conversation specifications.

- **Missing / Partial Requirements**: **None**. All requirements across daemon history aggregation, planner role message conversion, identity isolation, secret redaction, and Croatian system directives are fulfilled.
- **Scope Creep**: **None**.
- **Implementation Soundness**: **PASS**.

---

## Empirical RED Ablation Probes

All 5 detectors were empirically verified in an isolated on-disk workspace:

| Detector | Mutation / Ablation | Observed RED Result |
| :--- | :--- | :--- |
| `TestHistoryBlocksBecomeRoleMessages` | Remove `Producer`/`Trust`/`SourceURI` gate in `splitHistory` | `FAIL: role order [system user assistant assistant user], want [system user assistant user]` |
| `TestHistoryBlocksBecomeRoleMessages` | Drop `splitHistory` (pass raw blocks to assembler) | `FAIL: role order [system user], want [system user assistant user]` |
| `TestHistoryBlocksBecomeRoleMessages` | Remove Croatian language directive from `systemPrompt` | `FAIL: system prompt lost the language directive` |
| `TestChannelTurnCarriesConversationHistory` | Disable `conversationHistory` in `RunChannelTurn` | `FAIL: turn 2 does not see turn 1's user message` |
| `TestChatSessionCarriesHistory` | Disable `hist` accumulation in `handleChat` | `FAIL: second turn does not carry the session history` |

---

## Top-3 Weakest Points

1. **[internal/app/daemon/daemon_test.go:608-687](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L608-L687)**: Tests verify 2-turn history propagation and cross-identity separation, but omit boundary tests for the 12-pair sliding window eviction (>12 pairs) and the 1500-rune truncation boundary.
2. **[internal/app/daemon/daemon.go:213-222](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L213-L222)**: `handleChat` stringifies array indices (`fmt.Sprintf("%d", i)`) and parses them back (`fmt.Sscanf`) to conform to `historyBlocks`'s string-based lookup signature.
3. **[internal/llm/planner/planner.go:57](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L57)**: `strings.HasPrefix(b.SourceURI, "nexus://")` uses string prefix matching rather than URL scheme parsing.

---

VERDICT: PASS

# QA Verification Report: Conversation History Slice (Round 4)

**Scope**: Commit `ae44a58` on branch `slice/p0-conv-history` (HEAD `ae44a58`)  
**Reviewer**: `agy` (Antigravity QA / Reviewer)  
**Date**: 2026-09-07  
**Repository**: `/home/matej/HARNESS/nexus`  
**Base**: `53353d1` (`origin/main`)  

---

## Executive Summary

Commit `ae44a58` resolves both round-3 review findings (kilo F1 / codex #2 rune-clip fixture & exact boundary lock, and kilo F2 / codex #1 session history bounding detector). The test fixture for rune clipping in `TestConversationHistoryRules` has been strengthened to an offset byte plus 1,500 4-byte runes (`"a" + strings.Repeat("🙂", 1500)`), ensuring any byte-based slicing cuts mid-rune and is caught by UTF-8 validation, while explicit assertions lock the exact 1,500-rune budget ending in `"…"`. In addition, `TestSessionHistoryBounded` now drives 14 chat turns through the real daemon UDS socket, asserting the exact 12-pair retained window and verifying FIFO eviction of the oldest session message.

Full clean-export verification (`CGO_ENABLED=0 go test -count=1 ./...`) passes across all packages (exit 0). Controlled mutation probes executed at the reviewer UID confirmed all 6 committed detectors turn **RED** under defect injection.

---

## Verification of Round-3 Finding Closures

1. **[kilo F1 / codex #2] Rune-Clip Fixture and Exact Budget Assertion ([internal/app/daemon/daemon_test.go:736-740, 772-788](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L736-L740))**:
   - **Mechanism**: The fixture was updated from `strings.Repeat("š", 1600)` (which had even 2-byte runes landing exactly on the 1,500-byte boundary) to `"a" + strings.Repeat("🙂", 1500)`. Because `"a"` is 1 byte and `"🙂"` is 4 bytes, byte offset 1,500 lands inside the 375th emoji (`'a'` + 374 $\times$ 4 = 1,497 bytes $\rightarrow$ bytes 1,498–1,501), splitting the UTF-8 sequence `\xf0\x9f\x99\x82` after 3 bytes.
   - **Exact Cap Assertion**: The test asserts `utf8.ValidString(joined)`, verifies the presence of the clipped entry, asserts `len([]rune(clipped)) == 1500`, and verifies `strings.HasSuffix(clipped, "…")`.
   - **RED Proof**:
     - Reverting `clip` to byte slicing (`s[:entryCap] + "…"`) yields `FAIL: clip split a UTF-8 rune`.
     - Drifting `entryCap` from 1,500 to 1,599 yields `FAIL: clip budget drifted: 1501 runes, want exactly 1500`.
   - **Status**: **CLOSED**.

2. **[kilo F2 / codex #1] Interactive Session History Bounding Detector ([internal/app/daemon/daemon_test.go:838-870](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon_test.go#L838-L870))**:
   - **Mechanism**: `TestSessionHistoryBounded` spins up `testDaemon`, connects via UDS socket, and sends 14 consecutive chat messages (`session-msg-1` through `session-msg-14`). It inspects the planner's captured prompt blocks on the 14th turn, asserting:
     - Exactly `historyMaxPairs` (12) `history_user` blocks are passed as prior context.
     - The oldest message (`session-msg-1`) is evicted (`strings.Contains(joined, "session-msg-1\n") == false`).
     - The newest prior message (`session-msg-13`) is retained.
   - **RED Proof**:
     - Removing the in-place truncation in `handleChat` (`daemon.go:225-228`) yields `FAIL: daemon_test.go:862: session history carries 13 prior pairs, want exactly 12`.
   - **Status**: **CLOSED**.

---

## Two-Axis Review

### Standards Axis

Evaluated against [AGENTS.md](file:///home/matej/HARNESS/nexus/AGENTS.md), [ARCHITECTURE-ESSENTIALS.md](file:///home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md), and standard Go best practices:

- **Documented Standards Compliance**: **PASS**.
  - All tests execute in a clean git export on disk (no tmpfs dependencies).
  - No `map[string]any` introduced into kernel APIs.
  - Fail-closed security boundaries preserved: `splitHistory` enforces daemon producer, `nexus://` source URI, and `TrustUser` before converting blocks to role messages.
  - Monotone trust and redaction rules strictly maintained across both channel and interactive session loops.
- **Baseline Smells (Judgement Calls)**:
  1. *Primitive Obsession* ([internal/llm/planner/planner.go:57](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L57)): URI scheme check uses `strings.HasPrefix(b.SourceURI, "nexus://")` rather than parsed URI scheme validation.
  2. *Data Clump / Struct Duplication* ([internal/app/daemon/daemon.go:290-293, 363](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L290-L293)): Local `pair` struct (`user, final string; completed bool`) duplicates fields of package-level `histPair` (`user, final string`).
  3. *Windowing Idiom Divergence* ([internal/app/daemon/daemon.go:225-228, 354-356](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L225-L228)): Chat session uses in-place memory reuse `append(hist[:0], ...)` while channel history re-slices `entries = entries[...]`.

### Spec Axis

Evaluated against PRD §4/§6, Annex A contracts, and multi-turn history requirements:

- **Requirement Coverage**: **PASS**.
  - Multi-turn history carries up to 12 pairs of alternating user/assistant turns.
  - Channel history folds from durable journal admissions and success events; interactive chat folds from in-memory session history.
  - Secret redaction is verified end-to-end on session history re-feeding.
  - Both round-3 detector deficiencies are resolved with committed, revert-proof regression tests.
- **Scope Creep**: **None**.

---

## Empirical RED Ablation Table

All 6 committed detectors were validated via controlled defect injection at the reviewer UID in a clean on-disk export:

| Detector | Mutation / Ablation | Target File & Line | Observed Failure Output | Status |
| :--- | :--- | :--- | :--- | :--- |
| `TestConversationHistoryRules` | Revert `clip` to byte-slicing `s[:entryCap] + "…"` | [daemon.go:376-380](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L376-L380) | `FAIL: daemon_test.go:773: clip split a UTF-8 rune` | **RED** |
| `TestConversationHistoryRules` | Drift `entryCap` from 1500 to 1599 | [daemon.go:377](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L377) | `FAIL: daemon_test.go:785: clip budget drifted: 1501 runes, want exactly 1500` | **RED** |
| `TestSessionHistoryBounded` | Remove in-place slice truncation in `handleChat` | [daemon.go:225-228](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L225-L228) | `FAIL: daemon_test.go:862: session history carries 13 prior pairs, want exactly 12` | **RED** |
| `TestHistoryBlocksBecomeRoleMessages` | Remove `b.Trust == contracts.TrustUser` check in `splitHistory` | [planner.go:56](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L56) | `FAIL: planner_test.go:77: role order [system user assistant assistant user]` | **RED** |
| `TestConversationHistoryRules` | Make `pr.completed = true` conditional on `p.Final != ""` | [daemon.go:334](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L334) | `FAIL: daemon_test.go:769: empty-final turn misclassified as incomplete` | **RED** |
| `TestSessionHistoryStoresRedactedFinal` | Store unredacted `final` in session `d.chatHist[id]` | [daemon.go:217](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L217) | `FAIL: daemon_test.go:829: raw secret re-fed to the provider via session history: "the token is sk-VERYSECRET"` | **RED** |

---

## Top-3 Weakest Points

1. **[internal/app/daemon/daemon.go:395-408](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L395-L408)**: Completed turns with an empty final string (`final == ""`) generate a `history_assistant` block with empty content, producing an empty assistant role message in downstream provider payloads. While standard in OpenAI/Claude-style chat schemas, certain strict upstream LLM APIs reject empty assistant turns.
2. **[internal/app/daemon/daemon.go:225-228, 354-356](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L225-L228)**: Inconsistent slicing idioms between interactive sessions (`append(hist[:0], hist[len(hist)-historyMaxPairs:]...)`) and channel turns (`entries = entries[len(entries)-historyMaxPairs:]`). Both achieve the 12-pair window, but maintaining two distinct slice-bounding patterns increases cognitive overhead.
3. **[internal/llm/planner/planner.go:57](file:///home/matej/HARNESS/nexus/internal/llm/planner/planner.go#L57)**: `strings.HasPrefix(b.SourceURI, "nexus://")` relies on string prefix comparison rather than structured URI parsing (e.g. `url.Parse`), allowing arbitrary malformed URIs as long as they begin with the scheme prefix.

---

## Proof Ceiling

This review establishes commit-local Go behavior, multi-turn history invariants, secret redaction boundaries, and detector sensitivity on the Linux host. It does not establish live Telegram network delivery or live LLM provider multi-turn token consumption beyond the scripted provider seam.

---

VERDICT: PASS

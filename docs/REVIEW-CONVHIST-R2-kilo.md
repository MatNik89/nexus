# REVIEW-CONVHIST-R2-kilo — conversation-history round-2 closure (commit 22dc6c9)

**Scope:** `slice/p0-conv-history`, HEAD `22dc6c9` — folds all round-1 findings
(1 HIGH + 1 MED + 5 LOW). Read-only QA verification of each finding's closure and
RED-capability, plus a regression hunt in the changed region.

**Method:** reviewed the round-2 diff and all three round-1 review docs
(`REVIEW-CONVHIST-{codex,agy,kilo}.md`); traced the fixed paths (`splitHistory`,
`conversationHistory`, `historyBlocks`, session `handle`); ran the named regressions,
full `go test ./...`, and ablations in a clean detached worktree at `22dc6c9`
(`/home/matej/qa-convhist-r2`), at the reviewer uid.

## Functional closure (all correct)

| Round-1 finding | Fix | Status |
|---|---|---|
| codex HIGH — spoofed `history_*` Kind bypasses the fence | `splitHistory` accepts only `Kind∈{history_user,history_assistant}` **AND** `Producer=="daemon"` **AND** `Trust==TrustUser` **AND** `nexus://` source (`planner.go:54-57`); anything else flows through the assembler | closed |
| codex MED — cap over admissions, not completed pairs | `conversationHistory` filters `pairs[id].final != ""` before the 12-pair cut (`daemon.go:358-367`) | closed |
| kilo F1 — prefix over-match on the B3 boundary | `turn.succeeded` now uses exact membership in the admitted set (no `HasPrefix`; `strings` import removed) | closed |
| kilo F3 — session re-feeds unredacted final | session stores `loop.RedactText(redactor, final)` (`daemon.go:239`) | closed |
| codex L3/agy L1 — UTF-8 byte-splitting clip | `clip` counts runes, ellipsis inside the budget (`historyBlocks`) | closed |
| codex L4/kilo F2 — stale comment + dead const | comment rewritten; `entryCap` removed | closed |

All six fixes are functionally correct on inspection, `go build ./...` and
`go vet` are clean, and the full `go test ./...` is green (no regression).

## RED-capability (ablations at reviewer uid)

| Probe | Result |
|---|---|
| Remove the whole splitHistory gate | RED — spoofed block becomes an `assistant` role (`role order [system user assistant assistant user]`) |
| **Remove ONLY the `Trust == TrustUser` leg** | my probe RED; the **committed** spoof test stays **GREEN** (see F1) |
| `TestHistoryBlocksBecomeRoleMessages` / `TestChannelTurnCarriesConversationHistory` / `TestChatSessionCarriesHistory` | GREEN |
| `go test ./...` | all packages GREEN |

## Findings

**F1 [LOW] — the trust leg of the gate is not locked by the regression.**
The committed spoof case (`planner_test.go:377`) uses `Kind=history_assistant`,
`Producer="exec"`, `Source="tool://x"`, `Trust=TrustUser` — it exercises only the
producer and source legs. The trust leg (`b.Trust == contracts.TrustUser`,
`planner.go:56`) is the one that actually closes the HIGH: `observeGuard`
(`loop.go:149-152`) reserves only `user/system/repl/loop`, so a tool can emit
`Producer="daemon"`, `Source="nexus://…"`, `Kind="history_assistant"` with
`Trust=TrustUntrustedExternal`, which passes the loop and is stopped **only** by the
trust leg. Verified: (a) with the committed gate such a block is fenced (probe GREEN);
(b) ablating only the trust check turns my probe RED while the committed spoof test
stays GREEN. The critical leg is therefore unguarded against a future regression.

**F2 [LOW] — the MED, rune-clip and redaction fixes carry no red-capable detector.**
The completed-pairs filter (`daemon.go:358-367`), the rune-safe clip, and the session
redaction are behavioral fixes with no regression test (no new daemon test; the codex
MED probe was disposable, not committed). Ablating any of them leaves the full suite
GREEN. This is exactly agy round-1 L5 ("missing FIFO cap / boundary assertions"), still
unresolved, and it violates the repo's "every behavioral change leaves one runnable
check" discipline.

**F3 [LOW] — agy round-1 L3 is only half-folded.** The dead `entryCap` is gone, but the
session path still hardcodes `12` (`daemon.go:209`) where the channel path uses
`const historyMaxPairs = 12`, and still round-trips indices through strings
(`fmt.Sprintf("%d", i)` at `daemon.go:215`, `fmt.Sscanf` back at `daemon.go:220`).

## Confidence

Provable from code + execution: all six fixes are present and correct, the whole-gate
ablation is RED, and the full suite is green. The trust-leg gap (F1) is demonstrated by
two ablations; F2/F3 are confirmed by inspection against the committed tests and the
round-1 review docs. These are LOW severity, but under the all-severity rule they are
unresolved.

VERDICT: FAIL

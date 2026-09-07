# REVIEW-CONVHIST-R3-kilo — conversation-history round-3 closure (commit 017e051)

**Scope:** `slice/p0-conv-history`, HEAD `017e051` — folds all round-2 findings.
Read-only QA verification of each finding's closure and RED-capability, plus a
regression hunt.

**Method:** reviewed the round-3 diff and the three round-2 review docs; traced the
fixed paths (`splitHistory`, `conversationHistory`, `historyBlocks`, session `handle`);
ran the named regressions, full `go test ./...`, and ablations in a clean detached
worktree at `017e051` (`/home/matej/qa-convhist-r3`), at the reviewer uid.

## Functional closure (all correct)

| Round-2 finding | Fix | Status |
|---|---|---|
| codex #1 — empty final misclassified as incomplete | explicit `completed bool` owned by the `turn.succeeded` EVENT (`daemon.go:334`) | closed |
| codex #2 / kilo F2 — no committed daemon detectors | `TestConversationHistoryRules` + `TestSessionHistoryStoresRedactedFinal` | closed (see F1/F2) |
| codex #3 — stale/misattached comments | RunChannelTurn comment fixed; `completedTurnFinal` comment moved to its function | closed |
| codex #4 — session history unbounded | `hist` truncated in place at `historyMaxPairs` (`daemon.go:225-228`) | closed (untested) |
| codex #5 / kilo F3 — hardcoded `12` + string round-trip | shared `const historyMaxPairs`; `historyBlocks` takes `[]histPair` | closed |
| kilo F1 — trust leg of the gate not locked | `mkTrust` UNTRUSTED + daemon + nexus:// spoof case in the role test | closed (RED) |

All fixes are functionally correct on inspection, and `go test ./...` is green
(exit 0, no regression).

## RED-capability (ablations at reviewer uid)

| Ablation | Result |
|---|---|
| Remove `&& b.Trust == contracts.TrustUser` (trust leg) | RED — `role order [system user assistant assistant user]` |
| Gate `completed` on non-empty final | RED — `empty-final turn misclassified as incomplete` |
| Store raw (unredacted) session final | RED — `raw secret re-fed ... sk-VERYSECRET` |
| Cap **before** the completed filter | RED — `cap after completed filter broken: 11 user entries, want 12` |
| **Restore byte-slicing clip** | **GREEN — detector not RED-capable (see F1)** |
| Full `go test ./...` | all packages GREEN |

## Findings

**F1 [LOW] — the rune-clip regression is not RED-capable.**
`TestConversationHistoryRules` feeds `strings.Repeat("š", 1600)` (`daemon_test.go:737`)
and asserts `utf8.ValidString(joined) && Count(joined,"š") < 1600` (`daemon_test.go:769`).
"š" is a 2-byte rune and the byte cap is 1500 (even), so `s[:1500]` lands exactly on a
character boundary (750 "š") and yields valid UTF-8 — restoring the byte-slice clip
leaves the test GREEN (verified). The committed detector therefore does not lock the
rune-safety fix at `daemon.go:376-380`; it needs an offset/4-byte fixture (e.g.
`"a" + strings.Repeat("🙂", 1500)`, as the original codex probe used) so the 1500-byte
cut falls mid-rune.

**F2 [LOW] — the session-history bounding fix has no regression test.**
`daemon.go:225-228` now truncates `hist` in place at `historyMaxPairs`, but no test
drives a >12-turn session and asserts the retained window. The redaction seam is locked
(`TestSessionHistoryStoresRedactedFinal`) but the bounding (codex #4) is not.

## Note (not a finding)

The exact-membership B3 rule (`turn.succeeded` gated by exact `pairs[TurnID]`) is a
source-contract operator with no behaviorally-distinguishable leak (the exact map
lookup already prevented it); the committed "prefix identity" case exercises the exact
inbound filter, not the `turn.succeeded` membership. The operator is present and
correct, so this is a coverage nuance, not a defect.

## Confidence

Provable from code + execution: trust-leg, completed-bit, redact-seam and cap-order
ablations all turn RED; the full suite is green. F1 is demonstrated by the byte-slice
ablation passing; F2 is confirmed by inspection (no >12-turn session test exists). Both
are LOW, but under the all-severity rule they are unresolved.

VERDICT: FAIL

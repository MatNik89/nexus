# REVIEW-CONVHIST-R4-kilo — conversation-history round-4 closure (commit ae44a58)

**Scope:** `slice/p0-conv-history`, HEAD `ae44a58` — closes the two convergent round-3
findings (rune-clip detector, session-bound detector). Read-only QA verification of
closure and RED-capability, plus a regression hunt.

**Method:** reviewed the round-4 diff; traced the sharpened fixtures and the new
`TestSessionHistoryBounded`; ran the detectors and three ablations in a clean detached
worktree at `ae44a58` (`/home/matej/qa-convhist-r4`), at the reviewer uid; full
`go test ./...` for regression.

## Closure of round-3 findings

**F1 [LOW] — rune-clip detector not RED-capable. — CLOSED.**
The fixture is now `"a" + strings.Repeat("🙂", 1500)` (`daemon_test.go:737`) — a 1-byte
offset plus 4-byte runes, so any byte-based 1500-cut lands mid-rune. The assertion now
locks the exact contract: `utf8.ValidString`, `len([]rune(clipped)) == 1500`, and the
trailing ellipsis (`daemon_test.go:769-787`).

**F2 [LOW] — session-history bounding untested. — CLOSED.**
`TestSessionHistoryBounded` drives 14 session turns and asserts the last planner call
carries exactly `historyMaxPairs` prior `history_user` blocks, never the oldest, and
does include the newest prior pair (`daemon_test.go:840-870`).

Production code (`daemon.go`, `planner.go`, `planner_test.go`) is byte-identical to
round 3 — this commit is test-only.

## RED-capability (ablations at reviewer uid)

| Ablation | Result |
|---|---|
| Restore byte-slicing `clip` | RED — `clip split a UTF-8 rune` |
| Drift `entryCap` 1500 → 1599 | RED — `clip budget drifted: 1501 runes, want exactly 1500` |
| Remove the session truncation | RED — `session history carries 13 prior pairs, want exactly 12` |
| `TestConversationHistoryRules` / `TestSessionHistoryBounded` / `TestSessionHistoryStoresRedactedFinal` | GREEN |
| `go test ./...` | all packages GREEN (exit 0) |

## Note (not a finding)

The committed `REVIEW-CONVHIST-R3-agy.md` "weakest point #1" claims `historyBlocks`
emits an empty `history_assistant` block for empty finals; the code guards `if final !=
""` and correctly skips it. This is a factual error in a review artifact, not a
production defect. (The empty-final completed pair does produce a `history_user` with no
following assistant — a rare, chosen-design consequence of "empty final = completed",
not a regression.)

## Confidence

Both round-3 findings are closed and their detectors are RED-capable (three independent
ablations). Production code is unchanged from the RED-proven round 3, and the full suite
is green.

VERDICT: PASS

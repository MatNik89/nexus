# Conversation-history slice — Codex QA verification round 4

## Scope and artifact binding

- Repository: `/home/matej/HARNESS/nexus`
- Reviewed branch/revision: `slice/p0-conv-history` at `ae44a5825b1f643eaecb2316aab2dc9c907cf4d6`
- Parent: `017e05191656dfe17944a18fd75b25b6bb45faf4`
- Reviewer UID: `1000`
- Mode: solo, read-only production review. No production source, index, branch, commit, or live service was modified.
- Execution artifact: clean `git archive` export at `/home/matej/HARNESS/qa-convhist-r4-codex.ae44a58` on ext4 (`/dev/mmcblk0p2`), not `/tmp` (tmpfs). Controlled mutations were confined to that disposable export.
- Scoped immutable hashes after every mutation was restored:
  - `internal/app/daemon/daemon.go`: `8a1c63283424671fc0218b1ff30ccd1e9f2593dd73fe8a4fa578bfdb002b1eab`
  - `internal/app/daemon/daemon_test.go`: `b844391a817524ceadd522e158d65ea0306d1442653fa06a5cbb516c64da5519`
  - `internal/llm/planner/planner.go`: `1520ae72333558483aab510a20309e71fdf2608dadb77354c64e780634f10be6`
  - `internal/llm/planner/planner_test.go`: `57b02a099617e0f40665b7e58b2f93f86f1bcd2635cfcd6f4a871bc65ca79ff0`

## Result

Both convergent round-3 findings are closed. The committed detectors exercise the real daemon/UDS and journal-fold paths, pass at the immutable revision, and turn RED under each requested one-variable regression. The same-class producer/consumer sweep and full repository suite found no unresolved finding of any severity.

## Closure and RED-capability

| Round-3 finding | Closure at `ae44a58` | Controlled negative evidence |
|---|---|---|
| Rune clipping fixture/oracle was not boundary-exact | **CLOSED.** `TestConversationHistoryRules` now supplies `"a" + strings.Repeat("🙂", 1500)`, selects the resulting history-user block, requires valid UTF-8, requires exactly 1,500 output runes, and requires a terminal ellipsis (`internal/app/daemon/daemon_test.go:736-789`). This exercises the shared production `historyBlocks` rune clip (`internal/app/daemon/daemon.go:375-383`). | Replacing rune slicing with byte slicing made the test RED: `clip split a UTF-8 rune`. Changing only `entryCap` from `1500` to `1599` made it RED: `clip budget drifted: 1501 runes, want exactly 1500`. |
| Session window had no committed detector | **CLOSED.** `TestSessionHistoryBounded` sends 14 messages through the real UDS client, inspects the fourteenth planner call, requires exactly `historyMaxPairs` prior user entries, excludes `session-msg-1`, and includes newest prior entry `session-msg-13` (`internal/app/daemon/daemon_test.go:838-870`). This reaches the in-place session truncation at `internal/app/daemon/daemon.go:224-229`. | Removing only the session truncation made the test RED: `session history carries 13 prior pairs, want exactly 12`. |

The session assertion's use of `"session-msg-1\n"` avoids a substring false positive against `session-msg-10` through `session-msg-14`. The test observes the planner's captured input rather than recomputing the production window.

## Regression hunt

No findings.

- The class sweep found two producers only: interactive session history at `internal/app/daemon/daemon.go:188-229` and channel journal-fold history at `internal/app/daemon/daemon.go:284-357`. Both use the single `historyMaxPairs` owner and the shared `historyBlocks` transform.
- The sole provider-role consumer remains `splitHistory` at `internal/llm/planner/planner.go:42-75`; its daemon/source/trust gate is unchanged by this commit.
- Commit `ae44a58` changes only tests and carries forward round-3 review artifacts; it does not alter production behavior or add a dependency, state, API, or abstraction.
- `git diff --check` reports Markdown trailing spaces in `docs/REVIEW-CONVHIST-R3-agy.md:3-7`; those are the document's deliberate Markdown hard-break markers, not a runtime or maintainability defect in the reviewed closure.

## Verification evidence

Focused immutable-export tests:

```text
TMPDIR=/run/user/1000 CGO_ENABLED=0 /home/matej/.local/go/bin/go test \
  ./internal/app/daemon ./internal/llm/planner \
  -run '^Test(ConversationHistoryRules|SessionHistoryBounded|SessionHistoryStoresRedactedFinal|ChannelTurnCarriesConversationHistory|ChatSessionCarriesHistory|HistoryBlocksBecomeRoleMessages)$' \
  -count=1 -v
PASS — daemon 5/5; planner 1/1; exit 0
```

Affected-package regression gate after restoration:

```text
TMPDIR=/run/user/1000 CGO_ENABLED=0 /home/matej/.local/go/bin/go test \
  ./internal/app/daemon ./internal/llm/planner -count=1
PASS — both packages; exit 0
```

Full immutable-export suite:

```text
TMPDIR=/run/user/1000 CGO_ENABLED=0 /home/matej/.local/go/bin/go test \
  -count=1 -timeout=900s ./...
PASS — all discovered packages; exit 0
```

Static check:

```text
TMPDIR=/run/user/1000 CGO_ENABLED=0 /home/matej/.local/go/bin/go vet \
  ./internal/app/daemon ./internal/llm/planner
PASS — exit 0
```

Race instrumentation was attempted but is unavailable on this host: Go's race runtime stopped before test execution with `ThreadSanitizer: unsupported VMA range` (`Found 47 - Supported 48`). This is not evidence against the commit and is not promoted to a finding; the ordinary tests and static checks above ran at UID 1000.

## Adversarial conclusion

The strongest counterargument is that `TestSessionHistoryBounded` counts prior user blocks rather than separately counting assistant blocks. That does not leave the reported bound unproved: the bounded state is a `[]histPair`, every successful scripted turn contributes one pair, the production builder deterministically visits each pair, `TestChatSessionCarriesHistory` independently proves the assistant half reaches the next planner call, and removing the truncation alone turns the new detector RED at the exact operational seam.

Top-3 weakest points, none rising to a finding:

1. Race-detector evidence is unavailable because the host's VMA layout is unsupported by ThreadSanitizer; upgrade when the same revision can run under a supported race-runtime environment.
2. The session-bound detector uses the shared `historyMaxPairs` constant as its oracle, so it proves enforcement of the owned limit rather than independently fixing the product value at 12. This matches the requested contract; a future requirement that 12 itself be immutable would need a literal boundary oracle.
3. Deterministic scripted planners prove context selection and transport through the local daemon boundary, not a live provider's handling of the resulting role messages.

Topknot simplification pass: Lean already. The closure adds assertions at existing real seams and no production machinery.

Weakest link: absence of runnable race instrumentation on this host.

Proof ceiling: this review establishes commit-local behavior and detector sensitivity on the current Linux host. It does not establish live-provider compatibility, deployment state, or release readiness.

Skill update: none. The existing NEXUS audit procedure already required exact boundary and one-variable revert proofs; it directly caught the round-3 misses and needs no new reusable rule.

VERDICT: PASS

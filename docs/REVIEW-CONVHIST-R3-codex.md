# Conversation-history slice — Codex QA verification round 3

## Scope and artifact binding

- Repository: `/home/matej/HARNESS/nexus`
- Reviewed branch/revision: `slice/p0-conv-history` at `017e05191656dfe17944a18fd75b25b6bb45faf4`
- Parent: `22dc6c9d4ce99bda7d03ac2911ed0906f9576a8c`
- Mode: solo, read-only production review. No production source, index, branch, commit, or live service was modified.
- Execution artifact: two clean `git archive` exports under `/home/matej/HARNESS` on ext4 (`/dev/mmcblk0p2`), not `/tmp`; both exports and the short test-temp root were verified deleted after review.
- Reviewer UID: `1000`.
- Scoped immutable hashes:
  - `internal/app/daemon/daemon.go`: `8a1c63283424671fc0218b1ff30ccd1e9f2593dd73fe8a4fa578bfdb002b1eab`
  - `internal/app/daemon/daemon_test.go`: `900f6227e7d139db8e656c80c2d8c35b84155189d3ab837a893597ccddda8021`
  - `internal/llm/planner/planner.go`: `1520ae72333558483aab510a20309e71fdf2608dadb77354c64e780634f10be6`
  - `internal/llm/planner/planner_test.go`: `57b02a099617e0f40665b7e58b2f93f86f1bcd2635cfcd6f4a871bc65ca79ff0`

## Result

The implementation closes the reported functional defects, and the full repository suite is GREEN from the immutable export. The requested all-severity policy nevertheless requires FAIL because two LOW detector defects remain: the new interactive-session bound has no committed RED-capable detector, and the committed rune oracle accepts a wrong 1,599-rune cap.

## Findings

### 1. The interactive-session bound has no committed RED-capable detector

[CONCRETE][SEV: LOW] `internal/app/daemon/daemon_test.go:668` - `TestChatSessionCarriesHistory` sends only two messages, so it cannot observe the `historyMaxPairs` bound added at `internal/app/daemon/daemon.go:225-229`. No other committed test exercises more than 12 turns through `handleChat`.

- Input: remove only the `len(hist) > historyMaxPairs` truncation while retaining every committed test.
- Expected: a committed regression test fails because a long-lived session re-feeds or retains more than 12 prior pairs.
- Actual: `TMPDIR=/run/user/1000 go test ./internal/app/daemon -count=1` remained GREEN (`ok`, exit 0); the four named conversation-history daemon tests also remained GREEN.
- Root cause: the channel-history test covers its own journal fold and cap, but the separate in-memory session producer is exercised only below the threshold.

PROBE: controlled deletion in the disposable mutation export, followed by the complete daemon package test at UID 1000; the mutation was then restored and scoped files were byte-compared with the immutable baseline.

FIX: extend the real UDS session test past 12 successful turns and assert that the next planner call contains exactly the last 12 pairs and excludes the oldest pair; prove it RED with only the session truncation removed.

### 2. The rune detector does not lock the exact 1,500-rune boundary

[CONCRETE][SEV: LOW] `internal/app/daemon/daemon_test.go:769` - the assertion checks only valid UTF-8 and fewer than 1,600 copies of `š`. It does not assert the documented 1,500-rune output, the ellipsis inside that budget, or the 1,500/1,501 threshold.

- Input: change only `entryCap` at `internal/app/daemon/daemon.go:376` from `1500` to `1599`.
- Expected: the committed detector fails because the history entry exceeds the owned cap.
- Actual: `TMPDIR=/home/matej/.r3t go test ./internal/app/daemon -run '^TestConversationHistoryRules$' -count=1 -v` remained GREEN (exit 0).
- Root cause: `strings.Count(joined, "š") >= 1600` proves that some clipping happened; it does not prove the cap value or boundary behavior.

PROBE: controlled constant mutation in the disposable export. A separate byte-slicing mutation did turn the same test RED with `valid=false`, so UTF-8 safety is detected while exact cap preservation is not.

FIX: identify the rune-heavy history block directly and assert exactly 1,500 runes including the final ellipsis; add 1,500-rune unchanged and 1,501-rune clipped boundary cases.

## Round-2 closure matrix

| Claimed round-2 closure | Status | Evidence |
|---|---|---|
| Completion owned by `turn.succeeded`; empty final counts | CLOSED | `pair.completed` is set only by a valid `turn.succeeded`; replacing the completed predicate with `final != ""` turned `TestConversationHistoryRules` RED on `make it empty`. |
| Completed-only filter and cap after filter | CLOSED | Moving the cap before the filter turned the committed test RED: `11 user entries, want 12`. |
| Exact B3 membership with prefix identity | CLOSED | Weakening the admission identity comparison to prefix matching turned the committed test RED with `other identity secret`. The success leg also requires exact membership in the admitted map. |
| Rune-safe clipping | PARTIAL | Implementation is rune-safe and a byte-slicing mutation is RED, but Finding 2 proves the committed oracle does not lock the 1,500-rune contract. |
| Session final redacted through the real session path | CLOSED | Replacing the stored redacted final with the raw final turned `TestSessionHistoryStoresRedactedFinal` RED with `sk-VERYSECRET`. |
| Trust-leg spoof (`daemon` + `nexus://` + `UNTRUSTED`) | CLOSED | Removing only `b.Trust == TrustUser` turned `TestHistoryBlocksBecomeRoleMessages` RED with an extra assistant role. Static sibling tracing confirms tool observations may claim producer `daemon`, while `observeGuard` keeps their trust at tool/untrusted, making this trust leg decisive. |
| Session history bounded at shared `historyMaxPairs` | PARTIAL | The production truncation exists and both producers use the shared constant, but Finding 1 proves the session bound is not RED-capable in the committed suite. |
| `historyBlocks([]histPair)` and removed string index round-trip | CLOSED | Both session and channel paths pass typed pairs directly; no index formatting/parsing remains. |
| Both stale comments | CLOSED | `RunChannelTurn` now says chronological role blocks, and `completedTurnFinal` owns its correctly attached comment. |

## Verification evidence

Baseline focused tests from the clean export:

```text
TMPDIR=/home/matej/.r3t go test ./internal/app/daemon ./internal/llm/planner \
  -run 'Test(ConversationHistoryRules|SessionHistoryStoresRedactedFinal|HistoryBlocksBecomeRoleMessages|ChannelTurnCarriesConversationHistory|ChatSessionCarriesHistory)$' \
  -count=1 -v
PASS — daemon 4/4; planner 1/1; exit 0
```

Full immutable-export suite:

```text
CGO_ENABLED=0 TMPDIR=/run/user/1000 go test -count=1 ./...
PASS — all discovered packages; exit 0
```

The earlier full-suite attempt with ext4-backed `TMPDIR=/home/matej/.r3t` failed because the sandbox contract permits disposable RW grants only under `/run/user/1000`; the clean export itself remained on ext4. The required-root rerun above supersedes that environmental attempt.

Static analysis:

```text
go vet ./internal/app/daemon ./internal/llm/planner
PASS — exit 0
```

Controlled RED results at UID 1000:

- completed bit replaced by `final != ""` -> RED: empty-final turn misclassified;
- cap moved before completed filtering -> RED: 11 user entries, want 12;
- exact identity weakened to prefix matching -> RED: cross-identity secret present;
- rune slicing replaced by byte slicing -> RED: invalid UTF-8;
- stored session final changed from redacted to raw -> RED: marker secret present;
- planner trust predicate removed -> RED: extra assistant role.

Controlled false-GREEN results:

- session bound removed -> complete daemon package GREEN (Finding 1);
- rune cap changed from 1,500 to 1,599 -> `TestConversationHistoryRules` GREEN (Finding 2).

Every disposable mutation was restored before comparison; all four scoped source/test hashes matched the immutable baseline before cleanup.

## Adversarial conclusion

The strongest counterargument is that the functional implementation is correct, all explicitly highlighted completion/redaction/trust ablations are RED, and the full suite is GREEN. That is insufficient under the repository rule that every behavioral change must leave a RED-capable committed detector. The session-bound change has none, and the rune-cap detector accepts a concrete wrong boundary.

Security result: no reachable trust-fence or session-redaction vulnerability was confirmed in the reviewed slice. The trust predicate is decisive on the reachable tool-observation metadata path, and the marking redactor test exercises the real UDS session path.

Topknot simplification pass: Lean already; the production change reuses one typed pair shape and one shared history-window constant without a new abstraction or dependency.

Weakest link: no live or paid provider was authorized or run; deterministic local fakes establish history selection, role conversion, redaction, and trust fencing, not external-provider compatibility.

Proof ceiling: this review establishes commit-local Go behavior and detector sensitivity on the current Linux host. It does not establish live-provider behavior, release readiness, or deployment state.

Skill update: none. The existing NEXUS audit rules already require a revert-proof detector for each fix and exact boundary checks; these misses require test changes in the candidate, not a new audit rule.

VERDICT: FAIL

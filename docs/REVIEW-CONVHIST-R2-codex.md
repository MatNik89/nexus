# Conversation-history slice — Codex QA verification round 2

## Scope and artifact binding

- Repository: /home/matej/HARNESS/nexus
- Reviewed branch/revision: slice/p0-conv-history at 22dc6c9d4ce99bda7d03ac2911ed0906f9576a8c
- Parent: 06cc6643a98818f9bbfdf12f8a4c8fbaf28a5b93
- Mode: solo, read-only review. No production file, commit, branch, index, or live service was modified.
- Execution artifact: clean git archive exports under /home/matej on ext4 (/dev/mmcblk0p2), not /tmp.
- Reviewer UID: 1000.
- Scoped production hashes:
  - internal/app/daemon/daemon.go: 72c9daa55ca14231d9131d07b67030de2c15a912bc3d96fa6e1f835e4d8a9b40
  - internal/llm/planner/planner.go: 1520ae72333558483aab510a20309e71fdf2608dadb77354c64e780634f10be6
  - internal/llm/planner/planner_test.go: f30c9a11bfd00a5b2ab61e584cbffe2bad63417136d27749582bda31fb26cab4

## Result

Five intended behavior changes work for their tested non-empty cases and were independently proven RED-capable under controlled ablations. The revision nevertheless retains unresolved LOW findings, including one behavioral defect in the claimed COMPLETED-pair rule. The requested zero-finding policy therefore requires FAIL.

## Findings

### 1. A succeeded turn with an empty final is misclassified as incomplete

[CONCRETE][SEV: LOW] internal/app/daemon/daemon.go:360 - completion is inferred from pairs[id].final != "", even though completion is owned by the presence of an exact turn.succeeded event. Empty provider content is accepted by internal/llm/planner/planner.go:304-311, and the loop journals it as turn.succeeded in internal/kernel/loop/loop.go:258-270.

- Input: an admitted channel turn whose planner returns an empty Final.
- Expected: because the durable state is turn.succeeded, the turn counts as a COMPLETED pair for the 12-pair window; alternatively, empty finals must be rejected before turn.succeeded.
- Actual: the real loop returned success and journaled turn.succeeded, but conversationHistory returned zero blocks.
- Root cause: the fold overloads the final string as both data and a completion boolean; historyBlocks also omits an empty assistant message at line 402.

PROBE: disposable TestReviewR2CompletedEmptyFinalStillCountsAsCompleted exercised admission → RunChannelTurn → journal replay → conversationHistory and failed with: turn.succeeded with an empty final is COMPLETED but produced 0 history blocks, want a user/assistant pair.

FIX: track an explicit completed bit from turn.succeeded and define empty-assistant handling, or reject empty provider finals before recording success.

### 2. The daemon-side fixes have no committed regression detectors

[CONCRETE][SEV: LOW] internal/app/daemon/daemon_test.go:608 - commit 22dc6c9 changes completed filtering/cap ordering, exact-membership selection, session-final redaction, and rune clipping, but changes no daemon test. The command git diff --name-only 22dc6c9^ 22dc6c9 -- '*_test.go' lists only internal/llm/planner/planner_test.go. The round-1 missing FIFO/cap/boundary-test finding is therefore not folded into the delivered suite.

PROBE: reviewer-only tests proved the production fixes can be locked and are RED-capable:

- removing completed-only filtering: RED, want 12 completed pairs/24 blocks after filtering, got 12;
- applying the cap before filtering: RED, got 0;
- restoring byte clipping: RED, clip split a UTF-8 rune;
- restoring raw session-final storage: RED, history contained secret-value;
- restoring prefix matching: the exact-set-membership source-contract detector turned RED.

All reviewer-only detectors were GREEN after restoration, and all scoped production hashes returned to the immutable export values above. Those detectors were deliberately disposable and are not present in the reviewed commit.

FIX: commit focused daemon regressions for completed-only/filter-before-cap behavior, rune count plus UTF-8 validity, redacted session history, and the exact-membership rule; demonstrate their controlled RED before the production fix.

### 3. The stale-comment finding is only partially closed

[CONCRETE][SEV: LOW] internal/app/daemon/daemon.go:265-267 - the remaining RunChannelTurn comment says history “rides in as one chronological block,” while conversationHistory returns separate history_user and history_assistant blocks. At lines 294-302, the conversationHistory documentation is also accidentally attached to a three-line completedTurnFinal comment even though completedTurnFinal is declared later at line 414.

PROBE: static comparison against historyBlocks at lines 372-411 and splitHistory at internal/llm/planner/planner.go:45-75.

FIX: make the RunChannelTurn comment say chronological role blocks and move the stray completedTurnFinal comment to its actual function.

### 4. Interactive-session storage is unbounded despite the 12-pair replay window

[CONCRETE][SEV: LOW] internal/app/daemon/daemon.go:188-239 - every successful message appends to hist for the full connection lifetime. Lines 208-212 select only the last 12 for replay but never discard older pairs, so a long-lived REPL session retains every full user/final string indefinitely.

PROBE: static lifecycle trace: hist is initialized once before the connection loop, appended once per success, and has no truncation or reset before the connection closes.

FIX: retain at most 12 pairs after append; the discarded entries are already unreachable by the model and have no other owner.

### 5. Round-1 history-window duplication and index conversion residue remains

[NIT][SEV: LOW] internal/app/daemon/daemon.go:208-221 - the REPL path still hardcodes 12 separately from historyMaxPairs at line 304, converts slice indexes to decimal strings, then parses them back with an unchecked fmt.Sscanf. This was part of the round-1 Agy code-smell finding and was not changed by 22dc6c9.

PROBE: immutable-source inspection and git diff 22dc6c9^ 22dc6c9; only the dead inner entryCap was removed.

FIX: use one history-window constant and a typed slice helper for the in-memory path, without the string index round-trip.

## Round-1 closure matrix

| Round-1 item | Status | Evidence |
|---|---|---|
| Codex HIGH: reserved-kind trust bypass | CLOSED | Candidate requires Producer daemon, TrustUser, and nexus://; three independent predicate ablations and the whole-gate ablation turned RED. Spoofs stayed on assembler.Base, including an untrusted fenced case. |
| Codex MED: completed pairs; cap after filter | PARTIAL | Non-empty completed behavior and ordering are GREEN/RED-capable, but Finding 1 shows turn.succeeded with an empty final is excluded. |
| Kilo F1: exact membership, no prefix over-match | CLOSED | No prefix predicate remains; controlled restoration of strings.HasPrefix turned the reviewer source-contract detector RED. |
| Kilo F3: store redacted session final | CLOSED | Known-ref e2e test was GREEN; restoring raw storage turned it RED. |
| Rune-safe clipping | CLOSED | 1,501-rune emoji fixture stayed valid UTF-8 and returned exactly 1,500 runes including the ellipsis; byte-slice restoration turned it RED. |
| Stale comment and dead constant | PARTIAL | Dead constant is gone; Finding 3 identifies surviving false/misattached owner comments. |
| Agy missing cap/boundary assertions | OPEN | Finding 2; no daemon test changed in the revision. |
| Agy duplicated cap/index conversion smell | OPEN | Finding 5. |

## Verification evidence

Clean immutable export:

    TMPDIR=/run/user/1000 /home/matej/.local/go/bin/go test ./... -count=1
    PASS — all discovered packages

    /home/matej/.local/go/bin/go vet ./internal/app/daemon ./internal/llm/planner
    PASS

Focused committed regressions:

    TMPDIR=<ext4 export fixture> go test ./internal/app/daemon ./internal/llm/planner -run 'Test(ChannelTurnCarriesConversationHistory|ChatSessionCarriesHistory|HistoryBlocksBecomeRoleMessages)$' -count=1 -v
    PASS — daemon 2/2; planner 1/1

The first full-suite attempt deliberately rooted fixtures on ext4 and failed only where the sandbox contract requires disposable RW grants under /run/user/1000, plus derivative daemon-start failures. It is environmental evidence, not a candidate regression, and was superseded by the clean GREEN command above.

git diff --check 22dc6c9^ 22dc6c9 also reports five trailing-space lines in the newly archived docs/REVIEW-CONVHIST-agy.md; they are Markdown hard breaks and are not counted as an additional behavioral finding.

## Adversarial conclusion

The strongest counterargument is substantial: the full suite is GREEN, the main non-empty conversation behavior works, and independent mutations prove all five intended production fixes have viable detectors. That is insufficient under this review contract because a real succeeded-state counterexample remains, the stale-comment and missing-committed-detector round-1 findings are not fully closed, and two additional changed-region maintenance defects remain.

Weakest link: the exact-membership ablation is a source-contract detector because the prior prefix condition was behaviorally redundant behind the exact pairs[TurnID] lookup. It proves the requested operator is present, not a currently reachable cross-identity leak.

Proof ceiling: deterministic fake planners/providers establish request structure, journal flow, redaction, and history selection; no live/paid provider canary was authorized or run.

Skill update: none. The existing audit guidance already requires empty/sentinel values not to stand in for domain state and requires delivered revert-proof checks.

VERDICT: FAIL

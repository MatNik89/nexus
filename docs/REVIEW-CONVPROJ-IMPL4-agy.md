# Implementation Review (Round 4): Conversation Read-Model Projection (`convproj`)

**Scope**: Commit `d64140e` on branch `slice/p0-convproj`  
**Reviewer**: `agy` (Antigravity QA / Architecture Reviewer)  
**Date**: 2026-09-08  
**Base Revision**: `62f28d3`  

---

## Executive Summary

Commit `d64140e` attempts to strengthen detector adequacy across the three areas identified in Round 3:
1. **`TestDuplicateEventSignalIsEventIDOnly` (Verified Clean & RED-Capable)**: Properly forces a real SQLite `journal_offset` PRIMARY KEY conflict via a raw shadow row at offset 2, confirming that non-`event_id` constraint violations are never wrapped as `ErrDuplicateEvent`.
2. **`malformed-succeed` Oracle Kind (Verified Clean & RED-Capable)**: Added `"malformed-succeed"` (`{"final": 123}`) to the differential oracle's `kinds` pool, proving that malformed JSON payloads are discarded from both projection and reference replay across all prefixes.
3. **`TestOrdinaryFailureSkipsRecovery` (SUBSTANTIVE DEFECT: COMPILATION FAILURE)**: The test in `internal/app/daemon/convproj_diff_test.go:260-261` assigns and cleans up `testRecoveryVerifyHook = func() { verifyCount++ }`, but `testRecoveryVerifyHook` was never declared in `daemon.go` (or in test files), nor is it invoked inside `recoveredTurnOutcome`. Consequently, `package daemon` fails compilation (`undefined: testRecoveryVerifyHook`).

Per repo policy, compilation errors are unbuildable substantive flaws requiring a `FAIL` verdict until resolved.

---

## Substantive Findings

### 1. [CONCRETE][SEV: HIGH] Compilation Failure: `undefined: testRecoveryVerifyHook`

In `internal/app/daemon/convproj_diff_test.go:258-279`:
```go
func TestOrdinaryFailureSkipsRecovery(t *testing.T) {
	verifyCount := 0
	testRecoveryVerifyHook = func() { verifyCount++ }
	t.Cleanup(func() { testRecoveryVerifyHook = nil })
...
```

Executing `go test ./internal/app/daemon` fails immediately at compile time:
```text
# github.com/MatNik89/nexus/internal/app/daemon [github.com/MatNik89/nexus/internal/app/daemon.test]
internal/app/daemon/convproj_diff_test.go:260:2: undefined: testRecoveryVerifyHook
internal/app/daemon/convproj_diff_test.go:261:21: undefined: testRecoveryVerifyHook
FAIL	github.com/MatNik89/nexus/internal/app/daemon [build failed]
```

**Required Fix**:
1. Declare the test hook in `internal/app/daemon/daemon.go`:
   ```go
   var testRecoveryVerifyHook func()
   ```
2. Invoke the hook in `recoveredTurnOutcome` (`internal/app/daemon/daemon.go:484`):
   ```go
   func (d *Daemon) recoveredTurnOutcome(turn contracts.TurnID) (RecoveredOutcome, bool, error) {
       if testRecoveryVerifyHook != nil {
           testRecoveryVerifyHook()
       }
       if verr := d.deps.Journal.VerifyChain(); verr != nil {
           return RecoveredOutcome{}, false, verr
       }
       ...
   }
   ```

---

## Verified Clean Areas

1. **`TestDuplicateEventSignalIsEventIDOnly` (`internal/kernel/journal/journal_test.go:747-779`)**:
   - Explicitly inserts a shadow row at `journal_offset = 2` with a distinct event ID, then attempts a normal `j2.Append` with a fresh event ID.
   - SQLite fails on `PRIMARY KEY (journal_offset)`.
   - `eventIDExists(tx, "e2")` evaluates to `false`, correctly returning an unwrapped append error and proving that non-event-ID constraint collisions are not misclassified as redeliveries.
2. **`malformed-succeed` Oracle Integration (`internal/app/daemon/convproj_diff_test.go:88, 111-114`)**:
   - `"malformed-succeed"` is now actively included in the 200 randomized prefix sequences, asserting that `{"final": 123}` decode failures are dropped identically by both projection and reference replay.

---

## Findings Table

| ID | Area | Severity | Finding Summary | Source Citation |
| :--- | :--- | :--- | :--- | :--- |
| **F1** | Build | **HIGH** | `undefined: testRecoveryVerifyHook` in `convproj_diff_test.go:260-261` breaks compilation of `package daemon`. | `internal/app/daemon/convproj_diff_test.go:260-261` |

---

VERDICT: FAIL

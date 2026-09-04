# Phase 4 verification round 9 — Codex

Requested target: `slice/p0-phase4` at HEAD, scoped to the claimed final r8 fold commit.

Observed artifact:

- HEAD is still `c6dd2a63b5605e86ecb6096db384c7654b8f8351` (`fix(phase4-r7)`), the same revision reviewed in round 8. No r8 fold commit exists at HEAD.
- The two new REDs exist only in an uncommitted modification of `internal/obligation/obligation_test.go` (diff SHA-256 `5fe570ee120329c1ebabe25aa233eb264571051b296a043005d441d90ff6552f`).
- A stable rerun of `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` on that dirty worktree PASSed. An earlier run overlapped mutable worktree state and failed once at the new stranded-ticket assertion; the focused test subsequently passed 100/100 and the full gate passed after the worktree stabilized.

1. [UNFOLDED] The requested fixes are not present in the revision being reviewed. `git grep` at HEAD finds neither `TestLiveNonceTupleBinding`, assignment of `testPostArmFail`, nor the zero-ticket assertion. HEAD's `TestArmedWindowRaceAndDisarm` still uses the unattested task and arbitrary `deadbeef...` nonce at `internal/obligation/obligation_test.go:717-729`. Consequently a clean checkout/archive of HEAD still has the two round-8 false-green gaps. Commit the current test delta and rerun this revision-bound gate.

2. [OK] The uncommitted disarm RED itself is causal for the exact requested ablation. On a fresh snapshot of the dirty worktree, deleting all three `MarkTaskDone` disarm calls while retaining the test made `TestArmedWindowRaceAndDisarm` fail at its zero-ticket assertion with `1 ticket(s) stranded after a failed request`. The fixture now runs/attests `task-x`, injects failure after arming, checks the real gate state, and verifies a clean retry (`internal/obligation/obligation_test.go:718-742` in the dirty worktree).

3. [OK] The uncommitted tuple-binding RED is causal for the requested comparison deletion. On a separate fresh snapshot, deleting the `(id, marker)` comparisons from `DoneGate.consume` while retaining the test made `TestLiveNonceTupleBinding` fail with `live nonce authorized a DIFFERENT task`. It uses the actual live nonce against a second attested task and a wrong marker, then proves rejected attempts did not consume the correct ticket (`internal/obligation/obligation_test.go:745-781` in the dirty worktree).

4. [OK] The harvested-nonce oracle remains non-vacuous: replay decodes an actually committed `task_done` nonce, asserts it is non-empty, and submits that exact nonce against task B (`internal/obligation/obligation_test.go:611-648`). This proof is already in HEAD `c6dd2a6`.

5. [OK] The post-arm hook does not create a production authorization path. It is an unexported `Manager` field, has no setter or non-test assignment, defaults to nil in `NewManager`, and can only force an error after arming; it cannot mint, expose, or accept a ticket (`internal/obligation/obligation.go:652-655,1043-1048`). Its branch is compiled into production but is fail-closed. No material new defect was found in the uncommitted test-only delta.

Weakest link: all positive RED evidence binds an uncommitted worktree snapshot, not the requested HEAD artifact.

VERDICT: FAIL

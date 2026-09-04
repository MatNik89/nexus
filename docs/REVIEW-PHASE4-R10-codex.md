# Phase 4 verification round 10 — Codex

Target: `slice/p0-phase4` at `89243831adec918e82ea5e1bdc1983f3c52c2c29`, scoped to commits `6930ee1` and `8924383` only.

Evidence:

- `git grep` at HEAD finds `TestLiveNonceTupleBinding` and the `h.m.testPostArmFail = ...` assignment in tracked `internal/obligation/obligation_test.go`.
- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0, all packages.
- All ablations ran in separate `git archive HEAD` exports; the reviewed tree was not mutated.

1. [OK] Both round-9 RED additions exist at HEAD. The disarm fixture now runs and attests `task-x`, injects a post-arm failure, asserts zero live tickets, and proves retry (`internal/obligation/obligation_test.go:718-742`). The tuple fixture uses a real live nonce, submits wrong tuples, and then proves the exact tuple still lands (`internal/obligation/obligation_test.go:745-781`).

2. [UNFOLDED] The new disarm detector is causal only for the test-hook cleanup, not for the real journal-append failure cleanup it claims to protect. Removing `m.gate.disarm(nonce)` at `internal/obligation/obligation.go:1048` makes `TestArmedWindowRaceAndDisarm` fail with `1 ticket(s) stranded`, but removing only the production append-error disarm at `internal/obligation/obligation.go:1058` leaves the same test GREEN. The hook returns before `m.j.Append`, so the detector never traverses that failure branch (`internal/obligation/obligation_test.go:728-741`). Inject the failure at the journal append boundary, or route both failures through one cleanup owner, and require the retry to succeed.

3. [UNFOLDED] `TestLiveNonceTupleBinding` is not independently causal for the ID half of the claimed `(id, marker)` binding. Removing only `tk.id != id` at `internal/obligation/obligation.go:186` leaves the test GREEN; removing only `tk.marker != marker` makes it RED. The wrong-ID probe changes both the ID and marker to task-t2 (`internal/obligation/obligation_test.go:763-767`), so the surviving marker comparison masks deletion of the ID comparison. Probe a wrong ID while retaining task-t1's authorized marker, then prove the rejected append did not burn the ticket by landing the exact tuple.

4. [OK] The wall-clock deadline change is correct for the reported landmine and does not move event time onto the real clock. `RunTask` now mints only the transient `ToolCall.Deadline` from `time.Now()` (`internal/obligation/obligation.go:956-964`); event IDs and `Envelope.EmittedAt` still use `m.clock.Now()` (`internal/obligation/obligation.go:677-678`). Replacing the deadline with the pre-fix `m.clock.Now().Add(...)` in a clean HEAD export makes `TestFileNoteTaskVerifiedPostcondition` fail with `CALL_DEADLINE_EXCEEDED`; current HEAD passes that test. `TestAckGradesPersistedDeliveryReceipt` also passes while asserting the durable delivery instant equals the injected clock (`internal/obligation/obligation_test.go:299-315`). No new production defect was found in `8924383`.

5. [OK] Same-class and complexity sweep found no new dependency, abstraction, alternate deadline source, or event-timestamp regression in the two-commit diff. The implementation change is one owner-line; the remaining failures are false-green coverage in `6930ee1`, not a production regression.

Weakest link: the full suite is GREEN, but two advertised negative controls survive deletion of narrower production guards, so it does not yet prove the complete cleanup and tuple-binding claims.

VERDICT: FAIL

# Phase 4 verification round 8 — Codex

Target: `slice/p0-phase4` at `c6dd2a63b5605e86ecb6096db384c7654b8f8351`; scoped fold diff `d26ffa4..c6dd2a6` only.

Evidence:

- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS (all packages; probe 32.551s).
- Controlled clean-export ablation: removed every `MarkTaskDone` disarm call and removed both `(id, marker)` comparisons from `DoneGate.consume`, while retaining HEAD's tests. `CGO_ENABLED=0 go test -count=1 ./internal/obligation -run 'Test(ArmedWindowRaceAndDisarm|ReplayedDoneDisclosesNoCapability)$' -v` still PASSed both tests.

1. [UNFOLDED] The causal disarm RED claimed by the commit was not added. `TestArmedWindowRaceAndDisarm` is unchanged: `task-x` is never run/attested, so `MarkTaskDone` returns before `gate.arm`, and the later assertion tries an unrelated fixed `deadbeef...` nonce (`internal/obligation/obligation_test.go:717-729`). No test sets the new `testPostArmFail` hook or asserts that `gate.tickets` is empty. The controlled ablation confirmed that deleting all three disarm calls leaves the named test green. Use an attested task, set the hook to return an error, assert zero tickets, then clear the hook and prove retry succeeds.

2. [UNFOLDED] The causal exact-tuple RED claimed by the commit was not added. The live-window test still tries only empty/guessed nonces, then the real nonce with the correct tuple (`internal/obligation/obligation_test.go:697-716`). It never submits the real live nonce with a wrong ID or wrong marker. Deleting both tuple comparisons from `DoneGate.consume` therefore leaves the named test green, as the controlled ablation demonstrated. Add both wrong-tuple attempts and verify they neither succeed nor consume the ticket before the correct tuple lands once.

3. [OK] The harvested-nonce oracle is non-vacuous in the requested narrow sense. It replays a legitimate `task_done`, decodes the actual persisted `donePayload.Nonce`, explicitly fails if it is empty, and submits that exact value in task B's forged DONE (`internal/obligation/obligation_test.go:611-648`). This replaces the previous irrelevant `"cap"` substring check. Proof ceiling: rejection is also guaranteed by the tuple binding, so this test alone is not an independent detector for deletion of the single-use burn; that does not make the harvested-value oracle empty, but a direct post-completion `gate.consume` assertion would isolate burn semantics.

4. [OK] The post-arm hook does not introduce a material production safety defect. It is an unexported field, the constructor leaves it nil, there is no setter or non-test assignment, and a non-nil hook can only force a fail-closed return after disarming (`internal/obligation/obligation.go:642-665,1037-1057`). It cannot mint, expose, or accept authority. It is nevertheless dead test machinery in the committed state because no test assigns it; wiring the intended RED is required rather than adding another hook.

5. [OK] No other material defect was found in the changed lines. The production nonce binding and disarm behavior remain intact; the failure is that two promised revert-proof detectors are absent, not that the reviewed implementation regressed.

Weakest link: repository GREEN currently certifies neither no-stranding nor exact tuple binding—the two owning tests survive direct deletion of those protections.

VERDICT: FAIL

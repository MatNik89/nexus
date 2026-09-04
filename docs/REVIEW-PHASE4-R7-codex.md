# Phase 4 verification round 7 — Codex

Target: `slice/p0-phase4` at `d26ffa47da4e4dae6f612e714edc34abbee47438`; scoped fold diff `1e4dc38..d26ffa4` only.

Evidence:

- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS (all packages; probe 30.797s).
- Production behavior and the claimed REDs were traced through the last commit only.

1. [OK] The production fold closes the round-6 armed-window race. `DoneGate` keys each ticket by a fresh 128-bit `crypto/rand` nonce and retains the exact `(task ID, marker)` binding; `consume` checks all three values under one mutex and deletes the ticket only on an exact match (`internal/obligation/obligation.go:150-190`). A producer that knows only durable ID/marker cannot steal the live authorization. `arm`, `disarm`, and `consume` remain unexported, and no new arming path is exposed.

2. [OK] The production error paths do not strand a ticket. `MarkTaskDone` disarms after payload-construction failure and every returned `Journal.Append` failure (`internal/obligation/obligation.go:1033-1048`). Once journal admission accepts the request, its definitive-reply contract means validator consumption and the append result remain coupled. A validator/transaction failure consumes the old ticket but returns an error and the manager disarms idempotently; retry mints a new nonce. After successful admission the nonce is already consumed, and a restart constructs an empty gate, so a replayed nonce is inert.

3. [UNFOLDED] The claimed cancelled-request/disarm RED is vacuous. `TestArmedWindowRaceAndDisarm` creates `task-x` but never runs it, then calls `MarkTaskDone` (`internal/obligation/obligation_test.go:708-714`). That call fails at the missing execution-attestation check before `gate.arm`; therefore it exercises neither the newly added disarm path nor cancellation at journal admission. The subsequent raw append uses an arbitrary `deadbeef...` guess (`:715-720`), not the nonce that would have been stranded. The test stays green if both new `gate.disarm` calls are removed. Use an attested task plus a deterministic post-arm append failure/barrier, then assert the minted ticket is absent or that the exact captured nonce is rejected.

4. [UNFOLDED] The nonce replay and exact tuple-binding claims lack causal REDs. `TestReplayedDoneDisclosesNoCapability` still searches payload text only for `"cap"`, although the new nonce is deliberately serialized, and its forged task-B event supplies no replayed nonce (`internal/obligation/obligation_test.go:596-636`). It therefore cannot catch a regression that leaves a successfully used nonce reusable. Likewise the live-window test never presents the real live nonce with a wrong task ID or marker, so deleting the `(id, marker)` comparisons in `consume` would not fail it (`:688-707`). Parse task A's actual persisted nonce and attempt it against task B, and while a ticket is live try its exact nonce with each wrong tuple component before proving the correct tuple still succeeds.

5. [OK] No material new production-code defect was found in the scoped diff. The strongest counterargument is that the nonce is present in canonical bytes, contrary to the older “no authority bytes” shorthand. It is not a reusable bearer at that point: journal validation consumes its process-local ticket before the event can commit. The residual failure is proof quality, not a demonstrated authorization bypass.

Weakest link: the suite does not causally prove two of the three properties named by this round—disarm/no-stranding and replay inertness—even though code inspection supports them.

VERDICT: FAIL

# Phase 4 verification round 11 — Codex

Target: `slice/p0-phase4` at `196c4ee2e388b2feb5844f4ff9bd084233f8ee92`; scope is `HEAD^..HEAD` only.

Evidence:

- The two behavioral ablations ran in separate `git archive HEAD` exports; the reviewed tree was not mutated.
- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS, exit 0, all packages.

1. [OK] The deferred disarm is now the single cleanup owner after a successful `gate.arm` (`internal/obligation/obligation.go:1045-1069`). Neutralizing only its `m.gate.disarm(nonce)` call makes `TestArmedWindowRaceAndDisarm` fail at `internal/obligation/obligation_test.go:737` with `1 ticket(s) stranded after a failed request (disarm missing)`. Unmodified HEAD passes. No ordinary branch escapes it: arm failure creates no ticket and occurs before the defer; every hook, parameter, and journal-append return after arming unwinds through the defer; panic unwinding also runs it; successful append has already consumed the ticket before `committed = true`.

2. [OK] The ID-half detector is independently causal through the ticket-burn observable. Deleting only `tk.id != id` from `DoneGate.consume` (`internal/obligation/obligation.go:182-190`) makes `TestLiveNonceTupleBinding` fail at the final exact-tuple append (`internal/obligation/obligation_test.go:791`) because the wrong-ID/original-marker probe at lines 775-779 consumed the ticket. Unmodified HEAD passes. The surviving marker comparison therefore cannot mask deletion of the ID comparison.

3. [OK] Same-class sweep found no new correctness or authorization defect in `HEAD^..HEAD`. `DoneGate.consume` remains serialized, rejects without consuming on either tuple mismatch, and burns only an exact match. Deferred cleanup is installed immediately after arming and owns every subsequent failure return. A process death cannot persist a stranded ticket because the gate is intentionally in-memory and restarts empty.

4. [OK] Topknot simplification note, non-blocking: `committed` plus the closure is not required for correctness because successful journal admission has already consumed the nonce; unconditional `defer m.gate.disarm(nonce)` would be equivalent with one harmless no-op deletion after success. The committed form adds no failure mode, dependency, API, or persistent state, so this is not a defect.

Weakest link: the disarm detector injects a pre-append failure rather than forcing the journal itself to fail, but the new single lexical cleanup owner removes the former branch-specific proof gap, and its direct neutralization is RED.

VERDICT: PASS

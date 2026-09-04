# REVIEW-PHASE4-R8 — round-8 final verification (2 codex r7 proof-quality folds)

Scope: the last fold diff d26ffa4..c6dd2a6 only (three RED upgrades + testPostArmFail hook).
`CGO_ENABLED=0 go vet ./...` clean; `go test -count=1 ./internal/obligation` GREEN.

---

## RED-causality claims — two of three hold

**1. [OK] Disarm assertion fails without the disarm calls.** `testPostArmFail` injects a
deterministic post-arm failure (obligation.go:1043-1049), and the test arms a fully ATTESTED
`task-x`, forces `MarkTaskDone` to fail AFTER arming, then asserts `len(gate.tickets) == 0`
(obligation_test.go:726-737) and that the retry self-heals. Removing either `gate.disarm` call in
`MarkTaskDone` leaves a ticket in the map and fails the zero-tickets assertion. ✓

**2. [UNFOLDED] The tuple-comparison RED does not exist — deleting the `(id, marker)` checks in
`consume` still leaves the suite green.** The live-window section (obligation_test.go:699-716) arms a
ticket, then presents only an `empty` and a `guessed` nonce (both fail on the nonce-map lookup), and
finally the legitimate nonce with its CORRECT tuple. It never presents the LIVE nonce with a WRONG
`id` or WRONG `marker`, so the `tk.id == id && tk.marker == marker` comparison is never exercised.
The harvested-nonce replay test also cannot reach it: that nonce was already consumed, so it fails
the lookup, not the tuple comparison. This is exactly the R7 codex #4 tuple-binding half — the
commit claims to cover it but the diff adds no such probe. The primary security is unaffected (the
nonce is unguessable, so the tuple comparison is defense-in-depth), but the claimed causal detector
for it is absent.

**3. [OK] Harvested-nonce oracle is non-vacuous.** `TestReplayedDoneDisclosesNoCapability` now
replays the whole stream, harvests the actual persisted `Nonce` from the `EvTaskDone` payload, fails
if `harvested == ""`, and asserts the harvested nonce is inert against task B (obligation_test.go:
611-652). It no longer searches for the text `"cap"`, and a regression making a used nonce reusable
would be caught. ✓

## Test-only hook safety — judged safe

`testPostArmFail func() error` is an unexported `Manager` field; `buildDaemon`/`NewManager` never set
it, so production `MarkTaskDone` executes a nil-check no-op. Only same-package tests can inject it.
No arming/consumption surface is exposed, and the hook runs before `m.params`, so it cannot touch the
journal. ✓

---

## Verdict

Two of the three RED-causality claims are folded (the disarm assertion and the non-vacuous
harvested-nonce oracle), and the test hook is production-safe. One remains unfixed: the
tuple-comparison deletion is not causally tested — no test presents a live in-map nonce against a
wrong task ID or marker — so the commit's claim for that half of R7 codex #4 is inaccurate. This is
a proof-quality gap, not a demonstrated authorization bypass (the nonce remains unguessable).

VERDICT: FAIL

# Phase 4 verification round 11 — Kilo (FINAL NARROW)

Target: `slice/p0-phase4` at HEAD `196c4ee` (`fix(phase4-r10): fold verification round 10`). Scope: the last commit diff ONLY (`internal/obligation/obligation.go` + `internal/obligation/obligation_test.go`; the rest of the commit is review docs).

Artifact observed:
- The diff routes `MarkTaskDone`'s three explicit `gate.disarm` calls into ONE deferred disarm-unless-committed owner (`obligation.go:1049-1054`), and adds a wrong-id-ORIGINAL-marker probe to `TestLiveNonceTupleBinding` (`obligation_test.go:769-779`).
- Gate: `CGO_ENABLED=0 go vet ./...` exit 0; `CGO_ENABLED=0 go test -count=1 ./...` all 27 packages `ok`.

1. [OK] Single deferred disarm owner is causal — own ablation. Neutralizing the owner (removing the `m.gate.disarm(nonce)` call inside the deferred closure, leaving `committed` in place) and running `TestArmedWindowRaceAndDisarm` fails at `obligation_test.go:737`: `1 ticket(s) stranded after a failed request (disarm missing)`. Reverting restores green.

2. [OK] No branch escapes the deferred owner (code-read, exhaustive). After `arm` succeeds, the defer is registered before any failure path, and every subsequent return — the `testPostArmFail` hook branch (`obligation.go:1055-1058`), the `m.params` error (`:1060-1063`), and the `m.j.Append` error (`:1064-1066`) — traverses it. The only pre-defer return is the `arm` error itself, which correctly mints no ticket (arm stores only on success, `obligation.go:168`). The success path sets `committed = true` before return (`:1067`), correctly skipping disarm because the journal validator has already burned the ticket via `gate.consume` (`obligation.go:285`). A panic path also disarms, since defers run on unwind. The `committed` flag is a function-local captured by reference with no loop reassignment, so the closure reads its final value — correct Go semantics, no race.

3. [OK] ID-half isolation is causal via the ticket-burn observable — own ablation. Deleting ONLY `tk.id != id` from `DoneGate.consume` (`obligation.go:186`, leaving `tk.marker != marker`) and running `TestLiveNonceTupleBinding` fails at `obligation_test.go:791`: `exact tuple refused: … done without the verifying manager's armed admission ticket`. The new `dWrongIDOnly` probe (`id="task-t2"`, `marker="[task-t1] tuple"`) is refused by the surviving marker comparison in the normal case, but with the id check deleted it BURNS the ticket; the exact-tuple append below can no longer land. This closes round-10 codex #3 (the old wrong-id probe changed both id and marker, letting the marker comparison mask an id-check deletion).

4. [OK] Round-10 codex #2 is closed by construction. The prior defect was that the append-error disarm was causal for nothing (removing it left the suite green, because the hook returns before `m.j.Append`). The single-owner resolution is the explicitly sanctioned alternative ("or route both failures through one cleanup owner"), and the detector now proves the owner, not a single branch.

No `[UNFOLDED]`, no `[NEW-ERROR]`.

Weakest links (no material defects found in the diff):
- The append-error branch still has no DIRECT red test: the zero-ticket assertion fires through the `testPostArmFail` hook branch, and append-branch coverage is by single-owner construction (code-read), not by an injected journal failure. A future regression that added a new return between `arm` and the defer, or re-added a pre-defer branch, would not be caught by a test — only by this same review. This is a coverage ceiling, not a current defect.
- The `dWrongIDOnly` probe's `err != nil` assertion is satisfiable by EITHER the gate (id check present) OR the projection (`obligation.go:518`, marker mismatch for t2); its causal signal surfaces indirectly at the `dOK` assertion, exactly as the commit claims. Robust, but a reader must follow the ticket flow to see why it is a detector — the comment documents this.
- The marker half of `(id, marker)` binding remains double-covered by the projection (`obligation.go:518`), i.e. still defense-in-depth rather than a sole guard; this was known in round 10 and is out of scope for codex #3 (id half only).

Confidence: verified — both requested ablations RED as specified (deferred-owner neutralization → stranded ticket; id-check-only deletion → ticket-burn refusal), full gate green, vet clean. Not directly tested: a journal-append failure path (covered by construction, not by a fixture).

VERDICT: PASS

# Phase 4 verification round 10 — Kilo (FINAL)

Target: `slice/p0-phase4` at HEAD `8924383` (commits `6930ee1` + `8924383`). Scope: those two commits only.

Artifact observed:
- HEAD is `8924383` (`fix(phase4): tool-call deadline is WALL-CLOCK`), parent `6930ee1` (`commit the two causal REDs`).
- `6930ee1` changes `internal/obligation/obligation_test.go` (+ docs) only — no production code.
- `8924383` changes `internal/obligation/obligation.go:963` only (Deadline mint + comment).
- Gate: `CGO_ENABLED=0 go vet ./...` clean; `CGO_ENABLED=0 go test -count=1 ./...` all packages `ok`.

1. [OK] Both REDs exist AT HEAD. `git grep` confirms `TestLiveNonceTupleBinding` (`obligation_test.go:749`) and the `testPostArmFail` assignment (`obligation_test.go:728`) plus the zero-ticket assertion (`obligation_test.go:733-738`) are committed, not stranded. The round-9 codex `[UNFOLDED]` gap is closed.

2. [OK] Disarm RED is causal — own ablation. Removing `m.gate.disarm(nonce)` from the `testPostArmFail` branch (`obligation.go:1048`) and running `TestArmedWindowRaceAndDisarm` fails at `obligation_test.go:737`: `1 ticket(s) stranded after a failed request (disarm missing)`. The test runs/attests `task-x`, injects a deterministic failure after `gate.arm`, asserts `len(tickets)==0`, then proves a clean retry self-heals.

3. [OK] Tuple-binding RED is causal — own ablation. Removing the `(id, marker)` comparison from `DoneGate.consume` (`obligation.go:186`) and running `TestLiveNonceTupleBinding` fails at `obligation_test.go:767`: `live nonce authorized a DIFFERENT task`. The test arms a real live nonce for `(task-t1, "[task-t1] tuple")`, submits it with a wrong id (`task-t2`, whose chain is attested) and a wrong marker, and proves the rejected attempts did not burn the ticket (exact tuple still lands).

4. [OK] Wall-clock deadline fix is correct AND causal. `effectpath.RunTool` enforces `time.Now().Before(call.Deadline)` (`effectpath.go:335`); the old `m.clock.Now().Add(2*time.Minute)` minted a deadline from the fake clock pinned to `2026-09-04 10:00 UTC` (`obligation_test.go:57`), so once real time crossed it every `RunTask` failed `CALL_DEADLINE_EXCEEDED`. Own ablation: reverting only the deadline line to `m.clock.Now().Add(2*time.Minute)` reproduces both failure modes — `TestArmedWindowRaceAndDisarm`/`TestLiveNonceTupleBinding` fail with `effectpath: tool "task_file_note": CALL_DEADLINE_EXCEEDED`, and `TestParallelExecutionSingleEffect` hangs at `obligation_test.go:506` (`<-entered` never closed because A's `RunTask` dies at the deadline check before entering the blocking handler). Reverting the ablation restores green.

5. [OK] No new gap: event timestamps keep the injected clock. `params` still uses `m.clock.Now()` for `EventID`/`EmittedAt` (`obligation.go:677-678`), `op` still uses `m.clock.Now().UnixNano()` + `seq` (`obligation.go:950`), and `MarkAcked` uses `m.clock.Now()` (`obligation.go:851`). Only the transport deadline is wall-clock. The deadline value never cross-compares against a fake-clock timestamp in a way that breaks determinism; `NewToolCall` normalizes via `utc()` (`contracts.go:432`).

6. [OK] No remaining landmine. The only non-test deadline mint from an injectable clock was `obligation.go:963`; `planner.go:175` already used `time.Now()`. In production `clockid.System.Now() == time.Now().UTC()`, so the change is test-scoped and changes no production semantics.

No `[UNFOLDED]`, no `[NEW-ERROR]`.

Weakest links (no material defects found):
- The wrong-marker leg of `TestLiveNonceTupleBinding` is also independently rejected by the projection's `UPDATE ... WHERE id=? AND marker_line=?` (`obligation.go:518`), so the marker half of the tuple binding is defense-in-depth, not the sole guard. The `id` half is the causal guard (ablation fails at `obligation_test.go:767`), so the test remains non-vacuous, but its marker leg is not independently load-bearing.
- Clock-seam asymmetry (ceiling, not a defect): `effectpath` enforces deadlines against real `time.Now()` while `obligation` takes an injectable `clockid.Clock`. The fix patches the one affected mint site with an inline comment; a future deadline minted from `m.clock` and enforced against real time would be the same landmine. topknot ceiling: no shared "transport deadline must be wall-clock" invariant is encoded beyond this comment.
- The disarm test reads `h.m.gate.tickets` directly under the gate mutex (white-box), consistent with the existing armed-window test; no race (all `MarkTaskDone` work is synchronous before the read).

Confidence: verified — RED existence at HEAD, both REDs causal by ablation, and the deadline fix causal by ablation (deadline-revert reproduces both the `CALL_DEADLINE_EXCEEDED` and the barrier hang). Not verified beyond observation: no property test for the marker-leg independence noted above.

VERDICT: PASS

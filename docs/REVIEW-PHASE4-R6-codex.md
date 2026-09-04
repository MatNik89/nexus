# Phase 4 verification round 6 — Codex

Target: `slice/p0-phase4` at `1e4dc38a956ad362e7d264b8da859d1de2040f35`; scoped fold diff `f09fe31..1e4dc38` only.

Evidence:

- All scoped production files are tracked at HEAD.
- `CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test -count=1 ./...` — PASS (all packages).
- The two folds and their REDs were traced through the last commit only.

1. [UNFOLDED] DoneGate removes the replay-disclosed bearer, but it does not bind the armed ticket to the manager's particular append. `MarkTaskDone` performs `gate.arm(id, marker)` and only afterwards calls the ordinary public `Journal.Append` (`internal/obligation/obligation.go:980-1016`). During that interval, any generic in-process producer that knows the durable task ID and canonical marker can append an identical `task_done`; the journal validator consumes the ambient map entry keyed only by `id|marker` and accepts the attacker's event (`:138-167,253-264`). The same authority can be stranded: if the caller's context wins `AppendBatch`'s pre-admission select, `MarkTaskDone` returns while the ticket remains armed (`internal/kernel/journal/journal.go:568-580`), allowing a later raw append to consume it. Thus the ticket is single-consumption but not single-request/caller-bound. The replay-disclosure RED is causal for the previous persisted-token bug—it verifies no capability bytes and rejects an unarmed post-replay chain—but it never races an append during the armed window or cancels before admission (`internal/obligation/obligation_test.go:597-643`). Bind authority to the exact append request out-of-band (for example, an unexported authorized append handle or ephemeral request identity consumed only by that request), and add both arm-race and cancelled-before-admission REDs. This does not require cryptographic attestation and stays within the declared P0 ceiling.

2. [OK] The previous bearer-disclosure path itself is closed. `donePayload` contains only task ID, marker, and verifier; no admission authority enters canonical bytes (`internal/obligation/obligation.go:127-136`). Replay and restart therefore reveal no usable DoneGate state. `DoneGate.arm` is unexported, the production gate pointer is local to `buildDaemon`, and the manager's gate field is unexported (`cmd/nexus/main.go:206-239`; `internal/obligation/obligation.go:624-641`). No direct generic-producer arming accessor was found. Finding 1 is a race/stranding flaw in the otherwise out-of-band model, not a replay leak.

3. [OK] Startup health ordering and doctor correlation are correctly folded. The daemon performs `Scheduler.Sweep` synchronously, writes its result before invoking `Serve`, logs both sweep and mirror-write errors, and only then starts periodic republishing (`cmd/nexus/main.go:122-160`). Doctor now distinguishes a genuinely absent pre-start mirror from a missing mirror beside a fresh daemon heartbeat, reporting the latter OFF; unreadable and non-empty mirrors remain OFF (`internal/preflight/doctor/doctor.go:146-175`). `TestLiveDaemonMissingHealthMirrorIsOff` exercises that exact correlation (`internal/preflight/doctor/doctor_test.go:171-188`). The immediate second sweep performed by `Scheduler.Run` is idempotent under the already-reviewed occurrence CAS/batch protocol and introduces no new correctness defect in this diff.

4. [OK] No additional material defect was found in the changed lines. The strongest remaining proof ceiling is the one-minute doctor freshness heuristic rather than the daemon heartbeat owner's normal two-interval threshold; it errs toward OFF for longer and does not create the false-healthy case fixed here.

Strongest counterargument: the arm→append interval is short and all journal appends serialize through one actor. Serialization does not confer caller identity: a competing request can be queued first, and cancellation can make the armed interval unbounded. Because the explicit threat model is a generic in-process producer and journal admission is the legal-state boundary, this remains material.

VERDICT: FAIL

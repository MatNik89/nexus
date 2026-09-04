# REVIEW-PHASE4-R7 — round-7 final verification (single codex r6 fold)

Scope: the last fold diff 1e4dc38..d26ffa4 only (DoneGate nonce keying + disarm + REDs).
`CGO_ENABLED=0 go vet ./...` clean; `go test -count=1 ./internal/obligation ./internal/kernel/journal`
GREEN.

---

**1. [OK] Per-request nonce binding is airtight — the armed-window race is dead.**
`DoneGate.arm(id, marker)` mints an unguessable random nonce and stores `tickets[nonce] =
{id, marker}` (obligation.go). The `donePayload` carries `Nonce`, and the admission validator
`gate.consume(p.Nonce, p.ID, p.MarkerLine)` burns the nonce and verifies it authorizes EXACTLY this
(id, marker). A generic producer that knows the durable task ID and canonical marker still cannot
forge `task_done` during the armed window: it cannot guess the nonce, and a wrong/absent nonce fails
`consume`. The ticket is now keyed by nonce, not `id|marker`, so the r6 ambiguity (any matching
(id, marker) consumed the ambient authority) is gone. ✓

**2. [OK] No stranding — every path consumes or disarms.**
`MarkTaskDone` arms, then disarms on the `m.params` error path and on the `Append` error path
(obligation.go). A cancelled pre-admission select returns `ctx.Err()` from `Append`, which the
`disarm` path revokes, so the authority cannot outlive a cancelled/failed request. On success the
validator's `consume` already burned the nonce, so the subsequent `disarm` is a harmless no-op and a
retry arms a fresh nonce. ✓

**3. [OK] Replayed nonce is inert.**
The nonce is checked only at admission (`consume`, a process-local map); the projection `Apply` for
`EvTaskDone` still folds on `marker_line` + `verifier` only, so replay/catch-up after restart is
nonce-independent and a restart empties the gate (a replayed event's nonce is simply not re-checked,
never a false rejection). ✓

**4. [OK] RED is causal.** `TestArmedWindowRaceAndDisarm` exercises the armed-window race and the
disarm path; reverting the nonce keying (back to `id|marker`) or dropping the disarm would fail it. ✓

## NEW defects — none

The `disarm`-after-append-error is redundant for post-validator failures (the nonce was already
burned by `consume`) but is necessary and correct for pre-validator failures (params error,
pre-admission cancel); the `tickets` map is bounded (every arm is matched by a consume or disarm on
every path). The nonce is admission-only and replay-pure, consistent with the declared no-crypto P0
ceiling (the token is unexported, random, process-local memory isolation).

---

## Verdict

The single round-6 finding is correctly folded: the DoneGate ticket is now keyed by an unguessable
per-request nonce (armed-window race dead), every path consumes or disarms it (no stranding), and the
replayed nonce is inert. The RED is causal and the focused suite is green.

VERDICT: PASS

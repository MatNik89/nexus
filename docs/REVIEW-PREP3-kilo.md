# PREP3 review round 1 — Kilo (chaos-kill harness)

Target: `slice/p0-prep3` at HEAD `6863248` (new `internal/acceptance/chaos_linux_test.go`). Method: code-read, run the harness repeatedly + the suite in the repo. Working tree never modified.

## Suite / harness runs

`CGO_ENABLED=0 go vet ./...` exit 0. The chaos harness is **flaky**: across my runs it passed at 17.9s / 23.4s / 34.4s and failed twice with `chaos_linux_test.go:150: chaos harness vacuous: nothing was admitted` (49.4s and 12.4s). The commit's "count=3 repeat clean" is not robust — I hit a false-negative in 2 of ~5 runs.

## Finding 1 — [MED] The fake bot's `getUpdates` drains-and-clears (no re-delivery), so the harness is flaky AND under-tests exactly-once admission

`newFakeBot`'s `/getUpdates` handler (`acceptance_linux_test.go:202-208`) returns **all** pending updates and clears the queue (`b.updates = nil`), ignoring the `offset` parameter entirely. Real Telegram keeps an update pending until `getUpdates` advances past its `update_id` — the whole point of B2's "durable inbox persists before the remote offset advances."

Consequence — two defects in one:

1. **Flaky false-negative**: a SIGKILL that lands in the window between the adapter's poll (which drained the queue) and the durable `Admit` loses the batch permanently; the next incarnation's poll returns an empty queue, so nothing is ever admitted and the `admitted == 0` vacuity guard fires. This is exactly the "nothing was admitted" failure I reproduced — a harness-model bug, not a daemon bug.
2. **Under-tested invariant**: because the bot never re-delivers, the chaos harness never exercises the "redelivered update dedups" path that I2's "exactly-once admission" is meant to prove. I2's `cnt == 1` (`chaos_linux_test.go:129`) is then schema-guaranteed rather than load-bearing: the `chan_inbox` UNIQUE index on (adapter_id, channel_identity, update_id) already makes one row per `update_id` impossible in this single-adapter/single-chat world, so `COUNT(*) == 1` can never fail — the check is vacuous as a test of the admission logic.

Fix: make the fake bot honor `offset` and keep un-acknowledged updates pending (or push a same-`update_id` redelivery after each incarnation), and assert `cnt == 1` against a redelivery that must dedup.

## Finding 2 — [LOW] `verifyJournalChain` under-binds the production journal

`verifyJournalChain` (`chaos_linux_test.go:217-244`) checks only the `integrity_prev_hash` linkage (`prevHash != prev`); it never recomputes each event's `integrity_hash` from its payload the way the production `journal.Replay` does (`journal.go:634-640`, `chainHash` recomputation). So I1 detects a torn chain but not a payload tamper that preserves the linkage. `TestChaosCheckerDetectsCorruption` tampers `integrity_prev_hash` only, so the checker's RED-capability is proven for linkage, not for content. For a kill-survival harness this is a reasonable simplification (SIGKILL cannot realistically tamper content while preserving the hash fields), but it is strictly weaker than the production integrity check.

## Finding 3 — [LOW] Kill/traffic overlap is statistical, not provable

The kill delay is `500 + rng.Intn(3000)` ms (`chaos_linux_test.go:106`) while the adapter polls every 2 s, with no synchronization, so a given kill may land before the update is even polled (not in flight). Random timing plus cycles gives statistical coverage, and the calm drain (`:112-119`) ensures survivors reach TERMINAL, but no per-kill assertion proves a message was mid-processing when the SIGKILL landed.

## What holds

The vacuity guards are real and correctly fail on an empty world (`admitted == 0` at `:149-151`, `n == 0` at `:240-242`, and I4's `ErrNoRows` on a missing reminder). I3 (marker `LIKE` with `%03d` padding) and I4 (stable delivery id + SENT-gated receipt) are meaningful and correctly at-most-once / honesty checks. The corruption checker is RED-capable for linkage tampering.

## Verdict

The harness is a genuine chaos-kill exerciser with meaningful durable-invariant checks, but its traffic model is broken in a way that is both flaky (reproduced false-negative) and under-testing (no re-delivery, so the core exactly-once-admission check is vacuous) — a confirmed defect in the deliverable.

VERDICT: FAIL

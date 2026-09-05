# PREP3 verification round 2 — Kilo (chaos harness re-review)

Target: `slice/p0-prep3` at HEAD `d8604b2` (replaces the r1 harness). Method: code-read, run the harness repeatedly in the repo. Working tree never modified.

## r1 findings — all fixed

| r1 finding | Status |
|---|---|
| [MED] fake bot `getUpdates` drained-and-cleared (no redelivery) → flaky "nothing admitted" + vacuous `cnt==1` | **FIXED** — `getUpdates` now honors `offset` (returns `id >= offset`, keeps pending until the offset passes; `acceptance_linux_test.go:210-228`), so a crash after fetch redelivers; I2 is now exact-set (`pushedIDs == inbox set`, `chaos_linux_test.go:216-243`). |
| [LOW] `verifyJournalChain` checked linkage only | **FIXED** — I1 runs `journal.Open` + `Replay(0)` with the full closed event set (`verifyJournalProduction`, `:394-440`), recomputing per-event hashes, parsing envelopes, checking denormalized columns; the corruption test covers terminal-hash / linkage / envelope-content / denormalized-column (all four RED-capable, verified passing). |
| [LOW] kill/traffic overlap statistical | **FIXED** — phased kills: phase A waits for durably-ADMITTED then kills (guaranteed in-flight, asserted `inFlightKills >= 3`), phase B waits for a durably-enqueued reply then kills, phase C random; readiness + live-process SIGKILL asserted per cycle (`:54-89`). |

## NEW findings

### 1. [MED] The harness is still flaky — I4 (reminder must DELIVERED) races the legitimate E9 UNKNOWN outcome

Reproduced: `chaos_linux_test.go:283: I4 reminder did not converge to DELIVERED: DELIVERY_PENDING` (80s timeout) in 1 of ~8 runs; the other runs are green (25-42s). Root cause: the outbox uses UNKNOWN-first parking — the flush parks a row UNKNOWN, sends, then marks SENT (`channel.go`), and a SIGKILL landing between the remote-accept and the local SENT mark leaves the row UNKNOWN. The reminder delivery loop's SENT gate (`deliverPendingReminders`) only checks `st == "SENT"` and never reconciles UNKNOWN (correct E9 "no blind auto-retry" — only the human `redeliver` resolves it). So a kill in that window legitimately strands the reminder at DELIVERY_PENDING, the 60s calm drain times out, and I4 fails.

This is a **harness** over-strictness, not a daemon bug: the daemon's UNKNOWN handling is correct, but I4 assumes the reminder *must* converge to DELIVERED without driving (or accepting) the human reconcile. The harness should either drive `redeliver <dlv-id>` for the stranded row or accept `DELIVERY_PENDING` (+ a UNKNOWN row) as an honest chaos outcome, asserting no duplicate rows and no fabricated receipt instead of unconditional DELIVERED.

### 2. [LOW] Tool-less scoping is an acceptable division of labor, with one uncovered boundary

The chaos conversation is tool-less (I3 = exactly-once reply, honest), and the effect path is carried by I4 (reminder through scheduler→outbox→SENT) plus the deterministic B7 unit seams — this is a defensible split: ASK effects need approval the harness cannot drive, and the effect COMMIT is atomic and seam-covered. The residual boundary is that a RANDOM (non-deterministic) kill landing *inside* a tool-bearing turn (between S7 grant-consume and effect commit, or between effect commit and `turn.succeeded`) is exercised nowhere — the harness is tool-less, the seams are deterministic. That specific window is the documented at-least-once ceiling, so it is accepted, but it is a real boundary to name.

### 3. [LOW] Calm-drain condition omits reply-SENT

The drain (`:195-203`) waits only for `inboxExactlyTerminal && oblStatus == "DELIVERED"`; it does not wait for the chaos success replies to reach `SENT`. I3's `succeeded < 3` floor counts `status='SENT'`, so the drain can in principle break with replies still PENDING and I3 would under-count. In practice phase B's three enqueued replies + the 60s deadline + 2s flush tick mask this, but it is a latent race the fixed-sleep-free drain does not fully close.

## Verdict

The r1 findings are genuinely fixed (offset-faithful transport, production verifier, exact-set admission, phased kills, honest tool-less scoping). But the harness remains intermittently red — I reproduced a real "reminder stuck DELIVERY_PENDING" failure whose root cause is I4's over-strict convergence requirement against the legitimate E9 UNKNOWN outcome. A flaky gate is a confirmed broken finding.

VERDICT: FAIL

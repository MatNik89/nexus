# PREP3 Review Round 1 — agy

**Target:** Repo `/home/matej/HARNESS/nexus`, branch `slice/p0-prep3`, HEAD `6863248` (`feat(prep3): chaos-kill harness over the real binary`).  
**Reviewer:** `agy` (Antigravity).  
**Scope:** Deep adversarial review and stress testing of the chaos-kill harness (`internal/acceptance/chaos_linux_test.go`). The working tree was never modified during verification.

---

## 1. Test Suite & Chaos Execution

- Repository full suite: `CGO_ENABLED=0 go test -p 1 -count=1 ./...` across all 34 packages: **PASS**, exit 0 (0 `FAIL` / `panic`).
- Chaos harness default run: `TestChaosKillSurvival` passed in 19.1s (`chaos: 8 cycles, 12 updates admitted, 12 markers, reminder=DELIVERED`).
- Chaos harness 20-cycle run (`NEXUS_CHAOS_CYCLES=20`): Passed in 44.4s, but exposed a message loss discrepancy: `chaos: 20 cycles, 31 updates admitted, 38 markers, reminder=DELIVERED` (7 pushed markers were lost without triggering a test failure).

---

## 2. Adversarial Analysis & Findings

### Finding 1: [HIGH] `fakeBot` Destructive `/getUpdates` Drops Interrupted Messages on Crash
- **Location:** [`internal/acceptance/acceptance_linux_test.go:202-208`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L202-L208)
- **Description:** In `newFakeBot`, the `/getUpdates` handler performs a destructive drain:
  ```go
  case strings.HasSuffix(r.URL.Path, "/getUpdates"):
      b.mu.Lock()
      batch := b.updates
      b.updates = nil
      b.mu.Unlock()
      out, _ := json.Marshal(map[string]any{"ok": true, "result": batch})
      rw.Write(out)
  ```
  In the real Telegram Bot API, `/getUpdates` is non-destructive; unacknowledged updates remain available until the client passes an advanced `offset`.
- **Attack / Impact:** When the daemon receives a batch of updates from `fakeBot` and is killed by `SIGKILL` before those updates are durably committed to SQLite `chan_inbox`, the updates are permanently lost because `fakeBot` already cleared its memory (`b.updates = nil`). On the next cycle, the restarted daemon queries `offset: 0`, but `fakeBot` has nothing to return. In a 20-cycle chaos run, 7 out of 38 pushed markers were permanently dropped at the harness boundary rather than exercising Telegram at-least-once offset redelivery.

---

### Finding 2: [HIGH] Premature Drain Loop Termination in Calm Final Incarnation
- **Location:** [`internal/acceptance/chaos_linux_test.go:114-118`](file:///home/matej/HARNESS/nexus/internal/acceptance/chaos_linux_test.go#L114-L118)
- **Description:** The calm final incarnation attempts to drain survivors using:
  ```go
  deadline := time.Now().Add(30 * time.Second)
  for time.Now().Before(deadline) {
      if pending := w.countInbox(t, "private", "ADMITTED"); pending == 0 {
          break
      }
      time.Sleep(500 * time.Millisecond)
  }
  time.Sleep(2 * time.Second)
  stopF()
  ```
- **Attack / Impact:** At $t=0$ of the calm incarnation, if no messages were left in `ADMITTED` state by the previous killed process (i.e. previously admitted messages finished and unpolled updates are still queued in the bot), `w.countInbox(t, "private", "ADMITTED")` returns `0` immediately on the very first iteration. The loop breaks at $t=0$, sleeps for only 2 seconds, and kills the daemon with `stopF()`. Any remaining unpolled messages in the bot are prematurely truncated rather than allowed to drain to completion.

---

### Finding 3: [MED] `verifyJournalChain` Omits Cryptographic Recomputation of Event Hashes
- **Location:** [`internal/acceptance/chaos_linux_test.go:217-244`](file:///home/matej/HARNESS/nexus/internal/acceptance/chaos_linux_test.go#L217-L244)
- **Description:** `verifyJournalChain` docstring claims to re-derive the integrity chain *exactly* as the production journal does. However, it only checks pointer linkage (`prevHash != prev`). It does not recompute `chainHash(chainInput(...))` over event fields (`journal_offset`, `event_id`, `run_id`, `sequence`, `rpv`, `prevHash`, `sealedRef`, `envelope`) as production `Journal.Replay` / `verifyChainFull` does ([`internal/kernel/journal/journal.go:637-640`](file:///home/matej/HARNESS/nexus/internal/kernel/journal/journal.go#L637-L640)).
- **Attack / Impact:** If an event's payload, event ID, or sequence is tampered or corrupted while retaining `integrity_prev_hash`, `verifyJournalChain` returns `nil` (false PASS). `TestChaosCheckerDetectsCorruption` only tests mutating `integrity_prev_hash`, masking this gap.

---

### Finding 4: [LOW] Under-Constrained Invariants I2 and I3 Allow Dropped Messages to Pass
- **Location:** [`internal/acceptance/chaos_linux_test.go:149-162`](file:///home/matej/HARNESS/nexus/internal/acceptance/chaos_linux_test.go#L149-L162)
- **Description:** 
  - Invariant I2 checks `admitted > 0` but does not assert `admitted == markers`.
  - Invariant I3 checks `if cnt > 1 { t.Fatalf(...) }` (at-most-once), but does not assert `cnt == 1` (exactly-once completion after calm drain).
- **Attack / Impact:** If a crash-recovery bug causes 50% of messages to be silently dropped, I2 passes (because `admitted > 0`) and I3 passes (because `cnt == 0 <= 1`). In combination with Finding 1, this allowed the 20-cycle run to lose 7 markers and still report `PASS`.

---

## 3. Top-3 Weakest Points

1. **[HIGH] `fakeBot` Lacks Telegram Offset Retention:** [`internal/acceptance/acceptance_linux_test.go:202-208`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L202-L208) — Destructive `b.updates = nil` wipes updates on `/getUpdates`, losing in-flight updates on crash instead of re-serving them from `req.Offset`.
2. **[HIGH] Calm Drain Loop Exits at $t=0$ on Empty `ADMITTED` State:** [`internal/acceptance/chaos_linux_test.go:114-118`](file:///home/matej/HARNESS/nexus/internal/acceptance/chaos_linux_test.go#L114-L118) — `countInbox == 0` triggers immediately before new updates are polled from the bot.
3. **[MED] `verifyJournalChain` Checks Only Hash Linkage, Not Payload Integrity:** [`internal/acceptance/chaos_linux_test.go:217-244`](file:///home/matej/HARNESS/nexus/internal/acceptance/chaos_linux_test.go#L217-L244) — Missing `chainHash(chainInput(...))` recomputation leaves payload corruption undetected by the external checker.

---

## 4. Verdict

While the chaos harness successfully launches the real daemon and exercises SIGKILL under live traffic, the harness has material defects:
- `fakeBot`'s destructive update drain drops in-flight messages upon crash.
- The calm incarnation drain loop exits prematurely at $t=0$.
- Invariants I2 and I3 are under-constrained, allowing message loss to pass undetected.
- `verifyJournalChain` omits full payload cryptographic recomputation.

VERDICT: FAIL

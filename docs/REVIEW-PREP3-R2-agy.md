# PREP3 Verification Round 2 — agy

Target: branch `slice/p0-prep3` at HEAD `d8604b2` (`fix(prep3-r1): fold prep3 review round 1 (all three reviewers)`).
Method: Adversarial review of harness modifications, verified in clean `git archive HEAD` temp exports (`/tmp/nexus-prep3-r2-probe`), with ablation testing and full repository test execution (`CGO_ENABLED=0 go test -p 1 -count=1 ./...`). Working tree was not modified during testing.

---

## 1. Verified Folds from Round 1

| Round 1 Issue | Implementation in `d8604b2` | Verification & Status |
|---|---|---|
| **Fake Bot `getUpdates` Destructive Clear** | `internal/acceptance/acceptance_linux_test.go:209-228`: `fakeBot` now retains unacknowledged updates (`id >= req.Offset`) instead of `b.updates = nil`. | **VERIFIED**: Crash-after-fetch redelivers unacknowledged updates. In clean export ablation (reverting to destructive clear), `TestChaosKillSurvival` turned decisively RED (`I2 update 1 not TERMINAL (ADMITTED/ADMITTED) after the drain`). |
| **I1 Journal Production Verifier** | `internal/acceptance/chaos_linux_test.go:391-440`: `verifyJournalProduction` uses `journal.Open` with closed payload validator set and `Replay(0)`, recomputing per-event hashes, parsing envelopes, and checking denormalized columns. | **VERIFIED**: `TestChaosCheckerDetectsCorruption` (:446-488) successfully catches all four corruption branches (`terminal-hash`, `prev-hash-linkage`, `envelope-content`, and `denormalized-column`). |
| **I2 Exact-Set Admission** | `internal/acceptance/chaos_linux_test.go:216-243`: Grouped query over `chan_inbox` asserting `seen == pushedIDs`, `COUNT(*) == 1`, and `MIN(status) == MAX(status) == 'TERMINAL'`. | **VERIFIED**: Replaced the under-constrained `MIN(status)`-only check with complete set equality. |
| **I3 Scoping to Tool-Less Ingress** | `internal/acceptance/chaos_linux_test.go:115-126`: Chaos turns avoid ASK tools that trigger un-drivable HITL suspensions. | **VERIFIED**: Conversational ingress/reply durability is honestly scoped; effect durability under kill is anchored by I4 reminder flow and unit B7 matrix. |
| **Phased Kills & Overlap Proof** | `internal/acceptance/chaos_linux_test.go:159-192`: Phase A waits for `ADMITTED > 0` before killing; Phase B waits for reply enqueue; Phase C runs random cycles. Overlap check requires `inFlightKills >= 3`. | **VERIFIED**: Guarantees deterministic kill/traffic overlap on the real daemon binary. |

---

## 2. Findings

### [CONCRETE][SEV: HIGH] I4 reminder convergence to `DELIVERED` races the legitimate E9 `UNKNOWN` outcome

- **Location**: `internal/acceptance/chaos_linux_test.go:195-203, 280-300` and `internal/channel/channel.go:488-525`
- **Mechanism**:
  During reminder delivery, `Core.Flush` parks outbox rows to `UNKNOWN` before sending, then marks them `SENT`. When a SIGKILL lands after `markParkingUnknown` but before `markSent` commits, the outbox row for `dlv-...` remains parked as `UNKNOWN`.
  Per PRD §HARDQ E9 / B2, the daemon correctly forbids automatic blind retry of `UNKNOWN` outbox rows (requiring human confirmation via `nexus telegram redeliver <dlv-id>`). Consequently, subsequent `deliverPendingReminders` ticks see `DeliveryStatus(ctx, dlvID) == "UNKNOWN" != "SENT"` and skip calling `MarkDelivered`.
  The harness in `TestChaosKillSurvival`, however, assumes that the reminder unconditionally reaches `oblStatus == "DELIVERED"` within the 60s calm drain.
- **Evidence**:
  Reproduced directly during testing:
  ```
  === RUN   TestChaosKillSurvival
      chaos_linux_test.go:283: I4 reminder did not converge to DELIVERED: DELIVERY_PENDING
  --- FAIL: TestChaosKillSurvival (81.28s)
  ```
- **Remediation**:
  The harness should either (a) drive the `redeliver <dlv-id>` command if the outbox row was parked `UNKNOWN` by a kill in that window, or (b) accept `DELIVERY_PENDING` with an un-duplicated `UNKNOWN` outbox row as an honest chaos outcome under E9.

---

### [CONCRETE][SEV: HIGH] I4 does not verify receipt correlation on the `obligation.delivered` event

- **Location**: `internal/acceptance/chaos_linux_test.go:285-300`
- **Mechanism**:
  I4 verifies that `w.oblStatus == "DELIVERED"` and separately checks that `chan_outbox` contains a row matching `wantDlv := chaosDeliveryIDFor("occ-rem-chaos#1")` with status `SENT`.
  However, it never inspects the actual `obligation.delivered` journal event payload to verify that `p.ReceiptID == wantDlv` and `p.Producer == "telegram"`.
  If a faulty implementation were to pass an arbitrary or blank receipt ID into `obl.MarkDelivered`, the outbox check and the obligation table status check would both pass while the durable receipt linkage required by PRD §HARDQ B5 is broken.
- **Evidence**:
  Ablation in clean export: mutating `cmd/nexus/main.go:1083` to pass `ReceiptID: "forged-id"` still allowed `TestChaosKillSurvival` to pass I4.
- **Remediation**:
  Inspect the `obligation.delivered` event during production replay or via projection query, asserting that `delivered_by == "telegram"` and `receipt_id == wantDlv`.

---

### [CONCRETE][SEV: MED] Stale `daemon.sock` causes false-positive readiness in `killableDaemon`

- **Location**: `internal/acceptance/chaos_linux_test.go:63-76`
- **Mechanism**:
  When `sigkill` kills the daemon process, the Unix domain socket file `daemon.sock` is left on disk. In `killableDaemon`, the readiness loop checks:
  ```go
  sock := filepath.Join(w.base, "nexus", "system", "daemon.sock")
  ready := false
  for i := 0; i < 300; i++ {
      if _, err := os.Stat(sock); err == nil {
          ready = true
          break
      }
      time.Sleep(25 * time.Millisecond)
  }
  ```
  On all cycles after the first kill, `os.Stat(sock)` succeeds on iteration 0 (0ms), declaring the daemon ready before the newly spawned process has initialized, opened its journal, or started listening.
- **Evidence**:
  `killableDaemon` returns immediately on cycle 2+ regardless of startup delay. In phase C (random kills), a kill can land before the new daemon process has even begun running its initialization code.
- **Remediation**:
  Remove any stale socket file before starting the daemon process, and/or verify readiness by establishing a Unix connection to the socket.

---

### [CONCRETE][SEV: MED] I3 oracle does not enforce per-message reply outcome for non-succeeded turns

- **Location**: `internal/acceptance/chaos_linux_test.go:244-270`
- **Mechanism**:
  The test comments state: *"never two replies of either kind for one message, and never zero rows for a TERMINAL message."*
  However, the loop only counts matching `status='SENT'` success replies (`ok == 1 => succeeded++`) and checks `okAll > 1` (rejecting duplicates). If a message reached `TERMINAL` as an error (e.g. killed during handler execution) or if an admitted message produced 0 rows (`okAll == 0`), no failure is triggered. The declared `errish` variable at line 252 is unused.
- **Evidence**:
  `errish` at `:252` is assigned but never evaluated (`_ = errish` at `:263`). If a message is lost without an outbox row, `okAll == 0` does not fail as long as aggregate `succeeded >= 3`.
- **Remediation**:
  For each admitted message, assert that exactly one outbox row exists: either the expected `reply-for chaos-msg-NNN` or the typed handler-error reply (`"I could not process that message..."`).

---

### [CONCRETE][SEV: MED] `waitStore` lacks retry handling for transient SQLite lock contention

- **Location**: `internal/acceptance/chaos_linux_test.go:149-158, 317-336`
- **Mechanism**:
  `waitStore` loops until a deadline, but the helper functions it calls (`countSentReplies`, `countInbox`) invoke `t.Fatal(err)` immediately on any query error.
  When the running daemon holds an active write transaction, a concurrent read query can encounter `database is locked (261)` or `busy`. Because `t.Fatal` is called immediately, the test fails immediately instead of retrying within the deadline.
- **Evidence**:
  Clean-export runs under load intermittently fail with `database is locked (261)` inside `countSentReplies` / `countInbox`.
- **Remediation**:
  Allow query helper functions to return errors to `waitStore`, ignoring transient locked/busy errors until the timeout expires.

---

## 3. Top 3 Weakest Points

1. **E9 `UNKNOWN` Outbox State under SIGKILL vs I4 Assertion**: The interaction between outbox `UNKNOWN` parking and reminder `DELIVERY_PENDING` convergence creates an intermittent test failure when a kill lands mid-flush.
2. **Receipt ID Linkage Gap**: I4 asserts obligation table status and outbox row presence independently, omitting the verification of `receipt_id` on the `obligation.delivered` journal event.
3. **Socket Existence Readiness Check**: Checking `os.Stat(sock)` after SIGKILL provides false-positive readiness due to un-cleaned stale socket files.

---

## 4. Test Suite Execution

- `CGO_ENABLED=0 go test -p 1 -count=1 ./...`: **PASS** (all packages green, 0 failures, 0 panics).
- `TestChaosCheckerDetectsCorruption`: **PASS** (all 4 corruption branches verified RED-capable).
- Multi-run / stress runs of `TestChaosKillSurvival`: Passes generally, but reproduces the intermittent `DELIVERY_PENDING` timeout failure caused by E9 outbox parking.

---

VERDICT: FAIL

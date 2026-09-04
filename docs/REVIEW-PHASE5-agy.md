# REVIEW-PHASE5 — Phase 5 Deep Review & Security Integration Review

**Target Ref:** `slice/p0-phase5` @ HEAD (`f63143c`)  
**Commits Covered:** `9606515` (T22 channel core), `63f2314` (T23 telegram adapter), `f63143c` (T24 durable remote HITL & daemon wiring)  
**Reviewer:** `agy` (Antigravity)  
**Scope:** `internal/channel/`, `internal/channel/telegram/`, `internal/approval/`, `internal/app/daemon/daemon.go`, `cmd/nexus/main.go`.

---

## 1. Executive Summary & Verification Matrix

| Area | Security Target | Status | Summary |
|---|---|---|---|
| **(a) Exactly-Once Admission** | Crash/Redelivery/Race double-effect | **[MED-FINDING]** | Dedup index in `chan_inbox` prevents double-effect on replay, but missing `TERMINAL` state transition causes dropped messages if daemon crashes between admission and reply. |
| **(b) Delivery Honesty** | Blind-retry & mark failure cascade | **[HIGH-FINDING]** | If `send(o)` succeeds but both `EvOutboundSent` and `EvOutboundUnknown` fail to append, the row remains `PENDING` and `Adapter.Run` blindly retries sending every 2s tick. |
| **(c) Profile Isolation (B3)** | Cross-profile leaks & bindings | **[OK]** | Strict deny-default check (`bound != a.profile`); outbox and inbox are physically separated per journal database; unauthenticated chats never admit. |
| **(d) Durable HITL (T24)** | Preimage, spoofing, expiry, wiring | **[MED-FINDING]** | Exact-intent hash check in `ConsumeApproval` is secure against truncation collisions; single-use and expiry checks are robust; however, the loop↔approval runtime suspension and resume wiring is not integrated in `RunChannelTurn`. |
| **(e) YOLO / Policy Containment (F2)** | Channel input bypass to YOLO / DENY | **[OK]** | `RunChannelTurn` hardcodes `effectpath.ModeDefault`; channel messages cannot enable YOLO; `DecisionDeny` is strict. |
| **(f) External Surface & Injections** | Token leak, offsets, prompt trust | **[HIGH-FINDING]** | Telegram transport errors wrap standard library `*url.Error` containing `bot<token>` in URL path, leaking the secret bot token in logs and probe output. |
| **(g) Ledger RED Conformance** | T22, T23, T24 test existence & causality | **[OK]** | All 22 named RED tests exist across `channel_test.go`, `telegram_test.go`, `approval_test.go`, and `main_test.go`, and are causal. |

---

## 2. Security Findings

### Finding 1: Secret Bot Token leaked in `*url.Error` upon network/transport failures
- **Severity:** `[HIGH]`
- **File:Line:** [`internal/channel/telegram/telegram.go:107-116`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L107-L116)
- **Description & Failure Scenario:**
  In `Adapter.call`:
  ```go
  httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
      a.base+"/bot"+a.token+"/"+method, bytes.NewReader(body))
  ...
  resp, err := a.client.Do(httpReq)
  if err != nil {
      return fmt.Errorf("telegram: transport: %w", err)
  }
  ```
  When `http.Client.Do` fails (e.g., DNS failure, network timeout, connection reset, proxy error, TLS failure), the Go standard library wraps the error in `*url.Error`, where `url.Error.URL` contains the full request URL (`https://api.telegram.org/bot<TOKEN>/<method>`).
  Because `fmt.Errorf("telegram: transport: %w", err)` preserves the inner error message, formatting this error leaks the raw Telegram Bot Token into:
  1. Daemon stderr logs via [`cmd/nexus/main.go:177`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L177) (`fmt.Fprintf(os.Stderr, "nexus daemon: telegram: %v\n", aerr)`).
  2. Doctor probe outputs via [`internal/channel/telegram/telegram.go:253`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L253) (`pr.Detail = err.Error()`).
  
  This violates C1/HARDQ credential secrecy rules. Transport errors must sanitize/mask the URL or redact `/bot<token>/` before propagating.

---

### Finding 2: Duplicate send and blind retry loop on dual sent-mark and unknown-mark failure
- **Severity:** `[HIGH]`
- **File:Line:** [`internal/channel/channel.go:414-423`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L414-L423) and [`internal/channel/telegram/telegram.go:236-239`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L236-L239)
- **Description & Failure Scenario:**
  In `Core.Flush`:
  ```go
  for _, o := range pending {
      if err := send(o); err != nil {
          return fmt.Errorf("channel: delivery %s failed (stays pending): %w", o.DeliveryID, err)
      }
      sentErr := c.mark(ctx, EvOutboundSent, o.DeliveryID)
      if sentErr != nil {
          if uerr := c.mark(ctx, EvOutboundUnknown, o.DeliveryID); uerr != nil {
              return fmt.Errorf("channel: delivery %s accepted remotely but neither sent nor unknown mark is durable — STOP AND RECONCILE: %w", o.DeliveryID, uerr)
          }
          return fmt.Errorf("channel: delivery %s accepted remotely but the sent-mark failed — parked UNKNOWN for reconciliation: %w", o.DeliveryID, sentErr)
      }
  }
  ```
  If `send(o)` succeeds (Telegram HTTP 200), but `c.mark(..., EvOutboundSent, ...)` fails (e.g. SQLite database locked or transient I/O failure) AND `c.mark(..., EvOutboundUnknown, ...)` also fails, `Flush` returns an error.
  However, the row in `chan_outbox` remains with `status='PENDING'` in the database.
  In `telegram.Adapter.Run`:
  ```go
  case <-t.C:
      a.PollOnce(ctx)
      a.FlushOutbox(ctx) // Error return is discarded!
  ```
  Because `FlushOutbox` discards the error, on the next tick (2 seconds later), `a.FlushOutbox` calls `Core.Flush` again. `c.Pending(ctx)` re-reads `chan_outbox WHERE status='PENDING'`, finds the exact same row `o`, and calls `send(o)` again, transmitting duplicate messages over Telegram.
  This violates HARDQ B2 delivery honesty: accepted deliveries must never be blindly retried.

---

### Finding 3: Incomplete `StateTerminal` lifecycle drops uncompleted messages on crash
- **Severity:** `[MED]`
- **File:Line:** [`internal/channel/channel.go:46-52, 198-204`](file:///home/matej/HARNESS/nexus/internal/channel/channel.go#L46-L52) and [`internal/channel/telegram/telegram.go:189-191`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram.go#L189-L191)
- **Description & Failure Scenario:**
  `StateTerminal` is defined in `channel.go:51` (`StateTerminal State = "TERMINAL"`), but is never updated or written to `chan_inbox`.
  When an update is admitted, `chan_inbox.status` is set to `ADMITTED`.
  If the daemon crashes or is killed while executing the conversation turn (`a.handle`) before `EnqueueReply` completes:
  1. On restart, Telegram redelivers the unacked update.
  2. `Adapter.processUpdate` calls `Core.Admit`.
  3. `Admit` queries `InboundStatus`, finds `status == "ADMITTED"`, and returns `AdmitOutcome{Replayed: true}`.
  4. `processUpdate` sees `outcome.Replayed == true` and immediately returns `nil` without running `a.handle` or checking for a completed reply.
  5. `PollOnce` advances `a.offset` past the update.
  
  The user's message is permanently dropped and ignored without any reply or error, because admission is conflated with successful execution.

---

### Finding 4: Missing loop↔approval runtime suspension and resumption wiring
- **Severity:** `[MED]`
- **File:Line:** [`cmd/nexus/main.go:458-496`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L458-L496), [`internal/app/daemon/daemon.go:207-234`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L207-L234), and [`internal/kernel/loop/loop.go:279-285`](file:///home/matej/HARNESS/nexus/internal/kernel/loop/loop.go#L279-L285)
- **Description & Failure Scenario:**
  While `approval.Store` implements the complete standalone storage lifecycle (`Suspend`, `Approve`, `Deny`, `ConsumeApproval`, `SuspendedTurn`), the integration into the daemon conversation turn is not wired:
  1. When a model turn requests a tool requiring confirmation (`DecisionAsk`), `loop.RunTurn` returns `ErrNeedsApproval`.
  2. `daemon.RunChannelTurn` returns the error to `telegram.Adapter.processUpdate`, which catches it and replies: `"I could not process that message. Try again, or check the daemon log."`
  3. `approval.Store.Suspend` is never called by the runtime loop or daemon, so no `ApprovalChallenge` is emitted to Telegram.
  4. When a user sends `approve <challenge_id>`, `telegramHandler` marks the challenge `APPROVED` in the store, but there is no listener or trigger to rehydrate `SuspendedTurn` and resume execution.

---

### Finding 5: Lineage/Provenance misattribution in `userBlock` for channel inputs
- **Severity:** `[LOW]`
- **File:Line:** [`internal/app/daemon/daemon.go:237-245`](file:///home/matej/HARNESS/nexus/internal/app/daemon/daemon.go#L237-L245)
- **Description & Failure Scenario:**
  In `daemon.go`, `userBlock` constructs a `ContextBlock` with hardcoded `Producer: "repl"` and `SourceURI: "nexus://repl"`.
  When `RunChannelTurn` constructs a user block for incoming Telegram messages, it uses `userBlock`, stamping Telegram messages with REPL provenance instead of channel/telegram provenance.

---

### Finding 6: Un-trimmed whitespace in `telegramBindings` parsing
- **Severity:** `[LOW]`
- **File:Line:** [`cmd/nexus/main.go:448`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L448)
- **Description & Failure Scenario:**
  In `telegramBindings()`, `pair[i+1:]` is extracted without `strings.TrimSpace`. A binding like `NEXUS_TELEGRAM_BINDINGS="123= work"` sets profile ID `" work"`, which fails validation (`ProfileID.Valid()`) during `telegram.New` startup.

---

## 3. Test Suite & Verification Results

- `CGO_ENABLED=0 go vet ./...`: Clean (exit code 0).
- `CGO_ENABLED=0 go test -count=1 ./...`: All 27 packages passed uncached (exit code 0).
- Ledger RED Verification:
  - T22 (Channel Core): All 8 tests verified present and causal in [`internal/channel/channel_test.go`](file:///home/matej/HARNESS/nexus/internal/channel/channel_test.go).
  - T23 (Telegram Adapter): All 6 tests verified present and causal in [`internal/channel/telegram/telegram_test.go`](file:///home/matej/HARNESS/nexus/internal/channel/telegram/telegram_test.go).
  - T24 (Durable Remote HITL): All 8 tests verified present and causal in [`internal/approval/approval_test.go`](file:///home/matej/HARNESS/nexus/internal/approval/approval_test.go) and [`cmd/nexus/main_test.go`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go).

---

VERDICT: FAIL

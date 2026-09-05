# REVIEW-T27-R3 — Phase 7 (T27) Round 3 Re-Verification & Final Gate

**Target Ref:** `slice/p0-phase7` @ HEAD (`6eab172c91a45fa3560738dcae0f215015b6fb89`)  
**Commits Covered:** `d154153`, `f5577ad`, `82feed8`, `6eab172` (folds of Round 2 findings from codex #1–3 and agy #1–3) on top of `5fedd4e`.  
**Reviewer:** `agy` (Antigravity)  
**Scope:** Verification of all Round-2 fold claims (codex #1–3, agy #1–3), verification of causal detectors in clean `git archive HEAD` trees via negative ablation probes, execution of full test suite across the repository, and audit for any new defects introduced in the diff `5fedd4e..HEAD`.  
**Working Tree Discipline:** All negative ablations were performed strictly within temporary clean exports (`/tmp/nexus-t27-r3-probe`). The working tree in `/home/matej/HARNESS/nexus` remained clean throughout.

---

## 1. Executive Summary & Verification Matrix

The Round 2 fold batch (`d154153`, `f5577ad`, `82feed8`, `6eab172`) resolves all remaining architectural findings:

1. **Pre-Seal Consumer Prevention (codex #1):** In [`cmd/nexus/main.go:169-186`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L169-L186), capability sealing (`liveProbes` + `Seal` + `snap.On("conversation")` fail-closed check) runs as the very first step in `runDaemon`. Heartbeat (`hb.Run`), startup scheduler sweep (`sched.Sweep`), periodic scheduler (`sched.Run`), startup approved-resume scan (`resumeApprovedPending`), reminder delivery loop (`deliverPendingReminders`), and Telegram loop (`tgAdapter.Run`) all execute strictly AFTER the capability snapshot is sealed `ON`. Causal detector [`TestSealedOffStartupRunsNoConsumers`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L902-L952) was verified.
2. **Signed Acceptance Attestation & Key Provenance (codex #2):** [`scripts/p0-accept.sh`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh#L17-L27) signs `acceptance.json` with the owner SSH key (`ssh-keygen -Y sign -n nexus-acceptance`). In [`cmd/nexus/main.go:874-899`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L874-L899), `verifyAcceptanceAttestation` verifies the signature against `<config>/nexus/allowed_signers` under the `nexus-acceptance` namespace. Causal detector [`TestDoctorP0GrantLive`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L793-L858) covers no-attestation, unsigned, rogue-key, and digest-mismatch branches.
3. **Private Chat Enclosure & Non-Positive Chat Refusal (codex #3):** [`telegramBindings`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L976-L981) rejects non-positive chat IDs (`chat <= 0`), preventing reminders from targeting Telegram groups/supergroups. Causal detector [`TestTelegramBindingsStrict`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L560-L577) was verified.
4. **Inline Receipt Settlement on Immediate Ack (agy #1):** [`telegramHandler`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1083-L1087) settles the SENT-proven receipt inline before invoking `MarkAcked`, eliminating the ticker race window. Causal detector [`TestAckSettlesSentReceiptInline`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1191-L1235) was verified in both directions.
5. **Doctor Probe Tmpdir Cleanup (agy #2):** [`mustProbeCore`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L903-L922) returns a cleanup callback invoked via `defer` in `runDoctorP0`.
6. **Dead Code Elimination (agy #3):** Legacy unused helper `telegramProbeGate` removed from production flow.

All 34 packages in the repository pass `go vet ./...` and `go test -count=1 ./...` cleanly in ~55s.

### Verification Matrix

| # | Dimension / Claim | Implementation | Verification & Causal Probe | Status |
|---|---|---|---|---|
| 1 | **Seal Before Effectful Consumers** (codex #1) | `liveProbes` + `Seal` + `snap.On("conversation")` runs prior to heartbeat, sweep, resume scan, delivery loop, or adapter ([`cmd/nexus/main.go:169-186`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L169-L186)). | Starting consumers before seal check turns [`TestSealedOffStartupRunsNoConsumers`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L902-L952) **RED**: `sealed-OFF startup ran the delivery consumer`. | **[OK] VERIFIED & CAUSAL** |
| 2 | **Signed Attestation & Provenance** (codex #2) | `p0-accept.sh` signs `acceptance.json` with `NEXUS_RELEASE_KEY`; `verifyAcceptanceAttestation` checks `ssh-keygen -Y verify` against `allowed_signers` ([`cmd/nexus/main.go:874-899`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L874-L899)). | Ablating signature check in `verifyAcceptanceAttestation` turns [`TestDoctorP0GrantLive`](file:///home/matej/HARNESS/nexus/internal/acceptance/acceptance_linux_test.go#L793-L858) **RED**: `unsigned attestation granted (exit 0)`. | **[OK] VERIFIED & CAUSAL** |
| 3 | **Group Routing Refusal (`chat <= 0`)** (codex #3) | `telegramBindings` strictly rejects `chat <= 0` ([`cmd/nexus/main.go:976-981`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L976-L981)). | Ablating `chat <= 0` check turns [`TestTelegramBindingsStrict`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L560-L577) **RED**: `malformed bindings "-100123=private" accepted silently`. | **[OK] VERIFIED & CAUSAL** |
| 4 | **Inline SENT-Proven Receipt Settlement on Ack** (agy #1) | `telegramHandler` settles proven receipt inline before calling `MarkAcked` ([`cmd/nexus/main.go:1083-1087`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1083-L1087)). | Ablating inline receipt check turns [`TestAckSettlesSentReceiptInline`](file:///home/matej/HARNESS/nexus/cmd/nexus/main_test.go#L1191-L1235) **RED**: `SENT-proven ack rejected in the race window`. | **[OK] VERIFIED & CAUSAL** |
| 5 | **Doctor Disposable Probe Cleanup** (agy #2) | `mustProbeCore` returns cleanup closure called via `defer probeCleanup()` ([`cmd/nexus/main.go:792-793`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L792-L793), [`903-922`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L903-L922)). | Verified disposable journal directory under `/tmp` is removed immediately on doctor exit. | **[OK] VERIFIED** |
| 6 | **Full-Suite Integrity & Regression Check** | Full repository test suite re-run with live sandbox and bwrap on host. | `CGO_ENABLED=0 go test -count=1 ./...` passed across all 34 packages. | **[OK] VERIFIED** |

---

## 2. Deep Review & Negative Ablation Logs

All negative ablation probes were executed against clean `git archive HEAD` trees in `/tmp/nexus-t27-r3-probe`.

### 1. Seal Before Effectful Consumers (`TestSealedOffStartupRunsNoConsumers`)
- **Code Audit:** In [`cmd/nexus/main.go:169-186`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L169-L186), `sealStartupSnapshot` evaluates all live subsystem probes. If `!snap.On("conversation")`, `runDaemon` logs the status reason and returns exit code 1 immediately. No background goroutines, schedulers, sweep actors, resume scans, or outbox delivery loops are spawned.
- **Negative Ablation:** Injected scheduler sweep, delivery loop, and Telegram adapter execution prior to the `if !snap.On("conversation")` seal gate in `cmd/nexus/main.go`.
- **Ablation Output:**
  ```text
  === RUN   TestSealedOffStartupRunsNoConsumers
      acceptance_linux_test.go:939: sealed-OFF startup ran the delivery consumer: "Reminder: sealed reminder (reply: ack occ-rem-seal#1)"
  --- FAIL: TestSealedOffStartupRunsNoConsumers (9.98s)
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/acceptance	12.796s
  ```
- **Verdict:** `[OK]` Verified causal.

### 2. Signed Acceptance Attestation (`TestDoctorP0GrantLive`)
- **Code Audit:** In [`cmd/nexus/main.go:874-899`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L874-L899), `verifyAcceptanceAttestation` requires:
  - `acceptance.json` exists, is valid JSON, and records `suite == "internal/acceptance"` and `passed == true`.
  - Binary digest matching `os.Executable()`.
  - Signature file `acceptance.json.sig` exists and validates via `ssh-keygen -Y verify -f <config>/nexus/allowed_signers -I owner -n nexus-acceptance`.
- **Negative Ablation:** Bypassed the `ssh-keygen -Y verify` signature check block in `verifyAcceptanceAttestation`.
- **Ablation Output:**
  ```text
  === RUN   TestDoctorP0GrantLive
      acceptance_linux_test.go:819: unsigned attestation granted (exit 0):
          LIVE conversation   live provider round trip
          LIVE memory         profile journals open, projections folded
          LIVE profiles       work and private journals independently open
          LIVE reminders      durable scheduler substrate ready
          LIVE telegram       live getMe passed
          LIVE sandbox        trust-rooted bwrap, enforcement canary passed
          LIVE acceptance     signed acceptance pass for this exact binary (2026-09-05T00:00:00Z)
          P0-capable — all six PRD §6 capabilities measured live AND the acceptance suite attested this exact binary
  --- FAIL: TestDoctorP0GrantLive (0.43s)
  FAIL
  FAIL	github.com/MatNik89/nexus/internal/acceptance	2.458s
  ```
- **Verdict:** `[OK]` Verified causal.

### 3. Telegram Group Binding Rejection (`TestTelegramBindingsStrict`)
- **Code Audit:** In [`cmd/nexus/main.go:976-981`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L976-L981), `telegramBindings` verifies `if chat <= 0` and returns an error rejecting non-private/group chat IDs.
- **Negative Ablation:** Commented out the `chat <= 0` check in `telegramBindings`.
- **Ablation Output:**
  ```text
  === RUN   TestTelegramBindingsStrict
      main_test.go:570: malformed bindings "-100123=private" accepted silently
  --- FAIL: TestTelegramBindingsStrict (0.00s)
  FAIL
  FAIL	github.com/MatNik89/nexus/cmd/nexus	0.004s
  ```
- **Verdict:** `[OK]` Verified causal.

### 4. Inline Receipt Settlement on Ack (`TestAckSettlesSentReceiptInline`)
- **Code Audit:** In [`cmd/nexus/main.go:1083-1087`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L1083-L1087), `telegramHandler` executes:
  ```go
  occ := strings.TrimSpace(text[len("ack "):])
  if st, serr := b.chanCore.DeliveryStatus(ctx, deliveryIDFor(occ)); serr == nil && st == "SENT" {
      b.obl.MarkDelivered(ctx, occ, obligation.DeliveryReceipt{
          Producer: "telegram", ReceiptID: deliveryIDFor(occ)})
  }
  ```
  This guarantees that if the message has been sent to the user's client, incoming ack processing immediately records `EvDelivered` before calling `b.obl.MarkAcked`, preventing the race condition where `MarkAcked` executes before the delivery loop ticker.
- **Negative Ablation:** Commented out the inline `MarkDelivered` settlement check in `telegramHandler`.
- **Ablation Output:**
  ```text
  === RUN   TestAckSettlesSentReceiptInline
      main_test.go:1213: SENT-proven ack rejected in the race window: "Ack failed: obligation: ack refused — no durable delivery receipt for occurrence occ-rem-race#1 (B5)"
  --- FAIL: TestAckSettlesSentReceiptInline (0.15s)
  FAIL
  FAIL	github.com/MatNik89/nexus/cmd/nexus	0.167s
  ```
- **Verdict:** `[OK]` Verified causal.

---

## 3. Top 3 Weakest Points

1. **Host-Level Key Storage Ceiling ([`scripts/p0-accept.sh:20-27`](file:///home/matej/HARNESS/nexus/scripts/p0-accept.sh#L20-L27), [`cmd/nexus/main.go:880-898`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L880-L898)):** Acceptance attestation provenance relies on the single-user SSH release key stored at `NEXUS_RELEASE_KEY` and trusted public key in `<config>/nexus/allowed_signers`. On a single-user system, this represents the standard trust root, but any process with local read access to the unencrypted private key could forge an attestation for a modified binary.
2. **Subprocess Invocation on Doctor Preflight ([`cmd/nexus/main.go:891-897`](file:///home/matej/HARNESS/nexus/cmd/nexus/main.go#L891-L897)):** `verifyAcceptanceAttestation` forks external `ssh-keygen -Y verify` binary on every `nexus doctor --p0` execution. While safe and hermetic, this depends on `/usr/bin/ssh-keygen` availability in PATH.
3. **Loopback Bot API Trust Assumption ([`internal/foundation/config/config.go:318-325`](file:///home/matej/HARNESS/nexus/internal/foundation/config/config.go#L318-L325)):** `ValidateBounds` permits loopback URLs (`127.0.0.1`, `localhost`, `[::1]`) for testing and local Bot API server instances, trusting that local loopback ports are not unauthenticated malicious services.

---

VERDICT: PASS

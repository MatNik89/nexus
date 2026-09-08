# Code Review: Telegram Egress Pinned-IP Dialer (Round 2, Slice `p1-tg-dialer`)

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-tg-dialer`
- **Reviewed Commit**: `9f5cb3faafa875fc889d82e75a1a85b3489de890`
- **Design Owner Doc**: `docs/PLAN-TG-EGRESS-DIALER.md`
- **Kernel Contract**: `docs/ARCHITECTURE-ESSENTIALS.md` E11 (`E11 — Fail-closed is the default answer to the unknown; egress is a real dialer boundary`)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This round-2 code review evaluates the updated implementation of the `p1-tg-dialer` slice following the round-1 findings:
- `internal/channel/telegram/dialer.go` (the pinned dialer, embedded v4 classifier, expanded deny floor, and fail-closed receipt emission)
- `internal/channel/telegram/telegram.go` (configuration threading for `EgressAllow`, client instantiation, and `egressReceipt`)
- `internal/channel/channel.go` (`EvEgressAttempt` payload validator with field invariant checks and `Core.RecordEgress`)
- `cmd/nexus/main.go` (`EgressAllow` configuration wiring)
- `internal/channel/telegram/dialer_test.go` and `telegram_test.go` (comprehensive RED-proven detector suite)

All round-1 findings have been fully implemented and verified against the codebase:
1. **F1 (Receipt Fail-Closed)**: On permitted dials, `DialContext` enforces that receipt journaling must succeed before initiating the TCP connection. If `a.core.RecordEgress` fails, the dial fails closed immediately.
2. **F2 (`egress_allow` Deny-Default)**: In production mode, `newPinnedClient` verifies that the API host is explicitly present in `egress_allow` at initialization time. Loopback override mode remains exempt.
3. **F3 (Coalescing Removed)**: Adapter-side coalescing has been removed. Every `DialContext` invocation journals an auditable receipt, while HTTP/2 connection pooling prevents excessive journal writes during steady-state polling.
4. **F4 (`egressPayload` Field Invariants & Resolved Set)**: `egressPayload` now records the complete `Resolved []string` address set, and the payload validator strictly enforces that allowed attempts specify `Pinned` while refused attempts specify `Reason`.
5. **F5 (Expanded Deny Floor)**: Address classification now recursively inspects IPv6 transition forms (NAT64 `64:ff9b::/96` and IPv4-compatible `::/96`), and blocks documentation/benchmarking ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `198.18.0.0/15`, and `2001:db8::/32`).

---

## 2. In-Depth Verification of Round-1 Folds

### 2.1. F1: Fail-Closed Egress Receipts

- **Implementation (`internal/channel/telegram/dialer.go:183-187`)**:
  ```go
  pinned := norm[0]
  // PERMITTED: the receipt must be durable BEFORE the connection happens.
  if err := d.emit(egressDecision{Host: host, Resolved: norm, Pinned: pinned, Allowed: true}); err != nil {
      return nil, fmt.Errorf("telegram egress: receipt not durable, refusing dial: %w", err)
  }
  return d.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
  ```
- **Error Propagation**: `a.egressReceipt` returns the error from `a.core.RecordEgress`, which propagates through `d.emit` and aborts the connection attempt if the journal write fails.
- **Detector**: `TestDialerReceiptFailClosed` verifies that if the receipt callback returns an error, the dialer returns an error and no TCP connection is established.

### 2.2. F2: Egress Allow Deny-Default

- **Implementation (`internal/channel/telegram/dialer.go:204-216`)**:
  ```go
  if !loopback {
      admitted := false
      for _, h := range egressAllow {
          if strings.EqualFold(strings.TrimSpace(h), host) {
              admitted = true
              break
          }
      }
      if !admitted {
          return nil, fmt.Errorf("telegram: api host %q is not in egress_allow (deny-default)", host)
      }
  }
  ```
- **Wiring (`internal/channel/telegram/telegram.go:46, 97`, `cmd/nexus/main.go:163, 848`)**:
  `telegram.Config` carries `EgressAllow: resolved.Config.EgressAllow`, ensuring the configured allowlist is enforced at adapter startup.
- **Detector**: `TestPinnedClientEgressAllowDenyDefault` confirms that unlisted production hosts fail at client construction, admitted hosts succeed, and loopback overrides remain functional without allowlist entries.

### 2.3. F3: Direct Per-Dial Receipts

- Adapter-side caching (`lastPinned`, `egressMu`) has been removed. Every TCP dial performed by `http.Transport` issues a fresh receipt.
- Steady-state getUpdates polling reuses established HTTP/2 connections, eliminating unnecessary DNS lookups and journal spamming during normal operation.

### 2.4. F4: Payload Invariants & Resolved Set Capture

- **Payload Schema (`internal/channel/channel.go:202-208`)**:
  ```go
  type egressPayload struct {
      Host     string   `json:"host"`
      Resolved []string `json:"resolved,omitempty"`
      Pinned   string   `json:"pinned,omitempty"`
      Allowed  bool     `json:"allowed"`
      Reason   string   `json:"reason,omitempty"`
  }
  ```
- **Validator (`internal/channel/channel.go:175-192`)**:
  - Requires non-empty `Host`.
  - Rejects `Allowed == true` when `Pinned == ""`.
  - Rejects `Allowed == false` when `Reason == ""`.

### 2.5. F5: Expanded Address Classification & Transition Form Unwrapping

- **NAT64 & IPv4-Compatible Extraction (`internal/channel/telegram/dialer.go:67-94, 120-125`)**:
  - `embeddedV4` extracts embedded IPv4 addresses from `64:ff9b::/96` and `::/96` (excluding `::` and `::1`).
  - Extracted IPv4 addresses are recursively evaluated via `d.permitted(e)`, ensuring metadata (`169.254.169.254`) or private addresses encoded inside IPv6 transition headers are blocked.
- **Documentation & Benchmarking Ranges (`internal/channel/telegram/dialer.go:96-112, 136-144`)**:
  - Blocks `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` (RFC 5737), `198.18.0.0/15` (RFC 2544), `100.64.0.0/10` (CGNAT), and `2001:db8::/32`.
- **Detector**: `TestDialerRejectsNAT64Metadata` proves that `64:ff9b::a9fe:a9fe` (NAT64 encoding of `169.254.169.254`) is refused.

---

## 3. Top-3 Weakest Points / Implementation Notes

In accordance with review discipline, the following observations and minor points are documented:

1. **Refusal Emission Error Handling**:
   - *Observation*: In `internal/channel/telegram/dialer.go:159-162`, `refuse` discards the error from `_ = d.emit(...)`.
   - *Detail*: This is intentional and safe: for blocked dials, the connection is already prevented from proceeding. Failing to journal a refusal should not accidentally permit a connection. Permitted dials (`Allowed: true`), by contrast, strictly check `if err := d.emit(...); err != nil { return nil, err }`.
2. **Context Cancellation on Receipts**:
   - *Observation*: `a.egressReceipt` (`internal/channel/telegram/telegram.go:110-121`) uses `context.Background()` when calling `RecordEgress`.
   - *Detail*: This ensures that if the per-request HTTP dial context is cancelled near timeout, the local journal append does not abort mid-write.
3. **HTTP/2 Idle Connection Maintenance**:
   - *Observation*: `newPinnedClient` sets `MaxIdleConns: 10` and `IdleConnTimeout: 90 * time.Second`.
   - *Detail*: With default Telegram long polling (timeout 0 or active poll loop every few seconds), the single persistent TCP connection stays alive, so receipts are logged only on initial dial and on connection reconnects.

---

## 4. Test Suite Verification & Detector Adequacy

- All unit tests in `internal/channel/telegram` and `internal/channel` pass cleanly:
  - `TestDialerProductionAllPublicPins`: Verifies public IP pinning.
  - `TestDialerRejectsMetadataAndMappedForm`: Verifies plain and IPv4-mapped metadata rejection.
  - `TestDialerProductionDenyFloor`: Table test over 8 non-public address classes.
  - `TestDialerLoopbackMode`: Verifies loopback mode and mixed-set rejection.
  - `TestDialerHostNotPermitted`: Verifies off-policy host rejection.
  - `TestPinnedClientNoProxyNoRedirect`: Verifies proxy and redirect rejection.
  - `TestPinnedClientModeFromBase`: Verifies mode selection from base URL.
  - `TestDialerRejectsNAT64Metadata`: Verifies NAT64-embedded metadata rejection.
  - `TestDialerReceiptFailClosed`: Verifies permitted dials fail closed on journal receipt errors.
  - `TestDialerSequentialRebind`: Verifies dynamic rebinding detection across sequential calls.
  - `TestPinnedClientEgressAllowDenyDefault`: Verifies deny-default allowlist checks.
- Full repository test suite passes with 0 failures (`go test ./...` 0 FAIL).

---

VERDICT: PASS

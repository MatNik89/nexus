# Code Review: Telegram Egress Pinned-IP Dialer (Round 3, Slice `p1-tg-dialer`)

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-tg-dialer`
- **Reviewed Commit**: `29d53fc8016e56135709ca27be127354e47ea81b`
- **Design Owner Doc**: `docs/PLAN-TG-EGRESS-DIALER.md`
- **Kernel Contract**: `docs/ARCHITECTURE-ESSENTIALS.md` E11 (`E11 — Fail-closed is the default answer to the unknown; egress is a real dialer boundary`)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This round-3 code review evaluates the updated implementation of the `p1-tg-dialer` slice following the round-2 findings:
- `internal/channel/telegram/dialer.go` (`errEgressPreWire` sentinel, `forbiddenSpecialUse` prefix table, refusal receipt-error joining, and fail-closed permitted dials)
- `internal/channel/telegram/telegram.go` (`isPreWire` error classification recognizing `errEgressPreWire`)
- `internal/channel/channel.go` (`EvEgressAttempt` payload validator and `Core.RecordEgress`)
- `internal/channel/telegram/dialer_test.go` (new tests: `TestDialerRejectsRFC8215Local`, `TestEgressRefusalIsPreWire`, `TestPinnedClientTLSVerificationOn`, `TestEgressReceiptJournaled`)

All round-2 folds have been verified in the code:
1. **F2 (Definite Pre-Wire Classification via `errEgressPreWire`)**: The sentinel error `errEgressPreWire` wraps every dialer refusal and non-durable receipt error. `isPreWire()` in `telegram.go` inspects it via `errors.Is`, ensuring the channel outbox re-pends `PENDING` instead of erroneously parking rows in `UNKNOWN`.
2. **F3 (Consolidated `forbiddenSpecialUse` Prefix Table)**: Non-stdlib address classes are consolidated into a single prefix slice (`forbiddenSpecialUse`) including RFC8215 `64:ff9b:1::/48` local-use translation, CGNAT (`100.64.0.0/10`), documentation prefixes (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`), and benchmarking (`198.18.0.0/15`).
3. **F1 (Refusal Receipt Append Error Joined)**: If journal append fails during a refusal, the error is joined into the returned error rather than silently dropped, preserving the error context while maintaining `errEgressPreWire` classification.
4. **F4 (TLS Verification & Journal Egress Receipt Detectors)**: `TestPinnedClientTLSVerificationOn` proves untrusted certificates are rejected (no `InsecureSkipVerify` bypass), and `TestEgressReceiptJournaled` proves a complete, token-free receipt is durably journaled on a live poll.

---

## 2. In-Depth Verification of Round-2 Folds

### 2.1. F2: Pre-Wire Classification & Outbox State Preservation

- **Sentinel Definition & Propagation (`internal/channel/telegram/dialer.go:27, 160-166, 188-190`)**:
  ```go
  var errEgressPreWire = errors.New("telegram egress refused pre-wire")
  ```
  Both policy refusals and non-durable receipt errors wrap `errEgressPreWire`.
- **Classification in Telegram Adapter (`internal/channel/telegram/telegram.go:145-154`)**:
  ```go
  func isPreWire(err error) bool {
      if errors.Is(err, errEgressPreWire) {
          return true
      }
      var dnsErr *net.DNSError
      if errors.As(err, &dnsErr) {
          return true
      }
      return false
  }
  ```
- **Outbox Recovery Impact**: When `call()` fails with a pre-wire error, `Flush()` safely re-pends the outbound delivery as `PENDING` (safe for retry) because no data ever left the process.
- **Detector**: `TestEgressRefusalIsPreWire` tests a `url.Error` wrapping an `errEgressPreWire` error and asserts `isPreWire()` returns `true`.

### 2.2. F3: Consolidated `forbiddenSpecialUse` Table

- **Table Definition (`internal/channel/telegram/dialer.go:33-41`)**:
  ```go
  var forbiddenSpecialUse = []netip.Prefix{
      netip.MustParsePrefix("100.64.0.0/10"),   // RFC6598 CGNAT
      netip.MustParsePrefix("192.0.2.0/24"),    // RFC5737 documentation
      netip.MustParsePrefix("198.51.100.0/24"), // RFC5737 documentation
      netip.MustParsePrefix("203.0.113.0/24"),  // RFC5737 documentation
      netip.MustParsePrefix("198.18.0.0/15"),   // RFC2544 benchmarking
      netip.MustParsePrefix("2001:db8::/32"),   // RFC3849 documentation
      netip.MustParsePrefix("64:ff9b:1::/48"),  // RFC8215 local-use translation
  }
  ```
- **Evaluation (`internal/channel/telegram/dialer.go:140-144`)**:
  `permitted()` loops over `forbiddenSpecialUse` using `p.Contains(a)`.
- **Detector**: `TestDialerRejectsRFC8215Local` verifies that addresses under `64:ff9b:1::/48` are refused in production mode.

### 2.3. F1: Refusal Receipt Error Joining

- **Implementation (`internal/channel/telegram/dialer.go:159-166`)**:
  ```go
  refuse := func(reason string, resolved []netip.Addr) (net.Conn, error) {
      if aerr := d.emit(egressDecision{Host: host, Resolved: resolved, Reason: reason}); aerr != nil {
          return nil, fmt.Errorf("telegram egress: %s (receipt append failed: %v): %w", reason, aerr, errEgressPreWire)
      }
      return nil, fmt.Errorf("telegram egress: %s: %w", reason, errEgressPreWire)
  }
  ```
  If journal append fails when recording a refusal, the returned error reports both the refusal reason and the journal append error without dropping diagnostic evidence.

### 2.4. F4: TLS Certificate Verification & Journal Receipt Detectors

- **TLS Verification (`internal/channel/telegram/dialer_test.go:179-204`)**:
  `TestPinnedClientTLSVerificationOn` establishes a local TLS test server with an untrusted certificate and verifies that `newPinnedClient` fails handshake with a certificate verification error, proving `InsecureSkipVerify` is not enabled and certificate validation is active.
- **Journal Receipt (`internal/channel/telegram/dialer_test.go:206-240`)**:
  `TestEgressReceiptJournaled` runs `PollOnce` against a real journal, replays the journal events, verifies `EvEgressAttempt` exists, ensures bot tokens are not leaked in the payload, and confirms `Pinned != ""` and `len(Resolved) > 0`.

---

## 3. Top-3 Weakest Points / Implementation Notes

In accordance with review discipline, the following observations and minor points are documented:

1. **`Unmap()` Prior to Prefix Matching**:
   - *Observation*: In `internal/channel/telegram/dialer.go:123`, `a = a.Unmap()` is called before `permitted()` tests prefixes.
   - *Detail*: This ensures that IPv4-mapped IPv6 addresses (e.g. `::ffff:192.0.2.1`) are converted to 4-byte IPv4 addresses before matching against IPv4 prefixes in `forbiddenSpecialUse` (`192.0.2.0/24`, `100.64.0.0/10`), ensuring clean prefix containment.
2. **Deterministic Pinned Selection**:
   - *Observation*: `d.DialContext` selects `pinned := norm[0]`.
   - *Detail*: When multiple public IPs are resolved (e.g., dual-stack or multiple A records), the entire set must pass `permitted()` before `norm[0]` is dialed. If dynamic rebinding returns a different first IP on reconnect, the new IP is evaluated and journaled.
3. **HTTP/2 Connection Longevity**:
   - *Observation*: With `MaxIdleConns: 10` and `IdleConnTimeout: 90s`, `http.Transport` reuses the active TCP connection.
   - *Detail*: `DialContext` is invoked only when establishing a new connection. This keeps journal receipts proportional to network connection lifecycles rather than HTTP poll requests.

---

## 4. Test Suite Verification & Detector Adequacy

- All unit tests in `internal/channel/telegram` and `internal/channel` pass cleanly:
  - `TestDialerProductionAllPublicPins`: Public IP pinning.
  - `TestDialerRejectsMetadataAndMappedForm`: Metadata plain and mapped rejection.
  - `TestDialerProductionDenyFloor`: Public deny floor table test.
  - `TestDialerLoopbackMode`: Loopback mode acceptance and mixed-set rejection.
  - `TestDialerHostNotPermitted`: Non-API host rejection.
  - `TestPinnedClientNoProxyNoRedirect`: Ambient proxy and redirect rejection.
  - `TestPinnedClientModeFromBase`: Base URL mode selection.
  - `TestDialerRejectsNAT64Metadata`: NAT64-embedded metadata rejection.
  - `TestDialerReceiptFailClosed`: Permitted dial failure on receipt append error.
  - `TestDialerSequentialRebind`: Rebind rejection across sequential calls.
  - `TestPinnedClientEgressAllowDenyDefault`: Deny-default allowlist checks.
  - `TestDialerRejectsRFC8215Local`: RFC8215 local-use prefix rejection.
  - `TestEgressRefusalIsPreWire`: Pre-wire error classification.
  - `TestPinnedClientTLSVerificationOn`: Active TLS certificate verification.
  - `TestEgressReceiptJournaled`: Journal receipt integrity and token sanitization.
- Full repository test suite passes with 0 failures (`go test ./...` 0 FAIL).

---

VERDICT: PASS

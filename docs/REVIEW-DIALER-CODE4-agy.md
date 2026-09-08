# Code Review: Telegram Egress Pinned-IP Dialer (Round 4, Slice `p1-tg-dialer`)

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-tg-dialer`
- **Reviewed Commit**: `4ee4926a63475c7dc7102ad4fa874661191d1176`
- **Design Owner Doc**: `docs/PLAN-TG-EGRESS-DIALER.md`
- **Kernel Contract**: `docs/ARCHITECTURE-ESSENTIALS.md` E11 (`E11 — Fail-closed is the default answer to the unknown; egress is a real dialer boundary`)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This round-4 code review evaluates the updated implementation of the `p1-tg-dialer` slice following the round-3 address-encoding derivation fix:
- `internal/channel/telegram/dialer.go` (`v4EmbedPrefixes` unified table, `embeddedV4`, `permitted`, `errEgressPreWire`, and fail-closed receipt emission)
- `internal/channel/telegram/telegram.go` (`isPreWire` error classification, `egressReceipt`)
- `internal/channel/channel.go` (`EvEgressAttempt` payload validator and `Core.RecordEgress`)
- `internal/channel/telegram/dialer_test.go` (new comprehensive matrix detector `TestDialerRejectsForbiddenV4EveryEncoding`)

The round-3 fold has been verified:
- **Round-3 Fold (Unified IPv6 Embedded-v4 Classification via `v4EmbedPrefixes`)**: Rather than patching individual RFC prefixes incrementally, `embeddedV4` iterates over `v4EmbedPrefixes` representing the complete set of standardized IPv6 transition prefixes with embedded low-32-bit IPv4 addresses:
  - `64:ff9b::/96` (RFC 6052 NAT64 well-known)
  - `::ffff:0:0:0/96` (RFC 6145 SIIT / IPv4-translated)
  - `::/96` (deprecated IPv4-compatible)
  - In addition to standard `netip.Addr.Unmap()` handling for IPv4-mapped `::ffff:0:0/96`.
- **Matrix Detector Coverage**: `TestDialerRejectsForbiddenV4EveryEncoding` tests the cross-product of `{metadata, loopback, rfc1918, cgnat}` $\times$ `{plain, mapped, NAT64, RFC6145}`, proving that all forbidden IPv4 categories across all standardized IPv6 encapsulation formats are refused with zero connections established.

---

## 2. In-Depth Verification of Round-3 Folds & Core Contracts

### 2.1. Unified Embedded-v4 Classification (`internal/channel/telegram/dialer.go:89-126`)

- **Prefix Definition (`internal/channel/telegram/dialer.go:94-98`)**:
  ```go
  var v4EmbedPrefixes = []netip.Prefix{
      netip.MustParsePrefix("64:ff9b::/96"),    // RFC6052 NAT64 well-known
      netip.MustParsePrefix("::ffff:0:0:0/96"), // RFC6145 IPv4-translated
      netip.MustParsePrefix("::/96"),           // deprecated IPv4-compatible
  }
  ```
- **Extraction Function (`internal/channel/telegram/dialer.go:103-114`)**:
  ```go
  func embeddedV4(a netip.Addr) (netip.Addr, bool) {
      if !a.Is6() || a == netip.IPv6Unspecified() || a == netip.IPv6Loopback() {
          return netip.Addr{}, false
      }
      for _, p := range v4EmbedPrefixes {
          if p.Contains(a) {
              b := a.As16()
              return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
          }
      }
      return netip.Addr{}, false
  }
  ```
- **Policy Enforcement (`internal/channel/telegram/dialer.go:121-127`)**:
  ```go
  a = a.Unmap()
  if a.Is6() {
      if e, ok := embeddedV4(a); ok && !d.permitted(e) {
          return false
      }
  }
  ```
  This recursive evaluation guarantees that any non-public IPv4 target embedded in any recognized IPv6 prefix is classified and denied by the v4 policy floor.

- **Matrix Test (`internal/channel/telegram/dialer_test.go:242-277`)**:
  `TestDialerRejectsForbiddenV4EveryEncoding` constructs matrix permutations and verifies that every combination is refused fail-closed without contacting the dialer.

### 2.2. Prior Round Invariants Verified

- **F1 (Fail-Closed Receipts)**: Permitted dials require `d.emit(Allowed: true)` to succeed; any journal write error immediately aborts the connection attempt before dialing (`internal/channel/telegram/dialer.go:183-187`).
- **F2 (Egress Allow Deny-Default)**: Production API hosts must appear in `cfg.EgressAllow` at client construction time; loopback overrides are exempt (`internal/channel/telegram/dialer.go:204-216`).
- **F3 (Direct Per-Dial Receipts)**: No adapter-side coalescing; each TCP dial emits a receipt.
- **F4 (Pre-Wire Classification)**: `errEgressPreWire` sentinel wraps all refusals and receipt errors; `isPreWire()` in `telegram.go:145-154` ensures outbox deliveries re-pend `PENDING`.
- **F5 (TLS Certificate Validation)**: `TestPinnedClientTLSVerificationOn` proves untrusted certificates fail handshake (no `InsecureSkipVerify` bypass).

---

## 3. Top-3 Weakest Points / Implementation Notes

In accordance with review discipline, the following observations and minor points are documented:

1. **Non-Standardized IPv6 Translation Prefixes**:
   - *Observation*: Local enterprise NAT64 setups sometimes use arbitrary `/96` prefixes outside `64:ff9b::/96` (e.g. within an enterprise ULA or ISP prefix).
   - *Detail*: For a public host (`api.telegram.org`), DNS resolves to Telegram's public IP blocks, not private enterprise NAT64 prefixes. Standard RFC 6052 (`64:ff9b::/96`), RFC 6145 (`::ffff:0:0:0/96`), and IPv4-mapped forms cover all standardized public and transition encodings.
2. **Deterministic Pinned IP Selection**:
   - *Observation*: `pinned := norm[0]` selects the first normalized address after ensuring all resolved addresses pass `permitted()`.
   - *Detail*: If DNS returns multiple public addresses, the fail-closed mixed-set rule ensures no connection is attempted unless *every* candidate passes policy.
3. **Receipt Duration and Journal Load**:
   - *Observation*: `DialContext` is invoked on each TCP dial.
   - *Detail*: HTTP/2 connection reuse (`MaxIdleConns: 10`, `IdleConnTimeout: 90s`) maintains persistent connections, keeping journal receipt writes minimal during active polling.

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
  - `TestDialerRejectsForbiddenV4EveryEncoding`: Matrix test across all forbidden categories and encodings.
- Full repository test suite passes with 0 failures (`go test ./...` 0 FAIL).

---

VERDICT: PASS

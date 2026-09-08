# Code Review: Telegram Egress Pinned-IP Dialer (Slice `p1-tg-dialer`)

- **Repository**: `/home/matej/HARNESS/nexus`
- **Branch**: `slice/p1-tg-dialer`
- **Reviewed Commit**: `a5645387ee9d913368598bdc323cc20c5b621d33`
- **Design Owner Doc**: `docs/PLAN-TG-EGRESS-DIALER.md`
- **Kernel Contract**: `docs/ARCHITECTURE-ESSENTIALS.md` E11 (`E11 — Fail-closed is the default answer to the unknown; egress is a real dialer boundary`)
- **Reviewer**: Antigravity (`agy`)

---

## 1. Executive Summary

This code review evaluates the implementation of the Telegram pinned-IP egress dialer across:
- `internal/channel/telegram/dialer.go` (the pinned dialer, address classifier, and `newPinnedClient`)
- `internal/channel/telegram/telegram.go` (`New()` client initialization and `egressReceipt` coalescing)
- `internal/channel/channel.go` (`EvEgressAttempt` payload validator, event constant, and `Core.RecordEgress`)
- `internal/channel/telegram/dialer_test.go` and `telegram_test.go` (contract detectors and regression coverage)

The code faithfully implements the approved design in `docs/PLAN-TG-EGRESS-DIALER.md` and enforces the locked E11 egress boundary:
1. **Address Policy & Deny Floor**: Normalized with `netip.Addr.Unmap()`, the public deny floor rejects loopback, link-local (covering cloud metadata `169.254.169.254` in both plain and mapped forms), private networks, CGNAT, and ULA. Mode-sensitive partitioning cleanly handles loopback overrides without weakening production constraints. The mixed-set rule fails closed.
2. **TLS Verification Integrity**: `DialContext` returns a raw TCP `net.Conn` to the literal IP; `http.Transport` performs TLS negotiation and certificate validation against the original hostname (`ServerName`).
3. **Proxy & Redirect Containment**: `Transport.Proxy = nil` ignores ambient proxy variables, and `CheckRedirect` unconditionally aborts redirects.
4. **Coalesced Egress Receipts**: Refusals are logged unconditionally on every attempt; permitted connects are logged on first connect and on any pinned IP change, protected by mutex synchronization and decoupled from request cancellation deadlines.
5. **Detector Adequacy**: All unit tests are non-vacuous, contract-anchored, and verify fail-closed semantics across all failure modes. Full repository test suite passes clean (`go test ./...` 0 FAIL).

---

## 2. In-Depth Code & Contract Audit

### 2.1. Address Normalization, Deny Floor & Mixed-Set Rule

- **Normalization (`internal/channel/telegram/dialer.go:107-110`)**:
  ```go
  norm := make([]netip.Addr, 0, len(addrs))
  for _, a := range addrs {
      norm = append(norm, a.Unmap())
  }
  ```
  Calling `Unmap()` unwraps `::ffff:a.b.c.d` representations to standard 4-byte IPv4 addresses, ensuring that IPv4-mapped IPv6 encodings of forbidden addresses (such as `::ffff:169.254.169.254`) are evaluated against IPv4 classification rules.

- **Production Deny Floor (`internal/channel/telegram/dialer.go:71-85`)**:
  - `a.IsLoopback() || a.IsUnspecified()` rejects `127.0.0.0/8`, `::1`, `0.0.0.0`, and `::`.
  - `a.IsMulticast() || a.IsInterfaceLocalMulticast()` rejects multicast traffic.
  - `a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast()` rejects link-local ranges (`169.254.0.0/16` and `fe80::/10`), including cloud metadata endpoints (`169.254.169.254`).
  - `a.IsPrivate()` rejects RFC 1918 (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) and IPv6 ULA (`fc00::/7`).
  - CGNAT (`100.64.0.0/10`) is explicitly checked via `b[0] == 100 && b[1] >= 64 && b[1] <= 127` on IPv4 addresses.

- **Mode-Sensitive Policy (`internal/channel/telegram/dialer.go:34-47, 68-70`)**:
  - `egressModeForBase` inspects the configured `telegram_api_base` (already bounded by `config.ValidateBounds` to either `https://api.telegram.org` or a verified loopback URL).
  - In loopback mode, `permitted()` returns `a.IsLoopback()`, requiring all resolved IPs to be loopback.
  - In production mode, `permitted()` enforces the full public deny floor.

- **Mixed-Set Rule (`internal/channel/telegram/dialer.go:115-120`)**:
  - The loop iterates over all normalized addresses. If *any* address returns `!d.permitted(a)`, `DialContext` immediately emits a refusal receipt and returns an error without connecting.

### 2.2. TLS Handshake & SNI Preservation

- In `newPinnedClient` (`internal/channel/telegram/dialer.go:148-161`):
  `http.Transport` is configured with `DialContext: d.DialContext`.
- `d.DialContext` connects the underlying raw TCP socket to `net.JoinHostPort(pinned.String(), port)` and returns the resulting `net.Conn`.
- `http.Transport` performs TLS client negotiation on the returned `net.Conn` using the hostname from the request URL (`api.telegram.org`).
- TLS SNI (`ServerName`) and x509 certificate chain validation are performed against `api.telegram.org`, not the literal IP. Dialing the pinned IP does not degrade TLS security.

### 2.3. Proxy Sanitization & Redirect Rejection

- `Transport.Proxy = nil` (`internal/channel/telegram/dialer.go:154`):
  Overrides Go's default `http.ProxyFromEnvironment`, ensuring ambient environment variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`) are ignored.
- `CheckRedirect` (`internal/channel/telegram/dialer.go:165-167`):
  Returns an error on any 3xx redirection, preventing outbound request redirection.

### 2.4. Egress Receipts & Coalescing

- **Receipt Structure & Event Validation (`internal/channel/channel.go:175-212`)**:
  - `EvEgressAttempt` payload schema (`egressPayload{Host, Pinned, Allowed, Reason}`) requires non-empty `Host` and validates against `journal.PayloadValidator`.
  - Appended via `c.j.Append` through the journal actor, maintaining B7 single-write-owner invariants.
  - Token is omitted from payloads.
- **Coalescing Discipline (`internal/channel/telegram/telegram.go:110-130`)**:
  - **Refusals**: `if !d.Allowed` unconditionally records an egress refusal event on every blocked dial attempt.
  - **Permitted Connects**: Protected by `a.egressMu`. Only recorded when `a.lastPinned[d.Host] != pin` (initial connection or DNS IP rotation).
  - Uses `context.Background()` to ensure local journal appends are not cancelled by request context timeouts.

---

## 3. Top-3 Weakest Points / Implementation Notes

In accordance with review discipline, the following observations and minor points are documented:

1. **Resolution Cache Lifetime in `d.resolve`**:
   - *Observation*: `d.DialContext` (`internal/channel/telegram/dialer.go:102`) calls `d.resolve(ctx, host)` on every dial.
   - *Detail*: This adheres strictly to E11 by re-verifying the address set on every connection and detecting dynamic rebinding. Because HTTP/2 keep-alive connections reuse existing connections (`MaxIdleConns: 10`, `IdleConnTimeout: 90s`), standard polling does not issue excess DNS queries during steady-state polling.
2. **Error Logging on Asynchronous Journal Receipt Failures**:
   - *Observation*: In `internal/channel/telegram/telegram.go:116, 127`, `_ = a.core.RecordEgress(...)` ignores the error returned by `RecordEgress`.
   - *Detail*: In production, `RecordEgress` appends to SQLite. If the journal is closed or unwritable, the call fails silently. While this prevents network polling from failing due to logging errors, logging a diagnostic warning in the daemon log on journal write failure would provide extra visibility.
3. **Address Set Receipt Truncation**:
   - *Observation*: `egressDecision` holds `Resolved []netip.Addr`, while `egressPayload` in `channel.go:194-199` records `Pinned string`.
   - *Detail*: The journal receipt records the final pinned IP that was dialed (or `""` on refusal). The full candidate list is validated in-memory by the dialer. This is concise and prevents unbounded journal row growth for hosts with large round-robin pools.

---

## 4. Test Suite Verification & Detector Adequacy

- `internal/channel/telegram/dialer_test.go`:
  - `TestDialerProductionAllPublicPins`: Verifies public IP is pinned and an allowed receipt is emitted.
  - `TestDialerRejectsMetadataAndMappedForm`: Proves plain `169.254.169.254` and `::ffff:169.254.169.254` mixed with public IP are refused, resulting in zero dials and refusal receipts.
  - `TestDialerProductionDenyFloor`: Exhaustively checks 8 non-public address classes (`127.0.0.1`, `10.0.0.5`, `172.16.0.1`, `192.168.1.1`, `100.64.0.1`, `fc00::1`, `fe80::1`, `0.0.0.0`).
  - `TestDialerLoopbackMode`: Tests loopback mode accepting `127.0.0.1` and `::1`, and rejecting mixed public/loopback sets.
  - `TestDialerHostNotPermitted`: Verifies off-policy hosts are rejected with refusal receipts.
  - `TestPinnedClientNoProxyNoRedirect`: Confirms `Proxy == nil` and `CheckRedirect` error return.
  - `TestPinnedClientModeFromBase`: Confirms mode selection from `telegram_api_base`.
- `internal/channel/telegram/telegram_test.go`:
  - `TestRequestConstructionSanitized`: Confirms invalid API base fails closed at initialization without token leakage.
- Full test run (`go test ./...`) completed with 0 failures across all packages.

---

VERDICT: PASS

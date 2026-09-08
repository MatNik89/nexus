# Adversarial code review: Telegram pinned egress dialer, round 4

Target: commit `4ee4926a63475c7dc7102ad4fa874661191d1176` on `slice/p1-tg-dialer`  
Compared with: merge base `2030c01987693393b076e681c9f28dbcbf734eec` (`main...target`)  
Method: read-only review of the committed tree from a clean on-disk export. The export's scoped source hashes were checked against the target commit after temporary causal probes.

## Result

No new substantive correctness, security, E11-compliance, behavioral-equivalence, or buildability defect was found. The Round-3 blocker is closed, and all previously closed findings remain closed.

## Round-3 closure

**CLOSED — RFC 6145 IPv4-translated metadata bypass.** `v4EmbedPrefixes` now includes the well-known NAT64 prefix, RFC 6145 IPv4-translated prefix, and deprecated IPv4-compatible prefix (`internal/channel/telegram/dialer.go:89-98`). `embeddedV4` extracts the low 32 bits for those fixed standardized forms (`dialer.go:100-114`), and `permitted` recursively applies the ordinary v4 deny policy after `Unmap` normalization (`dialer.go:116-143`). The exact former bypass `::ffff:0:169.254.169.254` is therefore rejected before the dial sink.

The delivered `TestDialerRejectsForbiddenV4EveryEncoding` covers metadata, loopback, RFC1918, and CGNAT across plain v4, IPv4-mapped IPv6, well-known NAT64, and RFC 6145 translated forms (`internal/channel/telegram/dialer_test.go:314-351`). It is red-capable: removing only `::ffff:0:0:0/96` from production while retaining the test produced:

```text
--- FAIL: TestDialerRejectsForbiddenV4EveryEncoding
metadata admitted via encoding ::ffff:0:a9fe:a9fe
```

I also ran an independent temporary matrix, not derived from the production prefix slice, over six forbidden v4 boundary values and five representations, adding deprecated IPv4-compatible `::/96`; it passed. The temporary detector was removed and the export's source hashes were rebound to the target commit.

## Contract verification

- **Address policy:** every resolver answer is normalized with `Unmap`, every normalized member is checked, and pin selection occurs only after the whole set passes (`dialer.go:146-189`). Plain and mapped metadata, loopback, unspecified, multicast, interface/link-local, RFC1918, CGNAT, ULA, documentation, benchmarking, RFC 8215 local-use, NAT64, RFC 6145 translated, and deprecated-compatible forms are rejected by the combined predicates/tables (`dialer.go:29-41,89-143`). Loopback override returns true only for loopback addresses, so mixed loopback/public sets fail closed (`dialer.go:128-129,179-183`). Each new connection resolves and classifies afresh; there is no pin/admission cache.
- **TLS:** `newPinnedClient` installs only a raw-TCP `DialContext`; it does not install `DialTLSContext` and does not set `InsecureSkipVerify` (`dialer.go:192-249`). Go's `http.Transport` therefore performs TLS after dialing and derives `ServerName` from the original request target, not from the literal address used inside the custom dialer. `TestPinnedClientTLSVerificationOn` also proves an untrusted certificate is rejected (`dialer_test.go:255-277`). Pinning does not downgrade chain or hostname verification.
- **Proxy and redirects:** `http.Transport.Proxy` is explicitly nil and `CheckRedirect` always returns an error (`dialer.go:234-249`). Ambient HTTP(S) proxy variables are therefore ignored, and no redirect is followed.
- **Receipts:** coalescing is absent. Every `DialContext` decision emits a receipt; permitted receipt failure prevents the connect, and refusals preserve receipt failure while remaining typed pre-wire (`dialer.go:157-189`). Adapter construction always wires `egressReceipt`, which records the normalized set and pin through `Core.RecordEgress` (`internal/channel/telegram/telegram.go:90-120`; `internal/channel/channel.go:175-220`). The validator requires allowed/pinned and refused/reason. `TestEgressReceiptJournaled` proves the real adapter-to-journal path and token absence; its controlled `RecordEgress` ablation was previously observed RED.
- **Delivery state:** `errEgressPreWire` survives `http.Transport` wrapping and is recognized by `isPreWire` before token sanitization (`dialer.go:22-27,157-187`; `telegram.go:143-193`). Refusals and non-durable receipts therefore return to `PENDING`, not `UNKNOWN`.
- **Production composition:** production construction is deny-default on `egress_allow`, while validated loopback overrides are exempt (`dialer.go:192-219`). Both daemon and doctor pass the resolved allowlist (`cmd/nexus/main.go:156-164,844-849`).

## Test and build evidence

From clean export `/home/matej/HARNESS/nexus-dialer-4ee4926.oh8oYa`:

```text
/home/matej/.local/go/bin/go test ./internal/channel/telegram ./internal/channel ./cmd/nexus
PASS: telegram 0.878s; channel 0.460s; cmd/nexus 9.797s

/home/matej/.local/go/bin/go vet ./internal/channel/telegram ./internal/channel ./cmd/nexus
PASS

CGO_ENABLED=0 /home/matej/.local/go/bin/go test ./...
PASS: all packages

independent forbidden-address matrix
PASS

RFC6145-prefix causal ablation
RED for the expected translated-metadata admission
```

`go test -race ./internal/channel/telegram ./internal/channel` could not provide race evidence on this host: ThreadSanitizer stopped before executing tests with `unsupported VMA range; Found 47 - Supported 48`. This is an environment proof ceiling, not a code finding.

## Notes and three weakest points

1. **NOTE — standards-scope wording:** the comment's “COMPLETE” claim is correct only for the fixed, recognizable low-32 forms it names. RFC 6052 also defines Network-Specific Prefixes of lengths 32, 40, 48, 56, 64, and 96, with the embedded v4 bits in different positions. A bare IPv6 resolver answer cannot identify an organization-specific translator prefix without deployment configuration. No such prefix or reachable translator is established by this repository or target environment, so under the owner's directive this remains a deployment/theoretical ceiling, not a FAIL reason. Reference: https://www.rfc-editor.org/rfc/rfc6052.html#section-2.2.
2. **NOTE — pre-wire detector depth:** `TestEgressRefusalIsPreWire` validates the wrapped classifier directly rather than driving an actual outbox row through `UNKNOWN -> PENDING`. The implementation trace is complete, so this is only a proof-strength note.
3. **NOTE — TLS detector depth:** the TLS test proves verification remains enabled, but it does not observe a distinct hostname as SNI at the server. Original-host SNI is established from the concrete standard-library transport path; a dedicated trusted-CA/wrong-host test would make that proof more self-contained.

## Upfront guidance for Claude

The invariant-derived matrix materially improved this revision and prevented another one-address patch. Keep that approach, but separate “all fixed standardized forms recognized without deployment state” from “all deployment-specific translation prefixes.” Before review, compare any claimed complete protocol set with the primary standard, make the test oracle independent of the production table, include boundary values, and trace every fail-closed error through its durable caller state. This is the shortest reliable way to reduce review-loop regressions.

## Simplification

Lean already. The shared prefix table and one extraction loop replace bespoke byte-condition branches without a new dependency or abstraction layer.

VERDICT: PASS

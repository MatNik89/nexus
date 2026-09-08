# Adversarial code review: Telegram pinned egress dialer, round 3

Target: commit `29d53fc8016e56135709ca27be127354e47ea81b` on `slice/p1-tg-dialer`  
Compared with: merge base `2030c01987693393b076e681c9f28dbcbf734eec` (`main...target`)  
Method: read-only review of the committed tree from a clean on-disk export. The unrelated untracked `docs/REVIEW-DIALER-CODE3-agy.md` in the working tree was not read or modified.

## Finding

### F1 — HIGH — IPv4-translated IPv6 form bypasses the metadata deny floor

E11 requires the metadata IP to be blocked in every encoding (`docs/ARCHITECTURE-ESSENTIALS.md:152-154`), and the approved plan repeats that `169.254.169.254` must be rejected in every representation (`docs/PLAN-TG-EGRESS-DIALER.md:33-38`). The implementation does not meet that closed requirement.

`embeddedV4` recognizes only the well-known NAT64 prefix `64:ff9b::/96` and deprecated IPv4-compatible `::/96` (`internal/channel/telegram/dialer.go:89-115`). `Addr.Unmap` at lines 123 and 176 handles ordinary IPv4-mapped addresses, but it does not unmap the RFC 6145 IPv4-translated prefix `::ffff:0:0:0/96`. That prefix is also absent from `forbiddenSpecialUse` (`dialer.go:29-41`). Consequently, the translated metadata address `::ffff:0:169.254.169.254` (`::ffff:0:a9fe:a9fe`) passes every predicate at `dialer.go:118-145`, receives an allowed receipt, and reaches the dial sink at line 191.

I proved reachability with a temporary external detector in the clean export:

```text
resolver answer: ::ffff:0:a9fe:a9fe
expected: policy refusal and no dial
actual: TestCodexProbeRejectsIPv4TranslatedMetadata FAIL
        IPv4-translated metadata admitted; dial target="[::ffff:0:a9fe:a9fe]:443"
```

The detector file was removed after the run. This is not an editorial completeness issue: a resolver answer can select an IPv6 translation route to an IPv4 destination that the v4 policy expressly forbids. Fix the transition classifier (or reject the whole translated prefix) and add this exact address as a contract-anchored regression case. Then sweep the other standardized IPv4-embedded IPv6 forms against the literal “every encoding” invariant instead of extending an ad hoc list one review at a time.

## Round-2 closure verification

- **F1 closed:** every normal refusal attempts a receipt; a receipt-append failure is preserved in the returned error text and the dial remains pre-wire (`dialer.go:159-165`). A permitted decision also refuses to dial if its receipt is not durable (`dialer.go:186-191`).
- **F2 closed:** `errEgressPreWire` wraps policy refusals and non-durable permitted receipts (`dialer.go:22-27,159-165,186-190`); `isPreWire` recognizes it through transport wrapping with `errors.Is` before sanitization (`telegram.go:143-193`). Ablating that recognition made `TestEgressRefusalIsPreWire` RED for the expected reason.
- **F3 partially closed but superseded by F1:** the single special-use table now rejects all of `64:ff9b:1::/48` (`dialer.go:29-41`), and removing that prefix made `TestDialerRejectsRFC8215Local` RED. The new F1 is a different standardized IPv4-translation representation that the classifier still misses.
- **F4 closed:** `TestPinnedClientTLSVerificationOn` rejects an untrusted certificate and verifies that `InsecureSkipVerify` is not enabled (`dialer_test.go:255-277`). The transport uses raw `DialContext` rather than `DialTLSContext`; Go's `http.Transport` therefore performs TLS itself using the request target host, so the pinned TCP literal does not replace the original TLS hostname. `TestEgressReceiptJournaled` traverses the adapter-to-core-to-journal path and checks resolved set, pin, and token absence (`dialer_test.go:279-312`); ablating `Core.RecordEgress` made it RED.

All earlier round-1 items remain closed: `Unmap` normalization and whole-set refusal are applied before selecting a pin (`dialer.go:174-186`); every connection re-resolves; production `egress_allow` is deny-default and is passed by both daemon and doctor composition; coalescing is absent; the receipt validator enforces allowed/pinned and refused/reason; `Proxy:nil` and reject-all `CheckRedirect` are explicit (`dialer.go:194-251`).

## Test and build evidence

From clean export `/home/matej/HARNESS/nexus-dialer-29d53fc.ENE6Hb`:

```text
/home/matej/.local/go/bin/go test ./internal/channel/telegram ./internal/channel ./cmd/nexus
PASS: telegram 0.866s; channel 0.247s; cmd/nexus 9.953s

/home/matej/.local/go/bin/go vet ./internal/channel/telegram ./internal/channel ./cmd/nexus
PASS

CGO_ENABLED=0 /home/matej/.local/go/bin/go test ./...
PASS: all packages, including acceptance, daemon, preflight/probe, sandbox, channel, and telegram
```

The delivered tests are generally non-vacuous. Three causal ablations were independently observed RED: sentinel recognition, RFC 8215 prefix rejection, and durable journal emission. The new translated-metadata detector is also RED against the committed implementation, so the green suite has a demonstrated contract-coverage hole.

`go test -race ./internal/channel/telegram ./internal/channel` could not supply race evidence on this host: ThreadSanitizer terminated before tests with `unsupported VMA range; Found 47 - Supported 48`. This environment limitation is not the FAIL reason.

## Weakest points and notes

1. The address classifier is the decisive weakest point: it implements a nominally closed public/metadata floor as a manually enumerated set of transition formats, and the fresh counterexample demonstrates that the set is incomplete.
2. `TestEgressRefusalIsPreWire` proves the wrapped classifier but does not drive a real outbox row through `UNKNOWN -> PENDING`; the implementation trace is correct, so this is a proof-strength note, not another finding.
3. The TLS detector proves verification is enabled, while original-host SNI is established primarily by the standard-library transport path rather than by observing SNI at a test server. This is also a proof-strength note.

## Upfront guidance for Claude

Before sending the next egress revision to reviewers, derive hostile cases from the invariant rather than only from prior findings. For any rule phrased “every encoding,” build a table of standardized encodings/transition prefixes, run every forbidden v4 class through each representation, and include at least metadata, loopback, RFC1918, and CGNAT boundary cases. Also trace every newly introduced fail-closed error through its caller's durable state classifier and force every receipt append to fail in a boundary test. This would have exposed the current bypass before review and avoids the recurring fix-one-prefix/add-another-prefix loop.

VERDICT: FAIL

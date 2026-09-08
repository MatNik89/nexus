# Adversarial code review — Telegram E11 dialer, round 2

## Scope and immutable binding

Reviewed commit `9f5cb3faafa875fc889d82e75a1a85b3489de890`
(tree `0718e9a81a5d20f7be91aa0afa884d39777f024c`) on
`slice/p1-tg-dialer` against `main`/merge-base
`2030c01987693393b076e681c9f28dbcbf734eec`. I did not checkout or switch
branches. Source and tests were inspected and executed from a clean on-disk
`git archive` export.

## Substantive findings

### F1 — MEDIUM — Refusal receipt failures are still silently discarded

`internal/channel/telegram/dialer.go:159-161` calls `emit` for every refusal but
discards its returned error. Thus host rejection, resolver failure, an empty
answer set, or a forbidden/mixed set can fail to append its audit event while
the caller receives only the policy error. This contradicts the code/plan claim
that every refusal is journaled (`dialer.go:6-8`;
`docs/PLAN-TG-EGRESS-DIALER.md:76-78`) and E4's rule that security/audit events
are not best-effort.

The permitted path is now correctly fail-closed on receipt failure
(`dialer.go:182-187`), so round-1 F1 is only partially closed.

**Required fix:** preserve both failures, for example return a typed pre-wire
error that joins the refusal reason and receipt-append error. Add a refusal-side
append-failure detector proving no dial and proving the storage failure reaches
the caller.

### F2 — MEDIUM — All new policy/receipt failures are misclassified as ambiguous sends

`telegram.isPreWire` recognizes only `*net.DNSError` or
`*net.OpError{Op:"dial"}` (`internal/channel/telegram/telegram.go:143-154`).
The dialer returns ordinary formatted errors for policy refusals and durable
receipt failures (`dialer.go:159-161,184-185`). Go's `http.Transport` returns a
custom `DialContext` error without converting it to `net.OpError`; `call`
therefore wraps these definitely pre-wire outcomes in `channel.ErrAmbiguousSend`
(`telegram.go:178-187`). `Core.Flush` then leaves the outbox row UNKNOWN for
reconciliation instead of safely re-pending it (`internal/channel/channel.go:542-589`).

Concrete result: a forbidden DNS answer or transient receipt append failure can
strand a message in UNKNOWN even though no byte could have reached Telegram.
This is behavioral/correctness breakage, not merely an imprecise diagnostic.
`TestDialerReceiptFailClosed` calls `DialContext` directly and cannot detect the
caller-side misclassification.

**Required fix:** give every dialer pre-wire failure a stable typed/sentinel
classification recognized by `isPreWire`, then add an adapter/outbox test that
forces a policy refusal and a receipt failure and asserts PENDING, never UNKNOWN.

### F3 — MEDIUM — The production “public” floor still admits a standardized local-use translation prefix

The added handling correctly closes the specifically named
`64:ff9b::/96` NAT64 form and deprecated IPv4-compatible `::/96` form
(`internal/channel/telegram/dialer.go:67-94`). It does not reject
`64:ff9b:1::/48`, the standards-reserved local-use IPv4/IPv6 translation block.
That block passes every predicate in `permitted` and is not covered by the sole
`2001:db8::/32` IPv6 special-use check (`dialer.go:114-145`). RFC 8215 defines
`64:ff9b:1::/48` for translation inside local domains and marks it not globally
reachable: [RFC 8215](https://www.rfc-editor.org/rfc/rfc8215.html).

An attacker-controlled/poisoned AAAA answer in that prefix can therefore direct
the supposedly production-public dialer into a locally routed translation
domain. The exact translated IPv4 target is deployment-specific, but admitting
the entire explicitly local-use prefix already contradicts the production
public floor and locked E11's fail-closed treatment of metadata encodings.

**Required fix:** reject `64:ff9b:1::/48` wholesale in production and add it to
the 4-in-6 detector table. More generally, make the special-use policy a single
reviewable prefix table rather than continuing with scattered byte predicates.

### F4 — MEDIUM — Required TLS and durable-receipt detectors remain absent

The implementation's TLS behavior is correct: `DialContext` receives the
original authority, returns a raw TCP connection to the pinned literal, and
`http.Transport` performs TLS using the original URL host for SNI and
certificate hostname verification. However, the approved
`TLS-SNI-PRESERVED` detector is still not present
(`PLAN-TG-EGRESS-DIALER.md:86-88`). There is also no test that reads a real
journal event and verifies the normalized resolved set, pin, decision, and
secret absence. The direct callback tests cannot detect a broken
`Adapter.egressReceipt` or `Core.RecordEgress` serialization.

For a locked security boundary, this is a substantive delivery-proof gap. A
future `InsecureSkipVerify`, wrong `ServerName`, dropped resolved set, or
refusal append regression can remain green. The claimed RED ablation is not a
committed, independently reviewable artifact for these paths.

**Required fix:** add the design's real TLS server/SNI/wrong-certificate test
through `http.Client`, plus a fresh-journal receipt test covering permitted,
refused, and append-failure cases.

## Round-1 fold verification

- **F1 permitted-receipt fail-closed: CLOSED.** The callback returns `error`,
  `emit` propagates it, and no TCP dial occurs after an allowed receipt failure
  (`dialer.go:52-65,182-187`; `dialer_test.go:161-177`). The refusal half has
  the residual in current F1.
- **F2 `egress_allow`: CLOSED.** Production construction requires the exact API
  hostname, loopback override is explicitly exempt, and both composition-root
  call sites pass the resolved sealed list (`dialer.go:190-230`;
  `telegram.go:41-48,94-103`; `cmd/nexus/main.go:158-164,845-849`). The new
  deny-default test is red-capable against the previous constructor.
- **F3 coalescing: CLOSED.** `lastPinned`, its mutex, and suppression logic are
  deleted. Every `DialContext` decision reaches `emit`; permitted connects wait
  for a successful receipt (`dialer.go:148-187`). HTTP keep-alive—not adapter
  state—naturally limits receipt volume.
- **F4 event payload/validator: CLOSED in production code.** The adapter carries
  the full normalized set, and the journal payload stores it. Validation now
  enforces `allowed => pinned` and `refused => reason`
  (`telegram.go:106-120`; `channel.go:175-220`). Current F4 concerns the missing
  durable boundary detector, not the implementation mapping.
- **F5 named address cases: CLOSED, with the new residual in current F3.** Plain
  and mapped metadata, RFC1918, CGNAT `100.64.0.0/10`, ULA `fc00::/7`,
  loopback, unspecified, multicast, link-local, the well-known NAT64 prefix,
  IPv4-compatible forms, RFC5737/RFC2544 IPv4 ranges, and `2001:db8::/32` are
  rejected. `Unmap` occurs before the whole-set check, and selection happens
  only after every normalized answer passes (`dialer.go:114-187`). Loopback
  override still requires every answer to be loopback.

## Other verified controls and test quality

- `Proxy:nil` really disables ambient HTTP(S) proxy selection, and
  `CheckRedirect` rejects every redirect (`dialer.go:232-247`).
- Mixed-set rejection is structurally non-vacuous: the loop returns before
  choosing `norm[0]` if any answer fails (`dialer.go:170-187`).
- `TestDialerRejectsNAT64Metadata`, `TestDialerReceiptFailClosed`,
  `TestDialerSequentialRebind`, and `TestPinnedClientEgressAllowDenyDefault`
  exercise the relevant production functions and have meaningful negative
  assertions (`dialer_test.go:147-222`). They are not tampered or vacuous.
- NOTE: `TestPinnedClientModeFromBase` still claims resolver acceptance but
  asserts only the helper's Boolean result (`dialer_test.go:136-145`). This is
  editorial/test-description drift, not a verdict reason.

## Verification run

- `CGO_ENABLED=0 go test -count=1 ./internal/channel/telegram ./internal/channel ./cmd/nexus` — PASS.
- `CGO_ENABLED=0 go test -count=1 ./...` — PASS for all packages.
- `go vet ./internal/channel/telegram ./internal/channel ./cmd/nexus` — PASS.
- `go test -race -count=1 ./internal/channel/telegram` — environment ERROR:
  ThreadSanitizer reports unsupported ARM VMA range (`Found 47 - Supported 48`),
  so this run supplies no race verdict.

Topknot simplification: coalescing deletion was the correct reduction. The
remaining address checks should be consolidated into one prefix-policy table;
otherwise each review round adds another special-case branch.

Skill update: added a caller-error-classification rule to `nexus-audit` because
it would have caught F2 in round 1.

VERDICT: FAIL

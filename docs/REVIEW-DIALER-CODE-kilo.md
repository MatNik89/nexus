# Review of TG pinned-IP dialer implementation (`a5645387`)

**Scope**: commit `a5645387` on `slice/p1-tg-dialer` — `dialer.go`, `telegram.go`
wiring, `channel.go` receipt, tests, against `PLAN-TG-EGRESS-DIALER.md` + E11
(`ARCHITECTURE-ESSENTIALS.md:146-158`).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. The dialer implements the plan's address policy and the E11 contract
correctly. No substantive security / correctness / E11-compliance /
behavioral-equivalence / unbuildable defect found.

## Verification (as asked)

### 1. Address policy — correct, gaps are non-routable

`permitted` (`dialer.go:64-86`) after `Unmap()` (`:109`):
- rejects invalid, loopback, unspecified, multicast, interface-local-multicast,
  link-local-unicast, link-local-multicast, `IsPrivate` (RFC1918 v4 + ULA
  `fc00::/7`), and CGNAT `100.64/10` via the explicit `b[1]>=64 && b[1]<=127`
  check (correctly not covered by `IsPrivate`).
- MIXED-SET is fail-closed: `DialContext` refuses the whole resolution on the
  FIRST non-permitted answer (`:115-120`) — never pick-the-good-one.
- The metadata IP is blocked in plain AND mapped form: `TestDialerRejectsMetadataAndMappedForm`
  exercises `169.254.169.254` and `::ffff:169.254.169.254` in a mixed set.
- Loopback mode requires all-loopback (`:68-70`), mixed loopback+public refused
  (`TestDialerLoopbackMode`).

### 2. TLS is not downgraded — correct

`DialContext` returns a raw `net.Conn` from `d.dial` (`:123`); the
`http.Transport` performs TLS itself against the ORIGINAL hostname (SNI +
cert chain via the URL host), so pinning the literal IP never bypasses cert
validation. This is standard `http.Transport` semantics and the code comment
(`:90-92`) is accurate.

### 3. Proxy / redirect — correct

`Proxy: nil` on the explicit transport (`:154`) means "no proxy" (stdlib
contract: a nil `Proxy` field disables proxying; `http.DefaultTransport` is
what injects `ProxyFromEnvironment`), and `CheckRedirect` returns an error for
every redirect (`:165-167`). `TestPinnedClientNoProxyNoRedirect` asserts both.

### 4. Egress receipt coalescing — sound, satisfies E11 audit intent

`egressReceipt` (`telegram.go:114-129`): refusals are **always** journaled; a
permitted connect is journaled only when the pinned IP for the host first
appears or changes (mutex-guarded `lastPinned`). Analysis of the named holes:
- *Refusal dropped?* No — `!Allowed` short-circuits to `RecordEgress` before
  any coalescing.
- *Concurrency?* `lastPinned` read-compare-write is under `egressMu`; no race.
- *Rebind reusing a prior IP?* A rebind is an IP **change**, which is journaled;
  only same-IP steady state is coalesced. An alternation attack journals every
  flip (correct — that is the security event). A restart re-records once
  (in-memory, documented `:64`).

This preserves E11's "egress receipt" intent: every refusal + every distinct
pinned IP is durably recorded; steady-state 2s polling does not flood the
journal. The event id is unique per receipt (`channel.go:383`, pid+nanos+atomic
seq), so repeated refusals are not de-duplicated away.

### 5. Tests — non-vacuous, contract-anchored

All seven tests assert observable contract behavior (which address was dialed,
which was refused, what the receipt contained), not implementation internals.
`TestRequestConstructionSanitized` was correctly strengthened to fail-closed at
`New()` with no token leak.

## Notes (not FAIL reasons)

1. **Deny floor is a denylist, not `IsGlobalUnicast()`.** Omitted classes —
   limited broadcast `255.255.255.255`, reserved `240/4`, TEST-NET
   `192.0.2/24`/`198.51.100/24`/`203.0.113/24`, benchmark `198.18/15` — pass
   the floor but are non-routable (dial fails) and not useful SSRF targets; the
   SSRF-relevant set (loopback/link-local-metadata/private/CGNAT/ULA) is fully
   covered. An allowlist would be strictly tighter.
2. **`Unmap` covers `::ffff:0:0/96`, not the deprecated `::/96` IPv4-compatible
   form**; `::10.0.0.5` would slip the floor. Deprecated and non-routable.
3. **Receipt error is ignored** (`_ = RecordEgress`, `:116,127`). The receipt is
   best-effort audit evidence; the SECURITY CONTROL is the IP pinning, which is
   independent and fail-closed, so a lost receipt never weakens enforcement.
   Under journal failure the audit trail degrades silently — a stricter reading
   of "before connect" would fail-closed, but coupling the dialer to journal
   availability is a defensible thing to decline.
4. **Coalescing is untested** — no detector for the `lastPinned` first/change
   behavior (plan's RECEIPT-EMITTED). Logic is simple and correct by inspection,
   but the deviation from "append before every connect" ships unexercised.
5. **Deny-floor test omits multicast/interface-local/link-local-multicast
   classes** — the code checks them (`IsMulticast`/`IsInterfaceLocalMulticast`/
   `IsLinkLocalMulticast`), but `TestDialerProductionDenyFloor` doesn't feed
   them. Coverage gap only.
6. **TLS-SNI-PRESERVED is untested** here (relies on standard `http.Transport`
   behavior rather than a stub that asserts the SNI/cert hostname).

VERDICT: PASS

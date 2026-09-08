# Review of TG pinned-IP dialer implementation r2 (`9f5cb3f`)

**Scope**: commit `9f5cb3f` on `slice/p1-tg-dialer` — `dialer.go`, `telegram.go`
wiring, `channel.go` receipt, tests, against `PLAN-TG-EGRESS-DIALER.md` + E11
(`ARCHITECTURE-ESSENTIALS.md:146-158`).
**Reviewer**: kilo
**Date**: 2026-09-08

## Verdict

PASS. All five round-1 folds are closed, verified against the code; build is
clean and the full channel suite is green. No substantive security /
correctness / E11-compliance / behavioral-equivalence / unbuildable residual.

## Round-1 folds — closure status

### F1 — receipt error + permitted dial fail-closed — closed

`receipt func(egressDecision) error` (`dialer.go:57`); `emit` propagates it
(`:60-65`). The PERMITTED path fails the dial when the append fails
(`:184-186`), matching E11's "before connect" (no connect without a durable
receipt). `lastPinned` is gone (F3). `TestDialerReceiptFailClosed` proves a
permitted dial does not connect when the receipt errors.

### F2 — production host in egress_allow, deny-default — closed

`newPinnedClient` refuses a non-loopback host absent from `egressAllow`
(`dialer.go:206-217`); loopback override skips the check. `Config.EgressAllow`
is threaded (`telegram.go:46-47,96`) from `resolved.Config.EgressAllow`
(`main.go:163,848`). `TestPinnedClientEgressAllowDenyDefault` covers
empty/wrong/correct allowlist and the loopback exemption.

### F3 — coalescing deleted — closed

No `lastPinned` map remains; every `DialContext` decision is journaled via
`emit` (refusal and permitted alike), with the dialer comment correctly noting
HTTP keep-alive already coalesces the 2s polls.

### F4 — resolved set + validator field invariants — closed

`egressPayload` carries `Resolved []string` (`channel.go`); the
`EvEgressAttempt` validator enforces `Allowed ⇒ Pinned != ""` and
`!Allowed ⇒ Reason != ""` (`channel.go`), so a malformed receipt is rejected
at append, fail-closed.

### F5 — NAT64 / IPv4-compatible / doc / bench / 2001:db8 — closed

`embeddedV4` (`dialer.go:71-94`) extracts the v4 from the NAT64 well-known
prefix `64:ff9b::/96` and the deprecated IPv4-compatible `::/96` (excluding
`::`/`::1`), and `permitted` recursively classifies it by the v4 policy
(`:121-125`); `specialUseV4` (`:98-112`) rejects `192.0.2/24`,
`198.51.100/24`, `203.0.113/24`, `198.18/15`, and CGNAT `100.64/10`; the
`2001:db8::/32` doc range is rejected (`:139-144`). The NAT64-encoded metadata
IP is exercised by `TestDialerRejectsNAT64Metadata` (`64:ff9b::a9fe:a9fe` =
`169.254.169.254`).

## Notes (not FAIL reasons)

1. **Default-config inconsistency.** `defaults()` sets
   `TelegramAPIBase = https://api.telegram.org` (`config.go:124`) but no
   default `egress_allow` entry, so a fresh install with the default config
   fails closed at `New()` until the owner adds `api.telegram.org`. The
   deny-default is correct; the default layer should admit its own default
   host (or the setup docs must say so) — a polish item, not a security gap.
2. **Refusal receipt is best-effort, permitted receipt is fail-closed.** The
   refusal path uses `_ = d.emit(...)` (`dialer.go:160`) — the refusal itself
   blocks the dial unconditionally, so the control is independent of the
   receipt; only the refusal *evidence* is best-effort. Defensible asymmetry
   (the connect cannot happen without a receipt; a refusal has no connect to
   gate).
3. **Only the well-known `64:ff9b::/96` NAT64 prefix is classified.** A
   network-specific NAT64 prefix (RFC 6052 local config) embedding a forbidden
   v4 would not be detected — impractical to cover (the prefix is
   operator-local) and not a realistic DNS-poisoning vector.
4. **`0.0.0.0/8` non-zero addresses slip `IsUnspecified`** (only `0.0.0.0` is
   "unspecified"); e.g. `0.0.0.2` passes the floor. Non-routable in practice.
   An `IsGlobalUnicast()` allowlist would close this and note #3's residue in
   one move.

## Verified clean

- TLS: `DialContext` returns a raw `net.Conn`; the `http.Transport` does TLS
  against the ORIGINAL hostname (SNI + cert chain), so IP pinning never
  downgrades verification.
- `Proxy: nil` disables ambient proxy; `CheckRedirect` rejects every redirect.
- `embeddedV4` recursion is depth-1 (v6 → v4, no cycle) and correct bytewise
  for both the NAT64 prefix and the `::/96` form.
- All 11 dialer tests are non-vacuous and contract-anchored
  (`TestDialerReceiptFailClosed`, `TestDialerRejectsNAT64Metadata`,
  `TestDialerSequentialRebind`, `TestPinnedClientEgressAllowDenyDefault` among
  them); full channel suite passes.

VERDICT: PASS

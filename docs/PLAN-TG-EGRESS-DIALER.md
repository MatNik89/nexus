# PLAN: Telegram egress pinned-IP dialer (channel slice, E11 prerequisite)

## Why
E11 is a LOCKED invariant (`ARCHITECTURE-ESSENTIALS.md:146-158`): egress must go
through a policy-aware resolver/dialer that pins every resolved IP before
connect (anti-DNS-rebind), sanitizes proxy env, and rechecks redirects. The
Telegram adapter's client today is `&http.Client{Timeout: 65s}`
(`telegram.go:90`) — default transport, ambient proxy honored, unpinned DNS,
redirects followed. Every Telegram call (getUpdates/sendMessage/getFile and the
new voice file download) rides this non-compliant client. The voice slice adds
another attacker-influenced remote fetch, so it may not depend on E11-forbidden
behavior; a "pre-existing ceiling" is not an owner waiver (r3 codex F4).

Fix it ONCE, channel-wide, so voice simply reuses a compliant client — no
per-slice waiver, and the existing channel becomes compliant too.

## Design
Replace the adapter's client with one built from an explicit transport:

- `Proxy: nil` — ignore `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY`/`NO_PROXY`.
- `DialContext`: a pinning dialer.
  1. Split host:port from the address the transport asks to dial.
  2. Enforce the host is the configured Telegram API host and `egress_allow`
     admits it (`config.EgressAllow`, `config.go:39`) — deny-default.
  3. Resolve the host to its IP set ONCE; pick one; dial THAT literal IP.
  4. TLS still verifies the ORIGINAL hostname (ServerName = host), so pinning
     the IP cannot downgrade cert validation.
  5. A second dial for the same request (retry) re-pins from the same resolved
     set; a resolved IP outside the admitted policy is refused.
- `CheckRedirect`: reject every redirect (Bot API never legitimately 30x's a
  method call; the file download must not leave the host).
- The bot token appears only in the URL/path; the dialer and error paths keep
  today's token sanitization.

This is one client, shared by all adapter calls; no new dependency (net +
crypto/tls stdlib).

## Detectors (RED before, GREEN after)
- PROXY-IGNORED: with `HTTPS_PROXY` set to a canary server, a Bot API call does
  NOT connect to the canary. RED against the default-transport client.
- REDIRECT-REFUSED: a 30x from a fake API host yields a typed error and no
  follow request reaches the redirect target.
- REBIND-REFUSED: a resolver stub that returns an admitted IP on the first
  lookup and an off-policy IP on a second is refused on the second; the dial
  target is always the pinned admitted IP, never a late-swapped one.
- HOST-DENY: a host outside `egress_allow` / the configured API host is refused
  before any connect.
- TLS-SNI-PRESERVED: dialing the pinned IP still presents the hostname as SNI
  and validates the cert chain against the hostname (a cert for the wrong name
  is rejected).

## Non-goals
- A general kernel egress broker (that is S6.3 proper); this is the Telegram
  channel's compliant client, matching the E11 contract for this adapter.
- mTLS / cert pinning beyond standard hostname verification.

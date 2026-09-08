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
- `DialContext`: a pinning dialer with an EXPLICIT resolved-IP policy (r4
  codex F1 — `egress_allow` is host strings only, `config.go:29-44,322-329`,
  and cannot classify an address, so the IP policy is defined HERE):
  1. Split host:port; enforce the host is the configured Telegram API host and
     `egress_allow` admits it — deny-default.
  2. Resolve the host ONCE to its address set. NORMALIZE every answer with
     `netip.Addr.Unmap()` (an IPv4-mapped-IPv6 form of a forbidden address must
     classify the same as its plain v4 form).
  3. ADDRESS POLICY, mode-sensitive on the config-validated API base (r5 codex
     F1 — `config.ValidateBounds` already accepts ONLY the production endpoint
     or an explicit http(s) LOOPBACK override, `config.go:334-340`, so the two
     modes are exactly those):
     - PRODUCTION mode (`telegram_api_base == https://api.telegram.org`):
       PUBLIC DENY FLOOR on EVERY normalized answer — reject loopback,
       unspecified, multicast, interface-local, link-local (`169.254.0.0/16`,
       `fe80::/10` — covers the cloud metadata IP `169.254.169.254` in every
       representation), and — Telegram being public — private v4 (`10/8`,
       `172.16/12`, `192.168/16`), CGNAT (`100.64/10`), and ULA (`fc00::/7`).
     - LOOPBACK-OVERRIDE mode (the validated local bot-api / test endpoint):
       require EVERY normalized answer to be loopback; reject any non-loopback
       or mixed set. This keeps the supported local endpoint usable WITHOUT
       weakening production enforcement (a production host can never enter
       loopback mode — the base is fixed by config validation).
  4. MIXED-SET RULE (fail-closed, both modes): if ANY resolved answer fails the
     mode's policy, REFUSE the whole resolution — never dial a "good" answer
     from a poisoned set. Only when EVERY answer passes do we pin one and dial
     that literal IP.
  5. TLS verifies the ORIGINAL hostname (`ServerName = host`), so pinning the
     literal IP never downgrades cert validation.
  6. A retry re-resolves and re-applies steps 2-4; the connect target is always
     a freshly-classified pinned IP, never a late-swapped one.
- EGRESS RECEIPT (E11 mandatory, `ARCHITECTURE-ESSENTIALS.md:146-158`): before
  connect, a typed `EgressAttempt{host, resolvedSet, pinnedIP, decision, ts}`
  record is appended through the journal (the single canonical writer, E4/B7)
  — the honest data-flow evidence E11 requires; token never in the receipt.
- `CheckRedirect`: reject every redirect (Bot API never legitimately 30x's a
  method call; the file download must not leave the host).
- The bot token appears only in the URL/path; the dialer and error paths keep
  today's token sanitization.

This is one client, shared by all adapter calls; no new dependency (net/netip
+ crypto/tls stdlib).

## Detectors (RED before, GREEN after)
- PROXY-IGNORED: with `HTTPS_PROXY` set to a canary server, a Bot API call does
  NOT connect to the canary. RED against the default-transport client.
- REDIRECT-REFUSED: a 30x from a fake API host yields a typed error and no
  follow request reaches the redirect target.
- REBIND-REFUSED: a resolver stub returning an admitted IP on the first lookup
  and a deny-floor IP on a second is refused on the second; the dial target is
  always a freshly-classified pinned IP, never a late-swapped one.
- MIXED-SET-REFUSED: a single DNS answer set containing BOTH an admitted public
  IP and `169.254.169.254` — AND a second case with its IPv4-mapped-IPv6
  encoding — is refused entirely (no connect to either). RED against a
  pick-the-good-one dialer.
- RECEIPT-EMITTED: a successful connect appends exactly one typed
  `EgressAttempt` receipt naming the pinned IP; a refused resolution appends a
  receipt with the refuse decision. Token never present.
- HOST-DENY: a host outside `egress_allow` / the configured API host is refused
  before any connect.
- MODE-LOOPBACK: with a validated loopback API base, an all-loopback answer set
  connects (IPv4 `127.0.0.1` and IPv6 `::1` and `localhost` cases), while a
  mixed loopback+public set is refused; with the production base, an all-public
  answer connects and any loopback answer is refused. RED against a single
  fixed floor that breaks the local bot-api endpoint.
- TLS-SNI-PRESERVED: dialing the pinned IP still presents the hostname as SNI
  and validates the cert chain against the hostname (a cert for the wrong name
  is rejected).

## Non-goals
- A general kernel egress broker (that is S6.3 proper); this is the Telegram
  channel's compliant client, matching the E11 contract for this adapter.
- mTLS / cert pinning beyond standard hostname verification.

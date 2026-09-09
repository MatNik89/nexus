//go:build linux

// Package egress is the ONE E11 egress owner (AUDIT-FULL F1; PLAN-AUDIT-FIXES
// Slice A): a pinned-IP, proxy-sanitized, redirect-rejecting HTTP client that
// every outbound component (provider, Telegram) MUST build its transport
// from. There is no default-transport fallback anywhere: http.DefaultTransport
// honours ambient proxy variables, resolves DNS without policy and leaves no
// receipt — with a bearer key or bot token in flight that is an exfiltration
// path, not a client.
//
// Contract:
//   - ONE canonical endpoint unit: lowercased hostname + effective port
//     (explicit or the scheme default). Allowlist matching, the dial address,
//     the receipt and the S7 target string all use it.
//   - EVERY resolved address is classified; if ANY answer fails the mode
//     policy the WHOLE resolution is refused (no pick-the-good-one).
//   - A receipt sink is MANDATORY. On the permitted path the receipt must be
//     durable BEFORE the connection; a receipt failure fails the dial closed.
//   - Every refusal and receipt failure wraps ErrPreWire: nothing left the
//     process, so the caller may classify it as a DEFINITE pre-wire failure.
//   - TLS (SNI + chain verification) is done by http.Transport against the
//     ORIGINAL hostname; pinning the literal IP never downgrades verification.
package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrPreWire marks every egress refusal or non-durable-receipt failure:
// NOTHING left the process. Callers classify it as a definite pre-wire
// failure (the outbox may safely re-pend; never UNKNOWN).
var ErrPreWire = errors.New("egress refused pre-wire")

// ErrReceiptNotDurable marks a permitted dial refused because the receipt
// sink (the journal) could not make the receipt durable. It wraps
// ErrPreWire (nothing left the process) AND identifies a SUBSTRATE failure
// for health classification (Slice D): the wire is fine, the journal is not.
var ErrReceiptNotDurable = fmt.Errorf("egress receipt not durable: %w", ErrPreWire)

// Endpoint is the canonical endpoint unit: scheme (for the default port),
// lowercased hostname and the EFFECTIVE port.
type Endpoint struct {
	Scheme string
	Host   string
	Port   int
}

// Canonical is the ONE string form used for allowlist reporting, receipts
// and S7 targets: "host:port", always with the port.
func (e Endpoint) Canonical() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }

// Loopback reports whether the endpoint names the local machine — the
// validated local/test override mode (config.ValidateBounds restricts the
// Telegram base to production-or-loopback; the provider allows plaintext only
// to loopback). The address CLASS is decided by parsing an IP literal, never
// by a hostname prefix ("127.attacker.example" is a remote DNS name).
func (e Endpoint) Loopback() bool {
	if strings.EqualFold(e.Host, "localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(strings.Trim(e.Host, "[]")); err == nil {
		return ip.IsLoopback()
	}
	return false
}

// ParseEndpoint parses an http(s) base URL into the canonical unit.
func ParseEndpoint(base string) (Endpoint, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return Endpoint{}, fmt.Errorf("egress: base URL must be http(s) with a host (fail closed)")
	}
	e := Endpoint{Scheme: u.Scheme, Host: strings.ToLower(u.Hostname())}
	switch p := u.Port(); p {
	case "":
		if u.Scheme == "https" {
			e.Port = 443
		} else {
			e.Port = 80
		}
	default:
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Endpoint{}, fmt.Errorf("egress: invalid port %q (fail closed)", p)
		}
		e.Port = n
	}
	return e, nil
}

// Admitted reports whether the endpoint is on the deny-default allowlist.
// An entry WITHOUT a port means the endpoint's scheme-default port; an entry
// WITH a port means exactly that port. Hostnames compare case-insensitively.
// No wildcards (config.ValidateBounds already rejects them).
func Admitted(e Endpoint, egressAllow []string) bool {
	def := 80
	if e.Scheme == "https" {
		def = 443
	}
	for _, raw := range egressAllow {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		host, port := entry, def
		if h, p, err := net.SplitHostPort(entry); err == nil {
			n, perr := strconv.Atoi(p)
			if perr != nil {
				continue
			}
			host, port = h, n
		}
		if strings.EqualFold(strings.Trim(host, "[]"), e.Host) && port == e.Port {
			return true
		}
	}
	return false
}

// Decision is the typed receipt of ONE dial attempt (the E11 honest
// data-flow evidence). Refusals carry a Reason; a permitted connect carries
// the Pinned address. Resolved is the whole normalized answer set.
type Decision struct {
	Component string
	Host      string
	Port      int
	Resolved  []netip.Addr
	Pinned    netip.Addr
	Allowed   bool
	Reason    string
}

// ReceiptSink makes one Decision durable. It is MANDATORY: NewPinnedClient
// refuses a nil sink, and on the permitted path a sink error fails the dial.
type ReceiptSink func(Decision) error

// Options carries the resolver/dialer seams for tests. Nil means the stdlib
// resolver / a 30s net.Dialer.
type Options struct {
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
}

// forbiddenSpecialUse is the single reviewable table of non-public prefixes
// that the netip class predicates do not already cover. Classes with a
// stdlib predicate (loopback/private/link-local/multicast/ULA/...) stay in
// permitted().
var forbiddenSpecialUse = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC6598 CGNAT
	netip.MustParsePrefix("192.0.2.0/24"),    // RFC5737 documentation
	netip.MustParsePrefix("198.51.100.0/24"), // RFC5737 documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // RFC5737 documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC2544 benchmarking
	netip.MustParsePrefix("2001:db8::/32"),   // RFC3849 documentation
	netip.MustParsePrefix("64:ff9b:1::/48"),  // RFC8215 local-use translation
}

// v4EmbedPrefixes is the COMPLETE set of standardized IPv6 forms that carry an
// IPv4 address in their low 32 bits; any address in one of them is
// reclassified by the v4 policy on its embedded v4 ("metadata blocked in
// EVERY encoding"). IPv4-mapped ::ffff:0:0/96 is handled by Unmap first.
var v4EmbedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),    // RFC6052 NAT64 well-known
	netip.MustParsePrefix("::ffff:0:0:0/96"), // RFC6145 IPv4-translated
	netip.MustParsePrefix("::/96"),           // deprecated IPv4-compatible
}

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

type pinnedDialer struct {
	component    string
	loopbackMode bool
	endpoint     Endpoint
	resolve      func(ctx context.Context, host string) ([]netip.Addr, error)
	dial         func(ctx context.Context, network, addr string) (net.Conn, error)
	receipt      ReceiptSink
}

// permitted applies the mode's address policy to ONE normalized address.
func (d *pinnedDialer) permitted(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap()
	if a.Is6() {
		if e, ok := embeddedV4(a); ok && !d.permitted(e) {
			return false
		}
	}
	if d.loopbackMode {
		return a.IsLoopback()
	}
	if a.IsLoopback() || a.IsUnspecified() ||
		a.IsMulticast() || a.IsInterfaceLocalMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsPrivate() { // RFC1918 v4 + ULA fc00::/7
		return false
	}
	for _, p := range forbiddenSpecialUse {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// DialContext resolves the host, applies the mode policy to EVERY answer,
// refuses the whole resolution if any answer fails, journals the decision,
// then dials one pinned literal IP. A receipt failure on the PERMITTED path
// fails the dial closed.
func (d *pinnedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%s egress: malformed dial address: %w", d.component, ErrPreWire)
	}
	refuse := func(reason string, resolved []netip.Addr) (net.Conn, error) {
		if aerr := d.receipt(Decision{Component: d.component, Host: host, Port: d.endpoint.Port, Resolved: resolved, Reason: reason}); aerr != nil {
			return nil, fmt.Errorf("%s egress: %s (receipt append failed: %v): %w", d.component, reason, aerr, ErrPreWire)
		}
		return nil, fmt.Errorf("%s egress: %s: %w", d.component, reason, ErrPreWire)
	}
	if !strings.EqualFold(host, d.endpoint.Host) || port != strconv.Itoa(d.endpoint.Port) {
		return refuse("endpoint not permitted", nil)
	}
	addrs, err := d.resolve(ctx, host)
	if err != nil {
		return refuse("resolve failed", nil)
	}
	norm := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		norm = append(norm, a.Unmap())
	}
	if len(norm) == 0 {
		return refuse("no addresses resolved", nil)
	}
	for _, a := range norm {
		if !d.permitted(a) {
			return refuse("resolved address rejected by policy", norm)
		}
	}
	pinned := norm[0]
	if err := d.receipt(Decision{Component: d.component, Host: host, Port: d.endpoint.Port, Resolved: norm, Pinned: pinned, Allowed: true}); err != nil {
		return nil, fmt.Errorf("%s egress: refusing dial: %v: %w", d.component, err, ErrReceiptNotDurable)
	}
	return d.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
}

// NewPinnedClient builds the E11-compliant client for ONE endpoint: Proxy:nil
// (ambient proxy env ignored), the pinning DialContext, reject-all redirects.
// In PRODUCTION mode the endpoint must be admitted by egressAllow
// (deny-default); in the loopback mode egressAllow is not consulted (the
// loopback endpoint is the config-validated local/test override). The sink
// is mandatory. component names the caller in every receipt.
func NewPinnedClient(component, apiBase string, egressAllow []string, timeout time.Duration, opts Options, sink ReceiptSink) (*http.Client, error) {
	if component == "" {
		return nil, fmt.Errorf("egress: a component name is required (fail closed)")
	}
	if sink == nil {
		return nil, fmt.Errorf("%s egress: a receipt sink is required — no unreceipted egress (fail closed)", component)
	}
	ep, err := ParseEndpoint(apiBase)
	if err != nil {
		return nil, fmt.Errorf("%s %w", component, err)
	}
	loopback := ep.Loopback()
	if !loopback && !Admitted(ep, egressAllow) {
		return nil, fmt.Errorf("%s egress: endpoint %q is not in egress_allow (deny-default, fail closed)", component, ep.Canonical())
	}
	resolve := opts.Resolve
	if resolve == nil {
		r := net.DefaultResolver
		resolve = func(ctx context.Context, h string) ([]netip.Addr, error) {
			return r.LookupNetIP(ctx, "ip", h)
		}
	}
	dial := opts.Dial
	if dial == nil {
		nd := &net.Dialer{Timeout: 30 * time.Second}
		dial = nd.DialContext
	}
	d := &pinnedDialer{component: component, loopbackMode: loopback, endpoint: ep,
		resolve: resolve, dial: dial, receipt: sink}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("%s egress: redirects are not followed — one grant, one physical request (fail closed)", component)
		},
	}, nil
}

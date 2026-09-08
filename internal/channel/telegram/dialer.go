package telegram

// E11 egress boundary for the Telegram client (PLAN-TG-EGRESS-DIALER.md):
// a pinned-IP, proxy-sanitized, redirect-rejecting dialer. The bot token
// rides in the URL path, so every connection must reach ONLY the configured
// Telegram host and never a rebind/proxy peer. A typed receipt is journaled
// for EVERY dial decision (DialContext fires per TCP connection, and HTTP
// keep-alive already coalesces the 2s polls, so there is no flood to avoid).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// errEgressPreWire marks every egress refusal or non-durable-receipt failure:
// NOTHING left the process, so the caller must treat it as a DEFINITE pre-wire
// failure (isPreWire true -> outbox re-pends PENDING, never UNKNOWN). Without
// this, http.Transport wraps the dialer error opaquely and the outbox would
// strand the message for human reconciliation (codex round-2 F2).
var errEgressPreWire = errors.New("telegram egress refused pre-wire")

// forbiddenSpecialUse is the single reviewable table of non-public prefixes
// that the netip class predicates do not already cover (codex round-2 F3:
// one table, not scattered byte checks). Classes with a stdlib predicate
// (loopback/private/link-local/multicast/ULA/...) stay in permitted().
var forbiddenSpecialUse = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC6598 CGNAT
	netip.MustParsePrefix("192.0.2.0/24"),    // RFC5737 documentation
	netip.MustParsePrefix("198.51.100.0/24"), // RFC5737 documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // RFC5737 documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC2544 benchmarking
	netip.MustParsePrefix("2001:db8::/32"),   // RFC3849 documentation
	netip.MustParsePrefix("64:ff9b:1::/48"),  // RFC8215 local-use translation
}

// egressDecision is the typed receipt of one dial attempt (the E11 honest
// data-flow evidence). Refusals carry a Reason; a permitted connect carries
// the Pinned address. Resolved is the whole normalized answer set that was
// classified.
type egressDecision struct {
	Host     string
	Resolved []netip.Addr
	Pinned   netip.Addr
	Allowed  bool
	Reason   string
}

// egressModeForBase reports whether the configured API base is the validated
// loopback override (local bot-api / tests). config.ValidateBounds already
// restricts telegram_api_base to the production endpoint or a loopback URL,
// so these two modes are exhaustive.
func egressModeForBase(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return false
	}
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		return ip.IsLoopback()
	}
	return false
}

type pinnedDialer struct {
	loopbackMode bool
	apiHost      string
	resolve      func(ctx context.Context, host string) ([]netip.Addr, error)
	dial         func(ctx context.Context, network, addr string) (net.Conn, error)
	receipt      func(egressDecision) error
}

func (d *pinnedDialer) emit(dec egressDecision) error {
	if d.receipt != nil {
		return d.receipt(dec)
	}
	return nil
}

// embeddedV4 extracts a v4 address embedded in an IPv6 transition form that
// must be classified by the v4 policy (E11: metadata blocked in EVERY
// encoding). Covers the NAT64 well-known prefix 64:ff9b::/96 and the
// deprecated IPv4-compatible ::/96 form.
func embeddedV4(a netip.Addr) (netip.Addr, bool) {
	if !a.Is6() {
		return netip.Addr{}, false
	}
	b := a.As16()
	// NAT64 well-known prefix 64:ff9b::/96.
	if b[0] == 0x00 && b[1] == 0x64 && b[2] == 0xff && b[3] == 0x9b &&
		b[4] == 0 && b[5] == 0 && b[6] == 0 && b[7] == 0 &&
		b[8] == 0 && b[9] == 0 && b[10] == 0 && b[11] == 0 {
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	}
	// IPv4-compatible ::/96 (deprecated), excluding :: and ::1.
	allZeroHigh := true
	for i := 0; i < 12; i++ {
		if b[i] != 0 {
			allZeroHigh = false
			break
		}
	}
	if allZeroHigh && !(b[12] == 0 && b[13] == 0 && b[14] == 0 && (b[15] == 0 || b[15] == 1)) {
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	}
	return netip.Addr{}, false
}

// permitted applies the mode's address policy to ONE normalized address.
func (d *pinnedDialer) permitted(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap()
	// Classify any embedded-v4 transition form by the v4 policy too.
	if a.Is6() {
		if e, ok := embeddedV4(a); ok && !d.permitted(e) {
			return false
		}
	}
	if d.loopbackMode {
		return a.IsLoopback()
	}
	// Production public floor: reject every non-public class.
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
// refuses the whole resolution if any answer fails (no pick-the-good-one),
// journals the decision, then dials one pinned literal IP. On the PERMITTED
// path a receipt-append failure FAILS THE DIAL CLOSED (E11 requires the
// receipt). TLS (SNI + verification) is done by the http.Transport against
// the ORIGINAL hostname, so pinning the IP never downgrades verification.
func (d *pinnedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("telegram egress: malformed dial address")
	}
	refuse := func(reason string, resolved []netip.Addr) (net.Conn, error) {
		// A refusal is pre-wire (nothing dialed). The receipt-append error, if
		// any, is joined so the audit failure is not silently dropped (F1).
		if aerr := d.emit(egressDecision{Host: host, Resolved: resolved, Reason: reason}); aerr != nil {
			return nil, fmt.Errorf("telegram egress: %s (receipt append failed: %v): %w", reason, aerr, errEgressPreWire)
		}
		return nil, fmt.Errorf("telegram egress: %s: %w", reason, errEgressPreWire)
	}
	if !strings.EqualFold(host, d.apiHost) {
		return refuse("host not permitted", nil)
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
	// PERMITTED: the receipt must be durable BEFORE the connection happens.
	if err := d.emit(egressDecision{Host: host, Resolved: norm, Pinned: pinned, Allowed: true}); err != nil {
		return nil, fmt.Errorf("telegram egress: receipt not durable, refusing dial: %v: %w", err, errEgressPreWire)
	}
	return d.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
}

// newPinnedClient builds the E11-compliant Telegram client: Proxy:nil (ambient
// proxy env ignored), the pinning DialContext, and reject-all redirects. In
// PRODUCTION mode the API host must be admitted by egressAllow (deny-default);
// in the validated loopback-override mode egressAllow is not consulted (the
// loopback endpoint is the config-validated local/test override). A nil
// resolve/dial defaults to the stdlib resolver/dialer.
func newPinnedClient(apiBase string, egressAllow []string, timeout time.Duration,
	resolve func(context.Context, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
	receipt func(egressDecision) error) (*http.Client, error) {
	u, err := url.Parse(apiBase)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("telegram: invalid api base %q", apiBase)
	}
	host := u.Hostname()
	loopback := egressModeForBase(apiBase)
	if !loopback {
		admitted := false
		for _, h := range egressAllow {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				admitted = true
				break
			}
		}
		if !admitted {
			return nil, fmt.Errorf("telegram: api host %q is not in egress_allow (deny-default)", host)
		}
	}
	if resolve == nil {
		r := net.DefaultResolver
		resolve = func(ctx context.Context, h string) ([]netip.Addr, error) {
			return r.LookupNetIP(ctx, "ip", h)
		}
	}
	if dial == nil {
		nd := &net.Dialer{Timeout: 30 * time.Second}
		dial = nd.DialContext
	}
	d := &pinnedDialer{
		loopbackMode: loopback, apiHost: host,
		resolve: resolve, dial: dial, receipt: receipt,
	}
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
			return fmt.Errorf("telegram: redirects are not followed")
		},
	}, nil
}

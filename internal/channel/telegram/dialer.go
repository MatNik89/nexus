package telegram

// E11 egress boundary for the Telegram client (PLAN-TG-EGRESS-DIALER.md):
// a pinned-IP, proxy-sanitized, redirect-rejecting dialer. The bot token
// rides in the URL path, so every connection must reach ONLY the configured
// Telegram host and never a rebind/proxy peer.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// egressDecision is the typed receipt of one dial attempt (the E11 honest
// data-flow evidence). Refusals always carry a Reason; a permitted connect
// carries the Pinned address.
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
	receipt      func(egressDecision)
}

func (d *pinnedDialer) emit(dec egressDecision) {
	if d.receipt != nil {
		d.receipt(dec)
	}
}

// permitted applies the mode's address policy to ONE normalized address.
func (d *pinnedDialer) permitted(a netip.Addr) bool {
	if !a.IsValid() {
		return false
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
	// CGNAT 100.64.0.0/10 is not covered by IsPrivate.
	if a.Is4() {
		b := a.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return false
		}
	}
	return true
}

// DialContext resolves the host, applies the mode policy to EVERY answer,
// refuses the whole resolution if any answer fails (no pick-the-good-one),
// then dials one pinned literal IP. TLS (SNI + verification) is done by the
// http.Transport against the ORIGINAL hostname, so pinning the IP never
// downgrades certificate validation.
func (d *pinnedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("telegram egress: malformed dial address")
	}
	if !strings.EqualFold(host, d.apiHost) {
		d.emit(egressDecision{Host: host, Reason: "host not permitted"})
		return nil, fmt.Errorf("telegram egress: host not permitted")
	}
	addrs, err := d.resolve(ctx, host)
	if err != nil {
		d.emit(egressDecision{Host: host, Reason: "resolve failed"})
		return nil, fmt.Errorf("telegram egress: resolve: %w", err)
	}
	norm := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		norm = append(norm, a.Unmap())
	}
	if len(norm) == 0 {
		d.emit(egressDecision{Host: host, Reason: "no addresses"})
		return nil, fmt.Errorf("telegram egress: no addresses resolved")
	}
	for _, a := range norm {
		if !d.permitted(a) {
			d.emit(egressDecision{Host: host, Resolved: norm, Reason: "policy rejected " + a.String()})
			return nil, fmt.Errorf("telegram egress: resolved address rejected by policy")
		}
	}
	pinned := norm[0]
	d.emit(egressDecision{Host: host, Resolved: norm, Pinned: pinned, Allowed: true})
	return d.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
}

// newPinnedClient builds the E11-compliant Telegram client: Proxy:nil (ambient
// proxy env ignored), the pinning DialContext, and reject-all redirects. A nil
// resolve/dial defaults to the stdlib resolver/dialer; a nil receipt drops the
// evidence (callers wire it to the journal).
func newPinnedClient(apiBase string, timeout time.Duration,
	resolve func(context.Context, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
	receipt func(egressDecision)) (*http.Client, error) {
	u, err := url.Parse(apiBase)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("telegram: invalid api base %q", apiBase)
	}
	if resolve == nil {
		r := net.DefaultResolver
		resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return r.LookupNetIP(ctx, "ip", host)
		}
	}
	if dial == nil {
		nd := &net.Dialer{Timeout: 30 * time.Second}
		dial = nd.DialContext
	}
	d := &pinnedDialer{
		loopbackMode: egressModeForBase(apiBase),
		apiHost:      u.Hostname(),
		resolve:      resolve, dial: dial, receipt: receipt,
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

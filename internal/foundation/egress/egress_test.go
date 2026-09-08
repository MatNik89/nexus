//go:build linux

package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func recordDial(target *string) func(context.Context, string, string) (net.Conn, error) {
	return func(_ context.Context, _ string, addr string) (net.Conn, error) {
		*target = addr
		c, _ := net.Pipe()
		return c, nil
	}
}

func staticResolver(addrs ...string) func(context.Context, string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, netip.MustParseAddr(a))
	}
	return func(context.Context, string) ([]netip.Addr, error) { return out, nil }
}

func collect(rc *[]Decision) ReceiptSink {
	return func(d Decision) error { *rc = append(*rc, d); return nil }
}

func newTestDialer(loopback bool, resolve func(context.Context, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error), rc *[]Decision) *pinnedDialer {
	return &pinnedDialer{component: "test", loopbackMode: loopback,
		endpoint: Endpoint{Scheme: "https", Host: "api.telegram.org", Port: 443},
		resolve:  resolve, dial: dial, receipt: collect(rc)}
}

func TestDialerProductionAllPublicPins(t *testing.T) {
	var got string
	var rc []Decision
	d := newTestDialer(false, staticResolver("149.154.167.220"), recordDial(&got), &rc)
	conn, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443")
	if err != nil {
		t.Fatalf("public dial refused: %v", err)
	}
	conn.Close()
	if got != "149.154.167.220:443" {
		t.Fatalf("did not pin the public IP, dialed %q", got)
	}
	if len(rc) != 1 || !rc[0].Allowed || rc[0].Component != "test" || rc[0].Port != 443 {
		t.Fatalf("expected one allowed, component-tagged receipt, got %+v", rc)
	}
}

func TestDialerRejectsMetadataAndMappedForm(t *testing.T) {
	for _, meta := range []string{"169.254.169.254", "::ffff:169.254.169.254"} {
		var got string
		var rc []Decision
		d := newTestDialer(false, staticResolver("149.154.167.220", meta), recordDial(&got), &rc)
		_, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443")
		if err == nil || !errorsIsPreWire(err) {
			t.Fatalf("mixed set with %s was not refused pre-wire: %v", meta, err)
		}
		if got != "" {
			t.Fatalf("dialed %q despite a poisoned set containing %s", got, meta)
		}
		if len(rc) != 1 || rc[0].Allowed {
			t.Fatalf("expected one refuse receipt for %s, got %+v", meta, rc)
		}
	}
}

func errorsIsPreWire(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrPreWire.Error())
}

// The deny floor is ONE table shared by every component (plan A detector 7).
func TestDialerProductionDenyFloor(t *testing.T) {
	for _, bad := range []string{"127.0.0.1", "10.0.0.5", "172.16.0.1", "192.168.1.1", "100.64.0.1", "fc00::1", "fe80::1", "0.0.0.0", "192.0.2.1", "198.18.0.1", "2001:db8::1", "64:ff9b:1::1"} {
		for _, component := range []string{"provider", "telegram"} {
			var got string
			var rc []Decision
			d := newTestDialer(false, staticResolver(bad), recordDial(&got), &rc)
			d.component = component
			if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
				t.Fatalf("%s: production deny floor let %s through", component, bad)
			}
			if got != "" {
				t.Fatalf("%s: dialed forbidden %s", component, bad)
			}
		}
	}
}

func TestDialerLoopbackMode(t *testing.T) {
	for _, ok := range []string{"127.0.0.1", "::1"} {
		var got string
		var rc []Decision
		d := newTestDialer(true, staticResolver(ok), recordDial(&got), &rc)
		if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err != nil {
			t.Fatalf("loopback mode refused %s: %v", ok, err)
		}
	}
	var got string
	var rc []Decision
	d := newTestDialer(true, staticResolver("127.0.0.1", "149.154.167.220"), recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("loopback mode accepted a mixed set")
	}
}

// The dialer is bound to ONE endpoint unit: a different host OR a different
// port on the same host is refused before any resolution.
func TestDialerEndpointNotPermitted(t *testing.T) {
	for _, addr := range []string{"evil.example.com:443", "api.telegram.org:8443"} {
		var got string
		var rc []Decision
		d := newTestDialer(false, staticResolver("149.154.167.220"), recordDial(&got), &rc)
		if _, err := d.DialContext(context.Background(), "tcp", addr); err == nil {
			t.Fatalf("dialer accepted %s", addr)
		}
		if got != "" {
			t.Fatalf("dialed %q for %s", got, addr)
		}
	}
}

func TestPinnedClientNoProxyNoRedirectNoNilSink(t *testing.T) {
	if _, err := NewPinnedClient("provider", "https://api.telegram.org", []string{"api.telegram.org"}, 0, Options{Resolve: staticResolver("149.154.167.220")}, nil); err == nil {
		t.Fatal("nil receipt sink accepted — an unreceipted egress mode exists")
	}
	var rc []Decision
	c, err := NewPinnedClient("provider", "https://api.telegram.org", []string{"api.telegram.org"}, 0, Options{Resolve: staticResolver("149.154.167.220")}, collect(&rc))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("transport must set Proxy:nil (ambient proxy ignored)")
	}
	if c.CheckRedirect == nil || c.CheckRedirect(nil, nil) == nil {
		t.Fatalf("client must reject every redirect")
	}
}

func TestDialerRejectsNAT64Metadata(t *testing.T) {
	var got string
	var rc []Decision
	d := newTestDialer(false, staticResolver("64:ff9b::a9fe:a9fe"), recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("NAT64-embedded metadata IP was not refused")
	}
	if got != "" {
		t.Fatalf("dialed NAT64-embedded metadata %q", got)
	}
}

// A permitted dial whose receipt cannot be journaled FAILS CLOSED, for every
// component: no connection without the durable E11 receipt (plan A #4).
func TestDialerReceiptFailClosed(t *testing.T) {
	for _, component := range []string{"provider", "telegram"} {
		var got string
		d := &pinnedDialer{component: component, endpoint: Endpoint{Scheme: "https", Host: "api.telegram.org", Port: 443},
			resolve: staticResolver("149.154.167.220"), dial: recordDial(&got),
			receipt: func(Decision) error { return fmt.Errorf("journal down") }}
		_, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443")
		if err == nil || !errorsIsPreWire(err) {
			t.Fatalf("%s: permitted dial proceeded despite a failed receipt: %v", component, err)
		}
		if got != "" {
			t.Fatalf("%s: dialed %q despite the receipt not being durable", component, got)
		}
	}
}

func TestDialerSequentialRebind(t *testing.T) {
	calls := 0
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		calls++
		if calls == 1 {
			return []netip.Addr{netip.MustParseAddr("149.154.167.220")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	var got string
	var rc []Decision
	d := newTestDialer(false, resolve, recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err != nil {
		t.Fatalf("first (admitted) dial refused: %v", err)
	}
	got = ""
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("rebind to a metadata IP on the second resolve was not refused")
	}
	if got != "" {
		t.Fatalf("dialed a rebound metadata IP %q", got)
	}
}

func TestPinnedClientEgressAllowDenyDefault(t *testing.T) {
	var rc []Decision
	opts := Options{Resolve: staticResolver("149.154.167.220")}
	if _, err := NewPinnedClient("telegram", "https://api.telegram.org", nil, 0, opts, collect(&rc)); err == nil {
		t.Fatalf("empty egress_allow admitted the production host (must deny-default)")
	}
	if _, err := NewPinnedClient("telegram", "https://api.telegram.org", []string{"api.deepseek.com"}, 0, opts, collect(&rc)); err == nil {
		t.Fatalf("egress_allow without the API host admitted it")
	}
	if _, err := NewPinnedClient("telegram", "https://api.telegram.org", []string{"api.telegram.org"}, 0, opts, collect(&rc)); err != nil {
		t.Fatalf("admitted host rejected: %v", err)
	}
	if _, err := NewPinnedClient("telegram", "http://127.0.0.1:8081", nil, 0, Options{Resolve: staticResolver("127.0.0.1")}, collect(&rc)); err != nil {
		t.Fatalf("loopback override should not require egress_allow: %v", err)
	}
}

// Canonical endpoint unit (plan A detector 8): explicit non-default port only
// via a matching host:port entry; default-port forms are the same unit; case
// is normalized; same host + wrong port is denied.
func TestEndpointUnitAndAllowlist(t *testing.T) {
	cases := []struct {
		base  string
		allow []string
		want  bool
	}{
		{"https://provider.example:8443", []string{"provider.example:8443"}, true},
		{"https://provider.example:8443", []string{"provider.example"}, false}, // entry means :443
		{"https://provider.example", []string{"provider.example"}, true},
		{"https://provider.example", []string{"provider.example:443"}, true},
		{"https://provider.example:443", []string{"provider.example"}, true},
		{"https://Provider.EXAMPLE", []string{"provider.example"}, true},
		{"https://provider.example", []string{"provider.example:8443"}, false},
		{"http://provider.example", []string{"provider.example"}, true}, // http default 80
		{"http://provider.example", []string{"provider.example:443"}, false},
		{"https://provider.example", []string{"other.example", " provider.example "}, true},
	}
	for _, tc := range cases {
		ep, err := ParseEndpoint(tc.base)
		if err != nil {
			t.Fatalf("%s: %v", tc.base, err)
		}
		if got := Admitted(ep, tc.allow); got != tc.want {
			t.Fatalf("%s vs %v: admitted=%v want %v (canonical %s)", tc.base, tc.allow, got, tc.want, ep.Canonical())
		}
	}
	ep, _ := ParseEndpoint("https://Provider.EXAMPLE")
	if ep.Canonical() != "provider.example:443" {
		t.Fatalf("canonical = %q", ep.Canonical())
	}
	for _, bad := range []string{"ftp://x", "https://", "https://h:99999", "://x"} {
		if _, err := ParseEndpoint(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if ep, _ := ParseEndpoint("http://127.attacker.example"); ep.Loopback() {
		t.Fatal("hostname with a 127. prefix classified as loopback")
	}
}

func TestPinnedClientTLSVerificationOn(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	var rc []Decision
	c, err := NewPinnedClient("test", srv.URL, nil, 5*time.Second, Options{}, collect(&rc))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tr := c.Transport.(*http.Transport)
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify must never be set on the egress client")
	}
	_, err = c.Get(srv.URL)
	if err == nil {
		t.Fatalf("client accepted an untrusted certificate - TLS verification is bypassed")
	}
	if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "x509") {
		t.Fatalf("expected a certificate verification error, got: %v", err)
	}
}

// Every forbidden v4 class through every standardized IPv6 embedding is
// refused with no dial ("metadata blocked in EVERY encoding", E11).
func TestDialerRejectsForbiddenV4EveryEncoding(t *testing.T) {
	forbidden := map[string][4]byte{
		"metadata": {169, 254, 169, 254}, "loopback": {127, 0, 0, 1},
		"rfc1918": {10, 0, 0, 1}, "cgnat": {100, 64, 0, 1},
	}
	embed := []netip.Prefix{netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("::ffff:0:0:0/96")}
	for name, v4 := range forbidden {
		cands := []netip.Addr{netip.AddrFrom4(v4),
			netip.AddrFrom16([16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, v4[0], v4[1], v4[2], v4[3]})}
		for _, p := range embed {
			pb := p.Addr().As16()
			pb[12], pb[13], pb[14], pb[15] = v4[0], v4[1], v4[2], v4[3]
			cands = append(cands, netip.AddrFrom16(pb))
		}
		for _, a := range cands {
			var got string
			var rc []Decision
			ans := a
			d := newTestDialer(false, func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{ans}, nil }, recordDial(&got), &rc)
			if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
				t.Fatalf("%s admitted via encoding %s", name, a)
			}
			if got != "" {
				t.Fatalf("%s dialed via encoding %s -> %s", name, a, got)
			}
		}
	}
}

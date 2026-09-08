package telegram

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

// recordDial captures the address the pinned dialer actually connects to and
// returns a closed pipe end so the caller has a usable net.Conn.
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

func newTestDialer(loopback bool, resolve func(context.Context, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error), rc *[]egressDecision) *pinnedDialer {
	return &pinnedDialer{
		loopbackMode: loopback, apiHost: "api.telegram.org",
		resolve: resolve, dial: dial,
		receipt: func(d egressDecision) { *rc = append(*rc, d) },
	}
}

func TestDialerProductionAllPublicPins(t *testing.T) {
	var got string
	var rc []egressDecision
	d := newTestDialer(false, staticResolver("149.154.167.220"), recordDial(&got), &rc)
	conn, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443")
	if err != nil {
		t.Fatalf("public dial refused: %v", err)
	}
	conn.Close()
	if got != "149.154.167.220:443" {
		t.Fatalf("did not pin the public IP, dialed %q", got)
	}
	if len(rc) != 1 || !rc[0].Allowed {
		t.Fatalf("expected one allowed receipt, got %+v", rc)
	}
}

func TestDialerRejectsMetadataAndMappedForm(t *testing.T) {
	// The cloud metadata IP in plain v4 and its IPv4-mapped-IPv6 encoding must
	// both classify as link-local and be refused (plan MIXED-SET / normalize).
	for _, meta := range []string{"169.254.169.254", "::ffff:169.254.169.254"} {
		var got string
		var rc []egressDecision
		d := newTestDialer(false, staticResolver("149.154.167.220", meta), recordDial(&got), &rc)
		_, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443")
		if err == nil {
			t.Fatalf("mixed set with %s was not refused", meta)
		}
		if got != "" {
			t.Fatalf("dialed %q despite a poisoned set containing %s", got, meta)
		}
		if len(rc) != 1 || rc[0].Allowed {
			t.Fatalf("expected one refuse receipt for %s, got %+v", meta, rc)
		}
	}
}

func TestDialerProductionDenyFloor(t *testing.T) {
	for _, bad := range []string{"127.0.0.1", "10.0.0.5", "172.16.0.1", "192.168.1.1", "100.64.0.1", "fc00::1", "fe80::1", "0.0.0.0"} {
		var got string
		var rc []egressDecision
		d := newTestDialer(false, staticResolver(bad), recordDial(&got), &rc)
		if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
			t.Fatalf("production deny floor let %s through", bad)
		}
		if got != "" {
			t.Fatalf("dialed forbidden %s", bad)
		}
	}
}

func TestDialerLoopbackMode(t *testing.T) {
	for _, ok := range []string{"127.0.0.1", "::1"} {
		var got string
		var rc []egressDecision
		d := newTestDialer(true, staticResolver(ok), recordDial(&got), &rc)
		if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err != nil {
			t.Fatalf("loopback mode refused %s: %v", ok, err)
		}
	}
	// mixed loopback+public refused in loopback mode
	var got string
	var rc []egressDecision
	d := newTestDialer(true, staticResolver("127.0.0.1", "149.154.167.220"), recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("loopback mode accepted a mixed set")
	}
}

func TestDialerHostNotPermitted(t *testing.T) {
	var got string
	var rc []egressDecision
	d := newTestDialer(false, staticResolver("149.154.167.220"), recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "evil.example.com:443"); err == nil {
		t.Fatalf("dialer accepted a non-API host")
	}
	if got != "" {
		t.Fatalf("dialed a non-API host: %q", got)
	}
}

func TestPinnedClientNoProxyNoRedirect(t *testing.T) {
	c, err := newPinnedClient("https://api.telegram.org", 0, staticResolver("149.154.167.220"), nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("transport must set Proxy:nil (ambient proxy ignored)")
	}
	if c.CheckRedirect == nil {
		t.Fatalf("client must reject redirects")
	}
	if err := c.CheckRedirect(nil, nil); err == nil {
		t.Fatalf("CheckRedirect must return an error for any redirect")
	}
}

func TestPinnedClientModeFromBase(t *testing.T) {
	// A loopback API base selects loopback mode; production base selects the
	// public floor. Proven by which resolver answer each accepts.
	if m := egressModeForBase("http://127.0.0.1:8081"); !m {
		t.Fatalf("loopback base must select loopback mode")
	}
	if m := egressModeForBase("https://api.telegram.org"); m {
		t.Fatalf("production base must not select loopback mode")
	}
}

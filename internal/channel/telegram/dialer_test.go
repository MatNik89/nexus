package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/kernel/journal"
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
		receipt: func(d egressDecision) error { *rc = append(*rc, d); return nil },
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
	c, err := newPinnedClient("https://api.telegram.org", []string{"api.telegram.org"}, 0, staticResolver("149.154.167.220"), nil, nil)
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
	// A loopback API base selects loopback mode; a production base does not.
	if m := egressModeForBase("http://127.0.0.1:8081"); !m {
		t.Fatalf("loopback base must select loopback mode")
	}
	if m := egressModeForBase("https://api.telegram.org"); m {
		t.Fatalf("production base must not select loopback mode")
	}
}

func TestDialerRejectsNAT64Metadata(t *testing.T) {
	// The NAT64 well-known prefix embedding the metadata IP
	// (64:ff9b::169.254.169.254) must be refused (E11: every encoding).
	var got string
	var rc []egressDecision
	d := newTestDialer(false, staticResolver("64:ff9b::a9fe:a9fe"), recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("NAT64-embedded metadata IP was not refused")
	}
	if got != "" {
		t.Fatalf("dialed NAT64-embedded metadata %q", got)
	}
}

func TestDialerReceiptFailClosed(t *testing.T) {
	// A permitted dial whose receipt cannot be journaled must FAIL CLOSED:
	// no connection may happen without the durable E11 receipt.
	var got string
	d := &pinnedDialer{
		loopbackMode: false, apiHost: "api.telegram.org",
		resolve: staticResolver("149.154.167.220"),
		dial:    recordDial(&got),
		receipt: func(egressDecision) error { return fmt.Errorf("journal down") },
	}
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("permitted dial proceeded despite a failed receipt")
	}
	if got != "" {
		t.Fatalf("dialed %q despite the receipt not being durable", got)
	}
}

func TestDialerSequentialRebind(t *testing.T) {
	// A resolver that answers an admitted IP first and an off-policy IP on a
	// later call: the second DialContext must refuse (each connection is
	// re-classified, no cached admission).
	calls := 0
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		calls++
		if calls == 1 {
			return []netip.Addr{netip.MustParseAddr("149.154.167.220")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	var got string
	var rc []egressDecision
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
	// Production base whose host is NOT in egress_allow -> refuse to build
	// (deny-default). With the host admitted -> builds.
	if _, err := newPinnedClient("https://api.telegram.org", nil, 0, staticResolver("149.154.167.220"), nil, nil); err == nil {
		t.Fatalf("empty egress_allow admitted the production host (must deny-default)")
	}
	if _, err := newPinnedClient("https://api.telegram.org", []string{"api.deepseek.com"}, 0, staticResolver("149.154.167.220"), nil, nil); err == nil {
		t.Fatalf("egress_allow without the API host admitted it")
	}
	if _, err := newPinnedClient("https://api.telegram.org", []string{"api.telegram.org"}, 0, staticResolver("149.154.167.220"), nil, nil); err != nil {
		t.Fatalf("admitted host rejected: %v", err)
	}
	// Loopback override does not consult egress_allow (validated local endpoint).
	if _, err := newPinnedClient("http://127.0.0.1:8081", nil, 0, staticResolver("127.0.0.1"), nil, nil); err != nil {
		t.Fatalf("loopback override should not require egress_allow: %v", err)
	}
}

func TestDialerRejectsRFC8215Local(t *testing.T) {
	// RFC8215 64:ff9b:1::/48 is explicitly local-use, not globally reachable.
	var got string
	var rc []egressDecision
	d := newTestDialer(false, staticResolver("64:ff9b:1::1"), recordDial(&got), &rc)
	if _, err := d.DialContext(context.Background(), "tcp", "api.telegram.org:443"); err == nil {
		t.Fatalf("RFC8215 local-use prefix admitted in production")
	}
	if got != "" {
		t.Fatalf("dialed RFC8215 address %q", got)
	}
}

func TestEgressRefusalIsPreWire(t *testing.T) {
	// A dialer refusal wrapped the way http.Client returns it (url.Error) must
	// classify pre-wire, so the outbox re-pends PENDING and never strands the
	// row UNKNOWN.
	base := fmt.Errorf("telegram egress: resolved address rejected by policy: %w", errEgressPreWire)
	wrapped := &url.Error{Op: "Post", URL: "https://api.telegram.org/botX/getUpdates", Err: base}
	if !isPreWire(wrapped) {
		t.Fatalf("egress refusal not classified pre-wire (would strand the outbox UNKNOWN)")
	}
}

func TestPinnedClientTLSVerificationOn(t *testing.T) {
	// The pinned dial must NOT downgrade TLS. Two proofs: no InsecureSkipVerify,
	// and a handshake to a server with an UNTRUSTED cert is REJECTED (if
	// verification were bypassed this would succeed). This is the wrong-cert
	// detector: pinning the literal IP does not skip hostname/chain checks.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	c, err := newPinnedClient(srv.URL, nil, 5*time.Second, nil, nil, nil)
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

func TestEgressReceiptJournaled(t *testing.T) {
	// A real request through the pinned client must leave a durable, complete
	// egress receipt in the journal, carrying the resolved set + pin and NEVER
	// the bot token. Proves Adapter.egressReceipt -> Core.RecordEgress wiring.
	h := build(t, map[int64]string{42: "work"})
	_ = h.a.PollOnce(ctxT()) // a getUpdates dial happens regardless of the reply
	var allowed bool
	if err := h.j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType != channel.EvEgressAttempt {
			return nil
		}
		if strings.Contains(string(ev.Envelope.Payload), "123:token") {
			t.Fatalf("bot token leaked into an egress receipt")
		}
		var p struct {
			Host     string   `json:"host"`
			Resolved []string `json:"resolved"`
			Pinned   string   `json:"pinned"`
			Allowed  bool     `json:"allowed"`
		}
		if err := json.Unmarshal(ev.Envelope.Payload, &p); err != nil {
			return err
		}
		if p.Allowed && p.Pinned != "" && len(p.Resolved) > 0 {
			allowed = true
		}
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !allowed {
		t.Fatalf("no complete allowed egress receipt journaled after a poll")
	}
}

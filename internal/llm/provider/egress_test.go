//go:build linux

package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/egress"
)

// Slice A (AUDIT-FULL F1): the provider client used http.DefaultTransport —
// ambient proxy, unpinned DNS, no receipt — with the bearer key in flight.
// These detectors sit at the provider's OWN boundary: a poisoned resolution
// must produce ZERO sockets, a refused receipt (Allowed=false), and a
// definite pre-wire error.

func spyDial(count *int) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		*count++
		return nil, fmt.Errorf("spy: dial must not be reached")
	}
}

func staticResolve(addrs ...string) func(context.Context, string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, netip.MustParseAddr(a))
	}
	return func(context.Context, string) ([]netip.Addr, error) { return out, nil }
}

func prodConfig() (cfgOut struct {
	base  string
	allow []string
}) {
	return struct {
		base  string
		allow []string
	}{"https://api.provider.example", []string{"api.provider.example"}}
}

// Detector 1: metadata / RFC1918 answers -> refused, zero dials, refusal
// receipt journaled with the provider component.
func TestProviderEgressRefusesPoisonedResolution(t *testing.T) {
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	for _, poison := range []string{"169.254.169.254", "10.0.0.5", "::ffff:169.254.169.254", "64:ff9b::a9fe:a9fe"} {
		dials := 0
		var receipts []egress.Decision
		sink := func(d egress.Decision) error { receipts = append(receipts, d); return nil }
		pc := prodConfig()
		cfg := testConfig(t, pc.base)
		cfg.EgressAllow = pc.allow
		auth := newAuth()
		p, err := newAPIKeyWithEgress(cfg, auth, sink, egress.Options{Resolve: staticResolve("149.154.167.220", poison), Dial: spyDial(&dials)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-"+poison, p.Target()))
		if err == nil || !errors.Is(err, egress.ErrPreWire) {
			t.Fatalf("%s: poisoned resolution not refused pre-wire: %v", poison, err)
		}
		if dials != 0 {
			t.Fatalf("%s: %d socket dials despite a poisoned set", poison, dials)
		}
		if len(receipts) != 1 || receipts[0].Allowed || receipts[0].Component != "provider" || receipts[0].Reason == "" {
			t.Fatalf("%s: expected one provider refusal receipt, got %+v", poison, receipts)
		}
	}
}

// Detector 2: the ambient proxy is IGNORED — with HTTPS_PROXY set, the spy
// dialer sees the PINNED address, never the proxy.
func TestProviderEgressIgnoresAmbientProxy(t *testing.T) {
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	var dialed []string
	dial := func(_ context.Context, _ string, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, fmt.Errorf("spy: stop here")
	}
	pc := prodConfig()
	cfg := testConfig(t, pc.base)
	cfg.EgressAllow = pc.allow
	auth := newAuth()
	p, err := newAPIKeyWithEgress(cfg, auth, testSink, egress.Options{Resolve: staticResolve("149.154.167.220"), Dial: dial})
	if err != nil {
		t.Fatal(err)
	}
	_, cerr := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-proxy", p.Target()))
	if len(dialed) != 1 || dialed[0] != "149.154.167.220:443" {
		t.Fatalf("expected one dial to the pinned address, got %v (proxy env honored?); chat err: %v", dialed, cerr)
	}
	if tr, ok := p.client.Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("provider client transport is not the pinned owner with Proxy:nil")
	}
}

// Detector 3+4: a nil sink is refused at construction; a sink that cannot make
// the receipt durable produces ZERO dials.
func TestProviderEgressReceiptMandatoryAndFailClosed(t *testing.T) {
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	pc := prodConfig()
	cfg := testConfig(t, pc.base)
	cfg.EgressAllow = pc.allow
	if _, err := NewAPIKey(cfg, newAuth(), nil); err == nil {
		t.Fatal("provider built without a receipt sink")
	}
	dials := 0
	auth := newAuth()
	p, err := newAPIKeyWithEgress(cfg, auth, func(egress.Decision) error { return fmt.Errorf("journal down") },
		egress.Options{Resolve: staticResolve("149.154.167.220"), Dial: spyDial(&dials)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-r", p.Target()))
	if err == nil || !errors.Is(err, egress.ErrPreWire) {
		t.Fatalf("non-durable receipt did not fail the dial closed: %v", err)
	}
	if dials != 0 {
		t.Fatalf("%d dials despite a non-durable receipt", dials)
	}
}

// Detector 6 (F8): a VALID reply object completed below the cap followed by
// trailing bytes that push the total above it is REJECTED — a merely
// truncated object would not detect the LimitReader-then-decode bypass.
func TestProviderBodyCapRejectsValidObjectPlusTrailingBytes(t *testing.T) {
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	valid := `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`
	pad := strings.Repeat(" ", maxBodyBytes) // whitespace: json.Decoder would happily stop before it
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(valid))
		w.Write([]byte(pad))
	}))
	t.Cleanup(srv.Close)
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, srv.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-cap", p.Target()))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized body (valid prefix + padding) accepted: %v", err)
	}
	// Trailing NON-whitespace after a valid value below the cap is refused too.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(valid + `{"choices":[{"message":{"content":"smuggled"}}]}`))
	}))
	t.Cleanup(srv2.Close)
	p2, err := NewAPIKey(testConfig(t, srv2.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p2.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-trail", p2.Target())); err == nil {
		t.Fatal("two JSON values in one reply accepted")
	}
}

// Detector 6b (F8): a never-ending stream is cut at the transport ceiling with
// an error naming the ceiling; delivered content is reported INCOMPLETE.
func TestProviderStreamCutAtCeiling(t *testing.T) {
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, _ := w.(http.Flusher)
		chunk := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		var sent int64
		for sent <= maxStreamBytes+int64(len(chunk)) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			sent += int64(len(chunk))
			if fl != nil && sent%(1<<20) < int64(len(chunk)) {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, srv.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	var got int
	err = p.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-stream", p.Target()),
		func(string) error { got++; return nil })
	if err == nil || !strings.Contains(err.Error(), "ceiling") {
		t.Fatalf("endless stream not cut at the ceiling: %v (deltas %d)", err, got)
	}
}

// The S7 target and data descriptor use the ONE canonical endpoint unit.
func TestProviderTargetIsCanonicalEndpoint(t *testing.T) {
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	cfg := testConfig(t, "https://API.Provider.example")
	cfg.EgressAllow = []string{"api.provider.example"}
	p, err := NewAPIKey(cfg, newAuth(), testSink)
	if err != nil {
		t.Fatal(err)
	}
	if string(p.Target()) != "provider:api.provider.example:443" {
		t.Fatalf("target = %q", p.Target())
	}
	cfg8 := testConfig(t, "https://api.provider.example:8443")
	cfg8.EgressAllow = []string{"api.provider.example"} // means :443 — not this endpoint
	if _, err := NewAPIKey(cfg8, newAuth(), testSink); err == nil {
		t.Fatal("explicit non-default port admitted by a default-port entry")
	}
	cfg8.EgressAllow = []string{"api.provider.example:8443"}
	if _, err := NewAPIKey(cfg8, newAuth(), testSink); err != nil {
		t.Fatalf("host:port entry rejected: %v", err)
	}
}

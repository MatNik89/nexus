//go:build linux

// T15 RED table (tasks-P0): APIKey provider (OpenAI-compatible), egress
// bound to the config allowlist, structured output validate → re-ask(with
// AttemptGrant) → salvage — NEVER silent accept (SPEC P0.7); security/
// effect payloads strict (no tolerant accept); live probe registers into
// the T11 snapshot shape. Anchored to P0.7 + E12 provider block.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/egress"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

// testSink is the tests' receipt sink: it accepts every decision. Production
// wires channel.EgressSink; a nil sink is refused by the constructor.
func testSink(egress.Decision) error { return nil }

func testConfig(t *testing.T, baseURL string) config.Config {
	t.Helper()
	host := strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")
	return config.Config{
		ProviderBaseURL: baseURL,
		ProviderKeyEnv:  "NEXUS_TEST_KEY",
		ProviderModel:   "test-model",
		EgressAllow:     []string{host},
	}
}

func fakeOpenAI(t *testing.T, reply string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("missing bearer auth")
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["model"] != "test-model" {
			t.Errorf("model not honored: %v", req["model"])
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// Constructor is fail-closed: missing key env, empty model, and a base URL
// whose host is NOT in the egress allowlist are all refused.
func TestConstructorFailClosed(t *testing.T) {
	srv, _ := fakeOpenAI(t, "hi")
	cfg := testConfig(t, srv.URL)
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	if _, err := NewAPIKey(cfg, newAuth(), testSink); err != nil {
		t.Fatalf("valid construction refused: %v", err)
	}
	if _, err := NewAPIKey(cfg, nil, testSink); err == nil {
		t.Fatal("provider without an S7 authority accepted (ungoverned transport)")
	}
	t.Setenv("NEXUS_TEST_KEY", "")
	if _, err := NewAPIKey(cfg, newAuth(), testSink); err == nil {
		t.Fatal("empty API key accepted")
	}
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	noModel := cfg
	noModel.ProviderModel = ""
	if _, err := NewAPIKey(noModel, newAuth(), testSink); err == nil {
		t.Fatal("empty model accepted")
	}
	offList := cfg
	offList.EgressAllow = []string{"api.other.example"}
	if _, err := NewAPIKey(offList, newAuth(), testSink); err == nil {
		t.Fatal("provider host outside the egress allowlist accepted (kernel floor)")
	}
	badScheme := cfg
	badScheme.ProviderBaseURL = "ftp://x.example"
	if _, err := NewAPIKey(badScheme, newAuth(), testSink); err == nil {
		t.Fatal("non-http scheme accepted")
	}
	// Bearer key over plaintext http to a NON-loopback host is exposure.
	cleartext := cfg
	cleartext.ProviderBaseURL = "http://api.example.com"
	cleartext.EgressAllow = []string{"api.example.com"}
	if _, err := NewAPIKey(cleartext, newAuth(), testSink); err == nil {
		t.Fatal("plaintext http to a non-loopback host accepted")
	}
}

// Chat: OpenAI-compatible request wire, response content surfaced.
func TestChatRoundTrip(t *testing.T) {
	srv, calls := fakeOpenAI(t, "pong")
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, srv.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "ping"}}, issue(t, auth, "op-chat", p.Target()))
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "pong" || *calls != 1 {
		t.Fatalf("chat round trip broken: %+v calls=%d", out, *calls)
	}
	if caps := p.Capabilities(); caps.Model != "test-model" {
		t.Fatalf("capabilities: %+v", caps)
	}
	if dd := p.DataDescriptor(); len(dd.Hosts) != 1 || !dd.SendsConversation {
		t.Fatalf("data descriptor must name the egress host + conversation flow: %+v", dd)
	}
}

// A non-2xx or malformed provider reply is a typed error, never content.
func TestProviderErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream broken", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, srv.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-1", p.Target())); err == nil {
		t.Fatal("HTTP 502 surfaced as content")
	}
}

// THE Annex P0.2 transport oracle (Phase-2 codex #1/#2): the grant is
// consumed IN the transport — a second physical call on one grant fails
// ATTEMPT_NOT_AUTHORIZED, the HTTP counter stays 1, the S7 attempt
// counter stays 1.
func TestTransportConsumesGrantExactlyOnce(t *testing.T) {
	srv, calls := fakeOpenAI(t, "ok")
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, srv.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	g := issue(t, auth, "op-1", p.Target())
	msgs := []ChatMessage{{Role: "user", Content: "x"}}
	if _, err := p.Chat(context.Background(), msgs, g); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat(context.Background(), msgs, g); !errors.Is(err, s7.ErrAttemptNotAuthorized) {
		t.Fatalf("second physical call on one grant must fail ATTEMPT_NOT_AUTHORIZED: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("HTTP transport counter %d, must stay 1", *calls)
	}
	if n := auth.Attempts("op-1"); n != 1 {
		t.Fatalf("S7 attempt counter %d, must stay 1", n)
	}
	// A forged grant never reaches the wire at all.
	forged := s7.Grant{OperationID: "op-x", AttemptNo: 1, TargetID: p.Target(), Nonce: "deadbeef"}
	if _, err := p.Chat(context.Background(), msgs, forged); !errors.Is(err, s7.ErrAttemptNotAuthorized) {
		t.Fatalf("forged grant: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("forged grant reached the transport: %d", *calls)
	}
}

// EVERY redirect is refused in P0 — even to an allowlisted host: one
// grant authorizes exactly one physical request (Phase-2-r3 codex #2).
func TestRedirectOffAllowlistRefused(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"stolen"}}]}`)
	}))
	t.Cleanup(evil.Close)
	bouncer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/v1/chat/completions", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(bouncer.Close)
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, bouncer.URL), auth, testSink) // allowlist holds ONLY the bouncer
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-1", p.Target()))
	if err == nil || out.Content == "stolen" {
		t.Fatalf("redirect off the allowlist followed: %+v %v", out, err)
	}
}

// --- structured output (S2.3-min, SPEC P0.7) ---

type plan struct {
	Steps int `json:"steps"`
}

func validPlan(p plan) error {
	if p.Steps <= 0 {
		return fmt.Errorf("steps must be positive")
	}
	return nil
}

func extractor(t *testing.T) (*Extractor, *s7.Authority) {
	t.Helper()
	auth := s7.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	return NewExtractor(auth), auth
}

func newAuth() *s7.Authority {
	return s7.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
}

func issue(t *testing.T, a *s7.Authority, op string, target contracts.TargetID) s7.Grant {
	t.Helper()
	g, err := a.Issue(contracts.OperationID(op), target)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// The P0.7 literal: invalid output NEVER comes back as a value without an
// error — even when re-ask and salvage both fail.
func TestStructuredOutputNeverSilentAccept(t *testing.T) {
	e, auth := extractor(t)
	reasks := 0
	badReask := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		reasks++
		auth.Consume(g)
		return []byte(`still not json`), nil
	}
	_, err := Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`no json here at all`), validPlan, "op-1", "provider-a", badReask)
	if err == nil {
		t.Fatal("invalid structured output silently accepted")
	}
	if reasks != 1 {
		t.Fatalf("general-class invalid output must re-ask exactly once, did %d", reasks)
	}
	// A VALIDATION failure (well-formed JSON, bad content) also never
	// passes silently.
	_, err = Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`{"steps":0}`), validPlan, "op-2", "provider-a", badReask)
	if err == nil {
		t.Fatal("failed validation silently accepted")
	}
}

// Valid output passes with ZERO re-asks; unknown fields are rejected
// (strict decode), then recovered via re-ask. The fake transport CONSUMES
// the grant exactly as provider.Chat does.
func TestStructuredHappyAndReask(t *testing.T) {
	e, auth := extractor(t)
	reasks := 0
	goodReask := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		reasks++
		if err := auth.Consume(g); err != nil {
			t.Fatalf("re-ask ran without a consumable AttemptGrant (P0.2): %v", err)
		}
		return []byte(`{"steps":3}`), nil
	}
	got, err := Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`{"steps":2}`), validPlan, "op-1", "provider-a", goodReask)
	if err != nil || got.Steps != 2 || reasks != 0 {
		t.Fatalf("valid output must pass without re-ask: %+v %v reasks=%d", got, err, reasks)
	}
	got, err = Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`{"steps":1,"unknown_field":true}`), validPlan, "op-2", "provider-a", goodReask)
	if err != nil || got.Steps != 3 || reasks != 1 {
		t.Fatalf("re-ask recovery broken: %+v %v reasks=%d", got, err, reasks)
	}
	// The re-ask attempt is ACCOUNTED by S7 (one physical attempt).
	if auth.Attempts("op-2") != 1 {
		t.Fatalf("re-ask attempt not accounted: %d", auth.Attempts("op-2"))
	}
}

// Salvage: prose-wrapped JSON is recovered WITHOUT silently accepting the
// prose — the extracted object still validates.
func TestSalvageFromProseStillValidates(t *testing.T) {
	e, _ := extractor(t)
	badReask := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		return []byte(`nope`), nil
	}
	got, err := Extract[plan](context.Background(), e, ClassGeneral,
		[]byte("Sure! Here is the plan:\n```json\n{\"steps\":4}\n```\nEnjoy."),
		validPlan, "op-1", "provider-a", badReask)
	if err != nil || got.Steps != 4 {
		t.Fatalf("salvage broken: %+v %v", got, err)
	}
	// Salvaged-but-invalid content still fails.
	_, err = Extract[plan](context.Background(), e, ClassGeneral,
		[]byte("plan: {\"steps\":0}"), validPlan, "op-2", "provider-a", badReask)
	if err == nil {
		t.Fatal("salvaged content skipped validation")
	}
}

// SECURITY/EFFECT payloads: strict — a malformed discriminator reaches no
// sink, no re-ask, no salvage (P0.7 no-tolerant-accept clause).
func TestSecurityEffectClassStrictNoRepair(t *testing.T) {
	e, _ := extractor(t)
	reasks := 0
	reask := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		reasks++
		return []byte(`{"steps":1}`), nil
	}
	for _, class := range []PayloadClass{ClassSecurity, ClassEffect} {
		// Well-formed prose-wrapped JSON that salvage WOULD recover.
		_, err := Extract[plan](context.Background(), e, class,
			[]byte("ok: {\"steps\":2}"), validPlan, contracts.OperationID(fmt.Sprintf("op-%d", class)), "provider-a", reask)
		if err == nil {
			t.Fatalf("class %d: malformed security/effect payload repaired", class)
		}
	}
	if reasks != 0 {
		t.Fatalf("strict class attempted %d re-asks (tolerant accept)", reasks)
	}
}

// Nil validators are refused: "no validation" IS silent accept.
func TestNilValidatorRefused(t *testing.T) {
	e, _ := extractor(t)
	_, err := Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`{"steps":1}`), nil, "op-1", "provider-a",
		func(ctx context.Context, g s7.Grant) ([]byte, error) { return nil, nil })
	if err == nil {
		t.Fatal("nil validator accepted (silent-accept hole)")
	}
}

// The live probe registers into the T11 snapshot shape: reachable provider
// → Passed under the RESOLVED config's own hash; unreachable → failed,
// never a panic or a silent pass.
func TestProbeBindsResolvedConfig(t *testing.T) {
	srv, _ := fakeOpenAI(t, "ok")
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	cfg := testConfig(t, srv.URL)
	p, err := NewAPIKey(cfg, newAuth(), testSink)
	if err != nil {
		t.Fatal(err)
	}
	resolved := config.Resolved{Config: cfg}
	pr := p.Probe(context.Background(), resolved)
	if pr.Name != "provider" || !pr.Passed || pr.ConfigHash != resolved.ConfigHash() {
		t.Fatalf("probe not bound to the resolved config: %+v", pr)
	}
	srv.Close()
	pr = p.Probe(context.Background(), resolved)
	if pr.Passed {
		t.Fatal("unreachable provider probed as PASSED")
	}
}

// Stream: deltas arrive in order; a stream truncated before [DONE] is an
// ERROR (partial content must never pass as a full reply).
func TestStreamOrderAndTruncationHonesty(t *testing.T) {
	full := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"llo\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(full.Close)
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, full.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	if err := p.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}},
		issue(t, auth, "op-s1", p.Target()), func(d string) error { got.WriteString(d); return nil }); err != nil {
		t.Fatal(err)
	}
	if got.String() != "hello" {
		t.Fatalf("stream deltas broken: %q", got.String())
	}
	truncated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n")
		// connection closes WITHOUT [DONE]
	}))
	t.Cleanup(truncated.Close)
	p2, err := NewAPIKey(testConfig(t, truncated.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}},
		issue(t, auth, "op-s2", p2.Target()), func(string) error { return nil }); err == nil {
		t.Fatal("truncated stream passed as complete")
	}
}

// A re-ask callback CANNOT self-retry: the transport consumes the grant,
// so a second physical call inside one callback dies before the wire —
// transport counter 1, S7 attempt counter 1 (Phase-2 codex #2 oracle).
func TestReaskCallbackCannotSelfRetry(t *testing.T) {
	e, auth := extractor(t)
	transport := 0
	fakeTransport := func(g s7.Grant) ([]byte, error) {
		if err := auth.Consume(g); err != nil {
			return nil, err
		}
		transport++
		return []byte(`not json either`), nil
	}
	rogue := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		out, _ := fakeTransport(g)
		if _, err := fakeTransport(g); err == nil {
			t.Fatal("second physical call on one grant authorized")
		}
		return out, nil
	}
	_, err := Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`garbage`), validPlan, "op-1", "provider-a", rogue)
	if err == nil {
		t.Fatal("garbage accepted")
	}
	if transport != 1 {
		t.Fatalf("transport counter %d, must stay 1", transport)
	}
	if auth.Attempts("op-1") != 1 {
		t.Fatalf("S7 attempt counter %d, must stay 1", auth.Attempts("op-1"))
	}
}

// The LITERAL closed-discriminator fixture (Phase-2 codex #14): an
// unknown EFFECT/POLICY discriminator reaches NO sink — zero re-asks,
// zero sink invocations, immediate reject.
func TestUnknownEffectDiscriminatorReachesNoSink(t *testing.T) {
	type effectDirective struct {
		Kind string `json:"kind"`
	}
	sink := 0
	validateClosed := func(d effectDirective) error {
		switch d.Kind {
		case "send_message", "set_reminder":
			sink++ // a real consumer would dispatch here
			return nil
		}
		return fmt.Errorf("unknown effect discriminator (fail closed)")
	}
	e, auth := extractor(t)
	reasks := 0
	reask := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		reasks++
		auth.Consume(g)
		return []byte(`{"kind":"send_message"}`), nil
	}
	for name, raw := range map[string]string{
		"unknown-kind":  `{"kind":"wipe_disk"}`,
		"prose-wrapped": `sure: {"kind":"send_message"}`,
		"not-json":      `just do it`,
	} {
		_, err := Extract[effectDirective](context.Background(), e, ClassEffect,
			[]byte(raw), validateClosed, contracts.OperationID("op-"+name), "provider-a", reask)
		if err == nil {
			t.Fatalf("%s: malformed effect payload accepted", name)
		}
	}
	if sink != 0 || reasks != 0 {
		t.Fatalf("malformed effect payload reached a sink (sink=%d reasks=%d)", sink, reasks)
	}
}

// A grant minted for a DIFFERENT target never reaches this provider's
// wire (Phase-2-r2 codex #2), and an IP-class check governs the
// plaintext-loopback exception (r2 codex #11): "127.attacker.example" is
// a remote DNS name, not loopback.
func TestProviderTargetBindingAndLoopbackClass(t *testing.T) {
	srv, calls := fakeOpenAI(t, "ok")
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	p, err := NewAPIKey(testConfig(t, srv.URL), auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	foreign := issue(t, auth, "op-f", "provider:somewhere-else")
	if _, err := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, foreign); !errors.Is(err, s7.ErrAttemptNotAuthorized) {
		t.Fatalf("foreign-target grant reached the wire: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("foreign-target grant produced %d transport calls", *calls)
	}
	fake127 := config.Config{
		ProviderBaseURL: "http://127.attacker.example", ProviderKeyEnv: "NEXUS_TEST_KEY",
		ProviderModel: "m", EgressAllow: []string{"127.attacker.example"},
	}
	if _, err := NewAPIKey(fake127, newAuth(), testSink); err == nil {
		t.Fatal("hostname with a 127. prefix accepted as loopback (bearer key over cleartext)")
	}
}

// The re-ask state machine leaks nothing (Phase-2-r2 codex #3):
// consumed-then-error lands FAILED (never a permanent RUNNING), and an
// UNCONSUMED callback's bytes are ungoverned — ignored, operation
// CANCELLED, value never accepted from them.
func TestReaskStateNeverLeaks(t *testing.T) {
	// (a) consumed, then network error.
	e, auth := extractor(t)
	consumedThenError := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		if err := auth.Consume(g); err != nil {
			t.Fatal(err)
		}
		return nil, fmt.Errorf("connection reset")
	}
	_, err := Extract[plan](context.Background(), e, ClassGeneral,
		[]byte(`garbage`), validPlan, "op-a", "provider-a", consumedThenError)
	if err == nil {
		t.Fatal("garbage accepted")
	}
	if st, _ := auth.State("op-a"); st != contracts.AttemptFailed {
		t.Fatalf("consumed-then-error attempt leaked in state %v (want FAILED)", st)
	}
	// (b) callback returns VALID bytes without consuming the grant.
	e2, auth2 := extractor(t)
	ungoverned := func(ctx context.Context, g s7.Grant) ([]byte, error) {
		return []byte(`{"steps":7}`), nil // never touched the transport
	}
	_, err = Extract[plan](context.Background(), e2, ClassGeneral,
		[]byte(`garbage`), validPlan, "op-b", "provider-a", ungoverned)
	if err == nil {
		t.Fatal("ungoverned callback bytes accepted as a transport result")
	}
	if st, _ := auth2.State("op-b"); st != contracts.AttemptCancelled {
		t.Fatalf("ungoverned re-ask operation in state %v (want CANCELLED)", st)
	}
}

// A redirect to a host ON the allowlist is refused too: it would be a
// second physical request under one consumed grant.
func TestAllowlistedRedirectStillRefused(t *testing.T) {
	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls++
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"two-hops"}}]}`)
	}))
	t.Cleanup(backend.Close)
	bouncer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, backend.URL+"/v1/chat/completions", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(bouncer.Close)
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	auth := newAuth()
	cfg := testConfig(t, bouncer.URL)
	cfg.EgressAllow = append(cfg.EgressAllow, strings.TrimPrefix(backend.URL, "http://")) // BOTH allowlisted
	p, err := NewAPIKey(cfg, auth, testSink)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}, issue(t, auth, "op-1", p.Target()))
	if err == nil {
		t.Fatal("allowlisted redirect followed (two physical requests on one grant)")
	}
	if backendCalls != 0 {
		t.Fatalf("redirect target received %d requests under one grant", backendCalls)
	}
}

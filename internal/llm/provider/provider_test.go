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

// gen builds a Generate callback: attempt 1 returns first, attempt 2 returns
// second; it CONSUMES the grant exactly as provider.Chat does and counts
// physical calls.
func gen(t *testing.T, auth *s7.Authority, calls *int, first, second string) Generate {
	return func(ctx context.Context, g s7.Grant, reask bool) ([]byte, error) {
		if err := auth.Consume(g); err != nil {
			t.Fatalf("generation ran without a consumable AttemptGrant (P0.2): %v", err)
		}
		*calls++
		if reask {
			return []byte(second), nil
		}
		return []byte(first), nil
	}
}

// Detector 9f (b2) + P0.7 literal: invalid, invalid, nothing salvageable ->
// exactly TWO transport calls, NO value, S7 FAILED. A validation failure
// (well-formed JSON, bad content) also never passes silently.
func TestStructuredOutputNeverSilentAccept(t *testing.T) {
	e, auth := extractor(t)
	calls := 0
	_, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-1", "provider-a",
		gen(t, auth, &calls, `no json here at all`, `still not json`))
	if err == nil || !errors.Is(err, ErrStructuredInvalid) {
		t.Fatalf("invalid structured output silently accepted: %v", err)
	}
	if calls != 2 {
		t.Fatalf("initial + exactly one re-ask = 2 physical calls, got %d", calls)
	}
	if st, _ := auth.State("op-1"); st != contracts.AttemptFailed {
		t.Fatalf("S7 state %s, want FAILED", st)
	}
	calls = 0
	_, err = ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-2", "provider-a",
		gen(t, auth, &calls, `{"steps":0}`, `{"steps":0}`))
	if err == nil || calls != 2 {
		t.Fatalf("failed validation silently accepted (%v, calls %d)", err, calls)
	}
}

// Detector 9f (a): valid output passes with ONE call (no re-ask); invalid
// then valid -> exactly TWO calls, value from the re-ask, S7 SUCCEEDED with
// 2 accounted attempts.
func TestStructuredHappyAndReask(t *testing.T) {
	e, auth := extractor(t)
	calls := 0
	got, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-1", "provider-a",
		gen(t, auth, &calls, `{"steps":2}`, `{"steps":3}`))
	if err != nil || got.Steps != 2 || calls != 1 {
		t.Fatalf("valid output must pass without re-ask: %+v %v calls=%d", got, err, calls)
	}
	calls = 0
	got, err = ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-2", "provider-a",
		gen(t, auth, &calls, `{"steps":1,"unknown_field":true}`, `{"steps":3}`))
	if err != nil || got.Steps != 3 || calls != 2 {
		t.Fatalf("re-ask recovery broken: %+v %v calls=%d", got, err, calls)
	}
	if st, _ := auth.State("op-2"); st != contracts.AttemptSucceeded || auth.Attempts("op-2") != 2 {
		t.Fatalf("S7 %s attempts=%d, want SUCCEEDED/2", st, auth.Attempts("op-2"))
	}
}

// Detector 9f (b): salvage runs INSIDE the final attempt's outcome: a
// prose-wrapped valid object (in the re-ask, or in the original) is accepted
// AND S7 lands SUCCEEDED; salvaged-but-invalid content still fails.
func TestSalvageFromProseStillValidates(t *testing.T) {
	e, auth := extractor(t)
	calls := 0
	got, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-1", "provider-a",
		gen(t, auth, &calls, "Sure! Here is the plan:\n```json\n{\"steps\":4}\n```\nEnjoy.", `nope`))
	if err != nil || got.Steps != 4 || calls != 2 {
		t.Fatalf("salvage from the original broken: %+v %v calls=%d", got, err, calls)
	}
	if st, _ := auth.State("op-1"); st != contracts.AttemptSucceeded {
		t.Fatalf("salvaged value returned while S7 is %s (split brain)", st)
	}
	calls = 0
	_, err = ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-2", "provider-a",
		gen(t, auth, &calls, "plan: {\"steps\":0}", "again {\"steps\":0}"))
	if err == nil {
		t.Fatal("salvaged content skipped validation")
	}
	if st, _ := auth.State("op-2"); st != contracts.AttemptFailed {
		t.Fatalf("S7 %s after unsalvageable output, want FAILED", st)
	}
}

// Detector 9f (d): SECURITY/EFFECT payloads are strict — ONE call, no
// re-ask, no salvage, S7 FAILED.
func TestSecurityEffectClassStrictNoRepair(t *testing.T) {
	e, auth := extractor(t)
	for _, class := range []PayloadClass{ClassSecurity, ClassEffect} {
		calls := 0
		op := contracts.OperationID(fmt.Sprintf("op-%d", class))
		_, err := ExtractVia[plan](context.Background(), e, class, validPlan, op, "provider-a",
			gen(t, auth, &calls, "ok: {\"steps\":2}", `{"steps":1}`))
		if err == nil {
			t.Fatalf("class %d: malformed security/effect payload repaired", class)
		}
		if calls != 1 {
			t.Fatalf("class %d: strict class made %d calls (tolerant accept)", class, calls)
		}
		if st, _ := auth.State(op); st != contracts.AttemptFailed {
			t.Fatalf("class %d: S7 %s, want FAILED", class, st)
		}
	}
}

// Detector 9f (c) — ownership ablation: with S7's second-grant path disabled
// (MaxAttempts forced to 1) and the extractor/generator code untouched,
// exactly ONE physical call happens — the extractor holds no retry loop.
func TestStructuredReaskOwnedByS7Ablation(t *testing.T) {
	e, auth := extractor(t)
	saved := s7.PolicyStructured
	s7.PolicyStructured.MaxAttempts = 1
	t.Cleanup(func() { s7.PolicyStructured = saved })
	calls := 0
	_, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-abl", "provider-a",
		gen(t, auth, &calls, `not json`, `{"steps":3}`))
	if err == nil || calls != 1 {
		t.Fatalf("ablation: expected exactly 1 call and an error, got %d %v", calls, err)
	}
}

// Nil validators / generators are refused: "no validation" IS silent accept.
func TestNilValidatorRefused(t *testing.T) {
	e, auth := extractor(t)
	_, err := ExtractVia[plan](context.Background(), e, ClassGeneral, nil, "op-1", "provider-a",
		gen(t, auth, new(int), `{"steps":1}`, ``))
	if err == nil {
		t.Fatal("nil validator accepted")
	}
	if _, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-2", "provider-a", nil); err == nil {
		t.Fatal("nil generator accepted")
	}
}

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
// EVERY governed attempt (initial + re-ask) is one grant, one physical call
// (Phase-2 codex #2 oracle, extended to the whole operation): a rogue
// callback's second call on the same grant fails ATTEMPT_NOT_AUTHORIZED,
// so the transport counter equals the S7 attempt counter (2).
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
	rogue := func(ctx context.Context, g s7.Grant, reask bool) ([]byte, error) {
		out, _ := fakeTransport(g)
		if _, err := fakeTransport(g); err == nil {
			t.Fatal("second physical call on one grant authorized")
		}
		return out, nil
	}
	_, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-1", "provider-a", rogue)
	if err == nil {
		t.Fatal("garbage accepted")
	}
	if transport != 2 || auth.Attempts("op-1") != 2 {
		t.Fatalf("transport=%d attempts=%d, must both be 2 (one physical call per grant)", transport, auth.Attempts("op-1"))
	}
}

// The LITERAL closed-discriminator fixture (Phase-2 codex #14): an
// unknown EFFECT/POLICY discriminator reaches NO sink — one generation,
// zero re-asks, zero sink invocations, immediate reject.
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
	calls, reasks := 0, 0
	for name, raw := range map[string]string{
		"unknown-kind":  `{"kind":"wipe_disk"}`,
		"prose-wrapped": `sure: {"kind":"send_message"}`,
		"not-json":      `just do it`,
	} {
		raw := raw
		_, err := ExtractVia[effectDirective](context.Background(), e, ClassEffect, validateClosed,
			contracts.OperationID("op-"+name), "provider-a", func(ctx context.Context, g s7.Grant, reask bool) ([]byte, error) {
				auth.Consume(g)
				calls++
				if reask {
					reasks++
					return []byte(`{"kind":"send_message"}`), nil
				}
				return []byte(raw), nil
			})
		if err == nil {
			t.Fatalf("%s: malformed effect payload accepted", name)
		}
	}
	if sink != 0 || reasks != 0 || calls != 3 {
		t.Fatalf("malformed effect payload reached a sink or was re-asked (sink=%d reasks=%d calls=%d)", sink, reasks, calls)
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

// The state machine leaks nothing (Phase-2-r2 codex #3): consumed-then-error
// lands FAILED (never a permanent RUNNING), and an UNCONSUMED callback's
// bytes are ungoverned — ignored, operation CANCELLED, value never accepted.
func TestReaskStateNeverLeaks(t *testing.T) {
	// (a) consumed, then network error.
	e, auth := extractor(t)
	consumedThenError := func(ctx context.Context, g s7.Grant, reask bool) ([]byte, error) {
		if err := auth.Consume(g); err != nil {
			t.Fatal(err)
		}
		return nil, fmt.Errorf("connection reset")
	}
	_, err := ExtractVia[plan](context.Background(), e, ClassGeneral, validPlan, "op-a", "provider-a", consumedThenError)
	if err == nil {
		t.Fatal("garbage accepted")
	}
	if st, _ := auth.State("op-a"); st != contracts.AttemptFailed {
		t.Fatalf("consumed-then-error attempt leaked in state %v (want FAILED)", st)
	}
	// (b) callback returns VALID bytes without consuming the grant.
	e2, auth2 := extractor(t)
	ungoverned := func(ctx context.Context, g s7.Grant, reask bool) ([]byte, error) {
		return []byte(`{"steps":7}`), nil // never touched the transport
	}
	_, err = ExtractVia[plan](context.Background(), e2, ClassGeneral, validPlan, "op-b", "provider-a", ungoverned)
	if err == nil {
		t.Fatal("ungoverned callback bytes accepted as a transport result")
	}
	if st, _ := auth2.State("op-b"); st != contracts.AttemptCancelled {
		t.Fatalf("ungoverned operation in state %v (want CANCELLED)", st)
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

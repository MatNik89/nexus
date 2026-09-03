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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
)

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
	if _, err := NewAPIKey(cfg); err != nil {
		t.Fatalf("valid construction refused: %v", err)
	}
	t.Setenv("NEXUS_TEST_KEY", "")
	if _, err := NewAPIKey(cfg); err == nil {
		t.Fatal("empty API key accepted")
	}
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	noModel := cfg
	noModel.ProviderModel = ""
	if _, err := NewAPIKey(noModel); err == nil {
		t.Fatal("empty model accepted")
	}
	offList := cfg
	offList.EgressAllow = []string{"api.other.example"}
	if _, err := NewAPIKey(offList); err == nil {
		t.Fatal("provider host outside the egress allowlist accepted (kernel floor)")
	}
	badScheme := cfg
	badScheme.ProviderBaseURL = "ftp://x.example"
	if _, err := NewAPIKey(badScheme); err == nil {
		t.Fatal("non-http scheme accepted")
	}
}

// Chat: OpenAI-compatible request wire, response content surfaced.
func TestChatRoundTrip(t *testing.T) {
	srv, calls := fakeOpenAI(t, "pong")
	t.Setenv("NEXUS_TEST_KEY", "sk-test")
	p, err := NewAPIKey(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "ping"}})
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
	p, err := NewAPIKey(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}}); err == nil {
		t.Fatal("HTTP 502 surfaced as content")
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

func extractor(t *testing.T) (*Extractor, *s7min.Authority) {
	t.Helper()
	auth := s7min.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	return NewExtractor(auth), auth
}

// The P0.7 literal: invalid output NEVER comes back as a value without an
// error — even when re-ask and salvage both fail.
func TestStructuredOutputNeverSilentAccept(t *testing.T) {
	e, _ := extractor(t)
	reasks := 0
	badReask := func(ctx context.Context, g s7min.Grant) ([]byte, error) {
		reasks++
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
// (strict decode), then recovered via re-ask.
func TestStructuredHappyAndReask(t *testing.T) {
	e, auth := extractor(t)
	reasks := 0
	goodReask := func(ctx context.Context, g s7min.Grant) ([]byte, error) {
		reasks++
		if g.Nonce == "" {
			t.Fatal("re-ask ran without an AttemptGrant (P0.2)")
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
	badReask := func(ctx context.Context, g s7min.Grant) ([]byte, error) {
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
	reask := func(ctx context.Context, g s7min.Grant) ([]byte, error) {
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
		func(ctx context.Context, g s7min.Grant) ([]byte, error) { return nil, nil })
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
	p, err := NewAPIKey(cfg)
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
	p, err := NewAPIKey(testConfig(t, full.URL))
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	if err := p.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}},
		func(d string) error { got.WriteString(d); return nil }); err != nil {
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
	p2, err := NewAPIKey(testConfig(t, truncated.URL))
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.Stream(context.Background(), []ChatMessage{{Role: "user", Content: "x"}},
		func(string) error { return nil }); err == nil {
		t.Fatal("truncated stream passed as complete")
	}
}

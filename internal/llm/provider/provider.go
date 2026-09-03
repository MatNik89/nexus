//go:build linux

// Package provider owns the S2.1 APIKey provider (OpenAI-compatible HTTP,
// egress bound to the config allowlist — kernel floor) and the S2.3-min
// structured-output path: validate → re-ask (a NEW physical attempt) →
// salvage — NEVER silent accept (SPEC P0.7). Security/effect payload
// classes are STRICT: no re-ask, no salvage, immediate reject.
//
// EVERY physical transport call (Chat/Stream, and Probe through them)
// consumes an S7 AttemptGrant IN THE TRANSPORT itself (SPEC P0.2 /
// Phase-2 codex #1-#2: a grant consumed outside the transport was
// decorative — an adapter could call HTTP twice on one grant). The loop
// never learns the auth mode (E12).
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/kernel/closure"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
)

// ChatMessage is the provider-local wire shape (the T16 loop adapts
// kernel contracts to it).
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatOutput is one completed assistant reply.
type ChatOutput struct{ Content string }

// Capabilities is the static capability surface (S2.1).
type Capabilities struct {
	Model     string
	Streaming bool
}

// DataDescriptor declares what leaves the process and to where — the
// egress governance input (E12: honest data flow, no hidden sinks).
type DataDescriptor struct {
	Hosts             []string
	SendsConversation bool
}

// APIKey is the default P0 provider: bearer-key OpenAI-compatible HTTP.
// The key value lives ONLY in the private field, read once from the
// configured env var; it never appears in errors or descriptors.
//
// topknot ceiling (Phase-2 kilo #3): the egress boundary here is a
// host-string allowlist enforced at construction, with EVERY redirect
// categorically refused (one grant = one physical request) — resolved-IP
// pinning / DNS-rebinding defense (E11) is owned by the S6.3 dialer when
// it lands; until then this provider talks only to allowlisted hosts over
// TLS (or explicit loopback for tests). Upgrade trigger: S6.3.
type APIKey struct {
	baseURL string
	host    string
	model   string
	key     string
	allowed map[string]bool
	auth    *s7min.Authority
	client  *http.Client
}

// loopbackHost reports whether the host part names the local machine —
// the only place plaintext HTTP is tolerated (test/local providers). The
// address CLASS is decided by parsing an IP literal, never by hostname
// prefix (Phase-2-r2 codex #11: "127.attacker.example" is a remote DNS
// name, not loopback).
func loopbackHost(host string) bool {
	h := host
	if hp, _, err := net.SplitHostPort(host); err == nil {
		h = hp
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// NewAPIKey builds the provider FAIL-CLOSED: https only (plaintext only
// to loopback — a bearer key over cleartext is exposure), host must be on
// the egress allowlist (config only narrows the kernel floor), key env
// var must be set and non-empty, model is required, and an S7 authority
// is mandatory — there is no ungoverned transport.
func NewAPIKey(cfg config.Config, auth *s7min.Authority) (*APIKey, error) {
	if auth == nil {
		return nil, fmt.Errorf("provider: an S7 authority is required — no ungoverned transport (fail closed)")
	}
	u, err := url.Parse(cfg.ProviderBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("provider: base URL must be http(s) with a host (fail closed)")
	}
	if u.Scheme == "http" && !loopbackHost(u.Host) {
		return nil, fmt.Errorf("provider: plaintext http to a non-loopback host would expose the bearer key (fail closed)")
	}
	allowed := map[string]bool{}
	for _, h := range cfg.EgressAllow {
		allowed[h] = true
	}
	if !allowed[u.Host] {
		return nil, fmt.Errorf("provider: host %q is not on the egress allowlist (kernel floor, fail closed)", u.Host)
	}
	if cfg.ProviderKeyEnv == "" {
		return nil, fmt.Errorf("provider: no key env var configured (fail closed)")
	}
	key := os.Getenv(cfg.ProviderKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("provider: env var %s holds no API key (fail closed)", cfg.ProviderKeyEnv)
	}
	if cfg.ProviderModel == "" {
		return nil, fmt.Errorf("provider: a model is required (fail closed)")
	}
	p := &APIKey{
		baseURL: cfg.ProviderBaseURL, host: u.Host, model: cfg.ProviderModel, key: key,
		allowed: allowed, auth: auth,
	}
	p.client = &http.Client{
		Timeout: 120 * time.Second,
		// P0 refuses EVERY redirect (Phase-2-r3 codex #2): one grant
		// authorizes exactly one physical request — an in-client redirect
		// would be a second, differently-targeted request under the same
		// consumed grant, even to an allowlisted host.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("provider: redirects refused — one grant, one physical request (fail closed)")
		},
	}
	return p, nil
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

// Target is the S7 target every grant for THIS provider must be minted
// for — a grant minted for anything else never reaches the wire
// (Phase-2-r2 codex #2: an unbound provider grant was replayable).
func (p *APIKey) Target() contracts.TargetID {
	return contracts.TargetID("provider:" + p.host)
}

// transport performs ONE governed HTTP attempt: the grant is consumed
// HERE, immediately before the wire — a second call on the same grant
// fails ATTEMPT_NOT_AUTHORIZED before any bytes leave the process.
func (p *APIKey) transport(ctx context.Context, g s7min.Grant, body []byte) (*http.Response, error) {
	if g.TargetID != p.Target() {
		return nil, fmt.Errorf("provider: grant is not bound to this provider target: %w", s7min.ErrAttemptNotAuthorized)
	}
	if err := p.auth.Consume(g); err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider: transport: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		// Status only — upstream bodies can carry anything.
		return nil, fmt.Errorf("provider: upstream HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// Chat performs one OpenAI-compatible completion call under grant g.
func (p *APIKey) Chat(ctx context.Context, msgs []ChatMessage, g s7min.Grant) (ChatOutput, error) {
	if len(msgs) == 0 {
		return ChatOutput{}, fmt.Errorf("provider: empty message list (fail closed)")
	}
	body, err := json.Marshal(chatRequest{Model: p.model, Messages: msgs})
	if err != nil {
		return ChatOutput{}, fmt.Errorf("provider: %w", err)
	}
	resp, err := p.transport(ctx, g, body)
	if err != nil {
		return ChatOutput{}, err
	}
	defer resp.Body.Close()
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ChatOutput{}, fmt.Errorf("provider: malformed upstream reply: %w", err)
	}
	if len(out.Choices) == 0 {
		return ChatOutput{}, fmt.Errorf("provider: upstream reply carries no choices")
	}
	return ChatOutput{Content: out.Choices[0].Message.Content}, nil
}

// Capabilities reports the static surface.
func (p *APIKey) Capabilities() Capabilities {
	return Capabilities{Model: p.model, Streaming: true}
}

// DataDescriptor: conversations flow to exactly one host.
func (p *APIKey) DataDescriptor() DataDescriptor {
	return DataDescriptor{Hosts: []string{p.host}, SendsConversation: true}
}

// Probe is the LIVE provider probe for the T11 sealed snapshot: one tiny
// governed round trip, bound to the resolved config's own hash. The probe
// attempt is S7-accounted like every other physical call.
func (p *APIKey) Probe(ctx context.Context, resolved config.Resolved) closure.ProbeResult {
	pr := closure.ProbeResult{Name: "provider", ConfigHash: resolved.ConfigHash()}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	op := contracts.OperationID(fmt.Sprintf("provider-probe-%d", time.Now().UnixNano()))
	g, err := p.auth.Issue(op, p.Target())
	if err != nil {
		pr.Detail = err.Error()
		return pr
	}
	if _, err := p.Chat(ctx, []ChatMessage{{Role: "user", Content: "ping"}}, g); err != nil {
		p.auth.Report(op, s7min.OutcomeFailedTerminal)
		pr.Detail = err.Error()
		return pr
	}
	p.auth.Report(op, s7min.OutcomeSucceeded)
	pr.Passed = true
	return pr
}

// Stream performs one governed streaming completion (SSE), delivering
// content deltas in order. A non-nil return after partial deltas means
// the stream BROKE — the caller must treat received content as
// incomplete, never as a full reply.
func (p *APIKey) Stream(ctx context.Context, msgs []ChatMessage, g s7min.Grant, deliver func(delta string) error) error {
	if deliver == nil {
		return fmt.Errorf("provider: a delivery sink is required (fail closed)")
	}
	if len(msgs) == 0 {
		return fmt.Errorf("provider: empty message list (fail closed)")
	}
	body, err := json.Marshal(struct {
		chatRequest
		Stream bool `json:"stream"`
	}{chatRequest{Model: p.model, Messages: msgs}, true})
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	resp, err := p.transport(ctx, g, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sawDone := false
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fmt.Errorf("provider: malformed stream chunk: %w", err)
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				if err := deliver(c.Delta.Content); err != nil {
					return fmt.Errorf("provider: delivery: %w", err)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("provider: stream broke: %w", err)
	}
	if !sawDone {
		return fmt.Errorf("provider: stream ended without [DONE] — content is INCOMPLETE")
	}
	return nil
}

//go:build linux

// Package provider owns the S2.1 APIKey provider (OpenAI-compatible HTTP,
// egress bound to the config allowlist — kernel floor) and the S2.3-min
// structured-output path: validate → re-ask (carrying an S7 AttemptGrant,
// SPEC P0.2) → salvage — NEVER silent accept (SPEC P0.7). Security/effect
// payload classes are STRICT: no re-ask, no salvage, immediate reject.
// The loop never learns the auth mode (E12); OAuth and CLIAgent providers
// are later slices behind the same interface.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/kernel/closure"
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
type APIKey struct {
	baseURL string
	host    string
	model   string
	key     string
	client  *http.Client
}

// NewAPIKey builds the provider FAIL-CLOSED: http(s) scheme only, host
// must be on the egress allowlist (config only narrows the kernel floor),
// key env var must be set and non-empty, model is required.
func NewAPIKey(cfg config.Config) (*APIKey, error) {
	u, err := url.Parse(cfg.ProviderBaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("provider: base URL must be http(s) with a host (fail closed)")
	}
	allowed := false
	for _, h := range cfg.EgressAllow {
		if h == u.Host {
			allowed = true
			break
		}
	}
	if !allowed {
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
	return &APIKey{
		baseURL: cfg.ProviderBaseURL, host: u.Host, model: cfg.ProviderModel, key: key,
		client: &http.Client{Timeout: 120 * time.Second},
	}, nil
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

// Chat performs one OpenAI-compatible completion call.
func (p *APIKey) Chat(ctx context.Context, msgs []ChatMessage) (ChatOutput, error) {
	if len(msgs) == 0 {
		return ChatOutput{}, fmt.Errorf("provider: empty message list (fail closed)")
	}
	body, err := json.Marshal(chatRequest{Model: p.model, Messages: msgs})
	if err != nil {
		return ChatOutput{}, fmt.Errorf("provider: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatOutput{}, fmt.Errorf("provider: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return ChatOutput{}, fmt.Errorf("provider: transport: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Status only — upstream bodies can carry anything.
		return ChatOutput{}, fmt.Errorf("provider: upstream HTTP %d", resp.StatusCode)
	}
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
// completion round trip, bound to the resolved config's own hash.
func (p *APIKey) Probe(ctx context.Context, resolved config.Resolved) closure.ProbeResult {
	pr := closure.ProbeResult{Name: "provider", ConfigHash: resolved.ConfigHash()}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := p.Chat(ctx, []ChatMessage{{Role: "user", Content: "ping"}}); err != nil {
		pr.Detail = err.Error()
		return pr
	}
	pr.Passed = true
	return pr
}

// Stream performs one streaming completion (SSE), delivering content
// deltas in order. A non-nil return after partial deltas means the stream
// BROKE — the caller must treat received content as incomplete, never as
// a full reply.
func (p *APIKey) Stream(ctx context.Context, msgs []ChatMessage, deliver func(delta string) error) error {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("provider: transport: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("provider: upstream HTTP %d", resp.StatusCode)
	}
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

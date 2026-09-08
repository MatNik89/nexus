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
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/foundation/egress"
	"github.com/MatNik89/nexus/internal/kernel/closure"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
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
// Egress (E11, Slice A): the client is built ONLY through the shared
// pinned-IP owner (internal/foundation/egress) — allowlisted canonical
// endpoint, resolved-IP policy on EVERY answer, Proxy:nil, reject-all
// redirects (one grant = one physical request), durable receipt before
// every permitted dial. There is no default-transport fallback.
type APIKey struct {
	baseURL string
	// host is the canonical endpoint unit "host:port" (egress.Endpoint) —
	// the S7 target and the data descriptor use the same string.
	host   string
	model  string
	key    string
	auth   *s7.Authority
	client *http.Client
	// egressOpts is the test seam for the resolver/dialer (empty in
	// production); set by newAPIKeyWithEgress before the client is built.
	egressOpts egress.Options
}

// NewAPIKey builds the provider FAIL-CLOSED: https only (plaintext only
// to loopback — a bearer key over cleartext is exposure), host must be on
// the egress allowlist (config only narrows the kernel floor), key env
// var must be set and non-empty, model is required, and an S7 authority
// is mandatory — there is no ungoverned transport.
func NewAPIKey(cfg config.Config, auth *s7.Authority, sink egress.ReceiptSink) (*APIKey, error) {
	return (&APIKey{}).build(cfg, auth, sink)
}

func (p *APIKey) build(cfg config.Config, auth *s7.Authority, sink egress.ReceiptSink) (*APIKey, error) {
	if auth == nil {
		return nil, fmt.Errorf("provider: an S7 authority is required — no ungoverned transport (fail closed)")
	}
	if sink == nil {
		return nil, fmt.Errorf("provider: an egress receipt sink is required — no unreceipted transport (fail closed)")
	}
	ep, err := egress.ParseEndpoint(cfg.ProviderBaseURL)
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	if ep.Scheme == "http" && !ep.Loopback() {
		return nil, fmt.Errorf("provider: plaintext http to a non-loopback host would expose the bearer key (fail closed)")
	}
	if !egress.Admitted(ep, cfg.EgressAllow) {
		return nil, fmt.Errorf("provider: endpoint %q is not on the egress allowlist (kernel floor, fail closed)", ep.Canonical())
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
	p.baseURL, p.host, p.model, p.key, p.auth = strings.TrimRight(cfg.ProviderBaseURL, "/"), ep.Canonical(), cfg.ProviderModel, key, auth
	// The shared E11 owner: pinned dial, Proxy:nil, reject-all redirects
	// (one grant authorizes exactly one physical request — an in-client
	// redirect would be a second request under the same consumed grant),
	// receipt before every permitted dial. opts is empty in production;
	// newAPIKeyWithEgress injects resolver/dialer seams for tests.
	client, err := egress.NewPinnedClient("provider", cfg.ProviderBaseURL, cfg.EgressAllow, 120*time.Second, p.egressOpts, sink)
	if err != nil {
		return nil, fmt.Errorf("provider: %w", err)
	}
	p.client = client
	return p, nil
}

// maxBodyBytes bounds a buffered (non-streaming) upstream reply BEFORE any
// allocation-then-decode (AUDIT-FULL F8). maxStreamBytes bounds the total
// bytes a streaming reply may deliver at the TRANSPORT layer; it sits above
// the planner's own accumulation ceiling so that ceiling is observable
// independently. topknot ceiling: constants, not config — upgrade trigger: a
// real provider reply that exceeds them.
const (
	maxBodyBytes   = 8 << 20
	maxStreamBytes = 32 << 20
)

// readBounded reads at most max+1 bytes and REFUSES when more than max were
// present. Reading max+1 (not max) is what makes an oversized body
// detectable: a plain io.LimitReader(max) would hand a truncated-but-valid
// prefix to the decoder (plan-review r1 codex #4).
func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("provider: upstream body exceeds %d bytes (refusing, fail closed)", max)
	}
	return b, nil
}

// decodeSingle strictly decodes exactly ONE JSON value and requires EOF after
// it (trailing bytes — even whitespace-padded garbage — are refused).
func decodeSingle(b []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
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
func (p *APIKey) transport(ctx context.Context, g s7.Grant, body []byte) (*http.Response, error) {
	if g.TargetID != p.Target() {
		return nil, fmt.Errorf("provider: grant is not bound to this provider target: %w", s7.ErrAttemptNotAuthorized)
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
func (p *APIKey) Chat(ctx context.Context, msgs []ChatMessage, g s7.Grant) (ChatOutput, error) {
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
	raw, err := readBounded(resp.Body, maxBodyBytes)
	if err != nil {
		return ChatOutput{}, err
	}
	var out chatResponse
	if err := decodeSingle(raw, &out); err != nil {
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
		p.auth.Report(op, s7.OutcomeFailedTerminal, "", nil)
		pr.Detail = err.Error()
		return pr
	}
	p.auth.Report(op, s7.OutcomeSucceeded, "", nil)
	pr.Passed = true
	return pr
}

// Stream performs one governed streaming completion (SSE), delivering
// content deltas in order. A non-nil return after partial deltas means
// the stream BROKE — the caller must treat received content as
// incomplete, never as a full reply.
func (p *APIKey) Stream(ctx context.Context, msgs []ChatMessage, g s7.Grant, deliver func(delta string) error) error {
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
	// Transport-layer total ceiling (F8): a never-ending stream is cut here;
	// the planner's accumulator has its own, lower, ceiling.
	lim := &io.LimitedReader{R: resp.Body, N: maxStreamBytes}
	sc := bufio.NewScanner(lim)
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
		if lim.N <= 0 {
			return fmt.Errorf("provider: stream exceeded the %d-byte ceiling without [DONE] — cut, content is INCOMPLETE (fail closed)", maxStreamBytes)
		}
		return fmt.Errorf("provider: stream ended without [DONE] — content is INCOMPLETE")
	}
	return nil
}

// newAPIKeyWithEgress is the TEST seam for the shared egress owner: it builds
// the provider exactly like NewAPIKey but with an injected resolver/dialer so a
// detector can present a poisoned resolution (metadata/RFC1918/rebind) and
// prove ZERO sockets are dialed. Production never sets opts.
func newAPIKeyWithEgress(cfg config.Config, auth *s7.Authority, sink egress.ReceiptSink, opts egress.Options) (*APIKey, error) {
	p := &APIKey{egressOpts: opts}
	built, err := p.build(cfg, auth, sink)
	if err != nil {
		return nil, err
	}
	return built, nil
}

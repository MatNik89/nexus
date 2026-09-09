package planner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/llm/provider"
)

// scriptedChat consumes the grant (as provider.Chat does) and returns the
// scripted results in order.
type scriptedChat struct {
	auth    *s7.Authority
	results []error
	calls   int
	deltas  []string
}

func (s *scriptedChat) Chat(ctx context.Context, msgs []provider.ChatMessage, g s7.Grant) (provider.ChatOutput, error) {
	if err := s.auth.Consume(g); err != nil {
		return provider.ChatOutput{}, err
	}
	i := s.calls
	s.calls++
	if i < len(s.results) && s.results[i] != nil {
		return provider.ChatOutput{}, s.results[i]
	}
	return provider.ChatOutput{Content: "ok"}, nil
}

func (s *scriptedChat) Stream(ctx context.Context, msgs []provider.ChatMessage, g s7.Grant, deliver func(string) error) error {
	if err := s.auth.Consume(g); err != nil {
		return err
	}
	s.calls++
	for _, d := range s.deltas {
		if err := deliver(d); err != nil {
			return err
		}
	}
	return &provider.Failure{Code: s7.CodeHTTP5xx, Retryable: true, Cause: errors.New("cut mid-stream")}
}

// Detector 8 (planner boundary): a 429 proposal is retried by S7 (PolicyProvider)
// with a NEW grant and the turn succeeds after exactly two calls; a 400 is
// terminal after ONE call; a stream that fails after the first delivered
// delta is TERMINAL (one call) even though the provider proposed a retry.
func TestProviderRetryAtPlannerBoundary(t *testing.T) {
	auth := s7.NewAuthority(nil, time.Minute) // real clock: Execute's wait must elapse
	auth.SetJitterSource(func() float64 { return 0 })
	blocks := []contracts.ContextBlock{userBlockT(t, "hello")}
	sc := &scriptedChat{auth: auth, results: []error{&provider.Failure{Code: s7.CodeHTTP429, Retryable: true, Cause: errors.New("429")}}}
	p, _ := New(sc, auth, "provider:test", 64000)
	p.newOp = func() (contracts.OperationID, error) { return "op-429", nil }
	act, err := p.Plan(context.Background(), blocks)
	if err != nil || act.Final == nil || *act.Final != "ok" || sc.calls != 2 {
		t.Fatalf("429 retry: err=%v calls=%d", err, sc.calls)
	}
	if st, _ := auth.State("op-429"); st != contracts.AttemptSucceeded || auth.Attempts("op-429") != 2 {
		t.Fatalf("S7 %s attempts=%d", st, auth.Attempts("op-429"))
	}
	sc2 := &scriptedChat{auth: auth, results: []error{&provider.Failure{Code: s7.CodeHTTP4xx, Cause: errors.New("400")}}}
	p2, _ := New(sc2, auth, "provider:test", 64000)
	p2.newOp = func() (contracts.OperationID, error) { return "op-400", nil }
	if _, err := p2.Plan(context.Background(), blocks); err == nil || sc2.calls != 1 {
		t.Fatalf("400 must be terminal after one call: err=%v calls=%d", err, sc2.calls)
	}
	if st, _ := auth.State("op-400"); st != contracts.AttemptFailed {
		t.Fatalf("S7 %s, want FAILED", st)
	}
	sc3 := &scriptedChat{auth: auth, deltas: []string{"partial"}}
	p3, _ := NewStreaming(sc3, sc3, auth, "provider:test", func(string) error { return nil }, 64000)
	p3.newOp = func() (contracts.OperationID, error) { return "op-stream", nil }
	if _, err := p3.Plan(context.Background(), blocks); err == nil || sc3.calls != 1 {
		t.Fatalf("mid-stream failure must be terminal: err=%v calls=%d", err, sc3.calls)
	}
	if st, _ := auth.State("op-stream"); st != contracts.AttemptFailed {
		t.Fatalf("S7 %s after partial stream, want FAILED (never re-streamed)", st)
	}
}

// Detector 8b (ownership ablation): with S7's second-grant path disabled for
// provider operations (PolicyProvider.MaxAttempts=1) and the planner/provider
// code untouched, a 429 produces exactly ONE call and an error — the planner
// holds no retry loop of its own.
func TestPlannerHoldsNoRetryLoopAblation(t *testing.T) {
	saved := s7.PolicyProvider
	s7.PolicyProvider.MaxAttempts = 1
	t.Cleanup(func() { s7.PolicyProvider = saved })
	auth := s7.NewAuthority(nil, time.Minute)
	auth.SetJitterSource(func() float64 { return 0 })
	sc := &scriptedChat{auth: auth, results: []error{&provider.Failure{Code: s7.CodeHTTP429, Retryable: true, Cause: errors.New("429")}}}
	p, _ := New(sc, auth, "provider:test", 64000)
	p.newOp = func() (contracts.OperationID, error) { return "op-abl", nil }
	if _, err := p.Plan(context.Background(), []contracts.ContextBlock{userBlockT(t, "hello")}); err == nil || sc.calls != 1 {
		t.Fatalf("ablation: expected exactly 1 call and an error, got %d %v", sc.calls, err)
	}
}

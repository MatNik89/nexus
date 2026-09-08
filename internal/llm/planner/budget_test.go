package planner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/budget"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/llm/provider"
)

// Slice C detector 1 (AUDIT-FULL F5): the input blocks FIT the budget, but
// the system prompt + tool protocol the planner adds push the FINAL request
// over it -> the turn is refused with ZERO grants consumed and ZERO provider
// calls. RED against a block-level (or absent) budget check.
func TestPlanRefusesOverBudgetWireBeforeAnyGrant(t *testing.T) {
	auth := s7.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	fc := &fakeChat{auth: auth, reply: "unreachable"}
	blocks := []contracts.ContextBlock{userBlockT(t, "hello")}
	blockTokens := budget.Measure(blocks).Tokens
	// Fits the blocks with room to spare, but not the system prompt.
	limit := blockTokens + 20
	p, err := New(fc, auth, "provider:test", limit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.WithTools(map[contracts.ToolID]effectpath.ToolSpec{
		"echo": {Description: strings.Repeat("a long tool description ", 20)},
	}, "work"); err != nil {
		t.Fatal(err)
	}
	_, err = p.Plan(context.Background(), blocks)
	var over *budget.ErrOverBudget
	if !errors.As(err, &over) {
		t.Fatalf("over-budget wire not refused: %v", err)
	}
	if fc.calls != 0 {
		t.Fatalf("provider called %d times despite the refusal", fc.calls)
	}
	// Control: a generous limit lets the same turn through.
	fc2 := &fakeChat{auth: auth, reply: "ok"}
	p2, _ := New(fc2, auth, "provider:test", 64000)
	if _, err := p2.Plan(context.Background(), blocks); err != nil || fc2.calls != 1 {
		t.Fatalf("within-budget turn refused: %v (calls %d)", err, fc2.calls)
	}
	// A planner cannot be built without a positive limit.
	if _, err := New(fc, auth, "provider:test", 0); err == nil {
		t.Fatal("zero context limit accepted by the planner")
	}
}

func userBlockT(t *testing.T, text string) contracts.ContextBlock {
	t.Helper()
	b, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: "u1", Kind: "text", Content: strp(text),
		ContentHash: "h", SourceURI: "test://u1", Producer: "t",
		Trust: contracts.TrustUser, Sensitivity: contracts.Sensitivity(1), Lineage: []string{},
		ObservedAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// endlessStream never sends [DONE]: it delivers deltas until the sink refuses.
type endlessStream struct {
	auth  *s7.Authority
	sent  int
	chunk string
}

func (e *endlessStream) Chat(ctx context.Context, msgs []provider.ChatMessage, g s7.Grant) (provider.ChatOutput, error) {
	return provider.ChatOutput{}, errors.New("not used")
}

func (e *endlessStream) Stream(ctx context.Context, msgs []provider.ChatMessage, g s7.Grant, deliver func(string) error) error {
	if err := e.auth.Consume(g); err != nil {
		return err
	}
	for {
		if err := deliver(e.chunk); err != nil {
			return err
		}
		e.sent += len(e.chunk)
	}
}

// Slice C detector 4 (F8): a never-ending stream is cut at the planner's
// accumulator ceiling — the builder never exceeds it and the turn fails with
// the ceiling named, never a partial "final".
func TestStreamCutAtAccumulatorCeiling(t *testing.T) {
	auth := s7.NewAuthority(func() time.Time { return time.Unix(1000, 0) }, time.Minute)
	es := &endlessStream{auth: auth, chunk: strings.Repeat("x", 64*1024)}
	var delivered int
	p, err := NewStreaming(es, es, auth, "provider:test", func(d string) error { delivered += len(d); return nil }, 64000)
	if err != nil {
		t.Fatal(err)
	}
	act, err := p.Plan(context.Background(), []contracts.ContextBlock{userBlockT(t, "go")})
	if err == nil || !strings.Contains(err.Error(), "ceiling") || act.Final != nil {
		t.Fatalf("endless stream not cut at the ceiling: err=%v final=%v", err, act.Final != nil)
	}
	if delivered > maxStreamTotal {
		t.Fatalf("delivered %d bytes past the %d ceiling", delivered, maxStreamTotal)
	}
}

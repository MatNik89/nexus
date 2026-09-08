// T05 RED: hard-limit breach → REFUSE, never truncate-silently (S8.1-min).
package budget

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func block(t *testing.T, id, content string) contracts.ContextBlock {
	t.Helper()
	b, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID(id), Kind: "text", Content: &content,
		ContentHash: "h", SourceURI: "local:test", Producer: "test",
		Trust:       contracts.TrustUser,
		Sensitivity: contracts.SensitivityInternal, Lineage: []string{},
		ObservedAt: time.Unix(1000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEnforceRefusesOverLimit(t *testing.T) {
	big := block(t, "b1", strings.Repeat("x", 4000)) // ~1000 tokens
	m, err := (Budget{HardLimit: 100}).Enforce([]contracts.ContextBlock{big})
	var over *ErrOverBudget
	if !errors.As(err, &over) {
		t.Fatalf("over-limit blocks must be REFUSED, got err=%v", err)
	}
	if !m.Estimated {
		t.Fatal("estimate must be labeled as an estimate")
	}
}

func TestEnforceAcceptsWithinLimit(t *testing.T) {
	small := block(t, "b1", "hello")
	if _, err := (Budget{HardLimit: 100}).Enforce([]contracts.ContextBlock{small}); err != nil {
		t.Fatalf("within-limit refused: %v", err)
	}
}

// Slice C (F5): the budget measures the FINAL wire messages and refuses over
// the limit; a non-positive limit REFUSES (never "unlimited").
func TestEnforceWireRefusesOverLimitAndNoLimit(t *testing.T) {
	msgs := []WireMessage{{Role: "system", Content: strings.Repeat("s", 400)}, {Role: "user", Content: "hi"}}
	m, err := Budget{HardLimit: 50}.EnforceWire(msgs)
	var over *ErrOverBudget
	if !errors.As(err, &over) || m.Tokens <= 50 {
		t.Fatalf("over-budget wire not refused: %v (tokens %d)", err, m.Tokens)
	}
	if _, err := (Budget{HardLimit: 10_000}).EnforceWire(msgs); err != nil {
		t.Fatalf("within-limit wire refused: %v", err)
	}
	if _, err := (Budget{}).EnforceWire(msgs); !errors.Is(err, ErrNoBudget) {
		t.Fatalf("zero limit did not refuse: %v", err)
	}
	if _, err := (Budget{HardLimit: -5}).EnforceWire(msgs); !errors.Is(err, ErrNoBudget) {
		t.Fatalf("negative limit did not refuse: %v", err)
	}
}

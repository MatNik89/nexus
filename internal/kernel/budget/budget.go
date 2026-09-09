// Package budget is ContextBudget-min (S8.1-min, tasks-P0 T05): token
// measurement with a HARD limit that REFUSES on breach — never a silent
// truncation. The measurement is an ESTIMATE (len/4) and says so; a real
// tokenizer replaces the estimator behind the same interface later (S8.1
// full). topknot: estimator ceiling — upgrade when a provider exposes real
// token counts (T15).
package budget

import (
	"errors"
	"fmt"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Measurement carries the estimate and its provenance (estimates are
// visibly estimates — HARNESS-PLAN 8.1).
type Measurement struct {
	Tokens    int
	Estimated bool
}

// Measure estimates the token weight of blocks (len/4 heuristic).
func Measure(blocks []contracts.ContextBlock) Measurement {
	total := 0
	for _, b := range blocks {
		if b.Content != nil {
			total += len(*b.Content) / 4
		} else if b.ContentRef != nil {
			total += len(*b.ContentRef) / 4
		}
		total += len(b.Kind)/4 + 2 // structural overhead floor
	}
	return Measurement{Tokens: total, Estimated: true}
}

// ErrOverBudget is returned on a hard-limit breach.
type ErrOverBudget struct {
	Tokens, Limit int
}

func (e *ErrOverBudget) Error() string {
	return fmt.Sprintf("context budget exceeded: %d tokens over hard limit %d (refusing, not truncating)", e.Tokens, e.Limit)
}

// Budget enforces a hard token limit.
type Budget struct {
	HardLimit int
}

// Enforce refuses when blocks exceed the hard limit. It NEVER truncates.
func (b Budget) Enforce(blocks []contracts.ContextBlock) (Measurement, error) {
	m := Measure(blocks)
	if b.HardLimit > 0 && m.Tokens > b.HardLimit {
		return m, &ErrOverBudget{Tokens: m.Tokens, Limit: b.HardLimit}
	}
	return m, nil
}

// WireMessage is the provider-neutral shape of ONE final role/content
// message as it will leave the process. The budget measures THIS (Slice C,
// AUDIT-FULL F5): the planner adds system prompt, tool protocol and time text
// AFTER the context blocks, so a block-level measurement can pass while the
// actual request exceeds the limit.
type WireMessage struct {
	Role    string
	Content string
}

// MeasureWire estimates the token weight of the final wire messages (len/4
// heuristic + a per-message structural overhead floor).
func MeasureWire(msgs []WireMessage) Measurement {
	total := 0
	for _, m := range msgs {
		total += len(m.Content)/4 + len(m.Role)/4 + 4
	}
	return Measurement{Tokens: total, Estimated: true}
}

// ErrNoBudget is returned when enforcement is attempted without a positive
// hard limit: an unset or non-positive limit NEVER means "unlimited" at the
// wire — the config owner rejects it at Resolve, and this is the fail-closed
// backstop for a planner constructed without one.
var ErrNoBudget = errors.New("context budget: no positive hard limit configured (refusing, fail closed)")

// EnforceWire refuses when the FINAL wire messages exceed the hard limit. It
// NEVER truncates, and it refuses (not disables) when no positive limit is
// configured.
func (b Budget) EnforceWire(msgs []WireMessage) (Measurement, error) {
	m := MeasureWire(msgs)
	if b.HardLimit <= 0 {
		return m, ErrNoBudget
	}
	if m.Tokens > b.HardLimit {
		return m, &ErrOverBudget{Tokens: m.Tokens, Limit: b.HardLimit}
	}
	return m, nil
}

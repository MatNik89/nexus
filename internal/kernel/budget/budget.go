// Package budget is ContextBudget-min (S8.1-min, tasks-P0 T05): token
// measurement with a HARD limit that REFUSES on breach — never a silent
// truncation. The measurement is an ESTIMATE (len/4) and says so; a real
// tokenizer replaces the estimator behind the same interface later (S8.1
// full). topknot: estimator ceiling — upgrade when a provider exposes real
// token counts (T15).
package budget

import (
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

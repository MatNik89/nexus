//go:build linux

package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
)

// Tools exposes the memory store as in-process tools. POLICY (the
// composition root wires it): memory_remember = ASK — the PEP's
// exact-intent approval IS the "preview + approve" gate (the approval
// hash covers the exact content; any change re-asks); memory_recall =
// ALLOW (read-only). topknot ceiling: the interactive approval UX rides
// the NEEDS_APPROVAL surface until the durable-HITL owner (T24) lands;
// yolo sessions journal ALLOWED_BY_YOLO as everywhere else.
func Tools(store *Store) map[contracts.ToolID]effectpath.InProcFunc {
	return map[contracts.ToolID]effectpath.InProcFunc{
		"memory_remember": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			var args struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.Content == "" {
				return contracts.ToolResult{}, fmt.Errorf("memory_remember: a non-empty content argument is required (fail closed)")
			}
			// The PEP approval already covered this EXACT content: store
			// as an accepted explicit fact.
			id := "fact-" + string(c.ToolCallID)
			if err := store.SaveFact(id, args.Content); err != nil {
				return contracts.ToolResult{}, err
			}
			return textResult(c, fmt.Sprintf("remembered (%s): %s", id, args.Content))
		},
		"memory_recall": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			var args struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.Query == "" {
				return contracts.ToolResult{}, fmt.Errorf("memory_recall: a non-empty query argument is required (fail closed)")
			}
			hits, err := store.Recall(args.Query)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			body := "no facts found"
			if len(hits) > 0 {
				body = ""
				for _, h := range hits {
					body += "- " + h.Content + "\n"
				}
			}
			return textResult(c, body)
		},
	}
}

// Rules returns the memory tools' PEP decisions.
func Rules() map[contracts.ToolID]effectpath.Decision {
	return map[contracts.ToolID]effectpath.Decision{
		"memory_remember": effectpath.DecisionAsk,
		"memory_recall":   effectpath.DecisionAllow,
	}
}

func textResult(c contracts.ToolCall, text string) (contracts.ToolResult, error) {
	sum := sha256.Sum256([]byte(text))
	block, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID("obs-" + string(c.ToolCallID)), Kind: "tool_output",
		Content: &text, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://memory", Producer: "memory",
		Trust: contracts.TrustToolTrusted, Sensitivity: contracts.SensitivityInternal,
		Lineage: []string{string(c.ToolCallID)}, ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	now := time.Now().UTC()
	return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
		Status: contracts.ResultSucceeded, Output: []contracts.ContextBlock{block},
		StartedAt: now, FinishedAt: now.Add(time.Millisecond)}, nil
}

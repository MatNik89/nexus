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
	"github.com/MatNik89/nexus/internal/security/redact"
)

// Specs are the SEALED tool declarations for the memory tools — the
// planner builds ToolCalls FROM these, never from provider-supplied
// effect/kind fields (Phase-3 codex #3: a forged READ_ONLY class would
// dodge the receipt rule).
func Specs() map[contracts.ToolID]effectpath.ToolSpec {
	return map[contracts.ToolID]effectpath.ToolSpec{
		"memory_remember": {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "memory_remember.v1",
			Description:    `store a fact; args {"content":"...","supersedes":"<fact id, optional>","tags":["..."]}`},
		"memory_recall": {Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "memory_recall.v1",
			Description:    `retrieve facts; args {"query":"..."} or {"tag":"..."}`},
	}
}

// Rules returns the memory tools' PEP decisions: remember = ASK (the
// exact-intent approval hash covers the exact content — approval IS the
// preview gate; UX ceiling until the T24 durable-HITL owner), recall =
// ALLOW (read-only).
func Rules() map[contracts.ToolID]effectpath.Decision {
	return map[contracts.ToolID]effectpath.Decision{
		"memory_remember": effectpath.DecisionAsk,
		"memory_recall":   effectpath.DecisionAllow,
	}
}

// Tools exposes the store as in-process tools. Every handler REFUSES a
// call whose ProfileID differs from the store's bound profile (Phase-3
// codex #4: an approval for one profile must never effect another);
// recall output passes the known-ref redactor before the model boundary.
func Tools(store *Store, r redact.Redactor) map[contracts.ToolID]effectpath.InProcFunc {
	if r == nil {
		r = redact.None{}
	}
	profileGuard := func(c contracts.ToolCall) error {
		if c.ProfileID != store.Profile() {
			return fmt.Errorf("memory: call profile %q does not match the store profile (fail closed)", c.ProfileID)
		}
		return nil
	}
	return map[contracts.ToolID]effectpath.InProcFunc{
		"memory_remember": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				Content    string   `json:"content"`
				Supersedes string   `json:"supersedes,omitempty"`
				Tags       []string `json:"tags,omitempty"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.Content == "" {
				return contracts.ToolResult{}, fmt.Errorf("memory_remember: a non-empty content argument is required (fail closed)")
			}
			id := "fact-" + string(c.ToolCallID)
			var err error
			if args.Supersedes != "" {
				// "actually it's Y": the user-visible correction path
				// (Phase-3 kilo #2) — strictly linear, validated in the
				// append transaction.
				err = store.SupersedeLineage(ctx, args.Supersedes, id, args.Content, args.Tags, []string{string(c.ToolCallID)})
			} else {
				err = store.SaveFactLineage(ctx, id, args.Content, args.Tags, []string{string(c.ToolCallID)})
			}
			if err != nil {
				return contracts.ToolResult{}, err
			}
			// The write COMMITTED (journal append returned): attest it
			// with a receipt bound to this call+attempt (Phase-3 codex #3
			// / agy #1 — an effectful success without a receipt parks
			// UNKNOWN).
			sum := sha256.Sum256([]byte(args.Content))
			receipt := &contracts.CommitReceipt{
				Phase: contracts.PhaseAfterCommit, ToolCallID: c.ToolCallID,
				AttemptNo: c.AttemptNo, ContentHash: hex.EncodeToString(sum[:]),
			}
			return textResult(c, fmt.Sprintf("remembered (%s): %s", id, args.Content), receipt)
		},
		"memory_recall": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				Query string `json:"query,omitempty"`
				Tag   string `json:"tag,omitempty"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || (args.Query == "" && args.Tag == "") {
				return contracts.ToolResult{}, fmt.Errorf("memory_recall: a non-empty query or tag argument is required (fail closed)")
			}
			var hits []Row
			var err error
			if args.Tag != "" {
				hits, err = store.RecallTag(ctx, args.Tag)
			} else {
				hits, err = store.Recall(ctx, args.Query)
			}
			if err != nil {
				return contracts.ToolResult{}, err
			}
			body := "no facts found"
			// Monotone trust (Phase-3 codex #7): the observation is only
			// as trusted as its LEAST trusted fact — an accepted inference
			// derived from untrusted material stays inside the fence.
			// Lineage UNIONS the stored source chains with this call.
			trust := contracts.TrustToolTrusted
			lineage := []string{string(c.ToolCallID)}
			seen := map[string]bool{string(c.ToolCallID): true}
			if len(hits) > 0 {
				body = ""
				for _, h := range hits {
					if h.Trust == contracts.TrustUntrustedExternal {
						trust = contracts.TrustUntrustedExternal
					}
					for _, l := range h.Lineage {
						if !seen[l] {
							seen[l] = true
							lineage = append(lineage, l)
						}
					}
					// Known secret references never reach the model
					// boundary verbatim.
					body += "- " + redactText(r, h.Content) + "\n"
				}
			}
			return textResultLineage(c, body, nil, trust, lineage)
		},
	}
}

// redactText applies the known-ref redactor to one plain string.
func redactText(r redact.Redactor, s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "(unrenderable)"
	}
	red, err := r.Redact(b)
	if err != nil {
		return "(redaction failed — content withheld)"
	}
	var out string
	if json.Unmarshal(red, &out) != nil {
		return "(unrenderable)"
	}
	return out
}

func textResult(c contracts.ToolCall, text string, receipt *contracts.CommitReceipt) (contracts.ToolResult, error) {
	return textResultTrust(c, text, receipt, contracts.TrustToolTrusted)
}

func textResultTrust(c contracts.ToolCall, text string, receipt *contracts.CommitReceipt, trust contracts.TrustClass) (contracts.ToolResult, error) {
	return textResultLineage(c, text, receipt, trust, []string{string(c.ToolCallID)})
}

func textResultLineage(c contracts.ToolCall, text string, receipt *contracts.CommitReceipt, trust contracts.TrustClass, lineage []string) (contracts.ToolResult, error) {
	sum := sha256.Sum256([]byte(text))
	block, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID("obs-" + string(c.ToolCallID)), Kind: "tool_output",
		Content: &text, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://memory", Producer: "memory",
		Trust: trust, Sensitivity: contracts.SensitivityConfidential,
		Lineage: lineage, ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	now := time.Now().UTC()
	return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
		Status: contracts.ResultSucceeded, Output: []contracts.ContextBlock{block},
		StartedAt: now, FinishedAt: now.Add(time.Millisecond), Commit: receipt}, nil
}

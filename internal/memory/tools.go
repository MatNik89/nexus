//go:build linux

package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

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
			ArgsSchemaHash: "memory_remember.v4",
			Description: `store a fact; args {"content":"...","supersedes":"<fact id, optional>",` +
				`"claim_key":"<short token identifying what this fact is ABOUT, optional>",` +
				`"replaces_id":"<the [id] shown next to that same thing in a recent memory_recall — ` +
				`required together with claim_key WHENEVER one already exists>","tags":["..."]}. ` +
				`claim_key lets NEXUS auto-detect when this fact contradicts an earlier one about the SAME ` +
				`thing (e.g. "server_ip") instead of you needing to know its id for supersedes. You MUST call ` +
				`memory_recall first and pass the exact [id] it showed as replaces_id — this proves you ` +
				`actually looked at what you're replacing rather than guessing; a wrong or missing replaces_id ` +
				`is refused. A clean, proven update auto-replaces the old one; an uncertain contradiction is ` +
				`held for review instead of silently overwriting. You may also pass claim_key alongside ` +
				`supersedes to label an EXISTING fact for future claim_key-based lookups.`},
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
			if err := validRawUTF8(c.Arguments); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				Content    string   `json:"content"`
				Supersedes string   `json:"supersedes,omitempty"`
				ClaimKey   string   `json:"claim_key,omitempty"`
				ReplacesID string   `json:"replaces_id,omitempty"`
				Tags       []string `json:"tags,omitempty"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.Content == "" {
				return contracts.ToolResult{}, fmt.Errorf("memory_remember: a non-empty content argument is required (fail closed)")
			}
			id := "fact-" + string(c.ToolCallID)
			// resultText is set per-branch so the reported outcome is
			// always HONEST about what actually happened (code-review
			// finding, codex, round 1 MEDIUM, live-reproduced: the
			// identical-restatement no-op previously still reported
			// "remembered (fact-X)" for an id that was never written).
			var resultText string
			switch {
			case args.Supersedes != "":
				// "actually it's Y": the user-visible EXPLICIT correction
				// path (Phase-3 kilo #2) — strictly linear, validated in
				// the append transaction. Named explicit intent wins over
				// claim_key's own heuristic when both are given; claim_key
				// (if also given) still BOOTSTRAPS the corrected fact into
				// future claim_key-based lookup (code-review finding,
				// codex, round 1 HIGH).
				if err := store.SupersedeLineageWithClaim(ctx, args.Supersedes, id, args.Content, args.ClaimKey, args.Tags, []string{string(c.ToolCallID)}); err != nil {
					return contracts.ToolResult{}, err
				}
				resultText = fmt.Sprintf("remembered (%s): %s", id, args.Content)
			case args.ClaimKey != "":
				// Audn (S9.4 P1): auto-detect a contradiction against
				// whatever fact currently owns this claim_key, without the
				// model needing to already know its id in advance.
				// replaces_id PROVES the model actually looked at the
				// current value (a recent memory_recall) rather than
				// merely guessing the claim_key label — required whenever
				// one already exists; Apply itself refuses a wrong/missing
				// one (fail closed). ID, not content, is the correctness
				// bar (an earlier content-based version of this check was
				// defeated by an ABA content-cycle counterexample — see
				// applyRemembered's doc comment).
				outcome, err := store.Remember(ctx, id, args.Content, OriginExplicit, args.ClaimKey, stripIDBrackets(args.ReplacesID), args.Tags, []string{string(c.ToolCallID)})
				if err != nil {
					return contracts.ToolResult{}, err
				}
				switch outcome.Kind {
				case "noop":
					resultText = fmt.Sprintf("no change — %q already says exactly this (existing fact %s)", args.ClaimKey, outcome.EffectiveID)
				case "proposed":
					resultText = fmt.Sprintf("held for review (%s): this contradicts an existing EXPLICIT fact and was not auto-applied — %s", outcome.EffectiveID, args.Content)
				case "superseded":
					resultText = fmt.Sprintf("remembered (%s), replacing the previous value for %q: %s", outcome.EffectiveID, args.ClaimKey, args.Content)
				default: // "added"
					resultText = fmt.Sprintf("remembered (%s): %s", outcome.EffectiveID, args.Content)
				}
			default:
				if err := store.SaveFactLineage(ctx, id, args.Content, args.Tags, []string{string(c.ToolCallID)}); err != nil {
					return contracts.ToolResult{}, err
				}
				resultText = fmt.Sprintf("remembered (%s): %s", id, args.Content)
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
			return textResult(c, resultText, receipt)
		},
		"memory_recall": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			if err := validRawUTF8(c.Arguments); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				Query string `json:"query,omitempty"`
				Tag   string `json:"tag,omitempty"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || (args.Query == "" && args.Tag == "") {
				return contracts.ToolResult{}, fmt.Errorf("memory_recall: a non-empty query or tag argument is required (fail closed)")
			}
			// A JSON `\uD800`-style unpaired-surrogate escape is valid
			// ASCII text (validRawUTF8 above sees nothing wrong) but
			// still decodes to U+FFFD — the SAME class round 9/10 closed
			// on every WRITE path (code-review finding, codex, round 10
			// MEDIUM: read paths hadn't been checked symmetrically, so a
			// query could match a fact whose tag was corrupted to the
			// SAME substituted value by an unrelated bug or a historical
			// pre-fix record, retrieving the wrong fact).
			if err := validUTF8(args.Query, args.Tag); err != nil {
				return contracts.ToolResult{}, err
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
					// boundary verbatim. The [id] is echoed back as
					// replaces_id on a future memory_remember correction —
					// it PROVES the caller looked at THIS exact row, not
					// merely one that happens to currently share its
					// content (ABA fix, round 2 — see applyRemembered).
					line := fmt.Sprintf("- [%s] %s", h.ID, redactText(r, h.Content))
					if h.ContentionCount > 0 {
						// Audn's own visibility signal (S9.4 P1): a
						// contested fact is still surfaced (never a
						// silent drop) but flagged so the model can tell
						// the user it's been disputed rather than
						// stating it with unwarranted confidence.
						line += fmt.Sprintf(" (CONTESTED %d time(s) by a later, unresolved claim)", h.ContentionCount)
					}
					body += line + "\n"
				}
			}
			return textResultLineage(c, body, nil, trust, lineage)
		},
	}
}

// stripIDBrackets tolerates a caller copying memory_recall's rendered
// "- [id] content" line verbatim, brackets included, instead of just the
// id inside them (code-review finding, agy, round 3 NOTE) — pure format
// tolerance, never a weaker check: the unwrapped value must still match
// the current owner's id exactly, byte for byte, in applyRemembered.
// validRawUTF8 refuses invalid-UTF-8 arguments BEFORE json.Unmarshal ever
// runs on them (code-review finding, codex, round 7 MEDIUM, live-
// reproduced: Go's JSON decoder silently substitutes U+FFFD for invalid
// UTF-8 bytes DURING Unmarshal, so a check placed AFTER decoding — like
// Store's own validUTF8 on the decoded Go strings — can never observe
// the model's ORIGINAL bytes; it only ever sees the already-sanitized
// ones. codex's live counterexample: a raw 0xff byte inside
// replaces_id silently normalized to a DIFFERENT existing fact's actual
// id, defeating the exact-target check entirely). Checking the raw
// argument bytes here, before this handler's own json.Unmarshal, closes
// it at the boundary where the untrusted bytes actually enter.
func validRawUTF8(raw json.RawMessage) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("memory: tool arguments must be valid UTF-8 (fail closed)")
	}
	return nil
}

func stripIDBrackets(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
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

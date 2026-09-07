// drift.go — tgout plan v17: closed key contract + schema-drift classifier.
package planner

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/loop"
)

// topLevelWalk tokenizes ONE JSON object's top level and returns its
// (key, string-value-or-"") pairs in source order, plus whether the walk
// consumed exactly one whole object. Values that are not strings come
// back as "". It never decodes into a struct, so last-member-wins can
// hide nothing (tgout plan: closed key contract + drift routing).
func topLevelWalk(raw string) (pairs [][2]string, whole bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, false
	}
	depth := 1
	expectKey := true
	var key string
	for depth > 0 {
		t, err = dec.Token()
		if err != nil {
			return nil, false
		}
		switch v := t.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				depth++
				expectKey = false
			case '}', ']':
				depth--
				if depth == 1 {
					expectKey = true
				}
			}
		case string:
			if depth == 1 && expectKey {
				key = v
				expectKey = false
			} else if depth == 1 {
				pairs = append(pairs, [2]string{key, v})
				expectKey = true
			}
		default:
			if depth == 1 && !expectKey {
				pairs = append(pairs, [2]string{key, ""})
				expectKey = true
			}
		}
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false // trailing content (even malformed): not one whole object
	}
	return pairs, true
}

// validClosedContract reports whether the pairs satisfy the CLOSED KEY
// CONTRACT: exactly lowercase {action, tool_id, arguments(optional)},
// each once — case-INSENSITIVE duplicate detection, unknown or
// non-lowercase keys reject.
func validClosedContract(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	t, err := dec.Token()
	if err != nil {
		return false
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return false
	}
	seen := map[string]bool{}
	depth := 1
	expectKey := true
	for depth > 0 {
		t, err = dec.Token()
		if err != nil {
			return false
		}
		switch v := t.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				depth++
				expectKey = false
			case '}', ']':
				depth--
				if depth == 1 {
					expectKey = true
				}
			}
		case string:
			if depth == 1 && expectKey {
				lower := strings.ToLower(v)
				if v != lower {
					return false // non-lowercase key (case alias)
				}
				if lower != "action" && lower != "tool_id" && lower != "arguments" {
					return false // unknown key
				}
				if seen[lower] {
					return false // duplicate (any capitalization)
				}
				seen[lower] = true
				expectKey = false
			} else if depth == 1 {
				expectKey = true
			}
		default:
			if depth == 1 && !expectKey {
				expectKey = true
			}
		}
	}
	return seen["action"] && seen["tool_id"]
}

// classifyView strips at most ONE wrapping markdown code fence for
// CLASSIFICATION ONLY. A reply still fenced after one strip is prose.
func classifyView(reply string) (view string, stillFenced bool) {
	t := strings.TrimSpace(reply)
	if !strings.HasPrefix(t, "```") {
		return t, false
	}
	// STRICT wrapping fence only (impl review codex #4): the LAST
	// non-empty line must be exactly ``` and nothing may follow it —
	// an unclosed fence or fence-then-prose is prose, not a view.
	lines := strings.Split(t, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return t, true
	}
	body := strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
	return body, strings.HasPrefix(body, "```")
}

// driftTool classifies SCHEMA DRIFT: the view must be one whole JSON
// object, and the values of the keys action/tool_id/name (key match
// case-insensitive, ALL positions including duplicates, FIRST matching
// occurrence in source order wins) name a registered tool. Returns the
// selected tool by the tier precedence action > tool_id > name; within
// a tier, first source-order occurrence.
func (c *ChatPlanner) driftTool(view string) (contracts.ToolID, bool) {
	pairs, whole := topLevelWalk(view)
	if !whole || len(pairs) == 0 {
		return "", false
	}
	tiers := []string{"action", "tool_id", "name"}
	for _, tier := range tiers {
		for _, kv := range pairs {
			if strings.ToLower(kv[0]) != tier {
				continue
			}
			id := contracts.ToolID(kv[1])
			if _, known := c.specs[id]; known && kv[1] != "" {
				return id, true
			}
		}
	}
	return "", false
}

// driftError builds the typed carrier (loop package — no import cycle).
func driftError(tool contracts.ToolID) loop.DriftError {
	return loop.DriftError{
		Typed: contracts.TypedError{
			Code: "TOOL_SCHEMA_DRIFT", Category: contracts.ErrCatValidation,
			Retryability: contracts.RetryNever,
			SafeMessage:  "the model reply named tool " + string(tool) + " but broke the tool-call schema",
			Origin:       "planner",
		},
		Tool: tool,
	}
}

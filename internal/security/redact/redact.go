// Package redact implements the P0 known-ref secret redactor (HARDQ C1):
// exact-match replacement of KNOWN secret values (provider key, bot token,
// values of named env vars) BEFORE anything reaches the journal (P0.3).
// JSON input is redacted on DECODED string values, so escape-spelling
// variants (\", \\, e…) cannot smuggle a known secret past the match
// (Phase-1A r2 codex #5). Entropy/shape detection stays out of scope (P4).
package redact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Redactor replaces known secret values in a byte stream AND can preflight
// a string without mutating it (r3 codex #2: structural-field rejection is
// part of the required contract, not an optional extra interface).
type Redactor interface {
	// Redact returns the redacted bytes or an ERROR when it cannot redact
	// soundly (r4 codex #6: an over-budget valid-JSON input must be
	// rejected by the caller, never silently byte-matched — byte matching
	// cannot see decoded escape spellings).
	Redact([]byte) ([]byte, error)
	Touches(string) bool
}

// KnownRefs redacts an explicit name→value set.
type KnownRefs struct {
	// longest-first so an overlapping shorter secret cannot split a longer one
	entries []entry
}

type entry struct {
	name  string
	value string
}

// NewKnownRefs builds a redactor from name→secret-value pairs; empty values
// are ignored (they would match everything).
func NewKnownRefs(refs map[string]string) *KnownRefs {
	r := &KnownRefs{}
	for name, val := range refs {
		if val == "" {
			continue
		}
		r.entries = append(r.entries, entry{name: name, value: val})
	}
	sort.Slice(r.entries, func(i, j int) bool {
		if len(r.entries[i].value) != len(r.entries[j].value) {
			return len(r.entries[i].value) > len(r.entries[j].value)
		}
		return r.entries[i].name < r.entries[j].name
	})
	return r
}

func (r *KnownRefs) redactString(s string) string {
	for _, e := range r.entries {
		if strings.Contains(s, e.value) {
			s = strings.ReplaceAll(s, e.value, "[REDACTED:"+e.name+"]")
		}
	}
	return s
}

// Budgets (r3 codex #7: the previous recursive walk re-unmarshaled every
// subtree per depth level — superlinear). The tree walk below parses the
// input ONCE (O(n)) and bounds depth; over-budget input falls back to plain
// byte replacement — still redacted, never unbounded work.
const (
	maxJSONBytes = 1 << 20 // 1 MiB
	maxJSONDepth = 64
)

// redactValue rebuilds a decoded JSON value with every string redacted.
// The decoded tree is a PRIVATE traversal buffer — no kernel API carries
// map[string]any; the public surface stays []byte in, []byte out (E3 is a
// rule about contracts, not about a local decode inside the redactor).
func (r *KnownRefs) redactValue(v interface{}, depth int, out *bytes.Buffer) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("json depth budget exceeded")
	}
	switch t := v.(type) {
	case string:
		enc, err := json.Marshal(r.redactString(t))
		if err != nil {
			return err
		}
		out.Write(enc)
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			kb, err := json.Marshal(r.redactString(k)) // keys can carry secrets too
			if err != nil {
				return err
			}
			out.Write(kb)
			out.WriteByte(':')
			if err := r.redactValue(t[k], depth+1, out); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case []interface{}:
		out.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := r.redactValue(item, depth+1, out); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case json.Number:
		out.WriteString(t.String())
	case bool:
		if t {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case nil:
		out.WriteString("null")
	default:
		return fmt.Errorf("unexpected json node type %T", v)
	}
	return nil
}

// Redact processes b. Valid JSON within budget: parsed once, redacted on
// decoded string values. Valid JSON OVER budget (bytes or depth): typed
// ERROR — byte matching cannot see decoded escape spellings, so falling
// back would fail open (r4 codex #6). Non-JSON input: raw byte replacement
// (no decoded form exists to miss).
func (r *KnownRefs) Redact(b []byte) ([]byte, error) {
	if len(r.entries) == 0 {
		return b, nil
	}
	if json.Valid(b) {
		if len(b) > maxJSONBytes {
			return nil, fmt.Errorf("redact: json input exceeds the %d-byte budget (rejected, not byte-matched)", maxJSONBytes)
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v interface{}
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("redact: decode: %w", err)
		}
		var out bytes.Buffer
		if err := r.redactValue(v, 0, &out); err != nil {
			return nil, fmt.Errorf("redact: %w", err)
		}
		if !json.Valid(out.Bytes()) {
			return nil, fmt.Errorf("redact: transform produced invalid json")
		}
		return out.Bytes(), nil
	}
	out := b
	for _, e := range r.entries {
		if bytes.Contains(out, []byte(e.value)) {
			out = bytes.ReplaceAll(out, []byte(e.value), []byte(fmt.Sprintf("[REDACTED:%s]", e.name)))
		}
	}
	return out, nil
}

// Touches reports whether any known secret occurs in s (decoded-string
// check for callers that must REJECT rather than mutate — e.g. structural
// journal fields, r2 codex #2).
func (r *KnownRefs) Touches(s string) bool {
	for _, e := range r.entries {
		if strings.Contains(s, e.value) {
			return true
		}
	}
	return false
}

// None is a no-op redactor for contexts with no known secrets (tests).
// It explicitly reports NO matches — the structural preflight always runs;
// there is simply nothing to match.
type None struct{}

func (None) Redact(b []byte) ([]byte, error) { return b, nil }
func (None) Touches(string) bool             { return false }

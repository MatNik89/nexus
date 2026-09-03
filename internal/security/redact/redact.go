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

// Redactor replaces known secret values in a byte stream.
type Redactor interface {
	Redact([]byte) []byte
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

// redactJSONValue walks a decoded JSON tree, redacting every string value.
// Values are kept as json.RawMessage except strings (never map[string]any
// in any API — this is a private traversal).
func (r *KnownRefs) redactJSONValue(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, nil
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		out, err := json.Marshal(r.redactString(s))
		return out, err
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb bytes.Buffer
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, _ := json.Marshal(r.redactString(k)) // keys can carry secrets too
			sb.Write(kb)
			sb.WriteByte(':')
			v, err := r.redactJSONValue(obj[k])
			if err != nil {
				return nil, err
			}
			sb.Write(v)
		}
		sb.WriteByte('}')
		return sb.Bytes(), nil
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, err
		}
		var sb bytes.Buffer
		sb.WriteByte('[')
		for i, item := range arr {
			if i > 0 {
				sb.WriteByte(',')
			}
			v, err := r.redactJSONValue(item)
			if err != nil {
				return nil, err
			}
			sb.Write(v)
		}
		sb.WriteByte(']')
		return sb.Bytes(), nil
	default: // number/bool/null: cannot carry a string secret
		return trimmed, nil
	}
}

// Redact processes b: valid JSON is redacted on decoded string values
// (escape-proof); anything else falls back to raw byte replacement.
func (r *KnownRefs) Redact(b []byte) []byte {
	if len(r.entries) == 0 {
		return b
	}
	if json.Valid(b) {
		if out, err := r.redactJSONValue(b); err == nil {
			return out
		}
	}
	out := b
	for _, e := range r.entries {
		if bytes.Contains(out, []byte(e.value)) {
			out = bytes.ReplaceAll(out, []byte(e.value), []byte(fmt.Sprintf("[REDACTED:%s]", e.name)))
		}
	}
	return out
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
type None struct{}

func (None) Redact(b []byte) []byte { return b }

// Toucher is implemented by redactors that can CHECK without mutating.
type Toucher interface {
	Touches(string) bool
}

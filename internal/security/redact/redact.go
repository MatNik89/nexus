// Package redact implements the P0 known-ref secret redactor (HARDQ C1):
// exact-match replacement of KNOWN secret values (provider key, bot token,
// values of named env vars) BEFORE anything reaches the journal (P0.3).
// Entropy/shape detection is deliberately out of scope until P4.
package redact

import (
	"bytes"
	"fmt"
	"sort"
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
	value []byte
}

// NewKnownRefs builds a redactor from name→secret-value pairs; empty values
// are ignored (they would match everything).
func NewKnownRefs(refs map[string]string) *KnownRefs {
	r := &KnownRefs{}
	for name, val := range refs {
		if val == "" {
			continue
		}
		r.entries = append(r.entries, entry{name: name, value: []byte(val)})
	}
	sort.Slice(r.entries, func(i, j int) bool {
		if len(r.entries[i].value) != len(r.entries[j].value) {
			return len(r.entries[i].value) > len(r.entries[j].value)
		}
		return r.entries[i].name < r.entries[j].name
	})
	return r
}

func (r *KnownRefs) Redact(b []byte) []byte {
	out := b
	for _, e := range r.entries {
		if bytes.Contains(out, e.value) {
			out = bytes.ReplaceAll(out, e.value, []byte(fmt.Sprintf("[REDACTED:%s]", e.name)))
		}
	}
	return out
}

// None is a no-op redactor for contexts with no known secrets (tests).
type None struct{}

func (None) Redact(b []byte) []byte { return b }

//go:build linux

// Package health is the ONE channel-health owner (AUDIT-FULL F6; Slice D):
// no runtime failure of a channel adapter is silently discarded. The owner
// keeps the last state per component in memory AND in a failure-INDEPENDENT
// durable projection written with atomicwrite (a file under the profile's
// system dir — NOT the journal, because the journal-failure case is one of
// the states it must report), plus one redacted stderr line per state
// transition. It RENDERS the typed class the originating owner attached
// (channel.ClassifiedError); it never infers a class from text. Sealed
// capability rule: health is reported, never used to activate a fallback.
package health

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
)

// Class is the closed set of failure classes (typed at the origin).
type Class string

const (
	// ClassTransport: the wire or remote misbehaved; the adapter keeps
	// running under S7 scheduling (degraded).
	ClassTransport Class = "transport"
	// ClassRemoteRejected: the remote refuses this identity (401/403,
	// revoked token): the adapter STOPS; repair + restart is required.
	ClassRemoteRejected Class = "remote_rejected"
	// ClassSubstrate: the durable substrate (journal / S7 landing) failed:
	// FATAL, the adapter stops immediately (never retry a broken journal).
	ClassSubstrate Class = "substrate"
)

func (c Class) Valid() bool {
	return c == ClassTransport || c == ClassRemoteRejected || c == ClassSubstrate
}

// Fatal reports whether the class stops the adapter.
func (c Class) Fatal() bool { return c == ClassRemoteRejected || c == ClassSubstrate }

// Entry is the recorded state of one component.
type Entry struct {
	Component string    `json:"component"`
	Healthy   bool      `json:"healthy"`
	Class     Class     `json:"class,omitempty"`
	Code      string    `json:"code,omitempty"`
	Detail    string    `json:"detail,omitempty"` // REDACTED by the reporter
	Stopped   bool      `json:"stopped,omitempty"`
	At        time.Time `json:"at"`
}

// Owner records channel health.
type Owner struct {
	mu    sync.Mutex
	path  string
	log   io.Writer
	now   func() time.Time
	state map[string]Entry
}

// New builds the owner for one projection path; log receives one redacted
// line per state transition (stderr in production). The projection is
// written on EVERY transition; a projection write failure is returned to
// the caller (it is the health substrate itself failing) and never hides
// the reported state from memory.
func New(path string, log io.Writer) (*Owner, error) {
	if path == "" {
		return nil, fmt.Errorf("health: a projection path is required (fail closed)")
	}
	if log == nil {
		log = io.Discard
	}
	return &Owner{path: path, log: log, now: time.Now, state: map[string]Entry{}}, nil
}

// SetClock pins the clock (tests).
func (o *Owner) SetClock(now func() time.Time) { o.mu.Lock(); o.now = now; o.mu.Unlock() }

// Report records a classified failure for component. stopped marks that
// the component's runtime has terminated (fatal class or supervisor).
func (o *Owner) Report(component string, class Class, code, redactedDetail string, stopped bool) error {
	if !class.Valid() {
		class = ClassSubstrate // unclassified = fail closed
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	prev := o.state[component]
	e := Entry{Component: component, Healthy: false, Class: class, Code: code, Detail: redactedDetail, Stopped: stopped, At: o.now()}
	o.state[component] = e
	if prev.Healthy || prev.Class != class || prev.Code != code || prev.Stopped != stopped || prev.At.IsZero() {
		fmt.Fprintf(o.log, "nexus health: %s %s class=%s code=%s stopped=%v: %s\n", component, "DEGRADED", class, code, stopped, redactedDetail)
	}
	return o.writeLocked()
}

// Healthy records a successful cycle for component (clears a prior failure).
func (o *Owner) Healthy(component string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	prev, had := o.state[component]
	if had && prev.Healthy {
		return nil // no transition, no write
	}
	o.state[component] = Entry{Component: component, Healthy: true, At: o.now()}
	if had {
		fmt.Fprintf(o.log, "nexus health: %s RECOVERED\n", component)
	}
	return o.writeLocked()
}

// Snapshot returns the current in-memory state.
func (o *Owner) Snapshot() map[string]Entry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]Entry, len(o.state))
	for k, v := range o.state {
		out[k] = v
	}
	return out
}

func (o *Owner) writeLocked() error {
	entries := make([]Entry, 0, len(o.state))
	for _, e := range o.state {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Component < entries[j].Component })
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicwrite.Write(o.path, b, 0o600); err != nil {
		return fmt.Errorf("health: projection write failed: %w", err)
	}
	return nil
}

// Read loads the projection (doctor). A missing file is (nil, nil): no
// runtime health has been recorded yet.
func Read(path string) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("health: projection malformed: %w", err)
	}
	return out, nil
}

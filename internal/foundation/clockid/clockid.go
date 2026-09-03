// Package clockid provides the injectable wall/monotonic clock and ID
// generator seams (tasks-P0 T05, HARDQ A2 K0 step 2). Every downstream
// component takes these interfaces — no component invents its own clock or
// ID source, so any scenario can be replayed deterministically in tests.
package clockid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// Clock is the single time source.
type Clock interface {
	Now() time.Time                  // wall clock
	Since(t time.Time) time.Duration // monotonic-backed elapsed
}

// IDGen mints identifiers.
type IDGen interface {
	NewID(prefix string) string
}

// System is the production clock.
type System struct{}

func (System) Now() time.Time                  { return time.Now().UTC() } // Annex: every timestamp UTC
func (System) Since(t time.Time) time.Duration { return time.Since(t) }

// RandomIDs is the production generator: prefix-<16 hex bytes>.
type RandomIDs struct{}

func (RandomIDs) NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is unrecoverable for identity minting.
		panic(fmt.Sprintf("clockid: entropy source failed: %v", err))
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// Fake is a deterministic test clock: starts at a fixed instant and
// advances only when told to.
type Fake struct {
	now atomic.Int64 // unix nanos
}

// NewFake starts at the given instant.
func NewFake(start time.Time) *Fake {
	f := &Fake{}
	f.now.Store(start.UnixNano())
	return f
}

func (f *Fake) Now() time.Time                  { return time.Unix(0, f.now.Load()).UTC() }
func (f *Fake) Since(t time.Time) time.Duration { return f.Now().Sub(t) }
func (f *Fake) Advance(d time.Duration)         { f.now.Add(int64(d)) }

// SeqIDs is a deterministic test generator: prefix-000001, prefix-000002...
type SeqIDs struct {
	n atomic.Uint64
}

func (s *SeqIDs) NewID(prefix string) string {
	return fmt.Sprintf("%s-%06d", prefix, s.n.Add(1))
}

// Package sealedstore is the profile-scoped, content-addressed sealed
// artifact store (PLAN-CODING-TRIO.md invariant 4 + Slice 0): the durable
// home for bytes too large or too sensitive for the journal itself — a
// workspace-transaction's before/after image bundle, a coding-evidence
// manifest's captured output. Every artifact is named by its own SHA-256
// digest; a caller references one durably (a journal event field) and
// MUST hold the returned Pin open until that reference is itself durable,
// so a concurrent GC sweep can never delete an artifact in the window
// between it landing on disk and the reference that will protect it
// forever after being committed (PLAN-CODING-TRIO.md's GC/write race,
// codex round-4 HIGH #2).
package sealedstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
)

// digestRE is the closed shape a content digest (and therefore a store
// filename) may ever take — 64 lowercase hex characters, nothing else.
// Every path this package touches is built from a digest already matching
// this pattern; Get/GC refuse any other input outright (fail closed).
var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Store owns one profile's sealed-artifact directory. Construct with Open.
type Store struct {
	dir string

	mu   sync.Mutex
	pins map[string]int // digest -> live pin count
}

// Open binds a Store to dir, which the CALLER must already have created as
// a private (0700), non-symlink, caller-owned directory (pathx.EnsureDir)
// — sealedstore never resolves profile identity itself, matching the
// existing journal.Open convention (an already-resolved path in, no
// profile-to-path logic duplicated here).
func Open(dir string) (*Store, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("sealedstore: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sealedstore: %s is a symlink — refused (fail closed)", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sealedstore: %s is not a directory", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("sealedstore: %s permissions %o are too open (need 0700) — refused", dir, info.Mode().Perm())
	}
	return &Store{dir: dir, pins: map[string]int{}}, nil
}

// Pin holds one artifact live against GC. The caller obtained it from Put
// and MUST call Release exactly once, and only after the digest is
// durably referenced elsewhere (a journal event committed) — releasing
// early re-opens the GC race the pin exists to close; never releasing at
// all is a (bounded, single-process-lifetime) leak, not a correctness bug.
type Pin struct {
	store  *Store
	digest string
	once   sync.Once
}

// Release drops this pin. Safe to call more than once (idempotent no-op
// after the first call) so a defer alongside an explicit release never
// double-frees.
func (p *Pin) Release() {
	p.once.Do(func() {
		p.store.mu.Lock()
		defer p.store.mu.Unlock()
		if n := p.store.pins[p.digest]; n <= 1 {
			delete(p.store.pins, p.digest)
		} else {
			p.store.pins[p.digest] = n - 1
		}
	})
}

// Put durably writes data, content-addressed by its own SHA-256 digest,
// and returns a Pin the caller must Release once the digest is durably
// referenced elsewhere. A crash between Put returning and that reference
// landing is SAFE by construction: nothing yet points at the artifact, so
// losing it (to a post-restart GC sweep, which never re-establishes the
// dead process's in-memory pin) loses nothing a retry can't recreate by
// calling Put again.
func (s *Store) Put(data []byte) (digest string, pin *Pin, err error) {
	sum := sha256.Sum256(data)
	digest = hex.EncodeToString(sum[:])

	s.mu.Lock()
	s.pins[digest]++
	s.mu.Unlock()
	pin = &Pin{store: s, digest: digest}

	path := filepath.Join(s.dir, digest)
	if _, err := os.Lstat(path); err == nil {
		// Same digest already stored: content-addressing guarantees
		// identical bytes, so this is a no-op write, not a conflict.
		return digest, pin, nil
	} else if !os.IsNotExist(err) {
		pin.Release()
		return "", nil, fmt.Errorf("sealedstore: put %s: %w", digest, err)
	}
	if err := atomicwrite.Write(path, data, 0o600); err != nil {
		pin.Release()
		return "", nil, fmt.Errorf("sealedstore: put %s: %w", digest, err)
	}
	return digest, pin, nil
}

// Get reads and re-verifies one artifact by digest — every recovery use
// re-checks the digest before trusting the bytes (PLAN-CODING-TRIO.md
// invariant 4): a stored blob whose content no longer hashes to its own
// filename is corruption, never silently trusted.
func (s *Store) Get(digest string) ([]byte, error) {
	if !digestRE.MatchString(digest) {
		return nil, fmt.Errorf("sealedstore: %q is not a valid digest (fail closed)", digest)
	}
	path := filepath.Join(s.dir, digest)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("sealedstore: get %s: %w", digest, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("sealedstore: %s is a symlink — refused (fail closed)", digest)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("sealedstore: get %s: %w", digest, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, fmt.Errorf("sealedstore: %s content does not match its own digest (corrupt, fail closed)", digest)
	}
	return data, nil
}

// GC removes every stored artifact whose digest is neither in liveDigests
// (the caller's mark pass — every digest still referenced by a durable
// journal entry) nor currently pinned (an in-flight Put awaiting its
// journal reference). Mark-and-sweep in one pass, never per-artifact ad
// hoc deletion, so an artifact referenced by more than one record is never
// removed while any reference to it still lives.
func (s *Store) GC(liveDigests map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("sealedstore: gc: %w", err)
	}
	s.mu.Lock()
	pinned := make(map[string]bool, len(s.pins))
	for d := range s.pins {
		pinned[d] = true
	}
	s.mu.Unlock()

	var removed []string
	for _, e := range entries {
		name := e.Name()
		if !digestRE.MatchString(name) {
			continue // never touch a file this store did not itself create
		}
		if liveDigests[name] || pinned[name] {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("sealedstore: gc remove %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

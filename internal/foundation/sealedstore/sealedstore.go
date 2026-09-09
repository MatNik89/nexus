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
//
// Every read and write is DESCRIPTOR-RELATIVE (PLAN-CODING-TRIO.md's
// explicit requirement): Open resolves the store directory to a file
// descriptor exactly ONCE and every subsequent operation is an *at(2)
// syscall (openat/unlinkat/renameat) against that descriptor, never a
// fresh pathname lookup through the parent directories. This closes a
// real attack Open-by-pathname cannot: nothing after Open can swap the
// directory itself (rename it aside, replace it with a symlink) and
// redirect subsequent operations elsewhere — the descriptor keeps
// pointing at the original inode regardless of what happens to the path
// that named it (code-review CODE1 codex finding #4, reproduced live via
// a mid-flight directory rename).
package sealedstore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"

	"golang.org/x/sys/unix"
)

// digestRE is the closed shape a content digest (and therefore a store
// filename) may ever take — 64 lowercase hex characters, nothing else.
// Every name this package touches is validated against this pattern;
// Get/GC refuse any other input outright (fail closed).
var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Store owns one profile's sealed-artifact directory. Construct with
// Open; release with Close when the store is no longer needed.
type Store struct {
	dir   string // for error messages only — never used to build a path
	dirFd int    // held open for the Store's lifetime; every op is *at(dirFd, ...)

	mu        sync.Mutex
	pins      map[string]int // digest -> live pin count
	closeOnce sync.Once
}

// Open binds a Store to dir, which the CALLER must already have created as
// a private (0700), non-symlink, caller-owned directory (pathx.EnsureDir)
// — sealedstore never resolves profile identity itself, matching the
// existing journal.Open convention (an already-resolved path in, no
// profile-to-path logic duplicated here). The directory is opened exactly
// once here; every later operation is descriptor-relative to this open.
func Open(dir string) (*Store, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("sealedstore: open %s: %w (fail closed — must be an existing, non-symlink directory)", dir, err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("sealedstore: fstat %s: %w", dir, err)
	}
	if os.FileMode(st.Mode).Perm()&0o077 != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("sealedstore: %s permissions %o are too open (need 0700) — refused", dir, os.FileMode(st.Mode).Perm())
	}
	return &Store{dir: dir, dirFd: fd, pins: map[string]int{}}, nil
}

// Close releases the store's held directory descriptor. The Store must
// not be used afterward.
func (s *Store) Close() error {
	var err error
	s.closeOnce.Do(func() { err = unix.Close(s.dirFd) })
	return err
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
		p.store.releasePinLocked(p.digest)
	})
}

// releasePinLocked decrements or clears digest's pin count. Caller must
// already hold s.mu.
func (s *Store) releasePinLocked(digest string) {
	if n := s.pins[digest]; n <= 1 {
		delete(s.pins, digest)
	} else {
		s.pins[digest] = n - 1
	}
}

// Put durably writes data, content-addressed by its own SHA-256 digest,
// and returns a Pin the caller must Release once the digest is durably
// referenced elsewhere. A crash between Put returning and that reference
// landing is SAFE by construction: nothing yet points at the artifact, so
// losing it (to a post-restart GC sweep, which never re-establishes the
// dead process's in-memory pin) loses nothing a retry can't recreate by
// calling Put again.
//
// Put and GC serialize under the SAME critical section for their ENTIRE
// operation (code-review CODE1 codex finding #3) — not just a snapshot of
// the pins map — so a Put that registers a new pin can never race a
// concurrent GC sweep that already decided, under a now-stale snapshot,
// that the digest was unpinned.
func (s *Store) Put(data []byte) (digest string, pin *Pin, err error) {
	sum := sha256.Sum256(data)
	digest = hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pins[digest]++
	pin = &Pin{store: s, digest: digest}

	if existing, rerr := s.readAtLocked(digest); rerr == nil {
		// Same-name entry already exists: verify it ACTUALLY holds the
		// content its name promises (code-review CODE1 codex finding #4)
		// — a prior Lstat-only check would treat any file at that name as
		// "already stored," including a corrupt or foreign one.
		esum := sha256.Sum256(existing)
		if hex.EncodeToString(esum[:]) != digest {
			s.releasePinLocked(digest)
			return "", nil, fmt.Errorf("sealedstore: put %s: existing entry does not match its own digest (corrupt, fail closed)", digest)
		}
		return digest, pin, nil
	} else if !os.IsNotExist(rerr) {
		s.releasePinLocked(digest)
		return "", nil, fmt.Errorf("sealedstore: put %s: %w", digest, rerr)
	}

	if err := s.writeAtLocked(digest, data); err != nil {
		s.releasePinLocked(digest)
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
	data, err := s.readAt(digest)
	if err != nil {
		return nil, fmt.Errorf("sealedstore: get %s: %w", digest, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, fmt.Errorf("sealedstore: %s content does not match its own digest (corrupt, fail closed)", digest)
	}
	return data, nil
}

// gcPauseHook, when non-nil, runs after GC has listed the directory but
// BEFORE it deletes anything — still holding s.mu. Test-only seam (mirrors
// atomicwrite's pauseHook) that proves Put and GC actually serialize: a
// concurrent Put attempted while the hook blocks must itself block on
// s.mu until GC's critical section ends. Production never sets it.
var gcPauseHook func()

// GC removes every stored artifact whose digest is neither live (per
// loadLive, the caller's mark pass — every digest still referenced by a
// durable journal entry) nor currently pinned (an in-flight Put awaiting
// its journal reference). Mark-and-sweep in one pass under the SAME lock
// Put holds for its whole operation (see Put's doc comment), never
// per-file ad hoc deletion, so an artifact referenced by more than one
// record is never removed while any reference to it still lives.
//
// loadLive is called WHILE s.mu IS HELD, not before GC is invoked
// (code-review CODE2 codex finding #3, reproduced: GC(liveDigests
// map[string]bool) took an already-computed snapshot as a plain
// argument — a concurrent Put that pinned, got its journal reference
// committed, and released its pin entirely BEFORE GC even started could
// still be deleted by a GC call whose caller had captured liveDigests
// before that commit). Calling loadLive under the lock does not, by
// itself, make ANY possible liveDigests source race-free — that still
// requires loadLive to perform a FRESH read of whatever durably records
// references (the journal) on every call, never a memoized/cached
// snapshot — but it removes the window between "the caller decided what
// is live" and "GC actually started," which is the part sealedstore
// itself can control.
func (s *Store) GC(loadLive func() map[string]bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	liveDigests := loadLive()
	names, err := s.listNames()
	if err != nil {
		return nil, fmt.Errorf("sealedstore: gc: %w", err)
	}
	if gcPauseHook != nil {
		gcPauseHook()
	}
	var removed []string
	for _, name := range names {
		if !digestRE.MatchString(name) {
			continue // never touch a file this store did not itself create
		}
		if liveDigests[name] || s.pins[name] > 0 {
			continue
		}
		if err := unix.Unlinkat(s.dirFd, name, 0); err != nil && err != unix.ENOENT {
			return removed, fmt.Errorf("sealedstore: gc remove %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// readAt opens name relative to the store's held directory descriptor,
// with O_NOFOLLOW — if the final component is a symlink, the open itself
// fails (fail closed); there is no separate Lstat-then-open TOCTOU window.
func (s *Store) readAt(name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAtLocked(name)
}

// readAtLocked is readAt's body. Caller must already hold s.mu (Put calls
// it directly to stay inside its own critical section).
func (s *Store) readAtLocked(name string) ([]byte, error) {
	fd, err := unix.Openat(s.dirFd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file (fail closed)", name)
	}
	return io.ReadAll(f)
}

// writeAtLocked atomically creates name (content-addressed — this name is
// never overwritten in place once it exists): a randomly-suffixed temp
// name is created descriptor-relatively, written, fsynced, then renamed
// into place descriptor-relatively, then the directory itself is fsynced
// so the rename is durable. Caller must already hold s.mu.
func (s *Store) writeAtLocked(name string, data []byte) error {
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("tmp name: %w", err)
	}
	tmpName := "." + name + ".tmp-" + hex.EncodeToString(suffix[:])

	fd, err := unix.Openat(s.dirFd, tmpName,
		unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := os.NewFile(uintptr(fd), tmpName)
	cleanup := func() {
		tmp.Close()
		unix.Unlinkat(s.dirFd, tmpName, 0)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil { // data durable BEFORE the swap
		cleanup()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		unix.Unlinkat(s.dirFd, tmpName, 0)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := unix.Renameat(s.dirFd, tmpName, s.dirFd, name); err != nil {
		unix.Unlinkat(s.dirFd, tmpName, 0)
		return fmt.Errorf("rename: %w", err)
	}
	if err := unix.Fsync(s.dirFd); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}

// listNames returns every entry name directly in the store directory, via
// a fresh descriptor-relative open of "." against dirFd — never a
// pathname lookup of s.dir. Caller must already hold s.mu.
func (s *Store) listNames() ([]string, error) {
	fd, err := unix.Openat(s.dirFd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), s.dir)
	defer f.Close()
	return f.Readdirnames(-1)
}

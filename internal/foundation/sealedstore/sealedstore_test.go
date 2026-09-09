//go:build linux

package sealedstore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Detector: put/get round trip, content-addressed by the data's own digest.
func TestPutGetRoundTrip(t *testing.T) {
	s := open(t)
	data := []byte("hello sealed artifact")
	digest, pin, err := s.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Release()
	sum := sha256.Sum256(data)
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest %s does not match sha256 of the data", digest)
	}
	got, err := s.Get(digest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

// Detector: re-Put of identical content is a no-op, not a conflict — same
// digest, same bytes, no error.
func TestPutSameContentIsIdempotent(t *testing.T) {
	s := open(t)
	data := []byte("idempotent content")
	d1, p1, err := s.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	p1.Release()
	d2, p2, err := s.Put(data)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Release()
	if d1 != d2 {
		t.Fatalf("same content produced different digests: %s vs %s", d1, d2)
	}
}

// Detector (RED-capable — corruption must never be silently trusted):
// tampering with a stored artifact's bytes on disk must make Get refuse,
// not return the tampered content.
func TestGetRefusesCorruptedContent(t *testing.T) {
	s := open(t)
	digest, pin, err := s.Put([]byte("original content"))
	if err != nil {
		t.Fatal(err)
	}
	pin.Release()
	path := filepath.Join(s.dir, digest)
	if err := os.WriteFile(path, []byte("TAMPERED"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(digest); err == nil {
		t.Fatal("Get accepted tampered content — corruption not detected")
	}
}

// Detector: a symlink at a digest path is refused by Get, never followed.
// The symlink's name is the TRUE digest of its target's content (code-
// review CODE1 codex finding #6: an earlier version of this test used an
// arbitrary fake digest that didn't match the target content, so it
// passed via the unrelated digest-mismatch path in Get — never actually
// exercising the symlink-rejection code path at all). Naming it correctly
// means the ONLY thing that can make Get refuse here is the symlink
// check itself.
func TestGetRefusesSymlink(t *testing.T) {
	s := open(t)
	outsideData := []byte("outside data")
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, outsideData, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(outsideData)
	trueDigest := hex.EncodeToString(sum[:])
	if err := os.Symlink(outside, filepath.Join(s.dir, trueDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(trueDigest); err == nil {
		t.Fatal("Get followed a symlink — refused expected")
	}
}

// Detector: a malformed digest is refused outright (fail closed), never
// used to build a filesystem path.
func TestGetRefusesInvalidDigest(t *testing.T) {
	s := open(t)
	bad := []string{
		"",
		"not-hex",
		"../../etc/passwd",
		repeat("a", 36),
		"AA" + repeat("0", 62),
	}
	for _, d := range bad {
		if _, err := s.Get(d); err == nil {
			t.Fatalf("Get accepted invalid digest %q", d)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

// Detector: GC removes an artifact absent from liveDigests and unpinned;
// keeps one present in liveDigests; keeps one currently pinned even if
// absent from liveDigests (the in-flight-Put-awaiting-journal-reference
// case the GC/write-race fix exists for).
func TestGCMarkAndSweep(t *testing.T) {
	s := open(t)
	dOrphan, pOrphan, err := s.Put([]byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	pOrphan.Release() // no longer pinned, not referenced anywhere -> orphan

	dLive, pLive, err := s.Put([]byte("live"))
	if err != nil {
		t.Fatal(err)
	}
	pLive.Release() // unpinned, but WILL be in liveDigests below

	dPinned, pPinned, err := s.Put([]byte("pinned"))
	if err != nil {
		t.Fatal(err)
	}
	defer pPinned.Release() // still pinned during GC — must survive

	removed, err := s.GC(func() map[string]bool { return map[string]bool{dLive: true} })
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != dOrphan {
		t.Fatalf("GC removed %v, want exactly [%s]", removed, dOrphan)
	}
	if _, err := s.Get(dLive); err != nil {
		t.Fatalf("live artifact removed by GC: %v", err)
	}
	if _, err := s.Get(dPinned); err != nil {
		t.Fatalf("pinned artifact removed by GC: %v", err)
	}
	if _, err := s.Get(dOrphan); err == nil {
		t.Fatal("orphan artifact survived GC")
	}
}

// Detector: Release is idempotent — a defer alongside an explicit Release
// must never double-decrement or panic.
func TestPinReleaseIdempotent(t *testing.T) {
	s := open(t)
	_, pin, err := s.Put([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	pin.Release()
	pin.Release() // must not panic or corrupt state
}

// Detector (code-review CODE1 codex finding #3): Put and GC serialize
// under ONE critical section for their ENTIRE operation, not just a
// pins-map snapshot — a Put attempted while GC is mid-sweep (paused via
// the test-only gcPauseHook, still holding s.mu) must itself block until
// GC's critical section ends, never interleave with it.
func TestGCAndPutSerializeUnderOneCriticalSection(t *testing.T) {
	s := open(t)
	_, pOrphan, err := s.Put([]byte("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	pOrphan.Release()

	resumeGC := make(chan struct{})
	gcEntered := make(chan struct{})
	gcPauseHook = func() {
		close(gcEntered)
		<-resumeGC
	}
	defer func() { gcPauseHook = nil }()

	gcDone := make(chan error, 1)
	go func() {
		_, gerr := s.GC(func() map[string]bool { return map[string]bool{} })
		gcDone <- gerr
	}()
	<-gcEntered // GC is inside its critical section, paused mid-operation

	putDone := make(chan error, 1)
	go func() {
		_, pin, perr := s.Put([]byte("during-gc"))
		if perr == nil {
			pin.Release()
		}
		putDone <- perr
	}()

	select {
	case <-putDone:
		t.Fatal("Put completed while GC held its critical section paused — Put and GC are not serialized")
	case <-time.After(50 * time.Millisecond):
		// expected: Put is blocked waiting on s.mu
	}

	close(resumeGC)
	if err := <-gcDone; err != nil {
		t.Fatal(err)
	}
	if err := <-putDone; err != nil {
		t.Fatal(err)
	}
}

// Detector (code-review CODE2 codex finding #3): loadLive is called
// WHILE s.mu is held, not before GC starts — so a caller whose "live"
// source is a live reference to a value that changes between "GC is
// requested" and "GC actually runs" sees the value AS OF the sweep, not
// as of some earlier point. Simulates codex's exact reproduced failing
// sequence (mark computed BEFORE Put/commit/Release, GC only entering
// its critical section AFTER) but with loadLive reading a shared
// variable the caller updates at "commit time" — the fix is that GC
// reads it live, under the lock, not a plain pre-computed argument.
func TestGCLoadLiveIsCalledUnderTheLock(t *testing.T) {
	s := open(t)
	var mu sync.Mutex
	live := map[string]bool{} // stands in for "what the journal currently says is live"

	d, pin, err := s.Put([]byte("committed after mark was requested"))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate: journal reference commits, THEN the publication pin is
	// released — exactly the ordering the Pin doc comment requires.
	mu.Lock()
	live[d] = true
	mu.Unlock()
	pin.Release()

	loadLive := func() map[string]bool {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]bool, len(live))
		for k := range live {
			out[k] = true
		}
		return out
	}
	if _, err := s.GC(loadLive); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(d); err != nil {
		t.Fatalf("GC deleted an artifact that was live by the time loadLive ran under the lock: %v", err)
	}
}

// Detector: Open refuses a symlinked directory (fail closed).
func TestOpenRefusesSymlinkedDir(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("Open accepted a symlinked directory")
	}
}

// Detector: Open refuses an over-permissive directory (fail closed).
func TestOpenRefusesOverPermissiveDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open accepted a 0755 directory")
	}
}

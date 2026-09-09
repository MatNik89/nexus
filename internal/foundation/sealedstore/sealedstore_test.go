//go:build linux

package sealedstore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
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
func TestGetRefusesSymlink(t *testing.T) {
	s := open(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("outside data"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeDigest := "0000000000000000000000000000000000000000000000000000000000aa"
	if err := os.Symlink(outside, filepath.Join(s.dir, fakeDigest)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(fakeDigest); err == nil {
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

	removed, err := s.GC(map[string]bool{dLive: true})
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

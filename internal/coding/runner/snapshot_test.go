//go:build linux

package runner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Detector: a basic tree is copied faithfully and the returned digest
// reflects the exact content.
func TestCreateSnapshotCopiesTreeAndDigests(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.go"), []byte("package sub"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, cleanup, err := CreateSnapshot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	got, err := os.ReadFile(filepath.Join(snap.Dir, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package a" {
		t.Fatalf("a.go content = %q, want %q", got, "package a")
	}
	got2, err := os.ReadFile(filepath.Join(snap.Dir, "sub", "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != "package sub" {
		t.Fatalf("sub/b.go content = %q, want %q", got2, "package sub")
	}
	if snap.Digest == "" {
		t.Fatal("empty digest")
	}
}

// Detector: the digest is order-independent (same content, different
// creation order on disk still produces the same digest) but sensitive
// to content changes — a real Merkle-style digest, not just a file count.
func TestCreateSnapshotDigestIsDeterministicAndContentSensitive(t *testing.T) {
	src1 := t.TempDir()
	os.WriteFile(filepath.Join(src1, "a.go"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(src1, "b.go"), []byte("B"), 0o644)
	snap1, cleanup1, err := CreateSnapshot(src1)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup1()

	src2 := t.TempDir()
	os.WriteFile(filepath.Join(src2, "b.go"), []byte("B"), 0o644)
	os.WriteFile(filepath.Join(src2, "a.go"), []byte("A"), 0o644)
	snap2, cleanup2, err := CreateSnapshot(src2)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()

	if snap1.Digest != snap2.Digest {
		t.Fatalf("same content, different digests: %s vs %s", snap1.Digest, snap2.Digest)
	}

	src3 := t.TempDir()
	os.WriteFile(filepath.Join(src3, "a.go"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(src3, "b.go"), []byte("DIFFERENT"), 0o644)
	snap3, cleanup3, err := CreateSnapshot(src3)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup3()

	if snap1.Digest == snap3.Digest {
		t.Fatal("different content produced the same digest")
	}
}

// Detector: a symlink anywhere in the source tree is refused, never
// resolved-and-followed.
func TestCreateSnapshotRefusesSymlink(t *testing.T) {
	src := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateSnapshot(src); err == nil {
		t.Fatal("CreateSnapshot accepted a symlink in the source tree")
	}
}

// Detector: a hard-linked regular file is refused (kilo's independent
// plan-review finding: a self-exfiltration angle where a workspace owner
// hard-links a host secret into the tree and it gets faithfully copied).
func TestCreateSnapshotRefusesHardlink(t *testing.T) {
	src := t.TempDir()
	real := filepath.Join(src, "real.go")
	if err := os.WriteFile(real, []byte("package real"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(src, "linked.go")
	if err := os.Link(real, linked); err != nil {
		t.Skipf("hardlinks not supported on this filesystem: %v", err)
	}
	if _, _, err := CreateSnapshot(src); err == nil {
		t.Fatal("CreateSnapshot accepted a hard-linked file in the source tree")
	}
}

// Detector: a device/socket/fifo node is refused.
func TestCreateSnapshotRefusesFIFO(t *testing.T) {
	src := t.TempDir()
	fifoPath := filepath.Join(src, "fifo")
	if err := mkfifo(fifoPath); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	if _, _, err := CreateSnapshot(src); err == nil {
		t.Fatal("CreateSnapshot accepted a FIFO node in the source tree")
	}
}

// Detector: exceeding the depth cap is refused.
func TestCreateSnapshotRefusesTooDeep(t *testing.T) {
	src := t.TempDir()
	dir := src
	for i := 0; i < MaxSnapshotDepth+2; i++ {
		dir = filepath.Join(dir, "d")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "deep.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateSnapshot(src); err == nil {
		t.Fatal("CreateSnapshot accepted a tree deeper than MaxSnapshotDepth")
	}
}

// Detector: cleanup actually removes the disposable directory.
func TestCreateSnapshotCleanupRemovesDir(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "a.go"), []byte("A"), 0o644)
	snap, cleanup, err := CreateSnapshot(src)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(snap.Dir); !os.IsNotExist(err) {
		t.Fatalf("snapshot dir still exists after cleanup: %v", err)
	}
}

func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}

// Detector (plan-review round 1, kilo's independent finding): a plain
// os.Open FOLLOWS a symlink, and a post-open Stat().Mode().IsRegular()
// check does NOT catch this — it reports the FOLLOWED target's mode,
// which looks like an ordinary regular file. openRegularNoFollow must
// refuse outright when the final path component is a symlink, isolating
// this specific TOCTOU defense from CreateSnapshot's own WalkDir-time
// check (which only covers the window BEFORE this call, not a swap
// happening between the walk and the open).
func TestOpenRegularNoFollowRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.go")
	if err := os.WriteFile(real, []byte("package real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.go")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openRegularNoFollow(link); err == nil {
		t.Fatal("openRegularNoFollow followed a symlink to a regular file")
	}
}

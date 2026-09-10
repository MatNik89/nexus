//go:build linux

package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Detector: a real, minimal write — create a NEW file (MustNotExist) in
// a nested directory via OpenRoot + WalkDirBeneath + WriteFileBeneath —
// produces the exact expected content on disk.
func TestWriteFileBeneathCreatesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dirFd, err := WalkDirBeneath(rootFd, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFd)

	if _, err := WriteFileBeneath(dirFd, "f.go", []byte("package b\n"), 0o644, TargetExpectation{MustNotExist: true}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "a", "b", "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package b\n" {
		t.Fatalf("content = %q, want %q", got, "package b\n")
	}
}

// Detector: MustNotExist is enforced ATOMICALLY via RENAME_NOREPLACE — a
// file that already exists at baseName is refused, never silently
// overwritten as if it were new.
func TestWriteFileBeneathMustNotExistRefusesWhenFileAlreadyExists(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	_, err = WriteFileBeneath(rootFd, "f.go", []byte("new content"), 0o644, TargetExpectation{MustNotExist: true})
	if err == nil {
		t.Fatal("expected an error: f.go already exists, MustNotExist should refuse")
	}
	got, err := os.ReadFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "already here" {
		t.Fatalf("existing file was modified despite the refusal: %q", got)
	}
}

// Detector: WriteFileBeneath on a file that ALREADY EXISTS, with its
// CORRECT expected identity (captured via StatBeneath), replaces its
// content atomically (rename-over), rather than erroring or appending.
func TestWriteFileBeneathReplacesExistingFileWithMatchingIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFileBeneath(rootFd, "f.go", []byte("new content"), 0o644, TargetExpectation{Dev: dev, Ino: ino}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Fatalf("content = %q, want %q", got, "new content")
	}
}

// Detector: WalkDirBeneath with relDir=="" returns a usable fd for the
// root itself (a file living directly in the root, no subdirectory).
func TestWalkDirBeneathEmptyRelDirReturnsRoot(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dirFd, err := WalkDirBeneath(rootFd, "")
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFd)

	if _, err := WriteFileBeneath(dirFd, "top.go", []byte("x"), 0o644, TargetExpectation{MustNotExist: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "top.go")); err != nil {
		t.Fatal(err)
	}
}

// Detector (the plan's own core requirement — invariant 4 symlink
// defense): a SYMLINKED intermediate directory component must be
// refused outright, never resolved-and-followed. This is the real
// attack this whole package exists to close: a parent directory swapped
// for a symlink pointing outside the workspace.
func TestWalkDirBeneathRefusesSymlinkedDirectoryComponent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "a" is a SYMLINK pointing OUTSIDE root, not a real directory.
	if err := os.Symlink(outside, filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	_, err = WalkDirBeneath(rootFd, "a")
	if err == nil {
		t.Fatal("expected an error walking into a symlinked directory component")
	}
	// The outside file must remain completely untouched.
	got, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "s3cr3t" {
		t.Fatal("outside file was unexpectedly modified")
	}
}

// Detector (code-review finding, codex, round 1 — the CORRECTED test:
// the original version of this test asserted the OPPOSITE, wrong
// behavior — that a planted symlink would be silently overwritten. The
// plan's own identity model requires target drift to be REFUSED: "an
// inode change to the... tracked NAME resolving somewhere else, is
// refused"). A directory that is VALID when WalkDirBeneath resolves it,
// but whose target FILENAME is swapped for a symlink AFTER the caller
// captured its expected identity (simulating a Prepare-time capture,
// then a TOCTOU race before Apply), must make WriteFileBeneath REFUSE —
// via the descriptor-relative fstatat identity check immediately before
// the rename — not silently replace it.
func TestWriteFileBeneathRefusesTargetIdentityDrift(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The ORIGINAL file Prepare would have hashed and captured the
	// identity of.
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go") // simulates Prepare-time capture
	if err != nil {
		t.Fatal(err)
	}

	// TOCTOU: swap f.go for a symlink pointing outside the workspace
	// AFTER the expected identity was captured, BEFORE Apply writes.
	if err := os.Remove(filepath.Join(root, "f.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "f.go")); err != nil {
		t.Fatal(err)
	}

	_, err = WriteFileBeneath(rootFd, "f.go", []byte("attacker should not see this replace"), 0o644, TargetExpectation{Dev: dev, Ino: ino})
	if err == nil {
		t.Fatal("expected WriteFileBeneath to refuse — the target's identity drifted to a planted symlink")
	}

	// The planted symlink must be left EXACTLY as it was — untouched,
	// still a symlink, still pointing outside.
	fi, err := os.Lstat(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("f.go should still be the planted symlink — WriteFileBeneath must not have touched it")
	}
	outsideContent, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "s3cr3t" {
		t.Fatal("outside file was unexpectedly modified through the planted symlink")
	}
}

// Detector: OpenRoot itself refuses a symlinked root path (the FINAL
// component).
func TestOpenRootRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRoot(link); err == nil {
		t.Fatal("expected an error opening a symlinked root")
	}
}

// Detector (code-review finding, codex, round 1, empirically confirmed
// live on this kernel): a plain open()+O_NOFOLLOW only protects the
// FINAL path component — an INTERMEDIATE symlink anywhere earlier in
// rootPath is silently traversed. OpenRoot must refuse this too (via
// openat2's RESOLVE_NO_SYMLINKS, which applies to every component).
func TestOpenRootRefusesIntermediateSymlinkComponent(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(real, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	// "link" is a symlink to "real" — an INTERMEDIATE component, not the
	// final one, of the rootPath we pass to OpenRoot below.
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(link, "workspace") // "link" is intermediate here
	if _, err := OpenRoot(rootPath); err == nil {
		t.Fatal("expected an error: rootPath contains an intermediate symlink component")
	}
}

// Detector: a relative path with a ".." component must be refused
// outright by WalkDirBeneath's own defense-in-depth check, never even
// reaching the syscall layer.
func TestWalkDirBeneathRejectsDotDotComponent(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	if _, err := WalkDirBeneath(rootFd, "a/../b"); err == nil {
		t.Fatal("expected an error for a relative path containing '..'")
	}
}

// Detector: a base name containing a path separator (never a single
// clean component) must be refused.
func TestWriteFileBeneathRejectsBaseNameWithSeparator(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	if _, err := WriteFileBeneath(rootFd, "a/f.go", []byte("x"), 0o644, TargetExpectation{MustNotExist: true}); err == nil {
		t.Fatal("expected an error for a base name containing a separator")
	}
}

// Detector: content written through WriteFileBeneath is FULLY DURABLE —
// no partial temp files are left behind after a successful write.
func TestWriteFileBeneathLeavesNoTempFilesBehind(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	if _, err := WriteFileBeneath(rootFd, "f.go", []byte("x"), 0o644, TargetExpectation{MustNotExist: true}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.go" {
		t.Fatalf("directory entries = %v, want only [f.go]", entries)
	}
}

// Detector (code-review finding, codex, round 2): StatBeneath must
// REFUSE a symlink outright, not report its own identity as a
// capturable TargetExpectation — otherwise a caller could capture a
// planted symlink's Dev/Ino and later have WriteFileBeneath's identity
// check "match" it, authorizing an overwrite of the symlink instead of
// refusing it. This also proves StatBeneath does not silently FOLLOW
// the symlink either: if it did, it would succeed (reporting real.go's
// identity) rather than error.
func TestStatBeneathRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	if _, _, err := StatBeneath(rootFd, "real.go"); err != nil {
		t.Fatalf("StatBeneath on a real regular file should succeed: %v", err)
	}
	if _, _, err := StatBeneath(rootFd, "link.go"); err == nil {
		t.Fatal("expected StatBeneath to refuse a symlink, not report its identity")
	}
}

// Detector (code-review finding, codex, round 3): if a DIFFERENT writer
// replaces baseName concurrently, exactly in the window between
// WriteFileBeneath detecting an identity mismatch and performing its
// restoring exchange, that writer's content must SURVIVE — never be
// silently deleted by WriteFileBeneath's own cleanup. Uses the
// beforeRestoreExchange test seam to inject the concurrent write
// deterministically, without a real (flaky) goroutine race.
func TestWriteFileBeneathPreservesConcurrentWriteDuringMismatchRestore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	// A deliberately WRONG expectation guarantees the mismatch branch
	// runs (same technique as TestWriteFileBeneathRestoresForeignRegularFileOnIdentityMismatch).
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrongDev, wrongIno, err := StatBeneath(rootFd, "other.go")
	if err != nil {
		t.Fatal(err)
	}

	const concurrentContent = "concurrent editor's legitimate write"
	beforeRestoreExchange = func() {
		// Simulate another writer atomically replacing baseName with
		// its own new content, exactly in the exposed window.
		tmp := filepath.Join(root, ".other-writer-tmp")
		if err := os.WriteFile(tmp, []byte(concurrentContent), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(root, "f.go")); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRestoreExchange = func() {} })

	_, err = WriteFileBeneath(rootFd, "f.go", []byte("our content, should not land"), 0o644, TargetExpectation{Dev: wrongDev, Ino: wrongIno})
	if err == nil {
		t.Fatal("expected WriteFileBeneath to refuse (wrong expectation)")
	}

	// baseName must hold the ORIGINAL content restored — not our new
	// content, and not lost either.
	got, err := os.ReadFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original content" {
		t.Fatalf("f.go = %q, want restored %q", got, "original content")
	}

	// The concurrent writer's content must be found SOMEWHERE in the
	// directory — never silently deleted.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err == nil && string(b) == concurrentContent {
			found = true
		}
	}
	if !found {
		t.Fatal("the concurrent writer's content was lost — WriteFileBeneath deleted it during mismatch restore")
	}
}

// Detector (code-review finding, codex, round 2, defense-in-depth): even
// if a caller bypassed StatBeneath's own symlink refusal and captured a
// symlink's raw Dev/Ino directly (e.g. a future caller stats via some
// other path), WriteFileBeneath's OWN S_IFREG check on the displaced
// entry — not just StatBeneath's gate — must still refuse the swap.
// Identity (Dev/Ino) alone is not sufficient; type must match too.
func TestWriteFileBeneathRefusesSymlinkEvenWithMatchingRawIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	// Raw stat of the symlink itself, bypassing StatBeneath's own guard.
	var st unix.Stat_t
	if err := unix.Fstatat(rootFd, "link.go", &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatal(err)
	}

	_, err = WriteFileBeneath(rootFd, "link.go", []byte("attacker content"), 0o644, TargetExpectation{Dev: uint64(st.Dev), Ino: st.Ino})
	if err == nil {
		t.Fatal("expected WriteFileBeneath to refuse replacing a symlink even with its own exact Dev/Ino as the expectation")
	}
	fi, err := os.Lstat(filepath.Join(root, "link.go"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link.go should still be a symlink — WriteFileBeneath must not have touched it")
	}
}

// Detector (code-review finding, codex, round 2): even if a caller
// somehow obtained a foreign REGULAR file's Dev/Ino (not a symlink —
// StatBeneath already refuses those), WriteFileBeneath's exchange-
// verify-restore sequence must still refuse a target whose actual
// identity does not match, and must leave that foreign file completely
// untouched, restored under its original name.
func TestWriteFileBeneathRestoresForeignRegularFileOnIdentityMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	// Identity captured for a DIFFERENT file — simulates Prepare having
	// hashed one file, but f.go having since been replaced (out-of-band,
	// e.g. a concurrent writer) with an unrelated regular file of the
	// same name.
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	wrongDev, wrongIno, err := StatBeneath(rootFd, "other.go")
	if err != nil {
		t.Fatal(err)
	}

	_, err = WriteFileBeneath(rootFd, "f.go", []byte("should not land"), 0o644, TargetExpectation{Dev: wrongDev, Ino: wrongIno})
	if err == nil {
		t.Fatal("expected WriteFileBeneath to refuse — f.go's identity does not match the captured expectation")
	}

	got, err := os.ReadFile(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original content" {
		t.Fatalf("f.go was modified despite the identity mismatch: %q", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("directory entries = %v, want exactly [f.go, other.go] — no leftover temp files", entries)
	}
}

// Detector (code-review finding, codex, transaction.go round 1): a
// trailing failure AFTER the exchange has already landed (here: the
// directory fsync) must still report committed==true — the new content
// is genuinely on disk, and a caller that only checks err!=nil would
// wrongly conclude nothing happened.
func TestWriteFileBeneathReportsCommittedEvenWhenTrailingFsyncFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}

	orig := fsyncDirHook
	defer func() { fsyncDirHook = orig }()
	fsyncDirHook = func(fd int) error { return fmt.Errorf("injected fsync failure") }

	committed, err := WriteFileBeneath(rootFd, "f.go", []byte("new content"), 0o644, TargetExpectation{Dev: dev, Ino: ino})
	if err == nil {
		t.Fatal("expected the injected fsync error to propagate")
	}
	if !committed {
		t.Fatal("expected committed = true — the exchange already landed before the fsync failure")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "f.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "new content" {
		t.Fatalf("f.go content = %q, want %q — committed=true must mean the write genuinely took effect", got, "new content")
	}
}

// Detector (code-review finding, codex, transaction.go round 1): identity
// (Dev/Ino) alone does not catch a writer that mutates a file's content
// IN PLACE (same inode, via truncate+rewrite) between the caller
// capturing Dev/Ino and this call — invariant 4 explicitly requires
// content to be checked "independent of inode". ContentDigest closes
// this: even with a perfectly matching Dev/Ino, a content mismatch must
// still be refused.
func TestWriteFileBeneathRefusesContentDigestMismatchDespiteMatchingIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest := strings.Repeat("0", 64)

	// Same Dev/Ino, but a digest that does not match the ACTUAL current
	// content ("original content") — simulates a caller whose captured
	// before-content is stale relative to an in-place mutation that kept
	// the same inode.
	committed, err := WriteFileBeneath(rootFd, "f.go", []byte("new content"), 0o644, TargetExpectation{
		Dev: dev, Ino: ino, ContentDigest: wrongDigest,
	})
	if err == nil {
		t.Fatal("expected WriteFileBeneath to refuse — content digest does not match despite matching Dev/Ino")
	}
	if committed {
		t.Fatal("expected committed = false — the mismatch was caught and restored")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "f.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original content" {
		t.Fatalf("f.go content = %q, want untouched %q", got, "original content")
	}
}

// Detector: RemoveFileWithExpectedIdentity actually removes a file whose
// identity matches, and leaves no tombstone or temp artifacts behind.
func TestRemoveFileWithExpectedIdentityRemovesMatchingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveFileWithExpectedIdentity(rootFd, "f.go", TargetExpectation{Dev: dev, Ino: ino}); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "f.go")); !os.IsNotExist(statErr) {
		t.Fatal("f.go should have been removed")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory entries = %v, want empty — no leftover tombstone/temp files", entries)
	}
}

// Detector (code-review finding, codex, transaction.go round 1: the
// original RemoveFileWithExpectedIdentity was a plain fstatat-then-
// unlinkat, a real check-then-act race — rewritten with the same
// exchange-verify-restore discipline as WriteFileBeneath's replace
// path). A target whose identity has drifted (here: swapped for a
// symlink) must be refused, left completely untouched.
func TestRemoveFileWithExpectedIdentityRefusesDriftedTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go") // simulates an earlier capture
	if err != nil {
		t.Fatal(err)
	}

	// Drift: f.go is replaced by a symlink AFTER identity was captured.
	if err := os.Remove(filepath.Join(root, "f.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "f.go")); err != nil {
		t.Fatal(err)
	}

	err = RemoveFileWithExpectedIdentity(rootFd, "f.go", TargetExpectation{Dev: dev, Ino: ino})
	if err == nil {
		t.Fatal("expected RemoveFileWithExpectedIdentity to refuse — the target's identity drifted to a planted symlink")
	}
	fi, err := os.Lstat(filepath.Join(root, "f.go"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("f.go should still be the planted symlink — RemoveFileWithExpectedIdentity must not have touched it")
	}
	outsideContent, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "s3cr3t" {
		t.Fatal("outside file was unexpectedly modified through the planted symlink")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.go" {
		t.Fatalf("directory entries = %v, want exactly [f.go] — no leftover tombstone/temp files", entries)
	}
}

// Detector (code-review finding, codex, round 2, then round 3 — the
// round-2 "fix" moved a re-verification immediately before the final
// unlink of the PUBLIC name baseName, but that was still a separate
// fstatat-then-unlinkat: narrower, not closed. The round-3 redesign
// quarantines (renames away) baseName FIRST, unconditionally, so
// baseName is definitively vacated before any verification happens —
// there is no public name left to race a cleanup deletion against at
// all). A concurrent CREATE at baseName, injected via the
// beforeQuarantineVerify seam immediately after quarantine, must
// survive completely untouched, AND the original (correctly
// quarantined) target must still be removed successfully — the two
// no longer interfere with each other at all.
func TestRemoveFileWithExpectedIdentityPreservesConcurrentCreateDuringQuarantine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}

	const concurrentContent = "concurrent writer's legitimate new file"
	orig := beforeQuarantineVerify
	defer func() { beforeQuarantineVerify = orig }()
	fired := false
	beforeQuarantineVerify = func() {
		if fired {
			return
		}
		fired = true
		if err := os.WriteFile(filepath.Join(root, "f.go"), []byte(concurrentContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemoveFileWithExpectedIdentity(rootFd, "f.go", TargetExpectation{Dev: dev, Ino: ino}); err != nil {
		t.Fatalf("expected the original (correctly quarantined) target to be removed successfully, got: %v", err)
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}

	got, readErr := os.ReadFile(filepath.Join(root, "f.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != concurrentContent {
		t.Fatalf("f.go content = %q, want the concurrent writer's untouched content %q", got, concurrentContent)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "f.go" {
		t.Fatalf("directory entries = %v, want exactly [f.go] — no leftover quarantine files", entries)
	}
}

// Detector (code-review finding, codex, round 2): the earlier version of
// RemoveFileWithExpectedIdentity checked only Dev/Ino, so a file mutated
// IN PLACE (same inode) after its identity was captured would still
// "match" and be deleted, losing whatever content was written there in
// the meantime. ContentDigest+CheckMode close this the same way they
// close it for WriteFileBeneath's replace path.
func TestRemoveFileWithExpectedIdentityRefusesContentDigestMismatchDespiteMatchingIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest := strings.Repeat("0", 64)

	err = RemoveFileWithExpectedIdentity(rootFd, "f.go", TargetExpectation{Dev: dev, Ino: ino, ContentDigest: wrongDigest})
	if err == nil {
		t.Fatal("expected RemoveFileWithExpectedIdentity to refuse — content digest does not match despite matching Dev/Ino")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "f.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original content" {
		t.Fatalf("f.go content = %q, want untouched %q", got, "original content")
	}
}

// Detector (code-review finding, codex, round 2): Openat2's create Mode
// is masked by the process umask like any O_CREAT, so requesting 0666
// under a typical restrictive umask would silently produce a narrower
// mode — contradicting FileMutation.Mode's "the result ALWAYS ends up
// with" contract. WriteFileBeneath must Fchmod to the EXACT requested
// bits regardless of umask.
func TestWriteFileBeneathAppliesExactModeRegardlessOfUmask(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	oldUmask := unix.Umask(0o077) // would mask 0666 down to 0600 without the fix
	defer unix.Umask(oldUmask)

	if _, err := WriteFileBeneath(rootFd, "f.go", []byte("x"), 0o666, TargetExpectation{MustNotExist: true}); err != nil {
		t.Fatal(err)
	}
	fi, statErr := os.Stat(filepath.Join(root, "f.go"))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if fi.Mode().Perm() != 0o666 {
		t.Fatalf("f.go mode = %o, want exactly %o regardless of umask", fi.Mode().Perm(), 0o666)
	}
}

// Detector (code-review finding, codex, round 4): RemoveFileWithExpectedIdentity's
// initial quarantine step used a plain Renameat, which silently clobbers
// an existing entry at the destination name. Forces tempName (a var,
// specifically so this is possible) to return a name that ALREADY has
// unrelated sentinel content — RENAME_NOREPLACE must make the quarantine
// step itself fail, leaving both the sentinel content and baseName
// completely untouched.
func TestRemoveFileWithExpectedIdentityRefusesToClobberCollidingQuarantineName(t *testing.T) {
	root := t.TempDir()
	const collidingName = ".nexus-workspace-tmp-collision-test"
	if err := os.WriteFile(filepath.Join(root, collidingName), []byte("unrelated sentinel content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "f.go"), []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	dev, ino, err := StatBeneath(rootFd, "f.go")
	if err != nil {
		t.Fatal(err)
	}

	orig := tempName
	defer func() { tempName = orig }()
	tempName = func() (string, error) { return collidingName, nil }

	err = RemoveFileWithExpectedIdentity(rootFd, "f.go", TargetExpectation{Dev: dev, Ino: ino})
	if err == nil {
		t.Fatal("expected an error: the quarantine name collides with an existing unrelated file")
	}

	got, readErr := os.ReadFile(filepath.Join(root, collidingName))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "unrelated sentinel content" {
		t.Fatalf("colliding sentinel file content = %q, want untouched %q", got, "unrelated sentinel content")
	}
	got, readErr = os.ReadFile(filepath.Join(root, "f.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original content" {
		t.Fatalf("f.go content = %q, want untouched %q", got, "original content")
	}
}

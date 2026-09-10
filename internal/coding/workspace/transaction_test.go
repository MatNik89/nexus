//go:build linux

package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"golang.org/x/sys/unix"
)

// noopBindDurable stands in for the future S7/journal durable-reference
// hook (not built in this increment — see transaction.go's package doc)
// for tests that don't care about it.
var noopBindDurable = func(string) error { return nil }

func newTestStore(t *testing.T) *sealedstore.Store {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := sealedstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// Detector: a transaction touching several NEW files (none existed
// before) writes them all and reports Committed.
func TestApplyCreatesMultipleNewFiles(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package b\n"), Mode: 0o644},
	}, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}
	if res.BundleDigest == "" {
		t.Fatal("expected a non-empty BundleDigest")
	}
	for name, want := range map[string]string{"a.go": "package a\n", "b.go": "package b\n"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s content = %q, want %q", name, got, want)
		}
	}
}

// Detector: a transaction replacing EXISTING files (a real rename-like
// operation) writes the new content and preserves the sealed bundle.
func TestApplyReplacesMultipleExistingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.go"), []byte("package old_b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{RelDir: "sub", BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}
	got, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package new_a\n" {
		t.Fatalf("a.go content = %q", got)
	}
	got, err = os.ReadFile(filepath.Join(root, "sub", "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package new_b\n" {
		t.Fatalf("sub/b.go content = %q", got)
	}
}

// Detector: upfront validation (an invalid base name) must reject the
// WHOLE transaction before any file is touched — the second mutation's
// bad name must not leave the first mutation partially applied.
func TestApplyValidatesAllMutationsBeforeAnyWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "sub/b.go", After: []byte("package new_b\n"), Mode: 0o644}, // invalid base name
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: the second mutation's base name is invalid")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want untouched original %q", got, "package old_a\n")
	}
}

// Detector (the core invariant-4-step-5 guarantee): if a LATER mutation
// fails to write because its on-disk identity drifted mid-transaction
// (a symlink planted between Apply's prepare pass and its write pass —
// exactly the scenario the identity check exists to catch), every
// EARLIER mutation that already succeeded must be rolled back to its
// original content. A partial transaction must never be observable.
// Uses the beforeApplyWritesFile test seam (no-op in production) to
// inject the drift deterministically, at the exact moment Apply is
// about to write the second file — mirroring descriptor_test.go's own
// beforeRestoreExchange technique for the same class of problem.
func TestApplyRollsBackAlreadyWrittenFileOnMidTransactionFailure(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			if err := os.Remove(filepath.Join(root, "b.go")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "b.go")); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: b.go's identity drifted to a planted symlink mid-transaction")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}

	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want rolled back to %q", got, "package old_a\n")
	}
	fi, err := os.Lstat(filepath.Join(root, "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("b.go should still be the planted symlink")
	}
	outsideContent, err := os.ReadFile(filepath.Join(outside, "secret.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "s3cr3t" {
		t.Fatal("outside file was unexpectedly modified through the planted symlink")
	}
}

// Detector: a brand-new file (didn't exist before the transaction) that
// gets rolled back must be REMOVED, not left behind with new content.
func TestApplyRollsBackNewlyCreatedFileByRemovingIt(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "b.go")); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644}, // was new, but got planted as a symlink first
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: b.go now exists (as a symlink) when MustNotExist was required")
	}

	if _, statErr := os.Lstat(filepath.Join(root, "a.go")); statErr == nil {
		t.Fatal("a.go should have been removed by rollback (it was newly created)")
	} else if !os.IsNotExist(statErr) {
		t.Fatal(statErr)
	}
	fi, err := os.Lstat(filepath.Join(root, "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("b.go should still be the planted symlink, untouched")
	}
}

// Detector: replacing an existing file with an EXPLICITLY DIFFERENT mode
// actually changes its permission bits — WriteFileBeneath's replace path
// never preserves the pre-existing file's own mode, it always applies
// the mutation's own Mode (the temp file is created at that mode and
// exchanged into place). A caller that wants to preserve permissions
// must capture and pass them through itself.
func TestApplyReplaceAppliesMutationModeNotOriginalMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("new"), Mode: 0o600},
	}, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("a.go mode = %o, want %o (the mutation's own Mode, not the original 0644)", fi.Mode().Perm(), 0o600)
	}
}

// Detector: an empty mutation set is refused outright rather than
// silently doing nothing and reporting success.
func TestApplyRefusesEmptyMutationSet(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	if _, err := Apply(rootFd, store, nil, noopBindDurable); err == nil {
		t.Fatal("expected an error for an empty mutation set")
	}
}

// Detector: the sealed bundle Apply persists genuinely contains the
// before AND after bytes — reconstructable, not just a digest (the
// specific requirement invariant 4 calls out by name).
func TestApplyPersistsReconstructableBeforeAndAfterBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
	}, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Get(res.BundleDigest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded bundle
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Files) != 1 {
		t.Fatalf("decoded bundle has %d file records, want 1", len(decoded.Files))
	}
	if string(decoded.Files[0].Before) != "package old_a\n" {
		t.Fatalf("decoded before-image = %q, want %q", decoded.Files[0].Before, "package old_a\n")
	}
	if string(decoded.Files[0].After) != "package new_a\n" {
		t.Fatalf("decoded after-image = %q, want %q", decoded.Files[0].After, "package new_a\n")
	}
}

// Detector (code-review finding, codex, round 1): a file whose
// WriteFileBeneath call reports committed==true but ALSO returns an
// error (here: the trailing directory fsync fails right after the
// second mutation's exchange already landed) must still be included in
// Apply's own rollback set — not silently omitted because the call
// "returned an error". Both mutations must end up back at their
// original content.
func TestApplyRollsBackFileThatCommittedButThenReturnedAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	origHook := fsyncDirHook
	defer func() { fsyncDirHook = origHook }()
	calls := 0
	fsyncDirHook = func(fd int) error {
		calls++
		if calls == 2 { // b.go's own write — force it to commit, then fail
			return fmt.Errorf("injected fsync failure")
		}
		return unix.Fsync(fd)
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: b.go's trailing fsync was injected to fail")
	}

	gotA, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotA) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want rolled back to %q", gotA, "package old_a\n")
	}
	gotB, readErr := os.ReadFile(filepath.Join(root, "b.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(gotB) != "package old_b\n" {
		t.Fatalf("b.go content = %q, want rolled back to %q (it committed despite the fsync error and must still be rolled back)", gotB, "package old_b\n")
	}
}

// Detector (code-review finding, codex, round 1): a rollback must
// restore the ORIGINAL mode, not the mutation's requested after-mode —
// the earlier version stored the after-mode as "beforeMode" by mistake,
// so a 0644→0600 change, rolled back due to a later mutation's failure,
// would have restored the old BYTES at the NEW mode.
func TestApplyRollbackRestoresOriginalModeNotAfterMode(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package old_b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			if err := os.Remove(filepath.Join(root, "b.go")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "b.go")); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o600},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: b.go's identity drifted mid-transaction")
	}

	fi, statErr := os.Stat(filepath.Join(root, "a.go"))
	if statErr != nil {
		t.Fatal(statErr)
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want rolled back to %q", got, "package old_a\n")
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("a.go mode = %o, want rolled back to the ORIGINAL %o, not the mutation's after-mode %o", fi.Mode().Perm(), 0o644, 0o600)
	}
}

// Detector (code-review finding, codex, round 1): if bindDurable — the
// seam the future S7/journal durable reference will use — fails, Apply
// must abort BEFORE any file is touched. The plan's required ordering is
// "bundle, durable reference, THEN first write"; this proves Apply
// actually honors it rather than writing files first and reporting the
// durable-binding failure as an afterthought.
func TestApplyAbortsBeforeAnyWriteWhenBindDurableFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package b\n"), Mode: 0o644},
	}, func(digest string) error { return fmt.Errorf("simulated durable-binding failure") })
	if err == nil {
		t.Fatal("expected an error: bindDurable was made to fail")
	}

	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package old_a\n" {
		t.Fatalf("a.go content = %q, want untouched %q — no write should happen when bindDurable fails", got, "package old_a\n")
	}
	if _, statErr := os.Stat(filepath.Join(root, "b.go")); !os.IsNotExist(statErr) {
		t.Fatal("b.go should not have been created when bindDurable fails")
	}
}

// Detector: a mutation set naming the SAME target (RelDir+BaseName)
// twice is refused upfront, before any write — cheap, cleaner than
// relying on WriteFileBeneath's own identity check to incidentally catch
// it after the first write already landed.
func TestApplyRejectsDuplicateTarget(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("first"), Mode: 0o644},
		{BaseName: "a.go", After: []byte("second"), Mode: 0o644},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: a.go named twice in one mutation set")
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.go")); !os.IsNotExist(statErr) {
		t.Fatal("a.go should not have been created — duplicate-target validation must run before any write")
	}
}

// Detector (code-review finding, codex, round 2): if a NEWLY-CREATED
// file (no before-image, so rollback removes rather than restores it)
// gets its content mutated IN PLACE (same inode) by another writer
// after Apply successfully wrote it but before a LATER mutation fails
// and triggers rollback, that foreign content must survive — never be
// silently deleted just because the inode still matches. Exercises
// RemoveFileWithExpectedIdentity's ContentDigest binding through the
// full Apply rollback path, not just directly.
func TestApplyRollbackRefusesToDeleteForeignInPlaceContentMutation(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s3cr3t"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	const foreignContent = "a concurrent writer's in-place mutation, same inode"
	orig := beforeApplyWritesFile
	defer func() { beforeApplyWritesFile = orig }()
	fired := false
	beforeApplyWritesFile = func(baseName string) {
		if baseName == "b.go" && !fired {
			fired = true
			// Mutate a.go's content IN PLACE (O_TRUNC on the existing
			// file preserves its inode) — simulates a writer that
			// touched the file Apply just created, between that write
			// and the rollback this test forces below.
			if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(foreignContent), 0o644); err != nil {
				t.Fatal(err)
			}
			// Force b.go's own write to fail via the same symlink-drift
			// technique used elsewhere.
			if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "b.go")); err != nil {
				t.Fatal(err)
			}
		}
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: b.go's identity drifted to a planted symlink")
	}

	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != foreignContent {
		t.Fatalf("a.go content = %q, want the foreign in-place mutation %q preserved (rollback must refuse to delete content it never wrote)", got, foreignContent)
	}
}

// Detector (code-review finding, codex, symedit round 1): a caller that
// validates a preimage ITSELF (e.g. symedit's own Prepare/Apply split)
// and supplies ExpectedBeforeDigest must have Apply refuse — before any
// write — if the file's ACTUAL content at Apply's own capture point does
// not match, even though the caller's own earlier check might have
// passed against now-stale bytes. Closes the window between an outer
// caller's validation and Apply's own capture, rather than leaving a
// gap for a caller to (incorrectly) trust its own stale check.
func TestApplyRefusesWhenExpectedBeforeDigestMismatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("actual content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("new content"), Mode: 0o644, ExpectedBeforeDigest: sha256Hex([]byte("stale expected content"))},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: ExpectedBeforeDigest does not match a.go's actual content")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "actual content" {
		t.Fatalf("a.go content = %q, want untouched %q", got, "actual content")
	}
}

// Detector: ExpectedBeforeDigest, when it DOES match, does not block the
// write — it's a precondition, not an unconditional refusal.
func TestApplyAcceptsWhenExpectedBeforeDigestMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("actual content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("new content"), Mode: 0o644, ExpectedBeforeDigest: sha256Hex([]byte("actual content"))},
	}, noopBindDurable)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "new content" {
		t.Fatalf("a.go content = %q, want %q", got, "new content")
	}
}

// Detector: ExpectedBeforeDigest set on a mutation whose target file does
// NOT exist at all (existed==false) is also a mismatch — a caller that
// expected specific pre-existing content must not have Apply silently
// treat "the file is gone" as acceptable.
func TestApplyRefusesWhenExpectedBeforeDigestSetButFileMissing(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	_, err = Apply(rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("new content"), Mode: 0o644, ExpectedBeforeDigest: sha256Hex([]byte("expected content"))},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: a.go does not exist but ExpectedBeforeDigest was set")
	}
	if _, statErr := os.Stat(filepath.Join(root, "a.go")); !os.IsNotExist(statErr) {
		t.Fatal("a.go should not have been created")
	}
}

// Detector (code-review finding, codex, symedit round 2): content alone
// does not catch an atomic replace-with-identical-bytes that changes
// only the file's identity — CheckExpectedIdentity must independently
// refuse when Dev/Ino don't match the caller's expectation, even though
// content is byte-identical.
func TestApplyRefusesWhenExpectedIdentityMismatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	// Atomic replace-with-identical-bytes: new inode, same content.
	tmp := filepath.Join(root, ".replacement")
	if err := os.WriteFile(tmp, []byte("same content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(root, "a.go")); err != nil {
		t.Fatal(err)
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{
			BaseName: "a.go", After: []byte("new content"), Mode: 0o644,
			ExpectedBeforeDigest:  sha256Hex([]byte("same content")),
			ExpectedBeforeDev:     999999, ExpectedBeforeIno: 999999,
			CheckExpectedIdentity: true,
		},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: identity does not match despite matching content")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "same content" {
		t.Fatalf("a.go content = %q, want untouched %q", got, "same content")
	}
}

// Detector (code-review finding, codex, symedit round 2): a
// content-preserving chmod between an outer caller's own capture and
// Apply's capture must be caught by CheckExpectedMode — Apply must not
// silently overwrite the file at the caller's stale expected mode.
func TestApplyRefusesWhenExpectedModeMismatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	dev, ino, err := StatBeneath(rootFd, "a.go")
	if err != nil {
		t.Fatal(err)
	}

	_, err = Apply(rootFd, store, []FileMutation{
		{
			BaseName: "a.go", After: []byte("new content"), Mode: 0o600,
			ExpectedBeforeDigest:  sha256Hex([]byte("content")),
			ExpectedBeforeDev:     dev, ExpectedBeforeIno: ino,
			CheckExpectedIdentity: true,
			ExpectedBeforeMode:    0o644, // stale: actual mode is 0600
			CheckExpectedMode:     true,
		},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected an error: mode does not match the caller's expectation despite matching content and identity")
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "content" {
		t.Fatalf("a.go content = %q, want untouched %q", got, "content")
	}
}

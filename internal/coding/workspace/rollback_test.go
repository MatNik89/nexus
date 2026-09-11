//go:build linux

package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// realMidCrashBundle runs a REAL, successful apply() (actual files
// written on disk) and returns the resulting sealed Bundle — the SAME
// pattern recovery_test.go's own
// TestClassifyBundleOfARealSuccessfulApplyIsIndistinguishableFromMidCrash
// uses: a successful apply looks identical, on disk, to a mid-crash
// (every file AFTER, no mutation_committed marker) — exactly the shape
// rollbackBundle must operate on.
func realMidCrashBundle(t *testing.T, rootFd int, mutations []FileMutation) Bundle {
	t.Helper()
	store := newTestStore(t)
	res, err := apply(context.Background(), rootFd, store, mutations, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}
	raw, err := store.Get(res.BundleDigest)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Detector: a real mid-crash bundle (existing file replaced) is fully
// rolled back — content and mode restored exactly, and the reported
// state is NEVER_STARTED ("all-BEFORE achieved").
func TestRollbackBundleRestoresReplacedExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	})
	if got, err := os.ReadFile(filepath.Join(root, "a.go")); err != nil || string(got) != "package new\n" {
		t.Fatalf("precondition: expected the crash-shaped AFTER content on disk: got=%q err=%v", got, err)
	}

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED (all-BEFORE achieved)", state)
	}
	if len(classes) != 1 || classes[0].State != FileBefore {
		t.Fatalf("classes = %+v, want a single BEFORE entry", classes)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil || string(got) != "package old\n" {
		t.Fatalf("expected the file restored to its original content: got=%q err=%v", got, err)
	}
	if st, err := os.Stat(filepath.Join(root, "a.go")); err != nil || st.Mode().Perm() != 0o644 {
		t.Fatalf("expected mode restored to 0644: %v err=%v", st, err)
	}
}

// Detector: a real mid-crash bundle for a NEWLY CREATED file (Existed:
// false) is rolled back by REMOVING it entirely, not by writing empty
// content — invariant 4's own "did not exist before" case.
func TestRollbackBundleRemovesNewlyCreatedFile(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "new.go", After: []byte("package new\n"), Mode: 0o644},
	})
	if _, err := os.Stat(filepath.Join(root, "new.go")); err != nil {
		t.Fatalf("precondition: expected the file to exist (crash-shaped AFTER): %v", err)
	}

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", state)
	}
	if len(classes) != 1 || classes[0].State != FileBefore {
		t.Fatalf("classes = %+v, want a single BEFORE entry", classes)
	}
	if _, err := os.Stat(filepath.Join(root, "new.go")); !os.IsNotExist(err) {
		t.Fatalf("expected new.go to be removed, stat err = %v", err)
	}
}

// Detector: a MULTI-file mid-crash bundle where one file has ALREADY
// been restored (e.g. by a prior, partial rollback attempt) and one has
// not — rollbackBundle leaves the already-restored file untouched
// (idempotent) and still restores the other, converging to full
// NEVER_STARTED in one pass.
func TestRollbackBundleConvergesFromPartiallyRolledBackState(t *testing.T) {
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

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	})

	// Simulate a prior partial rollback attempt: a.go already restored
	// by hand, b.go left at AFTER (as a real crash mid-loop would leave
	// the remaining files).
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", state)
	}
	for _, c := range classes {
		if c.State != FileBefore {
			t.Fatalf("classes = %+v, want every entry BEFORE", classes)
		}
	}
	if got, err := os.ReadFile(filepath.Join(root, "b.go")); err != nil || string(got) != "package old_b\n" {
		t.Fatalf("expected b.go restored: got=%q err=%v", got, err)
	}
}

// Detector: a bundle already FOREIGN (a file mutated by something else
// entirely, matching neither image) refuses outright — rollbackBundle
// must write NOTHING, never guess.
func TestRollbackBundleRefusesForeignBundleWithoutWriting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	})
	// Something ELSE mutates the file to a THIRD, unrelated value.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", state)
	}
	if len(classes) != 1 || classes[0].State != FileForeign {
		t.Fatalf("classes = %+v, want a single FOREIGN entry", classes)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil || string(got) != "package foreign\n" {
		t.Fatalf("expected the foreign content UNTOUCHED: got=%q err=%v", got, err)
	}
}

// Detector: a bundle already fully at BEFORE (nothing to roll back —
// e.g. rollbackBundle called again after it already fully succeeded) is
// a pure no-op: reports NEVER_STARTED, touches nothing.
func TestRollbackBundleNoOpWhenAlreadyAllBefore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	})
	// Already rolled all the way back by hand.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", state)
	}
	if len(classes) != 1 || classes[0].State != FileBefore {
		t.Fatalf("classes = %+v, want a single BEFORE entry", classes)
	}
}

// Detector: a pre-cancelled context stops rollbackBundle from
// attempting ANY restore — zero writes — while still returning the
// truthful (unchanged) classification, never a guessed success.
func TestRollbackBundleWritesNothingUnderCancelledContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	})

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	classes, state, err := rollbackBundle(cctx, rootFd, b)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected errors.Is(err, context.Canceled): %v", err)
	}
	if state != TransactionMidCrash {
		t.Fatalf("state = %v, want MID_CRASH (cancelled before any restore was attempted)", state)
	}
	if len(classes) != 1 || classes[0].State != FileAfter {
		t.Fatalf("classes = %+v, want a single AFTER entry (untouched)", classes)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil || string(got) != "package new\n" {
		t.Fatalf("expected the AFTER content untouched: got=%q err=%v", got, err)
	}
}

// Detector: a MULTI-file bundle where one file is FOREIGN and the OTHER
// is legitimately AFTER — the whole bundle must refuse (FOREIGN),
// writing NOTHING, including to the OTHER file whose own per-file state
// would otherwise look like a normal candidate for restoration. A
// single-file Foreign test cannot distinguish this from a narrower
// per-file "skip if not AFTER" check alone — this needs a genuinely
// legitimate AFTER file present alongside the Foreign one to prove the
// WHOLE-bundle refusal is load-bearing, not redundant with the per-file
// loop.
func TestRollbackBundleRefusesWholeBundleWhenOnlyOneFileIsForeign(t *testing.T) {
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

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
		{BaseName: "b.go", After: []byte("package new_b\n"), Mode: 0o644},
	})
	// a.go becomes FOREIGN; b.go is left as a genuinely legitimate AFTER.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package foreign_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", state)
	}
	_ = classes
	// b.go must be UNTOUCHED — the whole-bundle refusal must have
	// prevented its restoration, not just a.go's.
	got, err := os.ReadFile(filepath.Join(root, "b.go"))
	if err != nil || string(got) != "package new_b\n" {
		t.Fatalf("expected b.go's AFTER content untouched (whole-bundle refusal must block it too): got=%q err=%v", got, err)
	}
}

// Detector (codex review round 1, MEDIUM, reproduced live): a NO-OP
// mutation (Before byte-identical to After) leaves an already-BEFORE
// file's identity UNCHANGED — rollbackBundle must not physically
// rewrite it. Needs a genuinely MIXED bundle (one no-op file, one real
// change) to force the overall classification to MID_CRASH at all — a
// single-file no-op bundle classifies NEVER_STARTED upfront and never
// even reaches the per-file loop this test is exercising.
func TestRollbackBundleDoesNotReplaceNoOpMutationEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("same\n"), Mode: 0o644}, // no-op: After == pre-existing content
		{BaseName: "b.go", After: []byte("new\n"), Mode: 0o644},
	})
	_, inoBefore, err := StatBeneath(rootFd, "a.go")
	if err != nil {
		t.Fatal(err)
	}

	_, state, err := rollbackBundle(context.Background(), rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", state)
	}
	_, inoAfter, err := StatBeneath(rootFd, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if inoAfter != inoBefore {
		t.Fatalf("a.go's no-op entry was physically replaced: ino %d -> %d", inoBefore, inoAfter)
	}
}

// Detector (codex review round 1, HIGH #2, reproduced live): a restore
// whose content write succeeds but whose OWN trailing durability fsync
// fails must not be silently absorbed into a clean-looking final
// classification — the returned error must say so, even though the
// file's CONTENT now matches BEFORE.
func TestRollbackBundleReportsDurabilityFailureAlongsideCleanClassification(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("package new\n"), Mode: 0o644},
	})

	errInjectedFsyncFailure := errors.New("injected fsync failure")
	origHook := fsyncDirHook
	defer func() { fsyncDirHook = origHook }()
	fsyncDirHook = func(fd int) error { return errInjectedFsyncFailure }

	classes, state, err := rollbackBundle(context.Background(), rootFd, b)
	if !errors.Is(err, errInjectedFsyncFailure) {
		t.Fatalf("expected errors.Is(err, errInjectedFsyncFailure): %v", err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED (the content itself IS correct — only durability is unproven)", state)
	}
	if len(classes) != 1 || classes[0].State != FileBefore {
		t.Fatalf("classes = %+v, want a single BEFORE entry", classes)
	}
}

// Detector (codex review round 1, HIGH #3, reproduced live): a directly
// constructed Bundle (never reachable through the real apply()->
// DecodeBundle path, but reachable by any caller holding a Bundle value
// directly) naming a duplicate (rel_dir, base_name) target must be
// refused OUTRIGHT — zero writes — not discovered only after a write
// already landed based on the first, ambiguous record.
func TestRollbackBundleRefusesMalformedDuplicateBundleWithoutWriting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("shared_after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	malformed := Bundle{Files: []FileRecord{
		{BaseName: "a.go", Existed: true, Before: []byte("first_before\n"), BeforeMode: 0o644, After: []byte("shared_after\n"), AfterMode: 0o644},
		{BaseName: "a.go", Existed: true, Before: []byte("second_before\n"), BeforeMode: 0o644, After: []byte("shared_after\n"), AfterMode: 0o644},
	}}

	classes, state, err := rollbackBundle(context.Background(), rootFd, malformed)
	if err == nil {
		t.Fatal("expected an error: duplicate (rel_dir, base_name) target")
	}
	if classes != nil || state != 0 {
		t.Fatalf("expected zero classes/state on outright refusal: classes=%+v state=%v", classes, state)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil || string(got) != "shared_after\n" {
		t.Fatalf("expected NO write to have happened at all: got=%q err=%v", got, err)
	}
}

// Detector (codex review round 2, HIGH #1, reproduced live): cancelling
// ctx DURING the LAST (and only) restore's own trailing fsync — a
// timing window the per-file pre-check cannot see, since it only runs
// BEFORE each restore, never during or after the final one — must still
// be observed. Without a post-loop check, this landed a clean
// NEVER_STARTED with a nil error, no trace of the cancellation at all.
func TestRollbackBundleReportsCancellationDuringFinalRestore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := realMidCrashBundle(t, rootFd, []FileMutation{
		{BaseName: "a.go", After: []byte("new\n"), Mode: 0o644},
	})

	ctx, cancel := context.WithCancel(context.Background())
	origHook := fsyncDirHook
	defer func() { fsyncDirHook = origHook }()
	fsyncDirHook = func(fd int) error {
		cancel() // cancellation lands DURING the restore's own fsync, after its own pre-check already passed
		return origHook(fd)
	}

	_, state, err := rollbackBundle(ctx, rootFd, b)
	if err == nil {
		t.Fatal("expected a non-nil error: cancellation during the final restore must not be silently lost")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected errors.Is(err, context.Canceled): %v", err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED (the content itself IS correct — only the cancellation must still be reported)", state)
	}
}

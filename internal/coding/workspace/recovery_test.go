//go:build linux

package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Detector: a bundle whose every file's CURRENT on-disk state matches
// its captured BEFORE image classifies as NeverStarted — the crash (or
// refusal) happened before Apply's write loop touched anything.
func TestClassifyBundleAllBeforeIsNeverStarted(t *testing.T) {
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

	b := Bundle{Files: []FileRecord{
		{BaseName: "a.go", Existed: true, Before: []byte("package old_a\n"), BeforeMode: 0o644, After: []byte("package new_a\n"), AfterMode: 0o644},
		{BaseName: "b.go", Existed: true, Before: []byte("package old_b\n"), BeforeMode: 0o644, After: []byte("package new_b\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", state)
	}
	for _, c := range classes {
		if c.State != FileBefore {
			t.Fatalf("%s classified %v, want BEFORE", c.BaseName, c.State)
		}
	}
}

// Detector: every file matching AFTER classifies MidCrash — the write
// loop finished (or ran far enough) but `mutation_committed` never
// landed; this layer cannot distinguish that from a genuine success
// (deliberately — see this file's own package doc comment: that
// distinction needs the NOT-YET-BUILT restart scan, which also checks
// whether mutation_committed exists in the journal).
func TestClassifyBundleAllAfterIsMidCrash(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package new_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package new_b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := Bundle{Files: []FileRecord{
		{BaseName: "a.go", Existed: true, Before: []byte("package old_a\n"), BeforeMode: 0o644, After: []byte("package new_a\n"), AfterMode: 0o644},
		{BaseName: "b.go", Existed: true, Before: []byte("package old_b\n"), BeforeMode: 0o644, After: []byte("package new_b\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionMidCrash {
		t.Fatalf("state = %v, want MID_CRASH", state)
	}
	for _, c := range classes {
		if c.State != FileAfter {
			t.Fatalf("%s classified %v, want AFTER", c.BaseName, c.State)
		}
	}
}

// Detector: a MIX of before/after files — the crash landed exactly
// mid-write-loop — also classifies MidCrash (not a distinct state; the
// recovery ACTION is identical for AllAfter and Mixed per invariant 4's
// own table: roll back through PolicyWorkspaceRollback either way).
func TestClassifyBundleMixedBeforeAfterIsMidCrash(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package new_a\n"), 0o644); err != nil {
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

	b := Bundle{Files: []FileRecord{
		{BaseName: "a.go", Existed: true, Before: []byte("package old_a\n"), BeforeMode: 0o644, After: []byte("package new_a\n"), AfterMode: 0o644},
		{BaseName: "b.go", Existed: true, Before: []byte("package old_b\n"), BeforeMode: 0o644, After: []byte("package new_b\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionMidCrash {
		t.Fatalf("state = %v, want MID_CRASH", state)
	}
	want := map[string]FileState{"a.go": FileAfter, "b.go": FileBefore}
	for _, c := range classes {
		if c.State != want[c.BaseName] {
			t.Fatalf("%s classified %v, want %v", c.BaseName, c.State, want[c.BaseName])
		}
	}
}

// Detector (invariant 4: "refuse; manual reconciliation only, never a
// guess"): any file matching NEITHER captured image makes the WHOLE
// transaction Foreign — even when every other file is a clean AFTER —
// Foreign takes priority over every other classification.
func TestClassifyBundleAnyForeignMakesWholeTransactionForeign(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package new_a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package SOMETHING_ELSE_ENTIRELY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := Bundle{Files: []FileRecord{
		{BaseName: "a.go", Existed: true, Before: []byte("package old_a\n"), BeforeMode: 0o644, After: []byte("package new_a\n"), AfterMode: 0o644},
		{BaseName: "b.go", Existed: true, Before: []byte("package old_b\n"), BeforeMode: 0o644, After: []byte("package new_b\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", state)
	}
	want := map[string]FileState{"a.go": FileAfter, "b.go": FileForeign}
	for _, c := range classes {
		if c.State != want[c.BaseName] {
			t.Fatalf("%s classified %v, want %v", c.BaseName, c.State, want[c.BaseName])
		}
	}
}

// Detector: a NEW file (Existed=false — the mutation would have
// CREATED it, not replaced it) that genuinely does not exist on disk
// classifies BEFORE — "did not exist" IS its captured before-state.
func TestClassifyBundleNewFileNeverCreatedIsBefore(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := Bundle{Files: []FileRecord{
		{BaseName: "new.go", Existed: false, After: []byte("package new\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED", state)
	}
	if classes[0].State != FileBefore {
		t.Fatalf("new.go classified %v, want BEFORE", classes[0].State)
	}
}

// Detector: content matching the after-image BYTE-FOR-BYTE, but a mode
// that matches NEITHER captured image, must be Foreign — content alone
// is not sufficient (invariant 4: "content, mode, and metadata
// expectations are checked explicitly... independent of inode" — the
// SAME discipline Apply's own preconditions already apply, mirrored
// here for restart-time classification).
func TestClassifyBundleContentMatchesButModeDoesNotIsForeign(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package new_a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := Bundle{Files: []FileRecord{
		// After-content matches exactly, but the bundle recorded mode
		// 0o644 for both before and after — the on-disk file is 0o600,
		// matching neither.
		{BaseName: "a.go", Existed: true, Before: []byte("package old_a\n"), BeforeMode: 0o644, After: []byte("package new_a\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", state)
	}
	if classes[0].State != FileForeign {
		t.Fatalf("a.go classified %v, want FOREIGN", classes[0].State)
	}
}

// Detector: a file the bundle says EXISTED before, but is now entirely
// MISSING (neither before nor after — an unexplained deletion), is
// Foreign, never silently treated as "still before."
func TestClassifyBundleMissingPreviouslyExistingFileIsForeign(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	b := Bundle{Files: []FileRecord{
		{BaseName: "gone.go", Existed: true, Before: []byte("package old\n"), BeforeMode: 0o644, After: []byte("package new\n"), AfterMode: 0o644},
	}}

	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionForeign {
		t.Fatalf("state = %v, want FOREIGN", state)
	}
	if classes[0].State != FileForeign {
		t.Fatalf("gone.go classified %v, want FOREIGN", classes[0].State)
	}
}

// Detector: DecodeBundle strictly decodes (no unknown fields, no
// trailing data) and refuses an empty file list — mirrors this
// package's own established discipline for every other closed-shape
// decoder.
func TestDecodeBundleRefusesMalformedShapes(t *testing.T) {
	if _, err := DecodeBundle([]byte(`{"files":[]}`)); err == nil {
		t.Fatal("expected an error: empty files list")
	}
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":true,"after":"eA==","after_mode":420}],"unknown_top_level_field":1}`)); err == nil {
		t.Fatal("expected an error: unknown top-level field")
	}
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":true,"after":"eA==","after_mode":420}]}{}`)); err == nil {
		t.Fatal("expected an error: trailing data after the JSON value")
	}
	good := Bundle{Files: []FileRecord{{BaseName: "a.go", Existed: false, After: []byte("x"), AfterMode: 420}}}
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeBundle(raw); err != nil {
		t.Fatalf("expected a well-formed bundle to decode: %v", err)
	}
}

// Detector: ClassifyBundle refuses an empty bundle outright — there is
// nothing to classify, and silently reporting "NeverStarted" for zero
// files would be a meaningless, never-actually-producible narrative.
func TestClassifyBundleRefusesEmptyBundle(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	if _, _, err := ClassifyBundle(rootFd, Bundle{}); err == nil {
		t.Fatal("expected an error: empty bundle")
	}
}

// Detector (integration): a REAL Apply call's own sealed bundle, read
// back exactly as a future restart scan would (store.Get + DecodeBundle),
// classifies MidCrash immediately after a genuine SUCCESS — proving this
// layer's own documented boundary is real, not assumed: classification
// alone cannot distinguish "succeeded" from "crashed after the last
// write" — that distinction is deliberately deferred to the restart
// scan (checking mutation_committed), not built here.
func TestClassifyBundleOfARealSuccessfulApplyIsIndistinguishableFromMidCrash(t *testing.T) {
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

	res, err := apply(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package new_a\n"), Mode: 0o644},
	}, noopBindDurable)
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

	_, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionMidCrash {
		t.Fatalf("state = %v, want MID_CRASH (this layer cannot tell success from mid-crash-all-after by design)", state)
	}
}

// Detector: trailing bytes that are NOT another JSON value (plain
// garbage, not just a second `{}`) must also be refused — codex review
// round 1, HIGH #1: dec.Token()'s error return was being treated as
// "no trailing data" instead of being distinguished from io.EOF.
func TestDecodeBundleRefusesInvalidTrailingGarbage(t *testing.T) {
	good := `{"files":[{"rel_dir":"","base_name":"a.go","existed":false,"after":"eA==","after_mode":420}]}`
	if _, err := DecodeBundle([]byte(good + "not valid json")); err == nil {
		t.Fatal("expected an error: invalid trailing garbage after the JSON value")
	}
}

// Detector: a sealed bundle entry missing "after"/"after_mode" must be
// refused outright, never silently decoded as an empty-file, mode-0
// after-image — codex review round 1, HIGH #1: that zero-value fill-in
// let a MISSING on-disk file falsely classify as FileBefore (a
// non-existent file with fr.Existed=false "matches" a zero-value after
// image just as easily as a real one).
// Detector: existed=false paired with a nonzero before/before_mode is
// an internally inconsistent record — "this file did not exist before"
// cannot coexist with a captured before-image — refused rather than
// silently ignored (codex review round 2, HIGH #1's remaining scope).
func TestDecodeBundleRefusesContradictoryExistedFalseWithBeforeSet(t *testing.T) {
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":false,"before":"eA==","after":"eQ==","after_mode":420}]}`)); err == nil {
		t.Fatal("expected an error: existed=false but before is set")
	}
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":false,"before_mode":420,"after":"eQ==","after_mode":420}]}`)); err == nil {
		t.Fatal("expected an error: existed=false but before_mode is set")
	}
}

// Detector: a mode outside the permission-bit vocabulary (anything
// foundation/descriptor.go's own capture could never produce) is
// refused, not passed through to classifyOneFile's fs.FileMode.Perm()
// comparison unexamined.
func TestDecodeBundleRefusesModeOutsidePermissionBits(t *testing.T) {
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":false,"after":"eQ==","after_mode":4294967295}]}`)); err == nil {
		t.Fatal("expected an error: after_mode outside the permission-bit vocabulary")
	}
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":true,"before":"eA==","before_mode":4294967295,"after":"eQ==","after_mode":420}]}`)); err == nil {
		t.Fatal("expected an error: before_mode outside the permission-bit vocabulary")
	}
}

// Detector: apply's OWN encoding of an EXISTING EMPTY file round-trips
// through DecodeBundle without error. Reproduces codex review round 2,
// HIGH #1 exactly: a round 1 attempt at required-field presence
// tracking rejected this legitimate bundle, because Go's `,omitempty`
// on FileRecord.Before drops an empty-but-real before-image from the
// wire, which a presence-checking decoder cannot tell apart from a
// genuinely missing field.
func TestDecodeBundleRoundTripsApplysOwnExistingEmptyFileBundle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := apply(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "empty.go", After: []byte("package p\n"), Mode: 0o644},
	}, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Get(res.BundleDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeBundle(raw); err != nil {
		t.Fatalf("apply's own existing-empty-file bundle failed to round-trip: %v; raw=%s", err, raw)
	}
}

// Detector: apply's OWN encoding of a NEWLY CREATED EMPTY file (After
// == nil, which apply writes as a real zero-byte file) round-trips
// through DecodeBundle without error. Reproduces codex review round 2,
// HIGH #1's second repro: a nil []byte marshals as JSON null, which a
// *[]byte presence-checking decoder mistook for an absent required
// field.
func TestDecodeBundleRoundTripsApplysOwnNewEmptyFileBundle(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := apply(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "new.go", After: nil, Mode: 0o644},
	}, noopBindDurable)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Get(res.BundleDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeBundle(raw); err != nil {
		t.Fatalf("apply's own new-empty-file bundle failed to round-trip: %v; raw=%s", err, raw)
	}
}

// Detector: a legal NO-OP mutation (After byte-identical to Before)
// whose write never actually ran — Apply was cancelled before it got
// there — must classify NEVER_STARTED, never MID_CRASH. classifyOneFile
// must resolve the BEFORE/AFTER overlap conservatively as BEFORE.
// Reproduces codex review round 1, HIGH #2 through the real apply()
// path (not a hand-built Bundle): a pre-cancelled context, one mutation
// whose After equals the file's actual current content, seals a bundle
// before ctx.Err() is checked and performs zero writes.
func TestClassifyBundleNoOpMutationCancelledBeforeWriteIsNeverStarted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := apply(ctx, rootFd, store, []FileMutation{
		{BaseName: "a.go", After: []byte("package same\n"), Mode: 0o644},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected apply to refuse under a pre-cancelled context")
	}
	if res.Committed {
		t.Fatal("expected Committed = false")
	}

	raw, err := store.Get(res.BundleDigest)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		t.Fatal(err)
	}
	if state != TransactionNeverStarted {
		t.Fatalf("state = %v, want NEVER_STARTED (nothing was ever written)", state)
	}
}

// Detector: apply refuses a mutation whose target mode lacks the
// owner-read bit, BEFORE any write — a committed after-mode recovery
// could never reopen to hash would make a legitimate transaction
// permanently unclassifiable after a crash. Reproduces codex review
// round 3, HIGH #1: apply(mode=0000) used to commit successfully, and
// ClassifyBundle then failed with "permission denied" trying to read
// the file back.
func TestApplyRefusesUnreadableAfterMode(t *testing.T) {
	root := t.TempDir()
	rootFd, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := newTestStore(t)

	res, err := apply(context.Background(), rootFd, store, []FileMutation{
		{BaseName: "locked.go", After: []byte("package p\n"), Mode: 0},
	}, noopBindDurable)
	if err == nil {
		t.Fatal("expected apply to refuse an after-mode lacking the owner-read bit")
	}
	if res.Committed {
		t.Fatal("expected Committed = false")
	}
	if _, statErr := os.Stat(filepath.Join(root, "locked.go")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no write to have happened, got stat err = %v", statErr)
	}
}

// Detector: DecodeBundle refuses an after_mode (or, on a file apply
// captured, a before_mode) without the owner-read bit — the same
// "impossible producer narrative" reasoning as the mode-range check,
// now that apply itself never legitimately produces one.
func TestDecodeBundleRefusesModeWithoutOwnerRead(t *testing.T) {
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":false,"after":"eQ==","after_mode":0}]}`)); err == nil {
		t.Fatal("expected an error: after_mode lacks the owner-read bit")
	}
}

// Detector: before_mode WITHOUT the owner-read bit must be ACCEPTED —
// it is the captured mode of the PRE-EXISTING file, which may belong to
// a different UID and be readable only through group/other bits; apply's
// own CaptureFileBeneath already proved it was readable at capture time.
// codex review round 4, HIGH #1: an earlier version of this decoder
// wrongly applied the same owner-read requirement to before_mode,
// reproduced live against a real cross-UID mode-0044 file that apply had
// legitimately captured and replaced.
func TestDecodeBundleAcceptsBeforeModeWithoutOwnerReadBit(t *testing.T) {
	if _, err := DecodeBundle([]byte(`{"files":[{"rel_dir":"","base_name":"a.go","existed":true,"before":"eA==","before_mode":36,"after":"eQ==","after_mode":420}]}`)); err != nil {
		t.Fatalf("expected before_mode=0o044 (no owner-read, other-readable) to be accepted: %v", err)
	}
}

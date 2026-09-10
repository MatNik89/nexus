//go:build linux

package symedit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/coding/workspace"
	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
	"github.com/MatNik89/nexus/internal/security/redact"
	"golang.org/x/sys/unix"
)

func ctxT() context.Context { return context.Background() }

func realGoBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go binary on PATH: %v", err)
	}
	return path
}

func realGoplsBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("gopls")
	if err != nil {
		t.Skipf("no gopls binary on PATH: %v", err)
	}
	return path
}

func testBackend(t *testing.T) (*sandbox.Bwrap, sandbox.ProbeReport) {
	t.Helper()
	b := sandbox.NewBwrap()
	rep, err := b.Probe(ctxT())
	if err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	return b, rep
}

func testJournal(t *testing.T) *journal.Journal {
	t.Helper()
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.db"), "work", redact.None{},
		map[string]journal.PayloadValidator{"coding.run": nil})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func testStore(t *testing.T) *sealedstore.Store {
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

func noopBindDurable(string) error { return nil }

// Detector: Prepare + Apply perform a REAL, end-to-end, governed
// RenameSymbol — real gopls session, real sandbox, real on-disk files —
// touching both the declaration and the call site, and the workspace
// root on disk actually ends up with the renamed identifier in both
// files. This is the whole point of Slice 3: no mocks anywhere in this
// path.
func TestPrepareApplyEndToEndRename(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	store := testStore(t)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(
		"package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-symedit-rename-1", TargetID: "target-symedit-rename",
		RunID: "run-symedit-rename-1", ProfileID: "work",
	}

	plan, err := Prepare(ctxT(), rootFd, backend, report, grants, j, req)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if len(plan.Edits) != 1 {
		t.Fatalf("expected exactly 1 file edited, got %d: %+v", len(plan.Edits), plan.Edits)
	}
	if plan.Edits[0].RelPath != "main.go" {
		t.Fatalf("expected main.go edited, got %q", plan.Edits[0].RelPath)
	}
	if len(plan.Edits[0].Edits) != 2 {
		t.Fatalf("expected 2 edits (declaration + call site), got %d", len(plan.Edits[0].Edits))
	}
	if len(plan.Preimages["main.go"].Content) == 0 {
		t.Fatal("expected non-empty preimage content for main.go")
	}
	if plan.PlanDigest == "" {
		t.Fatal("expected a non-empty PlanDigest")
	}

	res, err := Apply(rootFd, store, plan, noopBindDurable)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}

	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := "package main\n\nfunc Bar() int { return 1 }\n\nfunc main() { _ = Bar() }\n"
	if string(got) != want {
		t.Fatalf("main.go after apply = %q, want %q", got, want)
	}
}

// Detector (invariant 3's own explicit requirement — "Apply re-hashes
// every preimage immediately before writing... any drift invalidates
// the prepared plan"): if a file changes on disk between Prepare and
// Apply, Apply must refuse the WHOLE operation before touching
// anything, even though the caller-supplied Plan looks internally
// consistent. Built by hand (no real gopls session needed) to keep this
// test fast and to isolate Apply's own drift-check logic.
func TestApplyRefusesWhenFileDriftedSincePrepare(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n\nfunc Foo() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := testStore(t)

	originalContent := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	dev, ino, err := workspace.StatBeneath(rootFd, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	edits := []FileEdit{
		{RelPath: "main.go", Version: 1, Edits: []TextEdit{
			{StartByte: 14, EndByte: 17, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{
		"main.go": {Content: originalContent, Dev: dev, Ino: ino, Mode: 0o644},
	}
	planDigest, err := computePlanDigest("Bar", "main.go", edits, preimages)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		NewName:           "Bar",
		OpenedFileRelPath: "main.go",
		Edits:             edits,
		Preimages:         preimages,
		PlanDigest:        planDigest,
	}

	// Drift: main.go changes on disk after the plan above was captured.
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n\nfunc Foo() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Apply(rootFd, store, plan, noopBindDurable)
	if err == nil {
		t.Fatal("expected Apply to refuse — main.go drifted since the plan was captured")
	}

	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc Foo() int { return 2 }\n" {
		t.Fatalf("main.go was modified despite the refusal: %q", got)
	}
}

// Detector: Apply refuses a plan with no edits at all rather than
// silently reporting success for nothing.
func TestApplyRefusesEmptyPlan(t *testing.T) {
	src := t.TempDir()
	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := testStore(t)

	if _, err := Apply(rootFd, store, Plan{}, noopBindDurable); err == nil {
		t.Fatal("expected an error for an empty plan")
	}
}

// Detector: ExtractTouchedRelPaths resolves each documentChanges entry's
// URI to its workspace-relative path, deduplicating repeats.
func TestExtractTouchedRelPathsHappyPath(t *testing.T) {
	raw := []byte(`{"documentChanges":[
		{"textDocument":{"uri":"file:///work/src/a.go","version":1},"edits":[]},
		{"textDocument":{"uri":"file:///work/src/sub/b.go","version":0},"edits":[]},
		{"textDocument":{"uri":"file:///work/src/a.go","version":1},"edits":[]}
	]}`)
	got, err := ExtractTouchedRelPaths(raw, "/work/src")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a.go": true, "sub/b.go": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, want)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected path %q in %v", p, got)
		}
	}
}

// Detector: ExtractTouchedRelPaths fails closed on a documentChanges
// entry whose URI escapes workspaceRoot — the same refusal
// ParseWorkspaceEdit's own resolveFileURI would apply.
func TestExtractTouchedRelPathsRefusesEscapingURI(t *testing.T) {
	raw := []byte(`{"documentChanges":[{"textDocument":{"uri":"file:///etc/passwd","version":0},"edits":[]}]}`)
	if _, err := ExtractTouchedRelPaths(raw, "/work/src"); err == nil {
		t.Fatal("expected an error: URI escapes workspaceRoot")
	}
}

// Detector: ExtractTouchedRelPaths fails closed on a response with no
// documentChanges at all.
func TestExtractTouchedRelPathsRefusesEmptyDocumentChanges(t *testing.T) {
	if _, err := ExtractTouchedRelPaths([]byte(`{}`), "/work/src"); err == nil {
		t.Fatal("expected an error: no documentChanges present")
	}
}

// Detector (code-review finding, codex): a tampered plan — any edit's
// NewText, or anything else about it, mutated after Prepare — must be
// refused by Apply before it does anything, since Apply's own
// PlanDigest recomputation will no longer match.
func TestApplyRefusesTamperedPlan(t *testing.T) {
	src := t.TempDir()
	originalContent := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	if err := os.WriteFile(filepath.Join(src, "main.go"), originalContent, 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := testStore(t)

	dev, ino, err := workspace.StatBeneath(rootFd, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	edits := []FileEdit{
		{RelPath: "main.go", Version: 1, Edits: []TextEdit{
			{StartByte: 14, EndByte: 17, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{
		"main.go": {Content: originalContent, Dev: dev, Ino: ino, Mode: 0o644},
	}
	planDigest, err := computePlanDigest("Bar", "main.go", edits, preimages)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		NewName: "Bar", OpenedFileRelPath: "main.go",
		Edits: edits, Preimages: preimages, PlanDigest: planDigest,
	}

	// Tamper: change the edit's NewText AFTER the plan digest was
	// computed, without recomputing it — simulates a caller mutating the
	// plan (or plan corruption across a serialize/deserialize boundary).
	plan.Edits[0].Edits[0].NewText = "Evil"

	_, err = Apply(rootFd, store, plan, noopBindDurable)
	if err == nil {
		t.Fatal("expected Apply to refuse — the plan was tampered with after Prepare")
	}
	got, readErr := os.ReadFile(filepath.Join(src, "main.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(originalContent) {
		t.Fatalf("main.go was modified despite the refusal: %q", got)
	}
}

// Detector (code-review finding, codex, round 2 HIGH): the round-1
// tamper detector only mutated Edits[0].Edits[0].NewText — codex flagged
// that this alone does NOT prove PlanDigest binds a preimage's Content
// or Mode, only whatever it hashes about Edits. Mutates
// Preimages["main.go"].Content directly (leaving Edits untouched) and
// confirms Apply still refuses.
func TestApplyRefusesPlanWithTamperedPreimageContent(t *testing.T) {
	src := t.TempDir()
	originalContent := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	if err := os.WriteFile(filepath.Join(src, "main.go"), originalContent, 0o644); err != nil {
		t.Fatal(err)
	}
	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := testStore(t)

	dev, ino, err := workspace.StatBeneath(rootFd, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	edits := []FileEdit{
		{RelPath: "main.go", Version: 1, Edits: []TextEdit{
			{StartByte: 14, EndByte: 17, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{
		"main.go": {Content: originalContent, Dev: dev, Ino: ino, Mode: 0o644},
	}
	planDigest, err := computePlanDigest("Bar", "main.go", edits, preimages)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		NewName: "Bar", OpenedFileRelPath: "main.go",
		Edits: edits, Preimages: preimages, PlanDigest: planDigest,
	}

	// Tamper: replace the preimage's Content AFTER the plan digest was
	// computed — Edits (including NewText) are left untouched. Same
	// length, so the byte-offset edit would still apply "cleanly" to
	// the wrong original text.
	tampered := preimages["main.go"]
	tampered.Content = []byte("package main\n\nfunc Zzz() int { return 1 }\n")
	plan.Preimages = map[string]Preimage{"main.go": tampered}

	_, err = Apply(rootFd, store, plan, noopBindDurable)
	if err == nil {
		t.Fatal("expected Apply to refuse — the preimage's Content was tampered with after Prepare")
	}
	got, readErr := os.ReadFile(filepath.Join(src, "main.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(originalContent) {
		t.Fatalf("main.go was modified despite the refusal: %q", got)
	}
}

// Detector (code-review finding, codex, round 2 HIGH): mutating a
// preimage's Mode after Prepare — content and identity left untouched —
// must also be caught by PlanDigest, not just Content.
func TestApplyRefusesPlanWithTamperedPreimageMode(t *testing.T) {
	src := t.TempDir()
	originalContent := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	if err := os.WriteFile(filepath.Join(src, "main.go"), originalContent, 0o600); err != nil {
		t.Fatal(err)
	}
	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)
	store := testStore(t)

	dev, ino, err := workspace.StatBeneath(rootFd, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	edits := []FileEdit{
		{RelPath: "main.go", Version: 1, Edits: []TextEdit{
			{StartByte: 14, EndByte: 17, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{
		"main.go": {Content: originalContent, Dev: dev, Ino: ino, Mode: 0o600},
	}
	planDigest, err := computePlanDigest("Bar", "main.go", edits, preimages)
	if err != nil {
		t.Fatal(err)
	}
	plan := Plan{
		NewName: "Bar", OpenedFileRelPath: "main.go",
		Edits: edits, Preimages: preimages, PlanDigest: planDigest,
	}

	// Tamper: change the recorded Mode after the plan digest was
	// computed — content and identity untouched.
	tampered := preimages["main.go"]
	tampered.Mode = 0o644
	plan.Preimages = map[string]Preimage{"main.go": tampered}

	_, err = Apply(rootFd, store, plan, noopBindDurable)
	if err == nil {
		t.Fatal("expected Apply to refuse — the preimage's Mode was tampered with after Prepare")
	}
	fi, statErr := os.Stat(filepath.Join(src, "main.go"))
	if statErr != nil {
		t.Fatal(statErr)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("main.go mode = %o, want untouched %o", fi.Mode().Perm(), 0o600)
	}
}

// Detector (code-review finding, codex, HIGH): if a file changes on
// disk after gopls has already computed its WorkspaceEdit but before
// Prepare finishes reading preimages, Prepare must refuse rather than
// resolve gopls's (line, character) positions against the NEW content —
// which could silently produce a WRONG byte offset instead of a clean
// failure. Real end-to-end (real gopls, real sandbox) so the whole-tree
// digest sandwich is proven against an actual RunGoplsRename call, not
// a hand-built response.
func TestPrepareRefusesWhenSourceChangesDuringGoplsAnalysis(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(
		"package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	orig := afterGoplsRename
	defer func() { afterGoplsRename = orig }()
	fired := false
	afterGoplsRename = func() {
		if fired {
			return
		}
		fired = true
		// A line added at the TOP of the file shifts every later line's
		// byte offset — if Prepare trusted this content against gopls's
		// original positions, it would resolve to the wrong bytes.
		if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(
			"// unexpected concurrent edit\npackage main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-drift-1", TargetID: "target-drift",
		RunID: "run-drift-1", ProfileID: "work",
	}

	_, err = Prepare(ctxT(), rootFd, backend, report, grants, j, req)
	if err == nil {
		t.Fatal("expected Prepare to refuse — main.go changed during gopls analysis")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}
	// Caught by the per-file digest check now (added in a later round,
	// attributing the drift to the specific file) rather than the
	// coarser whole-tree digestAfter comparison this test originally
	// exercised — a strictly more precise outcome, same safety property.
	if !strings.Contains(err.Error(), "changed between the pre-analysis digest and this read") {
		t.Fatalf("expected the drift-specific error, got: %v", err)
	}
}

// Detector (code-review finding, codex): Prepare must refuse when rootFd
// does not identify req.SourceDir — an honest caller's mismatched-pair
// bug must be caught, not silently produce a plan computed against one
// tree but meant to be read against another.
func TestPrepareRefusesMismatchedRootFdAndSourceDir(t *testing.T) {
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	real := t.TempDir()
	other := t.TempDir()
	rootFd, err := workspace.OpenRoot(other) // deliberately the WRONG directory
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	req := runner.RenameRequest{
		SourceDir: real, FileRelPath: "main.go",
		Line: 0, Character: 0, NewName: "Bar",
		GoBinary: "go", GoplsBinary: "gopls", Timeout: 5 * time.Second,
		OperationID: "op-mismatch-1", TargetID: "target-mismatch",
		RunID: "run-mismatch-1", ProfileID: "work",
	}

	_, err = Prepare(ctxT(), rootFd, backend, report, grants, j, req)
	if err == nil {
		t.Fatal("expected Prepare to refuse — rootFd and req.SourceDir name different directories")
	}
	if !strings.Contains(err.Error(), "do not identify the same directory") {
		t.Fatalf("expected the identity-mismatch error specifically, got a different refusal: %v", err)
	}
}

// Detector (code-review finding, codex, round 2 HIGH): the FIRST
// verifyRootIdentity call only proves rootFd and req.SourceDir's
// pathname matched at Prepare's own start — it says nothing about a
// swap DURING the RunGoplsRename call. Uses the afterGoplsRename seam to
// rename req.SourceDir's own pathname away and put a DIFFERENT directory
// in its place, exactly in the window that seam covers, and confirms
// Prepare's second (post-gopls) identity re-check catches it.
func TestPrepareRefusesSourceDirPathnameSwappedDuringGoplsAnalysis(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(
		"package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	movedAway := src + "-moved"
	orig := afterGoplsRename
	defer func() { afterGoplsRename = orig }()
	fired := false
	afterGoplsRename = func() {
		if fired {
			return
		}
		fired = true
		if err := os.Rename(src, movedAway); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(src, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	defer os.RemoveAll(movedAway)

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-swap-1", TargetID: "target-swap",
		RunID: "run-swap-1", ProfileID: "work",
	}

	_, err = Prepare(ctxT(), rootFd, backend, report, grants, j, req)
	if err == nil {
		t.Fatal("expected Prepare to refuse — req.SourceDir's pathname was swapped to a different directory during gopls analysis")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}
	if !strings.Contains(err.Error(), "do not identify the same directory") {
		t.Fatalf("expected the identity-mismatch error specifically, got: %v", err)
	}
}

// Detector (code-review finding, codex, round 3): the PREVIOUS pathname-
// swap test's replacement directory had DIFFERENT (empty) content, so
// the whole-tree digest sandwich would have caught the swap too — that
// test could not isolate whether the identity re-check itself was
// behaviorally necessary. This test's replacement directory is a BYTE-
// IDENTICAL copy of the original, so digestBefore and digestAfter (and
// res.SnapshotDigest) all match regardless of the swap — ONLY the
// identity re-check can catch it, cleanly isolating its necessity.
func TestPrepareRefusesSourceDirSwappedForByteIdenticalDirectory(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	goModContent := []byte("module example.com/tiny\n\ngo 1.21\n")
	mainGoContent := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), goModContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), mainGoContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// A SEPARATE directory with byte-identical content but a genuinely
	// different identity (different inode).
	identicalReplacement := t.TempDir()
	if err := os.WriteFile(filepath.Join(identicalReplacement, "go.mod"), goModContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identicalReplacement, "main.go"), mainGoContent, 0o644); err != nil {
		t.Fatal(err)
	}

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	movedAway := src + "-moved"
	orig := afterGoplsRename
	defer func() { afterGoplsRename = orig }()
	fired := false
	afterGoplsRename = func() {
		if fired {
			return
		}
		fired = true
		if err := os.Rename(src, movedAway); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(identicalReplacement, src); err != nil {
			t.Fatal(err)
		}
	}
	defer os.RemoveAll(movedAway)

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-swap-identical-1", TargetID: "target-swap-identical",
		RunID: "run-swap-identical-1", ProfileID: "work",
	}

	_, err = Prepare(ctxT(), rootFd, backend, report, grants, j, req)
	if err == nil {
		t.Fatal("expected Prepare to refuse — req.SourceDir was swapped for a different (byte-identical) directory during gopls analysis")
	}
	if !fired {
		t.Fatal("test setup bug: the injection hook never fired")
	}
	if !strings.Contains(err.Error(), "do not identify the same directory") {
		t.Fatalf("expected the identity-mismatch error specifically (the only check that CAN catch a byte-identical swap), got: %v", err)
	}
}

// Detector (code-review finding, codex, round 3 HIGH): a two-point
// digestBefore/digestAfter sandwich bracketing the WHOLE RunGoplsRename
// call cannot see an ABA that changes and reverts entirely WITHIN that
// call — before gopls's own internal snapshot copy, and reverted before
// Prepare's own post-call checks. Uses beforeGoplsRename (fires right
// before the call) to change main.go, and afterGoplsRename (fires right
// after) to revert it — by the time Prepare's own digestAfter runs,
// everything looks identical to digestBefore again. Only comparing
// res.SnapshotDigest (the digest of what gopls ACTUALLY analyzed)
// against digestBefore can catch this.
func TestPrepareRefusesWhenSnapshotDigestDoesNotMatchPreAnalysisDigest(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	originalContent := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	duringGoplsContent := []byte("package main\n\nfunc Foo() int { return 999 }\n\nfunc main() { _ = Foo() }\n")

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), originalContent, 0o644); err != nil {
		t.Fatal(err)
	}

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	origBefore, origAfter := beforeGoplsRename, afterGoplsRename
	defer func() { beforeGoplsRename, afterGoplsRename = origBefore, origAfter }()
	beforeGoplsRename = func() {
		if err := os.WriteFile(filepath.Join(src, "main.go"), duringGoplsContent, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reverted := false
	afterGoplsRename = func() {
		if reverted {
			return
		}
		reverted = true
		if err := os.WriteFile(filepath.Join(src, "main.go"), originalContent, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-snapshot-aba-1", TargetID: "target-snapshot-aba",
		RunID: "run-snapshot-aba-1", ProfileID: "work",
	}

	_, err = Prepare(ctxT(), rootFd, backend, report, grants, j, req)
	if err == nil {
		t.Fatal("expected Prepare to refuse — main.go changed and reverted entirely within the gopls call, invisible to the endpoint digest sandwich alone")
	}
	if !reverted {
		t.Fatal("test setup bug: the revert hook never fired")
	}
	if !strings.Contains(err.Error(), "snapshot digest") {
		t.Fatalf("expected the snapshot-digest-specific error, got: %v", err)
	}
}

func TestSplitRelPath(t *testing.T) {
	cases := []struct{ in, wantDir, wantBase string }{
		{"a.go", "", "a.go"},
		{"sub/a.go", "sub", "a.go"},
		{"a/b/c.go", "a/b", "c.go"},
	}
	for _, c := range cases {
		dir, base := splitRelPath(c.in)
		if dir != c.wantDir || base != c.wantBase {
			t.Fatalf("splitRelPath(%q) = (%q,%q), want (%q,%q)", c.in, dir, base, c.wantDir, c.wantBase)
		}
	}
}

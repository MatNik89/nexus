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
	"github.com/MatNik89/nexus/internal/kernel/contracts"
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

func testDurableJournal(t *testing.T) *journal.Journal {
	t.Helper()
	events := s7.Events()
	for n, v := range workspace.Events() {
		events[n] = v
	}
	events["coding.run"] = nil
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.db"), "work", redact.None{},
		events, s7.NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func testDurableGrants(t *testing.T, j *journal.Journal) *s7.Authority {
	t.Helper()
	a, err := s7.New(j, time.Now, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Detector: Prepare + ApplyGoverned perform a REAL, end-to-end, governed
// RenameSymbol — real gopls session, real sandbox, real on-disk files,
// real S7 durable grant lifecycle — touching both the declaration and
// the call site, and the workspace root on disk actually ends up with
// the renamed identifier in both files. This is the whole point of
// Slice 3: no mocks anywhere in this path (folds what used to be a
// separate TestPrepareApplyEndToEndRename against the non-governed
// `apply` — removed once `apply`/workspace.Apply stopped being
// reachable outside this package's own tests, codex round 4 HIGH #1).
func TestApplyGovernedEndToEndRenameWiresRealS7Grant(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
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
		OperationID: "op-governed-rename-1", TargetID: "target-governed-rename",
		RunID: "run-governed-rename-1", ProfileID: "work",
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

	res, err := ApplyGoverned(ctxT(), rootFd, store, plan, grants, j, "run-governed-rename-1")
	if err != nil {
		t.Fatalf("ApplyGoverned failed: %v", err)
	}
	if !res.Committed {
		t.Fatal("expected Committed = true")
	}

	var rootSt unix.Stat_t
	if err := unix.Fstat(rootFd, &rootSt); err != nil {
		t.Fatal(err)
	}
	op := workspace.ApplyOperationID("work", uint64(rootSt.Dev), rootSt.Ino, plan.PlanDigest)
	state, ok := grants.State(op)
	if !ok || state != contracts.AttemptSucceeded {
		t.Fatalf("apply operation state = %v (ok=%v), want SUCCEEDED", state, ok)
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
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)

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

	_, err = ApplyGoverned(ctxT(), rootFd, store, plan, grants, j, "run-drift-1")
	if err == nil {
		t.Fatal("expected ApplyGoverned to refuse — main.go drifted since the plan was captured")
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
	if _, err := planMutations(Plan{}); err == nil {
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

	if _, err := planMutations(plan); err == nil {
		t.Fatal("expected planMutations to refuse — the plan was tampered with after Prepare")
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

	if _, err := planMutations(plan); err == nil {
		t.Fatal("expected planMutations to refuse — the preimage's Content was tampered with after Prepare")
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

	if _, err := planMutations(plan); err == nil {
		t.Fatal("expected planMutations to refuse — the preimage's Mode was tampered with after Prepare")
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

// Detector: a well-formed edit (a straightforward identifier rename)
// stages valid Go — verifyStagedSyntaxIsValid must accept it.
func TestVerifyStagedSyntaxIsValidAcceptsWellFormedEdit(t *testing.T) {
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			{StartByte: 19, EndByte: 22, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content}}
	if err := verifyStagedSyntaxIsValid(edits, preimages); err != nil {
		t.Fatalf("expected a well-formed edit to pass: %v", err)
	}
}

// Detector (invariant 3's own explicit requirement): an edit whose byte
// offsets are wrong enough to leave the staged result syntactically
// invalid Go must be refused, not silently offered as a Plan.
func TestVerifyStagedSyntaxIsValidRefusesInvalidSyntax(t *testing.T) {
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			// Deletes past the identifier into the surrounding syntax,
			// leaving invalid Go.
			{StartByte: 9, EndByte: 25, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content}}
	if err := verifyStagedSyntaxIsValid(edits, preimages); err == nil {
		t.Fatal("expected an error: the staged result is not valid Go")
	}
}

// Detector (code-review finding, codex round 1 HIGH #1 — the earlier
// version of this check also required gofmt-byte-identity and wrongly
// refused BOTH cases below; both are now accepted):
//  1. A CRLF file: gofmt normalizes CRLF to LF, but this codebase has an
//     explicit, already-tested contract (edit_test.go's own
//     TestParseWorkspaceEditPreservesCRLF) that CRLF must survive
//     byte-exact — a gofmt-identity check would reject every legitimate
//     CRLF rename.
//  2. A file that was already NOT gofmt-clean BEFORE this edit: gofmt
//     would reformat the WHOLE file, including parts this edit never
//     touched — that is not a defect IN the edit.
//
// Both are syntactically valid Go, so verifyStagedSyntaxIsValid (syntax
// only, never gofmt-identity) must accept both.
func TestVerifyStagedSyntaxIsValidAcceptsCRLFAndPreExistingUnformattedFiles(t *testing.T) {
	crlf := []byte("package p\r\n\r\nfunc Old() {}\r\n")
	crlfEdits := []FileEdit{
		{RelPath: "f.go", Edits: []TextEdit{{StartByte: 18, EndByte: 21, NewText: "New"}}},
	}
	if err := verifyStagedSyntaxIsValid(crlfEdits, map[string]Preimage{"f.go": {Content: crlf}}); err != nil {
		t.Fatalf("expected a CRLF rename to pass: %v", err)
	}

	// Pre-existing extra blank lines gofmt would collapse — valid Go,
	// never gofmt-clean, unrelated to the edit below.
	unformatted := []byte("package main\n\n\n\nfunc Foo() int { return 1 }\n")
	unformattedEdits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{{StartByte: 21, EndByte: 24, NewText: "Bar"}}},
	}
	if err := verifyStagedSyntaxIsValid(unformattedEdits, map[string]Preimage{"main.go": {Content: unformatted}}); err != nil {
		t.Fatalf("expected a rename on a pre-existing-unformatted file to pass: %v", err)
	}
}

// Detector: a touched file with no recorded preimage must be refused —
// verifyStagedSyntaxIsValid must never silently skip a file it cannot
// stage.
func TestVerifyStagedSyntaxIsValidRefusesMissingPreimage(t *testing.T) {
	edits := []FileEdit{
		{RelPath: "missing.go", Edits: []TextEdit{{StartByte: 0, EndByte: 0, NewText: "x"}}},
	}
	if err := verifyStagedSyntaxIsValid(edits, map[string]Preimage{}); err == nil {
		t.Fatal("expected an error: missing.go has no preimage")
	}
}

// Detector (code-review finding, codex round 2 HIGH #1): go/format.Source
// accepts a bare list of declarations, or a bare list of statements, as
// "valid" — NOT only a complete source file. A byte-offset bug severe
// enough to delete the package clause entirely would previously have
// been silently accepted. verifyStagedSyntaxIsValid uses go/parser.ParseFile
// instead, which has no such leniency: it always requires a complete file.
func TestVerifyStagedSyntaxIsValidRefusesDeclarationOnlyFragment(t *testing.T) {
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	edits := []FileEdit{
		// Replaces the ENTIRE file with a bare declaration — no package
		// clause. format.Source accepts this (it's a valid "declaration
		// list"); a real .go file parser must not.
		{RelPath: "main.go", Edits: []TextEdit{{StartByte: 0, EndByte: len(content), NewText: "func Bar() {}\n"}}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content}}
	if err := verifyStagedSyntaxIsValid(edits, preimages); err == nil {
		t.Fatal("expected an error: the staged result has no package clause — it is a declaration fragment, not a complete Go file")
	}
}

// Detector: the SAME leniency gap, for a bare statement list (also
// accepted by format.Source, also not a valid complete .go file).
func TestVerifyStagedSyntaxIsValidRefusesStatementOnlyFragment(t *testing.T) {
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n")
	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{{StartByte: 0, EndByte: len(content), NewText: "x := 1\nreturn x\n"}}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content}}
	if err := verifyStagedSyntaxIsValid(edits, preimages); err == nil {
		t.Fatal("expected an error: the staged result has no package clause — it is a statement fragment, not a complete Go file")
	}
}

// Detector (code-review finding, codex round 1 HIGH #2): the wiring
// itself — that Prepare actually CALLS verifyStagedSyntaxIsValid on the
// real edits it just parsed from a real gopls response — had no
// RED-capable detector; the 4 unit tests above only exercise the pure
// function directly. Ablating the production call in Prepare left the
// full real-gopls suite green. Uses the beforeStagedSyntaxCheck seam
// (mirrors this file's own beforeGoplsRename/afterGoplsRename pattern)
// to inject an edit that stages invalid Go into the REAL edits Prepare
// is about to check, through a REAL end-to-end gopls session — if
// Prepare's own call to verifyStagedSyntaxIsValid is ever removed or
// disconnected, this test turns RED (Prepare would return a plan
// instead of refusing).
func TestPrepareRefusesWhenStagedSyntaxIsInvalid(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	mainGoContent := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), mainGoContent, 0o644); err != nil {
		t.Fatal(err)
	}

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	orig := beforeStagedSyntaxCheck
	defer func() { beforeStagedSyntaxCheck = orig }()
	beforeStagedSyntaxCheck = func(edits []FileEdit) []FileEdit {
		for i := range edits {
			if edits[i].RelPath == "main.go" {
				// REPLACES (not appends to — avoiding an overlap conflict
				// with gopls's own real edits) main.go's whole edit list
				// with a single, IN-BOUNDS edit spanning the entire file,
				// staging a bare declaration fragment: valid input to
				// go/format.Source (it accepts a declaration list), but not
				// a complete .go file (no package clause) — exactly the
				// codex round-2 HIGH #1 leniency gap, exercised through the
				// real parser this wiring test proves is actually called,
				// not just through the unit-level fragment tests above.
				edits[i].Edits = []TextEdit{{StartByte: 0, EndByte: len(mainGoContent), NewText: "func Bar() {}\n"}}
			}
		}
		return edits
	}

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-badsyntax-1", TargetID: "target-badsyntax",
		RunID: "run-badsyntax-1", ProfileID: "work",
	}
	if _, err := Prepare(ctxT(), rootFd, backend, report, grants, j, req); err == nil {
		t.Fatal("expected Prepare to refuse: the injected edit stages invalid Go")
	}
}

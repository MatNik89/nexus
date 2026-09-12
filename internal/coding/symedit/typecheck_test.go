//go:build linux

package symedit

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/coding/workspace"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"golang.org/x/sys/unix"
)

func writeTinyModule(t *testing.T, dir string, mainGo []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), mainGo, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Detector: a well-formed edit against a real module must type-check
// cleanly via a real compile-only check against the staged (not live) tree.
func TestVerifyStagedTypeChecksAcceptsWellFormedEdit(t *testing.T) {
	goBin := realGoBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	writeTinyModule(t, src, content)
	digest := mustDigestTree(t, src)

	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			{StartByte: 19, EndByte: 22, NewText: "Bar"},
			{StartByte: 61, EndByte: 64, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content, Mode: 0o644}}

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, goBin, digest, edits, preimages,
		"op-tc-accept", "target-tc-accept", "run-tc-accept", "work"); err != nil {
		t.Fatalf("expected a well-formed edit to type-check: %v", err)
	}
}

// Detector (round 2 HIGH finding, codex + kilo, BOTH independently live-
// reproduced the identical bug): the round-1 fix (`go vet ./...`) closed
// the test-file false-NEGATIVE but introduced a false-POSITIVE — go vet
// runs its full default analyzer set (printf, buildtag, etc.), so an
// UNRELATED, pre-existing analyzer finding anywhere in the staged tree
// refused an otherwise perfectly valid rename. `go test -run=^$` alone
// does not help either (empirically verified: it runs the same limited
// vet subset internally and refuses on the identical finding) — only
// `-vet=off` actually disables analysis while still compiling everything,
// tests included.
func TestVerifyStagedTypeChecksAcceptsEditDespiteUnrelatedPreExistingVetFinding(t *testing.T) {
	goBin := realGoBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	writeTinyModule(t, src, content)
	// A second, untouched file with a real, pre-existing printf vet
	// finding — entirely unrelated to the rename below.
	warningOnly := []byte("package main\n\nimport \"fmt\"\n\nfunc warningOnly() { fmt.Printf(\"%d\", \"not-an-int\") }\n")
	if err := os.WriteFile(filepath.Join(src, "warning.go"), warningOnly, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := mustDigestTree(t, src)

	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			{StartByte: 19, EndByte: 22, NewText: "Bar"},
			{StartByte: 61, EndByte: 64, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content, Mode: 0o644}}

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, goBin, digest, edits, preimages,
		"op-tc-vet-warning", "target-tc-vet-warning", "run-tc-vet-warning", "work"); err != nil {
		t.Fatalf("FALSE REFUSAL: staged program type-checks; only an unrelated pre-existing vet finding exists: %v", err)
	}
}

// Detector (round 3 HIGH finding, codex, live-reproduced): the round-2
// fix (`go test -vet=off -run=^$ -count=1 ./...`) disables analysis
// correctly, but `-run=^$` only suppresses ORDINARY test functions — the
// generated test binary still runs, so package init() and any TestMain
// still EXECUTE. A compiling TestMain with any runtime side effect (here,
// os.Exit(7)) refused an otherwise fully valid rename for reasons having
// nothing to do with type-correctness. `-c -o /dev/null` compiles the
// test binary but never runs it, closing this for good.
func TestVerifyStagedTypeChecksAcceptsEditDespiteExecutableTestMain(t *testing.T) {
	goBin := realGoBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	writeTinyModule(t, src, content)
	// A TestMain that, if ever actually executed, exits nonzero —
	// proving the check never runs the compiled binary.
	testMain := []byte("package main\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { os.Exit(7) }\n")
	if err := os.WriteFile(filepath.Join(src, "main_test.go"), testMain, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := mustDigestTree(t, src)

	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			{StartByte: 19, EndByte: 22, NewText: "Bar"},
			{StartByte: 61, EndByte: 64, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content, Mode: 0o644}}

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, goBin, digest, edits, preimages,
		"op-tc-testmain", "target-tc-testmain", "run-tc-testmain", "work"); err != nil {
		t.Fatalf("FALSE REFUSAL: every source and test file type-checks; only a TestMain runtime exit would have run: %v", err)
	}
}

func mustDigestTree(t *testing.T, dir string) string {
	t.Helper()
	digest, err := runner.DigestTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// Detector: an edit that is syntactically valid Go but references an
// undefined identifier must be refused — this is exactly the class of
// defect verifyStagedSyntaxIsValid (parse-only) cannot see, and the
// whole reason this half of invariant 3's "format+type-check" exists.
func TestVerifyStagedTypeChecksRefusesUndefinedReference(t *testing.T) {
	goBin := realGoBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	writeTinyModule(t, src, content)
	digest := mustDigestTree(t, src)

	// Stages a call to an identifier that is never declared anywhere —
	// valid Go syntax (a plain call expression), invalid Go semantics.
	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			{StartByte: 61, EndByte: 64, NewText: "TotallyUndefinedIdentifier"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content, Mode: 0o644}}

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, goBin, digest, edits, preimages,
		"op-tc-undefined", "target-tc-undefined", "run-tc-undefined", "work"); err == nil {
		t.Fatal("expected an error: the staged result references an undefined identifier")
	}
}

// Detector (round 1 HIGH finding, codex + kilo, BOTH independently live-
// reproduced the identical bug): `go build ./...` never compiles
// `_test.go` files at all — a rename touching a _test.go file (an
// entirely ordinary case: renaming a symbol that tests reference) could
// stage an undefined reference there and the OLD `go build`-based check
// returned nil anyway (FALSE GREEN). Eventually fixed (round 4) by
// `go test -vet=off -c -o /dev/null ./...`, which compiles every file in
// the package — tests included — without running any analyzer or
// executing any of the compiled code.
func TestVerifyStagedTypeChecksRefusesUndefinedReferenceInTestFile(t *testing.T) {
	goBin := realGoBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	mainContent := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() {}\n")
	writeTinyModule(t, src, mainContent)
	testContent := []byte("package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) { _ = Foo() }\n")
	if err := os.WriteFile(filepath.Join(src, "main_test.go"), testContent, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := mustDigestTree(t, src)

	// Stages an edit ONLY in main_test.go, replacing the reference to
	// Foo with an undefined identifier — valid syntax, invalid semantics,
	// confined entirely to the test file.
	edits := []FileEdit{
		{RelPath: "main_test.go", Edits: []TextEdit{
			{StartByte: 65, EndByte: 68, NewText: "TotallyUndefinedIdentifier"},
		}},
	}
	preimages := map[string]Preimage{"main_test.go": {Content: testContent, Mode: 0o644}}

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, goBin, digest, edits, preimages,
		"op-tc-testfile", "target-tc-testfile", "run-tc-testfile", "work"); err == nil {
		t.Fatal("expected an error: the staged _test.go file references an undefined identifier")
	}
}

// Detector (round 1 HIGH finding, codex, live-reproduced end-to-end
// through a real gopls session + sandbox + S7 + Prepare + ApplyGoverned):
// this function's own CreateSnapshot(sourceDir) read is a NEW read of
// the tree Prepare's own digestBefore/digestAfter sandwich does not
// cover on its own — sourceDir could be swapped for a different, clean
// tree between Prepare's last digest check and this function's own
// snapshot, making a broken ORIGINAL workspace look like it type-checks
// while ApplyGoverned goes on to commit the edit to the original,
// descriptor-pinned (still broken) tree. wantDigest must match this
// function's own fresh snapshot digest, or it refuses.
func TestVerifyStagedTypeChecksRefusesDigestMismatch(t *testing.T) {
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	src := t.TempDir()
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	writeTinyModule(t, src, content)

	// A well-formed edit (declaration AND call site both renamed,
	// mirroring TestVerifyStagedTypeChecksAcceptsWellFormedEdit exactly)
	// — isolating the digest mismatch as the ONLY reason this must be
	// refused, not a coincidentally broken edit.
	edits := []FileEdit{
		{RelPath: "main.go", Edits: []TextEdit{
			{StartByte: 19, EndByte: 22, NewText: "Bar"},
			{StartByte: 61, EndByte: 64, NewText: "Bar"},
		}},
	}
	preimages := map[string]Preimage{"main.go": {Content: content, Mode: 0o644}}

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, realGoBinary(t), "not-the-real-digest", edits, preimages,
		"op-tc-digest-mismatch", "target-tc-digest-mismatch", "run-tc-digest-mismatch", "work"); err == nil {
		t.Fatal("expected an error: wantDigest does not match this function's own fresh snapshot of sourceDir")
	}
}

// Detector: a touched file with no recorded preimage must be refused —
// mirrors verifyStagedSyntaxIsValid's own identical detector.
func TestVerifyStagedTypeChecksRefusesMissingPreimage(t *testing.T) {
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	src := t.TempDir()
	writeTinyModule(t, src, []byte("package main\n\nfunc main() {}\n"))
	digest := mustDigestTree(t, src)

	edits := []FileEdit{
		{RelPath: "missing.go", Edits: []TextEdit{{StartByte: 0, EndByte: 0, NewText: "x"}}},
	}
	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, realGoBinary(t), digest, edits, map[string]Preimage{},
		"op-tc-missing", "target-tc-missing", "run-tc-missing", "work"); err == nil {
		t.Fatal("expected an error: missing.go has no preimage")
	}
}

// Detector: OperationID/TargetID/RunID/ProfileID/GoBinary are all
// required, fail closed — same contract as runner.Run/RenameRequest
// itself, checked BEFORE any snapshot or subprocess work.
func TestVerifyStagedTypeChecksRefusesMissingIdentity(t *testing.T) {
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	src := t.TempDir()

	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, "go", "digest", nil, nil,
		"", "target", "run", "work"); err == nil {
		t.Fatal("expected an error: OperationID is empty")
	}
	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, "", "digest", nil, nil,
		"op", "target", "run", "work"); err == nil {
		t.Fatal("expected an error: GoBinary is empty")
	}
	if err := verifyStagedTypeChecks(ctxT(), backend, report, grants, j, src, "go", "", nil, nil,
		"op", "target", "run", "work"); err == nil {
		t.Fatal("expected an error: wantDigest is empty")
	}
}

// Detector: snapshotFilePath must refuse a relative path that escapes
// the snapshot root — defense-in-depth alongside ParseWorkspaceEdit's
// own RelPath validation, exercised directly (never trust a single
// validation point twice removed from where the path is actually used).
func TestSnapshotFilePathRefusesEscapingRelPath(t *testing.T) {
	for _, bad := range []string{"", "/etc/passwd", "../outside.go", "a/../../outside.go"} {
		if _, err := snapshotFilePath("/snap", bad); err == nil {
			t.Fatalf("expected %q to be refused as escaping the snapshot root", bad)
		}
	}
	got, err := snapshotFilePath("/snap", "a/b.go")
	if err != nil || got != "/snap/a/b.go" {
		t.Fatalf("expected a clean relative path to resolve normally, got %q err=%v", got, err)
	}
}

// Detector: Prepare must actually WIRE verifyStagedTypeChecks into a
// real, end-to-end, gopls-driven Prepare call — not just expose it as a
// standalone, never-called function (the same class of wiring gap
// codex round 1 HIGH #2 found for verifyStagedSyntaxIsValid). Uses
// beforeStagedBuild (fires AFTER staging, right before the governed
// build) to rewrite the staged snapshot's main.go into something
// syntactically valid but semantically broken — the syntax check above
// it in Prepare's own pipeline would NOT catch this, only the real
// `go build` this test proves actually runs.
func TestPrepareRefusesWhenStagedTypeCheckFails(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	mainGoContent := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	src := t.TempDir()
	writeTinyModule(t, src, mainGoContent)

	rootFd, err := workspace.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rootFd)

	orig := beforeStagedBuild
	defer func() { beforeStagedBuild = orig }()
	beforeStagedBuild = func(snapDir string) {
		broken := []byte("package main\n\nfunc Bar() int { return 1 }\n\nfunc main() { _ = TotallyUndefinedIdentifier }\n")
		if err := os.WriteFile(filepath.Join(snapDir, "main.go"), broken, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	req := runner.RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-badtype-1", TargetID: "target-badtype",
		TypeCheckOperationID: "op-badtype-1-tc", TypeCheckTargetID: "target-badtype-tc",
		RunID: "run-badtype-1", ProfileID: "work",
	}
	if _, err := Prepare(ctxT(), rootFd, backend, report, grants, j, req); err == nil {
		t.Fatal("expected Prepare to refuse: the staged result does not type-check")
	}
}

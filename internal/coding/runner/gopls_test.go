//go:build linux

package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/s7"
)

func realGoplsBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("gopls")
	if err != nil {
		t.Skipf("no gopls binary on PATH: %v", err)
	}
	return path
}

// Detector: RunGoplsRename drives a real gopls stdio session end to end
// through the real bwrap sandbox and returns the correct structured
// WorkspaceEdit for a real rename (Foo -> Bar), touching both the
// declaration and the call site — the plan's decisive reason for gopls
// stdio over the gopls CLI (which emits unified diffs, not structured
// JSON).
func TestRunGoplsRenameEndToEnd(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	b, rep := testBackend(t)
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

	result, err := RunGoplsRename(ctxT(), b, rep, grants, j, RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-test-rename-1", TargetID: "target-test-rename",
		RunID: "run-test-rename-1", ProfileID: "work",
	})
	if err != nil {
		t.Fatalf("RunGoplsRename failed: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("gopls returned an LSP error: %+v", result.Error)
	}
	if len(result.Result) == 0 {
		t.Fatalf("expected a non-empty WorkspaceEdit result, got none: %+v", result)
	}
	if result.SnapshotDigest == "" || result.ToolchainDigest == "" || result.PolicyHash == "" {
		t.Fatalf("missing digests/policy hash: %+v", result)
	}

	var edit struct {
		DocumentChanges []struct {
			Edits []struct {
				NewText string `json:"newText"`
			} `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := json.Unmarshal(result.Result, &edit); err != nil {
		t.Fatalf("unmarshal WorkspaceEdit: %v\nraw: %s", err, result.Result)
	}
	if len(edit.DocumentChanges) != 1 || len(edit.DocumentChanges[0].Edits) != 2 {
		t.Fatalf("expected 1 file with 2 edits (declaration + call site), got: %s", result.Result)
	}
	for _, e := range edit.DocumentChanges[0].Edits {
		if e.NewText != "Bar" {
			t.Fatalf("expected every edit's newText to be %q, got %q", "Bar", e.NewText)
		}
	}
}

// Detector: renaming a position with no identifier is an LSP-level
// error — DATA the governed session completes normally with, not an
// attempt failure (mirrors TestRunReportsTestFailureAsDataNotAttemptFailure's
// data-vs-failure split for go test).
func TestRunGoplsRenameReportsLSPErrorAsData(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	tinyModule(t, src)

	result, err := RunGoplsRename(ctxT(), b, rep, grants, j, RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 0, Character: 0, NewName: "Whatever", // inside "package main" — no renameable identifier
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-test-rename-2", TargetID: "target-test-rename-err",
		RunID: "run-test-rename-2", ProfileID: "work",
	})
	if err != nil {
		t.Fatalf("RunGoplsRename itself returned an error for a normal LSP-level refusal: %v", err)
	}
	if result.Error == nil {
		t.Fatalf("expected an LSP error for an unrenameable position, got a result: %s", result.Result)
	}
}

// Detector (fail-closed identifiers): missing OperationID/TargetID/RunID/
// ProfileID is refused before any snapshot/sandbox work begins.
func TestRunGoplsRenameRequiresIdentifiers(t *testing.T) {
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	src := t.TempDir()
	tinyModule(t, src)

	_, err := RunGoplsRename(ctxT(), b, rep, grants, j, RenameRequest{
		SourceDir: src, FileRelPath: "main.go", NewName: "X",
		GoBinary: "go", GoplsBinary: "gopls",
	})
	if err == nil {
		t.Fatal("RunGoplsRename accepted a request with no OperationID/TargetID/RunID/ProfileID")
	}
}

// Detector (code-review finding, codex): a filename containing '#' or a
// space must not corrupt the URI gopls receives — raw string
// concatenation would (a '#' starts a URI fragment, a space is illegal
// unescaped). Uses a real filename with both, through the real sandbox.
func TestRunGoplsRenameHandlesSpecialCharacterFilename(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const weirdName = "weird #1 file.go"
	if err := os.WriteFile(filepath.Join(src, weirdName), []byte(
		"package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := RunGoplsRename(ctxT(), b, rep, grants, j, RenameRequest{
		SourceDir: src, FileRelPath: weirdName,
		Line: 2, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: 60 * time.Second,
		OperationID: "op-test-rename-weird", TargetID: "target-test-rename-weird",
		RunID: "run-test-rename-weird", ProfileID: "work",
	})
	if err != nil {
		t.Fatalf("RunGoplsRename failed on a filename with special characters: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("gopls returned an LSP error for a special-character filename: %+v", result.Error)
	}
	if len(result.Result) == 0 {
		t.Fatalf("expected a non-empty WorkspaceEdit result, got none: %+v", result)
	}
}

// Detector: FileRelPath validation is fail-closed on absolute paths and
// '..' escapes, using the SAME normalization (path.Clean) the actual URI
// construction relies on — not a weaker substring check.
func TestRunGoplsRenameRejectsUnsafeFileRelPath(t *testing.T) {
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	src := t.TempDir()
	tinyModule(t, src)

	cases := []string{"", "/etc/passwd", "../escape.go", "a/../../escape.go"}
	for _, rel := range cases {
		_, err := RunGoplsRename(ctxT(), b, rep, grants, j, RenameRequest{
			SourceDir: src, FileRelPath: rel, NewName: "X",
			GoBinary: "go", GoplsBinary: "gopls",
			OperationID: "op-x", TargetID: "target-x", RunID: "run-x", ProfileID: "work",
		})
		if err == nil {
			t.Fatalf("RunGoplsRename accepted unsafe FileRelPath %q", rel)
		}
	}
}

// Detector: fileURIFor percent-encodes URI-significant characters
// deterministically — a stronger, gopls-independent proof than relying
// on gopls's own parser leniency (which tolerated a raw '#'+space in
// practice, so TestRunGoplsRenameHandlesSpecialCharacterFilename alone
// would not have caught a REAL regression back to string concatenation).
func TestFileURIForEscapesSpecialCharacters(t *testing.T) {
	cases := map[string]string{
		"main.go":            "file:///work/src/main.go",
		"weird #1 file.go":   "file:///work/src/weird%20%231%20file.go",
		"has?query.go":       "file:///work/src/has%3Fquery.go",
		"literal%percent.go": "file:///work/src/literal%25percent.go",
	}
	for rel, want := range cases {
		got := fileURIFor(rel)
		if got != want {
			t.Errorf("fileURIFor(%q) = %q, want %q", rel, got, want)
		}
	}
}

// Detector (code-review finding, codex): a session killed by ITS OWN
// deadline is classified as CANCELLED, not a failed attempt — mirrors
// Run's identical classification (run.go). A 1ms Timeout cannot possibly
// complete a real gopls session, so the deadline fires mid-session.
func TestRunGoplsRenameClassifiesOwnDeadlineAsCancelled(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute) // generous grant TTL: only spec.Timeout should fire
	j := testJournal(t)

	src := t.TempDir()
	tinyModule(t, src)

	_, err := RunGoplsRename(ctxT(), b, rep, grants, j, RenameRequest{
		SourceDir: src, FileRelPath: "main.go",
		Line: 0, Character: 5, NewName: "Bar",
		GoBinary: goBin, GoplsBinary: goplsBin, Timeout: time.Millisecond,
		OperationID: "op-test-rename-deadline", TargetID: "target-test-rename-deadline",
		RunID: "run-test-rename-deadline", ProfileID: "work",
	})
	if err == nil {
		t.Fatal("expected an error from a 1ms deadline, got none")
	}
	if !strings.Contains(err.Error(), "cancelled by its own deadline") {
		t.Fatalf("expected the error to classify this as a self-deadline cancellation, got: %v", err)
	}
}

//go:build linux

package runner

import (
	"encoding/json"
	"os"
	"os/exec"
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

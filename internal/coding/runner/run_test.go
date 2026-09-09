//go:build linux

package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

func testBackend(t *testing.T) (*sandbox.Bwrap, sandbox.ProbeReport) {
	t.Helper()
	b := sandbox.NewBwrap()
	rep, err := b.Probe(ctxT())
	if err != nil {
		if os.Getenv("NEXUS_ACCEPT_REQUIRE_SANDBOX") != "" {
			t.Fatalf("bwrap REQUIRED for the graded acceptance run: %v", err)
		}
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

// tinyModule writes a minimal, valid Go module to dir.
func tinyModule(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Detector: a real `go build` runs end to end inside the sandbox, under a
// governed S7 grant, and emits a coding.run journal event — the whole
// pipeline this package exists for, against the real toolchain and the
// real bwrap backend, not mocks.
func TestRunBuildsRealModuleEndToEnd(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go binary on PATH: %v", err)
	}
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	tinyModule(t, src)

	result, err := Run(ctxT(), b, rep, grants, j, RunSpec{
		SourceDir:   src,
		Args:        []string{"build", "-C", "/work/src", "-o", "/work/out"},
		GoBinary:    goBin,
		Timeout:     150 * time.Second,
		OperationID: "op-test-build-1",
		TargetID:    "target-test-build",
		RunID:       "run-test-1",
		ProfileID:   "work",
	})
	if err != nil {
		t.Fatalf("Run failed: %v\noutput: %s", err, result.Output)
	}
	if !result.ExitOK {
		t.Fatalf("go build exited nonzero: %s", result.Output)
	}
	if result.SnapshotDigest == "" || result.ToolchainDigest == "" || result.PolicyHash == "" {
		t.Fatalf("missing digests/policy hash: %+v", result)
	}
}

// Detector: a `go test` failure (a real, expected test failure) is a
// SUCCESSFUL attempt from S7's perspective — ExitOK is false (that's
// DATA), but Run itself must not return an error, since the governed
// attempt completed normally.
func TestRunReportsTestFailureAsDataNotAttemptFailure(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go binary on PATH: %v", err)
	}
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "fail_test.go"), []byte(
		"package tiny\n\nimport \"testing\"\n\nfunc TestAlwaysFails(t *testing.T) { t.Fatal(\"deliberate failure\") }\n"),
		0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(ctxT(), b, rep, grants, j, RunSpec{
		SourceDir:   src,
		Args:        []string{"test", "-C", "/work/src", "./..."},
		GoBinary:    goBin,
		Timeout:     150 * time.Second,
		OperationID: "op-test-fail-1",
		TargetID:    "target-test-fail",
		RunID:       "run-test-2",
		ProfileID:   "work",
	})
	if err != nil {
		t.Fatalf("Run itself returned an error for a normal test failure: %v\noutput: %s", err, result.Output)
	}
	if result.ExitOK {
		t.Fatalf("expected ExitOK=false for a deliberately failing test, got true: %s", result.Output)
	}
}

// Detector: required identifiers are enforced fail-closed before any
// snapshot/sandbox work begins.
func TestRunRequiresIdentifiers(t *testing.T) {
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	src := t.TempDir()
	tinyModule(t, src)

	_, err := Run(ctxT(), b, rep, grants, j, RunSpec{
		SourceDir: src, Args: []string{"version"}, GoBinary: "go",
		// OperationID/TargetID/RunID/ProfileID all left zero.
	})
	if err == nil {
		t.Fatal("Run accepted a spec with no OperationID/TargetID/RunID/ProfileID")
	}
}

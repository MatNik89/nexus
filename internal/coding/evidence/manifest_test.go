//go:build linux

package evidence

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/checker"
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
		t.Skipf("bwrap unavailable: %v", err)
	}
	return b, rep
}

func testJournal(t *testing.T) *journal.Journal {
	t.Helper()
	j, err := journal.Open(filepath.Join(t.TempDir(), "journal.db"), "work", redact.None{}, map[string]journal.PayloadValidator{
		"coding.run":                nil,
		"coding.evidence_bound":     nil,
		"coding.evidence_completed": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func realGoBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go binary on PATH: %v", err)
	}
	return path
}

func writeModule(t *testing.T, dir, testBody string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/evidence\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(testBody), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Detector: a real fix — a test that fails on the base tree and passes
// on the candidate tree, with an unrelated test passing on both — is
// classified exactly per invariant 8 (FailToPass/PassToPass), through
// the real bwrap sandbox end to end, with a correctly causally-chained
// journal (bound -> base run -> candidate run -> completed, each
// event's ParentEventID pointing to the previous, strictly increasing
// JournalOffset).
func TestCaptureClassifiesRealFixEndToEnd(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	writeModule(t, baseDir, `package main

import "testing"

func TestStable(t *testing.T) {}
func TestBroken(t *testing.T) { t.Fatal("not fixed yet") }
`)
	candidateDir := t.TempDir()
	writeModule(t, candidateDir, `package main

import "testing"

func TestStable(t *testing.T) {}
func TestBroken(t *testing.T) {} // fixed
`)

	manifest, evidence, err := Capture(ctxT(), b, rep, grants, j, CaptureSpec{
		ContractID: "contract-1", WorkerID: "worker-1",
		BaseDir: baseDir, CandidateDir: candidateDir, GoBinary: goBin,
		Timeout: 150 * time.Second,
		BaseOperationID: "op-base-1", CandidateOperationID: "op-candidate-1",
		BaseTargetID: "target-base", CandidateTargetID: "target-candidate",
		RunID: "run-capture-1", ProfileID: "work",
	})
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}

	if len(manifest.Diff.FailToPass) != 1 || manifest.Diff.FailToPass[0] != "example.com/evidence.TestBroken" {
		t.Fatalf("expected FailToPass=[example.com/evidence.TestBroken], got %v", manifest.Diff.FailToPass)
	}
	if len(manifest.Diff.PassToPass) != 1 || manifest.Diff.PassToPass[0] != "example.com/evidence.TestStable" {
		t.Fatalf("expected PassToPass=[example.com/evidence.TestStable], got %v", manifest.Diff.PassToPass)
	}
	if len(manifest.Diff.PassToFail) != 0 || len(manifest.Diff.Missing) != 0 {
		t.Fatalf("expected zero regressions/missing, got PassToFail=%v Missing=%v", manifest.Diff.PassToFail, manifest.Diff.Missing)
	}
	if manifest.BaseTreeDigest == "" || manifest.CandidateTreeDigest == "" || manifest.BaseTreeDigest == manifest.CandidateTreeDigest {
		t.Fatalf("expected distinct, non-empty tree digests: base=%q candidate=%q", manifest.BaseTreeDigest, manifest.CandidateTreeDigest)
	}

	// Causal chain: bound -> base run -> candidate run -> completed.
	if manifest.BaseRunEvent.Envelope.ParentEventID == nil || *manifest.BaseRunEvent.Envelope.ParentEventID != manifest.BoundEvent.Envelope.EventID {
		t.Fatalf("base run's parent should be the bound event")
	}
	if manifest.CandidateRunEvent.Envelope.ParentEventID == nil || *manifest.CandidateRunEvent.Envelope.ParentEventID != manifest.BaseRunEvent.Envelope.EventID {
		t.Fatalf("candidate run's parent should be the base run event")
	}
	if manifest.CompletedEvent.Envelope.ParentEventID == nil || *manifest.CompletedEvent.Envelope.ParentEventID != manifest.CandidateRunEvent.Envelope.EventID {
		t.Fatalf("completed event's parent should be the candidate run event")
	}
	offsets := []uint64{manifest.BoundEvent.JournalOffset, manifest.BaseRunEvent.JournalOffset,
		manifest.CandidateRunEvent.JournalOffset, manifest.CompletedEvent.JournalOffset}
	for i := 1; i < len(offsets); i++ {
		if offsets[i] <= offsets[i-1] {
			t.Fatalf("expected strictly increasing journal offsets, got %v", offsets)
		}
	}

	// The produced checker.Evidence actually grades PASS under a
	// BEHAVIORAL contract requiring TestBroken to have been fixed.
	contract := checker.AcceptanceContract{ID: "contract-1", Worker: "worker-1", Criteria: []checker.Criterion{
		{CodingProofIs: &checker.CodingProofCriterion{Mode: checker.CodingProofBehavioral,
			RequiredFailToPass: []string{"example.com/evidence.TestBroken"}}},
	}}
	verdict, err := checker.Grade(contract, []checker.Evidence{evidence})
	if err != nil {
		t.Fatalf("Grade error: %v", err)
	}
	if !verdict.Pass {
		t.Fatalf("expected the produced evidence to grade PASS: %+v", verdict)
	}
}

// Detector: a real regression — a test that passes on base and fails on
// candidate — is reported in PassToFail and the produced evidence grades
// FAIL under checker.Grade, regardless of any unrelated FailToPass fix.
func TestCaptureDetectsRealRegressionEndToEnd(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	writeModule(t, baseDir, `package main

import "testing"

func TestWasFine(t *testing.T) {}
`)
	candidateDir := t.TempDir()
	writeModule(t, candidateDir, `package main

import "testing"

func TestWasFine(t *testing.T) { t.Fatal("regressed") }
`)

	manifest, evidence, err := Capture(ctxT(), b, rep, grants, j, CaptureSpec{
		ContractID: "contract-2", WorkerID: "worker-1",
		BaseDir: baseDir, CandidateDir: candidateDir, GoBinary: goBin,
		Timeout: 150 * time.Second,
		BaseOperationID: "op-base-2", CandidateOperationID: "op-candidate-2",
		BaseTargetID: "target-base-2", CandidateTargetID: "target-candidate-2",
		RunID: "run-capture-2", ProfileID: "work",
	})
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	if len(manifest.Diff.PassToFail) != 1 || manifest.Diff.PassToFail[0] != "example.com/evidence.TestWasFine" {
		t.Fatalf("expected PassToFail=[example.com/evidence.TestWasFine], got %v", manifest.Diff.PassToFail)
	}

	contract := checker.AcceptanceContract{ID: "contract-2", Worker: "worker-1", Criteria: []checker.Criterion{
		{CodingProofIs: &checker.CodingProofCriterion{Mode: checker.CodingProofBehavioral}},
	}}
	verdict, err := checker.Grade(contract, []checker.Evidence{evidence})
	if err != nil {
		t.Fatalf("Grade error: %v", err)
	}
	if verdict.Pass {
		t.Fatalf("expected the produced evidence to grade FAIL on a real regression: %+v", verdict)
	}
}

// Detector (fail-closed identifiers): missing required fields refused
// before any snapshot/sandbox work begins.
func TestCaptureRequiresIdentifiers(t *testing.T) {
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)
	dir := t.TempDir()
	writeModule(t, dir, "package main\n\nfunc TestX(t *testing.T) {}\n")

	_, _, err := Capture(ctxT(), b, rep, grants, j, CaptureSpec{
		BaseDir: dir, CandidateDir: dir, GoBinary: "go",
	})
	if err == nil {
		t.Fatal("Capture accepted a spec with no ContractID/WorkerID/identifiers")
	}
}

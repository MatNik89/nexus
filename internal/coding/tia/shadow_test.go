//go:build linux

package tia

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/coding/evidence"
	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
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
		"coding.run":              nil,
		"coding.tia_shadow_bound": nil,
		"coding.tia_shadow":       nil,
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

func leafFiles(bodyOK bool) (string, string) {
	body := `package leaf

func Double(n int) int { return n * 2 }
`
	if !bodyOK {
		body = `package leaf

func Double(n int) int { return n * 3 } // BUG: should be n*2
`
	}
	test := `package leaf

import "testing"

func TestDouble(t *testing.T) {
	if got := Double(3); got != 6 {
		t.Fatalf("Double(3) = %d, want 6", got)
	}
}
`
	return body, test
}

const unrelatedBody = `package unrelated

func Greet() string { return "hi" }
`
const unrelatedTest = `package unrelated

import "testing"

func TestGreet(t *testing.T) {
	if Greet() != "hi" {
		t.Fatal("Greet() wrong")
	}
}
`

// writeFixture writes a module with two INDEPENDENT packages (leaf,
// unrelated — neither imports the other) so a change confined to one has
// no reason to select the other.
func writeFixture(t *testing.T, dir string, leafOK bool) {
	t.Helper()
	leafBody, leafTest := leafFiles(leafOK)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tiashadow\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "leaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leaf", "leaf.go"), []byte(leafBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leaf", "leaf_test.go"), []byte(leafTest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "unrelated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated", "unrelated.go"), []byte(unrelatedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated", "unrelated_test.go"), []byte(unrelatedTest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newSpec(runID contracts.RunID) ShadowSpec {
	return ShadowSpec{
		ListOperationID: contracts.OperationID("op-list-" + string(runID)), ListTargetID: contracts.TargetID("target-list-" + string(runID)),
		FullTestOperationID: contracts.OperationID("op-full-" + string(runID)), FullTestTargetID: contracts.TargetID("target-full-" + string(runID)),
		SelectedTestOperationID: contracts.OperationID("op-selected-" + string(runID)), SelectedTestTargetID: contracts.TargetID("target-selected-" + string(runID)),
		RunID: runID, ProfileID: "work",
	}
}

// Detector: a real regression confined to one package (leaf) — with an
// unrelated, untouched package (unrelated) in the same module — is
// correctly selected (SelectedPackages == [leaf]), the real full-suite
// run finds the real failure, the real selected-subset run ALSO finds
// it (recall accurate), and no fallback is triggered.
func TestRunShadowSelectsOnlyAffectedPackageAndRecallAccurate(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	writeFixture(t, baseDir, true)
	candidateDir := t.TempDir()
	writeFixture(t, candidateDir, false) // leaf regresses; unrelated untouched

	spec := newSpec("shadow-1")
	spec.BaseDir, spec.CandidateDir, spec.GoBinary, spec.Timeout = baseDir, candidateDir, goBin, 150*time.Second

	result, err := RunShadow(ctxT(), b, rep, grants, j, spec)
	if err != nil {
		t.Fatalf("RunShadow failed: %v", err)
	}
	if result.FallbackReason != "" {
		t.Fatalf("expected no fallback, got %q", result.FallbackReason)
	}
	if len(result.SelectedPackages) != 1 || result.SelectedPackages[0] != "example.com/tiashadow/leaf" {
		t.Fatalf("SelectedPackages = %v, want [example.com/tiashadow/leaf]", result.SelectedPackages)
	}
	if len(result.ActualFailedPackages) != 1 || result.ActualFailedPackages[0] != "example.com/tiashadow/leaf" {
		t.Fatalf("ActualFailedPackages = %v, want [example.com/tiashadow/leaf]", result.ActualFailedPackages)
	}
	if !result.RecallEvaluated {
		t.Fatal("expected RecallEvaluated=true (a real non-fallback selected run was executed)")
	}
	if !result.RecallAccurate || len(result.MissedPackages) != 0 {
		t.Fatalf("expected RecallAccurate=true, MissedPackages=[], got RecallAccurate=%v Missed=%v", result.RecallAccurate, result.MissedPackages)
	}
	if len(result.FullPackages) != 2 {
		t.Fatalf("FullPackages = %v, want both leaf and unrelated", result.FullPackages)
	}
	if result.ShadowEvent.Envelope.ParentEventID == nil {
		t.Fatal("shadow event must be parented to the last run event")
	}
}

// Detector (PLAN-CODING-TRIO.md invariant 5): a go.mod change between
// base and candidate forces fallback to the full suite — selection must
// never guess at a workspace-wide build-configuration change.
func TestRunShadowFallsBackOnGoModChange(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	writeFixture(t, baseDir, true)
	candidateDir := t.TempDir()
	writeFixture(t, candidateDir, true)
	// Perturb go.mod only (add a comment) — a real content change, no
	// package-level effect, but still a build-control file change.
	if err := os.WriteFile(filepath.Join(candidateDir, "go.mod"), []byte("module example.com/tiashadow\n\ngo 1.21\n// perturbed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := newSpec("shadow-2")
	spec.BaseDir, spec.CandidateDir, spec.GoBinary, spec.Timeout = baseDir, candidateDir, goBin, 150*time.Second

	result, err := RunShadow(ctxT(), b, rep, grants, j, spec)
	if err != nil {
		t.Fatalf("RunShadow failed: %v", err)
	}
	if result.FallbackReason == "" {
		t.Fatal("expected a non-empty FallbackReason for a go.mod change")
	}
	if len(result.SelectedPackages) != len(result.FullPackages) {
		t.Fatalf("expected SelectedPackages == FullPackages on fallback, got selected=%v full=%v", result.SelectedPackages, result.FullPackages)
	}
	if result.RecallEvaluated {
		t.Fatal("expected RecallEvaluated=false on fallback — nothing was held back to measure")
	}
	if !result.RecallAccurate {
		t.Fatal("expected RecallAccurate=true on fallback (the full suite ran, trivially no gap)")
	}
}

// writeChangedStaleFixture writes a module with two INDEPENDENT packages:
// "changed" (its own source touched between base/candidate, but its test
// always passes) and "stale" (completely UNTOUCHED between base and
// candidate, but its test ALWAYS fails — a pre-existing bug unrelated to
// the change).
func writeChangedStaleFixture(t *testing.T, dir string, changedComment string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tiashadow\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	changedBody := changedComment + `package changed

func Value() int { return 42 }
`
	if err := os.WriteFile(filepath.Join(dir, "changed", "changed.go"), []byte(changedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	changedTest := `package changed

import "testing"

func TestValue(t *testing.T) {
	if Value() != 42 {
		t.Fatal("Value() wrong")
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "changed", "changed_test.go"), []byte(changedTest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	staleBody := `package stale

func Broken() int { return 1 }
`
	if err := os.WriteFile(filepath.Join(dir, "stale", "stale.go"), []byte(staleBody), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTest := `package stale

import "testing"

func TestBroken(t *testing.T) {
	t.Fatal("pre-existing bug, unrelated to any change")
}
`
	if err := os.WriteFile(filepath.Join(dir, "stale", "stale_test.go"), []byte(staleTest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Detector (code-review finding, codex: no existing test proved an
// actual recall MISS was correctly computed — replacing the comparison
// with a vacuous "missed=nil; recallAccurate=true" would have left every
// prior test green). A pre-existing, unrelated failure in an UNTOUCHED
// package ("stale") is exactly the case package-level TIA cannot catch:
// selection correctly picks only the touched package ("changed"), the
// real full-suite run finds "stale" failing (ground truth), and the real
// selected-subset run (which never touches "stale") cannot have observed
// it — MissedPackages must report it and RecallAccurate must be false.
func TestRunShadowDetectsRealRecallMiss(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	writeChangedStaleFixture(t, baseDir, "")
	candidateDir := t.TempDir()
	writeChangedStaleFixture(t, candidateDir, "// a real, behavior-preserving comment change\n")

	spec := newSpec("shadow-3")
	spec.BaseDir, spec.CandidateDir, spec.GoBinary, spec.Timeout = baseDir, candidateDir, goBin, 150*time.Second

	result, err := RunShadow(ctxT(), b, rep, grants, j, spec)
	if err != nil {
		t.Fatalf("RunShadow failed: %v", err)
	}
	if result.FallbackReason != "" {
		t.Fatalf("expected no fallback, got %q", result.FallbackReason)
	}
	if len(result.SelectedPackages) != 1 || result.SelectedPackages[0] != "example.com/tiashadow/changed" {
		t.Fatalf("SelectedPackages = %v, want [example.com/tiashadow/changed]", result.SelectedPackages)
	}
	if len(result.ActualFailedPackages) != 1 || result.ActualFailedPackages[0] != "example.com/tiashadow/stale" {
		t.Fatalf("ActualFailedPackages = %v, want [example.com/tiashadow/stale] (pre-existing bug)", result.ActualFailedPackages)
	}
	if !result.RecallEvaluated {
		t.Fatal("expected RecallEvaluated=true")
	}
	if len(result.MissedPackages) != 1 || result.MissedPackages[0] != "example.com/tiashadow/stale" {
		t.Fatalf("MissedPackages = %v, want [example.com/tiashadow/stale]", result.MissedPackages)
	}
	if result.RecallAccurate {
		t.Fatal("expected RecallAccurate=false — the selected subset never touched the failing package")
	}
}

// Detector (code-review finding, codex: PLAN-CODING-TRIO.md explicitly
// requires shadow mode to "always run the full suite" — the original
// implementation returned a hard error on any list-analysis failure,
// silently skipping the mandatory full run and producing NO shadow
// evidence at all). A real go-list-level failure (a genuine syntax error
// makes `go list -deps -test -json ./...` itself exit nonzero) must
// still: (1) not return an error from RunShadow, (2) record a non-empty
// FallbackReason, (3) still execute and journal the full-suite test run,
// (4) still append a final coding.tia_shadow event.
func TestRunShadowStillRunsFullSuiteOnListAnalysisFailure(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	writeFixture(t, baseDir, true)
	candidateDir := t.TempDir()
	writeFixture(t, candidateDir, true)
	// A genuine unresolvable import: `go list -deps -test -json ./...`
	// itself exits nonzero on this module, before any test ever runs
	// (verified live: a body-level syntax error alone does NOT make
	// `go list` fail — it only inspects package/import structure, not
	// function bodies — but a missing import does).
	if err := os.WriteFile(filepath.Join(candidateDir, "leaf", "leaf.go"), []byte(`package leaf

import "example.com/tiashadow/doesnotexist"

func Double(n int) int { return doesnotexist.X }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := newSpec("shadow-4")
	spec.BaseDir, spec.CandidateDir, spec.GoBinary, spec.Timeout = baseDir, candidateDir, goBin, 150*time.Second

	result, err := RunShadow(ctxT(), b, rep, grants, j, spec)
	if err != nil {
		t.Fatalf("RunShadow returned an error instead of falling back: %v", err)
	}
	if result.FallbackReason == "" {
		t.Fatal("expected a non-empty FallbackReason for a broken go list run")
	}
	if len(result.ActualFailedPackages) == 0 {
		t.Fatal("expected the full test run to still have executed and observed the broken package failing")
	}
	if (result.ShadowEvent.Envelope.EventID) == "" {
		t.Fatal("expected a real coding.tia_shadow event to still be appended")
	}
}

// Detector (code-review finding, codex, round 2: the digest/mutation/
// toolchain checks had no durable, RED-capable regression detector —
// only a temporary in-session disable/rerun/restore cycle). Pure,
// synthetic-value table test against checkRunIntegrity — no sandbox
// required.
func TestCheckRunIntegrity(t *testing.T) {
	base := runner.RunResult{SnapshotDigest: "d1", PostSnapshotDigest: "d1", ToolchainDigest: "tc1"}
	cases := []struct {
		name           string
		result         runner.RunResult
		expectDigest   string
		expectToolchain string
		wantErr        bool
	}{
		{"clean", base, "d1", "tc1", false},
		{"clean-no-toolchain-check", base, "d1", "", false},
		{"truncated", runner.RunResult{Truncated: true, SnapshotDigest: "d1", PostSnapshotDigest: "d1", ToolchainDigest: "tc1"}, "d1", "tc1", true},
		{"snapshot-mismatch", runner.RunResult{SnapshotDigest: "d2", PostSnapshotDigest: "d2", ToolchainDigest: "tc1"}, "d1", "tc1", true},
		{"mutated-during-run", runner.RunResult{SnapshotDigest: "d1", PostSnapshotDigest: "d1-mutated", ToolchainDigest: "tc1"}, "d1", "tc1", true},
		{"toolchain-drift", runner.RunResult{SnapshotDigest: "d1", PostSnapshotDigest: "d1", ToolchainDigest: "tc2"}, "d1", "tc1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkRunIntegrity("test-stage", c.result, c.expectDigest, c.expectToolchain)
			if c.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

// Detector (code-review finding, codex, round 2): a test run that exited
// nonzero with ZERO parsed outcomes and ZERO package-level failures (an
// unexplained crash, not an ordinary test failure) must be refused, not
// silently treated as "zero failures observed."
func TestCheckExplainedExit(t *testing.T) {
	cases := []struct {
		name    string
		exitOK  bool
		parsed  evidence.ParsedRun
		wantErr bool
	}{
		{"exit-ok", true, evidence.ParsedRun{}, false},
		{"nonzero-with-fail-outcome", false, evidence.ParsedRun{Outcomes: map[evidence.TestKey]evidence.TestOutcome{{Package: "p", Test: "T"}: evidence.OutcomeFail}}, false},
		{"nonzero-with-failed-package", false, evidence.ParsedRun{FailedPackages: []string{"p"}}, false},
		{"nonzero-unexplained", false, evidence.ParsedRun{}, true},
		// Round-3 codex finding: PASS/SKIP-only outcomes do NOT explain a
		// nonzero exit — a multi-package run can emit clean results for
		// some packages before a later command-level failure.
		{"nonzero-with-only-pass", false, evidence.ParsedRun{Outcomes: map[evidence.TestKey]evidence.TestOutcome{{Package: "p", Test: "T"}: evidence.OutcomePass}}, true},
		{"nonzero-with-only-skip", false, evidence.ParsedRun{Outcomes: map[evidence.TestKey]evidence.TestOutcome{{Package: "p", Test: "T"}: evidence.OutcomeSkip}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkExplainedExit("test-stage", c.exitOK, c.parsed)
			if c.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

// Detector (code-review finding, codex, round 3: checkRunIntegrity's
// production wiring was not load-bearing — deleting either call site
// left every existing test green, since the pure table test only
// exercises the helper in isolation). A test that PASSES but mutates its
// own /work/src as a side effect (exactly evidence.Capture's own
// TestCaptureRefusesWhenTestMutatesItsOwnSourceTree pattern) must make
// RunShadow refuse via the REAL production call sites, through the real
// bwrap sandbox — not just the synthetic unit test. RED-proven against
// BOTH call sites together (this single-package fixture's one test runs
// in both the full-suite AND the selected-subset stage, so disabling
// only one call site alone still left the other one catching it —
// confirmed by disabling each independently before writing this test).
func TestRunShadowRefusesWhenATestMutatesItsOwnSourceTree(t *testing.T) {
	goBin := realGoBinary(t)
	b, rep := testBackend(t)
	grants := s7.NewAuthority(time.Now, 5*time.Minute)
	j := testJournal(t)

	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "go.mod"), []byte("module example.com/tiashadow\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "main_test.go"), []byte("package main\n\nimport \"testing\"\n\nfunc TestStable(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(candidateDir, "go.mod"), []byte("module example.com/tiashadow\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidateDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// This test PASSES, but self-mutates its own source file as a side
	// effect during the FULL suite run — exactly the class of tree drift
	// checkRunIntegrity's post-run digest check exists to catch.
	if err := os.WriteFile(filepath.Join(candidateDir, "main_test.go"), []byte(`package main

import (
	"os"
	"testing"
)

func TestStable(t *testing.T) {
	if err := os.WriteFile("mutated.txt", []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := newSpec("shadow-5")
	spec.BaseDir, spec.CandidateDir, spec.GoBinary, spec.Timeout = baseDir, candidateDir, goBin, 150*time.Second

	_, err := RunShadow(ctxT(), b, rep, grants, j, spec)
	if err == nil {
		t.Fatal("expected RunShadow to refuse when the full test run mutates its own source tree")
	}
}

// Detector (code-review finding, codex, round 4): pure table test for
// checkTerminalCoverage — a package present in `expected` but absent
// from `seen` must be refused, regardless of whether OTHER packages in
// the same run reported real failures.
func TestCheckTerminalCoverage(t *testing.T) {
	cases := []struct {
		name     string
		expected []string
		seen     []string
		wantErr  bool
	}{
		{"full-coverage", []string{"a", "b"}, []string{"a", "b"}, false},
		{"extra-seen-is-fine", []string{"a"}, []string{"a", "b"}, false},
		{"empty-expected", nil, []string{"a"}, false},
		{"missing-one", []string{"a", "b"}, []string{"a"}, true},
		{"missing-all", []string{"a", "b"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkTerminalCoverage("test-stage", c.expected, c.seen)
			if c.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}


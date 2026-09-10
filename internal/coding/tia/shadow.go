//go:build linux

// Package tia is Slice 2's Test Impact Analysis orchestrator, SHADOW MODE
// ONLY (PLAN-CODING-TRIO.md): it never gates or skips a real test run. It
// measures whether internal/coding/impact's package-selection algorithm
// would have caught every failure a real full-suite run actually found,
// and journals the comparison — the plan's own mechanism for building
// confidence in TIA before it is ever trusted to select real test runs.
//
// Depends on internal/coding/impact (pure graph walk), internal/coding/
// runner (governed `go list`/`go test` subprocess launch), internal/
// coding/evidence (reuses ParseTestJSON for the selected/full test runs
// — no import cycle: tia -> evidence -> runner), internal/kernel/journal,
// internal/kernel/s7, internal/sandbox.
package tia

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/coding/evidence"
	"github.com/MatNik89/nexus/internal/coding/impact"
	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
)

// sandboxRoot is the fixed mount point runner.Run always stages the
// snapshot at. `go list`/`go test` run INSIDE the sandbox against that
// staged copy, so every workspace package's Dir is reported relative to
// THIS path, not the host snapshot directory — impact.BuildFileIndex
// must be built against this same root so its relative paths match
// runner.DigestTreeFiles' host-relative-to-snapshot paths exactly (both
// reduce to the identical "subpath/file.go" shape, since CreateSnapshot
// copies the tree faithfully).
const sandboxRoot = "/work/src"

// ShadowSpec describes one shadow-mode comparison: a base tree (before a
// change) and a candidate tree (after it), TIA's own selection over that
// change, and a real full-suite run to measure it against.
type ShadowSpec struct {
	BaseDir, CandidateDir string
	GoBinary              string
	Timeout               time.Duration

	// Separate identifiers per governed subprocess attempt (list, full
	// test, selected test) — mirrors Slice 1's evidence.CaptureSpec
	// pattern: each is its own S7-governed attempt with its own coding.run
	// journal event.
	ListOperationID         contracts.OperationID
	ListTargetID            contracts.TargetID
	FullTestOperationID     contracts.OperationID
	FullTestTargetID        contracts.TargetID
	SelectedTestOperationID contracts.OperationID
	SelectedTestTargetID    contracts.TargetID

	RunID     contracts.RunID
	ProfileID contracts.ProfileID
}

// ShadowResult is the human/audit-facing record of one shadow-mode
// comparison.
type ShadowResult struct {
	ChangedFiles         []impact.FileChange
	ChangedPackages      []string
	SelectedPackages     []string
	FullPackages         []string
	FallbackReason       string // non-empty when selection fell back to the full suite
	ActualFailedPackages []string
	MissedPackages       []string // ActualFailedPackages the selected subset's OWN execution did not observe
	RecallEvaluated      bool     // true only when a real selected-subset run was executed and compared
	RecallAccurate       bool
	ShadowEvent          journal.Event
}

// RunShadow runs the full TIA shadow-mode comparison: a governed
// `go list -deps -test -json` on the candidate tree builds the package
// graph and file index; DiffFileMaps + FileIndex.ResolveChanges resolve
// the base->candidate file changes to changed packages; Graph.Affected
// computes TIA's proposed selection. A real full-suite `go test -json`
// run establishes ground truth; when selection did not fall back, a
// SEPARATE real `go test -json` run over exactly the selected packages
// is also executed — comparing actual failures from the FULL run against
// actual failures from the SELECTED run's own execution is the recall
// measurement (codex's Slice 2 research: comparing failure package names
// only, without actually running the selected subset, is insufficient —
// a dependent package can still catch a dependency's compile failure by
// name alone, which would hide a real selection gap).
//
// A failed, truncated, or otherwise unresolvable `go list` run (or a
// parse/graph-build error on its output) does NOT abort — per
// PLAN-CODING-TRIO.md's explicit "always run the full suite" requirement,
// it is captured as FallbackReason and execution proceeds to the full
// test run regardless (the ONLY thing that changes is TIA has no real
// selection to compare against). RunShadow instead fails closed (returns
// an error, produces no result) on: a genuine infrastructure failure
// launching any governed subprocess; a truncated full or selected test
// run's captured output; a snapshot digest mismatch or mid-run mutation
// on any test-executing run (TOCTOU, mirrors evidence.Capture's own
// checks); a toolchain digest drift between the list/full-test/
// selected-test runs; or a test run that exited nonzero with zero parsed
// outcomes (an unexplained failure, not an ordinary test failure).
func RunShadow(ctx context.Context, backend sandbox.Backend, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal, spec ShadowSpec) (ShadowResult, error) {
	if spec.BaseDir == "" || spec.CandidateDir == "" || spec.GoBinary == "" {
		return ShadowResult{}, fmt.Errorf("tia: BaseDir, CandidateDir, and GoBinary are all required")
	}
	if !spec.ListOperationID.Valid() || !spec.ListTargetID.Valid() ||
		!spec.FullTestOperationID.Valid() || !spec.FullTestTargetID.Valid() ||
		!spec.SelectedTestOperationID.Valid() || !spec.SelectedTestTargetID.Valid() ||
		!spec.RunID.Valid() || !spec.ProfileID.Valid() {
		return ShadowResult{}, fmt.Errorf("tia: all operation/target/run/profile identifiers are required (fail closed)")
	}
	if j == nil {
		return ShadowResult{}, fmt.Errorf("tia: a journal is required — a shadow-mode measurement with no durable record proves nothing")
	}

	baseSnap, cleanupBase, err := runner.CreateSnapshot(spec.BaseDir)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: hashing base tree: %w", err)
	}
	defer cleanupBase()
	candidateSnap, cleanupCand, err := runner.CreateSnapshot(spec.CandidateDir)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: hashing candidate tree: %w", err)
	}
	defer cleanupCand()

	boundEvent, err := appendBoundEvent(ctx, j, spec, baseSnap.Digest, candidateSnap.Digest)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: %w", err)
	}

	// Analysis phase (list -> parse -> graph -> file index -> diff ->
	// resolve -> select): PLAN-CODING-TRIO.md Slice 2 requires shadow mode
	// to "always run the full suite" — an analysis-level problem (bad/
	// truncated list output, a parse or graph-build error, an unresolved
	// change) is captured as fallbackReason and the full suite ALWAYS
	// still runs (code-review finding, codex: the original version
	// returned a hard error here, silently skipping the mandatory full
	// run and producing no shadow evidence at all — the exact opposite of
	// what shadow mode exists to guarantee). Only a genuine infrastructure
	// failure from the list run's OWN launch (sandbox/context failure,
	// not a data-quality problem) still aborts outright, since the same
	// infrastructure failure would equally prevent the full test run.
	var (
		graph          *impact.Graph
		fileIdx        *impact.FileIndex
		listToolchain  string
		fallbackReason string
	)
	listParent := boundEvent.Envelope.EventID
	listResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir: candidateSnap.Dir, Args: []string{"list", "-C", sandboxRoot, "-deps", "-test", "-json", "./..."},
		GoBinary: spec.GoBinary, Timeout: spec.Timeout,
		OperationID: spec.ListOperationID, TargetID: spec.ListTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
		ParentEventID: &listParent,
	})
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: list run: %w", err)
	}
	listToolchain = listResult.ToolchainDigest
	switch {
	case listResult.Truncated:
		fallbackReason = "list run's captured output was truncated"
	case listResult.SnapshotDigest != candidateSnap.Digest:
		fallbackReason = fmt.Sprintf("candidate tree digest changed between snapshot (%s) and list execution (%s)", candidateSnap.Digest, listResult.SnapshotDigest)
	case listResult.PostSnapshotDigest != listResult.SnapshotDigest:
		fallbackReason = "candidate tree was mutated during the list run"
	case !listResult.ExitOK:
		fallbackReason = "go list exited nonzero"
	default:
		if parsed, perr := impact.ParseGoList(strings.NewReader(listResult.Stdout)); perr != nil {
			fallbackReason = fmt.Sprintf("parsing go list output: %v", perr)
		} else if idx, ferr := impact.BuildFileIndex(parsed, sandboxRoot); ferr != nil {
			fallbackReason = fmt.Sprintf("building file index: %v", ferr)
		} else {
			graph = impact.BuildGraph(parsed)
			fileIdx = idx
		}
	}

	var fullPackages, changedPackages, selected []string
	var changes []impact.FileChange
	if fallbackReason == "" {
		baseFiles, _, err := runner.DigestTreeFiles(baseSnap.Dir)
		if err != nil {
			fallbackReason = fmt.Sprintf("digesting base tree: %v", err)
		} else if candFiles, _, err := runner.DigestTreeFiles(candidateSnap.Dir); err != nil {
			fallbackReason = fmt.Sprintf("digesting candidate tree: %v", err)
		} else {
			changes = impact.DiffFileMaps(fileDigestMap(baseFiles), fileDigestMap(candFiles))
			fullPackages = graph.Targets()
			if changed, reason, ok := fileIdx.ResolveChanges(changes); !ok {
				fallbackReason = reason
			} else if affected, resolved := graph.Affected(changed); !resolved {
				fallbackReason = "Affected() reported an unresolved package in the closure"
			} else {
				changedPackages = changed
				selected = affected
			}
		}
	}
	fallback := fallbackReason != ""
	if fallback {
		// fullPackages may be nil here (the list/graph itself never
		// resolved) — that is an honest report, not a bug: there is
		// nothing to enumerate, so the full suite runs via `./...`
		// directly (it never depends on an explicit package list).
		selected = fullPackages
	}

	fullTestParent := listResult.JournalEvent.Envelope.EventID
	fullTestResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir: candidateSnap.Dir, Args: []string{"test", "-C", sandboxRoot, "-json", "./..."},
		GoBinary: spec.GoBinary, Timeout: spec.Timeout,
		OperationID: spec.FullTestOperationID, TargetID: spec.FullTestTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
		ParentEventID: &fullTestParent,
	})
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: full test run: %w", err)
	}
	if ierr := checkRunIntegrity("full test", fullTestResult, candidateSnap.Digest, listToolchain); ierr != nil {
		return ShadowResult{}, ierr
	}
	fullParsed, err := evidence.ParseTestJSON(fullTestResult.Stdout)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: parsing full test run: %w", err)
	}
	if eerr := checkExplainedExit("full test", fullTestResult.ExitOK, fullParsed); eerr != nil {
		return ShadowResult{}, eerr
	}
	if len(fullPackages) > 0 {
		if cerr := checkTerminalCoverage("full test", fullPackages, fullParsed.TerminalPackages); cerr != nil {
			return ShadowResult{}, cerr
		}
	}
	actualFailed := failedPackagesOf(fullParsed)

	lastParent := fullTestResult.JournalEvent.Envelope.EventID
	var missed []string
	var recallEvaluated, recallAccurate bool

	switch {
	case fallback:
		// Selection ran everything the full suite ran — trivially no
		// selection gap is possible, but this is NOT the same claim as
		// "recall measured": nothing was actually held back to measure
		// against.
		recallEvaluated = false
		recallAccurate = true
	case len(selected) == 0:
		// A real, non-fallback selection of ZERO packages: nothing was
		// run for the selected subset, so it cannot have observed any
		// failure the full suite found. Measured directly (`go test`
		// with zero package arguments defaults to the current directory
		// package, not "nothing" — invoking it here would silently test
		// the wrong thing) rather than by launching a vacuous subprocess.
		recallEvaluated = true
		missed = actualFailed
		recallAccurate = len(missed) == 0
	default:
		selectedArgs := append([]string{"test", "-C", sandboxRoot, "-json"}, selected...)
		selTestParent := lastParent
		selResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
			SourceDir: candidateSnap.Dir, Args: selectedArgs,
			GoBinary: spec.GoBinary, Timeout: spec.Timeout,
			OperationID: spec.SelectedTestOperationID, TargetID: spec.SelectedTestTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
			ParentEventID: &selTestParent,
		})
		if err != nil {
			return ShadowResult{}, fmt.Errorf("tia: selected test run: %w", err)
		}
		if ierr := checkRunIntegrity("selected test", selResult, candidateSnap.Digest, fullTestResult.ToolchainDigest); ierr != nil {
			return ShadowResult{}, ierr
		}
		selParsed, err := evidence.ParseTestJSON(selResult.Stdout)
		if err != nil {
			return ShadowResult{}, fmt.Errorf("tia: parsing selected test run: %w", err)
		}
		if eerr := checkExplainedExit("selected test", selResult.ExitOK, selParsed); eerr != nil {
			return ShadowResult{}, eerr
		}
		if cerr := checkTerminalCoverage("selected test", selected, selParsed.TerminalPackages); cerr != nil {
			return ShadowResult{}, cerr
		}
		selectedFailed := failedPackagesOf(selParsed)
		missed = sortedDiff(actualFailed, selectedFailed)
		recallEvaluated = true
		recallAccurate = len(missed) == 0
		lastParent = selResult.JournalEvent.Envelope.EventID
	}

	shadowEvent, err := appendShadowEvent(ctx, j, spec, changes, changedPackages, selected, fullPackages, fallbackReason, actualFailed, missed, recallEvaluated, recallAccurate, &lastParent)
	if err != nil {
		return ShadowResult{}, fmt.Errorf("tia: %w", err)
	}

	return ShadowResult{
		ChangedFiles:         changes,
		ChangedPackages:      changedPackages,
		SelectedPackages:     selected,
		FullPackages:         fullPackages,
		FallbackReason:       fallbackReason,
		ActualFailedPackages: actualFailed,
		MissedPackages:       missed,
		RecallEvaluated:      recallEvaluated,
		RecallAccurate:       recallAccurate,
		ShadowEvent:          shadowEvent,
	}, nil
}

// checkRunIntegrity centralizes the TOCTOU/mutation/toolchain-consistency
// checks shared by every test-executing runner.Run result this
// orchestrator uses (code-review finding, codex, round 2: the full and
// selected runs originally duplicated the list run's own checks inline,
// with the duplication itself leaving no single place a durable table
// test could exercise). Pure and synthetic-value testable — no sandbox
// required. expectedToolchain=="" skips the toolchain comparison (the
// list run has no earlier run to compare against).
func checkRunIntegrity(stage string, result runner.RunResult, expectedSnapshotDigest, expectedToolchain string) error {
	if result.Truncated {
		return fmt.Errorf("tia: %s run's captured output was truncated — refusing (fail closed)", stage)
	}
	if result.SnapshotDigest != expectedSnapshotDigest {
		return fmt.Errorf("tia: candidate tree digest changed between snapshot (%s) and %s execution (%s) — refusing (fail closed)",
			expectedSnapshotDigest, stage, result.SnapshotDigest)
	}
	if result.PostSnapshotDigest != result.SnapshotDigest {
		return fmt.Errorf("tia: candidate tree was mutated during the %s run — refusing (fail closed)", stage)
	}
	if expectedToolchain != "" && result.ToolchainDigest != expectedToolchain {
		return fmt.Errorf("tia: %s run resolved a DIFFERENT toolchain (%s vs %s) — refusing, a recall verdict would not be attributable to one consistent build (fail closed)",
			stage, expectedToolchain, result.ToolchainDigest)
	}
	return nil
}

// checkExplainedExit refuses a test run whose process exited nonzero
// without any parsed data that actually EXPLAINS the failure — a
// package-level compile failure (FailedPackages) or at least one
// OutcomeFail. Round-2 fix (code-review finding, codex, mirroring
// Slice 1's evidence.Capture precedent at manifest.go) accepted ANY
// non-empty parsed.Outcomes as sufficient, but a multi-package run can
// emit PASS/SKIP outcomes for packages that finished cleanly before a
// LATER command-level failure (a panic, an OOM, a toolchain crash)
// caused the overall process to exit nonzero — that partial stream must
// not be silently accepted as complete ground truth (round-3 finding,
// codex).
func checkExplainedExit(stage string, exitOK bool, parsed evidence.ParsedRun) error {
	if exitOK {
		return nil
	}
	if len(parsed.FailedPackages) > 0 {
		return nil
	}
	for _, outcome := range parsed.Outcomes {
		if outcome == evidence.OutcomeFail {
			return nil
		}
	}
	return fmt.Errorf("tia: %s run exited nonzero with no observed failure explaining it — refusing (fail closed)", stage)
}

// checkTerminalCoverage refuses a test run whose parsed TerminalPackages
// (packages that reached their OWN package-level pass/fail/skip action —
// see evidence.ParsedRun) does not include one of the packages it was
// expected to cover (code-review finding, codex, rounds 4-5):
// checkExplainedExit alone only catches a run with ZERO explanation for
// a nonzero exit — it does not catch a PARTIAL stream where package A
// genuinely reached a terminal failure (a real, valid explanation) but
// the `go test` process then crashed/was killed before package B ever
// reached ITS OWN terminal action — merely emitting "start" for B is not
// enough (round-5 finding: an earlier version of this check accepted any
// event at all, including "start", as sufficient). Without this, that
// partial run would be accepted as complete ground truth, potentially
// reporting accurate recall despite B never having been verified at all.
// Only checked when expected is non-empty — an unresolved/fallback
// selection with no known package list has nothing to check coverage
// against.
func checkTerminalCoverage(stage string, expected, seen []string) error {
	seenSet := make(map[string]bool, len(seen))
	for _, p := range seen {
		seenSet[p] = true
	}
	var missing []string
	for _, p := range expected {
		if !seenSet[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("tia: %s run reported no TERMINAL action for %d expected package(s) (e.g. %s) — refusing to treat a partial stream as complete (fail closed)",
			stage, len(missing), missing[0])
	}
	return nil
}

func fileDigestMap(files []runner.FileDigest) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.SHA256
	}
	return m
}

// failedPackagesOf derives every package with at least one observed
// failure from a parsed `go test -json` run: either a per-test Fail
// outcome, or a package-level compile failure (evidence.ParsedRun's own
// FailedPackages, which per-test outcomes alone cannot represent).
func failedPackagesOf(parsed evidence.ParsedRun) []string {
	failed := map[string]bool{}
	for _, p := range parsed.FailedPackages {
		failed[p] = true
	}
	for key, outcome := range parsed.Outcomes {
		if outcome == evidence.OutcomeFail {
			failed[key.Package] = true
		}
	}
	out := make([]string, 0, len(failed))
	for p := range failed {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// sortedDiff returns the elements of a not present in b, sorted.
func sortedDiff(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, x := range b {
		inB[x] = true
	}
	var out []string
	for _, x := range a {
		if !inB[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func appendBoundEvent(ctx context.Context, j *journal.Journal, spec ShadowSpec, baseDigest, candidateDigest string) (journal.Event, error) {
	payload := struct {
		BaseTreeDigest      string `json:"base_tree_digest"`
		CandidateTreeDigest string `json:"candidate_tree_digest"`
	}{baseDigest, candidateDigest}
	raw, err := json.Marshal(payload)
	if err != nil {
		return journal.Event{}, fmt.Errorf("marshal coding.tia_shadow_bound payload: %w", err)
	}
	return j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:     contracts.EventID("ev-" + string(spec.RunID) + "-tia-bound-" + randHex(8)),
		EventType:   "coding.tia_shadow_bound",
		RunID:       spec.RunID,
		EmittedAt:   time.Now().UTC(),
		ActorType:   contracts.ActorSystem,
		ActorID:     "coding-tia",
		PrincipalID: "nexus",
		WorkspaceID: "local",
		ProfileID:   spec.ProfileID,
		AttemptNo:   1,
		Payload:     raw,
		PayloadHash: "recomputed",
	})
}

func appendShadowEvent(ctx context.Context, j *journal.Journal, spec ShadowSpec, changes []impact.FileChange, changedPackages, selected, full []string, fallbackReason string, actualFailed, missed []string, recallEvaluated, recallAccurate bool, parent *contracts.EventID) (journal.Event, error) {
	changedPaths := make([]string, 0, len(changes))
	for _, c := range changes {
		changedPaths = append(changedPaths, c.Path)
	}
	payload := struct {
		ChangedFiles         []string `json:"changed_files"`
		ChangedPackages      []string `json:"changed_packages"`
		SelectedPackages     []string `json:"selected_packages"`
		FullPackages         []string `json:"full_packages"`
		FallbackReason       string   `json:"fallback_reason,omitempty"`
		ActualFailedPackages []string `json:"actual_failed_packages"`
		MissedPackages       []string `json:"missed_packages"`
		RecallEvaluated      bool     `json:"recall_evaluated"`
		RecallAccurate       bool     `json:"recall_accurate"`
	}{changedPaths, changedPackages, selected, full, fallbackReason, actualFailed, missed, recallEvaluated, recallAccurate}
	raw, err := json.Marshal(payload)
	if err != nil {
		return journal.Event{}, fmt.Errorf("marshal coding.tia_shadow payload: %w", err)
	}
	return j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:       contracts.EventID("ev-" + string(spec.RunID) + "-tia-shadow-" + randHex(8)),
		EventType:     "coding.tia_shadow",
		RunID:         spec.RunID,
		ParentEventID: parent,
		EmittedAt:     time.Now().UTC(),
		ActorType:     contracts.ActorSystem,
		ActorID:       "coding-tia",
		PrincipalID:   "nexus",
		WorkspaceID:   "local",
		ProfileID:     spec.ProfileID,
		AttemptNo:     1,
		Payload:       raw,
		PayloadHash:   "recomputed",
	})
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "nexus-tia-fallback"
	}
	return hex.EncodeToString(b)
}

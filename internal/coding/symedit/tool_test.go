//go:build linux

package symedit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/coding/workspace"
	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

// toolCall builds a contracts.ToolCall the same way the real planner does
// (sealed effect/kind copied from the tool's own Spec — never model-
// supplied), for driving Tools()'s InProcFunc handlers directly in tests.
func toolCall(t *testing.T, id contracts.ToolID, args any, profile contracts.ProfileID) contracts.ToolCall {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	specs := Specs()
	s, ok := specs[id]
	if !ok {
		t.Fatalf("no spec registered for %q", id)
	}
	params := contracts.ToolCallParams{
		ToolCallID: contracts.ToolCallID(fmt.Sprintf("tc-%s-%d", id, time.Now().UnixNano())),
		ToolID:     id, Arguments: raw, ArgsSchemaHash: s.ArgsSchemaHash,
		Effect: s.Effect, ExecutionKind: s.ExecutionKind,
		Deadline: time.Now().Add(2 * time.Minute), AttemptNo: 1, ProfileID: profile,
	}
	if s.Effect != contracts.EffectReadOnly {
		idem := "idem-" + string(params.ToolCallID)
		params.IdempotencyKey = &idem
	}
	c, err := contracts.NewToolCall(params)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeRenameFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/tiny\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := []byte("package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Detector: prepare→apply, end-to-end, real gopls + real sandbox + real
// S7, through the ACTUAL registered tool handlers (not Prepare/
// ApplyGoverned called directly) — proves the whole model-facing surface
// works: preview returns a plan_digest, apply with that exact digest
// commits, and the file on disk actually changed.
func TestRenameSymbolToolsEndToEnd(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)

	workspaceRoot := func(p contracts.ProfileID) (string, bool) {
		if p == "work" {
			return src, true
		}
		return "", false
	}
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if prepareResult.Status != contracts.ResultSucceeded || len(prepareResult.Output) != 1 {
		t.Fatalf("unexpected prepare result: %+v", prepareResult)
	}
	previewText := *prepareResult.Output[0].Content
	digest := extractPlanDigest(t, previewText)

	applyCall := toolCall(t, ToolRenameApply, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": digest,
		"touched_files": []string{"main.go"}, "old_name": "Foo",
	}, "work")
	applyResult, err := tools[ToolRenameApply](ctxT(), applyCall)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if applyResult.Status != contracts.ResultSucceeded || applyResult.Commit == nil {
		t.Fatalf("unexpected apply result (expected a commit receipt): %+v", applyResult)
	}

	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := "package main\n\nfunc Bar() int { return 1 }\n\nfunc main() { _ = Bar() }\n"
	if string(got) != want {
		t.Fatalf("file on disk = %q, want %q", got, want)
	}
}

// Detector: apply refuses when plan_digest does not match a fresh
// Prepare's own result — proves the tool never trusts a caller-supplied
// digest, always re-derives and compares.
func TestRenameSymbolApplyRefusesDigestMismatch(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)

	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	applyCall := toolCall(t, ToolRenameApply, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": "not-the-real-digest",
		"touched_files": []string{"main.go"},
	}, "work")
	if _, err := tools[ToolRenameApply](ctxT(), applyCall); err == nil {
		t.Fatal("expected an error: plan_digest does not match a fresh Prepare result")
	}
	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n" {
		t.Fatal("file was modified despite the digest mismatch refusal")
	}
}

// Detector: apply refuses when touched_files does not match a fresh
// preview's own file list — proves the claim visible in the raw-JSON
// approval prompt can never silently diverge from what plan_digest
// actually binds (code-review finding, codex, round 1).
func TestRenameSymbolApplyRefusesTouchedFilesMismatch(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)

	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	digest := extractPlanDigest(t, *prepareResult.Output[0].Content)

	applyCall := toolCall(t, ToolRenameApply, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": digest,
		"touched_files": []string{"main.go", "an-extra-file-not-in-the-real-plan.go"}, "old_name": "Foo",
	}, "work")
	if _, err := tools[ToolRenameApply](ctxT(), applyCall); err == nil {
		t.Fatal("expected an error: touched_files does not match a fresh preview's own file list")
	}
	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n" {
		t.Fatal("file was modified despite the touched_files mismatch refusal")
	}
}

// Detector: apply refuses when old_name does not match a fresh preview's
// own extracted symbol identity — proves the raw-JSON approval prompt
// can never show the wrong "what is being renamed" claim (owner spec:
// docs/PLAN-CODING-TRIO.md's "canonical preview: symbol identity +
// old/new name", code-review finding, codex, round 2).
// Detector (code-review finding, codex, round 4 HIGH, live-reproduced):
// once ANY apply lands OutcomeUnknown (workspace.ErrRollbackIncomplete —
// an incompletely-rolled-back mutation), rename_symbol must disable
// itself for the rest of THIS process's lifetime — a later call must
// never keep operating on a workspace known to be in an unresolved
// state before a restart re-runs buildDaemon's own scanClean gate.
// Uses the applyGoverned test seam (the underlying ErrRollbackIncomplete
// classification is workspace's own already-tested responsibility;
// workspace's own real-drift test seam is unexported and unreachable
// from this package — this isolates exactly what needs testing here:
// the poisoned latch's own wiring).
func TestRenameSymbolApplyDisablesToolAfterOutcomeUnknown(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)
	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	orig := applyGoverned
	t.Cleanup(func() { applyGoverned = orig })
	applyGoverned = func(ctx context.Context, rootFd int, store *sealedstore.Store, plan Plan, grants *s7.Authority, j *journal.Journal, runID contracts.RunID) (workspace.Result, error) {
		return workspace.Result{}, fmt.Errorf("workspace: apply governed: %w", workspace.ErrRollbackIncomplete)
	}

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("first prepare (before poisoning) failed: %v", err)
	}
	digest := extractPlanDigest(t, *prepareResult.Output[0].Content)

	applyCall := toolCall(t, ToolRenameApply, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": digest,
		"touched_files": []string{"main.go"}, "old_name": "Foo",
	}, "work")
	if _, err := tools[ToolRenameApply](ctxT(), applyCall); err == nil {
		t.Fatal("expected the faked ApplyGoverned failure to propagate")
	}

	// The tool must now be disabled — a SUBSEQUENT prepare (which would
	// otherwise succeed against the untouched real fixture) must refuse.
	secondPrepare := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	_, err = tools[ToolRenamePrepare](ctxT(), secondPrepare)
	if err == nil {
		t.Fatal("expected rename_symbol to be disabled after OutcomeUnknown")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected a 'disabled' refusal, got: %v", err)
	}
}

// Detector (code-review finding, codex, round 5 HIGH, live-reproduced):
// a mutation that fully COMMITTED to disk but then failed to durably
// RECORD that outcome (e.g. the EvMutationCommitted append itself
// failed) must ALSO poison the tool — err != nil alone is not the
// signal; result.Committed matters independently of which error is
// returned, since ErrRollbackIncomplete is not the only way NEXUS's own
// S7/journal state can stop reliably reflecting reality.
func TestRenameSymbolApplyDisablesToolAfterCommittedButUnrecordedOutcome(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)
	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	orig := applyGoverned
	t.Cleanup(func() { applyGoverned = orig })
	applyGoverned = func(ctx context.Context, rootFd int, store *sealedstore.Store, plan Plan, grants *s7.Authority, j *journal.Journal, runID contracts.RunID) (workspace.Result, error) {
		// Committed==true but a DIFFERENT error (not ErrRollbackIncomplete)
		// — simulates every file having been written, then the S7/journal
		// bookkeeping around that success itself failing.
		return workspace.Result{Committed: true}, fmt.Errorf("simulated: EvMutationCommitted append failed")
	}

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("first prepare (before poisoning) failed: %v", err)
	}
	digest := extractPlanDigest(t, *prepareResult.Output[0].Content)

	applyCall := toolCall(t, ToolRenameApply, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": digest,
		"touched_files": []string{"main.go"}, "old_name": "Foo",
	}, "work")
	_, applyErr := tools[ToolRenameApply](ctxT(), applyCall)
	if applyErr == nil {
		t.Fatal("expected the faked committed-but-unrecorded failure to propagate")
	}
	if !strings.Contains(applyErr.Error(), "written to disk") {
		t.Fatalf("expected an honest 'written to disk' message, got: %v", applyErr)
	}

	secondPrepare := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	if _, err := tools[ToolRenamePrepare](ctxT(), secondPrepare); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected rename_symbol to be disabled after a committed-but-unrecorded outcome, got: %v", err)
	}
}

// Detector (code-review finding, codex, round 5 HIGH, live-reproduced
// with a deterministic overlay): two concurrent rename_symbol_apply
// calls must never interleave — one must fully finish (including its
// own poisoning decision) before the other's mutation call begins.
// Without applyMu, both could pass the poisoned check before either
// finished, letting a second apply write AFTER the first has already
// poisoned the closure.
func TestRenameSymbolApplySerializesConcurrentCalls(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)
	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	orig := applyGoverned
	t.Cleanup(func() { applyGoverned = orig })
	var active atomic.Int32
	var sawOverlap atomic.Bool
	applyGoverned = func(ctx context.Context, rootFd int, store *sealedstore.Store, plan Plan, grants *s7.Authority, j *journal.Journal, runID contracts.RunID) (workspace.Result, error) {
		if active.Add(1) > 1 {
			sawOverlap.Store(true)
		}
		defer active.Add(-1)
		time.Sleep(50 * time.Millisecond) // widen the window a race would need
		return workspace.Result{}, fmt.Errorf("workspace: apply governed: %w", workspace.ErrRollbackIncomplete)
	}

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	digest := extractPlanDigest(t, *prepareResult.Output[0].Content)
	applyArgs := map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": digest,
		"touched_files": []string{"main.go"}, "old_name": "Foo",
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call := toolCall(t, ToolRenameApply, applyArgs, "work")
			tools[ToolRenameApply](ctxT(), call)
		}()
	}
	wg.Wait()

	if sawOverlap.Load() {
		t.Fatal("two concurrent applies entered applyGoverned simultaneously — applyMu did not serialize them")
	}
}

func TestRenameSymbolApplyRefusesOldNameMismatch(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)

	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	digest := extractPlanDigest(t, *prepareResult.Output[0].Content)

	applyCall := toolCall(t, ToolRenameApply, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar", "plan_digest": digest,
		"touched_files": []string{"main.go"}, "old_name": "NotTheRealOldName",
	}, "work")
	if _, err := tools[ToolRenameApply](ctxT(), applyCall); err == nil {
		t.Fatal("expected an error: old_name does not match a fresh preview's own symbol identity")
	}
	got, err := os.ReadFile(filepath.Join(src, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package main\n\nfunc Foo() int { return 1 }\n\nfunc main() { _ = Foo() }\n" {
		t.Fatal("file was modified despite the old_name mismatch refusal")
	}
}

// Detector: rename_symbol_prepare's preview text names the ORIGINAL
// symbol identity, not just the new name (owner spec: docs/
// PLAN-CODING-TRIO.md's "canonical preview: symbol identity + old/new
// name" — code-review finding, codex, round 2: the preview previously
// showed only the new name).
func TestRenameSymbolPreparePreviewNamesOldIdentity(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)
	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	previewText := *prepareResult.Output[0].Content
	if !strings.Contains(previewText, "Foo") {
		t.Fatalf("preview does not name the original symbol identity: %q", previewText)
	}
}

// Detector: a profile with no configured workspace root gets refused
// before anything else runs — no gopls session, no filesystem access.
func TestRenameSymbolPrepareRefusesUnconfiguredProfile(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	workspaceRoot := func(contracts.ProfileID) (string, bool) { return "", false }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "private")
	if _, err := tools[ToolRenamePrepare](ctxT(), prepareCall); err == nil {
		t.Fatal("expected an error: profile has no configured coding workspace")
	}
}

// Detector: the Specs() registration declares the EXACT ExecutionKind
// rename_symbol needs — closing the author-time mislabeling risk piece
// 1's own review identified (docs/SECTION-MAP.md's piece-2 obligation:
// "a dedicated test will assert Specs()[...].ExecutionKind ==
// ExecInProcessGoverned exactly").
func TestRenameSymbolSpecsDeclareExecInProcessGoverned(t *testing.T) {
	specs := Specs()
	for _, id := range []contracts.ToolID{ToolRenamePrepare, ToolRenameApply} {
		s, ok := specs[id]
		if !ok {
			t.Fatalf("no spec registered for %q", id)
		}
		if s.ExecutionKind != contracts.ExecInProcessGoverned {
			t.Fatalf("%s: ExecutionKind = %v, want ExecInProcessGoverned", id, s.ExecutionKind)
		}
	}
}

// Detector: rename_symbol_prepare is ALLOW (read-only, no approval
// needed) and rename_symbol_apply is ASK (mutates the workspace) —
// asserted against the actual Rules()/Specs() registration, not assumed.
func TestRenameSymbolRulesMatchEffectClass(t *testing.T) {
	rules := Rules()
	specs := Specs()
	if rules[ToolRenamePrepare] != effectpath.DecisionAllow {
		t.Fatalf("rename_symbol_prepare rule = %v, want ALLOW", rules[ToolRenamePrepare])
	}
	if rules[ToolRenameApply] != effectpath.DecisionAsk {
		t.Fatalf("rename_symbol_apply rule = %v, want ASK", rules[ToolRenameApply])
	}
	if specs[ToolRenamePrepare].Effect != contracts.EffectReadOnly {
		t.Fatalf("rename_symbol_prepare effect = %v, want EffectReadOnly", specs[ToolRenamePrepare].Effect)
	}
	if specs[ToolRenameApply].Effect == contracts.EffectReadOnly {
		t.Fatal("rename_symbol_apply must not be EffectReadOnly — it writes files")
	}
}

// Detector: every symedit tool output block is UNTRUSTED_EXTERNAL, never
// TOOL_TRUSTED — the preview text embeds touchedFiles(plan), filenames
// read from the TARGET repository's own tree, not authored by the user
// or this tool (code-review finding, codex, round 1: an unfenced
// TOOL_TRUSTED block would let a repository-controlled filename inject
// instruction-like text past the assembler's own trust fence).
func TestRenameSymbolToolOutputsAreUntrustedExternal(t *testing.T) {
	goBin := realGoBinary(t)
	goplsBin := realGoplsBinary(t)
	backend, report := testBackend(t)
	j := testDurableJournal(t)
	grants := testDurableGrants(t, j)
	store := testStore(t)

	src := t.TempDir()
	writeRenameFixture(t, src)
	workspaceRoot := func(p contracts.ProfileID) (string, bool) { return src, p == "work" }
	tools := Tools(workspaceRoot, goBin, goplsBin, store, backend, report, grants, j)

	prepareCall := toolCall(t, ToolRenamePrepare, map[string]any{
		"file": "main.go", "line": 2, "character": 5, "new_name": "Bar",
	}, "work")
	prepareResult, err := tools[ToolRenamePrepare](ctxT(), prepareCall)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if len(prepareResult.Output) != 1 || prepareResult.Output[0].Trust != contracts.TrustUntrustedExternal {
		t.Fatalf("prepare output trust = %+v, want exactly one UNTRUSTED_EXTERNAL block", prepareResult.Output)
	}
}

// extractPlanDigest pulls "plan_digest: <hex>" out of the preview text —
// mirroring exactly how a real model would parse the tool's own output
// to construct the follow-up apply call.
func extractPlanDigest(t *testing.T, text string) string {
	t.Helper()
	_, after, found := strings.Cut(text, "plan_digest: ")
	if !found {
		t.Fatalf("preview text has no plan_digest line: %q", text)
	}
	rest, _, _ := strings.Cut(after, "\n")
	if rest == "" {
		t.Fatalf("empty plan_digest extracted from: %q", text)
	}
	return rest
}

//go:build linux

package evidence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/kernel/checker"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
)

// CaptureSpec describes one proof-of-done capture: a base tree (before a
// change) and a candidate tree (after it), each run through `go test
// -json` inside Slice 0's governed sandbox.
type CaptureSpec struct {
	ContractID string
	WorkerID   string // the principal being JUDGED — never used as the evidence Producer (checker != worker)

	BaseDir      string
	CandidateDir string
	GoBinary     string
	// TestArgs defaults to {"test", "-json", "./..."} — "-C <dir>" is
	// added internally (it must be the first flag, and Slice 0 always
	// stages the snapshot at /work/src).
	TestArgs []string
	Timeout  time.Duration

	BaseOperationID      contracts.OperationID
	CandidateOperationID contracts.OperationID
	BaseTargetID         contracts.TargetID
	CandidateTargetID    contracts.TargetID
	RunID                contracts.RunID // ONE run id for the whole capture session — journal.Envelope.Sequence is the per-run causal order
	ProfileID            contracts.ProfileID

	// Slice 1 produces BEHAVIORAL evidence only (code-review finding,
	// codex: a caller-supplied StructuralPassed list, with zero
	// independent verification, let a REFACTOR-mode contract grade PASS
	// against a run with ZERO test transitions at all — reproduced with
	// `go test -json -run '^$'`, which exits 0 and emits no per-test
	// events). REFACTOR-mode grading needs a typed, independently
	// verified structural-postcondition receipt bound to the same
	// contract/candidate digest — that is Slice 3 symedit's job, not
	// built yet; until then, evidence this package produces always
	// leaves StructuralPassed empty, so a REFACTOR-mode criterion
	// correctly fails closed by construction (checker.go's own
	// validate() already requires a non-empty RequiredStructural for
	// REFACTOR mode, and gradeCodingProof requires every one of those
	// names to appear in StructuralPassed).
}

// Manifest is the human/audit-facing record of one capture — every
// digest and journal event a reviewer needs to independently verify the
// claimed proof, without re-deriving it from the checker.Evidence alone.
type Manifest struct {
	ContractID               string
	BaseTreeDigest           string
	CandidateTreeDigest      string
	BaseToolchainDigest      string
	CandidateToolchainDigest string
	Diff                     TestDiff
	BoundEvent               journal.Event
	BaseRunEvent             journal.Event
	CandidateRunEvent        journal.Event
	CapturedEvent            journal.Event
}

// Capture runs the base and candidate trees through `go test -json`
// (Slice 0's governed runner.Run), classifies every test's transition
// per invariant 8, and journals a causally-chained event sequence:
// coding.evidence_bound (tree identities declared BEFORE any test runs)
// -> the base run's own coding.run event (parented to bound) -> the
// candidate run's own coding.run event (parented to the base run) ->
// coding.evidence_captured (parented to the candidate run). Reuses
// runner.Run's own journal event for each run rather than duplicating a
// second event type for the same fact.
//
// Does NOT call checker.Grade itself — Capture produces evidence, it
// does not own the AcceptanceContract a later caller grades against
// (Slice 1 depends only on existing runner/journal/checker machinery,
// never on a contract that belongs to whichever slice consumes this).
//
// Fails closed, before producing any evidence: if either run's captured
// output was truncated (sandbox.Process.Truncated); if either run's OWN
// measured snapshot digest disagrees with the pinned snapshot's digest
// (a TOCTOU between snapshot and execution); if the candidate run's own
// `go test` process exited nonzero (code-review finding, codex: without
// this, a candidate package that fails to COMPILE — with zero per-test
// events — was invisible to classification and could still grade PASS
// on whatever OTHER tests happened to run); or if the base and candidate
// runs resolved DIFFERENT toolchain digests (a toolchain drift between
// the two runs could produce a FAIL_TO_PASS transition not actually
// attributable to the source change).
//
// Snapshots BaseDir/CandidateDir itself ONCE, up front, and keeps that
// private copy alive for the ENTIRE capture (passed to runner.Run as
// SourceDir instead of the live directory) — code-review finding, codex:
// hashing the live directory once, cleaning up, then letting runner.Run
// independently re-snapshot the STILL-LIVE directory only detects a
// mutation that happened before Run's own copy; it does nothing about a
// mutation to the live directory WHILE Run is executing. Sandboxing
// Capture's own already-private, already-hashed copy instead closes that
// window entirely — nothing about the live directory's later state can
// affect what was actually tested.
func Capture(ctx context.Context, backend sandbox.Backend, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal, spec CaptureSpec) (Manifest, checker.Evidence, error) {
	if spec.ContractID == "" || spec.WorkerID == "" {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: ContractID and WorkerID are both required")
	}
	if spec.BaseDir == "" || spec.CandidateDir == "" || spec.GoBinary == "" {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: BaseDir, CandidateDir, and GoBinary are all required")
	}
	if !spec.BaseOperationID.Valid() || !spec.CandidateOperationID.Valid() ||
		!spec.BaseTargetID.Valid() || !spec.CandidateTargetID.Valid() ||
		!spec.RunID.Valid() || !spec.ProfileID.Valid() {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: all operation/target/run/profile identifiers are required (fail closed)")
	}
	if j == nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: a journal is required — evidence with no durable record proves nothing")
	}

	testArgs := spec.TestArgs
	if len(testArgs) == 0 {
		testArgs = []string{"-json", "./..."}
	}
	fullArgs := append([]string{"test", "-C", "/work/src"}, testArgs...)

	baseSnap, cleanupBase, err := runner.CreateSnapshot(spec.BaseDir)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: hashing base tree: %w", err)
	}
	defer cleanupBase()
	candidateSnap, cleanupCand, err := runner.CreateSnapshot(spec.CandidateDir)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: hashing candidate tree: %w", err)
	}
	defer cleanupCand()

	boundEvent, err := appendBoundEvent(ctx, j, spec, baseSnap.Digest, candidateSnap.Digest)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: %w", err)
	}

	baseParent := boundEvent.Envelope.EventID
	baseResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir: baseSnap.Dir, Args: fullArgs, GoBinary: spec.GoBinary, Timeout: spec.Timeout,
		OperationID: spec.BaseOperationID, TargetID: spec.BaseTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
		ParentEventID: &baseParent,
	})
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base run: %w", err)
	}
	if baseResult.Truncated {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base run's captured output was truncated — refusing to grade from an incomplete stream")
	}
	if baseResult.SnapshotDigest != baseSnap.Digest {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base tree digest changed between snapshot (%s) and execution (%s) — refusing (fail closed)",
			baseSnap.Digest, baseResult.SnapshotDigest)
	}
	if baseResult.PostSnapshotDigest != baseResult.SnapshotDigest {
		// code-review finding, codex: the test itself could mutate its
		// own /work/src during execution (e.g. rewriting a fixture) —
		// the plan's own "hash again after" requirement, not yet closed
		// by the pre-run freeze alone.
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base tree was mutated DURING the run (pre-run %s, post-run %q) — refusing (fail closed)",
			baseResult.SnapshotDigest, baseResult.PostSnapshotDigest)
	}
	baseParsed, err := ParseTestJSON(baseResult.Stdout)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base run: %w", err)
	}

	candidateParent := baseResult.JournalEvent.Envelope.EventID
	candidateResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir: candidateSnap.Dir, Args: fullArgs, GoBinary: spec.GoBinary, Timeout: spec.Timeout,
		OperationID: spec.CandidateOperationID, TargetID: spec.CandidateTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
		ParentEventID: &candidateParent,
	})
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run: %w", err)
	}
	if candidateResult.Truncated {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run's captured output was truncated — refusing to grade from an incomplete stream")
	}
	if candidateResult.SnapshotDigest != candidateSnap.Digest {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate tree digest changed between snapshot (%s) and execution (%s) — refusing (fail closed)",
			candidateSnap.Digest, candidateResult.SnapshotDigest)
	}
	if candidateResult.PostSnapshotDigest != candidateResult.SnapshotDigest {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate tree was mutated DURING the run (pre-run %s, post-run %q) — refusing (fail closed)",
			candidateResult.SnapshotDigest, candidateResult.PostSnapshotDigest)
	}
	if baseResult.ToolchainDigest != candidateResult.ToolchainDigest {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base and candidate runs resolved DIFFERENT toolchains (%s vs %s) — refusing, a FAIL_TO_PASS transition would not be attributable solely to the source change (fail closed)",
			baseResult.ToolchainDigest, candidateResult.ToolchainDigest)
	}
	candidateParsed, err := ParseTestJSON(candidateResult.Stdout)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run: %w", err)
	}
	if !candidateResult.ExitOK && len(candidateParsed.FailedPackages) == 0 {
		// The candidate's own `go test` exited nonzero for a reason
		// Classify's per-test outcomes may not fully explain (e.g. a
		// vet failure, a panic, or a build tag mismatch) — refuse rather
		// than silently grade whatever per-test outcomes DID happen to
		// parse. An ORDINARY per-test failure is normal, expected data
		// (that's what FailToPass/PassToFail already classify); this
		// check is for exit failures classification cannot explain.
		if len(candidateParsed.Outcomes) == 0 {
			return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run exited nonzero with zero parsed test outcomes — refusing (fail closed)")
		}
	}

	diff := Classify(baseParsed.Outcomes, candidateParsed.Outcomes)

	evidence := checker.Evidence{
		Contract: spec.ContractID,
		Producer: "coding-evidence-capture",
		Coding: &checker.CodingProofEvidence{
			FailToPass:     diff.FailToPass,
			PassToPass:     diff.PassToPass,
			PassToFail:     diff.PassToFail,
			PassToSkip:     diff.PassToSkip,
			NewFail:        diff.NewFail,
			Missing:        diff.Missing,
			FailedPackages: candidateParsed.FailedPackages,
		},
	}

	capturedParent := candidateResult.JournalEvent.Envelope.EventID
	capturedEvent, err := appendCapturedEvent(ctx, j, spec, diff, candidateParsed.FailedPackages, &capturedParent)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: %w", err)
	}

	return Manifest{
		ContractID:               spec.ContractID,
		BaseTreeDigest:           baseSnap.Digest,
		CandidateTreeDigest:      candidateSnap.Digest,
		BaseToolchainDigest:      baseResult.ToolchainDigest,
		CandidateToolchainDigest: candidateResult.ToolchainDigest,
		Diff:                     diff,
		BoundEvent:               boundEvent,
		BaseRunEvent:             baseResult.JournalEvent,
		CandidateRunEvent:        candidateResult.JournalEvent,
		CapturedEvent:            capturedEvent,
	}, evidence, nil
}

func appendBoundEvent(ctx context.Context, j *journal.Journal, spec CaptureSpec, baseDigest, candidateDigest string) (journal.Event, error) {
	payload := struct {
		ContractID          string `json:"contract_id"`
		WorkerID            string `json:"worker_id"`
		BaseTreeDigest      string `json:"base_tree_digest"`
		CandidateTreeDigest string `json:"candidate_tree_digest"`
	}{spec.ContractID, spec.WorkerID, baseDigest, candidateDigest}
	raw, err := json.Marshal(payload)
	if err != nil {
		return journal.Event{}, fmt.Errorf("marshal coding.evidence_bound payload: %w", err)
	}
	return j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:     contracts.EventID("ev-" + string(spec.RunID) + "-bound-" + randHex(8)),
		EventType:   "coding.evidence_bound",
		RunID:       spec.RunID,
		EmittedAt:   time.Now().UTC(),
		ActorType:   contracts.ActorSystem,
		ActorID:     "coding-evidence",
		PrincipalID: "nexus",
		WorkspaceID: "local",
		ProfileID:   spec.ProfileID,
		AttemptNo:   1,
		Payload:     raw,
		PayloadHash: "recomputed",
	})
}

// appendCapturedEvent journals the classified diff as "coding.evidence_captured"
// — deliberately NOT named "completed" (code-review finding, codex, round
// 2: the plan's own wording, "the completion event is appended only
// after checker.Grade," means a "completed" event should carry a
// deterministic verdict; Capture has no AcceptanceContract to grade
// against, so what it actually produces is captured, unjudged evidence).
// Grading, and a future "coding.evidence_graded" event carrying the
// verdict, belongs to whichever later caller (Slice 3's not-yet-built
// symedit orchestration) owns the contract.
func appendCapturedEvent(ctx context.Context, j *journal.Journal, spec CaptureSpec, diff TestDiff, failedPackages []string, parent *contracts.EventID) (journal.Event, error) {
	payload := struct {
		ContractID     string   `json:"contract_id"`
		FailToPass     []string `json:"fail_to_pass"`
		PassToPass     []string `json:"pass_to_pass"`
		PassToFail     []string `json:"pass_to_fail"`
		FailToFail     []string `json:"fail_to_fail"`
		PassToSkip     []string `json:"pass_to_skip"`
		NewPass        []string `json:"new_pass"`
		NewFail        []string `json:"new_fail"`
		Missing        []string `json:"missing"`
		FailedPackages []string `json:"failed_packages"`
	}{
		spec.ContractID, diff.FailToPass, diff.PassToPass, diff.PassToFail,
		diff.FailToFail, diff.PassToSkip, diff.NewPass, diff.NewFail, diff.Missing, failedPackages,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return journal.Event{}, fmt.Errorf("marshal coding.evidence_captured payload: %w", err)
	}
	return j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:       contracts.EventID("ev-" + string(spec.RunID) + "-captured-" + randHex(8)),
		EventType:     "coding.evidence_captured",
		RunID:         spec.RunID,
		ParentEventID: parent,
		EmittedAt:     time.Now().UTC(),
		ActorType:     contracts.ActorSystem,
		ActorID:       "coding-evidence",
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
		return "nexus-evidence-fallback"
	}
	return hex.EncodeToString(b)
}

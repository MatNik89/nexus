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

	// Mode/StructuralPassed thread straight into the produced
	// checker.CodingProofEvidence — Capture does not verify structural
	// postconditions itself (that is Slice 3 symedit's job); it only
	// carries what the caller already declared as having held.
	Mode             checker.CodingProofMode
	StructuralPassed []string
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
	CompletedEvent           journal.Event
}

// Capture runs the base and candidate trees through `go test -json`
// (Slice 0's governed runner.Run), classifies every test's transition
// per invariant 8, and journals a causally-chained event sequence:
// coding.evidence_bound (tree identities declared BEFORE any test runs)
// -> the base run's own coding.run event (parented to bound) -> the
// candidate run's own coding.run event (parented to the base run) ->
// coding.evidence_completed (parented to the candidate run). Reuses
// runner.Run's own journal event for each run rather than duplicating a
// second event type for the same fact.
//
// Does NOT call checker.Grade itself — Capture produces evidence, it
// does not own the AcceptanceContract a later caller grades against
// (Slice 1 depends only on existing runner/journal/checker machinery,
// never on a contract that belongs to whichever slice consumes this).
//
// Fails closed, before producing any evidence, if either run's captured
// output was truncated (sandbox.Process.Truncated) or if a run's
// OWN measured snapshot digest disagrees with the digest declared in the
// bound event (a TOCTOU between declaration and execution) — an
// evidence bundle built from an incomplete or substituted tree is never
// silently accepted.
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

	baseDigest, cleanupBase, err := runner.CreateSnapshot(spec.BaseDir)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: hashing base tree: %w", err)
	}
	cleanupBase()
	candidateDigest, cleanupCand, err := runner.CreateSnapshot(spec.CandidateDir)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: hashing candidate tree: %w", err)
	}
	cleanupCand()

	boundEvent, err := appendBoundEvent(ctx, j, spec, baseDigest.Digest, candidateDigest.Digest)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: %w", err)
	}

	baseParent := boundEvent.Envelope.EventID
	baseResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir: spec.BaseDir, Args: fullArgs, GoBinary: spec.GoBinary, Timeout: spec.Timeout,
		OperationID: spec.BaseOperationID, TargetID: spec.BaseTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
		ParentEventID: &baseParent,
	})
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base run: %w", err)
	}
	if baseResult.Truncated {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base run's captured output was truncated — refusing to grade from an incomplete stream")
	}
	if baseResult.SnapshotDigest != baseDigest.Digest {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base tree digest changed between declaration (%s) and execution (%s) — refusing (fail closed)",
			baseDigest.Digest, baseResult.SnapshotDigest)
	}
	baseOutcomes, err := ParseTestJSON(baseResult.Stdout)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: base run: %w", err)
	}

	candidateParent := baseResult.JournalEvent.Envelope.EventID
	candidateResult, err := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir: spec.CandidateDir, Args: fullArgs, GoBinary: spec.GoBinary, Timeout: spec.Timeout,
		OperationID: spec.CandidateOperationID, TargetID: spec.CandidateTargetID, RunID: spec.RunID, ProfileID: spec.ProfileID,
		ParentEventID: &candidateParent,
	})
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run: %w", err)
	}
	if candidateResult.Truncated {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run's captured output was truncated — refusing to grade from an incomplete stream")
	}
	if candidateResult.SnapshotDigest != candidateDigest.Digest {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate tree digest changed between declaration (%s) and execution (%s) — refusing (fail closed)",
			candidateDigest.Digest, candidateResult.SnapshotDigest)
	}
	candidateOutcomes, err := ParseTestJSON(candidateResult.Stdout)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: candidate run: %w", err)
	}

	diff := Classify(baseOutcomes, candidateOutcomes)

	evidence := checker.Evidence{
		Contract: spec.ContractID,
		Producer: "coding-evidence-capture",
		Coding: &checker.CodingProofEvidence{
			FailToPass:       diff.FailToPass,
			PassToPass:       diff.PassToPass,
			PassToFail:       diff.PassToFail,
			Missing:          diff.Missing,
			StructuralPassed: spec.StructuralPassed,
		},
	}

	completedParent := candidateResult.JournalEvent.Envelope.EventID
	completedEvent, err := appendCompletedEvent(ctx, j, spec, diff, &completedParent)
	if err != nil {
		return Manifest{}, checker.Evidence{}, fmt.Errorf("evidence: %w", err)
	}

	return Manifest{
		ContractID:               spec.ContractID,
		BaseTreeDigest:           baseDigest.Digest,
		CandidateTreeDigest:      candidateDigest.Digest,
		BaseToolchainDigest:      baseResult.ToolchainDigest,
		CandidateToolchainDigest: candidateResult.ToolchainDigest,
		Diff:                     diff,
		BoundEvent:               boundEvent,
		BaseRunEvent:             baseResult.JournalEvent,
		CandidateRunEvent:        candidateResult.JournalEvent,
		CompletedEvent:           completedEvent,
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

func appendCompletedEvent(ctx context.Context, j *journal.Journal, spec CaptureSpec, diff TestDiff, parent *contracts.EventID) (journal.Event, error) {
	payload := struct {
		ContractID string   `json:"contract_id"`
		FailToPass []string `json:"fail_to_pass"`
		PassToPass []string `json:"pass_to_pass"`
		PassToFail []string `json:"pass_to_fail"`
		FailToFail []string `json:"fail_to_fail"`
		NewPass    []string `json:"new_pass"`
		NewFail    []string `json:"new_fail"`
		Missing    []string `json:"missing"`
	}{
		spec.ContractID, diff.FailToPass, diff.PassToPass, diff.PassToFail,
		diff.FailToFail, diff.NewPass, diff.NewFail, diff.Missing,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return journal.Event{}, fmt.Errorf("marshal coding.evidence_completed payload: %w", err)
	}
	return j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:       contracts.EventID("ev-" + string(spec.RunID) + "-completed-" + randHex(8)),
		EventType:     "coding.evidence_completed",
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

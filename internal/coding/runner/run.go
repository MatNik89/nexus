//go:build linux

package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
)

// PolicyCodingRun governs one physical coding-run subprocess attempt (a
// single `go list`/`go build`/`go test` launch) — non-durable, single
// attempt, read-only effect class.
//
// Plan-review round 1 (my own research + agy + kilo, independently
// converged): a coding-run operates entirely on a disposable snapshot
// (CreateSnapshot), never the live workspace, so a crash mid-run has no
// UNKNOWN outcome to reconcile — this needs invariant 2's ordinary
// retry/deadline/cancel governance, not S7's durable lifecycle (that is
// Slice 3's `workspace.Apply`, which genuinely mutates the live tree and
// therefore genuinely has an UNKNOWN-outcome-on-crash problem this
// doesn't).
var PolicyCodingRun = s7.Policy{
	EffectClass: contracts.EffectReadOnly,
	MaxAttempts: 1,
}

// RunSpec describes one coding-run.
type RunSpec struct {
	// SourceDir is the live workspace tree to snapshot — read ONLY
	// (CreateSnapshot copies it; the sandbox never sees SourceDir
	// itself, only the disposable copy).
	SourceDir string
	// Args are the `go` subcommand and its arguments, e.g.
	// {"list", "-json", "-test", "./..."} or {"test", "./..."}.
	Args []string
	// GoBinary resolves the toolchain: an absolute path, or a name
	// resolvable on PATH.
	GoBinary string
	Timeout  time.Duration

	// Identifying context for the S7 grant and the coding.run journal
	// event. All required (fail closed).
	OperationID contracts.OperationID
	TargetID    contracts.TargetID
	RunID       contracts.RunID
	ProfileID   contracts.ProfileID
}

// RunResult is the outcome of one coding-run.
type RunResult struct {
	ExitOK          bool // the `go` subprocess itself exited 0
	Output          string
	SnapshotDigest  string
	ToolchainDigest string
	PolicyHash      string
}

// Run executes one governed coding-run: snapshot the source tree,
// resolve+pin the toolchain, compile and launch it inside
// internal/sandbox's bwrap boundary under a non-durable S7 grant, and
// emit a coding.run journal event with the outcome.
//
// Deliberately bypasses internal/exectool and internal/kernel/effectpath
// entirely (plan-review round 1, §4, explicitly revising the plan's
// earlier "route through EffectPath" wording): both are shaped for
// MODEL-VISIBLE, human-approved tool calls (exectool.New seeds
// DecisionAsk unconditionally) — a coding-run is an internal initiative
// no human approves per attempt, and EffectPath.RunTool's ExecProcess
// dispatch goes through exactly ONE bound SandboxBackend regardless of
// ToolID, which is exectool.Adapter today. report is the caller's own,
// already-measured sandbox.ProbeReport (sandbox.Backend.Probe is
// expensive — a live bwrap capability + canary check — so it is measured
// once by the caller and reused across runs, mirroring
// internal/sandbox's own test-suite convention).
func Run(ctx context.Context, backend sandbox.Backend, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal, spec RunSpec) (RunResult, error) {
	if spec.GoBinary == "" {
		return RunResult{}, fmt.Errorf("runner: GoBinary is required")
	}
	if !spec.OperationID.Valid() || !spec.TargetID.Valid() || !spec.RunID.Valid() || !spec.ProfileID.Valid() {
		return RunResult{}, fmt.Errorf("runner: OperationID/TargetID/RunID/ProfileID are all required (fail closed)")
	}

	pin, err := ResolveToolchain(spec.GoBinary)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}

	// ONE shared sandbox WorkDir laid out as src/ (the snapshot) and
	// gocache/ siblings — sandbox.Spec has only ONE RW bind point, and
	// the plan requires GOCACHE/GOTMPDIR OUTSIDE the snapshot's source
	// tree (never polluting a later before/after tree-digest comparison
	// with the compiler's own cache writes).
	scratchRoot, err := os.MkdirTemp("", "nexus-coding-run-*")
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchRoot)
	srcDir := filepath.Join(scratchRoot, "src")
	gocacheDir := filepath.Join(scratchRoot, "gocache")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}
	if err := os.Mkdir(gocacheDir, 0o700); err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}
	snapDigest, err := copyTreeInto(spec.SourceDir, srcDir)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}

	// sandbox.Spec has only ONE WorkDir bind point (mapped to /work
	// inside the sandbox); scratchRoot itself is what's bound, so both
	// src/ and gocache/ are visible in-sandbox as siblings under /work —
	// GOCACHE/GOTMPDIR must therefore be set to their IN-SANDBOX paths
	// (/work/gocache), never the host paths used to create them above
	// (the sandboxed process cannot resolve a host path it was never
	// given visibility into).
	pin.Env["GOCACHE"] = "/work/gocache"
	pin.Env["GOTMPDIR"] = "/work/gocache"

	sbSpec := sandbox.Spec{
		Target:       spec.GoBinary,
		Args:         append([]string{}, spec.Args...),
		WorkDir:      scratchRoot,
		Timeout:      spec.Timeout,
		ExtraROBinds: pin.ROBinds,
		ExtraEnv:     pin.Env,
	}
	policy, err := backend.Compile(ctx, sbSpec, report)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: compile: %w", err)
	}

	if err := grants.Begin(spec.OperationID, spec.TargetID, PolicyCodingRun); err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}
	grant, err := grants.Next(spec.OperationID, nil)
	if err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}
	if err := grants.Consume(grant); err != nil {
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}

	deadline := time.Now().Add(spec.Timeout)
	if spec.Timeout <= 0 {
		deadline = time.Now().Add(10 * time.Minute) // sandbox.Compile's own ceiling
	}
	execCtx, cancelExec, err := grants.AttemptContext(ctx, spec.OperationID, deadline)
	if err != nil {
		reportOutcome(grants, spec.OperationID, s7.OutcomeUnknown, s7.CodeLocalRefused)
		return RunResult{}, fmt.Errorf("runner: %w", err)
	}

	proc, launchErr := backend.Launch(execCtx, policy)
	if launchErr != nil {
		cancelExec()
		reportOutcome(grants, spec.OperationID, s7.OutcomeFailedTerminal, s7.CodeLocalRefused)
		return RunResult{}, journalRunEvent(ctx, j, spec, RunResult{PolicyHash: policy.PolicyHash(), SnapshotDigest: snapDigest, ToolchainDigest: pin.HashDigest}, fmt.Errorf("launch: %w", launchErr))
	}
	waitErr := proc.Wait()
	execCause := context.Cause(execCtx)
	cancelExec()
	output := proc.Output()
	proc.Close()

	result := RunResult{
		ExitOK:          waitErr == nil,
		Output:          output,
		SnapshotDigest:  snapDigest,
		ToolchainDigest: pin.HashDigest,
		PolicyHash:      policy.PolicyHash(),
	}

	if waitErr != nil && execCause != nil {
		// Killed by ITS OWN deadline/cancel — the attempt itself never
		// completed; nothing durable happened downstream of it, so this
		// is CANCELLED, not a failed attempt (mirrors effectpath's own
		// classification for a read-only call killed by its own
		// deadline).
		grants.Cancel(spec.OperationID, nil)
		return result, journalRunEvent(ctx, j, spec, result, fmt.Errorf("coding-run cancelled by its own deadline: %w", waitErr))
	}

	// The SANDBOXED PROCESS's own exit code (result.ExitOK) is DATA, not
	// an S7 attempt failure — `go test` exiting nonzero on test failures
	// is an entirely normal, successfully-completed attempt. S7 only
	// tracks whether the governed ATTEMPT itself (launch, run to
	// completion inside the sandbox boundary) succeeded.
	if _, err := attestOrOutcome(ctx, backend, proc, policy, grants, spec.OperationID); err != nil {
		return result, journalRunEvent(ctx, j, spec, result, fmt.Errorf("attest: %w", err))
	}
	if err := grants.Report(spec.OperationID, s7.OutcomeSucceeded, "", nil); err != nil {
		return result, fmt.Errorf("runner: %w", err)
	}
	return result, journalRunEvent(ctx, j, spec, result, nil)
}

// attestOrOutcome verifies the attestation and, on failure, reports the
// S7 outcome as UNKNOWN (an effectful attempt without a valid receipt is
// never silently trusted or retried — E9) before returning the error.
// A read-only coding-run has nothing durable at stake, but the
// attestation itself still proves the process ran under the EXACT
// compiled policy, not a substituted one.
func attestOrOutcome(ctx context.Context, backend sandbox.Backend, proc *sandbox.Process, policy sandbox.CompiledPolicy, grants *s7.Authority, op contracts.OperationID) (sandbox.Attestation, error) {
	att, err := backend.Attest(ctx, proc, policy)
	if err != nil {
		reportOutcome(grants, op, s7.OutcomeUnknown, s7.CodeLocalRefused)
		return sandbox.Attestation{}, err
	}
	return att, nil
}

// reportOutcome reports best-effort — a Report failure here is already
// downstream of a failure being reported and does not change the
// caller's own error, only surfaces via the operation's own state if the
// caller inspects it later.
func reportOutcome(grants *s7.Authority, op contracts.OperationID, outcome s7.Outcome, code string) {
	_ = grants.Report(op, outcome, code, nil)
}

// journalRunEvent emits one coding.run event recording the run's
// outcome — this package's OWN closed event vocabulary (plan-review
// round 1, kilo: "not a borrowed tool-call event, the correct audit
// record"), never routed through EffectPath's tool-call event shape.
func journalRunEvent(ctx context.Context, j *journal.Journal, spec RunSpec, result RunResult, runErr error) error {
	if j == nil {
		return runErr
	}
	payload := struct {
		Args            []string `json:"args"`
		ExitOK          bool     `json:"exit_ok"`
		SnapshotDigest  string   `json:"snapshot_digest"`
		ToolchainDigest string   `json:"toolchain_digest"`
		PolicyHash      string   `json:"policy_hash"`
		Error           string   `json:"error,omitempty"`
	}{
		Args: spec.Args, ExitOK: result.ExitOK,
		SnapshotDigest: result.SnapshotDigest, ToolchainDigest: result.ToolchainDigest,
		PolicyHash: result.PolicyHash,
	}
	if runErr != nil {
		payload.Error = runErr.Error()
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("runner: marshal coding.run payload: %w", marshalErr)
	}
	_, appendErr := j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:     contracts.EventID("ev-" + string(spec.OperationID) + "-" + randHex(8)),
		EventType:   "coding.run",
		RunID:       spec.RunID,
		EmittedAt:   time.Now().UTC(),
		ActorType:   contracts.ActorSystem,
		ActorID:     "coding-runner",
		PrincipalID: "nexus",
		WorkspaceID: "local",
		ProfileID:   spec.ProfileID,
		AttemptNo:   1,
		Payload:     raw,
		PayloadHash: "recomputed",
	})
	if appendErr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("runner: journal coding.run: %w", appendErr)
	}
	return runErr
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(b)
}

//go:build linux

// Package exectool is the T26 owner: the ONE process-executing tool on
// the effect-path. It adapts the S6.2 sandbox.Backend behind the
// effectpath.SandboxBackend seam — ExecutionKind=ExecProcess is SEALED
// to it, everything else is rejected, and a result without a verified
// attestation is never returned as trusted. Observation pruning keeps
// the exit status and the error tail while bounding what reaches the
// model (E5/E8).
// Trace: tasks-P0 T26; CLAUDE.md sandbox hard rule; PRD §6 item 6.
package exectool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/sandbox"
)

// ToolID is the sealed identity of the exec tool.
const ToolID contracts.ToolID = "exec"

// execArgs is the CLOSED argument schema: an absolute program path and
// argv — NEVER a shell string (scripts/shebang are rejected by the
// backend before any sandbox exists).
type execArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Adapter runs exec calls through the REAL sandbox backend.
type Adapter struct {
	backend sandbox.Backend
	report  sandbox.ProbeReport
}

// New requires a LIVE passing probe report — a dead report refuses
// construction (fail closed; the composition root keeps exec off).
func New(b sandbox.Backend, report sandbox.ProbeReport) (*Adapter, error) {
	if b == nil || !report.Available || report.ProbeHash == "" {
		return nil, fmt.Errorf("exectool: a live passing sandbox probe is required (fail closed)")
	}
	return &Adapter{backend: b, report: report}, nil
}

// Rules is the PEP decision for the exec family: command execution is
// ASK — the owner confirms every exec (yolo journals ALLOWED_BY_YOLO).
func Rules() map[contracts.ToolID]effectpath.Decision {
	return map[contracts.ToolID]effectpath.Decision{ToolID: effectpath.DecisionAsk}
}

// Spec seals the planner-facing schema; ExecutionKind=ExecProcess is the
// property the whole slice exists for.
func Spec() map[contracts.ToolID]effectpath.ToolSpec {
	return map[contracts.ToolID]effectpath.ToolSpec{
		ToolID: {Effect: contracts.EffectIrreversible, ExecutionKind: contracts.ExecProcess,
			ArgsSchemaHash: "exec.v1",
			Description:    `run one sandboxed program; args {"command":"/abs/path","args":["..."]} — absolute ELF path, no shell strings`},
	}
}

// Launch implements effectpath.SandboxBackend: the ONLY door process
// execution can come through. Unknown tool ids and non-ExecProcess kinds
// are rejected, never run in-process (CLAUDE.md hard rule).
func (a *Adapter) Launch(ctx context.Context, call contracts.ToolCall) (contracts.ToolResult, error) {
	if call.ToolID != ToolID {
		return contracts.ToolResult{}, fmt.Errorf("exectool: unknown process tool %q (fail closed)", call.ToolID)
	}
	if call.ExecutionKind != contracts.ExecProcess {
		return contracts.ToolResult{}, fmt.Errorf("exectool: execution kind %d is not ExecProcess (fail closed)", call.ExecutionKind)
	}
	dec := json.NewDecoder(bytes.NewReader(call.Arguments))
	dec.DisallowUnknownFields()
	var args execArgs
	if err := dec.Decode(&args); err != nil {
		return contracts.ToolResult{}, fmt.Errorf("exectool: malformed args (closed schema): %w", err)
	}
	if !filepath.IsAbs(args.Command) {
		return contracts.ToolResult{}, fmt.Errorf("exectool: command must be an absolute path (no shell strings, fail closed)")
	}
	// Disposable RW workdir per call, under a guarded root; removed after.
	work, err := os.MkdirTemp("", "nexus-exec-")
	if err != nil {
		return contracts.ToolResult{}, err
	}
	defer os.RemoveAll(work)
	if err := os.Chmod(work, 0o700); err != nil {
		return contracts.ToolResult{}, err
	}
	timeout := time.Until(call.Deadline)
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 2 * time.Minute
	}
	policy, err := a.backend.Compile(ctx, sandbox.Spec{
		Target: args.Command, Args: args.Args, WorkDir: work, Timeout: timeout,
	}, a.report)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	proc, err := a.backend.Launch(ctx, policy)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	defer proc.Close()
	waitErr := proc.Wait()
	// The result is TRUSTED only under a verified attestation (T25); the
	// E9 commit receipt is BACKED by that attestation — an effectful
	// result without it parks UNKNOWN in the PEP.
	att, err := a.backend.Attest(ctx, proc, policy)
	if err != nil {
		return contracts.ToolResult{}, fmt.Errorf("exectool: result untrusted: %w", err)
	}
	status := "exit status: 0"
	if waitErr != nil {
		status = "exit status: " + waitErr.Error()
	}
	content := status + "\n" + pruneOutput(proc.Output())
	now := time.Now().UTC()
	res := contracts.ResultSucceeded
	if waitErr != nil {
		res = contracts.ResultFailed
	}
	block, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID("obs-exec-" + string(call.ToolCallID)),
		Kind:    "tool_output", Content: &content, ContentHash: hashHex(content),
		SourceURI: "nexus://exec/" + args.Command, Producer: "exec",
		// Subprocess output is EXTERNALLY INFLUENCED: untrusted fence.
		Trust: contracts.TrustUntrustedExternal, Sensitivity: contracts.SensitivityInternal,
		Lineage: []string{string(call.ToolCallID)}, ObservedAt: now,
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	return contracts.ToolResult{
		ToolCallID: call.ToolCallID, AttemptNo: call.AttemptNo, Status: res,
		Output:    []contracts.ContextBlock{block},
		StartedAt: now, FinishedAt: now,
		Commit: &contracts.CommitReceipt{
			Phase: contracts.PhaseAfterCommit, ToolCallID: call.ToolCallID,
			AttemptNo: call.AttemptNo, ContentHash: att.ClosureDigest,
		},
	}, nil
}

// hashHex is the sha256 hex of content (block integrity currency).
func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// pruneOutput bounds what reaches the model while PRESERVING the error
// tail — failures diagnose from their last lines (E5/E8: exit status +
// error lines survive pruning).
func pruneOutput(out string) string {
	const headMax, tailMax = 2048, 2048
	if len(out) <= headMax+tailMax {
		return out
	}
	head := out[:headMax]
	tail := out[len(out)-tailMax:]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
		tail = tail[i+1:] // start the tail at a line boundary
	}
	return head + fmt.Sprintf("\n…[%d bytes pruned]…\n", len(out)-len(head)-len(tail)) + tail
}

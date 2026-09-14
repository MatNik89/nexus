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
	"unicode/utf8"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/sandbox"
	"github.com/MatNik89/nexus/internal/security/redact"
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
	backend  sandbox.Backend
	report   sandbox.ProbeReport
	redactor redact.Redactor
	allow    map[string]bool
}

// New requires a LIVE passing probe report — a dead report refuses
// construction (fail closed; the composition root keeps exec off) — and
// the KNOWN-REF redactor: exec reads the whole host read-only, so its
// output passes the secret scrub BEFORE any model boundary (Phase-6
// kilo #2).
func New(b sandbox.Backend, report sandbox.ProbeReport, r redact.Redactor, execAllow []string) (*Adapter, error) {
	if b == nil || !report.Available || report.ProbeHash == "" || r == nil {
		return nil, fmt.Errorf("exectool: a live passing sandbox probe and a redactor are required (fail closed)")
	}
	allow := map[string]bool{}
	for _, p := range execAllow {
		if strings.ContainsRune(p, utf8.RuneError) {
			// Same corruption class as parseArgs' matching check: a
			// raw invalid byte and an unpaired surrogate escape both
			// decode to U+FFFD, letting two distinct configured
			// spellings alias to one stored string.
			return nil, fmt.Errorf("exectool: exec_allow entry %q contains an invalid-UTF-8 replacement character (fail closed)", p)
		}
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("exectool: exec_allow entry %q is not absolute (fail closed)", p)
		}
		if p != filepath.Clean(p) {
			// Symmetric to parseArgs' request-side check (round-4 code-
			// review): silently cleaning a non-lexically-clean config
			// entry would store it under a DIFFERENT key than what the
			// owner configured — "/safe/link/../git" (a real symlink
			// traversal) would be stored as "/safe/git", so a model
			// requesting the perfectly clean "/safe/git" would match an
			// entry the owner never actually listed. Rejecting outright,
			// independently of config.ValidateBounds's own check
			// (defense-in-depth — never rely solely on the config layer
			// having validated correctly), and storing verbatim below
			// closes this by construction, the same way as the request
			// side.
			return nil, fmt.Errorf("exectool: exec_allow entry %q is not a lexically clean path (fail closed)", p)
		}
		allow[p] = true
	}
	return &Adapter{backend: b, report: report, redactor: r, allow: allow}, nil
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

// parseArgs is the ONE decode+validate path for exec's arguments — shared
// by Launch and ArgGate (S6.1) so the DECISION and the EXECUTION always
// parse identically. A divergence between two independent parses could
// decide on one command and run another (round-2 code-review discipline,
// same principle as Audn's canonical-intent re-marshal).
func parseArgs(raw json.RawMessage) (execArgs, error) {
	if effectpath.HasDuplicateJSONKeys(raw) {
		// Last-wins duplicate keys let the approver-visible summary and
		// the executed value diverge (Phase-6 kilo #1) — refused.
		return execArgs{}, fmt.Errorf("exectool: duplicate argument keys (fail closed)")
	}
	// Decode Args as []*string, not []string: a JSON `null` array
	// element silently becomes the zero value "" under []string (round-2
	// code-review, live-reproduced: {"args":[null]} decoded to
	// Args==[]string{""}, so the approved/requested representation
	// diverged from what actually ran) — a nil pointer element makes the
	// substitution visible so it can be refused instead of silently
	// executed.
	var wire struct {
		Command string    `json:"command"`
		Args    []*string `json:"args,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return execArgs{}, fmt.Errorf("exectool: malformed args (closed schema): %w", err)
	}
	args := execArgs{Command: wire.Command, Args: make([]string, len(wire.Args))}
	for i, a := range wire.Args {
		if a == nil {
			return execArgs{}, fmt.Errorf("exectool: args[%d] is null, not a string (closed schema, fail closed)", i)
		}
		args.Args[i] = *a
	}
	// A raw invalid UTF-8 byte and an unpaired surrogate escape
	// (\ud800-\udfff with no partner) BOTH decode to the SAME literal
	// U+FFFD replacement character — two distinct byte sequences alias
	// to one visible string (round-1 code-review, live-reproduced:
	// "/tmp/tool-\ud800" and a raw 0xFF byte both produced
	// "/tmp/tool-�"). This is the identical corruption class
	// internal/memory/store.go's validUTF8 closes: reject the LITERAL
	// replacement character post-decode, uniformly, rather than
	// enumerate JSON escape forms — closes it here for the same reason.
	if strings.ContainsRune(args.Command, utf8.RuneError) {
		return execArgs{}, fmt.Errorf("exectool: command contains an invalid-UTF-8 replacement character (fail closed)")
	}
	for _, a := range args.Args {
		if strings.ContainsRune(a, utf8.RuneError) {
			return execArgs{}, fmt.Errorf("exectool: an argument contains an invalid-UTF-8 replacement character (fail closed)")
		}
	}
	if !filepath.IsAbs(args.Command) {
		return execArgs{}, fmt.Errorf("exectool: command must be an absolute path (no shell strings, fail closed)")
	}
	if args.Command != filepath.Clean(args.Command) {
		// A symlink component followed by ".." resolves differently in
		// the kernel than in filepath.Clean's purely lexical rewrite
		// (round-3 code-review, live-reproduced: "/safe/link/../git"
		// cleans to "/safe/git" — an allowlisted spelling — while the
		// kernel actually opens "/evil/git"). Refusing any non-lexically-
		// clean spelling outright closes this BY CONSTRUCTION: no
		// resolution is attempted, so there is no TOCTOU window either.
		// This is "lexically clean," never "canonical" — filepath.Clean
		// is a string operation with no knowledge of the filesystem, so
		// it cannot detect a symlink AT an already-clean path (a
		// separate, pre-existing exec_allow configuration-trust
		// question this check does not attempt to close).
		return execArgs{}, fmt.Errorf("exectool: command must already be a lexically clean path, no traversal or non-canonical segments (fail closed)")
	}
	return args, nil
}

// ArgGate is the S6.1 per-argument PEP restriction (effectpath.ArgGate):
// it can only narrow exec's static Ask down to Deny, never grant
// anything — an allowlisted, lexically clean command still falls through
// to the static Ask (approval is still required for every exec call);
// only a non-allowlisted or malformed one is denied before any approval
// ceremony is wasted (closing the round-1 finding: approving a call that
// Launch was always going to refuse anyway accomplished nothing).
func (a *Adapter) ArgGate(raw json.RawMessage) error {
	args, err := parseArgs(raw)
	if err != nil {
		return err
	}
	if !a.allow[args.Command] {
		return fmt.Errorf("exectool: %q is not in the exec_allow promoted-target list (deny-default, fail closed)", args.Command)
	}
	return nil
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
	args, err := parseArgs(call.Arguments)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	// PROMOTED-TARGET allowlist, DENY-DEFAULT (Phase-6 codex #5): only
	// programs the owner listed in exec_allow may run — an absolute ELF
	// interpreter (/bin/sh -c …) is not a loophole unless explicitly
	// promoted by the owner. Kept here as defense-in-depth even though
	// ArgGate (above) already checks this before Launch is ever reached
	// through the normal effect-path — never remove a redundant check
	// just because a decision moved earlier.
	if !a.allow[args.Command] {
		return contracts.ToolResult{}, fmt.Errorf("exectool: %q is not in the exec_allow promoted-target list (deny-default, fail closed)", args.Command)
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
	content := status + "\n" + pruneOutput(redactText(a.redactor, proc.Output()))
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

// redactText scrubs known secret references from subprocess output
// before the model boundary (the redactor works on JSON documents).
func redactText(r redact.Redactor, s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "(unrenderable)"
	}
	red, err := r.Redact(b)
	if err != nil {
		return "(redaction failed — content withheld)"
	}
	var out string
	if json.Unmarshal(red, &out) != nil {
		return "(unrenderable)"
	}
	return out
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

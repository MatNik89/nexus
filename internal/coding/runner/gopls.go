//go:build linux

package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
)

// MaxLSPMessageBytes bounds a single Content-Length-framed LSP message
// body — a misbehaving or compromised gopls could otherwise claim an
// arbitrarily large length and exhaust memory (plan-review round 2, agy's
// note on long-lived-session resource bounds).
const MaxLSPMessageBytes = 16 << 20

// RenameRequest specifies one symbol-rename query against a disposable
// snapshot of SourceDir — read-only: the returned WorkspaceEdit describes
// a candidate change, it never touches SourceDir or any live file (Slice
// 3's workspace.Apply is the separate, durable step that actually writes
// an accepted edit).
type RenameRequest struct {
	SourceDir   string
	FileRelPath string // slash-separated, relative to SourceDir
	Line        int    // 0-indexed
	Character   int    // 0-indexed UTF-16 code unit
	NewName     string

	GoBinary    string
	GoplsBinary string
	Timeout     time.Duration

	// Identifying context for the S7 grant and the coding.run journal
	// event. All required (fail closed) — same contract as RunSpec.
	OperationID contracts.OperationID
	TargetID    contracts.TargetID
	RunID       contracts.RunID
	ProfileID   contracts.ProfileID
}

// LSPError mirrors a JSON-RPC error object.
type LSPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RenameResult carries gopls's textDocument/rename response. Exactly one
// of Result/Error is set — an LSP-level error (e.g. "no identifier here")
// is DATA the governed session completed normally with, not an attempt
// failure, mirroring RunResult.ExitOK's data-vs-failure split.
type RenameResult struct {
	Result          json.RawMessage
	Error           *LSPError
	SnapshotDigest  string
	ToolchainDigest string
	PolicyHash      string
}

// RunGoplsRename drives a live `gopls serve` stdio JSON-RPC session
// (initialize -> initialized -> didOpen -> rename -> shutdown -> exit)
// inside the same sandboxed, S7-governed shape as Run — snapshot the
// source tree, pin the toolchain, sandbox.Compile, non-durable grant,
// sandbox.LaunchInteractive, attest, report, emit a coding.run journal
// event.
//
// Deliberately a live interactive conversation, not a pre-serialized
// batch write: empirically verified against the real gopls binary that
// writing the whole fixed request sequence (including exit) to stdin
// upfront races gopls's own async processing — gopls read the exit
// notification and shut down BEFORE finishing initialize, logging
// "server shutdown without initialization" and never answering the
// rename request. Only a genuine write-then-read-the-correlated-response
// loop (per request) works.
// validateFileRelPath fail-closed rejects anything but a clean,
// slash-separated, relative path: empty, absolute (leading '/'), or
// containing a '..' path component after cleaning (code-review finding,
// codex: a substring check on the raw string, as this used to be, does
// not use the same normalization path.Clean applies, so it is not
// provably equivalent to what actually gets joined into a URI/host
// path).
func validateFileRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("FileRelPath is required (fail closed)")
	}
	if path.IsAbs(p) {
		return fmt.Errorf("FileRelPath must be relative, got %q (fail closed)", p)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("FileRelPath escapes its root: %q (fail closed)", p)
	}
	return nil
}

// fileURIFor builds the in-sandbox file: URI for relPath (already
// validated by validateFileRelPath) via net/url, not string
// concatenation — a valid Go filename can contain '#', '?', a space, or
// a literal '%', all of which change or break URI syntax if pasted in
// raw (code-review finding, codex).
func fileURIFor(relPath string) string {
	return (&url.URL{Scheme: "file", Path: "/work/src/" + relPath}).String()
}

func RunGoplsRename(ctx context.Context, backend *sandbox.Bwrap, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal, req RenameRequest) (RenameResult, error) {
	if req.GoBinary == "" || req.GoplsBinary == "" {
		return RenameResult{}, fmt.Errorf("runner: GoBinary and GoplsBinary are both required")
	}
	if !req.OperationID.Valid() || !req.TargetID.Valid() || !req.RunID.Valid() || !req.ProfileID.Valid() {
		return RenameResult{}, fmt.Errorf("runner: OperationID/TargetID/RunID/ProfileID are all required (fail closed)")
	}
	if err := validateFileRelPath(req.FileRelPath); err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}

	pin, err := ResolveToolchain(req.GoBinary)
	if err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}

	scratchRoot, err := os.MkdirTemp("", "nexus-coding-gopls-*")
	if err != nil {
		return RenameResult{}, fmt.Errorf("runner: create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchRoot)
	srcDir := filepath.Join(scratchRoot, "src")
	gocacheDir := filepath.Join(scratchRoot, "gocache")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}
	if err := os.Mkdir(gocacheDir, 0o700); err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}
	snapDigest, err := copyTreeInto(req.SourceDir, srcDir)
	if err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}

	fileContent, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(req.FileRelPath)))
	if err != nil {
		return RenameResult{}, fmt.Errorf("runner: reading snapshotted target file: %w", err)
	}

	// Same WorkDir layout as Run: scratchRoot is the ONE bound root,
	// src/ and gocache/ are siblings inside it. gopls shells out to `go`
	// itself for type-checking, so PATH must resolve it inside the
	// sandbox — GOROOT is identity-bound at its host path (probe.go:
	// ExtraROBinds bind at the SAME absolute path), so GOROOT/bin is
	// where it resolves in-sandbox too.
	pin.Env["GOCACHE"] = "/work/gocache"
	pin.Env["GOTMPDIR"] = "/work/gocache"

	sbSpec := sandbox.Spec{
		Target:       req.GoplsBinary,
		Args:         []string{"serve"},
		WorkDir:      scratchRoot,
		Timeout:      req.Timeout,
		ExtraROBinds: pin.ROBinds,
		ExtraEnv:     pin.Env,
		// gopls's own loader unconditionally execs a bare "go" (verified
		// against golang.org/x/tools/internal/gocommand's source: no
		// absolute-path override hook exists) — ExtraPathDir is the one
		// sanctioned way to make that resolve; GOROOT/bin is already
		// covered by pin.ROBinds' GOROOT entry.
		ExtraPathDir: filepath.Join(pin.Env["GOROOT"], "bin"),
	}
	policy, err := backend.Compile(ctx, sbSpec, report)
	if err != nil {
		return RenameResult{}, fmt.Errorf("runner: compile: %w", err)
	}

	if err := grants.Begin(req.OperationID, req.TargetID, PolicyCodingRun); err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}
	grant, err := grants.Next(req.OperationID, nil)
	if err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}
	if err := grants.Consume(grant); err != nil {
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}

	deadline := time.Now().Add(req.Timeout)
	if req.Timeout <= 0 {
		deadline = time.Now().Add(30 * time.Second)
	}
	execCtx, cancelExec, err := grants.AttemptContext(ctx, req.OperationID, deadline)
	if err != nil {
		reportOutcome(grants, req.OperationID, s7.OutcomeUnknown, s7.CodeLocalRefused)
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}
	defer cancelExec()

	proc, err := backend.LaunchInteractive(execCtx, policy)
	if err != nil {
		if selfDeadlineCancelled(execCtx, err) {
			// The deadline can expire DURING launch preparation itself,
			// before any process ever starts — the same self-deadline
			// CANCELLED classification applies here as after Wait
			// (code-review finding, codex, rounds 2-3: reproduced
			// non-deterministically, and the naive context.Cause(execCtx)
			// check alone missed a narrower race where sandbox's own
			// synchronous deadline check fires before ctx.Done() is
			// actually published — see selfDeadlineCancelled's doc
			// comment). An unconditional FailedTerminal here made the
			// SAME deadline produce different S7 outcomes depending on
			// unrelated scheduling timing.
			grants.Cancel(req.OperationID, nil)
			journalGoplsRenameEvent(ctx, j, req, RenameResult{}, err)
			return RenameResult{}, fmt.Errorf("runner: launch cancelled by its own deadline: %w", err)
		}
		reportOutcome(grants, req.OperationID, s7.OutcomeFailedTerminal, s7.CodeLocalRefused)
		return RenameResult{}, fmt.Errorf("runner: launch: %w", err)
	}
	defer proc.Close()

	renameResp, sessionErr := runGoplsSession(proc, req, fileContent)

	waitErr := proc.Wait()
	if sessionErr == nil {
		sessionErr = waitErr
	}
	if sessionErr != nil && selfDeadlineCancelled(execCtx, sessionErr) {
		// Killed by ITS OWN deadline/cancel (mirrors Run's identical
		// classification, run.go): the attempt itself never completed,
		// nothing durable happened downstream of it, so this is
		// CANCELLED, not a failed attempt.
		grants.Cancel(req.OperationID, nil)
		journalGoplsRenameEvent(ctx, j, req, RenameResult{}, sessionErr)
		return RenameResult{}, fmt.Errorf("runner: gopls session cancelled by its own deadline: %w", sessionErr)
	}
	if sessionErr != nil {
		reportOutcome(grants, req.OperationID, s7.OutcomeFailedTerminal, s7.CodeLocalRefused)
		journalGoplsRenameEvent(ctx, j, req, RenameResult{}, sessionErr)
		return RenameResult{}, fmt.Errorf("runner: gopls session: %w\nstderr: %s", sessionErr, proc.Stderr())
	}

	if _, err := backend.AttestInteractive(ctx, proc, policy); err != nil {
		reportOutcome(grants, req.OperationID, s7.OutcomeUnknown, s7.CodeLocalRefused)
		journalGoplsRenameEvent(ctx, j, req, RenameResult{}, err)
		return RenameResult{}, fmt.Errorf("runner: attest: %w", err)
	}
	// A rename result the caller can act on requires S7 to have actually
	// ACCEPTED the success landing — Report can legitimately refuse
	// (operation no longer RUNNING, invalid transition, durability
	// failure), and a discarded refusal here would let a caller trust a
	// result S7 never recorded as succeeded (code-review finding, codex;
	// mirrors run.go's own propagation of this same Report call).
	if err := grants.Report(req.OperationID, s7.OutcomeSucceeded, "", nil); err != nil {
		journalGoplsRenameEvent(ctx, j, req, RenameResult{}, err)
		return RenameResult{}, fmt.Errorf("runner: %w", err)
	}

	result := RenameResult{
		SnapshotDigest:  snapDigest,
		ToolchainDigest: pin.HashDigest,
		PolicyHash:      policy.PolicyHash(),
	}
	if raw, ok := renameResp["error"]; ok && len(raw) > 0 && string(raw) != "null" {
		var lspErr LSPError
		if err := json.Unmarshal(raw, &lspErr); err != nil {
			journalGoplsRenameEvent(ctx, j, req, result, err)
			return RenameResult{}, fmt.Errorf("runner: malformed LSP error object: %w", err)
		}
		result.Error = &lspErr
	} else {
		result.Result = renameResp["result"]
	}
	if err := journalGoplsRenameEvent(ctx, j, req, result, nil); err != nil {
		return RenameResult{}, err
	}
	return result, nil
}

// journalGoplsRenameEvent emits a "coding.run" journal event for a
// gopls-rename session — same event type and envelope shape as Run's own
// journalRunEvent (a rename session IS a coding-run, just one driven
// through a live LSP conversation instead of a one-shot subprocess).
func journalGoplsRenameEvent(ctx context.Context, j *journal.Journal, req RenameRequest, result RenameResult, runErr error) error {
	if j == nil {
		return runErr
	}
	payload := struct {
		File            string `json:"file"`
		Line            int    `json:"line"`
		Character       int    `json:"character"`
		NewName         string `json:"new_name"`
		HasResult       bool   `json:"has_result"`
		HasLSPError     bool   `json:"has_lsp_error"`
		SnapshotDigest  string `json:"snapshot_digest"`
		ToolchainDigest string `json:"toolchain_digest"`
		PolicyHash      string `json:"policy_hash"`
		Error           string `json:"error,omitempty"`
	}{
		File: req.FileRelPath, Line: req.Line, Character: req.Character, NewName: req.NewName,
		HasResult: len(result.Result) > 0, HasLSPError: result.Error != nil,
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
		return fmt.Errorf("runner: marshal coding.run (gopls) payload: %w", marshalErr)
	}
	_, appendErr := j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:     contracts.EventID("ev-" + string(req.OperationID) + "-" + randHex(8)),
		EventType:   "coding.run",
		RunID:       req.RunID,
		EmittedAt:   time.Now().UTC(),
		ActorType:   contracts.ActorSystem,
		ActorID:     "coding-runner",
		PrincipalID: "nexus",
		WorkspaceID: "local",
		ProfileID:   req.ProfileID,
		AttemptNo:   1,
		Payload:     raw,
		PayloadHash: "recomputed",
	})
	if appendErr != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("runner: journal coding.run (gopls): %w", appendErr)
	}
	return runErr
}

// runGoplsSession performs the fixed initialize -> initialized ->
// didOpen -> rename -> shutdown -> exit conversation and returns the
// rename request's raw response message (id 2).
func runGoplsSession(proc *sandbox.InteractiveProcess, req RenameRequest, fileContent []byte) (map[string]json.RawMessage, error) {
	reader := bufio.NewReader(proc.Stdout())
	rootURI := "file:///work/src"
	// net/url, not string concatenation (code-review finding, codex): a
	// valid Go filename can contain '#', '?', a space, or non-ASCII
	// bytes, all of which change URI semantics if pasted in raw —
	// validateFileRelPath already rejected '..'/absolute paths, so
	// url.URL only needs to percent-encode what's left.
	fileURI := fileURIFor(req.FileRelPath)

	if err := lspWriteMessage(proc.Stdin(), map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"processId": nil,
			"rootUri":   rootURI,
			"capabilities": map[string]any{
				"textDocument": map[string]any{
					"rename": map[string]any{"dynamicRegistration": false},
				},
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("send initialize: %w", err)
	}
	if _, err := lspAwaitResponse(reader, 1); err != nil {
		return nil, fmt.Errorf("await initialize response: %w", err)
	}

	if err := lspWriteMessage(proc.Stdin(), map[string]any{
		"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{},
	}); err != nil {
		return nil, fmt.Errorf("send initialized: %w", err)
	}
	if err := lspWriteMessage(proc.Stdin(), map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri": fileURI, "languageId": "go", "version": 1, "text": string(fileContent),
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("send didOpen: %w", err)
	}

	if err := lspWriteMessage(proc.Stdin(), map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/rename",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": fileURI},
			"position":     map[string]any{"line": req.Line, "character": req.Character},
			"newName":      req.NewName,
		},
	}); err != nil {
		return nil, fmt.Errorf("send rename: %w", err)
	}
	renameResp, err := lspAwaitResponse(reader, 2)
	if err != nil {
		return nil, fmt.Errorf("await rename response: %w", err)
	}

	if err := lspWriteMessage(proc.Stdin(), map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "shutdown", "params": nil,
	}); err != nil {
		return nil, fmt.Errorf("send shutdown: %w", err)
	}
	if _, err := lspAwaitResponse(reader, 3); err != nil {
		return nil, fmt.Errorf("await shutdown response: %w", err)
	}
	if err := lspWriteMessage(proc.Stdin(), map[string]any{
		"jsonrpc": "2.0", "method": "exit", "params": nil,
	}); err != nil {
		return nil, fmt.Errorf("send exit: %w", err)
	}
	proc.Stdin().Close()

	return renameResp, nil
}

// lspAwaitResponse reads messages until one correlates to wantID (a
// response carries "result" or "error"), skipping interleaved
// server-initiated notifications (window/logMessage,
// textDocument/publishDiagnostics, etc — gopls sends these
// asynchronously between a request and its response). Bounded to 500
// skipped messages so a misbehaving server cannot hang this loop
// forever — the caller's ctx-cancellation-kills-tree behavior
// (LaunchInteractive's cancel-watch) is the outer, wall-clock bound.
func lspAwaitResponse(r *bufio.Reader, wantID int) (map[string]json.RawMessage, error) {
	for i := 0; i < 500; i++ {
		msg, err := lspReadMessage(r)
		if err != nil {
			return nil, err
		}
		idRaw, ok := msg["id"]
		if !ok {
			continue // a notification, not correlated to any request
		}
		var id int
		if err := json.Unmarshal(idRaw, &id); err != nil {
			continue // a string/other id type — not one of ours
		}
		if id != wantID {
			continue // a response to an earlier request we already consumed
		}
		if _, hasResult := msg["result"]; hasResult {
			return msg, nil
		}
		if _, hasError := msg["error"]; hasError {
			return msg, nil
		}
	}
	return nil, fmt.Errorf("runner: no response for request id %d after 500 messages (fail closed)", wantID)
}

// lspWriteMessage frames obj as a Content-Length-delimited JSON-RPC
// message and writes it to w.
func lspWriteMessage(w io.Writer, obj any) error {
	body, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// lspReadMessage reads one Content-Length-framed JSON-RPC message,
// fail-closed on a header claiming more than MaxLSPMessageBytes.
// maxLSPHeaderLines/maxLSPHeaderLineBytes bound the header section read
// before Content-Length's body limit even applies — codex's code-review
// finding: an unbounded number of header lines is its own resource
// vector, independent of MaxLSPMessageBytes.
// topknot ceiling: bufio.Reader.ReadString itself still grows unboundedly
// WHILE searching for a single line's '\n' (this check only fires after
// it returns), so this is a measured bound against a misbehaving but
// eventually-terminating PINNED gopls binary (already content-hash
// verified by sandbox.Compile's target/closure hashing before it ever
// runs), not a hardened defense against a fully adversarial byte stream.
// Upgrade trigger: if gopls ever becomes a caller-suppliable/untrusted
// binary rather than an operator-pinned one, replace with an
// incrementally-bounded reader (ReadSlice/Peek in a capped loop).
const (
	maxLSPHeaderLines     = 64
	maxLSPHeaderLineBytes = 8 << 10 // 8KB
)

func lspReadMessage(r *bufio.Reader) (map[string]json.RawMessage, error) {
	contentLength := -1
	for i := 0; ; i++ {
		if i >= maxLSPHeaderLines {
			return nil, fmt.Errorf("runner: LSP message header exceeds %d lines (fail closed)", maxLSPHeaderLines)
		}
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(line) > maxLSPHeaderLineBytes {
			return nil, fmt.Errorf("runner: LSP message header line exceeds %d bytes (fail closed)", maxLSPHeaderLineBytes)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				return nil, fmt.Errorf("runner: malformed Content-Length header %q: %w", val, err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("runner: LSP message missing Content-Length header (fail closed)")
	}
	if contentLength > MaxLSPMessageBytes {
		return nil, fmt.Errorf("runner: LSP message Content-Length %d exceeds max %d (fail closed)", contentLength, MaxLSPMessageBytes)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("runner: malformed LSP JSON body: %w", err)
	}
	return msg, nil
}

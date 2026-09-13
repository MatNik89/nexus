//go:build linux

// tool.go registers rename_symbol as a real, model-facing tool pair
// through the S6.0 PEP + S6.9 lifecycle (S6 tool-boundary, piece 2) —
// previously only reachable as an internal Go API from tests. Two tool
// IDs, mirroring invariant 3's own Prepare/Apply split:
//
//   - rename_symbol_prepare (EffectReadOnly, ALLOW, ExecInProcessGoverned):
//     runs the real, read-only gopls + staged-compile Prepare pipeline
//     and returns a preview (touched files, PlanDigest) — safe to call
//     freely, never writes anything.
//   - rename_symbol_apply (EffectReversible, ASK, ExecInProcessGoverned):
//     re-runs Prepare FRESH (never from a cache — the SAME "verify now,
//     don't trust history" discipline this whole program uses
//     everywhere else) and refuses unless the result's OWN PlanDigest
//     matches the caller-supplied expected plan_digest (copied from the
//     prepare call's own output) EXACTLY, then commits through
//     ApplyGoverned. Reusing effectpath's EXISTING exact-intent
//     Approvals mechanism this way needs zero new stateful machinery:
//     plan_digest is just another argument bound into the SAME approval
//     hash, so any change to the underlying code (or the model asking
//     for a DIFFERENT rename) invalidates any prior approval
//     automatically — no plan cache, no TTL, no new primitive.
//
// GoBinary/GoplsBinary and the coding-workspace root are ALWAYS resolved
// by Tools' own caller (host toolchain detection, config.Config.
// CodingWorkspaceRoot), never from the model's own JSON arguments (S6
// tool-boundary piece 1's own documented obligation, docs/SECTION-MAP.md
// — a model-influenced GoBinary would be a sandbox-escape surface).
package symedit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/coding/workspace"
	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
	"golang.org/x/sys/unix"
)

// ToolRenamePrepare/ToolRenameApply are the two tool IDs this file
// registers.
const (
	ToolRenamePrepare contracts.ToolID = "rename_symbol_prepare"
	ToolRenameApply   contracts.ToolID = "rename_symbol_apply"
)

// DefaultRenameTimeout bounds each governed sub-operation (the gopls
// session, the staged type-check) Prepare drives per call. topknot: a
// fixed ceiling, not caller-configurable yet.
const DefaultRenameTimeout = 60 * time.Second

// applyGoverned is a test seam: ApplyGoverned in production, letting a
// test substitute a fake to deterministically exercise the poisoned
// latch's OWN wiring (Tools' apply handler) without needing to
// reproduce a real filesystem-drift race through workspace's own
// (unexported, cross-package-unreachable) test seams — the underlying
// ErrRollbackIncomplete classification is workspace's own already-tested
// responsibility; this seam isolates what actually needs testing here:
// does an ErrRollbackIncomplete result correctly latch poisoned.
var applyGoverned = ApplyGoverned

// Specs are the SEALED tool declarations — the planner builds ToolCalls
// FROM these, never from provider-supplied effect/kind fields (same
// discipline as memory.Specs/obligation.Specs).
func Specs() map[contracts.ToolID]effectpath.ToolSpec {
	return map[contracts.ToolID]effectpath.ToolSpec{
		ToolRenamePrepare: {
			Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcessGoverned,
			ArgsSchemaHash: "rename_symbol_prepare.v1",
			Description: `preview a Go symbol rename; args {"file":"relative/path.go","line":0,"character":0,"new_name":"..."} ` +
				`(line/character are 0-indexed and must point at the symbol). Returns a preview and a plan_digest — ` +
				`call rename_symbol_apply with the SAME arguments plus that exact plan_digest to actually apply it.`,
		},
		ToolRenameApply: {
			Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcessGoverned,
			ArgsSchemaHash: "rename_symbol_apply.v3",
			Description: `apply a previously previewed Go symbol rename; args {"file":...,"line":...,"character":...,` +
				`"new_name":...,"plan_digest":"<from rename_symbol_prepare>","touched_files":["<the exact file list ` +
				`from rename_symbol_prepare's own preview>"],"old_name":"<the original symbol name from the preview, ` +
				`when the preview stated one>"}. touched_files must list every file the preview named, and old_name ` +
				`(when the preview stated one) must match it exactly — the user approving this call sees these raw ` +
				`arguments, so this is the ONLY way they see the full blast radius and the actual identity being ` +
				`renamed before approving. Refuses if the underlying code changed since the preview, or if ` +
				`touched_files/old_name do not exactly match a fresh preview (fail closed) — call ` +
				`rename_symbol_prepare again for a fresh plan_digest.`,
		},
	}
}

// Rules: prepare is read-only (ALLOW, never writes anything); apply
// mutates the workspace (ASK — the exact-intent hash proves the call is
// UNCHANGED since it was approved: any change to the target file/
// position/name, the underlying code, or the claimed touched_files list
// invalidates a prior approval, per effectpath's own Approvals contract.
// That integrity proof is NOT by itself an informed-consent proof —
// effectpath's shared ASK summary shows the raw JSON arguments verbatim
// (internal/approval/approval.go), so touched_files exists specifically
// to put the full blast radius into those arguments; the model relaying
// rename_symbol_prepare's own preview text in conversation is what
// actually shows the user a human-readable diff before they approve —
// code-review finding, codex, round 1: an earlier version of this
// comment overclaimed "approval IS the did-you-see-the-diff gate").

func Rules() map[contracts.ToolID]effectpath.Decision {
	return map[contracts.ToolID]effectpath.Decision{
		ToolRenamePrepare: effectpath.DecisionAllow,
		ToolRenameApply:   effectpath.DecisionAsk,
	}
}

// renameArgs is the args shape both tools share; apply decodes the same
// fields plus PlanDigest on top.
type renameArgs struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
	NewName   string `json:"new_name"`
}

func decodeRenameArgs(raw json.RawMessage) (renameArgs, error) {
	var a renameArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return renameArgs{}, fmt.Errorf("rename_symbol: arguments must be a JSON object: %w", err)
	}
	if a.File == "" || a.NewName == "" {
		return renameArgs{}, fmt.Errorf("rename_symbol: file and new_name are both required (fail closed)")
	}
	return a, nil
}

// Tools exposes the real, governed Prepare/ApplyGoverned pipeline as two
// in-process tools. workspaceRoot resolves a profile's ONE configured
// coding-workspace root (config.Config.CodingWorkspaceRoot — owner-
// configured, never model-chosen; a profile with no entry gets no
// access at all). goBinary/goplsBinary are resolved ONCE by the caller
// (host toolchain detection at daemon startup), never derived from a
// call's own arguments.
func Tools(workspaceRoot func(contracts.ProfileID) (string, bool), goBinary, goplsBinary string, store *sealedstore.Store, backend *sandbox.Bwrap, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal) map[contracts.ToolID]effectpath.InProcFunc {
	// poisoned latches true, for this daemon PROCESS's remaining
	// lifetime, the moment ANY apply lands OutcomeUnknown (an
	// incompletely-rolled-back mutation, workspace.ErrRollbackIncomplete)
	// — RestartScan only re-checks at the NEXT startup, so without this
	// a later rename could otherwise keep operating on the SAME
	// unresolved workspace for the rest of the CURRENT process's run
	// (code-review finding, codex, round 4 HIGH). A restart re-runs
	// buildDaemon's own scanClean gate (main.go), which is the actual
	// recovery-relevant check; this flag only prevents the CURRENT,
	// already-running process from digging the hole deeper in the
	// meantime.
	var poisoned atomic.Bool
	// applyMu serializes the ENTIRE apply operation — poisoned-check
	// through the mutation call and the resulting poisoning decision —
	// as one atomic critical section. Without this, two concurrent
	// rename_symbol_apply calls (the daemon serves multiple connections
	// concurrently) could both pass the poisoned check before either
	// finishes; one lands OutcomeUnknown and poisons the closure while
	// the OTHER is still mid-flight and then completes its own mutation
	// on top of the now-known-unresolved workspace anyway (code-review
	// finding, codex, round 5 HIGH, live-reproduced with a deterministic
	// overlay holding one apply at the mutation boundary while a second
	// poisoned the closure). rename_symbol_prepare is deliberately NOT
	// serialized under this same lock — it never writes anything, so
	// concurrent previews are safe; only mutation needs mutual exclusion.
	var applyMu sync.Mutex
	prepare := func(ctx context.Context, c contracts.ToolCall) (Plan, string, error) {
		if poisoned.Load() {
			return Plan{}, "", fmt.Errorf("rename_symbol: disabled — a prior apply left the workspace in an unresolved state (OutcomeUnknown); restart the daemon after manual reconciliation (fail closed)")
		}
		root, ok := workspaceRoot(c.ProfileID)
		if !ok {
			return Plan{}, "", fmt.Errorf("rename_symbol: no coding workspace configured for profile %q (fail closed)", c.ProfileID)
		}
		args, err := decodeRenameArgs(c.Arguments)
		if err != nil {
			return Plan{}, "", err
		}
		rootFd, err := workspace.OpenRoot(root)
		if err != nil {
			return Plan{}, "", fmt.Errorf("rename_symbol: open workspace root: %w", err)
		}
		defer unix.Close(rootFd)
		var st unix.Stat_t
		if err := unix.Fstat(rootFd, &st); err != nil {
			return Plan{}, "", fmt.Errorf("rename_symbol: stat workspace root: %w", err)
		}
		target := workspace.ApplyTargetID(c.ProfileID, uint64(st.Dev), st.Ino)
		req := runner.RenameRequest{
			SourceDir: root, FileRelPath: args.File, Line: args.Line, Character: args.Character, NewName: args.NewName,
			GoBinary: goBinary, GoplsBinary: goplsBinary, Timeout: DefaultRenameTimeout,
			OperationID: contracts.OperationID("rename-" + string(c.ToolCallID)), TargetID: target,
			TypeCheckOperationID: contracts.OperationID("rename-typecheck-" + string(c.ToolCallID)), TypeCheckTargetID: target,
			RunID: contracts.RunID("run-" + string(c.ToolCallID)), ProfileID: c.ProfileID,
		}
		plan, err := Prepare(ctx, rootFd, backend, report, grants, j, req)
		if err != nil {
			return Plan{}, "", err
		}
		return plan, root, nil
	}

	return map[contracts.ToolID]effectpath.InProcFunc{
		ToolRenamePrepare: func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			plan, _, err := prepare(ctx, c)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			oldName, haveOldName := originalSymbolName(plan)
			var text string
			if haveOldName {
				text = fmt.Sprintf(
					"Prepared rename of %q to %q, touching %d file(s): %s\nplan_digest: %s\n"+
						"Call rename_symbol_apply with the SAME arguments plus this exact plan_digest and old_name to apply it.",
					oldName, plan.NewName, len(plan.Edits), touchedFiles(plan), plan.PlanDigest)
			} else {
				text = fmt.Sprintf(
					"Prepared rename to %q, touching %d file(s): %s\nplan_digest: %s\n"+
						"Call rename_symbol_apply with the SAME arguments plus this exact plan_digest to apply it.",
					plan.NewName, len(plan.Edits), touchedFiles(plan), plan.PlanDigest)
			}
			return textResult(c, text, nil)
		},
		ToolRenameApply: func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			applyMu.Lock()
			defer applyMu.Unlock()
			var applyArgs struct {
				renameArgs
				PlanDigest   string   `json:"plan_digest"`
				TouchedFiles []string `json:"touched_files"`
				OldName      string   `json:"old_name"`
			}
			if err := json.Unmarshal(c.Arguments, &applyArgs); err != nil {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: arguments must be a JSON object: %w", err)
			}
			if applyArgs.PlanDigest == "" {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: plan_digest is required — call rename_symbol_prepare first (fail closed)")
			}
			if len(applyArgs.TouchedFiles) == 0 {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: touched_files is required (the file list from rename_symbol_prepare's own preview — fail closed)")
			}
			plan, root, err := prepare(ctx, c)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			if plan.PlanDigest != applyArgs.PlanDigest {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: the underlying code changed since the preview (plan_digest mismatch) — call rename_symbol_prepare again (fail closed)")
			}
			// touched_files/old_name are redundant with plan_digest for
			// INTEGRITY (plan_digest already hashes every edit's RelPath
			// and content) — their purpose is honesty in the raw-JSON
			// approval prompt the user actually sees (see Rules' own doc
			// comment): refusing a mismatch here means the claim visible
			// in that prompt can never silently diverge from what
			// plan_digest actually binds (docs/PLAN-CODING-TRIO.md's own
			// "canonical preview: symbol identity + old/new name +
			// complete path set + patch hash" requirement — code-review
			// finding, codex, round 2).
			if !sameFileSet(applyArgs.TouchedFiles, plan.Edits) {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: touched_files does not match a fresh preview's own file list (fail closed) — call rename_symbol_prepare again")
			}
			if freshOldName, ok := originalSymbolName(plan); ok && freshOldName != applyArgs.OldName {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: old_name does not match a fresh preview's own symbol identity (fail closed) — call rename_symbol_prepare again")
			}
			rootFd, err := workspace.OpenRoot(root)
			if err != nil {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: open workspace root: %w", err)
			}
			defer unix.Close(rootFd)
			result, err := applyGoverned(ctx, rootFd, store, plan, grants, j, contracts.RunID("run-"+string(c.ToolCallID)+"-apply"))
			if err != nil {
				// Poison on EITHER an incompletely-rolled-back mutation
				// OR a mutation that fully COMMITTED to disk but then
				// failed to durably RECORD that outcome (e.g. the
				// EvMutationCommitted append itself failed) — in both
				// cases NEXUS's own S7/journal state no longer reliably
				// reflects reality, so continuing without a restart's
				// fresh RestartScan is unsafe regardless of which of the
				// two happened (code-review finding, codex, round 5
				// HIGH, live-reproduced: the committed-but-unrecorded
				// case previously poisoned nothing at all).
				if errors.Is(err, workspace.ErrRollbackIncomplete) || result.Committed {
					poisoned.Store(true)
				}
				if result.Committed {
					return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: the rename WAS written to disk but its outcome could not be durably recorded (fail closed, rename_symbol now disabled until a restart) — verify the workspace manually: %w", err)
				}
				return contracts.ToolResult{}, err
			}
			if !result.Committed {
				return contracts.ToolResult{}, fmt.Errorf("rename_symbol_apply: apply did not commit (fail closed)")
			}
			sum := sha256.Sum256([]byte(plan.PlanDigest))
			receipt := &contracts.CommitReceipt{Phase: contracts.PhaseAfterCommit, ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo, ContentHash: hex.EncodeToString(sum[:])}
			return textResult(c, fmt.Sprintf("Applied rename to %q.", plan.NewName), receipt)
		},
	}
}

// touchedFiles lists a Plan's edited files, comma-separated, for the
// preview text.
func touchedFiles(plan Plan) string {
	return strings.Join(planFileNames(plan), ", ")
}

func planFileNames(plan Plan) []string {
	names := make([]string, 0, len(plan.Edits))
	for _, fe := range plan.Edits {
		names = append(names, fe.RelPath)
	}
	return names
}

// originalSymbolName extracts the identifier plan's own edits actually
// replaced — the byte range TextEdit.StartByte:EndByte, resolved against
// the SAME preimage bytes Prepare already captured, IS the pre-rename
// text (Prepare's own real gopls response, not a guess or a second LSP
// call). Docs/PLAN-CODING-TRIO.md's own "canonical preview: symbol
// identity + old/new name" requirement names this explicitly (code-
// review finding, codex, round 2: the preview previously showed only the
// NEW name). REQUIRES every edit's extracted slice to agree EXACTLY —
// returns ok=false (never a possibly-wrong guess) the moment two edits
// disagree, not just when the plan is structurally malformed (code-
// review finding, codex, round 3 HIGH, live-reproduced: silently
// reporting only the FIRST slice found would hide a genuinely
// inconsistent rename from the preview the user is meant to trust).
func originalSymbolName(plan Plan) (string, bool) {
	name := ""
	found := false
	for _, fe := range plan.Edits {
		pre, ok := plan.Preimages[fe.RelPath]
		if !ok {
			continue
		}
		for _, te := range fe.Edits {
			if te.StartByte < 0 || te.EndByte > len(pre.Content) || te.StartByte >= te.EndByte {
				continue
			}
			old := string(pre.Content[te.StartByte:te.EndByte])
			if old == "" {
				continue
			}
			if !found {
				name, found = old, true
				continue
			}
			if old != name {
				return "", false
			}
		}
	}
	return name, found
}

// sameFileSet reports whether claimed names exactly the same set of files
// as plan's own edits — order-independent (the model's own summary of a
// preview it read is not obligated to preserve iteration order), but
// exact otherwise (fail closed on ANY addition, omission, or typo).
func sameFileSet(claimed []string, edits []FileEdit) bool {
	if len(claimed) != len(edits) {
		return false
	}
	want := make(map[string]int, len(edits))
	for _, fe := range edits {
		want[fe.RelPath]++
	}
	for _, name := range claimed {
		if want[name] == 0 {
			return false
		}
		want[name]--
	}
	return true
}

// textResult builds a governed tool's successful ToolResult from plain
// text (same shape/discipline as memory.Tools' own textResult: a single
// trusted ContextBlock, lineage rooted at this call).
func textResult(c contracts.ToolCall, text string, receipt *contracts.CommitReceipt) (contracts.ToolResult, error) {
	sum := sha256.Sum256([]byte(text))
	block, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID("obs-" + string(c.ToolCallID)), Kind: "tool_output",
		Content: &text, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://symedit", Producer: "symedit",
		// UNTRUSTED_EXTERNAL, not TOOL_TRUSTED: this text embeds
		// touchedFiles(plan) — filenames read from the TARGET repository's
		// own tree (via gopls), not authored by the user or this tool
		// itself. A repository under multi-contributor control (the
		// common case for anything but a private solo repo) could name a
		// file to inject instruction-like text into an unfenced trusted
		// block (matches exectool.go's own identical precedent: any tool
		// output that can carry filesystem-derived content is
		// UNTRUSTED_EXTERNAL, never TOOL_TRUSTED — code-review finding,
		// codex).
		Trust: contracts.TrustUntrustedExternal, Sensitivity: contracts.SensitivityConfidential,
		Lineage: []string{string(c.ToolCallID)}, ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	now := time.Now().UTC()
	return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
		Status: contracts.ResultSucceeded, Output: []contracts.ContextBlock{block},
		StartedAt: now, FinishedAt: now.Add(time.Millisecond), Commit: receipt}, nil
}

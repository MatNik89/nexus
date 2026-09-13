//go:build linux

// rename.go is Slice 3's Prepare/Apply orchestration for RenameSymbol —
// PLAN-CODING-TRIO.md invariant 3's two-phase contract, tying together
// three already-converged, independently-reviewed pieces: Slice 0's
// runner.RunGoplsRename (a governed, sandboxed gopls session), this
// package's own pure edit.go (ParseWorkspaceEdit/ApplyEdits), and
// internal/coding/workspace's Apply (the sealed-bundle multi-file
// transaction coordinator).
//
// SCOPE OF THIS PIECE (deliberately, matching workspace.Apply's own
// stated scope before it): no real S7/journal wiring exists yet for a
// NEW "RenameSymbol" operation type covering THIS multi-file Apply as a
// whole (RunGoplsRename's own S7 grant covers only its own single
// coding.run journal event, generated during Prepare — it says nothing
// about the SEPARATE, later Apply step) — bindDurable is passed straight
// through to workspace.Apply exactly as that package already defines it;
// a real caller supplies the durable-reference hook once that wiring
// exists. Sealed-bundle storage path resolution (which profile's
// sealedstore directory) is the CALLER's responsibility — Apply accepts
// an already-open *sealedstore.Store, it never resolves or opens one
// itself.
//
// Invariant 3's Prepare-phase "format+type-check the staged result" step
// is PARTIALLY built here: verifyStagedSyntaxIsValid stages every edit
// (via ApplyEdits, the SAME function Apply itself uses) and refuses the
// whole Plan unless the result parses as valid Go — catching a byte-
// offset error severe enough to corrupt the file's own syntax. This is
// deliberately NARROWER than a full "format+type-check": an earlier
// version of this check also required the staged result to be
// byte-identical to gofmt's own output, on the theory that a correct
// gopls rename should always already be gofmt-clean — code-review
// finding, codex, round 1 HIGH: FALSE in two real cases this codebase
// already has an explicit, tested contract for — a file using CRLF line
// endings (gofmt normalizes CRLF to LF; edit_test.go's own
// TestParseWorkspaceEditPreservesCRLF requires CRLF survive byte-exact)
// and a file that was already not gofmt-clean BEFORE this edit (gofmt
// would reformat the WHOLE file, including parts this edit never
// touched, which is not a defect IN the edit). Both would have been
// wrongly refused. Never silently reformats the content instead of
// refusing (silently changing content after PlanDigest binds it would
// undermine PlanDigest's own tamper-evidence guarantee) — it just
// doesn't attempt gofmt-cleanliness at all, only parseability, and does
// so via go/parser.ParseFile rather than go/format.Source (code-review
// finding, codex round 2 HIGH #1: format.Source's own contract accepts a
// bare declaration/statement fragment as "valid," not only a complete
// file — a byte offset severe enough to delete the package clause would
// have silently passed). The TYPE-check half (semantic validity:
// unresolved references, duplicate declarations, anything syntax alone
// can't catch) is built in typecheck.go's verifyStagedTypeChecks: stages
// the same edits into a disposable snapshot (never the live workspace)
// and runs a real, analyzer-free compile
// (`go test -vet=off -run=^$ -count=1 ./...`) through Slice 0's
// already-governed sandboxed runner.Run.
package symedit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"

	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/coding/workspace"
	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
	"golang.org/x/sys/unix"
)

// Preimage is one file's captured state at Prepare time: its exact
// bytes (never re-fetched — ApplyEdits at Apply time works against
// THESE, so the after-image is deterministically derived from what
// Prepare actually validated, not from a later independent re-read),
// its descriptor-captured identity, and its mode (preserved
// unconditionally across a content-only edit — FileMutation.Mode always
// applies). Deliberately carries NO separately-stored digest field
// (code-review finding, codex, round 2 HIGH): a digest stored alongside
// Content is itself independently tamperable — a caller could corrupt
// just the stored digest, or just Content, leaving the other looking
// consistent to a check that trusts the stored value instead of
// recomputing it. Every consumer of this preimage's digest calls
// sha256Hex(Content) itself, always fresh, never through a cached field.
type Preimage struct {
	Content  []byte
	Dev, Ino uint64
	Mode     fs.FileMode
}

// Plan is Prepare's read-only output — the plan's own "canonical
// preview" (invariant 3): the requested new name, the file whose
// didOpen the gopls session used, the complete validated edit set (path
// set + byte-offset edits, already sorted by RelPath by
// ParseWorkspaceEdit), every touched file's captured preimage, and a
// PlanDigest binding all of the above together (code-review finding,
// codex: an exported, field-mutable Plan otherwise lets a caller alter
// an edit's NewText — or a preimage's Content, Dev/Ino, or Mode — after
// Prepare while leaving everything else looking consistent; Apply
// independently recomputes this SAME digest from the Plan it receives,
// hashing every preimage's ACTUAL Content/identity/Mode fresh rather
// than trusting any stored derived value, and refuses on any mismatch —
// tampering after Prepare, or corruption across a serialize/deserialize
// boundary, e.g. a durable HITL approval wait, is always detected,
// never silently trusted).
type Plan struct {
	NewName           string
	OpenedFileRelPath string
	Edits             []FileEdit
	Preimages         map[string]Preimage // RelPath -> captured preimage
	PlanDigest        string
	SnapshotDigest    string
	ToolchainDigest   string
	PolicyHash        string
}

// afterGoplsRename is a test seam: a no-op in production, it lets
// rename_test.go deterministically inject a concurrent write to the
// pinned root between RunGoplsRename returning and Prepare trusting any
// of its own preimage reads, without a real (flaky) goroutine race.
var afterGoplsRename = func() {}

// beforeGoplsRename is a test seam: a no-op in production, it lets
// rename_test.go deterministically inject a change to req.SourceDir
// immediately before RunGoplsRename's own internal snapshot copy — used
// together with afterGoplsRename (which can revert it) to construct a
// real ABA entirely within the gopls call's own window, without a real
// (flaky) goroutine race or any change to runner's own internals.
var beforeGoplsRename = func() {}

// beforeStagedSyntaxCheck is a test seam: the identity function in
// production, it lets rename_test.go inject an edit into the REAL edits
// Prepare is about to pass to verifyStagedSyntaxIsValid — through a REAL
// end-to-end gopls session — proving that call is actually wired in
// (code-review finding, codex round 1 HIGH #2: the 4 pure unit tests for
// verifyStagedSyntaxIsValid never exercised whether Prepare actually
// calls it; ablating the production call left the real-gopls suite
// green).
var beforeStagedSyntaxCheck = func(edits []FileEdit) []FileEdit { return edits }

// Prepare runs a read-only gopls rename session and validates its
// response into a Plan. rootFd MUST already be an open descriptor
// identifying req.SourceDir itself (invariant 4: "descriptor-pinned
// parent-directory... held open across Prepare->Apply" — the caller
// opens it once via workspace.OpenRoot); Prepare verifies this identity
// explicitly up front (code-review finding, codex, round 1: an
// honest-but-buggy caller passing a mismatched rootFd/SourceDir pair
// must be refused, not silently trusted) and does not open or close
// rootFd itself.
//
// (An earlier version of this fix tried resolving every operation
// through /proc/self/fd/<rootFd> instead of req.SourceDir's own
// pathname, to bind everything to rootFd's identity regardless of any
// later pathname swap. That broke RunGoplsRename's own internal
// snapshot copy: its tree-walk Lstat's its root argument and, seeing
// /proc/self/fd/N is structurally a symlink, refuses to descend into it
// at all — the SAME fail-closed "never resolve a symlink" discipline
// that protects it everywhere else. Reworking that walk to special-case
// a magic-link ROOT (while still refusing every symlink found DURING
// the walk) would touch already-converged Slice 0 code for this one
// caller's benefit, so this piece takes a narrower path instead.)
//
// verifyRootIdentity only proves rootFd and req.SourceDir's pathname
// were the same directory AT THE INSTANT it runs — it says nothing
// about a swap that happens AFTER. Prepare therefore re-verifies it a
// SECOND time immediately after RunGoplsRename returns (bracketing the
// single longest, highest-value operation in this function — code-
// review finding, codex, round 2 HIGH), and separately brackets the
// ENTIRE window from before RunGoplsRename is called through every
// preimage read afterward with two whole-tree content digests
// (runner.DigestTreeFiles(req.SourceDir), the same fail-closed,
// symlink-refusing walk RunGoplsRename's own internal snapshot uses),
// refusing if they differ — catching in-place CONTENT drift within a
// directory that stayed the same the whole time. Together these catch
// "req.SourceDir now names a different directory" (the identity
// re-check) and "req.SourceDir's own content changed" (the digest
// sandwich) as two DISTINCT failure modes, each independently.
//
// That still leaves the specific ABA where content changes and reverts
// ENTIRELY within RunGoplsRename's own call — invisible to a two-point
// digestBefore/digestAfter comparison bracketing the WHOLE call (code-
// review finding, codex, round 3 HIGH). Closing it does NOT need a
// held lock or any change to runner's internals: RunGoplsRename already
// returns res.SnapshotDigest, the digest of the EXACT tree its own
// internal copy captured — comparing that against digestBefore proves
// nothing changed between Prepare's own pre-check and gopls's copy
// moment specifically, which is exactly the window an outer sandwich
// can't see into. Prepare also retains digestBefore's PER-FILE digest
// list and re-checks each captured preimage against its own entry,
// attributing a drift to the specific file rather than only the
// aggregate. Together with the identity re-check and the final
// digestAfter sandwich, every window in this function is now covered by
// at least one specific, attributable check — no residual ABA ceiling
// remains for a single Prepare call.
func Prepare(ctx context.Context, rootFd int, backend *sandbox.Bwrap, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal, req runner.RenameRequest) (Plan, error) {
	if err := verifyRootIdentity(rootFd, req.SourceDir); err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}

	filesBefore, digestBefore, err := runner.DigestTreeFiles(req.SourceDir)
	if err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: digesting workspace before gopls analysis: %w", err)
	}
	beforeDigestByRelPath := make(map[string]string, len(filesBefore))
	for _, fd := range filesBefore {
		beforeDigestByRelPath[fd.Path] = fd.SHA256
	}

	beforeGoplsRename() // test seam: inject a change to req.SourceDir here, before gopls's own internal snapshot copy

	res, err := runner.RunGoplsRename(ctx, backend, report, grants, j, req)
	if err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: gopls rename: %w", err)
	}
	if res.Error != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: gopls reported an LSP error (code=%d): %s", res.Error.Code, res.Error.Message)
	}
	afterGoplsRename() // test seam: inject a concurrent write to req.SourceDir here

	// Re-verify identity immediately after the (typically longest)
	// RunGoplsRename call, before trusting anything it returned or
	// reading any preimage through rootFd — catches req.SourceDir's own
	// pathname having been swapped to point at a DIFFERENT directory
	// during that call, which the initial check above cannot (code-
	// review finding, codex, round 2 HIGH).
	if err := verifyRootIdentity(rootFd, req.SourceDir); err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: re-verifying after gopls rename: %w", err)
	}

	// res.SnapshotDigest is the digest of the EXACT tree RunGoplsRename's
	// own internal copy captured — comparing it against digestBefore
	// closes an ABA the endpoint digest-before/digest-after sandwich
	// alone cannot: req.SourceDir swapped to different content, snapshotted
	// by gopls in that state, then swapped BACK before this function's own
	// digestAfter runs (code-review finding, codex, round 3 HIGH — the
	// data needed to close this was already being returned and simply
	// never compared).
	if res.SnapshotDigest != digestBefore {
		return Plan{}, fmt.Errorf("symedit: prepare: gopls's own snapshot digest (%s) does not match the pre-analysis whole-tree digest (%s) — %s changed exactly during gopls's internal snapshot copy (fail closed)", res.SnapshotDigest, digestBefore, req.SourceDir)
	}

	// Discover the touched file set (a loose, non-authoritative pass —
	// see discover.go) so preimages can be read BEFORE the authoritative,
	// strict ParseWorkspaceEdit call below, which requires them up front.
	touched, err := ExtractTouchedRelPaths(res.Result, runner.GoplsSandboxRoot)
	if err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}

	preimages := make(map[string]Preimage, len(touched))
	contentByRelPath := make(map[string][]byte, len(touched))
	for _, relPath := range touched {
		content, dev, ino, mode, err := captureRelPathBeneath(rootFd, relPath)
		if err != nil {
			return Plan{}, fmt.Errorf("symedit: prepare: reading preimage of %q: %w", relPath, err)
		}
		wantDigest, ok := beforeDigestByRelPath[relPath]
		if !ok {
			return Plan{}, fmt.Errorf("symedit: prepare: %q was not present in the pre-analysis snapshot (fail closed)", relPath)
		}
		if got := sha256Hex(content); got != wantDigest {
			return Plan{}, fmt.Errorf("symedit: prepare: %q changed between the pre-analysis digest and this read (fail closed)", relPath)
		}
		preimages[relPath] = Preimage{Content: content, Dev: dev, Ino: ino, Mode: mode}
		contentByRelPath[relPath] = content
	}

	// The LAST check before trusting any of the above: bracket the whole
	// window (gopls call + every preimage read just performed) with a
	// second whole-tree digest.
	_, digestAfter, err := runner.DigestTreeFiles(req.SourceDir)
	if err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: digesting workspace after gopls analysis: %w", err)
	}
	if digestBefore != digestAfter {
		return Plan{}, fmt.Errorf("symedit: prepare: %s changed during gopls analysis (fail closed — the WorkspaceEdit may have been computed against different bytes than the ones just read)", req.SourceDir)
	}

	edits, err := ParseWorkspaceEdit(res.Result, runner.GoplsSandboxRoot, req.FileRelPath, contentByRelPath)
	if err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}
	if err := verifyEditsReplaceRequestedName(edits, req.NewName); err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}
	if err := verifyEditsReplaceSameOldText(edits, preimages); err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}
	edits = beforeStagedSyntaxCheck(edits) // test seam: no-op in production

	if err := verifyStagedSyntaxIsValid(edits, preimages); err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}

	if err := verifyStagedTypeChecks(ctx, backend, report, grants, j, req.SourceDir, req.GoBinary, digestAfter, edits, preimages,
		req.TypeCheckOperationID, req.TypeCheckTargetID, req.RunID, req.ProfileID); err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: %w", err)
	}

	planDigest, err := computePlanDigest(req.NewName, req.FileRelPath, edits, preimages)
	if err != nil {
		return Plan{}, fmt.Errorf("symedit: prepare: computing plan digest: %w", err)
	}

	return Plan{
		NewName:           req.NewName,
		OpenedFileRelPath: req.FileRelPath,
		Edits:             edits,
		Preimages:         preimages,
		PlanDigest:        planDigest,
		SnapshotDigest:    res.SnapshotDigest,
		ToolchainDigest:   res.ToolchainDigest,
		PolicyHash:        res.PolicyHash,
	}, nil
}

// verifyEditsReplaceRequestedName refuses the plan unless EVERY TextEdit
// replaces text with EXACTLY newName. ParseWorkspaceEdit validates the
// edit RANGES (no overlap, in-bounds) but never checks WHAT they replace
// text WITH — without this, a malformed or buggy gopls response could
// return a DIFFERENT replacement (e.g. "Baz" when the caller asked for
// "Bar") and every downstream check (plan_digest, preimage
// re-verification, ApplyGoverned's own ExpectedBefore* checks) would
// faithfully bind and commit that WRONG mutation, while the preview and
// plan.NewName keep claiming the ORIGINALLY REQUESTED name the entire
// time (code-review finding, codex, round 3 HIGH — live-reproduced via
// an adversarial gopls-response overlay).
func verifyEditsReplaceRequestedName(edits []FileEdit, newName string) error {
	for _, fe := range edits {
		for _, te := range fe.Edits {
			if te.NewText != newName {
				return fmt.Errorf("gopls's own edit for %q replaces text with %q, not the requested new_name %q (fail closed)", fe.RelPath, te.NewText, newName)
			}
		}
	}
	return nil
}

// verifyEditsReplaceSameOldText refuses the plan unless every edit's
// pre-rename text (the preimage bytes at TextEdit.StartByte:EndByte) is
// IDENTICAL across every edit. verifyEditsReplaceRequestedName alone is
// not enough: two edits could both write the SAME requested NewName
// while replacing two DIFFERENT original identifiers — a plan that
// silently folds two distinct symbols into one rename, not the single-
// symbol rename the caller asked for (code-review finding, codex, round
// 4 HIGH, live-reproduced via an adversarial gopls-response overlay: a
// plan with conflicting old identifiers but consistent NewText was
// committed uncaught). tool.go's own originalSymbolName degrades this
// SAME inconsistency gracefully for the PREVIEW TEXT (returns ok=false
// rather than guessing) — this is the corresponding SAFETY gate that
// actually refuses the plan, not just the display hint.
func verifyEditsReplaceSameOldText(edits []FileEdit, preimages map[string]Preimage) error {
	old := ""
	found := false
	for _, fe := range edits {
		pre, ok := preimages[fe.RelPath]
		if !ok {
			continue
		}
		for _, te := range fe.Edits {
			if te.StartByte < 0 || te.EndByte > len(pre.Content) || te.StartByte >= te.EndByte {
				continue
			}
			this := string(pre.Content[te.StartByte:te.EndByte])
			if this == "" {
				continue
			}
			if !found {
				old, found = this, true
				continue
			}
			if this != old {
				return fmt.Errorf("gopls's own edits replace inconsistent original text (%q and %q) — not a single-symbol rename (fail closed)", old, this)
			}
		}
	}
	return nil
}

// verifyStagedSyntaxIsValid is invariant 3's own explicit requirement,
// narrowed to syntax only (see this file's own package doc comment for
// the full scope note and why gofmt-cleanliness was dropped): stages
// every edit via ApplyEdits — the SAME function Apply itself uses, so
// this checks EXACTLY the bytes Apply will later write, not a
// separately-computed approximation — and refuses the whole Plan unless
// the result parses as a COMPLETE Go source file via go/parser.ParseFile
// (not go/format.Source — code-review finding, codex round 2 HIGH #1:
// format.Source's own documented contract accepts a bare list of
// declarations or statements as "valid," not only a complete file, so it
// fails OPEN for exactly the class of corruption — e.g. a byte offset
// severe enough to delete the package clause — this check exists to
// catch).
func verifyStagedSyntaxIsValid(edits []FileEdit, preimages map[string]Preimage) error {
	for _, fe := range edits {
		pre, ok := preimages[fe.RelPath]
		if !ok {
			return fmt.Errorf("%s has no preimage recorded (fail closed)", fe.RelPath)
		}
		after, err := ApplyEdits(pre.Content, fe.Edits)
		if err != nil {
			return fmt.Errorf("staging %s: %w", fe.RelPath, err)
		}
		// go/parser.ParseFile, not go/format.Source (code-review finding,
		// codex round 2 HIGH #1): format.Source's own documented contract
		// accepts EITHER a complete file OR a bare list of declarations OR
		// a bare list of statements — it exists to format snippets, not to
		// validate a COMPLETE .go file. A byte-offset bug severe enough to
		// delete the package clause (or otherwise reduce the file to a
		// declaration/statement fragment) would still make format.Source
		// return nil, silently failing OPEN exactly where this check
		// exists to fail closed. ParseFile has no such fragment leniency —
		// it always requires a complete source file.
		if _, err := parser.ParseFile(token.NewFileSet(), "", after, 0); err != nil {
			return fmt.Errorf("%s does not parse as a complete Go source file after staging the edit (fail closed): %w", fe.RelPath, err)
		}
	}
	return nil
}

// planMutations (below) recomputes plan's own PlanDigest and refuses if
// it does not match plan.PlanDigest — anything about the plan changed
// since Prepare, by tampering or corruption, is caught here before
// anything else runs. It then builds one workspace.FileMutation per
// edited file, computing After via ApplyEdits against the EXACT preimage
// bytes Prepare captured (never a fresh independent read — PlanDigest
// just proved that bytes-identity held), and carries the preimage's
// digest, identity, AND mode into the mutation's Expected* fields so
// workspace.Apply's OWN capture — happening later, at whatever moment it
// actually reads the file — is checked against ALL THREE, at that exact
// point (invariant 3: "Apply re-hashes every preimage immediately before
// writing... any drift invalidates the prepared plan"; invariant 4:
// "content, mode, and metadata expectations are checked explicitly...
// independent of inode", "the PRE-EDIT inode is a PRECONDITION check at
// Apply time" — code-review finding, codex, round 2: content alone does
// not catch an atomic replace-with-identical-bytes that changes only
// identity, or a content-preserving chmod).
//
// There is deliberately no package-private "pure, ungoverned apply"
// wrapper around workspace.Apply here any more (code-review finding,
// codex round 3 HIGH #1, round 4 HIGH #1: invariant 2 requires every
// dispatched workspace.Apply effect to be S7-governed, and a raw
// cross-package call from this package — even a private one only this
// package could reach — was itself the reachable bypass; workspace.Apply
// is now unexported too, so ApplyGoverned is the ONLY path from any
// package into a real mutation). This package's own tests that used to
// exercise the pure precondition-verification logic now call
// planMutations directly (no filesystem write involved in a refusal
// anyway) or ApplyGoverned (for the one detector that genuinely needs a
// real governed write attempt).

// ApplyGoverned is Apply's real S7/journal-governed counterpart (PLAN-
// CODING-TRIO.md invariants 2 and 4 step 2): the SAME plan-digest
// verification and mutation set Apply itself uses, applied through
// workspace.ApplyGoverned's durable S7 grant lifecycle instead of a
// caller-supplied bindDurable stub — a genuine, exact-intent AttemptGrant
// (its operation identity derives from the journal's own bound profile +
// workspace root identity + plan.PlanDigest, see
// workspace.ApplyOperationID), Consumed with a durable
// workspace.apply_started companion naming the sealed bundle before any
// file is written, now backs every RenameSymbol Apply. Makes exactly ONE
// attempt — see workspace.ApplyGoverned's own doc comment for why it
// does not drive its own retry loop, and workspace.UpdateMutationsAfterRollback
// for how a caller retries after a verified-clean-rollback failure.
func ApplyGoverned(ctx context.Context, rootFd int, store *sealedstore.Store, plan Plan, grants *s7.Authority, j *journal.Journal, runID contracts.RunID) (workspace.Result, error) {
	mutations, err := planMutations(plan)
	if err != nil {
		return workspace.Result{}, err
	}
	return workspace.ApplyGoverned(ctx, rootFd, store, mutations, grants, j, plan.PlanDigest, runID)
}

// planMutations recomputes plan's PlanDigest — refusing on mismatch,
// since anything about the plan changed since Prepare, by tampering or
// corruption, must be caught before anything else runs — and builds the
// workspace.FileMutation set both Apply and ApplyGoverned apply. It
// computes each file's After via ApplyEdits against the EXACT preimage
// bytes Prepare captured (never a fresh independent read — PlanDigest
// just proved that bytes-identity held), and carries the preimage's
// digest, identity, AND mode into the mutation's Expected* fields so
// workspace.Apply's OWN capture — happening later, at whatever moment it
// actually reads the file — is checked against ALL THREE, at that exact
// point (invariant 3: "Apply re-hashes every preimage immediately before
// writing... any drift invalidates the prepared plan"; invariant 4:
// "content, mode, and metadata expectations are checked explicitly...
// independent of inode", "the PRE-EDIT inode is a PRECONDITION check at
// Apply time" — code-review finding, codex, round 2: content alone does
// not catch an atomic replace-with-identical-bytes that changes only
// identity, or a content-preserving chmod).
func planMutations(plan Plan) ([]workspace.FileMutation, error) {
	if len(plan.Edits) == 0 {
		return nil, fmt.Errorf("symedit: apply: plan has no edits")
	}

	gotDigest, err := computePlanDigest(plan.NewName, plan.OpenedFileRelPath, plan.Edits, plan.Preimages)
	if err != nil {
		return nil, fmt.Errorf("symedit: apply: recomputing plan digest: %w", err)
	}
	if gotDigest != plan.PlanDigest {
		return nil, fmt.Errorf("symedit: apply: plan digest mismatch (got %s, want %s) — the plan was altered since Prepare (fail closed)", gotDigest, plan.PlanDigest)
	}

	mutations := make([]workspace.FileMutation, 0, len(plan.Edits))
	for _, fe := range plan.Edits {
		pre, ok := plan.Preimages[fe.RelPath]
		if !ok {
			return nil, fmt.Errorf("symedit: apply: %s has no preimage recorded in the plan (fail closed)", fe.RelPath)
		}

		after, err := ApplyEdits(pre.Content, fe.Edits)
		if err != nil {
			return nil, fmt.Errorf("symedit: apply: %s: %w", fe.RelPath, err)
		}

		relDir, baseName := splitRelPath(fe.RelPath)
		mutations = append(mutations, workspace.FileMutation{
			RelDir: relDir, BaseName: baseName, After: after, Mode: pre.Mode,
			ExpectedBeforeDigest:  sha256Hex(pre.Content),
			ExpectedBeforeDev:     pre.Dev,
			ExpectedBeforeIno:     pre.Ino,
			CheckExpectedIdentity: true,
			ExpectedBeforeMode:    pre.Mode,
			CheckExpectedMode:     true,
		})
	}
	return mutations, nil
}

// verifyRootIdentity confirms rootFd and sourceDir name the SAME
// directory (device+inode) at THIS instant — a sanity check against an
// honest caller's mistake (e.g. passing a rootFd opened for the wrong
// tree), not on its own a full security boundary (a fully adversarial
// caller controlling both values could trivially choose consistent-but-
// wrong ones regardless). Prepare calls this at TWO points (before and
// immediately after RunGoplsRename) — each call only proves the pairing
// was correct at that one instant, not continuously; see Prepare's own
// doc comment for what that combination does and does not close.
func verifyRootIdentity(rootFd int, sourceDir string) error {
	var rootSt, sourceSt unix.Stat_t
	if err := unix.Fstat(rootFd, &rootSt); err != nil {
		return fmt.Errorf("could not stat rootFd: %w", err)
	}
	if err := unix.Stat(sourceDir, &sourceSt); err != nil {
		return fmt.Errorf("could not stat SourceDir %q: %w", sourceDir, err)
	}
	if rootSt.Dev != sourceSt.Dev || rootSt.Ino != sourceSt.Ino {
		return fmt.Errorf("rootFd and req.SourceDir do not identify the same directory (fail closed)")
	}
	return nil
}

// planDigestInput is the canonical, deterministic serialization
// computePlanDigest hashes — encoding/json marshals map keys in sorted
// order, so this is stable regardless of Go map iteration order.
type planDigestInput struct {
	NewName           string                      `json:"new_name"`
	OpenedFileRelPath string                      `json:"opened_file_rel_path"`
	Edits             []FileEdit                  `json:"edits"`
	Preimages         map[string]preimageDigestIn `json:"preimages"`
}

// preimageDigestIn captures everything Apply actually consumes from a
// Preimage — content (via a FRESHLY computed digest, never Preimage's
// own non-existent stored field), identity, and mode — so tampering
// with ANY of those after Prepare changes the recomputed PlanDigest.
type preimageDigestIn struct {
	ContentDigest string `json:"content_digest"`
	Dev           uint64 `json:"dev"`
	Ino           uint64 `json:"ino"`
	Mode          uint32 `json:"mode"`
}

func computePlanDigest(newName, openedFileRelPath string, edits []FileEdit, preimages map[string]Preimage) (string, error) {
	canon := make(map[string]preimageDigestIn, len(preimages))
	for relPath, pre := range preimages {
		canon[relPath] = preimageDigestIn{
			ContentDigest: sha256Hex(pre.Content),
			Dev:           pre.Dev, Ino: pre.Ino,
			Mode: uint32(pre.Mode.Perm()),
		}
	}
	raw, err := json.Marshal(planDigestInput{
		NewName: newName, OpenedFileRelPath: openedFileRelPath,
		Edits: edits, Preimages: canon,
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

// captureRelPathBeneath is Prepare's own descriptor-relative capture
// (content + identity + mode, from one atomic open) of a slash-separated,
// workspace-relative path.
func captureRelPathBeneath(rootFd int, relPath string) (content []byte, dev, ino uint64, mode fs.FileMode, err error) {
	relDir, baseName := splitRelPath(relPath)
	dirFd, err := workspace.WalkDirBeneath(rootFd, relDir)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer unix.Close(dirFd)
	return workspace.CaptureFileBeneath(dirFd, baseName)
}

// splitRelPath splits a clean, slash-separated relative path into its
// directory (possibly "") and base name — the shape WalkDirBeneath/
// WriteFileBeneath/CaptureFileBeneath expect (a single directory-walk
// plus a single final component, never a whole path resolved in one
// path-string call).
func splitRelPath(relPath string) (relDir, baseName string) {
	if idx := strings.LastIndexByte(relPath, '/'); idx >= 0 {
		return relPath[:idx], relPath[idx+1:]
	}
	return "", relPath
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

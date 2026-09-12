//go:build linux

// typecheck.go is invariant 3's remaining Prepare-phase requirement,
// "format+type-check the staged result" — the TYPE-check half.
// verifyStagedSyntaxIsValid (rename.go) already covers the format/syntax
// half (parses as complete Go, never a fragment). This half stages every
// edit into a disposable snapshot — never the live workspace — and runs
// it through Slice 0's already-governed runner.Run: a real, genuinely
// compile-only check (`go test -vet=off -c -o /dev/null ./...`), catching
// unresolved references, duplicate declarations, and any other semantic
// defect syntax-only parsing can't — without ever EXECUTING any of the
// staged code.
//
// Three rounds were needed to reach a command that is actually
// compile-only, each converging on a REAL bug two of three reviewers
// independently live-reproduced:
//   - round 1 (codex + kilo): `go build ./...` never compiles `_test.go`
//     files at all — a rename touching a test file (ordinary: renaming a
//     symbol tests reference) could stage an undefined reference there
//     and this check returned nil anyway (FALSE GREEN).
//   - round 2 (codex + kilo, AGAIN both independently live-reproduced):
//     the round-1 fix (`go vet ./...`) type-checks tests too, but ALSO
//     runs go vet's full default analyzer set (printf, buildtag, etc.) —
//     so ANY unrelated, pre-existing analyzer finding anywhere in the
//     staged tree refused an otherwise perfectly valid rename. `go test
//     -run=^$` alone does not help either — empirically verified it runs
//     the SAME limited vet subset internally by default.
//   - round 3 (codex, live-reproduced): the round-2 fix (`go test
//     -vet=off -run=^$ -count=1 ./...`) disables analysis correctly, but
//     `-run=^$` only suppresses ORDINARY test functions — the generated
//     test BINARY still runs, so package init() and any TestMain still
//     EXECUTE. A staged, fully valid rename next to a compiling TestMain
//     that calls os.Exit(7) (or panics, hangs, or has any other runtime
//     side effect) was refused for reasons having nothing to do with
//     type-correctness.
//
// `go test -vet=off -c -o /dev/null ./...` closes all three: `-c`
// compiles the test binary (every file, tests included) but never runs
// it — verified empirically against real module fixtures covering all
// three prior failure modes simultaneously (undefined test-file
// reference still refused; unrelated pre-existing vet finding no longer
// refused; a TestMain that would exit nonzero if executed no longer
// refused, because it never executes). `/dev/null` is available inside
// the sandbox (internal/sandbox/probe.go's own `--dev /dev` bwrap bind).
//
// round 1 HIGH finding (codex, live-reproduced end-to-end through a real
// gopls session + sandbox + S7 + Prepare + ApplyGoverned): this
// function's own runner.CreateSnapshot(sourceDir) call is a NEW read of
// the source tree from disk that Prepare's own digestBefore/digestAfter
// sandwich (guarding every OTHER read in Prepare) does not cover — a
// caller could swap sourceDir's pathname to a different, clean tree
// after Prepare's digest checks but before this function's own snapshot,
// making a broken original workspace look like it "type-checks," then
// have ApplyGoverned commit the edit to the ORIGINAL (still broken)
// descriptor-pinned tree. Fixed: the caller passes the SAME
// digestBefore/digestAfter Prepare already computed; this function
// refuses unless its OWN CreateSnapshot's digest matches it exactly —
// the identical "compare against the already-authenticated digest"
// technique Prepare already uses for RunGoplsRename's own
// res.SnapshotDigest.
package symedit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/coding/runner"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/sandbox"
)

// DefaultTypeCheckTimeout bounds the staged, analyzer-free compile call
// issued during Prepare. topknot: a fixed ceiling, not caller-configurable
// yet — no caller has needed a longer one; raise this (or make it a
// Prepare parameter) only once one does.
const DefaultTypeCheckTimeout = 60 * time.Second

// beforeStagedBuild is a test seam: a no-op in production, it lets a
// test inspect or mutate the disposable staged snapshot directory
// immediately before the governed compile runs against it — proving it
// actually runs against these bytes, not a separately-computed
// approximation (same beforeX seam pattern as beforeStagedSyntaxCheck/
// beforeGoplsRename elsewhere in this package).
var beforeStagedBuild = func(snapDir string) {}

// verifyStagedTypeChecks stages every edit (via ApplyEdits — the SAME
// function Apply itself later uses, so this checks EXACTLY the bytes
// that will be written, not a separately-computed approximation) into a
// fresh runner.CreateSnapshot of sourceDir, then refuses unless
// `go test -vet=off -c -o /dev/null ./...` exits clean against that
// staged result. wantDigest MUST
// be the caller's own already-authenticated whole-tree digest of
// sourceDir (Prepare's digestBefore/digestAfter) — this function refuses
// if its own fresh snapshot of sourceDir does not match it exactly,
// closing the gap between whatever Prepare already proved and whatever
// this function's own later, independent read of the same directory
// actually observes (see this file's own package doc comment, round 1
// HIGH finding).
func verifyStagedTypeChecks(ctx context.Context, backend *sandbox.Bwrap, report sandbox.ProbeReport, grants *s7.Authority, j *journal.Journal, sourceDir, goBinary, wantDigest string, edits []FileEdit, preimages map[string]Preimage, opID contracts.OperationID, targetID contracts.TargetID, runID contracts.RunID, profileID contracts.ProfileID) error {
	if goBinary == "" {
		return fmt.Errorf("symedit: type-check: GoBinary is required (fail closed)")
	}
	if wantDigest == "" {
		return fmt.Errorf("symedit: type-check: wantDigest is required (fail closed)")
	}
	if !opID.Valid() || !targetID.Valid() || !runID.Valid() || !profileID.Valid() {
		return fmt.Errorf("symedit: type-check: OperationID/TargetID/RunID/ProfileID are all required (fail closed)")
	}

	snap, cleanup, err := runner.CreateSnapshot(sourceDir)
	if err != nil {
		return fmt.Errorf("symedit: type-check: snapshot: %w", err)
	}
	defer cleanup()
	if snap.Digest != wantDigest {
		return fmt.Errorf("symedit: type-check: %s's own digest (%s) does not match the already-authenticated pre-analysis digest (%s) — it changed, or was swapped, since Prepare last verified it (fail closed)", sourceDir, snap.Digest, wantDigest)
	}

	for _, fe := range edits {
		pre, ok := preimages[fe.RelPath]
		if !ok {
			return fmt.Errorf("symedit: type-check: %s has no preimage recorded (fail closed)", fe.RelPath)
		}
		after, err := ApplyEdits(pre.Content, fe.Edits)
		if err != nil {
			return fmt.Errorf("symedit: type-check: staging %s: %w", fe.RelPath, err)
		}
		dst, err := snapshotFilePath(snap.Dir, fe.RelPath)
		if err != nil {
			return fmt.Errorf("symedit: type-check: %w", err)
		}
		if err := os.WriteFile(dst, after, pre.Mode.Perm()); err != nil {
			return fmt.Errorf("symedit: type-check: staging %s into snapshot: %w", fe.RelPath, err)
		}
	}
	beforeStagedBuild(snap.Dir) // test seam: no-op in production

	result, runErr := runner.Run(ctx, backend, report, grants, j, runner.RunSpec{
		SourceDir:   snap.Dir,
		Args:        []string{"test", "-C", runner.GoplsSandboxRoot, "-vet=off", "-c", "-o", "/dev/null", "./..."},
		GoBinary:    goBinary,
		Timeout:     DefaultTypeCheckTimeout,
		OperationID: opID,
		TargetID:    targetID,
		RunID:       runID,
		ProfileID:   profileID,
	})
	if runErr != nil {
		return fmt.Errorf("symedit: type-check: staged compile did not run: %w", runErr)
	}
	if !result.ExitOK {
		return fmt.Errorf("symedit: type-check: staged result does not type-check (fail closed):\n%s", result.Output)
	}
	return nil
}

// snapshotFilePath resolves relPath against snapDir, refusing anything
// that would escape it — defense-in-depth alongside ParseWorkspaceEdit's
// own RelPath validation (never trust a single validation point twice
// removed from where the path is actually used, matching this
// codebase's own validateBaseName/validateFileRelPath precedent).
func snapshotFilePath(snapDir, relPath string) (string, error) {
	if relPath == "" || strings.HasPrefix(relPath, "/") {
		return "", fmt.Errorf("%q is not a clean relative path (fail closed)", relPath)
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("%q escapes the snapshot root (fail closed)", relPath)
	}
	full := filepath.Join(snapDir, clean)
	if full != snapDir && !strings.HasPrefix(full, snapDir+string(filepath.Separator)) {
		return "", fmt.Errorf("%q escapes the snapshot root (fail closed)", relPath)
	}
	return full, nil
}

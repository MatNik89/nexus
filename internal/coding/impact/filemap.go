package impact

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// FileChange describes one file's change between a base and candidate
// tree, as produced by DiffFileMaps from two snapshot digest maps
// (runner.DigestTreeFiles). Path is relative to the workspace root both
// snapshots were taken from.
type FileChange struct {
	Path string
	Kind ChangeKind
}

// ChangeKind classifies one FileChange.
type ChangeKind int

const (
	ChangeUnknown ChangeKind = iota
	ChangeAdded
	ChangeModified
	ChangeDeleted
)

// DiffFileMaps compares two relPath->contentDigest maps (each built from
// a runner.DigestTreeFiles result) and returns every file that changed.
// A file present in both maps with an unchanged digest is not reported.
func DiffFileMaps(base, candidate map[string]string) []FileChange {
	var changes []FileChange
	for path, baseDigest := range base {
		candDigest, inCandidate := candidate[path]
		switch {
		case !inCandidate:
			changes = append(changes, FileChange{Path: path, Kind: ChangeDeleted})
		case candDigest != baseDigest:
			changes = append(changes, FileChange{Path: path, Kind: ChangeModified})
		}
	}
	for path := range candidate {
		if _, inBase := base[path]; !inBase {
			changes = append(changes, FileChange{Path: path, Kind: ChangeAdded})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

// buildControlFiles are workspace-wide build-configuration files: a
// change to any of them can alter which packages exist or how they
// resolve, in ways this package's pure per-file mapping cannot model —
// PLAN-CODING-TRIO.md invariant 5 requires falling back to the full
// suite rather than guessing.
var buildControlFiles = map[string]bool{
	"go.mod":      true,
	"go.sum":      true,
	"go.work":     true,
	"go.work.sum": true,
}

// FileIndex maps a workspace-relative file path to the import path of the
// package that declares it (as a GoFiles/TestGoFiles/XTestGoFiles/
// EmbedFiles/TestEmbedFiles/XTestEmbedFiles entry), built from a single
// `go list -test -json` package listing — the CANDIDATE tree's listing,
// since that is the tree ResolveChanges resolves changes against.
//
// A union of base+candidate listings was considered (and initially
// implemented) but rejected on review (agy, Slice 2 review): a deleted
// file never reaches the index at all (ResolveChanges fails closed on
// ChangeDeleted before any lookup), and an added file is already present
// in the candidate listing's own package records — so base contributes
// nothing a union would need, while introducing a real stale-mapping
// hazard (a file no longer declared by any candidate package, e.g. a
// removed //go:embed line, would keep resolving to its OLD, no-longer-
// accurate base-side owner instead of correctly falling back as
// unmapped).
type FileIndex struct {
	root      string
	fileToPkg map[string]string
}

// BuildFileIndex indexes every package record's declared files
// (ForTest=="" and not the synthetic go-test-binary main package — see
// isSyntheticTestMain) against root, the absolute workspace root every
// FileChange.Path is relative to. A package directory outside root
// (stdlib, GOMODCACHE, or any other external dependency pulled in by
// `go list -deps`) is silently skipped, never indexed — verified live
// against a real `go list -deps -test -json` run: filepath.Rel does NOT
// error on Unix for a target outside root, it silently returns a
// "../"-prefixed path (code-review finding, codex+agy Slice 2 review,
// independently converged) — an unguarded index would otherwise pollute
// fileToPkg with thousands of non-workspace files under escaping keys.
func BuildFileIndex(pkgs []Package, root string) (*FileIndex, error) {
	idx := &FileIndex{root: root, fileToPkg: map[string]string{}}
	for _, p := range pkgs {
		if p.ForTest != "" || isSyntheticTestMain(p) {
			continue
		}
		relDir, err := filepath.Rel(root, p.Dir)
		if err != nil {
			return nil, fmt.Errorf("impact: package %s dir %q not under root %q: %w", p.ImportPath, p.Dir, root, err)
		}
		if relDir == ".." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) {
			continue // external package outside the workspace root — not ours to index
		}
		for _, files := range [][]string{p.GoFiles, p.TestGoFiles, p.XTestGoFiles, p.EmbedFiles, p.TestEmbedFiles, p.XTestEmbedFiles} {
			for _, f := range files {
				idx.fileToPkg[filepath.Join(relDir, f)] = p.ImportPath
			}
		}
	}
	return idx, nil
}

// ResolveChanges maps changes to the set of package import paths that
// must be considered changed, or reports fallbackReason and ok=false
// when any change cannot be safely classified — PLAN-CODING-TRIO.md
// invariant 5 (fail-safe toward the full suite, never a partial answer):
//   - any deletion (the deleted file's owning package may no longer
//     build the same way; TIA has no positive signal to reason about)
//   - any build-control file (go.mod/go.sum/go.work/go.work.sum)
//   - any file this index cannot map to a declared package file (outside
//     every known package's GoFiles/TestGoFiles/XTestGoFiles/embed sets —
//     including a change under a brand-new, unindexed package directory)
func (idx *FileIndex) ResolveChanges(changes []FileChange) (changedPackages []string, fallbackReason string, ok bool) {
	changed := map[string]bool{}
	for _, c := range changes {
		switch c.Kind {
		case ChangeAdded, ChangeModified:
			// handled below
		case ChangeDeleted:
			return nil, fmt.Sprintf("deleted file: %s", c.Path), false
		default:
			// Fail-closed on any unrecognized/zero-value Kind (code-review
			// finding, codex, Slice 2 review) — this repo's own hard rule
			// (CLAUDE.md: "unknown enum/kind/capability discriminator →
			// reject") applies here too: a FileChange constructed with an
			// omitted or future Kind must never be silently treated as an
			// ordinary resolvable change.
			return nil, fmt.Sprintf("unrecognized change kind %d for %s", c.Kind, c.Path), false
		}
		if buildControlFiles[filepath.Base(c.Path)] {
			return nil, fmt.Sprintf("build-control file changed: %s", c.Path), false
		}
		pkg, known := idx.fileToPkg[c.Path]
		if !known {
			return nil, fmt.Sprintf("unmapped file: %s", c.Path), false
		}
		changed[pkg] = true
	}
	for pkg := range changed {
		changedPackages = append(changedPackages, pkg)
	}
	sort.Strings(changedPackages)
	return changedPackages, "", true
}

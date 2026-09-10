package impact

import (
	"sort"
	"testing"
)

func syntheticPkgsForFileIndex(root string) []Package {
	return []Package{
		{ImportPath: "mod/a", Dir: root + "/a", GoFiles: []string{"a.go"}, TestGoFiles: []string{"a_test.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/b", Dir: root + "/b", GoFiles: []string{"b.go"}, EmbedFiles: []string{"static/data.txt"}, Match: []string{"./..."}},
		// ForTest variant of mod/a — must be skipped (redundant, same files as the base record).
		{ImportPath: "mod/a", Dir: root + "/a", ForTest: "mod/a", GoFiles: []string{"a.go", "a_test.go"}, Match: []string{"./..."}},
		// synthetic go-test-binary harness — must be skipped entirely.
		{ImportPath: "mod/a.test", Name: "main", Dir: root, GoFiles: []string{"/tmp/somewhere/generated.go"}},
	}
}

func TestBuildFileIndexResolvesDeclaredFiles(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	pkgs, reason, ok := idx.ResolveChanges([]FileChange{
		{Path: "a/a.go", Kind: ChangeModified},
		{Path: "b/static/data.txt", Kind: ChangeModified},
	})
	if !ok {
		t.Fatalf("expected ok=true, got fallbackReason=%q", reason)
	}
	sort.Strings(pkgs)
	want := []string{"mod/a", "mod/b"}
	if len(pkgs) != len(want) || pkgs[0] != want[0] || pkgs[1] != want[1] {
		t.Fatalf("ResolveChanges = %v, want %v", pkgs, want)
	}
}

func TestBuildFileIndexResolvesTestFile(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	pkgs, _, ok := idx.ResolveChanges([]FileChange{{Path: "a/a_test.go", Kind: ChangeModified}})
	if !ok || len(pkgs) != 1 || pkgs[0] != "mod/a" {
		t.Fatalf("ResolveChanges(a_test.go) = %v ok=%v, want [mod/a] true", pkgs, ok)
	}
}

// Detector (PLAN-CODING-TRIO.md invariant 5): a deleted file must force
// fallback, never a silent partial answer.
func TestResolveChangesFallsBackOnDeletion(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	_, reason, ok := idx.ResolveChanges([]FileChange{{Path: "a/a.go", Kind: ChangeDeleted}})
	if ok {
		t.Fatal("expected ok=false for a deleted file")
	}
	if reason == "" {
		t.Fatal("expected a non-empty fallbackReason")
	}
}

// Detector: a workspace-wide build-control file change must force
// fallback — it can change which packages exist or how they resolve, in
// ways this per-file mapping cannot model.
func TestResolveChangesFallsBackOnGoMod(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	_, reason, ok := idx.ResolveChanges([]FileChange{{Path: "go.mod", Kind: ChangeModified}})
	if ok {
		t.Fatal("expected ok=false for a go.mod change")
	}
	if reason == "" {
		t.Fatal("expected a non-empty fallbackReason")
	}
}

// Detector: a file this index cannot map to any declared package file —
// e.g. an added file under a brand-new, unindexed directory — forces
// fallback rather than silently ignoring it.
func TestResolveChangesFallsBackOnUnmappedFile(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	_, reason, ok := idx.ResolveChanges([]FileChange{{Path: "unknown/new.go", Kind: ChangeAdded}})
	if ok {
		t.Fatal("expected ok=false for an unmapped file")
	}
	if reason == "" {
		t.Fatal("expected a non-empty fallbackReason")
	}
}

// Detector: BuildFileIndex must skip both the ForTest variant (redundant
// restatement of the base package's own files) and the synthetic
// go-test-binary harness (its GoFiles point at a toolchain cache path
// outside the workspace, never a real source file to map).
func TestBuildFileIndexSkipsForTestVariantAndSyntheticHarness(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	// The synthetic harness's GoFiles entry is an absolute cache path, and
	// the ForTest variant redundantly restates mod/a's own files; if
	// either were indexed, the count below would be inflated (or a bogus
	// relative path derived from the cache path would appear). Assert the
	// index has exactly the 4 real declared files: a/a.go, a/a_test.go,
	// b/b.go, b/static/data.txt.
	if got := len(idx.fileToPkg); got != 4 {
		t.Fatalf("fileToPkg has %d entries, want 4: %+v", got, idx.fileToPkg)
	}
}

func TestDiffFileMapsClassifiesAddedModifiedDeleted(t *testing.T) {
	base := map[string]string{"a.go": "digest-a", "b.go": "digest-b", "c.go": "digest-c"}
	candidate := map[string]string{"a.go": "digest-a", "b.go": "digest-b2", "d.go": "digest-d"}
	got := DiffFileMaps(base, candidate)
	want := map[string]ChangeKind{"b.go": ChangeModified, "c.go": ChangeDeleted, "d.go": ChangeAdded}
	if len(got) != len(want) {
		t.Fatalf("DiffFileMaps returned %d changes, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		wantKind, known := want[c.Path]
		if !known {
			t.Fatalf("unexpected change for path %s: %+v", c.Path, c)
		}
		if c.Kind != wantKind {
			t.Fatalf("path %s: Kind = %v, want %v", c.Path, c.Kind, wantKind)
		}
	}
}

// Detector (code-review finding, codex+agy, Slice 2 review, verified live
// against a real `go list -deps -test -json` run): a package directory
// outside the workspace root (stdlib, GOMODCACHE, or any other external
// dependency pulled in by `go list -deps`) must never be indexed —
// filepath.Rel does NOT error on Unix for a target outside root, it
// silently returns a "../"-prefixed relative path.
func TestBuildFileIndexSkipsPackagesOutsideRoot(t *testing.T) {
	root := "/work/src"
	pkgs := []Package{
		{ImportPath: "mod/a", Dir: root + "/a", GoFiles: []string{"a.go"}, Match: []string{"./..."}},
		// external dependency: Dir is outside root entirely.
		{ImportPath: "fmt", Dir: "/usr/local/go/src/fmt", GoFiles: []string{"print.go"}},
	}
	idx, err := BuildFileIndex(pkgs, root)
	if err != nil {
		t.Fatal(err)
	}
	for path, pkg := range idx.fileToPkg {
		if pkg == "fmt" {
			t.Fatalf("external package fmt was indexed under escaping path %q", path)
		}
	}
	if len(idx.fileToPkg) != 1 {
		t.Fatalf("fileToPkg has %d entries, want 1 (only mod/a's a.go): %+v", len(idx.fileToPkg), idx.fileToPkg)
	}
	_, _, ok := idx.ResolveChanges([]FileChange{{Path: "../../usr/local/go/src/fmt/print.go", Kind: ChangeModified}})
	if ok {
		t.Fatal("expected ok=false for an escaping, unmapped path derived from an external package")
	}
}

// Detector (code-review finding, codex, Slice 2 review): an unrecognized
// or zero-value ChangeKind — e.g. a FileChange constructed directly with
// an omitted Kind — must fail closed like this repo's other unknown-enum
// discriminators, never be silently treated as an ordinary resolvable
// change.
func TestResolveChangesFallsBackOnUnrecognizedChangeKind(t *testing.T) {
	root := "/work/src"
	idx, err := BuildFileIndex(syntheticPkgsForFileIndex(root), root)
	if err != nil {
		t.Fatal(err)
	}
	_, reason, ok := idx.ResolveChanges([]FileChange{{Path: "a/a.go", Kind: ChangeUnknown}})
	if ok {
		t.Fatal("expected ok=false for ChangeUnknown")
	}
	if reason == "" {
		t.Fatal("expected a non-empty fallbackReason")
	}
	_, _, ok = idx.ResolveChanges([]FileChange{{Path: "a/a.go", Kind: ChangeKind(99)}})
	if ok {
		t.Fatal("expected ok=false for an out-of-range ChangeKind value")
	}
}

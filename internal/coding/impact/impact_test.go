//go:build linux

package impact

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// Detector: ParseGoList decodes a real, captured `go list -test -json`
// stream (concatenated JSON objects, NOT an array) for two real packages
// in this repo — proves the parser handles the actual output shape, not
// just a synthetic fixture.
func TestParseGoListRealSample(t *testing.T) {
	f, err := os.Open("testdata/golist_sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pkgs, err := ParseGoList(f)
	if err != nil {
		t.Fatal(err)
	}
	// Assert on every stream object, not just the first (code-review
	// CODE1 codex finding #6): a parser regression that decoded only the
	// first concatenated object must fail this test, so it must exercise
	// package identities that appear LATER in the stream too.
	var sealedstoreFound, atomicwriteFound []string
	for _, p := range pkgs {
		if p.ForTest != "" {
			continue
		}
		switch {
		case strings.HasSuffix(p.ImportPath, "sealedstore"):
			sealedstoreFound = append(sealedstoreFound, p.ImportPath)
			if !p.hasTests() {
				t.Fatalf("sealedstore package not detected as having tests: %+v", p)
			}
		case strings.HasSuffix(p.ImportPath, "atomicwrite"):
			atomicwriteFound = append(atomicwriteFound, p.ImportPath)
		}
	}
	if len(sealedstoreFound) != 1 {
		t.Fatalf("expected exactly one sealedstore base package entry, got %v", sealedstoreFound)
	}
	if len(atomicwriteFound) != 1 {
		t.Fatalf("expected exactly one atomicwrite base package entry (proves the stream was decoded past the first object), got %v", atomicwriteFound)
	}
	if len(pkgs) < 2 {
		t.Fatalf("expected at least 2 decoded stream objects, got %d", len(pkgs))
	}
}

// Detector: a malformed stream (truncated mid-object) is refused, never
// silently returns a partial package list.
func TestParseGoListRefusesMalformed(t *testing.T) {
	_, err := ParseGoList(strings.NewReader(`{"ImportPath":"a","Imports":[`))
	if err == nil {
		t.Fatal("ParseGoList accepted truncated JSON")
	}
}

func syntheticPkgs() []Package {
	return []Package{
		{ImportPath: "mod/a", Imports: []string{"fmt"}, GoFiles: []string{"a.go"}},
		{ImportPath: "mod/b", Imports: []string{"mod/a"}, GoFiles: []string{"b.go"}, TestGoFiles: []string{"b_test.go"}},
		{ImportPath: "mod/c", Imports: []string{"mod/b"}, GoFiles: []string{"c.go"}, TestGoFiles: []string{"c_test.go"}},
		{ImportPath: "mod/d", Imports: []string{"fmt"}, GoFiles: []string{"d.go"}, TestGoFiles: []string{"d_test.go"}},       // unrelated
		{ImportPath: "mod/e", Imports: []string{"mod/a"}, GoFiles: []string{"e.go"}},                                         // depends on a, no tests of its own
		{ImportPath: "mod/f", TestImports: []string{"mod/a"}, GoFiles: []string{"f.go"}, TestGoFiles: []string{"f_test.go"}}, // test-only import of a
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// Detector: a change to mod/a affects mod/b and mod/c (transitive
// dependents with tests) and mod/f (test-only importer), but NOT mod/d
// (unrelated) and NOT mod/e (depends on a but has no tests of its own).
func TestAffectedTransitiveClosure(t *testing.T) {
	g := BuildGraph(syntheticPkgs())
	got, resolved := g.Affected([]string{"mod/a"})
	if !resolved {
		t.Fatal("expected resolved=true for a known package")
	}
	want := []string{"mod/b", "mod/c", "mod/f"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/a) = %v, want %v", s, want)
	}
}

// Detector: a change to a leaf with no dependents and no tests of its own
// (mod/e) yields nothing.
func TestAffectedLeafNoTests(t *testing.T) {
	g := BuildGraph(syntheticPkgs())
	got, resolved := g.Affected([]string{"mod/e"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	if len(got) != 0 {
		t.Fatalf("Affected(mod/e) = %v, want empty", got)
	}
}

// Detector: a change to a package WITH its own tests always includes
// itself in the affected set.
func TestAffectedIncludesSelfWhenTested(t *testing.T) {
	g := BuildGraph(syntheticPkgs())
	got, _ := g.Affected([]string{"mod/b"})
	if !contains(got, "mod/b") {
		t.Fatalf("Affected(mod/b) = %v, want to include mod/b itself", got)
	}
}

// Detector (fail-safe toward closure, invariant 5): a changed path the
// Graph never saw is UNRESOLVABLE — the caller must fall back to the full
// suite; Affected must say so via resolved=false, never silently return a
// partial/empty answer that looks like "nothing affected."
func TestAffectedUnknownPackageIsUnresolved(t *testing.T) {
	g := BuildGraph(syntheticPkgs())
	_, resolved := g.Affected([]string{"mod/does-not-exist"})
	if resolved {
		t.Fatal("Affected claimed resolved=true for an unknown package")
	}
}

// Detector: a self-import in the raw Imports list (should never happen in
// real go list output, but defensively) does not create a self-loop that
// would corrupt the closure walk.
func TestBuildGraphIgnoresSelfImport(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "mod/x", Imports: []string{"mod/x"}, TestGoFiles: []string{"x_test.go"}},
	}
	g := BuildGraph(pkgs)
	got, resolved := g.Affected([]string{"mod/x"})
	if !resolved || !equalStrings(sorted(got), []string{"mod/x"}) {
		t.Fatalf("Affected(mod/x) = %v resolved=%v, want [mod/x] true", got, resolved)
	}
}

// Detector (code-review CODE1 codex finding #5): a test-only import edge
// must not propagate transitively into a production consumer that never
// depends on the changed package at all. Counterexample: mod/a2 <-test-
// mod/f2 <-production- mod/g2. mod/g2 imports mod/f2 in PRODUCTION, but
// mod/g2 has no dependency on mod/a2 whatsoever — mod/f2 only reaches
// mod/a2 through its own test file. A change to mod/a2 must select
// mod/f2's tests (mod/f2's test file genuinely imports mod/a2) but must
// NOT select mod/g2's tests.
func TestAffectedTestOnlyEdgeDoesNotPropagateTransitively(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "mod/a2", GoFiles: []string{"a2.go"}},
		{ImportPath: "mod/f2", TestImports: []string{"mod/a2"}, GoFiles: []string{"f2.go"}, TestGoFiles: []string{"f2_test.go"}},
		{ImportPath: "mod/g2", Imports: []string{"mod/f2"}, GoFiles: []string{"g2.go"}, TestGoFiles: []string{"g2_test.go"}},
	}
	g := BuildGraph(pkgs)
	got, resolved := g.Affected([]string{"mod/a2"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	want := []string{"mod/f2"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/a2) = %v, want %v (mod/g2 must NOT be included — it never depends on mod/a2)", s, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

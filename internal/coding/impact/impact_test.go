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
			if !hasTestsFixtureCheck(p) {
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

// hasTests reports whether a real `go list` package entry declares tests
// (the base record — not the synthetic ".test" main or a ForTest variant).
// Test-fixture-only: production selection now uses isTarget(), not this.
func hasTestsFixtureCheck(p Package) bool {
	return p.ForTest == "" && (len(p.TestGoFiles) > 0 || len(p.XTestGoFiles) > 0)
}

func syntheticPkgs() []Package {
	// Match: ["./..."] on every entry — these fixtures simulate real `go
	// test ./...` output, where every directly-matched workspace package
	// carries a non-empty Match regardless of whether it has test files
	// (isTarget() semantics, code-review finding, codex, Slice 2 research).
	return []Package{
		{ImportPath: "mod/a", Imports: []string{"fmt"}, GoFiles: []string{"a.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/b", Imports: []string{"mod/a"}, GoFiles: []string{"b.go"}, TestGoFiles: []string{"b_test.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/c", Imports: []string{"mod/b"}, GoFiles: []string{"c.go"}, TestGoFiles: []string{"c_test.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/d", Imports: []string{"fmt"}, GoFiles: []string{"d.go"}, TestGoFiles: []string{"d_test.go"}, Match: []string{"./..."}},       // unrelated
		{ImportPath: "mod/e", Imports: []string{"mod/a"}, GoFiles: []string{"e.go"}, Match: []string{"./..."}},                                         // depends on a, no tests of its own — still a real target
		{ImportPath: "mod/f", TestImports: []string{"mod/a"}, GoFiles: []string{"f.go"}, TestGoFiles: []string{"f_test.go"}, Match: []string{"./..."}}, // test-only import of a
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// Detector: a change to mod/a affects mod/a itself (a real go-test target
// even with no tests of its own), mod/b and mod/c (transitive dependents
// with tests), mod/e (transitive dependent with no tests of its own —
// still a real target `go test ./...` compiles and can fail on), and
// mod/f (test-only importer), but NOT mod/d (unrelated).
func TestAffectedTransitiveClosure(t *testing.T) {
	g := BuildGraph(syntheticPkgs())
	got, resolved := g.Affected([]string{"mod/a"})
	if !resolved {
		t.Fatal("expected resolved=true for a known package")
	}
	want := []string{"mod/a", "mod/b", "mod/c", "mod/e", "mod/f"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/a) = %v, want %v", s, want)
	}
}

// Detector (code-review finding, codex, Slice 2 research): a leaf with no
// dependents and no tests of its own (mod/e) is STILL included when
// changed directly — `go test ./...` compiles every directly-matched
// package regardless of test presence, so a compile error in mod/e itself
// must not go unselected.
func TestAffectedLeafNoTestsStillSelected(t *testing.T) {
	g := BuildGraph(syntheticPkgs())
	got, resolved := g.Affected([]string{"mod/e"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	if !contains(got, "mod/e") {
		t.Fatalf("Affected(mod/e) = %v, want to include mod/e itself (it is a real go-test target even with zero tests)", got)
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
		{ImportPath: "mod/x", Imports: []string{"mod/x"}, TestGoFiles: []string{"x_test.go"}, Match: []string{"./..."}},
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
		{ImportPath: "mod/a2", GoFiles: []string{"a2.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/f2", TestImports: []string{"mod/a2"}, GoFiles: []string{"f2.go"}, TestGoFiles: []string{"f2_test.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/g2", Imports: []string{"mod/f2"}, GoFiles: []string{"g2.go"}, TestGoFiles: []string{"g2_test.go"}, Match: []string{"./..."}},
	}
	g := BuildGraph(pkgs)
	got, resolved := g.Affected([]string{"mod/a2"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	// mod/a2 itself (a real go-test target) and mod/f2 (test-only importer)
	// are included; mod/g2 must NOT be included — it never depends on
	// mod/a2 at all, and (code-review finding, codex+agy) the terminal
	// test-owner edge must not leak a non-target owner into the result
	// either, so this also proves that guard independently of Finding 1.
	want := []string{"mod/a2", "mod/f2"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/a2) = %v, want %v (mod/g2 must NOT be included — it never depends on mod/a2)", s, want)
	}
}

// Detector (code-review finding, codex+agy, Slice 2 review): an owner
// reached only via a test-only import must itself be a real go-test
// target (isTarget()) to be selected. A dependency package pulled in by
// `go list -deps -test` (e.g. a non-workspace or unmatched package) can
// carry TestImports too — its test-only edge to the changed package must
// not leak it into the result just because it owns that edge.
func TestAffectedTestOnlyOwnerMustItselfBeATarget(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "mod/h", GoFiles: []string{"h.go"}, Match: []string{"./..."}},
		// mod/i is NOT a real go-test target (no Match — e.g. an
		// unmatched dependency record from `go list -deps -test`), yet it
		// still carries a test-only import of mod/h.
		{ImportPath: "mod/i", TestImports: []string{"mod/h"}, GoFiles: []string{"i.go"}, TestGoFiles: []string{"i_test.go"}},
	}
	g := BuildGraph(pkgs)
	got, resolved := g.Affected([]string{"mod/h"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	want := []string{"mod/h"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/h) = %v, want %v (mod/i must NOT be included — it is not a real go-test target)", s, want)
	}
}

// Detector (code-review finding, agy, Slice 2 review, verified against a
// real `go list -test -json` sample): the synthetic ".test" main package
// entry (ForTest=="" like a real base package, but not importable and not
// a real target) must not pollute the known set or add stdlib import
// edges into the graph.
func TestBuildGraphSkipsSyntheticTestMainPackage(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "mod/j", GoFiles: []string{"j.go"}, TestGoFiles: []string{"j_test.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/j.test", Name: "main", Imports: []string{"os", "testing", "mod/j"}},
	}
	g := BuildGraph(pkgs)
	_, resolved := g.Affected([]string{"mod/j.test"})
	if resolved {
		t.Fatal("Affected claimed resolved=true for the synthetic .test main package — it must never become a known node")
	}
	got, resolved := g.Affected([]string{"mod/j"})
	if !resolved {
		t.Fatal("expected resolved=true for mod/j")
	}
	if !equalStrings(sorted(got), []string{"mod/j"}) {
		t.Fatalf("Affected(mod/j) = %v, want [mod/j] (must not include mod/j.test)", got)
	}
}

// Detector (code-review finding, codex, Slice 2 round-2 review): the
// ".test" suffix is a naming CONVENTION for the synthetic go-test-binary
// harness, not a reserved suffix — a REAL, directly-matched workspace
// package whose own import path happens to end in ".test" (Match
// non-empty) must still be treated as a real target and kept in the
// production-edge graph, unlike the synthetic entry (which always has
// Match==nil).
func TestBuildGraphKeepsRealPackageWhoseImportPathEndsInDotTest(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "mod/k", GoFiles: []string{"k.go"}, Match: []string{"./..."}},
		// A real, directly-matched package (Match set) whose own import
		// path happens to end in ".test" — NOT the synthetic harness.
		{ImportPath: "mod/parser.test", Imports: []string{"mod/k"}, GoFiles: []string{"parser.test.go"}, Match: []string{"./..."}},
		{ImportPath: "mod/l", Imports: []string{"mod/parser.test"}, GoFiles: []string{"l.go"}, TestGoFiles: []string{"l_test.go"}, Match: []string{"./..."}},
	}
	g := BuildGraph(pkgs)
	got, resolved := g.Affected([]string{"mod/k"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	want := []string{"mod/k", "mod/l", "mod/parser.test"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/k) = %v, want %v (mod/parser.test is a REAL target, not the synthetic harness, and must not be discarded)", s, want)
	}
}

// Detector (code-review finding, codex, Slice 2 round-3 review): a real,
// non-synthetic, UNMATCHED dependency package (reached only via -deps,
// never directly matched — Match empty, like a stdlib or vendored
// package) whose import path also happens to end in ".test" must not be
// mistaken for the synthetic harness either. It carries its own package
// Name (never "main" here), so it must remain a real graph node and its
// production edges must still propagate through it.
func TestBuildGraphKeepsUnmatchedDependencyWhoseImportPathEndsInDotTest(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "mod/n", GoFiles: []string{"n.go"}, Match: []string{"./..."}},
		// An unmatched dependency (no Match — never directly targeted by
		// `go test ./...`) whose own import path ends in ".test", but is
		// NOT the synthetic harness: Name is its own package name.
		{ImportPath: "mod/mid.test", Name: "mid", Imports: []string{"mod/n"}, GoFiles: []string{"mid.test.go"}},
		{ImportPath: "mod/z", Imports: []string{"mod/mid.test"}, GoFiles: []string{"z.go"}, TestGoFiles: []string{"z_test.go"}, Match: []string{"./..."}},
	}
	g := BuildGraph(pkgs)
	got, resolved := g.Affected([]string{"mod/n"})
	if !resolved {
		t.Fatal("expected resolved=true")
	}
	want := []string{"mod/n", "mod/z"}
	if s := sorted(got); !equalStrings(s, want) {
		t.Fatalf("Affected(mod/n) = %v, want %v (mod/z must remain reachable through the unmatched mod/mid.test dependency)", s, want)
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

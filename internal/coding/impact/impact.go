// Package impact is Test Impact Analysis (TIA) for Go, package-granular:
// given a set of changed source files, compute the packages that must be
// PASSED TO `go test` — the transitive closure of dependents, intersected
// with packages `go test ./...` would actually build and run
// (PLAN-CODING-TRIO.md Slice 2).
//
// This package holds ONLY the pure, in-memory graph computation (parsing
// `go list`'s output, inverting the import graph, walking the closure) —
// no process spawning. `go list` itself must be launched through the
// coding-runner substrate (Slice 0) as a governed subprocess attempt; its
// stdout is handed to ParseGoList here. This split matches
// PLAN-CODING-TRIO.md's invariant 2: a pure graph walk over already-loaded
// data needs no S7 grant, only the physical `go list` launch does.
package impact

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Package is one Go package's identity and direct forward imports, as
// reported by `go list -test -json` over the whole module (module-scoped
// — this package never looks outside the caller-supplied package set).
type Package struct {
	ImportPath   string   `json:"ImportPath"`
	Dir          string   `json:"Dir"`
	Name         string   `json:"Name,omitempty"`
	ForTest      string   `json:"ForTest,omitempty"`
	Match        []string `json:"Match,omitempty"`
	Imports      []string `json:"Imports,omitempty"`
	TestImports  []string `json:"TestImports,omitempty"`
	XTestImports []string `json:"XTestImports,omitempty"`
	GoFiles      []string `json:"GoFiles,omitempty"`
	TestGoFiles  []string `json:"TestGoFiles,omitempty"`
	XTestGoFiles []string `json:"XTestGoFiles,omitempty"`

	EmbedFiles      []string `json:"EmbedFiles,omitempty"`
	TestEmbedFiles  []string `json:"TestEmbedFiles,omitempty"`
	XTestEmbedFiles []string `json:"XTestEmbedFiles,omitempty"`
}

// isTarget reports whether this package entry is a REAL workspace package
// `go test ./...` directly matches and therefore builds — the base
// record (ForTest == "") with a non-empty Match, as opposed to a
// synthetic ".test" main (empty Match) or a ForTest variant (restates the
// base package's own files under a different key). Live `go list -test
// -json` verified: the base package entry for a directly-matched package
// carries Match: ["<pattern>"]; its ".test" harness entry has an empty
// Match; its ForTest variant is excluded by the ForTest check already
// above (code-review finding, codex, Slice 2 research: `go test` ALWAYS
// compiles every directly-matched package, whether or not it has test
// files — Affected excluding no-test packages from selection let a
// compile error in an untested leaf go completely unselected while the
// full suite would have caught it).
func (p Package) isTarget() bool {
	return p.ForTest == "" && len(p.Match) > 0
}

// isSyntheticTestMain reports whether p is the generated go-test-binary
// main package (ImportPath "<pkg>.test") rather than a real workspace
// package. Three fields together are required (code-review finding,
// codex, Slice 2 round 2+3 review, verified against real `go list -test
// -json` output on multiple packages):
//   - ImportPath ends in ".test" — a naming CONVENTION for the harness,
//     not a reserved suffix, so this alone is not sufficient: a real
//     workspace package can legitimately have an import path ending in
//     ".test" too (round-2 finding).
//   - Match is empty — but this alone is not sufficient either: a real,
//     non-target DEPENDENCY package (reached only via -deps, never
//     directly matched by the `go test` pattern) also has an empty Match,
//     and could also happen to have a ".test"-suffixed import path
//     (round-3 finding) — skipping it would silently break a real
//     production edge running through it.
//   - Name == "main" — the synthetic harness is always package main; a
//     real, non-synthetic dependency package is never compiled as
//     package main under a ".test"-suffixed import path (a genuine `main`
//     package's own import path is never itself ".test"-suffixed). This
//     is what rules out the round-3 counterexample: an unmatched
//     dependency named e.g. "mid.test" still reports its OWN package
//     name (e.g. "mid"), never "main".
func isSyntheticTestMain(p Package) bool {
	return strings.HasSuffix(p.ImportPath, ".test") && len(p.Match) == 0 && p.Name == "main"
}

// ParseGoList decodes the CONCATENATED-JSON-OBJECTS stream `go list -json`
// (and `-test -json`) produces — this is NOT a JSON array, and a plain
// json.Unmarshal on the whole buffer would fail or silently decode only
// the first object depending on the caller's mistake. Every object in the
// stream must decode cleanly; a malformed one fails the whole parse
// (fail-safe toward the caller's full-suite fallback, invariant 5).
func ParseGoList(r io.Reader) ([]Package, error) {
	dec := json.NewDecoder(r)
	var pkgs []Package
	for dec.More() {
		var p Package
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("impact: malformed go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// Graph is the module-scoped reverse import graph, WITHIN the package set
// Graph was built from (an edge to anything outside that set — stdlib, a
// different module — is never tracked, since it can never be a
// "dependent" whose tests this module could run). Two distinct edge
// kinds are tracked separately (code-review CODE1 codex finding #5):
//
//   - prodRdeps: imp -> importers via that importer's PRODUCTION Imports.
//     These edges PROPAGATE transitively — a change to imp can affect an
//     importer's own importers, since production code is compiled in.
//   - testOwners: imp -> packages whose TestImports/XTestImports name imp.
//     These edges are TERMINAL — a package that only reaches imp through
//     its own test file's import is never itself reachable BY anything
//     upstream of it through imp (its production callers never see
//     imp), so this edge must never be walked past. Conflating the two
//     (as an earlier version of this code did) let a test-only import
//     propagate transitively into unrelated production consumers: for
//     A <-test- F <-production- G, a change to A would incorrectly also
//     select G's tests, even though G never depends on A at all.
type Graph struct {
	prodRdeps  map[string]map[string]bool
	testOwners map[string]map[string]bool
	// targets holds every REAL, directly-matched workspace package
	// (isTarget()) — NOT just ones with test files. `go test ./...`
	// compiles (and can therefore FAIL on) every matched package
	// regardless of whether it has tests, so selection must offer every
	// such package as a candidate, not only ones with actual test
	// functions (code-review finding, codex, Slice 2 research).
	targets map[string]bool
	known   map[string]bool
}

// BuildGraph inverts every package's forward Imports into reverse edges.
// ForTest-variant and synthetic ".test" main entries are skipped for edge
// purposes (they restate the base package's own imports); only the base
// package's isTarget() flag is recorded.
func BuildGraph(pkgs []Package) *Graph {
	g := &Graph{
		prodRdeps:  map[string]map[string]bool{},
		testOwners: map[string]map[string]bool{},
		targets:    map[string]bool{},
		known:      map[string]bool{},
	}
	for _, p := range pkgs {
		if p.ForTest != "" {
			continue // restates the base package; not a distinct node
		}
		if isSyntheticTestMain(p) {
			continue
		}
		g.known[p.ImportPath] = true
		if p.isTarget() {
			g.targets[p.ImportPath] = true
		}
		for _, imp := range p.Imports {
			if imp == p.ImportPath {
				continue
			}
			if g.prodRdeps[imp] == nil {
				g.prodRdeps[imp] = map[string]bool{}
			}
			g.prodRdeps[imp][p.ImportPath] = true
		}
		for _, imp := range append(append([]string{}, p.TestImports...), p.XTestImports...) {
			if imp == p.ImportPath {
				continue
			}
			// A test-only import of imp makes p a TERMINAL owner: a
			// change to imp affects p's own tests, but does not
			// propagate past p (see the Graph doc comment above).
			if g.testOwners[imp] == nil {
				g.testOwners[imp] = map[string]bool{}
			}
			g.testOwners[imp][p.ImportPath] = true
		}
	}
	return g
}

// Affected returns the packages (within this Graph's known set) that must
// be PASSED TO `go test` for a change to any of the changed import paths:
// every package reached by walking PRODUCTION import edges transitively
// from the changed set (intersected with real, directly-matched target
// packages — isTarget(), not just ones with test files: `go test`
// compiles, and can fail on, every matched package regardless of whether
// it has tests, so a change to the changed packages themselves is
// included even when they have none), PLUS — at every node visited along
// that production walk, including the changed packages themselves — any
// package that reaches that node only through a test-only import
// (TestImports/XTestImports).
// A test-only owner is added directly but never enqueued: it is a
// TERMINAL edge (see the Graph doc comment) — its own production
// importers, if any, never see the changed code through it, so the walk
// must not continue past it via that edge.
//
// An import path outside the known set (impact.Graph was built from a
// different or incomplete package listing) is treated as UNRESOLVABLE —
// per invariant 5 (fail-safe toward closure), the caller must fall back
// to the full suite rather than trust a partial answer; Affected reports
// this via the second return value.
func (g *Graph) Affected(changed []string) (affected []string, resolved bool) {
	resolved = true
	visited := map[string]bool{}
	result := map[string]bool{}
	queue := make([]string, 0, len(changed))
	for _, c := range changed {
		if !g.known[c] {
			resolved = false
			continue
		}
		if !visited[c] {
			visited[c] = true
			queue = append(queue, c)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if g.targets[cur] {
			result[cur] = true
		}
		for owner := range g.testOwners[cur] {
			// codex+agy finding: an owner reached only via a test-only
			// import must still be a real go-test target (isTarget()) to
			// be selected — an unmatched dependency package (e.g. a
			// stdlib or non-workspace package pulled in by `go list
			// -deps -test`) can carry TestImports too, and must not leak
			// into the result just because it owns a test-only edge.
			if g.targets[owner] {
				result[owner] = true // terminal: never enqueued
			}
		}
		for dep := range g.prodRdeps[cur] {
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}
	for pkg := range result {
		affected = append(affected, pkg)
	}
	sort.Strings(affected) // deterministic order (code-review CODE2 agy note 1) — map iteration is randomized in Go
	return affected, resolved
}

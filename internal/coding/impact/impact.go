// Package impact is Test Impact Analysis (TIA) for Go, package-granular:
// given a set of changed source files, compute the packages whose tests
// need to re-run — the transitive closure of dependents, intersected with
// packages that actually have tests (PLAN-CODING-TRIO.md Slice 2).
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
)

// Package is one Go package's identity and direct forward imports, as
// reported by `go list -test -json` over the whole module (module-scoped
// — this package never looks outside the caller-supplied package set).
type Package struct {
	ImportPath   string   `json:"ImportPath"`
	Dir          string   `json:"Dir"`
	ForTest      string   `json:"ForTest,omitempty"`
	Imports      []string `json:"Imports,omitempty"`
	TestImports  []string `json:"TestImports,omitempty"`
	XTestImports []string `json:"XTestImports,omitempty"`
	GoFiles      []string `json:"GoFiles,omitempty"`
	TestGoFiles  []string `json:"TestGoFiles,omitempty"`
	XTestGoFiles []string `json:"XTestGoFiles,omitempty"`
}

// hasTests reports whether this package entry itself declares tests (the
// base package record — not the synthetic ".test" main or a ForTest
// variant, which redundantly restate the same files).
func (p Package) hasTests() bool {
	return p.ForTest == "" && (len(p.TestGoFiles) > 0 || len(p.XTestGoFiles) > 0)
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
	tests      map[string]bool
	known      map[string]bool
}

// BuildGraph inverts every package's forward Imports into reverse edges.
// ForTest-variant and synthetic ".test" main entries are skipped for edge
// purposes (they restate the base package's own imports); only the base
// package's hasTests() flag is recorded.
func BuildGraph(pkgs []Package) *Graph {
	g := &Graph{
		prodRdeps:  map[string]map[string]bool{},
		testOwners: map[string]map[string]bool{},
		tests:      map[string]bool{},
		known:      map[string]bool{},
	}
	for _, p := range pkgs {
		if p.ForTest != "" {
			continue // restates the base package; not a distinct node
		}
		g.known[p.ImportPath] = true
		if p.hasTests() {
			g.tests[p.ImportPath] = true
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

// Affected returns the packages (within this Graph's known set) whose
// tests must re-run for a change to any of the changed import paths:
// every package reached by walking PRODUCTION import edges transitively
// from the changed set (intersected with packages that have tests, so a
// change to the changed packages themselves is included when they have
// tests), PLUS — at every node visited along that production walk,
// including the changed packages themselves — any package that reaches
// that node only through a test-only import (TestImports/XTestImports).
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
		if g.tests[cur] {
			result[cur] = true
		}
		for owner := range g.testOwners[cur] {
			result[owner] = true // terminal: never enqueued
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
	return affected, resolved
}

//go:build linux

// T18 RED table (tasks-P0): physical per-profile isolation (HARDQ B3) —
// identical content seeded in work+private, every query path stays inside
// its profile; FTS of profile A never returns B tokens; restart+replay
// preserves ProfileID on every row. Anchored to PRD §6 item 5 + E14.
package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func openProfile(t *testing.T, layout pathx.Layout, p contracts.ProfileID) *Store {
	t.Helper()
	dir, err := layout.ProfileDir(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := pathx.EnsureDir(layout.Base, dir); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "memory.db"), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedBoth(t *testing.T) (pathx.Layout, *Store, *Store) {
	t.Helper()
	layout := pathx.Layout{Base: filepath.Join(t.TempDir(), "nexus")}
	if err := os.MkdirAll(layout.Base, 0o700); err != nil {
		t.Fatal(err)
	}
	work := openProfile(t, layout, "work")
	private := openProfile(t, layout, "private")
	// IDENTICAL shared content in both profiles (ledger literal) plus one
	// UNIQUE token per profile as the leak oracle.
	for _, s := range []*Store{work, private} {
		if err := s.SaveFact("f-shared", "the shared meeting is on friday"); err != nil {
			t.Fatal(err)
		}
	}
	if err := work.SaveFact("f-work", "project ZEBRAPROJECT deadline monday"); err != nil {
		t.Fatal(err)
	}
	if err := private.SaveFact("f-priv", "doctor appointment WOLFSECRET thursday"); err != nil {
		t.Fatal(err)
	}
	return layout, work, private
}

// Cross-profile query → 0 hits; FTS of A never returns B tokens.
func TestCrossProfileQueriesReturnNothing(t *testing.T) {
	_, work, private := seedBoth(t)
	if hits, err := work.Search("WOLFSECRET"); err != nil || len(hits) != 0 {
		t.Fatalf("work profile leaked private tokens: %v %v", hits, err)
	}
	if hits, err := private.Search("ZEBRAPROJECT"); err != nil || len(hits) != 0 {
		t.Fatalf("private profile leaked work tokens: %v %v", hits, err)
	}
	// Both still find their OWN and the shared content.
	if hits, _ := work.Search("ZEBRAPROJECT"); len(hits) != 1 {
		t.Fatalf("work cannot find its own fact: %v", hits)
	}
	if hits, _ := work.Search("friday"); len(hits) != 1 {
		t.Fatalf("work cannot find shared content: %v", hits)
	}
	if hits, _ := private.Search("friday"); len(hits) != 1 {
		t.Fatalf("private cannot find shared content: %v", hits)
	}
}

// Physical isolation: two DIFFERENT database files, each under its own
// profile subtree; the system dir holds zero profile payloads (B3).
func TestPhysicalIsolationLayout(t *testing.T) {
	layout, work, private := seedBoth(t)
	if work.Path() == private.Path() {
		t.Fatal("profiles share one database file")
	}
	if !strings.Contains(work.Path(), "/profiles/work/") || !strings.Contains(private.Path(), "/profiles/private/") {
		t.Fatalf("stores outside their profile subtrees: %s / %s", work.Path(), private.Path())
	}
	if strings.HasPrefix(work.Path(), layout.SystemDir()) {
		t.Fatal("profile store under the system dir")
	}
}

// Every row is STAMPED with its profile, the stamp survives restart, and
// a store REFUSES to open a file stamped for another profile (the
// mutable-global-lookup class of bug becomes an open-time failure).
func TestProfileStampImmutableAcrossRestart(t *testing.T) {
	layout, work, _ := seedBoth(t)
	path := work.Path()
	work.Close()
	reopened, err := Open(path, "work")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := reopened.All()
	if err != nil || len(rows) != 2 {
		t.Fatalf("restart lost rows: %v %v", rows, err)
	}
	for _, r := range rows {
		if r.Profile != "work" {
			t.Fatalf("row %s lost its profile stamp: %q", r.ID, r.Profile)
		}
	}
	// A different profile can NEVER open this file.
	if _, err := Open(path, "private"); err == nil {
		t.Fatal("private profile opened the work store (fail closed)")
	}
	_ = layout
}

// The stamp is written by the STORE from its bound profile — a caller
// cannot smuggle a foreign profile onto a row.
func TestCallerCannotForgeProfileOnRow(t *testing.T) {
	_, work, _ := seedBoth(t)
	if err := work.SaveFact("f-x", "content"); err != nil {
		t.Fatal(err)
	}
	rows, _ := work.All()
	for _, r := range rows {
		if r.Profile != "work" {
			t.Fatalf("foreign profile stamp on row %s: %q", r.ID, r.Profile)
		}
	}
	// Invalid inputs are refused.
	if err := work.SaveFact("", "x"); err == nil {
		t.Fatal("empty fact id accepted")
	}
	if err := work.SaveFact("f-y", ""); err == nil {
		t.Fatal("empty content accepted")
	}
}

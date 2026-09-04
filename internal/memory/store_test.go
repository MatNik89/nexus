//go:build linux

// T18 RED table (tasks-P0): physical per-profile isolation (HARDQ B3) —
// identical content seeded in work+private, every query path stays inside
// its profile; FTS of profile A never returns B tokens; restart+REPLAY
// preserves ProfileID on every row and REPRODUCES the facts from the
// journal alone (Annex P0.3). Anchored to PRD §6 item 5 + E14.
package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/pathx"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

// openProfile opens the profile's ONE journal with the memory projection
// registered — exactly the production topology.
func openProfile(t *testing.T, layout pathx.Layout, p contracts.ProfileID) (*Store, string) {
	t.Helper()
	dir, err := layout.ProfileDir(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := pathx.EnsureDir(layout.Base, dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.db")
	j, err := journal.Open(path, p, redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	s, err := NewStore(j)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func testLayout(t *testing.T) pathx.Layout {
	t.Helper()
	layout := pathx.Layout{Base: filepath.Join(t.TempDir(), "nexus")}
	if err := os.MkdirAll(layout.Base, 0o700); err != nil {
		t.Fatal(err)
	}
	return layout
}

func seedBoth(t *testing.T) (pathx.Layout, *Store, *Store, string, string) {
	t.Helper()
	layout := testLayout(t)
	work, workPath := openProfile(t, layout, "work")
	private, privatePath := openProfile(t, layout, "private")
	// IDENTICAL shared content in both profiles (ledger literal) plus one
	// UNIQUE token per profile as the leak oracle.
	for _, s := range []*Store{work, private} {
		if err := s.SaveFact(ctxT(), "f-shared", "the shared meeting is on friday"); err != nil {
			t.Fatal(err)
		}
	}
	if err := work.SaveFact(ctxT(), "f-work", "project ZEBRAPROJECT deadline monday"); err != nil {
		t.Fatal(err)
	}
	if err := private.SaveFact(ctxT(), "f-priv", "doctor appointment WOLFSECRET thursday"); err != nil {
		t.Fatal(err)
	}
	return layout, work, private, workPath, privatePath
}

// Cross-profile query → 0 hits; FTS of A never returns B tokens.
func TestCrossProfileQueriesReturnNothing(t *testing.T) {
	_, work, private, _, _ := seedBoth(t)
	if hits, err := work.Search(ctxT(), "WOLFSECRET"); err != nil || len(hits) != 0 {
		t.Fatalf("work profile leaked private tokens: %v %v", hits, err)
	}
	if hits, err := private.Search(ctxT(), "ZEBRAPROJECT"); err != nil || len(hits) != 0 {
		t.Fatalf("private profile leaked work tokens: %v %v", hits, err)
	}
	if hits, _ := work.Search(ctxT(), "ZEBRAPROJECT"); len(hits) != 1 {
		t.Fatalf("work cannot find its own fact: %v", hits)
	}
	if hits, _ := work.Search(ctxT(), "friday"); len(hits) != 1 {
		t.Fatalf("work cannot find shared content: %v", hits)
	}
	if hits, _ := private.Search(ctxT(), "friday"); len(hits) != 1 {
		t.Fatalf("private cannot find shared content: %v", hits)
	}
}

// Physical isolation: two DIFFERENT journal files, each under its own
// profile subtree; the system dir holds zero profile payloads (B3 —
// asserted by ENUMERATING the system dir, not by string checks).
func TestPhysicalIsolationLayout(t *testing.T) {
	layout, _, _, workPath, privatePath := seedBoth(t)
	if workPath == privatePath {
		t.Fatal("profiles share one database file")
	}
	if !strings.Contains(workPath, "/profiles/work/") || !strings.Contains(privatePath, "/profiles/private/") {
		t.Fatalf("stores outside their profile subtrees: %s / %s", workPath, privatePath)
	}
	entries, err := os.ReadDir(layout.SystemDir())
	if err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), ".db") {
				t.Fatalf("profile payload in the system dir: %s", e.Name())
			}
		}
	}
}

// Restart + REPLAY: reopening the journal REPLAYS the chain (journal
// Open verifies it) and every row keeps its ProfileID; a FRESH projection
// fold from the same journal file reproduces the facts — the journal is
// the canon, the tables are derived (Annex P0.3, Phase-3 codex #1).
func TestProfileStampSurvivesRestartAndReplay(t *testing.T) {
	layout, work, _, workPath, _ := seedBoth(t)
	_ = layout
	rows, err := work.All(ctxT())
	if err != nil || len(rows) != 2 {
		t.Fatalf("seed rows: %v %v", rows, err)
	}
	// Simulate a fresh host: reopen the SAME journal file; Open replays
	// and verifies the chain, the projection re-inits, facts survive.
	// (The first handle must release its lease first.)
	if err := workClose(t, work); err != nil {
		t.Fatal(err)
	}
	j2, err := journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	s2, err := NewStore(j2)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s2.All(ctxT())
	if err != nil || len(rows) != 2 {
		t.Fatalf("restart lost rows: %v %v", rows, err)
	}
	for _, r := range rows {
		if r.Profile != "work" {
			t.Fatalf("row %s lost its profile stamp: %q", r.ID, r.Profile)
		}
	}
	// REPLAY REPRODUCTION (Phase-3-r2 codex #1): DROP the projection
	// tables and the checkpoint (the rebuild path), reopen — the
	// canonical stream alone reconstructs the FULL projection state.
	j2.Close()
	dropProjection(t, workPath)
	j3, err := journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatalf("reopen after projection drop: %v", err)
	}
	s3, err := NewStore(j3)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s3.All(ctxT())
	if err != nil || len(rows) != 2 {
		t.Fatalf("catch-up did not reconstruct the projection: %v %v", rows, err)
	}
	if hits, _ := s3.Search(ctxT(), "ZEBRAPROJECT"); len(hits) != 1 {
		t.Fatalf("rebuilt FTS broken: %v", hits)
	}
	j3.Close()
	j2, err = journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	// A different profile can NEVER open this file (journal binding).
	j2.Close()
	if _, err := journal.Open(workPath, "private", redact.None{}, Events(), NewProjection()); err == nil {
		t.Fatal("private profile opened the work journal (fail closed)")
	}
}

// workClose releases the store's journal (helper: Store owns no Close —
// the journal owner does).
func workClose(t *testing.T, s *Store) error {
	t.Helper()
	return s.j.Close()
}

// replayFacts folds the journal's memory events through the projection
// Apply logic into an independent count (no projection tables involved).
func replayFacts(t *testing.T, j *journal.Journal) map[string]bool {
	t.Helper()
	accepted := map[string]bool{}
	if err := j.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case EvFactSaved:
			var f factPayload
			if err := jsonUnmarshal(ev.Envelope.Payload, &f); err != nil {
				return err
			}
			accepted[f.ID] = true
		case EvFactAccepted:
			var d decisionPayload
			if err := jsonUnmarshal(ev.Envelope.Payload, &d); err != nil {
				return err
			}
			accepted[d.ID] = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return accepted
}

// The stamp comes from the JOURNAL's bound profile — a caller cannot
// smuggle a foreign profile onto a row, and invalid inputs are refused.
func TestCallerCannotForgeProfileOnRow(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if err := work.SaveFact(ctxT(), "f-x", "content"); err != nil {
		t.Fatal(err)
	}
	rows, _ := work.All(ctxT())
	for _, r := range rows {
		if r.Profile != "work" {
			t.Fatalf("foreign profile stamp on row %s: %q", r.ID, r.Profile)
		}
	}
	if err := work.SaveFact(ctxT(), "", "x"); err == nil {
		t.Fatal("empty fact id accepted")
	}
	if err := work.SaveFact(ctxT(), "f-y", ""); err == nil {
		t.Fatal("empty content accepted")
	}
}

// dropProjection removes the derived tables AND the checkpoint — the
// declared rebuild path.
func dropProjection(t *testing.T, path string) {
	t.Helper()
	db, err := sqlOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS mem_facts`,
		`DROP TABLE IF EXISTS mem_facts_fts`,
		`DELETE FROM proj_sync_offsets WHERE name='memory_facts'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

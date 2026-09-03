//go:build linux

// T19 RED table (tasks-P0): explicit memory (S9.2-min) — exact preview +
// approval, inferred review queue (not recallable until accepted),
// append-only supersession, recency retrieval, NO decay (HARDQ B8);
// MEMORY_FORGET/DATA_PURGE deferred to Annex P2.1 (their normative
// owner). Acceptance: PRD §6 item 2 — a fact survives restart into a
// fresh session, only in its profile.
package memory

import (
	"testing"
)

// "remember X" → the preview is the EXACT content, and the fact is not
// recallable until approved.
func TestRememberPreviewAndApprovalGate(t *testing.T) {
	_, work, _ := seedBoth(t)
	preview, err := work.Propose("f-1", "the wifi password is on the router", OriginExplicit)
	if err != nil {
		t.Fatal(err)
	}
	if preview != "the wifi password is on the router" {
		t.Fatalf("preview must be the EXACT content: %q", preview)
	}
	if hits, _ := work.Recall("wifi"); len(hits) != 0 {
		t.Fatalf("unapproved fact recallable: %v", hits)
	}
	if err := work.Accept("f-1"); err != nil {
		t.Fatal(err)
	}
	hits, err := work.Recall("wifi")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-1" {
		t.Fatalf("accepted fact not recallable: %v %v", hits, err)
	}
}

// The ledger literal: a REJECTED inference is never recallable — but the
// record is append-only (the decision itself is retained).
func TestRejectedInferenceNotRecallable(t *testing.T) {
	_, work, _ := seedBoth(t)
	if _, err := work.Propose("f-inf", "user probably prefers TABASCOHINT sauce", OriginInferred); err != nil {
		t.Fatal(err)
	}
	if err := work.Reject("f-inf"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := work.Recall("TABASCOHINT"); len(hits) != 0 {
		t.Fatalf("rejected inference recallable: %v", hits)
	}
	if hits, _ := work.Search("TABASCOHINT"); len(hits) != 0 {
		t.Fatalf("rejected inference searchable: %v", hits)
	}
	// Append-only: the row still exists with its decision.
	rows, _ := work.All()
	found := false
	for _, r := range rows {
		if r.ID == "f-inf" && r.Status == StatusRejected {
			found = true
		}
	}
	if !found {
		t.Fatal("rejected row purged (append-only violated)")
	}
	// A rejected fact cannot be resurrected by a later Accept.
	if err := work.Accept("f-inf"); err == nil {
		t.Fatal("rejected fact resurrected by Accept")
	}
}

// Supersession is append-only: "actually it's Y" — recall returns ONLY
// the latest version, both rows remain.
func TestSupersededFactReturnsOnlyLatest(t *testing.T) {
	_, work, _ := seedBoth(t)
	if _, err := work.Propose("f-a", "the CARKEYFACT is in the drawer", OriginExplicit); err != nil {
		t.Fatal(err)
	}
	if err := work.Accept("f-a"); err != nil {
		t.Fatal(err)
	}
	if err := work.Supersede("f-a", "f-a2", "the CARKEYFACT is in the jacket"); err != nil {
		t.Fatal(err)
	}
	hits, err := work.Recall("CARKEYFACT")
	if err != nil || len(hits) != 1 {
		t.Fatalf("superseded chain must return exactly the latest: %v %v", hits, err)
	}
	if hits[0].ID != "f-a2" || hits[0].Content != "the CARKEYFACT is in the jacket" {
		t.Fatalf("recall returned a stale version: %+v", hits[0])
	}
	// Append-only: both versions physically present.
	rows, _ := work.All()
	n := 0
	for _, r := range rows {
		if r.ID == "f-a" || r.ID == "f-a2" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("supersession destroyed history: %d rows", n)
	}
	// Superseding an unknown or unaccepted fact is refused.
	if err := work.Supersede("f-never", "f-x", "y"); err == nil {
		t.Fatal("superseded an unknown fact")
	}
	// A superseded chain can be corrected AGAIN (chain of 3).
	if err := work.Supersede("f-a2", "f-a3", "the CARKEYFACT is lost"); err != nil {
		t.Fatal(err)
	}
	hits, _ = work.Recall("CARKEYFACT")
	if len(hits) != 1 || hits[0].ID != "f-a3" {
		t.Fatalf("triple chain broken: %v", hits)
	}
}

// PRD §6 item 2 literal: an accepted fact survives restart into a FRESH
// session — and only in its profile.
func TestFactSurvivesRestartOnlyInItsProfile(t *testing.T) {
	layout, work, private := seedBoth(t)
	if _, err := work.Propose("f-r", "the RESTARTFACT lives here", OriginExplicit); err != nil {
		t.Fatal(err)
	}
	if err := work.Accept("f-r"); err != nil {
		t.Fatal(err)
	}
	workPath, privatePath := work.Path(), private.Path()
	work.Close()
	private.Close()
	_ = layout
	// Fresh sessions.
	work2, err := Open(workPath, "work")
	if err != nil {
		t.Fatal(err)
	}
	defer work2.Close()
	private2, err := Open(privatePath, "private")
	if err != nil {
		t.Fatal(err)
	}
	defer private2.Close()
	hits, err := work2.Recall("RESTARTFACT")
	if err != nil || len(hits) != 1 {
		t.Fatalf("accepted fact did not survive restart: %v %v", hits, err)
	}
	if hits, _ := private2.Recall("RESTARTFACT"); len(hits) != 0 {
		t.Fatalf("fact leaked across profiles after restart: %v", hits)
	}
}

// Retrieval is recency-ordered (newest accepted first). NO decay: an old
// fact stays recallable forever (B8 — structural: Recall has no age
// filter; asserted through ordering, not waiting).
func TestRecallRecencyOrder(t *testing.T) {
	_, work, _ := seedBoth(t)
	for _, f := range []struct{ id, content string }{
		{"f-old", "ORDERTOKEN first note"},
		{"f-new", "ORDERTOKEN second note"},
	} {
		if _, err := work.Propose(f.id, f.content, OriginExplicit); err != nil {
			t.Fatal(err)
		}
		if err := work.Accept(f.id); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := work.Recall("ORDERTOKEN")
	if err != nil || len(hits) != 2 {
		t.Fatalf("recall: %v %v", hits, err)
	}
	if hits[0].ID != "f-new" || hits[1].ID != "f-old" {
		t.Fatalf("recency order broken: %v", hits)
	}
}

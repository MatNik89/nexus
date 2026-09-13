//go:build linux

// T19 RED table (tasks-P0): explicit memory semantics — exact preview +
// approval gate, rejected-inference quarantine, STRICTLY LINEAR
// append-only supersession (diamond + supersede-of-superseded refused),
// stale content excluded from EVERY retrieval surface, tag/exact
// retrieval, recency, restart survival. HARDQ B8 + PRD §6 item 2.
package memory

import (
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func TestRememberPreviewAndApprovalGate(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	preview, err := work.Propose(ctxT(), "f-1", "the wifi password is on the router", OriginExplicit, "")
	if err != nil {
		t.Fatal(err)
	}
	if preview != "the wifi password is on the router" {
		t.Fatalf("preview must be the EXACT content: %q", preview)
	}
	if hits, _ := work.Recall(ctxT(), "wifi"); len(hits) != 0 {
		t.Fatalf("unapproved fact recallable: %v", hits)
	}
	if err := work.Accept(ctxT(), "f-1"); err != nil {
		t.Fatal(err)
	}
	hits, err := work.Recall(ctxT(), "wifi")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-1" {
		t.Fatalf("accepted fact not recallable: %v %v", hits, err)
	}
}

func TestRejectedInferenceNotRecallable(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if _, err := work.Propose(ctxT(), "f-inf", "user probably prefers TABASCOHINT sauce", OriginInferred, ""); err != nil {
		t.Fatal(err)
	}
	if err := work.Reject(ctxT(), "f-inf"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := work.Recall(ctxT(), "TABASCOHINT"); len(hits) != 0 {
		t.Fatalf("rejected inference recallable: %v", hits)
	}
	if hits, _ := work.Search(ctxT(), "TABASCOHINT"); len(hits) != 0 {
		t.Fatalf("rejected inference searchable: %v", hits)
	}
	rows, _ := work.All(ctxT())
	found := false
	for _, r := range rows {
		if r.ID == "f-inf" && r.Status == StatusRejected {
			found = true
			// Inference trust is UNTRUSTED (monotone — Phase-3 codex #7).
			if r.Trust != contracts.TrustUntrustedExternal {
				t.Fatalf("inferred fact carries trust %v", r.Trust)
			}
		}
	}
	if !found {
		t.Fatal("rejected row purged (append-only violated)")
	}
	if err := work.Accept(ctxT(), "f-inf"); err == nil {
		t.Fatal("rejected fact resurrected by Accept")
	}
}

// STRICTLY LINEAR supersession: only-latest recall, diamond refused,
// supersede-of-superseded refused, triple chain works.
func TestSupersessionStrictlyLinear(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if _, err := work.Propose(ctxT(), "f-a", "the CARKEYFACT is in the drawer", OriginExplicit, ""); err != nil {
		t.Fatal(err)
	}
	if err := work.Accept(ctxT(), "f-a"); err != nil {
		t.Fatal(err)
	}
	if err := work.Supersede(ctxT(), "f-a", "f-a2", "the CARKEYFACT is in the jacket"); err != nil {
		t.Fatal(err)
	}
	hits, err := work.Recall(ctxT(), "CARKEYFACT")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-a2" {
		t.Fatalf("recall must return exactly the latest: %v %v", hits, err)
	}
	// DIAMOND refused: a second successor of f-a (Phase-3 codex #5).
	if err := work.Supersede(ctxT(), "f-a", "f-evil", "the CARKEYFACT is stolen"); err == nil {
		t.Fatal("diamond successor accepted — two contradictory latest versions")
	}
	if hits, _ := work.Recall(ctxT(), "CARKEYFACT"); len(hits) != 1 {
		t.Fatalf("diamond leaked into recall: %v", hits)
	}
	// Append-only: both versions physically present.
	rows, _ := work.All(ctxT())
	n := 0
	for _, r := range rows {
		if r.ID == "f-a" || r.ID == "f-a2" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("supersession destroyed history: %d rows", n)
	}
	if err := work.Supersede(ctxT(), "f-never", "f-x", "y"); err == nil {
		t.Fatal("superseded an unknown fact")
	}
	// Chain of 3 stays legal (each step supersedes the current LEAF).
	if err := work.Supersede(ctxT(), "f-a2", "f-a3", "the CARKEYFACT is lost"); err != nil {
		t.Fatal(err)
	}
	hits, _ = work.Recall(ctxT(), "CARKEYFACT")
	if len(hits) != 1 || hits[0].ID != "f-a3" {
		t.Fatalf("triple chain broken: %v", hits)
	}
}

// EVERY retrieval surface shares the latest-accepted predicate — stale
// content never returns through Search either (Phase-3 codex #6).
func TestStaleContentExcludedFromEverySurface(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if err := work.SaveFact(ctxT(), "f-ip", "the STALESERVER is 10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := work.Supersede(ctxT(), "f-ip", "f-ip2", "the STALESERVER is 10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	for name, get := range map[string]func() ([]Row, error){
		"recall": func() ([]Row, error) { return work.Recall(ctxT(), "STALESERVER") },
		"search": func() ([]Row, error) { return work.Search(ctxT(), "STALESERVER") },
		"exact-new": func() ([]Row, error) {
			return work.Exact(ctxT(), "the STALESERVER is 10.0.0.2")
		},
	} {
		hits, err := get()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, h := range hits {
			if strings.Contains(h.Content, "10.0.0.1") {
				t.Fatalf("%s leaked stale content: %v", name, hits)
			}
		}
		if len(hits) != 1 {
			t.Fatalf("%s: want exactly the latest, got %v", name, hits)
		}
	}
	// The stale content is not even reachable byte-exactly.
	if hits, _ := work.Exact(ctxT(), "the STALESERVER is 10.0.0.1"); len(hits) != 0 {
		t.Fatalf("stale content reachable byte-exactly: %v", hits)
	}
}

// Tag + byte-exact retrieval (Phase-3 codex #8): tags isolate facts the
// FTS phrase cannot, and Exact selects by full byte-identical content.
func TestTagAndExactRetrieval(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if err := work.SaveFact(ctxT(), "f-t1", "the DOORCODE is 4411", "home"); err != nil {
		t.Fatal(err)
	}
	if err := work.SaveFact(ctxT(), "f-t2", "the DOORCODE is 9922", "office"); err != nil {
		t.Fatal(err)
	}
	hits, err := work.RecallTag(ctxT(), "office")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-t2" {
		t.Fatalf("tag retrieval broken: %v %v", hits, err)
	}
	// A tag that is a SUBSTRING of another never matches (token-bounded).
	if hits, _ := work.RecallTag(ctxT(), "offic"); len(hits) != 0 {
		t.Fatalf("substring tag matched: %v", hits)
	}
	hits, err = work.Exact(ctxT(), "the DOORCODE is 4411")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-t1" {
		t.Fatalf("byte-exact retrieval broken: %v %v", hits, err)
	}
	// Malformed tags are refused at the payload validator.
	if err := work.SaveFact(ctxT(), "f-t3", "x", "two words"); err == nil {
		t.Fatal("multi-word tag accepted")
	}
}

// PRD §6 item 2 literal: an accepted fact survives restart into a FRESH
// session — and only in its profile.
func TestFactSurvivesRestartOnlyInItsProfile(t *testing.T) {
	_, work, private, workPath, privatePath := seedBoth(t)
	if _, err := work.Propose(ctxT(), "f-r", "the RESTARTFACT lives here", OriginExplicit, ""); err != nil {
		t.Fatal(err)
	}
	if err := work.Accept(ctxT(), "f-r"); err != nil {
		t.Fatal(err)
	}
	workClose(t, work)
	workClose(t, private)
	j2, err := journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	work2, _ := NewStore(j2)
	jp, err := journal.Open(privatePath, "private", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer jp.Close()
	private2, _ := NewStore(jp)
	hits, err := work2.Recall(ctxT(), "RESTARTFACT")
	if err != nil || len(hits) != 1 {
		t.Fatalf("accepted fact did not survive restart: %v %v", hits, err)
	}
	if hits, _ := private2.Recall(ctxT(), "RESTARTFACT"); len(hits) != 0 {
		t.Fatalf("fact leaked across profiles after restart: %v", hits)
	}
}

// Recency order (newest first); no decay is structural (no age filter).
func TestRecallRecencyOrder(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	for _, f := range []struct{ id, content string }{
		{"f-old", "ORDERTOKEN first note"},
		{"f-new", "ORDERTOKEN second note"},
	} {
		if _, err := work.Propose(ctxT(), f.id, f.content, OriginExplicit, ""); err != nil {
			t.Fatal(err)
		}
		if err := work.Accept(ctxT(), f.id); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := work.Recall(ctxT(), "ORDERTOKEN")
	if err != nil || len(hits) != 2 {
		t.Fatalf("recall: %v %v", hits, err)
	}
	if hits[0].ID != "f-new" || hits[1].ID != "f-old" {
		t.Fatalf("recency order broken: %v", hits)
	}
}

// Recall observations are MONOTONE in trust (Phase-3 codex #7): an
// accepted INFERRED fact (untrusted source class) downgrades the whole
// observation to UNTRUSTED_EXTERNAL, and known secrets are redacted.
func TestRecallObservationMonotoneTrustAndRedaction(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if _, err := work.Propose(ctxT(), "f-i", "user likely visits MONOTONETOKEN daily", OriginInferred, ""); err != nil {
		t.Fatal(err)
	}
	if err := work.Accept(ctxT(), "f-i"); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-MEMSECRET-VALUE"
	if err := work.SaveFact(ctxT(), "f-s", "the MONOTONETOKEN key is "+secret); err != nil {
		t.Fatal(err)
	}
	r := redact.NewKnownRefs(map[string]string{"KEY": secret})
	tools := Tools(work, r)
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-r", ToolID: "memory_recall",
		Arguments: []byte(`{"query":"MONOTONETOKEN"}`), ArgsSchemaHash: "memory_recall.v1",
		Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tools["memory_recall"](ctxT(), call)
	if err != nil {
		t.Fatal(err)
	}
	b := out.Output[0]
	if b.Trust != contracts.TrustUntrustedExternal {
		t.Fatalf("observation with an inferred fact carries trust %v (laundering)", b.Trust)
	}
	if strings.Contains(*b.Content, secret) {
		t.Fatalf("known secret leaked into the observation: %q", *b.Content)
	}
	// Wrong-profile call is refused for BOTH tools (codex #4 literal).
	foreign := call
	foreign.ProfileID = "private"
	if _, err := tools["memory_recall"](ctxT(), foreign); err == nil {
		t.Fatal("foreign-profile recall executed")
	}
	rc, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-w", ToolID: "memory_remember",
		Arguments: []byte(`{"content":"x"}`), ArgsSchemaHash: "memory_remember.v1",
		Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "private",
		IdempotencyKey: strPtr("ik-1"),
	})
	if _, err := tools["memory_remember"](ctxT(), rc); err == nil {
		t.Fatal("foreign-profile remember executed")
	}
	// The remember receipt attests the commit (agy #1 literal).
	ok, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-ok", ToolID: "memory_remember",
		Arguments: []byte(`{"content":"receipt test"}`), ArgsSchemaHash: "memory_remember.v1",
		Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-2"),
	})
	res, err := tools["memory_remember"](ctxT(), ok)
	if err != nil {
		t.Fatal(err)
	}
	if res.Commit == nil || !res.Commit.ValidFor(ok) || res.Commit.Phase != contracts.PhaseAfterCommit {
		t.Fatalf("remember returned no valid commit receipt: %+v", res.Commit)
	}
}

// Full-state replay: rejected + superseded state also reconstructs from
// the canonical stream (Phase-3-r2 codex #1 tail).
func TestReplayReconstructsFullState(t *testing.T) {
	_, work, _, workPath, _ := seedBoth(t)
	if _, err := work.Propose(ctxT(), "f-rej", "REPLAYREJECT idea", OriginInferred, ""); err != nil {
		t.Fatal(err)
	}
	if err := work.Reject(ctxT(), "f-rej"); err != nil {
		t.Fatal(err)
	}
	if err := work.SaveFact(ctxT(), "f-sup", "REPLAYSUP v1"); err != nil {
		t.Fatal(err)
	}
	if err := work.Supersede(ctxT(), "f-sup", "f-sup2", "REPLAYSUP v2"); err != nil {
		t.Fatal(err)
	}
	workClose(t, work)
	dropProjection(t, workPath)
	j, err := journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	s, _ := NewStore(j)
	rows, err := s.All(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Row{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if byID["f-rej"].Status != StatusRejected {
		t.Fatalf("rejected state lost in replay: %+v", byID["f-rej"])
	}
	if hits, _ := s.Recall(ctxT(), "REPLAYSUP"); len(hits) != 1 || hits[0].ID != "f-sup2" {
		t.Fatalf("supersession chain lost in replay: %v", hits)
	}
}

// Known-secret content is REFUSED before the journal (Phase-3-r2 codex
// #15): the exact-preview rule means bytes are stored verbatim or not at
// all — never silently rewritten by redaction.
func TestSecretContentRefusedNotRewritten(t *testing.T) {
	layout := testLayout(t)
	dir, _ := layout.ProfileDir("work")
	if err := pathxEnsure(layout, dir); err != nil {
		t.Fatal(err)
	}
	const secret = "sk-STORE-SECRET"
	r := redact.NewKnownRefs(map[string]string{"KEY": secret})
	j, err := journal.Open(dir+"/journal.db", "work", r, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	s, err := NewStore(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFact(ctxT(), "f-1", "my key is "+secret); err == nil {
		t.Fatal("known-secret content accepted (would be silently rewritten by journal redaction)")
	}
	if _, err := s.Propose(ctxT(), "f-2", secret, OriginExplicit, ""); err == nil {
		t.Fatal("known-secret proposal accepted")
	}
	rows, _ := s.All(ctxT())
	if len(rows) != 0 {
		t.Fatalf("refused content left rows behind: %v", rows)
	}
	// A secret smuggled into a TAG is refused too — the gate covers the
	// whole payload (Phase-3-r4 codex #4 literal).
	if err := s.SaveFact(ctxT(), "f-tag", "safe fact", secret); err == nil {
		t.Fatal("known-secret tag accepted (stored tag would be rewritten under the receipt)")
	}
	if rows, _ := s.All(ctxT()); len(rows) != 0 {
		t.Fatalf("refused tag content left rows behind: %v", rows)
	}
	// Clean content still stores byte-exactly.
	if err := s.SaveFact(ctxT(), "f-3", "the key lives in the password manager"); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.Exact(ctxT(), "the key lives in the password manager")
	if len(hits) != 1 {
		t.Fatalf("clean content not stored byte-exactly: %v", hits)
	}
}

// LIKE metacharacters in tags never widen retrieval (Phase-3-r2 codex #8).
func TestTagLikeMetacharactersEscaped(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if err := work.SaveFact(ctxT(), "f-m1", "METATAG fact one", "office"); err != nil {
		t.Fatal(err)
	}
	if err := work.SaveFact(ctxT(), "f-m2", "METATAG fact two", "%"); err != nil {
		t.Fatal(err)
	}
	// "%" retrieves ONLY the literal-% tag, never everything.
	hits, err := work.RecallTag(ctxT(), "%")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-m2" {
		t.Fatalf("%% widened tag retrieval: %v %v", hits, err)
	}
	if hits, _ := work.RecallTag(ctxT(), "_"); len(hits) != 0 {
		t.Fatalf("_ matched arbitrary tags: %v", hits)
	}
}

// Fact lineage persists and reaches the recall observation (Phase-3-r2
// codex #7): approval never erases provenance.
func TestFactLineageSurvivesToRecall(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if err := work.SaveFactLineage(ctxT(), "f-l", "LINEAGETOKEN fact", nil, []string{"tc-origin-1"}, ""); err != nil {
		t.Fatal(err)
	}
	hits, err := work.Recall(ctxT(), "LINEAGETOKEN")
	if err != nil || len(hits) != 1 || len(hits[0].Lineage) != 1 || hits[0].Lineage[0] != "tc-origin-1" {
		t.Fatalf("stored lineage lost: %v %v", hits, err)
	}
	tools := Tools(work, redact.None{})
	call, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-recall-9", ToolID: "memory_recall",
		Arguments: []byte(`{"query":"LINEAGETOKEN"}`), ArgsSchemaHash: "memory_recall.v1",
		Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
	})
	out, err := tools["memory_recall"](ctxT(), call)
	if err != nil {
		t.Fatal(err)
	}
	got := out.Output[0].Lineage
	hasOrigin, hasCall := false, false
	for _, l := range got {
		if l == "tc-origin-1" {
			hasOrigin = true
		}
		if l == "tc-recall-9" {
			hasCall = true
		}
	}
	if !hasOrigin || !hasCall {
		t.Fatalf("observation lineage must union source + call: %v", got)
	}
}

// A database written by the PREVIOUS revision (v1: no lineage column, no
// checkpoint) is RESET and rebuilt from the canonical stream at Open —
// never patched blind (Phase-3-r3 codex #2 literal).
func TestV1SchemaDatabaseRebuildsAtOpen(t *testing.T) {
	_, work, _, workPath, _ := seedBoth(t)
	workClose(t, work)
	// Emulate the ACTUAL previous-revision on-disk state (Phase-3-r4
	// codex #2): the old TWO-COLUMN checkpoint table (no version column)
	// and a mem_facts without lineage — the migration itself is under
	// test, not just the version comparison.
	db, err := sqlOpen(workPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE mem_facts DROP COLUMN lineage`,
		`DROP TABLE proj_sync_offsets`,
		`CREATE TABLE proj_sync_offsets (name TEXT PRIMARY KEY, applied_offset INTEGER NOT NULL)`,
		`INSERT INTO proj_sync_offsets(name, applied_offset) VALUES('memory_facts', 4)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	j, err := journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatalf("open over a previous-revision projection failed: %v", err)
	}
	defer j.Close()
	s, _ := NewStore(j)
	rows, err := s.All(ctxT())
	if err != nil || len(rows) != 2 {
		t.Fatalf("v1 database not rebuilt: %v %v", rows, err)
	}
	if hits, _ := s.Search(ctxT(), "ZEBRAPROJECT"); len(hits) != 1 {
		t.Fatalf("rebuilt v1 FTS broken: %v", hits)
	}
	// The checkpoint-only corruption case (tables intact, checkpoint
	// gone) also rebuilds instead of double-inserting.
	j.Close()
	db, _ = sqlOpen(workPath)
	db.Exec(`DELETE FROM proj_sync_offsets WHERE name='memory_facts'`)
	db.Close()
	j2, err := journal.Open(workPath, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatalf("open with missing checkpoint failed: %v", err)
	}
	defer j2.Close()
	s2, _ := NewStore(j2)
	if rows, err := s2.All(ctxT()); err != nil || len(rows) != 2 {
		t.Fatalf("missing-checkpoint rebuild broken: %v %v", rows, err)
	}
}

// The production correction path UNIONS provenance (Phase-3-r3 codex #7
// literal): predecessor lineage ∪ predecessor id ∪ correction call.
func TestSupersessionUnionsLineageThroughTool(t *testing.T) {
	_, work, _, _, _ := seedBoth(t)
	if err := work.SaveFactLineage(ctxT(), "f-orig", "UNIONTOKEN v1", nil, []string{"source-block"}, ""); err != nil {
		t.Fatal(err)
	}
	tools := Tools(work, redact.None{})
	call, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "correction-call", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"UNIONTOKEN v2","supersedes":"f-orig"}`),
		ArgsSchemaHash: "memory_remember.v1",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-u1"),
	})
	if _, err := tools["memory_remember"](ctxT(), call); err != nil {
		t.Fatal(err)
	}
	hits, err := work.Recall(ctxT(), "UNIONTOKEN")
	if err != nil || len(hits) != 1 {
		t.Fatalf("correction recall: %v %v", hits, err)
	}
	want := map[string]bool{"source-block": false, "f-orig": false, "correction-call": false}
	for _, l := range hits[0].Lineage {
		if _, ok := want[l]; ok {
			want[l] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("correction lost lineage element %q: %v", k, hits[0].Lineage)
		}
	}
}

//go:build linux

// TASK/EPHEMERAL memory scope (S9.4 P1): scope is a durability/decay-
// eligibility class, not a physical isolation boundary (HARDQ B3/E14) —
// ScopeUser stays permanently decay-exempt (S=∞, unchanged); ScopeTask/
// ScopeEphemeral are explicitly NOT exempt, so a future Decay slice can
// apply per-scope half-lives. Went through THREE rounds of PLAN-level
// review (agy+kilo+codex) before any code was written; every finding in
// this file traces to one of those rounds.
package memory

import (
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

// Default Recall/RecallTag behavior is BYTE-IDENTICAL to before this
// slice: everything ever written before scope existed is implicitly
// ScopeUser, and omitting the new variadic scope arg filters to
// ScopeUser — so every pre-existing caller sees exactly what it saw
// before.
func TestScopeDefaultRecallIsUserOnly(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-user", "a durable fact"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-task", "a working note", OriginExplicit, "", "", nil, nil, ScopeTask); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "fact")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-user" {
		t.Fatalf("default recall must be user-only: %v %v", hits, err)
	}
	// The task-scoped note is invisible by default...
	if hits, _ := s.Recall(ctxT(), "note"); len(hits) != 0 {
		t.Fatalf("task-scoped note leaked into default recall: %v", hits)
	}
	// ...but reachable when explicitly asked for.
	hits, err = s.Recall(ctxT(), "note", ScopeTask)
	if err != nil || len(hits) != 1 || hits[0].ID != "f-task" {
		t.Fatalf("explicit ScopeTask recall failed: %v %v", hits, err)
	}
	// Both exist regardless of scope (All() is the scope-blind
	// verification surface).
	all, err := s.All(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range all {
		if r.ID == "f-user" || r.ID == "f-task" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("both facts must exist regardless of scope: %d", n)
	}
}

// RecallTag respects the same default-user-only / opt-in-broader rule.
func TestScopeDefaultRecallTagIsUserOnly(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-user", "x", "shared-tag"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-task", "y", OriginExplicit, "", "", []string{"shared-tag"}, nil, ScopeTask); err != nil {
		t.Fatal(err)
	}
	hits, err := s.RecallTag(ctxT(), "shared-tag")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-user" {
		t.Fatalf("default RecallTag must be user-only: %v %v", hits, err)
	}
	hits, err = s.RecallTag(ctxT(), "shared-tag", ScopeAll)
	if err != nil || len(hits) != 2 {
		t.Fatalf("ScopeAll RecallTag must see both: %v %v", hits, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE agy+kilo+codex each
// independently constructed, round 1 of plan review): claim_key
// ownership is PARTITIONED by scope — a task-scoped fact and a
// user-scoped fact can share the SAME claim_key label without
// conflicting, contradicting, or shadowing each other at all.
func TestScopeClaimKeyIsPartitioned(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-user", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil, ScopeUser); err != nil {
		t.Fatal(err)
	}
	// A task-scoped write with the SAME claim_key must be a plain ADD,
	// never see the user-scoped fact as an existing owner to contradict
	// or auto-supersede.
	out, err := s.Remember(ctxT(), "f-task", "server ip is 10.0.0.9", OriginExplicit, "server_ip", "", nil, nil, ScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "added" {
		t.Fatalf("outcome = %+v, want added (task claim_key must be independent of user's)", out)
	}
	// Both remain independently recallable in their own scope.
	userHits, err := s.Recall(ctxT(), "server", ScopeUser)
	if err != nil || len(userHits) != 1 || userHits[0].ID != "f-user" {
		t.Fatalf("user-scoped fact must be untouched: %v %v", userHits, err)
	}
	taskHits, err := s.Recall(ctxT(), "server", ScopeTask)
	if err != nil || len(taskHits) != 1 || taskHits[0].ID != "f-task" {
		t.Fatalf("task-scoped fact must exist independently: %v %v", taskHits, err)
	}
	// A SECOND task-scoped write, correcting the FIRST task fact via the
	// same claim_key, must auto-resolve WITHIN the task scope, still
	// never touching the user-scoped fact.
	out, err = s.Remember(ctxT(), "f-task-2", "server ip is 10.0.0.10", OriginExplicit, "server_ip", "f-task", nil, nil, ScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "superseded" {
		t.Fatalf("outcome = %+v, want superseded (task-scoped correction within its own scope)", out)
	}
	userHits, err = s.Recall(ctxT(), "server", ScopeUser)
	if err != nil || len(userHits) != 1 || userHits[0].ID != "f-user" {
		t.Fatalf("user-scoped fact must STILL be untouched by a task-scope-internal correction: %v %v", userHits, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex+kilo each independently
// constructed, round 2 of plan review): the NAMED supersedes=<id> path
// must refuse a cross-scope correction — a task-scoped correction naming
// a user-scoped fact's id (or vice versa) must NOT silently supersede
// it, since that would remove a durable fact from all default recall
// while its scope-hidden successor became unreachable too.
func TestScopeNamedSupersedeRefusesCrossScope(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-user", "the real value"); err != nil {
		t.Fatal(err)
	}
	if err := s.SupersedeLineageWithClaim(ctxT(), "f-user", "f-task", "a task's guess", "", nil, nil, ScopeTask); err == nil {
		t.Fatal("expected a refusal: cross-scope named supersede")
	}
	hits, err := s.Recall(ctxT(), "real")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-user" {
		t.Fatalf("the user-scoped fact must be completely untouched by the refused cross-scope supersede: %v %v", hits, err)
	}
	// The reverse direction is refused too.
	if err := s.SaveFactLineage(ctxT(), "f-taskorig", "a task fact", nil, nil, ScopeTask); err != nil {
		t.Fatal(err)
	}
	if err := s.SupersedeLineageWithClaim(ctxT(), "f-taskorig", "f-user2", "claims to be durable now", "", nil, nil, ScopeUser); err == nil {
		t.Fatal("expected a refusal: reverse-direction cross-scope named supersede")
	}
	hits, err = s.Recall(ctxT(), "task fact", ScopeTask)
	if err != nil || len(hits) != 1 || hits[0].ID != "f-taskorig" {
		t.Fatalf("the task-scoped fact must be completely untouched: %v %v", hits, err)
	}
	// Same-scope named supersede still works normally (not over-refused).
	if err := s.SupersedeLineageWithClaim(ctxT(), "f-taskorig", "f-tasknew", "an updated task fact", "", nil, nil, ScopeTask); err != nil {
		t.Fatalf("same-scope named supersede must still work: %v", err)
	}
}

// Malformed/unknown scope values are refused at the tool boundary.
func TestScopeToolRefusesUnknownWriteScope(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	tools := Tools(s, redact.None{})
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-1", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"x","scope":"bogus"}`),
		ArgsSchemaHash: "memory_remember.v5", Effect: contracts.EffectReversible,
		ExecutionKind: contracts.ExecInProcess, Deadline: timeNowPlusMinute(), AttemptNo: 1,
		ProfileID: "work", IdempotencyKey: strPtr("ik-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools["memory_remember"](ctxT(), call); err == nil {
		t.Fatal("expected a refusal: unknown scope")
	}
	// "all" is a QUERY-only pseudo-value, never a legal WRITE scope.
	call2, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-2", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"x","scope":"all"}`),
		ArgsSchemaHash: "memory_remember.v5", Effect: contracts.EffectReversible,
		ExecutionKind: contracts.ExecInProcess, Deadline: timeNowPlusMinute(), AttemptNo: 1,
		ProfileID: "work", IdempotencyKey: strPtr("ik-2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools["memory_remember"](ctxT(), call2); err == nil {
		t.Fatal("expected a refusal: \"all\" is not a valid write-side scope")
	}
}

func TestScopeToolAcceptsAllOnRecallOnly(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-1", "findable content"); err != nil {
		t.Fatal(err)
	}
	tools := Tools(s, redact.None{})
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "recall-1", ToolID: "memory_recall",
		Arguments: []byte(`{"query":"findable","scope":"all"}`), ArgsSchemaHash: "memory_recall.v2",
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
	if out.Output[0].Content == nil {
		t.Fatal("expected recall output")
	}
	call2, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "recall-2", ToolID: "memory_recall",
		Arguments: []byte(`{"query":"findable","scope":"bogus"}`), ArgsSchemaHash: "memory_recall.v2",
		Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools["memory_recall"](ctxT(), call2); err == nil {
		t.Fatal("expected a refusal: unknown recall scope")
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 1 of
// CODE review — the tool layer's parseScope only ever runs INSIDE
// tools.go; a caller of Store.Recall/RecallTag directly — a test, or any
// future non-tool consumer — bypasses it entirely). An unrecognized Scope
// passed straight to the Store API silently matched ZERO rows instead of
// erroring, since scopeFilter only ever used the value as an opaque bound
// SQL argument. A wrong answer must never look identical to "no results."
func TestScopeStoreRecallRejectsUnknownScopeDirectly(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-1", "findable content"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recall(ctxT(), "findable", Scope("bogus")); err == nil {
		t.Fatal("expected a refusal: unknown scope passed directly to Store.Recall")
	}
	if _, err := s.RecallTag(ctxT(), "sometag", Scope("bogus")); err == nil {
		t.Fatal("expected a refusal: unknown scope passed directly to Store.RecallTag")
	}
	// Valid scopes, including ScopeAll, still work.
	if _, err := s.Recall(ctxT(), "findable", ScopeAll); err != nil {
		t.Fatalf("ScopeAll must still work: %v", err)
	}
	if _, err := s.Recall(ctxT(), "findable", ScopeTask); err != nil {
		t.Fatalf("a valid persisted scope must still work: %v", err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 1 of
// CODE review, a SHARPER form of the previous finding): an explicit Go
// zero-value Scope("") must mean the SAME "unset, default to user" as
// omitting the parameter entirely — exactly what the WRITE side already
// does via factPayload.UnmarshalJSON. The original fix's closed-set
// switch didn't special-case "", so a variable holding a zero Scope
// (entirely natural in Go — an unset struct field, a default zero value)
// succeeded on write (normalized to user) but was REFUSED on read
// ("unknown scope"), contradicting the write side's own promised default.
func TestScopeExplicitZeroValueMatchesOmittedDefault(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	var zero Scope // the Go zero value — never explicitly set to anything
	if err := s.SaveFactLineage(ctxT(), "f-user", "a durable fact", nil, nil, zero); err != nil {
		t.Fatal(err)
	}
	// Recall with the explicit zero value must succeed and behave
	// identically to omitting the parameter (both mean ScopeUser).
	hits, err := s.Recall(ctxT(), "fact", zero)
	if err != nil || len(hits) != 1 || hits[0].ID != "f-user" {
		t.Fatalf("explicit zero Scope must mean the same default as omission: %v %v", hits, err)
	}
	omitted, err := s.Recall(ctxT(), "fact")
	if err != nil || len(omitted) != 1 || omitted[0].ID != hits[0].ID {
		t.Fatalf("explicit zero and omitted scope must return identical results: %v vs %v (%v)", hits, omitted, err)
	}
}

// Detector (codex round-1 code-review, live-reproduced): the variadic
// scope parameter is an OPTIONAL SINGLE value (the Go idiom for "0 or
// 1"), never a real multi-scope query API — passing more than one value
// must be refused outright, not silently resolved to just the first
// (which would hide a caller's mistake rather than surface it).
func TestScopeRecallRejectsMultipleValues(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-1", "findable content"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recall(ctxT(), "findable", ScopeTask, ScopeUser); err == nil {
		t.Fatal("expected a refusal: more than one scope value is ambiguous")
	}
	if _, err := s.RecallTag(ctxT(), "sometag", ScopeTask, ScopeUser); err == nil {
		t.Fatal("expected a refusal: more than one scope value is ambiguous (RecallTag)")
	}
}

// Detector (kilo+agy round-1 code-review NOTE, defense-in-depth parity
// with supersede's own scope check): resolveContest must refuse a
// cross-scope contest link exactly like the named-supersede path does,
// even though today's only producer (the OriginInferred branch of
// applyRemembered) can never construct one — its own existing-fact
// lookup is already scope-filtered to the proposal's scope. Direct DB
// tampering (never reachable through the public API) is the only way
// to construct the counterexample, so this proves the GUARD fires,
// not that the gap is live.
func TestScopeResolveContestRefusesCrossScopeTamperedLink(t *testing.T) {
	layout := testLayout(t)
	dir, err := layout.ProfileDir("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := pathxEnsure(layout, dir); err != nil {
		t.Fatal(err)
	}
	path := dir + "/journal.db"

	j1, err := journal.Open(path, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	s1, err := NewStore(j1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Remember(ctxT(), "f-1", "birthday is march 1st", OriginExplicit, "birthday", "", nil, nil, ScopeUser); err != nil {
		t.Fatal(err)
	}
	out, err := s1.Remember(ctxT(), "f-2", "birthday is april 1st", OriginInferred, "birthday", "f-1", nil, nil, ScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "proposed" {
		t.Fatalf("setup outcome = %+v, want proposed (contest must actually be pending)", out)
	}
	if err := j1.Close(); err != nil {
		t.Fatal(err)
	}

	// Tamper the proposal's scope directly on disk — the only way to
	// construct this state, since the OriginInferred lookup that
	// produces `contests` is itself scope-filtered and can never link
	// two facts across scopes through the public API.
	db, err := sqlOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE mem_facts SET scope='task' WHERE id='f-2'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	j2, err := journal.Open(path, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	s2, err := NewStore(j2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Accept(ctxT(), "f-2"); err == nil {
		t.Fatal("expected a refusal: contest link crosses scopes (fail closed)")
	}
}

// Detector (codex round-3 plan-review requirement, explicit): a REAL
// pre-v6 journal (no "scope" key at all in its stored payload — not a
// synthetic fixture) must retry idempotently after the 5→6 schema
// upgrade, never refused as "different intent" just because the
// canonical intent representation changed shape across the boundary.
func TestScopeRetryIdempotentAcrossV5ToV6Upgrade(t *testing.T) {
	layout := testLayout(t)
	dir, err := layout.ProfileDir("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := pathxEnsure(layout, dir); err != nil {
		t.Fatal(err)
	}
	path := dir + "/journal.db"

	// Write with the CURRENT code but an EMPTY scope — omitempty means
	// the journaled payload has NO "scope" key at all, genuinely
	// indistinguishable from a real pre-v6 event.
	j1, err := journal.Open(path, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	s1, err := NewStore(j1)
	if err != nil {
		t.Fatal(err)
	}
	out1, err := s1.Remember(ctxT(), "same-call", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := j1.Close(); err != nil {
		t.Fatal(err)
	}

	// Downgrade the ON-DISK PROJECTION to look like a genuine pre-v6
	// database: drop the scope column and roll the recorded version back
	// to 5 — the SAME pattern TestV1SchemaDatabaseRebuildsAtOpen uses.
	db, err := sqlOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		// A real v5 index referenced no scope column — drop the current
		// (scope-including) index BEFORE dropping the column itself,
		// since SQLite refuses to drop a column an index still names.
		`DROP INDEX ix_mem_claim_key`,
		`CREATE INDEX ix_mem_claim_key ON mem_facts(profile_id, claim_key) WHERE claim_key IS NOT NULL`,
		`ALTER TABLE mem_facts DROP COLUMN scope`,
		`DROP TABLE proj_sync_offsets`,
		`CREATE TABLE proj_sync_offsets (name TEXT PRIMARY KEY, applied_offset INTEGER NOT NULL)`,
		`INSERT INTO proj_sync_offsets(name, applied_offset) VALUES('memory_facts', 5)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("downgrade stmt %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: the version mismatch (5 recorded vs 6 current) triggers the
	// generic drop-and-rebuild-from-journal migration, replaying the
	// pre-v6-shaped event through the CURRENT Apply/UnmarshalJSON.
	j2, err := journal.Open(path, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatalf("reopen across the v5->v6 boundary failed: %v", err)
	}
	defer j2.Close()
	s2, err := NewStore(j2)
	if err != nil {
		t.Fatal(err)
	}

	// The EXACT same retry, post-upgrade, with no scope specified —
	// must be recognized as an idempotent retry of the SAME intent.
	out2, err := s2.Remember(ctxT(), "same-call", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil, "")
	if err != nil {
		t.Fatalf("genuine retry across a real v5->v6 upgrade must not be refused: %v", err)
	}
	if out1 != out2 {
		t.Fatalf("retry outcome mismatch across the v5->v6 boundary: before=%+v after=%+v", out1, out2)
	}
	hits, err := s2.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].Scope != ScopeUser {
		t.Fatalf("the migrated fact must normalize to ScopeUser: %v %v", hits, err)
	}
}

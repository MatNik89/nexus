//go:build linux

// Audn (S9.4 P1, HARDQ B8): automatic contradiction resolution on write,
// keyed by claim_key. Decay stays out of scope entirely — the whole
// current corpus is fact-type, permanently decay-exempt (HARDQ-agy
// Finding 4); this file tests only the ADD/SUPERSEDE/PROPOSE+CONTEST
// decision table.
package memory

import (
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

// A clean update (same claim_key, both explicit, different content,
// PROVEN via the correct replaces_id) is automatically resolved — no
// supersedes id required from the caller.
func TestAudnAutoSupersedesOnCleanExplicitUpdate(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	out, err := s.Remember(ctxT(), "f-2", "server ip is 5.6.7.8", OriginExplicit, "server_ip", "f-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "superseded" || out.EffectiveID != "f-2" {
		t.Fatalf("outcome = %+v, want superseded/f-2", out)
	}
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-2" {
		t.Fatalf("recall must return exactly the auto-superseded latest: %v %v", hits, err)
	}
	rows, _ := s.All(ctxT())
	n := 0
	for _, r := range rows {
		if r.ID == "f-1" || r.ID == "f-2" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("auto-supersede must not destroy history: %d rows", n)
	}
}

// Detector (code-review finding, kilo+codex, round 1 HIGH, live-
// reproduced by both independently): a claim_key match with DIFFERING
// content REFUSES when replaces_id is missing or wrong — a bare
// claim_key match is a weaker proof-of-awareness than the pre-Audn
// supersedes-by-id path (a semantic label is far more guessable than an
// opaque fact id), so the caller must PROVE it actually looked at the
// current fact.
func TestAudnRefusesWithoutCorrectReplacesID(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-2", "server ip is 5.6.7.8", OriginExplicit, "server_ip", "", nil, nil); err == nil {
		t.Fatal("expected a refusal: replaces_id missing")
	}
	if _, err := s.Remember(ctxT(), "f-3", "server ip is 5.6.7.8", OriginExplicit, "server_ip", "wrong-id", nil, nil); err == nil {
		t.Fatal("expected a refusal: replaces_id does not name the current owner")
	}
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-1" {
		t.Fatalf("the original fact must be unchanged after both refusals: %v %v", hits, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 1): a
// stale approval's replaces_id, computed against f-old, must NOT silently
// retarget an INTERVENING fact that has since taken over the same
// claim_key — the exact-intent check is re-derived FRESH at execution
// time, inside the same transaction, never trusted from approval time.
func TestAudnReplacesIDDetectsInterveningWrite(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-old", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// An intervening write changes who currently owns server_ip.
	if _, err := s.Remember(ctxT(), "f-intervening", "server ip is 9.9.9.9", OriginExplicit, "server_ip", "f-old", nil, nil); err != nil {
		t.Fatal(err)
	}
	// The STALE call still claims to be replacing f-old.
	if _, err := s.Remember(ctxT(), "f-stale", "server ip is 5.6.7.8", OriginExplicit, "server_ip", "f-old", nil, nil); err == nil {
		t.Fatal("expected a refusal: replaces_id is stale, the intervening write already changed the current owner")
	}
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-intervening" {
		t.Fatalf("the intervening fact must be unretargeted by the stale call: %v %v", hits, err)
	}
}

// Detector (code-review finding, codex, round 2 HIGH, live-reproduced —
// the ABA counterexample that defeated the ROUND-1 content-based check):
// A(V) is superseded by B(W) which is superseded by C(V) — C happens to
// share A's OLD content. A stale approval computed against A, naming
// A's CONTENT as proof, would wrongly re-validate against C (same
// content) and silently retarget onto the WRONG fact. Binding to A's id
// instead of its content closes this: C's id is never A's id, regardless
// of what content cycles back.
func TestAudnReplacesIDSurvivesContentABACycle(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	const v = "server ip is 1.2.3.4"
	if _, err := s.Remember(ctxT(), "a", v, OriginExplicit, "server_ip", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "b", "server ip is 9.9.9.9", OriginExplicit, "server_ip", "a", nil, nil); err != nil {
		t.Fatal(err)
	}
	// c's content cycles back to V — the SAME content "a" originally had.
	if _, err := s.Remember(ctxT(), "c", v, OriginExplicit, "server_ip", "b", nil, nil); err != nil {
		t.Fatal(err)
	}
	// A stale call, approved back when "a" (content V) was live, replays
	// now — content-wise V still matches c's current content, but c's id
	// is not "a".
	if _, err := s.Remember(ctxT(), "stale", "server ip is 5.6.7.8", OriginExplicit, "server_ip", "a", nil, nil); err == nil {
		t.Fatal("expected a refusal: replaces_id names \"a\", not the current owner \"c\", despite matching content")
	}
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].ID != "c" {
		t.Fatalf("the ABA replay must not retarget c: %v %v", hits, err)
	}
}

// Identical restatement (same claim_key, same content) is a true no-op —
// not a contradiction, nothing new recorded, and the reported outcome is
// HONEST about which id actually holds the claim (code-review finding,
// codex, round 1 MEDIUM: previously the tool reported a fabricated id
// that was never inserted).
func TestAudnNoOpOnIdenticalRestatement(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	out, err := s.Remember(ctxT(), "f-2", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "f-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "noop" || out.EffectiveID != "f-1" {
		t.Fatalf("outcome = %+v, want noop/f-1 (never the id that was never inserted)", out)
	}
	rows, err := s.All(ctxT())
	if err != nil || len(rows) != 1 {
		t.Fatalf("identical restatement must not create a new row: %v %v", rows, err)
	}
	hits, _ := s.Recall(ctxT(), "server")
	if len(hits) != 1 || hits[0].ID != "f-1" {
		t.Fatalf("recall must still return the original: %v", hits)
	}
}

// Detector (code-review finding, codex, round 2 MEDIUM, live-reproduced):
// reusing the SAME id across two Remember calls with identical content
// must report "noop" honestly, never a fabricated "added" for a row this
// call never touched — the post-append check can't tell "I just inserted
// id" from "id already existed" without an explicit pre-check.
func TestAudnReusedIDWithDifferentIntentIsRefused(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "server ip is 1.2.3.4", OriginExplicit, "my_key", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// Same id, but a DIFFERENT intent this time (replaces_id differs) —
	// this is NOT a retry of the same call, it's id reuse for a second,
	// distinct write. Must be refused, never silently reinterpreted.
	if _, err := s.Remember(ctxT(), "f-1", "server ip is 1.2.3.4", OriginExplicit, "my_key", "f-1", nil, nil); err == nil {
		t.Fatal("expected a refusal: id reused for a different intent")
	}
	rows, err := s.All(ctxT())
	if err != nil || len(rows) != 1 {
		t.Fatalf("the refused call must not create a second row: %v %v", rows, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 3
// MEDIUM): an id that pre-exists under a completely UNRELATED claim_key
// must not have that unrelated identity misattributed as the effective
// owner of THIS call's target claim_key. "does id already exist" was the
// wrong question — the outcome must always reflect who ACTUALLY owns the
// target claim_key, never the caller's own (possibly reused, possibly
// coincidental) id.
func TestAudnReusedIDFromUnrelatedClaimIsRefused(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	// "collision" already exists, but for a totally different claim.
	if _, err := s.Remember(ctxT(), "collision", "favorite color is blue", OriginExplicit, "favorite_color", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// The real owner of "target_key" is a different fact entirely.
	if _, err := s.Remember(ctxT(), "owner", "server ip is 1.2.3.4", OriginExplicit, "target_key", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// A call reuses "collision" as its id, but with a COMPLETELY
	// different intent (different claim_key, different content) — this
	// is id reuse across two distinct writes, never a retry, and must be
	// refused outright rather than misattributed or silently no-opped
	// (code-review finding, codex, round 3 MEDIUM — the original
	// counterexample this test is descended from — closed at the
	// intent-guard level in round 4 rather than by attribution alone).
	if _, err := s.Remember(ctxT(), "collision", "server ip is 1.2.3.4", OriginExplicit, "target_key", "owner", nil, nil); err == nil {
		t.Fatal("expected a refusal: \"collision\" was already used for a different remember call")
	}
	// Both pre-existing facts must be completely untouched by the
	// refused call.
	hits, err := s.Recall(ctxT(), "blue")
	if err != nil || len(hits) != 1 || hits[0].ID != "collision" {
		t.Fatalf("the unrelated fact under the reused id must be untouched: %v %v", hits, err)
	}
	hits, err = s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].ID != "owner" {
		t.Fatalf("target_key's real owner must be untouched by the refused call: %v %v", hits, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 5
// MEDIUM): an id created OUTSIDE Remember entirely (via plain SaveFact,
// exactly like every pre-Audn fact in the real corpus) has NO
// mem_remember_outcomes row to catch its reuse — and if the reusing call
// happens to land on the identical-restatement no-op branch, it never
// touches mem_facts either, so nothing backstops it. Reusing such an id
// must still be refused, not silently accepted as a noop against some
// unrelated claim_key.
func TestAudnReusedIDFromPreAudnSaveFactIsRefused(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	// "collision" is a genuine pre-Audn fact — created via SaveFact, the
	// same mechanism every historical fact in the real corpus used,
	// NEVER through Remember (so it has no mem_remember_outcomes row).
	if err := s.SaveFact(ctxT(), "collision", "favorite color is blue"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "owner", "server ip is 1.2.3.4", OriginExplicit, "target_key", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// Reusing "collision" for a call whose content happens to match
	// target_key's CURRENT value exactly — this would land on the
	// identical-restatement no-op branch, which touches neither
	// mem_facts nor (before this fix) mem_remember_outcomes for this id.
	if _, err := s.Remember(ctxT(), "collision", "server ip is 1.2.3.4", OriginExplicit, "target_key", "owner", nil, nil); err == nil {
		t.Fatal("expected a refusal: \"collision\" already denotes a pre-Audn fact")
	}
	hits, err := s.Recall(ctxT(), "blue")
	if err != nil || len(hits) != 1 || hits[0].ID != "collision" {
		t.Fatalf("the pre-Audn fact under the reused id must be untouched: %v %v", hits, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 6
// MEDIUM): the round-5 guard was asymmetric — it stopped Remember from
// reusing an id SaveFact already created, but not the reverse. A
// Remember call that lands on the identical-restatement no-op branch
// reserves its own id ONLY in mem_remember_outcomes (it never touches
// mem_facts at all), so a LATER SaveFact call reusing that same id
// sailed through untouched. The fix moved the guard into the ONE
// insert() path every caller shares.
func TestAudnSaveFactCannotReuseIDReservedByRememberNoOp(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "owner", "server ip is 1.2.3.4", OriginExplicit, "target_key", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// This lands on the identical-restatement no-op branch: "reserved"
	// never gets a mem_facts row, only a mem_remember_outcomes one.
	out, err := s.Remember(ctxT(), "reserved", "server ip is 1.2.3.4", OriginExplicit, "target_key", "owner", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "noop" || out.EffectiveID != "owner" {
		t.Fatalf("outcome = %+v, want noop/owner", out)
	}
	// A later, completely unrelated SaveFact must not be able to reuse
	// "reserved" — that id has already been recorded by the no-op above.
	if err := s.SaveFact(ctxT(), "reserved", "unrelated content"); err == nil {
		t.Fatal("expected a refusal: \"reserved\" is already recorded by an earlier Remember no-op")
	}
	hits, err := s.Recall(ctxT(), "unrelated")
	if err != nil || len(hits) != 0 {
		t.Fatalf("the refused SaveFact must not have created a row: %v %v", hits, err)
	}
}

// Detector (the SECOND live counterexample from round 6): an invalid-
// UTF-8 id must be refused BEFORE the event is even journaled — never
// committed and then reported as an error, which would leave a fact
// silently present despite Remember returning failure.
func TestAudnRefusesInvalidUTF8ID(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	badID := string([]byte{'i', 'd', '-', 0xff})
	if _, err := s.Remember(ctxT(), badID, "valid content", OriginExplicit, "key", "", nil, nil); err == nil {
		t.Fatal("expected a refusal: invalid UTF-8 id")
	}
	rows, err := s.All(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a refused call must never commit state: %+v", rows)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 7
// MEDIUM): the SAME invalid-UTF-8 sanitization problem exists one layer
// up, at the model-facing TOOL boundary — Go's json.Unmarshal silently
// substitutes U+FFFD for invalid bytes DURING decoding, so a check on
// the DECODED Go string (Store's own validUTF8) can never see the
// model's original bytes. codex's exact escalation: an invalid raw
// replaces_id normalized to a DIFFERENT existing fact's real id,
// defeating the exact-target check entirely. The fix checks the RAW
// argument bytes before this handler's own json.Unmarshal ever runs.
func TestMemoryRememberToolRefusesInvalidUTF8Arguments(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "victim", "the real value"); err != nil {
		t.Fatal(err)
	}
	tools := Tools(s, redact.None{})
	// content contains a raw invalid UTF-8 byte (0xff) — still inside
	// otherwise well-formed JSON.
	args := append([]byte(`{"content":"x`), 0xff)
	args = append(args, []byte(`","supersedes":"victim"}`)...)
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-bad-utf8", ToolID: "memory_remember", Arguments: args,
		ArgsSchemaHash: "memory_remember.v4", Effect: contracts.EffectReversible,
		ExecutionKind: contracts.ExecInProcess, Deadline: timeNowPlusMinute(), AttemptNo: 1,
		ProfileID: "work", IdempotencyKey: strPtr("ik-bad-utf8"),
	})
	if err != nil {
		t.Fatalf("constructing the call itself must not fail: %v", err)
	}
	if _, err := tools["memory_remember"](ctxT(), call); err == nil {
		t.Fatal("expected a refusal: invalid UTF-8 in tool arguments")
	}
	hits, err := s.Recall(ctxT(), "real")
	if err != nil || len(hits) != 1 || hits[0].ID != "victim" {
		t.Fatalf("the target fact must be completely untouched by the refused call: %v %v", hits, err)
	}
}

// Detector: a NARROWER class than a raw invalid byte — a JSON `\uD800`-
// style escape for an unpaired UTF-16 surrogate is perfectly valid
// ASCII/UTF-8 TEXT in the wire bytes (validRawUTF8's own check sees
// nothing wrong), but Go's JSON decoder still silently substitutes
// U+FFFD for it while decoding, corrupting the string exactly like a raw
// invalid byte would, just via a mechanism invisible at the raw-byte
// layer. Rejecting any string containing the replacement character
// itself, post-decode, closes this uniformly.
func TestMemoryRememberToolRefusesUnpairedSurrogateEscape(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "victim", "the real value"); err != nil {
		t.Fatal(err)
	}
	tools := Tools(s, redact.None{})
	// Every byte here is plain ASCII — valid UTF-8 at the wire level —
	// but \ud800 is an unpaired high surrogate with no valid decoding.
	args := []byte(`{"content":"x\ud800y","supersedes":"victim"}`)
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-surrogate", ToolID: "memory_remember", Arguments: args,
		ArgsSchemaHash: "memory_remember.v4", Effect: contracts.EffectReversible,
		ExecutionKind: contracts.ExecInProcess, Deadline: timeNowPlusMinute(), AttemptNo: 1,
		ProfileID: "work", IdempotencyKey: strPtr("ik-surrogate"),
	})
	if err != nil {
		t.Fatalf("constructing the call itself must not fail: %v", err)
	}
	if _, err := tools["memory_remember"](ctxT(), call); err == nil {
		t.Fatal("expected a refusal: unpaired surrogate escape decodes to a replacement character")
	}
	hits, err := s.Recall(ctxT(), "real")
	if err != nil || len(hits) != 1 || hits[0].ID != "victim" {
		t.Fatalf("the target fact must be completely untouched by the refused call: %v %v", hits, err)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 9
// MEDIUM): the round-8 fix validated id/content/claim_key/replaces_id at
// every entry point but MISSED tags/lineage on three of the four
// (SaveFactLineage, Propose, SupersedeLineageWithClaim — only Remember
// happened to already cover them). An unpaired surrogate escape inside a
// TAG passes the raw-byte check, decodes to U+FFFD, and — since tags
// drive RecallTag's exact-match retrieval — silently corrupts a
// behaviorally significant field via the plain SaveFact path, which
// never even routes through Remember's own checks.
func TestMemoryRememberToolRefusesUnpairedSurrogateInTag(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	tools := Tools(s, redact.None{})
	args := []byte(`{"content":"ordinary fact","tags":["team-\ud800"]}`)
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-tag-surrogate", ToolID: "memory_remember", Arguments: args,
		ArgsSchemaHash: "memory_remember.v4", Effect: contracts.EffectReversible,
		ExecutionKind: contracts.ExecInProcess, Deadline: timeNowPlusMinute(), AttemptNo: 1,
		ProfileID: "work", IdempotencyKey: strPtr("ik-tag-surrogate"),
	})
	if err != nil {
		t.Fatalf("constructing the call itself must not fail: %v", err)
	}
	if _, err := tools["memory_remember"](ctxT(), call); err == nil {
		t.Fatal("expected a refusal: unpaired surrogate escape in a tag decodes to a replacement character")
	}
	rows, err := s.All(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("the refused call must not commit a corrupted tag: %+v", rows)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 10
// MEDIUM): the write-side fixes closed every path that could CREATE a
// U+FFFD-corrupted string, but memory_recall's READ path hadn't been
// checked symmetrically — a query tag containing an unpaired surrogate
// escape is valid ASCII (validRawUTF8 sees nothing wrong) but decodes to
// the SAME U+FFFD a corrupted historical/pre-fix record might carry,
// letting a query silently match the WRONG fact instead of refusing.
func TestMemoryRecallToolRefusesUnpairedSurrogateInTagQuery(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	tools := Tools(s, redact.None{})
	args := []byte(`{"tag":"team-\ud800"}`)
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-recall-surrogate", ToolID: "memory_recall", Arguments: args,
		ArgsSchemaHash: "memory_recall.v1", Effect: contracts.EffectReadOnly,
		ExecutionKind: contracts.ExecInProcess, Deadline: timeNowPlusMinute(), AttemptNo: 1,
		ProfileID: "work",
	})
	if err != nil {
		t.Fatalf("constructing the call itself must not fail: %v", err)
	}
	if _, err := tools["memory_recall"](ctxT(), call); err == nil {
		t.Fatal("expected a refusal: unpaired surrogate escape in a recall tag query")
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 5
// MEDIUM): comparing raw post-redaction event bytes as "intent" is wrong
// — the known-ref redactor reserializes JSON with sorted keys whenever
// ANY reference is configured, even one entirely unrelated to this
// call's own content, so the SAME logical call could journal to
// different byte strings depending on redactor state that has nothing
// to do with it. A genuine retry, issued after the redactor's
// configuration changed (e.g. the profile restarted with a newly
// registered secret reference), must still be recognized as the SAME
// intent — never wrongly refused.
func TestAudnIntentSurvivesRedactorReconfiguration(t *testing.T) {
	layout := testLayout(t)
	dir, err := layout.ProfileDir("work")
	if err != nil {
		t.Fatal(err)
	}
	if err := pathxEnsure(layout, dir); err != nil {
		t.Fatal(err)
	}
	path := dir + "/journal.db"

	// First attempt: no known references configured at all.
	j1, err := journal.Open(path, "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	s1, err := NewStore(j1)
	if err != nil {
		t.Fatal(err)
	}
	out1, err := s1.Remember(ctxT(), "same-call", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := j1.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen with a KnownRefs redactor now configured for an UNRELATED
	// secret — this changes how the journal serializes/redacts payloads
	// in general, even though nothing in THIS call's content matches.
	r := redact.NewKnownRefs(map[string]string{"UNRELATED": "sk-unrelated-secret"})
	j2, err := journal.Open(path, "work", r, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	s2, err := NewStore(j2)
	if err != nil {
		t.Fatal(err)
	}
	// The EXACT same logical retry — same id, same content, same
	// claim_key, same replaces_id.
	out2, err := s2.Remember(ctxT(), "same-call", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil)
	if err != nil {
		t.Fatalf("genuine exact retry must not be refused after unrelated redactor reconfiguration: %v", err)
	}
	if out1 != out2 {
		t.Fatalf("retry outcome mismatch across redactor reconfiguration: first=%+v second=%+v", out1, out2)
	}
}

// Detector: a genuine RETRY of the exact same Remember call (same id —
// the codebase's own AttemptGrant model reuses the same ToolCallID
// across attempts, HARDQ E9) must stay idempotent, never hard-fail just
// because its own outcome was already recorded by a prior attempt.
func TestAudnRetryOfSameCallStaysIdempotent(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	// Attempt 1 of a second call, id "f-2", targeting server_ip with the
	// SAME content it already holds — a genuine no-op from the start.
	out1, err := s.Remember(ctxT(), "f-2", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "f-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Attempt 2: the exact same call retried (same id "f-2", nothing
	// about the world has changed) — must succeed identically, not
	// error on its own previously-recorded outcome.
	out2, err := s.Remember(ctxT(), "f-2", "server ip is 1.2.3.4", OriginExplicit, "server_ip", "f-1", nil, nil)
	if err != nil {
		t.Fatalf("a retry of the same call must stay idempotent, not error: %v", err)
	}
	if out1 != out2 {
		t.Fatalf("retry outcome mismatch: first=%+v second=%+v", out1, out2)
	}
}

// Detector (this is the LIVE COUNTEREXAMPLE codex constructed, round 4
// MEDIUM): retrying a call whose FIRST attempt actually ADDED a new fact
// must still report "added" on the retry — not silently flip to "noop"
// just because the retry's own claim_key lookup now finds itself as the
// new owner. A retry must return the ORIGINAL recorded outcome, never
// re-run the decision logic.
func TestAudnRetryOfSuccessfulAddPreservesOriginalOutcome(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	out1, err := s.Remember(ctxT(), "retry-add", "favorite color is blue", OriginExplicit, "favorite_color", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out1.Kind != "added" {
		t.Fatalf("first attempt outcome = %+v, want added", out1)
	}
	out2, err := s.Remember(ctxT(), "retry-add", "favorite color is blue", OriginExplicit, "favorite_color", "", nil, nil)
	if err != nil {
		t.Fatalf("a retry of a successful add must stay idempotent, not error: %v", err)
	}
	if out2 != out1 {
		t.Fatalf("retry must preserve the ORIGINAL outcome: first=%+v second=%+v", out1, out2)
	}
}

// Detector (the SECOND live counterexample from the same round): retrying
// a call whose first attempt SUPERSEDED an existing fact must still
// report "superseded" on the retry, not "noop".
func TestAudnRetryOfSuccessfulSupersedePreservesOriginalOutcome(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "old-fact", "birthday is march 3rd", OriginExplicit, "birthday", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	out1, err := s.Remember(ctxT(), "retry-change", "birthday is march 4th", OriginExplicit, "birthday", "old-fact", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out1.Kind != "superseded" {
		t.Fatalf("first attempt outcome = %+v, want superseded", out1)
	}
	out2, err := s.Remember(ctxT(), "retry-change", "birthday is march 4th", OriginExplicit, "birthday", "old-fact", nil, nil)
	if err != nil {
		t.Fatalf("a retry of a successful supersede must stay idempotent, not error: %v", err)
	}
	if out2 != out1 {
		t.Fatalf("retry must preserve the ORIGINAL outcome: first=%+v second=%+v", out1, out2)
	}
}

// An inference must never silently override a previously EXPLICIT fact
// (HARDQ-agy Finding 4, generalized) — it goes to review instead, and the
// contested fact's ContentionCount increments as a visibility signal.
// Detector (code-review finding, codex, round 1 HIGH, live-reproduced):
// ACCEPTING the contested proposal must ACTUALLY resolve the
// contradiction — the old fact must stop being "latest accepted" too,
// not remain alongside the newly-accepted one.
func TestAudnProposesInsteadOfOverridingExplicitFact(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "birthday is march 3rd", OriginExplicit, "birthday", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	out, err := s.Remember(ctxT(), "f-2", "birthday is april 1st", OriginInferred, "birthday", "f-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "proposed" || out.EffectiveID != "f-2" {
		t.Fatalf("outcome = %+v, want proposed/f-2", out)
	}
	hits, err := s.Recall(ctxT(), "birthday")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-1" {
		t.Fatalf("the explicit fact must still be the one recalled, unoverridden: %v %v", hits, err)
	}
	if hits[0].ContentionCount != 1 {
		t.Fatalf("contention_count = %d, want 1", hits[0].ContentionCount)
	}
	// The contested inference is NOT silently dropped — it exists, but
	// unreachable via Recall until a human Accepts it (the EXISTING
	// review gate, not new machinery).
	all, _ := s.All(ctxT())
	found := false
	for _, r := range all {
		if r.ID == "f-2" {
			found = true
			if r.Status != StatusProposed {
				t.Fatalf("contested inference status = %v, want proposed", r.Status)
			}
		}
	}
	if !found {
		t.Fatal("contested inference was silently dropped, not just held for review")
	}
	// Accepting it must ACTUALLY resolve the contradiction: f-1 stops
	// being latest-accepted, f-2 becomes the sole latest.
	if err := s.Accept(ctxT(), "f-2"); err != nil {
		t.Fatal(err)
	}
	hits, err = s.Recall(ctxT(), "birthday")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-2" {
		t.Fatalf("accepting the reviewed contradiction must resolve it, leaving f-2 as the sole latest: %v %v", hits, err)
	}
}

// Detector: REJECTING a contested proposal must leave the original fact
// completely unaffected and still the sole latest-accepted answer, AND
// must resolve the dispute rather than leaving a permanent false
// "unresolved claim" warning (code-review finding, codex, round 2 NOTE,
// live-reproduced: contention_count was never decremented on Reject).
func TestAudnRejectingContestedProposalLeavesOriginalIntact(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "birthday is march 3rd", OriginExplicit, "birthday", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-2", "birthday is april 1st", OriginInferred, "birthday", "f-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Reject(ctxT(), "f-2"); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "birthday")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-1" {
		t.Fatalf("rejecting the contest must leave the original untouched: %v %v", hits, err)
	}
	if hits[0].ContentionCount != 0 {
		t.Fatalf("contention_count = %d, want 0 — rejecting a contest must resolve it, not leave a permanent false warning", hits[0].ContentionCount)
	}
	// f-1 must still be correctable afterward — a rejected contest must
	// not permanently freeze it.
	if _, err := s.Remember(ctxT(), "f-3", "birthday is march 4th", OriginExplicit, "birthday", "f-1", nil, nil); err != nil {
		t.Fatalf("f-1 must still be correctable after the contest was rejected: %v", err)
	}
}

// Detector: if the contested fact is superseded through a DIFFERENT path
// (a named correction) while its contest is still pending, Accept must
// refuse — the leaf-check re-verifies the contested fact is STILL the
// latest accepted at accept-time, not merely at contest-creation time.
func TestAudnAcceptRefusesWhenContestedFactAlreadyHasSuccessor(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "birthday is march 3rd", OriginExplicit, "birthday", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-2", "birthday is april 1st", OriginInferred, "birthday", "f-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	// f-1 is corrected through the NAMED path before the contest is
	// resolved — f-1 is no longer the latest accepted.
	if err := s.SupersedeLineage(ctxT(), "f-1", "f-3", "birthday is march 4th", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Accept(ctxT(), "f-2"); err == nil {
		t.Fatal("expected a refusal: the contested fact already has a successor")
	}
	hits, err := s.Recall(ctxT(), "birthday")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-3" {
		t.Fatalf("the refused accept must leave f-3 as the sole latest: %v %v", hits, err)
	}
}

// An inference contradicting an EXISTING INFERENCE (not an explicit fact)
// is safe to auto-resolve — the unsafe direction is specifically
// inferred-overriding-explicit, not inferred-vs-inferred.
func TestAudnAutoSupersedesInferredOverInferred(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "seems to like coffee", OriginInferred, "drink_pref", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-2", "seems to like tea", OriginInferred, "drink_pref", "f-1", nil, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "tea")
	if err != nil || len(hits) != 1 || hits[0].ID != "f-2" {
		t.Fatalf("inferred-over-inferred must auto-resolve: %v %v", hits, err)
	}
}

// A new claim_key with no existing match is a plain add — replaces_id
// is irrelevant here (nothing to prove awareness of yet).
func TestAudnAddsWhenNoExistingClaim(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	out, err := s.Remember(ctxT(), "f-1", "favorite color is blue", OriginExplicit, "favorite_color", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "added" || out.EffectiveID != "f-1" {
		t.Fatalf("outcome = %+v, want added/f-1", out)
	}
	hits, err := s.Recall(ctxT(), "blue")
	if err != nil || len(hits) != 1 || hits[0].ClaimKey != "favorite_color" {
		t.Fatalf("plain add with claim_key failed: %v %v", hits, err)
	}
}

// An empty claim_key is byte-for-byte equivalent to SaveFactLineage —
// every existing caller that never opts into Audn must see identical
// behavior (no auto-resolution, no claim_key stored).
func TestAudnEmptyClaimKeyBehavesLikeSaveFact(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "the meeting is on friday", OriginExplicit, "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctxT(), "f-2", "the meeting is on friday too", OriginExplicit, "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "meeting")
	if err != nil || len(hits) != 2 {
		t.Fatalf("empty claim_key must never auto-resolve (plain independent facts): %v %v", hits, err)
	}
	for _, h := range hits {
		if h.ClaimKey != "" {
			t.Fatalf("empty claim_key must not be stored as a real key: %+v", h)
		}
	}
}

// claim_key with control characters/whitespace is refused at the journal
// boundary, same discipline as tags.
func TestAudnRejectsMalformedClaimKey(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "x", OriginExplicit, "two words", "", nil, nil); err == nil {
		t.Fatal("expected a rejection: claim_key must be a single token")
	}
}

// Detector (code-review finding, codex, round 1 MEDIUM, live-reproduced):
// a claim_key with leading/trailing whitespace must be REJECTED outright
// (not silently trimmed before storage) — a validator that checks the
// trimmed form but stores the ORIGINAL would let "server_ip " and
// "server_ip" become two silently divergent keys.
func TestAudnRejectsClaimKeyWithSurroundingWhitespace(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if _, err := s.Remember(ctxT(), "f-1", "x", OriginExplicit, "server_ip ", "", nil, nil); err == nil {
		t.Fatal("expected a rejection: claim_key has trailing whitespace")
	}
	if _, err := s.Remember(ctxT(), "f-2", "x", OriginExplicit, " server_ip", "", nil, nil); err == nil {
		t.Fatal("expected a rejection: claim_key has leading whitespace")
	}
}

// Detector (code-review finding, codex, round 2 MEDIUM, live-reproduced):
// a claim_key with EMBEDDED non-ASCII whitespace or control characters
// ("\r", NBSP, NUL) must be rejected too — round 1's check only caught
// literal " \t\n" and only at the leading/trailing edges.
func TestAudnRejectsClaimKeyWithEmbeddedShadowCharacters(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	bad := []string{"server\r_ip", "server\u00a0ip", "server\x00ip", "server\u2028ip"}
	for _, k := range bad {
		if _, err := s.Remember(ctxT(), "f-x", "x", OriginExplicit, k, "", nil, nil); err == nil {
			t.Fatalf("expected a rejection for embedded shadow character in claim_key %q", k)
		}
	}
}

// Detector (code-review finding, kilo, round 3 MEDIUM, live-reproduced):
// round 2's deny-list (IsSpace + IsControl) missed Unicode FORMAT
// characters -- none of these are space or control, but all are
// invisible, and all passed the round-2 check, letting "server_ip" and
// one of these embedded mid-key coexist as silently divergent shadow
// keys. The round-3 fix switched to an ALLOW-list (letters, digits, and
// a small punctuation set) so this closes as a CLASS, not one character
// at a time.
func TestAudnRejectsClaimKeyWithInvisibleFormatCharacters(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	bad := []string{
		"server\u200b_ip", // zero-width space
		"server\u200c_ip", // ZWNJ
		"server\u200d_ip", // ZWJ
		"server\ufeff_ip", // BOM
	}
	for _, k := range bad {
		if _, err := s.Remember(ctxT(), "f-x", "x", OriginExplicit, k, "", nil, nil); err == nil {
			t.Fatalf("expected a rejection for invisible format character in claim_key %q", k)
		}
	}
}

func TestSupersedeWithClaimBootstrapsFutureAudnLookups(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	// A pre-Audn fact — no claim_key at all, exactly like every fact that
	// existed before this feature shipped.
	if err := s.SaveFact(ctxT(), "f-old", "server ip is 1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.SupersedeLineageWithClaim(ctxT(), "f-old", "f-new", "server ip is 5.6.7.8", "server_ip", nil, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].ClaimKey != "server_ip" {
		t.Fatalf("the bootstrapped fact must now carry claim_key: %v %v", hits, err)
	}
	// A FUTURE claim_key-based correction must now find it.
	out, err := s.Remember(ctxT(), "f-newer", "server ip is 9.9.9.9", OriginExplicit, "server_ip", "f-new", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "superseded" {
		t.Fatalf("outcome = %+v, want superseded (the bootstrapped fact must be Audn-visible)", out)
	}
}

// Detector (code-review finding, codex, round 2 HIGH, live-reproduced):
// bootstrapping a claim_key onto TWO unrelated named corrections must not
// create two simultaneously "latest accepted" facts under the same key —
// that would silently split future Audn lookups.
func TestSupersedeWithClaimRefusesSecondUnrelatedOwner(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "a1", "fact A"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFact(ctxT(), "b1", "fact B"); err != nil {
		t.Fatal(err)
	}
	if err := s.SupersedeLineageWithClaim(ctxT(), "a1", "a2", "fact A updated", "shared_key", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SupersedeLineageWithClaim(ctxT(), "b1", "b2", "fact B updated", "shared_key", nil, nil); err == nil {
		t.Fatal("expected a refusal: shared_key is already owned by a2, a different lineage")
	}
	all, err := s.All(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range all {
		if r.ClaimKey == "shared_key" && r.Status == StatusAccepted {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("must never have more than one live owner of a claim_key: %d", n)
	}
	// Re-bootstrapping the SAME lineage (a2 itself, via a further named
	// correction) must still work — that's the ordinary case.
	if err := s.SupersedeLineageWithClaim(ctxT(), "a2", "a3", "fact A updated again", "shared_key", nil, nil); err != nil {
		t.Fatalf("re-supersede of the SAME claim_key owner must still work: %v", err)
	}
}

// Detector (code-review finding, agy, round 3 NOTE): a caller who copies
// memory_recall's rendered "[id]" verbatim, brackets included, must still
// succeed — pure format tolerance, not a weaker check (the unwrapped
// value still must match the real owner exactly).
func TestMemoryRememberToolToleratesBracketedReplacesID(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	tools := Tools(s, redact.None{})
	call1, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-1", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"server ip is 1.2.3.4","claim_key":"server_ip"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-1"),
	})
	if _, err := tools["memory_remember"](ctxT(), call1); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 {
		t.Fatalf("setup recall failed: %v %v", hits, err)
	}
	call2, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-2", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"server ip is 5.6.7.8","claim_key":"server_ip","replaces_id":"[` + hits[0].ID + `]"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-2"),
	})
	if _, err := tools["memory_remember"](ctxT(), call2); err != nil {
		t.Fatalf("bracketed replaces_id must be tolerated: %v", err)
	}
	hits, err = s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].Content != "server ip is 5.6.7.8" {
		t.Fatalf("bracketed replaces_id call failed to apply: %v %v", hits, err)
	}
}

// The memory_remember TOOL routes a claim_key-only call through Audn's
// auto-resolution (no supersedes id needed from the model), PROVING
// awareness via replaces_id (the [id] recall showed) on the second call.
func TestMemoryRememberToolRoutesClaimKeyThroughAudn(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	tools := Tools(s, redact.None{})
	call1, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-1", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"server ip is 1.2.3.4","claim_key":"server_ip"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-1"),
	})
	if _, err := tools["memory_remember"](ctxT(), call1); err != nil {
		t.Fatal(err)
	}
	// Obtain the real id rather than assuming the tool's internal id
	// scheme — this is what a real caller must do too (via memory_recall,
	// proven separately in TestMemoryRecallToolExposesFactID).
	hits, err := s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 {
		t.Fatalf("setup recall failed: %v %v", hits, err)
	}
	call2, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-2", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"server ip is 5.6.7.8","claim_key":"server_ip","replaces_id":"` + hits[0].ID + `"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-2"),
	})
	if _, err := tools["memory_remember"](ctxT(), call2); err != nil {
		t.Fatal(err)
	}
	hits, err = s.Recall(ctxT(), "server")
	if err != nil || len(hits) != 1 || hits[0].Content != "server ip is 5.6.7.8" {
		t.Fatalf("claim_key routing through the tool failed: %v %v", hits, err)
	}
}

// Detector: the TOOL refuses a claim_key correction with no replaces_id,
// exactly like the Store-level check.
func TestMemoryRememberToolRefusesClaimKeyWithoutReplacesID(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	tools := Tools(s, redact.None{})
	call1, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-1", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"server ip is 1.2.3.4","claim_key":"server_ip"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-1"),
	})
	if _, err := tools["memory_remember"](ctxT(), call1); err != nil {
		t.Fatal(err)
	}
	call2, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-2", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"server ip is 5.6.7.8","claim_key":"server_ip"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-2"),
	})
	if _, err := tools["memory_remember"](ctxT(), call2); err == nil {
		t.Fatal("expected a refusal: no replaces_id given for an existing claim_key")
	}
}

// The recall TOOL exposes each fact's [id] so a caller can echo it back
// as replaces_id — without this the claim_key correction path above is
// unusable by a real caller who never sees the internal id otherwise.
func TestMemoryRecallToolExposesFactID(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-visible", "server ip is 1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	toolset := Tools(s, redact.None{})
	call, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "recall-1", ToolID: "memory_recall",
		Arguments: []byte(`{"query":"server"}`), ArgsSchemaHash: "memory_recall.v1",
		Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
	})
	out, err := toolset["memory_recall"](ctxT(), call)
	if err != nil {
		t.Fatal(err)
	}
	if out.Output[0].Content == nil || !strings.Contains(*out.Output[0].Content, "f-visible") {
		t.Fatalf("recall output must expose the fact id for future replaces_id use: %v", out.Output[0].Content)
	}
}

// An explicit supersedes id wins over claim_key when the model supplies
// BOTH — named intent is never overridden by the automatic heuristic —
// but claim_key still bootstraps the corrected fact for future lookups.
func TestMemoryRememberToolSupersedesWinsOverClaimKey(t *testing.T) {
	s, _ := openProfile(t, testLayout(t), "work")
	if err := s.SaveFact(ctxT(), "f-orig", "the old value"); err != nil {
		t.Fatal(err)
	}
	toolset := Tools(s, redact.None{})
	call, _ := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "call-3", ToolID: "memory_remember",
		Arguments:      []byte(`{"content":"the new value","supersedes":"f-orig","claim_key":"unrelated_key"}`),
		ArgsSchemaHash: "memory_remember.v4",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: timeNowPlusMinute(), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: strPtr("ik-3"),
	})
	if _, err := toolset["memory_remember"](ctxT(), call); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Recall(ctxT(), "value")
	if err != nil || len(hits) != 1 || hits[0].Content != "the new value" || hits[0].ClaimKey != "unrelated_key" {
		t.Fatalf("named supersedes must win over claim_key for TARGETING, but claim_key must still bootstrap: %v %v", hits, err)
	}
}

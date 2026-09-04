//go:build linux

// T24 RED table (tasks-P0): durable remote HITL — ApprovalChallenge is
// exact-intent (canonical tool+args+target+ProfileID hash, C4), expiring,
// single-use; DecisionAsk commits TurnSuspended and the loop EXITS;
// ApprovalReceived resumes from the journal. REDs: approve-after-daemon-
// restart completes exactly once; replay → APPROVAL_REPLAY; modified args
// under an old approval → rejected; cross-profile (B3): a challenge
// created in work can NEVER be seen or approved via a private-bound
// context, incl. across restart; yolo (F2 policy half): ASK auto-allows
// journaled and a channel message can neither enable yolo nor authorize
// a DENY effect.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

func open(t *testing.T, dir string, clock clockid.Clock, profile contracts.ProfileID) (*Store, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(filepath.Join(dir, string(profile)+".db"), profile, redact.None{}, Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	s, err := NewStore(j, clock)
	if err != nil {
		t.Fatal(err)
	}
	return s, j
}

func call(tool, id, args string) contracts.ToolCall {
	idem := "ik-" + id
	c, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: contracts.ToolCallID(id), ToolID: contracts.ToolID(tool),
		Arguments: json.RawMessage(args), ArgsSchemaHash: "h1",
		Effect: contracts.EffectIrreversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: time.Now().Add(time.Hour), AttemptNo: 1,
		IdempotencyKey: &idem, ProfileID: "work",
	})
	if err != nil {
		panic(err)
	}
	return c
}

// The full durable cycle: Suspend commits TurnSuspended + the challenge;
// a PENDING challenge is listable; Approve appends ApprovalReceived; the
// approval admits the EXACT call once — and everything survives a
// DAEMON RESTART between suspend and approve (the ledger literal).
func TestApproveAfterRestartCompletesOnce(t *testing.T) {
	dir := t.TempDir()
	clock := clockid.NewFake(time.Now())
	s, j := open(t, dir, clock, "work")
	c := call("rm_file", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if ch.ChallengeID == "" {
		t.Fatal("no challenge id")
	}
	// RESTART: fresh store over the same journal.
	j.Close()
	s2, _ := open(t, dir, clock, "work")
	pending, err := s2.Pending(ctxT())
	if err != nil || len(pending) != 1 || pending[0].ChallengeID != ch.ChallengeID {
		t.Fatalf("challenge lost across restart: %v %v", pending, err)
	}
	if err := s2.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	// The approval admits the EXACT call exactly once.
	if err := s2.ConsumeApproval(ctxT(), c); err != nil {
		t.Fatalf("approved exact call refused: %v", err)
	}
	if err := s2.ConsumeApproval(ctxT(), c); err == nil {
		t.Fatal("approval consumed twice")
	}
	// The resumed turn context is recoverable from the journal.
	turn, run, ok, err := s2.SuspendedTurn(ctxT(), ch.ChallengeID)
	if err != nil || !ok || turn != "turn-1" || run != "run-1" {
		t.Fatalf("suspended turn not rehydratable: %v %v %v %v", turn, run, ok, err)
	}
}

// Replay: approving an ALREADY-approved challenge → APPROVAL_REPLAY.
func TestApprovalReplayRejected(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("rm_file", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	err = s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42")
	if !errors.Is(err, ErrApprovalReplay) {
		t.Fatalf("replayed approval must fail APPROVAL_REPLAY: %v", err)
	}
	// Approving an unknown challenge fails closed.
	if err := s.Approve(ctxT(), "ch-never", "tg:chat-42"); err == nil {
		t.Fatal("unknown challenge approved")
	}
}

// test_approval_cannot_authorize_modified_effect (C4 literal): an
// approval hashes the EXACT effect — modified args never ride it.
func TestApprovalCannotAuthorizeModifiedEffect(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("rm_file", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	tampered := call("rm_file", "tc-1", `{"path":"/etc/shadow"}`)
	if err := s.ConsumeApproval(ctxT(), tampered); err == nil {
		t.Fatal("modified args rode the old approval")
	}
	// The ORIGINAL still consumes (the tamper attempt burned nothing).
	if err := s.ConsumeApproval(ctxT(), c); err != nil {
		t.Fatalf("original refused after tamper attempt: %v", err)
	}
}

// Expiry: an expired challenge can neither be approved nor consumed.
func TestExpiredChallengeDead(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("rm_file", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(DefaultChallengeTTL + time.Minute)
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err == nil {
		t.Fatal("expired challenge approved")
	}
}

// Cross-profile (B3 chain literal): a challenge created in the WORK
// journal is invisible and unapprovable through the PRIVATE journal —
// including across restart (physically separate files; the RED proves
// the API surfaces nothing).
func TestCrossProfileChallengeInvisible(t *testing.T) {
	dir := t.TempDir()
	clock := clockid.NewFake(time.Now())
	work, _ := open(t, dir, clock, "work")
	private, _ := open(t, dir, clock, "private")
	c := call("rm_file", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := work.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := private.Pending(ctxT()); len(p) != 0 {
		t.Fatalf("work challenge visible in private: %v", p)
	}
	if err := private.Approve(ctxT(), ch.ChallengeID, "tg:chat-99"); err == nil {
		t.Fatal("work challenge approved through private")
	}
	// Work still owns it.
	if p, _ := work.Pending(ctxT()); len(p) != 1 {
		t.Fatal("work lost its own challenge")
	}
}

// Deny: an explicit denial closes the challenge; consume fails.
func TestDenyClosesChallenge(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("rm_file", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deny(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumeApproval(ctxT(), c); err == nil {
		t.Fatal("denied challenge consumed")
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err == nil {
		t.Fatal("denied challenge later approved")
	}
	if p, _ := s.Pending(ctxT()); len(p) != 0 {
		t.Fatalf("denied challenge still pending: %v", p)
	}
}

// The challenge SUMMARY shown to the approver names the exact effect
// (tool + target) — never raw secrets (redactor-clean by construction:
// the payload passes the journal's secret gates like every event).
func TestChallengeSummaryNamesEffect(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("rm_file", "tc-9", `{"path":"/home/x/notes.txt"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ch.Summary, "rm_file") || !strings.Contains(ch.Summary, "/home/x/notes.txt") {
		t.Fatalf("summary does not name the effect: %q", ch.Summary)
	}
}

// SOURCE BINDING (Phase-5 codex #6): only the ORIGINATING channel
// identity may decide its challenge — a foreign chat's approve/deny is
// refused and the challenge stays PENDING.
func TestForeignSourceCannotDecide(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("fs_delete", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-666"); err == nil {
		t.Fatal("foreign chat approved the challenge")
	}
	if err := s.Deny(ctxT(), ch.ChallengeID, "tg:chat-666"); err == nil {
		t.Fatal("foreign chat denied the challenge")
	}
	if err := s.ConsumeApproval(ctxT(), c); err == nil {
		t.Fatal("foreign decision produced a consumable approval")
	}
	// The rightful owner still can.
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumeApproval(ctxT(), c); err != nil {
		t.Fatal(err)
	}
}

// EXPIRY AT CONSUME with the >= boundary (Phase-5 codex #7): an approval
// granted in time but consumed at/after expiry is dead — the grant window
// closes at the consume moment, not the approve moment.
func TestApprovalExpiresAtConsume(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("fs_delete", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	// Advance EXACTLY to expiry: `now >= expires` must already refuse.
	clock.Advance(DefaultChallengeTTL)
	if err := s.ConsumeApproval(ctxT(), c); err == nil {
		t.Fatal("approval consumed at the expiry boundary")
	}
}

// SECRET REFUSAL (Phase-5 kilo #3): a challenge whose summary would
// carry bytes the redactor rewrites is REFUSED before any journal write —
// the approver channel never sees a known secret.
func TestChallengeRefusesSecretExposure(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	secret := "sk-live-abcdef123456"
	j, err := journal.Open(filepath.Join(t.TempDir(), "work.db"), "work",
		redact.NewKnownRefs(map[string]string{"api_key": secret}), Events(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	s, err := NewStore(j, clock)
	if err != nil {
		t.Fatal(err)
	}
	c := call("web_post", "tc-1", `{"auth":"`+secret+`"}`)
	if _, serr := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request")); serr == nil {
		t.Fatal("challenge exposing a known secret was created")
	}
	if p, _ := s.Pending(ctxT()); len(p) != 0 {
		t.Fatalf("refused challenge left state behind: %v", p)
	}
	// A clean call on the same store still works (the refusal is per-payload).
	if _, serr := s.Suspend(ctxT(), "turn-2", "run-2", call("fs_delete", "tc-2", `{"path":"/tmp/x"}`), "tg:chat-42", testBlocks(t, "original request")); serr != nil {
		t.Fatal(serr)
	}
}

// PROJECTION-ENFORCED expiry (Phase-5-r2 codex #5 / kilo #5): a decision
// event whose EVENT-OWNED time is past expiry is refused inside the
// append transaction itself — the API precheck race cannot commit it.
func TestProjectionRefusesExpiredDecision(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, j := open(t, t.TempDir(), clock, "work")
	c := call("fs_delete", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	// Bypass the API precheck: append the receipt DIRECTLY with an
	// emitted-at past expiry (the race's losing interleaving).
	payload, _ := json.Marshal(map[string]string{"challenge_id": ch.ChallengeID, "source": "tg:chat-42"})
	_, aerr := j.Append(ctxT(), contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID: "ev-race-1", EventType: EvApprovalReceived, RunID: "run-1",
		EmittedAt: clock.Now().Add(DefaultChallengeTTL + time.Minute),
		ActorType: contracts.ActorSystem, ActorID: "test", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: "work", AttemptNo: 1,
		Payload: payload, PayloadHash: "recomputed",
	})
	if aerr == nil {
		t.Fatal("projection committed a decision emitted past expiry")
	}
	if st, _, _, _ := s.challengeRow(ctxT(), ch.ChallengeID); st != "PENDING" {
		t.Fatalf("challenge left PENDING state on a refused decision: %s", st)
	}
}

// UNPREDICTABLE challenge ids (Phase-5-r2 codex #12-detector): two
// challenges over the SAME exact effect must get DIFFERENT ids — the old
// hash-prefix scheme would collide here.
func TestChallengeIDsUnpredictable(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	c := call("fs_delete", "tc-1", `{"path":"/tmp/x"}`)
	ch1, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := s.Suspend(ctxT(), "turn-2", "run-2", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if ch1.ChallengeID == ch2.ChallengeID {
		t.Fatalf("challenge ids derived from the effect hash: %s == %s", ch1.ChallengeID, ch2.ChallengeID)
	}
}

// testBlocks builds a minimal USER context slice for Suspend (resume
// rehydration currency).
func testBlocks(t *testing.T, text string) []contracts.ContextBlock {
	t.Helper()
	b, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: "blk-1", Kind: "user_message", Content: &text,
		ContentHash: "0000000000000000000000000000000000000000000000000000000000000000",
		SourceURI:   "nexus://telegram/chat-42", Producer: "telegram",
		Trust: contracts.TrustUser, Sensitivity: contracts.Sensitivity(1),
		Lineage: []string{}, ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return []contracts.ContextBlock{b}
}

// RETRY is ONE atomic batch (Phase-5-r4 codex #1): a SIGKILL inside the
// close-old+issue-new recipe leaves BOTH-or-NEITHER — never the old
// reconcile item open alongside a new live challenge (two consumable
// authorizations for one UNKNOWN effect).
func TestRetryRecipeAtomicUnderSigkill(t *testing.T) {
	if os.Getenv("APPR_RETRY_CHILD") == "1" {
		retryCrashChild()
		return
	}
	dir := t.TempDir()
	// Parent prepares the CONSUMED-and-open state WITHOUT the kill seam.
	clock := clockid.NewFake(time.Now())
	s, j := open(t, dir, clock, "work")
	c := call("fs_delete", "tc-1", `{"path":"/tmp/x"}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", c, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConsumeApproval(ctxT(), c); err != nil {
		t.Fatal(err)
	}
	j.Close()
	// Child runs RetryChallenge and dies mid-batch.
	cmd := exec.Command(os.Args[0], "-test.run", "TestRetryRecipeAtomicUnderSigkill")
	cmd.Env = append(os.Environ(), "APPR_RETRY_CHILD=1", "APPR_RETRY_DIR="+dir,
		"APPR_RETRY_ID="+ch.ChallengeID, "NEXUS_TEST_KILL_MID_BATCH=1")
	if err := cmd.Run(); err == nil {
		t.Fatal("child survived the mid-batch kill seam")
	}
	// Reopen: the batch must be all-or-nothing.
	s2, _ := open(t, dir, clock, "work")
	openOld, err := s2.ConsumedUnfinished(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s2.Pending(ctxT())
	if err != nil {
		t.Fatal(err)
	}
	if len(openOld) == 1 && len(pending) == 1 {
		t.Fatalf("retry recipe torn: old reconcile item open AND a new challenge live (two authorizations)")
	}
	if len(openOld) == 0 && len(pending) == 0 {
		t.Fatal("retry recipe lost BOTH sides")
	}
	// Recovery: a clean retry now yields exactly one live challenge and
	// closes the old item exactly once.
	if len(openOld) == 1 {
		if _, err := s2.RetryChallenge(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
			t.Fatal(err)
		}
	}
	openOld, _ = s2.ConsumedUnfinished(ctxT())
	pending, _ = s2.Pending(ctxT())
	if len(openOld) != 0 || len(pending) != 1 {
		t.Fatalf("post-recovery state: open=%d pending=%d (want 0/1)", len(openOld), len(pending))
	}
	// The closed item can never be retried again.
	if _, err := s2.RetryChallenge(ctxT(), ch.ChallengeID, "tg:chat-42"); err == nil {
		t.Fatal("closed reconcile item retried twice")
	}
}

func retryCrashChild() {
	dir := os.Getenv("APPR_RETRY_DIR")
	j, err := journal.Open(filepath.Join(dir, "work.db"), "work", redact.None{}, Events(), NewProjection())
	if err != nil {
		os.Exit(1)
	}
	s, err := NewStore(j, clockid.NewFake(time.Now()))
	if err != nil {
		os.Exit(1)
	}
	s.RetryChallenge(context.Background(), os.Getenv("APPR_RETRY_ID"), "tg:chat-42") // dies mid-batch
	os.Exit(0)
}

// LEGACY v1 suspension rows (no persisted context) degrade to an
// observation-only resume instead of stranding an approvable challenge
// (Phase-5-r4 codex #2); corrupt context stays fail-closed.
func TestLegacyContextDegradesNotStrands(t *testing.T) {
	blocks, err := parseContextJSON("")
	if err != nil || blocks != nil {
		t.Fatalf("legacy empty context must degrade to nil blocks: %v %v", blocks, err)
	}
	if _, err := parseContextJSON("{corrupt"); err == nil {
		t.Fatal("corrupt context accepted")
	}
	good, err := parseContextJSON("[]")
	if err != nil || len(good) != 0 {
		t.Fatalf("empty slice context: %v %v", good, err)
	}
}

// CANONICAL argument hashing (Phase-6 exec-spine find): the journal
// redactor rebuilds payloads through Go maps, reordering JSON keys — an
// approval must survive that byte-order change (key order carries no C4
// meaning), while any VALUE change still invalidates it.
func TestApprovalSurvivesKeyReorder(t *testing.T) {
	clock := clockid.NewFake(time.Now())
	s, _ := open(t, t.TempDir(), clock, "work")
	minted := call("exec", "tc-1", `{"command":"/bin/ls","args":["/"]}`)
	ch, err := s.Suspend(ctxT(), "turn-1", "run-1", minted, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	// The SAME intent with reordered keys (what a journal round trip
	// produces) must consume.
	reordered := call("exec", "tc-1", `{"args":["/"],"command":"/bin/ls"}`)
	if err := s.ConsumeApproval(ctxT(), reordered); err != nil {
		t.Fatalf("key-reordered identical intent refused: %v", err)
	}
	// A VALUE change is a different effect: never consumable.
	minted2 := call("exec", "tc-2", `{"command":"/bin/ls","args":["/"]}`)
	ch2, err := s.Suspend(ctxT(), "turn-2", "run-2", minted2, "tg:chat-42", testBlocks(t, "original request"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxT(), ch2.ChallengeID, "tg:chat-42"); err != nil {
		t.Fatal(err)
	}
	tampered := call("exec", "tc-2", `{"command":"/bin/rm","args":["/"]}`)
	if err := s.ConsumeApproval(ctxT(), tampered); err == nil {
		t.Fatal("value-changed effect consumed under the old approval")
	}
}

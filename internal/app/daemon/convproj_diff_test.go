//go:build linux

// tgout convproj DIFFERENTIAL oracle: the projection's history +
// recovery answers must be IDENTICAL to the retained reference replay
// on every prefix of randomized lifecycle sequences. A precedence bug
// in the fold cannot be mirrored in the independent reference, so
// false-green is structurally excluded (plan v2/v4).
package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/approval"
	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/conv"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/security/redact"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func openSQL(path string) (*sql.DB, error) { return sql.Open("sqlite", "file:"+path) }

func diffJournal(t *testing.T) (*Daemon, *journal.Journal) {
	t.Helper()
	dir := t.TempDir()
	ev := map[string]journal.PayloadValidator{}
	for _, n := range machine.EventTypes() {
		ev[n] = nil
	}
	for n, v := range approval.Events() {
		ev[n] = v
	}
	for n, v := range channel.Events() {
		ev[n] = v
	}
	j, err := journal.Open(filepath.Join(dir, "j.db"), "work", redact.None{}, ev, conv.NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	d := &Daemon{deps: Deps{Journal: j, Profile: "work"}}
	return d, j
}

func appendEv(t *testing.T, j *journal.Journal, id, typ, run string, turn *contracts.TurnID, payload string) {
	t.Helper()
	if _, err := j.Append(context.Background(), contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID: contracts.EventID(id), EventType: typ, RunID: contracts.RunID(run),
		TurnID: turn, EmittedAt: time.Now().UTC(), ActorType: contracts.ActorSystem,
		ActorID: "t", PrincipalID: "nexus", WorkspaceID: "local", ProfileID: "work",
		AttemptNo: 1, Payload: []byte(payload), PayloadHash: "recomputed",
	}); err != nil {
		t.Fatal(err)
	}
}

func blocksEqual(a, b []contracts.ContextBlock) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ac, bc := "", ""
		if a[i].Content != nil {
			ac = *a[i].Content
		}
		if b[i].Content != nil {
			bc = *b[i].Content
		}
		if a[i].Kind != b[i].Kind || ac != bc {
			return false
		}
	}
	return true
}

func TestConvProjectionMatchesReferenceOnEveryPrefix(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	kinds := []string{"admit", "succeed", "empty-succeed", "suspend", "resume", "fail", "ordinary-fail"}
	for seq := 0; seq < 200; seq++ {
		d, j := diffJournal(t)
		// A small pool of identities and update ids for interleaving.
		idents := []string{"chat-1", "chat-2", "chat-11"} // chat-1 is a prefix of chat-11
		evn := 0
		steps := 4 + rng.Intn(10)
		for s := 0; s < steps; s++ {
			ident := idents[rng.Intn(len(idents))]
			uid := 1 + rng.Intn(3)
			turnStr := fmt.Sprintf("turn-chan-%s-%d", ident, uid)
			turn := contracts.TurnID(turnStr)
			run := "run-" + turnStr
			evn++
			id := fmt.Sprintf("ev-%d-%d", seq, evn)
			switch kinds[rng.Intn(len(kinds))] {
			case "admit":
				appendEv(t, j, id, "channel.inbound_admitted", run, nil,
					fmt.Sprintf(`{"message_id":%q,"adapter_id":"telegram","channel_identity":%q,"update_id":%d,"text":%q}`, id, ident, uid, "u-"+id))
			case "succeed":
				appendEv(t, j, id, "turn.succeeded", run, &turn, fmt.Sprintf(`{"final":%q}`, "f-"+id))
			case "empty-succeed":
				appendEv(t, j, id, "turn.succeeded", run, &turn, `{"final":""}`)
			case "suspend":
				appendEv(t, j, id, "approval.turn_suspended", run, &turn,
					fmt.Sprintf(`{"turn_id":%q,"summary":%q,"expires_unix":9999999999,"call":{},"context":[],"challenge_id":"ch","run_id":%q,"effect_hash":"h","expected_source":"tg:x"}`, turnStr, "S-"+id, run))
			case "resume":
				appendEv(t, j, id, "turn.resumed", run, &turn, fmt.Sprintf(`{"turn_id":%q}`, turnStr))
			case "fail":
				appendEv(t, j, id, "turn.failed", run, &turn, fmt.Sprintf(`{"turn_id":%q,"error_code":"TOOL_SCHEMA_DRIFT","tool":"memory_recall"}`, turnStr))
			case "ordinary-fail":
				appendEv(t, j, id, "turn.failed", run, &turn, fmt.Sprintf(`{"turn_id":%q}`, turnStr))
			}
			// PREFIX CHECK: after every event, projection == reference
			// for history (all identities) and recovery (all turns).
			for _, qi := range idents {
				for _, qu := range []int{1, 2, 3} {
					cur := contracts.TurnID(fmt.Sprintf("turn-chan-%s-%d", qi, qu))
					pj, err := d.conversationHistory(qi, cur)
					if err != nil {
						t.Fatal(err)
					}
					ref, err := d.referenceConversationHistory(qi, cur)
					if err != nil {
						t.Fatal(err)
					}
					if !blocksEqual(pj, ref) {
						t.Fatalf("seq %d step %d: history diverged for %s cur=%s\nproj=%v\nref=%v", seq, s, qi, cur, pj, ref)
					}
					pr, pok, _ := d.recoveredTurnOutcome(cur)
					rr, rok, _ := d.referenceRecoveredTurnOutcome(cur)
					if pok != rok || pr.Kind != rr.Kind || pr.Final != rr.Final || pr.Code != rr.Code || pr.Tool != rr.Tool {
						t.Fatalf("seq %d step %d: recovery diverged for %s\nproj=%+v(%v)\nref=%+v(%v)", seq, s, cur, pr, pok, rr, rok)
					}
				}
			}
		}
	}
}

// LATENCY (relative, host-independent): the projection history path must
// be far faster than the retained reference replay on a large corpus
// incl. an incomplete tail (plan latency detector).
func TestConvProjectionFasterThanReference(t *testing.T) {
	if testing.Short() {
		t.Skip("latency")
	}
	d, j := diffJournal(t)
	// 1500 completed + 1500 incomplete-tail turns for chat-1.
	for i := 0; i < 1500; i++ {
		id := fmt.Sprintf("c%d", i)
		appendEv(t, j, "a"+id, "channel.inbound_admitted", "r"+id, nil,
			fmt.Sprintf(`{"message_id":%q,"adapter_id":"telegram","channel_identity":"chat-1","update_id":%d,"text":"u"}`, "a"+id, i+1))
		turn := contracts.TurnID(fmt.Sprintf("turn-chan-chat-1-%d", i+1))
		appendEv(t, j, "s"+id, "turn.succeeded", "r"+id, &turn, `{"final":"ok"}`)
	}
	for i := 1500; i < 3000; i++ { // incomplete tail (admitted, never succeeded)
		id := fmt.Sprintf("c%d", i)
		appendEv(t, j, "a"+id, "channel.inbound_admitted", "r"+id, nil,
			fmt.Sprintf(`{"message_id":%q,"adapter_id":"telegram","channel_identity":"chat-1","update_id":%d,"text":"u"}`, "a"+id, i+1))
	}
	cur := contracts.TurnID("turn-chan-chat-1-99999")
	tp := time.Now()
	for r := 0; r < 20; r++ {
		if _, err := d.conversationHistory("chat-1", cur); err != nil {
			t.Fatal(err)
		}
	}
	projDur := time.Since(tp)
	tr := time.Now()
	for r := 0; r < 20; r++ {
		if _, err := d.referenceConversationHistory("chat-1", cur); err != nil {
			t.Fatal(err)
		}
	}
	refDur := time.Since(tr)
	if projDur*10 > refDur {
		t.Fatalf("projection not >=10x faster: proj=%v ref=%v", projDur, refDur)
	}
	t.Logf("proj=%v ref=%v (%.0fx)", projDur, refDur, float64(refDur)/float64(projDur))
}

// REBUILD: a database folded at an OLD projection version refolds and
// the differential oracle passes on the rebuilt state.
func TestConvProjectionRebuild(t *testing.T) {
	dir := t.TempDir()
	dbp := filepath.Join(dir, "j.db")
	ev := map[string]journal.PayloadValidator{}
	for _, n := range machine.EventTypes() {
		ev[n] = nil
	}
	for n, v := range channel.Events() {
		ev[n] = v
	}
	open := func() *journal.Journal {
		j, err := journal.Open(dbp, "work", redact.None{}, ev, conv.NewProjection())
		if err != nil {
			t.Fatal(err)
		}
		return j
	}
	j := open()
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("c%d", i)
		appendEv(t, j, "a"+id, "channel.inbound_admitted", "r"+id, nil,
			fmt.Sprintf(`{"message_id":%q,"adapter_id":"telegram","channel_identity":"chat-1","update_id":%d,"text":%q}`, "a"+id, i, "u"+id))
		turn := contracts.TurnID(fmt.Sprintf("turn-chan-chat-1-%d", i))
		appendEv(t, j, "s"+id, "turn.succeeded", "r"+id, &turn, fmt.Sprintf(`{"final":%q}`, "f"+id))
	}
	d0 := &Daemon{deps: Deps{Journal: j, Profile: "work"}}
	before, _ := d0.conversationHistory("chat-1", "turn-chan-chat-1-99")
	ref0, _ := d0.referenceConversationHistory("chat-1", "turn-chan-chat-1-99")
	if len(before) == 0 {
		t.Fatal("rebuild fixture vacuous: empty pre-rebuild history")
	}
	if !blocksEqual(before, ref0) {
		t.Fatalf("projection != reference before rebuild:\nproj=%v\nref=%v", before, ref0)
	}
	j.Close()
	// Regress the projection version so reopen triggers a full refold.
	db, err := openSQL(dbp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE proj_sync_offsets SET version=0 WHERE name='conv'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	j2 := open()
	d1 := &Daemon{deps: Deps{Journal: j2, Profile: "work"}}
	after, err := d1.conversationHistory("chat-1", "turn-chan-chat-1-99")
	if err != nil {
		t.Fatal(err)
	}
	if !blocksEqual(before, after) {
		t.Fatalf("rebuild changed history:\nbefore=%v\nafter=%v", before, after)
	}
	refAfter, _ := d1.referenceConversationHistory("chat-1", "turn-chan-chat-1-99")
	if !blocksEqual(after, refAfter) {
		t.Fatalf("rebuilt projection != reference:\nproj=%v\nref=%v", after, refAfter)
	}
	_ = context.Background
}

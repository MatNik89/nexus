//go:build linux

// T17 RED table (tasks-P0): daemon owns the DB behind a UDS; scripted e2e
// conversation with a DETERMINISTIC fake provider through the journal;
// two concurrent clients → zero surfaced SQLITE_BUSY; heartbeat stop
// detectable within the interval; --yolo session mode (HARDQ F2) reaches
// the PEP per session, never from a message payload. Anchored to PRD §6
// item 1 + HARDQ B7/C8/F2.
package daemon

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/app/repl"
	"github.com/MatNik89/nexus/internal/approval"
	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/loop"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/llm/planner"
	"github.com/MatNik89/nexus/internal/llm/provider"
	"github.com/MatNik89/nexus/internal/security/redact"
)

// deterministic echo planner: final = "echo: <last user block content>".
type echoPlanner struct{ callTool bool }

func (p *echoPlanner) Plan(ctx context.Context, blocks []contracts.ContextBlock) (loop.Action, error) {
	if p.callTool {
		p.callTool = false
		c, err := contracts.NewToolCall(contracts.ToolCallParams{
			ToolCallID: "tc-ask", ToolID: "asker",
			Arguments: json.RawMessage(`{}`), ArgsSchemaHash: "h",
			Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess,
			Deadline: time.Now().Add(time.Minute), AttemptNo: 1, ProfileID: "work",
		})
		if err != nil {
			return loop.Action{}, err
		}
		return loop.Action{Call: &c}, nil
	}
	last := blocks[len(blocks)-1]
	content := ""
	if last.Content != nil {
		content = *last.Content
	}
	final := "echo: " + content
	return loop.Action{Final: &final}, nil
}

type capturingAudit struct {
	mu     sync.Mutex
	events []string
}

func (a *capturingAudit) Record(event string, call contracts.ToolCall) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

func testDaemon(t *testing.T, planner loop.Planner, audit effectpath.AuditSink) (*Daemon, string, *journal.Journal) {
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
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, ev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	if audit == nil {
		audit = &capturingAudit{}
	}
	d, err := New(Deps{
		Journal:        j,
		PlannerFactory: func(deliver func(string) error) (loop.Planner, error) { return planner, nil },
		Authority:      s7min.NewAuthority(nil, time.Minute),
		Profile:        "work",
		Rules:          map[contracts.ToolID]effectpath.Decision{"asker": effectpath.DecisionAsk},
		Tools: map[contracts.ToolID]effectpath.InProcFunc{
			"asker": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
				return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
					Status: contracts.ResultSucceeded, StartedAt: time.Unix(1, 0), FinishedAt: time.Unix(2, 0)}, nil
			},
		},
		Audit: audit, Redactor: redact.None{},
	})
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "daemon.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan error, 1)
	go func() { ready <- d.Serve(ctx, sock) }()
	for i := 0; i < 100; i++ {
		if _, err := net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return d, sock, j
}

type client struct {
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, sock string, yolo bool) *client {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &client{conn: conn, r: bufio.NewReader(conn)}
	c.send(t, map[string]any{"type": "hello", "yolo": yolo})
	return c
}

func (c *client) send(t *testing.T, frame map[string]any) {
	t.Helper()
	b, _ := json.Marshal(frame)
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

// chat sends one message and gathers frames until final/error.
func (c *client) chat(t *testing.T, text string) (string, string) {
	t.Helper()
	c.send(t, map[string]any{"type": "chat", "text": text})
	var final, errText string
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("connection broke: %v", err)
		}
		var f struct{ Type, Text string }
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("malformed frame %q: %v", line, err)
		}
		switch f.Type {
		case "final":
			return f.Text, errText
		case "error":
			return final, f.Text
		}
	}
}

// The scripted e2e oracle: deterministic input → expected rendered output,
// and the turn exists in the journal (fold → SUCCEEDED).
func TestScriptedConversationThroughJournal(t *testing.T) {
	_, sock, j := testDaemon(t, &echoPlanner{}, nil)
	c := dial(t, sock, false)
	out, errText := c.chat(t, "hello nexus")
	if errText != "" || out != "echo: hello nexus" {
		t.Fatalf("oracle broken: %q err=%q", out, errText)
	}
	var turnEvents []machine.FoldEvent
	if err := j.Replay(0, func(e journal.Event) error {
		if strings.HasPrefix(e.Envelope.EventType, "turn.") {
			turnEvents = append(turnEvents, machine.FoldEvent{Type: e.Envelope.EventType, Offset: e.JournalOffset})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, _, err := machine.TurnTable().Fold(contracts.TurnInvalid, turnEvents, nil)
	if err != nil || st != contracts.TurnSucceeded {
		t.Fatalf("conversation not in the journal: %v (%v)", st, err)
	}
}

// Two concurrent clients: every message answered, ZERO surfaced
// SQLITE_BUSY (single DB owner behind the UDS — HARDQ B7).
func TestTwoConcurrentClientsNoSqliteBusy(t *testing.T) {
	_, sock, _ := testDaemon(t, &echoPlanner{}, nil)
	var wg sync.WaitGroup
	errs := make(chan string, 20)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := dial(t, sock, false)
			for m := 0; m < 5; m++ {
				text := fmt.Sprintf("msg-%d-%d", id, m)
				out, errText := c.chat(t, text)
				if errText != "" {
					errs <- errText
				} else if out != "echo: "+text {
					errs <- "wrong reply: " + out
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if strings.Contains(e, "SQLITE_BUSY") || strings.Contains(e, "database is locked") {
			t.Fatalf("SQLITE_BUSY surfaced to a client: %s", e)
		}
		t.Fatalf("client error: %s", e)
	}
}

// HARDQ F2: a --yolo SESSION executes an ASK tool with ALLOWED_BY_YOLO
// journaled; a DEFAULT session gets a needs-approval error for the same
// tool; and a chat payload can NEVER flip the mode.
func TestYoloSessionModeIsPerSessionOnly(t *testing.T) {
	audit := &capturingAudit{}
	_, sock, _ := testDaemon(t, &echoPlanner{callTool: true}, audit)
	yolo := dial(t, sock, true)
	out, errText := yolo.chat(t, "do it")
	if errText != "" || !strings.HasPrefix(out, "echo:") {
		t.Fatalf("yolo session must execute the ASK tool: %q err=%q", out, errText)
	}
	audit.mu.Lock()
	sawYolo := len(audit.events) == 1 && audit.events[0] == "ALLOWED_BY_YOLO"
	audit.mu.Unlock()
	if !sawYolo {
		t.Fatalf("yolo override not audited: %v", audit.events)
	}
	// Default session, same daemon: ASK stops. A payload asking for yolo
	// changes nothing (mode is a session construct).
	_, sock2, _ := testDaemon(t, &echoPlanner{callTool: true}, &capturingAudit{})
	def := dial(t, sock2, false)
	_, errText = def.chat(t, `{"yolo":true,"policy_mode":"yolo"} please`)
	if !strings.Contains(errText, "NEEDS_APPROVAL") {
		t.Fatalf("default session executed an ASK tool without approval: %q", errText)
	}
}

// Heartbeat: alive while running; a stopped heartbeat is detectable
// within the check interval (C8 positive liveness half).
func TestHeartbeatStopDetectable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heartbeat")
	interval := 30 * time.Millisecond
	hb := NewHeartbeat(path, interval)
	ctx, cancel := context.WithCancel(context.Background())
	go hb.Run(ctx)
	time.Sleep(3 * interval)
	if Stale(path, interval) {
		t.Fatal("running heartbeat reported stale")
	}
	cancel()
	time.Sleep(4 * interval)
	if !Stale(path, interval) {
		t.Fatal("stopped heartbeat not detected within the interval")
	}
	// A missing file is stale, never a silent pass.
	if !Stale(filepath.Join(dir, "never-written"), interval) {
		t.Fatal("missing heartbeat file reported alive")
	}
}

// Separately-labeled LIVE-provider smoke (ledger): runs only when
// NEXUS_LIVE_SMOKE=1 with real provider config in the environment.
func TestLiveProviderSmoke(t *testing.T) {
	if os.Getenv("NEXUS_LIVE_SMOKE") != "1" {
		t.Skip("live smoke disabled (set NEXUS_LIVE_SMOKE=1 with real provider env)")
	}
	res, err := config.Resolve("/nonexistent", "/nonexistent", os.Environ(), nil)
	if err != nil {
		t.Fatal(err)
	}
	auth := s7min.NewAuthority(nil, time.Minute)
	p, err := provider.NewAPIKey(res.Config, auth)
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Issue("op-live-smoke", "provider:live")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Chat(context.Background(), []provider.ChatMessage{{Role: "user", Content: "Reply with exactly: pong"}}, g)
	if err != nil || out.Content == "" {
		t.Fatalf("live provider smoke failed: %q %v", out.Content, err)
	}
}

// FULL-SPINE deterministic e2e (Phase-2 codex #12): real repl.Run → UDS →
// daemon → loop → REAL planner → REAL provider transport (deterministic
// local SSE fake) → journal. Streaming deltas reach the terminal (T17
// streaming print), the reply renders, and the turn folds SUCCEEDED from
// the journal alone.
func TestFullSpineDeterministicTransport(t *testing.T) {
	// Deterministic OpenAI-compatible SSE endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"deterministic \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("NEXUS_SPINE_KEY", "sk-spine")
	cfg := config.Config{
		ProviderBaseURL: srv.URL, ProviderKeyEnv: "NEXUS_SPINE_KEY",
		ProviderModel: "spine-model", EgressAllow: []string{host}, DefaultProfile: "work",
	}
	dir := t.TempDir()
	ev := map[string]journal.PayloadValidator{}
	for _, n := range machine.EventTypes() {
		ev[n] = nil
	}
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, ev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	authority := s7min.NewAuthority(nil, time.Minute)
	prov, err := provider.NewAPIKey(cfg, authority)
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(Deps{
		Journal: j,
		PlannerFactory: func(deliver func(string) error) (loop.Planner, error) {
			return planner.NewStreaming(prov, prov, authority, prov.Target(), deliver)
		},
		Authority: authority, Profile: "work",
		Rules: map[contracts.ToolID]effectpath.Decision{},
		Tools: map[contracts.ToolID]effectpath.InProcFunc{},
		Audit: &capturingAudit{}, Redactor: redact.None{},
	})
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "spine.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Serve(ctx, sock)
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The REAL terminal client drives the whole spine.
	var out strings.Builder
	if err := repl.Run(strings.NewReader("hello nexus\n"), &out, sock, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "deterministic pong") {
		t.Fatalf("full-spine reply not rendered: %q", got)
	}
	var turnEvents []machine.FoldEvent
	if err := j.Replay(0, func(e journal.Event) error {
		if strings.HasPrefix(e.Envelope.EventType, "turn.") {
			turnEvents = append(turnEvents, machine.FoldEvent{Type: e.Envelope.EventType, Offset: e.JournalOffset})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, _, err := machine.TurnTable().Fold(contracts.TurnInvalid, turnEvents, nil)
	if err != nil || st != contracts.TurnSucceeded {
		t.Fatalf("full-spine turn folds to %v (%v)", st, err)
	}
}

// blockCapturingPlanner records the context blocks it is planned with.
type blockCapturingPlanner struct {
	mu     sync.Mutex
	blocks [][]contracts.ContextBlock
}

func (p *blockCapturingPlanner) Plan(ctx context.Context, blocks []contracts.ContextBlock) (loop.Action, error) {
	p.mu.Lock()
	cp := append([]contracts.ContextBlock{}, blocks...)
	p.blocks = append(p.blocks, cp)
	p.mu.Unlock()
	final := "ok"
	return loop.Action{Final: &final}, nil
}

// HONEST channel provenance detector (Phase-5-r2 codex #9: the round-1
// fix had no red-capable test): a channel turn's user block must name the
// REAL source — restoring REPL provenance turns this RED.
func TestChannelTurnCarriesChannelProvenance(t *testing.T) {
	p := &blockCapturingPlanner{}
	d, _, _ := testDaemon(t, p, nil)
	if _, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "hello"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.blocks) == 0 || len(p.blocks[0]) == 0 {
		t.Fatal("planner saw no blocks")
	}
	b := p.blocks[0][0]
	if b.SourceURI != "nexus://telegram/chat-42" || b.Producer != "telegram" {
		t.Fatalf("channel input masquerades as %q/%q (want nexus://telegram/chat-42 / telegram)", b.SourceURI, b.Producer)
	}
}

// DETERMINISTIC channel turn ids (Phase-5-r2 codex #1): the SAME
// redelivered update re-enters the SAME turn — the journal's unique event
// ids refuse a second run instead of duplicating effects under fresh
// identities; a DIFFERENT update still runs.
func TestRedeliveredUpdateCannotRerunCompletedTurn(t *testing.T) {
	p := &blockCapturingPlanner{}
	d, _, _ := testDaemon(t, p, nil)
	if _, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "hello"); err != nil {
		t.Fatal(err)
	}
	// Redelivery of update 7: the completed turn must NOT run again —
	// and the DURABLE original final is recovered instead of an error
	// (Phase-5-r3 codex #3: a crash between turn completion and channel
	// delivery must not turn a success into a false failure).
	recovered, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "hello")
	if err != nil {
		t.Fatalf("redelivery of a completed turn errored instead of recovering the final: %v", err)
	}
	if recovered != "ok" {
		t.Fatalf("recovered final %q, want the original %q", recovered, "ok")
	}
	p.mu.Lock()
	runs := len(p.blocks)
	p.mu.Unlock()
	if runs != 1 {
		t.Fatalf("completed turn planned twice: %d", runs)
	}
	// A different update id is a fresh turn.
	if _, err := d.RunChannelTurn(context.Background(), "chat-42", 8, "next"); err != nil {
		t.Fatal(err)
	}
}

// Completed-turn recovery FAILS CLOSED on a corrupt journal (Phase-5-r4
// codex #3): a final observed during a replay whose integrity chain later
// breaks is never served as a recovered outcome.
func TestRecoveryFailsClosedOnCorruptJournal(t *testing.T) {
	p := &blockCapturingPlanner{}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "journal.db")
	ev := map[string]journal.PayloadValidator{}
	for _, n := range machine.EventTypes() {
		ev[n] = nil
	}
	j, err := journal.Open(dbPath, "work", redact.None{}, ev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	d, err := New(Deps{
		Journal:        j,
		PlannerFactory: func(deliver func(string) error) (loop.Planner, error) { return p, nil },
		Authority:      s7min.NewAuthority(nil, time.Minute),
		Profile:        "work",
		Rules:          map[contracts.ToolID]effectpath.Decision{},
		Tools:          map[contracts.ToolID]effectpath.InProcFunc{},
		Audit:          &capturingAudit{}, Redactor: redact.None{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "hello"); err != nil {
		t.Fatal(err)
	}
	// Append one more event AFTER the completed turn, then corrupt it —
	// the chain now breaks after the final was observed.
	if _, err := j.Append(context.Background(), contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID: "ev-tail-1", EventType: machine.EvRunCreated, RunID: "run-tail",
		EmittedAt: time.Now().UTC(), ActorType: contracts.ActorSystem, ActorID: "test",
		PrincipalID: "nexus", WorkspaceID: "local", ProfileID: "work", AttemptNo: 1,
		Payload: []byte(`{"x":1}`), PayloadHash: "recomputed",
	}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE events SET integrity_hash='deadbeef' WHERE event_id='ev-tail-1'`); err != nil {
		t.Fatal(err)
	}
	// Redelivery: recovery must refuse — AND the decisive integrity
	// failure must surface, not the duplicate-event symptom (Phase-5-r5
	// codex #1).
	_, rerr := d.RunChannelTurn(context.Background(), "chat-42", 7, "hello")
	if rerr == nil {
		t.Fatal("recovered a final from a journal that fails integrity verification")
	}
	if !strings.Contains(rerr.Error(), "chain broken") {
		t.Fatalf("replay integrity error was not propagated: %v", rerr)
	}
}

// SUSPENDED-turn redelivery recovers the CHALLENGE SUMMARY (phase5-r4
// kilo LOW): a crash between the suspension and the channel terminal
// must replay the challenge, not a generic failure; a resumed turn's
// later success beats an earlier suspension (prep-low codex).
func TestRedeliveredSuspendedTurnRecoversChallenge(t *testing.T) {
	p := &blockCapturingPlanner{}
	d, _, j := testDaemon(t, p, nil)
	store, err := approval.NewStore(j, clockid.NewFake(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	suspend := func(id, turn, run string) approval.Challenge {
		t.Helper()
		idem := "ik-" + id
		c, err := contracts.NewToolCall(contracts.ToolCallParams{
			ToolCallID: contracts.ToolCallID(id), ToolID: "rm_file",
			Arguments: json.RawMessage(`{"path":"/tmp/x"}`), ArgsSchemaHash: "h1",
			Effect: contracts.EffectIrreversible, ExecutionKind: contracts.ExecInProcess,
			Deadline: time.Now().Add(time.Hour), AttemptNo: 1,
			IdempotencyKey: &idem, ProfileID: "work",
		})
		if err != nil {
			t.Fatal(err)
		}
		text := "do it"
		blk, err := contracts.NewContextBlock(contracts.ContextBlockParams{
			BlockID: contracts.BlockID("blk-" + id), Kind: "user_message", Content: &text,
			ContentHash: "0000000000000000000000000000000000000000000000000000000000000000",
			SourceURI:   "nexus://telegram/chat-42", Producer: "telegram",
			Trust: contracts.TrustUser, Sensitivity: contracts.Sensitivity(1),
			Lineage: []string{}, ObservedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		ch, err := store.Suspend(context.Background(), contracts.TurnID(turn),
			contracts.RunID(run), c, "tg:chat-42", []contracts.ContextBlock{blk})
		if err != nil {
			t.Fatal(err)
		}
		return ch
	}
	// Turn 7: suspension FIRST, then the turn actually succeeds — the
	// later turn.succeeded must win over the earlier suspension.
	suspend("tc-7", "turn-chan-chat-42-7", "run-chan-chat-42-7")
	out7, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "do it")
	if err != nil {
		t.Fatal(err)
	}
	redo7, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "do it")
	if err != nil {
		t.Fatalf("redelivery of a succeeded turn errored: %v", err)
	}
	if redo7 != out7 || strings.Contains(redo7, "APPROVAL NEEDED") {
		t.Fatalf("redelivery recovered %q, want the later success %q to beat the earlier suspension", redo7, out7)
	}
	// Turn 8: suspended-only (the crash left it mid-turn) — redelivery
	// must replay the challenge, not a generic failure.
	ch8 := suspend("tc-8", "turn-chan-chat-42-8", "run-chan-chat-42-8")
	if _, err := j.Append(context.Background(), contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID: "ev-turn-chan-chat-42-8-turn.created-1", EventType: "turn.created",
		RunID: "run-chan-chat-42-8", EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "loop", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: "work", AttemptNo: 1,
		Payload: []byte(`{"turn_id":"turn-chan-chat-42-8"}`), PayloadHash: "recomputed",
	}); err != nil {
		t.Fatal(err)
	}
	out8, err := d.RunChannelTurn(context.Background(), "chat-42", 8, "do it again")
	if err != nil {
		t.Fatalf("suspended-turn redelivery errored instead of recovering the challenge: %v", err)
	}
	if out8 != ch8.Summary {
		t.Fatalf("recovered %q, want the challenge summary %q", out8, ch8.Summary)
	}
}

// DOGFOOD 2026-09-07 (first live conversation): every turn claimed "no
// prior context" — RunChannelTurn passed ONLY the current message. A
// channel turn must carry the recent conversation history of ITS
// identity (and never another identity's).
func TestChannelTurnCarriesConversationHistory(t *testing.T) {
	p := &blockCapturingPlanner{}
	d, _, j := testDaemon(t, p, nil)
	core, err := channel.New(j)
	if err != nil {
		t.Fatal(err)
	}
	// Mirror the production path: ADMIT (the durable user text) before
	// running the turn — history reads the admission records.
	send := func(identity string, uid int64, text string) {
		t.Helper()
		if _, err := core.Admit(context.Background(), channel.Inbound{
			AdapterID: "telegram", ChannelIdentity: identity, UpdateID: uid,
			Text: text, Profile: "work"}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.RunChannelTurn(context.Background(), identity, uid, text); err != nil {
			t.Fatal(err)
		}
	}
	send("chat-42", 1, "my wifi password is banana42")
	send("chat-42", 2, "what did I just tell you?")
	// A DIFFERENT identity must not inherit chat-42's history (B3).
	send("chat-99", 3, "hello")
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.blocks) != 3 {
		t.Fatalf("want 3 planner calls, got %d", len(p.blocks))
	}
	flat := func(bs []contracts.ContextBlock) string {
		var sb strings.Builder
		for _, b := range bs {
			if b.Content != nil {
				sb.WriteString(*b.Content)
				sb.WriteString("\n")
			}
		}
		return sb.String()
	}
	second := flat(p.blocks[1])
	if !strings.Contains(second, "banana42") {
		t.Fatalf("turn 2 does not see turn 1's user message:\n%s", second)
	}
	if !strings.Contains(second, "ok") {
		t.Fatalf("turn 2 does not see the assistant's prior reply:\n%s", second)
	}
	third := flat(p.blocks[2])
	if strings.Contains(third, "banana42") {
		t.Fatalf("chat-99 inherited chat-42's history (cross-identity leak):\n%s", third)
	}
}

// DOGFOOD 2026-09-07: the interactive chat session must also carry its
// own conversation history — message 2 sees message 1 and its reply.
func TestChatSessionCarriesHistory(t *testing.T) {
	p := &blockCapturingPlanner{}
	_, sock, _ := testDaemon(t, p, nil)
	c := dial(t, sock, false)
	if out, errText := c.chat(t, "my locker code is 7714"); errText != "" || out == "" {
		t.Fatalf("first message failed: %q err=%q", out, errText)
	}
	if out, errText := c.chat(t, "what code did I mention?"); errText != "" || out == "" {
		t.Fatalf("second message failed: %q err=%q", out, errText)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.blocks) != 2 {
		t.Fatalf("want 2 planner calls, got %d", len(p.blocks))
	}
	joined := ""
	for _, b := range p.blocks[1] {
		if b.Content != nil {
			joined += *b.Content + "\n"
		}
	}
	if !strings.Contains(joined, "7714") || !strings.Contains(joined, "ok") {
		t.Fatalf("second turn does not carry the session history:\n%s", joined)
	}
}

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
	// Redelivery of update 7: the completed turn must NOT run again.
	if _, err := d.RunChannelTurn(context.Background(), "chat-42", 7, "hello"); err == nil {
		t.Fatal("redelivered update re-ran a completed turn")
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

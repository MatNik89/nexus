//go:build linux

// T23 RED table (tasks-P0): Telegram built-in adapter on the T22 core —
// long-poll getUpdates against a FAKE Bot API; per-chat profile binding
// DENY-DEFAULT (unbound chat → typed "which profile?" refusal, nothing
// admitted); non-text update → typed fail-closed reply, loop alive (C2);
// replayed update_id returns the existing outcome (no double effect);
// live channel probe bound to the resolved config. HARDQ A1/C2/B3.
package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/foundation/config"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

// fakeBot is a scripted Bot API: serves getUpdates batches and records
// sendMessage calls.
type fakeBot struct {
	mu      sync.Mutex
	batches [][]map[string]any
	sent    []string
	sentTo  []int64
	offsets []int64
	// tgout detectors: parse_mode per send, and a scripted parse-400
	// rejection for the first N sends carrying parse_mode.
	parseModes     []string
	rejectHTMLLeft int
	lastMethod     string
	lastRich       string
	sawTyping      bool
	sawSetCommands bool
	lastCommands   string
}

func (f *fakeBot) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			var req struct {
				Offset int64 `json:"offset"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			f.offsets = append(f.offsets, req.Offset)
			var batch []map[string]any
			if len(f.batches) > 0 {
				batch = f.batches[0]
				f.batches = f.batches[1:]
			}
			out, _ := json.Marshal(map[string]any{"ok": true, "result": batch})
			w.Write(out)
		case strings.HasSuffix(r.URL.Path, "/sendRichMessage"):
			var req struct {
				ChatID      int64 `json:"chat_id"`
				RichMessage struct {
					Markdown string `json:"markdown"`
				} `json:"rich_message"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			f.lastMethod = "sendRichMessage"
			f.lastRich = req.RichMessage.Markdown
			f.sentTo = append(f.sentTo, req.ChatID)
			w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var req struct {
				ChatID    int64  `json:"chat_id"`
				Text      string `json:"text"`
				ParseMode string `json:"parse_mode"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.ParseMode != "" && f.rejectHTMLLeft > 0 {
				f.rejectHTMLLeft--
				w.WriteHeader(400)
				w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
				return
			}
			f.lastMethod = "sendMessage"
			f.sent = append(f.sent, req.Text)
			f.sentTo = append(f.sentTo, req.ChatID)
			f.parseModes = append(f.parseModes, req.ParseMode)
			w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		case strings.HasSuffix(r.URL.Path, "/sendChatAction"):
			f.sawTyping = true
			w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/setMyCommands"):
			b, _ := io.ReadAll(r.Body)
			f.sawSetCommands = true
			f.lastCommands = string(b)
			w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"nexus_test_bot"}}`))
		default:
			http.Error(w, "unknown method", 404)
		}
	}
}

func textUpdate(id int64, chat int64, text string) map[string]any {
	return map[string]any{"update_id": id, "message": map[string]any{
		"message_id": id, "chat": map[string]any{"id": chat, "type": "private"}, "from": map[string]any{"id": chat}, "text": text}}
}

func photoUpdate(id int64, chat int64) map[string]any {
	return map[string]any{"update_id": id, "message": map[string]any{
		"message_id": id, "chat": map[string]any{"id": chat, "type": "private"}, "from": map[string]any{"id": chat},
		"photo": []any{map[string]any{"file_id": "f1"}}}}
}

type harness struct {
	a    *Adapter
	bot  *fakeBot
	core *channel.Core
	got  []channel.Inbound
	j    *journal.Journal
}

func build(t *testing.T, bindings map[int64]string) *harness {
	t.Helper()
	h, _ := buildAt(t, t.TempDir(), bindings)
	return h
}

func buildAt(t *testing.T, dir string, bindings map[int64]string) (*harness, string) {
	t.Helper()
	bot := &fakeBot{}
	srv := httptest.NewServer(bot.handler())
	t.Cleanup(srv.Close)
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, channel.Events(), channel.NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	core, err := channel.New(j)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{bot: bot, core: core}
	t.Setenv("NEXUS_TEST_TG", "123:token")
	a, err := New(Config{
		APIBase:  srv.URL,
		TokenEnv: "NEXUS_TEST_TG",
		Bindings: bindings,
		Profile:  "work",
	}, core, func(ctx context.Context, in channel.Inbound) (string, error) {
		h.got = append(h.got, in)
		return "reply to: " + in.Text, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.a = a
	h.j = j
	return h, dir
}

// A BOUND chat's text flows: admitted once, handled, reply sent to the
// same chat; the getUpdates offset advances past the update.
func TestBoundChatRoundTrip(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.batches = [][]map[string]any{{textUpdate(1, 42, "hello")}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 1 || h.got[0].Text != "hello" || h.got[0].Profile != "work" {
		t.Fatalf("handler saw: %+v", h.got)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.bot.sent) != 1 || h.bot.sent[0] != "reply to: hello" || h.bot.sentTo[0] != 42 {
		t.Fatalf("reply not delivered: %v to %v", h.bot.sent, h.bot.sentTo)
	}
	// Offset advanced past update 1 on the NEXT poll.
	h.bot.batches = [][]map[string]any{{}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	last := h.bot.offsets[len(h.bot.offsets)-1]
	if last != 2 {
		t.Fatalf("offset did not advance past the handled update: %d", last)
	}
}

// DENY-DEFAULT (B3): an UNBOUND chat gets the typed "which profile?"
// refusal, NOTHING is admitted, and the handler never runs.
func TestUnboundChatRefusedNothingAdmitted(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.batches = [][]map[string]any{{textUpdate(1, 999, "sneaky")}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 0 {
		t.Fatalf("unbound chat reached the handler: %+v", h.got)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.bot.sent) != 1 || !strings.Contains(h.bot.sent[0], "not bound to a profile") {
		t.Fatalf("typed refusal missing: %v", h.bot.sent)
	}
	// Nothing admitted into the durable inbox.
	if st, _ := h.core.InboundStatus(ctxT(), "any"); st != "" {
		t.Fatal("unbound admission leaked")
	}
	pending, _ := h.core.Pending(ctxT())
	if len(pending) != 0 {
		t.Fatalf("outbox rows remain: %v", pending)
	}
}

// C2: a NON-TEXT update (photo) gets a typed fail-closed reply and the
// loop stays alive for the next update.
func TestNonTextTypedRefusalLoopAlive(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.batches = [][]map[string]any{
		{photoUpdate(1, 42)},
		{textUpdate(2, 42, "after photo")},
	}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.bot.sent) != 1 || !strings.Contains(h.bot.sent[0], "only text") {
		t.Fatalf("typed non-text refusal missing: %v", h.bot.sent)
	}
	// The loop is ALIVE: the next text update processes normally.
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 1 || h.got[0].Text != "after photo" {
		t.Fatalf("loop died after non-text: %+v", h.got)
	}
}

// Replayed update_id: the SAME update delivered twice (Telegram redelivers
// after an unacked offset) produces ONE admission and ONE handler run.
func TestReplayedUpdateNoDoubleEffect(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.batches = [][]map[string]any{
		{textUpdate(1, 42, "once")},
		{textUpdate(1, 42, "once")}, // redelivery
	}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 1 {
		t.Fatalf("replayed update ran the handler %d times", len(h.got))
	}
}

// Handler failure: the update is ACKED away only after a durable
// admission — a handler error still leaves the admission durable (the
// reply is what fails) and a typed error reply goes back.
func TestHandlerErrorTypedReply(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.a.handle = func(ctx context.Context, in channel.Inbound) (string, error) {
		return "", fmt.Errorf("turn exploded")
	}
	h.bot.batches = [][]map[string]any{{textUpdate(1, 42, "boom")}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.bot.sent) != 1 || !strings.Contains(h.bot.sent[0], "could not process") {
		t.Fatalf("typed error reply missing: %v", h.bot.sent)
	}
}

// Construction fails closed; the live probe binds the resolved config.
func TestConstructionAndProbe(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	if _, err := New(Config{APIBase: "http://x", TokenEnv: "NEXUS_MISSING_TG",
		Bindings: map[int64]string{}, Profile: "work"}, h.core, h.a.handle); err == nil {
		t.Fatal("empty token accepted")
	}
	resolved := config.Resolved{}
	pr := h.a.Probe(ctxT(), resolved)
	if pr.Name != "channel" || !pr.Passed || pr.ConfigHash != resolved.ConfigHash() {
		t.Fatalf("probe: %+v", pr)
	}
}

// Crash between admission and handler (Phase-5 codex #2 literal): a
// redelivered NON-TERMINAL update RE-RUNS the handler — the message is
// never silently dropped; a TERMINAL redelivery is skipped.
func TestNonTerminalReplayRerunsHandler(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	// Emulate the crash: admit directly (as a prior incarnation did),
	// handler never ran, then the update is redelivered.
	if _, err := h.core.Admit(ctxT(), channel.Inbound{
		AdapterID: "telegram", ChannelIdentity: "chat-42", UpdateID: 5,
		Text: "crashed before handling", Profile: "work"}); err != nil {
		t.Fatal(err)
	}
	h.bot.batches = [][]map[string]any{{textUpdate(5, 42, "crashed before handling")}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 1 {
		t.Fatalf("non-terminal replay did not re-run the handler: %d runs", len(h.got))
	}
	// Now TERMINAL: a second redelivery is skipped.
	h.bot.batches = [][]map[string]any{{textUpdate(5, 42, "crashed before handling")}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 1 {
		t.Fatalf("terminal replay re-ran the handler: %d runs", len(h.got))
	}
}

// Group chats are refused fail-closed (Phase-5 codex #5: every group
// member would inherit the owner's USER trust).
func TestGroupChatRefused(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.batches = [][]map[string]any{{map[string]any{"update_id": 1, "message": map[string]any{
		"message_id": 1, "chat": map[string]any{"id": 42, "type": "group"},
		"from": map[string]any{"id": 777}, "text": "group takeover"}}}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.got) != 0 {
		t.Fatal("group message reached the handler")
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.bot.sent) != 1 || !strings.Contains(h.bot.sent[0], "private chat") {
		t.Fatalf("typed group refusal missing: %v", h.bot.sent)
	}
}

// The bot token NEVER leaks into errors (Phase-5 codex #9/kilo #2/agy #1):
// a transport failure against a dead endpoint carries no token bytes.
func TestTokenNeverInErrors(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	dead, err := New(Config{
		APIBase: "http://127.0.0.1:1", TokenEnv: "NEXUS_TEST_TG",
		Bindings: map[int64]string{42: "work"}, Profile: "work",
	}, h.core, h.a.handle)
	if err != nil {
		t.Fatal(err)
	}
	perr := dead.PollOnce(ctxT())
	if perr == nil {
		t.Fatal("dead endpoint succeeded")
	}
	if strings.Contains(perr.Error(), "123:token") {
		t.Fatalf("token leaked into the error: %v", perr)
	}
	pr := dead.Probe(ctxT(), configResolved())
	if pr.Passed || strings.Contains(pr.Detail, "123:token") {
		t.Fatalf("token leaked into the probe detail: %+v", pr)
	}
}

func configResolved() config.Resolved { return config.Resolved{} }

// PRE-WIRE classification (Phase-5-r2 kilo #2): a dial-phase failure
// (connection refused — nothing left the process) re-pends the row for a
// safe retry instead of parking it UNKNOWN forever.
func TestPreWireFailureRepends(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	dead, err := New(Config{
		APIBase: "http://127.0.0.1:1", TokenEnv: "NEXUS_TEST_TG",
		Bindings: map[int64]string{42: "work"}, Profile: "work",
	}, h.core, h.a.handle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := dead.FlushOutbox(ctxT()); err == nil {
		t.Fatal("dead endpoint flush succeeded")
	}
	pending, _ := h.core.Pending(ctxT())
	unknown, _ := h.core.Unreconciled(ctxT())
	if len(pending) != 1 || len(unknown) != 0 {
		t.Fatalf("connection-refused misclassified: pending=%d unknown=%d (want 1/0)", len(pending), len(unknown))
	}
}

// Request-construction errors are sanitized too (Phase-5-r2 codex #8):
// an invalid URL escape renders Go's parser error WITH the full bot URL.
func TestRequestConstructionSanitized(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	bad, err := New(Config{
		APIBase: "http://x/%zz", TokenEnv: "NEXUS_TEST_TG",
		Bindings: map[int64]string{42: "work"}, Profile: "work",
	}, h.core, h.a.handle)
	if err != nil {
		t.Fatal(err)
	}
	perr := bad.PollOnce(ctxT())
	if perr == nil {
		t.Fatal("invalid URL accepted")
	}
	if strings.Contains(perr.Error(), "123:token") {
		t.Fatalf("token leaked from request construction: %v", perr)
	}
}

// FRESH-AUDIT kilo F3: a crash between a typed refusal and the next
// poll's offset confirmation makes Telegram REDELIVER the refused update
// — the refusal delivery id is derived from the update identity, so the
// redelivered refusal is an idempotent no-op (ONE outbox row, one send),
// never a second user-visible message.
func TestRedeliveredRefusalIsIdempotent(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	// Same unbound-chat update delivered twice (adapter restarted with
	// offset 0 → Telegram re-serves the unconfirmed update).
	h.bot.batches = [][]map[string]any{
		{textUpdate(7, 999, "sneaky")},
		{textUpdate(7, 999, "sneaky")},
	}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.a.offset = 0 // simulate the restart: in-memory offset is gone
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	if len(h.bot.sent) != 1 {
		t.Fatalf("redelivered refusal produced %d sends, want exactly 1: %v", len(h.bot.sent), h.bot.sent)
	}
}

// TGOUT plan detectors — first-lease formatting + delivery honesty.

// A formatted reply carries parse_mode=HTML on its FIRST lease; a
// scripted parse-400 makes the NEXT flush carry the ORIGINAL plain
// bytes — and the row lands SENT (the detector asserts carried BYTES;
// scheduling is exactly the existing machinery).
func TestFirstLeaseFormattedThenPlainAfterParse400(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.rejectHTMLLeft = 1
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "**bold** reply"); err != nil {
		t.Fatal(err)
	}
	// First flush: rendered+parse_mode -> scripted parse-400 -> re-pend.
	_ = h.a.FlushOutbox(ctxT())
	// Existing machinery: definite error re-pends; next tick carries plain.
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if len(h.bot.sent) != 1 {
		t.Fatalf("want exactly 1 accepted send, got %d: %v", len(h.bot.sent), h.bot.sent)
	}
	if h.bot.sent[0] != "**bold** reply" {
		t.Fatalf("re-attempt did not carry the ORIGINAL plain bytes: %q", h.bot.sent[0])
	}
	if h.bot.parseModes[0] != "" {
		t.Fatalf("re-attempt still carried parse_mode: %q", h.bot.parseModes[0])
	}
	pending, _ := h.core.Pending(ctxT())
	if len(pending) != 0 {
		t.Fatalf("row not SENT after plain accept: %v", pending)
	}
}

// A formatted first lease that the wire ACCEPTS carries parse_mode=HTML
// with the rendered body.
func TestFirstLeaseCarriesRenderedHTML(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "**bold** reply"); err != nil {
		t.Fatal(err)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if len(h.bot.sent) != 1 || h.bot.parseModes[0] != "HTML" {
		t.Fatalf("first lease not rendered: sent=%v modes=%v", h.bot.sent, h.bot.parseModes)
	}
	if !strings.Contains(h.bot.sent[0], "<b>bold</b>") {
		t.Fatalf("rendered body missing: %q", h.bot.sent[0])
	}
}

// PRE-WIRE failure degradation (plan v10, declared): a dial failure
// consumes the first lease, so the first ACTUAL wire send is plain.
func TestPreWireFailureConsumesLease(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "**bold** reply"); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-wire dial failure: point the adapter at a dead URL
	// for the first flush only.
	live := h.a.base
	h.a.base = "http://127.0.0.1:1"
	_ = h.a.FlushOutbox(ctxT())
	h.a.base = live
	// The lease was consumed pre-wire; recovery re-pends via reconcile
	// semantics — drive the existing paths.
	_ = h.a.FlushOutbox(ctxT())
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	for i, m := range h.bot.parseModes {
		if m != "" {
			t.Fatalf("send %d carried parse_mode after a consumed lease: %v", i, h.bot.parseModes)
		}
	}
}

// RESTART persistence (plan v10): the attempts count survives a journal
// close/reopen — the re-attempt still carries plain.
func TestAttemptsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	open := func() (*harness, string) { return buildAt(t, dir, map[int64]string{42: "work"}) }
	h, jp := open()
	h.bot.rejectHTMLLeft = 1
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "**bold** reply"); err != nil {
		t.Fatal(err)
	}
	_ = h.a.FlushOutbox(ctxT()) // formatted attempt -> parse-400 -> re-pend
	h.j.Close()
	h2, _ := open()
	_ = jp
	if err := h2.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h2.bot.mu.Lock()
	defer h2.bot.mu.Unlock()
	if len(h2.bot.sent) != 1 || h2.bot.parseModes[0] != "" || h2.bot.sent[0] != "**bold** reply" {
		t.Fatalf("restart lost the lease count: sent=%v modes=%v", h2.bot.sent, h2.bot.parseModes)
	}
}

// VERSION REBUILD (plan v10): a database folded at the OLD projection
// version (v1 schema without attempts) refolds on open — the count is
// reconstructed from the canonical events and the re-attempt carries
// plain.
func TestOldVersionDatabaseRebuildsAttempts(t *testing.T) {
	dir := t.TempDir()
	h, _ := buildAt(t, dir, map[int64]string{42: "work"})
	h.bot.rejectHTMLLeft = 1
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "**bold** reply"); err != nil {
		t.Fatal(err)
	}
	_ = h.a.FlushOutbox(ctxT()) // consumes the first lease durably
	h.j.Close()
	// Regress the database to the v1 shape: drop the column by table
	// rebuild and stamp the OLD projection version.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE chan_outbox DROP COLUMN attempts`,
		`UPDATE proj_sync_offsets SET version=1 WHERE name='channel'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	db.Close()
	// Reopen: version mismatch -> reset + whole refold from events.
	h2, _ := buildAt(t, dir, map[int64]string{42: "work"})
	if err := h2.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h2.bot.mu.Lock()
	defer h2.bot.mu.Unlock()
	if len(h2.bot.sent) != 1 || h2.bot.parseModes[0] != "" {
		t.Fatalf("rebuilt database lost the lease count: sent=%v modes=%v", h2.bot.sent, h2.bot.parseModes)
	}
}

// R1: a pipe table is delivered via sendRichMessage (Bot API 10.1).
func TestPipeTableUsesRichMessage(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	tbl := "Evo:\n| Proizvod | Cijena |\n|---|---|\n| Kruh | 1,50 € |"
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", tbl); err != nil {
		t.Fatal(err)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if h.bot.lastMethod != "sendRichMessage" {
		t.Fatalf("table not sent via sendRichMessage: %q", h.bot.lastMethod)
	}
	if !strings.Contains(h.bot.lastRich, "| Proizvod | Cijena |") {
		t.Fatalf("raw markdown table not carried: %q", h.bot.lastRich)
	}
}

// R1 codex #1: an OVER-LIMIT table must NOT use sendRichMessage (no
// silent clip earning SENT) — it falls through to today's path.
func TestOverLimitTableSkipsRich(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	big := "| a | b |\n|---|---|\n" + strings.Repeat("| x | "+strings.Repeat("y", 100)+" |\n", 400)
	if len([]rune(big)) <= 32768 {
		t.Fatalf("fixture not over-limit: %d", len([]rune(big)))
	}
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", big); err != nil {
		t.Fatal(err)
	}
	_ = h.a.FlushOutbox(ctxT())
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if h.bot.lastMethod == "sendRichMessage" {
		t.Fatal("over-limit table clipped into sendRichMessage")
	}
}

// A non-table reply keeps the plain/HTML path (no rich).
func TestNonTableSkipsRichMessage(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	if _, err := h.core.EnqueueReply(ctxT(), "telegram", "chat-42", "work", "obican odgovor"); err != nil {
		t.Fatal(err)
	}
	if err := h.a.FlushOutbox(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if h.bot.lastMethod == "sendRichMessage" {
		t.Fatal("plain reply wrongly used sendRichMessage")
	}
}

// TG polish: a handled message fires the typing chat action.
func TestTypingActionOnHandledMessage(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.bot.batches = [][]map[string]any{{textUpdate(1, 42, "bok")}}
	if err := h.a.PollOnce(ctxT()); err != nil {
		t.Fatal(err)
	}
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if !h.bot.sawTyping {
		t.Fatal("typing chat action not sent for a handled message")
	}
}

// TG polish: registerCommands publishes the command menu.
func TestRegisterCommandsPublishesMenu(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	h.a.registerCommands(ctxT())
	h.bot.mu.Lock()
	defer h.bot.mu.Unlock()
	if !h.bot.sawSetCommands {
		t.Fatal("setMyCommands not called")
	}
	if !strings.Contains(h.bot.lastCommands, "help") {
		t.Fatalf("command menu missing entries: %q", h.bot.lastCommands)
	}
}

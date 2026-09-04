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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var req struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			f.sent = append(f.sent, req.Text)
			f.sentTo = append(f.sentTo, req.ChatID)
			w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
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
}

func build(t *testing.T, bindings map[int64]string) *harness {
	t.Helper()
	bot := &fakeBot{}
	srv := httptest.NewServer(bot.handler())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
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
	return h
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

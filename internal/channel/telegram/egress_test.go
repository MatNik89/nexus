package telegram

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/foundation/egress"
	"github.com/MatNik89/nexus/internal/kernel/journal"
)

// A shared-owner refusal wrapped the way http.Client returns it (url.Error)
// must classify pre-wire, so the outbox re-pends PENDING (Slice A phase; S7-
// scheduled PENDING/FAILED after Slice B) and never strands the row UNKNOWN.
func TestEgressRefusalIsPreWire(t *testing.T) {
	base := fmt.Errorf("telegram egress: resolved address rejected by policy: %w", egress.ErrPreWire)
	wrapped := &url.Error{Op: "Post", URL: "https://api.telegram.org/botX/getUpdates", Err: base}
	if !isPreWire(wrapped) {
		t.Fatalf("egress refusal not classified pre-wire (would strand the outbox UNKNOWN)")
	}
}

// The adapter refuses to exist without the shared receipt sink (no
// unreceipted egress mode, not even for tests).
func TestAdapterRequiresReceiptSink(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	if _, err := New(Config{APIBase: "http://127.0.0.1:1", TokenEnv: "NEXUS_TEST_TG",
		Bindings: map[int64]string{42: "work"}, Profile: "work"}, h.core, h.a.handle); err == nil {
		t.Fatal("adapter built without a receipt sink")
	}
}

// A real request through the shared pinned client must leave a durable,
// complete egress receipt in the journal, tagged component=telegram, carrying
// the resolved set + pin and NEVER the bot token.
func TestEgressReceiptJournaled(t *testing.T) {
	h := build(t, map[int64]string{42: "work"})
	_ = h.a.PollOnce(ctxT())
	var allowed bool
	if err := h.j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType != channel.EvEgressAttempt {
			return nil
		}
		if strings.Contains(string(ev.Envelope.Payload), "123:token") {
			t.Fatalf("bot token leaked into an egress receipt")
		}
		var p struct {
			Component string   `json:"component"`
			Host      string   `json:"host"`
			Resolved  []string `json:"resolved"`
			Pinned    string   `json:"pinned"`
			Allowed   bool     `json:"allowed"`
		}
		if err := json.Unmarshal(ev.Envelope.Payload, &p); err != nil {
			return err
		}
		if p.Component != "telegram" {
			t.Fatalf("receipt not tagged with its component: %+v", p)
		}
		if p.Allowed && p.Pinned != "" && len(p.Resolved) > 0 {
			allowed = true
		}
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !allowed {
		t.Fatalf("no complete allowed egress receipt journaled after a poll")
	}
}

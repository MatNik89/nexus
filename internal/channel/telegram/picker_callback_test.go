package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/channel"
	"github.com/MatNik89/nexus/internal/kernel/journal"
)

// mkCallback crafts a tgCallbackQuery the way Telegram delivers it (JSON), so
// the anonymous From/Message shapes are populated without fragile literals.
func mkCallback(t *testing.T, id string, from, chat int64, data string) *tgCallbackQuery {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"from":{"id":%d},"message":{"message_id":5,"chat":{"id":%d,"type":"private"}},"data":%q}`, id, from, chat, data)
	var cq tgCallbackQuery
	if err := json.Unmarshal([]byte(raw), &cq); err != nil {
		t.Fatalf("craft callback: %v", err)
	}
	return &cq
}

func withPicker(t *testing.T) (*harness, *PickerStore) {
	t.Helper()
	h := build(t, map[int64]string{42: "work"})
	ps := NewPickerStore(time.Minute)
	h.a.SetPicker(ps, time.UTC)
	return h, ps
}

// admissions counts admitted inbound messages. A callback is ephemeral UI and
// must NEVER admit a message (the E11 egress receipt it does journal is not an
// admission and is correctly ignored here).
func admissions(t *testing.T, h *harness) int {
	t.Helper()
	n := 0
	if err := h.j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == channel.EvInboundAdmitted {
			n++
		}
		return nil
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	return n
}

func TestCallbackForeignOwnerRefused(t *testing.T) {
	h, ps := withPicker(t)
	sess := ps.open(42, 2026, 9)
	// A tapper whose id != the session's chat is refused; the session is
	// untouched, NO message is admitted, and the query is still answered.
	cq := mkCallback(t, "cb1", 99, 42, encodeCallback(sess.sid, actDay, "2026-09-14"))
	if err := h.a.handlePickerCallback(context.Background(), cq); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if ps.get(sess.sid).stage != stageMonth {
		t.Fatalf("foreign owner advanced the session")
	}
	if h.bot.answerCount == 0 {
		t.Fatalf("callback was not answered (spinner left spinning)")
	}
	if admissions(t, h) != 0 {
		t.Fatalf("a callback admitted a message (must be ephemeral)")
	}
}

func TestCallbackIllegalInStateRefused(t *testing.T) {
	h, ps := withPicker(t)
	sess := ps.open(42, 2026, 9) // stageMonth
	// An hour tap is illegal before a day is chosen.
	cq := mkCallback(t, "cb1", 42, 42, encodeCallback(sess.sid, actHour, "9"))
	_ = h.a.handlePickerCallback(context.Background(), cq)
	if ps.get(sess.sid).stage != stageMonth {
		t.Fatalf("illegal-in-state hour tap advanced the session")
	}
	if h.bot.answerCount == 0 {
		t.Fatalf("illegal callback not answered")
	}
}

func TestCallbackExpiredOrMalformedRefused(t *testing.T) {
	h, _ := withPicker(t)
	for _, data := range []string{
		"pk:v1:" + newSID() + ":d:2026-09-14", // unknown (never-opened) sid
		"garbage",                             // malformed
	} {
		cq := mkCallback(t, "cb", 42, 42, data)
		if err := h.a.handlePickerCallback(context.Background(), cq); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	if h.bot.answerCount < 2 {
		t.Fatalf("expired/malformed callbacks must still be answered, got %d", h.bot.answerCount)
	}
}

func TestCallbackFullFlowReachesAwait(t *testing.T) {
	h, ps := withPicker(t)
	sess := ps.open(42, 2026, 9)
	ctx := context.Background()
	steps := []*tgCallbackQuery{
		mkCallback(t, "c1", 42, 42, encodeCallback(sess.sid, actDay, "2026-09-14")),
		mkCallback(t, "c2", 42, 42, encodeCallback(sess.sid, actHour, "9")),
		mkCallback(t, "c3", 42, 42, encodeCallback(sess.sid, actMinute, "30")),
	}
	for i, cq := range steps {
		if err := h.a.handlePickerCallback(ctx, cq); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	y, mo, d, hh, mi, ok := ps.AwaitingPick(42)
	if !ok || y != 2026 || mo != time.September || d != 14 || hh != 9 || mi != 30 {
		t.Fatalf("full pick did not reach await with the tapped datetime: %d-%v-%d %d:%d ok=%v", y, mo, d, hh, mi, ok)
	}
	if h.bot.answerCount != 3 {
		t.Fatalf("expected 3 callback answers, got %d", h.bot.answerCount)
	}
}

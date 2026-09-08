package telegram

// Ephemeral /cronjob calendar interaction (PLAN-CRONJOB.md): command open +
// button-tap callbacks. All sends here are BEST-EFFORT chrome (never
// outbox-tracked); the durable reminder is created by the daemon handler from
// the completed pick. All state transitions run on the single getUpdates
// goroutine, so the session pointer is mutated without extra locking.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func isCronjobCmd(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return t == "/cronjob" || t == "cronjob"
}

// openPicker starts an ephemeral session for chat and sends the month grid.
func (a *Adapter) openPicker(ctx context.Context, chat int64) error {
	loc := a.loc
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	sess := a.picker.open(chat, now.Year(), int(now.Month()))
	var res struct {
		MessageID int64 `json:"message_id"`
	}
	if err := a.call(ctx, "sendMessage", map[string]any{
		"chat_id": chat, "text": "Odaberi datum:",
		"reply_markup": map[string]any{"inline_keyboard": monthKeyboard(sess)},
	}, &res); err == nil {
		sess.messageID = res.MessageID
	}
	// Ephemeral: a send failure just means the owner re-runs /cronjob.
	return nil
}

// handlePickerCallback advances (or refuses) one button tap. It ALWAYS answers
// the callback query (clears the client spinner), makes NO durable write, and
// refuses benignly on any invalid/foreign/expired input.
func (a *Adapter) handlePickerCallback(ctx context.Context, cq *tgCallbackQuery) error {
	answer := func(text string) error {
		if cq != nil && cq.ID != "" {
			a.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": cq.ID, "text": text}, nil)
		}
		return nil
	}
	if a.picker == nil || cq == nil || cq.From == nil || cq.Message == nil {
		return answer("")
	}
	chat := cq.Message.Chat.ID
	// Same single-user + deny-default gate as messages.
	if cq.Message.Chat.Type != "private" {
		return answer("")
	}
	if bound, ok := a.bindings[chat]; !ok || bound != a.profile {
		return answer("")
	}
	// Owner binding: in a private chat the tapper id equals the chat id.
	if cq.From.ID != chat {
		return answer("Nije tvoj izbornik.")
	}
	cd, ok := parseCallback(cq.Data)
	if !ok {
		return answer("Isteklo.")
	}
	sess := a.picker.get(cd.sid)
	if sess == nil || sess.chatID != chat {
		return answer("Isteklo.")
	}
	msgID := cq.Message.MessageID

	switch cd.act {
	case actCancel:
		a.picker.DropSource(chat)
		a.editText(ctx, chat, msgID, "Otkazano.", nil)
		return answer("Otkazano.")

	case actNav:
		if sess.stage != stageMonth {
			return answer("")
		}
		step := 0
		switch cd.arg {
		case "+1":
			step = 1
		case "-1":
			step = -1
		default:
			return answer("")
		}
		t := time.Date(sess.viewYear, time.Month(sess.viewMonth), 1, 0, 0, 0, 0, time.UTC).AddDate(0, step, 0)
		sess.viewYear, sess.viewMonth = t.Year(), int(t.Month())
		a.editCalendar(ctx, chat, msgID, monthKeyboard(sess))
		return answer("")

	case actDay:
		if sess.stage != stageMonth {
			return answer("")
		}
		d, ok := parseISODayInMonth(cd.arg, sess.viewYear, sess.viewMonth)
		if !ok {
			return answer("")
		}
		sess.day, sess.stage = d, stageHour
		a.editText(ctx, chat, msgID, "Odaberi sat:", hourKeyboard(sess))
		return answer("")

	case actHour:
		if sess.stage != stageHour {
			return answer("")
		}
		h, err := strconv.Atoi(cd.arg)
		if err != nil || h < 0 || h > 23 {
			return answer("")
		}
		sess.hour, sess.stage = h, stageMinute
		a.editText(ctx, chat, msgID, "Odaberi minutu:", minuteKeyboard(sess))
		return answer("")

	case actMinute:
		if sess.stage != stageMinute {
			return answer("")
		}
		mi, err := strconv.Atoi(cd.arg)
		if err != nil || (mi != 0 && mi != 15 && mi != 30 && mi != 45) {
			return answer("")
		}
		sess.minute, sess.stage = mi, stageAwaitText
		a.editText(ctx, chat, msgID, fmt.Sprintf(
			"Termin: %04d-%02d-%02d %02d:%02d\nUpisi tekst podsjetnika kao poruku.",
			sess.viewYear, sess.viewMonth, sess.day, sess.hour, sess.minute), nil)
		return answer("")
	}
	return answer("")
}

// parseISODayInMonth parses YYYY-MM-DD and confirms it falls in the shown
// month with a valid day (Go's time normalizes overflow, so re-check).
func parseISODayInMonth(iso string, y, m int) (int, bool) {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return 0, false
	}
	if t.Year() != y || int(t.Month()) != m {
		return 0, false
	}
	return t.Day(), true
}

// editCalendar swaps only the inline keyboard (month nav). Best-effort.
func (a *Adapter) editCalendar(ctx context.Context, chat, msgID int64, rows [][]button) {
	a.call(ctx, "editMessageReplyMarkup", map[string]any{
		"chat_id": chat, "message_id": msgID,
		"reply_markup": map[string]any{"inline_keyboard": rows},
	}, nil)
}

// editText rewrites the message text (and keyboard if rows != nil). Best-effort;
// a Bot API "message is not modified" 400 is a harmless no-op here.
func (a *Adapter) editText(ctx context.Context, chat, msgID int64, text string, rows [][]button) {
	req := map[string]any{"chat_id": chat, "message_id": msgID, "text": text}
	if rows != nil {
		req["reply_markup"] = map[string]any{"inline_keyboard": rows}
	}
	a.call(ctx, "editMessageText", req, nil)
}

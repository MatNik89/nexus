package telegram

// /cronjob inline-calendar picker (PLAN-CRONJOB.md). EPHEMERAL by owner
// decision: the in-progress pick lives in memory with a TTL; only the final
// reminder + confirmation are durable (the daemon's telegram handler builds
// them via the existing turn recipe). This file owns the session store, the
// inline-keyboard calendar render, and the strict UNTRUSTED callback_data
// grammar. Sessions key by CHAT id: the adapter admits only PRIVATE chats, so
// chat id == the owner's user id, and the callback owner-binding checks
// from.id == the session's chat.
//
// The store is exported so the composition root can share ONE instance between
// the adapter (ephemeral UI: /cronjob + callbacks) and the durable handler
// (which reads the completed pick to create the reminder).

import (
	"crypto/rand"
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"time"
)

type pickerStage int

const (
	stageMonth     pickerStage = iota // choosing a day
	stageHour                         // day set, choosing an hour
	stageMinute                       // hour set, choosing a minute
	stageAwaitText                    // datetime set, waiting for the description
)

type pickerSession struct {
	sid       string
	chatID    int64
	messageID int64 // the calendar message being edited in place
	stage     pickerStage
	viewYear  int
	viewMonth int
	day       int
	hour      int
	minute    int
	expires   time.Time
}

// PickerStore holds live picker sessions, one per chat, with a TTL.
type PickerStore struct {
	mu   sync.Mutex
	byID map[string]*pickerSession
	ttl  time.Duration
	now  func() time.Time
}

// NewPickerStore creates the shared store (ttl bounds an abandoned pick).
func NewPickerStore(ttl time.Duration) *PickerStore {
	return &PickerStore{byID: map[string]*pickerSession{}, ttl: ttl, now: time.Now}
}

// open starts a session for chatID, REPLACING any existing live one for that
// chat (one live picker per chat, PLAN-CRONJOB.md).
func (s *PickerStore) open(chatID int64, y, m int) *pickerSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.byID {
		if sess.chatID == chatID {
			delete(s.byID, id)
		}
	}
	sess := &pickerSession{sid: newSID(), chatID: chatID, stage: stageMonth,
		viewYear: y, viewMonth: m, expires: s.now().Add(s.ttl)}
	s.byID[sess.sid] = sess
	return sess
}

// get returns the live session for sid, or nil if unknown/expired (lazy sweep).
func (s *PickerStore) get(sid string) *pickerSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[sid]
	if sess == nil {
		return nil
	}
	if s.now().After(sess.expires) {
		delete(s.byID, sid)
		return nil
	}
	return sess
}

// AwaitingPick reports whether chat has a completed pick waiting for its
// description, and returns the chosen wall-clock components. It does NOT
// consume the session (the handler drops it only after a durable commit).
func (s *PickerStore) AwaitingPick(chatID int64) (y int, mo time.Month, d, h, mi int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.byID {
		if s.now().After(sess.expires) {
			delete(s.byID, id)
			continue
		}
		if sess.chatID == chatID && sess.stage == stageAwaitText {
			return sess.viewYear, time.Month(sess.viewMonth), sess.day, sess.hour, sess.minute, true
		}
	}
	return 0, 0, 0, 0, 0, false
}

// DropSource removes any session for chat (after a durable commit, or cancel).
// Best-effort: correctness never depends on it (the durable reminder lookup is
// authoritative).
func (s *PickerStore) DropSource(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.byID {
		if sess.chatID == chatID {
			delete(s.byID, id)
		}
	}
}

func newSID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b) // 22 chars, url-safe
}

// --- UNTRUSTED callback_data grammar: pk:v1:<sid>:<act>:<arg> ---

type callbackAction string

const (
	actNav    callbackAction = "nav"
	actDay    callbackAction = "d"
	actHour   callbackAction = "h"
	actMinute callbackAction = "m"
	actCancel callbackAction = "x"
)

type callbackData struct {
	sid string
	act callbackAction
	arg string
}

// parseCallback strictly parses callback_data; ok=false on ANY deviation.
// Per-action arg RANGE + legal-in-state are checked by the handler against the
// live session.
func parseCallback(data string) (callbackData, bool) {
	if len(data) == 0 || len(data) > 64 {
		return callbackData{}, false
	}
	parts := strings.SplitN(data, ":", 5)
	if len(parts) != 5 || parts[0] != "pk" || parts[1] != "v1" {
		return callbackData{}, false
	}
	sid, act, arg := parts[2], callbackAction(parts[3]), parts[4]
	if !validSID(sid) {
		return callbackData{}, false
	}
	switch act {
	case actNav, actDay, actHour, actMinute, actCancel:
	default:
		return callbackData{}, false
	}
	return callbackData{sid: sid, act: act, arg: arg}, true
}

func validSID(sid string) bool {
	if len(sid) != 22 {
		return false
	}
	for _, r := range sid {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func encodeCallback(sid string, act callbackAction, arg string) string {
	return "pk:v1:" + sid + ":" + string(act) + ":" + arg
}

// --- inline-keyboard calendar render ---
// Each returns Telegram's reply_markup inline_keyboard: [][]button.

type button = map[string]any

func btn(text, data string) button { return button{"text": text, "callback_data": data} }

var monthNames = []string{"", "sij", "velj", "ozu", "tra", "svi", "lip", "srp", "kol", "ruj", "lis", "stu", "pro"}

// monthKeyboard renders the day grid for the session's viewed month, with
// month-nav and cancel. Past days in the current month are still tappable;
// WallTime validation + a past-instant check happen at commit.
func monthKeyboard(sess *pickerSession) [][]button {
	y, m := sess.viewYear, sess.viewMonth
	first := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
	days := first.AddDate(0, 1, -1).Day()
	lead := (int(first.Weekday()) + 6) % 7 // Monday=0
	header := []button{
		btn("<", encodeCallback(sess.sid, actNav, "-1")),
		btn(monthNames[m]+" "+strconv.Itoa(y), encodeCallback(sess.sid, actCancel, "")),
		btn(">", encodeCallback(sess.sid, actNav, "+1")),
	}
	dow := []button{}
	for _, d := range []string{"Po", "Ut", "Sr", "Ce", "Pe", "Su", "Ne"} {
		dow = append(dow, btn(d, encodeCallback(sess.sid, actCancel, "")))
	}
	rows := [][]button{header, dow}
	row := []button{}
	for i := 0; i < lead; i++ {
		row = append(row, btn(" ", encodeCallback(sess.sid, actCancel, "")))
	}
	for d := 1; d <= days; d++ {
		iso := first.AddDate(0, 0, d-1).Format("2006-01-02")
		row = append(row, btn(strconv.Itoa(d), encodeCallback(sess.sid, actDay, iso)))
		if len(row) == 7 {
			rows = append(rows, row)
			row = []button{}
		}
	}
	if len(row) > 0 {
		for len(row) < 7 {
			row = append(row, btn(" ", encodeCallback(sess.sid, actCancel, "")))
		}
		rows = append(rows, row)
	}
	rows = append(rows, []button{btn("Odustani", encodeCallback(sess.sid, actCancel, ""))})
	return rows
}

// hourKeyboard renders 0..23 in rows of 6 + Back(cancel-to-noop is not needed;
// cancel ends the pick).
func hourKeyboard(sess *pickerSession) [][]button {
	rows := [][]button{}
	row := []button{}
	for h := 0; h < 24; h++ {
		row = append(row, btn(pad2(h), encodeCallback(sess.sid, actHour, strconv.Itoa(h))))
		if len(row) == 6 {
			rows = append(rows, row)
			row = []button{}
		}
	}
	rows = append(rows, []button{btn("Odustani", encodeCallback(sess.sid, actCancel, ""))})
	return rows
}

// minuteKeyboard renders the coarse minute choices.
func minuteKeyboard(sess *pickerSession) [][]button {
	row := []button{}
	for _, mi := range []int{0, 15, 30, 45} {
		row = append(row, btn(pad2(mi), encodeCallback(sess.sid, actMinute, strconv.Itoa(mi))))
	}
	return [][]button{row, {btn("Odustani", encodeCallback(sess.sid, actCancel, ""))}}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

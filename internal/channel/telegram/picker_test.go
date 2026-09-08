package telegram

import (
	"testing"
	"time"
)

func TestParseCallbackStrict(t *testing.T) {
	sid := newSID() // valid 22-char sid
	good := "pk:v1:" + sid + ":d:2026-09-14"
	cd, ok := parseCallback(good)
	if !ok || cd.sid != sid || cd.act != actDay || cd.arg != "2026-09-14" {
		t.Fatalf("valid callback rejected or misparsed: %+v ok=%v", cd, ok)
	}
	bad := []string{
		"",                                   // empty
		"nope:v1:" + sid + ":d:1",            // wrong prefix
		"pk:v2:" + sid + ":d:1",              // wrong version
		"pk:v1:" + sid + ":zzz:1",            // unknown action
		"pk:v1:short:d:1",                    // bad sid length
		"pk:v1:" + sid + ":d",                // too few fields
		"pk:v1:" + sid + ":d:" + longArg(70), // over 64 bytes
	}
	for _, b := range bad {
		if _, ok := parseCallback(b); ok {
			t.Fatalf("malformed callback accepted: %q", b)
		}
	}
}

func longArg(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += "x"
	}
	return s
}

func TestPickerStoreOneLivePerChat(t *testing.T) {
	s := NewPickerStore(time.Minute)
	a := s.open(100, 2026, 9)
	b := s.open(100, 2026, 9) // same chat -> replaces a
	if s.get(a.sid) != nil {
		t.Fatalf("first session for the chat was not replaced")
	}
	if s.get(b.sid) == nil {
		t.Fatalf("second session is not live")
	}
}

func TestPickerStoreTTLExpiry(t *testing.T) {
	s := NewPickerStore(time.Minute)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	sess := s.open(1, 2026, 9)
	now = now.Add(2 * time.Minute) // past TTL
	if s.get(sess.sid) != nil {
		t.Fatalf("expired session still live")
	}
}

func TestPickerAwaitingPickAfterFullPick(t *testing.T) {
	s := NewPickerStore(time.Minute)
	sess := s.open(42, 2026, 9)
	sess.day, sess.hour, sess.minute, sess.stage = 14, 9, 30, stageAwaitText
	y, mo, d, h, mi, ok := s.AwaitingPick(42)
	if !ok || y != 2026 || mo != time.September || d != 14 || h != 9 || mi != 30 {
		t.Fatalf("awaiting pick wrong: %d-%v-%d %d:%d ok=%v", y, mo, d, h, mi, ok)
	}
	// a chat still choosing (not awaiting) is not returned
	other := s.open(43, 2026, 9)
	_ = other
	if _, _, _, _, _, ok := s.AwaitingPick(43); ok {
		t.Fatalf("a still-choosing session must not be awaiting")
	}
	s.DropSource(42)
	if _, _, _, _, _, ok := s.AwaitingPick(42); ok {
		t.Fatalf("dropped session still awaiting")
	}
}

func TestMonthKeyboardDaysParseBack(t *testing.T) {
	sess := &pickerSession{sid: newSID(), stage: stageMonth, viewYear: 2026, viewMonth: 9}
	rows := monthKeyboard(sess)
	found := 0
	for _, row := range rows {
		for _, b := range row {
			data, _ := b["callback_data"].(string)
			cd, ok := parseCallback(data)
			if ok && cd.act == actDay {
				d, ok := parseISODayInMonth(cd.arg, 2026, 9)
				if !ok || d < 1 || d > 30 {
					t.Fatalf("day button %q parsed to invalid day %d", cd.arg, d)
				}
				found++
			}
		}
	}
	if found != 30 { // September has 30 days
		t.Fatalf("expected 30 day buttons, got %d", found)
	}
}

package obligation

import "testing"

// The /cronjob front replay guard depends on ReminderIntent distinguishing
// NotFound from Found and returning the persisted body + WallTime so the
// confirmation is reproducible without any in-memory session.
func TestReminderIntentLookup(t *testing.T) {
	h := build(t)

	st, _, _, err := h.m.ReminderIntent(ctxT(), "rem-absent")
	if err != nil || st != ReminderNotFound {
		t.Fatalf("absent reminder: state=%v err=%v (want NotFound)", st, err)
	}

	if err := h.m.CreateReminder(ctxT(), "rem-x", "call the doctor", wall); err != nil {
		t.Fatalf("create: %v", err)
	}
	st, body, w, err := h.m.ReminderIntent(ctxT(), "rem-x")
	if err != nil || st != ReminderFound {
		t.Fatalf("created reminder: state=%v err=%v (want Found)", st, err)
	}
	if body != "call the doctor" || w != wall {
		t.Fatalf("persisted intent mismatch: body=%q wall=%+v", body, w)
	}
}

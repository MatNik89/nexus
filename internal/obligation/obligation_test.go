//go:build linux

// T21 RED table (tasks-P0): ObligationStore (9.6-min) — Reminder
// SCHEDULED→DUE→DELIVERY_PENDING→DELIVERED→ACKED|EXPIRED, writes THROUGH
// the journal; ack correlates to the SAME occurrence (HARDQ B5); MarkDone
// only with checker-graded Evidence (T12); concrete file_note task with a
// verified postcondition; a registry without handlers is impossible.
package obligation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/schedule"
	"github.com/MatNik89/nexus/internal/security/redact"
)

func ctxT() context.Context { return context.Background() }

type harness struct {
	m     *Manager
	sched *schedule.Scheduler
	clock *clockid.Fake
	dir   string
	path  string
}

func build(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	clock := clockid.NewFake(time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC))
	events := Events()
	for n, v := range schedule.Events() {
		events[n] = v
	}
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	j, err := journal.Open(filepath.Join(dir, "journal.db"), "work", redact.None{}, events,
		schedule.NewProjection(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	sched, err := schedule.New(j, clock)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(map[string]Handler{
		"file_note": FileNoteHandler(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(j, sched, reg, clock)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{m: m, sched: sched, clock: clock, dir: dir, path: filepath.Join(dir, "journal.db")}
}

func fireDue(t *testing.T, h *harness) schedule.Fired {
	t.Helper()
	h.clock.Advance(3 * time.Hour)
	fired, err := h.sched.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("sweep: %v %v", fired, err)
	}
	if err := h.m.MarkDue(ctxT(), fired[0]); err != nil {
		t.Fatal(err)
	}
	return fired[0]
}

var wall = schedule.WallTime{Year: 2026, Month: 9, Day: 4, Hour: 12, Minute: 30, TZ: "Europe/Zagreb"}

// Reminder lifecycle e2e: created (atomically with its schedule) → due →
// delivery_pending → delivered → acked; every step durable; restart
// preserves the state.
func TestReminderLifecycleEndToEnd(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-1", "call mom", wall); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.m.Status(ctxT(), "rem-1"); st != StateScheduled {
		t.Fatalf("state %v, want SCHEDULED", st)
	}
	f := fireDue(t, h)
	if st, _ := h.m.Status(ctxT(), "rem-1"); st != StateDeliveryPending {
		t.Fatalf("state after due %v, want DELIVERY_PENDING", st)
	}
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkAcked(ctxT(), f.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	st, err := h.m.Status(ctxT(), "rem-1")
	if err != nil || st != StateAcked {
		t.Fatalf("final state %v (%v), want ACKED", st, err)
	}
}

// HARDQ B5 literal: an ACK for occurrence N never closes N+1.
func TestAckForWrongOccurrenceNeverCloses(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-1", "first", wall); err != nil {
		t.Fatal(err)
	}
	f := fireDue(t, h)
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	// An ack for a DIFFERENT occurrence id is refused outright.
	if err := h.m.MarkAcked(ctxT(), "occ-rem-1#2"); err == nil {
		t.Fatal("ack for a foreign occurrence accepted")
	}
	if err := h.m.MarkAcked(ctxT(), "occ-other#1"); err == nil {
		t.Fatal("ack for an unknown occurrence accepted")
	}
	if st, _ := h.m.Status(ctxT(), "rem-1"); st != StateDelivered {
		t.Fatalf("wrong-occurrence ack changed state: %v", st)
	}
}

// Illegal lifecycle jumps abort at the append (default-reject table).
func TestIllegalTransitionsAbort(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-1", "x", wall); err != nil {
		t.Fatal(err)
	}
	// Delivered before due: illegal.
	if err := h.m.MarkDelivered(ctxT(), "occ-rem-1#1"); err == nil {
		t.Fatal("DELIVERED before DUE accepted")
	}
	// Acked before delivered: illegal.
	fireDue(t, h)
	if err := h.m.MarkAcked(ctxT(), "occ-rem-1#1"); err == nil {
		t.Fatal("ACKED before DELIVERED accepted")
	}
	// Expire from DELIVERY_PENDING is legal; ack after expiry is not.
	if err := h.m.MarkExpired(ctxT(), "occ-rem-1#1"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkDelivered(ctxT(), "occ-rem-1#1"); err == nil {
		t.Fatal("delivery after expiry accepted")
	}
	if st, _ := h.m.Status(ctxT(), "rem-1"); st != StateExpired {
		t.Fatalf("state %v, want EXPIRED", st)
	}
}

// file_note: the handler appends a line via the AtomicWriter; DONE exists
// only through the VERIFIED postcondition (checker != worker) — a claim
// with an unchanged file grades FAIL, a real execution grades PASS.
func TestFileNoteTaskVerifiedPostcondition(t *testing.T) {
	h := build(t)
	if err := h.m.CreateTask(ctxT(), "task-1", "file_note", `{"note":"buy milk"}`); err != nil {
		t.Fatal(err)
	}
	// MarkDone WITHOUT executing (file unchanged): the postcondition
	// verifier finds no content → Grade FAIL → task stays open.
	if err := h.m.MarkTaskDone(ctxT(), "task-1"); err == nil {
		t.Fatal("done claim with an unchanged file accepted")
	}
	if st, _ := h.m.Status(ctxT(), "task-1"); st == StateDone {
		t.Fatal("unverified claim closed the task")
	}
	// Execute the handler, then MarkDone: postcondition verifies.
	if err := h.m.RunTask(ctxT(), "task-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkTaskDone(ctxT(), "task-1"); err != nil {
		t.Fatalf("verified done refused: %v", err)
	}
	if st, _ := h.m.Status(ctxT(), "task-1"); st != StateDone {
		t.Fatalf("state %v, want DONE", st)
	}
	// The note physically exists in the profile-scoped file.
	b, err := os.ReadFile(filepath.Join(h.dir, "notes.txt"))
	if err != nil || !strings.Contains(string(b), "buy milk") {
		t.Fatalf("note not written: %q %v", b, err)
	}
}

// An empty handler registry is impossible; unknown task kinds fail closed.
func TestRegistryAndKindValidation(t *testing.T) {
	if _, err := NewRegistry(map[string]Handler{}); err == nil {
		t.Fatal("empty handler registry accepted")
	}
	h := build(t)
	if err := h.m.CreateTask(ctxT(), "task-x", "launch_rocket", `{}`); err == nil {
		t.Fatal("unknown task kind accepted")
	}
	if err := h.m.CreateReminder(ctxT(), "", "x", wall); err == nil {
		t.Fatal("empty reminder id accepted")
	}
}

// Restart durability: the whole lifecycle state survives a reopen.
func TestLifecycleSurvivesRestart(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-1", "persist me", wall); err != nil {
		t.Fatal(err)
	}
	f := fireDue(t, h)
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID); err != nil {
		t.Fatal(err)
	}
	h.m.Journal().Close()
	events := Events()
	for n, v := range schedule.Events() {
		events[n] = v
	}
	for _, n := range machine.EventTypes() {
		events[n] = nil
	}
	j, err := journal.Open(h.path, "work", redact.None{}, events, schedule.NewProjection(), NewProjection())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	sched, _ := schedule.New(j, h.clock)
	reg, _ := NewRegistry(map[string]Handler{"file_note": FileNoteHandler(h.dir)})
	m2, err := NewManager(j, sched, reg, h.clock)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := m2.Status(ctxT(), "rem-1"); st != StateDelivered {
		t.Fatalf("state lost across restart: %v", st)
	}
	if err := m2.MarkAcked(ctxT(), f.OccurrenceID); err != nil {
		t.Fatalf("ack after restart refused: %v", err)
	}
}

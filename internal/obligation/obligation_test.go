//go:build linux

// T21 RED table (tasks-P0): ObligationStore (9.6-min) — Reminder
// SCHEDULED→DUE→DELIVERY_PENDING→DELIVERED→ACKED|EXPIRED, writes THROUGH
// the journal; ack correlates to the SAME occurrence (HARDQ B5); MarkDone
// only with checker-graded Evidence (T12); concrete file_note task with a
// verified postcondition; a registry without handlers is impossible.
package obligation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"github.com/MatNik89/nexus/internal/schedule"
	"github.com/MatNik89/nexus/internal/security/redact"
)

type nopAudit struct{}

func (nopAudit) Record(string, contracts.ToolCall) error { return nil }

type nopMW struct{}

func (nopMW) BeforeTool(context.Context, contracts.ToolCall) error { return nil }
func (nopMW) AfterTool(context.Context, contracts.ToolCall, contracts.ToolResult) error {
	return nil
}
func (nopMW) OnError(ctx context.Context, e error) error { return e }

func ctxT() context.Context { return context.Background() }

type harness struct {
	m     *Manager
	sched *schedule.Scheduler
	clock *clockid.Fake
	dir   string
	path  string
}

// build wires the PRODUCTION shape: obligation events sealed to the
// registry kinds, the fire decorator in the scheduler batch, and task
// execution through a real EffectPath (PEP + S7).
func build(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	clock := clockid.NewFake(time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC))
	reg, err := NewRegistry(map[string]Kind{
		"file_note": {Handler: FileNoteHandler(dir), ValidateParams: ValidateFileNoteParams},
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := NewDoneGate()
	events := Events(reg, gate)
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
	auth := s7.NewAuthority(nil, time.Minute)
	lazy := &LazyRunner{}
	m, err := NewManager(j, sched, reg, clock, lazy, auth, dir, gate)
	if err != nil {
		t.Fatal(err)
	}
	pep, err := effectpath.NewPEP(Rules(), nil, effectpath.NewApprovals(nil, time.Minute), nopAudit{}, effectpath.ModeYolo)
	if err != nil {
		t.Fatal(err)
	}
	path, err := effectpath.NewEffectPath(pep, nopMW{},
		effectpath.NewInProcessExecutor(Tools(m)),
		effectpath.NewSandboxedProcessExecutor(nil), auth)
	if err != nil {
		t.Fatal(err)
	}
	lazy.R = path
	sched.SetFireDecorator(m.FireParams)
	return &harness{m: m, sched: sched, clock: clock, dir: dir, path: filepath.Join(dir, "journal.db")}
}

// fireDue sweeps past due: the DECORATED batch moves the obligation to
// DELIVERY_PENDING in the same transaction as the occurrence fire.
func fireDue(t *testing.T, h *harness) schedule.Fired {
	t.Helper()
	h.clock.Advance(3 * time.Hour)
	fired, err := h.sched.Sweep(ctxT())
	if err != nil || len(fired) != 1 {
		t.Fatalf("sweep: %v %v", fired, err)
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
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-1"}); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkAcked(ctxT(), f.OccurrenceID, AckGesture{Source: "test-gesture"}); err != nil {
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
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-1"}); err != nil {
		t.Fatal(err)
	}
	// An ack for a DIFFERENT occurrence id is refused outright.
	if err := h.m.MarkAcked(ctxT(), "occ-rem-1#2", AckGesture{Source: "g"}); err == nil {
		t.Fatal("ack for a foreign occurrence accepted")
	}
	if err := h.m.MarkAcked(ctxT(), "occ-other#1", AckGesture{Source: "g"}); err == nil {
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
	if err := h.m.MarkDelivered(ctxT(), "occ-rem-1#1", DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-x"}); err == nil {
		t.Fatal("DELIVERED before DUE accepted")
	}
	// Acked before delivered: illegal.
	fireDue(t, h)
	if err := h.m.MarkAcked(ctxT(), "occ-rem-1#1", AckGesture{Source: "g"}); err == nil {
		t.Fatal("ACKED before DELIVERED accepted")
	}
	// Expire from DELIVERY_PENDING is legal; ack after expiry is not.
	if err := h.m.MarkExpired(ctxT(), "occ-rem-1#1"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkDelivered(ctxT(), "occ-rem-1#1", DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-x"}); err == nil {
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
	if _, err := NewRegistry(map[string]Kind{}); err == nil {
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
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-1"}); err != nil {
		t.Fatal(err)
	}
	h.m.Journal().Close()
	reg, _ := NewRegistry(map[string]Kind{"file_note": {Handler: FileNoteHandler(h.dir), ValidateParams: ValidateFileNoteParams}})
	gate2 := NewDoneGate()
	events := Events(reg, gate2)
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
	auth2 := s7.NewAuthority(nil, time.Minute)
	m2, err := NewManager(j, sched, reg, h.clock, &LazyRunner{}, auth2, h.dir, gate2)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := m2.Status(ctxT(), "rem-1"); st != StateDelivered {
		t.Fatalf("state lost across restart: %v", st)
	}
	if err := m2.MarkAcked(ctxT(), f.OccurrenceID, AckGesture{Source: "g2"}); err != nil {
		t.Fatalf("ack after restart refused: %v", err)
	}
}

// The kind discriminator is SEALED (Phase-4 codex #13): a crafted
// canonical event with an unregistered kind is rejected at append and can
// never survive replay as an OPEN task.
func TestUnknownKindEventRejectedAtAppend(t *testing.T) {
	h := build(t)
	p, err := h.m.params(EvCreated, createdPayload{ID: "evil-1", Kind: "launch_rocket", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.j.Append(ctxT(), p); err == nil {
		t.Fatal("unknown obligation kind accepted into the canonical stream")
	}
	if _, err := h.m.Status(ctxT(), "evil-1"); err == nil {
		t.Fatal("unknown-kind obligation exists")
	}
}

// Ack grading consumes the PERSISTED delivery instant (Phase-4 codex #2):
// the durable obligation.delivered event's timestamp is what the checker
// sees, and an occurrence without that durable receipt cannot ack.
func TestAckGradesPersistedDeliveryReceipt(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-1", "graded", wall); err != nil {
		t.Fatal(err)
	}
	f := fireDue(t, h)
	deliveredClock := h.clock.Now()
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{Producer: "test-channel", ReceiptID: "rcpt-1"}); err != nil {
		t.Fatal(err)
	}
	at, err := h.m.DeliveredAt(ctxT(), f.OccurrenceID)
	if err != nil || !at.Equal(deliveredClock) {
		t.Fatalf("persisted delivery instant %v != durable event time %v (%v)", at, deliveredClock, err)
	}
	h.clock.Advance(time.Minute)
	if err := h.m.MarkAcked(ctxT(), f.OccurrenceID, AckGesture{Source: "test-gesture"}); err != nil {
		t.Fatal(err)
	}
}

// Task execution is GOVERNED and reconciled (Phase-4 codex #3/#5): the
// intent lands before dispatch, execution goes through the EffectPath,
// and a crashed dispatch (intent without attestation, note already
// written) reconciles WITHOUT appending the note twice.
func TestTaskIntentAndReconcileNeverDuplicates(t *testing.T) {
	h := build(t)
	// Normal governed run: intent + executed events exist.
	if err := h.m.CreateTask(ctxT(), "task-1", "file_note", `{"note":"first"}`); err != nil {
		t.Fatal(err)
	}
	if err := h.m.RunTask(ctxT(), "task-1"); err != nil {
		t.Fatal(err)
	}
	sawIntent, sawExec := false, false
	h.m.j.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case EvTaskIntent:
			sawIntent = true
		case EvTaskExecuted:
			sawExec = true
		}
		return nil
	})
	if !sawIntent || !sawExec {
		t.Fatalf("governed run missing intent/attestation: intent=%v exec=%v", sawIntent, sawExec)
	}
	// Crash emulation for task-2: intent journaled, note WRITTEN, but the
	// attestation never landed. RunTask must RECONCILE, not re-append.
	if err := h.m.CreateTask(ctxT(), "task-2", "file_note", `{"note":"crashy"}`); err != nil {
		t.Fatal(err)
	}
	ip, _ := h.m.params(EvTaskIntent, intentPayload{ID: "task-2", OperationID: "op-crashed"})
	if _, err := h.m.j.Append(ctxT(), ip); err != nil {
		t.Fatal(err)
	}
	handler := FileNoteHandler(h.dir)
	if _, _, err := handler(ctxT(), "task-2", `{"note":"crashy"}`); err != nil {
		t.Fatal(err)
	}
	if err := h.m.RunTask(ctxT(), "task-2"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(h.dir, "notes.txt"))
	if n := strings.Count(string(b), "[task-2] crashy"); n != 1 {
		t.Fatalf("reconcile appended the note %d times (want exactly 1): %q", n, b)
	}
	reconciled := false
	h.m.j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvTaskExecuted {
			var p executedPayload
			jsonUnmarshalT(t, ev.Envelope.Payload, &p)
			if p.ID == "task-2" && p.Reconciled {
				reconciled = true
			}
		}
		return nil
	})
	if !reconciled {
		t.Fatal("crashed dispatch not attested as RECONCILED")
	}
	if err := h.m.MarkTaskDone(ctxT(), "task-2"); err != nil {
		t.Fatalf("reconciled task cannot close: %v", err)
	}
}

func jsonUnmarshalT(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := jsonUnmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

// The projection ENFORCES the execution protocol (Phase-4-r2 codex
// #5/#18): a second concurrent claim loses the CAS; an attestation
// without (or not matching) the claim is forged; DONE without an
// attested marker is forged; an attestation never overwrites.
func TestProjectionEnforcesExecutionProtocol(t *testing.T) {
	h := build(t)
	if err := h.m.CreateTask(ctxT(), "task-p", "file_note", `{"note":"protocol"}`); err != nil {
		t.Fatal(err)
	}
	// Forged attestation with NO claim: aborted.
	ep, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-p", OperationID: "op-forged",
		MarkerLine: "[task-p] protocol"})
	if _, err := h.m.j.Append(ctxT(), ep); err == nil {
		t.Fatal("attestation without a claim accepted")
	}
	// Forged DONE with no attestation: aborted (even WITH the capability).
	dp, _ := h.m.params(EvTaskDone, donePayload{ID: "task-p", MarkerLine: "[task-p] protocol", Verifier: "postcondition-verifier"})
	if _, err := h.m.j.Append(ctxT(), dp); err == nil {
		t.Fatal("done without an attested execution accepted")
	}
	// Claim once; a SECOND claim loses the CAS.
	i1, _ := h.m.params(EvTaskIntent, intentPayload{ID: "task-p", OperationID: "op-one"})
	if _, err := h.m.j.Append(ctxT(), i1); err != nil {
		t.Fatal(err)
	}
	i2, _ := h.m.params(EvTaskIntent, intentPayload{ID: "task-p", OperationID: "op-two"})
	if _, err := h.m.j.Append(ctxT(), i2); err == nil {
		t.Fatal("second concurrent claim accepted (CAS violated)")
	}
	// Attestation under the WRONG operation: aborted.
	ew, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-p", OperationID: "op-two",
		MarkerLine: "[task-p] protocol"})
	if _, err := h.m.j.Append(ctxT(), ew); err == nil {
		t.Fatal("attestation under an unclaimed operation accepted")
	}
	// A forged marker under the VALID claim aborts (the projection
	// recomputes the canonical expectation — r3 codex #10).
	efm, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-p", OperationID: "op-one",
		MarkerLine: "[task-p] forged content"})
	if _, err := h.m.j.Append(ctxT(), efm); err == nil {
		t.Fatal("forged marker accepted under a valid claim")
	}
	// Correct attestation (canonical marker + notes path) lands ONCE.
	eok, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-p", OperationID: "op-one",
		MarkerLine: "[task-p] protocol"})
	if _, err := h.m.j.Append(ctxT(), eok); err != nil {
		t.Fatal(err)
	}
	eagain, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-p", OperationID: "op-one",
		MarkerLine: "[task-p] protocol"})
	if _, err := h.m.j.Append(ctxT(), eagain); err == nil {
		t.Fatal("attestation overwrite accepted")
	}
}

// Kind schema + marker safety at admission (Phase-4-r2 codex #13/#17/#19).
func TestTaskAdmissionSealedAndSafe(t *testing.T) {
	h := build(t)
	if err := h.m.CreateTask(ctxT(), "t-bad", "file_note", `not-json`); err == nil {
		t.Fatal("malformed file_note params accepted")
	}
	if err := h.m.CreateTask(ctxT(), "t-multi", "file_note", `{"note":"a\nb"}`); err == nil {
		t.Fatal("multi-line note accepted")
	}
	for _, badID := range []string{"a] b", "a b", "a\nb", "", strings.Repeat("x", 65)} {
		if err := h.m.CreateTask(ctxT(), badID, "file_note", `{"note":"x"}`); err == nil {
			t.Fatalf("ambiguous task id %q accepted (marker aliasing)", badID)
		}
	}
}

// Delivery requires the CHANNEL's receipt; ack requires the USER gesture;
// grading consumes the persisted producer (Phase-4-r2 codex #2).
func TestDeliveryReceiptAndGestureRequired(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-1", "receipted", wall); err != nil {
		t.Fatal(err)
	}
	f := fireDue(t, h)
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{}); err == nil {
		t.Fatal("delivery without a receipt identity accepted")
	}
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{Producer: "telegram", ReceiptID: "msg-42"}); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkAcked(ctxT(), f.OccurrenceID, AckGesture{}); err == nil {
		t.Fatal("ack without a gesture source accepted")
	}
	if err := h.m.MarkAcked(ctxT(), f.OccurrenceID, AckGesture{Source: "tool:tc-9"}); err != nil {
		t.Fatal(err)
	}
}

// The live-executor lease (Phase-4-r3 codex #2, r4 #3): call A is HELD
// INSIDE the claim→effect window by a blocking handler while call B
// enters with a DISTINCT operation — B must be refused by the lease, and
// the file carries ONE line.
func TestParallelExecutionSingleEffect(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	blockingKind := Kind{
		ValidateParams: ValidateFileNoteParams,
		Handler: func(ctx context.Context, taskID, params string) (string, string, error) {
			close(entered)
			<-release // A is now parked INSIDE the window
			return FileNoteHandler(dir)(ctx, taskID, params)
		},
	}
	h := buildWithKind(t, dir, "file_note", blockingKind)
	if err := h.m.CreateTask(ctxT(), "task-par", "file_note", `{"note":"parallel"}`); err != nil {
		t.Fatal(err)
	}
	aDone := make(chan error, 1)
	go func() { aDone <- h.m.RunTask(ctxT(), "task-par") }()
	<-entered // A holds the window
	bErr := h.m.RunTask(ctxT(), "task-par")
	if bErr == nil || !strings.Contains(bErr.Error(), "in progress") {
		t.Fatalf("second executor entered the claim→effect window: %v", bErr)
	}
	close(release)
	if err := <-aDone; err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if n := strings.Count(string(b), "[task-par] parallel"); n != 1 {
		t.Fatalf("parallel execution wrote the note %d times: %q", n, b)
	}
}

// buildWithKind builds the production-shaped harness around a custom kind.
func buildWithKind(t *testing.T, dir, kindName string, k Kind) *harness {
	t.Helper()
	clock := clockid.NewFake(time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC))
	reg, err := NewRegistry(map[string]Kind{kindName: k})
	if err != nil {
		t.Fatal(err)
	}
	gate := NewDoneGate()
	events := Events(reg, gate)
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
	auth := s7.NewAuthority(nil, time.Minute)
	lazy := &LazyRunner{}
	m, err := NewManager(j, sched, reg, clock, lazy, auth, dir, gate)
	if err != nil {
		t.Fatal(err)
	}
	pep, err := effectpath.NewPEP(Rules(), nil, effectpath.NewApprovals(nil, time.Minute), nopAudit{}, effectpath.ModeYolo)
	if err != nil {
		t.Fatal(err)
	}
	path, err := effectpath.NewEffectPath(pep, nopMW{},
		effectpath.NewInProcessExecutor(Tools(m)),
		effectpath.NewSandboxedProcessExecutor(nil), auth)
	if err != nil {
		t.Fatal(err)
	}
	lazy.R = path
	sched.SetFireDecorator(m.FireParams)
	return &harness{m: m, sched: sched, clock: clock, dir: dir, path: filepath.Join(dir, "journal.db")}
}

// The DONE admission capability (r4 codex #5): the FULL forged chain —
// valid claim, canonical marker, matching done, plausible verifier —
// dies at the capability gate; only the verifying manager holds it.
func TestForgedDoneChainDiesAtCapability(t *testing.T) {
	h := build(t)
	if err := h.m.CreateTask(ctxT(), "task-f", "file_note", `{"note":"forged"}`); err != nil {
		t.Fatal(err)
	}
	i1, _ := h.m.params(EvTaskIntent, intentPayload{ID: "task-f", OperationID: "op-f"})
	if _, err := h.m.j.Append(ctxT(), i1); err != nil {
		t.Fatal(err)
	}
	e1, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-f", OperationID: "op-f",
		MarkerLine: "[task-f] forged"})
	if _, err := h.m.j.Append(ctxT(), e1); err != nil {
		t.Fatal(err)
	}
	// UNARMED done: refused at ADMISSION — and nothing recoverable exists
	// in canonical bytes (the gate is out-of-band).
	d, _ := h.m.params(EvTaskDone, donePayload{ID: "task-f",
		MarkerLine: "[task-f] forged", Verifier: "postcondition-verifier"})
	if _, err := h.m.j.Append(ctxT(), d); err == nil {
		t.Fatal("unarmed forged DONE accepted — no note exists, no checker ran")
	}
	if st, _ := h.m.Status(ctxT(), "task-f"); st == StateDone {
		t.Fatal("forged chain closed the task")
	}
}

// The r5 codex #1 literal: after a LEGITIMATE completion, replaying the
// canonical stream yields NOTHING that can arm a second task's DONE —
// the ticket is single-use and out-of-band.
func TestReplayedDoneDisclosesNoCapability(t *testing.T) {
	h := build(t)
	// Legit completion of task A.
	if err := h.m.CreateTask(ctxT(), "task-a", "file_note", `{"note":"legit"}`); err != nil {
		t.Fatal(err)
	}
	if err := h.m.RunTask(ctxT(), "task-a"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkTaskDone(ctxT(), "task-a"); err != nil {
		t.Fatal(err)
	}
	// Adversary replays the WHOLE canonical stream and HARVESTS the used
	// nonce (it IS serialized — the claim is that it is inert, not
	// hidden; Phase-4-r7 codex #4).
	harvested := ""
	if err := h.m.j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvTaskDone {
			var p donePayload
			jsonUnmarshal(ev.Envelope.Payload, &p)
			harvested = p.Nonce
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if harvested == "" {
		t.Fatal("oracle vacuous: no persisted nonce harvested")
	}
	// Forge task B's full chain with everything replay COULD offer.
	if err := h.m.CreateTask(ctxT(), "task-b", "file_note", `{"note":"victim"}`); err != nil {
		t.Fatal(err)
	}
	i, _ := h.m.params(EvTaskIntent, intentPayload{ID: "task-b", OperationID: "op-b"})
	if _, err := h.m.j.Append(ctxT(), i); err != nil {
		t.Fatal(err)
	}
	e, _ := h.m.params(EvTaskExecuted, executedPayload{ID: "task-b", OperationID: "op-b",
		MarkerLine: "[task-b] victim"})
	if _, err := h.m.j.Append(ctxT(), e); err != nil {
		t.Fatal(err)
	}
	// The HARVESTED nonce is inert for task B — and for a re-done of A.
	d, _ := h.m.params(EvTaskDone, donePayload{ID: "task-b",
		MarkerLine: "[task-b] victim", Verifier: "postcondition-verifier", Nonce: harvested})
	if _, err := h.m.j.Append(ctxT(), d); err == nil {
		t.Fatal("post-replay forged DONE accepted (harvested nonce reusable)")
	}
	if st, _ := h.m.Status(ctxT(), "task-b"); st == StateDone {
		t.Fatal("victim task closed without verification")
	}
}

// The durable ack carries its gesture source — replay knows WHO closed
// the occurrence, and a sourceless canonical ack is rejected (r3 codex #1).
func TestAckGestureDurable(t *testing.T) {
	h := build(t)
	if err := h.m.CreateReminder(ctxT(), "rem-g", "gestured", wall); err != nil {
		t.Fatal(err)
	}
	f := fireDue(t, h)
	if err := h.m.MarkDelivered(ctxT(), f.OccurrenceID, DeliveryReceipt{Producer: "ch", ReceiptID: "r1"}); err != nil {
		t.Fatal(err)
	}
	// A forged sourceless ack event is refused at admission.
	bad, _ := h.m.params(EvAcked, occurrencePayload{OccurrenceID: f.OccurrenceID})
	if _, err := h.m.j.Append(ctxT(), bad); err == nil {
		t.Fatal("sourceless canonical ack accepted")
	}
	if err := h.m.MarkAcked(ctxT(), f.OccurrenceID, AckGesture{Source: "tool:tc-77"}); err != nil {
		t.Fatal(err)
	}
	found := ""
	h.m.j.Replay(0, func(ev journal.Event) error {
		if ev.Envelope.EventType == EvAcked {
			var p ackedPayload
			jsonUnmarshal(ev.Envelope.Payload, &p)
			found = p.Source
		}
		return nil
	})
	if found != "tool:tc-77" {
		t.Fatalf("ack gesture source not durable: %q", found)
	}
}

// The armed window is UNGUESSABLE (Phase-4-r6 codex #1): while a
// legitimate ticket is live, an attacker knowing the durable id+marker —
// but not the 128-bit request nonce — cannot land a done; and a FAILED
// append disarms its ticket (no stranded authority).
func TestArmedWindowRaceAndDisarm(t *testing.T) {
	h := build(t)
	if err := h.m.CreateTask(ctxT(), "task-w", "file_note", `{"note":"window"}`); err != nil {
		t.Fatal(err)
	}
	if err := h.m.RunTask(ctxT(), "task-w"); err != nil {
		t.Fatal(err)
	}
	// Arm a live ticket directly (the manager's own pre-append state).
	nonce, err := h.m.gate.arm("task-w", "[task-w] window")
	if err != nil {
		t.Fatal(err)
	}
	// Attacker in the armed window: right id+marker, no nonce / guessed
	// nonce — refused.
	for name, guess := range map[string]string{"empty": "", "guessed": "00112233445566778899aabbccddeeff"} {
		d, _ := h.m.params(EvTaskDone, donePayload{ID: "task-w",
			MarkerLine: "[task-w] window", Verifier: "postcondition-verifier", Nonce: guess})
		if _, err := h.m.j.Append(ctxT(), d); err == nil {
			t.Fatalf("%s-nonce attacker landed a done inside the armed window", name)
		}
	}
	// The legitimate ticket still works exactly once.
	d, _ := h.m.params(EvTaskDone, donePayload{ID: "task-w",
		MarkerLine: "[task-w] window", Verifier: "postcondition-verifier", Nonce: nonce})
	if _, err := h.m.j.Append(ctxT(), d); err != nil {
		t.Fatalf("legitimate armed append refused: %v", err)
	}
	// DISARM on failure (causal — Phase-4-r7 codex #3): a fully ATTESTED
	// task's MarkTaskDone fails deterministically AFTER arming; the gate
	// must hold ZERO tickets afterwards and the retry self-heals.
	// Removing the disarm calls fails the zero-tickets assertion.
	if err := h.m.CreateTask(ctxT(), "task-x", "file_note", `{"note":"strand"}`); err != nil {
		t.Fatal(err)
	}
	if err := h.m.RunTask(ctxT(), "task-x"); err != nil {
		t.Fatal(err)
	}
	h.m.testPostArmFail = func() error { return fmt.Errorf("injected post-arm failure") }
	if err := h.m.MarkTaskDone(ctxT(), "task-x"); err == nil {
		t.Fatal("injected post-arm failure swallowed")
	}
	h.m.testPostArmFail = nil
	h.m.gate.mu.Lock()
	stranded := len(h.m.gate.tickets)
	h.m.gate.mu.Unlock()
	if stranded != 0 {
		t.Fatalf("%d ticket(s) stranded after a failed request (disarm missing)", stranded)
	}
	// The retry self-heals.
	if err := h.m.MarkTaskDone(ctxT(), "task-x"); err != nil {
		t.Fatalf("retry after disarm refused: %v", err)
	}
}

// The live nonce authorizes EXACTLY its (id, marker) tuple (Phase-4-r7
// codex #4): the REAL armed nonce with a wrong id or wrong marker is
// refused; the exact tuple still lands once. Deleting the tuple
// comparison in consume fails this.
func TestLiveNonceTupleBinding(t *testing.T) {
	h := build(t)
	for _, id := range []string{"task-t1", "task-t2"} {
		if err := h.m.CreateTask(ctxT(), id, "file_note", `{"note":"tuple"}`); err != nil {
			t.Fatal(err)
		}
		if err := h.m.RunTask(ctxT(), id); err != nil {
			t.Fatal(err)
		}
	}
	nonce, err := h.m.gate.arm("task-t1", "[task-t1] tuple")
	if err != nil {
		t.Fatal(err)
	}
	// The REAL live nonce with a WRONG task id (t2's chain is attested).
	dWrongID, _ := h.m.params(EvTaskDone, donePayload{ID: "task-t2",
		MarkerLine: "[task-t2] tuple", Verifier: "postcondition-verifier", Nonce: nonce})
	if _, err := h.m.j.Append(ctxT(), dWrongID); err == nil {
		t.Fatal("live nonce authorized a DIFFERENT task")
	}
	// The ID half in ISOLATION (Phase-4-r10 codex #3): wrong id with the
	// ORIGINAL authorized marker — the marker comparison alone must not
	// mask a deleted id comparison. The append fails either way (the
	// projection also rejects the marker mismatch for t2), so the CAUSAL
	// observable is the ticket: with the id comparison deleted this
	// attempt BURNS it and the exact tuple below can no longer land.
	dWrongIDOnly, _ := h.m.params(EvTaskDone, donePayload{ID: "task-t2",
		MarkerLine: "[task-t1] tuple", Verifier: "postcondition-verifier", Nonce: nonce})
	if _, err := h.m.j.Append(ctxT(), dWrongIDOnly); err == nil {
		t.Fatal("live nonce authorized a different task carrying the original marker")
	}
	// The real live nonce with a WRONG marker.
	dWrongMarker, _ := h.m.params(EvTaskDone, donePayload{ID: "task-t1",
		MarkerLine: "[task-t1] forged-marker", Verifier: "postcondition-verifier", Nonce: nonce})
	if _, err := h.m.j.Append(ctxT(), dWrongMarker); err == nil {
		t.Fatal("live nonce authorized a DIFFERENT marker")
	}
	// Wrong-tuple attempts did NOT consume the ticket: the exact tuple
	// still lands.
	dOK, _ := h.m.params(EvTaskDone, donePayload{ID: "task-t1",
		MarkerLine: "[task-t1] tuple", Verifier: "postcondition-verifier", Nonce: nonce})
	if _, err := h.m.j.Append(ctxT(), dOK); err != nil {
		t.Fatalf("exact tuple refused: %v", err)
	}
}

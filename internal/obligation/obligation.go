//go:build linux

// Package obligation is the 9.6-min ObligationStore (T21, PRD §4.3/§6
// item 3; HARDQ B5; E13): TWO obligation kinds — Reminder
// (SCHEDULED→DUE→DELIVERY_PENDING→DELIVERED→ACKED|EXPIRED) and ONE
// concrete typed Task (file_note). Every mutation writes THROUGH the
// journal (P0.3); lifecycle legality is a default-reject machine table
// applied INSIDE the append transaction. Reminder firing joins the
// scheduler's batch via the FireDecorator — there is NO crash window
// between occurrence-fire and obligation admission (Phase-4 codex #1).
//
// Task execution runs THROUGH the sealed EffectPath (S6.0 decision + S7
// grant — Phase-4 codex #3): the executable is a registered in-process
// TOOL; execution journals an INTENT before dispatch and an attested
// marker-line postcondition after (codex #5); DONE exists only through
// the checker-verified presence of the task's immutable marker line
// (codex #6 — a whole-file hash breaks under later legitimate appends).
package obligation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/checker"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/schedule"
)

// State is the closed obligation lifecycle state.
type State string

const (
	// Reminder states (HARDQ B5).
	StateScheduled       State = "SCHEDULED"
	StateDue             State = "DUE"
	StateDeliveryPending State = "DELIVERY_PENDING"
	StateDelivered       State = "DELIVERED"
	StateAcked           State = "ACKED"
	StateExpired         State = "EXPIRED"
	// Task states.
	StateOpen State = "OPEN"
	StateDone State = "DONE"
)

// Event types (closed).
const (
	EvCreated         = "obligation.created"
	EvDue             = "obligation.due"
	EvDeliveryPending = "obligation.delivery_pending"
	EvDelivered       = "obligation.delivered"
	EvAcked           = "obligation.acked"
	EvExpired         = "obligation.expired"
	EvTaskIntent      = "obligation.task_exec_intent"
	EvTaskExecuted    = "obligation.task_executed"
	EvTaskDone        = "obligation.task_done"
)

// reminderTable is the default-reject lifecycle (applied in the append
// transaction: an illegal jump aborts the event).
func reminderTable() *machine.Table[State] {
	t, err := machine.NewTable("reminder", []machine.Transition[State]{
		{Event: EvDue, From: StateScheduled, To: StateDue},
		{Event: EvDeliveryPending, From: StateDue, To: StateDeliveryPending},
		{Event: EvDelivered, From: StateDeliveryPending, To: StateDelivered},
		{Event: EvAcked, From: StateDelivered, To: StateAcked},
		{Event: EvExpired, From: StateDeliveryPending, To: StateExpired},
		{Event: EvExpired, From: StateDelivered, To: StateExpired},
	})
	if err != nil {
		panic(err) // static table
	}
	return t
}

func taskTable() *machine.Table[State] {
	t, err := machine.NewTable("task", []machine.Transition[State]{
		{Event: EvTaskDone, From: StateOpen, To: StateDone},
	})
	if err != nil {
		panic(err)
	}
	return t
}

// --- payloads (closed) ---

type createdPayload struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // "reminder" | registered task kind
	Body       string `json:"body"`
	TaskParams string `json:"task_params,omitempty"`
}

type occurrencePayload struct {
	OccurrenceID string `json:"occurrence_id"`
}

type intentPayload struct {
	ID          string `json:"id"`
	OperationID string `json:"operation_id"`
}

type executedPayload struct {
	ID           string `json:"id"`
	OperationID  string `json:"operation_id"`
	ExpectedPath string `json:"expected_path"`
	MarkerLine   string `json:"marker_line"`
	Reconciled   bool   `json:"reconciled,omitempty"`
}

type donePayload struct {
	ID string `json:"id"`
}

// Events returns the payload validators — the kind discriminator is
// SEALED against {"reminder"} ∪ the registered task kinds (Phase-4 codex
// #13: a crafted canonical event with an unknown kind must not survive
// replay as an OPEN task).
func Events(taskKinds ...string) map[string]journal.PayloadValidator {
	allowed := map[string]bool{"reminder": true}
	for _, k := range taskKinds {
		allowed[k] = true
	}
	occV := func(raw json.RawMessage) error {
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.OccurrenceID == "" {
			return fmt.Errorf("obligation: occurrence id is required")
		}
		return nil
	}
	return map[string]journal.PayloadValidator{
		EvCreated: func(raw json.RawMessage) error {
			var p createdPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ID == "" || p.Kind == "" {
				return fmt.Errorf("obligation: id and kind are required")
			}
			if !allowed[p.Kind] {
				return fmt.Errorf("obligation: unknown obligation kind (fail closed; sealed set)")
			}
			return nil
		},
		EvDue: occV, EvDeliveryPending: occV, EvAcked: occV, EvExpired: occV,
		EvDelivered: func(raw json.RawMessage) error {
			var p deliveredPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.OccurrenceID == "" || p.Producer == "" || p.ReceiptID == "" {
				return fmt.Errorf("obligation: delivery requires occurrence, producer and receipt ids")
			}
			return nil
		},
		EvTaskIntent: func(raw json.RawMessage) error {
			var p intentPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ID == "" || p.OperationID == "" {
				return fmt.Errorf("obligation: intent requires task and operation ids")
			}
			return nil
		},
		EvTaskExecuted: func(raw json.RawMessage) error {
			var p executedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ID == "" || p.OperationID == "" || p.ExpectedPath == "" || p.MarkerLine == "" {
				return fmt.Errorf("obligation: execution attestation requires operation id, path and marker line")
			}
			return nil
		},
		EvTaskDone: func(raw json.RawMessage) error {
			var p donePayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ID == "" {
				return fmt.Errorf("obligation: done requires an id")
			}
			return nil
		},
	}
}

// Projection folds obligation events with LEGALITY enforced by the
// default-reject tables — the fold and the live path share one truth.
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "obligation" }
func (Projection) Version() int { return 2 } // v2: delivered_at + intent + marker columns

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS obl_obligations (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			body TEXT NOT NULL,
			status TEXT NOT NULL,
			occurrence_id TEXT,
			task_params TEXT NOT NULL DEFAULT '',
			intent_op TEXT NOT NULL DEFAULT '',
			expected_path TEXT NOT NULL DEFAULT '',
			marker_line TEXT NOT NULL DEFAULT '',
			delivered_at INTEGER NOT NULL DEFAULT 0,
			delivered_by TEXT NOT NULL DEFAULT '',
			created INTEGER NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS ux_obl_occurrence
			ON obl_obligations(occurrence_id) WHERE occurrence_id IS NOT NULL;
	`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS obl_obligations`)
	return err
}

func (Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	raw := ev.Envelope.Payload
	step := func(occ, event string, extra string, extraArgs ...any) error {
		rows, err := tx.Query(`SELECT id, status FROM obl_obligations WHERE occurrence_id=?`, occ)
		if err != nil {
			return err
		}
		var id, status string
		found := false
		if rows.Next() {
			found = true
			if err := rows.Scan(&id, &status); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("obligation: no obligation bound to occurrence %s (an ack for N never closes N+1 — fail closed)", occ)
		}
		next, err := reminderTable().Step(State(status), event)
		if err != nil {
			return fmt.Errorf("obligation: %w", err)
		}
		q := `UPDATE obl_obligations SET status=?` + extra + ` WHERE id=?`
		args := append([]any{string(next)}, append(extraArgs, id)...)
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
		return nil
	}
	switch ev.Envelope.EventType {
	case EvCreated:
		var p createdPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		status := StateScheduled
		occ := any(nil)
		if p.Kind != "reminder" {
			status = StateOpen
		} else {
			// The reminder binds to its scheduler occurrence up front:
			// stable derivation, one-shot sequence 1.
			occ = "occ-" + p.ID + "#1"
		}
		if _, err := tx.Exec(`INSERT INTO obl_obligations(id, kind, body, status, occurrence_id, task_params, created)
			VALUES(?,?,?,?,?,?,?)`,
			p.ID, p.Kind, p.Body, string(status), occ, p.TaskParams, int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("obligation: create: %w", err)
		}
	case EvDue:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvDue, "")
	case EvDeliveryPending:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvDeliveryPending, "")
	case EvDelivered:
		var p deliveredPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.Producer == "" || p.ReceiptID == "" {
			return fmt.Errorf("obligation: delivery without its receipt identity (fail closed)")
		}
		// The REAL delivery instant + producer persist (codex #2): ack
		// grading consumes these durable values, never synthesized ones.
		return step(p.OccurrenceID, EvDelivered, ", delivered_at=?, delivered_by=?",
			ev.Envelope.EmittedAt.UnixNano(), p.Producer)
	case EvAcked:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvAcked, "")
	case EvExpired:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvExpired, "")
	case EvTaskIntent:
		var p intentPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// CLAIM via compare-and-set (Phase-4-r2 codex #5/#18): only an
		// OPEN task with NO existing claim accepts an intent — a second
		// concurrent claimant's append aborts here, inside the
		// serialized append transaction.
		res, err := tx.Exec(`UPDATE obl_obligations SET intent_op=? WHERE id=? AND status=? AND intent_op=''`,
			p.OperationID, p.ID, string(StateOpen))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("obligation: CLAIMED — execution already claimed, unknown, or closed (fail closed)")
		}
	case EvTaskExecuted:
		var p executedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// The attestation must MATCH the claimed operation and land once
		// (codex #18: an unclaimed or re-attested execution is forged).
		res, err := tx.Exec(`UPDATE obl_obligations SET expected_path=?, marker_line=?
			WHERE id=? AND status=? AND intent_op=? AND intent_op!='' AND marker_line=''`,
			p.ExpectedPath, p.MarkerLine, p.ID, string(StateOpen), p.OperationID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("obligation: attestation does not match the claimed operation, or the task is already attested/closed (fail closed)")
		}
	case EvTaskDone:
		var p donePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT status FROM obl_obligations WHERE id=?`, p.ID)
		if err != nil {
			return err
		}
		var status string
		found := false
		if rows.Next() {
			found = true
			if err := rows.Scan(&status); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("obligation: done for an unknown task (fail closed)")
		}
		next, err := taskTable().Step(State(status), EvTaskDone)
		if err != nil {
			return fmt.Errorf("obligation: %w", err)
		}
		// DONE requires the attested marker (codex #18): a done event for
		// an unattested task is forged and aborts.
		res, err := tx.Exec(`UPDATE obl_obligations SET status=? WHERE id=? AND marker_line!=''`, string(next), p.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("obligation: done without an attested execution (fail closed)")
		}
	}
	return nil
}

// Handler executes ONE typed task kind and returns the IMMUTABLE marker
// line it wrote (path + exact line) — the CHECKER, not the handler,
// decides done (E13).
type Handler func(ctx context.Context, taskID, params string) (path, markerLine string, err error)

// fileNoteMu serializes note appends (Phase-4 codex #6: concurrent
// read-modify-replace would lose one line).
var fileNoteMu sync.Mutex

// FileNoteHandler appends "[<task-id>] <note>" via the T05 AtomicWriter —
// the marker line is the task's stable, per-task postcondition artifact.
func FileNoteHandler(dir string) Handler {
	return func(ctx context.Context, taskID, params string) (string, string, error) {
		var p struct {
			Note string `json:"note"`
		}
		if err := json.Unmarshal([]byte(params), &p); err != nil || p.Note == "" {
			return "", "", fmt.Errorf("file_note: a non-empty note is required (fail closed)")
		}
		if strings.ContainsAny(p.Note, "\n\r") {
			return "", "", fmt.Errorf("file_note: a note is one line (fail closed)")
		}
		line := "[" + taskID + "] " + p.Note
		path := filepath.Join(dir, "notes.txt")
		fileNoteMu.Lock()
		defer fileNoteMu.Unlock()
		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return "", "", err
		}
		if err := atomicwrite.Write(path, append(existing, []byte(line+"\n")...), 0o600); err != nil {
			return "", "", err
		}
		return path, line, nil
	}
}

// fileContainsLine is the postcondition verifier (checker != worker: it
// re-reads the REAL file).
func fileContainsLine(path, line string) (bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l == line {
			return true, nil
		}
	}
	return false, nil
}

// Kind bundles a task kind's handler with its ADMISSION validator — the
// kind schema is sealed at creation, not discovered at execution
// (Phase-4-r2 codex #13).
type Kind struct {
	Handler        Handler
	ValidateParams func(params string) error
}

// Registry is the CLOSED task-kind set; it can never be empty.
type Registry struct {
	kinds map[string]Kind
}

func NewRegistry(kinds map[string]Kind) (*Registry, error) {
	if len(kinds) == 0 {
		return nil, fmt.Errorf("obligation: a registry without handlers is impossible (fail closed)")
	}
	cp := make(map[string]Kind, len(kinds))
	for k, v := range kinds {
		if k == "" || v.Handler == nil || v.ValidateParams == nil {
			return nil, fmt.Errorf("obligation: invalid kind registration (fail closed)")
		}
		cp[k] = v
	}
	return &Registry{kinds: cp}, nil
}

// Kinds lists the registered task kinds (seals the event validator).
func (r *Registry) Kinds() []string {
	out := make([]string, 0, len(r.kinds))
	for k := range r.kinds {
		out = append(out, k)
	}
	return out
}

// ValidateFileNoteParams is the file_note admission schema.
func ValidateFileNoteParams(params string) error {
	var p struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(params), &p); err != nil || p.Note == "" {
		return fmt.Errorf("a JSON object with a non-empty note is required")
	}
	if strings.ContainsAny(p.Note, "\n\r") {
		return fmt.Errorf("a note is one line")
	}
	return nil
}

// EffectRunner is the sealed execution seam (the EffectPath — S6.0
// decision, S7 grant, receipt discipline). Task handlers NEVER run
// outside it (Phase-4 codex #3).
type EffectRunner interface {
	RunTool(ctx context.Context, call contracts.ToolCall, grant s7min.Grant) (contracts.ToolResult, error)
}

// Manager is the obligation facade over the profile's ONE journal.
type Manager struct {
	j        *journal.Journal
	sched    *schedule.Scheduler
	reg      *Registry
	clock    clockid.Clock
	runner   EffectRunner
	auth     *s7min.Authority
	notesDir string // IMMUTABLE profile-bound reconcile root (B3 — no globals)
	seq      atomic.Uint64
}

func NewManager(j *journal.Journal, s *schedule.Scheduler, r *Registry, c clockid.Clock,
	runner EffectRunner, auth *s7min.Authority, notesDir string) (*Manager, error) {
	if j == nil || s == nil || r == nil || c == nil || runner == nil || auth == nil || notesDir == "" {
		return nil, fmt.Errorf("obligation: journal, scheduler, registry, clock, effect runner, S7 authority and notes dir are required (fail closed)")
	}
	return &Manager{j: j, sched: s, reg: r, clock: c, runner: runner, auth: auth, notesDir: notesDir}, nil
}

func (m *Manager) Journal() *journal.Journal { return m.j }

func (m *Manager) params(eventType string, payload any) (contracts.EnvelopeParams, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contracts.EnvelopeParams{}, err
	}
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-obl-%d-%d-%d", os.Getpid(), m.clock.Now().UnixNano(), m.seq.Add(1))),
		EventType: eventType, RunID: "run-obligation", EmittedAt: m.clock.Now(),
		ActorType: contracts.ActorSystem, ActorID: "obligation", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: m.j.Profile(), AttemptNo: 1,
		Payload: raw, PayloadHash: "recomputed",
	}, nil
}

// FireParams is the schedule.FireDecorator: the DUE + DELIVERY_PENDING
// transitions join the scheduler's fire batch — one transaction, no
// crash window (Phase-4 codex #1).
func (m *Manager) FireParams(f schedule.Fired) ([]contracts.EnvelopeParams, error) {
	dueP, err := m.params(EvDue, occurrencePayload{OccurrenceID: f.OccurrenceID})
	if err != nil {
		return nil, err
	}
	pendP, err := m.params(EvDeliveryPending, occurrencePayload{OccurrenceID: f.OccurrenceID})
	if err != nil {
		return nil, err
	}
	return []contracts.EnvelopeParams{dueP, pendP}, nil
}

// CreateReminder creates the schedule AND the obligation in ONE atomic
// batch — a reminder can never exist without its schedule or vice versa.
func (m *Manager) CreateReminder(ctx context.Context, id, body string, w schedule.WallTime) error {
	schedP, err := m.sched.CreatedParams(id, body, w)
	if err != nil {
		return err
	}
	oblP, err := m.params(EvCreated, createdPayload{ID: id, Kind: "reminder", Body: body})
	if err != nil {
		return err
	}
	_, err = m.j.AppendBatch(ctx, []contracts.EnvelopeParams{schedP, oblP})
	return err
}

// taskIDOK: a closed id alphabet keeps the '[id] note' marker encoding
// UNAMBIGUOUS (Phase-4-r2 codex #17: an id containing ']' or whitespace
// could alias another task's marker).
func taskIDOK(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// CreateTask creates one typed task; unknown kinds and malformed params
// fail closed against the registry's PER-KIND validator (codex #13), and
// params carrying a known secret are refused before the journal (codex
// #19 — the receipt must describe the exact approved bytes).
func (m *Manager) CreateTask(ctx context.Context, id, kind, taskParams string) error {
	if !taskIDOK(id) {
		return fmt.Errorf("obligation: task id must be a short [A-Za-z0-9_-] slug (fail closed)")
	}
	k, ok := m.reg.kinds[kind]
	if !ok {
		return fmt.Errorf("obligation: unknown task kind %q (fail closed)", kind)
	}
	if err := k.ValidateParams(taskParams); err != nil {
		return fmt.Errorf("obligation: %s params: %w", kind, err)
	}
	payload := createdPayload{ID: id, Kind: kind, Body: kind, TaskParams: taskParams}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rewrites, err := m.j.RedactorRewrites(raw)
	if err != nil {
		return err
	}
	if rewrites {
		return fmt.Errorf("obligation: task params contain a known secret reference — refused (fail closed)")
	}
	p, err := m.params(EvCreated, payload)
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// MarkDue exists for callers OUTSIDE the scheduler batch (tests, manual
// recovery); production firing uses FireParams in the scheduler batch.
func (m *Manager) MarkDue(ctx context.Context, f schedule.Fired) error {
	batch, err := m.FireParams(f)
	if err != nil {
		return err
	}
	_, err = m.j.AppendBatch(ctx, batch)
	return err
}

func (m *Manager) occEvent(ctx context.Context, eventType, occ string) error {
	p, err := m.params(eventType, occurrencePayload{OccurrenceID: occ})
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// DeliveryReceipt is the transport's own attestation of a delivery —
// the producer identity and receipt id come from the CHANNEL, never from
// this store (Phase-4-r2 codex #2). topknot ceiling: cryptographic
// transport authentication of the producer arrives with the T22 channel
// owner; until then the receipt fields are declared and persisted.
type DeliveryReceipt struct {
	Producer  string `json:"producer"`
	ReceiptID string `json:"receipt_id"`
}

type deliveredPayload struct {
	OccurrenceID string `json:"occurrence_id"`
	Producer     string `json:"producer"`
	ReceiptID    string `json:"receipt_id"`
}

// MarkDelivered records the durable delivery receipt for the occurrence;
// a receipt without a producer and id is refused.
func (m *Manager) MarkDelivered(ctx context.Context, occ string, r DeliveryReceipt) error {
	if r.Producer == "" || r.ReceiptID == "" {
		return fmt.Errorf("obligation: a delivery receipt requires its producer and receipt id (fail closed)")
	}
	p, err := m.params(EvDelivered, deliveredPayload{OccurrenceID: occ, Producer: r.Producer, ReceiptID: r.ReceiptID})
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// AckGesture identifies the USER action closing the occurrence (a
// reminder_ack tool call id from an authenticated same-UID session; the
// T22 channel supplies its own gesture identity).
type AckGesture struct {
	Source string `json:"source"`
}

// MarkAcked closes the occurrence. The checker grades REAL artifacts
// (Phase-4 codex #2 / r2 #2): the delivery evidence carries the
// PERSISTED producer + delivered_at from the durable
// obligation.delivered event, and the ack carries the identified user
// gesture — neither is synthesized by this method.
func (m *Manager) MarkAcked(ctx context.Context, occ string, g AckGesture) error {
	if g.Source == "" {
		return fmt.Errorf("obligation: an ack requires its user-gesture source (fail closed)")
	}
	st, deliveredAt, deliveredBy, err := m.occurrenceDelivery(ctx, occ)
	if err != nil {
		return err
	}
	if deliveredAt.IsZero() || deliveredBy == "" {
		return fmt.Errorf("obligation: ack refused — no durable delivery receipt for occurrence %s (B5)", occ)
	}
	contract := checker.AcceptanceContract{ID: "occ:" + occ, Worker: "reminder-channel",
		Criteria: []checker.Criterion{{DeliveredAndAcked: &occ}}}
	verdict, err := checker.Grade(contract, []checker.Evidence{
		{Contract: "occ:" + occ, Producer: deliveredBy,
			Delivery: &checker.DeliveryEvidence{OccurrenceID: occ, DeliveredAt: deliveredAt}},
		{Contract: "occ:" + occ, Producer: g.Source,
			Ack: &checker.AckEvidence{OccurrenceID: occ, AckAt: m.clock.Now()}},
	})
	if err != nil {
		return fmt.Errorf("obligation: ack grading: %w", err)
	}
	if !verdict.Pass || st != StateDelivered {
		return fmt.Errorf("obligation: ack refused — occurrence %s is not in DELIVERED (B5)", occ)
	}
	return m.occEvent(ctx, EvAcked, occ)
}

// MarkExpired ends an undelivered/unacked occurrence.
func (m *Manager) MarkExpired(ctx context.Context, occ string) error {
	return m.occEvent(ctx, EvExpired, occ)
}

// taskToolID is the sealed tool the EffectPath dispatches for task
// execution (registered by Tools()).
const taskToolID = "task_file_note"

// executeGoverned is the ONE task-execution sequence, invoked ONLY from
// inside the sealed task tool (both the model path and RunTask land
// here): intent journaled BEFORE the effect; an earlier intent without
// attestation RECONCILES against the real file before any retry (the
// note is never appended twice — codex #5); the attestation lands after.
func (m *Manager) executeGoverned(ctx context.Context, id, opSuffix string) (string, string, error) {
	kind, taskParams, intentOp, markerLine, _, status, err := m.taskRow(ctx, id)
	if err != nil {
		return "", "", err
	}
	if status != StateOpen {
		return "", "", fmt.Errorf("obligation: task %s is not OPEN (fail closed)", id)
	}
	k, ok := m.reg.kinds[kind]
	if !ok || k.Handler == nil {
		return "", "", fmt.Errorf("obligation: no handler for kind %q (fail closed)", kind)
	}
	h := k.Handler
	if markerLine != "" {
		return "", "", fmt.Errorf("obligation: task %s already has an attested execution", id)
	}
	op := "op-" + opSuffix
	if intentOp != "" {
		// A prior CLAIM exists. Reconcile against the real file under the
		// STORED operation id — never a fresh one (codex #18).
		op = intentOp
		if probableLine, perr := m.probableMarker(id, taskParams); perr == nil {
			probablePath := filepath.Join(m.notesDir, "notes.txt")
			present, ferr := fileContainsLine(probablePath, probableLine)
			if ferr == nil && present {
				if err := m.attestExecution(ctx, id, op, probablePath, probableLine, true); err != nil {
					return "", "", err
				}
				return probablePath, probableLine, nil
			}
		}
		// Not present: the append never happened; dispatch under the
		// SAME claimed operation is safe.
	} else {
		// CLAIM via the projection CAS: a concurrent claimant loses here.
		intentP, err := m.params(EvTaskIntent, intentPayload{ID: id, OperationID: op})
		if err != nil {
			return "", "", err
		}
		if _, err := m.j.Append(ctx, intentP); err != nil {
			return "", "", fmt.Errorf("obligation: claim: %w", err)
		}
	}
	path, line, err := h(ctx, id, taskParams)
	if err != nil {
		return "", "", fmt.Errorf("obligation: handler: %w", err)
	}
	if err := m.attestExecution(ctx, id, op, path, line, false); err != nil {
		return "", "", err
	}
	return path, line, nil
}

// RunTask dispatches the task's sealed tool THROUGH the EffectPath
// (S6.0 decision + S7 grant — codex #3); the tool owns the
// intent/reconcile/attest sequence, so the model path and this
// programmatic path share ONE discipline.
func (m *Manager) RunTask(ctx context.Context, id string) error {
	op := contracts.OperationID(fmt.Sprintf("task-%s-%d", id, m.clock.Now().UnixNano()))
	args, err := json.Marshal(map[string]string{"task_id": id})
	if err != nil {
		return err
	}
	idem := "idem-" + string(op)
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: contracts.ToolCallID("tc-" + string(op)), ToolID: taskToolID,
		Arguments: args, ArgsSchemaHash: "task_file_note.v1",
		Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: m.clock.Now().Add(2 * time.Minute), AttemptNo: 1,
		IdempotencyKey: &idem, ProfileID: m.j.Profile(),
	})
	if err != nil {
		return err
	}
	grant, err := m.auth.Issue(op, effectpath.ToolTarget(call))
	if err != nil {
		return err
	}
	if _, err := m.runner.RunTool(ctx, call, grant); err != nil {
		return fmt.Errorf("obligation: governed execution: %w", err)
	}
	return nil
}

// probableMarker recomputes the deterministic marker for reconcile.
func (m *Manager) probableMarker(id, taskParams string) (string, error) {
	var p struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(taskParams), &p); err != nil || p.Note == "" {
		return "", fmt.Errorf("obligation: unreconstructable marker")
	}
	return "[" + id + "] " + p.Note, nil
}

func (m *Manager) attestExecution(ctx context.Context, id, op, path, line string, reconciled bool) error {
	p, err := m.params(EvTaskExecuted, executedPayload{ID: id, OperationID: op, ExpectedPath: path, MarkerLine: line, Reconciled: reconciled})
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// MarkTaskDone grades the POSTCONDITION with the T12 checker: the
// verifier re-reads the real file (checker != worker) and the task's
// IMMUTABLE marker line must be present. A claim with an unchanged file
// grades FAIL and the task stays open. topknot ceiling: the gap between
// the verifier read and the DONE append tolerates external truncation
// (replay determinism forbids filesystem reads inside the projection
// transaction); the marker artifact itself is append-stable.
func (m *Manager) MarkTaskDone(ctx context.Context, id string) error {
	_, _, _, markerLine, expPath, status, err := m.taskRow(ctx, id)
	if err != nil {
		return err
	}
	if status != StateOpen {
		return fmt.Errorf("obligation: task %s is not OPEN", id)
	}
	if markerLine == "" || expPath == "" {
		return fmt.Errorf("obligation: done refused — the task has no attested execution (E13: no evidence, no done)")
	}
	present, err := fileContainsLine(expPath, markerLine)
	if err != nil {
		return fmt.Errorf("obligation: postcondition verify: %w", err)
	}
	contract := checker.AcceptanceContract{ID: "task:" + id, Worker: "file_note-handler",
		Criteria: []checker.Criterion{{FileContainsLine: &checker.FileLineCriterion{Path: expPath, Line: markerLine}}}}
	verdict, err := checker.Grade(contract, []checker.Evidence{
		{Contract: "task:" + id, Producer: "postcondition-verifier",
			FileLine: &checker.FileLineEvidence{Path: expPath, Line: markerLine, Present: present}},
	})
	if err != nil {
		return fmt.Errorf("obligation: done grading: %w", err)
	}
	if !verdict.Pass {
		return fmt.Errorf("obligation: done refused — the postcondition does not verify (the task's marker line is absent)")
	}
	p, err := m.params(EvTaskDone, donePayload{ID: id})
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// --- reads (guarded projection queries) ---

func (m *Manager) Status(ctx context.Context, id string) (State, error) {
	rows, err := m.j.QueryProjection(ctx, `SELECT status FROM obl_obligations WHERE id=?`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", fmt.Errorf("obligation: unknown obligation %q", id)
	}
	var s string
	if err := rows.Scan(&s); err != nil {
		return "", err
	}
	return State(s), rows.Err()
}

// DeliveredAt exposes the persisted durable delivery instant.
func (m *Manager) DeliveredAt(ctx context.Context, occ string) (time.Time, error) {
	_, at, err := m.occurrenceState(ctx, occ)
	return at, err
}

func (m *Manager) occurrenceState(ctx context.Context, occ string) (State, time.Time, error) {
	st, at, _, err := m.occurrenceDelivery(ctx, occ)
	return st, at, err
}

func (m *Manager) occurrenceDelivery(ctx context.Context, occ string) (State, time.Time, string, error) {
	rows, err := m.j.QueryProjection(ctx, `SELECT status, delivered_at, delivered_by FROM obl_obligations WHERE occurrence_id=?`, occ)
	if err != nil {
		return "", time.Time{}, "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", time.Time{}, "", fmt.Errorf("obligation: no obligation bound to occurrence %q", occ)
	}
	var s, by string
	var at int64
	if err := rows.Scan(&s, &at, &by); err != nil {
		return "", time.Time{}, "", err
	}
	var t time.Time
	if at > 0 {
		t = time.Unix(0, at).UTC()
	}
	return State(s), t, by, rows.Err()
}

func (m *Manager) taskRow(ctx context.Context, id string) (kind, taskParams, intentOp, markerLine, expPath string, status State, err error) {
	rows, qerr := m.j.QueryProjection(ctx,
		`SELECT kind, task_params, intent_op, marker_line, expected_path, status FROM obl_obligations WHERE id=?`, id)
	if qerr != nil {
		return "", "", "", "", "", "", qerr
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", "", "", "", "", fmt.Errorf("obligation: unknown task %q", id)
	}
	var st string
	if err := rows.Scan(&kind, &taskParams, &intentOp, &markerLine, &expPath, &st); err != nil {
		return "", "", "", "", "", "", err
	}
	return kind, taskParams, intentOp, markerLine, expPath, State(st), rows.Err()
}

// LazyRunner breaks the composition cycle (tools need the manager, the
// EffectPath needs the tools, the manager needs the EffectPath): wire the
// real runner after construction; dispatch before wiring fails closed.
type LazyRunner struct {
	R EffectRunner
}

func (l *LazyRunner) RunTool(ctx context.Context, call contracts.ToolCall, grant s7min.Grant) (contracts.ToolResult, error) {
	if l.R == nil {
		return contracts.ToolResult{}, fmt.Errorf("obligation: effect runner not wired yet (fail closed)")
	}
	return l.R.RunTool(ctx, call, grant)
}

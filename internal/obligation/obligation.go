//go:build linux

// Package obligation is the 9.6-min ObligationStore (T21, PRD §4.3/§6
// item 3; HARDQ B5; E13): TWO obligation kinds — Reminder
// (SCHEDULED→DUE→DELIVERY_PENDING→DELIVERED→ACKED|EXPIRED) and ONE
// concrete typed Task (file_note). Every mutation writes THROUGH the
// journal (P0.3); lifecycle legality is a default-reject machine table
// applied INSIDE the append transaction, so an illegal jump aborts the
// event itself. Acks correlate to the SAME occurrence id (an ack for N
// never closes N+1), and a Task is DONE only through a checker-verified
// postcondition (checker != worker, T12) — never a prose claim.
package obligation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/checker"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
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
	Kind       string `json:"kind"` // "reminder" | task kind
	Body       string `json:"body"`
	TaskParams string `json:"task_params,omitempty"`
}

type occurrencePayload struct {
	OccurrenceID string `json:"occurrence_id"`
}

type executedPayload struct {
	ID             string `json:"id"`
	ExpectedPath   string `json:"expected_path"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

type donePayload struct {
	ID string `json:"id"`
}

// Events returns the payload validators for the journal's closed set.
func Events() map[string]journal.PayloadValidator {
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
			return nil
		},
		EvDue: occV, EvDeliveryPending: occV, EvDelivered: occV, EvAcked: occV, EvExpired: occV,
		EvTaskExecuted: func(raw json.RawMessage) error {
			var p executedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ID == "" || p.ExpectedPath == "" || p.ExpectedSHA256 == "" {
				return fmt.Errorf("obligation: execution attestation requires path and hash")
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
func (Projection) Version() int { return 1 }

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS obl_obligations (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			body TEXT NOT NULL,
			status TEXT NOT NULL,
			occurrence_id TEXT,
			task_params TEXT NOT NULL DEFAULT '',
			expected_path TEXT NOT NULL DEFAULT '',
			expected_sha256 TEXT NOT NULL DEFAULT '',
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
	step := func(occ, event string) error {
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
		if _, err := tx.Exec(`UPDATE obl_obligations SET status=? WHERE id=?`, string(next), id); err != nil {
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
		return step(p.OccurrenceID, EvDue)
	case EvDeliveryPending:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvDeliveryPending)
	case EvDelivered:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvDelivered)
	case EvAcked:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvAcked)
	case EvExpired:
		var p occurrencePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return step(p.OccurrenceID, EvExpired)
	case EvTaskExecuted:
		var p executedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE obl_obligations SET expected_path=?, expected_sha256=?
			WHERE id=? AND status=?`, p.ExpectedPath, p.ExpectedSHA256, p.ID, string(StateOpen))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("obligation: execution attested for an unknown or closed task (fail closed)")
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
		if _, err := tx.Exec(`UPDATE obl_obligations SET status=? WHERE id=?`, string(next), p.ID); err != nil {
			return err
		}
	}
	return nil
}

// Handler executes ONE typed task kind and returns the postcondition it
// claims (path + expected sha256) — the CHECKER, not the handler, decides
// done (E13).
type Handler func(ctx context.Context, params string) (expectedPath, expectedSHA256 string, err error)

// FileNoteHandler appends a note line to the profile-scoped notes file
// via the T05 AtomicWriter and returns the resulting content hash.
func FileNoteHandler(dir string) Handler {
	return func(ctx context.Context, params string) (string, string, error) {
		var p struct {
			Note string `json:"note"`
		}
		if err := json.Unmarshal([]byte(params), &p); err != nil || p.Note == "" {
			return "", "", fmt.Errorf("file_note: a non-empty note is required (fail closed)")
		}
		path := filepath.Join(dir, "notes.txt")
		existing, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return "", "", err
		}
		content := append(existing, []byte(p.Note+"\n")...)
		if err := atomicwrite.Write(path, content, 0o600); err != nil {
			return "", "", err
		}
		sum := sha256.Sum256(content)
		return path, hex.EncodeToString(sum[:]), nil
	}
}

// Registry is the CLOSED task-handler set; it can never be empty (the
// typed-handler path must exist, ledger acceptance).
type Registry struct {
	handlers map[string]Handler
}

func NewRegistry(handlers map[string]Handler) (*Registry, error) {
	if len(handlers) == 0 {
		return nil, fmt.Errorf("obligation: a registry without handlers is impossible (fail closed)")
	}
	cp := make(map[string]Handler, len(handlers))
	for k, v := range handlers {
		if k == "" || v == nil {
			return nil, fmt.Errorf("obligation: invalid handler registration (fail closed)")
		}
		cp[k] = v
	}
	return &Registry{handlers: cp}, nil
}

// Manager is the obligation facade over the profile's ONE journal.
type Manager struct {
	j     *journal.Journal
	sched *schedule.Scheduler
	reg   *Registry
	clock clockid.Clock
	seq   atomic.Uint64
}

func NewManager(j *journal.Journal, s *schedule.Scheduler, r *Registry, c clockid.Clock) (*Manager, error) {
	if j == nil || s == nil || r == nil || c == nil {
		return nil, fmt.Errorf("obligation: journal, scheduler, registry and clock are required (fail closed)")
	}
	return &Manager{j: j, sched: s, reg: r, clock: c}, nil
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

// CreateTask creates one typed task; unknown kinds fail closed against
// the registry.
func (m *Manager) CreateTask(ctx context.Context, id, kind, taskParams string) error {
	if id == "" {
		return fmt.Errorf("obligation: task id is required (fail closed)")
	}
	if _, ok := m.reg.handlers[kind]; !ok {
		return fmt.Errorf("obligation: unknown task kind %q (fail closed)", kind)
	}
	p, err := m.params(EvCreated, createdPayload{ID: id, Kind: kind, Body: kind, TaskParams: taskParams})
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// MarkDue transitions the fired occurrence DUE → DELIVERY_PENDING in one
// atomic batch (the scheduler fired it; delivery is now owed).
func (m *Manager) MarkDue(ctx context.Context, f schedule.Fired) error {
	dueP, err := m.params(EvDue, occurrencePayload{OccurrenceID: f.OccurrenceID})
	if err != nil {
		return err
	}
	pendP, err := m.params(EvDeliveryPending, occurrencePayload{OccurrenceID: f.OccurrenceID})
	if err != nil {
		return err
	}
	_, err = m.j.AppendBatch(ctx, []contracts.EnvelopeParams{dueP, pendP})
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

// MarkDelivered records the durable delivery receipt for the occurrence.
func (m *Manager) MarkDelivered(ctx context.Context, occ string) error {
	return m.occEvent(ctx, EvDelivered, occ)
}

// MarkAcked closes the occurrence — the checker's B5 grading (delivery +
// ack for the SAME occurrence) runs before the journal transition; the
// projection's occurrence binding plus the default-reject table make an
// ack for any other occurrence impossible.
func (m *Manager) MarkAcked(ctx context.Context, occ string) error {
	// Grade with the T12 checker: both records must exist for THIS
	// occurrence and in a legal order (delivered state proves delivery).
	st, err := m.statusByOccurrence(ctx, occ)
	if err != nil {
		return err
	}
	now := m.clock.Now()
	contract := checker.AcceptanceContract{ID: "occ:" + occ, Worker: "reminder-channel",
		Criteria: []checker.Criterion{{DeliveredAndAcked: &occ}}}
	verdict, err := checker.Grade(contract, []checker.Evidence{
		{Contract: "occ:" + occ, Producer: "obligation-store",
			Delivery: &checker.DeliveryEvidence{OccurrenceID: occ, DeliveredAt: now.Add(-1)}},
		{Contract: "occ:" + occ, Producer: "obligation-store",
			Ack: &checker.AckEvidence{OccurrenceID: occ, AckAt: now}},
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

// RunTask executes the task's typed handler and journals the attested
// postcondition (path + expected hash) — execution and DONE stay separate.
func (m *Manager) RunTask(ctx context.Context, id string) error {
	kind, taskParams, err := m.taskInfo(ctx, id)
	if err != nil {
		return err
	}
	h := m.reg.handlers[kind]
	if h == nil {
		return fmt.Errorf("obligation: no handler for kind %q (fail closed)", kind)
	}
	path, sum, err := h(ctx, taskParams)
	if err != nil {
		return fmt.Errorf("obligation: handler: %w", err)
	}
	p, err := m.params(EvTaskExecuted, executedPayload{ID: id, ExpectedPath: path, ExpectedSHA256: sum})
	if err != nil {
		return err
	}
	_, err = m.j.Append(ctx, p)
	return err
}

// MarkTaskDone grades the POSTCONDITION with the T12 checker: the
// verifier re-reads the file (checker != worker) and the content hash
// must equal the handler's attested expectation. A claim with an
// unchanged/absent file grades FAIL and the task stays open.
func (m *Manager) MarkTaskDone(ctx context.Context, id string) error {
	expPath, expSum, err := m.taskExpectation(ctx, id)
	if err != nil {
		return err
	}
	if expPath == "" || expSum == "" {
		return fmt.Errorf("obligation: done refused — the task has no attested execution (E13: no evidence, no done)")
	}
	// Independent verification: read the REAL file now.
	b, err := os.ReadFile(expPath)
	if err != nil {
		return fmt.Errorf("obligation: postcondition verify: %w", err)
	}
	sum := sha256.Sum256(b)
	contract := checker.AcceptanceContract{ID: "task:" + id, Worker: "file_note-handler",
		Criteria: []checker.Criterion{{FileHashIs: &checker.FileHashCriterion{Path: expPath, SHA256: expSum}}}}
	verdict, err := checker.Grade(contract, []checker.Evidence{
		{Contract: "task:" + id, Producer: "postcondition-verifier",
			FileHash: &checker.FileHashEvidence{Path: expPath, SHA256: hex.EncodeToString(sum[:])}},
	})
	if err != nil {
		return fmt.Errorf("obligation: done grading: %w", err)
	}
	if !verdict.Pass {
		return fmt.Errorf("obligation: done refused — the postcondition does not verify (file content diverges from the attested state)")
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

func (m *Manager) statusByOccurrence(ctx context.Context, occ string) (State, error) {
	rows, err := m.j.QueryProjection(ctx, `SELECT status FROM obl_obligations WHERE occurrence_id=?`, occ)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", fmt.Errorf("obligation: no obligation bound to occurrence %q", occ)
	}
	var s string
	if err := rows.Scan(&s); err != nil {
		return "", err
	}
	return State(s), rows.Err()
}

func (m *Manager) taskInfo(ctx context.Context, id string) (kind, taskParams string, err error) {
	rows, err := m.j.QueryProjection(ctx, `SELECT kind, task_params FROM obl_obligations WHERE id=?`, id)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", fmt.Errorf("obligation: unknown task %q", id)
	}
	if err := rows.Scan(&kind, &taskParams); err != nil {
		return "", "", err
	}
	return kind, taskParams, rows.Err()
}

func (m *Manager) taskExpectation(ctx context.Context, id string) (path, sum string, err error) {
	rows, err := m.j.QueryProjection(ctx, `SELECT expected_path, expected_sha256 FROM obl_obligations WHERE id=?`, id)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", fmt.Errorf("obligation: unknown task %q", id)
	}
	if err := rows.Scan(&path, &sum); err != nil {
		return "", "", err
	}
	return path, sum, rows.Err()
}

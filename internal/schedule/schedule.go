//go:build linux

// Package schedule is the 3.6-min durable one-shot scheduler (T20, PRD §6
// item 3; HARDQ B1/B7/C8): a reminder is a persisted WALL TIME in an IANA
// zone with dst=ONCE_FIRST (an autumn-fold wall time fires at its FIRST
// occurrence; a spring-gap wall time normalizes forward) and
// missed_run=COALESCE (a due instant missed while down fires exactly once
// on the next sweep, marked OVERDUE). Every fire executes the concrete
// HARDQ B7 recipe occurrence+run-admission: the occurrence event and the
// run's created+admitted events land in ONE journal batch transaction —
// replay can never show a fired occurrence without its admitted run.
//
// topknot ceiling: wake catch-up is the startup + periodic sweep (a tick
// after resume observes the overdue instant); a D-Bus PrepareForSleep
// hook is deferred until a consumer proves the tick insufficient.
// Recurrence (cron-like) belongs to the full P2.6 engine.
package schedule

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/atomicwrite"
	"github.com/MatNik89/nexus/internal/foundation/clockid"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
)

// Event types (closed; registered into the journal's set).
const (
	EvScheduleCreated = "schedule.created"
	EvOccurrenceFired = "schedule.occurrence_fired"
)

// overdueGrace: a fire later than this past due carries the OVERDUE notice.
const overdueGrace = 60 * time.Second

// WallTime is the user's LOCAL time intent, persisted verbatim (B1: the
// wall time + zone is the source of truth; UTC is derived at evaluation).
type WallTime struct {
	Year   int        `json:"year"`
	Month  time.Month `json:"month"`
	Day    int        `json:"day"`
	Hour   int        `json:"hour"`
	Minute int        `json:"minute"`
	TZ     string     `json:"tz"`
}

func (w WallTime) validate() error {
	if w.Year < 2000 || w.Year > 2100 || w.Month < 1 || w.Month > 12 ||
		w.Day < 1 || w.Day > 31 || w.Hour < 0 || w.Hour > 23 || w.Minute < 0 || w.Minute > 59 {
		return fmt.Errorf("schedule: impossible wall time (fail closed)")
	}
	if _, err := time.LoadLocation(w.TZ); err != nil {
		return fmt.Errorf("schedule: unknown IANA zone (fail closed)")
	}
	return nil
}

// dueUTC derives the firing instant: dst=ONCE_FIRST — for an autumn fold
// the FIRST (earlier-UTC) occurrence wins; a spring-gap wall time is
// normalized forward by the zone rules.
func (w WallTime) dueUTC() (time.Time, error) {
	loc, err := time.LoadLocation(w.TZ)
	if err != nil {
		return time.Time{}, err
	}
	t := time.Date(w.Year, w.Month, w.Day, w.Hour, w.Minute, 0, 0, loc)
	// ONCE_FIRST: if the same wall time also exists one hour earlier in
	// UTC (fold), take the earlier instant.
	earlier := t.Add(-time.Hour)
	if sameWall(earlier, w, loc) {
		t = earlier
	}
	return t.UTC(), nil
}

func sameWall(t time.Time, w WallTime, loc *time.Location) bool {
	lt := t.In(loc)
	return lt.Year() == w.Year && lt.Month() == w.Month && lt.Day() == w.Day &&
		lt.Hour() == w.Hour && lt.Minute() == w.Minute
}

// Fired is one delivered occurrence.
type Fired struct {
	ScheduleID   string
	OccurrenceID string
	RunID        string
	Body         string
	Overdue      bool
}

// --- payloads (closed) ---

type createdPayload struct {
	ID   string   `json:"id"`
	Body string   `json:"body"`
	Wall WallTime `json:"wall"`
}

type firedPayload struct {
	ScheduleID   string `json:"schedule_id"`
	OccurrenceID string `json:"occurrence_id"`
	Overdue      bool   `json:"overdue"`
}

// Events returns the payload validators for the journal's closed set.
func Events() map[string]journal.PayloadValidator {
	return map[string]journal.PayloadValidator{
		EvScheduleCreated: func(raw json.RawMessage) error {
			var p createdPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ID == "" || p.Body == "" {
				return fmt.Errorf("schedule: id and body are required")
			}
			return p.Wall.validate()
		},
		EvOccurrenceFired: func(raw json.RawMessage) error {
			var p firedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.ScheduleID == "" || p.OccurrenceID == "" {
				return fmt.Errorf("schedule: fired payload requires schedule and occurrence ids")
			}
			return nil
		},
	}
}

// Projection folds schedule events into queryable state in the append
// transaction. fired flips 0→1 exactly once (COALESCE + atomicity: a
// replayed double-fire aborts the append); the C8 counter advances with
// every fire.
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "schedule" }
func (Projection) Version() int { return 1 }

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sched_schedules (
			id TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			wall TEXT NOT NULL,
			fired INTEGER NOT NULL DEFAULT 0,
			created INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sched_meta (key TEXT PRIMARY KEY, value INTEGER NOT NULL);
	`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	for _, stmt := range []string{`DROP TABLE IF EXISTS sched_schedules`, `DROP TABLE IF EXISTS sched_meta`} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	switch ev.Envelope.EventType {
	case EvScheduleCreated:
		var p createdPayload
		if err := json.Unmarshal(ev.Envelope.Payload, &p); err != nil {
			return err
		}
		wall, err := json.Marshal(p.Wall)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sched_schedules(id, body, wall, fired, created) VALUES(?,?,?,0,?)`,
			p.ID, p.Body, string(wall), int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("schedule: create: %w", err) // dup id = PK violation → append aborted
		}
	case EvOccurrenceFired:
		var p firedPayload
		if err := json.Unmarshal(ev.Envelope.Payload, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE sched_schedules SET fired=1 WHERE id=? AND fired=0`, p.ScheduleID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("schedule: occurrence fired for an unknown or already-fired schedule (fail closed)")
		}
		if _, err := tx.Exec(`INSERT INTO sched_meta(key, value) VALUES('last_occurrence_fired', 1)
			ON CONFLICT(key) DO UPDATE SET value = value + 1`); err != nil {
			return err
		}
	}
	return nil
}

// Scheduler is the sweep owner over the profile's ONE journal.
type Scheduler struct {
	j           *journal.Journal
	clock       clockid.Clock
	counterFile string
	mu          sync.Mutex
	seq         uint64
}

func New(j *journal.Journal, c clockid.Clock) (*Scheduler, error) {
	if j == nil || c == nil {
		return nil, fmt.Errorf("schedule: a journal and a clock are required (fail closed)")
	}
	return &Scheduler{j: j, clock: c}, nil
}

// Journal exposes the underlying journal (test/composition seam).
func (s *Scheduler) Journal() *journal.Journal { return s.j }

// SetCounterFile enables the C8 observability file: the counter is
// mirrored there after every fire (doctor reads it without touching the
// single-writer journal).
func (s *Scheduler) SetCounterFile(path string) { s.counterFile = path }

func (s *Scheduler) params(eventType, runID string, payload any) (contracts.EnvelopeParams, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contracts.EnvelopeParams{}, err
	}
	s.mu.Lock()
	s.seq++
	n := s.seq
	s.mu.Unlock()
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-sched-%d-%d-%d", os.Getpid(), s.clock.Now().UnixNano(), n)),
		EventType: eventType, RunID: contracts.RunID(runID), EmittedAt: s.clock.Now(),
		ActorType: contracts.ActorSystem, ActorID: "scheduler", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: s.j.Profile(), AttemptNo: 1,
		Payload: raw, PayloadHash: "recomputed",
	}, nil
}

// CreatedParams builds the validated schedule.created envelope WITHOUT
// appending — composition seam for atomic cross-package batches (T21:
// [schedule.created, obligation.created] in one transaction).
func (s *Scheduler) CreatedParams(id, body string, w WallTime) (contracts.EnvelopeParams, error) {
	if id == "" || body == "" {
		return contracts.EnvelopeParams{}, fmt.Errorf("schedule: id and body are required (fail closed)")
	}
	if err := w.validate(); err != nil {
		return contracts.EnvelopeParams{}, err
	}
	return s.params(EvScheduleCreated, "run-schedule", createdPayload{ID: id, Body: body, Wall: w})
}

// CreateReminder persists one one-shot reminder (validated fail-closed;
// a duplicate id aborts in the projection).
func (s *Scheduler) CreateReminder(ctx context.Context, id, body string, w WallTime) error {
	p, err := s.CreatedParams(id, body, w)
	if err != nil {
		return err
	}
	_, err = s.j.Append(ctx, p)
	return err
}

// Sweep evaluates every unfired schedule against NOW and fires the due
// ones — the startup/wake/periodic catch-up in one place. Each fire is
// the atomic B7 batch [occurrence_fired, run.created, run.admitted].
func (s *Scheduler) Sweep(ctx context.Context) ([]Fired, error) {
	rows, err := s.j.QueryProjection(ctx, `SELECT id, body, wall FROM sched_schedules WHERE fired=0 ORDER BY created`)
	if err != nil {
		return nil, fmt.Errorf("schedule: sweep: %w", err)
	}
	type pending struct {
		id, body string
		wall     WallTime
	}
	var candidates []pending
	for rows.Next() {
		var p pending
		var wall string
		if err := rows.Scan(&p.id, &p.body, &wall); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(wall), &p.wall); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, p)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	var fired []Fired
	for _, c := range candidates {
		due, err := c.wall.dueUTC()
		if err != nil {
			return fired, err
		}
		if now.Before(due) {
			continue
		}
		f := Fired{
			ScheduleID:   c.id,
			OccurrenceID: "occ-" + c.id + "#1", // stable: one-shot sequence 1
			RunID:        "run-occ-" + c.id + "#1",
			Body:         c.body,
			Overdue:      now.Sub(due) > overdueGrace,
		}
		occP, err := s.params(EvOccurrenceFired, f.RunID, firedPayload{
			ScheduleID: c.id, OccurrenceID: f.OccurrenceID, Overdue: f.Overdue})
		if err != nil {
			return fired, err
		}
		turnPayload := json.RawMessage(fmt.Sprintf(`{"occurrence_id":%q}`, f.OccurrenceID))
		createdP, err := s.params(machine.EvRunCreated, f.RunID, nil)
		if err != nil {
			return fired, err
		}
		createdP.Payload = turnPayload
		admittedP, err := s.params(machine.EvRunAdmitted, f.RunID, nil)
		if err != nil {
			return fired, err
		}
		admittedP.Payload = turnPayload
		// THE B7 recipe: one transaction, all three durable or none.
		if _, err := s.j.AppendBatch(ctx, []contracts.EnvelopeParams{occP, createdP, admittedP}); err != nil {
			return fired, fmt.Errorf("schedule: fire %s: %w", c.id, err)
		}
		fired = append(fired, f)
	}
	if len(fired) > 0 && s.counterFile != "" {
		if n, err := s.LastOccurrenceFired(ctx); err == nil {
			atomicwrite.Write(s.counterFile, []byte(fmt.Sprintf("%d\n", n)), 0o600)
		}
	}
	return fired, nil
}

// LastOccurrenceFired reports the C8 positive-liveness counter.
func (s *Scheduler) LastOccurrenceFired(ctx context.Context) (uint64, error) {
	rows, err := s.j.QueryProjection(ctx, `SELECT value FROM sched_meta WHERE key='last_occurrence_fired'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var n uint64
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	return n, nil
}

// Run drives the periodic sweep until ctx ends (startup sweep included).
func (s *Scheduler) Run(ctx context.Context, interval time.Duration, onFire func(Fired)) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	sweep := func() {
		fired, err := s.Sweep(ctx)
		if err != nil {
			return // next tick retries; journal state is authoritative
		}
		for _, f := range fired {
			if onFire != nil {
				onFire(f)
			}
		}
	}
	sweep()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

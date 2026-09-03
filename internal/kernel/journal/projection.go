//go:build linux

package journal

import (
	"database/sql"
	"fmt"
)

// T07 (HARDQ B7 core): synchronous projection harness + generic
// transaction recipe. A SyncProjection folds core state in the SAME
// `BEGIN IMMEDIATE`-style transaction as the append (read-your-own-writes:
// a write is visible to the next same-process read, always). Observability
// projections instead lag asynchronously behind a DURABLE offset that
// advances only after their work is durable. The three CONCRETE recipes
// (inbox-admission+journal · occurrence+run-admission ·
// terminal-result+outbox) land with their first real consumers (T20/T22)
// on top of this sealed API.

// SyncProjection is core state folded atomically with each append.
// Init runs once at Open (schema); Apply runs INSIDE the append
// transaction — returning an error aborts the whole append (state and
// event commit together or not at all).
type SyncProjection interface {
	Name() string
	Init(db *sql.DB) error
	Apply(tx *sql.Tx, ev Event) error
}

// applySyncProjections is called by appendOne inside the open transaction.
func (j *Journal) applySyncProjections(tx *sql.Tx, ev Event) error {
	for _, p := range j.syncProjections {
		if err := p.Apply(tx, ev); err != nil {
			return fmt.Errorf("sync projection %s: %w", p.Name(), err)
		}
	}
	return nil
}

// Projector drives ONE asynchronous (observability-class) projection with a
// durable offset: the offset row advances in the SAME transaction as the
// projection's own write, so a crash can only ever REPLAY work — the offset
// is never ahead of durable processing (at-least-once, monotone).
type Projector struct {
	j    *Journal
	name string
}

// NewProjector registers a named async projection offset (starting at 0).
func (j *Journal) NewProjector(name string) (*Projector, error) {
	if name == "" {
		return nil, fmt.Errorf("projector: name is required")
	}
	if _, err := j.db.Exec(`CREATE TABLE IF NOT EXISTS projection_offsets (
		name TEXT PRIMARY KEY, current_offset INTEGER NOT NULL)`); err != nil {
		return nil, fmt.Errorf("projector schema: %w", err)
	}
	if _, err := j.db.Exec(`INSERT OR IGNORE INTO projection_offsets(name, current_offset) VALUES(?,0)`, name); err != nil {
		return nil, fmt.Errorf("projector init: %w", err)
	}
	return &Projector{j: j, name: name}, nil
}

// Offset returns the durable offset (last fully processed journal offset).
func (p *Projector) Offset() (uint64, error) {
	var off uint64
	err := p.j.db.QueryRow(`SELECT current_offset FROM projection_offsets WHERE name=?`, p.name).Scan(&off)
	if err != nil {
		return 0, fmt.Errorf("projector %s: offset: %w", p.name, err)
	}
	return off, nil
}

// Run processes every event past the durable offset. For each event, apply
// runs inside a transaction TOGETHER with the offset advance: apply's
// writes and the offset commit atomically, or neither does.
func (p *Projector) Run(apply func(tx *sql.Tx, ev Event) error) error {
	if apply == nil {
		return fmt.Errorf("projector %s: nil apply", p.name)
	}
	from, err := p.Offset()
	if err != nil {
		return err
	}
	return p.j.Replay(from, func(ev Event) error {
		tx, err := p.j.db.Begin()
		if err != nil {
			return fmt.Errorf("projector %s: begin: %w", p.name, err)
		}
		defer tx.Rollback()
		if err := apply(tx, ev); err != nil {
			return fmt.Errorf("projector %s at offset %d: %w", p.name, ev.JournalOffset, err)
		}
		res, err := tx.Exec(`UPDATE projection_offsets SET current_offset=? WHERE name=? AND current_offset=?`,
			int64(ev.JournalOffset), p.name, int64(ev.JournalOffset-1))
		if err != nil {
			return fmt.Errorf("projector %s: offset advance: %w", p.name, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("projector %s: offset moved concurrently (fencing)", p.name)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("projector %s: commit: %w", p.name, err)
		}
		return nil
	})
}

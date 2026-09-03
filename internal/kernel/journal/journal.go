// Package journal owns the EventJournal (Annex P0.3): the ONLY writer of
// canonical domain events. Interface (the seam): Open / Append / Replay /
// Close — everything else (the single serialized append actor that owns
// sequence allocation, SQLite-WAL durability, pre-append redaction) is
// implementation behind it (HARDQ B7; Essentials E4).
package journal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/security/redact"

	_ "modernc.org/sqlite"
)

// Journal is the single-write-owner event log. All appends funnel through
// one actor goroutine: sequence allocation and the durable INSERT happen in
// one place, so two live event sources (interactive turn + scheduler) can
// never fork or gap the sequence.
type Journal struct {
	db      *sql.DB
	redact  redact.Redactor
	reqs    chan appendReq
	done    chan struct{}
	closed  chan struct{}
}

type appendReq struct {
	params contracts.EnvelopeParams
	reply  chan appendReply
}

type appendReply struct {
	env contracts.Envelope
	err error
}

// Open opens (or creates) the journal at path. The redactor is an accepted
// dependency (never created inside): known-ref redaction runs BEFORE any
// byte is persisted (HARDQ C1, P0.3 secret-never-reaches-sink).
func Open(path string, r redact.Redactor) (*Journal, error) {
	// busy_timeout bounded (B7); WAL for durability + concurrent readers.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("journal open: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS events (
		sequence INTEGER PRIMARY KEY,
		envelope TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal schema: %w", err)
	}
	j := &Journal{
		db: db, redact: r,
		reqs: make(chan appendReq), done: make(chan struct{}), closed: make(chan struct{}),
	}
	go j.actor()
	return j, nil
}

// actor is the ONE goroutine that allocates sequence numbers and writes.
func (j *Journal) actor() {
	defer close(j.closed)
	var next uint64
	row := j.db.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM events`)
	if err := row.Scan(&next); err != nil {
		next = 0 // an unreadable journal will fail on first INSERT with cause
	}
	for {
		select {
		case <-j.done:
			return
		case req := <-j.reqs:
			req.reply <- j.appendOne(req.params, next+1)
			next++
		}
	}
}

func (j *Journal) appendOne(p contracts.EnvelopeParams, seq uint64) appendReply {
	// Redact BEFORE validation/persist so no sink — not even an error path —
	// carries the raw secret.
	p.Payload = json.RawMessage(j.redact.Redact([]byte(p.Payload)))
	p.Sequence = seq
	env, err := contracts.NewEnvelope(p) // single validation owner (P0.1)
	if err != nil {
		return appendReply{err: err}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return appendReply{err: fmt.Errorf("journal encode: %w", err)}
	}
	if _, err := j.db.Exec(`INSERT INTO events(sequence, envelope) VALUES(?,?)`, int64(seq), string(raw)); err != nil {
		return appendReply{err: fmt.Errorf("journal append: %w", err)}
	}
	return appendReply{env: env}
}

// Append validates, redacts, sequences and durably persists one event.
// Sequence allocation is the journal's alone — callers never supply it
// (the field in params is overwritten).
func (j *Journal) Append(ctx context.Context, p contracts.EnvelopeParams) (contracts.Envelope, error) {
	req := appendReq{params: p, reply: make(chan appendReply, 1)}
	select {
	case j.reqs <- req:
	case <-j.done:
		return contracts.Envelope{}, fmt.Errorf("journal is closed")
	case <-ctx.Done():
		return contracts.Envelope{}, ctx.Err()
	}
	select {
	case rep := <-req.reply:
		return rep.env, rep.err
	case <-ctx.Done():
		return contracts.Envelope{}, ctx.Err()
	}
}

// Replay folds every event with sequence > from, in order, through fn.
// State is a projection of this fold (S0.2) — there is no other read path
// for canonical history.
func (j *Journal) Replay(from uint64, fn func(contracts.Envelope) error) error {
	rows, err := j.db.Query(`SELECT envelope FROM events WHERE sequence > ? ORDER BY sequence`, int64(from))
	if err != nil {
		return fmt.Errorf("journal replay: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return fmt.Errorf("journal replay scan: %w", err)
		}
		env, err := contracts.ParseEnvelope([]byte(raw))
		if err != nil {
			return fmt.Errorf("journal replay decode: %w", err)
		}
		if err := fn(env); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Close stops the actor and releases the database. Idempotent.
func (j *Journal) Close() error {
	select {
	case <-j.done:
	default:
		close(j.done)
	}
	<-j.closed
	return j.db.Close()
}

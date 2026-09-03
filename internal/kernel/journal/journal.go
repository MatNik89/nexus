// Package journal owns the EventJournal (Annex P0.3): the ONLY writer of
// canonical domain events. Interface (the seam): Open / Append / Replay /
// Close — everything else (the single serialized append actor, per-run
// sequence + global journal offset allocation, the integrity hash chain,
// SQLite-WAL durability, pre-persist redaction) is implementation behind it
// (HARDQ B7; Essentials E4; Phase-1A review fold).
package journal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/security/redact"

	_ "modernc.org/sqlite"
)

// redactionPolicyVersion identifies the redaction rule set that processed
// each stored event (P0.3 MUST field). Bump on any redactor semantics change.
const redactionPolicyVersion = 1

// Event is the durable P0.3 journal record: the envelope plus journal-level
// MUST fields. JournalOffset is the GLOBAL total order; Envelope.Sequence
// is the PER-RUN causal order — deliberately distinct (Annex P0.3).
type Event struct {
	JournalOffset          uint64
	Envelope               contracts.Envelope
	RedactionPolicyVersion int
	IntegrityPrevHash      string
	IntegrityHash          string
	SealedPayloadRef       *string
}

// Journal is the single-write-owner event log, BOUND to one profile at Open
// (HARDQ B3: a private event physically cannot land in the work journal).
type Journal struct {
	db        *sql.DB
	profile   contracts.ProfileID
	redact    redact.Redactor
	reqs      chan appendReq
	done      chan struct{}
	actorDone chan struct{}
	closeOnce sync.Once
	closeErr  error
}

type appendReq struct {
	params contracts.EnvelopeParams
	reply  chan appendReply
}

type appendReply struct {
	ev  Event
	err error
}

// testPauseAfterCommit, when non-nil, runs after the durable commit but
// before the reply — the post-commit crash window the SIGKILL RED targets.
// Test-only seam; production never sets it.
var testPauseAfterCommit func()

// Open opens (or creates) the journal at path, bound to profile. The
// redactor is an accepted dependency and mandatory (fail closed).
func Open(path string, profile contracts.ProfileID, r redact.Redactor) (*Journal, error) {
	if r == nil {
		return nil, fmt.Errorf("journal open: redactor is required (fail closed)")
	}
	if !profile.Valid() {
		return nil, fmt.Errorf("journal open: a valid profile binding is required")
	}
	// synchronous=FULL: P0.3 requires durable flush before an append is
	// acknowledged (NORMAL can lose acknowledged commits on power failure).
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("journal open: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS events (
		journal_offset INTEGER PRIMARY KEY,
		event_id TEXT NOT NULL UNIQUE,
		run_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		envelope TEXT NOT NULL,
		redaction_policy_version INTEGER NOT NULL,
		integrity_prev_hash TEXT NOT NULL,
		integrity_hash TEXT NOT NULL,
		sealed_payload_ref TEXT,
		UNIQUE(run_id, sequence)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal schema: %w", err)
	}
	// Fail closed at Open: the recovery reads must succeed BEFORE the actor
	// starts (a swallowed MAX() error must not silently restart history).
	var lastOffset uint64
	var lastHash string
	row := db.QueryRow(`SELECT COALESCE(MAX(journal_offset),0) FROM events`)
	if err := row.Scan(&lastOffset); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal recovery (offset): %w", err)
	}
	if lastOffset > 0 {
		row = db.QueryRow(`SELECT integrity_hash FROM events WHERE journal_offset = ?`, int64(lastOffset))
		if err := row.Scan(&lastHash); err != nil {
			db.Close()
			return nil, fmt.Errorf("journal recovery (hash chain): %w", err)
		}
	}
	j := &Journal{
		db: db, profile: profile, redact: r,
		reqs: make(chan appendReq), done: make(chan struct{}), actorDone: make(chan struct{}),
	}
	go j.actor(lastOffset, lastHash)
	return j, nil
}

// actor is the ONE goroutine that allocates offsets/sequences and writes.
// Allocation advances ONLY after a successful durable commit (a rejected or
// failed append burns nothing — Phase-1A codex #2).
func (j *Journal) actor(lastOffset uint64, lastHash string) {
	defer close(j.actorDone)
	for {
		select {
		case <-j.done:
			return
		case req := <-j.reqs:
			rep := j.appendOne(req.params, lastOffset+1, lastHash)
			if rep.err == nil {
				lastOffset = rep.ev.JournalOffset
				lastHash = rep.ev.IntegrityHash
			}
			if testPauseAfterCommit != nil && rep.err == nil {
				testPauseAfterCommit()
			}
			req.reply <- rep
		}
	}
}

func (j *Journal) appendOne(p contracts.EnvelopeParams, offset uint64, prevHash string) appendReply {
	fail := func(err error) appendReply { return appendReply{err: err} }
	if p.ProfileID != j.profile {
		return fail(fmt.Errorf("journal append: envelope profile does not match the journal's bound profile (fail closed, B3)"))
	}
	// Redact BEFORE anything else touches the bytes, then recompute the
	// payload hash so integrity describes the STORED payload (Phase-1A
	// kilo #2 — a stale hash breaks the chain on exactly the secret-bearing
	// events).
	p.Payload = json.RawMessage(j.redact.Redact([]byte(p.Payload)))
	sum := sha256.Sum256(p.Payload)
	p.PayloadHash = hex.EncodeToString(sum[:])

	tx, err := j.db.Begin()
	if err != nil {
		return fail(fmt.Errorf("journal append begin: %w", err))
	}
	defer tx.Rollback()

	// Per-run causal sequence, allocated inside the transaction.
	var runSeq uint64
	row := tx.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM events WHERE run_id = ?`, string(p.RunID))
	if err := row.Scan(&runSeq); err != nil {
		return fail(fmt.Errorf("journal append (run sequence): %w", err))
	}
	p.Sequence = runSeq + 1

	env, err := contracts.NewEnvelope(p) // single validation owner (P0.1)
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return fail(fmt.Errorf("journal encode: %w", err))
	}
	// Defense in depth: redact the fully marshaled record too — a known
	// secret placed in a NON-payload field (actor, event type…) must also
	// never reach the sink (Phase-1A codex #10).
	raw = j.redact.Redact(raw)

	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write(raw)
	integrity := hex.EncodeToString(h.Sum(nil))

	if _, err := tx.Exec(
		`INSERT INTO events(journal_offset, event_id, run_id, sequence, envelope,
			redaction_policy_version, integrity_prev_hash, integrity_hash, sealed_payload_ref)
		 VALUES(?,?,?,?,?,?,?,?,NULL)`,
		int64(offset), string(env.EventID), string(env.RunID), int64(env.Sequence),
		string(raw), redactionPolicyVersion, prevHash, integrity,
	); err != nil {
		return fail(fmt.Errorf("journal append: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fail(fmt.Errorf("journal commit: %w", err))
	}
	return appendReply{ev: Event{
		JournalOffset: offset, Envelope: env,
		RedactionPolicyVersion: redactionPolicyVersion,
		IntegrityPrevHash:      prevHash, IntegrityHash: integrity,
	}}
}

// Append validates, redacts, sequences and durably persists one event,
// returning the full journal Event. ctx gates ADMISSION only: once the
// actor has accepted the request, Append waits for the definitive outcome —
// a caller can never see "cancelled" for an event that actually committed
// (Phase-1A codex #12 / kilo #3).
func (j *Journal) Append(ctx context.Context, p contracts.EnvelopeParams) (Event, error) {
	req := appendReq{params: p, reply: make(chan appendReply, 1)}
	select {
	case j.reqs <- req:
	case <-j.done:
		return Event{}, fmt.Errorf("journal is closed")
	case <-ctx.Done():
		return Event{}, ctx.Err()
	}
	rep := <-req.reply // admission accepted: outcome is definitive
	return rep.ev, rep.err
}

// Replay folds every event with journal_offset > from, in order. Errors
// carry the failing offset (causal context, constitution rule).
func (j *Journal) Replay(from uint64, fn func(Event) error) error {
	if fn == nil {
		return fmt.Errorf("journal replay: nil callback")
	}
	rows, err := j.db.Query(
		`SELECT journal_offset, envelope, redaction_policy_version,
		        integrity_prev_hash, integrity_hash, sealed_payload_ref
		 FROM events WHERE journal_offset > ? ORDER BY journal_offset`, int64(from))
	if err != nil {
		return fmt.Errorf("journal replay: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ev Event
		var raw string
		if err := rows.Scan(&ev.JournalOffset, &raw, &ev.RedactionPolicyVersion,
			&ev.IntegrityPrevHash, &ev.IntegrityHash, &ev.SealedPayloadRef); err != nil {
			return fmt.Errorf("journal replay scan: %w", err)
		}
		env, err := contracts.ParseEnvelope([]byte(raw))
		if err != nil {
			return fmt.Errorf("journal replay decode at offset %d: %w", ev.JournalOffset, err)
		}
		ev.Envelope = env
		if err := fn(ev); err != nil {
			return fmt.Errorf("journal replay callback at offset %d: %w", ev.JournalOffset, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("journal replay iteration: %w", err)
	}
	return nil
}

// VerifyChain re-computes the integrity hash chain over the whole journal.
func (j *Journal) VerifyChain() error {
	prev := ""
	rows, err := j.db.Query(`SELECT journal_offset, envelope, integrity_prev_hash, integrity_hash
		FROM events ORDER BY journal_offset`)
	if err != nil {
		return fmt.Errorf("journal verify: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var off uint64
		var raw, prevStored, stored string
		if err := rows.Scan(&off, &raw, &prevStored, &stored); err != nil {
			return fmt.Errorf("journal verify scan: %w", err)
		}
		if prevStored != prev {
			return fmt.Errorf("journal chain broken at offset %d: prev-hash mismatch", off)
		}
		h := sha256.New()
		h.Write([]byte(prev))
		h.Write([]byte(raw))
		if hex.EncodeToString(h.Sum(nil)) != stored {
			return fmt.Errorf("journal chain broken at offset %d: hash mismatch", off)
		}
		prev = stored
	}
	return rows.Err()
}

// Close stops the actor and releases the database. Concurrency-idempotent.
func (j *Journal) Close() error {
	j.closeOnce.Do(func() {
		close(j.done)
		<-j.actorDone
		j.closeErr = j.db.Close()
	})
	return j.closeErr
}

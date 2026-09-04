//go:build linux

// Package journal owns the EventJournal (Annex P0.3): the ONLY writer of
// canonical domain events. Interface (the seam): Open / Append / Replay /
// Close (+ VerifyChain) — everything else (the single serialized append
// actor, per-run sequence + global journal offset allocation, the
// full-record integrity hash chain, SQLite-WAL durability, pre-persist
// redaction, DB-persisted profile binding, single-writer detection) is
// implementation behind it (HARDQ B3/B7; Essentials E4; Phase-1A r2 fold).
package journal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/security/redact"

	_ "modernc.org/sqlite"
)

// redactionPolicyVersion identifies the redaction rule set that processed
// each stored event (P0.3 MUST field). Bump on any redactor semantics change.
const redactionPolicyVersion = 1

// PayloadValidator checks a payload for one closed event type.
type PayloadValidator func(json.RawMessage) error

// Event is the durable P0.3 journal record. JournalOffset is the GLOBAL
// total order; Envelope.Sequence is the PER-RUN causal order.
type Event struct {
	JournalOffset          uint64
	Envelope               contracts.Envelope
	RedactionPolicyVersion int
	IntegrityPrevHash      string
	IntegrityHash          string
	SealedPayloadRef       *string
}

// chainInput is the canonical encoding the integrity chain authenticates:
// EVERY journal field except integrity_hash itself. LENGTH-PREFIXED fields
// with an explicit nullability byte — delimiter characters inside IDs and
// NULL-vs-empty sealed refs cannot produce colliding encodings
// (r3 codex #1: "a|b","c" vs "a","b|c" collided under bare pipes).
func chainInput(offset uint64, eventID, runID string, seq uint64, rpv int, prevHash string, sealedRef *string, envelope []byte) []byte {
	var buf []byte
	putUint := func(v uint64) {
		buf = binary.AppendUvarint(buf, v)
	}
	putStr := func(v string) {
		buf = binary.AppendUvarint(buf, uint64(len(v)))
		buf = append(buf, v...)
	}
	putUint(offset)
	putStr(eventID)
	putStr(runID)
	putUint(seq)
	putUint(uint64(rpv))
	putStr(prevHash)
	if sealedRef == nil {
		buf = append(buf, 0)
	} else {
		buf = append(buf, 1)
		putStr(*sealedRef)
	}
	buf = binary.AppendUvarint(buf, uint64(len(envelope)))
	return append(buf, envelope...)
}

func chainHash(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

// Journal is the single-write-owner event log, BOUND to one profile — in
// the DATABASE, not just process memory (r2 codex #3).
type Journal struct {
	db              *sql.DB
	profile         contracts.ProfileID
	redact          redact.Redactor
	events          map[string]PayloadValidator
	syncProjections []SyncProjection
	leaseToken      string
	reqs            chan appendReq
	done            chan struct{}
	actorDone       chan struct{}
	closeOnce       sync.Once
	closeErr        error
}

type appendReq struct {
	batch []contracts.EnvelopeParams
	reply chan appendReply
}

type appendReply struct {
	evs []Event
	err error
}

// test-only fault seams (production never sets them).
var (
	testPauseAfterCommit func()
	testPauseBeforeReply func()
	testFailCommit       func() error
	testFailLeaseDelete  func() error
)

func selfStartToken() (int, string, error) {
	pid := os.Getpid()
	st, err := procStartTime(pid)
	return pid, st, err
}

func procStartTime(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	s := string(b)
	close := strings.LastIndex(s, ")")
	if close < 0 {
		return "", fmt.Errorf("malformed stat")
	}
	fields := strings.Fields(s[close+1:])
	if len(fields) < 20 {
		return "", fmt.Errorf("malformed stat")
	}
	return fields[19], nil
}

// Open opens (or creates) the journal at path, bound to profile, accepting
// only the given closed event-type set (r2 codex #7). It fails closed on:
// nil/empty dependencies, a profile mismatch with the database's own
// binding, a live concurrent writer, or an invalid integrity chain.
func Open(path string, profile contracts.ProfileID, r redact.Redactor, events map[string]PayloadValidator, syncProjections ...SyncProjection) (*Journal, error) {
	if r == nil {
		return nil, fmt.Errorf("journal open: redactor is required (fail closed)")
	}
	if !profile.Valid() {
		return nil, fmt.Errorf("journal open: a valid profile binding is required")
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("journal open: a closed event-type set is required (fail closed)")
	}
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("journal open: %w", err)
	}
	fail := func(e error) (*Journal, error) { db.Close(); return nil, e }
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
	); CREATE TABLE IF NOT EXISTS journal_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return fail(fmt.Errorf("journal schema: %w", err))
	}

	// Profile binding lives IN the database: a reopen under another profile
	// is refused regardless of process memory (r2 codex #3).
	tx, err := db.Begin()
	if err != nil {
		return fail(fmt.Errorf("journal open tx: %w", err))
	}
	var stored string
	err = tx.QueryRow(`SELECT value FROM journal_meta WHERE key='profile'`).Scan(&stored)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.Exec(`INSERT INTO journal_meta(key,value) VALUES('profile',?)`, string(profile)); err != nil {
			tx.Rollback()
			return fail(fmt.Errorf("journal open: recording profile binding: %w", err))
		}
	case err != nil:
		tx.Rollback()
		return fail(fmt.Errorf("journal open: reading profile binding: %w", err))
	case stored != string(profile):
		tx.Rollback()
		return fail(fmt.Errorf("journal open: database is bound to another profile (fail closed, B3)"))
	}

	if err := tx.Commit(); err != nil {
		return fail(fmt.Errorf("journal open commit: %w", err))
	}

	j := &Journal{
		db: db, profile: profile, redact: r, syncProjections: syncProjections,
		reqs: make(chan appendReq), done: make(chan struct{}), actorDone: make(chan struct{}),
	}
	for _, sp := range syncProjections {
		if sp == nil || sp.Name() == "" {
			db.Close()
			return nil, fmt.Errorf("journal open: invalid sync projection (fail closed)")
		}
	}
	// Defensive copy: the closed event set must stay closed after Open
	// (r3 codex #4 — a caller-held map is mutable and racy).
	j.events = make(map[string]PayloadValidator, len(events))
	for name, v := range events {
		if name == "" {
			db.Close()
			return nil, fmt.Errorf("journal open: empty event-type name in the closed set")
		}
		j.events[name] = v
	}
	// Single-writer lease (B7): value = pid:starttime:nonce. A LIVE holder
	// (any pid, incl. our own other handle — the NONCE distinguishes
	// handles, r3 codex #3) blocks this open; a dead one is taken over.
	pid, start, err := selfStartToken()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("journal open: self identity: %w", err)
	}
	nonceB := make([]byte, 8)
	if _, err := rand.Read(nonceB); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal open: nonce: %w", err)
	}
	token := fmt.Sprintf("%d:%s:%s", pid, start, hex.EncodeToString(nonceB))
	ltx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("journal open lease tx: %w", err)
	}
	var writerVal string
	err = ltx.QueryRow(`SELECT value FROM journal_meta WHERE key='writer'`).Scan(&writerVal)
	switch {
	case err == nil:
		parts := strings.SplitN(writerVal, ":", 3)
		if len(parts) == 3 {
			if oldPid, perr := strconv.Atoi(parts[0]); perr == nil {
				if st, serr := procStartTime(oldPid); serr == nil && st == parts[1] {
					ltx.Rollback()
					db.Close()
					return nil, fmt.Errorf("journal open: another live writer holds this journal (fail closed, B7)")
				}
			}
		}
	case err != sql.ErrNoRows:
		ltx.Rollback()
		db.Close()
		return nil, fmt.Errorf("journal open: reading writer lease: %w", err)
	}
	if _, err := ltx.Exec(`INSERT INTO journal_meta(key,value) VALUES('writer',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, token); err != nil {
		ltx.Rollback()
		db.Close()
		return nil, fmt.Errorf("journal open: writer lease: %w", err)
	}
	if err := ltx.Commit(); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal open lease commit: %w", err)
	}
	j.leaseToken = token
	// releaseLease surfaces its own failure (r5 codex #2): a swallowed
	// DELETE error would leave a live-token lease that blocks same-process
	// retries with a misleading primary error.
	releaseLease := func() error {
		if testFailLeaseDelete != nil {
			return testFailLeaseDelete()
		}
		if _, err := db.Exec(`DELETE FROM journal_meta WHERE key='writer' AND value=?`, token); err != nil {
			return fmt.Errorf("releasing writer lease: %w", err)
		}
		return nil
	}
	// Recovery runs UNDER our ownership (r4 codex #3: verify-before-lease
	// allowed a live writer to append between snapshot and handoff — stale
	// actor state). Any failure releases the exact acquired token.
	lastOffset, lastHash, err := j.verifyChainFull()
	if err != nil {
		relErr := releaseLease()
		closeErr := db.Close()
		return nil, fmt.Errorf("journal open: %w", errors.Join(err, relErr, closeErr))
	}
	// Projection schema + CATCH-UP run ONLY under our verified ownership
	// (Phase-1B codex #5) and only through RESTRICTED handles. A version
	// mismatch or missing checkpoint RESETS the projection and rebuilds it
	// from the canonical stream (Phase-3-r2 codex #1, r3 #2) — derived
	// state is never patched blind.
	if err := j.catchupProjections(lastOffset); err != nil {
		relErr := releaseLease()
		closeErr := db.Close()
		return nil, fmt.Errorf("journal open: projection catch-up: %w", errors.Join(err, relErr, closeErr))
	}

	go j.actor(lastOffset, lastHash)
	return j, nil
}

// catchupProjections replays canonical events into every sync projection
// whose durable checkpoint is behind head. Deleting a projection's
// checkpoint row (and its tables) is the REBUILD path: the next Open
// refolds from offset zero.
func (j *Journal) catchupProjections(head uint64) error {
	if len(j.syncProjections) == 0 {
		return nil
	}
	if _, err := j.db.Exec(`CREATE TABLE IF NOT EXISTS proj_sync_offsets (
		name TEXT PRIMARY KEY, applied_offset INTEGER NOT NULL,
		version INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	// Older databases carry the table without the version column.
	j.db.Exec(`ALTER TABLE proj_sync_offsets ADD COLUMN version INTEGER NOT NULL DEFAULT 0`)
	pdb := &ProjDB{db: j.db}
	for _, sp := range j.syncProjections {
		var cp uint64
		var ver int
		err := j.db.QueryRow(`SELECT applied_offset, version FROM proj_sync_offsets WHERE name=?`, sp.Name()).Scan(&cp, &ver)
		missing := err == sql.ErrNoRows
		if err != nil && !missing {
			return err
		}
		if missing || ver != sp.Version() {
			// RESET + rebuild: no checkpoint, or a schema from another
			// revision — derived state is dropped and refolded whole.
			if err := sp.Reset(pdb); err != nil {
				return fmt.Errorf("projection %s reset: %w", sp.Name(), err)
			}
			if _, err := j.db.Exec(`INSERT INTO proj_sync_offsets(name, applied_offset, version) VALUES(?1,0,?2)
				ON CONFLICT(name) DO UPDATE SET applied_offset=0, version=?2`, sp.Name(), sp.Version()); err != nil {
				return err
			}
			cp = 0
		}
		if err := sp.Init(pdb); err != nil {
			return fmt.Errorf("projection %s init: %w", sp.Name(), err)
		}
		if cp >= head {
			continue
		}
		if err := j.Replay(cp, func(ev Event) error {
			tx, err := j.db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err := sp.Apply(&ProjTx{tx: tx}, ev); err != nil {
				return fmt.Errorf("projection %s catch-up at offset %d: %w", sp.Name(), ev.JournalOffset, err)
			}
			if _, err := tx.Exec(`UPDATE proj_sync_offsets SET applied_offset=? WHERE name=?`,
				int64(ev.JournalOffset), sp.Name()); err != nil {
				return err
			}
			return tx.Commit()
		}); err != nil {
			return err
		}
	}
	return nil
}

func (j *Journal) actor(lastOffset uint64, lastHash string) {
	defer close(j.actorDone)
	for {
		select {
		case <-j.done:
			return
		case req := <-j.reqs:
			rep := j.appendBatch(req.batch, lastOffset+1, lastHash)
			if rep.err == nil && len(rep.evs) > 0 {
				last := rep.evs[len(rep.evs)-1]
				lastOffset = last.JournalOffset
				lastHash = last.IntegrityHash
			}
			if testPauseAfterCommit != nil && rep.err == nil {
				testPauseAfterCommit()
			}
			if testPauseBeforeReply != nil {
				testPauseBeforeReply()
			}
			req.reply <- rep
		}
	}
}

// structuralFields lists every non-payload field a known secret must never
// occupy: rather than mutating them (which would fork stored vs returned
// identity — r2 codex #2), the append is REJECTED.
func structuralFields(p contracts.EnvelopeParams) []string {
	out := []string{
		string(p.EventID), string(p.RunID), p.EventType,
		string(p.ActorID), string(p.PrincipalID), string(p.WorkspaceID), string(p.ProfileID),
	}
	if p.ParentEventID != nil {
		out = append(out, string(*p.ParentEventID))
	}
	if p.TurnID != nil {
		out = append(out, string(*p.TurnID))
	}
	if p.ToolCallID != nil {
		out = append(out, string(*p.ToolCallID))
	}
	if p.TenantID != nil {
		out = append(out, string(*p.TenantID))
	}
	if p.IdempotencyKey != nil {
		out = append(out, *p.IdempotencyKey)
	}
	return out
}

// prepare runs every PRE-transaction admission step for one envelope:
// profile binding, closed event-type, structural-secret rejection,
// semantic redaction + hash recompute, typed payload validation.
func (j *Journal) prepare(p contracts.EnvelopeParams) (contracts.EnvelopeParams, error) {
	if p.ProfileID != j.profile {
		return p, fmt.Errorf("journal append: envelope profile does not match the journal's bound profile (fail closed, B3)")
	}
	// Closed event-type admission + typed payload validation BEFORE
	// persistence (r2 codex #7).
	validator, ok := j.events[p.EventType]
	if !ok {
		return p, fmt.Errorf("journal append: unknown event type (fail closed; closed set has %d entries)", len(j.events))
	}
	// Known secrets in STRUCTURAL fields are rejected, not mutated
	// (r2 codex #2): stored and returned identity stay one and the same.
	// Touches is part of the required Redactor contract (r3 codex #2).
	for _, f := range structuralFields(p) {
		if j.redact.Touches(f) {
			return p, fmt.Errorf("journal append: a known secret occurs in a structural field (rejected fail-closed)")
		}
	}
	// Redact the payload SEMANTICALLY, then recompute its hash so integrity
	// describes the stored bytes (r1 kilo #2).
	redacted, rerr := j.redact.Redact([]byte(p.Payload))
	if rerr != nil {
		return p, fmt.Errorf("journal append: %w", rerr)
	}
	p.Payload = json.RawMessage(redacted)
	sum := sha256.Sum256(p.Payload)
	p.PayloadHash = hex.EncodeToString(sum[:])
	if validator != nil {
		if err := validator(p.Payload); err != nil {
			return p, fmt.Errorf("journal append: payload invalid for event type: %w", err)
		}
	}
	return p, nil
}

// insertInTx sequences, chains and persists ONE prepared envelope inside
// the caller's transaction, folding sync projections with it.
func (j *Journal) insertInTx(tx *sql.Tx, p contracts.EnvelopeParams, offset uint64, prevHash string) (Event, error) {
	var runSeq uint64
	row := tx.QueryRow(`SELECT COALESCE(MAX(sequence),0) FROM events WHERE run_id = ?`, string(p.RunID))
	if err := row.Scan(&runSeq); err != nil {
		return Event{}, fmt.Errorf("journal append (run sequence): %w", err)
	}
	p.Sequence = runSeq + 1

	env, err := contracts.NewEnvelope(p) // single validation owner (P0.1)
	if err != nil {
		return Event{}, err
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return Event{}, fmt.Errorf("journal encode: %w", err)
	}
	integrity := chainHash(chainInput(offset, string(env.EventID), string(env.RunID),
		env.Sequence, redactionPolicyVersion, prevHash, nil, raw))

	if _, err := tx.Exec(
		`INSERT INTO events(journal_offset, event_id, run_id, sequence, envelope,
			redaction_policy_version, integrity_prev_hash, integrity_hash, sealed_payload_ref)
		 VALUES(?,?,?,?,?,?,?,?,NULL)`,
		int64(offset), string(env.EventID), string(env.RunID), int64(env.Sequence),
		string(raw), redactionPolicyVersion, prevHash, integrity,
	); err != nil {
		return Event{}, fmt.Errorf("journal append: %w", err)
	}
	// Core-state projections fold IN THIS transaction (B7): event and
	// derived state commit together or not at all.
	ev := Event{
		JournalOffset: offset, Envelope: env,
		RedactionPolicyVersion: redactionPolicyVersion,
		IntegrityPrevHash:      prevHash, IntegrityHash: integrity,
	}
	if err := j.applySyncProjections(tx, ev); err != nil {
		return Event{}, fmt.Errorf("journal append: %w", err)
	}
	return ev, nil
}

// appendBatch persists a whole batch in ONE transaction — the concrete
// HARDQ B7 recipe primitive (occurrence+run-admission, inbox-admission+
// journal, terminal-result+outbox): every event in the batch is durable,
// or none is.
func (j *Journal) appendBatch(batch []contracts.EnvelopeParams, firstOffset uint64, prevHash string) appendReply {
	fail := func(err error) appendReply { return appendReply{err: err} }
	if len(batch) == 0 {
		return fail(fmt.Errorf("journal append: empty batch (fail closed)"))
	}
	prepared := make([]contracts.EnvelopeParams, 0, len(batch))
	for _, p := range batch {
		pp, err := j.prepare(p)
		if err != nil {
			return fail(err)
		}
		prepared = append(prepared, pp)
	}
	tx, err := j.db.Begin()
	if err != nil {
		return fail(fmt.Errorf("journal append begin: %w", err))
	}
	defer tx.Rollback()
	evs := make([]Event, 0, len(prepared))
	offset, hash := firstOffset, prevHash
	for _, p := range prepared {
		ev, err := j.insertInTx(tx, p, offset, hash)
		if err != nil {
			return fail(err)
		}
		evs = append(evs, ev)
		offset, hash = ev.JournalOffset+1, ev.IntegrityHash
		// Crash-consistency seam (test builds only): SIGKILL between
		// recipe members — the transaction must leave NOTHING durable.
		if len(evs) == 1 && len(prepared) > 1 && testing.Testing() &&
			os.Getenv("NEXUS_TEST_KILL_MID_BATCH") == "1" {
			syscall.Kill(os.Getpid(), syscall.SIGKILL)
		}
	}
	if testFailCommit != nil {
		if err := testFailCommit(); err != nil {
			return fail(fmt.Errorf("journal commit: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		return fail(fmt.Errorf("journal commit: %w", err))
	}
	return appendReply{evs: evs}
}

// Append validates, redacts, sequences and durably persists one event. ctx
// gates ADMISSION only: after the actor accepts, the outcome is definitive.
func (j *Journal) Append(ctx context.Context, p contracts.EnvelopeParams) (Event, error) {
	evs, err := j.AppendBatch(ctx, []contracts.EnvelopeParams{p})
	if err != nil {
		return Event{}, err
	}
	return evs[0], nil
}

// AppendBatch persists ALL the given events in ONE serialized transaction
// (HARDQ B7 recipes): all durable or none. ctx gates admission only.
func (j *Journal) AppendBatch(ctx context.Context, batch []contracts.EnvelopeParams) ([]Event, error) {
	req := appendReq{batch: batch, reply: make(chan appendReply, 1)}
	select {
	case j.reqs <- req:
	case <-j.done:
		return nil, fmt.Errorf("journal is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	rep := <-req.reply
	return rep.evs, rep.err
}

// Replay folds every event with journal_offset > from, in order, VERIFYING
// the integrity chain as it streams (r2 codex #1: replay must not serve
// tampered history). Verification always starts from offset 0 internally.
// RedactorRewrites reports whether the journal's OWN redactor would
// rewrite raw — the fail-closed seam for exact-bytes consumers (Phase-3-
// r3 codex #6: an independently injected redactor copy could disagree
// with what the append actually stores).
func (j *Journal) RedactorRewrites(raw []byte) (bool, error) {
	red, err := j.redact.Redact(raw)
	if err != nil {
		return true, fmt.Errorf("journal redactor: %w", err)
	}
	if string(red) == string(raw) {
		return false, nil
	}
	// Redaction re-serializes JSON, so byte inequality alone is not a
	// rewrite — compare the decoded VALUES (a real redaction changes
	// content, not just formatting).
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(red, &b) != nil {
		return true, nil // non-JSON that changed: treat as rewritten
	}
	return !reflect.DeepEqual(a, b), nil
}

func (j *Journal) Replay(from uint64, fn func(Event) error) error {
	if fn == nil {
		return fmt.Errorf("journal replay: nil callback")
	}
	prev := ""
	rows, err := j.db.Query(
		`SELECT journal_offset, event_id, run_id, sequence, envelope,
		        redaction_policy_version, integrity_prev_hash, integrity_hash, sealed_payload_ref
		 FROM events ORDER BY journal_offset`)
	if err != nil {
		return fmt.Errorf("journal replay: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ev Event
		var raw, eventID, runID string
		var seq uint64
		if err := rows.Scan(&ev.JournalOffset, &eventID, &runID, &seq, &raw,
			&ev.RedactionPolicyVersion, &ev.IntegrityPrevHash, &ev.IntegrityHash, &ev.SealedPayloadRef); err != nil {
			return fmt.Errorf("journal replay scan: %w", err)
		}
		if ev.IntegrityPrevHash != prev {
			return fmt.Errorf("journal replay: chain broken at offset %d (prev-hash mismatch)", ev.JournalOffset)
		}
		if chainHash(chainInput(ev.JournalOffset, eventID, runID, seq,
			ev.RedactionPolicyVersion, ev.IntegrityPrevHash, ev.SealedPayloadRef, []byte(raw))) != ev.IntegrityHash {
			return fmt.Errorf("journal replay: chain broken at offset %d (hash mismatch)", ev.JournalOffset)
		}
		prev = ev.IntegrityHash
		env, err := contracts.ParseEnvelope([]byte(raw))
		if err != nil {
			return fmt.Errorf("journal replay decode at offset %d: %w", ev.JournalOffset, err)
		}
		if string(env.EventID) != eventID || string(env.RunID) != runID || env.Sequence != seq {
			return fmt.Errorf("journal replay: denormalized columns diverge from envelope at offset %d", ev.JournalOffset)
		}
		ev.Envelope = env
		if ev.JournalOffset <= from {
			continue
		}
		if err := fn(ev); err != nil {
			return fmt.Errorf("journal replay callback at offset %d: %w", ev.JournalOffset, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("journal replay iteration: %w", err)
	}
	return nil
}

// verifyChainFull walks the whole journal, returning the last offset+hash.
func (j *Journal) verifyChainFull() (uint64, string, error) {
	prev := ""
	var lastOffset uint64
	rows, err := j.db.Query(`SELECT journal_offset, event_id, run_id, sequence, envelope,
		redaction_policy_version, integrity_prev_hash, integrity_hash, sealed_payload_ref
		FROM events ORDER BY journal_offset`)
	if err != nil {
		return 0, "", fmt.Errorf("chain verify: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var off, seq uint64
		var eventID, runID, raw, prevStored, stored string
		var rpv int
		var sealed *string
		if err := rows.Scan(&off, &eventID, &runID, &seq, &raw, &rpv, &prevStored, &stored, &sealed); err != nil {
			return 0, "", fmt.Errorf("chain verify scan: %w", err)
		}
		if prevStored != prev {
			return 0, "", fmt.Errorf("journal chain broken at offset %d: prev-hash mismatch", off)
		}
		if chainHash(chainInput(off, eventID, runID, seq, rpv, prevStored, sealed, []byte(raw))) != stored {
			return 0, "", fmt.Errorf("journal chain broken at offset %d: hash mismatch", off)
		}
		env, perr := contracts.ParseEnvelope([]byte(raw))
		if perr != nil {
			return 0, "", fmt.Errorf("chain verify: envelope undecodable at offset %d: %w", off, perr)
		}
		if string(env.EventID) != eventID || string(env.RunID) != runID || env.Sequence != seq {
			return 0, "", fmt.Errorf("chain verify: denormalized columns diverge from envelope at offset %d", off)
		}
		prev = stored
		lastOffset = off
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	return lastOffset, prev, nil
}

// VerifyChain re-computes the full-record integrity chain.
func (j *Journal) VerifyChain() error {
	_, _, err := j.verifyChainFull()
	return err
}

// Close stops the actor, RELEASES the writer lease (exactly our own token,
// r3 codex #3) and closes the database. Concurrency-idempotent.
func (j *Journal) Close() error {
	j.closeOnce.Do(func() {
		close(j.done)
		<-j.actorDone
		var leaseErr error
		if j.leaseToken != "" {
			if testFailLeaseDelete != nil {
				leaseErr = testFailLeaseDelete()
			} else if _, err := j.db.Exec(`DELETE FROM journal_meta WHERE key='writer' AND value=?`, j.leaseToken); err != nil {
				leaseErr = fmt.Errorf("journal close: releasing writer lease: %w", err)
			}
		}
		j.closeErr = errors.Join(leaseErr, j.db.Close())
	})
	return j.closeErr
}

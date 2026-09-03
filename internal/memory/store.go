//go:build linux

// Package memory owns the per-profile fact store scaffolding (T18, HARDQ
// B3 physical isolation): ONE SQLite file per profile under its own
// subtree, every row stamped with the store's BOUND profile (a caller can
// never smuggle a foreign stamp), and the binding persisted in the file —
// a store opened under the wrong profile fails at Open, not at query
// time. FTS lives inside the same file, so a search physically cannot
// cross profiles. T19 adds the memory SEMANTICS (preview/approval,
// review queue, supersession) on top of this sealed base.
package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	_ "modernc.org/sqlite"
)

// Origin says how a fact entered the system.
type Origin string

const (
	OriginExplicit Origin = "explicit"
	OriginInferred Origin = "inferred"
)

// Status is the closed review state.
type Status string

const (
	StatusProposed Status = "proposed"
	StatusAccepted Status = "accepted"
	StatusRejected Status = "rejected"
)

// Row is one stored fact.
type Row struct {
	ID      string
	Profile contracts.ProfileID
	Content string
	Origin  Origin
	Status  Status
}

// Store is a profile-BOUND fact store.
type Store struct {
	db      *sql.DB
	path    string
	profile contracts.ProfileID
}

// Open binds the store file to profile. A file stamped for another
// profile is refused (fail closed); a fresh file is stamped now.
func Open(path string, profile contracts.ProfileID) (*Store, error) {
	if !profile.Valid() {
		return nil, fmt.Errorf("memory: a profile is required (fail closed)")
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS store_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS facts (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			content TEXT NOT NULL,
			origin TEXT NOT NULL,
			status TEXT NOT NULL,
			supersedes TEXT,
			created_at INTEGER NOT NULL
		);
		CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(content, content_rowid='rowid');
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory: schema: %w", err)
	}
	// Profile binding: stamp a fresh file, verify an existing one.
	var stored string
	err = db.QueryRow(`SELECT value FROM store_meta WHERE key='profile'`).Scan(&stored)
	switch {
	case err == sql.ErrNoRows:
		if _, err := db.Exec(`INSERT INTO store_meta(key, value) VALUES('profile', ?)`, string(profile)); err != nil {
			db.Close()
			return nil, fmt.Errorf("memory: stamp: %w", err)
		}
	case err != nil:
		db.Close()
		return nil, fmt.Errorf("memory: binding: %w", err)
	case stored != string(profile):
		db.Close()
		return nil, fmt.Errorf("memory: store is bound to another profile (fail closed)")
	}
	return &Store{db: db, path: path, profile: profile}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Path reports the physical file (isolation proofs).
func (s *Store) Path() string { return s.path }

// insert writes one row in one transaction (fact + FTS together).
func (s *Store) insert(id, content string, origin Origin, status Status, supersedes *string) error {
	if id == "" || content == "" {
		return fmt.Errorf("memory: fact id and content are required (fail closed)")
	}
	if origin != OriginExplicit && origin != OriginInferred {
		return fmt.Errorf("memory: unknown origin (fail closed)")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO facts(id, profile_id, content, origin, status, supersedes, created_at)
		VALUES(?,?,?,?,?,?,?)`,
		id, string(s.profile), content, string(origin), string(status), supersedes, time.Now().UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("memory: save: %w", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO facts_fts(rowid, content) VALUES(?,?)`, rowid, content); err != nil {
		return fmt.Errorf("memory: fts: %w", err)
	}
	return tx.Commit()
}

// SaveFact stores one already-approved EXPLICIT fact (callers that ran
// the approval flow themselves; T18 seeding path).
func (s *Store) SaveFact(id, content string) error {
	return s.insert(id, content, OriginExplicit, StatusAccepted, nil)
}

// Propose enters a fact into the REVIEW state and returns the EXACT
// content as its preview ("remember X" shows precisely what will be
// stored — S9.2). Nothing proposed is recallable until accepted.
func (s *Store) Propose(id, content string, origin Origin) (string, error) {
	if err := s.insert(id, content, origin, StatusProposed, nil); err != nil {
		return "", err
	}
	return content, nil
}

// setStatus moves proposed → accepted|rejected. Decisions are terminal:
// a rejected fact is never resurrected, an accepted one never re-decided.
func (s *Store) setStatus(id string, to Status) error {
	res, err := s.db.Exec(`UPDATE facts SET status=? WHERE id=? AND status=?`,
		string(to), id, string(StatusProposed))
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("memory: fact is not awaiting review (unknown, or already decided — decisions are terminal)")
	}
	return nil
}

func (s *Store) Accept(id string) error { return s.setStatus(id, StatusAccepted) }
func (s *Store) Reject(id string) error { return s.setStatus(id, StatusRejected) }

// Supersede appends a CORRECTION: the new row points at the old one;
// recall follows the chain to the latest accepted version. Nothing is
// deleted (B8 append-only; FORGET/PURGE live in Annex P2.1).
func (s *Store) Supersede(oldID, newID, content string) error {
	var status string
	err := s.db.QueryRow(`SELECT status FROM facts WHERE id=?`, oldID).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("memory: cannot supersede an unknown fact (fail closed)")
	}
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	if Status(status) != StatusAccepted {
		return fmt.Errorf("memory: only an ACCEPTED fact can be superseded (fail closed)")
	}
	// The correction was explicitly stated by the user — accepted.
	return s.insert(newID, content, OriginExplicit, StatusAccepted, &oldID)
}

// Recall retrieves ACCEPTED, LATEST-version facts matching q, newest
// first. No decay: age never filters (HARDQ B8).
func (s *Store) Recall(q string) ([]Row, error) {
	if strings.TrimSpace(q) == "" {
		return nil, fmt.Errorf("memory: empty query (fail closed)")
	}
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	rows, err := s.db.Query(`
		SELECT f.id, f.profile_id, f.content, f.origin, f.status
		FROM facts_fts t JOIN facts f ON f.rowid = t.rowid
		WHERE facts_fts MATCH ?
		  AND f.status = 'accepted'
		  AND NOT EXISTS (SELECT 1 FROM facts n WHERE n.supersedes = f.id AND n.status = 'accepted')
		ORDER BY f.rowid DESC`, phrase)
	if err != nil {
		return nil, fmt.Errorf("memory: recall: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// Search runs an FTS query over ACCEPTED facts INSIDE this profile's
// file — cross-profile hits are physically impossible. The query is
// treated as a phrase (quoted), never as raw FTS syntax.
func (s *Store) Search(q string) ([]Row, error) {
	if strings.TrimSpace(q) == "" {
		return nil, fmt.Errorf("memory: empty query (fail closed)")
	}
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	rows, err := s.db.Query(`
		SELECT f.id, f.profile_id, f.content, f.origin, f.status
		FROM facts_fts t JOIN facts f ON f.rowid = t.rowid
		WHERE facts_fts MATCH ? AND f.status = 'accepted'
		ORDER BY f.rowid DESC`, phrase)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// All lists every fact regardless of status (verification/replay surface
// — never a model-facing retrieval path).
func (s *Store) All() ([]Row, error) {
	rows, err := s.db.Query(`SELECT id, profile_id, content, origin, status FROM facts ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func scanRows(rows *sql.Rows) ([]Row, error) {
	var out []Row
	for rows.Next() {
		var r Row
		var p, o, st string
		if err := rows.Scan(&r.ID, &p, &r.Content, &o, &st); err != nil {
			return nil, fmt.Errorf("memory: %w", err)
		}
		r.Profile = contracts.ProfileID(p)
		r.Origin = Origin(o)
		r.Status = Status(st)
		out = append(out, r)
	}
	return out, rows.Err()
}

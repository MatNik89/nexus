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

// Row is one stored fact.
type Row struct {
	ID      string
	Profile contracts.ProfileID
	Content string
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
			created_at TEXT NOT NULL
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

// SaveFact stores one fact stamped with the store's BOUND profile — the
// stamp is not caller input.
func (s *Store) SaveFact(id, content string) error {
	if id == "" || content == "" {
		return fmt.Errorf("memory: fact id and content are required (fail closed)")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO facts(id, profile_id, content, created_at) VALUES(?,?,?,?)`,
		id, string(s.profile), content, time.Now().UTC().Format(time.RFC3339Nano))
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

// Search runs an FTS query INSIDE this profile's file — cross-profile
// hits are physically impossible. The query is treated as a phrase
// (quoted), never as raw FTS syntax.
func (s *Store) Search(q string) ([]Row, error) {
	if strings.TrimSpace(q) == "" {
		return nil, fmt.Errorf("memory: empty query (fail closed)")
	}
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	rows, err := s.db.Query(`
		SELECT f.id, f.profile_id, f.content
		FROM facts_fts t JOIN facts f ON f.rowid = t.rowid
		WHERE facts_fts MATCH ?
		ORDER BY f.created_at DESC`, phrase)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// All lists every fact (verification/replay surface).
func (s *Store) All() ([]Row, error) {
	rows, err := s.db.Query(`SELECT id, profile_id, content FROM facts ORDER BY created_at`)
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
		var p string
		if err := rows.Scan(&r.ID, &p, &r.Content); err != nil {
			return nil, fmt.Errorf("memory: %w", err)
		}
		r.Profile = contracts.ProfileID(p)
		out = append(out, r)
	}
	return out, rows.Err()
}

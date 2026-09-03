//go:build linux

// Package memory owns explicit per-profile memory (T18/T19, HARDQ B3/B8)
// as a SYNCHRONOUS JOURNAL PROJECTION (Annex P0.3 / Phase-3 codex #1):
// every fact mutation is a journal EVENT in the ONE profile database —
// the projection folds it into queryable tables in the SAME transaction
// as the append, so copy/replaying a profile journal reproduces the
// facts, and no fact can exist without its canonical event. Physical
// isolation is the journal's own (one file per profile, profile binding
// verified at Open); FTS lives in the same file, so search cannot cross
// profiles.
//
// Semantics (B8): Propose returns the EXACT content as its preview;
// nothing proposed is recallable until accepted; decisions are terminal;
// supersession is append-only and STRICTLY LINEAR (a fact with ANY
// successor can never be superseded again — DB unique index + in-Apply
// leaf check, serialized by the journal's single append actor); NO decay;
// MEMORY_FORGET/DATA_PURGE stay with their Annex P2.1 owner.
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
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

// Memory event types (closed; registered into the journal's event set).
const (
	EvFactSaved      = "memory.fact_saved" // pre-approved explicit fact
	EvFactProposed   = "memory.fact_proposed"
	EvFactAccepted   = "memory.fact_accepted"
	EvFactRejected   = "memory.fact_rejected"
	EvFactSuperseded = "memory.fact_superseded"
)

// factPayload is the closed wire shape for saved/proposed facts.
type factPayload struct {
	ID          string                `json:"id"`
	Content     string                `json:"content"`
	Origin      Origin                `json:"origin"`
	Tags        []string              `json:"tags,omitempty"`
	Trust       contracts.TrustClass  `json:"trust_class"`
	Sensitivity contracts.Sensitivity `json:"sensitivity"`
}

type decisionPayload struct {
	ID string `json:"id"`
}

type supersedePayload struct {
	OldID string      `json:"old_id"`
	New   factPayload `json:"new"`
}

func (f factPayload) validate() error {
	if f.ID == "" || f.Content == "" {
		return fmt.Errorf("memory: fact id and content are required (fail closed)")
	}
	if f.Origin != OriginExplicit && f.Origin != OriginInferred {
		return fmt.Errorf("memory: unknown origin (fail closed)")
	}
	if !f.Trust.Valid() || !f.Sensitivity.Valid() {
		return fmt.Errorf("memory: trust and sensitivity classes are required (fail closed)")
	}
	for _, t := range f.Tags {
		if strings.TrimSpace(t) == "" || strings.ContainsAny(t, " \t\n") {
			return fmt.Errorf("memory: tags must be non-empty single tokens (fail closed)")
		}
	}
	return nil
}

// Events returns the payload validators the journal registers — malformed
// memory events are rejected at append, before any projection state.
func Events() map[string]journal.PayloadValidator {
	factV := func(raw json.RawMessage) error {
		var p factPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return p.validate()
	}
	decisionV := func(raw json.RawMessage) error {
		var p decisionPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.ID == "" {
			return fmt.Errorf("memory: decision requires a fact id")
		}
		return nil
	}
	supersedeV := func(raw json.RawMessage) error {
		var p supersedePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.OldID == "" {
			return fmt.Errorf("memory: supersede requires the old fact id")
		}
		return p.New.validate()
	}
	return map[string]journal.PayloadValidator{
		EvFactSaved: factV, EvFactProposed: factV,
		EvFactAccepted: decisionV, EvFactRejected: decisionV,
		EvFactSuperseded: supersedeV,
	}
}

// Projection folds memory events into queryable tables INSIDE the append
// transaction (B7 same-transaction core projection).
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "memory_facts" }

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mem_facts (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			content TEXT NOT NULL,
			origin TEXT NOT NULL,
			status TEXT NOT NULL,
			supersedes TEXT,
			tags TEXT NOT NULL DEFAULT '',
			trust INTEGER NOT NULL,
			sensitivity INTEGER NOT NULL,
			created INTEGER NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS ux_mem_supersedes
			ON mem_facts(supersedes) WHERE supersedes IS NOT NULL;
		CREATE VIRTUAL TABLE IF NOT EXISTS mem_facts_fts USING fts5(content);
	`)
	return err
}

func tagBlob(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return " " + strings.Join(tags, " ") + " "
}

func (Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	insert := func(f factPayload, status Status, supersedes *string) error {
		res, err := tx.Exec(`INSERT INTO mem_facts
			(id, profile_id, content, origin, status, supersedes, tags, trust, sensitivity, created)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			f.ID, string(ev.Envelope.ProfileID), f.Content, string(f.Origin), string(status),
			supersedes, tagBlob(f.Tags), int(f.Trust), int(f.Sensitivity), int64(ev.JournalOffset))
		if err != nil {
			return fmt.Errorf("memory: fact insert: %w", err)
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO mem_facts_fts(rowid, content) VALUES(?,?)`, rowid, f.Content); err != nil {
			return fmt.Errorf("memory: fts insert: %w", err)
		}
		return nil
	}
	decide := func(to Status, raw json.RawMessage) error {
		var d decisionPayload
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE mem_facts SET status=? WHERE id=? AND status='proposed'`,
			string(to), d.ID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("memory: fact is not awaiting review (unknown, or already decided — decisions are terminal)")
		}
		return nil
	}
	raw := ev.Envelope.Payload
	switch ev.Envelope.EventType {
	case EvFactSaved:
		var f factPayload
		if err := json.Unmarshal(raw, &f); err != nil {
			return err
		}
		return insert(f, StatusAccepted, nil)
	case EvFactProposed:
		var f factPayload
		if err := json.Unmarshal(raw, &f); err != nil {
			return err
		}
		return insert(f, StatusProposed, nil)
	case EvFactAccepted:
		return decide(StatusAccepted, raw)
	case EvFactRejected:
		return decide(StatusRejected, raw)
	case EvFactSuperseded:
		var p supersedePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// Leaf check INSIDE the append transaction (serialized by the one
		// append actor): only an ACCEPTED fact with NO successor of any
		// status can be corrected — strictly linear history. The unique
		// index is the DB-level backstop.
		rows, err := tx.Query(`SELECT
			(SELECT status FROM mem_facts WHERE id=?1),
			EXISTS(SELECT 1 FROM mem_facts WHERE supersedes=?1)`, p.OldID)
		if err != nil {
			return err
		}
		var status sql.NullString
		var hasSuccessor bool
		if rows.Next() {
			if err := rows.Scan(&status, &hasSuccessor); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !status.Valid {
			return fmt.Errorf("memory: cannot supersede an unknown fact (fail closed)")
		}
		if Status(status.String) != StatusAccepted {
			return fmt.Errorf("memory: only an ACCEPTED fact can be superseded (fail closed)")
		}
		if hasSuccessor {
			return fmt.Errorf("memory: fact already has a successor — supersession is strictly linear (fail closed)")
		}
		return insert(p.New, StatusAccepted, &p.OldID)
	}
	return nil // not a memory event
}

// Row is one stored fact.
type Row struct {
	ID          string
	Profile     contracts.ProfileID
	Content     string
	Origin      Origin
	Status      Status
	Tags        []string
	Trust       contracts.TrustClass
	Sensitivity contracts.Sensitivity
}

// Store is the memory facade over the profile's ONE journal.
type Store struct {
	j   *journal.Journal
	seq atomic.Uint64
}

func NewStore(j *journal.Journal) (*Store, error) {
	if j == nil {
		return nil, fmt.Errorf("memory: a journal is required (fail closed)")
	}
	return &Store{j: j}, nil
}

// Profile reports the bound profile (the journal's own binding).
func (s *Store) Profile() contracts.ProfileID { return s.j.Profile() }

func (s *Store) append(ctx context.Context, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("memory: %w", err)
	}
	_, err = s.j.Append(ctx, contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-mem-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), s.seq.Add(1))),
		EventType: eventType, RunID: "run-memory", EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "memory", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: s.j.Profile(), AttemptNo: 1,
		Payload: raw, PayloadHash: "recomputed",
	})
	return err
}

func explicitDefaults(id, content string, origin Origin, tags []string) factPayload {
	trust := contracts.TrustToolTrusted
	if origin == OriginInferred {
		// Inferences derive from conversation context that may include
		// untrusted material — monotone: never MORE trusted than source.
		trust = contracts.TrustUntrustedExternal
	}
	return factPayload{ID: id, Content: content, Origin: origin, Tags: tags,
		Trust: trust, Sensitivity: contracts.SensitivityConfidential}
}

// SaveFact stores one already-approved EXPLICIT fact (the PEP approval
// flow ran; T18 seeding path).
func (s *Store) SaveFact(ctx context.Context, id, content string, tags ...string) error {
	return s.append(ctx, EvFactSaved, explicitDefaults(id, content, OriginExplicit, tags))
}

// Propose enters a fact into the REVIEW state and returns the EXACT
// content as its preview. Nothing proposed is recallable until accepted.
func (s *Store) Propose(ctx context.Context, id, content string, origin Origin, tags ...string) (string, error) {
	if err := s.append(ctx, EvFactProposed, explicitDefaults(id, content, origin, tags)); err != nil {
		return "", err
	}
	return content, nil
}

func (s *Store) Accept(ctx context.Context, id string) error {
	return s.append(ctx, EvFactAccepted, decisionPayload{ID: id})
}

func (s *Store) Reject(ctx context.Context, id string) error {
	return s.append(ctx, EvFactRejected, decisionPayload{ID: id})
}

// Supersede appends a CORRECTION (strictly linear; validated inside the
// append transaction).
func (s *Store) Supersede(ctx context.Context, oldID, newID, content string, tags ...string) error {
	return s.append(ctx, EvFactSuperseded, supersedePayload{
		OldID: oldID, New: explicitDefaults(newID, content, OriginExplicit, tags)})
}

// latestAccepted is THE shared retrieval predicate: accepted, and not
// superseded by an accepted successor. Every model-facing read uses it
// (Phase-3 codex #6: a second filter WILL drift).
const latestAccepted = `f.status='accepted'
	AND NOT EXISTS (SELECT 1 FROM mem_facts n WHERE n.supersedes = f.id AND n.status='accepted')`

const maxQueryLen = 256
const resultLimit = 50

func (s *Store) queryRows(ctx context.Context, where, order string, args ...any) ([]Row, error) {
	q := `SELECT f.id, f.profile_id, f.content, f.origin, f.status, f.tags, f.trust, f.sensitivity
		FROM mem_facts f WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ` + fmt.Sprint(resultLimit)
	rows, err := s.j.QueryProjection(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var p, o, st, tags string
		var tr, se int
		if err := rows.Scan(&r.ID, &p, &r.Content, &o, &st, &tags, &tr, &se); err != nil {
			return nil, fmt.Errorf("memory: %w", err)
		}
		r.Profile = contracts.ProfileID(p)
		r.Origin, r.Status = Origin(o), Status(st)
		r.Trust, r.Sensitivity = contracts.TrustClass(tr), contracts.Sensitivity(se)
		if f := strings.Fields(tags); len(f) > 0 {
			r.Tags = f
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func checkQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", fmt.Errorf("memory: empty query (fail closed)")
	}
	if len(q) > maxQueryLen {
		return "", fmt.Errorf("memory: query exceeds %d bytes (fail closed)", maxQueryLen)
	}
	return q, nil
}

// Recall retrieves latest-accepted facts matching the FTS phrase, newest
// first. No decay: age never filters (B8).
func (s *Store) Recall(ctx context.Context, q string) ([]Row, error) {
	q, err := checkQuery(q)
	if err != nil {
		return nil, err
	}
	phrase := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	return s.queryRows(ctx,
		`f.rowid IN (SELECT rowid FROM mem_facts_fts WHERE mem_facts_fts MATCH ?) AND `+latestAccepted,
		"f.rowid DESC", phrase)
}

// Search is an alias retrieval surface with the SAME predicate.
func (s *Store) Search(ctx context.Context, q string) ([]Row, error) { return s.Recall(ctx, q) }

// RecallTag retrieves latest-accepted facts carrying the exact tag.
func (s *Store) RecallTag(ctx context.Context, tag string) ([]Row, error) {
	tag, err := checkQuery(tag)
	if err != nil {
		return nil, err
	}
	return s.queryRows(ctx, `f.tags LIKE ? AND `+latestAccepted, "f.rowid DESC", "% "+tag+" %")
}

// Exact retrieves the latest-accepted fact with BYTE-EXACT content.
func (s *Store) Exact(ctx context.Context, content string) ([]Row, error) {
	if content == "" {
		return nil, fmt.Errorf("memory: empty content (fail closed)")
	}
	return s.queryRows(ctx, `f.content = ? AND `+latestAccepted, "f.rowid DESC", content)
}

// All lists every fact regardless of status (verification/replay surface
// — never a model-facing retrieval path).
func (s *Store) All(ctx context.Context) ([]Row, error) {
	return s.queryRows(ctx, `1=1`, "f.rowid")
}

//go:build linux

// Package conv is the conversation READ MODEL (tgout convproj plan):
// a SyncProjection folding channel/turn lifecycle events into a
// per-turn table so conversationHistory is O(12) and recovery is O(1),
// replacing the per-turn full journal Replay(0) that caused the soak
// backlog explosion. The fold is observationally identical to the
// retained replay reference (proven by the differential oracle).
package conv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/MatNik89/nexus/internal/kernel/journal"
)

type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "conv" }

// Version 1: initial conv_turns schema.
func (Projection) Version() int { return 1 }

func (Projection) Init(db *journal.ProjDB) error {
	// hist_done is the completion MARKER (set by turn.succeeded only
	// after admission); hist_seq is ADMISSION order (nullable); the
	// partial index makes the 12-pair history query O(12) under an
	// incomplete tail. Provenance columns per Annex P0.3.
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS conv_turns (
			turn_id TEXT PRIMARY KEY,
			identity TEXT,
			update_id INTEGER,
			user_text TEXT,
			hist_done INTEGER NOT NULL DEFAULT 0,
			hist_final TEXT,
			rec_final TEXT,
			rec_state TEXT NOT NULL DEFAULT 'NONE',
			code TEXT,
			tool TEXT,
			susp_summary TEXT,
			created_seq INTEGER NOT NULL,
			hist_seq INTEGER,
			source_event_id TEXT NOT NULL,
			source_offset INTEGER NOT NULL,
			projection_version INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS conv_hist ON conv_turns(identity, hist_seq)
			WHERE hist_done=1 AND hist_seq IS NOT NULL;
	`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS conv_turns`)
	return err
}

// upsert creates the row on first sight (created_seq immutable) and
// always refreshes provenance to the last contributing event.
func upsert(tx *journal.ProjTx, turnID string, ev journal.Event) error {
	_, err := tx.Exec(`
		INSERT INTO conv_turns(turn_id, created_seq, source_event_id, source_offset, projection_version)
		VALUES(?1, ?2, ?3, ?2, 1)
		ON CONFLICT(turn_id) DO UPDATE SET source_event_id=?3, source_offset=?2`,
		turnID, ev.JournalOffset, string(ev.Envelope.EventID))
	return err
}

func (p Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	switch ev.Envelope.EventType {
	case "channel.inbound_admitted":
		var pl struct {
			ChannelIdentity string `json:"channel_identity"`
			UpdateID        int64  `json:"update_id"`
			Text            string `json:"text"`
		}
		if json.Unmarshal(ev.Envelope.Payload, &pl) != nil {
			return nil
		}
		turnID := fmt.Sprintf("turn-chan-%s-%d", pl.ChannelIdentity, pl.UpdateID)
		if err := upsert(tx, turnID, ev); err != nil {
			return err
		}
		// hist_seq = admission offset (history order); set once.
		// First admission wins (reference keeps the earliest user text
		// and admission offset): COALESCE both.
		_, err := tx.Exec(`UPDATE conv_turns SET identity=?, update_id=?,
			user_text=COALESCE(NULLIF(user_text,''), ?),
			hist_seq=COALESCE(hist_seq, ?) WHERE turn_id=?`,
			pl.ChannelIdentity, pl.UpdateID, pl.Text, ev.JournalOffset, turnID)
		return err
	case "turn.succeeded":
		if ev.Envelope.TurnID == nil {
			return nil
		}
		turnID := string(*ev.Envelope.TurnID)
		var pl struct {
			Final string `json:"final"`
		}
		json.Unmarshal(ev.Envelope.Payload, &pl)
		if err := upsert(tx, turnID, ev); err != nil {
			return err
		}
		// hist_done ONLY when admission already observed (hist_seq set):
		// a success preceding its admission is discarded and a later
		// admission never resurrects it (plan v4, mirrors the reference).
		if _, err := tx.Exec(`UPDATE conv_turns SET hist_done=1, hist_final=?
			WHERE turn_id=? AND hist_seq IS NOT NULL`, pl.Final, turnID); err != nil {
			return err
		}
		// recovery: SUCCEEDED requires a non-empty final (today's rule).
		if pl.Final != "" {
			_, err := tx.Exec(`UPDATE conv_turns SET rec_state='SUCCEEDED', rec_final=?,
				code=NULL, tool=NULL, susp_summary=NULL WHERE turn_id=?`, pl.Final, turnID)
			return err
		}
		return nil
	case "approval.turn_suspended":
		var pl struct {
			TurnID  string `json:"turn_id"`
			Summary string `json:"summary"`
		}
		if json.Unmarshal(ev.Envelope.Payload, &pl) != nil || pl.TurnID == "" || pl.Summary == "" {
			return nil
		}
		if err := upsert(tx, pl.TurnID, ev); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE conv_turns SET rec_state='SUSPENDED', susp_summary=?,
			code=NULL, tool=NULL, rec_final=NULL WHERE turn_id=?`, pl.Summary, pl.TurnID)
		return err
	case "turn.resumed":
		if ev.Envelope.TurnID == nil {
			return nil
		}
		turnID := string(*ev.Envelope.TurnID)
		if err := upsert(tx, turnID, ev); err != nil {
			return err
		}
		// resumed clears a stale suspension candidate.
		_, err := tx.Exec(`UPDATE conv_turns SET rec_state='NONE', susp_summary=NULL
			WHERE turn_id=? AND rec_state='SUSPENDED'`, turnID)
		return err
	case "turn.failed":
		if ev.Envelope.TurnID == nil {
			return nil
		}
		turnID := string(*ev.Envelope.TurnID)
		var pl struct {
			Code string `json:"error_code"`
			Tool string `json:"tool"`
		}
		json.Unmarshal(ev.Envelope.Payload, &pl)
		if err := upsert(tx, turnID, ev); err != nil {
			return err
		}
		// a terminal failure always overwrites recovery state.
		_, err := tx.Exec(`UPDATE conv_turns SET rec_state='FAILED', code=?, tool=?,
			susp_summary=NULL, rec_final=NULL WHERE turn_id=?`, pl.Code, pl.Tool, turnID)
		return err
	}
	return nil
}

// HistPair is one completed conversation pair, admission-ordered.
type HistPair struct {
	User  string
	Final string
}

// History returns up to n most-recent completed pairs for identity
// (excluding current), oldest-first. O(n) via the partial index.
func (Projection) History(ctx context.Context, j *journal.Journal, identity, current string, n int) ([]HistPair, error) {
	rows, err := j.QueryProjection(ctx,
		`SELECT user_text, hist_final FROM conv_turns
		 WHERE identity=? AND hist_done=1 AND hist_seq IS NOT NULL AND turn_id<>?
		 ORDER BY hist_seq DESC LIMIT ?`, identity, current, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistPair
	for rows.Next() {
		var h HistPair
		var final sql.NullString
		if err := rows.Scan(&h.User, &final); err != nil {
			return nil, err
		}
		h.Final = final.String
		out = append(out, h)
	}
	// reverse to oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// Recovered is the recovery outcome for one turn (rec_state NONE = no row).
type Recovered struct {
	State string
	Final string
	Code  string
	Tool  string
	Summ  string
}

func (Projection) Recovery(ctx context.Context, j *journal.Journal, turnID string) (Recovered, bool, error) {
	rows, err := j.QueryProjection(ctx,
		`SELECT rec_state, rec_final, code, tool, susp_summary FROM conv_turns WHERE turn_id=?`, turnID)
	if err != nil {
		return Recovered{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Recovered{}, false, rows.Err()
	}
	var r Recovered
	var final, code, tool, summ sql.NullString
	if err := rows.Scan(&r.State, &final, &code, &tool, &summ); err != nil {
		return Recovered{}, false, err
	}
	r.Final, r.Code, r.Tool, r.Summ = final.String, code.String, tool.String, summ.String
	if r.State == "NONE" {
		return Recovered{}, false, nil
	}
	return r, true, nil
}

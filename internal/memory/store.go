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
	"unicode"
	"unicode/utf8"

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
	// EvFactRemembered is Audn's own entry point (S9.4 P1, HARDQ B8 — fact-
	// type stays decay-exempt; this is the CONTRADICTION-RESOLUTION half,
	// not decay). Unlike every event above, the OUTCOME is not fixed by
	// the caller — Apply itself looks up the current latest-accepted fact
	// sharing ClaimKey (if any) and decides ADD/SUPERSEDE/PROPOSE+CONTEST
	// from what it finds, inside the SAME append transaction the leaf-
	// check for EvFactSuperseded already uses (serialized by the
	// journal's one append actor — no separate pre-check, no race).
	EvFactRemembered = "memory.fact_remembered"
)

// factPayload is the closed wire shape for saved/proposed facts.
type factPayload struct {
	ID          string                `json:"id"`
	Content     string                `json:"content"`
	Origin      Origin                `json:"origin"`
	Tags        []string              `json:"tags,omitempty"`
	Trust       contracts.TrustClass  `json:"trust_class"`
	Sensitivity contracts.Sensitivity `json:"sensitivity"`
	// Lineage carries the SOURCE chain (tool call ids, block ids) —
	// approval never erases provenance (Phase-3-r2 codex #7).
	Lineage []string `json:"lineage,omitempty"`
}

type decisionPayload struct {
	ID string `json:"id"`
}

type supersedePayload struct {
	OldID string      `json:"old_id"`
	New   factPayload `json:"new"`
	// ClaimKey optionally BOOTSTRAPS a pre-Audn fact into claim_key-based
	// lookup going forward (code-review finding, codex, round 1 HIGH:
	// without this, an EXISTING fact corrected via the named `supersedes`
	// path could never establish a key, so Audn could never improve the
	// pre-existing corpus it was built to help — every historical fact
	// keeps claim_key=NULL forever, permanently invisible to claim_key
	// lookups). The correction still targets the id the caller NAMED
	// explicitly; ClaimKey only labels the RESULT for future lookups.
	ClaimKey *string `json:"claim_key,omitempty"`
}

// validateClaimKey is the single shared rule for every payload that
// carries an optional claim key (rememberPayload, supersedePayload) —
// one definition so the two can never silently drift apart.
// validUTF8 rejects an invalid-UTF-8 id or content BEFORE any
// json.Marshal ever touches it (code-review finding, codex, round 6
// MEDIUM, live-reproduced). Checking this AFTER marshaling is too late
// and is dead code by construction: Go's encoding/json silently replaces
// invalid UTF-8 bytes with U+FFFD at Marshal time, so a validator that
// only ever sees already-marshaled-and-unmarshaled values (every
// Events() validator in this file does, by definition — see rememberV)
// can never observe the ORIGINAL invalid bytes at all. That silent
// substitution is exactly what caused a live counterexample: Remember's
// own post-append outcome lookup, keyed on the CALLER's original id,
// found nothing where the DB actually held the U+FFFD-substituted row —
// state committed, then still reported as an error. Called at every
// Store mutation entry point, on the caller's raw string arguments,
// before those arguments are ever marshaled for the first time.
// validUTF8 refuses invalid UTF-8 AND any string containing the U+FFFD
// replacement character (utf8.RuneError). The latter closes a narrower
// class than raw invalid bytes: a JSON `\uD800`-style escape for an
// unpaired UTF-16 surrogate is perfectly valid ASCII/UTF-8 TEXT in the
// wire bytes — utf8.Valid(raw) sees nothing wrong — but Go's own JSON
// decoder still substitutes U+FFFD for it while decoding, corrupting the
// string exactly like a raw invalid byte would, just via a different
// mechanism this function's caller can't see from the raw bytes alone.
// Rejecting the REPLACEMENT CHARACTER ITSELF, post-decode, catches every
// way a string could end up silently substituted, present or future,
// uniformly — never trying to enumerate the individual JSON escape forms
// that can trigger it.
func validUTF8(strs ...string) error {
	for _, s := range strs {
		if !utf8.ValidString(s) || strings.ContainsRune(s, utf8.RuneError) {
			return fmt.Errorf("memory: every id/content/replaces_id field must be valid UTF-8 with no replacement characters (fail closed)")
		}
	}
	return nil
}

func validateClaimKey(k *string) error {
	if k == nil {
		return nil
	}
	// ALLOW-list, not a deny-list (code-review finding, kilo, round 3
	// MEDIUM, live-reproduced: round 2's deny-list still missed Unicode
	// FORMAT characters — zero-width space U+200B, ZWNJ/ZWJ U+200C/D, BOM
	// U+FEFF — none of which are unicode.IsSpace or unicode.IsControl but
	// all invisible, so "server_ip" and one of these embedded mid-key
	// still coexisted as silently divergent shadow keys; a deny-list is
	// fragile BY CONSTRUCTION since it must enumerate every dangerous
	// Unicode category forever). An allow-list of exactly what a
	// claim_key legitimately needs — letters, digits, and a small set of
	// separator punctuation — closes the entire class at once, not just
	// the specific characters caught so far.
	v := *k
	if v == "" {
		return fmt.Errorf("memory: claim_key must be a non-empty single token (fail closed)")
	}
	for _, r := range v {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) ||
			r == '_' || r == '-' || r == '.' || r == ':'
		if !allowed {
			return fmt.Errorf("memory: claim_key must contain only letters, digits, '_', '-', '.', or ':' (fail closed)")
		}
	}
	return nil
}

// rememberPayload is EvFactRemembered's own closed shape — ClaimKey is
// OPTIONAL (nil means "no Audn involvement," Apply behaves exactly like
// EvFactSaved's plain accepted-insert) so every existing caller/behavior
// is preserved byte-for-byte when a claim key is never supplied.
// ReplacesID is the exact id of the fact the caller claims is CURRENTLY
// live under ClaimKey (code-review finding, kilo, round 1: a bare
// ClaimKey match is a WEAKER proof-of-awareness than the pre-Audn
// `supersedes`-by-id path, since a semantic label like "server_ip" is
// far more guessable/predictable by a prompt-injected instruction than
// an opaque per-call fact id). ID, not content, is the correctness bar:
// a content-based version of this check (round 2) was defeated by an ABA
// counterexample — A(V) superseded by B(W) superseded by C(V) — where an
// approval computed against A's content V would wrongly re-validate
// against C, which merely happens to share A's OLD content, and silently
// retarget onto the wrong fact (code-review finding, codex, round 2 HIGH,
// live-reproduced). A fact's id is assigned once at insert and its
// content is immutable thereafter (rows are only ever inserted, never
// UPDATEd), so id equality is both necessary AND sufficient proof the
// caller is looking at the SAME row, immune to any content cycle.
// ReplacesID comes from a real prior Recall — mirrors S6 tool-boundary
// piece 2's identical touched_files/old_name pattern.
type rememberPayload struct {
	New        factPayload `json:"new"`
	ClaimKey   *string     `json:"claim_key,omitempty"`
	ReplacesID *string     `json:"replaces_id,omitempty"`
}

func (p rememberPayload) validate() error {
	if err := p.New.validate(); err != nil {
		return err
	}
	return validateClaimKey(p.ClaimKey)
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
		if err := p.New.validate(); err != nil {
			return err
		}
		return validateClaimKey(p.ClaimKey)
	}
	rememberV := func(raw json.RawMessage) error {
		var p rememberPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return p.validate()
	}
	return map[string]journal.PayloadValidator{
		EvFactSaved: factV, EvFactProposed: factV,
		EvFactAccepted: decisionV, EvFactRejected: decisionV,
		EvFactSuperseded: supersedeV, EvFactRemembered: rememberV,
	}
}

// Projection folds memory events into queryable tables INSIDE the append
// transaction (B7 same-transaction core projection).
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "memory_facts" }

// Version 4: v3 lacked `contests` (a PENDING proposal's own link to the
// fact it disagrees with, resolved into a real `supersedes` link only at
// Accept time — code-review finding, codex, round 1 HIGH: without this,
// accepting a contested proposal never actually superseded the fact it
// contested, leaving both simultaneously "latest accepted"). A v3
// database RESETS and rebuilds, same precedent as prior version bumps.
// Version 5 adds mem_remember_outcomes (code-review finding, codex,
// round 3 MEDIUM, live-reproduced: a caller-id-reuse counterexample where
// the pre-existing row happened to belong to an UNRELATED claim proved
// that "does id already exist" is the wrong question — Remember's own
// outcome must be attributed by applyRemembered ITSELF, inside the same
// transaction, keyed by the call's own id (never shared between two
// calls, so reading it back afterward is race-free by construction —
// unlike re-deriving the outcome from generic claim_key/id state after
// the fact, which both editions before this one did and both got wrong
// in a different way).
func (Projection) Version() int { return 5 }

func (Projection) Reset(db *journal.ProjDB) error {
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS mem_facts`,
		`DROP TABLE IF EXISTS mem_facts_fts`,
		`DROP TABLE IF EXISTS mem_remember_outcomes`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mem_facts (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			content TEXT NOT NULL,
			origin TEXT NOT NULL,
			status TEXT NOT NULL,
			supersedes TEXT,
			claim_key TEXT,
			contention_count INTEGER NOT NULL DEFAULT 0,
			contests TEXT,
			tags TEXT NOT NULL DEFAULT '',
			trust INTEGER NOT NULL,
			sensitivity INTEGER NOT NULL,
			lineage TEXT NOT NULL DEFAULT '[]',
			created INTEGER NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS ux_mem_supersedes
			ON mem_facts(supersedes) WHERE supersedes IS NOT NULL;
		CREATE INDEX IF NOT EXISTS ix_mem_claim_key
			ON mem_facts(profile_id, claim_key) WHERE claim_key IS NOT NULL;
		CREATE VIRTUAL TABLE IF NOT EXISTS mem_facts_fts USING fts5(content);
		CREATE TABLE IF NOT EXISTS mem_remember_outcomes (
			call_id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			effective_id TEXT NOT NULL,
			intent TEXT NOT NULL
		);
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
	insert := func(f factPayload, status Status, supersedes, claimKey, contests *string) error {
		// An id already RESERVED by a Remember no-op outcome (which
		// never touches mem_facts at all — see applyRemembered) must
		// refuse EVERY insert path uniformly, not just Remember's own
		// (code-review finding, codex, round 6 MEDIUM, live-reproduced:
		// Remember("reserved", V, claim=K, replaces=owner) lands on the
		// identical-restatement no-op branch and reserves "reserved" only
		// in mem_remember_outcomes; a later SaveFact("reserved", ...)
		// never checks that table and succeeds, silently reusing an id
		// this feature's own stated invariant says can never be reused).
		// Checking here, in the ONE insert path shared by EvFactSaved,
		// EvFactProposed, and every applyRemembered branch, closes it
		// uniformly instead of re-guarding each caller separately.
		reserved, rerr := tx.Query(`SELECT 1 FROM mem_remember_outcomes WHERE call_id=?`, f.ID)
		if rerr != nil {
			return rerr
		}
		isReserved := reserved.Next()
		if cerr := reserved.Close(); cerr != nil {
			return cerr
		}
		if isReserved {
			return fmt.Errorf("memory: id %q was already used for a different write — ids must never be reused across distinct writes (fail closed)", f.ID)
		}
		lineage, lerr := json.Marshal(f.Lineage)
		if lerr != nil {
			return lerr
		}
		res, err := tx.Exec(`INSERT INTO mem_facts
			(id, profile_id, content, origin, status, supersedes, claim_key, contests, tags, trust, sensitivity, lineage, created)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			f.ID, string(ev.Envelope.ProfileID), f.Content, string(f.Origin), string(status),
			supersedes, claimKey, contests, tagBlob(f.Tags), int(f.Trust), int(f.Sensitivity), string(lineage), int64(ev.JournalOffset))
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
	// supersede is the shared leaf-check + lineage-union + insert-as-
	// accepted-successor logic — originally EvFactSuperseded's own inline
	// body, extracted so EvFactRemembered's Audn auto-resolve branch
	// reuses the EXACT same correctness checks (strictly-linear history,
	// provenance union) rather than a second, driftable copy.
	supersede := func(oldID string, newF factPayload, claimKey *string) error {
		// Leaf check INSIDE the append transaction (serialized by the one
		// append actor): only an ACCEPTED fact with NO successor of any
		// status can be corrected — strictly linear history. The unique
		// index is the DB-level backstop.
		rows, err := tx.Query(`SELECT
			(SELECT status FROM mem_facts WHERE id=?1),
			EXISTS(SELECT 1 FROM mem_facts WHERE supersedes=?1)`, oldID)
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
		// Provenance UNION (Phase-3-r3 codex #7): the correction carries
		// the predecessor's lineage ∪ the predecessor id ∪ the correction
		// call's own lineage — approval/correction never erases sources.
		var oldLineageRaw string
		lr, err := tx.Query(`SELECT lineage FROM mem_facts WHERE id=?`, oldID)
		if err != nil {
			return err
		}
		if lr.Next() {
			if err := lr.Scan(&oldLineageRaw); err != nil {
				lr.Close()
				return err
			}
		}
		if err := lr.Close(); err != nil {
			return err
		}
		var oldLineage []string
		json.Unmarshal([]byte(oldLineageRaw), &oldLineage)
		seen := map[string]bool{}
		merged := []string{}
		for _, l := range append(append(oldLineage, oldID), newF.Lineage...) {
			if l != "" && !seen[l] {
				seen[l] = true
				merged = append(merged, l)
			}
		}
		newF.Lineage = merged
		if claimKey != nil {
			// Detector (code-review finding, codex, round 2 HIGH, live-
			// reproduced): bootstrapping a claim_key onto a NAMED
			// correction, with no uniqueness check, let two unrelated
			// lineages each claim the SAME key — two simultaneously
			// "latest accepted" facts under one claim_key, silently
			// splitting future Audn lookups (whichever has the higher
			// rowid wins, the other orphaned but still live). Refuse
			// unless the current live owner (if any) IS the fact being
			// superseded — that's the normal, expected case.
			owner, oerr := tx.Query(`SELECT f.id FROM mem_facts f
				WHERE f.profile_id=? AND f.claim_key=? AND `+latestAccepted, string(ev.Envelope.ProfileID), *claimKey)
			if oerr != nil {
				return oerr
			}
			var ownerID string
			hasOwner := owner.Next()
			if hasOwner {
				if err := owner.Scan(&ownerID); err != nil {
					owner.Close()
					return err
				}
			}
			if cerr := owner.Close(); cerr != nil {
				return cerr
			}
			if hasOwner && ownerID != oldID {
				return fmt.Errorf("memory: claim_key %q is already owned by a different fact (%s) — cannot bootstrap a second live owner (fail closed)", *claimKey, ownerID)
			}
		}
		return insert(newF, StatusAccepted, &oldID, claimKey, nil)
	}
	// resolveContest converts a PENDING contest into a REAL supersede link
	// at the moment a human Accepts the contesting proposal (code-review
	// finding, codex, round 1 HIGH: previously Accept only flipped status,
	// so an accepted contradiction and the fact it contested stayed
	// SIMULTANEOUSLY "latest accepted" — this closes it). Runs the EXACT
	// same leaf-check + lineage-union discipline as supersede, but as an
	// UPDATE against the already-inserted (now-accepted) row rather than a
	// fresh INSERT. A REJECTED proposal needs no equivalent step — its own
	// disqualifying status already excludes it from latestAccepted, and it
	// never held a supersedes link, so the contested fact is automatically
	// left exactly as it was.
	resolveContest := func(rowID, contestedID string) error {
		rows, err := tx.Query(`SELECT
			(SELECT status FROM mem_facts WHERE id=?1),
			EXISTS(SELECT 1 FROM mem_facts WHERE supersedes=?1)`, contestedID)
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
			return fmt.Errorf("memory: cannot resolve contest — the contested fact no longer exists (fail closed)")
		}
		if Status(status.String) != StatusAccepted {
			return fmt.Errorf("memory: cannot resolve contest — the contested fact is no longer the latest accepted (fail closed)")
		}
		if hasSuccessor {
			return fmt.Errorf("memory: cannot resolve contest — the contested fact already has a successor (fail closed)")
		}
		var contestedLineageRaw, ownLineageRaw string
		lr, err := tx.Query(`SELECT lineage FROM mem_facts WHERE id=?`, contestedID)
		if err != nil {
			return err
		}
		if lr.Next() {
			if err := lr.Scan(&contestedLineageRaw); err != nil {
				lr.Close()
				return err
			}
		}
		if err := lr.Close(); err != nil {
			return err
		}
		or, err := tx.Query(`SELECT lineage FROM mem_facts WHERE id=?`, rowID)
		if err != nil {
			return err
		}
		if or.Next() {
			if err := or.Scan(&ownLineageRaw); err != nil {
				or.Close()
				return err
			}
		}
		if err := or.Close(); err != nil {
			return err
		}
		var contestedLineage, ownLineage []string
		json.Unmarshal([]byte(contestedLineageRaw), &contestedLineage)
		json.Unmarshal([]byte(ownLineageRaw), &ownLineage)
		seen := map[string]bool{}
		merged := []string{}
		for _, l := range append(append(contestedLineage, contestedID), ownLineage...) {
			if l != "" && !seen[l] {
				seen[l] = true
				merged = append(merged, l)
			}
		}
		mergedRaw, merr := json.Marshal(merged)
		if merr != nil {
			return merr
		}
		_, err = tx.Exec(`UPDATE mem_facts SET supersedes=?, lineage=?, contests=NULL WHERE id=?`,
			contestedID, string(mergedRaw), rowID)
		return err
	}
	decide := func(to Status, raw json.RawMessage) error {
		var d decisionPayload
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		var contests sql.NullString
		rows, err := tx.Query(`SELECT contests FROM mem_facts WHERE id=? AND status='proposed'`, d.ID)
		if err != nil {
			return err
		}
		found := rows.Next()
		if found {
			if err := rows.Scan(&contests); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("memory: fact is not awaiting review (unknown, or already decided — decisions are terminal)")
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
		if contests.Valid && contests.String != "" {
			if to == StatusAccepted {
				return resolveContest(d.ID, contests.String)
			}
			// Rejecting a contest RESOLVES it too — just in favor of the
			// original, not the challenger. The contention_count
			// increment at proposal time was a "this is being actively
			// disputed" signal; leaving it standing forever after the
			// dispute is over would keep recall falsely warning about an
			// "unresolved claim" against a fact that was never actually
			// in question again (code-review finding, codex, round 2
			// NOTE, live-reproduced: not model-reachable today since
			// OriginInferred has no production producer, but a real gap
			// once one exists).
			_, err := tx.Exec(`UPDATE mem_facts SET contention_count = contention_count - 1 WHERE id=? AND contention_count > 0`, contests.String)
			return err
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
		return insert(f, StatusAccepted, nil, nil, nil)
	case EvFactProposed:
		var f factPayload
		if err := json.Unmarshal(raw, &f); err != nil {
			return err
		}
		return insert(f, StatusProposed, nil, nil, nil)
	case EvFactAccepted:
		return decide(StatusAccepted, raw)
	case EvFactRejected:
		return decide(StatusRejected, raw)
	case EvFactSuperseded:
		var p supersedePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return supersede(p.OldID, p.New, p.ClaimKey)
	case EvFactRemembered:
		return applyRemembered(tx, ev, raw, insert, supersede)
	}
	return nil // not a memory event
}

// applyRemembered is Audn's own decision logic (S9.4 P1, HARDQ B8):
//   - no ClaimKey → identical to EvFactSaved's plain accepted-insert.
//   - ClaimKey with no current latest-accepted match → plain accepted-insert,
//     claim_key recorded for future lookups.
//   - ClaimKey matches an existing latest-accepted fact with IDENTICAL
//     content → true no-op (a restatement, not a contradiction — nothing
//     new to record).
//   - ClaimKey matches with DIFFERENT content: ReplacesID MUST equal the
//     match's CURRENT id exactly (code-review finding, kilo+codex, round 1
//     HIGH, live-reproduced by both independently: a bare claim_key match
//     is a WEAKER proof-of-awareness than the pre-Audn `supersedes`-by-id
//     path — a semantic label like "server_ip" is far more
//     guessable/predictable by a prompt-injected instruction than an opaque
//     per-call fact id, AND the fact actually targeted is resolved LATER
//     than approval time, so a stale approval could silently retarget a
//     DIFFERENT fact than the one the approver saw; a content-based version
//     of this check was itself defeated by an ABA content-cycle — codex,
//     round 2 HIGH, live-reproduced — id is immutable per row and never
//     reused, closing that). Missing or stale ReplacesID refuses outright —
//     fail closed, re-derived fresh inside this SAME transaction, never
//     trusted from approval time.
//   - ReplacesID verified, safe to auto-resolve (never an inference
//     silently overriding a previously EXPLICIT fact — HARDQ-agy Finding 4,
//     generalized) → SUPERSEDE, reusing the exact same leaf-check/lineage-
//     union path EvFactSuperseded uses.
//   - ReplacesID verified, UNSAFE to auto-resolve (existing is
//     explicit, incoming is only inferred) → insert as PROPOSED with
//     `contests` set to the existing fact's id (resolved into a REAL
//     supersede link only if a human later Accepts it — see
//     resolveContest) and increment the existing fact's contention_count
//     (the design doc's own "signal, never a silent drop").
func applyRemembered(tx *journal.ProjTx, ev journal.Event, raw json.RawMessage,
	insert func(factPayload, Status, *string, *string, *string) error,
	supersede func(string, factPayload, *string) error) error {
	var p rememberPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	// Intent is a CANONICAL re-marshal of the DECODED payload, never the
	// raw event bytes (code-review finding, codex, round 5 MEDIUM, live-
	// reproduced: raw is the POST-REDACTION journal payload — the known-
	// ref redactor reserializes JSON with sorted keys whenever ANY
	// reference is configured, even one unrelated to this content, so
	// the SAME logical call could journal to different byte strings on
	// two occasions depending on unrelated redactor state, wrongly
	// refusing a genuine retry as "different intent". Re-marshaling the
	// already-decoded `p` is redaction-independent and deterministic —
	// rememberPayload has no maps/floats, so Go's json.Marshal produces
	// the same bytes for the same field values every time).
	intentRaw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	intent := string(intentRaw)
	// Exact-intent idempotent-retry check, done FIRST, before any
	// decision logic runs at all (code-review finding, codex, round 4
	// MEDIUM, live-reproduced: an earlier UPSERT-based recordOutcome let
	// a retry of a MUTATING first attempt — "added" or "superseded" —
	// silently rewrite its own durable outcome to "noop", because the
	// retry's own claim_key lookup found ITSELF as the new owner and
	// concluded "identical restatement"; re-running the decision logic
	// at all on a retry is the bug, not just how its result gets stored).
	// Same id, SAME intent → the ORIGINAL outcome stands, untouched, no
	// decision logic re-run (a true no-op retry). Same id, DIFFERENT
	// intent → refuse outright: reusing an id for two distinct writes is
	// exactly the class of collision this feature has hardened against
	// every round so far (round 2's unrelated-claim bootstrap guard,
	// round 3's unrelated-claim attribution fix — same principle here).
	//
	// The guard must ALSO catch an id that already denotes a fact
	// created OUTSIDE Remember entirely — SaveFact/Propose/the named
	// supersedes path never write to mem_remember_outcomes at all (code-
	// review finding, codex, round 5 MEDIUM, live-reproduced: a pre-Audn
	// SaveFact id, reused for a Remember call that happens to land on
	// the identical-restatement no-op branch, was accepted instead of
	// refused, since that branch never inserts into mem_facts either —
	// nothing backstopped it). Checking mem_facts directly closes this:
	// every Remember success that DOES touch mem_facts (added/superseded/
	// proposed) inserts BOTH rows in the same transaction, so mem_facts
	// having this id with NO matching mem_remember_outcomes entry can
	// only mean the id was already spoken for by something else.
	factRows, err := tx.Query(`SELECT 1 FROM mem_facts WHERE id=?`, p.New.ID)
	if err != nil {
		return err
	}
	factExists := factRows.Next()
	if err := factRows.Close(); err != nil {
		return err
	}
	existing, err := tx.Query(`SELECT intent FROM mem_remember_outcomes WHERE call_id=?`, p.New.ID)
	if err != nil {
		return err
	}
	var existingIntent string
	foundExisting := existing.Next()
	if foundExisting {
		if err := existing.Scan(&existingIntent); err != nil {
			existing.Close()
			return err
		}
	}
	if err := existing.Close(); err != nil {
		return err
	}
	if foundExisting && existingIntent == intent {
		return nil // exact retry: the original outcome stands, untouched
	}
	if foundExisting || factExists {
		return fmt.Errorf("memory: id %q was already used for a different write — ids must never be reused across distinct writes (fail closed)", p.New.ID)
	}
	// Fresh id: run the normal decision logic, then record the outcome —
	// a plain INSERT is safe here (never a conflict), since the check
	// above, inside this SAME transaction, just proved no row exists yet.
	recordOutcome := func(kind, effectiveID string) error {
		_, err := tx.Exec(`INSERT INTO mem_remember_outcomes(call_id, kind, effective_id, intent) VALUES(?,?,?,?)`,
			p.New.ID, kind, effectiveID, intent)
		return err
	}
	if p.ClaimKey == nil {
		if err := insert(p.New, StatusAccepted, nil, nil, nil); err != nil {
			return err
		}
		return recordOutcome("added", p.New.ID)
	}
	rows, err := tx.Query(`SELECT f.id, f.content, f.origin FROM mem_facts f
		WHERE f.profile_id=? AND f.claim_key=? AND `+latestAccepted+`
		ORDER BY f.rowid DESC LIMIT 1`, string(ev.Envelope.ProfileID), *p.ClaimKey)
	if err != nil {
		return err
	}
	var existingID, existingContent, existingOriginRaw string
	found := false
	if rows.Next() {
		if err := rows.Scan(&existingID, &existingContent, &existingOriginRaw); err != nil {
			rows.Close()
			return err
		}
		found = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		if err := insert(p.New, StatusAccepted, nil, p.ClaimKey, nil); err != nil {
			return err
		}
		return recordOutcome("added", p.New.ID)
	}
	if existingContent == p.New.Content {
		// identical restatement — not a contradiction, nothing new to
		// record, but the OUTCOME still needs recording: the effective
		// owner is the PRE-EXISTING fact, never this call's own id.
		return recordOutcome("noop", existingID)
	}
	if p.ReplacesID == nil || *p.ReplacesID != existingID {
		return fmt.Errorf("memory: claim_key %q is no longer held by the fact replaces_id named — call recall first for a fresh value (fail closed)", *p.ClaimKey)
	}
	if Origin(existingOriginRaw) == OriginExplicit && p.New.Origin == OriginInferred {
		// Unsafe to auto-resolve: an inference must never silently
		// override a previously explicit fact. Route to the EXISTING
		// review gate instead of inventing a new one. No production
		// caller sets OriginInferred today (memory_remember always
		// passes OriginExplicit) — replaces_content above is the actual
		// safety bar for every current caller. This branch is reserved
		// for the future inferred-fact producer HARDQ B8 anticipates,
		// not dead code (code-review finding, kilo, round 2 NOTE).
		if err := insert(p.New, StatusProposed, nil, p.ClaimKey, &existingID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE mem_facts SET contention_count = contention_count + 1 WHERE id=?`, existingID); err != nil {
			return err
		}
		return recordOutcome("proposed", p.New.ID)
	}
	if err := supersede(existingID, p.New, p.ClaimKey); err != nil {
		return err
	}
	return recordOutcome("superseded", p.New.ID)
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
	Lineage     []string
	// ClaimKey is Audn's own comparison key (S9.4 P1) — empty when this
	// fact was never written through Remember. ContentionCount counts how
	// many times an UNSAFE-to-auto-resolve contradiction (see
	// applyRemembered) named this fact as the one it disagreed with — a
	// visibility signal (docs/DESIGN-memory-effectpath-kilo.md's own
	// "signal for semantic-health, never a silent drop"), not itself a
	// gate on anything.
	ClaimKey        string
	ContentionCount int
}

// Store is the memory facade over the profile's ONE journal.
type Store struct {
	j   *journal.Journal
	seq atomic.Uint64
}

// NewStore binds the facade to the profile journal. The exact-bytes check
// consults the JOURNAL'S OWN redactor (Phase-3-r3 codex #6: an injected
// redactor copy could disagree with what the append stores — the seam is
// journal-owned and cannot be mis-wired).
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
	// The exact-bytes gate covers the WHOLE event payload — content, tags,
	// ids, everything the journal would redact (Phase-3-r4 codex #4: a
	// secret smuggled into a tag earned a receipt while the stored tag was
	// rewritten). Bytes commit verbatim or not at all.
	rewrites, rerr := s.j.RedactorRewrites(raw)
	if rerr != nil {
		return fmt.Errorf("memory: redaction check: %w", rerr)
	}
	if rewrites {
		return fmt.Errorf("memory: payload contains a known secret reference — refused (store the secret's LOCATION, never its value)")
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

func explicitDefaults(id, content string, origin Origin, tags, lineage []string) factPayload {
	trust := contracts.TrustToolTrusted
	if origin == OriginInferred {
		// Inferences derive from conversation context that may include
		// untrusted material — monotone: never MORE trusted than source.
		trust = contracts.TrustUntrustedExternal
	}
	return factPayload{ID: id, Content: content, Origin: origin, Tags: tags,
		Trust: trust, Sensitivity: contracts.SensitivityConfidential, Lineage: lineage}
}

// SaveFact stores one already-approved EXPLICIT fact (the PEP approval
// flow ran; T18 seeding path).
func (s *Store) SaveFact(ctx context.Context, id, content string, tags ...string) error {
	return s.SaveFactLineage(ctx, id, content, tags, nil)
}

// SaveFactLineage carries the source chain (Phase-3-r2 codex #7).
func (s *Store) SaveFactLineage(ctx context.Context, id, content string, tags, lineage []string) error {
	if err := validUTF8(append([]string{id, content}, append(tags, lineage...)...)...); err != nil {
		return err
	}
	return s.append(ctx, EvFactSaved, explicitDefaults(id, content, OriginExplicit, tags, lineage))
}

// RememberOutcome reports what applyRemembered ACTUALLY decided — a
// caller must never fabricate a receipt for an id that was never
// inserted (code-review finding, codex, round 1 MEDIUM, live-reproduced:
// the identical-restatement no-op path previously still reported
// "remembered (fact-X)" for an id that was never written at all).
type RememberOutcome struct {
	Kind string // "added" | "noop" | "superseded" | "proposed"
	// EffectiveID is the id that now ACTUALLY holds this claim: the new
	// id for "added"/"superseded"/"proposed", or the PRE-EXISTING id for
	// "noop" (the restatement changed nothing).
	EffectiveID string
}

// Remember is Audn's own entry point (S9.4 P1): claimKey identifies what
// this fact is ABOUT, for automatic contradiction resolution against the
// current latest-accepted fact sharing it (see applyRemembered for the
// exact decision table). An empty claimKey behaves EXACTLY like
// SaveFactLineage — every existing caller that never opts into Audn sees
// byte-identical behavior. replacesID is REQUIRED whenever the caller
// believes claimKey already names an existing fact — Apply refuses (fail
// closed) if it does not match that fact's CURRENT id exactly, re-derived
// fresh inside the same transaction (code-review finding, kilo+codex,
// round 1 HIGH, ABA fix codex round 2 HIGH — see applyRemembered's own
// doc comment for the full rationale).
func (s *Store) Remember(ctx context.Context, id, content string, origin Origin, claimKey, replacesID string, tags, lineage []string) (RememberOutcome, error) {
	if err := validUTF8(append([]string{id, content, claimKey, replacesID}, append(tags, lineage...)...)...); err != nil {
		return RememberOutcome{}, err
	}
	var ck, rid *string
	if claimKey != "" {
		ck = &claimKey
	}
	if replacesID != "" {
		rid = &replacesID
	}
	if err := s.append(ctx, EvFactRemembered, rememberPayload{
		New: explicitDefaults(id, content, origin, tags, lineage), ClaimKey: ck, ReplacesID: rid}); err != nil {
		return RememberOutcome{}, err
	}
	// applyRemembered recorded its OWN decision, inside the same append
	// transaction, keyed by this call's own id — never derived here from
	// generic post-hoc mem_facts state (code-review finding, codex,
	// round 3 MEDIUM, live-reproduced: the prior "does id already exist"
	// heuristic misattributed a no-op's EffectiveID to this call's own id
	// whenever that id happened to collide with an UNRELATED pre-existing
	// fact, and was separately racy against a concurrent write to the
	// same claim_key). id is unique per call, so this read can never
	// observe another call's row.
	rows, err := s.j.QueryProjection(ctx, `SELECT kind, effective_id FROM mem_remember_outcomes WHERE call_id=?`, id)
	if err != nil {
		return RememberOutcome{}, fmt.Errorf("memory: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return RememberOutcome{}, fmt.Errorf("memory: internal inconsistency: append succeeded but recorded no outcome for %q", id)
	}
	var kind, effectiveID string
	if err := rows.Scan(&kind, &effectiveID); err != nil {
		return RememberOutcome{}, fmt.Errorf("memory: %w", err)
	}
	return RememberOutcome{Kind: kind, EffectiveID: effectiveID}, nil
}

// Propose enters a fact into the REVIEW state and returns the EXACT
// content as its preview. Nothing proposed is recallable until accepted.
func (s *Store) Propose(ctx context.Context, id, content string, origin Origin, tags ...string) (string, error) {
	if err := validUTF8(append([]string{id, content}, tags...)...); err != nil {
		return "", err
	}
	if err := s.append(ctx, EvFactProposed, explicitDefaults(id, content, origin, tags, nil)); err != nil {
		return "", err
	}
	return content, nil
}

func (s *Store) Accept(ctx context.Context, id string) error {
	if err := validUTF8(id); err != nil {
		return err
	}
	return s.append(ctx, EvFactAccepted, decisionPayload{ID: id})
}

func (s *Store) Reject(ctx context.Context, id string) error {
	if err := validUTF8(id); err != nil {
		return err
	}
	return s.append(ctx, EvFactRejected, decisionPayload{ID: id})
}

// Supersede appends a CORRECTION (strictly linear; validated inside the
// append transaction).
func (s *Store) Supersede(ctx context.Context, oldID, newID, content string, tags ...string) error {
	return s.SupersedeLineage(ctx, oldID, newID, content, tags, nil)
}

// SupersedeLineage carries the correction's own source chain; Apply
// unions it with the predecessor's stored lineage.
func (s *Store) SupersedeLineage(ctx context.Context, oldID, newID, content string, tags, lineage []string) error {
	return s.SupersedeLineageWithClaim(ctx, oldID, newID, content, "", tags, lineage)
}

// SupersedeLineageWithClaim is SupersedeLineage plus an optional claimKey
// stored on the NEW fact — BOOTSTRAPPING a pre-Audn fact (whose history
// has claim_key=NULL) into claim_key-based lookup going forward (code-
// review finding, codex, round 1 HIGH: without this, Audn could never
// improve the EXISTING corpus it was built to help — every historical
// fact stays permanently invisible to claim_key lookups). The correction
// still targets the id the caller NAMED explicitly; claimKey only labels
// the RESULT.
func (s *Store) SupersedeLineageWithClaim(ctx context.Context, oldID, newID, content, claimKey string, tags, lineage []string) error {
	if err := validUTF8(append([]string{oldID, newID, content, claimKey}, append(tags, lineage...)...)...); err != nil {
		return err
	}
	var ck *string
	if claimKey != "" {
		ck = &claimKey
	}
	return s.append(ctx, EvFactSuperseded, supersedePayload{
		OldID: oldID, New: explicitDefaults(newID, content, OriginExplicit, tags, lineage), ClaimKey: ck})
}

// latestAccepted is THE shared retrieval predicate: accepted, and not
// superseded by an accepted successor. Every model-facing read uses it
// (Phase-3 codex #6: a second filter WILL drift).
const latestAccepted = `f.status='accepted'
	AND NOT EXISTS (SELECT 1 FROM mem_facts n WHERE n.supersedes = f.id AND n.status='accepted')`

const maxQueryLen = 256
const resultLimit = 50

func (s *Store) queryRows(ctx context.Context, where, order string, args ...any) ([]Row, error) {
	q := `SELECT f.id, f.profile_id, f.content, f.origin, f.status, f.tags, f.trust, f.sensitivity, f.lineage, f.claim_key, f.contention_count
		FROM mem_facts f WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ` + fmt.Sprint(resultLimit)
	rows, err := s.j.QueryProjection(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var p, o, st, tags, lineage string
		var tr, se int
		var claimKey sql.NullString
		if err := rows.Scan(&r.ID, &p, &r.Content, &o, &st, &tags, &tr, &se, &lineage, &claimKey, &r.ContentionCount); err != nil {
			return nil, fmt.Errorf("memory: %w", err)
		}
		json.Unmarshal([]byte(lineage), &r.Lineage)
		r.Profile = contracts.ProfileID(p)
		r.Origin, r.Status = Origin(o), Status(st)
		r.Trust, r.Sensitivity = contracts.TrustClass(tr), contracts.Sensitivity(se)
		r.ClaimKey = claimKey.String
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
	// LIKE metacharacters in the model-controlled tag are ESCAPED — a
	// "%" tag matches only a literal "%" tag, never everything
	// (Phase-3-r2 codex #8).
	esc := strings.NewReplacer("|", "||", "%", "|%", "_", "|_").Replace(tag)
	return s.queryRows(ctx, `f.tags LIKE ? ESCAPE '|' AND `+latestAccepted, "f.rowid DESC", "% "+esc+" %")
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

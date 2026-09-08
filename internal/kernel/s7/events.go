//go:build linux

package s7

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/machine"
)

// Durable S7 events (Durable policies only). The s7_operations projection
// folds them; New rehydrates from it. The grant nonce (a bearer secret) is
// NEVER journaled — its SHA-256 identifies the lease.
const (
	EvOperationBegun     = "s7.operation_begun"
	EvAttemptAuthorized  = "s7.attempt_authorized"
	EvAttemptStarted     = "s7.attempt_started"
	EvLeaseRevoked       = "s7.lease_revoked"
	EvAttemptReported    = "s7.attempt_reported"
	EvOperationTerminal  = "s7.operation_terminal"
	EvOperationReconcile = "s7.operation_reconciled"
)

type begunPayload struct {
	Op       string          `json:"op"`
	Target   string          `json:"target"`
	Policy   json.RawMessage `json:"policy"`
	Deadline int64           `json:"deadline_unix,omitempty"`
}
type authorizedPayload struct {
	Op        string `json:"op"`
	AttemptNo int    `json:"attempt_no"`
	NonceHash string `json:"nonce_hash"`
	ExpiresAt int64  `json:"expires_unix"`
}
type startedPayload struct {
	Op        string `json:"op"`
	AttemptNo int    `json:"attempt_no"`
}
type revokedPayload struct {
	Op        string `json:"op"`
	AttemptNo int    `json:"attempt_no"`
	NonceHash string `json:"nonce_hash"`
}
type reportedPayload struct {
	Op        string `json:"op"`
	AttemptNo int    `json:"attempt_no"`
	Outcome   int    `json:"outcome"`
	Code      string `json:"code,omitempty"`
	Landing   int    `json:"landing"`
	NextAt    int64  `json:"next_attempt_unix,omitempty"`
}
type terminalPayload struct {
	Op    string `json:"op"`
	State string `json:"state"`
}
type reconcilePayload struct {
	Op     string `json:"op"`
	State  string `json:"state"`
	NextAt int64  `json:"next_attempt_unix,omitempty"`
}

// Events registers the closed s7.* event set with strict payload validators.
func Events() map[string]journal.PayloadValidator {
	req := func(name string) error { return fmt.Errorf("s7: %s requires op", name) }
	return map[string]journal.PayloadValidator{
		EvOperationBegun: func(raw json.RawMessage) error {
			var p begunPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.Target == "" || len(p.Policy) == 0 {
				return fmt.Errorf("s7: operation_begun requires op, target and policy")
			}
			_, err := unmarshalPolicy(p.Policy)
			return err
		},
		EvAttemptAuthorized: func(raw json.RawMessage) error {
			var p authorizedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.AttemptNo < 1 || len(p.NonceHash) != 64 || p.ExpiresAt == 0 {
				return fmt.Errorf("s7: attempt_authorized requires op, attempt_no>=1, nonce_hash, expires")
			}
			return nil
		},
		EvAttemptStarted: func(raw json.RawMessage) error {
			var p startedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.AttemptNo < 1 {
				return req("attempt_started")
			}
			return nil
		},
		EvLeaseRevoked: func(raw json.RawMessage) error {
			var p revokedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || len(p.NonceHash) != 64 {
				return req("lease_revoked")
			}
			return nil
		},
		EvAttemptReported: func(raw json.RawMessage) error {
			var p reportedPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.AttemptNo < 1 || p.Outcome < 1 || p.Landing < 1 {
				return req("attempt_reported")
			}
			return nil
		},
		EvOperationTerminal: func(raw json.RawMessage) error {
			var p terminalPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.State == "" {
				return req("operation_terminal")
			}
			return nil
		},
		EvOperationReconcile: func(raw json.RawMessage) error {
			var p reconcilePayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.State == "" {
				return req("operation_reconciled")
			}
			return nil
		},
	}
}

var eventSeq atomic.Int64

func (a *Authority) params(eventType string, payload any) contracts.EnvelopeParams {
	raw, _ := json.Marshal(payload)
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-s7-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), eventSeq.Add(1))),
		EventType: eventType, RunID: "run-s7", EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "s7", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: a.j.Profile(), AttemptNo: 1,
		Payload: raw, PayloadHash: "recomputed",
	}
}

func (a *Authority) evBegun(op contracts.OperationID, rec *operation) (contracts.EnvelopeParams, error) {
	pol, err := marshalPolicy(rec.policy)
	if err != nil {
		return contracts.EnvelopeParams{}, err
	}
	p := begunPayload{Op: string(op), Target: string(rec.target), Policy: pol}
	if !rec.deadline.IsZero() {
		p.Deadline = rec.deadline.Unix()
	}
	return a.params(EvOperationBegun, p), nil
}
func (a *Authority) evAuthorized(op contracts.OperationID, g Grant, nonceHash string) contracts.EnvelopeParams {
	return a.params(EvAttemptAuthorized, authorizedPayload{Op: string(op), AttemptNo: g.AttemptNo, NonceHash: nonceHash, ExpiresAt: g.ExpiresAt.Unix()})
}
func (a *Authority) evStarted(op contracts.OperationID, attemptNo int) contracts.EnvelopeParams {
	return a.params(EvAttemptStarted, startedPayload{Op: string(op), AttemptNo: attemptNo})
}
func (a *Authority) evLeaseRevoked(op contracts.OperationID, rec *operation) contracts.EnvelopeParams {
	return a.params(EvLeaseRevoked, revokedPayload{Op: string(op), AttemptNo: rec.attempts + 1, NonceHash: rec.nonceHash})
}
func (a *Authority) evReported(op contracts.OperationID, attemptNo int, outcome Outcome, code string, l Landing) contracts.EnvelopeParams {
	p := reportedPayload{Op: string(op), AttemptNo: attemptNo, Outcome: int(outcome), Code: code, Landing: int(l.Kind)}
	if !l.NextAt.IsZero() {
		p.NextAt = l.NextAt.Unix()
	}
	return a.params(EvAttemptReported, p)
}
func (a *Authority) evTerminal(op contracts.OperationID, state contracts.AttemptState) contracts.EnvelopeParams {
	return a.params(EvOperationTerminal, terminalPayload{Op: string(op), State: state.String()})
}

// appendLocked commits the given events in ONE batch (all or none). Called
// with a.mu held; the journal actor serializes independently.
func (a *Authority) appendLocked(ctx context.Context, params ...contracts.EnvelopeParams) error {
	if a.j == nil {
		return fmt.Errorf("s7: no journal bound")
	}
	_, err := a.j.AppendBatch(ctx, params)
	return err
}

// Projection is the durable s7_operations fold (SyncProjection).
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "s7" }
func (Projection) Version() int { return 1 }

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS s7_operations (
			op TEXT PRIMARY KEY,
			target TEXT NOT NULL,
			state TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL,
			deadline_unix INTEGER NOT NULL DEFAULT 0,
			next_attempt_unix INTEGER NOT NULL DEFAULT 0,
			lease_nonce_hash TEXT NOT NULL DEFAULT '',
			lease_expires_unix INTEGER NOT NULL DEFAULT 0,
			lease_started INTEGER NOT NULL DEFAULT 0,
			last_code TEXT NOT NULL DEFAULT '',
			policy_json TEXT NOT NULL,
			begun INTEGER NOT NULL
		);`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	_, err := db.Exec(`DROP TABLE IF EXISTS s7_operations`)
	return err
}

func (Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	raw := ev.Envelope.Payload
	switch ev.Envelope.EventType {
	case EvOperationBegun:
		var p begunPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		pol, err := unmarshalPolicy(p.Policy)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO s7_operations(op, target, state, max_attempts, deadline_unix, policy_json, begun)
			VALUES(?,?,?,?,?,?,?)`, p.Op, p.Target, contracts.AttemptPlanned.String(), pol.MaxAttempts, p.Deadline, string(p.Policy), int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("s7: begun insert: %w", err)
		}
	case EvAttemptAuthorized:
		var p authorizedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return exec1(tx, `UPDATE s7_operations SET state=?, lease_nonce_hash=?, lease_expires_unix=?, lease_started=0
			WHERE op=? AND state IN (?,?)`, contracts.AttemptAuthorized.String(), p.NonceHash, p.ExpiresAt, p.Op,
			contracts.AttemptPlanned.String(), contracts.AttemptFailedRetryable.String())
	case EvAttemptStarted:
		var p startedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return exec1(tx, `UPDATE s7_operations SET state=?, lease_started=1, attempts=attempts+1
			WHERE op=? AND state=?`, contracts.AttemptRunning.String(), p.Op, contracts.AttemptAuthorized.String())
	case EvLeaseRevoked:
		var p revokedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return exec1(tx, `UPDATE s7_operations SET state=?, lease_nonce_hash='', lease_expires_unix=0, lease_started=0
			WHERE op=? AND state=? AND lease_started=0`, contracts.AttemptPlanned.String(), p.Op, contracts.AttemptAuthorized.String())
	case EvAttemptReported:
		var p reportedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		var state string
		switch LandingKind(p.Landing) {
		case LandingSucceeded:
			state = contracts.AttemptSucceeded.String()
		case LandingRetry:
			state = contracts.AttemptFailedRetryable.String()
		case LandingTerminal:
			state = contracts.AttemptFailed.String()
		case LandingUnknown:
			state = contracts.AttemptUnknown.String()
		default:
			return fmt.Errorf("s7: reported with landing %d (fail closed)", p.Landing)
		}
		return exec1(tx, `UPDATE s7_operations SET state=?, next_attempt_unix=?, last_code=?, lease_nonce_hash='', lease_expires_unix=0, lease_started=0
			WHERE op=? AND state=?`, state, p.NextAt, p.Code, p.Op, contracts.AttemptRunning.String())
	case EvOperationTerminal:
		var p terminalPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// Idempotent with a preceding reported fold (same terminal state).
		if _, err := tx.Exec(`UPDATE s7_operations SET state=?, lease_nonce_hash='', lease_expires_unix=0 WHERE op=?`, p.State, p.Op); err != nil {
			return fmt.Errorf("s7: terminal: %w", err)
		}
	case EvOperationReconcile:
		var p reconcilePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return exec1(tx, `UPDATE s7_operations SET state=?, next_attempt_unix=? WHERE op=? AND state=?`, p.State, p.NextAt, p.Op, contracts.AttemptUnknown.String())
	}
	return nil
}

// exec1 runs one projection UPDATE that must touch EXACTLY one row: a fold
// that finds the operation in an unexpected state aborts the whole append
// (state and event commit together or not at all).
func exec1(tx *journal.ProjTx, query string, args ...any) error {
	res, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("s7 fold: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("s7 fold: operation not in the expected state for %q (fail closed)", strings.Fields(query)[1])
	}
	return nil
}

// rehydrate rebuilds every non-terminal durable operation from the
// projection. RUNNING (STARTED, no report) -> UNKNOWN durably; AUTHORIZED
// (lease, no STARTED) -> marked rehydrated (revoked on Next).
func (a *Authority) rehydrate(ctx context.Context) error {
	rows, err := a.j.QueryProjection(ctx, `SELECT op, target, state, attempts, deadline_unix, next_attempt_unix,
		lease_nonce_hash, lease_expires_unix, lease_started, last_code, policy_json, begun FROM s7_operations
		WHERE state IN (?,?,?,?,?)`, contracts.AttemptPlanned.String(), contracts.AttemptAuthorized.String(),
		contracts.AttemptRunning.String(), contracts.AttemptFailedRetryable.String(), contracts.AttemptUnknown.String())
	if err != nil {
		return fmt.Errorf("s7 rehydrate: %w", err)
	}
	defer rows.Close()
	type row struct {
		op, target, state, nonceHash, lastCode, policy string
		attempts, started                              int
		deadline, nextAt, leaseExp, begun              int64
	}
	var recs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.op, &r.target, &r.state, &r.attempts, &r.deadline, &r.nextAt, &r.nonceHash, &r.leaseExp, &r.started, &r.lastCode, &r.policy, &r.begun); err != nil {
			return fmt.Errorf("s7 rehydrate: %w", err)
		}
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range recs {
		pol, err := unmarshalPolicy([]byte(r.policy))
		if err != nil {
			return fmt.Errorf("s7 rehydrate %s: %w", r.op, err)
		}
		rec := &operation{target: contracts.TargetID(r.target), policy: pol, attempts: r.attempts, lastCode: r.lastCode}
		if r.deadline > 0 {
			rec.deadline = time.Unix(r.deadline, 0)
		}
		if r.nextAt > 0 {
			rec.nextAt = time.Unix(r.nextAt, 0)
		}
		op := contracts.OperationID(r.op)
		switch r.state {
		case contracts.AttemptPlanned.String():
			rec.state = contracts.AttemptPlanned
		case contracts.AttemptFailedRetryable.String():
			rec.state = contracts.AttemptFailedRetryable
		case contracts.AttemptUnknown.String():
			// Parked for reconciliation: rehydrated so Next keeps refusing it
			// and Reconcile (the only exit) can act after a restart.
			rec.state = contracts.AttemptUnknown
		case contracts.AttemptAuthorized.String():
			// Lease with no durable STARTED: the wire was never touched, but
			// the nonce is gone with the old process — revoke on Next.
			rec.state, rec.rehydrated, rec.nonceHash = contracts.AttemptAuthorized, true, r.nonceHash
			rec.expires = time.Unix(r.leaseExp, 0)
		case contracts.AttemptRunning.String():
			// Durable STARTED, no report: the wire MAY have been touched (E9)
			// -> UNKNOWN, durably, never re-granted.
			landing := Landing{Kind: LandingUnknown, Code: CodeCrashRecovered}
			if err := a.appendLocked(ctx, a.evReported(op, r.attempts, OutcomeUnknown, CodeCrashRecovered, landing), a.evTerminal(op, contracts.AttemptUnknown)); err != nil {
				return fmt.Errorf("s7 rehydrate %s: %w", r.op, err)
			}
			rec.state = contracts.AttemptUnknown
		default:
			continue
		}
		a.ops[op] = rec
	}
	return nil
}

// Reconcile exits UNKNOWN for a durable operation after the OWNER proved the
// remote state (E9: reconciliation, never a blind retry): ok=true -> SUCCEEDED,
// ok=false -> FAILED. The owner's companion commits in the same batch.
func (a *Authority) Reconcile(op contracts.OperationID, ok bool, build Builder) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, exists := a.ops[op]
	if !exists || rec.state != contracts.AttemptUnknown {
		return fmt.Errorf("s7 reconcile: operation %q is not UNKNOWN (fail closed)", op)
	}
	now := a.now()
	ev, landing := machine.EvAttemptReconciledOK, Landing{Kind: LandingSucceeded, Code: rec.lastCode}
	if !ok {
		// The remote effect is proven ABSENT: the SAME operation becomes
		// retryable within its existing budget (only Next may re-grant);
		// out of budget it is terminal.
		wait := a.backoffLocked(rec)
		if rec.attempts < rec.policy.MaxAttempts && (rec.deadline.IsZero() || now.Add(wait).Before(rec.deadline)) {
			ev, landing = machine.EvAttemptReconciledRetry, Landing{Kind: LandingRetry, NextAt: now.Add(wait), Code: rec.lastCode}
		} else {
			ev, landing = machine.EvAttemptReconciledFailed, Landing{Kind: LandingTerminal, Code: rec.lastCode}
		}
	}
	state, err := a.tbl.Step(rec.state, ev)
	if err != nil {
		return fmt.Errorf("s7 reconcile: %w", err)
	}
	if rec.policy.Durable {
		if build == nil {
			return ErrNoBuilder
		}
		c := build(landing)
		if c.Key != op || c.Params.EventType == "" {
			return fmt.Errorf("s7 reconcile: %w", ErrCompanion)
		}
		rp := reconcilePayload{Op: string(op), State: state.String()}
		if !landing.NextAt.IsZero() {
			rp.NextAt = landing.NextAt.Unix()
		}
		if err := a.appendLocked(context.Background(), a.params(EvOperationReconcile, rp), c.Params); err != nil {
			return fmt.Errorf("s7 reconcile: not durable (fail closed): %w", err)
		}
	}
	rec.state = state
	if landing.Kind == LandingRetry {
		rec.nextAt = landing.NextAt
	}
	return nil
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

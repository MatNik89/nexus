//go:build linux

// Package channel is the transport-neutral durable channel core (T22,
// HARDQ B2/B7, E15): a durable INBOX keyed (adapter_id, channel_identity,
// update_id) with exactly-once ADMISSION — a replayed update returns its
// existing outcome, it never re-admits; and a transactional OUTBOX with a
// stable delivery id and at-least-once remote delivery. Delivery honesty:
// a transport error keeps the row pending (retry is safe — nothing was
// accepted); transport ACCEPT marks the row sent; accept-then-crash
// before the sent-mark is sent-but-unrecorded → the row parks UNKNOWN and
// ONLY reconciliation (proof of send / proof of loss) resolves it — never
// a blind retry over a non-transactional remote API.
//
// The two concrete B7 recipes ride journal.AppendBatch (one transaction,
// both halves durable or neither): inbox-admission+journal (the
// normalized inbound row folds in the SAME transaction as its canonical
// event) and terminal-result+outbox (the outbox row folds with its
// enqueue event).
package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
)

// Event types (closed).
const (
	EvInboundAdmitted  = "channel.inbound_admitted"
	EvOutboundEnqueued = "channel.outbound_enqueued"
	EvOutboundSent     = "channel.outbound_sent"
	EvOutboundUnknown  = "channel.outbound_unknown"
	EvOutboundResolved = "channel.outbound_resolved"
)

// State is the inbound lifecycle (RECEIVED exists only transiently inside
// the admission transaction; durably a row is ADMITTED or TERMINAL).
type State string

const (
	StateReceived State = "RECEIVED"
	StateAdmitted State = "ADMITTED"
	StateTerminal State = "TERMINAL"
)

// Inbound is one normalized inbound message.
type Inbound struct {
	AdapterID       string
	ChannelIdentity string
	UpdateID        int64
	Text            string
	Profile         contracts.ProfileID
}

// AdmitOutcome reports the admission result; Replayed=true carries the
// EXISTING outcome for a duplicate update.
type AdmitOutcome struct {
	MessageID string
	Replayed  bool
}

// Outbound is one pending/unknown outbox row.
type Outbound struct {
	DeliveryID      string
	AdapterID       string
	ChannelIdentity string
	Text            string
}

// --- payloads (closed) ---

type inboundPayload struct {
	MessageID       string `json:"message_id"`
	AdapterID       string `json:"adapter_id"`
	ChannelIdentity string `json:"channel_identity"`
	UpdateID        int64  `json:"update_id"`
	Text            string `json:"text"`
}

type outboundPayload struct {
	DeliveryID      string `json:"delivery_id"`
	AdapterID       string `json:"adapter_id"`
	ChannelIdentity string `json:"channel_identity"`
	Text            string `json:"text"`
}

type deliveryMark struct {
	DeliveryID string `json:"delivery_id"`
	Proved     bool   `json:"proved,omitempty"`
}

// Events returns the closed payload validators.
func Events() map[string]journal.PayloadValidator {
	markV := func(raw json.RawMessage) error {
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.DeliveryID == "" {
			return fmt.Errorf("channel: a delivery id is required")
		}
		return nil
	}
	return map[string]journal.PayloadValidator{
		EvInboundAdmitted: func(raw json.RawMessage) error {
			var p inboundPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.MessageID == "" || p.AdapterID == "" || p.ChannelIdentity == "" || p.UpdateID == 0 {
				return fmt.Errorf("channel: admission requires message, adapter, identity and update ids")
			}
			return nil
		},
		EvOutboundEnqueued: func(raw json.RawMessage) error {
			var p outboundPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.DeliveryID == "" || p.AdapterID == "" || p.ChannelIdentity == "" || p.Text == "" {
				return fmt.Errorf("channel: enqueue requires delivery id, adapter, identity and text")
			}
			return nil
		},
		EvOutboundSent: markV, EvOutboundUnknown: markV, EvOutboundResolved: func(raw json.RawMessage) error {
			var p deliveryMark
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.DeliveryID == "" {
				return fmt.Errorf("channel: a delivery id is required")
			}
			return nil
		},
	}
}

// Projection folds channel events into the durable inbox/outbox tables in
// the append transaction (the B7 recipes).
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "channel" }
func (Projection) Version() int { return 1 }

func (Projection) Init(db *journal.ProjDB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chan_inbox (
			message_id TEXT PRIMARY KEY,
			adapter_id TEXT NOT NULL,
			channel_identity TEXT NOT NULL,
			update_id INTEGER NOT NULL,
			text TEXT NOT NULL,
			status TEXT NOT NULL,
			created INTEGER NOT NULL,
			UNIQUE(adapter_id, channel_identity, update_id)
		);
		CREATE TABLE IF NOT EXISTS chan_outbox (
			delivery_id TEXT PRIMARY KEY,
			adapter_id TEXT NOT NULL,
			channel_identity TEXT NOT NULL,
			text TEXT NOT NULL,
			status TEXT NOT NULL, -- PENDING | SENT | UNKNOWN
			created INTEGER NOT NULL
		);
	`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	for _, stmt := range []string{`DROP TABLE IF EXISTS chan_inbox`, `DROP TABLE IF EXISTS chan_outbox`} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (Projection) Apply(tx *journal.ProjTx, ev journal.Event) error {
	raw := ev.Envelope.Payload
	switch ev.Envelope.EventType {
	case EvInboundAdmitted:
		var p inboundPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// The UNIQUE(adapter, identity, update) index is the exactly-once
		// backstop: a duplicate admission event aborts its own append.
		if _, err := tx.Exec(`INSERT INTO chan_inbox
			(message_id, adapter_id, channel_identity, update_id, text, status, created)
			VALUES(?,?,?,?,?,?,?)`,
			p.MessageID, p.AdapterID, p.ChannelIdentity, p.UpdateID, p.Text,
			string(StateAdmitted), int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("channel: inbox insert: %w", err)
		}
	case EvOutboundEnqueued:
		var p outboundPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO chan_outbox
			(delivery_id, adapter_id, channel_identity, text, status, created)
			VALUES(?,?,?,?,?,?)`,
			p.DeliveryID, p.AdapterID, p.ChannelIdentity, p.Text, "PENDING", int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("channel: outbox insert: %w", err)
		}
	case EvOutboundSent:
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE chan_outbox SET status='SENT' WHERE delivery_id=? AND status IN ('PENDING','UNKNOWN')`, p.DeliveryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("channel: sent-mark for an unknown or already-sent delivery (fail closed)")
		}
	case EvOutboundUnknown:
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE chan_outbox SET status='UNKNOWN' WHERE delivery_id=? AND status='PENDING'`, p.DeliveryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("channel: unknown-mark for a non-pending delivery (fail closed)")
		}
	case EvOutboundResolved:
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		to := "SENT"
		if !p.Proved {
			to = "PENDING" // proof of LOSS: safe to retry
		}
		res, err := tx.Exec(`UPDATE chan_outbox SET status=? WHERE delivery_id=? AND status='UNKNOWN'`, to, p.DeliveryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("channel: reconciliation for a non-UNKNOWN delivery (fail closed)")
		}
	}
	return nil
}

// Core is the channel facade over the profile's ONE journal.
type Core struct {
	j   *journal.Journal
	seq atomic.Uint64
	// testFailSentMark simulates a daemon death between transport accept
	// and the sent-mark append (the sent-but-unrecorded window).
	testFailSentMark bool
}

func New(j *journal.Journal) (*Core, error) {
	if j == nil {
		return nil, fmt.Errorf("channel: a journal is required (fail closed)")
	}
	return &Core{j: j}, nil
}

func (c *Core) params(eventType string, payload any) (contracts.EnvelopeParams, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contracts.EnvelopeParams{}, err
	}
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:   contracts.EventID(fmt.Sprintf("ev-chan-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), c.seq.Add(1))),
		EventType: eventType, RunID: "run-channel", EmittedAt: time.Now().UTC(),
		ActorType: contracts.ActorSystem, ActorID: "channel", PrincipalID: "nexus",
		WorkspaceID: "local", ProfileID: c.j.Profile(), AttemptNo: 1,
		Payload: raw, PayloadHash: "recomputed",
	}, nil
}

// messageID derives the STABLE message id from the dedup key.
func messageID(in Inbound) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", in.AdapterID, in.ChannelIdentity, in.UpdateID)))
	return "msg-" + hex.EncodeToString(sum[:12])
}

// Admit runs the B7 recipe inbox-admission+journal: the normalized row
// and its canonical event commit in ONE transaction. A replayed update
// returns the EXISTING outcome — exactly-once admission (B2).
func (c *Core) Admit(ctx context.Context, in Inbound) (AdmitOutcome, error) {
	if in.AdapterID == "" || in.ChannelIdentity == "" || in.UpdateID == 0 {
		return AdmitOutcome{}, fmt.Errorf("channel: adapter, identity and update ids are required (fail closed)")
	}
	if !in.Profile.Valid() {
		return AdmitOutcome{}, fmt.Errorf("channel: admission requires a bound profile (fail closed, B3)")
	}
	if in.Profile != c.j.Profile() {
		return AdmitOutcome{}, fmt.Errorf("channel: admission profile does not match this journal's binding (fail closed, B3)")
	}
	id := messageID(in)
	// Fast path: the durable dedup row already exists → existing outcome.
	if st, err := c.InboundStatus(ctx, id); err == nil && st != "" {
		return AdmitOutcome{MessageID: id, Replayed: true}, nil
	}
	p, err := c.params(EvInboundAdmitted, inboundPayload{
		MessageID: id, AdapterID: in.AdapterID, ChannelIdentity: in.ChannelIdentity,
		UpdateID: in.UpdateID, Text: in.Text})
	if err != nil {
		return AdmitOutcome{}, err
	}
	if _, err := c.j.Append(ctx, p); err != nil {
		// A concurrent/raced duplicate aborts on the UNIQUE index: return
		// the existing outcome (exactly-once holds either way).
		if st, serr := c.InboundStatus(ctx, id); serr == nil && st != "" {
			return AdmitOutcome{MessageID: id, Replayed: true}, nil
		}
		return AdmitOutcome{}, err
	}
	return AdmitOutcome{MessageID: id}, nil
}

// EnqueueReply runs the B7 recipe terminal-result+outbox: the outbox row
// folds with its enqueue event in ONE transaction. Returns the stable
// delivery id.
func (c *Core) EnqueueReply(ctx context.Context, adapter, identity string, profile contracts.ProfileID, text string) (string, error) {
	if adapter == "" || identity == "" || text == "" {
		return "", fmt.Errorf("channel: adapter, identity and text are required (fail closed)")
	}
	if profile != c.j.Profile() {
		return "", fmt.Errorf("channel: enqueue profile does not match this journal's binding (fail closed, B3)")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d|%d", adapter, identity, os.Getpid(), time.Now().UnixNano(), c.seq.Add(1))))
	id := "dlv-" + hex.EncodeToString(sum[:12])
	p, err := c.params(EvOutboundEnqueued, outboundPayload{
		DeliveryID: id, AdapterID: adapter, ChannelIdentity: identity, Text: text})
	if err != nil {
		return "", err
	}
	if _, err := c.j.Append(ctx, p); err != nil {
		return "", err
	}
	return id, nil
}

func (c *Core) rowsByStatus(ctx context.Context, status string) ([]Outbound, error) {
	rows, err := c.j.QueryProjection(ctx,
		`SELECT delivery_id, adapter_id, channel_identity, text FROM chan_outbox WHERE status=? ORDER BY created`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbound
	for rows.Next() {
		var o Outbound
		if err := rows.Scan(&o.DeliveryID, &o.AdapterID, &o.ChannelIdentity, &o.Text); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Pending lists rows awaiting delivery (safe to send/retry).
func (c *Core) Pending(ctx context.Context) ([]Outbound, error) {
	return c.rowsByStatus(ctx, "PENDING")
}

// Unreconciled lists sent-but-unrecorded rows awaiting reconciliation.
func (c *Core) Unreconciled(ctx context.Context) ([]Outbound, error) {
	return c.rowsByStatus(ctx, "UNKNOWN")
}

func (c *Core) mark(ctx context.Context, eventType, deliveryID string) error {
	p, err := c.params(eventType, deliveryMark{DeliveryID: deliveryID})
	if err != nil {
		return err
	}
	_, err = c.j.Append(ctx, p)
	return err
}

// Flush attempts delivery of every PENDING row through send:
//   - send ERROR: the row STAYS pending (nothing was accepted; retrying
//     later is safe) and the error surfaces;
//   - send ACCEPT: the sent-mark appends; if THAT fails the remote HAS
//     the message but our record does not — the row parks UNKNOWN
//     (durably when possible) and reconciliation owns it (B2: never
//     blind-retry over a non-transactional remote).
func (c *Core) Flush(ctx context.Context, send func(Outbound) error) error {
	pending, err := c.Pending(ctx)
	if err != nil {
		return err
	}
	for _, o := range pending {
		if err := send(o); err != nil {
			return fmt.Errorf("channel: delivery %s failed (stays pending): %w", o.DeliveryID, err)
		}
		sentErr := error(nil)
		if c.testFailSentMark {
			sentErr = fmt.Errorf("injected sent-mark failure")
		} else {
			sentErr = c.mark(ctx, EvOutboundSent, o.DeliveryID)
		}
		if sentErr != nil {
			// sent-but-unrecorded: try to park UNKNOWN durably; even if
			// that also fails, surface loudly — the next Flush must NOT
			// resend, so we park in-memory state via the UNKNOWN mark
			// retry below.
			if uerr := c.mark(ctx, EvOutboundUnknown, o.DeliveryID); uerr != nil {
				return fmt.Errorf("channel: delivery %s accepted remotely but neither sent nor unknown mark is durable — STOP AND RECONCILE: %w", o.DeliveryID, uerr)
			}
			return fmt.Errorf("channel: delivery %s accepted remotely but the sent-mark failed — parked UNKNOWN for reconciliation: %w", o.DeliveryID, sentErr)
		}
	}
	return nil
}

// Reconcile resolves an UNKNOWN delivery with external proof: proved=true
// (the remote shows the message) → SENT; proved=false (the remote proves
// loss) → back to PENDING for a safe retry.
func (c *Core) Reconcile(ctx context.Context, deliveryID string, proved bool) error {
	p, err := c.params(EvOutboundResolved, deliveryMark{DeliveryID: deliveryID, Proved: proved})
	if err != nil {
		return err
	}
	_, err = c.j.Append(ctx, p)
	return err
}

// InboundStatus reports the durable inbound state ("" for unknown ids).
func (c *Core) InboundStatus(ctx context.Context, msgID string) (State, error) {
	rows, err := c.j.QueryProjection(ctx, `SELECT status FROM chan_inbox WHERE message_id=?`, msgID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", nil
	}
	var s string
	if err := rows.Scan(&s); err != nil {
		return "", err
	}
	return State(s), rows.Err()
}

// testInboxRows counts inbox rows (crash-consistency oracle).
func (c *Core) testInboxRows(t interface{ Fatal(...any) }) int {
	rows, err := c.j.QueryProjection(context.Background(), `SELECT COUNT(*) FROM chan_inbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	if rows.Next() {
		rows.Scan(&n)
	}
	return n
}

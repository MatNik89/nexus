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
	"errors"
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
	EvInboundTerminal  = "channel.inbound_terminal"
	EvOutboundEnqueued = "channel.outbound_enqueued"
	EvOutboundSent     = "channel.outbound_sent"
	EvOutboundUnknown  = "channel.outbound_unknown"
	EvOutboundResolved = "channel.outbound_resolved"
)

// ErrAmbiguousSend marks a transport outcome where the request may have
// been ACCEPTED remotely (the HTTP call was issued but its result is
// unknown) — the row parks UNKNOWN, never a blind retry (B2).
var ErrAmbiguousSend = errors.New("AMBIGUOUS_SEND")

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
	// Attempts counts prior IN-FLIGHT LEASES (folded from
	// channel.outbound_unknown): 0 means never wire-attempted — the
	// only state where a formatted body may be carried (tgout plan).
	Attempts int
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
	// Adapter+Identity (optional, together) BIND the reconciliation to
	// the delivery's FULL destination — a same-profile sibling chat or a
	// different adapter cannot re-pend another destination's delivery
	// (P0-prep r1 codex #2 + r2 codex #2). Source records WHO confirmed.
	Adapter  string `json:"adapter,omitempty"`
	Identity string `json:"identity,omitempty"`
	Source   string `json:"source,omitempty"`
}

type terminalPayload struct {
	MessageID string `json:"message_id"`
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
		EvInboundTerminal: func(raw json.RawMessage) error {
			var p terminalPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.MessageID == "" {
				return fmt.Errorf("channel: terminal requires a message id")
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
func (Projection) Version() int { return 2 } // v2: chan_outbox.attempts (tgout)

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
		-- attempts is FOLDED from the outbound_unknown event (tgout
		-- plan: the first-lease formatting signal derives from the
		-- canonical event stream, never a mutable side channel); the
		-- version bump rebuilds old databases by replay.
		CREATE TABLE IF NOT EXISTS chan_outbox (
			delivery_id TEXT PRIMARY KEY,
			adapter_id TEXT NOT NULL,
			channel_identity TEXT NOT NULL,
			text TEXT NOT NULL,
			status TEXT NOT NULL, -- PENDING | SENT | UNKNOWN
			created INTEGER NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0
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
	case EvInboundTerminal:
		var p terminalPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE chan_inbox SET status=? WHERE message_id=? AND status=?`,
			string(StateTerminal), p.MessageID, string(StateAdmitted))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("channel: terminal for an unknown or already-terminal message (fail closed)")
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
		res, err := tx.Exec(`UPDATE chan_outbox SET status='UNKNOWN', attempts=attempts+1 WHERE delivery_id=? AND status='PENDING'`, p.DeliveryID)
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
		q := `UPDATE chan_outbox SET status=? WHERE delivery_id=? AND status='UNKNOWN'`
		args := []interface{}{to, p.DeliveryID}
		if p.Identity != "" || p.Adapter != "" {
			// Atomic FULL-destination guard in the SAME statement: both
			// columns or none (a half guard is refused).
			if p.Identity == "" || p.Adapter == "" {
				return fmt.Errorf("channel: destination guard requires BOTH adapter and identity (fail closed)")
			}
			q += ` AND adapter_id=? AND channel_identity=?`
			args = append(args, p.Adapter, p.Identity)
		}
		res, err := tx.Exec(q, args...)
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
	// testPostCheckHook parks Admit between its dedup check and its
	// append (race-detector seam, Phase-5-r2 codex #9); nil in production.
	testPostCheckHook func()
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
	if c.testPostCheckHook != nil {
		// Test seam (Phase-5-r2 codex #9): parks racers HERE, between the
		// dedup check and the append, so the race detector deterministically
		// exercises the UNIQUE-index backstop. Never set in production.
		c.testPostCheckHook()
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
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d|%d", adapter, identity, os.Getpid(), time.Now().UnixNano(), c.seq.Add(1))))
	return c.EnqueueReplyID(ctx, "dlv-"+hex.EncodeToString(sum[:12]), adapter, identity, profile, text)
}

// EnqueueReplyID enqueues under a CALLER-STABLE delivery id (T27 codex
// #2: the reminder loop derives the id from the occurrence, so a crash
// between enqueue and any bookkeeping can never mint a SECOND row for
// the same notification). Re-enqueueing an existing id is a no-op that
// returns the id — idempotent by construction.
func (c *Core) EnqueueReplyID(ctx context.Context, id, adapter, identity string, profile contracts.ProfileID, text string) (string, error) {
	if id == "" || adapter == "" || identity == "" || text == "" {
		return "", fmt.Errorf("channel: id, adapter, identity and text are required (fail closed)")
	}
	if profile != c.j.Profile() {
		return "", fmt.Errorf("channel: enqueue profile does not match this journal's binding (fail closed, B3)")
	}
	if st, err := c.DeliveryStatus(ctx, id); err == nil && st != "" {
		return id, nil // already durably enqueued (idempotent replay)
	}
	p, err := c.params(EvOutboundEnqueued, outboundPayload{
		DeliveryID: id, AdapterID: adapter, ChannelIdentity: identity, Text: text})
	if err != nil {
		return "", err
	}
	if _, err := c.j.Append(ctx, p); err != nil {
		// A raced duplicate aborts on the primary key: idempotent.
		if st, serr := c.DeliveryStatus(ctx, id); serr == nil && st != "" {
			return id, nil
		}
		return "", err
	}
	return id, nil
}

// DeliveryStatus reports the durable outbox state of one delivery id
// ("" = unknown id). SENT is the at-least-once PROOF the wire accepted
// it — the only state a delivery receipt may be minted from.
func (c *Core) DeliveryStatus(ctx context.Context, id string) (string, error) {
	rows, err := c.j.QueryProjection(ctx,
		`SELECT status FROM chan_outbox WHERE delivery_id=?`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	var st string
	if err := rows.Scan(&st); err != nil {
		return "", err
	}
	return st, nil
}

func (c *Core) rowsByStatus(ctx context.Context, status string) ([]Outbound, error) {
	rows, err := c.j.QueryProjection(ctx,
		`SELECT delivery_id, adapter_id, channel_identity, text, attempts FROM chan_outbox WHERE status=? ORDER BY created`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbound
	for rows.Next() {
		var o Outbound
		if err := rows.Scan(&o.DeliveryID, &o.AdapterID, &o.ChannelIdentity, &o.Text, &o.Attempts); err != nil {
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

// Flush delivers every PENDING row with UNKNOWN-FIRST honesty (Phase-5
// codex #3/#4): the row is durably parked UNKNOWN BEFORE the wire is
// touched, so a crash mid-send, an ambiguous transport result, or a
// failed sent-mark ALL leave the row in reconciliation — a blind resend
// is structurally impossible. Outcomes:
//   - DEFINITE pre-wire failure (send returns a non-ambiguous error):
//     the row resolves back to PENDING (nothing left the process; retry
//     is safe) and the error surfaces;
//   - accept: the SENT mark closes it; if THAT mark fails the row simply
//     stays UNKNOWN (already durable) and the error surfaces;
//   - ErrAmbiguousSend (the wire was touched, result unknown): the row
//     stays UNKNOWN for reconciliation.
func (c *Core) Flush(ctx context.Context, send func(Outbound) error) error {
	pending, err := c.Pending(ctx)
	if err != nil {
		return err
	}
	// One poisoned head must not starve every later delivery (Phase-5-r2
	// codex #6): failures are collected and the loop CONTINUES; only a
	// journal-mark failure aborts (the durable substrate itself is broken).
	var failures []error
	for _, o := range pending {
		// Durable in-flight parking BEFORE the wire.
		if err := c.mark(ctx, EvOutboundUnknown, o.DeliveryID); err != nil {
			return fmt.Errorf("channel: delivery %s could not be parked in-flight — not sending: %w", o.DeliveryID, err)
		}
		sendErr := send(o)
		switch {
		case sendErr == nil:
			sentErr := error(nil)
			if c.testFailSentMark {
				sentErr = fmt.Errorf("injected sent-mark failure")
			} else {
				sentErr = c.markSentFromUnknown(ctx, o.DeliveryID)
			}
			if sentErr != nil {
				// Already durably UNKNOWN: reconciliation owns it.
				return fmt.Errorf("channel: delivery %s accepted but the sent-mark failed — stays UNKNOWN for reconciliation: %w", o.DeliveryID, sentErr)
			}
		case errors.Is(sendErr, ErrAmbiguousSend):
			failures = append(failures, fmt.Errorf("channel: delivery %s ambiguous — stays UNKNOWN for reconciliation: %w", o.DeliveryID, sendErr))
		default:
			// DEFINITE pre-wire failure: nothing left the process.
			if rerr := c.Reconcile(ctx, o.DeliveryID, false); rerr != nil {
				return fmt.Errorf("channel: delivery %s failed pre-wire and could not re-pend: %w", o.DeliveryID, errors.Join(sendErr, rerr))
			}
			failures = append(failures, fmt.Errorf("channel: delivery %s failed (re-pended): %w", o.DeliveryID, sendErr))
		}
	}
	return errors.Join(failures...)
}

// markSentFromUnknown closes an in-flight row as SENT.
func (c *Core) markSentFromUnknown(ctx context.Context, deliveryID string) error {
	return c.mark(ctx, EvOutboundSent, deliveryID)
}

// CompleteInbound is THE terminal-result+outbox recipe (B7): the inbound
// message's TERMINAL outcome and its reply's outbox row commit in ONE
// batch — no reply without its terminal, no terminal without its reply.
// A crash between handler completion and this call redelivers the update
// and the handler re-runs (at-least-once handling over exactly-once
// admission — B2).
func (c *Core) CompleteInbound(ctx context.Context, messageID, adapter, identity string, profile contracts.ProfileID, reply string) (string, error) {
	if messageID == "" || adapter == "" || identity == "" || reply == "" {
		return "", fmt.Errorf("channel: message id, adapter, identity and reply are required (fail closed)")
	}
	if profile != c.j.Profile() {
		return "", fmt.Errorf("channel: profile does not match this journal's binding (fail closed, B3)")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d|%d", adapter, identity, os.Getpid(), time.Now().UnixNano(), c.seq.Add(1))))
	deliveryID := "dlv-" + hex.EncodeToString(sum[:12])
	termP, err := c.params(EvInboundTerminal, terminalPayload{MessageID: messageID})
	if err != nil {
		return "", err
	}
	outP, err := c.params(EvOutboundEnqueued, outboundPayload{
		DeliveryID: deliveryID, AdapterID: adapter, ChannelIdentity: identity, Text: reply})
	if err != nil {
		return "", err
	}
	if _, err := c.j.AppendBatch(ctx, []contracts.EnvelopeParams{termP, outP}); err != nil {
		return "", err
	}
	return deliveryID, nil
}

// MarkInboundTerminal closes an inbound with NO reply (single event).
func (c *Core) MarkInboundTerminal(ctx context.Context, messageID string) error {
	p, err := c.params(EvInboundTerminal, terminalPayload{MessageID: messageID})
	if err != nil {
		return err
	}
	_, err = c.j.Append(ctx, p)
	return err
}

// Reconcile resolves an UNKNOWN delivery with external proof: proved=true
// (the remote shows the message) → SENT; proved=false (the remote proves
// loss) → back to PENDING for a safe retry.
func (c *Core) Reconcile(ctx context.Context, deliveryID string, proved bool) error {
	return c.ReconcileFor(ctx, deliveryID, proved, "", "", "")
}

// ReconcileFor reconciles WITH a destination binding: the transition
// commits only if the row's channel identity matches (empty identity =
// unbound internal call). The confirming source is persisted.
func (c *Core) ReconcileFor(ctx context.Context, deliveryID string, proved bool, adapter, identity, source string) error {
	p, err := c.params(EvOutboundResolved, deliveryMark{DeliveryID: deliveryID, Proved: proved,
		Adapter: adapter, Identity: identity, Source: source})
	if err != nil {
		return err
	}
	_, err = c.j.Append(ctx, p)
	return err
}

// UnreconciledFor lists UNKNOWN deliveries FOR ONE destination chat.
func (c *Core) UnreconciledFor(ctx context.Context, adapter, identity string) ([]Outbound, error) {
	rows, err := c.j.QueryProjection(ctx,
		`SELECT delivery_id, adapter_id, channel_identity, text, attempts FROM chan_outbox
		 WHERE status='UNKNOWN' AND adapter_id=? AND channel_identity=? ORDER BY created`, adapter, identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbound
	for rows.Next() {
		var o Outbound
		if err := rows.Scan(&o.DeliveryID, &o.AdapterID, &o.ChannelIdentity, &o.Text, &o.Attempts); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
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

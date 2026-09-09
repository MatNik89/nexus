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
	"strings"
	"sync/atomic"
	"time"

	"github.com/MatNik89/nexus/internal/channel/health"
	"github.com/MatNik89/nexus/internal/foundation/egress"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

// Event types (closed).
const (
	EvInboundAdmitted  = "channel.inbound_admitted"
	EvInboundTerminal  = "channel.inbound_terminal"
	EvOutboundEnqueued = "channel.outbound_enqueued"
	EvOutboundSent     = "channel.outbound_sent"
	EvOutboundUnknown  = "channel.outbound_unknown"
	EvOutboundResolved = "channel.outbound_resolved"
	// Slice B2 (full S7 delivery): an S7-decided re-pend (FAILED_RETRYABLE),
	// the terminal park (FAILED), and the durable record of a governed
	// control effect (setMyCommands registration).
	EvOutboundRepend = "channel.outbound_repend"
	EvOutboundFailed = "channel.outbound_failed"
	EvControlEffect  = "channel.control_effect"
)

// Failure is the adapter's TYPED classification of one physical call
// (Slice B2, one closed code vocabulary shared with s7): Ambiguous = the
// wire may have been touched (E9: UNKNOWN, never a blind retry);
// otherwise the failure is DEFINITE (nothing committed remotely) and
// Retryable is the adapter's PROPOSAL — S7's policy decides.
type Failure struct {
	Code      string
	Ambiguous bool
	Retryable bool
	Status    int // HTTP status when the remote answered (0 otherwise)
	Cause     error
}

// ClassifiedError carries the health CLASS the ORIGINATING owner assigned
// (Slice D): health only renders it — no string matching anywhere. An
// error without a class is treated as substrate (fatal, fail closed).
type ClassifiedError struct {
	Class health.Class
	Code  string
	Cause error
}

func (c *ClassifiedError) Error() string {
	if c.Cause != nil {
		return fmt.Sprintf("channel[%s/%s]: %v", c.Class, c.Code, c.Cause)
	}
	return fmt.Sprintf("channel[%s/%s]", c.Class, c.Code)
}

func (c *ClassifiedError) Unwrap() error { return c.Cause }

// ClassOf renders the class of an error tree: the first ClassifiedError
// found (errors.As walks Join trees); a substrate class anywhere wins;
// nil -> "" (healthy); an unclassified error -> substrate.
func ClassOf(err error) (health.Class, string) {
	if err == nil {
		return "", ""
	}
	var found *ClassifiedError
	if !errors.As(err, &found) {
		return health.ClassSubstrate, "unclassified"
	}
	// A joined error may carry several classes: substrate dominates.
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range j.Unwrap() {
			var c *ClassifiedError
			if errors.As(e, &c) && c.Class == health.ClassSubstrate {
				return c.Class, c.Code
			}
		}
	}
	return found.Class, found.Code
}

func substrate(code string, err error) error {
	return &ClassifiedError{Class: health.ClassSubstrate, Code: code, Cause: err}
}

func (f *Failure) Error() string {
	kind := "definite"
	if f.Ambiguous {
		kind = "ambiguous"
	}
	if f.Cause != nil {
		return fmt.Sprintf("channel: %s failure %s: %v", kind, f.Code, f.Cause)
	}
	return fmt.Sprintf("channel: %s failure %s", kind, f.Code)
}

func (f *Failure) Unwrap() error { return f.Cause }

// Is lets errors.Is(err, ErrAmbiguousSend) keep working for ambiguous
// typed failures.
func (f *Failure) Is(target error) bool { return f.Ambiguous && target == ErrAmbiguousSend }

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
	// Generation counts HUMAN redelivers (each begins a new S7 operation).
	Generation int
	// LastCode is the last recorded failure code (empty when none).
	LastCode string
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
	// OperationID binds an S7-paired mark to its delivery operation
	// (Slice B2): the validator and projection REJECT a mark whose operation
	// is not this delivery's (a companion for row B can never ride A's grant).
	OperationID string `json:"operation_id,omitempty"`
	Code        string `json:"code,omitempty"`
	NextAt      int64  `json:"next_attempt_unix,omitempty"`
}

// controlEffectPayload is the adapter's durable record of ONE governed
// control effect (setMyCommands registration): operation_id is bound to the
// payload hash, so a rehydrated record names exactly the menu it registered.
type controlEffectPayload struct {
	OperationID string `json:"operation_id"`
	Adapter     string `json:"adapter"`
	BotID       int64  `json:"bot_id"`
	Method      string `json:"method"`
	PayloadHash string `json:"payload_hash"`
	State       string `json:"state"`
}

// ControlOperation is the durable identity of ONE control effect: bound to
// the adapter, the REMOTE BOT (a replacement token is a different bot and a
// different operation — plan-review r10) and the hash of the exact canonical
// wire payload.
func ControlOperation(adapter string, botID int64, method, payloadHash string) contracts.OperationID {
	return contracts.OperationID(fmt.Sprintf("control:%s:%d:%s:%s", adapter, botID, method, payloadHash))
}

// ControlTarget binds the grant to the bot resource the effect mutates.
func ControlTarget(adapter string, botID int64, method string) contracts.TargetID {
	return contracts.TargetID(fmt.Sprintf("channel:%s:bot:%d:%s", adapter, botID, method))
}

// OperationFor is the stable S7 operation identity of ONE delivery
// generation: the first is `delivery:<id>`; a HUMAN redeliver starts a new
// generation `delivery:<id>:r<n>` (human authority begins a new operation,
// it never re-grants an exhausted one).
func OperationFor(o Outbound) contracts.OperationID {
	if o.Generation == 0 {
		return contracts.OperationID("delivery:" + o.DeliveryID)
	}
	return contracts.OperationID(fmt.Sprintf("delivery:%s:r%d", o.DeliveryID, o.Generation))
}

// TargetFor binds the grant to the immutable delivery resource, not the
// adapter (plan-review r2 codex #2).
func TargetFor(o Outbound) contracts.TargetID {
	return contracts.TargetID("channel:" + o.AdapterID + ":delivery:" + o.DeliveryID)
}

// operationMatchesDelivery: the S7 operation id belongs to THIS delivery
// (any generation).
func operationMatchesDelivery(op, deliveryID string) bool {
	base := "delivery:" + deliveryID
	return op == base || strings.HasPrefix(op, base+":r")
}

var controlMethods = map[string]bool{"setMyCommands": true}
var controlStates = map[string]bool{"PENDING": true, "RUNNING": true, "UNKNOWN": true, "SUCCEEDED": true, "FAILED_RETRYABLE": true, "FAILED": true}

type terminalPayload struct {
	MessageID string `json:"message_id"`
}

// Events returns the closed payload validators.
func Events() map[string]journal.PayloadValidator {
	// S7-paired marks (unknown/sent/repend/failed) REQUIRE the bound
	// operation id and its consistency with the delivery (Slice B2).
	markV := func(raw json.RawMessage) error {
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.DeliveryID == "" {
			return fmt.Errorf("channel: a delivery id is required")
		}
		if p.OperationID == "" {
			return fmt.Errorf("channel: an S7-paired mark requires its operation_id (fail closed)")
		}
		if !operationMatchesDelivery(p.OperationID, p.DeliveryID) {
			return fmt.Errorf("channel: mark operation %q does not belong to delivery %q (fail closed)", p.OperationID, p.DeliveryID)
		}
		return nil
	}
	controlV := func(raw json.RawMessage) error {
		var p controlEffectPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.OperationID == "" || p.Adapter == "" || p.BotID == 0 || p.Method == "" || p.PayloadHash == "" || p.State == "" {
			return fmt.Errorf("channel: control_effect requires operation_id, adapter, bot_id, method, payload_hash, state")
		}
		if !controlMethods[p.Method] || !controlStates[p.State] {
			return fmt.Errorf("channel: control_effect method/state outside the closed set (fail closed)")
		}
		if contracts.OperationID(p.OperationID) != ControlOperation(p.Adapter, p.BotID, p.Method, p.PayloadHash) {
			return fmt.Errorf("channel: control_effect operation_id is not bound to its bot + payload hash (fail closed)")
		}
		return nil
	}
	return map[string]journal.PayloadValidator{
		EvOutboundRepend: markV, EvOutboundFailed: markV, EvControlEffect: controlV,
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
		EvEgressAttempt: func(raw json.RawMessage) error {
			var p egressPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			if p.Host == "" {
				return fmt.Errorf("channel: an egress receipt requires a host")
			}
			if p.Component == "" {
				return fmt.Errorf("channel: an egress receipt requires the emitting component")
			}
			// Field invariants (codex F4): an allowed receipt names its
			// pinned IP; a refusal names its reason.
			if p.Allowed && p.Pinned == "" {
				return fmt.Errorf("channel: an allowed egress receipt requires a pinned address")
			}
			if !p.Allowed && p.Reason == "" {
				return fmt.Errorf("channel: a refused egress receipt requires a reason")
			}
			return nil
		},
	}
}

// EvEgressAttempt is the E11 egress receipt: an auditable record of EVERY dial
// decision of EVERY outbound component (provider, telegram — Slice A shared
// owner). DialContext fires per TCP connection and HTTP keep-alive already
// coalesces polling, so every decision is journaled (no adapter-side
// coalescing that would hide same-IP reconnects).
const EvEgressAttempt = "channel.egress_attempt"

type egressPayload struct {
	Component string   `json:"component"`
	Host      string   `json:"host"`
	Port      int      `json:"port,omitempty"`
	Resolved  []string `json:"resolved,omitempty"`
	Pinned    string   `json:"pinned,omitempty"`
	Allowed   bool     `json:"allowed"`
	Reason    string   `json:"reason,omitempty"`
}

var egressSeq atomic.Int64

// EgressSink returns the ONE journal-backed egress.ReceiptSink the composition
// root hands to every outbound component (built right after journal.Open,
// BEFORE the provider or any adapter exists). It appends through the profile's
// journal (the single canonical writer); no secret ever enters the payload.
// The append error is returned so the dialer FAILS THE PERMITTED DIAL CLOSED
// when the receipt cannot be made durable (E11 requires the receipt). A
// background ctx is used: the append is a local serialized write that must
// not be cancelled by a per-request deadline.
func EgressSink(j *journal.Journal) egress.ReceiptSink {
	return func(d egress.Decision) error {
		resolved := make([]string, 0, len(d.Resolved))
		for _, r := range d.Resolved {
			resolved = append(resolved, r.String())
		}
		pinned := ""
		if d.Pinned.IsValid() {
			pinned = d.Pinned.String()
		}
		raw, err := json.Marshal(egressPayload{
			Component: d.Component, Host: d.Host, Port: d.Port, Resolved: resolved,
			Pinned: pinned, Allowed: d.Allowed, Reason: d.Reason,
		})
		if err != nil {
			return err
		}
		_, err = j.Append(context.Background(), contracts.EnvelopeParams{
			SchemaID: "nexus.event", SchemaVersion: 1,
			EventID:   contracts.EventID(fmt.Sprintf("ev-egress-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), egressSeq.Add(1))),
			EventType: EvEgressAttempt, RunID: "run-channel", EmittedAt: time.Now().UTC(),
			ActorType: contracts.ActorSystem, ActorID: "egress", PrincipalID: "nexus",
			WorkspaceID: "local", ProfileID: j.Profile(), AttemptNo: 1,
			Payload: raw, PayloadHash: "recomputed",
		})
		return err
	}
}

// Projection folds channel events into the durable inbox/outbox tables in
// the append transaction (the B7 recipes).
type Projection struct{}

func NewProjection() *Projection { return &Projection{} }

func (Projection) Name() string { return "channel" }
func (Projection) Version() int { return 3 } // v3: FAILED status, generation, control effects (Slice B2)

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
			status TEXT NOT NULL, -- PENDING | SENT | UNKNOWN | FAILED
			created INTEGER NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			generation INTEGER NOT NULL DEFAULT 0,
			last_code TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS chan_control_effect (
			operation_id TEXT PRIMARY KEY,
			adapter_id TEXT NOT NULL,
			bot_id INTEGER NOT NULL,
			method TEXT NOT NULL,
			payload_hash TEXT NOT NULL,
			state TEXT NOT NULL,
			updated INTEGER NOT NULL
		);
	`)
	return err
}

func (Projection) Reset(db *journal.ProjDB) error {
	for _, stmt := range []string{`DROP TABLE IF EXISTS chan_inbox`, `DROP TABLE IF EXISTS chan_outbox`, `DROP TABLE IF EXISTS chan_control_effect`} {
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
	case EvOutboundRepend:
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// S7 decided FAILED_RETRYABLE: nothing left the process, the row
		// returns to PENDING for the S7-scheduled next attempt.
		res, err := tx.Exec(`UPDATE chan_outbox SET status='PENDING', last_code=? WHERE delivery_id=? AND status='UNKNOWN'`, p.Code, p.DeliveryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("channel: repend for a delivery that is not in flight (fail closed)")
		}
	case EvOutboundFailed:
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		// Terminal park: exhausted, terminal code, or a local refusal.
		res, err := tx.Exec(`UPDATE chan_outbox SET status='FAILED', last_code=? WHERE delivery_id=? AND status IN ('UNKNOWN','PENDING')`, p.Code, p.DeliveryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("channel: failed-mark for a delivery that is not in flight or pending (fail closed)")
		}
	case EvControlEffect:
		var p controlEffectPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO chan_control_effect(operation_id, adapter_id, bot_id, method, payload_hash, state, updated)
			VALUES(?,?,?,?,?,?,?) ON CONFLICT(operation_id) DO UPDATE SET state=excluded.state, updated=excluded.updated`,
			p.OperationID, p.Adapter, p.BotID, p.Method, p.PayloadHash, p.State, int64(ev.JournalOffset)); err != nil {
			return fmt.Errorf("channel: control_effect upsert: %w", err)
		}
	case EvOutboundResolved:
		var p deliveryMark
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		to := "SENT"
		gen := ""
		if !p.Proved {
			to = "PENDING" // proof of LOSS (human): a NEW S7 generation
			gen = ", generation=generation+1"
		}
		q := `UPDATE chan_outbox SET status=?` + gen + ` WHERE delivery_id=? AND status IN ('UNKNOWN','FAILED')`
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
		`SELECT delivery_id, adapter_id, channel_identity, text, attempts, generation, last_code FROM chan_outbox WHERE status=? ORDER BY created`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbound
	for rows.Next() {
		var o Outbound
		if err := rows.Scan(&o.DeliveryID, &o.AdapterID, &o.ChannelIdentity, &o.Text, &o.Attempts, &o.Generation, &o.LastCode); err != nil {
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

// markParams builds one S7-paired outbox mark bound to its operation.
func (c *Core) markParams(eventType, deliveryID string, op contracts.OperationID, code string, nextAt time.Time) (contracts.EnvelopeParams, error) {
	m := deliveryMark{DeliveryID: deliveryID, OperationID: string(op), Code: code}
	if !nextAt.IsZero() {
		m.NextAt = nextAt.Unix()
	}
	return c.params(eventType, m)
}

// companionBuilder is the channel's ONE owner-side mapping from an S7
// Landing to the outbox companion committed in the SAME batch as the S7
// transition (Slice B2): Succeeded -> SENT; Retry -> re-pend (PENDING);
// Terminal / Cancelled -> FAILED (the code names why); Unknown -> no
// companion (the row is already durably UNKNOWN from the Consume park).
func (c *Core) companionBuilder(o Outbound, op contracts.OperationID) s7.Builder {
	return func(l s7.Landing) s7.Companion {
		var (
			p   contracts.EnvelopeParams
			err error
		)
		switch l.Kind {
		case s7.LandingSucceeded:
			p, err = c.markParams(EvOutboundSent, o.DeliveryID, op, "", time.Time{})
		case s7.LandingRetry:
			p, err = c.markParams(EvOutboundRepend, o.DeliveryID, op, l.Code, l.NextAt)
		case s7.LandingTerminal:
			p, err = c.markParams(EvOutboundFailed, o.DeliveryID, op, l.Code, time.Time{})
		case s7.LandingCancelled:
			code := l.Code
			if code == "" {
				code = s7.CodeLocalRefused
			}
			p, err = c.markParams(EvOutboundFailed, o.DeliveryID, op, code, time.Time{})
		default:
			return s7.Companion{}
		}
		if err != nil {
			return s7.Companion{}
		}
		return s7.Companion{Key: op, Params: p}
	}
}

// Send is the adapter's physical delivery of ONE row under ONE grant. The
// adapter recomputes the row's operation/target, compares them with the
// grant, and passes the park companion into s7.Consume IMMEDIATELY before
// the wire (STARTED + UNKNOWN park commit in one batch). It returns nil,
// a typed *Failure, or (legacy) an error wrapping ErrAmbiguousSend.
type Send func(o Outbound, g s7.Grant, park s7.Companion) error

// Flush drives every PENDING row through the S7 owner (Slice B2): for each
// row the operation is begun (idempotent), the next grant is asked for
// (S7 decides due-ness, cap and deadline; exhaustion lands FAILED together
// with the companion), the adapter sends under that grant, and the outcome
// is REPORTED to S7 which decides the landing — SENT / re-pend / FAILED /
// UNKNOWN — committing the S7 transition and the outbox companion in ONE
// batch. The channel never resends on its own: a definite failure only
// returns to PENDING because S7 said "retry is safe", and the next tick
// asks S7 again. UNKNOWN (the wire may have been touched) is never resent
// (E9); a human `redeliver` starts a new generation.
func (c *Core) Flush(ctx context.Context, auth *s7.Authority, send Send) error {
	if auth == nil || send == nil {
		return fmt.Errorf("channel: flush requires the S7 authority and a send (fail closed)")
	}
	pending, err := c.Pending(ctx)
	if err != nil {
		return substrate("pending_query", err)
	}
	// One poisoned head must not starve every later delivery (Phase-5-r2
	// codex #6): failures are collected and the loop CONTINUES; only a
	// journal/S7 landing failure aborts (the durable substrate is broken).
	var failures []error
	for _, o := range pending {
		op, target := OperationFor(o), TargetFor(o)
		if err := auth.Begin(op, target, s7.PolicyDelivery); err != nil {
			failures = append(failures, &ClassifiedError{Class: health.ClassTransport, Code: s7.CodeLocalRefused,
				Cause: fmt.Errorf("channel: delivery %s: %w", o.DeliveryID, err)})
			continue
		}
		build := c.companionBuilder(o, op)
		g, err := auth.Next(op, build)
		switch {
		case errors.Is(err, s7.ErrNotDue):
			continue // S7 scheduled a later attempt
		case errors.Is(err, s7.ErrExhausted):
			failures = append(failures, &ClassifiedError{Class: health.ClassTransport, Code: o.LastCode,
				Cause: fmt.Errorf("channel: delivery %s exhausted its S7 budget — parked FAILED: %w", o.DeliveryID, err)})
			continue
		case err != nil:
			if strings.Contains(err.Error(), "not durable") {
				return substrate("s7_authorize", fmt.Errorf("channel: delivery %s: S7 authorization could not be made durable — not sending: %w", o.DeliveryID, err))
			}
			failures = append(failures, &ClassifiedError{Class: health.ClassTransport, Code: s7.CodeLocalRefused,
				Cause: fmt.Errorf("channel: delivery %s: %w", o.DeliveryID, err)})
			continue
		}
		parkParams, perr := c.markParams(EvOutboundUnknown, o.DeliveryID, op, "", time.Time{})
		if perr != nil {
			return substrate("params", perr)
		}
		sendErr := send(o, g, s7.Companion{Key: op, Params: parkParams})
		st, _ := auth.State(op)
		if st == contracts.AttemptAuthorized {
			// Never consumed: a LOCAL refusal (identity mismatch, request
			// build) — nothing physical ran; cancel + FAILED in one batch.
			if cerr := auth.Cancel(op, build); cerr != nil {
				return substrate("s7_cancel", fmt.Errorf("channel: delivery %s: local refusal could not be landed: %w", o.DeliveryID, errors.Join(sendErr, cerr)))
			}
			failures = append(failures, &ClassifiedError{Class: health.ClassTransport, Code: s7.CodeLocalRefused,
				Cause: fmt.Errorf("channel: delivery %s refused locally (FAILED): %w", o.DeliveryID, sendErr)})
			continue
		}
		var outcome s7.Outcome
		code := ""
		var f *Failure
		switch {
		case sendErr == nil:
			outcome = s7.OutcomeSucceeded
		case errors.As(sendErr, &f) && !f.Ambiguous:
			code = f.Code
			outcome = s7.OutcomeFailedTerminal
			if f.Retryable {
				outcome = s7.OutcomeFailedRetryable
			}
		case errors.As(sendErr, &f):
			outcome, code = s7.OutcomeUnknown, f.Code
		default:
			// Untyped or ErrAmbiguousSend: the wire MAY have been touched.
			outcome, code = s7.OutcomeUnknown, s7.CodeTransportPostWrite
		}
		if c.testFailSentMark && outcome == s7.OutcomeSucceeded {
			// Simulated death between transport accept and the landing: the
			// row is already durably UNKNOWN (park) — reconciliation owns it.
			return substrate("sent_mark", fmt.Errorf("channel: delivery %s accepted but the sent-mark failed — stays UNKNOWN for reconciliation: injected sent-mark failure", o.DeliveryID))
		}
		if rerr := auth.Report(op, outcome, code, build); rerr != nil {
			// The S7 landing + companion did not commit: the row STAYS
			// UNKNOWN (never claims SENT); the substrate is broken.
			return substrate("landing", fmt.Errorf("channel: delivery %s: landing failed — stays UNKNOWN for reconciliation: %w", o.DeliveryID, errors.Join(sendErr, rerr)))
		}
		if sendErr != nil {
			if f != nil && f.Code == "receipt_not_durable" {
				return substrate("receipt", fmt.Errorf("channel: delivery %s: %w", o.DeliveryID, sendErr))
			}
			cls := health.ClassTransport
			if f != nil && (f.Status == 401 || f.Status == 403) {
				cls = health.ClassRemoteRejected
			}
			failures = append(failures, &ClassifiedError{Class: cls, Code: code, Cause: fmt.Errorf("channel: delivery %s: %w", o.DeliveryID, sendErr)})
		}
	}
	return errors.Join(failures...)
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

// FailedFor lists FAILED (terminally parked) deliveries FOR ONE destination
// chat, with the code that parked them.
func (c *Core) FailedFor(ctx context.Context, adapter, identity string) ([]Outbound, error) {
	rows, err := c.j.QueryProjection(ctx,
		`SELECT delivery_id, adapter_id, channel_identity, text, attempts, generation, last_code FROM chan_outbox
		 WHERE status='FAILED' AND adapter_id=? AND channel_identity=? ORDER BY created`, adapter, identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbound
	for rows.Next() {
		var o Outbound
		if err := rows.Scan(&o.DeliveryID, &o.AdapterID, &o.ChannelIdentity, &o.Text, &o.Attempts, &o.Generation, &o.LastCode); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ControlEffectParams builds the adapter's durable companion for ONE
// governed control effect; the operation id is bound to the payload hash.
func (c *Core) ControlEffectParams(adapter string, botID int64, method, payloadHash, state string) (contracts.OperationID, contracts.EnvelopeParams, error) {
	op := ControlOperation(adapter, botID, method, payloadHash)
	p, err := c.params(EvControlEffect, controlEffectPayload{OperationID: string(op), Adapter: adapter, BotID: botID, Method: method, PayloadHash: payloadHash, State: state})
	return op, p, err
}

// ControlProof is the OWNER's typed evidence about the remote state of one
// control effect (Slice B2, plan-review r11): the bot the proof was read
// from, the payload hash it was compared against, and the comparison. It
// is reduced to the boolean handed to s7.Reconcile ONLY when it is bound to
// the operation being reconciled — a proof read from bot B can never resolve
// bot A's UNKNOWN effect.
type ControlProof struct {
	BotID       int64
	PayloadHash string
	RemoteEqual bool
}

// VerifyControlProof binds a proof to the operation it claims to resolve.
func VerifyControlProof(op contracts.OperationID, adapter, method string, proof ControlProof) (bool, error) {
	if ControlOperation(adapter, proof.BotID, method, proof.PayloadHash) != op {
		return false, fmt.Errorf("channel: control proof (bot %d, hash %.12s) is not bound to operation %s (fail closed)", proof.BotID, proof.PayloadHash, op)
	}
	return proof.RemoteEqual, nil
}

// ControlEffectState reads the recorded state of one control effect ("" =
// never recorded).
func (c *Core) ControlEffectState(ctx context.Context, op contracts.OperationID) (string, error) {
	rows, err := c.j.QueryProjection(ctx, `SELECT state FROM chan_control_effect WHERE operation_id=?`, string(op))
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

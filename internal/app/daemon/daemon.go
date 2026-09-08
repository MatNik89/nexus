//go:build linux

// Package daemon is the 14.1-min server half: it OWNS the journal DB
// behind a Unix domain socket (HARDQ B7: one writer process, clients talk
// UDS — SQLITE_BUSY can never surface to a client), emits the C8 liveness
// heartbeat, and binds the per-SESSION PolicyMode at hello time (HARDQ
// F2: --yolo is a local-CLI session construct; a chat payload is just
// text and can never flip it). Connections are accepted only from the
// same UID (SO_PEERCRED).
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/MatNik89/nexus/internal/conv"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/loop"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/security/redact"
)

// Deps are the daemon's collaborators — the composition root (cmd/nexus)
// wires real ones; tests wire deterministic fakes. PlannerFactory builds
// the session planner around a DELTA SINK (streaming print, T17): a
// planner that streams calls deliver per delta; deliver errors mean the
// client is gone. Authority is the ONE process-wide S7 owner.
type Deps struct {
	Journal        *journal.Journal
	PlannerFactory func(deliver func(delta string) error) (loop.Planner, error)
	Authority      *s7min.Authority
	Profile        contracts.ProfileID
	Rules          map[contracts.ToolID]effectpath.Decision
	Tools          map[contracts.ToolID]effectpath.InProcFunc
	Audit          effectpath.AuditSink
	// Redactor scrubs known secret references from every outbound error
	// surface (observations, UDS error frames) — same instance the
	// journal uses.
	Redactor redact.Redactor
	// SuspenderFor + DurableApprovals wire the T24 HITL owner into
	// CHANNEL turns (remote HITL): the factory binds each challenge to
	// its originating channel identity as the ONLY legal decision source;
	// interactive sessions keep surfacing NEEDS_APPROVAL directly.
	SuspenderFor     func(channelIdentity string) loop.Suspender
	DurableApprovals effectpath.DurableApprovals
	// Sandbox is the REAL S6.2 process door (T26). Nil = no passing
	// sandbox probe: every ExecProcess call refuses fail-closed.
	Sandbox effectpath.SandboxBackend
}

// Daemon serves chat sessions over a UDS.
type Daemon struct {
	deps    Deps
	session atomic.Uint64
	// nonce distinguishes daemon INCARNATIONS: turn/run/event ids must
	// stay unique across restarts over the same journal.
	nonce int64
}

func New(d Deps) (*Daemon, error) {
	if d.Journal == nil || d.PlannerFactory == nil || d.Audit == nil || d.Authority == nil || d.Redactor == nil {
		return nil, fmt.Errorf("daemon: journal, planner factory, S7 authority, audit sink and redactor are required (fail closed)")
	}
	if !d.Profile.Valid() {
		return nil, fmt.Errorf("daemon: a profile is required (fail closed)")
	}
	return &Daemon{deps: d, nonce: time.Now().UnixNano()}, nil
}

// frame is the newline-delimited JSON wire unit, both directions.
type frame struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Yolo bool   `json:"yolo,omitempty"`
	// Code carries a typed error code STRUCTURALLY across the UDS
	// boundary (tgout plan: the repl edge maps codes without string
	// classification). Tool names the drift's precedence winner.
	Code string `json:"code,omitempty"`
	Tool string `json:"tool,omitempty"`
}

// Serve accepts sessions until ctx ends. The socket is created 0600 in a
// directory the caller controls.
func (d *Daemon) Serve(ctx context.Context, sock string) error {
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("daemon: listen: %w", err)
	}
	defer ln.Close()
	if err := os.Chmod(sock, 0o600); err != nil {
		return fmt.Errorf("daemon: socket perms: %w", err)
	}
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("daemon: accept: %w", err)
		}
		go d.handle(ctx, conn)
	}
}

// sameUID verifies the peer over SO_PEERCRED — local-CLI entry only.
func sameUID(conn net.Conn) bool {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false
	}
	same := false
	raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		same = err == nil && int(cred.Uid) == os.Geteuid()
	})
	return same
}

func writeFrame(conn net.Conn, f frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(b, '\n'))
	return err
}

// handle runs ONE session: hello binds the PolicyMode, then chat turns.
func (d *Daemon) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	if !sameUID(conn) {
		return // silent close: not our user
	}
	r := bufio.NewReader(conn)
	var hello frame
	line, err := r.ReadString('\n')
	if err != nil || json.Unmarshal([]byte(line), &hello) != nil || hello.Type != "hello" {
		writeFrame(conn, frame{Type: "error", Text: "expected hello"})
		return
	}
	mode := effectpath.ModeDefault
	if hello.Yolo {
		mode = effectpath.ModeYolo
	}
	// Per-session effect path: the mode lives HERE and nowhere reachable
	// from message payloads.
	pep, err := effectpath.NewPEP(d.deps.Rules, effectpath.NewApprovals(nil, 5*time.Minute), d.deps.Audit, mode)
	if err != nil {
		writeFrame(conn, frame{Type: "error", Text: "session setup failed"})
		return
	}
	grants := d.deps.Authority
	path, err := effectpath.NewEffectPath(pep, orderOnlyMW{},
		effectpath.NewInProcessExecutor(d.deps.Tools),
		effectpath.NewSandboxedProcessExecutor(d.sandbox()), grants)
	if err != nil {
		writeFrame(conn, frame{Type: "error", Text: "session setup failed"})
		return
	}
	session := d.session.Add(1)
	// The session planner streams deltas straight onto this connection.
	planner, err := d.deps.PlannerFactory(func(delta string) error {
		return writeFrame(conn, frame{Type: "delta", Text: delta})
	})
	if err != nil {
		writeFrame(conn, frame{Type: "error", Text: "session setup failed"})
		return
	}
	l, err := loop.New(planner, path, grants, d.deps.Journal, d.deps.Redactor, loop.PolicyInteractive, 16, 3)
	if err != nil {
		writeFrame(conn, frame{Type: "error", Text: "session setup failed"})
		return
	}
	msgN := 0
	// Session-scoped conversation history (dogfood 2026-09-07: the chat
	// session had the same no-prior-context hole as the channel path).
	// In-memory is honest here: the history lives exactly as long as the
	// interactive connection.
	var hist []histPair
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return // client gone
		}
		var f frame
		if json.Unmarshal([]byte(line), &f) != nil || f.Type != "chat" {
			writeFrame(conn, frame{Type: "error", Text: "expected chat frame"})
			continue
		}
		msgN++
		block, berr := userBlock(fmt.Sprintf("user-%d-%d", session, msgN), f.Text)
		if berr != nil {
			writeFrame(conn, frame{Type: "error", Text: "invalid input"})
			continue
		}
		turn := contracts.TurnID(fmt.Sprintf("turn-%d-%d-%d", d.nonce, session, msgN))
		run := contracts.RunID(fmt.Sprintf("run-%d-%d-%d", d.nonce, session, msgN))
		hb, hbErr := historyBlocks(fmt.Sprintf("repl-%d", session), "nexus://repl/history", hist)
		if hbErr != nil {
			writeFrame(conn, frame{Type: "error", Text: "history assembly failed"})
			continue
		}
		blocks := append(hb, block)
		final, terr := l.RunTurn(ctx, turn, run, d.deps.Profile, blocks)
		if terr != nil {
			// Typed sentinels keep their names (NEEDS_APPROVAL etc.);
			// known secret references are scrubbed before the UI boundary.
			// A typed drift crosses the UDS boundary STRUCTURALLY
			// (tgout plan): code+tool fields, no string classification.
			var de loop.DriftError
			if errors.As(terr, &de) {
				writeFrame(conn, frame{Type: "error", Code: de.Typed.Code, Tool: string(de.Tool),
					Text: loop.RedactText(d.deps.Redactor, de.Typed.SafeMessage)})
				continue
			}
			writeFrame(conn, frame{Type: "error", Text: loop.RedactText(d.deps.Redactor, terr.Error())})
			continue
		}
		// The session history stores the REDACTED final — the same
		// bytes the journal keeps and the channel path re-feeds
		// (conv-hist kilo F3: the raw final could re-transmit a known
		// secret ref to the provider on the next turn).
		hist = append(hist, histPair{user: f.Text, final: loop.RedactText(d.deps.Redactor, final)})
		if len(hist) > historyMaxPairs {
			// Bounded in place (conv-hist r2 codex #4): a long-lived
			// session must not retain every pair forever.
			hist = append(hist[:0], hist[len(hist)-historyMaxPairs:]...)
		}
		writeFrame(conn, frame{Type: "final", Text: final})
	}
}

// RunChannelTurn executes ONE conversation turn for CHANNEL input —
// ALWAYS ModeDefault (a channel message can never enable yolo, HARDQ
// F2); an unapproved ASK suspends durably through the T24 owner and the
// challenge summary is the reply; the turn is journaled like any
// session turn. Provenance names the REAL source (Phase-5 codex #14).
func (d *Daemon) RunChannelTurn(ctx context.Context, identity string, updateID int64, text string) (string, error) {
	l, err := d.channelLoop(identity)
	if err != nil {
		return "", err
	}
	// DETERMINISTIC per-message ids (Phase-5-r2 codex #1): a redelivered
	// update re-enters the SAME turn — the journal's unique event ids then
	// refuse a second execution of an already-run turn instead of
	// duplicating its effects under fresh identities.
	turn := contracts.TurnID(fmt.Sprintf("turn-chan-%s-%d", identity, updateID))
	run := contracts.RunID(fmt.Sprintf("run-chan-%s-%d", identity, updateID))
	block, err := sourcedBlock(fmt.Sprintf("chan-%s-%d", identity, updateID), text,
		"nexus://telegram/"+identity, "telegram")
	if err != nil {
		return "", err
	}
	// CONVERSATION HISTORY (dogfood 2026-09-07): without it every turn
	// answered "no prior context". The recent history of THIS identity
	// rides in as chronological role blocks ahead of the current message.
	hist, herr := d.conversationHistory(identity, turn)
	if herr != nil {
		return "", fmt.Errorf("conversation history: %w", herr)
	}
	blocks := append(hist, block)
	final, err := l.RunTurn(ctx, turn, run, d.deps.Profile, blocks)
	if err != nil {
		// Crash between turn completion and channel delivery (Phase-5-r3
		// codex #3): the redelivered update re-enters the same turn and
		// collides on its event ids — recover the DURABLE final from the
		// journal instead of reporting a false failure.
		rec, ok, rerr := d.recoveredTurnOutcome(turn)
		if rerr != nil {
			// The DECISIVE failure is the broken canonical stream — never
			// mask it behind the duplicate-event symptom (Phase-5-r5
			// codex #1).
			return "", fmt.Errorf("daemon: completed-turn recovery refused: %w", rerr)
		}
		if ok {
			switch {
			case rec.Kind == "FAILED" && rec.Code == "TOOL_SCHEMA_DRIFT":
				// Reconstructed typed outcome (tgout: restart-stable
				// drift recovery) — the edge maps the code; constants
				// are reconstructed for this code by contract.
				return "", loop.DriftError{Typed: contracts.TypedError{
					Code: rec.Code, Category: contracts.ErrCatValidation,
					Retryability: contracts.RetryNever,
					SafeMessage:  "the model reply named tool " + string(rec.Tool) + " but broke the tool-call schema",
					Origin:       "planner",
				}, Tool: rec.Tool}
			case rec.Kind == "FAILED":
				// Ordinary terminal failure: exactly today's collision
				// error path — no invented user response (tgout v15).
				return "", err
			default:
				return rec.Final, nil
			}
		}
		return "", err
	}
	return final, nil
}

// conversationHistory folds this identity's COMPLETED past channel
// turns (admitted user text + succeeded final) into history_user /
// history_assistant blocks that the planner maps to real provider role
// messages — the stolen gateway pattern (see historyBlocks). Finals
// come from the journal, so they are the REDACTED delivered text.
// conversationHistory reads the conv projection (O(historyMaxPairs)),
// replacing the per-turn Replay(0) that caused the soak backlog
// explosion. Observationally identical to referenceConversationHistory
// (differential oracle). Emits history_user/history_assistant blocks.
func (d *Daemon) conversationHistory(identity string, current contracts.TurnID) ([]contracts.ContextBlock, error) {
	pairs, err := conv.Projection{}.History(context.Background(), d.deps.Journal, identity, string(current), historyMaxPairs)
	if err != nil {
		return nil, err
	}
	entries := make([]histPair, len(pairs))
	for i, p := range pairs {
		entries[i] = histPair{user: p.User, final: p.Final}
	}
	return historyBlocks(identity, "nexus://telegram/"+identity+"/history", entries)
}

func (d *Daemon) referenceConversationHistory(identity string, current contracts.TurnID) ([]contracts.ContextBlock, error) {
	type pair struct {
		user, final string
		completed   bool // owned by the turn.succeeded EVENT, not final != ""
	}
	pairs := map[string]*pair{}
	var order []string
	err := d.deps.Journal.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case "channel.inbound_admitted":
			// The durable user text lives in the ADMISSION record (B2);
			// its (identity, update_id) is exactly the turn id scheme.
			var p struct {
				ChannelIdentity string `json:"channel_identity"`
				UpdateID        int64  `json:"update_id"`
				Text            string `json:"text"`
			}
			if json.Unmarshal(ev.Envelope.Payload, &p) != nil || p.ChannelIdentity != identity {
				return nil
			}
			tid := fmt.Sprintf("turn-chan-%s-%d", p.ChannelIdentity, p.UpdateID)
			if tid == string(current) {
				return nil
			}
			if _, ok := pairs[tid]; !ok {
				pairs[tid] = &pair{}
				order = append(order, tid)
			}
			if pairs[tid].user == "" {
				pairs[tid].user = p.Text
			}
		case "turn.succeeded":
			// EXACT membership in this identity's admitted set — the
			// same boundary rule as the admission filter (conv-hist
			// kilo F1: a prefix check over-matches identities that are
			// prefixes of one another).
			if ev.Envelope.TurnID == nil || *ev.Envelope.TurnID == current {
				return nil
			}
			var p struct {
				Final string `json:"final"`
			}
			if json.Unmarshal(ev.Envelope.Payload, &p) == nil {
				if pr, ok := pairs[string(*ev.Envelope.TurnID)]; ok {
					pr.final = p.Final
					pr.completed = true // even when the final is empty
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Only COMPLETED pairs enter history (conv-hist codex MED): an
	// admission without a turn.succeeded (suspended/failed turn) is not
	// conversation yet — and the cap counts completed pairs, applied
	// AFTER the filter. Completion is the EVENT, not a non-empty final
	// (conv-hist r2 codex #1: an empty final is still a completed turn).
	var entries []histPair
	for _, id := range order {
		if pairs[id].completed {
			entries = append(entries, histPair{user: pairs[id].user, final: pairs[id].final})
		}
	}
	if len(entries) > historyMaxPairs {
		entries = entries[len(entries)-historyMaxPairs:]
	}
	return historyBlocks(identity, "nexus://telegram/"+identity+"/history", entries)
}

// historyMaxPairs is the FIFO window every history producer shares.
const historyMaxPairs = 12

type histPair struct{ user, final string }

// historyBlocks emits history_user/history_assistant blocks with
// zero-padded BlockIDs so the planner reconstructs chronology. The
// stolen shape (karfly/chatgpt_telegram_bot and every mature Telegram
// AI gateway): history is alternating ROLE messages, FIFO-capped —
// past finals reach the model as the assistant role, so model output
// is never re-minted as user-trust text.
//
// topknot: full-journal replay per channel turn is O(events);
// acceptable at P0 volumes — trigger for a transcript projection:
// replay latency visibly lagging a chat turn.
func historyBlocks(scope, sourceURI string, entries []histPair) ([]contracts.ContextBlock, error) {
	const entryCap = 1500 // runes, not bytes — clipping never splits UTF-8
	clip := func(s string) string {
		r := []rune(s)
		if len(r) > entryCap {
			return string(r[:entryCap-1]) + "…"
		}
		return s
	}
	var out []contracts.ContextBlock
	for i, e := range entries {
		user, final := e.user, e.final
		if user != "" {
			b, err := sourcedBlock(fmt.Sprintf("hist-%s-%06d-a-user", scope, i), clip(user), sourceURI, "daemon")
			if err != nil {
				return nil, err
			}
			b.Kind = "history_user"
			out = append(out, b)
		}
		if final != "" {
			b, err := sourcedBlock(fmt.Sprintf("hist-%s-%06d-b-nexus", scope, i), clip(final), sourceURI, "daemon")
			if err != nil {
				return nil, err
			}
			b.Kind = "history_assistant"
			out = append(out, b)
		}
	}
	return out, nil
}

// completedTurnFinal recovers the redacted final of an already-succeeded
// turn from its journaled turn.succeeded payload.
// topknot ceiling: full-journal replay per recovery lookup — a turn-state
// projection is the upgrade when replay latency is measurable (P1).
// RecoveredOutcome is the closed sum a turn collision can recover
// (tgout plan v14): later success wins; a suspension candidate is
// SUPPRESSED by a later turn.resumed for the same turn; a terminal
// turn.failed after that supersedes — with code+tool when the failure
// was a typed drift, empty Code for ordinary failures ("terminal
// observed, no renderable payload").
type RecoveredOutcome struct {
	Kind  string // "SUCCEEDED" | "SUSPENDED" | "FAILED"
	Final string // succeeded final or suspension summary
	Code  string // turn.failed error_code ("" = ordinary failure)
	Tool  contracts.ToolID
}

// recoveredTurnOutcome reads the conv projection (O(1)) instead of
// Replay(0). Same RecoveredOutcome sum as the reference.
func (d *Daemon) recoveredTurnOutcome(turn contracts.TurnID) (RecoveredOutcome, bool, error) {
	// FAIL-CLOSED on a corrupt chain (parity with the reference replay,
	// conv-hist Phase-5-r4 codex #3): recovery runs ONLY on the cold
	// redelivery/collision path, so a full chain verification here is
	// affordable and preserves "a projected final from a journal that
	// fails integrity is not evidence". The hot history path stays O(12).
	if verr := d.deps.Journal.VerifyChain(); verr != nil {
		return RecoveredOutcome{}, false, verr
	}
	r, ok, err := conv.Projection{}.Recovery(context.Background(), d.deps.Journal, string(turn))
	if err != nil {
		return RecoveredOutcome{}, false, err
	}
	if !ok {
		return RecoveredOutcome{}, false, nil
	}
	out := RecoveredOutcome{Kind: r.State, Code: r.Code, Tool: contracts.ToolID(r.Tool)}
	switch r.State {
	case "SUCCEEDED":
		out.Final = r.Final
	case "SUSPENDED":
		out.Final = r.Summ
	}
	return out, true, nil
}

func (d *Daemon) referenceRecoveredTurnOutcome(turn contracts.TurnID) (RecoveredOutcome, bool, error) {
	out, found := RecoveredOutcome{}, false
	err := d.deps.Journal.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case "turn.succeeded":
			if ev.Envelope.TurnID != nil && *ev.Envelope.TurnID == turn {
				var p struct {
					Final string `json:"final"`
				}
				if json.Unmarshal(ev.Envelope.Payload, &p) == nil && p.Final != "" {
					out, found = RecoveredOutcome{Kind: "SUCCEEDED", Final: p.Final}, true
				}
			}
		case "approval.turn_suspended":
			// A crash between the suspension and the channel terminal
			// must replay the CHALLENGE SUMMARY (phase5-r4 kilo LOW). A
			// later turn.succeeded/turn.resumed/turn.failed overrides.
			var p struct {
				TurnID  string `json:"turn_id"`
				Summary string `json:"summary"`
			}
			if json.Unmarshal(ev.Envelope.Payload, &p) == nil && p.TurnID == string(turn) && p.Summary != "" {
				out, found = RecoveredOutcome{Kind: "SUSPENDED", Final: p.Summary}, true
			}
		case "turn.resumed":
			// The resumed lifecycle SUPPRESSES a stale suspension
			// candidate (tgout r12 codex MED#2): suspended -> resumed ->
			// failed must recover the failure, not the old challenge.
			if ev.Envelope.TurnID != nil && *ev.Envelope.TurnID == turn && out.Kind == "SUSPENDED" {
				out, found = RecoveredOutcome{}, false
			}
		case "turn.failed":
			if ev.Envelope.TurnID != nil && *ev.Envelope.TurnID == turn {
				var p struct {
					Code string `json:"error_code"`
					Tool string `json:"tool"`
				}
				_ = json.Unmarshal(ev.Envelope.Payload, &p)
				// A later terminal always supersedes a stale
				// suspension, payload or not (tgout v15).
				out, found = RecoveredOutcome{Kind: "FAILED", Code: p.Code,
					Tool: contracts.ToolID(p.Tool)}, true
			}
		}
		return nil
	})
	if err != nil {
		// FAIL CLOSED (Phase-5-r4 codex #3): a final observed during a
		// replay that later fails integrity verification is not evidence —
		// and the integrity error itself is the diagnosis, propagated,
		// never swallowed (Phase-5-r5 codex #1).
		return RecoveredOutcome{}, false, err
	}
	return out, found, nil
}

// ResumeChannelTurn rehydrates a SUSPENDED channel turn after its
// approval (B6, Phase-5-r2 codex #2): the loop re-enters the ORIGINAL
// turn with the approved tool's observation and continues to a real final.
func (d *Daemon) ResumeChannelTurn(ctx context.Context, identity string, turn contracts.TurnID,
	run contracts.RunID, tag string, blocks []contracts.ContextBlock) (string, error) {
	l, err := d.channelLoop(identity)
	if err != nil {
		return "", err
	}
	return l.ResumeTurn(ctx, turn, run, d.deps.Profile, tag, blocks)
}

// channelLoop builds the per-turn channel loop (ModeDefault ALWAYS — F2).
func (d *Daemon) channelLoop(identity string) (*loop.Loop, error) {
	pep, err := effectpath.NewPEP(d.deps.Rules, effectpath.NewApprovals(nil, 5*time.Minute), d.deps.Audit, effectpath.ModeDefault)
	if err != nil {
		return nil, err
	}
	if d.deps.DurableApprovals != nil {
		pep.SetDurableApprovals(d.deps.DurableApprovals)
	}
	path, err := effectpath.NewEffectPath(pep, orderOnlyMW{},
		effectpath.NewInProcessExecutor(d.deps.Tools),
		effectpath.NewSandboxedProcessExecutor(d.sandbox()), d.deps.Authority)
	if err != nil {
		return nil, err
	}
	planner, err := d.deps.PlannerFactory(func(string) error { return nil })
	if err != nil {
		return nil, err
	}
	l, err := loop.New(planner, path, d.deps.Authority, d.deps.Journal, d.deps.Redactor, loop.PolicyInteractive, 16, 3)
	if err != nil {
		return nil, err
	}
	if d.deps.SuspenderFor != nil {
		l.SetSuspender(d.deps.SuspenderFor(identity))
	}
	return l, nil
}

// userBlock wraps terminal input as a USER-trust context block.
func userBlock(id, text string) (contracts.ContextBlock, error) {
	return sourcedBlock(id, text, "nexus://repl", "repl")
}

// sourcedBlock carries HONEST provenance (Phase-5 codex #14: channel
// input must never masquerade as terminal input).
func sourcedBlock(id, text, sourceURI, producer string) (contracts.ContextBlock, error) {
	sum := sha256Hex(text)
	return contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID(id), Kind: "user_message", Content: &text,
		ContentHash: sum, SourceURI: sourceURI, Producer: producer,
		Trust: contracts.TrustUser, Sensitivity: contracts.Sensitivity(1),
		Lineage: []string{}, ObservedAt: time.Now().UTC(),
	})
}

// orderOnlyMW is the P0 S6.9 chain: order seam only — real middleware
// (budget, redaction taps) registers in later slices.
type orderOnlyMW struct{}

func (orderOnlyMW) BeforeTool(context.Context, contracts.ToolCall) error { return nil }
func (orderOnlyMW) AfterTool(context.Context, contracts.ToolCall, contracts.ToolResult) error {
	return nil
}
func (orderOnlyMW) OnError(ctx context.Context, e error) error { return e }

// sandbox returns the wired S6.2 backend or the fail-closed refuser.
func (d *Daemon) sandbox() effectpath.SandboxBackend {
	if d.deps.Sandbox != nil {
		return d.deps.Sandbox
	}
	return noSandbox{}
}

// noSandbox refuses every process execution until the REAL bwrap backend
// binds (T26) — fail closed, never a fake pass.
type noSandbox struct{}

func (noSandbox) Launch(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
	return contracts.ToolResult{}, fmt.Errorf("daemon: no sandbox backend bound yet (T26) — process execution refused (fail closed)")
}

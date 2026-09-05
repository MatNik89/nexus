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
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"

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
		final, terr := l.RunTurn(ctx, turn, run, d.deps.Profile, []contracts.ContextBlock{block})
		if terr != nil {
			// Typed sentinels keep their names (NEEDS_APPROVAL etc.);
			// known secret references are scrubbed before the UI boundary.
			writeFrame(conn, frame{Type: "error", Text: loop.RedactText(d.deps.Redactor, terr.Error())})
			continue
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
	final, err := l.RunTurn(ctx, turn, run, d.deps.Profile, []contracts.ContextBlock{block})
	if err != nil {
		// Crash between turn completion and channel delivery (Phase-5-r3
		// codex #3): the redelivered update re-enters the same turn and
		// collides on its event ids — recover the DURABLE final from the
		// journal instead of reporting a false failure.
		recovered, ok, rerr := d.completedTurnFinal(turn)
		if rerr != nil {
			// The DECISIVE failure is the broken canonical stream — never
			// mask it behind the duplicate-event symptom (Phase-5-r5
			// codex #1).
			return "", fmt.Errorf("daemon: completed-turn recovery refused: %w", rerr)
		}
		if ok {
			return recovered, nil
		}
		return "", err
	}
	return final, nil
}

// completedTurnFinal recovers the redacted final of an already-succeeded
// turn from its journaled turn.succeeded payload.
// topknot ceiling: full-journal replay per recovery lookup — a turn-state
// projection is the upgrade when replay latency is measurable (P1).
func (d *Daemon) completedTurnFinal(turn contracts.TurnID) (string, bool, error) {
	final, found := "", false
	err := d.deps.Journal.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case "turn.succeeded":
			if ev.Envelope.TurnID != nil && *ev.Envelope.TurnID == turn {
				var p struct {
					Final string `json:"final"`
				}
				if json.Unmarshal(ev.Envelope.Payload, &p) == nil && p.Final != "" {
					final, found = p.Final, true
				}
			}
		case "approval.turn_suspended":
			// SUSPENDED analog of the success recovery (phase5-r4 kilo
			// LOW): a crash between the suspension and the channel
			// terminal must replay the CHALLENGE SUMMARY, not a generic
			// failure. A later turn.succeeded (resume) overrides this.
			var p struct {
				TurnID  string `json:"turn_id"`
				Summary string `json:"summary"`
			}
			if json.Unmarshal(ev.Envelope.Payload, &p) == nil && p.TurnID == string(turn) && p.Summary != "" {
				final, found = p.Summary, true
			}
		}
		return nil
	})
	if err != nil {
		// FAIL CLOSED (Phase-5-r4 codex #3): a final observed during a
		// replay that later fails integrity verification is not evidence —
		// and the integrity error itself is the diagnosis, propagated,
		// never swallowed (Phase-5-r5 codex #1).
		return "", false, err
	}
	return final, found, nil
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

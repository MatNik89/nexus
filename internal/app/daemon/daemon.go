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
)

// Deps are the daemon's collaborators — the composition root (cmd/nexus)
// wires real ones; tests wire deterministic fakes.
type Deps struct {
	Journal *journal.Journal
	Planner loop.Planner
	Profile contracts.ProfileID
	Rules   map[contracts.ToolID]effectpath.Decision
	Tools   map[contracts.ToolID]effectpath.InProcFunc
	Audit   effectpath.AuditSink
}

// Daemon serves chat sessions over a UDS.
type Daemon struct {
	deps    Deps
	session atomic.Uint64
}

func New(d Deps) (*Daemon, error) {
	if d.Journal == nil || d.Planner == nil || d.Audit == nil {
		return nil, fmt.Errorf("daemon: journal, planner and audit sink are required (fail closed)")
	}
	if !d.Profile.Valid() {
		return nil, fmt.Errorf("daemon: a profile is required (fail closed)")
	}
	return &Daemon{deps: d}, nil
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
	grants := s7min.NewAuthority(nil, time.Minute)
	path, err := effectpath.NewEffectPath(pep, orderOnlyMW{},
		effectpath.NewInProcessExecutor(d.deps.Tools),
		effectpath.NewSandboxedProcessExecutor(noSandbox{}), grants)
	if err != nil {
		writeFrame(conn, frame{Type: "error", Text: "session setup failed"})
		return
	}
	session := d.session.Add(1)
	l, err := loop.New(d.deps.Planner, path, grants, d.deps.Journal, loop.PolicyInteractive, 16, 3)
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
		turn := contracts.TurnID(fmt.Sprintf("turn-%d-%d", session, msgN))
		run := contracts.RunID(fmt.Sprintf("run-%d-%d", session, msgN))
		final, terr := l.RunTurn(ctx, turn, run, d.deps.Profile, []contracts.ContextBlock{block})
		if terr != nil {
			// Message only — never internal state; typed sentinels keep
			// their names (NEEDS_APPROVAL etc.) for the client to render.
			writeFrame(conn, frame{Type: "error", Text: terr.Error()})
			continue
		}
		writeFrame(conn, frame{Type: "final", Text: final})
	}
}

// userBlock wraps terminal input as a USER-trust context block.
func userBlock(id, text string) (contracts.ContextBlock, error) {
	sum := sha256Hex(text)
	return contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID(id), Kind: "user_message", Content: &text,
		ContentHash: sum, SourceURI: "nexus://repl", Producer: "repl",
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

// noSandbox refuses every process execution until the REAL bwrap backend
// binds (T26) — fail closed, never a fake pass.
type noSandbox struct{}

func (noSandbox) Launch(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
	return contracts.ToolResult{}, fmt.Errorf("daemon: no sandbox backend bound yet (T26) — process execution refused (fail closed)")
}

//go:build linux

// Package procx owns process identity and lifecycle (S1.2-min): process
// groups (Setpgid), group termination TERM→grace→KILL→Wait, and PID +
// start-token identity (a PID alone is never proof — Annex P1.2 seed).
package procx

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Token identifies a process instance: PID plus kernel start time — PID
// reuse cannot forge it.
type Token struct {
	PID   int
	Start string
}

// StartTime reads the kernel starttime for pid (field 22 of /proc/<pid>/stat).
func StartTime(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	s := string(b)
	close := strings.LastIndex(s, ")")
	if close < 0 {
		return "", fmt.Errorf("procx: malformed stat for pid %d", pid)
	}
	fields := strings.Fields(s[close+1:])
	if len(fields) < 20 {
		return "", fmt.Errorf("procx: malformed stat for pid %d", pid)
	}
	return fields[19], nil
}

// Alive reports whether the EXACT process instance the token names still
// runs (same PID and same start time).
func (t Token) Alive() bool {
	st, err := StartTime(t.PID)
	return err == nil && st == t.Start
}

// Tracked is a started process owning its own process group. Lifecycle is
// sealed: Wait/Terminate serialize internally (Phase-1B codex #16 —
// concurrent Wait on a raw Cmd is unsafe) and the group is signalled only
// while the leader token still matches (PID/PGID-reuse defense).
//
// SCOPE (honest, codex #17): this primitive controls the PROCESS GROUP. A
// descendant that calls setsid/setpgid escapes it — TREE containment is the
// sandbox owner's job (T25: bwrap PID namespace + --die-with-parent), not
// this package's. Callers needing tree guarantees go through the sandbox.
type Tracked struct {
	cmd     *exec.Cmd
	Token   Token
	waitErr error
	waited  chan struct{}
	waitOne sync.Once
}

// Start launches cmd in its OWN process group and captures its identity
// token. The caller must eventually call Terminate or Wait.
func Start(cmd *exec.Cmd) (*Tracked, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("procx start: %w", err)
	}
	st, err := StartTime(cmd.Process.Pid)
	if err != nil {
		// The process may have exited instantly; reap and report.
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("procx start: identity: %w", err)
	}
	return &Tracked{cmd: cmd, Token: Token{PID: cmd.Process.Pid, Start: st}, waited: make(chan struct{})}, nil
}

// wait reaps the leader exactly once, from any number of callers.
func (t *Tracked) wait() error {
	t.waitOne.Do(func() {
		t.waitErr = t.cmd.Wait()
		close(t.waited)
	})
	<-t.waited
	return t.waitErr
}

// Wait blocks until the leader exits (serialized).
func (t *Tracked) Wait() error { return t.wait() }

// Terminate kills the process GROUP: SIGTERM → grace → SIGKILL → Wait.
// It validates the leader TOKEN immediately before each signal (a reused
// PID/PGID is never signalled), serializes reaping, surfaces signal
// errors, and verifies the group is actually gone before reporting success.
func (t *Tracked) Terminate(grace time.Duration) error {
	pgid := t.Token.PID // Setpgid: pgid == leader pid
	signalGroup := func(sig syscall.Signal) error {
		if !t.Token.Alive() {
			return nil // leader already gone: never signal a reused pgid
		}
		if err := syscall.Kill(-pgid, sig); err != nil && err != syscall.ESRCH {
			return fmt.Errorf("procx terminate: signal %v: %w", sig, err)
		}
		return nil
	}
	if err := signalGroup(syscall.SIGTERM); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- t.wait() }()
	select {
	case <-done:
	case <-time.After(grace):
		if err := signalGroup(syscall.SIGKILL); err != nil {
			return err
		}
		<-done
	}
	if err := signalGroup(syscall.SIGKILL); err != nil { // stragglers
		return err
	}
	// Prove the GROUP is gone (not just the leader): any surviving member
	// with a live start token is a failure, not a silent success.
	if survivors := groupSurvivors(pgid); len(survivors) > 0 {
		return fmt.Errorf("procx terminate: %d group member(s) survived (group scope; tree containment is the sandbox owner's)", len(survivors))
	}
	return nil
}

// groupSurvivors lists live pids still in pgid.
func groupSurvivors(pgid int) []int {
	var out []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		s := string(b)
		cl := strings.LastIndex(s, ")")
		if cl < 0 {
			continue
		}
		f := strings.Fields(s[cl+1:])
		if len(f) < 3 {
			continue
		}
		if g, _ := strconv.Atoi(f[2]); g == pgid {
			out = append(out, pid)
		}
	}
	return out
}

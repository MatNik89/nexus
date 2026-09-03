//go:build linux

// Package procx owns process identity and lifecycle (S1.2-min): process
// groups (Setpgid), group termination TERM→grace→KILL→Wait, and PID +
// start-token identity (a PID alone is never proof — Annex P1.2 seed).
package procx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// Tracked is a started process owning its own process group.
type Tracked struct {
	Cmd   *exec.Cmd
	Token Token
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
	return &Tracked{Cmd: cmd, Token: Token{PID: cmd.Process.Pid, Start: st}}, nil
}

// Terminate kills the WHOLE process group: SIGTERM → grace → SIGKILL →
// Wait. It returns once the leader is reaped.
func (t *Tracked) Terminate(grace time.Duration) error {
	pgid := t.Token.PID // Setpgid: pgid == leader pid
	syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- t.Cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(grace):
		syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
	}
	// Belt and braces: any group stragglers get the KILL too.
	syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}

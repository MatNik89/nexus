//go:build linux

// T10 RED table (tasks-P0): the spawned TREE fully terminates (an orphan
// grandchild dies with the group — the ledger literal); identity survives a
// PID-reuse check (a token names an instance, not a number).
package procx

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// descendants of pgid, as (pid,starttime) pairs — the probe suite's oracle.
func groupMembers(t *testing.T, pgid int) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, _ := os.ReadDir("/proc")
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
		close := strings.LastIndex(s, ")")
		if close < 0 {
			continue
		}
		f := strings.Fields(s[close+1:])
		if len(f) < 20 {
			continue
		}
		if g, _ := strconv.Atoi(f[2]); g == pgid {
			out[fmt.Sprintf("%d:%s", pid, f[19])] = true
		}
	}
	return out
}

func TestTerminateKillsWholeTree(t *testing.T) {
	// sh spawns a background grandchild; both live in the leader's group.
	tr, err := Start(exec.Command("/bin/sh", "-c", "sleep 300 & sleep 300"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	before := groupMembers(t, tr.Token.PID)
	if len(before) < 2 {
		t.Fatalf("oracle vacuous: want leader+grandchild in the group, got %d", len(before))
	}
	if err := tr.Terminate(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	for member := range before {
		parts := strings.SplitN(member, ":", 2)
		pid, _ := strconv.Atoi(parts[0])
		if st, err := StartTime(pid); err == nil && st == parts[1] {
			t.Fatalf("group member %s survived Terminate", member)
		}
	}
}

// SIGTERM-ignoring processes still die via the KILL escalation.
func TestTerminateEscalatesToKill(t *testing.T) {
	tr, err := Start(exec.Command("/bin/sh", "-c", "trap '' TERM; sleep 300"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	if err := tr.Terminate(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("escalation too slow: %v", elapsed)
	}
	if tr.Token.Alive() {
		t.Fatal("TERM-ignoring leader survived the KILL escalation")
	}
}

// A token names an INSTANCE: after death, Alive is false, and a token with
// a forged start time never matches (PID-reuse defense).
func TestTokenNamesInstanceNotPid(t *testing.T) {
	tr, err := Start(exec.Command("/bin/sleep", "300"))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Token.Alive() {
		t.Fatal("live process reported dead")
	}
	forged := Token{PID: tr.Token.PID, Start: tr.Token.Start + "9"}
	if forged.Alive() {
		t.Fatal("forged start-time token matched a live process")
	}
	tr.Terminate(100 * time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	if tr.Token.Alive() {
		t.Fatal("dead process reported alive")
	}
}

// Concurrent Wait+Terminate serialize safely (codex #16).
func TestConcurrentWaitAndTerminate(t *testing.T) {
	tr, err := Start(exec.Command("/bin/sleep", "300"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{}, 3)
	go func() { tr.Wait(); done <- struct{}{} }()
	go func() { tr.Wait(); done <- struct{}{} }()
	go func() { tr.Terminate(200 * time.Millisecond); done <- struct{}{} }()
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent lifecycle calls deadlocked")
		}
	}
}

// A stale token never signals: Terminate on an already-dead instance is a
// no-op success, not a shot at a reused pgid (codex #16).
func TestStaleTokenNeverSignals(t *testing.T) {
	tr, err := Start(exec.Command("/bin/sleep", "0.1"))
	if err != nil {
		t.Fatal(err)
	}
	tr.Wait()
	time.Sleep(100 * time.Millisecond)
	if err := tr.Terminate(100 * time.Millisecond); err != nil {
		t.Fatalf("terminate of a dead instance must succeed as a no-op: %v", err)
	}
}

// The codex r2 #12 literal: the leader exits PROMPTLY on TERM while a
// TERM-resistant grandchild stays in the group — the KILL phase must still
// reach the grandchild (gating group signals on the leader token leaked it).
func TestTerminateReapsStragglerAfterPromptLeaderExit(t *testing.T) {
	// Background grandchild ignores TERM; the leader exits immediately.
	tr, err := Start(exec.Command("/bin/sh", "-c", `trap '' TERM; sleep 300 & exit 0`))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // leader already exited; grandchild lives
	survivorsBefore := groupMembers(t, tr.Token.PID)
	if len(survivorsBefore) < 1 {
		t.Fatal("oracle vacuous: the TERM-resistant grandchild is not in the group")
	}
	if err := tr.Terminate(200 * time.Millisecond); err != nil {
		t.Fatalf("Terminate leaked a straggler: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	for member := range survivorsBefore {
		parts := strings.SplitN(member, ":", 2)
		pid, _ := strconv.Atoi(parts[0])
		if st, err := StartTime(pid); err == nil && st == parts[1] {
			t.Fatalf("TERM-resistant group member %s survived (leader-token gate leak)", member)
		}
	}
}

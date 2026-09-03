// T05 RED: test_atomic_write_no_partial — the process is SIGKILLed inside
// the crash window (tmp durable, rename not yet done); the target must hold
// the OLD content in full, never half of the new one. Anchored to Annex
// P0.11 ("old OR new, never half"), not to the implementation.
package atomicwrite

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReplacesContent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := Write(p, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "two" {
		t.Fatalf("want %q got %q (%v)", "two", b, err)
	}
}

// Child mode: performs the write but blocks inside the crash window.
func TestMain(m *testing.M) {
	if os.Getenv("NEXUS_AW_CHILD") == "1" {
		target := os.Getenv("NEXUS_AW_TARGET")
		ready := os.Getenv("NEXUS_AW_READY")
		pauseHook = func() {
			os.WriteFile(ready, []byte("in-window"), 0o600)
			time.Sleep(time.Hour) // parent SIGKILLs us here
		}
		Write(target, []byte("NEW-CONTENT-THAT-MUST-NOT-LAND"), 0o600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAtomicWriteNoPartialOnKill(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data.txt")
	ready := filepath.Join(dir, "ready")
	if err := os.WriteFile(target, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run", "TestMain")
	cmd.Env = append(os.Environ(),
		"NEXUS_AW_CHILD=1", "NEXUS_AW_TARGET="+target, "NEXUS_AW_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait until the child is INSIDE the crash window, then SIGKILL.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatal("child never reached the crash window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target unreadable after crash: %v", err)
	}
	if string(b) != "OLD" {
		t.Fatalf("crash mid-write corrupted the target: %q (must be the full OLD content)", b)
	}
}

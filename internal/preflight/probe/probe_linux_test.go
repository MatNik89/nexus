//go:build linux

// Hostile conformance suite v0 (tasks-P0 T02) against the REAL bwrap backend
// on the REAL host. Spec anchors: PRD §6 item 6 (cannot read /etc/shadow,
// cannot reach the network, proven by hostile test), HARDQ B4 (ELF-only
// launch, minimal closure), HARDQ D1 (bwrap = P0 ENFORCED backend), T02 RED
// (availability fail-closed + hazard-safe negative controls per boundary).
package probe

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperPath builds the static probehelper once per test run.
func helperPath(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "probehelper")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building probehelper: %v\n%s", err, out)
	}
	return bin
}

func mustDetect(t *testing.T) Availability {
	t.Helper()
	av, err := Detect()
	if err != nil {
		t.Skipf("bwrap unavailable on this host (fail-closed path exercised elsewhere): %v", err)
	}
	return av
}

func run(t *testing.T, av Availability, spec Spec) (string, error) {
	t.Helper()
	if spec.Timeout == 0 {
		spec.Timeout = 10 * time.Second
	}
	cmd, err := Command(av, spec)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// --- availability fail-closed (T02 RED a) ---

func TestDetectFailClosedWhenBwrapAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH dir — bwrap cannot be found
	if _, err := Detect(); err == nil {
		t.Fatal("Detect with no bwrap on PATH must fail closed (UNAVAILABLE), got nil error")
	} else if !strings.Contains(err.Error(), "SANDBOX_CAPABILITY_UNAVAILABLE") {
		t.Fatalf("want SANDBOX_CAPABILITY_UNAVAILABLE, got: %v", err)
	}
}

// --- FS_RO boundary: /etc/shadow must not be readable (PRD §6 item 6) ---

func TestShadowNotReadableInsideSandbox(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	spec := Spec{Target: hp, Args: []string{"readfile", "/etc/shadow"}, WorkDir: t.TempDir()}
	// probehelper must be visible inside: bind its dir as the workdir target.
	spec.WorkDir = filepath.Dir(hp)
	spec.Target = "/work/" + filepath.Base(hp)
	out, err := runBound(t, av, spec, hp)
	if err == nil {
		t.Fatalf("/etc/shadow was READABLE inside the sandbox — boundary broken:\n%s", out)
	}
}

// runBound runs the helper by binding its host dir at /work and invoking
// /work/probehelper, so the target path exists inside the mount namespace.
func runBound(t *testing.T, av Availability, spec Spec, hostHelper string) (string, error) {
	t.Helper()
	// ELF check must run against the HOST path (the /work path does not exist
	// in the runner's namespace), so validate first, then swap in the inside
	// path. Command() re-checks; give it the host path via a symlink dir.
	spec2 := spec
	spec2.Target = hostHelper
	cmd, err := Command(av, spec2)
	if err != nil {
		return "", err
	}
	// Replace the trailing host target with the inside path.
	argv := cmd.Args
	for i := len(argv) - 1; i >= 0; i-- {
		if argv[i] == hostHelper {
			argv[i] = "/work/" + filepath.Base(hostHelper)
			break
		}
	}
	c := exec.Command(argv[0], argv[1:]...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// --- FS_RW: the bound workdir IS writable ---

func TestWorkdirWritable(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	spec := Spec{WorkDir: filepath.Dir(hp), Args: []string{"writefile", "/work/out.txt", "hello"}}
	out, err := runBound(t, av, spec, hp)
	if err != nil {
		t.Fatalf("write inside bound workdir must succeed, got: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(hp), "out.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("host must see the sandbox write: %v %q", err, b)
	}
}

// --- NET_DENY: egress blocked, even to loopback outside the netns ---

func TestNetworkDeniedInsideSandbox(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0") // test-owned sink
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	spec := Spec{WorkDir: filepath.Dir(hp), Args: []string{"dial", ln.Addr().String()}}
	out, err := runBound(t, av, spec, hp)
	if err == nil {
		t.Fatalf("sandboxed dial to host sink SUCCEEDED — egress boundary broken:\n%s", out)
	}
}

// --- dynamic ELF closure: /bin/ls must run (HARDQ B4) ---

func TestDynamicELFRuns(t *testing.T) {
	av := mustDetect(t)
	out, err := run(t, av, Spec{Target: "/bin/ls", Args: []string{"/"}, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("dynamically linked /bin/ls must run under the B4 closure, got: %v\n%s", err, out)
	}
}

// --- shebang / non-ELF rejected by the launcher (B4 seed) ---

func TestShebangRejectedBeforeSandbox(t *testing.T) {
	av := mustDetect(t)
	script := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho pwned\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Command(av, Spec{Target: script}); err == nil {
		t.Fatal("shebang script accepted as launch target — must be rejected (ELF-only)")
	}
}

// --- undeclared child-exec: paths outside the closure are not visible ---

func TestUndeclaredChildExecFails(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	outside := filepath.Join(t.TempDir(), "outside-bin") // exists on host, NOT bound
	if err := copyFile(hp, outside); err != nil {
		t.Fatal(err)
	}
	spec := Spec{WorkDir: filepath.Dir(hp), Args: []string{"exec", outside, "sleep", "0"}}
	out, err := runBound(t, av, spec, hp)
	if err == nil {
		t.Fatalf("exec of an undeclared host path SUCCEEDED inside the sandbox:\n%s", out)
	}
}

// --- PROC_TREE: killing the sandbox kills everything inside ---

func TestKillReapsWholeTree(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	spec := Spec{WorkDir: filepath.Dir(hp), Args: []string{"spawn-sleep", "30"}}
	specHost := spec
	specHost.Target = hp
	cmd, err := Command(av, specHost)
	if err != nil {
		t.Fatal(err)
	}
	argv := cmd.Args
	argv[len(argv)-3] = "/work/" + filepath.Base(hp)
	c := exec.Command(argv[0], argv[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1 * time.Second) // let it spawn the inner child
	syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	c.Wait()
	time.Sleep(500 * time.Millisecond)
	// With --unshare-pid + --die-with-parent nothing survives; verify no
	// probehelper processes remain.
	out, _ := exec.Command("pgrep", "-f", filepath.Base(hp)).Output()
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Fatalf("sandbox descendants survived kill: pids %s", out)
	}
}

// --- hazard-safe NEGATIVE CONTROLS (T02 RED b): a loosened profile must be
// DETECTED by the same assertions the suite uses. Test-owned resources only:
// canary file, loopback sink, our own process tree — never real /etc, never
// uncontrolled egress. ---

func TestNegativeControlCanaryReadableWhenROLoosened(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	canaryDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(canaryDir, "canary.txt"), []byte("CANARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		WorkDir:      filepath.Dir(hp),
		Args:         []string{"readfile", filepath.Join(canaryDir, "canary.txt")},
		LoosenROBind: canaryDir,
	}
	out, err := runBound(t, av, spec, hp)
	if err != nil {
		t.Fatalf("negative control broken: loosened RO-bind did NOT expose the canary "+
			"(the FS assertion would pass vacuously): %v\n%s", err, out)
	}
	_ = out
}

func TestNegativeControlDialSucceedsWhenNetLoosened(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	spec := Spec{WorkDir: filepath.Dir(hp), Args: []string{"dial", ln.Addr().String()}, LoosenNet: true}
	out, err := runBound(t, av, spec, hp)
	if err != nil {
		t.Fatalf("negative control broken: loosened net did NOT allow the dial "+
			"(the NET assertion would pass vacuously): %v\n%s", err, out)
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

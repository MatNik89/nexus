//go:build linux

// T25 RED table (tasks-P0): the T02 hostile conformance suite RE-RUN
// through the REAL S6.2 SandboxBackend (Probe→Compile→Launch→Attest) —
// /etc/shadow-class reads EACCES (canary, hazard-safe), egress denied,
// /bin/ls (dynamic ELF) runs, shebang rejected before any sandbox,
// timeout leaves the tree dead, and an attestation mismatch marks the
// result untrusted. Fail-closed: no probe → no compile → no launch.
package sandbox

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func ctxT() context.Context { return context.Background() }

func helperPath(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "probehelper")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building probehelper: %v\n%s", err, out)
	}
	return bin
}

func wdir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "wd")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// backend probes or skips (the fail-closed no-bwrap path has its own test).
func backend(t *testing.T) (*Bwrap, ProbeReport) {
	t.Helper()
	b := NewBwrap()
	rep, err := b.Probe(ctxT())
	if err != nil {
		if os.Getenv("NEXUS_ACCEPT_REQUIRE_SANDBOX") != "" {
			t.Fatalf("bwrap REQUIRED for the graded acceptance run: %v", err)
		}
		t.Skipf("bwrap unavailable (fail-closed path covered separately): %v", err)
	}
	return b, rep
}

// runThrough executes the FULL backend protocol and returns output+waitErr
// after verifying the attestation — exactly how T26 will consume it.
func runThrough(t *testing.T, b *Bwrap, rep ProbeReport, spec Spec) (string, error) {
	t.Helper()
	if spec.Timeout == 0 {
		spec.Timeout = 15 * time.Second
	}
	pol, err := b.Compile(ctxT(), spec, rep)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	p, err := b.Launch(ctxT(), pol)
	if err != nil {
		return "", err
	}
	defer p.Close()
	werr := p.Wait()
	att, aerr := b.Attest(ctxT(), p, pol)
	if aerr != nil {
		t.Fatalf("attestation of a clean run failed: %v", aerr)
	}
	if att.PolicyHash != pol.PolicyHash() || att.ClosureDigest == "" {
		t.Fatalf("attestation does not bind policy+closure: %+v", att)
	}
	return p.Output(), werr
}

// --- fail-closed protocol shape ---

func TestCompileRequiresLiveProbe(t *testing.T) {
	b := NewBwrap()
	if _, err := b.Compile(ctxT(), Spec{Target: "/bin/ls"}, ProbeReport{}); err == nil {
		t.Fatal("compile accepted a dead probe report")
	}
}

func TestLaunchRequiresCompiledPolicy(t *testing.T) {
	b := NewBwrap()
	if _, err := b.Launch(ctxT(), CompiledPolicy{}); err == nil {
		t.Fatal("launch accepted a zero policy")
	}
}

func TestCompileRejectsRelativeTarget(t *testing.T) {
	b, rep := backend(t)
	if _, err := b.Compile(ctxT(), Spec{Target: "bin/ls"}, rep); err == nil {
		t.Fatal("relative target compiled")
	}
}

func TestStaleProbePolicyRefused(t *testing.T) {
	b, rep := backend(t)
	forged := rep
	forged.ProbeHash = "0000"
	pol, err := b.Compile(ctxT(), Spec{Target: "/bin/ls", WorkDir: wdir(t)}, forged)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := b.Launch(ctxT(), pol); err == nil {
		t.Fatal("policy compiled under a different probe launched")
	}
}

// --- the T02 hostile suite through the REAL backend ---

// /etc/shadow-class boundary, HAZARD-SAFE: a canary outside the B4
// closure is unreadable inside; the REAL secret file is never touched.
func TestBackendShadowClassDenied(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	canaryDir := t.TempDir()
	canary := filepath.Join(canaryDir, "canary-secret")
	if err := os.WriteFile(canary, []byte("CANARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runThrough(t, b, rep, Spec{Target: hp, Args: []string{"readfile", canary}, WorkDir: wdir(t)})
	if err == nil || strings.Contains(out, "CANARY") {
		t.Fatalf("closure-external file readable inside the sandbox (shadow-class breach):\n%s", out)
	}
}

func TestBackendEgressDenied(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	out, werr := runThrough(t, b, rep, Spec{Target: hp, Args: []string{"dial", ln.Addr().String()}, WorkDir: wdir(t)})
	if werr == nil {
		t.Fatalf("sandboxed dial SUCCEEDED through the backend — egress broken:\n%s", out)
	}
}

func TestBackendDynamicELFRuns(t *testing.T) {
	b, rep := backend(t)
	out, err := runThrough(t, b, rep, Spec{Target: "/bin/ls", Args: []string{"/"}, WorkDir: wdir(t)})
	if err != nil {
		t.Fatalf("/bin/ls did not run through the backend: %v\n%s", err, out)
	}
}

func TestBackendShebangRejected(t *testing.T) {
	b, rep := backend(t)
	script := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho pwned\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The closure pin resolves the target at COMPILE time — a
	// shebang/script is refused BEFORE any policy exists (Phase-6-r4
	// codex #1: accepting a launch-time refusal instead made this
	// detector false-green under a compile-guard ablation).
	_, err := b.Compile(ctxT(), Spec{Target: script, WorkDir: wdir(t)}, rep)
	if err == nil {
		t.Fatal("shebang script compiled into a policy (must be rejected at Compile)")
	}
	if !strings.Contains(err.Error(), "not a native ELF") && !strings.Contains(err.Error(), "closure") {
		t.Fatalf("refused for an unexpected reason: %v", err)
	}
}

func TestBackendWorkdirWritable(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	wd := wdir(t)
	out, err := runThrough(t, b, rep, Spec{Target: hp, Args: []string{"writefile", "/work/out.txt", "hello"}, WorkDir: wd})
	if err != nil {
		t.Fatalf("workdir write failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(wd, "out.txt")); err != nil {
		t.Fatalf("workdir write not visible on the host: %v", err)
	}
}

func TestBackendTimeoutKillsTree(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	pol, err := b.Compile(ctxT(), Spec{Target: hp, Args: []string{"hang"}, WorkDir: wdir(t),
		Timeout: 2 * time.Second}, rep)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	p, err := b.Launch(ctxT(), pol)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if werr := p.Wait(); werr == nil {
		t.Fatal("hanging target exited cleanly")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not bound the hang")
	}
}

// ATTESTATION MISMATCH (T25 RED): a process attested against a DIFFERENT
// policy is untrusted.
func TestAttestationMismatchUntrusted(t *testing.T) {
	b, rep := backend(t)
	polA, err := b.Compile(ctxT(), Spec{Target: "/bin/ls", Args: []string{"/"}, WorkDir: wdir(t)}, rep)
	if err != nil {
		t.Fatal(err)
	}
	polB, err := b.Compile(ctxT(), Spec{Target: "/bin/ls", Args: []string{"/etc"}, WorkDir: wdir(t)}, rep)
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.Launch(ctxT(), polA)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Wait()
	if _, err := b.Attest(ctxT(), p, polB); err == nil {
		t.Fatal("process launched under policy A attested against policy B")
	}
	if !strings.Contains(mustAttestErr(t, b, p, polB), "untrusted") {
		t.Fatal("mismatch error does not mark the result untrusted")
	}
	// The honest attestation still works.
	if _, err := b.Attest(ctxT(), p, polA); err != nil {
		t.Fatalf("honest attestation refused: %v", err)
	}
}

func mustAttestErr(t *testing.T, b *Bwrap, p *Process, pol CompiledPolicy) string {
	t.Helper()
	_, err := b.Attest(ctxT(), p, pol)
	if err == nil {
		return ""
	}
	return err.Error()
}

// Output stays bounded even for a spewing target.
func TestOutputBounded(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	out, _ := runThrough(t, b, rep, Spec{Target: hp, Args: []string{"spew", strconv.Itoa(4 << 20)}, WorkDir: wdir(t)})
	if len(out) > (1<<20)+1024 {
		t.Fatalf("output unbounded: %d bytes captured", len(out))
	}
}

// VERSION-ONLY fake backend rejected (Phase-6 codex #2): a PATH bwrap
// that answers --version but enforces nothing must never pass Probe —
// the enforcement canary catches it.
func TestFakeBwrapRejected(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "bwrap")
	script := "#!/bin/sh\ncase \"$1\" in --version) echo \"bubblewrap 0.11.0\";; esac\nexit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	b := NewBwrap()
	_, err := b.Probe(ctxT())
	if err == nil {
		t.Fatal("version-only fake bwrap passed the probe")
	}
	// The TRUST ROOT is what refuses it (Phase-6-r2 codex #1): a
	// user-writable fake never reaches the behavioral canary, so an
	// ADAPTIVE fake has nothing to spoof.
	if !strings.Contains(err.Error(), "root-owned") {
		t.Fatalf("fake rejected for the wrong reason (trust root not causal): %v", err)
	}
}

// TARGET SWAP between Compile and Launch refused (Phase-6 codex #3):
// the policy pins the compile-time bytes.
func TestLaunchRefusesSwappedTarget(t *testing.T) {
	b, rep := backend(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "prog")
	orig, err := os.ReadFile("/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	pol, err := b.Compile(ctxT(), Spec{Target: target, WorkDir: wdir(t)}, rep)
	if err != nil {
		t.Fatal(err)
	}
	repl, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, repl, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Launch(ctxT(), pol); err == nil {
		t.Fatal("swapped target bytes launched under the old policy")
	}
}

// S7 CANCELLATION reaches the sandbox (Phase-6 codex #4): an
// already-cancelled context never launches; a live cancel kills the tree.
func TestCancelledContextNeverLaunches(t *testing.T) {
	b, rep := backend(t)
	pol, err := b.Compile(ctxT(), Spec{Target: "/bin/ls", Args: []string{"/"}, WorkDir: wdir(t)}, rep)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Launch(cancelled, pol); err == nil {
		t.Fatal("already-cancelled context launched a process")
	}
	// Live cancel: a hanging target dies promptly on ctx cancel.
	hp := helperPath(t)
	pol2, err := b.Compile(ctxT(), Spec{Target: hp, Args: []string{"hang"},
		WorkDir: wdir(t), Timeout: 2 * time.Minute}, rep)
	if err != nil {
		t.Fatal(err)
	}
	liveCtx, liveCancel := context.WithCancel(context.Background())
	p, err := b.Launch(liveCtx, pol2)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	start := time.Now()
	go func() { time.Sleep(300 * time.Millisecond); liveCancel() }()
	if werr := p.Wait(); werr == nil {
		t.Fatal("cancelled hang exited cleanly")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("cancel did not kill the tree promptly")
	}
}

// FULL-CLOSURE pin verified at Launch (Phase-6-r2 codex #2): ANY closure
// member whose bytes differ from the compile-time pin is refused — not
// just the main target. White-box: the policy is mutated in-package to
// simulate a dependency swapped between Compile and Launch (the
// black-box $ORIGIN variant was proven by the external review probe).
func TestLaunchRefusesSwappedClosureMember(t *testing.T) {
	b, rep := backend(t)
	pol, err := b.Compile(ctxT(), Spec{Target: "/bin/ls", Args: []string{"/"}, WorkDir: wdir(t)}, rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(pol.closurePins) < 2 {
		t.Fatalf("dynamic /bin/ls should pin target+loader+libs, got %d members", len(pol.closurePins))
	}
	// Pick a NON-target member (loader or a library) and corrupt its pin
	// — equivalent to the on-disk bytes changing after Compile.
	for dest := range pol.closurePins {
		if dest != "/nexus-target" {
			pol.closurePins[dest] = "deadbeef"
			break
		}
	}
	if _, err := b.Launch(ctxT(), pol); err == nil {
		t.Fatal("closure member with non-compile-time bytes launched")
	}
}

//go:build linux

// T25 RED table (tasks-P0): the T02 hostile conformance suite RE-RUN
// through the REAL S6.2 SandboxBackend (Probe→Compile→Launch→Attest) —
// /etc/shadow-class reads EACCES (canary, hazard-safe), egress denied,
// /bin/ls (dynamic ELF) runs, shebang rejected before any sandbox,
// timeout leaves the tree dead, and an attestation mismatch marks the
// result untrusted. Fail-closed: no probe → no compile → no launch.
package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

// --- ExtraROBinds / ExtraEnv (PLAN-CODING-TRIO.md Slice 0) ---

func robindDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "robind")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

// Detector: ExtraROBinds/ExtraEnv propagate through the FULL
// Compile->Launch->Attest protocol, not just probe.Prepare directly.
func TestExtraROBindsAndEnvPropagateThroughFullProtocol(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	dir := robindDir(t)
	canary := filepath.Join(dir, "canary.txt")
	if err := os.WriteFile(canary, []byte("CANARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runThrough(t, b, rep, Spec{
		Target: hp, Args: []string{"readfile", canary}, WorkDir: wdir(t),
		ExtraROBinds: []string{dir}, ExtraEnv: map[string]string{"NEXUS_CODING_TEST": "v1"},
	})
	if err != nil {
		t.Fatalf("ExtraROBinds did not propagate through Compile/Launch: %v\n%s", err, out)
	}
	// Assert on the ExtraEnv value itself (code-review CODE1 codex finding
	// #6: an earlier version of this test set ExtraEnv but never actually
	// checked its value, so an ablation of ExtraEnv propagation would
	// still leave this test green).
	envOut, err := runThrough(t, b, rep, Spec{
		Target: hp, Args: []string{"printenv", "NEXUS_CODING_TEST"}, WorkDir: wdir(t),
		ExtraEnv: map[string]string{"NEXUS_CODING_TEST": "v1"},
	})
	if err != nil {
		t.Fatalf("ExtraEnv did not propagate through Compile/Launch: %v\n%s", err, envOut)
	}
	if !strings.Contains(envOut, "v1") {
		t.Fatalf("ExtraEnv value did not reach the child process, got %q", envOut)
	}
}

// Detector: PolicyHash changes when ExtraROBinds/ExtraEnv change — a
// caller cannot silently widen sandbox visibility under an
// already-approved policy hash (mirrors the existing target/args/workdir
// binding this policy hash already provides).
func TestPolicyHashChangesWithExtraROBindsAndEnv(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	base := Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wdir(t)}
	polBase, err := b.Compile(ctxT(), base, rep)
	if err != nil {
		t.Fatal(err)
	}
	withBind := base
	withBind.ExtraROBinds = []string{robindDir(t)}
	polBind, err := b.Compile(ctxT(), withBind, rep)
	if err != nil {
		t.Fatal(err)
	}
	if polBase.PolicyHash() == polBind.PolicyHash() {
		t.Fatal("PolicyHash unchanged after adding an ExtraROBinds entry")
	}
	withEnv := base
	withEnv.ExtraEnv = map[string]string{"K": "V"}
	polEnv, err := b.Compile(ctxT(), withEnv, rep)
	if err != nil {
		t.Fatal(err)
	}
	if polBase.PolicyHash() == polEnv.PolicyHash() {
		t.Fatal("PolicyHash unchanged after adding an ExtraEnv entry")
	}
}

// Detector (code-review CODE1 agy finding #3): two syntactically-different
// spellings of the SAME effective bind (a trailing slash, a symlink alias)
// must fold to the SAME PolicyHash — a bind's identity is its canonical
// resolved path, not the caller's literal string.
func TestPolicyHashCanonicalizesROBindSpelling(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	dir := robindDir(t)
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	wd := wdir(t) // ONE shared WorkDir — WorkDir is also folded into
	// policyHash, so a fresh wdir(t) per spec would (correctly) change
	// the hash for an unrelated reason and make this detector vacuous.
	specA := Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wd, ExtraROBinds: []string{dir}}
	specB := Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wd, ExtraROBinds: []string{dir + "/"}}
	specC := Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wd, ExtraROBinds: []string{link}}
	polA, err := b.Compile(ctxT(), specA, rep)
	if err != nil {
		t.Fatal(err)
	}
	polB, err := b.Compile(ctxT(), specB, rep)
	if err != nil {
		t.Fatal(err)
	}
	polC, err := b.Compile(ctxT(), specC, rep)
	if err != nil {
		t.Fatal(err)
	}
	if polA.PolicyHash() != polB.PolicyHash() {
		t.Fatalf("trailing-slash spelling produced a different PolicyHash: %s vs %s", polA.PolicyHash(), polB.PolicyHash())
	}
	if polA.PolicyHash() != polC.PolicyHash() {
		t.Fatalf("symlink alias produced a different PolicyHash: %s vs %s", polA.PolicyHash(), polC.PolicyHash())
	}
}

// Detector (code-review CODE2 codex finding #2, reproduced): a directory
// bound via ExtraROBinds and renamed away then replaced at the SAME path
// between Compile and Launch must be refused, not silently served — the
// resolved path alone is not a stable identity across time, only the
// (device, inode) pair is.
func TestLaunchRefusesROBindDirectorySwappedAfterCompile(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "toolchain")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(dir, "canary")
	if err := os.WriteFile(canary, []byte("BEFORE"), 0o644); err != nil {
		t.Fatal(err)
	}
	pol, err := b.Compile(ctxT(), Spec{
		Target: hp, Args: []string{"readfile", canary}, WorkDir: wdir(t),
		ExtraROBinds: []string{dir},
	}, rep)
	if err != nil {
		t.Fatal(err)
	}
	old := dir + ".old"
	if err := os.Rename(dir, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	canary2 := filepath.Join(dir, "canary")
	if err := os.WriteFile(canary2, []byte("AFTER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Launch(ctxT(), pol); err == nil {
		t.Fatal("Launch accepted an ExtraROBinds directory swapped at the same path after Compile")
	}
}

// Detector (code-review CODE3 codex finding: a real bypass of the identity
// pin, reproduced live): the identity-pin check silently SKIPPED when the
// canonical path resolved at Launch has no entry in ExtraROBindIdentities
// (a symlink retargeted between Compile and Launch resolves to a
// DIFFERENT canonical path than what Compile pinned — the old code's
// `ok && mismatch` check treated the resulting map miss as "no check",
// fail OPEN). ExtraROBinds names a SYMLINK whose target changes between
// Compile and Launch — the retargeted bind must be refused, not silently
// accepted under the new target's bytes.
func TestLaunchRefusesROBindSymlinkRetargetedAfterCompile(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	parent := t.TempDir()
	beforeDir := filepath.Join(parent, "before")
	afterDir := filepath.Join(parent, "after")
	if err := os.Mkdir(beforeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(afterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beforeDir, "canary"), []byte("BEFORE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(afterDir, "canary"), []byte("AFTER"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "toolchain")
	if err := os.Symlink(beforeDir, link); err != nil {
		t.Fatal(err)
	}
	pol, err := b.Compile(ctxT(), Spec{
		Target: hp, Args: []string{"readfile", filepath.Join(afterDir, "canary")}, WorkDir: wdir(t),
		ExtraROBinds: []string{link},
	}, rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(afterDir, link); err != nil {
		t.Fatal(err)
	}
	p, err := b.Launch(ctxT(), pol)
	if err != nil {
		return // refused at Launch — the desired outcome
	}
	defer p.Close()
	werr := p.Wait()
	if werr == nil && strings.Contains(p.Output(), "AFTER") {
		t.Fatalf("retargeted symlink bind was accepted and exposed replacement bytes: %q", p.Output())
	}
	if werr == nil {
		t.Fatalf("retargeted symlink bind was accepted (launch succeeded with no identity-mismatch refusal): output=%q", p.Output())
	}
}

// Detector (code-review CODE1 codex finding #2): CompiledPolicy must not
// alias the caller's Spec — a post-Compile, pre-Launch mutation of the
// caller's own ExtraEnv/Args must NOT change what actually runs, since
// PolicyHash was already sealed over the pre-mutation values. A shallow
// `spec: spec` in Compile would let this desync the attested hash from
// the executed process.
func TestCompiledPolicyDoesNotAliasCallerSpec(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	spec := Spec{
		Target: hp, Args: []string{"printenv", "NEXUS_ALIAS_TEST"}, WorkDir: wdir(t),
		ExtraEnv: map[string]string{"NEXUS_ALIAS_TEST": "original"},
	}
	pol, err := b.Compile(ctxT(), spec, rep)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	sealedHash := pol.PolicyHash()

	// Mutate the CALLER's spec after Compile returned.
	spec.ExtraEnv["NEXUS_ALIAS_TEST"] = "mutated"
	spec.Args[1] = "NOT_SET_AT_ALL"

	if pol.PolicyHash() != sealedHash {
		t.Fatal("PolicyHash changed after mutating the caller's own Spec post-Compile")
	}
	p, err := b.Launch(ctxT(), pol)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer p.Close()
	werr := p.Wait()
	out := p.Output()
	if werr != nil {
		t.Fatalf("launch of the ORIGINAL (pre-mutation) command failed: %v\n%s", werr, out)
	}
	if !strings.Contains(out, "original") {
		t.Fatalf("Launch ran the MUTATED env, not the sealed one: %q", out)
	}
}

// Detector (PLAN-CODING-TRIO.md Slice 0, gopls session prerequisite):
// LaunchInteractive actually delivers a LIVE, bidirectional stdio
// conversation — a write to Stdin() reaches the sandboxed child, and its
// reply is observable on Stdout() BEFORE the process exits (never just a
// combined buffer inspected after Wait, which is what Process/Launch
// gives). Uses probehelper's cat-stdin fixture, not gopls, to keep this
// package's own test suite independent of any external toolchain.
func TestLaunchInteractiveLiveStdioRoundTrip(t *testing.T) {
	b, rep := backend(t)
	bin := helperPath(t)
	spec := Spec{Target: bin, Args: []string{"cat-stdin"}, WorkDir: wdir(t), Timeout: 15 * time.Second}
	pol, err := b.Compile(ctxT(), spec, rep)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	p, err := b.LaunchInteractive(ctxT(), pol)
	if err != nil {
		t.Fatalf("launch interactive: %v", err)
	}
	defer p.Close()

	reader := bufio.NewReader(p.Stdout())
	for i := 0; i < 3; i++ {
		line := fmt.Sprintf("ping-%d\n", i)
		if _, err := io.WriteString(p.Stdin(), line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		got, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read reply %d: %v", i, err)
		}
		want := fmt.Sprintf("echo: ping-%d\n", i)
		if got != want {
			t.Fatalf("reply %d: got %q, want %q", i, got, want)
		}
	}
	p.Stdin().Close() // signal EOF; the child exits cleanly
	if err := p.Wait(); err != nil {
		t.Fatalf("wait: %v\nstderr: %s", err, p.Stderr())
	}
	att, err := b.AttestInteractive(ctxT(), p, pol)
	if err != nil {
		t.Fatalf("attest interactive: %v", err)
	}
	if att.PolicyHash != pol.PolicyHash() || att.ClosureDigest == "" {
		t.Fatalf("attestation does not bind policy+closure: %+v", att)
	}
}

// Detector: killing an InteractiveProcess unblocks a pending Stdout read
// (never hangs a caller mid-conversation) — a plain io.Pipe never
// signals EOF on its own, only an explicit Close/CloseWithError does.
func TestLaunchInteractiveKillUnblocksStdoutRead(t *testing.T) {
	b, rep := backend(t)
	bin := helperPath(t)
	spec := Spec{Target: bin, Args: []string{"hang"}, WorkDir: wdir(t), Timeout: 15 * time.Second}
	pol, err := b.Compile(ctxT(), spec, rep)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	p, err := b.LaunchInteractive(ctxT(), pol)
	if err != nil {
		t.Fatalf("launch interactive: %v", err)
	}
	defer p.Close()

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := p.Stdout().Read(buf)
		readDone <- err
	}()
	time.Sleep(200 * time.Millisecond) // let the read block first
	p.Kill()

	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("expected a non-nil error unblocking the read after Kill")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stdout read never unblocked after Kill")
	}
}

// Detector (PLAN-CODING-TRIO.md Slice 0, gopls session): ExtraPathDir is
// folded into PolicyHash like ExtraROBinds/ExtraEnv — two Specs differing
// only in ExtraPathDir must compile to DIFFERENT policies.
func TestPolicyHashChangesWithExtraPathDir(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	dir := robindDir(t)
	base := Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wdir(t), ExtraROBinds: []string{dir}}
	polBase, err := b.Compile(ctxT(), base, rep)
	if err != nil {
		t.Fatal(err)
	}
	withPath := base
	withPath.ExtraPathDir = dir
	polPath, err := b.Compile(ctxT(), withPath, rep)
	if err != nil {
		t.Fatal(err)
	}
	if polBase.PolicyHash() == polPath.PolicyHash() {
		t.Fatal("PolicyHash unchanged after setting ExtraPathDir")
	}
}

// Detector: ExtraPathDir naming a directory NOT covered by the Spec's own
// ExtraROBinds is refused fail-closed at Launch (probe.Prepare) — PATH
// can only ever resolve into a directory already granted read-only
// visibility, never widen what's visible.
func TestLaunchRefusesExtraPathDirOutsideROBinds(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	outside := robindDir(t) // NOT added to ExtraROBinds below
	spec := Spec{Target: hp, Args: []string{"printenv", "PATH"}, WorkDir: wdir(t),
		ExtraPathDir: outside}
	pol, err := b.Compile(ctxT(), spec, rep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Launch(ctxT(), pol); err == nil {
		t.Fatal("Launch accepted an ExtraPathDir not covered by ExtraROBinds")
	}
}

// Detector: ExtraPathDir nested UNDER an ExtraROBinds entry (not equal to
// it) is accepted, and PATH inside the sandbox actually resolves there —
// the real shape gopls needs (GOROOT is the ExtraROBinds entry,
// GOROOT/bin is ExtraPathDir).
func TestLaunchAcceptsExtraPathDirNestedUnderROBinds(t *testing.T) {
	b, rep := backend(t)
	hp := helperPath(t)
	parent := robindDir(t)
	child := filepath.Join(parent, "bin")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Target: hp, Args: []string{"printenv", "PATH"}, WorkDir: wdir(t),
		ExtraROBinds: []string{parent}, ExtraPathDir: child}
	out, werr := runThrough(t, b, rep, spec)
	if werr != nil {
		t.Fatalf("launch: %v\n%s", werr, out)
	}
	if !strings.Contains(out, child) {
		t.Fatalf("PATH did not resolve to the nested ExtraPathDir: %q", out)
	}
}

// Detector (code-review finding, codex): the PRODUCTION cancellation path
// — the caller's ctx being cancelled/expiring while LaunchInteractive's
// own cancel-watch goroutine reacts — must unblock a pending Stdout()
// read, exactly like an explicit p.Kill() does. The earlier
// TestLaunchInteractiveKillUnblocksStdoutRead only covered the explicit
// call; this covers the actual ctx.Done() seam every real caller
// (RunGoplsRename) goes through, which is a MEANINGFULLY DIFFERENT code
// path (the watch goroutine, not the caller's own Kill call) and was the
// one still calling the raw handle's Kill() instead of proc.Kill(),
// leaving the pipes never closed.
func TestLaunchInteractiveContextCancelUnblocksStdoutRead(t *testing.T) {
	b, rep := backend(t)
	bin := helperPath(t)
	spec := Spec{Target: bin, Args: []string{"hang"}, WorkDir: wdir(t), Timeout: 15 * time.Second}
	pol, err := b.Compile(ctxT(), spec, rep)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ctx, cancel := context.WithCancel(ctxT())
	p, err := b.LaunchInteractive(ctx, pol)
	if err != nil {
		t.Fatalf("launch interactive: %v", err)
	}
	defer p.Close()

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := p.Stdout().Read(buf)
		readDone <- err
	}()
	time.Sleep(200 * time.Millisecond) // let the read block first
	cancel()                           // the production seam: caller cancels ctx, NOT p.Kill() directly

	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("expected a non-nil error unblocking the read after ctx cancellation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stdout read never unblocked after ctx cancellation — the cancel-watch goroutine did not reach the pipes")
	}
}

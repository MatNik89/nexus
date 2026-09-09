//go:build linux

// Package sandbox is the S6.2-P0 owner: the SandboxBackend contract
// every TOOL-executing subprocess on the effect-path goes through
// (ExecProcess — CLAUDE.md hard rule), and its ONLY P0 implementation,
// bwrap. It wraps the T02-hardened probe runner (B4 content-pinned
// closure, disposable RW workdir, net unshared, seccomp floor, promoted
// absolute ELF only, scripts/shebang rejected BEFORE any sandbox) behind
// the four-step Probe→Compile→Launch→Attest protocol, and binds every
// result to a profile attestation: a result whose attestation does not
// match its compiled policy is UNTRUSTED.
// Trace: tasks-P0 T25; HARDQ D1/B4; DESIGN-S0 interface (P0-min).
package sandbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MatNik89/nexus/internal/preflight/probe"
)

// Spec is the sealed launch request: an absolute HOST path of a promoted
// ELF executable, its argv, a disposable RW workdir and a hard timeout.
type Spec struct {
	Target  string
	Args    []string
	WorkDir string
	Timeout time.Duration

	// ExtraROBinds/ExtraEnv thread through to probe.Spec unchanged — a
	// narrowly-scoped, ADDITIVE read-only visibility grant for a governed
	// toolchain child-process closure (PLAN-CODING-TRIO.md Slice 0). Both
	// are folded into policyHash below: two Specs differing only here
	// must compile to DIFFERENT policies (a caller cannot silently widen
	// visibility under an already-approved policy hash).
	ExtraROBinds []string
	ExtraEnv     map[string]string
}

// ProbeReport is the live host measurement a policy compiles against.
type ProbeReport struct {
	Available    bool
	BwrapPath    string
	BwrapVersion string
	// ProbeHash pins the measurement identity: a policy compiled under a
	// different probe is rejected at Launch (stale-measurement guard).
	ProbeHash string
}

// CompiledPolicy is a SEALED launch plan: constructed only by Compile
// (unexported fields — no caller can forge or loosen one).
type CompiledPolicy struct {
	spec        Spec
	probeHash   string
	targetHash  string
	closurePins map[string]string
	policyHash  string
}

// PolicyHash is the policy's identity digest (target, argv, workdir,
// timeout, probe hash — length-prefixed, no concatenation ambiguity).
func (p CompiledPolicy) PolicyHash() string { return p.policyHash }

// Process is one launched sandboxed process.
type Process struct {
	handle     *probe.Handle
	policyHash string
	closure    map[string]string
	output     *boundedBuffer
	started    bool
	done       chan struct{}
	doneOnce   sync.Once
}

// Output returns the combined stdout+stderr captured so far (bounded).
func (p *Process) Output() string { return p.output.String() }

// Wait blocks until exit or timeout kill; the process tree is dead after.
func (p *Process) Wait() error {
	err := p.handle.Wait()
	p.finish()
	return err
}

func (p *Process) finish() {
	if p.done != nil {
		p.doneOnce.Do(func() { close(p.done) })
	}
}

// Kill terminates the whole tree.
func (p *Process) Kill() { p.handle.Kill() }

// Close releases resources.
func (p *Process) Close() {
	p.finish()
	p.handle.Close()
}

// Attestation binds a finished (or running) process to the EXACT policy
// and content-pinned closure it launched under. A consumer accepts a
// sandboxed result ONLY with a verified attestation.
type Attestation struct {
	PolicyHash    string
	ClosureDigest string
	ProbeHash     string
	AttestedAt    time.Time
}

// Backend is the S6.2-P0 contract (DESIGN-S0, P0-min: no activation).
type Backend interface {
	Probe(ctx context.Context) (ProbeReport, error)
	Compile(ctx context.Context, spec Spec, report ProbeReport) (CompiledPolicy, error)
	Launch(ctx context.Context, policy CompiledPolicy) (*Process, error)
	Attest(ctx context.Context, p *Process, policy CompiledPolicy) (Attestation, error)
}

// Bwrap is the P0 backend over the T02-hardened probe runner.
type Bwrap struct {
	av        probe.Availability
	bwrapHash string
	ok        bool
}

// NewBwrap constructs the backend; availability is measured by Probe.
func NewBwrap() *Bwrap { return &Bwrap{} }

// Probe measures the live host FAIL-CLOSED: bwrap absent or below the
// floor version returns an error and an unusable report — never a weaker
// fallback (T02 RED a).
func (b *Bwrap) Probe(ctx context.Context) (ProbeReport, error) {
	av, err := probe.Detect()
	if err != nil {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: %w", err)
	}
	// TRUST ROOT (Phase-6-r2 codex #1): a finite black-box canary cannot
	// authenticate an ADAPTIVE malicious backend, so the backend binary
	// must live on a root-owned, non-user-writable path BEFORE any
	// behavioral check — a fake dropped into a user-writable PATH entry
	// never becomes the trust anchor. Symlinks are resolved first.
	canonPath, err := filepath.EvalSymlinks(av.BwrapPath)
	if err != nil {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: backend path: %w", err)
	}
	st, err := os.Stat(canonPath)
	if err != nil {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: backend stat: %w", err)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid != 0 || st.Mode().Perm()&0o022 != 0 {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: backend %s is not a root-owned, non-writable executable — refusing to trust it", canonPath)
	}
	av.BwrapPath = canonPath
	// IDENTITY: the probe binds the bwrap BINARY CONTENT, not just its
	// pathname and self-reported version (Phase-6 codex #2: a fake that
	// prints a version string must not become the trust anchor).
	bwrapHash, err := hashFile(av.BwrapPath)
	if err != nil {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: cannot read the backend binary: %w", err)
	}
	// ENFORCEMENT: one live negative control THROUGH this exact backend —
	// a host directory outside the closure must be INVISIBLE inside. An
	// unconfined fake passes the version check but fails this.
	// Random, unmarked canary path: an adaptive fake cannot pattern-match
	// the probe invocation (defense in depth under the trust root).
	canaryDir, err := os.MkdirTemp("", randomHex(8))
	if err != nil {
		return ProbeReport{}, err
	}
	defer os.RemoveAll(canaryDir)
	if err := os.WriteFile(filepath.Join(canaryDir, "canary"), []byte("c"), 0o600); err != nil {
		return ProbeReport{}, err
	}
	if _, runErr := probe.Run(av, probe.Spec{Target: "/bin/ls", Args: []string{canaryDir},
		Timeout: 20 * time.Second}); runErr == nil {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: the backend did NOT confine (closure-external directory visible) — refusing to treat it as a sandbox")
	}
	// Positive control: a closure-internal run must still work.
	if _, runErr := probe.Run(av, probe.Spec{Target: "/bin/ls", Args: []string{"/"},
		Timeout: 20 * time.Second}); runErr != nil {
		return ProbeReport{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: confined positive control failed: %w", runErr)
	}
	b.av, b.bwrapHash, b.ok = av, bwrapHash, true
	return ProbeReport{
		Available: true, BwrapPath: av.BwrapPath, BwrapVersion: av.BwrapVersion,
		ProbeHash: digest("probe", av.BwrapPath, av.BwrapVersion, bwrapHash),
	}, nil
}

// Compile seals a launch plan against a live probe report. Everything
// invalid is rejected HERE, before any process exists: relative or
// non-ELF targets (the ELF check itself runs inside Prepare at Launch —
// Compile guards shape), missing report, zero/negative timeout ceilings.
func (b *Bwrap) Compile(ctx context.Context, spec Spec, report ProbeReport) (CompiledPolicy, error) {
	if !report.Available || report.ProbeHash == "" {
		return CompiledPolicy{}, fmt.Errorf("sandbox: compile requires a live passing probe report (fail closed)")
	}
	if !filepath.IsAbs(spec.Target) {
		return CompiledPolicy{}, fmt.Errorf("sandbox: launch target must be an absolute host path (fail closed)")
	}
	if spec.WorkDir != "" && !filepath.IsAbs(spec.WorkDir) {
		return CompiledPolicy{}, fmt.Errorf("sandbox: workdir must be absolute (fail closed)")
	}
	if spec.Timeout <= 0 {
		spec.Timeout = 30 * time.Second
	}
	if spec.Timeout > 10*time.Minute {
		return CompiledPolicy{}, fmt.Errorf("sandbox: timeout above the 10-minute ceiling (fail closed)")
	}
	// PIN THE EXECUTABLE BYTES at compile time (Phase-6 codex #3): the
	// policy — and therefore the C4 approval built over its call — binds
	// the CONTENT that will run, not a mutable pathname. Launch verifies
	// the memfd-pinned target against this digest.
	targetHash, err := hashFile(spec.Target)
	if err != nil {
		return CompiledPolicy{}, fmt.Errorf("sandbox: cannot pin the launch target (fail closed): %w", err)
	}
	// Pin the ENTIRE runtime closure — loader and every library — not
	// just the main ELF (Phase-6-r2 codex #2: a $ORIGIN library swapped
	// after Compile executed and attested under the old policy).
	pins, err := probe.ResolveClosureHashes(spec.Target)
	if err != nil {
		return CompiledPolicy{}, fmt.Errorf("sandbox: cannot pin the runtime closure (fail closed): %w", err)
	}
	roDigest, err := roBindsDigest(spec.ExtraROBinds)
	if err != nil {
		return CompiledPolicy{}, fmt.Errorf("sandbox: %w", err)
	}
	sealed := digest("policy", spec.Target, targetHash, closureDigest(pins),
		strings.Join(spec.Args, "\x00"),
		spec.WorkDir, spec.Timeout.String(), report.ProbeHash,
		roDigest, envDigest(spec.ExtraEnv))
	// Deep-copy spec's slice/map fields (code-review CODE1 codex finding
	// #2): a shallow `spec: spec` here would alias the caller's Args/
	// ExtraROBinds/ExtraEnv — a post-Compile, pre-Launch mutation by the
	// caller would then change what Launch actually runs while
	// policyHash above still reflects the pre-mutation values, breaking
	// the attestation binding between the two.
	return CompiledPolicy{
		spec:        cloneSpec(spec),
		probeHash:   report.ProbeHash,
		targetHash:  targetHash,
		closurePins: pins,
		policyHash:  sealed,
	}, nil
}

// cloneSpec deep-copies the slice/map fields of Spec so a CompiledPolicy
// can never be desynced from the caller's own Spec value after Compile
// returns (code-review CODE1 codex finding #2).
func cloneSpec(spec Spec) Spec {
	out := spec
	if spec.Args != nil {
		out.Args = append([]string(nil), spec.Args...)
	}
	if spec.ExtraROBinds != nil {
		out.ExtraROBinds = append([]string(nil), spec.ExtraROBinds...)
	}
	if spec.ExtraEnv != nil {
		out.ExtraEnv = make(map[string]string, len(spec.ExtraEnv))
		for k, v := range spec.ExtraEnv {
			out.ExtraEnv[k] = v
		}
	}
	return out
}

// roBindsDigest folds a read-only bind list into one deterministic digest
// (order-independent — the list is sorted first).
// roBindsDigest canonicalizes each path via EvalSymlinks BEFORE hashing
// (code-review CODE1 agy finding #3): two Specs naming the same effective
// bind through different syntactic spellings (a trailing slash, a
// symlink alias) must fold to the SAME policyHash — Prepare's own
// guardROBind canonicalizes independently for its security gate, but
// policyHash's job is to bind approval to the ACTUAL effective boundary,
// which is the canonical form, not the caller's literal string.
func roBindsDigest(binds []string) (string, error) {
	canon := make([]string, 0, len(binds))
	for _, b := range binds {
		c, err := filepath.EvalSymlinks(filepath.Clean(b))
		if err != nil {
			return "", fmt.Errorf("sandbox: cannot canonicalize ExtraROBinds entry %q: %w", b, err)
		}
		canon = append(canon, c)
	}
	sort.Strings(canon)
	h := sha256.New()
	for _, b := range canon {
		fmt.Fprintf(h, "%d:%s", len(b), b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// envDigest folds an env map into one deterministic digest (key-sorted).
func envDigest(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%d:%s=%d:%s", len(k), k, len(env[k]), env[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// closureDigest folds a closure pin map into one deterministic digest.
func closureDigest(pins map[string]string) string {
	keys := make([]string, 0, len(pins))
	for k := range pins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%d:%s%d:%s", len(k), k, len(pins[k]), pins[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hashFile is the bounded sha256 of a file's bytes.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 512<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Launch starts the sandboxed process under the sealed policy. The
// policy must originate from THIS backend's current probe (S1.2 process
// identity is the child pid inside the prepared handle; the probe layer
// owns setpgid + die-with-parent + kill-tree).
func (b *Bwrap) Launch(ctx context.Context, policy CompiledPolicy) (*Process, error) {
	// S7 owns cancellation (Phase-6 codex #4): an already-cancelled
	// attempt context must never start a process, and a later cancel
	// kills the whole tree.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sandbox: attempt context already cancelled — not launching (fail closed): %w", err)
	}
	if policy.policyHash == "" {
		return nil, fmt.Errorf("sandbox: launch requires a compiled policy (fail closed)")
	}
	if !b.ok {
		return nil, fmt.Errorf("sandbox: launch before a passing probe (fail closed)")
	}
	// Re-verify the backend binary NOW (TOCTOU on the trust anchor):
	// a bwrap swapped after the probe invalidates every policy.
	liveHash, err := hashFile(b.av.BwrapPath)
	if err != nil || liveHash != b.bwrapHash {
		return nil, fmt.Errorf("sandbox: the backend binary changed since the probe — refused (fail closed)")
	}
	if policy.probeHash != digest("probe", b.av.BwrapPath, b.av.BwrapVersion, b.bwrapHash) {
		return nil, fmt.Errorf("sandbox: policy was compiled under a DIFFERENT probe — stale measurement (fail closed)")
	}
	timeout := policy.spec.Timeout
	if dl, ok := ctx.Deadline(); ok {
		if until := time.Until(dl); until < timeout {
			timeout = until
		}
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("sandbox: attempt deadline already passed (fail closed)")
	}
	h, err := probe.Prepare(b.av, probe.Spec{
		Target: policy.spec.Target, Args: policy.spec.Args,
		WorkDir: policy.spec.WorkDir, Timeout: timeout,
		ExtraROBinds: policy.spec.ExtraROBinds, ExtraEnv: policy.spec.ExtraEnv,
	})
	if err != nil {
		return nil, err
	}
	// EVERY memfd-pinned closure member must be EXACTLY the compile-time
	// content — target, loader and libraries alike (codex #3 + r2 #2).
	launched := h.ClosureHashes()
	if got := launched["/nexus-target"]; got != policy.targetHash {
		h.Close()
		return nil, fmt.Errorf("sandbox: launch target bytes changed since Compile — refused (fail closed)")
	}
	if len(launched) != len(policy.closurePins) {
		h.Close()
		return nil, fmt.Errorf("sandbox: runtime closure shape changed since Compile — refused (fail closed)")
	}
	for dest, want := range policy.closurePins {
		if launched[dest] != want {
			h.Close()
			return nil, fmt.Errorf("sandbox: closure member %s changed since Compile — refused (fail closed)", dest)
		}
	}
	// One buffer for BOTH streams is safe WITHOUT a mutex: os/exec
	// documents that when Stdout and Stderr are the same ==-comparable
	// writer, at most one goroutine at a time calls Write (fresh-audit
	// kilo F1 rejected on that guarantee — do not "fix" this).
	out := &boundedBuffer{limit: 1 << 20}
	if err := h.SetOutput(out, out); err != nil {
		h.Close()
		return nil, err
	}
	if err := h.Start(); err != nil {
		h.Close()
		return nil, err
	}
	proc := &Process{handle: h, policyHash: policy.policyHash,
		closure: h.ClosureHashes(), output: out, started: true,
		done: make(chan struct{})}
	// Cancel-watch: S7 cancellation reaches the live tree.
	go func() {
		select {
		case <-ctx.Done():
			h.Kill()
		case <-proc.done:
		}
	}()
	return proc, nil
}

// Attest binds the process to its policy and content-pinned closure. A
// process launched under a different policy fails here — the caller must
// treat its result as UNTRUSTED (T25 RED: attestation mismatch).
func (b *Bwrap) Attest(ctx context.Context, p *Process, policy CompiledPolicy) (Attestation, error) {
	if p == nil || !p.started {
		return Attestation{}, fmt.Errorf("sandbox: nothing to attest (fail closed)")
	}
	if p.policyHash != policy.policyHash {
		return Attestation{}, fmt.Errorf("sandbox: ATTESTATION_MISMATCH — the process did not launch under this policy; its result is untrusted")
	}
	keys := make([]string, 0, len(p.closure))
	for k := range p.closure {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%d:%s%d:%s", len(k), k, len(p.closure[k]), p.closure[k])
	}
	return Attestation{
		PolicyHash:    policy.policyHash,
		ClosureDigest: hex.EncodeToString(h.Sum(nil)),
		ProbeHash:     policy.probeHash,
		AttestedAt:    time.Now().UTC(),
	}, nil
}

// randomHex returns n random bytes hex-encoded (canary naming).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "nexus-canary"
	}
	return hex.EncodeToString(b)
}

// digest is a length-prefixed sha256 over parts.
func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s", len(p), p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// boundedBuffer caps captured output (observation pruning lower bound;
// T26 prunes further but the transport itself must never grow unbounded).
type boundedBuffer struct {
	buf   strings.Builder
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.buf.Len()
	if room <= 0 {
		return len(p), nil // swallow beyond the cap; the process still runs
	}
	if len(p) > room {
		b.buf.Write(p[:room])
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *boundedBuffer) String() string { return b.buf.String() }

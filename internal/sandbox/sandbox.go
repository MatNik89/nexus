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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
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
	spec       Spec
	probeHash  string
	policyHash string
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
}

// Output returns the combined stdout+stderr captured so far (bounded).
func (p *Process) Output() string { return p.output.String() }

// Wait blocks until exit or timeout kill; the process tree is dead after.
func (p *Process) Wait() error { return p.handle.Wait() }

// Kill terminates the whole tree.
func (p *Process) Kill() { p.handle.Kill() }

// Close releases resources.
func (p *Process) Close() { p.handle.Close() }

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
	av probe.Availability
	ok bool
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
	b.av, b.ok = av, true
	return ProbeReport{
		Available: true, BwrapPath: av.BwrapPath, BwrapVersion: av.BwrapVersion,
		ProbeHash: digest("probe", av.BwrapPath, av.BwrapVersion),
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
	return CompiledPolicy{
		spec:      spec,
		probeHash: report.ProbeHash,
		policyHash: digest("policy", spec.Target, strings.Join(spec.Args, "\x00"),
			spec.WorkDir, spec.Timeout.String(), report.ProbeHash),
	}, nil
}

// Launch starts the sandboxed process under the sealed policy. The
// policy must originate from THIS backend's current probe (S1.2 process
// identity is the child pid inside the prepared handle; the probe layer
// owns setpgid + die-with-parent + kill-tree).
func (b *Bwrap) Launch(ctx context.Context, policy CompiledPolicy) (*Process, error) {
	if policy.policyHash == "" {
		return nil, fmt.Errorf("sandbox: launch requires a compiled policy (fail closed)")
	}
	if !b.ok {
		return nil, fmt.Errorf("sandbox: launch before a passing probe (fail closed)")
	}
	if policy.probeHash != digest("probe", b.av.BwrapPath, b.av.BwrapVersion) {
		return nil, fmt.Errorf("sandbox: policy was compiled under a DIFFERENT probe — stale measurement (fail closed)")
	}
	h, err := probe.Prepare(b.av, probe.Spec{
		Target: policy.spec.Target, Args: policy.spec.Args,
		WorkDir: policy.spec.WorkDir, Timeout: policy.spec.Timeout,
	})
	if err != nil {
		return nil, err
	}
	out := &boundedBuffer{limit: 1 << 20}
	if err := h.SetOutput(out, out); err != nil {
		h.Close()
		return nil, err
	}
	if err := h.Start(); err != nil {
		h.Close()
		return nil, err
	}
	return &Process{handle: h, policyHash: policy.policyHash,
		closure: h.ClosureHashes(), output: out, started: true}, nil
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

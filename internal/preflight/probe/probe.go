//go:build linux

// Package probe is the standalone sandbox feasibility probe (tasks-P0 T02).
// It has no kernel dependencies. It drives bwrap (the P0 ENFORCED backend,
// HARDQ D1) across the capability columns FS_RO / FS_RW / NET_DENY /
// SYSCALL / PROC_TREE.
//
// Closure model (B4 as refined by the Phase-0 review): the sandbox sees a
// SYNTHETIC closure — only the promoted ELF target and its resolved
// loader/library dependencies. Each file is copied into a SEALED memfd at
// Prepare time and bound via bwrap --ro-bind-data: after Prepare, neither a
// path swap, an inode truncation, nor a write through the descriptor can
// change what runs. Dependencies are resolved by parsing ELF metadata
// (PT_INTERP + DT_NEEDED + RUNPATH) — the untrusted target is NEVER
// executed to discover them (no ldd; Phase-0 r2 codex #4).
package probe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Availability reports whether the bwrap backend can be used at all.
// Absent or non-functional backend => exec capability OFF (fail-closed).
type Availability struct {
	BwrapPath    string
	BwrapVersion string
}

// MinBwrapVersion is the published floor (docs/PROBE-REPORT, HARDQ C5).
const MinBwrapVersion = "0.8.0"

// Detect fails closed: any error means UNAVAILABLE.
func Detect() (Availability, error) {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return Availability{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: bwrap not found: %w", err)
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return Availability{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: bwrap --version failed: %w", err)
	}
	ver := string(bytes.TrimSpace(out))
	fields := strings.Fields(ver)
	if len(fields) < 2 || versionLess(fields[len(fields)-1], MinBwrapVersion) {
		return Availability{}, fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: %q below floor %s", ver, MinBwrapVersion)
	}
	return Availability{BwrapPath: path, BwrapVersion: ver}, nil
}

// FloorProbe proves the WHOLE production launch path works on this host —
// closure resolution, sealed memfds, bwrap namespaces AND the seccomp
// syscall floor — by running a harmless static ELF that attempts
// PTRACE_TRACEME and must be DENIED (Phase-0 r2 codex #1: a weaker separate
// bwrap invocation proved nothing about real launches).
// probeTarget must be an ELF whose given args attempt ptrace and print
// "denied" on the denial path (nexus: `__probe-ptrace`; tests: probehelper
// `syscall-ptrace`).
func FloorProbe(av Availability, probeTarget string, args []string) error {
	out, err := Run(av, Spec{Target: probeTarget, Args: args, Timeout: 15 * time.Second})
	if err == nil {
		return fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: syscall floor INACTIVE (ptrace succeeded inside sandbox)")
	}
	if !strings.Contains(out, "denied") {
		return fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: confined launch failed on this host: %v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// Spec describes one sandboxed launch of a promoted ELF target.
// It cannot express a weakened boundary: negative-control switches live in
// an unexported loosen struct reachable only from this package's tests.
type Spec struct {
	Target  string // absolute HOST path of the promoted executable (ELF only)
	Args    []string
	WorkDir string // host dir bound read-write at /work (guarded; see Prepare)
	Timeout time.Duration

	loosen loosen
}

type loosen struct {
	roBind  string
	net     bool
	pidNS   bool
	seccomp bool
}

// ErrNotELF: the launch target is not a native ELF executable (shebang,
// script, foreign architecture). Refused BEFORE any sandbox is set up.
var ErrNotELF = fmt.Errorf("launch target is not a native ELF executable (scripts/shebang/foreign-arch rejected)")

const (
	maxClosureFiles = 64
	maxClosureBytes = 256 << 20
)

type closureFile struct {
	f    *os.File
	dest string
	hash string
}

func nativeMachine() (elf.Machine, error) {
	switch runtime.GOARCH {
	case "arm64":
		return elf.EM_AARCH64, nil
	case "amd64":
		return elf.EM_X86_64, nil
	default:
		return elf.EM_NONE, fmt.Errorf("unsupported GOARCH %s (fail closed)", runtime.GOARCH)
	}
}

// openNativeELF parses hdr and enforces native class+machine (Phase-0 r2
// codex #7: a compat-arch ELF must never run under a filter keyed to the
// native audit arch).
func openNativeELF(path string) (*elf.File, error) {
	ef, err := elf.Open(path)
	if err != nil {
		return nil, ErrNotELF
	}
	native, err := nativeMachine()
	if err != nil {
		ef.Close()
		return nil, err
	}
	if ef.Machine != native || ef.Class != elf.ELFCLASS64 {
		ef.Close()
		return nil, ErrNotELF
	}
	return ef, nil
}

// depSearchDirs returns the deterministic trusted library search order.
func depSearchDirs(runpaths []string) []string {
	dirs := append([]string{}, runpaths...)
	for _, d := range []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64"} {
		dirs = append(dirs, d)
		if matches, _ := filepath.Glob(d + "/*-linux-gnu*"); matches != nil {
			dirs = append(dirs, matches...)
		}
	}
	return dirs
}

// resolveClosure resolves target + interpreter + transitive DT_NEEDED
// libraries by ELF metadata only, pinning each into a sealed memfd.
func resolveClosure(target string) (files []closureFile, insideTarget string, err error) {
	total := 0
	closeAll := func() {
		for _, cf := range files {
			cf.f.Close()
		}
	}
	seen := map[string]bool{}
	add := func(hostPath, dest string) error {
		if seen[dest] {
			return nil
		}
		seen[dest] = true
		if len(files) >= maxClosureFiles {
			return fmt.Errorf("closure exceeds %d files", maxClosureFiles)
		}
		buf, err := os.ReadFile(hostPath)
		if err != nil {
			return fmt.Errorf("closure file %s: %w", hostPath, err)
		}
		total += len(buf)
		if total > maxClosureBytes {
			return fmt.Errorf("closure exceeds %d bytes", maxClosureBytes)
		}
		sum := sha256.Sum256(buf)
		mf, err := memfdWithContent(filepath.Base(dest), buf)
		if err != nil {
			return fmt.Errorf("pinning %s: %w", hostPath, err)
		}
		files = append(files, closureFile{f: mf, dest: dest, hash: hex.EncodeToString(sum[:])})
		return nil
	}

	ef, err := openNativeELF(target)
	if err != nil {
		return nil, "", err
	}
	interp := ""
	for _, p := range ef.Progs {
		if p.Type == elf.PT_INTERP {
			b := make([]byte, p.Filesz)
			p.ReadAt(b, 0)
			interp = strings.TrimRight(string(b), "\x00")
		}
	}
	var runpaths []string
	if rp, err := ef.DynString(elf.DT_RUNPATH); err == nil {
		for _, r := range rp {
			runpaths = append(runpaths, strings.Split(r, ":")...)
		}
	}
	needed, _ := ef.ImportedLibraries()
	ef.Close()

	insideTarget = "/nexus-target"
	if err := add(target, insideTarget); err != nil {
		closeAll()
		return nil, "", err
	}
	if interp != "" {
		if err := add(interp, interp); err != nil {
			closeAll()
			return nil, "", err
		}
	}
	// BFS over DT_NEEDED across trusted system libraries.
	dirs := depSearchDirs(runpaths)
	queue := append([]string{}, needed...)
	resolvedNames := map[string]bool{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if resolvedNames[name] {
			continue
		}
		resolvedNames[name] = true
		found := ""
		for _, d := range dirs {
			cand := filepath.Join(d, name)
			if st, err := os.Stat(cand); err == nil && st.Mode().IsRegular() {
				found = cand
				break
			}
		}
		if found == "" {
			closeAll()
			return nil, "", fmt.Errorf("cannot resolve library %q for %s", name, target)
		}
		if err := add(found, found); err != nil {
			closeAll()
			return nil, "", err
		}
		if lef, err := elf.Open(found); err == nil { // system lib: trusted parse
			if sub, err := lef.ImportedLibraries(); err == nil {
				queue = append(queue, sub...)
			}
			lef.Close()
		}
	}
	return files, insideTarget, nil
}

// guardWorkDir canonicalizes and rejects hazardous RW grants: root, shallow
// paths, foreign ownership, group/world-writable dirs.
// topknot ceiling: bwrap still binds by PATH, so a same-user swap between
// this check and namespace setup remains possible; the T25 backend closes it
// with a policy-owned disposable-directory capability. Upgrade trigger: T25.
func guardWorkDir(dir string) (string, error) {
	wd, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}
	if wd == "/" || len(strings.Split(strings.Trim(wd, "/"), "/")) < 2 {
		return "", fmt.Errorf("workdir %q refused: too broad for a disposable RW grant", wd)
	}
	st, err := os.Stat(wd)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("workdir %s is not a directory", dir)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(sys.Uid) != os.Geteuid() {
		return "", fmt.Errorf("workdir %s not owned by the current user", wd)
	}
	// World-writable is refused outright. Group-writable is tolerated because
	// user-private-group distros (umask 002) create 0775 dirs by default;
	// the T25 backend's policy-owned disposable dirs will be 0700 regardless.
	if st.Mode().Perm()&0o002 != 0 {
		return "", fmt.Errorf("workdir %s is world-writable", wd)
	}
	return wd, nil
}

// Handle is a running (or prepared) sandboxed process. The underlying
// command is PRIVATE: no caller can alter argv, fds or environment after
// Prepare (Phase-0 r2 codex #5).
type Handle struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	closers   []*os.File
	closeOnce sync.Once
	hashes    map[string]string
	started   bool
}

// ClosureHashes returns a copy of the sha256-per-inside-path attestation.
func (h *Handle) ClosureHashes() map[string]string {
	out := make(map[string]string, len(h.hashes))
	for k, v := range h.hashes {
		out[k] = v
	}
	return out
}

// SetOutput wires stdout/stderr; only valid before Start.
func (h *Handle) SetOutput(stdout, stderr io.Writer) error {
	if h.started {
		return fmt.Errorf("SetOutput after Start")
	}
	h.cmd.Stdout, h.cmd.Stderr = stdout, stderr
	return nil
}

// Pid returns the sandbox leader pid, or 0 before Start.
func (h *Handle) Pid() int {
	if h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// Start begins execution.
func (h *Handle) Start() error {
	err := h.cmd.Start()
	h.started = err == nil
	if err != nil {
		h.Close()
	}
	return err
}

// Kill terminates the sandbox leader; PID isolation + --die-with-parent
// take everything inside down with it.
func (h *Handle) Kill() {
	if h.cmd.Process != nil {
		h.cmd.Process.Kill()
	}
	h.cancel()
}

// Wait reaps the leader and releases every owned descriptor exactly once.
func (h *Handle) Wait() error {
	var err error
	if h.started {
		err = h.cmd.Wait()
	}
	h.Close()
	return err
}

// Close releases owned resources; safe to call on every failure path.
func (h *Handle) Close() {
	h.closeOnce.Do(func() {
		for _, f := range h.closers {
			f.Close()
		}
		h.cancel()
	})
}

// Prepare validates the target (native-ELF-only), resolves and seals its
// closure, and builds the bwrap command WITHOUT starting it. This is the
// ONLY launch path; tests use it unmodified.
func Prepare(av Availability, spec Spec) (*Handle, error) {
	if spec.Timeout <= 0 {
		spec.Timeout = 30 * time.Second
	}
	files, insideTarget, err := resolveClosure(spec.Target)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*Handle, error) {
		for _, cf := range files {
			cf.f.Close()
		}
		return nil, e
	}

	args := []string{
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--dir", "/work",
		"--clearenv", "--setenv", "PATH", "/nowhere",
		"--new-session",
	}
	if spec.WorkDir != "" {
		wd, err := guardWorkDir(spec.WorkDir)
		if err != nil {
			return fail(err)
		}
		args = append(args, "--bind", wd, "/work")
	}
	if spec.loosen.roBind != "" {
		args = append(args, "--ro-bind", spec.loosen.roBind, spec.loosen.roBind)
	}
	if !spec.loosen.net {
		args = append(args, "--unshare-net")
	}
	if !spec.loosen.pidNS {
		args = append(args, "--unshare-pid", "--die-with-parent")
	}

	ctx, cancel := context.WithTimeout(context.Background(), spec.Timeout)
	cmd := exec.CommandContext(ctx, av.BwrapPath)
	hashes := map[string]string{}
	var closers []*os.File
	fdNum := 3
	for _, cf := range files {
		cmd.ExtraFiles = append(cmd.ExtraFiles, cf.f)
		closers = append(closers, cf.f)
		args = append(args, "--perms", "0555", "--ro-bind-data", fmt.Sprint(fdNum), cf.dest)
		hashes[cf.dest] = cf.hash
		fdNum++
	}
	if !spec.loosen.seccomp {
		filt, err := seccompFilter()
		if err != nil {
			cancel()
			return fail(fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: syscall floor: %w", err))
		}
		cmd.ExtraFiles = append(cmd.ExtraFiles, filt)
		closers = append(closers, filt)
		args = append(args, "--seccomp", fmt.Sprint(fdNum))
		fdNum++
	}
	args = append(args, insideTarget)
	args = append(args, spec.Args...)
	cmd.Args = append([]string{av.BwrapPath}, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	return &Handle{cmd: cmd, cancel: cancel, closers: closers, hashes: hashes}, nil
}

// Run prepares, starts and waits, returning combined output.
func Run(av Availability, spec Spec) (string, error) {
	h, err := Prepare(av, spec)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	h.SetOutput(&out, &out)
	if err := h.Start(); err != nil {
		return "", err
	}
	err = h.Wait()
	return out.String(), err
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		var x, y int
		fmt.Sscanf(pa[i], "%d", &x)
		fmt.Sscanf(pb[i], "%d", &y)
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

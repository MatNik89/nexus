//go:build linux

// Package probe is the standalone sandbox feasibility probe (tasks-P0 T02).
// It has no kernel dependencies. It drives bwrap (the P0 ENFORCED backend,
// HARDQ D1) across the capability columns FS_RO / FS_RW / NET_DENY /
// SYSCALL / PROC_TREE.
//
// Closure model (B4 as refined by the Phase-0 review): the sandbox sees a
// SYNTHETIC closure — only the promoted ELF target and its resolved
// loader/library dependencies, each content-pinned via bwrap --ro-bind-data
// file descriptors (the bytes are fixed at hash time, so a path swap after
// validation cannot change what runs). No blanket /usr grant: an undeclared
// child executable simply does not exist inside the mount namespace.
package probe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Availability reports whether the bwrap backend can be used at all.
// Absent or non-functional backend => exec capability OFF (fail-closed);
// there is no weaker fallback.
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

// FloorProbe proves the kernel actually permits an unprivileged sandbox
// (user namespaces enabled and usable) by launching a trivial confined
// process. bwrap --version succeeding is NOT sufficient (kilo Phase-0 #1).
func FloorProbe(av Availability) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, av.BwrapPath,
		"--unshare-user", "--unshare-pid", "--unshare-net",
		"--dev", "/dev", "--proc", "/proc",
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin", "--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib64", "/lib64",
		"--clearenv", "/usr/bin/true")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: kernel floor probe failed (user namespaces disabled?): %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// Spec describes one sandboxed launch of a promoted ELF target.
// It cannot express a weakened boundary: the negative-control switches live
// in an unexported loosen struct reachable only from this package's tests.
type Spec struct {
	Target  string   // absolute HOST path of the promoted executable (ELF only)
	Args    []string
	WorkDir string   // host dir bound read-write at /work (canonicalized)
	Timeout time.Duration

	loosen loosen
}

// loosen holds hazard-safe negative-control switches (test-owned resources
// only). Unexported: production callers cannot set them (codex Phase-0 #7).
type loosen struct {
	roBind  string // extra host dir bound read-only (canary dir)
	net     bool   // do NOT unshare the network namespace
	pidNS   bool   // do NOT unshare pid / die-with-parent
	seccomp bool   // do NOT install the syscall-floor filter
}

// ErrNotELF: the launch target is not a plain ELF executable (shebang or
// other script). Refused BEFORE any sandbox is set up.
var ErrNotELF = fmt.Errorf("launch target is not an ELF executable (scripts/shebang rejected)")

// closureFile is one content-pinned file the sandbox will see.
type closureFile struct {
	f    *os.File
	dest string // absolute path inside the sandbox
	hash string
}

// resolveClosure opens the target and every transitively needed library plus
// the ELF interpreter, hashing each. Files stay open; their bytes are what
// the sandbox receives (--ro-bind-data), so post-hash swaps are inert.
func resolveClosure(target string) (files []closureFile, insideTarget string, err error) {
	closeAll := func() {
		for _, cf := range files {
			cf.f.Close()
		}
	}
	// add copies the file's bytes into an anonymous memfd at Prepare time:
	// after this, neither a rename NOR an in-place truncation of the host
	// path can change what the sandbox receives (proven by
	// TestTargetSwapAfterPrepareIsInert — an earlier fd-only design failed
	// it, because O_TRUNC rewrites the pinned inode).
	add := func(hostPath, dest string) error {
		buf, err := os.ReadFile(hostPath)
		if err != nil {
			return fmt.Errorf("closure file %s: %w", hostPath, err)
		}
		sum := sha256.Sum256(buf)
		mf, err := memfdWithContent(filepath.Base(dest), buf)
		if err != nil {
			return fmt.Errorf("pinning %s: %w", hostPath, err)
		}
		files = append(files, closureFile{f: mf, dest: dest, hash: hex.EncodeToString(sum[:])})
		return nil
	}

	ef, err := elf.Open(target)
	if err != nil {
		return nil, "", ErrNotELF
	}
	interp := ""
	for _, p := range ef.Progs {
		if p.Type == elf.PT_INTERP {
			b := make([]byte, p.Filesz)
			p.ReadAt(b, 0)
			interp = strings.TrimRight(string(b), "\x00")
		}
	}
	ef.Close()

	insideTarget = "/nexus-target"
	if err := add(target, insideTarget); err != nil {
		return nil, "", err
	}
	if interp != "" {
		// Dynamic ELF: resolve the full library set with the host loader's
		// own view (ldd on an already-promoted, hash-pinned target).
		if err := add(interp, interp); err != nil {
			closeAll()
			return nil, "", err
		}
		out, err := exec.Command("ldd", target).Output()
		if err != nil {
			closeAll()
			return nil, "", fmt.Errorf("resolving library closure: %w", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			// forms: "libc.so.6 => /usr/lib/... (0x..)" | "/lib/ld-... (0x..)"
			var lib string
			if i := strings.Index(line, "=>"); i >= 0 {
				lib = strings.TrimSpace(line[i+2:])
			} else {
				lib = strings.TrimSpace(line)
			}
			if j := strings.Index(lib, " ("); j >= 0 {
				lib = lib[:j]
			}
			lib = strings.TrimSpace(lib)
			if lib == "" || !strings.HasPrefix(lib, "/") || lib == interp {
				continue
			}
			if err := add(lib, lib); err != nil {
				closeAll()
				return nil, "", err
			}
		}
	}
	return files, insideTarget, nil
}

// Handle is a running sandboxed process (the production launch path).
type Handle struct {
	Cmd     *exec.Cmd
	cancel  context.CancelFunc
	closers []*os.File
	// ClosureHashes records sha256 per inside-path (attestation seed).
	ClosureHashes map[string]string
}

// Kill terminates the sandbox leader; with PID isolation + --die-with-parent
// everything inside dies with it.
func (h *Handle) Kill() {
	if h.Cmd.Process != nil {
		h.Cmd.Process.Kill()
	}
	h.cancel()
}

// Wait reaps the leader and releases closure file descriptors.
func (h *Handle) Wait() error {
	err := h.Cmd.Wait()
	for _, f := range h.closers {
		f.Close()
	}
	h.cancel()
	return err
}

// Prepare validates the target (ELF-only), resolves and content-pins its
// closure, and builds the bwrap command WITHOUT starting it, so the caller
// may wire stdout/stderr first. This is the ONLY launch path; tests use it
// unmodified (codex Phase-0 #3).
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
		wd, err := filepath.EvalSymlinks(spec.WorkDir)
		if err != nil {
			return fail(fmt.Errorf("workdir: %w", err))
		}
		st, err := os.Stat(wd)
		if err != nil || !st.IsDir() {
			return fail(fmt.Errorf("workdir %s is not a directory", spec.WorkDir))
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
	cmd := exec.CommandContext(ctx, av.BwrapPath) // args attached below
	hashes := map[string]string{}
	fdNum := 3 // first ExtraFiles fd
	for _, cf := range files {
		cmd.ExtraFiles = append(cmd.ExtraFiles, cf.f)
		// --perms 0555: ro-bind-data files default to non-executable.
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
		args = append(args, "--seccomp", fmt.Sprint(fdNum))
		fdNum++
	}
	args = append(args, insideTarget)
	args = append(args, spec.Args...)
	cmd.Args = append([]string{av.BwrapPath}, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	h := &Handle{Cmd: cmd, cancel: cancel, ClosureHashes: hashes}
	for _, cf := range files {
		h.closers = append(h.closers, cf.f)
	}
	return h, nil
}

// Start begins execution of a prepared handle.
func (h *Handle) Start() error {
	return h.Cmd.Start()
}

// Run prepares, starts and waits, returning combined output.
func Run(av Availability, spec Spec) (string, error) {
	h, err := Prepare(av, spec)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	h.Cmd.Stdout = &out
	h.Cmd.Stderr = &out
	if err := h.Start(); err != nil {
		h.Wait()
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

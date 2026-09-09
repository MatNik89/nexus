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
	"sort"
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

// PtraceDeniedSentinel is the EXACT line the floor-probe target must emit
// after its ptrace syscall returns EPERM. A unique sentinel — never a
// substring like "denied" — because ordinary bwrap setup diagnostics contain
// "Permission denied" and must count as floor FAILURE, not success
// (Phase-0 r3 codex #1: a fake prelaunch failure passed the old check).
const PtraceDeniedSentinel = "NEXUS_PTRACE_DENIED:EPERM"

// FloorProbe proves the WHOLE production launch path works on this host —
// closure resolution, sealed memfds, bwrap namespaces AND the seccomp
// syscall floor — by running a harmless static ELF that attempts
// PTRACE_TRACEME and must be DENIED. probeTarget must emit
// PtraceDeniedSentinel on its own line ONLY after observing EPERM from the
// syscall (nexus: `__probe-ptrace`; tests: probehelper `syscall-ptrace`).
func FloorProbe(av Availability, probeTarget string, args []string) error {
	out, err := Run(av, Spec{Target: probeTarget, Args: args, Timeout: 15 * time.Second})
	if err == nil {
		return fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: syscall floor INACTIVE (ptrace succeeded inside sandbox)")
	}
	sentinelSeen := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == PtraceDeniedSentinel {
			sentinelSeen = true
		}
	}
	if !sentinelSeen {
		return fmt.Errorf("SANDBOX_CAPABILITY_UNAVAILABLE: confined launch failed before the probe ran: %v: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// Spec describes one sandboxed launch of a promoted ELF target.
// It cannot express a weakened boundary: negative-control switches live in
// an unexported loosen struct reachable only from this package's tests.
// ExtraROBinds/ExtraEnv ADD narrowly-scoped read-only visibility (never
// remove an existing protection — network/pid/seccomp/remount-ro are
// unaffected) for a governed toolchain child-process closure
// (PLAN-CODING-TRIO.md Slice 0: `go`/`gopls` must locate and exec their
// own compiler/linker/asm/cgo children, which this sandbox otherwise
// makes entirely invisible).
type Spec struct {
	Target  string // absolute HOST path of the promoted executable (ELF only)
	Args    []string
	WorkDir string // host dir bound read-write at /work (guarded; see Prepare)
	Timeout time.Duration

	// ExtraROBinds are additional host directories bound READ-ONLY at the
	// SAME absolute path inside the sandbox — verbatim, so a toolchain's
	// own baked-in absolute paths keep resolving. Each is guarded by
	// guardROBind (owned by the current user or root, never group/world
	// writable, symlink-resolved) before Prepare ever touches bwrap argv.
	// Unlike the memfd-pinned shared-library closure, these are NOT
	// individually content-hashed — a toolchain install is large and
	// operator-controlled, not per-request content; only its identity
	// (the resolved canonical path, AND — when ExtraROBindIdentities pins
	// it — the resolved directory's device+inode) is verified, not its
	// bytes.
	ExtraROBinds []string
	// ExtraROBindIdentities, keyed by an ExtraROBinds entry's CANONICAL
	// (EvalSymlinks-resolved) path, pins that path's device+inode as
	// observed at an earlier point (sandbox.Compile) — Prepare, running
	// at Launch, refuses the bind if the SAME path now resolves to a
	// DIFFERENT directory (code-review CODE2 codex finding #2,
	// reproduced: a directory renamed away and replaced at the same path
	// between Compile and Launch served different bytes under an
	// unchanged policy). Optional: a nil map (or a path absent from it)
	// performs no identity check, so a caller that never runs through
	// sandbox.Compile (e.g. Prepare called directly in tests) is
	// unaffected. This is a cheap, bounded SWAP detector — it does not,
	// and cannot without unbounded cost, detect an in-place edit of files
	// WITHIN an unchanged directory; that remains the toolchain-pinning
	// caller's own responsibility (docs/PLAN-CODING-TRIO.md Slice 0).
	ExtraROBindIdentities map[string]ROBindIdentity
	// ExtraEnv sets additional environment variables beyond the fixed
	// PATH=/nowhere + LD_LIBRARY_PATH baseline (still --clearenv first —
	// this ADDS named variables, it does not restore the ambient
	// environment). Keys must be non-empty and contain no '=' or NUL.
	ExtraEnv map[string]string

	loosen loosen
}

type loosen struct {
	roBind    string
	net       bool
	pidNS     bool
	seccomp   bool
	remountRW bool // skip --remount-ro / (shadowing negative control only)
}

// ErrNotELF: the launch target is not a native ELF executable (shebang,
// script, foreign architecture). Refused BEFORE any sandbox is set up.
var ErrNotELF = fmt.Errorf("launch target is not a native ELF executable (scripts/shebang/foreign-arch rejected)")

// checkQueueBudget rejects a dependency-table expansion BEFORE any node
// slice is materialized (r5 codex #3).
func checkQueueBudget(cur, add int) error {
	if add > maxDepQueue || cur+add > maxDepQueue {
		return fmt.Errorf("dependency graph exceeds %d pending nodes", maxDepQueue)
	}
	return nil
}

const (
	maxClosureFiles   = 64
	maxClosureBytes   = 256 << 20
	maxClosureOneFile = 64 << 20
	maxInterpLen      = 4096
	maxDepQueue       = 256
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

// loadBounded opens path ONCE, fstats the open descriptor, and reads at
// most maxClosureOneFile+1 bytes from that same descriptor — validation and
// allocation share one file, so a swap or grow between stat and read cannot
// bypass the budget (Phase-0 r4 codex #2).
func loadBounded(path string, remaining int64) ([]byte, error) {
	cap := int64(maxClosureOneFile)
	if remaining < cap {
		cap = remaining // aggregate allowance enforced BEFORE this allocation
	}
	if cap <= 0 {
		return nil, fmt.Errorf("%s: closure byte budget exhausted", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file (%v)", path, err)
	}
	if st.Size() > cap {
		return nil, fmt.Errorf("%s exceeds the closure byte budget", path)
	}
	buf, err := io.ReadAll(io.LimitReader(f, cap+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > cap {
		return nil, fmt.Errorf("%s grew past the closure byte budget during read", path)
	}
	return buf, nil
}

// elfMeta describes one ELF, parsed FROM THE EXACT BYTES that get pinned —
// the metadata can never describe different content than the sandbox runs.
type elfMeta struct {
	machine   elf.Machine
	class     elf.Class
	interp    string
	needed    []string
	paths     []string // expanded DT_RUNPATH (preferred) or DT_RPATH
	isRunpath bool     // true when paths came from DT_RUNPATH
}

func parseELFMeta(buf []byte, hostDir string) (elfMeta, error) {
	ef, err := elf.NewFile(bytes.NewReader(buf))
	if err != nil {
		return elfMeta{}, ErrNotELF
	}
	defer ef.Close()
	m := elfMeta{machine: ef.Machine, class: ef.Class}
	for _, prg := range ef.Progs {
		if prg.Type == elf.PT_INTERP {
			if prg.Filesz > maxInterpLen {
				return elfMeta{}, fmt.Errorf("PT_INTERP length %d exceeds cap", prg.Filesz)
			}
			b := make([]byte, prg.Filesz)
			if _, err := prg.ReadAt(b, 0); err != nil && err != io.EOF {
				return elfMeta{}, fmt.Errorf("reading PT_INTERP: %w", err)
			}
			m.interp = strings.TrimRight(string(b), "\x00")
		}
	}
	m.needed, err = ef.ImportedLibraries()
	if err != nil {
		return elfMeta{}, err
	}
	raw, rerr := ef.DynString(elf.DT_RUNPATH)
	if rerr == nil && len(raw) > 0 {
		m.isRunpath = true
	} else {
		raw, _ = ef.DynString(elf.DT_RPATH)
	}
	m.paths = expandRunpaths(hostDir, raw)
	return m, nil
}

// expandRunpaths applies the dynamic-loader token rules the P0 contract
// needs: $ORIGIN/${ORIGIN} expands to the REFERRING object's directory;
// empty components, unexpanded-token components AND relative components are
// DROPPED (never resolved against the process cwd — r4 kilo #6).
func expandRunpaths(referrerDir string, raw []string) []string {
	var out []string
	for _, r := range raw {
		for _, comp := range strings.Split(r, ":") {
			if comp == "" {
				continue
			}
			comp = strings.ReplaceAll(comp, "${ORIGIN}", referrerDir)
			comp = strings.ReplaceAll(comp, "$ORIGIN", referrerDir)
			if strings.Contains(comp, "$") { // $LIB/$PLATFORM etc.: drop, fail-closed
				continue
			}
			comp = filepath.Clean(comp)
			if !filepath.IsAbs(comp) {
				continue
			}
			out = append(out, comp)
		}
	}
	return out
}

// depSearchDirs: per-referrer order = the referrer's own paths, then the
// inherited RPATH chain (old-dtags semantics: DT_RPATH applies through the
// tree unless the object has DT_RUNPATH — r4 codex #1), then the trusted
// system directories.
func depSearchDirs(direct, inherited []string) []string {
	dirs := append(append([]string{}, direct...), inherited...)
	for _, d := range []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64"} {
		dirs = append(dirs, d)
		if matches, _ := filepath.Glob(d + "/*-linux-gnu*"); matches != nil {
			dirs = append(dirs, matches...)
		}
	}
	return dirs
}

// insideLibDir is where every pinned library lands (single RO directory;
// the interpreter alone keeps its PT_INTERP path, which the kernel needs
// verbatim). LD_LIBRARY_PATH points ONLY here.
const insideLibDir = "/nexus-libs"

// resolveClosure loads target + interpreter + transitive DT_NEEDED
// libraries; each file is read once (bounded), hashed, sealed into a memfd,
// and its metadata parsed from those same bytes.
func resolveClosure(target string) (files []closureFile, insideTarget string, err error) {
	total := 0
	closeAll := func() {
		for _, cf := range files {
			cf.f.Close()
		}
	}
	seen := map[string]bool{}
	native, err := nativeMachine()
	if err != nil {
		return nil, "", err
	}
	// addBuf pins already-loaded bytes under dest.
	addBuf := func(buf []byte, dest string) error {
		if seen[dest] {
			return nil
		}
		seen[dest] = true
		if len(files) >= maxClosureFiles {
			return fmt.Errorf("closure exceeds %d files", maxClosureFiles)
		}
		total += len(buf)
		if total > maxClosureBytes {
			return fmt.Errorf("closure exceeds %d bytes", maxClosureBytes)
		}
		sum := sha256.Sum256(buf)
		mf, err := memfdWithContent(filepath.Base(dest), buf)
		if err != nil {
			return fmt.Errorf("pinning %s: %w", dest, err)
		}
		files = append(files, closureFile{f: mf, dest: dest, hash: hex.EncodeToString(sum[:])})
		return nil
	}

	remaining := func() int64 { return int64(maxClosureBytes - total) }
	targetBuf, err := loadBounded(target, remaining())
	if err != nil {
		return nil, "", err
	}
	rootMeta, err := parseELFMeta(targetBuf, filepath.Dir(target))
	if err != nil {
		return nil, "", err
	}
	if rootMeta.machine != native || rootMeta.class != elf.ELFCLASS64 {
		return nil, "", ErrNotELF
	}
	insideTarget = "/nexus-target"
	if err := addBuf(targetBuf, insideTarget); err != nil {
		closeAll()
		return nil, "", err
	}
	if rootMeta.interp != "" {
		ibuf, err := loadBounded(rootMeta.interp, remaining())
		if err != nil {
			closeAll()
			return nil, "", err
		}
		if err := addBuf(ibuf, rootMeta.interp); err != nil {
			closeAll()
			return nil, "", err
		}
	}

	type depNode struct {
		name      string
		direct    []string // referrer's own RUNPATH-or-RPATH
		inherited []string // ancestor RPATH chain (old-dtags)
	}
	var rootInherited []string
	if !rootMeta.isRunpath {
		rootInherited = rootMeta.paths
	}
	var queue []depNode
	push := func(nodes []depNode) error {
		if err := checkQueueBudget(len(queue), len(nodes)); err != nil {
			return err
		}
		queue = append(queue, nodes...)
		return nil
	}
	if err := checkQueueBudget(len(queue), len(rootMeta.needed)); err != nil {
		closeAll()
		return nil, "", err
	}
	var rootNodes []depNode
	for _, n := range rootMeta.needed {
		rootNodes = append(rootNodes, depNode{name: n, direct: rootMeta.paths, inherited: rootInherited})
	}
	if err := push(rootNodes); err != nil {
		closeAll()
		return nil, "", err
	}
	resolvedNames := map[string]bool{}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if resolvedNames[node.name] {
			continue
		}
		resolvedNames[node.name] = true
		found := ""
		for _, d := range depSearchDirs(node.direct, node.inherited) {
			cand := filepath.Join(d, node.name)
			if st, err := os.Stat(cand); err == nil && st.Mode().IsRegular() {
				found = cand
				break
			}
		}
		if found == "" {
			closeAll()
			return nil, "", fmt.Errorf("cannot resolve library %q for %s", node.name, target)
		}
		lbuf, err := loadBounded(found, remaining())
		if err != nil {
			closeAll()
			return nil, "", err
		}
		lmeta, err := parseELFMeta(lbuf, filepath.Dir(found))
		if err != nil {
			closeAll()
			return nil, "", fmt.Errorf("parsing %s: %w", found, err)
		}
		if err := addBuf(lbuf, filepath.Join(insideLibDir, node.name)); err != nil {
			closeAll()
			return nil, "", err
		}
		childInherited := node.inherited
		if !lmeta.isRunpath && len(lmeta.paths) > 0 {
			childInherited = append(append([]string{}, node.inherited...), lmeta.paths...)
		}
		if err := checkQueueBudget(len(queue), len(lmeta.needed)); err != nil {
			closeAll()
			return nil, "", err
		}
		var childNodes []depNode
		for _, n := range lmeta.needed {
			childNodes = append(childNodes, depNode{name: n, direct: lmeta.paths, inherited: childInherited})
		}
		if err := push(childNodes); err != nil {
			closeAll()
			return nil, "", err
		}
	}
	return files, insideTarget, nil
}

// allowedWorkRoots are the only trees a disposable RW grant may live under
// (Phase-0 r3 codex #2: two-component depth still admitted /home/<user>
// wholesale; a disposable dir belongs in a temp tree, full stop).
func allowedWorkRoots() []string {
	var roots []string
	consider := func(p string) {
		if p == "" {
			return
		}
		canon, err := filepath.EvalSymlinks(filepath.Clean(p))
		if err != nil {
			return
		}
		st, err := os.Stat(canon)
		if err != nil || !st.IsDir() {
			return
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			return
		}
		mode := st.Mode()
		switch {
		case int(sys.Uid) == os.Geteuid() && mode.Perm() == 0o700 &&
			strings.HasPrefix(canon, "/run/"):
			// Private per-user runtime root: ownership+0700 ALONE is not
			// enough (a 0700 $HOME satisfies the shape — proven by the
			// hostile-XDG RED), so the canonical path must also live under
			// /run/, where the kernel/init own the tree.
			roots = append(roots, canon)
		case sys.Uid == 0 && mode&os.ModeSticky != 0:
			// root-owned sticky world temp root (/tmp shape)
			roots = append(roots, canon)
		}
		// Anything else (a plain home dir, a hostile TMPDIR/XDG value) is
		// silently ignored — ambient environment cannot widen the allowlist
		// (Phase-0 r4 codex #3).
	}
	consider(os.TempDir())
	consider(os.Getenv("XDG_RUNTIME_DIR"))
	return roots
}

// guardWorkDir canonicalizes and rejects hazardous RW grants: anything
// outside the allowed disposable roots, foreign ownership, or group/world
// writability (0o022 — a shared-group dir is not single-user controlled).
// topknot ceiling: bwrap still binds by PATH, so a same-user swap between
// this check and namespace setup remains possible; the T25 backend closes it
// with a policy-owned disposable-directory capability. Upgrade trigger: T25.
func guardWorkDir(dir string) (string, error) {
	wd, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}
	inRoot := false
	for _, root := range allowedWorkRoots() {
		if wd != root && strings.HasPrefix(wd, root+string(filepath.Separator)) {
			inRoot = true
			break
		}
	}
	if !inRoot {
		return "", fmt.Errorf("workdir %q refused: disposable RW grants must live under %v", wd, allowedWorkRoots())
	}
	st, err := os.Stat(wd)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("workdir %s is not a directory", dir)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(sys.Uid) != os.Geteuid() {
		return "", fmt.Errorf("workdir %s not owned by the current user", wd)
	}
	if st.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("workdir %s is group/world-writable (%o)", wd, st.Mode().Perm())
	}
	return wd, nil
}

// guardROBind canonicalizes and rejects a hazardous read-only-visibility
// grant: a symlink component swap, foreign ownership (neither the current
// user nor root), or group/world write access (0o022 — writable by
// anyone but the owner is not a trusted, operator-controlled tree).
// Unlike guardWorkDir this is NOT restricted to /run or a root-sticky
// temp root — a read-only grant's risk is "whose bytes get exposed and
// executed," not "who can write into a shared RW area," so a normal
// user-owned install tree (e.g. $HOME/go/pkg/mod, holding the downloaded
// Go toolchain) is an acceptable shape here where it would NOT be for a
// disposable RW workdir.
// roBindDenyRoots are well-known sensitive system trees no caller may
// ever expose via ExtraROBinds, regardless of their ownership/permission
// bits (code-review CODE1 codex finding #1, reproduced live:
// ExtraROBinds: []string{"/etc"} made /etc/passwd readable — ownership
// and mode alone are not sufficient discrimination for "a trusted,
// operator-controlled toolchain install" vs. "the entire system"). This
// is defense-in-depth: the CALLER (the coding-runner, not yet built)
// still owns the responsibility of scoping ExtraROBinds to the actual
// Go toolchain paths it resolved via a trusted `go env` call — this
// denylist only guarantees probe itself never becomes the party that
// exposes core system directories, however a caller misuses it.
var roBindDenyRoots = []string{
	"/", "/etc", "/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32",
	"/boot", "/root", "/proc", "/sys", "/dev", "/run", "/var", "/opt",
}

// denylistedROBindRoot reports whether canon IS, or is nested under, one
// of roBindDenyRoots — a path-segment-aware check (never a bare string
// prefix, so "/etcetera" is not mistaken for "/etc").
func denylistedROBindRoot(canon string) bool {
	if canon == "/" {
		return true // ExtraROBinds can never name the filesystem root itself
	}
	for _, root := range roBindDenyRoots {
		if root == "/" {
			continue // handled above; canon is never "/" past this point
		}
		if canon == root || strings.HasPrefix(canon, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ROBindIdentity is an ExtraROBinds directory's device+inode, cheap to
// capture (one stat already performed by guardROBind) and cheap to
// compare — a bounded, generic defense against a directory SWAPPED at
// the same path, distinct from (and much cheaper than) hashing the
// directory's entire contents. See ExtraROBindIdentities' doc comment.
type ROBindIdentity struct {
	Dev, Ino uint64
}

// PinROBindIdentity resolves and validates dir exactly as guardROBind
// does (same canonicalization, denylist, ownership, and permission
// checks — a Compile-time caller must be refused for any reason a later
// Launch-time Prepare call would refuse it), and additionally returns
// its ROBindIdentity for pinning into a policy compiled now and launched
// later.
func PinROBindIdentity(dir string) (canon string, identity ROBindIdentity, err error) {
	canon, identity, err = guardROBind(dir)
	return canon, identity, err
}

func guardROBind(dir string) (string, ROBindIdentity, error) {
	canon, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: %w", dir, err)
	}
	if !filepath.IsAbs(canon) {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: not an absolute path (fail closed)", dir)
	}
	if denylistedROBindRoot(canon) {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: a well-known system directory may never be exposed (fail closed)", canon)
	}
	st, err := os.Stat(canon)
	if err != nil {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: %w", canon, err)
	}
	if !st.IsDir() {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: not a directory (fail closed)", canon)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: cannot determine ownership (fail closed)", canon)
	}
	if int(sys.Uid) != os.Geteuid() && sys.Uid != 0 {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: owned by neither the current user nor root (fail closed)", canon)
	}
	if st.Mode().Perm()&0o022 != 0 {
		return "", ROBindIdentity{}, fmt.Errorf("ro-bind %s: group/world-writable (%o) — not a trusted read-only tree (fail closed)", canon, st.Mode().Perm())
	}
	return canon, ROBindIdentity{Dev: uint64(sys.Dev), Ino: sys.Ino}, nil
}

// validEnvKey rejects an environment key that could confuse the child
// process or an argv-adjacent parser: empty, containing '=' (the KEY=VALUE
// separator itself), or a NUL byte.
// reservedEnvKeys are baseline-controlled variables ExtraEnv may never
// set (code-review CODE1 agy finding #2): bwrap applies --setenv
// sequentially, and a LATER ExtraEnv entry would silently OVERRIDE the
// earlier fixed LD_LIBRARY_PATH=/nexus-libs baseline (probe.go's own
// memfd-pinned-closure loader path), unseating the closure-pinning
// guarantee — a caller-controlled LD_LIBRARY_PATH or LD_PRELOAD could
// redirect the dynamic loader to unpinned shared objects. PATH is
// reserved too: it is deliberately fixed to /nowhere (every exec in this
// sandbox model uses an absolute path) and ExtraEnv exists to ADD
// capability, never to override a security-relevant baseline variable.
// LD_AUDIT is reserved for the same reason (code-review CODE2, codex +
// kilo independently flagged it): it makes the dynamic loader invoke an
// attacker-named audit library's callbacks during symbol resolution — a
// second loader-hijack vector alongside LD_PRELOAD.
var reservedEnvKeys = map[string]bool{
	"LD_LIBRARY_PATH": true,
	"LD_PRELOAD":      true,
	"LD_AUDIT":        true,
	"PATH":            true,
}

func validEnvKey(k string) bool {
	return k != "" && !strings.ContainsAny(k, "=\x00") && !reservedEnvKeys[k]
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
	closed    bool
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

// Start begins execution. A handle starts at most once; a repeat Start or a
// Start after Close is rejected WITHOUT touching the underlying command, so
// a live first launch is never orphaned (Phase-0 r3 codex #5).
func (h *Handle) Start() error {
	if h.started || h.closed {
		return fmt.Errorf("invalid handle state: already started or closed")
	}
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
		h.closed = true
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
		// The inside loader replays its OWN search ($ORIGIN points at
		// /nexus-target, not the host location), so every pinned library
		// lands in the single RO dir /nexus-libs and LD_LIBRARY_PATH points
		// only there. The root filesystem is remounted read-only after all
		// mounts (below), so the target cannot shadow a library by writing
		// into a loader-searched directory (r4 kilo #7).
		"--setenv", "LD_LIBRARY_PATH", insideLibDir,
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
	if len(spec.ExtraROBinds) > 0 {
		// Deterministic order: two Specs differing only in ExtraROBinds
		// ordering must compile to the SAME bwrap argv (and therefore the
		// same sandbox.Compile policyHash) — sort before guarding.
		binds := append([]string(nil), spec.ExtraROBinds...)
		sort.Strings(binds)
		seen := map[string]bool{}
		for _, b := range binds {
			canon, identity, err := guardROBind(b)
			if err != nil {
				return fail(err)
			}
			if seen[canon] {
				continue // de-duplicate after canonicalization
			}
			seen[canon] = true
			// A non-nil ExtraROBindIdentities means this Spec came through
			// sandbox.Compile, which pins EVERY declared ExtraROBinds entry
			// (code-review CODE3 codex finding: a symlink-retargeting
			// bypass). A caller's original bind string can be a symlink
			// (e.g. a toolchain alias); Compile pins the identity under
			// the canonical path IT resolved. If the symlink is retargeted
			// between Compile and Launch, guardROBind resolves a
			// DIFFERENT canonical path here — a plain "ok && mismatch"
			// check silently SKIPS the comparison on a map miss, fail
			// OPEN. A missing entry when the map is non-nil is therefore
			// refused outright, not treated as "no check requested" —
			// that meaning is reserved for a nil map (a caller that never
			// went through Compile at all, e.g. probe.Prepare called
			// directly in tests).
			if spec.ExtraROBindIdentities != nil {
				pinned, ok := spec.ExtraROBindIdentities[canon]
				if !ok {
					return fail(fmt.Errorf("ro-bind %s: no identity pin found for this resolved path — possible symlink-retarget attack since compile (fail closed)", canon))
				}
				if pinned != identity {
					return fail(fmt.Errorf("ro-bind %s: directory identity changed since it was compiled (dev/ino %v now, %v pinned) — possible swap attack (fail closed)", canon, identity, pinned))
				}
			}
			args = append(args, "--ro-bind", canon, canon)
		}
	}
	if len(spec.ExtraEnv) > 0 {
		keys := make([]string, 0, len(spec.ExtraEnv))
		for k := range spec.ExtraEnv {
			if !validEnvKey(k) {
				return fail(fmt.Errorf("extra env key %q is invalid (fail closed)", k))
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "--setenv", k, spec.ExtraEnv[k])
		}
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
	if !spec.loosen.remountRW {
		args = append(args, "--remount-ro", "/")
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

// ResolveClosureHashes resolves the FULL runtime closure of target
// (target ELF + loader + every library) and returns the inside-path →
// sha256 map WITHOUT launching anything (T25 Compile pins the whole
// closure — Phase-6-r2 codex #2: pinning only the main ELF left
// dependency substitution open between Compile and Launch).
func ResolveClosureHashes(target string) (map[string]string, error) {
	files, _, err := resolveClosure(target)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(files))
	for _, cf := range files {
		out[cf.dest] = cf.hash
		cf.f.Close()
	}
	return out, nil
}

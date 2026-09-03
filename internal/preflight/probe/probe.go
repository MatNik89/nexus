// Package probe is the standalone sandbox feasibility probe (tasks-P0 T02).
// It has no kernel dependencies. It drives bwrap (the P0 ENFORCED backend,
// HARDQ D1) across the capability columns FS_RO / FS_RW / NET_DENY /
// PROC_TREE and enforces the promoted-ELF launch rule (HARDQ B4 seed: shebang
// and non-ELF targets are rejected by the launcher, before bwrap runs).
package probe

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Availability reports whether the bwrap backend can be used at all.
// Absent backend => the caller must treat exec capability as OFF
// (fail-closed); there is no weaker fallback.
type Availability struct {
	BwrapPath    string
	BwrapVersion string
}

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
	return Availability{BwrapPath: path, BwrapVersion: string(bytes.TrimSpace(out))}, nil
}

// Spec describes one sandboxed launch. Loosen* fields exist ONLY for the
// suite's hazard-safe negative controls; production callers must leave them
// zero-valued.
type Spec struct {
	Target  string   // absolute path of the promoted executable (must be ELF)
	Args    []string
	WorkDir string   // host dir bound RW at /work
	Timeout time.Duration

	// Hazard-safe negative-control switches (test-owned resources only).
	LoosenROBind  string // extra host dir bound read-only (canary dir)
	LoosenNet     bool   // do NOT unshare the network namespace
	LoosenPIDNS   bool   // do NOT unshare pid / die-with-parent
}

// ErrNotELF is returned when the launch target is not a plain ELF executable
// (e.g. a shebang script). The launcher refuses BEFORE any sandbox is set up.
var ErrNotELF = errors.New("launch target is not an ELF executable (scripts/shebang rejected)")

func isELF(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false, err
	}
	return magic == [4]byte{0x7f, 'E', 'L', 'F'}, nil
}

// Command builds the bwrap invocation for spec. The read/execute closure is
// the B4 minimum: /usr (+ /bin,/lib* symlinks), /etc/ld.so.cache and CA certs
// as FILE grants — never recursive /etc. Anything else on the host is not
// visible inside the sandbox (undeclared child-exec fails with ENOENT).
func Command(av Availability, spec Spec) (*exec.Cmd, error) {
	elf, err := isELF(spec.Target)
	if err != nil {
		return nil, fmt.Errorf("launch target unreadable: %w", err)
	}
	if !elf {
		return nil, ErrNotELF
	}
	args := []string{
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/sbin", "/sbin",
		"--symlink", "usr/lib", "/lib",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--dir", "/work",
		"--clearenv", "--setenv", "PATH", "/usr/bin:/bin",
		"--new-session",
	}
	if _, err := os.Stat("/usr/lib64"); err == nil {
		args = append(args, "--symlink", "usr/lib64", "/lib64")
	}
	for _, f := range []string{"/etc/ld.so.cache", "/etc/ssl/certs/ca-certificates.crt", "/etc/resolv.conf"} {
		if _, err := os.Stat(f); err == nil {
			args = append(args, "--ro-bind", f, f)
		}
	}
	if spec.WorkDir != "" {
		args = append(args, "--bind", spec.WorkDir, "/work")
	}
	if spec.LoosenROBind != "" {
		args = append(args, "--ro-bind", spec.LoosenROBind, spec.LoosenROBind)
	}
	if !spec.LoosenNet {
		args = append(args, "--unshare-net")
	}
	if !spec.LoosenPIDNS {
		args = append(args, "--unshare-pid", "--die-with-parent")
	}
	args = append(args, spec.Target)
	args = append(args, spec.Args...)
	cmd := exec.Command(av.BwrapPath, args...)
	return cmd, nil
}

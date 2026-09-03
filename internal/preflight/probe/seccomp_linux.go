//go:build linux

package probe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
)

// Syscall-floor v0 (T02 SYSCALL column): a classic-BPF seccomp program,
// hand-assembled in pure Go (no cgo, no external dependency), passed to
// bwrap via --seccomp <fd>. Policy: allow by default, deny a blocklist of
// introspection/persistence syscalls with EPERM. The full per-arch
// allowlist profile is the P1 native-helper's job (DESIGN-S0); this floor
// only has to be REAL and hostile-testable in P0.

const (
	seccompRetAllow = 0x7fff0000
	seccompRetErrno = 0x00050000
	epermErrno      = 1 // EPERM

	bpfLD  = 0x00
	bpfW   = 0x00
	bpfABS = 0x20
	bpfJMP = 0x05
	bpfJEQ = 0x10
	bpfK   = 0x00
	bpfRET = 0x06

	// offsetof(struct seccomp_data, nr) == 0, arch == 4
	offNR   = 0
	offArch = 4

	auditArchAARCH64 = 0xC00000B7
	auditArchX86_64  = 0xC000003E
)

type sockFilter struct {
	Code uint16
	JT   uint8
	JF   uint8
	K    uint32
}

// deniedSyscalls per architecture: ptrace, process_vm_readv,
// process_vm_writev, userfaultfd, perf_event_open, open_by_handle_at,
// add_key, keyctl, request_key, kexec_load (where present).
func deniedSyscalls() (arch uint32, nrs []uint32, err error) {
	switch runtime.GOARCH {
	case "arm64":
		return auditArchAARCH64, []uint32{
			117, // ptrace
			270, // process_vm_readv
			271, // process_vm_writev
			282, // userfaultfd
			241, // perf_event_open
			265, // open_by_handle_at
			217, // add_key
			219, // keyctl
			218, // request_key
			104, // kexec_load
		}, nil
	case "amd64":
		return auditArchX86_64, []uint32{
			101, // ptrace
			310, // process_vm_readv
			311, // process_vm_writev
			323, // userfaultfd
			298, // perf_event_open
			304, // open_by_handle_at
			248, // add_key
			250, // keyctl
			249, // request_key
			246, // kexec_load
		}, nil
	default:
		return 0, nil, fmt.Errorf("no syscall-floor table for GOARCH %s (fail closed)", runtime.GOARCH)
	}
}

// seccompFilter builds the cBPF program and returns it as a read fd suitable
// for bwrap --seccomp.
func seccompFilter() (*os.File, error) {
	arch, nrs, err := deniedSyscalls()
	if err != nil {
		return nil, err
	}
	var prog []sockFilter
	// [0-1] load arch; an UNEXPECTED audit architecture is DENIED, not
	// allowed (Phase-0 r2 codex #7: allow-on-foreign-arch let a compat
	// 32-bit ELF bypass the whole table). JF jumps to the trailing deny.
	prog = append(prog, sockFilter{Code: bpfLD | bpfW | bpfABS, K: offArch})
	archJmpIdx := len(prog)
	prog = append(prog, sockFilter{Code: bpfJMP | bpfJEQ | bpfK, JT: 0, K: arch})
	// [2] load nr; then one JEQ per denied nr, all jumping to the trailing
	// deny instruction (targets fixed up below by recorded index).
	prog = append(prog, sockFilter{Code: bpfLD | bpfW | bpfABS, K: offNR})
	var jeqIdx []int
	for _, nr := range nrs {
		jeqIdx = append(jeqIdx, len(prog))
		prog = append(prog, sockFilter{Code: bpfJMP | bpfJEQ | bpfK, K: nr})
	}
	prog = append(prog, sockFilter{Code: bpfRET | bpfK, K: seccompRetAllow})
	denyIdx := len(prog)
	prog = append(prog, sockFilter{Code: bpfRET | bpfK, K: seccompRetErrno | epermErrno})
	for _, i := range jeqIdx {
		prog[i].JT = uint8(denyIdx - i - 1)
	}
	prog[archJmpIdx].JF = uint8(denyIdx - archJmpIdx - 1)

	var buf bytes.Buffer
	for _, ins := range prog {
		if err := binary.Write(&buf, binary.LittleEndian, ins); err != nil {
			return nil, err
		}
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	w.Close()
	return r, nil
}

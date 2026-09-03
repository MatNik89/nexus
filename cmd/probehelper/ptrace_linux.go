//go:build linux

package main

import "syscall"

// ptraceTraceme exercises the syscall-floor boundary from inside the
// sandbox: under the P0 seccomp filter it must fail with EPERM, in which
// case it reports the exact floor-probe sentinel (never a generic "denied"
// substring, which bwrap's own diagnostics could fake).
func ptraceTraceme() (sentinel string, err error) {
	_, _, errno := syscall.RawSyscall(syscall.SYS_PTRACE, 0 /*PTRACE_TRACEME*/, 0, 0)
	if errno == syscall.EPERM {
		return "NEXUS_PTRACE_DENIED:EPERM", errno
	}
	if errno != 0 {
		return "", errno
	}
	return "", nil
}

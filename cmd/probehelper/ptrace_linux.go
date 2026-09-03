//go:build linux

package main

import "syscall"

// ptraceTraceme exercises the syscall-floor boundary from inside the
// sandbox: under the P0 seccomp filter it must fail with EPERM.
func ptraceTraceme() error {
	_, _, errno := syscall.RawSyscall(syscall.SYS_PTRACE, 0 /*PTRACE_TRACEME*/, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

// runProbePtrace backs the hidden `__probe-ptrace` subcommand used by the
// doctor's kernel-floor probe: executed INSIDE the sandbox, it attempts
// PTRACE_TRACEME. Under the P0 syscall floor it must be denied (exit 1,
// "denied" on stderr); success (exit 0) means the floor is NOT active.
func runProbePtrace() int {
	_, _, errno := syscall.RawSyscall(syscall.SYS_PTRACE, 0 /*PTRACE_TRACEME*/, 0, 0)
	if errno == syscall.EPERM {
		// Exact sentinel, printed ONLY on an observed EPERM from the syscall
		// itself — bwrap setup diagnostics must never match it.
		fmt.Fprintln(os.Stderr, "NEXUS_PTRACE_DENIED:EPERM")
		return 1
	}
	if errno != 0 {
		fmt.Fprintf(os.Stderr, "ptrace failed differently: %v\n", errno)
		return 1
	}
	fmt.Println("ptrace ok")
	return 0
}

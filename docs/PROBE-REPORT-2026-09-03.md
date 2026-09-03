# PROBE-REPORT — hostile conformance suite v2 (T02, post Phase-0 review r2)

Date: 2026-09-03T13:46:24+02:00
Tested revision: b8e4a7402c95964606cbc143a9b3d0bf2da80a6a
Non-report diff vs tested revision at generation time:
  (empty list above = code byte-identical to the tested revision; only this report file differs)
Host: Linux 6.12.34+rpt-rpi-2712 aarch64 GNU/Linux
bwrap: bubblewrap 0.11.2
Go: go1.26.4 linux/arm64

## Published P0 kernel/ABI floor (HARDQ C5)
- Linux kernel with unprivileged user namespaces FUNCTIONAL — proven by FloorProbe running
  the production Prepare/Start path (sealed-memfd closure + seccomp) with a required
  in-sandbox ptrace DENIAL, not by reading a sysctl
- bubblewrap >= 0.8.0 on PATH (parsed + enforced by Detect)
- native seccomp table for GOARCH arm64/amd64; any other audit arch is DENIED in-filter;
  foreign/compat ELF targets rejected before launch

## Suite: five capability columns, each with a hazard-safe negative control
FS_RO (shadow literal + canary pair) / FS_RW / NET_DENY (loopback sink) /
SYSCALL (ptrace EPERM + no-filter control) / PROC_TREE ((pid,starttime) oracle + PIDNS control)
Pinning: SEALED memfd copies at Prepare — path swap, inode truncation and descriptor
writes are all inert (each has a RED).

## Uncached run at the tested revision:
--- PASS: TestDetectFailClosedWhenBwrapAbsent (0.00s)
--- PASS: TestDetectRejectsBelowFloorVersion (0.00s)
--- PASS: TestFloorProbeFunctionalOnThisHost (0.87s)
--- PASS: TestShadowNotReadableInsideSandbox (0.76s)
--- PASS: TestCanaryOutsideClosureNotReadable (0.60s)
--- PASS: TestNegativeControlCanaryReadableWhenROLoosened (0.66s)
--- PASS: TestWorkdirWritable (0.70s)
--- PASS: TestNetworkDeniedInsideSandbox (0.86s)
--- PASS: TestNegativeControlDialSucceedsWhenNetLoosened (0.75s)
--- PASS: TestSyscallFloorDeniesPtrace (0.72s)
--- PASS: TestNegativeControlPtraceAllowedWhenSeccompLoosened (0.62s)
--- PASS: TestDynamicELFRuns (0.03s)
--- PASS: TestShebangRejectedBeforeSandbox (0.00s)
--- PASS: TestUndeclaredChildExecFailsForUsrBinary (0.68s)
--- PASS: TestTargetSwapAfterPrepareIsInert (0.63s)
--- PASS: TestKillReapsWholeTree (2.66s)
--- PASS: TestNegativeControlChildSurvivesWhenPIDNSLoosened (2.68s)
--- PASS: TestHangingTargetCleanedUpWithinTimeout (4.20s)
--- PASS: TestMemfdSealedAgainstPostPrepareWrite (0.58s)
--- PASS: TestNoFDLeakAcrossRuns (0.75s)
--- PASS: TestWorkdirRootRefused (0.66s)
PASS
ok  	github.com/MatNik89/nexus/internal/preflight/probe	19.433s

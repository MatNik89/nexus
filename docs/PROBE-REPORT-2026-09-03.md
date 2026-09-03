# PROBE-REPORT — hostile conformance suite v1 (T02, post Phase-0 review)

Date: 2026-09-03T13:18:37+02:00
Git: 6fc4b1ebc16fe33ffaa8b0e74ff65df1be972fb5 (dirty working tree at generation; committed with this revision)
Host: Linux 6.12.34+rpt-rpi-2712 aarch64 GNU/Linux
bwrap: bubblewrap 0.11.2
Go: go1.26.4 linux/arm64
Functional userns floor: proven by TestFloorProbeFunctionalOnThisHost (bwrap --unshare-user launch)

## Published P0 kernel/ABI floor (HARDQ C5)
- Linux kernel with unprivileged user namespaces functional (proven by launch, not sysctl read)
- bubblewrap >= 0.8.0 on PATH (version parsed and enforced by Detect)
- seccomp syscall-floor table available for GOARCH (arm64/amd64; others fail closed)

## Suite: all five capability columns + negative control per boundary
FS_RO (shadow + canary) / FS_RW / NET_DENY / SYSCALL (ptrace EPERM) / PROC_TREE
Content pinning: memfd copy at Prepare (path swap AND inode truncation inert)

## Uncached run:
--- PASS: TestDetectFailClosedWhenBwrapAbsent (0.00s)
--- PASS: TestDetectRejectsBelowFloorVersion (0.00s)
--- PASS: TestFloorProbeFunctionalOnThisHost (0.02s)
--- PASS: TestShadowNotReadableInsideSandbox (0.60s)
--- PASS: TestCanaryOutsideClosureNotReadable (0.69s)
--- PASS: TestNegativeControlCanaryReadableWhenROLoosened (0.66s)
--- PASS: TestWorkdirWritable (0.66s)
--- PASS: TestNetworkDeniedInsideSandbox (0.61s)
--- PASS: TestNegativeControlDialSucceedsWhenNetLoosened (0.64s)
--- PASS: TestSyscallFloorDeniesPtrace (0.71s)
--- PASS: TestNegativeControlPtraceAllowedWhenSeccompLoosened (0.62s)
--- PASS: TestDynamicELFRuns (0.09s)
--- PASS: TestShebangRejectedBeforeSandbox (0.00s)
--- PASS: TestUndeclaredChildExecFailsForUsrBinary (0.58s)
--- PASS: TestTargetSwapAfterPrepareIsInert (0.62s)
--- PASS: TestKillReapsWholeTree (2.53s)
--- PASS: TestNegativeControlChildSurvivesWhenPIDNSLoosened (2.58s)
--- PASS: TestHangingTargetCleanedUpWithinTimeout (1.09s)
PASS
ok  	github.com/MatNik89/nexus/internal/preflight/probe	12.697s

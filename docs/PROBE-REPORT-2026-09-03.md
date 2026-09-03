# PROBE-REPORT — hostile conformance suite v4 (T02, post Phase-0 review r4)

Date: 2026-09-03T14:13:20+02:00
Tested revision: 68fcb2ee27da24aa2cbe1504e5130878f928de02
Non-report diff vs tested revision at generation time:
  (empty list above = code byte-identical to the tested revision)
Host: Linux 6.12.34+rpt-rpi-2712 aarch64 GNU/Linux
bwrap: bubblewrap 0.11.2
Go: go1.26.4 linux/arm64

## Published P0 kernel/ABI floor (HARDQ C5)
- unprivileged user namespaces FUNCTIONAL via the production Prepare/Start path with the
  exact in-sandbox sentinel NEXUS_PTRACE_DENIED:EPERM
- bubblewrap >= 0.8.0 (parsed+enforced); native seccomp arm64/amd64, foreign audit arch
  DENIED in-filter, foreign/compat ELF rejected pre-launch

## Boundaries: FS_RO / FS_RW / NET_DENY / SYSCALL / PROC_TREE, each with a hazard-safe
negative control. Closure: sealed-memfd synthetic; libs in RO /nexus-libs; rootfs
remounted read-only (lib shadowing impossible). Workdir grants: owner-owned,
no group/world write, only under a root-owned sticky temp root or a /run/-rooted
0700 user runtime dir (ambient TMPDIR/XDG cannot widen the allowlist).

## Uncached run at the tested revision:
--- PASS: TestDetectFailClosedWhenBwrapAbsent (0.00s)
--- PASS: TestDetectRejectsBelowFloorVersion (0.00s)
--- PASS: TestFloorProbeFunctionalOnThisHost (0.67s)
--- PASS: TestShadowNotReadableInsideSandbox (0.57s)
--- PASS: TestCanaryOutsideClosureNotReadable (0.57s)
--- PASS: TestNegativeControlCanaryReadableWhenROLoosened (0.63s)
--- PASS: TestWorkdirWritable (0.59s)
--- PASS: TestNetworkDeniedInsideSandbox (0.62s)
--- PASS: TestNegativeControlDialSucceedsWhenNetLoosened (0.62s)
--- PASS: TestSyscallFloorDeniesPtrace (0.61s)
--- PASS: TestNegativeControlPtraceAllowedWhenSeccompLoosened (0.65s)
--- PASS: TestDynamicELFRuns (0.05s)
--- PASS: TestShebangRejectedBeforeSandbox (0.00s)
--- PASS: TestUndeclaredChildExecFailsForUsrBinary (0.53s)
--- PASS: TestTargetSwapAfterPrepareIsInert (0.62s)
--- PASS: TestKillReapsWholeTree (2.70s)
--- PASS: TestNegativeControlChildSurvivesWhenPIDNSLoosened (2.48s)
--- PASS: TestHangingTargetCleanedUpWithinTimeout (4.08s)
--- PASS: TestMemfdSealedAgainstPostPrepareWrite (0.58s)
--- PASS: TestNoFDLeakAcrossRuns (0.73s)
--- PASS: TestWorkdirRootRefused (0.58s)
--- PASS: TestFloorProbeRejectsPrelaunchPermissionDenied (0.61s)
--- PASS: TestWorkdirOutsideAllowedRootsRefused (0.67s)
--- PASS: TestWorkdirGroupWritableRefused (0.64s)
--- PASS: TestHandleRejectsRepeatStart (2.58s)
--- PASS: TestHandleRejectsStartAfterClose (0.45s)
--- PASS: TestExpandRunpathsSemantics (0.00s)
--- PASS: TestOriginRunpathBinaryResolves (0.17s)
--- PASS: TestRootfsReadOnlyPreventsLibShadowing (0.59s)
--- PASS: TestHostileXDGRuntimeDirCannotWidenRoots (0.57s)
--- PASS: TestTransitiveRpathBinaryResolves (0.27s)
PASS
ok  	github.com/MatNik89/nexus/internal/preflight/probe	24.439s

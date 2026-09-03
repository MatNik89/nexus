# PROBE-REPORT — hostile conformance suite v5 (T02, post Phase-0 review r5)

Date: 2026-09-03T15:20:33+02:00
Tested revision: 17888b3967983f71669adb8b1f97f5da800fd180
Non-report diff vs tested revision at generation time:
  (empty list above = code byte-identical to the tested revision)
Host: Linux 6.12.34+rpt-rpi-2712 aarch64 GNU/Linux
bwrap: bubblewrap 0.11.2
Go: go1.26.4 linux/arm64

## Published P0 kernel/ABI floor (HARDQ C5)
- unprivileged user namespaces FUNCTIONAL via production Prepare/Start with exact
  in-sandbox sentinel NEXUS_PTRACE_DENIED:EPERM
- bubblewrap >= 0.8.0; native seccomp arm64/amd64, foreign audit arch DENIED in-filter,
  foreign/compat ELF rejected pre-launch

## Boundaries: FS_RO / FS_RW / NET_DENY / SYSCALL / PROC_TREE + read-only rootfs
(EROFS-causal shadowing pair), each with a hazard-safe negative control. Closure:
sealed-memfd synthetic, budgets enforced before allocation. Workdir grants:
owner-owned, no group/world write, only under a root-owned sticky temp root or a
/run/-rooted 0700 user runtime dir.

## Uncached run at the tested revision:
--- PASS: TestDetectFailClosedWhenBwrapAbsent (0.00s)
--- PASS: TestDetectRejectsBelowFloorVersion (0.00s)
--- PASS: TestFloorProbeFunctionalOnThisHost (0.40s)
--- PASS: TestShadowNotReadableInsideSandbox (0.38s)
--- PASS: TestCanaryOutsideClosureNotReadable (0.36s)
--- PASS: TestNegativeControlCanaryReadableWhenROLoosened (0.36s)
--- PASS: TestWorkdirWritable (0.39s)
--- PASS: TestNetworkDeniedInsideSandbox (0.35s)
--- PASS: TestNegativeControlDialSucceedsWhenNetLoosened (0.34s)
--- PASS: TestSyscallFloorDeniesPtrace (0.40s)
--- PASS: TestNegativeControlPtraceAllowedWhenSeccompLoosened (0.34s)
--- PASS: TestDynamicELFRuns (0.02s)
--- PASS: TestShebangRejectedBeforeSandbox (0.00s)
--- PASS: TestUndeclaredChildExecFailsForUsrBinary (0.36s)
--- PASS: TestTargetSwapAfterPrepareIsInert (0.37s)
--- PASS: TestKillReapsWholeTree (2.37s)
--- PASS: TestNegativeControlChildSurvivesWhenPIDNSLoosened (2.44s)
--- PASS: TestHangingTargetCleanedUpWithinTimeout (3.89s)
--- PASS: TestMemfdSealedAgainstPostPrepareWrite (0.40s)
--- PASS: TestNoFDLeakAcrossRuns (0.53s)
--- PASS: TestWorkdirRootRefused (0.36s)
--- PASS: TestFloorProbeRejectsPrelaunchPermissionDenied (0.37s)
--- PASS: TestWorkdirOutsideAllowedRootsRefused (0.39s)
--- PASS: TestWorkdirGroupWritableRefused (0.37s)
--- PASS: TestHandleRejectsRepeatStart (2.46s)
--- PASS: TestHandleRejectsStartAfterClose (0.36s)
--- PASS: TestExpandRunpathsSemantics (0.00s)
--- PASS: TestOriginRunpathBinaryResolves (0.15s)
--- PASS: TestRootfsReadOnlyPreventsLibShadowing (0.13s)
--- PASS: TestNegativeControlLibWriteSucceedsWithoutRemountRO (0.10s)
--- PASS: TestQueueBudgetRejectsBeforeAllocation (0.00s)
--- PASS: TestLoadBoundedHonorsAggregateAllowance (0.00s)
--- PASS: TestHostileXDGRuntimeDirCannotWidenRoots (0.44s)
--- PASS: TestTransitiveRpathBinaryResolves (0.15s)
PASS
ok  	github.com/MatNik89/nexus/internal/preflight/probe	19.004s

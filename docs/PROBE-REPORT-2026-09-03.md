# PROBE-REPORT — hostile conformance suite v3 (T02, post Phase-0 review r3)

Date: 2026-09-03T14:00:23+02:00
Tested revision: a5aae160c11ee4aebcdfe62d6c15a3dd3ae3a558
Non-report diff vs tested revision at generation time:
  (empty list above = code byte-identical to the tested revision)
Host: Linux 6.12.34+rpt-rpi-2712 aarch64 GNU/Linux
bwrap: bubblewrap 0.11.2
Go: go1.26.4 linux/arm64

## Published P0 kernel/ABI floor (HARDQ C5)
- unprivileged user namespaces FUNCTIONAL, proven via the production Prepare/Start path
  with a REQUIRED exact in-sandbox sentinel NEXUS_PTRACE_DENIED:EPERM (prelaunch bwrap
  diagnostics cannot fake it)
- bubblewrap >= 0.8.0 (parsed+enforced); native seccomp table arm64/amd64, foreign audit
  arch DENIED in-filter, foreign/compat ELF rejected pre-launch

## Boundaries: FS_RO / FS_RW / NET_DENY / SYSCALL / PROC_TREE — each with a hazard-safe
negative control; closure = sealed-memfd synthetic (swap/truncate/fd-write inert);
workdir grants only under TempDir/XDG_RUNTIME_DIR, 0700-owner-only.

## Uncached run at the tested revision:
--- PASS: TestDetectFailClosedWhenBwrapAbsent (0.00s)
--- PASS: TestDetectRejectsBelowFloorVersion (0.00s)
--- PASS: TestFloorProbeFunctionalOnThisHost (0.66s)
--- PASS: TestShadowNotReadableInsideSandbox (0.59s)
--- PASS: TestCanaryOutsideClosureNotReadable (0.54s)
--- PASS: TestNegativeControlCanaryReadableWhenROLoosened (0.50s)
--- PASS: TestWorkdirWritable (0.63s)
--- PASS: TestNetworkDeniedInsideSandbox (0.64s)
--- PASS: TestNegativeControlDialSucceedsWhenNetLoosened (0.68s)
--- PASS: TestSyscallFloorDeniesPtrace (0.64s)
--- PASS: TestNegativeControlPtraceAllowedWhenSeccompLoosened (0.56s)
--- PASS: TestDynamicELFRuns (0.05s)
--- PASS: TestShebangRejectedBeforeSandbox (0.00s)
--- PASS: TestUndeclaredChildExecFailsForUsrBinary (0.64s)
--- PASS: TestTargetSwapAfterPrepareIsInert (0.57s)
--- PASS: TestKillReapsWholeTree (2.58s)
--- PASS: TestNegativeControlChildSurvivesWhenPIDNSLoosened (2.55s)
--- PASS: TestHangingTargetCleanedUpWithinTimeout (4.03s)
--- PASS: TestMemfdSealedAgainstPostPrepareWrite (0.54s)
--- PASS: TestNoFDLeakAcrossRuns (0.70s)
--- PASS: TestWorkdirRootRefused (0.62s)
--- PASS: TestFloorProbeRejectsPrelaunchPermissionDenied (0.52s)
--- PASS: TestWorkdirOutsideAllowedRootsRefused (0.54s)
--- PASS: TestWorkdirGroupWritableRefused (0.49s)
--- PASS: TestHandleRejectsRepeatStart (2.54s)
--- PASS: TestHandleRejectsStartAfterClose (0.58s)
--- PASS: TestExpandRunpathsSemantics (0.00s)
--- PASS: TestOriginRunpathBinaryResolves (0.18s)
PASS
ok  	github.com/MatNik89/nexus/internal/preflight/probe	22.585s

# PROBE-REPORT — hostile conformance suite v0 (T02)

Date: 2026-09-03T13:00:48+02:00
Host: Linux 6.12.34+rpt-rpi-2712 aarch64 GNU/Linux
Kernel: 6.12.34+rpt-rpi-2712
bwrap: bubblewrap 0.11.2
Go: go1.26.4 linux/arm64
unprivileged_userns: n/a (default-enabled kernel)

## Published P0 kernel/ABI floor (HARDQ C5)
- Linux kernel with unprivileged user namespaces enabled
- bubblewrap >= 0.11 on PATH
- (Landlock NOT required for the bwrap backend; native helper P1 will add its own ABI floor)

## Suite result: 10/10 PASS (go test ./internal/preflight/probe/ -v)

=== RUN   TestDetectFailClosedWhenBwrapAbsent
--- PASS: TestDetectFailClosedWhenBwrapAbsent (0.00s)
=== RUN   TestShadowNotReadableInsideSandbox
--- PASS: TestShadowNotReadableInsideSandbox (0.65s)
=== RUN   TestWorkdirWritable
--- PASS: TestWorkdirWritable (0.59s)
=== RUN   TestNetworkDeniedInsideSandbox
--- PASS: TestNetworkDeniedInsideSandbox (0.55s)
=== RUN   TestDynamicELFRuns
--- PASS: TestDynamicELFRuns (0.02s)
=== RUN   TestShebangRejectedBeforeSandbox
--- PASS: TestShebangRejectedBeforeSandbox (0.00s)
=== RUN   TestUndeclaredChildExecFails
--- PASS: TestUndeclaredChildExecFails (0.62s)
=== RUN   TestKillReapsWholeTree
--- PASS: TestKillReapsWholeTree (2.07s)
=== RUN   TestNegativeControlCanaryReadableWhenROLoosened
--- PASS: TestNegativeControlCanaryReadableWhenROLoosened (0.57s)
=== RUN   TestNegativeControlDialSucceedsWhenNetLoosened
--- PASS: TestNegativeControlDialSucceedsWhenNetLoosened (0.58s)
PASS
ok  	github.com/MatNik89/nexus/internal/preflight/probe	(cached)

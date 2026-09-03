# REVIEW-PHASE0-R2 — round-2 verification of the Phase-0 fix commit

Scope: fold status of codex 13 + kilo 6 (overlapping → 19), NEW defects in the rewrite. Tests run
fresh (`go test -count=1 ./internal/preflight/...` — PASS, probe 38.7s uncached). Key claims verified
empirically on this host (bwrap 0.11.2, aarch64, functional userns).

---

## (1) Round-1 findings — all folded

- **codex #1 SYSCALL floor** → real cBPF seccomp via `--seccomp <fd>` (`seccomp_linux.go`), denied
  syscall table for arm64/amd64 (others fail closed), RED `TestSyscallFloorDeniesPtrace` + negative
  control `TestNegativeControlPtraceAllowedWhenSeccompLoosened`. ✓
- **codex #2 undeclared child-exec** → synthetic closure (`resolveClosure`), no `/usr` grant;
  `TestUndeclaredChildExecFailsForUsrBinary` runs `/usr/bin/id` and requires failure. ✓ (passes)
- **codex #3 argv-rewrite launcher mismatch** → production `Prepare/Start/Wait/Run` used unmodified;
  target pinned at `/nexus-target`, no argv rewriting. ✓
- **codex #4 /etc/shadow vacuity** → precondition asserts host `/etc/shadow` exists; canary pair
  (`TestCanaryOutsideClosureNotReadable` + loosen control) makes the deny assertion red-capable. ✓
- **codex #5 PROC_TREE vacuity** → host `(pid,starttime)` oracle + production `Handle.Kill()` +
  `TestNegativeControlChildSurvivesWhenPIDNSLoosened`. ✓
- **codex #6 doctor floor gap** → separate `sandbox-floor` check via `FloorProbe` (real confined
  launch `--unshare-user`), RED `kernel-floor-forced-fail`; version floor `MinBwrapVersion` enforced. ✓
- **codex #7 loosen switches exposed** → `Spec.loosen` unexported; production callers cannot set it. ✓
- **codex #8 target/workdir pinning** → memfd content pin (`TestTargetSwapAfterPrepareIsInert`),
  workdir `EvalSymlinks`. ✓
- **codex #9 resolv.conf** → removed; closure is target+interpreter+ldd libs only. ✓
- **codex #10 doctor symlink** → `Lstat` + symlink refusal + randomized `CreateTemp` + cleanup-fail=OFF. ✓
- **codex #11 static-check fail-open** → ELF-magic + `command -v ldd` + exit-2 semantics + automated
  `internal/buildcheck` REDs (dynamic→1, non-ELF→2, static→0). ✓
- **codex #12 Timeout dead** → `exec.CommandContext` wired; `TestHangingTargetCleanedUpWithinTimeout`.
  ⚠ see NEW-ERROR below — the wiring is present but the test is vacuous.
- **codex #13 report overstates** → regenerated "v1" report: uncached, all five columns + controls,
  functional userns floor (proven by launch), bwrap floor, seccomp floor, content pinning. ✓
- **kilo #1 doctor userns** → folded via codex #6. ✓ · **kilo #2 PROC under-test** → codex #5. ✓ ·
  **kilo #3 EACCES/ENOENT** → codex #4. ✓ · **kilo #4 resolv.conf** → codex #9. ✓ ·
  **kilo #5 argv hacks** → codex #3 (hacks deleted). ✓ · **kilo #6 static-check fail-open** → codex #11. ✓
- **agy #3/#4/#5** → PROC negative control + pgrep-oracle replaced by host `(pid,starttime)` oracle +
  PIDNS loosen control; positional argv hack deleted. ✓

## (2) NEW defects

**1. [NEW-ERROR] The "hang" fixture does not hang — `select {}` trips Go's deadlock detector, so
`TestHangingTargetCleanedUpWithinTimeout` is vacuous and codex #12's bounded-cleanup is unproven.**
`cmd/probehelper/main.go:66-67`: `case "hang": select {}`. Run directly:
`probehelper hang` → `fatal error: all goroutines are asleep - deadlock!` → exit 2, in ~milliseconds.
So the test's `h.Wait()` returns because the target *self-terminated*, not because the 3s timeout
fired; `stillAlive` is trivially empty. The test would pass identically if `exec.CommandContext`
were replaced by a plain `exec.Command` (no timeout). Measured: fresh run 1.90s vs the 3s timeout —
the timeout path is never exercised. This is exactly the false-GREEN class T02 exists to prevent.
Fix: `case "hang": for { time.Sleep(time.Hour) }` (an actively-sleeping goroutine is not "deadlocked",
so the fixture genuinely blocks and the timeout/kill path is what reaps it).
(The cleanup *mechanism* is likely correct: killing bwrap terminates the pid-namespace init, and the
kernel then SIGKILLs the rest of the namespace — I confirmed a bwrap child dies on `SIGKILL` of bwrap
with `--unshare-pid`; but the test does not prove it because the fixture never reaches that path.)

## Verified sound in the rewrite (the task's specific asks)

**2. [OK] seccomp assembler jump fixup is correct.** Traced the cBPF program: `[0] LD arch`, `[1] JEQ
JT=1→[3]`, `[2] RET ALLOW`, `[3] LD nr`, one JEQ per denied nr with `JT = denyIdx - i - 1`, trailing
`RET ALLOW`, then `RET ERRNO|EPERM`. For a JEQ at index i, `i + 1 + JT == denyIdx` — fixup lands on
the deny instruction; all offsets < 256. `sockFilter` is 8 bytes (2+1+1+4) matching `struct
sock_filter`, written LE, pipe-fed with no length prefix — matches bwrap `--seccomp`'s raw-array
format. arm64/amd64 syscall numbers verified against the tables.

**3. [OK] memfd lifetime is correct.** Content is copied into an anonymous memfd at Prepare
(`memfd_linux.go`), passed as ExtraFiles, bound via `--ro-bind-data --perms 0555`; the mount holds the
inode after the parent's fds close, so post-Prepare path swap AND in-place truncation are inert
(proven by `TestTargetSwapAfterPrepareIsInert`). Minor: `h.closers` and os/exec's auto-close both close
the same fds (harmless double-close).

**4. [OK] ldd-based closure and version comparison — acceptable, one low-severity note each.** `ldd`
runs only for dynamic targets (`interp != ""`); static targets (probehelper) skip it. The library
resolution trusts the host loader (LD_LIBRARY_PATH/ld.so.preload could redirect a lib into the
closure) — low risk for a single-user preflight, but worth noting the closure is host-env-dependent.
`versionLess` parses the last whitespace field of `bwrap --version` and compares numerically; a
non-standard version string is rejected (fail-closed), not silently accepted.

## (3) Docs consistency

`PROBE-REPORT` (v1) matches the code: 18 tests, all five columns + a negative control per boundary,
functional userns floor "proven by launch", bwrap >= 0.8.0, seccomp floor, memfd pinning. `E10/B4`
refinement (synthetic content-pinned closure, no /usr grant) is reflected in `probe.go`'s package
comment and the closure model. Consistent.

---

## Verdict

All 19 round-1 findings are correctly folded, and the specific rewrite risks (seccomp assembler,
memfd lifetime, ldd, version) check out. The one material defect is a vacuous test: the `hang`
fixture deadlocks instead of hanging, so the bounded-timeout-cleanup path (codex #12) — a P0 safety
guarantee — is never actually exercised, and the suite would report GREEN with the timeout removed.

VERDICT: FAIL

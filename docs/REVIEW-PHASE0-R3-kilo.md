# REVIEW-PHASE0-R3 — round-3 final verification of the Phase-0 fix (b8e4a74)

Scope: verify the 8 round-2 folds + hunt NEW defects in the rewritten resolver/handle/seccomp.
Tests run fresh: `go test -count=1 ./internal/preflight/... ./internal/buildcheck/...` — all PASS.

---

## The 8 folds — all [OK]

**1. [OK] memfd SEALED + post-Prepare write RED.**
`memfd_linux.go:28-55` creates with `MFD_CLOEXEC|MFD_ALLOW_SEALING`, applies
`F_ADD_SEALS (SEAL|SHRINK|GROW|WRITE)`, then VERIFIES via `F_GET_SEALS` (`got&allSeals==allSeals`).
RED `TestMemfdSealedAgainstPostPrepareWrite` asserts `WriteAt` on `closers[0]` fails. ✓

**2. [OK] seccomp arch fail-closed + native ELF machine check.**
`seccomp_linux.go:92-112` — the arch JEQ now has `JT=0` (native → next) and `JF = denyIdx-
archJmpIdx-1` (foreign audit arch → trailing ERRNO, i.e. DENY). Traced fixup: `i+1+JF == denyIdx`.
`openNativeELF` (probe.go:131-146) rejects non-native machine/class (`EM_AARCH64`/`EM_X86_64` +
`ELFCLASS64`), closing the compat-32-bit bypass. ✓

**3. [OK] Handle sealed (cmd private, owned fds, Close on all paths, fd-leak test).**
`Handle.cmd` unexported; accessors `ClosureHashes()` (returns a copy) / `SetOutput()` (rejects after
Start) / `Pid()`. `closers` now includes the seccomp filter fd (probe.go:426-434). `Close()` is
`sync.Once`-idempotent; `Start()` closes on error; `Wait()`/`Close()` release exactly once. RED
`TestNoFDLeakAcrossRuns` (5 runs, `/proc/self/fd` stable). ✓

**4. [OK] ldd eliminated — debug/elf resolver with budgets.**
`resolveClosure` (probe.go:162-262) parses `PT_INTERP` + `DT_RUNPATH` + `ImportedLibraries()`
(DT_NEEDED) with BFS; never executes the target or interpreter. Budgets `maxClosureFiles=64`,
`maxClosureBytes=256MiB`. ✓

**5. [OK] FloorProbe through the production path + required in-sandbox ptrace DENIAL.**
`FloorProbe` (probe.go:72-81) calls `Run(av, Spec{Target: probeTarget, ...})` — the full
sealed-memfd + bwrap + seccomp path — and requires `strings.Contains(out, "denied")`. Doctor wires
it via `/proc/self/exe __probe-ptrace` (doctor.go:55-57), dispatched in main.go:17-19. ✓

**6. [OK] workdir guard (/ , shallow, foreign-owned, world-writable refused; symlink RED; T25 ceiling).**
`guardWorkDir` (probe.go:269-292) EvalSymlinks, refuses `/` and 1-component paths, requires
ownership, refuses world-writable; group-writable tolerated with a documented rationale; T25 ceiling
recorded via topknot comment. RED `TestWorkdirRootRefused` covers `/` and a symlink-to-`/`. ✓

**7. [OK] hang fixture no longer trips the deadlock detector.**
`cmd/probehelper/main.go:66-71`: `for { time.Sleep(time.Hour) }` (active sleep, not `select{}`).
Report shows `TestHangingTargetCleanedUpWithinTimeout` at 4.20s > the 3s timeout — the timeout now
actually fires and the cleanup path is exercised. ✓

**8. [OK] report revision-bound.**
PROBE-REPORT records "Tested revision: b8e4a74…" + "Non-report diff … (empty list … code
byte-identical)" and lists all 21 tests, including the new sealing/fd/workdir REDs. ✓

---

## NEW defects — resolver edge cases (low severity, P1/T26-scoped, not T02-blocking)

**9. [NEW-ERROR] RUNPATH `$ORIGIN`/`${ORIGIN}`/`$LIB`/`${LIB}` is not expanded.**
`depSearchDirs` (probe.go:149-158) passes runpath entries through verbatim; a binary with
`RUNPATH=$ORIGIN/lib` would have the resolver probe the literal path `$ORIGIN/lib`, fail, and the
closure build errors out. ldd handled this via the real loader; the new resolver is a regression for
`$ORIGIN` binaries. Not hit by the current targets (static probehelper / system `/bin/ls` have no
`$ORIGIN`), but it WILL surface in T26 when arbitrary promoted binaries run. Fix (T26): expand
`$ORIGIN`/`${ORIGIN}` to the target's dir (and `$LIB`/`${LIB}` to the multiarch dir), or record the
ceiling and refuse `$ORIGIN`-bearing binaries until then.

**10. [NEW-ERROR] `DT_RPATH` is ignored (only `DT_RUNPATH` is read).**
`probe.go:207-212` reads `elf.DT_RUNPATH` only. Legacy binaries that set `DT_RPATH` (no RUNPATH) get
no rpath search. Low: DT_RPATH is deprecated and rare on modern systems; still a real omission vs the
resolver's stated "PT_INTERP + DT_NEEDED + RUNPATH" contract (the package comment omits RPATH).

**11. [NEW-ERROR] Empty RUNPATH component resolves against the process cwd, not the target's.**
`strings.Split(r, ":")` yields empty strings for `::`/trailing `:`, and `filepath.Join("", name)` +
`os.Stat` then searches the probe process's current directory. ld.so semantics make an empty
component mean the *target's* cwd. Subtle mismatch; only reachable via a crafted RUNPATH. Low.

## Verified sound (no defect)

**12. [OK] BFS budget — no bypass.** `add()` enforces `len(files) >= 64` before reading and the
cumulative byte cap before sealing; the queue is deduped (`resolvedNames`) and any overrun fails
closed (returns error, closes all). One non-material note: the byte cap is checked AFTER
`os.ReadFile`, so a single >256MiB file is fully buffered before rejection — acceptable for trusted
system libraries.

**13. [OK] Group-writable tolerance is deliberate and documented** (umask 002 / user-private-group
distros); world-writable (0o002) is refused, and the T25 backend's policy-owned 0700 dirs close the
residual. Correctly flagged as a ceiling, not silently accepted.

---

## Verdict

All 8 round-2 folds are correctly implemented and test-verified; no material new defect in the
seccomp assembler, handle lifecycle, memfd sealing, or the budgeted resolver's core. The three
resolver edge cases (`$ORIGIN`/DT_RPATH/empty-component) are genuine but low-severity, do not affect
the current T02 probe targets (static/system binaries), and are correctly bounded to T26/P1 work.

VERDICT: PASS

# REVIEW-PHASE0-R4 — round-4 final verification (a5aae16) + YOLO sanity-check

Scope: the 5 round-3 folds + NEW defects in changed lines only + the YOLO amendment (1258cc1).
Tests run fresh: `go test -count=1 ./internal/preflight/... ./internal/buildcheck/...` — all PASS.

---

## The 5 folds — all [OK]

**1. [OK] FloorProbe exact sentinel `NEXUS_PTRACE_DENIED:EPERM`, emitted only on observed EPERM.**
`PtraceDeniedSentinel` (probe.go:69); FloorProbe matches an exact trimmed LINE, not a substring
(probe.go:82-87). probehelper `ptraceTraceme()` (ptrace_linux.go:11-19) and nexus `runProbePtrace()`
(probe_linux.go:15-28) emit the sentinel ONLY when `errno == EPERM`. RED
`TestFloorProbeRejectsPrelaunchPermissionDenied` drives a fake bwrap that prints "Permission denied"
and asserts the floor FAILS. ✓

**2. [OK] workdir allowed-roots + 0o022 refusal + home/0770 REDs.**
`allowedWorkRoots()` = `os.TempDir()` + `XDG_RUNTIME_DIR` (probe.go:351-357); `guardWorkDir` requires
membership under a root, current-owner, and `perm&0o022 == 0` (group AND world writability refused,
probe.go:365-392). REDs `TestWorkdirOutsideAllowedRootsRefused` (home) + `TestWorkdirGroupWritableRefused`
(0770). ✓

**3. [OK] resolver RUNPATH/RPATH + $ORIGIN + LD_LIBRARY_PATH + tests.**
`expandRunpaths` (probe.go:166-182) expands `$ORIGIN`/`${ORIGIN}`, drops empty and `$`-token
components; `elfDeps` (probe.go:186-201) reads DT_RUNPATH with DT_RPATH legacy fallback; BFS carries
per-referrer runpaths (probe.go:300-343); `LD_LIBRARY_PATH` exported from the sealed-bind dirs only
(probe.go:501-520). Unit test `TestExpandRunpathsSemantics` + cc-built `TestOriginRunpathBinaryResolves`
($ORIGIN e2e). ✓

**4. [OK] pre-allocation budgets.**
`add()` stats before reading and enforces `maxClosureOneFile`/`maxClosureBytes` pre-read
(probe.go:236-250); `maxInterpLen=4096` caps PT_INTERP (probe.go:267); `maxDepQueue=256` caps the BFS
(probe.go:310). ✓

**5. [OK] Handle repeat-Start / Start-after-Close rejected + lifecycle REDs.**
`Start()` rejects `started || closed` without touching the live child (probe.go:436-446); `Close()` sets
`closed` under `closeOnce` (probe.go:468-476). REDs `TestHandleRejectsRepeatStart` +
`TestHandleRejectsStartAfterClose`. ✓

## NEW defects (changed lines only) — low severity

**6. [NEW-ERROR] A RELATIVE RUNPATH component still resolves against the process cwd.**
`expandRunpaths` drops empty and `$`-bearing components, but a plain relative component (e.g.
`RUNPATH=lib` or `RUNPATH=../lib`) has no `$` and is appended as `filepath.Clean("lib")` (still
relative), then `depSearchDirs` yields `filepath.Join("lib", name)` → `os.Stat` relative to the probe
process's cwd. This re-opens, in a narrower form, the exact "never cwd" gap the round-3 fix (kilo #11)
claimed to close; the unit test covers only `$ORIGIN`/empty/`$LIB`/absolute, not relative. Low:
relative RUNPATH is rare and the daemon cwd is trusted, but the resolver contract is violated. Fix:
drop non-absolute components after `$ORIGIN` expansion (`if !filepath.IsAbs(comp) { continue }`).

**7. [NEW-ERROR] `LD_LIBRARY_PATH` points at dirs on the WRITABLE bwrap tmpfs root.**
bwrap's `--ro-bind-data` creates parent dirs (e.g. `/lib`, `/tmp/<origin-dir>`) as ordinary
directories on the writable tmpfs root, so the sandboxed process (uid 0 in its userns) can write new
files into them. `LD_LIBRARY_PATH` (probe.go:519) lists exactly those dirs, and the loader consults it
before the pinned default paths — so a dynamic target that writes `libc.so.6` into `/lib` and then
re-execs itself would load its own lib in place of the sealed one. Not exploitable by the current P0
targets (probehelper/nexus are static; `/bin/ls` and the `$ORIGIN` fixture don't write+re-exec), but it
becomes the T26 lib-injection surface for arbitrary promoted binaries. Fix (T26): mount the root
read-only, or bind each lib dir read-only, rather than relying on `--ro-bind-data` parent-dir creation.

## YOLO amendment (1258cc1) — [OK], one wording note

The amendment matches HARDQ F2: confirmations-only bypass (ASK→ALLOW, journaled `ALLOWED_BY_YOLO`);
DENY, sandbox, egress, journal, redaction, profile isolation and the golden rule stay active;
local-CLI entry only; PRD §6 evaluated in default mode. T14 `PolicyMode{Default|Yolo}` with
`TestYoloAllowsAskButNeverDeny` + `TestYoloCannotBeSetByChannelInput`; T17 `--yolo` flag surfaced in
the status line; T24 yolo HITL RED asserts safety-nets unchanged. No contradiction with locked sources.
Wording note (non-blocking): the owner invariant "policy = S6.0 PEP (default-deny, ASK ≠ ALLOW)" is
now qualified by yolo's ASK→ALLOW but the hard rule doesn't say "in default mode" — a one-word
qualifier would prevent a literal reader from rejecting the yolo behavior.

---

## Verdict

All 5 round-3 folds are correctly implemented and test-verified; the exact-sentinel floor, allowed-root
workdir guard, per-referrer RUNPATH/RPATH resolver, pre-allocation budgets and Handle lifecycle are
all sound. The two new findings (relative-RUNPATH→cwd; LD_LIBRARY_PATH over writable tmpfs dirs) are
genuine but low-severity and correctly bounded to T26/edge cases, not the current trusted probe
harness. YOLO is consistent with F2.

VERDICT: PASS

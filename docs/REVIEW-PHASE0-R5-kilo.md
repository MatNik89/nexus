# REVIEW-PHASE0-R5 — round-5 final convergence check (bb5b4c6)

Scope: the 7 round-4 folds + NEW defects in lines changed by bb5b4c6. Tests run fresh:
`go test -count=1 ./internal/preflight/... ./internal/buildcheck/...` — probe 33, doctor 8,
buildcheck 3, all GREEN.

---

## The 7 folds — all [OK]

**1. [OK] Transitive old-dtags DT_RPATH inheritance per queue node.**
`depNode{direct, inherited}` (probe.go:324-328); a lib with DT_RPATH (not RUNPATH) appends its
paths to the child's `inherited` chain (probe.go:383-386), propagating the ancestor RPATH through
the BFS. `TestTransitiveRpathBinaryResolves` (main old-dtags RPATH → liba → libb) proves the
transitive lookup. ✓

**2. [OK] loadBounded single-descriptor fstat + bounded read; metadata from pinned bytes.**
`loadBounded` opens once, `f.Stat()` + `io.LimitReader(f, max+1)` on the SAME descriptor
(probe.go:146-167), so a swap/grow between stat and read cannot bypass the budget.
`parseELFMeta` parses machine/class/interp/needed/RUNPATH from the exact pinned `buf`
(probe.go:180-211) — metadata can never describe different bytes than the sandbox runs. Queue cap
checked pre-append in `push` (probe.go:334-340). ✓

**3. [OK] workdir roots: /run/-rooted 0700 user dir OR root-owned sticky temp; hostile-XDG RED.**
`allowedWorkRoots` (probe.go:402-440) EvalSymlinks+Clean, then admits only (a) owner==euid &&
perm==0700 && canonical under `/run/`, or (b) uid==0 && sticky. A 0700 `$HOME` (which an interim
shape-only rule wrongly admitted) is rejected because it is not `/run/`-rooted.
`TestHostileXDGRuntimeDirCannotWidenRoots` + `TestWorkdirGroupWritableRefused`. ✓

**4. [OK] report says owner-owned / no-group-world-write exactly.**
PROBE-REPORT v4 lines 19-21: "owner-owned, no group/world write, only under a root-owned sticky
temp root or a /run/-rooted 0700 user runtime dir (ambient TMPDIR/XDG cannot widen the
allowlist)." Matches the code. ✓

**5. [OK] yolo real-backend RED moved T24 → T27.** (68fcb2e; T24 keeps the policy REDs, the
hostile-suite-in-yolo integration RED moved to T27 where the real backend exists.) ✓

**6. [OK] relative RUNPATH components dropped.**
`expandRunpaths` now drops non-absolute components after `$ORIGIN` expansion
(`if !filepath.IsAbs(comp) { continue }`, probe.go:230-232). ✓

**7. [OK] libs in RO /nexus-libs + rootfs remount-ro + shadowing REDs.**
All libs pinned under the single `insideLibDir = "/nexus-libs"` (probe.go:379), LD_LIBRARY_PATH
points only there (probe.go:591), and `--remount-ro /` (probe.go:634) is emitted after all binds —
so the loader-searched dirs are read-only and the RW /work and /tmp mounts are unaffected.
`TestRootfsReadOnlyPreventsLibShadowing` asserts writes into /nexus-libs and /lib both fail. ✓

---

## NEW defects in changed lines — none material

**8. [NEW-ERROR, LOW] A descendant's DT_RUNPATH does not "cut" the inherited RPATH chain.**
`childInherited = node.inherited` is kept unchanged when the referrer has DT_RUNPATH
(probe.go:383-386). Under glibc new/old-dtags semantics, an object carrying DT_RUNPATH stops the
ancestor DT_RPATH inheritance for its own subtree, so the resolver over-propagates the ancestor
RPATH past a RUNPATH-bearing library. Consequence: in a mixed RPATH→…→RUNPATH chain the resolver
could pin a library from an ancestor RPATH dir that the real loader would NOT consult, producing a
closure/loader mismatch. Unexercised by every P0 target (static probehelper/nexus, system /bin/ls,
and the fixtures use RUNPATH-only or RPATH-only, never a mixed cut), so non-material for T02; worth
a T26 note when arbitrary promoted binaries are resolved. (The `TestTransitiveRpathBinaryResolves`
fixture covers inheritance-through-plain-objects, not the RUNPATH-cut case.)

No other defect in the changed lines: the single-fd `loadBounded` closes the stat/read TOCTOU, the
pre-append queue cap bounds the BFS, the rootfs remount-ro preserves the RW /work and /tmp mounts,
and `parseELFMeta` from pinned bytes removes the metadata/content divergence.

---

## Verdict

All 7 round-4 folds are correctly implemented, documented and test-verified; the report wording and
task renumbering are consistent. The one new observation (RUNPATH not cutting the inherited RPATH
chain) is a genuine but low-severity, unexercised glibc-semantics edge case, correctly bounded to
T26 and non-material for the current probe.

VERDICT: PASS

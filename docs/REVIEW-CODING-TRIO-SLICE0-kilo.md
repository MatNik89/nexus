# Review of coding-trio Slice 0 fold (b227412) — code review round 2

- **Reviewed**: commit `b227412` (`git diff cd7616f..b227412`) on `main`
- **Verdict**: PASS

All six CODE1 findings plus the notes are folded correctly. I re-verified each
against the actual code (not the commit message) and ran the four packages —
`probe` 35.2s, `sandbox` 17.3s, `sealedstore`, `impact` all green, against the
real bwrap backend.

## Re-verification (the five asks)

### 1. probe.go denylistedROBindRoot — bug fixed correctly for every entry

The old unconditional `return true` is gone. The new `denylistedROBindRoot`
(`probe.go:555-572`) special-cases `/` (`canon == "/"`) and, for every other
root, does a path-segment-aware check (`canon == root ||
strings.HasPrefix(canon, root+"/")`), so `/etc` and `/etc/passwd` are denied while
`/etcetera` is not. It runs in `guardROBind` BEFORE the ownership/mode checks. The
denylist is a fixed set of core system trees, correctly documented as
defense-in-depth with the caller still owning the toolchain-path scoping.

### 2. sandbox.go cloneSpec — real deep copy, all three fields

`cloneSpec` (`sandbox.go:252-270`) deep-copies `Args` (`append([]string(nil), …)`),
`ExtraROBinds` (same), and `ExtraEnv` (a fresh map with each k/v copied); `loosen`
is a value field and copies with the struct. The `CompiledPolicy` now holds this
independent copy, so a post-Compile mutation of the caller's Spec cannot desync
Launch from the attested policyHash. `roBindsDigest` now canonicalizes
(`EvalSymlinks`) before hashing and returns an error, folding the effective
boundary (not the literal spelling) into the hash.

### 3. sealedstore descriptor-relative rewrite — no leak, no TOCTOU, no deadlock

- **fd**: `dirFd` opened once (`Open`, O_CLOEXEC) and released by `Close`; every
  `readAtLocked`/`listNames` fd is `os.NewFile` + `defer Close`; `writeAtLocked`'s
  temp fd is closed exactly once (the `cleanup` closure runs only on the
  Write/Sync error paths, before the explicit `Close` at `:279`; the post-close
  paths use `unix.Unlinkat` directly, never a second Close).
- **TOCTOU**: all ops are `openat`/`renameat`/`unlinkat` against the held `dirFd`
  with `O_NOFOLLOW` on the final component, so there is no Lstat-then-open window
  and no parent-path re-resolution (a mid-flight directory rename can't redirect
  the already-open fd). `f.Stat()` + `io.ReadAll(f)` act on the open fd.
- **Put+GC single critical section**: `Put` holds `s.mu` for its whole body and
  calls the `*Locked` helpers; `GC` holds `s.mu` across `listNames` + the sweep
  and reads `s.pins[name]` directly (`:210`) rather than a snapshot. `readAt` and
  `Release` each lock once and call the non-locking variants — no re-lock, no
  deadlock. `gcPauseHook` runs inside GC's critical section, proving Put blocks.
- **Symlink safety**: `writeAtLocked` creates the temp with `O_CREAT|O_EXCL|
  O_NOFOLLOW` and `renameat`s it into place (replaces, never follows); `Put`'s
  existing-digest shortcut now re-verifies content via `readAtLocked` + hash
  (`:138-148`) instead of trusting Lstat success.

### 4. impact.go prodRdeps/testOwners split — correct, no under-inclusion

`BuildGraph` puts production imports in `prodRdeps` and test imports in
`testOwners`. `Affected` walks `prodRdeps` transitively and, at each visited node
including the changed set, adds `testOwners[cur]` to the result WITHOUT enqueueing
(terminal). I walked `TestAffectedTestOnlyEdgeDoesNotPropagateTransitively`:
`a2 <-test- f2 <-production- g2` → `Affected(a2) = {f2}` (g2 excluded), which the
old conflated graph would have gotten wrong (g2 included). The terminal edge is
correct because a package that reaches `imp` only through its test file has no
production dependency on `imp`, so its production callers never see `imp` — no
legitimate case becomes under-inclusive, and a package that is BOTH a production
and a test importer gets the `prodRdeps` edge (transitive) plus the `testOwners`
edge (terminal), so its own dependents are still walked. A `testOwners` member
always has tests (`TestImports` non-empty implies `TestGoFiles` non-empty), so the
unconditional `result[owner]=true` never selects a non-testable package.

### 5. False-green test fixes — now non-vacuous

`TestGetRefusesSymlink` names the symlink by the outside data's TRUE digest, so a
symlink-following Get would return matching bytes — the test can only pass via the
refusal path. `TestAffectedTestOnlyEdgeDoesNotPropagateTransitively` is the real
counterexample above. `TestParseGoListRealSample` asserts on both `sealedstore`
and `atomicwrite` (later stream objects), so a decode-first-only regression fails.
The `printenv` probehelper subcommand (`cmd/probehelper/main.go:84-89`) lets
`TestExtraROBindsAndEnvPropagateThroughFullProtocol` assert the env value, not
just its non-error presence.

## Notes (not FAIL reasons)

1. `reservedEnvKeys` (`probe.go:611-615`) covers `LD_LIBRARY_PATH`/`LD_PRELOAD`/
   `PATH` — the three that unseat the closure-pinning baseline. `LD_AUDIT` (another
   loader variable) is not reserved; low risk since the audit library would still
   have to resolve inside the sandbox's pinned closure + ro-binds, but worth adding
   if the caller surface ever exposes arbitrary env.
2. `denylistedROBindRoot` is a fixed list; a non-standard sensitive mount (e.g. a
   custom `/data`) would not be denied by probe itself. The doc comment correctly
   assigns that scoping to the future coding-runner via `go env` resolution — a
   layered design, but the caller-side allowlist should be a named Slice-0 deliverable
   so it isn't deferred indefinitely.

VERDICT: PASS

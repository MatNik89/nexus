# Review of coding-trio Slice 0 (partial) — code review round 1

- **Reviewed**: commit `cd7616f` (`git diff 2c05079..HEAD`) on `main`
- **Verdict**: PASS

The four pieces are correct against their claims. I verified the security-critical
probe/sandbox changes against the actual code and ran the four packages
(`probe`, `sandbox`, `sealedstore`, `impact`) — all green, with `probe` (32.8s) and
`sandbox` (14.6s) exercising the REAL bwrap backend, not mocks. Four notes below;
none is a correctness/security defect.

## Verification (as asked)

### 1. probe.go ExtraROBinds/ExtraEnv — no weakening of existing protections

For a caller passing NEITHER field, the new blocks are gated on
`len(spec.ExtraROBinds) > 0` / `len(spec.ExtraEnv) > 0` (`probe.go:698,714`), so the
argv is byte-identical to the pre-change path. For a caller that DOES pass them: each
guard is `guardROBind` (EvalSymlinks canonicalize, absolute, is-dir, owned-by-user-or-root,
reject 0o022 group/world-writable) and the bind is `--ro-bind <canon> <canon>` — read-only,
and the later `--remount-ro /` (`probe.go:730`) applies on top. Network/pid/seccomp/memfd
closure are all appended AFTER, unchanged. No write access, no isolation bypass. The
policy-hash fold is separate (piece 2). The "narrowly-scoped" phrasing in the Spec doc
comment overstates the guard (Note 1).

### 2. sandbox.go policyHash fold — correct, collision-free

`roBindsDigest`/`envDigest` (`sandbox.go:245-269`) sort before hashing and length-prefix
each element (`%d:%s`), so two Specs differing only in ExtraROBinds/ExtraEnv order produce
identical digests, and two differing in CONTENT produce distinct digests — no
concatenation-ambiguity collision, no boundary collision. The raw (not canonicalized) binds
are hashed, which is conservative: two Specs that canonicalize to the same bind still get
distinct hashes (fail toward MORE distinct policies, never fewer). The digests are folded
into `policyHash` (`sandbox.go:240-244`), and `Launch` threads both fields into
`probe.Spec` (`sandbox.go:340-342`). An approval/attestation is now bound to the effective
ro-bind/env boundary.

### 3. sealedstore — race-free Pin/GC, sound Get, no path injection

- **(a) Pin/Release/GC race.** `Put` registers the pin (`s.mu` held) BEFORE writing
  (`sealedstore.go:99-113`); `GC` does `ReadDir` FIRST, THEN snapshots `pins` under `s.mu`
  (`:154-163`). So any artifact present in `entries` was written after its pin was
  registered, which is before the snapshot — `pinned[d]` always covers it. No TOCTOU
  between "check pins" and "delete file". `Release` is `sync.Once`-idempotent (`:76-86`).
- **(b) Get defenses.** `digestRE` gate (`:125`), `Lstat` (no follow) + explicit symlink
  refusal (`:129-135`), then re-hash-and-compare (`:140-143`) — a corrupted blob never
  returns bytes. Note 2 on the Lstat→ReadFile step vs the plan's "descriptor-relative"
  wording.
- **(c) Path injection.** Every path is `filepath.Join(s.dir, digest)` with `digest`
  always `^[0-9a-f]{64}$` (Put derives it from sha256; Get/GC refuse anything else). No
  `..`, `/`, or traversal is expressible.

### 4. impact.go — graph walk correct; test-import handling is safe-but-imprecise

`ParseGoList` uses a `json.Decoder` `More()` loop (`:49-60`), correctly decoding the
concatenated-JSON-objects stream `go list` emits (a plain `json.Unmarshal` would only see
the first object). `BuildGraph` skips `ForTest != ""` entries (avoiding double-count of the
`.test` main and `[p.test]` variants), records reverse edges from both `Imports` and
`TestImports`/`XTestImports`, and skips self-imports. `Affected` walks the transitive
closure, marks `resolved=false` for any changed path outside `known` (fail-safe full-suite
fallback), and intersects with `tests`. Self-import, dedup, and transitive dependents are
all handled. Note 3 on the test-import edge semantics.

## Notes (not FAIL reasons)

1. **`guardROBind` does not enforce "narrowly-scoped".** It checks ownership + write bits
   only, so `ExtraROBinds: []string{"/"}` (or `/home/user`, `/etc`, which are all
   0755/root- or user-owned) would pass and grant read-only visibility of the entire host.
   This is safe (read-only + `--remount-ro`, and the caller is NEXUS's own trusted
   coding-runner passing toolchain paths, not untrusted input), but the Spec doc comment
   ("narrowly-scoped") overstates what the guard guarantees. Consider wording it as
   "operator-owned read-only visibility grants" and/or adding a caller-side allowlist of
   permitted toolchain prefixes in Slice 0's own review.
2. **`Get` is path-based, not descriptor-relative.** The plan (invariant 4) says "all reads
   and writes are descriptor-relative"; `Get` does `os.Lstat` then `os.ReadFile`, leaving a
   theoretical swap-to-symlink window between the two. Practically unreachable (0700
   daemon-owned dir; `Put`/`GC` never create symlinks; no concurrent writer) and the
   post-read digest check is the real gate (a symlink target fails the hash, so no data is
   ever returned). A `os.OpenFile(…, O_NOFOLLOW)` + `fstat` + `ReadAll` would close the gap
   and match the plan wording.
3. **`impact.go` treats test-only imports as base imports.** `TestImports`/`XTestImports`
   create the same `rdeps[imp][p]` edge as `Imports`, so a change to a test-only dependency
   of `p` is (transitively) attributed to packages that import `p` — safe over-inclusion,
   never under-inclusion. The comment "record it only as a self-referential test edge"
   (`impact.go:104-106`) doesn't match the code (which records a normal dependent edge) and
   is misleading. Correct behavior for shadow-mode TIA (precision is measured, not assumed),
   but the comment should be fixed or the edge distinguished.
4. **No concurrent Pin/GC test.** `TestGCMarkAndSweep` is sequential; there is no test
   racing `Put` against `GC` under `-race`. The code is race-free by inspection (see
   finding 3a), but a concurrent detector would guard a future regression.

VERDICT: PASS

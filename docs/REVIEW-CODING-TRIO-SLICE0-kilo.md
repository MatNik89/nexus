# Review of 1f33da2 — CODE4 narrow re-verification (one bypass fix)

- **Reviewed**: commit `1f33da2` (`git diff 6a6085b..1f33da2`) on `main`
- **Verdict**: PASS — the symlink-retarget bypass is closed, nothing new.

`go build ./...`, `go vet ./...`, and `go test -count=1 ./internal/...` all green.
Both new detectors pass against the real bwrap backend.

## Fix verification (the three asks)

### 1. The bypass is closed for every case, and no legitimate caller is wrongly refused

The old check `if pinned, ok := ...; ok && pinned != identity` failed OPEN on a map miss.
The new logic (`probe.go:823-832`) is:

```go
if spec.ExtraROBindIdentities != nil {
    pinned, ok := spec.ExtraROBindIdentities[canon]
    if !ok {
        return fail(... "no identity pin found for this resolved path — possible symlink-retarget attack since compile")
    }
    if pinned != identity {
        return fail(... "directory identity changed since it was compiled")
    }
}
```

Both the symlink-retarget (resolves to a *different* canonical path → `!ok` → refuse) and
the plain directory swap (same canonical path, different dev/inode → `pinned != identity` →
refuse) are now fail-closed. The `!= nil` gate is the correct discriminator: I grepped every
`ExtraROBindIdentities` construction site — the ONLY producer is `sandbox.Compile`
(`sandbox.go:252`, threaded at `:425`), which pins every declared `ExtraROBinds` entry, so a
non-nil map from Compile always has one entry per entry. A direct `probe.Prepare` caller
(only tests today) passes the nil zero value and correctly skips the check. A non-nil-but-empty
map alongside non-empty `ExtraROBinds` is an inconsistent Spec that can only arise from a bug
or a hand-built map — refusing it is the right fail-closed behavior, and nothing legitimate
constructs it.

### 2. The new detector reproduces the original attack

`TestLaunchRefusesROBindSymlinkRetargetedAfterCompile` compiles with `ExtraROBinds: [link]`
where `link → beforeDir`, pins `beforeDir`'s identity, then retargets `link → afterDir` and
asserts `Launch` refuses. The target's arg is deliberately `afterDir/canary` (readable only if
the bind is — wrongly — `afterDir`), so the OLD fail-open code would have launched, bound
`afterDir`, and returned "AFTER" (the test's `t.Fatalf("exposed replacement bytes")`). It is a
faithful reproduction of the bypass. PASS (1.45s, real bwrap).

### 3. `TestGCLoadLiveRunsWhileLockIsHeld` proves its name

`loadLive` blocks on a channel; while it is blocked inside `GC`'s critical section, a
concurrent `Put` is asserted to NOT complete within 50 ms (it blocks on `s.mu`), then
`resumeLoadLive` releases and both finish. This directly distinguishes "loadLive is called
under `s.mu`" from "loadLive is called just before `s.mu.Lock()`" — the mutation-sensitivity
gap the earlier `TestGCLoadLiveIsCalledUnderTheLock` left open. PASS.

## Notes (not FAIL reasons)

- `TestGCLoadLiveRunsWhileLockIsHeld` uses a 50 ms timing assertion (`time.After`), which is a
  mild flake risk on an extremely loaded CI, though the blocked-channel design makes a false
  pass structurally near-impossible (a false pass requires Put to wrongly acquire `s.mu` while
  GC holds it, which is the very bug under test). Acceptable.

VERDICT: PASS

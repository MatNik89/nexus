# Review of 6a6085b — CODE3 narrow re-verification (two findings only)

- **Reviewed**: commit `6a6085b` (`git diff b227412..6a6085b`) on `main`
- **Verdict**: PASS — both findings are closed, nothing new.

`go build ./...`, `go vet ./...`, and `go test -count=1 ./internal/...` all green.
`TestLaunchRefusesROBindDirectorySwappedAfterCompile` (real bwrap, 1.2s) and the full
`sealedstore` suite pass.

## Finding #1 — ExtraROBinds swap (path-pinned but not identity-pinned) — CLOSED

`guardROBind` now returns `(canon, ROBindIdentity{Dev,Ino}, err)` (`probe.go:587-635`),
and `PinROBindIdentity` is `guardROBind`'s canonicalize+denylist+ownership+perm check plus
the dev/inode capture (`probe.go:592-594`). `sandbox.Compile` pins each ExtraROBinds entry
at compile time and folds `roBindIdentitiesDigest` (sorted by canonical path, dev/inode)
into `policyHash` (`sandbox.go:244-276,296-312`); the pins are threaded through
`Spec.ExtraROBindIdentities` into `Launch` (`sandbox.go:425`) and `Prepare`, which re-runs
`guardROBind` at launch time and refuses when the same canonical path now resolves to a
different dev/inode (`probe.go:806-809`). The policyHash therefore binds both the path and
the identity, and the identity is re-verified at the actual launch.

The test is non-vacuous and exercises the real attack: it compiles with `ExtraROBinds:
[dir]` (canary "BEFORE"), then `os.Rename(dir, dir+".old")` and `os.Mkdir(dir)` + writes
"AFTER", and asserts `Launch` fails (`sandbox_linux_test.go`). The old identity-less code
would have launched and read "AFTER".

## Finding #2 — sealedstore.GC stale liveDigests — CLOSED

`GC` now takes `loadLive func() map[string]bool` and invokes it **while `s.mu` is held**
(`sealedstore.go:211-219`), so the "caller decided what is live" → "GC actually started"
window is removed — the part sealedstore itself controls. The doc comment is honest about
what remains the caller's job: `loadLive` must perform a FRESH read of the durable
reference source (the journal) on every call, never a memoized snapshot — a discipline
sealedstore cannot enforce, and correctly placed on the caller. The pin still covers the
`[Put → journal-commit → Release]` window, and the post-Release reference is now read
atomically with the sweep.

The test `TestGCLoadLiveIsCalledUnderTheLock` simulates exactly the
Put → commit (`live[d]=true`) → `Release` sequence and asserts the artifact survives a
`GC(loadLive)` whose `loadLive` reads fresh. It is non-vacuous against the old
`GC(map[string]bool)` API (a pre-committed snapshot would have deleted it).

## Notes (not FAIL reasons)

1. `TestGCLoadLiveIsCalledUnderTheLock` is slightly misnamed — it directly proves "a fresh
   `loadLive` is honored", not literally "called under the lock"; the lock placement is
   established by code inspection plus `TestGCAndPutSerializeUnderOneCriticalSection`'s
   `gcPauseHook`. Harmless.
2. The `loadLive` doc comment should add "must not call back into the Store (would deadlock
   under `s.mu`)" — the intended journal-scan caller doesn't re-enter, but a future caller
   might.
3. `PinROBindIdentity` is a bounded swap detector (dev+inode, not content); an in-place edit
   of files *within* an unchanged directory is not detected. This is correctly scoped in the
   `ExtraROBindIdentities` doc comment as the toolchain-pinning caller's responsibility, not
   a defect.

VERDICT: PASS

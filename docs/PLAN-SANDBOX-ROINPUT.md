# PLAN: sandbox read-only input grant (kernel slice, prerequisite for media)

## Why
The sandbox (`internal/sandbox`) mounts only the promoted executable's
memfd-pinned ELF closure + one writable WorkDir (`sandbox.go:301-324`,
`probe.Prepare`). A tool that must READ a trusted data file it does not
carry in its closure — the whisper model (`ggml-base.bin`, ~140 MB), later
image/video model weights — has no way to open it inside the namespace.
Copying big data into a per-call WorkDir on an SD-card Pi is rejected
(latency + a mutable model surface). This slice adds the missing primitive:
a digest-pinned, read-only bind of a fixed host file, with the SAME
compile-then-verify-at-launch discipline the closure pins already use.

This is a HARD-RULED area (S6.2 sandbox). It lands and passes the three-agent
review BEFORE the voice slice builds on it.

## Contract
Extend `sandbox.Spec` with:

```go
type InputGrant struct {
    HostPath string // absolute, canonical host path of a regular file
    Dest     string // absolute in-sandbox mount path (e.g. /inputs/model.bin)
    MaxSize  int64  // reject at Compile if the file exceeds this
}
type Spec struct {
    Target      string
    Args        []string
    WorkDir     string
    Timeout     time.Duration
    ReadOnly    []InputGrant // NEW: digest-pinned read-only binds
}
```

Empty `ReadOnly` = today's behavior byte-for-byte (no bind added, no policy
change) — the field is purely additive.

### Compile (pin)
For each grant, at Compile time:
- `EvalSymlinks` + `filepath.IsAbs` the HostPath; reject a non-canonical or
  relative path.
- `Dest` must be absolute, must NOT be `/work`, `/nexus-target`, a loader
  path, or collide with another grant's Dest (deny-default, fail closed).
- `os.Lstat`: must be a REGULAR file (no symlink, dir, device, fifo);
  owner = the daemon uid; not group/other-writable; size ≤ MaxSize.
- SHA-256 the bytes; store `{Dest, digest, size}` in `CompiledPolicy`
  alongside `closurePins`.

### Launch (verify + bind)
- Re-`Lstat` + re-hash each granted HostPath; if digest OR size differs from
  the compile-time pin → refuse (`fail closed`, identical wording/shape to
  the existing closure-member refusal at `sandbox.go:319-323`). This closes
  the TOCTOU window the same way the closure pins do.
- Bind each into the namespace read-only: bwrap `--ro-bind HOST DEST`
  (threaded through `probe.Prepare` where the WorkDir `--bind` is built).
- Nothing else in the host FS becomes visible (unchanged): the grant is an
  ALLOWLIST of exactly the pinned files.

The caller's argv references `Dest`, never the host path.

## Detectors (RED before, GREEN after; anchored to the sandbox contract)
- GRANTED-READABLE: a granted fixture file is readable at `Dest` inside the
  sandbox (a `cat Dest` target succeeds and returns the exact bytes).
- NEIGHBOR-INVISIBLE: an undeclared sibling file in the SAME host directory
  as a granted file is NOT openable inside the sandbox (proves it is a
  per-file allowlist, not a directory bind). RED against a naive
  `--ro-bind <dir>` implementation.
- TOCTOU-REFUSED: mutate the granted file's bytes between Compile and Launch
  → Launch refuses fail-closed, no process starts. RED against a
  bind-without-reverify implementation.
- REJECT-AT-COMPILE: a symlink, a device node, a group-writable file, and an
  over-MaxSize file each fail Compile with a typed error (table test).
- EMPTY-GRANT-UNCHANGED: a Spec with `ReadOnly == nil` produces byte-identical
  bwrap argv to the pre-slice build (guards the additive claim).

## Non-goals
- Writable extra binds (WorkDir stays the only RW mount).
- Directory grants, glob grants (single regular files only).
- Dynamic/runtime grant activation (sealed at Compile, per HARDQ B9).

# PLAN: sandbox read-only input grant (kernel slice, prerequisite for media)

## Why
The sandbox (`internal/sandbox`) mounts only the promoted executable's
memfd-pinned ELF closure + one writable WorkDir (`sandbox.go:301-324`,
`probe.Prepare`). A tool that must READ a trusted data file it does not
carry in its closure — the whisper model (`ggml-base.bin`, ~140 MB), later
image/video weights — has no way to open it inside the namespace. This
slice adds the missing primitive: a **held-fd**, digest-pinned, read-only
bind of a fixed host file, using the SAME sealed-fd discipline the closure
already uses (`--ro-bind-data <fd>`, `probe.go:645`), so a post-verify path
swap cannot change the bytes the sandbox reads.

HARD-RULED area (S6.2). Lands and passes the three-agent review BEFORE the
voice slice builds on it.

## Contract
Extend `sandbox.Spec` (additive; empty = today's behavior byte-for-byte):

```go
type InputGrant struct {
    HostPath string // absolute host path of a regular file, opened O_NOFOLLOW
    Dest     string // in-sandbox path; MUST be a single-file child of /inputs
    MaxSize  int64  // reject at Compile if the file exceeds this
}
type Spec struct {
    Target   string
    Args     []string
    WorkDir  string
    Timeout  time.Duration
    ReadOnly []InputGrant // NEW
}
```

### Compile (pin, once)
For each grant:
- Open `HostPath` with `O_NOFOLLOW|O_CLOEXEC` and HOLD the fd for the daemon
  lifetime (a swap/rename of the pathname afterwards cannot change the inode
  this fd points to — this is the property the pathname re-check lacked in
  v3, and it mirrors the closure's held-fd binding, not a re-`--ro-bind` of a
  pathname).
- `fstat` the fd: REGULAR file; owner is the daemon uid OR root (a packaged,
  root-owned, non-writable model is allowed — an intentional install choice);
  NOT group/other-writable; `nlink == 1`; size ≤ MaxSize.
- Read the bytes from THAT fd (bounded by MaxSize) and SHA-256 them.
- Normalize `Dest`: `filepath.Clean(Dest) == Dest`, absolute, and a direct
  single-segment child of the reserved `/inputs` dir (e.g. `/inputs/model.bin`).
  Reject equality with, or being an ancestor/descendant of, ANY built-in mount
  (`/tmp`, `/work`, `/nexus-target`, every closure dest) after normalization —
  closes the `/inputs/../nexus-target` shadowing class.
- Pin `{normalizedDest, digest, size}` into `CompiledPolicy`.

### Policy + attestation identity (v3 gap #2)
The canonical, dest-sorted grant set (`normalizedDest, digest, size` per grant)
enters BOTH `policyHash` (`sandbox.go:227-235`) and the launch `Attestation`
(`sandbox.go:352-375`). Two policies that differ only in a model grant MUST
have different `policyHash` and different attestation digests — no grant may
execute under an identity that does not name it.

### Launch (bind the held bytes)
- Re-`fstat` each HELD fd (not the pathname); if size/inode identity differs
  from the Compile pin → refuse fail-closed (identical shape to the closure
  refusal at `sandbox.go:319-323`).
- Bind each into the namespace read-only from the inherited fd:
  `--ro-bind /proc/self/fd/<N> <Dest>` (the fd is passed via `ExtraFiles`,
  as the closure already does). bwrap resolves the magic-symlink to the held
  inode; the original pathname is never re-opened at launch.
- Nothing else in the host FS becomes visible: the grant is an allowlist of
  exactly the pinned inodes.

The caller's argv references `Dest`, never the host path.

Residual (named ceiling): a same-uid, same-INODE content rewrite (truncate +
overwrite of the very inode the fd holds) between Compile hash and Launch is
not closed by held-fd binding; on a single-owner box the model is our own
trusted artifact. Full immutability (a sealed memfd copy) is rejected for a
~140 MB model on an SD-card Pi (per-call full read). Stated, not hidden.

## Detectors (RED before, GREEN after; anchored to the sandbox contract)
- GRANTED-READABLE: a granted fixture is readable at `Dest` inside the
  sandbox and returns the exact bytes.
- NEIGHBOR-INVISIBLE: an undeclared sibling in the same host dir is NOT
  openable inside the sandbox (proves per-file allowlist, not a dir bind).
- SWAP-DEFEATED: replace the source PATHNAME with different bytes between
  Compile and Launch → the sandbox still reads the ORIGINAL pinned bytes
  (held fd), proving the pathname swap is defeated. RED against a
  re-`--ro-bind <pathname>` implementation.
- POLICY-IDENTITY: two Specs differing only in a grant's file produce
  DIFFERENT `policyHash` and DIFFERENT attestation digests. RED against a
  grant-not-in-identity implementation.
- DEST-COLLISION: `/inputs/../nexus-target`, a `/work` descendant, and a
  non-`/inputs` dest each fail Compile with a typed error.
- REJECT-AT-COMPILE: a symlink source, device node, FIFO, `nlink>1`,
  group-writable, and over-MaxSize file each fail Compile.
- EMPTY-GRANT-UNCHANGED: `ReadOnly == nil` yields byte-identical bwrap argv
  to the pre-slice build (guards the additive claim).

## Non-goals
- Writable extra binds (WorkDir stays the only RW mount).
- Directory/glob grants (single regular files only).
- Aggregate disk / file-count quotas — that is the existing S6.2 **P2.2
  ResourceBudget** obligation (`docs/HARNESS-SPEC.md:1481-1484`), a separate
  slice; this slice does NOT re-implement it.
- Dynamic/runtime grant activation (sealed at Compile, per HARDQ B9).

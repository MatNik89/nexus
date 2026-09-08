# PLAN: sandbox read-only input registry (kernel slice, prerequisite for media)

## Why
The sandbox mounts only the executable's memfd-pinned ELF closure + one
writable WorkDir (`sandbox.go:301-324`). A tool that must READ a trusted data
file it does not carry in its closure — the whisper model (`ggml-base.bin`,
~140 MB), later image/video weights — cannot open it inside the namespace.
This slice adds that primitive. HARD-RULED area (S6.2); lands and passes the
three-agent review BEFORE the voice slice builds on it.

## Design — a startup-owned registry, NOT a per-call open (r3 codex F2)
`CompiledPolicy` is a copyable value compiled PER CALL (WorkDir is in
`policyHash`, `sandbox.go:227-235`, and voice makes a fresh WorkDir per
transcription). Opening a model fd inside per-call Compile and holding it
"for the daemon lifetime" would LEAK one fd per call → `EMFILE`. So the
immutable inputs get their OWN lifecycle, independent of the per-call policy:

- `InputRegistry`, built ONCE at daemon startup from config. For each
  configured input it opens `HostPath` with `O_NOFOLLOW|O_CLOEXEC`, `fstat`s
  the fd (REGULAR; owner = daemon uid OR root; NOT group/other-writable;
  `nlink == 1`; size ≤ MaxSize), reads the bytes from that fd once and records
  a startup SHA-256, and HOLDS the fd for the daemon lifetime. Opened once,
  bound many times — no per-call open, no leak.
- A per-call `Spec` does not open anything; it REFERENCES a registered input
  by id + `Dest`:

```go
type InputRef struct {
    ID   string // key into the InputRegistry
    Dest string // in-sandbox path; a single-file child of /inputs
}
type Spec struct {
    Target   string
    Args     []string
    WorkDir  string
    Timeout  time.Duration
    Inputs   []InputRef // NEW; empty = today's behavior byte-for-byte
}
```

## What is pinned — IDENTITY, not content-execution (r3 codex F3)
`--ro-bind` of a held fd defeats a pathname SWAP (the fd names a fixed inode),
but the inode is still a live file: a same-uid in-place rewrite (same inode,
same size) changes bytes the child reads without changing path/inode/size.
Sealing 140 MB into a memfd per the closure discipline would make content
attestation TRUE but costs 140 MB resident RAM. For a single-owner box the
model is our own trusted config artifact, so this slice pins IDENTITY and does
NOT claim content-pinned execution:

- The registry's startup digest is TAMPER-EVIDENCE (logged, surfaced by
  `nexus doctor`), NOT an execution guarantee.
- `policyHash` and the launch `Attestation` carry, per input, `{ID,
  normalizedDest, inode-identity (dev,ino), startup-digest}` — sorted,
  canonical. Two policies that differ in an input differ in identity. The
  attestation explicitly names this as INPUT-IDENTITY pinning, never "these
  exact bytes executed".
- Residual, stated: a same-uid in-place rewrite of the very inode between
  startup and a launch is not closed; it is a single-owner trusted-artifact
  ceiling, honestly scoped. A future memfd-seal variant can upgrade to
  content-pinned execution if a multi-user deployment ever needs it.

### Dest normalization (r3 codex #2)
`filepath.Clean(Dest) == Dest`, absolute, a direct single-segment child of the
reserved `/inputs` dir. Reject equality with — or being an ancestor/descendant
of — ANY built-in mount (`/tmp`, `/work`, `/nexus-target`, every closure dest)
AFTER normalization (closes `/inputs/../nexus-target` shadowing).

### Launch (bind the held fd)
- Re-`fstat` each HELD fd; if inode-identity/size differs from the startup
  record → refuse fail-closed (shape identical to the closure refusal at
  `sandbox.go:319-323`).
- Bind read-only from the inherited fd: prefer bwrap `--ro-bind-fd <N> <Dest>`
  where available (this host's bwrap has it), else `--ro-bind /proc/self/fd/<N>
  <Dest>`; the fd is passed via `ExtraFiles` exactly as the closure's
  `--ro-bind-data` fds already are (`probe.go:642-647`). The original pathname
  is never re-opened at launch.
- Nothing else in the host FS becomes visible.

## Detectors (RED before, GREEN after; anchored to the contract)
- GRANTED-READABLE: a registered fixture is readable at `Dest` inside the
  sandbox and returns the startup bytes.
- NEIGHBOR-INVISIBLE: an undeclared sibling in the same host dir is NOT
  openable inside the sandbox.
- SWAP-DEFEATED: replace the source PATHNAME with different bytes after startup
  → the sandbox still reads the ORIGINAL held inode. RED against a
  re-`--ro-bind <pathname>` implementation.
- NO-FD-LEAK: N sequential launches referencing the same input keep the daemon
  open-fd count flat (proves the registry, not per-call open). RED against a
  per-call-open implementation.
- POLICY-IDENTITY: two Specs differing only in an input produce DIFFERENT
  `policyHash` and DIFFERENT attestation digests.
- DEST-COLLISION: `/inputs/../nexus-target`, a `/work` descendant, and a
  non-`/inputs` dest each fail with a typed error.
- REJECT-AT-STARTUP: a symlink source, device node, FIFO, `nlink>1`,
  group-writable, and over-MaxSize input each fail registry construction.
- EMPTY-UNCHANGED: `Inputs == nil` yields byte-identical bwrap argv to the
  pre-slice build.

## Non-goals
- Content-pinned execution / sealed-memfd inputs (a later variant if a
  multi-user deployment needs it; identity-pinning is enough single-owner).
- Writable extra binds; directory/glob grants.
- Aggregate disk / file-count quotas — existing S6.2 **P2.2 ResourceBudget**
  (`HARNESS-SPEC.md:1481-1484`), a separate slice.
- Dynamic/runtime grant activation (sealed at startup, per HARDQ B9).

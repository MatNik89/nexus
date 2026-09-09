# Independent research: coding-runner substrate (Slice 0)

Reviewer: kilo. Grounded in the actual code at commit `1f33da2` (`main`).

## Q1 — Slice 0 does NOT need S7 durability; it needs non-durable grants

**Conclusion: no durable lifecycle for Slice 0.** The durable machinery is a Slice 3
(symedit multi-file Apply) concern, not a coding-runner prerequisite. Reading the S7
state-machine directly:

- `s7.Policy.Durable` (`internal/kernel/s7/s7.go:120`) is what turns an operation into
  "every transition is also journaled as an `s7.*` event" (`:20`). `Begin` refuses a
  Durable policy when the authority has no journal (`:364,377`); `Consume` requires
  exactly one companion (`:484`), `Report` requires a non-nil builder (`:531`), and
  `Reconcile` requires a non-nil builder (`:643`). This whole companion/journal/Reconcile
  shape exists to answer **"after a crash, which side of the effect actually happened?"**
- `EffectPath.RunTool` today drives the **non-durable** path only: `loop.go` issues
  `l.grants.Issue(op, target)` (`:317`), and `s7.Issue` is `Begin(PolicyTool)+Next`
  (`s7.go:391-401`) — `PolicyTool` is non-durable, one attempt, no companion.

`go list` / `go build` / `go test` are **read-only and idempotent from NEXUS's own
durable state**: the live workspace is never mutated (the coding-run operates on a private
snapshot, Q3), so there is no UNKNOWN-outcome-on-crash to reconcile — a crash mid-`go test`
loses only a disposable run, which a retry recreates byte-for-byte. That is the exact
opposite of Slice 3's `workspace.Apply`, which writes the *live* tree and therefore needs
the sealed before/after bundle + `Reconcile(false/true)`.

So Slice 0 needs **S7 governance, not S7 durability**: a `Policy{Durable:false}` grant
per physical subprocess attempt (`go list`, `go build`, `go test`), for the retry /
deadline / cancel ownership invariant 2 demands — but no journal events, no companion, no
`Reconcile`. The not-yet-built `EffectPath.RunDurableTool` should be scoped to Slice 3 only;
building it for Slice 0 would be over-engineering.

## Q2 — toolchain closure: the three paths nest, and the identity gap is acceptable

**Conclusion: RO-bind `GOMODCACHE`, not just `GOTOOLDIR`; directory-identity pin is the
load-bearing defense, per-executable hashing is a cheap optional addition.**

Measured on this host (`go env`):

```
GOROOT     = /home/matej/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.linux-arm64
GOTOOLDIR  = …/toolchain@v0.0.1-go1.26.6.linux-arm64/pkg/tool/linux_arm64
GOMODCACHE = /home/matej/go/pkg/mod
```

`GOTOOLDIR ⊂ GOROOT ⊂ GOMODCACHE` — the Go toolchain is *itself a module* in the module
cache (the `toolchain go1.26.6` directive downloaded it there). So a **single read-only
bind of `GOMODCACHE`** gives `go build` everything it needs (stdlib source under GOROOT,
tool binaries under GOTOOLDIR, dependency source in the module cache) with one
`guardROBind` + one `PinROBindIdentity` call — simpler than the plan's phrasing
("GOTOOLDIR via `go env GOTOOLDIR`") implies, and no separate GOROOT bind.

The identity gap is the right trade: `ExtraROBindIdentities` pins a bound directory's
**device+inode** (`probe.go:587-635,806-832`), which catches a directory *swap* and a
*symlink retarget* — the two attacks that change which tree is exposed — but not an
**in-place edit of a file within an unchanged directory**. For `GOTOOLDIR` specifically
that matters *more* than for ordinary data, because the tool binaries **execute**. The
cheapest correct design is two layers:

1. **Directory pin (load-bearing)**: `PinROBindIdentity(GOMODCACHE)` at coding-run start,
   folded into the policy hash, re-verified by `Prepare` at `Launch` (`probe.go:823-832`).
   This catches the swap/retarget that would substitute an entire malicious toolchain.
2. **Optional per-executable hash (cheap, defense-in-depth)**: hash the ~6 executables
   (`$GOROOT/bin/go`, plus `compile`/`link`/`asm`/`cgo`/`vet` under `GOTOOLDIR`) once at
   coding-run start, and refuse if any differs from the value captured when the toolchain
   was last pinned. This closes the in-place-edit gap for the executables specifically, at
   a bounded cost (6 file hashes, not a tree walk).

"Re-verified before each exec" is **overkill**: a mid-run in-place edit requires a
concurrent *host* write to an operator-owned, read-only-in-sandbox directory — out of the
coding-run threat model (the coding-run cannot write its own read-only toolchain bind).
Hash once at run start; the directory pin is the thing that must hold for the whole run.

## Q3 — snapshot: reuse `guardWorkDir`'s disposable-roots model + `filepath.WalkDir`

**Conclusion: a fresh temp dir per run, a running tree digest over a sorted (relpath,
content) stream, `WalkDir` + reject non-regular/non-dir, caps enforced during the copy.
NOT `sealedstore`.**

Existing pieces to reuse rather than reinvent:

- `guardWorkDir` (`internal/preflight/probe/probe.go:510-525`) already enforces the
  disposable-root + ownership + `0o022` discipline for a RW area. The snapshot's location
  should follow the same shape — a `MkdirTemp` under `os.TempDir()` / `XDG_RUNTIME_DIR`
  (`allowedWorkRoots`, `probe.go:495-505`), 0700, per run, `defer RemoveAll`.
- `pathx.EnsureDir` (`internal/foundation/pathx/pathx.go:83`) is the existing
  traversal-safe mkdir; the snapshot copy should reject anything that isn't a regular file
  or directory via `entry.Type()` (so symlink/device/socket/fifo are refused at the source,
  before any bytes cross), and enforce the file-count and byte-size caps inline.
- The tree digest is a single `sha256` over `(len(relpath):relpath + len(content):content)`
  for each file, with `relpath` collected in a stable sorted order (`io/fs.WalkDir` +
  `sort`), so the snapshot's identity is order-independent and comparable before/after the
  run (to detect undeclared mutation, invariant 7's anti-race). `atomicwrite`
  (`internal/foundation/atomicwrite/atomicwrite.go:20-40`) is the write pattern for any
  *post-run* artifact, but the snapshot copy itself is a plain copy into the disposable
  temp dir — there's no crash-consistency requirement on a disposable copy.

`sealedstore` is the wrong home: it is content-addressed *durable* storage for immutable
bytes the journal references (`sealedstore.go:1-12`). The snapshot is a **disposable RW
working copy**, recreated per run, deleted after — it should never be sealed or referenced
durably.

## Q4 — caller shape: `sandbox.Backend` directly + non-durable S7, NOT exectool/EffectPath

**Conclusion: agree — `sandbox.Backend`'s `Probe → Compile → Launch → Attest` is the right
primitive, wrapped in a non-durable S7 grant, with the runner emitting its own journal
event. Do NOT route through `EffectPath`/PEP.**

- `internal/exectool` is confirmed wrong: `New` seeds `DecisionAsk` (`exectool.go:72`) and
  `Launch` takes `contracts.ToolCall` (`:88`) — it is the model-visible, human-approval
  tool path. The coding-runner is an **internal initiative** with no per-call approver, so
  the ASK/ALLOW/DENY PEP (`effectpath.go:17-29`) has nothing to decide and adds no value.
- The audit/journal-consistency reason to *not* reuse PEP is actually served by the runner
  emitting its **own closed event vocabulary** (a `coding.run` event with the outcome, the
  snapshot digest, the S7 operation/attempt ids, the toolchain pin digests), mirroring how
  `channel`/`s7` own their event types — not by borrowing the tool-call event shape (which
  is for model-visible tools). Profile scoping is achieved by constructing the runner with
  the profile context, like every other kernel component.
- S7 governance remains required (invariant 2: every dispatched effect attempt needs a
  grant), but as a **non-durable** grant around the `sandbox.Backend` launch (Q1): the
  runner calls `Begin(op, target, Policy{Durable:false}) → Next → sandbox.Compile/Launch →
  Report(Succeeded/Failed)` directly against `internal/kernel/s7`, exactly as `loop.go`
  does for tool attempts, not through `EffectPath`.

**Recommended boundary**: `internal/coding/runner`, owning (a) snapshot creation + tree
digest, (b) `GOMODCACHE`/toolchain resolution + `PinROBindIdentity`, (c) `sandbox.Backend`
invocation, (d) the non-durable S7 grant around each physical attempt, and (e) the
`coding.run` journal event for the outcome. `internal/coding/impact` (already built) stays
the pure TIA graph and receives the `go list` stdout the runner captures — the runner is the
only place a subprocess is launched, matching invariant 2's split.

## Summary of independent deltas from the plan text

1. **Drop `RunDurableTool` from Slice 0's critical path.** The coding-runner uses
   non-durable S7 grants; durability is Slice 3's `workspace.Apply` only.
2. **RO-bind `GOMODCACHE`, not `GOTOOLDIR`** — the nesting makes a single bind sufficient;
   per-executable hashing is a cheap optional layer, "re-verify per exec" is overkill.
3. **Snapshot is a disposable temp dir, not sealedstore**, reusing `guardWorkDir`'s roots +
   `WalkDir` + a sorted content digest.
4. **Caller shape is `sandbox.Backend` + non-durable S7 directly, with the runner's own
   `coding.run` journal event** — never `exectool`/`EffectPath`/PEP.

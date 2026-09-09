# Plan-review round 1 — Slice 0 concrete design (`PLAN-CODING-TRIO.md` §Slice 0 + "concrete design, round 1")

- **Reviewed**: `16eef82` on `main` (the Slice 0 concrete-design section)
- **Verdict**: PASS — the design's shape is sound; the weakest link is a genuine but
  bounded completeness gap in §2's executable pinning (below), not a wrong architecture.

## Checklist reasoning

### 1. §1 — no S7 durability for Slice 0: HOLDS

Re-derived from the state machine, not the citation. `s7.Policy.Durable` (`s7.go:120`) is
what gates journal-binding (`Begin` requires `a.j != nil` only for Durable, `:363-365`) and
forces a `Companion`/`Builder` on every `Consume`/`Report`/`Reconcile` (`:484,:531,:643`).
That whole journal/companion/`Reconcile` shape exists to answer exactly one question: "after
a crash, did the effect actually happen?" — which only arises when an effect could have
touched a **live, durable** target. Slice 0's `go list`/`go build`/`go test` run entirely in a
disposable snapshot; the live workspace is never written; GOCACHE/GOTMPDIR point *inside* the
sandbox WorkDir (§2), so even the compiler's own writes are disposable. A crash mid-run
therefore has no UNKNOWN outcome to reconcile — discarding the snapshot and re-running is
byte-for-byte correct. I can find no missed crash-recovery scenario. The non-durable grant
(`Begin(Durable:false)→Next→Consume→Report`) is exactly the governance these need; `loop.go:317`
already proves the shape via `s7.Issue` (= `Begin(PolicyTool)+Next`, `s7.go:391-401`). §1 is
correct and does not contradict the plan's build-order split (durability stays Slice 3's).

### 2. §2 — toolchain pinning: the "FULL executable set" is NOT full — **gopls is missing**

This is the weakest link and it is a real gap, the same class as the already-folded
`bin/go`/`bin/gofmt` omission. §2 pins `GOTOOLDIR` entries + `$GOROOT/bin/go` +
`$GOROOT/bin/gofmt`, and RO-binds `GOROOT` + `GOMODCACHE`. But the Slice 0 design also
requires a `gopls` stdio-JSON-RPC session ("DECIDED now", and "gopls runs through Slice 0's
pinned session") — and `gopls` is a **separate binary**, not part of the toolchain:

- `go env GOBIN` is empty, `GOPATH=/home/matej/go`, so `gopls` (via `go install`) lands at
  `$GOPATH/bin/gopls` — which is **neither** under `GOROOT`
  (`/home/matej/go/pkg/mod/golang.org/toolchain@…/`) **nor** under `GOMODCACHE`
  (`/home/matej/go/pkg/mod`). So the dual RO-bind does not cover it.
- It is also not in the content-hash set (`GOTOOLDIR` + `bin/go` + `bin/gofmt`), so it is not
  identity/content-pinned.
- It is not installed on this host at all (`$GOPATH/bin/gopls` absent; `command -v gopls`
  empty), so the plan has an unstated prerequisite ("install `gopls` and resolve its binary
  path via `go env GOBIN`/`GOPATH/bin`, pin it like `bin/go`").

The gap is *not* the same as the "two Go installs" finding already folded (that's about
GOROOT layout); `gopls` is a distinct executable the runner execs for the LSP session, and
§2's claim "the FULL executable set" is therefore literally false. Fix: add `gopls` (its
resolved binary path) to both the RO-bind set and the per-executable content-hash set, and
state the install-and-resolve prerequisite. Severity: MEDIUM — bounded, same class as the
just-folded `bin/gofmt` gap, and the plan is explicitly "round 1 … codex findings folded
incrementally", so this is a completeness item to fold, not a wrong architecture.

### 3. §3 — snapshot: sound; one TOCTOU mechanism to pin down

The `os.MkdirTemp(os.TempDir())` choice satisfies `guardWorkDir`/`allowedWorkRoots`
(`probe.go:495-525`); `filepath.WalkDir` + reject non-regular/non-dir + reject symlink +
file-count/byte caps + a sorted `relpath‖sha256(bytes)` digest is the right minimal shape
(~50 LOC stdlib). Two notes, neither FAIL-worthy:

- **TOCTOU**: "reject symlinks outright" must be implemented with `O_NOFOLLOW` (or a
  descriptor-relative read), not a `Lstat`-then-`os.ReadFile` sequence — otherwise a symlink
  swapped between the walk and the copy would be followed and its target's bytes copied into
  the snapshot. Same class as the sealedstore `Get` fix already landed (CODE1/CODE2); the plan
  says "reject" but should say "reject via `O_NOFOLLOW`".
- **Hardlinks** (`nlink>1`) are not rejected; a hardlink to a host secret would be copied in
  by bytes. Low risk (requires the owner to hardlink their own secret into their repo; the
  coding-run's output is redacted per invariant 7), but worth one sentence.

### 4. §4 — bypassing EffectPath: AGREE, it loses no real governance

Argued from first principles, independent of the citations. `EffectPath`/PEP provides five
things, and none applies to an internal-initiative coding-run: (a) S6.0 ASK≠ALLOW — there is
no human to ask per attempt, and "is the coding capability enabled at all" is a
capability-activation question, correctly routed to the HARDQ B9 fail-closed-`Resolve`
pattern; (b) S6.9 order-only middleware — that is for tool *effects*, not internal
computation; (c) E9 UNKNOWN/reconcile — the run is read-only/idempotent (see §1), so no
UNKNOWN outcome exists; (d) the tool-call event shape — the runner emits its own closed
`coding.run` event (outcome, snapshot digest, S7 ids, pin digests) via `journal.Append`
(`journal.go:593` already accepts arbitrary typed payloads), which is the *correct* audit
record, not a borrowed tool-call event; (e) profile scoping — achieved by construction.

The one thing that MUST remain and does: the **sandbox** is the security boundary for the
*untrusted test binary* the run execs — the closure, the generated-test-binary-only-inside-tree
rule, network-deny, seccomp. §4 is explicit that bypassing EffectPath does not bypass S7, but
should also be explicit that the sandbox (not S7) is the load-bearing security boundary for
that untrusted child, and S7 is only retry/deadline/cancel governance. That's a wording
clarification, not a reason to keep EffectPath.

One concrete reason to *keep* EffectPath would be: if a future coding operation is
**model-initiated with per-call approval semantics** (e.g. the model explicitly requesting a
destructive edit that a human should approve). That is Slice 3/4's `workspace.Apply` concern,
not Slice 0's read-only runner — so it does not argue against §4 for Slice 0 specifically. I
therefore agree with the revision, with the sandbox-boundary wording note.

### 5. §5 — package boundary: sensible

`internal/coding/runner` owning snapshot + toolchain/gopls resolution-pinning + the
`sandbox.Backend` call + bounded output capture + the `coding.run` journal event is a clean
cohesion; `internal/coding/impact` stays pure (consumes the runner's captured `go list`
stdout); Slice 1's evidence consumes the runner's captured `go test -json`. Capture-bounded,
parse-elsewhere is the right split. Nothing belongs elsewhere.

## Weakest link (named, per discipline)

**§2's "FULL executable set" misses `gopls`** — the LSP server binary at `$GOPATH/bin/gopls`
(neither RO-bound nor content-hash-pinned, and not installed), which the Slice 0 gopls-session
transport must exec. Fold it into the pin set + state the install prerequisite, exactly as the
`bin/go`/`bin/gofmt` omission was folded one commit ago.

VERDICT: PASS

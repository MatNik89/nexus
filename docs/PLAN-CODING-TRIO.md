# PLAN v2: coding-agent trio (TIA / symbol-edit / proof-of-done)

Repo's own deferred ledger (`docs/tasks-P0.md:410`, "coding trio TIA/symedit/deep evidence
(P1)") names this as the next P1 priority after audit-hardening. NEXUS currently has ZERO
coding-agent tooling — `internal/exectool` is a sandboxed shell-exec primitive from P0,
nothing symbol-aware, nothing test-impact-aware, nothing that proves a coding task
actually completed beyond a worker's own prose claim. This is greenfield for the Go
codebase. A related Python tool ("symedit") was built for an older, different Python
harness (NEXUSv2) — not reusable code, but three of its lessons are load-bearing (below).

**v2 change record:** plan-review round 1 (codex FAIL 5 findings — 4 HIGH independently
re-verified against the actual code before folding, all confirmed real; kilo PASS with 4
notes; agy PASS-with-findings overlapping kilo+codex's smaller points). Folded: a new
Slice 0 (coding-runner substrate) inserted before evidence, because codex's HIGH #2 is
verified true — `internal/exectool`'s `Spec{Target,Args,WorkDir,Timeout}`
(`internal/sandbox/sandbox.go:33`) and its fresh-empty-`MkdirTemp`-per-call workdir
(`internal/exectool/exectool.go:116`) genuinely cannot host a real source checkout for
`go test`, an LSP session for `gopls`, or a governed `go list` call — none of slices 1-3
are buildable against the CURRENT exec substrate as originally written. Invariant 2
tightened (HIGH #1: an in-process `workspace.Apply` is still an effect attempt and needs
an S7 grant, not just subprocess calls). Invariant 3/Slice 3 expanded with concrete
`WorkspaceEdit`-application and symlink-defense mechanics (HIGH #3). Invariant 4 replaced
with a durable transaction protocol (HIGH #4). Slice 2 (TIA) given explicit change-mapping
inputs (MEDIUM). Terminology fixed: "fail-open" → "fail-safe toward closure" throughout
(kilo Note 2 + agy Finding 5, independently flagged by both).

## Research basis (four independent threads, each with primary-source citations)

1. Claude-side fork research (WebSearch-driven).
2. codex (gpt-5.6-sol) — deepest: read the actual old `/home/matej/NEXUSv2/core/symedit.py`
   and its test suite for real lessons, not just concept; proposed the most complete
   package split; explicit build order and fail-closed rule list; round-1 plan review
   found the exec-substrate gap (HIGH #2) that the other three research/review threads
   all missed.
3. kilo (DeepSeek V4 Pro) — verified `x/tools/refactor/rename`'s obsolescence notice
   directly; strongest citation discipline (marked unverified claims as negatives).
4. agy (Gemini) — most implementation-eager (jumped to concrete type signatures); useful
   as a completeness check but its "everything goes through S7" claim is the one point
   the other three threads disagree with and disprove (see Cross-slice invariant 2).

All four independently converged on:
- `golang.org/x/tools/refactor/rename` (and `gorename`) is **dead** — its own docs say it
  hasn't worked since Go modules and to use `gopls` instead. Never revive it.
  ([pkg.go.dev/golang.org/x/tools/refactor/rename](https://pkg.go.dev/golang.org/x/tools/refactor/rename))
- TIA belongs at **package granularity via the Go import graph** (`go/packages`,
  reverse-dependency walk — the same shape as Bazel's `rdeps(universe, changed)` query
  ([bazel.build/query/guide](https://bazel.build/query/guide)) and Microsoft's Azure
  DevOps TIA safe-fallback rule
  ([learn.microsoft.com/azure/devops/pipelines/test/test-impact-analysis](https://learn.microsoft.com/en-us/azure/devops/pipelines/test/test-impact-analysis))),
  **not** line-level coverage instrumentation (Datadog's approach — needs a baseline that
  can silently drift stale) and **not** ML predictive selection (Meta's approach — needs a
  large historical-failure dataset that doesn't exist for a single-user assistant).
- Mainstream coding agents (Aider, Cursor, Cline, OpenHands/SWE-agent) mostly do **not**
  do TIA or true symbol-edit today — Aider's tree-sitter repo-map is a **context-selection**
  tool (what to show the model), not an edit mechanism; it still edits via text
  search/replace blocks. `gopls` (LSP `workspace/executeCommand` rename, or the `gopls`
  CLI) is the only maintained, type-safe Go rename engine — built on the same public
  `go/ast` + `go/types` + `go/packages` stack NEXUS would otherwise have to reimplement.
- Proof-of-done should **not** be a new file-based system (in-toto link files, SLSA
  provenance bundles, sigstore signing) — that's supply-chain-grade ceremony
  disproportionate to a single-user local assistant. It should be a **content-addressed,
  structured manifest folded through the existing journal** (same shape as `s7.*` and
  `channel.*` events already are) — NEXUS's append-only, hash-chained journal already IS
  the tamper-evident substrate in-toto/SLSA bolt on externally. Steal SWE-bench's
  `FAIL_TO_PASS`/`PASS_TO_PASS` shape (the completion artifact names the *specific tests*,
  never a prose "all tests pass" claim —
  [github.com/SWE-bench/SWE-bench/blob/main/docs/reference/harness.md](https://github.com/SWE-bench/SWE-bench/blob/main/docs/reference/harness.md))
  and in-toto's typed-Statement-per-step discipline (bind subject digest + typed predicate,
  never free text — [github.com/in-toto/attestation](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md)).
- A caution from an agentic-trajectory study (AgentLens/"Lucky Pass",
  [arxiv.org/html/2605.12925v1](https://arxiv.org/html/2605.12925v1)): ~10.7% of
  SWE-agent trajectories that "pass tests" are gamed — blind retries, missing
  verification, or verification happening out of causal order relative to the edit. The
  proof must bind **ordering** (RED observed → edit applied → GREEN observed, in that
  order, each with a preimage/postimage digest), not just a final boolean.
- Lessons from the OLD Python symedit (architecture only, not code —
  `/home/matej/NEXUSv2/core/symedit.py`):
  - No language server available → **honest typed refusal**, never a silent textual-rename
    fallback (`symedit.py:194`).
  - Authorization must cover the **complete dynamic write-set** a `WorkspaceEdit` touches,
    not just the file the caller named — a real regression class was a multi-line
    whole-file `WorkspaceEdit` silently mis-applied by a naive single-line patcher, and a
    real security finding was a symlink-aliased write-target bypassing a lexical-path gate
    (`symedit.py:223`; `test_n2_6_mutation_preflight.py:201` — an existing test that
    rejects a cross-file rename authorized for only ONE of its two destination files).
  - These are the two invariants this plan's symedit slice must reproduce for Go.

## Cross-slice invariants (bind every slice)

1. **`internal/exectool` remains the sole physical-process-launch owner** — but see
   Slice 0: it needs new capability first (staged workdir input, closed toolchain
   child-closure), not just reuse as-is. Nothing in `internal/coding/*` calls `os/exec`
   directly.
2. **S7 governs every dispatched effect attempt — subprocess OR in-process — never pure
   internal computation that performs no effect.** (Corrected in v2, codex plan-review
   HIGH #1: `EffectPath.RunTool` consumes the S7 grant before execution regardless of
   which executor it dispatches to — `internal/kernel/effectpath/effectpath.go:431,445`;
   S7 authorizes one physical *attempt*, not specifically "a subprocess" —
   `internal/kernel/s7/s7.go:478`. `telegram.call()`'s pattern of consuming the grant
   immediately before the wire, with all pure work done first, is the model —
   `internal/channel/telegram/telegram.go:321,375`.) Concretely:
   - Pure internal calculations that perform NO effect and are not dispatched as a
     tool/effect (e.g. the reverse-import-graph WALK over an already-loaded
     `go/packages` result) need no S7 operation.
   - Every dispatched tool/effect attempt — INCLUDING an in-process
     `workspace.Apply` filesystem mutation, not just external subprocess calls like
     `go list`/`gopls`/`go test` — passes through S6.0/S6.9, consumes an S7 grant before
     its first effect, and reports success/failure/UNKNOWN afterward. Bind the Apply
     grant and its approval to the prepared-plan hash, the complete canonical write set,
     workspace identity, profile, and file identities (exact-intent, `AGENTS.md`).
3. **Two-phase Prepare/Apply for any mutation, never a one-shot write.** Prepare is
   read-only (resolve symbol, ask `gopls` for the `WorkspaceEdit`, hash every preimage,
   format+type-check the staged result, return a canonical preview: symbol identity +
   old/new name + complete path set + patch hash). Apply re-hashes every preimage
   immediately before writing (any drift invalidates the prepared plan) and applies
   through the `internal/coding/workspace` owner (Slice 3), never `gopls` writing files
   itself. **WorkspaceEdit application mechanics** (expanded in v2, codex HIGH #3 + agy
   Finding 4, both independently required this): define a CLOSED accepted subset of LSP
   edit shapes up front and reject everything else — no silent best-effort handling of an
   edit shape not explicitly implemented. Within that closed subset:
   - Resolve every LSP range from its native UTF-16 code-unit position into a byte offset
     against the IMMUTABLE preimage (never against a progressively-mutated copy).
   - Reject overlapping ranges, duplicate ranges, out-of-range offsets, and any
     inconsistency between the edit's declared document version and the preimage's own
     version/hash.
   - Apply all validated edits within one file in DESCENDING byte-offset order so earlier
     edits never invalidate later ones' offsets.
   - Preserve CRLF and final-newline exactly as found — never silently normalize
     line-endings as part of applying an edit.
   - State explicitly which `WorkspaceEdit` shapes v1 supports (e.g. plain `changes`) and
     which are refused outright in v1 (versioned `documentChanges`, annotated edits,
     resource-rename/create/delete operations) rather than left ambiguous.
4. **A rename touches MANY files — `atomicwrite` only makes ONE file old-or-new
   (`internal/foundation/atomicwrite/atomicwrite.go:19`).** `internal/coding/workspace`
   is the multi-file transaction coordinator, and it needs a DURABLE recovery protocol
   surviving a restart, not just "compare digests after" (v2, codex HIGH #4 — the
   original v1 wording promised all-or-none without saying where the transaction intent
   itself survives a crash; `docs/ARCHITECTURE-ESSENTIALS.md:55`'s E4 snapshot-before-
   effect + byte-identical-rollback requirement applies here too):
   1. Before the FIRST write: durably persist the complete canonical write set,
      before/after digests, and sealed before-images (the recovery journal entry, not
      just an in-memory plan).
   2. Record a prepared-transaction identity in the journal.
   3. Replace files individually through `atomicwrite`, one at a time.
   4. On an ordinary mid-transaction failure: restore every already-written file
      byte-for-byte from its sealed before-image.
   5. On restart: classify every file in the transaction's write set as BEFORE (matches
      sealed before-image), AFTER (matches the prepared after-image), or FOREIGN
      (matches neither).
   6. Only roll FORWARD to the exact prepared after-images, or roll BACK to the sealed
      before-images — a FOREIGN file requires refusal + reconciliation, never a silent
      guess or a blind re-run of the rename.
   7. Add crash-injection detectors at every individual-file boundary AND at the
      journal/artifact boundary (mirrors S7's own `SetAppendFault` seam pattern).
   - **Symlink defense** (v2, codex HIGH #3 + kilo Note 1 + agy Finding 3, all three
     independently required this — the exact regression class the old Python symedit
     hit): every candidate path in the write set must be resolved via
     `filepath.EvalSymlinks` against the canonical workspace root before ANY hash is
     computed or verified, and re-resolved/re-verified at Apply time (a symlink could be
     swapped between Prepare and Apply) — refuse the whole operation if any path escapes
     the canonical workspace root or if a parent-directory symlink changes between the
     two phases. Digest sets are keyed by canonical (not lexical) path throughout.
5. **Fail-SAFE toward closure on uncertainty** (v2 terminology fix — "fail-open" was the
   wrong term for behavior that is actually conservative/fail-closed; kilo Note 2 + agy
   Finding 5 both flagged this independently):
   - TIA: incomplete graph, unknown file type, unknown build configuration, or graph
     load error → run the FULL suite, never a guessed subset.
   - symedit: `gopls` unavailable, malformed response, or an edit/resource operation
     outside the closed-subset list in invariant 3 → refuse the whole edit; **never** a
     regex/text-based fallback rename.
   - evidence: a test process failure, truncated event stream, undeclared workspace
     mutation, or a manifest that can't be durably written → no completion verdict at
     all (not a downgraded one).
6. **The worker cannot grade its own output** — `internal/kernel/checker.Grade` already
   rejects `e.Producer == contract.Worker` (`internal/kernel/checker/checker.go:215`, kilo
   plan-review verified this directly). Extend it with coding-specific typed evidence
   kinds rather than inventing a second completion authority.
7. **Redact before journal, as everywhere else** (`docs/ARCHITECTURE-ESSENTIALS.md`):
   raw test/tool output may contain secrets; only bounded, redacted output (or a typed
   reference + digest into a profile-scoped sealed artifact) goes into the journal.
8. RED-before/GREEN-after is about a BEHAVIORAL fix; a semantics-preserving refactor
   (like `RenameSymbol` itself) can legitimately have an EMPTY `FAIL_TO_PASS` set, proven
   instead by its predeclared structural postcondition (the symbol no longer exists under
   its old name; every reference now resolves to the new one) plus full `PASS_TO_PASS`
   preservation (codex plan-review non-blocking note, adopted as an explicit rule so
   Slice 1's evidence schema doesn't wrongly require a non-empty `FAIL_TO_PASS` for
   every coding task).

## Build order

### Slice 0 — coding-runner substrate (NEW in v2, prerequisite for everything else)
Confirmed necessary by re-reading the actual code (codex plan-review HIGH #2, verified):
`internal/exectool`'s `Spec{Target,Args,WorkDir,Timeout}`
(`internal/sandbox/sandbox.go:33`) and its `os.MkdirTemp`-fresh-empty-per-call workdir
with no staged-input mechanism (`internal/exectool/exectool.go:116`) cannot host a real
source checkout for `go test`, an interactive LSP session for `gopls` (exectool is a
one-shot Launch→Wait→collect interface, not a transport —
`internal/exectool/exectool.go:129`), or even a plain governed `go list` call cleanly,
because `go/packages`' default driver shells out to `go list` internally with NO
command-runner injection seam
(`golang.org/x/tools@.../go/packages/packages.go:153`,
`golang.org/x/tools@.../internal/gocommand/invoke.go:209,248`) — that hidden subprocess
would bypass NEXUS's effect path entirely if `packages.Load` were called naively. `go
test`/`go build` also spawn compiler/linker/test-binary children the sandbox's synthetic
closure deliberately excludes today (`docs/ARCHITECTURE-ESSENTIALS.md:131`).
Concretely, before Slice 1 can build anything:
- Route every coding-related command as a typed `ToolCall` through `EffectPath`, never
  directly through raw `exectool` (matches invariant 2's grant-before-effect rule).
- Support a content-addressed staged `/work` directory that CAN be pre-populated with a
  real (or disposable-copy) source tree before launch, not just an empty temp dir.
- Define a pinned, closed Go-toolchain child-process closure (compiler/linker/test
  binaries the toolchain itself spawns) and an explicit network policy for it (deny by
  default; a missing module dependency is a typed refusal or a separately-authorized
  fetch, never a silent network fallback).
- For TIA (Slice 2): call `go list -deps -test -json` EXPLICITLY as a governed
  subprocess and parse its stdout in-process — do NOT call `packages.Load` with its
  default driver, since that hides an ungoverned `go list` invocation inside a
  library call.
- For symedit (Slice 3): choose and specify ONE gopls integration shape up front — either
  a governed ONE-SHOT `gopls` CLI/protocol call that returns a structured edit for a
  single Prepare, or an explicit BOUNDED LSP session with its own lifecycle (start,
  request, response, terminate) — do not leave this implicit; whichever is chosen becomes
  the pinned, doctor-probed integration for Slice 3.

### Slice 1 — proof-of-done / evidence manifest
Slice 1 depends only on EXISTING machinery (journal, `checker`) plus Slice 0's governed
test-runner — it does not depend on TIA or symedit existing (kilo plan-review verified
the dependency graph directly: `PLAN-CODING-TRIO.md:120-187` in v1).
- New closed journal event vocabulary in a package under `internal/coding/evidence`
  (mirrors the `s7.*`/`channel.*` pattern): binds acceptance-contract ID, worker/checker
  identity, profile + workspace canonical path, base commit/tree + preimage digest,
  patch digest + complete changed-file before/after digests, exact executable/toolchain
  digest, argv/cwd/allowlisted-env (never a shell string), S7 operation/attempt IDs,
  start/finish times + exit status, parsed `go test -json` results (FAIL_TO_PASS /
  PASS_TO_PASS shape — allowed empty for a proven-by-postcondition refactor per
  invariant 8), post-run workspace tree digest, explicit proof ceiling + fallback reason,
  deterministic checker verdict.
- The completion event must not race the tested source: hash the candidate tree → run
  tests in a disposable sandboxed copy bound to that hash (via Slice 0's staged-workdir
  substrate) → hash again after → reject the evidence if undeclared mutation occurred in
  between → commit through the journal's single-write-owner actor.
- Extend `internal/kernel/checker` with coding-specific typed evidence kinds; do not
  create a second completion authority.
- RED-capable detector: an evidence manifest missing any required field, or one produced
  out of causal order (GREEN recorded before the RED baseline, or before the edit's
  postimage digest exists), is refused by the checker.

### Slice 2 — TIA (`internal/coding/impact`), shadow mode first
- Change-mapping inputs, made explicit (v2, codex plan-review MEDIUM finding): base and
  candidate graph identities, per-package `Dir` + compiled/source/test/embed file sets,
  module/workspace files, active build tags, `GOOS`/`GOARCH`, and a toolchain+environment
  digest — a deleted or renamed file, or any file the mapping can't classify, MUST trigger
  full-suite selection (it cannot appear in a post-change-only graph at all).
- Governed `go list -deps -test -json` (via Slice 0, not `packages.Load`'s default
  driver) → invert the import graph → transitive closure of dependents of the changed
  packages → union with their test targets.
- Change-set source, made explicit (v2, kilo Note 3): in shadow mode, the source is
  `git diff` against the base commit; once Slice 3 exists, the journal's `coding.edit`
  events become an equally valid source — state which one is active per invocation.
- Ship in **shadow mode first**: always run the full suite AND record what TIA would have
  selected, comparing selected-vs-actual-failures over real runs before ever gating a real
  test cycle on TIA's output alone. This closes the one gap all four research threads
  flagged: no cited evidence that package-level Go TIA has been measured for recall in an
  agentic setting — the honest posture is "iteration accelerator," not "release proof,"
  until NEXUS has its own measured data. Shadow evidence must record the exact
  fallback-to-full-suite cause when it happens.
- Fail-safe rule (invariant 5) applies from day one.

### Slice 3 — symedit (`internal/coding/symedit` + `internal/coding/workspace`)
- Start with **only `RenameSymbol`** (matching topknot: ship the minimal-machinery core,
  not an ambitious API surface) — no arbitrary "safe delete" or AST insertion yet;
  `gopls`'s own reference-finding only covers the active build configuration and can't
  rule out reflection/string-based lookup either, so over-promising safety here is itself
  a correctness risk, not just scope creep.
- `gopls` runs through Slice 0's pinned, doctor-probed integration (disposable staged
  workspace, network denied by default).
- Prepare/Apply per invariant 3 (including the closed edit-shape subset and the UTF-16/
  overlap/CRLF mechanics); `internal/coding/workspace` multi-file coordinator per
  invariant 4 (including the durable recovery protocol and canonical-path symlink
  defense).
- Reproduce the two symedit lessons from the old Python tool: honest refusal with no LSP
  available (never textual fallback); authorization covers the COMPLETE dynamic
  write-set, not just the anchor file — with a RED-capable detector mirroring
  `test_n2_6_mutation_preflight.py`: a rename touching 2 files, authorized for only 1,
  must be refused before any write. Additional RED-capable detectors required (v2, codex
  HIGH #3): multiline whole-file replacement handled correctly; non-BMP UTF-16 columns
  and CRLF preserved; overlapping/out-of-range edits rejected; a path alias escaping the
  workspace rejected; a parent/target symlink swapped between Prepare and Apply rejected.

### Slice 4 — expand symedit's action set, ONE gopls code action at a time
- Only after slice 3 is deployed and dogfooded — each new action (e.g. a specific safe
  delete) gets its own RED-capable conformance test before being added, per invariant 5's
  spirit (don't promise more than what's been proven).
- Do not add tree-sitter, SCIP/Sourcegraph-style indexing infrastructure, ML test
  selection, or full SLSA/sigstore signing "later" speculatively — only if measured use
  in slices 0-3 demonstrates a real gap they'd close (topknot: no unrequested
  abstractions).

## Order + review

Slice 0 → 1 → 2 → 3 (→ 4 opportunistically later, not blocking this plan's closure). Each
slice: RED observed before / GREEN after at the real boundary, one runnable check left
behind, then dispatched for 3-agent adversarial plan-then-code review (codex/kilo/agy via
herdr) until codex+kilo converge PASS (agy supportive, not required alone) — same
discipline as `PLAN-AUDIT-FIXES.md`. Merge to main + autodeploy + push per standing rules
after each slice converges, not batched at the end.

## Status

v2 — plan-review round 1 folded (codex FAIL 5 findings, all 4 HIGH independently
re-verified against the code before folding; kilo PASS + 4 notes folded; agy
PASS-with-findings, overlap folded). Next: dispatch v2 for plan-review round 2.

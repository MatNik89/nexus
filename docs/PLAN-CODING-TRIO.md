# PLAN v3: coding-agent trio (TIA / symbol-edit / proof-of-done)

Repo's own deferred ledger (`docs/tasks-P0.md:410`, "coding trio TIA/symedit/deep evidence
(P1)") names this as the next P1 priority after audit-hardening. NEXUS currently has ZERO
coding-agent tooling — `internal/exectool` is a sandboxed shell-exec primitive from P0,
nothing symbol-aware, nothing test-impact-aware, nothing that proves a coding task
actually completed beyond a worker's own prose claim. This is greenfield for the Go
codebase. A related Python tool ("symedit") was built for an older, different Python
harness (NEXUSv2) — not reusable code, but three of its lessons are load-bearing (below).

**Change record.** v2 folded plan-review round 1 (codex FAIL 5, 4 HIGH re-verified; kilo
PASS+4 notes; agy PASS-with-findings) — added Slice 0, tightened S7 scope, expanded edit
mechanics, replaced the recovery invariant, gave TIA explicit inputs, fixed "fail-open"
terminology. **v3 folds plan-review round 2** (codex FAIL 5 NEW HIGH; kilo PASS+4 notes;
agy PASS+4 notes). One factual disagreement between reviewers was independently checked
against the code before folding: kilo claimed "multi-file `workspace.Apply` fits cleanly
[through the existing companion mechanism]" — checked directly and this is WRONG.
`loop.go:317` calls `l.grants.Issue(op, target)`, which hardcodes `PolicyTool`
(non-durable, `internal/kernel/s7/s7.go:393-395`); `EffectPath.RunTool`'s
`p.grants.Consume(grant)` passes **zero** companions
(`internal/kernel/effectpath/effectpath.go:445`) and every one of its `Report` calls
passes a **nil** builder (`effectpath.go:369,516`). The current tool-dispatch pipeline is
non-durable-only, end to end — codex's HIGH #2 (round 2) is confirmed real; kilo's PASS on
this specific point did not check past "Consume happens before dispatch" into whether a
companion could actually be carried. This is why, on a factual disagreement between two
independent reviewers, the resolution is "read the code," not "average the opinions."

## Research basis (four independent research threads before any review; two review rounds
since)

1. Claude-side fork research (WebSearch-driven).
2. codex (gpt-5.6-sol) — deepest at every stage: read the actual old
   `/home/matej/NEXUSv2/core/symedit.py` and its test suite for real lessons; found the
   exec-substrate gap (round-1 HIGH #2) and the durable-EffectPath gap (round-2 HIGH #2)
   that every other thread and reviewer missed, both independently re-verified true
   against the current code before folding.
3. kilo (DeepSeek V4 Pro) — verified `x/tools/refactor/rename`'s obsolescence directly;
   strong citation discipline; PASSed both review rounds, and its one factual claim that
   turned out wrong (above) is the kind of thing a second independent check exists to
   catch — the process worked as designed, not a reason to discount kilo generally.
4. agy (Gemini) — most implementation-eager; useful completeness checks each round (e.g.
   round 2's `gopls` CLI-vs-stdio-JSON-RPC transport finding, folded below) but its
   original "everything goes through S7" research claim was the one point the other three
   threads disagreed with and disproved (see invariant 2).

All four independently converged on (unchanged from v1/v2, still holds):
- `golang.org/x/tools/refactor/rename` (and `gorename`) is **dead** — its own docs say it
  hasn't worked since Go modules and to use `gopls` instead. Never revive it.
  ([pkg.go.dev/golang.org/x/tools/refactor/rename](https://pkg.go.dev/golang.org/x/tools/refactor/rename))
- TIA belongs at **package granularity via the Go import graph** (`go/packages`,
  reverse-dependency walk — the same shape as Bazel's `rdeps(universe, changed)` query
  ([bazel.build/query/guide](https://bazel.build/query/guide)) and Microsoft's Azure
  DevOps TIA safe-fallback rule
  ([learn.microsoft.com/azure/devops/pipelines/test/test-impact-analysis](https://learn.microsoft.com/en-us/azure/devops/pipelines/test/test-impact-analysis))),
  **not** line-level coverage instrumentation and **not** ML predictive selection.
- `gopls` is the only maintained, type-safe Go rename engine — built on the same public
  `go/ast`+`go/types`+`go/packages` stack NEXUS would otherwise reimplement.
- Proof-of-done should be a **content-addressed, structured manifest folded through the
  existing journal** (same shape as `s7.*`/`channel.*` events) — not a new file-based
  system (in-toto files, SLSA bundles, sigstore signing). Steal SWE-bench's
  `FAIL_TO_PASS`/`PASS_TO_PASS` shape
  ([github.com/SWE-bench/SWE-bench](https://github.com/SWE-bench/SWE-bench/blob/main/docs/reference/harness.md))
  and in-toto's typed-Statement-per-step discipline
  ([github.com/in-toto/attestation](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md)).
- AgentLens/"Lucky Pass" ([arxiv.org/html/2605.12925v1](https://arxiv.org/html/2605.12925v1)):
  ~10.7% of SWE-agent trajectories that "pass tests" are gamed — the proof must bind
  **ordering** (RED → edit → GREEN, each with a digest), not just a final boolean.
- Old Python symedit lessons (architecture only — `/home/matej/NEXUSv2/core/symedit.py`):
  no-LSP → honest typed refusal, never textual fallback (`:194`); authorization must cover
  the **complete dynamic write-set**, not just the named file — the historical failures
  were a multi-line whole-file `WorkspaceEdit` mis-applied by a naive patcher, and a
  symlink-aliased write-target bypassing a lexical-path gate (`:223`;
  `test_n2_6_mutation_preflight.py:201`).

## Cross-slice invariants (bind every slice)

1. **`internal/exectool` remains the sole physical-process-launch owner** — Slice 0 gives
   it the new capability this plan needs (staged workdir input, closed toolchain
   child-closure); nothing in `internal/coding/*` calls `os/exec` directly.
2. **S7 governs every dispatched effect attempt — subprocess OR in-process — never pure
   internal computation that performs no effect** (`EffectPath.RunTool` consumes the
   grant regardless of executor — `effectpath.go:431,445`; S7 authorizes one physical
   *attempt*, not specifically a subprocess — `internal/kernel/s7/s7.go:478`). A pure
   `go/packages`-style graph WALK over already-loaded data needs no S7 operation; every
   dispatched effect — including in-process `workspace.Apply` — does.
   **The current tool pipeline cannot carry this** (v3, codex round-2 HIGH #2, verified
   against the code — see Change record above): `loop.go` only issues non-durable
   `PolicyTool` grants with no companion and a nil `Report` builder throughout
   `EffectPath.RunTool`. `workspace.Apply` needs a genuinely NEW durable lifecycle,
   specified now:
   - A new `PolicyWorkspaceApply` (durable, `MaxAttempts` per its own retry policy — a
     multi-file apply is not "fire once, never retry" like `PolicyTool`).
   - Operation identity derived from `(profile, canonical workspace identity,
     prepared-plan hash)` — exact-intent bound, matching the ASK-grant rule in
     `AGENTS.md`.
   - `Consume` atomically commits `s7.attempt_started` together with a
     `workspace.apply_started` companion IN ONE BATCH (the existing paired-companion
     mechanism `s7.Consume`/`s7.Report` already support for other durable operations —
     see `internal/channel/telegram/telegram.go`'s `registerCommands` for the working
     precedent of exactly this pattern applied to a different owner).
   - `Report`/`Cancel`/`Reconcile` each commit their S7 transition with exactly ONE
     workspace companion — never a nil builder.
   - Either extend `EffectPath` with a second, explicitly durable-effect lifecycle
     method alongside `RunTool` (not overloading `RunTool` itself, which stays the
     non-durable `PolicyTool` path for ordinary tools), or specify an equally explicit
     alternate route that still enforces S6.0/S6.9 and never duplicates S7 ownership.
   - RED-capable detector: injecting a fault into EITHER half of the paired companion
     batch (the S7 half or the `workspace.apply_started` half) must result in ZERO
     filesystem writes (mirrors S7's own `SetAppendFault` seam pattern, already used
     elsewhere in this codebase to prove exactly this class of atomicity).
3. **Two-phase Prepare/Apply for any mutation, never a one-shot write.** Prepare is
   read-only (resolve symbol, ask `gopls` for the `WorkspaceEdit`, hash every preimage,
   format+type-check the staged result, return a canonical preview: symbol identity +
   old/new name + complete path set + patch hash). Apply re-hashes every preimage
   immediately before writing (any drift invalidates the prepared plan) and applies
   through `internal/coding/workspace` (Slice 3), never `gopls` writing files itself.
   **`WorkspaceEdit` shape — DECIDED now, not deferred** (v3, codex round-2 HIGH #4 +
   agy round-2 Note 4, both independently required a decision rather than an open
   "state explicitly which shape" placeholder):
   - v1 supports ONLY plain `WorkspaceEdit.changes` (`map[DocumentURI][]TextEdit]`).
     Everything else — versioned `documentChanges`, annotated edits, resource
     rename/create/delete operations — is refused outright with a typed
     `ErrUnsupportedEditShape`, in v1. This also determines the `gopls` client
     capabilities NEXUS advertises when it initializes the session (Slice 0), so
     `gopls` itself is asked to return only the supported shape.
   - Accept only local `file:` URIs with an empty authority, strictly decoded once
     (reject anything else — a non-local or malformed URI is a refusal, not a
     best-effort resolution attempt).
   - Resolve every LSP range from its native UTF-16 code-unit position into a byte
     offset against the IMMUTABLE preimage (never a progressively-mutated copy).
   - Reject overlapping ranges, duplicate ranges, out-of-range offsets, and any
     inconsistency between the edit's declared document version and the preimage's own
     version/hash.
   - Canonically sort the complete path set and reject it outright if two lexically
     distinct paths resolve to the SAME file identity (device+inode) — a duplicate
     target under two names is refused, not silently deduplicated.
   - Apply all validated edits within one file in DESCENDING byte-offset order.
   - Preserve CRLF and final-newline exactly as found — never silently normalize.
4. **A rename touches MANY files — `atomicwrite` only makes ONE file old-or-new
   (`internal/foundation/atomicwrite/atomicwrite.go:19`).** `internal/coding/workspace`
   is the multi-file transaction coordinator. **The recovery protocol needs a durably
   RECONSTRUCTABLE after-image, not just an after-image digest** (v3, codex round-2
   HIGH #3, verified: a digest alone cannot reconstruct the actual bytes after a crash,
   and re-running `gopls` to regenerate them would violate the no-blind-rerun rule):
   1. Before the FIRST write: persist ONE sealed transaction bundle containing the
      COMPLETE before-image AND after-image BYTES (not just digests) for every file in
      the write set, plus their permissions, canonical identities, and digests. `fsync`
      this bundle to durable storage.
   2. Atomically pair `workspace.apply_started{artifact_ref, digest}` with the S7
      `Consume` companion batch (invariant 2) — the journal entry names the sealed
      bundle by reference+digest, it does not duplicate the bundle's bytes into the
      journal itself.
   3. Replace files individually through `atomicwrite`, one at a time.
   4. On completing ALL per-file replacements: commit a terminal
      `workspace.mutation_committed` event (agy round-2 finding — this is the explicit
      "we finished" marker the recovery classification below depends on).
   5. On an ordinary mid-transaction failure (not a crash): restore every already-written
      file byte-for-byte from the sealed bundle's before-images.
   6. On restart, classify EVERY file in the transaction's write set as BEFORE (matches
      the sealed before-image), AFTER (matches the sealed after-image), or FOREIGN
      (matches neither) — this table is exhaustive:
      - `workspace.mutation_committed` exists → verify every file matches AFTER; any
        mismatch is FOREIGN, not silently accepted.
      - No commit event, all files BEFORE → transaction never effectively started;
        reconcile as "not applied," safe to retry the whole operation fresh.
      - No commit event, mixed BEFORE/AFTER → mid-crash; roll every AFTER-matching file
        back to its sealed before-image (never roll forward without the commit marker).
      - Any file FOREIGN (in any of the above) → refuse; manual reconciliation only,
        never a guess and never a blind re-run.
      - The recovery journal entry itself missing after a crash → SAFE, because step 1
        happens before any write: no journal entry means no write could have started
        (this is the load-bearing "step 1 before step 3" ordering — kilo round-2 verified
        this explicitly).
      - The recovery journal entry present but its referenced sealed bundle missing or
        corrupt → treat as FOREIGN (refuse, manual reconciliation) — a valid journal
        event pointing at a dead artifact is itself an anomaly, not a decodable state.
      - A CORRUPT (not missing) journal entry is already caught by the journal's own
        integrity hash-chain at `Journal.Open` (`internal/kernel/journal/journal.go:307,709`)
        — do not invent a second, parallel fail-open recovery log; the existing substrate
        already fails closed on this.
   7. Crash-injection detectors required, at minimum: fault before the sealed bundle is
      written; fault between the bundle and the journal pairing; fault at every
      individual-file boundary; fault after all files are written but before the
      `mutation_committed` event; a corrupt/missing sealed bundle referenced by a valid
      journal event.
   - **Symlink defense — mutation-time guard, not just a pre-check** (v3, codex round-2
     HIGH #4: `filepath.EvalSymlinks`-then-`rename`-by-pathname alone leaves a TOCTOU
     window — a parent-directory symlink can be swapped between the last check and the
     actual write, since `atomicwrite`'s current writer reopens the directory and renames
     by pathname, `internal/foundation/atomicwrite/atomicwrite.go:19`). Reject any
     symlink path COMPONENT in the write set outright at Prepare (not just the leaf).
     Perform the actual create/rename at Apply time descriptor-relatively — `openat2`
     with `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS` (or an equivalent directory-file-
     descriptor design) so the checked target and the written target are provably the
     SAME kernel object, not just the same lexical path re-resolved a second time.
     Digest sets are keyed by canonical (device+inode-bound) identity throughout, never
     by lexical path.
5. **Fail-SAFE toward closure on uncertainty** ("fail-open" was the wrong term in v1 for
   behavior that is actually conservative/fail-closed — kilo+agy both flagged this
   independently in round 1):
   - TIA: incomplete graph, unknown file type, unknown build configuration, or graph
     load error → run the FULL suite, never a guessed subset.
   - symedit: `gopls` unavailable, malformed response, or an edit/resource operation
     outside invariant 3's closed subset → refuse the whole edit; never a regex/text
     fallback rename.
   - evidence: a test process failure, truncated event stream, undeclared workspace
     mutation, or a manifest that can't be durably written → no completion verdict at
     all (not a downgraded one).
6. **The worker cannot grade its own output** — `internal/kernel/checker.Grade` already
   rejects `e.Producer == contract.Worker` (`checker.go:215`, verified directly). Extend
   it with coding-specific typed evidence kinds; do not invent a second authority.
7. **Redact before journal, as everywhere else** (`docs/ARCHITECTURE-ESSENTIALS.md`):
   raw test/tool output may contain secrets; only bounded, redacted output (or a typed
   reference + digest into a profile-scoped sealed artifact) goes into the journal.
8. RED-before/GREEN-after governs a BEHAVIORAL fix; a semantics-preserving refactor
   (`RenameSymbol` itself) can legitimately have an EMPTY `FAIL_TO_PASS` set, proven
   instead by its predeclared structural postcondition (the old name no longer exists,
   every reference resolves to the new one) plus full `PASS_TO_PASS` preservation.

## Build order

### Slice 0 — coding-runner substrate (prerequisite for everything else)
Verified necessary against the actual code (round 1) and given a concrete, buildable
contract (round 2, folding codex HIGH #1 + agy Notes 2/3): define a closed `CodingRunSpec`
before implementation begins, not just name the requirements.
- **Isolation.** Always construct a PRIVATE snapshot of the workspace after S7
  consumption — the live workspace is never exposed read-write to a coding-run process.
  Define the tree-digest algorithm, the allowed node types (regular files + directories
  only; no device/socket/fifo nodes), the symlink policy (reject, per invariant 4), and
  explicit file-count/byte-size caps for the snapshot.
- **Toolchain pinning.** Pin `GOROOT` host tools and mount the module cache read-only;
  set `CGO_ENABLED=0`, `GOTOOLCHAIN=local`, and disable module-fetch networking by
  default (a missing module dependency is a typed refusal or a separately-authorized
  fetch, never a silent network fallback). Isolate `GOCACHE`/`GOTMPDIR` to a location
  OUTSIDE the snapshot's source tree (agy round-2 Note 3) so the compiler's own cache
  writes during `go test`/`go build` never pollute the post-run workspace tree digest.
- **Child-process closure.** The sandbox-generated test/build binary is explicitly
  allowed to execute, but ONLY from inside the private staged work/build tree — arbitrary
  host executables remain absent, matching the existing closure discipline
  (`docs/ARCHITECTURE-ESSENTIALS.md:131`). The discovery/pinning mechanism for
  `compile`/`link`/`asm`/`cgo`/test-binary children is Slice 0's own hardest design
  surface (kilo round-2 Note 1) — treat it as such, not an afterthought.
- **`gopls` transport — DECIDED now:** a stdio JSON-RPC session
  (`gopls serve` → `initialize` → `textDocument/rename` → `shutdown`), not the `gopls`
  CLI (agy round-2 Note 2, verified: the CLI emits unified text diffs or writes files
  directly with `-w` — it does NOT emit structured `protocol.WorkspaceEdit` JSON, which
  invariant 3's mechanics require). This is Slice 0's integration; Slice 3 consumes it.
- Route every coding-related command as a typed `ToolCall` through `EffectPath`, never
  directly through raw `exectool`.
- For TIA (Slice 2): call `go list -deps -test -json` EXPLICITLY as a governed subprocess
  and parse its stdout in-process — never `packages.Load`'s default driver, which hides
  an ungoverned `go list` invocation inside a library call.
- **Required causal detectors** (codex round-2 HIGH #1): host-workspace immutability (the
  live workspace is never mutated by a coding run); external-symlink non-import (a
  symlink in the snapshot input is rejected, not silently followed); undeclared
  host-child rejection (any subprocess spawned outside the pinned closure is refused);
  toolchain-version swap detection; network denial under the default policy;
  candidate-digest mismatch (the snapshot's measured digest disagrees with its claimed
  one); generated-test-binary execution scoped correctly (runs, but only from inside the
  staged tree).

### Slice 1 — proof-of-done / evidence manifest
Depends only on EXISTING machinery (journal, `checker`) plus Slice 0's governed
test-runner — not on TIA or symedit existing (dependency graph verified independently
twice, round 1 and round 2).
- New closed journal event vocabulary under `internal/coding/evidence` (mirrors
  `s7.*`/`channel.*`): acceptance-contract ID, worker/checker identity, profile +
  workspace canonical path, base commit/tree + preimage digest, patch digest + complete
  changed-file before/after digests, exact executable/toolchain digest,
  argv/cwd/allowlisted-env (never a shell string), S7 operation/attempt IDs,
  start/finish times + exit status, parsed `go test -json` results (FAIL_TO_PASS/
  PASS_TO_PASS shape, empty allowed per invariant 8), post-run workspace tree digest,
  explicit proof ceiling + fallback reason, deterministic checker verdict.
- The completion event must not race the tested source: hash the candidate tree → run
  tests in Slice 0's disposable sandboxed snapshot bound to that hash → hash again after
  → reject the evidence if undeclared mutation occurred in between → commit through the
  journal's single-write-owner actor.
- Extend `internal/kernel/checker` with coding-specific typed evidence kinds.
- RED-capable detector: an evidence manifest missing any required field, or produced out
  of causal order (GREEN before the RED baseline, or before the edit's postimage digest
  exists), is refused by the checker.

### Slice 2 — TIA (`internal/coding/impact`), shadow mode first
- Change-mapping inputs, explicit: base/candidate graph identities, per-package `Dir` +
  compiled/source/test/embed file sets, module/workspace files, active build tags,
  `GOOS`/`GOARCH`, toolchain+environment digest — a deleted/renamed or unclassifiable file
  MUST trigger full-suite selection.
- **Authoritative change source — corrected in v3** (codex round-2 HIGH #5, verified: a
  journal `coding.edit` event can only describe a symedit-mediated mutation — it cannot
  observe user edits, other tools, generated files, or any workspace change after the
  event was written, so it cannot be an equal ALTERNATIVE source to filesystem reality):
  the actual base-versus-candidate filesystem/build-input comparison is the ONLY
  authoritative change source, and it must include untracked-but-build-relevant files
  found in the candidate package graph (a plain `git diff` alone misses these). Journal
  `coding.edit` events are provenance ANNOTATIONS that may be unioned in for
  explanatory/debugging purposes — they never replace the measured comparison. Any
  unexplained tree-digest difference, unsupported node type, submodule, or
  unclassifiable file → full suite.
- Governed `go list -deps -test -json` (Slice 0) → invert the import graph → transitive
  closure of dependents of the changed packages → union with their test targets.
- Ship in **shadow mode first**: always run the full suite AND record what TIA would have
  selected, comparing selected-vs-actual-failures over real runs before ever gating a
  real test cycle on TIA's output alone — no cited evidence exists that package-level Go
  TIA has been measured for recall in an agentic setting; the honest posture is
  "iteration accelerator," not "release proof," until NEXUS has its own measured data.
  Shadow evidence records the exact fallback-to-full-suite cause when it happens.
- Fail-safe rule (invariant 5) applies from day one.

### Slice 3 — symedit (`internal/coding/symedit` + `internal/coding/workspace`)
- Start with **only `RenameSymbol`** (topknot: minimal-machinery core, not an ambitious
  API surface) — `gopls`'s own reference-finding only covers the active build
  configuration and can't rule out reflection/string-based lookup either, so
  over-promising safety here is itself a correctness risk, not just scope creep.
- `gopls` runs through Slice 0's pinned stdio-JSON-RPC session.
- Prepare/Apply per invariant 3 (closed edit-shape subset, UTF-16/overlap/CRLF
  mechanics); `internal/coding/workspace` multi-file coordinator per invariant 4 (durable
  sealed-bundle recovery protocol, descriptor-relative symlink defense).
- Required RED-capable detectors: the two historical symedit regressions (multi-line
  whole-file mis-apply; symlink-aliased write-target — mirroring
  `test_n2_6_mutation_preflight.py`'s two-file-authorized-for-one refusal); plus (v2/v3)
  non-BMP UTF-16 columns and CRLF preserved correctly; overlapping/out-of-range edits
  rejected; a path alias escaping the workspace rejected; a parent/target symlink swapped
  between Prepare and Apply rejected via the descriptor-relative guard, not just a
  path-string re-check.

### Slice 4 — expand symedit's action set, ONE gopls code action at a time
- Only after Slice 3 is deployed and dogfooded — each new action gets its own
  RED-capable conformance test before being added.
- Do not add tree-sitter, SCIP/Sourcegraph-style indexing infrastructure, ML test
  selection, or full SLSA/sigstore signing speculatively — only if measured use in
  slices 0-3 demonstrates a real gap they'd close.

## Order + review

Slice 0 → 1 → 2 → 3 (→ 4 opportunistically later, not blocking this plan's closure). Each
slice: RED observed before / GREEN after at the real boundary, one runnable check left
behind, then dispatched for 3-agent adversarial plan-then-code review (codex/kilo/agy via
herdr) until codex+kilo converge PASS (agy supportive, not required alone). Merge to
main + autodeploy + push per standing rules after each slice converges, not batched.

## Status

v3 — plan-review round 2 folded (codex FAIL 5 NEW HIGH, all re-verified against the code
— including resolving a factual disagreement with kilo's round-2 PASS by reading the code
directly, kilo was wrong on that one point; kilo PASS + 4 notes folded; agy PASS + 4 notes
folded, including the gopls-transport finding that resolved an open question from round 1).
Next: dispatch v3 for plan-review round 3.

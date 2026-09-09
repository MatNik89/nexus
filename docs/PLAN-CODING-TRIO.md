# PLAN v1: coding-agent trio (TIA / symbol-edit / proof-of-done)

Repo's own deferred ledger (`docs/tasks-P0.md`, "coding trio TIA/symedit/deep evidence
(P1)") names this as the next P1 priority after audit-hardening. NEXUS currently has ZERO
coding-agent tooling — `internal/exectool` is a sandboxed shell-exec primitive from P0,
nothing symbol-aware, nothing test-impact-aware, nothing that proves a coding task
actually completed beyond a worker's own prose claim. This is greenfield for the Go
codebase. A related Python tool ("symedit") was built for an older, different Python
harness (NEXUSv2) — not reusable code, but three of its lessons are load-bearing (below).

## Research basis (four independent threads, each with primary-source citations)

1. Claude-side fork research (WebSearch-driven).
2. codex (gpt-5.6-sol) — deepest: read the actual old `/home/matej/NEXUSv2/core/symedit.py`
   and its test suite for real lessons, not just concept; proposed the most complete
   package split; explicit build order and fail-closed rule list.
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

1. **`internal/exectool` remains the sole physical-process-launch owner.** Nothing in
   `internal/coding/*` calls `os/exec` directly — `go list`, `gopls`, and `go test` all go
   through the existing sandboxed exec path (structured argv, executable allowlist,
   sandbox attestation, bounded redacted observations — `internal/exectool/exectool.go`).
2. **S7 governs physical subprocess attempts, not in-memory graph computation** (the
   converged position across codex+kilo, against agy's blanket "everything is an S7
   attempt" claim and against the fork's blanket "nothing needs S7" claim — both
   over-generalized). Concretely:
   - A pure `go/packages` graph load + reverse-dependency walk that resolves to **zero**
     external subprocess calls: no S7 grant.
   - `go/packages`' default driver DOES shell out to `go list -json` under the hood —
     THAT specific subprocess call is a physical attempt and goes through
     `exectool`+S7 like any other, even though the surrounding graph-walk logic is pure.
   - `gopls` requests (LSP `prepare`/`apply`) and `go test` runs are physical, bounded,
     retryable — full S7 `AttemptGrant` lifecycle, same as every other governed call in
     this codebase.
3. **Two-phase Prepare/Apply for any mutation, never a one-shot write** (codex's
   contribution, matching the project's own ASK-grant exact-intent rule in `AGENTS.md`):
   Prepare is read-only (resolve symbol, ask `gopls` for the `WorkspaceEdit`, hash every
   preimage, format+type-check the staged result, return a canonical preview: symbol
   identity + old/new name + complete path set + patch hash). Apply re-hashes every
   preimage immediately before writing (any drift invalidates the prepared plan — the
   same staleness discipline S7's `Consume` already enforces for grants) and applies
   through a dedicated multi-file workspace-mutation owner, not `gopls` writing files
   itself.
4. **`atomicwrite` protects ONE file at a time — a rename touches MANY.** A new
   `internal/coding/workspace` owner is required as the multi-file transaction
   coordinator: apply the complete prepared after-image set or none of it, verify every
   after-image hash, and on a crash mid-write, the affected files are UNKNOWN —
   reconciliation compares each file against the before/after digest sets, it never
   blindly re-runs the rename (same E9 discipline as everywhere else in this codebase).
5. **Fail-open on uncertainty, always toward the SAFER (more thorough / more refused)
   option, never toward silently doing less or accepting less proof:**
   - TIA: incomplete graph, unknown file type, unknown build configuration, or graph
     load error → run the FULL suite, never a guessed subset.
   - symedit: `gopls` unavailable, malformed response, or an edit/resource operation
     outside a small explicitly-supported set → refuse the whole edit; **never** a regex/
     text-based fallback rename.
   - evidence: a test process failure, truncated event stream, undeclared workspace
     mutation, or a manifest that can't be durably written → no completion verdict at
     all (not a downgraded one).
6. **The worker cannot grade its own output** (existing `internal/kernel/checker`
   anti-self-grading contract — extend it with coding-specific typed evidence kinds
   rather than inventing a second completion authority).
7. **Redact before journal, as everywhere else** (`docs/ARCHITECTURE-ESSENTIALS.md`):
   raw test/tool output may contain secrets; only bounded, redacted output (or a typed
   reference + digest into a profile-scoped sealed artifact) goes into the journal.

## Build order (codex's recommendation, adopted — evidence infra must exist before the
other two can prove anything they did)

### Slice 1 — proof-of-done / evidence manifest (build FIRST)
- New closed journal event vocabulary in a package under `internal/coding/evidence`
  (mirrors the `s7.*`/`channel.*` pattern): binds acceptance-contract ID, worker/checker
  identity, profile + workspace canonical path, base commit/tree + preimage digest,
  patch digest + complete changed-file before/after digests, exact executable/toolchain
  digest, argv/cwd/allowlisted-env (never a shell string), S7 operation/attempt IDs,
  start/finish times + exit status, parsed `go test -json` results (FAIL_TO_PASS /
  PASS_TO_PASS shape, not a boolean), post-run workspace tree digest, explicit proof
  ceiling + fallback reason, deterministic checker verdict.
- The completion event must not race the tested source: hash the candidate tree → run
  tests in a disposable sandboxed copy bound to that hash → hash again after → reject the
  evidence if undeclared mutation occurred in between → commit through the journal's
  single-write-owner actor.
- Extend `internal/kernel/checker` with coding-specific typed evidence kinds; do not
  create a second completion authority.
- RED-capable detector: an evidence manifest missing any required field, or one produced
  out of causal order (GREEN recorded before the RED baseline, or before the edit's
  postimage digest exists), is refused by the checker.

### Slice 2 — TIA (`internal/coding/impact`), shadow mode first
- `go/packages` (`NeedDeps|NeedImports`, `Tests=true`) load → invert the import graph →
  transitive closure of dependents of the changed packages → union with their test
  targets.
- Ship in **shadow mode first**: always run the full suite AND record what TIA would have
  selected, comparing selected-vs-actual-failures over real runs before ever gating a real
  test cycle on TIA's output alone. This closes the one gap all four research threads
  flagged: no cited evidence that package-level Go TIA has been measured for recall in an
  agentic setting — the honest posture is "iteration accelerator," not "release proof,"
  until NEXUS has its own measured data.
- Fail-open rule (invariant 5) applies from day one.

### Slice 3 — symedit (`internal/coding/symedit` + `internal/coding/workspace`)
- Start with **only `RenameSymbol`** (codex's explicit scope-down, matching topknot:
  ship the minimal-machinery core, not an ambitious API surface) — no arbitrary
  "safe delete" or AST insertion yet; `gopls`'s own reference-finding only covers the
  active build configuration and can't rule out reflection/string-based lookup either, so
  over-promising safety here is itself a correctness risk, not just scope creep.
- `gopls` runs as a **pinned, doctor-probed subprocess** through the existing sandbox path
  (disposable staged workspace, network denied by default — a missing module must produce
  a typed refusal or a separately-authorized fetch, never a hidden network fallback).
- Prepare/Apply per invariant 3; multi-file coordinator per invariant 4.
- Reproduce the two symedit lessons from the old Python tool (see Research basis): honest
  refusal with no LSP available (never textual fallback); authorization covers the
  COMPLETE dynamic write-set, not just the anchor file — with a RED-capable detector
  mirroring `test_n2_6_mutation_preflight.py`: a rename touching 2 files, authorized for
  only 1, must be refused before any write.

### Slice 4 — expand symedit's action set, ONE gopls code action at a time
- Only after slice 3 is deployed and dogfooded — each new action (e.g. a specific safe
  delete) gets its own RED-capable conformance test before being added, per invariant 5's
  spirit (don't promise more than what's been proven).
- Do not add tree-sitter, SCIP/Sourcegraph-style indexing infrastructure, ML test
  selection, or full SLSA/sigstore signing "later" speculatively — only if measured use
  in slices 1-3 demonstrates a real gap they'd close (topknot: no unrequested
  abstractions).

## Order + review

Slice 1 → 2 → 3 (→ 4 opportunistically later, not blocking this plan's closure). Each
slice: RED observed before / GREEN after at the real boundary, one runnable check left
behind, then dispatched for 3-agent adversarial plan-then-code review (codex/kilo/agy via
herdr) until codex+kilo converge PASS (agy supportive, not required alone) — same
discipline as `PLAN-AUDIT-FIXES.md`. Merge to main + autodeploy + push per standing rules
after each slice converges, not batched at the end.

## Status

v1 — first draft, synthesized from four independent research threads (Claude fork +
codex + kilo + agy), not yet reviewed. Next: dispatch to codex/kilo/agy for adversarial
plan review.

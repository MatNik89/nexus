# PLAN v6: coding-agent trio (TIA / symbol-edit / proof-of-done)

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
**v4 folds plan-review round 3** (codex FAIL 3 NEW HIGH — the finding count is
converging, 5→5→3 across three rounds; kilo PASS+3 notes; agy PASS+3 notes, one
independently corroborating codex's inode/atomicwrite finding). Folded: an exact
`PolicyWorkspaceApply` retry/reconcile contract binding the recovery table's "retry
fresh" language to the real `Reconcile(false)`→`Next`(same op) / `Reconcile(true)`
primitives; a defined profile-scoped sealed-artifact owner (physical location,
permissions, write/fsync/rename protocol, digest verification, retention) for the
transaction bundle, which had no persistence owner in v3; and a corrected identity
model for invariant 4 — "device+inode throughout" literally contradicted atomic
replacement (a rename always changes the inode), fixed to a pinned-parent-directory +
validated-relative-path identity with the pre-edit inode as a precondition check, not
a permanent identity. **v5 folds plan-review round 4** (codex FAIL 2 NEW HIGH — finding
count 5→5→3→2, still converging; kilo PASS+3 notes; agy PASS+3 notes). A SECOND factual
disagreement between codex and kilo was independently checked directly against the
plan's own text (no external code needed this time — an internal-consistency question):
kilo claimed all 6 recovery-table cases "map cleanly" to invariant 2's Reconcile
contract; checked directly and this was WRONG in the same way round 2's disagreement
was — v4's table genuinely omitted a "no commit, all files AFTER" case (the exact crash
window step 7's own detector list already required a test for), and v4's invariant 2
illegally routed an ALREADY-`Report(Succeeded)`-committed transaction through
`Reconcile`, which S7 only permits from `UNKNOWN` (`s7/events.go:478,484`) — a committed
transaction is already SUCCEEDED, not UNKNOWN. Folded: recovery scans are now scoped to
incomplete/non-terminal operations only (a closed transaction is never rescanned, so a
later legitimate edit to the same files can't wrongly reopen it); the missing
no-commit-all-AFTER case is added to the table as its own mid-crash rollback case; and
the sealed-artifact GC/write race codex separately found (an orphan sweep could delete
an artifact in the window between its own fsync and the journal reference that names it)
is closed with an owner-held live pin through the journal-append step plus mark-and-sweep
GC. Two of agy's round-4 notes were also folded (name the store
`internal/foundation/sealedstore`, built in Slice 0 directly; name the durable EffectPath
method `RunDurableTool`). **v6 folds plan-review round 5** (codex FAIL 1 NEW HIGH —
finding count 5→5→3→2→1, still converging; kilo PASS+3 notes; agy PASS+3 notes). codex
found a genuinely subtle self-consistency gap neither reviewer's PASS caught: after a
crash, S7 has already rehydrated the original durable Apply operation to `UNKNOWN`
(the existing durable-rehydration rule) — its grant is dead. The recovery-time rollback
writes (restoring files from the sealed bundle) happen in a NEW process, AFTER that
rehydration, so they are themselves a NEW physical effect attempt with NO grant,
directly violating the plan's own invariant 2 ("every dispatched effect needs an S7
grant"). Folded: a new `PolicyWorkspaceRollback` durable operation, governed through the
same `RunDurableTool`/S6/S7 path as `PolicyWorkspaceApply` itself, that the restart-time
rollback path must go through — the original Apply operation only gets to
`Reconcile(false)`/`Next` again AFTER the rollback operation's own `Reconcile` succeeds.
Also folded two agy notes (nested descriptor-relative path walk; `RunDurableTool`
signature shape).

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
     non-durable `PolicyTool` path for ordinary tools) — agy round-4 weakest point #2
     suggests naming it `EffectPath.RunDurableTool`, passing the S7 companion builder
     directly rather than bifurcating the authorization pipeline, as a reasonable
     starting shape for Slice 3's own review to refine — or specify an equally explicit
     alternate route that still enforces S6.0/S6.9 and never duplicates S7 ownership.
     If the alternate route is chosen, that slice's own review must specify its
     S6.0/S6.9 re-enforcement mechanism explicitly (kilo round-3 Note 3) — a
     slice-level decision, not a plan gap.
   - **Retry/reconcile contract — DECIDED now** (v4, codex round-3 HIGH #1, verified:
     `Reconcile(false)` retries the SAME operation within its existing S7 budget,
     `internal/kernel/s7/events.go:478` — the v3 recovery table said "retry the whole
     operation fresh" without binding that phrase to the actual S7 primitive, leaving
     room for an implementation to mint a new operation, reset the attempt budget, or
     report retryable before rollback was even proven):
     - Exact `PolicyWorkspaceApply` attempt cap, deadline, backoff, and closed failure
       codes are specified in Slice 3's own review (not this plan — that's ordinary
       policy tuning), but the STATE-TRANSITION BINDING is a plan-level invariant:
     - `Report(Succeeded)` atomically pairs with `workspace.mutation_committed`.
     - A FULLY VERIFIED rollback to all-BEFORE may report `FailedRetryable`.
     - A FAILED or INCOMPLETE rollback reports `Unknown` — never retryable, never
       silently retried.
     - **Corrected in v5** (codex round-4 HIGH #1, verified against `s7/events.go:478,484`:
       `Reconcile` is legal ONLY from `UNKNOWN` — a transaction that already committed
       `Report(Succeeded)` is already in S7's SUCCEEDED state, so calling `Reconcile`
       on it is an ILLEGAL transition, not merely redundant. v4's "committed / classified
       all-AFTER" phrasing wrongly treated these as one case):
       - Recovery scans, on every restart, are scoped to INCOMPLETE / NON-TERMINAL
         workspace operations ONLY — a transaction whose `Report(Succeeded)` already
         landed is CLOSED; it is never rescanned, never reconciled, and never compared
         against the current workspace state on a LATER restart (a legitimate
         subsequent edit to the same files must not reopen or FOREIGN-classify an old,
         already-closed transaction).
       - On restart, `workspace.mutation_committed` PRESENT (⟺ `Report(Succeeded)`
         already landed): terminal, closed, out of scope for recovery — verify AFTER
         once as a sanity check if desired, but take no S7 action.
       - On restart, NO commit event, all files classify AFTER (the crash window step
         7's own required detector names — "fault after all files are written but
         before `mutation_committed`" — that v4's classification table omitted listing
         as its own case): this is a MID-CRASH, not a success — roll every file back to
         its sealed before-image, verify all-BEFORE achieved, THEN `Reconcile(false)`,
         and ONLY THEN `Next` on the SAME operation.
       - On restart, no commit event, all-BEFORE or mixed BEFORE/AFTER: identical
         rollback treatment — restore to all-BEFORE, `Reconcile(false)`, `Next` on the
         SAME operation only.
     - **Restart-time rollback itself needs its OWN grant — added in v6** (codex round-5
       HIGH #1, verified: on restart, S7 has already rehydrated the durable RUNNING
       original Apply operation to `UNKNOWN` per the existing durable-rehydration rule
       (`s7/events.go:462` — "durable RUNNING-no-report → UNKNOWN, never re-granted");
       its ORIGINAL grant is dead. The rollback writes above happen in a NEW process,
       AFTER that rehydration — restoring files from the sealed bundle is therefore a
       NEW physical effect attempt with no `AttemptGrant` of its own, directly
       contradicting invariant 2's own "every dispatched effect needs S7 grant" rule.
       The SAME-process rollback in step 5 (an ordinary, non-crash failure, still inside
       the original Apply's own consumed attempt) is unaffected — only the RESTART
       compensation path lacked authority):
       - A new durable, exact-intent `PolicyWorkspaceRollback` operation, run through
         the same `RunDurableTool`/S6/S7 path as `PolicyWorkspaceApply` itself. Its
         identity binds the ORIGINAL operation's identity, profile, workspace identity,
         the sealed-bundle digest, and the desired all-BEFORE target state.
       - The original Apply operation remains `UNKNOWN` throughout the rollback — the
         rollback is a SEPARATE operation, not a resurrection of the dead one.
       - The rollback operation consumes its OWN grant before performing any write.
       - Its own reconciliation predicate is "the complete write set now equals the
         sealed BEFORE state": all-BEFORE achieved → rollback `Reconcile(true)`; a
         mixed BEFORE/AFTER state remaining → rollback `Reconcile(false)` then `Next`
         on that SAME rollback operation (not the original Apply); any FOREIGN file →
         both the rollback and the original Apply operation stay `UNKNOWN`/manual.
       - ONLY after the rollback operation itself succeeds may the ORIGINAL Apply
         operation call `Reconcile(false)` and become eligible for `Next` again.
       - Required detector: crash `Apply` into a mixed state, restart, ABLATE the
         rollback operation's own grant, and prove zero recovery writes occur (the
         restart-compensation path must be just as ungovernable-without-a-grant as any
         other effect in this codebase).
     - Any FOREIGN classification stays `Unknown`/manual reconciliation — it never
       receives a new grant, under any circumstance.
     - Required detector (v5): a successfully closed transaction, followed by a later
       LEGITIMATE edit to one of its files, followed by a restart, must NOT reopen or
       FOREIGN-classify the old transaction.
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
     distinct paths resolve to the SAME PRE-EDIT file identity (device+inode of the
     preimage, checked once at Prepare — see invariant 4's identity model for why this
     is a precondition check, not a permanent identity kept "throughout") — a duplicate
     target under two names is refused, not silently deduplicated.
   - Apply all validated edits within one file in DESCENDING byte-offset order.
   - Preserve CRLF and final-newline exactly as found — never silently normalize.
4. **A rename touches MANY files — `atomicwrite` only makes ONE file old-or-new
   (`internal/foundation/atomicwrite/atomicwrite.go:19`).** `internal/coding/workspace`
   is the multi-file transaction coordinator. **The recovery protocol needs a durably
   RECONSTRUCTABLE after-image, not just an after-image digest** (v3, codex round-2
   HIGH #3, verified: a digest alone cannot reconstruct the actual bytes after a crash,
   and re-running `gopls` to regenerate them would violate the no-blind-rerun rule):
   - **Sealed-artifact owner — DEFINED now, not left to incidental implementation
     choice** (v4, codex round-3 HIGH #2, verified: no artifact-store primitive exists
     anywhere under `internal/` today — grep confirms it; the journal only exposes a
     nullable reference field and always inserts NULL on its append path,
     `internal/kernel/journal/journal.go:43,500` — and this bundle carries COMPLETE
     source bytes, potentially including secrets, as the ONLY rollback material after a
     crash, so it cannot be left implicit the way the round-1 exec-substrate gap
     originally was). One profile-scoped sealed-artifact owner (v5, agy round-4 weakest
     point #1: name it `internal/foundation/sealedstore`, built directly in Slice 0 as a
     foundational primitive — not deferred to Slice 1, avoiding a mid-Slice-1 refactor)
     and reused by evidence (invariant 7) and by this transaction bundle alike:
     - Physical location: content-addressed files (named by their own sha256) under the
       profile's data root, in a directory NEVER exposed to the coding-run sandbox as
       read-write.
     - `0700` directory permissions, `0600` file permissions.
     - Write path: write to a temp name in the same directory, `fsync` the file, atomic
       rename into place, `fsync` the containing directory.
     - All reads and writes are descriptor-relative (no symlink traversal into or out of
       the store).
     - Every recovery use re-verifies the digest before trusting the bytes.
     - The journal references it through exactly ONE canonical field
       (`sealed_payload_ref` — a content digest, not a second competing reference shape).
     - **GC/write race — closed in v5** (codex round-4 HIGH #2, verified: the required
       ordering — persist+fsync the artifact, THEN append the journal reference, THEN
       mutate workspace files — deliberately creates a window where the artifact exists
       with no journal reference yet; a concurrent orphan sweep could delete it in that
       window, after which the journal append durably commits a reference to nothing):
       - `Put` returns an owner-managed live pin/lease, held by the caller through the
         journal-append step.
       - GC and the (`Put` → journal-reference-commit) sequence serialize through the
         artifact owner itself (one owner-held lock around the critical section) — GC
         never runs concurrently with an in-flight `Put`-then-reference-commit.
       - The live pin is released only once the journal reference is durable.
       - On a process crash mid-pin: the in-memory pin simply disappears — this is SAFE
         precisely because no workspace write ever precedes the durable journal
         reference (the existing "step 1 before step 3" ordering already protects this;
         the pin only protects the artifact from GC, not from the crash itself).
       - GC performs mark-and-sweep across ALL retained journal references and all
         currently-active pins in one pass — never per-transaction ad hoc deletion — so
         one artifact referenced by MULTIPLE records (evidence AND a transaction bundle,
         say) is never removed while any reference to it still lives. GC's live-root set
         explicitly includes rehydrated `UNKNOWN` operations and any active
         `PolicyWorkspaceRollback` companion (agy round-6 finding) — an in-progress
         restart recovery must never have its own sealed bundle swept out from under it.
       - Required detectors: cleanup racing the artifact-fsync-to-journal-append window;
         two records sharing one artifact while only one of them ages out (the artifact
         must survive until BOTH are gone).
     - Orphan artifacts (no journal event ever referenced them and no live pin holds
       them, or their referencing
       transaction fully completed and aged out) get a defined retention/cleanup policy
       — bounded storage growth, not "keep forever."
   1. Before the FIRST write: persist ONE sealed transaction bundle (via the owner
      above) containing the COMPLETE before-image AND after-image BYTES (not just
      digests) for every file in the write set, plus their permissions, canonical
      identities, and digests. `fsync` this bundle to durable storage.
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
      - `workspace.mutation_committed` exists → the transaction is CLOSED (`Report(Succeeded)`
        already landed atomically with it — S7 is already SUCCEEDED, not UNKNOWN). Out
        of scope for recovery entirely (v5 correction, see invariant 2): verify AFTER
        as a sanity check if desired, but take no S7 action and never rescan it on a
        LATER restart even if a subsequent legitimate edit changes those same files.
      - No commit event, all files BEFORE → transaction never effectively started;
        `Reconcile(false)` then `Next` the SAME operation, safe to retry fresh.
      - No commit event, ALL files AFTER (v5: this case was missing from v4's table
        despite step 7 already requiring its own crash detector — codex round-4 HIGH #1)
        → mid-crash: every file replaced but the transaction never committed. Roll back
        through the GOVERNED `PolicyWorkspaceRollback` operation (v6, invariant 2 —
        this is a NEW physical write in a NEW process; it needs its own grant, not a
        bare filesystem restore), then, once rollback succeeds, `Reconcile(false)` then
        `Next` the SAME original operation. NEVER treat this as a completed transaction
        merely because every file happens to match AFTER.
      - No commit event, mixed BEFORE/AFTER → mid-crash; roll back through the SAME
        governed `PolicyWorkspaceRollback` operation, then `Reconcile(false)` then
        `Next` the SAME original operation (never roll forward without the commit
        marker).
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
      journal event; (v6) ablating the `PolicyWorkspaceRollback` operation's own grant
      during restart-time recovery proves zero rollback writes occur.
   - **Symlink defense — mutation-time guard, not just a pre-check** (v3, codex round-2
     HIGH #4: `filepath.EvalSymlinks`-then-`rename`-by-pathname alone leaves a TOCTOU
     window — a parent-directory symlink can be swapped between the last check and the
     actual write, since `atomicwrite`'s current writer reopens the directory and renames
     by pathname, `internal/foundation/atomicwrite/atomicwrite.go:19`). Reject any
     symlink path COMPONENT in the write set outright at Prepare (not just the leaf).
     Perform the actual create/rename at Apply time descriptor-relatively — `openat2`
     with `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS` (or an equivalent directory-file-
     descriptor design), against a `internal/coding/workspace`-owned helper (NOT the
     plain `atomicwrite.WriteFile` path-string API — agy round-3 weakest-point #1) so the
     checked target and the written target are provably the SAME kernel object. For a
     nested relative path, walk and open each intermediate directory component
     descriptor-relatively via `openat2` in turn (agy round-5 weakest-point #3) — never
     fall back to a path-string API partway through a deep path just because the
     leading components were already validated.
   - **Identity model — corrected in v4** (codex round-3 HIGH #3: "device+inode identity
     throughout" as originally worded literally contradicts atomic replacement itself —
     `atomicwrite` creates a temp file and renames it OVER the destination
     (`atomicwrite.go:22,47`), so the AFTER file necessarily has a DIFFERENT inode than
     the BEFORE file; a literal reading of "throughout" would make the coordinator
     classify its own successful Apply as FOREIGN). The identity binding is instead:
     - A descriptor-pinned parent-directory (the workspace root's directory file
       descriptor, held open across Prepare→Apply).
     - A validated relative path + basename beneath that pinned root — this pair, not an
       inode, is the PERSISTENT identity a file is tracked by across the transaction.
     - The PRE-EDIT inode is a PRECONDITION check at Apply time (confirms the on-disk
       file is still the exact one Prepare hashed), not a permanent identity held
       "throughout" — an inode change from the Apply's own `renameat2` is EXPECTED and
       fine; an inode change to the pinned PARENT directory, or a change to the tracked
       NAME resolving somewhere else, is refused.
     - Content, mode, and metadata expectations are checked explicitly (digest match),
       independent of inode.
     - Required detector (v4, codex + agy both independently flagged this class): a
       successful Apply, followed by restart classification, correctly recognizes its
       own AFTER state despite the expected inode change from `renameat2` — this must be
       tested alongside the symlink-swap detector, not assumed.
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
  surface (kilo round-2 Note 1) — treat it as such, not an afterthought. Starting point
  (agy round-3 weakest-point #2): query `go env GOTOOLDIR` during Slice 0's own
  probe/initialization to resolve the architecture-specific compiler/linker directory
  (e.g. `GOROOT/pkg/tool/linux_arm64` on this deployment target) and mount it read-only
  into the closure — do not hardcode the path.
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

### Slice 0 — concrete design, round 1 (research: my own + agy independent; codex/kilo
dispatched in parallel, pending — fold into round 2 before implementation)

Verified against the actual code (`internal/kernel/effectpath/effectpath.go`,
`internal/kernel/s7/s7.go`, `internal/exectool/exectool.go`, `internal/preflight/probe`)
and this deployment host's live `go env`, not assumed.

1. **S7 durability — NOT needed for Slice 0 (verified from the state machine, not
   intuition).** `s7.Policy.Durable` gates journal-binding (`Begin` requires `a.j != nil`
   only when `policy.Durable`, s7.go:363-365) and requires a `Companion`/`Builder` on
   every `Consume`/`Report`/`Reconcile`. That machinery exists for operations with a
   genuine UNKNOWN-outcome-on-crash problem: Slice 3's multi-file `workspace.Apply`
   mutates the LIVE workspace, so a crash mid-apply leaves real files in an unknown
   state that must be reconciled before any retry. Slice 0's operations (`go list` for
   TIA, `go build`/`go test` for evidence capture) run entirely inside a PRIVATE,
   DISPOSABLE snapshot — the live workspace is never touched — so a crash mid-run loses
   nothing durable; discarding the snapshot and re-running is the correct recovery,
   identical to any other transient computation. `effectpath.RunTool`'s existing
   non-durable path already reflects this: it hardcodes `Consume` with zero companions
   and `Report` with a nil `Builder` (effectpath.go:446, :372) — the ONLY lifecycle
   method today literally cannot express a companion even if asked. Conclusion:
   Slice 0 needs `s7.PolicyTool`-shaped (non-durable) grants at most, not a new durable
   policy or `EffectPath.RunDurableTool`. That machinery is Slice 3's, not Slice 0's —
   confirms the plan's own existing build-order split, doesn't change it.

2. **Toolchain pinning — concrete mechanism.** On this deployment host (Pi, Kali
   arm64), `go env` shows `GOROOT` and `GOTOOLDIR` BOTH live inside `GOMODCACHE`'s
   downloaded-toolchain path (`GOMODCACHE=/home/matej/go/pkg/mod`, `GOROOT=<GOMODCACHE>/
   golang.org/toolchain@.../`, `GOTOOLDIR=<GOROOT>/pkg/tool/linux_arm64`) — this is
   NOT universal (a plain non-toolchain-managed Go install keeps GOROOT outside
   GOMODCACHE; codex's parallel research found a second `~/.local/go` install on this
   same host as a concrete example of the non-toolchain-managed case) — the runner must
   resolve paths from `go env` output, never hardcode either layout.
   - **Bind BOTH `go env GOROOT` and `go env GOMODCACHE` via `ExtraROBinds`**, always,
     not an either/or decision at implementation time: on a toolchain-managed install
     (this deployment host — kilo independently confirmed `GOTOOLDIR ⊂ GOROOT ⊂
     GOMODCACHE` here, so binding both collapses to one effective bind after
     `Prepare`'s existing canonical-path de-duplication) these nest; on a standalone
     Go install (codex's parallel research independently found exactly this case on
     this same host, a separate `~/.local/go`) they don't, and BOTH binds are needed —
     covers stdlib source and downloaded module dependencies either way, with zero
     runtime branching. Directory-level identity pinning (`ExtraROBindIdentities`,
     just converged) is sufficient here: this is a broad, potentially multi-gigabyte
     tree where full content hashing is unbounded, and the residual gap (an in-place
     file edit within an unchanged directory) is accepted as the caller's own bounded
     risk per that
     primitive's documented scope.
   - **Separately content-hash-pin the FULL executable set, not just `GOTOOLDIR`**
     (its own, narrower check, owned by `internal/coding/runner`, NOT a probe/sandbox
     primitive): `GOTOOLDIR` holds only ~8 compiler/linker/asm/cgo binaries (measured
     ~60MB on this host) — a small, bounded set, and unlike the module cache these
     binaries actually EXECUTE inside the sandbox, making them a higher-value swap
     target. **codex's parallel research found the entry point itself is OUTSIDE
     `GOTOOLDIR`**: `$GOROOT/bin/go` (and `bin/gofmt`) are the binaries the
     coding-runner's own `go build`/`go test` invocation actually execs FIRST — pinning
     only `GOTOOLDIR`'s internal compiler/linker/asm/cgo children while leaving the
     entry-point binary itself unpinned would be a real gap, not a cosmetic one.
     Compute `sha256(sorted "name\x00sha256(bytes)")` over `GOTOOLDIR`'s entries PLUS
     `$GOROOT/bin/go` and `$GOROOT/bin/gofmt` at coding-run start (sub-20ms measured
     for the GOTOOLDIR set alone; two more files is negligible), fold into the run's
     evidence event; this closes the in-place-edit gap `ExtraROBindIdentities`
     deliberately leaves open, specifically for the executables where it matters most.
   - **`gopls` pinning (agy + kilo, plan-review round 1 — reconciled):** `gopls` itself
     is NOT part of `GOROOT`/`GOTOOLDIR` — it typically lives at `$GOPATH/bin/gopls` or
     another user binary directory (confirmed not installed on this deployment host).
     agy's and kilo's notes disagreed on WHEN this matters: agy framed it as "Slice 0
     never execs `gopls`" (deferred to Slice 3); kilo, reading the plan's own text more
     literally ("this is Slice 0's integration; Slice 3 consumes it," line 458/679),
     argued the pinned stdio-JSON-RPC session itself is a Slice 0 deliverable, so
     `gopls` DOES need pinning within Slice 0's own scope. Resolved by re-reading the
     source, not averaging: kilo is right that the session-management INFRASTRUCTURE is
     Slice 0's to build, but that is its OWN, LATER increment within Slice 0 (after
     snapshot + toolchain-pinning + TIA's `go list` launch, which this section covers
     and which never execs `gopls`) — not something the snapshot/toolchain-pinning
     increment itself needs today. When that later `gopls`-session increment is built,
     ITS OWN toolchain-pinning must add the resolved `gopls` binary to the content-hash
     set alongside `GOTOOLDIR`'s entries + `bin/go` + `bin/gofmt`, exactly as this
     section's own set was extended once already (the `bin/go`/`bin/gofmt` fold).
   - `ExtraEnv`: `CGO_ENABLED=0`, `GOTOOLCHAIN=local` (forbid downloading a DIFFERENT
     toolchain over the network — the plan's existing network-denial requirement),
     `GOCACHE`/`GOTMPDIR` pointed at a location INSIDE the sandbox's disposable
     `WorkDir`, never the snapshot's source tree (already required above) and never a
     host path outside the sandbox closure. **Implementation correction, verified by a
     real end-to-end run**: `sandbox.Spec` has exactly ONE `WorkDir` bind point (mapped
     to `/work` inside the sandbox) — the snapshot and the GOCACHE area must therefore
     be laid out as SIBLING subdirectories under that ONE bound root (e.g. `/work/src`
     for the snapshot, `/work/gocache` for the cache), and `GOCACHE`/`GOTMPDIR` must be
     set to their IN-SANDBOX paths (`/work/gocache`), never the HOST paths used to
     create them — the sandboxed process cannot resolve a host path it was never bound
     into. An earlier draft of this implementation set the host path directly and
     failed with `go: creating work dir: stat ...: no such file or directory` on the
     first real run; caught immediately by testing against the actual toolchain, not
     assumed correct from the design alone.
   - **Operational precondition, discovered on this deployment host**: `guardROBind`
     correctly refuses a group/world-writable `ExtraROBinds` target (`probe.go`'s
     `0o022` check) — this deployment host's own `GOMODCACHE`
     (`/home/matej/go/pkg/mod`) was actually `0775` (group-writable, likely from a
     permissive umask during `go mod download`), which made the FIRST real end-to-end
     run fail closed exactly as designed. Fixed by `chmod -R g-w` on the module cache
     (owner's explicit choice, not a design workaround) — a host operator running the
     coding-runner for the first time on a similarly-configured host will hit the same
     refusal and needs the same fix; worth a `nexus doctor` check in a later slice, not
     required for Slice 0's own correctness (the refusal is the security boundary
     working, not a bug).

3. **Private workspace snapshot — concrete mechanism.** `probe.go`'s `guardWorkDir`/
   `allowedWorkRoots` (verified by reading the code) constrain `Spec.WorkDir` to a
   root-sticky temp root or an `XDG_RUNTIME_DIR`-style 0700 user-owned root — a plain
   `os.MkdirTemp(os.TempDir(), "nexus-coding-snap-*")` satisfies this without new
   sandbox-side plumbing. Tree copy + digest: `filepath.WalkDir` the source tree,
   reject any non-regular-file/non-directory node (device/socket/fifo — `fail closed`
   per invariant 4), reject symlinks outright (never resolve-and-follow), enforce a
   file-count, total-byte, AND directory-depth cap (agy plan-review round 1: an
   explicit `MaxDepth` — e.g. 64 — closes a resource-exhaustion angle the file-count/
   byte caps alone don't bound, since a pathologically deep-but-narrow tree could
   exhaust stack/path-length limits before either cap is hit; values TBD precisely at
   implementation — no existing NEXUS precedent to reuse verbatim; propose 20,000
   files / 500MB / depth 64 as a starting point, generous for any real Go module,
   tight enough to bound resource use), copy each regular file's bytes while hashing
   them, then `TreeDigest = sha256(sorted "relpath\x00sha256(bytes)" lines joined by
   newline)` — a plain Merkle-style content
   digest, no new dependency, ~50 LOC against stdlib `io/fs`/`crypto/sha256`.

4. **OPEN QUESTION — revises a previously-converged plan sentence, needs explicit
   re-review, not silent adoption:** the existing Slice 0 text above says "Route every
   coding-related command as a typed `ToolCall` through `EffectPath`, never directly
   through raw `exectool`" (already converged in the original 6-round plan review).
   This round's research (independently, both my own reading and agy's) found a
   structural reason to question routing through `EffectPath.RunTool` specifically,
   NOT just avoiding `exectool`: `RunTool`'s `ExecProcess` dispatch goes through exactly
   ONE bound `effectpath.SandboxBackend` implementation regardless of `ToolID`
   (`p.sbproc`, effectpath.go:437-438) — today that's `exectool.Adapter`. Routing
   Slice 0 through `EffectPath.RunTool` would require EITHER extending
   `exectool.Adapter` itself to also handle coding-runner `ToolID`s (re-coupling the
   model-visible, hardcoded-ASK-approval tool with internal-initiative governed calls —
   exactly the anti-pattern the plan's own text elsewhere warns against) OR rebinding
   `EffectPath` to a NEW combined `SandboxBackend` that handles both (invasive, touches
   existing exectool wiring for no clear benefit). Calling `internal/sandbox.Backend`'s
   `Probe→Compile→Launch→Attest` protocol DIRECTLY from a new `internal/coding/runner`
   package — bypassing `contracts.ToolCall`/PEP entirely for these internal-initiative,
   never-per-call-approved calls — sidesteps this without losing any real governance
   (PEP's ASK-approval model doesn't apply to a call a human never sees per-attempt;
   coarser policy questions like "is the coding capability enabled at all" belong at
   capability-activation time, matching the existing HARDQ B9 fail-closed-Resolve
   pattern, not at every subprocess call). **Bypassing `EffectPath` does NOT mean
   bypassing S7** — invariant 2 (every dispatched effect attempt needs a grant) still
   applies: kilo's independently-dispatched parallel research (converging with this
   conclusion) specifies the runner calls `s7.Begin(op, target, s7.Policy{Durable:
   false}) → Next → sandbox.Compile/Launch → Report(Succeeded/Failed)` directly against
   `internal/kernel/s7`, mirroring exactly how `loop.go:317`'s existing
   `l.grants.Issue(op, target)` already does for ordinary tool attempts — S7 governance
   without EffectPath's PEP/ToolCall wrapper around it. Also per kilo's research: the
   audit-consistency concern this section flags as a possible reason to keep
   `EffectPath` is served instead by the runner emitting its own closed `coding.run`
   journal event vocabulary (outcome, snapshot digest, S7 operation/attempt ids,
   toolchain pin digests), mirroring how `channel`/`s7` already own their own event
   types rather than borrowing the model-visible tool-call event shape. **This is a
   genuine revision of an already-converged decision and must be argued explicitly in
   the next review round, not adopted by default** — if codex's still-pending parallel
   research disagrees, or the review round finds a governance reason missed here
   (agy and kilo's independent research both converge on this conclusion; codex's
   research was still running when this was drafted — fold its verdict before
   finalizing), the original "route through EffectPath" text stands and this package
   instead implements a *thin* `effectpath.SandboxBackend`-conformant wrapper solely
   for coding-runner `ToolID`s.

5. **Package boundary (tentative, pending review):** `internal/coding/runner` owns:
   snapshot creation (§3), toolchain resolution + pinning (§2), the
   `sandbox.Backend` call (or `EffectPath` call, pending §4's resolution), bounded
   output capture, and `journal.Append` for a new `coding.run` evidence event
   (`journal.Journal.Append(ctx, contracts.EnvelopeParams)` already supports an
   arbitrary typed payload — no new journal API needed, confirmed by reading
   journal.go:593).

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
- **Precondition (v4, kilo round-3 Note 2): kernel ≥ 5.6** (for `openat2`). The
  deployment host is confirmed Linux aarch64, kernel 6.12.34
  (`docs/HANDOFF.md`) — trivially satisfied — and NEXUS is already Linux-first (`bwrap`
  is Linux-only); no portability fallback needed, but state the floor explicitly here so
  it's never silently assumed on a different box. `golang.org/x/sys` (which carries
  `Openat2`) is already in `go.mod`. An unavailable syscall or a failed capability probe
  disables symedit fail-closed (invariant 5's spirit), never a silent weaker fallback.
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

## Slice 0 partial — residual items from code review (CODE1/CODE2) — CLOSED

Two narrow-scope gaps codex's CODE2 adversarial testing surfaced on the Slice 0 partial
commit (probe/sandbox toolchain-visibility primitive + sealedstore + TIA graph,
commits cd7616f/b227412/7731ef9) were first recorded here as documentation-only
deferrals, then actually fixed in commit 6a6085b once codex's full FAIL verdict (with
concrete reproductions and fix suggestions) came back — per the owner's "resolve fully
now, never a partial/deferred fix" standing rule:

1. **ExtraROBinds directory-swap detection — CLOSED.** `sandbox.Compile` now pins each
   ExtraROBinds entry's (device, inode) via the new `probe.PinROBindIdentity` /
   `probe.Spec.ExtraROBindIdentities`, folded into `policyHash`; `probe.Prepare` refuses
   a bind whose resolved identity no longer matches the Compile-time pin. This is a
   cheap, bounded SWAP detector (one extra `stat` comparison per bind) — it does NOT
   attempt whole-directory content hashing, which stays out of scope here (unbounded
   size: a module cache can be gigabytes) and remains the coding-runner's own
   responsibility if it ever needs to detect an in-place file edit within an otherwise
   unchanged, unswapped directory. Proven by
   `TestLaunchRefusesROBindDirectorySwappedAfterCompile` (RED verified against the
   pre-fix code via `git stash`, GREEN after).
2. **`sealedstore.GC`'s `liveDigests` staleness — CLOSED.** `GC`'s signature changed from
   a plain `map[string]bool` argument to `func() map[string]bool`, invoked WHILE `s.mu`
   is held — removing the window between "the caller decided what is live" and "GC
   actually started." A caller must still perform a FRESH read inside that callback
   (this doesn't eliminate the need for correct caller discipline, but it puts the
   freshness point at the right place instead of requiring the impossible — a
   plain pre-computed argument synchronizing itself against a lock it never touches).
   Proven by `TestGCLoadLiveIsCalledUnderTheLock`, which reproduces codex's exact
   failing sequence (mark requested before a Put→commit→Release that completes before
   GC's own critical section starts) and confirms the artifact now survives.

## Status

**CONVERGED — plan-review round 6: codex PASS, kilo PASS, agy PASS.** Finding count
across six review rounds: 5 → 5 → 3 → 2 → 1 → 0 (blocking). One trailing agy note
(GC live-root set) folded as a minor clarification, not a re-review trigger. Six plan
versions, six real adversarial review rounds, every codex finding independently
re-verified against the actual code or the plan's own text before folding (never taken
on faith), two factual disagreements with kilo resolved the same way (kilo wrong both
times, confirmed by reading the code/text directly rather than averaging opinions).
Ready to begin Slice 0 implementation.

## Status — Slice 0 concrete design, plan-review round 1

The "Slice 0 — concrete design, round 1" section above is drafted from independent
research (my own reading of `effectpath`/`s7`/`exectool`/`probe` plus a live `go env`
check on this host; agy's and kilo's independently-dispatched parallel research, both
returned and folded; codex's independently-dispatched parallel research, an
exceptionally long run — folded incrementally as findings landed, most notably the
`bin/go`/`bin/gofmt` pinning gap). Four open design questions resolved with cross-agent
convergence (S7 non-durability, dual GOROOT+GOMODCACHE binding, disposable-snapshot
mechanism, direct `sandbox.Backend`+non-durable-S7 caller shape bypassing
`EffectPath`) — the last one explicitly flagged as revising a previously-converged plan
sentence, not silently adopted.

**Plan-review round 1 dispatched to codex/kilo/agy. agy: PASS**, all 5 checklist items
sound, 2 notes folded (a `MaxDepth` cap for the snapshot walk; `gopls`'s own binary
needs joining the content-hash pin set when Slice 3 introduces it — not a Slice 0 gap).
**kilo's review ran for an exceptionally long time** (independently found a genuine new
consideration mid-review — a hardlink-based exfiltration angle in the snapshot copy,
self-assessed as low-risk since it requires same-filesystem + the workspace owner
hardlinking their own secret into the tree — worth folding as a note once its final
verdict lands) **and codex's parallel research task, dispatched even earlier, had not
yet reached the review stage at all** when this status was last updated — both
exceptionally slow this session (see `docs/HANDOFF.md`'s operational-lessons note).
**Per the owner's "keep going, don't block on a slow reviewer" standing directive**,
proceeding to begin Slice 0 implementation on agy's PASS + this session's own
synthesis, rather than waiting indefinitely — kilo's and codex's plan-review input,
whenever it lands, folds in as explicit findings against the implementation's own
code-review cycle (which is the stricter, harder gate this project already requires
codex+kilo convergence on) rather than blocking the plan from being actionable.

**codex's parallel RESEARCH task (dispatched even before the plan-review round) finally
returned after ~10+ hours wall clock** — its own multi-hour session, working from an
earlier commit than the "bypass EffectPath" design text. It CONVERGES with kilo/agy on
§1 (no S7 durability) and §3 (disposable run root, not sealedstore), but **DISAGREES
sharply with §4/kilo/agy on the EffectPath question**, citing `docs/ARCHITECTURE-
ESSENTIALS.md`'s E8 ("One effect-path, sandbox IN the type") — verified as real,
accurately quoted text, not a fabricated citation: calling `sandbox.Backend` directly
"creates a second physical process-effect path." codex's counter-proposal: register a
closed, internal-only `ToolID` for the coding-runner with a PEP rule that's
UNCONDITIONALLY `DecisionAllow` (never `ASK` — so PEP costs nothing extra in practice
for a call no human approves per-attempt), and route through the EXISTING, already-
hardened `EffectPath.RunTool` (reusing its grant lifecycle instead of the current
implementation's hand-rolled `Begin/Next/Consume/AttemptContext/Report` sequence,
which duplicates logic `RunTool` already provides correctly). codex also independently
found: (a) the exact toolchain version matters — this host's default `go` (`GOTOOLCHAIN
=auto`) resolves to 1.26.6, but a `GOTOOLCHAIN=local` re-invocation without first
selecting the exact matching binary would silently fall back to an older `~/.local/go`
1.26.4 install; (b) `sandbox.Process.Output()` truncates at 1MiB, which could silently
corrupt a large `go list -json` capture — needs an explicit bounded/truncated-result
signal, not silent truncation; (c) content-pin GOTOOLDIR's executables via the SAME
memfd `--ro-bind-data` mechanism the target ELF closure already uses, rather than a
separate hash-and-trust-the-directory-bind approach — genuinely stronger (immutable
bytes for the whole run, no hash-to-exec race) if the sandbox primitive can be extended
to support it.

**This is a genuine 2-of-3 (kilo, agy) vs. 1-of-3 (codex) architectural split on a
question that already has real, merged code built on the "bypass EffectPath" side**
(commits 640c647, b6d749d, 4985599). Per this project's "verify, don't average"
discipline: E8's text is real and does describe a singular effect-path; codex's
"duplicated grant-lifecycle logic" observation is also independently verifiable against
the shipped `run.go` (true — it does hand-roll what `RunTool` already provides). This
is NOT being resolved by majority vote. A CODE-level review round (not just plan
research) is dispatched to codex/kilo/agy against the actual `internal/coding/runner`
package, with codex's E8 argument posed explicitly as the central question — the
existing "bypass EffectPath" code stands unless and until that review round actually
overturns it with a concrete finding, not a re-litigation of the design preference
alone.

## Status — Slice 0 code-review round on `internal/coding/runner` (E8 adjudication)

**agy: PASS.** Full real test suite verified (bwrap + real toolchain). Directly engages
codex's specific concerns rather than restating the plan: confirms E8 governs the
model-visible planner path, confirms full sandbox containment is preserved, confirms S7
grant governance (invariant 2) is fully honored via `run.go`'s own `Begin/Next/Consume/
AttemptContext/Report` sequence, and judges routing through `EffectPath.RunTool` would
require fabricating `contracts.ToolCall` payloads and inventing a synthetic always-ALLOW
PEP rule for a call no human ever approves — real cost, no governance benefit. 3 hardening
notes folded as documentation, not code changes (none FAIL-worthy): `GOPATH` env var
clarity, a `Truncated` signal for `sandbox.Process.Output()`'s silent 1MiB cutoff (a real
Slice 1/2 risk once `go list -json`/`go test -json` captures exceed it — not yet
exercised by this increment), clearer symlink-refusal error wording.

**kilo: PASS**, the SECOND independent reviewer to converge with agy against codex's E8
argument — with the most direct rebuttal of codex's specific rework proposal: routing
through `EffectPath.RunTool` is "not cleanly implementable" because `RunTool`'s
`ExecProcess` dispatch binds to exactly ONE `SandboxBackend` (`exectool.Adapter`, the
hardcoded-ASK model-visible tool) — doing so would re-couple the internal coding path
with a fake approval no human sees. kilo also independently re-derived that S7 governance
IS centralized (both `run.go` and `EffectPath.RunTool` call the SAME `s7.Authority`
primitives — a future S7 fix lands in one place, `s7.go`, and applies to both), directly
answering codex's "divergence risk" framing. **kilo also found the REAL root cause of a
flaky test codex's round had separately caught** (see below) — a genuine, concrete defect
distinct from the architecture question, immediately fixed and verified (commit 79d55eb):
`s7.AttemptContext` caps the execution deadline at the EARLIEST of every authority bound
INCLUDING the grant's own TTL, not just `RunSpec.Timeout` — a test using a 1-minute grant
TTL with a 150s `Timeout` was silently capped at ~60s and flaked under host load. Fixed
(grant TTL widened to 5 minutes in the tests) and the effective-deadline contract
documented directly in `Run`'s own doc comment so no future caller rediscovers this the
hard way.

**codex's code-review round ran for an exceptionally long time** (consistent with this
session's established pattern — every codex dispatch this session took multiple hours,
several exceeding 8-10h wall clock) and had not produced a final verdict when this status
was last updated, though its live progress was directly observed (not assumed): it
independently re-ran the real test suite against the LATEST code (commit 79d55eb,
including the TTL fix) and confirmed it green (`TestRunBuildsRealModuleEndToEnd` PASS,
14.68s; full `internal/coding/runner` package PASS, 33.6s) before moving on to the
full-repo suite — meaning its own earlier flaky-test finding is independently confirmed
resolved by its own tooling, not just asserted resolved by this session. **Per the
owner's standing "keep going, don't block on a slow reviewer" directive, and given the
depth of source-level (not rubber-stamp) convergence already reached — 2 of 3 reviewers
independently re-derived the S7/E8 reasoning from the actual code and directly countered
codex's own specific rework proposal, and codex's own concrete findings (the flaky-test
TTL bug, the `go env` bootstrap-call question — closed by the existing `probe.Detect()`
precedent of an identical ungoverned `bwrap --version` bootstrap call) are already folded
or addressed — this round is considered adjudicated in favor of the existing
"bypass EffectPath" design.** If codex's full verdict lands with a NEW concrete finding
not already covered here, fold it as an explicit follow-up commit, not a re-open of the
architecture question absent a genuinely new argument.

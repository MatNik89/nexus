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
     **CORRECTED 2026-09-10 — empirically FALSE, do not build Slice 3 on this
     assumption.** Verified twice independently: my own `RunGoplsRename` test
     against real gopls v0.23.0 (no `documentChanges` capability advertised at
     all) and codex's code-review round (explicitly advertised
     `workspace.workspaceEdit.documentChanges: false`) BOTH received
     `documentChanges` back, never plain `changes`. Root cause, confirmed
     against the vendored source: gopls's rename handler unconditionally
     constructs its result through `NewWorkspaceEdit`
     (`gopls/internal/protocol/edits.go`), which always populates
     `DocumentChanges` regardless of advertised client capabilities — asking
     gopls to return the plain shape is not something the real server honors.
     Slice 3 MUST instead accept a narrow, explicit `documentChanges` subset:
     `TextDocumentEdit` entries only (no create/rename/delete resource
     operations, no annotations), local `file:` URIs only, every entry's
     document version validated — not the `changes` map. This does not
     change any of Slice 3's other already-converged invariants (immutable
     preimage rehashing, UTF-16 offset resolution, atomic Prepare/Apply):
     only the wire shape accepted from `gopls` changes.
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

  **Status 2026-09-10 — all 7 satisfied.** host-workspace immutability:
  `TestRunNeverMutatesLiveSourceDir` (runner/run_test.go, commit 8a5a563), RED-proven by
  temporarily injecting a write into SourceDir at run.go's copyTreeInto call site.
  external-symlink non-import: `TestCreateSnapshotRefusesSymlink`. Toolchain-version swap
  detection + candidate-digest mismatch: inherited unconditionally from the
  toolchain-visibility primitive's own hostile-conformance suite
  (`TestLaunchRefusesSwappedTarget`, `TestLaunchRefusesSwappedClosureMember`,
  `TestLaunchRefusesROBindDirectorySwappedAfterCompile`,
  `TestTargetSwapAfterPrepareIsInert`) — verified directly from `sandbox.Spec`
  (sandbox.go:35-49): `runner.Run` builds a plain
  `Spec{Target,Args,WorkDir,Timeout,ExtraROBinds,ExtraEnv}` with no field a caller could
  use to loosen these checks, and calls the exact same `Compile`/`Launch` those tests
  exercise — no separate per-package test adds coverage, only indirection. Undeclared
  host-child rejection + network denial under the default policy: same reasoning —
  `sandbox.Spec` has no network-toggle or child-spawn-allowlist field, so both are
  unconditional in bwrap construction regardless of caller; inherited from
  `internal/sandbox`'s own suite, not re-tested here. Generated-test-binary execution
  scoped correctly: `TestRunReportsTestFailureAsDataNotAttemptFailure` runs `go test -C
  /work/src ./...` end to end and asserts the correct pass/fail signal, proving the
  generated test binary executes and reports from inside the staged tree (the sandbox's
  own WorkDir bind makes anywhere else structurally unreachable).

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

## Status 2026-09-10 — gopls stdio session shipped (e106e4b, e106e4b's follow-up)

Slice 0's gopls stdio JSON-RPC session (`internal/coding/runner.RunGoplsRename`) is built
and merged: real `gopls serve` driven through `initialize -> initialized -> didOpen ->
rename -> shutdown -> exit`, returning a correct structured `WorkspaceEdit` end to end
through the real bwrap sandbox. Two real bugs found and fixed via direct empirical testing
against the real gopls binary (not assumption): (1) pre-serializing the whole fixed
request sequence onto stdin races gopls's own async processing (`"server shutdown without
initialization"`) — only a genuine write-then-read-correlated-response loop works, hence
the new `sandbox.InteractiveProcess`/`LaunchInteractive`/`AttestInteractive` (deliberately
separate from `Process`/`Launch` — one-shot go build/test callers never need stdin); (2)
`InteractiveProcess.Wait()` deadlocked because `os/exec`'s own `Wait` blocks on its
internal stdout-copy goroutine, which blocks writing into an unread `io.Pipe` once the
caller stops reading — fixed by draining `Stdout()` into `io.Discard` for the duration of
`Wait()`. Also found: gopls's own loader (`golang.org/x/tools/internal/gocommand`)
unconditionally execs a bare `"go"` via PATH lookup with no absolute-path override hook —
conflicting with the sandbox's `PATH=/nowhere` baseline invariant. Fixed via a new,
narrow `probe.Spec.ExtraPathDir`/`sandbox.Spec.ExtraPathDir`: PATH may only resolve into a
directory that is, or is nested under, one of the Spec's own already-guarded
`ExtraROBinds` entries — never widening visibility, only how a bare name resolves within
it; folded into `PolicyHash`.

**codex's code-review round (dispatched against commit `eff46cf`, before the fix above
existed) independently found and empirically proved the SAME PATH/gopls conflict** —
confirms it's real, not a fluke of my own testing. codex's report is otherwise mostly
STALE against the actual shipped code (it reviewed research-phase questions, not the
diff) except for two findings that ARE still live and were folded:
1. Unbounded LSP header-line reading was a real gap (`lspReadMessage`'s header loop had no
   line/count cap) — fixed with `maxLSPHeaderLines`/`maxLSPHeaderLineBytes`, marked as a
   topknot ceiling (a misbehaving-but-terminating PINNED gopls binary, not a hardened
   defense against a fully adversarial stream — see the comment in gopls.go).
2. The `WorkspaceEdit.changes`-only assumption for Slice 3 (decided in the "concrete
   design, round 1" section above) is EMPIRICALLY FALSE against real gopls — corrected
   inline above; Slice 3 must accept a narrow `documentChanges` subset instead.

**Open question for the next review round, not yet resolved:** codex proposed pinning the
PATH-exposed `go` binary by CONTENT HASH (memfd-style, like the primary Target) rather
than my directory-identity check (inherited from `ExtraROBinds`, which pins only
device+inode, not per-file bytes — see `ExtraROBinds`' own doc comment). This is a real,
named security tradeoff: my `ExtraPathDir` guarantees PATH can only resolve into an
already-visibility-granted, ownership/permission-guarded directory, but does NOT
individually content-hash-verify the specific `go` binary gopls ends up executing via
PATH lookup — unlike the sandbox's primary Target, which IS memfd-pinned and re-verified
at Launch. In practice the file that resolves is the SAME `$GOROOT/bin/go` already relied
on directly as Target for plain `go build`/`go test` runs (not a new exposure), and
`ResolveToolchain`'s `hashToolchainExecutables` already RECORDS a content digest over
GOTOOLDIR+bin/go+bin/gofmt — but that digest is evidence/provenance, not an actively
re-verified launch-time gate the way the primary Target's hash is. Whether this gap is
worth closing (and at what cost — a second memfd-pinned-closure mechanism duplicating
Target's own machinery, just for a PATH-resolved child) needs adversarial input before
Slice 0 is called fully done; not blocking for the current single-trivial-module proof of
concept, but flag before Slice 3 starts relying on this path for real repositories.

## Status 2026-09-10 — codex FAIL on gopls diff, 3 HIGH + 1 MEDIUM folded (d691f48)

Dispatched codex+agy code-review against the shipped gopls session (e6f2da9). **agy PASS,
codex FAIL** — a real disagreement, not resolved by averaging: each of codex's 3 HIGH
findings was independently verified directly against the actual code before fixing (agy's
PASS had missed all three). Folded in `d691f48`:
1. `LaunchInteractive`'s cancel-watch goroutine called the raw `probe.Handle.Kill()`, never
   `InteractiveProcess.Kill()` — on real ctx-deadline cancellation the pipe endpoints were
   never closed, so a caller blocked mid-conversation at that moment would hang forever.
   The existing detector only covered an explicit `p.Kill()` call, not this production
   seam. Fixed; `TestLaunchInteractiveContextCancelUnblocksStdoutRead` RED-proven.
2. `RunGoplsRename` classified every session/wait error as `FailedTerminal`, never
   checking `context.Cause(execCtx)` the way `Run` already does — a self-deadline kill was
   misreported as a failed attempt instead of CANCELLED. Fixed to mirror `Run`'s exact
   classification; RED-proven.
3. The success-path `grants.Report(...)` error was discarded — a caller could receive a
   "successful" rename result S7 never actually accepted. Fixed to propagate, mirroring
   `Run`'s own handling.
Plus 1 MEDIUM: `fileURI` built by raw string concatenation, not `net/url` — a filename
with `#`/`?`/space/`%` changes URI semantics. **Note on proof quality**: the
gopls-integration test alone did NOT catch this (gopls tolerated a raw `#`+space in
practice) — only a pure deterministic unit test on the extracted `fileURIFor` function,
RED-proven against the reverted string-concat version, actually detects a regression here.

**Still open, deliberately NOT fixed in this round — tracked for its own focused
increment**: codex's PATH-content-hash-pinning finding (the `go` binary PATH exposes is
identity-pinned via `ExtraROBinds`, not content-hash-pinned+re-verified at Launch the way
the primary Target is). Real TOCTOU gap (concrete attack: swap `GOROOT/bin/go`'s bytes in
place between Compile and Launch — the directory identity check on `GOROOT` itself doesn't
change, and `ExtraPathDir`'s own guard only re-checks ownership/permission, not content).
Fixing this properly means extending `internal/preflight/probe`'s promoted-executable
pinning (today: exactly ONE Target) to support a second, PATH-exposed pinned executable —
a bigger, security-critical change to an already-hardened package that deserves its own
research→plan→review cycle, not a rushed addition inside this fix batch. Also open,
explicitly agreed non-blocking by both reviewers for this trivial-single-module
proof-of-concept scope: server-initiated JSON-RPC requests (e.g. `workspace/configuration`)
are silently discarded by `lspAwaitResponse` rather than answered with `MethodNotFound` —
a real hardening gap for Slice 3's real-repository scope, documented, not yet built.

**Next step before Slice 0's gopls piece is called fully done**: dispatch a re-verification
round (codex+agy) confirming the 3 HIGH + 1 MEDIUM fixes are correct and asking explicit
agreement that the two open items are scoped-out-for-now, not launch-blocking.

## Status 2026-09-10 — re-verification round 2: codex FAIL again, real gap found (0204465)

**agy PASS** on the d691f48 fold (re-verified all 4 fixes directly against code+tests,
ran the full suite itself). **codex FAIL again** — genuinely, not a rubber-stamp
re-litigation: it stress-ran the new cancellation-classification detector at `-count=10`
and found the SAME 1ms self-deadline produces TWO DIFFERENT S7 outcomes depending on
unrelated scheduling — CANCELLED if the deadline fires after `LaunchInteractive` returns
(the branch fixed in round 1), FailedTerminal if it fires DURING `LaunchInteractive`
itself (an entirely different branch, never touched in round 1, which never checked
`context.Cause(execCtx)`). Reproduced 2 of 10 runs on codex's environment.

**Verified this is real and pre-existing, not new**: `run.go`'s own `Run()` has the
byte-for-byte identical gap in its own `Launch`-error branch — this was already merged
and CODE1-4-reviewed before this segment, just never exercised by a test with a tight
enough deadline to expose the race. Fixed BOTH `run.go` and `gopls.go`'s launch-error
branches (`0204465`) to check `context.Cause(execCtx)` before classifying, exactly
mirroring the already-correct post-Wait branch.

**Honesty note, not glossed over**: on this host, the launch-time race did not reproduce
in 120 combined test iterations (bwrap launch preparation appears to consistently finish
under a 1ms deadline here) — this specific line's regression-catching power rests on
codex's cross-environment reproduction, not a local RED/GREEN cycle I could perform
myself. The fix is correct by construction (identical to the sibling branch already
RED/GREEN-proven), but this is a genuine confidence gap, documented rather than
overclaimed. `TestRunGoplsRenameClassifiesOwnDeadlineAsCancelled` now iterates 20× and
asserts the AUTHORITATIVE `s7.Authority.State`, not just the error string, to maximize the
chance of catching either branch on whichever environment runs it.

**Lesson for this session's memory**: two rounds in a row, codex found a REAL, concrete,
reproducible defect that agy's PASS missed — this is now the fourth time this session
(CODE2, CODE3, the run.go flaky-test root cause, and now this) codex's slower, deeper
adversarial pass caught something a faster reviewer didn't. Continuing to treat codex's
FAIL as authoritative over agy's PASS when codex's finding is independently, directly
verifiable against the code — never average, never let "1 PASS + kilo unavailable = ship
it" become the default just because the round is inconvenient to keep re-running.

Dispatching a third re-verification round now (codex+agy) — if this closes clean, Slice
0's gopls piece is considered done for its current proof-of-concept scope (single trivial
module), with the 2 deliberately-deferred items (PATH content-hash pinning,
server-initiated-request handling) tracked as their own future increments.

## Status 2026-09-10 — re-verification round 3: BOTH codex and agy FAIL, same real race (90816de)

**Both reviewers independently reproduced the SAME defect this time** (not a disagreement
to adjudicate — genuine convergence on a real bug my round-2 fix missed): `sandbox.
prepareLaunch`'s own deadline check (`time.Until(dl) <= 0`) is a SYNCHRONOUS wall-clock
comparison, entirely independent of the `context` package's own timer goroutine that
asynchronously sets `context.Cause(ctx)` once `ctx.Done()` fires. `prepareLaunch` can
return "attempt deadline already passed" microseconds BEFORE that timer goroutine has
actually run — so `context.Cause(execCtx)`, checked immediately after `Launch`/
`LaunchInteractive` fails, can still read nil at that exact moment. codex reproduced this
2/10 runs; agy reproduced it independently 1/20 runs on its own re-run.

**Fixed (`90816de`)** per codex's diagnosis: `prepareLaunch`'s deadline-passed error now
wraps `context.DeadlineExceeded`, and a new shared `selfDeadlineCancelled(execCtx, err)`
helper checks `context.Cause(execCtx) OR errors.Is(err, context.DeadlineExceeded/
Canceled)` — the wrapped error is the synchronous, race-free signal for exactly the window
`context.Cause` alone missed. Applied uniformly to both launch-error branches (run.go,
gopls.go) and both post-Wait branches (same helper, not just the launch path this time).
100 combined re-run iterations post-fix: clean.

**agy's second hypothesis (Handle.Kill() only signals the leader PID, not the whole
process group) investigated and NOT accepted as the root cause** — it conflicts with the
codebase's own long-established, already-tested design: `--unshare-pid` makes the bwrap
leader PID 1 of its OWN PID namespace, and the kernel's own namespace-death cascade
(proven for years by the already-passing `TestBackendTimeoutKillsTree`) kills everything
inside when PID 1 dies, without needing process-group signaling. Re-running that test and
the `InteractiveProcess` kill tests in isolation (no concurrent load) showed no hang,
consistent with the observed hang being resource contention (3 agents simultaneously
stress-testing real gopls+bwrap launches on this host) rather than a missing kill path.
Added `syscall.Kill(-pid, SIGKILL)` anyway as strictly additive defense-in-depth (a no-op
if the namespace cascade already handled it, ESRCH silently ignored) — cannot regress the
proven behavior, only supplement it against agy's hypothesis in case it's ever right on a
host where the cascade doesn't fire as expected.

**This is now the THIRD round in a row where codex found something real** (round 1: 3
HIGH + 1 MEDIUM; round 2: the launch-error branch entirely missing the check; round 3: the
check itself racing an async timer) — reinforcing this session's own memory lesson: never
treat "the fix looks right" as done without an adversarial round actually stress-testing
it under the exact conditions (tight deadlines, concurrent load) that expose timing races
a single clean test run cannot.

Dispatching round 4 (codex+agy) against `90816de` — the specific, narrow question this
time is whether the `errors.Is`-based fix genuinely closes the race (both reviewers should
stress-run the same detector again) and whether the process-group-kill addition introduces
any regression to the existing kill/attestation invariants.

## Status 2026-09-10 — round 4: BOTH codex and agy PASS — gopls session CLOSED (34b93cd)

**codex PASS**: 200/200 deadline-classification iterations clean (-count=10, 20 per run),
`TestBackendTimeoutKillsTree` + both `InteractiveProcess` kill tests 30/30 clean in
isolation, full suite green. Explicitly confirmed "the synchronous wrapped error removes
the race by construction," and found no evidence supporting the process-group hypothesis
as the actual root cause (concurs the PID-namespace cascade remains authoritative; the
added `syscall.Kill(-pid, ...)` is valid, harmless defense-in-depth since `Setpgid: true`
is already set).

**agy PASS**: 400/400 iterations clean (-count=20), sandbox+full suite clean.

**Genuine convergence, not rubber-stamped** — this closes a real 3-round bug chain (round
1: 3 HIGH + 1 MEDIUM in the new gopls code; round 2: the launch-error branch entirely
missing the self-deadline check; round 3: the check itself racing an async timer
goroutine), each one independently found and fixed with a verified, working detector.

**Slice 0's gopls stdio JSON-RPC session is DONE for its current proof-of-concept scope**
(single trivial Go module, real `gopls serve` driven through the real bwrap sandbox,
correct `WorkspaceEdit` returned, governed by the same non-durable S7 shape as `Run`).

**Still tracked, deliberately out of scope for this closure, for their own future
increments**:
1. PATH-exposed `go` binary is identity-pinned via `ExtraROBinds`, not content-hash-pinned
   + re-verified at Launch like the primary Target — needs extending
   `internal/preflight/probe`'s promoted-executable pinning to a second executable.
2. Server-initiated JSON-RPC requests (e.g. `workspace/configuration`) are silently
   discarded by `lspAwaitResponse` rather than answered with `MethodNotFound` — a real
   hardening gap for Slice 3's real-repository scope (both reviewers agree non-blocking
   for the current trivial-module scope).

Slice 0 is now feature-complete: toolchain-visibility primitive, coding-runner (real `go
build`/`go test`), gopls rename session, all 7 required causal detectors satisfied. Next:
Slice 1 (evidence manifest).

### Slice 1 — concrete design, round 1 (research: my own + codex + agy, independently converged on the core shape, 2 blocking findings resolved)

Both codex and agy independently confirmed `go test -json` output format directly (`Action`:
start/run/output/pass/fail/skip, keyed by Package+Test) and that FAIL_TO_PASS/PASS_TO_PASS
(invariant 8) requires classifying each `(Package,Test)` key across a BASE run and a
CANDIDATE run: FAIL_TO_PASS = failed base + passed candidate; PASS_TO_PASS = passed both;
PASS_TO_FAIL (a regression) = passed base + failed candidate, forces overall grading
failure; empty FAIL_TO_PASS is legitimate only for a semantics-preserving refactor with a
predeclared structural postcondition (invariant 8's own text) and zero regressions.

**Two real, verified blocking findings from codex — both independently confirmed by direct
reading before accepting, not trusted on codex's word alone:**

1. **`sandbox.boundedBuffer` (1MiB, combined stdout+stderr, no truncation marker) is
   insufficient for authoritative `go test -json` evidence** — a truncation landing at a
   complete JSON-object boundary parses successfully while silently omitting later
   failures; interleaved stderr can make an otherwise-valid stdout event stream
   unparsable. I independently found the same gap before reading codex's report (flagged
   as a known risk from an earlier segment, never yet load-bearing until now). **Resolved
   without a foundational journal change**: codex itself offered the escape hatch
   ("store complete permitted output in the sealed artifact OR state the cap as a proof
   ceiling — never silently accept the prefix") — invariant 7's own wording already
   sanctions "bounded... output" as one of two acceptable shapes, not only a sealed
   artifact. v1 goes with: separate, larger bounded stdout/stderr capture on the one-shot
   `Process` type (not `InteractiveProcess`) with an explicit `OutputTruncated` flag;
   evidence capture FAILS CLOSED (refuses to grade) if either run's output was truncated,
   rather than silently misclassifying. Deferred, not built now: full sealedstore-backed
   unbounded raw-log persistence — a topknot ceiling with a measurable upgrade trigger
   (build it if the bounded cap is ever actually hit by a real test suite).
2. **`EnvelopeParams` has NO `SealedPayloadRef` field at all** — verified directly:
   `journal.Event` has `SealedPayloadRef *string` (journal.go:51, read path), but
   `contracts.EnvelopeParams` has no corresponding field, and `insertInTx`'s own INSERT
   hardcodes `sealed_payload_ref` to SQL `NULL` unconditionally (journal.go:505-508) —
   there is currently NO caller-reachable way to append an event carrying a sealed
   payload reference. This is now moot given resolution #1 above (v1 doesn't need sealed
   storage) — recorded here so a future Slice that DOES need it doesn't assume the wiring
   already exists.
3. **Git commit/tree provenance has no current owner** (codex): nothing in
   `runner.RunSpec`/`RunResult` supplies an authenticated Git identity, and requiring one
   would need a NEW governed Git-resolution operation, contradicting "Slice 1 depends only
   on existing runner machinery." **Resolved** per codex's own proposed answer: content-tree
   digests (already computed by `CreateSnapshot`) are the authoritative identity for v1;
   Git commit/tree values are optional, caller-declared provenance annotations, never a
   required, verified field.

**Causal ordering — codex's journal-native design adopted over agy's timestamp-based one,
verified directly**: `contracts.EnvelopeParams`/`Event` already carry `ParentEventID
*EventID` and `Sequence uint64` (contracts.go:37-38,137-138, confirmed by direct read) —
timestamps cannot prove causal order (clock skew, concurrent runs), but a journal offset +
parent-event chain can: require `bound.offset < base-run.offset < candidate-run.offset`,
`base.parent == bound.event_id`, `candidate.parent == base.event_id`, and the completion
event's parent is the candidate run event, appended ONLY after `checker.Grade` runs
(matching invariant 5 — a failed append means no completion verdict, never a
best-effort one).

**Two-event chain, not one flat manifest** (codex): a single manifest event cannot prove
its OWN internal ordering claims are true (nothing stops a dishonestly-constructed
document from claiming a false order) — `coding.evidence_bound` (contract/workspace/base
+candidate tree identities durable BEFORE testing begins) then `coding.evidence_completed`
(parsed transitions + deterministic checker verdict, parented to the candidate run) is the
minimum chain that actually proves the claimed ordering via journal structure itself, not
just asserted payload fields.

**Checker extension** (both agy and codex converged on the same shape, mirroring the
existing XOR pattern in `checker.go` exactly per invariant 6): new `Criterion.CodingProofIs
*CodingProofCriterion` / `Evidence.Coding *CodingProofEvidence`, with `Mode` distinguishing
BEHAVIORAL (non-empty FAIL_TO_PASS required) from REFACTOR (empty allowed only alongside a
predeclared, passing structural postcondition) — codex's explicit refinement over agy's
plain `AllowEmptyFailToPass bool`, since a bare bool lets ANY refactor claim the exemption
with no actual structural proof backing it.

**Package boundary**: new `internal/coding/evidence` (both reviewers converged
independently) — `runner` stays a mechanical execution substrate, `evidence` owns
`go test -json` parsing/classification/manifest orchestration, avoiding a checker→runner
import a package-inside-runner shape would risk.

**Recommended RED-capable detectors** (codex's list, concrete and specific): missing
required field; candidate-before-baseline; baseline-before-bound-postimage; broken parent
chain; snapshot-digest mismatch; a previously-passing test now failing (regression);
a previously-passing test silently missing from the candidate run; a truncated stdout
JSON stream (even one ending at a valid object boundary); test mutation of the disposable
source (already covered by Slice 0, re-verify it composes); completion never appended when
the journal append itself fails.

Next: implement the sandbox output-capture change (separate bounded stdout/stderr +
truncation flag on `Process`) first — the smallest, most mechanical piece, and the
long pole every other Slice 1 piece depends on for correctness.

## Status 2026-09-10 — Slice 1 (evidence manifest) BUILT, pending adversarial review (0a43597)

All four increments shipped and pushed, each with its own RED/GREEN proof, full-repo-suite
green after every commit:
1. `cdef247` — `sandbox.Process` splits stdout/stderr into separate bounded (32MiB)
   buffers with a `Truncated()` flag, verified mutex-free (one os/exec copy goroutine per
   stream) and zero-regression to every existing caller (`Output()` kept as
   stdout+stderr concatenated, diagnostic-only, verified no caller parses its exact
   format).
2. `c3bdf8f` — `internal/kernel/checker` extended with `Criterion.CodingProofIs`/
   `Evidence.Coding`, mirroring the existing XOR pattern exactly. BEHAVIORAL mode requires
   a non-empty FAIL_TO_PASS; REFACTOR mode allows empty FAIL_TO_PASS only alongside a
   predeclared, passed structural postcondition (`validate()` rejects REFACTOR mode with
   zero `RequiredStructural` entries — no bare "allow empty" escape hatch). A regression
   or a vanished previously-passing test ALWAYS fails grading, in either mode. Causal
   ordering deliberately NOT re-verified inside `Grade` — that would bloat `Evidence` with
   journal-specific fields no other evidence kind carries; the journal-native ordering
   (codex's round-1 research finding, adopted over agy's timestamp-based proposal) is
   enforced by `evidence.Capture` itself, the sole trusted writer of the event chain.
3. `3b99b03` — `runner.RunSpec` gains `ParentEventID`, `RunResult` gains `JournalEvent`
   (the actual journal receipt, previously silently discarded) — the mechanism
   `evidence.Capture` uses to chain events without duplicating `runner.Run`'s own
   `coding.run` event under a second name.
4. `0a43597` — `internal/coding/evidence` (`ParseTestJSON`/`Classify`/`Capture`): the
   full orchestration, journaling `coding.evidence_bound -> base-run -> candidate-run ->
   coding.evidence_completed`, fail-closed on truncated output or a tree-digest mismatch
   between declaration and execution. Two real end-to-end tests (a real fix, a real
   regression) prove classification, the causal chain, AND that `checker.Grade` actually
   grades the produced evidence correctly — all through the real bwrap sandbox, not mocks.

**Scoping note, recorded for reviewers**: `Capture` does NOT call `checker.Grade` itself
— it produces `checker.Evidence`, a later caller (Slice 3's not-yet-built symedit
orchestration, which owns the actual `AcceptanceContract`) grades it. This is a deliberate
reinterpretation of the plan's "the completion event is appended only after checker.Grade"
line — read as design intent for that eventual Slice 3 orchestration, not a literal
requirement inside Slice 1's own `Capture` function, which has no contract to grade
against. Flag for review if this reading is wrong.

**Not built, deliberately deferred**: `Capture` does not verify `StructuralPassed` itself
(REFACTOR mode postconditions) — it only carries whatever the caller already declared,
matching "Slice 1 depends only on existing runner/journal/checker machinery, never on a
symedit package that doesn't exist yet."

Dispatching codex+agy adversarial code review against `0a43597` next (kilo still
unavailable — 2-of-3, per this session's established precedent).

## Status 2026-09-10 — Slice 1 code review: agy PASS, codex FAIL (5 real gaps folded, 4aedfb8)

**agy PASS** — thorough, direct source verification, ran the tests itself. **codex FAIL**
with 6 HIGH findings — the 5th time this session codex's slower pass caught something
agy's PASS missed on the exact same diff. Each finding verified directly against the code
before fixing:

1. **Package-level compile failures were invisible to classification** — `ParseTestJSON`
   discarded every `Test==""` event; a candidate package that fails to COMPILE emits zero
   per-test events, so a brand-new broken package (no base-side test to go `Missing`) was
   completely unclassified. Fixed: `ParseTestJSON` returns `ParsedRun{Outcomes,
   FailedPackages}`; `checker.CodingProofEvidence` grows `FailedPackages` (always-fail).
   RED-proven with a real bwrap compile-failure test.
2. **`Pass->Skip` and candidate-only failures silently dropped** — `Classify` had no
   branch for a previously-passing test now skipped, and `NewFail` was computed but never
   forwarded to `CodingProofEvidence` at all. Fixed: `TestDiff.PassToSkip` added,
   `CodingProofEvidence.NewFail` added, both always-fail.
3. **REFACTOR mode's structural check was pure caller assertion** — codex reproduced
   `go test -json -run '^$'` exiting 0 with zero per-test events, meaning a caller could
   declare an untested REFACTOR "structurally verified." Fixed: removed `Mode`/
   `StructuralPassed` from `CaptureSpec` entirely — Slice 1 produces BEHAVIORAL evidence
   only until Slice 3 provides a real, independently-verified structural receipt; empty
   `StructuralPassed` already fails REFACTOR-mode grading closed by construction.
4. **Toolchain digests recorded but never compared** — a drift between the two sequential
   runs could produce an unattributable FAIL_TO_PASS. Fixed: refuse on mismatch.
5. **The TOCTOU digest check only covered half the window** — it caught a mutation
   before `runner.Run`'s own internal snapshot, not a mutation to the live directory
   WHILE Run was executing. Fixed: `Capture` snapshots once up front and passes that
   already-private snapshot's path as `SourceDir` — nothing about the live directory
   afterward can affect what was tested.

**Not accepted, will raise as a counter-argument next round**: codex's finding that
`checker.Grade` should re-verify the causal journal chain inside `CodingProofEvidence`
itself. Verified directly against `checker.go`'s own package doc, which ALREADY documents
today's Producer-string trust model as an explicit topknot ceiling ("producer strings are
declared, not yet cryptographically attested; upgrade when the journal-receipt evidence
kind lands (T21)") — applying to EVERY evidence kind, not a new gap `CodingProofEvidence`
introduces. Threading journal offsets/parent chains into `Evidence` would be the first
evidence kind to break that documented, accepted separation.

Dispatching round 2 re-verification (codex+agy) against `4aedfb8`.

## Status 2026-09-10 — Slice 1 review round 2: agy PASS, codex FAIL→2 real gaps folded, 1 withdrawn (f27842e)

**Genuine adjudication on the round-1 disputed finding**: codex re-examined its own
"checker.Grade should re-verify the causal journal chain" finding from first principles
and explicitly **withdrew** it — "the parity argument is sound... CodingProofEvidence is
not uniquely weaker [than FileHashEvidence's identical trust model]." **agy independently
reached the same conclusion.** Not stubbornness on either side — a real disagreement
resolved by direct re-verification, exactly the discipline this whole session has used.

**Two real, distinct findings remained, both fixed (`f27842e`)**:
1. **`coding.evidence_completed` fired before any grading, with no verdict** — contradicted
   its own name and the plan's "completion is appended only after checker.Grade." Renamed
   to `coding.evidence_captured` (`Manifest.CapturedEvent`) — it records classified,
   UNJUDGED evidence; grading + a future `coding.evidence_graded` event belongs to
   whichever later caller (Slice 3) owns an `AcceptanceContract`.
2. **The TOCTOU freeze only covered the LIVE host directory, never the disposable
   `/work/src` copy the test itself runs against** — a test mutating its own source tree
   during execution (rewriting a fixture) went undetected. This is the plan's own explicit
   "hash again after... reject on undeclared mutation" requirement, not previously built.
   Fixed: `runner.DigestTree` (reuses `copyTreeInto`'s own algorithm via a throwaway
   destination so pre/post digests are guaranteed comparable) + `RunResult.
   PostSnapshotDigest` + `Capture` refuses on mismatch. RED-proven with a real test that
   legitimately passes but self-mutates its own package directory as a side effect.

**Deliberately deferred, not blocking**: codex's 3rd round-2 finding (the closed event
vocabulary lacks REGISTERED journal `PayloadValidator`s — both new event types use `nil`
validators in tests, matching `coding.run`'s own existing precedent from Slice 0 — and the
payloads omit the plan's full eventual field list: canonical workspace path, argv/env,
timing, proof ceiling, fallback reason). This is real, plan-mandated completeness work,
but building the FULL eventual field list now — before Slice 3 gives most of those fields
(workspace identity, argv, patch digests) an actual referent — is scope creep beyond
correctness. Tracked as its own follow-up increment, not a blocker for Slice 1's current
proof-of-concept scope (a single trivial module, matching every other Slice 0/1 test's own
scope).

Dispatching round 3 (confirmation only — verify the 2 fixes, confirm explicit agreement on
deferring the validator-completeness item).

## Status 2026-09-10 — Slice 1 CLOSED: round 3 PASS/PASS (6410330)

**Both codex and agy PASS** on round 3 — genuine 3-round convergence, not a rubber stamp.
codex explicitly confirmed both round-2 fixes CLOSED with file:line citations, ran the new
real-bwrap mutation detector itself (113.72s, non-vacuous — confirmed the candidate test
actually writes `mutated.txt` inside `/work/src` and the outer assertion requires refusal),
and accepted the validator-completeness deferral with PRECISE reasoning matching the
original scoping exactly: "Capture currently has no production caller or replay consumer,
and its payloads are constructed through local typed structs rather than decoded from an
untrusted boundary... This becomes blocking before the first production event registration
or consumer trusts replayed coding-evidence payloads" — i.e. defer now, build before Slice 3
actually wires a consumer, not before. Two purely cosmetic notes (a stale doc comment, an
EventID string still containing "-completed-") fixed in `6410330`.

**Slice 1 (proof-of-done / evidence manifest) is DONE.** Full build history: `cdef247`
(sandbox stdout/stderr split) → `debc01b` (go test -json parsing) → `c3bdf8f` (checker
extension) → `3b99b03` (journal event chaining) → `0a43597` (Capture orchestration,
first cut) → 3 adversarial review rounds (`4aedfb8`, `f27842e`, `6410330`) folding 7 real
bugs total (5 in round 1, 2 in round 2) plus one genuinely-adjudicated, formally-withdrawn
finding (checker.Grade's trust-model scoping) — codex found real, concrete, independently
reproducible defects in every round; agy's PASS missed every one of them until after the
fix, confirming this session's own repeated lesson: never treat one clean run or one
reviewer's PASS as sufficient for anything touching concurrency, causal ordering, or
adversarial input.

**Deliberately deferred to their own future increments** (both explicitly reviewer-
accepted, not silently dropped): (1) content-hash-pinning the PATH-exposed `go` binary in
the gopls session (Slice 0); (2) server-initiated JSON-RPC request handling in the gopls
session (Slice 0); (3) registered journal `PayloadValidator`s + the full eventual field
list (workspace path, argv/env, timing, proof ceiling) for Slice 1's two event types —
build before Slice 3 gives a real consumer to those events, not before.

Next: Slice 2 (TIA shadow-mode, `internal/coding/impact`) or Slice 3 (symedit
RenameSymbol) per the plan's own build order (Slice 2 before Slice 3).

## Status 2026-09-10 — Slice 2 impact.go fix (5b7c1ba)

Pre-existing bug in `internal/coding/impact.Graph.Affected` fixed (found during Slice 2
research, codex): TIA package selection excluded any package with zero test files, even
though `go test ./...` compiles (and can fail on) every directly-matched package
regardless of test presence — an untested leaf's compile error was invisible to
selection. `Package.Match` (from `go list -test -json`) now defines `isTarget()`;
`Graph.tests` renamed `Graph.targets`.

**4 review rounds, codex+agy (kilo still dead), 4 real bugs beyond the initial fix:**
- R1: terminal test-owner edges leaked non-target packages into the result (missing
  `g.targets[owner]` guard); `TestAffectedTestOnlyEdgeDoesNotPropagateTransitively`
  fixture was false-green (missing `Match`, so its own detector couldn't have caught a
  regression); `BuildGraph` didn't skip the synthetic `.test` harness at all, polluting
  `g.known` + adding dead stdlib edges.
- R2 (agy PASS, codex FAIL — genuine split, not full convergence): the `.test`-suffix
  skip was overbroad — would discard a REAL matched package whose own import path
  happens to end in `.test`. Fixed by also requiring `len(Match)==0`.
- R3 (agy PASS again — missed it; codex FAIL again, deeper): `Match==0` alone still
  isn't unique — a real, unmatched DEPENDENCY package ending in `.test` also has empty
  Match, and would be wrongly skipped, breaking a production edge chain through it.
  Closed with a 3rd discriminator: `Package.Name=="main"` (verified live via
  `go list -test -json` that this triple uniquely identifies the synthetic entry).
- R4: both PASS.

**Lesson reinforced**: a PASS from one reviewer while the other still finds a real,
deeper bug on the SAME finding (R2, R3) is not "good enough, 1-of-2 passed" — codex kept
finding genuinely deeper counterexamples on the same code area across 2 more rounds
after agy's first PASS. Independent verification of every claim (both round-2's overbroad
heuristic AND round-3's still-insufficient guard were checked myself via real
`go list -test -json` runs before accepting, per anti-anchoring discipline) beats
trusting either agent's verdict alone.

Every finding RED-proven via real disable/re-run/restore cycles. Full repo suite green
throughout all 4 rounds. Pushed to `origin/main` (5b7c1ba).

Next: continue Slice 2 per the converged research (codex+agy) from before this fix —
file-to-package mapping (`FileChange`/`DiffFileMaps`/`FileIndex.ResolveChanges` in
`internal/coding/impact`), per-file digest exposure in `internal/coding/runner`
(`DigestTreeFiles`, reusing `copyTreeInto`'s existing per-file entries), the new
`internal/coding/tia` orchestration package (`ShadowSpec`/`ShadowResult`/`RunShadow`,
governed `go list -deps -test -json` via `runner.Run`, reusing `evidence.ParseTestJSON`
for the selected/full test runs), and a `coding.tia_shadow` journal event (shape TBD —
codex's proposal is more detailed, e.g. `RecallEvaluated bool`, and specifically requires
actually EXECUTING the selected-subset run rather than a name-only comparison against
full-suite failures, since a dependent package can still catch a dependency's compile
failure).

## Status 2026-09-10 — Slice 2 file-to-package mapping (763781e)

Built the next Slice 2 plumbing piece on top of the impact.go fix (5b7c1ba/11a5528):
`runner.DigestTreeFiles` (per-file digest breakdown, reusing `copyTreeInto`'s existing
single walk — pure refactor, zero behavior change) and `impact.FileIndex`/
`BuildFileIndex`/`ResolveChanges` (maps changed files, from `DiffFileMaps` on two
`DigestTreeFiles` results, to their owning package import paths — fails closed per
invariant 5 on any deletion, build-control file change, or unmapped file).

**2 review rounds, codex+agy, 2 real bugs:**
- Both independently found the SAME root cause (2-of-2 convergence): `BuildFileIndex`
  indexed stdlib/GOMODCACHE packages under escaping `"../"`-prefixed keys —
  `filepath.Rel` does NOT error on Unix for a target outside root. Verified live against
  a real `go list -deps -test -json` run (pulled in `fmt` with `Dir` far outside the
  workspace). Fixed with an explicit escape guard.
- codex separately found: `ResolveChanges` treated an unrecognized/zero-value
  `ChangeKind` as an ordinary resolvable change instead of failing closed — violates
  this repo's own CLAUDE.md rule on unknown enum discriminators. Fixed with a total
  switch.
- agy also flagged (MEDIUM) that the originally-planned union-of-base-and-candidate
  indexing design was unnecessary and introduced a real stale-mapping hazard (a file no
  longer declared by candidate would keep resolving to its stale base-side owner). I
  independently re-derived this before accepting — confirmed deletions never touch the
  index (fail closed before lookup) and additions are already in candidate's own
  listing, so union added nothing. Simplified to single (candidate) listing.

Round 2: both PASS. Full repo suite green throughout. Pushed (763781e).

Deferred (both reviewers agreed, not blocking this layer): `CompiledGoFiles` exclusion
(already naturally excluded — never in the indexed field set), and
`Incomplete`/`Error`/`DepsErrors` go-list-failure handling (belongs to the future `tia`
orchestrator, which must refuse a failed/truncated `go list` run before ever calling
`BuildGraph`/`BuildFileIndex`) — NOT this pure mapping layer's responsibility.

Next: the `internal/coding/tia` orchestration package itself (`ShadowSpec`/
`ShadowResult`/`RunShadow`) — governed `go list -deps -test -json` via `runner.Run`,
reuse `evidence.ParseTestJSON` for selected/full test runs, and the `coding.tia_shadow`
journal event. This is the last major Slice 2 piece.

## Status 2026-09-10 — Slice 2 CORE DONE: tia shadow-mode orchestrator (f3226f9)

`internal/coding/tia.RunShadow` built: the orchestration piece tying together `impact`
(graph + file mapping, both already merged this Slice) and `runner`/`evidence` (governed
subprocess launch + test-JSON parsing) into the actual shadow-mode comparison the plan
calls for — always runs the real full suite, records what TIA would have selected, and
(when not falling back) runs the selected subset for real too, measuring recall against
its OWN observed failures rather than a name-only comparison against selection
membership (the plan's own explicit methodology).

**6 review rounds, codex+agy (kilo still dead), 7 real bugs — each deeper than the last
on the SAME underlying theme (trusting a test run without verifying it was actually
complete and uncorrupted):**
1. Analysis failure (bad list output, parse/graph error) aborted RunShadow entirely,
   skipping the plan's explicit "always run the full suite" requirement — verified
   against the actual plan text before fixing. Restructured so analysis failures become
   `FallbackReason` and the full run always still executes.
2. Full/selected test runs missing the TOCTOU/mutation checks the list run already had
   (agy+codex, same finding, 2-of-2 convergence).
3. No toolchain-digest consistency check across list/full/selected runs.
4. No test proved an actual recall MISS computes correctly — added a real end-to-end
   fixture with a pre-existing failure in an untouched package.
5. A test run exiting nonzero with zero parsed outcomes could be silently treated as
   "zero failures" — added `checkExplainedExit`.
6. The TOCTOU/toolchain checks had no durable end-to-end regression detector (only a
   temporary in-session disable/rerun proved they worked) — extracted a pure
   `checkRunIntegrity` + table tests, plus one real bwrap self-mutating-test detector.
7. (Deepest, 2 sub-rounds) `checkExplainedExit` accepted PASS/SKIP-only outcomes as
   explaining an unrelated nonzero exit; then, after adding a `SeenPackages`-based
   completeness check, codex found THAT was also insufficient — a package that only
   emitted `"start"` still counted as "covered." Closed by extending
   `evidence.ParsedRun` with `TerminalPackages` (package-level pass/fail/skip actions
   ONLY) and `checkTerminalCoverage`, requiring every expected package to actually
   finish, not merely appear.

**Lesson reinforced yet again**: agy's own round-5 "weakest points" list literally named
the exact bug codex found in that same round (the `start`-event-counts-as-coverage
issue) but reasoned it away incorrectly ("correctly handled because checkExplainedExit
catches it" — false). Spotting the smell isn't the same as following it to the actual
bug. Independent verification of every claim — including my own fixes' side effects —
beats trusting either agent's confidence.

Every finding RED-proven via real disable/rerun/restore, several through the actual
bwrap sandbox. Full repo suite green throughout all 6 rounds. Pushed (f3226f9).

**Slice 2 core is DONE.** Remaining, smaller, optional Slice 2 polish (not blocking):
none currently identified — the plan's core Slice 2 ask (shadow-mode: run the full
suite, record the selection, measure recall) is fully implemented and reviewed.

Next: Slice 3 (symedit RenameSymbol) — the last piece of the coding trio, per the plan's
build order.

## Status 2026-09-10 — Slice 3 first piece: symedit pure edit-shape layer (757a88d)

`internal/coding/symedit` (edit.go): the pure parsing/validation/application layer for
Slice 3's RenameSymbol — decodes gopls' raw `WorkspaceEdit` into the plan's decided
closed subset (`documentChanges`/`TextDocumentEdit` only), resolves LSP (line, UTF-16
character) positions into byte offsets against an immutable preimage, applies validated
edits. No I/O, no syscalls, no S7 — deliberately the smallest independently-testable
increment of Slice 3, mirroring how Slice 2 started with pure `impact.go` logic before
its I/O orchestration.

Self-review before dispatch caught 2 real bugs (workspace-root containment check
accepted a sibling directory `/work/src2` as if inside `/work/src`; failed to reject
`/etc/passwd`) plus a test off-by-one.

**3 review rounds, codex+agy (kilo still dead), 8 more real bugs — each round deeper on
the same theme (this is untrusted-ish data from an external process):**
1. Required LSP fields (range/start/end/line/character/newText) decoded as valid Go
   zero values when absent from JSON — `{"newText":"X"}` with no `range` silently became
   an in-bounds insertion at byte 0. Fixed: every required field is now a pointer, nil
   after decode is refused.
2. A response with BOTH `changes` and `documentChanges` silently processed the latter,
   dropping the former.
3. An `AnnotatedTextEdit`'s `annotationId` was silently ignored, accepted as ordinary.
4. Document version was never validated at all. Closed with a rule VERIFIED LIVE, twice,
   against real gopls v0.23.0 (not guessed): the one file the session actually opened
   always carries version 1; every other file touched by a cross-file rename always
   carries version 0.
5. (Deeper) the `openedFileRelPath` parameter driving that rule was itself unvalidated —
   empty or foreign values silently degraded the check to "everything must be 0" instead
   of failing closed, and nothing confirmed the opened file actually appeared in the
   response at all.
6. (Deeper still) the "closed, explicit subset" claim remained permissive: plain
   `json.Unmarshal` silently ignores unknown fields anywhere in the structure, and
   `annotationId: null` bypassed the `*string` nil-check (null and absent decode
   identically to a pointer field). Closed with `strictUnmarshal`
   (`DisallowUnknownFields`, recursive through the whole nested struct tree in one
   Decode call) and a `json.RawMessage`-based presence check that correctly
   distinguishes "key present with any value including null" from "key absent."
7. A file URI carrying a query or fragment was silently accepted, never inspected
   (2-of-2 convergence, non-blocking note both rounds, folded anyway since cheap).

Round 3: both PASS. Full repo suite green throughout. Pushed (757a88d).

**Lesson reinforced a third time this Slice-2/3 arc**: codex kept finding genuinely
DEEPER bugs on the SAME code area across 3 full rounds (version validation → the
parameter driving it → the decode strictness underneath it) — never re-litigating a
settled point, always a NEW, more precise counterexample. Every claim (including my own
empirical "version 1 for opened, 0 for everything else" rule) was verified directly
against real gopls output or the vendored LSP source before being trusted, in both
directions.

Next: `internal/coding/workspace` — the durable, S7-governed, crash-recoverable
multi-file transaction coordinator (invariant 4: sealed-bundle before/after images,
openat2 descriptor-relative symlink defense, the full crash-recovery classification
table, restart-time `PolicyWorkspaceRollback` with its own grant). This is Slice 3's
largest remaining piece — `internal/foundation/sealedstore` (its storage dependency)
already exists from earlier P1 work.

## Status 2026-09-10 — workspace descriptor-relative I/O primitive (first piece of Slice 3's multi-file transaction coordinator)

`internal/coding/workspace/descriptor.go` + `descriptor_test.go`: the foundational
descriptor-relative filesystem primitive every future write in the transaction
coordinator goes through — `OpenRoot` (openat2, `RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS`,
no `RESOLVE_BENEATH` since an absolute root path has no beneath-ancestor), `WalkDirBeneath`
(one `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)` per path
component, never falling back to path-string resolution), `WriteFileBeneath` (atomic
create-temp+fsync+rename+fsync-directory), `StatBeneath` (identity capture for
`TargetExpectation`).

**4 review rounds, codex+agy, 5 real bugs found — every round on the SAME symlink/TOCTOU
defense theme, each deeper than the last (matching the exact pattern from impact.go,
tia, and symedit earlier in this program):**

1. (Round 1, codex HIGH) `WriteFileBeneath` performed an unconditional rename-over —
   a planted symlink at the target name was silently overwritten instead of refused,
   contradicting the plan's own identity-drift requirement. My own test originally
   encoded the WRONG behavior (asserted successful overwrite). Fixed with a mandatory
   `TargetExpectation{MustNotExist, Dev, Ino}` parameter.
2. (Round 1, codex HIGH, empirically confirmed live on this kernel) `OpenRoot` used
   plain `open()+O_NOFOLLOW`, which POSIX/Linux only apply to the FINAL path component —
   an INTERMEDIATE symlink in rootPath was silently traversed, defeating every
   downstream `RESOLVE_BENEATH` containment. Fixed with `openat2(RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)`
   against the whole path.
3. (Round 2, codex HIGH) `StatBeneath` returned Dev/Ino for ANY node including a
   symlink — `AT_SYMLINK_NOFOLLOW` meant it reported the symlink's OWN identity rather
   than following it, but never checked TYPE — so a caller could capture a pre-existing
   symlink's identity and have `WriteFileBeneath`'s dev/ino check "match" it, a
   deterministic bypass requiring no race at all. Fixed: `StatBeneath` now requires
   `S_IFREG`, refuses any non-regular node; `WriteFileBeneath` independently re-checks
   `S_IFREG` on the displaced entry as defense-in-depth.
4. (Round 2, codex HIGH) Existing-file replacement was a plain `fstatat`-then-`renameat`
   — a genuine check-then-act race window, contradicting the plan's "provably the same
   kernel object" requirement. Fixed with an exchange-verify-restore protocol:
   `renameat2(RENAME_EXCHANGE)` swaps unconditionally FIRST (so whatever is actually at
   baseName at the swap instant — not an earlier check — ends up under the temp name),
   then the displaced entry is verified (S_IFREG + Dev/Ino match); on mismatch a second
   `RENAME_EXCHANGE` restores the original untouched.
5. (Round 3, codex HIGH) The restore path's own cleanup (`unix.Unlinkat(tmpName)` after
   the restoring exchange) was unconditional — if a DIFFERENT concurrent writer replaced
   baseName in the exact window between mismatch-detection and the restoring exchange,
   THEIR content would land under tmpName by the restore swap and then be silently
   deleted. Real data-loss bug, ordinary concurrent-write timing sufficient, no active
   attacker required. Fixed: `WriteFileBeneath` captures its OWN temp file's identity
   before the first exchange; after a mismatch-restore, only unlinks tmpName if it still
   identifies that exact file — otherwise leaves it in place and returns a distinct
   manual-recovery error. Verified via a deterministic test seam (`beforeRestoreExchange`,
   a no-op func var in production) injecting the exact-window concurrent write without a
   flaky real race.

Round 4: codex PASS (independently re-verified all 3 prior fixes, ran the suite ×20
stress, confirmed the remaining hardening note — temp identity captured name-relative
after close rather than fd-relative before close — requires an actor with pre-existing
arbitrary live-workspace write authority, not a newly crossed boundary, non-blocking).

**Review-agent availability note:** kilo (w8:p3) has been dead this entire session
(permanent). agy (w8:p4) hit its account quota mid-round-3 (`Individual quota reached...
Resets in ~11h`) and never returned a round-3 or round-4 verdict — effectively dead for
the remainder of this session too. This piece therefore converged on CODEX-ONLY review
from round 3 onward, a deviation from the mandatory 3-agent process (`nexus-mandatory-agent-review`)
forced by external tool availability, not a shortcut taken by choice. Every one of
codex's 5 findings across 4 rounds was independently verified (real syscalls, live
kernel behavior, `go test -count=20` stress runs) before being accepted, and every fix
was RED-proven (each guard temporarily disabled, confirmed the specific new test failed
exactly as predicted, restored) before being called closed — the same rigor applied
throughout this whole program, just with one fewer independent reviewer than the process
calls for. Full suite: `internal/coding/workspace` 16/16 (×20 stress), `go vet` clean,
full repo `CGO_ENABLED=0 go test ./...` green throughout every round.

Committed. Next: the S7-governed durable transaction wrapper on top of this primitive
(Prepare/Apply coordination, sealed-bundle before/after-image storage via the existing
`internal/foundation/sealedstore`, the crash-recovery classification table, restart-time
`PolicyWorkspaceRollback` with its own S7 grant) — this package's own scope was
deliberately just the descriptor-relative I/O layer everything above it will call.

## Status 2026-09-10 — workspace transaction.go: same-process sealed-bundle multi-file transaction

`internal/coding/workspace/transaction.go`: the second piece of Slice 3's multi-file
transaction coordinator, built on descriptor.go's now-converged primitives. Implements
`Apply(rootFd, store *sealedstore.Store, mutations []FileMutation, bindDurable func(digest
string) error) (Result, error)` — invariant 4 steps 1/3/4/5 of 7:

- Step 1: for every mutation, capture before-state (content+identity+mode from ONE
  descriptor-relative open — `captureFileBeneath`), marshal every file's before AND after
  bytes into ONE sealed bundle, `store.Put` it BEFORE any write.
- The `bindDurable` hook fires right after `Put` succeeds, still holding the pin, still
  before any write — the seam a future S7/journal integration will use to durably record
  the bundle reference; aborts with zero writes if it fails, preserving the plan's
  required "bundle, durable reference, THEN first write" ordering even though no real
  durable reference exists in this increment.
- Steps 3/4: each file replaced atomically in order via `WriteFileBeneath`, binding the
  write's precondition to the captured before-identity, before-content digest, and
  before-mode (not just Dev/Ino).
- Step 5: on any per-file failure — including a write that DID commit but then failed a
  later step — every already-committed file is rolled back to its sealed before-image.

Explicitly NOT built here (a later increment): the actual S7 `Consume` companion-batch
pairing behind the `bindDurable` hook, the restart-time crash-recovery classification
table (step 6), the governed `PolicyWorkspaceRollback` operation (step 6's restart path).

**5 review rounds on transaction.go itself, plus a further 2 rounds purely on
`RemoveFileWithExpectedIdentity`'s own evolution (7 rounds total on this piece, on top of
the 4 already spent on descriptor.go) — codex found a real bug in every single round
except the last:**

1. (Round 1) Five HIGH findings at once: the sealed-bundle pin was released before any
   durable reference could exist, and `Apply`'s own shape couldn't support the required
   bundle→reference→write ordering without a redesign; before-image captured via two
   SEPARATE opens (`StatBeneath` then `ReadFileBeneath`) left a window for in-place
   content mutation (same inode) to go undetected; a file `WriteFileBeneath` committed
   but then errored on a LATER step (trailing fsync, cleanup unlink) was silently omitted
   from rollback since the caller only tracked `err == nil`; rollback restored the
   WRONG — after, not before — file mode (a field-conflation bug); `RemoveFileWithExpectedIdentity`
   had the exact check-then-act race `WriteFileBeneath` itself had already been hardened
   against. Fixed: `bindDurable` hook; `captureFileBeneath` (one atomic open);
   `WriteFileBeneath` changed to return `(committed bool, err error)` with `Apply`
   tracking rollback membership by `committed`, not by `werr == nil`; before/after mode
   tracked as separate fields; `RemoveFileWithExpectedIdentity` rewritten with a
   tombstone-exchange (mirroring `WriteFileBeneath`'s own exchange-verify-restore).
2. (Round 2) The tombstone-exchange rewrite still checked only Dev/Ino, not content/mode,
   on the file it was about to delete — an in-place content mutation on a rollback target
   would still be silently destroyed; its OWN success-path final unlink (removing what it
   assumed was its own tombstone) was itself still check-then-act; Openat2's `O_CREAT`
   mode is masked by the process umask, silently contradicting `FileMutation.Mode`'s
   documented "the result ALWAYS ends up with this mode" contract. Fixed:
   `TargetExpectation` gained `ContentDigest`/`CheckMode`/`ExpectMode`, checked on
   `RemoveFileWithExpectedIdentity`'s target too; its final unlink gained an own-identity
   re-verification; `WriteFileBeneath` now `Fchmod`s the temp file to the exact requested
   bits (Fchmod bypasses umask); the mode-check sentinel changed from `ExpectMode != 0`
   (fail-open for the legitimate value 0000) to an explicit `CheckMode bool`.
3. (Round 3) The round-2 "fix" to the tombstone success path only NARROWED its
   check-then-act window (re-stat immediately before unlink) rather than closing it — a
   concurrent writer could still replace the public name `baseName` in that shrunk gap
   and have their file deleted. Fixed with a full redesign (per codex's own suggested
   approach): `RemoveFileWithExpectedIdentity` now does ONE unconditional rename of
   `baseName` to a private, unpredictable quarantine name FIRST — `baseName` is
   definitively vacated before any verification happens, so there is no public name left
   to race a cleanup deletion against at all (no tombstone, no exchange partner needed —
   the goal was always just "atomically vacate baseName", which a plain rename achieves
   in one syscall). The quarantined entry is verified and deleted on match, or renamed
   back via `RENAME_NOREPLACE` on mismatch (refusing if a concurrent creator now occupies
   `baseName`).
4. (Round 4, on the redesign) The initial quarantine step used plain `Renameat`, which
   silently clobbers an existing entry at the destination — astronomically unlikely given
   `tempName`'s 128-bit randomness, but the wrong primitive at a destructive, fail-closed
   boundary when the kernel provides exact no-clobber semantics for free. Fixed with
   `RENAME_NOREPLACE` on the quarantine step too; `tempName` became a package-level var
   so a test could force a deterministic collision and prove the fix.
5. (Round 5) codex **PASS** — traced the complete current function end to end against
   invariant 4 one more time and found nothing further; the one accepted ceiling (cleanup
   of the cryptographically unpredictable private quarantine name is not itself an
   atomic verify-and-unlink operation) is explicitly non-blocking, matching the same
   accepted ceiling already on `WriteFileBeneath`'s own private-temp cleanup paths.

Every finding across all 5 rounds independently verified before being accepted (real
syscalls, `go test -count=20` stress runs, and for every fix a genuine RED-proof: the
specific guard temporarily disabled, the new test confirmed to fail exactly as predicted,
then restored). Full suite: `internal/coding/workspace` 39/39 (×20 stress), `go vet`
clean, full repo `CGO_ENABLED=0 go test ./...` green throughout every round.

**Review-agent availability note (continuing the pattern from the descriptor.go status
section above):** agy (w8:p4) remained quota-blocked (`Individual quota reached`) for
this entire piece's review history — it never returned a verdict for any transaction.go
round. This piece therefore converged entirely on **codex-only** review, the same forced
deviation from the mandatory 2-active-agent process (kilo has been permanently dead all
session) documented for descriptor.go's later rounds — not a shortcut taken by choice.
Once agy's quota appeared to reset, a full catch-up review request was dispatched
covering everything it had missed; its own pane still showed the same live quota-reached
error at dispatch time, so that catch-up review could not run either. The piece is being
committed on codex's convergence alone, with this gap recorded transparently rather than
silently treated as full 2-agent coverage.

Committed. Next: symedit's Prepare/Apply orchestration layer tying `symedit.ParseWorkspaceEdit`/
`ApplyEdits` together with `runner.RunGoplsRename` and this `workspace` package to perform
a governed, end-to-end `RenameSymbol` — Slice 3's ultimate deliverable — OR continue
building out invariant 4's remaining scope (step 2's real S7/journal wiring, step 6/7's
crash-recovery classification + `PolicyWorkspaceRollback`) first. Both remain open; the
next increment should pick whichever is smaller/more reviewable first.

## Status 2026-09-10 — symedit.Prepare/Apply: real end-to-end RenameSymbol

`internal/coding/symedit/{rename.go,discover.go}` — Slice 3's own ultimate deliverable:
`Prepare`/`Apply` ties together the three already-converged pieces (Slice 0's
`runner.RunGoplsRename`, this package's own pure `ParseWorkspaceEdit`/`ApplyEdits`, and
`workspace.Apply`) into a real, governed, end-to-end `RenameSymbol` — proven by a genuine
integration test driving a real `gopls` session against a real on-disk Go module and
verifying the resulting files on disk.

- `ExtractTouchedRelPaths` (discover.go): a loose, non-authoritative pre-pass over the raw
  `WorkspaceEdit` to learn which files are touched, solving the chicken-and-egg
  `ParseWorkspaceEdit` itself creates by requiring every touched file's preimage up front.
  `ParseWorkspaceEdit` independently re-validates everything afterward and fails closed on
  any mismatch, so a wrong/incomplete result here can never be silently trusted.
- `Prepare`: runs the gopls session, captures every touched file's preimage
  (content+identity+mode, one atomic descriptor-relative open each), and returns a `Plan` —
  the "canonical preview" invariant 3 requires — bound together by a `PlanDigest`.
- `Apply`: recomputes `PlanDigest` and refuses on any mismatch before touching anything,
  then hands `workspace.Apply` a fully-specified precondition (content digest + identity +
  mode) per file, carried through to the exact point `workspace.Apply` captures its own
  authoritative "before" state.

Small supporting exports on already-converged code, both additive: `runner.GoplsSandboxRoot`
(promoted from a literal duplicated in two places in gopls.go to one shared constant) and
`workspace.CaptureFileBeneath` (promoted from an already-existing unexported helper).

**4 review rounds, codex found a real HIGH issue in every round but the last:**

1. (Round 1) Three findings at once: gopls computes its `WorkspaceEdit` against a
   disposable snapshot of `SourceDir` taken at an unobservable internal moment — reading
   preimages via `rootFd` afterward without any check could silently resolve gopls's
   (line, character) positions against DRIFTED content, corrupting the file instead of
   failing; symedit's own separate re-hash-then-compare pass (run before `workspace.Apply`
   was even called) still left a window before `workspace.Apply`'s own later capture; the
   exported, field-mutable `Plan` had no tamper-evidence at all. Fixed: `Prepare` brackets
   the whole gopls call with two whole-tree content digests
   (`runner.DigestTreeFiles`, the same fail-closed walk `RunGoplsRename`'s own snapshot
   uses) plus a `verifyRootIdentity` sanity check; the drift precondition moved INTO
   `workspace.Apply` itself via a new `ExpectedBeforeDigest` field on `FileMutation`,
   checked at its own single authoritative capture point; `Plan` gained a `PlanDigest`
   recomputed and verified by `Apply`.
2. (Round 2) Three more: the digest sandwich still resolved everything by
   `req.SourceDir`'s own PATHNAME, not `rootFd`'s identity — a pathname swap mid-call
   would go undetected; `PlanDigest` only bound the separately-stored, independently-
   tamperable `Preimage.Digest` STRING, not the actual `Content`/`Mode` fields
   `Apply` consumes; the Prepare→Apply precondition covered content only, not identity or
   mode (an atomic replace-with-identical-bytes, or a content-preserving chmod, would go
   undetected). An attempted fix routing everything through `/proc/self/fd/<rootFd>`
   broke `RunGoplsRename`'s own internal snapshot copy (its walk Lstats its root and
   refuses to descend into what it sees as a symlink — confirmed live) — reverted in
   favor of a narrower fix: `verifyRootIdentity` called a SECOND time immediately after
   `RunGoplsRename` returns; `Preimage.Digest` removed as a stored field entirely (every
   consumer computes `sha256Hex(Content)` fresh, never a cached value);
   `workspace.FileMutation` gained `ExpectedBeforeDev`/`ExpectedBeforeIno`/
   `CheckExpectedIdentity` and `ExpectedBeforeMode`/`CheckExpectedMode`, checked at the
   same single capture point as the content digest.
3. (Round 3) The two-point digest sandwich still could not see an ABA entirely WITHIN
   `RunGoplsRename`'s own call (content changed then reverted before the sandwich's own
   endpoints observe it) — closed using data that was already being returned and simply
   never compared: `RunGoplsRename`'s own `res.SnapshotDigest` (the digest of the EXACT
   tree its internal copy captured) is now compared against the pre-call digest, and the
   initial digest call's PER-FILE digest list is retained and each preimage checked
   against its own entry. Also strengthened the round-2 pathname-swap test to swap in a
   BYTE-IDENTICAL replacement directory (same content, different inode) — cleanly
   isolating that the identity re-check, not a coincidental digest mismatch, is what
   catches it.
4. (Round 4) codex **PASS** — confirmed all three boundaries closed, both new detectors
   genuinely isolated (traced explicitly), all earlier fixes still holding. Notes only,
   non-blocking: `SnapshotDigest`/`ToolchainDigest`/`PolicyHash` aren't bound into
   `PlanDigest` since nothing currently makes an Apply decision based on them — bind them
   once a real consumer (durable approval/evidence) starts trusting them, matching this
   program's established "defer until a real consumer needs it" pattern; full preimage
   bytes in memory are fine for this increment but must go through the sealed-artifact
   owner, never raw into the journal, once durable persistence lands.

Every finding independently verified before being accepted; every fix RED-proven (the
specific guard disabled, the new test confirmed to fail exactly as predicted, restored) —
including two rounds where an initial RED-proof attempt was itself found to be
insufficiently isolated (a coincidental downstream check masked whether the NEW guard was
actually load-bearing) and had to be redone with a more precisely constructed scenario
before being trusted. Full suite: `internal/coding/symedit` 25/25 (6 of them real,
non-mocked `gopls` sessions, ~12s), `internal/coding/workspace` 44/44 (×20 stress),
`go vet` clean, full repo `CGO_ENABLED=0 go test ./...` green throughout every round.

**Review-agent availability note (same pattern as every prior piece since the descriptor.go
status section above):** agy (w8:p4) remained quota-blocked for this entire piece's review
history too — every dispatch attempt (including several where the quota's own remaining-time
countdown had visibly ticked down between attempts, confirming it was genuinely still
blocked, not merely stale) returned the same `Individual quota reached` error. This piece
converged entirely on codex-only review, once again a forced deviation from the mandatory
2-active-agent process (kilo dead the whole session), not a shortcut.

Committed. **Slice 3's own core deliverable (governed, end-to-end RenameSymbol) is now
real and working.** Remaining open scope, still explicitly deferred and documented at each
piece's own boundary: real S7/journal wiring for the `bindDurable` hook (a genuine new
operation type, not yet designed); the restart-time crash-recovery classification table
and `PolicyWorkspaceRollback` (invariant 4 steps 6/7); the Prepare-phase "format+type-check
the staged result" step; expanding beyond `RenameSymbol` to further gopls actions (Slice 4,
explicitly not before Slice 3 is deployed and dogfooded per the plan's own build order).

## Status 2026-09-10 — S7/journal wiring for `workspace.Apply` (invariant 2's own contract, round 1 FAIL + real redesign)

First pass (`internal/coding/workspace/governed.go` v1) scoped the `bindDurable` seam
narrowly off invariant 4 §2's transaction-local table alone — a genuine mistake caught
immediately by codex round 1 (**FAIL, 7 HIGH findings**): the plan's own "Cross-slice
invariants" §2 (written well before this piece, at the SAME time as invariant 4's own
first draft) already fully specifies this wiring's shape, and v1 missed most of it:
caller-chosen `op`/`target` instead of exact-intent-derived ids; `MaxAttempts: 1` making
the plan's own required same-operation retry impossible; every post-Consume failure landed
`FailedTerminal` with no distinction from an unverified/incomplete rollback (the plan
requires `Unknown` for that case, never retried blind); `s7.AttemptContext` never called
(no cancel-owner registration); no `sealed_payload_ref` wiring; failure-path
Report/Cancel errors silently discarded; and the plan's own explicitly required
`SetAppendFault` paired-batch RED detector was missing.

**Redesign, addressing 6 of 7 findings directly:**
- `ApplyOperationID(profile, rootDev, rootIno, planDigest)` /
  `ApplyTargetID(profile, rootDev, rootIno)`: exact-intent identity derived from the
  transaction root's own descriptor identity + the prepared plan's digest — never a
  caller-chosen string (mirrors `channel.ControlOperation`'s existing precedent exactly).
- `PolicyWorkspaceApply` now carries a REAL budget (`MaxAttempts: 3`,
  `Deadline: 2m`, small `Backoff`) and a new closed `s7` code,
  `s7.CodeMutationRolledBack`, meaning exactly "a fully verified, complete rollback —
  safe to retry the SAME operation."
- `transaction.go` gained one new exported sentinel, `ErrRollbackIncomplete` (wrapped via
  `errors.Join` into `rollbackAndReport`'s existing failure path when any file's rollback
  itself failed or its post-write identity was never captured) — the ONLY change to the
  already-3-round-converged file; `Apply`'s own signature and every existing behavior is
  untouched. `ApplyGoverned` branches on its presence: absent → propose
  `FailedRetryable`/`CodeMutationRolledBack` (S7 itself decides retry-vs-terminal from the
  budget); present → `Report(Unknown)`, never retried.
- `ApplyGoverned` is now a bounded retry LOOP (Begin once; Next/Consume/Apply/Report per
  attempt; a `time.After`-based wait, `ctx`-cancellable, when `Next` returns `ErrNotDue`) —
  small backoff was a deliberate choice (a synchronous, foreground-scale operation, not a
  background delivery loop).
- `s7.AttemptContext(ctx, op, ...)` is now called immediately after `Consume` succeeds
  (registers the cancel owner; bounds the attempt's wall-clock budget via the new
  `AttemptTimeout: 30s`) — documented honestly as NOT interrupting an in-flight `Apply`
  call (synchronous local descriptor I/O has no blocking wait points to select on); this
  is a structural/bookkeeping guarantee, not mid-syscall cancellation.
- New `SetAppendFault` detector (`TestApplyGovernedFaultInEitherHalfOfConsumeBatchWritesZeroFiles`,
  both event-type positions) proving the plan's own required atomicity guarantee.

**2 findings NOT fixed, deliberately, with reasoning for codex round 2 to weigh in on:**
- **Finding 1's S6.0/S6.9 half** (exact-intent identity, the OTHER half of finding 1, IS
  fixed above): `RunDurableTool` does not exist anywhere in this codebase (grep confirms
  it — the plan names it only as agy's suggested "starting shape... for Slice 3's own
  review to refine," never a built API). `S6.0`'s `PEP.Decide` lives in
  `internal/kernel/effectpath` and is called ONLY from `EffectPath.RunTool` — the
  model-tool-call dispatch pipeline. Neither of this codebase's two other already-converged
  durable effects with the SAME shape — `runner.Run` (this package's own sibling,
  `PolicyCodingRun`) and `channel/telegram`'s `registerCommands`/`ControlEffect` — calls it
  either, for the identical reason: neither is dispatched BY that loop. `ApplyGoverned` is
  the same shape — symedit's own physical-mutation primitive, never itself the point a
  model tool call is authorized (no `rename_symbol` ToolID is wired into the loop
  anywhere yet; that future wiring is what would call `EffectPath.RunTool` first, THEN
  this primitive). Governed.go's own package doc now states this explicitly, per the
  plan's own permitted alternate route ("an equally explicit alternate route that still
  enforces S6.0/S6.9... that slice's own review must specify its S6.0/S6.9
  re-enforcement mechanism explicitly — a slice-level decision, not a plan gap").
- **Finding 4 (`sealed_payload_ref`)**: verified real — `contracts.EnvelopeParams` has no
  such field, and `journal.go`'s INSERT hardcodes SQL `NULL` for it unconditionally. But
  PLAN-CODING-TRIO.md's OWN Slice-1 section (line ~1186) already recorded this exact gap
  and explicitly deferred it: "recorded here so a future Slice that DOES need it doesn't
  assume the wiring already exists." That future slice's actual consumer — the canonical
  GC/mark-and-sweep traversal — IS invariant 4 step 6/7's not-yet-built recovery piece.
  Wiring the reference now, before its only consumer exists, is machinery ahead of need
  (topknot). The bundle digest IS recorded durably today, in `EvApplyStarted`/
  `EvMutationCommitted`'s JSON payload — just not yet through the canonical column.

Full verification: `internal/coding/workspace` green ×20, `internal/coding/symedit` green
(11 real-gopls tests total across the package), `internal/kernel/s7` green, `go vet`
clean, `go build ./...` clean. Every new branch RED-proven by targeted ablation (the
Unknown-vs-Retryable branch, exact-intent id derivation, and the retry loop's own
continuation — each confirmed to fail exactly as predicted when disabled, then restored).
Dispatched for codex round 2.

## Status 2026-09-10 — S7/journal wiring round 2 FAIL + real redesign (retry actually works now)

Round 2 (**FAIL, 5 new HIGH findings**) accepted both deliberate deferrals from round 1
(S6.0/S6.9 non-applicability, `sealed_payload_ref`) but found the retry design itself
broken, and 4 other real gaps:

1. **The advertised retry could never actually succeed.** A genuine rollback restores a
   file via `WriteFileBeneath`'s atomic exchange, which always mints a NEW inode — but the
   retry reused the SAME, now-stale, original `ExpectedBeforeDev/Ino`, so its own
   precondition check refused it unconditionally. The round-1 test only proved loop
   continuation using a synthetic seam that skipped an actual write/rollback cycle
   entirely — not a real retry. **Genuine, embarrassing gap — caught only because codex
   insisted the test prove this against a REAL rollback.**
2. `AttemptContext`'s derived context was discarded — the 30s `AttemptTimeout` was
   unobservable, and a concurrent `Cancel` could land S7 `CANCELLED` while writes
   continued.
3. The wrapper reimplemented a retry-driving loop using wall-clock `time.Until` against an
   `Authority` whose clock is caller-injectable — a frozen/fake clock could spin forever.
   `s7.Execute`, this codebase's ONE canonical retry driver, explicitly refuses `Durable`
   policies; no durable counterpart exists.
4. Operation identity accepted an arbitrary caller-supplied `profile`, independent of the
   journal's own bound profile.
5. Every retry's journaled event hardcoded `AttemptNo: 1`; validators used permissive
   `json.Unmarshal` and accepted any non-empty string as a "bundle digest."

**Redesign:**
- `ApplyGoverned` now makes exactly ONE attempt per call — it does NOT own a retry loop
  (matches this codebase's only other durable-effect precedent, `channel/telegram`'s
  `registerCommands`: each call does its own Begin/Next/Consume/Report and returns
  immediately, including on `ErrNotDue`; an external caller decides whether/when to call
  again). This is a simplification (removes the buggy sleep loop entirely), not added
  complexity, and fully closes finding 3.
- `transaction.go` gained `Result.RolledBack []RolledBackFile` (each restored file's
  post-rollback identity, captured via a re-`StatBeneath` right after
  `rollbackAndReport`'s own restore succeeds) and a new exported
  `UpdateMutationsAfterRollback(mutations, rolledBack)` helper. A caller retrying after a
  verified-clean-rollback failure MUST rebuild its mutations through this helper first —
  proven by a NEW test using a REAL rollback (content-only drift on a second file,
  preserving its own inode so the test isolates exactly one moving part): the retry fails
  with the OLD bug's exact symptom when RED-proven (stale mutations reused), and succeeds
  once `UpdateMutationsAfterRollback` is used.
- `Apply` itself gained a `ctx context.Context` parameter, checked once per mutation
  immediately before that file's write — a per-file boundary check (not mid-syscall
  interruption; local descriptor I/O has no cancellable wait point once a single
  `WriteFileBeneath` call starts). `ApplyGoverned` derives `AttemptContext` right after
  `Consume` succeeds and threads it through. Two new `transaction_test.go` detectors
  (already-cancelled context; cancelled between two files' writes, proving the SECOND file
  is never attempted and the first rolls back) — both RED-proven.
- `ApplyGoverned` derives its profile from `j.Profile()` (the journal's OWN bound profile)
  — the `profile` parameter is gone entirely, closing finding 4 by removing the possibility
  of disagreement rather than adding a cross-check.
- Every companion envelope now carries the grant's real `AttemptNo`. `Events()`'s
  validators now strictly decode (no unknown fields/trailing data, mirroring `s7.Events`'s
  own discipline), require a well-formed sha256-hex `bundle_digest` (new `validSHA256Hex`),
  and validate `Landing`/`Code` against S7's own closed sets (new exported `s7.KnownCode`,
  so this vocabulary check shares ONE source of truth with S7's own `Report`/`Events`
  rather than duplicating and risking drifting from it).
- New `ErrRetryNotDue` sentinel: an immediate re-call before backoff elapses returns it
  rather than blocking.

Full verification: `internal/coding/workspace` green at -count=20 (16 tests total, incl. 7
new/rewritten this round), `internal/coding/symedit` green, `internal/kernel/s7` green,
`go vet` clean, `go build ./...` clean, full `go test ./...` green. Every new/changed
branch RED-proven by targeted ablation, including — critically — the real
retry-after-rollback test itself: reverting to stale (pre-`UpdateMutationsAfterRollback`)
mutations on the second attempt reproduces the EXACT original bug's symptom (`"a.go"'s
current identity ... does not match the caller's expected before-identity`), proving the
test genuinely exercises what it claims rather than merely compiling.

Two deferrals from round 1 stand, both accepted by codex round 2: S6.0/S6.9 non-
applicability (no `rename_symbol` ToolID is wired into the model-tool-call loop anywhere in
this codebase yet — codex's own caveat: exact-intent IDs plus S7 do not themselves
"re-enforce S6"; a future model-facing caller must still pass through the real PEP first)
and `sealed_payload_ref` (no production GC/recovery consumer exists yet — that consumer IS
invariant 4 steps 6/7, still unbuilt). Also newly, explicitly deferred: a durable,
S7-owned retry-DRIVING primitive analogous to `s7.Execute` but for `Durable` policies — this
piece's single-attempt shape sidesteps needing one (matching the telegram precedent), but a
real background retry scheduler for RenameSymbol specifically remains unbuilt; a caller
today drives retries by hand.

Dispatched for codex round 3.

## Status 2026-09-10 — S7/journal wiring round 3 FAIL + real deadlock caught + real fixes

Round 3 (**FAIL, 4 new HIGH + 1 MEDIUM**) confirmed findings 3/4 from round 2 fully closed,
but found the retry design STILL had a real correctness gap, plus a genuine reachability
concern, a cancellation gap, and validator softness:

1. **HIGH — the ungoverned mutation path remained publicly reachable.** `symedit.Apply`
   was exported and called raw `workspace.Apply` with no S7 grant at all — a careless
   production caller could bypass governance entirely. **Fixed**: unexported to
   `symedit.apply` (package-private) — only reachable from this package's OWN tests
   (same-package `_test.go`, no import needed); production callers have only
   `ApplyGoverned`.
2. **HIGH — FailedRetryable was proposed without verifying the COMPLETE mutation set is
   all-BEFORE, not just the subset Apply happened to write.** The round-2 "real rollback"
   test's own scenario proved this: it left b.go drifted, reported attempt 1 retryable
   anyway, and only repaired b.go AFTER the fact — a.go's own clean rollback was NOT
   sufficient grounds to call the WHOLE transaction verified. **Fixed**: `transaction.go`'s
   write loop now tracks its index and passes the NEVER-REACHED tail of `prep` (including
   the file whose own write just failed) to `rollbackAndReport`, which re-verifies EACH
   one's current on-disk state against its ORIGINALLY captured before-state (new
   `verifyStillBefore`, new package-level `prepared` type — moved out of `Apply`'s own
   body so both functions can share it). A content-drift failure now correctly produces
   `ErrRollbackIncomplete` (Unknown) even when the file that ACTUALLY failed had a clean
   rollback of its own — new test `TestApplyGovernedReportsUnknownWhenAnUnreachedFileHasDrifted`
   proves this directly. The round-2 retry test's OWN scenario had to be redesigned to use
   a genuine infra-style failure (a chmod'd subdirectory — EACCES creating a temp file,
   never touching the target's own content/identity) instead of content drift, since drift
   now correctly disqualifies retry.
3. **HIGH — retry was unusable/non-durable at the actual symedit boundary** (durable,
   non-forgeable precondition reconstruction across a restart does not exist). Took
   codex's own explicitly offered alternative: `PolicyWorkspaceApply`'s `MaxAttempts`
   default is now **1** — this piece no longer claims retry support. A caller that wants
   it may override the package var with a real budget + `RetryableCodes`; `ApplyGoverned`'s
   single-attempt shape and `UpdateMutationsAfterRollback` still compose correctly for
   that caller (proven by the redesigned test #2 above, using a policy override).
4. **HIGH — cancellation could still land SUCCEEDED, and a consumed ctx-cancelled attempt
   was misclassified as a code-carrying failure.** `Apply` gained a FINAL `ctx.Err()` check
   immediately before declaring `Committed: true` (every write can have succeeded, but ctx
   may have been cancelled DURING the last one). `ApplyGoverned` now detects
   `errors.Is(applyErr, context.Canceled/DeadlineExceeded)` specifically and routes to
   `Cancel`, not `Report(FailedRetryable/Terminal)` — mirroring `runner.Run`'s own
   `selfDeadlineCancelled` classification for the identical distinction. New test
   `TestApplyGovernedCancelsOperationWhenCtxCancelledMidTransaction`.
5. **MEDIUM — the failure validator admitted impossible narratives and phantom attempt
   numbers.** `validLanding` now excludes `LandingSucceeded` (this is exclusively the
   FAILURE companion). `AttemptNo` is no longer precomputed as `Attempts(op)+1` before
   `Next` even runs (which could claim a phantom attempt for an operation Next terminalizes
   via exhaustion without ever issuing a grant) — it now starts at the REAL pre-call
   consumed count and only updates to the grant's own `AttemptNo` once `Consume` actually
   succeeds.

**A genuine deadlock, caught by the test suite itself, not review**: the first attempt at
fixing #5 above made `failedBuild` call `grants.Attempts(op)` LIVE, at invocation time —
but every builder S7 passes to `Next`/`Consume`/`Report`/`Cancel` is invoked WHILE S7
already holds its own internal mutex, so a same-goroutine call back into a method that
also locks it (`Attempts`) is a same-goroutine relock: an immediate, total deadlock, hung
the entire `go test` run past its 60s timeout instead of failing fast. Diagnosed by
noticing tests were HANGING, not failing, and bisecting the exact call. Fixed by capturing
the attempt number ONCE, outside any lock, before `Next` runs, and updating that captured
variable via a plain assignment (not a method call) once `Consume` succeeds — the actual
converged design in `governed.go` now.

A second, smaller bug surfaced by the fix above: `contracts.EnvelopeParams.AttemptNo` (the
generic envelope-level field, always >=1 by the journal's own admission rule — distinct
from this package's own `attempt_no` JSON payload field, which legitimately CAN be 0,
meaning "no attempt was ever consumed") needed its own clamp; `envelope()` now clamps only
the generic field, leaving the domain-truthful value in the payload untouched.

Full verification: `internal/coding/workspace` green at -count=20 (20 tests, several
new/redesigned this round), `internal/coding/symedit` green, `internal/kernel/s7` green,
`go vet` clean, `go build ./...` clean, full `go test ./...` green. Every changed branch
RED-proven by targeted ablation (the unreached-verification pass, the ctx-cancel-to-Cancel
routing); the deadlock's own RED proof is the empirical 60s hang itself, observed directly
rather than re-derived via a clean ablation (Go's unused-variable check made a faithful
ablation of the fixed code impossible without also breaking compilation — the hang was
reproduced and root-caused via a standalone debug test instead).

Dispatched for codex round 4.

## Status 2026-09-11 — S7/journal wiring round 4 FAIL + convergence on the remaining gaps

Round 4 (**FAIL, 2 new HIGH + 2 new MEDIUM**) confirmed the deadlock fix, the whole-set
all-BEFORE verification, the chmod'd-subdirectory test technique, and `MaxAttempts: 1`'s
default are ALL correct. Convergence is close — every remaining finding was small and
contained:

1. **HIGH — `symedit.apply` was unexported, but it still called the raw exported
   `workspace.Apply`, which itself required no S7 grant.** The actual owner boundary
   (invariant 2: every dispatched `workspace.Apply` effect must be S7-governed) was still
   crossable from any package. **Fixed properly**: `workspace.Apply` is now unexported to
   `workspace.apply` too — `ApplyGoverned` is the ONLY exported entry point into a real
   mutation, from any package. This forced a genuine, positive cleanup of `symedit`'s own
   test suite: the pure "digest tamper detection" tests (`TestApplyRefusesEmptyPlan`,
   `TestApplyRefusesTamperedPlan`, `TestApplyRefusesPlanWithTamperedPreimageContent/Mode`)
   never actually needed a real filesystem write in the first place — they now call
   `planMutations` directly (simpler, faster, and more precisely isolates what they
   claim); the one test that DOES need a real on-disk precondition check
   (`TestApplyRefusesWhenFileDriftedSincePrepare`) now goes through `ApplyGoverned`; and
   the now-redundant `TestPrepareApplyEndToEndRename` (superseded by
   `TestApplyGovernedEndToEndRenameWiresRealS7Grant`, which gained its extra
   plan-shape assertions) was deleted outright — deletion over addition, one fewer real
   `gopls` invocation per test run.
2. **HIGH — the final post-write ctx-cancellation check had no RED-capable detector of
   its own.** Both existing cancellation tests used two mutations, so the SECOND file's
   own ordinary per-iteration check already caught the cancellation — ablating only the
   NEW final check left both tests green (verified directly: it does). New
   `TestApplyRefusesSingleFileCommitWhenContextCancelledDuringItsOwnWrite` uses exactly
   ONE mutation, scheduling cancellation to fire AFTER that file's own (only) per-iteration
   check already passed — there is no second iteration to mask the ablation. Confirmed:
   the two-file test stays green when the final check alone is ablated (proving it WAS
   redundant coverage, exactly as codex diagnosed); the new one-file test turns RED.
3. **MEDIUM — the failure-event validator admitted impossible (attempt_no, landing, code)
   combinations** (e.g. attempt_no=0 with Unknown, or Retry paired with an unrelated known
   S7 code). Replaced the bare closed-landing-enum check with an exact
   `validApplyFailedNarrative` compatibility table scoped to what THIS owner actually
   emits: Cancelled/Terminal allow any attempt_no, code must be `""` for Cancelled;
   Retry/Unknown both require a CONSUMED attempt (attempt_no>=1); Retry requires
   `CodeMutationRolledBack` specifically, Unknown requires empty code; any other non-empty
   code is rejected outright (this owner never legitimately produces one). The
   now-unused, this-round-added `s7.KnownCode` export was removed again rather than left
   as dead kernel surface.
4. **MEDIUM — a `Consume`-succeeded-but-`AttemptContext`-failed gap between Consume and
   the write loop was misclassified as `CodeMutationRolledBack`** ("verified rollback,
   safe to retry") even though Apply's write loop was never entered — nothing was ever
   mutated, so nothing was ever rolled back. New `deriveAttemptContext` test seam
   (mirrors `applyFn`'s own pattern — a real clock race here would need a
   microsecond-precision window with no test-visible hook) lets a test force this
   deterministically. Fixed: this specific case now routes to the SAME `Cancel` branch as
   a ctx cancellation/deadline (the attempt never got to run because its own timing bound
   was already gone — the same shape, not a code-carrying failure). New
   `TestApplyGovernedCancelsWhenAttemptContextFailsAfterConsume`.

Full verification: `internal/coding/workspace` green at -count=20, `internal/coding/symedit`
green (now 1 fewer real-`gopls` test, same coverage), `internal/kernel/s7` green, `go vet`
clean, `go build ./...` clean, full `go test ./...` green. Every changed branch RED-proven
by targeted ablation, including the negative-control proof that the round-3 two-file
cancellation test does NOT independently exercise the final check (ablating it leaves that
test green, confirmed directly) — exactly the gap codex identified.

Dispatched for codex round 5.

## Status 2026-09-11 — S7/journal wiring round 5 FAIL + 2 more real fixes

Round 5 (**FAIL, 2 HIGH**) confirmed the round-4 fixes for the final-cancellation detector
and the `neverWrote`/`AttemptContext` classification are both correct, but found 2 more:

1. **HIGH — the round-4 exact-narrative table was ITSELF too strict, and codex proved it
   with a live reproduction, not just a hypothetical**: S7's own `Cancel` preserves the
   PRIOR report's code as `rec.lastCode` when building its companion — so cancelling an
   operation that already landed `FAILED_RETRYABLE`/`CodeMutationRolledBack` once (a
   legitimate step in the caller-driven retry path this package explicitly documents and
   supports) produces exactly `(attempt_no>=1, Cancelled, CodeMutationRolledBack)` — which
   the round-4 table rejected outright, making the durable `Cancel` append itself fail
   validation and **stranding the real operation in AUTHORIZED forever**. Fixed: `Cancelled`
   and `Terminal` now both accept `code == "" || (code == CodeMutationRolledBack &&
   attemptNo >= 1)` (Terminal has the identical possibility for the identical reason — Next's
   own exhaustion path echoes the same `rec.lastCode`). New
   `TestApplyGovernedCancelsRetryEligibleOperationCarryingPriorRetryCode` reproduces codex's
   EXACT sequence (attempt 1 fails with a genuinely verified rollback via the chmod
   technique, lands `FAILED_RETRYABLE`; attempt 2 deliberately reuses the STALE
   pre-rollback mutations — the same real bug the sibling retry test proves
   `UpdateMutationsAfterRollback` fixes — so it refuses pre-Consume, landing `Cancelled`
   via the exact narrative that used to fail validation) — RED-proven against the round-4
   table verbatim (reproduces the identical AUTHORIZED-stuck symptom, then fixed).
2. **HIGH — `ApplyGoverned` was not actually the only exported mutation entrypoint**:
   `workspace.WriteFileBeneath` and `workspace.RemoveFileWithExpectedIdentity`
   (descriptor.go's own atomic-replace/quarantine-remove primitives) remained exported,
   requiring no S7 grant, with zero current cross-package callers to break. Unexported
   both (`writeFileBeneath`/`removeFileWithExpectedIdentity`) — the read-only descriptor
   helpers (`OpenRoot`, `WalkDirBeneath`, `StatBeneath`, `CaptureFileBeneath`) stay
   exported as before. `ApplyGoverned`'s claim to be the sole exported path into a real
   mutation is now actually true.

Full verification: `internal/coding/workspace` green at -count=20 (23 tests now),
`internal/coding/symedit` green, `internal/kernel/s7` green, `go vet` clean, `go build
./...` clean, full `go test ./...` green. The new retry-cancellation test RED-proven
against the exact round-4 table (reproduces codex's own live reproduction verbatim before
the fix, passes after).

Dispatched for codex round 6.

## Status 2026-09-11 — S7/journal wiring CLOSED: round 6 PASS

Round 6: **PASS**, plainly. Both round-5 findings independently re-verified closed (codex
restored the OLD Cancelled-requires-empty-code branch in a disposable copy and confirmed
`TestApplyGovernedCancelsRetryEligibleOperationCarryingPriorRetryCode` turns RED with the
identical `state = AUTHORIZED` symptom, then confirmed the current code passes ×20; ran an
exhaustive scan of `internal/coding/workspace` for any other exported function performing
a physical mutation — found none, only read/walk/stat/capture helpers plus
`ApplyGoverned`). One editorial-only note (a stale sentence in `validApplyFailedNarrative`'s
own doc comment still said Cancelled always requires an empty code, though the
implementation and the rest of the comment already had it right) — fixed.

**This closes S7/journal wiring for `workspace.Apply`/RenameSymbol as its own converged
piece — 6 review rounds, each of the first 5 finding something real** (round 1: missed
invariant 2's own pre-existing contract entirely — exact-intent IDs, a real retry budget,
`AttemptContext`; round 2: the retry design was fundamentally broken — rollback always
mints a new inode, silently making the advertised retry impossible; round 3: still didn't
verify the WHOLE mutation set was all-BEFORE, only the written subset, PLUS a genuine
deadlock I introduced fixing round 2's own attempt-numbering bug, caught by the test suite
hanging; round 4: the raw `workspace.Apply` bypass was still exported, the final
cancellation check had no independent detector, two narrower validator gaps; round 5: the
round-4 validator fix was itself too strict — codex reproduced a live sequence where
cancelling a legitimate retry-eligible operation got permanently stuck `AUTHORIZED` — plus
two more raw mutation primitives were still exported). The empirical lesson already
recorded twice this program (workspace.Apply's own 7 rounds, symedit's Prepare/Apply's 4
rounds) held a third time, at even greater length: one clean test run and one reviewer PASS
is never sufficient for concurrent/filesystem/security-sensitive code — only sustained,
adversarial, round-after-round pressure converges it.

**What is now real and governed**: `ApplyGoverned` is the sole exported entry point into
any workspace mutation, from any package; exact-intent S7 operation identity derived from
the journal's own bound profile + root identity + plan digest; a durable
`workspace.apply_started`/`workspace.mutation_committed`/`workspace.apply_failed` event
trio with an exact, owner-scoped narrative-validation table; per-file ctx-cancellation
checks (including the specific post-last-write gap) correctly routed to S7 `Cancel`, never
a code-carrying failure; a genuinely verified all-BEFORE classification (re-checking every
mutation, not just the ones actually written) gating the ONE retryable code this owner
proposes; `RolledBack`/`UpdateMutationsAfterRollback` giving a caller-driven retry (for a
caller that opts in via a policy override — the default `MaxAttempts: 1` makes no retry
claim) a real, working path, proven end-to-end against an ACTUAL rollback, not a synthetic
stand-in.

**Still explicitly, deliberately NOT built** (unchanged from earlier rounds, restated for
anyone resuming this plan): the restart-time crash-recovery classification table and the
governed `PolicyWorkspaceRollback` operation (invariant 4 steps 6/7); `sealed_payload_ref`
wiring (no production GC/recovery consumer exists yet — that consumer IS the item above);
S6.0/S6.9 re-enforcement at a model-facing `rename_symbol` tool boundary (doesn't exist yet
in this codebase — `ApplyGoverned` is a primitive a future tool-dispatch layer would call
AFTER its own PEP check, mirroring `runner.Run`'s identical, already-accepted shape); a
durable, S7-owned retry-DRIVING primitive analogous to `s7.Execute` but for `Durable`
policies (this piece's single-attempt shape sidesteps needing one; a real background
retry scheduler for RenameSymbol specifically remains unbuilt).

Final verification before this status: `go build ./...` clean, `go vet ./...` clean,
`internal/coding/workspace` green at -count=20, `internal/coding/symedit` green,
`internal/kernel/s7` green, full `go test ./...` green.

## Status 2026-09-11 — Prepare-phase staged-formatting check (invariant 3, format half)

Next smallest piece after S7/journal wiring: invariant 3's own explicit Prepare-phase
requirement, "format+type-check the staged result" — previously explicitly deferred in
full. Scoped down to its FORMAT half only (the type-check half needs staging into a
disposable snapshot and running a real `go build`/`go vet` through Slice 0's sandboxed
runner — a materially larger piece of its own, still explicitly deferred, not silently
dropped).

New `symedit.verifyStagedFormatting(edits, preimages)`: stages every touched file's edit
via `ApplyEdits` — the SAME function `Apply` itself later uses, so this checks EXACTLY the
bytes that will be written, not a separately-computed approximation — and refuses the
whole Plan unless the result both parses as valid Go (`go/format.Source`'s own parse step,
catching every syntax error a bad byte offset could produce) and is byte-identical to
gofmt's own output. Rationale: `gopls` performs a byte-offset rename, never a reformat, so
a CORRECT `WorkspaceEdit` against valid Go source should always already be gofmt-clean —
a mismatch is treated as a real edit-shape defect (a wrong byte offset, a straddled
multi-line edit), never silently reformatted away, since silently changing content after
capture would also undermine `PlanDigest`'s own tamper-evidence guarantee. Wired into
`Prepare`, right after `ParseWorkspaceEdit` succeeds and before `PlanDigest` is computed.

4 new pure unit tests (no gopls needed, stdlib-only): accepts a well-formed edit; refuses
one that stages invalid Go syntax; refuses one that stages syntactically valid but NOT
gofmt-clean Go; refuses a touched file with no recorded preimage. The existing real-gopls
end-to-end test (`TestApplyGovernedEndToEndRenameWiresRealS7Grant`) continues to pass with
this check now active — confirms it is not a false-positive trap against genuine `gopls`
output.

Full verification: `internal/coding/symedit` green (including all real-gopls tests),
`go vet` clean, `go build ./...` clean, full `go test ./...` running, will confirm
separately. Dispatched for codex review.

## Status 2026-09-11 — staged-syntax check round 1 FAIL + real fixes

Round 1 (**FAIL, 2 HIGH**), both real:

1. **The gofmt-byte-identity half of the check rejected two real, already-contracted
   cases.** Codex reproduced both live: a CRLF file (gofmt normalizes CRLF→LF, but
   `edit_test.go`'s own `TestParseWorkspaceEditPreservesCRLF` already requires CRLF
   survive byte-exact) and a file that was already not gofmt-clean BEFORE the edit
   (gofmt reformats the WHOLE file, including parts the edit never touched — not a
   defect IN the edit). **Fixed by narrowing, not patching**: dropped the
   gofmt-byte-identity requirement entirely — `verifyStagedSyntaxIsValid` (renamed from
   `verifyStagedFormatting` for honesty) now checks ONLY that the staged result parses as
   valid Go (`go/format.Source`'s own parse step, its formatted output never compared
   against). New test proves both previously-broken cases now pass; the "refuses
   unformatted" test — whose own premise was the bug — is gone, replaced by this
   acceptance test.
2. **The Prepare-phase wiring itself had no RED-capable detector.** Codex proved it by
   ablating the production call and running the full real-gopls suite — stayed green. New
   `beforeStagedSyntaxCheck` test seam (mirrors this file's own
   `beforeGoplsRename`/`afterGoplsRename` pattern) lets a test inject an edit that stages
   invalid Go into the REAL edits a REAL end-to-end gopls-driven `Prepare` call is about to
   check. RED-proven directly: ablating the production call reproduces codex's own
   finding exactly (the test fails, Prepare wrongly returns a plan).

Full verification: `internal/coding/symedit` green (17 tests, all real-gopls tests still
passing), `go vet` clean, `go build ./...` clean, full `go test ./...` green. Dispatched
for codex round 2.

## Status 2026-09-11 — staged-syntax check round 2 FAIL + real fix

Round 2 (**FAIL, 1 HIGH**): both round-1 findings independently re-verified closed. New
finding: `go/format.Source`'s own documented contract accepts EITHER a complete file OR a
bare list of declarations OR a bare list of statements as "valid" — it exists to format
snippets, not to validate a complete `.go` file. Codex reproduced live: replacing an entire
valid file with `func Bar() {}\n` (no package clause at all) made the check return `nil`.
**Fixed**: switched from `go/format.Source` to `go/parser.ParseFile`, which has no such
fragment leniency — it always requires a complete source file. 2 new regression tests
(declaration-only fragment, statement-only fragment, both previously silently accepted).
Also fixed the round-1 wiring test per codex's own suggestion: its injected edit used an
out-of-bounds `EndByte: 999`, which could have been caught by `ApplyEdits`'s own bounds
validation rather than actually reaching the parser; it now replaces the whole file (an
in-bounds edit) with a declaration fragment, so the wiring test genuinely exercises the
same leniency gap the fix closes, not just "some error occurred." RED-proven: reverted to
`format.Source` and confirmed all 3 tests (2 new fragment tests + the wiring test) fail
exactly as predicted, then restored.

Full verification: `internal/coding/symedit` green (19 tests), `go vet` clean, `go build
./...` clean, full `go test ./...` green. Dispatched for codex round 3.

## Status 2026-09-11 — crash-recovery classification (invariant 4 step 6, first piece)

Explicit user authorization to start invariant 4 steps 6/7 (previously deferred as "large
and undesigned" — every prior status section calls this out). Scoped to the SMALLEST
reviewable first piece, matching this whole program's own established build order (a
pure primitive before the governed operation that uses it, exactly like descriptor.go
before transaction.go before governed.go): **classification only** — given a sealed
`Bundle`, tell the caller what state the write set is ACTUALLY in on disk right now.
Deliberately NOT built in this piece: the restart-time SCAN that finds every rehydrated-
`UNKNOWN` `workspace.apply` operation and calls this classification (needs a caller
outside this package — daemon startup, most likely); the governed
`PolicyWorkspaceRollback` operation invariant 4 §2 specifies for the AllAfter/Mixed cases
(its own new S7 durable operation type — a materially larger piece, its own reviewable
increment); the `Reconcile(false)`/`Next` calls the classification table specifies happen
AFTER classification decides what to do.

New `internal/coding/workspace/recovery.go`:
- `Bundle`/`FileRecord` — the SAME types `transaction.go`'s sealed-bundle JSON already
  used, exported (capitalized) so a caller outside this file can decode a bundle read back
  via `sealedstore.Store.Get` without knowing the JSON shape by hand — no wire-format
  change, purely a visibility widen.
- `DecodeBundle([]byte) (Bundle, error)` — strict decode (no unknown fields, no trailing
  data, mirrors this codebase's own established discipline for every closed-shape
  decoder), refuses an empty file list.
- `FileState` (Before/After/Foreign) + `FileClassification` + `TransactionState`
  (NeverStarted/MidCrash/Foreign) + `ClassifyBundle(rootFd int, b Bundle) ([]FileClassification, TransactionState, error)`
  — re-reads every bundle file's CURRENT on-disk state via the SAME descriptor-pinned
  rootFd discipline every other capture in this package uses, classifies each against the
  bundle's captured before/after images (content digest + mode, matching Apply's own
  precondition-check discipline exactly), and summarizes the whole transaction per
  invariant 4 step 6's own exhaustive table. Foreign takes priority — one foreign file
  makes the WHOLE transaction Foreign, even if every other file is a clean After.

10 tests: all-before/all-after/mixed/any-foreign/new-file-never-created/missing-
previously-existing-file/content-matches-but-mode-does-not (all pure, hand-built Bundle
fixtures) + 3 `DecodeBundle` shape-validation tests + 1 integration test proving this
layer's own documented boundary is real: classifying a REAL successful `apply()` call's
own sealed bundle (read back exactly as a future restart scan would) is INDISTINGUISHABLE
from MidCrash by design — this layer alone cannot tell "succeeded" from "crashed after the
last write"; that distinction is deliberately deferred to the not-yet-built restart scan
(which also checks whether `workspace.mutation_committed` exists in the journal).

Full verification: `internal/coding/workspace` green at -count=20 (33 tests, 10 new),
`internal/coding/symedit` green, `go vet` clean, `go build ./...` clean, full `go test
./...` green. Every branch RED-proven by targeted ablation, including one gap caught
mid-review by my own fight-the-fix pass (an untested mode-mismatch-with-matching-content
case) — added its own dedicated test before considering the ablation complete, not just
noted as a caveat. Dispatched for codex review.

## Status 2026-09-11 — crash-recovery classification round 1 FAIL + real fixes

codex round 1 verdict: **FAIL**, two HIGH findings, both reproduced live through
the real package APIs (no hypotheticals):

1. **`DecodeBundle` not actually strict.** The trailing-data check tested
   `dec.Token()` returning `nil` as "has trailing data" — but invalid trailing
   *garbage* (not just a second JSON value) makes `Token()` return a syntax
   error, which the check let straight through. Fixed by matching this
   package's own established `strictDecode` pattern (`internal/kernel/s7/s7.go`
   `strictDecode`, already `err != io.EOF`): require exactly `io.EOF`, reject
   everything else. The identical bug existed in `governed.go`'s
   `strictDecodeEvent` — swept in the same fix.

   Worse: the decoder validated unknown fields but not *required*-field
   *presence* — a bundle entry missing `after`/`after_mode` decoded as a
   zero-value (empty-file, mode-0) after-image instead of being refused, which
   could make a genuinely MISSING on-disk file misclassify as `FileBefore`.
   Fixed by decoding through a private `wireFileRecord` DTO with pointer
   fields (`*bool`/`*[]byte`/`*uint32`) so JSON-absent is distinguishable from
   a legitimate zero value; `DecodeBundle` now requires `existed`, `after`,
   `after_mode` always, and `before`/`before_mode` whenever `existed=true`,
   plus rejects duplicate `(rel_dir, base_name)` targets.

2. **No-op mutation misclassified as a completed write.** `apply` does not
   forbid a mutation whose `After` bytes+mode equal its `Before` image, and it
   seals the bundle (`store.Put`) before ever checking `ctx.Err()`. codex
   reproduced: existing unchanged file + a no-op mutation + a pre-cancelled
   context → zero writes performed, but `classifyOneFile` checked the AFTER
   image before the BEFORE image, so the overlapping match resolved to
   `FileAfter` → `TransactionMidCrash`. A future restart-scan would then drive
   a spurious rollback of a transaction that never touched disk. Fixed by
   swapping `classifyOneFile`'s check order: BEFORE is tested first, so an
   ambiguous overlap resolves conservatively as "nothing happened here yet."

Both fixes RED-proven via ablation against the real defect before restoring:
- `TestDecodeBundleRefusesInvalidTrailingGarbage` — reverting the `io.EOF`
  check → FAIL exactly as predicted (garbage accepted).
- `TestDecodeBundleRefusesMissingRequiredAfterFields` — reverting the
  presence checks → FAIL exactly as predicted (missing fields silently
  zero-filled).
- `TestClassifyBundleNoOpMutationCancelledBeforeWriteIsNeverStarted` —
  reverting the BEFORE/AFTER check order → FAIL with `state = MID_CRASH, want
  NEVER_STARTED`, the exact symptom codex reported.

3 new tests added (13 total in recovery_test.go), all GREEN restored;
`go vet ./...` clean; `internal/coding/workspace -count=20` green; full `go
test ./...` re-run. codex round 2 dispatched.

**Confidence:** DecodeBundle's shape/presence validation and the
BEFORE/AFTER overlap resolution are now verified against the exact
counterexamples codex constructed. Not independently re-audited for OTHER
malformed-shape variants beyond what round 1 found — that's what round 2 is
for. Weakest link: `classifyOneFile`'s conservative BEFORE-first resolution
is a heuristic (favor false-negative-as-safe over false-positive-as-unsafe),
not a proof — it's correct because a real content-changing mutation can never
have `After == Before`, but that invariant lives in `apply`'s caller
discipline, not in a type the compiler enforces.

## Status 2026-09-11 — crash-recovery classification round 2 FAIL + real fix

codex round 2 verdict: **FAIL**, one HIGH finding (round 1's other two findings
CLOSED and confirmed — trailing-garbage rejection and the BEFORE/AFTER overlap
fix both held). The new finding: round 1's own fix introduced a producer/
consumer incompatibility. Reproduced live:

`apply(existing empty file)` → `sealedstore.Get` → `DecodeBundle` rejected
its own producer's artifact with `existed=true but is missing required field
"before"/"before_mode"`.

Root cause: `FileRecord.Before`/`BeforeMode` carry `,omitempty` in
`transaction.go`, so a real (empty) before-image gets DROPPED from the wire —
but round 1's presence-tracking decoder (`*[]byte`/`*uint32` pointer fields)
demanded it be present whenever `existed=true`. A second round-trip break:
`FileMutation.After == nil` legitimately means "create an empty file" — the
encoder emits `"after":null`, and `*[]byte` decodes an explicit JSON `null`
identically to an absent key, so that legitimate bundle was rejected too.
Presence-pointers cannot distinguish "key absent" from "key present with its
zero value" for byte-slice content — Go's own JSON semantics make those two
cases indistinguishable on the wire once `omitempty`/`nil` are involved.

Fix: dropped the whole presence-tracking DTO. `DecodeBundle` now decodes
directly into `Bundle` — the SAME shape `apply`'s own `json.Marshal` produces
— guaranteeing symmetric round-tripping by construction, then validates
*values* (not presence) after decode: empty `base_name` refused, duplicate
`(rel_dir, base_name)` refused, `existed=false` paired with a nonzero
`before`/`before_mode` refused (an internally inconsistent record — codex's
own suggested fix), and `after_mode`/`before_mode` outside the `0o777`
permission-bit vocabulary refused (also codex's suggestion, closes the "mode
4294967295" counterexample). `existed=true` with an OMITTED before-image is
now accepted (classification naturally resolves it as `FileForeign` if it
can't verify the file, matching invariant 5's fail-safe-toward-closure — this
is deliberately NOT re-added as a decode-time requirement, since that's
exactly what broke round-tripping).

Declined one item from codex's "concrete fix" list: presence-tracking
`rel_dir`. `rel_dir=""` (root directory) is a fully legitimate value,
indistinguishable in meaning from an omitted key — there is no malformed
state this would catch that isn't already caught by the `(rel_dir,
base_name)` duplicate check or the base_name-empty check. Flagging this as a
disagreement rather than silently skipping it; will revisit if round 3 finds
a concrete exploit through it.

RED-proven via ablation before restoring: reverting the existed-false/before
consistency check → FAIL exactly as predicted; reverting the mode-range check
→ FAIL exactly as predicted. Added 2 round-trip regression tests reproducing
codex's exact repro (existing-empty-file bundle, new-empty-file bundle via
`After: nil`) — both GREEN against the fix, both would have failed against
round 1's version. 18 tests total in recovery_test.go now.

`internal/coding/workspace -count=20` green, `go vet ./...` clean, full `go
test ./...` re-run. codex round 3 dispatched.

**Confidence:** The two concrete round-trip repros codex gave are now covered
by regression tests and pass. Not proven: that no OTHER apply-producible
shape still breaks round-tripping — decoding into the exact same struct
apply encodes from makes an unknown regression class far less likely than
round 1's parallel-DTO approach, but it's not a formal proof of encoder/
decoder symmetry. Weakest link: the `existed=true`-with-omitted-before case
now silently falls through to classification-time Foreign resolution rather
than being caught at decode time — correct per invariant 5, but relies on
classifyOneFile's hash comparison actually failing safe, not on an explicit
decode-time check.

## Status 2026-09-11 — crash-recovery classification round 3 FAIL + real fix

codex round 3 verdict: **FAIL**, one HIGH finding (rounds 1+2's findings all
CLOSED and confirmed, including the empty-file round-trip repros and the
declined rel_dir presence-tracking — codex independently agreed that decision
is safe: "omitted rel_dir and explicit rel_dir:'' have the same root-directory
meaning... I found no distinct unsafe state").

New finding: `FileMutation.Mode` permits ANY permission value, including
`0000` (no owner-read). `apply` happily commits such a mutation — but restart
classification (`classifyOneFile`) must reopen the file `O_RDONLY` to hash
it, and no pre-chmod descriptor survives a crash. codex reproduced live: a
new file created at mode 0 and an existing readable file replaced with mode
0 both commit successfully through `apply`, decode successfully through
`DecodeBundle`, and then `ClassifyBundle` fails outright with `permission
denied` — a legitimate transaction that a restart scan could never classify
at all, not even to FOREIGN (the whole point of invariant 4 step 6 is that
classification ALWAYS produces an answer).

Fixed at the source, not just the symptom: `apply` (`transaction.go`) now
refuses any mutation whose target mode lacks the owner-read bit
(`m.Mode.Perm()&0o400 == 0`) in the SAME upfront pre-check pass as the
existing duplicate-target check — fail closed, before any write, no
mutations partially applied. `DecodeBundle` (`recovery.go`) gained the
matching decode-time guard: `after_mode` (always) and `before_mode` (when
`existed=true`) without the owner-read bit are refused as "impossible
producer narratives," now that `apply` itself never legitimately produces
one — defense in depth against a bundle from a future/different producer.

Both RED-proven via ablation: reverting the `apply`-side check →
`TestApplyRefusesUnreadableAfterMode` fails exactly as predicted (the mutation
commits instead of being refused); reverting the `DecodeBundle`-side check →
`TestDecodeBundleRefusesModeWithoutOwnerRead` fails exactly as predicted.
2 new tests (20 total in recovery_test.go). Existing workspace suite (`apply`,
`ApplyGoverned`, rollback, cancellation tests) all still green — no mode-0
usage anywhere else in the suite to collide with the new refusal.

`internal/coding/workspace -count=20` green, `go vet ./...` clean, full `go
test ./...` re-run. codex round 4 dispatched.

**Confidence:** The exact repro codex gave (mode-0 create and mode-0 replace,
both through the real `apply`/`ClassifyBundle` pipeline) is now refused
before any write, and the decode-side belt-and-suspenders check is
independently RED-proven. Not proven: whether some OTHER mode value combined
with a symlink/special-file edge case could still produce an unreadable
after-image outside the mode-bit check's coverage (e.g. a target directory
itself losing execute/search permission, which isn't checked here at all —
out of scope unless round 4 surfaces it as a real reproducible gap, since
`WalkDirBeneath`'s own descriptor-pinned discipline already governs
directory traversal, not this piece).

## Status 2026-09-11 — crash-recovery classification round 4 FAIL + real fix

codex round 4 verdict: **FAIL**, one HIGH finding (round 3's fix confirmed
correct on the after_mode side; rounds 1-3's other findings all still CLOSED,
including the nested-directory question round 4 was specifically asked to
check — codex found no defect: `apply` never mutates directory permissions,
and both capture and classification are descriptor-relative via
`WalkDirBeneath`, so an external directory permission change simply fails
closed as an error rather than a silent misclassification).

New finding: round 3's `before_mode` owner-read check was a wrong
"symmetric" assumption. The after-mode rule is sound BECAUSE every file
`apply` *writes* is owned by the NEXUS process itself — but `before_mode` is
the captured mode of a PRE-EXISTING file, which can legitimately belong to a
DIFFERENT UID and be readable only through group/other permission bits.
`apply`'s own `CaptureFileBeneath` already proved the file was readable AT
capture time (it's how `before` got captured at all) — mode bits alone don't
encode ownership, so requiring the owner-read bit specifically on
`before_mode` is simply false. codex reproduced with a real cross-UID
fixture (mode 0044, owned by a different UID via bubblewrap uid-mapping,
readable through other-bits): `apply` legitimately captured and replaced it,
but `DecodeBundle` rejected its own producer's bundle as an "impossible
producer narrative" — precisely the round-1/2 failure mode recurring for a
third owner-read-adjacent reason.

Fixed: removed the `before_mode&0o400==0` check entirely, keeping only the
`after_mode` owner-read requirement (justified by "every file apply *writes*
is owned by NEXUS," which genuinely does not apply to `before_mode`).
Replaced the wrong test assertion
(`TestDecodeBundleRefusesModeWithoutOwnerRead`'s second case) with
`TestDecodeBundleAcceptsBeforeModeWithoutOwnerReadBit`, reproducing codex's
exact repro value (`before_mode: 36` = `0o044`) as a decode-level unit test
(no bubblewrap/uid-mapping needed at the unit level — the decoder's contract
doesn't care HOW the mode got there, only that it must be accepted).
RED-proven via ablation: reintroducing the wrong before_mode check →
`TestDecodeBundleAcceptsBeforeModeWithoutOwnerReadBit` fails with the exact
error codex's live repro produced, character for character. 21 tests total
in recovery_test.go now.

`internal/coding/workspace -count=20` green, `go vet ./...` clean, full `go
test ./...` re-run. codex round 5 dispatched.

**Confidence:** This is the third consecutive round where a decode-time
"require X" check turned out to reject something `apply` can legitimately
produce (round 1: presence of before/after; round 2: same via a different
mechanism; round 4: before_mode's owner-read bit). The pattern itself is
now a signal: worth treating the NEXT candidate "require" check on
`DecodeBundle` with active suspicion — verify against a REAL `apply`-produced
artifact before proposing it, not just against a hand-built malformed shape.
Weakest link unchanged: no formal proof that `apply`'s encoder and
`DecodeBundle`'s validation are exhaustively symmetric — only reproduced-repro
coverage, which is why each round's fix keeps surfacing a new
producer/consumer mismatch instead of a wholly new defect class.

## Status 2026-09-11 — crash-recovery classification CLOSED: round 5 PASS

codex round 5 verdict: **PASS**. Cross-checked every `DecodeBundle`
validation rule against real `apply`-produced artifacts (not just hand-built
malformed JSON) as explicitly asked: empty base_name, duplicate target,
existed=false/before consistency, mode-range, and after_mode owner-read all
agree with `apply`'s own producer language — none of them reject anything
`apply` can legitimately produce. Also independently verified the after_mode
owner-read assumption itself is sound at the mechanism level: `writeFileBeneath`
creates the replacement file itself and applies the exact permission bits via
`Fchmod` (`descriptor.go:237/251`) — no path exists for `apply` to produce an
after-file NEXUS doesn't own, setgid directories or symlinks included.

Five review rounds total, closing (in order): (1) `DecodeBundle`'s trailing-
data check + presence-tracking gap; (2) round 1's own presence-tracking DTO
breaking round-trips on `apply`'s legitimate empty-file bundles; (3) mode-0
(no owner-read) making a committed transaction permanently unclassifiable;
(4) the `before_mode` owner-read check wrongly generalizing the after_mode
rule to a case where the ownership assumption doesn't hold. Final state:
`internal/coding/workspace/recovery.go` (`DecodeBundle`, `ClassifyBundle`,
`classifyOneFile`, `FileState`/`TransactionState`), one new owner-read
pre-check in `transaction.go`'s `apply`, one matching trailing-data fix swept
into `governed.go`'s `strictDecodeEvent`, 22 tests in `recovery_test.go`.

Full verification: `go build ./...` clean, `go vet ./...` clean,
`internal/coding/workspace -count=20` green (55 tests), full `go test ./...`
green across all 44 packages. Committing and pushing.

**Confidence:** High on what this piece actually claims — read-only,
descriptor-relative classification of a sealed bundle against invariant 4
step 6's table, verified against 5 rounds of adversarial review including
live cross-UID and unreadable-mode reproductions. Explicitly NOT proven (per
codex's own "proof ceiling" note, consistent across all 5 rounds): classification
reads the filesystem as a SEQUENCE of independent reads, not an atomic
snapshot — a concurrent external mutation between classifying file A and
file B is a real gap the future restart-scan/rollback executor must close
with its own mutation-time revalidation, not something this read-only piece
can or should solve. The restart-time SCAN and the governed
`PolicyWorkspaceRollback` operation remain the next pieces of invariant 4
steps 6/7, each its own reviewable increment per this program's established
build order.

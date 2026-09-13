# SECTION-MAP — plan od početka do kraja (dependency-order + status + design-index)

Cilj: plan jasan start-to-end, svaka sekcija jasna, PRIJE PRD-a. Jedino P0/P1/P2 scope-tiering
čeka PRD (#6). Status-legenda:
- **✅ DIZAJN-GOTOV** — postoji buildable Go dizajn (DESIGN-*.md), spreman za kod.
- **🔵 AUDITAN** — ima full-code-auditani svjetski izvor (MASTER-LANDSCAPE), treba Go-dizajn pass.
- **🟡 DIZAJN-TREBA** — MEHANIZAM sa skicom u planu, ali bez dediciranog dizajna/RED-a.
- **🟠 ODLUKA-PRD** — tanko / scope-ovisno / identitet; čeka PRD.
- **⬇ DESCOPE-v2** — svjesno odgođeno.

---

## 1) DEPENDENCY / BUILD-ORDER (DAG — po ID-u, ciklusi razriješeni; REVIEW2 K4)

Ključni fix: gdje je bila cirkularnost, jezgreni MINIMALNI ugovor ide rano, PUNA značajka kasnije.

```
FAZA K0 (primitivi — ništa ne ovisi prije):
  S0.1-0.5 contracts/machine/negotiation/schema/closure   ✅
  EventJournal paket (P0.3 write-owner — NIJE S0.3; S0.3=capability-negotiation)   ✅
  S1.1 config · S1.2 paths + PROCESS-IDENTITY primitiv (PID+start-token)  (→S0)
  S1.3 telemetry (projekcija journala)
  S8.1-min (ContextBudget.Measure+HardLimit) · S11.1-min (Assembler.Base — bez skills/archmap)
  S16.6-det (Checker deterministic: coding.diff_exit+state_invariant) — izdvojeni MIN ugovori (fix V-K4)

FAZA K1 (sigurnost+pouzdanost+workspace, →K0):
  S6.0 PEP · S6.1 perm · S6.2 sandbox · S6.9 lifecycle   ✅6.2
  S7 retry/queue/crash  (KONZUMIRA S1.2 process-identity; S1.2 NE ovisi o S7 — fix C05)
  S5 workspace/checkpoint (5.1/5.2/5.3)  (→S1)   ✅5.1/5.3   ← PRIJE S4 (fix DAG-01)
  6.3 egress · 6.4 secrets · 6.5 injection · 6.6 authn(`service`) · 6.7 governance · 6.8 supply-chain (→K1)

FAZA L (izvršni put, →K):
  S2 provider (DETERMINISTIČKI router; learned→P)  (→S6,S7)
  S3 core-loop  (→S2,S5,S6,S7,S8.1-min,S11.1-min,S16.6-det)   ✅3.1/3.3/3.4/3.5
  S4 tools/exec/edit/search  (→S3,S5,S6.2)   ✅4.3   ← search staje na grep/AST/LSP (archmap→M)

FAZA M (kontekst/memorija/prompt PUNI, →L):
  S8 puni (8.1-puni/8.2 compaction/8.3 archmap/8.4 cache/8.5 pruning) → 4.4 archmap-integr.   ✅8.3
  S9 (9.1 sesije/9.2 store/9.3 learn/9.4 Decay+Audn — Decay+Audn=P1 per HARDQ B8, P0=explicit facts; 9.5/9.6 SAMO persistence+namespace, binding→N/O)   ✅9.2/9.4
  S11 puni (11.2/11.4/11.5/11.6)

FAZA N (orkestracija/observability, →M):
  S12 (12.1-6 + 12.7 flows + 12.8 boards)  (→S3,S5.2,S7)
  S15 observability (→EventJournal) · S16.6-council-integr. · 6.10 exec-reviewer (→15.3/15.5)
  S10 retrieval (→S9, ADAPTERI) · 9.5 channel/ACL-binding (→10.6)

FAZA O (sučelja/asistent-lice, →N):
  S14 CLI/TUI puni + 14.5 channels-stdio-built-in (plugin-channels→P)
  S13 multimodal (adapteri+subprocess) + 4.8 computer-use

FAZA P (distribucija/service, →O):
  S17 plugin/update/packaging (extensions engine) · plugin-channels (14.5-external)
  S18 deploy/scaling + 18.5 fleet + 18.6 pairing (ID-ovi iz fleet/ids — K3)
  S16.1-16.4 eval/benchmark/ratchet/credit · S2.6 learned-router (reward←16.4)
  9.6 app-contract done-eval (←16.1)
```

**Owner-invarijante (drže kroz cijeli put):** policy=**S6.0** · lifecycle-order=**S6.9** ·
sandbox=**S6.2** (svaki subprocess) · process-tree=**S1.2** (unutar S6.2.Launch) · retry/cancel=**S7** ·
journal-write=**P0.3/EventJournal paket** (jedini writer; NE S0.3). Effect-path fix: DESIGN-FIXES-r2 K1/K2.

**AMENDED 2026-09-12** (owner-approved; codex round-2 review finding, live-verified): "every
subprocess" has one known, PRE-EXISTING, tracked exception — `internal/coding/runner`'s own
`ResolveToolchain`/`goEnvJSON` helper (used by every Slice 0/1/2/3 caller: evidence.Capture,
tia.RunShadow, symedit.Prepare/RunGoplsRename) runs `go env` through a raw, unsandboxed
`exec.Command`, before any S6.2/S7 involvement — not something newly introduced by this piece
(`ExecInProcessGoverned`, `internal/kernel/contracts.ExecutionKind`), which is only the first
place to name it explicitly. Not closed this round (out of proportion for this piece — touches
already multi-round-converged Slice 0 code); tracked as a known gap, not silently ignored.

**PIECE-2 DESIGN OBLIGATION — CLOSED 2026-09-13** (kilo + agy round-3 review, independently
raised): `RunSpec.GoBinary`/`RenameRequest.GoBinary` are caller-supplied fields, executed via the
unsandboxed `goEnvJSON` call above BEFORE any S6.2/S7 involvement. `internal/coding/symedit/
tool.go`'s `Tools(...)` now supplies `GoBinary`/`GoplsBinary` itself, resolved ONCE by `cmd/nexus/
main.go`'s `buildDaemon` via `exec.LookPath` at daemon startup — NEVER from `c.Arguments`.
`TestRenameSymbolSpecsDeclareExecInProcessGoverned` (tool_test.go) asserts the assembled
spec+handler composition, not just the enum constant in isolation.

**PIECE-2 ROUND-2/3 FINDINGS — disposition through round 3** (codex rounds 2-3, live-
reproduced/verified each time; kilo + agy independently endorsed the round-2 deferrals as sound
before codex's round-3 pass found the two must-fix items below and a safer alternative to the
third — see the ROUND-4 block below this one for what codex's round-3 pass ALSO found, since
round 3 was not in fact the final round):

- **CLOSED (round 3, MUST-FIX):** `buildDaemon` registered `runner.Events()` but omitted
  `workspace.Events()` — `rename_symbol_apply`'s `ApplyGoverned` call emits
  `workspace.apply_started`/`workspace.mutation_committed`, so EVERY production apply failed
  closed with "unknown event type" before writing anything. Live-reproduced by codex via a Go
  overlay against the real composition root, then independently RED/GREEN-ablation-confirmed
  (`cmd/nexus/main_test.go`'s `TestRenameSymbolApplyEventsRegisteredInProductionDaemon` — the
  test that should have caught this from round 1: the original production wiring test only
  exercised `rename_symbol_prepare`, never `apply`). Fixed by adding
  `for n, v := range workspace.Events() { events[n] = v }` alongside `runner.Events()`.
- **CLOSED (round 3, MUST-FIX):** `RunGoplsRename`'s non-self-deadline `LaunchInteractive`
  failure branch (`gopls.go`, the ordinary-error path, distinct from the self-deadline branch)
  never called `journalGoplsRenameEvent` at all — not even with an empty result — so ANY
  backend-crash/stale-policy/pipe/process-start failure produced zero audit trail. Fixed by
  adding the same `journalGoplsRenameEvent(ctx, j, req, partial, err)` call already present on
  every sibling failure branch, restoring symmetry with `run.go`'s own equivalent branch.
- **CLOSED (round 3, discovery only — DELIBERATELY NOT full auto-recovery):** piece 2 is the
  FIRST thing to make `ApplyGoverned` production-reachable, so a crash mid-apply previously had
  NO startup-time visibility at all (`RestartScan`/`RollbackGoverned` existed, fully tested, but
  were called from nowhere in production). `buildDaemon` now calls `workspace.RestartScan`
  once at startup for the profile's configured coding-workspace root, logging any findings.
  `RestartScan` is deliberately read-only by ITS OWN design (`scan.go`'s own doc comment:
  "discovery + classification... nothing more" — it performs no S7 transition, no rollback);
  automatically DRIVING `RollbackGoverned` off a MID_CRASH finding remains a separate,
  deliberately deferred "recovery driver" obligation — building one now, under this piece's own
  time pressure, risks a rushed driver making a wrong-classification call on a system this
  piece did not itself design. A finding staying visible-but-unresolved (rather than invisible)
  is the concrete improvement this round delivers.
- **CLOSED (round 3, via a safer alternative to full GC):** `sealedstore.GC` is still never
  invoked in production (a correct `loadLive` needs the union of every sealedstore consumer's
  live references — evidence, workspace/rollback, symedit — most of which aren't wired into
  `buildDaemon` yet either; a rushed one risks sweeping a live artifact, worse than unbounded
  growth). Instead of deferring outright, codex's own round-2 suggestion was implemented:
  `sealedstore.Store` gained an exported `MaxEntries int` (zero = unlimited, backward
  compatible) enforced in `Put` — refusing a NEW entry once the ceiling is hit can never delete
  a possibly-live artifact (no liveness knowledge needed at all), unlike GC's mark-and-sweep.
  `buildDaemon` sets `MaxEntries = 2000` on symedit's own store (one apply = one `Put`, per
  `transaction.go`'s own single-whole-bundle write — 2000 renames of headroom for single-user
  volume). Full cross-consumer GC remains tracked for once evidence/rollback are ALSO
  production-wired.
- **PARTIALLY CLOSED (round 3):** the owner's own "canonical preview: symbol identity +
  old/new name + complete path set + patch hash" requirement (PLAN-CODING-TRIO.md) was only
  half-satisfied — Prepare's preview showed only the NEW name, never the original symbol
  identity being renamed. Fixed: `tool.go`'s `originalSymbolName` extracts the pre-edit text
  directly from `TextEdit.StartByte:EndByte` against Prepare's own captured preimage (Prepare's
  own real gopls response, not a guess or a second LSP call) — the preview now names both, and
  `rename_symbol_apply` gained a required, cross-checked `old_name` argument (mirrors
  `touched_files`) so the raw-JSON approval prompt also states it. STILL NOT closed: the shared
  ASK approval summary (`internal/approval/approval.go`) shows only raw tool_id + raw JSON
  arguments — no rendered diff — for EVERY ASK tool in the system, not something piece 2
  introduces. Redesigning `approval.go`'s summary format touches a shared, heavily-tested
  component used by every existing ASK tool — out of proportion for this piece; track as its
  own "approval UX" item if a future tool's blast radius needs more than named fields.

**PIECE-2 ROUND-4/5/6 FINDINGS — disposition through round 6** (codex's round-3, round-4, and
round-5 passes together found 11 more findings beyond the rounds-2/3 block above, every one
live-reproduced via adversarial overlays; kilo + agy PASSed every round independently, agy
flagging the same first-edit-selection concern as a softer NOTE where codex proved it live with
an actual wrong mutation, kilo separately flagging a real test-coverage gap in round 5 that round
6 also closed):

- **CLOSED (MUST-FIX):** a live overlay proved `originalSymbolName`'s "take the first valid
  slice" behavior could silently hide a WRONG mutation — a SEPARATE overlay proved gopls
  returning `TextEdit.NewText` different from the requested `new_name` (e.g. "Baz" when the
  caller asked for "Bar") was accepted with no check at all, and the actual on-disk mutation
  ended up named "Baz" while the plan/preview claimed "Bar" throughout. Fixed in three parts:
  (1) `verifyEditsReplaceRequestedName` (`rename.go`), called from `Prepare` right after
  `ParseWorkspaceEdit`, refuses the whole plan unless EVERY edit's `NewText` equals `req.NewName`
  exactly; (2) a SEPARATE round-4 overlay then proved (1) alone was incomplete — two edits could
  both write the SAME requested name while replacing two DIFFERENT original identifiers (folding
  two distinct symbols into one "rename"); `verifyEditsReplaceSameOldText` (`rename.go`), also
  called from `Prepare`, refuses the plan unless every edit's OWN pre-rename text (from the
  preimage bytes) is identical across every edit; (3) `originalSymbolName` (`tool.go`, the
  PREVIEW-TEXT-only counterpart to (2)) independently requires the same agreement for display
  purposes, returning `ok=false` rather than guessing. New unit tests exercise all three directly
  with hand-built conflicting/mismatched edits (no fake gopls session needed).
- **CLOSED (MUST-FIX):** round 3's log-only `RestartScan` was not enough on its own — codex's
  bar: "at minimum unresolved/error states must disable mutation." `buildDaemon` now tracks
  `scanClean` (the scan ran with ZERO findings) and only binds `symeditTools` when true — a
  MID_CRASH finding, or the scan itself failing, now disables `rename_symbol` entirely at
  STARTUP. A round-4 overlay then proved this was cold-start-only: an `ApplyGoverned` call
  landing `OutcomeUnknown` (`workspace.ErrRollbackIncomplete`) DURING a running daemon's own
  lifetime left the tool live for the rest of that process's run, since `scanClean` is only
  evaluated once at boot. Fixed: `Tools()` (`tool.go`) now holds a process-lifetime
  `atomic.Bool` "poisoned" latch, set the moment any apply hits `ErrRollbackIncomplete`, checked
  at the top of both handlers — a RESTART re-runs `scanClean`'s own startup gate (the actual
  recovery-relevant check); the in-process latch only stops the CURRENT process from digging the
  hole deeper in the meantime. Automatically DRIVING `RollbackGoverned` from a finding remains a
  deliberately deferred recovery-driver obligation — closing "keeps operating on a known-bad
  workspace," not "requires a human/future driver to fix the mixed state itself."
- **CLOSED (via a safer alternative, same discipline as MaxEntries):** an entry-COUNT quota
  alone does not bound actual disk USAGE — `runner.MaxSnapshotBytes` permits up to 500MB of
  snapshot input per coding-run, base64-encoded into one sealed bundle. `sealedstore.Store`
  gained `MaxTotalBytes int64` (zero = unlimited), checked in `Put` alongside `MaxEntries` via
  one combined directory scan (`statAllLocked`). `buildDaemon` sets `MaxTotalBytes = 8 << 30`
  (8GiB) on symedit's own store.
- **CLOSED (codex disputed the round-3 deferral with a concrete, bounded fix — correctly, THEN
  round 6 found the "scoped to gopls.go only" call was itself wrong):** `journalGoplsRenameEvent`
  discarded the journal-append error whenever the original execution error was also non-nil, and
  all 6 gopls.go callers discarded the function's own return value, re-deriving their own message
  from the raw pre-journal error instead. Round 3 judged this a pre-existing gap symmetric with
  `run.go`'s identical `journalRunEvent` pattern, out of proportional scope. codex's round-4 pass
  argued the fix is NOT a disproportionate redesign — `errors.Join` plus actually propagating the
  helper's return value — and was right: fixed exactly that way in `gopls.go`. Round 4's own
  SECTION-MAP entry then claimed this could stay SCOPED TO `gopls.go` ONLY because "`run.go` is
  used by evidence/TIA, outside piece 2's blast radius" — round 5's codex pass proved this FALSE:
  `internal/coding/symedit/typecheck.go:144` (Prepare's OWN staged-type-check step, run on EVERY
  `rename_symbol_prepare` call) calls `runner.Run` directly, putting `run.go`'s identical bug
  squarely inside rename_symbol's own call graph. Fixed: the SAME `errors.Join` treatment applied
  to `journalRunEvent`, PLUS two entirely-missing `journalRunEvent` calls round 5 also found —
  the `AttemptContext` post-Consume failure branch and the `grants.Report` success-path failure
  branch — neither had EVER called `journalRunEvent` at all (mirroring the identical gap
  `gopls.go`'s own `AttemptContext` branch turned out to have, also fixed this round). New test
  `TestRunGoplsRenameCombinedExecutionAndJournalFailureIsNotHidden` (a closed-journal
  reproduction, matching codex's own overlay) RED/GREEN-ablation-proven for the `gopls.go` half;
  `run.go`'s own equivalent fix is verified by symmetry/inspection (identical shape to the
  already-proven `gopls.go` fix) — codex's own live overlay reproductions for the `run.go`-specific
  branches are not independently re-proven by a NEW committed test this round, a residual gap
  disclosed rather than silently accepted.
- **CLOSED (codex round 5 HIGH, live-reproduced):** the `poisoned` latch only triggered on
  `errors.Is(err, workspace.ErrRollbackIncomplete)` — but `ApplyGoverned` can also fully commit
  every write (`result.Committed == true`) and THEN fail while recording the outcome itself
  (e.g. the `EvMutationCommitted` append), a DIFFERENT error shape the latch never caught. Fixed:
  the apply handler (`tool.go`) now poisons on `errors.Is(err, workspace.ErrRollbackIncomplete)
  || result.Committed`, and returns an explicitly honest message when `result.Committed` is true
  ("the rename WAS written to disk but its outcome could not be durably recorded") rather than
  implying total failure. New test
  `TestRenameSymbolApplyDisablesToolAfterCommittedButUnrecordedOutcome`, RED/GREEN-ablation-proven.
- **CLOSED (codex round 5 HIGH, live-reproduced with a deterministic overlay — the most
  structurally significant round-5 finding):** the `poisoned` latch was a single `atomic.Bool`
  checked at the TOP of `prepare()`, with no synchronization around the mutation itself — two
  CONCURRENT `rename_symbol_apply` calls (the daemon serves multiple connections concurrently,
  `internal/app/daemon/daemon.go:118`) could both pass the poisoned check before either finished;
  one lands `OutcomeUnknown` and poisons the closure while the OTHER is still mid-flight and then
  completes its OWN mutation on top of the now-known-unresolved workspace anyway. Fixed: a
  `sync.Mutex` (`applyMu`) now serializes the ENTIRE apply operation — poisoned-check through the
  mutation call and the resulting poisoning decision — as one atomic critical section.
  `rename_symbol_prepare` is deliberately NOT serialized under this same lock (read-only, safe to
  run concurrently); only mutation needs mutual exclusion. New test
  `TestRenameSymbolApplySerializesConcurrentCalls` (a controlled-delay overlay proving two
  concurrent applies never enter the mutation call simultaneously), RED/GREEN-ablation-proven
  (caught the race in roughly 1 of 3 runs without the fix, consistent with the race's own
  inherent non-determinism; the fix makes it deterministic).
- **NOT fixed — TRACKED as a whole-daemon-level gap, out of piece-2's scope:** a second
  `sealedstore.Store` handle opened on the SAME directory has its own independent mutex, so
  `MaxEntries`/`MaxTotalBytes` are per-HANDLE, not per-DIRECTORY, invariants — two concurrent
  daemon processes against the same profile could each independently believe they're under
  quota. In production, `buildDaemon` opens exactly ONE handle for the whole daemon lifetime.
  codex's own round-4 counter-check found this is LESS exposed than first framed: the journal
  itself already refuses a second live-writer handle on the same profile
  (`journal.Journal`'s live-writer lease, `TestSecondLiveHandleRefused`) — a second daemon
  process against the same profile fails at journal-open, before it would ever reach
  sealedstore at all. The theoretical per-handle quota gap remains real ONLY for a
  hypothetical caller that opens a `sealedstore.Store` directly without going through
  `buildDaemon`'s own journal-gated startup path — not a piece-2-reachable scenario.

**P0 -min rezovi (HARDQ A2, jednoglasno, 2026-09-03):** K1 rubovi S7/S5 za P0 walking-skeleton
zadovoljavaju se IMENOVANIM -min ugovorima (isti mehanizam kao S8.1-min/S11.1-min/S16.6-det):
`s7-min` = AttemptGrant + cancel + no-retry (retryable→FAILED_TERMINAL) · `s5-min` = AtomicWriter ·
`3.6-min` = persisted schedule + wake catch-up (uvučen u P0) · `7.3-min` = inbox/outbox +
occurrence-idempotency (uvučen u P0). PUNI engine-i (S7 taxonomija/budgeti/fencing, S5 shadow-git/
worktree) ostaju u svojim kasnijim fazama. Redoslijed isporuke: HARNESS-SPEC vertikalni sliceovi;
build-order detalj → HARDQ-CONSOLIDATED A2 → tasks-P0.md.

---

## 2) FOLD ADDENDUMA A1-A10 U SEKCIJE (više ne vise kao dodaci)

| Addendum | Ide u | Kao |
|---|---|---|
| A1 OCR hardware-routing | **2.4 + 10.1 + 13.5** | ADAPTER pipeline (Unlimited-OCR GPU / OCRmyPDF CPU → FTS) |
| A2 model-scout | **2.4 + 3.6** | JEZGRA scout (informira, ne auto-swap) |
| A3.1 identitet | **→ PRD** | "jedna jezgra, dva profila" — ODLUKA-PRD |
| A3.2 self-diagnostic→Claude | **15.5 + 17.4** | RepairBundle (opt-in, ne autonoman) |
| A4-G1 computer-use | **NOVA 4.8** (`computer-use` flag) | InputInject KEY/MOUSE/TYPE=RED-tier |
| A4-G2 PersonalProfile | **NOVA 9.5** (`profiles`) | deny-default izolacija memory/secrets/channels |
| A4-G3 voice-runtime | **13.3** (proširuje) | barge-in/wake/consent ugovor |
| A4-G4 ObligationStore | **NOVA 9.6** (asistent) | cilj s done-def, MarkDone samo uz Evidence |
| A4-G5 control-plane | **3.6 + 14.5** | asistentsko lice nad automation |
| A4 G6-G9 | dopune 8.4/9.3/2.2/14.5 | cache-lineage/learn-control/AccountFleet/channel-caps |
| A5-A8 audit-nalazi | **već foldani** (MASTER-LANDSCAPE) | 4 diferencijatora + reference |
| A9 fold-gapovi | **12.2/7.2/17.1** | superstep/ingress-queue/Cordis |
| A10 GAPFIX | **12.7/12.8 flows/boards + 18.5/18.6 fleet/pairing** | NOVE `multi-agent`/`service` podsekcije |

**Nove podsekcije iz addenduma — UBAČENE u plan (commit 257e01b), sve `Scope: ODLUKA-PRD`:**
4.8 computer-use, 6.10 exec-auto-reviewer, 9.5 PersonalProfile, 9.6 ObligationStore,
12.7 flows, 12.8 boards, 18.5 fleet, 18.6 pairing (8 novih — 18.6 vraćen; fix D6/R2-16). Kanonski broj: 108 podsekcija (100 spec + 8 addendum).

---

## 3) STATUS-LEDGER — kanonski broj: **108 podsekcija** (100 spec v0.9 + 8 novih iz addenduma)

Nove nose ✅+🟠 = DIZAJN-GOTOV (GAPFIX) + scope-flag ODLUKA-PRD (NE "tanko"; fix D4/R2-14).
Inline pod-stavke sad nose status (fix D3/SEC-03).

**S0** ✅ 0.1 0.2 0.3 0.4 0.5 (codex) · P0.5-0.13 → u Annexu A (dodani; G1 zatvoren)
**S1** 🟡 1.1 config · 1.3 telemetry · ✅ 1.2 proc+process-identity
**S2** 🟡 2.1 auth · 2.2 fallback(+AccountFleet classify-only) · ✅ 2.3 structured · 🟡 2.4 local(+A1/A2) · 2.5 cost · 🟠 2.6 learned-router(→P)
**S3** ✅ 3.1 loop · 3.3 cancel · 3.4 stuck · 3.5 TIA · 🟡 3.2 streaming · 3.6 trigger-plane
**S4** ✅ 4.3 edit+symedit · 🔵 4.4 search · 🟡 4.1 registry · 4.2 exec · 4.6 MCP · 4.7 tool-budget · 🟠 4.5 web · **✅🟠 4.8 computer-use** (GAPFIX-kilo)
**S5** ✅ 5.1 checkpoint · 5.3 atomic/attest · 🟡 5.2 worktree
**S6** ✅ 6.0 PEP · 6.2 sandbox · 6.9 lifecycle · 🟡 6.1 perm · 6.3 egress · 6.4 secrets · 6.5 injection · 🟠 6.6 authn · 6.8 supply-chain · 🟡 6.7 governance(sidecar) · **✅🟠 6.10 exec-reviewer** (GAPFIX-kilo)
**S7** ✅ 7.1 taksonomija · 🔵 7.2 queue/lease/fencing · 🟡 7.3 crash+durable-delivery
**S8** ✅ 8.3 archmap · 🔵 8.2 compaction · 🟡 8.1 window · 8.4 cache · 8.5 pruning
**S9** ✅ 9.2 store · 9.4 Decay+Audn(P1 — HARDQ B8 2026-09-03; P0=explicit facts, no decay) · ⬇ 9.4 Dream/MemGit/MemAssoc(v2) · 🟡 9.1 sesije · 9.3 learn · **✅🟠 9.5 PersonalProfile · ✅🟠 9.6 ObligationStore** (GAPFIX-kilo; 9.6 write→journal P0.3)
**S10** 🟠 10.1 ingest · 10.4 GraphRAG · 🟡 10.6 retrieval-auth · 10.7 quality-gate · 🟡 10.2 chunk · 10.3 hibrid(sqlite-vec) · 🟠 10.5 CAG(reserved→degradira u RAG)
**S11** 🟡 11.1 assembly · 11.5 skill-lifecycle · 11.6 role-manifest · 🔵 11.2 Reversa · 🟠 11.3 projektne-instr · 🟡 11.4 prompt-opt
**S12** 🔵 12.1 subagenti · 12.2 DAG(+superstep) · 12.5 A2A · 🟡 12.3 routing(potrošač 2.6) · 12.4 HITL · 12.6 council · **✅🟠 12.7 flows · ✅🟠 12.8 boards** (GAPFIX-codex)
**S13** 🟠 13.1 vision · 13.2 image-gen · 13.3 voice(+A4-G3) · 13.4 video(subprocess ffmpeg) · 13.5 doc-mutacija(pure-Go pdfcpu) — svi ADAPTER+subprocess
**S14** 🟡 14.1 CLI/TUI · 14.5 channels(stdio-built-in;plugin→P) · 🟠 14.4 IDE/ACP · 🟡 14.2 SDK · 🟠 14.3 web-UI
**S15** ✅ 15.3 audit-chain · 🟡 15.1 tracing · 15.5 health(swallow-owner)+A3.2 · 🟡 15.2 cost-dash · 15.4 SLO
**S16** ✅ 16.6 checker · 🟡 16.1 benchmark+app-contract · 16.4 credit-ledger · 🟠 16.5 trajectory-export · 🟡 16.2 ratchet · 16.3 red-team(strix)
**S17** 🔵 17.1 plugin(Cordis) · 🟡 17.4 self-diag-bridge+A3.2 · 🟡 17.2 update(TUF) · 17.3 packaging
**S18** 🔵 18.1 worker/queue · **✅🟠 18.5 fleet · ✅🟠 18.6 pairing** (GAPFIX-codex; fleet/ids anti-cikl K3) · 🟡 18.2 tenant-gw · 18.3 canary · 18.4 backup(Litestream sada; premisa "S0.4" bila kriva — backend već SQLite-WAL)

**Zbroj (108):** ✅ ~24 dizajn-gotovih (uklj. 8 novih GAPFIX) · 🔵 9 auditanih · 🟡 ~40 dizajn-treba · 🟠 ~15 odluka-PRD · ⬇ 3 descope.

---

## 4) DESIGN-INDEX (dizajn-dok → podsekcije)

| Dok | Pokriva |
|---|---|
| `DESIGN-S0-sandbox-codex.md` | 0.1 0.2 0.3 0.4 0.5 (+P0.5) · 6.2 (+capability-matrica, Win/mac probe) |
| `DESIGN-memory-effectpath-kilo.md` | 9.4(descope) 9.2 8.3 · effect-path 3.1/3.3/3.4/6.0/6.9/1.2/7.1 · 2.3 |
| `DESIGN-checker-tia-edit-agy.md` | 3.1/16.6 checker · 3.5 TIA · 4.3/5.1/5.3 edit |
| `DESIGN-symedit-crypto-claude.md` | 4.3 symedit · 15.3/6.8 crypto-attest |
| `PLAN-HOLES-CONSOLIDATED.md` | 8 konvergentnih rupa + 6 must-resolve |
| `DESIGN-STATUS.md` | status 6 must-resolve |

---

## ŠTO OSTAJE ZA PRD (ne može prije)
- **Identitet** (A3.1 jedna-jezgra/dva-profila) → određuje što je P0.
- **P0/P1/P2 scope-tier** → koji flagovi ulaze u prvi build; sve 🟠 i većina NOVIH ovise o tome.
- Sve ostalo (dependency-order, fold, status, design-linkovi) je JASNO i PRD-neovisno.

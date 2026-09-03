# REVIEW-TASKS — docs/tasks-P0.md (the ONLY P0 task queue)

Axes: TRACE / ORDER / RED / SIZE. Sources: HARDQ-CONSOLIDATED, ARCHITECTURE-ESSENTIALS,
HARNESS-SPEC Annex A, PRD, DESIGN-FIXES-r2, SECTION-MAP, constitution.

Overall: high-quality ledger — correct walking-skeleton phases, correct T02 gating, correct -min
discipline (no full S7/S5/Activator creep; deferred ledger names them), six-for-six PRD §6 criteria
covered. Findings below are specific and fixable.

---

## ORDER

**1. [ORDER] T11 (EffectPath) consumes `s7.AttemptGrant`, which T12 (s7-min) builds — inverted.**
T11 (Phase 2) builds `EffectPath` "exactly per DESIGN-FIXES-r2 K1/K2", whose canonical signature is
`RunTool(ctx, call, grant s7.AttemptGrant)` and whose `RetryOwner.Record` takes that grant; T11's RED
`TestNoAttemptWithoutGrant` exercises it. T12 (the next task) then defines `AttemptGrant` + the
no-retry policy. A compliant "one task at a time, in order" implementer doing T11 first hits a missing
type. Fix: swap T12 before T11 (s7-min is a K1 primitive that the effect-path imports), or fold the
`AttemptGrant` type into T11's contract work and keep the no-retry policy in T12.

**2. [ORDER] T09 (sealed startup snapshot) requires "live provider/channel probes", but provider is
T13 (Phase 2) and channel is T20 (Phase 5) — acceptance unsatisfiable at its Phase-1 position.**
T09 acceptance: "snapshot lists exactly the compiled P0 capabilities with probe results"; its probe
set is "sandbox/provider/channel". At Phase 1 only the sandbox probe (T02) exists. Fix: scope T09 to
"sandbox probe only; provider/channel probe results register into the snapshot as T13/T20 land" (make
the snapshot extensible), or move T09's probe-integration half later. As written, T09 cannot be
marked DONE in Phase 1.

## TRACE

**3. [TRACE] `s5-min` = AtomicWriter (HARDQ A2) is adopted but lands in no task and is not deferred.**
A2: "`s5-min` = `AtomicWriter` only; full S5 shadow-git/worktree = P3." The ledger's deferred ledger
defers "full S5 (P3)" but no task builds `AtomicWriter` (tmp→fsync→rename) nor records it as
deferred-with-reason. E4 makes AtomicWriter the P0 file-mutation primitive (P0.11). Either add a
small AtomicWriter task/sub-item (or fold into T22's disposable-workdir writes), or record "s5-min
deferred — P0 has no user-workspace file mutation; first consumer = coding P1". As-is it is silently
lost.

**4. [TRACE] C8's positive half ("liveness heartbeat + last-occurrence-fired counter") lands nowhere.**
C8: "P0 health = liveness heartbeat + last-occurrence-fired counter; 15.5 SemanticHealth deferred."
The deferred ledger defers SemanticHealth only. No task owns the heartbeat or the
last-occurrence-fired counter (T15 daemon / T18 scheduler are the natural homes). Adopted → lost.

**5. [TRACE] A2 step 2's "deterministic clock/ID seams" is a K0 primitive with no explicit task.**
A2 walking-skeleton step 2: "K0: typed contracts, config, EventJournal, **deterministic clock/ID
seams**, minimal checker." Only T18 ("injected wall/monotonic clocks") and T05 (sequence) touch these,
both incidentally. The injectable clock/ID seam (needed for deterministic journal/scheduler tests) is
not a named deliverable. Minor: fold into T05 or add a K0 sub-item.

**6. [TRACE] `PRD §6.1`…`§6.6` citation convention is wrong — PRD §6 has numbered items 1–6, no
subsections.**
T02/T15–T21 cite "PRD §6.1/§6.2/§6.3/§6.4/§6.5/§6.6". PRD.md §6 is "Kriteriji gotovo" with items 1–6;
there is no §6.6 subsection (the constitution correctly writes "PRD §6 criteria"). This exact error
was already fixed in ARCHITECTURE-ESSENTIALS ("PRD §6 item 6, §7"); the ledger reintroduces it
systematically. Low severity but it is the queue implementers cite.

## RED

**7. [RED] T02's RED tests only the fail-closed path, not the security denial the task gates on.**
T02 RED: "bwrap absent → probe reports UNAVAILABLE." That is red-capable and D1-anchored, but the
task's PRD criterion (deny `/etc/shadow` + any egress) sits only in acceptance ("suite GREEN on the
deployment host"). For the single go/no-go gate of P0, the security property has no red-capable
detector (the suite IS the deliverable, so it cannot be observed RED-before). Acceptable, but flag it:
add at least one denial case to the RED set (e.g. "a fixture target that reads `/etc/shadow` must be
denied") so the gate's own RED exercises the property, not only the absence path.

## SIZE

**8. [SIZE] T20 bundles gateway core + durable inbox/outbox + built-in adapter + per-chat profile
binding + non-text fail-closed (A1/B2/C2/B3) with a 6-point SIGKILL matrix — too big for one
reviewable slice.**
This is the whole Telegram vertical in a single task. Split: (a) durable inbox/outbox + occurrence
idempotency (B2), then (b) built-in adapter + non-text fail-closed + per-chat binding (A1/C2/B3).
T11 (PEP + S6.9 + EffectPath, 8 REDs) is large too but coherent — the effect-path is one mechanism,
so it is defensible; T20 is not.

## OK (verified sound — not rubber-stamp)

- Walking-skeleton 7 steps map cleanly to phases; T02 gate honored (T22/T23 gated; T11 stubs
  `TestReadOnlyProcessStillSandboxed` until T23; header "nothing relies on the sandbox before T02").
- -min discipline held: T12 = no-retry/FAILED_TERMINAL, T09 = validation-only, no Activator.
- Every other HARDQ P0 item lands: A1→T20, B1→T18, B2→T20, B3→T16, B4→T22, B5→T10/T19, B6→T21,
  B7→T05/T15, B8→T17, B9→T07/T09, C1→T05, C2→T20, C3→T19, C4→T21, C5→T02, C7→T04+deferred,
  D1→T02/T22, F1→T03.
- REDs are overwhelmingly spec-anchored (Annex A RED names reused verbatim: `test_projection_cannot_
  bypass_journal`, `test_adapter_cannot_self_retry`, `test_s0_rejects_provenance_laundering`,
  `test_purge_cannot_complete_with_residual_copy`, `test_approval_cannot_authorize_modified_effect`).
- T24's ablation RED (revert a feature commit → criterion check fails) is a genuine non-vacuity
  guard; all six PRD §6 acceptances prove their criteria.

---

## Verdict

Strong ledger with correct phase structure, gating, -min discipline and mostly-spec-anchored REDs.
But it carries two real dependency-order bugs (T11/T12 inversion blocks the effect-path; T09 cannot
meet its acceptance at Phase 1), silently drops two adopted P0 items (s5-min/AtomicWriter, C8 health),
and has one oversized slice (T20). These are precisely the "errors become code" class, and all are
cheap to fix.

VERDICT: FAIL

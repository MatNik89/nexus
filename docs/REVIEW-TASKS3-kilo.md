# REVIEW-TASKS3 — round-3 final verification (6 residuals)

Scope: verify the 6 fixes ONLY + flag any NEW defect.

---

**1. [OK] MEMORY_FORGET deferred with DATA_PURGE to Annex P2.1.**
T19:197-203 — "Explicit memory (S9.2-min)" (FORGET removed from the task title); "MEMORY_FORGET
and DATA_PURGE both deferred to their Annex P2.1 contract — neither is a PRD §6 criterion and
P2.1 is their normative owner … supersession covers P0 correction needs"; Trace cites
"HARNESS-SPEC P2.1 (deferral authority)". Deferred ledger:307-308 — "MEMORY_FORGET + DATA_PURGE
(both P2.1 — …; P0 correction = supersession; USP #4 completes at P2)". Essentials E14:178-181
re-annotated to match — "BOTH land with their normative Annex P2.1 contract … P0 correction =
append-only supersession". All three agree. ✓

**2. [OK] T02 negative controls hazard-safe.**
T02 RED:34-39 — "negative controls per boundary, HAZARD-SAFE … FS control reads a non-secret
CANARY file planted in a test-owned dir (never recursive real `/etc`); net control reaches only a
test-owned local sink (never uncontrolled egress); process control escapes into a test-owned
process tree. Each loosened profile MUST turn its boundary check RED." The real `/etc/shadow`
denial stays in the acceptance (a read-deny, not an exposure); the loosening-detection proof is
now canary/sink/tree-safe. ✓

**3. [OK] T05 acceptance task-local + conformance REDs for all four seams.**
T05:70-71 acceptance — "task-local conformance tests for ALL FOUR seams pass (downstream import/use
checks are additionally enforced by later tasks, not by this one)". REDs map one-to-one:
AtomicWriter → `test_atomic_write_no_partial`; ContextBudget → hard-limit breach → refuse;
clock/ID → determinism conformance (two runs → identical event timelines); Assembler.Base →
deterministic golden output. All four seams have a red-capable, task-local detector. ✓

**4. [OK] T07 generic harness with fakes; concrete recipes → T20/T22 with their REDs;
misattribution dropped.**
T07:85-95 — "Generic harness … proven with contract-valid FAKE domain rows … The three CONCRETE
recipes land with their first real consumers and carry their crash/visibility REDs there:
inbox-admission+journal → T22 · occurrence+run-admission → T20 · terminal-result+outbox → T22."
The old "MarkDone-then-ListActive race (agy TOCTOU case)" is gone; the replacement RED is generic
("append-then-read of a core projection in the same process → new state visible"). T20 carries the
occurrence-idempotency crash REDs (restart → fires once); T22 carries the SIGKILL matrix for the
two delivery recipes. No consumer-before-producer. ✓

**5+6. [OK] T27 verifies the final live snapshot + stale/removed-probe RED.**
T27:296-297 acceptance — "final sealed capability snapshot contains LIVE sandbox+provider+channel
probe attestations (fakes gone)". T27:301-302 RED — "each live probe removed or stale-hashed → its
capability OFF in the snapshot and no dispatch to it." Satisfiable at T27 (all three live probes
exist by then: sandbox T25, provider T15, channel T23). ✓

---

## NEW defects — none

No new forward-reference, misattribution, lost adopted item, or order break. The FORGET deferral
is internally consistent across T19, the deferred ledger, and E14 (USP #4 explicitly deferred to
P2, supersession retained as the P0 correction path). T20/T22 do carry the crash/visibility REDs
for the recipes T07 hands them (occurrence idempotency / SIGKILL matrix respectively). The T02
negative controls are hazard-safe without weakening the real `/etc/shadow` denial in acceptance.

---

## Verdict

All 6 residuals are correctly folded, E14 matches, and no new defect was introduced.

VERDICT: PASS

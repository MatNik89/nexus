# REVIEW-TASKS2 — round-2 verification of tasks-P0.md v2 (27 tasks)

Scope: (1) fold/reject every round-1 finding (codex 15 + kilo 8 + agy 1); (2) no NEW defect;
(3) deferred ledger consistent. E14 annotation checked.

---

## (1) Round-1 findings — all folded

### codex (15) — all folded
**1. [OK] s5-min AtomicWriter** → T05 ("K0 seams: clock/ID injection · AtomicWriter (s5-min) ·
ContextBudget-min · Assembler-min"), RED `test_atomic_write_no_partial`. ✓
**2. [OK] C8 health signals** → T17 ("liveness heartbeat (C8 positive half)") + T20
("last_occurrence_fired counter (C8 positive half)"). ✓
**3. [OK] A1 release-signature half** → T27 ("minimal release signing … built-in Telegram adapter
integrity attested by that signature — HARDQ A1's second half"). ✓
**4. [OK] DATA_PURGE smuggled** → T19 defers it ("DATA_PURGE deferred to its full P2.1 contract");
deferred ledger names it. ✓
**5. [OK] T03 false P0-capable** → T03 grants `prerequisites-ready`; "P0-capable granted ONLY by
T27". ✓
**6. [OK] K0 seams absent** → T05 now owns clock/ID + ContextBudget-min + Assembler-min. ✓
**7. [OK] T09 forward-dependency** → T11 "sandbox-probe only at this phase … contract-valid probe
fakes for provider/channel (live probes register in T15/T23; T27 verifies the final snapshot)". ✓
**8. [OK] T11 unsatisfiable (stub-skip)** → T14 "sandbox executor = contract FAKE here; real backend
binds in T26 … no stub-skip in this task's own list"; acceptance = "7 contract REDs GREEN with
fakes". ✓
**9. [OK] B3 cross-profile truncated** → T24 carries the full B3 chain RED (occurrence/delivery/
approval never cross profiles, incl. restart/replay); T18 notes the integration REDs land in T24. ✓
**10. [OK] Phase-0 gate detectors** → T02 RED (b) negative controls (loosened `/etc`/net/PID-ns →
suite RED); T03 RED table-driven over all five prerequisites. ✓
**11. [OK] tolerant parser vs P0.7** → T15 "tolerance LIMITED to non-security/non-effect payloads" +
RED "malformed/unknown EFFECT/POLICY discriminator → reaches no sink". ✓
**12. [OK] conversation detector** → T17 RED "scripted e2e conversation oracle with a DETERMINISTIC
fake provider … + separately-labeled live-provider smoke". ✓
**13. [OK] no concrete Task / negative-only C3** → T21 concrete `file_note` handler (postcondition =
checker verifies file hash); T16 RED "POSITIVE breaker test — same-turn identical call N× → trips;
polling-policy call N× → no trip". ✓
**14. [OK] T24 ablation weak** → T27 RED "for EACH of the six criteria, a controlled feature-off …
turns exactly that criterion RED"; harness "lives OUTSIDE the ablated implementation". ✓
**15. [OK] T05/T20 too big** → split: T05→(T05 seams + T06 journal + T07 projections); T20→(T22
ingress/egress core + T23 adapter). ✓

### kilo (8) — all folded
**16. [OK] T11/T12 inversion** → T13 "s7-min (BEFORE the effect-path that consumes it)" precedes T14. ✓
**17. [OK] T09 forward-dep** → T11 fakes + deferred live probes (same as codex #7). ✓
**18. [OK] s5-min lost** → T05. ✓  **19. [OK] C8 lost** → T17/T20. ✓  **20. [OK] clock/ID seams** → T05. ✓
**21. [OK] PRD §6.x citation** → line 9 convention note ("PRD §6 item N"); all traces now "PRD §6
item N". ✓  **22. [OK] T02 weak RED** → negative controls added. ✓  **23. [OK] T20 size** → split. ✓

### agy (1) — folded
**24. [OK] s5-min in T08/T17** → T05 names "AtomicWriter (s5-min)" explicitly. ✓

## E14 annotation — [OK]
ARCHITECTURE-ESSENTIALS:178-181 — "MEMORY_FORGET … ≠ DATA_PURGE … — USP #4; **FORGET ships in P0,
the full DATA_PURGE contract lands with P2.1** (task-review fix — purge is not a PRD §6 criterion
and its Annex contract is P2)." Matches the deferred ledger and T19. ✓

---

## (2) NEW defect

**25. [NEW-ERROR] T07's third RED forward-references T21's obligation ops and carries a
misattribution — not satisfiable within its own Phase-1 task as written.**
T07 RED (line 88): "MarkDone-then-ListActive race (agy TOCTOU case) → new state visible."
`MarkDone`/`ListActive` are ObligationStore operations built in T21 (Phase 4), so at Phase 1 this
test cannot run against the real component. The read-your-own-writes property it targets is generic
and testable at T07 with the session/run projection (T06/T08), but the test is named against a
later component. Also "(agy TOCTOU case)" is a misattribution — agy's round-1 review (PASS) raised
only the s5-min note, no TOCTOU finding. Low severity, one-line fix: either rename to a generic
projection test ("append-then-read core projection → new state visible") or move the
obligation-specific race to T21.

Everything else in the renumbering is sound: T13-before-T14, T05-seams-before-consumers,
T22/T23 split preserving phase order, T02 gate re-pointed to T25-T26, and no adopted HARDQ item
lost (A1/B1-B9/C1-C5/C7/D1/F1 all still trace to a task or the deferred ledger).

## (3) Deferred ledger — [OK] consistent
"full S7 (P2) · full S5 shadow-git/worktree (P3) · DATA_PURGE full contract (P2.1 — FORGET ships in
P0, purge completes USP #4 at P2) · Decay+Audn (P1) · Activator (P4+) · entropy (P4) · SemanticHealth
evaluator (P2+; C8's two positive signals ARE in T17/T20) · upcast/quarantine (v2) · native helper
(P1) · plugin channels (P) · coding trio (P1)." Each entry matches the -min scope of its task and
nothing deferred is smuggled into T01–T27.

---

## Verdict

All 24 round-1 findings are folded correctly and the E14 annotation is accurate; renumbering is
order-sound and no adopted item is lost; the deferred ledger is consistent. The sole new issue is a
single low-severity RED-list item in T07 that names a later task's operations (and misattributes a
reviewer), so that one detector is not satisfiable within its own task as written.

VERDICT: FAIL

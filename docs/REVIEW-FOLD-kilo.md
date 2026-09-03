# REVIEW-FOLD — commit 18aabde (hard-questions fold verification)

Authority: HARDQ-CONSOLIDATED.md. Scope: (1) every ADOPTED item reflected or deferred;
(2) no fold distorted a resolution (bwrap / at-least-once / P1.6); (3) no new contradiction
with untouched sources.

---

## No-distortion checks (the three the task flagged) — all PASS

**1. [OK] D1 bwrap — hostile-suite invariant and fail-closed PRESERVED.**
E10:116-119 — "bubblewrap behind the `SandboxBackend` interface (Probe/Compile/Launch/Attest),
fail-closed, attested, and gated by the SAME hostile conformance suite — **the suite is the
invariant, the backend is swappable**." E10:127-128 — "bwrap absent → exec capability OFF
(conversation-only), no weaker fallback." E1:25-26 — bwrap is "a DECLARED install prerequisite
checked by the doctor preflight, never a silent dependency." Open list #1 — hostile suite on the
real deployment host. No weakening: fail-closed and the suite survive the backend swap. ✓

**2. [OK] B2 at-least-once — no creep back to exactly-once.**
E15:183-186 — "the local outbox gives exactly-once ADMISSION and at-least-once remote delivery —
a non-transactional remote API cannot be exactly-once; `sent-but-unrecorded` → UNKNOWN →
RECONCILING, never blind retry." Exactly the honest boundary from HARDQ B2. ✓

**3. [OK] A1 P1.6 split — plugin-path RED intact.**
HARNESS-SPEC:1467 AMENDMENT — "`channels requires extensions + S6.8 + S11.5` vrijedi ISKLJUČIVO
za dinamički učitane (`channel:plugin`) adaptere; RED slučaj A testira plugin put i ostaje na
snazi." E15:191-194 mirrors the split. The plugin RED (`INCOMPLETE_CAPABILITY_CLOSURE` /
`APPROVAL_REPLAY`) is preserved. ✓

---

## Unfolded / new-contradiction findings

**4. [UNFOLDED] C6 — P0.8/P0.9/P0.11 trigger relabeling is MISSING from HARNESS-SPEC, though the
traceability line says it lands there.**
HARDQ-CONSOLIDATED:183 ("HARNESS-SPEC Annex A (P1.6 split, P0.8/9/11 triggers)") + C6 ("P0.8 gates
`local-inference`, P0.9/P0.11 gate `coding`/user-workspace mutation … activation triggers
relabeled"). Verified: HARNESS-SPEC wiring table (1352-1358) and contract bodies P0.8/P0.9/P0.11
(1555-1568) are unchanged — no trigger/priority annotation. A P0 reader still sees "P0.8 Hardware-
fit", "P0.9 verify", "P0.11 shadow-checkpoint" and reads the "P0." prefix as a P0 gate; these gate
P1 features. Fix: one-line annotation on each ("activation trigger: `local-inference`/`coding`/
user-workspace mutation — NOT a P0 gate") in the wiring table or contract header.

**5. [UNFOLDED] C3 — stuck-detection scoping is not reflected in any changed file and not deferred.**
C3 ("stuck-detection scoped to WITHIN one interactive turn; scheduled/polling iterations carry an
explicit continuous-loop policy exempt from the identical-argument breaker") is P0-relevant: the
B1 scheduler runs polling iterations, and without the exemption the 3.4 STAGNATION breaker will
kill the reminder loop as "stuck". Verified absent from ARCHITECTURE-ESSENTIALS (E8/E9/P0 additions),
HARNESS-SPEC, and SECTION-MAP (only the pre-existing "3.4 stuck" status row remains). Fix: add one
line to E9 or the P0-additions list, or explicitly route it to tasks-P0.md as a RED
("scheduled iteration is not subject to identical-argument breaker").

**6. [NEW-ERROR] DESIGN-STATUS #3 still frames the native helper as the Linux backend — contradicting
E10's bwrap-P0 decision, and DESIGN-STATUS is only half-updated.**
E10 (changed) makes bwrap the P0 ENFORCED backend and native helper the P1 path. DESIGN-STATUS.md:10
(not updated by this fold) still says "Linux ENFORCED-nakon-live-probe (helper: no_new_privs→Landlock
→per-arch-seccomp→execveat…)" with no mention of bwrap. DESIGN-STATUS was partially updated — line 13
(#6) now reads "RESOLVED (user approved 2026-09-03)" — so the file is internally inconsistent: #6
carries the 2026-09-03 decision while #3 predates the D1 sandbox decision. DESIGN-STATUS sits in the
precedence chain as a current "dated status ledger", so a reader following it gets the stale sandbox
stance. Fix: update DESIGN-STATUS #3 (bwrap = P0 backend, native helper = P1; probe = bwrap
hostile-conformance) or add DESIGN-STATUS to the "stale claims" warning in the Essentials precedence
note.

---

## Borderline (OK, with note)

**7. [OK] B5 — two obligation types reflected, but the honesty nuance is only in a summary line.**
The "P0 additions" summary has "two obligation types Reminder/Task with assistant-grade evidence
(B5)" ✓. However B5's material constraint — "no generic 'finish Y' claim until a verifier exists;
receipt proves delivery, ack proves acknowledgment, neither proves external-world completion" — is
NOT pinned in a decision body. E13:155-156 still reads "ObligationStore.MarkDone accepts only
Evidence, so P0 obligations cannot ship without it" with no note that Reminder's terminal is
ACKED-via-receipt+ack and that generic finish-Y is out of P0. Not a distortion (both statements are
true), but the state machine + honesty nuance should be pinned in E13 or tasks-P0.md to prevent a
regression to a coding-only MarkDone.

Similarly: B3/B4 are reflected in E14/E10 bodies (not in the "-min cuts" summary list, which is
correct — they are amendments, not new -min cuts); C4 is reflected only as a pointer ("canonical
intent fields per HARDQ C4" in E15:190), acceptable for a second-order fix.

---

## Verdict

The three flagged no-distortion checks pass cleanly (bwrap keeps the hostile-suite + fail-closed;
at-least-once is honest; P1.6 keeps the plugin RED). But the fold is incomplete: two adopted items
(C6, C3) are neither reflected in the changed files nor deferred to tasks-P0.md, and one source in
the precedence chain (DESIGN-STATUS #3) now contradicts E10 while being only half-updated. C6 is a
labeling clarification; C3 is a live P0 field risk (scheduler vs stuck-detection breaker).

VERDICT: FAIL

# REVIEW3 — ARCHITECTURE-ESSENTIALS.md (targeted verification of 6 round-2 folds, commit d419885)

Narrow scope: verify only the 6 listed folds + flag NEW errors introduced by those edits.
Sources re-checked: HARNESS-SPEC.md (Annex A + "Redoslijed gradnje: FAZE" :1020–1057), PRD.md,
DESIGN-STATUS.md, SECTION-MAP.md, HARNESS-PLAN.md, PLAN-HOLES-CONSOLIDATED.md.

---

## Fold-by-fold

**1. [OK] E2 + Open list item 1 — PRD approval PENDING.**
E2:30-31 — "Proposed and written into PRD v1; **formal product approval is still PENDING**
(PRD:66 'awaits your confirmation'; DESIGN-STATUS #6 OPEN — see Open list)."
Open:187-188 — "PRD formal approval ... await the user's sign-off ... Until recorded, E2 is a
proposal, not a decision."
No "approved" remains. Matches PRD:66 and DESIGN-STATUS #6. ✓

**2. [OK] E4 — AtomicWriter scoped to S5.1/5.3; SQLite-WAL exempt.**
E4:51-55 — "File/workspace mutations (S5.1/5.3 scope, P0.11) use `AtomicWriter` (tmp → fsync →
rename) ... Persistence engines (SQLite-WAL spine) provide their own durable atomicity contract —
do NOT wrap per-record writes in rename."
Matches P0.11 (HARNESS-SPEC:1564-1566, owner S5.1/5.3). ✓

**3. [OK] E6 — SECTION-MAP DAG vs HARNESS-SPEC slices reconciled by role.**
E6:68-77 — "SECTION-MAP §1 (K0→K1→L→M–P) is the dependency DAG ... HARNESS-SPEC 'Redoslijed
gradnje' (P0–P6) is the vertical delivery-slice plan ... NOT the same ordering ... Residual
ordering conflicts → hard-questions." Open:192-193 carries it.
Claim verified against SPEC:1025-1034 (P1 = S2/3.1/4.1/6.2 before full S7 in P2:1034 and S5 in
P3:1038) vs SECTION-MAP K1 (S7+S5 before L). The worked example is accurate; S16.6-det and
sandbox-before-S4 invariants hold in both documents. ✓

**4. [OK] E7 heading — "Transactional", not "two-phase".**
E7:79 — "Transactional capability activation". Body retains Prepare → Commit → Activate +
RollbackToken + PREPARING/FAILED/ROLLING_BACK. ✓

**5. [OK] Header precedence — PRD + DESIGN-STATUS added, HARNESS-SPEC scoped to Annex A only.**
Header:8-14 — "PRD.md owns product decisions ... > **HARNESS-SPEC Annex A contracts** (the
contracts only — the SPEC body still carries stale salvage references ... never revive them via
'SPEC wins') > DESIGN-FIXES-r2 / DESIGN-*.md > DESIGN-STATUS (dated status ledger) > HARNESS-PLAN.md
> SECTION-MAP / PLAN-HOLES." Stale salvage body explicitly warned. ✓

**6. [OK] E10 — `execveat` + probe blocks only S6.2/arbitrary-exec.**
E10:104-109 — "irreversible `execveat` ... go/no-go preflight BEFORE production S6.2/arbitrary-exec
implementation (other P0 work may proceed; nothing may RELY on the sandbox boundary before the
probe passes)." Matches DESIGN-STATUS:10 (`execveat`) and PLAN-HOLES C2 ("Bez toga S6.2 i
arbitrary-exec NE u kod"). ✓

---

## NEW-ERROR introduced by the edits

**7. [NEW-ERROR] E2 heading still reads "(decided)" while the body now says "PENDING" / "a proposal, not a decision".**
E2:27 heading: "One kernel, two profiles; assistant FIRST **(decided)** + no self-modification".
The round-3 edit fixed the body (PENDING) but left the heading's "(decided)" intact. In round 2
heading and body were consistent (both asserted decided/approved); now they diverge, and the body's
own words "E2 is a proposal, not a decision" directly clash with "(decided)". A heading-level
reader is misled into the exact false-readiness signal the edit removed. Low severity (body is
unambiguous), but it is a new internal contradiction introduced by this edit — drop "(decided)"
or relabel to "(proposed)".

No other new errors found in the six edits (all new references verified: PRD:66, DESIGN-STATUS #6,
HARNESS-SPEC:1020–1056, C1/HARNESS-PLAN:3, S6.2/arbitrary-exec).

---

## Verdict

All six material folds are correctly applied and accurately sourced. The sole new error is a
low-severity heading/body mismatch ("(decided)" vs PENDING) that does not undermine the corrected
substance — the false "approved" claim is gone and PRD is correctly listed as Open #1.

VERDICT: PASS

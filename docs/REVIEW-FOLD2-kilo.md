# REVIEW-FOLD2 — commit 14814cf (round-2 fold verification, 12 fixes)

Authority: HARDQ-CONSOLIDATED.md. Scope: verify the 12 fixes + flag NEW contradictions.

---

## The 12 fixes — all verified

**1. [OK] Precedence ranks HARDQ-CONSOLIDATED right after PRD.**
Essentials:8-11 — "PRD.md owns product decisions … > **user-approved HARDQ-CONSOLIDATED
resolutions** (A/B/C/D/F items, 2026-09-03 — they supersede the specific clauses they amend in
Annex A, DESIGN-S0, DESIGN-STATUS and SECTION-MAP; the amended owners carry inline
change-records) > HARNESS-SPEC Annex A contracts …". ✓

**2. [OK] P1.6 REWRITTEN (not annotated), RED case A literally targets `channel:plugin`.**
HARNESS-SPEC:1461 (owner — "deklarira DVA puta: channel:builtin … channel:plugin"); 1462
(manifest — ChannelAdapterManifest SAMO channel:plugin + built-in deskriptor bez extensions-only
polja); 1465 (invarijante — plugin ne registrira prije extensions closurea, builtin statički +
release-potpis); 1466 (RED — "slučaj A registrira `channel:plugin` bez `extensions/S6.8`
attestationa; slučaj B … (zajednički za builtin i plugin put)"). ✓

**3. [OK] P0.8/P0.9/P0.11/P0.13 bodies + wiring table carry activation-trigger annotations.**
Bodies: 1556 (`local-inference` — NIJE P0 gate), 1561 (`coding` — NIJE P0 gate), 1566 (P0 = SAMO
AtomicWriter; rollback dio gates `coding`/workspace mutaciju), 1571 (P1 `memorija` decay — NIJE P0
gate; fact-tip S=∞). Wiring table 1355-1358 mirrors all four. ✓

**4. [OK] P0.1 migration clause carries the P0 reject-unknown scope note.**
HARNESS-SPEC:1381 — "P0-scope (HARDQ C7): u P0 je dovoljan `schema_version` + reject-unknown-
neobrađeno (raw input sačuvan); upcast-LANAC + karantena-sink aktiviraju se s prvom stvarnom
migracijom (v2)…". ✓

**5. [OK] SECTION-MAP both Decay rows now P1.**
Line 39 (DAG §1): "9.4 Decay+Audn — Decay+Audn=P1 per HARDQ B8, P0=explicit facts". Line 110
(STATUS-LEDGER §3): "9.4 Decay+Audn(P1 — HARDQ B8 2026-09-03; P0=explicit facts, no decay)". ✓

**6. [OK] DESIGN-STATUS #3 records bwrap-P0.**
Line 10 — "AŽURIRANO 2026-09-03 (HARDQ D1): **bwrap = P0 ENFORCED backend** … native helper …
= **P1 hardening put**. Probe OPEN = hostile-conformance suite protiv bwrapa na stvarnom
deployment hostu." ✓

**7. [OK] DESIGN-S0 top change-record supersedes native-first + Activator-as-P0.**
DESIGN-S0:3-10 — CHANGE-RECORD (1) bwrap P0 ENFORCED, native = P1 hardening path; "Every 'no
bwrap fallback'/native-first clause below reads accordingly"; (2) transactional Activator NOT a P0
build item. ✓

**8. [OK] E11 reject-unknown wording.**
Essentials:140-142 — "unknown schema ID/version → REJECT unprocessed in P0, raw input preserved
(the upcast chain + QUARANTINE sink are the v2 migration layer's — HARDQ C7 — and never a lossy
downcast or silent drop when they activate)". Matches C7. ✓

**9. [OK] E4 carries B7 WAL/busy_timeout/three BEGIN IMMEDIATE recipes.**
Essentials:60-64 — "WAL + bounded `busy_timeout`; one serialized append actor …; core-state
projections fold in the SAME transaction; three named `BEGIN IMMEDIATE` recipes …: inbox-admission
+ journal event · occurrence + run admission · terminal result + outbox enqueue. Daemon owns the DB;
CLI connects via UDS." Matches B7. ✓

**10. [OK] P0-additions carry C3 stuck-scope.**
Essentials:232-234 — "stuck-detection scoped to WITHIN one interactive turn — scheduled/polling
occurrences carry an explicit continuous-loop policy exempt from the identical-argument breaker
(C3; RED in tasks-P0.md)". ✓

**11. [OK] P0-additions carry C8 positive health signals.**
Essentials:235 — "P0 health = liveness heartbeat + last-occurrence-fired counter (C8)". ✓

**12. [OK] F1 five-check capability-scoped doctor.**
Essentials:235-240 — "checks kernel/ABI floor, bwrap, data-dir permissions, provider key, Telegram
token" (five) → "results are capability-scoped (no key → conversation off; no token → Telegram off;
no bwrap → exec off; unsafe data-dir → stateful startup blocked); `P0-capable` = all six PRD §6
criteria reachable, not sandbox alone". ✓

---

## NEW contradictions — none material

One minor residual (not a distortion, worth a one-line tidy): the P1.6 **wiring-table entry**
(HARNESS-SPEC:1344) still reads "P1.6 channels→extensions closure + HITL token" unqualified, while
the body (1461) now splits builtin/plugin. The table is a routing index, not the contract, and the
rewritten body sits immediately below, so no reader is misled — but "channels→extensions" there now
reads as blanket where the body says builtin requires no extensions. Suggest updating line 1344 to
"P1.6 channels closure (builtin: 7.3+approval-core+identity · plugin:+extensions/S6.8/S11.5) + HITL
token".

All other cross-checks clean: B7's "core-state projections fold in the SAME transaction" does not
violate E4's "never parallel writers" (the single append actor still owns append + sequence);
E4's memory-index (projection) vs memory-spine (separate store) distinction is internally coherent;
P0.11's "AtomicWriter-only in P0" annotation scopes the still-intact MUST clause rather than
contradicting it; E11's inline "— HARDQ C7 —" attribution compensates the source line not listing C7.

---

## Verdict

All 12 fixes are correctly folded into the owner documents, with the no-distortion constraints
(hostile-suite invariant, fail-closed, at-least-once honesty, plugin-path RED) preserved. The single
residual is a non-misleading wiring-table summary line that now trails the rewritten P1.6 body.

VERDICT: PASS

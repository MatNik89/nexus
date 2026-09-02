# HANDOFF — NEXUS (resume point za novu sesiju) — ČITAJ PRVO

Repo: `/home/matej/HARNESS/nexus/` (github MatNik89/nexus, private). Sve commitano, git čist.
Zadnji commit lanac završava PRD + SDD-metodom. Radi u **bypass modu**, **per-slice SDD**.

## ŠTO JE NEXUS
Osobni AI asistent (asistent-PRVO; coding = najjača grana, ne svrha). Greenfield **Go**,
`CGO_ENABLED=0`, single-binary kernel. Jedna jezgra, dva profila. Vlastiti korisnikov, editabilan
(jer ne može mijenjati Hermes). **Linux-prvo.**

## STANJE (gotovo)
- **Arhitektura ZAKLJUČANA:** `HARNESS-PLAN.md` (108 podsekcija S0-S18) + `SECTION-MAP.md` (build-order
  faze K-P) + `DESIGN-*.md` + `DESIGN-FIXES-r2.md`. **9 rundi red-teama (codex+kilo+agy) → 0 blokera,
  3× PASS.** Stručni verdikti (`VERDICT-*.md`): arhitektura potvrđena, bez redesigna.
- **PRD.md GOTOV** (product-sloj, korisnikov 6-datoteka standard). Odluke: asistent-prvo · P0=asistent-
  lice · Linux-prvo.
- **HARNESS-SPEC.md** = normativni Annex A (P0.1-P2.7 + dopuna P0.5-P0.13).

## METODA (odlučeno): SDD (Spec-Driven Development) PER-SLICE
- Veliki HARNESS-PLAN + DESIGN-* = **VIZIJA/referenca** (ne razlagati svih 108 u taskove — paraliza).
- Gradnja ide **po komadu (slice)**: fokusiran spec → arhitektura-podskup → **tasks.md** → implement → RED → sljedeći slice.
- Kanonski SDD = Constitution→Specify→Plan→**Tasks**→Implement (GitHub Spec Kit/Kiro). Nama fali **Tasks
  faza** — dodati. Multi-agent red-team = naš BMAD-adut, zadržati na svakom slice-u.

## SLJEDEĆI KORACI (redom, per-slice za P0)
1. **ARCHITECTURE.md** — postoji sadržajno (HARNESS-PLAN+DESIGN-*); po potrebi konsolidirati.
2. **ARCHITECTURE-ESSENTIALS.md** — 5-15 kritičnih odluka (cheat-sheet). *(sljedeće što radim)*
3. **Hard-questions** runda (agenti napadaju: što-puknuti/edge/over-engineered) → update PRD/Arch/Essentials.
4. **CLAUDE.md + AGENTS.md** za repo (constitution/agent-pravila).
5. **`tasks-P0.md`** — SAMO za P0 slice, granularno + traceable na PRD + acceptance po tasku + jedan-po-jedan.
6. **Implement P0** (walking-skeleton), test-first (RED iz Annex A + DESIGN-*).

## P0 SCOPE (asistent-lice vertikala, Linux) — 6 sposobnosti
1. Razgovor (TUI/CLI + provider) · 2. Pamćenje preko sesija (spine) · 3. Podsjetnici/obveze (ObligationStore)
4. Telegram kanal (+ udaljeni-HITL) · 5. Odvojeni profili posao/privatno (PersonalProfile) · 6. Siguran
sandbox (Linux Landlock+seccomp — NAJVEĆI RIZIK, dokazati hostile-conformance suiteom PRIJE oslanjanja).
Done-kriteriji: PRD.md §6.

## 4 CODE-VERIFIED DIFERENCIJATORA (P1, coding-grana): TIA · evidence-gate · AST-symedit · MEMORY_FORGET vs DATA_PURGE
## IZBAČENO IZ v1 (stručni konsenzus): learned-router, council/flows/boards, GraphRAG/CAG, fleet/pairing, video, GPU-OCR, plugin-DI

## KLJUČNI RIZICI: (1) S6.2 Linux sandbox od nule = load-bearing, probe-neverificiran. (2) širina 108. (3) asistentski USP mora biti bolji od Hermesa, ne "me too".

## ORKESTRACIJA (herdr) — GOTCHE
- 3 agenta: **target = pane_id** `w8:p2`=codex, `w8:p3`=kilo, `w8:p4`=agy (imena rev-* su NESTALA; NE koristi imena).
- `herdr agent prompt w8:pN "PROMPT" --wait --until working --timeout 8000`. **Provjeri da je stvarno krenulo** (status working; progutana greška = nije).
- Agentov **cwd zna biti TOPKNOT**, ne nexus → **koristi APSOLUTNE putanje** u promptu za čitanje/pisanje.
- `cd /path` kao prompt agentu MIJENJA njegov radni dir (CLI ga primi), ali herdr `cwd` field ne osvježava — provjeri čitanjem panea.
- Poll fajlova u pozadini (`run_in_background`), NE foreground sleep-loop.

## BYPASS MODE (korisnik traži prije koda)
- Agenti: relaunch svakog u bypass/yolo (codex: bypass-approvals; claude-agent: `--dangerously-skip-permissions`).
- Claude (ja): nova sesija u bypass-permissions modu.
- Sigurnosne mreže (sandbox/egress/audit) OSTAJU i u bypassu; gasi se samo potvrde.

## GIT: sve commitano, čisto. `git log --oneline` za povijest (review-lanac R2-R9 + PRD).

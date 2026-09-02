# REVIEW2 KONSOLIDIRANO — red-team OBA dokumenta (codex+kilo+agy+moderator)

Izvori: `REVIEW2-{codex,kilo,agy}.md`. Rangirano po konvergenciji + moderator-procjena.
**Presuda sva 3 neovisno: NIJE spremno za scaffold.** Nekoliko kritičnih blokera, dio su MOJE greške.

---

## KRITIČNI BLOKERI (moraju prije koda)

### K1 — EffectPath ZAOBILAZI sandbox (S6.2) — REALNA arhitektonska rupa (codex R2-C09, agy RUN-01)
`DESIGN-memory-effectpath-kilo.md` `RunTool` zove `p.proc.Spawn` (S1.2) izravno; **S6.2
SandboxBackend (Landlock/seccomp) NIJE u structu ni pozivu.** Proglašeni put S3→S4→S6.0→S6.9→S1.2→S7
NEMA S6.2. Znači svaki shell/exec alat bi se pokrenuo NESANDBOXIRAN. **Ovo je moja greška** — označio
sam "effect-path RESOLVED", a effect-path je bez sigurnosne membrane. Fix: `EffectPath` mora sadržavati
`sandbox.Boundary`; S1.2 je samo process-tracking UNUTAR `SandboxBackend.Launch`; put ide
S6.0.Decide→S6.9.Before→**S6.2.Launch**(koji koristi S1.2)→S7.

### K2 — EffectPath pretpostavlja da su SVI alati OS-procesi (agy RUN-02)
`proc.Spawn(call)` za svaki ToolCall lomi in-process Go alate (edit/read/grep/archmap/memory =
in-process, ne subprocess). Fix: `ToolExecutor` grananje `InProcessExecutor` vs `SandboxedProcessExecutor`.

### K3 — Fleet ↔ Pairing cirkularni Go import = COMPILE-BLOKER (kilo R2-10)
`GAPFIX-codex`: `fleet.ExecutionNode` ima `*pairing.DeviceID`, `pairing.RemoteInvoke` ima
`fleet.NodeID`/`fleet.PlacementGrant` → parent↔child import-cycle, Go NE kompajlira. Fix: zajednički
`contracts`/`types` paket za ID-ove.

### K4 — Build-order DAG ima cikluse i inverzije (codex C05-C08, agy DAG-01..04, kilo R2-09/13)
SECTION-MAP faze nisu DAG:
- **S4↔S5:** edit (4.3) treba ShadowGit(5.1)+AtomicWriter(5.3), a S4 je PRIJE S5 → **S5 mora prije S4** (agy DAG-01).
- **S3↔S16.6:** loop (3.1) ne završava bez Checker.Grade(16.6), a S3 je prije 16.6 (codex C06, agy DAG-04).
- **S3→S8/S11:** turn treba prompt(11.1)+budget(8.1) iz faze M (agy DAG-02).
- **L→M/P backdep:** 4.4 archmap owner=8.3(M); 2.6 learned-router reward=16.4(P) (codex C07).
- **M→O/P backdep:** 9.5→14.5/10.6; 9.6→16.1 (codex C08, kilo R2-13).
- **S1.2↔S7:** lease "kanonski recovery" u S1.2 ali definiran u S7 (codex C05).
- **7 novih stubova NEMA u DAG-u** — samo u status-ledgeru (kilo R2-09).
Fix: izvući MINIMALNE jezgrene ugovore (8.1/11.1/16.6-deterministic/S1.2-process-identity) u ranu fazu; DAG po ID-u ne po S-broju.

---

## DOC-INTEGRITET (fixabilno, dio MOJE greške u SECTION-MAP)

### D1 — Broj podsekcija nema jedan izvor istine (SVA 3: codex C01, kilo R2-01, agy SEC-01)
82 (plan) vs 81+inline (SECTION-MAP) vs **107 stvarnih `**X.Y` markera** vs 83 (ledger-zbroj). Fix:
generirati JEDAN kanonski ID-manifest iz plana, iz njega računati status/fazu. (100 iz spec-a + 7 novih = 107.)

### D2 — journal=S0.3 je KRIVO (codex R2-C03) — MOJA greška
U SECTION-MAP owner-invarijantama napisao "journal=0.3". **S0.3 je capability-negotiation**; journal
je **P0.3 UGOVOR** (+ nenumerirani paket), ne subsection S0.3. Fix: journal owner = P0.3/EventJournal paket.

### D3 — 12.3 i 13.4 IZOSTAVLJENI iz SECTION-MAP ledgera (agy SEC-02) — MOJA greška
12.3 routing i 13.4 video postoje u planu ali nemaju status/fazu u ledgeru. + 14 "inline" bez ikone (SEC-03).

### D4 — 7 stubova KRIVO klasificirani (kilo R2-14, agy SEC-04)
Imaju PUN buildable dizajn+RED u GAPFIX-*, a označeni 🟠 "tanko/ODLUKA-PRD". Status je "✅ DIZAJN-GOTOV
+ scope-flag", ne goli ODLUKA-PRD. Rizik: brutalni P0-rez ih krivo odbaci kao nedizajnirane.

### D5 — Preostali salvage u S10-S18 (kilo R2-02) — MOJ cleaner promašio
`core/kg.py`(10.4), `core/gate.py`(12.4), `core/pdf.py`(13.5) + ne-Go alati: alembic/yoyo(18.4),
pypdf/OCRmyPDF(13.5), sqlite-rag vs sqlite-vec drift(10.3). Fix: dovršiti cleanup.

### D6 — 18.6 device-pairing IZGUBLJEN (kilo R2-16)
GAPFIX-codex ima ODVOJEN 18.5 fleet + 18.6 pairing; plan/SECTION-MAP ih stopili u jedan 18.5. 18.6 dizajn bez plan-home.

### D7 — Fold-target kontradikcije (kilo R2-17)
G1/G2/G4/O6 imaju DVA različita home-a (GAPFIX-kilo sam sebe mapira drukčije nego plan): npr. ObligationStore
= "16.1+3.6" (GAPFIX) vs "9.6" (plan) → mijenja fazu M vs P. + A4-G6/G7/G9 fold DEKLARIRAN ali NIJE primijenjen (R2-18).

---

## GATE + IDENTITET (već znano iz PLAN-HOLES, sad potvrđeno+prošireno)

### G1 — 7 fantomskih ugovora P0.5-P0.13 (agy GATE-01, potvrđuje raniji nalaz)
Citirani kao gate u S0.3/S1/S2.3/S2.4/S3.5/S5/S9.4, NE postoje u Annex A (samo 17). + **65+ podsekcija bez
IJEDNOG Annex gatea** (agy GATE-02) — cijeli S10 retrieval i S13 multimodal bez gatea.

### G2 — Identitet: proturječje NA VRHU plana (agy ID-01) — nije riješeno
Redak 42 "coding = PRVORAZREDNI CILJ" vs A3.1 "asistent PRVO". Fix: vrh plana → "osobni asistent s
coding-jezgrom, jedna jezgra/dva profila". + checker opis u planu još usko "git-diff+exit" (ID-02) dok
je dizajn generički. + asimetrija: 4 coding dizajna, 0 asistentskih ugovora (ID-03).

---

## VLASNIŠTVO-INVARIJANTE prekršene (codex C10/C11, kilo R2-08/R2-11)
- **9.6 ObligationStore piše izravno u sqlite** → krši P0.3 single-write-owner (mora kroz journal) (kilo R2-11).
- **AccountFleet uvodi drugi retry-engine** → krši S7-sole-owner (codex C11).
- **policy/lifecycle zamijenjeni**: 3.1/4.8 rade `authorize(6.9)` a 6.9 je lifecycle; treba 6.0.Decide→6.9 (codex C10).
- **15.5 vs 6.10 dvostruki owner** swallow-signala (kilo R2-08).

## NEIZVODIVI RED-ovi (nova instanca uz cancel-no-side-effect)
- **4.8 "keystroke u password→6.1 DENY"** — OS input-injection ne zna što je pod kursorom (kilo R2-15).

---

## MODERATOR — najslabija karika + iskrena procjena
- **Najtvrđi nalaz:** K3 (fleet↔pairing cirkularni import) — mehanički, blokira compile.
- **Najvažniji za ispraviti:** K1 (sandbox nije u effect-pathu) — sigurnosna rupa; MOJ "RESOLVED" bio kriv.
- **Moje greške koje su našli:** effect-path bez sandboxa (K1), journal=S0.3 (D2), 12.3/13.4 izostavljeni (D3),
  7 stubova krivo klasificirani (D4), preostali salvage (D5), 18.6 izgubljen (D6), count-rascjep (D1).
- **Konvergencija:** count (3/3), sandbox-gap (2/3), DAG-cikluse (3/3), gate-void (2/3 + ranije), identitet (2/3).
- **Presuda:** dokumenti su bliže spremnosti nego prošli krug, ali NE za scaffold dok se ne zatvore K1-K4
  (kritični) + D1-D2 (manifest+journal). D3-D7/G1-G2 su doc-fixevi. Dio (7 stubova asistent-dubina, gate-void)
  čeka PRD scope-odluku.

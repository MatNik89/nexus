# REVIEW2-kilo — RED TEAM: S10-S18 + 7 NOVIH + addendum-fold

Metoda: cross-check `HARNESS-PLAN.md` ↔ `SECTION-MAP.md` ↔ `GAPFIX-*.md` ↔ `DESIGN-*.md` ↔
`PLAN-HOLES-CONSOLIDATED.md`. Tražim SAMO nove rupe (C1-C8 + DEEP su već pokriveni). Po ID-u.

---

## A. NOVE RUPE S10-S18

**R2-01 — Count je nekonzistentan (82 vs 107 vs 83).**
Header (`HARNESS-PLAN.md:5,7,784`) i C1 tvrde "82 podsekcije". Stvarni broj boldiranih
`**X.Y` marker-a = **107** (grep). `SECTION-MAP.md:99` Zbroj daje ~83 (22+9+30+12+3+7).
Tri dokumenta se ne slažu; kanonski "82" (kojim je C1 "ispravio" stari broj) je i sam netočan.
Ovo podriva sve "ZAOKRUŽEN"/"82 podsekcije" tvrdnje o potpunosti.

**R2-02 — Rezidualni salvage u S10-S18 nakon "salvage uklonjen".**
Header (`:3-5`) deklarira nula salvagea, ali u opsegu ostaju: `core/kg.py` (`:527`, 10.4),
`core/gate.py` (`:593`, 12.4), `core/pdf.py` (`:637`, 13.5). To su NEXUSv2 Python putanje u
greenfield Go planu. C1 je bio generalan; ovo su konkretni preostali slučajevi — cleanup je
deklariran dovršenim a nije.

**R2-03 — 13.5: dva dokumenta se izravno sukobljavaju.**
Plan `:636-637` = "pypdf/OCRmyPDF/pyHanko" (Python). `SECTION-MAP.md:92` = "pure-Go(pdfcpu/chromedp)".
Pure-Go odluka je zabilježena u SECTION-MAP ali NIJE propagirana u sekciju. (Razlika vs C4: ne
ponavljam "Py je ne-Go" — ovdje je "fold odluke nije došao do izvora".)

**R2-04 — 18.4 "alembic/yoyo-migrations" = Python alati.**
`HARNESS-PLAN.md:772` predlaže alembic/yoyo (SQLAlchemy/Yoyo) za migracije u Go SQLite projektu.
Treba golang-migrate/embedded SQL. C4 nije pokrio alembic/yoyo — novi ne-Go ostatak.

**R2-05 — 10.5 CAG + A1-OCR dijele istu NERAZRIJEŠENU local-inference capability.**
10.5 CAG implementacija je "UNCLEAR (infra vLLM/SGLang je 8.4)"; A1 GPU-put (Unlimited-OCR 3B VLM)
visi o ISTOJ 8.4/self-hosted infra. Dva neovisna potrošača (retrieval + OCR) ovise o jednoj
🟡 8.4 sposobnosti koja nema dizajn. Nitko ne vidi cross-dependency.

**R2-06 — 10.3 naming drift: `sqlite-rag` vs `sqlite-vec`.**
Plan `:524` = "LanceDB/sqlite-rag"; A9 KOMPONENTNA (`:1122`) = "Qdrant/LanceDB/sqlite-vec".
Različiti vektorski backend imenovan na dva mjesta. Dodatno, cross-encoder
"sentence-transformers/BGE" (`:524`) = PyTorch sidecar, nigdje nije označen kao subprocess-trošak.

**R2-07 — 18.2 citira `codex#5` kao RED gate koji ne postoji.**
C3 je pokrio codex#2/#3/#19 kao backlog. `18.2` (`:765`) dodaje `codex#5` — četvrti nepostojeći
Tier-3 gate, nije u Annex A ni u C3 listi.

**R2-08 — 15.5 vs 6.10: dvostruki vlasnik "swallow" signala.**
15.5 (`:696`) i 6.10/O6 (`GAPFIX-kilo.md:177-178`) OBA proizvode "error-swallow → breaker" signal.
Nije definiran owner signala (tko piše, tko konzumira S7 breaker). Preklapanje bez arbitraže.

---

## B. 7 STUBOVA (4.8 / 6.10 / 9.5 / 9.6 / 12.7 / 12.8 / 18.5)

**R2-09 — Svih 7 NEDOSTAJE u build-order DAG-u.**
`SECTION-MAP.md:15-46` (FAZA K-P) ne sadrži NITI JEDAN od 7 novih. Fold je otišao u STATUS-LEDGER
(§3) ali NE u DEPENDENCY/BUILD-ORDER (§1). Faza i dependency-edges 7 stubova su nedefinirani —
build-order je lažno potpun.

**R2-10 — Fleet ↔ Pairing: cirkularni Go package import (ne kompajlira).**
`GAPFIX-codex.md`: `fleet.ExecutionNode` ima `Device *pairing.DeviceID` (`:673`), a
`pairing.RemoteInvoke` ima `Node fleet.NodeID` (`:839`) + `Invoke(..., fleet.PlacementGrant, ...)`
(`:856`). Parent `fleet` ↔ child `fleet/pairing` (ili `pairing` ↔ `fleet`) = import-cycle.
Go odbija parent↔child ciklus. Treba zajednički `contracts`/`types` paket za ID-ove. Ovo je
compile-blocker u dizajnu, ne niša.

**R2-11 — 9.6 ObligationStore krši P0.3 single-write-owner.**
`GAPFIX-kilo.md:119` G4 = `ObligationStore{ store *sqlite.Store }` — PRAVI izravni SQLite write.
Suprotno `HARNESS-PLAN.md:88-89` ("EventJournal.Append = jedini trajni write-owner") i suprotno
`GAPFIX-codex.md:11-13` (board/fleet = "transakcijske PROJEKCIJE journal-eventa"). Dva GAPFIX
dokumenta se ne slažu: kilo piše memoriju mimo journala, codex sve kroz journal. "Trajni cilj"
(9.6) mora ići kroz journal, ne direktno u sqlite.

**R2-12 — 9.6 vs 9.2 vs 7.2: granica je ASSERTIRANA, ne definirana.**
Plan `:505` kaže "odvojeno od 9.2 razgovorne memorije i 7.2 infra-queue" — ali G4 nosi
`Recurrence *s3.ScheduleManifest` + `EvaluateDue` = strukturno IDENTIČNO raspoređenom 7.2 tasku
(lease) + 3.6 triggeru. Diskriminator "obligacija ≠ queue task ≠ memory goal" nema mehanizam:
tko dispatcha evaluaciju (S7 grant? 3.1 loop?), tko drži DONE-state (journal? queue? memory?). Bez
toga je 9.6 četvrti perzistentni state-store uz journal/queue/memory.

**R2-13 — Fazni hazardi 7 stubova (M→P i K→N):**
- **9.6 (S9/faza-M) → 16.1 app-contract (faza-P)** i → 16.6 (faza-L). `MarkDone` done-def
  = 16.1; evidence = 16.6. S9 građena prije P ali ovisi o P. Najteži forward-dep.
- **9.5 (faza-M) → 14.5 channel-binding (faza-O) + 10.6 ACL (faza-N)** (G2: memory+secrets+channels+cache
  kao JEDNA jedinica). M ovisi o O.
- **6.10 (S6/faza-K) → 15.3/15.5 (faza-N) + 5.3 taint (faza-L)** (O6). K ovisi o N.
- **12.8 (faza-N) → 9.6 (M) → 16.1 (P)**; lanac N→M→P.
Svi su "Scope: ODLUKA-PRD" pa se hazard skriva iza PRD-a — ali build-order DAG ih ne modelira.

**R2-14 — 7 stubova su pogrešno klasificirani kao "tanko/ODLUKA-PRD".**
Svi imaju PUN buildable dizajn + RED u `GAPFIX-*.md` (12.7/12.8/18.5 u codex; 4.8/6.10/9.5/9.6 u
kilo). Plan ih označava "Dizajn: GAPFIX-X. **Scope: ODLUKA-PRD**" — a legenda 🟠 = "tanko/bez
dizajna". Status je "✅ DIZAJN-GOTOV + 🟠 scope-flag", ne goli "ODLUKA-PRD". C6 brutalni rez će ih
krivo odbaciti kao nedizajnirane.

**R2-15 — 4.8 RED "keystroke u password-polje→6.1 DENY" je neizvodiv.**
OS-level input injection ne zna što je pod kursorom; detekcija password-polja traži OCR/semantičko
razumijevanje GUI-ja. Gate je deklariran ali nema implementacijski put (slično nemogućem
"cancel→nikakav side-effect" iz C5, ali nova instanca).

---

## C. ADDENDUM-FOLD — izgubljen / krivo mapiran sadržaj

**R2-16 — 18.6 device-pairing je IZGUBLJEN (osmi dizajn, nema ga u planu).**
`GAPFIX-codex.md` ima 18.5 (fleet) I 18.6 (pairing), s eksplicitnim "Device identitet/pairing je
odvojen u 18.6" (`:642`). Plan `:774` stapa oboje u JEDAN 18.5 "fleet + device-pairing". SECTION-MAP
`:97` isto stapa. "7 NOVIH" broji fleet+pairing kao JEDAN. 18.6 je pun dizajn bez plan-home.

**R2-17 — Fold-target kontradikcije (GAPFIX-kilo vs plan/SECTION-MAP):**
| Gap | GAPFIX-kilo sam sebe mapira | Plan/SECTION-MAP |
|---|---|---|
| G1 computer-use | "S4.5 (browser/computer-use)" (`:38`) | NOVA 4.8 (a 4.5 ostaje browser) |
| G2 PersonalProfile | "S6.6 proširenje" (`:225`) | NOVA 9.5 |
| G4 ObligationStore | "16.1+3.6 spoj" (`:225`) | NOVA 9.6 |
| O6 exec-auto-reviewer | "6.9 + 15.3/15.5" (`:180`) | NOVA 6.10 |
Izvor i odredište fold-a se ne slažu — svaka od ove 4 ima DVA različita home-a. Graditelj ne zna
je li ObligationStore memorija (9.x) ili eval/trigger (16.x/3.6), a to mijenja fazu (M vs P).

**R2-18 — A4-G6/G7/G9 fold je DEKLARIRAN ali NIJE PRIMIJENJEN.**
SECTION-MAP `:66` tvrdi fold (cache-lineage→8.4, learn-control→9.3, channel-caps→14.5), ali:
- 8.4 cache-key (`:467-468`) NEMA session-lineage (G6).
- 9.3 (`:492`) NEMA korisnički learning-control (G7).
- 14.5 (`:662`) NEMA surface-capability snapshot (G9).
G8 (AccountFleet→2.2) ima pun dizajn u `GAPFIX-agy.md` ali 2.2 (`:198-201`) i dalje piše samo
"Cooldown/KeyPool rotacija" — dizajn postoji, tekst sekcije nije ažuriran. Isti obrazac kao R2-14.

---

## D. UNCLEAR (10.5 CAG, 18.4 backup) — blokiraju li build?

**R2-19 — 10.5 CAG: NE blokira (decision buildable), ALI ima visak.**
`CAGDecision.Decide` je čisti Go + RED testira SAMO authz granu ("neautoriziran→RAG"). Taj dio je
buildable i testable DANAS. Nerazriješen je CAG IZVRŠITELJ (KV-preload = 8.4 self-hosted + vLLM/SGLang).
Plan ne kaže što se događa kad odluka vrati `CAG` a executor ne postoji → dangling decision.
**Zaključak:** ne blokira AKO se v1 eksplicitno svede na "CAG = reserved, odluka se degradira u RAG".
Plan to ne piše — treba dodati fallback rečenicu, inače je odluka lažno-buildable.

**R2-20 — 18.4 backup: NE blokira, ali je BLOKING-PREMISA KRIVA.**
"UNCLEAR dok S0.4 ne odabere persistence backend" (`:771`, `SECTION-MAP:97`) je category error:
S0.4 je verzioniranje EVENT-SHEME (SchemaRegistry/upcast), NE odabir persistence backenda. Backend
je već odabran (SQLite WAL modernc za single-user, Postgres samo za `service` — A9 `:1120-1121`).
Single-user backup (Litestream sidecar / WAL snapshot) je buildable sada; Postgres backup
(pgBackRest/WAL-G) prati `service` PRD. Plus: "alembic/yoyo" (R2-04) je Python ostatak u istoj rečenici.

---

## E. Presjek (što je provjereno / što nije)

- **Provjereno:** grep 107 podsekcija vs "82" claim; sve linije citirane gore čitane direktno;
  GAPFIX-{kilo,codex,agy} + SECTION-MAP + PLAN-HOLES + DESIGN-STATUS pročitani u cijelosti.
- **Neprovjereno (pretpostavka):** nisam kompajlirao fleet/pairing cycle (R2-10 je strukturna
  činjenica — Go parent↔child import-cycle ne prolazi compile, ali nisam ga pokrenuo); nisam
  neovisno potvrdio da vLLM/SGLang strogo treba za CAG (preuzeto iz planove vlastite tvrdnje).
- **Najslabija karika:** R2-10 (cirkularni import) je najtvrdi nalaz — specifičan, mehanički
  provjerljiv i blokira compile; ostalo su uglavnom konzistencija/fold/faza hazardi.

topknot: ultra+preflight

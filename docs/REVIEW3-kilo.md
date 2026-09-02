# REVIEW3-kilo — verifikacija r2 fixeva (K3/R2-11/R2-16/R2-02/R2-14/R2-17) + novi regresije

Provjera u dokumentima (ne na riječ): `DESIGN-FIXES-r2.md`, `SECTION-MAP.md`, `HARNESS-PLAN.md`,
`HARNESS-SPEC.md`, `GAPFIX-{kilo,codex,agy}.md` (mtimes: GAPFIX-* = 13:53-14:02, **NEPROMIJENJENI**;
fixevi = 22:11-22:15).

---

## A. VERIFIKACIJA DODIJELJENIH NALAZA

**K3 (fleet↔pairing cirkularni import) — DJELOMIČNO.**
Ciklus jest strukturno razriješen: `fleet→ids`, `pairing→ids`, nema parent↔child importa
(`DESIGN-FIXES-r2:58-66`, `SECTION-MAP:52,110`, plan `18.5/18.6` → `fleet/ids`). ALI dva problema:
1. **Izvor i dalje ima ciklus.** `GAPFIX-codex` (mtime 14:02, nepromijenjen) doslovno i dalje ima
   `fleet.ExecutionNode{ Device *pairing.DeviceID }` (`:673`) i `pairing.RemoteInvoke{ Node fleet.NodeID }`
   (`:839`). Fix je "nadjačava" nota, ne edit koda — grep nad dizajnom i dalje vraća ciklus.
2. **"ids = BEZ ovisnosti (list rung)" je NETOČNO.** `PlacementGrant` (stavljen u `ids`) nosi
   `Task queue.TaskID` + `Attempt reliability.AttemptGrant` → `ids` uvozi `queue`+`reliability`.
   Nije leaf; layering-claim kontradiktoran samom sebi. RED `TestNoImportCycle` postoji, ali source
   nije popravljen.

**R2-11 (9.6 piše kroz journal?) — DJELOMIČNO.**
`DESIGN-FIXES-r2:53-54` + `SECTION-MAP:101` ("9.6 write→journal P0.3") kažu DA. Ali `GAPFIX-kilo`
(mtime 13:54, nepromijenjen) i dalje ima `ObligationStore{ store *sqlite.Store }` (izravni write),
a plan `9.6` tekst (`:509`) ne spominje journal. Override postoji, ali kanonski dizajn i sekcija
kažu suprotno.

**R2-16 (18.6 vraćen?) — ZATVOREN.**
18.6 je u planu (`:779`), SECTION-MAP fold-tablici (A10 → `18.5/18.6`, `:79`), DAG-u (`:52`),
statusu (`:110`), broj podignut na 108.

**R2-02 (salvage kg/gate/pdf.py maknut?) — ZATVOREN.**
`core/kg.py` → "greenfield Go KG-store" (10.4, `:531`); `core/gate.py` → "greenfield Gate" (12.4,
`:596`); `core/pdf.py` → "Pure-Go pdfcpu/rsc.io-pdf" (13.5, `:639`). Sva tri NEXUSv2 `.py` referenca
maknuta. (Preostali `.py` u S0-S9 su izvan ovog nalaza.)

**R2-14 (7 stubova reklasificirano?) — ZATVOREN (u SECTION-MAP).**
Svih 8 novih nose `✅🟠` (`SECTION-MAP:96-110`) + eksplicitan fix "NE 'tanko'" (`:89`). Caveat: plan
tekst podsekcija i dalje piše goli "Scope: ODLUKA-PRD" bez ✅ — reklasifikacija nije propagirana u
plan, ali D4 je bio SECTION-MAP bug i tamo je zatvoren.

**R2-17 (fold-target) — NIJE.**
`GAPFIX-kilo` (nepromijenjen) i dalje sebe mapira G1→`S4.5`, G2→`S6.6`, G4→`16.1+3.6`, O6→`6.9`,
dok plan/SECTION-MAP drže `4.8/9.5/9.6/6.10`. Dva home-a po gapu i dalje postoje. Zatvoren je samo
D6 (18.6); D7 (fold-target kontradikcija) NIJE taknuta.

---

## B. NOVI BLOKERI / REGRESIJE (uveli ih fixevi)

**R3-01 — Patch-layer = dva izvora istine (meta-regresija, NAJVEĆI).**
Svi K1-K3 + vlasništvo fixevi su u `DESIGN-FIXES-r2` kao "nadjačava", dok su kanonski dizajni
(`GAPFIX-kilo`, `GAPFIX-codex`, `GAPFIX-agy`, `DESIGN-memory-effectpath-kilo`) NEPROMIJENJENI.
Graditelj koji otvori GAPFIX-codex sam vidi ciklus; GAPFIX-kilo sam vidi izravni sqlite; effect-path
dizajn sam vidi `RunTool` bez sandboxa. Fix je ručna patch-nota, ne propagiran edit → verifikabilnost
K-fixeva ovisi o tome čita li se DESIGN-FIXES-r2. Niti jedan "nadjačani" dokument ne nosi marker
"SUPERSEDED".

**R3-02 — Tri r2-fixa NISU propagirana u plan-tekst (bug još "živ" u sekciji):**
- **4.8** (`:317`) i dalje "keystroke u password-polje→6.1 DENY" — `DESIGN-FIXES-r2:68-72` je zamijenio
  RED neizvodivim→`TestKeystrokeIntoUnknownFieldRequiresApproval`, ali plan-tekst zadržao stari.
- **18.4** (`:774`) i dalje "UNCLEAR dok S0.4 ne odabere persistence backend" — premisa je u
  SECTION-MAP (`:110`) proglašena krivom ("S0.4 ne bira backend"), ali plan-tekst je nepromijenjen.
  (alembic/yoyo → golang-migrate jest popravljen.)
- **10.5** (`:535`) i dalje "Top-3 IMPLEMENTACIJA UNCLEAR" bez fallbacka; SECTION-MAP (`:102`) ima
  "reserved→degradira u RAG", plan nema. Dangling-CAG i dalje u planu.
  → Isti obrazac kao R3-01: fix zapisan u SECTION-MAP/DESIGN-FIXES, sekcija u planu nedirnuta.

**R3-03 — Broj i dalje nema jedan izvor (D1 nije zatvoren).**
SECTION-MAP: naslov §3 = "108" (`:87`), ali "Zbroj (107)" (`:112`), a stvarni zbroj popisanih ≈ 91
(24+9+40+15+3). HARNESS-PLAN = 108 (×3). Dodavanje 18.6 diglo je naslov na 108 a ostavilo "107" u
Zbroj-liniji. ID-manifest (D1 stvarni zahtjev) NIJE generiran.

**R3-04 — `fleet/ids` nije leaf (lažna "bez ovisnosti" oznaka).**
Preklapa s K3 ali zasebno vrijedi: `PlacementGrant` u `ids` povlači `queue.TaskID`+`reliability.
AttemptGrant` → `ids` ovisi o queue/reliability sloju. "list rung" tvrdnja u `DESIGN-FIXES-r2:61` je
kriva; ili izvuci samo ID-ove u `ids`, ili pomakni `PlacementGrant` iz `ids`.

**R3-05 — G1 djelomično: 7 od 9 ugovora, codex#N još nebackano.**
Annex A sad ima 7 realnih ugovora (P0.5-P0.9, P0.11, P0.13) s vlasnikom+MUST+RED-imenom
(`HARNESS-SPEC:1527-1565`) — G1 po tom dijelu zatvoren. ALI: (a) "P0.5–P0.13" kao raspon implicira 9,
P0.10/P0.12 "namjerno prazni"; (b) `codex#2/#3/#5/#19` i dalje citirani kao gate (10.6/12.5/18.2/8.4)
bez uporišta — nisu dotaknuti ovom rundom.

---

## C. Confidence

- **Provjereno (u dokumentima):** grep 108 markera; `.py`-salvage nestanak (kg/gate/pdf); 18.6 prisutan
  u plan+SECTION-MAP DAG/fold/status; ✅🟠 reklasifikacija; Annex A P0.5-P0.13 imaju MUST+RED imena;
  GAPFIX-* mtime-ovi (nepromijenjeni).
- **Neprovjereno:** nisam kompajlirao `fleet/ids` layout (R3-04 je strukturna analiza polja
  `PlacementGrant`); nisam potvrdio da `reliability`/`queue` nigdje ne uvoze `fleet` (po GAPFIX-codex
  ne uvoze — ali to je čitanje, ne build).
- **Najslabija karika:** R3-01 — cijeli fix-sloj je "nadjačava" nota nad nepromijenjenim izvorima; ako
  se DESIGN-FIXES-r2 ikad ispusti iz čitanja, svi K-fixevi tiho nestanu. Preporuka: edit u izvorima,
  ne patch-nota.

topknot: ultra+preflight

# REVIEW4-kilo — verifikacija iteracije-2 fixeva (moji R3 nalazi + novi)

Provjera u dokumentima: `DESIGN-FIXES-r2.md`, `GAPFIX-*`, `DESIGN-memory-effectpath-kilo`,
`HARNESS-PLAN.md`, `HARNESS-SPEC.md`, `SECTION-MAP.md` (mtimes 22:22-22:24, svi iteracija-2).

---

## A. VERIFIKACIJA MOJIH R3 NALAZA

**R3-01 (SUPERSEDED banneri) — DJELOMIČNO.**
Banneri postoje: `GAPFIX-kilo:1`, `GAPFIX-codex:1`, `DESIGN-memory-effectpath-kilo:1`. ALI
`GAPFIX-agy` (mtime 13:53, NEPROMIJENJEN) NEMA banner — a DESIGN-FIXES-r2:64-65 "nadjačava
GAPFIX-agy automatski failover" (AccountFleet = C11). Čitatelj GAPFIX-agy i dalje vidi "automatski
failover" kao važeći. 3/4 pokriveno.

**R3-02 (4.8/18.4/10.5 u planu) — ZATVOREN.**
- 4.8 (`:317`): "keystroke u nepoznato polje = approval-po-tipu-znaka (NE password-auto-detekcija)".
- 10.5 (`:535`): "CAG-izvršitelj reserved u v1; executor ne postoji → DEGRADIRA u RAG (ne dangling)".
- 18.4 (`:774`): "backend već SQLite-WAL — NE čeka S0.4 (S0.4=schema-verzioniranje, ne backend)".
Sva tri propagirana u plan-tekst.

**R3-03 (Zbroj 108) — DJELOMIČNO.**
Labela popravljena: `SECTION-MAP:112` "Zbroj (108)". ALI zbroj popisanih statusa ≈ **91**
(✅~24 + 🔵9 + 🟡~40 + 🟠~15 + ⬇3) ≠ 108 — ~17 podsekcija neupisano. ID-manifest (stvarni D1 zahtjev)
i dalje NIJE generiran.

**R3-04 (fleet/ids leaf) — ZATVOREN.**
`DESIGN-FIXES-r2:71-80`: `ids` = "SAMO skalarni ID-ovi: NodeID, DeviceID — TRUE leaf, nula importa";
`PlacementGrant` izbačen iz `ids` i vraćen u `fleet`. Banner `GAPFIX-codex:1` potvrđuje "PlacementGrant
ostaje u fleet, ne ids". Contradiction uklonjen.

**R3-05 (codex#N) — DJELOMIČNO.**
- codex#19 → formaliziran kao **P2.4** (`SPEC:1348`); codex#21 → P0.3 (`SPEC:1337`); codex#20→P0.2,
  codex#33→P2.6 (gate-mapa `SPEC:1336-1350`). ✓
- codex#2 (`plan:541`) i codex#3 (`plan:605`) sad eksplicitno "Tier-3 BACKLOG — ne formalni Annex". ✓
- **codex#5 (`plan:768`, 18.2) i dalje goli**, bez qualifiera — jedini preostali nebackani gate-citat.

**R2-11 (9.6 journal u planu) — DJELOMIČNO.**
Dizajn zatvoren: `DESIGN-FIXES-r2:66-67` + banner `GAPFIX-kilo:1` + `SECTION-MAP:101` ("write→journal
P0.3"). ALI plan-tekst 9.6 (`:509`) je NEPROMIJENJEN i i dalje ne spominje journal — tiha je, ne kriva.

**R2-17 (fold-target) — ZATVOREN.**
Banner `GAPFIX-kilo:1` eksplicitno proglašava kanonski fold: "G1→4.8/G2→9.5/G4→9.6/O6→6.10 je kanonski
u HARNESS-PLAN (ne self-mapping ovdje)". Dual-home razriješen deklaracijom.

---

## B. NOVI NALAZ

**N4-01 — `ToolSpec.ExecutionKind` je autoritativan u fixu, a NEMA ga u planu S4 (mat.-propagacija).**
`DESIGN-FIXES-r2:51,57` čini `ToolSpec.ExecutionKind ∈ {ExecInProcess, ExecProcess}` **autoritativnim
pečatom** grananja i izričito kaže "4.2 NE prisvaja tree-kill". ALI plan `4.1` i dalje definira
`ToolSpec{ID,SchemaHash,EffectClass}` (`:285`, bez ExecutionKind), a `4.2` i dalje tvrdi "tree-kill"
(`:288`). Najkritičniji izvršni ugovor (RunTool grananje) referencira polje kojeg nema u kanonskom
S4. Ovo je ISTI nalaz koji je REVIEW3-codex flagirao (V-K1); iteracija-2 popravila RunTool ali NE i
S4 ToolSpec u planu. Grep potvrđuje: `ExecutionKind` postoji SAMO u DESIGN-FIXES-r2 + banneri, nikad u
HARNESS-PLAN.

---

## C. PRESUDA

**NEMA BLOKERA za PRD.** K1-K4 + vlasništvo + gate-void su zatvoreni; preostalo su doc-propagation
sitnice, NE arhitektonski/tehnički blokeri.

**Ali PRIJE scaffolda (ne prije PRD-a) mora se počistiti:**
1. **N4-01** — dodati `ExecutionKind` u plan S4 `ToolSpec` + skinuti "tree-kill" iz 4.2 (prebaciti u S6.2.Launch/S1.2).
2. `GAPFIX-agy` banner (AccountFleet failover superseded).
3. `codex#5` (18.2) qualifier "Tier-3 BACKLOG" (kao #2/#3).
4. plan 9.6 tekst dodati "write→journal P0.3".
5. SECTION-MAP Zbroj: zbroj statusa ≠ 108 (ili generiraj ID-manifest koji je D1 tražio).

---

## Confidence

- **Provjereno:** banneri (grep SUPERSEDED + GAPFIX-agy:1 pročitan = bez bannera); 4.8/10.5/18.4
  plan-tekst; SECTION-MAP Zbroj=108 vs zbroj≈91; fleet/ids leaf (DESIGN-FIXES-r2:71-80); SPEC gate-mapa
  (codex#19→P2.4, #21→P0.3); `ExecutionKind` odsutan iz HARNESS-PLAN (grep).
- **Neprovjereno:** nisam kompajlirao `fleet/ids` layout (strukturna analiza iz teksta); nisam brojio
  svaku od 108 podsekcija u ledgeru (zbroj ≈91 je iz "~" brojki, ne točnog tallija).
- **Najslabija karika:** N4-01 — fix je autoritativan nad poljem koje plan ne definira; najlakše se
  "zatvori" ali i najlakše regresira ako se S4 ne takne prije scaffolda.

topknot: ultra+preflight

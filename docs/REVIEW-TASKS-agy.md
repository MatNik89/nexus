# REVIEW-TASKS-agy — Review of P0 Task Ledger (`tasks-P0.md`)

**Meta:** Adversarial review of `/home/matej/HARNESS/nexus/docs/tasks-P0.md` (Commit HEAD)  
**Recenzent:** Antigravity (Adversarial Task Ledger Review)  
**Authority & Sources:** `docs/HARDQ-CONSOLIDATED.md`, `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARNESS-SPEC.md` Annex A, `docs/PRD.md`, `docs/DESIGN-FIXES-r2.md`, `docs/SECTION-MAP.md`, `CLAUDE.md`, `AGENTS.md`.

---

## 1. PREGLED I METODOLOGIJA

Kao jedini autorizirani red čekanja za P0 implementaciju, `docs/tasks-P0.md` (24 zadatka, T01–T24, raspoređenih u 8 faza) podvrgnut je rigoroznom napadu kroz 4 obvezne osi:
1. **TRACE:** Točnost citiranja izvora (PRD, Annex A, HARDQ) i potpuna pokrivenost svih usvojenih HARDQ P0 stavki (bez izostavljanja usvojenog i bez krijumčarenja odgođenog).
2. **ORDER:** Ovisnosti među zadacima, poštovanje T02 sigurnosnih vrata (*gate*) i dosljednost `-min` discipline.
3. **RED QUALITY:** Provjera jesu li RED testovi red-capable i usidreni u specifikaciju (a ne izvedeni iz implementacije) te dokazuju li PRD §6 kriterije.
4. **SIZE / GRANULARITY:** Granularnost zadataka (jesu li pregledni i samostalno testabilni).

---

## 2. NALAZI PO OSIMA

---

### [TRACE] [OK] Nalaz 1 — Potpuna pokrivenost usvojenih HARDQ stavki i PRD §6 kriterija
* **Analiza:**  
  Svih 24 zadatka nose precizne `Trace:` oznake. Usvojene rezolucije iz `HARDQ-CONSOLIDATED.md` točno su mapirane u zadatke:
  - `HARDQ A1` (Telegram kao `channel:builtin`) → **T20**
  - `HARDQ A2` (P0 walking-skeleton redoslijed kroz `-min` ugovore) → **T01–T24**
  - `HARDQ B1` (Durable local scheduler + wake catch-up) → **T18**
  - `HARDQ B2` (Telegram inbox/outbox + honest delivery boundary) → **T20**
  - `HARDQ B3` (Fizička SQLite izolacija po profilu + `ProfileID` lanac) → **T16**
  - `HARDQ B4` (Ograničeni ELF runtime closure za Landlock/bwrap) → **T02, T22**
  - `HARDQ B5` (Reminder/Task asistentski dokazi) → **T10, T19**
  - `HARDQ B6` (Trajni HITL `TurnSuspended`/rehidracija) → **T21**
  - `HARDQ B7` (Jedan append actor + sinkrone projekcije + UDS) → **T05, T15**
  - `HARDQ B8` (Eksplicitne činjenice u P0, bez decay mehanizma) → **T17**
  - `HARDQ B9` (Statički sealed capability snapshot, bez Activatora) → **T07, T09**
  - `HARDQ C1–C5, C7, D1, F1` → **T02, T03, T04, T05, T19, T20, T21, T22**

---

### [TRACE] [MISSING] Nalaz 2 — Izostanak eksplicitnog spomena `s5-min` (`AtomicWriter`) u Phase 1
* **Lokacija:** `tasks-P0.md:84-90` (T08) i `164-175` (T17)
* **Analiza:**  
  `HARDQ-CONSOLIDATED.md:28`, `ARCHITECTURE-ESSENTIALS.md:80-81` i `CLAUDE.md:82` definiraju da u P0 ulaze dva ključna `-min` ugovora: `s7-min` (`AttemptGrant + cancel + no-retry`) i `s5-min` (`AtomicWriter` = pure Go `tmp → fsync → rename` za sve mutacije datoteka, P0.11).
  U `tasks-P0.md` zadatak **T12** eksplicitno nosi naziv `T12 — s7-min`, dok `s5-min` (`AtomicWriter`) nigdje u zadacima nije izrijekom spomenut (niti u T08 kod staza, niti u T17 kod eksplicitne memorije). Iako SQLite-WAL ima vlastitu atomičnost, pomoćni `AtomicWriter` (npr. u `internal/foundation/pathx` ili `internal/kernel/storage`) nužan je kako programer ne bi koristio nesigurni `os.WriteFile` za datotečne operacije.
* **Preporuka za popravak:** Eksplicitno imenovati `s5-min (AtomicWriter)` u sklopu **T08** (ili kao samostalni pod-artefakt u T04/T08) kako bi implementacija imala jasan zadatak za atomično pisanje datoteka.

---

### [TRACE] [OK] Nalaz 3 — Stroga zaštita odgođenog opsega (Deferred Ledger)
* **Lokacija:** `tasks-P0.md:259-263`
* **Analiza:**  
  Zasebni *Deferred ledger* na kraju dokumenta eksplicitno navodi sve odgođene komponente (`full S7 (P2)`, `full S5 (P3)`, `Decay+Audn (P1)`, `transactional Activator (P4+)`, `entropy secret detection (P4)`, `SemanticHealth (P2+)`, `upcast chain/quarantine (v2)`, `native sandbox helper (P1 hardening)`, `plugin channels (P)`, `coding trio TIA/symedit/deep evidence (P1)`). Nijedna od ovih komponenti nije prokrijumčarena u zadatke T01–T24.

---

### [ORDER] [OK] Nalaz 4 — T02 Sandbox Feasibility Gate i sekvencijalni integritet
* **Lokacija:** `tasks-P0.md:27-36` (T02) i `225-245` (Phase 6, T22–T23)
* **Analiza:**  
  - **T02** provodi standalone provjeru na stvarnom deployment hostu bez ovisnosti o jezgri i služi kao formalni *go/no-go gate* za Phase 6.
  - Linija 12 i zadatak T14 jamče da prije prolaska T02 i dolaska do Phase 6 sustav radi isključivo kao sigurna konverzacijska jezgra (REPL, provider, memorija, Telegram) bez pokretanja proizvoljnih naredbi na sustavu.
  - T11 definira `EffectPath`, ali se `TestReadOnlyProcessStillSandboxed` privremeno preskače (*stub-skip*) do T23, gdje se izričito aktivira. Nema cirkularnih ovisnosti.

---

### [ORDER] [OK] Nalaz 5 — Topološki redoslijed gradnje (K0 → Sigurna jezgra → Memorija → Obveze → Telegram → Sandbox)
* **Analiza:**  
  Redoslijed faza u potpunosti poštuje pravilo *Contract-First Tiering* iz HARDQ A2:
  1. **Phase 0:** Preflight & Alati (T01–T03)
  2. **Phase 1:** K0 primitivi (T04 ugovori → T05 journal → T06 state fold → T07 config → T08 process id → T09 capability snapshot → T10 checker)
  3. **Phase 2:** Sigurna konverzacijska kralježnica (T11 PEP/EffectPath → T12 s7-min → T13 Provider → T14 Loop → T15 REPL+UDS)
  4. **Phase 3:** Memorija i profili (T16 fizička SQLite izolacija → T17 eksplicitne činjenice)
  5. **Phase 4:** Obveze (T18 3.6-min scheduler → T19 ObligationStore)
  6. **Phase 5:** Telegram (T20 built-in gateway → T21 trajni HITL)
  7. **Phase 6:** Pješčanik (T22 bwrap backend → T23 exec tool)
  8. **Phase 7:** P0 Acceptance (T24 e2e verifikacija 6/6 kriterija)

---

### [RED] [OK] Nalaz 6 — Visoka kvaliteta i red-capabilnost svih 24 RED testova
* **Analiza:**  
  Nijedan RED test nije trivijalan ili izveden iz interne implementacije. Svaki test opisuje objektivni orakul:
  - **T02:** `/etc/shadow` čitanje → `EACCES`; dynamic ELF `/bin/ls` radi; shebang i nedeklarirana djeca se odbijaju; kill ubija cijelo stablo procesa.
  - **T05:** `test_projection_cannot_bypass_journal`; konkurentni turn+scheduler append daje neprekinute sekvence; tajne u payloadu se redaktiraju prije zapisa; `SIGKILL` matrica na commit granicama potvrđuje atomičnost.
  - **T14:** `test_s0_rejects_provenance_laundering` (untrusted sadržaj kroz compaction ne može postati `SYSTEM`).
  - **T16:** Pretraga u profilu A vraća 0 pogodaka za profil B; FTS5 indeks profila A ne sadrži tokene profila B.
  - **T17:** `test_purge_cannot_complete_with_residual_copy` (nakon purgea `grep` nad `.db` i WAL datotekama ne nalazi tekst).
  - **T18:** DST fold u Zagrebu okida točno jednom preko restarta; wake-up sweep pokreće propuštene podsjetnike uz `OVERDUE` oznaku.
  - **T19:** Potvrda za pojavu $N$ ne zatvara pojavu $N+1$; 10 uzastopnih identičnih polling izvršenja ne ruši stuck-breaker.
  - **T20:** `SIGKILL` matrica na svim točkama Telegram dostave konvergira u točno-jednom prijem i at-least-once dostavu; ne-tekstualne poruke vraćaju tipizirani odgovor bez rušenja.
  - **T21:** Odobrenje nakon restarta dovršava akciju točno jednom; izmjena argumenata poništava staro odobrenje.

---

### [RED] [OK] Nalaz 7 — Izravna veza s PRD §6 kriterijima "Gotovo"
* **Analiza:**  
  Svih 6 točaka iz `PRD.md §6` ima izravan dokaz u ledgeru:
  - §6.1 (Razgovor u terminalu) → T14, T15, T24
  - §6.2 (Pamćenje preživljava restart) → T16, T17, T24
  - §6.3 (Podsjetnik preživi gašenje i javi se na vrijeme) → T18, T19, T24
  - §6.4 (Telegram mobilni razgovor + HITL odobrenje) → T20, T21, T24
  - §6.5 (Posao/privatno nula curenja) → T16, T24
  - §6.6 (Pješčanik blokira `/etc/shadow` i mrežu) → T02, T22, T23, T24

---

### [SIZE] [OK] Nalaz 8 — Optimalna granularnost i samostalnost zadataka
* **Analiza:**  
  24 zadatka pružaju idealan balans:
  - Niti jedan zadatak nije prevelik (svaki pokriva jedan paket ili koherentnu značajku, cca 100–300 linija koda + testovi).
  - Niti jedan zadatak nije besmisleno sitan; čak i ugovorni zadaci (T04, T12) definiraju ključne granice tipova i orakule koji sprječavaju naknadni drift.

---

## 3. ZAKLJUČAK I PREPORUKE

`tasks-P0.md` je izvanredno strukturiran, tehnički besprijekoran i normativno usklađen task ledger.  
Jedina uočena sitnica je formalno uvrštavanje `s5-min (AtomicWriter)` u opis T08 (ili T17) kako bi pratio eksplicitno navođenje `s7-min` u T12.

Budući da su svi ugovori, ovisnosti, sigurnosna vrata i RED testovi u potpunosti ispravni, plan je **spreman za izvršenje**.

---

VERDICT: PASS

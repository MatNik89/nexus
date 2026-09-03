# REVIEW-TASKS3-agy — Task Ledger Verification Round 3 (`tasks-P0.md`)

**Meta:** Final targeted verification of the 6 residual fixes in `tasks-P0.md` and `ARCHITECTURE-ESSENTIALS.md`  
**Recenzent:** Antigravity (Adversarial Task Ledger Final Targeted Verification)  
**Authority & Sources:** `docs/HARDQ-CONSOLIDATED.md`, `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARNESS-SPEC.md` Annex A, `docs/PRD.md`.

---

## 1. PREGLED 6 CILJANIH REZIDUALNIH POPRAVAKA

---

### [OK] 1. `MEMORY_FORGET` i `DATA_PURGE` odgođeni u Annex P2.1
* **Lokacija:** `tasks-P0.md:197-208` (T19), `tasks-P0.md:306-308` (Deferred ledger), `ARCHITECTURE-ESSENTIALS.md:178-181` (E14)
* **Nalaz:** 
  - Budući da niti `MEMORY_FORGET` niti `DATA_PURGE` nisu kriteriji iz `PRD.md §6`, oba su u cijelosti odgođena u svoj normativni ugovor `Annex P2.1`.
  - Za potrebe P0 korekcija činjenica koristi se isključivo *append-only supersession* ("zapravo je Y" nadomješta staru činjenicu).
  - T19 i E14 točno nose ovu definiciju i usklađene RED testove.

---

### [OK] 2. Sigurnosno izolirane (hazard-safe) negativne kontrole u T02
* **Lokacija:** `tasks-P0.md:34-39` (T02)
* **Nalaz:**
  - RED testovi za namjerno labavljenje pravila u T02 definirani su na siguran način (*HAZARD-SAFE*):
    - FS kontrola čita ne-tajnu CANARY datoteku zasađenu u testnom direktoriju (nikada stvarni rekurzivni `/etc`).
    - Mrežna kontrola gađa isključivo lokalni testni sink (nikada nekontrolirani vanjski egress).
    - Procesna kontrola bježi u testno stablo procesa.
  - Svaki olabavljeni profil mora oboriti svoju specifičnu provjeru granice bez ugrožavanja stvarnog sustava.

---

### [OK] 3. Task-lokalni acceptance i conformance RED testovi za sva 4 šava u T05
* **Lokacija:** `tasks-P0.md:64-76` (T05)
* **Nalaz:**
  - Acceptance u T05 je 100% lokalan: zahtijeva prolazak testova usklađenosti za sva 4 šava (provjere uvoza downstream potrošača prepuštene su kasnijim zadacima).
  - RED testovi eksplicitno pokrivaju:
    1. `AtomicWriter` (`test_atomic_write_no_partial` uz SIGKILL).
    2. `ContextBudget` (odbijanje prekoračenja hard-limita).
    3. Injektibilni sat/ID (determinizam identičnih vremenskih linija kroz dva izvođenja).
    4. `Assembler.Base` (determinističko slaganje blokova prema zlatnom izlazu).

---

### [OK] 4. Generički testni sklop u T07 s lažnim redcima; konkretni recepti prebačeni u T20/T22
* **Lokacija:** `tasks-P0.md:85-96` (T07), `T20`, `T22`
* **Nalaz:**
  - T07 pruža generički harness za sinkrono foldanje u istoj transakciji, provjeren uz ugovorno valjane *fake* retke (nema ovisnosti o nepostojećim potrošačima).
  - Tri konkretna recepta smještena su kod svojih stvarnih prvih potrošača gdje se nalaze i njihovi crash/visibility RED testovi: `inbox-admission+journal` (T22), `occurrence+run-admission` (T20), `terminal-result+outbox` (T22).

---

### [OK] 5+6. T27 verificira konačni živi capability snapshot uz stale/removed-probe RED
* **Lokacija:** `tasks-P0.md:289-303` (T27)
* **Nalaz:**
  - T27 provjerava da konačni zapečaćeni snapshot sadrži **žive atestacije** probeova (sandbox, provider, channel), čime se fakes iz T11 zamjenjuju stvarnim stanjem.
  - RED set u T27 eksplicitno sadrži: uklanjanje bilo kojeg živog probea ili nepodudarnost hasha (*stale-hashed*) postavlja tu sposobnost u `OFF` u snapshotu i zabranjuje dispatch prema njoj.

---

## 2. PROVJERA NOVIH POGREŠAKA

- Nema novih grešaka, tipfelera, niti logičkih praznina.
- Redoslijed od 27 zadataka kroz 8 faza je topološki čist, linearan i oslobođen cirkularnosti.
- *Deferred ledger* je u potpunom skladu s implementacijskim opsegom P0.

---

## 3. ZAKLJUČAK

Svih 6 rezidualnih točaka iz druge runde besprijekorno je riješeno u `tasks-P0.md` i `ARCHITECTURE-ESSENTIALS.md`. P0 task ledger je **u potpunosti spreman za početak faze implementacije**.

---

VERDICT: PASS

# REVIEW-CONSTITUTION-agy — Review of Repository Constitution Files

**Meta:** Review of `/home/matej/HARNESS/nexus/CLAUDE.md` and `/home/matej/HARNESS/nexus/AGENTS.md` (Commit HEAD)  
**Recenzent:** Antigravity (Adversarial Constitution Review)  
**Authority & Sources:** `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARDQ-CONSOLIDATED.md`, `docs/HARNESS-SPEC.md` Annex A, `docs/PRD.md`, `docs/DESIGN-FIXES-r2.md`, `docs/DESIGN-S0-sandbox-codex.md`.

---

## 1. PREGLED I METODOLOGIJA

Ustavne datoteke repozitorija (`CLAUDE.md` za Claude Code sesije i `AGENTS.md` za radne/recenzentske agente) provjerene su kroz 4 osi:
1. **FIDELITY:** Usklađenost svih pravila sa zaključanom arhitekturom (`ARCHITECTURE-ESSENTIALS.md`, `HARDQ-CONSOLIDATED.md`, `HARNESS-SPEC.md` Annex A, `PRD.md`).
2. **MISSING:** Nedostaje li ključno pravilo koje bi dobronamjernog agenta moglo navesti na kršenje zaključane odluke?
3. **CONTRADICTION:** Postoje li interna proturječja, razilaženja između dviju datoteka ili neslaganja s izvorima?
4. **OVERREACH:** Postoje li pravila koja neopravdano sputavaju rad izvan onoga što izvori nalažu?

---

## 2. NALAZI PO OSIMA

---

### [FIDELITY] [OK] Nalaz 1 — Potpuna usklađenost identiteta, opsega i 8 temeljnih stupova
* **Lokacija:** `CLAUDE.md:3-7, 23-42` i `AGENTS.md:3-5, 17-28`
* **Analiza:**
  - Identitet ("asistent prvo, programiranje najjača P1 grana") i P0 opseg (6 sposobnosti iz PRD §4 uz PRD §6 acceptance kriterije) preneseni su dosljedno.
  - Svih 8 tvrdih pravila (*Hard rules*) vjerno preslikava zaključane ugovore:
    1. **Owner invarijante:** S6.0 PEP (default-deny, ASK≠ALLOW); S6.9 lifecycle (isključivo redoslijed); S7 (jedini retry/cancel owner uz `AttemptGrant`); S1.2 (identitet procesa); P0.3 (`EventJournal` jedini serijalizirani append autor).
    2. **Fail-closed:** Odbijanje nepoznatih enuma, tipova, shema i `ExecutionKind` (nikad pretpostavka, nikad tihi inproc fallback).
    3. **Sandbox:** Svaki subprocess ide kroz S6.2 `SandboxBackend` (`bwrap` u P0, native helper u P1); zabrana oslanjanja na sandbox prije prolaska neprijateljskog conformance suitea.
    4. **Trust & Secrets:** Monotoni trust/lineage (`MAX/union`); untrusted podatak nikad ne postaje instrukcija; redakcija tajni prije journala.
    5. **Profili:** Ne-nul `ProfileID` zapečaćen pri prijemu; jedna SQLite baza po profilu; povezivanje kanala prije prijema.
    6. **Tipovi:** Zabrana `map[string]any` u kernelu; opaque payload = `json.RawMessage` + zatvorena validacija.
    7. **Autonomija:** Zabrana autonomnog samomijenjanja koda (RepairBundle → opt-in → git fix → upgrade).
    8. **Dokazi:** Završenost ocjenjuje checker nad artefaktima (checker ≠ worker); `MarkDone` samo uz Evidence; zabrana slabljenja testova.

---

### [FIDELITY] [OK] Nalaz 2 — Sinkronizirana hijerarhija prvenstva izvora (Source Precedence)
* **Lokacija:** `CLAUDE.md:17-21` i `AGENTS.md:12-15`
* **Analiza:**
  - Redoslijed u oba dokumenta glasi: `PRD.md > user-approved HARDQ-CONSOLIDATED.md > HARNESS-SPEC Annex A contracts (zanemariti stale salvage u tijelu) > DESIGN-FIXES-r2 / DESIGN-*.md > DESIGN-STATUS > HARNESS-PLAN.md > SECTION-MAP / PLAN-HOLES`.
  - Eksplicitno je naloženo poštovanje inline change-recorda u matičnim dokumentima.
  - Nema nikakve diskrepancije među datotekama.

---

### [FIDELITY] [OK] Nalaz 3 — Go build i jezična pravila
* **Lokacija:** `CLAUDE.md:8-10, 55-59` i `AGENTS.md:6-7`
* **Analiza:**
  - `CGO_ENABLED=0`, per-OS kod isključivo kroz build-tagove, `modernc.org/sqlite` (WAL, bounded `busy_timeout`), `bwrap` kao deklarirani preduvjet koji provjerava `nexus doctor`.
  - Jezik repozitorija: **Engleski** za sav kod, komentare, commit poruke, dokumentaciju i među-agentske prompte (hrvatski samo u izravnoj interakciji s korisnikom).

---

### [CONTRADICTION] [OK] Nalaz 4 — Usklađenost publike (CLAUDE.md vs AGENTS.md)
* **Lokacija:** Cijeli opseg oba dokumenta
* **Analiza:**
  - Preklapanje u pravilima je namjerno i potpuno usklađeno.
  - Razlike su isključivo u specifičnostima uloga:
    - `CLAUDE.md` sadrži SDD metodologiju po sliceovima, instrukcije za grananje na Gitu i protokol orkestracije triju herdr agenata (`codex`, `kilo`, `agy`).
    - `AGENTS.md` sadrži precizne upute za stil rada radnih agenata (TDD: RED pa GREEN, najmanji uzročni diff, apsolutne staze u uputama, tagiranje nalaza u adversarijalnim recenzijama: `[BREAK|EDGE|OVERENG|FIDELITY|MISSING|UNFOLDED|NEW-ERROR|OK]`).
  - Nema internih kontradikcija ni kolizija sa zaključanim izvorima.

---

### [MISSING] [OK / Minor Note] Nalaz 5 — Izolacija stanja u testovima (Fresh temp fixtures)
* **Lokacija:** `CLAUDE.md:47` ("Stateful tests own fresh temp fixtures") naspram `AGENTS.md:29-33`
* **Analiza:**
  - `CLAUDE.md` izričito nalaže da testovi sa stanjem posjeduju vlastite svježe privremene fixture (kako se SQLite baze ili datoteke ne bi dijelile među testovima).
  - U `AGENTS.md` pod "Working style" pravilo o TDD-u i RED/GREEN testiranju je prisutno, a reference na ESSENTIALS pokrivaju `AtomicWriter` i testne ugovore. Nema materijalnog nedostatka koji bi omogućio zaobilaženje pravila.

---

### [OVERREACH] [OK] Nalaz 6 — Provjera prekomjernih ograničenja
* **Analiza:**
  - Sva pravila u `CLAUDE.md` i `AGENTS.md` izravno proizlaze iz potvrđenog PRD-a, 15 zaključanih odluka iz `ARCHITECTURE-ESSENTIALS.md`, ugovora iz Annexa A i rezolucija iz `HARDQ-CONSOLIDATED.md`.
  - Nema izmišljenih pravila niti nametanja proizvoljnih apstrakcija.

---

## 3. ZAKLJUČAK

Dokumenti [`CLAUDE.md`](file:///home/matej/HARNESS/nexus/CLAUDE.md) i [`AGENTS.md`](file:///home/matej/HARNESS/nexus/AGENTS.md) predstavljaju kristalno jasnu, normativno vjernu i međusobno neproturječnu ustavnu osnovu za sav budući rad u repozitoriju.

---

VERDICT: PASS

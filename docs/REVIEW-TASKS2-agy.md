# REVIEW-TASKS2-agy — Task Ledger Verification Round 2 (`tasks-P0.md` v2)

**Meta:** Verification of rewritten P0 Task Ledger (v2, 27 tasks) at Commit HEAD  
**Recenzent:** Antigravity (Adversarial Task Ledger Verification Round 2)  
**Authority & Sources:** `docs/HARDQ-CONSOLIDATED.md`, `docs/ARCHITECTURE-ESSENTIALS.md`, `docs/HARNESS-SPEC.md` Annex A, `docs/PRD.md`, `docs/REVIEW-TASKS-{codex,kilo,agy}.md`.

---

## 1. PREGLED I METODOLOGIJA

Nakon 1. runde recenzija, `docs/tasks-P0.md` je u cijelosti prerađen u v2 strukturu s **27 zadataka** (T01–T27) kroz 8 faza, a `ARCHITECTURE-ESSENTIALS.md` E14 je preciziran glede razgraničenja `MEMORY_FORGET` (P0) i `DATA_PURGE` (P2.1).

Verifikacija je obuhvatila 3 ključne točke:
1. **Ugrađivanje svih materijalnih nalaza iz 1. runde** (svih 15 Codexovih, 8 Kilovih i nalaza iz Agy recenzije).
2. **Provjera novih defekata** (redoslijed nakon renumeriranja, očuvanje svih usvojenih HARDQ stavki, red-capabilnost i zadovoljivost RED testova unutar vlastitog zadatka).
3. **Konzistentnost Deferred ledgera** s opsegom zadataka.

---

## 2. AUDIT UGRAĐIVANJA NALAZA IZ 1. RUNDE (16/16 SKUPINA NALAZA)

---

### [OK] 1. `s5-min` (AtomicWriter) implementacijski zadatak
* **Rješenje:** Uvršteno u **T05** (`tasks-P0.md:62-71`): `AtomicWriter tmp→fsync→rename, never in-place (the s5-min cut)` uz normativni RED test `test_atomic_write_no_partial` (SIGKILL mid-write → staro ili novo, nikad pola).

---

### [OK] 2. HARDQ C8 pozitivni signali zdravlja (heartbeat + brojač okidanja)
* **Rješenje:** 
  - **T17** (`tasks-P0.md:170-178`): `nexus daemon` emitira liveness heartbeat; RED provjerava detekciju prestanka kucanja unutar intervala.
  - **T20** (`tasks-P0.md:203-212`): Scheduler održava `last_occurrence_fired` brojač dostupan kroz doctor.
  - *Deferred ledger* precizira da je samo evaluacijski mehanizam SemanticHealth odgođen u P2+.

---

### [OK] 3. HARDQ A1 atestacija integriteta ugrađenih kanala release-potpisom
* **Rješenje:** Uvršteno u **T27** (`tasks-P0.md:282-293`): potpisivanje binarija i provjera integriteta `channel:builtin` adaptera putem potpisa izdanja uz RED: `tampered binary → signature verification fails`.

---

### [OK] 4. Uklanjanje krijumčarenja `DATA_PURGE` ugovora iz P0
* **Rješenje:** 
  - **T19** (`tasks-P0.md:190-200`): Ograničen na `MEMORY_FORGET` (reverzibilni tombstone). `DATA_PURGE` je eksplicitno odgođen u P2.1.
  - `ARCHITECTURE-ESSENTIALS.md:179-181` (E14) nosi usklađenu anotaciju.

---

### [OK] 5. Korekcija preuranjene `P0-capable` oznake u T03
* **Rješenje:** U **T03** (`tasks-P0.md:39-50`): doctor preflight izdaje isključivo status **`prerequisites-ready`**. Oznaka `P0-capable` dodjeljuje se tek u **T27** nakon uspješne verifikacije svih 6 živih PRD kriterija.

---

### [OK] 6. K0 šavovi (clock/ID injekcija, S8.1-min, S11.1-min)
* **Rješenje:** Uvršteno u **T05** (`tasks-P0.md:62-71`): definira injektibilni sat/ID generator, `ContextBudget.Measure+HardLimit` (S8.1-min) i `Assembler.Base` (S11.1-min).

---

### [OK] 7. T11 (ex-T09) ne ovisi o budućim probeovima
* **Rješenje:** U **T11** (`tasks-P0.md:111-120`): snapshot u Fazi 1 koristi stvarni T02 sandbox probe + ugovorno valjane *fake* probeove za provider i kanal. Živi probeovi registriraju se u **T15** i **T23**, a **T27** verificira konačni zapečaćeni snapshot.

---

### [OK] 8. Razrješenje T13/T14 inverzije i uklanjanje stub-skipa
* **Rješenje:** 
  - **T13** gradi `s7-min` (`AttemptGrant` + cancel + no-retry) *prije* `EffectPatha`.
  - **T14** gradi `EffectPath` s lažnim sandboxom i odmah zadovoljava svojih 7 ugovornih RED-ova.
  - Integracijski test stvarnog pješčanika (`TestReadOnlyProcessStillSandboxed`) prebačen je pod vlasništvo zadatka **T26** gdje se spaja stvarni backend. Nema nerješivih ili preskočenih RED-ova.

---

### [OK] 9. HARDQ B3 dokaz izolacije kroz cijeli kauzalni lanac
* **Rješenje:** Uvršteno u **T24** (`tasks-P0.md:248-259`): RED provjerava da pojava, isporuka ili odobrenje kreirano u `work` profilu *nikada* ne može biti viđeno, isporučeno ili odobreno preko chata vezanog uz `private` profil (i obrnuto), uključujući restart i replay.

---

### [OK] 10. Negativne kontrole za sigurnosna vrata Faze 0 (T02 i T03)
* **Rješenje:**
  - **T02** (`tasks-P0.md:34-37`): RED uključuje namjerno labavljenje pravila (rekurzivni `/etc`, uključen net, bez PID namespacea) koje *mora* pasti na svakoj granici.
  - **T03** (`tasks-P0.md:47-50`): Tablični RED koji pojedinačno lomi svaki od 5 preduvjeta i provjerava točno izolirani OFF ishod.

---

### [OK] 11. Zabrana tolerantnog parsera za sigurnosne i policy payloade
* **Rješenje:** U **T15** (`tasks-P0.md:150-159`): tolerancija je strogo ograničena na ne-sigurnosne payloade; RED `malformed/unknown EFFECT or POLICY discriminator → reaches no sink` sprječava tiho krpanje diskriminatora.

---

### [OK] 12. Deterministički orakul za konverzaciju u T17
* **Rješenje:** U **T17** (`tasks-P0.md:170-178`): RED uključuje skriptirani end-to-end orakul s determinističkim fake providerom (unos → očekivani renderirani ispis kroz journal) uz odvojeni live-provider smoke test.

---

### [OK] 13. Konkretni tipizirani Task handler i pozitivni stuck-breaker test
* **Rješenje:**
  - **T21** (`tasks-P0.md:213-225`): Implementira konkretni P0 Task handler **`file_note`** (dodavanje retka u bilješke profila preko `AtomicWriter`; verifikator provjerava hash datoteke; prazan registar je nemoguć).
  - **T16** (`tasks-P0.md:160-169`): Sadrži pozitivan test okidanja stuck-breakera za ponavljanje unutar istog turna uz provjeru imuniteta za polling politiku.

---

### [OK] 14. Senzitivnost ablacije za svih 6 PRD §6 kriterija u T27
* **Rješenje:** U **T27** (`tasks-P0.md:282-293`): Verifikacijski testni sklop živi izvan implementacije; za *svaki* od 6 kriterija kontrolirani feature-off prekidač ili ciljana mutacija obara točno taj kriterij.

---

### [OK] 15. Podjela prevelikih zadataka (T06+T07 i T22+T23)
* **Rješenje:**
  - Bivši T05 podijeljen je na **T06** (EventJournal trajnost i serijalizacija) i **T07** (Sinkrone projekcije i transakcijski recepti).
  - Bivši T20 podijeljen je na **T22** (Trajna transport-neutralna dostava B2) i **T23** (Telegram ugrađeni adapter).

---

### [OK] 16. Ispravak konvencije citiranja PRD-a
* **Rješenje:** U zaglavlju (linija 9) i kroz cijeli dokument uveden je kanonski oblik `"PRD §6 item N"`.

---

## 3. PROVJERA REDOSLIJEDA I ODSUTNOSTI NOVIH DEFEKATA

1. **Topološki redoslijed (27 zadataka):**  
   - `Phase 0` (T01–T03: Scaffold, Sandbox Probe, Doctor)
   - `Phase 1` (T04–T12: Ugovori, Seamovi, Journal, Projekcije, State machine, Config, Process ID, Snapshot, Checker)
   - `Phase 2` (T13–T17: s7-min, PEP/EffectPath, Provider, Loop, REPL)
   - `Phase 3` (T18–T19: Profili, Eksplicitna memorija + FORGET)
   - `Phase 4` (T20–T21: Scheduler + brojač, ObligationStore + file_note)
   - `Phase 5` (T22–T24: Durable channel core, Telegram adapter, Remote HITL)
   - `Phase 6` (T25–T26: SandboxBackend bwrap, Exec tool na EffectPathu)
   - `Phase 7` (T27: P0 acceptance run, release potpis, P0-capable certifikacija)
   - **Nema inverzija, nema cirkularnosti, T02 vrata strogo kontroliraju Phase 6.**

2. **Očuvanje HARDQ stavki:** Sve usvojene stavke (`A1, A2, B1–B9, C1–C5, C7, C8, D1, F1`) prisutne su u zadacima.
3. **Kvaliteta RED testova:** Svaki od 27 zadataka ima samostalan, red-capable test koji je zadovoljiv unutar vlastitog zadatka.
4. **Deferred ledger:** U potpunosti konzistentan s P0 opsegom (točno navodi sve komponente koje čekaju P1–P4).

---

## 4. ZAKLJUČAK

`tasks-P0.md` v2 (27 zadataka) predstavlja **vrhunski inženjerski task ledger** koji je u potpunosti integrirao sve primjedbe iz 1. runde recenzija, uklonio sve logičke i ovisnosne nedostatke te pruža savršenu osnovu za početak TDD implementacije.

---

VERDICT: PASS

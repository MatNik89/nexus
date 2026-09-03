# REVIEW-FOLD-agy — Hard-Questions Fold Verification (Commit 18aabde)

**Meta:** Verification of folded hard-questions resolutions into documentation  
**Authority:** `/home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md` (Sections A1/A2, B1–B9, C1–C8, D1 user-decision, F1 doctor preflight)  
**Changed Target Files:**
- `/home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md` (E1, E2, E6, E7, E10, E14, E15, P0 additions, Open list)
- `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (P1.6 AMENDMENT block at line 1467)
- `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md` (P0 -min cuts note at lines 62–68)

---

## 1. VERIFIKACIJA PO STAVKAMA IZ `HARDQ-CONSOLIDATED.md`

---

### A. Otvorena pitanja Q1 / Q2

* **[OK] A1. Q1 — Razrješenje Telegram P0 vs `channels requires extensions` (`HARNESS-SPEC.md:1467`, `ARCHITECTURE-ESSENTIALS.md:191-195`):**  
  - Ugovor `P1.6` u `HARNESS-SPEC.md` dopunjen je normativnim `AMENDMENT` blokom koji dijeli kanale na `channel:builtin` (P0 Telegram, stdio; zahtijeva samo 7.3 outbox + approval-core + identity; atestiran potpisom release artefakta P1.5) i `channel:plugin` (dinamički adapteri koji zahtijevaju `extensions + S6.8 + S11.5`).
  - `test_channels_contract_fails_closed` slučaj A ostaje neizmijenjen i testira plugin put.
  - Odluka `E15` i Open lista u `ARCHITECTURE-ESSENTIALS.md` točno bilježe zatvaranje Q1.

* **[OK] A2. Q2 — Obvezujući P0 redoslijed gradnje (`SECTION-MAP.md:62-68`, `ARCHITECTURE-ESSENTIALS.md:78-84`):**  
  - Usklađeno načelo: `HARNESS-SPEC` vertikalni sliceovi vežu isporuku, dok se `SECTION-MAP` DAG rubovi za K1 rano zadovoljavaju imenovanim `-min` ugovorima (`s7-min` = AttemptGrant + cancel + no-retry; `s5-min` = AtomicWriter; `3.6-min` = persisted scheduler; `7.3-min` = inbox/outbox).
  - Puni pogoni (S7 CAS fencing, S5 git worktree) ostaju u kasnijim fazama.
  - Detaljan redoslijed walking skeletona (7 koraka) ispravno je delegiran u `tasks-P0.md`.

---

### B. Konvergentni P0-BLOCKERi (B1–B9)

* **[OK] B1. Lokalni trajni raspoređivač u P0 (`ARCHITECTURE-ESSENTIALS.md:80-82, 219-220`, `SECTION-MAP.md:65`):**  
  - Uvučen `3.6-min` u P0: trajno sljedeće okidanje, `missed_run=COALESCE`, stabilni ID pojave, te startup i wake catch-up pregled (`EvaluateDue` range upit `DueAt <= now` s eksplicitnom OVERDUE obavijesti).

* **[OK] B2. Telegram inbound inbox + outbound outbox i poštena granica isporuke (`ARCHITECTURE-ESSENTIALS.md:180-186`):**  
  - Definiran transakcijski inbox `(adapter_id, channel_identity, update_id)` sa stanjima `RECEIVED → ADMITTED → TERMINAL` (normalizirana poruka perzistirana PRIJE pomaka remote offseta).
  - Outbox pruža točno-jednom prijem (admission) i barem-jednom (at-least-once) remote dostavu (`sent-but-unrecorded → UNKNOWN → RECONCILING`). Nema lažnih tvrdnji o exactly-once dostavi preko vanjskog Telegram API-ja.

* **[OK] B3. Fizička izolacija profila i nepromjenjivi ProfileID (`ARCHITECTURE-ESSENTIALS.md:170-176`):**  
  - Provedena fizička separacija: jedna SQLite baza po profilu (`~/.nexus/profiles/<p>/nexus.db`) + zasebna sistemska baza bez korisničkih podataka.
  - `ProfileID` se pečati pri prijemu i nepromjenjivo prenosi kroz cijeli kauzalni lanac.

* **[OK] B4. Ograničeni P0 ugovor za naredbe i Landlock zatvaranje (`ARCHITECTURE-ESSENTIALS.md:121-125`):**  
  - P0 dopušta samo promovirane apsolutne ELF izvršne datoteke s razriješenim hash-vezanim closureom (`/usr`, `/lib*`, `/bin`, `/sbin` kao direktoriji; `/etc/ld.so.cache` + CA certifikati kao FILE grantovi — NIKAD rekurzivni `/etc`) + jednokratni RW radni direktorij. Skripte i nedeklarirani child procesi se odbijaju.

* **[OK] B5. Dva tipa asistent obveza: Reminder i Task (`ARCHITECTURE-ESSENTIALS.md:154-156, 220-221`):**  
  - U P0 definirani `Reminder` (dokaz = trajni delivery receipt + korisnički ack) i tipizirani `Task` handleri. Coding artefakt ocjenjivanje (git-diff/exit-code) ostaje vezano uz P1 coding profil.

* **[OK] B6. Trajni HITL suspend / resume preživljava restart (`ARCHITECTURE-ESSENTIALS.md:187-190, 221-222`):**  
  - Pri `DecisionAsk` TurnLoop bilježi `TurnSuspended` u journal i izlazi (nema in-memory blokade). Prispijeće odobrenja bilježi `ApprovalReceived` i rehidrira stanje iz journala.

* **[OK] B7. Disciplina jednog pisca i sinkrone projekcije (`ARCHITECTURE-ESSENTIALS.md:221-223`):**  
  - Jedan serijalizirani append actor za `EventJournal`. Jezgrene projekcije stanja (sesije, obveze, memorijski indeks) foldaju se SINKRONO u istoj SQLite transakciji. Uveden obrazac daemon-owns-DB / CLI-connects-via-UDS.

* **[OK] B8. P0 memorija: eksplicitne činjenice bez decay mehanizma (`ARCHITECTURE-ESSENTIALS.md:162-169, 224`):**  
  - P0 memorija ograničena na eksplicitne činjenice vezane uz profil (spremanje uz exact preview, deterministički recall, lossless bajtovi).
  - FSRS decay i Audn premješteni u P1 `memorija` flag (i nikada se ne primjenjuju na eksplicitne činjenice, $S=\infty$).

* **[OK] B9. Statički capability snapshot u P0; transakcijski Activator odgođen (`ARCHITECTURE-ESSENTIALS.md:92-97, 224-225`):**  
  - P0 koristi statičku fail-closed validaciju (`Resolver.Resolve`) i zapečaćeni startup snapshot. Dinamička `ActivationPlan`/`RollbackVault` mašinerija (E7) formalno je odgođena za P4+ (ekstenzije).

---

### C. Usvojeni popravci drugog reda (C1–C8)

* **[OK] C1–C8 integracija (`ARCHITECTURE-ESSENTIALS.md:190, 222-225`):**  
  - `C1`: Redakcija tajni u P0 na temelju poznatih referenci (`SecretRef`).
  - `C2`: Fail-closed odgovor na ne-tekstualne Telegram poruke.
  - `C4`: Kanonska polja `ExactIntentPayload` (ime toola + kanonski JSON args + resurs + ProfileID; bez prolaznih timestampova i repo hasheva).
  - `C5`: Objava minimalnog kernela/ABI-ja i arhiviranje probea na deployment hostu.
  - `C7/C8`: S0.4 upcast chain i SemanticHealth odgođeni iz P0.

---

### D. Korisnička odluka D1 & Novi zahtjev F1 (bwrap backend & doctor preflight)

* **[OK] D1. bwrap kao P0 sandbox backend (`ARCHITECTURE-ESSENTIALS.md:25-27, 115-131, 228-232`):**  
  - `bwrap` je usvojen kao P0 ENFORCED backend iza `SandboxBackend` sučelja.
  - **Provjera invarijante:** Odluka NIJE oslabila sigurnosni zahtjev — bwrap prolazi ISTI neprijateljski (hostile) conformance suite; suite je invarijanta, backend je zamjenjiv. Nema slabijeg fallbacka: ako bwrap nije instaliran, izvršavanje naredbi je potpuno ugašeno (`conversation-only`).
  - Izvorni Landlock+seccomp helper (`DESIGN-S0-sandbox-codex.md`) ostaje definiran kao P1 hardening put.

* **[OK] F1. Installer / doctor preflight (`ARCHITECTURE-ESSENTIALS.md:26, 127-128, 223-224`):**  
  - Uveden `nexus doctor` preflight koji provjerava bwrap i kernel floor te nudi privolu za instalaciju.

---

## 2. PROVJERA PROTURJEČJA I DISTORZIJA

1. **Hostile-suite invarijanta i fail-closed:** Potpuno očuvani u E10. Nema tihog downloada niti oslabljenog moda.
2. **Semantika isporuke (At-least-once vs Exactly-once):** E15 eksplicitno navodi: *"the local outbox gives exactly-once ADMISSION and at-least-once remote delivery"*. Nema povratka na nerealne exactly-once tvrdnje za Telegram.
3. **P1.6 RED test za plugine:** `HARNESS-SPEC.md:1467` izričito nalaže: *"RED slučaj A testira plugin put i ostaje na snazi"*.
4. **Status PRD odobrenja:** Usklađen u `PRD.md:66`, `DESIGN-STATUS.md:13` i `ARCHITECTURE-ESSENTIALS.md:29-33, 231` kao formalno odobren od strane korisnika 2026-09-03.
5. **Nema novih proturječja** s netaknutim izvorima.

---

## 3. ZAKLJUČAK

Sve usvojene stavke iz `HARDQ-CONSOLIDATED.md` su **vjerno, točno i bez distorzija** ugrađene u dokumentaciju (`ARCHITECTURE-ESSENTIALS.md`, `HARNESS-SPEC.md`, `SECTION-MAP.md`) ili ispravno delegirane u `tasks-P0.md`.

---

VERDICT: PASS

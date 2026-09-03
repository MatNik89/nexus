# REVIEW-FOLD2-agy — Hard-Questions Fold Verification Round 2 (Commit 14814cf)

**Meta:** Verification of deep owner-document amendments resulting from Hard-Questions resolutions  
**Authority:** `/home/matej/HARNESS/nexus/docs/HARDQ-CONSOLIDATED.md`  
**Inspected Target Files:**
- `/home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md`
- `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md`
- `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`
- `/home/matej/HARNESS/nexus/docs/DESIGN-STATUS.md`
- `/home/matej/HARNESS/nexus/docs/DESIGN-S0-sandbox-codex.md`

---

## 1. VERIFIKACIJA 12 CILJANIH POPRAVAKA

---

### [OK] 1. Precedence hijerarhija uključuje HARDQ-CONSOLIDATED odmah iza PRD-a
* **Lokacija:** `ARCHITECTURE-ESSENTIALS.md:8-12`
* **Nalaz:** Zaglavlje eksplicitno postavlja redoslijed: `PRD.md > user-approved HARDQ-CONSOLIDATED resolutions (A/B/C/D/F items, 2026-09-03) > HARNESS-SPEC Annex A contracts...` uz objašnjenje da rezolucije nadjačavaju specifične klauzule koje mijenjaju u Annexu A, DESIGN-S0, DESIGN-STATUS i SECTION-MAP.

---

### [OK] 2. HARNESS-SPEC P1.6 klauzule u potpunosti prerađene za `channel:builtin` vs `channel:plugin`
* **Lokacija:** `HARNESS-SPEC.md:1459-1467`
* **Nalaz:**
  - **Vlasnik (1461):** Eksplicitno deklarira dva puta: `channel:builtin` (zahtijeva samo `7.3 + approval-core (6.0/12.4) + identity (6.6)`, atestiran potpisom release-artefakta P1.5) i `channel:plugin` (zahtijeva `extensions + S6.8 + S11.5 + 7.3 + approval-core`).
  - **Manifest (1462):** `ChannelAdapterManifest` je strogo vezan za `channel:plugin`; built-in nosi deskriptor bez extensions-only polja.
  - **Invarijante (1465):** Built-in adapter se registrira statički i ne prolazi runtime extensions gate.
  - **RED (1466):** Slučaj A doslovno glasi: *"slučaj A registrira channel:plugin bez extensions/S6.8 attestationa → INCOMPLETE_CAPABILITY_CLOSURE"*; Slučaj B (replay tokena) zajednički je za oba puta.

---

### [OK] 3. P0.8 / P0.9 / P0.11 / P0.13 tijela ugovora i wiring tablica nose anotacije okidača aktivacije
* **Lokacija:** `HARNESS-SPEC.md:1355-1358` (Wiring) i `1555-1574` (Tijela ugovora)
* **Nalaz:**
  - `P0.8`: Anotiran s `Activation trigger (HARDQ C6): local-inference — NIJE P0 gate`.
  - `P0.9`: Anotiran s `Activation trigger (HARDQ C6): coding profil — NIJE P0 gate`.
  - `P0.11`: Anotiran s `Activation trigger (HARDQ C6): u P0 vrijedi SAMO AtomicWriter klauzula (P0 file-mutacije); shadow-checkpoint/rollback dio gates coding/user-workspace mutaciju`.
  - `P0.13`: Anotiran s `Activation trigger (HARDQ B8, 2026-09-03): P1 memorija decay — NIJE P0 gate. P0 memorija = eksplicitne činjenice bez decaya`.

---

### [OK] 4. P0.1 migracijska klauzula nosi P0 `reject-unknown` opseg
* **Lokacija:** `HARNESS-SPEC.md:1381`
* **Nalaz:** Dodana klauzula: *"P0-scope (HARDQ C7): u P0 je dovoljan schema_version + reject-unknown-neobrađeno (raw input sačuvan); upcast-LANAC + karantena-sink aktiviraju se s prvom stvarnom migracijom (v2) i tada ovaj ugovor vrijedi u cijelosti"*.

---

### [OK] 5. SECTION-MAP oba zapisa za Decay označena kao P1
* **Lokacija:** `SECTION-MAP.md:39` (Faza M) i `SECTION-MAP.md:110` (Status ledger)
* **Nalaz:**
  - Linija 39: `S9 (9.1 sesije/9.2 store/9.3 learn/9.4 Decay+Audn — Decay+Audn=P1 per HARDQ B8, P0=explicit facts; 9.5/9.6 SAMO persistence+namespace, binding→N/O)`.
  - Linija 110: `**S9** ✅ 9.2 store · 9.4 Decay+Audn(P1 — HARDQ B8 2026-09-03; P0=explicit facts, no decay)`.

---

### [OK] 6. DESIGN-STATUS #3 bilježi `bwrap-P0`
* **Lokacija:** `DESIGN-STATUS.md:10`
* **Nalaz:** Must-resolve #3 ažuriran: *"AŽURIRANO 2026-09-03 (HARDQ D1): bwrap = P0 ENFORCED backend (korisnikova odluka; isti hostile-suite invarijant, fail-closed, atestiran); native helper = P1 hardening put. Probe OPEN = hostile-conformance suite protiv bwrapa na stvarnom deployment hostu"*.

---

### [OK] 7. DESIGN-S0 nosi gornji change-record koji nadjačava native-first i Activator-kao-P0
* **Lokacija:** `DESIGN-S0-sandbox-codex.md:3-11`
* **Nalaz:** Uvršten istaknuti `CHANGE-RECORD (HARDQ, 2026-09-03)` koji definira: (1) bwrap kao P0 ENFORCED backend uz očuvanje hostile suitea i fail-closed pravila (native Landlock je P1 hardening); (2) Transakcijski Activator NIJE P0 build item (P0 donosi fail-closed Resolve validaciju i sealed startup snapshot per HARDQ B9).

---

### [OK] 8. Essentials E11 formulacija za `reject-unknown`
* **Lokacija:** `ARCHITECTURE-ESSENTIALS.md:140-142`
* **Nalaz:** E11 točno glasi: *"unknown schema ID/version → REJECT unprocessed in P0, raw input preserved (the upcast chain + QUARANTINE sink are the v2 migration layer's — HARDQ C7 — and never a lossy downcast or silent drop when they activate)"*.

---

### [OK] 9. Essentials E4 nosi B7 WAL / `busy_timeout` / tri `BEGIN IMMEDIATE` recepta
* **Lokacija:** `ARCHITECTURE-ESSENTIALS.md:60-64`
* **Nalaz:** U E4 dodano: *"Physical commit boundaries (HARDQ B7): WAL + bounded busy_timeout; one serialized append actor owns Append + sequence allocation; core-state projections fold in the SAME transaction; three named BEGIN IMMEDIATE recipes commit together: inbox-admission + journal event · occurrence + run admission · terminal result + outbox enqueue. Daemon owns the DB; CLI connects via UDS"*.

---

### [OK] 10. P0-additions nose C3 doseg za stuck-detection
* **Lokacija:** `ARCHITECTURE-ESSENTIALS.md:232-234`
* **Nalaz:** Uvršteno: *"stuck-detection scoped to WITHIN one interactive turn — scheduled/polling occurrences carry an explicit continuous-loop policy exempt from the identical-argument breaker (C3; RED in tasks-P0.md)"*.

---

### [OK] 11. P0-additions nose C8 pozitivne zdravstvene signale
* **Lokacija:** `ARCHITECTURE-ESSENTIALS.md:235`
* **Nalaz:** Uvršteno: *"P0 health = liveness heartbeat + last-occurrence-fired counter (C8)"*.

---

### [OK] 12. P0-additions nose puni F1 5-check capability-scoped doctor
* **Lokacija:** `ARCHITECTURE-ESSENTIALS.md:235-240`
* **Nalaz:** Uvršteno: *"doctor preflight (F1): checks kernel/ABI floor, bwrap, data-dir permissions, provider key, Telegram token; anything missing → consented install or exact instructions; results are capability-scoped (no key → conversation off; no token → Telegram off; no bwrap → exec off; unsafe data-dir → stateful startup blocked); P0-capable = all six PRD §6 criteria reachable, not sandbox alone"*.

---

## 2. PROVJERA PROTURJEČJA (NEW-ERROR AUDIT)

* **Nema novih proturječja:** Sve promjene u matičnim dokumentima (`HARNESS-SPEC.md`, `DESIGN-STATUS.md`, `DESIGN-S0-sandbox-codex.md`, `SECTION-MAP.md`) savršeno su sinkronizirane s `ARCHITECTURE-ESSENTIALS.md` i `HARDQ-CONSOLIDATED.md`.
* **Invarijante očuvane:** Sigurnosne granice (`bwrap` fail-closed i hostile conformance suite), semantika isporuke (at-least-once za vanjski Telegram API), te izolacija profila ostaju 100% neokrnjene.

---

## 3. ZAKLJUČAK

Svih 12 ciljanih popravaka iz 2. runde verifikacije ugrađeno je **izravno u matične dokumente (owner documents)** na dubok, potpun i tehnički precizan način.

---

VERDICT: PASS

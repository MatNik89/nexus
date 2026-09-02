# RED TEAM REVIEW 3 — Verifikacija Fixeva i Regresijski Audit (Runda 3)
**Dokument:** `docs/REVIEW3-agy.md`  
**Datum:** 2026-09-02  
**Autor:** Antigravity (agy) — Red Team Reviewer  
**Kontekst:** Greenfield Go (`CGO_ENABLED=0`), nula salvagea, 108 podsekcija (S0–S18.6).  
**Verificirani dokumenti:** `/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md`, `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md`, `/home/matej/HARNESS/nexus/docs/REVIEW2-CONSOLIDATED.md`.

---

## SAŽETAK VERIFIKACIJE (Executive Summary)

Većina kritičnih blokera i strukturnih rupa iz Runde 2 (DAG inverzije, identitet, 108 manifest, zaboravljeni 12.3/13.4/18.6, P0.5–P0.13 ugovori) **uspješno je riješena i dokumentirana**.

Međutim, strogi Red Team audit samih fixeva otkrio je **1 NOVI KRITIČNI BUG u izvršnom kodu (RUN-01 fix)** te **3 prateće dokumentacijske regresije**:
1. **[NOVI BUG / BLOKER] Pogrešan uvjet grananja u `EffectPath.RunTool` (`DESIGN-FIXES-r2.md:34`):** Uvjet `if call.Effect != contracts.EffectReadOnly && call.RequiresProcess` uzrokuje da se **read-only procesni alati** (`ls`, `cat /etc/shadow`, `git log`, read-only bash) šalju na `InProcessExecutor` umjesto na `SandboxedProcessExecutor`. Posljedica: ili rušenje (inproc ne vrti procese) ili **nesandboxirano izvršenje** read-only shell naredbi!
2. **[REGRESIJA] `HARNESS-SPEC.md` Wiring tablica nije ažurirana:** P0.5–P0.13 su dodani na kraj specifikacije (redak 1527), ali normativna Wiring tablica (redak 1333) i dalje sadrži samo 17 ugovora.
3. **[REGRESIJA] Zaostali tipfeler zbroja u `SECTION-MAP.md:112`:** Zbroj navodi `Zbroj (107): ... uklj. 7 novih` umjesto `108` i `8 novih`.
4. **[ZAOSTALI SALVAGE] .py reference u tekstu plana:** 5 zaostalih referenci na python module (`core/supplychain.py`, `core/memory.py`, `circuit.py`, `permissions.py`, `broker.py`) još uvijek stoji u `HARNESS-PLAN.md`.

---

## DIO A: VERIFIKACIJA PRETHODNIH NALAZA (Status po ID-u)

### [RUN-01] Effect-Path: Ugradnja S6.2 Sandoxa (K1)
* **Status:** `[DJELOMIČNO / BUG U FIXU]`
* **Dokaz u dokumentu:** `DESIGN-FIXES-r2.md:21-41` ispravno dodaje `sandbox sandbox.Backend` i `sbproc ToolExecutor` u `EffectPath`.
* **Zašto NIJE potpuno zatvoren (Kritični Bug u retku 34):**
  U kodu `RunTool` stoji:
  ```go
  exec := p.inproc
  if call.Effect != contracts.EffectReadOnly && call.RequiresProcess {
      exec = p.sbproc
  }
  ```
  Ako napadač ili korisnik pokrene read-only shell naredbu (npr. `bash -c "cat /etc/passwd"` ili `git diff`), `call.Effect` je `EffectReadOnly`, pa gornji `if` evaluira u **`false`**. Poziv se dodjeljuje `p.inproc` (`InProcessExecutor`), koji prema specifikaciji (redak 15) ne pokreće OS procese!
* **Nužan fix:** Ukloniti provjeru `call.Effect` iz uvjeta procesnog grananja:
  ```go
  exec := p.inproc
  if call.RequiresProcess {
      exec = p.sbproc // BILO KOJI OS proces MORA ići u S6.2 sandbox!
  }
  ```

---

### [RUN-02] Razdvajanje InProcess vs SandboxedProcess alata (K2)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `DESIGN-FIXES-r2.md:12-20` definira zajednički `ToolExecutor` interface te konkretne tipove `InProcessExecutor` (za edit, read, grep, archmap, memory) i `SandboxedProcessExecutor` (za bash, exec, python koji idu kroz `S6.2 SandboxBackend.Launch` s ugrađenim `S1.2 ProcessTracker`). In-process alati više ne lome pretpostavku procesnog stabla.

---

### [SEC-01] Broj podsekcija usklađen na 108 (D1)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:**
  - `HARNESS-PLAN.md:4,7,786` svugdje ažuriran na kanonski broj: **108 podsekcija** (100 baznih iz v0.9 + 8 iz addenduma).
  - `SECTION-MAP.md:83,87` ažuriran na kanonski broj: **108 podsekcija**.
  - Stvarni broj `**X.Y` definicija u `HARNESS-PLAN.md` je točno **108** (od 0.1 do 18.6).
  *(Napomena: Manji tipfeler zbroja na retku 112 `SECTION-MAP.md` zabilježen kao REG-02).*

---

### [SEC-02] Vraćanje izostavljenih podsekcija `12.3` i `13.4` (D3)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:**
  - `SECTION-MAP.md:104`: eksplicitno uvršten `🟡 12.3 routing(potrošač 2.6)`.
  - `SECTION-MAP.md:105`: eksplicitno uvršten `13.4 video(subprocess ffmpeg)`.
  - Svih 14 "inline" podsekcija dobilo je eksplicitne statusne ikone u retcima 92–110.

---

### [GATE-01] Formalizacija ugovora P0.5–P0.13 u Spec Annex A (G1)
* **Status:** `[ZATVOREN / UZ MINOR DOK-FIX]`
* **Dokaz u dokumentu:** `HARNESS-SPEC.md:1527-1566` formalno definira svih 7 prethodno fantomskih ugovora (`P0.5`, `P0.6`, `P0.7`, `P0.8`, `P0.9`, `P0.11`, `P0.13`) s jasnim `MUST` zahtjevima, vlasnicima i RED testovima (`test_capability_unknown_fails_closed`, `test_rollback_byte_identical`, `test_decay_never_destroys_bytes`, itd.).
  *(Napomena: Wiring tablica na retku 1333 mora se dopuniti s ovih 7 ugovora — vidi REG-01).*

---

### [ID-01] Usklađivanje identiteta: Asistent-prvo na vrhu plana (G2)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:**
  - `HARNESS-PLAN.md:42`: zamijenjen stari naslov s `## IDENTITET: NEXUS = OSOBNI AI ASISTENT s vrhunskom coding-jezgrom (JEDNA JEZGRA, DVA PROFILA)`.
  - `HARNESS-PLAN.md:60`: opis diferencijatora ažuriran s uskog `git-diff+exit` na generički `AcceptanceContract` + `EvidenceBundle` (coding, delivery-receipt, calendar, grounding).
  - `SECTION-MAP.md:31`: Faza L preimenovana iz "coding-jezgra" u neutralni `FAZA L (izvršni put, →K)`.

---

### [DAG-01] Poredak gradnje: S5 (Workspace) ispred S4 (Alati) (K4)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `SECTION-MAP.md:17-55` restrukturirao je DAG u faze K0, K1, L, M, N, O, P:
  - `S5 (5.1/5.2/5.3)` je u **FAZI K1**, prije `S4` u **FAZI L**. `S4.3 EditEngine` sada može legalno uvoziti `S5.1 ShadowGit` i `S5.3 AtomicWriter`.
  - `S8.1-min` (budget) i `S11.1-min` (assembly) izvučeni su u **FAZU K0**, čime `S3 CoreLoop` u Fazi L nema unatrag-ovisnosti prema Fazi M.
  - `S16.6-det` je u **FAZI K1**, a multi-agent Council integracija u **FAZI N**.
  - `S14.5` kanali razdvojeni na stdio (Faza O) i plugin-channels (Faza P).

---

### [K3] Fleet ↔ Pairing cirkularni Go import
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `DESIGN-FIXES-r2.md:58-67` uvodi `internal/fleet/ids` (sadrži `NodeID`, `DeviceID`, `PlacementGrant`) kao leaf paket bez ovisnosti. Paketi `internal/fleet` i `internal/fleet/pairing` uvoze samo `ids`, eliminirajući cross-import ciklus.

---

### [D2] Ispravak vlasništva Journala (P0.3 vs S0.3)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `SECTION-MAP.md:20,59` ispravlja vlasništvo: `EventJournal paket (P0.3 write-owner — NIJE S0.3; S0.3=capability-negotiation)`.

---

### [D4] Klasifikacija 8 addendum podsekcija
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `SECTION-MAP.md:89-110` klasificira nove addendum komponente kao `✅🟠 = DIZAJN-GOTOV (GAPFIX) + scope-flag ODLUKA-PRD`.

---

### [D6] Vraćanje podsekcije `18.6 Device Pairing`
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `HARNESS-PLAN.md:776` i `SECTION-MAP.md:83,110` sadrže odvojeni `18.6 pairing` (OpenClaw-O11), čime je očuvan puni dizajn iz `GAPFIX-codex`.

---

### [VLASNIŠTVO] ObligationStore, AccountFleet, Policy/Lifecycle, SemanticHealth
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:** `DESIGN-FIXES-r2.md:48-57`:
  - `9.6 ObligationStore` piše kroz `EventJournal.Append` (P0.3), ne izravno u SQLite.
  - `AccountFleet (2.2)` samo klasificira, `S7` je jedini retry owner.
  - `S6.0` je jedini Decision Point, `S6.9` je samo Middleware redoslijed.
  - `15.5 SemanticHealth` je jedini producent swallow-signala.
  - `4.8 password-DENY` zamijenjen izvedivim testom `TestKeystrokeIntoUnknownFieldRequiresApproval`.

---

## DIO B: NOVI NALAZI I REGRESIJE (Runda 3)

### [REG-01 / BLOKER] Sigurnosna rupa u grananju `EffectPath.RunTool`
* **Dokument:** `DESIGN-FIXES-r2.md:34`
* **Ozbiljnost:** **KRITIČNA (Security / Execution Blocker)**
* **Opis:** 
  U kodu:
  ```go
  exec := p.inproc
  if call.Effect != contracts.EffectReadOnly && call.RequiresProcess {
      exec = p.sbproc
  }
  ```
  Ako se pozove alat koji pokreće OS proces, ali je označen kao `EffectReadOnly` (npr. `exec(argv=["cat", "/etc/shadow"])`, `exec(argv=["curl", "http://metadata"])`, `git log`, inspekcijske skripte), on **zaobilazi `sbproc` i šalje se na `inproc`**.
  Budući da `InProcessExecutor` ne pokreće procese, ovaj poziv ili puca (runtime error) ili — ako netko doda fallback — pokreće subprocess **IZVAN SANDBOXA**.
* **Popravak:** Uvjet za `sbproc` mora ovisiti ISKLJUČIVO o tome treba li operaciji OS proces:
  ```go
  exec := p.inproc
  if call.RequiresProcess {
      exec = p.sbproc
  }
  ```

---

### [REG-02] `HARNESS-SPEC.md` Wiring tablica nije sinkronizirana s dopunom
* **Dokument:** `HARNESS-SPEC.md:1333-1352`
* **Ozbiljnost:** Srednja (Doc-Integrity)
* **Opis:** Ugovor P0.5–P0.13 formalizirani su na dnu dokumenta (redak 1527), ali tablica `Wiring: ugovor → matična podsekcija` na vrhu Annexa A (redak 1333) i dalje prikazuje samo starih 17 ugovora.
* **Popravak:** Dodati 7 redaka u Wiring tablicu (P0.5 → S0.3, P0.6 → S1.1/1.2, P0.7 → S2.3, P0.8 → S2.4, P0.9 → S3.5, P0.11 → S5.1/5.3, P0.13 → S9.4).

---

### [REG-03] Tipfeler u sumarnom retku `SECTION-MAP.md:112`
* **Dokument:** `SECTION-MAP.md:112`
* **Ozbiljnost:** Niska (Kozmetika)
* **Opis:** Tekst glasi `**Zbroj (107):** ✅ ~24 dizajn-gotovih (uklj. 7 novih GAPFIX)...`, dok je u zaglavlju i u tekstu točan broj 108 i 8 novih GAPFIX podsekcija.
* **Popravak:** Ispraviti u `**Zbroj (108):** ... (uklj. 8 novih GAPFIX)`.

---

### [REG-04] Zaostala .py nomenklatura u `HARNESS-PLAN.md` (D5 nesavršenost)
* **Dokument:** `HARNESS-PLAN.md:258, 366, 382, 397, 492`
* **Ozbiljnost:** Niska (Čistoća)
* **Opis:** U tekstu plana ostalo je 5 referenci na Python nazive iz ranijih skica: `circuit.py` (258), `permissions.py` (366), `broker.py` (382), `core/supplychain.py` (397), `core/memory.py` (492).
* **Popravak:** Zamijeniti ih kanonskim Go paketima (`internal/circuit`, `internal/security/perm`, `internal/security/secrets`, `internal/security/supplychain`, `internal/memory`).

---

## TABLICA PROVJERE SVIH NALAZA (Scorecard)

| ID nalaza | Opis | Status | Napomena |
|---|---|---|---|
| **RUN-01** | Sandbox u EffectPathu | **DJELOMIČNO** | Sandbox u structu, ali `if EffectReadOnly` blokira sandboxing read-only procesa! (REG-01) |
| **RUN-02** | InProcess vs Sandboxed split | **ZATVOREN** | `ToolExecutor` razdvaja in-process Go alate od sandbox procesa |
| **SEC-01** | Kanonski broj 108 svugdje | **ZATVOREN** | 108 u planu, specu i mapi (uz kozmetički fix u retku 112) |
| **SEC-02** | 12.3 i 13.4 u ledgeru | **ZATVOREN** | 12.3 i 13.4 uvršteni sa statusom, inline podsekcije označene |
| **GATE-01** | P0.5–P0.13 formalizirani | **ZATVOREN** | Formalizirani u Annexu A (dopuniti Wiring tablicu) |
| **ID-01** | Asistent-prvo na vrhu plana | **ZATVOREN** | "Jedna jezgra, dva profila" i generički AcceptanceContract na vrhu |
| **DAG-01** | S5 prije S4 (Build DAG) | **ZATVOREN** | Restrukturirano u K0, K1, L, M, N, O, P bez ciklusa |
| **K3** | Fleet↔Pairing ciklus | **ZATVOREN** | Uveden `internal/fleet/ids` leaf paket |
| **D2** | Journal=P0.3 (ne S0.3) | **ZATVOREN** | Ispravljeno u mapi i planu |
| **D4** | Status addendum podsekcija | **ZATVOREN** | Označeni kao `✅🟠` |
| **D6** | 18.6 Device Pairing vraćen | **ZATVOREN** | Vraćen u plan i mapu |
| **VLASNIŠTVO** | Journal/S7/PEP/15.5 invarijante | **ZATVOREN** | Usklađeno u `DESIGN-FIXES-r2.md` |

---

## PREPORUKA ZA SLJEDEĆI KORAK

Nakon što se primijeni trivijalni **jednolinijski fix za REG-01** u `DESIGN-FIXES-r2.md` (`call.RequiresProcess` umjesto `call.Effect != EffectReadOnly`), arhitektonska dokumentacija je **100% SPREMNA ZA KODIRANJE I SCAFFOLD**.

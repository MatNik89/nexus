# RED TEAM REVIEW 4 — Finalna Verifikacija i Blokerski Audit (Rev 4509743)
**Dokument:** `docs/REVIEW4-agy.md`  
**Datum:** 2026-09-02  
**Autor:** Antigravity (agy) — Red Team Reviewer  
**Kontekst:** Greenfield Go (`CGO_ENABLED=0`), nula salvagea, 108 podsekcija (S0–S18.6).  
**Verificirani dokumenti:** `/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md`, `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md`, `/home/matej/HARNESS/nexus/docs/GAPFIX-*.md`, `/home/matej/HARNESS/nexus/docs/DESIGN-memory-effectpath-kilo.md`.

---

## SAŽETAK VERIFIKACIJE (Executive Summary)

U ovoj finalnoj rundi (Iteracija 2, Rev 4509743), **svi preostali blokeri, sigurnosne rupe u izvršnom putu, cirkularni importi i dokumentacijska neslaganja STVARNO SU ZATVORENI u izvornim dokumentima**.

**EKSPLICITNA PRESUDA:**  
> **NEMA BLOKERA — SPREMNO ZA PRD I SCAFFOLD JEZGRE.**

---

## 1. DETALJNA VERIFIKACIJA R3 NALAZA (Status po ID-u)

### [REG-01] Effect-Path: Grananje po `ExecutionKind` + ASK-switch + AfterTool Veto
* **Status:** `[ZATVOREN]`
* **Dokaz u kodu (`DESIGN-FIXES-r2.md:30-50`):**
  1. **Grananje:** Uvjet je zamijenjen s `if call.ExecutionKind == contracts.ExecProcess { exec = p.sbproc }`. Svi OS procesi (i read-only poput `ls`, `git log`, inspekcije) sada neizostavno idu u `S6.2 SandboxBackend.Launch`.
  2. **Totalni PEP switch:**
     ```go
     switch p.pep.Decide(call) {
     case s6.DENY:  return denied(call), nil
     case s6.ASK:
         if !p.pep.ApprovedExact(call) { return needsApproval(call), nil }
     case s6.ALLOW: // nastavi
     }
     ```
     `ASK` više ne pada u `ALLOW`, već zahtijeva exact-intent odobrenje (`ApprovedExact`) ili blokira izvršenje.
  3. **Greške i veto:** Na `exec.Execute` grešku poziva se `p.mw.OnError(ctx, err)` i preskače `AfterTool`. Ako `AfterTool` uloži post-exec veto (`verr != nil`), output se odbacuje i vraća greška (fail-closed).

---

### [REG-02] `HARNESS-SPEC.md` Wiring tablica ažurirana (+7 ugovora)
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu (`HARNESS-SPEC.md:1352-1358`):**
  Wiring tablica u Annexu A sada sadrži svih 24 normativnih ugovora (17 baznih + 7 formaliziranih):
  - `P0.5 Capability-matrix fail-closed` → S0.3
  - `P0.6 Config-bounds + process-identity` → S1.1/1.2
  - `P0.7 Structured-output never-silent` → S2.3
  - `P0.8 Hardware-fit fail-closed` → S2.4
  - `P0.9 In-turn verify never-silent` → S3.5
  - `P0.11 Shadow-checkpoint integritet` → S5.1/5.3
  - `P0.13 Decay-never-destroys-bytes` → S9.4

---

### [REG-03] `SECTION-MAP.md` Zbroj usklađen na 108
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu (`SECTION-MAP.md:112`):**
  Tekst glasi: `**Zbroj (108):** ✅ ~24 dizajn-gotovih (uklj. 8 novih GAPFIX) · 🔵 9 auditanih · 🟡 ~40 dizajn-treba · 🟠 ~15 odluka-PRD · ⬇ 3 descope.`  
  Broj podsekcija je sada potpuno usklađen kroz sve dokumente (108).

---

### [REG-04] Eliminacija zaostalih .py referenci u `HARNESS-PLAN.md`
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu (`HARNESS-PLAN.md`):**
  Svih 5 zaostalih Python naziva zamijenjeno je kanonskim Go paketima:
  - `258 (S3.4)`: `internal/circuit` (stuck-detection)
  - `366 (S6.1)`: `internal/security/perm` (permission gating)
  - `382 (S6.4)`: `internal/security/secrets` (secret redaction)
  - `397 (S6.8)`: `internal/security/supplychain` (supply-chain OSV)
  - `492 (S9.2)`: `internal/memory` (persistent memory spine)

---

## 2. VERIFIKACIJA K3 I META-REGRESIJA (Patch-layer i Banneri)

### [K3 / R3-04] `internal/fleet/ids` je čisti Leaf paket bez ovisnosti
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu (`DESIGN-FIXES-r2.md:71-80`):**
  - `internal/fleet/ids` sadrži isključivo skalarne tipove (`NodeID`, `DeviceID`) s 0 internih importa.
  - `PlacementGrant` je premješten u `internal/fleet`, čime `ids` ostaje striktni leaf paket na dnu stabla ovisnosti.

### [R3-01] Postavljanje `SUPERSEDED` bannera na starije dizajne
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu:**
  - `DESIGN-memory-effectpath-kilo.md:1`: nosi `⚠ EffectPath SUPERSEDED` banner koji usmjerava na `DESIGN-FIXES-r2.md`.
  - `GAPFIX-codex.md:1`: nosi `⚠ DJELOMIČNO SUPERSEDED` banner za `fleet/ids`.
  - `GAPFIX-kilo.md:1`: nosi `⚠ DJELOMIČNO SUPERSEDED` banner za `ObligationStore` (write ide kroz journal P0.3).

### [R3-02] Propagacija fixeva u tekst podsekcija `HARNESS-PLAN.md`
* **Status:** `[ZATVOREN]`
* **Dokaz u dokumentu (`HARNESS-PLAN.md`):**
  - `317 (S4.8)`: Ažurirano na `TestKeystrokeIntoUnknownFieldRequiresApproval` (approval po tipu znaka, ne nemoguća detekcija passworda).
  - `535 (S10.5)`: Ažurirano na `CAGDecision.Decide` uz eksplicitan fallback `CAG DEGRADIRA u RAG` (nema dangling stanja).
  - `774 (S18.4)`: Ažurirano na `Litestream / golang-migrate` nad SQLite-WAL bez čekanja S0.4.

---

## 3. FINALNI SCORECARD SVIH NALAZA

| Područje / ID | Opis | Status |
|---|---|---|
| **RUN-01 / REG-01** | Sandbox u EffectPathu + ExecutionKind grananje + Totalni ASK switch | **ZATVOREN** |
| **RUN-02** | InProcessExecutor vs SandboxedProcessExecutor split | **ZATVOREN** |
| **SEC-01 / REG-03** | Kanonski broj 108 podsekcija kroz sve dokumente i sume | **ZATVOREN** |
| **SEC-02** | Vraćanje 12.3 i 13.4 u ledger sa statusima | **ZATVOREN** |
| **GATE-01 / REG-02** | P0.5–P0.13 formalizirani i uvedeni u Spec Wiring tablicu | **ZATVOREN** |
| **ID-01** | Asistent-prvo identitet (Jedna jezgra, dva profila) na vrhu plana | **ZATVOREN** |
| **DAG-01** | Poredak gradnje: S5 ispred S4; K0/K1/L/M/N/O/P bez ciklusa | **ZATVOREN** |
| **K3 / R3-04** | `fleet/ids` leaf paket razrješava cirkularni import | **ZATVOREN** |
| **D2** | Journal write-owner je P0.3 paket (ne S0.3) | **ZATVOREN** |
| **D4** | Addendum podsekcije klasificirane kao `✅🟠` | **ZATVOREN** |
| **D6** | Podsekcija 18.6 Device Pairing očuvana i mapirana | **ZATVOREN** |
| **D5 / REG-04** | Uklonjene sve zaostale .py reference iz plana | **ZATVOREN** |
| **VLASNIŠTVO** | ObligationStore→Journal P0.3; S7 sole retry owner; PEP policy | **ZATVOREN** |

---

## ZAKLJUČAK I SPREMNOST ZA SLJEDEĆU FAZU

Nakon 4 kruga rigorozne Red Team revizije:
1. **Nema neriješenih blokera** u arhitekturi, tipovima, invarijantama ni poretku gradnje.
2. **Nema cirkularnih ovisnosti** među paketima niti fazama.
3. **Nema fantomskih gateova** u specifikaciji.
4. **Izvršni put je siguran:** svi OS procesi prolaze kroz S6.2 sandbox bez obzira na read-only klasifikaciju, a odobrenja (ASK) su fail-closed.

**Status repozitorija:** **POTPUNO SPREMNO ZA PRD.md I GO KERNEL SCAFFOLD.**

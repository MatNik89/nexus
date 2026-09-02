# RED TEAM REVIEW 5 — Verifikacija Iteracije 3 (Rev 51b0263)
**Dokument:** `docs/REVIEW5-agy.md`  
**Datum:** 2026-09-02  
**Autor:** Antigravity (agy) — Red Team Reviewer  
**Kontekst:** Greenfield Go (`CGO_ENABLED=0`), nula salvagea, 108 podsekcija (S0–S18.6).  
**Verificirani dokumenti:** `/home/matej/HARNESS/nexus/docs/DESIGN-FIXES-r2.md`, `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md`, `/home/matej/HARNESS/nexus/docs/GAPFIX-*.md`, `/home/matej/HARNESS/nexus/docs/DESIGN-memory-effectpath-kilo.md`.

---

## SAŽETAK VERIFIKACIJE (Executive Summary)

U ovoj 5. rundi provjere (Iteracija 3, Rev 51b0263) izvršena je detaljna inspekcija svih izmjena kojima su adresirani nalaza iz Codex i Kilo Review 4 izvještaja:
1. `RunTool` u `DESIGN-FIXES-r2.md` implementira **totalni switch** s `default-deny` za `PEP.Decide`, **totalni switch** s `default-reject` (ne inproc) za `ExecutionKind`, te **fail-closed veto obradu** koja ide kroz `OnError` i postavlja effect-phase `RECONCILING`.
2. `ToolSpec` i `ExecutionKind` kanonizirani su u `HARNESS-PLAN.md` §4.1.
3. `PlacementGrant` je premješten u neutralni `internal/contracts` paket, a `internal/fleet/ids` ostaje striktni leaf paket skalara bez internih importa. `pairing.RemoteInvoke` ima konkretan, necirkularan tipski potpis.
4. `AccountFleet` je u planu i mapi striktno ograničen na `Classify`-only bez vlastite failover petlje (`S7` je jedini owner).
5. Svi pomoćni dokumenti (`GAPFIX-agy.md`, `GAPFIX-codex.md`, `GAPFIX-kilo.md`, `DESIGN-memory-effectpath-kilo.md`) opremljeni su eksplicitnim `SUPERSEDED` bannerima.
6. Svih 7 ugovora (`P0.5`–`P0.13`) u planu je označeno kao „u Annexu A (dodan)”, a Wiring tablica u specu sinkronizirana je na 24 ugovora.
7. DAG u `SECTION-MAP.md` u potpunosti je dopunjen sa svim manjim i punim podsekcijama (`6.3`–`6.8`, `8.1-puni`, `9.1`, `9.3`, `16.6-det`).

---

## DETALJNA VERIFIKACIJA STAVKI PO ID-U

### 1. `RunTool` Robusnost i Sigurnosni Putevi (`DESIGN-FIXES-r2.md:30-60`)
* **PEP.Decide Total Switch + Default-Deny:** `[ZATVOREN]`  
  ```go
  switch p.pep.Decide(call) {
  case s6.ALLOW: // nastavi
  case s6.ASK:
      if !p.pep.ApprovedExact(call) { return needsApproval(call), nil }
  case s6.DENY:  return denied(call), nil
  default:       return denied(call), ErrInvalidDecision
  }
  ```
  Nepoznata ili neinicijalizirana odluka strogo fail-closed odbija poziv uz `ErrInvalidDecision`.
* **ExecutionKind Total Switch + Default-Reject:** `[ZATVOREN]`  
  ```go
  var exec ToolExecutor
  switch call.ExecutionKind {
  case contracts.ExecInProcess: exec = p.inproc
  case contracts.ExecProcess:   exec = p.sbproc
  default: return denied(call), p.mw.OnError(ctx, ErrUnknownExecKind)
  }
  ```
  Nepoznat `ExecutionKind` se ne pretpostavlja niti prosljeđuje na `inproc`, već se odbija kroz `OnError` životni ciklus.
* **AfterTool Veto -> OnError -> RECONCILING:** `[ZATVOREN]`  
  ```go
  if verr := p.mw.AfterTool(ctx, call, out); verr != nil {
      oe := p.mw.OnError(ctx, verr)
      phase := classifyEffectPhase(call, out)
      return p.retry.RecordVeto(grant, phase, oe)
  }
  ```
  Post-exec veto prolazi kroz `OnError` audit granu i prosljeđuje se u `RecordVeto` (koji za `IRREVERSIBLE+COMMITTED` prelazi u `UNKNOWN→RECONCILING` prema P1.4).

---

### 2. Kanonizacija `ToolSpec.ExecutionKind` (`HARNESS-PLAN.md:283`)
* **Status:** `[ZATVOREN]`  
* **Dokaz:** `4.1 tool-registry` eksplicitno definira `ToolSpec{ID, SchemaHash, EffectClass, ExecutionKind}` gdje `ExecutionKind ∈ {ExecInProcess, ExecProcess}` služi kao autoritativni pečat registryja koji diktira grananje u `EffectPath`.

---

### 3. Razrješenje Cirkularnosti Fleet/Pairing (`DESIGN-FIXES-r2.md:82-94`)
* **Status:** `[ZATVOREN]`  
* **Dokaz:**  
  - `internal/fleet/ids`: SAMO skalarni tipovi `type NodeID string; type DeviceID string` (0 importa, čisti list).
  - `internal/contracts`: posjeduje neutralni `PlacementGrant{Node ids.NodeID; Task queue.TaskID; Attempt s7.AttemptGrant; Exp time.Time}`.
  - `internal/fleet/pairing`: metoda `RemoteInvoke(node ids.NodeID, g contracts.PlacementGrant)` uvozi `ids` i `contracts`, **nula uvoza paketa `fleet`**. Nema ciklusa.

---

### 4. AccountFleet Vlasništvo Retryja (`HARNESS-PLAN.md:203`, `GAPFIX-agy.md:1`)
* **Status:** `[ZATVOREN]`  
* **Dokaz:** `HARNESS-PLAN.md:203` glasi: `AccountFleet SAMO klasificira (candidate+cooldownHint); S7 JEDINI izdaje grant/backoff/failover.` `GAPFIX-agy.md` nosi `SUPERSEDED` banner koji potvrđuje isto pravilo.

---

### 5. Annex A Ugovori i Wiring Tablica (`HARNESS-PLAN.md`, `HARNESS-SPEC.md`)
* **Status:** `[ZATVOREN]`  
* **Dokaz:**  
  - U `HARNESS-PLAN.md` uklonjene su sve reference na ugovore kao „kandidate”; ugovori P0.5, P0.6, P0.7, P0.8, P0.9, P0.11, P0.13 eksplicitno su navedeni kao formalizirani u Annexu A.
  - `HARNESS-SPEC.md:1352-1358` sadrži svih 7 novih ugovora u normativnoj Wiring tablici.

---

### 6. Potpunost Build DAG-a (`SECTION-MAP.md:18-56`)
* **Status:** `[ZATVOREN]`  
* **Dokaz:**  
  - Faza K0: `S0.1-0.5`, `EventJournal (P0.3)`, `S1.1`, `S1.2 (ProcessIdentity)`, `S1.3`, `S8.1-min`, `S11.1-min`, `S16.6-det`.
  - Faza K1: `S6.0-6.2`, `S6.9`, `S7`, `S5 (5.1-5.3)`, `6.3-6.8`.
  - Faza L: `S2`, `S3`, `S4`.
  - Faza M: `S8 (puni)`, `S9 (9.1 sesije, 9.2 store, 9.3 learn, 9.4 Decay+Audn, 9.5, 9.6)`, `S11 (puni)`.
  - Faze N, O, P: pokrivaju multi-agent, observability, sučelja, distribuciju i servise bez obrnutih ovisnosti.

---

## FINALNI ZAKLJUČAK I SPREMNOST ZA PRD / SCAFFOLD

Nakon 5 uzastopnih krugova rigorozne Red Team revizije:
- **NEMA cirkularnih ovisnosti** u paketnom stablu niti među fazama gradnje.
- **NEMA sigurnosnih bypassa** u izvršnom putu (`EffectPath` striktno provodi S6.0 PEP, S6.2 Sandbox za svaki proces, S6.9 Lifecycle i S7 Retry).
- **NEMA proturječja vlasništva** (Journal P0.3 je jedini pisac, S7 je jedini retry owner, S6.0 je jedini policy decision point).
- **NEMA fantomskih ugovora** (svih 24 ugovora normativno je definirano u Annexu A s pripadnim RED testovima).

---

> ### **PRESUDA:**  
> ### **NEMA BLOKERA — POTPUNO SPREMNO ZA PRD I GO KERNEL SCAFFOLD.**

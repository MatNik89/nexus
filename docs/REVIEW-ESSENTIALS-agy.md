# REVIEW-ESSENTIALS-agy — Adversarijalni Review (Anti-Anchoring, 3 Osi)

**Meta:** `/home/matej/HARNESS/nexus/docs/ARCHITECTURE-ESSENTIALS.md`  
**Recenzent:** Antigravity (Adversarial pass, bez rubber-stampa)  
**Izvori usporedbe:** `HARNESS-PLAN.md`, `HARNESS-SPEC.md`, `DESIGN-FIXES-r2.md`, `PLAN-HOLES-CONSOLIDATED.md`, `DESIGN-STATUS.md`, `PRD.md`, `SECTION-MAP.md`, `DESIGN-S0-sandbox-codex.md`, `DESIGN-memory-effectpath-kilo.md`, `DESIGN-symedit-crypto-claude.md`.

---

## 1. ANTI-ANCHORING: Neovisna sinteza TOP-10 kritičnih odluka iz izvora

Prije analize teksta `ARCHITECTURE-ESSENTIALS.md`, iz zaključane arhitekture i ugovora izvedena je neovisna lista 10 najkritičnijih odluka koje P0/P1 developer mora imati pred očima:

1. **Statički Single-Binary & Pure-Go Floor:** `CGO_ENABLED=0`, `modernc.org/sqlite` (WAL). Nula ne-Go alata in-process (isključivo subprocess sidecar ili pure-Go zamjena). (`PLAN-HOLES C4`, `HARNESS-PLAN:9`)
2. **Identitet & Profilna Arhitektura ("Jedna jezgra, dva profila"):** Asistent prvo (P0: razgovor, memorija, obveze, Telegram, Linux sandbox); programerski način je najjača grana (P1: TIA, symedit, coding-evidence). Isti S0-S9 kernel i TurnLoop. (`PRD §3-§4`, `SECTION-MAP:70`, `PLAN-HOLES C7`)
3. **EventJournal kao Jedini Trajni Write-Owner (P0.3):** Sav trajni state nastaje isključivo kroz `EventJournal.Append`. Projekcije (log/trace/state/metrics) nikad ne alociraju ID-ove niti pišu mimo journala; state se rekonstruira foldom; redakcija prije journala. (`HARNESS-SPEC:1396`, `DESIGN-FIXES-r2:92`)
4. **Stroge Owner-Invarijante:**
   - S6.0 PEP = jedini Policy Decision Point (`Decide()`, total switch, default-deny; ASK≠ALLOW).
   - S6.9 Lifecycle = isključivo redoslijed/order (before/after/error), nikad policy.
   - S7 = jedini vlasnik retry/cancel/deadline/fencing-a (`AttemptGrant` na svakom pokušaju).
   - S1.2 = vlasnik identiteta procesa (`PID + start-token`).
   - S6.2 = obvezni sandbox omotač svakog subprocessa. (`DESIGN-FIXES-r2:15-76`, `SECTION-MAP:58-60`)
5. **Tipizirani EffectPath Pipeline & Zapečaćeni ExecutionKind:** Total switch po zapečaćenom `ToolSpec.ExecutionKind`. Read-only shell je proces → sandbox; nepoznat kind → reject (nikad fallback u inproc). Konkretni tipovi u structu onemogućuju injektiranje nesandboxiranog executora. (`DESIGN-FIXES-r2:42-76`)
6. **Effect Taxonomy & Exact-Intent Faze (Umjesto Nemogućeg Cancela):** `CommitReceipt` + faze `PhaseBeforeCommit` / `PhaseAfterCommit` / `PhaseUnknown`. Svaki effectful poziv bez atestiranog receipta → `PhaseUnknown` → `RECONCILING` (nema slijepog retryja). Approval veže hash točne namjere (`exact-intent`). (`DESIGN-FIXES-r2:30-37`, `HARNESS-SPEC:1447`)
7. **Linux-First Sandbox Helper bez Unsandboxed Prozora (Najveći Rizik):** Self-reexec helper (`__sandbox_helper`): ABI probe → `PR_SET_NO_NEW_PRIVS` → Landlock ruleset → per-arch seccomp-BPF → `execveat`. Sanitizacija FD/env; fail-closed TOCTOU. Win/macOS u v1 = `UNAVAILABLE` + blokiran high-risk exec (nula slabijeg fallbacka). (`DESIGN-S0-sandbox-codex:1153-1200`, `PLAN-HOLES C2`)
8. **Monotoni Trust, Lineage & Univerzalni Fail-Closed:** `trust_class = MAX(sources)`, `lineage = union`. Untrusted podatak nikad ne postaje instrukcija (nema provenance launderinga). Nepoznata schema → `QUARANTINE`; SSRF metadata IP → hard-block prije diala; canary u izlazu → blok cijele isporuke. (`HARNESS-SPEC:1383`, `HARNESS-PLAN:98,373,380`)
9. **Spine Memorija, Profilna Izolacija & `FORGET ≠ PURGE`:** SQLite-WAL spine. P0 samo Decay (FSRS-lite, mijenja rang, ne briše) + Audn (supersede umjesto overwrite). `MEMORY_FORGET` (reverzibilni tombstone) ≠ `DATA_PURGE` (ireverzibilno brisanje i tombstona). `PersonalProfile` (S9.5) = deny-default izolacija work vs private (0 curenja). (`DESIGN-memory-effectpath-kilo:54-69`, `HARNESS-SPEC:1470,1570`)
10. **Evidence-Graded Completion (Worker ≠ Checker):** `Checker.Grade` ocjenjuje isključivo determinističke artefakte (git diff, stvarni exit kod, API receipt), nikad prozu modela. `ObligationStore.MarkDone` zahtijeva verificirani dokaz. (`HARNESS-SPEC:1387`, `HARNESS-PLAN:504`, `DESIGN-checker-tia-edit-agy`)

---

## 2. USPOREDBA I NALAZI PO OSIMA

---

### [FIDELITY] Nalaz F1 — Kriva atribucija ugovora za provenance laundering u E10
* **Lokacija u dokumentu:** `ARCHITECTURE-ESSENTIALS.md:75`
* **Citat iz Essentials:** `... (P0.3 provenance-laundering RED).`
* **Citat iz izvora (`HARNESS-SPEC.md:1335, 1383` / `TIER3-CONTRACTS.md:37`):**
  - Linija 1335: `| P0.1 Canonical Envelope + Envelope Schema + Upcaster | S0.1/S0.4 schema | codex#1 |`
  - Linija 1383: `- **RED — test_s0_rejects_provenance_laundering:** untrusted ContextBlock prođe kroz compaction/upcaster koji ukloni lineage i postavi trust_class=SYSTEM; prompt assembly MUST vratiti PROVENANCE_DOWNGRADE, run ne smije prijeći u RUNNING, a model sink mora imati 0 poziva.`
  - Dok je **P0.3** (`HARNESS-SPEC.md:1337, 1396`): `Jedini write-owner za event/log/trace/audit` (`test_projection_cannot_bypass_journal`).
* **Problem:** Essentials pogrešno navodi da je provenance laundering RED test dio ugovora `P0.3`, dok je on normativno vezan uz ugovor `P0.1` (Envelope Schema & Compaction Upcaster).
* **Ozbiljnost:** Srednja (fideliti / točnost referenci za P0 testiranje).

---

### [CONTRADICTION] Nalaz C1 — Zastarjeli status otvorenih stavki ("Otvoreno prije P0 koda" #2)
* **Lokacija u dokumentu:** `ARCHITECTURE-ESSENTIALS.md:123-125`
* **Citat iz Essentials:**
  ```markdown
  ## Otvoreno prije P0 koda (iz DESIGN-STATUS)
  1. Sandbox live-probe na stvarnom Linux kernelu (hostile conformance suite) — prvi zadatak.
  2. Annex A P0.x ugovori koji se citiraju moraju POSTOJATI prije citiranja kao gate (C3).
  ```
* **Citat iz izvora (`DESIGN-STATUS.md:9, 37-41` / `SECTION-MAP.md:93` / `HARNESS-SPEC.md:1534-1573`):**
  - `DESIGN-STATUS.md:9`: `Must-resolve #2 | S0 tipovi + P0.5 + activation-failure | RESOLVED | codex`
  - `DESIGN-STATUS.md:37`: `PREOSTAJE PRIJE KODA (samo tvoje): #3 stvarni-OS probe ... #6 PRD ... Sve ostalo dizajn-spremno za kod.`
  - `SECTION-MAP.md:93`: `**S0** ✅ 0.1 0.2 0.3 0.4 0.5 (codex) · P0.5-0.13 → u Annexu A (dodani; G1 zatvoren)`
  - `HARNESS-SPEC.md:1534`: `# Annex A — dopuna: ugovori P0.5–P0.13 (formalizacija citiranih gate-ova; REVIEW2 G1)`
* **Problem:** Essentials u završnom poglavlju navodi točku 2 kao *otvoreni preduvjet prije P0 koda*, iako je ta rupa (`G1 / C3`) u potpunosti formalizirana i razriješena u Annexu A (`HARNESS-SPEC.md:1534-1573`), što potvrđuju i `DESIGN-STATUS.md` i `SECTION-MAP.md`. Prije koda preostaje isključivo live probe i PRD odluka.
* **Ozbiljnost:** Visoka (stvara lažnu blokadu i proturječi `DESIGN-STATUS.md`).

---

### [FIDELITY] Nalaz F2 — Pogrešno citiranje addenduma A9 u E1
* **Lokacija u dokumentu:** `ARCHITECTURE-ESSENTIALS.md:13`
* **Citat iz Essentials:** `→ HARNESS-PLAN header, S1.2, A9; PLAN-HOLES C4.`
* **Citat iz izvora (`SECTION-MAP.md:79`):**
  - `A9 fold-gapovi → 12.2/7.2/17.1 (superstep/ingress-queue/Cordis)`
* **Problem:** Addendum `A9` se bavi orkestracijom, redovima i Cordis plugin runtimeom, a ne `CGO_ENABLED=0` zabranom in-process ne-Go alata. Točna atribucija za non-Go sidecare je `PLAN-HOLES C4` i `HARNESS-PLAN header/S1.2/S13`.
* **Ozbiljnost:** Niska (minorni tipfeler u referenci).

---

### [MISSING] Nalaz M1 — Izostavljen ugovor dvofazne aktivacije i capability matrice (P0.5 / S0.3 / S0.5)
* **Lokacija u dokumentu:** E8 / E9 (spomenuto samo fragmentirano u 1 rečenici)
* **Citat iz izvora (`DESIGN-S0-sandbox-codex.md` / `DESIGN-STATUS.md:9` / `HARNESS-SPEC.md:1539-1543`):**
  - `ActivationPlan(immutable + PlanHash)` + 3-step lifecycle: `Prepare` → `Commit` → `Activate` + `RollbackToken` + stanja `PREPARING / FAILED / ROLLING_BACK`.
  - `P0.5`: `Effective() = min(declared, measured)`; promjena konfiguracije invalidira grant; nepoznata sposobnost → reject.
* **Zašto je kritično za P0 programera:** "Atomska aktivacija konfiguracije je nemoguća kad gate ima vanjski OS side-efekt" bio je jedan od glavnih arhitektonskih blokera (Must-resolve #2). Programer u Fazi K0 koji piše inicijalizaciju kernela mora znati da se konfiguracija i capability profil ne mogu mutirati ad-hoc, već isključivo kroz immutable `ActivationPlan` s rollback garancijom.
* **Preporuka:** U E8 ili E9 eksplicitno dodati invariant dvofazne aktivacije (`Prepare/Commit/Activate` + `Effective=min(declared, measured)`).

---

### [MISSING] Nalaz M2 — Izostavljen ugovor atomskog FS pisanja i Pre-FS snapshota (P0.11 / S5.3)
* **Lokacija u dokumentu:** E15 spominje potpis shadow-checkpointa, ali nedostaje mehanika pisanja
* **Citat iz izvora (`HARNESS-SPEC.md:1564-1568` / `HARNESS-PLAN.md:340` / `PLAN-HOLES C5`):**
  - `AtomicWriter`: `tmp → fsync → rename` (nikad in-place prepisivanje).
  - Snapshot se uzima **PRIJE svakog FS-efekta** (ne svakog side-efekta općenito).
  - `Rollback` vraća byte-identično stanje bez diranja ne-staged korisničkih izmjena.
* **Zašto je kritično za P0 programera:** Bilo kakav rad na memoriji (S9), konfiguraciji (S1), obligation storeu (S9.6) ili alatu za izmjenu datoteka mora poštovati pravilo `tmp → fsync → rename` kako bi se spriječilo djelomično (corrupted) stanje diska pri padu procesa.
* **Preporuka:** Dodati pravilo `tmp → fsync → rename` i pre-FS snapshot u E8 ili E3.

---

### [CONTRADICTION / FIDELITY] Nalaz C2 — Neusklađenost P0 Telegram kanala i P1.6 ugovora (`channels requires extensions`)
* **Lokacija u dokumentu:** `ARCHITECTURE-ESSENTIALS.md:107, 118-119`
* **Citat iz Essentials:**
  - E14 (linija 107): `→ S14.5, S7.3 (N9), S12.4, P1.6.`
  - P0 scope-rez (linija 118-119): `P0 = 6 sposobnosti iz PRD §4 (... Telegram ...). Izbačeno iz v1: ... extensions engine ...`
* **Citat iz izvora (`HARNESS-SPEC.md:1459-1466` / `SECTION-MAP.md:48, 52`):**
  - `HARNESS-SPEC.md:1461`: `S0.5 deklarira channels requires extensions+7.3+approval-core`
  - `HARNESS-SPEC.md:1466`: `RED — test_channels_contract_fails_closed: slučaj A aktivira channels bez extensions/S6.8 attestationa → INCOMPLETE_CAPABILITY_CLOSURE.`
  - `SECTION-MAP.md:48`: `14.5 channels-stdio-built-in (plugin-channels→P)`
* **Problem:** Ako P0 uključuje Telegram (po PRD §4), a `ARCHITECTURE-ESSENTIALS.md` u E14 citira `P1.6` bez distinkcije, dolazi do blokade: ugovor P1.6 fail-closed ruši sustav s `INCOMPLETE_CAPABILITY_CLOSURE` ako `extensions` podsustav nije prisutan (a `extensions` je izbačen iz P0!).
* **Rješenje:** Essentials mora jasno precizirati da je **P0 Telegram ugrađeni (built-in) kanal** (iz Faze O), dok se P1.6 closure zahtjev (`requires extensions`) odnosi na dinamičke adapterske ekstenzije (Faza P).

---

### [NOT-CRITICAL] Nalaz N1 — Balans selekcije: E9 i E15 kao implementacijski detalji vs arhitektonski ugovori
* **Lokacija u dokumentu:** E9 (Go tipovi / `map[string]any`) i E15 (Kripto bug / `hash.Sum(key)`)
* **Analiza:**
  - E9 se bavi izbjegavanjem `map[string]any` i korištenjem `json.RawMessage`, što je više Go stilski idiom nego krupna arhitektonska odluka na razini sustava.
  - E15 je specifični patch za kripto bug (D2 fix: zabrana `hash.Sum(key)`).
  - Iako su oba korisna, zauzimaju 2 od 15 slotova dok su krupne mehanike poput **Dvofazne aktivacije (ActivationPlan P0.5)** i **Atomskog FS pisanja (`tmp->fsync->rename` P0.11)** ostale nespomenute.
* **Preporuka:** E15 i E9 se mogu kompaktirati pod E8 (Fail-closed & Security invariants) kako bi se oslobodio prostor za P0.5 (ActivationPlan) i P0.11 (Atomic Write/Snapshot).

---

## 3. TOP-3 NAJSLABIJE TOČKE DOKUMENTA

Ako se zanemare sitne reference, 3 točke koje najviše ugrožavaju praktičnu vrijednost cheat-sheeta su:

1. **Lažni otvoreni bloker #2 na dnu dokumenta:** Zbunjuje programera tvrdnjom da P0.x ugovori još ne postoje, iako su formalizirani u `HARNESS-SPEC.md:1534-1573`.
2. **Kriva atribucija u E10 (`P0.3` umjesto `P0.1`):** Programer koji piše RED test za provenance laundering tražit će specifikaciju pod krivim brojem ugovora.
3. **Nerazjašnjen status P0 Telegrama naspram P1.6 capability closurea:** Bez napomene da je Telegram built-in kanal, implementacija P0 bi po slovu P1.6 ugovora bila neizvediva bez S17 ekstenzija.

---

## 4. PREPORUČENI PATCH (KORAK PO KORAK)

Kako bi `ARCHITECTURE-ESSENTIALS.md` bio 100% usklađen sa zaključanim izvorima, potrebno je primijeniti sljedeće izmjene:

1. **E1 (linija 13):** Zamijeniti `A9` s `PLAN-HOLES C4, S1.2, S13`.
2. **E10 (linija 75):** Zamijeniti `(P0.3 provenance-laundering RED)` s `(P0.1 provenance-laundering RED)`.
3. **E14 (linija 102-107):** Dodati razjašnjenje: `Telegram = built-in adapter (P0); P1.6 extensions-closure vrijedi za dinamičke kanale u Fazi P.`
4. **E8 / E9:** Dodati eksplicitnu napomenu za `P0.5 ActivationPlan` (`Prepare/Commit/Activate`) i `P0.11 AtomicWriter` (`tmp->fsync->rename`).
5. **Dno dokumenta (linije 123-126):** Obrisati stavku 2 iz "Otvoreno prije P0 koda" jer su P0.x ugovori već formalizirani i dodani u Annex A.

---

VERDICT: FAIL

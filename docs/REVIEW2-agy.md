# RED TEAM REVIEW 2 — Cross-Document & System Logic Audit
**Dokument:** `docs/REVIEW2-agy.md`  
**Datum:** 2026-09-02  
**Autor:** Antigravity (agy) — Red Team Reviewer  
**Kontekst:** Greenfield Go (`CGO_ENABLED=0`), nula salvagea, 19 sekcija (S0–S18).  
**Ciljni dokumenti:** `/home/matej/HARNESS/nexus/docs/HARNESS-PLAN.md`, `/home/matej/HARNESS/nexus/docs/SECTION-MAP.md`, `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md`, `/home/matej/HARNESS/nexus/docs/DESIGN-*.md`, `/home/matej/HARNESS/nexus/docs/PLAN-HOLES-CONSOLIDATED.md`, `/home/matej/HARNESS/nexus/docs/DESIGN-STATUS.md`.

---

## SAŽETAK NALAZA (Executive Summary)

Plan je uspješno očišćen od salvage-koda i opremljen parcijalnim dizajnom (S0, Sandbox, Effect-path, Edit/TIA, Symedit/Crypto). Međutim, detaljan **Red Team audit cjeline i međusobne usklađenosti dokumenata** otkrio je **5 skupina kritičnih rupa i nelogičnosti**:

1. **Aritmetički i inventarni rascjep (SEC):** `HARNESS-PLAN.md` tvrdi 82 podsekcije, `SECTION-MAP.md` broji "81 + inline", a stvarni tekst sadrži **107 podsekcija**. Dva stvarna modula (`12.3` routing i `13.4` video) potpuno su **izbrisana/zaboravljena** u ledgeru `SECTION-MAP.md`, dok je 14 podsekcija gurnuto u "inline" zonu bez statusne klasifikacije.
2. **Cirkularne i invertirane ovisnosti gradnje (DAG):** Build DAG u `SECTION-MAP.md` postavlja alate (S4) prije workspace primitiva (S5), iako `S4.3 EditEngine` izravno ovisi o `S5.1 ShadowGit` i `S5.3 AtomicWriter`. Core loop (S3 u Fazi L) ovisi o prompt assemblyju (S11) i window managementu (S8) koji su smješteni u kasniju Fazu M. Kanali (S14.5 u Fazi O) ovise o ekstenzijama (S17 u Fazi P).
3. **Annex A Gate Void & Fantomski ugovori (GATE):** `HARNESS-PLAN.md` citira 7 nepostojećih "fantomskih" ugovora (`P0.5`, `P0.6`, `P0.7`, `P0.8`, `P0.9`, `P0.11`, `P0.13`) koji **uopće ne postoje u normativnom Annexu A** (gdje postoji samo 17 ugovora). Preko **65 podsekcija** (uključujući cijeli S10 retrieval, S13 multimodal, te većinu S1, S6, S8, S9, S14, S15, S16) visi **potpuno bez formalnog Annex A gatea**.
4. **Šizofrenija identiteta i coding-pristranost (ID):** Vrh plana (`HARNESS-PLAN.md:42`) još uvijek ima direktivu *"coding = PRVORAZREDNI CILJ"*, što je u izravnom proturječju s Addendumom A3.1 (*"osobni asistent PRVO, coding grana"*). Faza L u mapi je nazvana *"coding-jezgra"*, checker u planu se opisuje isključivo kao `git-diff + exit-code`, a asistentske komponente (ObligationStore, PersonalProfile, kanali) nemaju ni 10% dubine dizajna coding podsustava.
5. **Kritična rupa u izvršnom putu (RUN):** `DESIGN-memory-effectpath-kilo.md` u `EffectPath.RunTool` poziva izravno `p.proc.Spawn` (S1.2), **potpuno zaobilazeći `S6.2 SandboxBackend` (Landlock/seccomp)**. Također pretpostavlja da su svi alati OS procesi, lomeći in-process Go alate (`pkg/edit`, AST search, in-memory RAG).

---

## 1. CROSS-DOCUMENT NESLAGANJA (HARNESS-PLAN vs SECTION-MAP)

### [SEC-01] Rascjep u brojanju podsekcija (82 vs 81 vs 88 vs 105 vs 107)
* **Lokacija:** `HARNESS-PLAN.md:4,7,784`, `SECTION-MAP.md:77,99`.
* **Nalaz:** 
  - `HARNESS-PLAN.md` na tri mjesta tvrdi: *"82 podsekcije"*.
  - `SECTION-MAP.md` u naslovu ledgera tvrdi: *"svih 81 + inline pod-stavke"*, a u zbroju (redak 99) navodi: `22 + 9 + 30 + 12 + 3 + 7 = 83`.
  - Stvarno prebrojavanje eksplicitno definiranih `**X.Y` cjelina u tekstu `HARNESS-PLAN.md` daje **107 podsekcija** (100 naslijeđenih iz v0.9 spec-a + 7 novih iz addenduma: 4.8, 6.10, 9.5, 9.6, 12.7, 12.8, 18.5).
  - Korisnički prompt navodi "88 podsekcija".
* **Posljedica:** Nijedan dokument nema točnu evidenciju opsega. Agenti i planeri operiraju s 25 podsekcija "u magli".
* **Popravak:** Standardizirati kanonski broj na **107 podsekcija** (ili točno specificirati baznih 100 + 7 addenduma) kroz sve dokumente.

### [SEC-02] Izbrisane / Zaboravljene podsekcije u SECTION-MAP ledgeru: `12.3` i `13.4`
* **Lokacija:** `SECTION-MAP.md:91-92` vs `HARNESS-PLAN.md:595,633`.
* **Nalaz:**
  - `12.3 Routing` (subagent model-routing potrošač) definiran je u `HARNESS-PLAN.md:595` i `HARNESS-SPEC.md:670`, ali je **potpuno preskočen i izostavljen** u statusnom ledgeru `SECTION-MAP.md:91`.
  - `13.4 Video obrada` (FFmpeg, WhisperX, ComfyUI) definiran je u `HARNESS-PLAN.md:633` i `HARNESS-SPEC.md:728`, ali je **potpuno preskočen i izostavljen** u statusnom ledgeru `SECTION-MAP.md:92`.
* **Posljedica:** `SECTION-MAP.md` ima "rupu u mreži" — 2 stvarne podsekcije nemaju dodijeljen status, fazu ni vlasnika.
* **Popravak:** Eksplicitno dodati `12.3` (kao `🔵 AUDITAN` ili `🟡 DIZAJN-TREBA`) u S12 i `13.4` (kao `🟠 ODLUKA-PRD / ADAPTER`) u S13.

### [SEC-03] "Inline" podsekcije bez statusne klasifikacije
* **Lokacija:** `SECTION-MAP.md:85,89,90,93,94,95,96,97`.
* **Nalaz:** Čak 14 podsekcija je u ledgeru označeno prefiksom `inline` bez standardnih statusnih ikona (✅/🔵/🟡/🟠/⬇):
  - `6.7 governance`, `10.2 chunk`, `10.3 hibrid`, `10.5 CAG`, `11.4 prompt-opt`, `14.2 SDK`, `14.3 web-UI`, `15.2 cost-dash`, `15.4 SLO`, `16.2 ratchet`, `16.3 red-team`, `17.2 update`, `17.3 packaging`, `18.2 tenant-gw`, `18.3 canary`, `18.4 backup`.
* **Posljedica:** Status-ledger je lažno sažet; za 14 komponenti se ne zna jesu li P0/P1, imaju li dizajn ili čekaju PRD.
* **Popravak:** Svakoj inline podsekciji dodijeliti eksplicitni status (npr. `17.2 update (TUF)` → `🟡`, `10.5 CAG` → `🟡`, `14.3 web-UI` → `🟠`).

### [SEC-04] Podsekcije s nedefiniranim statusom unutar istog retka
* **Lokacija:** `SECTION-MAP.md:81,83,85,88,91,97`.
* **Nalaz:** 
  - U S2: `2.4 local` i `2.5 cost` navedeni su iza `✅ 2.3 structured` bez ikakvog prefiksa statusa.
  - U S4: `4.8 computer-use (NOVA)` nema statusnu oznaku.
  - U S6: `6.10 exec-auto-reviewer (NOVA)` nema statusnu oznaku.
  - U S9: `9.5 PersonalProfile` i `9.6 ObligationStore` nemaju statusnu oznaku.
  - U S12: `12.7 flows` i `12.8 boards` nemaju statusnu oznaku.
  - U S18: `18.5 fleet+device-pairing` nema statusnu oznaku.
* **Posljedica:** Čitatelj mora nagađati jesu li nove komponente dizajnirane, auditane ili odgođene.
* **Popravak:** Dodati eksplicitne oznake (većina novih je `🟠 ODLUKA-PRD` prema retku 71).

---

## 2. LOGIKA REDOSLIJEDA GRADNJE I CIRKULARNE OVISNOSTI (DAG FAZA K–P)

```
Trenutni DAG u SECTION-MAP.md:
FAZA K: S0 -> S1 (->S0) -> S6 (->S0,S1) -> S7 (->S0,S1)
FAZA L: S2 (->S0,S7) -> S3 (->S0,S2,S6,S7) -> S4 (->S3,S6) -> S5 (->S1,S4) -> S16.6 (->S3)
FAZA M: S8 (->S0,S3) -> S9 (->S0,S7) -> S11 (->S0,S8)
FAZA N: S12 (->S3,S5.2,S7) -> S15 (->S0.3) -> S10 (->S9)
FAZA O: S14 (->jezgra) -> S13 (ADAPTERI)
FAZA P: S17 -> S18 -> S16.1-16.4
```

### [DAG-01] Inverzija i cirkularnost S4 (Alati) vs S5 (Workspace/VCS)
* **Lokacija:** `SECTION-MAP.md:25-26`, `DESIGN-checker-tia-edit-agy.md:580-660`.
* **Problem:** DAG definira redoslijed: `S4 tools (->S3,S6)` pa zatim `S5 workspace (->S1,S4)`.
* **Arhitektonska kolizija:**
  1. `S4.3 EditEngine` (file edit) zahtijeva atomično pisanje na disk (`S5.3 AtomicWriter`) i snimanje stanja radnog direktorija prije mutacije (`S5.1 ShadowGit snapshot`).
  2. Ako S4 ovisi o S5 primitivima za sigurnu manipulaciju datotekama, S4 **ne može biti izgrađen prije S5**.
  3. Suprotno tome, S5 (repozitorij, worktree, checkpoint) je temeljni sloj za pohranu koji ovisi samo o FS/proc sloju (S1), a ne o alatima koji ga konzumiraju.
* **Posljedica:** Nemoguće je kompajlirati/testirati `S4.3` bez gotovog `S5.1/5.3`.
* **Popravak:** Promijeniti poredak u Fazi L: **S5 ide PRIJE S4**. S5 ovisi o `(→S0, S1)`, a S4 ovisi o `(→S0, S1, S5, S6)`.

### [DAG-02] Inverzija Faze L (Core Loop S3) vs Faze M (Prompt Assembly S11 & Window Budget S8)
* **Lokacija:** `SECTION-MAP.md:24,30,32`.
* **Problem:** `S3 core-loop` smješten je u Fazu L, dok su `S8 context-management` (window budget, compaction) i `S11 prompt/skills` smješteni u Fazu M.
* **Arhitektonska kolizija:**
  1. Core loop u svakom turnu mora: (a) dohvatiti prompt i instrukcije (`S11.1`), (b) provjeriti token budget i primijeniti compaction ako je prozor pun (`S8.1/8.2`), (c) poslati context blockove provideru (`S2`).
  2. Ako S8 i S11 nisu implementirani (Faza M), TurnLoop u Fazi L ne može izvršiti niti jedan cjeloviti LLM turn bez privremenih "mock" skela koje se kasnije moraju baciti.
* **Popravak:** Razdvojiti minimalne jezgrene ugovore za Prompt & Context Budget u Fazu K/L (npr. `S11.1 Assembly Minimal` i `S8.1 Budget Minimal`), dok napredne značajke (compaction 8.2, archmap 8.3, skill-forge 11.5) ostaju u Fazi M.

### [DAG-03] Cross-Phase inverzija: Kanali (Faza O) ovise o Ekstenzijama/Pakiranju (Faza P)
* **Lokacija:** `SECTION-MAP.md:40,44` vs `HARNESS-SPEC.md:1454 (P1.6)`.
* **Problem:** `S14.5 Channels` smješten je u Fazu O (Sučelja). Međutim, normativni ugovor `Annex A P1.6` eksplicitno nalaže:
  `channels requires extensions + 7.3 + approval-core`.
  Sustav ekstenzija/plugina (`S17.1`) i distribucijskog pakiranja (`S17.3`) nalazi se u **Fazi P**.
* **Posljedica:** Faza O ne može zadovoljiti closure i prolaz sigurnosnog gatea `P1.6` jer podsustav `extensions` iz Faze P još ne postoji.
* **Popravak:** Premjestiti bazični `extensions` engine u Fazu K/L ili formalno razdvojiti `S14.5 Channels (Built-in CLI/Stdio)` od `S14.5 External Channel Adapters` koji se vežu na Fazu P.

### [DAG-04] Cirkularnost S16.6 Checker (Faza L) vs S12.6 Council (Faza N)
* **Lokacija:** `SECTION-MAP.md:27,35`, `HARNESS-SPEC.md:1518 (P2.7)`.
* **Problem:** `S16.6 Evidence-Checker` uvršten je u Fazu L (dizajn-gotov). Ugovor `P2.7` i specifikacija navode Council kao jednu od verifikacijskih strategija checkera. No, `S12.6 Council` gradi se tek u Fazi N.
* **Posljedica:** Checker se u Fazi L mora strogo ograničiti na determinističke orakle (`coding.diff_exit`, `system.state_invariant`), dok se multi-agent delegacija mora eksplicitno izolirati kao opcionalni plugin u Fazi N.
* **Popravak:** Zabilježiti u SECTION-MAP: `S16.6 (v1 deterministic only) -> Faza L`, `S16.6 (Council integration) -> Faza N`.

---

## 3. ANNEX A GATE VOID & FANTOMSKI UGOVORI (P0.5–P0.13)

Normativni **Annex A** u `HARNESS-SPEC.md:1325-1524` sadrži točno **17 normativnih ugovora**:
- **P0 (4):** P0.1 (Envelope/ContextBlock), P0.2 (Retry/Cancel owner), P0.3 (Journal write-owner), P0.4 (Capability closure).
- **P1 (6):** P1.1 (skills.lock), P1.2 (Orphan sweep), P1.3 (MCP auth), P1.4 (Draft-commit-verify), P1.5 (Artifact sign), P1.6 (Channels HITL).
- **P2 (7):** P2.1 (Memory forget/purge), P2.2 (Resource budget), P2.3 (Queue lease), P2.4 (Cache boundary), P2.5 (Residency), P2.6 (Automation clock), P2.7 (Council).

### [GATE-01] Citiranje 7 nepostojećih "Fantomskih" ugovora u HARNESS-PLAN-u
* **Lokacija:** `HARNESS-PLAN.md:105,171,224,274,349,508`.
* **Nalaz:** `HARNESS-PLAN.md` na više mjesta opravdava spremnost sekcija pozivajući se na ugovore koji **NE POSTOJE u specifikaciji**:
  1. **`P0.5` ("capability-matrix fail-closed"):** citiran u S0.3 i S1 kao gate. U Annexu A postoji samo P0.1–P0.4. (Codex je napisao draft u zasebnom fajlu, ali nije formaliziran u specu).
  2. **`P0.6` ("config-bounds + process-identity"):** citiran u S1.1 i S1.2. Ne postoji.
  3. **`P0.7` ("structured-never-silent"):** citiran u S2.3. Ne postoji.
  4. **`P0.8` ("hardware-fit-fail-closed"):** citiran u S2.4. Ne postoji.
  5. **`P0.9` ("verify-never-silent"):** citiran u S3.5. Ne postoji.
  6. **`P0.11` ("shadow-checkpoint-integrity"):** citiran u S5. Ne postoji.
  7. **`P0.13` ("decay-never-destroys-bytes"):** citiran u S9.4. Ne postoji.
* **Posljedica:** Plan stvara lažni osjećaj pokrivenosti ("gate postoji"). U stvarnosti, te sekcije nemaju normativni temelj u Annexu A.
* **Popravak:** Ili formalno unijeti P0.5–P0.13 u `HARNESS-SPEC.md Annex A` ili ukloniti reference i priznati ih kao `UNRESOLVED / Honest Gap`.

### [GATE-02] Popis 65+ podsekcija koje "vise" bez ijednog Annex A Gatea
* **Lokacija:** Cijeli `HARNESS-PLAN.md` kroz analizu 107 podsekcija.
* **Nalaz:** Sljedeće podsekcije nemaju NITI JEDAN stvarni Annex A ugovor koji ih štiti kao verifikacijski gate:
  - **S1:** 1.1 (config), 1.2 (proc — samo posredno kroz P1.2).
  - **S2:** 2.1 (auth), 2.3 (structured), 2.4 (local), 2.6 (routing).
  - **S3:** 3.1 (core loop), 3.2 (streaming), 3.5 (TIA / verifikacija).
  - **S4:** 4.1 (registry), 4.3 (edit), 4.4 (search), 4.5 (web), 4.7 (tool budget).
  - **S5:** 5.1 (checkpoint), 5.3 (atomic writer).
  - **S6:** 6.0 (PEP policy), 6.1 (permissions), 6.3 (egress), 6.5 (injection), 6.6 (authn), 6.10 (exec reviewer).
  - **S8:** 8.1 (window), 8.3 (archmap), 8.5 (pruning).
  - **S9:** 9.1 (sessions), 9.3 (learn), 9.5 (profiles), 9.6 (obligations).
  - **S10:** 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7 (CIJELI RETRIEVAL NEMA NITI JEDAN ANNEX GATE!).
  - **S11:** 11.1, 11.2, 11.3, 11.4, 11.6.
  - **S12:** 12.1, 12.2, 12.3, 12.7.
  - **S13:** 13.1, 13.2, 13.3, 13.4, 13.5 (CIJELI MULTIMODAL NEMA NITI JEDAN ANNEX GATE!).
  - **S14:** 14.1, 14.2, 14.3.
  - **S15:** 15.1, 15.2, 15.4, 15.5.
  - **S16:** 16.1, 16.2, 16.3, 16.4, 16.5.
  - **S17:** 17.2, 17.3.
  - **S18:** 18.2, 18.3, 18.4, 18.5.
* **Posljedica:** Više od 60% sustava razvija se bez formalnih `MUST/MUST NOT` invarijanti i bez orakla za verifikacijsku matricu.

---

## 4. ŠIZOFRENIJA IDENTITETA I CODING-PRISTRANOST (JEDNA JEZGRA / DVA PROFILA)

### [ID-01] Izravno proturječje direktiva: Redak 42 vs Addendum A3.1
* **Lokacija:** `HARNESS-PLAN.md:42` vs `HARNESS-PLAN.md:882`.
* **Problem:**
  - Redak 42: `## DIREKTIVA: coding profil = PRVORAZREDNI CILJ (bolji od Kilo Code / OpenHands / OpenCode)`.
  - Redak 882 (Addendum A3.1): `Korisnik: "nexus je AI asistent koji ima coding capabilities, a ne suprotno." Dosad je plan na vrhu imao: "coding = PRVORAZREDNI CILJ", a asistent kao nadgradnja. OVO JE OBRNUTO. Pravi identitet: NEXUS = OSOBNI AI ASISTENT s vrhunskim CODING capabilityjem.`
* **Posljedica:** Dokument ima dvije oprečne direktive na razmaku od 800 redaka. Svaki agent koji čita od vrha dobiva pogrešan kontekst prioriteta.
* **Popravak:** Ažurirati vrh `HARNESS-PLAN.md` i zamijeniti redak 42 kanonskom formulacijom: *"NEXUS: Osobni asistent s industrijskom coding jezgrom — Jedna jezgra, dva profila (AssistantProfile / CodingProfile)"*.

### [ID-02] Ograničenost Evidence-Checkera na `git-diff + exit-code` u tekstu plana
* **Lokacija:** `HARNESS-PLAN.md:57-58` vs `DESIGN-checker-tia-edit-agy.md:38-44`.
* **Problem:** U `HARNESS-PLAN.md:57` diferencijator se i dalje opisuje usko: *"checker ocjenjuje git-diff + realan exit-code, NE prozu workera"*.
* **Arhitektonska kolizija:** Iako je agy u `DESIGN-checker-tia-edit-agy.md` proširio checker generičkim ugovorima (`ContractChannelDeliveryReceipt`, `ContractCalendarMutation`, `ContractResearchGrounding`), tekst glavnog plana to ne reflektira i tretira checker kao isključivo programerski alat.
* **Popravak:** Ažurirati opis u `HARNESS-PLAN.md` tako da referencira generički `AcceptanceContract` i `EvidenceBundle`.

### [ID-03] Asimetrija u dubini dizajna: Coding (duboko) vs Asistent (plitko)
* **Lokacija:** `DESIGN-*.md` vs `HARNESS-PLAN.md S9.5, S9.6, S14.5`.
* **Nalaz:** 
  - Coding podsustav ima 4 detaljna dokumenta s konkretnim Go structovima, tipovima i RED testovima (`DESIGN-checker-tia-edit-agy.md`, `DESIGN-symedit-crypto-claude.md`, `DESIGN-memory-effectpath-kilo.md`, `DESIGN-S0-sandbox-codex.md`).
  - Asistentski podsustav (A4-G2 `PersonalProfile` 9.5, A4-G4 `ObligationStore` 9.6, A4-G5 `ControlPlane`, A4-G3 `Voice runtime` 13.3, Kanali 14.5) opisan je samo u crticama i odgođen pod `ODLUKA-PRD` bez ijednog Go ugovora ili structa.
* **Posljedica:** Ako se odmah krene u kodiranje, NEXUS će ispasti 100% coding harness, a asistent će ostati mrtvo slovo na papiru.
* **Popravak:** Napisati `DESIGN-assistant-profile.md` koji definira `ObligationStore`, `PersonalProfile` i `ChannelBridge` prije početka implementacije Faze M/O.

---

## 5. KRITIČNE RUPE U IZVRŠNOM PUTU I RUNTIME ARHITEKTURI

### [RUN-01] Effect-Path potpuno zaobilazi S6.2 SandboxBackend (Landlock/Seccomp)
* **Lokacija:** `DESIGN-memory-effectpath-kilo.md:130-144` vs `DESIGN-S0-sandbox-codex.md:1088-1120`.
* **Kritični Bug:**
  U `DESIGN-memory-effectpath-kilo.md`, `EffectPath.RunTool` je definiran ovako:
  ```go
  func (p EffectPath) RunTool(ctx, call ToolCall) (ToolResult, error) {
      d := p.pep.Decide(call)                 // 1) S6.0 policy (JEDINI)
      if d == DENY { return denied(call) }
      if err := p.mw.BeforeTool(call); err != nil { return p.mw.OnError(err) } // 2) S6.9
      out, err := p.proc.Spawn(call)          // 3) S1.2 process-tree (JEDINI) -> KRITIČNO!
      return p.retry.Classify(call, out, err) // 4) S7 retry/cancel (JEDINI)
  }
  ```
  - `EffectPath` poziva `p.proc.Spawn(call)` izravno na `S1.2 Process`!
  - `S6.2 SandboxBackend` (Landlock, seccomp BPF, probe/compile/launch/attest) **uopće se ne nalazi u structu `EffectPath` niti se poziva**!
  - Ako je `EffectPath` jedini autoritativni izvršni put za pokretanje alata, onda se **svaki shell/exec alat pokreće POTPUNO NESANDBOXIRAN** izravno kroz S1.2!
* **Popravak:** `EffectPath` mora sadržavati `sandbox sandbox.Boundary`. `S1.2 ProcessManager` mora služiti samo kao niska razina za praćenje procesnog stabla unutar `SandboxBackend.Launch`, a `EffectPath` mora zvati `p.sandbox.Launch(ctx, compiledPolicy)`.

### [RUN-02] Lažna pretpostavka da su svi alati OS procesi (`p.proc.Spawn`)
* **Lokacija:** `DESIGN-memory-effectpath-kilo.md:141`, `DESIGN-checker-tia-edit-agy.md:580`.
* **Problem:** 
  `EffectPath.RunTool` pretpostavlja da se svaki `ToolCall` izvršava kao `proc.Spawn(call)`.
  Međutim, ključni alati NEXUS-a su **in-process Go funkcije**:
  - `pkg/edit` (`ExactReplace`, `ASTSymRename`) — radi u memoriji procesa i piše preko `AtomicWriter`.
  - `read_file`, `grep_search`, `archmap_query` — in-process čitanja.
  - `vector_recall`, `memory_store` — in-process SQLite pozivi.
* **Posljedica:** `EffectPath` ne razlikuje in-process tool izvršavanje od out-of-process sandboxed izvršavanja.
* **Popravak:** Razdvojiti u `EffectPath`:
  ```go
  type ToolExecutor interface {
      Execute(ctx context.Context, call contracts.ToolCall) (contracts.ToolResult, error)
  }
  // InProcessExecutor (za edit/read/memory) vs SandboxedProcessExecutor (za bash/exec/python)
  ```

---

## 6. TABLIČNI PREGLED SVIH NALAZA I PREPORUČENIH AKCIJA

| ID | Kategorija | Ozbiljnost | Opis rupa / nelogičnosti | Potrebna akcija |
|---|---|---|---|---|
| **SEC-01** | Cross-Doc | Visoka | Rascjep u brojanju podsekcija (82 vs 81 vs 107) | Uskladiti sve dokumente na točno 107 podsekcija |
| **SEC-02** | Cross-Doc | Visoka | Podsekcije `12.3` i `13.4` izostavljene iz SECTION-MAP ledgera | Dodati 12.3 i 13.4 u ledger sa statusom |
| **SEC-03** | Cross-Doc | Srednja | 14 podsekcija gurnuto u "inline" bez statusne oznake | Dodijeliti eksplicitni status (✅/🔵/🟡/🟠/⬇) |
| **SEC-04** | Cross-Doc | Niska | Nove podsekcije iz addenduma nemaju statusne ikone u linijama | Označiti ih kao `🟠 ODLUKA-PRD` |
| **DAG-01** | Build-Order | **Kritična** | S4 (Alati) ovisi o S5 (Workspace), ali je u mapi stavljen prije S5 | Premjestiti S5 ispred S4 u Fazi L |
| **DAG-02** | Build-Order | Visoka | Core Loop (S3 u Fazi L) ne može raditi bez Prompt/Context (S8/S11 u Fazi M) | Izvući minimalne S8.1/S11.1 ugovore u Fazu K/L |
| **DAG-03** | Build-Order | Srednja | Kanali (S14.5 Faza O) ovise o Ekstenzijama (S17 Faza P) po P1.6 | Razdvojiti stdio kanale od plugin kanala |
| **DAG-04** | Build-Order | Srednja | Checker (Faza L) citira Council (Faza N) | Ograničiti v1 checker na determinističke orakle |
| **GATE-01** | Annex Gate | **Kritična** | Citiranje 7 fantomskih ugovora (P0.5-P0.13) kojih nema u Annexu A | Unijeti ugovore u Spec Annex A ili ih proglasiti Gapom |
| **GATE-02** | Annex Gate | Visoka | 65+ podsekcija nema niti jedan stvarni Annex A gate | Definirati nedostajuće minimalne ugovore |
| **ID-01** | Identitet | Visoka | Proturječje: "coding prvorazredni cilj" (r.42) vs A3.1 "asistent prvo" | Ispraviti vrh HARNESS-PLAN.md na "jedna jezgra, dva profila" |
| **ID-02** | Identitet | Srednja | Opis checkera u planu ograničen na git-diff umjesto generičkog | Ažurirati HARNESS-PLAN na AcceptanceContract |
| **ID-03** | Identitet | Visoka | Asimetrija dizajna: 4 coding dokumenta vs 0 za asistentske ugovore | Napisati ugovore za ObligationStore i PersonalProfile |
| **RUN-01** | Runtime | **Kritična** | `EffectPath` zaobilazi `SandboxBackend` i direktno zove `proc.Spawn` | Ugraditi `SandboxBackend.Launch` u `EffectPath` |
| **RUN-02** | Runtime | Visoka | `EffectPath` pretpostavlja da su svi alati procesi (lomi in-process edit/RAG) | Uvesti grananje InProcess vs SandboxedExecutor |

---

## ZAKLJUČAK RED TEAMA

Plan je napravio ogroman korak eliminacijom salvage iluzije i formalizacijom jezgrenih tipova. Međutim, **prelazak na kodiranje u ovom trenutku bio bi preuranjen** zbog **3 kritična blokera**:
1. **`RUN-01`**: Sigurnosni sandbox se uopće ne poziva u trenutnom kodu `EffectPath`-a.
2. **`DAG-01`**: Poredak gradnje S4 prije S5 stvara ciklus pri implementaciji edit-enginea.
3. **`GATE-01` & `ID-01`**: Dokumentacija pati od fantomskih gateova i proturječnih direktiva identiteta.

Nakon usklađivanja ove 3 stavke i presude PRD-a o opsegu P0, scaffold jezgre može započeti na potpuno čistim temeljima.

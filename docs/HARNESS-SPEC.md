# Anatomija idealnog AI harnessa — konsenzus dokument

**Verzija:** 0.9 — Annex A ugrađen + S0.5/S12.6 dodane u tijelo (codex mrtvi-wiring fix); matrica ODBLOKIRANA
**Status:** U PREGOVORU — lov-na-rupe skoro gotov. Redoslijed do produkcije:
**V1-V8 ✓ → Tier-3 Annex A ✓ → verifikacijska matrica (SLJEDEĆE, odblokirano).**
v0.7 KONSENZUS (3× AGREE 2026-09-01) bio temelj; v0.8 ga čisti.
(v0.4 strukturni konsenzus 2026-08-31 ostaje temelj; recenzenti codex/gpt-5.6-sol,
kilo/DeepSeek V4 Pro, agy/Gemini 3.7 Flash; moderator claude.)
**Cilj:** Definirati sve sekcije i podsekcije koje harness mora sadržavati, redoslijedom
kojim se programiraju, tako da i najslabiji model unutar njega daje najbolje moguće rješenje.
Za svaku podsekciju: top 3 postojeća open-source projekta s GitHuba i zašto.

---

## Protokol konsenzusa (za agente-recenzente)

1. Pročitaj cijeli dokument (uključivo CHANGELOG na dnu — tamo je razrješenje tvojih primjedbi iz prošle runde).
2. PREPIŠI svoj `REVIEW-<ime>.md` novim sadržajem.
3. Format: prva linija `VERDICT: AGREE v<verzija iz zaglavlja>` ili
   `VERDICT: CHANGES v<verzija iz zaglavlja>`; ako CHANGES,
   numerirane primjedbe s KONKRETNIM prijedlogom. Primjedbe iz prošle runde koje su
   riješene ne ponavljaj; ako je razrješenje krivo, reci zašto.
4. Konsenzus = svi recenzenti AGREE na istoj verziji.

**Kriteriji za top 3:**
- Top-3 = najbolji postojeći projekt *te specifične podsekcije* — dopuštene su
  biblioteke/komponente ako su najdublje na tom problemu; kategorija se označava:
  `(harness)`, `(komponenta)`, `(standard)`, `(benchmark)`, `(research)` — zadnje dvije
  su referentni radovi/datasetovi, ne produkcijski kandidati.
- Javni repo + licenca koja dopušta uvid i reuse. Zatvoreni proizvodi mogu biti SAMO
  referent s oznakom `†` i ne broje se u top-3. Source-available (ne-OSI) označava se `♦`.
- Aktivnost i dubina implementacije, ne zvjezdice. Arhiviran projekt ne smije u top-3.
- **Status verifikacije:** trenutne liste su RADNI top-3 (konsenzus po znanju agenata).
  Konačna promocija zahtijeva verifikacijsku matricu (URL, licenca, datum zadnjeg
  releasea, putanja do implementacije, kategorija) — to je sljedeća faza nakon
  strukturnog konsenzusa; ovdje se ne tvrdi da je provedena.

---

## Vodeće načelo

Harness je mozak; model je zamjenjivo gorivo. Cilj je mjerljiv, ne aksiom: **harness mora
povećati task-success, sigurnost i troškovnu učinkovitost za definirani skup modela,
dokazano uparenim end-to-end evalom** (isti task corpus, sa i bez sloja harnessa).

- **RUNTIME ≠ HARNESS ≠ SANDBOX (v0.7, Red Hat okvir):** tri odvojena vlasnika, tri failure
  moda. **Sandbox = SUBTRACTIVE** (deny-by-default, oduzima autoritet; njegov pad = agent
  napravio što NE smije). **Harness = ADDITIVE** (dodaje znanje/alate/kvalitetu; njegov pad =
  agent LOŠE napravio što je trebao). Ovo je podjela FAILURE-MODA, ne kod-vs-data kriterij.
  ISPRAVAK v0.8 (H1): additive NE znači data — provider ABI, petlja, eval-runner, prompt-
  assembler su additive mehanizmi koji su ipak KOD. Jedini kriterij za kod-vs-data je:
  **mehanizam/invariant/trust-boundary = kod; domensko znanje i politika unutar boundsa =
  data** (vidi odjeljak "Vlasništvo: kod vs data").

- **Capability floor modela:** tool calling, dovoljan kontekst, structured output; ispod
  toga harness kompenzira (constrained decoding, format edita, verifikacijske petlje), ali
  se granica mjeri, ne pretpostavlja.
- **Jezgra vs. capability profili (precizirano v0.3, prošireno v0.7):** Jezgra je
  product-neutralna: S0, S1, S2 (bez 2.4), S3 (bez 3.5 i 3.6), 4.1+4.6+4.7, S6 generički
  (6.0–6.5, 6.9), S7, S8 (bez 8.3), minimalni S11 (11.1+11.3), 9.1 (save/resume sesija —
  svaki stateful harness), minimalni S14 (14.1), S15 (uklj. 15.5), S17 packaging/update — i
  S16 kontinuirano (uklj. 16.6 minimalni checker).
- **COMPOSABLE FEATURE-FLAGS (v0.8, V2 3:0):** sposobnosti su neovisni flagovi, ne
  all-or-nothing bundlei. Svaki ima MJERLJIV OKIDAČ (v0.8, V6 3:0) — kad okidač nastupi,
  flag postaje OBVEZAN (uklj. njegov gate); inače je isključen. Flag smije samo SUZITI
  kernelov maksimum, nikad proširiti.

  | Flag | Sadržaj | Okidač (kad postaje obvezan) |
  |---|---|---|
  | `coding` | 3.5+TIA · 4.2–4.4 · S5 · 8.3 | zadatak mijenja izvorni kod repoa |
  | `vision` | 13.1 | ulaz sadrži sliku/screenshot |
  | `image-out` | 13.2 (+canvas) | zadatak traži generiranu sliku/dijagram |
  | `voice` | 13.3 | ulaz/izlaz je audio |
  | `video` | 13.4 | zadatak obrađuje video |
  | `documents` | 13.5 (mutacija) | mutira DOCX/PDF/forme kao deliverable (read=10.1) |
  | `vector-retrieval` | 10.1–10.3 · 10.6 · 10.7 | korpus ne stane u kontekst |
  | `graph-retrieval` | 10.4 | upiti traže odnose, ne samo sličnost |
  | `preload-cache` | 10.5 CAG | mali stabilni autorizirani korpus |
  | `memorija` | 9.2–9.4 | više od jedne sesije istog korisnika |
  | `multi-agent` | S12 (+council 16.6) | zadatak dijeli na delegirane podagente |
  | `full-UX` | 14.2–14.4 (SDK=14.2) | izlaže API/Web/IDE/SDK izvan CLI-ja |
  | `channels` | 14.5 + delivery(7.3) + identity(6.6/12.4) | udaljeni messaging kanal |
  | `automation` | 3.6 (admission=jezgra) | cron/webhook/watch/heartbeat trigger |
  | `extensions` | 6.8 · 11.5 · 11.6 · 17.1 | učita tuđi/self-authored skill/plugin |
  | `service` | 6.6–6.8 · S18 (+baseline 6.7 uvijek) | servira >1 korisnika / hosta tuđe podatke |
  | `distributed-artifact` | 6.8 · 17.2 · 17.3 | isporučuje binarni/self-update artefakt |
  | `destilacija` | 16.5 | izvozi trajektorije za trening |

  **Bundle-aliasi (shorthand, ne obveza):** `multimodal` = vision+image-out+voice+video+
  documents; `retrieval` = vector+graph+preload. Alias uključuje samo flagove čiji je okidač
  stvarno nastupio, ne sve redom.
  **Referentni signed-packovi (v0.8, V3 3:0 — NISU obvezni dio profila):** Reversa
  (`legacy-analysis`), Field-bridge (`field-support`), PDF-adapteri — domenski packovi koji se
  aktiviraju eksplicitnim capabilityjem i koriste postojeće gateove (11.5 / 15.3+17.2 / 13.5),
  ne univerzalni invarianti `coding`/`service`.
  Ovaj dokument time ostaje spec OPĆEG harnessa; `coding` je samo najrazrađeniji flag.

---

## S0. Ugovori, stanje i lifecycle (NOVO u v0.2)

Kanonske sheme prije prve linije petlje — sve ostalo ih konzumira.

### 0.1 Typed message / tool-call / tool-result / error schema
Interop-ugovori: MCP = agent↔alat (ovdje); A2A = agent↔agent (normativno u 12.5,
obvezan tek uz `multi-agent` profil).
- **MCP specification + SDKs** (standard) — interoperabilni tool/resource ugovor
- **OpenAI Agents SDK** (harness) — najmanji typed loop/tool/handoff ugovor
- **LangGraph** (harness) — state-graph ugovor s tipiziranim porukama

### 0.2 Run/turn/tool state machine s legalnim prijelazima
- **LangGraph** — eksplicitni graf stanja + checkpoint
- **OpenHands** — event stream kao izvor istine za stanje
- **Temporal** (komponenta) — durable state machine semantika

### 0.3 Capability negotiation po modelu/provideru
- **LiteLLM** — model info (streaming, tools, context limit) po provideru
- **PydanticAI** — model adapteri s deklariranim sposobnostima
- **Portkey AI Gateway** — capability-aware routing

### 0.4 Verzioniranje i migracija event/sesijskog formata
- **OpenHands** — verzioniran event stream
- **CloudEvents** (standard) — envelope s verzijom sheme
- **Temporal** (komponenta) — workflow versioning patterns

### 0.5 Capability-flag closure resolver (NOVO v0.9; ugovor Annex A P0.4)
Jezgra; jedini aktivator capability-flagova i njihovih gateova (composable flags iz Vodećeg
načela). Manifest s `requires/conflicts/provides/gates`, topološka aktivacija, fail-closed na
nepoznatu verziju/cycle; adapter se ne učita prije `ACTIVE`; bundle-alias ne aktivira flag bez
njegova okidača. Puni ugovor + RED test `test_every_flag_fails_when_one_gate_is_ablated`:
**Annex A P0.4**. Aktivira ga i `distributed-artifact` (P1.5) i `channels→extensions` (P1.6).
- **vlastiti resolver** (jezgra) — nema OSS ekvivalenta za capability-closure ove vrste
- Referentni obrasci: Cargo/Bazel feature-resolution (komponenta), OSGi capability model

---

## S1. Temelji (Foundation)

### 1.1 Konfiguracija (slojevita: default → global → projekt → env → CLI flag)
- **Codex CLI** — TOML config s `-c` override po ključu, profili
- **Aider** — yaml + .env + CLI, dokumentiran precedence
- **Continue** — config.yaml s JSON schemom, hot-reload

### 1.2 Cross-platform putanje i procesi
- **goose** — Rust, čist keyring/paths sloj preko OS-a
- **Codex CLI** — Rust, identično ponašanje macOS/Linux/Windows
- **Aider** — Python cross-platform PTY i path sloj

### 1.3 Strukturirano logiranje od prvog dana
- **OpenTelemetry SDK/spec** (standard) — primarni model za spanove/logove/metrike
- **structlog** (Python) / **tracing** (Rust) (komponente) — strukturni log samog harnessa
- **OpenHands** (harness) — event stream kao centralna istina
- Zahtjevi: korelacijski ID-evi `run/turn/tool_call/attempt`, causal parent, monotoni
  sequence, schema version, redakcija PRIJE pohrane.

---

## S2. Provider sloj

### 2.1 Multi-provider apstrakcija
- **LiteLLM** — de facto standard, 100+ providera kroz jedan format
- **PydanticAI** — čisti adapteri s capability negotiation i error normalizacijom
- **Vercel AI SDK** (komponenta, TypeScript) — de facto TS standard: unified provider
  ugovor, structured output, streaming, tool-loop (zatvara TS-prazninu dokumenta)
- Runners-up: Continue (proizvodni potrošač, ne dublja apstrakcija), Rig (Rust typed)

### 2.2 Fallback / retry / rate-limit / load balancing
- **LiteLLM** — router s fallback lancima, cooldown
- **Portkey AI Gateway** — retries, fallbacks, timeouts, load balancing
- **Kong AI Gateway** (komponenta) — retry+fallback (v3.10+), 5 load-balancing strategija,
  desetljeće L7 proxy dubine; Apache-2.0, verificirano webfetchom (kilo, runda 2).
  Runners-up za matricu: Envoy AI Gateway, TensorZero. Napomena: dio naprednih AI
  značajki je komercijalni Konnect — matrica potvrđuje OSS granicu.

### 2.3 Tool-call parsing i structured output (kritično za slabe modele!)
- **Instructor** (komponenta) — Pydantic validacija + automatski re-ask
- **Outlines** (komponenta) — constrained decoding, gramatika garantira valjan JSON
- **BAML** (komponenta) — schema-first DSL, parser tolerira šum u outputu

### 2.4 Lokalni modeli (offline put)
- **Ollama** — standard za lokalno serviranje, kompatibilan API
- **llama.cpp** — temelj svega lokalnog, server mode
- **vLLM** — produkcijsko serviranje kad ima GPU-a
- Zahtjev (obsidian, hardware-aware fit): preflight provjeri RAM/VRAM/quantization/AVX-CUDA;
  odbij model koji ne stane (ne OOM-crash), predloži lakšu kvantizaciju. Mjeri na hostu, ne iz
  imena. Runner-upovi za matricu: Odysseus, Colibri. (Distinktno od provider-data ugovora P2.5.)

### 2.5 Token accounting i cost tracking
- **LiteLLM** — cijene po modelu ugrađene, spend log
- **Aider** — prikaz cijene po sesiji u realnom vremenu
- **Helicone** — proxy-based accounting bez izmjene koda

### 2.6 Model routing (pravi model za pravi zadatak) (PREMJEŠTENO iz 12.3 u v0.8, V1 2:1)
Kanonski vlasnik OVDJE (jezgra) — routing je provider/cost odluka koja vrijedi i za jednog
agenta, ne multi-agent concern. Bazični barbell/fallback (2.2) je jezgra; NAUČENI semantički
router je opcionalna implementacija iza istog jezgrenog ABI-ja. (12.3 = multi-agent potrošač
s cross-refom ovamo.)
- **RouteLLM** — naučeni router jak/slab model, dokazana ušteda
- **semantic-router** — brzo semantičko usmjeravanje
- **LiteLLM router** — pravila + fallback u praksi
- Runner-up: ClawRouter (lokalni, 15 dim) — matrica

---

## S3. Agentska petlja (Core Loop)

### 3.1 Plan-act-observe petlja
- **OpenHands** — eksplicitan event-driven loop (action/observation parovi)
- **smolagents** — CodeAct pristup: model piše kod umjesto JSON tool-callova
- **SWE-agent** — ACI dizajn — snažan javni dokaz (SWE-bench) da diže uspjeh slabijih modela
- † Claude Code — zatvoreni referent produkcijske petlje

### 3.2 Streaming i inkrementalni prikaz
- **Gemini CLI** — open-source referentna stream implementacija
- **Codex CLI** — Rust stream handling, robustan na prekide
- **OpenHands** — streaming kroz event stream

### 3.3 Prekid, cancel i turn management
- **goose** — čist cancel semantics u Rust petlji
- **OpenHands** — pause/resume agenta kroz event stream
- **Codex CLI** — prekid mid-turn bez gubitka stanja

### 3.4 Error recovery unutar petlje (alat pao ≠ petlja pala)
- **OpenHands** — observation nosi error, model ga vidi i korigira se
- **LangGraph** — checkpointing: nastavak od zadnjeg dobrog stanja
- **SWE-agent** — poruke o grešci formatirane da ih model razumije

### 3.5 Deterministička verifikacija u petlji (lint/compile feedback) (NOVO)
Najveća slabost malih modela su sintaksne greške i tipfeleri — harness ih hvata
deterministički i vraća modelu u ISTOM turnu, prije završetka koraka.
- **Aider** — automatski linter run nakon edita + re-prompt za ispravak
- **SWE-agent** — lint/test hookovi prije prihvaćanja edita
- Treći: **UNCLEAR** — traži se harness s punom check-and-reprompt petljom (pokreni nakon
  edita → dijagnostika modelu u istom turnu → odbij završetak dok ne prođe); ast-grep je
  detektor, ne petlja — premješten isključivo u 4.4.
- Dopuna (v0.7, NEXUS TIA): **test-impact analiza** — coverage/dependency-graf × git diff
  pokreće samo pogođene testove uz full-suite fallback kad je mapiranje zastarjelo.
  Kandidati: NEXUS `tia.py` (salvage), pytest-testmon, Bazel affected-targets. `coding` profil.

### 3.6 Run trigger plane i admission (NOVO v0.7)
Capability-profil `automation`. S3 definira ponašanje NAKON početka runa; ova podsekcija
definira TKO/ŠTO smije pokrenuti run. Admission i envelope validator su jezgra.
Manual/schedule/webhook/watch/heartbeat stvaraju isti S0 run-envelope i PRIJE petlje prolaze
dedup, idempotency key, policy, budget i audit — autonoman trigger ima iste granice kao ručni.
- **vlastiti trigger-admission adapteri** (uz S0) + salvage NEXUS `schedule.py`/`watch.py`
- **Temporal Schedules** (komponenta)
- **APScheduler** (komponenta)

---

## S4. Tool sustav

### 4.1 Tool registry s typed schemama
- **FastMCP / MCP SDK** (standard) — type-safe definicija alata dekoratorima
- **smolagents** — alat = Python funkcija s type hintovima, auto-schema
- **OpenAI Agents SDK** — typed function tools s validacijom

### 4.2 Shell / izvršavanje koda
- **OpenHands** — runtime s bash sesijom koja drži stanje
- **Codex CLI** — sandboxirani exec s policy slojem
- **gptme** — minimalan ali potpun shell tool

### 4.3 Uređivanje datoteka (format edita presudan za slabe modele!)
- **Aider** — benchmarkirani edit formati (diff, whole, udiff) po modelu — snažan
  javni dokaz (Aider edit-format benchmark) da format edita diže uspješnost slabog modela
- **Cline** — diff-based edit s checkpointom po koraku
- **Codex CLI** — patch-apply pristup s auditabilnim tokom

### 4.4 Aktivna pretraga i navigacija koda (alati koje model poziva)
- **ripgrep** (komponenta) — industrijski standard tekstualne/regex pretrage
- **ast-grep** (komponenta) — strukturna AST pretraga
- **Serena** — LSP-based simbolička navigacija kao MCP server
- (Ambijentalna reprezentacija repoa je 8.3 — namjerno razdvojeno.)

### 4.5 Web i browser automatizacija
- **browser-use** — DOM-based agent kontrola browsera
- **Playwright MCP** — accessibility-tree pristup
- **Skyvern** — vision + DOM hibrid za robusnost
- Runner-up (obsidian): Camoufox (stealth Firefox, accessibility-tree izlaz) — SAMO uz
  eksplicitnu ToS/robots/legal policy jezgre (anti-bot bypass NIJE sigurnosno neutralan; V-odbijeno kao core cilj)

### 4.6 MCP klijent (vanjski alati bez mijenjanja jezgre)
- **goose** — extension sustav nativno na MCP-u
- **Cline** — MCP marketplace pristup
- **Codex CLI** — MCP klijent u Rust jezgri
- Runner-up (obsidian): AXI "wrap existing CLI" (gh/exa → kompaktan TOON output) — lakša
  alternativa punom MCP serveru za postojeće CLI-jeve; auth/transport ugovor iz P1.3 vrijedi jednako.

### 4.7 Tool exposure budget i scoped discovery (NOVO v0.7)
Jezgra. Registry (4.1) registrira alate, ali ne ograničava koliko schema ulazi PO TURNU.
Kernel izlaže najmanji capability-scoped tool-view, sheme dohvaća lazy, mjeri schema-token
budget i selection miss-rate. Hipoteza (za matricu, iz vault bilješke — NEprovjereno): Pi
(≈4 alata, štur prompt, mala petlja) navodno pobjeđuje veće harnesse na Terminal-Bench 2.0
— princip "manje alata + scoped visibility diže selekciju slabog modela", ne kao dokazan broj.
- **vlastiti `ToolView`** nad 4.1
- **pi-mono** — minimal-tool referent (session/compaction claim je u 8.2, ne ovdje)
- **MCP `tools/list` + paginacija** — discovery primitiv

---

## S5. Workspace, VCS i checkpoint/rollback (NOVO u v0.2)

Za coding harness jezgrena sposobnost — sigurnosna mreža za slabe modele.

### 5.1 Checkpoint i rollback po koraku (undo)
- **Cline** — checkpoint/restore diff-a po koraku
- **Aider** — auto-commit po turnu + `/undo`
- **LangGraph** — checkpoint mehanizam primijenjen na stanje

### 5.2 Izolacija radnog prostora (worktree/container po zadatku ili subagentu)
- **OpenHands** — workspace izolacija runtimeom
- **SWE-agent** — izolirani task/workspace interface
- **OpenAI Agents SDK Sandbox Agents** (harness, beta) — workspace manifesti, odvojene
  sandbox sesije, snapshots/resume/cleanup; beta — matrica testira symlink/traversal
  granicu i cleanup-failure put prije promocije

### 5.3 Dirty-tree ownership, atomic write, conflict detection, provenance artefakata
- **Aider** — git-aware edit/commit tijek (tuđi dirty rad se ne gazi)
- **Codex CLI** — atomski patch-apply s odbijanjem konflikta
- **Cline** — razlikovanje generiranog vs. korisničkog sadržaja u checkpointima
- Zahtjev (obsidian, sandbox-neutralni atestor): provenance artefakta treba dolaziti iz NIŽEG
  trust sloja (sandbox koji je vidio stvarni argv/fs/network učinak), ne iz runtimea koji sam
  sebe atestira. Kandidat izvoz: W3C PROV-O; veže se na 6.2 (sandbox) i 15.3 (audit).

---

## S6. Sigurnost i dozvole

Policy i sandbox OBLIKUJU tool executor od prvog side-effecting alata — ne dolaze poslije.
Primarna obrana od injectiona je arhitektura (odvajanje instrukcija od podataka, least
privilege, argument-level policy, approval za visok rizik); detektori su defense-in-depth.

### 6.0 Trust classification i policy enforcement point
- **Codex CLI** — approval politike (untrusted/on-request/on-failure/never)
- **OpenHands** — security analyzer nad akcijama prije izvršenja
- **Open Policy Agent (OPA)** (komponenta) — kontekstualni policy decision point
  ("smije li subjekt X izvršiti alat Y nad resursom Z s argumentima A"); executor
  provodi odluku fail-closed. (LlamaFirewall je detekcijski sloj — samo u 6.5.)

### 6.1 Permission gating (ask/allow/deny po alatu i argumentu)
- **Codex CLI** — approval + sandbox eskalacija po komandi
- **goose** — per-extension dozvole
- **gptme** — jasne potvrde rizičnih naredbi
- † Claude Code — referent za allowlist pravila po argumentu

### 6.2 Sandbox izvršavanja (fs + procesi)
- **Codex CLI** — OS-native: Seatbelt (macOS) + Landlock/seccomp (Linux)
- **OpenHands** — container izolacija cijelog runtimea
- **gVisor** (komponenta) — kernel-level sandbox; (E2B/Firecracker kao microVM opcija)
- Dopuna (v0.7, NEXUS GORTEX): opcionalna **out-of-process membrana** — opasni I/O
  (diff/procesi/Landlock/MCP) u malom kompajliranom helperu iza autentificiranog verzioniranog
  IPC-a, bez fallbacka na nezaštićen in-process put. Kandidati: NEXUS `gortex/` Go (salvage),
  Codex `linux-sandbox` Rust obrazac, OpenHands runtime. Cross-ref 4.2. Unified sandbox
  runner-up: OpenSandbox/AIO (Browser+Shell+File+MCP+VSCode u jednom containeru) — matrica.

### 6.3 Network egress kontrola
- **Codex CLI** — sandbox default bez mreže, eksplicitan opt-in
- **microsandbox** — microVM s kontroliranim izlazom
- **OpenHands** — network policy na container razini

### 6.4 Secret handling i redakcija
- **goose** — keyring integracija, secrets izvan configa
- **detect-secrets** (komponenta) — pre-send scan
- (PII redakcija → kanonski vlasnik je 6.7 Presidio; ovdje samo secret/keyring, ne dupliraj)

### 6.5 Prompt injection obrana (untrusted sadržaj ≠ instrukcije)
- **LlamaFirewall (PurpleLlama)** — aktivno održavan detekcijski sloj
- **NeMo Guardrails** — programabilne ograde; poznate FP/FN granice navesti
- **LLM Guard** — input/output skeneri
- Mjerenje obrana: **AgentDojo** (eval suite za injection). Rebuff IZBAČEN (arhiviran).
- Dopuna (v0.7, NEXUS canary): **exfiltration tripwire** — per-install sintetički token;
  detekcija tokena u izlazu dokazuje MOGUĆ širi boundary breach, pa reakcija mora biti
  FAIL-CLOSED: blokiraj cijelu isporuku (ne samo ukloni token), audit, alert, rotacija;
  nastavak tek uz eksplicitnu ljudsku odluku. Rotira se, NIKAD ne zamjenjuje egress/secret
  policy (defense-in-depth). Negativni test pripada 16.3. Kandidati: NEXUS `canary.py`
  (salvage), Canarytokens, garak/promptfoo exfil testovi.

### 6.6 AuthN/AuthZ i tenant/workspace izolacija (NOVO)
- **LiteLLM proxy** — ključevi, timovi, budgeti
- **Dify** — multi-tenant workspace model
- **Open WebUI** — RBAC nad korisnicima/grupama

### 6.7 Data governance: retention, brisanje, export, PII (NOVO)
**Dva sloja (v0.8, V8 3:0):** (a) **BASELINE — jezgra, svaki persistentni store** (session/
memorija/log/transcript): klasifikacija, retention default, export, hard-delete propagacija.
Aktivira ga persistence, NE `service` — lokalni single-user harness ipak persistira.
(b) **SERVICE dodatak** (`service` flag): DSAR/legal-hold/tenant-policy/rezidencija.
- **Presidio** (komponenta) — PII detekcija/anonimizacija (kanonski PII vlasnik)
- **Langfuse** — retention i masking politika nad trace-ovima
- Baseline treći: **vlastiti SQLite retention/purge engine** (TTL + hard-delete po isteku
  politike) — jer OSS transcript-governance ne postoji (Fides arhiviran 28.8.2026.,
  OpenMetadata governira data-assete ne transcript). Service treći: UNCLEAR.

### 6.8 Supply-chain: plugin provenance i signing (NOVO)
- **Sigstore/cosign** (komponenta) — potpisivanje artefakata
- **OSV-Scanner** (komponenta) — ranjivosti ovisnosti
- **in-toto** (standard) — provenance lanca isporuke

### 6.9 Non-bypassable lifecycle enforcement (NOVO v0.7)
Jezgra. Svaki run i side-effect prolazi fiksni redoslijed success-faza `before_run →
before_tool → after_tool → before_deliver`; jezgreni policy ide PRVI i ZADNJI, a plugin hook
smije samo SUZITI odluku — nikad proširiti ovlast ni preskočiti redakciju/audit. `on_error`
NIJE zadnji član niza nego GRANA iz SVAKE faze: greška u bilo kojoj fazi skače na on_error s
obveznim causal-errorom + auditom i BEZ izvršavanja kasnijih success-hookova. Profil smije
registrirati hook, ne promijeniti redoslijed.
- **vlastiti typed middleware chain** + salvage NEXUS `permissions.py`/`hooks.py`
- † Claude Code — referent (PreToolUse/PostToolUse hooks rade i u bypass modu)
- **DeepSeek Harness / Cordis** hook sustav — iza conformance gatea

---

## S7. Pouzdanost i durable execution (NOVO u v0.2)

Prije je bilo rasuto po 2.2/3.3/3.4/10.2 — sad jedna sekcija, gradi se odmah nakon petlje.

### 7.1 Timeout/cancellation propagacija + retryable-vs-terminal taksonomija grešaka
- **Temporal** (komponenta) — referentna retry/timeout semantika
- **LangGraph** — cancel/timeout na razini grafa
- **tenacity** (komponenta) — deklarativni retry s klasifikacijom

### 7.2 Idempotency, bounded budgets, backpressure, concurrency nad dijeljenim stanjem
- **Temporal** — idempotency ključevi, task queue lease
- **Restate** — lagani durable runtime
- **Dapr Workflows** — durable orkestracija bez teškog clustera
- Zahtjev (obsidian, cost-checkpoint u grafu): budget provjera PRIJE skupe operacije unutar
  petlje/grafa, ne samo globalni cap na kraju — nadopuna ResourceBudget ugovoru (P2.2).

### 7.3 Crash recovery i durable checkpoints
- **LangGraph** — checkpoint/resume iz spremljenog stanja
- **Temporal** — replay iz event historije
- **OpenHands** — rekonstrukcija zabilježenog stanja iz event streama
  (NE tvrditi deterministički replay bez snimljenih model/tool rezultata i zamrznute okoline)
- Dopuna (v0.7, N9 durable delivery — kanonski owner ovdje, cross-ref 3.4 i 14.5):
  `run done` ≠ `result delivered` su DVA trajna stanja; transactional outbox + idempotent
  delivery + receipt sprječavaju gubitak odgovora i ponovljeni side-effect nakon gateway
  crasha. Obvezno kad se uključe `channels`/`service`/udaljeni `full-UX`. Kandidati:
  vlastiti outbox, Temporal, Restate; salvage NEXUS `queue.py`.

---

## S8. Kontekst management

### 8.1 Window budžet i truncation strategija
- **Aider** — repo map skaliran po dostupnom budžetu
- **letta (MemGPT)** — eksplicitna hijerarhija core/recall/archival
- **OpenHands** — budžetiranje event streama

### 8.2 Kompakcija / summarizacija povijesti
- **letta (MemGPT)** — izvorni rad na self-editing memoriji
- **OpenHands** — condenser komponenta (više strategija)
- **pi-mono** (harness/komponenta, TypeScript) — JSONL session tree, compaction i
  branch-summary semantika (HIPOTEZA za matricu; kanonski vlasnik pi-mono claima je OVDJE,
  4.7 ga samo cross-referencira kao minimal-tool referent). LangGraph runner-up.

### 8.3 Ambijentalna reprezentacija codebasea (repo map/index u promptu)
- **Aider** — tree-sitter + graph ranking, dokazano na benchmarku
- **Repomix** — pakiranje stabla repoa u strukturirani kontekst
- **Continue** — codebase index s lokalnim embeddinzima
- Runners-up (obsidian): Understand-Anything/Scan (interaktivni KG codebasea >200k linija),
  Graphify (pre-indexiran KG, tvrdnja ~71× manje tokena — za matricu)

### 8.4 Prompt/inference cache i reuse
Dva odvojena sloja (H12): (a) **API sloj** — propagacija `cache-control` breakpointa, bez
kontrole KV memorije; (b) **lokalni/self-hosted sloj** — prefix/KV caching na inference razini.
- (a) **LiteLLM** — cache-control propagacija po provideru
- (b) **vLLM** — prefix caching; **SGLang** — RadixAttention (SAMO lokalno serviranje/cluster)
- Dobitak se MJERI (hit-rate, TTFT, cijena) po provideru/modelu — nema univerzalne brojke.
- Cache key MORA nositi identity/policy/content hash (backlog codex#19: tenant/principal/
  sensitivity granica — bez toga cache curi tuđi/zastarjeli sadržaj).

### 8.5 Observation pruning: kraćenje izlaza alata NA IZVORU (NOVO)
Tisuće linija stdout-a guše slab model prije nego kompakcija povijesti išta stigne.
- **RTK (Rust Token Killer)** (komponenta, obsidian) — proxy koji filtrira izlaz 100+ CLI
  (npm test/git diff/docker) prije prompta; filter-pa-komprimiraj po alatu
- **Headroom** (komponenta, obsidian) — multi-tier compression + `learn` iz neuspjelih sesija
- **SWE-agent** — ACI pager (navigacija po stranicama umjesto dumpa)
- Runners-up: Aider (stdout head+tail+error-linije), OpenHands (truncation filtri)

---

## S9. Memorija (preko sesija) — CAPABILITY PROFIL

### 9.1 Sesije: save/resume/branch
**save/resume je JEZGRA** (svaki stateful harness; gradi se u P3, naslanja se na 7.3);
branch i user-visible session management su profil.
- **goose** — session storage s nastavkom
- **OpenHands** — event stream omogućuje rekonstrukciju sesije
- **Codex CLI** — resume sesija iz zapisa
- Runner-up (obsidian): Stash (shared team-memory — transcript+tool-callovi u agent-native
  virtualni FS, semantička pretraga preko sesija; `multi-agent` handoff)

### 9.2 Perzistentna memorija (činjenice o korisniku/projektu)
- **letta (MemGPT)** — agent sam uređuje svoju memoriju
- **mem0** — memory layer s ekstrakcijom činjenica + graf
- **zep** — temporal knowledge graph
- Obavezno: memory write policy, forget/delete put, untrusted-context labeling.
- Runner-up (obsidian): OpenHuman Memory Tree + TokenJuice — eksplicitan napad na cold-start
  memorijski trošak (scored markdown stablo + SuperContext preload + kompresija).

### 9.3 Učenje iz iskustva (greške se ne ponavljaju)
Produkcijski OSS protuprimjer postoji od 2/2026: Hermes Agent trajno sprema memoriju i
iz iskustva izrađuje proceduralne SKILL dokumente. PAŽNJA (trust boundary): odobrenje
korisnika za skill-write je KONFIGURABILNA ograda i zadano je ISKLJUČENO
(`skills.write_approval: false` default) — harness koji ovo preuzima mora ogradu
uključiti po defaultu. Polje još nema standardiziran dokaz da samodogradnja pouzdano
smanjuje ponavljanje grešaka.
- **Hermes Agent (Nous Research)** (harness) — verificirano: perzistentna datotečna
  memorija, session search, memory-provider plugini, agent-authored skills
- **cognee** — ECL destilacija znanja (napomena: preklapa se s 10.4 — matrica odlučuje)
- **Reflexion** (research) — verbalna samorefleksija; **Voyager** (research) runner-up

### 9.4 Memorijska konsolidacija i decay (NOVO v0.7, iz NEXUS EXTRAS 5)
Capability-profil `memorija`; write/delete/provenance gateovi su jezgra. Offline (sleep-time)
konsolidacija epizoda → gistova + knowledge-graf; decay mijenja RANG, ne postojanje (lossless
hot/warm/cold, original u dohvatljivoj arhivi); ADD/NOOP/SUPERSEDE razrješenje kontradikcija
na read; verzionirani write i reversible forget/tombstone; temporalni upiti.
- **salvage NEXUS** `dream.py`+`sleeptime.py`+`audn.py`+`memgit.py`+`recall.py`+`memassoc.py`
- **letta (MemGPT)** — sleep-time engine
- **mem0 / zep** — konsolidacija + temporal graf

---

## S10. Retrieval (RAG) — CAPABILITY PROFIL

### 10.1 Ingestion i parsiranje dokumenata
- **RAGFlow (deepdoc)** — najdublje parsiranje layouta (tablice, forme)
- **docling** — moderan document converter
- **unstructured** — širina formata
- Runners-up (obsidian): Marker (PDF→Markdown za RAG), MinerU (layout+formula extract),
  OCRmyPDF/Surya (OCR sloj)
- Zahtjev (obsidian, SurfSense): "static RAG = table-stakes, moat su LIVE konektori" — jedan
  typed REST endpoint po izvoru + isti izvor izložen kao MCP tool (svjež dohvat, ne samo indeks).

### 10.2 Chunking strategije
Zahtjev (v0.8, iz vault): usporediti semantic (cosine-drop), parent-child (child embed +
parent kontekst) i contextual (prepend sažetka — rješava "it"/"the company") istim corpusom.
- **LlamaIndex** — najviše strategija (semantic, sentence, hierarchical, parent-child)
- **chonkie** — specijaliziran, brz, samo chunking
- **RAGFlow** — template-based chunking po tipu dokumenta

### 10.3 Vektorska + hibridna pretraga s rerankingom
Zahtjev (v0.8): eksplicitan **cross-encoder rerank stage** + mjerenje recall/latency tradeoffa
(bez neprovjerene "+17%" brojke).
- **Qdrant** — engine dubina: dense+sparse+hybrid fusion
- **Haystack** — pipeline s BM25+dense+rerank komponentama
- **sentence-transformers / BGE** (komponenta) — cross-encoder rerank
- Runners-up (obsidian): LanceDB / sqlite-rag (file-based hibrid bez servera — za "file-based
  v1" pravilo, međukorak prema Qdrantu); query-rewrite prije retrievala (podiže ceiling)

### 10.4 GraphRAG (odnosi, ne samo sličnost)
- **Microsoft GraphRAG** — referentna implementacija
- **LightRAG** — jeftinija i brža alternativa
- **cognee** — graf + vektor kombinacija

### 10.5 CAG (cache-augmented generation)
- **Decision rule (v0.7, iz vault RAG-vs-CAG):** mali/stabilni/autoriziran skup znanja →
  preload u KV/prefix stanje (CAG); velik/dinamičan korpus → RAG; hibrid se bira uparenim
  corpus-evalom. Time podsekcija više nije prazna na razini ODLUKE.
- Top-3 IMPLEMENTACIJA i dalje **UNCLEAR** — prefix-cache infrastruktura (vLLM/SGLang) je u
  8.4 jer sama ne bira/autorizira/osvježava znanje; treba end-to-end OSS kandidat.

### 10.6 Retrieval authorization i tenant filteri (NOVO)
- **Qdrant** — payload filter prije rerankinga
- **OpenSearch** — document-level security
- **Vespa** — filtriranje u ranking pipelineu

### 10.7 Retrieval quality, freshness i provenance gate (NOVO v0.7)
Flag `vector-retrieval`; ACL/provenance enforcement ostaje jezgra. RAG-ekvivalent 3.5/
16.2: odgovor se NE smatra grounded ako retrieval nije izmjeren — golden queries, Recall@k/
MRR/nDCG, freshness SLA, citat do izvornog chunka, detekcija zastarjelog indeksa.
- **Ragas** (komponenta) — RAG metrike
- **BEIR** (benchmark) — retrieval benchmark
- **Phoenix** — retrieval evaluatori
- (v0.8: konkretni chunking/rerank zahtjevi premješteni U 10.2/10.3 gdje im je vlasnik;
  10.7 drži samo eval-metrike/freshness/provenance. Ingestion 10.1 = READ-ONLY; dokument-
  MUTACIJA je u 13.5 `documents`, NE pod flagom `vector-retrieval`.)
- V7 IZGLASANO (3:0 B): 10.7 ostaje ZASEBAN retrieval-grounding owner; generičko CI
  izvršenje/ratchet je 16.2 (cross-ref, ne fold).

---

## S11. Prompt i instrukcijski sloj

Minimalni 11.1 + 11.3 grade se u fazi P1 (prije prve petlje) — vidi redoslijed gradnje.

### 11.1 System prompt assembly: uloga, pravila, format, precedence, provenance
- **Aider** — prompti brušeni benchmarkom po modelu
- **SWE-agent** — dokaz da ACI/prompt dizajn diže uspjeh više od izbora modela
- **OpenClaw** — eksplicitno sklapanje ponašanja iz workspace/config datoteka
  (SOUL.md/AGENTS.md/TOOLS.md) — najbliži match za precedence/provenance
- Runner-up: Open Interpreter (profili po modelu)

### 11.2 Proširive upute: skills/commands/recipes
- **Hermes Agent** (harness) — perzistentni, agent-authored i plugin-provided skills,
  instalacija/aktivacija, razlika proceduralno znanje vs. memorija
- **Agent Skills standard (anthropics/skills, SKILL.md spec)** (standard) — de facto OSS
  standard koji usvajaju crush, OpenCode, goose (Fabric izbačen: biblioteka prompt
  patterna, ne mehanizam ekstenzije)
- **Codex CLI** — skills mehanizam
- Runners-up: goose recipes, OpenClaw
- † Claude Code — referent (SKILL.md + progressive disclosure)
- Dopuna (v0.7, NEXUS Reversa — premješteno iz 17.1 jer je DATA ne izvršni plugin):
  **legacy reverse-engineering skill-pack** — iz nepoznatog repoa gradi dokazivu
  specifikaciju (SDD/PRD) + migracijski plan PRIJE mutacije; zabranu legacy edita
  (`allowLegacyEdits:false`) provodi kernel 6.1/6.9, ne skill; lifecycle kroz 11.5.
  `coding`+`extensions` profil, signed data. Kandidati: NEXUS `reversa.py` + 40 skillova
  (salvage), OpenRewrite recipes, vlastiti spec-generation pack.

### 11.3 Projektne instrukcije (AGENTS.md konvencija)
- **Codex CLI** — AGENTS.md standard
- **OpenCode** — AGENTS.md podrška
- **Cline** — .clinerules
- † Claude Code — referent (CLAUDE.md hijerarhija)

### 11.4 Automatska optimizacija prompta
- **DSPy** — prompt kao program, optimizatori
- **TextGrad** — gradijenti kroz tekst
- **promptfoo** — A/B eval promptova

### 11.5 Skill lifecycle i governance (NOVO v0.7)
Capability-profil `extensions`; scan/signature/activation/rollback su jezgra, SKILL.md i
rubrike su data. Skill prolazi `install → scan → pin → activate → measure → update →
rollback → archive` uz provenance, permission-delta i eval-gate prije povjerenja — vrijedi i
za marketplace i za agent-authored (self-authoring inertan do gatea, nikad autonomna kernel
mutacija). Pre-install audit gleda injection/exfil/opasne tool-chain kombinacije/install skripte.
- **vlastiti lifecycle** (uz 6.8/17.1) + salvage NEXUS Skill Forge (score→doctor→merge→scout)
  samo u izoliranoj eval grani
- **Cisco AI Defense Skill Scanner** (komponenta)
- **ZIRAN** (komponenta) / skills.sh audit katalog (standard); **Depx** (dep/tool-chain audit)
- Zahtjev (obsidian, source-grounded skill synthesis): agent-authored/generirani skill MORA
  nositi source manifest + citate + verziju izvora + expiry/freshness + evalove; NotebookLM/
  Hermes generiranje nije dokaz točnosti. `write_approval` default ON, auto-install default OFF.

### 11.6 Deklarativni Role → Agent → Skill/Pack ugovor (NOVO v0.7)
Manifest interpreter je jezgra; sadržaj je `extensions`/`multi-agent` data. Role, agent
template, team i skill su VALIDIRANI PODATKOVNI MANIFESTI — nova rola = nova mapa, nula izmjene
jezgre (NEXUS lekcija). Kernel jedini rješava precedence, capabilityje, budget i output
contract; core NE zna nazive domena/rola.
- **vlastiti schema-first manifest** po NEXUS obrascu (ROLE.md/AGENT.md/team.yaml/SKILL.md)
- **CrewAI** — YAML role
- **OpenClaw / MetaGPT** — workspace-config / roles-as-data

---

## S12. Orkestracija i multi-agent — CAPABILITY PROFIL

### 12.1 Subagenti i delegacija (izolacija konteksta)
- **LangGraph** — supervisor pattern
- **CrewAI** — role-based delegacija
- **Microsoft Agent Framework (MAF) 1.0** (harness) — izravni nasljednik
  AutoGena+Semantic Kernela (merge 4/2026), aktualni multi-agent workflow primitivi
- Magentic-One (research/legacy) — Task-/Progress-Ledger obrazac; konsolidiran pod
  Microsoftov agent stack — matrica potvrđuje živi li zasebno
- † Claude Code — referent (Task tool s tipovima subagenata)

### 12.2 Workflow/DAG s determinističkim tokom
- **LangGraph** — graf stanja s checkpointima
- **Temporal** (komponenta) — durable execution standard
- **Google ADK** (harness) — source-level workflow runtime: deterministički + model-led
  koraci u istom grafu, paralelno izvođenje, resume, ugrađeni evali
- Runners-up: MAF, Mastra (TS; `ee/` granica za matricu), Marvin (živi nasljednik
  arhiviranog ControlFlowa), Agno, Dify Workflow (neprovjeren suspend/resume)

### 12.3 Model routing (POTROŠAČ — kanonski vlasnik je 2.6 od v0.8)
Premješteno u 2.6 (jezgra). Ovdje ostaje samo multi-agent potrošač: koji podagent dobiva
koji model. Vidi 2.6.

### 12.4 Human-in-the-loop točke
- **LangGraph** — interrupt/approve primitivi
- **OpenAI Agents SDK** — serializable HITL interruptions
- **Microsoft Agent Framework (MAF)** — graph workflows + checkpointing + HITL
  (zamjenjuje stale AutoGen unos nakon službene sukcesije)

### 12.5 Agent-to-agent interoperabilnost i discovery (A2A) (NOVO u v0.5)
Materijalan uz `multi-agent` profil; nije obveza lokalnog single-agent harnessa.
Normativno referencira sheme iz 0.1 i capability model iz 0.3.
- **A2A specification + SDKs (Linux Foundation)** (standard) — kanonski wire ugovor:
  Agent Cards (`/.well-known/agent-card.json`), discovery, task lifecycle, artefakti
- **Microsoft Agent Framework** (harness) — A2A klijent i server + MCP u istom runtimeu
- **Google ADK** (harness) — konzumiranje/izlaganje A2A agenata, auto Agent Card
  (A2A integracija označena `experimental`)
- **Sigurnosna cijena:** Agent Card NIJE autorizacija — obvezni ostaju validacija sheme
  i veličine, allowlist/SSRF obrana pri discoveryju, AuthN/AuthZ, timeout/cancel,
  idempotentni task ID-evi, audit korelacija.

### 12.6 Multi-persona deliberation (Council) (NOVO v0.9; ugovor Annex A P2.7)
`multi-agent` flag; 16.6 ga konzumira kao reviewer-strategiju, ali deterministički checker/
promotion-gate ostaje neovisan. N persona → sealed nezavisni review → anonimni cross-review →
chairman sinteza uz OČUVAN materijalni dissent; svi rade nad istim `artifact_revision_hash`;
chairman ne smije izbaciti dissent bez adjudication-recorda; quorum+budget hard gate; council
NE može sam promovirati ni zamijeniti 16.6. Puni ugovor + RED `test_council_cannot_drop_sealed_
material_dissent`: **Annex A P2.7**.
- **salvage NEXUS `council.py`** (5 persona → anonimni cross-review → chairman)
- **Microsoft Agent Framework** (group deliberation) / CrewAI hierarchical / LangGraph supervisor

---

## S13. Multimodal — CAPABILITY PROFIL

### 13.1 Vision input (screenshotovi, dijagrami)
- **browser-use** — screenshot+DOM hibrid
- **Open Interpreter** — vision za computer use
- **OpenHands** — vision u browsing petlji

### 13.2 Generacija/edit slika
- **ComfyUI** — node-based pipeline, standard
- **InvokeAI** — produkcijski UI + API
- **diffusers** (komponenta) — programatska baza
- Dopuna (v0.7, NEXUS canvas): **strukturirani dijagram-out** — kompaktni validirani JSON
  scene/graph → deterministički render u editabilan dijagram + SVG/PNG. Hipoteza (za matricu):
  znatno manji token-trošak od sirovog SVG-a. Render mora biti schema-validiran i sandboxirano
  izveden (valjan JSON NE dokazuje odsutnost semantičke injekcije u labelama/linkovima — SVG
  sanitizacija obavezna). Kandidati: NEXUS `canvas.py` (salvage), Excalidraw, Mermaid.
  `image-out` flag, renderer = sandboxiran plugin.

### 13.3 Audio/voice (STT+TTS petlja)
- **pipecat** — realtime voice agent framework
- **LiveKit Agents** — produkcijski voice stack
- **speaches** — kompatibilan STT/TTS server (whisper)

### 13.4 Video obrada
- **FFmpeg** (komponenta) — deterministička obrada
- **WhisperX** — word-level transkripcija + diarizacija (razumijevanje sadržaja)
- **ComfyUI** — generacija/video nodeovi
- Runner-up (obsidian): Remotion/HyperFrames (deterministički HTML/React → MP4; agenti
  već znaju HTML, bez teških GPU diffusera)
- (yt-dlp = ingestion utility, ne kandidat obrade.)

### 13.5 Sigurne operacije nad dokumentima (NOVO v0.7; renumerirano iz 13.6 u v0.8, H3)
Flag `documents` (jedan multimodal flag, ne cijeli S13); path/signature/consent/atomic-write
granice = jezgra. **V4 (2:1): 13.5 je INSTANCA generičkog "Artifact-transformer" ugovora
(typed op → preview/dry-run → atomic staging (5.3) → output-verify → commit/rollback), NE
zasebna anatomija po formatu.** Isti ugovor pokriva PDF/DOCX/spreadsheet/sliku — dodavanje
novog formata = novi adapter, ne nova podsekcija. Read-ingestion ostaje odvojen u 10.1.
- **pypdf** (komponenta) + salvage NEXUS `pdf.py` adapteri (tek nakon parity testova)
- **OCRmyPDF** (komponenta)
- **pyHanko** (komponenta) — sign/verify

---

## S14. Sučelja (UX)

Minimalni REPL/CLI gradi se rano (P1 treba interakciju); puni UX kasno.

### 14.1 CLI/TUI
- **Codex CLI** — Rust TUI, brz
- **Gemini CLI** — open-source referentni agentic CLI
- **crush** ♦ — najljepši TUI pristup (source-available FSL-1.1-MIT, nije OSI)
- † Claude Code — zatvoreni referent kategorije

### 14.2 Headless / API / embeddable SDK mode (preimenovano v0.7)
- **OpenHands** — REST+WS server oko agenta
- **Dify** — self-hostable platforma s API serverom
- **goose** — API mode
- Dopuna (v0.7, NEXUS SDK): **embeddable typed SDK** — fluent API, dry-run default (safe u
  test/CI), koristi ISTI S0/6.9/7.x put kao CLI (ne poseban nereentrantan executor — NEXUS
  audit dug). Kandidati: NEXUS `sdk.py` (salvage), OpenAI Agents SDK, Vercel AI SDK. `full-UX`.

### 14.3 Web UI
- **Open WebUI** — standard za chat frontend
- **LibreChat** — multi-provider chat
- **OpenHands** — web UI nad agentom

### 14.4 IDE integracija
- **Cline** — VS Code standard
- **Continue** — IDE-first arhitektura
- **Agent Client Protocol (ACP)** (standard) — standardizirana agent↔editor granica:
  wire protocol, capability negotiation, službeni Rust/TS/Python/Java/Kotlin SDK-ovi
- Runner-up: Roo Code (Cline fork s modovima); AG-UI (standard za agent↔frontend
  event tok) — kvalificiran runner-up za 14.2/14.3, matrica uspoređuje

### 14.5 Messaging/channel gateway (NOVO v0.7)
Capability-profil `channels`; auth/routing/delivery state-machine su jezgra, adapteri
(Telegram/Slack/Discord/Matrix/Signal/WhatsApp/email) su pluginovi. Zajednički ugovor:
identitet po kanalu, deny-by-default allowliste, reply threading, delivery receipt,
idempotent retry, udaljeni HITL (agent pita na mobitel, run čeka odgovor). Dostava se veže
na durable-delivery (7.3).
- **salvage NEXUS `gateway/`** (6 kanala, deny-default)
- **OpenClaw** — channel gateway + topic routing
- **Rasa channel connectors** / Matterbridge

---

## S15. Observability

### 15.1 Tracing (OTel GenAI konvencije)
- **OpenTelemetry GenAI** (standard) — primaran; ostali su konzumenti
- **Langfuse** — self-hostable backend
- **Phoenix (Arize)** — OTel-first, jak na evalima

### 15.2 Usage/cost dashboardi
- **LiteLLM** — spend po ključu/modelu/timu
- **Langfuse** — cost po trace-u
- **Helicone** — proxy analytics

### 15.3 Transcript i audit log
- Dopuna (v0.7, NEXUS): **tamper-evident** — append-only hash-lanac + verify() (tamper
  blokira UPDATE/DELETE). Napomena: lokalni hash-lanac sam nije dokaz protiv POTPUNOG rewritea
  — dodati vanjski potpisani checkpoint. Kandidati: NEXUS `audit.py` (salvage) + Sigstore Rekor
  anchor, immudb, Trillian.

- **OpenHands** — event stream omogućuje audit i rekonstrukciju zabilježenog stanja
  (ne "savršen replay")
- **AgentOps** — session replay za agente
- **Langfuse** — trace kao auditabilan zapis

### 15.4 Metrics i SLO (NOVO — premješteno iz 18.3; nije deployment mehanizam)
- **Prometheus** (komponenta) — metrike i alerting standard
- **Grafana** (komponenta) — SLO dashboardi
- **OpenTelemetry Collector** (komponenta) — jedinstven put metrika iz 1.3/15.1

### 15.5 Semantic agent health i degradation (NOVO v0.7)
Jezgra; pragovi = bounded config po profilu. Uz procesni liveness (18.3) i servisni SLO
(15.4), hvata agenta koji je ŽIV ali degradira: ponavljanje istog toola, rast correction/
recidivism rate, pad verified-success trenda, tihi drift na fallback, rast progutanih
best-effort grešaka (NEXUS `health.py` swallow-counter). Otvara S7 circuit-breaker prije
lažno-zdravog rada. Vault bilješka tvrdi da self-critique/health introspection nema većina
auditiranih harnessa — tretirati kao hipotezu o rijetkosti, ne provjeren "0/28".
- **vlastiti OTel metrički agregator** (uz S7) + salvage NEXUS `circuit.py`/`health.py`
- **Langfuse** — evaluator metrike
- **Prometheus rules** — alerting

---

## S16. Eval i dokaz kvalitete

Bez ovoga se vodeće načelo ne može dokazati — mini-eval postoji od P0, ne od kraja.
Zahtjevi: versioniran task corpus, holdout, deterministic fixtures, snapshot identiteta
(model/prompt/tool verzije), ablation test da harness stvarno može postati RED, paired
baseline gate, cost/latency/turn budžeti, flaky politika.

### 16.1 Task-level benchmark (end-to-end)
- **SWE-bench** (benchmark) — standard za coding agente (nedovoljan sam: ne izolira doprinos harnessa)
- **Aider benchmark suite** — praktičan, brz, po modelu i edit formatu
- **Inspect (UK AISI)** — općeniti eval framework za agente
- Dopuna (v0.7, NEXUS app-contract): **deliverable-level acceptance contract** — za
  aplikacijski zadatak versionirani ugovor provjerava tražene ekrane/tokove/persistence/
  negativne slučajeve, ne samo test-count (sprječava da djelomičan demo prođe kao gotova app).
  Runner/non-vacuity = jezgra; ugovor = `coding`/domenski pack data. Kandidati: NEXUS
  `app_contract.py` (salvage), Cucumber/Gherkin, Playwright contract suite.
- Timing-zahtjev (obsidian, contract-negotiation): generator+evaluator dogovore "done" ugovor
  KROZ fajlove PRIJE prvog reda koda — acceptance je artefakt, ne osjećaj (veže 3.1+3.5).

### 16.2 Regression/ratchet u CI
- **promptfoo** — eval kao test suite u CI
- **DeepEval** — pytest-stil LLM testovi
- **Opik** — OSS eval tracking kroz vrijeme (zamjena za zatvoreni Braintrust)

### 16.3 Red-team / adversarial testiranje
- **garak** — LLM vulnerability scanner
- **PyRIT** — adversarialni framework
- **promptfoo redteam** — automatizirani napadi u CI

### 16.4 Feedback i data flywheel (NOVO)
Eval set raste iz produkcije, ne ostaje sintetički.
- **Langfuse** — annotations na trace + curation
- **Opik** — capture + annotacije + regresija
- **AgentOps** — session replay → curation
- Dopuna (v0.7, NEXUS credit/reward — kanonski owner ovdje, potrošači 9.3/11.5/12.3):
  **provenance-aware credit ledger** — memorija/skill/ruta prima kredit SAMO iz
  revision-bound VERIFICIRANOG ishoda (test-gate/loop-judge, nikad samoprijava), s
  exploration budgetom i rollbackom; routing uči iz toga (epsilon-greedy bandit). Kandidati:
  NEXUS `reward.py`/`credit.py` (salvage), Vowpal Wabbit contextual bandits, Langfuse/Opik.

### 16.5 Trajectory export za destilaciju (OPCIONALNI PROFIL) (NOVO)
- **SWE-gym** — okruženje + trajektorije za trening coding agenata
- **Axolotl** (komponenta) — SFT/DPO potrošač standardnih formata
- **ShareGPT/JSONL format** (standard) — izvozni format trajektorija

### 16.6 Neovisna provjera, anti-sycophancy i confidence (NOVO v0.7)
Minimalni checker/gate = jezgra; council i višemodelni reviewer = `multi-agent` strategija.
Prije predaje zaseban checker pokušava OBORITI premisu i zaključak na ARTEFAKTIMA
(git-diff + exit-code + deterministički signali), NE na prozi kandidata (checker≠worker);
kalibrira nesigurnost (confidence iz logproba) i blokira "uspjeh" kad dokaz ne prati tvrdnju.
Devil's-advocate, "nikad ne validiraj premisu", stop-hooks protiv praise-spam/false-success.
Sadrži i NEXUS council obrazac (N persona → anonimni cross-review → chairman sinteza uz
očuvan dissent) kao `multi-agent` kandidat. HIPOTEZA (za matricu, iz vault bilješke —
NEprovjereno): strukturno odvajanje checkera od workera na razini izvršnih artefakata je
rijetko među auditiranim harnessima (Reflexion/CrewAI-manager/AutoGen-debate imaju OBLIKE
samoprovjere) — ne apsolutna tvrdnja "0/28".
- **vlastiti checker** po NEXUS `loop_engine`/`confidence`/`prreview` obrascu (salvage)
- **UQLM** (komponenta) — uncertainty iz logproba
- **Inspect scorers** / DeepEval — eval-strana provjera

---

## S17. Ekstenzibilnost i distribucija

### 17.1 Plugin sustav
- **goose** — MCP-based extensions
- **DeepSeek Harness** (harness, developer preview!) — "everything is a plugin" nad
  Cordis DI kernelom: petlja, servisi, alati, hookovi, policy i SDK svi zamjenjivi —
  arhitektonski najdublji pristup kategorije; rc-status i breaking-change rizik za matricu
- **OpenCode** — plugin mehanizam
- Runner-up: Open WebUI (pipelines/functions — UI sloj, plići)
- † Claude Code — referent (skills+agents+MCP paketi)
- (v0.7: Reversa NIJE ovdje — premješteno u 11.2 kao skill-pack DATA; 17.1 ostaje samo
  izvršni plugin loader/ABI. Vidi 11.2.)

### 17.2 Update mehanizam i release kanali
- **Codex CLI** — kanali + samostalan update
- **goose** — versioned release + self-update
- **Aider** — višekanalni in-code upgrade (`--upgrade`, provjera verzije na startu,
  stable/dev grane)
- Sigurnosna osnova: **The Update Framework (python-tuf)** (standard+komponenta) —
  rollback/freeze zaštita update metadata lanca; TUF verificira i dohvaća, a harness i
  dalje posjeduje kanale, atomsku instalaciju i rollback. Kvalificirani runners-up za
  matricu: Ollama (in-binary self-update), axoupdater/self_update (komponente).

### 17.3 Packaging (jedan binary / laka instalacija)
- **goose** — Rust single binary (harness primjer)
- **dist (bivši cargo-dist)** (komponenta) — reusable mehanizam: cross-platform binarni
  artefakti, installeri, manifest, release CI (Ollama izbačen: dobro zapakiran proizvod,
  ne packaging alat)
- **Astral uv** (komponenta) — moderni Python packaging put

### 17.4 Field diagnostics i upgrade bridge (NOVO v0.7, iz NEXUS EXTRAS 19)
Capability-profil `service`/distribuirani artefakt; redaction/consent/update-verification =
jezgra. Uz opt-in korisnika harness gradi scrubban, reproducibilan bundle (verzije +
korelacijski ID), veže ga uz issue i nudi VERIFICIRAN update — bez ijednog automatskog slanja
ili instalacije (teren: bug → bundle → issue → `upgrade`). Cross-ref 15.3 i 17.2.
- **salvage NEXUS `bugreport.py`** + update-notify
- **GlitchTip** / Sentry (komponenta)
- **OpenTelemetry Collector + issue adapter**

---

## S18. Deployment i operacije (NOVO u v0.2)

### 18.1 Headless worker/API razdvajanje, queue/lease, horizontalno skaliranje
- **OpenHands** — konkurentne sesije s perzistencijom
- **Dify** — produkcijski multi-worker deployment
- **Temporal** (komponenta) — queue/lease semantika

### 18.2 Multi-tenant gateway: rate-limit, ključevi, budgeti
- **LiteLLM proxy** — de facto standard prednjeg sloja
- **Portkey AI Gateway** — governance nad providerima
- **Dify** — tenant kvote
- Pattern (obsidian, qm / Cloudflare Durable Objects): per-user izolirani workspace kao JEDNA
  jedinica (memory+files+keychain+permissions+cron+budget zajedno; "drugi chat_id" NIJE tenant
  boundary) + security posture Strict/Auto/Dangerous. Veže 5.2+6.6. Backend nije propisan.

### 18.3 Health/readiness + rollout/canary/rollback
- **Kubernetes** (komponenta) — health/readiness/rollout standard
- **Argo Rollouts** (komponenta) — canary/progressive delivery
- **Flagger** (komponenta) — automatizirani canary s metričkim gateom
- (Komponente, ne agent harnessi — namjerno. SLO/alerting je 15.4.)

### 18.4 State/schema migracije + backup/restore (NOVO — izdvojeno iz 18.3)
- **UNCLEAR — legalno prazno** dok S0.4 ne odabere persistence backend; kandidati ovise
  o session/event storeu (npr. sqlite/Postgres migracijski alati + point-in-time backup).

---

## Vlasništvo: KOD vs DATA (NOVO v0.7 — konsenzus sva 4 IDEAS izvora)

**Zlatno pravilo:** hardkodiraj MEHANIZAM, INVARIANT i TRUST BOUNDARY; podatkovno deklariraj
POLITIKU (unutar dopuštenog raspona kernela), DOMENSKO ZNANJE i EKSTENZIJE. Enforcement = kod;
sadržaj pravila = data — ali NIKAD kao slobodni tekst u promptu (kritična NEXUS v0 greška:
safety pravila kao tekst u identity.py umjesto kernel enforcementa).

**HARDKODIRANO ≠ JEZGRA (H1/kilo#3):** profilni mehanizam (S5, 6.6–6.8, S10 engine...) je i
dalje KOD — samo se AKTIVIRA okidačem profila, ne gradi uvijek. Kad dolje piše "S6 cijeli" ili
"S5 cijeli", to znači cijeli MEHANIZAM je kod (ne skill/config), NE da je cijela sekcija uvijek
uključena. "Uvijek uključeno" je zasebna os (jezgra vs profil iz Vodećeg načela).

**Autoritet je jednosmjeran:**
```
kernel postavlja granice → profil ih aktivira → role/agent biraju dopušteni rad
→ skill daje proceduru → plugin izvršava SAMO kroz kernel gate → eval odlučuje promociju
```
```
kernel code
 └─ capability-profile manifest (data; aktivira obvezne gateove)
     └─ role (data: cilj, whenToUse, dozvoljeni capabilityji, budget)
         └─ agent template (data: role + model policy + tool/skill set + output contract)
             └─ skill (SKILL.md: procedura, rubric)
                 └─ resources (data)
plugin/tool = izvršni kod → kernel ga učita TEK nakon signature/provenance/permission provjere
```

**HARDKODIRANO (kod):** S0 ugovori · S1 config-resolver+paths+logging · S2 provider ABI+
fallback+cost · S3 petlja+streaming+cancel+recovery+trigger-admission+delivery-state · 4.1
registry+validacija+lazy-exposure, 4.2 primitivni opasni alati · S5 cijeli · **S6 cijeli,
fail-closed, non-bypassable** · S7 cijeli · S8 mehanizmi (pruning INTERFACE) · 9.1 sesije +
memorijski SPINE + write-approval + delete/export enforcement · S10 ingest-transaction +
chunk-identity + **ACL/tenant filter PRIJE retrievala** + provenance · S11 prompt ASSEMBLER +
precedence + trust-fencing + skill-activation protokol + signature · S12 scheduler/DAG
validacija + izolacija + handoff envelope + A2A auth · S13 input-validation + MIME/size caps +
egress/consent · S14 jedan API + auth/CSRF/XSS + durable-delivery · S15 event/trace schema +
redaction-before-export + tamper-evident audit + semantic-health signali · S16 izolirani runner
+ revision-binding + non-vacuity + promotion/rollback autoritet + evaluator↔candidate separacija
· S17 ABI + manifest-validator + signature/provenance + install atomicity + rollback · S18
tenant-identity + quota + queue-lease + migration-lock + rollout state-machine.

**DATA / SKILL / PLUGIN / CONFIG:** role/agent/team (ROLE.md/AGENT.md/team.yaml + output
contract) · skills (SKILL.md procedura/rubric) · projektne instrukcije (AGENTS.md/CLAUDE.md) ·
system-prompt SADRŽAJ · tool DEFINICIJE (MCP serveri, CLI wrapperi) · workflow/DAG DEFINICIJE
(YAML) · gateway kanali (adapteri) · retrieval korpusi + chunking/query/freshness POLICY +
golden setovi · memorijski SADRŽAJ + extraction heuristike · eval TASKOVI/rubrike/adversarial/
judge-promptovi · multimodal backend izbor + recepti · SLO pragovi (unutar sigurnih granica) ·
capability-profili = konfiguracija · policy SADRŽAJ (allowlist/RBAC/deny-DSL/approval modovi) —
POTPISANA konfiguracija koja smije samo SUZITI bazni maksimum, nikad proširiti.

**6 testova "kod ili data?":** (1) može li greška omogućiti nedopušten side-effect/curenje/
gubitak/korupciju? → kod. (2) mora li pravilo vrijediti i kad su model/skill/plugin
zlonamjerni? → kod. (3) protokol koji svi adapteri jednako provode? → kernel interface +
conformance test. (4) zamjenjiv provider/tool/backend? → sandboxiran plugin iza ABI. (5)
domensko znanje/SOP/stil/persona/rubric/workflow? → skill/role/pack data. (6) mijenja se po
korisniku/projektu bez sigurnosne semantike? → config/data (tipizirano, bounds iz kernela).

**Nepreskočive granice (nikad samo SKILL.md/config):** legalnost state-tranzicije/schema-
migracije · permission/sandbox/egress/secret/tenant odluka · atomic-write/checkpoint/lease/
idempotency/crash-commit · redakcija/audit/provenance/artifact-identity · budget/cancel/timeout/
resource-cap · eval non-vacuity/promotion/rollback autoritet · plugin signature/permission-delta
· retrieval ACL i retention/delete izvršenje.

---

## Redoslijed gradnje: FAZE (zamjenjuje linearni niz iz v0.1)

Linearni S1→S15 je odbačen (sve 3 recenzije). Gradnja ide vertikalnim fazama; svaka faza
isporučuje provjerljiv, siguran, mjerljiv sustav.

- **P0 — Ugovor i dokaz:** S0 sheme; threat model i trust boundaryji (6.0); capability
  matrica providera (0.3); mini task corpus + vanjski end-to-end checker (16.1 klica);
  **16.6 klica** (neovisni checker odvojen od kandidata); u threat modelu zaključati
  redoslijed i failure-grane budućeg 6.9.
- **P1 — Najmanji sigurni vertical slice:** S1; S2 minimum (2.1+2.3); minimalni system
  prompt i projektne instrukcije (11.1+11.3); jedna turn petlja (3.1); jedan bezopasan
  typed tool (4.1) + **4.7 tool-exposure budget** (zajedno s registryjem); **6.9 lifecycle
  chokepoint obvezan** (nijedan tool/hook nema alternativni put); policy gate + sandbox
  (6.1+6.2); strukturirani trace (1.3); minimalni REPL (14.1 klica); jedan RED/GREEN eval.
- **P2 — Pouzdanost:** S7 cijeli (uklj. N9 durable-delivery u 7.3, prije svakog budućeg
  `channels`/`service` izlaza); 3.3+3.4; 8.5 observation pruning; 2.2 fallback; **minimalni
  15.5 signalni sloj + veza na S7 circuit-breaker** (puni metrički backend u P4).
- **P3 — Kvaliteta modelskog rada:** 4.3+4.4; 3.5 deterministička verifikacija (+ TIA uz
  `coding`); S5 workspace/rollback; S8 kontekst (8.1–8.4); 9.1 save/resume sesija;
  **11.2+11.4** (izričito NE 11.5/11.6 — čekaju `extensions` + 6.8 gate u P4).
  (Coding-profil dijelovi ove faze vrijede kad je profil uključen.)
- **P4 — Operatersko sučelje i audit:** S14 minimum (14.1) + S15 (uklj. 15.5 export/
  dashboard/pragovi) + 6.4–6.5; 14.2–14.4 (embeddable SDK) samo uz `full-UX`; uz
  `extensions` aktivirati **6.8+11.5+11.6 kao JEDNU cjelinu** (supply-chain PRIJE extension
  sadržaja); uz `channels` **14.5 tek nakon 7.3** + identity/approval 6.6/12.4.
- **P5 — Capability flagovi (samo oni čiji je okidač nastupio; NE bundle-closure):** uz
  `automation` **3.6**; uz `memorija` 9.2–**9.4**; uz `vector-retrieval` 10.1–10.3+10.6+**10.7**
  (`graph-retrieval` dodaje 10.4, `preload-cache` 10.5 — SAMO ako im okidač nastupi, ne cijeli
  S10); uz `multi-agent` S12 (+ council 16.6); pojedinačni multimodal flagovi `vision`/
  `image-out`/`voice`/`video`/`documents` aktiviraju samo svoju podsekciju (13.1/13.2/13.3/
  13.4/**13.5**), ne cijeli S13; uz `destilacija` **16.5**. Nijedan flag nije "gotov" bez
  vlastitog gatea/evala.
- **P6 — Distribucija i operacije:** 17.2–17.3 kad se isporučuje artefakt; 17.1 pluginovi
  iza već izgrađenih 6.8/11.5/11.6 gateova; uz `service`/distribuirani artefakt **17.4 nakon
  15.3 i 17.2**; S18 samo za `service`.
- **Kontinuirano od P0:** S16 raste sa svakom fazom (novi sloj = novi eval); **16.6 checker**
  raste s S16 dokazima; **15.5** dobiva profile-specific signale samo kad se profil aktivira.
  16.4 verified-credit ledger ostaje dormantan jezgreni ugovor dok ga potrošač ne aktivira.

---

## Eksplicitne odluke (zapisano da se ne otvara ponovno)

1. **Fine-tuning sloj se NE dodaje** kao harness sekcija — protivi se načelu "model =
   zamjenjivo gorivo". Jedina dodirna točka: 16.5 export trajektorija (opcionalni profil).
2. **Zatvoreni proizvodi** (Claude Code) = `†` referent, ne broje se u top-3.
   **Codex CLI ostaje eligible** — javni repo pod Apache-2.0 licencom (tvrdnja iz jedne
   recenzije da je zatvoren je netočna). **crush** = `♦` source-available.
3. **Samoreferenca na alate sudionika konsenzusa je zabranjena** (uklonjeno iz 2.2/14.2/17.2).
4. **CAG ostaje UNCLEAR** (10.5) dok ne postoje 3 prava end-to-end kandidata; KV-cache
   infrastruktura pripada 8.4.
5. **UNCLEAR je legalan unos** — bolji od popunjavanja trećeg mjesta radi forme (2.2, 6.7, 17.2).
6. **Verifikacijska matrica** (URL, licenca, aktivnost, putanja implementacije po svih ~65
   podsekcija × 3 kandidata) = SLJEDEĆA faza nakon strukturnog konsenzusa. Ovaj dokument
   je konsenzus o strukturi i radnim listama; konačna promocija top-3 zahtijeva svježi
   upareni dokaz, ne glasovanje agenata.

---

## CHANGELOG v0.1 → v0.2 (razrješenje recenzija)

**Prihvaćeno od svih:** Claude Code van top-3 (†); fazni redoslijed umjesto linearnog;
eval od P0; minimalni prompt sloj u P1.

**codex:** #1 načelo mjerljivo + profili (prihvaćeno); #2 matrica (prihvaćeno kao faza —
odluka 6); #3 faze (prihvaćeno, spojeno s agy fazama); #4 S0 (dodano); #5 security
proširen 6.0/6.6–6.8 + LlamaFirewall umjesto Rebuffa (prihvaćeno; 5.9 taint spojen u 6.5);
#6 S7 pouzdanost (dodano); #7 profili + ContextSource granice (prihvaćeno); #8 CAG/10x
(prihvaćeno — 10.5 UNCLEAR, 8.4 mjeri); #9 eval zahtjevi + Opik (prihvaćeno); #10 zamjene
(prihvaćeno uz zadržavanje Codex CLI kao OSS); #11 Portkey/Temporal/FFmpeg/uv (prihvaćeno);
#12 PydanticAI u 2.1, Qdrant u 10.3 (prihvaćeno); #13 log/event/trace razdvojeni + replay
tvrdnja ispravljena (prihvaćeno); #14 S5 workspace (dodano); #15 S18 deployment, fine-tuning
NE (prihvaćeno). OpenCode predložen na više mjesta — uvršten gdje ima jasnu dubinu (11.3,
17.1); za 3.2/4.4/12.1/14.1 postoje jači specifični kandidati.

**kilo:** #1 Claude Code (riješeno opcijom A — † referent); #2 samoreferenca (uklonjena);
#3 kriterij komponente (dodano); #4 checkpoint/rollback (S5); #5 deployment (S18);
#6 flywheel (16.4); #7 fine-tuning NE (odluka 1); #8 S13/S14 kontradikcija (riješeno
fazama + "kontinuirano od P0"); #9 S9-minimum ranije (P1); #10 multimodal kasnije (P5);
#11 S5-prije-S6 potvrda (zadržano, sad 6 prije 8); #12 ControlFlow van (Temporal);
#13 1.3 OTel/structlog (prihvaćeno); #14 Presidio (6.4); #15 Rebuff van (LlamaFirewall;
AgentDojo za mjerenje); #16 video obrada (FFmpeg/WhisperX); #17 crush ♦ (označeno).

**agy:** #1 zatvoreni softver (Claude Code †; ali Codex CLI JE open source — Apache-2.0,
javni repo — ostaje); #2 faze + eval rano (prihvaćeno); #3 deterministička verifikacija
(3.5); #4 worktree/izolacija (5.2); #5 observation pruning (8.5); #6 4.4 vs 8.3
razdvojeni točno po prijedlogu; #7 7.2/7.3 dedupliciran (Reflexion/Voyager/cognee u 9.3);
#8 zamjene uvrštene gdje nisu u koliziji s drugim recenzijama (Portkey, SWE-agent, gVisor,
Magentic-One, ast-grep, Repomix); Mentat NIJE uvršten (neaktivan projekt); #9 trajectory
export (16.5 opcionalni profil).

## CHANGELOG v0.2 → v0.3 (razrješenje runde 2)

**Verdikti runde 2:** agy AGREE v0.2; codex CHANGES (7); kilo CHANGES (9, najavio AGREE).

**codex:** #1 core/profile mapa precizirana točno po prijedlogu (coding/browser/
local-inference/full-UX/service profili) + uvjetno-obvezni okidači; S17 packaging/update
ostavljen u jezgri (svaki isporučeni artefakt ga treba — okidač-formulacija to pokriva).
#2 → 2.2 popunjeno (Kong umjesto Envoya — vidi kilo #3, jedini webfetch-verificiran;
Envoy+TensorZero runners-up), 6.7 ostaje UNCLEAR (Fides arhiviran — prihvaćeno), 17.2
popunjeno (Aider treći + TUF kao sigurnosna osnova točno po tvom scope-ograničenju).
#3 OPA u 6.0, LlamaFirewall samo 6.5 (prihvaćeno). #4 ast-grep van iz 3.5, treći UNCLEAR
(prihvaćeno). #5 Sandbox Agents (beta) u 5.2 (prihvaćeno). #6 dist/cargo-dist u 17.3
(prihvaćeno). #7 18.3 podijeljen na 18.3+18.4, SLO → 15.4 (prihvaćeno).

**kilo:** #1 S17 jezgra ✓, S18 = `service` profil s obveznim okidačem (ne čista jezgra —
lokalni single-user CLI ne treba horizontalno skaliranje; okidač čini tvoju poantu
obvezujućom gdje vrijedi). #2 9.1 save/resume = jezgra, u P3 (prihvaćeno). #3 Kong u 2.2
(prihvaćeno — tvoja verifikacija presudila). #4 Ollama u 17.2 nije treći (Aider je harness
s višekanalnim upgradeom; Ollama imenovan kao kvalificiran runner-up — matrica može
presuditi drukčije). #5 OpenMetadata = konceptualni referent u napomeni, UNCLEAR ostaje
(tvoja opcija B, usklađeno s codex dokazom o Fidesu). #6 (research) oznake + rečenica o
zrelosti u 9.3 (prihvaćeno). #7 Agent Skills standard umjesto Fabrica (prihvaćeno).
#8 Magentic-One (microsoft/autogen) nazivlje + izbačen iz 5.2 (prihvaćeno). #9 SWE-bench
(benchmark) tag + nove kategorije u kriterijima (prihvaćeno).

**agy:** AGREE zabilježen; nominacije razriješene: 2.2 TensorZero = runner-up; 6.7 Fides
odbijen (arhiviran 28.8.2026., codex verifikacija); 17.2 Aider prihvaćen, axoupdater u
napomeni.

**Runda 3 (v0.3):** kilo AGREE, agy AGREE; codex CHANGES s 2 materijalne, obje foldane
u v0.4: (1) P4/P6 usklađeni s core/profile mapom (uvjetni okidači umjesto bezuvjetnog
naloga), (2) protokolska verdict-linija sada verzijski generička.

**v0.4 KONSENZUS postignut 2026-08-31 (3× AGREE).**

## CHANGELOG v0.4 → v0.5 (LANDSCAPE UPDATE — post-cutoff kandidati)

Povod: web-provjera našla kandidate koje su svi recenzenti propustili (zajednički
knowledge cutoff). Sva tri PROPOSAL-a konvergirala na: Vercel AI SDK→2.1 (TS praznina),
Hermes Agent→9.3 + ispravak činjenične tvrdnje, ADK→12.2 (umjesto uvjetnog Difyja),
AutoGen→MAF (12.1+12.4, merge 4/2026), DeepSeek Harness→17.1.

**A2A lokacija (2:1):** codex+agy → nova 12.5 (multi-agent profil; S0 je jezgra, a A2A
nije obveza single-agent harnessa) vs kilo → S0.5 (kanonski ugovor kao MCP). Odluka:
12.5, uz cross-ref napomenu u 0.1 (kilova poanta o istom redu ugovora zabilježena).

**Pojedinačne promocije (jedan predlagač, bez protivljenja):** OpenClaw→11.1 (codex:
SOUL.md/AGENTS.md precedence match; Open Interpreter runner-up), Hermes→11.2 (codex;
goose runner-up), ACP standard→14.4 (codex; Roo Code runner-up), pi-mono→8.2 (codex;
LangGraph runner-up), Marvin runner-up 12.2 (kilo — popunjava ControlFlow sukcesiju),
Rig runner-up 2.1 (agy), AG-UI runner-up 14.2/14.3 (codex).

**Hold (svjesno NE promovirano):** Strands/Agno/Qwen-Agent — širina bez dokazane dubine
na specifičnoj podsekciji (runners-up gdje relevantno); Claude Agent SDK — Python SDK
MIT ali TS pod komercijalnim uvjetima i oba bundlaju zatvoreni runtime → ne prolazi
top-3 kriterij; Mastra — runner-up 12.2, `ee/` granica za matricu.

**9.3 formulacija:** codexova preciznija verzija (Hermes NUDI spremanje skilla uz
odobrenje — ne bezuvjetna autonomija) preglasala kilovu tvrdnju "automatsko"; kilova
poanta da je stara rečenica činjenično netočna prihvaćena u cijelosti.

**Runda v0.5:** kilo AGREE, agy AGREE; codex CHANGES s 3 nalaza, svi foldani u v0.6:
(1) Hermes 9.3 trust boundary ispravljen — `skills.write_approval: false` je default
(codex verificirao source: tools/write_approval.py), memorijska tvrdnja svedena na
verificirano; (2) Tidepool uklonjen iz 15.3 (identificirani repo nije trace-replay alat,
kilova nominacija pala na verifikaciji); (3) stale protokolski token uklonjen.

**Potvrdna runda (v0.6):** potvrdi tri gornja popravka. Sve promocije ostaju RADNE do
verifikacijske matrice (odluka 6) — novi unosi tamo dobivaju i negativni test granice
koju navodno pokrivaju.

## CHANGELOG v0.6 → v0.7 r1 (integracija N1-N10 + 19 NEXUS EXTRAS)

Izvor: IDEAS-CONSENSUS.md (obsidian rudarenje, 4 agenta) + NEXUS-AUDIT.md [B] (19 extras).
Cilj: ništa od onoga što NEXUS stvarno ima ne smije ostati izvan sekcija; obsidian ideje
mapirane po selekciji.

**11 novih podsekcija:** 3.6 run-trigger-plane · 4.7 tool-exposure-budget · 6.9 lifecycle-
enforcement · 9.4 memorijska-konsolidacija/decay · 10.7 retrieval-quality-gate · 11.5 skill-
lifecycle/governance · 11.6 role→agent→skill data-ugovor · 14.5 channel-gateway · 15.5
semantic-agent-health · 16.6 anti-sycophancy/samoprovjera · 17.4 field-diagnostics/upgrade.

**Dopune postojećih:** Vodeće načelo (N1 subtractive/additive) · novi odjeljak "Vlasništvo:
kod vs data" · 3.5 (TIA) · 6.2 (GORTEX membrana) · 6.5 (canary tripwire) · 7.3 (durable
delivery N9) · 8.2 (pi-mono) · 10.1 (PDF driver) · 10.2/10.3 (chunking/cross-encoder) · 10.5
(CAG decision rule) · 13.2 (canvas) · 14.2 (embeddable SDK + rename) · 15.3 (tamper-evident) ·
16.1 (app-contract) · 16.4 (credit/reward ledger) · 17.1 (Reversa).

**Spajanja (nema duplog unosa):** anti-sycophancy EXTRA + council + N2 → 16.6;
**app-contract → 16.1** (NE 16.6 — ispravak r2: acceptance-kriterij ≠ reviewer-strategija);
gateway EXTRA + N8 → 14.5; skill-forge EXTRA + N4 → 11.5; proaktivnost EXTRA + N7 → 3.6;
health-swallow EXTRA + N5 → 15.5; role-data EXTRA → 11.6; credit → 16.4; field-bridge → 17.4.
Pokrivenost: 10/10 N + 19/19 EXTRAS.

## r2 GLASANJE — zatvoreno (svaki agent = 1 glas)

T1 sleep-time→**9.4** (A, 3:0) · T2 durable-delivery→**7.3** (A, 3:0) · T3 canvas→**13.2**
(A, 3:0) · **T4 PDF→nova 13.6 `documents`** (B, 2:1 — codex+kilo B, agy A; MUTACIJA≠ingest
presudilo) · T5 field-bridge→**17.4** (A, 3:0) · T6 Reversa=**coding+extensions**, bez
zasebnog profila (3:0) · T7 app-contract→**16.1** (A, 3:0).
Q1 **documents = zaseban profil** (3:0; read u 10.1, mutacija u 13.6) · Q2 **17.4 nova
podsekcija** (3:0) · Q3 credit-ledger = **dormantan jezgreni ugovor + profil-aktivacija**
(konsenzus: shema/provenance u jezgri, ledger/bandit aktivni uz memorija/extensions/routing).

## codex r2 primjedbe — foldano u r2

1. Core/profile mapa + faze bile stale → dodani profili automation/channels/extensions/
   documents, memorija=9.2–9.4, jezgra bez 3.6, nove podsekcije u mapi. (Faze P0-P6 još
   treba dopuniti uvjetnim aktivacijama — OSTAJE za r3.)
2. PDF krivo u 10.1 → premješteno u novu 13.6, 10.1 read-only. (=T4-B)
3. Reversa krivo pod 17.1 (izvršni plugin) → premješteno u 11.2 (DATA skill-pack) + 11.5
   lifecycle; 17.1 ostaje loader/ABI.
4. Neprovjerene tvrdnje kao činjenice → 4.7 (Pi/TB2.0), 13.2 ("3×"/"injection-proof"→
   "schema-validiran+sandboxiran"), 15.5 ("0/28") sad označene HIPOTEZA za matricu.
5. Canary reakcija preslaba → 6.5 sad FAIL-CLOSED blok cijele isporuke + audit + ljudska
   odluka, ne samo redakcija tokena.
6. 6.9 on_error kao zadnji linearni član → redefiniran kao GRANA iz svake faze.
7. changelog app-contract→16.6 greška → ispravljeno na 16.1.

**Runda 3 (potvrda):** agenti potvrde r2 foldove (VERDICT AGREE/CHANGES v0.7); jedino OSTALO
je codex #1-dio: dopuniti faze P0-P6 uvjetnim aktivacijama novih podsekcija. Sve promocije
RADNE do verifikacijske matrice. **Nakon 4× AGREE: faza LOVA NA RUPE/ŠUM u konsenzusu**
(eksplicitni sljedeći korak po uputi korisnika — traži se ima li praznina, kontradikcija,
mrtvih grana ili neprovjerenih tvrdnji u cijelom konsenzusu, ne samo v0.7).

## CHANGELOG v0.7 → v0.8 (LOV NA RUPE — red-team 3 agenta, 63 nalaza → 30 dedup)

Izvor: HOLES-{codex(37),kilo(13),agy(13)}.md + HOLES-CONSENSUS.md (moderator triaža).

**TIER 1 — 12 jasnih grešaka FOLDANO:** H1 kod-vs-data načelo ispravljeno (additive≠data;
mehanizam/invariant/trust-boundary=kod) + "hardkodirano≠jezgra" · H2 16.6 "0/28 (§7)" →
hipoteza, mrtav §7 maknut · H3 S13 renumeracija (13.6→13.5, iza 13.4; fantomski 13.5
riješen) · H4 10.7 mrtva dopuna → zahtjevi premješteni u 10.2/10.3 · H5 stale UNCLEAR/broj
(vidi dolje) · H6 P4 "15.5 export" → "15.2/15.4/15.5" · H7 Presidio dupli (6.7 kanonski) ·
H8 pi-mono dupli (8.2 kanonski, 4.7 cross-ref, "stvarni"→hipoteza) · H9 11.6↔annex
(cross-ref) · H10 destilacija profil vraćen · H11 superlativi ("najjači/dokazano" → "snažan
dokaz + benchmark") · H12 8.4 API-sloj vs lokalni-sloj razdvojen + cache-key granica.

**TIER 2 — 8 spornih NA GLASANJU (V1-V8, r2):** V1 model-routing 12.3 vs S2-jezgra · V2
profili all-or-nothing vs composable-flags · V3 Reversa/Field-bridge/PDF obvezni vs
referentni-packovi · V4 13.5 documents vs generički Artifact-transformer · V5 salvage vs
javni-top3 razdvajanje · V6 profil-okidači 5/12 vs svih-12 · V7 10.7 zaseban vs fold-u-16.2
· V8 6.7 data-governance service-only vs baseline+service.

**TIER 3 — 17 UGOVORNIH RUPA (backlog PRIJE verifikacijske matrice, ne u v0.8):**
codex#6-8 S0 sheme bez kanonskih polja/ContextBlock-provenance · #9 profil-closure resolver
(nova 0.5) · #12 MEMORY_FORGET vs DATA_PURGE razdvojiti · #13 provider data-processing/
rezidencija · #14 MCP klijent auth/peer-identity/transport-trust · #15 distribucijski
artefakt→6.8 gate · #16 draft/commit/compensate za ireverzibilne side-effecte · #17 bounded
budgets izvršni mehanizam (CPU/RAM/disk/socket) · #18 queue lease/reclaim/fencing (audit VEĆ
našao kvar) · #19 cache tenant/principal granica · #20 retry/cancel/recovery jedan owner (S7)
· #21 event/log/trace/audit jedan write-owner · #33 automation clock/DST/missed-run · agy#1
TOCTOU skill-hash-on-load (skills.lock) · agy#2 startup orphan/lock sweep · agy#5 Council
formalna 12.6 · codex#10 channels→extensions gate + HITL owner.

**TIER 4 — METODOLOŠKI (napomene, ne mijenja mehaniku):** "≈94% pokriveno" = "header
presence nad v0.6" (85≠97, DJEL≠IMA) · verifikacijska matrica dobiva HARD LIMIT pojavljivanja
po projektu (over-indexing: OpenHands 23×/Codex 20×/Aider 16×) + critical-gate floor
(0.1/0.2/7.2 se ne kompenziraju perifernim IMA) · "ništa se ne baca" → "ništa dokazano se ne
gubi iz salvage backloga, ali u spec ulazi samo generički invariant s neovisnim 2. potrošačem"
· cilj "najslabiji model" vezati uz capability-floor + fail-closed ispod.

**H5 stvarni UNCLEAR (ažurirano):** 3.5 (3. check-and-reprompt), 6.7 (data-governance 3.),
10.5 (CAG implementacija). Spec ima **97 podsekcija** (ne "~65") — verifikacijska matrica
izvlači popis automatski iz headinga.

## V1-V8 GLASANJE — zatvoreno + primijenjeno (v0.8)

- **V1 → A (2:1** codex+kilo A, agy B): model-routing premješten u **novu 2.6 (jezgra)**;
  12.3 = multi-agent potrošač. Bazični barbell=jezgra, naučeni router=opcija iza ABI.
- **V2 → A (3:0):** profili = **composable feature-flags** s okidačima (tablica u Vodećem
  načelu); `multimodal`/`retrieval` sada bundle-aliasi, ne all-or-nothing.
- **V3 → A (3:0):** Reversa/Field-bridge/PDF = **referentni signed-packovi**, izbačeni iz
  obveznog `coding`/`service` closurea.
- **V4 → A (2:1** codex+kilo A, agy B-nijansa): 13.5 = **instanca generičkog Artifact-
  transformer ugovora**, ne anatomija po formatu.
- **V5 → A (3:0):** svaka podsekcija dobiva 3 polja — **Normativni-mehanizam / Salvage-source
  / RADNI-javni-top3**; NEXUS/vlastiti kod NE broji se kao javni top-3. (Template-pravilo;
  mehanička primjena na svih 97 podsekcija ide u Tier-3/matrix-prep pass.)
- **V6 → A (3:0):** okidač napisan za svih 18 flagova (tablica).
- **V7 → B (3:0):** 10.7 ostaje ZASEBAN (retrieval-grounding/freshness/provenance) + cross-ref
  na 16.2 (generički CI ratchet).
- **V8 → A (3:0):** 6.7 podijeljen — **baseline (jezgra, svaki persistentni store)** +
  service dodatak.

**codex kritičan redoslijed-fix (svi AGREE):** uklonjena kontradikcija "nakon V1-V8 spec ide u
matricu" — točan redoslijed je **V1-V8 → Tier-3 ugovorni pass (BLOKIRA matricu) → matrica**.
Tier-3 nisu opcija nego normativni kriteriji BEZ kojih matrica rangira po nepreciznim zahtjevima.

**r2 (v0.8 potvrda):** provjeri primjenu V1-V8 (VERDICT AGREE/CHANGES v0.8). Nakon 3× AGREE →
Tier-3 ugovorni pass (17 rupa) kao zasebna faza, pa tek onda verifikacijska matrica.

## CHANGELOG v0.8 — obsidian POJAČAVA/NOVO potpuno foldano (korisnik uhvatio rupu 2×)

Strukturne obsidian ideje (N1-N10, subtractive/additive) bile foldane od v0.7. Ali batch
konkretnih kandidata-alata živio je samo u IDEAS-CONSENSUS.md. Sad foldani u kandidatske liste:
RTK/Headroom→8.5 · Understand-Anything/Scan/Graphify→8.3 · Marker/MinerU/OCRmyPDF/Surya→10.1 ·
LanceDB/sqlite-rag/query-rewrite→10.3 · Camoufox→4.5 · Remotion/HyperFrames→13.4 · Stash→9.1 ·
hardware-aware-fit+Odysseus/Colibri→2.4 · AXI→4.6 · sandbox-attestor/PROV-O→5.3 ·
cost-checkpoint→7.2 · OpenHuman/TokenJuice→9.2 · SurfSense-live-connectors→10.1 ·
source-grounded-skill+Depx→11.5 · qm/Durable-Objects-pattern→18.2 · contract-negotiation→16.1.

**Matrix runner-up pool (obsidian, manje vrijedno — za razmatranje u matrici, ne blokira):**
find-skills meta-skill (11.2), NotebookLM→SKILL.md factory (11.2), Shannon pentester (16.3),
prime-agent IPython-jedini-alat (3.1/4.2 — uz blast-radius ogradu iz V-odbijenih), Databricks
failure-mode checklist (7.x/16 dijagnostika), "run-like-a-company" org-chart (12.1), Taste
anti-slop skill (14.1), VoiceBox (13.3), claude-mem (9.2), openworker deliverable-first (12.4),
MarkItDown (10.1).

**Svjesno ODBIJENO (ne ulazi):** AutoAgent (auto-rewrite harnessa), trgovački/osobni domenski
alati, anti-bot bypass kao core cilj — vidi ODBIJ u IDEAS-CONSENSUS.md.

---

# ANNEX A — TIER-3 NORMATIVNI UGOVORI (foldano u v0.9)

Ovi ugovori su OBVEZNI kriteriji verifikacijske matrice — svaki kandidat u vezanoj
podsekciji mora dokazati pripadni ugovor. Bez Annexa spec je 'lista projekata'; s njim je
'normativni ugovor'. Redoslijed: V1-V8 ✓ → Annex A (OVDJE) → matrica ODBLOKIRANA.

## Wiring: ugovor → matična podsekcija (matrica čita ovdje)

| Ugovor | Vezano na | Rupa |
|---|---|---|
| P0.1 Envelope/state/migracija/ContextBlock | S0.1–S0.4 | codex#6-8 |
| P0.2 Jedini owner retry/deadline/cancel | S7.1 (2.2/3.3/3.4 delegati) | codex#20 |
| P0.3 Jedini write-owner event/log/trace/audit | S0/S15 EventJournal | codex#21 |
| P0.4 Capability-flag closure resolver | nova S0.5 | codex#9 |
| P1.1 Runtime skill TOCTOU (skills.lock) | 11.5+6.8 | agy#1 |
| P1.2 Startup orphan/lock sweep | 7.3 (5.2/1.2) | agy#2 |
| P1.3 MCP transport/peer-identity/auth | 4.6 (6.5/6.6/7) | codex#14 |
| P1.4 draft→approve→commit→verify→compensate | 6.9+7 | codex#16 |
| P1.5 Distribucijski artefakt → 6.8 gate | S0.5 (6.8/17.2/17.3) | codex#15 |
| P1.6 channels→extensions closure + HITL token | S0.5/14.5/6/7 | codex#10 |
| P2.1 MEMORY_FORGET vs DATA_PURGE | 9.x (S6.7 nadjačava) | codex#12 |
| P2.2 Izvršni bounded budgets | 7.2 (4.2/6.2 OS) | codex#17 |
| P2.3 Queue lease/reclaim/fencing | 7.2/18.1 | codex#18 |
| P2.4 Cache tenant/principal granica | 8.4 (6.6/10.6/6.7) | codex#19 |
| P2.5 Provider data-processing/rezidencija | 0.3/2.1 (6.7/6.9) | codex#13 |
| P2.6 Automation clock/DST/missed-run | 3.6 | codex#33 |
| P2.7 Council formalni ugovor | nova S12.6 | agy#5 |
| P0.5 Capability-matrix fail-closed | S0.3 | REVIEW2 G1 |
| P0.6 Config-bounds + process-identity | S1.1/1.2 | REVIEW2 G1 |
| P0.7 Structured-output never-silent | S2.3 | REVIEW2 G1 |
| P0.8 Hardware-fit fail-closed (trigger: `local-inference`, ne-P0) | S2.4 | REVIEW2 G1 |
| P0.9 In-turn verify never-silent (trigger: `coding`, ne-P0) | S3.5 | REVIEW2 G1 |
| P0.11 Shadow-checkpoint integritet (P0: samo AtomicWriter; ostalo trigger `coding`) | S5.1/5.3 | REVIEW2 G1 |
| P0.13 Decay-never-destroys-bytes (trigger: P1 `memorija` decay, ne-P0) | S9.4 | REVIEW2 G1 |



# Normativne konvencije

- `MUST`/`MUST NOT` su obvezni kriteriji verifikacijske matrice.
- Svako odbijanje MUST vratiti tipizirani `error.code`, zapisati kanonski događaj i završiti prije nedopuštenog sinka ili prijelaza stanja.
- Svaki RED test MUST biti red-capable: kontrolirana mutacija krši navedeni invariant; oracle promatra trajno stanje, proces ili vanjski sink, ne samo vraćeni error.
- ID-evi MUST biti stabilni kroz retry/replay; timestampi MUST biti UTC; deadline/lease trajanja MUST koristiti monotoni sat.

# P0 — kanonski ugovori i vlasništvo

## P0.1 — S0 kanonski envelope, state-machine, migracija i `ContextBlock` (codex #6–8)

- **Vlasnik:** S0.1–S0.4; svi adapteri konzumiraju isti ugovor.
- **Zajednički `Envelope` MUST imati:** `schema_id`, `schema_version`, `event_id`, `event_type`, `run_id`, nullable `turn_id`, nullable `tool_call_id`, nullable `parent_event_id`, `sequence`, `emitted_at`, `actor_type`, `actor_id`, `principal_id`, nullable `tenant_id`, `workspace_id`, `attempt_no`, nullable `idempotency_key`, `payload`, `payload_hash`.
- **`Message` MUST imati:** `message_id`, `role` (`SYSTEM|USER|ASSISTANT|TOOL`), `content_blocks[]`, `created_at`; svaki element `content_blocks` MUST biti `ContextBlock`.
- **`ToolCall` MUST imati:** `tool_call_id`, `tool_id`, `arguments`, `arguments_schema_hash`, `effect_class` (`READ_ONLY|REVERSIBLE|IRREVERSIBLE`), `deadline`, `attempt_no`, `idempotency_key` za svaki state-changing poziv.
- **`ToolResult` MUST imati:** `tool_call_id`, `attempt_no`, `status` (`SUCCEEDED|FAILED|CANCELLED|UNKNOWN`), `output_blocks[]`, nullable `error`, `started_at`, `finished_at`; rezultat MUST korelirati s postojećim pozivom i istim attemptom.
- **`TypedError` MUST imati:** `code`, `category` (`VALIDATION|AUTHN|AUTHZ|POLICY|RESOURCE|TIMEOUT|CANCELLED|DEPENDENCY|CONFLICT|INTERNAL|UNKNOWN_EFFECT`), `retryability` (`NEVER|S7_POLICY|AFTER`), `safe_message`, nullable `retry_after`, `origin`, nullable `cause_event_id`; proizvoljni string ne smije upravljati retryjem.
- **`ContextBlock` MUST imati:** `block_id`, `kind`, `content` ili `content_ref` (točno jedno), `content_hash`, `source_uri`, `producer`, `trust_class` (`SYSTEM|USER|TOOL_TRUSTED|UNTRUSTED_EXTERNAL`), `sensitivity` (`PUBLIC|INTERNAL|CONFIDENTIAL|SECRET`), `lineage[]`, `observed_at`, nullable `expires_at`.
- **Stateovi MUST biti:** run `CREATED→ADMITTED→RUNNING→{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}`; turn `CREATED→RUNNING→{SUCCEEDED|FAILED|CANCELLED}`; tool attempt `PLANNED→AUTHORIZED→RUNNING→{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}`. `SUCCEEDED|FAILED|CANCELLED` su terminalni; `UNKNOWN` smije prijeći samo u `SUCCEEDED|FAILED|MANUAL_RECOVERY` putem eksplicitnog reconciliation događaja.
  **AMENDMENT (Phase-1B review, 2026-09-03):** kanonski `*.cancelled` event je legalan iz SVAKOG ne-terminalnog stanja prije završetka (run: CREATED/ADMITTED/RUNNING; turn: CREATED/RUNNING; attempt: PLANNED/AUTHORIZED/RUNNING) — korisnik smije otkazati posao koji još čeka. Cancel iz `UNKNOWN` NIJE legalan (UNKNOWN izlazi samo rekoncilijacijom). Jedan event-tip, multi-from tablica; nema sufiksiranih alias imena.
- **Migracija MUST:** čuvati immutable raw event; koristiti jedinstveni registry `schema_id/version`; upcastati monotono verziju-po-verziju; karantenirati nepoznati schema ID/verziju; zabraniti lossy downcast; zapisati `migration_from`, `migration_to`, `migration_id`, `input_hash`, `output_hash`. **P0-scope (HARDQ C7):** u P0 je dovoljan `schema_version` + reject-unknown-neobrađeno (raw input sačuvan); upcast-LANAC + karantena-sink aktiviraju se s prvom stvarnom migracijom (v2) i tada ovaj ugovor vrijedi u cijelosti.
- **Invarijante:** `sequence` strogo raste po runu; ID i causal parent se ne mijenjaju kroz migraciju; transformacije S8/S9/S10/S11 MUST očuvati `lineage` te smiju samo pooštriti `trust_class`/`sensitivity`; unknown polje se očuva, unknown discriminator se odbija.
- **RED — `test_s0_rejects_provenance_laundering`:** untrusted `ContextBlock` prođe kroz compaction/upcaster koji ukloni `lineage` i postavi `trust_class=SYSTEM`; prompt assembly MUST vratiti `PROVENANCE_DOWNGRADE`, run ne smije prijeći u `RUNNING`, a model sink mora imati 0 poziva.

## P0.2 — Jedini owner retryja, deadlinea i cancela: S7 (codex #20)

- **Vlasnik:** S7.1; S2.2 samo normalizira provider error i predlaže fallback target; S3.3 emitira user cancel; S3.4 prikazuje konačni `TypedError` modelu bez retryja.
- **`ExecutionPolicy` MUST imati:** `operation_id`, `effect_class`, `deadline`, `max_attempts`, `attempt_timeout`, `backoff_policy`, `retryable_codes[]`, `fallback_targets[]`, `idempotency_key`, `cancel_token_id`.
- **`AttemptGrant` MUST imati:** `operation_id`, `attempt_no`, `target_id`, `issued_at`, `expires_at`, `grant_nonce`; svaki fizički provider/tool pokušaj MUST predočiti jedinstveni S7 grant.
- **Stanja:** `PENDING→GRANTED→RUNNING→{SUCCEEDED|FAILED_RETRYABLE|FAILED_TERMINAL|CANCELLED|UNKNOWN}`; samo S7 smije iz `FAILED_RETRYABLE` izdati sljedeći `AttemptGrant`.
- **Invarijante:** zbroj attempta ne prelazi `max_attempts`; deadline i cancel propagiraju se u sve child taskove; `CANCELLED` je terminalan; `IRREVERSIBLE+UNKNOWN` se ne retrya bez reconciliationa; adapter/loop ne smije sadržavati vlastitu retry petlju.
- **RED — `test_adapter_cannot_self_retry`:** lažni provider dvaput pozove transport s istim `AttemptGrant`; drugi poziv MUST pasti s `ATTEMPT_NOT_AUTHORIZED`, transport counter mora ostati 1, a S7 attempt counter 1.

## P0.3 — Jedini write-owner za event/log/trace/transcript/audit (codex #21)

- **Vlasnik:** `EventJournal.append()` u S0/S15; S1.3, S15.1 i S15.3 su read-only projekcije.
- **`JournalEvent` MUST koristiti S0 `Envelope` i dodatno imati:** `journal_offset`, `redaction_policy_version`, `integrity_prev_hash`, `integrity_hash`, nullable `sealed_payload_ref`.
- **Write put:** `validate schema → classify/redact → atomic append+sequence allocation → durable flush → publish projection offsets`; nijedan sink ne smije sam alocirati event ID/sequence.
- **Projekcije MUST imati:** `source_event_id`, `source_offset`, `projection_version`; log = dijagnostička projekcija, trace = span projekcija, transcript = user-visible projekcija, security audit = append-only projekcija s vanjskim integrity checkpointom.
- **Stanja projekcije:** `UNSEEN→APPLIED`; replay istog `source_event_id+projection_version` je idempotentan; offset se ne potvrđuje prije durable zapisa.
- **Invarijante:** nema paralelnog write API-ja; redakcija se događa prije journala/projekcija; projekcija ne smije izmijeniti source događaj; replay i audit moraju referencirati isti `event_id`; tajna smije postojati samo kao autorizirani sealed reference, nikad u indeksiranom payloadu.
- **RED — `test_projection_cannot_bypass_journal`:** trace exporter pokuša izravno zapisati event s canary tajnom bez `source_event_id`; write MUST pasti s `NON_CANONICAL_WRITE`, nijedan sink ne smije sadržavati canary i napadački event ne smije biti appendan (zaseban redaktirani rejection-event je dopušten).

## P0.4 — Capability/feature-flag closure resolver (codex #9)

- **Vlasnik:** nova S0.5; jedini aktivator capability flagova i njihovih gateova.
- **`CapabilityManifest` MUST imati:** `manifest_schema_version`, `flag_id`, `flag_version`, `provides[]`, `requires[]`, `conflicts[]`, `gates[]`, `adapters[]`, `trigger_id`, `trigger_version`, `config_hash`, `signature`.
- **`GateAttestation` MUST imati:** `gate_id`, `gate_version`, `implementation_id`, `config_hash`, `status`, `verified_at`, `expires_at`, `evidence_ref`.
- **Stanja:** `DECLARED→RESOLVED→VALIDATED→ACTIVE`; failure ide u `REJECTED`; parcijalno `ACTIVE` stanje nije legalno.
- **Resolver MUST:** proširiti tranzitivni closure; detektirati cycle/conflict/unknown version; topološki aktivirati; potvrditi sve gate attestatione nad istim config hashom; atomically objaviti aktivni set.
- **Invarijante:** adapter se ne učitava prije `ACTIVE`; manifest/policy može samo suziti kernel maksimum; promjena manifesta invalidira closure i zahtijeva novu atomsku aktivaciju; bundle-alias nije capability i ne smije prisilno aktivirati flag bez njegova triggera.
- **RED — `test_every_flag_fails_when_one_gate_is_ablated`:** za svaki od 18 flagova ukloniti po jedan deklarirani `gate`; resolver MUST vratiti `INCOMPLETE_CAPABILITY_CLOSURE`, aktivni set ostaje prethodna verzija i nijedan adapter iz nevaljanog closurea nije učitan.

# P1 — sigurnosne granice

## P1.1 — Runtime TOCTOU provjera skilla (`skills.lock`) (agy #1)

- **Vlasnik:** S11.5+S6.8 loader; install-time scan nije dovoljan.
- **`skills.lock` entry MUST imati:** `lock_schema_version`, `skill_id`, `skill_version`, `canonical_root`, `tree_digest_alg`, `tree_digest`, `file_manifest[]` (`relative_path`,`mode`,`size`,`digest`), `signer_id`, `signature`, `permission_set_hash`, `dependency_digests[]`, `scanner_policy_version`, `approved_at`.
- **Stanja:** `STAGED→SCANNED→PINNED→VERIFIED_ON_LOAD→ACTIVE`; mismatch vodi u `QUARANTINED`; revocation vodi u `REVOKED`.
- **Load MUST:** otvoriti canonical root bez symlink escapea; hashirati canonical path+mode+bytes; verificirati signature/permission hash; izvršiti ili prompt-assembleati točno verificirane bytes preko content-addressed objekta/open handlea, bez path re-read TOCTOU prozora.
- **Invarijante:** provjera pri svakom process startu, aktivaciji i hot reloadu; lock i skill moraju biti na istom digestu; nepoznata datoteka ili permission delta fail-closed; agent-authored skill ostaje inertan do nove verifikacije.
- **RED — `test_skill_swap_after_install_is_quarantined`:** nakon validnog installa zamijeniti `SKILL.md` symlinkom ili promijeniti jedan byte prije loadanja; loader MUST vratiti `SKILL_DIGEST_MISMATCH`, stanje `QUARANTINED`, a promijenjeni sadržaj ne smije ući u prompt ni executor.

## P1.2 — Startup orphan/process-tree/workspace-lock sweep (agy #2)

- **Vlasnik:** S7.3 startup recovery; samo resursi s dokazanim vlasništvom harnessa.
- **`ResourceLease` MUST imati:** `resource_id`, `resource_type` (`PROCESS_TREE|WORKSPACE_LOCK|TEMP_DIR`), `run_id`, `owner_instance_id`, nullable `pid`, nullable `pid_start_token`, nullable `process_group_or_cgroup`, `canonical_path`, `acquired_at`, `heartbeat_at`, `expires_at`, `cleanup_policy`, `fencing_token`.
- **Stanja:** `OWNED→ORPHAN_SUSPECTED→FENCED→CLEANING→RELEASED`; nedokazivo vlasništvo vodi u `QUARANTINED`, ne u kill/delete.
- **Sweep MUST:** usporediti durable run state, lease expiry, PID start-token i process group; prvo fenceati novi rad; terminirati cijelo dokazano stablo `TERM→bounded wait→KILL`; potvrditi exit; tek tada otpustiti lock i označiti cleanup dovršenim.
- **Invarijante:** PID bez start-tokena nije dokaz identiteta; aktivan lease se ne dira; symlink/putanja izvan workspace roota se ne čisti; djelomični cleanup ostaje `CLEANING/QUARANTINED`, nikad `RELEASED`.
- **RED — `test_sigkill_restart_reaps_owned_tree_only`:** SIGKILLati worker nakon što ostavi child proces i workspace lock, zatim pokrenuti recovery uz nepovezani proces s recikliranim PID-om; sweep MUST ugasiti dokazani child, osloboditi njegov lock i ostaviti nepovezani proces živim.

## P1.3 — MCP transport, peer identity i auth ugovor (codex #14)

- **Vlasnik:** S4.6 uz S6.5/S6.6/S7; primjenjuje se i na lokalni single-user harness.
- **`McpPeerDescriptor` MUST imati:** `server_id`, `transport` (`LOCAL_STDIO|REMOTE_HTTPS`), `endpoint`, `expected_identity` (local executable digest ili remote issuer/audience/SPKI pin), `auth_method`, `requested_scopes[]`, `credential_ref`, `trust_policy_version`, `allowed_capabilities[]`, `max_tool_count`, `max_schema_bytes`, `max_description_bytes`, `expires_at`.
- **Stanja:** `DISCOVERED_UNTRUSTED→IDENTITY_VERIFIED→AUTHENTICATED→AUTHORIZED→CONNECTED`; mismatch/revocation vodi u `REJECTED/REVOKED` i zatvara transport.
- **Invarijante:** remote transport MUST koristiti TLS i verificirani peer; redirect/DNS reconnect zahtijeva ponovnu provjeru; credential broker šalje samo scope intersection i nikad credential u prompt/log; `tools/list` je untrusted `ContextBlock`; schema/description prolaze size, provenance i injection fencing prije registracije; cancel/reconnect pripada S7.
- **RED — `test_mcp_wrong_pinned_peer_gets_no_credentials`:** server vrati valjan TLS cert za drugo dopušteno ime, ali ne odgovara pin/issuer-audience zapisu; klijent MUST vratiti `MCP_PEER_IDENTITY_MISMATCH`, credential sink i tool registry moraju ostati prazni.

## P1.4 — `draft→approve→commit→verify→compensate` za ireverzibilne učinke (codex #16)

- **Vlasnik:** S6.9+S7; tool adapter implementira plan/commit/verify/compensate iza ugovora.
- **`EffectIntent` MUST imati:** `intent_id`, `effect_type`, `effect_class`, `principal_id`, nullable `tenant_id`, `target`, `canonical_payload`, `payload_hash`, `preview`, `idempotency_key`, `created_at`, `expires_at`, `required_approval`, nullable `compensator`, `recovery_owner`.
- **`ApprovalGrant` MUST imati:** `grant_id`, `intent_id`, `payload_hash`, `principal_id`, `allowed_effect`, `issued_at`, `expires_at`, `single_use_nonce`, `decision`.
- **Stanja:** `DRAFT→VALIDATED→APPROVAL_PENDING→APPROVED→COMMITTING→{COMMITTED|UNKNOWN}`; `COMMITTED→VERIFIED`; failure može u `COMPENSATING→COMPENSATED` ili `MANUAL_RECOVERY`. Cancel je legalan samo prije `COMMITTING`.
- **Invarijante:** intent se durable zapisuje prije vanjskog poziva; approval je vezan uz exact payload/target/effect i single-use; commit koristi intent-stabilan idempotency key; timeout nakon slanja daje `UNKNOWN`, ne automatski retry; uspjeh zahtijeva provider receipt + postcondition; compensator nije rollback tvrdnja dok nije verificiran.
- **RED — `test_approval_cannot_authorize_modified_effect`:** nakon previewa i approvala promijeniti recipient/amount uz isti grant; commit MUST vratiti `APPROVAL_BINDING_MISMATCH`, vanjski sink mora imati 0 poziva, a grant mora biti opozvan.

## P1.5 — Distribucijski artefakt obvezno aktivira S6.8 (codex #15)

- **Vlasnik:** `distributed-artifact` closure u S0.5; zahtijeva S6.8+S17.2+S17.3.
- **`ArtifactManifest` MUST imati:** `artifact_id`, `artifact_type`, `version`, `platform`, `artifact_digest`, `source_revision`, `build_recipe_digest`, `builder_identity`, `sbom_digest`, `provenance_attestation_digest`, `signer_id`, `signature`, `permission_delta`, `scan_policy_version`, `scan_result`, `update_metadata_version`, `update_metadata_expiry`, `release_channel`.
- **Stanja:** `BUILT→SCANNED→ATTESTED→SIGNED→STAGED→PUBLISHED`; kompromitacija/istek vodi u `REVOKED`; preskakanje stanja nije legalno.
- **Invarijante:** svi dokazi vežu isti `artifact_digest`; publish/update installer verificira signature, provenance, permission delta, vulnerability policy i non-expired update metadata prije byte writea; plugin-specifični scan može biti dodatak, ne zamjena baselineu.
- **RED — `test_post_signature_artifact_tamper_blocks_publish`:** promijeniti jedan byte staged binara nakon potpisa; publish MUST vratiti `ARTIFACT_DIGEST_MISMATCH`, release kanal i install target moraju ostati byte-identični prethodnom stanju.

## P1.6 — `channels→extensions` closure i jezgreni remote-HITL token (codex #10)

- **Vlasnik:** S0.5 deklarira DVA puta (HARDQ A1, 2026-09-03): `channel:builtin` (kompajliran u potpisani binary, statička registracija) `requires 7.3 + approval-core (6.0/12.4) + identity (6.6)` — integritet atestira potpis release-artefakta (P1.5); `channel:plugin` (dinamički učitan) `requires extensions + S6.8 + S11.5 + 7.3 + approval-core`. S6 posjeduje auth/approval, S7 durable wait/resume, S12.4 je samo multi-agent potrošač. Isti adapter-interface i conformance suite za oba puta.
- **`ChannelAdapterManifest` (SAMO `channel:plugin`) MUST imati:** `adapter_id`, `adapter_version`, `artifact_digest`, `channel_type`, `permissions[]`, `network_endpoints[]`, `provenance`, `signature`, `lifecycle_entry_id` (S11.5), `supply_chain_attestation_id` (S6.8). Built-in adapter nosi deskriptor bez extensions-only polja (`adapter_id`, `channel_type`, `permissions[]`, `network_endpoints[]`); njegov digest je digest release binarija.
- **`ApprovalChallenge` MUST imati:** `challenge_id`, `run_id`, `intent_id`, `payload_hash`, `principal_id`, `channel_identity`, `issued_at`, `expires_at`, `single_use_nonce`, `allowed_decisions[]`.
- **Stanja HITL-a:** `WAITING→{APPROVED|DENIED|EXPIRED|CANCELLED}`; samo S7 durable transition smije nastaviti run; ponovni odgovor na terminalni challenge je replay.
- **Invarijante:** `channel:plugin` adapter se ne registrira prije active `extensions` closurea; `channel:builtin` adapter se registrira statički i NE prolazi runtime extensions gate (atestiran release-potpisom); odgovor mora biti autentificiran kao isti principal+channel binding; token je exact-intent, expiring i single-use; approval ne može proširiti kernel policy; adapter ne može sam nastaviti run.
- **RED — `test_channels_contract_fails_closed` (tablični):** slučaj A registrira `channel:plugin` bez `extensions/S6.8` attestationa; slučaj B dvaput pošalje isti valjani approval odgovor (zajednički za builtin i plugin put); A MUST dati `INCOMPLETE_CAPABILITY_CLOSURE`, B `APPROVAL_REPLAY`, a u oba slučaja nema adapter side-effecta niti drugog resumea.
- *Change-record: builtin/plugin split usvojen jednoglasno u hard-questions rundi (HARDQ-CONSOLIDATED A1, 2026-09-03); prijašnji tekst je gate primjenjivao na sve kanale, što je blokiralo P0 Telegram.*

# P2 — pouzdanost i podaci

## P2.1 — `MEMORY_FORGET` nasuprot `DATA_PURGE` (codex #12)

- **Vlasnik:** S9 posjeduje `MEMORY_FORGET`; S6.7 posjeduje autoritativni `DATA_PURGE` i uvijek nadjačava S9.
- **`MemoryForget` MUST imati:** `operation_id`, `memory_ids[]` ili immutable `selection_snapshot_hash`, `principal_id`, `reason`, `requested_at`, nullable `reversible_until`, `tombstone_version`; učinak je isključivo zabrana recalla/rankinga.
- **`DataPurge` MUST imati:** `purge_id`, `subject_id`, nullable `tenant_id`, `authority`, `scope`, `store_targets[]`, `requested_at`, `approval_hash`, `legal_hold_check`, `per_store_status[]`, `backup_disposition`, nullable `completed_at`.
- **Stanja forgeta:** `ACTIVE→FORGOTTEN→RESTORED` do roka. **Stanja purgea:** `REQUESTED→AUTHORIZED→RUNNING→{COMPLETED|PARTIAL|BLOCKED_LEGAL_HOLD|FAILED}`; `COMPLETED` je ireverzibilan.
- **Invarijante:** forgotten sadržaj ostaje arhiviran ali nikad u recallu; purge briše hot/warm/cold, vektor, graf, cache, indekse i izvedene exporte te definira backup expiry/crypto-erasure; purge briše i forget tombstone content; `COMPLETED` tek nakon potvrde+post-delete probe svakog storea; audit čuva samo minimalni non-content dokaz.
- **RED — `test_purge_cannot_complete_with_residual_copy`:** isti subject staviti u memory, vector, cache i backup index; jedan adapter lažno vrati success bez brisanja; post-delete probe MUST postaviti stanje `PARTIAL`, vratiti `PURGE_INCOMPLETE` i zabraniti `COMPLETED` događaj.

## P2.2 — Izvršni bounded resource budgets (codex #17)

- **Vlasnik:** S7.2 accounting/cancel; S4.2/S6.2 OS enforcement; adapter ne smije sam proglasiti limit provedenim.
- **`ResourceBudget` MUST imati po runu i tool attemptu:** `wall_ms`, `cpu_ms`, `rss_bytes`, `disk_write_bytes`, `file_count`, `process_count`, `socket_count`, `network_in_bytes`, `network_out_bytes`, `output_bytes`, `input_tokens`, `output_tokens`, `cost_minor_units`, `tool_calls`, `loop_steps`; svaki limit ima `SOFT|HARD` i jedinicu.
- **`ResourceLedger` MUST imati:** `budget_id`, `parent_budget_id`, `reserved`, `consumed`, `observed_at`, `source`, `fencing_token`.
- **Stanja:** `AVAILABLE→RESERVED→CONSUMING→{RELEASED|EXHAUSTED|CANCELLED}`.
- **Invarijante:** child budget ≤ preostali parent; kumulativni counteri monotono rastu, a gaugeovi (`rss/process/socket`) knjiže peak; hard limit atomically fencea nove radnje, cancela i drain/kill cijelo owned stablo; soft limit samo emitira signal; cleanup se ne knjiži kao uspjeh; supported platform mora imati realni enforcement ili capability fail-closed.
- **RED — `test_hard_process_budget_kills_descendants`:** tool s `process_count=2` pokrene parent+2 child procesa; treći proces MUST izazvati `RESOURCE_LIMIT_EXCEEDED`, cijelo owned stablo mora završiti, nijedan kasniji tool ne smije krenuti, ledger mora pokazati `EXHAUSTED`.

## P2.3 — Queue lease, reclaim i fencing (codex #18)

- **Vlasnik:** S7.2 semantika; S18.1 distributed adapter.
- **`QueueTask` MUST imati:** `task_id`, `payload_hash`, `idempotency_key`, `state`, `attempt_no`, `max_attempts`, nullable `lease_id`, nullable `owner_worker_id`, `fencing_token`, nullable `leased_at`, nullable `heartbeat_at`, nullable `lease_expires_at`, nullable `result_hash`.
- **Stanja:** `READY→LEASED→RUNNING→COMMIT_PENDING→SUCCEEDED`; expiry/crash daje `LEASED/RUNNING→READY` uz novi attempt i strogo veći fencing token; `COMMIT_PENDING` s nepoznatim vanjskim učinkom ide u `UNKNOWN_EFFECT→RECONCILING`, nikad izravno u `READY`; terminalno još `FAILED|DEAD_LETTER|CANCELLED`.
- **Invarijante:** claim/reclaim je atomski compare-and-swap; heartbeat vrijedi samo za aktualni lease+token; svaka state mutacija i result/side-effect commit nosi aktualni fencing token; stari token se odbija; ACK tek nakon durable result commit; jedan task+idempotency key ima najviše jedan prihvaćen terminalni rezultat; ireverzibilni commit koristi P1.4 reconciliation ugovor.
- **RED — `test_reclaimed_worker_cannot_commit_with_stale_fence`:** worker A dobije token 7 i bude zamrznut (`SIGSTOP`) preko expiryja; worker B dobije token 8; nakon nastavka A pokuša commit s 7 dok B commita s 8; A MUST dobiti `STALE_FENCING_TOKEN`, a durable store mora sadržavati točno jedan rezultat, B-ov.

## P2.4 — Cache tenant/principal/policy/provenance granica (codex #19)

- **Vlasnik:** S8.4; S6.6/S10.6 daju authz/policy input, S6.7 purge hook.
- **`CacheKey` MUST imati:** `namespace`, `tenant_id` (non-null u service modu), `principal_scope_hash`, `authz_policy_version`, `sensitivity`, `source_or_corpus_version`, `model_id`, `provider_id`, `prompt_template_hash`, `tool_schema_hash`, `content_hash`, `purge_generation`.
- **`CacheValue` MUST imati:** `value_hash`, `lineage[]`, `created_at`, `expires_at`, `producer_revision`, `encryption_key_ref` kad je dopušten osjetljiv sadržaj.
- **Stanja:** `MISS→WRITTEN→HIT`; policy/revocation/purge vodi u `INVALIDATED`; expired/revoked entry nikad nije `HIT`.
- **Invarijante:** lookup zahtijeva cijeli typed key, bez wildcard fallbacka; namespace je tenant-partitioned; authz se evaluira prije lookup/writea; `SECRET` je `NO_STORE` po defaultu; policy/corpus/model/prompt promjena mijenja key; purge/revocation atomically povećava generation i onemogućuje stare entryje.
- **RED — `test_cross_tenant_cache_key_cannot_alias`:** tenant A upiše entry, tenant B pošalje isti content/prompt hash i pokuša izostaviti ili krivotvoriti tenant dio; schema/authz MUST vratiti `CACHE_SCOPE_MISMATCH`, B dobiva MISS i nikad sadržaj A.

## P2.5 — Provider data-processing i rezidencijski preflight (codex #13)

- **Vlasnik:** S0.3/S2.1 descriptor; S6.7/S6.9 enforcement prije mrežnog sinka; svaki fallback ponovno prolazi gate.
- **`ProviderDataDescriptor` MUST imati:** `provider_id`, `model_id`, `endpoint`, `processing_regions[]`, `storage_regions[]`, `retention_seconds`, `zero_retention`, `training_use`, `human_review`, `encryption_in_transit`, `encryption_at_rest`, `accepted_data_classes[]`, `subprocessor_policy_version`, `dpa_version`, `descriptor_source`, `descriptor_digest`, `signature`, `attested_at`, `expires_at`.
- **`RequestDataPolicy` MUST imati:** `principal_id`, nullable `tenant_id`, `purpose`, `data_classes[]`, `allowed_processing_regions[]`, `max_retention_seconds`, `training_allowed`, `human_review_allowed`, `required_encryption`, `provider_allowlist[]`, nullable `consent_ref`.
- **Stanja:** provider target `CANDIDATE→POLICY_EVALUATED→AUTHORIZED→SENT`; unknown/expired descriptor ili mismatch vodi u `DENIED`, ne fallback-send.
- **Invarijante:** evaluacija se događa prije serializacije i DNS/connecta; unknown vrijednost je najrestriktivnija/deny; request policy i provider descriptor koriste intersection; redakcija ne smije lažno sniziti data class bez lineagea; svaki fallback/reroute se ponovno autorizira; descriptor/decision se auditira bez prompt sadržaja.
- **RED — `test_fallback_cannot_bypass_residency_policy`:** primarni EU/zero-retention provider vrati retryable error, fallback je US+training; S7 MUST dobiti `DATA_POLICY_DENIED`, fallback network sink mora imati 0 poziva, a audit mora sadržavati denied provider ID i policy version bez sadržaja prompta.

## P2.6 — Automation clock, DST, missed-run i overlap semantika (codex #33)

- **Vlasnik:** S3.6; S7 daje idempotency/lease, ne rasporednu semantiku.
- **`ScheduleManifest` MUST imati:** `schedule_id`, `schedule_version`, `expression`, `iana_timezone`, `dst_gap_policy` (`SKIP|NEXT_VALID|ERROR`), `dst_fold_policy` (`ONCE_FIRST|ONCE_SECOND|TWICE`), `missed_run_policy` (`SKIP|COALESCE|CATCH_UP`), `max_catch_up`, `max_lateness_ms`, `overlap_policy` (`FORBID|QUEUE|PARALLEL`), `max_concurrency`, `start_at`, nullable `end_at`.
- **`ScheduledOccurrence` MUST imati:** `schedule_id`, `scheduled_local_time`, `scheduled_instant_utc`, `fold_index`, `occurrence_id`, nullable `admitted_at`; za `TWICE` occurrence/idempotency MUST uključiti UTC instant+fold index, a za `ONCE_FIRST|ONCE_SECOND` samo policy-odabrani fold smije proizvesti occurrence.
- **Stanja:** `CALCULATED→DUE→{ADMITTED|MISSED|COALESCED|SUPPRESSED_OVERLAP}→RUNNING→TERMINAL`.
- **Invarijante:** raspored čuva UTC instant i IANA zonu; wall clock računa occurrence, monotoni clock mjeri lateness/lease; restart ponovno računa iste occurrence ID-eve; missed/overlap politika je eksplicitna, bounded i auditirana; `FORBID` je single-flight.
- **RED — `test_zagreb_dst_fold_runs_once_across_restart`:** raspored `30 2 * * *`, zona `Europe/Zagreb`, `ONCE_FIRST`, restart između dva pojavljivanja 02:30 pri jesenskom DST foldu; scheduler MUST proizvesti točno jedan occurrence/run i jedan idempotency key, a drugi fold ne smije doći do admissiona ni side-effecta.

## P2.7 — Formalni Council ugovor, nova S12.6 (agy #5)

- **Vlasnik:** nova S12.6; S16.6 ga konzumira kao `multi-agent` strategiju, ali deterministic checker/promotion gate ostaje neovisan.
- **`CouncilRequest` MUST imati:** `council_id`, `run_id`, `artifact_revision_hash`, `question`, `member_specs[]` (`member_id`,`role_id`,`model_policy`,`context_policy`), `min_quorum`, `independence_policy`, `anonymization_policy`, `evidence_refs[]`, `deadline`, `budget_id`, `chair_spec`, `aggregation_rule`, `dissent_policy`, `output_schema_version`.
- **`ReviewRecord` MUST imati:** `review_id`, `sealed_input_hash`, `member_pseudonym`, `finding_id`, `claim`, `evidence_refs[]`, `severity`, `confidence`, `submitted_at`, `signature`.
- **Stanja:** `FORMED→INPUTS_SEALED→INDEPENDENT_REVIEW→CROSS_REVIEW→SYNTHESIS→{VERIFIED|FAILED_QUORUM|REJECTED}`; član ne vidi tuđe zapise prije vlastitog sealed commita.
- **Invarijante:** svi članovi rade nad istim artifact hashom; cross-review skriva identitet; chair ne smije mijenjati evidence ref ni izbaciti materijalni dissent bez eksplicitnog adjudication recorda; quorum i budget su hard gate; output nosi majority, dissent i unresolved; council ne može sam promovirati niti zamijeniti 16.6 checker.
- **RED — `test_council_cannot_drop_sealed_material_dissent`:** jedan sealed review s materijalnim dokazom proturječi većini, a chair synthesis ga izostavi; verifier MUST vratiti `DISSENT_DROPPED`, council ostaje `REJECTED` i promotion gate ne smije dobiti success signal.

---

# Annex A — dopuna: ugovori P0.5–P0.13 (formalizacija citiranih gate-ova; REVIEW2 G1)

Ovih 7 ugovora plan je citirao kao gate a nisu bili u Annexu (agy GATE-01). Sad formalizirani.
(Numeracija nekontiguirana — P0.10/P0.12 namjerno prazni; zadržani IDovi kako ih plan citira.)

## P0.5 — Capability-matrix fail-closed (dizajn: DESIGN-S0-sandbox-codex)
- **Vlasnik:** S0.3 negotiation. `Effective()=min(declared,measured)`; nepoznato → error, NE pretpostavka.
- **MUST:** measurement nosi target/version/config + measured-vector + probe-id/rev + evidence + expiry + hash; promjena bilo čega invalidira grant.
- **RED — `test_capability_unknown_fails_closed`:** provider bez izmjerene sposobnosti → route/dispatch odbijen, nula pretpostavljenih sposobnosti.

  **AMENDMENT (Phase-1B r2 review, 2026-09-03) — P0-min attestation vector:** u P0
  (T11 sealed startup snapshot, HARDQ B9: bez runtime aktivacije) measurement nosi
  {probe name, passed, detail, config-hash}; config-hash MORA biti sha256 digest
  RESOLVED konfiguracije (shape-validiran, ne proizvoljan string) i snapshot ga
  TRAJNO nosi. Probe-id/rev, expiry i measured-vector stižu s pravim S0.3
  negotiation vlasnikom (P1) — freshness je u P0 strukturalan: probe se mjeri
  jednom pri startu, snapshot umire s procesom, promjena configa = restart =
  novo mjerenje. Mismatch hash → capability OFF (stale, fail closed).

## P0.6 — Config-bounds + process-identity
- **Vlasnik:** S1.1 config, S1.2 proc.
- **MUST:** config NE smije proširiti kernel floor (egress/sandbox/budget); reload nevaljan → stara generacija ostaje. Proces se identificira PID+start-token (PID sam nije dokaz vlasništva).
- **RED — `test_config_cannot_widen_kernel_floor`** + **`test_process_identity_requires_start_token`**.

## P0.7 — Structured-output nikad tiho prihvaćen
- **Vlasnik:** S2.3.
- **MUST:** nevalidan T → salvage/re-ask (re-ask nosi S7 AttemptGrant, P0.2); NIKAD tiho prihvati. Za security/effect payload nema tolerantnog accepta.
- **RED — `test_structured_output_never_silent_accept`.**

## P0.8 — Hardware-fit fail-closed
- **Vlasnik:** S2.4. **Activation trigger (HARDQ C6): `local-inference` — NIJE P0 gate;** ID zadržan radi citata.
- **MUST:** model > izmjereni RAM/VRAM → `Fits=false`+prijedlog kvantizacije, NIKAD OOM-pokušaj.
- **RED — `test_model_over_capacity_refused_not_oom`.**

## P0.9 — In-turn verify nikad tiho
- **Vlasnik:** S3.5. **Activation trigger (HARDQ C6): `coding` profil — NIJE P0 gate;** ID zadržan radi citata.
- **MUST:** dijagnostika (lint/test/exit) vraća se U ISTOM turnu; korak se NE prihvaća dok ne prođe; TIA nije completion-gate dok paired full-suite ne dokaže 0 promašenih regresija.
- **RED — `test_verify_failure_blocks_step_same_turn`.**

## P0.11 — Shadow-checkpoint integritet
- **Vlasnik:** S5.1/5.3. **Activation trigger (HARDQ C6): u P0 vrijedi SAMO `AtomicWriter` klauzula (P0 file-mutacije); shadow-checkpoint/rollback dio gates `coding`/user-workspace mutaciju.**
- **MUST:** `Rollback` vraća byte-identično pre-stanje za podržani scope (content+eksplicitna metadata); NE dira ne-staged korisničke promjene; snapshot PRIJE svakog FS-efekta; atomic write = stari ILI novi, nikad pola. Potpis receipta = HMAC/ed25519 (ne raw concat, DESIGN-symedit-crypto D2).
- **RED — `test_rollback_byte_identical`** + **`test_atomic_write_no_partial`.**

## P0.13 — Decay nikad ne briše bajtove
- **Vlasnik:** S9.4. **Activation trigger (HARDQ B8, 2026-09-03): P1 `memorija` decay — NIJE P0 gate.** P0 memorija = eksplicitne činjenice bez decaya; kad se Decay aktivira (P1, uz izmjeren retrieval problem), ovaj ugovor vrijedi u cijelosti i fact-tip je izuzet (S=∞).
- **MUST:** `Decay` mijenja RANG ne postojanje; original UVIJEK u hot/warm/cold arhivi; konsolidacija untrusted epizode → gist ostaje UNTRUSTED (P0.3 lineage). `Forget`=tombstone (reverzibilno) ≠ `DATA_PURGE` (P2.1).
- **RED — `test_decay_never_destroys_bytes`.**

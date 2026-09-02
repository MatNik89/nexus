# Adversarijalni Inženjerski Review: Sekcije S13–S18 i Addendumi A1–A10
## Dekonstrukcija Planiranih Rupa, Scope-Rizika i Razrješenje Proturječja Identiteta

**Datum:** 2026-09-02  
**Autor:** agy (Gemini 3.7 Flash)  
**Temeljna premisa:** Nexus se gradi kao **100% ČISTI GREENFIELD U GO (`CGO_ENABLED=0`)**. Svi "Salvage: core/X.py", "GORTEX Go REUSE 1:1", "NEXUSv2" i Python-port retci u planu su **MRTVI i nepostojeći**. Validan izvor je isključivo kôd iz 29 auditiranih svjetskih repozitorija ili konkretno specificiran Go dizajn. Sve ostalo je **nepokrivena rupa**.

---

## 1. STRUKTURNE RUPE BEZ REALNOG IZVORA IMPLEMENTACIJE

Gubitkom Python salvagea, niz podsekcija u S13–S18 i Addendumima ostao je na razini jedne rečenice ili s referencama na Python/Node/Rust biblioteke koje **ne mogu postojati u čistom Go single-binarcu bez CGO-a**.

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        PREGLED NEPOKRIVENIH RUPA (S13–S18 & A1–A10)                    │
├─────────┬───────────────────────────────┬──────────────────────────────────────────────┤
│ ID      │ Podsekcija u Planu            │ Stvarni Problem i Izvor Iluzije              │
├─────────┼───────────────────────────────┼──────────────────────────────────────────────┤
│ S13.1   │ Vision (`vision`)             │ browser-use je Python; nema Go CDP/VLM koda  │
│ S13.3   │ Voice (`voice`)               │ pipecat/LiveKit/speaches su Python/C++       │
│ S13.4   │ Video (`video`)               │ Remotion je Node.js/React; WhisperX je PyTorch│
│ S13.5   │ Document Mutation (`documents`)│ pypdf/OCRmyPDF/pyHanko su Python biblioteke  │
│ S14.4   │ IDE / ACP Protocol            │ ACP standard nema službeni Go SDK            │
│ S16.5   │ Trajectory Export             │ Jednorečenični popis formata bez Go dizajna  │
│ S11.3   │ Dynamic Prompt Compression    │ Nema algoritma za dinamičko rezanje vještina │
│ A1      │ OCR Hardware Routing          │ Unlimited-OCR je 3B VLM; OCRmyPDF je Python  │
│ A2      │ Local-Model Freshness Scout   │ llmfit je Rust binarni paket                 │
│ A4.G1   │ Computer-Use GUI Input        │ Zahtijeva OS-specifične C/X11/Win32 hookove  │
└─────────┴───────────────────────────────┴──────────────────────────────────────────────┘
```

### 1.1. Detaljna dekonstrukcija pojedinih rupa i inženjersko rješenje:

#### [S13.1] Vision & Browser-Use
- **Problem:** Plan navodi `browser-use screenshot+DOM / Open-Interpreter / OpenHands` i `Salvage: core/vision.py`. `browser-use` je Python Playwright framework, a `core/vision.py` ne postoji u Go-u.
- **Što stvarno treba:** 
  1. Pure-Go VLM payload encoder (`encoding/base64`, provjera MIME tipa `image/jpeg`, `image/png`, skaliranje preko pure-Go `golang.org/x/image/draw`).
  2. Za browser interakciju: integracija s pure-Go Chrome DevTools Protocol bibliotekom (`github.com/chromedp/chromedp`) ili delegiranje na vanjski MCP Browser poslužitelj.

#### [S13.3] Voice Conversation Runtime
- **Problem:** Plan navodi `pipecat/LiveKit-Agents/speaches STT+TTS` i `Salvage: core/voice.py`. Sve navedeno su teški Python/WebRTC servisi. Ubacivanje WebRTC/VAD/barge-in logike u Go single binary bez CGO-a i vanjskih ovisnosti je iluzija.
- **Što stvarno treba:**
  1. Odvojiti **Voice Core (Adapter)** od **Voice Conversational Runtimea**.
  2. Za P0/P1: Isključivo REST/WebSocket klijentski adapter prema vanjskim TTS/STT providerima (OpenAI Audio, ElevenLabs, Deepgram, Whisper API).
  3. Lokalni streaming WebRTC / LiveKit agent mora biti **izdvojeni sidecar kontejner**, nikako dio temeljnog Go binarca.

#### [S13.4] Video Processing & Remotion
- **Problem:** Plan navodi `Remotion/HyperFrames (HTML→MP4)` i `WhisperX (word-level diarization)`. Remotion zahtijeva Node.js/Chromium/React runtime, a WhisperX zahtijeva Python/PyTorch/CUDA stog.
- **Što stvarno treba:**
  1. Ukloniti iluziju "in-process" obrade videa.
  2. Go jezgra smije imati isključivo `pkg/media/ffmpeg.go` wrapper koji pokreće lokalno instalirani `/usr/bin/ffmpeg` kroz sigurni `exec.CommandContext` s timeoutom i Landlock ograničenjem.

#### [S13.5] Document Mutation (PDF / DOCX)
- **Problem:** Plan se oslanja na `pypdf`, `OCRmyPDF`, `pyHanko` i `core/pdf.py`. U pure-Go okruženju (`CGO_ENABLED=0`) te biblioteke ne postoje.
- **Što stvarno treba:**
  1. Za inspekciju i manipulaciju PDF-a koristiti pure-Go biblioteke: `github.com/pdfcpu/pdfcpu` (Apache-2.0, za split/merge/watermark/metadata) i `rsc.io/pdf` za čitanje teksta.
  2. Zadržati ugovor iz S13.5: svaka mutacija mora ići kroz preview $\to$ staging $\to$ atomic commit.

#### [S14.4] IDE / ACP (Agent Client Protocol)
- **Problem:** Plan navodi `Agent-Client-Protocol standard (Rust/TS/Py/Java/Kotlin SDK)`. Go **nije podržan** službenim ACP SDK-om.
- **Što stvarno treba:**
  1. Napisati vlastiti lagani JSON-RPC 2.0 stdio poslužitelj u `pkg/interfaces/acp/server.go`.
  2. Definirati Go structove za ACP poruke (`InitializeRequest`, `NewSessionRequest`, `PromptRequest`, `ToolCallRequest`).

#### [S16.5] Trajectory Export
- **Problem:** Jedna rečenica u planu (`SWE-gym/Axolotl/ShareGPT-JSONL`) bez definicije formata, sanitizacije ili koda.
- **Što stvarno treba:**
  1. Implementirati `pkg/journal/export.go` koji čita append-only SQLite journal sesije i projicira ga u standardni ShareGPT/OpenAI Fine-Tuning JSONL format (`{"messages": [{"role": "...", "content": "..."}]}`).
  2. Obavezna primjena redaction filtera (uklanjanje API ključeva i tajni) prije izvoza.

---

## 2. SCOPE-RIZIK: REALNOST GREENFIELD GRADNJE (SOLO + CLAUDE)

Plan s 82 podsekcije, 18 flagova, 17 Tier-3 ugovora, grafikom, videom, glasom, OCR-om, A2A mrežom, flotama i desktop aplikacijom je **recept za paralizu i nikad dovršen projekt**. 

Mora se povući **stroga, bespoštedna Pareto granica** između P0 (Temelj), P1 (Proširenja) i P2 (Odgoda).

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                          RUTHLESS TRIJAGE: P0 vs P1 vs P2                              │
├────────────────────────────────┬───────────────────────────────────────────────────────┤
│ P0: TEMELJ (Dan 1–14)          │ • S0 (17 Tier-3 ugovora, Go FSM, SQLite WAL journal)  │
│ *Single-Binary CLI/TUI*        │ • S1 (Process lifecycle, Pdeathsig, Signal handler)   │
│ *Ultra-brzo, bez CGO-a*        │ • S2 (Provider sloj, AccountFleet, Pricing ceiling)   │
│ *Fokus: Neprobojni Coding Loop*│ • S3 (Attempt-Execute-Verify, Doom loop, Evidence Gate)│
│                                │ • S4 (FS alati, Bash, 9-stage replacer, LSP inject)   │
│                                │ • S5 (Shadow Git snapshot & rollback)                 │
│                                │ • S6 (Landlock v5 LSM + Seccomp kernel sandbox)       │
│                                │ • S7 (SQLite lease queue, CAS fencing, Outbox N9)     │
│                                │ • S8 (Token compaction, Prompt-cache split, sync.Pool)│
│                                │ • S9 (5-Scope Memory Spine u SQLite WAL + Forget P2.1)│
│                                │ • S11 (System prompt, AGENTS.md / SKILL.md parser)    │
│                                │ • S14.1 (Bubbletea TUI & CLI dispatcher)              │
│                                │ • S16.6 (Programski Evidence-Graded Checker)          │
│                                │ • S17.1 (Cordis DI container kernel)                  │
├────────────────────────────────┼───────────────────────────────────────────────────────┤
│ P1: ASISTENT & KANALI (Dan 15+)│ • S12.1/S12.5 (Subagent delegacija, Swarm handoff)    │
│ *Osobni Asistent Proširenja*  │ • S14.5 (Messaging gateway: Telegram & Discord samo)  │
│                                │ • S10 (Hybrid search: SQLite FTS5 + Ollama embedder)  │
│                                │ • A1/A2 (OCR CLI wrapper + Model Scout HTTP sweep)    │
│                                │ • A4.G2/G4 (PersonalProfile izolacija, ObligationStore)│
├────────────────────────────────┼───────────────────────────────────────────────────────┤
│ P2: ODGOĐENO / TEŠKI ENTERPRISE│ • Desktop GUI (Wails/Fyne) ─── TUI & Web dashboard    │
│ *Van opsega osnovne gradnje*   │   su sasvim dovoljni; Wails dodaje CGO/Webview ovisnost│
│                                │ • Video / Remotion obrada ─── nije core asistent funk.│
│                                │ • Voice WebRTC LiveKit ─── preskupo za solo Go build  │
│                                │ • Computer-Use GUI klikanje ─── nestabilno i rizično  │
│                                │ • Device Pairing & Fleet management ─── enterprise    │
│                                │ • Kubernetes Argo Canary rollout (S18) ─── enterprise │
└────────────────────────────────┴───────────────────────────────────────────────────────┘
```

---

## 3. RAZRJEŠENJE PROTURJEČJA IDENTITETA

### 3.1. Priroda Konflikta u Dokumentaciji
- **Vrh plana (S0–S6):** Formuliran kao *"Coding-harness prvorazredni cilj"* s fokusom na SWE-bench, diff verifikaciju, AST analizu i kompajlerske petlje.
- **Addendum A3.1:** Formuliran kao *"Nexus JE prvenstveno UNIVERZALNI OSOBNI MULTIPLATFORM ASISTENT (kao Hermes), a kodiranje je samo jedna grana znanja"*.

Ako se ovaj konflikt ne razriješi na razini koda, doći će do **arhitektonske shizofrenije**:
- Osobni asistent treba ležernu komunikaciju na Telegramu, toleranciju na neformalne upute i upravljanje obvezama.
- Coding harness treba rigorozan Evidence Gate (`git diff > 0 && exit == 0`), stroge granice repozitorija i fail-closed ponašanje.

### 3.2. Kanonsko Inženjersko Rješenje: "Jedna Jezgra, Dva Profila"

Nexus **NIJE** dva odvojena sustava niti kompromisni hibrid. Nexus je:

> **Deterministički, Sigurni Kernel za Izvršavanje Zadataka (Engine Level) koji pokreće Specijalizirane Profile Ponašanja (Capability Level).**

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                              NEXUS UNIFICIRANA ARHITEKTURA                             │
│                                                                                        │
│  ┌──────────────────────────────────────────────────────────────────────────────────┐  │
│  │ SUČELJA: TUI (Terminal), CLI, Web Dashboard, Kanali (Telegram, Discord)          │  │
│  └────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                           │                                            │
│  ┌────────────────────────────────────────┴─────────────────────────────────────────┐  │
│  │ PROFILI PONAŠANJA (Behavior Profiles):                                            │  │
│  │                                                                                   │  │
│  │  ┌──────────────────────────────────────┐  ┌───────────────────────────────────┐  │  │
│  │  │ ASSISTANT PROFILE (Personalni mod)   │  │ CODING PROFILE (Inženjerski mod)  │  │  │
│  │  │ • Telegram/Discord chat              │  │ • Rigorozni Evidence Gate         │  │  │
│  │  │ • ObligationStore (ciljevi/rokovi)   │  │ • Shadow Git & Staging            │  │  │
│  │  │ • PersonalProfile (posao/obitelj)    │  │ • Live LSP dijagnostika           │  │  │
│  │  │ • Model Scout & Obavijesti           │  │ • Test Impact Analysis (TIA)      │  │  │
│  │  └──────────────────────────────────────┘  └───────────────────────────────────┘  │  │
│  └────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                           │                                            │
│  ┌────────────────────────────────────────┴─────────────────────────────────────────┐  │
│  │ ZAJEDNIČKA JEZGRA (S0–S9, S17.1):                                                 │  │
│  │ • 17 Tier-3 ugovora (Append-only SQLite Journal, Jedan Write-Owner P0.3)         │  │
│  │ • Landlock v5 LSM Kernel Sandbox (S6) • AccountFleet & Provider Layer (S2)       │  │
│  │ • Cordis DI Container (S17.1) • 5-Scope Memory Spine (S9)                        │  │
│  └──────────────────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

#### Pravila Unifikacije:
1. **Jedan izvršni runtime:** Nema paralelnih petlji. Sav rad ide kroz standardni `TurnLoop` u S3.
2. **Gating ovisi o profilu:** 
   - Ako je zadatak pokrenut u `CodingProfile` modu $\to$ aktivira se **S16.6 Evidence-Graded Gate** (nema završetka bez `git diff > 0` i prolaska testova).
   - Ako je zadatak pokrenut u `AssistantProfile` modu $\to$ odgovor se vraća korisniku bez zahtjeva za git diffom, ali se bilježi u **ObligationStore** i **Memory Spine**.
3. **Puna separacija tajni i memorije:** `PersonalProfile` (G2) jamči da poslovni kod i privatni razgovori nikada ne dijele SQLite memorijski kontekst.

---

## 4. ZAKLJUČAK I SMJERNICE ZA P0 GRADNJU

1. **Odbaciti sve iluzije o Python salvageu.** Svi paketi pišu se od nule u čistom Go-u (`CGO_ENABLED=0`).
2. **Implementirati P0 opseg bez odgode.** Fokusirati se isključivo na S0–S9, S14.1 (TUI), S16.6 i S17.1.
3. **Odgoditi P2 komponente (Desktop GUI, Video, LiveKit Voice, Fleet)** do stabilizacije jezgre.
4. **Zadržati 4 dokazana diferencijatora:** Evidence-Graded Loop, TIA, AST symedit i formalni Memory Forget protokol unutar `CodingProfile` i `AssistantProfile` okvira.

PLAN S14

# Hibrid-Hibrida Sinteza: Sekcija S14 (Korisnička Sučelja i UX)
## CORE JEZGRA JE LIBRARY — TANKI ADAPTERI (TUI, SDK, WEB, IDE, CHANNELS)

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S14.1–S14.5)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P1.6, N9 Outbox)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `charmbracelet/bubbletea`, `embed`, `net/http`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Arhitektonska Strategija: Jezgra kao Biblioteka (Core-as-Library)

Sekcija S14 provodi temeljno načelo dizajna modernih aplikacija:
1. **Jezgra je Samostalna Go Biblioteka (`pkg/engine`):** Sav rad (petlja, alati, sigurnost, memorija, retry) nalazi se u čistom Go API-ju.
2. **Sva Sučelja su Tanki Adapteri:**
   - **CLI / TUI (14.1):** Bubbletea TUI kompajliran unutar istog statičkog binara.
   - **Embeddable SDK (14.2):** **KLJUČNI INVARIJANT:** SDK koristi **potpuno isti izvršni put** (S0 koverte, S6.9 lifecycle chokepoint, S7 execution supervisor) kao i CLI — nema posebnih "zaobilaznih" executora!
   - **Web UI & Dashboard (14.3):** Ugrađeni statički asseti (`embed.FS`) posluženi kroz `nexus serve`.
   - **IDE & ACP (14.4):** Agent Communication Protocol (ACP) standard preko stdio/JSON-RPC.
   - **Messaging Gateway (14.5):** 6 kanala (Telegram, Slack, Discord, WhatsApp, Matrix, Webhook) iza unificiranog ugovora s **Annex A P1.6** closure provjerom (`channels requires extensions`) i **N9 Transactional Outbox** pouzdanom dostavom.

---

## 2. Arhitektura S14 Sustava Sučelja

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    S14 UX & INTERFACES                                  │
│                                                                                         │
│  ┌───────────────┐ ┌───────────────┐ ┌───────────────┐ ┌───────────────┐ ┌─────────────┐│
│  │ 14.1 CLI/TUI  │ │ 14.2 SDK API  │ │ 14.3 WEB UI   │ │ 14.4 IDE/ACP  │ │14.5 GATEWAY ││
│  │ Bubbletea     │ │ Embeddable Go │ │ Embedded SPA  │ │ Stdio /       │ │Telegram/    ││
│  │ Lipgloss /    │ │ Library API   │ │ Dashboard     │ │ JSON-RPC      │ │Slack/Discord││
│  │ Desktop Wails │ │ (S0/6.9/7.x)  │ │ (embed.FS)    │ │ (Zed/Cursor)  │ │(Annex P1.6) ││
│  └───────┬───────┘ └───────┬───────┘ └───────┬───────┘ └───────┬───────┘ └──────┬──────┘│
│          │                 │                 │                 │                │       │
│          └─────────────────┴────────┬────────┴─────────────────┴────────────────┘       │
│                                     │ (Jedinstveni Application-API Poziv)               │
│                                     ▼                                                   │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ APPLICATION CORE ENGINE (pkg/engine)                                              │  │
│  │   • S0 Envelope & Schema • S6.9 Lifecycle Chokepoint • S7 Execution Supervisor    │  │
│  │   • S1.3 EventJournal (OTel SSE Stream) • N9 Durable Delivery Outbox              │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S14.1: CLI i Terminalsko Sučelje (Bubbletea TUI i Wails Desktop)

- **Tip:** `ADAPTER` (Go paket `pkg/interfaces/tui` i `cmd/nexus`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Pure-Go Reaktivni TUI — Single Binary):* **Codex CLI / crush + charmbracelet/bubbletea** — model-view-update arhitektura (Elm stil), nula vanjskih dinamičkih C biblioteka, ncurses zamijenjen čistim Go ANSI rendererom (`lipgloss`, `bubbles`).
  - *Aspekt B (Strujanje Odgovora i Status Alata u Realnom Vremenu):* **Aider TUI + Claude Code** — live diff preglednici, animirani spinneri statusa alata, sintaksno isticanje koda (chroma).
  - *Aspekt C (Ista Jezgra za Desktop):* **Wails / Fyne** — grafička desktop aplikacija dijeli 100% isti Go engine bez IPC uskih grla.
- **Sinteza (Go dizajn skica):**
  ```go
  package tui

  import (
      "context"
      "fmt"

      tea "github.com/charmbracelet/bubbletea"
      "github.com/charmbracelet/lipgloss"
      "nexus/pkg/contract"
      "nexus/pkg/engine"
  )

  type MainModel struct {
      engine      *engine.ApplicationEngine
      sessionID   string
      messages    []contract.Message
      inputPrompt string
      isStreaming bool
      statusMsg   string
      err         error
  }

  func NewMainModel(eng *engine.ApplicationEngine, sessionID string) MainModel {
      return MainModel{
          engine:    eng,
          sessionID: sessionID,
      }
  }

  func (m MainModel) Init() tea.Cmd {
      return nil
  }

  func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
      switch msg := msg.(type) {
      case tea.KeyMsg:
          switch msg.String() {
          case "ctrl+c", "q":
              return m, tea.Quit
          case "enter":
              // Pošalji prompt engineu
              m.statusMsg = "Running tool pipeline..."
              m.isStreaming = true
              return m, m.submitPromptCmd(m.inputPrompt)
          }
      case contract.StreamDelta:
          // Obradi pristigli delta token (S3.2)
          m.statusMsg = fmt.Sprintf("Receiving tokens: %s", msg.DeltaText)
          return m, nil
      }
      return m, nil
  }

  func (m MainModel) View() string {
      titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
      return fmt.Sprintf("%s\n\nSession: %s\nStatus: %s\n> %s",
          titleStyle.Render("NEXUS HARNESS TUI"), m.sessionID, m.statusMsg, m.inputPrompt)
  }

  func (m MainModel) submitPromptCmd(prompt string) tea.Cmd {
      return func() tea.Msg {
          // Poziva zajednički engine API
          return nil
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `cli.py` i `tui/` modula u čisti Go Bubbletea model u `pkg/interfaces/tui`.
- **Ugovor / RED Gate:**
  - **Annex A P0.3**; RED test: `test_tui_renders_streaming_deltas_from_journal_stream`.
- **Verifikacija aspekata:**
  - charmbracelet/bubbletea (MIT, Go standard za TUI), lipgloss (MIT).
- **Pareto-floor potvrda:**
  - Jamči 100% prenosiv statički single-binary TUI na Windows, Linux i macOS platformama bez runtime ovisnosti.

---

## S14.2: Headless / API i Embeddable Go SDK

- **Tip:** `MEHANIZAM` (Go paket `pkg/engine` i `pkg/sdk`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Jedinstveni Izvršni Put — Zabrana Bypass Executora):* **HARNESS-SPEC v0.9 (S14.2 Invarijant)** — **NAŠ ARHITEKTONSKI INVARIJANT:** Bilo da se harness koristi kao CLI alat, web poslužitelj ili uvezeni Go paket u nekom drugom programu (`import "nexus/pkg/sdk"`), **svaki upit prolazi isti S0 Envelope, S6.9 Lifecycle Chokepoint i S7 Recovery Supervisor**.
  - *Aspekt B (Programabilni SDK API):* **LangGraph / Temporal Go SDK** — jednostavne metode: `RunSession()`, `StreamSession()`, `SubmitApproval()`, `GetState()`.
  - *Aspekt C (REST / SSE / gRPC Poslužitelj):* **LiteLLM / Dify API** — standardizirani HTTP endpointi (`POST /v1/chat/completions`, `GET /v1/events/stream`).
- **Sinteza (Go dizajn skica):**
  ```go
  package sdk

  import (
      "context"
      "nexus/pkg/contract"
      "nexus/pkg/engine"
  )

  type NexusClient struct {
      engine *engine.ApplicationEngine
  }

  func NewNexusClient(cfg engine.Config) (*NexusClient, error) {
      eng, err := engine.NewApplicationEngine(cfg)
      if err != nil {
          return nil, err
      }
      return &NexusClient{engine: eng}, nil
  }

  // ExecuteTurn provodi potpuni turn kroz cjelokupni S6.9 sigurnosni cjevovod
  func (c *NexusClient) ExecuteTurn(ctx context.Context, sessionID string, prompt string) (*contract.Envelope, error) {
      return c.engine.ExecuteTurn(ctx, sessionID, prompt)
  }

  // StreamTurn vraća kanal sa strukturiranim SSE događajima
  func (c *NexusClient) StreamTurn(ctx context.Context, sessionID string, prompt string) (<-chan contract.StreamDelta, error) {
      return c.engine.StreamTurn(ctx, sessionID, prompt)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `sdk.py` u Go `pkg/sdk`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1 & P0.2**; Gate test: `test_embeddable_sdk_enforces_lifecycle_and_pep_policies`.
- **Verifikacija aspekata:**
  - Temporal Go client (MIT), standardni Go interface contract.
- **Pareto-floor potvrda:**
  - Omogućuje ugradnju NEXUS-a u bilo koju Go aplikaciju kao biblioteku uz punu garanciju sigurnosti i pouzdanosti.

---

## S14.3: Web UI i Ugrađeni Dashboard (Single-Binary Serve)

- **Tip:** `ADAPTER` (Go paket `pkg/interfaces/web`, statički asseti u `embed.FS`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Ugrađeni SPA Dashboard bez Eksternog Node.js Servera):* **OpenHands / LibreChat + Go `embed`** — statički kompajlirani React/Vue/Svelte frontend ugrađuje se u Go binar (`//go:embed dist/*`).
  - *Aspekt B (Real-Time SSE Event Stream):* **Open WebUI** — Server-Sent Events za live prikaz razmišljanja modela, izvođenja alata i diffova.
  - *Aspekt C (Multi-User Dashboard i Revizija):* **Dify Dashboard** — pregled sesija, potrošnje tokena (S2.5) i upravljanje vještinama (S11.5).
- **Sinteza (Go dizajn skica):**
  ```go
  package web

  import (
      "embed"
      "io/fs"
      "net/http"
      "nexus/pkg/engine"
  )

  //go:embed dist/*
  var embeddedUI embed.FS

  type WebServer struct {
      engine *engine.ApplicationEngine
      port   int
  }

  func (ws *WebServer) Start(addr string) error {
      mux := http.NewServeMux()

      // 1. API Rute
      mux.HandleFunc("/api/v1/sessions", ws.handleSessions)
      mux.HandleFunc("/api/v1/stream", ws.handleStreamSSE)
      mux.HandleFunc("/api/v1/approval", ws.handleApproval)

      // 2. Statički SPA Frontend poslužitelj
      uiFS, _ := fs.Sub(embeddedUI, "dist")
      mux.Handle("/", http.FileServer(http.FS(uiFS)))

      return http.ListenAndServe(addr, mux)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `serve-dashboard` i API ruta u Go `pkg/interfaces/web`.
- **Ugovor / RED Gate:**
  - Povezano s P0.3; RED test: `test_embedded_web_dashboard_serves_spa_and_streams_sse`.
- **Verifikacija aspekata:**
  - Go standard `embed.FS`, Open WebUI API spec (MIT).
- **Pareto-floor potvrda:**
  - Korisnik dobiva web sučelje pokretanjem jedne naredbe (`nexus serve`) bez potrebe za instaliranjem Node.js-a ili npm paketa.

---

## S14.4: IDE Integracija i ACP (Agent Communication Protocol)

- **Tip:** `ADAPTER` (Go paket `pkg/interfaces/acp`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Agent Communication Protocol — ACP Standard):* **Zed / Cursor / VSCode ACP Spec** — JSON-RPC 2.0 protokol preko `stdio` koji omogućuje editoru da kontrolira agenta i prikazuje inline izmjene u editoru.
  - *Aspekt B (LSP Simbolička Povezanost):* **Language Server Protocol** — dvosmjerna razmjena dijagnostike i simbola (povezano sa S4.4 Code Navigation).
- **Sinteza (Go dizajn skica):**
  ```go
  package acp

  import (
      "bufio"
      "context"
      "encoding/json"
      "os"
      "nexus/pkg/engine"
  )

  type ACPStdioServer struct {
      engine *engine.ApplicationEngine
  }

  func (s *ACPStdioServer) RunStdio(ctx context.Context) error {
      scanner := bufio.NewScanner(os.Stdin)
      for scanner.Scan() {
          var req JSONRPCRequest
          if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
              continue
          }
          // Obradi ACP zahtjev iz Zed / VSCode editora i vrati JSON-RPC odgovor
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/acp.py` u Go `pkg/interfaces/acp`.
- **Ugovor / RED Gate:**
  - Povezano s P0.1; RED test: `test_acp_stdio_server_handles_initialize_and_prompt_requests`.
- **Verifikacija aspekata:**
  - ACP specification (Zed industries), JSON-RPC 2.0 standard.
- **Pareto-floor potvrda:**
  - Omogućuje besprijekorno korištenje NEXUS-a unutar modernih IDE okruženja.

---

## S14.5: Messaging i Channel Gateway (Telegram, Slack, Discord, Udaljeni HITL)

- **Tip:** `ADAPTER` (Adapteri kanala) + `MEHANIZAM` (Gateway Router), capability flag `channels`
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Annex A P1.6 Uvjet Zatvaranja — `channels requires extensions`):* **Annex A P1.6 (agy#3)** — **NAŠ ARHITEKTONSKI INVARIJANT:** Uključivanje `channels` flaga zahtijeva prisutnost `extensions` mogućnosti. Pokušaj pokretanja gatewaya bez odgovarajućeg tokena ili profila ruši se na provjeri zatvaranja (`CLOSURE_FAILED`).
  - *Aspekt B (Standardizirani Channels Ugovor):* **NEXUSv2 (`gateway/`) + OpenClaw** — svaki adapter kanala (Telegram, Slack, Discord, WhatsApp, Matrix, Webhook) implementira:
    1. `Identity / Allowlist:` Provjera pošiljatelja prema bijeloj listi (`allowed_user_ids`).
    2. `Threading:` Mapiranje chata/threada na izoliranu NEXUS sesiju (S9.1).
    3. `Receipt & Idempotency:` Potvrda primitka poruke radi sprječavanja dupliciranja.
    4. `Udaljeni HITL Odobrenje:` Slanje interaktivnih gumba (Approve / Reject) za Annex A P1.6 odobrenja na mobitel.
  - *Aspekt C (Pouzdana Isporuka kroz Transactional Outbox):* **Annex A N9 + S7.3** — poruke prema kanalima šalju se kroz trajni SQLite outbox, osiguravajući isporuku i nakon pada mreže.
- **Sinteza (Go dizajn skica):**
  ```go
  package gateway

  import (
      "context"
      "fmt"
      "nexus/pkg/contract"
      "nexus/pkg/engine"
      "nexus/pkg/reliability/outbox"
  )

  type ChannelAdapter interface {
      ChannelName() string
      SendMessage(ctx context.Context, recipientID string, text string, buttons []contract.ActionButton) error
      StartListener(ctx context.Context, incomingHandler func(msg InboundChannelMessage)) error
  }

  type InboundChannelMessage struct {
      Channel      string `json:"channel"` // "telegram" | "slack" | "discord"
      SenderID     string `json:"sender_id"`
      ThreadID     string `json:"thread_id"`
      Text         string `json:"text"`
      IsApproval   bool   `json:"is_approval"`
      ApprovalToken string `json:"approval_token,omitempty"`
  }

  type ChannelGatewayManager struct {
      adapters      map[string]ChannelAdapter
      allowlist     map[string]map[string]bool // channel -> senderID -> allowed
      engine        *engine.ApplicationEngine
      outboxManager *outbox.TransactionalOutboxManager
  }

  func (cgm *ChannelGatewayManager) HandleInbound(ctx context.Context, msg InboundChannelMessage) error {
      // 1. Provjera bijele liste pošiljatelja (Identity / Allowlist)
      if !cgm.allowlist[msg.Channel][msg.SenderID] {
          return fmt.Errorf("SECURITY_DENY: sender %s is not in %s allowlist", msg.SenderID, msg.Channel)
      }

      // 2. Ako je odgovor na HITL izazov (P1.6 Udaljeno Odobrenje)
      if msg.IsApproval {
          return cgm.engine.SubmitApprovalDecision(msg.ApprovalToken, contract.ApprovalDecision{
              Decision: msg.Text, // "APPROVE" | "REJECT"
          })
      }

      // 3. Pokreni turn kroz standardni engine
      sessionID := fmt.Sprintf("%s-%s", msg.Channel, msg.ThreadID)
      _, err := cgm.engine.ExecuteTurn(ctx, sessionID, msg.Text)
      return err
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `gateway/` (6 adaptera kanala: telegram, slack, discord, whatsapp, matrix, webhook) u Go `pkg/gateway`.
- **Ugovor / RED Gate:**
  - **Annex A P1.6 & N9**; RED testovi:
    1. `test_channels_cannot_enable_without_extensions` (provjera P1.6 closure zavisnosti).
    2. `test_gateway_allowlist_blocks_unauthorized_telegram_sender`.
- **Verifikacija aspekata:**
  - Telegram Bot API (Go standard), Slack Bolt API, Matrix SDK.
- **Pareto-floor potvrda:**
  - Omogućuje upravljanje agentom i odobravanje rizičnih akcija s bilo kojeg uređaja uz strogu bijelu listu i pouzdanu dostavu.

---

## Rezime S14 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **14.1 CLI & TUI** | ADAPTER | • **Bubbletea / Lipgloss:** Pure-Go single binary TUI<br>• **Wails / Fyne:** Desktop grafička aplikacija | **Annex A P0.3** (`test_tui_renders_streaming_deltas`) |
| **14.2 Embeddable SDK**| MEHANIZAM | • **Unified Engine API:** **Isti S0/6.9/7.x put** (nema zaobilaznih executora) | **Annex A P0.1 & P0.2** (`test_embeddable_sdk_enforces_policies`) |
| **14.3 Web UI** | ADAPTER | • **Go `embed.FS`:** SPA dashboard ugrađen u binar (`nexus serve`) | **Annex A P0.3** (`test_embedded_web_dashboard_serves_spa`) |
| **14.4 IDE / ACP** | ADAPTER | • **Zed / VSCode ACP Spec:** JSON-RPC 2.0 stdio server | **Annex A P0.1** (`test_acp_stdio_server_handles_requests`) |
| **14.5 Messaging Gateway**| ADAPTER + MEHANIZAM | • **Annex A P1.6:** **`channels requires extensions` closure**<br>• **Annex A N9 + S7.3:** **Transactional Outbox & Udaljeni HITL** | **Annex A P1.6 & N9** (`test_channels_cannot_enable_without_extensions`) |

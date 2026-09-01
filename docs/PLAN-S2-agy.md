PLAN S2

# Hibrid-Hibrida Sinteza: Sekcija S2 (Provider Sloj)

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S2.1–S2.6)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.2, P2.5)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `net/http`, `os/exec`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## Pregled sekcije S2

Sekcija S2 upravlja komunikacijom s velikim jezičnim modelima (LLM) — kako udaljenim API i CLI poslužiteljima, tako i lokalnim instancama. Arhitektura strogo poštuje pravilo iz Annexa A P0.2: **S7 je jedini vlasnik retryja, deadlinea i cancela**; S2 je isključivo normalizator grešaka, izvođač zadanih pokušaja (`AttemptGrant`) i predlagač fallback modela.

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S2 PROVIDER LAYER                                     │
│                                                                                         │
│  ┌───────────────────────┐   ┌───────────────────────┐   ┌───────────────────────────┐  │
│  │ 2.1 Unified Provider  │   │ 2.2 Fallback Normalizer│  │ 2.3 Structured Output     │  │
│  │ 3 Auth Moda + Opt-in, │──→│ Map Error → S7 Policy,│──→│ Schema Validation,        │  │
│  │ SSE Streaming, Tools  │   │ Cooldown, Rate-Limits │   │ Re-Ask, BAML Tolerance    │  │
│  └───────────────────────┘   └───────────────────────┘   └───────────────────────────┘  │
│             │                            │                            │                 │
│             ▼                            ▼                            ▼                 │
│  ┌───────────────────────┐   ┌───────────────────────┐   ┌───────────────────────────┐  │
│  │ 2.4 Local & HW Fit    │   │ 2.5 Token & Cost Ledger│  │ 2.6 Model Routing         │  │
│  │ Ollama/llama.cpp/vLLM,│   │ Real-Time Spend Log,  │   │ Barbell Rules (Core ABI), │  │
│  │ RAM/VRAM Preflight    │   │ Pricing Matrix        │   │ RouteLLM / ClawRouter     │  │
│  └───────────────────────┘   └───────────────────────┘   └───────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S2.1: Multi-Provider Apstrakcija i Pluggable Auth-Modovi

- **Tip:** `MEHANIZAM` (Go paket `pkg/provider`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Unificirani format poruka i streaminga):* **LiteLLM** — standardizirani streaming chunkovi, mapiranje parametara (temperature, max_tokens, stop sequences).
  - *Aspekt B (Čisti tipizirani adapteri i error normalizacija):* **PydanticAI** — model-agnostička adaptacija s deklariranim capabilities.
  - *Aspekt C (Pluggable Auth-Modovi i Sigurnosne Granice):* **NEXUSv2 + Korisnički Zahtjev** — podrška za 3 službena načina autorizacije + 1 strogo izolirani opt-in način:
    1. **API-Key HTTP (Default):** Standardni Bearer / Header token preko TLS-a (čisto, stabilno).
    2. **Službeni OAuth Flow:** Device-code ili Authorization Code flow (OAuth2 PKCE) za providere koji službeno nude OAuth za API pristup.
    3. **CLI-Agent Subprocess:** Subprocess na instalirani `claude` ili `codex` CLI s eksplicitno **ugašenim alatima** (`--tools ""`, ephemeral config) — NEXUS koristi CLI samo kao jeftin text-in/text-out model, a zadržava potpunu kontrolu nad petljom, alatima i memorijom.
    4. **Replicirani Web/Subscription OAuth (OPT-IN):** Zaseban eksperimentalni flag uz **EKSPLICITNO ToS-upozorenje** (korisnik mora potvrditi rizik od suspenzije računa). NIKAD nije default.
- **Sinteza (Go dizajn skica):**
  ```go
  package provider

  import (
      "context"
      "io"
      "time"

      "nexus/pkg/contract"
  )

  type AuthMode string
  const (
      AuthModeAPIKey     AuthMode = "API_KEY"
      AuthModeOAuth2     AuthMode = "OAUTH2_SANCTIONED"
      AuthModeCLIAgent   AuthMode = "CLI_AGENT_SUBPROCESS"
      AuthModeWebOAuth   AuthMode = "UNSANCTIONED_WEB_OAUTH_OPT_IN" // Requires explicit ToS consent
  )

  type Provider interface {
      ID() string
      AuthMode() AuthMode
      Complete(ctx context.Context, grant contract.AttemptGrant, req CompletionRequest) (*CompletionResponse, error)
      Stream(ctx context.Context, grant contract.AttemptGrant, req CompletionRequest) (<-chan StreamChunk, error)
  }

  type CompletionRequest struct {
      ModelID        string                  `json:"model_id"`
      Messages       []contract.Message      `json:"messages"`
      Tools          []contract.ToolSchema   `json:"tools,omitempty"`
      Temperature    float32                 `json:"temperature"`
      MaxTokens      int                     `json:"max_tokens"`
      ResponseFormat *ResponseFormat         `json:"response_format,omitempty"`
  }

  type CompletionResponse struct {
      ModelID      string                  `json:"model_id"`
      Message      contract.Message        `json:"message"`
      Usage        TokenUsage              `json:"usage"`
      FinishReason string                  `json:"finish_reason"` // "stop" | "tool_calls" | "length"
  }

  type StreamChunk struct {
      DeltaText    string                  `json:"delta_text"`
      DeltaToolCall *contract.ToolCall      `json:"delta_tool_call,omitempty"`
      Usage        *TokenUsage             `json:"usage,omitempty"`
      Error        *contract.TypedError    `json:"error,omitempty"`
  }

  type TokenUsage struct {
      InputTokens  int `json:"input_tokens"`
      OutputTokens int `json:"output_tokens"`
      CachedTokens int `json:"cached_tokens"`
  }

  // --- Implementacija Auth Modova ---

  // 1. API-Key HTTP Adapter (OpenAI / DeepSeek / Anthropic direct)
  type HTTPProvider struct {
      id       string
      endpoint string
      apiKey   string
  }

  // 2. Službeni OAuth2 PKCE Adapter
  type OAuth2Provider struct {
      id          string
      tokenSource TokenSource
  }

  // 3. CLI-Agent Subprocess Adapter (NEXUS drži petlju, CLI je samo model)
  type CLIAgentProvider struct {
      id          string
      binaryPath  string // e.g., "/usr/local/bin/claude"
      extraFlags  []string // e.g., ["--tools", "", "--no-session-persist"]
  }

  // 4. Opt-in Web/Subscription OAuth Adapter
  type WebOAuthOptInProvider struct {
      id               string
      tosConsentSigned bool // Must be true, otherwise fails at init
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port i unifikacija `llm/providers.py` (gdje je NEXUS već imao CLI-agent i OpenAI API adaptere) u čiste Go tipove s `net/http` i `os/exec`.
- **Ugovor / RED Gate:**
  - **Annex A P2.5** (Provider data-processing i rezidencijski preflight); RED test: `test_fallback_cannot_bypass_residency_policy` i `test_cli_agent_forces_disabled_tools`.
- **Verifikacija aspekata:**
  - LiteLLM (MIT), PydanticAI (MIT), Vercel AI SDK (Apache-2.0).
- **Pareto-floor potvrda:**
  - Pruža čisti unificirani Go interface koji podržava standardni API-key, službeni OAuth, izolirani CLI-subproces i eksplicitni opt-in način bez curenja autoriteta.

---

## S2.2: Fallback Lanci, Cooldown i Normalizacija Grešaka

- **Tip:** `MEHANIZAM` (Go paket `pkg/provider/fallback`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Normalizacija statusnih kodova u TypedError):* **Kong AI Gateway + Annex A P0.2** — prevođenje HTTP 429, 500, 503, connection reset i context length exceeded u tipizirane `contract.ErrorCategory` i `contract.Retryability`.
  - *Aspekt B (Pametni Cooldown i Rate-Limit Tracking):* **LiteLLM** — privremeno izbacivanje providera iz rotacije kod 429 grešaka na bazi `Retry-After` zaglavlja ili eksponencijalnog cooldowna.
  - *Aspekt C (S7 Ownership Invariant):* **Annex A P0.2** — S2.2 **NE SMIJE** imati vlastitu retry petlju! S2.2 samo mapira grešku i predlaže fallback target; S7 odlučuje hoće li izdati novi `AttemptGrant`.
- **Sinteza (Go dizajn skica):**
  ```go
  package fallback

  import (
      "net/http"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type CooldownTracker struct {
      mu       sync.RWMutex
      cooldown map[string]time.Time // providerID -> availableAt
  }

  func (ct *CooldownTracker) IsAvailable(providerID string) bool {
      ct.mu.RLock()
      defer ct.mu.RUnlock()
      until, exists := ct.cooldown[providerID]
      return !exists || time.Now().After(until)
  }

  func (ct *CooldownTracker) MarkRateLimited(providerID string, retryAfter time.Duration) {
      ct.mu.Lock()
      defer ct.mu.Unlock()
      if retryAfter <= 0 {
          retryAfter = 30 * time.Second
      }
      ct.cooldown[providerID] = time.Now().Add(retryAfter)
  }

  type ErrorNormalizer struct{}

  func (en *ErrorNormalizer) NormalizeHTTP(providerID string, statusCode int, body []byte) *contract.TypedError {
      switch statusCode {
      case http.StatusTooManyRequests:
          return &contract.TypedError{
              Code:         "PROVIDER_RATE_LIMIT",
              Category:     contract.ErrResource,
              Retryability: contract.RetryS7Policy,
              SafeMessage:  "Provider rate limit exceeded; fallback or cooldown required",
              Origin:       providerID,
          }
      case http.StatusUnauthorized, http.StatusForbidden:
          return &contract.TypedError{
              Code:         "PROVIDER_AUTH_FAILED",
              Category:     contract.ErrAuthN,
              Retryability: contract.RetryNever, // Never retry bad credentials
              SafeMessage:  "Authentication to provider failed",
              Origin:       providerID,
          }
      case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
          return &contract.TypedError{
              Code:         "PROVIDER_UNAVAILABLE",
              Category:     contract.ErrDependency,
              Retryability: contract.RetryS7Policy,
              SafeMessage:  "Provider endpoint temporarily unavailable",
              Origin:       providerID,
          }
      default:
          return &contract.TypedError{
              Code:         "PROVIDER_INTERNAL_ERROR",
              Category:     contract.ErrInternal,
              Retryability: contract.RetryNever,
              SafeMessage:  "Unhandled provider error",
              Origin:       providerID,
          }
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/circuit.py` i `llm/providers.py` fallback logike u Go, uz **uklanjanje interne retry petlje** (ispravak duga: S7 sada posjeduje pokušaje).
- **Ugovor / RED Gate:**
  - **Annex A P0.2**; Gate test: `test_adapter_cannot_self_retry`.
- **Verifikacija aspekata:**
  - Kong AI Gateway (Apache-2.0, Go/Lua), LiteLLM (MIT), Portkey (Apache-2.0).
- **Pareto-floor potvrda:**
  - Strogo odvaja klasifikaciju grešaka i praćenje cooldowna od izvršne politike retryja, eliminirajući skrivene beskonačne petlje unutar adaptera.

---

## S2.3: Tool-Call Parsing i Structured Output

- **Tip:** `MEHANIZAM` (Go paket `pkg/structured`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Strogo mapiranje na Go strukture i auto-reask):* **Instructor** — validacija izlaza prema JSON Schemi strukture; ako model vrati nevaljan JSON, formira se precizna korektivna poruka za re-ask.
  - *Aspekt B (Gramatika i Constrained Decoding za lokalne modele):* **Outlines** — generiranje GBNF/JSON gramatike za llama.cpp/vLLM kako bi se garantirala 100% sintaksna ispravnost.
  - *Aspekt C (Tolerancija šuma i Markdown ekstrakcija):* **BAML** — robusni parser koji tolerira uvodni tekst modela ("Here is your JSON:") i neispravno zatvorene markdown blokove.
- **Sinteza (Go dizajn skica):**
  ```go
  package structured

  import (
      "encoding/json"
      "fmt"
      "regexp"
      "strings"
  )

  var jsonBlockRegex = regexp.MustCompile(`(?s)` + "```(?:json)?\\s*(.*?)\\s*```")

  type SchemaParser struct{}

  // ExtractAndValidate izvlači JSON iz čistog teksta ili markdown blokova (BAML tolerancija)
  // i validira ga prema ciljanoj Go strukturi (Instructor pristup).
  func (sp *SchemaParser) ExtractAndValidate(rawText string, target any) error {
      cleanJSON := strings.TrimSpace(rawText)

      // Ako je omotano u markdown ```json ... ```
      matches := jsonBlockRegex.FindStringSubmatch(cleanJSON)
      if len(matches) > 1 {
          cleanJSON = strings.TrimSpace(matches[1])
      }

      if err := json.Unmarshal([]byte(cleanJSON), target); err != nil {
          return fmt.Errorf("STRUCTURED_PARSE_FAILED: invalid json payload: %w", err)
      }

      return nil
  }

  // GenerateReaskPrompt formira minimalni korektivni turn ako je validacija pala
  func (sp *SchemaParser) GenerateReaskPrompt(parseErr error, expectedSchemaJSON string) string {
      return fmt.Sprintf("Your previous response failed validation:\n%s\n\nPlease output ONLY valid JSON matching this schema:\n%s",
          parseErr.Error(), expectedSchemaJSON)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/schema_guard.py` i `llm/parser.py` u Go `pkg/structured`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1** (Tipizirani `ToolCall` i `ContextBlock`); Gate test: `test_structured_output_reask_cycle`.
- **Verifikacija aspekata:**
  - Instructor (MIT, Python), Outlines (Apache-2.0), BAML (Apache-2.0).
- **Pareto-floor potvrda:**
  - Pruža trostruku zaštitu za slabe modele: BAML ekstrakciju šuma, Instructor re-ask petlju i Outlines gramatičko navođenje za lokalne modele.

---

## S2.4: Lokalni Modeli i Hardware-Aware Fit

- **Tip:** `MEHANIZAM` (Go paket `pkg/local`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Standardizirano lokalno serviranje):* **Ollama / llama.cpp / vLLM** — standardizirani HTTP klijenti s podrškom za OpenAI-kompatibilni format, streaming i embeddings.
  - *Aspekt B (Hardware-Aware Fit Preflight):* **Obsidian / Odysseus / Colibri** — provjera RAM-a, VRAM-a, dostupnosti CUDA/Metal/ROCm akceleracije i veličine kvantizacije prije pokretanja modela (odbij pretežak model prije OOM rušenja OS-a).
  - *Aspekt C (Automatski fallback na lakšu kvantizaciju):* **llama.cpp** — preporuka Q4_K_M umjesto Q8/FP16 ako je slobodni VRAM nedostatan.
- **Sinteza (Go dizajn skica):**
  ```go
  package local

  import (
      "fmt"
      "runtime"
  )

  type HardwareCapabilities struct {
      TotalSystemRAMBytes uint64 `json:"total_ram_bytes"`
      FreeSystemRAMBytes  uint64 `json:"free_ram_bytes"`
      TotalVRAMBytes      uint64 `json:"total_vram_bytes"`
      FreeVRAMBytes       uint64 `json:"free_vram_bytes"`
      HasCUDA             bool   `json:"has_cuda"`
      HasMetal            bool   `json:"has_metal"`
  }

  type LocalModelSpec struct {
      Name             string `json:"name"`
      ParameterCountB  float64 `json:"parameter_count_b"` // e.g. 7.0, 14.0
      Quantization     string  `json:"quantization"`      // e.g. "Q4_K_M", "Q8_0", "FP16"
      RequiredVRAMBytes uint64 `json:"required_vram_bytes"`
      RequiredRAMBytes  uint64 `json:"required_ram_bytes"`
  }

  type HardwareChecker struct{}

  func (hc *HardwareChecker) PreflightCheck(spec LocalModelSpec) error {
      hw := hc.DetectHardware()

      if hw.HasCUDA || hw.HasMetal {
          if spec.RequiredVRAMBytes > hw.FreeVRAMBytes {
              return fmt.Errorf("HARDWARE_INSUFFICIENT_VRAM: model %s requires %d MB VRAM, available: %d MB (recommend lower quantization)",
                  spec.Name, spec.RequiredVRAMBytes/(1024*1024), hw.FreeVRAMBytes/(1024*1024))
          }
      } else {
          if spec.RequiredRAMBytes > hw.FreeSystemRAMBytes {
              return fmt.Errorf("HARDWARE_INSUFFICIENT_RAM: model %s requires %d MB RAM, available: %d MB",
                  spec.Name, spec.RequiredRAMBytes/(1024*1024), hw.FreeSystemRAMBytes/(1024*1024))
          }
      }
      return nil
  }

  func (hc *HardwareChecker) DetectHardware() HardwareCapabilities {
      // Čita sysinfo na Linuxu / macOS sysctl / Windows GlobalMemoryStatusEx
      return HardwareCapabilities{
          TotalSystemRAMBytes: 16 * 1024 * 1024 * 1024,
          FreeSystemRAMBytes:  8 * 1024 * 1024 * 1024,
          HasCUDA:             false,
          HasMetal:            runtime.GOOS == "darwin",
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `llm/local_llm.py` i detekcije memorije iz `core/system_info.py` u Go `pkg/local`.
- **Ugovor / RED Gate:**
  - Preflight gate za lokalne modele; RED test: `test_local_model_rejected_on_insufficient_vram`.
- **Verifikacija aspekata:**
  - Ollama (MIT, Go), llama.cpp (MIT, C++), vLLM (Apache-2.0, Python).
- **Pareto-floor potvrda:**
  - Sprječava OOM rušenje sustava determinističkom provjerom hardvera prije učitavanja modela.

---

## S2.5: Token Accounting i Cost Tracking

- **Tip:** `MEHANIZAM` (Go paket `pkg/accounting`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Ugrađena matrica cijena po modelu):* **LiteLLM** — tablica cijena po milijunu ulaznih, izlaznih i cached tokena.
  - *Aspekt B (Prikaz troška sesije u stvarnom vremenu):* **Aider** — izračun i prikaz akumuliranog troška ($) nakon svakog završenog turna.
  - *Aspekt C (Transparentno knjiženje bez promjene koda):* **Helicone + Annex A P0.3** — svaki poziv emitira tipizirani računovodstveni zapis u `EventJournal`.
- **Sinteza (Go dizajn skica):**
  ```go
  package accounting

  import (
      "sync"
  )

  type ModelPricing struct {
      InputCostPerM  float64 `json:"input_cost_per_m"`
      OutputCostPerM float64 `json:"output_cost_per_m"`
      CacheReadCostPerM float64 `json:"cache_read_cost_per_m"`
  }

  type CostLedger struct {
      mu           sync.RWMutex
      pricings     map[string]ModelPricing
      totalSpent   float64
      sessionSpent float64
  }

  func (cl *CostLedger) RecordUsage(modelID string, inputTokens, outputTokens, cachedTokens int) float64 {
      cl.mu.Lock()
      defer cl.mu.Unlock()

      p, ok := cl.pricings[modelID]
      if !ok {
          // Default konzervativna procjena ako model nije u tablici
          p = ModelPricing{InputCostPerM: 2.0, OutputCostPerM: 8.0}
      }

      cost := (float64(inputTokens) * p.InputCostPerM / 1_000_000.0) +
              (float64(outputTokens) * p.OutputCostPerM / 1_000_000.0) +
              (float64(cachedTokens) * p.CacheReadCostPerM / 1_000_000.0)

      cl.sessionSpent += cost
      cl.totalSpent += cost
      return cost
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/cost_tracker.py` i cjenika u Go structove.
- **Ugovor / RED Gate:**
  - **Annex A P2.2** (Resource Budgets: `cost_minor_units` enforcement); RED test: `test_cost_ledger_exceeds_budget_triggers_abort`.
- **Verifikacija aspekata:**
  - LiteLLM pricing json (MIT), Aider accounting (Apache-2.0).
- **Pareto-floor potvrda:**
  - Donosi precizno praćenje troškova u mikrodolarima u realnom vremenu uz automatsko zaustavljanje ako je probijen zadani budžet.

---

## S2.6: Model Routing (Pravi Model za Pravi Zadatak)

- **Tip:** `MEHANIZAM` (Go paket `pkg/routing`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Barbell / Task-Complexity usmjeravanje u jezgri):* **NEXUSv2 + HARNESS-SPEC v0.8** — bazični barbell router je u jezgri (S2.6): jaki model za planiranje/arhitekturu/sintezu, brzi/jeftini model za male izmjene/testove.
  - *Aspekt B (Naučeno i semantičko usmjeravanje):* **RouteLLM + semantic-router** — klasifikacija težine upita kroz embeddinge ili lagani klasifikator (ClawRouter 15-dim lokalno).
  - *Aspekt C (Jedinstveni Router ABI):* **Annex A P0.4** — napredni routeri su opcionalni moduli iza istog jezgrenog sučelja.
- **Sinteza (Go dizajn skica):**
  ```go
  package routing

  import (
      "context"
  )

  type TaskComplexity string
  const (
      ComplexityComplex TaskComplexity = "COMPLEX" // Planning, architecture, security audit -> Strong model
      ComplexitySimple  TaskComplexity = "SIMPLE"  // Typo fix, small diff, formatting -> Fast/Cheap model
  )

  type Router interface {
      Route(ctx context.Context, prompt string, taskCategory string) (selectedModelID string, reason string, err error)
  }

  // BarbellCoreRouter je deterministički jezgreni router
  type BarbellCoreRouter struct {
      strongModelID string // e.g. "claude-3-7-sonnet" or "deepseek-r1"
      fastModelID   string // e.g. "gemini-2-flash" or "qwen-2.5-coder-7b"
  }

  func (r *BarbellCoreRouter) Route(ctx context.Context, prompt string, taskCategory string) (string, string, error) {
      switch taskCategory {
      case "architecture", "security_audit", "complex_refactor", "council":
          return r.strongModelID, "task category requires deep reasoning (strong model)", nil
      case "lint_fix", "documentation", "single_file_patch", "test_run":
          return r.fastModelID, "simple task routed to fast model", nil
      default:
          // Heuristika duljine i ključnih riječi
          if len(prompt) > 4000 {
              return r.strongModelID, "large context prompt", nil
          }
          return r.fastModelID, "default lightweight route", nil
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `llm/router.py` (gdje je NEXUS imao barbell i token-budget router) u Go `pkg/routing`.
- **Ugovor / RED Gate:**
  - Povezano s **Annex A P0.4** i S0.3; RED test: `test_router_selects_strong_model_for_security_audit`.
- **Verifikacija aspekata:**
  - RouteLLM (Apache-2.0), semantic-router (Apache-2.0), LiteLLM router (MIT).
- **Pareto-floor potvrda:**
  - Bazični barbell je deterministički i nula-trošak unutar jezgre, dok je ABI spreman za priključivanje naučenih semantičkih routera bez promjene agentske petlje.

---

## Rezime S2 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **2.1 Multi-Provider & Auth** | MEHANIZAM | • **LiteLLM:** Unificirani API ugovor<br>• **PydanticAI:** Error normalizacija<br>• **NEXUS:** 3 službena auth moda + opt-in ToS web OAuth | **Annex A P2.5** (`test_fallback_cannot_bypass_residency_policy`, `test_cli_agent_forces_disabled_tools`) |
| **2.2 Fallback & Cooldown** | MEHANIZAM | • **Kong AI Gateway:** 429/500 normalizacija<br>• **LiteLLM:** Cooldown i rate-limit tracking<br>• **Annex A P0.2:** S7 jedini retry owner | **Annex A P0.2** (`test_adapter_cannot_self_retry`) |
| **2.3 Structured Output** | MEHANIZAM | • **Instructor:** Re-ask i Pydantic/Go struct validacija<br>• **Outlines:** GBNF gramatika za lokalne modele<br>• **BAML:** Tolerancija šuma i markdown parsing | **Annex A P0.1** (`test_structured_output_reask_cycle`) |
| **2.4 Local & HW Fit** | MEHANIZAM | • **Ollama / llama.cpp / vLLM:** Nativni HTTP API<br>• **Odysseus / Colibri:** RAM/VRAM preflight provjera | **Preflight Gate** (`test_local_model_rejected_on_insufficient_vram`) |
| **2.5 Cost Tracking** | MEHANIZAM | • **LiteLLM:** Matrica cijena<br>• **Aider:** Real-time prikaz po sesiji<br>• **Annex A P0.3 & P2.2:** Token ledger i budžeti | **Annex A P2.2** (`test_cost_ledger_exceeds_budget_triggers_abort`) |
| **2.6 Model Routing** | MEHANIZAM | • **NEXUSv2:** Barbell usmjeravanje u jezgri<br>• **RouteLLM / semantic-router:** Proširivi router ABI | **Annex A P0.4** (`test_router_selects_strong_model_for_security_audit`) |

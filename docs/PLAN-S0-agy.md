PLAN S0

# Hibrid-Hibrida Sinteza: Sekcija S0 (Ugovori, Stanje i Lifecycle)

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S0.1–S0.5)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1–P0.4)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## Pregled sekcije S0

Sekcija S0 definira temeljne invarijante cijelog harnessa prije izvršenja prve linije koda agentske petlje. Svih 5 podsekcija su **MEHANIZMI** (čisti Go kod unutar jezgre), a ne vanjski adapteri. 

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S0 KERNEL ENGINE                                      │
│                                                                                         │
│  ┌───────────────────────┐   ┌───────────────────────┐   ┌───────────────────────────┐  │
│  │ S0.1 Canonical Schemas│   │  S0.2 State Machine   │   │ S0.5 Closure Resolver     │  │
│  │ Envelope, Message,    │──→│ Run / Turn / Attempt  │──→│ 18 Feature Flags,         │  │
│  │ ContextBlock, Error   │   │ Transitions & Recovery│   │ Gates, Topological Order  │  │
│  └───────────────────────┘   └───────────────────────┘   └───────────────────────────┘  │
│             │                            │                            │                 │
│             ▼                            ▼                            ▼                 │
│  ┌───────────────────────┐   ┌───────────────────────┐                                  │
│  │ S0.3 Cap Negotiation  │   │ S0.4 Schema Migrator  │                                  │
│  │ Model Descriptors,    │   │ Immutable Event Log,  │                                  │
│  │ Residency Preflight   │   │ Monotonic Upcasting   │                                  │
│  └───────────────────────┘   └───────────────────────┘                                  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S0.1: Typed Message / Tool-Call / Tool-Result / Error Schema

- **Tip:** `MEHANIZAM` (Go paket `pkg/contract`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Tool Interoperability & Schema Contract):* **MCP specification + SDKs** — standardizirani JSON-RPC 2.0 formati argumenata, tipizirani opisi alata i resursa.
  - *Aspekt B (Kompaktna agentska semantika poruka):* **OpenAI Agents SDK** — čisti `Message`, `Role` i razdvojeni blokovi sadržaja.
  - *Aspekt C (Provjerenost podrijetla i nepromjenjivost):* **LangGraph** — immutable čvorovi s korelacijskim identifikatorima i roditeljskim vezama.
  - *Aspekt D (Tipizirana taksonomija grešaka):* **Annex A P0.1** — `TypedError` s kategorijama, retryabilnošću i uzročnim stablom.
- **Sinteza (Go dizajn skica):**
  ```go
  package contract

  import "time"

  type Role string
  const (
      RoleSystem    Role = "SYSTEM"
      RoleUser      Role = "USER"
      RoleAssistant Role = "ASSISTANT"
      RoleTool      Role = "TOOL"
  )

  type TrustClass string
  const (
      TrustSystem          TrustClass = "SYSTEM"
      TrustUser            TrustClass = "USER"
      TrustToolTrusted     TrustClass = "TOOL_TRUSTED"
      TrustUntrustedExt    TrustClass = "UNTRUSTED_EXTERNAL"
  )

  type Sensitivity string
  const (
      SensPublic       Sensitivity = "PUBLIC"
      SensInternal     Sensitivity = "INTERNAL"
      SensConfidential Sensitivity = "CONFIDENTIAL"
      SensSecret       Sensitivity = "SECRET"
  )

  type ContextBlock struct {
      BlockID      string       `json:"block_id" validate:"required,uuid4"`
      Kind         string       `json:"kind" validate:"required"` // "text" | "image" | "resource"
      Content      *string      `json:"content,omitempty"`
      ContentRef   *string      `json:"content_ref,omitempty"`
      ContentHash  string       `json:"content_hash" validate:"required"` // SHA-256
      SourceURI    string       `json:"source_uri"`
      Producer     string       `json:"producer"`
      TrustClass   TrustClass   `json:"trust_class" validate:"required"`
      Sensitivity  Sensitivity  `json:"sensitivity" validate:"required"`
      Lineage      []string     `json:"lineage"` // parent block IDs
      ObservedAt   time.Time    `json:"observed_at"`
      ExpiresAt    *time.Time   `json:"expires_at,omitempty"`
  }

  type EffectClass string
  const (
      EffectReadOnly     EffectClass = "READ_ONLY"
      EffectReversible   EffectClass = "REVERSIBLE"
      EffectIrreversible EffectClass = "IRREVERSIBLE"
  )

  type ToolCall struct {
      ToolCallID          string      `json:"tool_call_id" validate:"required"`
      ToolID              string      `json:"tool_id" validate:"required"`
      Arguments           []byte      `json:"arguments" validate:"required"` // Raw JSON bytes
      ArgumentsSchemaHash string      `json:"arguments_schema_hash" validate:"required"`
      EffectClass         EffectClass `json:"effect_class" validate:"required"`
      Deadline            time.Time   `json:"deadline"`
      AttemptNo           int         `json:"attempt_no" validate:"gte=1"`
      IdempotencyKey      *string     `json:"idempotency_key,omitempty"`
  }

  type ToolResultStatus string
  const (
      ToolStatusSucceeded ToolResultStatus = "SUCCEEDED"
      ToolStatusFailed    ToolResultStatus = "FAILED"
      ToolStatusCancelled ToolResultStatus = "CANCELLED"
      ToolStatusUnknown   ToolResultStatus = "UNKNOWN"
  )

  type ToolResult struct {
      ToolCallID   string           `json:"tool_call_id" validate:"required"`
      AttemptNo    int              `json:"attempt_no" validate:"gte=1"`
      Status       ToolResultStatus `json:"status" validate:"required"`
      OutputBlocks []ContextBlock   `json:"output_blocks"`
      Error        *TypedError      `json:"error,omitempty"`
      StartedAt    time.Time        `json:"started_at"`
      FinishedAt   time.Time        `json:"finished_at"`
  }

  type ErrorCategory string
  const (
      ErrValidation    ErrorCategory = "VALIDATION"
      ErrAuthN         ErrorCategory = "AUTHN"
      ErrAuthZ         ErrorCategory = "AUTHZ"
      ErrPolicy        ErrorCategory = "POLICY"
      ErrResource      ErrorCategory = "RESOURCE"
      ErrTimeout       ErrorCategory = "TIMEOUT"
      ErrCancelled     ErrorCategory = "CANCELLED"
      ErrDependency    ErrorCategory = "DEPENDENCY"
      ErrConflict      ErrorCategory = "CONFLICT"
      ErrInternal      ErrorCategory = "INTERNAL"
      ErrUnknownEffect ErrorCategory = "UNKNOWN_EFFECT"
  )

  type Retryability string
  const (
      RetryNever    Retryability = "NEVER"
      RetryS7Policy Retryability = "S7_POLICY"
      RetryAfter    Retryability = "AFTER"
  )

  type TypedError struct {
      Code         string        `json:"code" validate:"required"`
      Category     ErrorCategory `json:"category" validate:"required"`
      Retryability Retryability  `json:"retryability" validate:"required"`
      SafeMessage  string        `json:"safe_message" validate:"required"`
      RetryAfter   *time.Duration `json:"retry_after,omitempty"`
      Origin       string        `json:"origin"`
      CauseEventID *string       `json:"cause_event_id,omitempty"`
  }

  type Envelope struct {
      SchemaID       string         `json:"schema_id" validate:"required"`
      SchemaVersion  int            `json:"schema_version" validate:"gte=1"`
      EventID        string         `json:"event_id" validate:"required,uuid4"`
      EventType      string         `json:"event_type" validate:"required"`
      RunID          string         `json:"run_id" validate:"required,uuid4"`
      TurnID         *int           `json:"turn_id,omitempty"`
      ToolCallID     *string        `json:"tool_call_id,omitempty"`
      ParentEventID  *string        `json:"parent_event_id,omitempty"`
      Sequence       int64          `json:"sequence" validate:"gte=1"`
      EmittedAt      time.Time      `json:"emitted_at"`
      ActorType      string         `json:"actor_type"` // "user" | "model" | "system" | "tool"
      ActorID        string         `json:"actor_id"`
      PrincipalID    string         `json:"principal_id"`
      TenantID       *string        `json:"tenant_id,omitempty"`
      WorkspaceID    string         `json:"workspace_id"`
      AttemptNo      int            `json:"attempt_no" validate:"gte=1"`
      IdempotencyKey *string        `json:"idempotency_key,omitempty"`
      Payload        []byte         `json:"payload"`
      PayloadHash    string         `json:"payload_hash" validate:"required"`
  }
  ```
- **Salvage iz NEXUSv2:**
  - Tipizacija i refaktoriranje ad-hoc dictova iz `core/events.py` i `core/store.py` u Go structove sa strogim tagovima.
- **Ugovor / RED Gate:**
  - **Annex A P0.1**; Gate test: `test_s0_rejects_provenance_laundering`.
- **Verifikacija aspekata:**
  - MCP SDK v1.0.0 (MIT, aktivno), OpenAI Agents SDK (Apache-2.0, aktivno), LangGraph (MIT, aktivno).
- **Pareto-floor potvrda:**
  - Sinteza pokriva MCP interoperabilnost (A), Agents SDK jednostavnost (B), LangGraph lineage (C) i Annex A robusnu taksonomiju grešaka (D) u jednom Go paketu bez cgo-a.

---

## S0.2: Run / Turn / Tool State Machine s Legalnim Prijelazima

- **Tip:** `MEHANIZAM` (Go paket `pkg/state`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Eksplicitni graf tranzicija i terminalna stanja):* **LangGraph** — formalna matrica stanja koja zabranjuje ilegalne skokove.
  - *Aspekt B (Event-Sourced State Derivation):* **OpenHands** — stanje sustava se uvijek može deterministički rekonstruirati iz niza događaja.
  - *Aspekt C (Durable Reconciliation i Unknown Recovery):* **Temporal** — semantika stanja `UNKNOWN` koja zahtijeva eksplicitnu pomirbu (reconciliation).
- **Sinteza (Go dizajn skica):**
  ```go
  package state

  import (
      "errors"
      "fmt"
      "sync"
  )

  type RunState string
  const (
      RunCreated   RunState = "CREATED"
      RunAdmitted  RunState = "ADMITTED"
      RunRunning   RunState = "RUNNING"
      RunSucceeded RunState = "SUCCEEDED"
      RunFailed    RunState = "FAILED"
      RunCancelled RunState = "CANCELLED"
      RunUnknown   RunState = "UNKNOWN"
  )

  type TurnState string
  const (
      TurnCreated   TurnState = "CREATED"
      TurnRunning   TurnState = "RUNNING"
      TurnSucceeded TurnState = "SUCCEEDED"
      TurnFailed    TurnState = "FAILED"
      TurnCancelled TurnState = "CANCELLED"
  )

  type AttemptState string
  const (
      AttemptPlanned    AttemptState = "PLANNED"
      AttemptAuthorized AttemptState = "AUTHORIZED"
      AttemptRunning    AttemptState = "RUNNING"
      AttemptSucceeded  AttemptState = "SUCCEEDED"
      AttemptFailed     AttemptState = "FAILED"
      AttemptCancelled  AttemptState = "CANCELLED"
      AttemptUnknown    AttemptState = "UNKNOWN"
  )

  var validRunTransitions = map[RunState]map[RunState]bool{
      RunCreated:   {RunAdmitted: true, RunCancelled: true, RunFailed: true},
      RunAdmitted:  {RunRunning: true, RunCancelled: true, RunFailed: true},
      RunRunning:   {RunSucceeded: true, RunFailed: true, RunCancelled: true, RunUnknown: true},
      RunUnknown:   {RunSucceeded: true, RunFailed: true, RunCancelled: true}, // via Reconciliation event only
      RunSucceeded: {}, // Terminal
      RunFailed:    {}, // Terminal
      RunCancelled: {}, // Terminal
  }

  type StateMachine struct {
      mu           sync.RWMutex
      currentRun   RunState
      currentTurn  TurnState
      attempts     map[string]AttemptState
  }

  func NewStateMachine() *StateMachine {
      return &StateMachine{
          currentRun: RunCreated,
          attempts:   make(map[string]AttemptState),
      }
  }

  func (sm *StateMachine) TransitionRun(target RunState) error {
      sm.mu.Lock()
      defer sm.mu.Unlock()

      allowed, exists := validRunTransitions[sm.currentRun]
      if !exists || !allowed[target] {
          return fmt.Errorf("illegal run transition: %s -> %s", sm.currentRun, target)
      }
      sm.currentRun = target
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Zamjena ad-hoc `while` zastavica iz `core/loop_engine.py` i `core/state.py` formalnim Go FSM motorom.
- **Ugovor / RED Gate:**
  - **Annex A P0.1 & P0.2**; Gate test: `test_adapter_cannot_self_retry` i `test_state_machine_rejects_illegal_transition`.
- **Verifikacija aspekata:**
  - LangGraph (MIT), OpenHands (MIT), Temporal (MIT/Go SDK).
- **Pareto-floor potvrda:**
  - Uklanja nesigurnost dinamičkog Pythona i daje O(1) thread-safe matricu prijelaza s čistim terminalnim stanjima.

---

## S0.3: Capability Negotiation po Modelu / Provideru

- **Tip:** `MEHANIZAM` (Go paket `pkg/capability`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Statički registar modela i granica):* **LiteLLM** — kontekstni prozor, cijene, podrška za alate i modalitete.
  - *Aspekt B (Tipizirani runtime pregovori adaptera):* **PydanticAI** — normalizacija mogućnosti i model-agnostička adaptacija.
  - *Aspekt C (Residency i Data-Policy Preflight):* **Portkey + Annex A P2.5** — obvezna provjera jurisdikcije, nultog zadržavanja i DPA prije slanja mrežnog zahtjeva.
- **Sinteza (Go dizajn skica):**
  ```go
  package capability

  import "fmt"

  type ModelDescriptor struct {
      ModelID             string   `json:"model_id"`
      ProviderID          string   `json:"provider_id"`
      MaxContextTokens    int      `json:"max_context_tokens"`
      MaxOutputTokens     int      `json:"max_output_tokens"`
      SupportsTools       bool     `json:"supports_tools"`
      SupportsVision      bool     `json:"supports_vision"`
      SupportsPromptCache bool     `json:"supports_prompt_cache"`
      ProcessingRegions   []string `json:"processing_regions"` // e.g., ["EU", "US"]
      ZeroDataRetention   bool     `json:"zero_data_retention"`
      TrainingUse         bool     `json:"training_use"`
  }

  type RequestDataPolicy struct {
      PrincipalID           string   `json:"principal_id"`
      RequiredRegions       []string `json:"required_regions"` // e.g., ["EU"]
      ForbidTrainingUse     bool     `json:"forbid_training_use"`
      RequireZeroRetention  bool     `json:"require_zero_retention"`
      AllowedClassifications []string `json:"allowed_classifications"`
  }

  type CapabilityNegotiator struct {
      registry map[string]ModelDescriptor
  }

  func (cn *CapabilityNegotiator) ValidatePreflight(modelID string, policy RequestDataPolicy) error {
      desc, ok := cn.registry[modelID]
      if !ok {
          return fmt.Errorf("model %s not registered in capability matrix", modelID)
      }

      if policy.ForbidTrainingUse && desc.TrainingUse {
          return fmt.Errorf("policy violation: model %s allows training use", modelID)
      }

      if policy.RequireZeroRetention && !desc.ZeroDataRetention {
          return fmt.Errorf("policy violation: model %s does not guarantee zero data retention", modelID)
      }

      // Jurisdikcijski presjek
      if len(policy.RequiredRegions) > 0 {
          matched := false
          for _, req := range policy.RequiredRegions {
              for _, prov := range desc.ProcessingRegions {
                  if req == prov {
                      matched = true
                      break
                  }
              }
          }
          if !matched {
              return fmt.Errorf("data residency violation: model regions %v do not satisfy %v", desc.ProcessingRegions, policy.RequiredRegions)
          }
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port i tipizacija `core/capreg.py` i `llm/providers.py` tablica u Go registar.
- **Ugovor / RED Gate:**
  - **Annex A P2.5**; Gate test: `test_fallback_cannot_bypass_residency_policy`.
- **Verifikacija aspekata:**
  - LiteLLM (MIT), PydanticAI (MIT), Portkey (Apache-2.0).
- **Pareto-floor potvrda:**
  - Kombinira statičke podatke o modelima s nezaobilaznim sigurnosnim preflightom podataka prije otvaranja socketa.

---

## S0.4: Verzioniranje i Migracija Event / Sesijskog Formata

- **Tip:** `MEHANIZAM` (Go paket `pkg/schema`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Standardizirani omot događaja):* **CloudEvents** — globalno usvojena specifikacija polja za događaje.
  - *Aspekt B (Nepromjenjiva pohrana sirovih događaja):* **OpenHands** — čuvanje izvornog zapisa bez destruktivnih izmjena.
  - *Aspekt C (Monotoni Upcasting lanac):* **Temporal** — postupno podizanje verzija (`v1 -> v2 -> v3`) uz karantenu nepoznatih diskriminatora.
- **Sinteza (Go dizajn skica):**
  ```go
  package schema

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
  )

  type UpcasterFunc func(rawPayload []byte) (newPayload []byte, err error)

  type MigrationStep struct {
      FromVersion int
      ToVersion   int
      Upcast      UpcasterFunc
  }

  type SchemaRegistry struct {
      migrations map[string]map[int]MigrationStep // schemaID -> fromVersion -> Step
  }

  func (sr *SchemaRegistry) UpcastToLatest(schemaID string, currentVersion int, targetVersion int, raw []byte) ([]byte, error) {
      if currentVersion == targetVersion {
          return raw, nil
      }
      if currentVersion > targetVersion {
          return nil, fmt.Errorf("lossy downcast forbidden: v%d -> v%d", currentVersion, targetVersion)
      }

      current := raw
      for v := currentVersion; v < targetVersion; v++ {
          step, ok := sr.migrations[schemaID][v]
          if !ok || step.ToVersion != v+1 {
              return nil, fmt.Errorf("broken migration chain for %s: missing step from v%d", schemaID, v)
          }
          next, err := step.Upcast(current)
          if err != nil {
              return nil, fmt.Errorf("migration error in %s v%d->v%d: %w", schemaID, v, v+1, err)
          }
          current = next
      }
      return current, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/events.py` formata u Go i povezivanje s SQLite WAL append-only tablicama.
- **Ugovor / RED Gate:**
  - **Annex A P0.1 (Migracija)**; Gate test: `test_s0_rejects_provenance_laundering`.
- **Verifikacija aspekata:**
  - CloudEvents v1.0.2 (CNCF standard), OpenHands (MIT), Temporal (MIT).
- **Pareto-floor potvrda:**
  - Čuva nepromjenjivi raw event u bazi, a na read-pathu omogućuje siguran, deterministički upcast na najnoviju verziju strukture.

---

## S0.5: Capability-Flag Closure Resolver

- **Tip:** `MEHANIZAM` (Go paket `pkg/featureflags`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Topološko razrješenje ovisnosti i detekcija ciklusa):* **Cargo / Bazel** — deterministički DAG solver za feature flagove.
  - *Aspekt B (Međusobno isključivanje i konfliktne politike):* **OSGi Module Layer** — stroga provjera konflikata prije aktivacije.
  - *Aspekt C (Atestacija sigurnosnih gateova):* **Annex A P0.4** — obvezna potvrda implementacije svih pripadajućih gateova prije učitavanja adaptera.
- **Sinteza (Go dizajn skica):**
  ```go
  package featureflags

  import "fmt"

  type CapabilityManifest struct {
      FlagID       string   `json:"flag_id"`
      Version      string   `json:"version"`
      Provides     []string `json:"provides"`
      Requires     []string `json:"requires"`
      Conflicts    []string `json:"conflicts"`
      Gates        []string `json:"gates"`
      ConfigHash   string   `json:"config_hash"`
  }

  type GateAttestation struct {
      GateID     string `json:"gate_id"`
      ConfigHash string `json:"config_hash"`
      Status     string `json:"status"` // "VERIFIED"
  }

  type ClosureResolver struct {
      manifests map[string]CapabilityManifest
  }

  func (cr *ClosureResolver) ResolveActiveClosure(requestedFlags []string, attestations map[string]GateAttestation) ([]string, error) {
      activeSet := make(map[string]bool)
      for _, f := range requestedFlags {
          activeSet[f] = true
      }

      // 1. Provjera ovisnosti (Requires)
      for flag := range activeSet {
          m, ok := cr.manifests[flag]
          if !ok {
              return nil, fmt.Errorf("unknown capability flag: %s", flag)
          }
          for _, req := range m.Requires {
              if !activeSet[req] {
                  return nil, fmt.Errorf("INCOMPLETE_CAPABILITY_CLOSURE: flag %s requires %s", flag, req)
              }
          }
      }

      // 2. Provjera konflikata (Conflicts)
      for flag := range activeSet {
          m := cr.manifests[flag]
          for _, conf := range m.Conflicts {
              if activeSet[conf] {
                  return nil, fmt.Errorf("CAPABILITY_CONFLICT: flag %s conflicts with %s", flag, conf)
              }
          }
      }

      // 3. Provjera sigurnosnih gateova (Gates)
      for flag := range activeSet {
          m := cr.manifests[flag]
          for _, gate := range m.Gates {
              att, exists := attestations[gate]
              if !exists || att.Status != "VERIFIED" || att.ConfigHash != m.ConfigHash {
                  return nil, fmt.Errorf("INCOMPLETE_CAPABILITY_CLOSURE: missing verified gate %s for flag %s", gate, flag)
              }
          }
      }

      var resolved []string
      for f := range activeSet {
          resolved = append(resolved, f)
      }
      return resolved, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Potpuno novi Go modul (greenfield), realizira ugovor iz Annexa A P0.4.
- **Ugovor / RED Gate:**
  - **Annex A P0.4**; Gate test: `test_every_flag_fails_when_one_gate_is_ablated`.
- **Verifikacija aspekata:**
  - Cargo feature resolution spec (Rust OSS), OSGi Core Release 8 (Eclipse Foundation).
- **Pareto-floor potvrda:**
  - Pruža fail-closed garanciju da se niti jedan adapter ne može inicijalizirati ako mu stablo ovisnosti ili sigurnosni gateovi nisu 100% zadovoljeni.

---

## Rezime S0 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **0.1 Typed Schemas** | MEHANIZAM | MCP SDK + OpenAI Agents SDK + LangGraph + Annex A | **Annex A P0.1** (`test_s0_rejects_provenance_laundering`) |
| **0.2 State Machine** | MEHANIZAM | LangGraph + OpenHands + Temporal | **Annex A P0.1 & P0.2** (`test_adapter_cannot_self_retry`) |
| **0.3 Cap Negotiation**| MEHANIZAM | LiteLLM + PydanticAI + Portkey (P2.5) | **Annex A P2.5** (`test_fallback_cannot_bypass_residency_policy`) |
| **0.4 Versioning** | MEHANIZAM | CloudEvents + OpenHands + Temporal | **Annex A P0.1** (`test_s0_rejects_lossy_downcast`) |
| **0.5 Closure Resolver**| MEHANIZAM | Cargo/Bazel DAG solver + OSGi Module Spec | **Annex A P0.4** (`test_every_flag_fails_when_one_gate_is_ablated`) |

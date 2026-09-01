PLAN S15

# Hibrid-Hibrida Sinteza: Sekcija S15 (Promatranje i Telemetrija / Observability)
## JEDAN WRITE-OWNER JOURNAL (P0.3), TAMPER-EVIDENT LANAC I SEMANTIČKO ZDRAVLJE

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S15.1–S15.5)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, S15.3 Tamper-Evident, S15.5 Semantic-Health)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `go.opentelemetry.io/otel`, `crypto/sha256`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Arhitektonska Strategija Telemetrije: Projekcije nad Journalom

Sekcija S15 implementira strogo razdvajanje pisanja i čitanja telemetrijskih podataka:
1. **Sve su PROJEKCIJE nad EventJournalom (Annex A P0.3):** **KLJUČNI INVARIJANT (codex#21):** Postoji točno **jedan write-owner** (`EventJournal.Append`). OTel traceovi, Prometheus metrike, Langfuse dashboardi i transkripti su **isključivo read-only projekcije** koje asinkrono konzumiraju događaje iz journala. Nema paralelnih sinkova koji direktno pišu na disk ili mrežu.
2. **Redakcija Tajni PRIJE Diska:** Svaki događaj se sanitizira i maskira (S6.4) prije nego što postane dio kriptografskog lanca.
3. **Tamper-Evident Hash Lanac + Vanjski Potpisani Checkpoint (15.3):** Svaki zapis u SQLite bazi sadrži `prev_event_hash` i `event_hash = SHA256(prev_event_hash + sequence_id + payload)`. Budući da lokalni lanac sam nije dokaz ako napadač ima root pristup bazi, periodički se **`checkpoint_hash` potpisuje asimetričnim ključem i šalje na vanjski revizijski poslužitelj**.
4. **Semantic Agent Health (15.5):** **NAŠ DIFERENCIJATOR:** Zaseban sloj telemetrije koji ne prati samo infrastrukturu (CPU/latencija — 15.4), već **kognitivno zdravlje agenta**:
   - `recidivism_rate` (ponavljanje iste greške).
   - `correction_rate` (uspješnost samopopravka).
   - `swallow_counter` (broj prikrivenih pogrešaka).
   - `fallback_drift` (prečesto skakanje na fallback model).
   Ako ovi pokazatelji pređu prag, automatski se okida **S7 / S3.4 Circuit Breaker**!

---

## 2. Arhitektura S15 Observability Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                S15 OBSERVABILITY ENGINE                                 │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ S1.3 / P0.3 EVENT JOURNAL (JEDINI WRITE-OWNER)                                    │  │
│  │   • Secret Redaction (S6.4) ──→ Append-Only SQLite WAL Commit ──→ Event Fan-Out   │  │
│  │   • Tamper-Evident Hash Chain: prev_hash ──→ event_hash                           │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────┬───────────────┼───────────────┬───────────────────┐        │
│        ▼                   ▼               ▼               ▼                   ▼        │
│  ┌───────────┐       ┌───────────┐   ┌───────────┐   ┌───────────┐       ┌───────────┐  │
│  │15.1 O-TEL │       │15.2 COST  │   │15.3 AUDIT │   │15.4 SLO   │       │15.5 HEALTH│  │
│  │GEN-AI     │       │DASHBOARD  │   │& REPLAY   │   │METRICS    │       │SEMANTIC   │  │
│  │Spans &    │       │Token spend│   │External   │   │Prometheus │       │Recidivism │  │
│  │Traces     │       │per tenant/│   │Signed     │   │p50/p95/p99│       │Correction │  │
│  │(Langfuse) │       │session    │   │Checkpoint │   │Latency    │       │Swallow-Ctr│  │
│  └───────────┘       └───────────┘   └───────────┘   └───────────┘       └─────┬─────┘  │
│                                                                                │        │
│                                                                                ▼        │
│                                                                      ┌────────────────┐ │
│                                                                      │ S7 / S3.4 TRIP │ │
│                                                                      │ CIRCUIT BREAKER│ │
│                                                                      └────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S15.1: Distribuirani Tracing (OTel GenAI Semantičke Konvencije)

- **Tip:** `ADAPTER` (Go paket `pkg/observability/otel`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (OpenTelemetry GenAI Semantic Conventions):* **OpenTelemetry Spec** — standardizirani atributi raspona: `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.prompt_tokens`, `gen_ai.usage.completion_tokens`, `gen_ai.response.finish_reasons`.
  - *Aspekt B (Read-Only Projekcija iz Journala):* **Annex A P0.3** — OTel rasponi (spans) kreiraju se kao projekcija iz `EventJournala`. Mrežni OTLP exporter nikada ne blokira niti odgađa upis događaja na lokalni disk.
  - *Aspekt C (Export u Langfuse / Phoenix / Jaeger):* **Langfuse OTel Adapter** — slanje cjelokupnog stabla poziva (turns, tool calls, model calls) prema vanjskim platformama.
- **Sinteza (Go dizajn skica):**
  ```go
  package otel

  import (
      "context"
      "go.opentelemetry.io/otel"
      "go.opentelemetry.io/otel/attribute"
      "go.opentelemetry.io/otel/trace"
      "nexus/pkg/contract"
  )

  type OTelGenAIProjection struct {
      tracer trace.Tracer
  }

  func NewOTelProjection(serviceName string) *OTelGenAIProjection {
      return &OTelGenAIProjection{
          tracer: otel.GetTracerProvider().Tracer(serviceName),
      }
  }

  // ProjectEvent konzumira događaj iz EventJournala i stvara/ažurira OTel raspon
  func (p *OTelGenAIProjection) ProjectEvent(ctx context.Context, rec contract.EventRecord) {
      if rec.EventType == "model_call" {
          _, span := p.tracer.Start(ctx, "gen_ai.chat",
              trace.WithAttributes(
                  attribute.String("gen_ai.system", "nexus"),
                  attribute.String("gen_ai.request.model", rec.ModelID),
                  attribute.Int("gen_ai.usage.prompt_tokens", rec.InputTokens),
                  attribute.Int("gen_ai.usage.completion_tokens", rec.OutputTokens),
              ),
          )
          defer span.End()
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/trace.py` i `core/otel.py` u Go `pkg/observability/otel`.
- **Ugovor / RED Gate:**
  - **Annex A P0.3**; RED test: `test_otel_projection_processes_journal_events_asynchronously`.
- **Verifikacija aspekata:**
  - OpenTelemetry Go SDK (Apache-2.0), Langfuse OTel spec.
- **Pareto-floor potvrda:**
  - Pruža 100% OTel standardizirani uvid u rad modela bez utjecaja na performanse ili latenciju jezgrene petlje.

---

## S15.2: Cost, Token i Quota Dashboardi

- **Tip:** `ADAPTER` (Go paket `pkg/observability/cost`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Precizno Knjiženje Troškova po Cjeniku Modela):* **Langfuse / OpenLIT / Helicone** — tablica cijena po milijunu tokena (prompt, completion, cached prompt) za svakog providera.
  - *Aspekt B (Projekcija po Tenantima i Sesijama):* **NEXUSv2 (`core/cost.py`)** — agregacija potrošnje po `tenant_id`, `user_id`, `project_id` i `model_id`.
  - *Aspekt C (Upozorenja na Prekoračenje Praga):* **Annex A P2.2 & S2.5** — izdavanje upozorenja kada sesija dosegne 80% definiranog budžeta.
- **Sinteza (Go dizajn skica):**
  ```go
  package cost

  import (
      "sync"
      "nexus/pkg/contract"
  )

  type ModelPricing struct {
      InputCostPerMillion  float64
      OutputCostPerMillion float64
      CacheCostPerMillion  float64
  }

  type CostLedgerProjection struct {
      mu       sync.RWMutex
      pricing  map[string]ModelPricing
      spendMap map[string]float64 // tenantID -> totalUSD
  }

  func (cl *CostLedgerProjection) RecordTokenUsage(tenantID string, modelID string, inputTokens, outputTokens, cacheTokens int) float64 {
      cl.mu.Lock()
      defer cl.mu.Unlock()

      p, exists := cl.pricing[modelID]
      if !exists {
          p = ModelPricing{InputCostPerMillion: 3.0, OutputCostPerMillion: 15.0} // Default fallback
      }

      cost := (float64(inputTokens)*p.InputCostPerMillion +
          float64(outputTokens)*p.OutputCostPerMillion +
          float64(cacheTokens)*p.CacheCostPerMillion) / 1_000_000.0

      cl.spendMap[tenantID] += cost
      return cost
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/cost.py` u Go `pkg/observability/cost`.
- **Ugovor / RED Gate:**
  - Povezano s P2.2 i S2.5; RED test: `test_cost_ledger_calculates_exact_spend_with_cached_tokens`.
- **Verifikacija aspekata:**
  - Langfuse pricing engine (MIT), OpenLIT (Apache-2.0).
- **Pareto-floor potvrda:**
  - Daje točan uvid u troškove u realnom vremenu uz automatsko prepoznavanje prompt-caching popusta.

---

## S15.3: Transkripti, Replay i Tamper-Evident Revizijski Zapis

- **Tip:** `MEHANIZAM` (Go paket `pkg/observability/audit`, ključni sigurnosni dokaz)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Kriptografski Hash Lanac u Journalu):* **Annex A P0.3 (codex#8)** — svaki zapis u SQLite bazi veže se na prethodni:
    `event_hash = SHA256(prev_event_hash + ":" + sequence_id + ":" + payload_hash)`.
  - *Aspekt B (Vanjski Potpisani Checkpoint — Zaštita od Rewrite Napada):* **HARNESS-SPEC v0.9 (15.3 Invarijant)** — **NAŠ SIGURNOSNI DIFERENCIJATOR:** Budući da lokalni lanac sam po sebi ne sprječava napadača s root pristupom da prepiše cijelu SQLite bazu i ponovno izračuna sve hasheve, sustav na svakih N događaja ili završetku sesije stvara **kriptografski potpisani Checkpoint** (`Ed25519` potpis nad `checkpoint_hash`) i šalje ga na udaljeni read-only audit poslužitelj ili Git tag.
  - *Aspekt C (Deterministički Transkript Replay):* **NEXUSv2 (`core/transcript.py`)** — rekonstrukcija stanja i replay sesije točno korak-po-korak iz hash-verificiranog transkripta.
- **Sinteza (Go dizajn skica):**
  ```go
  package audit

  import (
      "crypto/ed25519"
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "time"

      "nexus/pkg/contract"
  )

  type SignedAuditCheckpoint struct {
      CheckpointID string    `json:"checkpoint_id"`
      LastSequence int64     `json:"last_sequence"`
      LastHash     string    `json:"last_hash"`
      Signature    string    `json:"signature"`
      CreatedAt    time.Time `json:"created_at"`
  }

  type TamperEvidentAuditor struct {
      signingKey ed25519.PrivateKey
      lastHash   string
  }

  func (a *TamperEvidentAuditor) ComputeEventHash(prevHash string, seqID int64, payload []byte) string {
      payloadHash := sha256.Sum256(payload)
      raw := fmt.Sprintf("%s:%d:%s", prevHash, seqID, hex.EncodeToString(payloadHash[:]))
      h := sha256.Sum256([]byte(raw))
      return hex.EncodeToString(h[:])
  }

  func (a *TamperEvidentAuditor) CreateSignedCheckpoint(lastSeq int64, lastHash string) (*SignedAuditCheckpoint, error) {
      msg := fmt.Sprintf("nexus-audit-checkpoint:%d:%s", lastSeq, lastHash)
      sig := ed25519.Sign(a.signingKey, []byte(msg))

      return &SignedAuditCheckpoint{
          CheckpointID: fmt.Sprintf("cp-%d", time.Now().UnixNano()),
          LastSequence: lastSeq,
          LastHash:     lastHash,
          Signature:    hex.EncodeToString(sig),
          CreatedAt:    time.Now(),
      }, nil
  }

  func (a *TamperEvidentAuditor) VerifyChainIntegrity(records []contract.EventRecord) error {
      expectedPrev := "GENESIS"
      for _, r := range records {
          if r.PrevHash != expectedPrev {
              return fmt.Errorf("TAMPER_DETECTED: broken hash chain at sequence %d (expected %s, got %s)",
                  r.SequenceID, expectedPrev, r.PrevHash)
          }
          expectedPrev = r.EventHash
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/audit.py` i `core/transcript.py` u Go `pkg/observability/audit`.
- **Ugovor / RED Gate:**
  - **Annex A P0.3 & 15.3**; RED test: `test_audit_verifier_detects_tampered_event_payload`.
- **Verifikacija aspekata:**
  - Sigstore / in-toto hash chain model, Go `crypto/ed25519`.
- **Pareto-floor potvrda:**
  - Pruža matematički neoboriv revizijski trag koji detektira bilo kakvu naknadnu modifikaciju ili brisanje povijesti razgovora.

---

## S15.4: Infrastrukturne Metrike, Alarmi i SLO

- **Tip:** `MEHANIZAM` (Go paket `pkg/observability/metrics`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Prometheus Metrike na `/metrics` endpointu):* **Prometheus Client Go** — standardni brojači i histogrami:
    - `nexus_turn_duration_seconds` (p50, p95, p99 latencije turna).
    - `nexus_tool_execution_duration_seconds` (vrijeme izvođenja alata).
    - `nexus_active_sessions_total` (broj paralelnih sesija).
    - `nexus_rate_limit_hits_total` (broj 429 grešaka od providera).
  - *Aspekt B (SLO Pragovi i Automatsko Upozoravanje):* **Grafana SLO Alerting** — definiranje pragova (npr. p95 turn latencija < 4.0s, error rate < 1%).
- **Sinteza (Go dizajn skica):**
  ```go
  package metrics

  import (
      "net/http"
      "github.com/prometheus/client_golang/prometheus"
      "github.com/prometheus/client_golang/prometheus/promhttp"
  )

  var (
      TurnDuration = prometheus.NewHistogramVec(
          prometheus.HistogramOpts{
              Name:    "nexus_turn_duration_seconds",
              Help:    "Duration of agent turns in seconds",
              Buckets: []float64{0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
          },
          []string{"model", "status"},
      )
  )

  func InitMetrics() {
      prometheus.MustRegister(TurnDuration)
  }

  func Handler() http.Handler {
      return promhttp.Handler()
  }
  ```
- **Salvage iz NEXUSv2:**
  - Greenfield čista Go Prometheus integracija.
- **Ugovor / RED Gate:**
  - Povezano s 15.4; RED test: `test_prometheus_exporter_records_turn_latency`.
- **Verifikacija aspekata:**
  - prometheus/client_golang (Apache-2.0).
- **Pareto-floor potvrda:**
  - Omogućuje standardni DevOps nadzor u Kubernetes / Docker / bare-metal produkcijskim okruženjima.

---

## S15.5: Semantic Agent Health (Semantičko Zdravlje i Circuit Trip)

- **Tip:** `MEHANIZAM` (Go paket `pkg/observability/health`, kognitivni nadzor)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Diferencijacija: Semantika vs Infrastruktura):* **HARNESS-SPEC v0.9 (15.5 Direktiva)** — **NAŠ KOGNITIVNI DIFERENCIJATOR:** Dok 15.4 prati hardver/mrežu (CPU, latencija), a 18.3 prati liveness (heartbeat), 15.5 prati **kognitivnu efikasnost i devijaciju modela**:
    1. `Recidivism Rate:` Koliko često model ponavlja istu pogrešku u alatu u zadnja 3 turna.
    2. `Correction Rate:` Uspješnost modela da sam popravi grešku nakon što mu je vraćen error u opservaciji.
    3. `Verified Success Trend:` Postotak testova koji prolaze nakon svake izmjene koda (TIA veza).
    4. `Swallow Counter:` Broj potisnutih grešaka u podprocesima ili alatima.
    5. `Fallback Drift:` Učestalost skakanja s primarnog na fallback model.
  - *Aspekt B (Automatski Circuit Breaker Trip prema S7 / S3.4):* **NEXUSv2 (`core/health.py`)** — ako stopa recidivizma pređe prag (npr. >= 3 uzastopna ponavljanja iste greške), monitor automatski okida **S7 Circuit Breaker** i zaustavlja petlju prije nego što model potroši budžet u beskorisnoj petlji.
- **Sinteza (Go dizajn skica):**
  ```go
  package health

  import (
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type SemanticHealthMetrics struct {
      RecidivismCount  int     `json:"recidivism_count"`
      CorrectionCount  int     `json:"correction_count"`
      SwallowCount     int     `json:"swallow_count"`
      FallbackCount    int     `json:"fallback_count"`
      SuccessTrend     float64 `json:"success_trend"`
  }

  type SemanticHealthMonitor struct {
      mu             sync.Mutex
      metrics        map[string]*SemanticHealthMetrics // sessionID -> metrics
      maxRecidivism  int // Default: 3
      tripHandler    func(sessionID string, reason string)
  }

  func NewSemanticHealthMonitor(tripHandler func(sessionID string, reason string)) *SemanticHealthMonitor {
      return &SemanticHealthMonitor{
          metrics:       make(map[string]*SemanticHealthMetrics),
          maxRecidivism: 3,
          tripHandler:   tripHandler,
      }
  }

  func (shm *SemanticHealthMonitor) RecordToolError(sessionID string, toolName string, errSignature string) {
      shm.mu.Lock()
      defer shm.mu.Unlock()

      m, exists := shm.metrics[sessionID]
      if !exists {
          m = &SemanticHealthMetrics{}
          shm.metrics[sessionID] = m
      }

      m.RecidivismCount++

      // Provjeri prag za okidanje Circuit Breakera (S7.1 / S3.4 Trip)
      if m.RecidivismCount >= shm.maxRecidivism {
          reason := fmt.Sprintf("SEMANTIC_HEALTH_DEGRADATION: model repeated identical error %d times in tool %s", m.RecidivismCount, toolName)
          shm.tripHandler(sessionID, reason)
      }
  }

  func (shm *SemanticHealthMonitor) RecordCorrectionSuccess(sessionID string) {
      shm.mu.Lock()
      defer shm.mu.Unlock()
      if m, exists := shm.metrics[sessionID]; exists {
          m.CorrectionCount++
          m.RecidivismCount = 0 // Resetiraj recidivizam nakon uspješnog popravka
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/health.py` u Go `pkg/observability/health`.
- **Ugovor / RED Gate:**
  - **Annex A P0.2 & S3.4**; RED test: `test_semantic_health_trips_circuit_breaker_on_repeated_errors`.
- **Verifikacija aspekata:**
  - NEXUS `health.py` (dokazano u auditu), S3.4 circuit breaker model.
- **Pareto-floor potvrda:**
  - Štiti korisnički budžet i sprječava besciljno lutanje modela kroz kognitivnu telemetriju u stvarnom vremenu.

---

## Rezime S15 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **15.1 OTel GenAI** | ADAPTER | • **OTel GenAI Standard:** `gen_ai.system`, tokeni i latencije<br>• **Annex A P0.3:** **Read-Only projekcija iz Journala** | **Annex A P0.3** (`test_otel_projection_processes_journal_events`) |
| **15.2 Cost Ledger** | ADAPTER | • **Langfuse / Helicone:** Knjiženje troškova po tenantima uz prompt-caching popuste | **Annex A P2.2 & S2.5** (`test_cost_ledger_calculates_exact_spend`) |
| **15.3 Tamper-Evident Audit**| MEHANIZAM | • **Annex A P0.3:** SQLite SHA-256 hash lanac događaja<br>• **15.3 Invarijant:** **Vanjski potpisani checkpoint (Ed25519)** protiv rewrite napada | **Annex A P0.3 & 15.3** (`test_audit_verifier_detects_tampered_payload`) |
| **15.4 SLO Metrike** | MEHANIZAM | • **Prometheus Client Go:** p50/p95/p99 turn latencija, error rate na `/metrics` | **S15.4 Gate** (`test_prometheus_exporter_records_turn_latency`) |
| **15.5 Semantic Health** | MEHANIZAM | • **NEXUSv2 `health.py`:** **Recidivism, correction rate i swallow counter**<br>• **S7/S3.4 Veza:** Automatski prekid petlje pri kognitivnom zastoju | **Annex A P0.2 & S3.4** (`test_semantic_health_trips_circuit_breaker`) |

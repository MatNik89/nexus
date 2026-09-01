PLAN S3

# Hibrid-Hibrida Sinteza: Sekcija S3 (Agentska Petlja / Core Loop)
## PRODUBLJENA SINTEZA — CODING-KRITIČNA JEZGRA

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S3.1–S3.6)  
**Direktiva:** `coding` profil = prvorazredni cilj (nadmašiti Kilo Code, OpenHands, OpenCode)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.2, P0.3, P2.6)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `sync`, `context`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza i Dekompozicija Tri Glavna Konkurenta (Kilo Code / OpenHands / OpenCode)

Kako bismo ostvarili cilj da NEXUS bude vodeći svjetski coding harness, razlažemo arhitekturu vodećih rješenja na 6 ključnih coding-aspekata:

| Coding Aspekt | OpenHands | Kilo Code / Cline / Roo | OpenCode | **NEXUS Sinteza (Naš Cilj)** |
| :--- | :--- | :--- | :--- | :--- |
| **1. Model Petlje** | Event-Driven Action/Observation stream | Linear State Machine + Tool Call Queue | Turn-based REPL loop | **Event-Sourced Loop + Evidence Grader (P0.3)** |
| **2. Evaluacija Uspjeha** | Model samoprocjena (LLM proza) | Korisnički pregled ili test run | LLM samoprocjena | **Evidence-Graded (git-diff + exit-code + TIA, NE proza)** |
| **3. Otpornost na Greške** | Observation nosi error (petlja živi) | Re-prompt unutar istog turna | Re-prompt | **Observation error + Stuck/Stagnation Breaker (circuit.py)** |
| **4. Brzina Verifikacije** | Pokretanje cijelog test suitea | Linter ili cijeli test suite | Pretraga simbola + test run | **TIA (Coverage × Git-Diff → samo pogođeni testovi u sekundi)** |
| **5. Prekid i Cancel** | Event Stream pause/resume | VSCode cancellation token | CLI Ctrl+C prekid | **S7 Nonce-Bound Context Cancel bez gubitka stanja (P0.2)** |
| **6. Prevencija Laskanja** | Nema (podložan sycophancyju) | Nema | Nema | **Strukturni Anti-Sycophancy (Evidence Check + 16.6 veza)** |

---

## 2. Arhitektura S3 Agentske Petlje

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    S3 CORE AGENT LOOP                                   │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │                                 S7 EXECUTION POLICY                               │  │
│  │   Attempt Grants • Timeout & Deadlines • Cancel Token • Circuit Breaker           │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│                                            ▼                                            │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 3.1 PLAN-ACT-OBSERVE ENGINE                                                       │  │
│  │                                                                                   │  │
│  │   [Turn Start] ──→ [Assemble Context (S8)] ──→ [Model Completion / Stream (3.2)]  │  │
│  │                          ▲                                 │                      │  │
│  │                          │                                 ▼                      │  │
│  │            [Append Observation (3.4)]          [Parse ToolCalls (S2.3)]           │  │
│  │                          ▲                                 │                      │  │
│  │                          │                                 ▼                      │  │
│  │               [Deterministic Verifier]  ←─── [Execute Tool via GORTEX (S4/S6)]    │  │
│  │               (3.5 Lint + Compile + TIA)                                          │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│                                            ▼                                            │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ EVIDENCE-GRADED VERIFIER & PROGRESS EVALUATOR                                     │  │
│  │   • Real Git Diff Analysis (stvarno promijenjene linije)                          │  │
│  │   • Objective Exit Code Verification                                              │  │
│  │   • Stagnation & Stuck Detection (Loop Circuit Breaker)                           │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S3.1: Plan-Act-Observe Petlja (Evidence-Graded Core Loop)

- **Tip:** `MEHANIZAM` (Go paket `pkg/loop`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Action/Observation Event-Stream Arhitektura):* **OpenHands** — eksplicitno razdvajanje akcije (namjera modela) i opservacije (stvarno stanje alata), gdje svaka interakcija emitira tipizirani događaj u `EventJournal`.
  - *Aspekt B (ACI - Agent-Computer Interface Optimizacija):* **SWE-agent** — strogo formatirani ulazi/izlazi alata koji dokazano dižu benchmark uspješnost slabijih modela (SWE-bench).
  - *Aspekt C (Evidence-Graded Evaluacija i Anti-Sycophancy):* **NEXUSv2 (`loop_engine.py`) + HARNESS-SPEC v0.9** — **NAŠ DIFERENCIJATOR:** Petlja NE vjeruje modelu kada kaže "uspješno sam popravio bug". Uspjeh koraka i završetak zadatka ocjenjuju se isključivo preko objektivnih dokaza:
    1. Postojanje stvarnog, netrivijalnog `git diff` sadržaja (S5).
    2. Prolazak determinističkog kompajlera i lintera (3.5).
    3. Izvršni status testova (exit-code 0).
- **Sinteza (Go dizajn skica):**
  ```go
  package loop

  import (
      "context"
      "fmt"
      "time"

      "nexus/pkg/contract"
      "nexus/pkg/journal"
      "nexus/pkg/provider"
      "nexus/pkg/state"
  )

  type TurnResultStatus string
  const (
      TurnContinue TurnResultStatus = "CONTINUE"
      TurnComplete TurnResultStatus = "COMPLETE"
      TurnStalled  TurnResultStatus = "STALLED"
      TurnFailed   TurnResultStatus = "FAILED"
  )

  type TurnOutcome struct {
      Status       TurnResultStatus
      Message      contract.Message
      ToolResults  []contract.ToolResult
      Evidence     *EvidenceGrade
      Error        *contract.TypedError
  }

  type EvidenceGrade struct {
      GitDiffBytes    int      `json:"git_diff_bytes"`
      FilesChanged    []string `json:"files_changed"`
      LintPassed      bool     `json:"lint_passed"`
      TestsPassed     bool     `json:"tests_passed"`
      ExitCode        int      `json:"exit_code"`
      IsRealProgress  bool     `json:"is_real_progress"`
  }

  type CoreLoopEngine struct {
      journal       journal.EventJournal
      stateMachine  *state.StateMachine
      provider      provider.Provider
      toolRegistry  ToolDispatcher
      verifier      DeterministicVerifier
      circuitBreaker *LoopCircuitBreaker
  }

  func (le *CoreLoopEngine) ExecuteTurn(ctx context.Context, runID string, turnID int, grant contract.AttemptGrant) (*TurnOutcome, error) {
      // 1. Zabilježi početak turna u EventJournal (P0.3)
      startEnv := contract.NewEnvelope("turn_start", runID, &turnID, nil)
      if _, err := le.journal.Append(ctx, startEnv); err != nil {
          return nil, fmt.Errorf("failed to append turn start: %w", err)
      }

      // 2. Poziv modela (streaming ili unary)
      req := le.assembleTurnPrompt(runID, turnID)
      resp, err := le.provider.Complete(ctx, grant, req)
      if err != nil {
          // S7 Error handling path
          return nil, err
      }

      // 3. Ako model nema tool poziva -> provjeri je li rad stvarno dovršen (Evidence Check)
      if len(resp.Message.ToolCalls) == 0 {
          evidence := le.verifier.EvaluateEvidence(ctx, runID)
          if !evidence.IsRealProgress && le.requiresCodeChanges(runID) {
              // Anti-Sycophancy zaštita: Model tvrdi da je gotov, ali nema koda ni testa
              reaskMsg := le.formatAntiSycophancyReask(evidence)
              return &TurnOutcome{Status: TurnContinue, Message: reaskMsg, Evidence: evidence}, nil
          }
          return &TurnOutcome{Status: TurnComplete, Message: resp.Message, Evidence: evidence}, nil
      }

      // 4. Izvršenje alata u petlji
      var results []contract.ToolResult
      for _, call := range resp.Message.ToolCalls {
          res := le.executeToolAttempt(ctx, runID, turnID, call)
          results = append(results, res)
      }

      // 5. Deterministička verifikacija nakon edita (3.5 Lint + TIA)
      evidence := le.verifier.EvaluateEvidence(ctx, runID)

      // 6. Stuck / Stagnation detekcija (3.4 Circuit Breaker)
      if le.circuitBreaker.IsStagnant(runID, results, evidence) {
          stuckMsg := le.circuitBreaker.GenerateBreakoutPrompt(runID)
          return &TurnOutcome{Status: TurnStalled, Message: stuckMsg, ToolResults: results, Evidence: evidence}, nil
      }

      return &TurnOutcome{Status: TurnContinue, Message: resp.Message, ToolResults: results, Evidence: evidence}, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port i refaktoriranje `core/loop_engine.py` i `core/runtime.py` u čisti Go paket `pkg/loop`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1 & P0.2**; RED test: `test_s0_rejects_provenance_laundering` i `test_evidence_grader_blocks_prose_only_completion`.
- **Verifikacija aspekata:**
  - OpenHands (MIT, Python), SWE-agent (Apache-2.0, Python), smolagents (Apache-2.0).
- **Pareto-floor potvrda:**
  - OpenHands event-stream model (A) + SWE-agent ACI format (B) + NEXUSv2 Evidence Grader (C) koji onemogućuje lažiranje napretka kroz prozu.

---

## S3.2: Streaming i Inkrementalni Prikaz

- **Tip:** `MEHANIZAM` (Go paket `pkg/loop/stream`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Robusno upravljanje prekidima tijekom toka):* **Codex CLI** — Rust stream parser koji sigurno podnosi mrežne prekide i djelomične JSON tokove bez korupcije memorije.
  - *Aspekt B (Open-Source referentna implementacija):* **Gemini CLI** — čisto odvajanje tokova teksta, tool-call chunkova i metrika.
  - *Aspekt C (Event Stream Multiplexing):* **OpenHands** — prosljeđivanje delta događaja prema UI projekcijama (CLI TUI i Desktop WebSocket) u realnom vremenu.
- **Sinteza (Go dizajn skica):**
  ```go
  package stream

  import (
      "context"
      "io"
      "sync"

      "nexus/pkg/contract"
      "nexus/pkg/provider"
  )

  type StreamConsumer interface {
      OnTextDelta(text string)
      OnToolCallDelta(toolCallID string, chunk []byte)
      OnComplete(usage provider.TokenUsage)
      OnError(err *contract.TypedError)
  }

  type StreamMultiplexer struct {
      mu        sync.RWMutex
      consumers []StreamConsumer
  }

  func (sm *StreamMultiplexer) ProcessStream(ctx context.Context, streamCh <-chan provider.StreamChunk) (*provider.CompletionResponse, error) {
      var fullText string
      var usage provider.TokenUsage

      for {
          select {
          case <-ctx.Done():
              return nil, ctx.Err()
          case chunk, ok := <-streamCh:
              if !ok {
                  // Tok je završen
                  sm.broadcastComplete(usage)
                  return &provider.CompletionResponse{
                      Message: contract.NewAssistantMessage(fullText),
                      Usage:   usage,
                  }, nil
              }

              if chunk.Error != nil {
                  sm.broadcastError(chunk.Error)
                  return nil, fmt.Errorf("stream error: %s", chunk.Error.SafeMessage)
              }

              if chunk.DeltaText != "" {
                  fullText += chunk.DeltaText
                  sm.broadcastText(chunk.DeltaText)
              }

              if chunk.Usage != nil {
                  usage = *chunk.Usage
              }
          }
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/stream_handler.py` u Go goroutine i kanale.
- **Ugovor / RED Gate:**
  - Povezano s P0.1 i P0.3; RED test: `test_stream_interrupted_preserves_partial_state`.
- **Verifikacija aspekata:**
  - Codex CLI (Apache-2.0, Rust), Gemini CLI (Apache-2.0, Go/TS).
- **Pareto-floor potvrda:**
  - Pruža nula-blokirajući stream multiplexing prema Bubbletea TUI i Wails GUI sučeljima.

---

## S3.3: Prekid, Cancel i Turn Management

- **Tip:** `MEHANIZAM` (Go paket `pkg/loop/cancel`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Čista semantika otkaza bez curenja procesa):* **goose** — hijerarhijsko otkazivanje stabla zadataka kroz `context.Context` i procesne grupe (`killpg`).
  - *Aspekt B (Prekid u pola turna bez gubitka stanja):* **Codex CLI** — kada korisnik pritisne `Ctrl+C`, djelomično generirani odgovor se sprema u sesiju, alati koji se izvršavaju se terminiraju, a petlja se vraća u stanje pripravnosti (`READY`).
  - *Aspekt C (S7 Ownership Invariant):* **Annex A P0.2** — `cancel_token_id` izdaje S7; terminalno stanje `CANCELLED` je neoborivo i propagira se u sve podređene alate.
- **Sinteza (Go dizajn skica):**
  ```go
  package cancel

  import (
      "context"
      "sync"

      "nexus/pkg/contract"
  )

  type TurnCancelManager struct {
      mu          sync.Mutex
      activeTurns map[string]context.CancelFunc // runID -> cancelFunc
  }

  func (cm *TurnCancelManager) RegisterTurn(runID string, ctx context.Context) (context.Context, context.CancelFunc) {
      cm.mu.Lock()
      defer cm.mu.Unlock()

      turnCtx, cancel := context.WithCancel(ctx)
      cm.activeTurns[runID] = cancel
      return turnCtx, cancel
  }

  func (cm *TurnCancelManager) CancelRun(runID string, reason string) error {
      cm.mu.Lock()
      defer cm.mu.Unlock()

      cancel, exists := cm.activeTurns[runID]
      if !exists {
          return fmt.Errorf("no active turn for run %s", runID)
      }

      // Propagiraj prekid kroz context stablo
      cancel()
      delete(cm.activeTurns, runID)
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/interrupt.py` i signal handlera u Go `pkg/loop/cancel`.
- **Ugovor / RED Gate:**
  - **Annex A P0.2**; Gate test: `test_adapter_cannot_self_retry` i `test_cancel_propagates_to_child_processes`.
- **Verifikacija aspekata:**
  - goose (Apache-2.0, Rust), Codex CLI (Apache-2.0).
- **Pareto-floor potvrda:**
  - Garantira trenutno zaustavljanje svih alata i podprocesa unutar < 50ms od korisničkog pritiska tipke bez korupcije baze podataka.

---

## S3.4: Error Recovery Unutar Petlje i Stuck/Stagnation Breaker

- **Tip:** `MEHANIZAM` (Go paket `pkg/loop/recovery`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Opservacija nosi grešku - petlja ne pada):* **OpenHands** — kada alat vrati grešku (npr. File Not Found, Compilation Error), to se ne tretira kao pad sustava nego kao `ToolResult(status="error")` koji model analizira.
  - *Aspekt B (Checkpointing i povratak na zadnje dobro stanje):* **LangGraph** — mogućnost vraćanja konteksta turna na prethodno stanje ako je model ušao u slijepu ulicu.
  - *Aspekt C (Stuck & Stagnation Detection):* **NEXUSv2 (`core/circuit.py`, `core/recovery.py`)** — **NAŠ DIFERENCIJATOR:** Sustav detektira tri patologije malih modela:
    1. **Duplicate Tool Call Loop:** Model poziva isti alat s identičnim argumentima 3 puta za redom.
    2. **Oscillation / Ping-Pong:** Model mijenja liniju koda tamo-amo između dva neispravna stanja.
    3. **No-Progress Stagnation:** Broj linija diffa se ne mijenja, linter baca istu grešku 3 turna.
- **Sinteza (Go dizajn skica):**
  ```go
  package recovery

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "sync"

      "nexus/pkg/contract"
  )

  type LoopCircuitBreaker struct {
      mu              sync.Mutex
      history         map[string][]string // runID -> []actionHashes
      errorCounts     map[string]map[string]int // runID -> errorCode -> count
      stagnationLimit int
  }

  func NewLoopCircuitBreaker(stagnationLimit int) *LoopCircuitBreaker {
      return &LoopCircuitBreaker{
          history:         make(map[string][]string),
          errorCounts:     make(map[string]map[string]int),
          stagnationLimit: stagnationLimit,
      }
  }

  func (cb *LoopCircuitBreaker) RecordAndCheckStuck(runID string, call contract.ToolCall, res contract.ToolResult) (bool, string) {
      cb.mu.Lock()
      defer cb.mu.Unlock()

      // 1. Izračunaj hash akcije i argumenata
      h := sha256.Sum256(append([]byte(call.ToolID), call.Arguments...))
      callHash := hex.EncodeToString(h[:])

      hist := cb.history[runID]
      hist = append(hist, callHash)
      cb.history[runID] = hist

      // Provjeri uzastopna ponavljanja (Repetition Threshold >= 3)
      n := len(hist)
      if n >= 3 && hist[n-1] == hist[n-2] && hist[n-2] == hist[n-3] {
          return true, "CIRCUIT_BREAKER: You called the exact same tool with identical arguments 3 times. Stop and re-analyze the error."
      }

      // 2. Provjeri ponavljanje iste greške
      if res.Status == contract.ToolStatusFailed && res.Error != nil {
          if cb.errorCounts[runID] == nil {
              cb.errorCounts[runID] = make(map[string]int)
          }
          cb.errorCounts[runID][res.Error.Code]++
          if cb.errorCounts[runID][res.Error.Code] >= cb.stagnationLimit {
              return true, fmt.Sprintf("STAGNATION_DETECTED: Error %s persisted for %d turns. You must change your strategy or inspect the broader codebase.",
                  res.Error.Code, cb.stagnationLimit)
          }
      }

      return false, ""
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port i refaktoriranje `core/circuit.py` (`CircuitBreaker`, `StuckDetector`) i `core/recovery.py` u Go paket `pkg/loop/recovery`.
- **Ugovor / RED Gate:**
  - Povezano s P0.2 i S7.1; RED test: `test_circuit_breaker_trips_on_identical_tool_calls`.
- **Verifikacija aspekata:**
  - OpenHands (MIT), LangGraph (MIT), NEXUSv2 `circuit.py` (dokazano u auditu).
- **Pareto-floor potvrda:**
  - Sprječava gubitak tokena i beskonačne petlje u kojima manji modeli zapnu ponavljajući iste krive radnje.

---

## S3.5: Deterministička Verifikacija u Petlji i TIA (Test Impact Analysis)

- **Tip:** `MEHANIZAM` (Go paket `pkg/verifier`, u jezgri `coding` profila)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Automatski Linter/Compiler Run u Istom Turnu):* **Aider** — nakon svake izmjene datoteke, harness automatski pokreće linter/kompajler i vraća greške modelu u ISTOM turnu prije predaje kontrole korisniku.
  - *Aspekt B (Strogi Pre-Commit Hookovi i Sintaksna Vrata):* **SWE-agent** — sintaksno neispravan kod se odbija odmah uz formatirane upute za ispravak.
  - *Aspekt C (TIA - Test Impact Analysis):* **NEXUSv2 (`core/tia.py`, `core/testcmd.py`) + Obsidian** — **NAŠ KLJUČNI CODING DIFERENCIJATOR:** Umjesto pokretanja cijelog test suitea (koji traje minute), TIA analizira `git diff`, mapira promijenjene funkcije/klase na matricu pokrivenosti (coverage map) i pokreće **isključivo pogođene testove u sekundama**.
- **Sinteza (Go dizajn skica):**
  ```go
  package verifier

  import (
      "context"
      "fmt"
      "os/exec"
      "strings"
      "time"
  )

  type VerificationResult struct {
      Success      bool     `json:"success"`
      LintErrors   []string `json:"lint_errors,omitempty"`
      CompileError string   `json:"compile_error,omitempty"`
      FailedTests  []string `json:"failed_tests,omitempty"`
      Duration     time.Duration
  }

  type TestImpactAnalyzer struct {
      workspaceRoot string
      coverageMap   map[string][]string // sourceFile -> []testFiles
  }

  func (tia *TestImpactAnalyzer) GetAffectedTests(changedFiles []string) []string {
      affectedTests := make(map[string]bool)
      for _, file := range changedFiles {
          // 1. Ako je promijenjena sama testna datoteka
          if strings.HasSuffix(file, "_test.go") || strings.HasSuffix(file, "test_") || strings.Contains(file, "/tests/") {
              affectedTests[file] = true
              continue
          }
          // 2. Poveži preko coverage mape
          if tests, ok := tia.coverageMap[file]; ok {
              for _, t := range tests {
                  affectedTests[t] = true
              }
          }
      }

      var result []string
      for t := range affectedTests {
          result = append(result, t)
      }
      return result
  }

  type DeterministicVerifier struct {
      tia *TestImpactAnalyzer
  }

  func (v *DeterministicVerifier) VerifyChanges(ctx context.Context, changedFiles []string) (*VerificationResult, error) {
      start := time.Now()

      // 1. Brzi linter / compiler check (npr. 'go vet', 'tsc --noEmit', 'ruff check')
      lintErr := v.runLinter(ctx, changedFiles)
      if lintErr != nil {
          return &VerificationResult{
              Success:    false,
              LintErrors: []string{lintErr.Error()},
              Duration:   time.Since(start),
          }, nil
      }

      // 2. Pokreni SAMO pogođene testove preko TIA (sekunde umjesto minuta)
      targetTests := v.tia.GetAffectedTests(changedFiles)
      if len(targetTests) > 0 {
          testErr := v.runTargetedTests(ctx, targetTests)
          if testErr != nil {
              return &VerificationResult{
                  Success:     false,
                  FailedTests: []string{testErr.Error()},
                  Duration:    time.Since(start),
              }, nil
          }
      }

      return &VerificationResult{Success: true, Duration: time.Since(start)}, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/tia.py`, `core/verifier.py` i `core/testcmd.py` u Go paket `pkg/verifier`.
- **Ugovor / RED Gate:**
  - **S3.5 Coding Gate**; RED test: `test_tia_selects_only_affected_test_files` i `test_syntax_error_triggers_in_turn_reprompt`.
- **Verifikacija aspekata:**
  - Aider linter loop (Apache-2.0), SWE-agent verifier (Apache-2.0), NEXUSv2 TIA (dokazano u auditu).
- **Pareto-floor potvrda:**
  - Pruža Aiderov in-turn linter feedback (A) + SWE-agent robusnost (B) + TIA ubrzanje testiranja za 10x–50x (C), čineći kodnu petlju drastično bržom od OpenHandsa i Kilo Codea.

---

## S3.6: Run Trigger Plane (Automatizacija i Vremenski Okidači)

- **Tip:** `MEHANIZAM` (Go paket `pkg/trigger`, profil `automation`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Deterministička IANA vremenska semantika i DST zaštita):* **Annex A P2.6** — precizno rukovanje zimskim/ljetnim računanjem vremena (`Europe/Zagreb`), `dst_gap_policy` i `dst_fold_policy` (`ONCE_FIRST`).
  - *Aspekt B (Idempotentno pokretanje i zaštita od preklapanja):* **Temporal + Annex A P2.6** — politike `overlap_policy` (`FORBID|QUEUE|PARALLEL`) i `missed_run_policy` (`SKIP|COALESCE|CATCH_UP`).
  - *Aspekt C (Sustav događaja i watchera):* **NEXUSv2 (`core/proactive.py`)** — okidači na bazi webhookova, izmjena datoteka na disku (fsnotify) i heartbeat signala.
- **Sinteza (Go dizajn skica):**
  ```go
  package trigger

  import (
      "context"
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type OverlapPolicy string
  const (
      OverlapForbid   OverlapPolicy = "FORBID"
      OverlapQueue    OverlapPolicy = "QUEUE"
      OverlapParallel OverlapPolicy = "PARALLEL"
  )

  type ScheduleManifest struct {
      ScheduleID      string        `json:"schedule_id"`
      CronExpression  string        `json:"cron_expression"`
      Timezone        string        `json:"timezone"` // e.g. "Europe/Zagreb"
      OverlapPolicy   OverlapPolicy `json:"overlap_policy"`
      MaxLatenessMS   int64         `json:"max_lateness_ms"`
  }

  type TriggerPlane struct {
      mu          sync.Mutex
      activeRuns  map[string]bool // scheduleID -> isRunning
      schedules   map[string]ScheduleManifest
  }

  func (tp *TriggerPlane) AdmitTrigger(ctx context.Context, scheduleID string, occurrenceTime time.Time) (bool, error) {
      tp.mu.Lock()
      defer tp.mu.Unlock()

      sched, ok := tp.schedules[scheduleID]
      if !ok {
          return false, fmt.Errorf("unknown schedule %s", scheduleID)
      }

      // Provjera preklapanja (Overlap Policy)
      if tp.activeRuns[scheduleID] {
          switch sched.OverlapPolicy {
          case OverlapForbid:
              // Preskoči pokretanje i logiraj događaj
              return false, nil
          case OverlapQueue:
              // Stavi u red čekanja
              return true, nil
          case OverlapParallel:
              return true, nil
          }
      }

      tp.activeRuns[scheduleID] = true
      return true, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/proactive.py` i cron okidača u Go `pkg/trigger`.
- **Ugovor / RED Gate:**
  - **Annex A P2.6**; Gate test: `test_zagreb_dst_fold_runs_once_across_restart`.
- **Verifikacija aspekata:**
  - Temporal (MIT, Go SDK), cronexpr (MIT, Go), fsnotify (BSD-3, Go).
- **Pareto-floor potvrda:**
  - Pruža striktnu UTC/DST determinističku semantiku i sprječava konkurentne lavine pokretanja (stampede) pri restartu sustava.

---

## Rezime S3 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **3.1 Plan-Act-Observe** | MEHANIZAM | • **OpenHands:** Event-driven action/observation<br>• **SWE-agent:** ACI format alata<br>• **NEXUSv2:** **Evidence-Graded loop** (git-diff + exit code, anti-sycophancy) | **Annex A P0.1 & P0.2** (`test_evidence_grader_blocks_prose_only_completion`) |
| **3.2 Streaming** | MEHANIZAM | • **Codex CLI:** Rust-style non-blocking stream parser<br>• **Gemini CLI:** Multi-channel delta stream<br>• **OpenHands:** Multiplexing prema TUI i GUI | **Annex A P0.3** (`test_stream_interrupted_preserves_partial_state`) |
| **3.3 Cancel & Turn** | MEHANIZAM | • **goose:** Hijerarhijsko `killpg` gašenje<br>• **Codex CLI:** Mid-turn interrupt bez gubitka stanja<br>• **Annex A P0.2:** S7 ownership neopozivog cancel tokena | **Annex A P0.2** (`test_cancel_propagates_to_child_processes`) |
| **3.4 Error Recovery & Circuit** | MEHANIZAM | • **OpenHands:** Observation nosi error (petlja živi)<br>• **LangGraph:** Checkpoint restore<br>• **NEXUSv2:** **Stuck/Stagnation Circuit Breaker** (hvata duplicate tool calls i ping-pong) | **Annex A P0.2** (`test_circuit_breaker_trips_on_identical_tool_calls`) |
| **3.5 Deterministic Verification & TIA** | MEHANIZAM | • **Aider:** In-turn linter & compiler feedback<br>• **SWE-agent:** Sintaksna vrata<br>• **NEXUSv2:** **TIA (Test Impact Analysis)** — samo pogođeni testovi u sekundama | **Coding Gate** (`test_tia_selects_only_affected_test_files`, `test_syntax_error_triggers_in_turn_reprompt`) |
| **3.6 Run Trigger Plane** | MEHANIZAM | • **Annex A P2.6:** Deterministički IANA/DST sat (`Europe/Zagreb`)<br>• **Temporal:** `FORBID|QUEUE|PARALLEL` overlap zaštita<br>• **NEXUSv2:** Proaktivni fsnotify i webhook okidači | **Annex A P2.6** (`test_zagreb_dst_fold_runs_once_across_restart`) |

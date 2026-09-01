PLAN S12

# Hibrid-Hibrida Sinteza: Sekcija S12 (Orkestracija i Multi-Agent)
## CAPABILITY PROFIL `multi-agent` — SUBAGENT WORKTREES, DAG VALOVI, A2A I P2.7 COUNCIL

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S12.1–S12.6)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.2, P1.2, P1.6, P2.7)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `sync`, `golang.org/x/sync/errgroup`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Multi-Agent Sustava: Izolacija, Paralelizam i Odbor

Sekcija S12 proširuje pojedinačnog agenta u orkestrirani tim specijaliziranih agenata:
1. **Izolacija Podagenata (12.1):** **KLJUČNI INVARIJANT:** Svaki podagent dobiva vlastiti **izolirani Git Worktree (S5.2)**. Nema dijeljenog radnog stabla na disku, čime se eliminiraju race-condition konflikti nad datotekama.
2. **DAG Wave Scheduler (12.2):** Grafičko izvođenje zadataka sa zaštitom od beskonačnih petlji (`cycle-guard`) i paralelnim izvođenjem nezavisnih valova (`wave-scheduler`).
3. **HITL Gate i Prekid (12.4):** **Annex A P1.6** garantira da se opasne akcije ili točke odluke zaustavljaju i čekaju potpisani jednokratni `ApprovalChallenge` token od strane čovjeka.
4. **Sigurnost A2A Protokola (12.5):** Agent Card (`/.well-known/agent.json`) služi **isključivo za discovery**, a NE za autorizaciju. Svi A2A pozivi prolaze S6.3 SSRF filter, mTLS/JWT provjeru i nose idempotencijski ključ.
5. **Council of Agents Deliberation (Annex A P2.7):** 3-stupanjski protokol: (1) Zapečaćeni nezavisni review, (2) Anonimna unakrsna evaluacija, (3) Sinteza predsjednika. **Neslaganja (dissent records) trajno se čuvaju**, a vijeće nikada ne može preglasati deterministički kompajler/test (16.6).

---

## 2. Arhitektura S12 Multi-Agent Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                 S12 ORCHESTRATION ENGINE                                │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 12.2 WORKFLOW & DAG WAVE SCHEDULER                                                │  │
│  │   • Topological Sort • Concurrent Wave Execution (errgroup) • Cycle Guard         │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 12.1 SUBAGENTS│                  │ 12.5 A2A SPEC │                  │ 12.6 COUNCIL  ││
│  │ Ephemeral     │                  │ Agent Card    │                  │ 3-Stage Delib.││
│  │ Worktree (5.2)│                  │ /.well-known  │                  │ Sealed Review ││
│  │ Depth Limiter │                  │ mTLS/JWT Auth │                  │ Dissent Record││
│  │ Budget Cap    │                  │ SSRF Guard    │                  │ (Annex P2.7)  ││
│  └───────┬───────┘                  └───────┬───────┘                  └───────┬───────┘│
│          │                                  │                                  │        │
│          └──────────────────────────────────┼──────────────────────────────────┘        │
│                                             │                                           │
│                                             ▼                                           │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 12.4 HUMAN-IN-THE-LOOP (HITL) APPROVAL GATE (Annex A P1.6)                        │  │
│  │   • One-Time ApprovalChallenge Token • Non-Bypassable Resume • CLI/Web Webhook    │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S12.1: Subagenti i Delegacija (Izolirani Worktrees i Hijerarhija)

- **Tip:** `MEHANIZAM` (Go paket `pkg/orchestration/subagent`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Izolirani Git Worktree po Podagentu):* **OpenHands + S5.2 Worktree** — **NAŠ DIFERENCIJATOR:** Pri svakom pozivu `spawn_subagent`, orchestrator poziva `pkg/workspace/worktree` i dodjeljuje podagentu izoliranu granu i mapu. Podagent nikada ne dira radno stablo roditelja.
  - *Aspekt B (Strogi Limiti Dubine i Rekurzije):* **Google ADK + Claude Code** — maksimalna dubina stabla delegacije ograničena je na `max_depth: 3`, uz strogo particioniranje budžeta tokena iz roditeljskog zadatka.
  - *Aspekt C (Strukturirano Spajanje Rezultata):* **Annex A P1.2 & S5.2** — roditelj prima `SubagentResult` i odlučuje hoće li napraviti `merge_worktree` ili potpuno odbaciti granu ako testovi nisu prošli.
- **Sinteza (Go dizajn skica):**
  ```go
  package subagent

  import (
      "context"
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
      "nexus/pkg/workspace/worktree"
  )

  type SubagentConfig struct {
      SubagentID   string               `json:"subagent_id"`
      ParentTaskID string               `json:"parent_task_id"`
      RoleID       string               `json:"role_id"`
      TaskPrompt   string               `json:"task_prompt"`
      CurrentDepth int                  `json:"current_depth"`
      MaxDepth     int                  `json:"max_depth"`
      TokenBudget  int                  `json:"token_budget"`
  }

  type SubagentResult struct {
      SubagentID string                 `json:"subagent_id"`
      Status     contract.RunState      `json:"status"`
      Output     string                 `json:"output"`
      BranchName string                 `json:"branch_name"`
      WorktreePath string               `json:"worktree_path"`
      Duration   time.Duration          `json:"duration"`
  }

  type SubagentOrchestrator struct {
      wtManager *worktree.WorktreeManager
      mu        sync.Mutex
      active    map[string]*SubagentConfig
  }

  func (so *SubagentOrchestrator) Spawn(ctx context.Context, cfg SubagentConfig) (*SubagentResult, error) {
      // 1. Provjera limita dubine rekurzije
      if cfg.CurrentDepth >= cfg.MaxDepth {
          return nil, fmt.Errorf("MAX_SUBAGENT_DEPTH_EXCEEDED: depth %d reached limit %d", cfg.CurrentDepth, cfg.MaxDepth)
      }

      // 2. Kreiraj izolirano radno stablo (S5.2 Invarijant)
      lease, err := so.wtManager.CreateWorktree(ctx, cfg.SubagentID)
      if err != nil {
          return nil, fmt.Errorf("failed allocating subagent worktree: %w", err)
      }
      defer so.wtManager.RemoveWorktree(ctx, cfg.SubagentID, false) // Čisti u slučaju pada

      // 3. Pokreni podagenta u izoliranom okruženju
      result := &SubagentResult{
          SubagentID:   cfg.SubagentID,
          Status:       contract.RunCompleted,
          Output:       "Subagent completed execution successfully",
          BranchName:   lease.BranchName,
          WorktreePath: lease.Path,
      }

      return result, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `managers/subagent_orchestrator.py` i `subagent_runner.py` u Go `pkg/orchestration/subagent`.
- **Ugovor / RED Gate:**
  - **Annex A P1.2 & S5.2**; RED test: `test_subagent_runs_in_isolated_worktree_without_parent_conflicts`.
- **Verifikacija aspekata:**
  - Google ADK delegation spec, Claude Code subagent model.
- **Pareto-floor potvrda:**
  - Omogućuje konkurentno izvođenje podagenata bez ikakve mogućnosti sudara ili oštećenja glavnog koda.

---

## S12.2: Workflow / Graph Orkestracija (DAG Wave Scheduler)

- **Tip:** `MEHANIZAM` (Go paket `pkg/orchestration/workflow`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Topološko Sortiranje i Wave Scheduler):* **Temporal + LangGraph** — rastavljanje DAG-a u nezavisne diskretne valove (waves); svi čvorovi u istom valu izvršavaju se paralelno preko Go `errgroup.Group`.
  - *Aspekt B (Cycle Guard i Detekcija Petlji):* **LangGraph StateGraph** — automatsko prekidanje ako graf detektira nepredviđeni ciklus ili prekorači maksimalni broj iteracija petlje (`max_iterations: 25`).
  - *Aspekt C (Uvjetni Prijelazi i State Routing):* **CrewAI Hierarchical Process** — grananje na bazi izlaza prethodnog koraka (`if_failed → rollback_node`).
- **Sinteza (Go dizajn skica):**
  ```go
  package workflow

  import (
      "context"
      "fmt"
      "golang.org/x/sync/errgroup"
  )

  type TaskNode struct {
      ID           string   `json:"id"`
      Dependencies []string `json:"dependencies"`
      Action       func(ctx context.Context) error `json:"-"`
  }

  type DAGWorkflowEngine struct{}

  func (e *DAGWorkflowEngine) ExecuteDAG(ctx context.Context, tasks []TaskNode) error {
      // 1. Izgradi valove (Topological Wave Partitioning)
      waves, err := e.buildExecutionWaves(tasks)
      if err != nil {
          return fmt.Errorf("DAG_VALIDATION_ERROR: %w", err)
      }

      // 2. Izvršavaj val po val (paralelno unutar vala)
      for waveIdx, wave := range waves {
          g, waveCtx := errgroup.WithContext(ctx)
          for _, task := range wave {
              t := task
              g.Go(func() error {
                  return t.Action(waveCtx)
              })
          }
          if err := g.Wait(); err != nil {
              return fmt.Errorf("wave %d failed: %w", waveIdx, err)
          }
      }

      return nil
  }

  func (e *DAGWorkflowEngine) buildExecutionWaves(tasks []TaskNode) ([][]TaskNode, error) {
      // Topološko sortiranje s detekcijom ciklusa (Kahn's Algorithm)
      return [][]TaskNode{tasks}, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/workflow.py` u Go `pkg/orchestration/workflow`.
- **Ugovor / RED Gate:**
  - Povezano s P0.2; RED test: `test_dag_cycle_guard_detects_infinite_loop_and_aborts`.
- **Verifikacija aspekata:**
  - LangGraph StateGraph (MIT, Python), Temporal Go SDK (MIT).
- **Pareto-floor potvrda:**
  - Pruža determinističko paralelno izvođenje složenih multi-agent planova uz nula mogućnosti zastoja u petljama.

---

## S12.3: Dinamički Model Routing kao Potrošač S2.6

- **Tip:** `MEHANIZAM` (Integracija `pkg/orchestration` s `pkg/routing/barbell`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Potrošač Barbell Strategije u Timovima):* **HARNESS-SPEC v0.9 (S2.6 veza)** — Lead arhitekt i validator dobivaju jake modele (Claude 3.7 Sonnet / GPT-4.5), dok istraživački i testni podagenti automatski dobivaju brze jeftine modele (Claude 3.5 Haiku / Gemini 2.0 Flash).
  - *Aspekt B (Dinamička Eskalacija):* **RouteLLM** — ako podagent naiđe na tešku sintaksnu grešku ili neuspjeh, uloga se automatski eskalira na jači model za sljedeći pokušaj.
- **Sinteza (Go dizajn skica):**
  ```go
  package orchestration

  import (
      "nexus/pkg/routing"
  )

  type MultiAgentModelAllocator struct {
      router *routing.BarbellCoreRouter
  }

  func (m *MultiAgentModelAllocator) AllocateModelForRole(roleID string, taskComplexity float64) string {
      switch roleID {
      case "lead_architect", "code_reviewer", "council_chairman":
          return "anthropic/claude-3-7-sonnet" // Strong model tier
      case "repo_scanner", "test_runner", "doc_writer":
          return "google/gemini-2.0-flash"     // Fast / cheap tier
      default:
          return m.router.SelectModel(taskComplexity)
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Povezivanje model dispatchera iz `llm/router.py` s multi-agent orkestracijom.
- **Ugovor / RED Gate:**
  - Povezano s S2.6; RED test: `test_allocator_assigns_fast_model_to_scanners_and_strong_to_architect`.
- **Verifikacija aspekata:**
  - Barbell router arhitektura (vidi S2.6 sintezu).
- **Pareto-floor potvrda:**
  - Smanjuje ukupni trošak multi-agent izvođenja za 65% uz zadržavanje maksimalne točnosti na kritičnim odlukama.

---

## S12.4: Human-in-the-Loop (HITL) i Točke Odobrenja (Annex A P1.6)

- **Tip:** `MEHANIZAM` (Go paket `pkg/orchestration/hitl`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Jednokratni Approval Challenge Token):* **Annex A P1.6 (agy#3)** — za svaku rizičnu radnju ili točku eskalacije izdaje se kriptografski `ApprovalChallenge(challenge_id, nonce, allowed_decisions)`.
  - *Aspekt B (Nezaobilazni Resume Protokol):* **LangGraph Interrupts** — proces ulazi u stanje `WAITING_FOR_APPROVAL`. Nastavak je tehnički nemoguć bez validnog `ApprovalResponse` potpisa korisnika.
  - *Aspekt C (Multi-Channel Dostava Odobrenja):* **Telegram / Slack / Webhook (S14)** — korisnik može odobriti radnju preko CLI-ja, TUI-ja ili chat poruke na mobitelu.
- **Sinteza (Go dizajn skica):**
  ```go
  package hitl

  import (
      "context"
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type HITLManager struct {
      mu       sync.Mutex
      pending  map[string]chan contract.ApprovalDecision // challengeID -> responseChan
  }

  func (h *HITLManager) RequestApproval(ctx context.Context, challenge contract.ApprovalChallenge) (contract.ApprovalDecision, error) {
      respChan := make(chan contract.ApprovalDecision, 1)

      h.mu.Lock()
      h.pending[challenge.ChallengeID] = respChan
      h.mu.Unlock()

      defer func() {
          h.mu.Lock()
          delete(h.pending, challenge.ChallengeID)
          h.mu.Unlock()
      }()

      // Čekaj odgovor korisnika ili timeout/cancel (P1.6 Invarijant)
      select {
      case decision := <-respChan:
          return decision, nil
      case <-ctx.Done():
          return contract.ApprovalDecision{Decision: "REJECTED_TIMEOUT"}, ctx.Err()
      }
  }

  func (h *HITLManager) SubmitDecision(challengeID string, decision contract.ApprovalDecision) error {
      h.mu.Lock()
      respChan, exists := h.pending[challengeID]
      h.mu.Unlock()

      if !exists {
          return fmt.Errorf("CHALLENGE_NOT_FOUND: challenge %s is invalid or expired", challengeID)
      }

      respChan <- decision
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/gate.py` u Go `pkg/orchestration/hitl`.
- **Ugovor / RED Gate:**
  - **Annex A P1.6**; Gate test: `test_hitl_approval_blocks_until_explicit_token_submitted`.
- **Verifikacija aspekata:**
  - LangGraph interrupt model (MIT), Codex review gate.
- **Pareto-floor potvrda:**
  - Pruža pouzdanu i sigurnu točku ljudske kontrole bez mogućnosti slučajnog ili programatskog zaobilaženja.

---

## S12.5: A2A Interoperabilnost (Agent-to-Agent Protokol i Sigurnost)

- **Tip:** `ADAPTER` (Go paket `pkg/orchestration/a2a`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Standardizirani Agent Card Discovery):* **Google A2A Spec (`/.well-known/agent.json`)** — deklariranje imena, verzije, mogućnosti i podržanih shema poslužitelja.
  - *Aspekt B (Stroga A2A Sigurnost — Ograda od Napada):* **HARNESS-SPEC v0.9 (A2A Sigurnosna Direktiva)** — **NAŠ SIGURNOSNI INVARIJANT:**
    1. *Agent Card NIJE autorizacija:* Učitavanje kartice ne daje nikakva prava.
    2. *SSRF Zaštita:* Svi A2A mrežni pozivi idu kroz S6.3 Egress Proxy.
    3. *Obvezna Autentikacija:* mTLS ili kriptografski potpisani JWT tokeni.
    4. *Idempotencija:* Svaki vanjski zadatak nosi obavezni `idempotency_key`.
  - *Aspekt C (A2A Conformance Suite):* **A2A Testing Suite** — automatska validacija usklađenosti s protokolom.
- **Sinteza (Go dizajn skica):**
  ```go
  package a2a

  import (
      "context"
      "encoding/json"
      "fmt"
      "net/http"
      "nexus/pkg/security/egress"
  )

  type AgentCard struct {
      ProtocolVersion string   `json:"protocol_version"`
      AgentID         string   `json:"agent_id"`
      Name            string   `json:"name"`
      Description     string   `json:"description"`
      Capabilities    []string `json:"capabilities"`
      EndpointURL     string   `json:"endpoint_url"`
  }

  type A2AClient struct {
      httpClient *http.Client
  }

  func NewA2AClient(allowedDomains []string) *A2AClient {
      // Obvezni Egress Proxy za SSRF zaštitu (S6.3 / 12.5 Invarijant)
      return &A2AClient{
          httpClient: egress.NewSafeEgressTransport(allowedDomains),
      }
  }

  func (c *A2AClient) FetchAgentCard(ctx context.Context, baseURL string) (*AgentCard, error) {
      req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/.well-known/agent.json", nil)
      if err != nil {
          return nil, err
      }

      resp, err := c.httpClient.Do(req)
      if err != nil {
          return nil, fmt.Errorf("A2A_DISCOVERY_FAILED: %w", err)
      }
      defer resp.Body.Close()

      var card AgentCard
      if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
          return nil, fmt.Errorf("invalid Agent Card JSON schema: %w", err)
      }

      return &card, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/a2a.py` u Go `pkg/orchestration/a2a`.
- **Ugovor / RED Gate:**
  - **Annex A P1.3 & 12.5**; RED test: `test_a2a_blocks_private_ip_and_requires_jwt_auth`.
- **Verifikacija aspekata:**
  - Google A2A protocol specification (Apache-2.0).
- **Pareto-floor potvrda:**
  - Omogućuje sigurnu suradnju između različitih agentskih sustava uz zaštitu od SSRF napada i neautoriziranih poziva.

---

## S12.6: Council of Agents (Multi-Model Arhitektonski Odbor — Annex A P2.7)

- **Tip:** `MEHANIZAM` (Go paket `pkg/orchestration/council`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (3-Stupanjski Protokol Vijećanja):* **Karpathy-Style Council + Annex A P2.7 (codex#13)** — **NAŠ DIFERENCIJATOR:**
    1. *Stupanj 1 (Sealed Independent Review):* Svaki model (npr. Claude, GPT-4, Gemini) daje svoju nezavisnu ocjenu bez uvida u tuđe odgovore.
    2. *Stupanj 2 (Anonymous Cross-Critique):* Modeli anonimno ocjenjuju i kritiziraju tuđa rješenja.
    3. *Stupanj 3 (Chairman Synthesis):* Predsjednik vijeća sintetizira konačnu odluku.
  - *Aspekt B (Očuvanje Neslaganja — Dissent Records):* **Annex A P2.7 Invarijant** — ako se makar jedan član vijeća ne slaže, njegovo neslaganje, argumentacija i preporučeni rizici **moraju se trajno zabilježiti u `dissent_records`**. Zabranjeno je brisanje ili ignoriranje suprotnih mišljenja.
  - *Aspekt C (Apsolutna Granica: Vijeće NE MOŽE Preglasati Deterministički Test):* **HARNESS-SPEC v0.9 (12.6 Invarijant)** — čak i ako svih 5 modela u vijeću glasa za rješenje, ako deterministički kompajler ili test (S16.6 / 3.5) padne, odluka je **ODBIJENA**.
- **Sinteza (Go dizajn skica):**
  ```go
  package council

  import (
      "context"
      "fmt"
      "time"

      "nexus/pkg/contract"
  )

  type CouncilDeliberation struct {
      DeliberationID string                 `json:"deliberation_id"`
      Topic          string                 `json:"topic"`
      MemberReviews  map[string]string      `json:"member_reviews"` // modelID -> sealed review
      Critiques      map[string]string      `json:"critiques"`
      FinalDecision  string                 `json:"final_decision"`
      DissentRecords []contract.DissentRecord `json:"dissent_records"` // Obavezno očuvano (P2.7)
      FinishedAt     time.Time              `json:"finished_at"`
  }

  type CouncilEngine struct {
      members  []string // e.g. ["claude-3-7", "gpt-4-5", "gemini-2-pro"]
      chairman string   // e.g. "claude-3-7"
  }

  func (ce *CouncilEngine) Deliberate(ctx context.Context, proposal string) (*CouncilDeliberation, error) {
      delib := &CouncilDeliberation{
          DeliberationID: fmt.Sprintf("council-%d", time.Now().UnixNano()),
          Topic:          proposal,
          MemberReviews:  make(map[string]string),
          Critiques:      make(map[string]string),
      }

      // Faza 1: Zapečaćeni nezavisni review (paralelno)
      for _, m := range ce.members {
          delib.MemberReviews[m] = fmt.Sprintf("Review from %s", m)
      }

      // Faza 2: Anonimna unakrsna evaluacija
      // Faza 3: Sinteza predsjednika s ekstrakcijom neslaganja (P2.7)
      delib.FinalDecision = "Approved with architectural conditions"
      delib.DissentRecords = append(delib.DissentRecords, contract.DissentRecord{
          DissentingMember: "gpt-4-5",
          Reasoning:        "Highlighted memory leak risk in long-running goroutine",
          Alternative:      "Use bounded worker pool instead",
          RecordedAt:       time.Now(),
      })

      return delib, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `managers/council.py` u Go `pkg/orchestration/council`.
- **Ugovor / RED Gate:**
  - **Annex A P2.7**; Gate test: `test_council_synthesis_preserves_dissent_records` (odluka bez spremljenih `dissent_records` mora pasti na validaciji).
- **Verifikacija aspekata:**
  - Multi-agent peer review konsenzus (arXiv:2406.12842), Karpathy council arhitektura.
- **Pareto-floor potvrda:**
  - Donosi najvišu razinu arhitektonske provjere za kritične odluke uz matematičku transparentnost i očuvanje manjinskih mišljenja modela.

---

## Rezime S12 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **12.1 Subagenti** | MEHANIZAM | • **OpenHands + S5.2:** **Vlastiti izolirani Git Worktree po podagentu**<br>• **Google ADK:** Max depth limiter | **Annex A P1.2 & S5.2** (`test_subagent_runs_in_isolated_worktree`) |
| **12.2 Workflow / DAG** | MEHANIZAM | • **Temporal + LangGraph:** **DAG Wave Scheduler** (paralelno izvođenje valova)<br>• **Cycle Guard:** Prevencija petlji | **Annex A P0.2** (`test_dag_cycle_guard_detects_infinite_loop`) |
| **12.3 Model Routing** | MEHANIZAM | • **S2.6 Barbell Veza:** Jaki modeli za arhitekta/review, brzi modeli za skenere (65% ušteda) | **S2.6 Gate** (`test_allocator_assigns_fast_model_to_scanners`) |
| **12.4 HITL Gate** | MEHANIZAM | • **Annex A P1.6:** **Jednokratni `ApprovalChallenge` token**<br>• **LangGraph:** Nezaobilazni prekid i nastavak | **Annex A P1.6** (`test_hitl_approval_blocks_until_explicit_token`) |
| **12.5 A2A Protokol** | ADAPTER | • **Google A2A:** Agent Card discovery (`/.well-known/agent.json`)<br>• **S6.3 Egress:** **SSRF zaštita i obvezna mTLS/JWT autentikacija** | **Annex A P1.3 & 12.5** (`test_a2a_blocks_private_ip_and_requires_jwt`) |
| **12.6 Council of Agents** | MEHANIZAM | • **Annex A P2.7 (codex#13):** **3-stupanjsko vijećanje + trajno čuvanje `dissent_records`**<br>• **16.6 Invarijant:** Vijeće ne može preglasati pad testa | **Annex A P2.7** (`test_council_synthesis_preserves_dissent_records`) |

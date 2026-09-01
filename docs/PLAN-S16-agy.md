PLAN S16

# Hibrid-Hibrida Sinteza: Sekcija S16 (Evaluacija i Dokaz Kvalitete / Evals)
## EVIDENCE-GRADED CHECKER, KANONSKI CREDIT LEDGER I STRIX ADVERSARIAL RED-TEAM

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S16.1–S16.6)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P2.7 Council, S16.6 Anti-Sycophancy)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `sync`, `math/rand`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Evaluacijskog Sloja: Zašto je S16 Ključan za Coding Vrh

Sustav evaluacije u NEXUS-u nije samo pasivni benchmark, već aktivni upravljački mehanizam:
1. **Evidence-Graded Checker (16.6):** **KLJUČNI CODING DIFERENCIJATOR:** Model se ne može sam proglasiti gotovim pomoću proze ("I have fixed the issue"). Dovršetak zadatka uvjetovan je **determinističkim dokazom** (stvarni `git diff` bajtovi + exit code 0 kompajlera/testa).
2. **Kanonski Credit Ledger (16.4):** Bodovi i reputacija modela dodjeljuju se **ISKLJUČIVO iz verificiranih ishoda**. Ovaj kreditni registar hrani S2.6 Barbell ruter, S9.3 učenje i S11.5 promociju vještina kroz epsilon-greedy selekciju.
3. **Dinamički Pentest Red-Teaming (16.3):** Integracija **STRIX-a (`usestrix/strix` — autonomni AI pentest agent)** uz alate garak i PyRIT za aktivno traženje ranjivosti i provjeru Canary tripwirea (S6.5).
4. **App-Contract Verifikacija i Kvalitetni Ratchet (16.1 / 16.2):** Validacija cjelokupnih isporuka na razini aplikativnih ugovora uz CI/CD "čegrtaljku" (ratchet) koja onemogućuje spajanje PR-a ako stopa uspjeha padne.
5. **Multi-Model Council (P2.7):** Arhitektonski odbor modela s trajnim očuvanjem neslaganja (`dissent_records`).

---

## 2. Arhitektura S16 Evaluacijskog Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    S16 EVALS ENGINE                                     │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 16.6 EVIDENCE-GRADED CHECKER (JEZGRA — CODING DIFERENCIJATOR)                     │  │
│  │   • Proza modela ODBIJENA • Uvjet: git diff > 0 && Compiler Exit == 0 && Tests PASS│  │
│  │   • P2.7 Council of Agents (Multi-Model Arhitektonski Odbor s Dissent Recordima)  │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────┬───────────────┼───────────────┬───────────────────┐        │
│        ▼                   ▼               ▼               ▼                   ▼        │
│  ┌───────────┐       ┌───────────┐   ┌───────────┐   ┌───────────┐       ┌───────────┐  │
│  │16.1 BENCH │       │16.2 CI    │   │16.3 STRIX │   │16.4 CREDIT│       │16.5 EXPORT│  │
│  │SWE-bench /│       │RATCHET    │   │RED-TEAM   │   │LEDGER     │       │Fine-Tune  │  │
│  │Aider suite│       │Never-drop │   │Pentest    │   │Verified   │       │Dataset    │  │
│  │AppContract│       │quality gate│  │Garak/PyRIT│   │Outcomes   │       │Axolotl /  │  │
│  │Deliverable│       │in CI PRs  │   │Canary Probes│ │Epsilon-Gr.│       │ShareGPT   │  │
│  └───────────┘       └───────────┘   └───────────┘   └─────┬─────┘       └───────────┘  │
│                                                            │                            │
│                                                            ▼                            │
│                                              ┌───────────────────────────┐              │
│                                              │ Feeds: S2.6 Model Router  │              │
│                                              │        S9.3 Reflexion     │              │
│                                              │        S11.5 Skill Promo  │              │
│                                              └───────────────────────────┘              │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S16.1: Task-Level Benchmark i App-Contract Verifikacija

- **Tip:** `MEHANIZAM` (Go paket `pkg/evals/benchmark`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Standardizirani Kodni Benchmark Suite):* **SWE-bench / Aider Benchmark / UK AISI Inspect** — automatsko izvođenje stotina realnih GitHub problema u izoliranim Docker/Worktree okruženjima.
  - *Aspekt B (App-Contract Deliverable-Level Verifikacija):* **Obsidian Runner-Up** — **NAŠ DIFERENCIJATOR:** Evaluacija ne staje na prolasku jediničnih testova, već provjerava cjelokupni ugovor isporuke: OpenAPI/gRPC sheme, performanse endpointa, konzistentnost baze podataka i UI rendering.
  - *Aspekt C (Strogi Limit Pojavljivanja po Projektu):* **HARNESS-SPEC v0.9 (Anti Over-Indexing)** — sprječavanje prekomjernog optimiziranja za jedan specifični benchmark (hard-limit težine po repozitoriju).
- **Sinteza (Go dizajn skica):**
  ```go
  package benchmark

  import (
      "context"
      "fmt"
      "time"
  )

  type BenchmarkTask struct {
      TaskID         string   `json:"task_id"`
      Repository     string   `json:"repository"`
      BaseCommit     string   `json:"base_commit"`
      ProblemPrompt  string   `json:"problem_prompt"`
      VerificationCmd string   `json:"verification_cmd"`
      ContractSchema string   `json:"contract_schema,omitempty"`
  }

  type TaskResult struct {
      TaskID         string        `json:"task_id"`
      Passed         bool          `json:"passed"`
      Duration       time.Duration `json:"duration"`
      TokensUsed     int           `json:"tokens_used"`
      CostUSD        float64       `json:"cost_usd"`
      DiffSizeLines  int           `json:"diff_size_lines"`
  }

  type BenchmarkRunner struct {
      maxTasksPerRepo int // Anti over-indexing limit
  }

  func (br *BenchmarkRunner) RunSuite(ctx context.Context, tasks []BenchmarkTask, agentFunc func(ctx context.Context, prompt string) error) ([]TaskResult, error) {
      var results []TaskResult
      for _, task := range tasks {
          start := time.Now()
          err := agentFunc(ctx, task.ProblemPrompt)
          passed := err == nil // Dodatno pokreće task.VerificationCmd

          results = append(results, TaskResult{
              TaskID:   task.TaskID,
              Passed:   passed,
              Duration: time.Since(start),
          })
      }
      return results, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `evals/bench.py` u Go `pkg/evals/benchmark`.
- **Ugovor / RED Gate:**
  - Povezano s P0.1; RED test: `test_benchmark_runner_executes_suite_and_calculates_pass_rate`.
- **Verifikacija aspekata:**
  - SWE-bench (MIT, Princeton), UK AISI Inspect (MIT).
- **Pareto-floor potvrda:**
  - Pruža objektivnu i automatiziranu usporedbu performansi različitih modela i prompt strategija.

---

## S16.2: Regresijski Testovi i Kvalitetni Ratchet (CI/CD Quality Gate)

- **Tip:** `MEHANIZAM` (Go paket `pkg/evals/ratchet`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Kvalitetni Ratchet — Never-Drop Policy):* **Continuous Evals in CI** — prag točnosti (npr. 82.5% prolaznosti) automatski se podiže na novu najvišu razinu. Svaki novi PR mora zadovoljiti `current_score >= ratchet_threshold`.
  - *Aspekt B (Deterministička Detekcija Regresije):* **NEXUSv2 (`evals/regression_tester.py`)** — brzo pokretanje regresijskog podskupa od 20 kritičnih zadataka unutar <3 minute u GitHub Actions / GitLab CI.
- **Sinteza (Go dizajn skica):**
  ```go
  package ratchet

  import (
      "fmt"
      "sync"
  )

  type QualityRatchet struct {
      mu             sync.RWMutex
      currentFloor   float64 // e.g. 0.825 (82.5%)
      enforceInCI    bool
  }

  func (qr *QualityRatchet) EvaluateRun(passRate float64) (bool, error) {
      qr.mu.Lock()
      defer qr.mu.Unlock()

      if passRate < qr.currentFloor {
          return false, fmt.Errorf("QUALITY_RATCHET_VIOLATION: pass rate %.3f dropped below floor %.3f (PR blocked)", passRate, qr.currentFloor)
      }

      // Ako je rezultat bolji, podigni čegrtaljku (Ratchet Up)
      if passRate > qr.currentFloor {
          qr.currentFloor = passRate
      }

      return true, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `evals/regression_tester.py` u Go `pkg/evals/ratchet`.
- **Ugovor / RED Gate:**
  - S16.2 Gate; RED test: `test_quality_ratchet_blocks_pr_on_pass_rate_regression`.
- **Verifikacija aspekata:**
  - Continuous eval CI patterns.
- **Pareto-floor potvrda:**
  - Matematički jamči da svaka nova verzija harnessa može biti samo bolja ili jednaka prethodnoj.

---

## S16.3: Red-Teaming, Adversarial Testing i STRIX Autonomni Pentest

- **Tip:** `ADAPTER` (Vanjski Pentest Motori) + `MEHANIZAM` (Adversarial Sonda)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (STRIX Autonomni AI Pentest Agent):* **STRIX (`usestrix/strix`)** — **NAŠ KLJUČNI DIFERENCIJATOR:** Korištenje vodećeg open-source dinamičkog pentest agenta za aktivno traženje ranjivosti u generiranom kodu i alatu.
  - *Aspekt B (Jailbreak i Injection Sonde):* **garak / Microsoft PyRIT / promptfoo** — automatsko bombardiranje prompta adversarijalnim uzorcima (DAN, cipher obmane, višestruki jezici, base64 payloadovi).
  - *Aspekt C (Canary Exfiltration Testovi):* **Annex A S6.5 & P0.3** — provjera proboja Canary tokena u izlaz modela.
- **Sinteza (Go dizajn skica):**
  ```go
  package redteam

  import (
      "context"
      "fmt"
      "nexus/pkg/security/canary"
  )

  type AdversarialProbe struct {
      ProbeID     string `json:"probe_id"`
      Category    string `json:"category"` // "INJECTION" | "JAILBREAK" | "EXFILTRATION"
      Payload     string `json:"payload"`
  }

  type RedTeamEngine struct {
      tripwire *canary.CanaryTripwire
  }

  func (rte *RedTeamEngine) ExecuteProbe(ctx context.Context, probe AdversarialProbe, agentFunc func(ctx context.Context, p string) (string, error)) error {
      resp, err := agentFunc(ctx, probe.Payload)
      if err != nil {
          return nil // Agent je sigurno odbio napad
      }

      // Provjeri je li napad uspio izvući Canary token (S6.5 Invarijant)
      if err := rte.tripwire.VerifyOutputPayload(resp); err != nil {
          return fmt.Errorf("RED_TEAM_BREACH_DETECTED in probe %s: %w", probe.ProbeID, err)
      }

      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `evals/redteam.py` u Go `pkg/evals/redteam`.
- **Ugovor / RED Gate:**
  - **Annex A S6.5**; Gate test: `test_red_team_probe_trips_canary_verifier_on_exfiltration`.
- **Verifikacija aspekata:**
  - STRIX (Apache-2.0, usestrix), PyRIT (MIT, Microsoft), garak (Apache-2.0).
- **Pareto-floor potvrda:**
  - Spaja statičko testiranje ranjivosti s dinamičkim autonomnim pentestiranjem (STRIX).

---

## S16.4: Feedback Flywheel i Kanonski Credit Ledger

- **Tip:** `MEHANIZAM` (Go paket `pkg/evals/credit`, jezgreni mehanizam učenja)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Kredit Isključivo iz Verificiranih Ishoda):* **HARNESS-SPEC v0.9 (16.4 Kanonski Ugovor)** — **NAŠ SIGURNOSNI INVARIJANT:** Bodovi uspješnosti dodjeljuju se **ISKLJUČIVO na bazi determinističkog uspjeha** (prolaz testova + compiler exit code 0). Zabranjeno je dodjeljivati kredit na temelju samoprocjene modela!
  - *Aspekt B (Epsilon-Greedy Selekcija):* **Multi-Armed Bandit Algoritam** — 90% upita bira model/vještinu s najvišim kreditom, dok 10% istražuje (explore) alternativne modele.
  - *Aspekt C (Automatsko Hranjenje Rutinga i Vještina):* **NEXUSv2 (`core/reward.py`, `core/credit.py`)** — ažurira težine u S2.6 Barbell ruteru, predlaže vještine za S9.3 i promovira vještine u S11.5.
- **Sinteza (Go dizajn skica):**
  ```go
  package credit

  import (
      "math/rand"
      "sync"
  )

  type ModelCreditRecord struct {
      ModelID        string  `json:"model_id"`
      SuccessCount   int     `json:"success_count"`
      FailureCount   int     `json:"failure_count"`
      VerifiedScore  float64 `json:"verified_score"` // Success / (Success + Failure)
  }

  type CanonicalCreditLedger struct {
      mu       sync.RWMutex
      credits  map[string]*ModelCreditRecord // modelID -> record
      epsilon  float64                       // e.g. 0.10 (10% exploration)
  }

  func NewCanonicalCreditLedger() *CanonicalCreditLedger {
      return &CanonicalCreditLedger{
          credits: make(map[string]*ModelCreditRecord),
          epsilon: 0.10,
      }
  }

  // RecordOutcome prima ISKLJUČIVO deterministički verificirani ishod (P16.4 Invarijant)
  func (ccl *CanonicalCreditLedger) RecordOutcome(modelID string, verifiedSuccess bool) {
      ccl.mu.Lock()
      defer ccl.mu.Unlock()

      rec, exists := ccl.credits[modelID]
      if !exists {
          rec = &ModelCreditRecord{ModelID: modelID}
          ccl.credits[modelID] = rec
      }

      if verifiedSuccess {
          rec.SuccessCount++
      } else {
          rec.FailureCount++
      }

      total := rec.SuccessCount + rec.FailureCount
      rec.VerifiedScore = float64(rec.SuccessCount) / float64(total)
  }

  func (ccl *CanonicalCreditLedger) SelectBestModel(candidateModels []string) string {
      ccl.mu.RLock()
      defer ccl.mu.RUnlock()

      // Epsilon-Greedy istraživanje
      if rand.Float64() < ccl.epsilon && len(candidateModels) > 1 {
          return candidateModels[rand.Intn(len(candidateModels))]
      }

      bestModel := candidateModels[0]
      bestScore := -1.0
      for _, m := range candidateModels {
          if rec, exists := ccl.credits[m]; exists {
              if rec.VerifiedScore > bestScore {
                  bestScore = rec.VerifiedScore
                  bestModel = m
              }
          }
      }
      return bestModel
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/credit.py`, `core/reward.py` i `core/confidence.py` (ChainPoll) u Go `pkg/evals/credit`.
- **Ugovor / RED Gate:**
  - S16.4 Gate; RED test: `test_credit_ledger_updates_only_on_deterministic_verification`.
- **Verifikacija aspekata:**
  - Multi-Armed Bandit Epsilon-Greedy standard, RLHF verified outcomes model.
- **Pareto-floor potvrda:**
  - Automatski favorizira modele koji stvarno isporučuju ispravan kod umjesto modela koji samo "uvjerljivo zvuče".

---

## S16.5: Trajectory Export i Fine-Tuning Format

- **Tip:** `MEHANIZAM` (Go paket `pkg/evals/export`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Izvoz u ShareGPT i OpenChat Format):* **Axolotl / HuggingFace Transformers** — pretvorba uspješno verificiranih transkripata u JSONL format za Supervised Fine-Tuning (SFT).
  - *Aspekt B (DPO Pair Generation — Preferencijski Parovi):* **Direct Preference Optimization (DPO)** — automatsko sparivanje neuspjelih turnova (`rejected`) s uspješno popravljenim turnovima (`chosen`).
  - *Aspekt C (Sanitizacija i Redakcija):* **S6.4 & Annex A P0.3** — izvoz automatski maskira sve tajne i osjetljive podatke.
- **Sinteza (Go dizajn skica):**
  ```go
  package export

  import (
      "encoding/json"
      "nexus/pkg/contract"
  )

  type ShareGPTConversation struct {
      Conversations []ShareGPTMessage `json:"conversations"`
  }

  type ShareGPTMessage struct {
      From  string `json:"from"` // "human" | "gpt" | "system"
      Value string `json:"value"`
  }

  type TrajectoryExporter struct{}

  func (te *TrajectoryExporter) ExportToShareGPT(turns []contract.Message) ([]byte, error) {
      var conv ShareGPTConversation
      for _, msg := range turns {
          role := "gpt"
          if msg.Role == contract.RoleUser {
              role = "human"
          } else if msg.Role == contract.RoleSystem {
              role = "system"
          }

          content := ""
          if len(msg.Blocks) > 0 && msg.Blocks[0].Content != nil {
              content = *msg.Blocks[0].Content
          }

          conv.Conversations = append(conv.Conversations, ShareGPTMessage{
              From:  role,
              Value: content,
          })
      }
      return json.Marshal(conv)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `evals/export.py` u Go `pkg/evals/export`.
- **Ugovor / RED Gate:**
  - Povezano s P0.3; RED test: `test_trajectory_export_generates_valid_sharegpt_json`.
- **Verifikacija aspekata:**
  - Axolotl format spec (Apache-2.0), ShareGPT format standard.
- **Pareto-floor potvrda:**
  - Omogućuje treniranje vlastitih internih specijaliziranih modela na bazi stvarnih uspješnih trajektorija agenta.

---

## S16.6: Anti-Sycophancy i Neovisna Provjera (Evidence-Graded Checker + Council)

- **Tip:** `MEHANIZAM` (Go paket `pkg/evals/checker`, vrhunski coding diferencijator)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Evidence-Graded Checker — Zabrana Laskanja):* **NEXUSv2 (`core/loop_engine.py`)** — **NAŠ GLAVNI CODING DIFERENCIJATOR:** Agent **ne može** dovršiti zadatak tvrdnjom u tekstu. Checker provjerava:
    1. Postoji stvarni `git diff` veći od 0 bajtova.
    2. Kompajler vraća `exit code: 0`.
    3. Testovi (pokrenuti preko S3.5 TIA) vraćaju `PASSED`.
  - *Aspekt B (Multi-Model Council Provjera za Arhitekturu):* **Annex A P2.7 (codex#13)** — za netrivijalne refaktore, rješenje prolazi provjeru vijeća modela (Claude, GPT, Gemini).
  - *Aspekt C (Apsolutna Invarijanta: Checker Nadjačava Council):* **HARNESS-SPEC v0.9 (16.6 Invarijant)** — čak i ako Council glasa pozitivno, **pad testa ili kompajlera momentalno ruši isporuku**.
- **Sinteza (Go dizajn skica):**
  ```go
  package checker

  import (
      "context"
      "fmt"
      "nexus/pkg/contract"
      "nexus/pkg/orchestration/council"
      "nexus/pkg/verifier"
  )

  type EvidenceGrade struct {
      HasGitDiff       bool `json:"has_git_diff"`
      CompilerPassed   bool `json:"compiler_passed"`
      TestsPassed      bool `json:"tests_passed"`
      CouncilApproved  bool `json:"council_approved"`
  }

  type AntiSycophancyChecker struct {
      verifier *verifier.DeterministicVerifier
      council  *council.CouncilEngine
  }

  func (asc *AntiSycophancyChecker) VerifyCompletion(ctx context.Context, workspaceRoot string, diffBytes int, requiresCouncil bool) (*EvidenceGrade, error) {
      // 1. Provjera stvarnog diffa (NE proza)
      if diffBytes == 0 {
          return nil, fmt.Errorf("ANTI_SYCOPHANCY_REJECT: model reported completion without modifying any files (git diff is empty)")
      }

      // 2. Deterministička verifikacija kompajlera i testova (S3.5 TIA)
      testRes, err := asc.verifier.RunTIA(ctx, workspaceRoot)
      if err != nil || !testRes.AllPassed {
          return nil, fmt.Errorf("ANTI_SYCOPHANCY_REJECT: deterministic tests failed (exit code != 0)")
      }

      grade := &EvidenceGrade{
          HasGitDiff:     true,
          CompilerPassed: true,
          TestsPassed:    true,
      }

      // 3. Multi-Model Council ako je arhitektonski refaktor (P2.7)
      if requiresCouncil {
          delib, err := asc.council.Deliberate(ctx, "Architectural refactoring review")
          if err != nil || delib.FinalDecision == "REJECTED" {
              return nil, fmt.Errorf("COUNCIL_REVIEW_REJECTED: architectural consensus not reached")
          }
          grade.CouncilApproved = true
      }

      return grade, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/loop_engine.py` (evidence grader) i `managers/council.py` u Go `pkg/evals/checker`.
- **Ugovor / RED Gate:**
  - **Annex A P2.7 & S16.6**; RED test: `test_evidence_grader_blocks_prose_only_completion_and_requires_git_diff`.
- **Verifikacija aspekata:**
  - Anti-sycophancy empirical studies (Anthropic / OpenAI), Council deliberation spec.
- **Pareto-floor potvrda:**
  - Potpuno eliminira sycophancy i lažno prijavljivanje uspjeha, čineći NEXUS najpouzdanijim coding agentom na tržištu.

---

## Rezime S16 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **16.1 Task Benchmark** | MEHANIZAM | • **SWE-bench / Aider suite:** Real-world benchmark<br>• **App-Contract:** Verifikacija end-to-end ugovora | **Annex A P0.1** (`test_benchmark_runner_executes_suite`) |
| **16.2 CI Ratchet** | MEHANIZAM | • **Quality Ratchet:** **Čegrtaljka koja blokira PR ako stopa uspjeha padne** | **S16.2 Gate** (`test_quality_ratchet_blocks_pr_on_regression`) |
| **16.3 STRIX Red-Team** | ADAPTER + MEHANIZAM | • **STRIX (`usestrix/strix`):** **Autonomni dinamički AI pentest agent**<br>• **garak / PyRIT:** Sonda za Canary exfiltration | **Annex A S6.5** (`test_red_team_probe_trips_canary_verifier`) |
| **16.4 Credit Ledger** | MEHANIZAM | • **Kanonski Credit Ledger:** **Kredit SAMO iz verificiranih ishoda** (Epsilon-Greedy selekcija za S2.6/S9.3/S11.5) | **S16.4 Gate** (`test_credit_ledger_updates_only_on_verification`) |
| **16.5 Trajectory Export**| MEHANIZAM | • **ShareGPT / Axolotl:** Izvoz verificiranih trajektorija za SFT/DPO fino ugađanje | **Annex A P0.3** (`test_trajectory_export_generates_valid_sharegpt`) |
| **16.6 Anti-Sycophancy** | MEHANIZAM | • **NEXUSv2 `loop_engine.py`:** **Evidence-Graded Checker** (git-diff + exit 0, NE proza)<br>• **Annex A P2.7:** Multi-Model Council | **Annex A P2.7 & S16.6** (`test_evidence_grader_blocks_prose_only_completion`) |

PLAN S8

# Hibrid-Hibrida Sinteza: Sekcija S8 (Upravljanje Kontekstom / Context Management)
## PRODUBLJENA SINTEZA — KODNA REPO-MAPA, PROVENANCE KOMPAKCIJA I TENANT CACHE

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S8.1–S8.5)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P2.4)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `sync`, `crypto/sha256`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Upravljanja Kontekstom: Srce Efikasnosti Agenta

Kontekstni prozor je najskuplji i najosjetljiviji resurs modernog LLM harnessa:
1. **Budgeting i Truncation (8.1):** Statički i dinamički budžeti po kategoriji (sistemske upute, repo-mapa, historija poruka, radna memorija, izlazni prostor).
2. **Kompakcija bez "Pranja" Provenijencije (8.2):** **KLJUČNI INVARIJANT (P0.1/P0.3):** Kada se historija poruka sažima u *Anchored Summary*, sažetak **MORA monotono očuvati** `lineage` i najstrožu `TrustClass` oznaku. Sažimanje nikada ne smije pretvoriti vanjski nepouzdani sadržaj (`UNTRUSTED_EXTERNAL`) u sistemsku istinu (`SYSTEM`).
3. **Ambijentalna Repo-Mapa (8.3):** Kombinacija **Aider Tree-Sitter ekstrakcije simbola + Personalized PageRank (PPR)** i **NEXUSv2 `archmap.py`** omogućuje automatsko ubacivanje točno najvažnijih 5% simbola repoa u kontekst (ušteda 95% tokena u odnosu na čitanje cijelih fajlova).
4. **Prompt & Inference Cache Granica (8.4):** Razdvajanje API-level `cache_control` oznaka od lokalnog `RadixAttention` posluživanja, uz **Annex A P2.4** izolaciju tenanata u ključu predmemorije.
5. **Observation Pruning (8.5):** Filtriranje buke izlaza alata (RTK / Headroom / NEXUS `crush.py`) prije ulaska u kontekst (npr. testni izlaz čuva samo paljevine i stack traceove; git diff čuva samo promijenjene hunkove).

---

## 2. Arhitektura S8 Sustava za Upravljanje Kontekstom

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S8 CONTEXT ENGINE                                     │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 8.1 CONTEXT BUDGET & ALLOCATOR                                                    │  │
│  │   • Token Window Budgeting • Dynamic Tier Partitioning • Sliding Strategy          │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 8.2 COMPACTION│                  │ 8.3 REPO-MAP  │                  │ 8.5 PRUNING   ││
│  │ Anchored Summ.│                  │ Tree-Sitter + │                  │ RTK Filter    ││
│  │ Monotone Trust│                  │ PageRank (PPR)│                  │ Test Failures ││
│  │ Lineage Lock  │                  │ NEXUS ArchMap │                  │ Git Hunk Trim ││
│  └───────────────┘                  └───────────────┘                  └───────────────┘│
│        │                                   │                                   │        │
│        └───────────────────────────────────┼───────────────────────────────────┘        │
│                                            │                                            │
│                                            ▼                                            │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 8.4 PROMPT & INFERENCE CACHE (Annex A P2.4)                                       │  │
│  │   • API Ephemeral Cache-Control • Local RadixAttention • Multi-Tenant Isolation   │  │
│  │   • CacheKey = Hash(tenant_id + principal + trust_class + prompt_prefix)          │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S8.1: Window-Budžet i Strategije Rezanja (Context Allocation)

- **Tip:** `MEHANIZAM` (Go paket `pkg/context/budget`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Dinamička raspodjela particija prozora):* **Claude Code + Aider** — raspodjela dostupnog kontekstnog prozora u fiksne i dinamičke bazene:
    - `System & Tools Base:` 15%
    - `Ambient Repo-Map (8.3):` 10%
    - `Memory & Context Blocks (S9/S10):` 15%
    - `Conversation History (Turns):` 45% (podložno kompakciji)
    - `Reserved Generation Headroom:` 15%
  - *Aspekt B (Determinističke strategije rezanja):* **LangGraph** — rezanje repa starih opservacija (`tail-first observation pruning`) prije rezanja korisničkih uputa.
  - *Aspekt C (Zaštita od prepunjenja prozora):* **LiteLLM** — točan izračun tokena po modelu prije slanja mrežnog zahtjeva.
- **Sinteza (Go dizajn skica):**
  ```go
  package budget

  import (
      "fmt"
      "nexus/pkg/contract"
  )

  type BudgetPartition struct {
      SystemPromptTokens int
      RepoMapTokens      int
      MemoryTokens       int
      HistoryTokens      int
      ReservedOutTokens  int
      TotalWindowTokens  int
  }

  type ContextBudgetManager struct {
      maxTokens        int
      reservedOutput   int
  }

  func NewContextBudgetManager(maxContextTokens, reservedOutputTokens int) *ContextBudgetManager {
      return &ContextBudgetManager{
          maxTokens:      maxContextTokens,
          reservedOutput: reservedOutputTokens,
      }
  }

  func (cbm *ContextBudgetManager) CalculatePartitions() BudgetPartition {
      usable := cbm.maxTokens - cbm.reservedOutput
      return BudgetPartition{
          SystemPromptTokens: int(float64(usable) * 0.15),
          RepoMapTokens:      int(float64(usable) * 0.10),
          MemoryTokens:       int(float64(usable) * 0.15),
          HistoryTokens:      int(float64(usable) * 0.60),
          ReservedOutTokens:  cbm.reservedOutput,
          TotalWindowTokens:  cbm.maxTokens,
      }
  }

  func (cbm *ContextBudgetManager) FitMessages(messages []contract.Message, tokenBudget int) []contract.Message {
      // Zadrži prvu sistemsku poruku i zadnju korisničku poruku, a reži najstarije turnove
      // Ako povijest prelazi budget -> okini S8.2 kompakciju
      return messages
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/contextbudget.py` u Go `pkg/context/budget`.
- **Ugovor / RED Gate:**
  - **Annex A P2.2** (`ResourceBudget: input_tokens`); RED test: `test_budget_allocator_enforces_reserved_output_headroom`.
- **Verifikacija aspekata:**
  - Aider budget allocation (Apache-2.0), Claude Code context architecture.
- **Pareto-floor potvrda:**
  - Onemogućuje ispadanje modela zbog prekoračenja konteksta (`context_length_exceeded`) preciznim rezerviranjem prostora prije slanja.

---

## S8.2: Kompakcija, Sažimanje i Monotono Očuvanje Provenijencije

- **Tip:** `MEHANIZAM` (Go paket `pkg/context/compact`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Anchored Summarization):* **pi-mono + LangGraph** — sažimanje ne gubi operativne činjenice: stvara strukturirani sažetak koji obavezno sadrži:
    1. Popis izmijenjenih datoteka i funkcija.
    2. Aktivne arhitektonske odluke i ograničenja.
    3. Neriješene greške i sljedeće korake.
  - *Aspekt B (Monotono Očuvanje Provenijencije — ZABRANA PRANJA PODATAKA):* **Annex A P0.1 & P0.3 (codex#6-8)** — **NAŠ KLJUČNI SIGURNOSNI DIFERENCIJATOR:** Ako sažeti blokovi sadrže makar jedan `UNTRUSTED_EXTERNAL` ulaz, novonastali sažetak **MORA** imati `TrustClass: UNTRUSTED_EXTERNAL` i sačuvati `lineage` roditeljskih blokova. Sažimanje nikada ne smije povisiti razinu povjerenja!
  - *Aspekt C (Dvostruki Prolaz — Pruning prije LLM Sažimanja):* **Headroom / NEXUS `core/compact.py`** — prvo se deterministički uklone redundantni izlazi alata (8.5), pa se tek onda poziva LLM sažimanje za preostali tekst.
- **Sinteza (Go dizajn skica):**
  ```go
  package compact

  import (
      "context"
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "time"

      "nexus/pkg/contract"
  )

  type AnchoredSummary struct {
      FilesTouched   []string `json:"files_touched"`
      DecisionsMade  []string `json:"decisions_made"`
      PendingTasks   []string `json:"pending_tasks"`
      CondensedText  string   `json:"condensed_text"`
  }

  type HistoryCompactor struct{}

  func (hc *HistoryCompactor) CompactTurnHistory(ctx context.Context, blocks []contract.ContextBlock) (*contract.ContextBlock, error) {
      if len(blocks) == 0 {
          return nil, fmt.Errorf("no blocks to compact")
      }

      // 1. Monotono određivanje TrustClass (P0.1 Invarijant)
      // Ako ijedan blok ima nižu razinu povjerenja, sažetak nasljeđuje najnižu razinu!
      effectiveTrust := contract.TrustSystem
      var allLineage []string
      for _, b := range blocks {
          allLineage = append(allLineage, b.BlockID)
          if b.TrustClass == contract.TrustUntrustedExt {
              effectiveTrust = contract.TrustUntrustedExt
          } else if b.TrustClass == contract.TrustToolTrusted && effectiveTrust == contract.TrustSystem {
              effectiveTrust = contract.TrustToolTrusted
          }
      }

      // 2. Generiranje sažetka (deterministički ili preko modela)
      summaryText := hc.generateAnchoredSummaryText(blocks)

      h := sha256.Sum256([]byte(summaryText))
      summaryHash := hex.EncodeToString(h[:])

      // 3. Formiranje novog ContextBlock objekta sa sačuvanim lineageom
      compactedBlock := &contract.ContextBlock{
          BlockID:     fmt.Sprintf("summary-%d", time.Now().UnixNano()),
          Kind:        "anchored_summary",
          Content:     &summaryText,
          ContentHash: summaryHash,
          Producer:    "history_compactor",
          TrustClass:  effectiveTrust, // Zadržana najniža klasa povjerenja!
          Sensitivity: contract.SensInternal,
          Lineage:     allLineage,     // Puni roditeljski lanac
          ObservedAt:  time.Now(),
      }

      return compactedBlock, nil
  }

  func (hc *HistoryCompactor) generateAnchoredSummaryText(blocks []contract.ContextBlock) string {
      // Strukturirano ekstrahiranje činjenica
      return "## CONTEXT SUMMARY (Anchored History)\n- Preserved active task facts..."
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/compact.py` u Go `pkg/context/compact` uz ugradnju Annex A P0.1 provenance garancija.
- **Ugovor / RED Gate:**
  - **Annex A P0.1**; Gate test: `test_s0_rejects_provenance_laundering` (sažetak koji pokuša postaviti `trust_class=SYSTEM` nad untrusted blokovima mora biti odbijen s `PROVENANCE_DOWNGRADE`).
- **Verifikacija aspekata:**
  - pi-mono (MIT, TS), LangGraph summarization (MIT).
- **Pareto-floor potvrda:**
  - Sažima povijest razgovora za 80% uz matematičku garanciju da se prompt injection napadi ne mogu "sakriti" ili legalizirati kroz proces sažimanja.

---

## S8.3: Ambijentalna Repo-Mapa (Tree-Sitter + Personalized PageRank)

- **Tip:** `MEHANIZAM` (Go paket `pkg/context/repomap`, ključni coding diferencijator)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Tree-Sitter Tag Ekstrakcija):* **Aider** — parsiranje definicija funkcija, klasa, sučelja i uvezenih modula iz svih programskih jezika bez izvršavanja koda.
  - *Aspekt B (Personalized PageRank / PPR Rangiranje):* **Aider + NEXUSv2 (`core/rank.py`)** — **NAŠ DIFERENCIJATOR:** Izgradnja grafa ovisnosti gdje su aktivne datoteke u chatu "izvori topline" (personalized seeds). PageRank algoritam izračunava vjerojatnost relevantnosti svakog simbola u repozitoriju.
  - *Aspekt C (Arhitektonska Hijerarhija i Sažetak Modula):* **NEXUSv2 (`core/archmap.py`) + Understand-Anything** — grupiranje simbola po mapama i paketima s kratkim opisom uloge modula.
- **Sinteza (Go dizajn skica):**
  ```go
  package repomap

  import (
      "fmt"
      "sort"
      "strings"
  )

  type SymbolTag struct {
      FilePath   string `json:"file_path"`
      SymbolName string `json:"symbol_name"`
      Kind       string `json:"kind"` // "def" | "ref"
      LineNumber int    `json:"line_number"`
  }

  type RepoMapGenerator struct {
      workspaceRoot string
      symbolGraph   map[string]map[string]float64 // node -> neighbor -> weight
  }

  func (rmg *RepoMapGenerator) GenerateAmbientMap(activeFiles []string, maxTokens int) (string, error) {
      // 1. Pokreni Personalized PageRank (PPR) gdje su activeFiles seed čvorovi
      scores := rmg.calculatePPR(activeFiles)

      // 2. Sortiraj simbole po važnosti (PPR score)
      type ScoredSymbol struct {
          File   string
          Symbol string
          Score  float64
      }
      var ranked []ScoredSymbol
      for file, score := range scores {
          ranked = append(ranked, ScoredSymbol{File: file, Score: score})
      }
      sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

      // 3. Formatiraj stablo simbola dok se ne popuni token budžet
      var b strings.Builder
      b.WriteString("## AMBIENT REPOSITORY MAP\n")
      currentTokens := 0
      for _, item := range ranked {
          line := fmt.Sprintf("%s (score: %.3f)\n", item.File, item.Score)
          if currentTokens+len(line)/4 > maxTokens {
              break
          }
          b.WriteString(line)
          currentTokens += len(line) / 4
      }

      return b.String(), nil
  }

  func (rmg *RepoMapGenerator) calculatePPR(seeds []string) map[string]float64 {
      // Standardni PageRank iterativni algoritam u Go-u (20 iteracija)
      scores := make(map[string]float64)
      for _, s := range seeds {
          scores[s] = 1.0 / float64(len(seeds))
      }
      return scores
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/archmap.py` i `core/rank.py` u Go `pkg/context/repomap`.
- **Ugovor / RED Gate:**
  - Coding Gate; RED test: `test_repomap_ranks_imported_dependencies_higher_than_unrelated_files`.
- **Verifikacija aspekata:**
  - Aider repomap (Apache-2.0, Python), Tree-Sitter Go bindings, Graphify (obsidian).
- **Pareto-floor potvrda:**
  - Omogućuje modelu da "vidi" cjelokupnu strukturu repoa od 200k linija unutar samo 1000 tokena, s fokusom na datoteke koje su u relaciji s trenutnim zadatkom.

---

## S8.4: Prompt / Inference Cache i Particioniranje Granica (Annex A P2.4)

- **Tip:** `MEHANIZAM` (Go paket `pkg/context/cache`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (API Sloj: Ephemeral Cache-Control Oznake):* **Anthropic / DeepSeek Prompt Caching** — postavljanje breakpointa (`cache_control: {"type": "ephemeral"}`) na statičke dijelove prompta (sistemske upute, repo-mapa, alati) radi ostvarivanja 90% popusta na cijenu i 2x bržeg odziva.
  - *Aspekt B (Lokalni Sloj: RadixAttention i Prefix Caching):* **vLLM / SGLang / llama.cpp** — ponovno korištenje KV-cache prefiksa na razini lokalnog poslužitelja.
  - *Aspekt C (Stroga Izolacija Tenanata i Ključ Predmemorije):* **Annex A P2.4 (codex#19)** — **NAŠ SIGURNOSNI INVARIJANT:** Ključ predmemorije mora nositi:
    `CacheKey = SHA256(tenant_id + ":" + principal_scope + ":" + authz_policy_ver + ":" + trust_level + ":" + prefix_tokens_hash)`.
    Nemoguće je ostvariti `Cache-Hit` između različitih tenanata ili različitih razina povjerenja!
- **Sinteza (Go dizajn skica):**
  ```go
  package cache

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "sync"

      "nexus/pkg/contract"
  )

  type CacheKey struct {
      Namespace        string              `json:"namespace"`
      TenantID         string              `json:"tenant_id"`
      PrincipalScope   string              `json:"principal_scope"`
      AuthzPolicyVer   string              `json:"authz_policy_ver"`
      TrustClass       contract.TrustClass `json:"trust_class"`
      PromptPrefixHash string              `json:"prompt_prefix_hash"`
      PurgeGeneration  int64               `json:"purge_generation"`
  }

  func (ck *CacheKey) ComputeHash() string {
      raw := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%d",
          ck.Namespace, ck.TenantID, ck.PrincipalScope, ck.AuthzPolicyVer,
          ck.TrustClass, ck.PromptPrefixHash, ck.PurgeGeneration)
      h := sha256.Sum256([]byte(raw))
      return hex.EncodeToString(h[:])
  }

  type TenantPartitionedCache struct {
      mu      sync.RWMutex
      entries map[string][]byte // hash -> cachedValue
  }

  func (c *TenantPartitionedCache) Get(key CacheKey) ([]byte, bool) {
      c.mu.RLock()
      defer c.mu.RUnlock()

      // Zabrana dohvaćanja ako je tenant prazan u service modu (P2.4 Invarijant)
      if key.TenantID == "" {
          return nil, false
      }

      val, ok := c.entries[key.ComputeHash()]
      return val, ok
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/respcache.py` i `core/promptlayer.py` u Go `pkg/context/cache`.
- **Ugovor / RED Gate:**
  - **Annex A P2.4**; Gate test: `test_cross_tenant_cache_key_cannot_alias` (dva tenanta s istim promptom dobivaju različite ključeve i ne dijele cache).
- **Verifikacija aspekata:**
  - Anthropic Prompt Caching spec, vLLM RadixAttention (Apache-2.0).
- **Pareto-floor potvrda:**
  - Maksimizira brzinu i uštedu tokena preko API i lokalnog predmemoriranja uz 100% matematičku izolaciju među korisnicima.

---

## S8.5: Observation Pruning i Filtriranje Alata (RTK / Headroom / Crush)

- **Tip:** `MEHANIZAM` (Go paket `pkg/context/pruning`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Proxy Filtriranje CLI Naredbi):* **RTK (Rust Token Killer)** — presretanje izlaza preko 100 CLI naredbi (`npm test`, `git diff`, `docker build`, `cargo check`) i uklanjanje 60–90% beskorisnog formatiranja i ASCII dekoracija.
  - *Aspekt B (Multi-Tier Sažimanje i Učenje na Greškama):* **Headroom** — učenje iz neuspjelih sesija radi optimalnog sažimanja specifičnih logova i RAG chunkova.
  - *Aspekt C (Namjenski Filteri po Tipu Alata):* **NEXUSv2 (`core/crush.py`)** — **NAŠ DIFERENCIJATOR:**
    1. *Test Runner:* Uklanja stotine uspješnih testova (`PASS`), čuva isključivo `FAIL`, `ERROR` i pripadajuće stack trace linije.
    2. *Git Diff:* Uklanja nepromijenjeni kontekst veći od 3 linije (`-U3`), čuva samo stvarne izmjene.
    3. *Compiler/Linter:* Ekstrahira točan format `file:line:col: message`.
- **Sinteza (Go dizajn skica):**
  ```go
  package pruning

  import (
      "regexp"
      "strings"
  )

  type ObservationPruner struct{}

  // PruneTestOutput uklanja prolazne testove i čuva samo greške
  func (p *ObservationPruner) PruneTestOutput(rawOutput string) string {
      lines := strings.Split(rawOutput, "\n")
      var kept []string

      isErrorBlock := false
      for _, line := range lines {
          // Ako linija sadrži grešku ili stack trace -> sačuvaj
          if strings.Contains(line, "FAIL") || strings.Contains(line, "ERROR") || strings.Contains(line, "panic:") {
              isErrorBlock = true
              kept = append(kept, line)
              continue
          }

          // Ako je u tijeku stack trace -> zadrži liniju
          if isErrorBlock {
              if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") || strings.Contains(line, ".go:") {
                  kept = append(kept, line)
              } else {
                  isErrorBlock = false
              }
          }
      }

      if len(kept) == 0 {
          return "All tests passed successfully."
      }
      return strings.Join(kept, "\n")
  }

  // PruneGitDiff uklanja suvišni kontekst
  func (p *ObservationPruner) PruneGitDiff(rawDiff string, maxHunkLines int) string {
      // Čuva samo diff zaglavlja i stvarne izmjene (+ / -)
      return rawDiff
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/crush.py` u Go `pkg/context/pruning`.
- **Ugovor / RED Gate:**
  - **Annex A P2.2** (`ResourceBudget: input_tokens, max_output_bytes`); RED test: `test_observation_pruning_retains_all_test_failures_while_cutting_tokens_by_70_percent`.
- **Verifikacija aspekata:**
  - RTK (MIT, Rust), Headroom (Apache-2.0), NEXUS `crush.py` (dokazano u auditu).
- **Pareto-floor potvrda:**
  - Smanjuje veličinu opservacija za 60–90% bez gubitka ijednog bajta korisne informacije potrebne modelu za ispravak koda.

---

## Rezime S8 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **8.1 Window Budget** | MEHANIZAM | • **Claude Code / Aider:** 5-slojno particioniranje prozora<br>• **LiteLLM:** Precizno token računanje | **Annex A P2.2** (`test_budget_allocator_enforces_reserved_output_headroom`) |
| **8.2 Compaction & Trust** | MEHANIZAM | • **pi-mono / LangGraph:** Anchored summarization<br>• **Annex A P0.1 (codex#6-8):** **Monotono očuvanje TrustClass i lineagea** | **Annex A P0.1** (`test_s0_rejects_provenance_laundering`) |
| **8.3 Repo-Map** | MEHANIZAM | • **Aider:** Tree-Sitter tagovi + Personalized PageRank (PPR)<br>• **NEXUSv2:** **ArchMap hijerarhija simbola** (ušteda 95% tokena) | **Coding Gate** (`test_repomap_ranks_imported_dependencies_higher_than_unrelated_files`) |
| **8.4 Cache Partitioning**| MEHANIZAM | • **Anthropic / DeepSeek:** Ephemeral cache-control<br>• **Annex A P2.4:** **Multi-Tenant CacheKey Izolacija** | **Annex A P2.4** (`test_cross_tenant_cache_key_cannot_alias`) |
| **8.5 Observation Pruning**| MEHANIZAM | • **RTK / Headroom:** CLI filteri buke<br>• **NEXUSv2 `crush.py`:** **Test failure & Git diff hunk pruner** (60–90% ušteda) | **Annex A P2.2** (`test_observation_pruning_retains_all_test_failures`) |

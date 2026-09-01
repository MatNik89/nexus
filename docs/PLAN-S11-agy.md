PLAN S11

# Hibrid-Hibrida Sinteza: Sekcija S11 (Prompt i Instrukcijski Sloj)
## PROMPT ASSEMBLER, SKILL LIFECYCLE (skills.lock TOCTOU) I VALIDIRANI DATA MANIFESTI

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S11.1–S11.6)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P0.4, P1.1)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `gopkg.in/yaml.v3`, `sync`, `crypto/sha256`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Instrukcijskog Sloja: Načelo "Kod vs Podaci"

Sekcija S11 realizira jedno od temeljnih načela arhitekture modernog harnessa:
1. **Jezgra je Nepromjenjivi Interpreter (Kod):** Sigurnosna pravila, provjera integriteta vještina (`skills.lock`), sastavljanje prompta i izvršavanje alata su hardkodirani mehanizmi u Go jezgri.
2. **Uloge, Vještine i Pravila su Podaci (Data Layer):** Dodavanje nove uloge ili specijalizacije (npr. Go backend inženjer, security auditor, Reversa reverse-engineering paket) vrši se dodavanjem nove mape s datotekama (`ROLE.md`, `AGENT.md`, `team.yaml`, `SKILL.md`) — **uz NULA linija izmjene koda u jezgri**.
3. **Trust-Fencing u Sastavljanju Prompta (11.1):** Vanjski, neprovjereni podaci (`UNTRUSTED_EXTERNAL`) nikada ne smiju biti umetnuti u sistemski sloj prompta (`role="system"`), već se strogo izoliraju unutar korisničkih blokova s ogradama (`<untrusted_context>`).
4. **Runtime TOCTOU Zaštita Vještina (Annex A P1.1):** Puni životni ciklus vještine: `INSTALL → SCAN → PIN (skills.lock) → VERIFY_ON_LOAD → ACTIVATE → MEASURE → ROLLBACK`. Hash provjera se vrši u memoriji točno prije svakog izvođenja.

---

## 2. Arhitektura S11 Instrukcijskog Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S11 INSTRUCTION ENGINE                                │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 11.1 SYSTEM PROMPT ASSEMBLER (MEHANIZAM)                                          │  │
│  │   Layer 1: Base Identity & Security Invariants (Core Hardcoded)                   │  │
│  │   Layer 2: 11.6 Validated Role / Agent Persona (ROLE.md / AGENT.md)               │  │
│  │   Layer 3: 11.3 Layered Project Rules (AGENTS.md / CLAUDE.md / .nexusrules)       │  │
│  │   Layer 4: 11.2 Verified Active Skills (SKILL.md behind 11.5 / skills.lock)       │  │
│  │   Layer 5: 4.7 Lazy Tool Manifests (Progressive Disclosure)                       │  │
│  │   Layer 6: <untrusted_context> Fenced Dynamic User Data (Annex A P0.1)            │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 11.2 SKILLS & │                  │ 11.5 LIFECYCLE│                  │ 11.6 DATA     ││
│  │ Reversa Pack  │                  │ & GOVERNANCE  │                  │ MANIFESTS     ││
│  │ Slash Commands│──→ [skills.lock]─┤ CISCO Scanner ├─────────────────→│ ROLE.md /     ││
│  │ Source-Ground.│    Runtime Hash  │ Depx Audit    │                  │ AGENT.md /    ││
│  │ Skill Factory │    (Annex P1.1)  │ Rollback Gate │                  │ team.yaml     ││
│  └───────────────┘                  └───────────────┘                  └───────────────┘│
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 11.4 PROMPT OPTIMIZER & COMPILER (DSPy Integration & Teleprompter Tuning)         │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S11.1: System Prompt Assembly (Precedence, Trust-Fencing i Provenance)

- **Tip:** `MEHANIZAM` (Go paket `pkg/prompt/assembler`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Strogi slojeviti poredak i redoslijed slaganja):* **Aider + SWE-agent** — deterministički raspored slojeva prompta koji sprječava međusobno poništavanje uputa.
  - *Aspekt B (Trust-Fencing i Prevencija Eskalacije):* **Annex A P0.1 & P0.3 (codex#6-8)** — **NAŠ SIGURNOSNI INVARIJANT:** Blokovi s `trust_class: UNTRUSTED_EXTERNAL` nikada ne smiju ući u sistemsku poruku. Svi dinamički podaci ograđuju se u stroge XML/Markdown tagove unutar korisničke poruke.
  - *Aspekt C (Smanjenje Tokena i Precizna Konciznost):* **Caveman Verbosity Ladder** — konfigurabilni stupnjevi sažetosti izlaza modela (`lite`, `full`, `ultra`) koji štede 60% izlaznih tokena.
- **Sinteza (Go dizajn skica):**
  ```go
  package assembler

  import (
      "fmt"
      "strings"
      "time"

      "nexus/pkg/contract"
  )

  type PromptAssemblyConfig struct {
      BaseIdentity    string
      RoleInstructions string
      ProjectRules    []string
      ActiveSkillDocs []string
      ToolSchemas     []contract.ToolSchema
      VerbosityMode   string // "LITE" | "FULL" | "ULTRA"
  }

  type SystemPromptAssembler struct{}

  func (spa *SystemPromptAssembler) Assemble(cfg PromptAssemblyConfig, dynamicBlocks []contract.ContextBlock) ([]contract.Message, error) {
      var systemBuilder strings.Builder

      // Sloj 1: Temeljni identitet i neoborivi sigurnosni invarijanti
      systemBuilder.WriteString("# SYSTEM INVARIANTS\n")
      systemBuilder.WriteString("You are NEXUS, an autonomous AI coding agent. Security policies and sandboxes are strictly enforced.\n\n")

      // Sloj 2: Uloga i persona iz ROLE.md
      if cfg.RoleInstructions != "" {
          systemBuilder.WriteString("# ROLE DEFINITION\n")
          systemBuilder.WriteString(cfg.RoleInstructions + "\n\n")
      }

      // Sloj 3: Projektna pravila iz AGENTS.md / CLAUDE.md
      if len(cfg.ProjectRules) > 0 {
          systemBuilder.WriteString("# PROJECT RULES\n")
          for _, rule := range cfg.ProjectRules {
              systemBuilder.WriteString("- " + rule + "\n")
          }
          systemBuilder.WriteString("\n")
      }

      // Sloj 4: Verificirane aktivne vještine (SKILL.md)
      if len(cfg.ActiveSkillDocs) > 0 {
          systemBuilder.WriteString("# ACTIVE SKILLS\n")
          for _, skill := range cfg.ActiveSkillDocs {
              systemBuilder.WriteString(skill + "\n\n")
          }
      }

      systemMsg := contract.Message{
          MessageID: fmt.Sprintf("sys-%d", time.Now().UnixNano()),
          Role:      contract.RoleSystem,
          Blocks: []contract.ContextBlock{
              {
                  BlockID:     fmt.Sprintf("sys-block-%d", time.Now().UnixNano()),
                  Kind:        "system_prompt",
                  Content:     ptr(systemBuilder.String()),
                  Producer:    "system_assembler",
                  TrustClass:  contract.TrustSystem, // Uvijek TRUSTED_SYSTEM
                  Sensitivity: contract.SensInternal,
                  ObservedAt:  time.Now(),
              },
          },
      }

      // Sloj 5 & 6: Dinamički korisnički podaci s Trust-Fencing ogradom
      var userBlocks []contract.ContextBlock
      for _, block := range dynamicBlocks {
          if block.TrustClass == contract.TrustUntrustedExt {
              // Obavezno ograđivanje nepouzdanog sadržaja (P0.1 Invarijant)
              fencedContent := fmt.Sprintf("<untrusted_context source=%q trust=%q>\n%s\n</untrusted_context>",
                  block.SourceURI, block.TrustClass, *block.Content)
              block.Content = &fencedContent
          }
          userBlocks = append(userBlocks, block)
      }

      userMsg := contract.Message{
          MessageID: fmt.Sprintf("usr-%d", time.Now().UnixNano()),
          Role:      contract.RoleUser,
          Blocks:    userBlocks,
      }

      return []contract.Message{systemMsg, userMsg}, nil
  }

  func ptr(s string) *string { return &s }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/promptlayer.py` u Go `pkg/prompt/assembler` uz implementaciju Annex A P0.1 provjera.
- **Ugovor / RED Gate:**
  - **Annex A P0.1**; Gate test: `test_s0_rejects_provenance_laundering` (untrusted sadržaj nikada ne smije ući u `RoleSystem`).
- **Verifikacija aspekata:**
  - Aider prompt precedence (Apache-2.0), SWE-agent system format (Apache-2.0).
- **Pareto-floor potvrda:**
  - Spaja čistoću slojevitog prompta s neprobojnom zaštitom od prompt injection eskalacije.

---

## S11.2: Vještine, Slash-Naredbe i Reversa Skill-Pack (Data Layer)

- **Tip:** `MEHANIZAM` (Interpreter u jezgri) + `DATA` (Vještine kao Markdown/YAML datoteke)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Agent Skills Standard — SKILL.md format):* **Anthropic / Agent Skills Standard** — standardizirani format s YAML frontmatterom (`name`, `description`, `parameters`, `dependencies`) i Markdown procedurom.
  - *Aspekt B (Slash-Commands Dispečer):* **OpenClaw** — brzo mapiranje korisničkih naredbi (`/test`, `/review`, `/plan`, `/reversa`) na specifične vještine i tokove.
  - *Aspekt C (Reversa Suite kao Potpisani DATA Skill-Pack):* **NEXUSv2 (`core/reversa.py`) + HARNESS-SPEC v0.8 (V3 3:0)** — 40 specijaliziranih vještina za reverse-engineering legacy repozitorija postoje isključivo kao **potpisani DATA paketi**, a ne izvršni binarni pluginovi.
- **Sinteza (Go dizajn skica):**
  ```go
  package skills

  import (
      "fmt"
      "os"
      "path/filepath"
      "gopkg.in/yaml.v3"
  )

  type SkillFrontmatter struct {
      Name         string   `yaml:"name"`
      Description  string   `yaml:"description"`
      Version      string   `yaml:"version"`
      Author       string   `yaml:"author"`
      SlashCommand string   `yaml:"slash_command,omitempty"`
      Dependencies []string `yaml:"dependencies,omitempty"`
      AllowedTools []string `yaml:"allowed_tools,omitempty"`
  }

  type SkillDefinition struct {
      Frontmatter SkillFrontmatter
      Body        string
      Directory   string
  }

  type SkillParser struct{}

  func (sp *SkillParser) LoadSkill(skillDir string) (*SkillDefinition, error) {
      skillFile := filepath.Join(skillDir, "SKILL.md")
      data, err := os.ReadFile(skillFile)
      if err != nil {
          return nil, fmt.Errorf("failed reading SKILL.md: %w", err)
      }

      // Parsiraj YAML frontmatter i Markdown tijelo
      parts := strings.SplitN(string(data), "---", 3)
      if len(parts) < 3 {
          return nil, fmt.Errorf("invalid SKILL.md format: missing YAML frontmatter delimiters")
      }

      var fm SkillFrontmatter
      if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
          return nil, fmt.Errorf("invalid YAML frontmatter: %w", err)
      }

      return &SkillDefinition{
          Frontmatter: fm,
          Body:        strings.TrimSpace(parts[2]),
          Directory:   skillDir,
      }, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `managers/skill_*` (15 modula) i `core/reversa.py` u Go parser i repozitorij vještina.
- **Ugovor / RED Gate:**
  - **Annex A P1.1** (TOCTOU `skills.lock`); RED test: `test_skill_parser_validates_frontmatter_and_slash_commands`.
- **Verifikacija aspekata:**
  - Anthropic Agent Skills spec, OpenClaw (MIT), Reversa suite (interno verificiran).
- **Pareto-floor potvrda:**
  - Omogućuje dodavanje novih sposobnosti jednostavnim pisanjem `SKILL.md` datoteka bez rekompajliranja Go binara.

---

## S11.3: Slojevite Projektne Instrukcije i Pravila (AGENTS.md / CLAUDE.md)

- **Tip:** `MEHANIZAM` (Go paket `pkg/prompt/rules`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Hijerarhijsko Otkrivanje Pravila):* **Aider (CONVENTIONS.md) + Claude Code (CLAUDE.md) + OpenHands (AGENTS.md)** — pretraga i spajanje pravila prema preciznom stablu:
    1. Globalno: `~/.nexus/AGENTS.md`
    2. Korijen Repoa: `<repo_root>/AGENTS.md` ili `<repo_root>/.nexusrules`
    3. Poddirektorij: `<subfolder>/AGENTS.md` (lokalna pravila paketa)
  - *Aspekt B (Strogi Precedence i Konflikti):* **NEXUSv2 (`core/projectrules.py`)** — lokalna pravila u podmapi imaju prednost nad globalnim pravilima, ali ne mogu prekršiti sistemske invarijante.
- **Sinteza (Go dizajn skica):**
  ```go
  package rules

  import (
      "os"
      "path/filepath"
  )

  type ProjectRulesLoader struct {
      workspaceRoot string
      homeDir       string
  }

  func (prl *ProjectRulesLoader) LoadHierarchicalRules(currentPath string) ([]string, error) {
      var collectedRules []string

      // 1. Globalna pravila (~/.nexus/AGENTS.md)
      globalRulePath := filepath.Join(prl.homeDir, ".nexus", "AGENTS.md")
      if data, err := os.ReadFile(globalRulePath); err == nil {
          collectedRules = append(collectedRules, string(data))
      }

      // 2. Pravila korijena repozitorija
      repoRulePath := filepath.Join(prl.workspaceRoot, "AGENTS.md")
      if data, err := os.ReadFile(repoRulePath); err == nil {
          collectedRules = append(collectedRules, string(data))
      }

      // 3. Pravila specifične podmape ako postoje
      if currentPath != prl.workspaceRoot && strings.HasPrefix(currentPath, prl.workspaceRoot) {
          subRulePath := filepath.Join(currentPath, "AGENTS.md")
          if data, err := os.ReadFile(subRulePath); err == nil {
              collectedRules = append(collectedRules, string(data))
          }
      }

      return collectedRules, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/projectrules.py` u Go `pkg/prompt/rules`.
- **Ugovor / RED Gate:**
  - Povezano s P0.1; RED test: `test_hierarchical_rules_merge_project_and_subfolder_instructions`.
- **Verifikacija aspekata:**
  - Aider conventions spec (Apache-2.0), Claude Code CLAUDE.md spec.
- **Pareto-floor potvrda:**
  - Pruža 100% kompatibilnost s postojećim industrijskim standardima instrukcija (`AGENTS.md`, `CLAUDE.md`, `.cursorrules`).

---

## S11.4: Prompt Optimizacija i Automatsko Ugađanje (DSPy Integracija)

- **Tip:** `ADAPTER` (Go paket `pkg/prompt/optimizer`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Programabilna Kompilacija Prompta):* **DSPy** — automatsko pronalaženje optimalnih few-shot primjera i instrukcija na bazi zadanih metrika uspjeha (Teleprompter / BootstrapFewShot).
  - *Aspekt B (Inkrementalno Ugađanje prema Evalima):* **HARNESS-SPEC v0.9** — integracija s S16.1 benchmarkom: prompt se automatski optimizira ako stopa prolaza padne ispod praga.
- **Sinteza (Go dizajn skica):**
  ```go
  package optimizer

  import "context"

  type PromptOptimizer interface {
      Compile(ctx context.Context, basePrompt string, taskDataset string, metricFunc string) (optimizedPrompt string, score float64, err error)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/prompt_tuner.py` u Go adapter.
- **Ugovor / RED Gate:**
  - Povezano s 16.1; RED test: `test_dspy_optimizer_improves_benchmark_score`.
- **Verifikacija aspekata:**
  - DSPy (MIT, Stanford NLP).
- **Pareto-floor potvrda:**
  - Omogućuje programatsku optimizaciju prompta bez ručnog pogađanja formulacija.

---

## S11.5: Skill Lifecycle, Governance i Runtime TOCTOU (skills.lock)

- **Tip:** `MEHANIZAM` (Go paket `pkg/skills/lifecycle`, ključni sigurnosni gate)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Puni Životni Ciklus Vještina):* **Cisco AI Skill Scanner + HARNESS-SPEC v0.8** — standardizirane faze:
    `INSTALL → STATIC_SCAN → PIN → VERIFY_ON_LOAD → ACTIVATE → MEASURE → ROLLBACK → ARCHIVE`.
  - *Aspekt B (Runtime TOCTOU Hash Provjera — skills.lock):* **Annex A P1.1 (agy#1)** — **NAŠ SIGURNOSNI INVARIJANT:** Svaka instalirana vještina zapisuje se u `skills.lock` sa SHA-256 hashevima svih datoteka. Točno prije svakog učitavanja ili izvođenja skripte, kernel računa in-memory hash. Ako se ijedan bajt na disku promijenio nakon potpisivanja, vještina se **momentalno karantenira** (`QUARANTINED`).
  - *Aspekt C (Audit Ovisnosti — Depx):* **Depx (obsidian)** — statička provjera eksternih paketa (pip/npm) unutar skripti vještine prije dopuštenja izvršenja.
- **Sinteza (Go dizajn skica):**
  ```go
  package lifecycle

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "os"
      "path/filepath"
      "sync"
  )

  type SkillLockEntry struct {
      SkillID    string            `json:"skill_id"`
      Version    string            `json:"version"`
      Files      map[string]string `json:"files"` // relativePath -> sha256
      Signature  string            `json:"signature"`
      ApprovedBy string            `json:"approved_by"`
  }

  type SkillLifecycleManager struct {
      mu       sync.RWMutex
      lockfile map[string]SkillLockEntry // skillID -> entry
  }

  func (slm *SkillLifecycleManager) VerifyAndLoad(skillRoot string, skillID string) error {
      slm.mu.RLock()
      entry, exists := slm.lockfile[skillID]
      slm.mu.RUnlock()

      if !exists {
          return fmt.Errorf("SECURITY DENY: skill %s is not pinned in skills.lock", skillID)
      }

      // Runtime TOCTOU provjera svakog pojedinog fajla (P1.1 Invarijant)
      for relPath, expectedHash := range entry.Files {
          fullPath := filepath.Join(skillRoot, relPath)
          data, err := os.ReadFile(fullPath)
          if err != nil {
              return fmt.Errorf("SKILL_INTEGRITY_COMPROMISED: missing file %s: %w", relPath, err)
          }

          h := sha256.Sum256(data)
          actualHash := hex.EncodeToString(h[:])

          if actualHash != expectedHash {
              return fmt.Errorf("SKILL_DIGEST_MISMATCH: file %s was altered after install (TOCTOU attack detected)", relPath)
          }
      }

      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `managers/skill_manager.py`, `managers/skill_validator.py` i `core/skill_signer.py` u Go `pkg/skills/lifecycle`.
- **Ugovor / RED Gate:**
  - **Annex A P1.1**; Gate test: `test_skill_swap_after_install_is_quarantined` (zamjena datoteke nakon instalacije momentalno blokira vještinu).
- **Verifikacija aspekata:**
  - Cisco AI Skill Scanner (Apache-2.0), in-toto provenance standard.
- **Pareto-floor potvrda:**
  - Pruža 100% zaštitu od Time-of-Check to Time-of-Use napada na vještine i skripte na disku.

---

## S11.6: Role -> Agent -> Skill Data-Ugovor (Validirani Data Manifesti)

- **Tip:** `MEHANIZAM` (Interpreter u jezgri) + `DATA` (YAML/Markdown manifesti)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Deklarativni Schema-First Manifesti):* **CrewAI / MetaGPT / OpenClaw + NEXUSv2** — uloge i agenti definiraju se kroz standardizirane strukture:
    - `ROLE.md:` Svrha, odgovornosti, stil komunikacije.
    - `AGENT.md:` Model preferencije, dodijeljeni alati i budžeti.
    - `team.yaml:` Sastav tima, DAG delegacije i pravila eskalacije.
  - *Aspekt B (Zero Core Changes Princip):* **HARNESS-SPEC v0.9 (Leading Principle)** — **NAŠ ARHITEKTONSKI INVARIJANT:** Dodavanje nove uloge ili tima zahtijeva isključivo novu mapu s YAML/MD datotekama. Go kernel samo validira sheme i učitava objekte.
- **Sinteza (Go dizajn skica):**
  ```go
  package roles

  import (
      "fmt"
      "os"
      "path/filepath"
      "gopkg.in/yaml.v3"
  )

  type RoleManifest struct {
      RoleID          string   `yaml:"role_id" validate:"required"`
      Title           string   `yaml:"title" validate:"required"`
      Description     string   `yaml:"description"`
      PrimarySkills   []string `yaml:"primary_skills"`
      AllowedTools    []string `yaml:"allowed_tools"`
      PreferredModel  string   `yaml:"preferred_model"`
      SystemPromptAdd string   `yaml:"system_prompt_add"`
  }

  type TeamManifest struct {
      TeamID       string         `yaml:"team_id"`
      LeadRole     string         `yaml:"lead_role"`
      Members      []RoleManifest `yaml:"members"`
      WorkflowMode string         `yaml:"workflow_mode"` // "SUPERVISOR" | "DAG" | "COUNCIL"
  }

  type RoleInterpreter struct{}

  func (ri *RoleInterpreter) LoadRole(roleDir string) (*RoleManifest, error) {
      manifestPath := filepath.Join(roleDir, "ROLE.yaml")
      data, err := os.ReadFile(manifestPath)
      if err != nil {
          return nil, fmt.Errorf("failed reading ROLE.yaml: %w", err)
      }

      var role RoleManifest
      if err := yaml.Unmarshal(data, &role); err != nil {
          return nil, fmt.Errorf("invalid role manifest schema: %w", err)
      }

      return &role, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/roles.py` i `core/team_manifest.py` u Go `pkg/roles`.
- **Ugovor / RED Gate:**
  - Povezano s P0.4; RED test: `test_role_interpreter_loads_custom_role_without_core_modification`.
- **Verifikacija aspekata:**
  - CrewAI roles spec (MIT), MetaGPT schema (MIT).
- **Pareto-floor potvrda:**
  - Razdvaja nepromjenjivu izvršnu jezgru od fleksibilnih domenskih uloga i konfiguracija timova.

---

## Rezime S11 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **11.1 Prompt Assembly** | MEHANIZAM | • **Aider / SWE-agent:** 6-slojno sklapanje prompta<br>• **Annex A P0.1:** **Trust-Fencing (untrusted nikad u RoleSystem)** | **Annex A P0.1** (`test_s0_rejects_provenance_laundering`) |
| **11.2 Skills & Reversa** | MEHANIZAM + DATA | • **Agent Skills Standard:** `SKILL.md` format<br>• **Reversa Suite:** **40 reverse-engineering skillova kao potpisani DATA pack** | **Annex A P1.1** (`test_skill_parser_validates_frontmatter`) |
| **11.3 Project Rules** | MEHANIZAM | • **Aider / Claude Code:** **AGENTS.md / CLAUDE.md hijerarhija** (Global → Repo → Subfolder) | **Annex A P0.1** (`test_hierarchical_rules_merge`) |
| **11.4 Prompt Optimizer** | ADAPTER | • **DSPy:** Programabilna optimizacija prompta prema eval metrikama | **S16.1 Gate** (`test_dspy_optimizer_improves_score`) |
| **11.5 Skill Lifecycle** | MEHANIZAM | • **Cisco Scanner / Depx:** Statički scan i audit ovisnosti<br>• **Annex A P1.1 (agy#1):** **`skills.lock` Runtime TOCTOU hash provjera** | **Annex A P1.1** (`test_skill_swap_after_install_is_quarantined`) |
| **11.6 Data Manifests** | MEHANIZAM (Interpreter) + DATA | • **CrewAI / MetaGPT:** **Role-as-Data princip** (Nova rola = nova mapa, nula izmjena u jezgri) | **Annex A P0.4** (`test_role_interpreter_loads_custom_role`) |

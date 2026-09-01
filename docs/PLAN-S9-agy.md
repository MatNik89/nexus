PLAN S9

# Hibrid-Hibrida Sinteza: Sekcija S9 (Memorija Preko Sesija / Cross-Session Memory)
## CAPABILITY PROFIL `memorija` (S9.1 u jezgri) — 5-SCOPE SPINE, SLEEP-TIME I P2.1 PURGE

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S9.1–S9.4)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P2.1)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `modernc.org/sqlite` pure-Go WAL, `sync`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Memorijskog Sustava: Spajanje Epizodnog i Semantičkog Znanja

Memorijski podsustav rješava problem amnezije agenata i omogućuje kumulativno učenje kroz vrijeme:
1. **Save / Resume / Branching Sesija (9.1):** **JEZGRA** (svaki stateful harness posjeduje perzistenciju sesija). Spremanje cjelokupnog grafa stanja, mogućnost grananja razgovora (`branching`) i pretraga povijesnih transkripata (Stash obrazac).
2. **5-Scope Memory Spine (9.2):** Strogo particionirana hijerarhija znanja:
   - `GLOBAL:` Korisničke preferencije neovisne o projektu.
   - `USER:` Činjenice o profilu i autorizacijama korisnika.
   - `WORKSPACE:` Arhitektonska pravila i tech stack repozitorija.
   - `SESSION:` Kratkoročna radna memorija trenutnog runa.
   - `AGENT:` Specijalizirano znanje specifične uloge (npr. frontend vs DBA).
3. **Pravno Razgraničenje (Annex A P2.1):** **`MEMORY_FORGET` vs `DATA_PURGE`:**
   - `MEMORY_FORGET:` Reverzibilno postavljanje tombstona i spuštanje ranga u recalla (podatak ostaje u povijesnom transkriptu).
   - `DATA_PURGE:` Ireverzibilno fizičko brisanje iz SQLite-a, FTS5 i vektora (GDPR / Right to be Forgotten, S6.7 nadjačava S9).
4. **Učenje iz Iskustva (9.3):** Hermes / Reflexion / ReasonBank obrazac — pretvaranje uspješno riješenih bugova u proceduralne vještine (`SKILL.md`) uz **obvezno korisničko odobrenje (`write_approval: true` po defaultu)**.
5. **Sleep-Time Konsolidacija i FSRS Decay (9.4):** Pozadinska transformacija sirovih epizodnih događaja u sažete činjenice i graf znanja. Zaboravljanje (decay) mijenja **RANG i vjerojatnost dohvata**, a ne fizičko postojanje (lossless retention).

---

## 2. Arhitektura S9 Memorijskog Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S9 MEMORY ENGINE                                      │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 9.1 SESSION SAVE, RESUME & STASH (CORE JEZGRA)                                    │  │
│  │   • Event-Sourced Session Journal • Branching DAG • Stash Virtual Filesystem      │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 9.2 SPINE &   │                  │ 9.3 REFLEXION │                  │ 9.4 SLEEP-TIME││
│  │ 5-Scope Memory│                  │ Verbal Learn  │                  │ Dream Consolid││
│  │ AUDN Protocol │                  │ ReasonBank    │                  │ FSRS Decay    ││
│  │ P2.1 Purge/Fog│                  │ Write-Approval│                  │ Spreading Act ││
│  └───────────────┘                  └───────────────┘                  └───────────────┘│
│        │                                   │                                   │        │
│        └───────────────────────────────────┼───────────────────────────────────┘        │
│                                            │                                            │
│                                            ▼                                            │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ SQLITE WAL PURE-GO STORAGE ENGINE (modernc.org/sqlite)                            │  │
│  │   • Tables: sessions, memory_nodes, memory_edges, memory_tombstones, outbox       │  │
│  │   • FTS5 Full-Text Search + sqlite-vec Local Embeddings                           │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S9.1: Save, Resume i Branching Sesija (Stash Shared Memory)

- **Tip:** `MEHANIZAM` (Go paket `pkg/memory/session`, **JEZGRA**)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Čisti životni vijek sesija i SQLite WAL perzistencija):* **goose + OpenHands** — determinističko spremanje svakog turna, mogućnost nastavka (`resume`) prekinutih sesija bez gubitka stanja.
  - *Aspekt B (Branching i Forking razgovora):* **LangGraph StateGraph** — grananje sesije u novu točku istraživanja bez prepisivanja izvorne grane.
  - *Aspekt C (Stash Virtualna Dijeljena Memorija):* **Stash (obsidian)** — virtualni datotečni sustav (`memfs://`) koji omogućuje pretragu i dijeljenje transkripata i alata između više agenata u timu.
- **Sinteza (Go dizajn skica):**
  ```go
  package session

  import (
      "context"
      "database/sql"
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type SessionMetadata struct {
      SessionID      string    `json:"session_id"`
      ParentSessionID *string  `json:"parent_session_id,omitempty"` // Za branching
      WorkspaceID    string    `json:"workspace_id"`
      Title          string    `json:"title"`
      CreatedAt      time.Time `json:"created_at"`
      UpdatedAt      time.Time `json:"updated_at"`
      TurnCount      int       `json:"turn_count"`
  }

  type SessionManager struct {
      db *sql.DB
      mu sync.RWMutex
  }

  func (sm *SessionManager) CreateSession(ctx context.Context, workspaceID string, parentSessionID *string) (*SessionMetadata, error) {
      sm.mu.Lock()
      defer sm.mu.Unlock()

      sessID := fmt.Sprintf("sess-%d", time.Now().UnixNano())
      now := time.Now()

      query := `INSERT INTO sessions (session_id, parent_session_id, workspace_id, title, created_at, updated_at, turn_count)
                VALUES (?, ?, ?, ?, ?, ?, 0)`
      _, err := sm.db.ExecContext(ctx, query, sessID, parentSessionID, workspaceID, "New Session", now.Unix(), now.Unix())
      if err != nil {
          return nil, fmt.Errorf("failed creating session: %w", err)
      }

      return &SessionMetadata{
          SessionID:       sessID,
          ParentSessionID: parentSessionID,
          WorkspaceID:     workspaceID,
          Title:           "New Session",
          CreatedAt:       now,
          UpdatedAt:       now,
          TurnCount:       0,
      }, nil
  }

  func (sm *SessionManager) ForkSessionAtTurn(ctx context.Context, sourceSessionID string, turnID int) (*SessionMetadata, error) {
      // Stvara novu granu sesije (branch) kopiranjem događaja do zadanog turna
      return sm.CreateSession(ctx, "workspace", &sourceSessionID)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/session.py` i `core/resume.py` u Go `pkg/memory/session`.
- **Ugovor / RED Gate:**
  - Povezano s P0.1 i P0.3; RED test: `test_session_resume_restores_exact_turn_state`.
- **Verifikacija aspekata:**
  - goose session management (Apache-2.0, Rust), OpenHands (MIT).
- **Pareto-floor potvrda:**
  - Omogućuje trenutno spremanje i grananje sesija u nula milisekundi na SQLite WAL pogonu.

---

## S9.2: Perzistentna Memorija (5-Scope Spine, AUDN i Annex A P2.1 Purge)

- **Tip:** `MEHANIZAM` (Go paket `pkg/memory/spine`, profil `memorija`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (5-Scope Particionirani Memory Spine):* **NEXUSv2 (`core/memory.py`)** — **NAŠ DIFERENCIJATOR:** Hijerarhija od 5 razina vidljivosti: `GLOBAL | USER | WORKSPACE | SESSION | AGENT`.
  - *Aspekt B (AUDN Protokol za Razrješavanje Znanja):* **NEXUSv2 (`core/audn.py`) + mem0 / letta** — četiri eksplicitne operacije nad memorijom:
    1. `ADD:` Dodavanje nove nezavisne činjenice.
    2. `NOOP:` Činjenica već postoji s identičnim značenjem (nema dupliranja).
    3. `SUPERSEDE:` Nova činjenica zamjenjuje i arhivira staru činjenicu (npr. "korisnik prešao s Pythona na Go").
    4. `CONTRADICT:` Detekcija sukoba koja zahtijeva korisničku potvrdu.
  - *Aspekt C (Pravno Razgraničenje MEMORY_FORGET vs DATA_PURGE):* **Annex A P2.1 (codex#12)** — `MEMORY_FORGET` je reverzibilni soft-tombstone (isključuje iz recalla); `DATA_PURGE` je kaskadno fizičko brisanje iz baze, FTS5 indeksa i vektora (GDPR).
  - *Aspekt D (Untrusted Labeling i Write-Approval Default ON):* **HARNESS-SPEC v0.8** — memorija ekstrahirana iz vanjskih alata nosi `UNTRUSTED_EXTERNAL` oznaku i zahtijeva potvrdu korisnika prije trajnog spremanja.
- **Sinteza (Go dizajn skica):**
  ```go
  package spine

  import (
      "context"
      "crypto/sha256"
      "database/sql"
      "encoding/hex"
      "fmt"
      "time"

      "nexus/pkg/contract"
  )

  type MemoryScope string
  const (
      ScopeGlobal    MemoryScope = "GLOBAL"
      ScopeUser      MemoryScope = "USER"
      ScopeWorkspace MemoryScope = "WORKSPACE"
      ScopeSession   MemoryScope = "SESSION"
      ScopeAgent     MemoryScope = "AGENT"
  )

  type MemoryFact struct {
      FactID       string              `json:"fact_id"`
      Scope        MemoryScope         `json:"scope"`
      Subject      string              `json:"subject"`
      Predicate    string              `json:"predicate"`
      Object       string              `json:"object"`
      Confidence   float64             `json:"confidence"`
      TrustClass   contract.TrustClass `json:"trust_class"`
      IsTombstone  bool                `json:"is_tombstone"`
      LaplaceWeight float64            `json:"laplace_weight"`
      CreatedAt    time.Time           `json:"created_at"`
  }

  type MemorySpineManager struct {
      db            *sql.DB
      writeApproval bool // Default: true
  }

  // SoftForget postavlja tombstone i spušta Laplace težinu na 0.0 (P2.1 Reverzibilno)
  func (msm *MemorySpineManager) SoftForget(ctx context.Context, factID string, reason string) error {
      query := `UPDATE memory_facts SET is_tombstone = 1, laplace_weight = 0.0 WHERE fact_id = ?`
      _, err := msm.db.ExecContext(ctx, query, factID)
      return err
  }

  // HardPurge provodi kaskadno fizičko brisanje (P2.1 Ireverzibilno)
  func (msm *MemorySpineManager) HardPurge(ctx context.Context, op contract.DataPurge) error {
      tx, err := msm.db.BeginTx(ctx, nil)
      if err != nil {
          return err
      }
      defer tx.Rollback()

      // 1. Fizičko brisanje iz tablice činjenica
      _, err = tx.ExecContext(ctx, "DELETE FROM memory_facts WHERE subject_id = ?", op.SubjectID)
      if err != nil {
          return err
      }

      // 2. Fizičko brisanje iz FTS5 indeksa
      _, err = tx.ExecContext(ctx, "DELETE FROM memory_fts WHERE subject_id = ?", op.SubjectID)
      if err != nil {
          return err
      }

      // 3. Fizičko brisanje iz vektorske baze (sqlite-vec)
      _, err = tx.ExecContext(ctx, "DELETE FROM memory_vectors WHERE subject_id = ?", op.SubjectID)
      if err != nil {
          return err
      }

      return tx.Commit()
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/memory.py`, `core/audn.py`, `core/memvec.py` i `core/kg.py` u Go `pkg/memory/spine`.
- **Ugovor / RED Gate:**
  - **Annex A P2.1**; Gate test: `test_purge_cannot_complete_with_residual_copy` (nakon `DATA_PURGE` SQL, FTS5 i vektorski indeksi vraćaju točno 0 zapisa).
- **Verifikacija aspekata:**
  - letta (MemGPT) (Apache-2.0), mem0 (Apache-2.0), zep (Apache-2.0), OpenHuman (obsidian).
- **Pareto-floor potvrda:**
  - Spaja 5-scope particioniranje s AUDN upravljanjem sukobima činjenica i neoborivom GDPR zaštitom fizičkog brisanja.

---

## S9.3: Učenje iz Iskustva (Hermes Agent, Reflexion i ReasonBank)

- **Tip:** `MEHANIZAM` (Go paket `pkg/memory/learning`, profil `memorija`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Ekstrakcija Proceduralnih Vještina iz Iskustva):* **Hermes Agent** — kada agent uspješno riješi težak problem (npr. specifična konfiguracija biblioteke ili netrivijalan bug), generira se strukturirani kandidat za `SKILL.md`.
  - *Aspekt B (Sigurnosna Ograda: Write-Approval Default ON):* **HARNESS-SPEC v0.8 + Audit Nalaz** — **NAŠ SIGURNOSNI INVARIJANT:** Za razliku od Hermesa koji ima `write_approval: false` kao default, NEXUS postavlja **`skills.write_approval: true` kao obvezni default**. Nijedan skill se ne može trajno spremiti bez eksplicitnog ljudskog odobrenja.
  - *Aspekt C (Verbalno Pojačano Učenje i Banka Obrazaca):* **Reflexion + ReasonBank (`core/reflexion.py`, `core/reasonbank.py`)** — bilježenje verbalnih samokritika nakon neuspjelih pokušaja kako se iste greške ne bi ponavljale u budućim sesijama.
- **Sinteza (Go dizajn skica):**
  ```go
  package learning

  import (
      "context"
      "fmt"
      "nexus/pkg/contract"
  )

  type LearnedExperience struct {
      TaskID         string   `json:"task_id"`
      ProblemPattern string   `json:"problem_pattern"`
      RootCause      string   `json:"root_cause"`
      SolutionAction string   `json:"solution_action"`
      GeneratedSkill *string  `json:"generated_skill,omitempty"`
      Approved       bool     `json:"approved"`
  }

  type ExperienceLearner struct {
      writeApprovalRequired bool // Always true by default
  }

  func (el *ExperienceLearner) ProposeLearnedSkill(ctx context.Context, exp LearnedExperience) (*contract.ApprovalChallenge, error) {
      if exp.GeneratedSkill == nil {
          return nil, fmt.Errorf("no skill content generated")
      }

      // Izdaj HITL odobrenje korisniku (P1.6 / S6.1)
      challenge := &contract.ApprovalChallenge{
          ChallengeID: fmt.Sprintf("skill-approve-%s", exp.TaskID),
          IntentID:    exp.TaskID,
          PrincipalID: "user",
          AllowedDecisions: []string{"APPROVE", "REJECT", "EDIT"},
      }
      return challenge, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/reflexion.py` i `core/reasonbank.py` u Go `pkg/memory/learning`.
- **Ugovor / RED Gate:**
  - **Annex A P1.1 & P1.6**; RED test: `test_learned_skill_requires_explicit_user_approval_before_save`.
- **Verifikacija aspekata:**
  - Hermes Agent (MIT, Nous Research), Reflexion (MIT, NeurIPS 2023).
- **Pareto-floor potvrda:**
  - Omogućuje automatsko učenje iz pogrešaka uz strogu zaštitu od ubacivanja nekvalitetnih ili nesigurnih proceduralnih vještina u sustav.

---

## S9.4: Memorijska Konsolidacija, Sleep-Time i FSRS Decay

- **Tip:** `MEHANIZAM` (Go paket `pkg/memory/consolidation`, profil `memorija`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Sleep-Time / Dream Konsolidacija):* **NEXUSv2 (`core/dream.py`, `core/sleeptime.py`) + cognee** — pozadinski proces (pokreće se kada je agent neaktivan) koji čita sirove epizodne sesije, izdvaja sažete činjenice (gists) i povezuje ih u Knowledge Graph.
  - *Aspekt B (Spreading Activation i Asocijativni Recall):* **NEXUSv2 (`core/memassoc.py`)** — kada se dohvati jedan memorijski čvor, srodni povezani čvorovi u grafu dobivaju povišenu razinu aktivacije (`activation_level = base + decay * weight`).
  - *Aspekt C (FSRS / Ebbinghaus Decay Rangiranje):* **NEXUSv2 (`core/recall.py`)** — algoritam ponavljanja s razmakom (Free Spaced Repetition Scheduler): zaboravljanje **NE briše podatke fizički**, već smanjuje njihov rang pri pretrazi (`relevance = similarity * recency_weight`).
- **Sinteza (Go dizajn skica):**
  ```go
  package consolidation

  import (
      "context"
      "math"
      "time"
  )

  type SleepTimeConsolidator struct {
      decayHalfLifeDays float64 // e.g. 30.0 days
  }

  // CalculateFSRSRank izračunava težinu dohvata memorije na bazi proteklog vremena i učestalosti
  func (stc *SleepTimeConsolidator) CalculateFSRSRank(similarity float64, lastRecalled time.Time, accessCount int) float64 {
      daysPassed := time.Since(lastRecalled).Hours() / 24.0
      // Ebbinghaus eksponencijalna krivulja zaboravljanja
      retentionFactor := math.Exp(-daysPassed / stc.decayHalfLifeDays)

      // Faktor učestalosti (Frequency Boost)
      frequencyBoost := math.Log1p(float64(accessCount)) * 0.1

      return similarity * (retentionFactor + frequencyBoost)
  }

  func (stc *SleepTimeConsolidator) RunBackgroundConsolidation(ctx context.Context) error {
      // 1. Pročitaj sirove transkripte neaktivnih sesija
      // 2. Ekstrahiraj entitete i relacije u Knowledge Graph
      // 3. Rekalibriraj FSRS težine
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/dream.py`, `core/sleeptime.py`, `core/recall.py` i `core/memassoc.py` u Go `pkg/memory/consolidation`.
- **Ugovor / RED Gate:**
  - Povezano s P2.1; RED test: `test_decay_lowers_search_rank_without_physical_data_loss`.
- **Verifikacija aspekata:**
  - cognee graph extraction (Apache-2.0), FSRS algoritam (MIT, Go/Rust standard).
- **Pareto-floor potvrda:**
  - Osigurava da dugotrajno korištenje ne preplavi memoriju zastarjelim detaljima, dok istovremeno čuva važne i često korištene arhitektonske činjenice.

---

## Rezime S9 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **9.1 Session Management** | MEHANIZAM (JEZGRA) | • **goose / OpenHands:** Event-sourced perzistencija sesija<br>• **LangGraph:** Branching i forking sesija<br>• **Stash:** Virtualni FS transkripata | **Annex A P0.3** (`test_session_resume_restores_exact_turn_state`) |
| **9.2 5-Scope Spine & Purge** | MEHANIZAM | • **NEXUSv2:** **5-Scope Memory Spine** (`GLOBAL|USER|WORKSPACE|SESSION|AGENT`)<br>• **AUDN Protokol:** (`ADD\|NOOP\|SUPERSEDE\|CONTRADICT`)<br>• **Annex A P2.1:** **`MEMORY_FORGET` vs `DATA_PURGE`** | **Annex A P2.1** (`test_purge_cannot_complete_with_residual_copy`) |
| **9.3 Experience Learning** | MEHANIZAM | • **Hermes Agent:** Ekstrakcija proceduralnih vještina<br>• **NEXUSv2:** **`write_approval: true` Default Sigurnosna Ograda**<br>• **Reflexion / ReasonBank:** Verbalno učenje na greškama | **Annex A P1.1 & P1.6** (`test_learned_skill_requires_explicit_user_approval`) |
| **9.4 Sleep-Time & Decay** | MEHANIZAM | • **NEXUSv2 `dream.py`:** **Pozadinska Sleep-Time Konsolidacija** (epizode → KG)<br>• **FSRS / Ebbinghaus Decay:** Gubitak ranga bez fizičkog gubitka podataka | **Annex A P2.1** (`test_decay_lowers_search_rank_without_loss`) |

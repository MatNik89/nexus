PLAN S5

# Hibrid-Hibrida Sinteza: Sekcija S5 (Workspace, VCS, Checkpoint i Rollback)
## CODING-RELEVANTNA SINTEZA — SIGURNOSNA MREŽA I IZOLACIJA

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S5.1–S5.3)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P1.2, P1.4)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `os/exec`, `path/filepath`, `sync`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Workspace i VCS Mehanizama

Sekcija S5 pruža kritičnu sigurnosnu mrežu (safety net) za rad autonomnih agenata nad izvornim kodom:
1. **Per-Step Checkpoint & Rollback (5.1):** Manji i srednji modeli često u 3. ili 4. koraku unište dobar rad iz 1. i 2. koraka. Mogućnost trenutnog povratka na bilo koji prethodni korak (undo) bez onečišćenja korisničke git povijesti omogućuje hrabro eksperimentiranje.
2. **Worktree & Subagent Izolacija (5.2):** Paralelni podagenti (S12) i zadaci ne smiju pisati po istom radnom stablu. Korištenje `git worktree` pruža izolaciju bez skupog kloniranja repoa, uz automatsko čišćenje zaostalih lockova pri startu (P1.2).
3. **Dirty-Tree, Atomic Staging i Provenance (5.3):** Zaštita korisničkog nekomitanog rada, detekcija konflikata (first-writer-wins) i bilježenje autorstva svake linije koda (datamarking / provenance) sprječavaju nenamjerni gubitak podataka.

---

## 2. Arhitektura S5 Workspace Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S5 WORKSPACE ENGINE                                   │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 5.1 SHADOW-GIT CHECKPOINT & ROLLBACK                                              │  │
│  │   • Out-of-Tree Shadow Git (.nexus/shadow_git) • Step-by-Step Snapshots           │  │
│  │   • Instant Rollback • Visual Diff Between Turns • Safe Undo for Weak Models      │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┴───────────────────────────────────┐        │
│        ▼                                                                       ▼        │
│  ┌─────────────────────────────────────────┐   ┌─────────────────────────────────────┐  │
│  │ 5.2 WORKTREE & RUNTIME ISOLATION        │   │ 5.3 ATOMIC STAGING & PROVENANCE     │  │
│  │   • Ephemeral Git Worktrees per Subagent│   │   • Dirty-Tree Preflight & Stashing │  │
│  │   • P1.2 Startup Orphan & Lock Sweep    │   │   • Atomic Staging (.nexus_staging) │  │
│  │   • Fast Merge-Back or Discard          │   │   • First-Writer-Wins Conflict Guard│  │
│  │   • ResourceLease Evidencija u SQLite   │   │   • Datamarking & Taint Attribution │  │
│  └─────────────────────────────────────────┘   └─────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S5.1: Checkpoint i Rollback po Koraku (Shadow-Git Sigurnosna Mreža)

- **Tip:** `MEHANIZAM` (Go paket `pkg/workspace/checkpoint`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Shadow-Git arhitektura izvan glavnog stabla):* **Cline / Roo Code + NEXUSv2 (`core/checkpoint.py`, `core/memgit.py`)** — kreiranje izoliranog git repoa unutar `.nexus/shadow_git/` s postavljenim `--work-tree` na stvarni projekt. Korisnički `.git` ostaje 100% netaknut i čist od privremenih commitova.
  - *Aspekt B (Automatski Snapshot po Turnu i Diff Pregled):* **Aider** — automatsko bilježenje izmjena nakon svakog alata uz generiranje sažetka promjena (`git diff --stat`).
  - *Aspekt C (Deterministički Rollback i Replay):* **LangGraph + Annex A P0.3** — svaki snapshot je vezan uz `turn_id` i `sequence_id` iz `EventJournala`, omogućujući trenutni povratak (`git reset --hard <snapshot_hash>`).
- **Sinteza (Go dizajn skica):**
  ```go
  package checkpoint

  import (
      "context"
      "fmt"
      "os"
      "os/exec"
      "path/filepath"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type SnapshotRecord struct {
      TurnID       int       `json:"turn_id"`
      CommitHash   string    `json:"commit_hash"`
      Timestamp    time.Time `json:"timestamp"`
      Summary      string    `json:"summary"`
      ChangedFiles []string  `json:"changed_files"`
  }

  type ShadowGitManager struct {
      mu            sync.Mutex
      workspaceRoot string
      shadowGitDir  string
      snapshots     map[int]SnapshotRecord // turnID -> snapshot
  }

  func NewShadowGitManager(workspaceRoot string) (*ShadowGitManager, error) {
      shadowDir := filepath.Join(workspaceRoot, ".nexus", "shadow_git")
      if err := os.MkdirAll(shadowDir, 0755); err != nil {
          return nil, err
      }

      mgr := &ShadowGitManager{
          workspaceRoot: workspaceRoot,
          shadowGitDir:  shadowDir,
          snapshots:     make(map[int]SnapshotRecord),
      }

      // Inicijaliziraj shadow git ako ne postoji
      if !fileExists(filepath.Join(shadowDir, "HEAD")) {
          if err := mgr.runGit("init"); err != nil {
              return nil, fmt.Errorf("failed to init shadow git: %w", err)
          }
      }

      return mgr, nil
  }

  func (m *ShadowGitManager) CaptureTurnSnapshot(ctx context.Context, turnID int, summary string) (*SnapshotRecord, error) {
      m.mu.Lock()
      defer m.mu.Unlock()

      // 1. Dodaj sve datoteke u shadow git indeks
      if err := m.runGit("add", "-A"); err != nil {
          return nil, fmt.Errorf("shadow git add failed: %w", err)
      }

      // 2. Kreiraj commit vezan uz turnID
      msg := fmt.Sprintf("nexus-checkpoint: turn-%d: %s", turnID, summary)
      if err := m.runGit("commit", "--allow-empty", "-m", msg); err != nil {
          return nil, fmt.Errorf("shadow git commit failed: %w", err)
      }

      // 3. Dohvati commit hash
      out, err := m.outputGit("rev-parse", "HEAD")
      if err != nil {
          return nil, err
      }
      commitHash := strings.TrimSpace(string(out))

      rec := SnapshotRecord{
          TurnID:     turnID,
          CommitHash: commitHash,
          Timestamp:  time.Now(),
          Summary:    summary,
      }
      m.snapshots[turnID] = rec
      return &rec, nil
  }

  func (m *ShadowGitManager) RollbackToTurn(ctx context.Context, targetTurnID int) error {
      m.mu.Lock()
      defer m.mu.Unlock()

      rec, exists := m.snapshots[targetTurnID]
      if !exists {
          return fmt.Errorf("snapshot for turn %d does not exist", targetTurnID)
      }

      // Resetiraj radno stablo na stanje zadanog snapshot commita
      if err := m.runGit("reset", "--hard", rec.CommitHash); err != nil {
          return fmt.Errorf("rollback to turn %d failed: %w", targetTurnID, err)
      }

      // Očisti novonastale netrackirane datoteke
      if err := m.runGit("clean", "-fd"); err != nil {
          return fmt.Errorf("shadow git clean failed: %w", err)
      }

      return nil
  }

  func (m *ShadowGitManager) runGit(args ...string) error {
      fullArgs := append([]string{"--git-dir=" + m.shadowGitDir, "--work-tree=" + m.workspaceRoot}, args...)
      cmd := exec.Command("git", fullArgs...)
      return cmd.Run()
  }

  func (m *ShadowGitManager) outputGit(args ...string) ([]byte, error) {
      fullArgs := append([]string{"--git-dir=" + m.shadowGitDir, "--work-tree=" + m.workspaceRoot}, args...)
      cmd := exec.Command("git", fullArgs...)
      return cmd.Output()
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/checkpoint.py` i `core/memgit.py` u Go `pkg/workspace/checkpoint` uz eliminaciju Python git ovisnosti.
- **Ugovor / RED Gate:**
  - **Annex A P0.3 & P1.4**; Gate test: `test_shadow_git_reverts_unwanted_edits_without_polluting_user_git`.
- **Verifikacija aspekata:**
  - Cline / Roo Code shadow-git (Apache-2.0, TS), Aider checkpointing (Apache-2.0).
- **Pareto-floor potvrda:**
  - Čuva kompletnu povijest koraka u izoliranom shadow-gitu s vremenom snapshotiranja < 50ms, ne dirajući korisničku `.git` konfiguraciju.

---

## S5.2: Workspace Izolacija (Git Worktrees i Runtime Sandboxes)

- **Tip:** `MEHANIZAM` (Go paket `pkg/workspace/worktree`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Izolirana radna stabla po podagentu / grani):* **OpenHands / SWE-agent / NEXUSv2 (`core/worktree.py`)** — korištenje `git worktree add` za stvaranje paralelnih, laganih radnih prostora koji dijele isti lokalni `.git` repozitorij bez umnožavanja diska.
  - *Aspekt B (Startup Orphan Sweep i Čišćenje Lockova):* **Annex A P1.2 + agy#2** — obvezno evidentiranje svakog worktreeja u SQLite tablici `resource_leases` uz automatsko uklanjanje napuštenih worktreeja (`git worktree prune`) i brisanje `.git/worktrees/<name>/locked` pri restartu nakon `SIGKILL` prekida.
  - *Aspekt C (Brzi Merge-Back ili Discard):* **Git Worktree Standard** — atomski merge promjena u glavnu granu ili potpuno odbacivanje ako testovi podagenta nisu prošli.
- **Sinteza (Go dizajn skica):**
  ```go
  package worktree

  import (
      "context"
      "fmt"
      "os"
      "os/exec"
      "path/filepath"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type WorktreeLease struct {
      ID          string    `json:"id"`
      SubagentID  string    `json:"subagent_id"`
      Path        string    `json:"path"`
      BranchName  string    `json:"branch_name"`
      CreatedAt   time.Time `json:"created_at"`
      HeartbeatAt time.Time `json:"heartbeat_at"`
  }

  type WorktreeManager struct {
      mu            sync.Mutex
      repoRoot      string
      worktreeBase  string
      activeLeases  map[string]*WorktreeLease // subagentID -> lease
  }

  func NewWorktreeManager(repoRoot string) (*WorktreeManager, error) {
      base := filepath.Join(repoRoot, ".nexus", "worktrees")
      if err := os.MkdirAll(base, 0755); err != nil {
          return nil, err
      }

      mgr := &WorktreeManager{
          repoRoot:     repoRoot,
          worktreeBase: base,
          activeLeases: make(map[string]*WorktreeLease),
      }

      // Pokreni P1.2 Startup Sweep napuštenih worktreeja
      if err := mgr.StartupSweepOrphans(); err != nil {
          return nil, fmt.Errorf("worktree startup sweep failed: %w", err)
      }

      return mgr, nil
  }

  func (wm *WorktreeManager) CreateWorktree(ctx context.Context, subagentID string) (*WorktreeLease, error) {
      wm.mu.Lock()
      defer wm.mu.Unlock()

      branchName := fmt.Sprintf("nexus-subagent-%s", subagentID)
      wtPath := filepath.Join(wm.worktreeBase, subagentID)

      // git worktree add -b <branchName> <wtPath> HEAD
      cmd := exec.CommandContext(ctx, "git", "-C", wm.repoRoot, "worktree", "add", "-b", branchName, wtPath, "HEAD")
      if out, err := cmd.CombinedOutput(); err != nil {
          return nil, fmt.Errorf("failed to create git worktree: %s (%w)", string(out), err)
      }

      lease := &WorktreeLease{
          ID:          subagentID,
          SubagentID:  subagentID,
          Path:        wtPath,
          BranchName:  branchName,
          CreatedAt:   time.Now(),
          HeartbeatAt: time.Now(),
      }
      wm.activeLeases[subagentID] = lease
      return lease, nil
  }

  func (wm *WorktreeManager) RemoveWorktree(ctx context.Context, subagentID string, mergeBack bool) error {
      wm.mu.Lock()
      defer wm.mu.Unlock()

      lease, exists := wm.activeLeases[subagentID]
      if !exists {
          return fmt.Errorf("no active worktree for subagent %s", subagentID)
      }

      if mergeBack {
          // Mergeaj promjene natrag u glavnu granu
          mergeCmd := exec.CommandContext(ctx, "git", "-C", wm.repoRoot, "merge", "--no-ff", lease.BranchName)
          if out, err := mergeCmd.CombinedOutput(); err != nil {
              return fmt.Errorf("failed to merge worktree branch: %s (%w)", string(out), err)
          }
      }

      // Ukloni worktree i obriši granu
      _ = exec.Command("git", "-C", wm.repoRoot, "worktree", "remove", "--force", lease.Path).Run()
      _ = exec.Command("git", "-C", wm.repoRoot, "branch", "-D", lease.BranchName).Run()

      delete(wm.activeLeases, subagentID)
      return nil
  }

  // StartupSweepOrphans provodi P1.2 čišćenje napuštenih stabala
  func (wm *WorktreeManager) StartupSweepOrphans() error {
      // 1. Prune git worktreeja
      cmd := exec.Command("git", "-C", wm.repoRoot, "worktree", "prune")
      _ = cmd.Run()

      // 2. Ukloni zaostale zaključane mape
      files, _ := os.ReadDir(wm.worktreeBase)
      for _, f := range files {
          if f.IsDir() {
              wtDir := filepath.Join(wm.worktreeBase, f.Name())
              _ = exec.Command("git", "-C", wm.repoRoot, "worktree", "remove", "--force", wtDir).Run()
              _ = os.RemoveAll(wtDir)
          }
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/worktree.py` i SQLite lease evidencije u Go `pkg/workspace/worktree`.
- **Ugovor / RED Gate:**
  - **Annex A P1.2** (Startup sweep); RED test: `test_sigkill_restart_reaps_owned_tree_only` (ubij proces, novi startup mora očistiti sve lockove i radna stabla).
- **Verifikacija aspekata:**
  - Git worktree spec (Git SCM standard), OpenHands workspace model (MIT).
- **Pareto-floor potvrda:**
  - Omogućuje konkurentni rad proizvoljnog broja podagenata na istom disku bez sukoba datoteka i bez dupliciranja `.git` povijesti.

---

## S5.3: Dirty-Tree, Atomsko Izvođenje, Konflikt Detekcija i Provenance

- **Tip:** `MEHANIZAM` (Go paket `pkg/workspace/staging`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Preflight Provjera i Sigurno Stashanje Ne-komitanog Rada):* **NEXUSv2 (`core/git_guard.py`)** — prije početka rada agent provjerava je li radno stablo prljavo (`git status --porcelain`). Ako jest, korisnikov rad se automatski stasha u privremeni izolirani stash slot (`nexus-auto-stash-<timestamp>`).
  - *Aspekt B (Atomsko Staging Zapisivanje i First-Writer-Wins):* **Annex A P1.4 + S4.3** — svaka izmjena datoteke vrši se kroz `.nexus_staging` datoteku uz provjeru SHA-256 hasha prije upisa (First-Writer-Wins detekcija konflikta). Ako je drugi proces ili korisnik promijenio datoteku u međuvremenu, upis se odbija s `CONFLICT`.
  - *Aspekt C (Provenance, Taint Tracking i Datamarking):* **NEXUSv2 (`core/provenance.py`) + PROV-O Standard** — svaka izmijenjena linija koda označava se metapodacima: koji model, u kojem turnu, s kojim `run_id` i `trust_class` oznakom je generirao liniju.
- **Sinteza (Go dizajn skica):**
  ```go
  package staging

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "os"
      "sync"
      "time"

      "nexus/pkg/contract"
      "nexus/pkg/sys"
  )

  type FileProvenanceRecord struct {
      FilePath      string              `json:"file_path"`
      FileHash      string              `json:"file_hash"`
      LastAuthor    string              `json:"last_author"` // runID / modelID
      TurnID        int                 `json:"turn_id"`
      TrustClass    contract.TrustClass `json:"trust_class"`
      ModifiedAt    time.Time           `json:"modified_at"`
      LineRanges    []LineRangeMarker   `json:"line_ranges"`
  }

  type LineRangeMarker struct {
      StartLine  int                 `json:"start_line"`
      EndLine    int                 `json:"end_line"`
      Author     string              `json:"author"`
      TrustClass contract.TrustClass `json:"trust_class"`
  }

  type SafeAtomicWriter struct {
      mu            sync.Mutex
      sanitizer     *sys.PathSanitizer
      provenanceLog map[string]*FileProvenanceRecord // filePath -> record
  }

  func (saw *SafeAtomicWriter) AtomicWrite(filePath string, newContent []byte, expectedBaseHash string, author string, turnID int, trust contract.TrustClass) error {
      saw.mu.Lock()
      defer saw.mu.Unlock()

      safePath, err := saw.sanitizer.ResolveSafePath(filePath)
      if err != nil {
          return fmt.Errorf("SECURITY DENY: %w", err)
      }

      // 1. First-Writer-Wins provjera konflikta (ako datoteka već postoji)
      if existingData, err := os.ReadFile(safePath); err == nil {
          currentHashBytes := sha256.Sum256(existingData)
          currentHash := hex.EncodeToString(currentHashBytes[:])

          if expectedBaseHash != "" && currentHash != expectedBaseHash {
              return fmt.Errorf("CONFLICT_DETECTED: file %s was modified by another actor (expected %s, got %s)",
                  filePath, expectedBaseHash, currentHash)
          }
      }

      // 2. Atomska pohrana kroz staging privremenu datoteku
      stagingPath := safePath + ".nexus_staging"
      if err := os.WriteFile(stagingPath, newContent, 0644); err != nil {
          return fmt.Errorf("failed writing staging file: %w", err)
      }

      // 3. Atomski rename (OS garancija)
      if err := os.Rename(stagingPath, safePath); err != nil {
          _ = os.Remove(stagingPath)
          return fmt.Errorf("atomic rename commit failed: %w", err)
      }

      // 4. Zabilježi provenance i datamarking
      newHashBytes := sha256.Sum256(newContent)
      saw.provenanceLog[filePath] = &FileProvenanceRecord{
          FilePath:   filePath,
          FileHash:   hex.EncodeToString(newHashBytes[:]),
          LastAuthor: author,
          TurnID:     turnID,
          TrustClass: trust,
          ModifiedAt: time.Now(),
      }

      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/provenance.py` (datamarking i praćenje porijekla) i `core/git_guard.py` (dirty tree zaštita) u Go `pkg/workspace/staging`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1 & P1.4**; Gate test: `test_first_writer_wins_rejects_concurrent_file_mutation` i `test_provenance_records_author_and_trust_class`.
- **Verifikacija aspekata:**
  - PROV-O (W3C standard), git stash spec, atomic file write POSIX semantics.
- **Pareto-floor potvrda:**
  - Sprječava gubitak koda kod paralelnih izmjena i pruža kompletnu revizijsku sljedivost (tko je generirao koju liniju koda).

---

## Rezime S5 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **5.1 Checkpoint & Rollback** | MEHANIZAM | • **Cline / Roo Code:** Out-of-tree `.nexus/shadow_git`<br>• **Aider:** Automatski snapshot po turnu<br>• **LangGraph:** Instant revert i undo sigurnosna mreža | **Annex A P0.3 & P1.4** (`test_shadow_git_reverts_unwanted_edits_without_polluting_user_git`) |
| **5.2 Worktree & Izolacija** | MEHANIZAM | • **OpenHands / SWE-agent:** Izolirano radno stablo po zadatku<br>• **Annex A P1.2 + agy#2:** **Startup Orphan Sweep** napuštenih worktreeja i lockova | **Annex A P1.2** (`test_sigkill_restart_reaps_owned_tree_only`) |
| **5.3 Atomic Staging & Provenance** | MEHANIZAM | • **NEXUSv2:** Dirty-tree stash zaštita<br>• **First-Writer-Wins:** Detekcija konflikta na bazi hasha<br>• **PROV-O:** **Datamarking & Taint Attribution** svake linije koda | **Annex A P0.1 & P1.4** (`test_first_writer_wins_rejects_concurrent_file_mutation`) |

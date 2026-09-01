PLAN S1

# Hibrid-Hibrida Sinteza: Sekcija S1 (Temelji / Foundation)

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S1.1–S1.3)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P1.2)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `golang.org/x/sys`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## Pregled sekcije S1

Sekcija S1 uspostavlja temeljne operativne mehanizme sustava: slojevitu konfiguraciju s nepromjenjivim prioritetom, cross-platformsku izolaciju procesa i sigurnost datotečnog sustava te OTel-kompatibilno strukturirano logiranje vezano na jedinstveni `EventJournal` write-owner.

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S1 FOUNDATION LAYER                                   │
│                                                                                         │
│  ┌───────────────────────┐   ┌───────────────────────┐   ┌───────────────────────────┐  │
│  │ S1.1 Config Resolver  │   │ S1.2 Cross-Platform Sys│  │ S1.3 Telemetry & Journal  │  │
│  │ 6-slojni Precedence,  │──→│ Path Sandbox, PGID,   │──→│ Single Write-Owner (P0.3),│  │
│  │ Schema Validacija,    │   │ Job Objects (Win),    │   │ OTel Spans, Redaction,    │  │
│  │ Dot-notation Override │   │ Process Tree Sweep    │   │ Zero-Alloc Structured Log │  │
│  └───────────────────────┘   └───────────────────────┘   └───────────────────────────┘  │
│             │                            │                            │                 │
│             ▼                            ▼                            ▼                 │
│      [TOML/Env/Flags]            [OS Kernel Syscalls]          [SQLite WAL Append-Only] │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S1.1: Slojevita Konfiguracija (Config Resolver)

- **Tip:** `MEHANIZAM` (Go paket `pkg/config`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Strogi deterministički poredak prioriteta / Precedence):* **Aider** — dokumentirani, nepromjenjivi 6-slojni poredak spajanja: `Default u kodu → Global (~/.nexus/config.toml) → Projekt (.nexus/config.toml) → .env datoteka → Environment varijable (NEXUS_*) → CLI zastavice / -c overrides`.
  - *Aspekt B (Snažna tipizacija, TOML profili i dot-notation override):* **Codex CLI** — TOML konfiguracija s imenovanim profilima (`--profile dev|ci|prod`) i granularnim izmjenama po točkastom ključu (`-c model.provider=deepseek -c sandbox.timeout=60s`).
  - *Aspekt C (Statička validacija i schema-driven hot-reload):* **Continue** — kompletna validacija sheme prije primjene (fail-closed na nepoznati ili pogrešan tip) uz mogućnost sigurnog reloadanja konfiguracije tijekom rada.
- **Sinteza (Go dizajn skica):**
  ```go
  package config

  import (
      "fmt"
      "os"
      "strings"
      "time"
  )

  type Config struct {
      Version     int               `toml:"version" validate:"required,gte=1"`
      ActiveProfile string          `toml:"profile"`
      Model       ModelConfig       `toml:"model"`
      Sandbox     SandboxConfig     `toml:"sandbox"`
      Telemetry   TelemetryConfig   `toml:"telemetry"`
      Storage     StorageConfig     `toml:"storage"`
      FeatureFlags []string         `toml:"feature_flags"`
  }

  type ModelConfig struct {
      DefaultProvider string        `toml:"default_provider" validate:"required"`
      DefaultModel    string        `toml:"default_model" validate:"required"`
      FallbackModels  []string      `toml:"fallback_models"`
      RequestTimeout  time.Duration `toml:"request_timeout" validate:"required"`
  }

  type SandboxConfig struct {
      Mode            string        `toml:"mode" validate:"oneof=landlock bwrap container disabled"`
      MaxMemoryMB     int           `toml:"max_memory_mb" validate:"gte=64"`
      MaxCPUTimeSec   int           `toml:"max_cpu_time_sec" validate:"gte=1"`
      AllowedReadDirs []string      `toml:"allowed_read_dirs"`
      AllowedWriteDirs []string     `toml:"allowed_write_dirs"`
  }

  type TelemetryConfig struct {
      LogLevel        string        `toml:"log_level" validate:"oneof=debug info warn error"`
      EnableOTel      bool          `toml:"enable_otel"`
      OTelEndpoint    string        `toml:"otel_endpoint"`
  }

  type StorageConfig struct {
      DataDir         string        `toml:"data_dir"`
      JournalDBPath   string        `toml:"journal_db_path"`
  }

  type Resolver struct {
      defaults Config
  }

  func (r *Resolver) Resolve(globalPath, projectPath, envPath string, cliFlags map[string]any, dotOverrides []string) (*Config, error) {
      cfg := r.defaults

      // 1. Učitaj Global TOML ako postoji
      if globalPath != "" && fileExists(globalPath) {
          if err := loadTOML(globalPath, &cfg); err != nil {
              return nil, fmt.Errorf("failed loading global config: %w", err)
          }
      }

      // 2. Učitaj Projektni TOML ako postoji (gazi global)
      if projectPath != "" && fileExists(projectPath) {
          if err := loadTOML(projectPath, &cfg); err != nil {
              return nil, fmt.Errorf("failed loading project config: %w", err)
          }
      }

      // 3. Učitaj .env datoteku
      if envPath != "" && fileExists(envPath) {
          loadDotEnv(envPath)
      }

      // 4. Primijeni NEXUS_* environment varijable
      applyEnvOverrides(&cfg)

      // 5. Primijeni eksplicitne CLI zastavice
      applyFlagOverrides(&cfg, cliFlags)

      // 6. Primijeni -c dot-notation overrides (npr. "sandbox.max_memory_mb=1024")
      for _, ov := range dotOverrides {
          if err := applyDotOverride(&cfg, ov); err != nil {
              return nil, fmt.Errorf("invalid dot-override %q: %w", ov, err)
          }
      }

      // 7. Konačna validacija sheme (fail-closed)
      if err := validateStruct(&cfg); err != nil {
          return nil, fmt.Errorf("config schema validation failed: %w", err)
      }

      return &cfg, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Zamjena netipiziranog `core/config.py` i rascjepkanih `.env` učitavača strogo tipiziranim Go paketom `pkg/config` uz korištenje `pelletier/go-toml/v2`.
- **Ugovor / RED Gate:**
  - Povezano s **Annex A P0.4** (Closure Resolver validira `config_hash`); RED test: `test_config_precedence_and_validation` (provjerava da CLI flag gazi ENV, ENV gazi projekt, nevaljana vrijednost odmah ruši pokretanje).
- **Verifikacija aspekata:**
  - Codex CLI TOML config (Apache-2.0, Rust), Aider precedence (Apache-2.0, Python), Continue schema config (Apache-2.0, TS).
- **Pareto-floor potvrda:**
  - Sinteza objedinjuje Aiderov 6-slojni poredak prioriteta (A), Codexovu podršku za profile i točkaste ključeve (B) te Continueovu strogu schema validaciju (C) unutar čistog Go koda bez vanjskih runtimeova.

---

## S1.2: Cross-Platform Putanje i Procesi

- **Tip:** `MEHANIZAM` (Go paket `pkg/sys`, s per-OS implementacijama: `process_unix.go`, `process_windows.go`, `path.go`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Sigurne kanonske putanje i XDG standard):* **goose** — čist OS-agnostički sloj za rad s putanjama (`XDG_DATA_HOME` na Linuxu, `Application Support` na macOS-u, `%LOCALAPPDATA%` na Windowsima) uz strogu zaštitu od path traversal napada (`..`).
  - *Aspekt B (Determinističko upravljanje procesnim stablom i signalima):* **Codex CLI** — identično ponašanje gašenja i terminacije procesnih grupa na sva 3 operacijska sustava (`SIGINT` → graceful grace period → `SIGKILL`).
  - *Aspekt C (Nadzor procesa i hvatanje izlaza bez blokiranja):* **Aider** — robusno strujanje stdout/stderr tokova uz prevenciju deadlocka kod punog međuspremnika (buffer overflow).
- **Sinteza (Go dizajn skica):**
  ```go
  package sys

  import (
      "context"
      "fmt"
      "io"
      "os"
      "os/exec"
      "path/filepath"
      "strings"
      "time"
  )

  // --- Sigurnost putanja (Path Security) ---

  type PathSanitizer struct {
      workspaceRoot string
  }

  func NewPathSanitizer(workspaceRoot string) (*PathSanitizer, error) {
      abs, err := filepath.Abs(workspaceRoot)
      if err != nil {
          return nil, err
      }
      realPath, err := filepath.EvalSymlinks(abs)
      if err != nil {
          realPath = abs
      }
      return &PathSanitizer{workspaceRoot: realPath}, nil
  }

  func (ps *PathSanitizer) ResolveSafePath(relOrAbsPath string) (string, error) {
      var target string
      if filepath.IsAbs(relOrAbsPath) {
          target = filepath.Clean(relOrAbsPath)
      } else {
          target = filepath.Clean(filepath.Join(ps.workspaceRoot, relOrAbsPath))
      }

      // Razriješi symlinkove radi sprječavanja symlink escapea
      realTarget, err := filepath.EvalSymlinks(target)
      if err != nil && !os.IsNotExist(err) {
          return "", fmt.Errorf("symlink resolution failed: %w", err)
      }
      if err == nil {
          target = realTarget
      }

      // Provjera granice radnog prostora
      if !strings.HasPrefix(target, ps.workspaceRoot+string(filepath.Separator)) && target != ps.workspaceRoot {
          return "", fmt.Errorf("SECURITY DENY: path %q escapes workspace root %q", relOrAbsPath, ps.workspaceRoot)
      }

      return target, nil
  }

  // --- Cross-Platform Procesna Grupa ---

  type ProcessGroup interface {
      Start() error
      Wait() (*ExecResult, error)
      Signal(sig os.Signal) error
      Kill() error
      PID() int
      PGID() int
  }

  type ExecResult struct {
      ExitCode   int
      Duration   time.Duration
      Stdout     []byte
      Stderr     []byte
      Terminated bool
  }

  type NativeProcessGroup struct {
      cmd        *exec.Cmd
      ctx        context.Context
      cancelFunc context.CancelFunc
      pgid       int
      startTime  time.Time
  }

  // Build tags razdvajaju implementaciju:
  // process_unix.go (Linux/Darwin):
  //   cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
  //   Kill() šalje syscall.Kill(-pgid, syscall.SIGKILL)
  //
  // process_windows.go:
  //   cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
  //   Kill() koristi Windows Job Object (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE) ili taskkill /T /F
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/paths.py` (kanonizacija putanja i workspace scoping) i `core/subproc.py` (streaming izlaza, timeouti) u Go `pkg/sys`. Nativni Go kod GORTEX-a se izravno integrira u ovaj paket.
- **Ugovor / RED Gate:**
  - **Annex A P1.2** (`ResourceLease` evidencija i startup orphan sweep); RED test: `test_sigkill_restart_reaps_owned_tree_only` i `test_path_traversal_escape_rejected`.
- **Verifikacija aspekata:**
  - goose (Apache-2.0, Rust/Goose sys), Codex CLI (Apache-2.0, Rust sys), Aider (Apache-2.0, Python subprocess).
- **Pareto-floor potvrda:**
  - Nudi hermetičnu zaštitu od `..` i symlink escape napada (A), pouzdano ubija cjelokupna procesna stabla na Linuxu/macOS/Windowsima bez curenja siročadi (B) i sprječava I/O blokade kod streamanja alata (C).

---

## S1.3: Strukturirano Logiranje i Telemetrija od Prvog Dana

- **Tip:** `MEHANIZAM` (Go paket `pkg/telemetry` i `pkg/journal`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Standardizirani OTel kontekst i span hijerarhija):* **OpenTelemetry SDK / Spec** — industrijski standard za distribuirano praćenje: `trace_id`, `span_id`, causal parent, baggage, OTel semantičke konvencije za AI tokove (`gen_ai.system`, `gen_ai.request.model`).
  - *Aspekt B (Zero-Allocation brzo strukturirano logiranje):* **structlog (Python) / tracing (Rust) / zerolog (Go)** — strukturirani JSON logovi s nula alokacija na vrućoj stazi, strogo tipizirana polja (`Int64()`, `Str()`, `Dur()`, `Err()`).
  - *Aspekt C (Event Stream kao Jedini Trajni Izvor Istine / Write-Owner):* **OpenHands + Annex A P0.3** — `EventJournal.Append` je JEDINI write-owner u sustavu. Svi logovi, metrike, transkripti i revizijski tragovi su read-only projekcije (views) ovog toka.
- **Sinteza (Go dizajn skica):**
  ```go
  package journal

  import (
      "context"
      "crypto/sha256"
      "database/sql"
      "encoding/hex"
      "fmt"
      "sync"
      "time"

      "nexus/pkg/contract"
  )

  type JournalEvent struct {
      contract.Envelope
      JournalOffset int64  `json:"journal_offset"`
      PrevHash      string `json:"prev_hash"`
      IntegrityHash string `json:"integrity_hash"`
  }

  type EventJournal interface {
      Append(ctx context.Context, env contract.Envelope) (*JournalEvent, error)
      ReadFromOffset(ctx context.Context, offset int64, limit int) ([]JournalEvent, error)
      Subscribe(ctx context.Context, fromOffset int64) (<-chan JournalEvent, error)
  }

  type SQLiteEventJournal struct {
      db       *sql.DB
      mu       sync.Mutex
      lastSeq  int64
      lastHash string
  }

  func (j *SQLiteEventJournal) Append(ctx context.Context, env contract.Envelope) (*JournalEvent, error) {
      j.mu.Lock()
      defer j.mu.Unlock()

      // 1. Redakcija tajni i osjetljivih podataka PRIJE trajnog upisa
      redactedPayload := RedactSecrets(env.Payload)
      env.Payload = redactedPayload

      // 2. Alokacija strogog monotonog sequence broja
      j.lastSeq++
      env.Sequence = j.lastSeq

      // 3. Izračun kriptografskog hash lanca (P0.3 Integrity)
      payloadHash := sha256.Sum256(env.Payload)
      env.PayloadHash = hex.EncodeToString(payloadHash[:])

      hashInput := fmt.Sprintf("%d:%s:%s:%d", env.Sequence, j.lastHash, env.PayloadHash, env.EmittedAt.UnixNano())
      integrityHash := sha256.Sum256([]byte(hashInput))
      currentHash := hex.EncodeToString(integrityHash[:])

      je := &JournalEvent{
          Envelope:      env,
          JournalOffset: j.lastSeq,
          PrevHash:      j.lastHash,
          IntegrityHash: currentHash,
      }

      // 4. Atomski upis u SQLite tablicu (P0.3 Append-Only)
      query := `INSERT INTO journal_events (sequence, stream_id, event_type, payload, prev_hash, integrity_hash, emitted_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)`
      _, err := j.db.ExecContext(ctx, query, je.Sequence, je.RunID, je.EventType, je.Payload, je.PrevHash, je.IntegrityHash, je.EmittedAt.UnixNano())
      if err != nil {
          j.lastSeq-- // Rollback in-memory brojača pri grešci baze
          return nil, fmt.Errorf("journal append failed: %w", err)
      }

      j.lastHash = currentHash
      return je, nil
  }

  // --- Projekcije (Views) ---

  type Projection interface {
      Apply(event JournalEvent) error
  }

  type DiagnosticLogProjection struct{}
  func (p *DiagnosticLogProjection) Apply(event JournalEvent) error {
      // Formatira u zero-alloc JSON log za stdout/stderr
      return nil
  }

  type OTelSpanProjection struct{}
  func (p *OTelSpanProjection) Apply(event JournalEvent) error {
      // Emitira OTel span/event preko OTLP gRPC/HTTP exportera
      return nil
  }

  type UserTranscriptProjection struct{}
  func (p *UserTranscriptProjection) Apply(event JournalEvent) error {
      // Formatira događaj za korisnički TUI/CLI prikaz
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port i refaktoriranje `core/trace.py` i `core/log.py` u OTel-kompatibilni `pkg/telemetry` uz vezivanje na `modernc.org/sqlite` append-only engine.
- **Ugovor / RED Gate:**
  - **Annex A P0.3** (Jedini write-owner za event/log/trace/audit); Gate test: `test_projection_cannot_bypass_journal`.
- **Verifikacija aspekata:**
  - OpenTelemetry Go SDK (Apache-2.0, CNCF standard), zerolog (MIT), OpenHands (MIT).
- **Pareto-floor potvrda:**
  - Eliminira podijeljenu istinu: OTel industrijski standard (A) + zero-allocation performanse (B) + kriptografski verificiran append-only journal (C) s obaveznom redakcijom prije diska.

---

## Rezime S1 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **1.1 Slojevita Konfiguracija** | MEHANIZAM | • **Aider:** 6-slojni deterministički precedence<br>• **Codex CLI:** TOML profili i `-c` dot-notation<br>• **Continue:** Schema validacija i hot-reload | **Annex A P0.4** (`test_config_precedence_and_validation`) |
| **1.2 Cross-Platform Sys** | MEHANIZAM | • **goose:** XDG putanje i safe path isolation<br>• **Codex CLI:** Cross-platform process groups & clean kill<br>• **Aider:** Non-blocking async I/O streaming | **Annex A P1.2** (`test_sigkill_restart_reaps_owned_tree_only`, `test_path_traversal_escape_rejected`) |
| **1.3 Telemetrija & Journal** | MEHANIZAM | • **OpenTelemetry:** Standardni spanovi i korelacijski ID-evi<br>• **zerolog / structlog:** Zero-alloc JSON logiranje<br>• **OpenHands + Annex A P0.3:** `EventJournal.Append` jedini write-owner | **Annex A P0.3** (`test_projection_cannot_bypass_journal`) |

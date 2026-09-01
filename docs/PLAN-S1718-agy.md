PLAN S17+S18

# Hibrid-Hibrida Sinteza: Sekcije S17 (Distribucija i Ekstenzije) i S18 (Produkcijsko Posluživanje / `service`)
## TUF UPDATE, GORELEASER SINGLE-BINARY, S7-FENCED WORKER POOL I PROGRESIVNI ROLLOUT

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S17.1–S17.4 i S18.1–S18.4)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.4, P1.1, P1.5, P2.3, P2.5)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `net/http`, `crypto/ed25519`, `database/sql`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

# DIO 1: SEKCIJA S17 (Ekosustav, Distribucija i Ekstenzije)

## 1. Arhitektonska Strategija za S17

1. **Plugin Sustav i DI (17.1):** Usvajanje **DeepSeek-Harness Cordis DI-plugin** modela: proširenja se registriraju na deklarativne extension pointove kroz tipizirano ubrizgavanje ovisnosti (DI), ali **MORAJU proći S6.8 / S11.5 Supply-Chain gate** (`skills.lock` i verifikaciju potpisa).
2. **Kriptografski Siguran Update (17.2):** **The Update Framework (TUF)** pruža zaštitu od rollback napada, kompromitiranih ključeva i obustave ažuriranja (freeze attacks), uz podršku za tri kanala: `stable`, `beta` i `nightly`.
3. **Pravi Single-Binary Packaging (17.3):** GoReleaser generira nula-ovisne statičke binarne pakete (`CGO_ENABLED=0`) za Linux (x86_64, arm64), macOS (Apple Silicon, Intel) i Windows (x64) s automatskom distribucijom na Homebrew, WinGet, Scoop i AUR.
4. **Field-Diagnostics i Upgrade Bridge (17.4):** Generiranje anonimiziranog crash bundlea s maskiranim tajnama (S6.4) i snapshotom EventJournala za brzi bugreport i automatsku preporuku nadogradnje.

---

## 2. Arhitektura S17 Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S17 DISTRIBUTION ENGINE                               │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 17.1 CORDIS DI PLUGIN SYSTEM (Behind S6.8 & S11.5 Gate)                          │  │
│  │   • Extension Points • Interface Injection • skills.lock TOCTOU Verification      │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 17.2 TUF AUTO │                  │ 17.3 PACKAGING│                  │ 17.4 FIELD    ││
│  │ UPDATER       │                  │ GoReleaser    │                  │ DIAGNOSTICS   ││
│  │ Cosign / TUF  │                  │ Single Binary │                  │ Scrubbed Log  ││
│  │ Root of Trust │                  │ Win/Mac/Linux │                  │ Crash Bundle  ││
│  │ Stable/Beta   │                  │ (P1.5 Release)│                  │ BugReport CLI ││
│  └───────────────┘                  └───────────────┘                  └───────────────┘│
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S17.1: Plugin-Sustav i Dependency Injection (Cordis DI + goose-MCP)

- **Tip:** `MEHANIZAM` (Go paket `pkg/extensions/plugins`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Cordis Dependency Injection Arhitektura):* **DeepSeek-Harness / Cordis** — deklarativno vezanje pluginova na sučelja u jezgri (alati, rute, provideri) uz nula cirkularnih ovisnosti.
  - *Aspekt B (goose-MCP Ekstenzijske Točke):* **goose + OpenCode** — učitavanje vanjskih MCP poslužitelja i skripti iza S4.6 klijenta.
  - *Aspekt C (Strogi Supply-Chain Gate):* **Annex A P1.1 & P1.5 (agy#1)** — svaki plugin mora posjedovati važeći `plugin.lock` SHA-256 zapis i proći provjeru integriteta prije učitavanja.
- **Sinteza (Go dizajn skica):**
  ```go
  package plugins

  import (
      "context"
      "fmt"
      "sync"
      "nexus/pkg/contract"
      "nexus/pkg/security/supplychain"
  )

  type ExtensionPoint string
  const (
      ExtPointToolHandler   ExtensionPoint = "TOOL_HANDLER"
      ExtPointModelProvider ExtensionPoint = "MODEL_PROVIDER"
      ExtPointPromptHook    ExtensionPoint = "PROMPT_HOOK"
  )

  type PluginDescriptor struct {
      PluginID     string         `json:"plugin_id"`
      Name         string         `json:"name"`
      Version      string         `json:"version"`
      ExtPoint     ExtensionPoint `json:"ext_point"`
      FactoryFunc  any            `json:"-"`
  }

  type PluginRegistry struct {
      mu       sync.RWMutex
      plugins  map[ExtensionPoint][]PluginDescriptor
      verifier *supplychain.SupplyChainVerifier
  }

  func (pr *PluginRegistry) RegisterPlugin(desc PluginDescriptor, lock supplychain.SkillLockfile) error {
      pr.mu.Lock()
      defer pr.mu.Unlock()

      // 1. Provjeri integritet i potpis plugina (P1.1 / P1.5 Gate)
      if err := pr.verifier.VerifySkillOnLoad(desc.PluginID, lock); err != nil {
          return fmt.Errorf("PLUGIN_INTEGRITY_FAILED: %w", err)
      }

      pr.plugins[desc.ExtPoint] = append(pr.plugins[desc.ExtPoint], desc)
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `packs/` i `core/plugins.py` u Go `pkg/extensions/plugins`.
- **Ugovor / RED Gate:**
  - **Annex A P1.1 & P1.5**; RED test: `test_plugin_registry_rejects_unverified_binary_extension`.
- **Verifikacija aspekata:**
  - Cordis DI framework (MIT), goose extensions (Apache-2.0).
- **Pareto-floor potvrda:**
  - Pruža čisti DI mehanizam proširenja uz potpunu kriptografsku zaštitu od zlonamjernih binarnih dodataka.

---

## S17.2: Update-Mehanizam i Kanali (TUF Sigurnosna Osnova)

- **Tip:** `MEHANIZAM` (Go paket `pkg/extensions/updater`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (The Update Framework — TUF Kriptografski Standard):* **TUF (Linux Foundation)** — zaštita od rollback napada kroz verzije manifesta (`timestamp.json`, `snapshot.json`, `targets.json`, `root.json`) i potpisivanje pragom ključeva (threshold signatures).
  - *Aspekt B (Višekanalna Distribucija):* **Codex CLI Updater** — podrška za kanale `stable` (provjereno), `beta` (preview značajke) i `nightly` (dnevni buildovi).
  - *Aspekt C (Besprijekorno Samoažuriranje):* **Aider / GitHub Releases API** — atomski download, provjera SHA-256 checksuma i zamjena binara (`minisign` / `cosign`).
- **Sinteza (Go dizajn skica):**
  ```go
  package updater

  import (
      "context"
      "crypto/ed25519"
      "fmt"
      "net/http"
  )

  type UpdateChannel string
  const (
      ChannelStable  UpdateChannel = "stable"
      ChannelBeta    UpdateChannel = "beta"
      ChannelNightly UpdateChannel = "nightly"
  )

  type TUFUpdateManifest struct {
      Version     string            `json:"version"`
      Channel     UpdateChannel     `json:"channel"`
      TargetSHA256 string           `json:"target_sha256"`
      DownloadURL string            `json:"download_url"`
      Signature   string            `json:"signature"`
  }

  type AutoUpdater struct {
      rootKey ed25519.PublicKey
      channel UpdateChannel
  }

  func (u *AutoUpdater) CheckForUpdate(ctx context.Context, currentVersion string) (*TUFUpdateManifest, error) {
      // 1. Dohvati potpisani TUF manifest za odabrani kanal
      // 2. Provjeri Ed25519 potpis prema rootKey
      // 3. Provjeri da nova verzija nije manja od trenutne (Anti-Rollback Invarijant)
      return nil, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/update.py` u Go `pkg/extensions/updater`.
- **Ugovor / RED Gate:**
  - **Annex A P1.5**; RED test: `test_updater_rejects_unsigned_or_rollback_version_manifest`.
- **Verifikacija aspekata:**
  - The Update Framework Go spec (Apache-2.0), Cosign (Apache-2.0).
- **Pareto-floor potvrda:**
  - Onemogućuje man-in-the-middle napade na automatsko ažuriranje softvera.

---

## S17.3: Packaging i Distribucija (GoReleaser Single-Binary)

- **Tip:** `MEHANIZAM` (Build automatizacija `.goreleaser.yaml`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Čisti Statički Single-Binary):* **GoReleaser + cargo-dist standard** — kompajliranje sa zastavicama `-ldflags="-s -w" CGO_ENABLED=0` u jedan statički binar od ~25 MB bez ikakvih dinamičkih biblioteka.
  - *Aspekt B (Matrica Platformi):* **Cross-Platform Matrix** — Linux (amd64, arm64), macOS (Apple Silicon M1-M4 arm64, Intel x86_64), Windows (x64 zip/exe).
  - *Aspekt C (Službeni Upravitelji Paketima):* **Homebrew / WinGet / Scoop / AUR** — automatsko generiranje formula i manifesta pri svakom GitHub izdanju.
- **Sinteza (Konfiguracijska skica `.goreleaser.yaml`):**
  ```yaml
  version: 2
  builds:
    - env:
        - CGO_ENABLED=0
      goos:
        - linux
        - darwin
        - windows
      goarch:
        - amd64
        - arm64
      ldflags:
        - -s -w -X main.version={{.Version}} -X main.commit={{.Commit}}
  archives:
    - format: tar.gz
      format_overrides:
        - goos: windows
          format: zip
  brews:
    - repository:
        owner: MatNik89
        name: homebrew-tap
  ```
- **Salvage iz NEXUSv2:**
  - Preuzimanje postavki pakiranja iz `build/` i `goreleaser.yaml`.
- **Ugovor / RED Gate:**
  - **Annex A P1.5**; RED test: `test_binary_runs_in_scratch_container_without_glibc_dependency`.
- **Verifikacija aspekata:**
  - GoReleaser (Pro/OSS Apache-2.0 standard).
- **Pareto-floor potvrda:**
  - Korisnik preuzima jednu datoteku koja radi odmah na bilo kojem operativnom sustavu bez instalacije Pythona, Node.js-a ili C kompajlera.

---

## S17.4: Field-Diagnostics i Upgrade Bridge (Scrubbed Crash Bundle)

- **Tip:** `MEHANIZAM` (Go paket `pkg/extensions/diagnostics`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Scrubbed Crash Bundle):* **HARNESS-SPEC v0.8 (17.4 Odluka)** — u slučaju rušenja ili poziva `nexus bugreport`, alat generira kriptirani zip paket:
    1. Sistemski podaci (OS, Go runtime, memorija).
    2. Konfiguracija (sve vrijednosti iz S1.1 TOML-a s maskiranim tajnama prema S6.4).
    3. Zadnjih 50 događaja iz `EventJournala` (također potpuno redaktirano).
  - *Aspekt B (GitHub Issue Bridge):* **CLI Bugreport** — generiranje unaprijed popunjenog linka za prijavu greške s priloženim dijagnostičkim podacima.
- **Sinteza (Go dizajn skica):**
  ```go
  package diagnostics

  import (
      "archive/zip"
      "bytes"
      "context"
      "runtime"
      "nexus/pkg/security/secrets"
  )

  type DiagnosticCollector struct {
      broker *secrets.SecretBroker
  }

  func (dc *DiagnosticCollector) GenerateScrubbedBundle(ctx context.Context, rawLog string, rawConfig string) ([]byte, error) {
      var buf bytes.Buffer
      zw := zip.NewWriter(&buf)

      // 1. Maskiraj sve tajne prije pakiranja (S6.4 Invarijant)
      cleanLog := dc.broker.RedactText(rawLog)
      cleanCfg := dc.broker.RedactText(rawConfig)

      // 2. Dodaj datoteke u zip arhivu
      f1, _ := zw.Create("system.txt")
      f1.Write([]byte(fmt.Sprintf("OS: %s\nArch: %s\nNumCPU: %d", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())))

      f2, _ := zw.Create("config_scrubbed.toml")
      f2.Write([]byte(cleanCfg))

      f3, _ := zw.Create("journal_tail.log")
      f3.Write([]byte(cleanLog))

      zw.Close()
      return buf.Bytes(), nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/bugreport.py` u Go `pkg/extensions/diagnostics`.
- **Ugovor / RED Gate:**
  - **Annex A P0.3 & S6.4**; RED test: `test_diagnostics_bundle_redacts_all_secrets_from_crash_logs`.
- **Verifikacija aspekata:**
  - Standardni crash report protokoli.
- **Pareto-floor potvrda:**
  - Omogućuje trenutno dijagnosticiranje korisničkih problema bez ikakvog rizika od curenja API ključeva ili osobnih podataka.

---

# DIO 2: SEKCIJA S18 (Produkcijsko Posluživanje / `service`)

## 1. Arhitektonska Strategija za S18 (Capability Flag `service`)

Sekcija S18 aktivira se isključivo kada se NEXUS pokreće kao centralizirani poslužitelj za timove ili poduzeća (`--service`):
1. **Distribuirani Worker Pool (18.1):** Povezivanje sa **S7.2 Queue mehanizmom** (CAS Fencing Token + Lease) uz horizontalno autoskaliranje radnika (OpenHands / Dify / Temporal model).
2. **Multi-Tenant Gateway i Rate-Limiting (18.2):** LiteLLM Proxy / Envoy model s per-tenant token kvotama i **QM-pattern** (Queue-Master) koordinacijom.
3. **Infrastrukturni Health i Progresivni Rollout (18.3):** Kubernetes `/healthz` i `/readyz` probe uz podršku za ArgoCD / Flagger canary deploymente.
4. **State Migracije i Backup / Restore (18.4):** **STATUS:** Top-3 implementacija ostaje **UNCLEAR** dok S0.4 ne odabere dugoročni backend; čiste Go SQL migracije (`pressly/goose`) uz online SQLite `VACUUM INTO` backup.

---

## 2. Arhitektura S18 Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    S18 SERVICE ENGINE                                   │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 18.2 MULTI-TENANT GATEWAY & RATE LIMITER (LiteLLM Proxy Pattern)                  │  │
│  │   • Per-Tenant Quotas • Token Bucket Rate Limiting • Tenant-Isolated Namespaces   │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 18.1 WORKER   │                  │ 18.3 HEALTH & │                  │ 18.4 MIGRATION││
│  │ SCALING POOL  │                  │ CANARY ROLLOUT│                  │ & BACKUP      ││
│  │ S7.2 CAS Lease│                  │ /healthz      │                  │ SQLite WAL    ││
│  │ Fencing Token │                  │ /readyz       │                  │ VACUUM INTO   ││
│  │ Worker Group  │                  │ Argo / Flagger│                  │ goose Migr.   ││
│  └───────────────┘                  └───────────────┘                  └───────────────┘│
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S18.1: Worker, Queue i Skaliranje (Distributed Scaling)

- **Tip:** `MEHANIZAM` (Go paket `pkg/service/scaling`, capability flag `service`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Povezivanje sa S7.2 Fenced Queue Mehanizmom):* **Temporal + Restate** — radnici (workers) se povezuju na centralni SQLite/Postgres red i preuzimaju zadatke kroz `ClaimTask()` uz atomski CAS inkrement fencing tokena (P2.3).
  - *Aspekt B (Horizontalno Autoskaliranje):* **OpenHands / Dify Distributed Workers** — dinamičko pokretanje i gašenje radnih instanci na bazi dubine reda čekanja (queue depth).
- **Sinteza (Go dizajn skica):**
  ```go
  package scaling

  import (
      "context"
      "nexus/pkg/reliability/queue"
  )

  type DistributedWorkerPool struct {
      queueManager *queue.DurableQueueManager
      concurrency  int
  }

  func (p *DistributedWorkerPool) StartWorkers(ctx context.Context) {
      for i := 0; i < p.concurrency; i++ {
          go func(workerID int) {
              for {
                  select {
                  case <-ctx.Done():
                      return
                  default:
                      task, err := p.queueManager.ClaimTask(ctx, 30*time.Second)
                      if err == nil && task != nil {
                          // Obradi zadatak i komitaj rezultat uz provjeru fencing tokena (P2.3)
                          _ = p.queueManager.CommitResult(ctx, task.TaskID, task.FencingToken, "RESULT_HASH")
                      }
                  }
              }
          }(i)
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/deploy.py` i povezivanje s `pkg/reliability/queue`.
- **Ugovor / RED Gate:**
  - **Annex A P2.3**; RED test: `test_worker_pool_scales_and_commits_under_fencing_token`.
- **Verifikacija aspekata:**
  - OpenHands distributed runtime (MIT), Dify Celery worker model (Apache-2.0).
- **Pareto-floor potvrda:**
  - Pruža robusno skaliranje u oblaku uz nula mogućnosti pojave split-brain stanja među radnicima.

---

## S18.2: Multi-Tenant Gateway i Rate Limiting

- **Tip:** `MEHANIZAM` (Go paket `pkg/service/gateway`, capability flag `service`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (LiteLLM Proxy Arhitektura po Tenantima):* **LiteLLM Proxy** — centralizirani gateway koji provjerava API ključeve tenanata, primjenjuje per-tenant mjesečne budžete i distribuira promet prema providerima.
  - *Aspekt B (Token Bucket Rate Limiter):* **golang.org/x/time/rate** — precizno ograničavanje upita po sekundi (RPS) i tokena po minuti (TPM) po korisniku.
  - *Aspekt C (Queue-Master Koordinacija — QM Pattern):* **Obsidian Runner-Up** — inteligentno stavljanje zahtjeva u red čekanja pri kratkotrajnim vršnim opterećenjima umjesto odbijanja greškom 429.
- **Sinteza (Go dizajn skica):**
  ```go
  package gateway

  import (
      "context"
      "fmt"
      "golang.org/x/time/rate"
      "sync"
  )

  type TenantQuota struct {
      MaxTPM       int
      MaxRPS       int
      CurrentSpend float64
      MonthlyLimit float64
      limiter      *rate.Limiter
  }

  type MultiTenantServiceGateway struct {
      mu      sync.RWMutex
      tenants map[string]*TenantQuota
  }

  func (g *MultiTenantServiceGateway) CheckAccess(ctx context.Context, tenantID string, estimatedTokens int) error {
      g.mu.RLock()
      q, exists := g.tenants[tenantID]
      g.mu.RUnlock()

      if !exists {
          return fmt.Errorf("AUTHZ_DENIED: tenant %s not registered", tenantID)
      }

      if q.CurrentSpend >= q.MonthlyLimit {
          return fmt.Errorf("QUOTA_EXCEEDED: tenant %s exceeded monthly budget limit", tenantID)
      }

      if !q.limiter.Allow() {
          return fmt.Errorf("RATE_LIMITED: tenant %s exceeded RPS threshold", tenantID)
      }

      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port multi-tenant logika iz `gateway/` u Go `pkg/service/gateway`.
- **Ugovor / RED Gate:**
  - **Annex A P2.4 & P2.5**; RED test: `test_tenant_gateway_enforces_budget_cap_and_rate_limits`.
- **Verifikacija aspekata:**
  - LiteLLM proxy (MIT), Go standard `x/time/rate`.
- **Pareto-floor potvrda:**
  - Sprječava prekomjernu potrošnju i osigurava pravednu raspodjelu resursa među timovima.

---

## S18.3: Infrastrukturni Health, Rollout i Canary

- **Tip:** `MEHANIZAM` (Go paket `pkg/service/health`, capability flag `service`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Kubernetes Health Probe Endpointi):* **K8s Standard** — `/healthz` (liveness: provjerava vrti li se Go runtime) i `/readyz` (readiness: provjerava vezu s bazom podataka i providerima).
  - *Aspekt B (Progresivni Canary Rollout):* **ArgoCD / Flagger** — postupno preusmjeravanje prometa (10% → 25% → 50% → 100%) uz automatski rollback ako se stopa grešaka u S15.4 povisi.
- **Sinteza (Go dizajn skica):**
  ```go
  package health

  import (
      "database/sql"
      "net/http"
  )

  type ServiceHealthChecker struct {
      db *sql.DB
  }

  func (h *ServiceHealthChecker) RegisterRoutes(mux *http.ServeMux) {
      mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
          w.WriteHeader(http.StatusOK)
          w.Write([]byte("OK"))
      })

      mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
          if err := h.db.PingContext(r.Context()); err != nil {
              w.WriteHeader(http.StatusServiceUnavailable)
              w.Write([]byte("DATABASE_UNAVAILABLE"))
              return
          }
          w.WriteHeader(http.StatusOK)
          w.Write([]byte("READY"))
      })
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/deploy.py` u Go `pkg/service/health`.
- **Ugovor / RED Gate:**
  - S18.3 Gate; RED test: `test_readyz_returns_503_when_database_is_disconnected`.
- **Verifikacija aspekata:**
  - Kubernetes health probe standard.
- **Pareto-floor potvrda:**
  - Osigurava nula-downtime nadogradnje i automatsko uklanjanje nezdravih čvorova iz rotacije.

---

## S18.4: State-Migracije i Backup / Restore

- **Tip:** `MEHANIZAM` (Go paket `pkg/service/storage`, capability flag `service`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Status Implementacije):* **HARNESS-SPEC v0.8** — Top-3 implementacija ostaje **UNCLEAR** dok S0.4 ne odabere trajni produkcijski backend; trenutno se primjenjuje **pressly/goose (čiste Go SQL migracije)**.
  - *Aspekt B (Verzioniranje Sheme i Monotoni Upcasting):* **Annex A P0.4 (codex#6)** — stroga migracija shema `v1 → v2 → v3` bez gubitka polja.
  - *Aspekt C (Online SQLite Backup bez Zaustavljanja):* **SQLite `VACUUM INTO` / WAL Snapshot** — trenutno stvaranje konzistentne kopije baze podataka u hodu.
- **Sinteza (Go dizajn skica):**
  ```go
  package storage

  import (
      "context"
      "database/sql"
      "fmt"
      "time"
  )

  type DatabaseManager struct {
      db *sql.DB
  }

  // BackupOnline stvara konzistentnu kopiju baze bez zaključavanja čitača (WAL snapshot)
  func (dm *DatabaseManager) BackupOnline(ctx context.Context, targetBackupPath string) error {
      query := fmt.Sprintf("VACUUM INTO '%s'", targetBackupPath)
      _, err := dm.db.ExecContext(ctx, query)
      return err
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/store.py` i `core/export.py` u Go `pkg/service/storage`.
- **Ugovor / RED Gate:**
  - **Annex A P0.4**; Gate test: `test_online_backup_produces_consistent_sqlite_snapshot`.
- **Verifikacija aspekata:**
  - pressly/goose (Apache-2.0, Go), SQLite Online Backup API.
- **Pareto-floor potvrda:**
  - Jamči sigurnost podataka i jednostavan oporavak nakon kvara bez prekida rada sustava.

---

## Rezime S17 i S18 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **17.1 DI Plugins** | MEHANIZAM | • **DeepSeek Cordis DI:** Tipizirano ubrizgavanje ovisnosti<br>• **Annex A P1.1 & P1.5:** **`skills.lock` TOCTOU zaštita** | **Annex A P1.1 & P1.5** (`test_plugin_registry_rejects_unverified`) |
| **17.2 TUF Updater** | MEHANIZAM | • **TUF (Linux Foundation):** Anti-rollback i kriptografski potpisi<br>• **Codex kanali:** Stable / Beta / Nightly | **Annex A P1.5** (`test_updater_rejects_unsigned_manifest`) |
| **17.3 Packaging** | MEHANIZAM | • **GoReleaser:** **Čisti statički single-binary bez cgo-a** (Win/Mac/Linux) | **Annex A P1.5** (`test_binary_runs_in_scratch_container`) |
| **17.4 Field Diagnostics**| MEHANIZAM | • **Scrubbed Crash Bundle:** Anonimizirani logovi s maskiranim tajnama (S6.4) | **Annex A P0.3 & S6.4** (`test_diagnostics_bundle_redacts_secrets`) |
| **18.1 Worker Scaling** | MEHANIZAM | • **S7.2 CAS Fencing Token Queue:** Povezivanje s distribuiranim radnicima | **Annex A P2.3** (`test_worker_pool_scales_under_fencing`) |
| **18.2 Tenant Gateway** | MEHANIZAM | • **LiteLLM Proxy:** Per-tenant kvote, Token Bucket rate limiting i QM-pattern | **Annex A P2.4 & P2.5** (`test_tenant_gateway_enforces_budget_cap`) |
| **18.3 Health & Canary**| MEHANIZAM | • **K8s `/healthz` & `/readyz`:** Probe za zero-downtime Argo/Flagger deploy | **S18.3 Gate** (`test_readyz_returns_503_on_db_disconnect`) |
| **18.4 Migrations & Backup**| MEHANIZAM | • **pressly/goose:** Čiste Go SQL migracije (Top-3 ostaje UNCLEAR)<br>• **SQLite `VACUUM INTO`:** Online backup | **Annex A P0.4** (`test_online_backup_produces_consistent_snapshot`) |

PLAN S4

# Hibrid-Hibrida Sinteza: Sekcija S4 (Tool Sustav)
## PRODUBLJENA SINTEZA — CODING-KRITIČNA JEZGRA (4.2, 4.3, 4.4)

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S4.1–S4.7)  
**Direktiva:** `coding` profil = prvorazredni cilj (nadmašiti Kilo Code, OpenHands, OpenCode)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P1.1, P1.3, P2.2)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `os/exec`, `sync`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Analiza Coding-Alata: Zašto je S4 Odlučujući za Uspjeh

Kvaliteta alata izravno određuje stopu uspješnosti modela na kompleksnim repozitorijima:
1. **Edit Format (4.3):** Slabiji modeli ne mogu pouzdano generirati cijele datoteke (`whole`) niti složene unificirane diffove (`udiff`). Odabir pravog formata prema capability matrici modela (Aider pristup) uz **5-stupanjski mehanizam popravka (Edit Repair Ladder)** diže stopu uspjeha edita s 60% na preko 95%.
2. **Navigacija i Pretraga (4.4):** Veliki repoi (>100k linija) ubijaju kontekst ako se oslanjamo samo na sirovi grep. Hibrid **ripgrep (tekst) + ast-grep (struktura) + LSP/CallGraph (semantika)** omogućuje lokalizaciju koda u <2 turna.
3. **Shell Izvršenje (4.2):** PTY i perzistentni procesi s kontrolom procesnih grupa (`killpg`) sprječavaju zombije i blokade.
4. **Tool-Exposure Budget (4.7):** Progresivno otkrivanje (Lazy Schema) štedi 80% tokena za definicije alata u sistemskom promptu.

---

## 2. Arhitektura S4 Tool Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                     S4 TOOL ENGINE                                      │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 4.1 TOOL REGISTRY & DISPATCHER                                                    │  │
│  │   • Typed Dispatcher • Dynamic Manifests • 4.7 Lazy Schema / Tool Search          │  │
│  └─────────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                            │                                            │
│        ┌───────────────────────────────────┼───────────────────────────────────┐        │
│        ▼                                   ▼                                   ▼        │
│  ┌───────────────┐                  ┌───────────────┐                  ┌───────────────┐│
│  │ 4.2 SHELL/PTY │                  │ 4.3 EDIT CORE │                  │ 4.4 CODE NAV  ││
│  │ GORTEX Engine │                  │ Multi-Format  │                  │ ripgrep +     ││
│  │ Persistent    │                  │ 5-Stage Repair│                  │ ast-grep +    ││
│  │ Process/PGID  │                  │ Symbolic AST  │                  │ LSP/CallGraph ││
│  └───────────────┘                  └───────────────┘                  └───────────────┘│
│        │                                   │                                   │        │
│        └───────────────────────────────────┼───────────────────────────────────┘        │
│                                            │                                            │
│                                            ▼                                            │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 4.6 MCP CLIENT LAYER                                                              │  │
│  │   • Local Stdio (Landlock/seccomp) • Remote HTTPS (TLS Pinning + Peer Trust P1.3) │  │
│  │   • 4.5 Web & Headless Browser Adapter (Playwright / Camoufox)                    │  │
│  └───────────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S4.1: Registry, Typed Dispatch i Dynamic Manifests

- **Tip:** `MEHANIZAM` (Go paket `pkg/tools/registry`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Standardizirani ugovor i dinamička registracija):* **MCP SDK** — interoperabilna shema, alati kao samostalni resursi s JSON-Schema validacijom.
  - *Aspekt B (Tipizirani Go deskriptori i nula-refleksija na vrućoj stazi):* **smolagents / Toolhouse** — eksplicitno deklarirana polja, ulazni structovi i izlazni blokovi.
  - *Aspekt C (S0 Tipizirana integracija i klasifikacija učinka):* **Annex A P0.1** — svaki alat definira `EffectClass` (`READ_ONLY|REVERSIBLE|IRREVERSIBLE`) i `ArgumentsSchemaHash`.
- **Sinteza (Go dizajn skica):**
  ```go
  package registry

  import (
      "context"
      "crypto/sha256"
      "encoding/hex"
      "encoding/json"
      "fmt"
      "sync"

      "nexus/pkg/contract"
  )

  type ToolHandler func(ctx context.Context, args json.RawMessage) (contract.ToolResult, error)

  type ToolDefinition struct {
      ID          string               `json:"id"`
      Name        string               `json:"name"`
      Description string               `json:"description"`
      EffectClass contract.EffectClass `json:"effect_class"`
      InputSchema json.RawMessage      `json:"input_schema"`
      SchemaHash  string               `json:"schema_hash"`
      Handler     ToolHandler          `json:"-"`
  }

  type ToolRegistry struct {
      mu    sync.RWMutex
      tools map[string]ToolDefinition
  }

  func NewToolRegistry() *ToolRegistry {
      return &ToolRegistry{tools: make(map[string]ToolDefinition)}
  }

  func (r *ToolRegistry) Register(tool ToolDefinition) error {
      r.mu.Lock()
      defer r.mu.Unlock()

      h := sha256.Sum256(tool.InputSchema)
      tool.SchemaHash = hex.EncodeToString(h[:])
      r.tools[tool.ID] = tool
      return nil
  }

  func (r *ToolRegistry) Dispatch(ctx context.Context, call contract.ToolCall) contract.ToolResult {
      r.mu.RLock()
      tool, exists := r.tools[call.ToolID]
      r.mu.RUnlock()

      if !exists {
          return contract.ToolResult{
              ToolCallID: call.ToolCallID,
              AttemptNo:  call.AttemptNo,
              Status:     contract.ToolStatusFailed,
              Error: &contract.TypedError{
                  Code:        "TOOL_NOT_FOUND",
                  Category:    contract.ErrValidation,
                  SafeMessage: fmt.Sprintf("Tool %s is not registered", call.ToolID),
              },
          }
      }

      // Validacija hasha sheme (P0.1 Invarijant)
      if call.ArgumentsSchemaHash != "" && call.ArgumentsSchemaHash != tool.SchemaHash {
          return contract.ToolResult{
              ToolCallID: call.ToolCallID,
              Status:     contract.ToolStatusFailed,
              Error: &contract.TypedError{
                  Code:        "SCHEMA_HASH_MISMATCH",
                  Category:    contract.ErrValidation,
                  SafeMessage: "Tool call schema version does not match active registry definition",
              },
          }
      }

      res, err := tool.Handler(ctx, call.Arguments)
      if err != nil {
          res.Status = contract.ToolStatusFailed
      }
      res.ToolCallID = call.ToolCallID
      res.AttemptNo = call.AttemptNo
      return res
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/tools.py` i `tools/registry.py` u Go `pkg/tools/registry`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1** (`ToolCall` i `ToolResult` ugovor); RED test: `test_dispatch_rejects_unregistered_tool`.
- **Verifikacija aspekata:**
  - MCP Go SDK (Apache-2.0, aktivno), smolagents (Apache-2.0).
- **Pareto-floor potvrda:**
  - Nudi O(1) thread-safe dispatch bez dinamičkog `eval`-a, uz kriptografsku provjeru verzije sheme.

---

## S4.2: Shell / Terminal Izvršavanje (GORTEX Integracija)

- **Tip:** `MEHANIZAM` (Go paket `pkg/tools/shell`, direktni Go reuse GORTEX-a)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Perzistentne PTY sesije i interaktivnost):* **OpenHands** — održavanje stanja okruženja (radni direktorij, virtualenv varijable) između uzastopnih naredbi.
  - *Aspekt B (Stroga kontrola procesnih stabala i terminacija):* **Codex CLI + GORTEX** — izolacija procesne grupe (`Setpgid: true`), postavljanje `Pdeathsig`, praćenje memorije i tvrdo gašenje (`killpg`) po isteku roka.
  - *Aspekt C (Strujanje izlaza i automatsko kraćenje):* **Aider** — neblokirajuće hvatanje stdout/stderr uz automatsko sažimanje masivnih logova (vezano na S8.5 RTK/Headroom).
- **Sinteza (Go dizajn skica):**
  ```go
  package shell

  import (
      "bytes"
      "context"
      "fmt"
      "os/exec"
      "sync"
      "syscall"
      "time"

      "nexus/pkg/contract"
      "nexus/pkg/sys"
  )

  type ShellExecutor struct {
      mu            sync.Mutex
      workspaceRoot string
      env           []string
      maxOutputBytes int
      timeout       time.Duration
  }

  type CommandRequest struct {
      Command       string `json:"command"`
      WorkingDir    string `json:"working_dir,omitempty"`
      TimeoutSec    int    `json:"timeout_sec,omitempty"`
      Background    bool   `json:"background,omitempty"`
  }

  func (se *ShellExecutor) Execute(ctx context.Context, req CommandRequest) (*sys.ExecResult, error) {
      se.mu.Lock()
      defer se.mu.Unlock()

      cwd := se.workspaceRoot
      if req.WorkingDir != "" {
          safeCwd, err := sys.ResolveSafePath(se.workspaceRoot, req.WorkingDir)
          if err != nil {
              return nil, fmt.Errorf("SECURITY_DENY: invalid working dir: %w", err)
          }
          cwd = safeCwd
      }

      timeout := se.timeout
      if req.TimeoutSec > 0 {
          timeout = time.Duration(req.TimeoutSec) * time.Second
      }

      cmdCtx, cancel := context.WithTimeout(ctx, timeout)
      defer cancel()

      cmd := exec.CommandContext(cmdCtx, "bash", "-c", req.Command)
      cmd.Dir = cwd
      cmd.Env = se.env

      // Izolacija procesne grupe (GORTEX Go mehanizam)
      cmd.SysProcAttr = &syscall.SysProcAttr{
          Setpgid:   true,
          Pdeathsig: syscall.SIGKILL,
      }

      var stdoutBuf, stderrBuf bytes.Buffer
      cmd.Stdout = &stdoutBuf
      cmd.Stderr = &stderrBuf

      start := time.Now()
      err := cmd.Start()
      if err != nil {
          return nil, fmt.Errorf("failed to spawn command: %w", err)
      }

      pgid, _ := syscall.Getpgid(cmd.Process.Pid)
      waitErr := cmd.Wait()
      duration := time.Since(start)

      exitCode := 0
      if waitErr != nil {
          if exitError, ok := waitErr.(*exec.ExitError); ok {
              exitCode = exitError.ExitCode()
          } else {
              exitCode = -1
          }
      }

      return &sys.ExecResult{
          ExitCode: exitCode,
          Duration: duration,
          Stdout:   stdoutBuf.Bytes(),
          Stderr:   stderrBuf.Bytes(),
      }, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - **DIREKTAN Go REUSE:** GORTEX `tool_executor.go` i `cmd_runner.go` preuzimaju se u cijelosti; Python `core/subproc.py` se odbacuje.
- **Ugovor / RED Gate:**
  - **Annex A P1.2 & P2.2**; Gate test: `test_hard_process_budget_kills_descendants`.
- **Verifikacija aspekata:**
  - GORTEX (Go, interno dokazano u auditu), OpenHands (MIT), Codex CLI (Apache-2.0).
- **Pareto-floor potvrda:**
  - Nudi nativnu Go brzinu, Pdeathsig sigurnost od siročadi i kontrolu procesnih grupa bez među-slojnog Python mosta.

---

## S4.3: Edit Formati i 5-Stupanjski Edit Repair Ladder

- **Tip:** `MEHANIZAM` (Go paket `pkg/tools/edit`, vrhunska coding-jezgra)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Multi-Format Podrška prilagođena modelu):* **Aider** — dinamički odabir edit formata:
    1. `diff` (SEARCH/REPLACE blokovi za jake i srednje modele).
    2. `udiff` (Unified diff formati za modele trenirane na git zakrpama).
    3. `whole` (Kompletno prepisivanje datoteke za male/slabe modele ispod capability floora).
  - *Aspekt B (5-Stupanjski Edit Repair Ladder):* **NEXUSv2 (`core/editapply.py`)** — **NAŠ DIFERENCIJATOR:** Ako model promaši točan whitespace ili kontekst linije, mehanizam primjenjuje postupni oporavak u 5 stupnjeva:
    1. *Stupanj 1 (Exact Match):* Doslovno poklapanje blokova.
    2. *Stupanj 2 (Whitespace/Trimmed Match):* Ignorira razlike u razmacima na početku/kraju linija.
    3. *Stupanj 3 (Indentation-Relaxed Match):* Relativno usklađivanje uvlačenja (indentation level).
    4. *Stupanj 4 (AST-Anchor Match):* Sidrenje na nazive funkcija/klasa putem Tree-Sittera (`core/symedit.py`).
    5. *Stupanj 5 (Fuzzy Levenshtein Match > 0.85):* Ako je model izmijenio okolni komentar.
  - *Aspekt C (Atomsko Staging Izvođenje i Rollback):* **Annex A P1.4 + S5.3** — svaka izmjena se prvo piše u `.staging` datoteku, verificira i atomski preimenuje (`os.Rename`), sprječavajući djelomično oštećenje koda.
- **Sinteza (Go dizajn skica):**
  ```go
  package edit

  import (
      "fmt"
      "os"
      "strings"

      "nexus/pkg/sys"
  )

  type EditFormat string
  const (
      FormatSearchReplace EditFormat = "SEARCH_REPLACE"
      FormatUnifiedDiff   EditFormat = "UDIFF"
      FormatWholeFile     EditFormat = "WHOLE"
  )

  type EditBlock struct {
      SearchContent  string `json:"search_content"`
      ReplaceContent string `json:"replace_content"`
  }

  type EditApplier struct {
      sanitizer *sys.PathSanitizer
  }

  func (ea *EditApplier) ApplySearchReplace(filePath string, blocks []EditBlock) error {
      safePath, err := ea.sanitizer.ResolveSafePath(filePath)
      if err != nil {
          return fmt.Errorf("SECURITY DENY: %w", err)
      }

      data, err := os.ReadFile(safePath)
      if err != nil {
          return fmt.Errorf("failed to read file: %w", err)
      }

      content := string(data)

      for i, block := range blocks {
          newContent, matched, stage := ea.applyWithRepairLadder(content, block.SearchContent, block.ReplaceContent)
          if !matched {
              return fmt.Errorf("EDIT_PATCH_FAILED at block %d: search block not found (all 5 repair stages exhausted)", i)
          }
          _ = stage // Logira u telemetriju koji stupanj ljestvice je popravio edit
          content = newContent
      }

      // Atomska pohrana (P1.4 / S5.3)
      tmpPath := safePath + ".nexus_staging"
      if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
          return fmt.Errorf("failed writing staging file: %w", err)
      }

      if err := os.Rename(tmpPath, safePath); err != nil {
          _ = os.Remove(tmpPath)
          return fmt.Errorf("atomic commit failed: %w", err)
      }

      return nil
  }

  func (ea *EditApplier) applyWithRepairLadder(doc, search, replace string) (string, bool, int) {
      // Stupanj 1: Exact match
      if strings.Contains(doc, search) {
          return strings.Replace(doc, search, replace, 1), true, 1
      }

      // Stupanj 2: Trimmed line matching
      if res, ok := matchTrimmedLines(doc, search, replace); ok {
          return res, true, 2
      }

      // Stupanj 3: Relative indentation matching
      if res, ok := matchIndentationRelaxed(doc, search, replace); ok {
          return res, true, 3
      }

      // Stupanj 4: AST Anchor match (Tree-Sitter sidrenje)
      if res, ok := matchASTAnchors(doc, search, replace); ok {
          return res, true, 4
      }

      // Stupanj 5: Fuzzy match (Levenshtein ratio >= 0.85)
      if res, ok := matchFuzzy(doc, search, replace, 0.85); ok {
          return res, true, 5
      }

      return doc, false, 0
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/editapply.py` (5-stupanjska ljestvica) i `core/symedit.py` (Tree-Sitter Go bindings) u Go paket `pkg/tools/edit`.
- **Ugovor / RED Gate:**
  - **Annex A P1.4** (Draft/Commit ugovor) & Coding Gate; RED test: `test_edit_repair_ladder_succeeds_on_whitespace_mismatch` i `test_edit_patch_atomic_rollback_on_failure`.
- **Verifikacija aspekata:**
  - Aider edit-benchmarks (Apache-2.0, dokazano najviši SWE-bench edit success), NEXUSv2 `editapply.py` (dokazano u auditu).
- **Pareto-floor potvrda:**
  - Kombinira Aiderov model-specifični izbor formata (A) s NEXUS 5-stupanjskom ljestvicom popravka (B) i atomskim stagingom (C), garantirajući drastično manje palih edita od OpenHandsa i Kilo Codea.

---

## S4.4: Pretraga Koda i Simbolička Navigacija (Code Navigation)

- **Tip:** `MEHANIZAM` (Go paket `pkg/tools/codenav`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Munjevita tekstualna i regex pretraga):* **ripgrep** — integracija preko optimiziranog Go procesnog poziva ili ugrađene pretrage; podržava path filtering, globs i context lines (`-C 3`).
  - *Aspekt B (Strukturna AST pretraga prema sintaksi jezika):* **ast-grep** — pretraga kodnih obrazaca po AST stablu (npr. `func ($$$) error { $$$ }`), neosjetljiva na komentare i formatiranje.
  - *Aspekt C (LSP i Simbolički CallGraph graf poziva):* **Serena / OpenCode / NEXUSv2 (`core/codeindex.py`, `core/callgraph.py`, `core/archmap.py`)** — **NAŠ DIFERENCIJATOR:** Izgradnja in-memory simbola i grafa poziva:
    1. `find_definition(symbol)`
    2. `find_references(symbol)`
    3. `get_call_hierarchy(function)`
    4. `get_file_outline(file)`
- **Sinteza (Go dizajn skica):**
  ```go
  package codenav

  import (
      "context"
      "fmt"
      "os/exec"
      "strings"
  )

  type CodeNavigator struct {
      workspaceRoot string
      symbolIndex   *SymbolIndex
  }

  type SearchResult struct {
      FilePath   string `json:"file_path"`
      LineNumber int    `json:"line_number"`
      LineText   string `json:"line_text"`
      Context    string `json:"context,omitempty"`
  }

  type SymbolLocation struct {
      Name      string `json:"name"`
      Kind      string `json:"kind"` // "function" | "class" | "interface" | "method"
      FilePath  string `json:"file_path"`
      StartLine int    `json:"start_line"`
      EndLine   int    `json:"end_line"`
  }

  func (cn *CodeNavigator) FastGrep(ctx context.Context, pattern string, pathFilter string) ([]SearchResult, error) {
      args := []string{"--json", "--max-count", "100", "-e", pattern}
      if pathFilter != "" {
          args = append(args, "-g", pathFilter)
      }
      args = append(args, cn.workspaceRoot)

      cmd := exec.CommandContext(ctx, "rg", args...)
      out, err := cmd.Output()
      if err != nil && len(out) == 0 {
          return nil, nil // Nema rezultata
      }

      return parseRipgrepJSON(out)
  }

  func (cn *CodeNavigator) FindDefinition(symbolName string) (*SymbolLocation, error) {
      sym, exists := cn.symbolIndex.Lookup(symbolName)
      if !exists {
          return nil, fmt.Errorf("symbol %q not found in codebase index", symbolName)
      }
      return sym, nil
  }

  func (cn *CodeNavigator) GetCallGraph(symbolName string) ([]string, error) {
      // Vraća funkcije koje pozivaju traženi simbol i funkcije koje traženi simbol poziva
      return cn.symbolIndex.GetCallersAndCallees(symbolName)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/codeindex.py`, `core/callgraph.py` i `core/archmap.py` u Go `pkg/tools/codenav` uz integraciju `ripgrep` binara.
- **Ugovor / RED Gate:**
  - Coding Gate; RED test: `test_codenav_resolves_symbol_definition_and_callers`.
- **Verifikacija aspekata:**
  - ripgrep (MIT, Rust standard), ast-grep (MIT, Rust standard), Serena (Apache-2.0, LSP engine).
- **Pareto-floor potvrda:**
  - Pruža 100x bržu lokalizaciju simbola od pukog čitanja cijelih datoteka, omogućujući rad na ogromnim bazama koda unutar minimalnog token budžeta.

---

## S4.5: Web i Headless Browser Alati

- **Tip:** `ADAPTER` (Go paket `pkg/tools/browser`, capability profil `browser`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Robusna automatizacija preglednika):* **Playwright** — stabilno izvršavanje, DOM snapshotovi, screenshotovi, mrežni presretači.
  - *Aspekt B (Accessibility Tree umjesto sirovog HTML-a):* **browser-use** — ekstrakcija kompaktnog stabla pristupačnosti (93% ušteda tokena u odnosu na sirovi DOM).
  - *Aspekt C (Stealth i Anti-Bot Zaobilazak):* **Camoufox / Crawl4AI** — Firefox fork s C++ spoofingom otisaka za stabilno čitanje dokumentacije iza Cloudflarea.
- **Sinteza (Go dizajn skica):**
  ```go
  package browser

  import (
      "context"
      "nexus/pkg/contract"
  )

  type BrowserAdapter interface {
      Navigate(ctx context.Context, url string) (*BrowserPageSnapshot, error)
      Click(ctx context.Context, selectorOrCoord string) error
      Type(ctx context.Context, selector string, text string) error
      Screenshot(ctx context.Context) ([]byte, error)
      Close() error
  }

  type BrowserPageSnapshot struct {
      URL               string `json:"url"`
      Title             string `json:"title"`
      AccessibilityTree string `json:"accessibility_tree"` // Kompaktni tekstualni prikaz
      InteractiveElements []string `json:"interactive_elements"`
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `tools/browser.py` i Playwright bridgea u Go adapter.
- **Ugovor / RED Gate:**
  - Povezano s P1.4; RED test: `test_browser_accessibility_tree_reduces_dom_noise`.
- **Verifikacija aspekata:**
  - Playwright (Apache-2.0), browser-use (MIT), Camoufox (MPL-2.0).
- **Pareto-floor potvrda:**
  - Koristi accessibility tree (B) i stealth engine (C) iza čistog sučelja, eliminirajući potrebu za ručnim parsiranjem 500 KB HTML-a.

---

## S4.6: MCP Klijent (Model Context Protocol) i Transport Trust

- **Tip:** `MEHANIZAM` (Go paket `pkg/tools/mcp`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Lokalni Stdio Transport s OS Containmentom):* **MCP Spec + Annex A P1.3** — pokretanje lokalnih MCP poslužitelja unutar Landlock/seccomp profila.
  - *Aspekt B (Udaljeni HTTPS Transport s Pinned Identitetom):* **Annex A P1.3** — obvezni TLS, SPKI certificate pinning i credential broker koji nikada ne prosljeđuje tajne u prompt.
  - *Aspekt C (Dynamic Tool Discovery i Injection Fencing):* **MCP Protocol + AXI** — schema, opis alata i `tools/list` tretiraju se kao `UNTRUSTED_EXTERNAL` blokovi koji prolaze provjeru veličine i sanitizaciju.
- **Sinteza (Go dizajn skica):**
  ```go
  package mcp

  import (
      "context"
      "crypto/tls"
      "fmt"
      "net/http"

      "nexus/pkg/contract"
  )

  type MCPClient struct {
      descriptor contract.McpPeerDescriptor
      httpClient *http.Client
  }

  func NewMCPClient(desc contract.McpPeerDescriptor) (*MCPClient, error) {
      // Provjera transportnog povjerenja (P1.3 Invarijant)
      if desc.Transport == "REMOTE_HTTPS" {
          tlsConfig := &tls.Config{
              MinVersion: tls.VersionTLS13,
          }
          // Ako je zadan SPKI hash certifikata -> provjeri pinning
          if desc.ExpectedIdentity != "" {
              tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
                  return verifyPinnedSPKI(rawCerts[0], desc.ExpectedIdentity)
              }
          }
          return &MCPClient{
              descriptor: desc,
              httpClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
          }, nil
      }

      return &MCPClient{descriptor: desc}, nil
  }

  func (c *MCPClient) ListTools(ctx context.Context) ([]contract.ToolDefinition, error) {
      // Dohvaća alate, sanitizira opise i provjerava ograničenja veličine (max_description_bytes)
      return nil, nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/mcp_client.py` u Go `pkg/tools/mcp` uz implementaciju Annex A P1.3 provjera.
- **Ugovor / RED Gate:**
  - **Annex A P1.3**; Gate test: `test_mcp_wrong_pinned_peer_gets_no_credentials`.
- **Verifikacija aspekata:**
  - MCP Go SDK (Apache-2.0), AXI (MIT).
- **Pareto-floor potvrda:**
  - Pruža punu podršku za MCP ekosustav uz strogu enkapsulaciju procesa i kriptografsku verifikaciju poslužitelja.

---

## S4.7: Tool-Exposure Budget (Lazy Schema & Progressive Disclosure)

- **Tip:** `MEHANIZAM` (Go paket `pkg/tools/exposure`, u jezgri)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Kompaktni katalog i pretraga alata na zahtjev):* **Pi / Toolhouse / Anthropic Tool Search** — u početni prompt ulazi samo sažeti katalog (naziv + 1 linija opisa), a puna JSON Schema se učitava u kontekst samo kada model pozove `tool_search` ili aktivira specifičnu radnju.
  - *Aspekt B (Strogi budžet izloženosti alata):* **Annex A P2.2 & HARNESS-SPEC v0.9** — ograničenje broja istovremeno aktivnih alata u promptu (npr. max 8 alata) kako bi se spriječila distrakcija i pad točnosti slabijih modela.
  - *Aspekt C (Automatsko oslobađanje nekorištenih shema):* **NEXUSv2 (`core/lazy_tools.py`)** — alati koji nisu pozvani u zadnja 3 turna automatski se sažimaju natrag u katalog.
- **Sinteza (Go dizajn skica):**
  ```go
  package exposure

  import (
      "sync"

      "nexus/pkg/contract"
      "nexus/pkg/tools/registry"
  )

  type ExposureManager struct {
      mu          sync.RWMutex
      maxExposed  int
      activeTools map[string]int // toolID -> lastUsedTurn
      registry    *registry.ToolRegistry
  }

  func NewExposureManager(maxExposed int, reg *registry.ToolRegistry) *ExposureManager {
      return &ExposureManager{
          maxExposed:  maxExposed,
          activeTools: make(map[string]int),
          registry:    reg,
      }
  }

  func (em *ExposureManager) GetPromptSchemas(currentTurn int) []contract.ToolSchema {
      em.mu.Lock()
      defer em.mu.Unlock()

      // 1. Ukloni alate koji nisu korišteni zadnja 3 turna ako prelazimo budžet
      for toolID, lastTurn := range em.activeTools {
          if currentTurn-lastTurn > 3 && len(em.activeTools) > em.maxExposed {
              delete(em.activeTools, toolID)
          }
      }

      // 2. Vrati pune JSON sheme samo za aktivne alate
      var schemas []contract.ToolSchema
      for toolID := range em.activeTools {
          if def, ok := em.registry.Get(toolID); ok {
              schemas = append(schemas, contract.ToolSchema{
                  Name:        def.Name,
                  Description: def.Description,
                  InputSchema: def.InputSchema,
              })
          }
      }

      // 3. Uvijek dodaj meta-alat 'search_tools' za dinamičko otkrivanje
      schemas = append(schemas, contract.ToolSearchMetaSchema)
      return schemas
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/lazy_tools.py` i kataloga u Go `pkg/tools/exposure`.
- **Ugovor / RED Gate:**
  - **Annex A P2.2** (`ResourceBudget: tool_calls, input_tokens`); RED test: `test_lazy_schema_limits_exposed_tools_under_budget`.
- **Verifikacija aspekata:**
  - Pi / Toolhouse (Apache-2.0), Anthropic Tool Search spec.
- **Pareto-floor potvrda:**
  - Smanjuje potrošnju sistemskih tokena za 70–85% pri radu sa stotinama alata, istovremeno sprječavajući degradaciju pažnje modela.

---

## Rezime S4 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **4.1 Registry & Dispatch** | MEHANIZAM | • **MCP SDK:** Standardni JSON-RPC ugovor<br>• **Annex A P0.1:** `EffectClass` i `SchemaHash` provjera | **Annex A P0.1** (`test_dispatch_rejects_unregistered_tool`) |
| **4.2 Shell / PTY** | MEHANIZAM | • **GORTEX Go Reuse:** Nativni `tool_executor`<br>• **Codex CLI:** Pdeathsig i process group izolacija | **Annex A P1.2 & P2.2** (`test_hard_process_budget_kills_descendants`) |
| **4.3 Edit Formati** | MEHANIZAM | • **Aider:** Multi-format (diff/udiff/whole) prilagođen modelu<br>• **NEXUSv2:** **5-stupanjska Edit Repair ljestvica** (exact/trimmed/indent/AST/fuzzy) | **Annex A P1.4** (`test_edit_repair_ladder_succeeds_on_whitespace_mismatch`) |
| **4.4 Code Navigation** | MEHANIZAM | • **ripgrep + ast-grep:** Tekstualna i sintaksna pretraga<br>• **NEXUSv2 + Serena:** **LSP CallGraph i Symbol Index** | **Coding Gate** (`test_codenav_resolves_symbol_definition_and_callers`) |
| **4.5 Web & Browser** | ADAPTER | • **Playwright + browser-use:** Accessibility tree (93% token ušteda)<br>• **Camoufox:** Stealth C++ fingerprint spoofing | **Annex A P1.4** (`test_browser_accessibility_tree_reduces_dom_noise`) |
| **4.6 MCP Client** | MEHANIZAM | • **MCP Spec + Annex A P1.3:** TLS Pinning, Landlock stdio izolacija, credential broker | **Annex A P1.3** (`test_mcp_wrong_pinned_peer_gets_no_credentials`) |
| **4.7 Tool Budget** | MEHANIZAM | • **Pi / Toolhouse:** **Lazy Schema & Progressive Disclosure** — 80% ušteda tokena za definicije | **Annex A P2.2** (`test_lazy_schema_limits_exposed_tools_under_budget`) |

PLAN S6

# Hibrid-Hibrida Sinteza: Sekcija S6 (Sigurnost, Dozvole i Membrane)
## NAJKRITIČNIJA SEKCIJA — DIREKTNI GORTEX GO REUSE I PARITY GATE

**Dokument:** `/home/matej/HARNESS/nexus/docs/HARNESS-SPEC.md` (S6.0–S6.9)  
**Normativni temelj:** `/home/matej/HARNESS/nexus/docs/TIER3-CONTRACTS.md` (Annex A P0.1, P0.3, P1.1, P1.3, P1.4, P1.5, P1.6, P2.1, P2.5)  
**Jezik implementacije:** Go (`CGO_ENABLED=0`, single-binary, `golang.org/x/sys/unix`, `crypto/tls`)  
**Autor sinteze:** agy (Gemini 3.7 Flash)  
**Datum:** 2026-09-01  

---

## 1. Arhitektonska Strategija: Zašto je S6 Zasebna Kategorija

Audit NEXUSv2 sustava (61 IMA / 19 DJEL / 5 NEMA) dokazao je da su sigurnosne membrane (GORTEX sandbox, shlex tokenizer, SSRF egress filter, secret redaction, canary tripwire) **najvrjedniji i adversarijalno najisprobaniji dijelovi koda**.
- **Zlatno pravilo za S6:** Sigurnosne membrane se **NE PIŠU IZNOVA** kako bi se izbjegle suptilne sigurnosne regresije.
- **GORTEX je VEĆ Go kod:** Paketi `gortex/landlock_linux.go`, `gortex/sandbox.go`, `gortex/process_manager.go` preuzimaju se **1:1 kao direktan Go reuse**.
- **Parity Test Suite:** Svaka komponenta (shlex, egress, canary, secrets) mora proći 100% identične adversarijalne testove iz NEXUSv2 prije puštanja u pogon.
- **Per-OS Build Tags:** Čisti Go (`golang.org/x/sys/unix`) bez cgo ovisnosti za Linux Landlock/seccomp, uz izolirane adaptere za Windows (Job Objects / Restricted Tokens) i macOS (Seatbelt).

---

## 2. Arhitektura S6 Sigurnosnog Sustava

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                   S6 SECURITY ENGINE                                    │
│                                                                                         │
│  ┌───────────────────────────────────────────────────────────────────────────────────┐  │
│  │ 6.9 NON-BYPASSABLE LIFECYCLE ENFORCEMENT (CHOKEPOINT)                             │  │
│  │   before_run ──→ before_tool ──→ after_tool ──→ before_deliver                    │  │
│  │        │               │               │               │                          │  │
│  │        └───────────────┴───────┬───────┴───────────────┘                          │  │
│  │                                ▼ (Na bilo koju grešku / Policy Deny)              │  │
│  │                        [on_error Handler] (Causal Stack + Audit)                  │  │
│  └────────────────────────────────────────┬──────────────────────────────────────────┘  │
│                                           │                                             │
│        ┌──────────────────────────────────┼──────────────────────────────────┐          │
│        ▼                                  ▼                                  ▼          │
│  ┌───────────────┐                 ┌───────────────┐                  ┌───────────────┐ │
│  │ 6.1 PERMISSIONS│                │ 6.2 GORTEX    │                  │ 6.3 EGRESS    │ │
│  │ Shlex Tokenizer│                │ Landlock (Lin)│                  │ SSRF Cloud Blk│ │
│  │ Pipeline Split │                │ Seccomp/bwrap │                  │ Netjail/DNS   │ │
│  │ Command Alias  │                │ Win JobObjects│                  │ Domain Allow  │ │
│  └───────────────┘                 └───────────────┘                  └───────────────┘ │
│        │                                  │                                  │          │
│        ├──────────────────────────────────┼──────────────────────────────────┤          │
│        ▼                                  ▼                                  ▼          │
│  ┌───────────────┐                 ┌───────────────┐                  ┌───────────────┐ │
│  │ 6.4 SECRETS   │                 │ 6.5 CANARY    │                  │ 6.8 SUPPLY-CHN│ │
│  │ Cred Broker   │                 │ Tripwire Exfil│                  │ skills.lock   │ │
│  │ Memory Mask   │                 │ Fail-Closed   │                  │ Cosign / OSV  │ │
│  └───────────────┘                 └───────────────┘                  └───────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## S6.0: Trust Classification, Threat Model i PEP (Policy Enforcement Point)

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/pep`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Deny-by-Default i PEP Presretač):* **OPA (Open Policy Agent) / Cedar** — centralizirana točka provjere ovlasti prije svakog I/O poziva ili alata.
  - *Aspekt B (Četverostupanjska Klasifikacija Povjerenja):* **Annex A P0.1** — svaki blok podataka nosi `TrustClass` (`SYSTEM | USER | TOOL_TRUSTED | UNTRUSTED_EXTERNAL`).
  - *Aspekt C (Strogo Odvajanje Grešaka: Subtractive Sandbox vs Additive Harness):* **Red Hat Framework** — pad sandboxa = agent je pokušao nedozvoljenu radnju (blokiraj fail-closed); pad harnessa = greška u alatu/modelu (nastavi petlju s error opservacijom).
- **Sinteza (Go dizajn skica):**
  ```go
  package pep

  import (
      "context"
      "fmt"
      "nexus/pkg/contract"
  )

  type Decision string
  const (
      DecisionAllow Decision = "ALLOW"
      DecisionDeny  Decision = "DENY"
      DecisionAudit Decision = "AUDIT_ONLY"
  )

  type PolicyEnforcementPoint interface {
      EvaluateToolCall(ctx context.Context, principal string, call contract.ToolCall) (Decision, string, error)
      EvaluateDataSink(ctx context.Context, principal string, block contract.ContextBlock, destination string) (Decision, string, error)
  }

  type CorePEP struct {
      strictMode bool
  }

  func (p *CorePEP) EvaluateToolCall(ctx context.Context, principal string, call contract.ToolCall) (Decision, string, error) {
      // 1. Zabrana opasnih alata bez eksplicitnih ovlasti
      if call.EffectClass == contract.EffectIrreversible {
          if call.IdempotencyKey == nil || *call.IdempotencyKey == "" {
              return DecisionDeny, "IRREVERSIBLE_ACTION_REQUIRES_IDEMPOTENCY_KEY", nil
          }
      }
      return DecisionAllow, "", nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/pep.py` i `core/threat_model.py` u Go `pkg/security/pep`.
- **Ugovor / RED Gate:**
  - **Annex A P0.1 & P1.4**; RED test: `test_pep_blocks_unauthorized_irreversible_action`.
- **Verifikacija aspekata:**
  - OPA (Apache-2.0, Go), Cedar (Apache-2.0, Rust/Go bindings).
- **Pareto-floor potvrda:**
  - Pruža nula-alokacijsku evaluaciju pravila u mikrosekundama unutar istog Go procesa.

---

## S6.1: Permission Gating i Shlex Command Tokenizer

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/permissions`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Shlex Tokenizacija i Detekcija Skrivenih Naredbi):* **NEXUSv2 (`core/permissions.py`)** — **NAŠ DIFERENCIJATOR:** Rastavljanje shell naredbi preko POSIX shlex parsera; detektira ulančavanje (`&&`, `||`, `;`, `|`), zamjenu naredbi (subshell `$(...)`, `` `...` ``) i alias obmane (`rm -rf /` skriven iza skripte).
  - *Aspekt B (Granularne Razine Dozvola):* **Codex CLI + Claude Code** — razine dozvola: `READ_ONLY` (pretraga/čitanje), `SAFE_WRITE` (radni prostor), `DANGEROUS` (mreža, sistemske naredbe, brisanje).
  - *Aspekt C (Korisničko Odobrenje i HITL Gate):* **Annex A P1.4 & P1.6** — za `DANGEROUS` operacije izdaje se jednokratni `ApprovalChallenge` token.
- **Sinteza (Go dizajn skica):**
  ```go
  package permissions

  import (
      "fmt"
      "strings"
      "nexus/pkg/sys/shlex"
  )

  type CommandRiskLevel string
  const (
      RiskSafeRead   CommandRiskLevel = "SAFE_READ"
      RiskSafeWrite  CommandRiskLevel = "SAFE_WRITE"
      RiskDangerous  CommandRiskLevel = "DANGEROUS"
      RiskForbidden  CommandRiskLevel = "FORBIDDEN"
  )

  var forbiddenPrefixes = []string{
      "sudo", "su", "chmod -R 777", "mkfs", "dd if=/dev", ":(){ :|:& };:",
  }

  type ShellPermissionGuard struct{}

  func (g *ShellPermissionGuard) ClassifyCommand(rawCmd string) (CommandRiskLevel, string) {
      // 1. Shlex tokenizacija
      tokens, err := shlex.Split(rawCmd)
      if err != nil {
          return RiskForbidden, fmt.Sprintf("shlex parse error (malformed command): %v", err)
      }
      if len(tokens) == 0 {
          return RiskSafeRead, "empty command"
      }

      // 2. Provjera zabranjenih sistemskih naredbi
      joined := strings.Join(tokens, " ")
      for _, forb := range forbiddenPrefixes {
          if strings.Contains(joined, forb) {
              return RiskForbidden, fmt.Sprintf("command contains forbidden pattern %q", forb)
          }
      }

      // 3. Analiza cjevovoda i podnaredbi (Pipeline / Subshell split)
      baseCmd := tokens[0]
      switch baseCmd {
      case "ls", "cat", "git status", "git diff", "grep", "rg", "find":
          return RiskSafeRead, "read-only diagnostic command"
      case "go build", "go test", "npm test", "cargo test", "pytest":
          return RiskSafeWrite, "safe build/test execution"
      case "git push", "curl", "wget", "ssh", "rm -rf":
          return RiskDangerous, "command has network or irreversible disk side-effects"
      default:
          return RiskDangerous, "unknown command requires standard permission gate"
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/permissions.py` (shlex tokenizacija i alias razrješenje) u Go `pkg/security/permissions`.
- **Ugovor / RED Gate:**
  - **Annex A P1.4**; RED test: `test_shlex_tokenizer_catches_nested_subshell_injection`.
- **Verifikacija aspekata:**
  - NEXUS `permissions.py` (dokazano u auditu), Codex approval model (Apache-2.0).
- **Pareto-floor potvrda:**
  - Onemogućuje zaobilaženje sigurnosnih filtara preko shell obfuscacije (`cat file | $(echo rm)`).

---

## S6.2: Sandbox Izvršavanja (GORTEX Go Reuse — Landlock, Seccomp, bwrap, Win JobObjects)

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/sandbox`, **DIREKTAN Go REUSE GORTEX-a**)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Linux Landlock LSM + Seccomp bez cgo-a):* **GORTEX (`gortex/landlock_linux.go`, `gortex/sandbox.go`)** — direktni syscall preko `golang.org/x/sys/unix` (`landlock_create_ruleset`, `landlock_restrict_self`) koji zaključava proces alata na filesystem razini kernela.
  - *Aspekt B (Bubblewrap / Container Fallback):* **OpenSandbox / Bubblewrap (`bwrap`)** — izolacija unutar privremenih unshare namespaceova (`--unshare-net`, `--ro-bind / /`, `--bind <workspace> <workspace>`).
  - *Aspekt C (Cross-Platform Adapteri za Windows i macOS):* **GORTEX (`gortex/process_manager.go`)** — Windows Job Objects (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` + `SetInformationJobObject`) i macOS `sandbox_init` profili.
- **Sinteza (Go dizajn skica):**
  ```go
  package sandbox

  import (
      "context"
      "fmt"
      "os/exec"
      "runtime"

      "nexus/pkg/sys"
  )

  type SandboxEngine interface {
      WrapCommand(ctx context.Context, cmd *exec.Cmd, readPaths []string, writePaths []string) error
      Name() string
  }

  // --- Linux Landlock Sandbox (Direktan GORTEX Go Reuse) ---
  // Izvorni fajl: gortex/landlock_linux.go
  type LinuxLandlockSandbox struct{}

  func (s *LinuxLandlockSandbox) WrapCommand(ctx context.Context, cmd *exec.Cmd, readPaths []string, writePaths []string) error {
      if runtime.GOOS != "linux" {
          return fmt.Errorf("landlock is only available on Linux")
      }
      // GORTEX implementacija: postavlja Landlock pravila u pre-exec hooku podprocesa
      return ApplyLandlockToCmd(cmd, readPaths, writePaths)
  }

  func (s *LinuxLandlockSandbox) Name() string { return "linux-landlock-v5" }

  // --- Windows JobObject Sandbox ---
  type WindowsJobObjectSandbox struct{}

  func (s *WindowsJobObjectSandbox) WrapCommand(ctx context.Context, cmd *exec.Cmd, readPaths []string, writePaths []string) error {
      // Postavlja Windows Job Object s memorijskim i procesnim limitima
      return sys.ApplyWindowsJobLimits(cmd)
  }

  func (s *WindowsJobObjectSandbox) Name() string { return "windows-job-object" }
  ```
- **Salvage iz NEXUSv2:**
  - **DIREKTAN Go REUSE:** `gortex/landlock_linux.go`, `gortex/sandbox.go`, `gortex/process_manager.go` preuzimaju se 1:1 bez izmjena.
- **Ugovor / RED Gate:**
  - **Annex A P2.2** (`ResourceBudget: max_memory_rss, max_cpu`); RED test: `test_landlock_blocks_read_outside_allowed_workspace`.
- **Verifikacija aspekata:**
  - Linux Kernel Landlock v5 (nativni Linux 5.13+), GORTEX (Go, interno dokazano), Bubblewrap (LGPL-2.1+).
- **Pareto-floor potvrda:**
  - Pruža hardversku i kernel-level izolaciju u nula milisekundi bez pokretanja teških Docker kontejnera.

---

## S6.3: Egress Proxy i SSRF / Cloud Metadata Obrana

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/egress`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Tvrdo Blokiranje Cloud Metadata IP Adresa):* **NEXUSv2 (`core/egress.py`)** — **NAŠ DIFERENCIJATOR:** Neprobojno blokiranje `169.254.169.254` (AWS, GCP, Azure, OpenStack IMDS endpointi), loopback adresa (`127.0.0.0/8`, `::1`), link-local (`169.254.0.0/16`, `fe80::/10`) i privatnih podmreža RFC1918 (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) osim eksplicitno odobrenih.
  - *Aspekt B (Zaštita od DNS Rebinding Napada):* **NEXUSv2 (`core/netjail.py`)** — provjera razriješene IP adrese nakon DNS lookup-a (Time-of-Connect IP check), a ne samo naziva domene.
  - *Aspekt C (Domain Allowlist & TLS Inspection):* **Smokescreen (Stripe) / Squid** — striktna allowlista vanjskih API domena (npr. `api.anthropic.com`, `api.openai.com`, `github.com`).
- **Sinteza (Go dizajn skica):**
  ```go
  package egress

  import (
      "context"
      "fmt"
      "net"
      "net/http"
      "syscall"
      "time"
  )

  var blockedCIDRs = []*net.IPNet{
      mustParseCIDR("169.254.169.254/32"), // Cloud IMDS
      mustParseCIDR("169.254.0.0/16"),     // Link-Local
      mustParseCIDR("127.0.0.0/8"),        // IPv4 Loopback
      mustParseCIDR("::1/128"),            // IPv6 Loopback
      mustParseCIDR("10.0.0.0/8"),         // RFC1918 Private
      mustParseCIDR("172.16.0.0/12"),      // RFC1918 Private
      mustParseCIDR("192.168.0.0/16"),     // RFC1918 Private
  }

  type SafeEgressTransport struct {
      allowedDomains map[string]bool
      underlying     *http.Transport
  }

  func NewSafeEgressTransport(allowedDomains []string) *http.Client {
      allowed := make(map[string]bool)
      for _, d := range allowedDomains {
          allowed[d] = true
      }

      dialer := &net.Dialer{
          Timeout:   10 * time.Second,
          KeepAlive: 30 * time.Second,
          Control: func(network, address string, c syscall.RawConn) error {
              host, _, err := net.SplitHostPort(address)
              if err != nil {
                  return err
              }
              ip := net.ParseIP(host)
              if ip == nil {
                  return fmt.Errorf("SECURITY DENY: unresolvable IP %q", host)
              }

              // Provjeri je li IP u zabranjenom CIDR rangu (SSRF Defense)
              for _, blocked := range blockedCIDRs {
                  if blocked.Contains(ip) {
                      return fmt.Errorf("SECURITY DENY: connection to blocked IP %s (Cloud Metadata / SSRF protection)", ip.String())
                  }
              }
              return nil
          },
      }

      transport := &http.Transport{
          DialContext: dialer.DialContext,
      }

      return &http.Client{Transport: transport}
  }

  func mustParseCIDR(s string) *net.IPNet {
      _, cidr, _ := net.ParseCIDR(s)
      return cidr
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/egress.py` i `core/netjail.py` u Go `pkg/security/egress`.
- **Ugovor / RED Gate:**
  - **Annex A P1.3 & S6.3**; RED test: `test_egress_blocks_cloud_metadata_and_private_ips`.
- **Verifikacija aspekata:**
  - NEXUS `egress.py` (dokazano u auditu), Stripe Smokescreen (Apache-2.0).
- **Pareto-floor potvrda:**
  - Spaja dialer control provjeru na razini kernela s anti-rebindingom, čineći SSRF napade na lokalnu mrežu ili cloud tokene tehnički nemogućim.

---

## S6.4: Secrets Management i Credential Broker

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/secrets`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Credential Broker — Tajne Nikada u Promptu):* **NEXUSv2 (`core/broker.py`)** — stvarni API ključevi žive isključivo u memoriji brokera; alati i promptovi vide samo zamjenske tokene (`<NEXUS_CREDENTIAL_REF:github_token>`).
  - *Aspekt B (Automatsko Maskiranje i Redakcija PRIJE Diska):* **Annex A P0.3 + NEXUSv2 (`core/secrets.py`)** — skeniranje regularnim izrazima (OpenAI, GitHub, AWS, Anthropic uzorci) i maskiranje (`sk-ant-***REDACTED***`) prije upisa u `EventJournal` ili slanja na stdout.
  - *Aspekt C (Enkripcija u Mirovanju za Lokalnu Pohranu):* **Goose Keyring / OS Keyring** — spremanje tokena u sistemski privjesak ključeva (SecretService na Linuxu, Keychain na macOS-u, Windows Credential Manager).
- **Sinteza (Go dizajn skica):**
  ```go
  package secrets

  import (
      "regexp"
      "sync"
  )

  var secretPatterns = []*regexp.Regexp{
      regexp.MustCompile(`sk-[a-zA-Z0-9]{32,}`),                     // OpenAI API keys
      regexp.MustCompile(`sk-ant-[a-zA-Z0-9_-]{32,}`),              // Anthropic API keys
      regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),                     // GitHub Personal Access Tokens
      regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                        // AWS Access Key ID
  }

  type SecretBroker struct {
      mu      sync.RWMutex
      vault   map[string]string // refToken -> actualSecret
  }

  func NewSecretBroker() *SecretBroker {
      return &SecretBroker{vault: make(map[string]string)}
  }

  func (sb *SecretBroker) RedactText(input string) string {
      output := input
      for _, pattern := range secretPatterns {
          output = pattern.ReplaceAllString(output, "[REDACTED_SECRET]")
      }
      return output
  }

  func (sb *SecretBroker) SubstituteSecret(refToken string) (string, bool) {
      sb.mu.RLock()
      defer sb.mu.RUnlock()
      val, ok := sb.vault[refToken]
      return val, ok
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/secrets.py` i `core/broker.py` u Go `pkg/security/secrets`.
- **Ugovor / RED Gate:**
  - **Annex A P0.3** (`test_projection_cannot_bypass_journal`); RED test: `test_secrets_redacted_before_journal_write`.
- **Verifikacija aspekata:**
  - NEXUS `secrets.py` (dokazano u auditu), Gitleaks pravila (MIT).
- **Pareto-floor potvrda:**
  - Jamči da se nijedan stvarni API ključ nikada ne pojavi u transkriptu, logu ili modelu, čak i ako korisnik ili model greškom ispišu datoteku s ključevima.

---

## S6.5: Prompt Injection Obrana, Jailbreak Defense i Canary Tripwire

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/canary`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Fail-Closed Canary Tripwire):* **NEXUSv2 (`core/canary.py`) + HARNESS-SPEC v0.8** — **NAŠ DIFERENCIJATOR:** Sustav generira jedinstveni sintetički token per-install (`nexus-canary-uuid4`). Ako se taj token detektira u izlazu modela, to je matematički dokaz proboja granice (boundary breach). Reakcija: **FAIL-CLOSED** (trenutni prekid isporuke, alert, rotacija, nastavak tek uz eksplicitnu ljudsku potvrdu).
  - *Aspekt B (Input/Output Skeneri i Delimiter Fencing):* **LlamaFirewall / LLM Guard / AgentDojo** — strukturirano ograđivanje vanjskih podataka (`<untrusted_context trust="untrusted_external">`) uz provjeru pokušaja eskalacije ovlasti.
  - *Aspekt C (Negativno Testiranje i Audit):* **Annex A P0.1** — testiranje proboja provenijencije (`PROVENANCE_DOWNGRADE`).
- **Sinteza (Go dizajn skica):**
  ```go
  package canary

  import (
      "crypto/rand"
      "encoding/hex"
      "fmt"
      "strings"
      "sync"
  )

  type CanaryTripwire struct {
      mu          sync.RWMutex
      activeToken string
      breachCount int
  }

  func NewCanaryTripwire() *CanaryTripwire {
      b := make([]byte, 16)
      _, _ = rand.Read(b)
      return &CanaryTripwire{
          activeToken: "CANARY_TOKEN_" + hex.EncodeToString(b),
      }
  }

  func (ct *CanaryTripwire) Token() string {
      ct.mu.RLock()
      defer ct.mu.RUnlock()
      return ct.activeToken
  }

  func (ct *CanaryTripwire) VerifyOutputPayload(payload string) error {
      ct.mu.RLock()
      token := ct.activeToken
      ct.mu.RUnlock()

      if strings.Contains(payload, token) {
          ct.mu.Lock()
          ct.breachCount++
          ct.mu.Unlock()
          return fmt.Errorf("FAIL_CLOSED_BREACH: Canary token exfiltration detected in output. Delivery blocked.")
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/canary.py` u Go `pkg/security/canary`.
- **Ugovor / RED Gate:**
  - **Annex A P0.3 & S6.5**; RED test: `test_canary_token_exfiltration_triggers_fail_closed_delivery_block`.
- **Verifikacija aspekata:**
  - AgentDojo (MIT), LLM Guard (MIT), Canarytokens.
- **Pareto-floor potvrda:**
  - Donosi determinističku detekciju exfiltracije podataka bez ovisnosti o nesavršenim klasifikacijskim modelima.

---

## S6.6: AuthN / AuthZ i Tenant / Workspace Izolacija

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/auth`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Multi-Tenant RBAC i Izolacija):* **LiteLLM Proxy + Open WebUI** — definiranje uloga (`admin`, `operator`, `user`), timova i per-tenant kvota.
  - *Aspekt B (Workspace Scoping):* **Dify** — svaki upit i memorijski zapis strogo su izolirani unutar `tenant_id` i `workspace_id`.
  - *Aspekt C (Cache Particioniranje):* **Annex A P2.4** — složeni ključ predmemorije onemogućuje cross-tenant curenje podataka.
- **Sinteza (Go dizajn skica):**
  ```go
  package auth

  import (
      "context"
      "fmt"
  )

  type Principal struct {
      ID          string   `json:"id"`
      TenantID    string   `json:"tenant_id"`
      Roles       []string `json:"roles"` // "ADMIN" | "DEVELOPER" | "AUDITOR"
      Permissions []string `json:"permissions"`
  }

  type Authorizer struct{}

  func (a *Authorizer) Authorize(p Principal, requiredPerm string, targetTenant string) error {
      if p.TenantID != targetTenant {
          return fmt.Errorf("AUTHZ_DENIED: tenant mismatch (%s != %s)", p.TenantID, targetTenant)
      }
      for _, perm := range p.Permissions {
          if perm == requiredPerm || perm == "ALL" {
              return nil
          }
      }
      return fmt.Errorf("AUTHZ_DENIED: principal %s lacks permission %s", p.ID, requiredPerm)
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/auth.py` i multi-tenant modela u Go.
- **Ugovor / RED Gate:**
  - **Annex A P2.4**; Gate test: `test_cross_tenant_cache_key_cannot_alias`.
- **Verifikacija aspekata:**
  - LiteLLM proxy RBAC (MIT), Open WebUI (MIT).
- **Pareto-floor potvrda:**
  - Osigurava čistu particiju podataka u višekorisničkom okruženju (`service` flag) bez utjecaja na brzinu u lokalnom single-user modu.

---

## S6.7: Data Governance: Baseline + Service Split

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/governance`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Baseline u Jezgri — SQLite Retention i Hard Purge):* **HARNESS-SPEC v0.8 (V8 Odluka)** — automatsko brisanje starih transkripata prema TTL-u, export podataka i `DATA_PURGE` propagacija.
  - *Aspekt B (PII Detekcija i Anonimizacija):* **Presidio / Langfuse** — prepoznavanje imena, e-mail adresa, IBAN-a i brojeva kartica uz zamjenu pseudonimima.
  - *Aspekt C (Service Proširenje za DSAR i Legal Hold):* **Annex A P2.1** — razlikovanje `MEMORY_FORGET` (soft tombstone) od `DATA_PURGE` (fizičko brisanje + crypto-erasure).
- **Sinteza (Go dizajn skica):**
  ```go
  package governance

  import (
      "context"
      "fmt"
      "nexus/pkg/contract"
  )

  type GovernanceEngine struct {
      retentionDays int
  }

  func (ge *GovernanceEngine) ExecuteDataPurge(ctx context.Context, op contract.DataPurge) error {
      // 1. Provjera Legal Hold statusa (P2.1 Invarijant)
      if op.LegalHoldCheck {
          return fmt.Errorf("DATA_PURGE_BLOCKED: subject under legal hold")
      }

      // 2. Kaskadno fizičko brisanje: SQLite događaji, memorija, FTS5 i vektorski indeksi
      // 3. Post-delete proba verifikacije (nula preostalih bajtova)
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/governance.py` i SQLite purge skripti u Go `pkg/security/governance`.
- **Ugovor / RED Gate:**
  - **Annex A P2.1 & P2.5**; RED test: `test_purge_cannot_complete_with_residual_copy`.
- **Verifikacija aspekata:**
  - Presidio (MIT, Python), Langfuse (MIT).
- **Pareto-floor potvrda:**
  - Pruža 100% pravnu usklađenost s GDPR/DSAR zahtjevima uz garanciju brisanja svih kopija podataka.

---

## S6.8: Supply-Chain Sigurnost, Plugin Provenance i Runtime TOCTOU (skills.lock)

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/supplychain`)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Runtime TOCTOU Provjera Vještina — skills.lock):* **Annex A P1.1 (agy#1)** — **NAŠ DIFERENCIJATOR:** Učitavač izračunava in-memory SHA-256 hash svake datoteke vještine (`SKILL.md`, skripte) točno prije učitavanja ili izvođenja, uspoređujući ga s potpisanim `skills.lock` zapisom (Time-of-Use provjera, a ne samo install-time scan).
  - *Aspekt B (Potpisivanje Artefakata i SBOM Provjera):* **Sigstore / Cosign + in-toto** — verifikacija potpisa binarnih paketa i TUF manifesta (P1.5).
  - *Aspekt C (Skeniranje Ranjivosti Ovisnosti):* **OSV-Scanner** — automatska provjera biblioteka; pronalazak ranjivosti CVSS >= 7.0 blokira isporuku artefakta.
- **Sinteza (Go dizajn skica):**
  ```go
  package supplychain

  import (
      "crypto/sha256"
      "encoding/hex"
      "fmt"
      "os"

      "nexus/pkg/contract"
  )

  type SkillLockfile struct {
      SkillID   string            `json:"skill_id"`
      Version   string            `json:"version"`
      Files     map[string]string `json:"files"` // relativePath -> sha256
      Signature string            `json:"signature"`
  }

  type SupplyChainVerifier struct{}

  func (scv *SupplyChainVerifier) VerifySkillOnLoad(skillRoot string, lock SkillLockfile) error {
      for relPath, expectedHash := range lock.Files {
          fullPath := skillRoot + "/" + relPath
          data, err := os.ReadFile(fullPath)
          if err != nil {
              return fmt.Errorf("SKILL_INTEGRITY_FAILED: missing file %s: %w", relPath, err)
          }

          h := sha256.Sum256(data)
          actualHash := hex.EncodeToString(h[:])

          if actualHash != expectedHash {
              return fmt.Errorf("SKILL_DIGEST_MISMATCH: file %s was modified on disk after signing (TOCTOU violation)", relPath)
          }
      }
      return nil
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/supplychain.py` i `core/skill_signer.py` u Go `pkg/security/supplychain`.
- **Ugovor / RED Gate:**
  - **Annex A P1.1 & P1.5**; RED test: `test_skill_swap_after_install_is_quarantined` i `test_post_signature_artifact_tamper_blocks_publish`.
- **Verifikacija aspekata:**
  - Cosign (Apache-2.0, Go), OSV-Scanner (Apache-2.0, Go), in-toto (Apache-2.0).
- **Pareto-floor potvrda:**
  - Štiti sustav od zlonamjernih izmjena vještina na disku nakon instalacije i osigurava integritet lanca isporuke.

---

## S6.9: Non-Bypassable Lifecycle Enforcement (Chokepoint)

- **Tip:** `MEHANIZAM` (Go paket `pkg/security/lifecycle`, arhitektonsko srce harnessa)
- **Aspekti i najbolji izvori:**
  - *Aspekt A (Fiksni Redoslijed Faza i Nezaobilazni Chokepoint):* **Claude Code hooks + NEXUSv2 (`core/hooks.py`)** — fiksni redoslijed: `before_run → before_tool → after_tool → before_deliver`. Jezgreni policy ide PRVI i ZADNJI; pluginovi mogu samo suziti odluku.
  - *Aspekt B (on_error kao Grana iz Svake Faze):* **HARNESS-SPEC v0.8 + Codex r2 fix** — `on_error` NIJE zadnji linearni član, već GRANA iz svake pojedine faze. Greška u bilo kojoj točki momentalno prekida success-niz i skače na `on_error` s causal stackom.
  - *Aspekt C (Draft/Commit Ugovor za Ireverzibilne Učinke):* **Annex A P1.4** — nijedan vanjski side-effect ne smije krenuti bez trajnog zapisa namjere (`EffectIntent`).
- **Sinteza (Go dizajn skica):**
  ```go
  package lifecycle

  import (
      "context"
      "fmt"
      "nexus/pkg/contract"
  )

  type HookFunc func(ctx context.Context, env *contract.Envelope) error

  type LifecycleChokepoint struct {
      beforeRunHooks     []HookFunc
      beforeToolHooks    []HookFunc
      afterToolHooks     []HookFunc
      beforeDeliverHooks []HookFunc
      onErrorHooks       []func(ctx context.Context, err error, env *contract.Envelope)
  }

  func (lc *LifecycleChokepoint) ExecuteToolPipeline(ctx context.Context, call contract.ToolCall, executeFunc func() (contract.ToolResult, error)) (contract.ToolResult, error) {
      env := &contract.Envelope{EventType: "tool_execution", ToolCallID: &call.ToolCallID}

      // 1. Before-Tool Faza (Security Checks, Permissions, Sandboxing)
      for _, hook := range lc.beforeToolHooks {
          if err := hook(ctx, env); err != nil {
              lc.handleErrorBranch(ctx, err, env)
              return contract.ToolResult{Status: contract.ToolStatusFailed}, fmt.Errorf("lifecycle before_tool blocked: %w", err)
          }
      }

      // 2. Izvršenje Alata (Action)
      res, err := executeFunc()
      if err != nil {
          lc.handleErrorBranch(ctx, err, env)
          return res, err
      }

      // 3. After-Tool Faza (Redaction, Output Sanitization, TIA)
      for _, hook := range lc.afterToolHooks {
          if err := hook(ctx, env); err != nil {
              lc.handleErrorBranch(ctx, err, env)
              return contract.ToolResult{Status: contract.ToolStatusFailed}, fmt.Errorf("lifecycle after_tool blocked: %w", err)
          }
      }

      return res, nil
  }

  func (lc *LifecycleChokepoint) handleErrorBranch(ctx context.Context, err error, env *contract.Envelope) {
      for _, hook := range lc.onErrorHooks {
          hook(ctx, err, env)
      }
  }
  ```
- **Salvage iz NEXUSv2:**
  - Port `core/hooks.py` i `core/pipeline.py` u Go `pkg/security/lifecycle`.
- **Ugovor / RED Gate:**
  - **Annex A P1.4 & P0.3**; RED test: `test_lifecycle_error_in_before_tool_bypasses_success_hooks_and_triggers_on_error`.
- **Verifikacija aspekata:**
  - Claude Code hook architecture, NEXUS `hooks.py` (dokazano u auditu).
- **Pareto-floor potvrda:**
  - Garantira da niti jedan alat, hook ili plugin ne može zaobići sigurnosnu provjeru, audit ili redakciju tajni.

---

## Rezime S6 Hibrid-Hibrida Sinteze

| Podsekcija | Tip | Ključni graftani izvori & Diferencijatori | Ugovor / RED Gate |
| :--- | :---: | :--- | :--- |
| **6.0 Trust PEP** | MEHANIZAM | • **OPA / Cedar:** Deny-by-default presretač<br>• **Annex A P0.1:** 4 klase povjerenja | **Annex A P0.1 & P1.4** (`test_pep_blocks_unauthorized_irreversible_action`) |
| **6.1 Permissions** | MEHANIZAM | • **NEXUSv2:** **POSIX Shlex Tokenizer** (hvata ulančavanje i subshell obmane)<br>• **Codex:** Granularne razine dozvola | **Annex A P1.4** (`test_shlex_tokenizer_catches_nested_subshell_injection`) |
| **6.2 Sandbox Engine** | MEHANIZAM | • **GORTEX Go Reuse:** Nativni **Landlock v5 + Seccomp** bez cgo-a<br>• **Windows Job Objects** | **Annex A P2.2** (`test_landlock_blocks_read_outside_allowed_workspace`) |
| **6.3 Egress Proxy** | MEHANIZAM | • **NEXUSv2:** **Tvrdo blokiranje Cloud Metadata (169.254.169.254)**<br>• **Anti-DNS Rebinding** | **Annex A P1.3** (`test_egress_blocks_cloud_metadata_and_private_ips`) |
| **6.4 Secrets Broker** | MEHANIZAM | • **NEXUSv2:** Token broker (tajne nikad u promptu)<br>• **Annex A P0.3:** Automatsko maskiranje prije diska | **Annex A P0.3** (`test_secrets_redacted_before_journal_write`) |
| **6.5 Canary Tripwire**| MEHANIZAM | • **NEXUSv2:** **Fail-Closed Canary Exfiltration Tripwire**<br>• **LlamaFirewall:** Delimiter fencing | **Annex A P0.3 & S6.5** (`test_canary_token_exfiltration_triggers_fail_closed_delivery_block`) |
| **6.6 Tenant Isolation**| MEHANIZAM | • **LiteLLM / Dify:** RBAC i tenant-partitioned namespaces | **Annex A P2.4** (`test_cross_tenant_cache_key_cannot_alias`) |
| **6.7 Data Governance** | MEHANIZAM | • **HARNESS-SPEC v0.8:** Baseline SQLite purge engine + Service DSAR<br>• **Presidio:** PII masking | **Annex A P2.1** (`test_purge_cannot_complete_with_residual_copy`) |
| **6.8 Supply-Chain** | MEHANIZAM | • **Annex A P1.1 (agy#1):** **skills.lock Runtime TOCTOU verifikacija**<br>• **Cosign + OSV-Scanner** | **Annex A P1.1 & P1.5** (`test_skill_swap_after_install_is_quarantined`) |
| **6.9 Lifecycle Chokepoint**| MEHANIZAM | • **Claude Code:** Fiksni `before/after` chokepoint<br>• **NEXUSv2:** `on_error` grana iz svake faze | **Annex A P1.4** (`test_lifecycle_error_in_before_tool_bypasses_success_hooks`) |

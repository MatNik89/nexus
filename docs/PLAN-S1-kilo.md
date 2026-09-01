PLAN S1

# PLAN-S1 — S1 Temelji (config / cross-platform paths+procesi / structured logging)

Jezgra `CGO_ENABLED=0`. S1 je 100% MEHANIZAM (mi pišemo). Vlasništvo (kod-vs-data): resolver/
paths/proc/logging-pipeline = KOD; config-vrijednosti = DATA (smiju samo SUZITI kernel maksimum).
Gate: 1.3 veže **P0.3** (jedan write-owner). Format po protokolu; razina detalja = S0 kanonska.

---

## 1.1 Konfiguracija (slojevita: default → global → projekt → env → CLI flag)

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- TOML + `-c` override PO KLJUČU + profili → **Codex CLI** (harness)
- yaml + .env + CLI, DOKUMENTIRAN precedence → **Aider** (harness)
- config.yaml + JSON-schema validacija + hot-reload → **Continue** (harness)

**Sinteza (Go resolver; vrijednosti = DATA koje samo sužavaju kernel maksimum):**
```go
type Layer int // Default < Global < Project < Env < CLI (dokumentirani precedence)

type Config struct { // jedan typed struct, ne map[string]any
    Model ModelBlock `json:"model"`
    Sandbox SandboxBlock `json:"sandbox"`   // vrijednost smije SAMO pooštriti kernel floor
    Egress EgressBlock `json:"egress"`      // allowlist može samo Smanjiti, ne proširiti
    Logging LoggingBlock `json:"logging"`
    Profiles map[string]Profile `json:"profiles"` // named profile = overlay
}

type Resolver struct {
    layers []Layer                       // redoslijed učitavanja
    schema *jsonschema.Validator         // schema validacija PRIJE mergea
}
func (r *Resolver) Load() (*Config, error)          // merge po precedenci, default-reject na nepoznat ključ
func (r *Resolver) Override(key string, v any) error // per-key `-c` override, tipizirano
func (c *Config) Merge(overlay Profile) error        // profile = parcijalni overlay
func (c *Config) ValidateBounds(kernel KernelMax) error // config NE smije proširiti kernel floor (fail-closed)
```
Hot-reload: `fsnotify` na project-config + `SIGHUP` → re-`ValidateBounds` → atomska zamjena pointera
(`atomic.Pointer[Config]`); nevaljana nova verzija se odbija, stara ostaje.

**Salvage:** NEXUSv2 `core/paths.py` (`config_files` repo→user→cwd) + `cli.py _load_cfg` →
**Python→Go port** (NEXUS imao file-ljestvicu, ali env/CLI nisu u istom resolveru — ovo ujedinjuje).

**Ugovor/RED:** bez dediciranog Annex ugovora; gate = kod-vs-data načelo (resolver=kod, vrijednost=
data) + P0.4 `config_hash` (config ulazi u closure hash). RED (naš): config pokuša PROŠIRITI kernel
maksimum (npr. egress allowlist doda `*` ili sandbox spusti ispod floora) → `ValidateBounds` → error,
vrijednost odbijena (nema tihog proširenja).

**Verifikacija:** Codex CLI (Apache-2.0, aktivan), Aider (Apache-2.0, aktivan), Continue (Apache-2.0,
aktivan) — svi stvarni.

**Pareto floor:** per-key override + profili (Codex) + dokumentiran precedence (Aider) + schema-
validacija + hot-reload (Continue) + bounds-validacija (naš — niti jedan od 3 ne provodi "config ne
smije proširiti kernel maksimum") → ≥ svaki.

---

## 1.2 Cross-platform putanje i procesi

**Tip:** MEHANIZAM (per-OS build-tagovi; ovo je cross-platform JEZGRA)

**Aspekti → najbolji izvor:**
- čist keyring/paths sloj preko OS-a → **goose** (harness, Rust)
- identično ponašanje macOS/Linux/Windows → **Codex CLI** (harness, Rust)
- cross-platform PTY + path sloj → **Aider** (harness, Python)

**Sinteza (Go; standardne dir-e preko `os`, procesi preko build-tagova + `SysProcAttr`):**
```go
// paths.go (bez build tagova — stdlib je već cross-platform)
func ConfigDir() (string, error) // os.UserConfigDir()/nexus
func CacheDir()  (string, error) // os.UserCacheDir()/nexus
func DataDir()   (string, error) // os.UserConfigDir()/nexus (trajno stanje)
func KeyringPath() (string, error) // delegira na OS keychain gdje dostupan, inače chmod-600 fajl

// proc_linux.go  (//go:build linux)
type ProcGroup struct{ pgid int }
func StartDetached(name string, args []string, o SpawnOpts) (*ProcGroup, error) {
    cmd := exec.Command(name, args...)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // vlastita process grupa
    ... start; return &ProcGroup{pgid: cmd.Process.Pid}
}
func (p *ProcGroup) Terminate() error { return syscall.Kill(-p.pgid, syscall.SIGTERM) } // cijelo stablo
func (p *ProcGroup) Kill() error      { return syscall.Kill(-p.pgid, syscall.SIGKILL) }

// proc_windows.go (//go:build windows)
func StartDetached(...) { cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP} }
func (p *ProcGroup) Terminate() error { exec taskkill /T /F /PID p.pid } // /T = stablo
// (Job Object s KILL_ON_JOB_CLOSE kao jača varijanta — matrica)

// proc_darwin.go — isto kao linux (killpg), bez seccomp/Landlock (S6 adapter)

// identity dokaz za P1.2 (orphan sweep): PID SAM nije dokaz — PID+start-token
func StartToken(pid int) (string, error) // linux: /proc/<pid>/stat starttime; win/mac: syscall
```
PTY: `github.com/creack/pty` (pure-Go, syscall — bez cgo).

**Salvage:** NEXUSv2 `core/paths.py` (`platformdirs`) + `core/subproc.py` (`_kill_tree`: killpg/
taskkill) → **Python→Go port** (NEXUS VEĆ imao killpg+taskkill logiku — direktna logika, ne copy).

**Ugovor/RED:** bez dediciranog Annex ugovora; gate = cross-platform korektnost + **P1.2**
(orphan/lock sweep ovisi o `StartToken` + process-grupi). RED (naš, veže P1.2): spawn detachiranu
grupu (parent→child), SIGKILL parent, restart → sweeper dokazano reapa child preko start-tokena, a
`Terminate()` na grupu ubija CIJELO stablo (test: child preživi parentov direktni kill, ali NE
grupni `Terminate`).

**Verifikacija:** goose (Apache-2.0, aktivan), Codex CLI (Apache-2.0, aktivan), Aider (Apache-2.0,
aktivan) — svi stvarni.

**Pareto floor:** paths-apstrakcija (goose) + per-OS konzistentnost (Codex) + PTY (Aider) +
process-grupa kill + PID start-token (naš, iz NEXUS `_kill_tree` — niti jedan od 3 ne daje start-
token identitet) → ≥ svaki.

---

## 1.3 Strukturirano logiranje od prvog dana

**Tip:** MEHANIZAM (OTel-kompatibilan; JEDAN write-owner = P0.3)

**Aspekti → najbolji izvor:**
- span/log/metrika model s korelacijom → **OpenTelemetry SDK/spec** (standard)
- strukturni log samog harnessa (key=value, bez printf) → **structlog** (Python) / **tracing** (Rust)
- event stream kao centralna istina → **OpenHands** (harness)

**Sinteza (slog + OTel bridge; SVE trajno pisanje kroz journal, ne zasebne fajlove):**
```go
// kernel/journal/logger.go — jedini write-owner je journal (S0 P0.3), logger = tanki emit
type Logger struct { journal *journal.Journal; redactor *redactor.Redactor }

func (l *Logger) Emit(ev journal.JournalEvent) error
//   validate schema → redact (PRIJE pisanja) → journal.Append (atomic seq) → projekcije

// korelacijski kontekst: run_id/turn_id/tool_call_id/attempt kao slog attrs + OTel baggage
func (l *Logger) With(runID, turnID, toolCallID string, attempt uint32) *Logger
// slog + otel su PROJEKCIJE, ne pisci:
//   log        = diagnostic projekcija (slog JSON handler čita journal offset)
//   trace      = span projekcija (go.opentelemetry.io/otel spanovi iz journal eventa)
//   transcript = user-visible projekcija (S15.3)
//   metrics    = OTLP export iz journala (S15.4) — OTel-kompatibilan, ali izveden
```
`Logger.Emit` NE smije sam alocirati `event_id`/`sequence` (P0.3: nijedan sink ne alocira ID).

**Salvage:** NEXUSv2 `core/trace.py` (JSONL spanovi) + `core/otel.py` (genai atributi) →
**Python→Go port**; journal iz S0 = **direktan Go reuse** (isti `EventJournal`).

**Ugovor/RED:** **P0.3** + `test_projection_cannot_bypass_journal` — trace exporter pokuša IZRAVNO
zapisati event s canary tajnom bez `source_event_id` → `NON_CANONICAL_WRITE`, nijedan sink ne smije
sadržavati canary, napadački event nije appendan (zaseban redaktirani rejection-event dopušten).
Dodatno (naš): korelacijski ID-evi `run/turn/tool_call/attempt` + `causal_parent` obavezni na svakom
spanu (spec zahtjev, red 153-154).

**Verifikacija:** OpenTelemetry (CNCF, Apache-2.0, aktivan), structlog (MIT, aktivan), tracing (MIT,
aktivan), OpenHands (MIT, aktivan) — svi stvarni.

**Pareto floor:** OTel span/log/metrika model (OpenTelemetry) + key=value strukturni slog (structlog/
tracing) + event-stream-as-truth (OpenHands) + JEDAN write-owner redact-before-write (naš, P0.3 —
niti jedan od 3 ne nameće single-writer) → ≥ svaki.

---

## S1 cross-cutting napomene

1. **1.2 je cross-platform JEZGRA:** `os.UserConfigDir/CacheDir/HomeDir` (stdlib, bez build-tagova)
   za putanje; build-tagovi SAMO za proces-grupu/kill (linux killpg, win taskkill/Job Object, darwin
   killpg). Ovo je temelj za S6 sandbox (per-OS adapteri) i P1.2 orphan-sweep (start-token).
2. **1.3 ovisi o S0 journalu:** `Logger` je tanki sloj iznad `EventJournal` (P0.3) — NE gradi se
   prije S0.1/S0.3. Redakcija je u writeru (P0.3 invariant), ne u pozivatelju.
3. **Honest gap (S1):** 1.1 i 1.2 nemaju dedicirani Annex RED ugovor (isto kao 0.3) — gate je
   kod-vs-data načelo + P1.2/P0.4 posredno. Predlažem (matrix-prep) P0.6 „config-bounds + process-
   identity" ako moderator želi simetriju; inače su posredni gateovi dovoljni.
4. **Verifikacija:** svi top-3 kandidati stvarni (Codex/Aider/Continue/goose/OTel/structlog/tracing/
   OpenHands — licence+aktivnost potvrđene). Salvage `paths.py`/`subproc.py`/`trace.py` interni,
   postoje (audit potvrdio), port je logika ne ekosustav.

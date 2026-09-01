PLAN S1

# S1 — Temelji

## Zajedničke odluke i paket-layout

- S1 je jezgra. Konfiguracija, putanje, procesni lifecycle i dijagnostički događaji nisu skillovi ni zamjenjivi workflow pluginovi.
- Predloženi paketi: `internal/foundation/config`, `internal/foundation/pathx`, `internal/foundation/procx`, `internal/foundation/telemetry`. Platform-specific kod je samo u `pathx/*_{linux,darwin,windows}.go` i `procx/*_{unix,linux,darwin,windows}.go`; ostatak ne grana po `runtime.GOOS`.
- Ovisnosti: `config -> pathx`; `procx -> pathx + kernel/contracts`; `telemetry -> kernel/contracts + kernel/journal`. Nijedan S1 paket ne smije ovisiti o CLI/TUI/desktop sloju.
- Ručno pisani Go structovi i validatori ostaju normativni. TOML/JSON Schema, OTLP i CLI flagovi su ulazne/izlazne projekcije.
- Novi runtime dependency opravdan je samo gdje stdlib nema ekvivalent: `pelletier/go-toml/v2` za TOML, `fsnotify/fsnotify` za 3-OS file notifications te `golang.org/x/sys/{unix,windows}` za procesno stablo/OS identitet. Nema vanjskog config, process-supervisor ni telemetry runtimea.

## 1.1 Konfiguracija — default → global → projekt → env → CLI

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Tipizirani TOML slojevi i runtime override | Codex CLI | Eksplicitni layer stack, named profile unutar globalnog sloja i CLI override kao zadnji sloj. |
| Dokumentiran env/file/CLI precedence | Aider | Jedan ispisivi precedence red i ekvivalentni nazivi opcija kroz file/env/CLI. |
| Schema validation i automatsko osvježavanje | Continue | Strict parse/validate cijelog kandidata pa atomska zamjena snapshot-a; stari snapshot ostaje na svakoj grešci. |
| Per-key provenance i trust granica | Vlastiti kod + NEXUS salvage | Svaka efektivna vrijednost pamti owner layer; projekt ne smije relaksirati operator-owned security polja. |
| Stabilnost aktivnog runa | Vlastiti kod | Run pri `ADMITTED` pinna `config_generation+config_hash`; hot reload vrijedi za nove runove, osim monotone emergency deny/revocation politike. |

- **Sinteza:** `internal/foundation/config` ima jedan resolver. Named profile je dio globalnog operator sloja, ne šesti filesystem trust-sloj: `defaults < global.base < global.profile < project < env < CLI`.

```go
type LayerKind uint8 // DEFAULT|GLOBAL|GLOBAL_PROFILE|PROJECT|ENV|CLI
type TrustClass uint8 // BUILTIN|OPERATOR|WORKSPACE|PROCESS|INVOCATION

type Source struct {
    Kind       LayerKind
    Trust      TrustClass
    URI        string
    Digest     string
    LoadedAt   time.Time
}

type Optional[T any] struct {
    Set   bool
    Value T
}

type ConfigKey uint16 // zatvoreni enum svih leaf ključeva
type SecretRef struct { Store, Key string }

type Config struct {
    Profile   string
    Provider  ProviderConfig
    Execution ExecutionConfig
    Telemetry TelemetryConfig
    Security  SecurityConfig
    Features  FeatureConfig
}

type PartialConfig struct {
    Profile      Optional[string]
    Provider     PartialProviderConfig
    Execution    PartialExecutionConfig
    Telemetry    PartialTelemetryConfig
    Security     PartialSecurityConfig
    Features     PartialFeatureConfig
}

type EffectiveConfig struct {
    Generation uint64
    ConfigHash string
    Values     Config
    Origins    map[ConfigKey]Source // zatvoreni enum ključeva; jedan origin za svaki leaf
}

type Snapshot struct {
    Effective EffectiveConfig
    Sources   []Source
}

type ResolveInput struct {
    Defaults Config
    GlobalTOML, ProjectTOML []byte
    Environment []string
    CLI PartialConfig
    SelectedProfile string
    WorkspaceTrusted bool
}

type Resolver interface {
    Resolve(ctx context.Context, in ResolveInput) (*Snapshot, *contracts.TypedError)
}

type Store interface {
    Current() *Snapshot
    Publish(expectedGeneration uint64, candidate *Snapshot) error
}
```

  Normativni merge i reload:

  1. Embedded default je potpun i validan. Svaki sljedeći layer se strict-decodea u zaseban tipizirani `PartialConfig`; duplicate/unknown key, wrong type, invalid enum, overflow, nekanonska putanja ili nepoznata env varijabla faila cijeli layer.
  2. Scalar zamjenjuje scalar; struct se mergea po eksplicitno postavljenim leafovima; lista zamjenjuje cijelu listu; mapa ključeva se mergea samo na poljima koja su u Go ugovoru deklarirana kao keyed collections. Nema implicitnog append/union ponašanja.
  3. Env grammar je zatvorena: `NEXUS__SECTION__FIELD`; vrijednosti prolaze isti parser/validator kao TOML. CLI koristi typed flagove koji grade `PartialConfig`, ne tekstualni `key=value` interpreter.
  4. Project layer se čita samo iz kanonskog workspace roota. Operator-owned polja (`security.*`, credential store, egress maksimum, sandbox maksimum, extension trust) project smije samo suziti; relaksacija vraća `CONFIG_TRUST_VIOLATION`.
  5. Secret bytes nisu dio `EffectiveConfig`; config nosi samo `SecretRef`. `Explain(path)` vraća efektivnu vrijednost i origin, ali redaktira secret-ref metadata prema politici.
  6. `ConfigHash` je SHA-256 kanonske, tipizirane efektivne konfiguracije s referencama, bez runtime-resolved secret bytes. Taj isti hash ulazi u S0.5 `GateAttestation`.
  7. `fsnotify` gleda parent direktorije global/project datoteka da preživi atomic rename. Notification je samo hint, ne dokaz svježine: prije admissiona novog runa resolver uspoređuje source identity/digest, a eksplicitni reload uvijek radi puni read. Event samo zakazuje bounded debounce; resolver ponovno čita *sve* slojeve. Publish je generation-CAS + jedna `atomic.Pointer[Snapshot]` zamjena.
  8. Parse/validation/gate failure emitira kanonski `config.reload_rejected` preko journala i ostavlja prethodnu generaciju. Uspjeh emitira `config.generation_published`; trenutni run nastavlja s pinned snapshotom.
  9. Bootstrap redoslijed je `path roots -> defaults/global -> journal -> optional project/env/CLI -> publish`. Prije journala dopušten je samo bounded stderr bootstrap code bez vrijednosti konfiguracije.

- **Salvage:** Python→Go port platformskih rootova, stabilnog `workspace_id`, profile izolacije i precedence/trust ideje iz `/home/matej/NEXUSv2/core/paths.py:21-76,98-107`. Ne portirati dinamički `dict.update()` niti šest odvojenih YAML loadera u callerima: grep pokazuje da `config_files()` zasebno konzumiraju guardian, egress, update, web, executor i drugi moduli, što bi zadržalo više schema ownera. `NEXUS_STATE` ponašanje ostaje test/portable-root override, ali se preimenuje u jedan dokumentirani `NEXUS_STATE_ROOT` prije javnog freezea.

- **Ugovor/RED:** nema zasebnog Annex ugovora za 1.1; gateovi su S0 **P0.1** (svaki reload outcome je tipizirani Envelope), **P0.4** (objavljena konfiguracija i gate attestations moraju imati isti `config_hash`) te ownership načelo da security layer može samo suziti kernel maksimum. RED testovi: `test_precedence_exact_default_global_profile_project_env_cli`, `test_project_cannot_relax_operator_security`, `test_unknown_key_rejects_whole_generation`, `test_failed_hot_reload_keeps_previous_snapshot`, `test_running_run_keeps_pinned_generation`, `test_config_explain_reports_origin_without_secret`, `test_config_hash_change_invalidates_capability_closure`. Kontrolirani mutant koji objavi djelomično parsirani reload mora promijeniti oracle u RED.

- **Verifikacija:** provjereno 2026-09-01 na primarnim izvorima. [Codex loader](https://github.com/openai/codex/blob/main/codex-rs/config/src/loader/mod.rs) stvarno gradi ordered system/user/profile/project/runtime stack i onemogućuje project slojeve u untrusted direktoriju; [Codex CLI shared options](https://github.com/openai/codex/blob/main/codex-rs/utils/cli/src/shared_options.rs) potvrđuje profile override. [Aider configuration](https://aider.chat/docs/config.html) stvarno izlaže YAML, env/`.env` i CLI oblike. [Continue config reference](https://docs.continue.dev/reference) stvarno ima verzionirani `config.yaml`, a [configuration guide](https://docs.continue.dev/customize/deep-dives/configuration) potvrđuje refresh na save; VS Code manifest veže YAML na JSON schema. Codex/Aider/Continue repozitoriji su aktivni; licence su redom Apache-2.0, Apache-2.0 i Apache-2.0. Hot reload se ne kopira kao TypeScript runtime nego kao provjereno ponašanje.

- **Pareto floor:** **PASS za 1.1.** Očuvani su Codex slojevi/profil/runtime override, Aiderova transparentna env/CLI ekvivalencija i Continueov schema+reload UX. Sinteza dodaje per-key origin, trust monotonicity, generation pinning i atomic publish. Najjači kontraargument je dodatna `fsnotify` ovisnost; stdlib nema 3-OS watcher, a polling povećava latency/bateriju i još uvijek traži isti transactional reload. Upgrade gate: ako se u mjerenju pokaže da eksplicitni reload zadovoljava proizvodni UX, `fsnotify` se briše bez promjene resolver ugovora.

## 1.2 Cross-platform putanje i procesi

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Platform-native data/config/cache rootovi | goose | Jedan portable-root override te macOS/Linux/Windows defaulti bez hardkodiranog `~/.nexus`. |
| Stvarna 3-OS separacija i process-tree disciplina | Codex CLI | Build-tagged OS implementacije, structured argv, zatvoreni stdin, child-tree cleanup i OS-specifični identitet. |
| Windows/path/PTY compatibility fixtures | Aider | Windows CI i model-generated path corpus; PTY ostaje capability iza sučelja, ne sigurnosni owner. |
| Go-native cancellation i bounded pipe lifecycle | Go `os/exec` | `CommandContext`, custom `Cancel`, `WaitDelay`, paralelni bounded drain i obvezni `Wait`. |
| Dokazano vlasništvo prije cleanupa | Annex P1.2 | PID nije identitet; start-token, group/job i durable lease vežu proces uz run prije predaje handlea calleru. |

- **Sinteza — putanje:** `internal/foundation/pathx` vraća tipizirane rootove i zatvorene workspace putanje. Pozivatelj nikad konkatenira user path string s rootom.

```go
type RootKind uint8 // DATA|CONFIG|CACHE|STATE|TEMP|WORKSPACE

type Root struct {
    Kind      RootKind
    Canonical string
    VolumeID  string
}

type SafePath struct {
    Root     Root
    Relative string
    Absolute string
}

type ResolvePolicy struct {
    MustExist bool
    AllowFinalSymlink bool
    AllowUNC bool
}

type Paths interface {
    Root(kind RootKind, create bool) (Root, error)
    Workspace(path string) (Root, error)
    ResolveWithin(root Root, untrustedRelative string, policy ResolvePolicy) (SafePath, error)
    AtomicTemp(target SafePath) (*os.File, SafePath, error)
}
```

  Pravila putanja:

  1. `NEXUS_STATE_ROOT`, ako je postavljen, mora biti apsolutan, kanoniziran i ne-smije biti symlink; pod njim su `data/config/cache/state/temp`. Inače OS file određuje default: Linux XDG uz standardne fallbacke, macOS Application Support/Caches, Windows Roaming/Local AppData.
  2. `Workspace()` koristi absolute path, razrješava postojeće symlink komponente i sprema volume/file identity. `workspace_id` je SHA-256 kanonskog OS-normaliziranog identiteta, ne korisničkog stringa.
  3. `ResolveWithin` odbija absolute input, `..` escape, NUL, alternate data stream, Windows device basename, UNC/device namespace bez eksplicitne politike i symlink/reparse escape. Nakon otvaranja ponovno provjerava file identity da smanji TOCTOU prozor; S6 kasnije dodaje handle-relative sigurni I/O.
  4. Temp datoteka je na istom volumenu/direktoriju kao cilj; save radi write -> file sync -> close -> atomic rename -> directory sync gdje OS podržava. Partial failure čuva original i vraća temp reference za recovery.

- **Sinteza — procesi:** zajednički runner je u `procx/runner.go`; OS operacije su iza neexportiranog build-tagged sučelja.

```go
type ProcessSpec struct {
    OperationID string
    Executable  string
    Args        []string
    Dir         pathx.SafePath
    Env         []EnvVar
    Stdin       io.Reader
    StdoutLimit, StderrLimit int64
    Deadline, GracePeriod time.Duration
    TerminalMode TerminalMode // PIPE|INHERIT|PTY
}

type TerminalMode uint8 // PIPE|INHERIT|PTY
type EnvVar struct {
    Name, Value string
    Secret bool
}

type ProcessIdentity struct {
    PID        int
    StartToken string
    GroupToken string // Unix PGID/session ili Windows Job ID
}

type CapturedStream struct {
    Bytes     []byte
    Total     int64
    SHA256    string
    Truncated bool
}

type ProcessResult struct {
    Identity ProcessIdentity
    ExitCode int
    StartedAt, FinishedAt time.Time
    Stdout, Stderr CapturedStream
    TimedOut, Cancelled bool
}

type LeaseSink interface {
    Reserve(context.Context, ProcessSpec) (reservationID string, fencingToken uint64, err error)
    Bind(context.Context, string, uint64, ProcessIdentity) error
    Release(context.Context, string, uint64, ProcessResult) error
}

type Runner interface {
    Run(context.Context, ProcessSpec, LeaseSink) (ProcessResult, *contracts.TypedError)
}
```

  Normativni lifecycle:

  1. `Executable` se razrješava `exec.LookPath`; relative-current-directory `ErrDot`, shell string i empty argv se odbijaju. Runner poziva isključivo `exec.CommandContext(path, args...)`; nema `sh -c`, `cmd /C` ni PowerShell interpolacije.
  2. Environment se gradi iz minimalnog OS allowlista + eksplicitnih `EnvVar`; duplicate case-insensitive Windows key, NUL i nedopušten secret env se odbijaju. Stdin je defaultno zatvoren.
  3. Prije `Start`a S7 `LeaseSink.Reserve` trajno rezervira ownership nonce. Nakon starta runner čita `PID+start-token+group/job`, zatim mora durable `Bind`; Bind failure odmah terminira i reap-a stablo. Caller ne dobiva živ handle prije Binda.
  4. Linux/macOS `proc_unix.go` postavlja `SysProcAttr{Setpgid:true}`; nakon ponovne provjere start-tokena/fencing tokena cancel radi `killpg` semantiku preko `unix.Kill(-pgid, SIGTERM)`, čeka monotoni `GracePeriod`, ponovno verificira identity, zatim `unix.Kill(-pgid, SIGKILL)` i `Wait`. Linux baseline ne koristi `Pdeathsig`: smrt Go creator threada nije isto što i smrt procesa, a lease je kanonski recovery dokaz.
  5. Windows `proc_windows.go` koristi `CREATE_NEW_PROCESS_GROUP` i `x/sys/windows` Job Object s `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. Graceful cancel šalje podržani console/control signal; zatim zatvara job. `taskkill.exe /PID <pid> /T /F`, lociran preko Win32 `GetSystemDirectory` (nikad env/PATH lookup), bounded je fallback samo nakon ponovne provjere start-tokena i fencing tokena.
  6. stdout/stderr se dreniraju paralelno do EOF da child ne blokira. Memorija je bounded; višak se hashira i odbacuje uz `Truncated=true`. `WaitDelay` ograničava potomka koji zadrži pipe. Sve goroutine se joinaju prije rezultata.
  7. Timeout/cancel razlikuju se od exit codea i mapiraju u S0 `TypedError`; runner nema retry petlju. `LeaseSink.Release` se poziva tek nakon potvrđenog reapa; failure ostavlja lease u `CLEANING`, ne lažno `RELEASED`.
  8. `TerminalMode=PTY` zahtijeva build-tagged PTY adapter i capability gate. Ako adapter nije aktivan, faila `PTY_UNAVAILABLE`; nikad tiho ne prelazi na shell.

- **Salvage:** Python→Go port root/profile/workspace ponašanja iz `/home/matej/NEXUSv2/core/paths.py:21-53,98-107` te structured argv, process-group timeout, bounded pipe drain i stderr path-redaction fixturea iz `/home/matej/NEXUSv2/core/subproc.py:24-44,47-117`. Ne portirati “best-effort” exception swallowing ni Windows `taskkill` kao primarni identity mehanizam. NEXUS `subproc.py:67-81` rlimits su dokazano samo POSIX best-effort i pripadaju kasnijem S6 sandbox/resource adapteru, ne portable S1 obećanju.

- **Ugovor/RED:** Annex **P1.2** određuje `ResourceLease`, start-token, fencing i `TERM -> wait -> KILL -> verified exit`; S1.2 pruža jedini procesni primitive, dok S7.3 ostaje owner leasa/recoveryja. P0.2 zabranjuje runner retry. Obvezni RED `test_sigkill_restart_reaps_owned_tree_only`; S1 lokalni gateovi: `test_timeout_kills_grandchild_and_drains_pipes`, `test_recycled_pid_with_wrong_start_token_survives`, `test_bind_failure_kills_before_returning_handle`, `test_argv_never_enters_shell`, `test_stdout_stderr_caps_do_not_deadlock`, `test_workspace_symlink_and_windows_device_escape_rejected`, `test_three_os_path_golden_vectors`. Svaki OS integration test mora pokrenuti stvarni child/grandchild, ne mockati `exec.Cmd`.

- **Verifikacija:** [goose environment/path guide](https://github.com/aaif-goose/goose/blob/main/documentation/docs/guides/environment-variables.md) stvarno dokumentira jedan root override i različite macOS/Linux/Windows lokacije. [Codex packaging targets](https://github.com/openai/codex/blob/main/scripts/codex_package/targets.py), [platform dispatch](https://github.com/openai/codex/blob/main/codex-rs/cli/src/main.rs) i [spawn code](https://github.com/openai/codex/blob/main/codex-rs/core/src/spawn.rs) potvrđuju 3-OS buildove, OS grananje i child cleanup pristup. Go [os/exec](https://pkg.go.dev/os/exec) stvarno daje `CommandContext`, custom `Cancel` i `WaitDelay`; [SysProcAttr](https://pkg.go.dev/syscall) i [`x/sys`](https://pkg.go.dev/golang.org/x/sys/unix) izlažu potrebne OS primitive. Microsoftov [`taskkill`](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/taskkill) dokumentira `/T`, ali nije ownership dokaz. Aider ima Windows CI, no aktualni [absolute-path bug](https://github.com/Aider-AI/aider/issues/5620) je konkretna kontraevidencija za sigurnosni path floor; njegovu implementaciju zato ne graftamo kao granicu.

- **Pareto floor:** **PASS za dokazive aspekte 1.2.** Gooseov OS layout, Codexova 3-OS separacija/process cleanup i Aiderova compatibility test površina ostaju, dok ownership/start-token i bounded drains pooštravaju granicu. Ne tvrdimo da S1 zamjenjuje S6 sandbox: process-group/job kontrola nije filesystem/network izolacija. Najslabija točka je Windows race između `CreateProcess` i Job assignment; RED test mora forsirati child-spawn u tom prozoru. Ako se race reproducira, upgrade je suspended-create + direct `CreateProcess` adapter, ne širenje taskkill fallbacka.

## 1.3 Strukturirano logiranje od prvog dana

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Standardni log/span/metric model i korelacija | OpenTelemetry spec/SDK | OTel-kompatibilni recordi, trace/span kontekst, Resource/Scope i OTLP adapter. |
| Ergonomični strukturirani event + context | structlog / Rust `tracing` | Tipizirani attrs, scoped span context, severity/target i jeftini filter prije izgradnje payload-a. |
| Jedan centralni event stream | OpenHands | Dijagnostika i span lifecycle prvo su S0 događaji; sinkovi slušaju journal, ne publishaju paralelno. |
| Kanonski write/redaction/integrity owner | Annex P0.3 | `EventJournal.Append` jedini alocira event ID/sequence i jedini put do log/trace/transcript/audit projekcija. |

- **Sinteza:** `internal/foundation/telemetry` ima dva jasno različita API-ja: emitter proizvodi kanonski dijagnostički događaj *preko journala*; projector samo prevodi već appendani `JournalEvent` u sink record.

```go
type AttrKind uint8 // STRING|BOOL|INT64|FLOAT64|DURATION|BYTES_REF
type Attr struct {
    Key string
    Kind AttrKind
    String string
    Bool bool
    Int64 int64
    Float64 float64
}

type Diagnostic struct {
    EventName string
    Severity Severity
    Body string
    Attributes []Attr
    RunID string
    TurnID, ToolCallID *string
    AttemptNo uint32
    ParentEventID *string
}

type Severity uint8 // TRACE|DEBUG|INFO|WARN|ERROR|FATAL

type SpanSpec struct {
    Name, Kind string
    Attributes []Attr
    ParentEventID *string
}

type SpanHandle struct { StartEventID string }
type SpanOutcome struct {
    Status string
    ErrorCode *string
    Attributes []Attr
}

type Emitter interface {
    Emit(context.Context, Diagnostic) (contracts.Envelope, *contracts.TypedError)
    StartSpan(context.Context, SpanSpec) (context.Context, SpanHandle, *contracts.TypedError)
    EndSpan(context.Context, SpanHandle, SpanOutcome) *contracts.TypedError
}

type JournalEvent struct {
    Envelope contracts.Envelope
    JournalOffset uint64
    RedactionPolicyVersion string
    IntegrityPrevHash, IntegrityHash string
    SealedPayloadRef *string
}

type ProjectionRecord struct {
    SourceEventID string
    SourceOffset uint64
    ProjectionVersion string
    Log OTelLogRecord
}

type InstrumentationScope struct {
    Name, Version string
    Attributes []Attr
}

type OTelLogRecord struct {
    Timestamp, ObservedTimestamp time.Time
    TraceID [16]byte
    SpanID [8]byte
    TraceFlags byte
    SeverityText string
    SeverityNumber int32
    Body json.RawMessage
    Resource []Attr
    Scope InstrumentationScope
    Attributes []Attr
    EventName string
}

type Projector interface {
    Apply(context.Context, JournalEvent) (ProjectionRecord, error)
}

type Exporter interface {
    Export(context.Context, []ProjectionRecord) (ackedThrough uint64, err error)
}
```

  `OTelLogRecord` prati stabilni OTel Logs Data Model: `Timestamp`, `ObservedTimestamp`, `TraceID`, `SpanID`, `TraceFlags`, `SeverityText/Number`, `Body`, `Resource`, `InstrumentationScope`, `Attributes`, `EventName`. NEXUS korelacija je dodatno eksplicitna u attrs: `nexus.event_id`, `run_id`, nullable `turn_id/tool_call_id`, `attempt_no`, `parent_event_id`, `sequence`, `schema_id/version`, `journal_offset`.

  Normativni tok i vlasništvo:

  1. `Emitter` konstruira S0 payload i poziva injektirani `EventJournal.Append`; nema file/stdout/OTLP handle. Journal radi `validate -> classify/redact -> atomic sequence+append -> durable flush` i vraća finalni envelope.
  2. Span start/end su zasebni kanonski događaji. Context nosi samo stabilne identifikatore; concurrent goroutine dobiva eksplicitni child context, bez globalnog mutable span stacka.
  3. Projector prima samo `JournalEvent` s validnim integrity chainom i redaction-policy versionom. Record bez `source_event_id+source_offset+projection_version` vraća `NON_CANONICAL_WRITE`.
  4. Projekcija je idempotentna na `(source_event_id,projection_version)` i ima stanje `UNSEEN->APPLIED`. Sink cursor se pomiče tek nakon durable file write/fsync ili uspješnog OTLP ack-a; failure ostavlja cursor i dopušta at-least-once replay.
  5. OTLP exporter je adapter, nikad journal. Ne generira nove trace/span ID-eve pri svakom batchu: stabilna mapping funkcija iz kanonskih run/event ID-eva daje isti OTel kontekst pri replayu.
  6. Metrics su izvedene iz istih source događaja (npr. latency/token/cost), ne drugi counter write-put. Transcript/audit koriste isti `event_id` i različitu `projection_version`.
  7. Attribute keys su allowlisted po event schema; vrijednosti su bounded i tipizirane. Secret nikad nije attr/body; dopušten je samo autorizirani `sealed_payload_ref`, koji log/trace exporter ne dereferencira.
  8. Za stdlib/package ergonomiju postoji `slog.Handler` bridge koji `slog.Record` pretvara u `Diagnostic` i poziva Emitter. Bridge odbija unsupported vrijednosti; ne smije izravno pisati default `slog` sink.
  9. Jedina bootstrap iznimka je minimalni stderr zapis oblika `NEXUS_BOOTSTRAP_ERROR code=<safe-code>` kad journal još nije moguće otvoriti. Nema user texta, attrs, putanja ni secreta; nakon init-a bootstrap sink se nepovratno zatvara.
  10. Exporter nema vlastiti retry/backoff loop; neuspjeh se vraća S7 policy owneru. Telemetry export failure ne mijenja poslovni run state, ali gubitak obveznog kanonskog state/audit append-a blokira pripadni state prijelaz.

- **Salvage:** Python→Go port span parent/child, monotonic duration i error-type-without-message ponašanja iz `/home/matej/NEXUSv2/core/trace.py:22-56,91-110`, plus typed GenAI attribute nazive iz `/home/matej/NEXUSv2/core/otel.py:31-45`. Ne portirati `trace.py:58-64` (paralelni JSONL writer i progutani error), globalni counter iz `trace.py:14-19`, ni `otel.py:59-82` (novi trace ID po batchu, direct network sink, swallowing). To izravno krši P0.3/replay korelaciju. GORTEX trace queue također nije reuse: bounded queue tiho dropa spanove i piše zasebnim putem.

- **Ugovor/RED:** Annex **P0.3** je puni gate; obvezni `test_projection_cannot_bypass_journal` mora kroz stvarni file i fake-OTLP sink dokazati `NON_CANONICAL_WRITE`, odsutnost canary secreta i 0 napadačkih zapisa. P0.1 daje obvezne correlation/causality identifikatore, P0.2 zabranjuje exporter self-retry. Dodatni RED: `test_redaction_occurs_before_any_projection`, `test_same_event_replay_keeps_otel_ids`, `test_projection_replay_is_idempotent`, `test_failed_export_does_not_advance_cursor`, `test_concurrent_spans_keep_explicit_parents`, `test_secret_attr_never_reaches_file_or_otlp`, `test_bootstrap_sink_closes_after_journal_init`, `test_metric_log_trace_share_source_event_id`.

- **Verifikacija:** [OpenTelemetry Logs Data Model](https://opentelemetry.io/docs/specs/otel/logs/data-model/) stvarno definira timestamp/observed timestamp, trace/span context, severity, body, Resource, Scope, attrs i event name; [OTel logging spec](https://opentelemetry.io/docs/specs/otel/logs/) potvrđuje API/SDK bridge i korelaciju logova s traceovima. [Rust tracing](https://docs.rs/tracing/latest/tracing/) stvarno modelira structured events, scoped spans, causal parent i subscriber/filter; [structlog](https://www.structlog.org/en/stable/) stvarno koristi event dictionaries, bound context i processor pipeline. [OpenHands tool/event architecture](https://docs.openhands.dev/sdk/arch/tool-system) potvrđuje Action/Observation evente, a njegov core event-stream opis potvrđuje centralni hub. OTel i OpenHands core su Apache-2.0/MIT prema svojim repozitorijima; `tracing` je MIT; structlog je MIT/Apache-2.0. Njihovi runtimi nisu kernel dependency; official OTel Go exporter smije biti adapter tek nakon parity testa.

- **Pareto floor:** **PASS za 1.3.** OTel record/correlation, structlog/tracing structured-context ergonomija i OpenHands central-event ideja ostaju, uz stroži P0.3 single-writer/redaction/integrity ugovor. Sinteza je bolja od NEXUSv2 jer replay ne mijenja trace ID, exporter failure ne gubi cursor i nijedan logger ne zaobilazi journal. Najjači kontraargument je da appendanje svakog debug eventa može opteretiti journal: filter se zato izvršava prije payload konstrukcije, ali odluka o sampling/dropu mora biti policy događaj i nikad ne smije obuhvatiti state/security/audit klase. Performance floor mora biti izmjeren; bez benchmarka throughput/latency ostaje proof ceiling.

## S1 izlazni gate

S1 je spreman za S2 tek kada:

1. config matrix pokrije svaku precedence kombinaciju, per-key origin, hostile project config, hot-reload parse failure i generation pinning;
2. `GOOS=linux`, `darwin`, `windows` uz `CGO_ENABLED=0` kompajliraju sve S1 pakete, a native CI na sva tri OS-a pokrene path i child/grandchild cleanup testove;
3. kontrolirani process mutant koji ubije samo parent ostavi grandchild i učini P1.2 gate RED, dok prava implementacija potvrđuje reap;
4. P0.3 bypass mutant izravno piše file/OTLP i pouzdano čini `test_projection_cannot_bypass_journal` RED;
5. file i OTLP golden vectors nose isti source event, correlation IDs, redaction i schema version;
6. race/fuzz testovi pokriju config reload, env/CLI parsere, path traversal/symlink/reparse/Windows device corpus te bounded stdout/stderr;
7. benchmark zabilježi config resolve/reload, journal-to-projection throughput i logging overhead s filterom uključenim/isključenim; pragovi se postavljaju tek iz izmjerenog baselinea.

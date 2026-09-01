PLAN S17+S18

# S17 — Ekstenzibilnost i distribucija

## Zajednički vlasnički rez: ekstenzija nije nova jezgra, a update nije `git pull`

- **Kernel ostaje zatvoren:** plugin može dodati alat, hook, provider ili UI adapter, ali ne može
  zamijeniti S0 envelope/state-machine, P0.3 journal, S6 PEP, S7 retry/cancel ni promotion gate.
  DeepSeek Harness/Cordis daje dobar DI+lifecycle uzor, ali njegov “everything is a plugin” nije
  dozvola da treća strana zamijeni sigurnosne vlasnike.
- **Jedan prenosiv model ekstenzije:** ugrađene ekstenzije su statički linkani Go adapteri; vanjske
  ekstenzije su procesno izolirani MCP serveri. Go `plugin` paket nije ABI ni cross-platform temelj
  (nije podržan na Windowsu i veže build-identitet), pa ga v1 namjerno nema. Novi in-process ABI
  ili WASM runtime uvodi se tek uz dokazanu potrebu i zaseban threat model.
- **Packs nisu pluginovi:** `packs/`, skills, role i agent manifesti ostaju inertni DATA sadržaj iz
  S11. Izvršni payload, install script ili deklarirani command odmah prelazi u S17.1 i prolazi
  S11.5→S6.8→P1.5 zatvaranje.
- **Artefakt je identitet odluke:** build, scan, attest, sign, publish, download i install moraju
  govoriti o istom digestu. TUF štiti metadata/dohvat; S17 i dalje posjeduje staging, atomsku
  aktivaciju, smoke, rollback i očuvanje prethodnog artefakta.
- **Paket-layout:** `internal/extensions/{manifest,registry,lifecycle,mcp}`,
  `internal/update/{tuf,channel,stage,activate}`, `internal/dist/{manifest,verify}` i
  `internal/fieldbridge/{bundle,consent,issue,upgrade}`. Adapteri nemaju drugi downloader,
  permission engine, retry loop, subprocess runner ni event sink.

```go
type ExtensionKind uint8
const (
    ExtensionBuiltin ExtensionKind = iota
    ExtensionMCPProcess
)

type ExtensionManifest struct {
    SchemaVersion uint16
    ID, Version, Kind, EntryPoint string
    ArtifactID, ArtifactDigest, ManifestDigest string
    HostAPIRange, MCPVersion string
    Provides, Requires, Conflicts []string
    RequestedPermissions []string
    ConfigSchemaDigest string
}

type ExtensionState uint8
const (
    ExtensionDiscovered ExtensionState = iota
    ExtensionVerified
    ExtensionGranted
    ExtensionStarting
    ExtensionReady
    ExtensionDraining
    ExtensionStopped
    ExtensionRevoked
)

type Extension interface {
    Manifest() ExtensionManifest
    Start(context.Context, HostServices) (ExtensionInstance, error)
}

type HostServices interface {
    RegisterTools(context.Context, []ToolDescriptor) error
    SubscribeEvents(context.Context, EventFilter) (<-chan EventView, error) // read-only projection
    RequestEffect(context.Context, EffectDraft) (EffectReceipt, error)     // uvijek kroz S6/S7
}
```

**Globalne invarijante S17:**

1. Discovery nije trust. Nijedan byte nije loadan ni izvršen prije S11.5 pinanja, S6.8 skena,
   P1.1 hash-on-load provjere i P1.5 artefakt-verifikacije.
2. Ekstenzija dobiva capability-scoped `HostServices`, nikad raw store, journal writer, key material,
   executor ili policy mutator. Ne može registrirati isti capability dvaput ni proširiti grant nakon
   starta bez nove odluke.
3. Disable/revoke prvo zatvara nove pozive, zatim kroz S7 deadline drenira ili otkazuje aktivne,
   uklanja registracije i proces. Crash plugin procesa je observation, ne crash hosta.
4. Update channel (`stable`, `beta`, `nightly`, custom) bira dopuštene targete, ali nikad ne preskače
   expiry, threshold signatures, hash/length, platform, permission-delta ili scan policy.
5. “Update dostupan”, “download verificiran”, “staged”, “activated”, “smoke passed” i “committed”
   odvojena su stanja. Mrežna ili smoke greška ostavlja aktivni binary i podatke netaknute.
6. Isti izgrađeni digest prolazi promotion. Release job ne rebuilda nakon testa; installer mora
   potvrditi da je instalirani byte identičan objavljenom targetu.

## 17.1 Izvršni plugin sustav i zatvoreni host ABI

- **Tip:** MEHANIZAM REGISTRYJA/LIFECYCLEA + ADAPTER ZA MCP; STATIČKI BUILTIN ADAPTERI

- **Aspekti:** DeepSeek Harness/Cordis daje eksplicitne servise, dependency injection i teardown
  lifecycle; goose daje praktičan MCP procesni rub za vanjske ekstenzije; OpenCode daje hook/tool
  plugin ergonomiju. Naš hibrid uzima njihove proširive rubove, ali ne Cordisovu maksimalnu
  zamjenjivost kernela: sigurnosni i durable vlasnici ostaju nepromjenjivi Go kod.

- **Sinteza:** `ExtensionRegistry` registrira samo manifestom deklarirane i closure-validirane
  capabilityje. Builtin implementacije ulaze compile-time listom; vanjski plugin pokreće se kao MCP
  subprocess isključivo kroz S4.2/S6.2/S7.1. Registry daje svakoj instanci capability-scoped proxyje,
  journal subscription bez write metode i effect API koji ponovno prolazi PEP. S0.5 resolver prije
  aktivacije računa zavisnosti i obvezno uključuje `extensions`, S11.5 i S6.8.

```go
type ExtensionRegistry interface {
    Discover(context.Context, []ArtifactRef) ([]ExtensionManifest, error)
    Verify(context.Context, ExtensionManifest) (VerificationReceipt, error)
    Activate(context.Context, ExtensionManifest, GrantSet) (ExtensionHandle, error)
    Drain(context.Context, ExtensionID) error
    Revoke(context.Context, ExtensionID, string) error
}

type ExtensionHandle struct {
    ID, Version, ArtifactDigest, GrantDigest string
    InstanceID string
    State ExtensionState
    Process s7.ProcessRef // samo za MCP; host ga ne izlaže pluginu
}

type MCPProcessSpec struct {
    CommandRef string       // registry-resolved, nije shell string
    Args []string
    EnvAllowlist []string
    ProtocolVersion string
    ToolSchemaDigests []string
    ResourceBudget s7.ResourceBudget
}
```

  Tool registracija je transakcijska: validiraj sve deskriptore i kolizije, zatim objavi cijeli set;
  djelomična registracija se rollbacka. MCP handshake mora potvrditi pinanu verziju i schema digest.
  Hot reload nije v1 zahtjev; update ekstenzije radi drain→verify novi artifact→start→health→swap,
  s prethodnom verzijom zadržanom do uspješnog smokea.

- **Salvage:** iz `/home/matej/NEXUSv2/core/pack_registry.py:43-99,122-204` Python→Go portirati
  bounded manifest read, zabranu symlinka/path escapea, duplicate-field/unknown-field validaciju i
  unique capability owner; iz `pack_registry.py:215-276` portirati doctor provjere kao DATA-pack
  preflight. **Ne portirati kao plugin ABI:** taj registry namjerno “never run manifest-supplied
  commands” (`:215-216`) i zato dokazuje samo siguran DATA loader. `packs/` ostaje S11 sadržaj.
  GORTEX/S4 subprocess i S6 grant membrane se direktno koriste, ne dupliciraju.

- **Ugovor/RED:** P0.4 closure, P1.1 skill/artifact hash-on-load, P1.5 artifact closure, S6.8 i
  S11.5. Glavni RED `test_plugin_bytes_swapped_after_scan_never_execute` zamijeni payload između
  skena i `Start`; runtime hash mora vratiti `ARTIFACT_DIGEST_MISMATCH`, bez pokrenutog procesa i bez
  tool registracije. Dodatno: `test_plugin_cannot_register_journal_writer`,
  `test_duplicate_capability_activation_is_atomic`, `test_revoked_plugin_drains_then_loses_grants`
  i `test_mcp_crash_is_observation_not_host_crash`.

- **Verifikacija:** [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) stvarno je
  developer-preview plugin arhitektura nad Cordisom; njegov
  [Cordis primer](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/cordis-primer.md)
  potvrđuje services/context/injection/lifecycle obrazac. goose stvarno koristi MCP ekstenzije, a
  OpenCode ima plugin/hook površinu. To ne dokazuje siguran Go ABI: naš MCP containment, hash-on-load,
  capability proxy i teardown moraju imati vlastite integration i process-leak testove na sva 3 OS-a.

- **Pareto-floor:** **PASS uz suženje Cordis ideje.** Zadržava Cordisovu DI/lifecycle jasnoću,
  gooseovu interoperabilnost i OpenCode ergonomiju, ali ima manju trust površinu i stvarnu
  Windows/Linux/macOS izvedivost. Najjači protuargument je cijena subprocessa; prihvatljiva je jer
  treća strana prelazi trust boundary, dok hot-path adapteri mogu biti audited static builtins.

## 17.2 TUF update mehanizam i release kanali

- **Tip:** MEHANIZAM SIGURNOG UPDATE STATE-MACHINEA + TUF KLIJENT + TANKI CHANNEL ADAPTER

- **Aspekti:** TUF daje root/targets/snapshot/timestamp metadata, threshold potpise te rollback,
  freeze i mix-and-match zaštitu; Codex CLI daje eksplicitne release kanale; Aider/goose daju
  neblokirajući version-check i korisnički pokrenut upgrade. TUF ne daje atomsku aktivaciju ni
  aplikacijski rollback, pa ih posjeduje naš updater.

- **Sinteza:** binary nosi trusted TUF root i `UpdatePolicy`. Kanal se mapira na delegated target
  role, a target mora točno odgovarati OS/arch/edition/capability setu. Check je cacheiran,
  timeout-bounded i opt-out; nikad ne blokira CLI startup. Install je side-by-side: download u novu
  content-addressed putanju, P1.5 verify, migration/compatibility preflight, smoke novog binaryja,
  pa atomic pointer/launcher swap. Windows ne pokušava prepisati aktivni `.exe`; promjenu aktivira
  mali postojeći launcher ili sljedeći start. Prethodni target ostaje rollback kandidat.

```go
type UpdateChannel string
const (
    ChannelStable UpdateChannel = "stable"
    ChannelBeta UpdateChannel = "beta"
    ChannelNightly UpdateChannel = "nightly"
)

type UpdateState uint8
const (
    UpdateIdle UpdateState = iota
    UpdateMetadataVerified
    UpdateTargetSelected
    UpdateDownloaded
    UpdateArtifactVerified
    UpdateStaged
    UpdateSmokePassed
    UpdateActivated
    UpdateCommitted
    UpdateRolledBack
)

type UpdatePlan struct {
    CurrentVersion, TargetVersion string
    Channel UpdateChannel
    Platform, ArtifactDigest string
    CurrentSchema, TargetSchema uint32
    PermissionDelta []string
    TUFMetadataVersions map[string]uint64
    MetadataExpiry time.Time
}

type Updater interface {
    Check(context.Context, UpdateChannel) (UpdatePlan, error)
    Stage(context.Context, UpdatePlan) (StagedArtifact, error)
    Activate(context.Context, StagedArtifact) (UpdateReceipt, error)
    Rollback(context.Context, UpdateReceipt) error
}
```

  Root rotation slijedi TUF threshold/version pravila; expiry se nikad ne ignorira. Nepouzdan sat
  daje fail-closed `TRUSTED_TIME_UNAVAILABLE` i operatoru objašnjava recovery put, ali ne uvodi
  `--skip-signature`. Permission delta zahtijeva novu S6 odluku prije aktivacije. Automatski download
  može biti opt-in; automatski install je default OFF. Rollback aplikacije dopušten je samo ako S0.4/
  18.4 kompatibilnost podataka dopušta starom binaryju čitanje trenutne sheme; inače roll-forward.

- **Salvage:** iz `/home/matej/NEXUSv2/core/update.py:29-34,53-67,95-160` Python→Go portirati
  terminal scrub, trusted-config opt-out, bounded/cacheirani best-effort check i safe notice. Iz
  `update.py:180-213` zadržati UX provjere “explicit upgrade” i jasnu offline grešku. **Ne portirati**
  `git fetch` + `merge --ff-only`: to nema TUF metadata, SBOM/provenance/permission-delta closure,
  pokreće povučeni kod u smokeu i nema dokazani atomski rollback.

- **Ugovor/RED:** P1.5 je obvezni gate; P0.4 zatvara `distributed-artifact` na S6.8+17.2+17.3;
  S7 posjeduje timeout/cancel. Glavni RED `test_post_signature_mutation_never_reaches_install_target`
  mijenja jedan byte staged targeta nakon potpisa; rezultat mora biti `ARTIFACT_DIGEST_MISMATCH`, a
  aktivni binary byte-identičan starom. Dodatno: expired timestamp/snapshot, niža target verzija,
  mixed metadata verzije, novi permission bez approvala, Windows running-binary swap i smoke failure
  moraju ostaviti staru verziju aktivnom.

- **Verifikacija:** [TUF specifikacija](https://theupdateframework.github.io/specification/) potvrđuje
  metadata role, expiry, threshold, hash/length i rollback/freeze/mix-and-match obrane. Codex/Aider/
  goose potvrđuju channel/upgrade UX, ne našu sigurnost. Integracijski verifier mora koristiti
  lokalni TUF repository fixture i kontrolirane mutacije; clean VM testovi moraju potvrditi install,
  offline check, interrupted activation i rollback zasebno za Windows/Linux/macOS.

- **Pareto-floor:** **PASS.** TUF je jači sigurnosni temelj od svakog kandidatskog self-updatera,
  a side-by-side activation dodaje ono što TUF svjesno ne rješava. Cijena dodatnih metadata rola je
  opravdana samo za distribuirane artefakte; development build bez release kanala ne aktivira ovaj
  profil.

## 17.3 Reproducibilan Go single-binary packaging i promotion

- **Tip:** MEHANIZAM ARTEFAKT-MANIFESTA + BUILD/RELEASE ADAPTER; VANJSKI RELEASE ALATI

- **Aspekti:** goose demonstrira stvarno single-binary korisničko iskustvo; `dist` daje generički
  cross-platform release CI, arhive/installere i manifest; Astral uv pokazuje snažan runtime/package
  bootstrap, ali je Python-rješenje i zato nije primarni put za Go proizvod. Go toolchain daje
  `CGO_ENABLED=0`, build metadata i cross-compile; Syft/Cosign/attestation adapteri popunjavaju P1.5
  SBOM, signer i provenance, ne postaju drugi release owner.

- **Sinteza:** `cmd/nexus` i Bubble Tea TUI grade se iz iste jezgre kao čisti Go binary. Release
  matrix proizvodi OS/arch targete jednom, zapisuje `ArtifactManifest`, generira SBOM+provenance,
  skenira, potpisuje i tek isti digest promovira kanalima. Default asseti i migracije embedaju se;
  korisnički podaci/config nisu u binaryju. `dist` je primarni release-orchestrator adapter ako
  generički Go project lane zadovolji parity matricu; GoReleaser je runner-up, ne drugi paralelni put.

```go
type BuildIdentity struct {
    SourceRevision, SourceTreeDigest string
    GoVersion, ModuleGraphDigest string
    BuildRecipeDigest, BuilderIdentity string
    GOOS, GOARCH string
    CGOEnabled bool
}

type ReleaseArtifact struct {
    Manifest p15.ArtifactManifest
    Build BuildIdentity
    BinaryPath, ArchivePath string
    SBOMPath, ProvenancePath string
}

type ReleasePipeline interface {
    Build(context.Context, BuildIdentity) ([]ReleaseArtifact, error)
    Verify(context.Context, ReleaseArtifact) (VerificationReceipt, error)
    Promote(context.Context, ReleaseArtifact, UpdateChannel) (PublishReceipt, error)
}
```

  Build koristi zaključani `go.mod/go.sum`, `-trimpath`, deterministički version injection i praznu
  mrežu nakon dependency-fetch faze. `CGO_ENABLED=0` je gate za CLI/TUI/service izdanje; Wails desktop
  je zaseban platform paket i ne smije se lažno nazvati single-binary univerzalnim artefaktom.
  Installeri najprije verificiraju target pa pišu novu versioned putanju; nikad `curl | sh` bez
  unaprijed verificiranog metadata/digesta. Release provenance razlikuje source build od repackaginga.

- **Salvage:** nema Python→Go packaging koda koji vrijedi portirati. Iz NEXUS release konvencija
  zadržati samo popis ulaznih asseta i verzijski UX nakon provjere stvarnog repoa; `packs/` se
  distribuira kao zasebni P1.5 artefakt ili se embedano veže u glavni manifest. NEXUS `core/update.py`
  nije packaging dokaz.

- **Ugovor/RED:** P1.5 ArtifactManifest i state-machine su gate, uz S6.8 scan i S17.2 TUF publish.
  Glavni RED `test_release_rebuild_after_green_cannot_be_promoted` testira revision A, zatim rebuilda
  A drugim recipe digestom; promotion mora odbiti novi digest s `EVIDENCE_ARTIFACT_MISMATCH`.
  Dodatno: clean-host install bez Go/Python runtimea, tampered archive, pogrešan OS/arch, SBOM za
  drugi digest i reinstall/rollback moraju imati determinističke ishode.

- **Verifikacija:** [`dist`](https://axodotdev.github.io/cargo-dist/) stvarno proizvodi release CI,
  instalere i cross-platform artefakte za podržane project modele; goose potvrđuje single-binary UX,
  a uv je samo komparativni packaging izvor. Naš claim ostaje uvjetan do reproducibilnog double-builda
  u dva čista buildera i clean-VM install matrice; “Go se cross-compila” nije dokaz da asseti,
  certifikati, quarantine/notarization i updater rade.

- **Pareto-floor:** **PASS uz jedan release lane.** Dobiva goose jednostavnost i `dist` automatizaciju,
  uz P1.5 identitet koji kandidati sami ne garantiraju. Ne uvodimo istovremeno `dist` i GoReleaser;
  bira se jedan nakon matrix proofa.

## 17.4 Opt-in field diagnostics → issue → verificirani upgrade bridge (`service`)

- **Tip:** JEZGRENI MEHANIZAM LOKALNOG BUNDLEA/CONSENTA + SERVICE ISSUE/UPLOAD ADAPTERI

- **Aspekti:** NEXUS bugreport daje local-first scrubbed bundle koji korisnik ručno pregleda;
  GlitchTip/Sentry daju issue grouping i release correlation; OTel Collector daje vendor-neutralan
  transport. Naš bridge spaja te aspekte, ali telemetry sink ne dobiva automatski upload ni update
  authority.

- **Sinteza:** snapshotter iz P0.3 projekcija i allowlisted diagnostics izvora gradi kanonski,
  bounded payload. Scrubber radi po strukturiranim poljima, zatim value-pattern i canary pass nakon
  finalne serializacije. Arhiva normalizira redoslijed/mtime i dobiva digest; korisnik pregleda točno
  taj staged digest i eksplicitno odobrava destinaciju. Issue receipt veže bundle digest, consent i
  correlation ID. `UpgradeSuggestion` može referencirati samo TUF-verificirani S17.2 target čiji
  release metadata tvrdi da ispravlja issue; issue attachment nikad nije executable source.

```go
type DiagnosticBundle struct {
    BundleID, PayloadDigest, SchemaVersion string
    AppVersion, ArtifactDigest, Platform string
    CorrelationIDs []string
    IncludedSections, RedactedFields []string
    ContainsSensitiveMemory bool
    SizeBytes uint64
}

type ConsentReceipt struct {
    BundleDigest, Destination, PrincipalID string
    ApprovedAt, ExpiresAt time.Time
    PolicyHash, ReceiptDigest string
}

type IssueReceipt struct {
    IssueID, URL, BundleDigest, CorrelationID string
    RemoteReceipt string
}

type UpgradeSuggestion struct {
    IssueID, CurrentVersion, MinimumFixedVersion string
    TUFRole, TargetDigest string
}
```

  Bundle generation nema mrežni pristup. `Submit` prima immutable bundle handle+consent receipt i
  tek tada koristi S6.3/P2.5-style data policy, S7 retry i S7.3 outbox. Slanje i instalacija su dva
  odvojena approvala; nijedno nije default. Bundle ne uključuje prompt/transcript/memory osim posebno
  označenog opt-ina. Determinističnost znači isti frozen snapshot→isti payload digest; consent vrijeme
  nije dio payload identiteta.

- **Salvage:** korisnikov naziv `core/bugreport.py` ne postoji u auditiranom NEXUSv2; stvarni owner je
  `/home/matej/NEXUSv2/managers/bugreport.py`. Iz `managers/bugreport.py:1-24,34-102` Python→Go
  portirati key+value scrub, depth/size cap, failed-run izbor, trusted config redaction i “review by
  hand”; iz `:105-113` zadržati generički issue URL bez automatskog sadržaja. Iz
  `/home/matej/NEXUSv2/core/export.py:48-117,235-299` portirati fail-closed config redaction,
  archive bounds i SQLite integrity check gdje su primjenjivi. Ne portirati swallowed audit failure
  ni pretpostaviti da scrubber daje apsolutnu tajnost.

- **Ugovor/RED:** P1.4 za vanjsko slanje, P1.5 za ponuđeni update, S6.3/S6.4/S6.7 i S7.3 za upload;
  P0.3 za correlation. Glavni RED `test_final_serialized_canary_blocks_upload_before_network`
  ubacuje canary kroz nested traceback tako da preživi prvi scrub; final gate mora vratiti
  `DIAGNOSTIC_DLP_BLOCKED` i network spy mora vidjeti nula pokušaja. Dodatno: upload bez pristanka,
  izmjena bundlea nakon pristanka, issue s arbitrary binary linkom i auto-install moraju biti RED.

- **Verifikacija:** lokalni NEXUS kod potvrđuje local-first bundle i višeslojni scrub. Sentry/
  GlitchTip i OTel potvrđuju correlation/transport mogućnosti, ne dokaz da naš bundle ne sadrži
  tajnu. Dokaz zahtijeva adversarial corpus (nested JSON, ANSI, base64-like vrijednosti, paths,
  prompt/memory), final-byte canary test i zero-network spy prije approvala.

- **Pareto-floor:** **PASS uz local-first granicu.** Zadržava NEXUS privatnost, observability
  korelaciju i verificirani recovery put. Najjači protuargument je da scrub nikad nije savršen;
  zato default bundle izostavlja slobodni sadržaj, korisnik pregleda immutable bytes, a upload ostaje
  zasebna ireverzibilna odluka.

# S18 — Deployment i operacije (`service` capability flag)

## Zajednički vlasnički rez: service je adapter nad istom jezgrom, ne drugi harness

- **Uvjetna aktivacija:** `service` closure najmanje zahtijeva `headless`, authn/tenant S6.6,
  governance S6.7, S7 queue/outbox, S14.2 API, S15 metrics/audit, S17 distribuirani artefakt i S18.4
  migration/backup readiness. `multi-agent`, `channels` i retrieval flagovi dodaju svoje closuree;
  nisu implicitno uključeni.
- **Nema paralelnih ownera:** S18.1 ne implementira retry, lease ili fencing; mapira S7 ugovor na
  odabrani broker/workflow backend. S18.2 ne implementira provider adapter; posjeduje authenticated
  tenant ingress i delegira remote-provider data policy S2/S6. S18.3 čita S15 projekcije i upravlja
  rolloutom. S18.4 posjeduje schema compatibility i recovery, ali ne application writes.
- **Tenant je auth rezultat:** `TenantID` i `PrincipalID` dolaze iz verificiranih S6.6 claimova.
  `chat_id`, API path, header koji klijent sam bira, workspace path ili model-producirani tekst nikad
  nisu tenant granica.
- **Lokalni i service backend nisu lažno isti:** SQLite/modernc ostaje local spine. Horizontalni
  service persistence backend nije izabran u S0.4, pa 18.4 ostaje legalno `UNCLEAR`; do odluke se
  grade samo sučelja i contract test kit, ne lažni distributed SQLite.
- **Paket-layout:** `internal/service/{worker,gateway,health,rollout,migration,backup}` i backend
  adapteri u `internal/service/adapters/{temporal,litellm,kubernetes,argo,flagger}`.

```go
type TenantContext struct {
    TenantID, PrincipalID, AuthSessionID string
    Roles, Scopes []string
    PolicyHash, ResidencyPolicyID, BudgetPolicyID string
    AuthenticatedAt, ExpiresAt time.Time
}

type ServiceBackend interface {
    DistributedQueue() s7.LeasedQueue
    TenantState() TenantStateStore
    MigrationStore() MigrationStore
    BackupStore() BackupStore
}
```

**Globalne invarijante S18:**

1. Svaki durable row, object, cache key, queue item, outbox record, lock, budget reservation i audit
   event nosi server-derived `TenantID`; missing/unknown tenant je deny, ne globalni namespace.
2. Worker commit zahtijeva aktualni S7 fencing token. HTTP retry, worker restart ili broker redelivery
   ne smiju duplicirati ireverzibilni effect ni rezultat.
3. Deploy target je P1.5-verificirani digest koji je prošao S16 gate. Canary ne rebuilda i ne povlači
   mutable tag; rollout receipt veže exact image/binary, config, schema i policy digest.
4. Schema/data promjena nikad pretpostavlja da se stari binary može vratiti. Rollback je dopušten
   samo uz dokazanu backward compatibility; inače se zaustavlja rollout i radi roll-forward.
5. Backup nije uspješan dok immutable manifest, integrity, encryption policy i periodični restore
   drill ne potvrde čitljiv recovery do deklariranog RPO/RTO.

## 18.1 Headless API/worker razdvajanje, queue/lease i horizontalno skaliranje

- **Tip:** SERVICE ADAPTER + DISTRIBUTED QUEUE BACKEND; S7 OSTAJE JEDINI MEHANIZAM UGOVORA

- **Aspekti:** OpenHands daje concurrent session/worker proizvodni obrazac; Dify daje odvojene API i
  worker procese; Temporal daje durable workflow/task redelivery. Naš hibrid koristi te deployment
  topologije, ali svaku implementaciju mora mapirati na P2.3 lease/heartbeat/reclaim/fencing i N9
  outbox, bez pretpostavke da naziv “durable workflow” sam zadovoljava ugovor.

- **Sinteza:** API validira S0 envelope, S6 tenant/policy i budget reservation, zatim atomically
  enqueua `WorkItem` i outbox/journal događaj. Worker claim radi preko S7 `LeasedQueue`; heartbeat samo
  obnavlja lease, a commit radi CAS nad `LeaseID+FencingToken`. Procesna liveness, task heartbeat i
  semantic progress su tri različita signala. Graceful drain prestaje claimati, završava/otkazuje
  aktivne pokušaje unutar deadlinea i vraća nedovršene leaseove za siguran reclaim.

```go
type WorkerAPI interface {
    Submit(context.Context, TenantContext, contracts.Envelope) (s7.QueueReceipt, error)
    Status(context.Context, TenantContext, string) (RunView, error)
    Cancel(context.Context, TenantContext, string) (s7.CancelReceipt, error)
}

type Worker struct {
    ID, BuildDigest string
    Queue s7.LeasedQueue
    Runtime Application
}

type WorkerAttempt struct {
    TenantID, TaskID, AttemptID, WorkerID string
    LeaseID string
    FencingToken uint64
    Deadline time.Time
}
```

  Temporal adapter prevodi workflow/activity identitete u naš receipt, ali application state commit
  i dalje provjerava naš monotoni fencing token; Temporal attempt ID sam nije autorizacija. Broker
  bez atomic claim+fencing primitive nije kompatibilan adapter. Per-subagent worktree i cleanup
  ostaju S5.2/P1.2, a result delivery S7.3 — worker ne zove kanal izravno.

- **Salvage:** iz `/home/matej/NEXUSv2/core/queue.py:16-68` može se portirati samo minimalna task
  schema i ideja atomic pending→running CAS za local mode. **Ne portirati u service:** `:13,49-68`
  nema lease expiry, heartbeat, reclaim ni fencing, pa `running` nakon pada ostaje orphan; `:71-95`
  tretira delivery kao best-effort i nema transactional outbox. Iz `core/fleet.py` portirati korisne
  run/worker identitete tek nakon parity provjere s P2.3.

- **Ugovor/RED:** P0.2 retry owner, P1.2 startup orphan sweep, P2.2 budgets, P2.3 queue lease/fencing
  i S7.3 durable delivery. Glavni RED je Annex scenarij: worker A s tokenom 7 se zamrzne, lease istekne,
  worker B CAS-reclaima token 8 i commita; kasni A commit mora vratiti `STALE_FENCE` bez state/effect
  promjene. Dodatno: API timeout+retry ne duplicira task, worker SIGKILL se reclaima, drain ne uzima
  novi rad i `RUN_DONE` bez `RESULT_DELIVERED` ostaje deliverable u outboxu.

- **Verifikacija:** OpenHands/Dify potvrđuju API+worker deployment uzor, Temporal durable execution,
  ali nijedan naziv nije dokaz našeg fencing invariant-a. Adapter conformance suite mora se izvršiti
  nad stvarnim backendom s dva procesa, kontroliranim zamrzavanjem, satom i killom; in-memory test ne
  dokazuje multi-host CAS ni recovery.

- **Pareto-floor:** **PASS samo uz S7 conformance.** Zadržava proizvodnu topologiju kandidata i
  Temporal durability, a dodaje eksplicitni stale-writer guard koji NEXUS nema. Ako jedan-process
  deployment zadovoljava opterećenje, service koristi isti API s local queueom i ne uvodi Temporal.

## 18.2 Authenticated multi-tenant gateway: kvote, ključevi, budžeti i rezidencija

- **Tip:** SERVICE MEHANIZAM TENANT GRANICE + ADAPTERI ZA PROVIDER GATEWAY/RATE STORE

- **Aspekti:** LiteLLM Proxy daje virtual keys, provider routing i spend/rate tracking; Portkey daje
  provider governance; Dify daje workspace/tenant kvote. “Per-user workspace kao jedna jedinica”
  daje važniji invariant od feature popisa: memory, files, key refs, permissions, cron i budget imaju
  isti server-derived tenant partition.

- **Sinteza:** ingress prvo autentificira, zatim derivira immutable `TenantContext`; tek poslije
  parsira workload. `TenantGateway` rezervira rate/cost/concurrency budžet atomically prije enqueuea,
  izdaje scoped secret reference i primjenjuje data/residency policy prije svake provider serializacije.
  LiteLLM/Portkey mogu biti outbound provider-proxy adapteri, ali ne autentificiraju naš application
  tenant niti postaju source of truth za budgets. Strict/Auto/Dangerous posture je policy preset
  unutar operatorova maksimuma; `Dangerous` ne može isključiti kernel deny pravila.

```go
type TenantWorkspaceKey struct {
    TenantID, WorkspaceID string
}

type BudgetReservation struct {
    ReservationID, TenantID, PrincipalID string
    RouteID, ProviderID string
    MaxCostMinorUnits, MaxTokens, MaxConcurrency uint64
    ExpiresAt time.Time
    FencingToken uint64
}

type TenantGateway interface {
    Authenticate(context.Context, AuthInput) (TenantContext, error)
    Authorize(context.Context, TenantContext, contracts.Envelope) (PolicyDecision, error)
    Reserve(context.Context, TenantContext, BudgetRequest) (BudgetReservation, error)
    Dispatch(context.Context, TenantContext, BudgetReservation, contracts.Envelope) (RunReceipt, error)
}
```

  Svi store interfacei zahtijevaju tenant key kao prvi argument i rade deny-default ako nedostaje.
  Secret broker vraća scoped handle, nikad plaintext u config/prompt/log. Svaki S2 fallback ponovno
  evaluira P2.5 s originalnim request policyjem i novim provider descriptorom. Cache key uključuje
  tenant/principal/policy hash (P2.4). Distributed limiter mora imati atomic reserve/settle/release;
  procesni mutex nije service implementacija.

- **Salvage:** `/home/matej/NEXUSv2/gateway/base.py:68-119` daje koristan deny-default allowlist,
  a `:10-27,91-101,119-159` outbound scrub, ordered split i artifact-send granicu; portirati samo kao
  S14 channel adapter. **Ne portirati kao tenant gateway:** `_allowed(chat_id)` i
  `NEXUS_OPEN_GATEWAY=1` (`:103-117`) nisu authn/tenant boundary. NEXUS config/provider key handling
  može dati UX, ali tenant secret izolacija zahtijeva novi S6.6 broker contract.

- **Ugovor/RED:** P2.4 tenant cache, P2.5 provider-data processing/residency, P2.2 bounded budgets,
  S6.6/S6.7 i P0.4 closure. Glavni RED `test_chat_id_cannot_select_tenant_partition` šalje dva
  verificirana principala s istim attacker-controlled chat ID-em; drugi ne smije vidjeti prvi memory,
  file, key, budget ni cache entry. Dodatno: EU-only zahtjev s US fallbackom daje nula network poziva;
  20 paralelnih reservations na preostali budget dopušta najviše atomically raspoloživi iznos.

- **Verifikacija:** LiteLLM/Portkey/Dify potvrđuju gateway/workspace/spend uzore, ali ne našu cijelu
  isolation jedinicu. Conformance koristi dva tenanta i pokušava IDOR kroz svaku tablicu, object key,
  queue, cache, metrics label, backup i error path; network spy dokazuje P2.5 pre-connect deny, a race
  test atomsku budget rezervaciju.

- **Pareto-floor:** **PASS uz vlastiti tenant owner.** Kandidatski gatewayi ostaju korisni outbound
  adapteri, ali ne raspršujemo auth/budget istinu između njih. Najjači protuargument je potreba za
  distribuiranim rate-storeom; backend ostaje adapter, ali atomic reservation contract nije opcionalan.

## 18.3 Liveness/readiness/startup, rollout, canary i rollback odluka

- **Tip:** SERVICE MEHANIZAM HEALTH UGOVORA + KUBERNETES/ARGO/FLAGGER ADAPTERI

- **Aspekti:** Kubernetes razlikuje startup, readiness i liveness probe; Argo Rollouts daje
  versionirane canary korake/analysis i abort/rollback; Flagger daje metrički vođenu progresiju.
  NEXUS readiness mapa daje dobru fail-closed preflight ideju, ali nema probe semantiku ni rollout
  state-machine.

- **Sinteza:** `/livez` odgovara samo je li procesni event loop sposoban napredovati; ne zove remote
  providere ni migracije. `/startupz` ostaje false dok lokalna inicijalizacija, recovery i schema
  check ne završe. `/readyz` provjerava može li instanca sigurno primiti **novi** rad: journal write,
  queue claim, required policy/key backend i compatibility; degradirani opcionalni provider routa se
  kroz S2, ne ruši sve podove. Rollout koristi immutable P1.5 digest, versionirani plan i S15 metric/
  semantic-health projekcije. Svaka faza ima min sample+duration i precizan threshold operator.

```go
type ProbeKind uint8
const (
    ProbeStartup ProbeKind = iota
    ProbeReadiness
    ProbeLiveness
)

type HealthReceipt struct {
    InstanceID, BuildDigest, ConfigHash, SchemaVersion string
    Kind ProbeKind
    Healthy bool
    Checks []HealthCheck
    ObservedAt time.Time
}

type RolloutPlan struct {
    PlanID, TargetDigest, PreviousDigest string
    ConfigHash, MigrationPlanDigest, MetricPolicyDigest string
    Steps []CanaryStep
    RollbackCompatibility CompatibilityDecision
}

type RolloutState uint8
const (
    RolloutPlanned RolloutState = iota
    RolloutPreflighted
    RolloutCanary
    RolloutPaused
    RolloutPromoted
    RolloutAborted
    RolloutRolledBack
    RolloutNeedsRollForward
)
```

  Canary evaluator čita frozen window iz P0.3/S15, ne mutable dashboard snapshot. Boundary je
  normativan (`error_rate_ppm > max`, ne nejasno “oko 1%”). Liveness failure ne služi kao semantic
  model-health signal; 15.5 može otvoriti S7 circuit ili zaustaviti rollout bez restart-loopa.
  Rollback najprije pita 18.4 compatibility; ako stari binary ne može čitati novu shemu, stanje je
  `NEEDS_ROLL_FORWARD`, nikad slijepi pod rollback.

- **Salvage:** iz `/home/matej/NEXUSv2/core/deploy.py:6-32` Python→Go portirati bounded check-map i
  fail-closed `ready=all(checks)` kao početni preflight. Ne portirati jednu `readiness()` funkciju kao
  sve probe: provider/packs/gortex provjere moraju biti klasificirane, timeout-bounded i bez side
  effecta. Iz `core/health.py` portirati semantic signale samo u S15.5 projekciju, ne u `/livez`.

- **Ugovor/RED:** P1.5 artifact identity, P0.3 journal projection, S15.4/S15.5 i S18.4 compatibility.
  Glavni RED `test_canary_threshold_boundary_and_incompatible_rollback` daje error rate točno na
  dopuštenom maksimumu (ne smije abortirati), zatim jedan iznad i schema odluku
  `OLD_BINARY_CANNOT_READ`; rollout mora stati u `NEEDS_ROLL_FORWARD`, bez pokretanja starog poda.
  Dodatno: provider outage ne smije uzrokovati liveness restart storm, startup ne prima promet, a
  mutable image tag različitog digesta pada preflight.

- **Verifikacija:** [Kubernetes probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes)
  potvrđuju odvojene startup/readiness/liveness uloge; [Argo Rollouts](https://argo-rollouts.readthedocs.io/en/stable/)
  i [Flagger](https://docs.flagger.app/main/usage/how-it-works) potvrđuju progressive delivery i
  metric rollback obrasce. Njihova prisutnost nije dokaz dobrih pragova: kind/env integration test
  mora ubaciti metric failure, pod kill, dependency outage i incompatible schema transition.

- **Pareto-floor:** **PASS uz eksplicitnu probe semantiku.** Zadržava standardne operatore i dodaje
  typed compatibility odluku. Ne gradimo vlastiti orchestrator: bez Kubernetes deploymenta isti
  health contract ostaje API/CLI preflight, a Argo/Flagger adapteri nisu aktivni.

## 18.4 Backend-vezane state/schema migracije i dokazivi backup/restore

- **Tip:** MEHANIZAM MIGRATION/BACKUP UGOVORA + BACKEND ADAPTER; PRIMARNI SERVICE PICK `UNCLEAR`

- **Aspekti:** S0.4 daje version/compatibility pravila; SQLite online backup daje local consistent
  snapshot mehanizam; PostgreSQL PITR daje service-grade WAL recovery; Alembic i yoyo-migrations daju
  versionirane forward/rollback migracije. Nijedan je legalan primarni service pick dok S0.4 ne
  odabere store/topologiju. Python migration runner također bi bio operativni sidecar, ne dio Go
  single binaryja, pa mora pobijediti Go-native alternative u matrici.

- **Sinteza:** sada se pišu samo `MigrationCoordinator`, `BackupProvider` i shared conformance kit.
  Local modernc.org/sqlite adapter koristi embedded, checksummed Go migration registry. Service
  adapter dolazi nakon odluke SQLite-single-node nasuprot PostgreSQL-distributed; horizontalni
  multi-host deployment ne pretpostavlja shared SQLite. Migracije su expand→bounded resumable
  backfill→validate→cutover→contract, s compatibility matricom za `old/new app × old/expanded/new
  schema`. Destruktivni contract čeka dok nijedan podržani binary ne treba stari oblik.

```go
type MigrationPhase uint8
const (
    MigrationExpand MigrationPhase = iota
    MigrationBackfill
    MigrationValidate
    MigrationCutover
    MigrationContract
)

type Migration struct {
    ID string
    FromVersion, ToVersion uint32
    Checksum string
    Phase MigrationPhase
    Reversible bool
    MinReaderVersion, MinWriterVersion string
    Apply func(context.Context, MigrationTx, BackfillCursor) (BackfillCursor, error)
    Validate func(context.Context, MigrationTx) error
}

type MigrationReceipt struct {
    MigrationID, Checksum, BackendID string
    Phase MigrationPhase
    Cursor, InputDigest, OutputDigest string
    StartedAt, DurableAt time.Time
    State string // STARTED | CHECKPOINTED | VALIDATED | COMMITTED | FAILED
}

type BackupManifest struct {
    BackupID, TenantScope, BackendKind, BackendVersion string
    SchemaVersion uint32
    AppVersion, AppArtifactDigest string
    ConsistencyPoint, ParentBackupID string
    PayloadDigest, EncryptionKeyRef string
    CreatedAt time.Time
}

type BackupProvider interface {
    Create(context.Context, BackupRequest) (BackupManifest, error)
    Verify(context.Context, BackupManifest) (VerificationReceipt, error)
    RestoreToStaging(context.Context, BackupManifest) (RestoreHandle, error)
    PromoteRestore(context.Context, RestoreHandle) (RestoreReceipt, error)
}
```

  Migration lock ima owner/lease/fencing i startup sweep; drugi migrator ne može preskočiti partially
  applied korak. Svaki backfill ima bounded batch/resource budget i durable cursor. Backup se radi na
  konzistentnoj točki, encrypta prije udaljenog spremanja, manifest se potpisuje i testira restoreom.
  Restore je staging→integrity→schema compatibility/upcast→application smoke→atomic cutover; nikad
  ekstrakcija preko aktivnih podataka. Multi-tenant restore po defaultu ne može prepisati drugi tenant.
  RPO/RTO su mjerene politike, ne tvrdnja na temelju uspješnog archive writea.

- **Salvage:** iz `/home/matej/NEXUSv2/core/store.py:21,112-194,204-220` portirati
  `BEGIN IMMEDIATE` serialization, newer-schema fail i additive repair ideju za local SQLite, ali
  **ne** monolitni `_migrate` ni jedan `SCHEMA_VERSION=1` bez migration registryja. Iz
  `/home/matej/NEXUSv2/core/export.py:164-232` portirati SQLite online backup, 0600 temp,
  fsync+atomic replace i explicit sensitive-memory marker; iz `:235-299` archive path/count/size,
  duplicate-member i `quick_check` obrane; iz `:303-399` staging/refuse-first/pre-import recovery.
  Popraviti dokazne stropove: process-local lock ne zaustavlja druge writere, exact-schema check ne
  dopušta podržani upcast, a multi-file `os.replace` loop može ostaviti miješano stanje. `core/export.py`
  postaje mehanizam, ne dokaz da je service backup gotov.

- **Ugovor/RED:** S0.4 je prethodni owner verzioniranja; P1.2 lock/orphan sweep, P2.2 resource
  budgets, P1.5 backup/tool artefakti gdje se distribuiraju, P2.4 tenant granica i 18.3 rollout
  compatibility. Glavni RED `test_kill_mid_backfill_resumes_same_checksum_without_skip_or_double_apply`
  prekida proces nakon durable cursora N; novi owner s višim fenceom mora nastaviti od N, a stari
  owner ne smije commitati. Dodatno: checksum promijenjene već-aplicirane migracije, corrupt/truncated
  backup, krivi encryption key, cross-tenant restore, unavailable PITR segment i rollback starog
  binaryja preko contracted sheme moraju fail-closed prije cutovera.

- **Verifikacija:** SQLite backup API, PostgreSQL PITR i Alembic/yoyo dokumentacija potvrđuju pojedine
  mehanizme, ali ne daju zajednički backend-agnostični rollback. Primarni pick ostaje **UNCLEAR** dok
  S0.4 ne zapiše store i service topology. Gate je backend conformance: dvije istovremene migracije,
  kill na svakoj fazi, live old/new binary compatibility, bit-corruption i stvarni restore drill na
  fresh instanceu uz mjerene RPO/RTO.

- **Pareto-floor:** **CONDITIONAL PASS / PICK UNCLEAR.** Ugovor je kodabilan bez prejudiciranja
  baze, local SQLite salvage je vrijedan, a service adapter se ne izmišlja unaprijed. Najjači
  protuargument je da backend-neutralni migration API može postati prenizak zajednički nazivnik;
  zato samo lifecycle/receipti ostaju zajednički, dok SQL, locking, snapshot i PITR semantika ostaju
  backend-specifični adapter contracti.

# Završni gate S17+S18

1. `go test ./internal/extensions/... ./internal/update/... ./internal/dist/... ./internal/fieldbridge/...`
   mora uključiti hash-swap, expired/rollback TUF, post-sign mutation, plugin process leak i
   zero-network-before-consent mutante.
2. `go test -race ./internal/service/...` mora uključiti tenant IDOR, budget reservation race,
   stale-fence commit, queue reclaim i migration lock/fence testove.
3. Adapter conformance radi protiv stvarnog MCP procesa, lokalnog TUF repositoryja i odabranog queue/
   persistence backenda; mock-only GREEN nije promotion dokaz.
4. Windows/Linux/macOS clean-host matrica dokazuje CLI/TUI install/update/rollback bez runtime setupa.
   Kubernetes/Argo/Flagger i multi-host testovi pokreću se samo za `service` profil.
5. Controlled ablation mora dokazati reporting chain: isključivanje digest provjere, tenant filtera,
   fencing CAS-a ili migration checksum checka mora cijeli promotion gate učiniti RED.

**Pareto presuda:** S17 je prihvatljiv samo kao mali, zatvoreni extension/update/distribution rub;
S18 je uvjetni service sloj koji reutilizira jezgru. Time se ne uvode Go dynamic plugin, vlastiti
orchestrator, dva retry enginea, dva journal writera ni prerano odabrana distributed baza. Najslabija
karika ostaje 18.4 service backend: ugovor i test-kit su određeni, ali implementacija i kandidat ne
smiju dobiti PASS prije S0.4 odluke i stvarnog crash/restore dokaza.

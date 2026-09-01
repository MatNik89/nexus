PLAN S14

# S14 — Sučelja UX

## Zajednički vlasnički rez: jezgra je library, svako sučelje je tanki adapter

- **Jedini application owner:** `internal/application` je in-process Go library nad S0/S3/S6.9/S7.
  Posjeduje admission, run lifecycle, event cursor, cancel, challenge-response i artifact access.
  Nema `cli executor`, `web executor`, `sdk executor` ni `channel executor`.
- **Tanki adapteri:** CLI/TUI/desktop, HTTP/SSE, web, ACP i channel pluginovi samo autentificiraju
  transport, validiraju wire format i prevode u isti `Application` API. UI projekcije nikad ne pišu
  kernel state niti izvode tool poziv.
- **Podaci:** teme, keybinding, layout, endpoint/config manifesti, IDE capability, channel allowliste
  i adapter postavke. Podaci mogu samo suziti capability/policy/budget; ne mogu označiti principal
  trusted niti preskočiti S6.9.
- **Paket-layout:** `internal/application/{contract,service,events,challenge}`;
  `internal/interfaces/{cli,tui,http,web,desktop,acp,channel}`. Wire modeli su odvojeni od kernel
  structova; mapper eksplicitno odbija polja koja klijent ne smije postaviti.

```go
type RunID string
type EventCursor uint64

type SubmitRun struct {
    SchemaVersion uint16
    IdempotencyKey string
    SessionRef *contracts.SessionRef
    Goal contracts.ContextBlock
    Workspace workspace.Ref
    RequestedCapabilities []capability.Ref
    RequestedBudget budget.Request
    EffectMode effects.Mode // DRY_RUN | COMMIT_ALLOWED; policy može samo suziti
}

type RunView struct {
    RunID RunID
    State contracts.RunState
    ResultRef *contracts.ArtifactRef
    DeliveryState *delivery.State
    LastEvent EventCursor
}

type ChallengeAnswer struct {
    ChallengeID string
    Decision contracts.Decision
    Nonce string
    PayloadHash string
}

type Application interface {
    Submit(context.Context, auth.Principal, SubmitRun) (RunView, error)
    GetRun(context.Context, auth.Principal, RunID) (RunView, error)
    Watch(context.Context, auth.Principal, RunID, EventCursor) (<-chan contracts.Envelope, error)
    Cancel(context.Context, auth.Principal, RunID, contracts.CancelCause) error
    AnswerChallenge(context.Context, auth.Principal, ChallengeAnswer) error
    OpenArtifact(context.Context, auth.Principal, contracts.ArtifactRef) (io.ReadCloser, ArtifactMeta, error)
}
```

**Globalne invarijante S14:**

1. `auth.Principal` stvara transport authenticator/host identity provider, ne request body, argv,
   ACP `_meta` ni channel text. Svaka metoda ponovno radi tenant/resource authorization.
2. `Application.Submit` uvijek gradi S0 envelope, prolazi S0.5 closure, S6.9 admission i S7 budget/
   idempotency. Adapter ne smije pozvati S3 loop, provider ili tool registry izravno.
3. `Watch` je projekcija kanonskog P0.3 journala. Cursor je monoton; reconnect s istim cursorom
   daje iste evente. Spori consumer dobiva bounded disconnect/resume, ne usporava kernel niti gubi
   state/security event bez vidljivog gap-a.
4. External input, model/tool output i transcript uvijek su untrusted data. Terminal ANSI/OSC,
   HTML, Markdown, IDE markup i channel formatting kodiraju se na izlazu; frontend nije PEP.
5. Transport ACK, HTTP 200, WebSocket send i UI render nisu run success ni durable delivery.
   Run state mijenja kernel, a dostavu samo S7.3 outbox+receipt state-machine.
6. Jedan request/error/event vocabulary vrijedi za sve adaptere. Compatibility adapter (npr.
   OpenAI wire ili AG-UI) mapira gubitke eksplicitno i nikad ne postaje drugi source of truth.

## 14.1 CLI/TUI + desktop shell

- **Tip:** TANKI ADAPTERI NAD `Application`; BUBBLE TEA TUI JE PRIMARNI TERMINALNI PICK

- **Aspekti:** Codex TUI daje čist turn/session/tool prikaz i streaming; Gemini CLI daje široku
  cross-platform CLI ergonomiju; crush daje kvalitetan Bubble Tea layout, komponente i interakciju,
  ali FSL-1.1-MIT nije OSI licenca pa je dizajnerski referent, ne copy-source. Bubble Tea daje Go
  Elm-style `Init/Update/View` model. NEXUS daje korisne teme, `/loop`, role i plain terminal tok.
  Desktop treba isti core: Wails je primarni pick radi reusea web komponenata i Go bindinga; Fyne je
  contingency ako webview/runtime matrica padne.

- **Sinteza:** jedan `nexus` binary iz `CGO_ENABLED=0` s podnaredbama `run`, `status`, `watch`,
  `cancel`, `approve`, `sessions` i `tui`. CLI noninteractive izlaz ima stabilan `--output=text|json`
  contract; TUI je Bubble Tea projekcija istog `Watch` streama. `tea.Cmd` smije samo zvati
  `Application`, nikad provider/tool. Approval ekran dohvaća kernelov immutable preview i šalje
  samo challenge ID+exact decision; modelov tekst nije confirmation UI.

```go
type CLI struct { App application.Application; Identity auth.HostIdentityProvider }

type TUIModel struct {
    App application.Application
    Principal auth.Principal
    ActiveRun application.RunID
    Cursor application.EventCursor
    View RunProjection
    PendingChallenge *application.ChallengeView
    Width, Height int
}

type DesktopBridge struct { App application.Application; Identity auth.HostIdentityProvider }
func (b *DesktopBridge) Submit(ctx context.Context, in DesktopSubmit) (application.RunView, error)
func (b *DesktopBridge) Cancel(ctx context.Context, id string) error
func (b *DesktopBridge) Answer(ctx context.Context, in DesktopChallengeAnswer) error
```

  Untrusted terminal output se normalizira u text spans; ESC/C0/OSC52/hyperlink kontrolni nizovi
  nikad se ne ispisuju sirovo. Pasted input ostaje jedan user block i ima size cap. `Ctrl-C` prvi put
  šalje S7 cancel aktivnom runu, drugi put zatvara UI tek nakon bounded drain/terminal restorea.
  Resize i stream burst koalesciraju samo presentation evente; audit/state/security eventi ostaju.

  Desktop se gradi kao **zaseban frontend artefakt** nad istim Go modulom, ne kao uvjet CLI binaryja.
  Wails može ugraditi frontend assete u executable, ali koristi OS webview; Fyne development traži C
  compiler i grafičke drivere. Zato je poštena garancija: CLI/TUI je jedan CGO-free binary;
  `nexus-desktop` prolazi zasebnu Win/macOS/Linux packaging matricu. Ne održavamo i Wails i Fyne:
  Wails ostaje primary dok install-from-clean-OS gate ne pokaže runtime/setup problem; tada se mjeri Fyne.

- **Salvage:** iz `cli.py:1-83,109-160,2702-2710` portirati command vocabulary, config hookup i
  terminal UX, ali svaki handler svesti na application call. Iz `tui/app.py:12-19,22-115`
  Python→Go portirati teme, width bounds, commands i graceful EOF/Ctrl-C. **Ne portirati**
  `tui/app.py:35-43,85-94,111-112` direct `Nexus()`/`loop()`/`handle()` pozive; oni krše jedan API.
  Desktop nema Python salvage; reusea web view modele iz 14.3, ne kernel metode.

- **Ugovor/RED:** P0.1/P0.2/P0.3, P2.2 te S6.9. RED:
  `test_every_cli_tui_desktop_action_crosses_application_admission` instrumentira jedini admission
  seam, ablira ga i očekuje da run/cancel/approve iz sva tri adaptera failaju prije side-effecta.
  Dodatno: `test_tui_never_emits_untrusted_terminal_escape`,
  `test_ctrl_c_cancels_once_and_restores_terminal`,
  `test_json_mode_contains_no_ansi_or_progress_noise`,
  `test_tui_reconnect_resumes_exact_cursor_without_duplicate_state` i 3-OS binary smoke.

- **Verifikacija:** Bubble Tea službeni source potvrđuje Elm `Init/Update/View`, terminal input i
  cancellation površinu ([official](https://github.com/charmbracelet/bubbletea)); to ne dokazuje
  naš cleanup ni single-binary build—njih dokazuje `CGO_ENABLED=0 go build`+clean-VM smoke. Wails
  službeno navodi bundled assets/single executable i Go↔JS binding
  ([official](https://wails.io/docs/introduction/)); Fyne službeno navodi end-user bez dependency
  setupa, ali development C compiler zahtjev ([official](https://docs.fyne.io/started/quick/)).

- **Pareto floor:** **PASS za CLI/TUI dizajn; desktop je uvjetan packaging gateom.** Dobivamo
  Codex/Gemini funkcionalni floor i crush-style TUI bez kopiranja FSL koda, uz jaču zajedničku
  admission granicu. Najjači counterargument je da Wails “single executable” nije isto što i nula
  OS komponenti; zato desktop ne proširuje CLI distribucijsku garanciju bez clean-OS dokaza.

## 14.2 Headless API i embeddable SDK

- **Tip:** TANKI IN-PROCESS SDK + HTTP/SSE TRANSPORT ADAPTER; NEMA DRUGOG EXECUTORA

- **Aspekti:** OpenHands daje conversation REST resurse + real-time event socket; Dify daje
  headless workflow/API ergonomiju; goose pokazuje CLI/desktop/API nad istim proizvodom. NEXUS SDK
  daje fluent usage i dry-run default, a `openai_api.py` loopback/auth/body-cap/generic-error obrane.
  Naš graft je manja površina: kanonski async runs API + resumable SSE; WebSocket se dodaje samo ako
  bidirectional latency mjerenje pokaže potrebu. SDK je in-process typed client istog sučelja.

- **Sinteza:** public application contract ima resurse:
  `POST /v1/runs`, `GET /v1/runs/{id}`, `GET /v1/runs/{id}/events?after=`,
  `POST /v1/runs/{id}:cancel`, `POST /v1/challenges/{id}:answer` i bounded artifact download.
  Long run vraća `202 + RunView`; `Idempotency-Key` se atomski veže uz canonical request hash.
  SSE nosi journal sequence/event ID i podržava `Last-Event-ID`. OpenAI-compatible chat je zaseban
  lossy compatibility adapter koji submitira run i čeka/streama projekciju; nije kanonski API.

```go
type SDK struct {
    App application.Application
    Identity auth.HostIdentityProvider
    Defaults SDKDefaults // DryRun=true; data samo sužava
}

type RunBuilder struct { request application.SubmitRun; err error }
func (s SDK) NewRun(goal contracts.ContextBlock) RunBuilder
func (b RunBuilder) WithWorkspace(workspace.Ref) RunBuilder
func (b RunBuilder) WithCapability(capability.Ref) RunBuilder
func (b RunBuilder) WithBudget(budget.Request) RunBuilder
func (b RunBuilder) Submit(ctx context.Context) (application.RunView, error)

type HTTPAdapter struct {
    App application.Application
    Auth auth.HTTPAuthenticator
    Limits HTTPPolicy
}
```

  SDK default je `DRY_RUN`: isti S0/6.9/7 put stvara intent/preview, ali policy ne izdaje commit
  grant. Prelazak na effects-enabled zahtijeva eksplicitni host policy, ne `.Live()` boolean.
  Embedded host mora dati `HostIdentityProvider`; caller ne smije sam konstruirati admin principal.
  Extension/tool/skill injection ide kroz S11.5/S17 manifest+closure, ne mutable registry mapu.

  HTTP granica ima typed size/depth/count capove, authn+tenant authz, rate/backpressure, TLS izvan
  loopbacka, Host/Origin policy, deny-default CORS, generic typed errors i tajne izvan response/loga.
  Cookie-auth mutacije traže CSRF; bearer API ne prihvaća credential u queryju. API schema se
  generira kao projekcija ručnih Go contracta i versionira additive; nije owner contracta.

- **Salvage:** iz `sdk.py:23-82` portirati chainable builder ergonomiju, one-runtime reuse i dry-run
  namjeru, ali **ne** `sdk.py:41-45,64-74` mutable tool injection/team manifest side-effect niti
  unknown-skill nonfatal fallback. Iz `gateway/openai_api.py:56-161` portirati loopback Host guard,
  constant-time bearer compare, bounded body, bounded admission i generic errors. **Ne portirati**
  `openai_api.py:30-43` history→jedan string folding (gubi typed role/provenance), global `_run_lock`
  kao concurrency owner ni fake one-chunk streaming; S7 posjeduje concurrency, S3 pravi stream.

- **Ugovor/RED:** P0.1/P0.2/P0.3, P2.2 i svi effect ugovori kroz S6.9. RED:
  `test_sdk_and_http_cannot_bypass_application_policy` koristi isti forbidden tool request kroz SDK,
  canonical HTTP i OpenAI adapter; sva tri moraju dati isti typed denial i nula tool poziva.
  Dodatno: `test_dry_run_never_receives_commit_grant`,
  `test_same_idempotency_key_different_payload_is_rejected`,
  `test_sse_resume_is_gap_visible_and_duplicate_safe`,
  `test_client_cannot_supply_principal_tenant_or_trust`,
  `test_slow_consumer_cannot_exhaust_journal_writer` i public-bind auth/TLS gate.

- **Verifikacija:** OpenHands službeni agent-server opis stvarno ima conversation REST, event
  endpoint i WebSocket ([official source](https://github.com/OpenHands/software-agent-sdk/tree/main/openhands-agent-server));
  goose službeno oglašava CLI/desktop/API ([official](https://block.github.io/goose/)). Lokalni SDK
  doista završava na `Nexus.handle`, ali ne dokazuje formalni S6.9/S7 admission; novi conformance
  test mora dokazati isti event trace za sve adaptere.

- **Pareto floor:** **PASS uz async run API i jedan in-process interface.** Zadržava OpenHands
  realtime/headless, Dify workflow ergonomiju i NEXUS fluent embed, ali uklanja zaseban executor.
  SSE umjesto obveznog WebSocketa je namjerno jednostavniji floor; upgrade tek ako mjerena
  bidirectional potreba ne stane u HTTP command+SSE event model.

## 14.3 Web UI

- **Tip:** TANKI WEB ADAPTER/PROJEKCIJA NAD 14.2; VLASTITI MINIMALNI SHELL, NE EMBED PLATFORME

- **Aspekti:** Open WebUI daje zreo chat/multimodal UX; LibreChat multi-provider postavke;
  OpenHands agent run, tool/event i workspace UI. AG-UI je kvalificiran wire runner-up za typed
  frontend evente. NEXUS dashboard daje loopback/Host guard, bounded event tail, redaction i
  `textContent` rendering. Ne graftamo čitavu Open WebUI/LibreChat aplikaciju jer bi duplicirala
  auth/provider/storage vlasnike; preuzimamo provjerene UX obrasce nad vlastitim API-jem.

- **Sinteza:** statički frontend asseti su versioned build artefakt ugrađen u web/desktop shell.
  Browser govori isključivo 14.2 API-ju. View-state se rekonstruira iz `RunView + Watch(cursor)`;
  optimistic input je označen local-pending dok server ne vrati run/event ID. Tool args/result,
  Markdown i artifacts renderiraju se kroz allowlisted typed komponente; raw HTML, script URL,
  chain-of-thought i tajne se nikad ne šalju. AG-UI mapper je opcionalna projekcija S0 eventa,
  bez prihvaćanja `Raw/Custom` eventa kao naredbe.

```go
type WebProjection interface {
    Snapshot(context.Context, auth.Principal, application.RunID) (UIState, application.EventCursor, error)
    MapEvent(contracts.Envelope) (UIEvent, bool)
}

type UIEvent struct {
    Seq application.EventCursor
    Kind UIEventKind
    RunID application.RunID
    Payload json.RawMessage // validiran po Kind prije browsera
}
```

  Service mode koristi secure/httpOnly/SameSite cookie ili bearer, CSP nonce, HSTS/TLS, restrictive
  CORS, CSRF za cookie mutacije, websocket/SSE origin provjeru, upload/download cap i tenant authz.
  Loopback mode i dalje provjerava literal Host radi DNS rebindinga. Output se stavlja u DOM kroz
  text nodes/sanitized renderer; terminal ANSI nije HTML. Artifact preglednik služi siguran MIME,
  `nosniff`, attachment default i nikad proizvoljnu workspace putanju.

- **Salvage:** iz `gateway/dashboard.py:1-19,28-46,299-372` portirati loopback+Host obranu, bounded
  tail, redaction, GET-only monitor i `textContent` princip; iz HTML-a portirati responsive cards,
  focus-visible i run detail. **Ne portirati** dashboardov direktni read-only SQLite query kao data
  path: i čitanje mora kroz `Application.GetRun/Watch` zbog tenant/policy/projection invarianti.
  `serve-dashboard` iz `cli.py:2425-2440` postaje samo bootstrap HTTP adaptera.

- **Ugovor/RED:** P0.1/P0.3, S6.6/S6.7, P2.2; challenge odgovori koriste approval core, ne frontend
  status. RED: `test_web_model_output_cannot_execute_markup_or_ui_intent` streama `<script>`, SVG,
  OSC i AG-UI `Raw` payload; DOM nema executable node ni application mutation. Dodatno:
  `test_cross_tenant_run_and_artifact_are_404_or_denied`,
  `test_cookie_mutation_requires_csrf_and_same_origin`,
  `test_sse_reconnect_rebuilds_same_projection`,
  `test_browser_cannot_mark_challenge_approved_without_kernel_receipt` i accessibility keyboard/
  screen-reader smoke.

- **Verifikacija:** AG-UI službeno definira event-based agent↔frontend granicu i snapshot/delta/
  streaming obrasce ([official](https://github.com/ag-ui-protocol/ag-ui)); vlastita dokumentacija
  također priznaje da identity nije ugrađen u svaki adapter, pa auth ostaje naš host owner. NEXUS
  dashboard kod stvarno koristi static HTML, JSON, `textContent`, caps i Host guard. Open WebUI/
  LibreChat/OpenHands UX vrijednost ostaje matrix screenshot/task fixture, ne arhitektonska tvrdnja.

- **Pareto floor:** **PASS na minimalnom shellu, ne na feature-parity tvrdnji.** Dobiva chat/tool/
  run ergonomiju i AG-UI interop bez drugog runtimea/storagea. Najjači counterargument je da vlastiti
  frontend košta više od embedanja Open WebUI-ja; embed se odbija jer bi njegovi auth/provider/store
  owneri stvorili dual-control plane. Reconsider tek ako adapter-only integracija prođe isti policy
  trace bez tih vlasnika.

## 14.4 IDE integracija / ACP

- **Tip:** ACP v1 STDIO ADAPTER NAD `Application` + S4 TOOL BRIDGE

- **Aspekti:** Cline daje bogat VS Code agent UX; Continue editor-first context/tool integraciju;
  ACP daje vendor-neutral JSON-RPC, initialization/capability negotiation, sessions, streaming
  update, permission, filesystem, terminal i cancel ugovore. Primarni pick je ACP v1; native VS Code/
  JetBrains pluginovi su tanki launch/config shellovi samo ako editor nema ACP client.

- **Sinteza:** `nexus acp` je stdio JSON-RPC server. `initialize` mapira ACP capability na S0.3
  measured intersection; `session/new|load|prompt` mapira na S9.1+`Application.Submit/Watch`;
  `session/cancel` delegira S7. Editor filesystem/terminal/MCP capability registrira se kao S4 tool
  adapter s workspace leaseom, S6 permissionom i S7 budgetom—editorov “permission granted” ne širi
  kernel policy. ACP session ID, run ID i workspace lease eksplicitno su vezani.

```go
type ACPServer struct {
    App application.Application
    Peer auth.LocalPeerAuthenticator
    Tools tool.Registry
    Sessions SessionBindingStore
    MaxFrameBytes int64
}

type SessionBinding struct {
    ACPConnectionID, ACPSessionID string
    Principal auth.PrincipalRef
    NexusSession contracts.SessionRef
    Workspace workspace.LeaseRef
    CapabilityHash, PolicyHash string
}
```

  Stdio ima jedan JSON-RPC writer, bounded frame/depth, stdout samo protokol, logovi stderr/journal.
  Unknown method/enum faila standardnim errorom; unknown `_meta` se čuva samo kao untrusted opaque
  data i ne utječe na auth/policy. ACP traži absolute paths, ali jezgra ih dodatno realpath/handle
  veže uz workspace; apsolutno nije autorizirano. Client-provided MCP konfiguracija ponovno prolazi
  S4.6 auth i S6.8/S11.5 closure. Concurrent sessions imaju odvojene budgete/workspace leaseove.

- **Salvage:** nema gotovog ACP adaptera u navedenom NEXUS salvageu. Portirati samo CLI session/
  cancel/event mapping nakon što application API postoji; ne omatati `cli.py` subprocess niti
  parsirati njegov ljudski output. NEXUS gateway process/JSON discipline je obrazac, ali ACP se piše
  prema pinned v1 shemi i official conformance fixtureu.

- **Ugovor/RED:** P0.1/P0.2/P0.3, S4/S5/S6 i P2.2; permission request odgovori ne zaobilaze approval
  core. RED: `test_acp_permission_cannot_expand_kernel_policy` simulira editor koji odobri write
  izvan workspacea; application/tool PEP mora odbiti i ne otvoriti putanju. Dodatno:
  `test_acp_stdout_contains_only_jsonrpc`,
  `test_acp_absolute_path_still_requires_workspace_lease`,
  `test_cancel_notification_reaches_single_s7_owner`,
  `test_two_sessions_do_not_share_principal_budget_or_workspace`,
  `test_client_mcp_is_not_auto_trusted` i official ACP transcript conformance.

- **Verifikacija:** ACP v1 službeno potvrđuje JSON-RPC methods/notifications, init/auth, session
  prompt/update/cancel, bidirectional permission i stdio subprocess model
  ([official overview](https://agentclientprotocol.com/protocol/v1/overview)). Dokument izričito
  kaže da je model “trusted editor”, što **nije** dokaz naše kernel autorizacije; zato ACP permission
  ostaje samo signal korisnika. Cline/Continue UX aspekti i client compatibility mjere se stvarnim
  editor fixtureom, ne popisom SDK-ova.

- **Pareto floor:** **PASS na standardnoj granici; editor coverage uvjetan.** ACP zadržava Cline/
  Continue kontekst, sessions, permission i terminal UX bez IDE-specifičnog executora. Najveći rizik
  je privilege confusion editor↔kernel; dvostruki policy rez je namjeran, ne duplikacija ownera:
  editor potvrđuje user intent, kernel jedini odlučuje capability.

## 14.5 Messaging/channel gateway (`channels`; zahtijeva `extensions`)

- **Tip:** JEZGREN INBOX/IDENTITY/OUTBOX/HITL MEHANIZAM + CHANNEL PLUGIN ADAPTERI

- **Aspekti:** NEXUS ima zajednički gateway i Telegram/Slack/Discord/Matrix/Signal/WhatsApp
  adaptere, deny-default allowliste, signature/replay provjere, chunking, egress i redaction;
  OpenClaw daje topic/channel routing; Rasa/Matterbridge daju connector širinu. Naš graft dodaje ono
  što lokalni kod nema: durable inbox dedup, principal binding, thread model, S7.3 transactional
  outbox, precizne receipt stateove i P1.6 single-use remote HITL.

- **Sinteza:** channel plugin ne prima `Application`; prima/šalje samo typed transport records preko
  S17 extension hosta. Jezgra prvo verificira manifest/closure, zatim signature/platform identity,
  allowlist i durable dedup, mapira `(adapter,channel_identity)` u interni principal i tek tada
  submitira application request. Thread binding je eksplicitan i tenant-scoped; tekst `approve`
  bez aktivnog exact challengea je obična poruka.

```go
type ChannelInbound struct {
    AdapterID, AdapterVersion string
    ChannelType string
    ProviderEventID string
    ChannelIdentity, ConversationID, ThreadID, MessageID, ReplyTo string
    OccurredAt, ReceivedAt time.Time
    Content []contracts.ContextBlock
    Attachments []contracts.ArtifactRef
    SignatureEvidence contracts.EvidenceRef
}

type DeliveryIntent struct {
    DeliveryID string
    RunID application.RunID
    ResultDigest string
    AdapterID, ChannelType, ChannelIdentity, ConversationID, ThreadID string
    ReplyTo string
    IdempotencyKey string
    PayloadRef contracts.ArtifactRef
}

type DeliveryReceipt struct {
    DeliveryID, AdapterID, ProviderMessageID string
    State delivery.State // PENDING|LEASED|ACCEPTED|DELIVERED|RETRYABLE|DEAD|UNKNOWN
    Attempt uint32
    FencingToken uint64
    ReceivedAt time.Time
    Evidence contracts.EvidenceRef
}

type ChannelAdapter interface {
    Manifest() extensions.ChannelAdapterManifest
    Receive(context.Context, EventSink) error
    Send(context.Context, DeliveryIntent, delivery.AttemptGrant) (DeliveryReceipt, error)
    Reconcile(context.Context, DeliveryIntent) (DeliveryReceipt, error)
}
```

  P1.6 closure je doslovna: `channels requires extensions+7.3+approval-core`; adapter manifest mora
  imati `adapter_id/version/artifact_digest/channel_type/permissions/network_endpoints/provenance/
  signature/lifecycle_entry_id/supply_chain_attestation_id`. S0.5 ne može aktivirati parcijalan set.
  Plugin je izolirani signed subprocess/extension ABI; Go runtime plugin se ne koristi zbog
  portability/version problema.

  Inbox unique key je `(adapter_id, provider_event_id)` ili dokumentirani canonical fallback hash;
  ACK provideru ide tek nakon durable inbox commit-a, ne nakon runa. Result commit i outbox insert su
  jedna transakcija/journal decision: `RUN_DONE` i `RESULT_DELIVERY_PENDING` su odvojeni. S7 worker
  leasea outbox uz fencing token, šalje s intent-stabilnim idempotency keyem i klasificira odgovor:
  provider “accepted/message_id” je `ACCEPTED`, ne nužno `DELIVERED`; platform receipt ili
  documented final semantics tek daju `DELIVERED`. Timeout nakon send-a je `UNKNOWN`→reconcile,
  nikad slijepi retry. Ordered chunks su child deliveries s rednim brojem i završnim aggregate receiptom.

  Remote HITL koristi Annex `ApprovalChallenge{challenge_id,run_id,intent_id,payload_hash,
  principal_id,channel_identity,issued_at,expires_at,single_use_nonce,allowed_decisions}`. Odgovor
  mora doći iz istog authenticated principal+channel bindinga, biti exact-intent, neistekao i
  single-use. Adapter samo emitira answer; S7 durable transition jedini nastavlja run.

- **Salvage:** iz `gateway/base.py:27-59,68-177` portirati lossless bounded chunking, deny-default,
  redaction-before-egress, artifact workspace containment i stop-on-failed-chunk. Iz Slack/
  WhatsApp adaptera portirati webhook signature/replay checks; iz Matrixa whoami; iz Discorda/
  Signala reconnect parsing; iz Telegrama allowlist-before-media compute. Adaptere portirati tek
  iza parity fixturea i novog manifest ABI-ja.

  **Ne portirati kao durable:** `gateway/base.py:62-65` tretira `None`/nepoznat odgovor kao delivered;
  `core/queue.py:81-86` označi task `done` prije dostave i proguta delivery failure; in-memory Slack
  `_seen` deque i channel queue nestaju na crashu. To su konkretni N9 kontraprimjeri. Telegram
  `gateway/telegram.py:67-82,127-141` cijeli file/body bufferira bez zajedničkog MediaGate capa;
  attachment ingest mora kroz S13/S10 granice.

- **Ugovor/RED:** P1.6 + S7.3 N9, P0.1/P0.2/P0.3, P2.2/P2.3 te S6 auth/egress. Kanonski Annex RED
  `test_channels_contract_fails_closed`: bez `extensions/S6.8` attestationa daje
  `INCOMPLETE_CAPABILITY_CLOSURE`; dvostruki approval odgovor daje `APPROVAL_REPLAY`; nema adapter
  side-effecta ni drugog resumea. Dodatno:
  `test_run_done_is_not_result_delivered`,
  `test_crash_after_provider_accept_reconciles_without_duplicate_send`,
  `test_provider_ack_waits_for_durable_inbox_commit`,
  `test_same_event_is_submitted_once_after_restart`,
  `test_wrong_identity_thread_or_expired_nonce_cannot_approve`,
  `test_failed_chunk_prevents_later_chunks_and_aggregate_delivery` i
  `test_stale_delivery_fence_cannot_record_receipt`.

- **Verifikacija:** lokalni kod dokazuje šest konkretnih adaptera i korisne membrane, ali također
  dokazuje gubitak delivery statea u `queue.py`; zato je salvage djelomičan, ne gotov gateway.
  Platform API `200/ok/message_id` semantika mora se po adapteru dokumentirati i contract-testirati
  snimljenim signed fixtureima. OpenClaw/Rasa/Matterbridge kandidati prolaze licence, auth, thread,
  receipt i idempotency matricu; broj konektora nije zamjena za N9.

- **Pareto floor:** **PASS na ugovoru; adapteri uvjetni parity gateom.** Zadržava NEXUS channel
  širinu i deny-default te dodaje crash-safe inbox/outbox, precizne receipt stateove i exact remote
  HITL. Najjači counterexample je platforma bez final delivery receipta: takav adapter može završiti
  na `ACCEPTED` uz dokumentirani proof ceiling, ali ne smije lagati `DELIVERED`.

## S14 aktivacijska i conformance matrica

| Površina | Obvezni gate prije aktivacije | Dokaz jednog application ownera |
|---|---|---|
| CLI/TUI | 3-OS CGO-free build, ANSI/OSC, cancel/drain, JSON output | isti admission/event trace kao SDK |
| Desktop | clean-OS install/launch, binding auth, CSP/XSS, cancel | Wails/Fyne bridge zove samo `Application` |
| Headless/API/SDK | authz/tenant, idempotency, SSE resume, dry-run, backpressure | policy ablation ruši sve entrypointe |
| Web | CSP/CSRF/CORS/Host, tenant isolation, reconnect, accessibility | nema DB/kernel/tool direct importa |
| ACP | official v1 transcript, stdout purity, workspace/capability isolation | ACP mapper→Application/S4 PEP |
| Channels | P1.6 closure, durable inbox/outbox, receipt/reconcile, HITL replay | plugin nema Application ni S7 write pristup |

**Pareto-floor sud:** S14 je **PASS na buildable dizajnu** samo ako conformance test pošalje isti
literalni run/cancel/approval kroz svaki aktivni adapter i dobije isti kanonski journal/policy ishod
(uz wire-specifične projekcije). Tri proof-ceilinga ostaju eksplicitna: desktop “single executable”
ne dokazuje clean-OS bez sistemskog webview/graphics preduvjeta; frontend protokol ne rješava auth;
provider receipt koji znači samo acceptance ne dokazuje krajnju dostavu.

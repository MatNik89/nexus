PLAN S2

# S2 — Provider sloj

## Zajedničke odluke i paket-layout

- S2 je jezgreni MEHANIZAM. Provider definicije, auth manifesti i cjenici jesu podaci; izbor cilja, capability/policy provjera, serializacija i normalizacija odgovora ostaju kod.
- Predloženi layout je `internal/provider/{contract,registry,auth,transport,structured,local,cost,routing}`. Provider-specific codec živi u `internal/provider/adapters/<id>` i implementira isti ABI; nema provider SDK-a u kernelu i nema Python/Node sidecara.
- Ovisnosti su jednosmjerne: `contract <- {registry,auth,transport,structured,local,cost,routing} <- adapters`; svi provider DTO-i koje dijele podpaketi (`OutputContract`, `UsageObservation`, `Money`, `ProviderError`) žive u `contract`, a podpaketi sadrže ponašanje nad njima. Tako `contract` nikad ne importira vlastiti implementacijski podpaket. S2 smije tražiti odluku S6/S7, ali ih ne smije zaobići niti implementirati njihovu petlju.
- `S7.1` je jedini owner deadlinea, cancela, attempt-granta, backoffa i ponovnog pokušaja. `S2.2` samo pretvara jednu završenu transportnu opservaciju u tipizirani error i preporučeni sljedeći cilj. Nijedan provider, auth adapter, HTTP client, CLI wrapper ni parser nema automatski retry.
- `S2.6` je jedini owner poretka modelskih ciljeva. `S12.3` je potrošač. Baseline router ulazi u single binary; naučeni router je opcionalni adapter iza istog ABI-ja.
- Svaki izlaz providera je nepouzdan vanjski ulaz. Granice su: HTTP peer, OAuth authorization server/browser callback, lokalni CLI proces i lokalni model server. Tajne, prompt, model output, usage i cijena nikad se ne zapisuju mimo kanonskog `EventJournal.Append` puta.
- Mrežni provider request ne smije biti ni serializiran prije P2.5 odluke. Isti preflight ponavlja se za svaki fallback/reroute, uključujući promjenu auth moda ili endpointa.

Zajednički ABI je malen i jednak za API-key HTTP, službeni OAuth HTTP, CLI-agent subprocess i eksperimentalni subscription-OAuth adapter:

```go
package contract

type AuthMode uint8

const (
    AuthAPIKeyHTTP AuthMode = iota + 1
    AuthOfficialOAuthHTTP
    AuthCLIAgent
    AuthExperimentalSubscriptionOAuth
)

type TargetRef struct {
    ProviderID string
    ModelID    string
    EndpointID string
    AuthMode   AuthMode
}

type ProviderDescriptor struct {
    ProviderID       string
    AdapterVersion   string
    ProtocolVersion  string
    Models           []ModelDescriptor
    AuthModes        []AuthModeDescriptor
    DataProcessing   []contracts.ProviderDataDescriptor
    DescriptorDigest string
    Signature        []byte
    ExpiresAt        time.Time
}

type Request struct {
    Envelope       contracts.Envelope
    Target         TargetRef
    OperationID    string
    OutputContract *OutputContract
    Parameters     GenerationParameters
    DataPolicy     contracts.RequestDataPolicy
    BudgetID       string
}

type Response struct {
    Envelope      contracts.Envelope
    Target        TargetRef
    FinishReason  FinishReason
    Usage         UsageObservation
    ProviderReqID string
    RawDigest     string
    Warnings      []ProviderWarning
}

// Invoke smije napraviti najviše jedan fizički pokušaj i mora potrošiti grant
// prije prvog side effecta. Stream je isti contract: jedan grant, jedan request.
type Provider interface {
    // Descriptor je immutable registry snapshot; ne smije raditi network discovery.
    Descriptor() ProviderDescriptor
    Invoke(context.Context, contracts.AttemptGrant, Request) (Response, *ProviderError)
}

type Registry interface {
    Register(Provider) error
    Describe(context.Context, TargetRef) (ProviderDescriptor, error)
    Resolve(TargetRef) (Provider, error)
}
```

Normativni call-path je uvijek:

```text
S0.3 capability negotiation
  -> P2.5 provider-data authorization (prije serialize/DNS/connect)
  -> S2.6 ordered route decision
  -> S7 budget/deadline/cancel + unique AttemptGrant
  -> Provider.Invoke: exactly one physical HTTP/CLI/local-server attempt
  -> S2.2 normalized observation/advice
  -> S7 terminal outcome ili novi grant za ponovno autorizirani target
```

## 2.1 Multi-provider apstrakcija

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Široka normalizacija provider protokola i grešaka | LiteLLM | Provider-specific codecovi iza jednog stabilnog request/response ABI-ja; ne ugrađivati Python runtime ni njegov retry router. |
| Eksplicitne model capabilities i upozorenja za nepodržane opcije | PydanticAI + Vercel AI SDK | Capability descriptor odlučuje prije poziva; nema tihog dropanja tool/stream/schema opcije. |
| Jedinstveni generate/stream/tool output ugovor | Vercel AI SDK | Isti envelope i finish/usage/warning model za sve transportne oblike. |
| Completion-only CLI provider | NEXUS salvage | Instalirani `claude`/`codex` radi kao izolirani text-in/out subprocess s ugašenim alatima; NEXUS zadržava petlju i tool authority. |
| Višestruki, eksplicitni auth modovi | Vlastiti Go kod + službeni provider auth ugovori | API key je default; OAuth je dopušten samo kada provider službeno autorizira API flow; CLI auth ostaje unutar CLI-ja; nesankcionirani replica-flow je zaseban opasan opt-in. |
| Data-processing i rezidencijski fail-closed gate | Annex P2.5 | Descriptor je potpisan/istekao i autorizacija se radi za svaki konkretni endpoint prije mrežnog sinka. |

- **Sinteza — provider/capability ugovor:** registry ne zaključuje mogućnosti iz imena modela niti verzije klijenta. Adapter objavljuje potpisani, verzionirani descriptor; S0.3 pregovara presjek zahtjeva i descriptor-a.

```go
type Capability uint16 // CHAT|STREAM|TOOLS|PARALLEL_TOOLS|JSON_SCHEMA|VISION|...

type ModelDescriptor struct {
    ModelID            string
    Revision           string
    Capabilities       []Capability
    ContextTokens      uint64
    MaxOutputTokens    uint64
    InputDataClasses   []contracts.DataClass
    SupportedSchemas   structured.SchemaSubset
    UsageFields        []cost.UsageField
    PriceSnapshotID    string
}

type AuthModeDescriptor struct {
    Mode              AuthMode
    CredentialKind    string
    SecretRef         *contracts.SecretRef
    OAuth             *auth.OAuthDescriptor
    CLI               *transport.CLIManifest
    SanctionedForAPI  bool
    TermsRef          string
    TermsVersion      string
}

type GenerationParameters struct {
    MaxOutputTokens uint64
    TemperaturePPM  *uint32
    TopPPM          *uint32
    Stop             []string
    Seed             *int64
}
```

  Pravila:

  1. Adapter mora strict-decodeati provider odgovor uz byte/depth/item limite. Unknown polje smije ostati samo kao bounded `json.RawMessage` u sealed evidenceu; ne ulazi u kernel API kao `map[string]any`.
  2. Unsupported capability ili parameter vraća `CAPABILITY_UNSUPPORTED` prije poziva. Adapter ne smije tiho izbaciti `tools`, schema, stop, seed, stream ni sigurnosnu opciju.
  3. Provider/model/endpoint/auth identitet dio je `TargetRef` i svaki od tih elemenata ulazi u route, policy, attempt i audit zapis. Promjena auth moda nije transparentni fallback.
  4. Provider descriptor je konfiguracijski artefakt, ne tvrdnja samog providera u runtimeu. Unknown/expired/unsigned data-processing polje faila P2.5.
  5. `Provider.Invoke` radi jednu fizičku operaciju. HTTP redirect na drugi origin ili CLI koji sam pokreće mrežni retry tretira se kao kršenje adapter ugovora, osim provider-dokumentiranog transportnog redirecta koji ostaje isti autorizirani origin i ne stvara novi inference attempt.

- **Sinteza — auth modovi:** `AuthStrategy` samo priprema credential material za već autorizirani `TargetRef`; ne zove model i ne bira drugi mod.

```go
type AuthContext struct {
    PrincipalID string
    Target      contract.TargetRef
    SecretScope contracts.SecretScope
    Now         time.Time
}

type PreparedAuth struct {
    HeaderRefs  []contracts.SecretHeaderRef // bytes injektira S6 secret broker u zadnjem trenutku
    CLILeaseRef string
    TokenExpiry *time.Time
    AuthDigest  string
}

type AuthStrategy interface {
    Mode() contract.AuthMode
    // Prepare je local-only: vraća valjani cached credential ili AuthActionRequired.
    // Nikad ne radi browser/token HTTP poziv i ne troši inference AttemptGrant.
    Prepare(context.Context, AuthContext) (PreparedAuth, *AuthActionRequired, *contracts.TypedError)
}

type AuthActionRequired struct {
    OperationID string
    ProviderID string
    Kind string // BEGIN_AUTH_CODE|POLL_DEVICE_TOKEN|REFRESH_TOKEN|WORKLOAD_TOKEN
    EndpointID string
    NotBefore *time.Time
    StateDigest string
}

type AuthExecutor interface {
    // S7 izdaje zaseban grant za svaki fizički authorization/token pokušaj.
    Execute(context.Context, contracts.AttemptGrant, AuthActionRequired) *contracts.TypedError
}

type OAuthDescriptor struct {
    Issuer, AuthorizationEndpoint, TokenEndpoint string
    DeviceEndpoint *string
    ClientIDRef contracts.SecretRef
    RedirectURIs, Scopes, Audiences []string
    Flow OAuthFlow // AUTH_CODE_PKCE|DEVICE_CODE|CLIENT_CREDENTIALS|MANAGED_IDENTITY
    ProviderDocsRef, PolicyVersion string
    SanctionedForAPI bool
}
```

  **(a) API-key HTTP — default:**

  - Config sadrži samo `SecretRef`; S6.7 secret broker dohvaća bytes tek nakon P2.5 i S7 granta, injektira ih u allowlisted header (`Authorization: Bearer`, `x-api-key`, `api-key`...) i briše privremeni buffer poslije requesta koliko Go runtime dopušta.
  - Endpoint scheme mora biti HTTPS osim eksplicitnog loopback/local moda. Redirect je defaultno ugašen; proxy i custom CA su operator-owned config. Headeri, URL query, error i journal nikad ne nose key.
  - Više ključeva nisu skriveni `_KeyPool` retry. Svaki credential/endpoint je zaseban `TargetRef`; S2.6 ga može rangirati, a samo S7 smije izdati drugi grant.

  **(b) Službeni OAuth HTTP — samo kada provider službeno nudi OAuth za taj API:**

  - Dopušteni flowovi su Authorization Code + PKCE za browser-capable desktop/native app, Device Authorization samo kada provider objavi device endpoint i odobri device flow za API, te workload/managed-identity flow gdje ga provider dokumentira. `issuer`, endpointi, audience i scopes dolaze iz potpisanog operator/provider manifesta; nema discoveryja s user-controlled URL-a.
  - State, PKCE verifier/challenge, exact loopback redirect, issuer/audience/scope i TLS moraju se verificirati. Refresh token sprema S6.7, vezan uz provider+principal+client+scope; concurrent refresh se single-flighta. `invalid_grant` je terminal auth observation, ne beskonačni relogin.
  - `Prepare` smije koristiti samo još-valjani cached token. Login/refresh/poll vraća `AuthActionRequired`; S7 ga vodi kao zasebnu operaciju i izdaje zaseban grant za svaki fizički authorization/token pokušaj. Tek nakon uspješnog auth stanja izdaje se inference grant. Device polling interval/`slow_down` semantika slijedi provider/RFC manifest, ali auth adapter nema internu polling/retry petlju.
  - Primjeri dopuštenih klasa su Microsoft Foundry/Entra tokeni i Google Cloud OAuth/ADC kada službeni API docs to potvrde. OpenAI ili Anthropic API ne dobivaju OAuth mod samo zato što njihov potrošački UI/CLI ima login; njihov javni API descriptor ostaje API-key dok službena dokumentacija ne kaže drukčije.

  **(c) CLI-agent subprocess:**

```go
type CLIManifest struct {
    ExecutableID, ExecutableDigest string
    Args []string
    ToolDisableArgs, EphemeralArgs, IgnoreConfigArgs []string
    AllowedEnv []string
    MaxStdoutBytes, MaxStderrBytes int64
    Protocol CLIOutputProtocol
}
```

  - `procx.Run` dobiva fiksni argv manifest, kanonski executable digest, minimalni env, zatvoren/pipe stdin, bounded output i S7 process lease. Nema shella, inherited tool configa, MCP servera ni implicitnog workspace writea.
  - `claude` zahtijeva ekvivalent `-p --output-format json --tools ""`; `codex` zahtijeva ephemeral/ignore-user-config/ignore-rules, read-only sandbox i ugašen shell/tool surface. Start-time probe mora dokazati da instalirana verzija podržava sve obvezne zastavice; inače `CLI_SAFETY_FLAGS_UNSUPPORTED` prije prompta.
  - CLI posjeduje vlastitu login sesiju. NEXUS nikad ne čita, parsira, kopira ni osvježava `~/.claude`, `~/.codex` ili browser cookie store. Stdout je nepouzdan provider output i prolazi iste limite, schema parser i prompt-injection klasifikaciju kao HTTP odgovor.
  - Ako CLI interno radi mrežni retry koji se ne može ugasiti ili dokazivo ograničiti na jedan inference attempt, taj adapter ne zadovoljava P0.2 i ne ulazi u verified provider set. “Jedan subprocess” nije automatski dokaz “jednog billable attempta”.

  **Eksperimentalni `subscription-bez-CLI kroz repliciran OAuth` — zaseban flag, nikad default:**

```go
type ExperimentalOAuthAcknowledgement struct {
    ProviderID, PrincipalID string
    TermsRef, TermsVersion string
    WarningVersion string
    AcceptedRisks []string // TERMS_BREACH|ACCOUNT_SUSPENSION|ACCOUNT_BAN|TOKEN_REVOCATION
    AcceptedAt, ExpiresAt time.Time
    Signature []byte
}
```

  - Capability flag je `experimental-subscription-oauth-replica=false` u buildu i konfiguraciji. Ne smije ga uključiti project config, skill, rola, pack, fallback ili remote manifest; treba operator-level flag i svježi potpisani acknowledgement.
  - UI/CLI mora prije aktivacije doslovno prikazati: **“Ovaj neslužbeni flow može prekršiti uvjete korištenja providera i dovesti do ukidanja tokena, suspenzije ili trajnog bana računa. Provider ga možda promijeni bez najave. Koristite isključivo na vlastiti rizik.”**
  - Nema credential/cookie scraping-a, posuđivanja first-party client ID-a, lažnog user-agenta ni zaobilaženja CAPTCHA/MFA/device bindinga. Adapter je dostupan samo ako operator registrira vlastiti dopušteni public client i manifest ima endpoint/scopes; inače faila `EXPERIMENTAL_AUTH_UNAVAILABLE`.
  - Ovaj mod nikad nije automatski fallback, nikad ne dijeli refresh token sa službenim OAuth modom, i svaki poziv nosi `auth_mode=EXPERIMENTAL_SUBSCRIPTION_OAUTH` u audit događaju. Terms/promjena client policyja invalidira acknowledgement i capability closure.

- **Salvage:** Python→Go port ideja registry/status i API/CLI dual-patha iz `/home/matej/NEXUSv2/llm/providers.py:127-177,313-492,651-740`. Posebno zadržati Codex completion-only argv disciplinu (`providers.py:313-362`) i Claude `--tools ""` (`providers.py:365-398`). Ne portirati `_with_retry` (`providers.py:51-55`), `_KeyPool.rotate()` (`providers.py:28-45`) ni interne fallback petlje; krše P0.2. Ne portirati čitanje CLI credential datoteka i `_anthropic_key()` (`providers.py:180-302,690-704`): CLI auth ostaje u CLI-ju, a API auth ide samo kroz S6 secret broker i službeni provider ugovor.

- **Ugovor/RED:** Annex **P0.1** je envelope/schema gate, **P0.2** ograničava `Invoke` na jedan grant/jedan attempt, **P2.5** normira `ProviderDataDescriptor` i `RequestDataPolicy`. Obvezni RED: `test_fallback_cannot_bypass_residency_policy`. Lokalni RED-ovi: `test_unsupported_capability_fails_before_transport`, `test_provider_response_limits_before_decode`, `test_api_key_never_enters_log_url_or_error`, `test_oauth_mode_rejected_without_sanctioned_api_descriptor`, `test_oauth_callback_rejects_state_issuer_audience_or_pkce_mismatch`, `test_cli_agent_refuses_missing_tool_disable_flag`, `test_cli_auth_store_is_never_read`, `test_experimental_oauth_requires_operator_ack_and_never_autofallbacks`. Za svaki auth mod network/process fake broji fizičke sink pozive; policy-denied slučaj mora ostati na nuli.

- **Verifikacija:** provjereno 2026-09-01 na primarnim izvorima. [LiteLLM](https://github.com/BerriAI/litellm) stvarno izlaže objedinjeni 100+ provider format, OpenAI-compatible greške, cost i router/fallback površinu. [PydanticAI model API](https://ai.pydantic.dev/models/overview/) i [Vercel AI SDK provider model](https://ai-sdk.dev/docs/foundations/providers-and-models) potvrđuju provider apstrakciju i capability razlike; Vercelov API dodatno potvrđuje provider registry, stream i structured output. [OpenAI API reference](https://platform.openai.com/docs/api-reference) dokumentira API-key auth; [Microsoft Foundry auth](https://learn.microsoft.com/en-us/azure/foundry/foundry-models/how-to/configure-entra-id) dokumentira Entra bearer token za inference. OAuth flow floor dolazi iz [RFC 8252](https://www.rfc-editor.org/rfc/rfc8252), [RFC 7636](https://www.rfc-editor.org/rfc/rfc7636) i [RFC 8628](https://www.rfc-editor.org/rfc/rfc8628); device flow nije univerzalna zamjena za auth-code native app. Nije verificirano da OpenAI/Anthropic potrošačka subscription prijava dopušta izravni API pristup, zato je replica-mod nesankcioniran, fail-closed i izvan default closurea.

- **Pareto floor:** **PASS za 2.1.** Očuvani su LiteLLM širina/error taxonomy, PydanticAI/Vercel capability-transparentnost i NEXUS API+CLI dual-path, ali bez vanjskog runtimea i bez skrivenih retry ownera. Najjači kontraargument je da jedan `Provider` ABI skriva različitu streaming/CLI semantiku; descriptor i warnings eksplicitno iznose razlike, dok jedan lifecycle smanjuje broj sigurnosnih granica. Najslabija točka je dokaz da CLI radi točno jedan upstream attempt; adapter ostaje `UNVERIFIED` dok controlled proxy/fixture ili službeni CLI ugovor to ne dokaže.

## 2.2 Fallback / retry / rate-limit / load balancing

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Provider-error taxonomy i deployment cooldown signal | LiteLLM | Pretvoriti odgovor jednog pokušaja u stabilni `ProviderError` i health/rate-limit opservaciju. |
| Gateway fallback/load-balance policy model | Portkey + Kong AI Gateway | Deklarativna pravila i target ordering, ali bez ugradnje gateway retry petlje. |
| Jedan retry/deadline/cancel owner | Annex P0.2 | S7 jedini izdaje novi grant; S2 nikad ne spava, broji attempt ili ponovno poziva transport. |
| Fail-closed reroute policy | Annex P2.5 | Svaki predloženi fallback vraća se kroz capability/data/cost gate prije S7 granta. |
| Stagnation/circuit signal | NEXUS `core/circuit.py` | Stabilizirani failure signature je bounded opservacija za S7; nije drugi circuit/retry owner u S2. |

- **Sinteza:** S2.2 ima dva čista koraka: `Normalize(one observation)` i `Advise(eligible snapshot, observation)`. Oboje su deterministični i bez I/O-a.

```go
type ErrorClass uint8 // AUTH|RATE_LIMIT|TIMEOUT|OVERLOAD|POLICY|INVALID_REQUEST|...
type RetryDisposition uint8 // NEVER|S7_POLICY_MAY_RETRY|RECONCILE_FIRST
type EffectCertainty uint8 // NOT_SENT|SENT_NO_COMMIT|COMMITTED|UNKNOWN

type RateLimitObservation struct {
    Scope      string // credential|model|provider|tenant
    Limit      *uint64
    Remaining  *uint64
    ResetAt    *time.Time
    RetryAfter *time.Duration
    Source     string // status/header/body
}

type ProviderError struct {
    Code, SafeMessage       string
    Class                   ErrorClass
    Target                  contract.TargetRef
    HTTPStatus              *uint16
    RetryDisposition        RetryDisposition
    EffectCertainty         EffectCertainty
    Billable                cost.BillableState
    RateLimit               *RateLimitObservation
    ProviderRequestID       string
    CauseEventID            string
    RawDigest               string
}

type FallbackAdvice struct {
    OperationID string
    Failed      contract.TargetRef
    Ordered     []contract.TargetRef
    ReasonCodes []string
    NotBefore   *time.Time
    PolicyHash  string
}

type ErrorNormalizer interface {
    Normalize(TransportObservation) ProviderError
}

type FallbackAdvisor interface {
    Advise(RouteDecision, ProviderError, HealthSnapshot) FallbackAdvice
}
```

  Invarijante:

  1. `Normalize` dobiva samo završenu opservaciju jednog granta. String matching smije biti zadnji bounded fallback; provider status/header/code i transport phase imaju prioritet. Unknown post-send failure daje `EffectCertainty=UNKNOWN`, nikad optimistic retryable.
  2. `FallbackAdvisor` može samo filtrirati/reorderirati targete iz već potpisanog `RouteDecision`; ne izmišlja provider/model/key i ne poziva registry/network.
  3. `NotBefore` je hint iz `Retry-After`/cooldown snapshot-a. S2 ne radi `sleep`, timer ni waiting state; S7 odlučuje stane li hint u deadline i izdaje li novi grant.
  4. Rate-limit stanje je scope-aware. Credential cooldown ne smije automatski hladiti cijeli provider; unknown scope koristi najširi sigurni scope dok se ne dokaže uži.
  5. Load balance je route ordering iz 2.6, ne paralelni fan-out. Hedging je zasebna S7 policy s dva jedinstvena granta i budgetom; nije implicitno ponašanje S2.
  6. Svaki kandidat iz advicea ponovno prolazi S0.3 capability, P2.5 data i S7.2 budget preflight. Deny je terminal za taj target, a ne signal za slanje.

- **Salvage:** portirati `_TRANSIENT` samo kao test corpus za typed normalizer, ne kao proizvodnu string-listu (`/home/matej/NEXUSv2/llm/providers.py:25`). Portirati cooldown/actual-winner observability iz `FallbackModel` (`providers.py:743-807`) i stabilizirani failure-signature pristup iz `/home/matej/NEXUSv2/core/circuit.py:23-75` u S7 health input. Ne portirati `_with_retry`, key rotaciju ni `FallbackModel.complete()` petlju. `core/circuit.py:46-80` ostaje S7 stagnation policy; S2 samo emitira strukturirani signal.

- **Ugovor/RED:** Annex **P0.2**, obvezni `test_adapter_cannot_self_retry`: drugi transport poziv s istim `AttemptGrant` vraća `ATTEMPT_NOT_AUTHORIZED`, counter ostaje 1. Annex **P2.5** dodaje `test_fallback_cannot_bypass_residency_policy`. Lokalni RED-ovi: `test_normalizer_never_calls_transport`, `test_retry_after_is_hint_not_sleep`, `test_unknown_effect_requires_reconciliation`, `test_advisor_cannot_invent_target`, `test_credential_cooldown_does_not_poison_provider_scope`, `test_all_fallback_targets_reenter_policy_and_budget_gates`. Kontrolirani mutant koji u adapter doda standardni HTTP retry mora učiniti P0.2 oracle RED.

- **Verifikacija:** [LiteLLM router](https://github.com/BerriAI/litellm) stvarno implementira retries/fallbacks/cooldown/load balancing i error mapping; upravo zato graftamo njegove observability/taxonomy aspekte, ne izvršnog ownera. [Kong AI Gateway](https://docs.konghq.com/gateway/latest/ai-gateway/) i [Portkey gateway](https://portkey.ai/docs/product/ai-gateway) potvrđuju deklarativne gateway reliability/routing uzorke. Aktualni LiteLLM bug reports o različitim retry/cooldown putovima dodatna su kontraevidencija za dupliranje tog izvršnog stroja unutar S2; jedan S7 owner je stroži dizajn od bilo kojeg kandidata pojedinačno.

- **Pareto floor:** **PASS za 2.2.** Zadržani su provider taxonomy, cooldown/rate-limit signali i policy ordering iz tri kandidata, uz jaču P0.2 granicu. “Fallback bez vlastite petlje” zvuči manje sposobno samo ako se S2 promatra izolirano; end-to-end sposobnost ostaje u S7 i postaje dokaziva jednim attempt ledgerom. Najslabija točka su provider-specific error mape koje driftaju; upgrade gate je contract fixture za svaki adapter i unknown-safe default, ne generički retry middleware.

## 2.3 Tool-call parsing i structured output

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Schema-validacija i repair feedback | Instructor | Tipizirani validation report, ali ponovni upit delegira S7 i traži novi grant. |
| Token-level constrained decoding za lokalne modele | Outlines | JSON Schema/grammar se kompajlira u local-runtime constraint kada adapter to stvarno podržava. |
| Schema-first tolerant extraction iz slabog outputa | BAML | Bounded parser za jedan JSON objekt uz eksplicitni transformation receipt; bez izmišljanja odsutnih vrijednosti. |
| Provider-native structured output + warnings | Vercel AI SDK | Preferirati nativni schema/tool mode po descriptoru i prijaviti schema subset mismatch. |
| Kanonski tipovi | S0.1 | Konačni rezultat uvijek strict-validira ručno pisani Go ugovor; vanjska schema nije owner. |

- **Sinteza:** jedan `OutputContract` bira strategiju prema pregovorenoj capability, ali konačna validacija je ista.

```go
type OutputMode uint8 // TEXT|JSON_VALUE|TYPED_OBJECT|TOOL_CALLS
type Enforcement uint8 // PROVIDER_NATIVE|LOCAL_CONSTRAINED|PARSE_ONLY

type OutputContract struct {
    ContractID, SchemaVersion, SchemaDigest string
    Mode OutputMode
    MaxBytes, MaxDepth, MaxItems uint64
    AllowUnknownFields bool
    AllowMultipleToolCalls bool
    GoTypeID string
    JSONSchema []byte // izvedena projekcija, nije schema owner
}

type ParseReceipt struct {
    Strategy Enforcement
    InputDigest, ExtractedDigest string
    PrefixBytes, SuffixBytes uint64
    Transformations []Transformation
    CompleteJSONValue bool
    SchemaValidated bool
}

type StructuredDecoder interface {
    Prepare(ModelDescriptor, OutputContract) (ProviderOutputOptions, error)
    Decode(OutputContract, BoundedProviderOutput) (contracts.Envelope, ParseReceipt, *ProviderError)
}
```

  Pipeline:

  1. Ako descriptor dokazano podržava odgovarajući JSON-schema/tool subset, adapter šalje provider-native contract i bilježi točnu projection digest. Ako ne podržava, ne tvrdi strict mode.
  2. Za lokalni runtime s logits kontrolom može se koristiti Outlines-style schema→automaton constraint; to je adapter capability, ne obvezna cgo/Rust ovisnost jezgre. Prvi build može delegirati constraint lokalnom serveru kroz podržani API.
  3. Bounded parser prihvaća točno jednu potpunu JSON vrijednost, uz dopušten whitespace/markdown fence po policyju. Odbija truncation, duplicate keys, NaN/Infinity, depth/size overflow, trailing drugi objekt i nevaljani Unicode.
  4. Tolerantni korak smije ukloniti dokumentirani fence/prefix/suffix i normalizirati dokazivo sintaktički šum; ne smije dodati obvezno polje, pogađati enum, mijenjati broj/string, spojiti dva tool calla ili dovršiti prekinuti JSON. Svaka transformacija ulazi u `ParseReceipt`.
  5. Konačni decode ide u zatvoreni S0 Go tip i poziva njegov validator. `JSON_VALUE` ostaje bounded opaque raw value; `TYPED_OBJECT` nikad ne vraća `map[string]any`.
  6. Validation failure vraća `STRUCTURED_OUTPUT_INVALID` s bounded path/code repair hintom. S2 ne radi re-ask; S7 može izdati novi grant ako policy/budget/deadline dopušta.

- **Salvage:** Python→Go port structured/tool-call konfiguracije i output-bound ideja iz `/home/matej/NEXUSv2/llm/providers.py:503-649`; zadržati tool schema i truncation detekciju iz API adaptera (`providers.py:417-492`). Ne portirati provider-mutabilni schema state ni automatski `_with_retry`; output contract je immutable request field, a repair je novi S7 attempt.

- **Ugovor/RED:** Annex **P0.1** je finalni typed/provenance gate; **P0.2** zabranjuje parseru/Instructor-style wrapperu samostalni re-ask. RED-ovi: `test_truncated_json_is_not_repaired`, `test_duplicate_key_and_second_json_value_rejected`, `test_tolerant_parser_cannot_invent_required_field`, `test_schema_subset_mismatch_fails_before_call`, `test_invalid_structured_output_reask_requires_new_grant`, `test_parallel_tool_call_rejected_when_contract_forbids_it`, `test_parse_receipt_covers_every_byte_transform`. Mutant koji dopiše nedostajući `}` ili default obvezno polje mora pasti.

- **Verifikacija:** [Instructor](https://github.com/567-labs/instructor) potvrđuje schema validation/retry obrazac; graftamo validation feedback, ne njegovu retry petlju. [Outlines](https://github.com/dottxt-ai/outlines) i [outlines-core](https://github.com/dottxt-ai/outlines-core) potvrđuju schema/regex→FSM i token maskiranje za constrained generation. [BAML](https://github.com/BoundaryML/baml) potvrđuje schema-first type-safe output i SAP za šumni JSON; tolerantnost nije dokaz semantičke istovjetnosti, zato je naš dopušteni transform set uži i receipt obvezan. [Vercel Output API](https://ai-sdk.dev/docs/reference/ai-sdk-core/output) eksplicitno razlikuje valjani proizvoljni JSON od schema-validiranog objekta, što opravdava odvojene modeove.

- **Pareto floor:** **PASS za 2.3.** Očuvani su Instructor repair feedback, Outlines constrained generation i BAML noise tolerance, ali nijedan ne smije preuzeti S7 attempt ownership ili S0 schema ownership. Glavni tradeoff je više terminalnih parse failurea umjesto “korisnog” nagađanja; za tool/agent granicu lažno validan objekt je skuplji od eksplicitnog novog attempta. Najslabija točka je schema-subset matrica providera; svaki adapter mora imati golden capability fixtures po protocol versionu.

## 2.4 Lokalni modeli (offline put)

- **Tip:** ADAPTER iza jezgrenog provider ABI-ja; hardware-fit preflight je MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Jednostavni local model lifecycle i API | Ollama | Loopback adapter, model inventory i explicit pull/load akcije. |
| Minimalni GGUF runtime i široka CPU/GPU podrška | llama.cpp | OpenAI-compatible server adapter i precizni model/context parametri. |
| Produkcijski GPU throughput | vLLM | Remote/local serving adapter za GPU host; ista capability/data politika. |
| Mjereni host fit prije loada | NEXUS `hwdetect.py` + vlastiti kod | RAM/VRAM/backend/SIMD mjerenje + model metadata; conservative footprint, refusal prije OOM-a. |
| Single-binary granica | Go dizajn | Harness ne embedda model runtime; upravlja lokalnim endpointom/procesom kroz S1/S7. Nema cgo u jezgri. |

- **Sinteza:** lokalni server je provider target, ne poseban put oko policyja. “Local” znači verificirani loopback/Unix socket i lokalni proces; udaljeni vLLM endpoint je remote provider i mora imati puni P2.5 descriptor.

```go
type HostProfile struct {
    SnapshotID string
    OS, Arch string
    LogicalCPU uint32
    CPUFeatures []string // AVX2|AVX512|NEON|...
    RAMTotal, RAMAvailable uint64
    GPUs []GPUProfile
    MeasuredAt time.Time
}

type GPUProfile struct {
    DeviceID, Backend string // CUDA|ROCM|METAL
    VRAMTotal, VRAMAvailable uint64
    DriverVersion string
}

type ModelFootprint struct {
    ArtifactDigest, Format, Quantization string
    ParameterBytes, RuntimeOverheadBytes uint64
    KVBytesPerToken uint64
    RequestedContextTokens uint64
    GPUOffloadBytes uint64
    RequiredCPUFeatures []string
}

type FitDecision struct {
    Allowed bool
    HostSnapshotID, ArtifactDigest string
    RequiredRAM, RequiredVRAM, ReserveRAM, ReserveVRAM uint64
    ReasonCode string
    SuggestedInstalledTargets []contract.TargetRef
}

type FitEvaluator interface {
    Evaluate(HostProfile, ModelFootprint, contracts.ResourceBudget) FitDecision
}
```

  Invarijante:

  1. Host snapshot mjeri trenutno dostupni RAM/VRAM i stvarni backend; model footprint dolazi iz verificiranog artefakt metadata/digesta. Ni jedno se ne zaključuje iz marketing imena modela.
  2. Potreba je najmanje weights/resident tensors + KV cache za traženi context + runtime overhead + eksplicitna sigurnosna rezerva. Checked arithmetic faila overflow. Multi-GPU fit navodi placement, ne zbraja slijepo neupotrebljiv VRAM.
  3. Nedostajući hardware signal daje `FIT_UNKNOWN`, ne optimistic allow. User smije eksplicitno spustiti context/odabrati manju instaliranu kvantizaciju, ali ne zaobići hard OS resource budget.
  4. `Allowed=false` nastaje prije model load/spawn. Sugestije sadrže samo već instalirane i digest-verificirane artefakte; download/pull je zasebna eksplicitna mrežna operacija sa S6.8 supply-chain gateom.
  5. Local endpoint mora biti loopback/Unix socket, imati verificirani process lease i model digest. Ne smije vjerovati bilo kojem procesu koji se slučajno javi na portu.
  6. Ollama/llama.cpp/vLLM feature razlike ostaju u descriptoru. OpenAI compatibility nije dokaz potpune semantičke kompatibilnosti; tool/schema/stream capability se testira zasebno.

- **Salvage:** Python→Go port OpenAI-compatible `LocalProvider`/model probe ideje iz `/home/matej/NEXUSv2/llm/providers.py:667-687`. Portirati OS/GPU/RAM probe strategiju iz `/home/matej/NEXUSv2/core/hwdetect.py:20-127`, uključujući neimportiranje teškog ML runtimea i razlikovanje CUDA/ROCm/MPS. Ne portirati grubi tier kao fit odluku (`hwdetect.py:109-115`), cache bez generation/TTL-a ni “missing probe = zero = continue”; novi fit koristi measured availability, artefakt metadata i fail-closed unknown.

- **Ugovor/RED:** Annex **P2.2** daje hard resource budget i S7.2 enforcement; **P2.5** vrijedi za udaljeni/local data descriptor, uz loopback-local descriptor koji eksplicitno dokazuje nultu vanjsku obradu. RED-ovi: `test_fit_refuses_before_model_load`, `test_fit_uses_available_not_total_memory`, `test_fit_unknown_hardware_fails_closed`, `test_model_name_cannot_override_artifact_metadata`, `test_smaller_quant_suggestion_is_installed_and_verified`, `test_remote_vllm_cannot_claim_local_data_policy`, `test_loopback_port_requires_owned_process_identity`, `test_capability_probe_rejects_partial_openai_compatibility`. Integration gateovi se vrte na Linux/macOS/Windows; GPU-specifični testovi koriste snimljene signed probe fixtures plus barem jedan stvarni canary po backendu prije označavanja tog backenda verified.

- **Verifikacija:** [Ollama](https://github.com/ollama/ollama/blob/main/docs/api/openai-compatibility.mdx), [llama.cpp server](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) i [vLLM OpenAI-compatible server](https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html) stvarno izlažu OpenAI-like HTTP površine, ali svaka dokumentira vlastiti subset/parametre; zato jedan codec bez capability probea nije dovoljan. NEXUS `hwdetect.py` stvarno mjeri RAM te NVIDIA/ROCm/Apple backend, ali njegov tier nije model-footprint dokaz.

- **Pareto floor:** **PASS za 2.4 uz eksplicitni proof ceiling.** Očuvani su Ollama UX, llama.cpp portability i vLLM throughput put bez ugradnje tri runtimea u Go binary. Dodani measured fit i owned-process gate sprečavaju dvije klase lažno “lokalnog” uspjeha. Najjači kontraargument je da conservative fit odbija modele koje OS swap/unified-memory možda ipak pokrene; to je namjeran sigurnosni floor. Kalibracija se može kasnije poboljšati izmjerenim backend-specifičnim overhead modelom, ali ne smije preskočiti pre-load refusal.

## 2.5 Token accounting i cost tracking

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Model/provider cjenik i spend breakdown | LiteLLM | Verzijski price snapshot, tier/cache/reasoning token kategorije i reconciliation. |
| Sesijski prikaz troška | Aider | Brzi user-facing current-turn/session projection iz kanonskog cost ledgera. |
| Proxy-neutral usage observability | Helicone | Provider request/usage attribution iza adaptera, bez obveznog vanjskog proxyja. |
| Concurrent pre-call reservation | LiteLLM + Annex P2.2 | S7.2 rezervira worst-case budget prije poziva i atomically settlea nakon rezultata. |
| Točna money semantika | Vlastiti Go kod | Nema float USD; integer nano/minor units, valuta i rational rates s checked arithmetic. |

- **Sinteza:** S2.5 posjeduje price/usage normalizaciju i quote/settlement izračun; S7.2 posjeduje rezervaciju, hard budget, prekid i admission. Cost projekcija nije zasebni durable writer.

```go
type Money struct {
    Currency string // ISO 4217
    NanoUnits int64 // 10^-9 currency unit; checked arithmetic
}

type TokenCategory uint8 // INPUT|OUTPUT|CACHE_READ|CACHE_WRITE|REASONING|AUDIO|IMAGE
type MeasurementKind uint8 // PROVIDER_REPORTED|LOCAL_COUNTED|ESTIMATED|RECONCILED
type BillableState uint8 // BILLABLE|NOT_BILLABLE|UNKNOWN|UNPRICED_SUBSCRIPTION

type UsageItem struct {
    Category TokenCategory
    Units uint64
    Kind MeasurementKind
}

type UsageObservation struct {
    Items []UsageItem
    Billable BillableState
    ProviderUsageDigest string
    ObservedAt time.Time
}

type PriceRate struct {
    Category TokenCategory
    UnitsPerBlock uint64
    PricePerBlock Money
    ServiceTier string
}

type PriceSnapshot struct {
    SnapshotID, ProviderID, ModelID, Revision string
    Rates []PriceRate
    EffectiveFrom, ExpiresAt time.Time
    SourceRef, Digest string
    Signature []byte
}

type CostQuote struct {
    OperationID, SnapshotID string
    Maximum Money
    Assumptions []string
}

type CostSettlement struct {
    OperationID, AttemptNonce, SnapshotID string
    Actual *Money
    Usage UsageObservation
    Status string // SETTLED|ESTIMATED|PENDING_RECONCILIATION|UNPRICED
}
```

  Invarijante:

  1. Cjenik je pinned po attemptu. Model alias/promjena cijene usred runa ne prepisuje povijest. Unknown/expired snapshot ne daje `0`; vraća `PRICE_UNKNOWN`, a S7 policy odlučuje smije li uopće admitati unpriced target.
  2. Prije attempta cost engine računa conservative maximum iz input usagea, `max_output_tokens`, tier/cache/multimodal kategorija i snapshot-a; S7.2 atomically rezervira iz budgeta prije granta. Concurrent attempti ne mogu svi potrošiti isti ostatak.
  3. Nakon svakog ishoda, uključujući 4xx/5xx, timeout, truncation i unknown effect, provider usage se bilježi kao reported/estimated/unknown te rezervacija settlea ili ostaje bounded reconciliation liability. “Failed request” nije sinonim “free”.
  4. Provider reported usage i lokalni tokenizer procjena ostaju odvojeni. Reconciliation može dodati novu projekciju, ne mutirati originalnu opservaciju.
  5. CLI subscription bez per-call cijene je `UNPRICED_SUBSCRIPTION`, nikad `0 USD`. Prate se request count i dostupni token usage, uz zasebne hard capove; nije usporediv s API-key marginal costom bez eksplicitne policy pretpostavke.
  6. Sve operacije koriste checked integer/rational arithmetic i eksplicitno rounding pravilo “ceiling on reservation, provider-invoice precision on settlement”. Currency conversion nije dio v1; različite valute se ne zbrajaju.
  7. UI/TUI/telemetry su read-only projekcije iz journal događaja `cost.quoted`, `cost.settled`, `cost.reconciled`; ne vode paralelni total.

- **Salvage:** portirati `Usage`, per-turn/global cap ideju i actual winner attribution iz `/home/matej/NEXUSv2/llm/providers.py:58-125,743-807`, te Claude `total_cost_usd` extraction fixture iz `/home/matej/NEXUSv2/core/circuit.py:107-119`. Ne portirati statički `PRICE` dict, `len(text)/4`, float USD ni “missing cost = 0” (`providers.py:66-79`; `circuit.py:85-119`). `CostTracker` lock pokazuje potrebu za concurrency safety, ali novi durable reservation/settlement owner je S7.2.

- **Ugovor/RED:** Annex **P2.2** je glavni gate: budget prije admissiona, atomic accounting i hard abort. **P0.2** veže settlement uz jedinstveni attempt nonce. RED-ovi: `test_concurrent_quotes_cannot_oversubscribe_budget`, `test_unknown_price_is_not_zero`, `test_failed_or_truncated_call_retains_liability`, `test_cli_subscription_is_unpriced_not_free`, `test_price_snapshot_pinned_across_midrun_update`, `test_cache_reasoning_and_tier_rates_are_not_dropped`, `test_money_overflow_fails_closed`, `test_cost_projection_cannot_write_parallel_total`. Mutant koji `Money` zamijeni `float64` ili unknown s nulom mora pasti na boundary fixtureu.

- **Verifikacija:** [LiteLLM spend tracking](https://github.com/BerriAI/litellm-docs/blob/main/docs/proxy/cost_tracking.md) potvrđuje provider/model/tier cost map i spend breakdown, a [budget reservation docs](https://github.com/BerriAI/litellm-docs/blob/main/docs/proxy/users.md) potvrđuju estimate→reserve→settle obrazac. [Aider usage docs](https://aider.chat/docs/usage.html) potvrđuju user-facing token/cost reporting; [Helicone cost tracking](https://docs.helicone.ai/features/advanced-usage/costs) potvrđuje proxy/observability cost attribution. Kandidati ne uklanjaju drift cjenika niti invoice reconciliation, zato snapshot digest i unknown-not-zero ostaju naš dodatni floor.

- **Pareto floor:** **PASS za 2.5.** LiteLLM price breadth/reservation, Aider session UX i Helicone attribution ostaju, uz jaču money/concurrency/reconciliation semantiku. Najjači kontraargument je da nano-units ne mogu vjerno predstaviti svaki proizvoljni rate; `PriceRate` je rational `Money per UnitsPerBlock`, pa se ne uvodi decimal runtime. Najslabija točka je svježina provider cjenika; release/matrix mora dokazati source digest i expiry, a bill reconciliation ostaje zaseban dokazni strop.

## 2.6 Model routing (pravi model za pravi zadatak)

- **Tip:** MEHANIZAM; naučeni semantic router je opcionalni ADAPTER iza istog ABI-ja

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Naučeni strong/weak cost-quality prag | RouteLLM | Opcionalni scorer nad već policy-eligible kandidatima, s kalibriranom verzijom modela. |
| Brzi semantički intent route | semantic-router | Lokalni feature/scorer adapter bez remote embedding side effecta u baselineu. |
| Rules/cost/latency/load routing | LiteLLM Router | Deklarativni filteri i score komponente, ali ne njegova fallback/retry izvedba. |
| Deterministički cold-start barbell | NEXUS salvage | Static strong/cheap/local ordering kada nema dokazanih outcomesa. |
| Verified-outcome learning | NEXUS reward ledger | Učiti samo iz checker/acceptance rezultata vezanih uz artefakt i route revision, ne iz model self-reporta. |
| Policy-first eligibility | Annex P2.5 + S0.3 | Router nikada ne vidi zabranjeni provider kao birljivi cilj; exploration ne prelazi closure. |

- **Sinteza:** 2.6 je jedini owner `RouteDecision`. Router je čist nad pinned snapshotima; ne poziva model/provider, ne rezervira budget i ne izvršava fallback.

```go
type RouteRequest struct {
    OperationID, TaskType, Phase string
    RequiredCapabilities []contract.Capability
    DataPolicyHash, BudgetID string
    InputTokens, MaxOutputTokens uint64
    LatencySLO time.Duration
    QualityFloor uint32
    DeterminismSeed uint64
}

type Candidate struct {
    Target contract.TargetRef
    CapabilityAttestation string
    DataAuthorizationID string
    PriceSnapshotID string
    HealthSnapshotID string
    EstimatedMaxCost contract.Money
    EstimatedLatency time.Duration
    QualityClass uint32
}

type RouteDecision struct {
    DecisionID, OperationID string
    RouterID, RouterVersion string
    InputSnapshotHash string
    Ordered []contract.TargetRef
    Scores []TargetScore
    ReasonCodes []string
    CreatedAt, ExpiresAt time.Time
}

type Router interface {
    Rank(RouteRequest, []Candidate) (RouteDecision, *contracts.TypedError)
}

type OutcomeRecord struct {
    DecisionID, TargetID, ArtifactRevision string
    CheckerID, CheckerVersion string
    Passed bool
    QualityScore uint32
    SettledCost *contract.Money
    Latency time.Duration
    EvidenceRef string
}
```

  Normativni routing:

  1. Registry daje inventory, S0.3 uklanja capability mismatch, P2.5 autorizira konkretni endpoint, a S7.2/cost uklanja target koji ne stane u hard budget. Tek taj `[]Candidate` ulazi u `Router.Rank`.
  2. Baseline je deterministički barbell: eksplicitni task/quality policy bira minimalno sposoban cheap/local target, uz strong target kao sljedeći kandidat; tie-break je stabilni `TargetRef`, ne map iteration/random.
  3. Naučeni router je iza `learned-model-routing` capability flaga i istog `Router` ABI-ja. Model/scaler/threshold/training-data digesti i calibration report ulaze u `RouterVersion`; load failure atomically vraća baseline, ne polovično aktivan scorer.
  4. Default feature extraction je lokalna i data-minimal. Ako scorer traži remote embedding/LLM poziv, to je zasebna provider operacija s P2.5, cost budgetom i S7 grantom; skriveni routing call nije dopušten.
  5. Reward smije doći samo iz verificiranog checker/app-contract outcomea vezanog uz isti artifact revision i route decision. Modelov “uspjelo je”, user sentiment bez attributiona ili nedovršeni run ne mijenjaju weights.
  6. Exploration ostaje unutar policy-eligible, budget-fit i capability-fit seta, s bounded stopom. Security/data deny nema exploration override.
  7. `RouteDecision` je immutable i journaliran bez prompta; sadrži input snapshot hash i reason codes. Reproduciranje s istim requestom/snapshotom/seedem mora vratiti isti poredak.
  8. Nakon errora S2.2 može samo suziti/reorderirati preostali `Ordered` popis. S7 ponovno radi P2.5/budget gate i izdaje novi grant; router sam nikad ne zove provider.

- **Salvage:** Python→Go port task bucketa, verified `reward.best` i deterministic barbell cold-starta iz `/home/matej/NEXUSv2/llm/router.py:10-37`; portirati `select()`/provider preference kao baseline ideju iz `/home/matej/NEXUSv2/llm/providers.py:810-889`. Ne portirati plitki lexical bucket kao konačni semantic scorer, Python `FallbackModel`, unutarnji provider poziv ni implicitni random exploration. Existing reward records moraju se migrirati samo ako imaju artifact/checker evidence; inače su cold-start hints, ne trusted training truth.

- **Ugovor/RED:** Annex **P2.5** obvezno prethodi i ponavlja se na svaki reroute; **P0.2** osigurava da route decision nije attempt grant; **P0.4** aktivira learned router tek kad su model, gates i config hash u punom closureu. RED-ovi: `test_learned_top_choice_cannot_route_policy_denied_target`, `test_router_cannot_call_provider_or_reserve_budget`, `test_remote_embedding_router_requires_own_authorized_attempt`, `test_cold_start_barbell_is_deterministic`, `test_model_self_report_cannot_update_reward`, `test_outcome_wrong_artifact_revision_is_ignored`, `test_same_snapshot_seed_replays_same_order`, `test_learned_adapter_failure_atomically_falls_back_to_baseline`, `test_exploration_never_crosses_capability_data_or_cost_gate`.

- **Verifikacija:** [RouteLLM](https://github.com/lm-sys/RouteLLM) stvarno nudi trained strong/weak router, kalibrirani cost-quality threshold i evaluacijski okvir; njegov default model pair/dataset nije dokaz za NEXUS workload pa se weights ne graftaju bez lokalne kalibracije. [semantic-router](https://github.com/aurelio-labs/semantic-router) potvrđuje semantičko routing sučelje; [LiteLLM router](https://github.com/BerriAI/litellm) potvrđuje cost/latency/usage/load-balance strategije. NEXUS `router.py` stvarno uči pobjednika iz `passed` outcomea i ima barbell fallback, ali bucket je samo `phase:code/text:size` te nema data/capability attestation; port je ponašanje, ne kod 1:1.

- **Pareto floor:** **PASS za 2.6.** RouteLLM learned cost-quality, semantic-router intent signal, LiteLLM operational scores i NEXUS verified-outcome/barbell ponašanje ostaju iza jednog replayable ABI-ja. Najjači kontraargument je da policy-first filtering može ukloniti informacije potrebne scoreru; zabranjeni cilj ne smije biti opcija, ali agregirani non-sensitive comparative features mogu biti input ako ne omogućuju rekonstrukciju prompta/policy tajne. Najslabija točka je domain shift learned routera; baseline ostaje aktivan dok shadow evaluation na stvarnim NEXUS checker outcomeima ne dokaže kvalitetu i trošak bez porasta policy/timeout failurea.

## S2 sekcijski gate i redoslijed implementacije

1. `contract` + fake provider s one-grant/one-attempt enforcementom; P0.2 mutant mora RED.
2. P2.5 descriptor/authorization wiring prije HTTP serializer/DNS dialera; forbidden fallback sink mora ostati na 0.
3. API-key HTTP adapter kao prvi default auth mod; strict response limits, no-secret telemetry i OpenAI-compatible fixture.
4. Error normalizer/advisor bez retry petlje te deterministički baseline router; S7 fake jedini izdaje drugi grant.
5. Structured decoder i tool-call fixtures; tek zatim provider-specific structured modes.
6. Cost quote/settlement s S7.2 concurrent reservation gateom.
7. Local provider adapters + measured hardware-fit preflight na 3 OS-a.
8. Službeni OAuth adapter po provider manifestu; auth-code PKCE prvi, device flow samo gdje je službeno dopušten.
9. CLI-agent adapter s tool-disable/version probeom i upstream-attempt proof ceilingom.
10. Learned router u shadow modu; aktivacija tek nakon lokalnog calibration/holdout gatea.
11. Eksperimentalni subscription-OAuth replica adapter zadnji, izvan default build closurea, samo uz operator acknowledgement i verificirani legal/ToS manifest.

Sekcijski acceptance zahtijeva da jedan isti golden request kroz API-key, službeni OAuth, CLI-agent i local adapter proizvede isti kanonski `Response`/`ProviderError` oblik gdje capabilities dopuštaju, dok sink brojači dokazuju: policy deny = 0 fizičkih poziva; jedan grant = najviše 1 fizički poziv; drugi pokušaj postoji samo nakon novog S7 granta. Vanjski projekti potvrđuju aspekte, ali nisu runtime dependency ni vlasnici NEXUS ugovora.

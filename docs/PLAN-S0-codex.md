PLAN S0

# S0 — Kernel ugovori i izvršno stanje

## Zajedničke odluke

- Jedini normativni vlasnik S0 ugovora je ručno pisani Go kod u `internal/kernel/*`; JSON Schema, OpenAPI, protobuf i generatori smiju biti izvedene projekcije, nikad izvor istine.
- Jezgra ostaje `CGO_ENABLED=0` single-binary. Nema ugrađenog LangGraph/OpenHands/Temporal runtimea; preuzimaju se dokazani obrasci, ne procesi ni njihove ovisnosti.
- Predloženi paketi: `contracts`, `journal`, `machine`, `negotiation`, `migration`, `closure`. Ovisnosti teku `contracts <- journal <- machine`; `negotiation`, `migration` i `closure` ovise o `contracts`, ali ne jedni o drugima.
- `EventJournal.Append` je jedini trajni write-owner. State, log, trace, transcript i audit su projekcije kanonskih događaja.
- `map[string]any` nije dopušten u kernel API-jima. Opaque payload i nepoznata JSON polja nose se kao `json.RawMessage`; svaki poznati discriminator prolazi zatvorenu validaciju.

## 0.1 Typed message / tool-call / tool-result / error schema

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Interoperabilni tool-call/result sadržaj | MCP specification | MCP-ove razdvojene vrste sadržaja i strukturirani rezultat mapirati na kanonske `ContextBlock` vrijednosti; MCP ostaje adapter na rubu. |
| Minimalan zatvoren skup run-item tipova | OpenAI Agents SDK | Zasebne Go vrste za poruku, tool call, tool result, handoff/approval događaje; bez generičkog “item” objekta u jezgri. |
| Tipizirano stanje i reducer granica | LangGraph | State je izvedena tipizirana projekcija; reduceri primaju događaje i ne pišu trajno stanje mimo journala. |
| Provenance, trust i sensitivity | Annex P0.1 | Vlastiti `ContextBlock`; kandidatima nedostaje cijeli traženi trust/lineage ugovor. |
| Jedinstveni event identity/causality ugovor | Annex P0.1 + CloudEvents konvencije | Obvezni ID, type, version, parent, sequence i time atributi, ali s NEXUS-specifičnim run/turn/tool identitetom. |

- **Sinteza:** `internal/kernel/contracts` je dependency-free domena (osim Go stdlib). Skice su javni oblik ugovora, ne puni kod:

```go
type SchemaVersion uint32
type Sequence uint64

type Envelope struct {
    SchemaID       string          `json:"schema_id"`
    SchemaVersion  SchemaVersion   `json:"schema_version"`
    EventID        string          `json:"event_id"`
    EventType      EventType       `json:"event_type"`
    RunID          string          `json:"run_id"`
    TurnID         *string         `json:"turn_id"`
    ToolCallID     *string         `json:"tool_call_id"`
    ParentEventID  *string         `json:"parent_event_id"`
    Sequence       Sequence        `json:"sequence"`
    EmittedAt      time.Time       `json:"emitted_at"`
    ActorType      ActorType       `json:"actor_type"`
    ActorID        string          `json:"actor_id"`
    PrincipalID    string          `json:"principal_id"`
    TenantID       *string         `json:"tenant_id"`
    WorkspaceID    string          `json:"workspace_id"`
    AttemptNo      uint32          `json:"attempt_no"`
    IdempotencyKey *string         `json:"idempotency_key"`
    Payload        json.RawMessage `json:"payload"`
    PayloadHash    string          `json:"payload_hash"`
}

type Message struct {
    MessageID     string         `json:"message_id"`
    Role          MessageRole    `json:"role"` // SYSTEM|USER|ASSISTANT|TOOL
    ContentBlocks []ContextBlock `json:"content_blocks"`
    CreatedAt     time.Time      `json:"created_at"`
}

type ContextBlock struct {
    BlockID      string          `json:"block_id"`
    Kind         BlockKind       `json:"kind"`
    Content      json.RawMessage `json:"content,omitempty"`
    ContentRef   *ContentRef     `json:"content_ref,omitempty"`
    ContentHash  string          `json:"content_hash"`
    SourceURI    string          `json:"source_uri"`
    Producer     Producer        `json:"producer"`
    TrustClass   TrustClass      `json:"trust_class"`
    Sensitivity  Sensitivity     `json:"sensitivity"`
    Lineage      []LineageEntry  `json:"lineage"`
    ObservedAt   time.Time       `json:"observed_at"`
    ExpiresAt    *time.Time      `json:"expires_at"`
}

type ToolCall struct {
    ToolCallID         string          `json:"tool_call_id"`
    ToolID             string          `json:"tool_id"`
    Arguments          json.RawMessage `json:"arguments"`
    ArgumentsSchemaHash string         `json:"arguments_schema_hash"`
    EffectClass        EffectClass     `json:"effect_class"`
    Deadline           time.Time       `json:"deadline"`
    AttemptNo          uint32          `json:"attempt_no"`
    IdempotencyKey     *string         `json:"idempotency_key"`
}

type ToolResult struct {
    ToolCallID  string         `json:"tool_call_id"`
    AttemptNo   uint32         `json:"attempt_no"`
    Status      ResultStatus   `json:"status"`
    OutputBlocks []ContextBlock `json:"output_blocks"`
    Error       *TypedError    `json:"error"`
    StartedAt   time.Time      `json:"started_at"`
    FinishedAt  time.Time      `json:"finished_at"`
}

type TypedError struct {
    Code         string          `json:"code"`
    Category     ErrorCategory   `json:"category"`
    Retryability Retryability    `json:"retryability"`
    SafeMessage  string          `json:"safe_message"`
    RetryAfter   *time.Duration  `json:"retry_after"`
    Origin       string          `json:"origin"`
    CauseEventID *string         `json:"cause_event_id"`
}

type Validator interface {
    ValidateEnvelope(Envelope) *TypedError
    ValidateMessage(Message) *TypedError
    ValidateToolCall(ToolCall) *TypedError
    ValidateToolResult(ToolResult, ToolCall) *TypedError
    ValidateContextBlock(ContextBlock) *TypedError
}
```

  Obvezna pravila implementacije:

  1. `ContextBlock` ima točno jedno od `content` i `content_ref`; hash se provjerava nad kanonskim bajtovima sadržaja ili referencirane vrijednosti prije uporabe.
  2. State-changing `ToolCall` mora imati `idempotency_key`; `ToolResult` mora odgovarati postojećem `tool_call_id+attempt_no`.
  3. `TypedError.retryability` je jedini strojno čitljiv retry signal. `safe_message` se nikad ne parsira radi upravljanja tokom.
  4. Trust redoslijed je `SYSTEM > TOOL_TRUSTED > USER > UNTRUSTED_EXTERNAL`; transformacija smije zadržati ili sniziti povjerenje, nikad ga povisiti. Sensitivity se smije zadržati ili pooštriti.
  5. `PayloadHash` koristi jedan kanonski JSON encoder i SHA-256; NaN/Infinity, duplicate keys, unknown enum/discriminator i nekanonsko vrijeme se odbijaju prije appendanja.
  6. Wire adapter radi `external -> validated contracts -> kernel` i obratno. MCP/A2A/provider tipovi ne ulaze u kernel pakete.

- **Salvage:** Python→Go port samo ponašanja append/fold iz `/home/matej/NEXUSv2/core/events.py:17-50` i byte-stabilnog JSON-a iz `events.py:223-228`. Stari `kind+dict` događaj nije dovoljno tipiziran i ne portira se kao API. Atomski izvedeni `run.json` iz `events.py:246-266` kasnije je projekcija, ne drugi izvor istine.

- **Ugovor/RED:** Annex **P0.1** je obvezni gate, osobito `test_s0_rejects_provenance_laundering`. Dodatni RED testovi: `test_context_block_requires_exactly_one_body`, `test_state_change_requires_idempotency_key`, `test_unknown_discriminator_is_rejected`, `test_tool_result_attempt_must_match_call`. Svi moraju dokazati: rejection event je dopušten, ali run ne prelazi u `RUNNING` i model/tool sink ostaje na 0 poziva.

- **Verifikacija:** provjereno 2026-09-01 na primarnim izvorima. [MCP tools spec](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/draft/server/tools.mdx) stvarno razlikuje typed content i structured result; [OpenAI Agents SDK result model](https://openai.github.io/openai-agents-python/results/) stvarno razlikuje message/tool/handoff/approval iteme i ima resumable state; [LangGraph Graph API](https://docs.langchain.com/oss/python/langgraph/graph-api) stvarno definira state schema, node, edge i reducer. MCP je u licencnoj tranziciji opisanoj u njegovu [LICENSE](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/LICENSE); OpenAI Agents SDK i LangGraph repozitoriji navode MIT. Ne kopirati generator ni SDK kod: standard/SDK služe kao wire i type-shape dokaz.

- **Pareto floor:** **PASS za opseg 0.1.** MCP interop ostaje moguć kroz lossless adapter; OpenAI-jeva razdvojenost itema ostaje; LangGraphova typed-state/reducer granica ostaje. Sinteza dodaje Annex provenance, sensitivity, causality, idempotency i korelaciju koje nijedan kandidat sam ne pokriva. Najslabija točka je kanonski JSON profil: mora dobiti cross-language golden vectors prije zamrzavanja javnog wire formata.

## 0.2 Run/turn/tool state machine s legalnim prijelazima

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Eksplicitni graf i validacija prijelaza | LangGraph | Tablične legalne tranzicije i reducer nad eventima; nema izravnog mutiranja statusa. |
| Event stream kao izvor istine | OpenHands core | Svaka prihvaćena naredba prvo postaje kanonski događaj; prikazi su fold/projekcije. |
| Deterministički replay i durable semantics | Temporal | Workflow odluke moraju biti determinističke; vanjski/model/tool rad nije dio replay reducera. |
| Retry/cancel/deadline vlasništvo | Annex P0.2 | S7 izdaje svaki `AttemptGrant`; adapter fizički pokušaj ne smije sam ponoviti. |

- **Sinteza:** `internal/kernel/machine` je mali command handler nad `journal.EventJournal`; nije opći workflow engine.

```go
type RunState uint8     // CREATED, ADMITTED, RUNNING, SUCCEEDED, FAILED, CANCELLED, UNKNOWN
type TurnState uint8    // CREATED, RUNNING, SUCCEEDED, FAILED, CANCELLED
type AttemptState uint8 // PLANNED, AUTHORIZED, RUNNING, SUCCEEDED, FAILED, CANCELLED, UNKNOWN

type EntityKind uint8 // RUN|TURN|ATTEMPT

type TransitionCommand struct {
    EntityKind      EntityKind
    EntityID        string
    From            uint8
    To              uint8
    CauseEventID    string
    ExpectedSequence Sequence
    Reconciliation *Reconciliation
    AttemptGrant    *AttemptGrant
}

type TransitionResult struct {
    EventID string
    Sequence Sequence
    State uint8
}

type EventJournal interface {
    Append(ctx context.Context, expected Sequence, event contracts.Envelope) (JournalOffset, error)
    ReadRun(ctx context.Context, runID string, after Sequence) ([]contracts.Envelope, error)
}

type Machine interface {
    Transition(context.Context, TransitionCommand) (TransitionResult, *contracts.TypedError)
    Current(context.Context, EntityKind, string) (uint8, Sequence, error)
}
```

  Legalne tablice su compile-time podaci i jedini autoritet:

  - run: `CREATED->ADMITTED->RUNNING->{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}`;
  - turn: `CREATED->RUNNING->{SUCCEEDED|FAILED|CANCELLED}`;
  - attempt: `PLANNED->AUTHORIZED->RUNNING->{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}`;
  - terminalna stanja nemaju izlaz; `UNKNOWN` zahtijeva reconciliation event i smije samo u `SUCCEEDED|FAILED|MANUAL_RECOVERY`.

  Obrada je `validate command + expected state/sequence -> validate S7 grant/reconciliation -> build event -> EventJournal.Append -> return new projection`. Concurrent stale command gubi na compare-and-append provjeri; nikad ne radi “last writer wins”. Reducer je čista funkcija `Fold(previous, event)`, bez sata, RNG-a, mreže, modela ili filesystema. Tool/provider poziv smije nastupiti tek nakon durable `RUNNING` attempt događaja i mora predočiti jedinstveni, neistekli S7 `AttemptGrant`.

- **Salvage:** Python→Go port event-source ideje i snapshot-consistent folda iz `/home/matej/NEXUSv2/core/events.py:1-6,29-32`. Ne portirati `/home/matej/NEXUSv2/core/workflow.py:27-60`: proizvoljan string state, upsert mimo legalnog prethodnog stanja i progutani exceptioni krše P0.1/P0.2. `/home/matej/NEXUSv2/core/state.py` ostaje domenska projekcija, ne kernel machine.

- **Ugovor/RED:** Annex **P0.1** legalne tranzicije i **P0.2** vlasništvo retryja/cancela. Obvezni `test_adapter_cannot_self_retry`; dodatno `test_terminal_state_has_no_exit`, `test_stale_expected_sequence_loses`, `test_unknown_requires_reconciliation`, `test_replay_performs_zero_external_calls`, `test_cancel_propagates_and_closes_children`.

- **Verifikacija:** [LangGraph persistence](https://docs.langchain.com/oss/python/langgraph/persistence) stvarno checkpointa stanje po koraku i podržava history/replay, ali dokumentira da se rad nakon checkpointa ponovno izvršava; zato se ne grafta obećanje sigurne reprize vanjskih side-effecta. [Temporal workflow definition](https://github.com/temporalio/documentation/blob/main/docs/encyclopedia/workflow/workflow-definition.mdx) stvarno zahtijeva determinističan command slijed i izdvaja nedeterminističan rad u Activities; [Temporal Go SDK](https://github.com/temporalio/sdk-go/blob/master/workflow/workflow.go) pokazuje history-backed replay obrazac. [OpenHands core](https://github.com/OpenHands/openhands) je aktivan i core je MIT (enterprise podstablo nije); koristi se samo provjerljiv event-source obrazac.

- **Pareto floor:** **PASS za opseg 0.2.** Dobivamo eksplicitnost LangGrapha, event-source granicu OpenHandsa i Temporalovu determinističku semantiku bez vanjskog runtimea koji bi slomio single-binary. Ne tvrdimo Temporalovu distribuiranu durable-execution razinu: floor se odnosi na S0 state-machine semantiku, ne na cijeli Temporal proizvod. Najjači kontraargument je crash između fizičkog side-effecta i rezultata; `UNKNOWN + reconciliation`, idempotency i P1 draft/commit/compensate su obvezni odgovor, ne automatski retry.

## 0.3 Capability negotiation po modelu/provideru

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Širina model/provider metadata | LiteLLM model registry | Normalizirani limiti, modaliteti, tool/JSON/stream/reasoning support i regija, uz provenance i rok valjanosti. |
| Eksplicitna semantika sposobnosti | PydanticAI `ModelProfile` | Deklarirane, tipizirane sposobnosti; `unknown` se ne promovira u `supported`. |
| Policy-aware katalog i eligibility | Portkey Model Catalog | Allowlist/budget/rate-limit podaci ulaze kao constrainti, ali routing odluka ostaje vlasništvo S2.6. |
| Determinizam i fail-closed | Vlastiti kod + NEXUS salvage | Čista funkcija nad snapshotom descriptorâ; nema implicitne mreže ni fallbacka. |

- **Sinteza:** 0.3 određuje *eligible set* i objašnjava odbijanja; ne bira najjeftiniji/najbrži model i ne izvršava fallback (to pripada S2.6/S7).

```go
type Support uint8 // UNKNOWN|UNSUPPORTED|SUPPORTED

type Capabilities struct {
    Tools, ParallelTools, JSONSchema, Streaming, Reasoning Support
    InputModalities  []Modality
    OutputModalities []Modality
}

type ModelDescriptor struct {
    ProviderID, ModelID, DescriptorVersion string
    MaxInputTokens, MaxOutputTokens uint64
    Capabilities Capabilities
    Regions []string
    SourceURI, SourceDigest string
    ObservedAt, ExpiresAt time.Time
}

type Requirements struct {
    RequiredCapabilities []Capability
    InputModalities, OutputModalities []Modality
    MinInputTokens, MinOutputTokens uint64
    AllowedProviders, AllowedRegions []string
}

type Eligibility struct {
    Descriptor ModelDescriptor
    Eligible bool
    Rejections []ReasonCode
}

type Negotiator interface {
    Evaluate(Requirements, []ModelDescriptor, time.Time) ([]Eligibility, *contracts.TypedError)
}
```

  Pravila: descriptor bez verzije/digesta, s isteklim `ExpiresAt`, nepoznatim required supportom ili manjim limitom je neeligible; prazna allowlista znači policy-defined default, ne “dopusti sve”; rezultat je stabilno sortiran po `provider_id,model_id,descriptor_version`; input snapshot i odluka emitiraju se hashom u journal. Live probe može osvježiti descriptor samo kroz zaseban adapter/gate, nikad usred čiste evaluacije.

- **Salvage:** Python→Go port determinističkog, fail-closed resolver obrasca iz `/home/matej/NEXUSv2/core/capabilities.py:54-61,64-95`; ne portirati njegove hardkodirane multimodal backend nazive kao model ugovor. Statički podaci iz `/home/matej/NEXUSv2/core/capreg.py:80-98` mogu biti seed fixture, ne runtime autoritet.

- **Ugovor/RED:** odluka se zapisuje P0.1 `Envelope`om; P0.2 zabranjuje da negotiator ili provider adapter radi retry/fallback. RED: `test_unknown_support_never_satisfies_requirement`, `test_expired_descriptor_is_ineligible`, `test_negotiation_is_order_independent`, `test_negotiator_does_not_call_network`, `test_routing_cannot_select_rejected_model`.

- **Verifikacija:** [LiteLLM model registry](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json) i njegov schema oblik stvarno nose context/output limite, modalities i brojne support oznake; koristi se samo podatkovni oblik jer repozitorij ima miješane/community-enterprise licencne granice. [PydanticAI ModelProfile](https://pydantic.dev/docs/ai/api/pydantic-ai/profiles/) stvarno deklarira tools, JSON schema/output, media/thinking/native-tool sposobnosti; projekt je MIT. [Portkey Model Catalog](https://portkey.ai/docs/product/model-catalog) stvarno centralizira modele, limite, pricing i policy kontrole, ali katalog je proizvodna/hostana površina; zato je samo referentni obrazac, ne kernel dependency. Portkey gateway repo je MIT, no to ne proširuje automatski licencu na hostani katalog.

- **Pareto floor:** **PASS za negotiation mehanizam.** Metadata širina, eksplicitni profile i policy constrainti su očuvani, a tri-state/freshness/provenance uklanjaju opasan “missing=false/true” drift. Ne pokušavamo doseći LiteLLM-ovu brojnost adaptera ni Portkeyev hosted control plane u jezgri; oni pripadaju adapterima. Najslabija točka je svježina provider podataka, pa matrix gate mora uključiti stale/contradictory descriptor fixtures i jasno mjeriti datum zadnje potvrde.

## 0.4 Verzioniranje i migracija event/sesijskog formata

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Stabilan event context/version identitet | CloudEvents | `id/type/specversion/dataschema` obrazac, mapiran na stroži S0 Envelope. |
| Deterministična evolucija replay logike | Temporal versioning | Eksplicitni version marker/upcaster ID i replay testovi starih historija. |
| Event-history kontinuitet | OpenHands | Event history ostaje raw source; izvedeni trenutni oblik se može ponovno izgraditi. |
| Lossless one-step upcast i karantena | Annex P0.1 | Jedan registry, bez preskakanja verzija, bez downcasta, sa hash dokazom svake migracije. |

- **Sinteza:** `internal/kernel/migration` nikad ne prepisuje izvorni zapis. Registry se zamrzava pri bootu; duplicate ili gap ruši boot prije primanja runa.

```go
type SchemaKey struct { ID string; Version contracts.SchemaVersion }
type RawEvent struct { Bytes []byte; Hash string }

type MigrationReceipt struct {
    MigrationFrom, MigrationTo SchemaKey
    MigrationID string
    InputHash, OutputHash string
}

type Upcaster interface {
    ID() string
    From() SchemaKey
    To() SchemaKey // MUST biti ista ID i From.Version+1
    Upcast(json.RawMessage) (json.RawMessage, *contracts.TypedError)
}

type Registry interface {
    DecodeAndUpcast(RawEvent, SchemaKey) (
        contracts.Envelope, []MigrationReceipt, *contracts.TypedError)
    Current(string) (contracts.SchemaVersion, bool)
}
```

  Algoritam: (1) hash i durable pohrana raw bajtova; (2) strict decode outer envelope uz hvatanje unknown polja kao `map[string]json.RawMessage`; (3) lookup exact `schema_id/version`; (4) primjena isključivo `vN->vN+1`; (5) nakon svakog koraka provjera stabilnih ID/parent/sequence, očuvanja unknown polja i monotone trust/sensitivity politike; (6) receipt s input/output hashom; (7) validacija current sheme. Unknown ID/version, gap, duplicate upcaster ili lossy transformacija ide u karantenu i ne ulazi u state fold. Downcast API ne postoji.

  Za kompatibilnost se čuvaju tri fixture sloja: raw golden events svih izdanih verzija, očekivani current projection i očekivani migration receipts. `schema/codegen` može generirati dokumentaciju i adapter test vectors tek iz ovih Go contracta.

- **Salvage:** `/home/matej/NEXUSv2/core/store.py:118-140,168-191,204-219` daje korisne concurrency i “newer runtime fails” slučajeve za port testova, ali njegove in-place `ALTER/DROP` DB migracije nisu event upcaster i ne portiraju se kao 0.4 dizajn. Byte-stabilna serializacija iz `events.py:223-228` prenosi se kao golden-vector ponašanje.

- **Ugovor/RED:** Annex **P0.1**, ponovno `test_s0_rejects_provenance_laundering`, sada pokrenut za svaki upcast korak. Dodatni RED: `test_unknown_schema_is_quarantined`, `test_upcast_gap_rejects_registry`, `test_unknown_fields_survive_all_upcasts`, `test_id_parent_sequence_cannot_change`, `test_downcast_api_absent`, `test_old_golden_history_replays_to_same_state`.

- **Verifikacija:** [CloudEvents spec](https://github.com/cloudevents/spec/blob/main/cloudevents/spec.md) stvarno propisuje event context atribute poput `id`, `source`, `specversion`, `type` i `dataschema`; [CloudEvents primer](https://github.com/cloudevents/spec/blob/main/cloudevents/primer.md) jasno ostavlja event data versioning proizvođaču, pa se CloudEvents ne navodi kao dokaz za naš migration registry. [Temporal workflow definition/versioning](https://github.com/temporalio/documentation/blob/main/docs/encyclopedia/workflow/workflow-definition.mdx) stvarno obrađuje determinističnu evoluciju workflow koda. OpenHands repo potvrđuje event-history arhitekturu, ali pregledani primarni materijal nije dokazao strogi version-by-version lossless upcaster; taj aspekt se **ne grafta** kao navodna OpenHands sposobnost.

- **Pareto floor:** **PASS za dokazano.** CloudEvents envelope identitet i Temporalov deterministic-evolution obrazac ostaju; OpenHands se koristi samo za potvrđeni history/source model. Annex dodaje jači lossless registry, karantenu i migration receipts. Najslabija točka je očuvanje unknown JSON polja kroz više koraka; property/fuzz test mora generirati nested unknown vrijednosti i kontroliranom mutant-verzijom dokazati da gate postaje RED.

## 0.5 Capability-flag closure resolver

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Tranzitivni requirements/capabilities i all-or-fail resolution | OSGi Resolver model | Potpuni konzistentni closure ili `REJECTED`; nema parcijalnog active seta. |
| Determinističan additive feature graf | Cargo features | Eksplicitni `requires/provides`; stabilna closure ekspanzija, ali bez Cargo implicitnog union/bundle aktiviranja. |
| Konfiguracijski uvjeti | Bazel configurable attributes | Trigger/policy uvjet je zaseban input resoluciji, ne skriveni side effect manifesta. |
| Gate attestations i atomska objava | Annex P0.4 | Svaki flag mora imati sve gate dokaze nad istim config hashom prije jedne objave. |
| Minimalna kernel implementacija | Vlastiti kod | Jedan resolver i immutable `ActiveSet`; nema plugin frameworka ni vanjskog DI/runtimea. |

- **Sinteza:** `internal/kernel/closure` radi nad prethodno verificiranim, inertnim manifestima. Manifest ne sadrži executable command. Signature/artifact provjera pripada supply-chain gateu; resolver samo zahtijeva valjanu attestation referencu.

```go
type FlagRef struct { ID, Version string }
type GateRef struct { ID, Version string }
type AdapterRef struct { ID, ArtifactDigest string }

type CapabilityManifest struct {
    ManifestSchemaVersion string
    FlagID, FlagVersion string
    Provides, Requires, Conflicts []FlagRef
    Gates []GateRef
    Adapters []AdapterRef
    TriggerID, TriggerVersion string
    ConfigHash, Signature string
}

type GateAttestation struct {
    GateID, GateVersion, ImplementationID, ConfigHash string
    Status GateStatus
    VerifiedAt, ExpiresAt time.Time
    EvidenceRef string
}

type ResolutionPlan struct {
    State ResolutionState // DECLARED|RESOLVED|VALIDATED|REJECTED
    Generation uint64
    ConfigHash string
    OrderedFlags []FlagRef
    ManifestDigest, EvidenceDigest string
}

type ActiveSet struct {
    Generation uint64
    ConfigHash string
    Flags []FlagRef
    AdapterDescriptors []AdapterRef
}

type Resolver interface {
    Resolve(ResolveInput) (ResolutionPlan, *contracts.TypedError)
}
type ActiveSetStore interface {
    Current() *ActiveSet
    Publish(expectedGeneration uint64, validated ResolutionPlan) (*ActiveSet, error)
}
```

  Determinističan postupak:

  1. U početni set ulaze samo flagovi čiji je verzionirani trigger nastupio; bundle-alias se samo razvija u prijedloge i ne predstavlja trigger.
  2. Expand `requires` do fiksne točke; `provides` zadovoljava requirement samo uz jednoznačnog, verzijski kompatibilnog providera.
  3. Odbij unknown schema/flag/gate version, invalid signature/config hash, missing requirement, ambiguity, conflict i cycle. Cycle se prijavljuje punom stabilno sortiranom putanjom.
  4. Stabilni topološki red koristi `(flag_id,flag_version)` kao tie-break; identičan input daje identične digestove.
  5. Svaki gate mora imati `PASS`, isti `config_hash`, važeći rok i evidence reference. Policy/manifest smije samo suziti kernel maksimum.
  6. Cijeli plan postaje `VALIDATED` prije objave. `Publish` radi generation-CAS i jednu `atomic.Pointer[ActiveSet]` zamjenu; failure ostavlja prethodni pointer. Adapter code/artifact nije učitan niti callable prije objave; nakon objave adapter loader ponovno provjerava generation i artifact digest.
  7. Na restartu se active set ponovno računa iz manifestâ, triggerâ i attestations; ne vjeruje se starom cacheu. Promjena bilo kojeg manifesta/configa/evidencea mijenja digest i zahtijeva novu objavu.

- **Salvage:** Python→Go port stroge manifest validacije, duplicate-owner odbijanja i no-execute loada iz `/home/matej/NEXUSv2/core/pack_registry.py:143-205,215-220`. `/home/matej/NEXUSv2/core/capreg.py:80-98` može dati početni katalog capability ID-jeva, ali nema transitive closure, conflicts, versions, attestations ni atomsku aktivaciju; zato nije mehanizam za reuse. GORTEX Go trace u `/home/matej/NEXUSv2/gortex/observability.go:27-61` nije salvage za resolver ni journal: bounded queue tiho gubi zapise i piše paralelnim putem.

- **Ugovor/RED:** Annex **P0.4** i njegov obvezni `test_every_flag_fails_when_one_gate_is_ablated` za svih 18 flagova. Dodatni RED: `test_bundle_alias_without_trigger_activates_nothing`, `test_cycle_and_conflict_leave_previous_generation`, `test_stale_attestation_is_rejected`, `test_config_hash_mismatch_is_rejected`, `test_publish_cas_prevents_split_brain`, `test_adapter_unavailable_before_active_publish`, `test_manifest_change_invalidates_cached_closure`.

- **Verifikacija:** [Cargo features](https://doc.rust-lang.org/nightly/cargo/reference/features.html) stvarno koristi additive feature uniju; upravo zato ne preuzimamo implicitnu uniju koja bi kršila trigger invariant. [Bazel configurable attributes](https://bazel.build/versions/7.7.0/docs/configurable-attributes) stvarno daje eksplicitni configuration-conditioned selection, ali nije potpuni capability closure resolver. [OSGi Resolver](https://docs.osgi.org/specification/osgi.core/7.0.0/service.resolver.html) stvarno modelira resources, requirements, capabilities i konzistentan wiring. Sva tri su referentni obrasci; njihov kod se ne vendora. Ne postoji verificiran OSS kandidat koji već zadovoljava Annex P0.4 kao cjelinu, pa vlastiti mali resolver ostaje opravdan.

- **Pareto floor:** **PASS.** OSGi-jeva potpuna konzistentnost, Cargo deterministična/additive closure ideja i Bazel eksplicitni config uvjet ostaju, uz stroži trigger, gate-attestation i atomic-publish ugovor. Sinteza je namjerno uža od svakog općeg build/module sustava i održava samo NEXUS flag graf. Najslabija točka je crash granica objave: durable zapis smije označiti najviše `VALIDATED`; jedini runtime `ACTIVE` autoritet je atomski pointer, a restart uvijek ponovno validira. Crash/fault-injection matrica mora dokazati da se nikad ne vidi miješana generacija.

## S0 izlazni gate

S0 se smatra spremnim za sljedeću sekciju tek kada:

1. svi Annex P0.1 i P0.4 RED testovi prvo padaju na kontrolirano oslabljenoj implementaciji, zatim prolaze;
2. P0.2 `test_adapter_cannot_self_retry` prolazi kroz stvarni fake transport boundary;
3. P0.3 `EventJournal.append()` ugovor ima definiran API iz 0.2 i njegov `test_projection_cannot_bypass_journal` je GREEN — puna journal implementacija može biti planirana u S15, ali paralelni writer nije dopušten;
4. fuzz/property testovi pokrivaju JSON decode, migration unknown-field preservation, legal transition graf i closure cycle/conflict graf;
5. race test i crash/fault-injection provjere potvrde compare-and-append i active-set publication;
6. `GOOS=linux`, `windows`, `darwin` uz `CGO_ENABLED=0` kompajliraju kernel pakete bez platformskog sidecara;
7. golden wire vectors i public enum/error code lista budu zamrznuti kao verzionirani compatibility fixturei.


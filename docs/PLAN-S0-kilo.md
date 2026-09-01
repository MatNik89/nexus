PLAN S0

# PLAN-S0 — S0 ugovori, stanje, lifecycle (hibrid-hibrida sinteza, batch 1)

Jezgra `CGO_ENABLED=0`. S0 je 100% MEHANIZAM (mi pišemo). Gate: Annex A P0.1 (envelope/state/
migracija/ContextBlock) i P0.4 (closure resolver). Format po HARNESS-PLAN.md.

---

## 0.1 Typed message / tool-call / tool-result / error schema

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- wire-interop tool/resource ugovor (JSON-RPC, content types) → **MCP spec** (standard)
- minimalan typed loop/tool/handoff ugovor → **OpenAI Agents SDK** (harness)
- typed messages u state-graphu + checkpoint → **LangGraph** (harness)

**Sinteza (Go structovi, JSON tagovi, `json.RawMessage` payload):**
```go
type Envelope struct {
    SchemaID, SchemaVersion string              // registry key
    EventID  string `json:"event_id"`           // ULID
    EventType string `json:"event_type"`
    RunID string `json:"run_id"`
    TurnID, ToolCallID, ParentEventID *string `json:",omitempty"`
    Sequence uint64 `json:"sequence"`           // monotono po runu
    EmittedAt time.Time `json:"emitted_at"`     // UTC
    ActorType, ActorID, PrincipalID string
    TenantID *string `json:",omitempty"`
    WorkspaceID string
    AttemptNo uint32
    IdempotencyKey *string `json:",omitempty"`
    Payload json.RawMessage `json:"payload"`
    PayloadHash string `json:"payload_hash"`    // sha256(payload)
}

type ErrCategory int // iota: VALIDATION|AUTHN|AUTHZ|POLICY|RESOURCE|TIMEOUT|
                     //       CANCELLED|DEPENDENCY|CONFLICT|INTERNAL|UNKNOWN_EFFECT
type Retryability int // NEVER | S7_POLICY | AFTER
type TypedError struct {
    Code string; Category ErrCategory; Retryability Retryability
    SafeMessage string; RetryAfter *int64; Origin string; CauseEventID *string
}

type TrustClass int // SYSTEM | USER | TOOL_TRUSTED | UNTRUSTED_EXTERNAL
type Sensitivity int // PUBLIC | INTERNAL | CONFIDENTIAL | SECRET
type ContextBlock struct {
    BlockID, Kind string
    Content json.RawMessage `json:",omitempty"` // XOR
    ContentRef *string `json:",omitempty"`      // XOR
    ContentHash string
    SourceURI, Producer string
    TrustClass TrustClass; Sensitivity Sensitivity
    Lineage []string
    ObservedAt time.Time; ExpiresAt *time.Time `json:",omitempty"`
}

type ToolCall struct { ToolCallID, ToolID string; Arguments json.RawMessage
    ArgumentsSchemaHash string; EffectClass EffectClass // READ_ONLY|REVERSIBLE|IRREVERSIBLE
    Deadline time.Time; AttemptNo uint32; IdempotencyKey *string }
type ToolResult struct { ToolCallID string; AttemptNo uint32
    Status ResultStatus // SUCCEEDED|FAILED|CANCELLED|UNKNOWN
    OutputBlocks []ContextBlock; Error *TypedError; StartedAt, FinishedAt time.Time }
```
Validacija: `Validator.Validate(Envelope)` — odbija unknown discriminator (fail-closed), čuva
unknown polje; `Content` XOR `ContentRef` provjeren u konstruktoru.

**Salvage:** NEXUSv2 `core/events.py` (typed eventi + `RUN_JSON_SCHEMA`) → **Python→Go port**;
GORTEX tool-response schema = **direktan Go reuse** (već tipiziran).

**Ugovor/RED:** **P0.1** + `test_s0_rejects_provenance_laundering` — untrusted ContextBlock kroz
compaction/upcaster koji skine `lineage` i digne `trust_class=SYSTEM` → `PROVENANCE_DOWNGRADE`,
run ne smije u `RUNNING`, model sink 0 poziva.

**Verifikacija:** MCP (Apache-2.0, aktivan), OpenAI Agents SDK (MIT, aktivan), LangGraph (MIT,
aktivan) — svi stvarni.

**Pareto floor:** naš Envelope je NADSKUP sva tri (MCP interop preko `EventType`+payload-kinda;
OpenAI minimalnost preko jedne strukture; LangGraph typed-message preko `ContextBlock`), + `trust_class/
sensitivity/lineage` koje niti jedan od 3 nema → sinteza ≥ svaki na svakom aspektu.

---

## 0.2 Run/turn/tool state machine s legalnim prijelazima

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- eksplicitni graf stanja + checkpoint → **LangGraph**
- event stream kao izvor istine za stanje → **OpenHands**
- durable state machine + reconciliation → **Temporal**

**Sinteza (eksplicitna prijelazna tablica; stanje = derivat event streama, ne mutable polje):**
```go
type RunState int  // CREATED|ADMITTED|RUNNING|SUCCEEDED|FAILED|CANCELLED|UNKNOWN
type TurnState int // CREATED|RUNNING|SUCCEEDED|FAILED|CANCELLED
type AttemptState int // PLANNED|AUTHORIZED|RUNNING|SUCCEEDED|FAILED|CANCELLED|UNKNOWN

type Transition struct { Event EventType; From, To State; Guard func(*Envelope) bool }
// run:  CREATED→ADMITTED→RUNNING→{SUCCEEDED|FAILED|CANCELLED|UNKNOWN}
// UNKNOWN → samo SUCCEEDED|FAILED|MANUAL_RECOVERY (reconciliation event)
var runTransitions = map[RunState][]Transition{ ... } // tabelarno, jedan izvor

func (sm *StateMachine) Apply(cur State, e *Envelope) (State, error) {
    // legalna tranzicija? default: reject (fail-closed) — nevalidan prijelaz = TypedError{INTERNAL}
}
func (sm *StateMachine) Reconcile(cur RunState, e *Envelope) (RunState, error) // UNKNOWN→terminal
```
`StateMachine` je čist (bez I/O); stanje se REKONSTRUIRA foldom eventa iz journala (P0.3), pa je
checkpoint = offset u journal, ne snapshot mutabilnog stanja.

**Salvage:** NEXUSv2 `core/state.py` (GoalState) + `loop_engine.py` status enum → port, ali uz
ISPRAVAK: NEXUS je imao implicitne `if/return` prijelaze, bez tablice — ovo uvodi eksplicitnu
tabelu + default-reject (razlog postojanja 0.2).

**Ugovor/RED:** **P0.1** „Stateovi MUST biti" (run/turn/attempt legalni prijelazi; `UNKNOWN` samo
reconciliation). RED (iz P0.1): nelegalan prijelaz (npr. `RUNNING→ADMITTED`) → reject; `SUCCEEDED→
RUNNING` → reject; provjera da reconciliation event jedini otvara `UNKNOWN`.

**Verifikacija:** LangGraph/OpenHands/Temporal — svi MIT, aktivni.

**Pareto floor:** eksplicitna tablica + guardovi (LangGraph-grade) + event-derived state
(OpenHands-grade) + `UNKNOWN→reconciliation` (Temporal-grade) → ≥ svaki.

---

## 0.3 Capability negotiation po modelu/provideru

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- model-info po provideru (streaming/tools/context limit) → **LiteLLM**
- deklarirane sposobnosti na adapteru → **PydanticAI**
- capability-aware routing (routiraj po sposobnosti, ne imenu) → **Portkey AI Gateway**

**Sinteza (capability kao DATA + fail-closed na nepoznato — „mjeri, ne pretpostavljaj"):**
```go
type CapabilityFloor struct { ToolCalling, StructuredOutput, Streaming bool; ContextLimit int64 }

type DeclaredCaps struct { ProviderID, ModelID string; Cap CapabilityFloor } // iz providera
type MeasuredCaps  struct { Cap CapabilityFloor; ProbedAt time.Time }        // iz probe/usage

type ModelCapability struct { Declared *DeclaredCaps; Measured *MeasuredCaps }

type CapabilityMatrix struct { byID map[string]ModelCapability }
func (m *CapabilityMatrix) Resolve(provider, model string) (ModelCapability, error)
// nepoznato → error (fail-closed): pozivatelj MORA probati prije upotrebe sposobnosti
func (c ModelCapability) Effective() CapabilityFloor // Measured nadjačava Declared; floor je min
```
Capability floor je `min(declared, measured)`; ispod floora harness kompenzira (constrained
decoding / edit-format), ali se granica MJERI.

**Salvage:** NEXUSv2 `llm/providers.py` (`configure_json_output`, heavy-gate) + `core/capabilities.py`
+ `core/capreg.py` → **Python→Go port** (NEXUS već ima fail-closed capability gate — direktno).

**Ugovor/RED:** **bez zasebnog Annex ugovora** (poštena praznina) — gate = capability-floor načelo
(Vodeće načelo) + P2.5 (data-rezidencija, 0.3-susjedni aspekt). RED (naš, izvan Annex): `Resolve`
nepoznatog provider/modela → error; pokušaj `ToolCalling` na modelu čiji `Effective().ToolCalling
== false` → `TypedError{POLICY}`. **Flag:** 0.3 je kandidat za Tier-3 rezidual (dedicirani RED).

**Verifikacija:** LiteLLM (MIT, aktivan), PydanticAI (MIT, aktivan), Portkey (aktivan, core MIT).

**Pareto floor:** capability-as-data (LiteLLM) + declared-on-adapter (PydanticAI) + capability-aware
routing (Portkey) + fail-closed-on-unknown (naš dodatak, niti jedan od 3 ne fail-closa) → ≥ svaki.

---

## 0.4 Verzioniranje i migracija event/sesijskog formata

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- verzioniran event stream → **OpenHands**
- envelope sa schema verzijom (`schema_id`+`version`+`datacontenttype`) → **CloudEvents**
- workflow versioning patterns (version-per-step, upcast) → **Temporal**

**Sinteza (registry + monoton upcast + karantena + lossless):**
```go
type Upcaster func(old json.RawMessage) (json.RawMessage, error) // lossless v→v+1
type SchemaRegistry struct {
    byID map[string]map[int]Upcaster // schema_id -> version -> upcaster
}
func (r *SchemaRegistry) Upcast(schemaID string, from, to int, raw json.RawMessage) (json.RawMessage, error)
// lanac from..to, svaki korak lossless; nepoznat schema/verzija → QUARANTINE (ne odbij tiho, ne downcast)

type MigrationRecord struct { From, To string; MigrationID string; InputHash, OutputHash string }
// zapisuje se za SVAKI upcast; immutable raw event se ČUVA netaknut
```
Downcast zabranjen (nema `to < from`); unknown field se očuva kroz `json.RawMessage` (bez
re-marshal gubitka), unknown discriminator se odbija.

**Salvage:** NEXUSv2 `core/store.py` (`SCHEMA_VERSION`+`_migrate`) + `core/events.py`
(`RUN_JSON_SCHEMA`) → **Python→Go port**; `modernc.org/sqlite` za durable registry.

**Ugovor/RED:** **P0.1** „Migracija MUST" (immutable raw, registry, monoton upcast, karantena,
bez lossy downcast, migration_record). RED: upcast koji skine `lineage`/digne `trust_class` →
`PROVENANCE_DOWNGRADE` (isti P0.1 RED kao 0.1, ali kroz upcaster).

**Verifikacija:** OpenHands (MIT), CloudEvents (CNCF, Apache-2.0), Temporal (MIT) — svi stvarni.

**Pareto floor:** registry+monoton upcast (Temporal-grade) + envelope schema-verzija
(CloudEvents-grade) + immutable raw + karantena (naš dodatak — niti jedan od 3 ne karantenira
nepoznato) → ≥ svaki.

---

## 0.5 Capability-flag closure resolver

**Tip:** MEHANIZAM (jezgra; nema OSS ekvivalenta ove vrste)

**Aspekti → najbolji izvor (referentni obrasci):**
- feature resolution: `requires/conflicts/optional`, tranzitivni closure, cycle-detekcija → **Cargo**
- capability/requirement matching + resolver → **OSGi**
- hermetic dependency graph + topološka aktivacija → **Bazel**

**Sinteza (manifest + gate-attestation + atomska aktivacija):**
```go
type CapabilityManifest struct {
    ManifestSchemaVersion, FlagID, FlagVersion string
    Provides, Requires, Conflicts []string
    Gates, Adapters []string
    TriggerID, TriggerVersion string
    ConfigHash, Signature string
}
type GateAttestation struct {
    GateID, GateVersion, ImplementationID, ConfigHash string
    Status string; VerifiedAt, ExpiresAt time.Time; EvidenceRef string
}

type Resolver struct { manifests map[string]CapabilityManifest; atts map[string]GateAttestation }
func (r *Resolver) Resolve(flags []string) (active []string, err error) {
    // 1) tranzitivni closure  2) cycle/conflict/unknown-version → REJECTED
    // 3) topološki redoslijed 4) svi gate attestationi nad ISTIM config_hash
    // 5) atomski objavi aktivni set (parcijalno ACTIVE nije legalno)
}
type State int // DECLARED→RESOLVED→VALIDATED→ACTIVE; failure→REJECTED
```
Bundle-alias NIJE capability: `multimodal` se širi u {vision,image-out,voice,video,documents} ali
aktivira SAMO one čiji je `trigger_id` stvarno nastupio (ne forsira).

**Salvage:** NEXUSv2 `core/capabilities.py` + `core/capreg.py` (usable heavy-gate) → **Python→Go
port** (NEXUS ima capability resolution, ali NE formalni closure s conflicts/atomskom aktivacijom —
ovo je nadogradnja, ne copy).

**Ugovor/RED:** **P0.4** + `test_every_flag_fails_when_one_gate_is_ablated` — za svaki od 18
flagova ukloni jedan gate → `INCOMPLETE_CAPABILITY_CLOSURE`, aktivni set ostaje prethodna verzija,
nijedan adapter nevaljanog closurea nije učitan.

**Verifikacija:** Cargo (rust-lang, Apache-2.0/MIT, aktivan), Bazel (Google, Apache-2.0, aktivan),
OSGi (Eclipse, EPL, aktivan standard) — svi stvarni.

**Pareto floor:** closure+cycle/conflict (Cargo-grade) + capability/requirement matching (OSGi-grade)
+ topološka aktivacija (Bazel-grade) + gate-attestation nad istim hash + atomska aktivacija (naš
dodatak — niti jedan od 3 ne radi gate-attestation) → ≥ svaki.

---

## Cross-cutting napomene za S0 batch

1. **Enumi u Go:** `iota` + `String()` + `Valid()` funkcija; default-reject grana u svakom
   validatoru (kompenzira nedostatak sum-types — Rust bi bio rigorozniji, ali je odluka Go).
2. **Jedan schema owner:** svi tipovi iz 0.1 su JEDINI izvor za 0.2/0.3/0.4/0.5 (i za S7/S15
   kasnije) — nema duplih definicija (P0.3 write-owner ovisi o ovome).
3. **Honest gap:** 0.3 (capability negotiation) nema dedicirani Annex RED ugovor — jedina S0
   podsekcija bez svog RED gatea; predlažem da se u matričnom prep-passu doda P0.5 „capability
   matrix fail-closed" ili se 0.3 eksplicitno veže na capability-floor načelo kao gate.
4. **Verifikacija aspekata:** svi top-3 kandidati + referentni obrasci su stvarni (licence/aktivnost
   gore); jedini „salvage" koji traži provjeru prije port je NEXUS `capreg.py` (interni, postoji —
   audit potvrdio `core/capabilities.py`+`core/capreg.py`).

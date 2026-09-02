DESIGN S0 + SANDBOX

# Paketni rez

```text
internal/kernel/contracts       # S0.1; bez I/O
internal/kernel/machine         # S0.2; čisti fold
internal/kernel/negotiation     # S0.3; čisti resolver
internal/kernel/schema          # S0.4; registry/upcast/quarantine sink
internal/kernel/closure         # S0.5; čisti Resolve + stateful Activator
internal/security/sandbox       # S6.2 zajednički ugovor
internal/security/sandbox/linux # linux build tag
internal/security/sandbox/win   # windows build tag; v1 UNAVAILABLE
internal/security/sandbox/mac   # darwin build tag; v1 UNAVAILABLE
```

```text
contracts <- machine
contracts <- negotiation
contracts <- schema
contracts <- closure <- journal
contracts <- sandbox
```

## Zajedničke primitive

```go
package contracts

type (
    SchemaID       string
    EventID        string
    RunID          string
    TurnID         string
    ToolCallID     string
    PrincipalID    string
    TenantID       string
    WorkspaceID    string
    ActorID        string
    BlockID        string
    CapabilityID   string
    FlagID         string
    GateID         string
    AdapterID      string
    EvidenceRef    string
    IdempotencyKey string
    Digest         [32]byte
)

type SchemaRef struct {
    ID      SchemaID
    Version uint32
}

type CanonicalJSON struct {
    Bytes json.RawMessage // owned copy; RFC-8785 canonical form at boundary
    Hash  Digest          // SHA-256(Bytes)
}

type ContentRef struct {
    URI    string
    Digest Digest
    Size   uint64
}

type Optional[T any] struct {
    Value T
    Valid bool
}
```

**Invarijante**

- ID string: non-empty, UTF-8, maksimalno 128 bajtova, bez kontrole/NUL-a.
- `CanonicalJSON.Bytes`: maksimalna veličina određena pozivnim ugovorom; hash se ponovno računa.
- Vrijeme na wireu: UTC RFC3339Nano; trajanje/deadline u procesu: monotoni `time.Time`/`time.Duration`.
- `Optional.Valid=false`: `Value` se ignorira; nema sentinel praznih ID-eva.
- Konstruktor radi defensive copy; `Validate()` obvezan na svakom decode boundaryju.

---

# S0.1 — kanonski typed ugovori

## Tipovi

```go
package contracts

type ActorType uint8
const (
    ActorKernel ActorType = iota + 1
    ActorUser
    ActorAgent
    ActorTool
    ActorService
)

type EventType string // registry-validiran zatvoreni discriminator po schema ID-u

type Envelope struct {
    Schema           SchemaRef
    EventID          EventID
    EventType        EventType
    RunID            RunID
    TurnID           Optional[TurnID]
    ToolCallID       Optional[ToolCallID]
    ParentEventID    Optional[EventID]
    Sequence         uint64
    EmittedAt        time.Time
    ActorType        ActorType
    ActorID          ActorID
    PrincipalID      PrincipalID
    TenantID         Optional[TenantID]
    WorkspaceID      WorkspaceID
    AttemptNo        uint32
    IdempotencyKey   Optional[IdempotencyKey]
    Payload          CanonicalJSON
}

type Role uint8
const (
    RoleSystem Role = iota + 1
    RoleUser
    RoleAssistant
    RoleTool
)

type Message struct {
    MessageID     string
    Role          Role
    ContentBlocks []ContextBlock
    CreatedAt     time.Time
}

type BlockKind uint8
const (
    BlockText BlockKind = iota + 1
    BlockJSON
    BlockImage
    BlockAudio
    BlockDocument
    BlockToolObservation
)

type TrustClass uint8
const (
    TrustSystem TrustClass = iota + 1
    TrustUser
    TrustToolTrusted
    TrustUntrustedExternal
)

type Sensitivity uint8
const (
    SensitivityPublic Sensitivity = iota + 1
    SensitivityInternal
    SensitivityConfidential
    SensitivitySecret
)

type LineageRelation uint8
const (
    LineageCopiedFrom LineageRelation = iota + 1
    LineageDerivedFrom
    LineageSummarizedFrom
    LineageRetrievedFrom
)

type LineageRef struct {
    BlockID     BlockID
    ContentHash Digest
    Relation    LineageRelation
}

type ProducerRef struct {
    ActorType   ActorType
    ActorID     ActorID
    PrincipalID PrincipalID
    EventID     EventID
}

type InlineContent struct {
    Encoding string // utf-8 | canonical-json; zatvorena validacija po BlockKind
    Bytes    []byte
}

type ContextBlock struct {
    BlockID      BlockID
    Kind         BlockKind
    Inline       Optional[InlineContent]
    Ref          Optional[ContentRef]
    ContentHash  Digest
    SourceURI    string
    Producer     ProducerRef
    Trust        TrustClass
    Sensitivity  Sensitivity
    Lineage      []LineageRef
    ObservedAt   time.Time
    ExpiresAt    Optional[time.Time]
}

type EffectClass uint8
const (
    EffectReadOnly EffectClass = iota + 1
    EffectReversible
    EffectIrreversible
)

type ToolCall struct {
    ToolCallID         ToolCallID
    ToolID             string
    Arguments          CanonicalJSON
    ArgumentsSchemaHash Digest
    Effect             EffectClass
    Deadline           time.Time
    AttemptNo          uint32
    IdempotencyKey     Optional[IdempotencyKey]
}

type ToolResultStatus uint8
const (
    ToolSucceeded ToolResultStatus = iota + 1
    ToolFailed
    ToolCancelled
    ToolUnknown
)

type ToolResult struct {
    ToolCallID   ToolCallID
    AttemptNo    uint32
    Status       ToolResultStatus
    OutputBlocks []ContextBlock
    Error        Optional[TypedError]
    StartedAt    time.Time
    FinishedAt   time.Time
}

type ErrorCategory uint8
const (
    ErrorValidation ErrorCategory = iota + 1
    ErrorAuthN
    ErrorAuthZ
    ErrorPolicy
    ErrorResource
    ErrorTimeout
    ErrorCancelled
    ErrorDependency
    ErrorConflict
    ErrorInternal
    ErrorUnknownEffect
)

type Retryability uint8
const (
    RetryNever Retryability = iota + 1
    RetryByS7Policy
    RetryAfter
)

type TypedError struct {
    Code         string
    Category     ErrorCategory
    Retryability Retryability
    SafeMessage  string
    RetryAfter   Optional[time.Time]
    Origin       string
    CauseEventID Optional[EventID]
}
```

## API

```go
func NewEnvelope(in EnvelopeInput) (Envelope, error)
func DecodeEnvelope(raw []byte, reg SchemaLookup) (Envelope, error)
func (e Envelope) Validate(reg SchemaLookup) error

func NewInlineBlock(in InlineBlockInput) (ContextBlock, error)
func NewReferencedBlock(in ReferencedBlockInput) (ContextBlock, error)
func DeriveBlock(kind BlockKind, content InlineContent, sources []ContextBlock,
    producer ProducerRef, now time.Time) (ContextBlock, error)
func (b ContextBlock) Validate() error

func NewToolCall(in ToolCallInput) (ToolCall, error)
func CorrelateResult(call ToolCall, result ToolResult) error
```

## Invarijante

- `Envelope.Payload.Hash == SHA256(Payload.Bytes)`; `Schema/EventType` registry-validni.
- `Sequence > 0`; sequence strogo raste po `RunID`; `ParentEventID != EventID`.
- `ContextBlock`: `Inline.Valid XOR Ref.Valid`; hash odgovara inline bajtovima ili ref digestu.
- `DeriveBlock`: `Trust = maxRestrictiveness(sources.Trust)`;
  `Sensitivity = max(sources.Sensitivity)`; lineage sadrži svaki source `(BlockID,ContentHash)`.
- `TrustSystem` se ne smije izvesti iz nesistemskog sourcea; empty lineage je legalan samo za source block.
- `Effect != EffectReadOnly` zahtijeva non-empty `IdempotencyKey`.
- `ToolResult.ToolCallID/AttemptNo` moraju odgovarati pozivu; `ToolFailed` zahtijeva `Error`;
  `ToolSucceeded` zabranjuje `Error`; `ToolUnknown` zahtijeva `ErrorUnknownEffect`.
- `TypedError.SafeMessage`: bez tajne/raw payloada/stacka; samo `Code` upravlja politikom.
- Enumi: `Valid()` + exhaustive `String()`; vrijednost `0` i unknown discriminator fail-closed.

## RED

- `TestS01RejectsProvenanceLaundering`: untrusted source → derived SYSTEM/bez lineagea;
  `ErrProvenanceDowngrade`, nula journal/model sink poziva.
- `TestS01ContextBlockRequiresExactlyOneContentForm`: inline+ref i neither → `ErrContentUnion`.
- `TestS01RejectsPayloadHashMismatch`: mutiran payload uz stari hash → decode reject.
- `TestS01StateChangingToolRequiresIdempotencyKey`: reversible/irreversible bez keya → reject.
- `TestS01ToolResultMustMatchCallAndAttempt`: isti call ID, drugi attempt → `ErrCorrelation`.
- `TestS01UnknownEnumFailsClosed`: svaki enum s `255` → validation reject; nula dispatcha.

---

# S0.2 — event-derived state machine

## Tipovi

```go
package machine

type RunState uint8
const (
    RunCreated RunState = iota + 1
    RunAdmitted
    RunRunning
    RunSucceeded
    RunFailed
    RunCancelled
    RunUnknown
    RunManualRecovery
)

type TurnState uint8
const (
    TurnCreated TurnState = iota + 1
    TurnRunning
    TurnSucceeded
    TurnFailed
    TurnCancelled
)

type AttemptState uint8
const (
    AttemptPlanned AttemptState = iota + 1
    AttemptAuthorized
    AttemptRunning
    AttemptSucceeded
    AttemptFailed
    AttemptCancelled
    AttemptUnknown
    AttemptManualRecovery
)

type EventKind uint16
const (
    EventRunAdmitted EventKind = iota + 1
    EventRunStarted
    EventRunSucceeded
    EventRunFailed
    EventRunCancelled
    EventRunEffectUnknown
    EventRunReconciledSucceeded
    EventRunReconciledFailed
    EventRunManualRecovery
    EventTurnStarted
    EventTurnSucceeded
    EventTurnFailed
    EventTurnCancelled
    EventAttemptAuthorized
    EventAttemptStarted
    EventAttemptSucceeded
    EventAttemptFailed
    EventAttemptCancelled
    EventAttemptEffectUnknown
    EventAttemptReconciledSucceeded
    EventAttemptReconciledFailed
    EventAttemptManualRecovery
)

type GuardID uint8
const (
    GuardNone GuardID = iota
    GuardAdmissionValid
    GuardCurrentAttempt
    GuardReconciliationEvidence
)

type Transition[S comparable] struct {
    From  S
    Event EventKind
    To    S
    Guard GuardID
}

type Snapshot struct {
    Run          RunState
    Turns        map[contracts.TurnID]TurnState
    Attempts     map[AttemptKey]AttemptState
    LastSequence uint64
    LastEventID  contracts.EventID
    Offset       uint64
}

type GuardContext struct {
    Snapshot Snapshot
    Event    contracts.Envelope
    Payload  contracts.CanonicalJSON
}

type Guard func(GuardContext) error

type Machine struct {
    runTable     map[runKey]Transition[RunState]
    turnTable    map[turnKey]Transition[TurnState]
    attemptTable map[attemptKey]Transition[AttemptState]
    guards       map[GuardID]Guard
}
```

## API

```go
func NewCanonicalMachine() *Machine
func (m *Machine) Apply(prior Snapshot, ev contracts.Envelope, offset uint64) (Snapshot, error)
func (m *Machine) Fold(events []JournalRecord) (Snapshot, error)
```

## Invarijante

- Tablice su compile-time konstruirane, validirane na duplicate `(from,event)` i nakon toga immutable.
- Nema implicitne self-transition; nepostojeći ključ → `ErrIllegalTransition`.
- `Apply` je čist: ne radi I/O i ne mutira `prior`; journal append prethodi projection updateu.
- `ev.Sequence == prior.LastSequence+1`; replay istog `EventID` dopušten samo s identičnim hashom.
- Terminalni `SUCCEEDED|FAILED|CANCELLED` nemaju izlaz.
- `UNKNOWN` izlazi samo reconciliation eventom u `SUCCEEDED|FAILED|MANUAL_RECOVERY`.
- Run ne može `SUCCEEDED` dok aktivni turn/attempt nije terminalan i acceptance gate nije u eventu.

## RED

- `TestS02RejectsRunningToAdmitted`: `RunRunning + EventRunAdmitted` → reject; snapshot byte-identičan.
- `TestS02RejectsSucceededToRunning`: terminalni run + start event → reject.
- `TestS02UnknownRequiresReconciliationEvidence`: unknown + ordinary success → reject; reconciliation
  bez evidence refa → reject; valjan reconciliation → success.
- `TestS02RejectsSequenceGapAndFork`: sequence `N+2` ili isti sequence/drugi event hash → reject.
- `TestS02FoldIsDeterministic`: isti canonical event niz → isti snapshot hash/offset.
- `TestS02GuardFailureDoesNotAdvanceProjection`: guard reject → offset/sequence/stanje nepromijenjeni.

---

# S0.3 — capability floor + P0.5

## Tipovi

```go
package negotiation

type Support uint8
const (
    SupportUnknown Support = iota
    SupportNo
    SupportYes
)

type CapabilityVector struct {
    ToolCalling      Support
    StructuredOutput Support
    Streaming        Support
    ContextTokens    uint64 // 0 = unknown, nikad "unlimited"
}

type CapabilityFloor struct {
    ToolCalling      bool
    StructuredOutput bool
    Streaming        bool
    MinContextTokens uint64
}

type Measurement struct {
    TargetID       string
    TargetVersion  string
    ConfigHash     contracts.Digest
    Vector         CapabilityVector
    ProbeID        string
    ProbeRevision  string
    Evidence       contracts.EvidenceRef
    MeasuredAt     time.Time
    ExpiresAt      time.Time
    MeasurementHash contracts.Digest
}

type Declaration struct {
    TargetID      string
    TargetVersion string
    ConfigHash    contracts.Digest
    Vector        CapabilityVector
    DescriptorHash contracts.Digest
}

type CapabilityMatrix struct {
    Declaration Declaration
    Measurement Measurement
}

type CapabilityGrant struct {
    TargetID       string
    TargetVersion  string
    ConfigHash     contracts.Digest
    Effective      CapabilityVector
    FloorHash      contracts.Digest
    MatrixHash     contracts.Digest
    IssuedAt       time.Time
    ExpiresAt      time.Time
}

type Resolver interface {
    Resolve(now time.Time, floor CapabilityFloor, matrix CapabilityMatrix) (CapabilityGrant, error)
}
```

## P0.5 — capability-matrix fail-closed

**Obvezna polja**

```text
floor: required booleans + min_context_tokens
declaration: target_id + target_version + config_hash + declared vector + descriptor_hash
measurement: isti target/version/config + measured vector + probe ID/revision + evidence + time/expiry + hash
grant: effective vector + floor/matrix hash + expiry
```

**Invarijante**

- `Effective.Support = YES` samo ako `Declared==YES && Measured==YES`; svaki `UNKNOWN/NO` → `NO`.
- `Effective.ContextTokens = min(nonzero declared, nonzero measured)`; bilo koji `0` → unknown/fail.
- Floor se evaluira nad `Effective`, nikad samo nad declarationom.
- Target ID/version/config moraju biti identični; measurement mora biti neistekao i hash-validan.
- Neuspjeh vraća typed error i nula route/provider/tool poziva; nema optimistic fallbacka.
- Grant pinna matrix/floor/config hash; adapter prije fizičkog pokušaja mora predočiti neistekli grant.
- Promjena target verzije/configa/probe revizije invalidira grant i traži novo mjerenje.

## RED tijelo

```go
func TestP05CapabilityMatrixFailsClosed(t *testing.T) {
    cases := []struct {
        name   string
        mutate func(*CapabilityMatrix, *CapabilityFloor)
        code   string
    }{
        {"declared_yes_measured_unknown", measuredToolUnknown, "CAPABILITY_UNMEASURED"},
        {"declared_yes_measured_no", measuredToolNo, "CAPABILITY_BELOW_FLOOR"},
        {"context_unknown_zero", measuredContextZero, "CONTEXT_LIMIT_UNKNOWN"},
        {"context_below_floor", measuredContextBelowFloor, "CAPABILITY_BELOW_FLOOR"},
        {"measurement_expired", expireMeasurement, "CAPABILITY_MEASUREMENT_EXPIRED"},
        {"target_version_mismatch", changeMeasuredVersion, "CAPABILITY_MATRIX_MISMATCH"},
        {"config_hash_mismatch", changeMeasuredConfig, "CAPABILITY_MATRIX_MISMATCH"},
        {"measurement_hash_tampered", corruptMeasurementHash, "CAPABILITY_EVIDENCE_INVALID"},
    }
    for _, tc := range cases {
        // Arrange valid floor+matrix; mutation introduces exactly one defect.
        // Act Resolve, then attempt Route/Provider.Call with returned grant.
        // Assert exact tc.code; no grant; routeCounter==0; transportCounter==0;
        // journal contains one redacted capability_denied event; active target unchanged.
    }
}
```

## Dodatni RED

- `TestS03GrantBoundToConfigAndTarget`: valjan grant + drugi target/config → pre-transport reject.
- `TestS03EffectiveUsesIntersection`: declaration NO, measurement YES → effective NO.
- `TestS03GrantExpiresBeforeAttempt`: grant istekne između routea i transporta → nula transporta.
- `TestS03UnknownNeverMeansUnlimited`: context `0` ne prolazi floor `1`.

---

# S0.4 — schema registry, upcast, quarantine

## Tipovi

```go
package schema

type Validator interface {
    Validate(raw contracts.CanonicalJSON) error
}

type Descriptor struct {
    Ref      contracts.SchemaRef
    Current  bool
    Validator Validator
}

type RawRecord struct {
    EnvelopeBytes []byte
    Payload       contracts.CanonicalJSON
    InputHash     contracts.Digest
}

type Upcaster interface {
    ID() string
    From() contracts.SchemaRef
    To() contracts.SchemaRef // ista ID; To.Version == From.Version+1
    Upcast(context.Context, contracts.CanonicalJSON) (contracts.CanonicalJSON, error)
}

type MigrationRecord struct {
    SchemaID   contracts.SchemaID
    From       uint32
    To         uint32
    MigrationID string
    InputHash  contracts.Digest
    OutputHash contracts.Digest
}

type QuarantineRecord struct {
    Raw         RawRecord
    ReasonCode  string
    ObservedAt  time.Time
}

type QuarantineSink interface {
    Put(context.Context, QuarantineRecord) error
}

type Registry interface {
    Lookup(contracts.SchemaRef) (Descriptor, bool)
    Current(contracts.SchemaID) (Descriptor, bool)
    Upcast(context.Context, RawRecord) (contracts.CanonicalJSON, []MigrationRecord, error)
    Freeze() error
}
```

## Invarijante

- Registracija samo prije `Freeze`; unique schema ref i unique adjacent upcaster edge.
- Upcast lanac strogo `v→v+1`; nema downcasta, skoka, ciklusa ni promjene `SchemaID`.
- Prije svakog koraka validira se input schema; poslije koraka output schema/hash.
- Raw event bytes ostaju immutable; upcast daje novu projekciju + `MigrationRecord` po koraku.
- Event/run/turn/tool/parent ID, lineage, trust i sensitivity: očuvani ili restriktivniji.
- Unknown polja: byte-semantika očuvana kroz reserved extension mapu; unknown discriminator → quarantine.
- Unknown schema/version, broken chain ili invalid output → quarantine; nikakav consumer poziv.

## RED

- `TestS04UnknownSchemaGoesToQuarantine`: unknown ID/version → jedan quarantine zapis, nula consumera.
- `TestS04RejectsLossyDowncast`: current v3 → request v2 → `ErrDowncastForbidden`.
- `TestS04UpcastPreservesIdentityAndLineage`: mutirajući upcaster ukloni parent/lineage → reject.
- `TestS04BrokenAdjacentChainFailsClosed`: v1→v3 bez v2 edgea → quarantine.
- `TestS04RawRecordNeverMutated`: poslije uspješnog lanca original bytes/hash identični.
- `TestS04RegistryImmutableAfterFreeze`: late register → `ErrRegistryFrozen`.

---

# S0.5 — closure resolver + realna aktivacija

## Tipovi

```go
package closure

type Version string

type VersionConstraint struct {
    MinInclusive Version
    MaxExclusive Version
}

type CapabilityRef struct {
    ID      contracts.CapabilityID
    Version VersionConstraint
}

type GateRef struct {
    ID      contracts.GateID
    Version VersionConstraint
}

type AdapterRef struct {
    ID      contracts.AdapterID
    Version VersionConstraint
}

type ManifestSignature struct {
    SignerID  string
    Algorithm string
    Signature []byte
}

type CapabilityManifest struct {
    SchemaVersion uint32
    FlagID        contracts.FlagID
    FlagVersion   Version
    Provides      []CapabilityRef
    Requires      []CapabilityRef
    Conflicts     []CapabilityRef
    Gates         []GateRef
    Adapters      []AdapterRef
    TriggerID     string
    TriggerVersion Version
    ConfigHash    contracts.Digest
    Signature     ManifestSignature
}

type ActivationState uint8
const (
    ActivationDeclared ActivationState = iota + 1
    ActivationResolved
    ActivationValidated
    ActivationPreparing
    ActivationReadyToCommit
    ActivationActive
    ActivationRejected
    ActivationFailed
    ActivationRollingBack
    ActivationRolledBack
    ActivationRollbackFailed
)

type ActivationStep struct {
    Order       uint32
    FlagID      contracts.FlagID
    Gate        GateRef
    Adapter     contracts.Optional[AdapterRef]
    Required    []contracts.CapabilityID
}

type ActivationPlan struct {
    PlanID          string
    BaseGeneration  uint64
    BaseActiveHash  contracts.Digest
    ManifestSetHash contracts.Digest
    ConfigHash      contracts.Digest
    PolicyHash      contracts.Digest
    OrderedFlags    []contracts.FlagID
    Steps           []ActivationStep
    RollbackOrder   []uint32
    CreatedAt       time.Time
    ExpiresAt       time.Time
    PlanHash        contracts.Digest
}

type GateStatus uint8
const (
    GatePassed GateStatus = iota + 1
    GateFailed
)

type GateAttestation struct {
    PlanHash          contracts.Digest
    GateID            contracts.GateID
    GateVersion       Version
    ImplementationID  string
    ImplementationHash contracts.Digest
    ConfigHash        contracts.Digest
    PolicyHash        contracts.Digest
    Status            GateStatus
    PreparedDigest    contracts.Digest
    VerifiedAt        time.Time
    ExpiresAt         time.Time
    Evidence          contracts.EvidenceRef
}

type RollbackToken struct {
    Ref      string
    PlanHash contracts.Digest
    GateID   contracts.GateID
    Opaque   []byte // nikad journal/prompt; scoped Activatoru
}

type PrepareState uint8
const (
    PrepareAbsent PrepareState = iota + 1
    PrepareApplied
    PrepareRolledBack
    PrepareUnknown
)

type PrepareRecovery struct {
    State       PrepareState
    Attestation contracts.Optional[GateAttestation]
    Token       contracts.Optional[RollbackToken]
}

type ActiveSet struct {
    Generation uint64
    PlanHash   contracts.Digest
    Flags      []contracts.FlagID
    SetHash    contracts.Digest
}

type Resolver interface {
    Resolve(manifests []CapabilityManifest, requested []contracts.FlagID,
        base ActiveSet, configHash, policyHash contracts.Digest, now time.Time) (ActivationPlan, error)
}

type Gate interface {
    Ref() GateRef
    Prepare(context.Context, ActivationPlan, ActivationStep) (GateAttestation, RollbackToken, error)
    ReconcilePrepare(context.Context, ActivationPlan, ActivationStep) (PrepareRecovery, error)
    Rollback(context.Context, RollbackToken) error
}

type RollbackVault interface {
    Put(context.Context, RollbackToken) (ref string, digest contracts.Digest, err error)
    Get(context.Context, string, contracts.Digest) (RollbackToken, error)
    Delete(context.Context, string) error
}

type Journal interface {
    CommitActivation(context.Context, ActivationPlan, []GateAttestation,
        expectedBaseGeneration uint64) (ActiveSet, error)
}

type Activator interface {
    Activate(context.Context, ActivationPlan) (ActiveSet, error)
    Recover(context.Context, string) error
}
```

## Resolve/Activate algoritam

```text
Resolve (čist, bez I/O):
  canonicalize+verify manifests
  expand transitive requires
  reject unknown version / missing provider / cycle / conflict / bundle alias
  topological sort with deterministic FlagID tie-break
  emit immutable ActivationPlan; hash svih polja osim PlanHash

Activate (jedini side-effect owner):
  CAS state VALIDATED -> PREPARING
  for step in plan order:
    durable append PREPARE_INTENT(plan_hash, step, deterministic_prepare_key)
    Gate.Prepare; idempotentno po (plan_hash, step); samo reverzibilne, neizložene pripreme
    verify attestation PlanHash+ConfigHash+PolicyHash+implementation+expiry
    seal rollback token u RollbackVault
    durable append PREPARED(attestation, rollback_token_ref+digest)
  CAS PREPARING -> READY_TO_COMMIT
  Journal.CommitActivation(plan, attestations, BaseGeneration)  # jedan durable CAS/append
  atomic.Pointer[ActiveSet].Store(newSet)                        # infallible visibility swap
  expose prepared adapters only through new generation
  state -> ACTIVE

Prepare failure / expired attestation / journal CAS conflict:
  state -> FAILED -> ROLLING_BACK
  rollback prepared steps in reverse order
  state -> ROLLED_BACK | ROLLBACK_FAILED
  previous ActiveSet stays visible

Crash recovery:
  journal ACTIVE event wins; rebuild pointer from fold before admission
  PREPARE_INTENT bez PREPARED -> Gate.ReconcilePrepare(plan, step)
    APPLIED -> seal token + PREPARED -> ROLLING_BACK
    ABSENT/ROLLED_BACK -> nastavi rollback prethodnih koraka
    UNKNOWN -> ROLLBACK_FAILED + quarantine
  PREPARING/READY without ACTIVE -> ROLLING_BACK
  ROLLBACK_FAILED -> quarantine resources; no new activation over same gates
```

## Invarijante

- Resolver nikad ne pokreće gate/adapter/OS/network I/O.
- Plan je immutable; svaka izmjena daje drugi `PlanHash`.
- Svaki attestation veže isti plan/config/policy hash i nije istekao pri commitu.
- `Prepare` ne smije objaviti capability niti napraviti ireverzibilan efekt; takav gate je invalidan.
- `Prepare` mora biti idempotentan po `(PlanHash,Step)` i imati dokaziv `ReconcilePrepare`; bez toga
  manifest se odbija prije PREPARING.
- Prije svakog external Preparea durable je `PREPARE_INTENT`; rollback token je sealed durable state,
  journal sadrži samo ref+digest.
- Samo jedan journal CAS objavljuje cijeli `ActiveSet`; nema per-flag ACTIVE eventa.
- Base generation mismatch → cijeli plan stale; rollback svih priprema.
- Adapter može biti pripremljen/quiescent prije commita, ali nije discoverable/callable prije ACTIVE.
- `ROLLBACK_FAILED` fail-closed blokira capability i zahtijeva manual recovery.
- Bundle alias nije `FlagID`; trigger ne aktivira flag bez closurea.

## RED

- `TestS05EveryFlagFailsWhenOneGateIsAblated`: tablično svaki flag bez jednog gatea →
  `ErrIncompleteClosure`; prethodni ActiveSet identičan; nula Prepare poziva.
- `TestS05RejectsCycleConflictAndUnknownVersion`: svaka greška → REJECTED; nula side effecta.
- `TestS05AttestationsMustBindSamePlanHash`: jedan gate attestira drugi hash → FAILED→ROLLING_BACK;
  rollback svih prethodnih priprema; nula journal activation commita.
- `TestS05PrepareFailureCannotPartiallyActivate`: gate N fail; flagovi 1..N-1 nisu discoverable;
  reverse rollback; previous generation ostaje.
- `TestS05CrashBetweenExternalPrepareAndReceiptReconciles`: pad nakon external preparea, prije PREPARED;
  restart kroz deterministic prepare key otkrije efekt, spremi token i rollbacka; capability nikad ACTIVE.
- `TestS05UnreconcilablePrepareIsRejectedBeforeExecution`: gate bez `ReconcilePrepare` dokaza →
  validation reject; nula Prepare poziva.
- `TestS05ConcurrentPlansHaveSingleGenerationWinner`: dva plana nad base generation 7;
  jedan commitira generation 8, drugi CAS fail+rollback; nema uniona setova.
- `TestS05CrashBeforeCommitRollsBackOnRecovery`: durable PREPARING bez ACTIVE → rollback, old set active.
- `TestS05CrashAfterCommitRebuildsWholeSet`: ACTIVE event durable, pointer nije swap-an → restart fold
  objavi cijelu novu generaciju prije admissiona.
- `TestS05RollbackFailureQuarantinesGate`: rollback greška → ROLLBACK_FAILED; gate se ne može ponovno
  koristiti niti capability oglasiti.

---

# S6.2 — sandbox contract

## Capability matrica

```go
package sandbox

type Capability uint8
const (
    CapabilityFSReadScope Capability = iota + 1 // FS_RO
    CapabilityFSWriteScope                       // FS_RW; prazan allowlist = deny all writes
    CapabilityNetworkDeny                        // NET_DENY
    CapabilitySyscallFilter                      // SYSCALL
    CapabilityProcessTree                        // PROC_TREE
)

type Enforcement uint8
const (
    EnforcementUnavailable Enforcement = iota
    EnforcementUnverified
    EnforcementEnforced
)

type CapabilityState struct {
    Capability Capability
    Enforcement Enforcement
    Evidence   contracts.EvidenceRef
    Limitation string
}

type CapabilityMatrix struct {
    BackendID      string
    BackendVersion string
    GOOS           string
    GOARCH         string
    OSVersion      string
    KernelVersion  string
    States         []CapabilityState
    ProbedAt       time.Time
    ExpiresAt      time.Time
    ReportHash     contracts.Digest
}
```

| Backend | FS_RO | FS_RW | NET_DENY | SYSCALL | PROC_TREE | v1 status |
|---|---:|---:|---:|---:|---:|---|
| Linux Landlock+seccomp+process-group/pidfd | ENFORCED nakon live probea | ENFORCED nakon live probea | ENFORCED kroz seccomp socket policy | ENFORCED per-arch | ENFORCED | kandidat |
| Windows | UNAVAILABLE | UNAVAILABLE | UNAVAILABLE | UNAVAILABLE | UNVERIFIED/Job Object | high-risk BLOKIRAN |
| macOS | UNAVAILABLE | UNAVAILABLE | UNAVAILABLE | UNAVAILABLE | UNVERIFIED/process-group | high-risk BLOKIRAN |

## Tipovi i sučelja

```go
type RiskClass uint8
const (
    RiskLow RiskClass = iota + 1
    RiskHigh
)

type PathAccess uint8
const (
    PathRead PathAccess = 1 << iota
    PathWrite
    PathExecute
    PathCreate
    PathRemove
)

type PathGrant struct {
    CanonicalPath string
    Access        PathAccess
    Device        uint64
    Inode         uint64
}

type NetworkPolicy struct {
    DenyIPv4       bool
    DenyIPv6       bool
    DenyRawSockets bool
    AllowUnixFDs   []int
}

type SyscallProfileID string

type ExecSpec struct {
    ExecutablePath string
    ExecutableHash contracts.Digest
    Argv           []string
    WorkingDir     string
    Env            []string // kompletan allowlisted env; nema implicitnog inheritancea
    Stdin          contracts.Optional[contracts.ContentRef]
}

type Spec struct {
    SpecID          string
    Risk            RiskClass
    Required        []Capability
    Paths           []PathGrant
    Network         NetworkPolicy
    Syscalls        SyscallProfileID
    ProcessLimit    uint32
    Exec            ExecSpec
    PolicyHash      contracts.Digest
    Deadline        time.Time
}

type ProbeReport struct {
    Matrix      CapabilityMatrix
    BackendHash contracts.Digest
    Evidence    []contracts.EvidenceRef
}

type CompiledPathRule struct {
    CanonicalPath string
    Access        PathAccess
    Device        uint64
    Inode         uint64
}

type BPFInstruction struct {
    Code uint16
    JT   uint8
    JF   uint8
    K    uint32
}

type CompiledPolicy struct {
    BackendID       string
    BackendHash     contracts.Digest
    ProbeHash       contracts.Digest
    SpecHash        contracts.Digest
    Required        []Capability
    Paths           []CompiledPathRule
    LandlockABI     uint32
    SeccompArch     uint32
    SeccompProgram  []BPFInstruction
    ExecutableHash  contracts.Digest
    PolicyHash      contracts.Digest
    ExpiresAt       time.Time
}

type PendingProcess struct {
    PID          int
    StartToken   string
    PIDFD        int
    Control      *os.File
    Nonce        [32]byte
    PolicyHash   contracts.Digest
}

type Attestation struct {
    BackendID       string
    BackendHash     contracts.Digest
    SpecHash        contracts.Digest
    PolicyHash      contracts.Digest
    ProbeHash       contracts.Digest
    PID             int
    StartToken      string
    HelperHash      contracts.Digest
    Enforced        []Capability
    LandlockABI     uint32
    SeccompArch     uint32
    Nonce           [32]byte
    AttestedAt      time.Time
    AttestationHash contracts.Digest
}

type SandboxedProcess struct {
    PID         int
    StartToken  string
    PIDFD       int
    Attestation Attestation
    Stdout      io.ReadCloser
    Stderr      io.ReadCloser
}

type SandboxBackend interface {
    ID() string
    Probe(context.Context) (ProbeReport, error)
    Compile(context.Context, Spec, ProbeReport) (CompiledPolicy, error)
    Launch(context.Context, CompiledPolicy) (PendingProcess, error)
    Attest(context.Context, PendingProcess, CompiledPolicy) (Attestation, error)
}

type Supervisor interface {
    Start(context.Context, SandboxBackend, Spec) (SandboxedProcess, error)
    Terminate(context.Context, SandboxedProcess, time.Duration) error
}
```

## Zajedničke invarijante

- `Probe` je read-only; `Compile` je čist; `Launch` pokreće samo trusted helper na pre-exec barijeri.
- Target executable ne smije biti mapiran/izvršen prije uspješnog `Attest`.
- `Compile` zahtijeva `ENFORCED` za svaki `Spec.Required`; `UNVERIFIED` nije dovoljno.
- `RiskHigh` minimalno zahtijeva svih pet capabilityja; caller to ne može suziti configom.
- Backend selection je eksplicitan; nema automatskog prelaska na slabiji backend.
- Policy/probe/backend/helper/executable hashovi moraju se poklapati u attestationu.
- `Attest` failure: helper se ubija prije EXEC signala; target sink counter ostaje 0.
- Svi naslijeđeni FD-i zatvoreni osim numeriranih stdio/control/policy FD-ova; control FD close-on-exec.
- Timeout/cancel/kill koristi PID start-token + pidfd/process-tree, nikad samo reciklabilni PID.

## Supervisor.Start

```text
report  := backend.Probe()
policy  := backend.Compile(spec, report)
pending := backend.Launch(policy)      # trusted helper; target još nije execan
att     := backend.Attest(pending, policy)
verify attestation + required caps
send fixed one-byte EXEC over inherited control FD
wait helper exec-success/error record
return SandboxedProcess
on any error: close EXEC path, terminate verified helper identity, wait, return typed error
```

---

# Linux backend (`CGO_ENABLED=0`)

## Procesni oblik

```text
nexus supervisor
  └─ exec current nexus binary: __sandbox_helper
       inherited: policy FD, control socketpair, stdio, executable O_PATH FD
       target bytes nisu učitani
       LockOSThread; bez user plugina/goroutinea
       ABI probe
       PR_SET_NO_NEW_PRIVS
       Landlock create/add/restrict
       seccomp(SECCOMP_SET_MODE_FILTER) na calling locked threadu
       write fixed-size attestation; wait EXEC byte
       close policy/control/foreign FDs
       execveat(executable_fd, AT_EMPTY_PATH, argv, allowlisted_env)
```

## Helper redoslijed — normativno

```go
func linuxHelper(policyFD, controlFD, executableFD int) error {
    runtime.LockOSThread()
    requireSingleHelperMode()
    p := readAndHashBoundedPolicy(policyFD)
    verifyParentPeerAndNonce(controlFD, p.Nonce)
    closeAllFDsExcept(policyFD, controlFD, executableFD, 0, 1, 2)

    abi := probeLandlockABI()                         // 1
    requireABIAndRights(abi, p)
    requireSeccompArch(runtime.GOARCH, p.SeccompArch)
    verifyExecutableFD(executableFD, p.ExecutableHash)
    verifyPathIdentities(p.Paths)

    prctlNoNewPrivileges()                            // 2
    ruleset := createLandlockRuleset(abi, p.Paths)   // 3
    addLandlockRules(ruleset, p.Paths)
    restrictSelf(ruleset)
    close(ruleset)

    installSeccompBPF(p.SeccompProgram)              // 4
    attestAppliedPolicy(controlFD, p, abi)
    requireExecBarrierByte(controlFD)
    close(controlFD)
    return execveatFD(executableFD, p.Argv, p.Env)    // 5; nema povratka
}
```

## Linux invarijante

- Helper se pokreće iz verificiranog current-executable digesta; skriveni mode nije dostupan kroz
  obični CLI bez inherited nonce+peer bindinga.
- Path grant se ponovno otvara s `openat2` (`RESOLVE_BENEATH|NO_MAGICLINKS|NO_SYMLINKS`) i provjerava
  `(device,inode)` neposredno prije Landlock rulea.
- Target se izvršava kroz prethodno otvoreni `O_PATH` FD + `execveat(AT_EMPTY_PATH)`; path swap ne mijenja cilj.
- Landlock i seccomp primjenjuju se na locked calling thread; `execveat` uklanja trusted Go runtime
  sibling threadove, a jedini preživjeli target thread nasljeđuje oba ograničenja. Nema untrusted koda
  na sibling threadu prije execa.
- Landlock handled-rights su ABI-intersection, ali nedostajući required right → UNAVAILABLE, ne ignore.
- Seccomp program je checked-in, generiran po `GOARCH`, hashiran i unit-verified za jump bounds/default-deny.
- `NET_DENY`: zatvori inherited sockete; BPF blokira AF_INET/AF_INET6/raw socket stvaranje i relevantne
  connect/send syscalle; Unix FD allowlist eksplicitna.
- `PROC_TREE`: novi process group; seccomp blokira escape (`setsid`, nedopušten `setpgid`, namespace/
  ptrace primitive); supervisor koristi pidfd gdje kernel podržava i čeka cijelo owned stablo.
- Target ne dobiva policy/control FD, host env, secret env, cwd izvan grantova ni writable temp izvan policyja.
- Kernel/ABI/arch mismatch između Probe i helper re-probea → helper ne šalje attestation/EXEC.

## Linux RED

- `TestLinuxSandboxFirstInstructionCannotWriteOutsideGrant`: targetova prva instrukcija piše sentinel
  izvan RW granta; nema datoteke, EPERM, dokaz nula unsandboxed target instrukcija.
- `TestLinuxSandboxAllowsDeclaredReadAndWrite`: RO read uspije; RW write uspije; RO write odbijen.
- `TestLinuxSandboxDeniesIPv4IPv6DNSAndLoopback`: svi mrežni pokušaji odbijeni uz NET_DENY.
- `TestLinuxSandboxBlocksProfileSyscall`: fixture pozove profile-denied syscall; proces dobiva ugovoreni
  errno/kill, supervisor ostaje živ.
- `TestLinuxSandboxKillsEscapingDescendants`: child/grandchild pokušaju `setsid/setpgid`; escape odbijen;
  Terminate ukloni cijelo dokazano stablo.
- `TestLinuxSandboxRejectsExecutableSwap`: path zamijenjen nakon Compile; izvrši se pinned FD ili launch reject,
  nikad zamjenski binary.
- `TestLinuxSandboxClosesUnexpectedInheritedFDs`: target enumerira FD-e; vidi samo ugovorene.
- `TestLinuxSandboxProbeHelperTOCTOUFailsClosed`: probe hash/ABI mutiran prije helpera → nula target execa.
- `TestLinuxSandboxAttestationMismatchNeverReleasesBarrier`: wrong spec/helper/policy hash → helper killed.
- `TestLinuxSandboxUnsupportedKernelBlocksHighRiskExec`: Landlock/seccomp unsupported →
  `SANDBOX_CAPABILITY_UNAVAILABLE`; target counter 0; nema bwrap fallbacka.

---

# Windows backend — v1 fail-closed

```go
//go:build windows

type WindowsBackend struct{}

func (WindowsBackend) Probe(context.Context) (ProbeReport, error) {
    return reportAllUnavailable("no promoted AppContainer backend"), nil
}
func (WindowsBackend) Compile(context.Context, Spec, ProbeReport) (CompiledPolicy, error) {
    return CompiledPolicy{}, ErrSandboxCapabilityUnavailable
}
func (WindowsBackend) Launch(context.Context, CompiledPolicy) (PendingProcess, error) {
    return PendingProcess{}, ErrSandboxCapabilityUnavailable
}
func (WindowsBackend) Attest(context.Context, PendingProcess, CompiledPolicy) (Attestation, error) {
    return Attestation{}, ErrSandboxCapabilityUnavailable
}
```

## Stvarni Windows hardver/VM promotion-probe

```text
CURRENT PROOF: NOT RUN — nema pristupa Windows hardveru u ovoj fazi.
Matrica: Windows 11 podržane release grane × amd64/arm64 × admin/non-admin × CLI/desktop packaging.
Build: CGO_ENABLED=0; release-signed binary; clean user profile; network online+offline.

W0 baseline: trenutni backend vraća UNAVAILABLE; high-risk fixture target nikad nije kreiran.
W1 API/support: AppContainer/restricted-token API availability + OS build; experimental API sam nije floor.
W2 token: uklonjeni privilegiji/SID-ovi, low integrity/AppContainer SID, token ne može otvoriti host secret.
W3 filesystem: RW samo granted dir; deny user profile/system/temp/UNC; junction/reparse/symlink escape deny.
W4 network: deny DNS, IPv4, IPv6, loopback, private/link-local; proxy env ne otvara izlaz.
W5 process: create suspended -> assign containment/job -> resume; fixture spawn prije assignment mora biti 0.
W6 tree: child/grandchild/breakaway flag/COM/WMI spawn pokušaji; svi ostaju contained ili su denied.
W7 handles: inherited handle allowlist; target ne vidi token, control, file ni socket handleove.
W8 kill: Job close/terminate ubija dokazano stablo; nepovezan/recycled PID ostaje živ.
W9 attestation: query stvarni token/AppContainer/job/ACL/network state; nije dovoljno “API call success”.
W10 update: isti suite nakon OS cumulative updatea; promjena evidence hash-a invalidira capability grant.

PROMOTE samo ako W0-W10 GREEN bez admin privilegije i capability matrica daje ENFORCED za svih 5.
Inače backend ostaje UNAVAILABLE; Job Object sam može služiti S1.2 lifecycleu, nikad S6.2 sandboxu.
```

## Windows RED imena

- `TestWindowsUnavailableBlocksHighRiskExec`
- `TestWindowsNoJobObjectOnlyPromotion`
- `TestWindowsCreateAssignResumeHasNoChildRace`
- `TestWindowsAppContainerDeniesPathAndNetworkEscape`
- `TestWindowsBreakawayChildCannotEscapeTree`
- `TestWindowsAttestationQueriesAppliedSecurityState`

---

# macOS backend — v1 fail-closed

```go
//go:build darwin

type DarwinBackend struct{}

func (DarwinBackend) Probe(context.Context) (ProbeReport, error) {
    return reportAllUnavailable("no proven supported dynamic sandbox backend"), nil
}
func (DarwinBackend) Compile(context.Context, Spec, ProbeReport) (CompiledPolicy, error) {
    return CompiledPolicy{}, ErrSandboxCapabilityUnavailable
}
func (DarwinBackend) Launch(context.Context, CompiledPolicy) (PendingProcess, error) {
    return PendingProcess{}, ErrSandboxCapabilityUnavailable
}
func (DarwinBackend) Attest(context.Context, PendingProcess, CompiledPolicy) (Attestation, error) {
    return Attestation{}, ErrSandboxCapabilityUnavailable
}
```

## Stvarni macOS hardver promotion-probe

```text
CURRENT PROOF: NOT RUN — nema pristupa macOS hardveru u ovoj fazi.
Matrica: trenutno podržane macOS major verzije × Apple Silicon + Intel × CLI tarball + notarized app/helper.
Build: CGO_ENABLED=0; codesigned/notarized proizvodni oblik; clean standard-user račun.

M0 baseline: backend vraća UNAVAILABLE; high-risk target nikad nije kreiran.
M1 support: kandidat mora koristiti dokumentiran/podržan deployment mehanizam; prisutnost sandbox-exec
   binarke ili uspješan exit code sama nije dokaz niti promotion kriterij.
M2 filesystem: RW samo grant; deny home/Keychain/TCC/system/temp; symlink/hardlink/FSEvents race fixtures.
M3 network: deny DNS, IPv4, IPv6, loopback, private/link-local; inherited socket/proxy env ne zaobilaze.
M4 process: policy se nasljeđuje kroz exec/fork; child ne može pokrenuti unsandboxed helper/launchd put.
M5 tree: child/grandchild/session/process-group escape; terminate uklanja owned tree, ne tuđi PID.
M6 entitlements: CLI vs app bundle/helper imaju isti deklarirani floor; missing entitlement fail-closed.
M7 handles/FD: target vidi samo allowlist; nema control/policy/keychain FD-a.
M8 attestation: čita stvarno primijenjeni profile/identity state, vezan uz process start-token i policy hash.
M9 update: ponoviti cijeli suite nakon svakog podržanog macOS security updatea.

PROMOTE samo ako M0-M9 GREEN na oba CPU tipa i capability matrica daje ENFORCED za svih 5.
Inače backend ostaje UNAVAILABLE; killpg je S1.2 lifecycle, nikad S6.2 sandbox.
```

## macOS RED imena

- `TestDarwinUnavailableBlocksHighRiskExec`
- `TestDarwinSandboxExecPresenceIsNotPromotionEvidence`
- `TestDarwinCandidateDeniesPathNetworkAndChildEscape`
- `TestDarwinEntitlementMismatchFailsClosed`
- `TestDarwinAttestationQueriesAppliedSecurityState`

---

# Cross-platform conformance gate

```go
func SandboxConformanceSuite(t *testing.T, backend SandboxBackend, realOS bool)
```

```text
TestSandboxMissingOneRequiredCapabilityBlocksLaunch
  mutate exactly one required capability ENFORCED→UNVERIFIED; Compile fails; Launch counter 0.

TestSandboxNeverFallsBackToWeakerBackend
  selected backend unavailable while weaker backend registered; zero call to weaker backend.

TestSandboxPolicyHashBindsCompileLaunchAttest
  change one path/argv/env byte after Compile; Attest fails; target counter 0.

TestSandboxProbeExpiryBlocksLaunch
  expired ProbeReport; Compile/Launch fail; required capability not assumed.

TestSandboxHelperFailureIsReaped
  helper exits between Launch and Attest; supervisor waits/reaps; no zombie, no target.

TestSandboxCancellationBeforeExecNeverRunsTarget
  cancel at barrier; helper reaped; sentinel target output absent.

TestSandboxHighRiskRequiresAllFiveCapabilities
  caller requests four of five; kernel adds floor/rejects; config cannot weaken.

TestSandboxAttestationIsNotSelfReportedByTarget
  malicious target prints fake attestation; ignored; only helper control channel accepted.
```

## Promotion pravilo

```text
Unit/fake GREEN != OS enforcement.
Cross-compile GREEN != OS enforcement.
VM/hardver RED suite + queried applied state + exact release artifact hash = ENFORCED.
Bez toga capability = UNVERIFIED ili UNAVAILABLE.
High-risk exec admission zahtijeva svih pet ENFORCED u jednom backendu.
```

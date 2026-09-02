> **⚠ DJELOMIČNO SUPERSEDED (REVIEW2/3):** fleet↔pairing cross-import (ExecutionNode.Device↔RemoteInvoke.Node) je ZAMIJENJEN `internal/fleet/ids` layoutom iz **`DESIGN-FIXES-r2.md` §K3** (PlacementGrant ostaje u `fleet`, ne `ids`). Za effect-path/vlasništvo vidi DESIGN-FIXES-r2.

GAPFIX codex

# Orkestracija — buildable gap-fix za S7/S12/S18

## Vlasništvo i minimalni paketni rez

- `internal/orchestration/{graph,superstep,flow,board}` posjeduje definiciju grafa, BSP izvršenje,
  kompilaciju flowa i kolaboracijske projekcije. Ne posjeduje retry, deadline, lease ni vanjske efekte.
- `internal/reliability/queue` ostaje jedini owner task-leasea, refresh/reclaima, fencing-tokena i DLQ-a.
- `internal/fleet/{registry,placement,pairing,transport}` posjeduje identitet uređaja/nodea, capability
  snapshot i placement. Rad se uvijek dispatcha preko S7 `AttemptGrant` + queue leasea.
- `internal/journal` je jedini trajni write-owner. Checkpoint, board i fleet tablice su transakcijske
  projekcije journal-eventa, ne paralelni izvori istine.
- Svi payloadi koriste S0 `Envelope`, `SchemaRef`, `ValueRef`, `ContextBlock`, `PrincipalRef` i
  `EvidenceRef`. Nema `any`, `map[string]any`, runtime Go tipova ni dinamičke deserializacije.

Ovim se dodaju samo četiri mehanizma. Flow nije drugi workflow runtime, board nije druga queue,
fleet nije drugi retry-owner, a uređaj nije implicitno izvršni node.

---

# 12.2 dopuna — typed BSP/Pregel superstep-mode

- **Tip:** MEHANIZAM JEZGRE; izbor execution-moda unutar postojećeg S12.2 DAG ownera.
- **Lokacija u planu:** zamijeniti sadašnji opis 12.2 ugovorom ispod. `DAG_WAVES` ostaje jeftini
  default; `BSP_SUPERSTEP` se bira kada paralelni nodeovi dijele reducirano stanje. S7.3 dobiva
  cross-reference na pending-write recovery, ali ne novi scheduler.

## Sinteza — Go skice

```go
package graph

type GraphID string
type GraphRevision [32]byte
type NodeID string
type ChannelID string
type Superstep uint64

type ExecutionMode uint8
const (
    DAGWaves ExecutionMode = iota + 1
    BSPSuperstep
)

type ReducerKind uint8
const (
    ReplaceOne ReducerKind = iota + 1 // točno jedan writer
    AppendOrdered                      // kanonski sort prije reducea
    SetUnion                          // schema-validirani elementi
    MinValue
    MaxValue
)

type ChannelSpec struct {
    ID       ChannelID
    Schema   contracts.SchemaRef
    Reducer  ReducerKind
    Readers  []NodeID
    Writers  []NodeID
    Required bool
}

type GraphSpec struct {
    ID              GraphID
    Revision        GraphRevision
    Mode            ExecutionMode
    Nodes           []NodeSpec
    Edges           []EdgeSpec
    Channels        []ChannelSpec
    FailurePolicy   FailurePolicy
    MaxSupersteps   uint32
    Budget          contracts.BudgetRef
    CapabilityHash  [32]byte
    PolicyHash      [32]byte
}

// ValueRef je S0-zatvoren: codec prihvaća samo registry schema/version.
type ChannelValue struct {
    Envelope contracts.Envelope
    Schema   contracts.SchemaRef
    Content  contracts.ContentRef
    Digest   [32]byte
    Lineage  contracts.Lineage
}

type StepSnapshot struct {
    RunID         contracts.RunID
    Graph         GraphID
    Revision      GraphRevision
    Step          Superstep
    JournalOffset uint64
    Values        map[ChannelID]ChannelValue // immutable view; engine ne izlaže mutatore
    SnapshotHash  [32]byte
}

type TaskInput struct {
    Envelope      contracts.Envelope
    Node          NodeID
    Step          Superstep
    SnapshotHash  [32]byte
    Reads         []ChannelValue
    Attempt       reliability.AttemptGrant
}

type ProposedWrite struct {
    Envelope     contracts.Envelope
    Node         NodeID
    TaskID       contracts.TaskID
    Ordinal      uint32
    Channel      ChannelID
    Value        ChannelValue
    AttemptNonce [32]byte
}

type Reducer interface {
    Reduce(ctx context.Context, spec ChannelSpec, base *ChannelValue,
        writes []ProposedWrite) (ChannelValue, error)
}

type StepCommit struct {
    RunID          contracts.RunID
    Graph          GraphID
    Revision       GraphRevision
    Step           Superstep
    BaseHash       [32]byte
    PendingHash    [32]byte
    NextHash       [32]byte
    TerminalTasks  []contracts.TaskID
    JournalOffset  uint64
}

type PendingWriteStore interface {
    Stage(ctx context.Context, write ProposedWrite) error
    List(ctx context.Context, run contracts.RunID, step Superstep) ([]ProposedWrite, error)
}

type SuperstepEngine interface {
    Prepare(ctx context.Context, spec GraphSpec, prior *StepCommit) (StepSnapshot, []TaskInput, error)
    Commit(ctx context.Context, spec GraphSpec, snap StepSnapshot,
        terminal []TaskResult) (StepCommit, error)
}
```

## Izvršni algoritam i invarijante

1. `Prepare` validira pinned `GraphRevision`, capability/policy hash, S7 budget i S0 sheme. Iz
   posljednjeg `StepCommit` eventa materijalizira jedan `StepSnapshot` i daje svim runnable taskovima
   isti `SnapshotHash`. Task API nema pristup live channel-storeu.
2. Svaki task zapisuje `ProposedWrite` u pending područje pod unique ključem
   `(run_id,graph_revision,step,task_id,attempt_nonce,ordinal)`. Isti ključ + isti digest je replay;
   isti ključ + drugi digest je `IDEMPOTENCY_CONFLICT`.
3. Task ne postaje terminalno uspješan dok njegov pending-write nije durable. Za `PURE` ili dokazano
   idempotentan task resume može ponovno upotrijebiti write; za nedovršen vanjski efekt S7 stanje je
   `UNKNOWN_EFFECT`, ne drugi poziv.
4. `Commit` čita samo writeove aktualnih `AttemptGrant` nonceova, provjerava writer ACL i točnu
   schema-verziju te monotonost trust/sensitivity/lineage polja. Zatim kanonski sortira po
   `(channel_id,node_id,task_id,ordinal)`. Reducer ne smije ovisiti o completion redoslijedu.
5. U jednoj journal-transakciji zapisuje `SUPERSTEP_COMMITTED` s `BaseHash`, `PendingHash`,
   `NextHash` i projection updateom checkpointa. Sljedeći korak ne može biti admitted prije durable
   commita. Pad prije commita ostavlja isti prethodni snapshot; pad poslije commita vidi novi.
6. `ReplaceOne` odbija 0 ili >1 write kad je kanal required. Custom reducer u v1 nije dozvoljen:
   zatvoreni reducer-enum smanjuje nondeterminism i supply-chain površinu. Plugin reducer je kasniji
   upgrade tek nakon 6.8/11.5 gatea i dokazne determinism provjere.
7. Implicitni ciklus je nevaljan. Eksplicitni `LoopEdge` mora nositi `MaxIterations`, S7 budget,
   typed exit-predicate i progress metric; inače `GRAPH_CYCLE_UNBOUNDED`. To zadržava DAG default,
   ali ne gubi korisni review→fix loop iz LangGrapha.

```go
type LoopEdge struct {
    From, To       NodeID
    MaxIterations  uint32
    Exit           Predicate // zatvoreni typed AST, ne eval/JS
    ProgressMetric MetricRef
    Budget         contracts.BudgetRef
}
```

## Salvage

- **Python→Go port obrasca, ne koda:** NEXUS `core/workflow.py:27-87` daje persistirane nodeove,
  dependency rubove i stanja; zadržati samo deklarativni graph input. Njegov best-effort swallow i
  overwrite po `(run_id,agent)` nisu dopušteni kao izvršna istina.
- NEXUS-ov postojeći wave scheduler/cycle guard iz audita postaje `DAGWaves`; novi
  `BSPSuperstep` dijeli validator, S7 grantove i journal s njim.
- S7.3 pending-write tablica i S0 schema registry su reuse-owneri; S12 ne uvodi zasebnu bazu ili codec.

## Ugovor / RED gate

- `RED-S12.2-BSP-01 same_snapshot_within_step`: node B ne smije pročitati write nodea A iz istog
  superstepa; dobiva vrijednost iz `SnapshotHash` koraka N.
- `RED-S12.2-BSP-02 completion_order_independent`: isti task-writeovi završeni redom A,B,C i C,A,B
  moraju dati identičan `PendingHash`, `NextHash` i checkpoint bytes.
- `RED-S12.2-BSP-03 reduce_checkpoint_atomic`: fault injection nakon reducea, prije fsynca, mora nakon
  restarta vratiti korak N; nikad reducirano stanje bez odgovarajućeg checkpoint eventa.
- `RED-S12.2-BSP-04 lineage_cannot_downgrade`: write s ispravnim contentom, ali nižim trust/sensitivity
  od input lineagea, mora odbiti cijeli superstep bez journal advancea.
- `RED-S12.2-BSP-05 stale_attempt_write`: write starog attempt noncea nakon regranta ne ulazi u reduce.
- `RED-S12.2-BSP-06 unbounded_loop_rejected`: ciklus bez `MaxIterations+Budget+ProgressMetric` ne prolazi
  graph admission.

## Who-has-what referenca i Pareto-floor

- **LangGraph IMA DUBOKO:** immutable channel state unutar koraka i next-step visibility su u
  `libs/langgraph/langgraph/pregel/main.py:2959-2969`; atomic reduce pa checkpoint u
  `pregel/_loop.py:683-724`; task pending-write recovery u `pregel/_loop.py:415-505,661-816`.
- **LangGraph je PLIĆI na ugovoru:** state update/control payloadi su `Any`
  (`graph/state.py:131-200`; `types.py:821-824`), a vanjski side-effect nije exactly-once.
- **Naš floor:** najmanje LangGraphova BSP determinističnost i partial-step recovery, plus S0 typed
  envelope, lineage gate, S7 effect taxonomy/fencing i jedan journal write-owner.

---

# 7.2 dopuna — lease refresh, conditional reclaim i DLQ

- **Tip:** MEHANIZAM JEZGRE; proširenje postojećeg P2.3 queue contracta.
- **Lokacija u planu:** u 7.2 iza postojećeg claim/heartbeat/reclaim ugovora. S7.3 outbox ostaje
  vlasnik deliveryja, a S18.1 samo worker-potrošač.

## Sinteza — Go skice

```go
package queue

type TaskState uint8
const (
    Ready TaskState = iota + 1
    Leased
    CommitPending
    Succeeded
    FailedRetryable
    FailedTerminal
    DeadLettered
    UnknownEffect
    Reconciling
    Cancelled
)

type TaskID string
type LeaseID string
type WorkerID string

type QueueTask struct {
    ID              TaskID
    Tenant          contracts.TenantID
    IntentID        contracts.IntentID
    IdempotencyKey  contracts.IdempotencyKey
    RequestHash     [32]byte
    Kind            contracts.OperationKind
    Payload         contracts.ValueRef
    Effect          reliability.EffectClass
    State           TaskState
    Attempt         uint32
    MaxAttempts     uint32
    NotBefore       time.Time
    AttemptDeadline time.Time
    LeaseID         LeaseID
    LeaseOwner      WorkerID
    Fence           uint64
    LeaseExpiresAt  time.Time
    HeartbeatSeq    uint64
    ProgressSeq     uint64
    ResultHash      [32]byte
    ErrorCode       contracts.ErrorCode
    DeadLetter      *DeadLetterMeta
}

type LeaseToken struct {
    TaskID          TaskID
    LeaseID         LeaseID
    Owner           WorkerID
    Fence           uint64
    Attempt         uint32
    AttemptNonce    [32]byte
    ExpiresAt       time.Time
    HardDeadline    time.Time
}

type DeadLetterMeta struct {
    Reason          contracts.ErrorCode
    FailedAt        time.Time
    LastLease       LeaseToken
    OriginalHash    [32]byte
    DiagnosticRefs  []contracts.EvidenceRef
    ReplayCount     uint32
}

type Store interface {
    Enqueue(context.Context, EnqueueRequest) (TaskID, error)
    Claim(context.Context, ClaimRequest) (QueueTask, LeaseToken, error)
    Refresh(context.Context, LeaseToken, Progress) (LeaseToken, error)
    BeginCommit(context.Context, LeaseToken, [32]byte) error
    Commit(context.Context, LeaseToken, contracts.ResultRef) error
    Fail(context.Context, LeaseToken, reliability.Failure) error
    ReclaimExpired(context.Context, time.Time, uint32) ([]TaskID, error)
    RequeueDeadLetter(context.Context, DLQReplayRequest) (TaskID, error)
}
```

## Tranzicije i invarijante

1. `Enqueue` atomically claim-a unique `(tenant_id,idempotency_key)`. Isti request hash vraća postojeći
   task; različit hash vraća `IDEMPOTENCY_KEY_REUSED`. Idempotency tombstone živi najmanje koliko i
   najdulji DLQ replay prozor.
2. `Claim` radi jedan SQL `UPDATE ... WHERE state=READY AND not_before<=now RETURNING`, postavlja novi
   random `LeaseID`, povećava `Fence` i `Attempt`, te izdaje S7 `AttemptGrant`. Samo jedan worker dobiva
   redak.
3. `Refresh` radi CAS nad `(task_id,state=LEASED,lease_id,owner,fence)` i monotonim `HeartbeatSeq`.
   Produžuje do `min(now+LeaseTTL, AttemptDeadline)`; nikad ne pomiče hard deadline i ne može beskonačno
   držati task. Refresh sa starim tokenom je `STALE_FENCING_TOKEN`, ne noop-success.
4. `ReclaimExpired` ponovno provjerava da je lease još istekao unutar iste transakcije. Za `PURE` ili
   receiptom dokazano idempotentan rad prelazi u `READY` s većim fenceom; ako je efekt možda izvršen,
   prelazi u `UNKNOWN_EFFECT→RECONCILING`, nikad izravno u retry.
5. `Fail` premješta u DLQ kada je greška terminalna, payload nečitljiv, iscrpljen attempt/age budget
   ili operator-policy tako kaže. DLQ čuva originalni typed payload/ref, request hash, zadnji lease,
   razlog i dokaze; korumpirani payload se nikad ne pokušava izvršiti.
6. `RequeueDeadLetter` zahtijeva operator/HITL scope, ponovno provjerava S6 policy i capability closure,
   zadržava isti `IntentID` i idempotency identitet te stvara novi attempt. Za izmijenjeni payload
   potreban je novi intent/key; “edit and replay” nije dopušten.
7. `Commit` vrijedi samo za aktualni fence i nakon `BeginCommit`. ACK nastaje nakon durable result
   commita; channel delivery ide preko S7.3 outboxa, pa `run_done != result_delivered` ostaje očuvano.

**Minimalna SQLite shema:** `queue_tasks` ima unique `(tenant_id,idempotency_key)`; indeks
`(state,not_before,priority,created_at)`; `queue_dlq` je immutable snapshot keyed by `(task_id,fence)`;
svaka mutacija i pripadni journal event nastaju u jednoj `BEGIN IMMEDIATE` transakciji.

## Salvage

- **Port uz obvezni popravak:** NEXUS `core/queue.py:49-68` ima ispravan single-winner
  `pending→running` CAS i `core/queue.py:71-103` izolira task failure. Ne preuzeti tvrdnju
  “crash-safe”: nema lease, refresh, reclaim ni fence, a delivery exception se guta.
- **Port:** NEXUS `core/fleet.py:57-77` ima attempt-conditional claim/heartbeat i
  `core/fleet.py:80-100` odbija kasni attempt update; objediniti semantiku u S7 token, ne zadržati
  drugi fencing-owner u fleetu.
- `core/circuit.py` daje stagnation/circuit signale, ali odluku retry/DLQ donosi ovaj S7 owner.

## Ugovor / RED gate

- `RED-S7.2-Q-01 concurrent_claim_single_winner`: 32 claimera nad istim taskom → jedan lease/fence.
- `RED-S7.2-Q-02 stale_refresh_cannot_extend`: nakon reclaima refresh starim tokenom mora vratiti
  `STALE_FENCING_TOKEN`, a `lease_expires_at` ostaje nepromijenjen.
- `RED-S7.2-Q-03 refresh_cannot_cross_deadline`: heartbeat prije isteka ne smije produžiti lease preko
  `AttemptDeadline`; task se zatim reclaima/fencea.
- `RED-S7.2-Q-04 stale_worker_cannot_commit`: worker A/fence 7 zastane, B dobije fence 8 i commita;
  A-ov result mora biti odbijen, kanonski result ostaje B.
- `RED-S7.2-Q-05 unknown_effect_not_retried`: pad nakon simuliranog vanjskog efekta, prije result commita,
  mora završiti u `UNKNOWN_EFFECT`, uz nula novih fizičkih pokušaja do reconciliation receipt-a.
- `RED-S7.2-Q-06 dlq_replay_preserves_intent`: replay istog DLQ recorda ne smije duplicirati izvršenje;
  ista idempotency-key s promijenjenim hashom mora biti odbijena.
- `RED-S7.2-Q-07 corrupt_payload_quarantined`: nečitljiv S0 payload ide u DLQ s diagnostic refom i
  nikad se ne predaje workeru.

## Who-has-what referenca i Pareto-floor

- **OpenClaw IMA DUBOKO:** claim token/owner u `src/channels/message/ingress-queue.ts:40-72`, DLQ i
  resubmit ishodi u `:84-137`, atomic claim u `:930-969`, token-guarded refresh u `:972-998` te
  stale conditional release/recovery u `:1000-1091`.
- **NEXUS IMA PLIĆE:** atomic claim postoji (`core/queue.py:49-68`), ali `running` nakon crasha nema
  lease/reclaim; auditirani `fleet.py` daje korisni attempt-CAS, ne durable task queue.
- **Naš floor:** OpenClaw claim/refresh/stale-CAS/DLQ plus stroži hard-deadline, monotoni fence,
  effect-aware `UNKNOWN_EFFECT`, idempotency retention i S7.3 outbox.

---

# 12.7 nova podsekcija — versioned flows kao compiler u S12.2

- **Tip:** DATA-MANIFEST + MEHANIZAM KOMPILATORA; nema zasebnog executora.
- **Lokacija u planu:** nova `12.7 Flow definicije, kompilacija i run-control` pod `multi-agent`.
  Flow UX/API adapteri idu u S14, ali kanonska validacija i compile pripadaju S12.

## Sinteza — Go skice

```go
package flow

type FlowID string
type FlowVersion uint64
type FlowState uint8
const (
    Draft FlowState = iota + 1
    Validated
    Active
    Deprecated
)

type NodeKind uint8
const (
    AgentStep NodeKind = iota + 1
    ToolStep
    HumanGate
    RouterStep
    MapStep
    JoinStep
    WaitStep
    SubflowStep
)

type FlowDefinition struct {
    ID             FlowID
    Version        FlowVersion
    RevisionHash   [32]byte
    State          FlowState
    Input          contracts.SchemaRef
    Output         contracts.SchemaRef
    Nodes          []FlowNode
    Edges          []FlowEdge
    CapabilityRefs []contracts.CapabilityRef
    Budget         contracts.BudgetRef
    Owner          contracts.PrincipalRef
    Signature      contracts.SignatureRef
}

type FlowNode struct {
    ID       graph.NodeID
    Kind     NodeKind
    Input    contracts.SchemaRef
    Output   contracts.SchemaRef
    Agent    *AgentStepSpec
    Tool     *ToolStepSpec
    Gate     *HumanGateSpec
    Router   *RouterSpec
    Map      *MapSpec
    Join     *JoinSpec
    Wait     *WaitSpec
    Subflow  *SubflowSpec
}

// Validate zahtijeva da je postavljena točno jedna konfiguracija koja odgovara Kind.
type FlowEdge struct {
    From, To graph.NodeID
    When     *Predicate // zatvoreni typed AST: Eq/Ne/Lt/Le/Gt/Ge/And/Or/Exists
    Loop     *graph.LoopEdge
}

type Compiler interface {
    Validate(context.Context, FlowDefinition) (ValidationReport, error)
    Compile(context.Context, FlowDefinition) (graph.GraphSpec, error)
}

type Service interface {
    PutDraft(context.Context, FlowDefinition) (FlowDefinition, error)
    Activate(context.Context, FlowID, FlowVersion, [32]byte) error
    Start(context.Context, StartRequest) (contracts.RunID, error)
    Pause(context.Context, contracts.RunID, contracts.PrincipalRef) error
    Resume(context.Context, contracts.RunID, contracts.ApprovalRef) error
    Cancel(context.Context, contracts.RunID, contracts.PrincipalRef) error
}
```

## Kompilacijski ugovor

1. Flow je immutable versioned data. `PutDraft` stvara novu verziju; `Activate` CAS-om pinna točan
   `RevisionHash`. Aktivna verzija se ne mijenja in-place.
2. `Validate` provjerava jedinstvene node ID-eve, typed edge kompatibilnost, reachability, output path,
   capability closure, S6 policy, S7 budget i cikluse. Router predicate može čitati samo deklarirana
   typed polja; nema JS/eval/prompt-string routing-a.
3. `Compile` proizvodi jedini kanonski `graph.GraphSpec`. `MapStep` postaje bounded fan-out kanal,
   `JoinStep` reducer, `HumanGate` S12.4 interrupt, `WaitStep` S7 durable timer, `SubflowStep` pinned
   child graph namespace. Nema flow-specifičnog retrya ili checkpointa.
4. `Start` pinna flow version, graph revision, config, policy, capability closure i workspace base u
   run envelope. Hot update utječe samo na nove runove; resume uvijek koristi pinned revision.
5. Agent/model smije predložiti flow draft, ali ga ne može aktivirati niti konstruirati nedeklarirani
   edge. Aktivacija je operator/policy odluka i audit event.

## Salvage

- **Port:** NEXUS `core/workflow.py:18-79` daje node/needs/state reprezentaciju za osnovni importer;
  `managers/subagent_orchestrator.py` daje agent-node lifecycle; S12.2 compiler mora zamijeniti
  best-effort persistence i dodati typed output ugovor.
- Reuse postojećih Nexus plan-ownera: S11.6 role/agent/skill manifest interpreter, S12.2 graph,
  S12.4 HITL, S7 durable timer/budget i S0.4 migracije. Nema novog expression dependencyja u v1.

## Ugovor / RED gate

- `RED-S12.7-FLOW-01 immutable_active_revision`: promjena aktivne definicije bez nove verzije i novog
  hash-a mora biti odbijena; postojeći run zadržava stari graph revision.
- `RED-S12.7-FLOW-02 typed_edge_mismatch`: output `schema:A@2` spojen na input `schema:B@1` ne smije
  proći compile bez registriranog lossless upcastera iz S0.4.
- `RED-S12.7-FLOW-03 undeclared_model_route`: model-output koji imenuje nepostojeći node/edge mora
  biti tretiran kao untrusted data i odbijen, ne dinamički izvršen.
- `RED-S12.7-FLOW-04 no_shadow_executor`: fault u S7 grant-owneru mora zaustaviti flow; flow servis ne
  smije lokalno retryati node niti advanceati stanje.
- `RED-S12.7-FLOW-05 bounded_loop_only`: eksplicitni loop bez cap/budget/progress metrike je invalid.
- `RED-S12.7-FLOW-06 restart_pins_revision`: nakon restarta i aktivacije v2, run pokrenut na v1 mora
  nastaviti isključivo na v1 checkpointu.

## Who-has-what referenca i Pareto-floor

- **OpenClaw IMA ŠIROKO:** full-code landscape audit bilježi 58 workflow/flow source fajlova i stvarni
  assistant-flow surface; to je referenca za authoring/run-control, ne za naš kanonski executor
  (`HARNESS-PLAN.md`, Addendum A8/A9: OpenClaw flows → nove asistentske podsekcije).
- **LangGraph IMA DUBOKO:** conditional routing, fan-out i join (`graph/state.py:928-1030`,
  `types.py:704-776`) te durable nested graph semantics; njegov široki `Any` i otvoreni ciklusi nisu
  graftani.
- **Naš floor:** OpenClawova assistant ergonomija + LangGraphova graph izražajnost, ali jedan S12.2
  typed compiler/runtime, S7 sole retry-owner i pinned immutable flow revision.

---

# 12.8 nova podsekcija — shared boards/tasks kao autorizirana journal-projekcija

- **Tip:** MEHANIZAM PODATAKA + PROJEKCIJA; S14 CLI/TUI/web/desktop su tanki adapteri.
- **Lokacija u planu:** nova `12.8 Shared board/task coordination`. Aktivira je `multi-agent`; read-only
  osobni board može biti UI projekcija bez multi-agent dispatcha. Nije zamjena za S7 queue ni S12.2 DAG.

## Sinteza — Go skice

```go
package board

type BoardID string
type CardID string
type Revision uint64

type CardState uint8
const (
    Todo CardState = iota + 1
    Ready
    Running
    Blocked
    InReview
    Done
    CardCancelled
)

type Board struct {
    ID          BoardID
    Tenant      contracts.TenantID
    Session     *contracts.SessionID
    Revision    Revision
    Columns     []Column
    PolicyHash  [32]byte
}

type TaskCard struct {
    ID             CardID
    Board          BoardID
    Revision       Revision
    Title          string
    Description    contracts.ContextBlock
    State          CardState
    Assignees      []contracts.PrincipalRef
    DependsOn      []CardID
    FlowRun        *contracts.RunID
    ChildRun       *contracts.RunID
    Obligation     *contracts.ObligationRef
    Artifacts      []contracts.ArtifactRef
    Evidence       []contracts.EvidenceRef
    Sensitivity    contracts.Sensitivity
    DueAt          *time.Time
}

type OpKind uint8
const (
    CreateCard OpKind = iota + 1
    MoveCard
    AssignCard
    SetCardState
    LinkRun
    LinkObligation
    AddArtifact
    AddEvidence
    AddComment
)

type Mutation struct {
    Envelope         contracts.Envelope
    Board            BoardID
    ExpectedRevision Revision
    IdempotencyKey   contracts.IdempotencyKey
    Kind             OpKind
    Card             CardID
    Payload          contracts.ValueRef
}

type Store interface {
    Snapshot(context.Context, BoardID, contracts.PrincipalRef) (Board, []TaskCard, error)
    Apply(context.Context, Mutation, contracts.PrincipalRef) (Revision, error)
}
```

## Invarijante i prijelazi

1. `Apply` validira AuthN/AuthZ, tenant/profile scope, expected board revision, idempotency key i typed
   op payload; zatim atomically appenda `BOARD_MUTATED` journal event i ažurira projection revision.
2. Isti key+isti op vraća prethodni rezultat; isti key+drugi op je konflikt. Dvije mutacije nad istom
   revision imaju jednog pobjednika; gubitnik dobiva `REVISION_CONFLICT` s aktualnom revision, bez
   automatskog LLM mergea.
3. Dozvoljeni prijelazi su zatvoreni. `Running→InReview` može predložiti worker; `InReview→Done`
   zahtijeva S16.6 checker/user evidence kad je kartica vezana uz run. Agent koji izvršava rad ne može
   sam sebi izdati verified completion.
4. Card je koordinacijski prikaz. `CreateCard`, `MoveCard` ili replay journal-projekcije nikad ne
   enqueueaju rad. Jedini side-effectful bridge je eksplicitni `Start linked flow`, koji prvo kreira
   S12.7 run i S7 task, pa potom journalom veže njihove ID-eve.
5. `DependsOn` mora biti acikličan unutar boarda. Card ne prelazi u `Ready` dok svi dependency cardovi
   nisu `Done`; to je readiness projekcija, ne workflow scheduling autoritet.
6. Description/comment/artifact zadržavaju S0 trust, sensitivity i lineage. Subagent vidi samo kartice
   dopuštene vlastitim principal/capability closureom. S14 renderer nikad ne izvršava raw board HTML.
7. `Done` za run s vanjskom isporukom zahtijeva i completion evidence i S7.3 delivery receipt kada je
   delivery dio card contracta.

## Salvage

- **Port:** NEXUS `core/fleet.py:1-13,80-100` korisno razdvaja agent `activity` od human `workflow`
  stanja i attempt-conditionalno odbija stragglera; očuvati ta dva osi u projectionu.
- **Port samo za read model:** NEXUS `core/workflow.py:63-79` daje dependency/state view, a
  `gateway/dashboard.py` može informirati adapter. Ne salvageati best-effort swallow ni shared mutable
  row kao kanonsku istinu.
- S0 journal, S6 auth, S7 run/delivery, S12.7 flow i S16.6 checker su postojeći owneri; board samo veže
  njihove referencije.

## Ugovor / RED gate

- `RED-S12.8-BOARD-01 concurrent_revision`: dva `MoveCard` zahtjeva s istim `ExpectedRevision` → točno
  jedan uspijeva; drugi dobiva konflikt i ništa nije izgubljeno.
- `RED-S12.8-BOARD-02 worker_cannot_self_verify`: worker koji je proizveo linked-run rezultat ne može
  postaviti `Done` bez neovisnog evidence/checker principal-a.
- `RED-S12.8-BOARD-03 projection_replay_has_no_effect`: rebuild board projectiona iz journala mora
  proizvesti nula novih queue taskova, tool poziva i poruka.
- `RED-S12.8-BOARD-04 tenant_and_sensitivity`: subagent drugog tenant/profile scopea ne smije ni otkriti
  naslov sensitive kartice.
- `RED-S12.8-BOARD-05 idempotency_conflict`: isti mutation key s drukčijim payload hashom mora biti
  odbijen, a revision ostaje ista.
- `RED-S12.8-BOARD-06 delivery_contract`: run `SUCCEEDED`, ali outbox bez receipt-a, ne smije automatski
  postaviti delivery-obvezanu karticu na `Done`.

## Who-has-what referenca i Pareto-floor

- **OpenClaw IMA DUBOKO za board surface:** `src/boards/board-store.ts:20-58` definira revisioned store,
  snapshot, ops i grant; `:60-63,176-223` postavlja count/byte granice i validira widget sadržaj.
- **NEXUS IMA PLIĆE:** `core/fleet.py` ima korisne odvojene activity/workflow osi, ali nije revisioned,
  multi-actor task board s evidence gateom.
- **Naš floor:** OpenClaw revision/grant/bounds obrazac + NEXUS dvije osi, uz journal-only writes,
  optimistic CAS, S16.6 neovisni completion gate i eksplicitnu zabranu board→execution side effecta.

---

# 18.5 nova podsekcija — fleet control-plane i placement

- **Tip:** SERVICE MEHANIZAM; aktivira se `service` ili `device-mesh` capability flagom.
- **Lokacija u planu:** nova `18.5 Fleet membership, placement i drain`; koristi 18.1 workere, ali ne
  duplicira njihov queue. Device identitet/pairing je odvojen u 18.6.

## Sinteza — Go skice

```go
package fleet

type FleetID string
type NodeID string
type FleetEpoch uint64
type NodeEpoch uint64

type NodeState uint8
const (
    NodeJoining NodeState = iota + 1
    NodeReady
    NodeDraining
    NodeOffline
    NodeRevoked
)

type Capacity struct {
    CPUCores uint16
    RAMBytes uint64
    DiskFree uint64
    Slots    uint16
}

type ExecutionNode struct {
    ID                NodeID
    Fleet             FleetID
    Device            *pairing.DeviceID
    WorkloadPrincipal contracts.PrincipalRef
    Epoch             NodeEpoch
    State             NodeState
    OS, Arch          string
    AgentVersion      string
    Capabilities      []contracts.CapabilityClaim
    CapabilityHash    [32]byte
    Capacity          Capacity
    HeartbeatSeq      uint64
    LastSeen          time.Time
}

type FleetSnapshot struct {
    Fleet        FleetID
    Epoch        FleetEpoch
    Nodes        []ExecutionNode
    SnapshotHash [32]byte
}

type PlacementRequest struct {
    Task              queue.TaskID
    Required          []contracts.CapabilityRef
    Budget            contracts.BudgetRef
    DataResidency     contracts.ResidencyPolicy
    WorkspaceAffinity *contracts.WorkspaceRef
}

type PlacementGrant struct {
    Task               queue.TaskID
    Node               NodeID
    FleetEpoch         FleetEpoch
    NodeEpoch          NodeEpoch
    CapabilityHash     [32]byte
    PolicyHash         [32]byte
    Attempt            reliability.AttemptGrant
    ExpiresAt          time.Time
}

type Registry interface {
    Register(context.Context, NodeRegistration) (ExecutionNode, error)
    Heartbeat(context.Context, SignedHeartbeat) (ExecutionNode, error)
    Drain(context.Context, NodeID, time.Time) error
    Revoke(context.Context, NodeID, contracts.PrincipalRef) error
    Snapshot(context.Context, FleetID) (FleetSnapshot, error)
}

type PlacementPolicy interface {
    Place(context.Context, PlacementRequest, FleetSnapshot) (PlacementGrant, error)
}
```

## Fleet ugovor

1. Node registracija veže workload principal, node epoch, software version i S0.3 declared∩measured
   capabilities. Capability claim nije modelov tekst; mora doći iz verifier/attestor adaptera.
2. Signed heartbeat nosi `(node_id,node_epoch,seq,capability_hash,capacity,timestamp)`. Registry prihvaća
   samo strogo veći `seq`; promjena capability hash-a povećava fleet epoch i invalidira neiskorištene
   placement grantove.
3. `Place` radi nad immutable `FleetSnapshot`, filtrira policy/residency/capability/budget, zatim koristi
   deterministički tie-break `(available_slots desc, load asc, node_id asc)`. Rezultat pinna snapshot,
   node epoch i capability hash.
4. Dispatch provjerava grant neposredno prije S7 `Claim`. Ako su node/capability/policy epoch stale,
   task ostaje `READY` i ponovno se placea. Nakon mogućeg vanjskog efekta ne radi se transparentan
   reroute nego S7 `UNKNOWN_EFFECT` reconciliation.
5. S18 smije drainati/revokeati node, ali ne izdaje retry niti lease. Svaki remote invoke nosi aktualni
   S7 `LeaseToken/AttemptGrant`; stale node result ne može commitati.

## Salvage

- **Python→Go port:** NEXUS `core/fleet.py:25-77` ima concurrency-safe run identity,
  attempt-CAS i conditional heartbeat; `:80-100` ima terminal/attempt guard; `:139-155` korisni
  render-side stale view. Portati semantiku, ali ne PID-liveness kao remote-node dokaz.
- `core/queue.py` nije salvage za fleet scheduler; queue ostaje popravljeni S7 owner. GORTEX ostaje
  lokalni executor iza `NodeTransport`, ne control-plane baza.

## Ugovor / RED gate

- `RED-S18.5-FLEET-01 heartbeat_replay`: signed heartbeat sa seq 40 nakon prihvaćenog seq 41 mora biti
  odbijen i ne smije vratiti node iz `Draining/Revoked` u `Ready`.
- `RED-S18.5-FLEET-02 capability_changed_after_place`: promjena capability hash-a između placementa i
  dispatcha mora odbiti grant prije claima.
- `RED-S18.5-FLEET-03 stale_node_result`: task se s nodea A/fence 7 reassigna na B/fence 8; A-ov kasni
  result se odbija i ne mijenja board/run state.
- `RED-S18.5-FLEET-04 drain_is_not_success`: drain s aktivnim taskom mora zaustaviti nove claimove,
  pričekati ili fenceati postojeći rad te nikad označiti task uspješnim samo zato što je node ugašen.
- `RED-S18.5-FLEET-05 deterministic_placement`: permutacija reda istog fleet snapshota mora izabrati
  isti node i proizvesti isti decision hash.

## Who-has-what referenca i Pareto-floor

- **OpenClaw IMA DUBOKO na node protokolu:** presence, pair/list i dynamic tool/skill registri su u
  `packages/gateway-protocol/src/schema/nodes.ts:18-150`; idempotent invoke i ordered progress u
  `:153-191`; bounded pending drain/enqueue u `:200-235`.
- **NEXUS IMA PLIĆE:** `core/fleet.py` robustno prati lokalne run attemptove, ali nema remote fleet
  membership, capability-pinned placement ni horizontalni node protocol.
- **Naš floor:** OpenClaw node ergonomija + NEXUS attempt guards, uz typed S0 payload, capability
  attestation, immutable placement snapshot i S7 lease/fencing kao jedini execution owner.

---

# 18.6 nova podsekcija — device pairing i remote invoke trust boundary

- **Tip:** SIGURNOSNI MEHANIZAM + TRANSPORT ADAPTER; `device-mesh` flag, uz obvezne S6/S7/S18.5
  closure dependencyje.
- **Lokacija u planu:** nova `18.6 Paired devices i execution-node protocol`. Device je korisnički
  identitet/credential; node je izvršna instanca. Jedan device može hostati više nodeova, a headless
  service node može imati workload identitet bez ljudskog device pairinga.

## Sinteza — Go skice

```go
package pairing

type DeviceID string
type PairingID string
type CredentialEpoch uint64

type PairingState uint8
const (
    PairRequested PairingState = iota + 1
    PairChallenged
    PairApproved
    PairActive
    PairRotating
    PairRevoked
    PairExpired
)

type PairingChallenge struct {
    ID              PairingID
    DevicePublicKey []byte
    Nonce           [32]byte
    CodeHash        [32]byte
    RequestedScopes []contracts.Scope
    Profile         contracts.ProfileID
    ExpiresAt       time.Time
    State           PairingState
}

type PairedDevice struct {
    ID              DeviceID
    Profile         contracts.ProfileID
    PublicKey       []byte
    DisplayName     string
    Platform        string
    Family          string
    Scopes          []contracts.Scope
    CredentialEpoch CredentialEpoch
    State           PairingState
    LastSeen        time.Time
}

type PairingService interface {
    Start(context.Context, PairingRequest) (PairingChallenge, error)
    Prove(context.Context, PairingID, []byte, []byte) (PendingApproval, error)
    Approve(context.Context, PairingID, contracts.PrincipalRef) (PairedDevice, Credential, error)
    Rotate(context.Context, DeviceID, contracts.PrincipalRef) (Credential, error)
    Revoke(context.Context, DeviceID, contracts.PrincipalRef) error
}

type RemoteInvoke struct {
    Envelope              contracts.Envelope
    InvokeID              contracts.InvokeID
    IntentID              contracts.IntentID
    IdempotencyKey        contracts.IdempotencyKey
    Node                  fleet.NodeID
    ExpectedCapabilityHash [32]byte
    Approval              *contracts.ApprovalRef
    Session               contracts.SessionRef
    Attempt               reliability.AttemptGrant
    Input                 contracts.ValueRef
    Deadline              time.Time
}

type Progress struct {
    InvokeID contracts.InvokeID
    Node     fleet.NodeID
    Seq      uint64
    Chunk    contracts.ValueRef
}

type NodeTransport interface {
    Invoke(context.Context, fleet.PlacementGrant, RemoteInvoke) (<-chan Progress, <-chan Result, error)
}
```

## Pairing i transport ugovor

1. `Start` stvara kratkoživući single-use nonce/code challenge; baza sprema samo code hash. `Prove`
   zahtijeva possession privatnog ključa nad challenge transcriptom. Proof ne aktivira uređaj:
   operator/HITL `Approve` sužava scope i izdaje credential.
2. Privatni ključ ostaje na uređaju u OS key storeu. Server sprema javni ključ i hash/ID credentiala,
   nikad plaintext bearer token. Credential je vezan uz `DeviceID+ProfileID+Scopes+Epoch+Expiry`.
3. Rotate atomically povećava credential epoch i opoziva prethodni credential. Revoke je neposredan:
   prekida nove invokeove i fencea aktivne S7 grantove; offline node se ne može vratiti starim tokenom.
4. `RemoteInvoke` zahtijeva mTLS ili signed challenge, aktualni device/node epoch, exact scope, pinned
   capability/policy hash, S7 attempt grant, idempotency key i deadline. Pairing/card/capability oglas
   sam po sebi nije autorizacija.
5. Progress je bounded typed stream sa strogo monotonim `Seq`; duplicate se ignorira samo ako je digest
   isti, a isti seq+drugi digest je protocol violation. Result commit ide kroz S7 fence i S7.3 outbox.
6. Discovery/connection koristi S6.3 SSRF/egress gate; setup QR/code ne smije sadržavati dugovječni
   puni credential. Default pairing scope je minimalan, ne operator-admin.

## Salvage

- NEXUS nema dokazani device-pairing owner za port. Salvagea se samo S6 credential/secrets broker,
  S0.3 capability negotiation, S7 grant/fence i `core/fleet.py` stabilni run/attempt identitet.
- **Pattern, ne code-copy:** OpenClaw odvaja device token/pairing od node presence/invoke protokola;
  naš Go dizajn zadržava tu granicu, ali dodaje proof-of-possession, minimalni default scope,
  capability hash, S7 fence i tenant/profile binding.

## Ugovor / RED gate

- `RED-S18.6-PAIR-01 challenge_replay`: drugi `Prove` istog ili isteklog challengea mora biti odbijen;
  ne smije nastati drugi credential.
- `RED-S18.6-PAIR-02 proof_is_not_approval`: valjan key proof bez operator approvala ne smije dobiti
  aktivni scope niti invokeati node.
- `RED-S18.6-PAIR-03 rotated_token_revoked`: nakon rotatea stari epoch ne smije heartbeatati, dohvatiti
  pending task ni poslati result.
- `RED-S18.6-PAIR-04 cross_profile_denied`: uređaj u profilu A ne može čitati/invokeati task profila B
  čak ni s valjanim potpisom.
- `RED-S18.6-PAIR-05 capability_advertisement_not_auth`: node oglasi alat koji nije u policy closureu;
  invoke mora biti odbijen prije transporta.
- `RED-S18.6-PAIR-06 progress_equivocation`: isti progress seq s drugim digestom fencea invoke i bilježi
  audit incident; nikad se ne spaja kao novi chunk.
- `RED-S18.6-PAIR-07 offline_reconnect_stale_fence`: nakon isteka leasea i reassigna offline uređaj se
  spoji te vrati rezultat starog attempta; commit mora biti odbijen.

## Who-has-what referenca i Pareto-floor

- **OpenClaw IMA DUBOKO:** device approve/reject/remove/rotate/revoke i public-key metadata su u
  `packages/gateway-protocol/src/schema/devices.ts:7-65`; setup-code/QR i access profile u `:75-116`.
  Node pairing, presence, dynamic capabilities, idempotent invoke i progress su odvojeni u
  `packages/gateway-protocol/src/schema/nodes.ts:40-191`.
- **OpenClaw je referenca, ne security floor:** njegov protokol još koristi `Unknown` payload mjesta i
  setup-code može predstavljati vrlo širok bootstrap pristup; Nexus ostaje typed, deny-default i
  operator-approved.
- **Naš floor:** najmanje OpenClawova multiplatform pairing/node ergonomija, plus proof-of-possession,
  credential epoch/revocation, S0 typed envelope, S6 policy/SSRF i S7 stale-result fence.

---

# Integracijski acceptance gate

Promjena je spremna za fold u `HARNESS-PLAN.md` tek kada jedan in-memory/SQLite contract harness dokaže:

1. BSP daje byte-identičan checkpoint pod permutiranim completion redoslijedom i ne otkriva same-step
   writeove.
2. Queue prođe claim/refresh/reclaim/DLQ crash matrix, uključujući `UNKNOWN_EFFECT` i stale fence.
3. Flow compiler proizvodi isti `GraphSpec` iz istog canonical manifest bytesa i nema shadow retry/
   checkpoint put.
4. Board journal replay je side-effect free, revision-CAS je single-winner, a `Done` traži neovisni
   evidence principal.
5. Fleet placement je deterministic i invalidira se na capability epoch promjenu.
6. Pairing replay/rotation/cross-profile/stale-result testovi svi fail-closed prolaze.

**Pareto-floor presuda:** PASS na dizajnu. Svaki graft zadržava dublji dokazani mehanizam izvora, ali
ne preuzima njihove slabije granice (`Any`, best-effort persistence, implicitnu autorizaciju ili drugi
executor). Najveći implementacijski rizik je transakcijska granica `journal event + projection +
checkpoint`; zato ona mora biti prvi storage contract test prije scheduler/UX implementacije.

PLAN S7

# S7 — Pouzdanost i durable execution

## Zajedničke odluke i paket-layout

- S7 je **jedini** vlasnik retryja, deadlinea, cancela, durable attempta, resource accountinga, queue leasea i recovery odluke. S2.2 samo normalizira provider grešku i predlaže fallback; S3.3 samo emitira user-cancel; S3.4 pretvara terminalni rezultat u observation. Nijedan provider, tool, loop, gateway ili delivery adapter nema vlastitu retry petlju.
- Predloženi layout: `internal/durable/{contract,attempt,budget,queue,recovery,outbox,timer}`. `contract` ne ovisi o adapterima; `attempt` izdaje jednokratne grantove; `budget` rezervira i knjiži; `queue` posjeduje lease/fencing semantiku; `recovery` folda P0.3 journal i planira nastavak; `outbox` odvaja result commit od dostave. Distributed backendi iz S18 implementiraju ista sučelja, ne mijenjaju stanja.
- `context.Context` je propagation mehanizam, ne policy owner. S7 iz durable `ExecutionPolicy` proizvodi root deadline/cancel cause i child contexte; adapter ne smije sam dodati širi deadline, retry ili “on timeout try again”. Kraći lokalni I/O timeout smije postojati samo kao dio S7 `AttemptGrant.ExpiresAt`, bez novog pokušaja.
- Goroutine i channel služe za in-process izvršavanje i wakeup/backpressure. Durable istina je SQLite/journal state + CAS; `sync.Mutex`, `sync/atomic` i zatvaranje channela nisu dokaz crash recoveryja niti cross-process ownershipa.
- Osnovni Go runtime pišemo sami radi single-binary cilja. Temporal, LangGraph, tenacity, Restate i Dapr služe kao semantički referenti i opcionalni service adapteri; vanjski workflow runtime nije uvjet za lokalni harness.

```go
package durable

type OperationID string
type AttemptID string
type FencingToken uint64

type ErrorClass uint8 // RETRYABLE|TERMINAL|CANCELLED|UNKNOWN_EFFECT|BUDGET_EXHAUSTED

type TypedFailure struct {
    Code            contracts.ErrorCode
    Class           ErrorClass
    Source          string
    TargetID        string
    RetryAfter      *time.Duration
    EffectCertainty contracts.EffectCertainty
    CauseRef        string
}

type Clock interface {
    Now() time.Time
    NewTimer(time.Duration) Timer
}

type Store interface {
    WithImmediate(context.Context, func(Tx) error) error
    Recover(context.Context) error
}
```

Globalne invarijante:

1. Svaki fizički provider/tool/delivery pokušaj mora potrošiti točno jedan S7 grant atomski prije I/O-a. Ponovna uporaba granta odbija se prije transporta.
2. Durable tranzicija prethodi vanjskom učinku kad god je učinak moguć; rezultat/receipt se durable zapisuje prije ACK-a. Timeout nije dokaz da učinak nije nastao.
3. Monotoni sequence/fencing token, ne wall clock, odlučuje koji worker smije commitati. Wall clock služi za expiry tek uz durable lease i CAS.
4. P0.3 `EventJournal.Append` ostaje jedini write-owner događaja. S7 state tablice su materialized operational state ažuriran u istoj SQLite transakciji s kanonskim journal eventom; ne emitiraju paralelnu historiju.
5. Sve goroutine koje S7 pokrene imaju owner, bounded queue, child budget i cancel/drain put. Run nije terminalan dok owned children nisu završili ili dokazano prešli u `UNKNOWN/QUARANTINED` recovery stanje.

## 7.1 Timeout/cancellation propagacija i retryable-vs-terminal taksonomija

- **Tip:** MEHANIZAM

- **Aspekti:** Temporal je najbolji referent za razdvojene attempt/workflow timeoutove, durable retry policy, heartbeat i cancellation; LangGraph daje graph-level failure/interrupt i checkpointed error flow; tenacity daje jasan `retry/stop/wait` kompozicijski model. Najbolji graft je Temporalova semantika i tenacityjeva deklarativnost, ali u našem malom Go scheduleru s jednim S7 ownerom i bez skrivene library retry dekoracije.

- **Sinteza:** `AttemptController` prima jednu `ExecutionPolicy`, klasificirani rezultat prethodnog pokušaja i aktualni durable state. Izdaje `AttemptGrant` samo legalnim prijelazom; physical boundary poziva `Consume` koji radi CAS `ISSUED→RUNNING`. Backoff je durable timer event, ne `time.Sleep`, pa restart ne resetira čekanje ni attempt counter.

```go
type ExecutionPolicy struct {
    OperationID      OperationID
    EffectClass      contracts.EffectClass
    Deadline         time.Time
    MaxAttempts      uint32
    AttemptTimeout   time.Duration
    Backoff          BackoffPolicy
    RetryableCodes   []contracts.ErrorCode
    FallbackTargets  []string
    IdempotencyKey   string
    CancelTokenID    string
}

type AttemptGrant struct {
    AttemptID     AttemptID
    OperationID   OperationID
    AttemptNo     uint32
    TargetID      string
    RequestDigest string
    IssuedAt      time.Time
    ExpiresAt     time.Time
    GrantNonce    string
    Fence         FencingToken
}

type AttemptState uint8 // PENDING|ISSUED|RUNNING|SUCCEEDED|FAILED_RETRYABLE|FAILED_TERMINAL|CANCELLED|UNKNOWN

type AttemptController interface {
    Start(context.Context, ExecutionPolicy, budget.Reservation) (AttemptGrant, error)
    Consume(context.Context, AttemptGrant) (context.Context, error)
    Complete(context.Context, AttemptGrant, AttemptResult) error
    Cancel(context.Context, OperationID, contracts.CancelCause) error
    Next(context.Context, OperationID) (AttemptGrant, error)
}
```

  Normativni tok:

```text
canonical request + ExecutionPolicy
  -> reserve child budget (7.2)
  -> durable PENDING -> ISSUED
  -> adapter boundary Consume(grant): CAS ISSUED -> RUNNING
  -> S7-created context(deadline, cancel-cause) propagira u svu djecu
  -> exactly one physical attempt
  -> durable terminal attempt result
  -> FAILED_RETRYABLE + attempts/deadline/budget available
       -> durable backoff timer -> re-authorize fallback through S6 -> next grant
     else -> operation terminal
```

  Invarijante:

  1. `MaxAttempts` broji sve targete i fallbackove iste operacije; fallback nije novi besplatni retry budget. `AttemptNo` strogo raste i nikad se ne vraća nakon restarta.
  2. Adapter vraća provider-native podatke, a 2.2 mapper ih jednom pretvara u `TypedFailure`. S7 klasificira po typed codeu/effect certaintyju, ne substringu poruke. Unknown code je terminalan dok policy eksplicitno ne kaže drukčije.
  3. User cancel prelazi operaciju u `CANCELLED`, fencea nove grantove, poziva cancel-cause root contexta te zahtijeva S6/process backendu drain→kill owned tree. `CANCELLED` je terminalan; kasni success starog attempta odbija fencing token.
  4. `IRREVERSIBLE + UNKNOWN_EFFECT` nikad ne dobiva sljedeći grant. Ide u P1.4 reconciliation/compensation; transport timeout nije retryable činjenica.
  5. Svaki fallback ponovno prolazi S6 policy, residency i credential scope prije granta. Denied fallback završava typed terminal errorom bez network poziva.
  6. Jitter se računa deterministički iz `OperationID+AttemptNo+policy digest` i sprema kao timer deadline, tako da replay ne bira novo vrijeme. `Retry-After` se clamp-a na preostali operation deadline.
  7. Child deadline je `min(parent deadline, grant expiry)`. Adapter može ranije vratiti, ali ne može stvoriti context koji nadživi grant niti progutati cancel i prijaviti success bez validnog fencea.

- **Salvage:** iz `/home/matej/NEXUSv2/core/recovery.py:16-42` Python→Go portirati početnu taksonomiju rate-limit/timeout/truncated/empty/invalid/fatal i ideju da samo poznato recoverable retrya; iz `core/circuit.py:38-75` portirati stabilizirani failure signature i `STAGNATION/FLIP_FLOP/NO_PROGRESS` kao dodatni stop-signal. **Ne portirati** `run_with_recovery()` (`recovery.py:45-65`) jer je upravo paralelni retry owner s `time.sleep`, lokalnim attempt counterom i string klasifikacijom. Reward statistika može predlagati buduću policy verziju, ali ne mijenja aktivni policy usred operacije.

- **Ugovor/RED:** Annex **P0.2** je kanonski ugovor. Obvezni `test_adapter_cannot_self_retry`: isti grant poslan dvaput mora dati `ATTEMPT_NOT_AUTHORIZED`, transport count=1 i S7 count=1. Dodatni RED-ovi: `test_cancel_fences_all_children_and_late_success`; `test_irreversible_unknown_never_retries`; `test_restart_preserves_attempt_count_and_backoff_deadline`; `test_fallback_reenters_data_policy_before_connect`; `test_deadline_is_minimum_across_parent_and_attempt`; `test_stagnation_stops_before_max_attempts_without_reclassifying_error`.

- **Verifikacija:** provjereno 2026-09-01. Temporalova službena [activity timeout dokumentacija](https://github.com/temporalio/documentation/blob/main/docs/develop/typescript/activities/timeouts.mdx) potvrđuje odvojene timeoute, heartbeat, retry policy i cancellation delivery; LangGraphova [fault-tolerance dokumentacija](https://docs.langchain.com/oss/python/langgraph/fault-tolerance) potvrđuje retry policy, checkpointed failure provenance i zasebnu interrupt semantiku; [tenacity](https://tenacity.readthedocs.io/en/stable/index.html) potvrđuje retry/stop/wait kompoziciju. Nijedan se ne ugrađuje kao drugi runtime owner. Lokalni NEXUS klasifikator i circuit stvarno postoje, ali nisu P0.2 compliant retry engine.

- **Pareto floor:** **PASS na ugovoru.** Zadržava Temporalovu durable semantiku, LangGraphov failure flow i tenacityjevu čitljivost, a uklanja najopasniji dupli-owner uzorak. Strongest counterexample je adapter koji interno koristi SDK automatske retryje; provider klijent mora ih konfigurirati na `max_attempts=1`, a transport-count parity test mora to dokazati. Najslabija točka je cancel nekancelabilnog syscalla/provider poziva; grant expiry fencea kasni rezultat, ali fizičko zaustavljanje ovisi o S6 backend capabilityju i mora biti označeno `ENFORCED|OBSERVED|UNAVAILABLE`.

## 7.2 Idempotency, izvršni bounded budgets, backpressure i queue lease/fencing

- **Tip:** MEHANIZAM

- **Aspekti:** Temporal daje idempotency/activity task i heartbeat mentalni model; Restate log-first journal, idempotent invocation i epoch fencing; Dapr durable retry/timer/workflow primitives. Najbolji graft je Restateov monotoni epoch/fence za stale workere, Temporalov lease/heartbeat obrazac i naš mali SQLite CAS scheduler s Annex P2.2 punim resource ledgerom.

- **Sinteza:** `BudgetManager` prije svake skupe operacije atomski rezervira child budget iz preostalog parenta. GORTEX/S6 OS backend mjeri i provodi CPU/RAM/disk/file/process/socket/network; provider adapter prijavljuje tokene/cijenu. S7 je jedini accounting/cancel owner i ne prihvaća adapterovo `enforced=true` bez verificiranog enforcement sourcea.

```go
type Limit struct { Value uint64; Mode LimitMode; Unit Unit } // SOFT|HARD
type ResourceBudget struct {
    BudgetID, ParentBudgetID string
    WallMS, CPUMS, RSSBytes, DiskWriteBytes, FileCount Limit
    ProcessCount, SocketCount, NetworkInBytes, NetworkOutBytes Limit
    OutputBytes, InputTokens, OutputTokens, CostMinorUnits Limit
    ToolCalls, LoopSteps Limit
}
type ResourceLedger struct {
    BudgetID, ParentBudgetID string
    Reserved, Consumed Usage
    ObservedAt time.Time
    Source EnforcementSource
    Fence FencingToken
    State BudgetState // AVAILABLE|RESERVED|CONSUMING|RELEASED|EXHAUSTED|CANCELLED
}
type BudgetManager interface {
    Reserve(context.Context, string, ResourceBudget) (Reservation, error)
    Consume(context.Context, Reservation, UsageDelta) (ResourceLedger, error)
    Release(context.Context, Reservation) error
}
```

  Counteri su monotoni; RSS/process/socket su peak gaugeovi. Pre-check se radi prije LLM/tool/embedding/browser operacije, a streaming usage se knjiži tijekom rada. HARD breach u jednoj SQLite transakciji postavlja `EXHAUSTED`, povećava fence i zabranjuje nove grantove; zatim 7.1 cancel/drain/kill. SOFT samo emitira signal. Child rezervacija nikad ne prelazi preostali parent.

  **Queue podmehanizam:** SQLite je source of truth; bounded Go channel je samo wakeup hint. Claim i reclaim su jedinstveni `UPDATE ... WHERE state/token/expiry` CAS unutar `BEGIN IMMEDIATE`. Svaki lease dobiva strogo veći fencing token iz durable per-queue sequencea.

```go
type QueueTask struct {
    TaskID, PayloadHash, IdempotencyKey string
    State QueueState // READY|LEASED|RUNNING|COMMIT_PENDING|SUCCEEDED|UNKNOWN_EFFECT|RECONCILING|FAILED|DEAD_LETTER|CANCELLED
    AttemptNo, MaxAttempts uint32
    LeaseID, OwnerWorkerID *string
    Fence FencingToken
    LeasedAt, HeartbeatAt, LeaseExpiresAt *time.Time
    ResultHash *string
}
type Queue interface {
    Enqueue(context.Context, TaskIntent) (QueueTask, error)
    Claim(context.Context, WorkerID, time.Duration) (QueueTask, error)
    Heartbeat(context.Context, LeaseRef) error
    BeginCommit(context.Context, LeaseRef, string) error
    CommitResult(context.Context, LeaseRef, ResultRef) error
    ReclaimExpired(context.Context, QueueID, uint32) ([]QueueTask, error)
}
```

  Queue invarijante:

  1. Idempotency key pripada intentu, ne attemptu. Isti key+isti payload vraća postojeći task/result; isti key+drugi payload daje `IDEMPOTENCY_KEY_REUSE`.
  2. Claim radi `READY→LEASED`, postavlja worker/lease/expiry i veći fence. Worker zatim durable prelazi `LEASED→RUNNING`. Heartbeat vrijedi samo za aktualni lease+token i ne može oživjeti terminalni task.
  3. Expired `LEASED|RUNNING` smije u `READY` samo atomskim reclaimom koji zatvara stari lease, povećava attempt+fence i poštuje max attempts. Stari worker nakon thaw/restarta ne smije heartbeatati, mijenjati state ni commitati rezultat.
  4. `COMMIT_PENDING` s mogućim vanjskim učinkom nikad se ne reclaim-a ravno u READY. Ide `UNKNOWN_EFFECT→RECONCILING` prema P1.4; ACK slijedi tek durable result commit.
  5. Backpressure je eksplicitan: queue depth, per-tenant concurrency, reserved-budget i worker slots odlučuju admission. Puni wake channel ne gubi task jer durable READY red ostaje; producer dobiva `QUEUED|BACKPRESSURE|REJECTED`, nikad blokadu bez deadlinea.
  6. In-process shared maps koriste mutex/atomic samo za cache; svaka autoritativna read-modify-write odluka ide kroz store CAS. Race detector GREEN nije zamjena za multi-process SQLite test.

- **Salvage:** iz `/home/matej/NEXUSv2/core/queue.py:16-68` portirati malu tablicu, durable enqueue i atomic pending→running single-winner namjeru. **Audit finding je potvrđen iz koda:** schema ima samo `pending/running/done/failed`, bez `lease_id`, heartbeat/expiry/fencing tokena; `drain()` ostavlja crashani `running` zauvijek, `_finish()` (`97-102`) nije token-conditional, a delivery grešku (`83-86`) guta nakon `done`. Zato se queue ne copy-pastea kao gotov mehanizam. Iz `core/fleet.py:57-77` portirati attempt-conditional CAS/heartbeat i iz `80-100` zaštitu od stale terminal/workflow writea, ali zamijeniti PID/time stale heuristiku durable leaseom i P1.2 identityjem. Iz `core/circuit.py:83-104` portirati cost ledger kao input; `over_budget()` nije P2.2 jer nema reservation, hard OS enforcement ni pune dimenzije.

- **Ugovor/RED:** Annex **P2.2** i **P2.3** su obvezni. Kanonski RED `test_hard_process_budget_kills_descendants`: limit 2, parent+2 children mora dati `RESOURCE_LIMIT_EXCEEDED`, ubiti owned tree, fenceati kasnije alate i ledger=`EXHAUSTED`. Kanonski RED `test_reclaimed_worker_cannot_commit_with_stale_fence`: worker A token 7 zamrznut preko expiryja, B token 8, A commit odbijen, točno jedan B rezultat. Dodatno: `test_child_budget_cannot_overreserve_parent`; `test_cost_checkpoint_denies_before_provider_call`; `test_same_idempotency_key_different_payload_rejected`; `test_crashed_running_task_is_reclaimed`; `test_commit_pending_unknown_never_requeues`; `test_full_wakeup_channel_does_not_drop_durable_task`; multi-process claim test + Go race detector.

- **Verifikacija:** Restateova službena [architecture dokumentacija](https://docs.restate.dev/references/architecture) potvrđuje log-first step commit, idempotency i monotone epoch fencing protiv starih pokušaja; Dapr službena [workflow dokumentacija](https://docs.dapr.io/developing-applications/building-blocks/workflow/workflow-features-concepts/) potvrđuje durable retry politike/timere preko restarta. Lokalni queue/fleet/circuit pregled potvrđuje i salvage vrijednost i opisanu lease/reclaim rupu. Temporal/Restate/Dapr ne postaju dependency baselinea.

- **Pareto floor:** **PASS tek nakon stvarnog process/resource i stale-worker gatea.** Sinteza zadržava jednostavnost NEXUS SQLite queuea, ali dodaje ono što audit dokazano nema: lease, heartbeat, reclaim i fence. Strongest counterexample je A koji se odmrzne nakon što je B završio; svaki state/result/outbox write mora nositi fence, ne samo claim. Najslabija točka su jednaki hard CPU/RAM/socket capovi na sva tri OS-a; unsupported hard dimension mora failati capability closure, ne pasti na telemetry-only.

## 7.3 Crash recovery, durable checkpoints i durable delivery

- **Tip:** MEHANIZAM

- **Aspekti:** LangGraph daje checkpoint po graph koraku, pending writes, history/replay i resume; Temporal daje event-history recovery i recorded activity results; OpenHands daje event-stream rekonstrukciju; Restate daje log-first durable completion. Za N9 je transactional outbox najbolji obrazac: result commit i outbox insert u jednoj transakciji, zatim at-least-once relay s idempotentnim receiptom.

- **Sinteza:** P0.3 journal je kanonska historija, a checkpoint je optimizacija s `source_offset`, schema/policy/capability digestima i refovima na spremljene nondeterminističke rezultate. Restart verificira checkpoint, replaya događaje nakon offseta, reconciliira P1.2 resurse/P2.3 leaseove i nastavlja samo legalno stanje. Model/tool/provider rezultat se ne poziva ponovno ako je njegov durable receipt već u historiji.

```go
type DurableCheckpoint struct {
    RunID               string
    SourceOffset        uint64
    StateDigest         string
    WorkspaceRevision   workspace.RevisionID
    SchemaVersion       uint32
    PolicyDigest        string
    CapabilityDigest    string
    ResultRefs          []ArtifactRef
    CreatedAt           time.Time
}

type RecoveryPlan struct {
    RunID, PlanDigest string
    FromOffset       uint64
    State            contracts.RunState
    Reconcile        []ReconcileIntent
    Resume           []OperationID
    Quarantine       []ResourceRef
}
```

  Replay je deterministički samo nad zabilježenim eventima/rezultatima i zamrznutim policy/schema/capability verzijama. Ako nedostaje vanjski receipt ili environment nije reproducibilan, stanje je `UNKNOWN/RECONCILING`; nema tvrdnje da ponovno pokretanje LLM-a ili API-ja daje isti rezultat.

  **N9 delivery podmehanizam:** izvršni i delivery status su dvije osi. `RUN_RESULT_COMMITTED` znači da je rezultat durable i run više ne izvršava business side-effect; ne znači da ga je kanal primio. U istoj P0.3/SQLite transakciji koja commita result event nastaje outbox red.

```go
type DeliveryState uint8 // PENDING|LEASED|SENDING|DELIVERED|UNKNOWN_DELIVERY|RECONCILING|DEAD_LETTER|CANCELLED
type OutboxEntry struct {
    DeliveryID, RunID, ResultHash, ChannelID string
    RecipientScopeHash, IdempotencyKey       string
    State                                    DeliveryState
    AttemptNo, MaxAttempts                   uint32
    LeaseID                                  *string
    Fence                                    FencingToken
    NextAttemptAt                            time.Time
    Receipt                                  *DeliveryReceipt
}
type DeliveryReceipt struct {
    DeliveryID, ProviderMessageID, ResultHash string
    ChannelID, RecipientScopeHash              string
    AcceptedAt                                 time.Time
    SignatureOrEvidence                        []byte
}
```

  Normativni delivery tok:

```text
business result verified
  -> one SQLite txn: append RUN_RESULT_COMMITTED + insert OUTBOX_PENDING(result hash)
  -> run execution axis = DONE; delivery axis = PENDING
  -> relay claims outbox via P2.3 lease/fence
  -> S7 issues one delivery AttemptGrant with stable idempotency key
  -> channel send
  -> receipt bound to delivery/result/recipient -> durable DELIVERED -> ACK lease

send timeout after bytes may have left host
  -> UNKNOWN_DELIVERY -> reconcile/query by idempotency key/provider message ID
  -> resend only if channel proves idempotent dedupe; otherwise human/manual recovery
```

  Invarijante:

  1. Result event i outbox row nastaju ili oba ili nijedan. To nije drugi event writer: `EventJournal.AppendWithOutbox` je P0.3-owned transakcijska metoda, a S7 šalje typed intent.
  2. Restart nakon `RUN_RESULT_COMMITTED` nikad ponovno ne pokreće agent/tool run samo zato što delivery nije potvrđen. Relay nastavlja iz outboxa.
  3. Delivery je najmanje at-least-once. Exactly-once user-visible ishod tvrdi se samo kad kanal prihvaća stabilni idempotency key ili nudi queryable receipt/dedup; bez toga timeout poslije senda ostaje `UNKNOWN_DELIVERY`, ne automatski resend ni `DELIVERED`.
  4. Receipt mora vezati `DeliveryID+ResultHash+ChannelID+RecipientScopeHash`; receipt za drugi payload/recipient ne zatvara entry. Stale relay fence ne može zapisati receipt.
  5. Dead-letter čuva rezultat i causal error; ne briše niti ponovno izvršava business run. Manual redrive stvara novi delivery attempt istog `DeliveryID/idempotency keya`, osim eksplicitno odobrene nove dostave.
  6. Recovery prvo fencea stare workere/leaseove, zatim reconciliira `UNKNOWN_EFFECT/UNKNOWN_DELIVERY`, pa tek onda izdaje nove grantove. Redoslijed se ne smije invertirati.

- **Salvage:** iz `/home/matej/NEXUSv2/core/resume.py:13-44` portirati baseline/per-iteration checkpoint, last-good izbor i restore-prije-redrive namjeru; **ne portirati** pretpostavku da filesystem restore čini vanjske učinke idempotentnima (`resume.py:6-7` to pošteno negira). Iz `core/fleet.py:1-13` portirati dvije statusne osi i attempt-conditional heartbeat; nova os je execution vs delivery, ne samo activity vs workflow. Iz `core/queue.py` portirati durable row/relay skelet, ali izričito ispraviti `drain()` redoslijed: sada `_finish(done)` prethodi dostavi, delivery exception se guta (`81-86`) i nema outbox/receipt, pa je N9 prekršen. `core/recovery.py` taksonomija i `core/circuit.py` stop-signali ulaze u plan, ne smiju sami redriveati.

- **Ugovor/RED:** P0.3 single writer, P0.2 attempt owner, P1.2 startup sweep, P1.4 unknown-effect i P2.3 lease/fencing zajedno su gate; N9 dodaje delivery acceptance. RED-ovi: `test_crash_after_result_commit_before_send_resumes_delivery_not_run`; `test_result_and_outbox_are_atomic_under_sigkill`; `test_send_succeeded_receipt_lost_becomes_unknown_not_blind_retry`; `test_duplicate_relay_with_same_idempotency_key_delivers_one_logical_result`; `test_wrong_result_or_recipient_receipt_rejected`; `test_stale_delivery_worker_cannot_commit_receipt`; `test_corrupt_checkpoint_falls_back_to_journal_without_reexecuting_recorded_effect`; `test_missing_environment_or_receipt_enters_reconciliation_not_deterministic_replay`.

- **Verifikacija:** LangGraphova službena [persistence dokumentacija](https://docs.langchain.com/oss/python/langgraph/persistence) potvrđuje per-step checkpoint, pending writes, history i replay, ali također kaže da koraci nakon checkpointa ponovno izvršavaju LLM/API pozive; zato ih naš journal mora spremiti ili označiti unknown. Restateova [request lifecycle dokumentacija](https://docs.restate.dev/guides/request-lifecycle) potvrđuje persist-before-execute, journal replay i idempotent cross-service invocation. [Transactional outbox obrazac](https://docs.aws.amazon.com/en_en/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html) potvrđuje atomic state+outbox zapis i potrebu idempotentnog consumer/delivery puta. Lokalni queue pregled izravno potvrđuje N9 kvar.

- **Pareto floor:** **PASS na state-machineu; uvjetan na channel receipt capabilityju.** Sinteza zadržava LangGraphovu preglednu historiju, Temporal/Restate durable rezultate i NEXUS jednostavni SQLite spine, ali razdvaja “gotov rad” od “primljena poruka”. Strongest counterexample je provider koji je prihvatio poruku, zatim prekinuo vezu bez query/dedupe API-ja; nijedan harness ne može znati ishod, pa je jedini korektan state `UNKNOWN_DELIVERY`. Najslabija točka je atomsko spajanje journala i outboxa: mora biti jedna stvarna SQLite transakcija, ne dva API poziva iza zajedničkog helpera.

## S7 acceptance gate

S7 je spreman tek kada jedan isti black-box harness dokaže sva četiri failure prozora: prije attempt I/O-a, tijekom effecta, između result/outbox commita i deliveryja te nakon senda prije receipta.

1. Controlled mutation koja adapteru vrati grant bez CAS consumea mora oboriti P0.2 test; nijedan SDK auto-retry ne smije ostati skriven.
2. P2.2 test mora koristiti stvarne descendant procese i OS enforcement; P2.3 mora koristiti dva procesa, pravi SQLite store, zamrznutog stale workera i monotone fenceove.
3. SIGKILL se injektira nakon svake durable tranzicije queuea, checkpointa i outboxa; restart završava legalnim stanjem bez dupliciranog business side-effecta ili izgubljenog rezultata.
4. Backpressure test puni wake channel i worker pool, ali svaki prihvaćeni durable task ostaje dohvatljiv; prekoračenje hard budgeta fencea budući rad prije cleanup-a.
5. Delivery adapteri objavljuju `IDEMPOTENT_SEND|QUERYABLE_RECEIPT|AT_LEAST_ONCE_ONLY`; acceptance oracle zahtijeva `DELIVERED` samo za dokazani receipt, a `AT_LEAST_ONCE_ONLY` ambiguity ostavlja `UNKNOWN_DELIVERY`.

**Proof ceiling:** ovaj plan definira buildable Go ugovore, vlasništvo i red-capable failure matricu te iz koda potvrđuje da NEXUS queue nema lease/reclaim/fencing i guta delivery failure. Ne dokazuje implementirani scheduler, stvarni 3-OS hard resource enforcement, crash consistency SQLite sheme ni exactly-once ponašanje vanjskih kanala. Te tvrdnje postaju `PROVEN` tek uz fault-injection, multi-process, controlled-mutation i receipt-capability testove na imenovanoj reviziji.

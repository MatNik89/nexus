PLAN S3

# S3 — Agentska petlja (Core Loop)

## Zajedničke odluke i paket-layout

- S3 je coding-kritična jezgra, ne još jedan generički workflow framework. Predloženi layout je `internal/loop/{contract,engine,reducer,turn,stream,recovery,verify,impact,trigger}`; adapteri modela ostaju u S2, alati u S4–S6, a svaki fizički pokušaj, deadline i cancel-token ostaju u S7.
- Kanonska hijerarhija je `Run -> Turn -> Step -> Action -> Observation`. `Turn` je jedan provider poziv i njegovo potpuno tool-settlement razdoblje; `Step` je jedna semantička granica unutar turna; `Action` je prijedlog efekta, a `Observation` jedini model-visible rezultat tog prijedloga. Plan i modelova završna proza jesu podaci, nikad stanje ni dokaz.
- S0 `EventJournal.Append` ostaje jedini trajni write-owner. S3 ima čisti reducer `state + durable event -> state + requested effects`; stream delte su prolazne i ne mijenjaju replay stanje. Izvršitelji efekata vraćaju događaje, ne mutiraju loop state.
- S7.1 je jedini owner retryja, backoffa, attempt timeouta, deadlinea i fizičkog cancela. S3.3 samo emitira zahtjev za cancel; S3.4 samo pretvara konačni pokušaj u model-readable `Observation`. “Model je pokušao ponovno” znači novi `ActionID`; “isti fizički pokušaj ponovno” zahtijeva novi S7 `AttemptGrant`.
- Coding dovršetak je dvofazan: model predlaže `FinishAction`, zatim jezgra prelazi u `VERIFYING`; tek 3.5 može emitirati `CompletionAccepted`. Deterministički RED ne može preglasati ni worker, ni checker, ni council. Ne postoji alternativni “final string -> success” put.
- Paralelni tool callovi prvo se svi trajno evidentiraju, zasebno autoriziraju i pokreću; turn se ne zatvara dok svaki prihvaćeni `ActionID` nema točno jednu terminalnu opservaciju. Rezultati se modelu izlažu determinističkim poretkom deklariranog `ActionIndex`, ne poretkom završetka.
- Coding profil ne uvodi drugu petlju: aktivira verificatore, TIA, code-edit/LSP adaptere, app-contract i strože promotion kriterije kroz S0.5 closure. Isti reducer radi i za ostale profile.

Minimalni zajednički ugovor:

```go
package loop

type RunID string
type TurnID string
type StepID string
type ActionID string

type RunPhase uint8
const (
    RunAdmitted RunPhase = iota + 1
    RunActive
    RunVerifying
    RunSucceeded
    RunFailed
    RunCancelled
    RunPaused
    RunUnknown
)

type ActionKind uint8 // MODEL_CALL|TOOL_CALL|VERIFY|FINISH|RECONCILE

type Action struct {
    Envelope   contracts.Envelope
    ID         ActionID
    TurnID     TurnID
    StepID     StepID
    Index      uint32
    Kind       ActionKind
    TargetID   string
    Input      []contracts.ContextBlock
    Effect     contracts.EffectClass
    Operation  contracts.ExecutionPolicy
}

type ObservationStatus uint8 // SUCCEEDED|FAILED|DENIED|CANCELLED|UNKNOWN

type Observation struct {
    Envelope       contracts.Envelope
    ActionID       ActionID
    AttemptNo      uint32
    Status         ObservationStatus
    Output         []contracts.ContextBlock
    Error          *contracts.TypedError
    EffectCertainty contracts.EffectCertainty
    EvidenceRefs   []string
}

type Event struct {
    Envelope contracts.Envelope
    Kind     EventKind
    Payload  json.RawMessage
}

type EffectRequest struct {
    Action Action
    Kind   EffectKind
}

type Reducer interface {
    Apply(State, Event) (State, []EffectRequest, *contracts.TypedError)
}

type Engine interface {
    Admit(context.Context, contracts.Envelope) (RunID, *contracts.TypedError)
    Dispatch(context.Context, RunID) *contracts.TypedError
    AppendExternal(context.Context, Event) *contracts.TypedError
    State(context.Context, RunID) (State, error)
}
```

Normativni coding put:

```text
admitted goal -> provider turn -> complete Action event(s)
  -> capability/policy/approval -> S7 AttemptGrant -> one physical execution
  -> exactly one terminal Observation per Action -> next provider turn
  -> FinishAction -> VERIFYING -> diff-derived VerificationPlan
  -> diagnostics + TIA + required gates -> evidence grade
  -> CompletionAccepted | VerificationObservation -> repair turn | terminal failure
```

## 3.1 Plan-act-observe petlja

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Kilo Code | OpenHands | OpenCode | Najbolji izvor i graft |
|---|---|---|---|---|
| Action/Observation domena | V2 trajno projicira potpune tool callove i typed settlement | Eksplicitni `ActionEvent` i korelirani `ObservationEvent` prvorazredni su API | `ToolPart` prolazi `pending/running/completed/error` | **OpenHands** za javni domenski par; dodati Kilo settlement disciplinu i naš S0 envelope. |
| Event-stream i replay granica | V2 razlikuje durable Session stream od live-only text/reasoning/tool-input fragmenata | Event log nosi conversation događaje; callback/token lane daje live tok | Processor mapira stream `start/delta/tool-call/tool-result/tool-error/finish` u session partove | **Kilo V2** za durable/ephemeral rez; **OpenCode** za širinu normaliziranih provider stream eventa. |
| Turn/tool razdvajanje | Jedan `llm.stream` po provider turnu; complete call se trajno projicira prije eager child starta; svi started tool fiberi se drainaju prije nastavka | Agent korak emitira akciju, executor naknadno emitira opservaciju | Processor ima zasebne lifecycle partove i deferred settlement | **Kilo V2**; dodatno naš `Run/Turn/Step/Action` ugovor uklanja implicitne petlje. |
| Paralelni alati | Assistant message ID ulazi u settlement jer provider call ID smije kolidirati između turnova | Korelacija observationa s action ID-em | Deferred po tool-call ID-u, ali cleanup može rano označiti spor alat prekinutim | **Kilo V2**, uz našu zabranu time-based “pretpostavi aborted” settlementa. |
| Error kao podatak | Tool failure je typed tool-result error | Observation/AgentError je dio event povijesti | `tool-error` završava part u error stanju | **OpenHands** za semantiku korekcije; naš 3.4 određuje fatalnu granicu i effect certainty. |
| Coding workspace dokaz | V2 pre-captures workspace/session granice, ali completion nije dokaz prihvata | Akcije i opservacije su auditabilne, ali model-final nije artifact-grade gate | Snapshot prije streama i patch nakon step-a | **OpenCode** snapshot/diff ideja + **NEXUS evidence-graded loop** kao autoritativni completion gate. |
| Stuck/no-progress | Bounded provider turns; nije evidence-progress semantika | Ugrađen detector za ponavljanje action-observation/error/monologue/alternation | Doom-loop traži dopuštenje nakon jednakih tool+input poziva | **OpenHands** pattern širina + **NEXUS** durable `STAGNATION=3`, `NO_PROGRESS=5`; napredak se računa iz artefakta i dokaza, ne teksta. |
| Verify-loop | Nema javno dokazanu, neovisnu artifact-grade acceptance petlju | Nema javno dokazanu petlju koja worker final veže uz git diff + exit code | Snapshot/patch je vidljivost, ne dokaz correctnessa | **NEXUS loop_engine/runtime**; 3.5 odvaja deterministic gate od semantičkog reviewera. |

- **Sinteza — reducer i lifecycle:** `Engine.Dispatch` je mali dispatcher nad čistim reducerom, ne `while true` koji izravno zove providere i alate. Svaki vanjski efekt nastaje kao `EffectRequest`, a rezultat se vraća kao validirani S0 događaj.

```go
type State struct {
    RunID              RunID
    Phase              RunPhase
    Revision           uint64
    ActiveTurn         *TurnID
    Turns              map[TurnID]TurnState
    Actions            map[ActionID]ActionState
    WorkspaceBase      WorkspaceRevision
    WorkspaceCurrent   WorkspaceRevision
    Criteria           []Criterion
    Progress           ProgressState
    PendingVerification *VerificationPlanID
}

type ActionState struct {
    Action      Action
    Phase       ActionPhase // PROPOSED|ADMITTED|AUTHORIZED|RUNNING|TERMINAL
    Observation *Observation
}

func Reduce(s State, e Event) (State, []EffectRequest, *contracts.TypedError) {
    // total switch po poznatom EventKindu; bez I/O-a, sata, RNG-a ili goroutinea
    // legalni prijelazi delegiraju se S0.2 machineu
}
```

  Obvezne invarijante:

  1. Model response prvo završava u potpunim, schema-valid `Action` objektima. Fragment tool-inputa nikad se ne izvršava. Za paralelni batch svi action događaji moraju biti durable prije pokretanja prvog childa.
  2. `ActionID` je globalno jedinstven unutar runa; provider-local call ID je samo metadata. Terminalni `Observation` mora referencirati postojeći `ActionID+AttemptNo`; drugi terminalni rezultat je idempotentni exact replay ili `DUPLICATE_TERMINAL_OBSERVATION`.
  3. `FinishAction` ne završava run. Reducer ga pretvara u `VerificationRequested`, zaključava točan workspace revision i čeka 3.5. Samo `CompletionAccepted` nad tim revision hashom smije prijeći u `RunSucceeded`.
  4. Model-visible kontekst za idući turn sastavlja se iz durable eventa do zatvorene prethodne granice. Live delte, nepotpuni tool input, orphan rezultat i budući paralelni rezultat ne ulaze u prompt.
  5. Eager paralelno izvršavanje dopušteno je tek nakon pojedinačnog S6/S7 odobrenja. Nakon zatvaranja provider streama engine drain-a sve pokrenute childove; cancel signalizira S7, ali ne izmišlja rezultat nakon fiksnog grace perioda.
  6. Plan je opcionalni `ContextBlock(kind=PLAN)`. Njegova izmjena ne dokazuje napredak i ne otključava side-effect. CodeAct kandidat smije se ponuditi samo kao S4 alat čiji se generirani kod izvršava kroz S6/S7; ne dobiva skriveni interpreter u loop kernelu.
  7. `ProgressState` napreduje samo ako se promijeni kanonski workspace revision i/ili se stekne novi nezavisan dokaz/kriterij. Rephrasing, druga proza, ponovljeni read i besmisleni diff ne resetiraju breaker.

- **Sinteza — evidence-graded završetak i anti-sycophancy:**

```go
type FinishProposal struct {
    ActionID        ActionID
    ClaimedSummary  string
    ClaimedFiles    []string // display-only, nikad source of truth
    ClaimedChecks   []string // display-only
}

type CompletionDecision struct {
    RevisionHash       string
    DeterministicGates []GateResult
    SemanticCriteria   []CriterionResult
    ReviewerID         *string
    Accepted           bool
}

func DecideCompletion(b EvidenceBundle, semantic []CriterionResult) CompletionDecision {
    // Svaki required deterministic gate mora biti GREEN.
    // Semantički reviewer smije FAIL/UNCLEAR, nikad RED pretvoriti u GREEN.
}
```

  Harness sam računa diff od `WorkspaceBase` do zaključanog `WorkspaceCurrent`, stvarni write-set, exit status i evidence digest. Ne vjeruje `ClaimedFiles`, `verification_status`, sažetku ni checkerovoj riječi “passed”. S16.6 se veže kao neovisni promotion/checker sloj: reviewer radi nad zapečaćenim revisionom i evidence bundleom, a promjena artefakta automatski poništava odluku.

- **Salvage:** Python→Go port jezgre ideje iz `/home/matej/NEXUSv2/loop_engine.py:1-18,47-93,187-245,288-310,328-391`: zasebni doer/checker, harness-owned stvarni verify exit, bounded loop, kriteriji i circuit feedback. Portirati stvarni runtime evidence merge iz `/home/matej/NEXUSv2/runtime.py:758-825` te unutarnji JSON action/observation tok i runtime-owned tool rezultat iz `/home/matej/NEXUSv2/llm/executor.py:1830-1965,2084-2170,2230-2471`. Ne portirati worker-declared changed-file list i `head` prvih pet datoteka (`loop_engine.py:288-307`), leksički mutation-verb gate (`312-325`), modelov `verification_status==blocked` kao autoritet (`173-185`), ni dvije ugniježđene `while` petlje kao odvojene ownere. Go engine računa worktree snapshot/diff, a `Blocked` može emitirati samo policy/approval/tool boundary.

- **Ugovor/RED:** Annex **P0.1** zatvara envelope/provenance, **P0.2** zatvara retry/cancel owner, a **P0.3** čini journal jedinim write putem. Obvezni Annex RED je `test_adapter_cannot_self_retry`. Lokalni RED-ovi: `test_finish_claim_cannot_skip_verification`, `test_worker_declared_files_cannot_hide_real_diff`, `test_parallel_actions_each_get_one_terminal_observation`, `test_duplicate_provider_call_id_across_turns_does_not_alias`, `test_partial_tool_arguments_never_execute`, `test_checker_cannot_turn_exit_one_green`, `test_model_blocked_claim_cannot_park_run`, `test_replay_performs_zero_external_calls`.

- **Verifikacija:** provjereno 2026-09-01 na primarnim izvorima i lokalnim službenim snapshotima. [Kilo V2 Session spec](https://github.com/Kilo-Org/kilocode/blob/main/specs/v2/session.md) stvarno odvaja prompt admission od izvršenja, radi jedan `llm.stream` po provider turnu, trajno projicira potpune tool callove prije izvršenja, drain-a started tool fiber-e i odvaja live fragmente od durable streama. Lokalni Kilo snapshot je commit `3cb82a0907f888749435c1d208e56d8365747df2`. [OpenHands LocalConversation](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/conversation/impl/local_conversation.py) i [stuck detector](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/conversation/stuck_detector.py) stvarno koriste Action/Observation događaje i detektiraju ponavljajuće action-observation/error, monologue i alternating obrasce. [OpenCode processor](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/session/processor.ts) stvarno ima typed tool lifecycle, snapshot prije streama te stream evente za tool result/error i step granice. Nije verificirano da ijedan od ta tri projekta ima NEXUS-ov artifact-grade `diff + stvarni exit + independent criterion` completion gate; zato se to vodi kao naš diferencijator, ne kao graftana tvrdnja.

- **Pareto floor:** **PASS za 3.1.** OpenHandsov Action/Observation model, Kilo V2 durable settlement i OpenCode typed stream/snapshot aspekti ostaju, ali completion autoritet je stroži: artefakt i izvršni dokaz pobjeđuju prozu. Strongest counterexample je paralelni spor alat nakon zatvaranja streama; floor zahtijeva drain ili eksplicitni S7 cancel+terminal rezultat, nikad timeout-pretpostavku. Najslabija točka je semantička kvaliteta criteria checkera; ona ostaje fail/unclear-only i mora proći S16.6, pa ne može kompromitirati deterministic floor.

## 3.2 Streaming i inkrementalni prikaz

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Typed lifecycle i delta događaji | Codex app-server/protocol | `turn/started`, `item/started`, nula ili više delti, `item/completed`, `turn/completed` mapirati na S0 envelope. |
| Provider stream normalizacija | OpenCode processor / Gemini CLI | Text/reasoning/tool-input/tool-call/result/error/finish normalizirati u zatvoren skup kanala. |
| Durable nasuprot live-only | Kilo V2 / OpenHands token callback | Durable semantičke granice imaju replay cursor; prolazne delte ne troše journal offset. |
| Cancel aktivnih i queued toolova | Gemini CLI / Codex | Abort se propagira, ali terminalno stanje potvrđuje S7 settlement; UI signal nije dokaz da je side-effect stao. |
| Reconnect i backpressure | Kilo V2 | UI može izgubiti/coalesceati delte i rekonstruirati finalni sadržaj iz durable događaja. |

- **Sinteza:** postoje dvije eksplicitne trake. `DeltaHub` je bounded, best-effort i bez durable autoriteta; `EventJournal` sadrži samo stabilne granice. Spori renderer nikad ne smije usporiti model/tool executor.

```go
type StreamChannel uint8 // TEXT|REASONING_SUMMARY|TOOL_INPUT|TOOL_PROGRESS|USAGE

type Delta struct {
    RunID      RunID
    TurnID     TurnID
    ItemID     string
    Channel    StreamChannel
    DeltaSeq   uint64
    Bytes      []byte
    FinalHash  *string
}

type SubscriptionPolicy struct {
    Capacity        uint32
    Overflow        OverflowPolicy // COALESCE_TEXT|DROP_PROGRESS|DISCONNECT
    IncludeChannels []StreamChannel
}

type DeltaHub interface {
    Publish(Delta) PublishResult
    Subscribe(context.Context, RunID, SubscriptionPolicy) (<-chan Delta, func())
}

type StreamAssembler interface {
    Apply(Delta) *contracts.TypedError
    Seal(itemID string, durableFinal []contracts.ContextBlock) *contracts.TypedError
}
```

  Pravila: `(RunID,TurnID,ItemID,Channel,DeltaSeq)` je monotono po kanalu; duplicate exact delta je no-op, rupa/konflikt označava live prikaz desinkroniziranim. `Seal` provjerava final hash, a durable finalni item uvijek zamjenjuje live akumulator. Reasoning kanal se ne persistira niti izvozi ako provider/policy dopušta samo summary. Bounded subscriber radi coalescing texta i drop progressa; nikad ne odbacuje terminalni durable događaj jer on ne prolazi kroz `DeltaHub`. Mid-stream error zatvara item kao typed error/aborted final, ne kao uspješnu parcijalnu poruku.

- **Salvage:** Python→Go port modela odvojenih tracer/session događaja iz `/home/matej/NEXUSv2/runtime.py:1110-1142` i tool/transcript granica iz `/home/matej/NEXUSv2/llm/executor.py:2230-2471`. Ne portirati string-list `convo` kao stream state niti izravne best-effort zapise iz executora; svi durable događaji idu P0.3 journalom. NEXUS nema dovoljno jasan delta/reconnect ABI, pa je većina 3.2 vlastiti Go kod.

- **Ugovor/RED:** Annex **P0.1** za event identity, **P0.2** za mid-stream cancel i **P0.3** za durable write owner. RED-ovi: `test_slow_ui_cannot_block_tool_settlement`, `test_dropped_deltas_reconcile_to_durable_final`, `test_partial_tool_input_never_dispatches`, `test_cancelled_stream_cannot_emit_success_final`, `test_reasoning_channel_obeys_export_policy`, `test_delta_cannot_allocate_journal_sequence`.

- **Verifikacija:** [Codex app-server protocol](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md) stvarno izlaže turn/item lifecycle, inkrementalne delte, interrupt i turn-level diff; [Codex protocol v1](https://github.com/openai/codex/blob/main/codex-rs/docs/protocol_v1.md) potvrđuje SSE turn i `Op::Interrupt`. [Gemini CLI stream hook](https://github.com/google-gemini/gemini-cli/blob/main/packages/cli/src/ui/hooks/useGeminiStream.ts) stvarno aborta aktivne async operacije i otkazuje pending tool callove. Kilo V2 izričito kaže da live fragmenti nisu u replayable streamu. Verificirana je API semantika, ne hard claim da su svi njihovi raceovi riješeni.

- **Pareto floor:** **PASS za 3.2.** Zadržani su Codex typed lifecycle, Gemini cancel propagation i Kilo durable/live rez. Naš bounded dual-lane dizajn dodaje dokaziv backpressure i reconnect invariant bez trajnog zapisivanja svakog tokena. Najslabija točka je provider koji nakon disconnecta nastavi računati/raditi; S7 stanje ostaje `CANCELLING/UNKNOWN` dok transport ne potvrdi završetak i sustav ne laže da je otkazivanje fizički dovršeno.

## 3.3 Prekid, cancel i turn management

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Mid-turn interrupt s nastavkom | Codex | Precizan `(thread/run, turn)` target; završni status `interrupted`; novi turn koristi potvrđenu povijesnu granicu. |
| Drain aktivnog ownership lanca | Kilo V2 | Interrupt čeka cleanup/settlement i suppressa već queued rerun, ali durable input ostaje za fresh wake. |
| Pause/resume događaj | OpenHands | Pause je eksplicitno stanje conversationa, ne exception koji se izgubi. |
| Tool queue cancel | Gemini CLI | Aktivni abort signal plus čišćenje pending tool queuea. |

- **Sinteza:** user/policy cancel je command, ne izravna mutacija i ne `context.CancelFunc` spremljen u stanje.

```go
type CancelScope uint8 // RUN|TURN|ACTION
type CancelRequest struct {
    Envelope contracts.Envelope
    Scope    CancelScope
    TargetID string
    Reason   contracts.TypedError
}

type TurnPhase uint8 // CREATED|STREAMING|SETTLING|VERIFYING|COMPLETED|FAILED|CANCEL_REQUESTED|CANCELLING|CANCELLED

type TurnManager interface {
    RequestCancel(context.Context, CancelRequest) *contracts.TypedError
    RequestPause(context.Context, RunID, string) *contracts.TypedError
    Resume(context.Context, RunID, ResumePolicy) (TurnID, *contracts.TypedError)
}
```

  `RequestCancel` append-a `CancelRequested` i šalje S7 `cancel_token_id`; S7 propagira u provider, proc tree i child grants. S3 čeka svaki child u `SUCCEEDED|FAILED_*|CANCELLED|UNKNOWN`, zatim emitira `TurnCancelled`. `PAUSED` je dopušten samo na durable boundaryju bez živog neobrađenog childa; zahtjev usred efekta prvo prolazi cancel/drain. Resume ostaje isti `RunID`, dobiva novi `TurnID` i eksplicitni `ResumePolicy` koji pin-a config/capability/workspace revision ili traži migraciju. Parcijalni text završava kao `AbortedItem` s digestom, ne kao assistant final. Queue/steer inputi imaju zasebne S0 evente i ne mogu potiho preuzeti aktivni turn.

- **Salvage:** Python→Go port resume/CAS i stale-gate cancel ideje iz `/home/matej/NEXUSv2/runtime.py:1176-1205` te bounded cleanup namjere iz `/home/matej/NEXUSv2/llm/executor.py:1777-1830`. Ne portirati best-effort `except: pass` kao dokaz cleanup-a niti modelov `blocked` output kao pause command. OS process-tree detalji pripadaju S1.2/S7.3.

- **Ugovor/RED:** Annex **P0.2** je glavni gate: S3 emitira cancel, S7 ga izvršava; `CANCELLED` je terminalan. RED-ovi: `test_cancel_propagates_and_closes_children`, `test_pause_waits_for_terminal_child_settlement`, `test_resume_never_replays_unknown_effect`, `test_late_tool_result_after_cancel_cannot_reopen_turn`, `test_cancelled_partial_text_is_not_assistant_final`, `test_wrong_turn_interrupt_is_rejected`.

- **Verifikacija:** Codexovi službeni [app-server docs](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md) potvrđuju `turn/interrupt` i terminalni `interrupted`; Kilo V2 potvrđuje interrupt/drain/suppression ugovor. OpenHands SDK `LocalConversation` ima cancellation token i pause/interrupt lifecycle, a Gemini hook pokazuje aktivni abort plus pending-tool cancel. Nijedan kandidat sam ne dokazuje fizičko gašenje svih provider side-effecta; zato `UNKNOWN` ostaje legalan i zahtijeva reconciliation.

- **Pareto floor:** **PASS za 3.3.** Codex targetirani interrupt, Kilo drain i OpenHands pause ostaju, uz jači invariant da `CANCELLED` nije emitiran dok childovi nisu terminalni. Cijena je sporiji vidljivi cancel kada provider ne potvrdi prekid; to je namjerna epistemička granica, ne razlog za lažni terminalni status.

## 3.4 Error recovery unutar petlje (alat pao ≠ petlja pala)

- **Tip:** MEHANIZAM

- **Aspekti:**

| Aspekt | Kilo Code | OpenHands | OpenCode | Najbolji izvor i graft |
|---|---|---|---|---|
| Tool failure ne ruši petlju | Typed tool-result error nakon settlementa | Error observation vraća se agentu kao event | `tool-error` završava tool part | **OpenHands** error-as-observation + Kilo typed settlement. |
| Provider/loop fatalna granica | V2 ostavlja terminal failure nakon bounded overflow recovery | `AgentErrorEvent` razlikuje agent failure | Processor mapira provider error na message error | **OpenHands/OpenCode**, ali naš taxonomy strogo odvaja repairable domain error od kernel corruptiona. |
| Crash/resume | Running tool iz prethodnog procesa označi interrupted i ne replaya side effect | Persistirani event/state omogućuju resume | Snapshot/part lifecycle pomaže dijagnozi | **Kilo V2** za non-replay, **LangGraph** samo za checkpoint obrazac; naš `UNKNOWN+reconcile` je stroži. |
| Stuck detection | Bounded turn cap | Više ponavljajućih obrazaca | Doom-loop identical tool/input | **OpenHands širina + NEXUS normalized signature**, ali reset samo na dokazani napredak. |
| Retry ownership | Lokalni overflow recovery postoji | Framework može imati lokalna ponašanja | Processor koristi vlastitu retry policy | **Annex P0.2**, ne kandidat: samo S7 smije ponoviti fizički pokušaj. |

- **Sinteza — taxonomy i opservacija:**

```go
type FailureClass uint8
const (
    FailureDomain FailureClass = iota + 1 // model smije korigirati novom akcijom
    FailurePolicy                          // model vidi denial; ne smije zaobići gate
    FailureTransient                       // S7 odlučuje o novom grantu
    FailureUnknownEffect                   // reconcile prije bilo čega
    FailureInvariant                       // terminal/safe-pause, ne šalje se modelu kao repair task
    FailureSecurity                        // terminal/quarantine
)

type RecoveryDisposition uint8 // OBSERVE|WAIT_S7|RECONCILE|PAUSE|TERMINATE

type RecoveryDecision struct {
    Class       FailureClass
    Disposition RecoveryDisposition
    Observation *Observation
    Signature   FailureSignature
}

type RecoveryClassifier interface {
    Classify(Action, contracts.TypedError, contracts.EffectCertainty) RecoveryDecision
}
```

  Pravila:

  1. Tool compile/lint/path/schema/policy failure nakon završetka jednog pokušaja postaje točno jedna terminalna `Observation`; model smije predložiti novi `ActionID`. Exception ne smije preskočiti event boundary i srušiti cijelu petlju.
  2. Provider/tool transient error s `FAILED_RETRYABLE` ne stvara S3 retry. Engine ga prosljeđuje S7; ako S7 izda novi grant, isti `OperationID` dobiva novi `AttemptNo`. Tek finalna iscrpljena greška ide modelu.
  3. Journal corruption, illegal transition, invalid signature/provenance, sandbox escape i secret leak nisu “feedback modelu”. Run ide `PAUSED/FAILED/UNKNOWN`, sinkovi se gase, a operator dobiva sigurnu dijagnostiku.
  4. `IRREVERSIBLE+UNKNOWN` obvezno stvara `ReconcileAction`; nema automatskog replaya ni “pretpostavi da nije uspjelo”. Checkpoint obnavlja reducer state, ne izvršava ponovno vanjski čvor.
  5. Failure observation ima bounded safe diagnostics, machine code, origin, retryability i effect certainty. Raw attacker-controlled stderr je `UNTRUSTED_EXTERNAL ContextBlock`, nikad instrukcija.

- **Sinteza — stuck/no-progress:** current string normalization postaje strukturirani signature nad stvarnim stanjem.

```go
type FailureSignature struct {
    Code          string
    ActionKind    ActionKind
    TargetID      string
    WorkspaceHash string
    EvidenceGaps  []string
}

type ProgressProof struct {
    BeforeRevision string
    AfterRevision  string
    NewGateGreens  []string
    ClearedFailures []string
}

type StuckPolicy struct {
    SameSignatureLimit uint32 // default 3
    NoProgressLimit    uint32 // default 5
    DetectAlternation  bool
}
```

  Defaulti portaju NEXUS `STAGNATION=3` i `NO_PROGRESS=5`, ali “success” ih ne resetira sam po sebi. Reset zahtijeva valjan `ProgressProof`: nova revizija s relevantnim diffom, novi deterministic gate green ili zatvoreni criterion. A/B/A/B na istom revisionu i istim evidence gapovima je `FLIP_FLOP`. Isti test nad istim revision hashom ne stvara novu informaciju i ne dobiva novi model turn; S7 može dati novi pokušaj samo po vlastitoj politici, a breaker ga i dalje broji.

- **Salvage:** Python→Go port error taxonomy/fixtures iz `/home/matej/NEXUSv2/core/recovery.py:15-42`, ali ne `run_with_recovery()` (`45-65`) jer sadrži vlastiti retry/sleep i krši P0.2. Portirati noise-normalized stagnation, flip-flop i durable thresholds iz `/home/matej/NEXUSv2/core/circuit.py:1-80`, ali signature zamijeniti strukturiranim poljima gdje postoje; spend meter ostaje S2.5. Portirati model-visible `OBSERVATION: TOOL ERROR/POLICY/STUTTER` ponašanje iz `/home/matej/NEXUSv2/llm/executor.py:2084-2170,2230-2471`, ne string prefikse kao state machine. Checkpoint/resume ideje iz `/home/matej/NEXUSv2/runtime.py:1176-1205` ostaju, ali svaki unknown side-effect traži reconciliation.

- **Ugovor/RED:** Annex **P0.2** obvezno zabranjuje lokalni retry; **P0.3** osigurava da failure i observation dijele kanonsku povijest. Obvezni `test_adapter_cannot_self_retry`. Lokalni RED-ovi: `test_tool_error_becomes_observation_not_loop_crash`, `test_s3_cannot_issue_second_attempt_grant`, `test_irreversible_unknown_requires_reconciliation`, `test_invariant_error_is_not_sent_as_model_repair_prompt`, `test_rephrased_same_failure_hits_stagnation`, `test_meaningless_diff_does_not_reset_no_progress`, `test_alternating_failures_trip_flip_flop`, `test_resume_does_not_replay_terminal_or_unknown_action`.

- **Verifikacija:** OpenHandsovi službeni SDK izvori potvrđuju Action/Observation korelaciju i zasebne stuck obrasce; Kilo V2 potvrđuje typed tool-result errors i da se running tool nakon crasha označi interrupted bez silent replaya; OpenCode processor potvrđuje `tool-error` lifecycle. [LangGraph persistence](https://docs.langchain.com/oss/python/langgraph/persistence) stvarno checkpointa state i pending writes, ali njegova dokumentacija izričito kaže da se čvorovi nakon odabranog checkpointa ponovno izvršavaju, uključujući LLM/API pozive. Zato LangGraph nije dovoljan dokaz exactly-once side-effecta i grafta se samo checkpoint obrazac.

- **Pareto floor:** **PASS za 3.4.** Error-as-observation i crash recovery ostaju, dok P0.2 uklanja skrivene duple pokušaje. Najjači counterargument je da centralni S7 produžuje put za bezazleni parse retry; to je prihvatljiva cijena jer i malformed odgovor može biti billable pokušaj, a jedinstveni attempt ledger je jedini način da budget/cancel/idempotency ostanu istiniti. Najslabija točka je klasifikacija effect certainty na nedokumentiranim alatima; takav adapter mora završiti `UNKNOWN`, ne optimistično `FAILED`.

## 3.5 Deterministička verifikacija u petlji (lint/compile feedback + TIA)

- **Tip:** MEHANIZAM; language/build/TIA collector-i su ADAPTERI coding profila

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Post-edit dijagnostika u istom turnu | Aider + NEXUS executor | Nakon svake prihvaćene code mutacije pokrenuti bounded parse/compile/LSP dijagnostiku i vratiti je kao observation prije `FinishAction`. |
| Workspace diff/provenance | OpenCode snapshot + NEXUS runtime | Harness sam računa base→current diff i write-set; modelov popis datoteka nije dokaz. |
| Check-and-reprompt | Aider | RED dijagnostika vraća se modelu kao typed verification observation; completion se odbija dok obvezni gate nije green. |
| Test-impact analiza | NEXUS TIA + pytest-testmon | `coverage/dependency map x git diff -> impacted tests`, uz fail-closed full-suite fallback. |
| Širi dependency graph | Bazel affected-targets obrazac | Adapter smije koristiti build graph kada je deklariran i freshness verificiran. |
| Evidence grade | NEXUS loop_engine/runtime | Točan argv/cwd/exit/test counts/revision/digesti dolaze iz executor-a; worker i checker proza nisu autoritet. |
| Anti-sycophancy/promotion | NEXUS + S16.6 | Deterministički gateovi autoritativni; nezavisni reviewer može fail/unclear, nikad override RED-a. |
| Oracle integritet | NEXUS `protected_test_path` + S6 | Promjena testa/configa/verifiera degradira nezavisnost i traži base-pinned ili vanjski oracle. |

  Eksplicitni floor prema tri coding referenta:

| Coding-loop aspekt | Kilo Code | OpenHands | OpenCode | Naša donja granica |
|---|---|---|---|---|
| Što se provjerava nakon edita | V2 čuva tool settlement i session/workspace granicu; dostupni coding/LSP alati nisu isto što i obvezni automatski acceptance gate | Test/lint se može izvesti kao agent action i rezultat vratiti kao observation; javni SDK core ne čini taj rezultat univerzalnim completion autoritetom | Pre/post snapshot i patch daju stvarni prikaz promjene; processor sam po sebi ne dokazuje correctness | Stvarni diff automatski određuje plan; brza dijagnostika je obvezna nakon code mutacije, a required gate prije completiona. |
| Kako RED ulazi u petlju | Typed tool-result error može otvoriti sljedeći provider turn | Neuspjeh je observation koju agent vidi i može korigirati | `tool-error`/command result ostaje typed session part | Svaki verifier RED je `VerificationObservation` nad točnim revisionom; nema acceptancea dok je required gate RED. |
| Tko smije proglasiti uspjeh | Nije verificiran neovisni `diff+exit+criteria` promotion owner | Nije verificiran neovisni artifact-grade promotion owner | Model/session finish i patch vidljivost nisu nezavisan grade | Sam harness računa deterministic pass; S16.6 reviewer je fail/unclear-only i ne može preglasati RED. |
| Pogođeni testovi i fallback | Nije verificiran coverage×diff TIA ugovor s full-suite fallbackom | Nije verificiran takav jezgreni ugovor | Nije verificiran takav jezgreni ugovor | NEXUS TIA/testmon/Bazel adapter iza freshness/completeness ugovora; unknown uvijek full suite. |
| Re-run i flakiness | Provider/tool lifecycle može imati vlastite retry politike | Framework/runtime ponašanje nije naš attempt autoritet | Processor ima vlastitu retry policy | Svaki verifier attempt troši S7 grant; svi rezultati ostaju u bundleu, bez cherry-pickanja prvog GREEN-a. |

  Tvrdnja “nije verificiran” ovdje je namjerno ograničena na pregledani javni core/spec, ne tvrdnja da projekt nigdje nema plugin ili korisnički workflow koji može pokrenuti test. Floor se gradi iz dokazanih mehanizama, ne iz odsutnosti koju nismo mogli iscrpno dokazati.

- **Sinteza — verification plan i evidence:** plan se izvodi iz cilja/criteria, stvarnog diffa i projektnih manifesta, ne iz modelove tvrdnje “tests passed”.

```go
type GateKind uint8 // PARSE|LINT|TYPECHECK|BUILD|TARGETED_TEST|FULL_TEST|APP_CONTRACT|SEMANTIC_REVIEW
type GateStrength uint8 // ADVISORY|REQUIRED|RELEASE

type VerificationPlan struct {
    ID              VerificationPlanID
    RunID           RunID
    RevisionHash    string
    BaseRevisionHash string
    DiffDigest      string
    CriteriaDigest  string
    Gates           []GateSpec
    TIA             *ImpactSelection
    PlanDigest      string
}

type GateSpec struct {
    GateID       string
    Kind         GateKind
    Strength     GateStrength
    RunnerID     string
    Argv         []string
    WorkingDir   string
    Timeout      time.Duration
    ExpectedExit []int
    ConfigDigest string
}

type GateResult struct {
    GateID        string
    RevisionHash  string
    AttemptNo     uint32
    ExitCode      int
    TestCount     *uint64
    FailedTests   []string
    StdoutRef     string
    StderrRef     string
    StartedAt     time.Time
    FinishedAt    time.Time
    Toolchain     ToolchainIdentity
    EvidenceDigest string
}

type EvidenceBundle struct {
    RunID, RevisionHash, BaseRevisionHash string
    DiffRef, DiffDigest, WriteSetDigest   string
    PlanDigest                            string
    Results                               []GateResult
    OracleIntegrity                       OracleIntegrity
    BundleDigest                          string
}
```

  Svaki `GateSpec` je typed argv, nikad repo-controlled shell string. Config iz worktreea je nepouzdan; auto-discovered `test-cmd/lint-cmd` ne izvršava se prije policy/approvala i S7 granta. Svaki command je jedan fizički pokušaj s jednim grantom. Ponovno pokretanje istog gatea nad istim revisionom nije “repair”; dopušteno je samo kao novi S7 attempt zbog priznate flakiness politike, a rezultat ostaje višestruk i ne smije se cherry-pickati.

- **Sinteza — isti-turn feedback i completion gate:**

```go
type VerificationCoordinator interface {
    AfterMutation(context.Context, WorkspaceRevision, MutationRecord) ([]Observation, *contracts.TypedError)
    PlanCompletion(context.Context, RunID, WorkspaceRevision) (VerificationPlan, *contracts.TypedError)
    ExecuteGate(context.Context, contracts.AttemptGrant, GateSpec) (GateResult, *contracts.TypedError)
    Grade(EvidenceBundle, []CriterionResult) CompletionDecision
}
```

  `AfterMutation` bira samo brze, lokalne, bounded adaptere (parser/LSP/compile affected package) i vraća dijagnostiku u istom turnu. Ne pokreće full suite nakon svakog znaka. `PlanCompletion` zatim slojevito radi: (1) parse/LSP, (2) TIA targeted tests, (3) package build/lint/typecheck, (4) full/release/app-contract gate kada scope, stale mapa, sigurnosna granica ili criterion to zahtijeva. Brzi green nikad nije dovoljan ako completion policy traži višu razinu.

- **Sinteza — TIA ugovor:**

```go
type ImpactIndex struct {
    IndexID          string
    ProjectID        string
    SourceRevision   string
    TestCatalogDigest string
    BuildConfigDigest string
    ToolchainDigest  string
    CollectorID      string
    CoverageComplete bool
    FileToTests      map[string][]string
    GeneratedAt      time.Time
}

type ImpactSelection struct {
    DiffDigest    string
    IndexID       string
    Tests         []string
    Fallback      ImpactFallback // NONE|FULL_SUITE|AFFECTED_TARGETS
    Reasons       []string
    SelectionDigest string
}
```

  Index je autoritativan samo ako je nastao iz potpune instrumentirane enumeracije na poznatom source revisionu i istom test catalog/build config/toolchain digestu. Failed, aborted ili collection-incomplete refresh ne može objaviti `CoverageComplete=true`. Prazan/unknown diff, stale/missing indeks, promjena testa/fixturea/configa/build grapha, unmapped non-doc file ili broad boundary vraća `FULL_SUITE`. Čista docs promjena smije izabrati nula code testova samo ako poseban docs gate ostane zadovoljen. Targeted green se zapisuje, ali terminalna politika može zahtijevati full suite.

- **Sinteza — oracle i strukturna anti-sycophancy:** ako diff dira test, fixture, linter config, build manifest, verifier, generated golden ili acceptance rubric, `OracleIntegrity=CHANGED`. Taj rezultat ne može biti neovisni dokaz vlastite promjene bez base-pinned/externog checkera. S16.6 reviewer dobiva zapečaćeni `EvidenceBundle`; ne vidi workerov samohvalni sažetak prije vlastite ocjene. Reviewer može otkriti semantički problem i zaustaviti promotion, ali `exit=1`, missing gate, stale revision ili broken oracle ne može pretvoriti u pass.

- **Salvage:** Python→Go port konzervativnog test-command prepoznavanja i protected-oracle klasifikacije iz `/home/matej/NEXUSv2/core/testcmd.py:1-61`, ali runtime API mora biti `[]string` argv, ne shell string. Portirati coverage-context inversion, diff→test selection i full-suite fallback iz `/home/matej/NEXUSv2/core/tia.py:1-129`; pooštriti refresh tako da failed/partial collection ne objavi autoritativan indeks. Portirati real-exit evidence i independent checker rez iz `/home/matej/NEXUSv2/loop_engine.py:1-18,266-310,328-391` te post-mutation command ledger/final validation iz `/home/matej/NEXUSv2/llm/executor.py:1860-1965,2084-2154,2336-2341,2473-2488` i `/home/matej/NEXUSv2/runtime.py:758-825`. Ne portirati tri identična verifier pokušaja iz `/home/matej/NEXUSv2/core/verifier.py:52-65`, keyword fallback bez exita (`45-50`), model-declared file evidence niti validator loop mimo S7.

- **Ugovor/RED:** Annex **P0.2** vrijedi za svaki verifier pokušaj; **P0.3** veže result i evidence na journal; S16.6 je promotion potrošač, ne owner deterministic pass-a. Obvezni Annex RED `test_adapter_cannot_self_retry`. Coding RED suite: `test_worker_claim_pass_but_exit_one_is_red`, `test_checker_claim_pass_cannot_override_red_gate`, `test_result_from_old_revision_is_rejected`, `test_changed_test_downgrades_oracle_independence`, `test_stale_tia_index_forces_full_suite`, `test_unmapped_non_doc_change_forces_full_suite`, `test_failed_tia_refresh_cannot_publish_complete_index`, `test_zero_impacted_tests_is_not_silent_success`, `test_repo_config_cannot_auto_execute_test_command`, `test_same_revision_retest_needs_new_s7_grant`, `test_post_edit_diagnostic_returns_in_same_turn`, `test_real_diff_ignores_worker_file_claims`.

- **Verifikacija:** [Aider config/docs](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/config/aider_conf.md) i [linter implementation](https://github.com/Aider-AI/aider/blob/main/aider/linter.py) stvarno imaju auto-lint, test command i dijagnostiku za repair; to potvrđuje check-and-reprompt aspekt, ne neovisni artifact-grade acceptance. [pytest-testmon](https://github.com/tarpas/pytest-testmon) stvarno selektira testove pogođene promijenjenim datotekama. Lokalni NEXUS `tia.py` stvarno inverzira coverage per-test contexte i fail-closed vraća full suite na unknown/unmapped non-doc promjenu. OpenCode processor stvarno pre-captures snapshot; Kilo/OpenHands/OpenCode javno ne dokazuju naš cijeli evidence-grade ugovor. Sigurnosni counterexample je i službeni Aider issue o repo-controlled auto-test/lint konfiguraciji; zato se njihov mehanizam grafta iza S6/S7, ne kopira kao trust odluka.

- **Pareto floor:** **PASS za 3.5.** Aiderov brzi repair feedback, OpenCode snapshot, testmon/NEXUS impact selection i NEXUS real-exit grading ostaju. Sinteza je stroža jer dokaz veže uz točan revision, oracle provenance i S7 attempt. Najjači kontraargument je da full-suite fallback poništava TIA ubrzanje na velikim repoima; odgovor nije popustiti gate nego ulagati u svjež, kompletan dependency/coverage index i koristiti targeted tests za iteraciju, dok terminalni proof ostaje proporcionalan riziku. Najslabija točka je cross-language TIA collector kvaliteta; svaki adapter ostaje `UNVERIFIED` dok stale/partial/rename/generated-code fixturei ne pokažu fail-closed ponašanje.

## 3.6 Run trigger plane i admission

- **Tip:** MEHANIZAM za admission/schedule semantiku; ADAPTER za `manual|schedule|webhook|watch|heartbeat`; capability flag `automation`

- **Aspekti:**

| Aspekt | Najbolji izvor | Graft u NEXUS |
|---|---|---|
| Jedinstveni ulaz svih triggera | Vlastiti kod + S0 envelope | Svaki izvor proizvodi isti `TriggerRequest`; nema privilegiranog cron/webhook puta. |
| Schedule/overlap/catch-up | Temporal Schedules | Eksplicitni overlap, backfill/catch-up i durable schedule identitet kao semantički uzor, bez Temporal servisa/runtimea. |
| Misfire/coalesce/concurrency | APScheduler | Bounded missed-run i max-running ponašanje kao fixture/source. |
| Go single-binary cron parsing | `robfig/cron/v3` + Go `time/tzdata` | Parser je pure-Go kandidat; IANA baza se embed-a, ali Annex DST policy ostaje naš owner. |
| Admission safety | Annex P2.6 + S0/S6/S7 | Dedupe, closure, authn/authz, policy, budget i durable idempotency prethode petlji. |

- **Sinteza — trigger/admission:**

```go
type TriggerKind uint8 // MANUAL|SCHEDULE|WEBHOOK|WATCH|HEARTBEAT

type TriggerRequest struct {
    Envelope       contracts.Envelope
    TriggerID      string
    TriggerKind    TriggerKind
    TriggerEventID string
    Principal      contracts.Principal
    RequestedFlags []string
    RunSpecRef     string
    IdempotencyKey string
    ReceivedAt     time.Time
}

type AdmissionDecision struct {
    RequestHash   string
    ClosureHash   string
    PolicyHash    string
    BudgetLeaseID string
    RunID         *RunID
    Status        AdmissionStatus // ADMITTED|DUPLICATE|DENIED|DEFERRED
    Reasons       []string
}

type TriggerAdapter interface {
    Kind() TriggerKind
    Decode(context.Context, []byte) (TriggerRequest, *contracts.TypedError)
}

type AdmissionGate interface {
    Admit(context.Context, TriggerRequest) (AdmissionDecision, *contracts.TypedError)
}
```

  Fiksni admission redoslijed je: bounded decode -> authenticate principal/source -> S0.1 validate envelope -> dedupe `TriggerEventID` -> S0.5 closure -> policy/approval -> budget reservation -> durable run intent/idempotency -> S3.1 start. Denied ili duplicate zahtjev radi nula provider/tool poziva. Webhook signature/replay window, watch canonical path/debounce i heartbeat liveness jesu adapter-specific preconditions; nijedan ne preskače zajednički gate.

- **Sinteza — schedule/DST:** implementirati točno Annex P2.6 `ScheduleManifest`, `ScheduledOccurrence` i stanja. Wall-clock occurrence računa se s embedded IANA zonom; lateness i lease koriste monotoni dio lokalnog procesa. `occurrence_id` je deterministički hash schedule/version/local time/UTC instant/fold policy. Restart ponovno izračuna isti ID i admission dedupe ukloni duplikat. `CATCH_UP` je bounded `max_catch_up`; `FORBID` je single-flight; S7 lease odlučuje tko smije izvršiti već odabrani occurrence, ali ne računa kada on postoji.

```go
type Scheduler interface {
    Next(ScheduleManifest, time.Time) ([]ScheduledOccurrence, *contracts.TypedError)
    Reconcile(context.Context, string, time.Time) ([]ScheduledOccurrence, *contracts.TypedError)
}

// Build uključuje blank import "time/tzdata" kako single binary ne ovisi o host tzdb-u.
```

  `robfig/cron/v3` može biti parser dependency samo ako matrix potvrdi njegov DOM/DOW i DST output prema našim golden vektorima; njegov output nije vlasnik `ONCE_FIRST/ONCE_SECOND/TWICE`, gap, missed ni overlap odluke. Temporal/APScheduler su referentni izvori/komponente za matricu, ne runtime dependencyji.

- **Salvage:** Python→Go port ideja schedule/watch adaptera iz `/home/matej/NEXUSv2/core/schedule.py` i `/home/matej/NEXUSv2/core/watch.py` tek nakon line-level parity audita; njihove timer/clock/default semantike nisu unaprijed autoritativne. Portirati runtime run-admission/event povezivanje samo gdje već prolazi kanonski owner; stari direktni spawn ili best-effort event write ne prelazi u S3.6.

- **Ugovor/RED:** Annex **P2.6** je obvezni gate, posebno `test_zagreb_dst_fold_runs_once_across_restart`. P0.1/P0.3 vrijede za trigger envelope i journal, P0.4 za aktivaciju `automation`, P0.2 za admitted run attempt. Dodatni RED-ovi: `test_webhook_cannot_bypass_common_admission`, `test_duplicate_trigger_event_starts_one_run`, `test_denied_trigger_performs_zero_provider_calls`, `test_catch_up_is_bounded`, `test_forbid_overlap_is_single_flight`, `test_dst_gap_policy_is_explicit`, `test_schedule_restart_reuses_occurrence_id`, `test_automation_adapter_cannot_activate_without_closure`.

- **Verifikacija:** [Temporal Go Schedules docs](https://github.com/temporalio/documentation/blob/main/docs/develop/go/workflows/schedules.mdx) stvarno imaju schedule ID, overlap policy i backfill; [APScheduler guide](https://github.com/agronholm/apscheduler/blob/master/docs/userguide.rst) stvarno razlikuje task/schedule/job, coalesce, misfire grace i max concurrent jobs. [robfig/cron v3](https://github.com/robfig/cron) stvarno je pure-Go parser s per-schedule `CRON_TZ`, a Go [`time/tzdata`](https://pkg.go.dev/time/tzdata) embed-a timezone bazu kao fallback. Niti jedan od tih izvora sam ne zadovoljava Annexov eksplicitni fold/gap/restart ID ugovor; taj dio je vlastiti kod i mora se dokazati golden DST matricom.

- **Pareto floor:** **PASS za 3.6 dizajn, uvjetno do matrixa.** Temporal overlap/backfill i APScheduler misfire/coalesce semantika očuvane su bez vanjskog servisa, a svi triggeri dobivaju isti admission safety floor. Najjači counterexample je restart unutar zagrebačkog jesenskog folda; Annex RED mora dokazati jedan run i jedan idempotency key. Najslabija točka je civil-time enumerator, pa `automation` flag ne smije postati `ACTIVE` prije cross-platform IANA/DST golden testova na Windowsu, macOS-u i Linuxu.

## S3 integracijski gate prije sljedeće coding sekcije

S3 nije spreman samo zato što se skice kompajliraju. Minimalna matrica prije S4/S5 implementacije mora dokazati:

1. replay 10 000 miješanih run/turn/action/observation događaja daje identičan state hash i nula vanjskih poziva;
2. controlled crash u svakoj granici `action durable -> grant -> effect -> result durable` ne stvara dupli side-effect niti izgubljeni terminalni status;
3. paralelni batch s brzim i sporim alatom daje po jednu opservaciju u deklariranom poretku, uključujući cancel;
4. lažljivi worker i lažljivi checker ne mogu promovirati `exit=1`, stale revision, skriveni diff ni promijenjeni oracle;
5. TIA ne može vratiti autoritativno nula testova za unknown/stale/unmapped non-doc promjenu;
6. `STAGNATION`, `NO_PROGRESS` i flip-flop aktiviraju se i kada model preformulira istu grešku ili napravi irelevantan diff;
7. automation DST fold/gap, restart, missed-run i overlap vektori prolaze s determinističkim occurrence ID-em;
8. race detector, cancellation leak test i bounded-stream backpressure test prolaze na sva tri OS-a koja podržava proizvod.

Pareto-floor cijelog S3 je **zadovoljen na razini buildable dizajna, ne još runtime dokaza**. Kandidatski aspekti su verificirani, ali tvrdnja “bolji coding harness” ostaje nedokazana dok implementacija ne prođe ove RED testove i paired behavioral coding gate protiv Kilo Code/OpenHands/OpenCode na istim zadacima, budžetu, modelu i workspace revisionu.

PLAN S12

# S12 — Orkestracija i multi-agent (`multi-agent` capability flag)

## Zajednički vlasnički rez

- **Kod jezgre:** typed delegation envelope, subagent supervisor, workspace-lease binding, DAG
  validator/wave scheduler, durable node state, HITL consumer, A2A PEP/task bridge i Council
  state-machine/verifier.
- **Podaci:** role/team/DAG manifesti, routing zahtjevi, Agent Card sadržaj, council member policy,
  prompts i aggregation/dissent policy. Podatak može samo suziti capability, permission i budget.
- **Vanjski adapteri:** provider/modeli, Git CLI, A2A transport/SDK, remote agenti i eventualni
  Temporal/Restate service. V1 ne ugrađuje LangGraph/CrewAI/ADK/MAF runtime: kanonski Go scheduler
  ostaje single-binary i zove ih samo iza A2A ili eksplicitnog adaptera.
- **Paket-layout:** `internal/orchestration/{contract,supervisor,dag,routing,hitl,council}` i
  `internal/interop/a2a/{contract,discovery,client,server,auth}`. S12 koristi S0 contracts/machine,
  S2.6 routing, S5.2 workspaces, S6 policy, S7 durability/budgets i S16 checkere; ne preuzima
  njihovo vlasništvo.

```go
type AgentRunID string
type NodeID string

type DelegationEnvelope struct {
    Envelope contracts.Envelope
    ParentRunID string
    AgentRunID AgentRunID
    NodeID NodeID
    RoleRef, AgentRef string
    Goal contracts.ContextBlock
    Inputs []contracts.ContextBlock
    OutputContract contracts.SchemaRef
    Workspace workspace.LeaseRef
    Budget budget.GrantRef
    Route routing.RouteDecisionRef
    CapabilityClosureHash, PolicyHash string
}

type AgentResult struct {
    Envelope contracts.Envelope
    AgentRunID AgentRunID
    NodeID NodeID
    WorkspaceBaseRevision, ChangeSetDigest string
    Output json.RawMessage
    EvidenceRefs []contracts.EvidenceRef
    VerificationStatus contracts.VerificationStatus
}
```

**Globalne invarijante S12:**

1. Svaki subagent ima zaseban `agent_run_id`, S7 child budget i S5.2 workspace lease; ne
   impersonira parent run i ne dijeli writable stablo s peerom.
2. Handoff/model/A2A/council output je untrusted P0.1 data. Samo runtime-owned typed status,
   checker evidence i journal transition mogu otključati sljedeći čvor.
3. Retry/deadline/cancel je isključivo S7. S12 deklarira dependency i propagacijsku politiku, ali
   ne vrti vlastitu retry petlju niti reinterpretira timeout kao failure/success.
4. Svaki graph, route, workspace base, policy i capability closure pinna se prije admissiona.
   Hot reload stvara novu generaciju; aktivni DAG ostaje na staroj.
5. `multi-agent` se aktivira samo kroz S0.5 closure. Nedostupan isolation, budget, checker ili auth
   gate znači `REJECTED`, nikad tihi pad na shared workspace ili “best effort” council/A2A.

## 12.1 Subagenti i delegacija s izolacijom konteksta i workspacea

- **Tip:** MEHANIZAM SUPERVISORA + PROCESS/PROVIDER ADAPTER

- **Aspekti:** LangGraph daje supervisor/subgraph i izolirani state; CrewAI role-based delegation;
  Microsoft Agent Framework eksplicitne agent/workflow primitive. NEXUS daje stvarni OS-process
  runner, typed output contract, fenced handoff i coding-kritični S5 worktree obrazac. Naš floor je
  jači: svaki subagent, uključujući sekvencijalne/DAG valove, dobiva vlastiti worktree snapshot;
  integracija nikad ne nastaje konkurentnim pisanjem u parent tree.

- **Sinteza:** `SubagentSupervisor` atomically admitira child run tek nakon S2.6 route, S7 budget i
  S5.2 `WorkspaceLease`. Za coding node S5.2 stvara detached worktree nad eksplicitnim base commitom.
  Agent radi i verificira samo ondje, zatim predaje immutable `ChangeSet` (base hash, patch/tree
  digest, changed paths, evidence). Integrator primjenjuje changesetove jedan po jedan kroz S5.3
  conflict/provenance gate u zasebnom integration worktreeu; parent workspace se mijenja tek nakon
  finalne verifikacije/atomic commit-a. Retry istog nodea nastavlja isti fenced worktree samo ako S7
  lease vrijedi; novi attempt dobiva novi fencing token.

```go
type DelegationRequest struct {
    ParentRunID string
    Node NodeSpec
    Goal contracts.ContextBlock
    Inputs []contracts.ContextBlock
    BaseRevision string
    RequestedBudget budget.Request
    RequiredCapabilities []capability.Ref
    OutputContract contracts.SchemaRef
}

type ChildHandle interface {
    ID() AgentRunID
    Wait(context.Context) (AgentResult, error)
    Events() <-chan contracts.Envelope
}

type SubagentSupervisor interface {
    Admit(context.Context, DelegationRequest) (ChildHandle, error)
    Cancel(context.Context, AgentRunID, contracts.CancelCause) error
    Reconcile(context.Context, AgentRunID) (AgentResult, error)
}

type ChangeSet struct {
    ChangeSetID, BaseRevision, TreeDigest, PatchDigest string
    Paths []workspace.PathChange
    Provenance []contracts.LineageEdge
    EvidenceRefs []contracts.EvidenceRef
}
```

  Invarijante:

  1. `WorkspaceLease.Isolated` mora biti true za svaki mutable coding node. Dirty/non-git base se
     prvo eksplicitno snapshotira kroz S5 ili admission faila `WORKSPACE_ISOLATION_UNAVAILABLE`;
     shared-write fallback je zabranjen.
  2. Svaki child dobiva samo bounded goal, potrebne verified skillove i typed upstream outpute.
     Parent conversation/secret/context ne nasljeđuje se implicitno; S6/S8 policy odabire blokove.
  3. `agent_run_id`, process identity/start-token, workspace lease i S7 fencing token čine jedan
     identity tuple. Stari/zombi child ne smije emitirati accepted result niti sinkronizirati promjene.
  4. Modelovo `verification_status:"verified"` nije authority. Runtime validira output schemu,
     changeset digest, checker evidence i workspace base prije node `SUCCEEDED` prijelaza.
  5. Peer worktreeovi nikad se međusobno ne montiraju. Handoff je immutable output/evidence ref;
     ako downstream treba upstream kod, scheduler mu stvara novi worktree nad verificiranim
     integration commitom, ne daje upstreamovu živu mapu.
  6. Cancel parenta atomically fencea nove child radnje, propagira S7 cancel svim potomcima, drain-a
     output kanale, cleanup delegira S5.2/P1.2 sweeperu i tek tada zatvara parent.

- **Salvage:** iz `/home/matej/NEXUSv2/subagent_runner.py:1-6,14-55` Python→Go portirati one-job/
  one-process envelope i atomic output-file namjeru; iz
  `managers/subagent_orchestrator.py:87-122,180-204,309-349` portirati child result validaciju,
  fenced upstream handoff i integrator rezultat. Iz `core/worktree.py:47-80,113-208` portirati exact
  revision worktree, NUL-safe diff, containment/conflict detection i atomic sync kao S5 helper.
  **Ne portirati** `worktree.session` shared fallback (`core/worktree.py:88-104`) niti procesni path
  koji serijalizira shared workspace (`runtime.py:539-555`): novi contract zahtijeva per-child
  worktree i changeset-based integraciju.

- **Ugovor/RED:** P0.1/P0.2 + P1.2 + P2.2/P2.3 te S5.2/S5.3. RED:
  `test_two_subagents_never_share_writable_root` pokreće dva peer nodea koja zapisuju isti sentinel;
  svaki mora vidjeti samo vlastiti worktree, a parent ostaje byte-identičan do integration gatea.
  Dodatno: `test_dirty_repo_cannot_degrade_to_shared_multi_agent`,
  `test_stale_child_fence_cannot_submit_changeset`,
  `test_model_verified_claim_without_checker_evidence_is_rejected`,
  `test_parent_cancel_drains_and_reaps_all_children` i
  `test_conflicting_changesets_preserve_both_sources_and_block_commit`.

- **Verifikacija:** lokalni NEXUS stvarno pokreće child kao zaseban proces i ima worktree helper;
  `llm/executor.py:2891-2917` čak odbija shared fallback za paralelni agentic path. Međutim
  `runtime.py:539-555` potvrđuje da process i DAG path još nemaju per-agent worktree te se
  serijaliziraju/shared-runaju. LangGraph službene subgraph upute potvrđuju zaseban state i
  checkpoint/HITL mogućnost, ali ne dokazuju Git workspace izolaciju.

- **Pareto floor:** **PASS na dizajnu; isolation je hard gate.** Dobivamo LangGraph/CrewAI/MAF
  delegation ergonomiju uz jaču coding izolaciju i evidence boundary. Najjači counterexample je
  non-git/dirty projekt; rješenje nije dijeljeno stablo nego S5 snapshot adapter ili eksplicitno
  odbijanje `multi-agent` mutacije.

## 12.2 Workflow/DAG s determinističkim tokom

- **Tip:** MEHANIZAM DAG VALIDATORA I DURABLE WAVE SCHEDULERA

- **Aspekti:** LangGraph daje graph/state/checkpoint model; Temporal durable event-history,
  deterministic replay i cancellation referent; Google ADK daje sequential/parallel/loop workflow
  agente uz model-led čvorove. Naš Go scheduler uzima statički graph, Kahn cycle validation,
  determinističke waveove i S7 journal/lease semantiku bez vanjskog workflow runtimea.

- **Sinteza:** `GraphCompiler` validira unique node ID-eve, sve edge reference, acikličnost, output→
  input schema kompatibilnost, capability closure, budget sum i HITL/checker gateove prije admissiona.
  Rezultat je immutable `CompiledDAG`. `WaveScheduler` računa ready-set iz journal-derived terminalnih
  stanja, sortira ga po stabilnom node ID-u, atomically rezervira child budget/workspace lease i
  pokreće do `max_parallel`. Wave barrier ne čeka proizvoljno sve ako policy dopušta streaming
  successors; pojedini successor postaje ready tek kad su svi njegovi required predecessors
  `VERIFIED`, nikad na modelovom `done` tekstu.

```go
type NodeSpec struct {
    NodeID NodeID
    AgentRef string
    Needs []NodeID
    InputContract, OutputContract contracts.SchemaRef
    RequiredCapabilities []capability.Ref
    Budget budget.Request
    FailurePolicy FailurePolicy // FAIL_FAST|CONTINUE_INDEPENDENT|COMPENSATE
    HumanGate *HITLGateRef
    CheckerRef contracts.CheckerRef
}

type DAGManifest struct {
    SchemaVersion, DAGID, DAGVersion, ConfigHash string
    Nodes []NodeSpec
    MaxParallel uint32
    Deadline time.Time
}

type CompiledDAG struct {
    ManifestDigest, ClosureHash, TopologyDigest string
    Nodes map[NodeID]NodeSpec
    Waves [][]NodeID
}

type DAGScheduler interface {
    Admit(context.Context, CompiledDAG, string) (WorkflowRun, error)
    Tick(context.Context, string) ([]SchedulerDecision, error)
    Cancel(context.Context, string, contracts.CancelCause) error
}
```

  Stanja nodea: `DECLARED→READY→LEASED→RUNNING→COMMIT_PENDING→{VERIFIED|FAILED|CANCELLED}`;
  `WAITING_HUMAN` je durable wait između `RUNNING` i ponovnog `READY`. Workflow je
  `COMPILED→ADMITTED→RUNNING→{SUCCEEDED|FAILED|CANCELLED|COMPENSATING}`.

  Invarijante:

  1. Unknown dep, self-edge, cycle, duplicate ID, output/input schema mismatch ili graph preko
     node/edge/depth/budget limita faila prije ijednog spawna.
  2. Ready odluka i lease reservation su jedna journal/CAS transakcija. Dva scheduler ticka ne mogu
     dvaput admitirati isti node; reclaim koristi S7 fencing, ne in-memory `done` mapu.
  3. Stabilni ready order služi replayu/auditu; concurrency interleaving nije poslovni signal.
     Result acceptance ovisi o node ID+attempt+fence+output digest, ne o completion redoslijedu.
  4. Failed predecessor nikad ne otključava dependent. `CONTINUE_INDEPENDENT` dopušta samo nodeove
     bez tranzitivne ovisnosti; `COMPENSATE` zove S6.9/S7 effect contract.
  5. Graph se ne mutira tijekom runa. Model-led plan može predložiti novi manifest/version, ali mora
     ponovno proći compile/admission; ne ubacuje edge u aktivni graph.
  6. Temporal/Restate adapter, ako se aktivira service profilom, projicira isti contract i nema
     vlastiti retry owner; lokalni journal ostaje source of truth ili se deployment eksplicitno
     prebaci na jedan durable owner — nikad dual-write scheduler.

- **Salvage:** iz `managers/subagent_orchestrator.py:124-178,222-241` portirati Kahn cycle/unknown
  guard, sorted ready-set, parallel wave i failed-stage block; iz `core/workflow.py:18-85` portirati
  bounded edge/state projekciju. Nadogradnja je materijalna: postojeći workflow zapis je best-effort
  live monitor, nije attempt-granular journal (`core/workflow.py:4-10`), a schedulerov `done` je
  in-memory. S7 journal/lease/fence zato se pišu kao kanonski owner.

- **Ugovor/RED:** P0.1/P0.2/P0.3 + P2.2/P2.3. RED:
  `test_cycle_and_unknown_dependency_spawn_zero_children`;
  `test_concurrent_ticks_admit_node_once`;
  `test_failed_predecessor_never_unlocks_dependent`;
  `test_crash_replay_rebuilds_same_ready_set`;
  `test_stale_attempt_result_cannot_advance_dag`;
  `test_dynamic_model_edge_requires_new_graph_version` i
  `test_budget_reservation_precedes_wave_spawn`.

- **Verifikacija:** lokalni NEXUS testovi stvarno pokrivaju `architect→backend∥frontend→qa`,
  cycle rejection i failed prerequisite (`subagent_orchestrator.py:370-463`), ali ne crash replay/
  lease/fencing. Google ADK API dokumentira Sequential/Parallel/Loop workflow agents; durable
  recovery dolazi kroz zasebne integracije poput Restatea. Zato ADK nije dokaz da naš lokalni DAG
  ispunjava S7.

- **Pareto floor:** **PASS uz S7 journal, ne uz postojeći monitor.** Zadržava graph ergonomiju i
  parallelism bez Temporal service obveze. Najjači counterexample je crash nakon spawn-a prije
  state commita; atomic admission+lease i reconcile child identity moraju odlučiti nastaviti,
  fenceati ili označiti unknown — nikad slijepo spawnati duplikat.

## 12.3 Model routing — multi-agent potrošač kanonskog S2.6 ownera

- **Tip:** TANKI ADAPTER/POTROŠAČ; NEMA NOVOG ROUTING MEHANIZMA

- **Aspekti:** MAF/CrewAI dopuštaju per-agent model config, Google ADK model na LLM agentu, a
  LangGraph supervisor bira subgraph/agent. Nijedan aspekt ne opravdava drugi scorer: S2.6 već
  posjeduje capability, quality, cost, latency, privacy i health routing.

- **Sinteza:** compiler za svaki node proizvodi typed `AgentRouteRequest` i jednom zove S2.6.
  Odluka se pinna u `DelegationEnvelope`; scheduler je samo izvršava. Re-route/fallback je nova S2.6
  odluka nad istim intentom i S7 attemptom, s ponovnim S6.7 provider-data gateom. Chair/member routing
  u councilu koristi isti API; A2A remote agent nije model route nego 12.5 capability endpoint.

```go
type AgentRouteRequest struct {
    RunID string
    NodeID NodeID
    RoleRef, AgentRef string
    Required provider.CapabilityFloor
    ContextClass contracts.Sensitivity
    Budget budget.GrantRef
    LatencyClass routing.LatencyClass
    IndependenceGroup string
}

type AgentRouter interface {
    ResolveAgent(context.Context, AgentRouteRequest) (routing.RouteDecisionRef, error)
}
```

  Invarijante:

  1. S12 ne računa model score, ne prati provider health i ne vrti fallback. Svaka odluka referencira
     S2.6 evidence/config hash.
  2. Node ne starta ako ruta ne zadovoljava declared/measured capability floor ili data policy.
  3. `IndependenceGroup` je constraint za Council (npr. različit provider/model family kad policy to
     traži), ne dokaz epistemičke neovisnosti; route receipt bilježi ostvareno odstupanje.
  4. Promjena routea usred node attempta nije dopuštena. Novi attempt dobiva novu odluku i audit link.

- **Salvage:** NEXUS orchestration prosljeđuje agent name, dok provider selection živi drugdje;
  portirati samo role/agent metadata iz `subagent_orchestrator.py`, a `llm/router.py` ostaje S2.6
  Python→Go salvage. Ne graftati model izbore u DAG scheduler.

- **Ugovor/RED:** S2.6 + Annex P0.2/P2.5. RED:
  `test_orchestrator_cannot_bypass_s2_route_decision`;
  `test_agent_fallback_rechecks_provider_data_policy`;
  `test_route_change_requires_new_attempt` i
  `test_council_independence_constraint_failure_is_visible`.

- **Verifikacija:** spec izričito označava 12.3 kao potrošača 2.6. Kandidatske per-agent model
  konfiguracije dokazuju potrebu za inputom, ne potrebu za novim ownerom.

- **Pareto floor:** **PASS minimalnim adapterom.** Ovo je namjerni anti-duplication rez; sve više od
  request/receipt mapiranja stvorilo bi dva routing authorityja i kontradikciju pri fallbacku.

## 12.4 Human-in-the-loop točke

- **Tip:** MEHANIZAM POSTAVLJANJA GATEA + POTROŠAČ S6/S7 APPROVAL CORE-a

- **Aspekti:** LangGraph `interrupt` daje pause/resume u graphu; OpenAI Agents SDK serializable HITL
  interruption; MAF checkpointed workflow HITL. Naš dodatni floor je P1.6 exact-intent,
  authenticated, expiring, single-use challenge; S12 samo veže challenge uz node/DAG.

- **Sinteza:** node može deklarirati `HITLGateRef{before_effect, after_draft, on_policy}`. Scheduler
  traži S6 approval core da izda `ApprovalChallenge`, journalira `WAITING_HUMAN` i S7 durable parkira
  run bez worker goroutinea/procesa. Lokalni CLI/TUI/desktop i remote channel koriste isti
  `SubmitDecision` API. Samo S6 verificira principal/channel/token i atomically radi terminalni
  prijelaz; samo S7 nastavlja node. Freeform feedback je novi user data block i novi intent, nikad
  prešutno `APPROVED`.

```go
type HITLGateRef struct {
    GateID string
    Trigger GateTrigger
    AllowedDecisions []approval.Decision
    Timeout time.Duration
}

type HITLCoordinator interface {
    PauseForApproval(context.Context, string, NodeID, effects.EffectIntent) (
        approval.ApprovalChallenge, error)
    SubmitDecision(context.Context, approval.SignedDecision) (approval.DecisionReceipt, error)
}
```

  Invarijante:

  1. Challenge nosi sva P1.6 polja i payload hash exact effect intenta. Preview tekst nije authority;
     odobren je samo hashirani intent i deklarirana odluka.
  2. `WAITING→APPROVED|DENIED|EXPIRED|CANCELLED` je terminalno po challengeu. Duplicate/replay,
     wrong principal/channel ili late answer ne nastavlja node.
  3. Approval ne može proširiti kernel policy, capability closure, tool args ili budget. Promjena
     payload-a nakon approvala zahtijeva novi challenge.
  4. Channel adapter se ne registrira bez `channels→extensions+7.3+approval-core` closurea. Lokalni
     UI nema iznimku od exact-intent/single-use pravila.
  5. Human answer se ne injektira kao system instrukcija. Typed odluka upravlja stateom; opcionalni
     komentar ostaje provenance-fenced user data za novi turn.

- **Salvage:** iz `/home/matej/NEXUSv2/core/gate.py:25-92` portirati persisted open gate, atomic
  single-winner answer/consume i stale-attempt cancel; ne portirati semantiku gdje je odgovor samo
  string bez principal/channel/payload hash/expiry/nonca. `gate.py:55-83` je dobar atomic baseline,
  ali ne P1.6 remote token. DAG wiring dolazi iz 12.2, durability iz S7.

- **Ugovor/RED:** **Annex P1.6**. Kanonski
  `test_channels_contract_fails_closed`: bez extensions/S6.8 attestationa dobiva se
  `INCOMPLETE_CAPABILITY_CLOSURE`; drugi odgovor istim tokenom daje `APPROVAL_REPLAY`; nema side
  effecta ni drugog resumea. Dodatno: `test_approval_payload_change_requires_new_challenge`,
  `test_wrong_principal_or_channel_cannot_resume`,
  `test_expired_local_and_remote_challenges_match` i
  `test_freeform_comment_is_not_approval`.

- **Verifikacija:** NEXUS gate stvarno čuva pitanje, atomically prihvaća jedan answer, jednom ga
  consumea i otkazuje stale attempt; nema auth/expiry/exact-intent token. LangGraph službeni docs
  potvrđuju subgraph interrupt+resume, ali njegova primitiva sama ne dokazuje naš principal binding.

- **Pareto floor:** **PASS samo kao P1.6 consumer.** Dobivamo framework HITL ergonomiju uz jaču
  replay/auth granicu. Najjači napad je odobriti benigni draft pa zamijeniti payload prije commit-a;
  exact payload hash i S6.9 commit preflight moraju odbiti promjenu.

## 12.5 Agent-to-agent interoperabilnost i discovery (A2A)

- **Tip:** ADAPTER IZA KANONSKOG A2A KLIJENT/SERVER UGOVORA + SIGURNOSNI MEHANIZAM

- **Aspekti:** Linux Foundation A2A specification je wire contract za Agent Card, discovery,
  messages/tasks/artifacts, streaming i push; Microsoft Agent Framework daje client+hosting bridge;
  Google ADK daje A2A izlaganje/konzumiranje, ali spec ga označava eksperimentalnim. Primarni pick je
  službeni A2A Go SDK/schema iza našeg PEP-a; framework adapteri su conformance peers, ne authority.

- **Sinteza:** `A2ADiscovery` prihvaća samo prekonfiguriran/registry-odobren HTTPS origin, dohvaća
  `/.well-known/agent-card.json` kroz S6 egress/SSRF konektor s pinned DNS IP-em, bez redirecta,
  bounded responseom i typed schema validationom. JWS se provjerava kad policy zahtijeva; i valjan
  potpis dokazuje integritet/izvor carda, ne authorization. `A2AClient` bira podržani interface,
  dobiva credentials out-of-band iz S6 brokera i mapira remote task state u S0/S7 journal.
  `A2AServer` služi minimalni javni card i authenticated task API preko istog local orchestration
  ownera; remote caller nikad ne zaobilazi S6/S7/S12 admission.

```go
type AgentEndpoint struct {
    Origin, CardURL, InterfaceURL string
    CardDigest, CardVersion, SignerID string
    Transport A2ATransport
    SecuritySchemes []SecurityScheme
    ExpiresAt time.Time
    EgressDecisionRef string
}

type RemoteTaskIntent struct {
    IntentID, RequestHash, IdempotencyKey string
    PrincipalID, TenantID string
    Endpoint AgentEndpoint
    Message a2a.Message
    RequiredSkillID string
    Deadline time.Time
    Budget budget.GrantRef
}

type A2AClient interface {
    Discover(context.Context, ApprovedOrigin) (AgentEndpoint, error)
    Send(context.Context, RemoteTaskIntent) (RemoteTaskHandle, error)
    Get(context.Context, AgentEndpoint, string) (a2a.Task, error)
    Cancel(context.Context, AgentEndpoint, string) error
}

type A2AServer interface {
    PublicCard(context.Context) (a2a.AgentCard, error)
    Handle(context.Context, AuthenticatedPeer, a2a.Request) (a2a.Response, error)
}
```

  Invarijante:

  1. Agent Card je untrusted discovery metadata. Card-advertised URL, skill, auth scheme, extension,
     file/artifact URI i push callback ponovno prolaze allowlist, schema, authz i SSRF policy.
  2. SSRF obrana validira scheme/host/port, sve resolved IP-e, odbija loopback/private/link-local/
     multicast/metadata i DNS rebinding tako da spaja na provjereni IP uz TLS `ServerName`; redirects
     su off. Svaki reconnect/redirect zahtijevao bi novu provjeru.
  3. Credentials se dobivaju out-of-band i šalju samo endpointu/scopeu koji je S6 autorizirao;
     nikad ne ulaze u Agent Card, prompt, task artifact, URL query ili log.
  4. Lokalni `IdempotencyKey` se atomically claim-a uz `RequestHash` prije mreže. A2A send
     idempotency nije univerzalno zajamčena: timeout nakon slanja vodi `UNKNOWN_REMOTE_EFFECT` i
     poll/reconcile po poznatom remote task/context ID-u; nema slijepog ponovnog Send-a.
  5. Incoming server request autentificira peer na svakom pozivu, autorizira tenant/skill, size/
     rate/budget capira message/history/artifact i mapira jedan external task na jedan local run.
  6. Task/artifact/message nose P0.1 lineage, sensitivity i source agent/card digest. Remote
     `completed` nije naš `VERIFIED`; S16 checker odlučuje lokalno prije uporabe/promocije.
  7. Public card ne otkriva interne URL-ove, credentials, tenant-specifične skillove ni policy.
     Extended card je authenticated/authorized i session-scoped. Card cache poštuje digest/expiry;
     promjena endpointa/security scheme invalidira auth session.
  8. Push webhook je zaseban untrusted transport: callback URL SSRF-gated, delivery bounded i
     authenticated; receiver provjerava expected task ID i obrađuje duplikate idempotentno.

- **Salvage:** NEXUS nema navedeni A2A client/server pa nema lažnog “porta”. Reuse su S0 typed
  envelope/state, S6 `egress.py` SSRF hardblock/auth broker, S7 task/idempotency/reconcile i S12.1
  admission. MAF/ADK adapteri služe conformance testovima; ne uvode Python/.NET runtime u Go jezgru.

- **Ugovor/RED:** P0.1/P0.2 + P2.2/P2.5, S6.3/S6.6 i A2A schema/conformance. RED:
  `test_agent_card_is_not_authorization` daje potpisan card s admin skillom neautoriziranom
  principalu; client mora vratiti `A2A_SKILL_FORBIDDEN` i poslati nula task requestova.
  Dodatno: `test_card_url_dns_rebind_cannot_reach_metadata`,
  `test_card_redirect_to_private_ip_is_rejected`,
  `test_same_idempotency_key_different_request_hash_is_rejected`,
  `test_send_timeout_reconciles_without_blind_resend`,
  `test_incoming_task_cannot_cross_tenant`,
  `test_remote_completed_stays_unverified_until_local_checker`,
  `test_duplicate_push_notification_commits_once` i službeni A2A conformance corpus za client/server.

- **Verifikacija:** službeni A2A spec potvrđuje `/.well-known/agent-card.json`, security schemes,
  per-request auth, server-side authorization, HTTPS/TLS, JWS card signatures, task/artifact model te
  SSRF provjeru file/push URL-ova. Spec također pokazuje da auth ostaje implementacijska odgovornost;
  card nije credential ni authorization odluka. Microsoft službeno dokumentira A2A client/service i
  hosting, uključujući Go server provider; Google ADK A2A ostaje eksperimentalni matrix peer.

- **Pareto floor:** **PASS na contractu; interoperabilnost čeka conformance matrix.** Standardni wire
  format ostaje netaknut, a dodajemo nužne SSRF/auth/idempotency granice. Najjači counterexample je
  remote agent koji je prihvatio task, ali odgovor je izgubljen: “retry” može duplicirati vanjski
  učinak, pa unknown-state reconciliation mora imati prednost nad dostupnošću.

## 12.6 Multi-persona deliberation (Council)

- **Tip:** MEHANIZAM SEALED COUNCIL STATE-MACHINEA + MODEL ADAPTERI

- **Aspekti:** NEXUS daje pet persona→anonimni cross-review→chairman oblik; MAF/CrewAI/LangGraph
  daju group/supervisor orchestration. P2.7 dodaje ono presudno: isti artifact revision, sealed
  nezavisni commit prije cross-reviewa, pseudonime, quorum/budget, finding/evidence identitet,
  dissent-preservation verifier i eksplicitnu zabranu promotion authorityja.

- **Sinteza:** `CouncilCoordinator` validira `CouncilRequest`, snapshotira artifact/evidence i za
  svakog člana stvara izolirani S12.1 child run s istim sealed input hashom. Član ne dobiva peer
  output dok njegov signed `ReviewRecord` nije durable commitan. Nakon quorum-a coordinator stvara
  per-council pseudonime i cross-review pakete bez model/provider/role identiteta. Chair sintetizira
  typed findings; neovisni `CouncilVerifier` uspoređuje sve materijalne finding ID-eve/evidence refs
  s outputom i zahtijeva `AdjudicationRecord` za svaki spojeni/odbačeni dissent. Rezultat ide S16.6
  kao reviewer evidence, nikad izravno promotion gateu.

```go
type CouncilRequest struct {
    CouncilID, RunID, ArtifactRevisionHash, Question string
    Members []CouncilMemberSpec
    MinQuorum uint32
    IndependencePolicy IndependencePolicy
    AnonymizationPolicy AnonymizationPolicy
    EvidenceRefs []contracts.EvidenceRef
    Deadline time.Time
    BudgetID string
    Chair CouncilMemberSpec
    AggregationRule, DissentPolicy string
    OutputSchemaVersion string
}

type ReviewRecord struct {
    ReviewID, SealedInputHash, MemberPseudonym, FindingID string
    Claim string
    EvidenceRefs []contracts.EvidenceRef
    Severity contracts.Severity
    Confidence float64
    SubmittedAt time.Time
    Signature string
}

type AdjudicationRecord struct {
    FindingID, Decision, Rationale string
    PreservedEvidenceRefs []contracts.EvidenceRef
    ChairSignature string
}

type CouncilOutput struct {
    CouncilID, ArtifactRevisionHash string
    Majority []SynthesizedFinding
    Dissent []ReviewRecord
    Unresolved []ReviewRecord
    Adjudications []AdjudicationRecord
    VerificationReceipt contracts.EvidenceRef
}
```

  Stanja su točno P2.7:
  `FORMED→INPUTS_SEALED→INDEPENDENT_REVIEW→CROSS_REVIEW→SYNTHESIS→VERIFIED`, uz
  terminalna `FAILED_QUORUM|REJECTED`.

  Invarijante:

  1. Svi članovi i chair rade nad istim `artifact_revision_hash`; mismatch ili artifact promjena
     odbija cijeli council, ne “osvježava” samo jednog člana.
  2. Nema peer-output read API-ja prije vlastitog durable sealed review commita. Scheduler/barrier,
     ne prompt, provodi neovisnost.
  3. Pseudonim je council-scoped, nasumično mapiran i chair/modelu ne otkriva persona, provider,
     senioritet ni redoslijed; audit owner čuva encrypted mapping samo za odgovornost.
  4. Quorum i S7 budget/deadline su hard gateovi. Dead member se ne “dropa” ako time pada ispod
     quorum-a; chair se ne pokreće bez dovoljnog broja valjanih sealed reviewa.
  5. Materiality je versioned trusted policy/checker klasifikacija, ne chair prose. Chair ne smije
     promijeniti evidence ref, izmisliti finding ID ni izbaciti dissent bez adjudication recorda.
  6. Output uvijek razlikuje majority, dissent i unresolved. “Consensus” ne znači gubitak manjinskog
     dokaza; confidence nije glas niti zamjena evidenceu.
  7. Council može preporučiti; S16.6 deterministic checker/promotion gate ostaje odvojen i mora
     prihvatiti/rejectati artifact na stvarnim dokazima. Council vlastiti `VERIFIED` znači samo da
     je protokol deliberacije ispunjen.

- **Salvage:** iz `/home/matej/NEXUSv2/managers/council.py:1-7,23-66` portirati tri faze,
  paralelno neovisno prikupljanje, anonimni cross-review, chairman i provenance fencing. **Ne
  portirati kao dovoljan ugovor:** postojeći `_safe` tiho dropa člana (`council.py:69-73`), nema
  artifact hash, sealed durable commit, pseudonym mapping, quorum, budget, typed finding/evidence,
  dissent verifier ni promotion separation. `subagent_orchestrator.py` daje isolated child seam,
  a S7 journal/barrier daje stvarnu sealed neovisnost.

- **Ugovor/RED:** **Annex P2.7 je hard gate.** Kanonski
  `test_council_cannot_drop_sealed_material_dissent`: chair izostavi jedan materijalni sealed nalaz;
  verifier vraća `DISSENT_DROPPED`, council ostaje `REJECTED`, a promotion gate ne dobiva success.
  Dodatno: `test_member_cannot_read_peers_before_sealed_commit`,
  `test_artifact_hash_mismatch_rejects_whole_council`,
  `test_quorum_failure_prevents_chair`,
  `test_pseudonymization_hides_member_identity`,
  `test_chair_cannot_rewrite_evidence_ref` i
  `test_council_verified_cannot_satisfy_checker_gate`.

- **Verifikacija:** NEXUS lokalni kod stvarno ima pet persona, parallel answer, anonimni answer list,
  cross-review i chairman te fencea model output; test čak pokazuje da jedan mrtav član biva tiho
  ispušten, što je konkretna P2.7 rupa. Kandidatski group-chat/supervisor feature nije dokaz sealed
  independence ili dissent preservation; to mora proći naše black-box RED-ove.

- **Pareto floor:** **PASS samo s P2.7 verifierom.** Čuva NEXUSovu jednostavnu trofaznu ergonomiju,
  a dodaje dokaziv anti-groupthink floor. Najjači counterargument je da isti model/provider u pet
  persona nije stvarno neovisan; `IndependencePolicy` mora to prikazati i, kad je diversity hard
  requirement, S2.6 mora osigurati različite routeove ili council faila — persona prompt nije dokaz.

## S12 završni gate i dokazni strop

- **Obvezni acceptance chain:** `S0.5 multi-agent closure → DAG compile → S2.6 route → S7 child
  budget/lease → S5.2 isolated worktree → S12 child/council/A2A execution → S16 checker →
  changeset integration`. HITL umeće P1.6 durable wait, ne paralelni resume path.
- **Minimalni concurrency/platform testovi:** dva scheduler ticka na istoj SQLite WAL bazi;
  SIGKILL parenta s aktivnim childovima/worktreeovima; Windows worktree path+process cleanup;
  cancel tijekom wavea; conflicting changesets; A2A duplicate delivery/DNS rebinding; Council
  sealed barrier pod članom koji timeouta.
- **Descent proof:** vanjski LangGraph/Temporal/ADK/MAF runtime odbijen je kao core jer ruši Go
  single-binary i stvara drugog retry/journal ownera. Standard/native Git worktree, Go concurrency,
  `net/http` i službeni A2A schema/SDK pokrivaju contract; vanjski runtime ostaje adapter samo kad
  deployment eksplicitno treba service durability/interoperabilnost.
- **Fight-the-fix:** najopasniji preostali slučaj je kompromitiran, pravilno autentificiran remote
  A2A agent ili subagent koji vraća uvjerljiv ali lažan “verified” rezultat. Auth, worktree i protocol
  conformance to ne rješavaju; P0.1 provenance, runtime-owned evidence i S16 checker moraju ostati
  izvan agentova output authorityja.
- **Proof ceiling:** ovaj plan potvrđuje lokalne salvage seamove i aktualni A2A wire/security
  contract, ali ne dokazuje Go implementaciju, cross-framework conformance, OS cleanup ni da
  persona/model diversity poboljšava kvalitetu. To zahtijeva RED/GREEN, A2A conformance suite i
  paired council eval nad frozen artifactima.
- **S12 Pareto-floor sudac:** **PASS kao dizajn; 12.1 isolation, P1.6 i P2.7 su hard gateovi.** Ako
  bilo koji dopušta shared-write fallback, replay approval ili izgubljeni dissent, `multi-agent`
  capability ostaje `REJECTED`.

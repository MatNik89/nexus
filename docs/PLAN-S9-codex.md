PLAN S9

# S9 — Memorija preko sesija (`memory` capability)

## Zajedničke odluke i paket-layout

- **Granica capabilityja:** 9.1 save/resume i kanonski session record dio su jezgre svakog stateful harnessa. Branch/browse/search te 9.2–9.4 aktiviraju se preko composable `memory` flag-a. Bez njega jezgra i dalje može crash-safe nastaviti run, ali ne radi cross-session recall, automatsko učenje ni konsolidaciju.
- Predloženi layout: `internal/session/{contract,store,resume,branch}` i `internal/memory/{contract,store,writegate,recall,learn,consolidate}`. Oba koriste isti `modernc.org/sqlite` pure-Go WAL spine i P0.3 journal; nema drugog JSONL ownera ni embedded Python servisa. Vektori i KG su izvedeni indeksi, ne source of truth.
- P0.1, ne P0.3, normativno posjeduje `ContextBlock.lineage/trust_class/sensitivity`; P0.3 posjeduje jedini append u event/log/trace/transcript/audit historiju. S9 mora zadovoljiti oba: svaka memorija i izvedeni zapis čuvaju P0.1 provenance, a svaki state prijelaz ide kroz P0.3 journal.
- Model ne piše izravno u trajnu memoriju niti skill. `memory.write_approval=true` i `skills.write_approval=true` su sigurni defaulti: model/background job proizvodi immutable proposal, a hash-bound approval dopušta točno jednu commit operaciju. Config može suziti sustav na read-only; isključivanje approval gatea mora biti eksplicitna, auditirana korisnička odluka.
- `MEMORY_FORGET` i `DATA_PURGE` nisu sinonimi. Forget je S9 reversible recall tombstone; purge je S6.7 autoritativno ireverzibilno brisanje svih kopija/indeksa/cachea/backupa i uvijek nadjačava S9. Decay i consolidation nisu ni jedno: mijenjaju rank/tier, ne postojanje izvora.

```go
package memory

type Scope uint8 // PROJECT|TASK|USER|LESSON|CORPUS
type RecordState uint8 // PROPOSED|APPROVAL_PENDING|ACTIVE|SUPERSEDED|FORGOTTEN|PURGE_PENDING

type Record struct {
    MemoryID, TenantID, PrincipalScopeHash string
    Scope Scope
    Block contracts.ContextBlock
    Confidence float32
    ValidFrom time.Time
    ValidTo *time.Time
    State RecordState
    Version uint64
    Supersedes []string
    CreatedAt, UpdatedAt time.Time
}

type Store interface {
    WithImmediate(context.Context, func(Tx) error) error
    ByID(context.Context, string) (Record, error)
    Recall(context.Context, RecallQuery) ([]RecallHit, error)
}
```

Globalne invarijante:

1. Source of truth su immutable P0.3 eventi + verzionirani memory recordi. FTS, vector, KG, persona block, gist i association graph imaju `source_generation` i mogu se obrisati/rebuildati bez gubitka činjenice.
2. Trust se nikad ne izvodi iz scopea: `USER` scope nije automatski P0.1 `USER` trust. Model-derived, web-derived i tool-derived memorija ostaje ista ili niža trust klasa kroz extract, summary, recall i skill proposal.
3. Svaki write/forget/supersede/restore nosi optimistic `expected_version`; SQLite `BEGIN IMMEDIATE` + conditional update sprječava lost update između CLI-ja, desktopa, gatewaya i background joba.
4. Recall je bounded i authz-first. Tenant/principal/sensitivity policy primjenjuje se prije FTS/vector/KG pretrage; denied zapis ne smije procuriti ni kroz score, snippet, entity ili association edge.
5. Nema fizičkog deletea iz S9. Jedini put do potpunog brisanja je P2.1 `DataPurge` pod S6.7; minimalni non-content audit receipt ostaje prema ugovoru.

## 9.1 Sesije: save, resume i branch

- **Tip:** MEHANIZAM (save/resume JEZGRA; branch/search UI `memory` profil)

- **Aspekti:** goose je najbolji referent za user-facing resume/fork/edit tok; OpenHands za rekonstrukciju conversation statea iz event streama; Codex CLI za robustan resume iz trajnog zapisa. Hermes/Stash dodaju cross-session FTS i agent-native handoff prikaz. Naš graft je P0.3 event-derived session head s S7.3 durable checkpointom, bez kopiranja historije pri branchanju.

- **Sinteza:** `SessionStore` u istoj SQLite transakciji appenduje session event i ažurira izvedeni head. `Save` sprema samo canonical event offset, pinned config/policy/capability digeste, workspace checkpoint i model/provider identitet; model context se ponovno projektira preko S8. Resume validira sve digeste, poziva S7.3 recovery/reconciliation i nastavlja samo iz legalnog S0 stanja. Branch je novi session ID s immutable parent offsetom; naknadni parent eventi ne ulaze u branch.

```go
type Session struct {
    SessionID, RunID, TenantID, PrincipalID string
    State contracts.RunState
    HeadOffset uint64
    WorkspaceRevision, CheckpointRef string
    ConfigDigest, PolicyDigest, CapabilityDigest string
    ProviderID, ModelID string
    ParentSessionID *string
    ForkOffset *uint64
    CreatedAt, UpdatedAt time.Time
    Version uint64
}
type ResumePlan struct {
    Session Session
    ReplayFrom, ReplayThrough uint64
    RestoreCheckpointRef string
    Reconcile []durable.ReconcileIntent
    Compatibility migration.Decision
}
type SessionStore interface {
    Create(context.Context, CreateSession) (Session, error)
    Save(context.Context, SavePoint) (Session, error)
    PlanResume(context.Context, string) (ResumePlan, error)
    Fork(context.Context, ForkRequest) (Session, error)
    Search(context.Context, SessionQuery) (Page[SessionHit], error)
}
```

  Invarijante:

  1. Save je atomic: journal event, head offset, checkpoint ref i version commitaju zajedno; crash prije commita ostavlja prethodni head. Checkpoint nije “saved” dok artifact digest nije verificiran.
  2. Resume ne ponavlja već commitani provider/tool/irreversible effect. Nedostajući receipt ili environment mismatch ide `UNKNOWN→RECONCILING` kroz S7/P1.4, nikad na slijepi replay.
  3. Fork zapisuje `parent_session_id+fork_offset+parent_digest`; branch vidi točno parent prefix i vlastiti suffix. Nema mutable copyja ni shared head pointera.
  4. System prompt snapshot služi auditu, ali nije automatski ponovno autoritativan: S0.4 migracija i aktualni S6 policy moraju odobriti nastavak. Aktualni policy smije suziti stari session.
  5. Search vraća stvarne event/message refove i bounded bookend/window projekciju; summary je pomoćni S8 blok, ne zamjena za kanonski transcript.

- **Salvage:** iz `/home/matej/NEXUSv2/core/resume.py:13-44` Python→Go portirati baseline checkpoint, per-iteration last-good checkpoint i plan koji odbija terminalni run; spojiti sa S7.3 umjesto zasebnog recovery ownera. Iz `core/session.py:172-257` portirati sanitizirani ID, atomic staging i durable result-marker ideju samo za session artefakte. **Ne portirati `session.py` kao razgovorni session store:** taj modul je shell-command executor s vlastitim thread/Popen timeout ownerom. P0.3 `events` postaje jedina historija; `core/checkpoint.py` ref ide u session savepoint.

- **Ugovor/RED:** P0.3 write-owner i S7.3 durable recovery su obvezni; P0.1 štiti message/block lineage. RED-ovi: `test_save_crash_before_commit_keeps_previous_head`; `test_resume_never_reexecutes_recorded_irreversible_effect`; `test_fork_is_frozen_at_parent_offset`; `test_terminal_session_cannot_resume_as_running`; `test_policy_tightening_blocks_old_session_before_sink`; `test_corrupt_checkpoint_digest_enters_reconciliation`; `test_session_projection_cannot_bypass_journal`.

- **Verifikacija:** provjereno 2026-09-01. gooseova službena [CLI dokumentacija](https://github.com/aaif-goose/goose/blob/main/documentation/docs/guides/goose-cli-commands.md) potvrđuje resume, named/session-ID resume, fork i edit+fork. Hermesova službena [session dokumentacija](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/sessions.md) potvrđuje SQLite+FTS5, pune poruke, parent session ID, resume/search i bounded window/bookend rezultate. Lokalni NEXUS `resume.py` stvarno radi event+checkpoint resume plan; `session.py` je dokazano drugi koncept.

- **Pareto floor:** **PASS uz crash/replay gate.** Dostiže goose user tok i OpenHands event-derived floor, a dodaje digest-bound resume i no-replay effect zaštitu. Strongest counterexample je crash nakon vanjskog učinka, prije local receipta; to nije “resume” nego P1.4 `UNKNOWN` reconciliation i mora ostati blokirano. Najslabija točka je branch UX, ali contract ne ovisi o CLI/TUI/desktop prikazu.

## 9.2 Perzistentna memorija o korisniku i projektu

- **Tip:** MEHANIZAM + OPCIONALNI INDEX ADAPTERI

- **Aspekti:** letta/MemGPT je najbolji izvor za uvijek-prisutne bounded memory blokove i agent edit semantiku; mem0 za extract→add/update/delete i hybrid semantic/keyword/entity/temporal recall; Zep/Graphiti za temporalni KG s episode provenanceom. OpenHuman Memory Tree/TokenJuice daju cold-start scored-tree/SuperContext ideje, ali ostaju runner-up dok matrica ne dokaže kvalitet i trošak na istom korpusu.

- **Sinteza:** `modernc.org/sqlite` WAL spine drži pet scopeova (`PROJECT|TASK|USER|LESSON|CORPUS`), verzije, proposal/approval, tombstone i temporalne veze. FTS5 je baseline; pure-Go embedding adapter i KG projekcija su opcionalni lanes iza capability attestationa. `RecallEngine` radi bounded reciprocal-rank fusion keyworda, recencyja, vectora, temporalnog KG-a, confidencea i 9.4 decay scorea; lane failure degradira uz događaj, ali authz/provenance failure faila zatvoreno.

```go
type MemoryProposal struct {
    ProposalID string
    Candidate Record
    Operation string // ADD|UPDATE|SUPERSEDE|FORGET
    ExpectedVersion uint64
    SourceEvidenceRefs []string
    PayloadHash, PolicyDigest string
    CreatedAt, ExpiresAt time.Time
}
type WritePolicy struct {
    RequireApproval bool // constructor default MUST be true
    AllowedScopes []Scope
    MaxRecordBytes, MaxPending uint64
    PendingTTL time.Duration
}
type WriteApproval struct {
    ApprovalID, ProposalID, PayloadHash, PrincipalID string
    Decision contracts.Decision
    IssuedAt, ExpiresAt time.Time
    Nonce string
}
type RecallQuery struct {
    TenantID, PrincipalID, PrincipalScopeHash string
    Scopes []Scope
    Text string
    AsOf *time.Time
    Limit, TokenBudget uint64
    IncludeForgotten bool
}
```

  Normativni write tok:

```text
untrusted ContextBlocks + source evidence
  -> extract proposal (bez DB mutacije)
  -> P0.1 trust/sensitivity/lineage derivation
  -> dedup/contradiction preview
  -> APPROVAL_PENDING (default)
  -> hash-bound approve|edit-and-approve|deny|expire
  -> BEGIN IMMEDIATE: version guard + memory event + record commit
  -> derived-index outbox -> FTS/vector/KG generation
```

  Invarijante:

  1. `NewWritePolicy()` postavlja `RequireApproval=true`; missing/invalid config također fail-safe ostaje true. Approval vrijedi za exact payload, scope, tenant, sensitivity, operation i version te je single-use. Edit proizvodi novi payload hash i novi approval.
  2. Pending queue je bounded, paginiran, vidljiv i ima TTL/notification; puna queue vraća `MEMORY_APPROVAL_BACKPRESSURE`, ne auto-approve niti tihi drop. Read-only mode ne oglašava write capability modelu.
  3. Recallani zapis izlazi kao P0.1 `ContextBlock`, ne string. Model/tool/web zapis ostaje `UNTRUSTED_EXTERNAL` ili `TOOL_TRUSTED` prema dokazu; scope `USER` ne mijenja trust. S8 ga stavlja u data channel.
  4. FTS/vector/KG entry nosi memory ID+version+tenant. Stale generation nikad nije HIT; rebuild je idempotentan. Exact delete iz jednog logical storea ne smije ukloniti shared derived row koji drugi store/version još referencira.
  5. `MemoryForget` postavlja reversible tombstone i recall-deny u istoj transakciji; original ostaje u cold archiveu. `DataPurge` iz S6.7 preuzima ownership, briše sve storeove/indekse/cache/backupe i tek nakon probea javlja completion.

- **Salvage:** iz `/home/matej/NEXUSv2/core/memory.py:21-147` portirati 5-scope spine, typed entry, SQLite WAL/`BEGIN IMMEDIATE`, hot/cold, generation i centralni untrusted recall fence. Iz `core/memvec.py:23-138` portirati scope-keyed vector identity, keyword+recency+vector+PPR RRF i graceful optional-lane fallback; iz `core/kg.py:81-150` atomic KG generation i bounded local/global traversal. `core/memgit.py:25-104` daje atomic snapshot/restore ideju, ali snapshot ne smije biti skrivena kopija koju P2.1 purge zaboravi. Ne portirati path-derived 16-char store identity niti raw content kao jedini logical key; novi ID uključuje tenant/principal i stable UUID.

- **Ugovor/RED:** Annex **P2.1** je kanonski forget/purge gate; P0.1/P0.3 su provenance/journal gateovi. Obvezni `test_purge_cannot_complete_with_residual_copy`: memorija u hot/vector/cache/backup uz lažni success jednog adaptera mora završiti `PARTIAL/PURGE_INCOMPLETE`, bez `COMPLETED`. Dodatno: `test_memory_write_approval_defaults_on_when_config_missing`; `test_modified_proposal_invalidates_approval`; `test_untrusted_memory_cannot_enter_instruction_channel`; `test_forget_is_reversible_but_excluded_from_recall`; `test_data_purge_overrides_forget_restore`; `test_cross_tenant_vector_and_kg_cannot_alias`; `test_index_failure_preserves_source_record_and_outbox`.

- **Verifikacija:** mem0 službeni [how-it-works](https://github.com/mem0ai/mem0/blob/main/docs/core-concepts/how-it-works.mdx) potvrđuje semantic/keyword/entity/temporal retrieval i eksplicitne update/delete operacije; Zepov OSS [Graphiti](https://github.com/getzep/graphiti) potvrđuje temporalne validity windowe, episode provenance i hybrid graph retrieval. Letta službena [memory-block dokumentacija](https://docs.letta.com/tutorials/attaching-detaching-blocks/) potvrđuje persistent structured blocks. Lokalni memory/memvec/kg mehanizmi postoje, ali trenutni `_forget` fizički briše hot red i zato nije P2.1 ugovor bez redesign-a.

- **Pareto floor:** **PASS tek uz approval-default i residual-copy gate.** Nadskup je letta blokova, mem0 hybrid recalla i Zep temporalnosti, ali ostaje jedan SQLite source of truth. Strongest counterargument je approval fatigue: Hermesovi javni bugovi pokazuju da nevidljiva/unbounded queue čini gate neupotrebljivim. Zato bounded queue, inline preview/edit, notification i read-only mode pripadaju mehanizmu; sigurni default se ne rješava gašenjem gatea.

## 9.3 Učenje iz iskustva

- **Tip:** MEHANIZAM + S11.5 SKILL-PROPOSAL ADAPTER

- **Aspekti:** Hermes je najbolji produkcijski OSS referent za persistent memory, session search i agent-authored skill loop; Reflexion za cited verbalnu analizu neuspjeha; cognee ECL za graph-aware destilaciju i reuse. NEXUS `reasonbank` dodaje važniji floor: samo checker-verified uspjeh smije učiti strategiju, a retrieval mora odbiti nerelevantan/noisy lesson.

- **Sinteza:** `LearningEngine` sluša kanonske terminalne run događaje. Failure može proizvesti kratki `REFLECTION`; samo `VERIFIED` rezultat s nezavisnim evidence-gradeom iz S16.6 može proizvesti `STRATEGY`. Oba su untrusted proposals s exact evidence refovima, nikad činjenice. Kandidat za proceduru šalje se S11.5 `SkillProposalSink`; S9 nema file-write API. Skill aktivacija zahtijeva zaseban user approval, S6.8 scan/lock i P1.1 hash-on-load gate.

```go
type LearningKind uint8 // REFLECTION|STRATEGY|SKILL_CANDIDATE
type LearningCandidate struct {
    CandidateID, RunID, ArtifactRevisionHash string
    Kind LearningKind
    Content contracts.ContextBlock
    Outcome contracts.Outcome
    CheckerEvidenceRefs []string
    Quality float32
    Applicability, Counterexample string
    SourceDigest, PromptVersion string
    State string // OBSERVED|ELIGIBLE|DISTILLED|PROPOSED|APPROVED|ACTIVE|REJECTED
}
type LearningEngine interface {
    Observe(context.Context, contracts.TerminalRun) ([]LearningCandidate, error)
    Retrieve(context.Context, LearningQuery) ([]LearningCandidate, error)
}
type SkillProposalSink interface {
    Propose(context.Context, skills.Proposal) (skills.ProposalID, error)
}
```

  Invarijante:

  1. Workerova proza “uspješno” nije learning signal. Success strategy zahtijeva S16.6 checker evidence nad istim artifact revisionom; failed run ostaje reflection i ne može se označiti proven strategyjem.
  2. Svaki lesson citira run/event/evidence blockove; model output zadržava untrusted trust. Prompt injection u trajectoryju ostaje data i ne može pozvati memory/skill write mimo proposal API-ja.
  3. Retrieval traži apsolutni relevance floor, scope/authz i redundancy filter. Ako ništa ne prelazi floor, vraća prazno; top-k nije obveza da se ubrizga noise.
  4. Skill proposal mora sadržavati generalizabilnu situaciju, proceduru, dokaz, kontra-primjer i eval plan. Jednokratna case note ili istraživački transcript ostaje memory evidence, ne SKILL.md.
  5. Aktivni behavior se ne mijenja tijekom runa koji je proizveo lesson. Novi memory/skill generation vrijedi tek za sljedeći run nakon approval+gateova.

- **Salvage:** iz `/home/matej/NEXUSv2/core/reflexion.py:9-35` portirati kratku cited reflection formu i failure-only trigger, uz P0.1 typed block. Iz `core/reasonbank.py:17-110` portirati quality floor, `VERIFIED` trajectory distill, exact dedup, bounded bank, absolute relevance floor i diversity filter. `core/sleeptime.py:148-209` tool-log facts mogu hraniti operational lessons tek nakon approvala. Ne portirati “LLM output pa direktan `memory.save`”; commit ide kroz 9.2 write gate. Hermesov auto-skill je S11.5 proposal, ne S9 side effect.

- **Ugovor/RED:** P0.1/P0.3 + P1.1 (skill TOCTOU) i 9.2 approval gate su obvezni. RED-ovi: `test_worker_self_report_cannot_create_verified_strategy`; `test_failed_run_never_teaches_success_strategy`; `test_trajectory_injection_cannot_call_memory_or_skill_sink`; `test_skill_candidate_cannot_bypass_s11_5_and_s6_8`; `test_learning_write_approval_defaults_on`; `test_irrelevant_query_returns_no_lesson`; `test_same_run_generation_not_visible_mid_run`; `test_ablation_removing_lesson_does_not_falsely_claim_improvement`.

- **Verifikacija:** Hermesov službeni [repo](https://github.com/NousResearch/hermes-agent) tvrdi persistent memory, session search i autonomous skill creation, a stvarni [`write_approval.py`](https://github.com/NousResearch/hermes-agent/blob/main/tools/write_approval.py) potvrđuje staged memory/skill write mehanizam i da je upstream gate default **OFF**. To je dokaz kandidatske sposobnosti, ne dokaz da smanjuje ponavljanje grešaka. Lokalni Reflexion/ReasonBank kod stvarno ima cited failure lesson, checker-quality gate, dedup, bounded bank i relevance floor. Cognee [skill opis](https://github.com/topoteretes/cognee/blob/main/cognee/skill.md) potvrđuje graph-aware reuse, ali outcome poboljšanje ostaje matrix hipoteza.

- **Pareto floor:** **PASS na safety/traceability flooru; outcome floor je NEPROVEN do A/B evala.** Sinteza zadržava Hermesov loop, Reflexion failure insight i Cognee graph reuse, ali uvodi verifier evidence i approval. Strongest counterexample je uvjerljiv lesson koji učvrsti pogrešnu strategiju; samo checker evidence, kontra-primjer i holdout delta mogu ga promovirati. Bez mjerljivog pada repeated-failure ratea plan ne smije tvrditi “self-improving”.

## 9.4 Memorijska konsolidacija i decay

- **Tip:** MEHANIZAM (`memory` capability; scheduler/lease delegira S3.6/S7)

- **Aspekti:** letta/MemGPT je najbolji referent za sleep-time obradu izvan hot turna; mem0 za additive extraction i contradiction odluke; Zep/Graphiti za temporalni fact history s provenanceom. NEXUS daje konkretan hibrid: turn/idle watermark, epizode→gist, atomic KG refresh, lossless archive, ADD/NOOP/SUPERSEDE, FSRS-lite decay i spreading activation.

- **Sinteza:** S3.6 određuje clock/DST schedule, a S7 izdaje lease, fencing token i budget; 9.4 posjeduje samo memory consolidation state machine. Job claim-a točan journal range/generation, grupira stare događaje u epizode, predlaže gist/facts/KG edgeove, provodi AUDN nad verzioniranim činjenicama i commitira odobreni batch + watermark atomically. `SUPERSEDE` zatvara `valid_to` starog recorda i povezuje novi; ne briše ga. Decay računa samo `RankScore`, a tier migration hot→warm→cold zadržava content-addressed original dostupnim deep recallu.

```go
type ConsolidationRun struct {
    JobID, ScopeID string
    SourceFrom, SourceThrough uint64
    SourceGeneration, PolicyDigest string
    LeaseID string
    Fence durable.FencingToken
    State string // DUE|LEASED|EXTRACTING|PROPOSED|APPROVAL_PENDING|COMMITTING|COMMITTED|FAILED
    ProposalIDs []string
    StartedAt, UpdatedAt time.Time
}
type ResolutionDecision struct {
    Operation string // ADD|NOOP|SUPERSEDE
    NewMemoryID string
    ExistingMemoryIDs []string
    EvidenceRefs []string
    DecisionModelID, PromptVersion string
    Confidence float32
}
type DecayState struct {
    MemoryID string
    AccessCount uint64
    LastAccessAt time.Time
    StabilityDays, Retrievability float64
    CalculatedAt time.Time
}
type Consolidator interface {
    Due(context.Context, ScopeID) (bool, error)
    Propose(context.Context, durable.Lease) (ConsolidationRun, error)
    CommitApproved(context.Context, string, []WriteApproval) error
}
```

  Invarijante:

  1. Watermark napreduje samo ako je cijeli claimed range uspješno obrađen ili dokazano nema kandidata. Extract/store/index/approval failure zadržava range; retry dedupira po `source_range_digest`.
  2. Dvije instance ne konsolidiraju isti scope/range: S7 lease+fencing token prati svaki proposal/commit. In-process mutex nije dovoljan za multi-process WAL spine.
  3. Gist/KG/lesson lineage je unija svih source epizoda. Gist je model-derived untrusted `ContextBlock`; originali ostaju dostupni i temporalni upit može vratiti stanje prije/poslije supersedea.
  4. `ADD` stvara novu verziju; `NOOP` ništa ne mutira; `SUPERSEDE` atomically zatvara staru validity window i dodaje novi record+edge. Neuspjeh nakon new insert ne smije ostaviti staru činjenicu izbrisanu.
  5. Decay, low confidence ili neuporaba smiju samo sniziti rank i premjestiti storage tier. Nikad ne stvaraju `FORGOTTEN`, delete ni purge. Recall touch timestamp je monoton i ne smije ići unatrag zbog stale workera.
  6. Consolidation-derived writeovi također prolaze default-ON approval. Bounded batch approval veže exact list/digeste; parcijalno odobren batch commitira samo odobrene proposal ID-eve i watermark evidentira koje source činjenice ostaju unresolved.

- **Salvage:** iz `/home/matej/NEXUSv2/core/sleeptime.py:54-145` portirati turn/idle due, claimed-turn watermark, serialized digest, bounded fact extraction, ADD/NOOP/SUPERSEDE audit i “failure ne pomiče watermark”; zamijeniti in-process scope lock S7 leaseom. Iz `core/dream.py:16-49` portirati old-episode grouping, gist, lossless archive, interrupted-run recovery i KG refresh. Iz `core/audn.py:29-58` portirati exact/vector dedup i typed decision, ali **ne** `repo._forget(old)`; novi supersede je temporalna verzija. Iz `core/recall.py:22-70` portirati monotoni access, true-half-life rank i lossless cold archive; iz `core/memassoc.py:12-37` bounded spreading activation. `core/memgit.py` snapshot/restore je dodatna undo zaštita, ne zamjena za temporalne recorde ni P2.1 purge.

- **Ugovor/RED:** Annex P2.1 + P0.1/P0.3 obvezni su; S7 fencing vrijedi za background job. RED-ovi: `test_decay_changes_rank_never_existence_or_recallability`; `test_supersede_preserves_temporal_old_fact`; `test_gist_cannot_launder_source_trust`; `test_partial_store_failure_does_not_advance_watermark`; `test_stale_consolidator_fence_cannot_commit`; `test_interrupted_gist_recovers_without_duplicate`; `test_consolidation_batch_requires_exact_approval`; `test_data_purge_removes_gist_kg_association_and_archive`; `test_deep_recall_returns_original_after_one_year`.

- **Verifikacija:** lokalni `sleeptime.py`, `dream.py`, `audn.py`, `recall.py` i `memassoc.py` stvarno implementiraju navedene trigger/watermark, gist/archive, AUDN, half-life rank i association mehanizme. Zepov [Graphiti](https://github.com/getzep/graphiti) potvrđuje validity windows, superseded history i episode provenance. Dokazani lokalni nedostatak je destructive `audn.py:53-55` save-then-forget, pa temporalni supersede mora biti redesign, ne 1:1 port.

- **Pareto floor:** **PASS tek uz lossless/lease/approval RED-ove.** Sinteza je nadskup letta sleep-timea, mem0 odluka, Zep temporalnosti i NEXUS decay/associationa bez drugog scheduler ownera. Strongest counterexample je approval queue koja blokira beskonačnu konsolidaciju; bounded backpressure i vidljiv pending state rješavaju liveness, ali ne opravdavaju auto-approval. Najslabija točka je kvaliteta gist/contradiction odluke; original+lineage ostaju dohvatljivi, a matrix mora mjeriti temporal QA i contradiction precision prije promocije.

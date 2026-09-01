PLAN S16

# S16 — Eval i dokaz kvalitete

## Zajednički vlasnički rez: dokaz ocjenjuje artefakt, ne autora ni njegovu prozu

- **Jedini promotion owner:** `EvaluationGate` odlučuje samo iz revision-bound `EvidenceBundlea` i
  versioniranog acceptance ugovora. Worker, model-judge, benchmark adapter, council, dashboard,
  credit ledger i CI wrapper mogu proizvesti dokaz ili preporuku, ali ne mogu emitirati `PROMOTED`.
- **Izvršenje nije duplicirano:** eval runner otvara izolirani workspace preko S5.2, a svaki agent,
  checker, red-team alat i command pokreće kroz S4/S6/S7. S16 ne implementira drugi subprocess,
  retry, sandbox, network ili timeout sloj.
- **Kanonski zapis:** task, corpus, environment, model/prompt/tool/skill verzije, artifact revision,
  command receipt, score i odluka ulaze kao typed događaji kroz P0.3 journal. Izvještaji, matrice,
  trajectory export i credit statistike su projekcije tih događaja.
- **Podaci:** corpus, fixture, rubric, app-contract, holdout split, attack corpus, ratchet pragovi,
  flaky politika, candidate-matrix limit i profile-specific criteria. Kod posjeduje schema-validaciju,
  non-vacuity, isolation, evidence binding, critical-floor, paired comparison i state-machine.
- **Fail-closed semantika:** `HARNESS_ERROR`, `UNSCORED`, `INCONCLUSIVE`, `FAILED` i `PASSED` su
  različita stanja. Crash checkera, nula pronađenih testova, nedostajući baseline ili nečitljiv rezultat
  nikad se ne pretvaraju u task failure, task pass ili neutralnu nulu.
- **Paket-layout:** `internal/eval/{contract,runner,benchmark,ratchet,redteam,credit,trajectory,checker}`;
  adapteri u `internal/eval/adapters/{swebench,aider,inspect,promptfoo,garak,pyrit,strix,shannon}`.

```go
type EvalIdentity struct {
    CorpusID, CorpusVersion, TaskID, TaskVersion string
    FixtureDigest, ContractDigest, CheckerDigest string
    HarnessRevision, ArtifactRevision string
    ModelID, ModelSnapshot, PromptDigest, ToolsetDigest, SkillsetDigest string
    ConfigHash, EnvironmentDigest string
    Seed uint64
}

type EvidenceKind uint8
const (
    EvidenceDeterministic EvidenceKind = iota
    EvidenceHumanVerified
    EvidenceModelAdvisory
    EvidenceCouncilAdvisory
)

type EvidenceRef struct {
    EvidenceID string
    Kind EvidenceKind
    Identity EvalIdentity
    SourceEventIDs []string
    PayloadDigest string
    ProducedAt time.Time
}

type EvaluationState uint8
const (
    EvalPlanned EvaluationState = iota
    EvalInputsFrozen
    EvalRunning
    EvalScoring
    EvalPassed
    EvalFailed
    EvalUnscored
    EvalInconclusive
)

type EvaluationReceipt struct {
    EvalID string
    Identity EvalIdentity
    State EvaluationState
    Criteria []CriterionResult
    Evidence []EvidenceRef
    CostMinorUnits, LatencyMS, Turns uint64
    GatePolicyVersion string
    ReceiptDigest string
}
```

**Globalne invarijante S16:**

1. Promjena bilo kojeg `EvalIdentity` polja invalidira prethodni receipt; re-score je novi događaj,
   nikad prepisivanje starog rezultata.
2. Svaki required kriterij ima neovisni oracle nad observable stanjem. Model output, summary,
   `verification_status`, broj testova ili council većina nisu sami dokaz.
3. Deterministički RED ili nedostajući critical-gate dokaz ne može kompenzirati prosjek, confidence,
   ni broj perifernih GREEN kriterija.
4. Checker/rubric/hidden fixture nisu dostupni worker workspaceu i nisu mutable tijekom runa.
   Checker radi nad zaključanim artifact revisionom; promjena artefakta nakon freezea daje `STALE_EVIDENCE`.
5. Corpus split i seeds određuju se prije runa. Holdout se ne koristi za tuning; kontaminirani task
   se označava i isključuje iz promotion scorea, ne briše retroaktivno.
6. Svaki runner mora dokazati non-vacuity: NOP pada barem jedan fail-to-pass kriterij, oracle prolazi
   sve required kriterije, a kontrolirana checker mutacija cijeli gate čini RED.

### Verifikacijska matrica — hard limit protiv over-indexinga i critical floor

```go
type CandidateMatrixPolicy struct {
    PolicyVersion string
    TotalCandidateSlots uint32
    MaxSlotsPerProject uint32 // default ceil(5% * TotalCandidateSlots), zaključan prije scoringa
    MaxSlotsPerSubsection uint8 // 1 po projektu u istoj podsekciji
    CriticalGateIDs []string
    RequiredIndependentImplementations uint8
}
```

- Hard limit broji **top-3 slotove**, ne usputne reference, standarde ili salvage napomene. Projekt
  preko capa ostaje u runner-up evidence poolu, ali ne dobiva dodatni ponderirani slot. Cap se ne smije
  povećati nakon što se vidi pobjednik; iznimka zahtijeva novu verziju matrice i ponovni puni score.
- Default `ceil(5% × svi top-3 slotovi)` konkretno bi za 300 slotova dao 15, čime OpenHands/Codex/
  Aider više ne mogu dominirati matricom s 23/20/16 slotova. Ovo je anti-correlation guard, ne tvrdnja
  da je 5% univerzalno optimalno; kalibrira se prije prve matrice, ne prema poželjnom rezultatu.
- Critical floor najmanje uključuje S0.1, S0.2 i S7.2 te svaki Annex-MUST gate: kandidat s najboljim
  ukupnim scoreom pada ako bilo koji primjenjivi critical gate nema reproducibilni GREEN. Na critical
  podsekciji traže se dvije neovisne implementacije/evidence family kad postoje; shared fork se ne
  broji kao neovisnost.
- **RED:** `test_matrix_cap_and_critical_floor_cannot_be_outvoted` daje jednom projektu 16. slot i
  kandidatu maksimalne periferne bodove uz RED S7.2; matrica mora odbiti slot s `PROJECT_SLOT_CAP`
  i promotion s `CRITICAL_GATE_FAILED`.

## 16.1 Task-level benchmark i deliverable-level app-contract

- **Tip:** MEHANIZAM BENCH RUNNERA + DATA CONTRACTI + ADAPTERI ZA JAVNE BENCHMARKOVE

- **Aspekti:** SWE-bench daje realne repo issue→patch taskove i test-patch grading; Aider suite daje
  brz model/edit-format comparative signal; Inspect daje opći dataset/solver/scorer/sandbox okvir.
  NEXUS `bench.py` daje fresh fixture, hidden checker izvan workspacea, F2P/P2P, `UNSCORED`, NOP/oracle
  non-vacuity te odvojeni repairability pokušaj. NEXUS app-contract dodaje ono što coding benchmarki
  ne pokrivaju: traženi ekrani, tokovi, persistence i negativni slučajevi kao versionirani deliverable.

- **Sinteza:** vlastiti Go `BenchmarkRunner` je kanonski owner. SWE-bench/Aider/Inspect samo mapiraju
  njihove task/result formate u isti `TaskSpec`; ne spajamo tri runtimea. Task contract se generira i
  zaključava u datoteke **prije prvog edit eventa**. Worker dobiva goal i javni acceptance dio, ali
  hidden oracle/checker ostaje read-only izvan njegova mounta.

```go
type TaskSpec struct {
    SchemaVersion uint16
    TaskID, Version string
    Goal contracts.ContextBlock
    FixtureDigest, ContractDigest string
    PublicCriteria, HiddenCriteria []CriterionSpec
    FailToPass, PassToPass []string
    Budgets s7.ResourceBudget
    NetworkPolicy string
    RequiredCapabilities []string
}

type AcceptanceContract struct {
    ContractID, Version, ArtifactKind string
    FrozenBeforeEventID string
    Requirements []Requirement
    Screens, Flows, Persistence, NegativeCases []CriterionSpec
    EvidenceRules []EvidenceRule
    Digest string
}

type CommandReceipt struct {
    CommandID, ArgvDigest, EnvironmentDigest string
    ArtifactRevision, CheckerDigest string
    ExitCode int
    StdoutDigest, StderrDigest string
    StartedAt, FinishedAt time.Time
    S7AttemptGrantID string
}

type BenchmarkRunner interface {
    ValidateTask(context.Context, TaskSpec) (ValidationReceipt, error)
    Run(context.Context, TaskSpec, AgentRef, uint32) (EvaluationReceipt, error)
}
```

  Svaki epoch dobiva fresh S5.2 worktree/fixture i pinned identity. Agent timeout ocjenjuje postojeći
  workspace; checker crash/timeout je `UNSCORED`. `pass@k` (barem jedan uspjeh) i `pass^k` (svi
  uspjesi) izvještavaju se odvojeno. Feedback-repair na istom workspaceu je posebna `repairability`
  metrika, ne lažno nazvan fresh pass@2.

  App-contract zahtijeva dokaz po requirement ID-u, ne broj testova. `implemented` nije `verified`;
  screenshot bez interakcijskog dokaza ne zadovoljava flow, unit test bez realnog persistence boundaryja
  ne zadovoljava persistence. `not_applicable` traži policy-dopušten reason+review. Contract promjena
  nakon prvog edit eventa stvara novi run, ne pomiče goalpost.

- **Salvage:** iz `/home/matej/NEXUSv2/evals/bench.py:30-105,257-359,366-412` Python→Go portirati
  `HarnessError≠task fail`, hidden-checker granicu, F2P/P2P, fresh epoch, `pass@k/pass^k` i NOP/oracle
  validaciju. Iz `bench.py:46-85,203-254` portirati clean-env/process-tree/sandbox namjeru, ali samu
  provedbu delegirati S4/S6/S7. Iz `core/app_contract.py:24-67,94-143,512-552,1399-1480` portirati
  bounded schema, duplicate/shape/path obrane, revision/evidence refs i requirement evaluation.
  **Ne portirati** heuristic failure label kao ground truth, SHA-1 snapshot, `judge-only` kao zamjenu
  deterministic checkeru ni Python checker protocol kao kernel schema.

- **Ugovor/RED:** P0.1/P0.3, P0.4 za potrebne capabilityje, S5/S6/S7 i 16.6 promotion gate.
  Glavni RED `test_partial_demo_cannot_satisfy_app_contract` daje aplikaciju s ekranom i unit testom,
  ali bez traženog save→restart→reload toka; test-count je GREEN, no contract mora vratiti
  `REQUIRED_FLOW_UNVERIFIED`. Dodatno: `test_nop_fails_and_oracle_passes`,
  `test_checker_crash_is_unscored_not_failure`, `test_worker_cannot_read_or_modify_hidden_checker`,
  `test_contract_cannot_change_after_first_edit`, `test_pass_at_k_excludes_unscored_without_fabrication`
  i `test_feedback_retry_is_not_reported_as_fresh_pass_at_two`.

- **Verifikacija:** [SWE-bench](https://github.com/SWE-bench/SWE-bench) stvarno ocjenjuje repo issue
  kroz patch/test okolinu; [Aider benchmark](https://aider.chat/docs/leaderboards/) stvarno uspoređuje
  coding izvedbu i edit formate; [Inspect](https://inspect.aisi.org.uk/) stvarno ima task/dataset/
  solver/scorer i sandbox adaptere. Inspect dokumentacija izričito kaže da provisioning sandboxa ne
  znači da sav scorer/runner kod radi u sandboxu, pa ga ne tretiramo kao automatsku containment garanciju.
  Lokalni NEXUS kod potvrđuje navedene salvage aspekte; Go parity fixtures moraju dokazati iste ishode.

- **Pareto-floor:** **PASS uz vlastiti runner i app-contract.** Zadržava SWE-bench realizam, Aiderovu
  brzinu i Inspectovu općenitost, a dodaje deliverable-level dokaz koji ne može proći djelomični demo.
  Najjači protuargument je da hidden checker može kodirati pogrešan cilj; zato NOP+oracle+review
  validiraju checker, a contract je frozen artifact prije rada, ne nevidljivi pokretni kriterij.

## 16.2 Regression, paired baseline i ratchet u CI

- **Tip:** MEHANIZAM PROMOTION RATCHETA + CI ADAPTERI; PROMPTFOO JE PRIMARNI PROMPT/RAG ADAPTER

- **Aspekti:** promptfoo daje deklarativne evale i CI/red-team lane; DeepEval daje pytest-style LLM
  kriterije; Opik daje tracking kroz verzije. NEXUS regression tester daje jednostavan snapshot/delta,
  ali jedna agregatna pass-rate vrijednost ne vidi critical regression, paired task identitet, flake,
  cost, latency ili turn budžet. Naš ratchet zadržava vanjske executore kao adaptere, ali odluku donosi
  nad typed receiptima istog task/config/model snapshot para.

- **Sinteza:** baseline je immutable, digestiran i promotion-authorized artefakt. Candidate i baseline
  rade na istim taskovima, splitu, seeds, provider snapshotu, budgetu i okolišu; nedostajući par je
  `INCOMPARABLE`, ne nula. Ratchet evaluira task-level delta i confidence interval, ali critical gates
  su zero-tolerance. Cost/latency/turn limit je zaseban constraint, ne dio score prosjeka.

```go
type BaselineManifest struct {
    BaselineID, Revision, CorpusDigest, ConfigHash string
    ModelSnapshot, EnvironmentDigest string
    Receipts []string
    ApprovedBy, Signature string
}

type RatchetPolicy struct {
    Version string
    MinPairedTasks uint32
    MaxFailureDeltaPPM int64
    MaxCostDeltaPPM, MaxLatencyDeltaPPM, MaxTurnDeltaPPM int64
    CriticalGateIDs []string
    FlakyPolicy FlakyPolicy
    ConfidencePPM uint32
}

type RatchetDecision struct {
    BaselineID, CandidateRevision, PairSetDigest string
    TaskDeltas []TaskDelta
    CriticalFailures []string
    BudgetRegressions []string
    State string // PROMOTE | REJECT | INCONCLUSIVE
    EvidenceDigest string
}
```

  Flake se mjeri ponovljenim pinned seedovima i čuva failing seed/trace. Retry ne briše prvi failure.
  Task iznad dopuštenog variancea ide u `QUARANTINED_FLAKY` i ne ulazi ni u brojnik ni nazivnik dok
  nema vlasnika/expiryja; previše karantene čini gate `INCONCLUSIVE`. Baseline se nikad automatski ne
  zamjenjuje candidateom; promotion receipt i korisnik/release policy eksplicitno ga mijenjaju.

  Critical floor i matrix hard-limit iz uvoda obvezni su CI pre-check. Aggregate napredak ne može
  preglasati S0/S6/S7/Annex regresiju, podatkovni leak, test-discovery=0 ni app-contract failure.

- **Salvage:** iz `/home/matej/NEXUSv2/evals/regression_tester.py:11-26` portirati jednostavan
  baseline→candidate delta prikaz samo kao UI. **Ne portirati** mutable `baseline.json`, “no baseline”
  kao prolazan status niti ukupnu `rate` kao promotion odluku. Iz `evals/bench.py:322-359` portirati
  poštenu `None=UNSCORED` statistiku i pass reliability; raw epoch receipts ostaju auditabilni.

- **Ugovor/RED:** P0.3 i critical P0.4 closure. Glavni RED
  `test_aggregate_gain_cannot_mask_critical_regression` poboljša 99 perifernih taskova, ali ablira jedan
  S7 fencing test; ratchet mora vratiti `REJECT`. Dodatno: `test_missing_baseline_is_inconclusive`,
  `test_unpaired_identity_cannot_enter_delta`, `test_retry_does_not_erase_flake`,
  `test_zero_discovered_tests_is_red`, `test_cost_latency_turn_limits_are_independent_constraints`,
  `test_baseline_cannot_self_replace_without_promotion_receipt` i matrix cap RED iz uvoda.

- **Verifikacija:** [promptfoo](https://github.com/promptfoo/promptfoo) stvarno nudi deklarativne
  prompt/agent/RAG evale, red-team i CI; [DeepEval](https://github.com/confident-ai/deepeval) nudi
  test-style LLM evaluaciju; [Opik](https://github.com/comet-ml/opik) prati eval/experiment rezultate.
  Njihova integracija nije dokaz našeg paired identiteta ili critical floora; mutation gate mora
  namjerno slomiti critical test i promatrati konačni CI status.

- **Pareto-floor:** **PASS uz immutable paired receipt.** Vanjski alati zadržavaju authoring i UI
  prednosti, a vlastiti mali ratchet uklanja false-green agregata. Najslabija točka je statistička snaga
  skupih model runova; nedovoljan N iskreno daje `INCONCLUSIVE`, nikad automatski promotion.

## 16.3 Red-team i adversarial testiranje

- **Tip:** MEHANIZAM ENGAGEMENT/ORACLE UGOVORA + ADAPTERI; STRIX JE PRIMARNI DINAMIČKI PENTEST PICK

- **Aspekti:** garak daje široke LLM probe/detector kataloge; PyRIT daje multi-turn orkestraciju,
  konvertere i attack strategije; promptfoo daje deklarativni CI red-team. **Strix (`usestrix/strix`)**
  materijalno širi listu: dinamički pokreće cilj, koordinira pentest agente i traži working PoC.
  Shannon daje source-aware web/API pentest i reproducibilne exploit nalaze. NEXUS redteam ima dobar
  intermediate-control oracle za fencing, secret redaction, egress i canary, ali nema stvarni dynamic
  target exploit lane.

- **Sinteza:** tri lanea iza jednog ugovora, bez miješanja rezultata:

  1. `CONTROL_ABLATION` — deterministički lokalni testovi PEP/fencing/redaction/egress/tenant gatea;
  2. `MODEL_ATTACK` — garak/PyRIT/promptfoo probe nad model/harness površinom;
  3. `DYNAMIC_TARGET` — Strix primarno, Shannon runner-up, samo nad disposable izoliranim targetom.

```go
type Engagement struct {
    EngagementID, PolicyVersion string
    Lane string
    TargetArtifactDigest, TargetRevision string
    AllowedHosts, AllowedCIDRs, AllowedPorts []string
    DeniedActions []string
    CredentialRefs []contracts.SecretRef
    ApprovalIntentID, ApprovalPayloadHash string
    StartsAt, ExpiresAt time.Time
    Budget s7.ResourceBudget
    CleanupPlanID string
}

type AttackCase struct {
    CaseID, TaxonomyID, PayloadDigest, OracleVersion string
    Preconditions []string
    ExpectedControl, ExpectedSink string
}

type Finding struct {
    FindingID, CaseID, TargetRevision string
    Status string // EXPLOITED | BLOCKED | POTENTIAL | FALSE_POSITIVE | UNSCORED
    EvidenceRefs []EvidenceRef
    ReproducerDigest, EffectDigest string
    Severity, CWE string
}

type RedTeamAdapter interface {
    Plan(context.Context, Engagement) (AttackPlan, error)
    Execute(context.Context, s7.AttemptGrant, AttackPlan) ([]Finding, error)
}
```

  Engagement target se resolvea i pinna prije approvala; redirect, DNS promjena ili host izvan
  allowliste daje nula socketova. DYNAMIC lane je default OFF, zahtijeva explicit `redteam-dynamic`
  capability closure, S6.9 approval, disposable worktree/container/network i cleanup proof. Nikad se
  ne usmjerava na produkciju po inferred URL-u. Credentiali ostaju broker refs i scopeani su na target.

  Nalaz postaje `EXPLOITED` samo ako neovisni oracle reproducira observable effect na istom target
  revisionu; agentova proza/CVSS nije dokaz. `POTENTIAL` je vidljiv, ali ne blokira release kao
  verificirana ranjivost bez policy odluke. Svaki attack corpus ima benign negative controls i seeded
  vulnerable positives, pa se mjere i sensitivity i false-positive rate.

- **Salvage:** iz `/home/matej/NEXUSv2/evals/redteam.py:15-93,130-203` Python→Go portirati
  intermediate oracle: payload mora preživjeti kao fenced data, secret se redaktira na granici,
  URL staje na egress PEP-u i canary se traži na svakom outbound sinku. Portirati obfuscation corpus,
  ali ne `_naive_blocklist` kao obranu. Iz `redteam.py:144-190` portirati discriminator koji razlikuje
  bare canary obedience od citiranja napada u odbijanju. External tool output ostaje untrusted finding.

- **Ugovor/RED:** P0.1/P0.3/P0.4, S6.2/S6.3/S6.9, S7 budget/cancel i P1.4 za učinke. Glavni RED
  `test_dynamic_pentest_cannot_escape_approved_target` daje redirect/DNS odgovor na privatni
  neodobreni IP; connection spy mora ostati 0 i run završiti `TARGET_SCOPE_VIOLATION`. Dodatno:
  `test_control_ablation_detects_removed_fence`, `test_seeded_vulnerability_requires_reproducible_poc`,
  `test_benign_target_bounds_false_positive`, `test_agent_claim_without_effect_is_potential_not_exploited`,
  `test_redteam_credentials_never_reach_prompt_or_report`, `test_cancel_kills_attack_process_tree`
  i `test_cleanup_unknown_blocks_next_dynamic_engagement`.

- **Verifikacija:** [Strix](https://github.com/usestrix/strix) je Apache-2.0 OSS i službeni repo
  navodi dynamic execution, multi-agent pentest, PoC validaciju, Docker sandbox i headless CI; to ga
  čini jačim kandidatom od čisto prompt-level skenera za DYNAMIC lane, ali Docker/runtime znači da
  nije single-binary jezgra. [Shannon](https://github.com/KeygraphHQ/shannon) je AGPL-3.0 OSS web/API
  pentest kandidat i također tvrdi working-PoC nalaze; licenca i runtime izoliraju ga kao subprocess
  adapter, bez koda u binaryju. [garak](https://github.com/NVIDIA/garak),
  [PyRIT](https://github.com/Azure/PyRIT) i [promptfoo](https://github.com/promptfoo/promptfoo) ostaju
  stvarni specijalizirani model/CI alati. Nijedna README tvrdnja ne dokazuje detection rate; seeded
  positive/negative corpus i scope-escape test to mjere lokalno.

- **Pareto-floor:** **PASS uz tri odvojena lanea.** Zadržava breadth garak/PyRIT/promptfooa,
  dodaje Strixov stvarni exploit floor i Shannon kao neovisni runner-up, a NEXUS intermediate oracles
  sprječavaju output-only false green. Najveći rizik je blast radius dinamičkog pentesta; zato je
  capability default OFF, target digest-bound, network deny-default i cleanup unknown hard stop.

## 16.4 Feedback flywheel i kanonski credit ledger

- **Tip:** MEHANIZAM KANONSKOG CREDIT LEDGERA + BOUNDED BANDIT POLICY; POTROŠAČI SU 2.6/9.3/11.5

- **Aspekti:** Langfuse daje trace annotation/curation; Opik experiment/eval capture; AgentOps session
  replay. NEXUS `reward.py` daje jednostavan verified-intent epsilon-greedy izbor, a `credit.py` daje
  per-exposure dedup i Laplace-smoothed effectiveness. Presudna rupa: `reward.record()` javno prima
  proizvoljan float bez evidence receipta, a `credit.reward()` prima običan `passed bool`; docstring
  “verified only” nije provedena granica.

- **Sinteza:** prije odluke consumer appenduje immutable `ExposureRecorded` s eligible kandidatima,
  izabranim kandidatom, propensityjem, policyjem i artifact/config hashom. Nakon 16.1/16.6 samo
  `EvaluationReceipt{PASSED|FAILED}` odgovarajućeg revisiona može autorizirati `CreditAssigned`.
  Ledger je niz kanonskih događaja kroz journal; SQLite tablice su rebuildable projekcija.

```go
type CreditDimension uint8 // ROUTE | MEMORY | SKILL | PROMPT

type ExposureRecorded struct {
    ExposureID, RunID string
    Dimension CreditDimension
    ContextBucketHash string
    EligibleCandidateDigests []string
    SelectedCandidateDigests []string
    SelectionPolicyVersion string
    PropensityPPM uint32
    Exploration bool
    ArtifactRevision, ConfigHash string
}

type CreditAssigned struct {
    CreditID, ExposureID, EvaluationReceiptID string
    ArtifactRevision, EvidenceDigest string
    RewardPolicyVersion string
    RewardPPM int32
    CostMinorUnits uint64
    AttributionKind string // OBSERVATIONAL | RANDOMIZED_CONTRAST | HUMAN_ADJUDICATED
    ReversesCreditID *string
}

type SelectionPolicy struct {
    Version string
    EpsilonPPM uint32
    MinObservations uint32
    MaxCreditInfluencePPM uint32
    SafetyCriticalExploration bool // kernel maksimum false; config ne može proširiti
}
```

  Duplicate receipt/exposure par je idempotentan. Revoked/corrected evaluation stvara reversal event,
  ne UPDATE. Credit je bounded nudge među već relevantnim/allowed kandidatima; nikad ne preskače S0.5,
  auth, trust, skill lifecycle ili router capability floor. Epsilon RNG je CSPRNG u produkciji i seeded
  u evalima; exploration ima zaseban S7 cost/risk budget i defaultno je zabranjen za irreversible,
  security-critical i tenant-policy-sensitive odluke.

  `2.6` bira samo iz routeova koji su već capability/policy eligible; `9.3` smije predložiti memory/
  experience rank; `11.5` smije predložiti skill canary/update. Nijedan consumer ne aktivira novu
  rutu/memoriju/skill bez vlastitog gatea. Uspjeh koreliranog bundlea ne dokazuje uzročnost svakog
  člana: takav credit je `OBSERVATIONAL`; causal claim zahtijeva randomized/shadow contrast ili ljudsku
  adjudikaciju. Feedback primjer ulazi u corpus tek nakon redakcije, consent/retention i holdout dedupa.

- **Salvage:** iz `/home/matej/NEXUSv2/core/reward.py:23-42` portirati mean/count i epsilon-greedy
  oblik; iz `core/credit.py:28-76` portirati per-run dedup, `used/helpful` i Laplace neutral `0.5` kao
  jednu projection metriku. **Ne portirati** `reward.py:13-20` javni float writer, globalni RNG,
  content-SHA1 identitet ni `credit.reward(items, passed)` boolean autoritet. Candidate ID je digest
  versioniranog route/memory/skill artefakta; svaki write zahtijeva verified receipt.

- **Ugovor/RED:** P0.3, P0.4 i 16.6. Glavni RED
  `test_model_self_report_cannot_mint_credit` pozove credit API s `passed=true`/model “success”, ali
  bez valjanog revision-bound receipt-a; mora dobiti `UNVERIFIED_REWARD_SOURCE`, a ledger/projekcija
  ostaju nepromijenjeni. Dodatno: `test_receipt_revision_must_match_exposure`,
  `test_duplicate_receipt_cannot_double_credit`, `test_retracted_outcome_appends_reversal`,
  `test_credit_cannot_make_ineligible_route_skill_or_memory_eligible`,
  `test_exploration_never_crosses_safety_policy` i `test_observational_bundle_is_not_labeled_causal`.

- **Verifikacija:** lokalni `reward.py:1-5` tvrdi verified-only namjeru, ali `record` potpis i tijelo
  ne provjeravaju provenance; `credit.py:28-52` prima samo boolean. To je dokaz za port algoritma, ne
  membrane. Langfuse/Opik/AgentOps annotation/replay mogu hraniti curation queue, ali ne daju sami
  revision-bound credit autoritet. A/B fixture s lažnim receiptom, duplicate deliveryjem i reversalom
  mora dokazati ledger prije ikakvog online učenja.

- **Pareto-floor:** **PASS nakon dodavanja receipt membrane.** Zadržava NEXUSovu malu i razumljivu
  bandit jezgru te observability alata, ali uklanja self-report poisoning i lažni causal attribution.
  Najjači counterargument je spor learning pod strogo verificiranim signalom; to je namjerna cijena —
  manjak dokaza ostaje neutralan, ne puni harness pogrešnim samouvjerenjem.

## 16.5 Trajectory export za destilaciju (`trajectory-export` opcionalni flag)

- **Tip:** MEHANIZAM PRIVACY-SAFE JOURNAL PROJEKCIJE + FORMAT ADAPTERI

- **Aspekti:** SWE-gym daje coding okruženja i agent trajectory podatke; Axolotl konzumira SFT/DPO
  datasete; ShareGPT/JSONL je široko podržan interchange oblik. Najbolji graft nije trening runtime u
  harnessu, nego jedan kanonski typed trajectory iz S0/P0.3 događaja i lossless/explicitly-lossy
  adapteri. Sam trening ostaje izvan harnessa.

- **Sinteza:** exporter čita samo terminalne runove i pinned `EvaluationReceipt`. Akcije, observationi,
  tool call/result, artifact diff refs, costs i verifier outcomes zadržavaju causal order/provenance.
  Raw hidden chain-of-thought se nikad ne izvozi; reasoning samo ako je eksplicitni user-visible summary.
  Default dataset uključuje samo verificirane ishode, no failure trajectories se mogu izvesti zasebno
  s jasnom labelom radi DPO/debuga.

```go
type TrajectoryManifest struct {
    DatasetID, SchemaVersion, CorpusVersion string
    SourceJournalID string
    StartOffset, EndOffset uint64
    InclusionPolicyVersion, RedactionPolicyVersion string
    ConsentReceiptIDs []string
    LicensePolicyVersion string
    Shards []ShardManifest
    ManifestDigest, Signature string
}

type TrajectoryStep struct {
    Sequence uint64
    EventID, ParentEventID, ActionID string
    Kind string
    InputBlocks, OutputBlocks []contracts.ContextBlock
    ToolID, ToolSchemaHash string
    ArtifactRevisionBefore, ArtifactRevisionAfter string
    Outcome string
    EvidenceRefs []EvidenceRef
}

type TrajectoryExporter interface {
    Export(context.Context, ExportRequest) (TrajectoryManifest, error)
}
```

  `trajectory-export` closure kroz P0.4 zahtijeva S6.7 governance, P0.3 redaction, tenant authorization,
  provenance/licence policy, S7 budget i S6.3 ako napušta host. Export je atomic staged shard→verify
  count/digest/schema→commit; partial shard nikad ne dobiva manifest. Svaki adapter (`ShareGPTJSONL`,
  `SWEGym`, `AxolotlSFT/DPO`) objavljuje koja polja gubi. Tenant/principal/secret/workspace absolute
  path se pseudonimiziraju ili izostavljaju; canary scan ide nakon serializationa.

  DATA_PURGE mora pronaći dataset/shard preko source lineagea, obrisati/crypto-eraseati sve kopije i
  opozvati manifest; prethodni digest ostaje minimalni audit dokaz prema S6.7. Holdout taskovi se ne
  izvoze u training pool bez eksplicitnog declassification događaja, čime se sprječava benchmark leak.

- **Salvage:** iz `evals/bench.py:257-332` portirati epoch/action/result identitet i verified outcome,
  a iz `loop_engine.py` samo typed action/observation/evidence redoslijed. NEXUS nema dokazani privacy-
  safe trajectory manifest owner; Python session/transcript dump nije dovoljan i ne portira se kao
  dataset. S9 memory export također nije training consent.

- **Ugovor/RED:** P0.1 lineage, P0.3, P0.4 i P2.1 purge. Glavni RED
  `test_trajectory_export_blocks_secret_and_holdout_leak` ubaci canary secret i holdout task u validan
  run; serialized shards i manifest ne smiju sadržavati canary/task sadržaj, a export mora vratiti
  `EXPORT_POLICY_DENIED` za nedeklasificirani holdout. Dodatno:
  `test_export_requires_active_capability_closure`, `test_partial_shard_has_no_committed_manifest`,
  `test_adapter_declares_lossy_fields`, `test_purge_revokes_every_derived_shard` i
  `test_replay_produces_byte_identical_manifest_with_injected_clock`.

- **Verifikacija:** [SWE-gym](https://github.com/SWE-Gym/SWE-Gym) stvarno pruža coding agent
  training/eval okruženje; [Axolotl](https://github.com/axolotl-ai-cloud/axolotl) stvarno konzumira
  standardne SFT/preference datasete. “ShareGPT JSONL” je praktičan format, ne dovoljno strog standard;
  zato je samo adapter, a naš versionirani manifest je kanonski. Format smoke ne dokazuje privacy,
  purge ni benchmark contamination; serialized-byte canary i delete probe su gate.

- **Pareto-floor:** **PASS kao opcionalna projekcija.** Daje kompatibilnost s training alatima bez
  uvođenja trainer/runtime ovisnosti ili drugog event formata. Najveći rizik je nepovratno curenje
  podataka/holdouta; zato flag nije core default, closure je obvezan i export bez consent/licence/
  lineage dokaza faila prije prvog shard writea.

## 16.6 Neovisna provjera, anti-sycophancy i confidence

- **Tip:** JEZGRENİ MINIMALNI DETERMINISTIČKI CHECKER + ADVISORY REVIEW ADAPTERI; COUNCIL JE
  `multi-agent` STRATEGIJA, NE PROMOTION OWNER

- **Aspekti:** Inspect scorers daju opće artifact/custom scoring; UQLM daje uncertainty/logprob
  signale; DeepEval daje semantičke evaluator obrasce. NEXUS `loop_engine` daje najvažniji coding
  diferencijator: doer i checker su odvojeni, a harness gleda stvarni diff i exit code. NEXUS
  `confidence.py` daje devil's-advocate/reframing, ChainPoll parse discipline i flip signal, ali model
  consistency/self-confidence nije istina. NEXUS council daje više perspektiva; P2.7 tek čini
  neovisnost, quorum i dissent dokazivima.

- **Sinteza:** `ArtifactChecker` radi nad read-only snapshotom točnog revisiona. Harness, ne worker,
  računa base→current diff, write-set, file digeste i command exit receipte. Required deterministic
  kriteriji moraju svi proći. Semantic checker može vratiti `FAIL` ili `UNCLEAR`; bez neovisnog
  executable/human oracla ne smije preokrenuti deterministic RED u GREEN.

```go
type EvidenceBundle struct {
    BundleID string
    BaseRevision, ArtifactRevision, DiffDigest, WriteSetDigest string
    ContractDigest, CheckerDigest, EnvironmentDigest string
    Commands []CommandReceipt
    Artifacts []ArtifactEvidence
    Criteria []CriterionEvidence
    SourceEventIDs []string
    SealedAt time.Time
}

type CheckVerdict uint8
const (
    CheckPass CheckVerdict = iota
    CheckFail
    CheckUnclear
    CheckHarnessError
    CheckStale
)

type CriterionResult struct {
    CriterionID string
    Verdict CheckVerdict
    EvidenceRefs []EvidenceRef
    OracleID, OracleVersion string
    SafeReasonCode string
}

type ArtifactChecker interface {
    Freeze(context.Context, ArtifactRef, AcceptanceContract) (EvidenceBundle, error)
    Check(context.Context, EvidenceBundle) ([]CriterionResult, error)
    Decide(context.Context, EvidenceBundle, []CriterionResult) (EvaluationReceipt, error)
}
```

  Stateovi su `PENDING→EVIDENCE_FROZEN→CHECKING→{ACCEPTED|REJECTED|INCONCLUSIVE|STALE}`. Worker
  nikad ne bira checker, ne vidi hidden kriterije i ne piše command receipt. `FinishAction` iz 3.1
  samo traži provjeru; `RunSucceeded` nastaje tek nakon `ACCEPTED` nad istim revisionom.

  Confidence se veže uz specifični claim+evidence, nosi sample count/calibration set i služi samo za
  prioritizaciju dodatne provjere. `votes=0`, missing logprobs ili flip daju `UNCLEAR`, ne numeric pass.
  Model reviewer input je P0.1 untrusted data i prompt-injection fenced. Praise/agreement nema score;
  finding bez evidence refa je advisory.

  Council je adapter na već definirani S12.6/P2.7: svi članovi dobivaju isti `ArtifactRevision` i
  sealed `EvidenceBundle`; output čuva majority+dissent+unresolved. Council `VERIFIED` znači samo da
  je protokol deliberacije valjan. `EvaluationGate` i dalje zahtijeva minimalni checker; council može
  dodati FAIL/UNCLEAR ili prioritet, nikad sam dati ACCEPTED.

- **Salvage:** iz `/home/matej/NEXUSv2/loop_engine.py:1-18,47-93,187-245,288-310,328-391`
  Python→Go portirati doer/checker separation, harness-owned verify exit, bounded kriterije i evidence
  merge; odbaciti worker-declared file list, prvih-pet-file diff i modelov `verification_status` kao
  autoritet. Iz `core/confidence.py:18-32,45-100,130-141` portirati devil prompt, critical reframe,
  final-line parse i flip kao advisory signal. **Ne portirati** `score(): 0.7*poll+0.3*self-reflect`
  kao promotion confidence niti exception→quiet false. Iz `managers/council.py:36-73` portirati samo
  trofazni UX; kanonska provedba je već S12.6, a silent member-drop se odbacuje.

- **Ugovor/RED:** P0.1/P0.3, **P2.7 council**, S3.1/S3.5 i app-contract. Glavni RED
  `test_checker_cannot_turn_exit_one_green`: worker i tri reviewera napišu “passed”, council jednoglasno
  potvrdi, ali harness-owned command receipt ima exit 1; rezultat mora biti `REJECTED`, bez
  `RunSucceeded/CreditAssigned`. Dodatno: `test_worker_claimed_files_cannot_hide_real_diff`,
  `test_artifact_change_after_freeze_is_stale`, `test_zero_checker_results_is_harness_error`,
  `test_model_judge_can_fail_but_not_override_deterministic_fail`,
  `test_council_verified_cannot_satisfy_checker_gate`, Annex P2.7
  `test_council_cannot_drop_sealed_material_dissent` i `test_checker_ablation_turns_full_gate_red`.

- **Verifikacija:** lokalni `loop_engine` stvarno odvaja doer/checker i koristi stvarni verify izlaz,
  ali sadrži i slabije worker/file heuristike koje se ne portiraju. `confidence.py:52-100` stvarno
  faila unparseable vote u “nema valjanih glasova”, dok `score():122-127` ipak miješa self-reflection;
  zato je samo advisory. `managers/council.py:36-73` stvarno ima persona→anonymous review→chair, ali
  tiho dropa kvarove; S12.6/P2.7 parity gate zatvara tu rupu. Inspect/UQLM/DeepEval feature nije dokaz
  neovisnosti: mutation test mora pokazati da proza i council ne mogu preglasati exit/diff.

- **Pareto-floor:** **PASS uz deterministic core kao neuklonjivi floor.** Dobiva Inspectovu scorer
  fleksibilnost, UQLM/ChainPoll uncertainty signal i P2.7 council širinu, ali coding completion ostaje
  stroži od svih: git diff + stvarni exit + requirement evidence pobjeđuju prozu. Najjači protuargument
  je da neki semantički kriteriji nemaju izvršni oracle; tada je pošten rezultat `UNCLEAR` ili human
  gate, ne model-generated GREEN.

## S16 zajednički verification gate

```text
RED 1: NOP fixture ili zero-discovery prođe -> task/corpus INVALID, promotion nema receipt.
RED 2: worker kaže success, checker exit=1 -> REJECTED bez RunSucceeded i bez kredita.
RED 3: candidate poboljša prosjek, critical S7.2 pada -> ratchet REJECT.
RED 4: model self-report ili council-only verdict pokušava kredit -> UNVERIFIED_REWARD_SOURCE.
RED 5: dynamic pentest redirecta izvan approved targeta -> 0 neodobrenih socketova.
RED 6: trajectory s canary tajnom/holdoutom -> nema committed sharda ni egressa.
RED 7: jedan projekt pređe matrix cap -> slot invalid; cap se ne mijenja nakon scorea.
```

Release gate koristi frozen mini-corpus s: jednim coding F2P/P2P taskom, jednim app-contractom koji
namjerno ima djelomični UI, jednim critical fencing taskom, seeded vulnerable+benign red-team parom,
lažnim credit receiptom i trajectory canaryjem. Najprije se kontroliranom mutacijom ablira svaki
oracle/critical gate i promatra konačni RED, zatim se vraća ispravan owner i očekuje GREEN. Svi model
laneovi imaju pinned snapshot/seed gdje provider dopušta; neponovljivi live rezultat ostaje zasebna
canary evidencija, nikad jedini release dokaz.

## Konačni Pareto sudac S16

- **Floor najboljih kandidata:** SWE-bench real tasks; Aider brzi edit/model benchmark; Inspect opći
  runner/scorer; promptfoo/garak/PyRIT attack breadth; Strix/Shannon dynamic PoC; Langfuse/Opik
  curation; SWE-gym/Axolotl export; UQLM/council dodatna neovisna perspektiva.
- **Naš dodatni floor:** deliverable app-contract, NOP+oracle non-vacuity, paired immutable ratchet,
  critical gates, hard project cap, scope-bound dynamic pentest, receipt-bound credit, privacy-safe
  trajectory i evidence-graded coding checker koji proza ne može preglasati.
- **Odbijeno kao nepotrebna/opasna složenost:** vlastiti general-purpose eval DSL uz `TaskSpec`,
  vlastiti trainer, vlastiti pentest agent, tri paralelna benchmark runtimea, model-score kao promotion,
  online credit bez verified receipta i council kao drugi checker owner.
- **Presuda:** **PARETO FLOOR ZADOVOLJEN NA RAZINI DIZAJNA.** Implementacijska promocija je blokirana
  dok mini-corpus ne dokaže NOP/oracle, checker ablation, paired critical ratchet, scope-escape,
  credit-poison i export-canary RED→GREEN. Najveći proof ceiling je eksterna/dinamička valjanost:
  lokalni seeded target dokazuje containment i detekcijski put, ne real-world coverage Strixa/Shannona
  niti generalizaciju model-based semantic checkera.

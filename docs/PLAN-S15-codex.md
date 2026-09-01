PLAN S15

# S15 — Observability

## Zajednički vlasnički rez: jedan journal, pet read-only projekcija

- **Jedini kanonski writer:** `EventJournal.Append()` iz P0.3 validira, klasificira/redaktira,
  dodjeljuje `event_id`/sequence/offset, atomski zapisuje i durable-flusha. Trace, log, transcript,
  audit, cost, metrics i health nemaju drugi write API za činjenice o runu.
- **Projekcija nije novi source of truth:** smije trajno zapisati izvedeni redak ili poslati OTLP/
  Prometheus payload samo uz `source_event_id`, `source_offset` i `projection_version`. Cursor napreduje
  tek u istoj transakciji s durable lokalnim zapisom ili nakon potvrđenog vanjskog slanja; replay je
  idempotentan po `(projection_name, projection_version, source_event_id)`.
- **Granica kontrole:** S15 promatra i preporučuje. S2.5 posjeduje provider usage/cost činjenice,
  S7 posjeduje budget, retry i circuit prijelaze, S16 posjeduje eval/promotion, a S18.3 liveness.
  Dashboard, alert ili modelova samoprocjena ne mogu označiti run uspješnim niti otvoriti circuit.
- **Podaci:** dashboardi, alert/SLO pravila, retention, export endpointi i bounded health pragovi su
  konfiguracija. Go kod posjeduje journal/projection protokol, redakciju, integrity verifikaciju,
  cardinality granice i ovlaštenje za čitanje.
- **Paket-layout:** `internal/observability/{projection,tracing,cost,audit,metrics,health}`; svaki
  paket ovisi o read-only `JournalReader`, nikad o SQLite writeru ili provider/tool executorima.

```go
type ProjectionKey struct {
    Name, Version, SourceEventID string
}

type ProjectionCursor struct {
    Name, Version string
    AppliedOffset uint64
}

type Projection interface {
    Name() string
    Version() string
    Apply(context.Context, contracts.JournalEvent, ProjectionTx) error
}

type ProjectionTx interface {
    PutOnce(ProjectionKey, []byte) error
    CommitWithCursor(ProjectionCursor) error // derived write + cursor atomically
}

type Runner struct {
    Journal contracts.JournalReader
    Cursors CursorStore
    Policy ProjectionPolicy
}
```

**Globalne invarijante S15:**

1. Sink odbija svaki zapis bez valjanog source ID-a/offseta/verzije s `NON_CANONICAL_WRITE`.
2. Projekcija ne mijenja source event, ne izmišlja činjenicu koja nije prisutna u eventu i ne
   pretvara `UNKNOWN` u nulu, success ili benigno stanje.
3. Redakcija se dogodila prije journala. Projekcija ipak radi field allowlist; `SECRET` se izvozi
   samo kao autorizirani sealed reference, nikad kao indeksirani attribute, label ili transcript.
4. Export failure ne ruši user run i ne pomiče cursor. Rastući projection lag je sam vidljiv iz
   journal/cursor razlike; kvar kanonskog journala nije “best effort” nego S7 durability incident.
5. Promjena projection verzije rebuilda novu namespace od offseta 0 i atomski prebacuje reader;
   ne mutira povijest na mjestu. Retention/purge slijedi S6.7/P2.1 i ostavlja propisani dokazni trag.
6. Visokokardinalni ili nepoznati label/attribute ključevi se odbijaju prije sinka. Prompt, tool
   argument, workspace path, user/tenant ID i slobodni error tekst nisu metričke oznake.

## 15.1 Tracing po OpenTelemetry GenAI konvencijama

- **Tip:** MEHANIZAM PROJEKCIJE + OTLP ADAPTER; OPENTELEMETRY JE PRIMARNI WIRE PICK

- **Aspekti:** OpenTelemetry GenAI daje zajednički span/attribute vocabulary i OTLP transport;
  Phoenix daje OTel-first ingest i eval korelaciju; Langfuse daje self-hosted trace UI i OTLP ingest.
  Najbolji graft po aspektu je: OTel za ugovor, canonical journal za identitet i parentage, Phoenix/
  Langfuse samo kao zamjenjivi consumer. NEXUS daje typed OTLP JSON vrijednosti i ne fabricira
  nepoznate token countove, ali njegov synthetic trace ID po export batchu i direct JSONL writer
  ruše replay identitet i P0.3.

- **Sinteza:** run/turn/model/tool/retrieval start/end događaji već nose stabilne `trace_id`,
  `span_id` i nullable `parent_span_id`; ID se alocira prije `Append`, nikad u exporteru. `TraceProjector`
  deterministički mapira event parove u OTel span. Ako crash ostavi samo start, span ostaje
  `UNSET/incomplete`; samo S7 reconciliation event smije ga završiti, exporter ne fabricira success.

```go
type TraceLink struct {
    TraceID [16]byte
    SpanID [8]byte
    ParentSpanID *[8]byte
}

type SpanFact struct {
    SourceEventID string
    SourceOffset uint64
    ProjectionVersion string
    Link TraceLink
    Operation string // run | turn | chat | tool | retrieval
    Start, End time.Time
    Status codes.StatusCode // UNSET | OK | ERROR
    Attributes []attribute.KeyValue // stroga allowlista
}

type TraceProjector struct {
    SemConvVersion string
    Exporter sdktrace.SpanExporter
    Egress security.EgressAuthorizer
}
```

  GenAI semantic-convention verzija se pinna uz projection verziju. Mapper eksplicitno podržava
  `gen_ai.operation.name`, `gen_ai.provider.name`, request/response model i stvarne usage buckete.
  Procjena se označava `estimated=true` ili izostavlja; nikad se ne predstavlja kao stvarna usage
  vrijednost. Raw prompt, completion, tool args/result i error poruka su default OFF; izvoze se samo
  digest, veličina, trust/sensitivity klasa i siguran error code. Opt-in content export prolazi zasebnu
  S6.7 policy+consent odluku i egress allowlist.

  OTLP je jedini backend seam. Langfuse/Phoenix adapter znači endpoint+auth konfiguraciju, ne njihove
  SDK direct writere. Batch ima bounded size/time, ali cursor se pomiče samo do zadnjeg potvrđenog
  source offseta; retry iste serije daje iste trace/span ID-eve.

- **Salvage:** iz NEXUS `core/trace.py:22-56` Python→Go portirati thread/goroutine-safe parent stack,
  start/end mjerenje i `error.type` bez secret poruke; iz `core/trace.py:66-82` portirati snapshot-HWM
  ideju, ali cursor učiniti durable. Iz `core/otel.py:17-45` portirati typed OTLP vrijednosti i pravilo
  “unknown usage se izostavlja”; iz `core/otel.py:74-80` portirati OTLP/HTTP preko S6.3 egressa.
  **Ne portirati** `trace.py:58-64` JSONL direct writer ni silent failure, niti `otel.py:60-73`
  synthetic per-batch ID-eve. Stari `gen_ai.system` se ne kopira bez pinned semconv mappera.

- **Ugovor/RED:** Annex A P0.3 + P0.1 lineage. Glavni RED
  `test_trace_projection_cannot_bypass_journal` pozove OTLP sink s canary tajnom i bez
  `source_event_id`; očekuje `NON_CANONICAL_WRITE`, nula canaryja u lokalnom i mrežnom sinku i nula
  cursor pomaka. Dodatno: `test_trace_replay_preserves_ids_and_is_idempotent`,
  `test_export_failure_does_not_ack_offset`, `test_crash_does_not_fabricate_success_span`,
  `test_parentage_matches_canonical_events` i `test_default_trace_exports_no_content_or_secret`.

- **Verifikacija:** OpenTelemetry GenAI semantic conventions stvarno definiraju GenAI operacije i
  signal modele, ali su razvojna površina koju treba verzijski pinati
  ([official](https://opentelemetry.io/docs/specs/semconv/gen-ai/)); OTel također upozorava da content
  atributi mogu nositi osjetljiv sadržaj. Phoenix prima OTLP traceove
  ([official](https://arize.com/docs/phoenix/tracing/how-to-tracing/setup-tracing/instrumentation/open-telemetry));
  Langfuse podržava OTel ingest ([official](https://langfuse.com/integrations/native/opentelemetry)).
  To dokazuje wire mogućnost, ne naš replay, secrecy ni parentage; njih dokazuju gore navedeni testovi
  s capture collectorom i kontroliranom exporter ablacjom.

- **Pareto-floor:** **PASS uz pinned mapper i journal-identitete.** Zadržava najbolju interoperabilnost
  OTel-a te UI/eval vrijednost Phoenix/Langfusea, a nadmašuje NEXUS stabilnim replay ID-evima i jednim
  writerom. Najjači protuargument je churn razvojnih GenAI konvencija; zato mapper nije kernel schema,
  već versionirana projekcija koju se može rebuildati bez migracije kanonskih događaja.

## 15.2 Usage i cost dashboardi

- **Tip:** MEHANIZAM READ-ONLY COST PROJEKCIJE + DASHBOARD ADAPTERI; S2.5 OSTAJE FACT OWNER

- **Aspekti:** LiteLLM daje spend slicing po modelu/ključu/timu; Langfuse korelira usage/cost s
  traceom; Helicone daje proxy analytics. Primarni floor nije njihov proxy nego vlastita deterministička
  projekcija S2.5 događaja: time ne dupliciramo provider routing, secrets ni budget enforcement.
  Langfuse je primarni vanjski dashboard pick; LiteLLM/Helicone su runner-up consumeri samo ako
  deployment već koristi njihove proxyje.

- **Sinteza:** S2.5 appenduje `UsageRecorded`, `PriceResolved` i eventualni `InvoiceAdjustment` kao
  kanonske događaje. Cost projektor koristi cijenu i valutu verzioniranu u događaju; nova cjenovna
  tablica nikad retroaktivno ne mijenja povijest. Svi iznosi su integer minor/micro jedinice, ne float.

```go
type UsageSource uint8
const (UsageActual UsageSource = iota; UsageEstimated)

type Usage struct {
    Input, CacheRead, CacheWrite, ReasoningOutput, Output uint64
    Source UsageSource
}

type CostFact struct {
    SourceEventID string
    SourceOffset uint64
    RunID, Provider, Model string
    TenantAlias string // policy-controlled opaque alias, ne principal/secret
    Usage Usage
    PriceTableVersion string
    Currency string
    CostMinorUnits int64
    CostSource string // PROVIDER | INFERRED | ADJUSTMENT
}

type CostProjector struct { Prices ReadOnlyPriceRegistry; Sink ProjectionTx }
```

  Usage bucketi imaju dokumentiranu međusobnu isključivost; inclusive provider total se prvo
  normalizira u S2.5 kako cache/reasoning tokeni ne bi bili dvaput zbrojeni. `UNKNOWN` ostaje unknown.
  Procjena je vidljivo odvojena od actuala i ne može postati invoice činjenica. Provider račun se
  usklađuje novim adjustment događajem, ne UPDATE-om starog retka. Dashboard authz filtrira tenant
  prije agregacije; labeli ne sadrže API-key, prompt, user ID ni raw tenant ID.

- **Salvage:** iz `core/circuit.py:83-119` portirati ideju realnog provider-reported troška i bounded
  vremenskog prozora, ali **ne** float USD ledger, parser CLI stringa ni direktne DB writeove. Usage
  parsing dolazi iz typed S2 provider adaptera; S15 agregira već kanonsku činjenicu. NEXUS nema
  dokazano jedinstven price-version owner ni cache-token taksonomiju, pa to nije 1:1 salvage.

- **Ugovor/RED:** P0.3; S2.5 i P2.2 su vlasnici cost/budget izvršenja. Glavni RED
  `test_cost_projection_does_not_double_count_cached_tokens` daje inclusive total plus cache-read
  bucket i očekuje točan normalizirani zbroj, jedan `CostFact` nakon dvostrukog replaya i isti iznos
  nakon promjene aktualne price tablice. Dodatno: `test_estimate_never_becomes_actual`,
  `test_invoice_adjustment_appends_without_history_rewrite`, `test_cost_view_denies_cross_tenant`
  i `test_cost_labels_never_contain_key_principal_or_prompt`.

- **Verifikacija:** Langfuse dokumentira model/usage/cost praćenje i razlikovanje inferred/explicit
  cijene ([official](https://langfuse.com/docs/observability/features/token-and-cost-tracking)); LiteLLM
  dokumentira spend tracking po key/user/team tagovima
  ([official](https://docs.litellm.ai/docs/proxy/cost_tracking)). To ne dokazuje da providerovi token
  bucketi imaju istu semantiku niti da invoice odgovara procjeni; fixture po provideru i reconciliation
  event su obvezni dokaz.

- **Pareto-floor:** **PASS uz S2.5 kao jedini fact owner.** Daje slicing vodećih dashboarda bez još
  jednog proxy/control planea i bez retroaktivne cijene. Najslabija točka je provider invoice drift;
  floor je pošten jer ga bilježi kao adjustment, a ne skriva kao “točan” izračun.

## 15.3 Transcript i tamper-evident audit

- **Tip:** MEHANIZAM INTEGRITYJA U `EventJournal` + DVIJE READ-ONLY PROJEKCIJE + VANJSKI WITNESS

- **Aspekti:** NEXUS `audit.py` daje serijalizirani append i provjeru hash-lanca; OpenHands event
  stream daje rekonstrukciju zabilježenog stanja; AgentOps/Langfuse daju session/trace pregled.
  Lokalni lanac ipak ne dokazuje da je privilegirani napadač prepisao cijelu bazu. Sigstore Rekor,
  immudb ili Trillian daju odvojeni witness/transparent-log aspekt. Sinteza zato drži jedan lokalni
  journal chain i periodično sidri samo tail hash izvan lokalne trust granice.

- **Sinteza:** hash-lanac je dio `EventJournal.Append`, ne zasebna audit tablica. Hashira se točan,
  jednom generiran persisted record byte-string uz domain separation i length framing; nema naknadne
  JSON re-kanonikalizacije.

```go
func EventHash(prev [32]byte, offset uint64, record []byte) [32]byte {
    h := sha256.New()
    h.Write([]byte("nexus-journal-v1\x00"))
    h.Write(prev[:])
    binary.Write(h, binary.BigEndian, offset)
    binary.Write(h, binary.BigEndian, uint64(len(record)))
    h.Write(record)
    var out [32]byte; copy(out[:], h.Sum(nil)); return out
}

type IntegrityCheckpoint struct {
    JournalID string
    EndOffset uint64
    TailHash, PreviousCheckpointHash [32]byte
    CreatedAt time.Time
    RedactionPolicyVersion string
    SignerKeyID, Algorithm string
    Signature []byte
    Witness string
    Receipt, InclusionProof []byte
}

type IntegrityStatus uint8
const (
    IntegrityBroken IntegrityStatus = iota
    IntegrityLocalOnly
    IntegrityExternallyWitnessed
)
```

  Checkpoint potpisuje operator-controlled key i šalje se odvojenom witnessu; key na istom hostu sam
  nije odvojena trust granica. Receipt za checkpoint koji pokriva offset N appenduje journal owner kao
  događaj N+1 i ulazi u sljedeći checkpoint. Verifier provjerava lokalni lanac, journal ID, offset
  kontinuitet, potpis/key rotation, witness inclusion/consistency i očekivanu cadence. Bez valjanog
  vanjskog receipta rezultat je `LOCAL_CHAIN_ONLY`, nikad “tamper proof”. Witness dobiva samo hash i
  minimalne metapodatke, bez sadržaja.

  Transcript i security audit su različiti filteri istih source događaja. Transcript prikazuje
  user-safe turn/tool sažetak; audit prikazuje policy, approval, effect, auth i lifecycle činjenice.
  Oba nose source ID/offset i mogu se rebuildati. Raw osjetljiv sadržaj postoji samo u autoriziranom
  sealed storeu. `DATA_PURGE` može uništiti sealed blob/key i izvedenu kopiju, dok lanac zadržava hash,
  tip i tombstone u granicama S6.7/P2.1; hash sam može biti osobni podatak pa retention policy i dalje
  vrijedi.

- **Salvage:** iz `core/audit.py:23-39,42-52` portirati `BEGIN IMMEDIATE`/single-append discipline i
  full-chain verifier **u EventJournal owner**, ne `audit()` API ili zasebnu tablicu. Iz
  `core/transcript.py:24-40,63-81` portirati caps i determinističke tool sažetke kao projection mapper.
  **Ne portirati** `transcript.py:43-60` direct `append_event`/silent swallow, audit UPDATE/DELETE
  tvrdnju kao dokaz protiv full rewritea, niti generativni title u audit. External witness je nov.

- **Ugovor/RED:** Annex A P0.3, P2.1 i S6.7. Glavni RED
  `test_full_local_rewrite_fails_external_checkpoint` zamijeni cijeli lokalni journal i izgradi uredan
  novi hash-lanac; provjera mora pasti na zadnjem prihvaćenom vanjskom checkpointu. Dodatno:
  `test_delete_mutate_or_reorder_breaks_chain`, `test_forged_or_wrong_journal_checkpoint_fails`,
  `test_missing_witness_is_local_only_not_verified`, `test_checkpoint_receipt_is_next_canonical_event`,
  `test_transcript_and_audit_share_source_ids` i `test_canary_secret_never_enters_index_or_witness`.

- **Verifikacija:** Sigstore transparency log dokumentira javno provjerljiv append-only zapis i
  inclusion dokaz ([official](https://docs.sigstore.dev/logging/overview/)); immudb dokumentira
  kriptografski provjerljive immutable transakcije
  ([official](https://docs.immudb.io/master/immudb.html)). To potvrđuje dostupnost witness mehanizma,
  ne end-to-end zaštitu našeg checkpoint klijenta, key custody ili availability. Fault-injection s
  lažnim witnessom, ukradenim lokalnim DB-om i checkpoint gapom ostaje release gate.

- **Pareto-floor:** **PASS samo uz vanjski witness; lokalno je eksplicitno LIMITED.** Zadržava NEXUS
  chain/concurrency kvalitetu i audit UX kandidata, ali popravlja najvažniju epistemsku grešku: uredan
  lokalni lanac nije dokaz protiv potpunog rewritea. Offline instalacija radi bez witnessa, ali smije
  prijaviti samo `LOCAL_CHAIN_ONLY`.

## 15.4 Metrics i SLO

- **Tip:** MEHANIZAM BOUNDED METRIC PROJEKCIJE + PROMETHEUS/OTEL ADAPTER; NIJE DEPLOYMENT OWNER

- **Aspekti:** Prometheus daje metrike, histogram i alerting primitives; Grafana dashboard/SLO
  vizualizaciju; OTel Collector jedinstven export fan-out. Journal je izvor, Prometheus je primarni
  pull/export pick, Grafana consumer, Collector opcionalni fan-out. Niti jedan ne dobiva direct
  instrumentation writer mimo P0.3.

- **Sinteza:** `MetricProjector` izvodi bounded counter/gauge/histogram state iz terminalnih i
  lifecycle događaja. Dopuštene metrike uključuju run terminal state/profile, verified latency,
  provider attempt/error kategoriju, tool class/outcome/latency, queue/outbox/projection lag,
  breaker transition i budget rejection. Labels su compile-time allowlista s bounded enum vrijednostima.

```go
type SLISpec struct {
    Name string
    EligibleEventType string
    GoodPredicateVersion string
    Window time.Duration
    ObjectivePPM uint32
    Exclusions []string // versionirano i vidljivo, ne ad hoc query
}

type MetricFact struct {
    SourceEventID string
    SourceOffset uint64
    Name string
    Labels map[string]string
    Value int64
    Unit string
}

type MetricProjector struct {
    Registry *prometheus.Registry
    LabelPolicy LabelPolicy
    SLOs []SLISpec
}
```

  SLO definira brojnik i nazivnik unaprijed: npr. `VERIFIED / eligible terminal runs`, dostava unutar
  praga za runs koji su zatražili remote delivery, ili projection freshness. Policy/user cancel i
  dependency outage ne nestaju iz statistike; svako izuzeće je versionirano i zasebno prikazano.
  Pragovi nisu hardkodirani u planu. Latency koristi histograme, monotoni duration i UTC window
  boundary; replay ne smije dvaput inkrementirati. Per-run drill-down ide preko opaque trace exemplara,
  ne kroz `run_id` label.

  Exporter/pull kvar ne blokira run, ali ne napreduje cursor i povećava projection-lag signal dobiven
  iz journal taila minus durable cursor. S18.3 odgovara “je li proces/servis živ”; 15.4 odgovara
  “ispunjava li uslugu mjerljivi service objective”; nema health-state prijelaza.

- **Salvage:** iz `core/trace.py:84-88` portirati bounded latency summary samo kao derived view, a
  iz `core/otel.py:17-28` typed OTLP vrijednosti. NEXUS nema zaseban dokazani Prometheus/SLO owner;
  `health.py` counter se **ne** portira ovdje jer direktno piše paralelnu tablicu i miješa semantic
  degradation s operativnim SLO-om.

- **Ugovor/RED:** P0.3. Glavni RED `test_metric_replay_is_exactly_once` dvaput replaya isti terminalni
  događaj i očekuje counter+histogram sample jednom. Dodatno:
  `test_metric_rejects_unbounded_or_sensitive_label`, `test_slo_denominator_cannot_silently_exclude_failure`,
  `test_exporter_down_preserves_run_and_cursor_and_exposes_lag`, `test_window_boundary_uses_injected_clock`
  i `test_liveness_event_cannot_be_counted_as_verified_success`.

- **Verifikacija:** Prometheus službeno podržava counter/gauge/histogram model i label-based serije
  ([official](https://prometheus.io/docs/concepts/metric_types/)); OTel Collector prima, obrađuje i
  izvozi telemetry signale ([official](https://opentelemetry.io/docs/collector/)). To ne dokazuje
  bounded cardinality, exactly-once projekciju ni pošten SLO nazivnik; property testovi generiraju
  replay/reorder i attacker-controlled labele, a fixture test literalno provjerava denominator.

- **Pareto-floor:** **PASS uz journal-derived metrike.** Dobiva Prometheus/Grafana interoperabilnost
  i OTel fan-out bez paralelnog telemetry sourcea. Cijena je eventual consistency; prihvatljiva je jer
  S15 nije control path, a lag je eksplicitno mjerljiv i nikad se ne prikazuje kao svježe stanje.

## 15.5 Semantic agent health i degradation

- **Tip:** MEHANIZAM DETERMINISTIČKE HEALTH PROJEKCIJE; S7 JE JEDINI CIRCUIT OWNER

- **Aspekti:** NEXUS `circuit.py` daje stabilne failure signature, stagnation/no-progress obrasce i
  circuit namjeru; `health.py` prepoznaje progutane best-effort kvarove; Langfuse evaluator metrike i
  Prometheus rules daju agregaciju/alerting. Najbolji graft je vlastiti evidence-derived aggregator
  plus S7 command seam: S15 računa stanje i preporuku, S7 validira aktualni policy/epoch te jedini
  appenduje `CircuitOpened/HalfOpened/Closed`.

- **Sinteza:** health se računa samo iz typed checker/user/provider/tool/lifecycle događaja, nikad iz
  modelove tvrdnje “uspjelo”. Svaka mjera ima eligibility, minimalni sample, fiksni window, pinned
  baseline, warn/open/close prag i hysteresis/cooldown. Nedostatak pouzdanog signala je `UNKNOWN`, ne 0.

```go
type HealthState uint8
const (
    HealthUnknown HealthState = iota
    HealthHealthy
    HealthDegraded
    HealthCritical
)

type SemanticMeasure struct {
    Name string
    Numerator, Denominator uint64
    WindowStartOffset, WindowEndOffset uint64
    BaselineVersion string
    State HealthState
}

type HealthSnapshot struct {
    Profile, PolicyVersion string
    Measures []SemanticMeasure
    EvidenceOffsets []uint64
    GeneratedFromOffset uint64
    State HealthState
}

type CircuitRecommendation struct {
    Profile, PolicyVersion string
    EvidenceStart, EvidenceEnd uint64
    ReasonCodes []string
    RecommendedState string // OPEN | HALF_OPEN | CLOSE; nije prijelaz
    ExpiresAt time.Time
}
```

  Normativne definicije:

  - `recidivism_rate`: checkerom potvrđen failure signature ponovno se javio nakon typed
    `CorrectionAccepted`/verified fixa, podijeljeno s eligible ispravljenim signatureima;
  - `correction_rate`: accepted/verified rezultat dobije typed user ili checker `CorrectionRecorded`
    unutar bounded horizonta, podijeljeno s eligible verified rezultatima; slobodni chat nije signal;
  - `verified_success_trend`: checker-verified uspjesi u tekućem fixed-event windowu naspram pinned
    baselinea, uz minimalni N i interval pouzdanosti; model self-score se ignorira;
  - `fallback_drift`: neplanirani/error fallback attempti kroz eligible routeove; policy-planirani
    barbell/hedge routeovi se izuzimaju eksplicitnim reason codeom;
  - `swallow_count`: canonical `BestEffortFailed{subsystem,error_class}` događaji po bounded enumima;
    ako se journal ne može zapisati, to je S7 durability incident, ne skriveni emergency counter;
  - `repeat_without_progress`: isti normalized tool/action signature ponovljen bez promijenjenog
    observation/evidence hasha. S3 koristi isti signature unutar runa; 15.5 gleda trend kroz runove.

  S7 prihvaća preporuku samo ako policy version, profile, evidence tail i expiry odgovaraju aktualnom
  stanju. Hysteresis zahtijeva stroži open i blaži close prag plus cooldown, da signal ne flip-flopa.
  Projekcija koja kasni, nema minimum N ili je pokvarena mora vratiti `UNKNOWN`; za safety profil S7
  može policyjem blokirati nove high-risk runove, ali tu odluku ne donosi S15.

  Razgraničenje je testabilno: S18.3 = proces/endpoint živ; 15.4 = servisni latency/error/delivery SLO;
  15.5 = kvaliteta dokazano izvršenih odluka/rezultata degradira. Council/self-critique i 16.x eval
  mogu proizvesti typed evidence, ali ne mogu sami sebi dati verified status.

- **Salvage:** iz `core/circuit.py:23-75` Python→Go portirati noise-normalized signature,
  stagnation/flip-flop/no-progress detekciju i bounded history; S7 ostaje owner akcije. Iz
  `core/health.py:1-8` portirati ideju vidljivosti progutanih kvarova i stabilnih tagova. **Ne portirati**
  `health.py:16-34` zasebni `swallow_counts` writer ni dvostruko silent failure; umjesto njega svi
  best-effort catch seamovi appenduju typed event. Iz `core/provenance.py:18-24` portirati samo
  least-trust princip kroz S0 ContextBlock, ne njegove statističke tvrdnje.

- **Ugovor/RED:** P0.3, P0.2/S7 i S3.5/16.6 checker granica. Glavni RED
  `test_model_claim_cannot_mask_checker_failure` daje modelov “success” uz non-zero checker exit/diff
  failure; verified-success trend mora pasti i model event se ne smije brojiti u brojnik. Dodatno:
  `test_missing_correction_signal_is_unknown_not_zero`, `test_planned_fallback_is_not_drift`,
  `test_disabled_or_lagging_projection_cannot_report_healthy`, `test_stale_recommendation_cannot_open_circuit`,
  `test_hysteresis_prevents_threshold_flap`, `test_best_effort_failure_has_one_canonical_event` i
  `test_health_cannot_replace_liveness_slo_or_checker`.

- **Verifikacija:** lokalni `core/circuit.py:38-75` stvarno normalizira failure tekst i detektira
  streak/flip-flop/no-progress; `core/health.py:16-25` stvarno broji swallow, ali u zasebnom writeru i
  može nestati bez traga. Langfuse evaluator i Prometheus rule mogu prikazati izvedene metrike, ali ne
  dokazuju naše semantičke definicije. Vault tvrdnja da je mehanizam rijedak ostaje **hipoteza**, nije
  kriterij. Dokaz su deterministic fixtures s kontradiktornim model/checker signalima, ablation koja
  health projection učini RED i integration test koji promatra da samo S7 mijenja circuit state.

- **Pareto-floor:** **PASS uz strogo odvojenu preporuku od akcije.** Nadmašuje obični liveness/SLO
  hvatanjem alive-but-degrading ponašanja i zadržava NEXUS failure-signature prednost, bez trećeg
  circuit ownera. Najslabija točka su korekcijski/baseline pragovi specifični proizvodu; zato su
  versionirani bounded profile-data, `UNKNOWN` je prvorazredno stanje, a promotion čeka kalibracijski
  corpus umjesto izmišljenih univerzalnih brojeva.

## S15 zajednički verification gate

```text
RED 1: direct trace/transcript/audit/metric/health write bez source ID-a -> NON_CANONICAL_WRITE.
RED 2: exporter padne nakon derived writea, prije ACK-a -> replay bez duplikata, cursor nije preskočio.
RED 3: canary secret u promptu/tool erroru -> nema ga u OTLP, metric labelu, transcript indeksu ni witnessu.
RED 4: cijeli lokalni journal prepisan s konzistentnim novim lancem -> vanjski checkpoint mismatch.
RED 5: model tvrdi success, checker faila -> trace status/error, SLO i semantic-health ostaju failure.
RED 6: metric/health projekcija kasni ili je ugašena -> UI kaže STALE/UNKNOWN, nikad HEALTHY/fresh.
```

Release gate pokreće isti event corpus kroz SQLite replay 2×, OTLP capture collector, Prometheus
registry, transcript/audit derived store, fake external witness i S7 circuit spy. Oracle uspoređuje
source ID/offset, sink sadržaj, cursor i side-effect counter; nije dovoljno provjeriti samo vraćeni error.
Kontrolirana mutacija koja uvede direct writer mora učiniti cijeli gate RED prije nego se GREEN smatra
dokazom. Cross-platform testovi koriste injected clock i temp state; mrežni test samo lokalni fake sink.

## Konačni Pareto sudac S15

- **Floor najboljih kandidata:** OTel/OTLP interoperabilnost; Langfuse/Phoenix trace/cost UX;
  Prometheus/Grafana SLO; NEXUS chain, signature i swallow visibility; vanjski transparent witness.
- **Naš dodatni floor:** jedan kanonski writer, stable replay identity, secret-safe bounded projections,
  pošten `LOCAL_CHAIN_ONLY`, exactly-once derived state i semantic-health koji ne preuzima S7/S16.
- **Odbijeno kao nepotrebna složenost:** paralelni telemetry SDK writeri, dva tracing backenda u
  jezgri, vlastiti metrics TSDB, vlastiti transparency log, dashboard kao control plane i model-based
  health judge. Adapter se dodaje tek kad isti projection contract ne može zadovoljiti stvarni sink.
- **Presuda:** **PARETO FLOOR ZADOVOLJEN NA RAZINI DIZAJNA.** Implementacijska promocija ostaje
  blokirana dok direct-writer ablation, crash/replay, secret canary, full-rewrite/witness i S7-only
  circuit testovi ne prođu. Najveći proof ceiling je vanjska witness/key-custody operativa: lokalni
  fake potvrđuje protokol, ali ne dostupnost ili neovisnost produkcijskog trust boundaryja.

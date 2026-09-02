# NEXUS — HARNESS PLAN (hibrid-hibrida dizajn po podsekciji)

> **⚠ SALVAGE UKLONJEN (2026-09-02):** nexus je ČIST GREENFIELD Go — nula porta/reusea iz ijednog repa.
> Svi `Salvage:`/GORTEX/NEXUSv2 oslonci obrisani. Izvor po podsekciji = full-code-auditani svjetski
> kandidat ILI napisan Go-dizajn. Rupe + RESOLVED/UNRESOLVED: `PLAN-HOLES-CONSOLIDATED.md`. 108 podsekcija.

**Što je ovo:** buildable dizajn-plan. Za SVAKU podsekciju spec-a (S0–S18, **108 podsekcija** = 100 spec v0.9 + 8 novih addendum (greenfield, nula salvagea))
prolazi se HIBRID-HIBRIDA sinteza nad njezinim top-3 kandidatima (merge-skill princip):
razloži svaki kandidat na aspekte → po aspektu uzmi najbolje iz sva tri → graftaj u JEDAN
sintetizirani dizajn → sudac (Pareto floor: sinteza ≥ najbolji kandidat na svakom aspektu) →
verifikacija (aspekt mora biti STVARAN, ne tvrdnja). Kad je plan gotov, kodiranje = implementacija.

**Jezik:** Go (3× AGREE). Jezgra `CGO_ENABLED=0` single-binary; `modernc.org/sqlite` pure-Go
spine; sandbox per-OS build-tagged (Linux Landlock+seccomp real, Win/Mac adapteri); TUI
bubbletea; desktop Wails/Fyne (odluka kasnije). Sve sigurnosne membrane pišu se OD NULE u Go (greenfield; GORTEX ne postoji).
**Repo:** MatNik89/nexus (private). CI 3-OS na kraju.

**Format po podsekciji:**
- **Tip:** MEHANIZAM (mi pišemo) | ADAPTER (vanjski alat iza sučelja)
- **Aspekti** (razložak kandidata) + najbolji izvor po aspektu
- **Sinteza:** naš dizajn (za MEHANIZAM) ili sučelje + primarni pick (za ADAPTER)
- **Ugovor/RED:** koji Annex A ugovor + RED test je gate
- **Verifikacija:** je li svaki graftani aspekt stvaran (URL/licenca/aktivnost)
- **Pareto floor:** potvrda da sinteza nije gora ni na jednom aspektu

**Status:** U IZRADI. Redoslijed: S0 → S18 (kernel-prvi po fazama P0-P6).

---

## Protokol (za agente)
Svaki agent za dodijeljenu sekciju: pročita spec podsekciju + top-3 + Annex ugovor + NEXUS
kandidat. Napravi hibrid-sintezu po gornjem formatu u `PLAN-<sekcija>-<ime>.md`.
Moderator spaja u ovaj dokument uz Pareto-floor sudca (odvojen od autora — barbell).
Verifikacija aspekata: WebSearch/WebFetch za sporne tvrdnje.

---

<!-- Sinteze se dodaju ispod, sekcija po sekcija -->

---

## IDENTITET: NEXUS = OSOBNI AI ASISTENT s vrhunskom coding-jezgrom (JEDNA JEZGRA, DVA PROFILA)

Asistent-prvo (A3.1). `AssistantProfile` = prvorazredno lice; `CodingProfile` = najjača grana znanja
(mora nadmašiti Kilo Code / OpenHands / OpenCode). Isti kernel (S0-S9), gating ovisi o profilu.

Korisnički zahtjev: NEXUS mora biti ODLIČAN coding harness — cilj nadmašiti Kilo Code,
OpenHands, OpenCode. `coding` profil (3.5 + 4.2–4.4 + S5 + 8.3 + TIA + app-contract + Reversa)
dobiva DUBLJI hibrid-sintezni pass od ostalih, s dodatnom rigoroznošću.

**Što moramo BAREM izjednačiti (iz analize 28 harnessa):**
- **Aider** — benchmarkirani edit-formati (diff/whole/udiff/search-replace) po modelu + tree-sitter/
 PageRank repo-map. Edit-format je dokazani lever za slab model → 4.3 mora imati više formata + izbor po modelu.
- **Codex** — turn/session/tool/context razdvojeni moduli (zlatni standard modularnosti) → naša S0/S3 dekompozicija.
- **OpenHands** — Action/Observation event-stream model → čist runtime; error u observationu, ne pad petlje (3.4).
- **Cline/Roo** — checkpoint/rollback po koraku (shadow-git) → 5.1 sigurnosna mreža.
- **OpenCode/Kilo** — LSP-integrirana simbolička navigacija → 4.4 (LSP + AST + callgraph).

**Naši DIFERENCIJATORI (što ostali NEMAJU — iz NEXUS audita + obsidiana):**
- **Evidence-graded loop**: checker ocjenjuje generički `AcceptanceContract`+`EvidenceBundle`
  (coding=git-diff+exit; asistent=delivery-receipt/calendar/grounding), NIKAD prozu
 workera (anti-sycophancy strukturno) → 3.1 + 16.6. Nijedan od tri konkurenta to nema.
- **TIA** (3.5): coverage × git-diff → samo pogođeni testovi (sekunde umjesto minuta).
- **app-contract** (16.1): deliverable-level acceptance (ekrani/tokovi/persistence), ne test-count.
- **Skill-forge** (11.5) + **credit/reward ledger** (16.4): harness uči iz coding-iskustva, greške se ne ponavljaju.
- **Reversa** (11.2): reverse-engineering legacy repoa → spec+migracijski plan prije mutacije.

**Organizacija:** kad hibrid-sinteza dođe do coding-sekcija (3.x, 4.2-4.4, 5.x, 8.3), radi se
KAO ZASEBAN produbljeni blok — svaka od te tri meta (Kilo/OpenHands/OpenCode) razložena na
coding-aspekte, hibrid mora na SVAKOM aspektu biti ≥ najbolji od njih + naši diferencijatori nadgradnja.

## PROVIDER MEHANIZAM (izvor-osnova za 2.1/2.2/2.6)

NEXUS spaja na modele DVA načina (greenfield dizajn):
1. **CLI-agent provideri** — subprocess na instalirani `codex`/`claude` CLI s UGAŠENIM alatima
 (`--tools ""`, ephemeral, ignore-config) → tuđi harness kao čisti text-in/out model, bez API ključa.
2. **API provideri** — OpenAI-kompatibilan base_url+api_key HTTP (DeepSeek/LiteLLM/Local).
+ router (naučeni winner + barbell fallback), KeyPool rotacija, CostTracker.
Go-verzija ZADRŽAVA oba + sigurnosnu finesu (CLI-agent bez alata — NEXUS drži petlju/alate).

---

# S0 — KANONSKA SINTEZA (3-way konsenzus codex+kilo+agy, Pareto-floor moderator)

**Konvergencija:** sva 3 agenta neovisno došla na isti struct-set i dekompoziciju. Kanonski
izvor = ručno pisani Go u `internal/kernel/*`; JSON-Schema/OpenAPI/protobuf su IZVEDENE
projekcije, nikad izvor istine. Jezgra `CGO_ENABLED=0`. **Pravilo: nema `map[string]any` u
kernel API-jima** — opaque payload i nepoznata polja kao `json.RawMessage`, poznati
discriminator kroz zatvorenu validaciju (fail-closed).

**Paket-layout (codex):** `contracts <- journal <- machine`; `negotiation`, `migration`,
`closure` ovise o `contracts` ali ne jedni o drugima. `EventJournal.Append` = jedini trajni
write-owner (P0.3); state/log/trace/transcript/audit su projekcije.

**0.1 typed schema (MEHANIZAM):** `Envelope`/`Message`/`ContextBlock`/`ToolCall`/`ToolResult`/
`TypedError` Go structovi (polja iz Annex P0.1). Enumi = `iota`+`String()`+`Valid()`,
default-reject grana (kompenzira nedostatak Go sum-types). `Content` XOR `ContentRef` u
konstruktoru. **RED:** P0.1 `test_s0_rejects_provenance_laundering`. **Pareto:** NADSKUP MCP+OpenAI-SDK+LangGraph
(dodan trust_class/sensitivity/lineage koje nijedan nema).

**0.2 state-machine (MEHANIZAM):** eksplicitna prijelazna TABLICA `map[State][]Transition{Event,
From,To,Guard}`, default-reject; state = REKONSTRUIRAN fold-om eventa iz journala (ne mutable
polje); checkpoint = journal offset. `UNKNOWN`→samo reconciliation event. **RED:** P0.1 nelegalan prijelaz (RUNNING→ADMITTED, SUCCEEDED→RUNNING)→reject.
**Pareto:** tablica+guard (LangGraph) + event-derived (OpenHands) + UNKNOWN-reconcile (Temporal).

**0.3 capability negotiation (MEHANIZAM):** `CapabilityFloor{ToolCalling,StructuredOutput,
Streaming,ContextLimit}`; `Effective()=min(declared,measured)`; `Resolve` nepoznatog → error
(fail-closed, "mjeri ne pretpostavljaj"). **HONEST GAP (kilo+moderator):** 0.3 JEDINA S0
podsekcija — ugovor **P0.5 "capability-matrix fail-closed"** je u Annexu A (dodan);
Tier-3. Ugovor **P0.5** je u Annexu A (dodan).
**Pareto:** cap-as-data (LiteLLM)+declared-on-adapter (PydanticAI)+cap-routing (Portkey)+fail-closed (naš).

**0.4 verzioniranje/migracija (MEHANIZAM):** `SchemaRegistry{byID map[string]map[int]Upcaster}`,
monoton lossless upcast lanac, nepoznato→QUARANTINE (ne tiho-odbij, ne downcast), immutable raw
event čuvan, `MigrationRecord{From,To,ID,InputHash,OutputHash}`. **RED:** P0.1 migracija-MUST + provenance-
downgrade kroz upcaster. **Pareto:** registry+upcast (Temporal)+envelope-verzija (CloudEvents)+
karantena (naš, nijedan od 3 ne karantenira).

**0.5 capability-flag closure resolver (MEHANIZAM, nova jezgra):** `CapabilityManifest{provides/
requires/conflicts/gates/trigger/config_hash/signature}` + `GateAttestation`; `Resolver.Resolve`:
tranzitivni closure → cycle/conflict/unknown-version→REJECTED → topološka aktivacija → svi gate-
att nad ISTIM config_hash → atomski aktivni set (parcijalno ACTIVE nelegalno). Bundle-alias NIJE
capability (ne forsira flag bez triggera). **RED:** P0.4
`test_every_flag_fails_when_one_gate_is_ablated` (18 flagova). **Pareto:** closure/cycle (Cargo)+
cap-matching (OSGi)+topološko (Bazel)+gate-attestation (naš).

**S0 verifikacija:** svi kandidati stvarni (MCP Apache-2.0, OpenAI-SDK/LangGraph/LiteLLM/PydanticAI/
OpenHands/Temporal MIT, CloudEvents CNCF, Cargo/Bazel/OSGi) — licence+aktivnost potvrđeni.
**S0 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md)** (P0.5 ugovor za 0.3 je u Annexu A).

---

# S1 — KANONSKA SINTEZA (Temelji; 3-way konsenzus, Pareto-floor PASS sve 3)

**Paketi:** `foundation/{config, pathx, procx, telemetry}`; `config→pathx`, `procx→pathx+
contracts`, `telemetry→contracts+journal`. Nijedan S1 paket NE ovisi o CLI/TUI/desktop sloju.
Vlasništvo: resolver/paths/proc/logging = KOD; config-vrijednosti = DATA (samo SUŽAVAJU kernel max).

**1.1 config (MEHANIZAM):** `Resolver` interface, precedence Default<Global<Project<Env<CLI,
jedan typed `Config` struct (NE map[string]any), schema-validacija PRIJE mergea, per-key `-c`
override s origin-trackingom, profile=overlay. Hot-reload: `fsnotify`+SIGHUP → `ValidateBounds`
(config NE smije proširiti kernel floor, fail-closed) → atomska zamjena (`atomic.Pointer`),
generation-pinning (run nastavlja s pinned snapshotom); nevaljano→`config.reload_rejected` event,
stara generacija ostaje. **RED:** config proširi kernel max
(egress `*` / sandbox ispod floora)→reject. **Caveat (codex):** fsnotify je +ovisnost; upgrade-gate
= ako mjerenje pokaže da eksplicitni reload zadovoljava, briše se bez promjene ugovora. **Pareto PASS.**

**1.2 cross-platform paths+procesi (MEHANIZAM, cross-platform JEZGRA):** putanje preko stdlib
`os.UserConfigDir/CacheDir` (bez build-tagova). Procesi build-tagged: `proc_unix.go`
(`SysProcAttr{Setpgid:true}`, killpg `-pgid` SIGTERM→GracePeriod→SIGKILL→Wait; NE Pdeathsig — smrt
Go threada ≠ smrt procesa; S1.2 daje PROCESS-IDENTITY primitiv (PID+start-token), a lease-owner je S7 koji ga konzumira — S1.2 NE ovisi o S7), `proc_windows.go`
(`CREATE_NEW_PROCESS_GROUP` + Job Object `KILL_ON_JOB_CLOSE`; taskkill `/T /F` preko
`GetSystemDirectory` NE PATH — bounded fallback, NIJE ownership dokaz), `proc_darwin.go`=killpg.
PID+**start-token** identitet (P1.2 orphan-sweep; PID sam nije dokaz). PTY: `creack/pty` pure-Go,
build-tagged, fail `PTY_UNAVAILABLE` (nikad tihi shell fallback). **RED
(P1.2):** SIGKILL parent, restart → sweeper reapa child preko start-tokena; grupni Terminate ubija
stablo. **Caveat (codex):** Windows CreateProcess↔Job race — RED mora forsirati child-spawn u tom
prozoru; Aider absolute-path bug = protuevidencija (ne graftamo njegov path floor). **Pareto PASS.**

**1.3 strukturirano logiranje (MEHANIZAM, OTel-kompatibilan, JEDAN write-owner=P0.3):**
`telemetry` = dva API-ja: **emitter** proizvodi kanonski dijagnostički event PREKO journala;
**projector** prevodi appendani `JournalEvent` u sink-record. slog/OTel/trace/transcript/metrics su
PROJEKCIJE, ne pisci — nijedan ne alocira event_id/sequence. `OTelLogRecord` prati OTel Logs Data
Model; korelacija (`run/turn/tool_call/attempt/parent/sequence/journal_offset`) eksplicitna u attrs.
OTLP exporter = adapter, stabilan ID-mapping (replay ne mijenja trace-ID). `slog.Handler` bridge →
Emitter (odbija unsupported). Bootstrap iznimka: `NEXUS_BOOTSTRAP_ERROR code=<safe>` na stderr dok
journal nije otvoren, pa se nepovratno zatvori. **RED (P0.3):** `test_projection_cannot_bypass_journal` + redaction-before-
projection, replay-keeps-otel-ids, idempotent-projection, failed-export-doesnt-advance-cursor,
concurrent-spans-keep-parents, secret-never-reaches-sink. **Caveat:** append svakog debug eventa
može opteretiti journal → filter PRIJE payload konstrukcije, ali sampling/drop je POLICY event i
NIKAD ne obuhvaća state/security/audit klase; throughput floor MJERI se (proof ceiling do benchmarka).
**Pareto PASS.**

**S1 honest gap:** 1.1 i 1.2 nemaju dedicirani Annex RED (kao 0.3) — gate posredno kroz P1.2/P0.4 +
kod-vs-data načelo. Ugovori **P0.6** (config-bounds+process-identity) i P0.5 su u Annexu A. **Verifikacija:** svi kandidati stvarni (Codex/Aider/Continue/goose Apache-2.0; OTel
CNCF; structlog/tracing/OpenHands MIT). **S1 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S2 — KANONSKA SINTEZA (Provider sloj; 3-way, Pareto PASS)

**Owner granica (P0.2):** retry/deadline/cancel = S7; 2.2/2.6 NIKAD ne retry-aju (svaki pokušaj
nosi `s7.AttemptGrant`). Routing owner = 2.6 jezgra (12.3=multi-agent potrošač). Oba NEXUS načina
spajanja zadržana (API HTTP + CLI-agent subprocess).

**2.1 multi-provider + 3 AUTH-MODA (MEHANIZAM) — ovo odgovara na OAuth pitanje:**
Jedan `Provider` interface (`Chat/Stream/Capabilities/DataDescriptor`), auth pluggable `AuthMode`:
- **(a) `APIKeyAuth`** — DEFAULT, čisto (Bearer, OpenAI-kompatibilan)
- **(b) `OAuthAuth`** — device/auth-code→token→refresh, keyring-cache (1.2); SANKCIONIRANO gdje
 provider službeno nudi OAuth za API
- **(c) `CLIAgentAuth`** — subprocess `codex exec --tools "" --ephemeral` / `claude -p --tools ""`;
 tuđi CLI = čisti text-in/out, NEXUS drži petlju/alate, bez API ključa
- **(d) `SubscriptionOAuthAuth`** — ZASEBAN config blok, NIKAD default; repliciran OAuth bez CLI za
 subscription. Loader FAIL-CLOSED odbija `Resolve` ako `subscription_oauth.acknowledged_tos != true`
 + redacted ToS-upozorenje ("može prekršiti ToS providera, rizik bana") prije prve upotrebe.
`AuthCredential.Redacted()` — nikad curenje. Petlja NE zna koji mod (razlika samo `Capabilities()`).
**RED:** (d) bez
acknowledged_tos→error; (c) bez `--tools ""`→odbij. **Pareto:** LiteLLM+PydanticAI+Vercel-AI-SDK +
3-auth-moda (nijedan od 3 nema CLI-agent ni OAuth-device auth).

**2.2 fallback (MEHANIZAM, TANKI — NE retry petlja):** `FallbackPlanner.Plan(err,cur)→
(Target, s7.FallbackProposal)` — vraća PRIJEDLOG, puni `s7.ExecutionPolicy.fallback_targets[]` +
mapira provider-error→kategorični kod; retry je S7. AccountFleet SAMO klasificira (candidate+cooldownHint); S7 JEDINI izdaje grant/backoff/failover. **RED P0.2:** `test_adapter_cannot_self_retry` (2× isti
AttemptGrant→`ATTEMPT_NOT_AUTHORIZED`, counter=1). **Pareto:** LiteLLM+Portkey+Kong + no-self-retry.

**2.3 structured output (MEHANIZAM):** `StructuredOutput[T].Validate→re-ask→salvage`, NIKAD tiho
prihvati nevalidan T. Tolerant parser (strip fences/trailing comma). Constrained-decoding (Outlines)
= OPT-IN adapter za local (2.4), ne jezgra za HTTP. **RED:** malformiran JSON→salvage/re-ask, silent-accept zabranjen.
**Pareto:** Instructor+Outlines+BAML.

**2.4 lokalni + hardware-fit (ADAPTER+MEHANIZAM):** `LocalProvider{Kind:ollama|llama_cpp|vllm}`
OpenAI-kompatibilan HTTP; `HardwareFit.Fits(spec,quant)` preflight MJERI RAM/VRAM/AVX/CUDA na hostu
→ ne stane: odbij+predloži kvantizaciju (ne OOM). `Capabilities()`=Measured. **RED:** model>RAM/VRAM→Fits=false+prijedlog. **Pareto:** Ollama+
llama.cpp+vLLM+hardware-fit (obsidian).

**2.5 cost tracking (MEHANIZAM):** `CostTracker.Record(model,usage)` price-table, real usage ili
len//4 estimate (označen), MONOTON cumulative spend→S7 circuit-breaker (P2.2 cost_minor_units);
feed-a 16.4 verified-credit. **RED:**
spend>ceiling→fence (ne samo log). **Pareto:** LiteLLM+Aider+Helicone+budget-ceiling.

**2.6 model routing (MEHANIZAM, jezgra owner):** `Router` ABI; `BarbellRouter` (plan/execute/verify
po fazi) = jezgra; `LearnedRouter{bandit}` epsilon-greedy OPT-IN, reward SAMO iz 16.4 verificiranog
(nikad self-report). Route fail-closed: kandidat ispod capability floora (S0.3) odbijen. **RED:** Route na model ispod floora→error. **Pareto:** RouteLLM+
semantic-router+LiteLLM+barbell+verified-bandit.

**S2 honest gap:** 2.1/2.3/2.4/2.5 bez dediciranog Annex RED — gate posredno (P2.5 za 2.1, S0.1 za
2.3, hardware-fit za 2.4, P2.2 za 2.5). U Annexu A: P0.7 structured-never-silent, P0.8 hardware-fit-fail-closed. **Verifikacija:** svi kandidati stvarni (Kong webfetch-verificiran).
**S2 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).** Auth-mod dizajn (a-d) odgovara korisnikovom OAuth/subscription pitanju.

---

# S3 — KANONSKA SINTEZA (Core Loop; PRVA coding-kritična, produbljeno; 3-way, Pareto PASS)

**Owner granica (P0.2, najvažnija S3 invarijanta):** S3 NIKAD ne retry-a — 3.4 classify-a→S7,
3.3 emitira user-cancel, svaki pokušaj nosi `s7.AttemptGrant`. Kilo Code = "to-beat" meta (nema
javnu licencu, ne izvor); OpenHands/OpenCode/SWE-agent = javni referenti.

**3.1 plan-act-observe (MEHANIZAM, coding-jezgra):** `Loop{planner,executor,verifier,checker,
state,journal}`: plan(adaptivan small/medium/large) → S6.0.Decide→S6.9.Before (policy pa order) → execute(→Observation,
3.4) → verify(3.5 in-turn) → **GRADE(16.6 checker)** → re-plan s dokazom. **DIFERENCIJATOR
evidence-graded:** `Checker.Grade` ocjenjuje **git-diff + realan exit-code + determinističke
signale, NIKAD prozu workera** (anti-sycophancy strukturno) — NITKO od Kilo/OpenHands/OpenCode
nema. **RED:** worker kaže "done" bez
diff/testa → Grade=FAIL, run ne u SUCCEEDED. **Pareto:** Action/Obs+event-stream (OpenHands) +
modularnost (Codex) + adaptivni planer (SWE-agent ACI) + evidence-graded (naš).

**3.2 streaming (MEHANIZAM):** `Chunk{Delta,Seq,Finish}` channel + ctx-cancel; stream=projekcija
journala. **RED:** cancel usred streama→channel zatvoren, turn CANCELLED. **Pareto:** Gemini+Codex+OpenHands+cancel-token.

**3.3 cancel/turn (MEHANIZAM):** `TurnController{Cancel/Pause(offset)/Resume(offset)}`; cancel=S7
token, pause/resume=journal offset (replay fold, ne mutable state); CANCELLED terminalan.
**RED P0.2:** cancel usred tool-poziva→prekid, nikakav side-effect. **Pareto:** goose+OpenHands+Codex+S7-token.

**3.4 error recovery (MEHANIZAM, coding-jezgra):** tool-pad → `TypedError` PAKIRAN u observation
(model vidi, korigira se — alat pao ≠ petlja pala); classify→retryable(S7)/terminal(re-plan).
**DIFERENCIJATOR stuck-detection** (`internal/circuit`): STAGNATION (isti tool/arg N puta) + NO_PROGRESS
(verify ne zelena M puta) → breaker prije lažno-zdravog vrtenja. NITKO nema. **RED P0.2:** tool-pad→obs nosi error, loop
ne crasha; STAGNATION→breaker. **Pareto:** error-u-obs (OpenHands) + checkpoint (LangGraph) +
razumljiv-format (SWE-agent) + stuck-detection (naš).

**3.5 deterministička verifikacija + TIA (MEHANIZAM, coding-jezgra, `coding` flag):** in-turn
verify (pokreni→dijagnostika NATRAG U ISTOM TURNU→odbij završetak dok ne prođe). **DIFERENCIJATOR
TIA:** coverage/dependency-graf × git-diff → SAMO pogođeni testovi (sekunde umjesto punog suitea),
full-suite fallback kad stale. NITKO od 3 nema (vrte pun suite/linter). **RED:** edit s lint-
greškom→Diagnostic, korak ne prihvaćen, model dobiva u istom turnu; TIA pokreće samo pogođene.
Treći slot UNCLEAR (spec praznina — naša petlja je odgovor). **Pareto:** Aider+SWE-agent+TIA (naš).

**3.6 run-trigger-plane (MEHANIZAM admission=jezgra; triggeri=`automation`):** `Trigger` interface
(Manual/Schedule/Webhook/Watch/Heartbeat) → SVI kroz isti S0 envelope → dedup+idempotency+policy+
budget+audit PRIJE petlje (autonomni = iste granice kao ručni). `ScheduleManifest` s P2.6 clock/DST/
missed-run/overlap. **RED P2.6:** `test_zagreb_dst_fold_runs_
once_across_restart`. **Pareto:** NEXUS-admission+Temporal-DST+APScheduler.

**S3 = 3 CODING-DIFERENCIJATORA gdje POBJEĐUJEMO:** evidence-graded checker (3.1), stuck-detection
(3.4), TIA (3.5) — nijedan od Kilo/OpenHands/OpenCode nema. **Honest gap:** 3.2/3.3/3.5 bez
dediciranog Annex RED (P0.9 "verify-never-silent" — u Annexu A). **Verifikacija:** svi
javni kandidati stvarni; Kilo Code = to-beat meta bez javne licence. **S3 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S4 — KANONSKA SINTEZA (Tool sustav; coding-kritično 4.2-4.4; 3-way, Pareto PASS)

**4.1 tool-registry (MEHANIZAM):** typed schema po alatu (`ToolSpec{ID,SchemaHash,EffectClass,ExecutionKind}`; ExecutionKind∈{ExecInProcess,ExecProcess} pečati grananje za effect-path),
dispatch, timeout, result-envelope, permission-wrapper. **RED:** poziv bez validne sheme→reject. **Pareto:** FastMCP+smolagents+OpenAI-SDK.

**4.2 shell/exec (MEHANIZAM, coding):** bash-sesija koja drži stanje, rlimits, tree-kill.
**RED:** timeout→cijelo procesno stablo ubijeno (P1.2). **Pareto:** OpenHands+
Codex-sandbox+gptme + gortex-reuse.

**4.3 edit formati (MEHANIZAM, PRESUDNI coding-lever, PRODUBLJENO):** multi-format engine
(WHOLE/DIFF/UDIFF/SEARCH_REPLACE) + **izbor PO MODELU** (Aider benchmark: slab→SEARCH_REPLACE,
jak→DIFF) + **deterministički LADDER** `exact(editapply)→fuzzy(editrepair, više kandidata→REFUSE)→
AST/LSP symedit`. Fail-closed na ambiguous (nikad pogađaj). Atomic staged→verify(3.5)→commit,
dirty-tuđi-rad se ne gazi (5.3). **DIFERENCIJATOR:** symedit (AST-scoped rename/refactor) + refuse-
on-ambiguous — Aider/Cline/Codex nemaju. **RED:** 2 identična matcha→REFUSE; symedit rename ne
dira string/komentar (AST). **Pareto:** Aider(multi-format/per-model)+Cline(checkpoint)+Codex+ladder+symedit (naš).

**4.4 pretraga koda (MEHANIZAM+ADAPTER, coding, PRODUBLJENO):** slojevito text→AST→simbol→
semantika→arhitektura. `SearchStack{ripgrep, ast-grep, serena-LSP, codeindex, callgraph, archmap}`.
Svaki Hit datamarked-untrusted + source_uri. **DIFERENCIJATOR dev-inteligencija IZNAD pretrage:**
pre-indexirani simboli + call-graph + arhitektura-kao-queryable-graf (NEXUS) — konkurenti staju na
LSP+grep. **RED:** stale index→re-scan, ne krivi hit. **Pareto:** ripgrep+ast-grep+Serena+dev-intel (naš).

**4.5 web/browser (ADAPTER, `browser` flag):** browser-use+Playwright-MCP+Skyvern; Camoufox
runner-up SAMO uz ToS/robots policy jezgre. Egress-gated (S6.3). **RED:** browser bez egress-checka→blokiran.

**4.6 MCP klijent (MEHANIZAM+ADAPTER):** klijent + **P1.3 transport/auth ugovor** (local-stdio vs
remote-https, peer-identity pin, scoped OAuth, tool-list size-limit + injection-fencing, cancel=S7).
**RED P1.3:** `test_mcp_wrong_pinned_peer_gets_no_credentials`. **Pareto:** goose+Cline+Codex+auth-ugovor.

**4.7 tool-exposure-budget (MEHANIZAM, jezgra):** per-turn minimalni capability-scoped tool-view,
lazy schema-fetch, mjeri schema-token + selection-miss-rate (Pi princip: manje alata pobjeđuje).
**RED:** turn dobiva samo scoped set, ne cijeli registry. **Pareto:** pi-mono+MCP-discovery+ToolView (naš).

**4.8 computer-use (`computer-use` flag, iz A4-G1):** OS-wide GUI input-injection; `InputInject` KEY/MOUSE/TYPE=RED-tier, SCREENSHOT=YELLOW; approval S6.0.Decide→6.9+P1.4; keystroke u nepoznato polje = approval-po-tipu-znaka (NE password-auto-detekcija — neizvodiva). Dizajn: GAPFIX-kilo. **Scope: ODLUKA-PRD.**

**S4 = 2 coding-diferencijatora:** edit-ladder+symedit (4.3), dev-inteligencija (4.4). gortex
diff/exec/mcp = greenfield Go. **Honest gap:** 4.1/4.2/4.4/4.7 bez dediciranog Annex RED
(posredno P1.1/P1.3/P2.2). **Verifikacija:** kandidati stvarni (Mentat izbačen — neaktivan).
**S4 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S5 — KANONSKA SINTEZA (Workspace/VCS/checkpoint; coding sigurnosna mreža; 3-way, Pareto PASS)

Git: `go-git` (pure-Go, bez cgo) ili `exec git` fallback. Gate P1.2 (orphan/lock sweep).

**5.1 checkpoint/rollback (MEHANIZAM):** `ShadowGit` zaseban repo IZVAN radnog stabla (ne zagađuje
korisničku povijest — **diferencijator** vs Aider auto-commit/Cline grana); `Snapshot(step)` PRIJE
svakog side-effecta (content-addressed, jeftino), `Rollback` byte-identičan, checkpoint-ID=journal
offset. **RED:** Rollback→byte-identično
pre-stanje, ne dira ne-staged korisničke promjene. **Pareto:** Cline+Aider+LangGraph+shadow-git-izvan-stabla (naš).

**5.2 workspace izolacija (MEHANIZAM+ADAPTER):** git worktree per-task/per-subagent (`NewIsolated
(owner)` detached, isti .git), eksplicitan `SyncBack` first-writer-wins, `Cleanup` lock-release;
container backend (S6) isti interface. **Temelj za S12** (svaki subagent svoj worktree, nema
konflikta). **RED P1.2:** `test_sigkill_restart_reaps_owned_
tree_only` + symlink izvan roota→odbij, djelomični cleanup→CLEANING nikad RELEASED. **Pareto:**
OpenHands+SWE-agent+Sandbox-Agents(beta)+worktree-per-subagent (naš).

**5.3 dirty/atomic/conflict/provenance (MEHANIZAM):** `AtomicWriter` tmp→fsync→rename (nikad in-
place, nikad pola fajla); `TaintTier{GENERATED|USER|UNKNOWN}` first-writer-wins (USER-tainted +
GENERATED write→CONFLICT, tuđi rad se ne gazi); **sandbox-neutralni atestor** (obsidian): provenance
iz NIŽEG trust sloja (sandbox vidio stvarni argv/fs/net), runtime NE atestira sam sebe (6.0
trust-boundary), export W3C PROV-O (veže 6.2+15.3). **RED:** konkurentni GENERATED-vs-USER edit→CONFLICT,
korisnički sadržaj sačuvan; atomic write usred pada→stari ili novi, nikad pola. **Pareto:** Aider+
Codex+Cline+sandbox-atestor-PROV-O (naš).

**S5 diferencijator:** shadow-git izvan stabla (byte-identičan undo bez zagađenja povijesti) +
worktree-per-subagent (S12 temelj) + sandbox-neutralni atestor. **Honest gap:** 5.1/5.3 bez
dediciranog Annex RED (P0.11 u Annexu A). **Verifikacija:** kandidati stvarni; atomic-write greenfield Go. **S5 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S6 — KANONSKA SINTEZA (Sigurnost; NAJKRITIČNIJA — membrane GREENFIELD od nule + parity-suite; 3-way, Pareto PASS)

**Načelo (audit-konsenzus):** izbrušene membrane se PRESAĐUJU s parity testom, ne pišu iznova.
Membrane greenfield u Go, fail-closed, non-bypassable (GORTEX ne postoji — piše se od nule).

**6.0 trust/PEP (MEHANIZAM):** OPA-stil policy decision point ("smije li subjekt X alat Y nad Z"),
executor fail-closed provodi. **Pareto:** Codex-approval+OpenHands-analyzer+OPA.

**6.1 permission gating (MEHANIZAM):** `internal/security/perm` shlex-token analiza po argumentu (GREEN/
YELLOW/RED, 5-tier), hooks pre_tool deny. **RED:** RED-regex komanda→deny. **Pareto:** Codex+goose+gptme+per-argument.

**6.2 sandbox (MEHANIZAM, per-OS build-tag, GREENFIELD od nule — NAJVEĆI BLOKER):** `Boundary` interface;
`sandbox_linux.go` (Landlock 3 syscalla preko `x/sys/unix` BEZ cgo + seccomp prctl BPF + bwrap),
`sandbox_windows.go` (Job Object KILL_ON_JOB_CLOSE + restricted token), `sandbox_darwin.go`
(Seatbelt sandbox-exec adapter — nema Landlock ekvivalent). **GREENFIELD (nema reusea)**
(`landlock_linux.go`/`sandbox.go`/`process_manager.go`/`tool_executor.go`/`netjail.go` — NE port,
NE prepisati; samo adapter koji ga zove). Opasni I/O u out-of-process helperu iza autentificiranog
IPC, NIKAD in-process fallback. **RED P2.2 + PARITY:** Landlock write na ne-dopušten path→EPERM
identično NEXUS membrani; bez dopuštenog patha→fail-closed. **Pareto:** Codex+OpenHands+gVisor+parity-suite (naš, greenfield).

**6.3 egress (MEHANIZAM):** allowlist + **SSRF metadata hard-block** (169.254.169.254 u decimal/hex/
octal/mapped-IPv6 → REFUSE PRIJE diala) + redirect re-check + `netjail filtered_jail` (bwrap prazan
netns, jedini izlaz UDS→FilterProxy). **RED:** metadata-IP u bilo kojem kodiranju→block. **Pareto:** Codex+microsandbox+OpenHands+SSRF-hardening (naš).

**6.4 secrets (MEHANIZAM):** keyring, `internal/security/secrets` redact (entropija+shape), SECRET_ARGS. **RED:** secret u izlazu→redigiran prije journala (P0.3).

**6.5 injection-obrana + canary (MEHANIZAM):** datamark fence + guardian judge + deny-list;
**canary tripwire** per-install token, detekcija u izlazu→**FAIL-CLOSED blok cijele isporuke** +
audit+alert+rotacija+ljudska odluka (ne samo redakcija). **RED:** canary u izlazu→blok isporuke.
**Pareto:** LlamaFirewall+NeMo+LLM-Guard+canary-failclosed (naš); AgentDojo mjeri.

**6.6 authn/tenant (`service` flag):** channel-scoped allowlist deny-default, PKCE OAuth; multi-
tenant RBAC.

**6.7 data-governance:** BASELINE
(jezgra, svaki store: retention/delete/export/PII — Presidio + vlastiti SQLite purge) + SERVICE
dodatak (DSAR/legal-hold). **RED P2.1:** MEMORY_FORGET vs DATA_PURGE razdvojeni.

**6.8 supply-chain (`extensions` flag):** Sigstore/cosign potpis + OSV-Scanner + in-toto provenance;
`internal/security/supplychain` OSV + trust-by-hash. **RED P1.5:** post-signature artefakt tamper→publish blok.

**6.9 lifecycle enforcement (MEHANIZAM, jezgra):** `MiddlewareChain` success-faze `before_run→
before_tool→after_tool→before_deliver` (jezgreni policy PRVI i ZADNJI, plugin hook samo SUZI);
**on_error = GRANA iz svake faze** (causal-error+audit, BEZ kasnijih success-hookova). + **P1.4
draft→approve→commit→verify→compensate** za ireverzibilne side-effecte. **RED P1.4:** `test_approval_cannot_authorize_modified_effect`. **Pareto:** NEXUS-hooks+
Claude-Code-referent+DeepSeek-Cordis+on_error-grana (naš).

**6.10 exec-auto-reviewer (`service`, iz A9/OpenClaw-O6):** exec pregledan NAKON izvršenja (host-node phase) — jača 6.9. Dizajn: GAPFIX-kilo. **Scope: ODLUKA-PRD.**

**S6 = membrane GREENFIELD od nule + parity-suite, per-OS build-tags, fail-closed svugdje.**
**Verifikacija:** kandidati stvarni; membrane greenfield (nema NEXUS izvora). **S6 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S7 — KANONSKA SINTEZA (Pouzdanost; JEDINI owner retry/cancel P0.2; 3-way, Pareto PASS)

**S7 je JEDINI vlasnik retry/deadline/cancel** — 2.2/3.3/3.4 delegati; svaki fizički pokušaj nosi
`s7.AttemptGrant`.

**7.1 timeout/cancel/retry-taksonomija (MEHANIZAM):** `ExecutionPolicy{operation_id,effect_class,
deadline,max_attempts,attempt_timeout,backoff,retryable_codes,fallback_targets,idempotency_key,
cancel_token}` + `AttemptGrant{attempt_no,target,expires_at,grant_nonce}`; stanja PENDING→GRANTED→
RUNNING→{SUCCEEDED|FAILED_RETRYABLE|FAILED_TERMINAL|CANCELLED|UNKNOWN}; samo S7 iz FAILED_RETRYABLE
izdaje sljedeći grant; IRREVERSIBLE+UNKNOWN se NE retry-a bez reconciliationa. context za deadline/
cancel (propagira u child). **RED P0.2:**
`test_adapter_cannot_self_retry`. **Pareto:** Temporal+LangGraph+tenacity+S7-sole-owner (naš).

**7.2 idempotency/budgets/lease/fencing (MEHANIZAM, jezgra — POPRAVLJA NEXUS bug):**
- **Queue (P2.3):** `QueueTask{state,attempt,lease_id,owner_worker,fencing_token,heartbeat,
 lease_expires,result_hash}`; `Claim` atomski CAS READY→LEASED uz `fencing_token++` (single-winner);
 `Heartbeat` vrijedi samo za aktualni lease; `Reclaim` expiry/crash→READY s novim pokušajem + STROGO
 većim tokenom; stari token na commit→`STALE_FENCING_TOKEN`; ACK tek nakon durable commit;
 COMMIT_PENDING+nepoznat učinak→UNKNOWN_EFFECT→RECONCILING (NIKAD ravno READY). **Popravlja audit-
 nalaz** (NEXUS queue.py nema lease/reclaim za `running` nakon pada).
- **ResourceBudget (P2.2, IZVRŠNI ne advisory):** wall/CPU/RSS/disk/file/process/socket/net/output/
 tokens/cost/tool-calls/loop-steps, svaki SOFT|HARD; hard→atomic fence+cancel+drain/kill owned
 stablo (S1.2 process-group + S6); child budget ≤ preostali parent; gaugeovi knjiže PEAK; cleanup≠
 success. **cost-checkpoint** (obsidian): budget provjera PRIJE skupe operacije u grafu.
**RED P2.3:** `test_reclaimed_worker_cannot_commit_with_stale_fence` (A token 7 zamrznut, B token 8
commit, A sa 7→STALE, točno jedan rezultat=B). **RED P2.2:** `test_hard_process_budget_kills_
descendants`. **Pareto:** Temporal+Restate+Dapr+lease/fencing/executable-budget (naš — popravlja bug).

**7.3 crash-recovery + durable-delivery (MEHANIZAM):** resume idempotent iz event-loga (journal
fold, ne mutable state); WAL spine (`modernc.org/sqlite`); **N9 durable-delivery:** `run_done ≠
result_delivered` DVA trajna stanja, transactional outbox + idempotent-delivery + receipt (gateway
crash ne gubi odgovor ni ne ponavlja side-effect). **Startup orphan/lock sweep (P1.2):** `ResourceLease
{PROCESS_TREE|WORKSPACE_LOCK|TEMP_DIR}` + start-token (S1.2), OWNED→ORPHAN_SUSPECTED→FENCED→CLEANING→
RELEASED; nedokazivo vlasništvo→QUARANTINED ne kill. **RED P1.2:** `test_sigkill_restart_reaps_owned_tree_only`. **Pareto:** LangGraph+Temporal+OpenHands+
durable-delivery-outbox+orphan-sweep (naš).

**S7 = popravlja glavni audit-nalaz (queue lease/fencing) + N9 durable-delivery.** **Verifikacija:**
Temporal/Dapr stvarni; Restate source-available (matrica). **S7 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S8 — KANONSKA SINTEZA (Kontekst management; 3-way, Pareto PASS)

**8.1 window-budžet (MEHANIZAM):** `ContextBudget` token-mjerenje + hard-limit + pressure-state +
fold; fallback estimate vidljiv. **Pareto:**
Aider+letta+OpenHands.

**8.2 kompakcija (MEHANIZAM, jezgra):** anchored 8-sekcijski summary (cilj/napredak/odluke/greške/
otvorena-pitanja/artefakti/sljedeći-korak/ograničenja). **KLJUČNO P0.3:** sažetak NASLJEĐUJE
lineage — `trust_class(sažetak)=MAX(izvori)`, `sensitivity=MAX`, `lineage=union+summary-id`;
sažetak NE smije oprati untrusted→SYSTEM/instrukcija (injection vektor). Branch-summary (pi-mono).
**RED P0.3:** `test_s0_rejects_provenance_laundering`
(untrusted kroz compactor koji skine lineage→PROVENANCE_DOWNGRADE). **Pareto:** letta+OpenHands+pi-mono+lineage-monoton (naš).

**8.3 repo-mapa (MEHANIZAM, coding):** Aider tree-sitter + **PPR (personalized PageRank) rangiranje**
+ NEXUS `archmap` (arhitektura kao queryable graf — coding-diferencijator iznad pretrage). Skalirano
po token-budžetu. **Pareto:** Aider+Repomix+Continue+archmap-PPR (naš).

**8.4 cache (MEHANIZAM, dva sloja):** (a) API `cache-control` propagacija; (b) lokalni prefix/
RadixAttention (SAMO self-hosted). **Cache-key MORA nositi (codex#19):** namespace/tenant/principal-
scope/authz-policy-version/sensitivity/corpus-version/model/prompt-hash/purge-generation — bez
wildcard fallbacka; SECRET=NO_STORE; purge/revocation povećava generation. **RED P2.4 (codex#19):** `test_cross_tenant_cache_key_cannot_alias`.
**Pareto:** LiteLLM+vLLM+SGLang+tenant-scoped-key (naš).

**8.5 observation-pruning (MEHANIZAM):** filter-PA-komprimiraj tipiziran adapter PO ALATU — test
čuva failures, git-diff čuva hunks, crawl čuva citations; raw artefakt po referenci + fidelity eval.
**RED:** pruning čuva error-linije/exit-status.
**Pareto:** RTK+Headroom+SWE-agent+crush (naš, per-tool typed adapter).

**S8 = lineage-monoton kompakcija (anti-injection) + tenant-scoped cache + archmap-PPR coding-
diferencijator.** **Honest gap:** 8.1/8.3/8.5 bez dediciranog Annex RED (P0.3 pokriva 8.2, codex#19
pokriva 8.4). **Verifikacija:** kandidati stvarni (RTK/Headroom obsidian, matrica). **S8 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S9 — KANONSKA SINTEZA (Memorija; `memorija` flag osim 9.1=jezgra; 3-way, Pareto PASS)

**9.1 sesije (JEZGRA):** save/resume=svaki stateful harness (naslanja 7.3); branch/user-visible=
profil. Memory-SPINE=SQLite-WAL (`modernc.org/sqlite`, bez cgo). **Pareto:** goose+OpenHands+Codex+spine.

**9.2 perzistentna memorija (`memorija`):** 5-scope spine (`internal/memory`), memvec+kg+entres;
write-approval DEFAULT ON, untrusted-context labeling (P0.3). **P2.1 MEMORY_FORGET (reverzibilno,
zabrana recalla) vs DATA_PURGE (ireverzibilno, S6.7 nadjačava, briše i tombstone).** **RED P2.1:** `test_purge_cannot_complete_with_residual_copy`. **Pareto:** letta+mem0+zep+forget/purge-split.

**9.3 učenje-iz-iskustva (`memorija`):** Hermes Agent (produkcijski OSS, agent-authored skills —
PAŽNJA write_approval default OFF u Hermesu, MI ga uključujemo); reasonbank/reflexion/selfimprove
quality-gated (promocija samo iz verificiranog). **Pareto:** Hermes+cognee+research-referenti.

**9.4 konsolidacija/decay (`memorija`; Decay+Audn P0, ostalo v2):** `Consolidator.Dream(episodes)→gist+KG`
(offline sleep-time); `Decay` FSRS-lite mijenja RANG ne postojanje (LOSSLESS — original uvijek u
hot/warm/cold arhivi); `Audn.Resolve` kontradikcije ADD/NOOP/SUPERSEDE na read (ne silent overwrite);
`MemGit` verzionirani write (undo memorije); `MemAssoc` spreading-activation; temporalni upiti ("što
sam znao TADA"). **RED:** decay NE briše bajtove; konsolidacija
untrusted epizode→gist ostaje UNTRUSTED (P0.3). **Pareto:** letta+mem0+zep+sleep-time-decay-AUDN (naš).

**9.5 PersonalProfile (`profiles` flag, iz A4-G2):** izolacijska jedinica posao/privatno/obitelj; deny-default memory/secrets/channels/cache; write u `work`→query u `family`=0 hitova. Dizajn: GAPFIX-kilo. **Scope: ODLUKA-PRD.**

**9.6 ObligationStore (asistent-jezgra, iz A4-G4):** trajni cilj s eksplicitnom done-definicijom, `MarkDone` SAMO uz Evidence; veže 9.1+3.6; odvojeno od 9.2 razgovorne memorije i 7.2 infra-queue. **Scope: ODLUKA-PRD.**

**S9 = memorija GREENFIELD (Decay+Audn P0, Dream/MemGit/MemAssoc v2) + forget/purge trust-razdvajanje.**
**Honest gap:** 9.1/9.3/9.4 bez dediciranog Annex RED (P0.13 "decay-never-destroys-bytes" — u Annexu A).
**Verifikacija:** kandidati stvarni; salvage interni (audit potvrdio). **S9 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S10 — KANONSKA SINTEZA (Retrieval; `vector/graph-retrieval`+`preload-cache` flagovi; 3-way)

Većina ADAPTER (sučelje+primarni-pick, ne spajaj baze); ACL/provenance/freshness=MEHANIZAM jezgra.

**10.1 ingestion (ADAPTER, READ-ONLY):** RAGFlow-deepdoc/docling/unstructured; runners-up Marker/
MinerU/OCRmyPDF/Surya (obsidian). SurfSense live-konektori (typed REST + MCP tool).

**10.2 chunking (ADAPTER):** semantic(cosine-drop)/parent-
child/contextual usporedba; LlamaIndex+chonkie+RAGFlow.

**10.3 hibrid+rerank (ADAPTER):** Qdrant
(dense+sparse+RRF)+Haystack + **cross-encoder** (BGE preko subprocess-sidecara — ne in-process Go) + LanceDB/sqlite-vec
(file-based v1)+query-rewrite.

**10.4 GraphRAG (`graph-retrieval`):** Microsoft-GraphRAG/LightRAG/cognee obrazac; greenfield Go KG-store (multi-hop nad spine).

**10.5 CAG (`preload-cache`, MEHANIZAM decision-rule):** `CAGDecision.Decide(corpus,
authz)→CAG|RAG|HYBRID` (mali/stabilni/AUTORIZIRAN→CAG preload; velik/dinamičan→RAG; neautoriziran→
RAG). CAG-izvršitelj (KV-preload) je `reserved` u v1 — ako executor ne postoji, odluka `CAG` DEGRADIRA u RAG (ne dangling). Puni preload prati 8.4 self-hosted infra. **RED:** neautoriziran korpus→RAG
(ne preload). **Pareto:** decision-rule (top-3 pošteno prazno).

**10.6 retrieval-auth (MEHANIZAM jezgra + ADAPTER):** **ACL PRIJE candidate-generation** (deny-
default, NE post-filter); `RetrievalRequest{principal,tenant,corpus_version,policy_version}`; cache
invalidacija nakon revocationa; nedopušten dokument NIKAD u candidate/rerank/trace. Qdrant-payload-
filter/OpenSearch-doc-security/Vespa. **RED codex#2 (Tier-3 BACKLOG — ne formalni Annex):** nedopušten dokument ne uđe.
**10.7 quality-gate (`vector-retrieval`, MEHANIZAM):** golden-queries + Recall@k/MRR/nDCG +
freshness-SLA + citat-do-izvornog-chunka + stale-index-detekcija; odgovor NIJE grounded ako retrieval
nije izmjeren. Ragas/BEIR/Phoenix. Cross-ref 16.2 (generički ratchet). **RED:** grounding-tvrdnja bez
mjerenja→odbij.

**S10 = ADAPTER-sučelja (ne spajaj engine) + ACL-prije-retrievala + iskren CAG-UNCLEAR + quality-
gate.** **Verifikacija:** kandidati stvarni (obsidian runners-up matrica). **S10 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S11 — KANONSKA SINTEZA (Prompt/instrukcijski sloj; 3-way, Pareto PASS)

**11.1 assembly (MEHANIZAM, jezgra):** deterministički prompt-assembler + precedence + trust-fencing
(untrusted ContextBlock NE postaje instrukcija — P0.3) + provenance. **Pareto:** Aider+SWE-agent+OpenClaw.

**11.2 skills (DATA):** Agent-Skills-standard (SKILL.md) + Hermes agent-authored + Codex-skills.
**Reversa** = signed skill-pack OVDJE (data, ne 17.1 plugin): legacy→spec+migracijski-plan PRIJE
mutacije, `allowLegacyEdits:false` provodi kernel 6.1/6.9. Source-grounded synthesis: skill nosi
source-manifest+citate+expiry+eval. **Pareto:** goose+Agent-Skills+Codex+Reversa.

**11.3 projektne-instrukcije:** AGENTS.md standard (Codex/OpenCode/Cline .clinerules).

**11.4 prompt-optimizacija:** DSPy+TextGrad+promptfoo.

**11.5 skill-lifecycle/governance (MEHANIZAM jezgra, `extensions`):** `install→scan→pin→activate→
measure→update→rollback→archive`; scan=injection/exfil/tool-chain/install-skripte (pre-install);
**P1.1 TOCTOU runtime hash-on-load** (`SkillLock.VerifyOnLoad` — provjera pri SVAKOM loadu, canonical
root bez symlink-escapea, mismatch→QUARANTINED); write_approval DEFAULT ON, auto-install DEFAULT OFF,
self-authoring inertan do gatea. **RED P1.1:** `test_skill_swap_after_install_is_quarantined`.
**Pareto:** NEXUS-forge+Cisco+ZIRAN+TOCTOU-hash (naš).

**11.6 role→agent→skill data-manifest (MEHANIZAM interpreter=jezgra, sadržaj=data):** role/agent-
template/team/skill=VALIDIRANI PODATKOVNI MANIFESTI (nova rola=nova mapa, nula izmjene jezgre —
NEXUS lekcija); kernel jedini rješava precedence/capability/budget/output-contract; core NE zna
nazive domena. **Pareto:** CrewAI-YAML+OpenClaw+
MetaGPT+manifest-interpreter (naš).

**S11 = TOCTOU skill-hash + role-data-hijerarhija (nula-izmjene-jezgre) + Reversa-kao-data.**
**Verifikacija:** kandidati stvarni; salvage interni. **S11 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S12 — KANONSKA SINTEZA (Orkestracija/multi-agent; `multi-agent` flag; 3-way, Pareto PASS)

**12.1 subagenti/delegacija (MEHANIZAM):** subagent-orchestrator; **svaki subagent = vlastiti
worktree (5.2)** — nema dijeljenog radnog stabla, nema kolizije, eksplicitan sync-back (first-writer-
wins). solo/parallel/sequential; fenced handoff envelope. **Pareto:** LangGraph+CrewAI+MAF+worktree-per-subagent (naš).

**12.2 workflow/DAG (MEHANIZAM):** DAG scheduler + cycle-guard + wave-scheduler; deterministički +
model-led koraci (Google ADK obrazac). **Pareto:** LangGraph+
Temporal+ADK.

**12.3 routing:** POTROŠAČ 2.6 (jezgra owner tamo); ovdje samo "koji subagent koji model".

**12.4 HITL (MEHANIZAM):** greenfield `Gate` persistira jednokratnu ljudsku odluku (preživi procesnu
granicu); durable approval token (P1.6): exact-intent+expiring+single-use, ne može proširiti kernel
policy. **Pareto:** LangGraph+OpenAI-SDK+MAF.

**12.5 A2A interop (MEHANIZAM+ADAPTER, A2A spec):** `AgentCard`+`A2AServer`(idempotent task IDs)+
`A2AClient.Discover`. **SIGURNOSNA CIJENA (card NIJE autorizacija):** schema+size validacija PRIJE
parsea, allowlist/SSRF obrana pri discoveryju (6.3 metadata-IP/redirect-recheck), AuthN/AuthZ (P1.3),
timeout/cancel (S7), idempotent task-ID, audit-korelacija (15.3); remote agent=UNTRUSTED peer
(P0.1 trust_class, izlaz ne postaje instrukcija). Referencira 0.1 sheme + 0.3 capability.
**RED codex#3 (Tier-3 BACKLOG — ne formalni Annex):** A2A adapter conformance (version-negotiation/revocation/replay/bypass).
**Pareto:** A2A-spec+MAF+ADK+sigurnosni-conformance (naš).

**12.6 council (MEHANIZAM, P2.7):** `CouncilRequest` sealed nezavisni review → anonimni cross-review
→ chairman sinteza; svi nad istim `artifact_revision_hash`; chairman NE izbacuje materijalni dissent
bez adjudication-recorda; quorum+budget hard-gate; council NE promovira sam ni ne zamjenjuje 16.6
checker. **RED P2.7:** `test_council_cannot_drop_
sealed_material_dissent`. **Pareto:** NEXUS-council+MAF+CrewAI-hierarchical+dissent-očuvan (naš).

**12.7 flows (`multi-agent`, iz A10/OpenClaw-O9):** workflow-graf; ide u 12.2 DAG owner (superstep-mode). **Scope: ODLUKA-PRD.**

**12.8 boards (`service`, iz A10/OpenClaw-O10):** kanban/task-board; transakcijske tablice (journal owner P0.3). **Scope: ODLUKA-PRD.**

**S12 = worktree-per-subagent + A2A-card-nije-autorizacija + council-dissent-očuvan.**
**Verifikacija:** A2A-spec/MAF/ADK stvarni. **S12 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S13 — KANONSKA SINTEZA (Multimodal; composable flagovi; 3-way, Pareto PASS)

Većina ADAPTER (backend iza sučelja) + MEHANIZAM (input-validation/MIME-size-caps/egress-consent jezgra).

**13.1 vision (`vision`):** browser-use screenshot+DOM / Open-Interpreter / OpenHands; egress-gated.
**13.2 image-gen+canvas (`image-out`):** ComfyUI/InvokeAI/diffusers;
+ **canvas** strukturirani-dijagram-JSON→SVG/PNG (schema-validiran + sandboxiran render, NE "injection-
proof" tvrdnja — SVG sanitizacija obavezna).

**13.3 voice (`voice`):** pipecat/LiveKit-Agents/speaches STT+TTS.

**13.4 video (`video`):** FFmpeg (deterministička obrada)/WhisperX (word-level+diarizacija)/ComfyUI;
runner-up Remotion/HyperFrames (HTML→MP4).

**13.5 dokument-mutacija (`documents`, INSTANCA Artifact-transformer ugovora):** typed-op → preview/
dry-run → atomic-staging (5.3) → output-verify → commit/rollback; MIME/size caps; NE anatomija-po-
formatu (isti ugovor za PDF/DOCX/spreadsheet/sliku — novi format = adapter, ne podsekcija). Pure-Go pdfcpu/rsc.io-pdf; teški formati (OCR/potpis)
kroz subprocess-sidecar (OCRmyPDF/pyHanko), nikad in-process. Read-ingestion odvojen (10.1).
**RED:** sign/edit bez consent/atomic→odbij.

**S13 = generički Artifact-transformer (ne format-anatomija) + canvas-sandboxiran + input-caps jezgra.**
**Verifikacija:** kandidati stvarni. **S13 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S14 — KANONSKA SINTEZA (Sučelja UX; jezgra=library+tanki adapteri; 3-way, Pareto PASS)

**Načelo:** jezgra=library, sva sučelja=TANKI adapteri nad istim application-API (bez poslovne
logike u UI-ju). Minimalni CLI/TUI rano (P1); puni UX kasno (P4).

**14.1 CLI/TUI (jezgra minimum):** bubbletea TUI (garantiran single-binary) + Codex-stil dispatcher;
crush ♦ referent. Desktop Wails/Fyne (odluka kasnije — WebView vs pure-Go toolchain).

**14.2 headless/API/embeddable-SDK (`full-UX`):** OpenAI-kompatibilan REST/SSE +
**embeddable SDK isti S0/6.9/7.x put** (dry-run default, ne poseban nereentrantan executor — NEXUS
audit dug).

**14.3 web-UI:** Open-WebUI/LibreChat/OpenHands +
serve-dashboard (loopback+Host-check, read-only, XSS-safe).

**14.4 IDE/ACP:** Agent-Client-Protocol standard (wire+capability-negotiation, Rust/TS/Py/Java/
Kotlin SDK) + Cline/Continue; workspace-trust/URI-normalizacija/dirty-buffer conflict granice.
**14.5 messaging/channel-gateway (`channels`):** auth/routing/delivery-state-machine=jezgra,
adapteri (TG/Slack/Discord/Matrix/Signal/WhatsApp/email)=pluginovi; ugovor identity/deny-default-
allowlist/threading/receipt/idempotent-retry/**udaljeni-HITL** (agent pita na mobitel, run čeka);
durable-delivery (7.3); **`channels requires extensions`** (P1.6). **RED P1.6:** `test_channels_contract_fails_closed` (bez extensions-closurea→INCOMPLETE + replay-approval→APPROVAL_REPLAY).

**Napomena (obsidian, S2 veza):** FreeLLMAPI/awesome-freellm-apis = provider-config PRESET (data),
opcionalan API-key put za razvoj/prototip; NE default, NE produkcija (njihov vlastiti disclaimer +
ToS rizik) — ide u 2.1 kao preset, ne u UX jezgru.

**S14 = library-jezgra + tanki adapteri + channels-requires-extensions + embeddable-SDK-isti-put.**
**Verifikacija:** kandidati stvarni (crush ♦ FSL). **S14 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S15 — KANONSKA SINTEZA (Observability; sve PROJEKCIJE journala P0.3; 3-way, Pareto PASS)

**Načelo (P0.3, codex#21):** log/trace/transcript/metrics/audit su PROJEKCIJE jednog write-ownera
(`EventJournal`), NE paralelni sinkovi; redakcija PRIJE journala.

**15.1 tracing (MEHANIZAM):** OTel-GenAI semantika (span/log/metrika, correlation run/turn/tool/
attempt/parent/sequence); OTLP exporter=adapter, stabilan ID-mapping (replay ne mijenja trace-ID).
Langfuse/Phoenix=backendovi.

**15.2 cost-dashboardi:**
LiteLLM/Langfuse/Helicone spend po ključu/trace.

**15.3 transcript/audit (MEHANIZAM):** **tamper-evident** append-only hash-lanac + verify() (tamper
blokira UPDATE/DELETE) + **vanjski potpisani checkpoint** (lokalni lanac SAM nije dokaz protiv
potpunog rewritea — Sigstore-Rekor/immudb/Trillian).

**15.4 metrics/SLO:** Prometheus/Grafana/OTel-Collector; latency/error/
availability SLI (izveden iz journala).

**15.5 semantic-agent-health (MEHANIZAM jezgra):** distinktno od 15.4-SLO i 18.3-liveness — hvata
agenta ŽIV-ali-degradira: recidivism/correction-rate/verified-success-trend/fallback-drift/swallow-
counter → S7 circuit-breaker prije lažno-zdravog rada. **RED:** rast recidivisma→breaker (ne čeka liveness-fail). Vault-tvrdnja
"većina harnessa nema self-critique/health" = hipoteza, ne "0/28".

**S15 = journal-projekcije (jedan write-owner) + tamper-evident+vanjski-anchor + semantic-health.**
**Honest gap:** 15.1/15.2/15.4/15.5 bez dediciranog Annex RED (P0.3 pokriva pisanje). **Verifikacija:**
OTel/Prometheus/Grafana stvarni. **S15 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S16 — KANONSKA SINTEZA (Eval; dokazuje vodeće načelo; 3-way, Pareto PASS)

Zahtjevi: versioniran task-corpus, holdout, deterministic-fixtures, snapshot-identiteta, ablation
(harness može postati RED), paired-baseline-gate, cost/latency/turn-budžeti, flaky-politika.
**Matrica dobiva HARD-LIMIT pojavljivanja po projektu (anti over-indexing) + critical-gate floor**
(0.1/0.2/7.2 se ne kompenziraju perifernim IMA).

**16.1 task-benchmark (MEHANIZAM):** SWE-bench (benchmark)/Aider-suite/Inspect + **app-contract**
deliverable-level (ekrani/tokovi/persistence, ne test-count).

**16.2 ratchet:** promptfoo/DeepEval/Opik + baseline; retrieval-ratchet cross-ref
10.7.

**16.3 red-team:** garak/PyRIT/promptfoo-redteam +
**STRIX** (usestrix/strix — dinamički pentest agent, PoC-validacija) + Shannon; kandidat `security-
audit` skill-pack.

**16.4 feedback-flywheel + credit-ledger (MEHANIZAM, kanonski owner):** provenance-aware credit —
memorija/skill/ruta prima kredit SAMO iz revision-bound VERIFICIRANOG ishoda (test-gate/loop-judge,
NIKAD self-report); epsilon-greedy; feeds 2.6/9.3/11.5. Dormantan jezgreni ugovor dok potrošač ne
aktivira (Q3). Langfuse/Opik/AgentOps annotations.

**16.5 trajectory-export (`destilacija`):** SWE-gym/Axolotl/ShareGPT-JSONL.

**16.6 anti-sycophancy/neovisna-provjera (MEHANIZAM jezgra + `multi-agent`):** minimalni checker=
jezgra, council=multi-agent strategija. Checker OBARA premisu+zaključak na ARTEFAKTIMA (git-diff+
exit-code+determinističke signale), NE prozi (checker≠worker); confidence iz logproba (UQLM);
blokira "uspjeh" kad dokaz ne prati tvrdnju. **CODING-diferencijator** (nitko od Kilo/OpenHands/
OpenCode nema). Council (P2.7) sealed→anonimni-cross→chairman, dissent-očuvan, NE zamjenjuje checker.
**RED
P2.7:** `test_council_cannot_drop_sealed_material_dissent`. **Pareto:** vlastiti-checker+UQLM+Inspect+council.

**S16 = evidence-graded checker (coding-diferencijator) + verified-credit-ledger + strix-red-team +
matrica-hard-limit.** **Verifikacija:** kandidati stvarni (strix 36k aktivan). **S16 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# S17 — KANONSKA SINTEZA (Ekstenzibilnost/distribucija; 3-way, Pareto PASS)

**17.1 plugin-sustav (`extensions`):** DeepSeek-Harness Cordis DI "everything-is-a-plugin" (najdublji
pristup) + goose-MCP + OpenCode; **pluginovi iza 6.8/11.5 gatea** (signature/scan/permission-delta
PRIJE učitavanja — P1.5).

**17.2 update/kanali:** Codex-kanali + goose-self-update + Aider-višekanalni;
**TUF sigurnosna-osnova** (rollback/freeze zaštita metadata lanca).

**17.3 packaging:** goose-Go-single-binary + **dist/cargo-dist** (reusable
cross-platform artefakt) + uv (Python-strane). Naš cilj: `CGO_ENABLED=0` single-binary.

**17.4 field-diagnostics/upgrade-bridge (`service`):** opt-in scrubban reproducibilan bundle (verzije
+ korelacijski-ID) → issue → VERIFICIRAN update, bez auto-slanja/instalacije (teren: bug→bundle→issue→
upgrade). **RED P1.5:** `test_post_signature_artifact_tamper_blocks_publish`.

# S18 — KANONSKA SINTEZA (Deployment/operacije; `service` flag; 3-way, Pareto PASS)

**18.1 worker/queue/scaling:** OpenHands konkurentne-sesije + Dify multi-worker + Temporal queue-
lease (iz S7 — P2.3 fencing). headless worker/API razdvajanje.

**18.2 multi-tenant-gateway:** LiteLLM-proxy per-tenant ključevi/budgeti +
Portkey + Dify-kvote; non-null tenant_id na svakom persistentnom envelopeu (codex#5).

**18.3 health/
rollout/canary:** Kubernetes/Argo-Rollouts/Flagger (komponente); SHA-pinned canary (NEXUS live-canary
obrazac).

**18.4 state-migracije/backup:** backend je već SQLite-WAL (Postgres samo `service`) — NE čeka S0.4 (S0.4=schema-verzioniranje, ne backend). Single-user backup = Litestream/WAL-snapshot SADA; Postgres backup prati `service`. Migracije
golang-migrate/embedded-SQL migracije + point-in-time backup (Litestream za SQLite).

**18.5 fleet (`service`, iz A10/OpenClaw-O11):** placement/registry nodova; fleet NIJE drugi retry-owner (S7 jedini). ID-ovi iz `internal/fleet/ids` (K3 anti-cikl). Dizajn: GAPFIX-codex. **Scope: ODLUKA-PRD.**

**18.6 device-pairing (`service`, iz A10/OpenClaw-O11):** identitet+pairing uređaja; ovisi o `fleet/ids`, ne o `fleet` (K3). Dizajn: GAPFIX-codex. **Scope: ODLUKA-PRD.**

**S17+S18 = plugin-iza-supply-chain-gatea + TUF-update + single-binary + service-deployment.**
**Verifikacija:** DeepSeek-Harness (rc, matrica), dist/TUF/K8s/Argo stvarni. **S17+S18 STATUS: DIZAJN (salvage uklonjen; RESOLVED/UNRESOLVED po podsekciji → PLAN-HOLES-CONSOLIDATED.md).**

---

# ═══ SVIH 19 SEKCIJA (S0-S18) DIZAJNIRANO — PLAN DIZAJN-POTPUN, IZVOR-NEPOTPUN ═══

**Datum:** 2026-09-01. Metoda: hibrid-hibrida sinteza po podsekciji (3 agenta neovisno → moderator
Pareto-floor merge). **108 podsekcija** (100 spec + 8 addendum), svaka: Tip · aspekti+izvor · Go sinteza · RED · Pareto.

**Coding-diferencijatori (bolji od Kilo/OpenHands/OpenCode):** evidence-graded loop (3.1/16.6),
stuck-detection (3.4), TIA (3.5), edit-ladder+symedit (4.3), dev-inteligencija (4.4), shadow-git (5.1).
**Popravljeni NEXUS bugovi:** queue lease/fencing (7.2). **GRADNJA OD NULE** (nema salvagea — svaki modul greenfield Go). **Auth-modovi** (API/OAuth/CLI-agent/subscription-opt-in). **Sigurnosne membrane
reuse-ne-rewrite** (parity test).

**SLJEDEĆE (dogovoreni tijek, 6-datoteka workflow):**
1. PRD.md (product-sloj — KORISNIK vodi) + ARCHITECTURE-ESSENTIALS.md (cheat-sheet kritičnih odluka)
2. HARD-QUESTIONS review-runda = ITERACIJA (agenti napadaju plan: što će puknuti/edge/over-engineered)
3. CLAUDE.md + AGENTS.md (nexus repo) + Go scaffold (folder-paketi + stub + data-modeli iz S0)
4. Go/no-go → kodiranje P0 kernela

---

# ADDENDUM A1 — OCR PIPELINE s hardware-routingom (korisnički zahtjev, spanira 2.4/10.1/13.5/retrieval)

**Cilj:** iz velike količine PDF-ova napraviti ODLIČAN OCR — slikovni PDF dobije TEXT LAYER +
metapodatke → tekst sa slike pretraživ u tražilici (full-text). Harness DETEKTIRA NVIDIA/CUDA i
bira put.

**Mehanizam (jezgra, veže 2.4 HardwareFit):**
```go
type OCRBackend int // UNLIMITED_OCR_GPU | CPU_FALLBACK
func SelectOCR(fit HardwareFit) OCRBackend {
 // NVIDIA CUDA + VRAM ≥ 8GB → UNLIMITED_OCR_GPU (najbolja kvaliteta, tablice/layout)
 // inače → CPU_FALLBACK (bez GPU ovisnosti, ne OOM, ne crash)
 if fit.CUDA && fit.VRAMBytes >= 8<<30 { return UNLIMITED_OCR_GPU }
 return CPU_FALLBACK
}
```

**GPU put (`local-inference`+GPU okidač):** **baidu/Unlimited-OCR** (MIT, 3B VLM, BF16 ~6GB, CUDA
12.9, one-shot dugi PDF + tablice/layout, do 32k tokena). Opt-in heavy (fail-closed ako VRAM ne
stane — 2.4 HardwareFit odbije, ne OOM). Adapter iza `OCRBackend`.

**CPU fallback put (bez NVIDIA):** **OCRmyPDF** (Tesseract) — dodaje NEVIDLJIVI text-layer slikovnom
PDF-u → izlaz je PDF/A: pretraživ + metapodatci, bez GPU-a; + **Surya** (CPU-capable layout/OCR) za
teš\e layoute. Isti `DocOp` Artifact-transformer ugovor (13.5): image-PDF → text-layer → atomic
staged → verify → commit. Marker/MinerU za layout-heavy.

**Pretraga (veže 10.1 ingest + 10.3 hibrid + 9.2 spine FTS):** ekstrahirani tekst → FTS index
(SQLite FTS5 u spine) + embed za semantičku pretragu → tekst sa slike nalaziv i egzaktno (FTS) i
semantički (vektor). Batch-processing velike količine PDF-ova kroz queue (7.2 lease) — otporno na pad.

**Ugovor/RED:**
- image-only PDF → nakon pipelinea full-text pretraga NAĐE tekst sa slike (RED: pretraga prije=0
 hitova, poslije=hit na sadržaj slike).
- GPU odsutan → CPU put IPAK proizvede pretraživ PDF/A (RED: nema NVIDIA → ne crash, ne GPU-import,
 izlaz pretraživ). GPU prisutan ali VRAM<8GB → HardwareFit odbije Unlimited-OCR + padne na CPU (ne OOM).
- batch 1000 PDF-ova, pad usred → queue-lease (7.2) nastavi, nema dvostruke obrade (idempotency).

**Profili/flagovi:** `documents` (mutacija=text-layer) + `vector-retrieval` (FTS/embed) + GPU put
uz `local-inference`+CUDA okidač. **Verifikacija:** Unlimited-OCR (MIT, GPU), OCRmyPDF (MPL/GPL,
CPU), Surya (aktivan), Tesseract (Apache-2.0) — stvarni.

**Placement u tijeku:** ovo je dopuna 2.4+10.1+13.5, NE nova sekcija — folda se u hard-questions
iteraciju (agenti provjere hardware-routing granicu + CPU-fallback kvalitetu).

---

# ADDENDUM A2 — LOCAL-MODEL FRESHNESS SCOUT (hardkodirano; korisnički zahtjev) + llmfit u 2.4

**Rupa koju zatvara:** 2.4 je imao hardware-fit (right-sizing), ali NIJE imao mehanizam da PRATI
ima li NOVIH lokalnih LLM-ova na tržištu koji stanu na hardver. Korisnik traži da to bude HARDKODIRANO.

**Mehanizam (JEZGRA, hardkodirano — analogno skill_scout iz 11.5, ali za lokalne modele):**
```go
type ModelScout struct{ sources []ModelSource; fit HardwareFit } // JEZGRA
// izvori (DATA): Ollama library, HuggingFace, llmfit katalog (94+ modela, raste)
func (s *ModelScout) Sweep(ctx) []ModelCandidate
// periodički (3.6 schedule/heartbeat): dohvati NOVE lokalne modele → HardwareFit (2.4) filtrira
// što STANE na ovaj hardver → llmfit-score (quality/speed/fit/context) → rangiraj
// vrati kandidate koji STANU i NADMAŠE trenutni pick
```
**Kod-vs-data:** scout ENGINE = hardkodiran (jezgra); izvori/katalog modela = DATA (config).
**SIGURNOSNA GRANICA (isto kao skill_scout):** scout **INFORMIRA** (report → CLI/kanal/dashboard),
NIKAD ne mijenja produkcijski model sam — čovjek/policy odobrava adopciju (ne autonomna zamjena;
konzistentno s "no autonomous change without gate" iz cijelog plana). Read-only sweep, paced.

**llmfit = primarni 2.4 kandidat (zamjenjuje Odysseus/Colibri runner-up):** ili (a) shell-out adapter
na instalirani `llmfit` binary (Rust, već na Piju — kao CLI-agent provideri, subprocess), ili (b)
konzumiraj llmfit KATALOG (94+ modela + scoring) kao ModelSource. Podržava Ollama/llama.cpp/MLX/
Docker-Model-Runner/LM-Studio (multi-GPU, MoE, dinamička kvantizacija) — pokriva 2.4 backendove.

**Veza:** 2.4 (hardware-fit right-sizing) + 3.6 (schedule/heartbeat trigger — periodički sweep) +
2.6 (routing može uzeti novi bolji lokalni model kad ga scout+HardwareFit odobre) + 15.5 (health —
ako novi model zamijeni stari, prati degradaciju). `local-inference` flag.

**Ugovor/RED:** RED (naš): scout nađe novi model koji stane → REPORT (ne auto-swap); model koji NE
stane na hardver → izostavljen iz reporta (HardwareFit filtrira, ne predloži OOM-kandidat); nema
produkcijske zamjene bez eksplicitnog odobrenja. **Verifikacija:** llmfit (Rust, AlexsJones, aktivan,
instaliran); Ollama-library/HF (izvori). **Placement:** dopuna 2.4, ide u hard-questions iteraciju.

---

# ADDENDUM A3 — IDENTITET (korekcija) + SELF-DIAGNOSTIC→CLAUDE-HANDOFF (korisnički zahtjev)

## A3.1 Identitet Nexusa — KOREKCIJA (mijenja PRD framing)
**Nexus JE prvenstveno UNIVERZALNI OSOBNI MULTIPLATFORM ASISTENT** (kao Hermes po namjeni), a
**"bolje kodiranje" je JEDNA GRANA znanja/profil, NE identitet.** Ranija formulacija "coding-harness
prvorazredni cilj" se PRECIZIRA: coding-diferencijatori (S3/S4/S5 — evidence-graded loop, TIA,
edit-ladder, itd.) ostaju valjani i jaki, ali kao `coding` PROFIL/grana, ne svrha harnessa.
Svrha = osobni asistent na svim platformama (S14 channels: TG/WA/Slack/... = prvorazredno lice),
a coding/RAG/memorija/multimodal su grane znanja koje se aktiviraju po potrebi.
**Zašto novi Nexus a ne Hermes:** korisnik NE može mijenjati/petljati po Hermes kodu (nije njegov
za slobodnu izmjenu); Nexus = njegov VLASTITI, editable, koji uči iz upotrebe.

## A3.2 Self-diagnostic → Claude repair-handoff (hardkodirano; jača 15.5 + 17.4)
**Zahtjev:** Nexus mora vidjeti KAD nešto s NJIM nije u redu → imati log → moći poslati MENI (Claude)
na popravak/nadogradnju. Petlja samoodržavanja s čovjekom+Claudeom u petlji (NE autonomni self-fix).
```go
type SelfDiagnostic struct{ health *SemanticHealth; journal *Journal } // JEZGRA
// 15.5 detektira: recidivism/correction-rate/verified-success-pad/fallback-drift/swallow-counter/
// STAGNATION/NO_PROGRESS/crash/nevaljan-invariant
func (s *SelfDiagnostic) OnAnomaly(a Anomaly) RepairBundle
// → scrubban reproducibilan bundle (17.4): verzije + korelacijski-ID + relevantni journal isječak
// + minimalni repro + koja invarijanta/RED je pukao — SVE redigrano (secrets/PII van)
```
**Tok (teren):** anomalija → SelfDiagnostic gradi RepairBundle → korisnik vidi (dashboard/kanal) →
**opt-in šalje Claudeu** (issue/bundle) → Claude popravi u repou → `nexus upgrade` (ff-only, trusted-
origin). NEXUS na terenu NE dira vlastiti source (zlatno pravilo iz NEXUS loze) — popravak ide kroz
mene i git, ne autonomno. Ovo je 15.5 (detekcija) + 17.4 (bundle→issue→upgrade) + eksplicitni
"repair-handoff-to-Claude" korak koji korisnik traži.
**RED:** anomalija → RepairBundle sadrži uzrok + repro + redigrane podatke (0 secrets/PII); bundle se
NE šalje bez opt-ina; nexus NE mijenja vlastiti kod autonomno.

---

# ADDENDUM A4 — HERMES AUDIT gap-analiza (osobni-asistent rupe koje plan NIJE imao)

Puni Hermes audit (3 agenta, ~/.hermes/hermes-agent) — konsenzus: **~80% preklapanje na osi
"osobni asistent"**; naš plan je bio coding-nagnut pa MU FALE asistentski obrasci koje Hermes ima.
Kako je identitet sad "asistent prvo" (A3.1), foldam materijalne rupe:

**G1 — OS-wide computer-use (MATERIJALNO, NOVO):** asistent bez rada u lokalnim DESKTOP aplikacijama
ostaje browser/terminal-only. Nije samo `vision` (13.1) — input-injection (klik/tipkanje) je
side-effect s DRUGAČIJIM approvalom/replayom/focusom/OS-permissionima. → nova podsekcija u S4/S13,
`computer-use` flag; approval kroz S6.0.Decide→6.9 + P1.4 (ireverzibilni GUI side-effect). Obrazac-ideja (NE kod): Hermes
computer_use obrazac (ne kod — Go rebuild).

**G2 — Izolirani osobni PROFIL (MATERIJALNO):** "posao"/"privatno"/"obitelj" NE smiju dijeliti
memoriju/tajne/channel-route/cache/automation/skill-state. Annex tenant/cache granice pomažu, ali
korisnički profil-LIFECYCLE nije definiran. → jača 6.6/6.7 + 9.2: `PersonalProfile` kao izolacijska
jedinica (kao qm/Durable-Objects pattern iz obsidiana — memory+secrets+channels+budget zajedno).

**G3 — Voice kao RAZGOVORNI runtime (MATERIJALNO):** 13.3 je STT/TTS adapter, ali fali barge-in/
echo-cancel/wake-owner/client-vs-host-capture/partial-audio/consent-recording-indicator/cancel. →
jača 13.3 punim voice-conversation ugovorom.

**G4 — Trajni korisnički CILJ/obveza (MATERIJALNO, NOVO):** asistent mora pamtiti "dovrši X"
ODVOJENO od razgovorne memorije (9.2) i infra-queue-taska (7.2). Bez toga cilj postaje prompt-tekst
ili cron bez završnosti. → nova podsekcija: `ObligationStore` (cilj s eksplicitnom done-definicijom,
persistira preko sesija, ne cron). Veže 9.1+3.6.

**G5 — Osobno-asistentski control-plane nad automation (MATERIJALNO):** 3.6 ima trigger-admission,
ali fali korisničko lice ("podsjeti me", "svaki ponedjeljak", "kad stigne mail od X") → jača 3.6 +
14.5 kao asistentski control-plane, ne samo cron-mehanizam.

**G6 — prompt-cache stabilnost kroz session-lineage (SLABIJE):** 8.4 cache-key nema session-lineage
dimenziju → dopuna 8.4. **G7 — user learning-control-plane (SLABIJE):** 9.3/16.4 uče, ali korisnik
ne vidi/upravlja ("zapamti da volim X", "zaboravi to") → jača 9.3 korisničkim learning-kontrolama.
**G8 — provider account-fleet (SLABIJE):** između 2.1-auth/2.2-fallback/2.5-cost fali "fleet" više
računa istog providera (rotacija, per-account budžet) → dopuna 2.2 KeyPool→AccountFleet. **G9 —
channel surface-capability snapshot (SLABIJE):** 14.5 treba bogatiji "što ovaj kanal podržava"
(markdown/slike/gumbi/thread) → dopuna 14.5.

**G10 pet/presence = nizak prioritet** (skill/persona, ne kernel). **G11:** Hermes NIJE automatski
bolji na A1/A2/A3 (OCR/model-scout/self-repair) — te su naše, ostaju.

**Placement:** G1-G5 = materijalne, ulaze u plan (nove podsekcije/jače postojeće); G6-G9 = dopune;
sve ide u hard-questions iteraciju. Ovo je najvrjedniji output Hermes audita — plan je bio coding-
nagnut, sad dobiva asistentsku dubinu koju Hermes dokazano ima.

---

# ADDENDUM A5 — OpenClaw FULL-CODE audit (dokaz da README-razina daje LAŽNE tvrdnje)

Prvi full-code audit vanjskog harnessa (OpenClaw, 561MB lokalni klon, 25k fajlova). Rezultat MIJENJA
povjerenje u README-razinu ostatka plana:

## KRITIČNA KOREKCIJA (O3): "stuck-detection" NIJE naš diferencijator
Plan (S3.4) je tvrdio: evidence-graded + stuck-detection "nitko od Kilo/OpenHands/OpenCode nema".
**NETOČNO** — OpenClaw IMA `tool-loop-detection.ts` (786 linija) + `tool-loop-no-progress.ts` +
`tool-loop-admission.ts` + `tool-loop-argument-churn.ts`. Naša tvrdnja je bila iz README/reputacije,
ne iz koda. **Ispravak: stuck-detection ostaje NAŠ zahtjev, ali NIJE unikatan — OpenClaw je referenca,
ne meta-koju-pobjeđujemo.** (Evidence-graded git-diff-checker tek treba provjeriti protiv njihovog koda.)

## GAP-ovi (O1-O14) — što OpenClaw ima a plan NEMA:
- **O1** harness-registry kao pluggable plugin-vlasništvo (`harness/registry.ts`)
- **O2** subagent registry + **sweeper + fencing + orphan-recovery + liveness** (~100 fajlova) — naš
 12.1 ima worktree-per-subagent ali NE sweeper/fencing/announce-loop
- **O3** tool-loop-detection (vidi korekciju gore)
- **O4** compaction-depth: planning-projection + failure-proof (naš 8.2 nema)
- **O5** model live-turn-probes + sticky-selection + failover-cooldown + catalog-browse (naš 2.x plići)
- **O6** exec-**auto-reviewer** (pregleda exec NAKON) + host-node-phases (naš 6.1/6.9 nema auto-reviewer)
- **O7** identity dubina: per-channel-prefix + human-delay + avatar + incognito (naš 11.1 plići)
- **O8** commitments (agent se OBVEŽE na posao — "promised-work") — koncept ne postoji u planu
- **O9** flows (58 fajlova workflow) — naš 12.2 DAG plići
- **O10** boards + tasks (kanban+task-mgmt, 87+14) — task/board površina nema u planu
- **O11** fleet + claws-lifecycle + **device-pairing** — naš 18.x nema fleet ni pairing
- **O12** state snapshot · **O13** meeting-bot + realtime-transcription · **O14** tool-call-repair paket

## ŠTO OVO ZNAČI (iskreno):
1. README/git-tree razina je proizvela BAREM jednu lažnu "diferencijator" tvrdnju (O3). Ostale
 ("evidence-graded nitko nema", "TIA nitko nema", "symedit nitko nema") su SUMNJIVE dok ih ne
 provjerimo protiv STVARNOG koda konkurenata.
2. OpenClaw sam nosi 14 gapova — a to je JEDAN harness. Full Tier-A landscape audit (25 harnessa)
 je NUŽAN, ne opcionalan — inače gradimo na neprovjerenim tvrdnjama.
3. Gapovi O2/O8/O9/O10/O11 su asistentska+orkestracijska dubina koju plan nema (kandidati za
 nove podsekcije nakon Tier-A tablice).

**Placement:** ovo je PRVI Tier-A audit. Ostali (OpenHands/OpenCode/Codex/Aider/...) tek slijede →
tek nakon svih Tier-A + who-has-what tablice smiju se "diferencijator" i "top-3" tvrdnje smatrati dokazane.

---

# ADDENDUM A6 — CODE-VERIFIED diferencijatori (11 full-code audita: 3 PALA, 3 PREŽIVJELA)

Full-code audit 11 harnessa (Hermes, OpenClaw, OpenHands, Aider, OpenCode, Codex, Cline, Continue,
Roo, Goose, Kilo) — PROVJERA naših "nitko nema" tvrdnji protiv STVARNOG koda. Rezultat ISPRAVLJA plan.

## OBORENE (README-plan je bio KRIV — 3):
- **stuck-detection** → OBORENO DEFINITIVNO. Cline `runtime/safety/loop-detection.ts:66-153`
 (`toolCallSignature`+`consecutiveIdenticalCount`+soft/hard threshold = naš STAGNATION 1:1);
 OpenClaw `tool-loop-detection.ts` (786 lin); Roo `processor.ts:29,356`; Codex `tool_inspection.rs`
 (djelomično). **NIJE naš diferencijator.**
- **barbell plan/execute** → OBORENO. Aider `architect_coder.py` (ArchitectCoder→editor s
 `editor_model`); Continue role-separation. **NIJE unikatan.**
- **edit-ladder exact→fuzzy→refuse** → NIJE NOVEL. Aider (`search_replace.py:62`+`udiff_coder.py:270`),
 OpenCode 9-stupanjski replacer. Delta = SAMO AST-symedit tier + fail-closed-na-više-kandidata.

## PREŽIVJELE (stvarni diferencijatori nakon 11 audita — 3):
- **TIA (coverage×diff→pogođeni testovi)** — NITKO nema. Aider `base_coder.py:1616` pokreće PUN
 test_cmd; Continue/OpenCode/Codex/Cline/Roo grep "coverage/affected" = samo ignore-liste. **STOJI.**
- **evidence-git-diff+exit-code completion GATE** (programski checker≠worker, ne prompt-tekst) —
 NITKO nema pravi. Continue `review.ts:350` pass=postoji-patch (ne exit-code); OpenCode verifikacija
 = samo tekst u promptu; Cline `attempt_completion` nema odvojenog checkera. **STOJI (s nijansom).**
- **AST/LSP symedit** (symbol-scoped rename, ne text-match) — NITKO nema ZA EDIT. Svi koriste
 tree-sitter za repomap/lint, NE za edit (Continue `deterministic.ts` = AST-assisted PLACEMENT, ne
 symedit; Aider tree-sitter samo u repomap). **STOJI.**

## POSLJEDICA ZA PLAN:
1. S3.4/S3.1 tekst se ISPRAVLJA: makni "stuck-detection nitko nema" i "barbell nitko nema" — to su
 sad NAŠI ZAHTJEVI (dobri), ali s referencama (Cline/OpenClaw/Aider), NE unikati.
2. Naš iskren coding-USP se svodi na TROJKU: **TIA + evidence-completion-gate + AST-symedit** — to
 NITKO od 9 coding-harnessa nema. Uz to: kombinacija svega + Go-single-binary + hardened membrana.
3. **Metodološka pouka (za korisnika):** README/git-tree razina proizvela je 3 lažne "unikat"
 tvrdnje od 6. Full-code audit je JEDINI pošten put. Preostali Tier-A (frameworks + landscape) tek
 slijede — ali coding-tvrdnje su sad code-provjerene.

---

# ADDENDUM A7 — landscape nastavak (jcode, ruflo) — trojka i dalje stoji

**jcode** (Rust, 27.8MB RAM-efikasan coding harness): NE obara našu trojku (nema TIA/evidence-gate/
symedit; loop zreo ali "pliće na coding gateovima"). Naša S6 sigurnost DUBLJA (jcode classifier sam
priznaje "nije sandbox", nema Landlock/egress). **Uzeti obrasce:** RAM-lifecycle (Arc-shared immutable
messages, static/dynamic prompt-cache split, drop-dup-provider-transcript, single-flight route-memo) —
meta za naš Go binary; account-candidate + cost-model (curated→OpenRouter→models.dev, unknown≠free) — feed za A4-G8 AccountFleet (classify-only; failover-owner=S7). MCP child ne nasljeđuje credential env (least-authority — dobar detalj).

**ruflo** (Rust meta-harness, 66k★): NIJE stub ali MALEN (~2000 lin swarm/authz + watermarking) i
**marketing-težak** (5× više doc-bajtova nego koda — "star=warning" djelomično opravdan). Stvaran samo
u security/provenance niši (CASA envelope + watermarking crate); coding/memorija/dubina TANKI. NE obara
trojku. **Uzeti:** watermarking/provenance obrazac (minorno). Potvrđuje A6 lekciju: reputacija laže.

**Stanje trojke nakon 14 full-code audita** (Hermes/OpenClaw/OpenHands/Aider/OpenCode/Codex/Cline/
Continue/Roo/Goose/Kilo/jcode/ruflo + Hermes): **TIA, evidence-git-diff-gate, AST-symedit — NITKO nema.
Trojka STOJI.** (Frameworks tier + landscape još slijede za S9/S12 who-has-what.)

---

# ADDENDUM A8 — FINALNI code-grounded ispravak (29 harnessa auditano; master u MASTER-LANDSCAPE.md)

29 full-code audita gotovo (svi Tier-A + Hermes/OpenClaw). Master who-has-what: `docs/MASTER-LANDSCAPE.md`.
Ovo je KONAČNA iskrena slika koja zamjenjuje README-tvrdnje.

## NAŠ STVARNI USP — 4 diferencijatora (NITKO od 29 nema, code-provjereno):
1. **Evidence-graded diff+exit-code completion GATE** (checker≠worker, ne prompt-tekst) — svi 29 NEMA
2. **TIA** (coverage×diff→pogođeni testovi) — svi 29 NEMA
3. **AST/LSP symedit** (symbol-scoped edit, ne text-match) — svi 29 NEMA
4. **Formal MEMORY_FORGET vs DATA_PURGE** (P2.1 reverzibilno vs ireverzibilno) — svi 29 NEMA
+ KOMBINACIJA: sve u JEDNOM hardened Go single-binary osobni-asistent+coding harnessu — nijedan
harness ima SVE; svaki ima komadiće.

## OBORENO DEFINITIVNO (bilo lažno u planu; sad referencirano):
stuck-detection (Cline/Codex/Roo/Kilo), edit-ladder (OpenCode 9-st./Aider), shadow-git (OpenClaw/Kilo/
DeepSeek), OS-sandbox (Codex Seatbelt), queue-fencing (OpenClaw ingress-queue/DeepSeek cron-fence).
Sve to OSTAJE naš zahtjev, ali s REFERENCAMA (uči od njih), NE kao "nitko nema".

## GAPOVI ZA FOLD (iz 29 audita — što uzeti u plan/gradnju):
- **LangGraph BSP/Pregel superstep** (deterministički multi-agent + atomski reduce+checkpoint) → S12.2
- **OpenClaw ingress-queue** (claim/refresh/stale-CAS-reclaim/DLQ = pravi lease, dublje od NEXUS) → S7.2 referenca
- **OpenClaw O1-14** (commitments/flows/boards/fleet+pairing/exec-auto-reviewer) → asistentske podsekcije
- **DeepSeek-Harness Cordis** (everything-is-plugin DI) → S17.1 obrazac (potvrditi iz A5-nasljeđa)
- **jcode RAM-lifecycle** (Arc-shared/static-dynamic-cache-split/drop-dup) → Go single-binary efikasnost
- **jcode cost-model + account-candidate** (curated→OpenRouter→models.dev) → A4-G8 AccountFleet (classify-only; S7 failover-owner)
- **MAF nativni A2A+MCP hosting** (potvrđeno; MAF `go/` je STUB — nismo u Go-konkurenciji) → S12.5
- **Haystack RAG pipeline dubina** (BM25+dense+rerank komponente) → S10 adapteri
- **CrewAI/AutoGen/MetaGPT team-role obrasci** → S12.1 (role-as-data već imamo, potvrđeno)

## METODOLOŠKA POUKA (za korisnika — dokazano):
README/git-tree/reputacija razina proizvela je LAŽNE "unikat" tvrdnje (stuck/barbell/edit-ladder/
shadow-git). SAMO full-code audit (29 harnessa) dao je iskrenu sliku: 4 stvarna diferencijatora,
ne 6. Ovo je bio ispravan pristup od početka (korisnikova vizija). Plan je sad code-grounded.

**SLJEDEĆE:** plan je iskren i potpun → PRD.md (korisnik vodi) + ARCHITECTURE-ESSENTIALS → hard-
questions review → CLAUDE/AGENTS + scaffold → gradnja. USP je sad 4 code-dokazana + kombinacija.

---

# ADDENDUM A9 — FOLD A8 gapova u sekcije + KOMPONENTNA strategija (RAG/OCR/vector/baza/memorija)

## Fold gapova iz 29 audita u dizajn sekcija:
- **S12.2** ← LangGraph BSP/Pregel superstep obrazac: kanali nepromjenjivi unutar koraka, taskovi vide
 update tek idući korak, atomski reduce+checkpoint na kraju superstepa. NAŠ DAG dobiva deterministički
 superstep-mode (uz zadržan typed S0 envelope — LangGraph ima `Any`, mi typed).
- **S7.2** ← OpenClaw `ingress-queue` (claim/refresh/stale-CAS-reclaim/DLQ) je REFERENCA dublja od
 NEXUS queue — naš lease/fencing (P2.3) uzima taj obrazac (DLQ + refresh eksplicitno).
- **S12.6 / nove asistentske podsekcije** ← OpenClaw O1-14: **commitments** (promised-work, veže G4
 ObligationStore), **flows** (workflow), **boards/tasks** (kanban), **fleet+device-pairing** (S18),
 **exec-auto-reviewer** (exec pregledan NAKON — jača 6.9). Ovi ulaze kao `multi-agent`/`service`
 addendum-podsekcije u gradnji.
- **S17.1** ← DeepSeek-Harness Cordis "everything-is-plugin" DI-kernel obrazac (potvrđen kandidat).
- **Go binary efikasnost** ← jcode RAM-lifecycle (Arc-shared→Go pointeri na immutable, static/dynamic
 prompt-cache split, drop-dup-buffer) — cilj sličan 27.8MB.
- **2.2 AccountFleet** ← jcode cost-model + account-candidate (classify-only; S7 jedini izdaje grant/failover).
- **S12.5** ← MAF nativni A2A+MCP hosting (potvrđeno; MAF `go/` je STUB — nismo u Go-konkurenciji).
- **S10** ← Haystack BM25+dense+rerank komponentna dubina (adapter-referenca).

## KOMPONENTNA STRATEGIJA (RAG/OCR/vector/baza/memorija) — ZAŠTO nisu full-audited:
Ove su ADAPTERI iza sučelja (adaptiramo, ne kopiramo kod) (greenfield) — NE trebaju 300 full-code
audita, nego laganu capability-potvrdu PO ADAPTERU u trenutku gradnje te sekcije:
- **MEMORIJA (S9):** greenfield (Decay+Audn P0; Dream/MemGit/MemAssoc v2; letta/mem0/zep referenca) →
 Go port. letta/mem0/zep = referenca. NAJDUBLJI naš dio.
- **BAZA:** `modernc.org/sqlite` pure-Go spine (memorija/journal/queue/FTS5), bez cgo → single-binary.
 Postgres samo za `service`. (S9.1 + S0.4)
- **VECTOR (S10.3):** ADAPTER iza `Retriever` — pick (Qdrant/LanceDB/sqlite-vec) capability-confirm pri gradnji.
- **RAG (S10):** adapteri (chunker/embedder/reranker) iza sučelja; Haystack/LlamaIndex kao referenca.
- **OCR (A1):** hardware-routing Unlimited-OCR(GPU)/OCRmyPDF(CPU) → searchable-PDF + FTS. Gotov dizajn.
Pravilo: KOMPONENTE se biraju laganom matricom (URL/licenca/radi-li-X) KAD gradimo tu sekciju, ne unaprijed.

**PLAN JE SAD POTPUN I CODE-GROUNDED.** Sljedeće: PRD (korisnik) + Essentials → hard-questions review
→ CLAUDE/AGENTS + scaffold → gradnja P0. Komponente (vector/OCR backend) biraju se u gradnji sekcije.

---

# ADDENDUM A10 — GAPOVI RIJEŠENI (buildable Go dizajn; puni u docs/GAPFIX-*.md)

Svi gapovi iz 29-harness audita sad imaju BUILDABLE Go dizajn (struct/interface skice +
RED + paket-placement) u `docs/GAPFIX-{codex,kilo,agy}.md`. Index + sekcija-placement:

## Orkestracija (GAPFIX-codex) — paketi `internal/orchestration/{graph,superstep,flow,board}`, `internal/fleet/{registry,placement,pairing,transport}`
- **12.2 BSP/Pregel superstep-mode** — `EdgeSequential` default / `BSPSuperstep` kad paralelni nodeovi
 dijele reducirano stanje; kanali immutable u koraku, atomski reduce+checkpoint (S7.3), dijeli
 validator+S7-grantove+journal s našim typed S0 envelope (ne LangGraph `Any`). RED gate.
- **7.2 ingress-queue** ostaje JEDINI lease-owner + DLQ + refresh/reclaim + fencing (OpenClaw-obrazac).
- **12.x flows + boards** — workflow + kanban kao nove `multi-agent` podsekcije; board/fleet tablice transakcijske (journal owner).
- **18.x fleet + device-pairing** — `internal/fleet` identitet uređaja/nodea; fleet NIJE drugi retry-owner.

## Asistent (GAPFIX-kilo) — Hermes G1-G5 + OpenClaw
- **G1 computer-use** → S4.5+13.1+6.9/6.1+P1.4: `InputInject` KEY/MOUSE/TYPE=RED-tier, SCREENSHOT=YELLOW/
 CONFIDENTIAL; bez EffectIntent-approval→S6.0 odbij; keystroke u nepoznato polje→approval-po-tipu (P1.4). RED.
- **G2 PersonalProfile** (posao/privatno/obitelj) → `ProfileRegistry` deny-default izolacija (memory/
 secrets/channels/cache); write u `work`→query u `family`=0 hitova. RED.
- **G3 voice-runtime** → S13.3+S7+6.7: barge-in/wake/consent; audio prije consent→odbij. Obrazac-ideja (NE kod): Hermes voice.
- **G4 ObligationStore** → SQLite spine + 3.6 trigger; `MarkDone` SAMO uz Evidence (done-def git-diff/exit). RED.
- **G5 control-plane** + **exec-auto-reviewer** (exec pregledan NAKON) + **identity-dubina** (per-channel-prefix/human-delay).

## Efikasnost/Provider/Plugin (GAPFIX-agy)
- **jcode RAM-lifecycle** → `ImmutableMessageList` (Go ekvivalent Arc), static/dynamic prompt-cache
 split, single-flight route-resolver, drop-dup. RED: `go test -race` 50 gorutina; static-prefix SHA
 invariant; single-flight 100→1 izvršenje.
- **2.2 AccountFleet** → account-CLASSIFY (candidate+cooldownHint) + credential-cache-invalidate + tiered cost-model (failover-owner=S7)
 (curated→OpenRouter→models.dev, unknown≠free).
- **17.1 Cordis DI-kernel** → everything-is-plugin obrazac.

**SVI GAPOVI RIJEŠENI** (design-level). Puni Go dizajn: GAPFIX-*.md (commitani). Plan je sad
POTPUN i code-grounded. **SLJEDEĆE: PRD (korisnik) + Essentials → hard-questions → scaffold → kod.**

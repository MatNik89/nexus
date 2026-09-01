# NEXUS — HARNESS PLAN (hibrid-hibrida dizajn po podsekciji)

**Što je ovo:** buildable dizajn-plan. Za SVAKU podsekciju spec-a (S0–S18, 100 podsekcija)
prolazi se HIBRID-HIBRIDA sinteza nad njezinim top-3 kandidatima (merge-skill princip):
razloži svaki kandidat na aspekte → po aspektu uzmi najbolje iz sva tri → graftaj u JEDAN
sintetizirani dizajn → sudac (Pareto floor: sinteza ≥ najbolji kandidat na svakom aspektu) →
verifikacija (aspekt mora biti STVARAN, ne tvrdnja). Kad je plan gotov, kodiranje = implementacija.

**Jezik:** Go (3× AGREE). Jezgra `CGO_ENABLED=0` single-binary; `modernc.org/sqlite` pure-Go
spine; sandbox per-OS build-tagged (Linux Landlock+seccomp real, Win/Mac adapteri); TUI
bubbletea; desktop Wails/Fyne (odluka kasnije). GORTEX membrana = direktan Go reuse, ne port.
**Repo:** MatNik89/nexus (private). CI 3-OS na kraju.

**Format po podsekciji:**
- **Tip:** MEHANIZAM (mi pišemo) | ADAPTER (vanjski alat iza sučelja)
- **Aspekti** (razložak kandidata) + najbolji izvor po aspektu
- **Sinteza:** naš dizajn (za MEHANIZAM) ili sučelje + primarni pick (za ADAPTER)
- **Salvage:** što iz NEXUSv2 (Go reuse ili Python→Go port)
- **Ugovor/RED:** koji Annex A ugovor + RED test je gate
- **Verifikacija:** je li svaki graftani aspekt stvaran (URL/licenca/aktivnost)
- **Pareto floor:** potvrda da sinteza nije gora ni na jednom aspektu

**Status:** U IZRADI. Redoslijed: S0 → S18 (kernel-prvi po fazama P0-P6).

---

## Protokol (za agente)
Svaki agent za dodijeljenu sekciju: pročita spec podsekciju + top-3 + Annex ugovor + NEXUS
salvage kandidat. Napravi hibrid-sintezu po gornjem formatu u `PLAN-<sekcija>-<ime>.md`.
Moderator spaja u ovaj dokument uz Pareto-floor sudca (odvojen od autora — barbell).
Verifikacija aspekata: WebSearch/WebFetch za sporne tvrdnje; NEXUS kod za salvage.

---

<!-- Sinteze se dodaju ispod, sekcija po sekcija -->

---

## DIREKTIVA: `coding` profil = PRVORAZREDNI CILJ (bolji od Kilo Code / OpenHands / OpenCode)

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
- **Evidence-graded loop** (loop_engine): checker ocjenjuje git-diff + realan exit-code, NE prozu
  workera (anti-sycophancy strukturno) → 3.1 + 16.6. Nijedan od tri konkurenta to nema.
- **TIA** (3.5): coverage × git-diff → samo pogođeni testovi (sekunde umjesto minuta).
- **app-contract** (16.1): deliverable-level acceptance (ekrani/tokovi/persistence), ne test-count.
- **Skill-forge** (11.5) + **credit/reward ledger** (16.4): harness uči iz coding-iskustva, greške se ne ponavljaju.
- **Reversa** (11.2): reverse-engineering legacy repoa → spec+migracijski plan prije mutacije.

**Organizacija:** kad hibrid-sinteza dođe do coding-sekcija (3.x, 4.2-4.4, 5.x, 8.3), radi se
KAO ZASEBAN produbljeni blok — svaka od te tri meta (Kilo/OpenHands/OpenCode) razložena na
coding-aspekte, hibrid mora na SVAKOM aspektu biti ≥ najbolji od njih + naši diferencijatori nadgradnja.

## PROVIDER MEHANIZAM (salvage-osnova za 2.1/2.2/2.6)

NEXUS spaja na modele DVA načina (llm/providers.py — potvrđeno čitanjem):
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
konstruktoru. **Salvage:** `core/events.py` Python→Go port; GORTEX tool-response schema Go reuse.
**RED:** P0.1 `test_s0_rejects_provenance_laundering`. **Pareto:** NADSKUP MCP+OpenAI-SDK+LangGraph
(dodan trust_class/sensitivity/lineage koje nijedan nema).

**0.2 state-machine (MEHANIZAM):** eksplicitna prijelazna TABLICA `map[State][]Transition{Event,
From,To,Guard}`, default-reject; state = REKONSTRUIRAN fold-om eventa iz journala (ne mutable
polje); checkpoint = journal offset. `UNKNOWN`→samo reconciliation event. **Salvage:**
`core/state.py`+`loop_engine` status, uz ISPRAVAK (NEXUS imao implicitne if/return prijelaze →
sad tablica). **RED:** P0.1 nelegalan prijelaz (RUNNING→ADMITTED, SUCCEEDED→RUNNING)→reject.
**Pareto:** tablica+guard (LangGraph) + event-derived (OpenHands) + UNKNOWN-reconcile (Temporal).

**0.3 capability negotiation (MEHANIZAM):** `CapabilityFloor{ToolCalling,StructuredOutput,
Streaming,ContextLimit}`; `Effective()=min(declared,measured)`; `Resolve` nepoznatog → error
(fail-closed, "mjeri ne pretpostavljaj"). **Salvage:** `llm/providers.py configure_json_output`
+ `core/capabilities.py`+`capreg.py` Python→Go. **HONEST GAP (kilo+moderator):** 0.3 JEDINA S0
podsekcija bez dedicated Annex RED — RJEŠENJE: dodati **P0.5 "capability-matrix fail-closed"** u
Tier-3 (matrix-prep) ILI vezati na capability-floor načelo kao gate. Odluka: dodati P0.5 ugovor.
**Pareto:** cap-as-data (LiteLLM)+declared-on-adapter (PydanticAI)+cap-routing (Portkey)+fail-closed (naš).

**0.4 verzioniranje/migracija (MEHANIZAM):** `SchemaRegistry{byID map[string]map[int]Upcaster}`,
monoton lossless upcast lanac, nepoznato→QUARANTINE (ne tiho-odbij, ne downcast), immutable raw
event čuvan, `MigrationRecord{From,To,ID,InputHash,OutputHash}`. **Salvage:** `core/store.py
SCHEMA_VERSION+_migrate` + `modernc.org/sqlite` durable. **RED:** P0.1 migracija-MUST + provenance-
downgrade kroz upcaster. **Pareto:** registry+upcast (Temporal)+envelope-verzija (CloudEvents)+
karantena (naš, nijedan od 3 ne karantenira).

**0.5 capability-flag closure resolver (MEHANIZAM, nova jezgra):** `CapabilityManifest{provides/
requires/conflicts/gates/trigger/config_hash/signature}` + `GateAttestation`; `Resolver.Resolve`:
tranzitivni closure → cycle/conflict/unknown-version→REJECTED → topološka aktivacija → svi gate-
att nad ISTIM config_hash → atomski aktivni set (parcijalno ACTIVE nelegalno). Bundle-alias NIJE
capability (ne forsira flag bez triggera). **Salvage:** `core/capabilities.py`+`capreg.py` port +
NADOGRADNJA (NEXUS nema formalni closure+conflicts+atomsku aktivaciju). **RED:** P0.4
`test_every_flag_fails_when_one_gate_is_ablated` (18 flagova). **Pareto:** closure/cycle (Cargo)+
cap-matching (OSGi)+topološko (Bazel)+gate-attestation (naš).

**S0 verifikacija:** svi kandidati stvarni (MCP Apache-2.0, OpenAI-SDK/LangGraph/LiteLLM/PydanticAI/
OpenHands/Temporal MIT, CloudEvents CNCF, Cargo/Bazel/OSGi) — licence+aktivnost potvrđeni.
**S0 STATUS: ZAOKRUŽEN** (uz akciju: dodati P0.5 ugovor za 0.3 u Annex).

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
stara generacija ostaje. **Salvage:** `core/paths.py config_files` + `cli.py _load_cfg` port
(ujedinjuje env/CLI koje NEXUS nije imao u istom resolveru). **RED:** config proširi kernel max
(egress `*` / sandbox ispod floora)→reject. **Caveat (codex):** fsnotify je +ovisnost; upgrade-gate
= ako mjerenje pokaže da eksplicitni reload zadovoljava, briše se bez promjene ugovora. **Pareto PASS.**

**1.2 cross-platform paths+procesi (MEHANIZAM, cross-platform JEZGRA):** putanje preko stdlib
`os.UserConfigDir/CacheDir` (bez build-tagova). Procesi build-tagged: `proc_unix.go`
(`SysProcAttr{Setpgid:true}`, killpg `-pgid` SIGTERM→GracePeriod→SIGKILL→Wait; NE Pdeathsig — smrt
Go threada ≠ smrt procesa, lease je kanonski recovery), `proc_windows.go`
(`CREATE_NEW_PROCESS_GROUP` + Job Object `KILL_ON_JOB_CLOSE`; taskkill `/T /F` preko
`GetSystemDirectory` NE PATH — bounded fallback, NIJE ownership dokaz), `proc_darwin.go`=killpg.
PID+**start-token** identitet (P1.2 orphan-sweep; PID sam nije dokaz). PTY: `creack/pty` pure-Go,
build-tagged, fail `PTY_UNAVAILABLE` (nikad tihi shell fallback). **Salvage:** `core/paths.py` +
`core/subproc.py _kill_tree` port; NE portirati rlimits (POSIX best-effort → pripada S6). **RED
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
journal nije otvoren, pa se nepovratno zatvori. **Salvage:** `core/trace.py`+`core/otel.py` port;
journal = S0 reuse. **RED (P0.3):** `test_projection_cannot_bypass_journal` + redaction-before-
projection, replay-keeps-otel-ids, idempotent-projection, failed-export-doesnt-advance-cursor,
concurrent-spans-keep-parents, secret-never-reaches-sink. **Caveat:** append svakog debug eventa
može opteretiti journal → filter PRIJE payload konstrukcije, ali sampling/drop je POLICY event i
NIKAD ne obuhvaća state/security/audit klase; throughput floor MJERI se (proof ceiling do benchmarka).
**Pareto PASS.**

**S1 honest gap:** 1.1 i 1.2 nemaju dedicirani Annex RED (kao 0.3) — gate posredno kroz P1.2/P0.4 +
kod-vs-data načelo. Akcija: dodati **P0.6 "config-bounds + process-identity"** u matrix-prep (uz
P0.5 iz S0). **Verifikacija:** svi kandidati stvarni (Codex/Aider/Continue/goose Apache-2.0; OTel
CNCF; structlog/tracing/OpenHands MIT). **S1 STATUS: ZAOKRUŽEN.**

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
**Salvage:** `llm/providers.py` (CLI subprocess+API+KeyPool+CostTracker) port. **RED:** (d) bez
acknowledged_tos→error; (c) bez `--tools ""`→odbij. **Pareto:** LiteLLM+PydanticAI+Vercel-AI-SDK +
3-auth-moda (nijedan od 3 nema CLI-agent ni OAuth-device auth).

**2.2 fallback (MEHANIZAM, TANKI — NE retry petlja):** `FallbackPlanner.Plan(err,cur)→
(Target, s7.FallbackProposal)` — vraća PRIJEDLOG, puni `s7.ExecutionPolicy.fallback_targets[]` +
mapira provider-error→kategorični kod; retry je S7. Cooldown/KeyPool rotacija. **Salvage:**
`router.py FallbackModel` + `_KeyPool`. **RED P0.2:** `test_adapter_cannot_self_retry` (2× isti
AttemptGrant→`ATTEMPT_NOT_AUTHORIZED`, counter=1). **Pareto:** LiteLLM+Portkey+Kong + no-self-retry.

**2.3 structured output (MEHANIZAM):** `StructuredOutput[T].Validate→re-ask→salvage`, NIKAD tiho
prihvati nevalidan T. Tolerant parser (strip fences/trailing comma). Constrained-decoding (Outlines)
= OPT-IN adapter za local (2.4), ne jezgra za HTTP. **Salvage:** `executor.py` SYSTEM protokol +
`spawner.py _salvage_json`. **RED:** malformiran JSON→salvage/re-ask, silent-accept zabranjen.
**Pareto:** Instructor+Outlines+BAML.

**2.4 lokalni + hardware-fit (ADAPTER+MEHANIZAM):** `LocalProvider{Kind:ollama|llama_cpp|vllm}`
OpenAI-kompatibilan HTTP; `HardwareFit.Fits(spec,quant)` preflight MJERI RAM/VRAM/AVX/CUDA na hostu
→ ne stane: odbij+predloži kvantizaciju (ne OOM). `Capabilities()`=Measured. **Salvage:**
`providers.py LocalProvider`. **RED:** model>RAM/VRAM→Fits=false+prijedlog. **Pareto:** Ollama+
llama.cpp+vLLM+hardware-fit (obsidian).

**2.5 cost tracking (MEHANIZAM):** `CostTracker.Record(model,usage)` price-table, real usage ili
len//4 estimate (označen), MONOTON cumulative spend→S7 circuit-breaker (P2.2 cost_minor_units);
feed-a 16.4 verified-credit. **Salvage:** `providers.py CostTracker`+`circuit.py add_cost`. **RED:**
spend>ceiling→fence (ne samo log). **Pareto:** LiteLLM+Aider+Helicone+budget-ceiling.

**2.6 model routing (MEHANIZAM, jezgra owner):** `Router` ABI; `BarbellRouter` (plan/execute/verify
po fazi) = jezgra; `LearnedRouter{bandit}` epsilon-greedy OPT-IN, reward SAMO iz 16.4 verificiranog
(nikad self-report). Route fail-closed: kandidat ispod capability floora (S0.3) odbijen. **Salvage:**
`router.py`+`reward.py`. **RED:** Route na model ispod floora→error. **Pareto:** RouteLLM+
semantic-router+LiteLLM+barbell+verified-bandit.

**S2 honest gap:** 2.1/2.3/2.4/2.5 bez dediciranog Annex RED — gate posredno (P2.5 za 2.1, S0.1 za
2.3, hardware-fit za 2.4, P2.2 za 2.5). Matrix-prep kandidati: P0.7 structured-never-silent,
P0.8 hardware-fit-fail-closed. **Verifikacija:** svi kandidati stvarni (Kong webfetch-verificiran).
**S2 STATUS: ZAOKRUŽEN.** Auth-mod dizajn (a-d) odgovara korisnikovom OAuth/subscription pitanju.

---

# S3 — KANONSKA SINTEZA (Core Loop; PRVA coding-kritična, produbljeno; 3-way, Pareto PASS)

**Owner granica (P0.2, najvažnija S3 invarijanta):** S3 NIKAD ne retry-a — 3.4 classify-a→S7,
3.3 emitira user-cancel, svaki pokušaj nosi `s7.AttemptGrant`. Kilo Code = "to-beat" meta (nema
javnu licencu, ne salvage-izvor); OpenHands/OpenCode/SWE-agent = javni salvage-referenti.

**3.1 plan-act-observe (MEHANIZAM, coding-jezgra):** `Loop{planner,executor,verifier,checker,
state,journal}`: plan(adaptivan small/medium/large) → authorize(6.9) → execute(→Observation,
3.4) → verify(3.5 in-turn) → **GRADE(16.6 checker)** → re-plan s dokazom. **DIFERENCIJATOR
evidence-graded:** `Checker.Grade` ocjenjuje **git-diff + realan exit-code + determinističke
signale, NIKAD prozu workera** (anti-sycophancy strukturno) — NITKO od Kilo/OpenHands/OpenCode
nema. **Salvage:** `loop_engine.py` (majority-checker) + `runtime.py` (handle→loop→verify→learn) +
`executor.py _run_loop` — NAJVRJEDNIJI salvage u kernelu. **RED:** worker kaže "done" bez
diff/testa → Grade=FAIL, run ne u SUCCEEDED. **Pareto:** Action/Obs+event-stream (OpenHands) +
modularnost (Codex) + adaptivni planer (SWE-agent ACI) + evidence-graded (naš).

**3.2 streaming (MEHANIZAM):** `Chunk{Delta,Seq,Finish}` channel + ctx-cancel; stream=projekcija
journala. **Salvage:** `providers.py` (`stream:False` SLABO → POPRAVAK na pravi token-stream).
**RED:** cancel usred streama→channel zatvoren, turn CANCELLED. **Pareto:** Gemini+Codex+OpenHands+cancel-token.

**3.3 cancel/turn (MEHANIZAM):** `TurnController{Cancel/Pause(offset)/Resume(offset)}`; cancel=S7
token, pause/resume=journal offset (replay fold, ne mutable state); CANCELLED terminalan.
**Salvage:** `core/steer.py`+`core/circuit.py`. **RED P0.2:** cancel usred tool-poziva→prekid, nikakav side-effect. **Pareto:** goose+OpenHands+Codex+S7-token.

**3.4 error recovery (MEHANIZAM, coding-jezgra):** tool-pad → `TypedError` PAKIRAN u observation
(model vidi, korigira se — alat pao ≠ petlja pala); classify→retryable(S7)/terminal(re-plan).
**DIFERENCIJATOR stuck-detection** (`circuit.py`): STAGNATION (isti tool/arg N puta) + NO_PROGRESS
(verify ne zelena M puta) → breaker prije lažno-zdravog vrtenja. NITKO nema. **Salvage:**
`recovery.py`+`executor.py OBSERVATION`+`circuit.py`. **RED P0.2:** tool-pad→obs nosi error, loop
ne crasha; STAGNATION→breaker. **Pareto:** error-u-obs (OpenHands) + checkpoint (LangGraph) +
razumljiv-format (SWE-agent) + stuck-detection (naš).

**3.5 deterministička verifikacija + TIA (MEHANIZAM, coding-jezgra, `coding` flag):** in-turn
verify (pokreni→dijagnostika NATRAG U ISTOM TURNU→odbij završetak dok ne prođe). **DIFERENCIJATOR
TIA:** coverage/dependency-graf × git-diff → SAMO pogođeni testovi (sekunde umjesto punog suitea),
full-suite fallback kad stale. NITKO od 3 nema (vrte pun suite/linter). **Salvage:** `verifier.py`
(DORMANTAN→REVIVATI) + `tia.py` + `testcmd.py` + `executor.py _validate`. **RED:** edit s lint-
greškom→Diagnostic, korak ne prihvaćen, model dobiva u istom turnu; TIA pokreće samo pogođene.
Treći slot UNCLEAR (spec praznina — naša petlja je odgovor). **Pareto:** Aider+SWE-agent+TIA (naš).

**3.6 run-trigger-plane (MEHANIZAM admission=jezgra; triggeri=`automation`):** `Trigger` interface
(Manual/Schedule/Webhook/Watch/Heartbeat) → SVI kroz isti S0 envelope → dedup+idempotency+policy+
budget+audit PRIJE petlje (autonomni = iste granice kao ručni). `ScheduleManifest` s P2.6 clock/DST/
missed-run/overlap. **Salvage:** `schedule.py`+`watch.py`. **RED P2.6:** `test_zagreb_dst_fold_runs_
once_across_restart`. **Pareto:** NEXUS-admission+Temporal-DST+APScheduler.

**S3 = 3 CODING-DIFERENCIJATORA gdje POBJEĐUJEMO:** evidence-graded checker (3.1), stuck-detection
(3.4), TIA (3.5) — nijedan od Kilo/OpenHands/OpenCode nema. **Honest gap:** 3.2/3.3/3.5 bez
dediciranog Annex RED (P0.9 "verify-never-silent" kandidat za matrix-prep). **Verifikacija:** svi
javni kandidati stvarni; Kilo Code = to-beat meta bez javne licence. **S3 STATUS: ZAOKRUŽEN.**

---

# S4 — KANONSKA SINTEZA (Tool sustav; coding-kritično 4.2-4.4; 3-way, Pareto PASS)

**4.1 tool-registry (MEHANIZAM):** typed schema po alatu (`ToolSpec{ID,SchemaHash,EffectClass}`),
dispatch, timeout, result-envelope, permission-wrapper. **Salvage:** `core/tools.py`+`toolschema.py`.
**RED:** poziv bez validne sheme→reject. **Pareto:** FastMCP+smolagents+OpenAI-SDK.

**4.2 shell/exec (MEHANIZAM, coding):** bash-sesija koja drži stanje, rlimits, tree-kill.
**Salvage:** `core/subproc.py run_isolated` (rlimits/grupa-kill) port + **`gortex/tool_executor.go`
= Go REUSE** (već Go). **RED:** timeout→cijelo procesno stablo ubijeno (P1.2). **Pareto:** OpenHands+
Codex-sandbox+gptme + gortex-reuse.

**4.3 edit formati (MEHANIZAM, PRESUDNI coding-lever, PRODUBLJENO):** multi-format engine
(WHOLE/DIFF/UDIFF/SEARCH_REPLACE) + **izbor PO MODELU** (Aider benchmark: slab→SEARCH_REPLACE,
jak→DIFF) + **deterministički LADDER** `exact(editapply)→fuzzy(editrepair, više kandidata→REFUSE)→
AST/LSP symedit`. Fail-closed na ambiguous (nikad pogađaj). Atomic staged→verify(3.5)→commit,
dirty-tuđi-rad se ne gazi (5.3). **DIFERENCIJATOR:** symedit (AST-scoped rename/refactor) + refuse-
on-ambiguous — Aider/Cline/Codex nemaju. **Salvage:** `editapply.py`+`editrepair.py`+`symedit.py`
port; **`gortex/diff_engine.go` Go REUSE**. **RED:** 2 identična matcha→REFUSE; symedit rename ne
dira string/komentar (AST). **Pareto:** Aider(multi-format/per-model)+Cline(checkpoint)+Codex+ladder+symedit (naš).

**4.4 pretraga koda (MEHANIZAM+ADAPTER, coding, PRODUBLJENO):** slojevito text→AST→simbol→
semantika→arhitektura. `SearchStack{ripgrep, ast-grep, serena-LSP, codeindex, callgraph, archmap}`.
Svaki Hit datamarked-untrusted + source_uri. **DIFERENCIJATOR dev-inteligencija IZNAD pretrage:**
pre-indexirani simboli + call-graph + arhitektura-kao-queryable-graf (NEXUS) — konkurenti staju na
LSP+grep. **Salvage:** `codeindex.py`+`coderetrieval.py`+`callgraph.py`+`archmap.py`+`lsp.py` port.
**RED:** stale index→re-scan, ne krivi hit. **Pareto:** ripgrep+ast-grep+Serena+dev-intel (naš).

**4.5 web/browser (ADAPTER, `browser` flag):** browser-use+Playwright-MCP+Skyvern; Camoufox
runner-up SAMO uz ToS/robots policy jezgre. Egress-gated (S6.3). **Salvage:** `core/browse.py`
egress-guarded CDP. **RED:** browser bez egress-checka→blokiran.

**4.6 MCP klijent (MEHANIZAM+ADAPTER):** klijent + **P1.3 transport/auth ugovor** (local-stdio vs
remote-https, peer-identity pin, scoped OAuth, tool-list size-limit + injection-fencing, cancel=S7).
**Salvage:** `core/mcp.py`+`mcpauth.py` (OAuth) port; `gortex/mcp_server.go` Go reuse.
**RED P1.3:** `test_mcp_wrong_pinned_peer_gets_no_credentials`. **Pareto:** goose+Cline+Codex+auth-ugovor.

**4.7 tool-exposure-budget (MEHANIZAM, jezgra):** per-turn minimalni capability-scoped tool-view,
lazy schema-fetch, mjeri schema-token + selection-miss-rate (Pi princip: manje alata pobjeđuje).
**RED:** turn dobiva samo scoped set, ne cijeli registry. **Pareto:** pi-mono+MCP-discovery+ToolView (naš).

**S4 = 2 coding-diferencijatora:** edit-ladder+symedit (4.3), dev-inteligencija (4.4). gortex
diff/exec/mcp = Go REUSE (ne port). **Honest gap:** 4.1/4.2/4.4/4.7 bez dediciranog Annex RED
(posredno P1.1/P1.3/P2.2). **Verifikacija:** kandidati stvarni (Mentat izbačen — neaktivan).
**S4 STATUS: ZAOKRUŽEN.**

---

# S5 — KANONSKA SINTEZA (Workspace/VCS/checkpoint; coding sigurnosna mreža; 3-way, Pareto PASS)

Git: `go-git` (pure-Go, bez cgo) ili `exec git` fallback. Gate P1.2 (orphan/lock sweep).

**5.1 checkpoint/rollback (MEHANIZAM):** `ShadowGit` zaseban repo IZVAN radnog stabla (ne zagađuje
korisničku povijest — **diferencijator** vs Aider auto-commit/Cline grana); `Snapshot(step)` PRIJE
svakog side-effecta (content-addressed, jeftino), `Rollback` byte-identičan, checkpoint-ID=journal
offset. **Salvage:** `core/checkpoint.py`+`core/memgit.py` port. **RED:** Rollback→byte-identično
pre-stanje, ne dira ne-staged korisničke promjene. **Pareto:** Cline+Aider+LangGraph+shadow-git-izvan-stabla (naš).

**5.2 workspace izolacija (MEHANIZAM+ADAPTER):** git worktree per-task/per-subagent (`NewIsolated
(owner)` detached, isti .git), eksplicitan `SyncBack` first-writer-wins, `Cleanup` lock-release;
container backend (S6) isti interface. **Temelj za S12** (svaki subagent svoj worktree, nema
konflikta). **Salvage:** `core/worktree.py` port. **RED P1.2:** `test_sigkill_restart_reaps_owned_
tree_only` + symlink izvan roota→odbij, djelomični cleanup→CLEANING nikad RELEASED. **Pareto:**
OpenHands+SWE-agent+Sandbox-Agents(beta)+worktree-per-subagent (naš).

**5.3 dirty/atomic/conflict/provenance (MEHANIZAM):** `AtomicWriter` tmp→fsync→rename (nikad in-
place, nikad pola fajla); `TaintTier{GENERATED|USER|UNKNOWN}` first-writer-wins (USER-tainted +
GENERATED write→CONFLICT, tuđi rad se ne gazi); **sandbox-neutralni atestor** (obsidian): provenance
iz NIŽEG trust sloja (sandbox vidio stvarni argv/fs/net), runtime NE atestira sam sebe (6.0
trust-boundary), export W3C PROV-O (veže 6.2+15.3). **Salvage:** `core/provenance.py` datamark port
+ `gortex/diff_engine.go` atomic Go REUSE. **RED:** konkurentni GENERATED-vs-USER edit→CONFLICT,
korisnički sadržaj sačuvan; atomic write usred pada→stari ili novi, nikad pola. **Pareto:** Aider+
Codex+Cline+sandbox-atestor-PROV-O (naš).

**S5 diferencijator:** shadow-git izvan stabla (byte-identičan undo bez zagađenja povijesti) +
worktree-per-subagent (S12 temelj) + sandbox-neutralni atestor. **Honest gap:** 5.1/5.3 bez
dediciranog Annex RED (P0.11 kandidat). **Verifikacija:** kandidati stvarni; `diff_engine.go` Go
reuse. **S5 STATUS: ZAOKRUŽEN.**

---

# S6 — KANONSKA SINTEZA (Sigurnost; NAJKRITIČNIJA — membrane salvage 1:1, ne rewrite; 3-way, Pareto PASS)

**Načelo (audit-konsenzus):** izbrušene membrane se PRESAĐUJU s parity testom, ne pišu iznova.
GORTEX je već Go → direktan reuse. Fail-closed, non-bypassable.

**6.0 trust/PEP (MEHANIZAM):** OPA-stil policy decision point ("smije li subjekt X alat Y nad Z"),
executor fail-closed provodi. **Salvage:** `core/permissions.py classify` + `core/provenance.py`.
**Pareto:** Codex-approval+OpenHands-analyzer+OPA.

**6.1 permission gating (MEHANIZAM):** `permissions.py` shlex-token analiza po argumentu (GREEN/
YELLOW/RED, 5-tier), hooks pre_tool deny. **Salvage:** `core/permissions.py`+`core/hooks.py` port.
**RED:** RED-regex komanda→deny. **Pareto:** Codex+goose+gptme+per-argument.

**6.2 sandbox (MEHANIZAM, per-OS build-tag, GORTEX Go REUSE):** `Boundary` interface;
`sandbox_linux.go` (Landlock 3 syscalla preko `x/sys/unix` BEZ cgo + seccomp prctl BPF + bwrap),
`sandbox_windows.go` (Job Object KILL_ON_JOB_CLOSE + restricted token), `sandbox_darwin.go`
(Seatbelt sandbox-exec adapter — nema Landlock ekvivalent). **GORTEX = Go REUSE 1:1**
(`landlock_linux.go`/`sandbox.go`/`process_manager.go`/`tool_executor.go`/`netjail.go` — NE port,
NE prepisati; samo adapter koji ga zove). Opasni I/O u out-of-process helperu iza autentificiranog
IPC, NIKAD in-process fallback. **RED P2.2 + PARITY:** Landlock write na ne-dopušten path→EPERM
identično NEXUS membrani; bez dopuštenog patha→fail-closed. **Pareto:** Codex+OpenHands+gVisor+GORTEX-parity (naš).

**6.3 egress (MEHANIZAM):** allowlist + **SSRF metadata hard-block** (169.254.169.254 u decimal/hex/
octal/mapped-IPv6 → REFUSE PRIJE diala) + redirect re-check + `netjail filtered_jail` (bwrap prazan
netns, jedini izlaz UDS→FilterProxy). **Salvage:** `core/egress.py`+`core/netjail.py` port/reuse.
**RED:** metadata-IP u bilo kojem kodiranju→block. **Pareto:** Codex+microsandbox+OpenHands+SSRF-hardening (naš).

**6.4 secrets (MEHANIZAM):** keyring, `broker.py` redact (entropija+shape), SECRET_ARGS. **Salvage:**
`core/secrets.py`+`core/broker.py` port. **RED:** secret u izlazu→redigiran prije journala (P0.3).

**6.5 injection-obrana + canary (MEHANIZAM):** datamark fence + guardian judge + deny-list;
**canary tripwire** per-install token, detekcija u izlazu→**FAIL-CLOSED blok cijele isporuke** +
audit+alert+rotacija+ljudska odluka (ne samo redakcija). **Salvage:** `core/provenance.py datamark`+
`core/guardrail.py`+`core/guardian.py`+`core/canary.py`. **RED:** canary u izlazu→blok isporuke.
**Pareto:** LlamaFirewall+NeMo+LLM-Guard+canary-failclosed (naš); AgentDojo mjeri.

**6.6 authn/tenant (`service` flag):** channel-scoped allowlist deny-default, PKCE OAuth; multi-
tenant RBAC. **Salvage:** `gateway/base.py`+`core/oauthcode.py`. **6.7 data-governance:** BASELINE
(jezgra, svaki store: retention/delete/export/PII — Presidio + vlastiti SQLite purge) + SERVICE
dodatak (DSAR/legal-hold). **RED P2.1:** MEMORY_FORGET vs DATA_PURGE razdvojeni.

**6.8 supply-chain (`extensions` flag):** Sigstore/cosign potpis + OSV-Scanner + in-toto provenance;
`core/supplychain.py` OSV + trust-by-hash. **RED P1.5:** post-signature artefakt tamper→publish blok.

**6.9 lifecycle enforcement (MEHANIZAM, jezgra):** `MiddlewareChain` success-faze `before_run→
before_tool→after_tool→before_deliver` (jezgreni policy PRVI i ZADNJI, plugin hook samo SUZI);
**on_error = GRANA iz svake faze** (causal-error+audit, BEZ kasnijih success-hookova). + **P1.4
draft→approve→commit→verify→compensate** za ireverzibilne side-effecte. **Salvage:** `permissions.py`+
`hooks.py`. **RED P1.4:** `test_approval_cannot_authorize_modified_effect`. **Pareto:** NEXUS-hooks+
Claude-Code-referent+DeepSeek-Cordis+on_error-grana (naš).

**S6 = membrane REUSE ne rewrite (gortex 1:1 + parity), per-OS build-tags, fail-closed svugdje.**
**Verifikacija:** kandidati stvarni; GORTEX interni (audit potvrdio). **S6 STATUS: ZAOKRUŽEN.**

---

# S7 — KANONSKA SINTEZA (Pouzdanost; JEDINI owner retry/cancel P0.2; 3-way, Pareto PASS)

**S7 je JEDINI vlasnik retry/deadline/cancel** — 2.2/3.3/3.4 delegati; svaki fizički pokušaj nosi
`s7.AttemptGrant`.

**7.1 timeout/cancel/retry-taksonomija (MEHANIZAM):** `ExecutionPolicy{operation_id,effect_class,
deadline,max_attempts,attempt_timeout,backoff,retryable_codes,fallback_targets,idempotency_key,
cancel_token}` + `AttemptGrant{attempt_no,target,expires_at,grant_nonce}`; stanja PENDING→GRANTED→
RUNNING→{SUCCEEDED|FAILED_RETRYABLE|FAILED_TERMINAL|CANCELLED|UNKNOWN}; samo S7 iz FAILED_RETRYABLE
izdaje sljedeći grant; IRREVERSIBLE+UNKNOWN se NE retry-a bez reconciliationa. context za deadline/
cancel (propagira u child). **Salvage:** `core/recovery.py classify+STRATEGY`. **RED P0.2:**
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
**Salvage:** `core/queue.py _claim` + `core/circuit.py` + `core/fleet.py` port UZ POPRAVAK.
**RED P2.3:** `test_reclaimed_worker_cannot_commit_with_stale_fence` (A token 7 zamrznut, B token 8
commit, A sa 7→STALE, točno jedan rezultat=B). **RED P2.2:** `test_hard_process_budget_kills_
descendants`. **Pareto:** Temporal+Restate+Dapr+lease/fencing/executable-budget (naš — popravlja bug).

**7.3 crash-recovery + durable-delivery (MEHANIZAM):** resume idempotent iz event-loga (journal
fold, ne mutable state); WAL spine (`modernc.org/sqlite`); **N9 durable-delivery:** `run_done ≠
result_delivered` DVA trajna stanja, transactional outbox + idempotent-delivery + receipt (gateway
crash ne gubi odgovor ni ne ponavlja side-effect). **Startup orphan/lock sweep (P1.2):** `ResourceLease
{PROCESS_TREE|WORKSPACE_LOCK|TEMP_DIR}` + start-token (S1.2), OWNED→ORPHAN_SUSPECTED→FENCED→CLEANING→
RELEASED; nedokazivo vlasništvo→QUARANTINED ne kill. **Salvage:** `core/resume.py`+`core/store.py` WAL.
**RED P1.2:** `test_sigkill_restart_reaps_owned_tree_only`. **Pareto:** LangGraph+Temporal+OpenHands+
durable-delivery-outbox+orphan-sweep (naš).

**S7 = popravlja glavni audit-nalaz (queue lease/fencing) + N9 durable-delivery.** **Verifikacija:**
Temporal/Dapr stvarni; Restate source-available (matrica). **S7 STATUS: ZAOKRUŽEN.**

---

# S8 — KANONSKA SINTEZA (Kontekst management; 3-way, Pareto PASS)

**8.1 window-budžet (MEHANIZAM):** `ContextBudget` token-mjerenje + hard-limit + pressure-state +
fold; fallback estimate vidljiv. **Salvage:** `core/contextbudget.py`+`core/context.py`. **Pareto:**
Aider+letta+OpenHands.

**8.2 kompakcija (MEHANIZAM, jezgra):** anchored 8-sekcijski summary (cilj/napredak/odluke/greške/
otvorena-pitanja/artefakti/sljedeći-korak/ograničenja). **KLJUČNO P0.3:** sažetak NASLJEĐUJE
lineage — `trust_class(sažetak)=MAX(izvori)`, `sensitivity=MAX`, `lineage=union+summary-id`;
sažetak NE smije oprati untrusted→SYSTEM/instrukcija (injection vektor). Branch-summary (pi-mono).
**Salvage:** `core/compact.py anchored_summary`. **RED P0.3:** `test_s0_rejects_provenance_laundering`
(untrusted kroz compactor koji skine lineage→PROVENANCE_DOWNGRADE). **Pareto:** letta+OpenHands+pi-mono+lineage-monoton (naš).

**8.3 repo-mapa (MEHANIZAM, coding):** Aider tree-sitter + **PPR (personalized PageRank) rangiranje**
+ NEXUS `archmap` (arhitektura kao queryable graf — coding-diferencijator iznad pretrage). Skalirano
po token-budžetu. **Salvage:** `core/archmap.py`+`core/rank.py PPR`+`core/codeindex.py`+`core/wiki.py`.
Runners-up (obsidian): Understand-Anything/Scan, Graphify. **Pareto:** Aider+Repomix+Continue+archmap-PPR (naš).

**8.4 cache (MEHANIZAM, dva sloja):** (a) API `cache-control` propagacija; (b) lokalni prefix/
RadixAttention (SAMO self-hosted). **Cache-key MORA nositi (codex#19):** namespace/tenant/principal-
scope/authz-policy-version/sensitivity/corpus-version/model/prompt-hash/purge-generation — bez
wildcard fallbacka; SECRET=NO_STORE; purge/revocation povećava generation. **Salvage:** `core/
respcache.py`+`core/promptlayer.py`. **RED codex#19:** `test_cross_tenant_cache_key_cannot_alias`.
**Pareto:** LiteLLM+vLLM+SGLang+tenant-scoped-key (naš).

**8.5 observation-pruning (MEHANIZAM):** filter-PA-komprimiraj tipiziran adapter PO ALATU — test
čuva failures, git-diff čuva hunks, crawl čuva citations; raw artefakt po referenci + fidelity eval.
**Salvage:** `core/crush.py` (fail-open head+tail fallback). Kandidati: RTK (proxy 100+ CLI),
Headroom (multi-tier + learn), SWE-agent ACI-pager. **RED:** pruning čuva error-linije/exit-status.
**Pareto:** RTK+Headroom+SWE-agent+crush (naš, per-tool typed adapter).

**S8 = lineage-monoton kompakcija (anti-injection) + tenant-scoped cache + archmap-PPR coding-
diferencijator.** **Honest gap:** 8.1/8.3/8.5 bez dediciranog Annex RED (P0.3 pokriva 8.2, codex#19
pokriva 8.4). **Verifikacija:** kandidati stvarni (RTK/Headroom obsidian, matrica). **S8 STATUS: ZAOKRUŽEN.**

---

# S9 — KANONSKA SINTEZA (Memorija; `memorija` flag osim 9.1=jezgra; 3-way, Pareto PASS)

**9.1 sesije (JEZGRA):** save/resume=svaki stateful harness (naslanja 7.3); branch/user-visible=
profil. Memory-SPINE=SQLite-WAL (`modernc.org/sqlite`, bez cgo). **Salvage:** `core/session.py`+
`core/resume.py`. Runner-up: Stash (shared team-memory, multi-agent handoff). **Pareto:** goose+OpenHands+Codex+spine.

**9.2 perzistentna memorija (`memorija`):** 5-scope spine (`core/memory.py`), memvec+kg+entres;
write-approval DEFAULT ON, untrusted-context labeling (P0.3). **P2.1 MEMORY_FORGET (reverzibilno,
zabrana recalla) vs DATA_PURGE (ireverzibilno, S6.7 nadjačava, briše i tombstone).** **Salvage:**
`core/memory.py`+`memvec.py`+`kg.py`+`entres.py`. Runner-up: OpenHuman-Memory-Tree/TokenJuice
(cold-start). **RED P2.1:** `test_purge_cannot_complete_with_residual_copy`. **Pareto:** letta+mem0+zep+forget/purge-split.

**9.3 učenje-iz-iskustva (`memorija`):** Hermes Agent (produkcijski OSS, agent-authored skills —
PAŽNJA write_approval default OFF u Hermesu, MI ga uključujemo); reasonbank/reflexion/selfimprove
quality-gated (promocija samo iz verificiranog). **Salvage:** `core/reflexion.py`+`core/reasonbank.py`.
Reflexion/Voyager=research referenti. **Pareto:** Hermes+cognee+research-referenti.

**9.4 konsolidacija/decay (`memorija`, veliki salvage):** `Consolidator.Dream(episodes)→gist+KG`
(offline sleep-time); `Decay` FSRS-lite mijenja RANG ne postojanje (LOSSLESS — original uvijek u
hot/warm/cold arhivi); `Audn.Resolve` kontradikcije ADD/NOOP/SUPERSEDE na read (ne silent overwrite);
`MemGit` verzionirani write (undo memorije); `MemAssoc` spreading-activation; temporalni upiti ("što
sam znao TADA"). **Salvage:** `core/dream.py`+`sleeptime.py`+`audn.py`+`memgit.py`+`recall.py`+
`memassoc.py` — NAJBOGATIJI memorijski salvage. **RED:** decay NE briše bajtove; konsolidacija
untrusted epizode→gist ostaje UNTRUSTED (P0.3). **Pareto:** letta+mem0+zep+sleep-time-decay-AUDN (naš).

**S9 = najbogatiji NEXUS salvage (6 memorijskih modula) + forget/purge trust-razdvajanje.**
**Honest gap:** 9.1/9.3/9.4 bez dediciranog Annex RED (P0.13 "decay-never-destroys-bytes" kandidat).
**Verifikacija:** kandidati stvarni; salvage interni (audit potvrdio). **S9 STATUS: ZAOKRUŽEN.**

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

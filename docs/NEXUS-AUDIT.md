# NEXUS × HARNESS-SPEC v0.6 — ZAJEDNIČKA AUDIT TABLICA

**Datum:** 2026-08-31 · **Meta:** /home/matej/NEXUSv2 @ 1d2583e (read-only, svježe oči)
**Auditori:** codex (line-level), kilo (line-level, 6 paralelnih pregleda), agy, claude
(moderator, header-level). Pojedinačni auditi: AUDIT-{codex,kilo,agy,claude}.md.
**Konsenzus po retku = većina glasova; kod neslaganja naveden dissent.**

## SAŽETAK: 61 IMA · 19 DJELOMIČNO · 5 NEMA (od 85 podsekcija) ≈ 94% spec-a barem djelomično pokriveno

| Spec | Konsenzus | Ključni dokaz | Napomena |
|---|---|---|---|
| 0.1 typed sheme | DJEL | core/toolschema.py:24,94; core/events.py:18 | validacija plića od pune JSON-Scheme; error je string |
| 0.2 state machine | DJEL | core/state.py:18; core/workflow.py:18 | stanja postoje, prijelazi se NE validiraju |
| 0.3 capability negotiation | IMA | core/capabilities.py:54-96; llm/providers.py:138 | fail-closed |
| 0.4 verzioniranje formata | DJEL | core/store.py:21 _migrate; core/events.py:57 | DB+run.json da; ccjsonl/session ne |
| 1.1 slojevita config | DJEL | core/paths.py:56 (repo→user→cwd) | env/CLI nisu u istom resolveru |
| 1.2 cross-platform | IMA | core/paths.py:16; core/subproc.py:29 killpg/taskkill | |
| 1.3 strukturirano logiranje | IMA | core/trace.py:22 JSONL; core/otel.py:48 | kilo dissent: nema logging pipeline (print) |
| 2.1 multi-provider | IMA | llm/providers.py:127 ABC + 7 backendova | |
| 2.2 fallback/retry | IMA | providers.py:764 FallbackModel; _KeyPool; recovery.py:41 | nema distribuirani rate-limit (codex) |
| 2.3 structured output | IMA | spawner.py:54 _salvage_json; no-guess-on-mutation guard | |
| 2.4 lokalni modeli | IMA | providers.py:667 LocalProvider (Ollama/llama.cpp) | |
| 2.5 cost tracking | IMA | providers.py:58 CostTracker; circuit.py:85 | `cost_tracking` flag mrtav |
| 3.1 petlja | IMA | loop_engine.py:119-245; executor.py:1815; runtime.py:498 | anti-sycophancy strukturno (checker≠worker) |
| 3.2 streaming | DJEL | providers.py:439 `"stream": False` | NEMA token-streaminga — sva 4 audita |
| 3.3 cancel/turn | DJEL | steer.py:88; circuit.py:46 | nema Esc/Ctrl-C mid-turn; nema cancel-token |
| 3.4 error recovery | IMA | recovery.py:16-65 taksonomija; OBSERVATION put | |
| 3.5 verifikacija u petlji | IMA | executor.py:1882; testcmd.py; tia.py | core/verifier.py dormantan |
| 4.1 tool registry | IMA | tools.py:70 PRIMITIVES; toolschema.py | codex dissent: validacija plitka |
| 4.2 shell exec | IMA | subproc.py:47 rlimits; gortex/tool_executor.go | najjači podsustav (kilo) |
| 4.3 edit formati | IMA | editapply.py:70 exact→fuzzy→refuse; editrepair; symedit | |
| 4.4 pretraga koda | IMA | codeindex; coderetrieval; lsp.py:152; callgraph; archmap | šire od spec top-3 |
| 4.5 browser/web | IMA | web.py ladder; browse.py:209 egress-guarded CDP | |
| 4.6 MCP | IMA | mcp.py klijent + mcpserve.py SERVER + mcpauth OAuth | bidirekcionalan |
| 5.1 checkpoint/rollback | IMA | checkpoint.py:41 shadow-git; memgit.py | 2 nezavisna undo sustava |
| 5.2 workspace izolacija | IMA | worktree.py:89; landlock; gortex | |
| 5.3 atomic/conflict | DJEL | worktree.py:168 sync_back | tools.py:19 write_file zaobilazi atomiku (codex) |
| 6.0 policy point | IMA | permissions.py:376 classify GREEN/YELLOW/RED | |
| 6.1 permission gating | IMA | permissions.py:170 5-tier; hooks.py:81 | per-tool+argument |
| 6.2 sandbox | IMA | landlock.py; gortex/sandbox.go; netjail bwrap | honest degrade |
| 6.3 egress | IMA | egress.py:202; netjail.py:390 filtered_jail | SSRF metadata hard-block |
| 6.4 secrets | IMA | secrets.py; broker.py:82 (entropija); SECRET_ARGS | |
| 6.5 injection obrana | IMA | provenance.py:40 datamark; guardrail; guardian | fence+judge+deny-list |
| 6.6 authn/tenant | DJEL | gateway/base.py:103 deny-default; oauthcode PKCE | nema multi-tenant RBAC |
| 6.7 data governance | DJEL | memory._forget; export._scrub | nema deklarativnu retention politiku |
| 6.8 supply-chain | DJEL | supplychain.py:148 OSV; trust-by-hash | kripto-POTPISIVANJE NEMA |
| 7.1 timeout/retry taksonomija | DJEL | recovery.py:16 classify | eksplicitni cancel fali |
| 7.2 idempotency/backpressure | DJEL | queue.py:49 atomic claim; USD ceiling | **codex nalaz: nema lease/reclaim za `running` nakon pada — "crash-safe" tvrdnja neostvarena za worker-crash** |
| 7.3 crash recovery | IMA | resume.py:29 idempotent iz event loga; WAL | |
| 8.1 window budžet | IMA | contextbudget.py:60 | |
| 8.2 kompakcija | IMA | compact.py:73 anchored 8-sekcijski summary | |
| 8.3 repo mapa | IMA | codeindex.py:652 repo_map; archmap; rank.py PPR | |
| 8.4 cache | IMA | respcache.py; promptlayer.py prefix breakpoint | |
| 8.5 observation pruning | IMA | crush.py:262 content-aware; compact.py:113 | vrhunsko rješenje (agy) |
| 9.1 sesije | IMA | session.py:172; resume.py; cmd_resume | branch NEMA (kilo) |
| 9.2 perzistentna memorija | IMA | memory.py 5 scopeova; memvec; kg; entres | SQLite-WAL spine |
| 9.3 učenje iz iskustva | IMA | reflexion; reasonbank:33 quality-gated; selfimprove; mastery | evidence-gated promotion |
| 10.1 ingestion | IMA | ingest.py:184; pdf.py 3-tier; mineru | |
| 10.2 chunking | IMA | ingest.py:87 markdown; coderetrieval AST | |
| 10.3 hibridna pretraga | IMA | memvec RRF; fts bm25; rerank cross-encoder | pun stack |
| 10.4 GraphRAG | IMA | kg.py:134 multi-hop; know.py | LightRAG-stil, ne puni GraphRAG |
| 10.5 CAG | NEMA | — | i spec ga drži UNCLEAR |
| 10.6 retrieval auth | NEMA | scope≠principal/ACL | single-user dizajn |
| 11.1 prompt assembly | IMA | promptlayer.py:20; executor.py:2523 | kilo dissent: razasuto, nema jedan assembler |
| 11.2 skills | IMA | skill_loader L1/L2/L3 gated; 128 skillova | najdublji viđeni skill sustav |
| 11.3 projektne instrukcije | DJEL | projectrules.py (.nexusrules) | AGENTS.md samo kao anchor, ne interop |
| 11.4 prompt optimizacija | IMA | optimizer.py:38 propose→eval→promote atomski | |
| 12.1 subagenti | IMA | subagent_orchestrator.py:87; 61 agent paketa | |
| 12.2 workflow/DAG | IMA | _run_dag wave scheduler + cycle-guard; workflow.py | state validacija slaba |
| 12.3 routing | IMA | router.py:21 naučeni; reward.py epsilon-greedy bandit | samoučeći |
| 12.4 HITL | IMA | gate.py:25 persistira jednokratnu ljudsku odluku | |
| 12.5 A2A | NEMA | — | ista slijepa točka kao spec prije v0.5 |
| 13.1 vision | IMA | vision.py:37-71 egress-gated | |
| 13.2 slike | IMA | imagegen; image.py atomski; heavygen fail-closed | |
| 13.3 voice | IMA | voice.py:56-136 STT+TTS | |
| 13.4 video | IMA | video.py:196,454; avatar.py | |
| 14.1 CLI/TUI | IMA | cli.py 2731 linija; tui/ + tmux view | cli.py hotspot |
| 14.2 headless/API | IMA | gateway/openai_api.py OpenAI-kompatibilan + SSE | jezgra nereentrantna (codex) |
| 14.3 web UI | IMA | gateway/dashboard.py read-only XSS-safe | |
| 14.4 IDE | NEMA | samo skill-export interop | nema ACP/ekstenziju |
| 15.1 tracing | IMA | otel.py:31 GenAI atributi OTLP | |
| 15.2 cost dashboardi | DJEL | CostTracker + USD budget | nema dashboard UI |
| 15.3 transcript/audit | IMA | transcript.py; audit.py:23 HASH-CHAIN | tamper-evident |
| 15.4 metrics/SLO | DJEL | health.py swallow; readiness | nema latency/SLO |
| 16.1 benchmark | IMA | bench.py hidden checker F2P/P2P; benchmark_runner | |
| 16.2 ratchet CI | IMA | regression_tester; ratchet_baseline.json; 3-OS CI | |
| 16.3 red-team | IMA | redteam.py non-vacuous; cmdjail; yolo evali | |
| 16.4 flywheel | IMA | reward.py verified-outcome store; leaderboard | |
| 16.5 trajectory export | DJEL | reasonbank trajectory_of; export | nema ShareGPT/SFT format |
| 17.1 pluginovi | IMA | skills/packs/hub/import + hooks | nije DI-kernel ABI |
| 17.2 update | IMA | update.py:180 ff-only trusted-origin + smoke import | nema potpise/kanale |
| 17.3 packaging | DJEL | pyproject + Dockerfile multi-stage | nema single binary |
| 18.1 worker/queue | DJEL | queue.py durable; fleet.py | single-process, ne horizontalno |
| 18.2 multi-tenant gateway | NEMA | single-key, global budget | |
| 18.3 health/rollout | DJEL | Dockerfile HEALTHCHECK; live-canary.yml | nema staged rollback |
| 18.4 migracije/backup | IMA | store._migrate WAL; export.py:164-347 verificiran bundle | |

**5 NEMA: CAG (10.5), retrieval-ACL (10.6), A2A (12.5), IDE/ACP (14.4), multi-tenant (18.2)**
— od toga su CAG i A2A slijepe točke koje je i spec imao do v0.5 (isti knowledge cutoff).

---

## EXTRAS — što NEXUS ima, a spec NEMA (unija sva 4 audita, eksplicitno)

1. **GORTEX** (`gortex/`, Go) — kompajlirana execution membrana preko UNIX socketa: diff
   patching, procesi, MCP server, Landlock — opasni I/O izvan Python runtimea.
2. **Multi-channel gateway** (`gateway/`) — Telegram/WhatsApp/Slack/Discord/Matrix/Signal s
   deny-by-default allowlistama + asinkroni HITL ("agent pita na mobitel, run čeka odgovor").
3. **Anti-sycophancy sloj** — `loop_engine` (checker ocjenjuje git-diff+exit code, ne prozu),
   `confidence.py` BS-detector (ChainPoll C=0.7·O+0.3·S, flip_test), `prreview.py`
   devil's-advocate. Strukturno, ne prompt.
4. **Skill Forge samoevolucija** (`managers/skill_*` 15 modula) — score→doctor→merge→scout
   (GitHub skaut) petlja s rewardom iz verifikacije; harness sam održava svoje vještine.
5. **Sleep-time memorija** — `dream.py` (epizode→gistovi+KG), `sleeptime.py` (background
   kuracija), `audn.py` (ADD/NOOP/SUPERSEDE), `memgit.py` (git-verzija svakog write-a),
   `recall.py` (FSRS-lite decay), `memassoc.py` (spreading activation).
6. **Credit/Reward ledger** (`credit.py`, `reward.py`) — memorija/skill/ruta dobiva kredit
   SAMO iz verificiranih ishoda; routing uči iz toga (epsilon-greedy bandit).
7. **Council** (`managers/council.py`) — 5 persona → anonimni cross-review → chairman sinteza.
8. **Tamper-evident audit** (`audit.py`) — append-only hash-lanac s verify().
9. **Canary tripwire** (`canary.py`) — per-install token za detekciju prompt-leaka.
10. **TIA** (`tia.py`) — .coverage × git diff → pokreni samo pogođene testove.
11. **Reversa** (`reversa.py` + 40 skillova) — reverse-engineering naslijeđenih repoa u
    specifikacije/migracijske planove, allowLegacyEdits:false.
12. **App contract** (`app_contract.py`) — completeness gate za domenske aplikacije.
13. **Proaktivnost** (`schedule.py` cron+heartbeat, `watch.py` file-triggeri).
14. **Role→Agent→Skill data hijerarhija** — 18 rola / 61 agent / 128 skillova kao PODACI
    koje jezgra interpretira ("nova rola = nova mapa, nula izmjena jezgre").
15. **Canvas** (`canvas.py`) — kompaktni JSON → Excalidraw SVG/PNG (3× manje tokena).
16. **PDF driver** (`pdf.py` 87KB) — edit/merge/forms/sign/OCR.
17. **SDK** (`sdk.py`) — fluent embeddable API, dry-run default.
18. **Health swallow telemetrija** (`health.py`) — broji progutane best-effort greške.
19. **Field bridge** (`bugreport.py`, update notify) — teren: scrubban bundle → issue → upgrade.

---

## VERDIKT — **KONSENZUS 4:0 (2026-08-31): A — KERNEL-EXTRACTION NADOGRADNJA NEXUS-a**

Runda pomirenja: claude i agy ažurirali s B na A pred line-level dokazima (izbrušene
sigurnosne membrane: ReDoS-otporni permission regex s process-substitution detekcijom,
SSRF normalizacija oktalnih/hex/mapped-IPv6 IP-jeva, deterministički editrepair ladder —
rewrite = ponovno otkrivanje svih tih rubova bez postojećeg 3-OS CI ratchet dokaza).
NIJE greenfield, NIJE gomilanje featurea: S0 ugovori se grade NOVI, dokazani moduli
postaju adapteri, parity gate čuva ponašanje. Finalni glasovi: REVIEW-{codex,kilo,agy}.md.

**Dogovorenih prvih 5 koraka ekstrakcije (codex, potvrđeno):**
1. Behavioral parity gate PRIJE reza (s negativnim slučajevima — mora pasti kad se
   adapter zaobiđe)
2. Verzionirani S0 envelope (Message/ToolCall/ToolResult/Error + schema ID); jedna
   vertikala end-to-end, stari formati iza adaptera
3. Centralni state machine s tablicom legalnih tranzicija + upcaster za stare evente
4. Jedan schema/migration owner (task_queue i reason_bank DDL u store.py; arhitektonski
   test brani runtime modulima vlastite tablice)
5. Rasjeći KOMPOZICIJU ne membrane: executor.py → portovi (prompt/driver/dispatch/policy),
   permissions/egress/worktree/memory ostaju adapteri; cli.py → parse/dispatch; stari put
   se briše tek po dokazano ekvivalentnoj vertikali
+ usput zatvoriti codexov queue-lease nalaz (7.2) i 5 NEMA rupa tek kad ih profil aktivira.

### Povijest glasanja (prije pomirenja): PODIJELJENO 2:2

**Za NADOGRADNJU (codex, kilo):** pokrivenost 59-64/85 IMA; najvrjedniji podsustavi
(permissions membrane, egress/netjail, memory spine, eval/red-team) su koherentni i
adversarijalno dokazani; greenfield do pariteta = procjena 2,5-4× truda (codex); rewrite
riskira regresiju izbrušenih sigurnosnih rubova. Codexov plan: **kernel extraction** —
izdvojiti typed envelope + state machine + jedan schema owner, postojeći moduli postaju
adapteri; NIJE nastavak gomilanja featurea.

**Za NOVI+SALVAGE (agy, claude):** 977 fajlova / 84 organske steal-runde bez S0 sloja;
executor.py 3721 linija miješa prompt+dispatch+confinement+orkestraciju; store schema
ownership curi (queue/reasonbank prave vlastite tablice); dvojezična Go/Python
sinkronizacija; retrofit spec-ove core/profile mape = razrez monolita. Salvage lista
(crush, netjail+egress, checkpoint, rank+callgraph, tia, editrepair, confidence+grader,
bench, canvas) presađuje dragulje u čist P0-P6 kostur.

**Zajedničko svima:** (a) NEXUS je rudnik dokazanih rješenja — ništa se ne baca;
(b) S0 ugovorni sloj mora nastati (nijedan audit ga nije našao potpunog); (c) 5 NEMA rupa
je malo i poznato; (d) executor.py i cli.py se moraju rascijepiti u OBA scenarija.

**Ključni sporni fakt:** cijena razvezivanja postojećeg couplinga (nadogradnja) vs cijena
ponovnog dokazivanja sigurnosnih granica (greenfield). Procjene se razlikuju 3-10×
(agy: greenfield 2-3 tj; codex: greenfield 2,5-4× od 1,0× ekstrakcije) — NIJEDNA nije
mjerena, obje su inferencija.

**Sljedeći korak:** runda pomirenja verdikta.

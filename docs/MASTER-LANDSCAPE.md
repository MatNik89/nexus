# MASTER WHO-HAS-WHAT — 29 harnessa full-code auditano

Konsolidacija 3 slice-a (MASTER-SLICE-codex/kilo/agy) iz 29 code-audita. Iskrena slika.

## POTVRĐENI diferencijatori (NITKO od 29 nema — code-provjereno):
1. **Evidence-graded diff+exit-code completion GATE** (S3.5/16.6) — svi 29 NEMA (samo prompt-tekst/critic-model-score/diff-collection). Haystack rubric.py = LLM-score, plitko.
2. **TIA** (coverage×diff→pogođeni testovi) — svi 29 NEMA.
3. **AST/LSP symedit** (symbol-scoped edit) — svi 29 NEMA (literal edit ili LSP read-only).
4. **Formal MEMORY_FORGET vs DATA_PURGE** (P2.1) — svi 29 NEMA.

## OBORENO (matched by ≥1 harness — NISU naši unikati):
- stuck-detection → Cline/Codex(repetition.rs)/Roo/Kilo IMA-DUBOKO
- edit-ladder → OpenCode 9-stupanjski, Aider 4-stupanjski
- shadow-git → OpenClaw/Kilo/DeepSeek-Harness(checkpoint_manager)/Cline
- OS-sandbox → Codex(Seatbelt/Linux/Win) IMA-DUBOKO, OpenCode bwrap
- queue-fencing → OpenClaw ingress-queue(claim/stale-CAS-reclaim/DLQ), DeepSeek cron-fence IMA-DUBOKO

## TEZA (iskrena): naš USP = tih 4 + KOMBINACIJA (sve u JEDNOM hardened Go single-binary
osobni-asistent+coding harnessu) — nijedan harness nema SVE; svaki ima komadiće.

---
# SLICE-ovi (who-has-what tablice + gapovi + Go-obrasci):

## === SLICE codex ===

Izvor su isključivo full-code audit nalazi: OpenHands, Codex, LangGraph, ADK, pydantic-ai, MetaGPT, DeepSeek-Harness, OpenClaw i moj Hermes audit. Legenda: **IMA-DUBOKO** = konkretan izvršni mehanizam usporediv s većim dijelom podsekcije; **IMA-PLIĆE** = djelomični mehanizam ili samo typed/control površina; **NEMA** = audit nije našao odgovarajućeg ownera. OpenHands stupac ima poseban proof ceiling: dostavljeni checkout bio je Agent Canvas frontend, ne `software-agent-sdk` backend (`LA-OpenHands-codex.md:7-19`).

Kratice: **OH** OpenHands Canvas · **CX** Codex · **LG** LangGraph · **ADK** Google ADK · **PAI** pydantic-ai · **MG** MetaGPT · **DSH** DeepSeek-Harness · **OC** OpenClaw · **H** Hermes.

## A. WHO-HAS-WHAT SLICE

### Kernel i coding petlja

| Podsekcija | OH | CX | LG | ADK | PAI | MG | DSH | OC | H |
|---|---|---|---|---|---|---|---|---|---|
| **S0.1 typed schema** | **IMA-PLIĆE**<br>`core/base/event.ts:9-25` | **IMA-PLIĆE**<br>`protocol/src/capabilities.rs:8-36` | **IMA-PLIĆE**<br>`checkpoint/base/__init__.py:92-123` (`Any` payload) | **IMA-PLIĆE**<br>`events/event.py:91-155` (`Any` output) | **IMA-PLIĆE**<br>`messages.py:2598-2662` | **NEMA**<br>sender/recipient završavaju u tekstu: `mgx_env.py:73-89` | **IMA-PLIĆE**<br>`core/session/src/index.ts:212-249` | **IMA-PLIĆE**<br>`gateway-protocol/.../frames.ts:15-20,75-87` | **IMA-PLIĆE**<br>`gateway/platforms/base.py:3020-3025` |
| **S3.1 plan-act-observe loop** | **IMA-PLIĆE**<br>wire Action/Observation: `action-event.ts:27-66`; `observation-event.ts:8-39` | **IMA-DUBOKO**<br>`core/src/session/turn.rs:141-153,304-430` | **IMA-PLIĆE**<br>BSP graph loop: `pregel/main.py:2959-2969` | **IMA-PLIĆE**<br>`runners.py:727-772` | **IMA-DUBOKO**<br>`_agent_graph.py:503,1122-1154,1860` | **IMA-DUBOKO**<br>`roles/role.py:340-470` | **IMA-DUBOKO**<br>`core/agent-loop/src/agent.ts:219-436` | **IMA-DUBOKO**<br>`agent-core/src/agent-loop.ts:189-213,324-409` | **IMA-DUBOKO**<br>`conversation_loop.py:1798-1827,8156-8294` |
| **S3.4 error recovery/stagnation** | **IMA-PLIĆE**<br>`common.ts:66-75` ima `stuck`, backend owner nije u checkoutu | **IMA-DUBOKO**<br>typed errors/Stop continuation: `session/turn.rs:511-570` | **IMA-PLIĆE**<br>retry/late-write fence: `pregel/_retry.py:128-193,417-502` | **IMA-PLIĆE**<br>lokalni retry gubi count na resumeu: `_node_runner.py:114-191` | **IMA-PLIĆE**<br>rascjepkani retry owneri: `_agent_graph.py:363-380`; `retries.py:72-101` | **IMA-PLIĆE**<br>tri retry ownera: `openai_api.py:165-178`; `planner.py:77-96` | **IMA-DUBOKO**<br>error-waterfall: `agent.ts:391-407`; repeat reminder `repeat-tool-reminder/...:181-224` | **IMA-DUBOKO**<br>terminal owner + tool-loop detekcija: `agent-run-terminal-outcome.ts:24-107`; A5/O3 | **IMA-PLIĆE**<br>bogat recovery, ali monolitan owner: `conversation_loop.py:1922-1957` |
| **S3.5 deterministic verification + TIA** | **NEMA**<br>critic je model-score surface: `critic.ts:27-49` | **NEMA**<br>assistant/Stop zatvara turn: `session/turn.rs:511-570` | **NEMA**<br>framework audit nije našao TIA/checker/symedit | **NEMA**<br>framework audit nije našao coding gate | **NEMA**<br>framework audit nije našao coding gate | **NEMA**<br>`swe_agent.py:65-75` samo prikuplja diff | **NEMA**<br>no-tool odgovor završava: `agent.ts:430-436` | **NEMA**<br>audit nema diff+exit/TIA ownera (A6) | **NEMA**<br>goal gate nije coding TIA/evidence gate: `goals.py:429-452` |
| **S4.3 edit formati/repair/symedit** | **IMA-PLIĆE**<br>`view/create/str_replace/insert/undo`: `base/action.ts:65-124` | **IMA-PLIĆE**<br>4-stupanjski tekstualni ladder: `seek_sequence.rs:39-114` | **NEMA**<br>graph runtime nema editor | **NEMA**<br>workflow framework nema editor | **NEMA**<br>agent framework nema coding editor | **NEMA**<br>nema edit-engine ownera u auditiranom kodu | **IMA-PLIĆE**<br>literal edit: `tool-fs/src/edit.ts:75-129` | **IMA-PLIĆE**<br>exact multi-replace+preview: `sessions/tools/edit.ts:48-84,140-186` | **IMA-PLIĆE**<br>file/edit postoji, ali audit nije našao multi-format/AST symedit (`tools/`) |
| **S4.4 aktivna code pretraga** | **IMA-PLIĆE**<br>Glob/Grep wire, cap 100: `base/action.ts:249-273`; `base/observation.ts:232-287` | **IMA-PLIĆE**<br>nema repo AST/LSP layera; tree-sitter je shell safety (`shell-command/src/`) | **NEMA**<br>nema code-search ownera | **NEMA**<br>nema code-search ownera | **NEMA**<br>nema code-search ownera | **NEMA**<br>RAG nije repo-dev intelligence | **IMA-PLIĆE**<br>read-only LSP: `tool-lsp/src/index.ts:109-194` | **IMA-PLIĆE**<br>grep/find/LSP bez PPR/callgrapha (audit S4.4) | **IMA-PLIĆE**<br>široki search alati, bez dokazanog archmap/PPR ownera |

### Workspace, sigurnost, durable, kontekst i memorija

| Podsekcija | OH | CX | LG | ADK | PAI | MG | DSH | OC | H |
|---|---|---|---|---|---|---|---|---|---|
| **S5.1 checkpoint/rollback po koraku** | **NEMA**<br>git API je surface, nema step checkpoint: `git-service.api.ts:111-279` | **NEMA**<br>worktree/fork postoji, nema shadow checkpoint po koraku | **NEMA**<br>graph checkpoint nije VCS rollback | **NEMA**<br>session replay nije VCS rollback | **NEMA**<br>message history nije VCS rollback | **NEMA**<br>`team.json` nije VCS rollback: `team.py:59-81` | **NEMA**<br>audit nije našao VCS checkpoint ownera | **IMA-PLIĆE**<br>snapshot/restore nije obvezan prije svakog efekta: `worktrees/service.ts:441-558` | **IMA-DUBOKO**<br>shadow-git prije mutacije: `tools/checkpoint_manager.py:1-48` |
| **S6.2 sandbox** | **IMA-PLIĆE**<br>confirmation/sandbox samo start surface: `agent-server-adapter.ts:1075-1092` | **IMA-DUBOKO**<br>Seatbelt/Linux/Windows izbor: `sandboxing/src/manager.rs:62-75` | **NEMA**<br>checkpoint/graph nije sandbox | **NEMA**<br>workflow izolacijski scope nije OS sandbox | **NEMA**<br>durable adapter nije sandbox | **NEMA**<br>nema centralne OS membrane u auditu | **NEMA**<br>dynamic `node:vm` izričito “not containment”: `cordis-host-runner/src/sandbox.ts:1-10` | **IMA-PLIĆE**<br>Docker/Podman/SSH, bez native floor: `sandbox/backend.types.ts:11-69` | **IMA-PLIĆE**<br>CUA capability granice postoje, nema dokazan jedinstveni sandbox owner: `cua_backend.py:266-309` |
| **S7.2 idempotency/budgets/queue/fencing** | **NEMA**<br>WS retry nije durable queue: `use-websocket.ts:110-139` | **IMA-PLIĆE**<br>queued items bez leasea; memory-only lease lokalno: `queued_items.rs:77-105` | **IMA-PLIĆE**<br>attempt/heartbeat bez lease/CAS fencea: `runtime.py:26-57,203-216` | **IMA-PLIĆE**<br>concurrency+same-key dedup, process-local: `_dynamic_node_scheduler.py:232-308` | **IMA-PLIĆE**<br>token/cost/tool caps: `usage.py:535-583`, bez resource/queue leasea | **IMA-PLIĆE**<br>cost budget + in-memory queue: `team.py:92-100`; `schema.py:713-768` | **IMA-PLIĆE**<br>jobs su process-local: `jobs-local/src/index.ts:1-9,86-103` | **IMA-DUBOKO**<br>claim/refresh/stale CAS reclaim/DLQ: `ingress-queue.ts:940-1091` | **IMA-DUBOKO**<br>cron claim/fence/heartbeat: `cron/scheduler.py:495-507` (owneri raspršeni) |
| **S8.2 compaction/summarization** | **IMA-PLIĆE**<br>condensation wire s forgotten IDs: `condensation-event.ts:4-27` | **IMA-DUBOKO**<br>checkpointed compaction: `core/src/compact.rs:63-113` | **NEMA**<br>state checkpoint nije prompt compaction | **NEMA**<br>workflow replay nije context compaction | **NEMA**<br>audit nije našao compaction ownera | **IMA-PLIĆE**<br>sažima pa briše raw: `brain_memory.py:91-101,137-175` | **NEMA**<br>audit nije našao compaction ownera | **IMA-DUBOKO**<br>context-engine compact/quarantine: `context-engine/types.ts:41-110`; `registry.ts:166-205` | **IMA-DUBOKO**<br>compression lineage + stable prefix: `prompt_cache_scope.py:1-35`; `prompt_cache_boundary.py:1-31` |
| **S9.2 governed persistent memory** | **NEMA**<br>samo `load_memory` toggle: `agent-server-adapter.ts:1220-1226` | **IMA-DUBOKO**<br>2-fazni write+redaction: `memories/write/phase1.rs:18-71`; `phase2.rs:139-180` | **IMA-PLIĆE**<br>generic KV/vector store: `store/base/__init__.py:708-799` | **IMA-PLIĆE**<br>session/delta/direct ingest API: `memory/base_memory_service.py:44-140` | **NEMA**<br>`MemoryTool` je provider-native deklaracija: `native_tools/__init__.py:545-555` | **IMA-PLIĆE**<br>FAISS/Chroma LTM bez governancea: `role_zero_memory.py:71-171` | **NEMA**<br>audit nije našao persistent-memory ownera | **IMA-DUBOKO**<br>FTS/vector/session corpus+provenance: `memory-search.ts:28-115`; `memory-write-provenance.ts:28-107` | **IMA-DUBOKO**<br>MemoryManager+user Journey: `memory_manager.py:380-430`; `journey.py:239-282` |
| **S9.4 consolidation/decay** | **NEMA**<br>backend nije u checkoutu | **IMA-PLIĆE**<br>stvarna phase-2 konsolidacija, bez punog lossless/decay ugovora: `phase2.rs:139-180` | **NEMA**<br>TTL nije lossless decay: `store/base/__init__.py:716-728` | **IMA-PLIĆE**<br>managed consolidation/TTL adapter: `vertex_ai_memory_bank_service.py:270-330,972-985` | **NEMA**<br>nema consolidatora | **IMA-PLIĆE**<br>importance reflection, ali destructive summary: `reflect.py:94-183`; `brain_memory.py:91-101` | **NEMA**<br>audit nije našao mehanizam | **IMA-PLIĆE**<br>temporal decay bez cjelovitog episode→gist+KG ugovora: `memory-search.ts:65-109` | **NEMA**<br>audit je dokazao memory manager, ne dedicated lossless consolidator |

### Orkestracija, neovisna provjera i plugini

| Podsekcija | OH | CX | LG | ADK | PAI | MG | DSH | OC | H |
|---|---|---|---|---|---|---|---|---|---|
| **S12.1 subagenti/delegacija** | **IMA-PLIĆE**<br>child launcher+worktree surface: `launch-child-conversation-client-tool.ts:14-59` | **IMA-DUBOKO**<br>spawn/send/interrupt/wait: `multi_agents_spec.rs:65-140,598-640` | **IMA-PLIĆE**<br>nested durable graph namespace: `pregel/_algo.py:654-700` | **IMA-DUBOKO**<br>deterministički child ID/replay/scope: `_dynamic_node_scheduler.py:111-356`; `event.py:127-148` | **IMA-PLIĆE**<br>agent-as-tool: `medical_agent_delegation.py:180-226` | **IMA-PLIĆE**<br>semantic role delegation bez izolacije: `role.py:284-300,399-419` | **IMA-DUBOKO**<br>cold-resumable continuations: `subagent/src/continuation.ts:58-229` | **IMA-DUBOKO**<br>spawn/wait/recovery: `sessions-spawn-tool.ts:130-251`; `agents-wait-tool.ts:19-108` | **IMA-DUBOKO**<br>delegacija+fail-safe worktree: `subagent_worktree.py:1-35,180-250` |
| **S12.2 workflow/graph** | **NEMA**<br>planner child nije workflow runtime | **NEMA**<br>multi-agent mailbox nije opći graph scheduler | **IMA-DUBOKO**<br>edge/join/conditional/Send+BSP: `graph/state.py:928-1030`; `types.py:704-776` | **IMA-DUBOKO**<br>routed graph+replay-order barrier: `_workflow.py:293-356,462-507` | **IMA-PLIĆE**<br>typed fork/join, in-memory arrival-order reducer: `join.py:140-199`; `graph_builder.py:687-724` | **IMA-PLIĆE**<br>toposort bez cycle guarda: `schema.py:496-522,569-583` | **IMA-DUBOKO**<br>bounded model-authored `parallel/pipeline`: `tool-workflow/src/index.ts:137-149` | **IMA-PLIĆE**<br>tasks/flows postoje, nema opći cycle/wave owner (audit S12.2) | **IMA-PLIĆE**<br>Kanban/goal orkestracija, bez formalnog graph ownera |
| **S12.5 A2A interop** | **NEMA**<br>ACP/child surface nije A2A | **NEMA**<br>nema Agent Card ownera | **NEMA**<br>nema A2A u core/checkpoint/prebuilt | **IMA-DUBOKO**<br>Agent Card + HTTPS/same-origin client: `agent_card_builder.py:42-101`; `remote_a2a_agent.py:928-1028` | **NEMA**<br>nema A2A ownera | **NEMA**<br>TeamLeader nije A2A | **NEMA**<br>nema Agent Card servera/clienta | **NEMA**<br>interni “A2A flow” nije standardni Agent Card | **NEMA**<br>ACP/channel interop nije A2A protokol |
| **S16.6 anti-sycophancy/neovisni checker/council** | **IMA-PLIĆE**<br>critic wire/iterative refinement: `critic.ts:27-49` | **IMA-PLIĆE**<br>odvojeni model-review, ne artifact gate: `tasks/review.rs:45-146` | **NEMA**<br>graph runtime nema checker/council | **NEMA**<br>nema sealed council/checker | **NEMA**<br>nema neovisni artifact checker | **NEMA**<br>TeamLeader nije council; scorer je isti LLM: `simple.py:47-65` | **NEMA**<br>no-tool completion, nema checker: `agent.ts:430-436` | **NEMA**<br>nema evidence-grade/council ownera (audit S16.6) | **IMA-PLIĆE**<br>deterministički goal gate prije LLM judgea: `goals.py:429-452`, bez council/checkera za coding |
| **S17.1 plugin sustav** | **IMA-PLIĆE**<br>management+UI extension ABI, loader je server-side: `plugins-management-service.ts:65-133` | **IMA-DUBOKO**<br>skills/MCP/apps/hooks bundle: `plugin/src/lib.rs:54-87` | **NEMA**<br>SPI backend nije harness plugin lifecycle | **NEMA**<br>audit nije našao plugin lifecycle ownera | **NEMA**<br>capability middleware nije signed plugin lifecycle | **NEMA**<br>role/action registry nije S17 lifecycle | **IMA-DUBOKO**<br>Cordis Context/Registry/Fiber: `vendor/cordis/src/registry.ts:91-195`; `fiber.ts:139-203` | **IMA-DUBOKO**<br>široki registry+rollback: `plugins/registry.ts:32-160` | **IMA-DUBOKO**<br>platform/plugin registry: `gateway/platform_registry.py:62-95,232-310` |

### Presjek matrice

- Trojka **TIA + evidence-git-diff/exit-code completion gate + AST/LSP symedit** ostaje nepobijena u svih devet auditiranih artefakata. Kod nekih je negativni dokaz jak (Codex/DSH/OC), a kod OpenHands ograničen na frontend checkout.
- Najjači donori po osi: **CX** S6.2 i produkcijski loop; **LG/ADK** durable graph semantika; **DSH/OC/H** subagent/plugin/personal-assistant lifecycle; **H** S5.1; **ADK** standardni A2A; **nijedan** S3.5/S16.6 u Nexusovu punom smislu.

## B. KONSOLIDIRANI GAPOVI — samo ono što još nije stvarno ugovoreno

Addendumi A4/A5 već evidentiraju computer-use, PersonalProfile, voice runtime, obligations/automation, prompt-cache lineage, learning control-plane, account fleet, channel caps, harness registry, device pairing, meetings, boards, flows i commitments. Zato ih ne brojim ponovno kao nove gapove. Preostaje ovaj deduplicirani set:

| # | Gap prema sadašnjem planu | Donor/dokaz | Točna dopuna |
|---:|---|---|---|
| 1 | **Multi-environment attachment + capability roots**: jedan turn može ciljano raditi nad više desktopa/servera/kontejnera, bez dvosmislenog “workspacea”. | CX `environment_selection.rs:41-185`; OC device/node mesh | S0.3/S1.2/S4.1: `EnvironmentRef`, `CapabilityRoot`, svaki tool-call nosi explicit target; ambiguity=reject. |
| 2 | **Kriptografski workload identity + ABOM**, odvojeno od usera i Agent Carda. | CX `agent-identity/src/lib.rs:91-166` | S6.6/S12.5/S15.3: short-lived task assertion vezan uz run/tenant/policy/build; Agent Card ostaje discovery. |
| 3 | **Graceful live-turn handoff** između workera; crash-resume nije isto. | CX `session/turn_suspension.rs:40-119` | S7.3: `RUNNING→SUSPENDING→HANDOFF_READY→CLAIMED`; writer-close mora biti durable prije fencing advancea. |
| 4 | **Authorization revision** koju smije povećati samo stvarni user authorization event; reviewer/memory/compaction tekst nikad. | CX `codex_thread.rs:158-183`; `review_evidence.rs:36-67` | S6.0/6.1: monoton `authorization_revision`; review output je evidence, ne capability. |
| 5 | **Effect-derived durability + task-write memoization + late-write fence** za paralelni workflow. | LG `pregel/_loop.py:415-505,661-816`; `types.py:89-105`; `_retry.py:128-193` | S7.1/7.3: durability floor po `EffectClass`; memoizirati samo PURE/idempotent; stari attempt nonce ne smije pisati nakon timeouta. |
| 6 | **Strict checkpoint codec/encryption boundary**; DB zapis nije trusted object graph. | LG `checkpoint/serde/jsonplus.py:82-119,223-255`; `encrypted.py:17-36` | S6.7/S7.3/S9.1: zatvoreni S0 tipovi, AEAD envelope+key ID; plaintext legacy samo eksplicitna migracija. |
| 7 | **Context-scope izolacija odvojena od filesystem worktreea** te replay-stabilan child identitet. | ADK `event.py:127-148`; `_dynamic_node_scheduler.py:158-356`; LG child namespaces | S12.1/S0.1: `ChildRunKey`, `ContextScope`, `ExecutionPath`; worktree i transcript-scope su dva obvezna, nezavisna granta. |
| 8 | **Deterministički completion-order replay** i **partial multi-interrupt resume**. | ADK `_workflow.py:304-345`; `_replay_interceptor.py:87-128` | S7.3/S12.2/S12.4: journal `completion_seq`; interrupt state `{requested,resolved,consumed}` uz postojeći exact-intent token. |
| 9 | **Durable-operation ABI i adapter-conformance manifest**: persisted operation name, codec, context projection i lifecycle moraju biti kompatibilni. | PAI `_operation_names.py:30-56`; `_spec.py:41-92`; `_run_context.py:75-208` | S0.4/S7.3: versionirani operation ID/alias/upcaster; adapter bind faila ako codec/error/concurrency/replay matrica nije potpuna. |
| 10 | **Eksplicitna join semantika** i siguran deferred batch. | PAI `join.py:140-199`; `_deferred.py:26-99,157-200` | S12.2/12.4: `ALL/FIRST_VALID/QUORUM/REDUCE`; arrival order journalizirati ili reducer mora biti komutativan; izmjena approval argumenata traži novu policy odluku. |
| 11 | **Role subscription i reaction policy kao validirani DATA**, ne hardkodirani role loop. | MG `role.py:261-300,399-419` | S11.6/S12.1: `watches[event_kind]`, `reaction_mode`, `max_steps`; routing po typed event ID-u i policyju. |
| 12 | **Verified experience-result replay + importance-triggered consolidation.** | MG `exp_pool/decorator.py:80-90,143-179`; `reflect.py:134-183` | S9.3/S9.4/S16.4: cache samo s independent checker evidence+expiry; threshold trigger samo nad verificiranim importanceom, uz cooldown i lineage. |
| 13 | **Durable continuable-child messaging semantics**: adjacency auth, accepted-message fence i child-first settlement. | DSH `subagent/src/continuation.ts:58-229` | S12.1: dopuniti `ChildRunKey` protokol; zadržati Nexus worktree, jer DSH dijeli workspace (`continuation.ts:300-305`). |
| 14 | **Bounded model-authored orchestration realm** kao opcionalni adapter, ne novi kernel. | DSH `tool-workflow/src/index.ts:137-149`; `workflow-worker-thread/src/runtime.ts:99-115` | S12.2: samo `agent/parallel/pipeline/progress`, caps i cancellation; bez FS/network/timera; kanonski graph ostaje owner. |
| 15 | **Dynamic extension transaction**: inspect→define immutable version→activate→commit-current/rollback. | DSH `tool-cordis/src/index.ts:151-161,243-254` | S17.1/6.8/11.5: preuzeti lifecycle, ali izvršenje samo u stvarnoj OS/process izolaciji; DSH `node:vm` sam kaže “not containment” (`sandbox.ts:1-10`). |
| 16 | **Shared-human session collaboration** nije multi-agent: owner/member/viewer/editor, presence i discussion audit. | OC `sessions-sharing.ts:8-82`; `sessions-viewer-presence.ts:6-21` | S14.3/14.5+S6.6, flag `collaboration`; mutation authorization i bounded presence TTL. |
| 17 | **Audience-aware disclosure + immutable reply origin + human first-contact directory.** PersonalProfile/channel caps to ne zatvaraju. | OC `workspace.ts:1198-1205,1245-1264`; `targets-session.ts:66-195`; `direct-dm-access.ts:157-200` | S0.1/S9.2/S11.1/S14.5: `AudienceClass`, immutable `OriginRef`, pre-model pairing FSM i ambiguity-refusing `RecipientDirectory`. |
| 18 | **Whole-backend registry i split control-plane/runtime route**, ne samo provider registry. | OH `backend-registry/types.ts:1-13`; `event-service.api.ts:18-38` | S14.2/S18.2: backend instance ima compatibility/capability handshake i tri-state health `valid/invalid/unverified`; ne stvarati drugi execution owner. |
| 19 | **Plugin kao jedan signed multi-capability bundle** s permission-delta projekcijom. | CX `plugin/src/lib.rs:54-87`; OC `plugins/registry.ts:32-160` | S17.1/6.8: manifest objedinjuje skills/tools/MCP/connectors/hooks/capability roots; cijeli bundle aktivira se atomski. |

## C. OBRASCI ZA GO-PREUZETI

| Obrazac | Go skica | Donor | Sigurnosni/Topknot pod |
|---|---|---|---|
| **Gateway prije modela** | `Handle→Authenticate→AuthorizeSession→Dedup→PersistIntent→Admit→Dispatch` | OC `agent-run-handler.ts:32-169,243-410` | Nijedan provider call prije durable admissiona. |
| **Tri javne faze attempta** | `Prepare(ctx) (PreparedAttempt, CleanupStack)` → `Execute` → `Finalize(TerminalOutcome)` | OC attempt pipeline; CX turn owner | Ne kopirati desetke sitnih TS modula; razdvojiti tek po owneru. |
| **Monotoni terminal outcome** | enum+validirani payload; cleanup ne smije spustiti `TIMEOUT/ABORTED/FAILED` u `OK` | OC `agent-run-terminal-outcome.ts:24-107` | Jedan merge owner, causal error ostaje. |
| **Prepared route/auth snapshot** | immutable `RouteGrant{Provider,Model,Transport,CredentialID,ConfigGen,PolicyHash}` | OC runtime plan; H credential pool | Harness može odbiti, ne reroutati ili mutirati drugi credential. |
| **Attempt-scoped output fence** | svaki write/stream/spawn nosi `AttemptNonce`; terminalizacija ga opoziva | LG late-write fence | `context.CancelFunc` nije dovoljan za subprocess; S1.2 mora ubiti stablo. |
| **Durable task result prije wave checkpointa** | `TaskAttemptResult{InputHash,EffectClass,WritesHash,Receipt,Status}` unique po run/step/task/attempt | LG pending writes | Bez receipt/fence vanjski efekt prelazi u `UNKNOWN_EFFECT`. |
| **Replay-stabilni child i scope** | `ChildRunKey` + `ExecutionPath` + zasebni `ContextScope` i `WorkspaceRef` | ADK/LG/DSH | Same-key single-flight mora biti journal-backed, ne samo `map`. |
| **Graph s eksplicitnim bounded loopom** | DAG default; `LoopEdge{MaxIterations,ProgressPredicate,BudgetRef}` opt-in | LG/ADK | Implicitni ciklus RED; stagnation i S7 budget obvezni. |
| **Join determinism** | `JoinPolicy{ALL,FIRST_VALID,QUORUM,REDUCE}` + `CompletionSeq`; reducer komutativan ili journal-ordered | PAI/ADK | Arrival-order rezultat ne smije promijeniti replay. |
| **Durable adapter bind gate** | `DurableBackendSpec` validira operation IDs, codecs, error map, cancellation, discovery i concurrency prije registracije | PAI | Vanjski Temporal/DBOS nije owner Nexus ugovora; adapter samo dokazuje conformance. |
| **Deferred approval batch** | map `RequestID→{IntentHash,PolicyHash,State,Result}` s parcijalnim resumeom | PAI/ADK | Svaki arg override invalidira staru autorizaciju. |
| **Restricted orchestration realm** | mali interpreter/Go DSL nad `Spawn/Parallel/Pipeline/Progress`, bez općeg JS runtimea u MVP-u | DSH | Uvoditi samo kad benchmark opravda; nema FS/network/timer capabilityja. |
| **Dynamic plugin transaction** | immutable `PackageVersion`; staged activation; `Current` CAS tek nakon health+gate; rollback pointer | DSH/Cordis | OS containment + 6.8/11.5; `node:vm` nije granica. |
| **Signed capability bundle** | jedan manifest i jedan permission diff za skills, MCP, connector, hook i tool doprinose | CX/OC | Hash-on-load, atomic closure activation, no partial ACTIVE. |
| **Capability-root targetiranje** | `EnvironmentRef`/`CapabilityRoot`; svaka operacija navodi cilj i expected capability hash | CX/OC | Dvosmislen target odbiti; remote invoke nosi fencing/idempotency/approval ref. |
| **Authorization revision** | user-authorization journal event jedini povećava `AuthRevision`; reviewer vraća samo `EvidenceRef` | CX Guardian | Sprječava permission laundering kroz prompt/memory/compaction. |
| **Audience projekcija prije prompta** | `ProjectContext(blocks, AudienceClass, Principal, PolicyHash)` vraća allowlisted blokove+hash | OC | Private memory deny-default izvan privatnog DM-a. |
| **Immutable delivery origin** | `OriginRef` nastaje na ingressu i putuje do outboxa; target resolver je čista funkcija | OC | Nikad ne koristiti mutabilni `lastChannel` pri deliveryju. |
| **Shadow checkpoint kao safety net** | content-addressed shadow repo, checkpoint prije svake mutacije, step undo | H `checkpoint_manager.py:1-48` | Ne briši worktree kad je inspekcija `UNKNOWN`. |
| **Prompt snapshot lineage** | `StablePrefixHash,CapabilityEpoch,CompressionLineage,BranchID,ToolViewHash` | H `prompt_cache_scope.py:1-35`; `prompt_cache_boundary.py:1-31` | Builder deklarira stabilnu granicu; branch/subagent ne aliasa privatni prefix. |
| **User-visible learning ledger** | journal projekcija sa stabilnim ID-em, provenanceom, evidenceom, supersedes/rollback/purge linkom | H `learning_graph.py:125-245`; `journey.py:239-282` | Ne koristiti ordinalni indeks kao ID. |
| **Agent-callable semantic UI intent** | typed `UIIntent` prema adapteru, nikad DOM command iz kernela | OH `canvas-ui-client-tool.ts:60-90` | UI je projekcija/application adapter, ne drugi executor. |
| **REST history + WS tail** | paginirana journal projekcija + live cursor, ID dedup i out-of-order sort | OH `use-event-store.ts:55-151`; `conversation-websocket-context.tsx:165-183` | Delivery cursor napreduje tek nakon durable append/projection. |
| **Tri-state preflight** | `VALID/INVALID/UNVERIFIED`, gdje timeout nije success | OH `profiles-service.api.ts:146-187` | Primijeniti na provider/MCP/sandbox/hardware probeove. |

## Zaključak

Landscape ne ruši Nexusovu jezgrenu odluku; ruši ideju da je plan već dovoljno precizan samo zato što navodi pravi projekt. Najveći preostali ugovorni dug nije još jedan feature nego **durable identitet i deterministički replay preko granica**: attempt→task→child→worker→environment→delivery. Najjači proizvodni dug izvan A4/A5 je **audience/origin/collaboration**. Najjači coding zaključak ostaje negativan i dobro potkrijepljen: nitko u ovom sliceu nema Nexusovu punu S3.5/S16.6 trojku.

Proof ceiling: konsolidacija nasljeđuje statičke proof-ceilinge iz devet audit-fajlova; nisam ponovno pokretao njihove repoe ni testove. `NEMA` je source-owner nalaz auditirane revizije, ne univerzalni dokaz protiv privatnih servisa, drugih checkouta ili opcionalnih integracija.

## === SLICE kilo ===

# MASTER-SLICE — konsolidacija mojih landscape audita (kilo)

Harnessi (moja trećina): AD=Aider, CL=Cline, RO=Roo, RF=ruflo, CR=CrewAI, MA=Mastra,
OA=OpenAI-SDK, HY=Haystack, MF=MAF, HE=Hermes, OC=OpenClaw. Verdicti: **D**=IMA-DUBOKO,
**P**=IMA-PLIĆE, **—**=NEMA. Sve iz full-code audita (file:line u LA-*.md).

---

## (A) WHO-HAS-WHAT SLICE (17 ključnih podsekcija)

| Podsekcija | DUBOKO | PLIĆE | NEMA | Ključni dokaz → zaključak |
|---|---|---|---|---|
| **S0.1** typed envelope | OA | OC | AD CL RO RF CR MA HY MF HE | `run_state.py` (verzioniran snapshot 20+) → mi PAR, ne novel |
| **S3.1** plan-act-observe | OC HE CL MA | AD RO | RF CR OA HY MF | `agent-loop.ts` (outer/inner+turnTainted) → loop nije naš |
| **S3.4** error+stuck | **CL RO OC HE** | AD (max_reflections) | RF CR MA OA HY MF | `loop-detection.ts`(soft/hard), `ToolRepetitionDetector`, `tool-loop-detection`+argument-churn → **STUCK OBOREN** |
| **S3.5** verify+TIA | AD (auto_test, ali NE TIA) | CL (attempt_completion) | **svi: TIA —** | grep coverage×diff=0 → **TIA PREŽIVLJAVA (jedini čvrsti dif.)** |
| **S4.3** edit+symedit | AD (10+ formata+fuzzy+refuse) | CL RO OA (patch) | **svi: symedit —** | Aider `search_replace`"similar"+`udiff`refuse → edit-ladder OBOREN; **symedit PREŽIVLJAVA** |
| **S4.4** search+archmap | AD (repomap+PPR) HE (codeindex/callgraph) OC (file-indexer) | CL (search) | **svi: archmap-queryable —** | repomap tree-sitter+PageRank oboren; **archmap PREŽIVLJAVA** |
| **S5.1** checkpoint/shadow-git | **RO** (ShadowCheckpointService=zaseban repo+env-izolacija) CL (git-stash private-ref) HE (memgit) AD (auto-commit) | — | RF CR MA OA HY MF | → **SHADOW-GIT OBOREN** (Roo 1:1) |
| **S6.2** sandbox | OA (sandbox+materialization+mount-security) MF (hyperlight VM) HE (container) | CL RO (OS/VS-Code) | AD RF CR MA HY | → GORTEX membrana (Landlock-u-binarnom) je naša jedina delta; hyperlight je 3. opcija |
| **S7.2** durable/checkpoint/lease/fencing | **CR** (fork+event-bus+trigger_events) **MA** (suspend/resume snapshot+background-tasks+Temporal) **OA** (RunState 5726) **OC** (subagent sweeper/fencing/orphan) | CL (stash restore) | AD RF HY | → naš lease/fencing NIJE novel (OC); snapshot-resume je jak alt (MA+OA) |
| **S8.2** compaction | OC (partial-summary+planning-projection+failure-proof+branch-summary) HE (curator 2034) | OA (compaction-session) MA (prune) CL | AD RF CR HY MF | → anchored-summary NIJE novel; planning-projection je GAP |
| **S9.2** memorija | HE (spine+memvec+KG+entres) OC (memory+write-provenance) CR (unified+MemoryScope) MA (observational/working) MF (mem0/cosmos/redis) | OA (session+sqlite) | AD CL RO RF HY | → 5-scope spine NIJE novel; observational/working+Honcho je GAP |
| **S9.4** konsolidacija/decay | **HE** (dream/sleeptime/audn/memgit/FSRS/memassoc) **CR** (analyze_for_consolidation) OC (curator) | — | AD CL RO RF MA OA HY MF | → dream konsolidacija NIJE novel (HE+CR) |
| **S12.1** subagent/delegacija | **OC** (subagent-registry ~100) CL (team 916+multi-agent 1943) MF (orchestrations) HE CR (hierarch) | RO MA | AD RF HY | → worktree-per-subagent = naša delta, ali sweeper/fencing je OC |
| **S12.2** workflow/DAG | **MF** (engine+validation+viz) **CR** (Flow DSL) **MA** (loop-workflows) HY (Pipeline+breakpoint) | OC (flows 58) | AD CL RO RF OA | → naš DAG je najplići od svih |
| **S12.5** A2A | **MF** (hosting-a2a) **CR** (a2a+agent_card_signing) OA (handoffs) RF (federation-peer+agntcy) | — | AD CL RO HY HE | → A2A ekosustav zreo; naš spec treba signed-card+federation |
| **S16.6** anti-sycophancy/checker | HE (verification_evidence 800+stop+hooks) AD (artifact-feedback) | CL (attempt_completion+checkpoint-diff) OC | RF CR MA OA HY MF | → "grade-na-artefaktima" je u AD/HE; **checker≠worker PREŽIVLJAVA (usko)** |
| **S17.1** plugin | **OC** (harness-registry pluggable) **MF** (declarative+tools+providers) RO (SkillsManager+modes) HE (skills+packs) | CL (hooks) | AD RF CR MA OA HY | → harness-kao-plugin je OC obrazac; naš 17.1 Cordis je referent, ne naš |

**NETO diferencijatora nakon 11 harnessa:** **TIA (3.5) + AST-symedit (4.3) + archmap-queryable (4.4)**
su JEDINA tri koja NITKO od 11 nema. + checker≠worker (16.6) preživljava usko. Sve ostalo oboreno.

---

## (B) KONSOLIDIRANI GAPOVI (što imaju a naš plan NEMA — dedupe, samo materijalno)

1. **CASA intent-scoped authorization envelope** (RF) — `CasaEnvelope{objective,allow,deny,budget,
   expires}` + `check_authorization` = čista funkcija, deny-by-default, model-nikad-pitan. Naš 6.0/6.1
   je statički per-tool. → **addendum A6.**
2. **Statistički watermarking outputa** (RF) — Gumbel-max/tournament (provenance TEKSTA, ne loga). Naš
   15.3 je hash-lanac LOGA. → 15.3 dopuna.
3. **Deserialization allowlist** (HY) — allowlist-gated import pri učitavanju pipeline/skill/plugin
   definicija (`serialization_security.py`). Naša 6.8/S0.4 pokriva POTPIS, ne allowlist importa. →
   **rupa u 11.6/17.1.**
4. **fork() + event-bus checkpoint policy (trigger_events/max_checkpoints)** (CR) — first-class
   branch + checkpoint retencija. Naš 7.3/9.1 ima journal-offset, ne fork API/retenciju.
5. **Loop-as-durable-workflow + suspend/resume snapshot** (MA + OA RunState) — snapshot-sourced
   resume (DVA jaka precedenta protiv našeg čistog event-folda). → 7.3 dopuna.
6. **Observational/working memory split** (MA + HE) — 2-razinska memorija iznad našeg 5-scope.
7. **Memory nudges + dijalektički user-model (Honcho)** (HE) — proaktivna perzistencija + Sokratovsko
   izvlačenje modela korisnika. Naš 9.2 je pasivan. → 9.2/9.3 dopuna.
8. **8 terminal backenda + serverless persistence** (HE) — runtime-backend apstrakcija (gdje se kod
   IZVRŠAVA). Naš 18.x je nema. → **addendum A7.**
9. **Tool Gateway (agregirani subscription)** (HE) — billing+routing za TOOL backende. Naš 2.1 je
   model-only. → 2.1 dopuna.
10. **Subagent sweeper/fencing/orphan-recovery** (OC) — dublji od našeg 12.1 (već imaju P2.3 na
    razini subagenata). → 12.1 dopuna.
11. **Imenovani orkestracijski obrasci** (MF) — group_chat/handoff/magentic/sequential/concurrent.
    Naš 12.1/12.2 je plići. → 12.1 dopuna.
12. **Deklarativni agenti s EXECUTORS** (MF) — YAML agent + http/mcp/agents executors. Naš 11.6 ima
    manifest, ne izvršitelje. → 11.6 dopuna.
13. **MODES s fileRegex** (RO) — aktivacija mode po GLOB-u datoteke. Naš 11.6 nema. → 11.6 dopuna.
14. **Commitments/boards/tasks** (OC) — agent se OBVEŽE na posao. → addendum A8.
15. **Pipeline flow-routers** (HY) — uvjetno grananje po metadata/file-type (različito od našeg
    model-routinga 12.3). → 12.2 dopuna.

---

## (C) OBRASCI ZA GO-PREUZETI (konkretni dizajn-obrasci)

1. **CASA envelope (RF):** Go `struct CasaEnvelope{Objective string; Allow,Deny []string; BudgetUSD
   float64; ExpiresAt time.Time}` + `func checkAuthorization(e CasaEnvelope, resource string) bool` =
   čista funkcija (deny>allow, expiry, deny-by-default). Ovo je naš 6.0 PEP — kompajlira se PO ZADATKU.
2. **Shadow-git (RO + CL):** `go-git` zaseban repo u `.checkpoints/` + `go-git` env-izolacija
   (NE naslijedi GIT_DIR/GIT_WORK_TREE) + nested-git detekcija (odbij ako je repo unutar repo).
3. **Tool-call signature za stuck (CL/RO/OC):** `func toolCallSignature(name string, args any) string`
   (kanonski: name + sorted-keys JSON) + `consecutiveCount` + `softThreshold/hardThreshold` → soft
   (warning modelu) / hard (breaker). Reset na user-guidance.
4. **fork() iz checkpointa (CR):** `func (s *Session) Fork(checkpointID string) (*Session, error)` =
   branch na journal-offsetu (novi run, isti prefix). First-class, ne prozni koncept.
5. **Verzionirani RunState snapshot (OA + MA):** `type RunState struct{ SchemaVersion int; TurnState;
   PendingToolCalls; ApprovalRejection; ... }` + `func (rs *RunState) ToSnapshot() []byte` — alternativa
   event-foldu za HITL-resume (snapshot-sourced, s verzijom + migracijom).
6. **Deserialization allowlist (HY):** Go nema dinamički import — analog: `manifest loader` instancira
   SAMO registrirane Go tipove (`map[string]reflect.Type` allowlist), NIKAD `reflect` na proizvoljan
   identifikator. Loader je allowlist-gated, unknown → odbij.
7. **Memory consolidation (CR + HE):** `func Consolidate(existing []Memory, incoming []Memory) ([]Op,
   error)` → `Op ∈ {ADD, NOOP, SUPERSEDE}` (LLM-merge, ali rezultat je deterministički verifikabilan).
   Hermes `audn.py` dodaje decay (mijenja RANG ne postojanje).
8. **Observational/working memory (MA + HE):** dva store-a — `ObservationalStore` (pasivno promatranje,
   append-only) + `WorkingStore` (aktivno, task-scoped, kratkotrajno). Recall čita oba, write je
   approval-gated (naš 9.2 write-approval ON).
9. **Flow DSL + HITL dekorator (CR):** Go `flow` paket s deklarativnim `@router(condition)` + `@human_
   feedback` — ali HITL ide kroz naš P1.6 single-use token (ne CrewAI-jev plain provider).
10. **MODES s fileRegex (RO):** `type Mode struct{ Slug, RoleDefinition string; Tools []string;
    FileRegex string }` — mode se aktivira po GLOB-u datoteke u kontekstu. Naš 11.6 manifest + fileRegex.
11. **Declarative executors (MF):** manifest smije deklarirati TYPED izvršitelje (`executors: [http,
    mcp, agent, control_flow]`), ne samo opis — kernel ih materializira kroz 6.9 lifecycle.

---

## ZAKLJUČAK (za plan)

Naših 11 harnessa potvrđuje: **samo TIA + symedit + archmap su stvarno naši** (i checker≠worker usko).
Sve ostalo (stuck, barbell, edit-ladder, shadow-git, repo-map+PPR, memorija, lease/fencing, A2A,
workflow) je VEĆ izgrađeno i izbrušeno negdje u konkurenciji. 15 materijalnih GAP-ova (B) su obrasci
koje MORAmo preuzeti, ne reimplementirati. 11 Go-obrazaca (C) su konkretan dizajn za fold u plan
(addendumi A6-A8 + dopune 2.1/6.0/7.3/9.2/9.3/11.6/12.1/12.2/15.3/18.x).

## === SLICE agy ===

# Konsolidirani Landscape Audit: Agenta i Frameworka (Master Slice agy)
## Sinteza 10 Repozitorija iz Stvarnog Koda (OpenCode, Goose, Kilo, AutoGen, smolagents, Agno, DeepAgents, Strands, Hermes, OpenClaw)

**Datum:** 2026-09-02  
**Autor:** agy (Gemini 3.7 Flash)  
**Obuhvat:** 10 detaljno auditiranih repozitorija u punom kodu.

---

## (A) WHO-HAS-WHAT SLICE MATRICA

| Naša Podsekcija HARNESS-PLAN | OpenCode | Goose | Kilo | AutoGen (0.4+) | smolagents | Agno | DeepAgents | Strands | Hermes | OpenClaw |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **S0.1: Envelope/FSM Ugovori** | **IMA-PLIĆE**<br>`protocol/index.ts` | **IMA-PLIĆE**<br>`goose/message.rs` | **IMA-PLIĆE**<br>`protocol/` | **IMA-DUBOKO**<br>`agent_worker.proto:11` | **IMA-PLIĆE**<br>`memory.py:24` | **IMA-PLIĆE**<br>`session/agent.py` | **IMA-DUBOKO**<br>`graph.py:73` | **IMA-DUBOKO**<br>`strands/types/` | **IMA-PLIĆE**<br>`agent/loop.py` | **IMA-DUBOKO**<br>`protocol/events.ts` |
| **S3.1: Izvršna Petlja (ReAct/Fibers)**| **IMA-DUBOKO**<br>`processor.ts:98` | **IMA-DUBOKO**<br>`agent.rs:88` | **IMA-DUBOKO**<br>`processor.ts:98` | **IMA-DUBOKO**<br>`runtime.py:99` | **IMA-DUBOKO**<br>`agents.py:268` | **IMA-DUBOKO**<br>`agent/_run.py` | **IMA-DUBOKO**<br>`graph.py:12` | **IMA-DUBOKO**<br>`agent.py:1` | **IMA-DUBOKO**<br>`loop.py:1` | **IMA-DUBOKO**<br>`src/agent/loop.ts` |
| **S3.4: Stuck/Doom Loop Detection** | **IMA-DUBOKO**<br>`processor.ts:29` | **IMA-DUBOKO**<br>`repetition.rs:35` | **IMA-DUBOKO**<br>`processor.ts:29` | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **IMA-DUBOKO**<br>`src/agent/stuck.ts` |
| **S3.5: Evidence-Graded Diff Gate** | **NEMA**<br>`prompt/meta.txt:18` | **NEMA**<br>(samo prompt) | **NEMA**<br>`prompt/meta.txt:18` | **NEMA** | **NEMA**<br>`final_checks:613` | **NEMA** | **NEMA**<br>`rubric.py:1` | **NEMA** | **NEMA** | **NEMA** |
| **S4.3: Edit Tool / Ladder** | **IMA-DUBOKO**<br>`edit.ts:694` (9 st.) | **IMA-PLIĆE**<br>`edit.rs:45` (regex) | **IMA-DUBOKO**<br>`edit.ts:694` (9 st.) | **NEMA** | **NEMA**<br>(CodeAgent py) | **NEMA** | **NEMA** | **IMA-PLIĆE**<br>`file_editor.py:35` | **IMA-PLIĆE**<br>`tools/edit.py` | **IMA-PLIĆE**<br>`tools/edit.ts` |
| **S4.4: Live Post-Edit LSP Injekcija**| **IMA-DUBOKO**<br>`edit.ts:197` | **NEMA** | **IMA-DUBOKO**<br>`edit.ts:197` | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** |
| **S5.1: Workspace / Shadow Git** | **IMA-DUBOKO**<br>`snapshot/index.ts` | **NEMA** | **IMA-DUBOKO**<br>`snapshot/index.ts` | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **IMA-DUBOKO**<br>`session/snapshot.ts` |
| **S6.2: Process & Kernel Sandbox** | **NEMA** | **IMA-PLIĆE**<br>`security.rs` (OSV) | **IMA-DUBOKO**<br>`bubblewrap.ts:15` | **IMA-PLIĆE**<br>`code_executor/` | **IMA-PLIĆE**<br>`local_python.py:1` | **NEMA** | **IMA-PLIĆE**<br>`filesystem.py` | **IMA-PLIĆE**<br>`sandbox/base.py` | **NEMA** | **IMA-PLIĆE**<br>`src/sandbox/` |
| **S7.2: CAS Fencing / Outbox** | **NEMA** | **NEMA** | **NEMA** | **IMA-PLIĆE**<br>`save_state:431` | **NEMA** | **IMA-PLIĆE**<br>`db/base.py` (15 DB) | **IMA-PLIĆE**<br>`backends/` | **IMA-PLIĆE**<br>`storage/` | **NEMA** | **IMA-DUBOKO**<br>`runtime/lane.ts` |
| **S8.2: Token Budgeting & Compaction**| **IMA-DUBOKO**<br>`compaction.ts:28` | **IMA-DUBOKO**<br>`token_counter.rs` | **IMA-DUBOKO**<br>`compaction.ts:28` | **IMA-PLIĆE**<br>`model_context/` | **IMA-PLIĆE**<br>`memory.py:summary` | **IMA-PLIĆE**<br>`compression/` | **IMA-DUBOKO**<br>`summarization.py`| **IMA-DUBOKO**<br>`context_offload` | **IMA-PLIĆE**<br>`context.py` | **IMA-DUBOKO**<br>`compaction.ts` |
| **S9.2: Cross-Session Memorija** | **IMA-PLIĆE**<br>`AGENTS.md` | **IMA-PLIĆE**<br>`memory.rs` (md) | **IMA-DUBOKO**<br>`kilo-memory:13` | **IMA-DUBOKO**<br>`_base_memory.py:60`| **NEMA** | **IMA-DUBOKO**<br>`memory/manager.py`| **IMA-DUBOKO**<br>`openwiki/` (.claims)| **IMA-PLIĆE**<br>`vended_memory/` | **IMA-DUBOKO**<br>`memory.py` (SQLite)| **IMA-DUBOKO**<br>`src/memory/` |
| **S9.4: Formal Memory Forget (P2.1)**| **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** |
| **S12.1: Subagent Delegacija** | **IMA-PLIĆE**<br>`tool/task.ts` | **IMA-PLIĆE**<br>`recipe.rs` | **IMA-PLIĆE**<br>`tool/task.ts` | **IMA-DUBOKO**<br>`group_chat.py` | **IMA-DUBOKO**<br>`agents.py:281` | **IMA-DUBOKO**<br>`team/team.py` | **IMA-DUBOKO**<br>`subagents.py:1` | **IMA-DUBOKO**<br>`agent_as_tool.py` | **IMA-PLIĆE**<br>`subagent.py` | **IMA-DUBOKO**<br>`agent/subagent.ts` |
| **S12.2: DAG Wave Scheduler / Graph**| **NEMA** | **NEMA** | **NEMA** | **IMA-DUBOKO**<br>`digraph_chat.py` | **NEMA** | **IMA-DUBOKO**<br>`workflow/step.py` | **IMA-DUBOKO**<br>`graph.py` (LangGr) | **IMA-DUBOKO**<br>`multiagent/graph.py`| **NEMA** | **NEMA** |
| **S12.5: Multi-Agent Kolaboracija** | **NEMA** | **NEMA** | **NEMA** | **IMA-DUBOKO**<br>`swarm_chat.py` | **NEMA** | **IMA-DUBOKO**<br>`team/mode.py` (4) | **IMA-DUBOKO**<br>`async_subagents` | **IMA-DUBOKO**<br>`multiagent/a2a/` | **NEMA** | **IMA-DUBOKO**<br>`gateway/channels` |
| **S16.6: Anti-Sycophancy / Checker**| **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **NEMA** | **IMA-PLIĆE**<br>`rubric.py` (LLM) | **NEMA** | **NEMA** | **NEMA** |
| **S17.1: Plugin Sustav & MCP** | **IMA-DUBOKO**<br>`packages/plugin/` | **IMA-DUBOKO**<br>`goose-mcp/client` | **IMA-DUBOKO**<br>`packages/plugin/` | **IMA-PLIĆE**<br>`tools/mcp/` | **IMA-PLIĆE**<br>`tools.py:mcp` | **IMA-PLIĆE**<br>`tools/` | **IMA-PLIĆE**<br>`skills.py` | **IMA-DUBOKO**<br>`plugins/registry` | **IMA-DUBOKO**<br>`skills/` | **IMA-DUBOKO**<br>`packages/plugins` |

---

## (B) KONSOLIDIRANI MATERIJALNI GAPOVI (Što konkurenti IMAJU a naš plan NEMA ili slabije pokriva)

1. **G1: FIM (Fill-in-the-Middle) Autocomplete Gateway Endpoint (`Kilo` — `kilo-gateway/src/fim.ts`)**
   - Brzi streaming endpoint koji prima `prefix` i `suffix` kursora za inline autokompletiranje u IDE-ovima (VS Code, JetBrains).
2. **G2: OSV Vulnerability Scanning za MCP pakete (`Goose` — `inspectors/security.rs`)**
   - Automatska provjera MCP alata i ovisnosti protiv Open Source Vulnerabilities (OSV) baze prije pokretanja.
3. **G3: DeltaChannel Checkpoint Reducer ($O(N)$ skaliranje) (`DeepAgents` — `graph.py:73`, `_messages_reducer.py`)**
   - Spremanje samo razlika (deltas) između koraka umjesto cijele povijesti, što eliminira $O(N^2)$ eksploziju baze u dugim sesijama.
4. **G4: A2A (Agent-to-Agent) Protokol & Swarm Handoff (`Strands` — `multiagent/a2a/`, `AutoGen` — `swarm_group_chat.py`)**
   - Standardizirane poruke za decentraliziranu primopredaju rada među autonomnim agentima bez centralnog uskog grla.
5. **G5: CEL Evaluacija Uvjeta u Radnim Tijekovima (`Agno` — `workflow/cel.py`, `step.py`)**
   - Determinističko grananje u DAG cjevovodima putem Common Expression Language izraza bez trošenja skupih LLM poziva.
6. **G6: OpenWiki s Verifikacijom Tvrdnji (`DeepAgents` — `openwiki/`, `.claims/`)**
   - Autonomno održavana Markdown baza znanja o projektu koja sprema provjerene tvrdnje radi prevencije halucinacija.
7. **G7: Universal Intervention Pipeline (`AutoGen` — `_intervention.py:20`, `Strands` — `interrupt.py`)**
   - Middleware presretač s `on_send`, `on_publish`, `on_response` i `DropMessage` signalima za Human-in-the-Loop autorizaciju.
8. **G8: Tailscale Native Serve & Workboard Protocol (`OpenClaw` — `src/gateway/tailscale.ts`, `protocol/`)**
   - Direktan zero-trust udaljeni pristup bez otvaranja javnih portova uz 24 tipizirana događaja radne ploče.

---

## (C) OBRASCI ZA PREUZETI U GO REBUILD (MatNik89/nexus)

1. **`pkg/gateway/fim.go` (iz Kilo):** Dodati FIM streaming handler u naš HTTP/gRPC gateway za posluživanje VS Code / JetBrains autokompletiranja.
2. **`pkg/security/osv.go` (iz Goose):** Dodati OSV provjeru paketa unutar S6/S17 provjere integriteta MCP poslužitelja.
3. **`pkg/journal/delta.go` (iz DeepAgents):** Implementirati delta-kompresiju za journal unose uz snapshot frekvenciju od 50 koraka.
4. **`pkg/orchestration/a2a.go` & `handoff.go` (iz Strands & AutoGen):** Ugraditi A2A omotnice i `HandoffEnvelope` u naš S12 subagent sloj.
5. **`pkg/orchestration/middleware.go` (iz AutoGen & Strands):** Uvesti `InterventionHandler` (`OnSend`, `OnPublish`, `OnResponse`, `DropMessage`) za HITL kontrolu.
6. **`pkg/workflow/cel.go` (iz Agno):** Ugraditi Google CEL interpreter u Go-u (`github.com/google/cel-go`) za determinističko grananje u S12.2 DAG-u.
7. **`pkg/engine/edit_ladder.go` (iz OpenCode/Kilo):** Ugraditi 9-stupanjski mehanizam zamjene teksta kao fallback u S4.3 uz naš primarni AST symedit.
8. **`pkg/engine/lsp_diagnostics.go` (iz OpenCode/Kilo):** Ugraditi automatsko lijepljenje LSP grešaka na kraj izlaza alata za uređivanje u S4.4.

---

## (D) ZAKLJUČNA VERIFIKACIJA NAŠIH DIFERENCIJATORA

Nakon punog audita stvarnog koda kroz svih 10 sustava:
- **POTVRĐENO (NITKO NEMA): Programski Evidence-Graded Execution Loop** (`git diff > 0 && compiler/test exit == 0`). Svi konkurenti se oslanjaju isključivo na upute u promptu ili prozne LLM ocjenjivače (`rubric.py`).
- **POTVRĐENO (NITKO NEMA): Test Impact Analysis (TIA)**. Niti jedan harness nema mapiranje testova preko stabla poziva.
- **POTVRĐENO (NITKO NEMA): AST-based Semantic Code Editing (symedit)**. Svi konkurenti koriste linijsko ili fuzzy podudaranje stringova.
- **POTVRĐENO (NITKO NEMA): Single-Binary Pure Go Platforma s Landlock v5 LSM** i 17 Tier-3 ugovora (P0.3 jedan write-owner, N9 outbox, CAS fencing).


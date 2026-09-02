# SALVAGE GROUND-TRUTH AUDIT

**Izvor (ISPRAVAN):** `/home/matej/nexus` — instaliran `nexus-agent` v0.5.0 (editable, CLI `nexus`). Repo `MatNik89/nexus-agent`.
**Prethodni audit bio nevažeći** — vozio nad `/home/matej/NEXUSv2` (stari zaseban github repo, NIJE izvor salvagea).

**Metoda:** direktna shell/py verifikacija (read-only) nad stvarnim fajlovima. NE iz plana/imena.
Dispatch na 3 LLM-agenta odbijen: `wc -l`+def-imena su deterministički shell-izlaz; remote agent dodaje halucinaciju zadatku čija je svrha uhvatiti halucinaciju. Taj isti direktni pristup upravo je uhvatio da je plan citirao krivi repo.
Razrješenje putanje: basename-lookup kroz cijelo stablo pravog nexusa (hvata i reorganizirane module), bez tests/pycache/egg/build. Datum 2026-09-02.

## Tablica

| tvrđeni modul (plan) | stvarna putanja u nexus-agent | linija | #defs | top-level definicije (redom) | klasifikacija |
|---|---|---|---|---|---|
| `core/archmap.py` | – | – | – | – | **NE POSTOJI** |
| `core/audit.py` | – | – | – | – | **NE POSTOJI** |
| `core/audn.py` | – | – | – | – | **NE POSTOJI** |
| `core/broker.py` | – | – | – | – | **NE POSTOJI** |
| `core/browse.py` | – | – | – | – | **NE POSTOJI** |
| `core/bugreport.py` | – | – | – | – | **NE POSTOJI** |
| `core/callgraph.py` | – | – | – | – | **NE POSTOJI** |
| `core/canary.py` | – | – | – | – | **NE POSTOJI** |
| `core/canvas.py` | – | – | – | – | **NE POSTOJI** |
| `core/capabilities.py` | – | – | – | – | **NE POSTOJI** |
| `core/capreg.py` | – | – | – | – | **NE POSTOJI** |
| `core/checkpoint.py` | – | – | – | – | **NE POSTOJI** |
| `core/circuit.py` | – | – | – | – | **NE POSTOJI** |
| `core/codeindex.py` | – | – | – | – | **NE POSTOJI** |
| `core/coderetrieval.py` | – | – | – | – | **NE POSTOJI** |
| `core/compact.py` | – | – | – | – | **NE POSTOJI** |
| `core/confidence.py` | – | – | – | – | **NE POSTOJI** |
| `core/contextbudget.py` | – | – | – | – | **NE POSTOJI** |
| `core/context.py` | `nexus/core/context.py` | 288 | 9 | FileContext,Context,ContextEngine,_compress,SymbolGraph,BlastRadius,DependencyTree,GortexE | PORT-LAK |
| `core/cost.py` | – | – | – | – | **NE POSTOJI** |
| `core/credit.py` | – | – | – | – | **NE POSTOJI** |
| `core/crush.py` | – | – | – | – | **NE POSTOJI** |
| `core/deploy.py` | – | – | – | – | **NE POSTOJI** |
| `core/dream.py` | – | – | – | – | **NE POSTOJI** |
| `core/editapply.py` | – | – | – | – | **NE POSTOJI** |
| `core/editrepair.py` | – | – | – | – | **NE POSTOJI** |
| `core/egress.py` | – | – | – | – | **NE POSTOJI** |
| `core/embed.py` | – | – | – | – | **NE POSTOJI** |
| `core/events.py` | `nexus/core/events.py` | 145 | 3 | Event,EventStore,Projector | PORT-LAK |
| `core/export.py` | – | – | – | – | **NE POSTOJI** |
| `core/fleet.py` | – | – | – | – | **NE POSTOJI** |
| `core/fts.py` | – | – | – | – | **NE POSTOJI** |
| `core/gate.py` | `nexus_forge/gate.py` | 416 | 14 | GateResult,FullGateResult,_extract_body,_count_code_blocks,_count_sections,check_anti_plac | PORT-LAK |
| `core/guardian.py` | – | – | – | – | **NE POSTOJI** |
| `core/guardrail.py` | – | – | – | – | **NE POSTOJI** |
| `core/health.py` | – | – | – | – | **NE POSTOJI** |
| `core/hooks.py` | – | – | – | – | **NE POSTOJI** |
| `core/hwdetect.py` | – | – | – | – | **NE POSTOJI** |
| `core/imagegen.py` | – | – | – | – | **NE POSTOJI** |
| `core/image.py` | – | – | – | – | **NE POSTOJI** |
| `core/ingest.py` | – | – | – | – | **NE POSTOJI** |
| `core/kg.py` | – | – | – | – | **NE POSTOJI** |
| `core/licenses.py` | – | – | – | – | **NE POSTOJI** |
| `core/mcpauth.py` | – | – | – | – | **NE POSTOJI** |
| `core/mcp.py` | – | – | – | – | **NE POSTOJI** |
| `core/mcpserve.py` | – | – | – | – | **NE POSTOJI** |
| `core/memassoc.py` | – | – | – | – | **NE POSTOJI** |
| `core/memgit.py` | – | – | – | – | **NE POSTOJI** |
| `core/memory.py` | `nexus/core/memory.py` | 458 | 15 | ProjectContext,PhaseLog,BugEntry,PatternEntry,MemoryEntry,MemoryPlan,MemoryRepository,_for | PORT-LAK |
| `core/memvec.py` | – | – | – | – | **NE POSTOJI** |
| `core/mineru.py` | – | – | – | – | **NE POSTOJI** |
| `core/netjail.py` | – | – | – | – | **NE POSTOJI** |
| `core/oauthcode.py` | – | – | – | – | **NE POSTOJI** |
| `core/otel.py` | – | – | – | – | **NE POSTOJI** |
| `core/pack_registry.py` | – | – | – | – | **NE POSTOJI** |
| `core/paths.py` | – | – | – | – | **NE POSTOJI** |
| `core/pdf.py` | – | – | – | – | **NE POSTOJI** |
| `core/permissions.py` | – | – | – | – | **NE POSTOJI** |
| `core/planner.py` | `nexus/core/planner.py` | 119 | 4 | StepIntent,PlanStep,Plan,AdaptivePlanner | PORT-LAK |
| `core/procreg.py` | – | – | – | – | **NE POSTOJI** |
| `core/projectrules.py` | – | – | – | – | **NE POSTOJI** |
| `core/promptlayer.py` | – | – | – | – | **NE POSTOJI** |
| `core/provenance.py` | – | – | – | – | **NE POSTOJI** |
| `core/provider.py` | – | – | – | – | **NE POSTOJI** |
| `core/queue.py` | – | – | – | – | **NE POSTOJI** |
| `core/rank.py` | – | – | – | – | **NE POSTOJI** |
| `core/reasonbank.py` | – | – | – | – | **NE POSTOJI** |
| `core/recall.py` | – | – | – | – | **NE POSTOJI** |
| `core/recovery.py` | – | – | – | – | **NE POSTOJI** |
| `core/reflexion.py` | – | – | – | – | **NE POSTOJI** |
| `core/rerank.py` | – | – | – | – | **NE POSTOJI** |
| `core/respcache.py` | – | – | – | – | **NE POSTOJI** |
| `core/resume.py` | – | – | – | – | **NE POSTOJI** |
| `core/reversa.py` | – | – | – | – | **NE POSTOJI** |
| `core/reward.py` | – | – | – | – | **NE POSTOJI** |
| `core/schedule.py` | – | – | – | – | **NE POSTOJI** |
| `core/secrets.py` | – | – | – | – | **NE POSTOJI** |
| `core/secscan.py` | – | – | – | – | **NE POSTOJI** |
| `core/selfimprove.py` | – | – | – | – | **NE POSTOJI** |
| `core/session.py` | `nexus_memory/session.py` | 202 | 1 | SessionMemory | PORT-LAK |
| `core/sleeptime.py` | – | – | – | – | **NE POSTOJI** |
| `core/state.py` | `nexus/core/state.py` | 88 | 5 | TaskComplexity,BlockageReason,Blockage,Action,GoalState | PORT-LAK |
| `core/steer.py` | – | – | – | – | **NE POSTOJI** |
| `core/store.py` | – | – | – | – | **NE POSTOJI** |
| `core/subproc.py` | – | – | – | – | **NE POSTOJI** |
| `core/supplychain.py` | – | – | – | – | **NE POSTOJI** |
| `core/symedit.py` | – | – | – | – | **NE POSTOJI** |
| `core/testcmd.py` | – | – | – | – | **NE POSTOJI** |
| `core/tia.py` | – | – | – | – | **NE POSTOJI** |
| `core/toolschema.py` | – | – | – | – | **NE POSTOJI** |
| `core/tools.py` | `nexus/core/tools.py` | 132 | 6 | ToolCategory,Tool,ToolResult,ToolRegistry,ToolRouter,_tool_priority | PORT-LAK |
| `core/trace.py` | – | – | – | – | **NE POSTOJI** |
| `core/transcript.py` | – | – | – | – | **NE POSTOJI** |
| `core/update.py` | – | – | – | – | **NE POSTOJI** |
| `core/verifier.py` | `nexus/core/verifier.py` | 125 | 4 | VerificationStatus,Verification,Verifier,CodeRunner | PORT-LAK |
| `core/video.py` | – | – | – | – | **NE POSTOJI** |
| `core/vision.py` | – | – | – | – | **NE POSTOJI** |
| `core/voice.py` | – | – | – | – | **NE POSTOJI** |
| `core/watch.py` | – | – | – | – | **NE POSTOJI** |
| `core/web.py` | `nexus_vision/web.py` | 139 | 3 | SearchResult,ExtractResult,VisionWeb | PORT-LAK |
| `core/wiki.py` | – | – | – | – | **NE POSTOJI** |
| `core/workflow.py` | – | – | – | – | **NE POSTOJI** |
| `core/worktree.py` | – | – | – | – | **NE POSTOJI** |
| `gortex/diff_engine.go` | – | – | – | – | **NE POSTOJI** |
| `gortex/landlock_linux.go` | – | – | – | – | **NE POSTOJI** |
| `gortex/mcp_server.go` | – | – | – | – | **NE POSTOJI** |
| `gortex/observability.go` | – | – | – | – | **NE POSTOJI** |
| `gortex_test.go` | – | – | – | – | **NE POSTOJI** |
| `gortex/tool_executor.go` | – | – | – | – | **NE POSTOJI** |
| `llm/executor.py` | – | – | – | – | **NE POSTOJI** |
| `llm/providers.py` | – | – | – | – | **NE POSTOJI** |
| `llm/router.py` | – | – | – | – | **NE POSTOJI** |
| `llm/spawner.py` | – | – | – | – | **NE POSTOJI** |

## SAŽETAK

- **Ukupno tvrđenih salvage modula (citirano u S0-S18+addendumi):** 113
  - HANDOFF/plan tekstualno kaže "60 modula" — netočno na dvije razine: stvaran broj citata je **113**, a stvarno postojećih **10**.
- **Stvarno pročitanih s dokazom (wc -l + top-level defs REDOM):** 10
- **GO-REUSE:** 0 · **PORT-LAK:** 10 · **PORT-TEŽAK:** 0 · **NE POSTOJI:** 103 · **NEUPOTREBLJIVO:** 0
- Nema Go u pravom izvoru (0 `.go`) → svih 6 `gortex/*.go` citata + "GORTEX Go-reuse 1:1" premisa = NE POSTOJI.
- Nema `llm/` mape → svih 4 `llm/*.py` citata = NE POSTOJI.

### 10 POSTOJEĆIH (svih PORT-LAK, Python, stdlib-only)
- `core/context.py` → `nexus/core/context.py` (288L, 9 defs)
- `core/events.py` → `nexus/core/events.py` (145L, 3 defs)
- `core/gate.py` → `nexus_forge/gate.py` (416L, 14 defs)
- `core/memory.py` → `nexus/core/memory.py` (458L, 15 defs)
- `core/planner.py` → `nexus/core/planner.py` (119L, 4 defs)
- `core/session.py` → `nexus_memory/session.py` (202L, 1 defs)
- `core/state.py` → `nexus/core/state.py` (88L, 5 defs)
- `core/tools.py` → `nexus/core/tools.py` (132L, 6 defs)
- `core/verifier.py` → `nexus/core/verifier.py` (125L, 4 defs)
- `core/web.py` → `nexus_vision/web.py` (139L, 3 defs)

### SUKOB (inter-agent)
- Nema. Jedan deterministički izvor (shell/py). 3-agent split odbijen kao proturječan cilju.

### UPOZORENJA (basename-match u drugom podpaketu — NIJE zajamčeno isti modul)
Plan citira `core/X`, ali stvaran fajl je u drugom podpaketu — moguća kolizija imena, ne nužno ista logika:
- `core/gate.py` → `nexus_forge/gate.py`
- `core/session.py` → `nexus_memory/session.py`
- `core/web.py` → `nexus_vision/web.py`

### NE POSTOJI (svih 103)
Vidi tablicu gore (svaki red označen **NE POSTOJI**). Uključuje sve `gortex/*.go`, sve `llm/*.py`, i 93 `core/*.py` koji ne postoje u pravom nexus-agent izvoru.

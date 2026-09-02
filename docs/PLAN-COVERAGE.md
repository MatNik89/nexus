# PLAN-COVERAGE — provjera pokrivenosti HARNESS-PLAN.md (S0-S18)

**NE mijenja HARNESS-PLAN.md.** Salvage-postojanje mjereno protiv ISPRAVNOG izvora `/home/matej/nexus` (nexus-agent).
Full-code auditano = podsekcija ima file:line dokaz u MASTER-LANDSCAPE (17 podsekcija); ostalo = kandidati na README/licenca razini.
**IZVOR (loose)** = što plan tvrdi (bilo koji imenovan kandidat/salvage/dizajn spašava). **IZVOR (strogo)** = neauditan kandidat NE spašava (tvoja uputa: ne skrivaj rupe iza neprovjerenih kandidata).

| ID | naslov | KAND (#) | full-code aud | SALVAGE (modul=postoji) | IZVOR loose | IZVOR strogo |
|---|---|---|---|---|---|---|
| S0.1 | typed schema (MEHANIZAM): | 3 | DA | core/events.py=DA | OBOJE | OBOJE |
| S0.2 | state-machine (MEHANIZAM): | 2 | NE | core/state.py=DA | OBOJE | SALVAGE |
| S0.3 | capability negotiation (MEHANIZAM): | 2 | NE | llm/providers.py=NE, core/capabilities.py=NE | MERGE | PIŠEMO IZNOVA |
| S0.4 | verzioniranje/migracija (MEHANIZAM): | 1 | NE | core/store.py=NE | MERGE | PIŠEMO IZNOVA |
| S0.5 | capability-flag closure resolver (MEHANI | 6 | NE | core/capabilities.py=NE | MERGE | PIŠEMO IZNOVA |
| S1.1 | config (MEHANIZAM): | 1 | NE | core/paths.py=NE | MERGE | PIŠEMO IZNOVA |
| S1.2 | cross-platform paths+procesi (MEHANIZAM, | 1 | NE | core/paths.py=NE, core/subproc.py=NE | MERGE | PIŠEMO IZNOVA |
| S1.3 | strukturirano logiranje (MEHANIZAM, OTel | 4 | NE | core/trace.py=NE, core/otel.py=NE | MERGE | PIŠEMO IZNOVA |
| S2.1 | multi-provider + 3 AUTH-MODA (MEHANIZAM) | 3 | NE | llm/providers.py=NE | MERGE | PIŠEMO IZNOVA |
| S2.2 | fallback (MEHANIZAM, TANKI — NE retry pe | 1 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S2.3 | structured output (MEHANIZAM): | 1 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S2.4 | lokalni + hardware-fit (ADAPTER+MEHANIZA | 2 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S2.5 | cost tracking (MEHANIZAM): | 2 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S2.6 | model routing (MEHANIZAM, jezgra owner): | 1 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S3.1 | plan-act-observe (MEHANIZAM, coding-jezg | 5 | DA | nema | MERGE | MERGE(aud) |
| S3.2 | streaming (MEHANIZAM): | 3 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S3.3 | cancel/turn (MEHANIZAM): | 2 | NE | core/steer.py=NE, core/circuit.py=NE | MERGE | PIŠEMO IZNOVA |
| S3.4 | error recovery (MEHANIZAM, coding-jezgra | 3 | DA | nema | MERGE | MERGE(aud) |
| S3.5 | deterministička verifikacija + TIA (MEHA | 2 | DA | nema | MERGE | MERGE(aud) |
| S3.6 | run-trigger-plane (MEHANIZAM admission=j | 3 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S4.1 | tool-registry (MEHANIZAM): | 2 | NE | core/tools.py=DA | OBOJE | SALVAGE |
| S4.2 | shell/exec (MEHANIZAM, coding): | 2 | NE | core/subproc.py=NE, gortex/tool_executor.go=NE | MERGE | PIŠEMO IZNOVA |
| S4.3 | edit formati (MEHANIZAM, PRESUDNI coding | 3 | DA | gortex/diff_engine.go=NE | MERGE | MERGE(aud) |
| S4.4 | pretraga koda (MEHANIZAM+ADAPTER, coding | 0 | DA | nema | PIŠEMO IZNOVA | MERGE(aud) |
| S4.5 | web/browser (ADAPTER, `browser` flag): | 5 | NE | core/browse.py=NE | MERGE | PIŠEMO IZNOVA |
| S4.6 | MCP klijent (MEHANIZAM+ADAPTER): | 3 | NE | core/mcp.py=NE, gortex/mcp_server.go=NE | MERGE | PIŠEMO IZNOVA |
| S4.7 | tool-exposure-budget (MEHANIZAM, jezgra) | 1 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S5.1 | checkpoint/rollback (MEHANIZAM): | 3 | DA | core/checkpoint.py=NE, core/memgit.py=NE | MERGE | MERGE(aud) |
| S5.2 | workspace izolacija (MEHANIZAM+ADAPTER): | 2 | NE | core/worktree.py=NE | MERGE | PIŠEMO IZNOVA |
| S5.3 | dirty/atomic/conflict/provenance (MEHANI | 3 | NE | core/provenance.py=NE, gortex/diff_engine.go=NE | MERGE | PIŠEMO IZNOVA |
| S6.0 | trust/PEP (MEHANIZAM): | 2 | NE | core/permissions.py=NE, core/provenance.py=NE | MERGE | PIŠEMO IZNOVA |
| S6.1 | permission gating (MEHANIZAM): | 1 | NE | core/permissions.py=NE, core/hooks.py=NE | MERGE | PIŠEMO IZNOVA |
| S6.2 | sandbox (MEHANIZAM, per-OS build-tag, GO | 2 | DA | nema | MERGE | MERGE(aud) |
| S6.3 | egress (MEHANIZAM): | 2 | NE | core/egress.py=NE, core/netjail.py=NE | MERGE | PIŠEMO IZNOVA |
| S6.4 | secrets (MEHANIZAM): | 0 | NE | core/secrets.py=NE, core/broker.py=NE | PIŠEMO IZNOVA | PIŠEMO IZNOVA |
| S6.5 | injection-obrana + canary (MEHANIZAM): | 0 | NE | core/provenance.py=NE, core/guardrail.py=NE, core/guardian.p | PIŠEMO IZNOVA | PIŠEMO IZNOVA |
| S6.6 | authn/tenant (`service` flag): | 1 | NE | gateway/base.py=DA, core/oauthcode.py=NE | OBOJE | SALVAGE |
| S6.8 | supply-chain (`extensions` flag): | 2 | NE | core/supplychain.py=NE | MERGE | PIŠEMO IZNOVA |
| S6.9 | lifecycle enforcement (MEHANIZAM, jezgra | 2 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S7.1 | timeout/cancel/retry-taksonomija (MEHANI | 1 | NE | core/recovery.py=NE | MERGE | PIŠEMO IZNOVA |
| S7.2 | idempotency/budgets/lease/fencing (MEHAN | 1 | DA | core/queue.py=NE, core/circuit.py=NE, core/fleet.py=NE | MERGE | MERGE(aud) |
| S7.3 | crash-recovery + durable-delivery (MEHAN | 2 | NE | core/resume.py=NE, core/store.py=NE | MERGE | PIŠEMO IZNOVA |
| S8.1 | window-budžet (MEHANIZAM): | 2 | NE | core/contextbudget.py=NE, core/context.py=DA | OBOJE | SALVAGE |
| S8.2 | kompakcija (MEHANIZAM, jezgra): | 1 | DA | core/compact.py=NE | MERGE | MERGE(aud) |
| S8.3 | repo-mapa (MEHANIZAM, coding): | 3 | NE | core/archmap.py=NE, core/rank.py=NE, core/codeindex.py=NE, c | MERGE | PIŠEMO IZNOVA |
| S8.4 | cache (MEHANIZAM, dva sloja): | 3 | NE | core/=DA, core/promptlayer.py=NE | OBOJE | SALVAGE |
| S8.5 | observation-pruning (MEHANIZAM): | 1 | NE | core/crush.py=NE | MERGE | PIŠEMO IZNOVA |
| S9.1 | sesije (JEZGRA): | 2 | NE | core/session.py=DA, core/resume.py=NE | OBOJE | SALVAGE |
| S9.2 | perzistentna memorija (`memorija`): | 1 | DA | core/memory.py=DA | OBOJE | OBOJE |
| S9.3 | učenje-iz-iskustva (`memorija`): | 2 | NE | core/reflexion.py=NE, core/reasonbank.py=NE | MERGE | PIŠEMO IZNOVA |
| S9.4 | konsolidacija/decay (`memorija`, veliki  | 1 | DA | core/dream.py=NE | MERGE | MERGE(aud) |
| S10.1 | ingestion (ADAPTER, READ-ONLY): | 4 | NE | core/ingest.py=NE, core/pdf.py=NE, core/embed.py=NE, core/re | MERGE | PIŠEMO IZNOVA |
| S10.4 | GraphRAG (`graph-retrieval`): | 5 | NE | core/kg.py=NE | MERGE | PIŠEMO IZNOVA |
| S10.6 | retrieval-auth (MEHANIZAM jezgra + ADAPT | 1 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S10.7 | quality-gate (`vector-retrieval`, MEHANI | 1 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S11.1 | assembly (MEHANIZAM, jezgra): | 3 | NE | core/promptlayer.py=NE | MERGE | PIŠEMO IZNOVA |
| S11.2 | skills (DATA): | 2 | NE | core/reversa.py=NE | MERGE | PIŠEMO IZNOVA |
| S11.3 | projektne-instrukcije: | 6 | NE | nema | MERGE | RUPA |
| S11.5 | skill-lifecycle/governance (MEHANIZAM je | 1 | NE | managers/skill_=NE | MERGE | PIŠEMO IZNOVA |
| S11.6 | role→agent→skill data-manifest (MEHANIZA | 3 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S12.1 | subagenti/delegacija (MEHANIZAM): | 2 | DA | managers/subagent_orchestrator.py=NE | MERGE | MERGE(aud) |
| S12.2 | workflow/DAG (MEHANIZAM): | 2 | DA | core/workflow.py=NE | MERGE | MERGE(aud) |
| S12.4 | HITL (MEHANIZAM): | 2 | NE | core/gate.py=DA | OBOJE | SALVAGE |
| S12.5 | A2A interop (MEHANIZAM+ADAPTER, A2A spec | 1 | DA | nema | MERGE | MERGE(aud) |
| S12.6 | council (MEHANIZAM, P2.7): | 2 | NE | managers/council.py=NE | MERGE | PIŠEMO IZNOVA |
| S13.1 | vision (`vision`): | 6 | NE | core/vision.py=NE, core/imagegen.py=NE, core/canvas.py=NE | MERGE | RUPA |
| S13.3 | voice (`voice`): | 3 | NE | core/voice.py=NE | MERGE | RUPA |
| S13.4 | video (`video`): | 4 | NE | core/video.py=NE | MERGE | RUPA |
| S13.5 | dokument-mutacija (`documents`, INSTANCA | 1 | NE | core/pdf.py=NE | MERGE | PIŠEMO IZNOVA |
| S14.1 | CLI/TUI (jezgra minimum): | 3 | NE | tui/=NE, gateway/dashboard.py=NE | MERGE | PIŠEMO IZNOVA |
| S14.4 | IDE/ACP: | 3 | NE | nema | MERGE | RUPA |
| S14.5 | messaging/channel-gateway (`channels`): | 1 | NE | gateway/=NE | MERGE | PIŠEMO IZNOVA |
| S15.1 | tracing (MEHANIZAM): | 1 | NE | core/trace.py=NE, core/otel.py=NE | MERGE | PIŠEMO IZNOVA |
| S15.3 | transcript/audit (MEHANIZAM): | 1 | NE | core/audit.py=NE, core/transcript.py=NE | MERGE | PIŠEMO IZNOVA |
| S15.5 | semantic-agent-health (MEHANIZAM jezgra) | 1 | NE | core/health.py=NE, core/circuit.py=NE | MERGE | PIŠEMO IZNOVA |
| S16.1 | task-benchmark (MEHANIZAM): | 2 | NE | nema | MERGE | PIŠEMO IZNOVA |
| S16.4 | feedback-flywheel + credit-ledger (MEHAN | 1 | NE | core/reward.py=NE, core/credit.py=NE | MERGE | PIŠEMO IZNOVA |
| S16.5 | trajectory-export (`destilacija`): | 3 | NE | nema | MERGE | RUPA |
| S16.6 | anti-sycophancy/neovisna-provjera (MEHAN | 3 | DA | core/confidence.py=NE, managers/council.py=NE | MERGE | MERGE(aud) |
| S17.1 | plugin-sustav (`extensions`): | 6 | DA | core/update.py=NE | MERGE | MERGE(aud) |
| S17.4 | field-diagnostics/upgrade-bridge (`servi | 1 | NE | core/bugreport.py=NE | MERGE | PIŠEMO IZNOVA |
| S18.1 | worker/queue/scaling: | 25 | NE | core/queue.py=NE, core/store.py=NE, core/export.py=NE, core/ | MERGE | PIŠEMO IZNOVA |

## SAŽETAK

- Ukupno podsekcija S0-S18: **82**
- IZVOR **loose**: {'OBOJE': 9, 'MERGE': 70, 'PIŠEMO IZNOVA': 3}
- IZVOR **strogo**: {'OBOJE': 2, 'SALVAGE': 7, 'PIŠEMO IZNOVA': 52, 'MERGE(aud)': 15, 'RUPA': 6}

## RUPE — strogo (6) — NAJVAŽNIJE

- **S11.3** projektne-instrukcije: | sal=[nema]
- **S13.1** vision (`vision`): | sal=[core/vision.py=NE, core/imagegen.py=NE, core/canvas.py=NE]
- **S13.3** voice (`voice`): | sal=[core/voice.py=NE]
- **S13.4** video (`video`): | sal=[core/video.py=NE]
- **S14.4** IDE/ACP: | sal=[nema]
- **S16.5** trajectory-export (`destilacija`): | sal=[nema]

## Kandidati navedeni ali NISU full-code auditani (65) — README-razina

S0.2, S0.3, S0.4, S0.5, S1.1, S1.2, S1.3, S2.1, S2.2, S2.3, S2.4, S2.5, S2.6, S3.2, S3.3, S3.6, S4.1, S4.2, S4.5, S4.6, S4.7, S5.2, S5.3, S6.0, S6.1, S6.3, S6.4, S6.5, S6.6, S6.8, S6.9, S7.1, S7.3, S8.1, S8.3, S8.4, S8.5, S9.1, S9.3, S10.1, S10.4, S10.6, S10.7, S11.1, S11.2, S11.3, S11.5, S11.6, S12.4, S12.6, S13.1, S13.3, S13.4, S13.5, S14.1, S14.4, S14.5, S15.1, S15.3, S15.5, S16.1, S16.4, S16.5, S17.4, S18.1

## Salvage modul NE postoji na disku (51)

- S0.3: llm/providers.py=NE, core/capabilities.py=NE
- S0.4: core/store.py=NE
- S0.5: core/capabilities.py=NE
- S1.1: core/paths.py=NE
- S1.2: core/paths.py=NE, core/subproc.py=NE
- S1.3: core/trace.py=NE, core/otel.py=NE
- S2.1: llm/providers.py=NE
- S3.3: core/steer.py=NE, core/circuit.py=NE
- S4.2: core/subproc.py=NE, gortex/tool_executor.go=NE
- S4.3: gortex/diff_engine.go=NE
- S4.5: core/browse.py=NE
- S4.6: core/mcp.py=NE, gortex/mcp_server.go=NE
- S5.1: core/checkpoint.py=NE, core/memgit.py=NE
- S5.2: core/worktree.py=NE
- S5.3: core/provenance.py=NE, gortex/diff_engine.go=NE
- S6.0: core/permissions.py=NE, core/provenance.py=NE
- S6.1: core/permissions.py=NE, core/hooks.py=NE
- S6.3: core/egress.py=NE, core/netjail.py=NE
- S6.4: core/secrets.py=NE, core/broker.py=NE
- S6.5: core/provenance.py=NE, core/guardrail.py=NE, core/guardian.py=NE, core/canary.py=NE
- S6.8: core/supplychain.py=NE
- S7.1: core/recovery.py=NE
- S7.2: core/queue.py=NE, core/circuit.py=NE, core/fleet.py=NE
- S7.3: core/resume.py=NE, core/store.py=NE
- S8.2: core/compact.py=NE
- S8.3: core/archmap.py=NE, core/rank.py=NE, core/codeindex.py=NE, core/wiki.py=NE
- S8.5: core/crush.py=NE
- S9.3: core/reflexion.py=NE, core/reasonbank.py=NE
- S9.4: core/dream.py=NE
- S10.1: core/ingest.py=NE, core/pdf.py=NE, core/embed.py=NE, core/rerank.py=NE
- S10.4: core/kg.py=NE
- S11.1: core/promptlayer.py=NE
- S11.2: core/reversa.py=NE
- S11.5: managers/skill_=NE
- S12.1: managers/subagent_orchestrator.py=NE
- S12.2: core/workflow.py=NE
- S12.6: managers/council.py=NE
- S13.1: core/vision.py=NE, core/imagegen.py=NE, core/canvas.py=NE
- S13.3: core/voice.py=NE
- S13.4: core/video.py=NE
- S13.5: core/pdf.py=NE
- S14.1: tui/=NE, gateway/dashboard.py=NE
- S14.5: gateway/=NE
- S15.1: core/trace.py=NE, core/otel.py=NE
- S15.3: core/audit.py=NE, core/transcript.py=NE
- S15.5: core/health.py=NE, core/circuit.py=NE
- S16.4: core/reward.py=NE, core/credit.py=NE
- S16.6: core/confidence.py=NE, managers/council.py=NE
- S17.1: core/update.py=NE
- S17.4: core/bugreport.py=NE
- S18.1: core/queue.py=NE, core/store.py=NE, core/export.py=NE, core/pdf.py=NE, core/ingest.py=NE, core/fts.py=NE, core/embed.py=NE, memory/=NE

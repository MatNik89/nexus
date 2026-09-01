PLAN S12

# PLAN-S12 — S12 Orkestracija i multi-agent (`multi-agent` flag)

Jezgra `CGO_ENABLED=0`. S12 je MEHANIZAM (mi pišemo). **Ključna izolacija:** podagent = VLASTITI
worktree (5.2) — nema dijeljenog radnog stabla, nema dijeljenog context windowa. Gate: P2.7 (council),
P1.6 (channels/HITL token owner). 12.3 je POTROŠAČ (vlasnik 2.6).

---

## 12.1 Subagenti i delegacija (izolacija konteksta)

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- supervisor pattern → **LangGraph**
- role-based delegacija → **CrewAI**
- aktualni multi-agent workflow primitivi → **MAF 1.0** (nasljednik AutoGen+SK)

**Sinteza (subagent = vlastiti worktree iz 5.2; izolacija konteksta po zadatku):**
```go
type Subagent struct {
    manifest AgentManifest        // 11.6 validirani data-manifest (role/agent/skill)
    worktree *Worktree            // VLASTITI worktree iz 5.2 — nema dijeljenog stabla
    router   Router               // 12.3 (koji model podagentu — 2.6 ABI)
}
type Orchestrator struct{}
func (o *Orchestrator) Spawn(ctx, spec SubagentSpec) (*Subagent, error)
// delegacija = task-spec + result (NE shared context window):
//   podagent dobiva KOMPAKTAN task-spec (cilj + granice + output-contract), ne roditeljsku povijest
//   result = typed OutputContract (11.6), ne slobodni tekst — orchestrator verificira contract
// Magentic-One Task-/Progress-Ledger = referentni obrazac (research/legacy — matrica)
```
Komunikacija kroz eksplicitni contract, ne dijeljeni context → izbjegava "shared context = shared
amnesia" (obsidian nalaz).

**Salvage:** NEXUSv2 `subagent_runner.py` + `managers/subagent_orchestrator.py` (:87) →
**Python→Go port**.

**Ugovor/RED:** 5.2/P1.2 (worktree izolacija). RED (naš): dva podagenta ne dijele worktree (svaki
svoje stablo, sync-back eksplicitan); podagent ne vidi roditeljski context (task-spec + contract samo).

**Verifikacija:** LangGraph (MIT), CrewAI (MIT), MAF 1.0 (MIT, 4/2026) — stvarni.

**Pareto floor:** supervisor (LangGraph) + role-based (CrewAI) + multi-agent primitivi (MAF) +
worktree-per-subagent (naš — nitko ne izolira podagenta na razini radnog stabla) → ≥ svaki.

---

## 12.2 Workflow/DAG s determinističkim tokom

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- graf stanja s checkpointima → **LangGraph**
- durable execution standard → **Temporal**
- source-level workflow runtime (deterministički + model-led) → **Google ADK**

**Sinteza (DAG + cycle-guard + wave-scheduler + resume):**
```go
type DAG struct{ nodes map[string]Node; edges []Edge }
func (d *DAG) Validate() error // cycle → reject; unknown node → reject; duplicate edge → reject
func (d *DAG) TopoSort() ([]Wave, error) // wave-scheduler: nezavisni čvorovi → isti val (paralelno)
type Scheduler struct{}
func (s *Scheduler) Run(ctx, dag DAG, budget ResourceBudget) (*RunResult, error)
// val po val; resume iz checkpointa (7.3 journal offset); budget iz 7.2 (P2.2)
// deterministički korak i model-led korak u ISTOM grafu (ADK obrazac)
```
DAG je data (team.yaml), scheduler je kod — isti scheduler pokreće svaki team.

**Salvage:** NEXUSv2 `core/workflow.py` (:27) + `managers/subagent_orchestrator.py` (_run_dag :125)
→ **Python→Go port**.

**Ugovor/RED:** 12.2 spec (deterministički tok). RED (naš): cycle → `Validate` reject (fail-closed);
nepoznat node → reject; resume iz crasha → val se nastavlja od checkpointa (ne od nule).

**Verifikacija:** LangGraph (MIT), Temporal (MIT), Google ADK (Apache-2.0) — stvarni.

**Pareto floor:** graf+checkpoint (LangGraph) + durable (Temporal) + deterministički+model-led (ADK)
+ cycle-guard + wave-scheduler (naš) → ≥ svaki.

---

## 12.3 Model routing (POTROŠAČ — vlasnik 2.6)

**Tip:** MEHANIZAM (tanki potrošač)

**Sinteza (koji podagent dobiva koji model — kroz 2.6 ABI):**
```go
type SubagentRouter struct{ router Router } // konzumira 2.6 Router ABI (barbell/learned)
func (s *SubagentRouter) Route(sub Subagent, task Task) (Target, error)
// podagent = plan/execute/verify faza → barbell (2.6); naučeni router samo iza ABI
// capability floor (S0.3) + cost (2.5) + verified-score (16.4) — isti kriteriji kao 2.6
```
Nema zasebne logike — 12.3 je SAMO multi-agent potrošač kanonskog 2.6 vlasnika.

**Salvage:** NEXUSv2 `llm/router.py` → **Python→Go port** (već portirano za 2.6).

**Ugovor/RED:** 2.6 (fail-closed na capability floor). RED (naš): podagent na model ispod floora →
error (isti 2.6 RED, kroz potrošača).

**Verifikacija:** vidi 2.6 (RouteLLM/semantic-router/LiteLLM) — nema novih kandidata.

**Pareto floor:** nije zaseban mehanizam — potrošač 2.6; Pareto je u 2.6.

---

## 12.4 Human-in-the-loop točke

**Tip:** MEHANIZAM

**Aspekti → najbolji izvor:**
- interrupt/approve primitivi → **LangGraph**
- serializable HITL interruptions → **OpenAI Agents SDK**
- graph workflows + checkpointing + HITL → **MAF**

**Sinteza (gate + jezgreni decision-token, P1.6):**
```go
type Gate struct{}
func (g *Gate) Ask(ctx, intent EffectIntent, q string) (ApprovalChallenge, error) // WAITING
func (g *Gate) Answer(ctx, ch ApprovalChallenge, d Decision) error
// P1.6: ApprovalChallenge = exact-intent, expiring, single-use, signed — S6/S7 jezgra vlasnik
// adapter (channel) NE može sam nastaviti run; ponovni odgovor na terminalni challenge = replay
// stanja: WAITING → {APPROVED|DENIED|EXPIRED|CANCELLED}; samo S7 durable transition nastavlja run
```
HITL je durable-wait (7.3), ne busy-poll — čovjek može odgovoriti kasnije (kanal/CLI), run čeka.

**Salvage:** NEXUSv2 `core/gate.py` (ask :25, answer :55, consume :67) → **Python→Go port**.

**Ugovor/RED:** **P1.6** + `test_channels_contract_fails_closed` (tablični): A `channels` bez
`extensions/S6.8` → `INCOMPLETE_CAPABILITY_CLOSURE`; B isti validni approval dvaput → `APPROVAL_REPLAY`.

**Verifikacija:** LangGraph (MIT), OpenAI Agents SDK (MIT), MAF (MIT) — stvarni.

**Pareto floor:** interrupt/approve (LangGraph) + serializable (OpenAI SDK) + graph-HITL (MAF) +
jezgreni single-use token (naš — nitko ne veže HITL na S6/S7 token vlasništvo) → ≥ svaki.

---

## 12.5 Agent-to-agent interoperabilnost i discovery (A2A)

**Tip:** MEHANIZAM + ADAPTER (A2A spec)

**Aspekti → najbolji izvor:**
- kanonski wire ugovor (Agent Cards `/.well-known/agent-card.json`, task lifecycle) → **A2A spec (Linux Foundation)**
- A2A klijent + server + MCP u istom runtimeu → **MAF**
- auto Agent Card → **Google ADK** (experimental)

**Sinteza (A2A klijent/server + Agent Card + sigurnosna cijena):**
```go
type AgentCard struct{ /* name, description, skills[], capabilities[], auth, endpoint */ }
type A2AServer struct{} // serve card + task lifecycle (idempotent task IDs)
type A2AClient struct{}
func (c *A2AClient) Discover(ctx, url string) (AgentCard, error)
// SIGURNOSNA CIJENA (12.5 — card NIJE autorizacija):
//   validacija sheme + veličine (max_card_bytes) PRIJE parsea
//   allowlist + SSRF obrana pri discoveryju (6.3 — metadata IP / redirect re-check)
//   AuthN/AuthZ (P1.3 obrazac), timeout/cancel (S7), idempotent task ID-evi, audit korelacija (15.3)
// remote agent = UNTRUSTED peer (P0.1 ContextBlock trust_class) — izlaz ne postaje instrukcija
```
A2A referencira sheme iz 0.1 i capability model iz 0.3; materijalan samo uz `multi-agent`.

**Salvage:** NEXUSv2 MCP klijent/server obrazac (`core/mcp.py`/`mcpserve.py`) → **Python→Go port** kao
A2A šablon (A2A je nov, MCP je već provjereni transport/auth obrazac).

**Ugovor/RED:** 12.5 spec (sigurnosna cijena) + P1.3-adjacent. RED (naš): A2A discovery na SSRF
(metadata IP) → refuse; card preko size limita → reject; idempotent task ID — dupli task → isti rezultat,
ne dupli side-effect.

**Verifikacija:** A2A spec (Linux Foundation, v1.0.1, 150+ org), MAF (MIT), Google ADK (Apache-2.0,
experimental) — stvarni.

**Pareto floor:** kanonski wire ugovor (A2A) + klijent/server (MAF) + auto-card (ADK) + **sigurnosna
cijena (card≠authz, SSRF, idempotent-task)** (naš) → ≥ svaki.

---

## 12.6 Multi-persona deliberation (Council)

**Tip:** MEHANIZAM (P2.7 ugovor)

**Aspekti → najbolji izvor:**
- 5 persona → anonimni cross-review → chairman → **NEXUS council.py**
- group deliberation → **MAF** / hierarchical → **CrewAI** / supervisor → **LangGraph**

**Sinteza (sealed nezavisni review → anon cross → chairman sinteza, dissent OČUVAN):**
```go
type Council struct{}
type CouncilRequest struct {
    CouncilID, RunID, ArtifactRevisionHash, Question string
    MemberSpecs []MemberSpec // member_id, role_id, model_policy, context_policy
    MinQuorum int; IndependencePolicy, AnonymizationPolicy string
    EvidenceRefs []string; Deadline time.Time; BudgetID string
    ChairSpec ChairSpec; AggregationRule, DissentPolicy, OutputSchemaVersion string
}
func (c *Council) Run(req CouncilRequest) (Verdict, error)
// FORMED → INPUTS_SEALED → INDEPENDENT_REVIEW (član ne vidi tuđe zapise prije sealed commita)
// → CROSS_REVIEW (identitet skriven) → SYNTHESIS → {VERIFIED|FAILED_QUORUM|REJECTED}
// chairman NE smije izbaciti materijalni dissent bez adjudication-record
// quorum+budget hard gate; output = majority + dissent + unresolved
// council NE može sam promovirati NI zamijeniti 16.6 checker (deterministički gate neovisan)
```
Council je `multi-agent` STRATEGIJA koju 16.6 KONZUMIRA — ali promotion-gate (16.6) ostaje
deterministički i neovisan.

**Salvage:** NEXUSv2 `managers/council.py` (5 persona → anonimni cross → chairman) →
**Python→Go port**.

**Ugovor/RED:** **P2.7** + `test_council_cannot_drop_sealed_material_dissent` — sealed review s
materijalnim dokazom proturječi većini, chairman ga izostavi → `DISSENT_DROPPED`, council `REJECTED`,
promotion gate ne dobiva success.

**Verifikacija:** NEXUS council.py (interni), MAF (MIT), CrewAI (MIT), LangGraph (MIT) — stvarni.

**Pareto floor:** persona+anonimni (NEXUS) + group deliberation (MAF) + hierarchical (CrewAI) +
supervisor (LangGraph) + **sealed-nezavisnost + dissent-očuvanje + hard-quorum** (naš, P2.7) → ≥ svaki.

---

## S12 cross-cutting napomene

1. **Izolacija je srž S12:** podagent = vlastiti worktree (5.2) + kompaktan task-spec + typed
   OutputContract (11.6) — NIKAD dijeljeni context window ni dijeljeno radno stablo (amnesia vektor).
2. **12.3 je potrošač, ne vlasnik:** routing logika je u 2.6; 12.3 samo bira model po podagentu kroz
   isti ABI. Nema duplicirane routing logike.
3. **12.5 sigurnosna cijena je obvezna, ne opcionalna:** Agent Card NIJE autorizacija — SSRF/
   size-validacija/idempotent-task/auth su jezgra; remote agent = UNTRUSTED peer (P0.1).
4. **12.6 council ≠ 16.6 checker:** council je multi-persona STRATEGIJA (stohastična, quorum); 16.6
   je deterministički gate (git-diff+exit-code) — council NE može promovirati ni zamijeniti checker.
5. **Honest gap:** 12.1/12.2/12.3/12.5 nemaju dedicirani Annex RED (gate = 5.2/P1.2 + 2.6 + P1.3
   posredno); 12.4 ima P1.6, 12.6 ima P2.7. Ako moderator želi simetriju: P0.16 "A2A-discovery-never-
   without-SSRF-guard".
6. **Verifikacija:** svi kandidati stvarni (LangGraph/CrewAI/OpenAI-SDK/Temporal MIT, MAF MIT, ADK
   Apache-2.0, A2A Linux Foundation); salvage `subagent_runner/subagent_orchestrator/workflow/council/
   gate` interni (postoje — audit potvrdio), port je logika.

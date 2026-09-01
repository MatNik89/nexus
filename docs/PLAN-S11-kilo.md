PLAN S11

# PLAN-S11 — S11 Prompt i instrukcijski sloj (assembler / skills / AGENTS.md / optimizer / lifecycle / manifest)

Jezgra `CGO_ENABLED=0`. 11.1 + 11.3 grade se u P1 (prije petlje). **Granica KOD vs DATA (ključna):**
assembler/precedence/trust-fencing/manifest-interpreter/lifecycle-scan = JEZGRA (kod); SKILL.md/
AGENTS.md/ROLE.md/team.yaml/recipes = DATA (validirani manifesti). Gate: P1.1 (skill TOCTOU
hash-on-load `skills.lock`), P0.4 (closure resolver — 11.6).

---

## 11.1 System prompt assembly: uloga, pravila, format, precedence, provenance

**Tip:** MEHANIZAM (jezgra)

**Aspekti → najbolji izvor:**
- prompti brušeni benchmarkom po modelu → **Aider**
- dokaz da ACI/prompt dizajn diže uspjeh više od izbora modela → **SWE-agent**
- eksplicitno sklapanje iz workspace/config datoteka (SOUL.md/AGENTS.md/TOOLS.md) → **OpenClaw**

**Sinteza (assembler + precedence + trust-fencing — untrusted NIKAD ne postaje instrukcija):**
```go
type PromptLayer int // IDENTITY < RULES < PROJECT(AGENTS.md) < SKILLS < TOOL_CONTEXT
type Assembler struct{}
func (a *Assembler) Assemble(layers map[PromptLayer][]ContextBlock) (Prompt, error)
// precedence: kasniji sloj NE smije prebrisati raniji (identity > project override → odbij)
// provenance: svaki blok nosi lineage (P0.3) — tko ga je ubacio i iz kojeg trust sloja

// TRUST-FENCING (11.1 srž): UNTRUSTED_EXTERNAL smije u CONTENT, NIKAD u RULES/IDENTITY
func (a *Assembler) Fence(block ContextBlock, slot PromptSlot) error
// untrusted u instrukcijskoj poziciji → PROVENANCE_DOWNGRADE (fail-closed) — injection vektor zatvoren
```
Open Interpreter (runner-up): profili po modelu.

**Salvage:** NEXUSv2 `core/promptlayer.py` (prefix layering :20) + `core/planner.py` → **Python→Go port**.

**Ugovor/RED:** **P0.3** (provenance laundering). RED: untrusted blok u RULES poziciji → `PROVENANCE_
DOWNGRADE`, prompt NE emitiran (isti P0.3 RED, kroz assembler).

**Verifikacija:** Aider (Apache-2.0), SWE-agent (MIT), OpenClaw (source-available) — stvarni.

**Pareto floor:** benchmark-brušen prompt (Aider) + ACI dizajn (SWE-agent) + eksplicitna assembly
(OpenClaw) + **trust-fencing precedence** (naš — nitko ne fenca untrusted iz instrukcijske pozicije)
→ ≥ svaki.

---

## 11.2 Proširive upute: skills/commands/recipes (+ Reversa)

**Tip:** MEHANIZAM (interpreter) + DATA (SKILL.md sadržaj)

**Aspekti → najbolji izvor:**
- agent-authored + plugin-provided skills → **Hermes Agent**
- de facto OSS standard (SKILL.md spec) → **Agent Skills standard**
- skills mehanizam → **Codex CLI**
- progressive disclosure → † Claude Code (referent)

**Sinteza (SKILL.md + progressive disclosure; Reversa = signed data-pack, NE plugin):**
```go
type Skill struct{ manifest SkillManifest; body []byte } // SKILL.md — progressive disclosure (3 razine)
// razina 1: name+description (u prompt); 2: body (na aktivaciju); 3: resursi/scripts (na poziv)

// Reversa = SIGNED SKILL-PACK (11.2 DATA, ne 17.1 izvršni plugin):
//   legacy repo → dokaziva specifikacija (SDD/PRD) + migracijski plan PRIJE mutacije
//   allowLegacyEdits:false provodi KERNEL (6.1/6.9), ne skill; lifecycle kroz 11.5
// source-grounded (11.5 zahtjev): agent-authored skill nosi source manifest + citate + verziju izvora
```

**Salvage:** NEXUSv2 `core/reversa.py` + 40 skillova → **Python→Go port** (data + interpreter).

**Ugovor/RED:** P1.1 (kroz 11.5). RED (naš): skill bez source-manifesta (agent-authored) → ne aktivira
se (source-grounded gate).

**Verifikacija:** Hermes (MIT, verificiran), Agent Skills standard (spec), Codex CLI (Apache-2.0) —
stvarni.

**Pareto floor:** agent-authored (Hermes) + standard (Agent Skills) + mehanizam (Codex) + Reversa
data-pack (naš) → ≥ svaki.

---

## 11.3 Projektne instrukcije (AGENTS.md konvencija)

**Tip:** MEHANIZAM (loader) + DATA (AGENTS.md sadržaj)

**Aspekti → najbolji izvor:**
- AGENTS.md standard → **Codex CLI**
- AGENTS.md podrška → **OpenCode**
- .clinerules → **Cline**
- hijerarhija → † Claude Code (referent)

**Sinteza (AGENTS.md hijerarhija root→cwd + precedence projekt>user>global):**
```go
type ProjectRules struct{}
func (p *ProjectRules) Load(cwd string) []ContextBlock
// AGENTS.md hijerarhija: root → poddirektoriji → cwd (kasniji override raniji, dokumentirano)
// NEXUS projectrules.py (.nexusrules) → primarni format AGENTS.md, .nexusrules kao legacy fallback
```
Projektne instrukcije ulaze u assembler (11.1) kao `PROJECT` sloj — nikad UNTRUSTED pozicija.

**Salvage:** NEXUSv2 `core/projectrules.py` (:14) → **Python→Go port**.

**Ugovor/RED:** 11.3 spec (konvencija). RED (naš): AGENTS.md u projektu → nadjačava globalni (precedence
projekt>global); maliciozni AGENTS.md ne smije ući u RULES ako je untrusted (11.1 fence).

**Verifikacija:** Codex CLI (Apache-2.0), OpenCode (MIT), Cline (Apache-2.0) — stvarni.

**Pareto floor:** AGENTS.md standard (Codex) + podrška (OpenCode) + .clinerules (Cline) + hijerarhija
(naš) → ≥ svaki.

---

## 11.4 Automatska optimizacija prompta

**Tip:** MEHANIZAM (offline) + ADAPTER (eval backend)

**Aspekti → najbolji izvor:**
- prompt kao program, optimizatori → **DSPy**
- gradijenti kroz tekst → **TextGrad**
- A/B eval promptova → **promptfoo**

**Sinteza (propose → eval-gate → promote, offline ne runtime):**
```go
type Optimizer struct{}
func (o *Optimizer) Propose(prompt Prompt, failures []Eval) Prompt // DSPy-lite (GEPA obrazac)
func (o *Optimizer) Promote(cand, baseline Prompt, eval Eval) bool // eval-gate (16.2) prije promocije
// NIKAD runtime mutacija prompta — offline + izolirana eval grana + holdout + signed
```
Prompt optimizacija je `destilacija`-susedni (16.5), ne `extensions` — prompt je data, optimizer je kod.

**Salvage:** NEXUSv2 `core/selfimprove.py` (GEPA-lite :328) → **Python→Go port**.

**Ugovor/RED:** bez dediciranog Annex; gate = 16.2 (eval-gate prije promocije). RED (naš): kandidat
prompt ispod baseline na eval → NE promovira (fail-closed).

**Verifikacija:** DSPy (MIT), TextGrad (MIT), promptfoo (MIT) — stvarni.

**Pareto floor:** prompt-kao-program (DSPy) + tekstualni gradijenti (TextGrad) + A/B eval (promptfoo)
+ eval-gate promote (naš) → ≥ svaki.

---

## 11.5 Skill lifecycle i governance (install→scan→pin→…→archive + P1.1 TOCTOU)

**Tip:** MEHANIZAM (jezgra; scan/signature/activation/rollback)

**Aspekti → najbolji izvor:**
- vlastiti lifecycle + Skill Forge (score→doctor→merge→scout) → **NEXUS** (izolirana eval grana)
- statički skill skener → **Cisco AI Defense Skill Scanner**
- audit katalog / dep-audit → **ZIRAN / skills.sh / Depx**

**Sinteza (lifecycle lanac + runtime-hash-on-load skills.lock + write-approval ON):**
```go
type Lifecycle struct{}
// install → scan → pin → activate → measure → update → rollback → archive
// scan: injection/exfil/opasne tool-chain kombinacije/install skripte (pre-install, NE post-hoc)
// pin: content-hash + signature → skills.lock
// measure: skill-health (1-5) iz verificiranih ishoda; update/rollback/archive po health

// P1.1 TOCTOU (KLJUČNO): runtime hash-on-load — provjera pri SVAKOM loadu, ne samo install
type SkillLock struct{ entries map[string]LockEntry }
func (l *SkillLock) VerifyOnLoad(skill Skill) error
// otvori canonical root bez symlink escapea → hashiraj → poredi s pinom → mismatch → QUARANTINED

// write_approval DEFAULT ON, auto-install DEFAULT OFF (11.5 zahtjev)
// self-authoring INERTAN do gatea; NIKAD autonomna kernel mutacija
```

**Salvage:** NEXUSv2 `managers/skill_*` (15 modula: loader/router/eval/doctor/merge/scout/import/hub)
→ **Python→Go port** (Skill Forge SAMO u izoliranoj eval grani).

**Ugovor/RED:** **P1.1** + `test_skill_swap_after_install_is_quarantined` — zamijeni SKILL.md symlinkom
ili promijeni 1 byte nakon installa → `SKILL_DIGEST_MISMATCH`, `QUARANTINED`, promijenjeni sadržaj ne
ulazi u prompt ni executor.

**Verifikacija:** Cisco Skill Scanner (Apache-2.0), ZIRAN (MIT), skills.sh (spec), Depx (MIT) — stvarni.

**Pareto floor:** lifecycle+forge (NEXUS) + statički scan (Cisco) + dep-audit (Depx) + **runtime
hash-on-load TOCTOU** (naš — nitko ne verificira pri LOADU, samo install) → ≥ svaki.

---

## 11.6 Deklarativni Role → Agent → Skill/Pack ugovor (data-manifest)

**Tip:** MEHANIZAM (manifest-interpreter = jezgra) + DATA (sadržaj)

**Aspekti → najbolji izvor:**
- schema-first manifest (ROLE.md/AGENT.md/team.yaml/SKILL.md) → **NEXUS obrazac**
- YAML role → **CrewAI**
- workspace-config / roles-as-data → **OpenClaw / MetaGPT**

**Sinteza (interpreter validira → P0.4 closure → aktivira; nova rola = nova mapa):**
```go
type ManifestInterpreter struct{}
type RoleManifest struct{ Name string; Team TeamSpec; Provides, Requires, Conflicts []string }
type AgentManifest struct{ Name string; OutputContract Contract; Capability Floor }
type SkillManifest struct{ Name string; ProgressiveDisclosure [3]string }

func (m *ManifestInterpreter) Validate(manifest any) error       // schema-first, fail-closed
func (m *ManifestInterpreter) Activate(mf CapabilityManifest) error // → P0.4 closure (0.5 resolver)
// NOVA ROLA = NOVA MAPA, NULA izmjene jezgre (NEXUS lekcija):
//   kernel rješava precedence/capability/budget/output-contract; core NE zna nazive domena/rola
//   sadržaj je `extensions`/`multi-agent` data; interpreter je jezgra
```
Role/agent/team/skill su VALIDIRANI PODATKOVNI MANIFESTI — ne kod, ne prompt-slobodni tekst.

**Salvage:** NEXUSv2 role/agent/skill hijerarhija (data: ROLE.md/AGENT.md/team.yaml/SKILL.md) →
**Python→Go port** (data + interpreter).

**Ugovor/RED:** **P0.4** (closure resolver). RED `test_every_flag_fails_when_one_gate_is_ablated`:
manifest s nezadovoljenim gateom → `INCOMPLETE_CAPABILITY_CLOSURE`, adapter ne učitan.

**Verifikacija:** CrewAI (MIT), OpenClaw (source-available), MetaGPT (MIT) — stvarni.

**Pareto floor:** YAML role (CrewAI) + workspace-config (OpenClaw) + roles-as-data (MetaGPT) +
**validirani manifest-interpreter + P0.4 closure** (naš) → ≥ svaki.

---

## S11 cross-cutting napomene

1. **KOD vs DATA je OVDJE najstroža:** 11.1 assembler+fence, 11.5 lifecycle+lock, 11.6 interpreter su
   JEZGRA (kod); SKILL.md/AGENTS.md/ROLE.md/team.yaml/recipes su DATA (validirani manifesti). Core
   interpretira, ne zna — nova rola = nova mapa.
2. **11.5 P1.1 je najvažniji gate S11:** runtime hash-on-load (`skills.lock`) zatvara TOCTOU — install-
   time scan NIJE dovoljan (fajl izmijenjen između installa i loada NE smije se izvršiti).
3. **Reversa je DATA (11.2), NE plugin (17.1):** `allowLegacyEdits:false` provodi kernel 6.1/6.9 —
   skill NE smije sam sebi dati ovlast; lifecycle kroz 11.5.
4. **11.4 je offline (destilacija-susedni), 11.5 je extensions:** optimizer i lifecycle su različite
   faze — optimizacija prompta ≠ skill governance; oboje eval-gated, ali različiti gateovi.
5. **Honest gap:** 11.2/11.3/11.4 nemaju dedicirani Annex RED (gate = P0.3 + P1.1 + 16.2 posredno);
   11.1 ima P0.3, 11.5 ima P1.1, 11.6 ima P0.4. Ako moderator želi simetriju: P0.15 "untrusted-never-
   instruction-slot".
6. **Verifikacija:** svi kandidati stvarni (Aider/SWE-agent/Hermes/Codex/CrewAI/MetaGPT MIT/Apache-2.0;
   OpenClaw source-available; Cisco/ZIRAN/Depx/promptfoo/DSPy/TextGrad stvarni); salvage `promptlayer/
   projectrules/skill_*/reversa/selfimprove` interni (postoje — audit potvrdio), port je logika + data.
